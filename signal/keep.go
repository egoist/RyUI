package signal

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// Keeping state across reloads (nui dev): the app's Go code restarts on a
// change, and its signals come back with their values, as React's Fast
// Refresh keeps hooks' state. A signal is known by where it's made: the
// call path (function names, not lines, so edits elsewhere don't move
// it), its type, and its place among the live signals of that type made
// there. Adding a signal of a type before others of that type made at
// the same place, or changing its type, resets those; values that don't
// survive JSON (funcs, unexported fields) aren't kept. Off in a built
// app: then New pays one nil check.

var keep *keeper

type keeper struct {
	saved map[string][]byte
	live  map[string]func() ([]byte, bool)
	used  map[string]map[int]bool // the places taken, by call path
}

// KeepState turns keeping on, with the values saved by the last run (nil
// for none). Call it before the app makes signals.
func KeepState(saved map[string][]byte) {
	if saved == nil {
		saved = map[string][]byte{}
	}
	keep = &keeper{saved: saved, live: map[string]func() ([]byte, bool){}, used: map[string]map[int]bool{}}
}

// KeptState returns the live signals' values, to give to the next run's
// KeepState; they're also restored if the app is mounted again in this
// one (a player reconnecting). Main thread.
func KeptState() map[string][]byte {
	if keep == nil {
		return nil
	}
	out := make(map[string][]byte, len(keep.live))
	for k, f := range keep.live {
		if b, ok := f(); ok {
			out[k] = b
		}
	}
	for k, v := range out {
		keep.saved[k] = v
	}
	return out
}

// core is the prefix of nui's own packages: their signals (the
// environment, list rows) are the framework's state, not the app's.
var core = []string{
	"github.com/egoist/nui/ui.", "github.com/egoist/nui/signal.", "github.com/egoist/nui/app.",
	"github.com/egoist/nui/internal/remote.", "github.com/egoist/nui/internal/boot.",
}

func callPath() (string, bool) {
	var pcs [48]uintptr
	n := runtime.Callers(4, pcs[:]) // above keepSignal and New
	frames := runtime.CallersFrames(pcs[:n])
	var b strings.Builder
	first := true
	for {
		f, more := frames.Next()
		if first {
			for _, p := range core {
				if strings.HasPrefix(f.Function, p) {
					return "", false
				}
			}
			first = false
		}
		if !strings.HasPrefix(f.Function, "runtime.") {
			b.WriteString(f.Function)
			b.WriteByte('<')
		}
		if !more {
			break
		}
	}
	return b.String(), true
}

func keepSignal[T any](s *Signal[T], equal func(a, b T) bool) {
	if equal == nil {
		return // can't tell a value survived JSON
	}
	path, ok := callPath()
	if !ok {
		return
	}
	// Places are counted per type: a new string signal doesn't move the
	// ints made at the same place.
	var zero T
	path += fmt.Sprintf("%T", zero)
	k := keep
	used := k.used[path]
	if used == nil {
		used = map[int]bool{}
		k.used[path] = used
	}
	i := 0
	for used[i] {
		i++
	}
	used[i] = true
	key := path + "#" + strconv.Itoa(i)
	if b, ok := k.saved[key]; ok {
		delete(k.saved, key)
		var v T
		if json.Unmarshal(b, &v) == nil {
			s.value = v
		}
	}
	k.live[key] = func() ([]byte, bool) {
		b, err := json.Marshal(s.value)
		if err != nil {
			return nil, false
		}
		var back T
		if json.Unmarshal(b, &back) != nil || !equal(back, s.value) {
			return nil, false
		}
		return b, true
	}
	// Gone with its owner.
	OnCleanup(func() {
		delete(k.live, key)
		delete(used, i)
	})
}

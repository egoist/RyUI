package ui

import (
	"encoding/binary"
	"errors"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// calls are the Calls waiting for their Result, by call id.
var (
	calls    = map[uint32]func([]byte, error){}
	nextCall uint32
)

// Call runs method of a native module (the host's own, such as
// "secureStore" or "photos", or one the app registers in Swift and
// Kotlin), with args; done gets its result, on the main thread. Main
// thread.
//
// Packages wrap the built-in modules (ryui/secure); an app's own modules
// take whatever bytes it gives them, JSON for instance.
func Call(module, method string, args []byte, done func(result []byte, err error)) {
	r := activeRoot()
	if r == nil {
		if done != nil {
			done(nil, errors.New("ui: Call before Mount"))
		}
		return
	}
	nextCall++
	id := nextCall
	if done != nil {
		calls[id] = done
	}
	r.batch.CommandArgs(r.el.id, ops.CmdCall, func(b []byte) []byte {
		le := binary.LittleEndian
		b = le.AppendUint32(b, id)
		b = le.AppendUint32(b, uint32(len(module)))
		b = append(b, module...)
		b = le.AppendUint32(b, uint32(len(method)))
		b = append(b, method...)
		return append(b, args...)
	})
}

// CallWait is Call from a goroutine: it waits for the result. Never on
// the main thread, which the result comes on.
func CallWait(module, method string, args []byte) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	Post(func() {
		Call(module, method, args, func(b []byte, err error) { ch <- result{b, err} })
		flushAll()
	})
	r := <-ch
	return r.b, r.err
}

// ViewID is the id the host knows n's view by, for a package's module
// call that points at a view (the button a popover comes from, the
// WebView to print); 0 if n has no view of its own, or not yet (its
// effects haven't run, it's disposed). A host answers a module call for
// a view it doesn't have as for none. Main thread.
func ViewID(n Node) uint32 {
	if n.e == nil || !n.e.real || n.e.dead {
		return 0
	}
	return uint32(n.e.id)
}

// result delivers a Result event to its Call.
func result(b []byte) {
	if len(b) < 5 {
		return
	}
	id := binary.LittleEndian.Uint32(b)
	done := calls[id]
	if done == nil {
		return
	}
	delete(calls, id)
	if b[4] == 0 {
		done(nil, errors.New(string(b[5:])))
		return
	}
	done(append([]byte(nil), b[5:]...), nil)
}

// urlHandlers get the URLs the app is opened with; pendingURL is one
// that came before any handler (the URL the app launched with).
var (
	urlHandlers []*func(string)
	pendingURL  string
)

// OnOpenURL calls f with each URL the app is opened with: its scheme
// (ryui.json's "scheme", myapp://...), or a universal or app link
// (ryui.json's "links"), at launch or since. A URL that came before any
// handler (the one the app launched with) goes to the first. It stops
// with the component's owner. Main thread.
func OnOpenURL(f func(url string)) {
	p := &f
	urlHandlers = append(urlHandlers, p)
	signal.OnCleanup(func() {
		for i, h := range urlHandlers {
			if h == p {
				urlHandlers = append(urlHandlers[:i], urlHandlers[i+1:]...)
				return
			}
		}
	})
	if pendingURL != "" {
		u := pendingURL
		pendingURL = ""
		f(u)
	}
}

func incoming(url string) {
	if len(urlHandlers) == 0 {
		pendingURL = url
		return
	}
	hs := make([]*func(string), len(urlHandlers))
	copy(hs, urlHandlers)
	for _, h := range hs {
		(*h)(url)
	}
}

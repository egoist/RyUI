package signal_test

import (
	"sync"
	"testing"

	"github.com/egoist/ryui/signal"
)

func TestAmbientEffect(t *testing.T) {
	runs := 0
	var dispose func()
	signal.Root(func(d func()) {
		dispose = d
		signal.RenderEffect(func() {
			runs++
			signal.Ambient()
		})
	})
	signal.AmbientChanged()
	if runs != 2 {
		t.Errorf("after a change: %d runs, want 2", runs)
	}
	signal.AmbientChanged()
	if runs != 3 {
		t.Errorf("after another: %d runs, want 3", runs)
	}
	dispose()
	signal.AmbientChanged()
	if runs != 3 {
		t.Errorf("after disposal: %d runs, want 3", runs)
	}
}

// An effect follows what it reads alone: an effect it makes, or what it
// runs untracked, reads for itself. Show's and Dynamic's effects, which
// build whole subtrees, must not rebuild them when the language changes.
func TestAmbientIsTheReaders(t *testing.T) {
	var outer, inner, untracked, memoReader, plain int
	lang := "en"
	var dispose func()
	signal.Root(func(d func()) {
		dispose = d
		signal.RenderEffect(func() {
			outer++
			signal.RenderEffect(func() {
				inner++
				signal.Ambient()
			})
		})
		signal.RenderEffect(func() {
			untracked++
			signal.Untrack(func() int {
				signal.Ambient()
				return 0
			})
		})
		signal.RenderEffect(func() {
			plain++
			owner := signal.NewOwner()
			signal.RunWithOwner(owner, signal.Ambient)
			owner.Dispose()
		})
		// A memo that reads it changes its readers only if its value did.
		m := signal.NewMemo(func() string {
			signal.Ambient()
			return lang
		})
		signal.RenderEffect(func() {
			memoReader++
			m.Get()
		})
	})
	defer dispose()
	signal.AmbientChanged()
	if inner != 2 || outer != 1 || untracked != 1 || plain != 1 || memoReader != 1 {
		t.Errorf("inner %d (want 2), outer %d, untracked %d, plain %d, memo's reader %d (want 1)", inner, outer, untracked, plain, memoReader)
	}
	lang = "de"
	signal.AmbientChanged()
	if memoReader != 2 {
		t.Errorf("memo's reader %d, want 2", memoReader)
	}
}

// A read and a signal's read both subscribe, in either order, run after
// run, and the links are reused.
func TestAmbientWithSignals(t *testing.T) {
	s := signal.New(1)
	runs := 0
	var dispose func()
	signal.Root(func(d func()) {
		dispose = d
		signal.RenderEffect(func() {
			runs++
			s.Get()
			signal.Ambient()
		})
	})
	defer dispose()
	s.Set(2)
	signal.AmbientChanged()
	s.Set(3)
	signal.AmbientChanged()
	if runs != 5 {
		t.Errorf("%d runs, want 5", runs)
	}
	if a := testing.AllocsPerRun(100, signal.AmbientChanged); a != 0 {
		t.Errorf("a change allocates %v times", a)
	}
}

// Reads from goroutines race with nothing, as go test -race checks.
func TestAmbientFromGoroutines(t *testing.T) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				signal.Ambient()
			}
		}()
	}
	var dispose func()
	signal.Root(func(d func()) {
		dispose = d
		signal.RenderEffect(func() {})
	})
	defer dispose()
	for range 100 {
		signal.AmbientChanged()
	}
	wg.Wait()
	signal.AmbientChanged() // whatever was subscribed by chance is gone
}

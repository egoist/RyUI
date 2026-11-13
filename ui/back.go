package ui

import (
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// backScope is where the user's going back is taken: a screen (popping
// it), a sheet (dismissing it) or the root (leaving the app).
type backScope struct {
	e   *element
	fns []*func() bool // OnBack's, in the order they came
	// fallback is what going back does when no handler takes it: pops
	// the screen, closes the sheet; nil for the root (the host leaves).
	fallback func()
	listens  bool
}

// backCtx passes the scope OnBack registers in: the nearest screen
// (above a stack's first) or sheet; none: the root's.
var backCtx = signal.NewContext[*backScope](nil)

func newBackScope(e *element, fallback func()) *backScope {
	return &backScope{e: e, fallback: fallback}
}

// OnBack takes the user's going back from where the component is: a
// screen pushed on a Stack (its back button, Android's back button and
// gesture, the edge swipe on iOS), a sheet (a swipe down, a tap outside,
// Android's back), or the app itself (Android's back with no screen left
// to pop) for a component outside them or on a stack's first screen.
// f runs instead, the latest registered first, and returns true when it
// took it: to ask before discarding a draft, or to close a panel of the
// screen's own first. When every handler returns false, going back goes
// on as it would have: the screen pops, the sheet closes, the app
// leaves.
//
// While a screen or sheet has a handler, the platform doesn't animate
// the user's going back interactively: iOS's edge swipe is off on that
// screen and its sheet can't be swiped away (a swipe down asks f
// instead), and Android's predictive back doesn't preview the pop. On
// iOS the root has no back to take.
//
// Call it while building a component; the handler goes when the
// component's owner is disposed.
//
//	ui.OnBack(func() bool {
//		if !dirty.Peek() {
//			return false
//		}
//		ui.Alert(ui.AlertProps{Title: "Discard changes?", Actions: ...})
//		return true
//	})
func OnBack(f func() bool) {
	s := backCtx.Use()
	if s == nil {
		r := current()
		if r.back == nil {
			r.back = newBackScope(r.el, nil)
		}
		s = r.back
	}
	s.add(f)
}

func (s *backScope) add(f func() bool) {
	p := &f
	s.fns = append(s.fns, p)
	if !s.listens {
		s.listens = true
		s.e.handlers().back = s.run
		s.e.listen(ops.EventBack)
	}
	if len(s.fns) == 1 {
		s.e.r.batch.SetBool(s.e.id, ops.PropInterceptBack, true)
	}
	signal.OnCleanup(func() {
		for i, x := range s.fns {
			if x == p {
				s.fns = append(s.fns[:i], s.fns[i+1:]...)
				break
			}
		}
		if len(s.fns) == 0 && !s.e.dead {
			s.e.r.batch.SetBool(s.e.id, ops.PropInterceptBack, false)
		}
	})
}

// run is the host's Back: the handlers, latest first, then the default.
func (s *backScope) run() {
	for i := len(s.fns) - 1; i >= 0; i-- {
		if i >= len(s.fns) {
			continue // a handler removed others
		}
		if (*s.fns[i])() {
			return
		}
	}
	if s.e.dead {
		return
	}
	if s.fallback != nil {
		s.fallback()
		return
	}
	s.e.r.batch.Command(s.e.id, ops.CmdBackExit, nil)
}

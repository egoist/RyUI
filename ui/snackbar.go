package ui

import (
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// SnackbarDuration is how long a Snackbar shows.
type SnackbarDuration = ops.SnackbarDuration

const (
	// SnackbarShort shows it for about four seconds (Material's short
	// duration, the default).
	SnackbarShort = ops.SnackbarDurationShort
	// SnackbarLong shows it for about ten seconds.
	SnackbarLong = ops.SnackbarDurationLong
	// SnackbarIndefinite shows it until it's dismissed (the dismiss
	// func, the user's swipe, another snackbar) or its action is pressed.
	SnackbarIndefinite = ops.SnackbarDurationIndefinite
)

// SnackbarProps configures a Snackbar.
type SnackbarProps struct {
	// Message is what it says. It's shown once, so it's a string, as an
	// Alert's is.
	Message string
	// Action is its button (Undo): its Title, which follows the app's
	// language while it shows, and its OnPress, which runs as the button
	// dismisses it. Its Role, Icon, Disabled and Children aren't shown.
	Action *Action
	// Duration is how long it shows (SnackbarShort: the default). Hosts
	// lengthen it as the platform recommends for accessibility: on
	// Android, AccessibilityManager's recommended timeout, which keeps
	// one with an action longer while a screen reader is on.
	Duration SnackbarDuration
	// OnDismiss runs when it goes away other than by its action: it timed
	// out, the user swiped it away, another snackbar replaced it, or
	// dismiss was called. After an Undo's OnPress it doesn't run, so it's
	// where a deletion the action could undo is made for good.
	OnDismiss func()
}

// snackbar is a Snackbar showing.
type snackbar struct {
	r     *Root
	win   *element // the window it's on (the root's, or a Window node)
	e     *element
	owner *signal.Owner // its action's title effect; its cleanup destroys it
	p     SnackbarProps
}

// Snackbar shows a brief message at the bottom of the window now, with
// an optional action such as Undo, and returns a func that dismisses it
// (a no-op once it's gone). A new snackbar replaces the one showing on
// its window: the old one is dismissed first, and its OnDismiss runs;
// another window's stays. iOS has none of
// its own, so the host draws a capsule in the system material above the
// tab bar or toolbar and the keyboard; Android shows Material's
// Snackbar (com.google.android.material.snackbar.Snackbar) above the
// screen's bottom bar, FAB and the keyboard; macOS an overlay in an
// NSVisualEffectView at the bottom of the window; Windows an overlay
// drawn as Fluent's InfoBar; Linux an AdwToast in an AdwToastOverlay
// around the window's content; the web an element with role status. A
// swipe dismisses it on touch screens, and screen readers announce it.
// On a desktop it goes on the window an Alert would (that of the view
// whose event shows it, else of the component calling it, else the main
// window).
func Snackbar(p SnackbarProps) (dismiss func()) {
	r := activeRoot()
	win := alertWindow(r)
	for old := r.snacks[win]; old != nil; old = r.snacks[win] {
		old.dismiss() // its OnDismiss might show another
	}
	s := &snackbar{r: r, win: win, p: p}
	withRoot(r, func() Node {
		e := newElement(ops.KindSnackbar, true)
		s.e = e
		b := r.batch
		b.SetInt(e.id, ops.PropSnackbarWindow, int64(win.id))
		if p.Message != "" {
			b.SetString(e.id, ops.PropSnackbarMessage, p.Message)
		}
		if p.Duration != SnackbarShort {
			b.SetEnum(e.id, ops.PropSnackbarDuration, int32(p.Duration))
		}
		// An owner of its own, under the root's: its action's title
		// follows the app's language while it shows, and disposing the
		// root takes it down.
		signal.RunWithOwner(r.owner, func() {
			s.owner = signal.NewOwner()
			signal.RunWithOwner(s.owner, func() {
				signal.OnCleanup(func() {
					if r.snacks[win] == s {
						delete(r.snacks, win)
					}
					e.destroy()
				})
				if p.Action != nil && p.Action.Title != nil {
					if t, ok := signal.StaticValue(p.Action.Title); ok {
						if t != "" {
							b.SetString(e.id, ops.PropSnackbarAction, t)
						}
						return
					}
					sent := ""
					signal.RenderEffect(func() {
						if t := p.Action.Title.Get(); t != sent && !e.dead {
							sent = t
							r.batch.SetString(e.id, ops.PropSnackbarAction, t)
						}
					})
				}
			})
		})
		h := e.handlers()
		h.action = func(int64) {
			s.close()
			if p.Action != nil && p.Action.OnPress != nil {
				p.Action.OnPress()
			}
		}
		h.dismissed = s.dismiss
		e.listen(ops.EventAction)
		e.listen(ops.EventDismissed)
		b.Command(e.id, ops.CmdPresent, nil)
		return Node{}
	})
	if r.snacks == nil {
		r.snacks = map[*element]*snackbar{}
	}
	r.snacks[win] = s
	return s.dismiss
}

// close destroys it (the host dismisses it, animated), if it's showing.
func (s *snackbar) close() bool {
	if s.e.dead {
		return false
	}
	s.owner.Dispose()
	return true
}

// dismiss closes it and runs its OnDismiss, if it was showing.
func (s *snackbar) dismiss() {
	if s.close() && s.p.OnDismiss != nil {
		s.p.OnDismiss()
	}
}

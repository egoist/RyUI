package fake

import (
	"fmt"

	"github.com/egoist/asuka/ops"
)

// Windows, as a desktop host keeps them: the main window (the root's) and
// each Window's, with their state, which WindowAction commands change and
// the host reports (WindowState, posted: run it with RunPosted) while the
// core listens. The main window opens at the environment's size.

// WindowInfo is a window's options and state, as the fake host keeps
// them.
type WindowInfo struct {
	Config ops.WindowConfig
	State  ops.WindowState
	// Closed: the main window closed (the app would quit), or a Window
	// was told to close itself (WindowAction Close, which the core never
	// sends a Window).
	Closed bool
	// Actions are the WindowAction commands it got, in order.
	Actions []ops.WindowAction
	// Fits are the sizes WindowAction Fit gave it, in order (a window
	// sized to its content).
	Fits [][2]float32
	// Shown counts the times it was shown (opened shown, Show, Focus).
	Shown int
}

// Flyout returns a flyout Window's anchor and placement, as its Flyout
// prop says; ok is false for a window that's no flyout.
func (h *Host) Flyout(v *View) (f ops.Flyout, ok bool) {
	p, has := v.Props[ops.PropWindowFlyout]
	if !has || v.Kind != ops.KindWindow {
		return ops.Flyout{}, false
	}
	return ops.DecodeFlyout(p.Bytes)
}

// DismissFlyout closes a flyout or a panel as a desktop host does as it
// loses the keyboard (a click elsewhere) or on an Escape its content
// leaves: as the user closing it.
func (h *Host) DismissFlyout(v *View) {
	w := h.wins[v.ID]
	if w == nil || w.Config.Flags&(ops.WindowFlyout|ops.WindowPanel) == 0 {
		panic("fake: DismissFlyout on a window that's no flyout or panel")
	}
	h.CloseWindow(v)
}

// Window returns v's window (v the root, or a Window); nil if it has
// none yet.
func (h *Host) Window(v *View) *WindowInfo { return h.wins[v.ID] }

func (h *Host) window(v *View) *WindowInfo {
	if h.wins == nil {
		h.wins = map[ops.NodeID]*WindowInfo{}
	}
	w := h.wins[v.ID]
	if w == nil {
		w = &WindowInfo{State: ops.WindowState{Width: h.Env.Width, Height: h.Env.Height, Flags: ops.WindowVisible | ops.WindowFocused}}
		h.wins[v.ID] = w
	}
	return w
}

// windowCommand applies a window's command: its options, Present,
// WindowAction.
func (h *Host) windowCommand(v *View, c ops.CommandID, args []byte) {
	switch c {
	case ops.CmdWindowOptions:
		if v.Kind != ops.KindView {
			panic("fake: WindowOptions not on the root")
		}
		w := h.window(v)
		w.Config = ops.DecodeWindowConfig(args)
		h.configure(w)
	case ops.CmdPresent:
		if v.Kind != ops.KindWindow {
			return
		}
		w := h.window(v)
		w.Config = ops.DecodeWindowConfig(v.Props[ops.PropWindowConfig].Bytes)
		w.State = ops.WindowState{Width: 800, Height: 600, Flags: ops.WindowVisible | ops.WindowFocused}
		h.configure(w)
		if w.State.Flags&ops.WindowVisible != 0 {
			w.Shown++
		}
		if w.State.Flags&ops.WindowFocused != 0 {
			h.key = v.ID
		}
		h.reportWindow(v)
	case ops.CmdWindowAction:
		if v.Kind != ops.KindWindow && v.Kind != ops.KindView {
			panic(fmt.Sprintf("fake: WindowAction on a %s", v.Kind))
		}
		a, x, y, ok := ops.DecodeWindowAction(args)
		if !ok {
			panic("fake: WindowAction: bad args")
		}
		w := h.window(v)
		w.Actions = append(w.Actions, a)
		s := &w.State
		switch a {
		case ops.WindowActionShow:
			s.Flags |= ops.WindowVisible
			w.Shown++
		case ops.WindowActionHide:
			s.Flags &^= ops.WindowVisible | ops.WindowFocused
		case ops.WindowActionFocus:
			s.Flags |= ops.WindowVisible | ops.WindowFocused
			w.Shown++
			s.Flags &^= ops.WindowMinimizedState
			if v.Kind == ops.KindWindow {
				h.key = v.ID
			} else {
				h.key = 0
			}
		case ops.WindowActionMinimize:
			s.Flags |= ops.WindowMinimizedState
			s.Flags &^= ops.WindowFocused
		case ops.WindowActionMaximize:
			s.Flags |= ops.WindowMaximizedState
		case ops.WindowActionRestore:
			s.Flags &^= ops.WindowMinimizedState | ops.WindowMaximizedState
		case ops.WindowActionFullscreen:
			s.Flags |= ops.WindowFullscreenState
		case ops.WindowActionExitFullscreen:
			s.Flags &^= ops.WindowFullscreenState
		case ops.WindowActionAlwaysOnTop:
			s.Flags |= ops.WindowOnTop
		case ops.WindowActionNotAlwaysOnTop:
			s.Flags &^= ops.WindowOnTop
		case ops.WindowActionSize:
			s.Width, s.Height = x, y
		case ops.WindowActionPosition:
			s.X, s.Y = x, y
		case ops.WindowActionFit:
			s.Width, s.Height = x, y
			w.Fits = append(w.Fits, [2]float32{x, y})
		case ops.WindowActionClickThrough:
			s.Flags |= ops.WindowClickThroughState
		case ops.WindowActionNotClickThrough:
			s.Flags &^= ops.WindowClickThroughState
		case ops.WindowActionClose:
			w.Closed = true
			s.Flags &^= ops.WindowVisible | ops.WindowFocused
		}
		h.reportWindow(v)
	}
}

func (h *Host) configure(w *WindowInfo) {
	c := &w.Config
	s := &w.State
	if c.Width > 0 {
		s.Width = c.Width
	}
	if c.Height > 0 {
		s.Height = c.Height
	}
	if c.Flags&ops.WindowPositioned != 0 {
		s.X, s.Y = c.X, c.Y
	}
	for _, f := range [...]struct {
		c ops.WindowFlags
		s ops.WindowStateFlags
	}{{ops.WindowMaximized, ops.WindowMaximizedState}, {ops.WindowFullscreen, ops.WindowFullscreenState}, {ops.WindowAlwaysOnTop, ops.WindowOnTop},
		{ops.WindowClickThrough, ops.WindowClickThroughState}} {
		if c.Flags&f.c != 0 {
			s.Flags |= f.s
		}
	}
	if c.Flags&ops.WindowHidden != 0 {
		s.Flags &^= ops.WindowVisible | ops.WindowFocused
	}
}

// reportWindow posts v's window's state, if the core listens for it.
func (h *Host) reportWindow(v *View) {
	if !v.Listens[ops.EventWindowState] {
		return
	}
	w := h.wins[v.ID]
	if w == nil {
		if v.Kind != ops.KindView {
			return // a Window not presented yet
		}
		w = h.window(v)
	}
	id, s := v.ID, w.State
	h.Post(func() {
		if h.Views[id] != nil {
			h.dispatch(ops.Event{ID: id, Kind: ops.EventWindowState, Bytes: ops.AppendWindowState(nil, s)})
		}
	})
}

// SetWindowState changes v's window's state as the user does (moving,
// resizing, minimizing it, focusing another), and reports it at once if
// the core listens.
func (h *Host) SetWindowState(v *View, s ops.WindowState) {
	h.window(v).State = s
	if v.Listens[ops.EventWindowState] {
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventWindowState, Bytes: ops.AppendWindowState(nil, s)})
	}
}

// KeyWindow is the window in use: the Window presented or focused last
// that's still open, else the root (the main window).
func (h *Host) KeyWindow() *View {
	if v := h.Views[h.key]; v != nil && v.Kind == ops.KindWindow {
		return v
	}
	return h.Root()
}

// SetKeyWindow makes v's window the one in use (v a Window, or the root),
// as the user clicking it does.
func (h *Host) SetKeyWindow(v *View) {
	h.key = 0
	if v.Kind == ops.KindWindow {
		h.key = v.ID
	}
}

// CloseWindow closes v's window as the user does (its close button): a
// window the core asks about (it listens for CloseRequest) is asked and
// stays; otherwise a Window is dismissed (Dismissed) and the main window
// closes (Closed: the app would quit).
func (h *Host) CloseWindow(v *View) {
	if v.Listens[ops.EventCloseRequest] {
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventCloseRequest})
		return
	}
	if v.Kind == ops.KindView {
		h.window(v).Closed = true
		return
	}
	h.listening(v, ops.EventDismissed)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventDismissed})
}

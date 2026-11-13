//go:build windows

package windows

import (
	"errors"
	"syscall"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/native"
)

// More of the host's modules, each file its own, as host/darwin's are:
// a file adds its modules from its init (moduleHooks) and its native
// views (builtinViews: asuka/sdk/camera's, asuka/sdk/maps'). Their windows (the
// services' message window, the camera and sign-in dialogs, the camera's
// preview) are of a class of their own, whose messages go to the
// function each registers (ownWindows), apart from the host's views.

// moduleHooks add modules: m's name is the module's.
var moduleHooks []func(h *Host, m map[string]map[string]asyncMethod)

// builtinViews are the host's own native views (ui.Native kinds Asuka's
// packages make), by kind; an app's of the same kind (package native)
// comes second.
var builtinViews = map[string]func(native.Context) native.View{}

// moreModule is a module the hooks add, by name.
func (h *Host) moreModule(name string) map[string]asyncMethod {
	if h.more == nil {
		h.more = map[string]map[string]asyncMethod{}
		for _, f := range moduleHooks {
			f(h, h.more)
		}
	}
	return h.more[name]
}

const classOwn = "AsukaOwn" // the windows of more.go's modules

var (
	ownProc    uintptr
	ownWindows = map[uintptr]func(m uint32, wp, lp uintptr) (uintptr, bool){}
	ownMsgWin  uintptr // the modules' message window
	ownTimers  = map[uintptr]func(){}
	ownNextID  uintptr
)

// ownWindow makes a window of the modules' class, whose messages go to
// handle (unhandled: DefWindowProc's).
func ownWindow(ex uint32, title string, style uint32, x, y, w, ht int32, parent uintptr, handle func(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool)) uintptr {
	if ownProc == 0 {
		ownProc = syscall.NewCallback(func(hwnd, m, wp, lp uintptr) uintptr {
			if f := ownWindows[hwnd]; f != nil {
				if r, ok := f(uint32(m), wp, lp); ok {
					return r
				}
			}
			if m == WM_NCDESTROY {
				delete(ownWindows, hwnd)
			}
			return DefWindowProc(hwnd, uint32(m), wp, lp)
		})
		RegisterClass(classOwn, CS_HREDRAW|CS_VREDRAW, ownProc, AppIcon())
	}
	hwnd := CreateWindow(ex, classOwn, title, style, x, y, w, ht, parent, 0)
	if hwnd != 0 && handle != nil {
		ownWindows[hwnd] = func(m uint32, wp, lp uintptr) (uintptr, bool) { return handle(hwnd, m, wp, lp) }
	}
	return hwnd
}

// ownTimer calls f every ms milliseconds, on the main thread, until the
// returned function is called.
func ownTimer(ms uint32, f func()) (stop func()) {
	if ownMsgWin == 0 {
		ownMsgWin = ownWindow(0, "", 0, 0, 0, 0, 0, ^uintptr(2), func(_ uintptr, m uint32, wp, _ uintptr) (uintptr, bool) { // HWND_MESSAGE
			if m == WM_TIMER {
				if f := ownTimers[wp]; f != nil {
					f()
				}
				return 0, true
			}
			return 0, false
		})
	}
	ownNextID++
	id := ownNextID
	ownTimers[id] = f
	SetTimer(ownMsgWin, id, ms)
	return func() {
		if ownTimers[id] != nil {
			KillTimer(ownMsgWin, id)
			delete(ownTimers, id)
		}
	}
}

// offMain runs f on a goroutine and answers with what it returns, on
// the main thread.
func (h *Host) offMain(f func() ([]byte, error), reply func([]byte, error)) {
	go func() {
		b, err := f()
		h.Post(func() { reply(b, err) })
	}()
}

// answerNil answers at once, with nothing.
func answerNil(_ []byte, reply func([]byte, error)) { reply(nil, nil) }

// failing is a method that fails with msg.
func failing(msg string) asyncMethod {
	err := errors.New(msg)
	return func(_ []byte, reply func([]byte, error)) { reply(nil, err) }
}

var errNotAllowed = errors.New("not allowed")

// dialog is a window of the modules' own over the window in use, which
// it disables until it closes (as a sheet does): the camera's, the web
// sign-in's.
type dialog struct {
	hwnd, owner uintptr
	k           float32 // pixels per point
	closed      bool
	onClose     func() // the user closed it (its close box, Alt+F4)
}

// newDialog makes a dialog of w by ht points' client area, centered over
// the window in use, whose messages go to handle first; show shows it.
func (h *Host) newDialog(title string, w, ht float32, handle func(d *dialog, m uint32, wp, lp uintptr) (uintptr, bool)) *dialog {
	d := &dialog{owner: h.inUseWindow()}
	dpi := DPI(d.owner)
	style := uint32(WS_POPUP | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_CLIPCHILDREN)
	r := frameRect(w, ht, style, dpi)
	wr := WindowRect(d.owner)
	x, y := wr.Left+(wr.W()-r.W())/2, wr.Top+(wr.H()-r.H())/2
	if d.owner == 0 {
		x, y = 100, 100
	}
	d.hwnd = ownWindow(h.topLevelStyle(), title, style, x, y, r.W(), r.H(), d.owner, func(_ uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
		if handle != nil {
			if r, ok := handle(d, m, wp, lp); ok {
				return r, true
			}
		}
		if m == WM_CLOSE {
			if d.onClose != nil {
				d.onClose()
			} else {
				d.close()
			}
			return 0, true
		}
		return 0, false
	})
	d.k = dpiScale(DPI(d.hwnd))
	SetDarkTitleBar(d.hwnd, h.dark)
	return d
}

func (d *dialog) show() {
	if d.owner != 0 {
		EnableWindow(d.owner, false)
	}
	ShowWindow(d.hwnd, SW_SHOW)
	SetForegroundWindow(d.hwnd)
}

// close closes the dialog, giving the owner back its input first (so
// Windows activates it, not another app).
func (d *dialog) close() {
	if d.closed {
		return
	}
	d.closed = true
	if d.owner != 0 {
		EnableWindow(d.owner, true)
		SetForegroundWindow(d.owner)
	}
	DestroyWindow(d.hwnd)
}

// px is points in the dialog's pixels.
func (d *dialog) px(pt float32) int32 { return int32(pt*d.k + 0.5) }

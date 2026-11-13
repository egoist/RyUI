// Package hotkey gives the app global shortcuts: keys that reach it
// while another app is in front, to bring up a launcher's panel, start
// and stop a recording, paste a snippet (Electron's globalShortcut).
//
//	k := hotkey.Register(hotkey.Props{
//		Shortcut:    ui.Val(ui.Cmd("space").With(ui.Shift)),
//		Description: locales.Live.ShowLauncher,
//		OnPress:     func() { open.Set(!open.Peek()) },
//	})
//	ui.Show(func() bool { return k.Err() != nil }, func() ui.Node {
//		return ui.Text(locales.Live.ShortcutTaken)
//	})
//
// A shortcut is registered while the component that registers it lives
// (its owner), and unregistered as it's disposed; OnPress runs on the
// main thread each time it's pressed, whichever app has the keyboard.
// The shortcut follows its accessor: set it to the keys the user picks
// in the app's settings and the old keys are let go as the new ones are
// taken.
//
// Each platform's own way, needing no permission:
//
//   - macOS: Carbon's hot keys (RegisterEventHotKey), as every Mac app's
//     global shortcut is; no Accessibility permission. The system tells
//     when another app has the same keys only for some (its own, the
//     ones registered as exclusive): two apps may both get a press.
//   - Windows: RegisterHotKey, which fails while another app has the
//     keys. The press lets the app take the foreground.
//   - Linux: under Wayland, apps can't see keys pressed in other apps:
//     the desktop binds the shortcut for the app through the XDG desktop
//     portal's GlobalShortcuts (GNOME 48, KDE Plasma 6, Hyprland). The
//     desktop may ask the user to confirm it or pick other keys, lists it
//     with its Description in its settings, and needs the app installed
//     (it knows apps by their desktop entry, asuka.json's id); a window
//     OnPress shows or focuses takes the focus with the press's
//     activation token, as Wayland asks. Under X11
//     the app grabs the keys itself (XGrabKey), which fails while
//     another app has them. A Wayland desktop without the portal (GNOME
//     before 48): ErrNotAvailable.
//   - Phones and the web have none: ErrNotAvailable. A page gets the
//     keyboard only while it has the focus.
//
// Primary is ⌘ on a Mac and Ctrl elsewhere; Control is the control key
// (on Windows and Linux, as Primary). Only the press is reported: not
// every platform tells when the keys are let go (Windows doesn't), and a
// key held down doesn't repeat it.
package hotkey

import (
	"encoding/binary"
	"errors"
	"runtime"
	"strings"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// The errors Err reports.
var (
	// ErrTaken: another app has the keys (or this one does, from another
	// Register).
	ErrTaken = errors.New("hotkey: the shortcut is taken")
	// ErrNotAllowed: the user declined it (Linux's portal asks), or the
	// desktop won't bind shortcuts for the app (not installed: no desktop
	// entry).
	ErrNotAllowed = errors.New("hotkey: not allowed")
	// ErrNotAvailable: the platform has no global shortcuts (phones, the
	// web, a Wayland desktop without the portal).
	ErrNotAvailable = errors.New("hotkey: not available")
	// ErrKey: the keyboard has no such key, or the platform can't take it
	// in a global shortcut.
	ErrKey = errors.New("hotkey: no such key")
)

// Props configures a global shortcut.
type Props struct {
	// Shortcut is the keys. A Shortcut without a Key registers nothing
	// (a setting the user cleared).
	Shortcut signal.Accessor[ui.Shortcut]
	// Description says what it does, where the desktop lists the app's
	// shortcuts and asks the user to confirm them (Linux's portal:
	// GNOME's and KDE's settings): "Show the launcher". Optional.
	Description signal.Accessor[string]
	// OnPress runs on the main thread each time the shortcut is pressed.
	OnPress func()
}

// Status is where a registration stands.
type Status uint8

const (
	// Off: nothing registered (no Key, or Unregister).
	Off Status = iota
	// Pending: asked for, not answered yet (Linux's portal may be asking
	// the user).
	Pending
	// Active: registered; presses reach OnPress.
	Active
	// Failed: not registered; Err says why.
	Failed
)

func (s Status) String() string {
	return [...]string{"off", "pending", "active", "failed", "?"}[min(int(s), 4)]
}

// Hotkey is a registered global shortcut.
type Hotkey struct {
	props  Props
	owner  *signal.Owner
	id     uint32 // the host's registration, 0 while none
	keys   ui.Shortcut
	status *signal.Signal[Status]
	err    *signal.Signal[error]
}

// Register registers a global shortcut for as long as the current owner
// (the component calling it) lives. Main thread.
func Register(p Props) *Hotkey {
	k := &Hotkey{props: p, status: signal.New(Off), err: signal.New[error](nil)}
	k.owner = signal.NewOwner()
	signal.RunWithOwner(k.owner, func() {
		signal.Effect(func() {
			var s ui.Shortcut
			if p.Shortcut != nil {
				s = p.Shortcut.Get()
			}
			desc := ""
			if p.Description != nil {
				desc = p.Description.Get()
			}
			k.register(s, desc)
			signal.OnCleanup(k.release)
		})
	})
	return k
}

// Status is where the registration stands. Reading it tracks it.
func (k *Hotkey) Status() Status { return k.status.Get() }

// Err is why the shortcut isn't registered (ErrTaken, ErrNotAllowed,
// ErrNotAvailable, ErrKey, or the platform's error), nil while it is or
// is pending. Reading it tracks it.
func (k *Hotkey) Err() error { return k.err.Get() }

// Unregister lets the keys go before the owner is disposed. Main thread.
func (k *Hotkey) Unregister() { k.owner.Dispose() }

// The registrations: one long poll ("next", answered with the presses
// since the last) runs while any is active. Stopping it ends its
// generation: its last answer ("stopped") is ignored, and a poll starts
// afresh with the next registration (on whichever root is mounted then).
var (
	byID    = map[uint32]*Hotkey{}
	nextID  uint32
	polling bool
	pollGen int
)

func (k *Hotkey) register(s ui.Shortcut, desc string) {
	if s.Key == "" {
		k.set(Off, nil)
		return
	}
	s = normal(s)
	for _, o := range byID {
		if o.keys == s {
			k.set(Failed, ErrTaken)
			return
		}
	}
	nextID++
	id := nextID
	k.id, k.keys = id, s
	byID[id] = k
	k.set(Pending, nil)
	le := binary.LittleEndian
	b := le.AppendUint32(nil, id)
	b = append(b, byte(s.Mods))
	b = le.AppendUint32(b, uint32(len(s.Key)))
	b = append(append(b, s.Key...), desc...)
	ui.Call("hotkey", "register", b, func(_ []byte, err error) {
		if byID[id] != k {
			return // let go meanwhile
		}
		if err != nil {
			delete(byID, id)
			k.id, k.keys = 0, ui.Shortcut{}
			k.set(Failed, hostError(err))
			return
		}
		k.set(Active, nil)
		if !polling {
			polling = true
			poll(pollGen)
		}
	})
}

// release lets the current keys go.
func (k *Hotkey) release() {
	if k.id == 0 {
		k.set(Off, nil)
		return
	}
	id := k.id
	delete(byID, id)
	k.id, k.keys = 0, ui.Shortcut{}
	k.set(Off, nil)
	ui.Call("hotkey", "unregister", binary.LittleEndian.AppendUint32(nil, id), nil)
	if len(byID) == 0 && polling {
		polling = false
		pollGen++
		ui.Call("hotkey", "stop", nil, nil)
	}
}

func (k *Hotkey) set(s Status, err error) {
	signal.Batch(func() {
		k.status.Set(s)
		k.err.Set(err)
	})
}

// poll waits for presses: "next" answers with the ids pressed (u32
// each) since it last did; "stop" fails it with "stopped".
func poll(gen int) {
	ui.Call("hotkey", "next", nil, func(b []byte, err error) {
		if gen != pollGen {
			return // stopped
		}
		if err != nil {
			polling = false
			return
		}
		for ; len(b) >= 4; b = b[4:] {
			if k := byID[binary.LittleEndian.Uint32(b)]; k != nil && k.status.Peek() == Active && k.props.OnPress != nil {
				k.props.OnPress()
			}
		}
		if gen == pollGen {
			poll(gen)
		}
	})
}

// normal is s as the host takes it: its key lowercase, Control as
// Primary where they're one key (not on a Mac).
func normal(s ui.Shortcut) ui.Shortcut {
	s.Key = strings.ToLower(s.Key)
	if runtime.GOOS != "darwin" && s.Mods&ui.Control != 0 {
		s.Mods = s.Mods&^ui.Control | ui.Primary
	}
	return s
}

// hostError maps a host's answer to the package's errors.
func hostError(err error) error {
	msg := err.Error()
	switch {
	case msg == "taken":
		return ErrTaken
	case msg == "not allowed":
		return ErrNotAllowed
	case msg == "no key":
		return ErrKey
	case msg == "not available", strings.HasPrefix(msg, "no module"), strings.Contains(msg, "isn't on the web"):
		return ErrNotAvailable // phones and the web have no module
	}
	return err
}

// Package dock is the app's icon where the desktop keeps its running
// apps: the Dock on a Mac, the taskbar on Windows, the dock or launcher
// on Linux. It shows a task's progress on the icon (a download, an
// export) and asks for the user's attention (a call coming in, a long
// task done while the user is in another app). The icon's count is
// notify.SetBadge.
//
// Phones and the web have no such icon: there these do nothing (a
// phone shows a task's progress in a notification: asuka/background).
// All are main-thread calls.
package dock

import (
	"encoding/binary"
	"math"

	"github.com/egoist/asuka/ui"
)

// State is how a progress shows.
type State uint8

const (
	// None shows no progress.
	None State = iota
	// Normal shows how far the task is, Progress.Value.
	Normal
	// Indeterminate shows the task under way, how far unknown: Windows's
	// marquee, a third of the bar sliding across on a Mac, a full bar on
	// Linux.
	Indeterminate
	// Paused shows the task stopped where it is: yellow on Windows and
	// on a Mac; as Normal on Linux.
	Paused
	// Error shows the task failed where it is: red on Windows and on a
	// Mac; as Normal on Linux.
	Error
)

// Progress is a task's progress on the app's icon.
type Progress struct {
	// State is how it shows; Normal when it's None and Value is above 0.
	State State
	// Value is how far the task is, 0 to 1.
	Value float64
}

// SetProgress shows a task's progress on the app's icon, until it's set
// again; Progress{} takes it off. macOS draws a bar over the bottom of
// the Dock icon (the badge stays over it); Windows shows it on the main
// window's taskbar button (ITaskbarList3); Linux shows it on the launcher
// entry of docks that read Unity's launcher API (Dash to Dock, Ubuntu's
// dock, KDE Plasma's task manager; GNOME's own dash shows none).
func SetProgress(p Progress) {
	state := p.State
	v := p.Value
	if math.IsNaN(v) {
		v = 0
	}
	v = min(max(v, 0), 1)
	if state == None && v > 0 {
		state = Normal
	}
	if state > Error {
		state = Normal
	}
	b := []byte{byte(state)}
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
	ui.Call("dock", "progress", b, nil)
}

// Attention is how much a RequestAttention asks.
type Attention uint8

const (
	// Informational asks once: the Dock icon bounces once (macOS), the
	// taskbar button flashes once and stays lit until the app is used
	// (Windows).
	Informational Attention = iota
	// Critical asks until the user comes to the app: the Dock icon
	// bounces until then (macOS), the taskbar button and the window's
	// title bar flash until then (Windows).
	Critical
)

// RequestAttention asks the user to come to the app while it isn't the
// one in use (it does nothing while it is), until cancel or the user
// comes. Linux: the window is marked urgent where the desktop shows it
// (X11's urgency hint, the taskbar's or dock's highlight); on Wayland it
// asks to be activated, which GNOME and KDE show as a notification that
// the app is ready (a click brings it), and which can't be taken back.
func RequestAttention(a Attention) (cancel func()) {
	var id uint32
	cancelled := false
	ui.Call("dock", "attention", []byte{byte(a)}, func(b []byte, err error) {
		if err == nil && len(b) >= 4 {
			id = binary.LittleEndian.Uint32(b)
		}
		if cancelled && id != 0 {
			ui.Call("dock", "cancelAttention", binary.LittleEndian.AppendUint32(nil, id), nil)
		}
	})
	return func() {
		if cancelled {
			return
		}
		cancelled = true
		if id != 0 {
			ui.Call("dock", "cancelAttention", binary.LittleEndian.AppendUint32(nil, id), nil)
		}
	}
}

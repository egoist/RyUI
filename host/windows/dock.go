//go:build windows

package windows

import (
	"encoding/binary"
	"math"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/dock on Windows: the taskbar button of the app's main window.
// "progress" is ITaskbarList3's progress (normal, indeterminate, paused
// in yellow, error in red), set again when Explorer makes the button
// again (TaskbarButtonCreated: the window showing, Explorer restarting).
// "attention" flashes the button of the window in use (FlashWindowEx):
// once for an informational request, which leaves the button lit until
// the app's used, and with its title bar until the window comes to the
// foreground for a critical one; nothing while the app is in the
// foreground.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["dock"] = h.dockModule()
	})
}

var dockState struct {
	shown     bool   // a progress set
	state     uint32 // its TBPF_ flag
	value     float64
	buttonMsg uint32 // TaskbarButtonCreated
	flashID   uint32 // the last attention request
	flashWin  uintptr
}

// dockStates are asuka/dock's states as TBPF_ flags: none, normal,
// indeterminate, paused, error.
var dockStates = [...]uint32{TBPF_NOPROGRESS, TBPF_NORMAL, TBPF_INDETERMINATE, TBPF_PAUSED, TBPF_ERROR}

func (h *Host) dockModule() map[string]asyncMethod {
	d := &dockState
	if d.buttonMsg == 0 {
		d.buttonMsg = TaskbarButtonCreated()
	}
	return map[string]asyncMethod{
		// args: u8 state, f64 value.
		"progress": func(args []byte, reply func([]byte, error)) {
			state, value := TBPF_NOPROGRESS, 0.0
			if len(args) >= 9 {
				if int(args[0]) < len(dockStates) {
					state = int(dockStates[args[0]])
				}
				value = math.Float64frombits(binary.LittleEndian.Uint64(args[1:]))
			}
			d.state, d.value, d.shown = uint32(state), value, state != TBPF_NOPROGRESS
			// The button may not be there yet (the window not shown): set
			// again as it's made.
			if h.window != 0 {
				SetTaskbarProgress(h.window, d.state, d.value)
			}
			reply(nil, nil)
		},
		// args: u8 critical; result: u32 the request's ID, 0 if none
		// was made.
		"attention": func(args []byte, reply func([]byte, error)) {
			w := h.inUseWindow()
			if w == 0 || ForegroundIsOurs() {
				reply(binary.LittleEndian.AppendUint32(nil, 0), nil)
				return
			}
			if len(args) > 0 && args[0] == 1 {
				FlashWindow(w, FLASHW_ALL|FLASHW_TIMERNOFG, 0)
			} else {
				FlashWindow(w, FLASHW_TRAY, 1)
			}
			d.flashID++
			d.flashWin = w
			reply(binary.LittleEndian.AppendUint32(nil, d.flashID), nil)
		},
		// args: u32 the request's ID.
		"cancelAttention": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 && binary.LittleEndian.Uint32(args) == d.flashID && d.flashWin != 0 {
				FlashWindow(d.flashWin, FLASHW_STOP, 0)
				d.flashWin = 0
			}
			reply(nil, nil)
		},
	}
}

// dockMessage sets the progress again on a new taskbar button.
func (h *Host) dockMessage(m uint32) {
	if d := &dockState; d.shown && d.buttonMsg != 0 && m == d.buttonMsg {
		SetTaskbarProgress(h.window, d.state, d.value)
	}
}

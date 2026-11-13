//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"fmt"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/hotkey on Windows: RegisterHotKey with a message window of the
// modules' (HWND_MESSAGE), which gets WM_HOTKEY for a press whichever
// app has the keyboard. It fails while another app has the keys
// ("taken"); MOD_NOREPEAT keeps a key held down from pressing it again.
// The press gives the app the right to take the foreground, so OnPress
// can bring its window up. Primary and Control are both Ctrl; a
// character is the key that types it on the keyboard's layout (with
// Shift if it takes it), as the menu bar's accelerators are.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["hotkey"] = h.hotkeyModule() })
}

// hotkeys are the registrations: the package's ids by the ids Windows
// has them by (an app's are 0 to 0xBFFF), and the presses "next" hasn't
// answered with yet.
var hotkeys struct {
	win     uintptr
	ids     map[int]uint32
	nextWin int
	pressed []byte
	waiting func([]byte, error)
}

// hotkeyWindow is the window hot keys post WM_HOTKEY to.
func hotkeyWindow() uintptr {
	if hotkeys.win == 0 {
		hotkeys.win = ownWindow(0, "", 0, 0, 0, 0, 0, ^uintptr(2), func(_ uintptr, m uint32, wp, _ uintptr) (uintptr, bool) { // HWND_MESSAGE
			if m != WM_HOTKEY {
				return 0, false
			}
			if id, ok := hotkeys.ids[int(wp)]; ok {
				hotkeys.pressed = binary.LittleEndian.AppendUint32(hotkeys.pressed, id)
				flushHotkeys()
			}
			return 0, true
		})
		hotkeys.ids = map[int]uint32{}
	}
	return hotkeys.win
}

// hotkeyModule: "register" (u32 id, u8 mods, u32 key length, the key,
// the description, which Windows doesn't show), "unregister" (u32 id),
// "next" (answered with the ids pressed since, u32 each, once there are
// some) and "stop" (fails the waiting "next" with "stopped").
func (h *Host) hotkeyModule() map[string]asyncMethod {
	le := binary.LittleEndian
	return map[string]asyncMethod{
		"register": func(args []byte, reply func([]byte, error)) {
			if len(args) < 9 || int(le.Uint32(args[5:])) > len(args)-9 {
				reply(nil, errors.New("hotkey.register: bad arguments"))
				return
			}
			id, mods := le.Uint32(args), args[4]
			key := string(args[9 : 9+le.Uint32(args[5:])])
			vk, wmods, ok := hotKey(key, mods)
			if !ok {
				reply(nil, errors.New("no key"))
				return
			}
			w := hotkeyWindow()
			win, ok := freeHotkeyID()
			if !ok {
				reply(nil, errors.New("hotkey: too many registered"))
				return
			}
			if err := RegisterHotKey(w, win, wmods|MOD_NOREPEAT, vk); err != nil {
				if errors.Is(err, ERROR_HOTKEY_ALREADY_REGISTERED) {
					reply(nil, errors.New("taken"))
				} else {
					reply(nil, fmt.Errorf("hotkey: RegisterHotKey: %v", err))
				}
				return
			}
			hotkeys.ids[win] = id
			reply(nil, nil)
		},
		"unregister": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				id := le.Uint32(args)
				for win, i := range hotkeys.ids {
					if i == id {
						UnregisterHotKey(hotkeys.win, win)
						delete(hotkeys.ids, win)
					}
				}
			}
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if w := hotkeys.waiting; w != nil {
				w(nil, errors.New("stopped")) // one poll at a time
			}
			hotkeys.waiting = reply
			flushHotkeys()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			w := hotkeys.waiting
			hotkeys.waiting, hotkeys.pressed = nil, nil
			if w != nil {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// freeHotkeyID is an id Windows has no hot key of the app's by.
func freeHotkeyID() (int, bool) {
	for range 0xC000 {
		hotkeys.nextWin = (hotkeys.nextWin + 1) % 0xC000
		if _, used := hotkeys.ids[hotkeys.nextWin]; !used {
			return hotkeys.nextWin, true
		}
	}
	return 0, false
}

// flushHotkeys answers the waiting "next" with the presses.
func flushHotkeys() {
	if hotkeys.waiting == nil || len(hotkeys.pressed) == 0 {
		return
	}
	w, b := hotkeys.waiting, hotkeys.pressed
	hotkeys.waiting, hotkeys.pressed = nil, nil
	w(b, nil)
}

// hotKey is a shortcut's virtual key and MOD_ flags: the menu bar's
// accelerator for it (accelerator, in menubar.go), whose modifiers are
// the accelerator table's.
func hotKey(key string, mods byte) (vk uint16, wmods uint32, ok bool) {
	a, ok := accelerator(key, mods)
	if !ok || a.Key == 0 {
		return 0, 0, false
	}
	if a.Virt&FCONTROL != 0 {
		wmods |= MOD_CONTROL
	}
	if a.Virt&FSHIFT != 0 {
		wmods |= MOD_SHIFT
	}
	if a.Virt&FALT != 0 {
		wmods |= MOD_ALT
	}
	return a.Key, wmods, true
}

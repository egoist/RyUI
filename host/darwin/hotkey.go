//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/hotkey on a Mac: Carbon's hot keys (RegisterEventHotKey), which
// need no Accessibility permission, registered exclusively: another
// app's exclusive registration of the same keys fails one ("taken"), as
// do the system's own shortcuts (Spotlight's ⌘Space, Mission Control's),
// which CopySymbolicHotKeys lists. Presses come through the application's
// event target as AppKit's event loop runs. A hot key is the process's,
// not a window's: the registrations are the package's.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["hotkey"] = h.hotkeyModule() })
}

// hotkeys are the registrations, by the package's ids, and the presses
// "next" hasn't answered with yet.
var hotkeys struct {
	refs    map[uint32]uintptr
	pressed []byte
	waiting func([]byte, error)
}

// hotkeyModule: "register" (u32 id, u8 mods, u32 key length, the key,
// the description, which a Mac doesn't show), "unregister" (u32 id),
// "next" (answered with the ids pressed since, u32 each, once there are
// some) and "stop" (fails the waiting "next" with "stopped").
func (h *Host) hotkeyModule() map[string]method {
	le := binary.LittleEndian
	return map[string]method{
		"register": func(args []byte, reply func([]byte, error)) {
			if len(args) < 9 || int(le.Uint32(args[5:])) > len(args)-9 {
				reply(nil, errors.New("hotkey.register: bad arguments"))
				return
			}
			id, mods := le.Uint32(args), args[4]
			key := string(args[9 : 9+le.Uint32(args[5:])])
			code, cmods, ok := carbonHotKey(key, mods)
			if !ok {
				reply(nil, errors.New("no key"))
				return
			}
			if SystemHotKey(code, cmods) {
				reply(nil, errors.New("taken"))
				return
			}
			if err := OnHotKey(hotkeyPressed); err != nil {
				reply(nil, errors.New("not available"))
				return
			}
			ref, st := RegisterHotKey(code, cmods, id)
			switch {
			case st == EventHotKeyExistsErr:
				reply(nil, errors.New("taken"))
				return
			case st != 0:
				reply(nil, fmt.Errorf("hotkey: RegisterEventHotKey failed (%d)", st))
				return
			}
			if hotkeys.refs == nil {
				hotkeys.refs = map[uint32]uintptr{}
			}
			hotkeys.refs[id] = ref
			reply(nil, nil)
		},
		"unregister": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				id := le.Uint32(args)
				UnregisterHotKey(hotkeys.refs[id])
				delete(hotkeys.refs, id)
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

// hotkeyPressed is Carbon's hot key event for id.
func hotkeyPressed(id uint32) {
	if _, ok := hotkeys.refs[id]; !ok {
		return
	}
	hotkeys.pressed = binary.LittleEndian.AppendUint32(hotkeys.pressed, id)
	flushHotkeys()
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

// carbonKeys are the named keys' key codes (Events.h's kVK_), the same on
// every layout.
var carbonKeys = map[string]uint16{
	"enter": 0x24, "tab": 0x30, "space": 0x31, "backspace": 0x33, "escape": 0x35,
	"delete": 0x75, "home": 0x73, "end": 0x77, "pageup": 0x74, "pagedown": 0x79,
	"left": 0x7B, "right": 0x7C, "down": 0x7D, "up": 0x7E,
	"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60, "f6": 0x61, "f7": 0x62, "f8": 0x64,
	"f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F, "f13": 0x69, "f14": 0x6B, "f15": 0x71,
	"f16": 0x6A, "f17": 0x40, "f18": 0x4F, "f19": 0x50, "f20": 0x5A,
}

// carbonHotKey is a shortcut's key code and Carbon modifiers: a named
// key's, or the key that types the character on the keyboard's layout
// (with Shift if it takes it, as + on a US keyboard). Primary is ⌘.
func carbonHotKey(key string, mods byte) (code, cmods uint32, ok bool) {
	k := strings.ToLower(key)
	if c, named := carbonKeys[k]; named {
		code, ok = uint32(c), true
	} else if rs := []rune(k); len(rs) == 1 {
		c, shift, found := KeyCodeFor(rs[0])
		if !found {
			return 0, 0, false
		}
		code, ok = uint32(c), true
		if shift {
			cmods |= CarbonShift
		}
	}
	if !ok {
		return 0, 0, false
	}
	if mods&1 != 0 {
		cmods |= CarbonCmd
	}
	if mods&2 != 0 {
		cmods |= CarbonShift
	}
	if mods&4 != 0 {
		cmods |= CarbonOption
	}
	if mods&8 != 0 {
		cmods |= CarbonControl
	}
	return code, cmods, true
}

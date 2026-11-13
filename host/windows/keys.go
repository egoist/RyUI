package windows

import (
	"unicode"

	"github.com/egoist/asuka/ops"
)

// A hardware keyboard's keys for the views that declare them (the Keys
// prop: DESIGN.md's Keys), the parts apart from Win32: a virtual key's
// name, its modifiers, and the walk out from the focused view to the
// nearest view that takes it (they build anywhere, so they're tested
// anywhere). keys_windows.go routes the message loop's keys with them.

// The modifiers of a shortcut: MenuItem's Mods, the Keys prop's.
const (
	modPrimary = 1
	modShift   = 2
	modAlt     = 4
	modControl = 8
)

// The virtual keys (winuser.h) a key's name comes from.
const (
	vkBack     = 0x08
	vkTab      = 0x09
	vkReturn   = 0x0D
	vkEscape   = 0x1B
	vkSpace    = 0x20
	vkPrior    = 0x21
	vkNext     = 0x22
	vkEnd      = 0x23
	vkHome     = 0x24
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkDelete   = 0x2E
	vk0        = 0x30
	vk9        = 0x39
	vkA        = 0x41
	vkZ        = 0x5A
	vkNumpad0  = 0x60
	vkNumpad9  = 0x69
	vkF1       = 0x70
	vkF12      = 0x7B
	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12 // Alt
	vkLWin     = 0x5B
	vkRWin     = 0x5C
	vkProcess  = 0xE5 // a key an input method took (VK_PROCESSKEY)
	vkDeadMask = 0x80000000
)

// namedVKs are the named keys of the Keys prop by virtual key.
var namedVKs = map[uint32]string{
	vkReturn: "enter", vkEscape: "escape", vkTab: "tab", vkSpace: "space",
	vkDelete: "delete", vkBack: "backspace",
	vkUp: "up", vkDown: "down", vkLeft: "left", vkRight: "right",
	vkHome: "home", vkEnd: "end", vkPrior: "pageup", vkNext: "pagedown",
}

// fNames are f1 to f12.
var fNames = [...]string{"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12"}

// asciiNames are the names of the keys that type a character below 128:
// one string each, made once.
var asciiNames = func() (t [128]string) {
	for c := range t {
		t[c] = string(rune(c))
	}
	return t
}()

// keyName is the Keys prop's name of the virtual key vk going down: a
// named key's (enter, up, f5...), else the character it types without
// modifiers, lower case. Letters and digits are their virtual keys' (as
// the menu bar's accelerators have them, so Ctrl+D is the D key on any
// layout), the numeric keypad's digits the digits; any other key's
// character is char's (the keyboard layout's: MapVirtualKeyEx's
// MAPVK_VK_TO_CHAR, a dead key's with its high bit set). "": a key of no
// name (a modifier, a key typing nothing).
func keyName(vk uint32, char func(vk uint32) uint32) string {
	if s, ok := namedVKs[vk]; ok {
		return s
	}
	switch {
	case vk >= vkF1 && vk <= vkF12:
		return fNames[vk-vkF1]
	case vk >= vkA && vk <= vkZ:
		return asciiNames[vk-vkA+'a']
	case vk >= vk0 && vk <= vk9:
		return asciiNames[vk]
	case vk >= vkNumpad0 && vk <= vkNumpad9:
		return asciiNames[vk-vkNumpad0+'0']
	case vk == vkShift || vk == vkControl || vk == vkMenu || vk == vkLWin || vk == vkRWin || vk == vkProcess:
		return ""
	}
	if char == nil {
		return ""
	}
	r := rune(char(vk) &^ vkDeadMask)
	if r <= ' ' || r == 0x7F || !unicode.IsPrint(r) {
		return ""
	}
	r = unicode.ToLower(r)
	if r < 128 {
		return asciiNames[r]
	}
	return string(r)
}

// keyMods are the Keys prop's modifiers of the keys held: Ctrl is
// primary (Windows has no other control key: a declared Control is Ctrl
// too, as the menu bar has it; keysOf folds it into primary).
func keyMods(ctrl, shift, alt bool) uint8 {
	var m uint8
	if ctrl {
		m |= modPrimary
	}
	if shift {
		m |= modShift
	}
	if alt {
		m |= modAlt
	}
	return m
}

// keysOf is a Keys prop as the host matches it: its keys, Control as
// primary (both are Ctrl here, as in the menu bar's accelerators).
func keysOf(b []byte) []ops.Key {
	keys := ops.DecodeKeys(b)
	for i := range keys {
		if keys[i].Mods&modControl != 0 {
			keys[i].Mods = keys[i].Mods&^modControl | modPrimary
		}
	}
	return keys
}

// keyTaker is the view that takes a key going down (name and mods as
// keyName and keyMods have them) with the keyboard's focus in from: the
// nearest, from it out through parent, whose keys match it; and the
// key's index in its keys. The zero N: none, the key goes on.
func keyTaker[N comparable](from N, parent func(N) N, keys func(N) []ops.Key, name string, mods uint8) (N, int) {
	var none N
	for n := from; n != none; n = parent(n) {
		if i := ops.MatchKey(keys(n), name, mods); i >= 0 {
			return n, i
		}
	}
	return none, -1
}

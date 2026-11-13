package windows

import (
	"testing"

	"github.com/egoist/asuka/ops"
)

// A virtual key's name: named keys, letters and digits by their virtual
// keys, the keypad's digits, the layout's character for the rest (lower
// case, a dead key's too), none for modifiers and keys typing nothing.
func TestKeyName(t *testing.T) {
	// A German layout's characters, as MapVirtualKeyEx gives them.
	layout := map[uint32]uint32{
		0xBA: 'Ü',                        // VK_OEM_1
		0xBC: ',',                        // VK_OEM_COMMA
		0xDC: '^' | vkDeadMask,           // VK_OEM_5: a dead key
		0x6B: '+',                        // VK_ADD
		0x41: 'A', 0x31: '1', 0x0D: 0x0D, // never asked
	}
	asked := 0
	char := func(vk uint32) uint32 { asked++; return layout[vk] }
	for _, c := range []struct {
		vk   uint32
		want string
	}{
		{vkReturn, "enter"}, {vkEscape, "escape"}, {vkTab, "tab"}, {vkSpace, "space"},
		{vkDelete, "delete"}, {vkBack, "backspace"}, {vkUp, "up"}, {vkDown, "down"},
		{vkLeft, "left"}, {vkRight, "right"}, {vkHome, "home"}, {vkEnd, "end"},
		{vkPrior, "pageup"}, {vkNext, "pagedown"}, {vkF1, "f1"}, {0x74, "f5"}, {vkF12, "f12"},
		{'A', "a"}, {'D', "d"}, {'Z', "z"}, {'0', "0"}, {'9', "9"},
		{vkNumpad0, "0"}, {vkNumpad0 + 7, "7"},
		{0xBA, "ü"}, {0xBC, ","}, {0xDC, "^"}, {0x6B, "+"},
		{vkShift, ""}, {vkControl, ""}, {vkMenu, ""}, {vkLWin, ""}, {vkRWin, ""}, {vkProcess, ""},
		{0x14, ""}, // Caps Lock: no character
		{0x7C, ""}, // F13: not a Keys prop's key
	} {
		if got := keyName(c.vk, char); got != c.want {
			t.Errorf("keyName(%#x) = %q, want %q", c.vk, got, c.want)
		}
	}
	if asked != 6 { // ü , ^ + Caps Lock and F13: the rest are known
		t.Errorf("the layout asked %d times", asked)
	}
	if keyName(0xBA, nil) != "" {
		t.Error("a character with no layout")
	}
}

// Ctrl is primary; a declared Control is Ctrl too, as the menu bar's
// accelerators have it.
func TestKeyMods(t *testing.T) {
	if m := keyMods(false, false, false); m != 0 {
		t.Errorf("none: %d", m)
	}
	if m := keyMods(true, true, true); m != modPrimary|modShift|modAlt {
		t.Errorf("all: %d", m)
	}
	keys := keysOf(ops.AppendKeys(nil, []ops.Key{
		{Name: "d", Mods: modPrimary}, {Name: "e", Mods: modControl},
		{Name: "f", Mods: modControl | modShift}, {Name: "g", Mods: modAlt},
	}))
	ctrl := keyMods(true, false, false)
	for _, c := range []struct {
		name string
		mods uint8
		want int
	}{
		{"d", ctrl, 0}, {"e", ctrl, 1}, {"f", ctrl | modShift, 2}, {"g", modAlt, 3},
		{"d", 0, -1}, {"e", 0, -1}, {"f", ctrl, -1}, {"g", ctrl | modAlt, -1},
	} {
		if i := ops.MatchKey(keys, c.name, c.mods); i != c.want {
			t.Errorf("%s %d: %d, want %d", c.name, c.mods, i, c.want)
		}
	}
}

type keyView struct {
	name   string
	parent *keyView
	keys   []ops.Key
}

// The nearest view with the key takes it: the focused one's, then its
// ancestors', the window's root last.
func TestKeyTaker(t *testing.T) {
	root := &keyView{name: "root", keys: []ops.Key{{Name: "escape"}, {Name: "space"}}}
	outer := &keyView{name: "outer", parent: root, keys: []ops.Key{{Name: "up"}, {Name: "down"}, {Name: "escape"}, {Name: "d", Mods: modPrimary}}}
	mid := &keyView{name: "mid", parent: outer}
	field := &keyView{name: "field", parent: mid, keys: []ops.Key{{Name: "tab"}}}
	parent := func(v *keyView) *keyView { return v.parent }
	keys := func(v *keyView) []ops.Key { return v.keys }
	for _, c := range []struct {
		from *keyView
		name string
		mods uint8
		want string
		i    int
	}{
		{field, "tab", 0, "field", 0},
		{field, "down", 0, "outer", 1},
		{field, "escape", 0, "outer", 2},
		{field, "space", 0, "root", 1},
		{field, "d", modPrimary, "outer", 3},
		{field, "D", modPrimary, "outer", 3},
		{field, "d", 0, "", -1},
		{field, "a", 0, "", -1},
		{field, "down", modShift, "", -1},
		{root, "escape", 0, "root", 0},
		{root, "up", 0, "", -1},
		{nil, "escape", 0, "", -1},
	} {
		v, i := keyTaker(c.from, parent, keys, c.name, c.mods)
		got := ""
		if v != nil {
			got = v.name
		}
		if got != c.want || i != c.i {
			t.Errorf("%s %d: %q %d, want %q %d", c.name, c.mods, got, i, c.want, c.i)
		}
	}
}

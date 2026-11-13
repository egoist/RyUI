package ops

import "strings"

// Key is a key of a view's Keys prop: a lower case character or a named
// key (enter, escape, tab, space, delete, backspace, up, down, left,
// right, home, end, pageup, pagedown, f1-f12), with modifiers (1
// primary, 2 shift, 4 alt, 8 control: MenuItem's Mods).
type Key struct {
	Name string
	Mods uint8
}

// AppendKeys appends the Keys prop's encoding of keys to b.
func AppendKeys(b []byte, keys []Key) []byte {
	b = le.AppendUint32(b, uint32(len(keys)))
	for _, k := range keys {
		b = appendStr(b, k.Name)
		b = append(b, k.Mods)
	}
	return b
}

// DecodeKeys reads a Keys prop; what's cut short is left out.
func DecodeKeys(b []byte) []Key {
	r := &reader{b: b}
	n := int(r.u32())
	if r.err != nil || n > len(b) {
		return nil
	}
	keys := make([]Key, 0, n)
	for range n {
		name := string(r.bytes())
		mods := r.u8()
		if r.err != nil {
			break
		}
		keys = append(keys, Key{Name: name, Mods: mods})
	}
	return keys
}

// MatchKey is the index of the key in keys that a key going down is
// (name as the Keys prop says it: a character lower case, as the key
// types it without modifiers), or -1.
func MatchKey(keys []Key, name string, mods uint8) int {
	name = KeyName(name)
	for i, k := range keys {
		if k.Mods == mods && k.Name == name {
			return i
		}
	}
	return -1
}

// KeyName is a key's name as the Keys prop has it: a single character
// lower case, a named key as it is.
func KeyName(name string) string {
	if len([]rune(name)) == 1 {
		return strings.ToLower(name)
	}
	return name
}

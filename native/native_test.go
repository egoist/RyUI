package native

import (
	"math"
	"testing"
)

func TestPropRoundTrip(t *testing.T) {
	le := func(name string, typ byte, v []byte) []byte {
		b := []byte{byte(len(name)), 0, 0, 0}
		b = append(append(b, name...), typ)
		return append(b, v...)
	}
	if n, v, ok := DecodeProp(le("on", typeBool, []byte{1})); !ok || n != "on" || v != true {
		t.Errorf("bool: %q %v %v", n, v, ok)
	}
	f := math.Float64bits(2.5)
	fb := []byte{byte(f), byte(f >> 8), byte(f >> 16), byte(f >> 24), byte(f >> 32), byte(f >> 40), byte(f >> 48), byte(f >> 56)}
	if _, v, ok := DecodeProp(le("x", typeFloat, fb)); !ok || v != 2.5 {
		t.Errorf("float: %v %v", v, ok)
	}
	if _, _, ok := DecodeProp(le("x", typeString, []byte{9, 0, 0, 0, 'a'})); ok {
		t.Error("a short string decoded")
	}
	b := EncodeEvent("changed", map[string]any{"value": 3, "label": "x", "skip": []int{1}})
	if string(b[:4]) != "\x07\x00\x00\x00" || string(b[4:11]) != "changed" || b[11] != 2 {
		t.Errorf("event %q", b)
	}
}

package scan

import (
	"encoding/binary"
	"testing"
)

func TestDecode(t *testing.T) {
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, 2)
	for _, s := range []string{"qr", "https://example.com/a\nb", "ean13", "4006381333931"} {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	codes, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Code{{QR, "https://example.com/a\nb"}, {EAN13, "4006381333931"}}
	if len(codes) != 2 || codes[0] != want[0] || codes[1] != want[1] {
		t.Fatalf("got %v, want %v", codes, want)
	}
	if c, err := decode(binary.LittleEndian.AppendUint32(nil, 0)); err != nil || len(c) != 0 {
		t.Fatalf("none: %v %v", c, err)
	}
	for _, bad := range [][]byte{nil, {1, 0, 0, 0}, b[:len(b)-1], {0xff, 0xff, 0xff, 0x7f}} {
		if _, err := decode(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
}

func TestCodes(t *testing.T) {
	var got []Code
	kinds, found := Codes(func(c Code) { got = append(got, c) }, QR, EAN13).CameraScan()
	if kinds != "qr,ean13" {
		t.Errorf("kinds %q", kinds)
	}
	found("qr", "hello")
	if len(got) != 1 || got[0] != (Code{QR, "hello"}) {
		t.Errorf("got %v", got)
	}
	if k, _ := Codes(nil).CameraScan(); k != "" {
		t.Errorf("every kind: %q", k)
	}
}

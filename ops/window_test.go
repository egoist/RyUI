package ops

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestWindowConfig(t *testing.T) {
	c := WindowConfig{Width: 800, Height: 600, MinWidth: 320, MaxHeight: 900, X: 10, Y: -20,
		Flags: WindowFixedSize | WindowModal, TitleBar: TitleBarHidden, Background: RGBA(1, 2, 3, 255),
		Parent: 7, StateKey: "editor", Title: "Notes — 2"}
	if got := DecodeWindowConfig(AppendWindowConfig(nil, &c)); got != c {
		t.Fatalf("got %+v", got)
	}
	// The older form: four floats, then the title.
	var b []byte
	for _, v := range []float32{640, 480, 200, 100} {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	b = append(b, "Old"...)
	if got := DecodeWindowConfig(b); got != (WindowConfig{Width: 640, Height: 480, MinWidth: 200, MinHeight: 100, Title: "Old"}) {
		t.Fatalf("older form %+v", got)
	}
	// Cut short: no panic.
	full := AppendWindowConfig(nil, &c)
	for n := range len(full) {
		DecodeWindowConfig(full[:n])
	}
}

func TestWindowStateAndAction(t *testing.T) {
	s := WindowState{X: 1, Y: 2, Width: 300, Height: 200, Flags: WindowVisible | WindowFocused}
	if got := DecodeWindowState(AppendWindowState(nil, s)); got != s {
		t.Fatalf("state %+v", got)
	}
	if a, x, y, ok := DecodeWindowAction(AppendWindowAction(nil, WindowActionSize, 640, 480)); !ok || a != WindowActionSize || x != 640 || y != 480 {
		t.Fatalf("size %v %v %v %v", a, x, y, ok)
	}
	if a, _, _, ok := DecodeWindowAction(AppendWindowAction(nil, WindowActionFocus, 0, 0)); !ok || a != WindowActionFocus {
		t.Fatal("focus")
	}
	if _, _, _, ok := DecodeWindowAction([]byte{byte(WindowActionPosition), 1}); ok {
		t.Fatal("a short Position decoded")
	}
}

func TestFlyoutAndFit(t *testing.T) {
	f := Flyout{Tray: 3, X: 10, Y: 20, Width: 30, Height: 40, Placement: PlacementTopEnd, Gap: 4}
	b := AppendFlyout(nil, f)
	if got, ok := DecodeFlyout(b); !ok || got != f {
		t.Fatalf("flyout %+v %v", got, ok)
	}
	for n := range len(b) {
		if _, ok := DecodeFlyout(b[:n]); ok {
			t.Fatalf("a flyout cut to %d bytes decoded", n)
		}
	}
	if a, x, y, ok := DecodeWindowAction(AppendWindowAction(nil, WindowActionFit, 280, 120)); !ok || a != WindowActionFit || x != 280 || y != 120 {
		t.Fatalf("fit %v %v %v %v", a, x, y, ok)
	}
	if a, _, _, ok := DecodeWindowAction(AppendWindowAction(nil, WindowActionClickThrough, 0, 0)); !ok || a != WindowActionClickThrough {
		t.Fatal("click-through")
	}
}

package windows

import (
	"slices"
	"testing"
)

func TestTakeDockItem(t *testing.T) {
	for _, c := range []struct {
		args, rest []string
		n          int
	}{
		{[]string{`C:\a\App.exe`}, []string{`C:\a\App.exe`}, -1},
		{[]string{`C:\a\App.exe`, "--asuka-dock-item=3"}, []string{`C:\a\App.exe`}, 3},
		{[]string{`C:\a\App.exe`, "x", "--asuka-dock-item=12", "y"}, []string{`C:\a\App.exe`, "x", "y"}, 12},
		{[]string{`C:\a\App.exe`, "--asuka-dock-item=no"}, []string{`C:\a\App.exe`, "--asuka-dock-item=no"}, -1},
		{[]string{"--asuka-dock-item=1"}, []string{"--asuka-dock-item=1"}, -1}, // the program itself
	} {
		rest, n := takeDockItem(c.args)
		if n != c.n || !slices.Equal(rest, c.rest) {
			t.Errorf("takeDockItem(%q) = %q, %d; want %q, %d", c.args, rest, n, c.rest, c.n)
		}
	}
	if n, ok := dockItemPayload(dockItemLaunch + "7"); !ok || n != 7 {
		t.Errorf("payload: %d, %v", n, ok)
	}
	for _, s := range []string{"", "myapp://x", dockItemLaunch, dockItemLaunch + "-1"} {
		if _, ok := dockItemPayload(s); ok {
			t.Errorf("%q read as a Dock menu item", s)
		}
	}
}

// An image is fitted into the icon's square, its aspect kept, its colors
// no longer premultiplied.
func TestFitIcon(t *testing.T) {
	// 4×2, opaque red on the left half, half-transparent blue on the
	// right (premultiplied BGRA).
	px := make([]byte, 4*2*4)
	for y := range 2 {
		for x := range 4 {
			i := (y*4 + x) * 4
			if x < 2 {
				copy(px[i:], []byte{0, 0, 255, 255})
			} else {
				copy(px[i:], []byte{128, 0, 0, 128})
			}
		}
	}
	out := fitIcon(px, 4, 2, 4)
	at := func(x, y int) []byte { i := (y*4 + x) * 4; return out[i : i+4] }
	if a := at(0, 0)[3]; a != 0 {
		t.Errorf("above the image: alpha %d", a)
	}
	if p := at(0, 1); p[2] != 255 || p[3] != 255 {
		t.Errorf("red: %v", p)
	}
	if p := at(3, 2); p[0] != 255 || p[3] != 128 {
		t.Errorf("blue, half: %v", p)
	}
	if a := at(0, 3)[3]; a != 0 {
		t.Errorf("below the image: alpha %d", a)
	}
}

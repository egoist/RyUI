//go:build windows && amd64

package windows

import (
	stdimage "image"
	"testing"

	"github.com/egoist/ryui/conformance"
	"github.com/egoist/ryui/ui"
)

// TestConformance draws the conformance sheet in a real window and
// measures what it shows (package conformance).
func TestConformance(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return conformance.Sheet() })
	var img *stdimage.NRGBA
	onMain(t, func() {
		showHost(h)
		img = windowImage(h.window)
	})
	if img == nil {
		t.Fatal("the window has no size")
	}
	conformance.Check(t, "windows", img, "windows")
}

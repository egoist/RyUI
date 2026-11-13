//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
)

// Opacity between 0 and 1 fades a view and what's in it as one picture,
// as a layer's does. A view without child windows fades its own drawing.
// One with child windows is drawn faded by a picture over it
// (picture.go): child windows can't be translucent (a layered child
// window's own children aren't painted), so its tree is drawn over what's
// behind it and blended with that, and the picture, which takes no
// clicks, shows that over it. The view and what's in it are still there,
// under the picture, and take clicks and the keyboard.

// applyOpacity follows n.opacity: hidden at none (applyHidden), a
// picture between 0 and 1 if it has child windows.
func (h *Host) applyOpacity(n *node) {
	h.applyHidden(n)
	h.updatePicture(n)
}

// faded is whether n is drawn faded by a picture.
func (h *Host) faded(n *node) bool {
	return n.hwnd != 0 && n.opacity > 0.01 && n.opacity < 1 && NextWindow(n.hwnd, GW_CHILD) != 0
}

// ownAlpha is how much n's own drawing fades: its opacity, unless a
// picture fades it with what's in it.
func (n *node) ownAlpha() float32 {
	if n.pict != nil {
		return 1
	}
	return n.opacity
}

func alpha255(v float32) uint8 { return uint8(math.Round(float64(min(max(v, 0), 1)) * 255)) }

//go:build windows && amd64

package windows

import (
	"encoding/binary"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

var le = binary.LittleEndian

func (h *Host) setCommon(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropBackground:
		n.background = ops.Color(v.Int)
		h.redraw(n)
	case ops.PropCornerRadius:
		n.cornerRadius = float32(v.Float)
		h.redraw(n)
	case ops.PropBorderWidth:
		n.borderWidth = float32(v.Float)
		h.redraw(n)
	case ops.PropBorderColor:
		n.borderColor = ops.Color(v.Int)
		h.redraw(n)
	case ops.PropOpacity:
		// Child windows can't be translucent (layered child windows need
		// the app's manifest): a view's own drawing fades, and none hides
		// it.
		n.opacity = float32(v.Float)
		h.applyHidden(n)
		h.redraw(n)
	case ops.PropHidden:
		n.hidden = v.Int != 0
		h.applyHidden(n)
	case ops.PropClip:
		// Child windows always clip their children.
	case ops.PropA11yLabel:
		n.a11yLabel = v.Str
		if n.hwnd != 0 && n.kind == ops.KindView {
			// The view's window text is its accessible name.
			SetWindowText(n.hwnd, v.Str)
		}
	case ops.PropA11yHint, ops.PropA11yRole, ops.PropA11yHidden, ops.PropA11yActions, ops.PropA11yAdjustable:
		// Not yet on Windows: the controls' own accessibility only.
	case ops.PropColor:
		n.color = ops.Color(v.Int)
		h.redraw(n)
		if n.kind == ops.KindButton {
			h.redrawControl(n)
		}
	case ops.PropTextStyle:
		n.textStyle = int32(v.Int)
		h.applyFont(n)
	case ops.PropFontSize:
		n.fontSize = float32(v.Float)
		h.applyFont(n)
	case ops.PropFontWeight:
		n.fontWeight = int32(v.Int)
		h.applyFont(n)
	case ops.PropFontFamily:
		n.fontFamily = v.Str
		h.applyFont(n)
	case ops.PropTextAlign:
		n.textAlign = int32(v.Int)
		if n.rich != nil {
			n.rich.invalidate(h, n)
		}
		h.redraw(n)
	case ops.PropLineLimit:
		n.lineLimit = int(v.Int)
		if n.rich != nil {
			n.rich.invalidate(h, n)
		}
		h.redraw(n)
	case ops.PropDisabled:
		n.disabled = v.Int != 0
		if n.ctl != 0 {
			EnableWindow(n.ctl, !n.disabled)
		}
		if n.kind == ops.KindStepper {
			for _, b := range n.stepButtons() {
				EnableWindow(b, !n.disabled)
			}
		}
		h.redraw(n)
	case ops.PropMenu:
		n.menu = decodeActions(v.Bytes)
	case ops.PropMenuTrigger:
		n.menuTrigger = int32(v.Int)
	case ops.PropGradient:
		if len(v.Bytes) < 4 || le.Uint32(v.Bytes) == 0 {
			n.gradient = nil
		} else {
			n.gradient = append(n.gradient[:0], v.Bytes...)
		}
		h.redraw(n)
	case ops.PropShadow, ops.PropTransform, ops.PropMaterial, ops.PropTransition, ops.PropMatch:
		// Shadows, transforms, materials, transitions and matched views:
		// not yet on Windows (child windows draw in place, opaque).
		if p == ops.PropShadow {
			n.shadow = append(n.shadow[:0], v.Bytes...)
		}
	case ops.PropSwipeActions:
		if n.cell != nil {
			n.cell.swipe = decodeActions(v.Bytes)
		}
	}
}

// applyHidden shows or hides n's window: Hidden, or no opacity.
func (h *Host) applyHidden(n *node) {
	if n.hwnd == 0 {
		return
	}
	hide := n.hidden || n.opacity <= 0.01
	if n.nav != nil && n.kind == ops.KindScreen {
		return // a stack shows its top screen, tabs their page
	}
	if hide {
		if n.tracking || n.spaceDown {
			// A view that goes doesn't stay pressed.
			n.spaceDown = false
			h.cancelPress(n)
			if Capture() == n.hwnd {
				n.releasing = true
				ReleaseCapture()
				n.releasing = false
			}
		}
		ShowWindow(n.hwnd, SW_HIDE)
	} else {
		ShowWindow(n.hwnd, SW_SHOWNA)
	}
}

// applyFont sets the font of what draws its own text from n's text props.
func (h *Host) applyFont(n *node) {
	switch {
	case n.rich != nil:
		n.rich.invalidate(h, n)
	case n.kind == ops.KindTextField || n.kind == ops.KindWheel || n.kind == ops.KindDatePicker:
		if n.ctl != 0 {
			SendMessage(n.ctl, WM_SETFONT, h.nodeFont(n), 1)
			h.layoutField(n)
		}
	case n.kind == ops.KindButton:
		if n.ctl != 0 {
			SendMessage(n.ctl, WM_SETFONT, h.nodeFont(n), 1)
		}
	}
	h.redraw(n)
}

// decodeActions reads a Menu prop's items.
func decodeActions(b []byte) []action {
	if len(b) < 4 {
		return nil
	}
	n := int(le.Uint32(b))
	b = b[4:]
	out := make([]action, 0, n)
	for range n {
		if len(b) < 5 {
			break
		}
		a := action{role: ops.AlertButtonRole(b[0])}
		b = b[1:]
		a.title, b = readString(b)
		a.icon, b = readString(b)
		out = append(out, a)
	}
	return out
}

func readString(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(le.Uint32(b))
	if len(b) < 4+n {
		return "", nil
	}
	return string(b[4 : 4+n]), b[4+n:]
}

// decodeStrings reads a u32 count, then u32-length strings.
func decodeStrings(b []byte) []string {
	if len(b) < 4 {
		return nil
	}
	n := int(le.Uint32(b))
	b = b[4:]
	out := make([]string, 0, n)
	for range n {
		var s string
		s, b = readString(b)
		if b == nil && len(out) < n-1 {
			break
		}
		out = append(out, s)
	}
	return out
}

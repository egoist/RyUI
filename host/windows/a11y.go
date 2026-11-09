//go:build windows && amd64

package windows

import (
	"strings"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// Screen readers on Windows (Narrator, NVDA) read the host's views
// through UI Automation: each view of the host's own drawing answers
// WM_GETOBJECT with a provider of its own (internal/windows' uia.go) on
// top of its window's: its name (Label; a text's text; a pressable view's
// texts, as VoiceOver reads a row), its help text (Hint, else Tooltip),
// its control type (Role: a button, a heading, an image, a link, a text,
// a search field; a pressable view is a button), whether a screen reader
// skips it (Decorative, with everything in it), and the Invoke pattern
// for a view that presses. Announce raises a notification. A view's
// screen reader actions are its context menu (Shift+F10, the menu key, a
// right-click), and an adjustable view takes the arrow keys, as a slider
// does: UI Automation's RangeValue pattern sets a double, which a
// callback of Go's can't take.

type accessible struct {
	h *Host
	n *node
}

func (a accessible) Property(id int32) any {
	h, n := a.h, a.n
	if n.hwnd == 0 {
		return nil
	}
	switch id {
	case UIA_NamePropertyId:
		if n.a11yLabel != "" {
			return n.a11yLabel
		}
		switch {
		case n.kind == ops.KindText && n.text != "":
			return n.text
		case n.pressable():
			var parts []string
			collectTexts(n, &parts)
			if len(parts) > 0 {
				return strings.Join(parts, ", ")
			}
		}
	case UIA_HelpTextPropertyId:
		if n.a11yHint != "" {
			return n.a11yHint
		}
		if n.tooltip != "" {
			return n.tooltip
		}
	case UIA_ControlTypePropertyId:
		switch ops.Role(n.a11yRole) {
		case ops.RoleButton:
			return int32(UIA_ButtonControlTypeId)
		case ops.RoleHeader, ops.RoleText:
			return int32(UIA_TextControlTypeId)
		case ops.RoleImage:
			return int32(UIA_ImageControlTypeId)
		case ops.RoleLink:
			return int32(UIA_HyperlinkControlTypeId)
		case ops.RoleSearch:
			return int32(UIA_EditControlTypeId)
		}
		switch {
		case n.pressable():
			return int32(UIA_ButtonControlTypeId)
		case n.kind == ops.KindText:
			return int32(UIA_TextControlTypeId)
		case n.kind == ops.KindImage || n.kind == ops.KindSymbol:
			return int32(UIA_ImageControlTypeId)
		case n.a11yLabel != "":
			return int32(UIA_GroupControlTypeId)
		}
	case UIA_HeadingLevelPropertyId:
		if ops.Role(n.a11yRole) == ops.RoleHeader {
			return int32(HeadingLevel1)
		}
	case UIA_IsControlElementPropertyId, UIA_IsContentElementPropertyId:
		if h.a11yHidden(n) {
			return false
		}
		if n.kind == ops.KindText || n.pressable() || n.a11yLabel != "" {
			return true
		}
	}
	return nil
}

func (a accessible) Invokes() bool {
	return a.n.hwnd != 0 && !a.n.disabled && a.n.listening(ops.EventPress)
}

func (a accessible) Invoke() { a.h.press(a.n, a.n.hwnd) }

// collectTexts gathers the texts in n, as its name: their labels, or
// their texts.
func collectTexts(n *node, out *[]string) {
	for _, c := range n.children {
		if c.hidden || c.a11yHidden {
			continue
		}
		switch {
		case c.a11yLabel != "":
			*out = append(*out, c.a11yLabel)
		case c.kind == ops.KindText && c.text != "":
			*out = append(*out, c.text)
		default:
			collectTexts(c, out)
		}
	}
}

// a11yHidden reports whether n, or a view it's in, is hidden from screen
// readers.
func (h *Host) a11yHidden(n *node) bool {
	for x := n; x != nil; x = x.parent {
		if x.a11yHidden {
			return true
		}
	}
	return false
}

// getObject answers WM_GETOBJECT for UI Automation: n's provider.
func (h *Host) getObject(n *node, hwnd, wp, lp uintptr) (uintptr, bool) {
	if int32(lp) != UiaRootObjectID || n.ctl != 0 || n.kind == ops.KindNative {
		return 0, false // a control's window, or the app's: theirs
	}
	if n.uia == 0 {
		n.uia = NewProvider(hwnd, accessible{h: h, n: n})
		if n.uia == 0 {
			return 0, false
		}
	}
	return ReturnProvider(hwnd, wp, lp, n.uia), true
}

// dropProvider lets go of a view's provider as its window goes.
func (h *Host) dropProvider(n *node, hwnd uintptr) {
	if n.uia == 0 {
		return
	}
	DropProviders(hwnd)
	Release(n.uia)
	n.uia = 0
}

// setA11y sets a screen reader prop.
func (h *Host) setA11y(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropA11yHint:
		n.a11yHint = v.Str
	case ops.PropA11yRole:
		n.a11yRole = int32(v.Int)
	case ops.PropA11yHidden:
		n.a11yHidden = v.Int != 0
	case ops.PropA11yActions:
		n.a11yActions = decodeStrings(v.Bytes)
		h.a11yFocusable(n)
	case ops.PropA11yAdjustable:
		n.adjustable = v.Int != 0
		h.a11yFocusable(n)
	}
	if n.hwnd != 0 && ClientsListening() {
		NotifyWinEvent(EVENT_OBJECT_NAMECHANGE, n.hwnd, OBJID_CLIENT, CHILDID_SELF)
	}
}

// a11yFocusable makes a view with actions or adjustable a tab stop: the
// keyboard reaches its context menu and its arrow keys.
func (h *Host) a11yFocusable(n *node) {
	if n.hwnd == 0 || n.kind != ops.KindView {
		return
	}
	if len(n.a11yActions) > 0 || n.adjustable {
		SetWindowLong(n.hwnd, GWL_STYLE, WindowLong(n.hwnd, GWL_STYLE)|WS_TABSTOP)
	}
}

// adjustKey adjusts an adjustable view with the arrow keys: up and the
// reading direction's forward arrow by one step, the others back.
func (h *Host) adjustKey(n *node, vk uintptr) bool {
	if !n.adjustable {
		return false
	}
	var by int64
	switch vk {
	case VK_UP:
		by = 1
	case VK_DOWN:
		by = -1
	case VK_RIGHT, VK_LEFT:
		by = 1
		if (vk == VK_LEFT) != h.isRTL(n) {
			by = -1
		}
	default:
		return false
	}
	h.emit(n, ops.Event{Kind: ops.EventAdjust, Int: by})
	return true
}

// actionsMenu shows a view's screen reader actions as a popup menu (its
// context menu), at the mouse or, from the keyboard (lp -1), at the view;
// the action taken is A11yAction.
func (h *Host) actionsMenu(n *node, hwnd, lp uintptr) bool {
	if len(n.a11yActions) == 0 {
		return false
	}
	id := n.id
	fromKeys := int32(lp) == -1
	h.afterCore(func() {
		n := h.nodes[id]
		if n == nil || n.hwnd == 0 {
			return
		}
		acts := make([]action, len(n.a11yActions))
		for i, s := range n.a11yActions {
			acts[i] = action{title: s}
		}
		rtl := h.isRTL(n)
		m, om := h.actionMenu(acts, rtl)
		p := CursorPos()
		if fromKeys {
			r := WindowRect(n.hwnd)
			p = Point{X: r.Left, Y: r.Bottom}
			if rtl {
				p.X = r.Right
			}
		}
		flags := uint32(TPM_LEFTALIGN | TPM_TOPALIGN | TPM_RIGHTBUTTON)
		if rtl {
			flags |= TPM_LAYOUTRTL
		}
		h.modal++
		if om != nil {
			h.menuLoop(true)
		}
		got := TrackPopupMenu(m, flags, p.X, p.Y, RootWindow(n.hwnd))
		h.menuLoop(false)
		h.modal--
		DestroyMenu(m)
		h.forget(om)
		if got > 0 {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventA11yAction, Int: int64(got - 1)})
			}
		}
	})
	return true
}

// announce has screen readers say text.
func (h *Host) announce(text string) {
	if text == "" || h.window == 0 {
		return
	}
	Announce(h.windowInUse(), text)
}

//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A hardware keyboard's keys for the views that declare them (the Keys
// prop, UseKeys' on the root or a Window node): the message loop hands
// each key going down (WM_KEYDOWN, WM_SYSKEYDOWN, and their repeats) to
// routeKey before anything else sees it (TranslateMessage, the menus'
// accelerators, IsDialogMessage's Tab and arrows, the host's own keys:
// Escape closing a panel, a flyout, a sheet or a popover; the focused
// control's: a field's caret, its Enter). It starts at the focused
// window's node (a field's EDIT is its TextField's; a window inside a
// control, a combo box's EDIT, its nearest window the host knows), or at
// the root of the top-level window that has the keyboard with nothing in
// it focused (the main window's root, a Window's, a sheet's, a
// popover's), and goes out through the nodes' parents (keyParent): a
// list's cell on to its list, a sheet's or a popover's node to the
// window it covers, ending at the root of a main window or a Window. The
// nearest node whose keys match takes the key, which is consumed, and the
// core gets Key with its index. The key's letting go is consumed with it,
// and Alt's, if the key took Alt (or DefWindowProc would open the menu
// bar as Alt goes up).
//
// The keys of an input method composing text are its own: those it takes
// come as VK_PROCESSKEY, and none is routed while it composes.

// routeKey gives a key message to the nearest view whose Keys have it, as
// the message loop gets it; false: the key goes on.
func (h *Host) routeKey(m *Msg) bool {
	switch m.Message {
	case WM_KEYDOWN, WM_SYSKEYDOWN:
	case WM_KEYUP, WM_SYSKEYUP:
		if m.WParam < uintptr(len(h.keyUps)) && h.keyUps[m.WParam] {
			h.keyUps[m.WParam] = false
			return true
		}
		return false
	default:
		return false
	}
	if m.WParam == VK_PROCESSKEY || m.WParam >= uintptr(len(h.keyUps)) {
		return false
	}
	if m.LParam&(1<<30) == 0 {
		// Pressed, not repeated: a letting go missed (it went to another
		// app's window) isn't waited for any longer.
		h.keyUps[m.WParam] = false
	}
	if KeyDown(VK_LWIN) || KeyDown(VK_RWIN) {
		return false // the system's shortcuts, none of the Keys prop's
	}
	from := h.keyStart(m.Hwnd)
	if from == nil {
		return false
	}
	name := keyName(uint32(m.WParam), KeyChar)
	if name == "" || Composing(m.Hwnd) {
		return false
	}
	mods := keyMods(KeyDown(VK_CONTROL), KeyDown(VK_SHIFT), KeyDown(VK_MENU))
	n, i := keyTaker(from, h.keyParent, func(n *node) []ops.Key { return n.keys }, name, mods)
	if n == nil {
		return false
	}
	h.keyUps[m.WParam] = true
	if mods&modAlt != 0 {
		h.keyUps[VK_MENU] = true
	}
	h.emit(n, ops.Event{Kind: ops.EventKey, Int: int64(i)})
	return true
}

// keyStart is the node a key for hwnd (the window with the keyboard's
// focus) starts at: its window's, or its nearest ancestor's the host
// knows, up to its top-level window: a Window's, a sheet's or a
// popover's (whose node's parent is the Window, the sheet, the popover),
// or the main window's root. Nil: not a window of the app's views (a
// dialog's, the system's).
func (h *Host) keyStart(hwnd uintptr) *node {
	if hwnd == 0 {
		return nil
	}
	top := RootWindow(hwnd)
	for w := hwnd; w != 0; w = Parent(w) {
		if n := h.byHwnd[w]; n != nil {
			return n
		}
		if w == top {
			break // Parent is a popup's owner past here
		}
	}
	if top == h.window {
		return h.root
	}
	return nil
}

// keyParent is the node a key goes out to from n: its parent in the
// tree; from a node the core leaves without one, where it shows: a list's
// cell its list, a sheet's or a popover's node the root of the window it
// covers (its owner: the main window's root, a Window, a sheet, a
// popover), so its way ends at that window's root, with its UseKeys'. The
// root and a Window are ends: a Window's keys are its own. A header's
// search field goes to the screen it searches; another view the host
// made (a stack's header) to the view its window is in.
func (h *Host) keyParent(n *node) *node {
	switch {
	case n == h.root:
		return nil
	case n.searchOf != nil:
		if s := n.searchOf.nav.screen; s != nil {
			return s
		}
		return n.searchOf
	case n.parent != nil:
		return n.parent
	case n.cell != nil:
		if l := n.cell.l.n; h.nodes[l.id] == l {
			return l
		}
		return nil
	case n.kind == ops.KindSheet, n.kind == ops.KindPopover:
		if r := h.windowRoot(n.nav.owner); r != nil && r != n {
			return r
		}
		return h.root
	case n.kind == ops.KindWindow, n.hwnd == 0:
		return nil
	}
	top := RootWindow(n.hwnd)
	for w := n.hwnd; w != top && w != 0; {
		w = Parent(w)
		if p := h.byHwnd[w]; p != nil && p != n {
			return p
		}
	}
	if top == h.window && top != n.hwnd {
		return h.root
	}
	return nil
}

// windowRoot is the node a top-level window of the app's shows (the
// root, a Window, a sheet, a popover), or nil.
func (h *Host) windowRoot(w uintptr) *node {
	if w == 0 {
		return nil
	}
	if r := h.rootOf(w); r != nil {
		return r
	}
	if f := h.byHwnd[w]; f != nil && f.kind == kindPopoverWindow {
		return f.parent
	}
	return nil
}

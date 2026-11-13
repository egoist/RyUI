//go:build linux && !android

package linux

import (
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// Keys (the Keys prop, ui.KeyMap and ui.UseKeys): each window of the
// app's, and each popover (GTK takes a popover's keys from its focus
// widget up to it, not through the window), has a key controller of the
// host's in the capture phase, ahead of the window's own: GTK runs a
// widget's controllers the last added first, and the application's
// accelerators (the menus' shortcuts, a global scope shortcut
// controller) run from the window's shortcut manager's, which GTK makes
// with the window. A key going down, and each repeat, starts at the
// window's focus widget's node (the nearest widget out from it that is a
// node's view: an entry's GtkText is its field's; the header bar's search
// entry, its screen's), or the window's root (the main window's root, a
// Window node; a popover's node) where no node has the focus, and goes
// out through the nodes' parents, and on from a node with none where it
// shows (keyOut): a sheet's (an AdwDialog in the window it was presented
// over) and a popover's node to that window's root, which ends it, a
// Window node ending its content (a list's cells are the host's views,
// in the list). The
// first node whose Keys have the key takes it: the controller stops it,
// so neither the focused control (an entry's caret, its activate) nor the
// accelerators nor a panel's or a popover's Escape get it, and the node
// gets Key with the key's index. Not while an input method composes text
// in the focused widget (its preedit-changed, with text): its keys are
// its own.
//
// A key is matched as it types with no modifiers: the named keys by the
// key's keyval (KP_ variants too; ISO_Left_Tab, Shift+Tab, is tab), a
// character by the keyval of the key's level 0 in the event's layout
// (Shift+a is a with Shift, Shift+1 is 1 with Shift), lower case; a key
// typed with AltGr (level 2 and up) is the character it types. Ctrl is
// primary, and the control modifier too (as the menus' accelerators take
// both); Super and Hyper match no Keys.

func init() {
	commonHooks = append(commonHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if p != ops.PropKeys {
			return false
		}
		n.keys = n.keys[:0]
		for _, k := range ops.DecodeKeys(v.Bytes) {
			if k.Mods&8 != 0 { // control: Ctrl, as primary is
				k.Mods = k.Mods&^8 | 1
			}
			n.keys = append(n.keys, k)
		}
		return true
	})
}

// routeKeys gives a window the key controller that routes keys to the
// nodes' Keys, in the capture phase.
func (h *Host) routeKeys(w *win) {
	h.keyController(w.window, func() (*win, *node) { return w, w.root })
}

// keyController adds a capture phase key controller routing keys to the
// nodes' Keys to native (a window, or a popover: GTK propagates a key
// from a popover's focus widget up to the popover, not the window under
// it). at is the window it's in and the node a key starts from when no
// node in native has the focus.
func (h *Host) keyController(native Ptr, at func() (*win, *node)) {
	k := EventControllerKeyNew()
	EventControllerSetPropagationPhase(k, PhaseCapture)
	Connect(k, "key-pressed", func(keyval, keycode, state uint32) bool {
		w, start := at()
		if w == nil {
			return false
		}
		return h.keyPressed(w, native, start, k, keyval, keycode, state)
	})
	WidgetAddController(native, k)
}

// keyPressed routes a key going down in native, in w, reporting whether
// a node's Keys took it.
func (h *Host) keyPressed(w *win, native Ptr, start *node, ctrl Ptr, keyval, keycode, state uint32) bool {
	ev := Key.EventControllerGetCurrentEvent(ctrl)
	if ev != 0 && Key.KeyEventIsModifier(ev) {
		return false
	}
	focus := WindowGetFocus(w.window)
	if focus != 0 && native != w.window && focus != native && !inside(focus, native) {
		focus = 0
	}
	if focus != 0 && composing(focus) {
		return false
	}
	name, mods := keyOf(native, ev, keyval, keycode, state)
	if name == "" {
		return false
	}
	return h.routeKey(h.focusNode(w, native, focus, start), name, mods)
}

// routeKey gives a key to the nearest node, from n out (keyOut), whose
// Keys have it.
func (h *Host) routeKey(n *node, name string, mods uint8) bool {
	for ; n != nil; n = h.keyOut(n) {
		if h.takeKey(n, name, mods) {
			return true
		}
	}
	return false
}

// keyOut is where a key goes on from n, not taking it: n's parent (a
// list's cells and rows are views of the host's in it, with their
// parents); from a node the core leaves without one, where it shows: a
// sheet's node the root of the window it was presented over (the main
// window's root, a Window node), a popover's node the root of what its
// anchor is in (a window's, or a sheet's node). The root and a Window
// node are ends (nil): a Window's keys are its own.
func (h *Host) keyOut(n *node) *node {
	if n.parent != nil {
		return n.parent
	}
	switch n.kind {
	case ops.KindSheet:
		if w := n.nav.over; w != nil && !w.closing && w.root != nil && w.root != n {
			return w.root
		}
	case ops.KindPopover:
		var a *node
		if po, ok := n.ext.(*popover); ok {
			a = h.nodes[po.anchor]
		}
		for a != nil && a.parent != nil {
			a = a.parent
		}
		if a != nil && a != n {
			return a
		}
	default:
		return nil
	}
	if h.main != nil {
		return h.main.root
	}
	return nil
}

// takeKey gives n the key if its Keys have it.
func (h *Host) takeKey(n *node, name string, mods uint8) bool {
	i := ops.MatchKey(n.keys, name, mods)
	if i < 0 {
		return false
	}
	h.dispatch(ops.Event{ID: n.id, Kind: ops.EventKey, Int: int64(i)})
	return true
}

// focusNode is the node the keyboard's focus is in, in native (focus
// its focus widget): the nearest node's view out from focus, else start.
func (h *Host) focusNode(w *win, native, focus Ptr, start *node) *node {
	for f := focus; f != 0 && f != native; f = WidgetGetParent(f) {
		if n := h.byWidget[f]; n != nil && h.nodes[n.id] == n {
			return n
		}
		if s := w.search; s != nil && f == s.entry && s.screen != nil {
			return s.screen
		}
	}
	return start
}

// keyToNodes gives a key (a shortcut's key, lower case: down, k; mods
// as the Keys prop's) to the Keys of the views in the window in use, as
// the keyboard would: an automation run's key step.
func (h *Host) keyToNodes(name string, mods uint8) bool {
	if name == "esc" {
		name = "escape"
	}
	w := h.activeWin
	if w == nil || w.closing {
		w = h.main
	}
	if w == nil {
		return false
	}
	focus := WindowGetFocus(w.window)
	if focus != 0 && composing(focus) {
		return false
	}
	return h.routeKey(h.focusNode(w, w.window, focus, w.root), ops.KeyName(name), mods)
}

// inside reports whether w is inside native.
func inside(w, native Ptr) bool {
	for ; w != 0; w = WidgetGetParent(w) {
		if w == native {
			return true
		}
	}
	return false
}

// keyOf is a key event's key as the Keys prop names it (a lower case
// character, or a named key; "": neither) and its modifiers.
func keyOf(window, ev Ptr, keyval, keycode, state uint32) (string, uint8) {
	var mods uint8
	if state&ModControl != 0 {
		mods |= 1
	}
	if state&ModShift != 0 {
		mods |= 2
	}
	if state&(ModAlt|ModMeta) != 0 {
		mods |= 4
	}
	if state&(ModSuper|ModHyper) != 0 {
		mods |= 16 // no Keys have it
	}
	if name := namedKey(keyval); name != "" {
		return name, mods
	}
	var r uint32
	if ev != 0 && Key.KeyEventGetLevel(ev) >= 2 {
		// AltGr's: the character it types, with any Shift it took.
		r = KeyvalToUnicode(Key.KeyvalToLower(keyval))
		if Key.KeyEventGetConsumedModifiers(ev)&ModShift != 0 {
			mods &^= 2
		}
	} else {
		// The key's level 0, in the event's layout.
		var kv, consumed uint32
		var group, effGroup, level int32
		if ev != 0 {
			group = int32(Key.KeyEventGetLayout(ev))
		}
		if Key.DisplayTranslateKey(WidgetGetDisplay(window), keycode, 0, group, &kv, &effGroup, &level, &consumed) {
			r = KeyvalToUnicode(Key.KeyvalToLower(kv))
		}
		if r == 0 { // a keypad's digit with Num Lock: as typed
			r = KeyvalToUnicode(Key.KeyvalToLower(keyval))
		}
	}
	if r < 0x20 || r == 0x7f {
		return "", 0
	}
	return ops.KeyName(string(rune(r))), mods
}

// namedKey is a keyval's name among the Keys prop's named keys, or "".
func namedKey(keyval uint32) string {
	switch keyval {
	case 0xff0d, 0xff8d, 0xfe34: // Return, KP_Enter, ISO_Enter
		return "enter"
	case keyEscape:
		return "escape"
	case 0xff09, 0xfe20, 0xff89: // Tab, ISO_Left_Tab (Shift+Tab), KP_Tab
		return "tab"
	case 0x20, 0xff80: // space, KP_Space
		return "space"
	case 0xffff, 0xff9f: // Delete, KP_Delete
		return "delete"
	case 0xff08:
		return "backspace"
	case keyUp, keyKPUp:
		return "up"
	case keyDown, keyKPDown:
		return "down"
	case keyLeft, keyKPLeft:
		return "left"
	case keyRight, keyKPRight:
		return "right"
	case keyHome, 0xff95:
		return "home"
	case keyEnd, 0xff9c:
		return "end"
	case keyPageUp, 0xff9a:
		return "pageup"
	case keyPageDown, 0xff9b:
		return "pagedown"
	}
	if keyval >= 0xffbe && keyval <= 0xffc9 { // F1-F12
		return fKeys[keyval-0xffbe]
	}
	return ""
}

var fKeys = [12]string{"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12"}

// watchPreedit notes whether an input method composes text in a GtkText
// or a GtkTextView (its preedit-changed): composing reads it.
func watchPreedit(w Ptr) {
	if w == 0 {
		return
	}
	Connect(w, "preedit-changed", func(s uintptr) {
		var on Ptr
		if s != 0 && *At[byte](Ptr(s)) != 0 {
			on = 1
		}
		Key.ObjectSetData(w, "asuka-preedit", on)
	})
}

// composing reports whether an input method composes text in a widget.
func composing(w Ptr) bool { return Key.ObjectGetData(w, "asuka-preedit") != 0 }

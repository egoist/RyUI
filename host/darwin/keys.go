//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// Keys on a Mac (ops.PropKeys): a local event monitor for key down,
// installed once, sees each key going down (and each repeat) as the
// application takes it from its queue, before NSApplication's
// sendEvent: hands it to the menu bar's key equivalents
// (performKeyEquivalent:) and the key window's first responder (a field
// editor, cancelOperation:, insertNewline:). So it serves every window
// the same way, whatever its class: the main window, a Window's, a
// sheet's, a popover's, a flyout's or a panel's (a nonactivating panel
// takes the keyboard without the app being active, and its keys still
// come through the app's queue). An NSWindow sendEvent: override would
// come after the menu bar's key equivalents, and only in the windows the
// host subclasses (NSWindow for most, AsukaPanel for floating ones),
// not a popover's.
//
// The key starts at the first responder's node (a field editor's: its
// field's; a view the host doesn't know: its nearest superview that it
// does), else, when the window itself or nothing of the host's has the
// focus, at the window's root node (the root, a Window's, a sheet's or
// a popover's node); it goes out through the nodes' parents, as the core
// has them, to that root (a Window's, a sheet's or a popover's content
// is under its node, which has no parent: another window's keys are its
// own). The first node whose Keys have it takes it: the event goes no
// further and the node gets Key. While an input
// method composes text (the first responder has marked text), its keys
// are its own.

// keyMonitor is the local monitor of keys going down.
var keyMonitor ID

// AppKit's.
const (
	nsEventMaskKeyDown = 1 << 10

	nsEventModifierShift   = 1 << 17
	nsEventModifierOption  = 1 << 19
	nsEventModifierCommand = 1 << 20
)

// watchKeys installs the key monitor (once; it routes to theHost).
func watchKeys() {
	if keyMonitor != 0 {
		return
	}
	blk := NewBlock(func(_ objc.Block, ev ID) ID {
		if h := theHost; h != nil && h.keyDown(ev) {
			return 0
		}
		return ev
	})
	keyMonitor = Retain(Send(Class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", nsEventMaskKeyDown, uintptr(blk)))
}

// setKeys caches a node's Keys prop, decoded.
func (n *node) setKeys(b []byte) {
	n.keys = ops.DecodeKeys(b)
	if len(n.keys) == 0 {
		n.keys = nil
	}
}

// keyDown routes a key going down; true: a node took it.
func (h *Host) keyDown(ev ID) bool {
	w := Send(ev, "window")
	if w == 0 {
		w = Send(Send(Class("NSApplication"), "sharedApplication"), "keyWindow")
	}
	if w == 0 {
		return false
	}
	fr := Send(w, "firstResponder")
	if RespondsTo(fr, "hasMarkedText") && SendBool(fr, "hasMarkedText") {
		return false // an input method's
	}
	n := h.keyStart(w, fr)
	if n == nil {
		return false
	}
	name, mods := keyOf(ev)
	if name == "" {
		return false
	}
	for ; n != nil; n = n.parent {
		if n.keys != nil {
			if i := ops.MatchKey(n.keys, name, mods); i >= 0 {
				h.emit(n, ops.Event{Kind: ops.EventKey, Int: int64(i)})
				return true
			}
		}
	}
	return false
}

// keyStart is the node a key in window w starts at, fr its first
// responder: fr's, or w's root (nil: w isn't the app's).
func (h *Host) keyStart(w, fr ID) *node {
	v := fr
	if v != 0 && SendBool(v, "isKindOfClass:", uintptr(Class("NSText"))) && SendBool(v, "isFieldEditor") {
		if d := Send(v, "delegate"); d != 0 {
			v = d // a field being edited
		}
	}
	if v != 0 && SendBool(v, "isKindOfClass:", uintptr(Class("NSView"))) {
		for ; v != 0; v = Send(v, "superview") {
			if n := h.byView[v]; h.alive(n) {
				return n
			}
		}
	}
	// The window, or nothing of the host's, has the focus.
	if x := h.winOf(w); x != nil {
		return x.root
	}
	// A sheet's or a popover's window: its content's node.
	return h.nodeIn(Send(w, "contentView"), 3)
}

// nodeIn is the node of v or of the first view under v, depth levels
// down at most, the host knows.
func (h *Host) nodeIn(v ID, depth int) *node {
	if v == 0 {
		return nil
	}
	if n := h.byView[v]; h.alive(n) {
		return n
	}
	if depth == 0 {
		return nil
	}
	for _, c := range ArrayItems(Send(v, "subviews")) {
		if n := h.nodeIn(c, depth-1); n != nil {
			return n
		}
	}
	return nil
}

// keyOf is a key event's name, as the Keys prop has it (a named key, or
// the character the key types without modifiers), and its modifiers (1
// ⌘, 2 shift, 4 option, 8 control).
func keyOf(ev ID) (string, uint8) {
	f := uint(Send(ev, "modifierFlags"))
	var mods uint8
	if f&nsEventModifierCommand != 0 {
		mods |= 1
	}
	if f&nsEventModifierShift != 0 {
		mods |= 2
	}
	if f&nsEventModifierOption != 0 {
		mods |= 4
	}
	if f&nsEventModifierControl != 0 {
		mods |= 8
	}
	if name, ok := keyCodeNames[uint16(SendInt(ev, "keyCode"))]; ok {
		return name, mods
	}
	// The character without modifiers: Shift is a modifier (⇧1 is 1 with
	// shift, not !), as option and control are.
	var s ID
	if RespondsTo(ev, "charactersByApplyingModifiers:") {
		s = Send(ev, "charactersByApplyingModifiers:", 0)
	} else {
		s = Send(ev, "charactersIgnoringModifiers")
	}
	c := GoString(s)
	if len([]rune(c)) != 1 {
		return "", 0 // a dead key, or none
	}
	return ops.KeyName(c), mods
}

// keyCodeNames are the named keys, by their virtual key codes.
var keyCodeNames = map[uint16]string{
	0x24: "enter", 0x4c: "enter", // Return, the keypad's Enter
	0x35: "escape", 0x30: "tab", 0x31: "space",
	0x75: "delete", 0x33: "backspace", // ⌦, ⌫
	0x7e: "up", 0x7d: "down", 0x7b: "left", 0x7c: "right",
	0x73: "home", 0x77: "end", 0x74: "pageup", 0x79: "pagedown",
	0x7a: "f1", 0x78: "f2", 0x63: "f3", 0x76: "f4", 0x60: "f5", 0x61: "f6",
	0x62: "f7", 0x64: "f8", 0x65: "f9", 0x6d: "f10", 0x67: "f11", 0x6f: "f12",
}

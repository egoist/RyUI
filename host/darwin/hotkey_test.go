//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"testing"

	"github.com/ebitengine/purego"

	"github.com/egoist/asuka/hotkey"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// hotkeyArgs is hotkey.Register's "register": u32 id, u8 mods, the key.
func hotkeyArgs(id uint32, mods ui.Modifiers, key string) []byte {
	le := binary.LittleEndian
	b := append(le.AppendUint32(nil, id), byte(mods))
	b = le.AppendUint32(b, uint32(len(key)))
	return append(append(b, key...), "a test's"...)
}

// A hot key registers with Carbon; its event (sent through the
// application's event target, as the system sends it) answers "next";
// unregistered, it doesn't. Keys a Mac hasn't, and the system's own
// shortcuts, fail.
func TestHotkeyModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		m := h.module("hotkey")
		call := func(name string, args []byte) (out []byte, err error, answered bool) {
			m[name](args, func(b []byte, e error) { out, err, answered = b, e, true })
			return
		}
		// F19 with every modifier: no app or system has it.
		const id = 7001
		if _, err, ok := call("register", hotkeyArgs(id, ui.Primary|ui.Shift|ui.Alt|ui.Control, "f19")); !ok || err != nil {
			fatalf(t, "register: %v %v", ok, err)
		}
		if hotkeys.refs[id] == 0 {
			fatal(t, "no Carbon registration")
		}
		var got []byte
		var nerr error
		answered := false
		m["next"](nil, func(b []byte, e error) { got, nerr, answered = b, e, true })
		if answered {
			fatal(t, "next answered before a press")
		}
		if err := SendHotKeyEvent(id); err != nil {
			fatal(t, err)
		}
		if !answered || nerr != nil || len(got) != 4 || binary.LittleEndian.Uint32(got) != id {
			fatalf(t, "next after a press: %v %x %v", answered, got, nerr)
		}
		// Another app's id (a signature of its own) isn't ours; one not
		// registered is dropped.
		SendHotKeyEvent(9999)
		if len(hotkeys.pressed) != 0 {
			fatalf(t, "an unknown id: %x", hotkeys.pressed)
		}
		call("unregister", binary.LittleEndian.AppendUint32(nil, id))
		if _, ok := hotkeys.refs[id]; ok {
			fatal(t, "still registered")
		}
		SendHotKeyEvent(id)
		if len(hotkeys.pressed) != 0 {
			fatal(t, "a press after unregister")
		}
		// "stop" fails the waiting poll.
		answered = false
		m["next"](nil, func(b []byte, e error) { nerr, answered = e, true })
		call("stop", nil)
		if !answered || nerr == nil || nerr.Error() != "stopped" {
			fatalf(t, "stop: %v %v", answered, nerr)
		}
		if _, err, _ := call("register", hotkeyArgs(id+1, ui.Primary, "nope")); err == nil || err.Error() != "no key" {
			fatalf(t, "a key a Mac hasn't: %v", err)
		}
		// The key that types "k" on this layout, and "+" with Shift (the
		// main row's, not the keypad's).
		if code, mods, ok := carbonHotKey("k", 1); !ok || mods != CarbonCmd || code > 127 {
			fatalf(t, "k: %d %x %v", code, mods, ok)
		}
		if code, mods, ok := carbonHotKey("+", 0); !ok || mods != CarbonShift || code >= 0x41 && code <= 0x5C {
			fatalf(t, "+: %#x %#x %v", code, mods, ok)
		}
		// The system's ⌘Tab (the app switcher).
		if _, err, _ := call("register", hotkeyArgs(id+2, ui.Primary, "tab")); err == nil || err.Error() != "taken" {
			fatalf(t, "⌘Tab: %v", err)
		}
		// Keys registered exclusively elsewhere (as another app's would
		// be): taken.
		ref, st := RegisterHotKey(0x50, CarbonCmd|CarbonControl, 99) // F19
		if st != 0 {
			fatalf(t, "the other registration: %d", st)
		}
		_, err, _ := call("register", hotkeyArgs(id+3, ui.Primary|ui.Control, "f19"))
		UnregisterHotKey(ref)
		if err == nil || err.Error() != "taken" {
			fatalf(t, "keys another has: %v", err)
		}
	})
}

// Through the package: Register in a component; a press runs OnPress
// on the main thread; disposing it lets the keys go.
func TestHotkeyPackage(t *testing.T) {
	presses := 0
	var k *hotkey.Hotkey
	h, r := mount(t, func() ui.Node {
		k = hotkey.Register(hotkey.Props{
			Shortcut: ui.Val(ui.Cmd("f18").With(ui.Shift | ui.Alt | ui.Control)),
			OnPress:  func() { presses++ },
		})
		return ui.StaticText("x")
	})
	var id uint32
	onMain(func() {
		if !spin(func() bool { return k.Status() == hotkey.Active }) {
			fatalf(t, "status %v, %v", k.Status(), k.Err())
		}
		for i := range hotkeys.refs {
			id = i
		}
		// The poll is waiting once the registration's answer is in.
		if !spin(func() bool { return hotkeys.waiting != nil }) {
			fatal(t, "no poll")
		}
		SendHotKeyEvent(id)
		if !spin(func() bool { return presses == 1 }) {
			fatalf(t, "%d presses", presses)
		}
		if realKeys(0x4F, 1<<17|1<<18|1<<19) { // F18 with Shift, Control, Option
			if !spin(func() bool { return presses == 2 }) {
				fatalf(t, "a real key press: %d presses", presses)
			}
		} else {
			t.Log("not trusted to post key events (Accessibility): the real key press is skipped")
		}
		h.enter(r.Dispose)
		spin(func() bool { return len(hotkeys.refs) == 0 })
		if len(hotkeys.refs) != 0 || k.Status() != hotkey.Off {
			fatalf(t, "after dispose: %v, %v", hotkeys.refs, k.Status())
		}
	})
}

// realKeys posts a key's press and release with flags to the system
// (CGEventPost, as a keyboard would), if the process may (Accessibility).
func realKeys(code uint16, flags uint64) bool {
	as := Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
	var trusted func() bool
	purego.RegisterLibFunc(&trusted, as, "AXIsProcessTrusted")
	if !trusted() {
		return false
	}
	var create func(src uintptr, code uint16, down bool) uintptr
	var setFlags func(ev uintptr, flags uint64)
	var post func(tap uint32, ev uintptr)
	purego.RegisterLibFunc(&create, CoreGraphics(), "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&setFlags, CoreGraphics(), "CGEventSetFlags")
	purego.RegisterLibFunc(&post, CoreGraphics(), "CGEventPost")
	for _, down := range []bool{true, false} {
		ev := create(0, code, down)
		setFlags(ev, flags)
		post(0, ev) // kCGHIDEventTap
		CFRelease(ev)
	}
	return true
}

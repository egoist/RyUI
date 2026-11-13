//go:build windows

package windows

import (
	"encoding/binary"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/sdk/hotkey"
	"github.com/egoist/asuka/ui"
)

// hotkeyArgs is hotkey.Register's "register": u32 id, u8 mods, the key.
func hotkeyArgs(id uint32, mods ui.Modifiers, key string) []byte {
	le := binary.LittleEndian
	b := append(le.AppendUint32(nil, id), byte(mods))
	b = le.AppendUint32(b, uint32(len(key)))
	return append(append(b, key...), "a test's"...)
}

// pumpFor runs the thread's messages until ok, or a while.
func pumpFor(ok func() bool) bool {
	var msg Msg
	for i := 0; !ok() && i < 2000; i++ {
		for PeekMessage(&msg) {
			TranslateMessage(&msg)
			DispatchMessage(&msg)
		}
		if !ok() {
			time.Sleep(time.Millisecond)
		}
	}
	return ok()
}

// A hot key registers with Windows; WM_HOTKEY (posted, as the system
// posts it, and from a key press SendInput makes) answers "next";
// unregistered, it doesn't. The same keys twice are taken; keys Windows
// hasn't fail.
func TestHotkeyModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		m := h.moreModule("hotkey")
		call := func(name string, args []byte) (out []byte, err error) {
			m[name](args, func(b []byte, e error) { out, err = b, e })
			return
		}
		const id = 7001
		keys := ui.Primary | ui.Shift | ui.Alt
		if _, err := call("register", hotkeyArgs(id, keys, "f11")); err != nil {
			fatalf("register: %v", err)
		}
		var win int
		for w, i := range hotkeys.ids {
			if i == id {
				win = w
			}
		}
		if win == 0 {
			fatalf("not registered: %v", hotkeys.ids)
		}
		if _, err := call("register", hotkeyArgs(id+1, keys, "f11")); err == nil || err.Error() != "taken" {
			fatalf("the same keys again: %v", err)
		}
		if _, err := call("register", hotkeyArgs(id+2, ui.Primary, "nope")); err == nil || err.Error() != "no key" {
			fatalf("a key Windows hasn't: %v", err)
		}
		var got []byte
		answered := false
		next := func() {
			answered = false
			m["next"](nil, func(b []byte, e error) { got, answered = b, e == nil })
		}
		next()
		PostMessage(hotkeys.win, WM_HOTKEY, uintptr(win), 0)
		if !pumpFor(func() bool { return answered }) || len(got) != 4 || binary.LittleEndian.Uint32(got) != id {
			fatalf("next after WM_HOTKEY: %v %x", answered, got)
		}
		// A real press: Ctrl+Shift+Alt+F11 from SendInput.
		next()
		if err := SendKeys(VK_CONTROL, VK_SHIFT, VK_MENU, VK_F1+10); err != nil {
			t.Logf("SendInput: %v (no interactive desktop?)", err)
		} else if !pumpFor(func() bool { return answered }) {
			fatalf("no WM_HOTKEY from the key press")
		}
		call("stop", nil)
		call("unregister", binary.LittleEndian.AppendUint32(nil, id))
		if len(hotkeys.ids) != 0 {
			fatalf("still registered: %v", hotkeys.ids)
		}
		// The keys are free again.
		if _, err := call("register", hotkeyArgs(id+3, keys, "f11")); err != nil {
			fatalf("after unregister: %v", err)
		}
		call("unregister", binary.LittleEndian.AppendUint32(nil, id+3))
	})
}

// Through the package: Register in a component; a press runs OnPress;
// disposing it lets the keys go.
func TestHotkeyPackage(t *testing.T) {
	presses := 0
	var k *hotkey.Hotkey
	h, r := mount(t, func() ui.Node {
		k = hotkey.Register(hotkey.Props{
			Shortcut: ui.Val(ui.Cmd("f10").With(ui.Shift | ui.Alt)),
			OnPress:  func() { presses++ },
		})
		return ui.StaticText("x")
	})
	onMain(t, func() {
		if !pumpFor(func() bool { h.runPosted(); return k.Status() == hotkey.Active && hotkeys.waiting != nil }) {
			fatalf("status %v, %v", k.Status(), k.Err())
		}
		var win int
		for w := range hotkeys.ids {
			win = w
		}
		PostMessage(hotkeys.win, WM_HOTKEY, uintptr(win), 0)
		if !pumpFor(func() bool { h.runPosted(); return presses == 1 }) {
			fatalf("%d presses", presses)
		}
		h.enter(r.Dispose)
		h.runPosted()
		if len(hotkeys.ids) != 0 || k.Status() != hotkey.Off {
			fatalf("after dispose: %v, %v", hotkeys.ids, k.Status())
		}
	})
}

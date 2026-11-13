//go:build linux && !android

package linux

import (
	"encoding/binary"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/sdk/hotkey"
	"github.com/egoist/asuka/ui"
)

// hotkeyArgs is hotkey.Register's "register": u32 id, u8 mods, the key,
// the description.
func hotkeyArgs(id uint32, mods ui.Modifiers, key, desc string) []byte {
	b := append(le.AppendUint32(nil, id), byte(mods))
	b = le.AppendUint32(b, uint32(len(key)))
	return append(append(b, key...), desc...)
}

// resetHotkeys forgets how shortcuts were registered, for a test of one
// way.
func resetHotkeys(t *testing.T, portal bool) {
	onMain(func() {
		hotkeys.way, hotkeys.regs, hotkeys.pressed, hotkeys.waiting = 0, nil, nil, nil
		hotkeys.portal = hotkeyPortal{activated: hotkeys.portal.activated}
		hotkeyPortalOnly = portal
	})
	t.Cleanup(func() { onMain(func() { hotkeyPortalOnly = false; hotkeys.way = 0 }) })
}

// poll is a waiting "next": the ids it was answered with, once it is.
type poll struct {
	ids  []uint32
	err  error
	done bool
}

func startPoll(h *Host) *poll {
	p := &poll{}
	onMain(func() {
		h.module("hotkey")["next"](nil, func(b []byte, err error) {
			for ; len(b) >= 4; b = b[4:] {
				p.ids = append(p.ids, binary.LittleEndian.Uint32(b))
			}
			p.err, p.done = err, true
		})
	})
	return p
}

func (p *poll) answered() bool {
	var d bool
	onMain(func() { d = p.done })
	return d
}

// Under X11 (Xvfb): the keys are grabbed on the root window; a press
// (xdotool's, through XTest, as a keyboard's) answers "next", once
// however long the key is held; another client's grab of the same keys
// makes them taken; unregistered, a press doesn't come.
func TestHotkeyX11(t *testing.T) {
	if !IsX11(DisplayGetDefault()) {
		t.Skip("not on X11")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool to press keys")
	}
	h, _ := mount(t, emptyRoot)
	resetHotkeys(t, false)
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(1, ui.Primary|ui.Shift, "f11", "")); err != nil {
		t.Fatalf("register: %v", err)
	}
	p := startPoll(h)
	if err := exec.Command("xdotool", "key", "ctrl+shift+F11").Run(); err != nil {
		t.Fatal(err)
	}
	settle(t, h, func() bool { return p.done })
	if p.err != nil || len(p.ids) != 1 || p.ids[0] != 1 {
		t.Fatalf("next: %v %v", p.ids, p.err)
	}
	// Held down: the server repeats it; it's pressed once.
	p = startPoll(h)
	exec.Command("xdotool", "keydown", "ctrl+shift+F11").Run()
	time.Sleep(800 * time.Millisecond)
	exec.Command("xdotool", "keyup", "ctrl+shift+F11").Run()
	settle(t, h, func() bool { return p.done })
	time.Sleep(100 * time.Millisecond)
	var extra []byte
	onMain(func() { extra = hotkeys.pressed })
	if len(p.ids) != 1 || len(extra) != 0 {
		t.Fatalf("held: %v, then %x", p.ids, extra)
	}
	// Caps Lock on: still the shortcut.
	exec.Command("xdotool", "key", "Caps_Lock").Run()
	p = startPoll(h)
	exec.Command("xdotool", "key", "ctrl+shift+F11").Run()
	settle(t, h, func() bool { return p.done })
	exec.Command("xdotool", "key", "Caps_Lock").Run()
	// "+" is Shift and = on a US keyboard.
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(2, ui.Primary, "+", "")); err != nil {
		t.Fatalf("register +: %v", err)
	}
	onMain(func() {
		if r := hotkeys.regs[2]; r == nil || r.xmods != xControlMask|xShiftMask {
			fatalf(t, "+: %+v", r)
		}
	})
	callModule(t, h, "hotkey", "unregister", le.AppendUint32(nil, 2))
	// Another client has Ctrl+Alt+F10: taken.
	other := otherXClient(t)
	other.grab(t, "F10", xControlMask|xMod1Mask)
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(3, ui.Primary|ui.Alt, "f10", "")); err == nil || err.Error() != "taken" {
		t.Fatalf("keys another client has: %v", err)
	}
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(4, ui.Primary, "nope", "")); err == nil || err.Error() != "no key" {
		t.Fatalf("no such key: %v", err)
	}
	// Unregistered: the key isn't grabbed.
	callModule(t, h, "hotkey", "unregister", le.AppendUint32(nil, 1))
	p = startPoll(h)
	exec.Command("xdotool", "key", "ctrl+shift+F11").Run()
	time.Sleep(200 * time.Millisecond)
	if p.answered() {
		t.Fatalf("a press after unregister: %v", p.ids)
	}
	callModule(t, h, "hotkey", "stop", nil)
	if !p.answered() || p.err == nil || p.err.Error() != "stopped" {
		t.Fatalf("stop: %v", p.err)
	}
}

// xClient is a connection of the test's own to the X server: another
// app.
type xClient struct {
	dpy               Ptr
	open              func(name *byte) Ptr
	grabKey           func(dpy Ptr, keycode int32, mods uint32, window uint64, ownerEvents bool, pointerMode, keyboardMode int32) int32
	sync              func(dpy Ptr, discard bool) int32
	close             func(dpy Ptr) int32
	keysym            func(name string) uint64
	keycode           func(dpy Ptr, keysym uint64) uint8
	defaultRootWindow func(dpy Ptr) uint64
}

func otherXClient(t *testing.T) *xClient {
	lib, err := purego.Dlopen("libX11.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Skip(err)
	}
	c := &xClient{}
	purego.RegisterLibFunc(&c.open, lib, "XOpenDisplay")
	purego.RegisterLibFunc(&c.grabKey, lib, "XGrabKey")
	purego.RegisterLibFunc(&c.sync, lib, "XSync")
	purego.RegisterLibFunc(&c.close, lib, "XCloseDisplay")
	purego.RegisterLibFunc(&c.keysym, lib, "XStringToKeysym")
	purego.RegisterLibFunc(&c.keycode, lib, "XKeysymToKeycode")
	purego.RegisterLibFunc(&c.defaultRootWindow, lib, "XDefaultRootWindow")
	if c.dpy = c.open(nil); c.dpy == 0 {
		t.Skip("no second X connection")
	}
	t.Cleanup(func() { c.close(c.dpy) })
	return c
}

func (c *xClient) grab(t *testing.T, key string, mods uint32) {
	kc := c.keycode(c.dpy, c.keysym(key))
	for _, lock := range xLocks {
		c.grabKey(c.dpy, int32(kc), mods|lock, c.defaultRootWindow(c.dpy), false, 1, 1)
	}
	c.sync(c.dpy, false)
}

// Through the portal (a fake one, on the session bus): the shortcuts
// are bound in a session with their descriptions and preferred
// triggers, bound again in a new one as they change; Activated is a
// press; declined, the new one isn't allowed and the others stay bound.
// Without the portal, global shortcuts aren't available.
func TestHotkeyPortal(t *testing.T) {
	if exec.Command("python3", "-c", "from gi.repository import Gio").Run() != nil {
		t.Skip("no python3-gi for the test's portal")
	}
	h, _ := mount(t, emptyRoot)
	resetHotkeys(t, true)
	// No GlobalShortcuts (xdg-desktop-portal-gtk's portal has none, or
	// none runs): not available.
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(1, ui.Primary, "k", "")); err == nil || err.Error() != "not available" {
		t.Fatalf("without the portal: %v", err)
	}
	server := exec.Command("python3", "testdata/globalshortcuts.py")
	in, _ := server.StdinPipe()
	out, _ := server.StdoutPipe()
	if err := server.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { in.Close(); server.Wait() }()
	ready := make([]byte, 6)
	if _, err := io.ReadFull(out, ready); err != nil || string(ready) != "ready\n" {
		t.Fatalf("the portal: %q %v", ready, err)
	}
	resetHotkeys(t, true)
	test := func(method string, params func() Ptr) any { return portalTestCall(t, method, params) }
	bound := func() []string {
		v, _ := test("Bound", nil).([]any)
		var out []string
		sessions := map[string]bool{}
		for _, x := range v[0].([]any) {
			f := x.([]any)
			sessions[f[0].(string)] = true
			out = append(out, f[1].(string)+"="+f[2].(string))
		}
		if len(sessions) > 1 {
			t.Fatalf("%d sessions bound", len(sessions))
		}
		return out
	}
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(1, ui.Primary|ui.Alt, "k", "Show the launcher")); err != nil {
		t.Fatalf("register: %v", err)
	}
	if b := strings.Join(bound(), ","); b != "CTRL+ALT+k=CTRL+ALT+k|Show the launcher" {
		t.Fatalf("bound: %s", b)
	}
	p := startPoll(h)
	test("Press", func() Ptr { return VTuple(VStr("CTRL+ALT+k")) })
	settle(t, h, func() bool { return p.done })
	if p.err != nil || len(p.ids) != 1 || p.ids[0] != 1 {
		t.Fatalf("next: %v %v", p.ids, p.err)
	}
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(2, ui.Shift, "f9", "")); err != nil {
		t.Fatalf("register 2: %v", err)
	}
	if b := strings.Join(bound(), ","); b != "CTRL+ALT+k=CTRL+ALT+k|Show the launcher,SHIFT+F9=SHIFT+F9|SHIFT+F9" {
		t.Fatalf("bound again: %s", b)
	}
	p = startPoll(h)
	test("Press", func() Ptr { return VTuple(VStr("CTRL+ALT+k")) })
	settle(t, h, func() bool { return p.done })
	if len(p.ids) != 1 || p.ids[0] != 1 {
		t.Fatalf("in the new session: %v %v", p.ids, p.err)
	}
	// Pressed while no "next" waits: kept, with its activation token,
	// for the next.
	test("Press", func() Ptr { return VTuple(VStr("SHIFT+F9")) })
	settle(t, h, func() bool { return len(hotkeys.pressed) == 4 })
	onMain(func() {
		if hotkeys.token != "token-SHIFT+F9" {
			fatalf(t, "the activation token: %q", hotkeys.token)
		}
	})
	p = startPoll(h)
	settle(t, h, func() bool { return p.done })
	var token string
	onMain(func() { token = hotkeys.token })
	if len(p.ids) != 1 || p.ids[0] != 2 || token != "" {
		t.Fatalf("kept: %v %q", p.ids, token)
	}
	// The user declines a third: it isn't allowed; the two stay bound.
	test("Decline", nil)
	if _, err := callModule(t, h, "hotkey", "register", hotkeyArgs(3, ui.Primary, "j", "")); err == nil || err.Error() != "not allowed" {
		t.Fatalf("declined: %v", err)
	}
	settle(t, h, func() bool { return hotkeys.portal.session != "" && !hotkeys.portal.busy })
	if b := strings.Join(bound(), ","); b != "CTRL+ALT+k=CTRL+ALT+k|Show the launcher,SHIFT+F9=SHIFT+F9|SHIFT+F9" {
		t.Fatalf("after declining: %s", b)
	}
	// Unregistered: bound without it; the last: the session closes.
	callModule(t, h, "hotkey", "unregister", le.AppendUint32(nil, 1))
	settle(t, h, func() bool { return hotkeys.portal.session != "" && !hotkeys.portal.busy })
	if b := strings.Join(bound(), ","); b != "SHIFT+F9=SHIFT+F9|SHIFT+F9" {
		t.Fatalf("after unregister: %s", b)
	}
	callModule(t, h, "hotkey", "unregister", le.AppendUint32(nil, 2))
	settle(t, h, func() bool { return hotkeys.portal.session == "" })
	if b := bound(); len(b) != 0 {
		t.Fatalf("after the last: %v", b)
	}
}

// portalTestCall calls the test portal's own methods.
func portalTestCall(t *testing.T, method string, params func() Ptr) any {
	t.Helper()
	var r struct {
		v    any
		err  error
		done bool
	}
	onMain(func() {
		conn, err := Bus(BusSession)
		if err != nil {
			fatalf(t, "%v", err)
		}
		var p Ptr
		if params != nil {
			p = params()
		}
		DBusCall(conn, portalDest, portalPath, "dev.asuka.Test", method, p, -1, func(reply Ptr, err error) {
			r.v, r.err, r.done = VariantGo(reply), err, true
		})
	})
	for i := 0; i < 5000; i++ {
		var d bool
		onMain(func() { d = r.done })
		if d {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !r.done || r.err != nil {
		t.Fatalf("%s: %v", method, r.err)
	}
	return r.v
}

// Through the package: Register in a component; a press runs OnPress;
// disposing it lets the keys go.
func TestHotkeyPackage(t *testing.T) {
	if !IsX11(DisplayGetDefault()) {
		t.Skip("not on X11")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool to press keys")
	}
	resetHotkeys(t, false)
	presses := 0
	var k *hotkey.Hotkey
	h, r := mount(t, func() ui.Node {
		k = hotkey.Register(hotkey.Props{
			Shortcut: ui.Val(ui.Cmd("j").With(ui.Alt)),
			OnPress:  func() { presses++ },
		})
		return ui.StaticText("x")
	})
	settle(t, h, func() bool { return k.Status() == hotkey.Active && hotkeys.waiting != nil })
	exec.Command("xdotool", "key", "ctrl+alt+j").Run()
	settle(t, h, func() bool { return presses == 1 })
	onMain(func() { h.enter(r.Dispose) })
	settle(t, h, func() bool { return len(hotkeys.regs) == 0 && k.Status() == hotkey.Off })
}

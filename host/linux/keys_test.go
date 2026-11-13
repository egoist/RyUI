//go:build linux && !android

package linux

import (
	"os/exec"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Keys are pressed as a keyboard's are: xdotool through XTest, into the
// X window with the keyboard's focus, so they come through GTK's event
// propagation, its controllers and its input method as a user's.

// keyboard gives w's X window the keyboard's focus and returns a func
// pressing keys there (xdotool's names: Up, ctrl+k).
func keyboard(t *testing.T, w *win) func(keys ...string) {
	t.Helper()
	if !IsX11(DisplayGetDefault()) {
		t.Skip("not on X11")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool to press keys")
	}
	var xid uintptr
	settle(t, theHost, func() bool { return WidgetGetMapped(w.window) })
	onMain(func() { xid = Call(Sym("gdk_x11_surface_get_xid"), NativeGetSurface(w.window)) })
	xdo := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
			t.Fatalf("xdotool %v: %v %s", args, err, out)
		}
	}
	xdo("windowfocus", "--sync", strconv.Itoa(int(xid)))
	settle(t, theHost, func() bool { return WindowIsActive(w.window) })
	return func(keys ...string) {
		t.Helper()
		xdo(append([]string{"key", "--clearmodifiers"}, keys...)...)
	}
}

// log is what the app did, from the main thread, read from the test's.
type keyLog struct {
	mu  sync.Mutex
	got []string
}

func (l *keyLog) add(s string) func() {
	return func() { l.mu.Lock(); l.got = append(l.got, s); l.mu.Unlock() }
}

func (l *keyLog) is(want ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Equal(l.got, want)
}

// waitLog waits until log is want, failing with what it is otherwise.
func waitLog(t *testing.T, h *Host, l *keyLog, want ...string) {
	t.Helper()
	for i := 0; i < 400; i++ {
		var ok bool
		onMain(func() { ok = l.is(want...) })
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t.Fatalf("got %q, want %q", l.got, want)
}

// A focused entry inside a view with Keys: the view's Up and Down take
// the keys from the entry (its cursor stays), Enter its activate
// (OnSubmit), Ctrl+K a menu item's accelerator; the entry's own Escape
// is its own; a character nothing declares types into it, Shift+Down
// isn't Down, and the menu's Ctrl+J is still the menu's. A held Down
// repeats.
func TestKeysAroundField(t *testing.T) {
	var log keyLog
	value := signal.New("hello")
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(ui.Val([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{
				{Title: "Kill", Shortcut: ui.Cmd("k"), OnPress: log.add("menu k")},
				{Title: "Join", Shortcut: ui.Cmd("j"), OnPress: log.add("menu j")},
			}}})),
			ui.View(ui.ViewProps{Keys: ui.KeyMap{
				{Key: "up"}:                log.add("up"),
				{Key: "down"}:              log.add("down"),
				{Key: "enter"}:             log.add("enter"),
				ui.Cmd("k"):                log.add("view k"),
				{Key: "1", Mods: ui.Shift}: log.add("shift 1"),
			}},
				ui.TextField(ui.TextFieldProps{Value: value, OnSubmit: log.add("submit"), Keys: ui.KeyMap{
					{Key: "escape"}: log.add("field escape"),
				}}),
			),
		)
	})
	press := keyboard(t, h.main)
	var e Ptr
	onMain(func() {
		f := find(h, ops.KindTextField)[0]
		e = f.field.editable
		WidgetGrabFocus(e)
		EditableSetPosition(e, 2)
	})
	settle(t, h, func() bool { return WidgetHasFocus(EditableGetDelegate(e)) && len(h.appItems) == 2 })
	press("Up", "Down", "Down", "Return", "Escape", "ctrl+k", "shift+1")
	waitLog(t, h, &log, "up", "down", "down", "enter", "field escape", "view k", "shift 1")
	onMain(func() {
		if p := EditableGetPosition(e); p != 2 || EditableGetText(e) != "hello" {
			fatalf(t, "the entry moved to %d, has %q", p, EditableGetText(e))
		}
	})
	// What no Keys have: the entry's, the menu's.
	press("x", "shift+Down", "ctrl+j")
	settle(t, h, func() bool { return value.Peek() == "hexllo" })
	waitLog(t, h, &log, "up", "down", "down", "enter", "field escape", "view k", "shift 1", "menu j")
	// Held: each repeat.
	exec.Command("xdotool", "keydown", "Down").Run()
	time.Sleep(900 * time.Millisecond)
	exec.Command("xdotool", "keyup", "Down").Run()
	settle(t, h, func() bool {
		log.mu.Lock()
		defer log.mu.Unlock()
		return len(log.got) >= 8+3
	})
}

// UseKeys: the window's keys, reached with nothing focused; a Window's
// own are its, the main window's not reached from it, and come before
// the menus' accelerators there too (a Window's window gets the
// application, and its accelerators, as it opens).
func TestUseKeys(t *testing.T) {
	var log keyLog
	open := signal.New(false)
	h, r := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{
			{Key: "space"}:  log.add("main space"),
			{Key: "escape"}: log.add("main escape"),
			ui.Cmd("e"):     log.add("main ctrl e"),
		})
		return ui.Column(ui.Style{},
			ui.MenuBar(ui.Val([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{
				{Title: "Kill", Shortcut: ui.Cmd("k"), OnPress: log.add("menu k")},
			}}})),
			ui.StaticText("nothing to focus"),
			ui.Window(ui.WindowProps{Open: open, Width: 300, Height: 200, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{
					{Key: "escape"}: log.add("window escape"),
					ui.Cmd("k"):     log.add("window k"),
				})
				return ui.StaticText("in the window")
			}}),
		)
	})
	press := keyboard(t, h.main)
	onMain(func() {
		if f := WindowGetFocus(h.main.window); f != 0 && h.focusNode(h.main, h.main.window, f, h.root) != h.root {
			fatalf(t, "something has the focus")
		}
	})
	press("space", "ctrl+e", "Escape", "a")
	waitLog(t, h, &log, "main space", "main ctrl e", "main escape")
	onMain(func() { h.enter(func() { open.Set(true); r.Flush() }) })
	w := theWindow(t, h)
	press = keyboard(t, w)
	press("Escape", "space", "ctrl+k")
	waitLog(t, h, &log, "main space", "main ctrl e", "main escape", "window escape", "window k")
	press = keyboard(t, h.main)
	press("ctrl+k")
	waitLog(t, h, &log, "main space", "main ctrl e", "main escape", "window escape", "window k", "menu k")
}

// A key goes on from a node the core leaves without a parent where it
// shows: a list's row (a cell) to its list and the views around it; a
// sheet's node to the window it covers, whose UseKeys (a sheet's
// content's UseKeys too) come after the content's keys; a sheet over a
// Window to that Window's, never the main window's.
func TestKeysSheetListRow(t *testing.T) {
	var log keyLog
	sheet, win, winSheet := signal.New(false), signal.New(false), signal.New(false)
	h, r := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "f2"}: log.add("main f2"), {Key: "f3"}: log.add("main f3")})
		return ui.Column(ui.Style{},
			ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(200)}, Keys: ui.KeyMap{{Key: "f3"}: log.add("around f3")}},
				ui.List(ui.ListProps{}, signal.Static([]int{1, 2}), func(i int) int { return i },
					func(signal.Accessor[int], signal.Accessor[int]) ui.Node {
						return ui.Row(ui.Style{Height: ui.Pt(44)}, ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
					})),
			ui.Sheet(ui.SheetProps{Open: sheet, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{{Key: "f4"}: log.add("sheet usekeys")})
				return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "f2"}: log.add("sheet f2")}},
					ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
			}}),
			ui.Window(ui.WindowProps{Open: win, Width: 300, Height: 200, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{{Key: "f5"}: log.add("window f5")})
				return ui.Sheet(ui.SheetProps{Open: winSheet, Content: func() ui.Node {
					return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
				}})
			}}),
		)
	})
	// focusIn focuses a field under a node of kind k (a cell's: k 0).
	focusIn := func(k ops.Kind) {
		t.Helper()
		settle(t, h, func() bool {
			for _, f := range find(h, ops.KindTextField) {
				top, inCell := f, false
				for top.parent != nil {
					if _, ok := top.ext.(*cell); ok {
						inCell = true
					}
					top = top.parent
				}
				if (k == 0 && inCell || k != 0 && top.kind == k) && WidgetGetMapped(f.widget) {
					WidgetGrabFocus(f.field.editable)
					return WidgetHasFocus(EditableGetDelegate(f.field.editable))
				}
			}
			return false
		})
	}
	press := keyboard(t, h.main)
	focusIn(0)
	press("F3", "F2")
	waitLog(t, h, &log, "around f3", "main f2")
	onMain(func() { h.enter(func() { sheet.Set(true); r.Flush() }) })
	focusIn(ops.KindSheet)
	press("F2", "F3", "F4")
	want := []string{"around f3", "main f2", "sheet f2", "main f3", "sheet usekeys"}
	waitLog(t, h, &log, want...)
	onMain(func() { h.enter(func() { sheet.Set(false); win.Set(true); r.Flush() }) })
	w := theWindow(t, h)
	press = keyboard(t, w)
	onMain(func() { h.enter(func() { winSheet.Set(true); r.Flush() }) })
	focusIn(ops.KindSheet)
	press("F2", "F3", "F5")
	waitLog(t, h, &log, append(want, "window f5")...)
}

// A sheet and an alert go on the window they're declared in (their
// Window prop), whichever window is in use: a sheet opened in the same
// batch as its Window, before that Window is in use, on that Window, its
// keys on to that Window's UseKeys, never the main window's; a sheet of
// the main window's, with a Window in use, on the main window; an alert
// from a button in a Window, on that Window.
func TestSheetAlertOnDeclaredWindow(t *testing.T) {
	var log keyLog
	win, mainSheet := signal.New(false), signal.New(false)
	h, r := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "f2"}: log.add("main f2")})
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Sheet(ui.SheetProps{Open: mainSheet, Content: func() ui.Node { return ui.StaticText("main sheet") }}),
			ui.Window(ui.WindowProps{Open: win, Width: 400, Height: 300, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{{Key: "f5"}: log.add("window f5")})
				return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "f6"}: log.add("window view f6")}},
					ui.Button(ui.ButtonProps{Title: ui.Val("Alert"), OnPress: func() { ui.Alert(ui.AlertProps{Title: "In the window"}) }}),
					ui.Sheet(ui.SheetProps{Open: signal.New(true), Content: func() ui.Node {
						return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
					}}))
			}}),
		)
	})
	keyboard(t, h.main) // the main window in use
	onMain(func() { h.enter(func() { win.Set(true); r.Flush() }) })
	w := theWindow(t, h)
	sheetOn := func(n *node) Ptr { return WidgetGetRoot(n.nav.dialog) }
	var winSheet *node
	settle(t, h, func() bool {
		for _, n := range find(h, ops.KindSheet) {
			if n.nav.dialog != 0 && sheetOn(n) != 0 {
				winSheet = n
				return true
			}
		}
		return false
	})
	onMain(func() {
		if sheetOn(winSheet) != w.window {
			fatalf(t, "the Window's sheet is on %#x, not the Window %#x (main %#x)", sheetOn(winSheet), w.window, h.main.window)
		}
	})
	press := keyboard(t, w)
	settle(t, h, func() bool {
		f := find(h, ops.KindTextField)
		if len(f) == 1 && WidgetGetMapped(f[0].widget) {
			WidgetGrabFocus(f[0].field.editable)
			return WidgetHasFocus(EditableGetDelegate(f[0].field.editable))
		}
		return false
	})
	press("F2", "F5")
	waitLog(t, h, &log, "window f5")

	// The main window's sheet, with the Window in use.
	onMain(func() {
		if h.activeWin != w {
			fatalf(t, "the Window isn't in use")
		}
		h.enter(func() { mainSheet.Set(true); r.Flush() })
	})
	settle(t, h, func() bool {
		for _, n := range find(h, ops.KindSheet) {
			if n != winSheet {
				return n.nav.dialog != 0 && sheetOn(n) == h.main.window
			}
		}
		return false
	})

	// An alert from a button in the Window: on the Window.
	onMain(func() {
		for _, b := range find(h, ops.KindButton) {
			h.emit(b, ops.Event{Kind: ops.EventPress})
		}
	})
	settle(t, h, func() bool {
		a := find(h, ops.KindAlert)
		return len(a) == 1 && a[0].nav.dialog != 0 && sheetOn(a[0]) == w.window
	})
}

// A panel's content declaring Escape takes it before the panel's own
// Escape, which closes it; a popover's likewise, and its window's keys
// (UseKeys) are reached from it.
func TestKeysPanelPopover(t *testing.T) {
	var log keyLog
	open, pop := signal.New(false), signal.New(false)
	h, r := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "f2"}: log.add("main f2")})
		return ui.Column(ui.Style{},
			ui.Popover(ui.PopoverProps{Open: pop, Content: func() ui.Node {
				return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "escape"}: log.add("popover escape")}},
					ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
			}}, ui.StaticText("anchor")),
			ui.Window(ui.WindowProps{Open: open, Panel: true, Width: 300, Height: 100, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{{Key: "escape"}: log.add("panel escape")})
				return ui.StaticText("panel")
			}}),
		)
	})
	press := keyboard(t, h.main)
	onMain(func() { h.enter(func() { pop.Set(true); r.Flush() }) })
	var field *node
	settle(t, h, func() bool {
		fs := find(h, ops.KindTextField)
		if len(fs) == 1 && WidgetGetMapped(fs[0].widget) {
			field = fs[0]
			WidgetGrabFocus(field.field.editable)
			return WidgetHasFocus(EditableGetDelegate(field.field.editable))
		}
		return false
	})
	press("Escape", "F2")
	waitLog(t, h, &log, "popover escape", "main f2")
	onMain(func() {
		if !pop.Peek() || !WidgetGetMapped(field.widget) {
			fatalf(t, "the popover closed")
		}
		h.enter(func() {
			pop.Set(false)
			open.Set(true)
			r.Flush()
		})
	})
	w := theWindow(t, h)
	press = keyboard(t, w)
	press("Escape")
	waitLog(t, h, &log, "popover escape", "main f2", "panel escape")
	time.Sleep(100 * time.Millisecond)
	onMain(func() {
		if !open.Peek() || !WidgetGetVisible(w.window) {
			fatalf(t, "the panel closed")
		}
	})
}

// An input method composing text (GTK's own: Ctrl+Shift+U starts a
// character by its code, its preedit shown) has its keys: a Keys' e isn't
// taken from it; once it's done, e is the Keys'.
func TestKeysComposing(t *testing.T) {
	var log keyLog
	value := signal.New("")
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "e"}: log.add("e")}},
			ui.TextField(ui.TextFieldProps{Value: value}))
	})
	press := keyboard(t, h.main)
	var d Ptr
	onMain(func() {
		e := find(h, ops.KindTextField)[0].field.editable
		WidgetGrabFocus(e)
		d = EditableGetDelegate(e)
	})
	settle(t, h, func() bool { return WidgetHasFocus(d) })
	press("ctrl+shift+u")
	var composes bool
	for i := 0; i < 100 && !composes; i++ {
		time.Sleep(5 * time.Millisecond)
		onMain(func() { composes = composing(d) })
	}
	if !composes {
		t.Skip("the input method doesn't compose with Ctrl+Shift+U here")
	}
	press("e", "9", "space") // é
	settle(t, h, func() bool { return value.Peek() == "é" })
	onMain(func() {
		if composing(d) {
			fatalf(t, "still composing")
		}
	})
	press("e")
	waitLog(t, h, &log, "e")
	if value.Peek() != "é" {
		t.Fatalf("typed %q", value.Peek())
	}
}

// An automation run's key step goes to the Keys first, as a key from the
// keyboard: the focused field's view's Down, and its Ctrl+K before the
// menu item's.
func TestKeysScript(t *testing.T) {
	var log keyLog
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(ui.Val([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{
				{Title: "Kill", Shortcut: ui.Cmd("k"), OnPress: log.add("menu k")},
			}}})),
			ui.View(ui.ViewProps{Keys: ui.KeyMap{
				{Key: "down"}: log.add("down"),
				ui.Cmd("k"):   log.add("view k"),
			}}, ui.TextField(ui.TextFieldProps{Value: signal.New("")})),
		)
	})
	settle(t, h, func() bool { return len(h.appItems) == 1 })
	onMain(func() {
		e := find(h, ops.KindTextField)[0].field.editable
		WidgetGrabFocus(e)
		for _, k := range []string{"down", "ctrl+k"} {
			if err := h.key(k); err != nil {
				fatalf(t, "%s: %v", k, err)
			}
		}
	})
	waitLog(t, h, &log, "down", "view k")
}

//go:build darwin && !ios

package darwin

import (
	"slices"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// AppKit's key codes.
const (
	kcA      = 0x00
	kcD      = 0x02
	kcReturn = 0x24
	kcEscape = 0x35
	kcSpace  = 0x31
	kcUp     = 0x7e
	kcDown   = 0x7d
)

// pressKey puts a key going down in the application's queue, for window
// w, as the window server would, and runs the queue: the key monitor,
// then NSApplication's sendEvent: (the menu bar, the key window).
func pressKey(w ID, code uint16, chars string, flags uint, repeat bool) {
	app := Send(Class("NSApplication"), "sharedApplication")
	unmodified := chars
	ev := MsgKeyEvent(Class("NSEvent"), Sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
		10, Point{}, flags, float64(time.Now().UnixNano())/1e9, SendInt(w, "windowNumber"), 0,
		String(chars), String(unmodified), repeat, code)
	Send(app, "postEvent:atStart:", uintptr(ev), 0)
	drain()
}

// drain runs the application's queue until it's empty.
func drain() {
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for {
		until := Send(Class("NSDate"), "distantPast")
		ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1)
		if ev == 0 {
			return
		}
		Send(app, "sendEvent:", uintptr(ev))
	}
}

// focus makes n's window key and n its first responder.
func focus(n *node) ID {
	w := Send(n.view, "window")
	Send(w, "makeKeyAndOrderFront:", 0)
	Send(w, "makeFirstResponder:", uintptr(n.view))
	return w
}

// Up and Down in a field go to the view around it that declared them,
// not to the field's caret; a character it didn't declare goes into the
// field; held, a key's repeats are taken too. A one-line field (its
// field editor has the focus) and a multiline one (its text view).
func TestKeysAroundFieldDarwin(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		sel := signal.New(0)
		h, _ := mount(t, func() ui.Node {
			return ui.View(ui.ViewProps{Keys: ui.KeyMap{
				{Key: "down"}: func() { sel.Set(sel.Peek() + 1) },
				{Key: "up"}:   func() { sel.Set(sel.Peek() - 1) },
			}}, ui.TextField(ui.TextFieldProps{Value: signal.New("hello"), Multiline: multiline}))
		})
		onMain(func() {
			n := find(h, ops.KindTextField)[0]
			w := focus(n)
			if n.tracking != 0 {
				Send(w, "makeFirstResponder:", uintptr(n.tracking))
			}
			ed := h.editing(n)
			if ed == 0 || Send(w, "firstResponder") != ed {
				fatalf(t, "multiline %v: the field isn't being edited", multiline)
			}
			Send(ed, "setSelectedRange:", 2, 0)
			pressKey(w, kcDown, "\uf701", 0, false)
			pressKey(w, kcDown, "\uf701", 0, true)
			pressKey(w, kcUp, "\uf700", 0, false)
			if sel.Peek() != 1 {
				fatalf(t, "multiline %v: selection %d, want 1", multiline, sel.Peek())
			}
			if r := MsgRange(ed, Sel("selectedRange")); r.Location != 2 || r.Length != 0 {
				fatalf(t, "multiline %v: the caret moved: %+v", multiline, r)
			}
			pressKey(w, kcA, "a", 0, false)
			if got := GoString(Send(ed, "string")); got != "heallo" {
				fatalf(t, "multiline %v: the field has %q", multiline, got)
			}
		})
	}
}

// The nearest first: a field's own Keys, then the view around it's; what
// neither has goes on (Return: Submit).
func TestKeysNearestFirstDarwin(t *testing.T) {
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{
			{Key: "escape"}: rec("outer escape"),
			{Key: "up"}:     rec("outer up"),
		}}, ui.TextField(ui.TextFieldProps{Value: signal.New(""), OnSubmit: rec("submit"), Keys: ui.KeyMap{
			{Key: "up"}:                rec("field up"),
			{Key: "a", Mods: ui.Shift}: rec("field shift a"),
		}}))
	})
	onMain(func() {
		w := focus(find(h, ops.KindTextField)[0])
		pressKey(w, kcUp, "\uf700", 0, false)
		pressKey(w, kcEscape, "\x1b", 0, false)
		pressKey(w, kcA, "A", nsEventModifierShift, false)
		pressKey(w, kcReturn, "\r", 0, false)
		want := []string{"field up", "outer escape", "field shift a", "submit"}
		if !slices.Equal(got, want) {
			fatalf(t, "got %q, want %q", got, want)
		}
	})
}

// A view's ⌘S comes before the menu bar's: in a field inside it, the
// view's runs; in a field elsewhere, the menu's Save.
func TestKeysBeforeMenuBarDarwin(t *testing.T) {
	saved, viewSaved := 0, 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{
				{Title: "File", Items: []ui.MenuItem{{Title: "Save", Shortcut: ui.Cmd("s"), OnPress: func() { saved++ }}}},
			})),
			ui.View(ui.ViewProps{Keys: ui.KeyMap{ui.Cmd("s"): func() { viewSaved++ }}},
				ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("in")})),
			ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("out")}),
		)
	})
	onMain(func() {
		in, out := fieldsInOut(t, h)
		const kcS = 0x01
		w := focus(out)
		pressKey(w, kcS, "s", nsEventModifierCommand, false)
		if saved != 1 || viewSaved != 0 {
			fatalf(t, "out: menu %d, view %d", saved, viewSaved)
		}
		focus(in)
		pressKey(w, kcS, "s", nsEventModifierCommand, false)
		if saved != 1 || viewSaved != 1 {
			fatalf(t, "in: menu %d, view %d", saved, viewSaved)
		}
	})
}

// fieldsInOut are the text fields of placeholder "in" and the other.
func fieldsInOut(t *testing.T, h *Host) (in, out *node) {
	for _, n := range find(h, ops.KindTextField) {
		if n.placeholder == "in" {
			in = n
		} else {
			out = n
		}
	}
	if in == nil || out == nil {
		fatal(t, "no in and out fields")
	}
	return in, out
}

// UseKeys on the main window takes keys when nothing has the focus, and
// last when a field has it.
func TestUseKeysDarwin(t *testing.T) {
	var got []string
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{
			{Key: "space"}: func() { got = append(got, "space") },
			{Key: "down"}:  func() { got = append(got, "down") },
		})
		return ui.Column(ui.Style{Padding: ui.All(10)}, ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
	})
	onMain(func() {
		n := find(h, ops.KindTextField)[0]
		w := focus(n)
		Send(w, "makeFirstResponder:", 0) // the window
		pressKey(w, kcSpace, " ", 0, false)
		focus(n)
		pressKey(w, kcDown, "\uf701", 0, false)
		if !slices.Equal(got, []string{"space", "down"}) {
			fatalf(t, "got %q", got)
		}
	})
}

// A Window's keys are its own: UseKeys in its content, from a field in
// it or with nothing focused; the main window's don't reach it.
func TestKeysInWindowDarwin(t *testing.T) {
	open := signal.New(false)
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("main escape"), {Key: "space"}: rec("main space")})
		return ui.Window(ui.WindowProps{Open: open, Width: 300, Height: 200, Content: func() ui.Node {
			ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("window escape")})
			return ui.Column(ui.Style{Padding: ui.All(10)}, ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
		}})
	})
	onMain(func() {
		post(h, func() { open.Set(true) })
		x := onlyWin(t, h)
		n := find(h, ops.KindTextField)[0]
		focus(n)
		pressKey(x.window, kcEscape, "\x1b", 0, false)
		pressKey(x.window, kcSpace, " ", 0, false)
		if s := GoString(Send(h.editing(n), "string")); s != " " {
			fatalf(t, "the Window's field has %q, want the space", s)
		}
		Send(x.window, "makeFirstResponder:", 0)
		pressKey(x.window, kcEscape, "\x1b", 0, false)
		Send(h.window, "makeKeyAndOrderFront:", 0)
		pressKey(h.window, kcEscape, "\x1b", 0, false)
		if want := []string{"window escape", "window escape", "main escape"}; !slices.Equal(got, want) {
			fatalf(t, "got %q, want %q", got, want)
		}
	})
}

// In a sheet (a window of its own), its content's keys come first, then
// the window it's a sheet of: the main window's UseKeys (UseKeys in the
// sheet's content too), from a field in it or with nothing focused. A
// sheet over a Window goes on to that Window's, never the main window's.
func TestKeysInSheetDarwin(t *testing.T) {
	open, win, winSheet := signal.New(false), signal.New(false), signal.New(false)
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "up"}: rec("main up"), {Key: "down"}: rec("main down")})
		return ui.Column(ui.Style{},
			ui.Sheet(ui.SheetProps{Open: open, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{ui.Cmd("d"): rec("sheet usekeys")})
				return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "down"}: rec("sheet down")}},
					ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
			}}),
			ui.Window(ui.WindowProps{Open: win, Width: 300, Height: 200, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{{Key: "down"}: rec("window down")})
				return ui.Sheet(ui.SheetProps{Open: winSheet, Content: func() ui.Node {
					return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
				}})
			}}),
		)
	})
	onMain(func() {
		Send(h.window, "makeKeyAndOrderFront:", 0)
		post(h, func() { open.Set(true) })
		n := find(h, ops.KindTextField)[0]
		w := focus(n)
		if w == h.window {
			fatal(t, "the field isn't in a sheet")
		}
		pressKey(w, kcDown, "\uf701", 0, true)
		pressKey(w, kcUp, "\uf700", 0, false)
		pressKey(w, kcD, "d", nsEventModifierCommand, false)
		Send(w, "makeFirstResponder:", 0) // the sheet's window
		pressKey(w, kcUp, "\uf700", 0, false)
		if want := []string{"sheet down", "main up", "sheet usekeys", "main up"}; !slices.Equal(got, want) {
			fatalf(t, "got %q, want %q", got, want)
		}
		got = nil
		post(h, func() { open.Set(false); win.Set(true) })
		x := onlyWin(t, h)
		Send(x.window, "makeKeyAndOrderFront:", 0)
		Send(h.window, "orderOut:", 0) // the app isn't active: the Window in use
		post(h, func() { winSheet.Set(true) })
		n = nil
		for _, f := range find(h, ops.KindTextField) {
			if v := Send(f.view, "window"); v != 0 && Send(v, "sheetParent") == x.window {
				n = f
			}
		}
		if n == nil {
			fatal(t, "the field isn't in a sheet of the Window")
		}
		w = focus(n)
		pressKey(w, kcDown, "\uf701", 0, false)
		pressKey(w, kcUp, "\uf700", 0, false) // the main window's: not here
		if want := []string{"window down"}; !slices.Equal(got, want) {
			fatalf(t, "got %q, want %q", got, want)
		}
	})
}

// A popover's content goes on to its anchor's window: the main window's
// UseKeys, after what the content declared.
func TestKeysInPopoverDarwin(t *testing.T) {
	open := signal.New(false)
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "up"}: rec("main up"), {Key: "down"}: rec("main down")})
		return ui.Column(ui.Style{Padding: ui.All(20)},
			ui.Popover(ui.PopoverProps{Open: open, Content: func() ui.Node {
				return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "down"}: rec("popover down")}},
					ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
			}}, ui.Button(ui.ButtonProps{Title: ui.Val("Info")})))
	})
	onMain(func() {
		Send(h.window, "makeKeyAndOrderFront:", 0) // shown, for the popover to show
		post(h, func() { open.Set(true) })
		if pops := find(h, ops.KindPopover); len(pops) != 1 || pops[0].popover.pop == 0 {
			fatal(t, "no NSPopover")
		}
		w := focus(find(h, ops.KindTextField)[0])
		if w == h.window {
			fatal(t, "the field isn't in the popover's window")
		}
		pressKey(w, kcDown, "\uf701", 0, false)
		pressKey(w, kcUp, "\uf700", 0, false)
		Send(w, "makeFirstResponder:", 0)
		pressKey(w, kcUp, "\uf700", 0, false)
		if want := []string{"popover down", "main up", "main up"}; !slices.Equal(got, want) {
			fatalf(t, "got %q, want %q", got, want)
		}
	})
}

// A list's row is a cell of the host's, with no parent in the core's
// tree: a key in it goes on to the list, the views around it, the
// window's.
func TestKeysInListRowDarwin(t *testing.T) {
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "up"}: rec("main up")})
		return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}, Keys: ui.KeyMap{{Key: "down"}: rec("around down")}},
			ui.List(ui.ListProps{}, signal.Static([]int{1, 2, 3}), func(i int) int { return i },
				func(it signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
					return ui.Row(ui.Style{Height: ui.Pt(44)}, ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
				}))
	})
	onMain(func() {
		fields := find(h, ops.KindTextField)
		if len(fields) == 0 {
			fatal(t, "no rows bound")
		}
		w := focus(fields[0])
		pressKey(w, kcDown, "\uf701", 0, false)
		pressKey(w, kcUp, "\uf700", 0, false)
		if want := []string{"around down", "main up"}; !slices.Equal(got, want) {
			fatalf(t, "got %q, want %q", got, want)
		}
	})
}

// While an input method composes text, its keys are its own.
func TestKeysMarkedTextDarwin(t *testing.T) {
	downs := 0
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "down"}: func() { downs++ }}},
			ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
	})
	onMain(func() {
		n := find(h, ops.KindTextField)[0]
		w := focus(n)
		ed := h.editing(n)
		const notFound = 1<<63 - 1 // NSNotFound
		Send(ed, "setMarkedText:selectedRange:replacementRange:", uintptr(String("にほ")), 2, 0, notFound, 0)
		if !SendBool(ed, "hasMarkedText") {
			fatal(t, "no marked text")
		}
		pressKey(w, kcDown, "\uf701", 0, false)
		if downs != 0 {
			fatal(t, "a key of the input method's was taken")
		}
		Send(ed, "unmarkText")
		pressKey(w, kcDown, "\uf701", 0, false)
		if downs != 1 {
			fatalf(t, "downs %d after the text was committed", downs)
		}
	})
}

// In a panel, a declared Escape is the content's; an Escape no one
// declared puts the panel away, as before.
func TestKeysEscapeInPanelDarwin(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Panel: true, FitContent: true, Hidden: true, MinWidth: 400})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	escapes := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "escape"}: func() { escapes++ }}},
				ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("in")})),
			ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("out")}),
		)
	})
	onMain(func() {
		w := h.window
		post(h, func() { ui.MainWindow().Show() })
		in, out := fieldsInOut(t, h)
		focus(in)
		pressKey(w, kcEscape, "\x1b", 0, false)
		if escapes != 1 || !SendBool(w, "isVisible") {
			fatalf(t, "escapes %d, visible %v", escapes, SendBool(w, "isVisible"))
		}
		focus(out)
		pressKey(w, kcEscape, "\x1b", 0, false)
		if escapes != 1 || SendBool(w, "isVisible") {
			fatalf(t, "escapes %d, visible %v: an Escape no one declared left the panel up", escapes, SendBool(w, "isVisible"))
		}
	})
}

// Key names: named keys by their codes, characters as they type without
// modifiers (⇧ is a modifier), lower case.
func TestKeyOfDarwin(t *testing.T) {
	onMain(func() {
		mk := func(code uint16, chars string, flags uint) ID {
			return MsgKeyEvent(Class("NSEvent"), Sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				10, Point{}, flags, 0, 0, 0, String(chars), String(chars), false, code)
		}
		for _, c := range []struct {
			code  uint16
			chars string
			flags uint
			name  string
			mods  uint8
		}{
			{kcReturn, "\r", 0, "enter", 0},
			{0x4c, "\x03", 0, "enter", 0},
			{0x33, "\x7f", 0, "backspace", 0},
			{0x75, "\uf728", 0, "delete", 0},
			{0x30, "\x19", nsEventModifierShift, "tab", 2},
			{0x7a, "\uf704", 0, "f1", 0},
			{kcD, "D", nsEventModifierCommand | nsEventModifierShift, "d", 3},
			{kcA, "a", nsEventModifierControl | nsEventModifierOption, "a", 12},
		} {
			name, mods := keyOf(mk(c.code, c.chars, c.flags))
			if name != c.name || mods != c.mods {
				t.Errorf("key %#x %q: %q %d, want %q %d", c.code, c.chars, name, mods, c.name, c.mods)
			}
		}
	})
}

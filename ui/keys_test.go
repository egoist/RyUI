package ui_test

import (
	"slices"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A search field's Up, Down and Enter go to the view around it, which
// declared them; what it didn't declare goes on to the field.
func TestKeysAroundField(t *testing.T) {
	h := fake.New()
	sel := signal.New(0)
	opened := -1
	r := ui.Mount(h, func() ui.Node {
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{
			{Key: "down"}:  func() { sel.Set(sel.Peek() + 1) },
			{Key: "up"}:    func() { sel.Set(sel.Peek() - 1) },
			{Key: "enter"}: func() { opened = sel.Peek() },
			ui.Cmd("D"):    func() { sel.Set(100) },
		}}, ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
	})
	defer r.Dispose()
	r.Flush()
	field := h.OfKind(ops.KindTextField)[0]
	for _, k := range []string{"down", "down", "up", "down"} {
		if !h.Key(field, k, 0) {
			t.Fatalf("%s wasn't taken", k)
		}
	}
	r.Flush()
	if sel.Peek() != 2 {
		t.Fatalf("selection %d, want 2", sel.Peek())
	}
	h.Key(field, "enter", 0)
	if opened != 2 {
		t.Fatalf("opened %d", opened)
	}
	// A character, any case, with its modifiers exactly.
	if !h.Key(field, "d", 1) || sel.Peek() != 100 {
		t.Fatalf("⌘D: %d", sel.Peek())
	}
	for _, k := range []struct {
		name string
		mods uint8
	}{{"a", 0}, {"down", 2}, {"d", 0}, {"d", 3}, {"escape", 0}} {
		if h.Key(field, k.name, k.mods) {
			t.Fatalf("%s %d was taken", k.name, k.mods)
		}
	}
}

// The nearest view with a key takes it: a field's own Keys, then the
// views around it, then the window's (UseKeys), which also takes keys
// when nothing has the focus.
func TestKeysNearestFirst(t *testing.T) {
	h := fake.New()
	var got []string
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{
			{Key: "escape"}: func() { got = append(got, "window escape") },
			{Key: "space"}:  func() { got = append(got, "window space") },
		})
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{
			{Key: "escape"}: func() { got = append(got, "outer escape") },
			{Key: "tab"}:    func() { got = append(got, "outer tab") },
		}}, ui.TextField(ui.TextFieldProps{Value: signal.New(""), Keys: ui.KeyMap{
			{Key: "tab"}: func() { got = append(got, "field tab") },
		}}))
	})
	defer r.Dispose()
	r.Flush()
	field := h.OfKind(ops.KindTextField)[0]
	h.Key(field, "tab", 0)
	h.Key(field, "escape", 0)
	h.Key(field, "space", 0)
	h.Key(nil, "space", 0) // nothing focused: the window's
	h.Key(nil, "tab", 0)   // nor the views': no one
	want := []string{"field tab", "outer escape", "window space", "window space"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// UseKeys' keys go with their component, and the last one's win.
func TestUseKeysOwners(t *testing.T) {
	h := fake.New()
	show := signal.New(true)
	var got []string
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "left"}: func() { got = append(got, "app") }})
		return ui.Show(show, func() ui.Node {
			ui.UseKeys(ui.KeyMap{
				{Key: "left"}:  func() { got = append(got, "viewer") },
				{Key: "right"}: func() { got = append(got, "viewer right") },
			})
			return ui.StaticText("viewer")
		})
	})
	defer r.Dispose()
	r.Flush()
	h.Key(nil, "left", 0)
	h.Key(nil, "right", 0)
	show.Set(false)
	r.Flush()
	h.Key(nil, "left", 0)
	if h.Key(nil, "right", 0) {
		t.Fatal("right taken after its component went")
	}
	want := []string{"viewer", "viewer right", "app"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A view's keys go with it.
func TestKeysDisposed(t *testing.T) {
	h := fake.New()
	show := signal.New(true)
	n := 0
	r := ui.Mount(h, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "escape"}: func() { n++ }}},
				ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
		})
	})
	defer r.Dispose()
	r.Flush()
	field := h.OfKind(ops.KindTextField)[0]
	h.Key(field, "escape", 0)
	show.Set(false)
	r.Flush()
	if len(h.OfKind(ops.KindTextField)) != 0 || n != 1 {
		t.Fatalf("n %d", n)
	}
}

// Keys inside a Window are the Window's, and UseKeys there is the
// Window's: a key in its content never reaches the main window's.
func TestKeysInWindow(t *testing.T) {
	h := fake.New()
	open := signal.New(true)
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("main"), ui.Cmd("k"): rec("main k")})
		return ui.Window(ui.WindowProps{Open: open, Content: func() ui.Node {
			ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("window")})
			return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
		}})
	})
	defer r.Dispose()
	r.Flush()
	win := h.OfKind(ops.KindWindow)[0]
	field := h.OfKind(ops.KindTextField)[0]
	h.Key(field, "escape", 0)
	h.Key(nil, "escape", 0) // the Window has the keyboard
	h.Key(h.Root(), "escape", 0)
	if h.Key(field, "k", 1) || h.Key(win, "k", 1) {
		t.Fatal("the main window's key taken in a Window")
	}
	if !slices.Equal(got, []string{"window", "window", "main"}) {
		t.Fatalf("got %q", got)
	}
}

// A List's row is a cell the host made, with no parent in the core's
// tree: a key in it goes on to the List, then the views around it and
// the window.
func TestKeysListRow(t *testing.T) {
	h := fake.New()
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("window escape")})
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "down"}: rec("around down")}},
			ui.List(ui.ListProps{}, signal.Static([]int{1, 2}), func(i int) int { return i },
				func(it signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
					return ui.Row(ui.Style{}, ui.TextField(ui.TextFieldProps{Value: signal.New(""), Keys: ui.KeyMap{
						{Key: "enter"}: rec("row enter"),
					}}))
				}))
	})
	defer r.Dispose()
	r.Flush()
	cell := h.NewCell()
	h.Bind(h.OfKind(ops.KindList)[0], 1, cell)
	r.Flush()
	field := h.OfKind(ops.KindTextField)[0]
	for _, k := range []string{"enter", "down", "escape"} {
		if !h.Key(field, k, 0) {
			t.Fatalf("%s wasn't taken in a row", k)
		}
	}
	if !slices.Equal(got, []string{"row enter", "around down", "window escape"}) {
		t.Fatalf("got %q", got)
	}
}

// A sheet's and a popover's node has no parent in the core's tree: a key
// in it goes on to the window it covers, whose UseKeys (UseKeys in the
// sheet's content too: the window it's in) come after what the content
// declared. A sheet presented from a Window's content goes on to that
// Window, never the main window.
func TestKeysSheetPopover(t *testing.T) {
	h := fake.New()
	sheet, pop, win, winSheet := signal.New(true), signal.New(true), signal.New(false), signal.New(true)
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	content := func(name, key string) func() ui.Node {
		return func() ui.Node {
			ui.UseKeys(ui.KeyMap{ui.Cmd(key): rec(name + " usekeys")})
			return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "escape"}: rec(name + " escape")}},
				ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val(name)}))
		}
	}
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{
			{Key: "escape"}: rec("main escape"),
			ui.Cmd("k"):     rec("main k"),
		})
		return ui.Column(ui.Style{},
			ui.Sheet(ui.SheetProps{Open: sheet, Content: content("sheet", "j")}),
			ui.Popover(ui.PopoverProps{Open: pop, Content: content("popover", "l")}, ui.StaticText("anchor")),
			ui.Window(ui.WindowProps{Open: win, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{ui.Cmd("w"): rec("window w")})
				return ui.Sheet(ui.SheetProps{Open: winSheet, Content: content("window sheet", "m")})
			}}),
		)
	})
	defer r.Dispose()
	r.Flush()
	field := func(name string) *fake.View {
		for _, v := range h.OfKind(ops.KindTextField) {
			if v.String(ops.PropTextFieldPlaceholder) == name {
				return v
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	for i, name := range []string{"sheet", "popover"} {
		got = nil
		f := field(name)
		key := []string{"j", "l"}[i]
		node := h.OfKind(ops.KindSheet)[0]
		if name == "popover" {
			node = h.OfKind(ops.KindPopover)[0]
		}
		h.Key(f, "escape", 0)
		h.Key(f, "k", 1)
		h.Key(f, key, 1)
		h.Key(node, "escape", 0) // its window, nothing focused in it
		h.Key(node, "k", 1)
		want := []string{name + " escape", "main k", name + " usekeys", "main escape", "main k"}
		if !slices.Equal(got, want) {
			t.Fatalf("%s: got %q, want %q", name, got, want)
		}
	}
	got = nil
	win.Set(true)
	r.Flush()
	f := field("window sheet")
	h.Key(f, "escape", 0)
	h.Key(f, "w", 1)
	h.Key(f, "m", 1) // its UseKeys: the Window's
	if h.Key(f, "k", 1) || h.Key(f, "j", 1) {
		t.Fatal("a sheet over a Window reached the main window's keys")
	}
	if !slices.Equal(got, []string{"window sheet escape", "window w", "window sheet usekeys"}) {
		t.Fatalf("got %q", got)
	}
}

// A sheet and an alert go on the window they're declared in (their
// Window prop), whichever window is in use: a sheet opened with its
// Window, before it's in use, on that Window, its keys on to that
// Window's; one of the main window's, with a Window in use, on the main
// window. An alert goes on the window of the view whose event shows it.
func TestSheetAlertWindow(t *testing.T) {
	h := fake.New()
	win, mainSheet, pop := signal.New(false), signal.New(false), signal.New(false)
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	alert := func(title string) ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val(title), OnPress: func() { ui.Alert(ui.AlertProps{Title: title}) }})
	}
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("main escape")})
		return ui.Column(ui.Style{},
			alert("main alert"),
			ui.Sheet(ui.SheetProps{Open: mainSheet, Content: func() ui.Node { return ui.StaticText("main sheet") }}),
			ui.Window(ui.WindowProps{Open: win, Content: func() ui.Node {
				ui.UseKeys(ui.KeyMap{{Key: "escape"}: rec("window escape")})
				return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "f1"}: rec("window f1")}},
					alert("window alert"),
					ui.Popover(ui.PopoverProps{Open: pop, Content: func() ui.Node { return alert("popover alert") }}, ui.StaticText("anchor")),
					ui.Sheet(ui.SheetProps{Open: signal.New(true), Content: func() ui.Node {
						// A view of its own (a sheet has one child).
						return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "f1"}: rec("sheet f1")}},
							ui.TextField(ui.TextFieldProps{Value: signal.New("")}), alert("sheet alert"))
					}}))
			}}),
		)
	})
	defer r.Dispose()
	r.Flush()
	h.SetKeyWindow(h.Root())
	win.Set(true) // and its sheet, in the same batch
	r.Flush()
	w := h.OfKind(ops.KindWindow)[0]
	sheet := h.OfKind(ops.KindSheet)[0]
	if h.PresentedOn(sheet) != w {
		t.Fatalf("the Window's sheet is on %v", h.PresentedOn(sheet))
	}
	h.Key(h.OfKind(ops.KindTextField)[0], "escape", 0)
	if !slices.Equal(got, []string{"window escape"}) {
		t.Fatalf("got %q", got)
	}
	// The main window's sheet, with the Window in use.
	h.SetKeyWindow(w)
	mainSheet.Set(true)
	r.Flush()
	for _, s := range h.OfKind(ops.KindSheet) {
		if s != sheet && h.PresentedOn(s) != h.Root() {
			t.Fatalf("the main window's sheet is on %v", h.PresentedOn(s))
		}
	}
	pop.Set(true)
	r.Flush()
	for _, c := range []struct {
		title string
		on    *fake.View
	}{{"main alert", h.Root()}, {"window alert", w}, {"sheet alert", w}, {"popover alert", w}} {
		h.Tap(h.Find(ops.KindButton, c.title)[0])
		r.Flush()
		var a *fake.View
		for _, x := range h.OfKind(ops.KindAlert) {
			if x.String(ops.PropAlertTitle) == c.title {
				a = x
			}
		}
		if a == nil || h.PresentedOn(a) != c.on {
			t.Fatalf("%s: on %v, want %v", c.title, a, c.on)
		}
	}
}

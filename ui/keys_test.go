package ui_test

import (
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
// Window's.
func TestKeysInWindow(t *testing.T) {
	h := fake.New()
	open := signal.New(true)
	var got []string
	r := ui.Mount(h, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: func() { got = append(got, "main") }})
		return ui.Window(ui.WindowProps{Open: open, Content: func() ui.Node {
			ui.UseKeys(ui.KeyMap{{Key: "escape"}: func() { got = append(got, "window") }})
			return ui.StaticText("in the window")
		}})
	})
	defer r.Dispose()
	r.Flush()
	win := h.OfKind(ops.KindWindow)[0]
	h.Key(win, "escape", 0)
	h.Key(nil, "escape", 0)
	if len(got) != 2 || got[0] != "window" || got[1] != "main" {
		t.Fatalf("got %q", got)
	}
}

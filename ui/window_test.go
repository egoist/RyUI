package ui_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

func TestWindow(t *testing.T) {
	open := signal.New(false)
	title := signal.New("Inspector")
	runs, closed := 0, 0
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Window(ui.WindowProps{Open: open, Title: title, Width: 320, Height: 240, MinWidth: 200,
				OnClose: func() { closed++ },
				Content: func() ui.Node {
					signal.Effect(func() { open.Get(); runs++ })
					return ui.Text(signal.Func(func() string {
						return "width " + itoa(int(ui.UseEnvironment().Get().Width))
					}))
				}}),
		)
	})
	if len(h.Presented()) != 0 {
		t.Fatal("open while closed")
	}
	open.Set(true)
	r.Flush()
	ps := h.Presented()
	if len(ps) != 1 || ps[0].Kind != ops.KindWindow {
		t.Fatalf("presented %v", ps)
	}
	w := ps[0]
	if w.String(ops.PropWindowTitle) != "Inspector" || w.Float(ops.PropWindowWidth) != 320 ||
		w.Float(ops.PropWindowHeight) != 240 || w.Float(ops.PropWindowMinWidth) != 200 {
		t.Fatalf("window props %v", w.Props)
	}
	// The host sizes it and reports its environment: the content follows.
	h.Dispatch(ops.Event{ID: w.ID, Kind: ops.EventEnvironment, Env: ops.Environment{Width: 320, Height: 240, Scale: 2, FontScale: 1}})
	if s := w.Text(); s != "width 320" {
		t.Fatalf("the window's content says %q", s)
	}
	title.Set("Inspector — 2")
	r.Flush()
	if w.String(ops.PropWindowTitle) != "Inspector — 2" {
		t.Fatal("the title didn't follow its signal")
	}
	// The user closes it: Open goes false, the content is disposed.
	h.Dismiss(w)
	if open.Peek() || closed != 1 || len(h.OfKind(ops.KindWindow)) != 0 {
		t.Fatalf("open %v closed %d windows %d", open.Peek(), closed, len(h.OfKind(ops.KindWindow)))
	}
	runs = 0
	open.Set(true)
	open.Set(false)
	r.Flush()
	if runs != 1 {
		t.Fatalf("runs %d", runs)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func TestMenuBar(t *testing.T) {
	saved, shown := 0, signal.New(true)
	dirty := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Show(shown, func() ui.Node {
				return ui.MenuBar(signal.Func(func() []ui.MenuBarMenu {
					return []ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{
						{Title: "New", Shortcut: ui.Cmd("n")},
						ui.MenuSeparator,
						{Title: "Save", Shortcut: ui.Cmd("s").With(ui.Shift), Disabled: !dirty.Get(), OnPress: func() { saved++ }},
					}}}
				}))
			}),
		)
	})
	items := h.MenuBar()
	if len(items) != 3 || items[0].Menu != "File" || items[0].Key != "n" || items[0].Mods != byte(ui.Primary) ||
		items[1].Flags != 1 || items[2].Flags != 2 || items[2].Mods != byte(ui.Primary|ui.Shift) {
		t.Fatalf("menus %+v", items)
	}
	h.ChooseMenuItem(2)
	if saved != 1 {
		t.Fatal("the item's OnPress didn't run")
	}
	// It follows its signals, and sends only what changed.
	sent := len(h.Commands(ops.CmdMenuBar))
	dirty.Set(true)
	r.Flush()
	if items := h.MenuBar(); items[2].Flags != 0 || len(h.Commands(ops.CmdMenuBar)) != sent+1 {
		t.Fatalf("after a change %+v", items)
	}
	r.Flush()
	if len(h.Commands(ops.CmdMenuBar)) != sent+1 {
		t.Fatal("sent again without a change")
	}
	// Disposed, the app has no menus of its own.
	shown.Set(false)
	r.Flush()
	if items := h.MenuBar(); len(items) != 0 {
		t.Fatalf("after dispose %+v", items)
	}
}

func TestWindowOptions(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Title: "Notes", Width: 800, Height: 600, MinWidth: 400})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	cmds := h.Commands(ops.CmdWindowOptions)
	if len(cmds) != 1 {
		t.Fatalf("%d WindowOptions", len(cmds))
	}
	b := cmds[0].Value.Bytes
	f := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])) }
	if f(0) != 800 || f(1) != 600 || f(2) != 400 || f(3) != 0 || string(b[16:]) != "Notes" {
		t.Fatalf("args %v", b)
	}
}

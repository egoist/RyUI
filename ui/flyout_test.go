package ui_test

import (
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A flyout is a Window with a Flyout prop: its anchor follows its
// accessor, it's sized to its content (WindowAction Fit, again only as
// the content's size changes), a Modal Window in it is modal to it, and
// the host closing it (lost focus, Escape) closes it as the user does,
// disposing its content.
func TestFlyout(t *testing.T) {
	open := signal.New(false)
	anchor := signal.New(ui.Anchor{Tray: 3})
	rows := signal.New(2)
	confirm := signal.New(false)
	cleanups := 0
	h, r := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{
			Open:       open,
			Flyout:     &ui.Flyout{Anchor: anchor, Placement: ui.PlacementBottom, Gap: 4},
			FitContent: true, MinWidth: 120, MaxWidth: 300, MaxHeight: 400,
			Content: func() ui.Node {
				signal.OnCleanup(func() { cleanups++ })
				return ui.Column(ui.Style{Padding: ui.All(10)},
					ui.Column(ui.StyleFunc(func() ui.Style {
						return ui.Style{Width: ui.Pt(200), Height: ui.Pt(float32(30 * rows.Get()))}
					})),
					ui.Window(ui.WindowProps{Open: confirm, Modal: true, Title: ui.Val("Quit?"), Width: 300, Height: 120,
						Content: func() ui.Node { return ui.StaticText("Quit?") }}),
				)
			},
		})
	})
	open.Set(true)
	r.Flush()
	fl := h.OfKind(ops.KindWindow)
	if len(fl) != 1 {
		t.Fatalf("%d windows", len(fl))
	}
	w := fl[0]
	info := h.Window(w)
	c := info.Config
	want := ops.WindowFlyout | ops.WindowFitContent | ops.WindowFixedSize | ops.WindowSkipTaskbar | ops.WindowNoMinimize | ops.WindowNoMaximize
	if c.Flags != want || c.Parent != h.Root().ID || c.MaxWidth != 300 {
		t.Fatalf("config %+v (flags %b, want %b)", c, c.Flags, want)
	}
	if f, ok := h.Flyout(w); !ok || f != (ops.Flyout{Tray: 3, Placement: ops.PlacementBottom, Gap: 4}) {
		t.Fatalf("flyout %+v %v", f, ok)
	}
	// Sized to its content: 200 + 20 by 2 × 30 + 20, with nothing else.
	if len(info.Fits) != 1 || info.Fits[0] != [2]float32{220, 80} {
		t.Fatalf("fits %v", info.Fits)
	}
	r.Flush()
	if len(info.Fits) != 1 {
		t.Fatalf("a flush that changed nothing sized it again: %v", info.Fits)
	}
	rows.Set(4)
	r.Flush()
	if len(info.Fits) != 2 || info.Fits[1] != [2]float32{220, 140} {
		t.Fatalf("fits %v", info.Fits)
	}
	// Taller than MaxHeight: held to it.
	rows.Set(40)
	r.Flush()
	if got := info.Fits[len(info.Fits)-1]; got != [2]float32{220, 400} {
		t.Fatalf("fit %v", got)
	}
	// The anchor moves: placed again, nothing else sent.
	anchor.Set(ui.Anchor{X: 10, Y: 20, Width: 30, Height: 40})
	r.Flush()
	if f, _ := h.Flyout(w); f.Tray != 0 || f.X != 10 || f.Height != 40 {
		t.Fatalf("flyout after the anchor moved %+v", f)
	}
	// A dialog over it is modal to it.
	confirm.Set(true)
	r.Flush()
	var modal *ops.WindowConfig
	for _, x := range h.OfKind(ops.KindWindow) {
		if x != w {
			modal = &h.Window(x).Config
		}
	}
	if modal == nil || modal.Parent != w.ID || modal.Flags&ops.WindowModal == 0 {
		t.Fatalf("modal %+v", modal)
	}
	// Closed as the user closes it: the dialog with it.
	h.DismissFlyout(w)
	if open.Peek() || cleanups != 1 || len(h.OfKind(ops.KindWindow)) != 0 {
		t.Fatalf("open %v cleanups %d windows %d", open.Peek(), cleanups, len(h.OfKind(ops.KindWindow)))
	}
}

// A panel as the main window: its options, sized to its content from
// the first flush, and click-through switched from its handle.
func TestPanelAndClickThrough(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Panel: true, FitContent: true, Hidden: true, MinWidth: 600, MaxHeight: 420})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	var w *ui.WindowHandle
	results := signal.New(0)
	h, r := mount(t, func() ui.Node {
		w = ui.UseWindow()
		return ui.Column(ui.Style{},
			ui.Column(ui.Style{Height: ui.Pt(56)}),
			ui.Column(ui.StyleFunc(func() ui.Style { return ui.Style{Height: ui.Pt(float32(40 * results.Get()))} })),
		)
	})
	root := h.Root()
	info := h.Window(root)
	c := info.Config
	want := ops.WindowPanel | ops.WindowAlwaysOnTop | ops.WindowSkipTaskbar | ops.WindowNoMinimize | ops.WindowNoMaximize |
		ops.WindowFitContent | ops.WindowFixedSize | ops.WindowHidden
	if c.Flags != want {
		t.Fatalf("flags %b, want %b", c.Flags, want)
	}
	if len(info.Fits) != 1 || info.Fits[0] != [2]float32{600, 56} {
		t.Fatalf("fits %v", info.Fits)
	}
	results.Set(3)
	r.Flush()
	if got := info.Fits[len(info.Fits)-1]; got != [2]float32{600, 176} {
		t.Fatalf("fit %v", got)
	}
	results.Set(30)
	r.Flush()
	if got := info.Fits[len(info.Fits)-1]; got != [2]float32{600, 420} {
		t.Fatalf("fit %v", got)
	}
	h.RunPosted()
	if w.ClickThrough().Get() {
		t.Fatal("click-through from the start")
	}
	w.SetClickThrough(true)
	r.Flush()
	h.RunPosted()
	if !w.ClickThrough().Get() || info.Actions[len(info.Actions)-1] != ops.WindowActionClickThrough {
		t.Fatalf("click-through %v, actions %v", w.ClickThrough().Get(), info.Actions)
	}
	w.SetClickThrough(false)
	r.Flush()
	h.RunPosted()
	if w.ClickThrough().Get() {
		t.Fatal("still click-through")
	}
}

// ClickThrough opens a Window that lets the mouse through.
func TestClickThroughWindow(t *testing.T) {
	open := signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, ClickThrough: true, Transparent: true, AlwaysOnTop: true,
			Content: func() ui.Node { return ui.StaticText("HUD") }})
	})
	w := h.OfKind(ops.KindWindow)[0]
	if s := h.Window(w); s.Config.Flags&ops.WindowClickThrough == 0 || s.State.Flags&ops.WindowClickThroughState == 0 ||
		s.Config.Flags&ops.WindowFitContent != 0 || len(s.Fits) != 0 {
		t.Fatalf("config %+v state %+v", s.Config, s.State)
	}
}

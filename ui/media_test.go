package ui_test

import (
	"testing"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

func TestImageTakesItsNaturalSize(t *testing.T) {
	var loaded [2]float32
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart},
			ui.Image(ui.ImageProps{Source: ui.Val("https://example.com/a.png"), OnLoad: func(w, h float32) { loaded = [2]float32{w, h} }}),
			ui.Image(ui.ImageProps{Source: ui.Val("b.png"), Style: ui.Style{Width: ui.Pt(100)}}),
		)
	})
	imgs := h.OfKind(ops.KindImage)
	a, b := imgs[0], imgs[1]
	if a.String(ops.PropImageSource) != "https://example.com/a.png" || a.Frame[2] != 0 || b.Frame[2] != 100 || b.Frame[3] != 0 {
		t.Fatalf("before loading: %v %v", a.Frame, b.Frame)
	}
	// Natural sizes arrive: no wider than the root (390), aspect kept.
	h.Dispatch(ops.Event{ID: a.ID, Kind: ops.EventLoaded, X: 800, Y: 400})
	h.Dispatch(ops.Event{ID: b.ID, Kind: ops.EventLoaded, X: 200, Y: 100})
	if a.Frame[2] != 390 || a.Frame[3] != 195 || b.Frame[2] != 100 || b.Frame[3] != 50 || loaded != [2]float32{800, 400} {
		t.Fatalf("after loading: %v %v", a.Frame, b.Frame)
	}
}

func TestGestures(t *testing.T) {
	var pans []ui.Gesture
	var swipes []ui.SwipeDirection
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{
			OnPan:   func(g ui.Gesture) { pans = append(pans, g) },
			OnSwipe: func(d ui.SwipeDirection) { swipes = append(swipes, d) },
			Style:   ui.Style{Width: ui.Pt(100), Height: ui.Pt(100)},
		})
	})
	v := h.OfKind(ops.KindView)[1] // a gesture makes the view real
	if !v.Listens[ops.EventPan] || !v.Listens[ops.EventSwipe] || v.Listens[ops.EventPinch] {
		t.Fatal("listens")
	}
	h.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventPan, G: ops.Gesture{State: ui.GestureChanged, TranslationX: 12}})
	h.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventSwipe, Int: int64(ui.SwipeLeft)})
	if len(pans) != 1 || pans[0].TranslationX != 12 || len(swipes) != 1 || swipes[0] != ui.SwipeLeft {
		t.Fatal(pans, swipes)
	}
}

func TestAnimateWrapsWhatItChanges(t *testing.T) {
	wide := signal.New(false)
	other := signal.New(0)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				w := float32(100)
				if wide.Get() {
					w = 200
				}
				return ui.Style{Width: ui.Pt(w), Height: ui.Pt(50), Opacity: map[bool]float32{false: 1, true: 0.5}[wide.Get()]}
			})}),
			ui.Text(signal.Func(func() string { other.Get(); return "x" })),
		)
	})
	h.Reset()
	ui.Animate(func() { wide.Set(true) })
	r.Flush()
	got := ""
	for _, op := range h.Ops {
		switch {
		case op.Code == ops.OpCommand && op.Cmd == ops.CmdAnimateBegin:
			got += "begin "
		case op.Code == ops.OpCommand && op.Cmd == ops.CmdAnimateEnd:
			got += "end "
		case op.Code == ops.OpSetProp:
			got += "prop "
		case op.Code == ops.OpSetFrame:
			got += "frame "
		}
	}
	if got != "begin prop end begin frame end " {
		t.Fatalf("%q\n%s", got, h.Log())
	}
	// A later flush doesn't animate.
	h.Reset()
	wide.Set(false)
	r.Flush()
	if len(h.Commands(ops.CmdAnimateBegin)) != 0 {
		t.Fatal("animated again")
	}
}

func TestNative(t *testing.T) {
	sel := signal.New(1)
	var changed []int64
	h, r := mount(t, func() ui.Node {
		return ui.Native("Segmented", ui.NativeProps{
			Props: map[string]any{"items": "Day,Week", "selected": sel},
			On: map[string]func(ui.NativeEvent){"changed": func(e ui.NativeEvent) {
				changed = append(changed, e.Int("index"))
				sel.Set(int(e.Int("index")))
			}},
		})
	})
	v := h.OfKind(ops.KindNative)[0]
	if v.String(ops.PropNativeType) != "Segmented" || !v.Listens[ops.EventNative] {
		t.Fatal("native view")
	}
	h.Reset()
	sel.Set(0)
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 {
		t.Fatalf("\n%s", h.Log())
	}
	h.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventNative, Bytes: ui.EncodeNativeEventForTest("changed", map[string]any{"index": int64(1)})})
	if len(changed) != 1 || changed[0] != 1 || sel.Peek() != 1 {
		t.Fatal(changed)
	}
}

func TestNativeRejectsOtherTypes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	mount(t, func() ui.Node {
		return ui.Native("X", ui.NativeProps{Props: map[string]any{"bad": []int{1}}})
	})
}

func TestImageAspectRatioOverridesItsOwn(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 12},
			ui.Image(ui.ImageProps{Source: ui.Val("x"), Style: ui.Style{AspectRatio: 16.0 / 9}}),
			ui.StaticText("after"),
		)
	})
	img := h.OfKind(ops.KindImage)[0]
	// Before it loads, the box already has its size, and what follows
	// comes after it.
	if img.Frame[3] != 197 || h.One(ops.KindText, "after").Frame[1] != 20+197+12 {
		t.Fatalf("%v %v", img.Frame, h.One(ops.KindText, "after").Frame)
	}
	h.Dispatch(ops.Event{ID: img.ID, Kind: ops.EventLoaded, X: 1200, Y: 800}) // 3:2
	if img.Frame[3] != 197 {
		t.Fatalf("after loading: %v", img.Frame)
	}
}

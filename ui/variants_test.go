package ui_test

import (
	"testing"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func bg(v interface{ Int(ops.PropID) int64 }) ops.Color { return ops.Color(v.Int(ops.PropBackground)) }

// A View's style variants are laid over its style while it's in their
// states, in order (pressed over hovered), each setting only its fields.
func TestStyleVariants(t *testing.T) {
	presses := 0
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{
			OnPress: func() { presses++ },
			Style: ui.Style{Background: ui.SecondaryBackground,
				Hovered: &ui.Style{Background: ui.SecondaryGroupedBackground, BorderColor: ui.Accent},
				Pressed: &ui.Style{Background: ui.Separator}},
		})
	})
	v := h.OfKind(ops.KindView)[1]
	border := func() ops.Color { return ops.Color(v.Int(ops.PropBorderColor)) }
	if bg(v) != ui.SecondaryBackground || border() != 0 {
		t.Fatalf("at rest: %v %v", bg(v), border())
	}
	h.SetHovered(v, true)
	if bg(v) != ui.SecondaryGroupedBackground || border() != ui.Accent {
		t.Fatalf("hovered: %v %v", bg(v), border())
	}
	h.SetPressed(v, true)
	if bg(v) != ui.Separator || border() != ui.Accent {
		t.Fatalf("pressed and hovered: %v %v", bg(v), border())
	}
	h.SetPressed(v, false)
	h.SetHovered(v, false)
	if bg(v) != ui.SecondaryBackground || border() != ui.Clear && border() != 0 {
		t.Fatalf("left: %v %v", bg(v), border())
	}
	h.Click(v)
	if presses != 1 {
		t.Fatalf("%d presses", presses)
	}
}

// A View tracks only the states its variants name: one with no Pressed
// variant doesn't report its pressed state (and keeps the platform's
// highlight); a variant alone makes a view.
func TestVariantsTrackWhatTheyName(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(nil,
			ui.View(ui.ViewProps{OnPress: func() {}, Style: ui.Style{Hovered: &ui.Style{Opacity: 0.5}}}),
			ui.View(ui.ViewProps{OnPress: func() {}, Style: ui.Style{Padding: ui.All(4)}}),
			ui.View(ui.ViewProps{Style: ui.Style{Pressed: &ui.Style{Opacity: 0.5}}}),
		)
	})
	vs := h.OfKind(ops.KindView)
	if len(vs) != 4 {
		t.Fatal(h.Root().Tree())
	}
	hover, plain, pressed := vs[1], vs[2], vs[3]
	if !hover.Listens[ops.EventHover] || hover.Listens[ops.EventPressed] {
		t.Errorf("hover variant listens %v", hover.Listens)
	}
	if plain.Listens[ops.EventHover] || plain.Listens[ops.EventPressed] {
		t.Errorf("no variants listens %v", plain.Listens)
	}
	if !pressed.Listens[ops.EventPressed] || pressed.Listens[ops.EventHover] {
		t.Errorf("pressed variant listens %v", pressed.Listens)
	}
}

// A StyleFunc's variants work as a Style's: a flattened container whose
// style comes to have one gets a view, to report the state.
func TestStyleFuncVariants(t *testing.T) {
	selectable := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(nil,
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				s := ui.Style{Padding: ui.All(4)}
				if selectable.Get() {
					s.Pressed = &ui.Style{Background: ui.Separator}
				}
				return s
			})}, ui.StaticText("x")),
		)
	})
	if n := len(h.OfKind(ops.KindView)); n != 1 {
		t.Fatalf("%d views, want the root's alone:\n%s", n, h.Root().Tree())
	}
	selectable.Set(true)
	r.Flush()
	vs := h.OfKind(ops.KindView)
	if len(vs) != 2 || !vs[1].Listens[ops.EventPressed] || len(vs[1].Children) != 1 {
		t.Fatalf("%s", h.Root().Tree())
	}
	v := vs[1]
	h.SetPressed(v, true)
	if bg(v) != ui.Separator {
		t.Fatalf("pressed: %v", bg(v))
	}
	h.SetPressed(v, false)
	if bg(v) != 0 {
		t.Fatalf("released: %v", bg(v))
	}
}

// An Interaction is a View's states for other views' styles: its
// children's (a group's hover), its siblings' (a peer's). The View
// tracks a state from when something first reads it.
func TestInteractionGroupAndPeer(t *testing.T) {
	card := ui.NewInteraction()
	peer := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(nil,
			ui.View(ui.ViewProps{OnPress: func() {}, Interaction: card},
				ui.StaticText("Title", ui.Style{Color: ui.Label}.When(card.Hovered, ui.Style{Color: ui.Accent}))),
			ui.Show(peer, func() ui.Node {
				return ui.Text(signal.Func(func() string {
					if card.Pressed() {
						return "down"
					}
					return "up"
				}))
			}),
		)
	})
	v := h.OfKind(ops.KindView)[1]
	title := h.OfKind(ops.KindText)[0]
	if !v.Listens[ops.EventHover] || v.Listens[ops.EventPressed] {
		t.Fatalf("before Pressed is read: %v", v.Listens)
	}
	color := func() ops.Color { return ops.Color(title.Int(ops.PropColor)) }
	h.SetHovered(v, true)
	if color() != ui.Accent {
		t.Fatalf("group hover: %v", color())
	}
	h.SetHovered(v, false)
	if color() != ui.Label {
		t.Fatalf("left: %v", color())
	}
	peer.Set(true) // reads card.Pressed: the view reports it from now on
	r.Flush()
	if !v.Listens[ops.EventPressed] {
		t.Fatalf("after Pressed is read: %v", v.Listens)
	}
	status := h.OfKind(ops.KindText)[1]
	h.SetPressed(v, true)
	if status.String(ops.PropTextText) != "down" {
		t.Fatalf("peer: %q", status.String(ops.PropTextText))
	}
	h.SetPressed(v, false)
	if status.String(ops.PropTextText) != "up" {
		t.Fatalf("peer: %q", status.String(ops.PropTextText))
	}
}

// Unless sets a field in the other state: shown on a row's hover.
func TestUnless(t *testing.T) {
	row := ui.NewInteraction()
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Interaction: row},
			ui.StaticText("Delete", ui.Style{}.Unless(row.Hovered, ui.Style{Hidden: true})))
	})
	v, del := h.OfKind(ops.KindView)[1], h.OfKind(ops.KindText)[0]
	if !del.Bool(ops.PropHidden) {
		t.Fatal("shown at rest")
	}
	h.SetHovered(v, true)
	if del.Bool(ops.PropHidden) {
		t.Fatal("hidden on hover")
	}
}

// Given to several Views, a state is theirs together; a View that goes
// away leaves its states, and one made again joins.
func TestInteractionAcrossViews(t *testing.T) {
	it := &ui.Interaction{} // the zero one is ready
	second := signal.New(true)
	var hovered []bool
	h, r := mount(t, func() ui.Node {
		signal.Effect(func() { hovered = append(hovered, it.Hovered()) })
		return ui.Column(nil,
			ui.View(ui.ViewProps{Interaction: it}),
			ui.Show(second, func() ui.Node { return ui.View(ui.ViewProps{Interaction: it}) }),
		)
	})
	vs := h.OfKind(ops.KindView)
	a, b := vs[1], vs[2]
	h.SetHovered(a, true)
	h.SetHovered(b, true)
	h.SetHovered(a, false)
	if !it.Hovered() {
		t.Fatal("one of them is hovered")
	}
	second.Set(false) // b goes while hovered
	r.Flush()
	if it.Hovered() {
		t.Fatal("stuck hovered after the view went")
	}
	second.Set(true)
	r.Flush()
	b = h.OfKind(ops.KindView)[2]
	if !b.Listens[ops.EventHover] {
		t.Fatalf("the new view listens %v", b.Listens)
	}
	h.SetHovered(b, true)
	if !it.Hovered() {
		t.Fatal("the new view's hover")
	}
	want := []bool{false, true, false, true}
	if len(hovered) != len(want) {
		t.Fatalf("hovered %v, want %v", hovered, want)
	}
	for i := range want {
		if hovered[i] != want[i] {
			t.Fatalf("hovered %v, want %v", hovered, want)
		}
	}
}

// Dragging and DragOver follow a drag: the source's while it's dragged,
// the target's while a drag it takes is over it.
func TestDragVariants(t *testing.T) {
	src := ui.NewInteraction()
	h, _ := mount(t, func() ui.Node {
		return ui.Column(nil,
			ui.View(ui.ViewProps{
				Interaction: src,
				Drag:        &ui.DragSource{Items: func() []ui.DragItem { return []ui.DragItem{{Text: "x"}} }},
				Style:       ui.Style{Dragging: &ui.Style{Opacity: 0.4}},
			}),
			ui.View(ui.ViewProps{
				Drop:  &ui.DropTarget{Accept: []string{"text/plain"}},
				Style: ui.Style{BorderWidth: 2, DragOver: &ui.Style{BorderColor: ui.Accent}},
			}),
		)
	})
	vs := h.OfKind(ops.KindView)
	source, target := vs[1], vs[2]
	h.SetDragPhase(source, ops.DragPhaseBegan)
	if source.Float(ops.PropOpacity) != float64(float32(0.4)) || !src.Dragging() {
		t.Fatalf("dragging: %v", source.Float(ops.PropOpacity))
	}
	h.DragOver(target, ops.GestureStateBegan, 1, 1)
	if ops.Color(target.Int(ops.PropBorderColor)) != ui.Accent {
		t.Fatal("drag over")
	}
	h.DragOver(target, ops.GestureStateCancelled, 1, 1)
	h.SetDragPhase(source, ops.DragPhaseCancelled)
	if ops.Color(target.Int(ops.PropBorderColor)) == ui.Accent || src.Dragging() {
		t.Fatal("drag ended")
	}
}

// Following a state costs no allocation per change.
func TestVariantsDontAllocate(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: func() {}, Style: ui.Style{Background: ui.SecondaryBackground,
			Pressed: &ui.Style{Background: ui.Separator}}})
	})
	v := h.OfKind(ops.KindView)[1]
	h.SetPressed(v, true)
	h.SetPressed(v, false)
	if n := testing.AllocsPerRun(100, func() {
		h.SetPressed(v, true)
		h.SetPressed(v, false)
	}); n != 0 {
		t.Fatalf("%v allocations per press", n)
	}
}

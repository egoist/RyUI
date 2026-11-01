package ui_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// A view's pressed and hover states reach the app as they change, once
// per change, and styles follow them: the view's own (:active, :hover)
// and its children's (group-hover).
func TestPressedAndHoverStates(t *testing.T) {
	pressed, hovered := signal.New(false), signal.New(false)
	var presses int
	var log []string
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{
			OnPress: func() { presses++ },
			OnPressedChange: func(on bool) {
				log = append(log, map[bool]string{true: "pressed", false: "released"}[on])
				pressed.Set(on)
			},
			OnHoverChange: hovered.Set,
			Style: ui.StyleFunc(func() ui.Style {
				switch {
				case pressed.Get():
					return ui.Style{Background: ui.Separator}
				case hovered.Get():
					return ui.Style{Background: ui.SecondaryBackground}
				}
				return ui.Style{}
			}),
		}, ui.Text(ui.Val("Title"), ui.StyleFunc(func() ui.Style {
			if hovered.Get() {
				return ui.Style{Color: ui.Accent}
			}
			return ui.Style{}
		})))
	})
	v := h.OfKind(ops.KindView)[1]
	text := h.OfKind(ops.KindText)[0]
	if !v.Listens[ops.EventPressed] || !v.Listens[ops.EventHover] || v.Listens[ops.EventDragOver] || v.Listens[ops.EventDragging] {
		t.Fatalf("listens %v", v.Listens)
	}
	bg := func() ops.Color { return ops.Color(v.Props[ops.PropBackground].Int) }
	color := func() ops.Color { return ops.Color(text.Props[ops.PropColor].Int) }

	h.SetHovered(v, true)
	if bg() != ui.SecondaryBackground || color() != ui.Accent {
		t.Fatalf("hovered: background %v, text %v", bg(), color())
	}
	h.Click(v)
	if presses != 1 || !slices.Equal(log, []string{"pressed", "released"}) || bg() != ui.SecondaryBackground {
		t.Fatalf("click: %d presses, %v, background %v", presses, log, bg())
	}
	// A repeated report changes nothing.
	h.SetPressed(v, true)
	h.SetPressed(v, true)
	if !slices.Equal(log, []string{"pressed", "released", "pressed"}) || bg() != ui.Separator {
		t.Fatalf("held: %v, background %v", log, bg())
	}
	h.SetPressed(v, false)
	h.SetHovered(v, false)
	if bg() != 0 || color() != 0 {
		t.Fatalf("left: background %v, text %v", bg(), color())
	}
}

// A view with nothing but an interaction state has a view of its own,
// to take the touch.
func TestInteractionMakesAView(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{OnHoverChange: func(bool) {}}, ui.StaticText("x"))
	})
	if v := h.OfKind(ops.KindView); len(v) != 2 || !v[1].Listens[ops.EventHover] || v[1].Listens[ops.EventPressed] {
		t.Fatal(h.Root().Tree())
	}
}

// A view that goes away while pressed, hovered or under a drag leaves
// each state, so what follows it elsewhere doesn't stay stuck.
func TestInteractionLeftWhenTheViewGoes(t *testing.T) {
	shown := signal.New(true)
	var pressed, hovered, over []bool
	h, _ := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			return ui.View(ui.ViewProps{
				OnPressedChange: func(on bool) { pressed = append(pressed, on) },
				OnHoverChange:   func(on bool) { hovered = append(hovered, on) },
				Drop:            &ui.DropTarget{Accept: []string{"*/*"}, OnOverChange: func(on bool) { over = append(over, on) }},
			})
		})
	})
	v := h.OfKind(ops.KindView)[1]
	h.SetPressed(v, true)
	h.SetHovered(v, true)
	h.DragOver(v, ops.GestureStateBegan, 1, 2)
	shown.Set(false)
	if !slices.Equal(pressed, []bool{true, false}) || !slices.Equal(hovered, []bool{true, false}) || !slices.Equal(over, []bool{true, false}) {
		t.Fatalf("pressed %v, hovered %v, over %v", pressed, hovered, over)
	}
	// One that's in none reports nothing.
	shown.Set(true)
	shown.Set(false)
	if len(pressed) != 2 || len(hovered) != 2 || len(over) != 2 {
		t.Fatalf("pressed %v, hovered %v, over %v", pressed, hovered, over)
	}
}

type card struct{ title string }

// A drag within the app carries the app's values: the target that takes
// their type gets them as they were when the drag began, and the source
// hears it was dropped.
func TestDragWithinTheApp(t *testing.T) {
	c := signal.New(&card{"Write tests"})
	var phases []ui.DragPhase
	var overs []bool
	var moves [][2]float32
	var drops []ui.Drop
	h, _ := mount(t, func() ui.Node {
		return ui.Column(nil,
			ui.View(ui.ViewProps{Drag: &ui.DragSource{
				Items: func() []ui.DragItem {
					return []ui.DragItem{{Value: c.Get(), MIME: "application/x-card", Text: c.Get().title}}
				},
				OnPhase: func(p ui.DragPhase) { phases = append(phases, p) },
			}}),
			ui.View(ui.ViewProps{Drop: &ui.DropTarget{
				Accept:       []string{"application/x-card"},
				OnOverChange: func(on bool) { overs = append(overs, on) },
				OnMove:       func(x, y float32) { moves = append(moves, [2]float32{x, y}) },
				OnDrop:       func(d ui.Drop) { drops = append(drops, d) },
			}}),
			ui.View(ui.ViewProps{Drop: &ui.DropTarget{Accept: []string{"image/*"}, OnDrop: func(ui.Drop) { t.Error("an image target took a card") }}}),
		)
	})
	views := h.OfKind(ops.KindView)
	src, target, images := views[1], views[2], views[3]
	if got := src.DragItems(); len(got) != 1 || !got[0].Value || got[0].MIME != "application/x-card" || got[0].Text != "Write tests" || got[0].Kind != ui.SharedText {
		t.Fatalf("drag items %+v", got)
	}
	if !slices.Equal(target.DropAccept(), []string{"application/x-card"}) || !target.Listens[ops.EventDrop] || !src.Listens[ops.EventDragging] {
		t.Fatal("wiring")
	}

	h.Drag(src, images, 5, 5)
	if !slices.Equal(phases, []ui.DragPhase{ui.DragBegan, ui.DragCancelled}) {
		t.Fatalf("dropped on a target that takes none: %v", phases)
	}

	phases = nil
	first := c.Peek()
	h.Dispatch(ops.Event{ID: src.ID, Kind: ops.EventDragging, Int: int64(ui.DragBegan)})
	c.Set(&card{"Changed meanwhile"}) // the drag carries what it began with
	h.DragOver(target, ops.GestureStateBegan, 10, 20)
	h.DragOver(target, ops.GestureStateChanged, 11, 21)
	d := ops.Drop{X: 11, Y: 21, Source: src.ID}
	h.Dispatch(ops.Event{ID: target.ID, Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
	h.DragOver(target, ops.GestureStateCancelled, 11, 21) // after the drop: nothing more
	h.Dispatch(ops.Event{ID: src.ID, Kind: ops.EventDragging, Int: int64(ui.DragDropped)})

	if !slices.Equal(phases, []ui.DragPhase{ui.DragBegan, ui.DragDropped}) || !slices.Equal(overs, []bool{true, false}) {
		t.Fatalf("phases %v, overs %v", phases, overs)
	}
	if !slices.Equal(moves, [][2]float32{{10, 20}, {11, 21}}) {
		t.Fatalf("moves %v", moves)
	}
	if len(drops) != 1 || !drops[0].Local || drops[0].X != 11 || drops[0].Y != 21 || len(drops[0].Items) != 1 {
		t.Fatalf("drops %+v", drops)
	}
	if it := drops[0].Items[0]; it.Value != first || it.Text != "Write tests" || it.MIME != "application/x-card" {
		t.Fatalf("dropped %+v", it)
	}
	if got := src.DragItems(); got[0].Text != "Changed meanwhile" {
		t.Fatalf("the source's items follow its signal: %+v", got)
	}
}

// A drop from another app gets the items its target takes, in order.
func TestDropFromAnotherApp(t *testing.T) {
	var drops []ui.Drop
	var overs []bool
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Drop: &ui.DropTarget{
			Accept:       []string{"image/*", "text/uri-list"},
			OnOverChange: func(on bool) { overs = append(overs, on) },
			OnDrop:       func(d ui.Drop) { drops = append(drops, d) },
		}})
	})
	v := h.OfKind(ops.KindView)[1]
	photo := ops.Item{Kind: ui.SharedImage, Path: "/caches/IMG_1.heic", Name: "IMG_1.heic", MIME: "image/heic", Size: 1200}
	note := ops.Item{Kind: ui.SharedText, Text: "hello", MIME: "text/plain; charset=utf-8", Size: -1}
	link := ops.Item{Kind: ui.SharedLink, Text: "https://ryui.dev", MIME: "text/uri-list", Size: -1}
	h.DropItems(v, 3, 4, true, photo, note, link)
	if !slices.Equal(overs, []bool{true, false}) || len(drops) != 1 {
		t.Fatalf("overs %v, drops %+v", overs, drops)
	}
	d := drops[0]
	if d.Local || !d.Copied || d.X != 3 || d.Y != 4 || len(d.Items) != 2 {
		t.Fatalf("drop %+v", d)
	}
	if a, b := d.Items[0], d.Items[1]; a.Path != photo.Path || a.Kind != ui.SharedImage || a.Size != 1200 || !a.IsFile() || b.Text != link.Text || b.Kind != ui.SharedLink {
		t.Fatalf("items %+v", d.Items)
	}
}

// What a drag carries goes to the host with its kind, path, name and
// type filled in, and again only when it changes.
func TestDragItemsOnTheWire(t *testing.T) {
	path := signal.New("photos/cat.png")
	h, r := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Drag: &ui.DragSource{Items: func() []ui.DragItem {
			return []ui.DragItem{
				{Path: path.Get()},
				{Text: "plain words"},
				{Kind: ui.SharedLink, Text: "https://ryui.dev"},
				{Value: 42},
			}
		}}})
	})
	v := h.OfKind(ops.KindView)[1]
	got := v.DragItems()
	abs, _ := filepath.Abs("photos/cat.png")
	want := []ops.Item{
		{Kind: ui.SharedImage, Path: abs, Name: "cat.png", MIME: "image/png", Size: -1},
		{Kind: ui.SharedText, Text: "plain words", MIME: "text/plain", Size: -1},
		{Kind: ui.SharedLink, Text: "https://ryui.dev", MIME: "text/uri-list", Size: -1},
		{Kind: ui.SharedText, MIME: "application/octet-stream", Size: -1, Value: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	h.Reset()
	path.Set("photos/cat.png") // the same: nothing sent
	path.Set("notes/a.pdf")
	r.Flush()
	if n := h.Count(ops.OpSetProp); n != 1 {
		t.Fatalf("%d SetProps, want 1", n)
	}
	if it := v.DragItems()[0]; it.Kind != ui.SharedFile || it.MIME != "application/pdf" || it.Name != "a.pdf" {
		t.Fatalf("pdf %+v", it)
	}
}

// A view whose drag is under way and goes away: a drop that comes after
// still gets its items.
func TestDropAfterTheSourceWent(t *testing.T) {
	shown := signal.New(true)
	var drops []ui.Drop
	h, _ := mount(t, func() ui.Node {
		return ui.Column(nil,
			ui.Show(shown, func() ui.Node {
				return ui.View(ui.ViewProps{Drag: &ui.DragSource{Items: func() []ui.DragItem { return []ui.DragItem{{Value: "row 3", MIME: "application/x-row"}} }}})
			}),
			ui.View(ui.ViewProps{Drop: &ui.DropTarget{Accept: []string{"application/x-row"}, OnDrop: func(d ui.Drop) { drops = append(drops, d) }}}),
		)
	})
	views := h.OfKind(ops.KindView)
	src, target := views[1], views[2]
	h.Dispatch(ops.Event{ID: src.ID, Kind: ops.EventDragging, Int: int64(ui.DragBegan)})
	shown.Set(false)
	d := ops.Drop{Source: src.ID}
	h.Dispatch(ops.Event{ID: target.ID, Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
	if len(drops) != 1 || len(drops[0].Items) != 1 || drops[0].Items[0].Value != "row 3" {
		t.Fatalf("drops %+v", drops)
	}
}

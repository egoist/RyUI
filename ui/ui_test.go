package ui_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

func mount(t *testing.T, root func() ui.Node) (*fake.Host, *ui.Root) {
	t.Helper()
	h := fake.New()
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	return h, r
}

func counter() ui.Node {
	count := signal.New(0)
	return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Add"),
			OnPress: func() { count.Update(func(n int) int { return n + 1 }) },
		}),
		ui.Show(signal.Func(func() bool { return count.Get() > 2 }),
			func() ui.Node { return ui.StaticText("That's a lot") }),
	)
}

func TestCounter(t *testing.T) {
	h, _ := mount(t, counter)
	if h.Applies != 2 {
		t.Fatalf("mount took %d applies, want 2 (props, then frames)", h.Applies)
	}
	root := h.Root()
	if root == nil || root.Frame != [4]float32{0, 0, 390, 844} {
		t.Fatalf("root %+v", root)
	}
	// The Column is flattened: Text and Button sit in the root view,
	// offset by its padding.
	if got := root.Tree(); got != "View 0,0 390x844\n  Text 16,16 358x20 \"Count: 0\"\n  Button 16,44 358x44 \"Add\"\n" {
		t.Fatalf("tree:\n%s", got)
	}
	btn := h.One(ops.KindButton, "Add")
	h.Tap(btn)
	if got := root.Text(); got != "Count: 1|Add" {
		t.Fatal(got)
	}
	h.Tap(btn)
	h.Tap(btn)
	if got := root.Text(); got != "Count: 3|Add|That's a lot" {
		t.Fatal(got)
	}
}

func TestOneSetOneSetProp(t *testing.T) {
	name := signal.New("Ada")
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Text(name),
			ui.StaticText("below"),
		)
	})
	h.Reset()
	name.Set("Grace")
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 || h.Measures > 1 {
		t.Fatalf("%d SetProps, %d measures:\n%s", h.Count(ops.OpSetProp), h.Measures, h.Log())
	}
	if n := h.Count(ops.OpSetFrame); n != 0 { // same size: nothing moved
		t.Fatalf("%d SetFrames:\n%s", n, h.Log())
	}
	if h.Applies != 1 {
		t.Fatalf("%d applies", h.Applies)
	}

	// Taller: it and what follows move.
	h.Reset()
	name.Set(strings.Repeat("x", 100))
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 || h.Count(ops.OpSetFrame) != 2 {
		t.Fatalf("\n%s", h.Log())
	}
}

func TestEqualWriteSendsNothing(t *testing.T) {
	n := signal.New(1)
	h, r := mount(t, func() ui.Node {
		return ui.Text(signal.Func(func() string { return fmt.Sprint(n.Get() % 2) }))
	})
	h.Reset()
	n.Set(3) // the text stays "1"
	r.Flush()
	if len(h.Ops) != 0 || h.Applies != 0 {
		t.Fatalf("\n%s", h.Log())
	}
}

func TestFlattenedContainerMakesNoView(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Row(ui.Style{Gap: 4}, ui.StaticText("a"), ui.StaticText("b")),
			ui.View(ui.ViewProps{Style: ui.Style{Background: ui.Red}}, ui.StaticText("c")),
		)
	})
	views := h.OfKind(ops.KindView)
	if len(views) != 2 { // the root and the red view
		t.Fatalf("%d views:\n%s", len(views), h.Root().Tree())
	}
	want := "View 0,0 390x844\n  Text 10,10 8x20 \"a\"\n  Text 22,10 8x20 \"b\"\n  View 10,30 370x20\n    Text 0,0 370x20 \"c\"\n"
	if got := h.Root().Tree(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestStyleFuncMakesContainerReal(t *testing.T) {
	on := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Column(ui.StyleFunc(func() ui.Style {
				s := ui.Style{Padding: ui.All(5)}
				if on.Get() {
					s.Background = ui.Blue
				}
				return s
			}), ui.StaticText("x")),
		)
	})
	if got := h.Root().Tree(); got != "View 0,0 390x844\n  Text 15,15 360x20 \"x\"\n" {
		t.Fatal(got)
	}
	on.Set(true)
	r.Flush()
	if got := h.Root().Tree(); got != "View 0,0 390x844\n  View 10,10 370x30\n    Text 5,5 360x20 \"x\"\n" {
		t.Fatal(got)
	}
}

type todo struct {
	ID    int
	Title string
	Done  bool
}

func TestForMovesOneRow(t *testing.T) {
	var start []todo
	for i := 0; i < 1000; i++ {
		start = append(start, todo{ID: i, Title: fmt.Sprint("item ", i)})
	}
	items := signal.NewFunc(start, nil)
	created := 0
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{}, ui.For(items, func(t todo) int { return t.ID },
			func(it signal.Accessor[todo], _ signal.Accessor[int]) ui.Node {
				created++
				return ui.Text(signal.Func(func() string { return it.Get().Title }))
			}))
	})
	if created != 1000 || len(h.Root().Children) != 1000 {
		t.Fatalf("created %d", created)
	}
	// Move item 10 to the end.
	next := append([]todo{}, start[:10]...)
	next = append(next, start[11:]...)
	next = append(next, start[10])
	h.Reset()
	items.Set(next)
	r.Flush()
	if h.Count(ops.OpRemove) != 1 || h.Count(ops.OpInsert) != 1 || h.Count(ops.OpCreate) != 0 || h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("remove %d insert %d create %d setprop %d", h.Count(ops.OpRemove), h.Count(ops.OpInsert), h.Count(ops.OpCreate), h.Count(ops.OpSetProp))
	}
	kids := h.Root().Children
	if kids[999].Text() != "item 10" || kids[10].Text() != "item 11" {
		t.Fatal("wrong order")
	}

	// Change one item: one SetProp, nothing made.
	next = append([]todo{}, next...)
	next[500].Title = "changed"
	h.Reset()
	items.Set(next)
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 || created != 1000 {
		t.Fatalf("\n%s", h.Log())
	}

	// Remove one: one Destroy.
	h.Reset()
	items.Set(append([]todo{}, next[1:]...))
	r.Flush()
	if h.Count(ops.OpDestroy) != 1 || len(h.Root().Children) != 999 {
		t.Fatalf("\n%s", h.Log())
	}
}

func TestShowDisposesItsBranch(t *testing.T) {
	on := signal.New(true)
	ticks := signal.New(0)
	runs := 0
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("top"),
			ui.Show(on, func() ui.Node {
				signal.Effect(func() { ticks.Get(); runs++ })
				return ui.Row(ui.Style{}, ui.Text(signal.Func(func() string { return fmt.Sprint(ticks.Get()) })), ui.StaticText("!"))
			}, func() ui.Node { return ui.StaticText("off") }),
			ui.StaticText("bottom"),
		)
	})
	if got := h.Root().Text(); got != "top|0|!|bottom" {
		t.Fatal(got)
	}
	on.Set(false)
	r.Flush()
	if got := h.Root().Text(); got != "top|off|bottom" {
		t.Fatal(got)
	}
	runs = 0
	ticks.Set(1)
	r.Flush()
	if runs != 0 {
		t.Fatal("an effect of a disposed branch ran")
	}
	if len(h.OfKind(ops.KindText)) != 3 {
		t.Fatalf("views leaked:\n%s", h.Root().Tree())
	}
	on.Set(true)
	r.Flush()
	if got := h.Root().Text(); got != "top|1|!|bottom" {
		t.Fatal(got)
	}
}

func TestTextFieldTypingDoesNotRoundTrip(t *testing.T) {
	text := signal.New("")
	submitted := ""
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.TextField(ui.TextFieldProps{Value: text, Placeholder: ui.Val("New todo"), OnSubmit: func() {
				submitted = text.Peek()
				text.Set("")
			}}),
			ui.Text(text),
		)
	})
	tf := h.OfKind(ops.KindTextField)[0]
	h.Reset()
	h.Type(tf, "milk")
	for _, op := range h.Ops {
		if op.ID == tf.ID && op.Code == ops.OpSetProp {
			t.Fatalf("typing echoed back: %s", op.String())
		}
	}
	if h.Root().Text() != "milk" {
		t.Fatal(h.Root().Text())
	}
	h.Submit(tf)
	if submitted != "milk" || tf.String(ops.PropTextFieldValue) != "" {
		t.Fatalf("submitted %q, field %q", submitted, tf.String(ops.PropTextFieldValue))
	}
	_ = r
}

func TestToggle(t *testing.T) {
	on := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{}, ui.Toggle(ui.ToggleProps{Value: on}),
			ui.Text(signal.Func(func() string { return fmt.Sprint(on.Get()) })))
	})
	sw := h.OfKind(ops.KindToggle)[0]
	h.Reset()
	h.Flip(sw)
	if !on.Peek() || h.Root().Text() != "true" {
		t.Fatal("toggle")
	}
	for _, op := range h.Ops {
		if op.ID == sw.ID && op.Code == ops.OpSetProp {
			t.Fatal("toggle echoed back")
		}
	}
}

func TestSafeAreaAndKeyboard(t *testing.T) {
	h, r := mount(t, func() ui.Node {
		return ui.SafeArea(ui.Style{Padding: ui.All(4)}, ui.StaticText("hi"))
	})
	txt := h.One(ops.KindText, "hi")
	if txt.Frame != [4]float32{4, 51, 382, 20} {
		t.Fatal(txt.Frame)
	}
	env := h.Env
	env.Keyboard = 300
	h.SetEnvironment(env)
	_ = r
	// The column now ends above the keyboard; with the text at the top,
	// nothing visible moved.
	if txt.Frame != [4]float32{4, 51, 382, 20} {
		t.Fatal(txt.Frame)
	}
	env.Width, env.Height = 844, 390 // rotate
	env.SafeLeft, env.SafeRight, env.SafeTop = 47, 47, 0
	h.SetEnvironment(env)
	if txt.Frame != [4]float32{51, 4, 742, 20} || h.Root().Frame != [4]float32{0, 0, 844, 390} {
		t.Fatal(txt.Frame, h.Root().Frame)
	}
}

func TestListBindsOnlyWhatTheHostAsks(t *testing.T) {
	var all []todo
	for i := 0; i < 10000; i++ {
		all = append(all, todo{ID: i, Title: fmt.Sprint("row ", i)})
	}
	items := signal.NewFunc(all, nil)
	rows := 0
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{}, items, func(t todo) int { return t.ID },
			func(it signal.Accessor[todo], _ signal.Accessor[int]) ui.Node {
				rows++
				return ui.Row(ui.Style{Padding: ui.All(12), Gap: 8},
					ui.Text(signal.Func(func() string { return it.Get().Title })),
					ui.Text(signal.Func(func() string {
						if it.Get().Done {
							return "✓"
						}
						return ""
					})),
				)
			})
	})
	list := h.OfKind(ops.KindList)[0]
	if list.Frame != [4]float32{0, 0, 390, 844} {
		t.Fatal(list.Frame)
	}
	if rows != 0 || h.ListCount(list) != 10000 {
		t.Fatalf("rows %d count %d", rows, h.ListCount(list))
	}
	var cells []*fake.View
	for i := 0; i < 20; i++ {
		c := h.NewCell()
		h.Bind(list, i, c)
		cells = append(cells, c)
	}
	if rows != 20 {
		t.Fatalf("%d rows made", rows)
	}
	if cells[3].Frame != [4]float32{0, 0, 390, 44} || cells[3].Text() != "row 3" {
		t.Fatalf("%v %q", cells[3].Frame, cells[3].Text())
	}
	// Recycle cell 0 for item 5000: one SetProp, nothing made.
	h.Reset()
	h.Bind(list, 5000, cells[0])
	if rows != 20 || h.Count(ops.OpSetProp) != 1 || h.Count(ops.OpCreate) != 0 || cells[0].Text() != "row 5000" {
		t.Fatalf("rows %d\n%s", rows, h.Log())
	}
	// An item changes: its bound row updates in place.
	next := append([]todo{}, all...)
	next[5000].Done = true
	h.Reset()
	items.Set(next)
	r.Flush()
	if cells[0].Text() != "row 5000|✓" || h.Count(ops.OpSetProp) != 1 {
		t.Fatalf("%q\n%s", cells[0].Text(), h.Log())
	}
	// Unbinding a cell disposes its row.
	h.Unbind(list, cells[1])
	if len(cells[1].Children) != 0 {
		t.Fatal("row not destroyed")
	}
}

func TestSteadyStateFlushAllocatesNothing(t *testing.T) {
	if signal.Debug {
		t.Skip("nui_debug's main-thread checks allocate")
	}
	labels := []string{"one", "two"}
	sel := signal.New(0)
	text := signal.Func(func() string { return labels[sel.Get()] })
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(8)},
			ui.Row(ui.Style{Gap: 4}, ui.Text(text), ui.StaticText("static")),
			ui.Text(text),
		)
	})
	i := 0
	allocs := testing.AllocsPerRun(200, func() {
		i++
		h.Reset()
		sel.Set(i % 2)
		r.Flush()
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per update", allocs)
	}
}

func TestPost(t *testing.T) {
	msg := signal.New("loading")
	h, _ := mount(t, func() ui.Node { return ui.Text(msg) })
	done := make(chan struct{})
	go func() {
		ui.Post(func() { msg.Set("loaded") })
		close(done)
	}()
	<-done
	h.RunPosted()
	if h.Root().Text() != "loaded" {
		t.Fatal(h.Root().Text())
	}
}

func TestDisposeDestroysEverything(t *testing.T) {
	h := fake.New()
	r := ui.Mount(h, counter)
	r.Dispose()
	if len(h.Views) != 0 {
		t.Fatalf("%d views left", len(h.Views))
	}
}

func TestIdenticalContentMeasuresOnce(t *testing.T) {
	measures := func(n int) int {
		h, _ := mount(t, func() ui.Node {
			var rows []ui.Node
			for i := 0; i < n; i++ {
				rows = append(rows, ui.Row(ui.Style{}, ui.StaticText("same label"), ui.Button(ui.ButtonProps{Title: ui.Val("Go")})))
			}
			return ui.Column(ui.Style{}, rows...)
		})
		return h.Measures
	}
	// Identical rows share measurements: 200 rows ask the host no more
	// than 50 do (both overflow the screen, so they lay out alike).
	if a, b := measures(50), measures(200); a != b || a > 12 {
		t.Fatalf("50 rows: %d measures, 200 rows: %d", a, b)
	}
}

// applyHook calls back into the core while it applies a batch, as Android
// does when laying out a list binds its cells.
type applyHook struct {
	*fake.Host
	during func()
}

func (h *applyHook) Apply(b *ops.Batch) {
	h.Host.Apply(b)
	if f := h.during; f != nil {
		h.during = nil
		f()
	}
}

func TestFlushWhileApplying(t *testing.T) {
	h := &applyHook{Host: fake.New()}
	n := signal.New(0)
	r := ui.Mount(h, func() ui.Node {
		return ui.Text(signal.Func(func() string { return fmt.Sprint(n.Get()) }))
	})
	defer r.Dispose()
	h.Reset()
	h.during = func() {
		n.Set(2)
		r.Flush()
	}
	n.Set(1)
	r.Flush()
	// The nested flush applies only its own op, not the batch being applied.
	if got := h.Count(ops.OpSetProp); got != 2 || h.One(ops.KindText, "2") == nil {
		t.Fatalf("%d props\n%s", got, h.Log())
	}
}

func TestSnappingNeverShrinksMeasuredContent(t *testing.T) {
	h := fake.New()
	h.Env.Scale = 2.625
	// Content 113.90476 wide (299px) at x 20: rounding both edges to
	// pixels would make it 298px.
	h.MeasureFunc = func(v *fake.View, c layout.Constraints) layout.Size { return layout.Size{W: 113.90476, H: 48} }
	r := ui.Mount(h, func() ui.Node {
		return ui.Row(ui.Style{Padding: ui.All(20)}, ui.Button(ui.ButtonProps{Title: ui.Val("Add a note")}))
	})
	defer r.Dispose()
	f := h.One(ops.KindButton, "Add a note").Frame
	if px := math.Round(float64(f[2] * 2.625)); px < 299 {
		t.Fatalf("frame %v is %vpx wide", f, px)
	}
}

func TestNilPropsAreZeroValues(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Button(ui.ButtonProps{}),
			ui.TextField(ui.TextFieldProps{}),
			ui.Toggle(ui.ToggleProps{}),
			ui.Slider(ui.SliderProps{}),
			ui.Scroll(ui.ScrollProps{}, ui.Text(nil)),
		)
	})
	// No prop is sent for a nil accessor: the host's default is the zero value.
	for _, k := range []ops.Kind{ops.KindButton, ops.KindTextField, ops.KindToggle, ops.KindScroll, ops.KindText} {
		for _, v := range h.OfKind(k) {
			for id := range v.Props {
				if id >= ops.FirstKindProp {
					t.Errorf("%v has prop %d", k, id)
				}
			}
		}
	}
}

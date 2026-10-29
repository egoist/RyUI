package ui_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/internal/boot"
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

// The app's accent color (nui.json's) goes to the host when a root
// mounts, before its views.
func TestAccent(t *testing.T) {
	boot.Accent, boot.DynamicColor = "#7c3aed", "1"
	t.Cleanup(func() { boot.Accent, boot.DynamicColor = "", "" })
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	cmds := h.Commands(ops.CmdAccent)
	if len(cmds) != 1 {
		t.Fatalf("%d accent commands", len(cmds))
	}
	args := cmds[0].Value.Bytes
	if len(args) != 9 || ops.Color(binary.LittleEndian.Uint64(args)) != ui.RGB(0x7c, 0x3a, 0xed) || args[8] != 1 {
		t.Fatalf("args %v", args)
	}
	for i, op := range h.Ops {
		if op.Code == ops.OpCommand {
			for _, o := range h.Ops[:i] {
				if o.Code == ops.OpCreate && o.Kind != ops.KindView {
					t.Fatal("a view was made before the accent")
				}
			}
			break
		}
	}
	if c, ok := ui.ParseHex("7c3aed80"); !ok || c != ui.RGBA(0x7c, 0x3a, 0xed, 0x80) {
		t.Fatalf("ParseHex %x %v", c, ok)
	}
	if _, ok := ui.ParseHex("#7c3ae"); ok {
		t.Fatal("ParseHex took 5 digits")
	}
}

// Symbols: their name on the view, measured once for a name and font;
// icons ride with buttons and actions.
func TestSymbol(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: ui.Red}),
			ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: ui.Red}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Share"), Icon: ui.IconShare, Menu: []ui.Action{
				{Title: "Copy", Icon: ui.IconCopy}, {Title: "Delete", Role: ui.ActionDestructive},
			}}),
		)
	})
	syms := h.OfKind(ops.KindSymbol)
	if len(syms) != 2 || syms[0].String(ops.PropSymbolName) != "heart.fill" || syms[0].Frame[3] != 22 {
		t.Fatalf("symbols %q %v", syms[0].String(ops.PropSymbolName), syms[0].Frame)
	}
	if h.Measures != 2 { // the two symbols are measured once; the button
		t.Fatalf("%d measures", h.Measures)
	}
	b := h.OfKind(ops.KindButton)[0]
	if b.String(ops.PropButtonIcon) != "square.and.arrow.up" {
		t.Fatalf("button icon %q", b.String(ops.PropButtonIcon))
	}
	if got := b.Actions(ops.PropMenu); len(got) != 2 || got[0] != "Copy" || got[1] != "Delete" {
		t.Fatalf("titles %v", got)
	}
	if got := b.ActionIcons(ops.PropMenu); len(got) != 2 || got[0] != "doc.on.doc" || got[1] != "" {
		t.Fatalf("icons %v", got)
	}
}

// A multiline field grows with its text, up to its line limit.
func TestMultilineTextField(t *testing.T) {
	text := signal.New("")
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{}, ui.TextField(ui.TextFieldProps{Value: text, Multiline: true, Style: ui.Style{Lines: 3}}))
	})
	f := h.OfKind(ops.KindTextField)[0]
	if !f.Bool(ops.PropTextFieldMultiline) || f.Frame[3] != 36 {
		t.Fatalf("frame %v", f.Frame)
	}
	h.Type(f, "a\nb")
	r.Flush()
	if f.Frame[3] != 56 || text.Peek() != "a\nb" {
		t.Fatalf("after two lines: %v %q", f.Frame, text.Peek())
	}
	text.Set("1\n2\n3\n4\n5")
	r.Flush()
	if f.Frame[3] != 76 {
		t.Fatalf("past the limit: %v", f.Frame)
	}
}

// A segmented control writes the user's choice; code's choice is sent.
// A progress bar clamps its value.
func TestSegmentedAndProgress(t *testing.T) {
	sel := signal.New(1)
	done := signal.New(0.25)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Segmented(ui.SegmentedProps{Segments: []string{"Day", "Week", "Month"}, Selected: sel}),
			ui.Progress(done),
			ui.Progress(nil),
		)
	})
	s := h.OfKind(ops.KindSegmented)[0]
	if s.Props[ops.PropSegmentedSelected].Int != 1 || s.Frame[3] != 32 {
		t.Fatalf("segmented %+v", s.Frame)
	}
	h.Reset()
	h.Select(s, 2)
	r.Flush()
	if sel.Peek() != 2 || h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("selected %d, %d props sent back", sel.Peek(), h.Count(ops.OpSetProp))
	}
	sel.Set(0)
	r.Flush()
	if s.Props[ops.PropSegmentedSelected].Int != 0 {
		t.Fatal("code's choice not sent")
	}
	ps := h.OfKind(ops.KindProgress)
	done.Set(1.5)
	r.Flush()
	if ps[0].Props[ops.PropProgressValue].Float != 1 || !ps[1].Bool(ops.PropProgressIndeterminate) {
		t.Fatalf("progress %v %v", ps[0].Props[ops.PropProgressValue].Float, ps[1].Bool(ops.PropProgressIndeterminate))
	}
}

// A date picker sends its instant in Unix milliseconds, and the user's
// choice writes the signal without being sent back.
func TestDatePicker(t *testing.T) {
	when := signal.New(time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC))
	h, r := mount(t, func() ui.Node {
		return ui.Row(ui.Style{}, ui.DatePicker(ui.DatePickerProps{Value: when, Mode: ui.DateAndTime,
			Min: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}))
	})
	d := h.OfKind(ops.KindDatePicker)[0]
	if d.Props[ops.PropDatePickerValue].Int != when.Peek().UnixMilli() || d.Props[ops.PropDatePickerMin].Int == 0 || d.Frame[2] != 200 {
		t.Fatalf("props %+v frame %v", d.Props, d.Frame)
	}
	picked := time.Date(2026, 12, 24, 18, 0, 0, 0, time.UTC)
	h.Reset()
	h.Dispatch(ops.Event{ID: d.ID, Kind: ops.EventValueChanged, Float: float64(picked.UnixMilli())})
	r.Flush()
	if !when.Peek().Equal(picked) || h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("picked %v, %d props sent", when.Peek(), h.Count(ops.OpSetProp))
	}
}

// The host's time zone is Go's local time.
func TestTimeZone(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	env := h.Env
	env.TimeZone = "Asia/Tokyo"
	h.SetEnvironment(env)
	if time.Local.String() != "Asia/Tokyo" {
		t.Fatalf("time.Local %v", time.Local)
	}
}

// A bound row carries its swipe actions, which run for its current
// index; binding near the end loads more, once per length; ScrollToItem
// is a command.
func TestListSwipeAndEnd(t *testing.T) {
	items := signal.NewFunc([]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, nil)
	var deleted []int
	ends := 0
	var list ui.Node
	h, r := mount(t, func() ui.Node {
		list = ui.List(ui.ListProps{
			SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { deleted = append(deleted, i) }}}
			},
			OnEndReached: func() {
				ends++
				items.Set(append(items.Peek(), 10, 11))
			},
		}, items, func(i int) int { return i }, func(it signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get()) }))
		})
		return list
	})
	lv := h.OfKind(ops.KindList)[0]
	cell := h.NewCell()
	h.Bind(lv, 2, cell)
	r.Flush()
	if got := cell.Actions(ops.PropSwipeActions); len(got) != 1 || got[0] != "Delete" {
		t.Fatalf("swipe actions %v", got)
	}
	h.Dispatch(ops.Event{ID: cell.ID, Kind: ops.EventAction, Int: 0})
	if len(deleted) != 1 || deleted[0] != 2 {
		t.Fatalf("deleted %v", deleted)
	}

	// Near the end: more, once.
	h.Bind(lv, 8, cell)
	h.Bind(lv, 9, cell)
	h.RunPosted()
	r.Flush()
	if ends != 1 || len(items.Peek()) != 12 {
		t.Fatalf("ends %d, %d items", ends, len(items.Peek()))
	}
	h.Bind(lv, 11, cell)
	h.RunPosted()
	if ends != 2 {
		t.Fatalf("ends %d after the new end", ends)
	}

	ui.ScrollToItem(list, 5, true)
	r.Flush()
	if cmds := h.Commands(ops.CmdScrollTo); len(cmds) != 1 || cmds[0].Value.Bytes[0] != 5 || cmds[0].Value.Bytes[4] != 1 {
		t.Fatalf("scroll commands %v", cmds)
	}
}

// Copy and Share are commands to the root.
func TestCopyShare(t *testing.T) {
	h, r := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() { ui.Copy("hi"); ui.Share("https://go.dev") }})
	})
	h.Tap(h.One(ops.KindButton, "Share"))
	r.Flush()
	if c := h.Commands(ops.CmdCopy); len(c) != 1 || string(c[0].Value.Bytes) != "hi" {
		t.Fatalf("copy %v", c)
	}
	if c := h.Commands(ops.CmdShare); len(c) != 1 || string(c[0].Value.Bytes) != "https://go.dev" {
		t.Fatalf("share %v", c)
	}
}

// Transforms, shadows and gradients are sent as bytes props; a view with
// only a transform is a real view (it's drawn moved).
func TestTransformShadowGradient(t *testing.T) {
	spin := signal.New(float32(0))
	h, r := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Transform: ui.Transform{TranslateX: 10, Scale: 2, Rotate: spin.Get()},
				Shadow:   ui.Shadow{Color: ui.Label, Radius: 8, Y: 2},
				Gradient: ui.LinearGradient(ui.Red, ui.Blue)}
		})}, ui.StaticText("x"))
	})
	v := h.Find(ops.KindView, "")[0]
	for v.Props[ops.PropTransform].Bytes == nil && len(v.Children) > 0 {
		v = v.Children[0]
	}
	f32 := func(b []byte, i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:])) }
	tb := v.Props[ops.PropTransform].Bytes
	if len(tb) != 20 || f32(tb, 0) != 10 || f32(tb, 2) != 2 || f32(tb, 3) != 2 || f32(tb, 4) != 0 {
		t.Fatalf("transform %v", tb)
	}
	g := v.Props[ops.PropGradient].Bytes
	if binary.LittleEndian.Uint32(g) != 2 || f32(g[24:], 0) != 1 { // two stops; the second at 1
		t.Fatalf("gradient %v", g)
	}
	if len(v.Props[ops.PropShadow].Bytes) != 20 {
		t.Fatalf("shadow %v", v.Props[ops.PropShadow].Bytes)
	}
	h.Reset()
	spin.Set(90)
	r.Flush()
	// One prop, and no layout: a transform is drawn, not laid out.
	if h.Count(ops.OpSetProp) != 1 || h.Count(ops.OpSetFrame) != 0 || f32(v.Props[ops.PropTransform].Bytes, 4) != 90 {
		t.Fatalf("%d props, %d frames for a rotation", h.Count(ops.OpSetProp), h.Count(ops.OpSetFrame))
	}
}

// A repeating animation's count and autoreverse ride with AnimateBegin.
func TestRepeatingAnimation(t *testing.T) {
	on := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			if on.Get() {
				return ui.Style{Opacity: 0.5}
			}
			return ui.Style{Opacity: 1}
		})})
	})
	ui.AnimateWith(ui.Animation{Duration: 0.8, Repeat: ui.Forever, Autoreverses: true}, func() { on.Set(true) })
	r.Flush()
	b := h.Commands(ops.CmdAnimateBegin)[0].Value.Bytes
	if len(b) != 10 || int32(binary.LittleEndian.Uint32(b[5:])) != -1 || b[9] != 1 {
		t.Fatalf("args %v", b)
	}
}

// A Call goes to the host as a command; its Result comes to done.
func TestCall(t *testing.T) {
	h, r := mount(t, func() ui.Node { return ui.StaticText("x") })
	h.Modules = map[string]func(string, []byte) ([]byte, error){
		"echo": func(method string, args []byte) ([]byte, error) {
			if method == "fail" {
				return nil, fmt.Errorf("nope")
			}
			return append([]byte(method+":"), args...), nil
		},
	}
	var got []string
	ui.Call("echo", "say", []byte("hi"), func(b []byte, err error) { got = append(got, string(b)) })
	ui.Call("echo", "fail", nil, func(b []byte, err error) { got = append(got, "err "+err.Error()) })
	ui.Call("missing", "x", nil, func(b []byte, err error) { got = append(got, "err "+err.Error()) })
	r.Flush()
	h.RunPosted()
	if strings.Join(got, "|") != `say:hi|err nope|err no module "missing"` {
		t.Fatalf("got %q", got)
	}
}

// A URL the app is opened with goes to its handlers; one before any
// handler waits for the first.
func TestIncomingURL(t *testing.T) {
	var got []string
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	h.Dispatch(ops.Event{ID: 0, Kind: ops.EventIncomingURL, Str: "myapp://launch"})
	r2 := ui.Mount(h, func() ui.Node {
		ui.OnOpenURL(func(u string) { got = append(got, u) })
		return ui.StaticText("y")
	})
	h.Dispatch(ops.Event{ID: 0, Kind: ops.EventIncomingURL, Str: "myapp://later"})
	r2.Dispose()
	h.Dispatch(ops.Event{ID: 0, Kind: ops.EventIncomingURL, Str: "myapp://gone"})
	if strings.Join(got, " ") != "myapp://launch myapp://later" {
		t.Fatalf("got %v", got)
	}
}

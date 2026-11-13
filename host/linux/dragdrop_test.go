//go:build linux && !android

package linux

import (
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A drag within the app, as a mouse makes it (xdotool's, through the X
// server): the source reports Began then Dropped, the target DragOver as
// it comes and goes, and Drop with the source's Go value, as the app's
// own drop.
func TestDragDropInApp(t *testing.T) {
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool to move the mouse")
	}
	type card struct{ name string }
	value := &card{"first"}
	var phases []ui.DragPhase
	var overs []bool
	var drops []ui.Drop
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Padding: ui.All(20), Gap: 100, Align: ui.AlignStart},
			ui.View(ui.ViewProps{
				Drag: &ui.DragSource{
					Items: func() []ui.DragItem {
						return []ui.DragItem{{Value: value, MIME: "application/x-asuka-test", Text: "first"}}
					},
					OnPhase: func(p ui.DragPhase) { phases = append(phases, p) },
				},
				Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(80), Background: colors.Blue},
			}),
			ui.View(ui.ViewProps{
				Drop: &ui.DropTarget{
					Accept:       []string{"application/x-asuka-test"},
					OnOverChange: func(on bool) { overs = append(overs, on) },
					OnDrop:       func(d ui.Drop) { drops = append(drops, d) },
				},
				Style: ui.Style{Width: ui.Pt(120), Height: ui.Pt(120), Background: colors.Gray},
			}),
		)
	})
	var src, dst *node
	settle(t, h, func() bool {
		for n := range dragSources {
			src = n
		}
		for n := range dropTargets {
			dst = n
		}
		return src != nil && dst != nil && WidgetGetMapped(src.widget) && WidgetGetMapped(dst.widget) && src.placed && dst.placed
	})
	xdrag(t, h, src, dst)(func() bool { return len(phases) > 0 }, func() bool { return len(overs) > 0 })
	settle(t, h, func() bool { return len(drops) == 1 && len(phases) == 2 && len(overs) == 2 })
	onMain(func() {
		if !slices.Equal(phases, []ui.DragPhase{ui.DragBegan, ui.DragDropped}) {
			fatalf(t, "the source's phases: %v", phases)
		}
		if !slices.Equal(overs, []bool{true, false}) {
			fatalf(t, "the target's overs: %v", overs)
		}
		d := drops[0]
		if !d.Local || len(d.Items) != 1 || d.Items[0].Value != value {
			fatalf(t, "the drop: %+v", d)
		}
		if d.X < 30 || d.X > 90 || d.Y < 30 || d.Y > 90 {
			fatalf(t, "dropped at %v,%v, want about 60,60", d.X, d.Y)
		}
	})
	// What the drag carried is forgotten once it's through.
	settle(t, h, func() bool { return len(dropReads) == 0 && appDrag.drag == 0 })
}

// A drag's content: the files, the text (or the links' URLs), the links
// as text/uri-list and the app's private type, which marks a drag of only
// values.
func TestDragContent(t *testing.T) {
	mount(t, emptyRoot)
	onMain(func() {
		formats := func(items []ops.Item) []string {
			p := dragContent(7, items)
			defer ObjectUnref(p)
			// As other apps see them: the values' types serialized.
			f := Call(Sym("gdk_content_formats_union_serialize_mime_types"), Draw.ContentProviderRefFormats(p))
			defer Draw.ContentFormatsUnref(f)
			return MIMETypes(f)
		}
		has := func(ms []string, m string) bool { return slices.Contains(ms, m) }
		text := formats([]ops.Item{{Kind: ops.ShareKindText, Text: "hello", MIME: "text/plain"}})
		if !has(text, "text/plain;charset=utf-8") || !has(text, dragTypeLocal) || has(text, "text/uri-list") {
			fatalf(t, "a text's formats: %v", text)
		}
		link := formats([]ops.Item{{Kind: ops.ShareKindLink, Text: "https://example.com", MIME: "text/uri-list"}})
		if !has(link, "text/uri-list") || !has(link, "text/plain;charset=utf-8") {
			fatalf(t, "a link's formats: %v", link)
		}
		file := formats([]ops.Item{{Kind: ops.ShareKindFile, Path: "/etc/hostname", Name: "hostname", MIME: "text/plain"}})
		if !has(file, "text/uri-list") {
			fatalf(t, "a file's formats: %v", file)
		}
		value := formats([]ops.Item{{Value: true, MIME: "application/x-card"}})
		if fmt.Sprint(value) != fmt.Sprint([]string{dragTypeLocal}) {
			fatalf(t, "a value's formats: %v", value)
		}
	})
}

// A drop as another app's comes (the app's own drag, its drop read as
// if it came from elsewhere): a text read as text, a file as a file, a
// link as a link; a type the target doesn't take isn't over it.
func TestDropFromOtherApp(t *testing.T) {
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool to move the mouse")
	}
	items := signal.New(0)
	sets := [][]ui.DragItem{
		{{Text: "Hello from Asuka"}},
		{{Kind: ui.SharedFile, Path: "/etc/hostname"}},
		{{Kind: ui.SharedLink, Text: "https://example.com/a"}},
	}
	var phases []ui.DragPhase
	var drops []ui.Drop
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Padding: ui.All(20), Gap: 100, Align: ui.AlignStart},
			ui.View(ui.ViewProps{
				Drag: &ui.DragSource{
					Items:   func() []ui.DragItem { return sets[items.Get()] },
					OnPhase: func(p ui.DragPhase) { phases = append(phases, p) },
				},
				Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(80), Background: colors.Blue},
			}),
			ui.View(ui.ViewProps{
				Drop:  &ui.DropTarget{Accept: []string{"*/*"}, OnDrop: func(d ui.Drop) { drops = append(drops, d) }},
				Style: ui.Style{Width: ui.Pt(120), Height: ui.Pt(120), Background: colors.Gray},
			}),
		)
	})
	var src, dst *node
	settle(t, h, func() bool {
		for n := range dragSources {
			src = n
		}
		for n := range dropTargets {
			dst = n
		}
		return src != nil && dst != nil && WidgetGetMapped(src.widget) && WidgetGetMapped(dst.widget) && src.placed && dst.placed
	})
	drag := xdrag(t, h, src, dst)
	for i := range sets {
		onMain(func() { post(h, func() { items.Set(i) }) })
		phases = nil
		// Once it lifts, the drop doesn't know it's the app's own.
		drag(func() bool {
			if len(phases) > 0 && appDrag.drag != 0 {
				appDrag.drag = 0
			}
			return len(phases) > 0 && appDrag.drag == 0
		}, func() bool { return true })
		settle(t, h, func() bool { return len(drops) == i+1 && len(phases) == 2 })
		d := drops[i]
		if d.Local || len(d.Items) != 1 {
			t.Fatalf("drop %d: %+v", i, d)
		}
		it := d.Items[0]
		switch i {
		case 0:
			if it.Kind != ui.SharedText || it.Text != "Hello from Asuka" || it.MIME != "text/plain" {
				t.Errorf("the text: %+v", it)
			}
		case 1:
			if !it.IsFile() || it.Path != "/etc/hostname" || it.Name != "hostname" || it.Size <= 0 {
				t.Errorf("the file: %+v", it)
			}
		case 2:
			if it.Kind != ui.SharedLink || it.Text != "https://example.com/a" || it.MIME != "text/uri-list" {
				t.Errorf("the link: %+v", it)
			}
		}
	}
}

// xdrag returns a function that drags from the middle of src to the
// middle of dst with the mouse (xdotool, through the X server): once
// it's moved a little, it waits until lifted holds; over dst, until over
// does, then lets go.
func xdrag(t *testing.T, h *Host, src, dst *node) func(lifted, over func() bool) {
	var xid uintptr
	var from, to [2]int
	onMain(func() {
		win := h.main.window
		native := WidgetGetNative(win)
		xid = Call(Sym("gdk_x11_surface_get_xid"), NativeGetSurface(native))
		var sx, sy float64
		Call(Sym("gtk_native_get_surface_transform"), native, Addr(&sx), Addr(&sy))
		at := func(n *node) [2]int {
			var p Point
			WidgetComputePoint(n.widget, win, &Point{X: n.frame[2] / 2, Y: n.frame[3] / 2}, &p)
			return [2]int{int(float64(p.X) + sx), int(float64(p.Y) + sy)}
		}
		from, to = at(src), at(dst)
	})
	if xid == 0 {
		t.Skip("not an X11 window")
	}
	w := strconv.Itoa(int(xid))
	xdo := func(args ...string) {
		if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
			t.Fatalf("xdotool %v: %v %s", args, err, out)
		}
	}
	move := func(p [2]int) { xdo("mousemove", "--window", w, strconv.Itoa(p[0]), strconv.Itoa(p[1])) }
	return func(lifted, over func() bool) {
		move([2]int{from[0] - 1, from[1] - 1})
		time.Sleep(20 * time.Millisecond)
		move(from)
		time.Sleep(50 * time.Millisecond)
		xdo("mousedown", "1")
		time.Sleep(50 * time.Millisecond)
		for i := 1; i <= 20; i++ {
			f := float64(i) / 20
			move([2]int{from[0] + int(f*float64(to[0]-from[0])), from[1] + int(f*float64(to[1]-from[1]))})
			time.Sleep(20 * time.Millisecond)
			if i == 5 {
				settle(t, h, lifted)
			}
		}
		settle(t, h, over)
		xdo("mouseup", "1")
	}
}

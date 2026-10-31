//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"testing"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// AppKit runs on the main thread: TestMain keeps it, and the tests send
// their work there.
var mainQ = make(chan func())

func TestMain(m *testing.M) {
	done := make(chan int)
	go func() { done <- m.Run() }()
	for {
		select {
		case f := <-mainQ:
			f()
		case code := <-done:
			os.Exit(code)
		}
	}
}

func onMain(f func()) {
	ok := make(chan struct{})
	mainQ <- func() {
		defer close(ok)
		WithPool(f)
	}
	<-ok
}

// mount mounts root on a new host (a window of its own), as Run does but
// without AppKit's event loop: work the host defers runs at once.
func mount(t *testing.T, root func() ui.Node) (*Host, *ui.Root) {
	var h *Host
	var r *ui.Root
	onMain(func() {
		h = newHost()
		h.enter(func() { r = ui.Mount(h, root) })
	})
	t.Cleanup(func() {
		onMain(func() {
			h.enter(r.Dispose)
			Send(h.window, "close")
		})
	})
	return h, r
}

// find returns the nodes of kind k.
func find(h *Host, k ops.Kind) []*node {
	var out []*node
	for _, n := range h.nodes {
		if n.kind == k {
			out = append(out, n)
		}
	}
	return out
}

func text(n *node) string {
	if n.tracking != 0 {
		return GoString(Send(n.tracking, "string"))
	}
	return GoString(Send(n.view, "stringValue"))
}

func TestCounter(t *testing.T) {
	count := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Update(func(n int) int { return n + 1 }) }}),
		)
	})
	onMain(func() {
		texts, buttons := find(h, ops.KindText), find(h, ops.KindButton)
		if len(texts) != 1 || len(buttons) != 1 {
			t.Fatalf("%d texts, %d buttons", len(texts), len(buttons))
		}
		label, button := texts[0], buttons[0]
		if s := text(label); s != "Count: 0" {
			t.Fatalf("text %q", s)
		}
		// A real click, through the button's target and action.
		Send(button.view, "performClick:", 0)
		if s := text(label); s != "Count: 1" {
			t.Fatalf("after a click: %q", s)
		}
		f := MsgRect(label.view, Sel("frame"))
		if f.Origin.X != 16 || f.Origin.Y != 16 || f.Size.Width <= 0 || f.Size.Height <= 0 {
			t.Fatalf("text frame %+v", f)
		}
		if Send(label.view, "window") != h.window {
			t.Fatal("the text isn't in the window")
		}
	})
}

func TestTextWraps(t *testing.T) {
	long := "The quick brown fox jumps over the lazy dog, again and again, until the line has to wrap."
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Width: ui.Pt(120)}, ui.StaticText(long))
	})
	onMain(func() {
		f := MsgRect(find(h, ops.KindText)[0].view, Sel("frame"))
		if f.Size.Width > 120 || f.Size.Height < 40 {
			t.Fatalf("a long text in 120pt: %+v", f.Size)
		}
	})
}

func TestTextField(t *testing.T) {
	value := signal.New("")
	submitted := 0
	h, _ := mount(t, func() ui.Node {
		return ui.TextField(ui.TextFieldProps{Value: value, OnSubmit: func() { submitted++ }})
	})
	onMain(func() {
		f := find(h, ops.KindTextField)[0]
		Send(f.view, "setStringValue:", uintptr(String("hello")))
		h.textChanged(f.view)
		if value.Peek() != "hello" {
			t.Fatalf("the signal is %q", value.Peek())
		}
		h.textCommand(f.view, 0, Sel("insertNewline:"))
		if submitted != 1 {
			t.Fatalf("submitted %d times", submitted)
		}
		// The app's write reaches the field.
		h.enter(func() { ui.Post(func() { value.Set("set") }) })
		h.runPosted()
		if s := text(f); s != "set" {
			t.Fatalf("the field shows %q", s)
		}
	})
}

func TestListBindsOnlyWhatShows(t *testing.T) {
	items := make([]int, 10000)
	for i := range items {
		items[i] = i
	}
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{}, signal.Static(items), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(40)}},
					ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) })))
			})
	})
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		if len(l.shown) == 0 || len(l.shown) > 60 {
			t.Fatalf("%d rows shown of 10,000", len(l.shown))
		}
		if total := l.tops[len(l.tops)-1]; total < 10000*40-1 {
			t.Fatalf("the content is %.0f tall", total)
		}
		// Scrolled to the middle, the rows there are bound.
		h.scrollToOffset(l.n, 0, 200000, false)
		h.scrolledList(Send(l.scroll, "contentView"))
		found := false
		for r, rv := range l.shown {
			if r == 5000 {
				found = true
				if s := text(firstText(rv.cells[0].n)); s != "Row 5000" {
					t.Fatalf("row 5000 shows %q", s)
				}
			}
		}
		if !found {
			t.Fatal("row 5000 isn't shown at 200,000pt")
		}
	})
}

func TestStackPushAndBack(t *testing.T) {
	var nav *ui.Navigator
	disposed := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.StaticText("Home")
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	onMain(func() {
		h.enter(func() {
			ui.Post(func() {
				nav.Push(func() ui.Node {
					signal.OnCleanup(func() { disposed++ })
					return ui.StaticText("Detail")
				}, ui.ScreenOptions{Title: ui.Val("Detail")})
			})
		})
		h.runPosted()
		if s := GoString(Send(h.window, "title")); s != "Detail" {
			t.Fatalf("the window's title is %q", s)
		}
		if !h.chrome.back || h.chrome.toolbar == 0 {
			t.Fatal("no back button")
		}
		// The toolbar's back button: Popped, and the core disposes the screen.
		h.back()
		if disposed != 1 {
			t.Fatalf("the popped screen's owner was disposed %d times", disposed)
		}
		if s := GoString(Send(h.window, "title")); s != "Home" {
			t.Fatalf("after back, the title is %q", s)
		}
		if nav.Depth() != 1 {
			t.Fatalf("depth %d", nav.Depth())
		}
	})
}

func TestDisposeDestroysViews(t *testing.T) {
	var h *Host
	var r *ui.Root
	onMain(func() {
		h = newHost()
		h.enter(func() {
			r = ui.Mount(h, func() ui.Node {
				return ui.Column(ui.Style{}, ui.StaticText("a"), ui.Button(ui.ButtonProps{Title: ui.Val("b")}))
			})
		})
		if len(h.nodes) == 0 {
			t.Fatal("no views")
		}
		h.enter(r.Dispose)
		if len(h.nodes) != 0 || len(h.byView) != 0 {
			t.Fatalf("%d nodes, %d views left", len(h.nodes), len(h.byView))
		}
		Send(h.window, "close")
	})
}

func firstText(n *node) *node {
	if n.kind == ops.KindText {
		return n
	}
	for _, c := range n.children {
		if t := firstText(c); t != nil {
			return t
		}
	}
	return nil
}

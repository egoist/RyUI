//go:build windows && amd64

package windows

import (
	"fmt"
	"os"
	"testing"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// Win32 windows belong to the thread that made them: TestMain keeps the
// main thread, and the tests send their work there.
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

// failure is a test failing on the main thread: t.Fatal there would end
// the main goroutine, so fatalf panics and onMain fails the test.
type failure string

func fatalf(format string, args ...any) { panic(failure(fmt.Sprintf(format, args...))) }

func onMain(t *testing.T, f func()) {
	ok := make(chan any)
	mainQ <- func() {
		defer func() { ok <- recover() }()
		f()
	}
	if r := <-ok; r != nil {
		if msg, isFailure := r.(failure); isFailure {
			t.Fatal(string(msg))
		}
		panic(r)
	}
}

// mount mounts root on a new host (a window of its own, not shown), as
// Run does but without the message loop: work the host defers runs at
// once.
func mount(t *testing.T, root func() ui.Node) (*Host, *ui.Root) {
	var h *Host
	var r *ui.Root
	onMain(t, func() {
		h = newHost()
		h.enter(func() { r = ui.Mount(h, root) })
	})
	t.Cleanup(func() {
		onMain(t, func() {
			h.enter(r.Dispose)
			DestroyWindow(h.window)
			DestroyWindow(h.park)
			DestroyWindow(h.msgWin)
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

// frame is a node's window's rectangle in its parent, in points.
func frame(h *Host, n *node) [4]float32 {
	r := WindowRectIn(n.hwnd, Parent(n.hwnd))
	return [4]float32{h.pt(r.Left), h.pt(r.Top), h.pt(r.W()), h.pt(r.H())}
}

func TestCounter(t *testing.T) {
	count := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Update(func(n int) int { return n + 1 }) }}),
		)
	})
	onMain(t, func() {
		texts, buttons := find(h, ops.KindText), find(h, ops.KindButton)
		if len(texts) != 1 || len(buttons) != 1 {
			fatalf("%d texts, %d buttons", len(texts), len(buttons))
		}
		label, button := texts[0], buttons[0]
		if label.text != "Count: 0" {
			fatalf("text %q", label.text)
		}
		// A real click: the BUTTON's, through BN_CLICKED to its parent.
		SendMessage(button.ctl, BM_CLICK, 0, 0)
		if label.text != "Count: 1" {
			fatalf("after a click: %q", label.text)
		}
		f := frame(h, label)
		if f[0] != 16 || f[1] != 16 || f[2] <= 0 || f[3] <= 0 {
			fatalf("text frame %v", f)
		}
		if RootWindow(label.hwnd) != h.window {
			fatalf("the text isn't in the window")
		}
		if WindowText(button.ctl) != "Add" {
			fatalf("the button's title is %q", WindowText(button.ctl))
		}
	})
}

func TestTextWraps(t *testing.T) {
	long := "The quick brown fox jumps over the lazy dog, again and again, until the line has to wrap."
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Width: ui.Pt(120)}, ui.StaticText(long))
	})
	onMain(t, func() {
		f := frame(h, find(h, ops.KindText)[0])
		if f[2] > 120 || f[3] < 40 {
			fatalf("a long text in 120pt: %v", f)
		}
	})
}

func TestTextField(t *testing.T) {
	value := signal.New("")
	submitted := 0
	h, _ := mount(t, func() ui.Node {
		return ui.TextField(ui.TextFieldProps{Value: value, OnSubmit: func() { submitted++ }})
	})
	onMain(t, func() {
		f := find(h, ops.KindTextField)[0]
		// Typing: the EDIT's own text change, reported with EN_CHANGE.
		for _, r := range "hello" {
			SendMessage(f.ctl, WM_CHAR, uintptr(r), 0)
		}
		if value.Peek() != "hello" {
			fatalf("the signal is %q", value.Peek())
		}
		SendMessage(f.ctl, WM_KEYDOWN, VK_RETURN, 0)
		if submitted != 1 {
			fatalf("submitted %d times", submitted)
		}
		// The app's write reaches the field.
		h.enter(func() { ui.Post(func() { value.Set("set") }) })
		h.runPosted()
		if s := WindowText(f.ctl); s != "set" {
			fatalf("the field shows %q", s)
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
	onMain(t, func() {
		l := find(h, ops.KindList)[0].list
		if len(l.shown) == 0 || len(l.shown) > 60 {
			fatalf("%d rows shown of 10,000", len(l.shown))
		}
		if total := l.tops[len(l.tops)-1]; total < 10000*40-1 {
			fatalf("the content is %.0f tall", total)
		}
		// Scrolled to the middle, the rows there are bound.
		h.scrollToOffset(l.n, 0, 200000)
		found := false
		for r, rv := range l.shown {
			if r == 5000 {
				found = true
				if s := firstText(rv.cells[0].n).text; s != "Row 5000" {
					fatalf("row 5000 shows %q", s)
				}
				if y := WindowRectIn(rv.n.hwnd, l.n.hwnd).Top; y < 0 || h.pt(y) > l.viewport(h) {
					fatalf("row 5000 is at %d", y)
				}
			}
		}
		if !found {
			fatalf("row 5000 isn't shown at 200,000pt")
		}
		// The scroll bar's thumb shows, and follows.
		if b := l.n.bar; b == nil || !b.visible || b.thumb.Top <= 0 {
			fatalf("no thumb in the middle of the list")
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
	onMain(t, func() {
		h.enter(func() {
			ui.Post(func() {
				nav.Push(func() ui.Node {
					signal.OnCleanup(func() { disposed++ })
					return ui.StaticText("Detail")
				}, ui.ScreenOptions{Title: ui.Val("Detail")})
			})
		})
		h.runPosted()
		stack := find(h, ops.KindStack)[0]
		header := stack.nav.bar
		if s := header.nav.screen; s == nil || s.title != "Detail" {
			fatalf("the header doesn't show the pushed screen")
		}
		if !shown(header.nav.back) {
			fatalf("no back button")
		}
		// The header's back button: Popped, and the core disposes the screen.
		SendMessage(header.nav.back, BM_CLICK, 0, 0)
		if disposed != 1 {
			fatalf("the popped screen's owner was disposed %d times", disposed)
		}
		if s := header.nav.screen; s == nil || s.title != "Home" {
			fatalf("after back, the header doesn't show Home")
		}
		if nav.Depth() != 1 {
			fatalf("depth %d", nav.Depth())
		}
		if shown(header.nav.back) {
			fatalf("a back button on the root screen")
		}
	})
}

func TestDisposeDestroysWindows(t *testing.T) {
	onMain(t, func() {
		h := newHost()
		var r *ui.Root
		h.enter(func() {
			r = ui.Mount(h, func() ui.Node {
				return ui.Column(ui.Style{}, ui.StaticText("a"), ui.Button(ui.ButtonProps{Title: ui.Val("b")}),
					ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
			})
		})
		if len(h.nodes) == 0 {
			fatalf("no views")
		}
		var windows []uintptr
		for hw := range h.byHwnd {
			windows = append(windows, hw)
		}
		h.enter(r.Dispose)
		if len(h.nodes) != 0 || len(h.byHwnd) != 0 {
			fatalf("%d nodes, %d windows left", len(h.nodes), len(h.byHwnd))
		}
		for _, hw := range windows {
			if IsWindow(hw) {
				fatalf("window %x is still there", hw)
			}
		}
		DestroyWindow(h.window)
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

// shown reports whether a window is set visible (the test's top-level
// window isn't shown, so IsWindowVisible is false for all).
func shown(hwnd uintptr) bool { return WindowLong(hwnd, GWL_STYLE)&WS_VISIBLE != 0 }

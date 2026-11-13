//go:build windows

package windows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Win32 windows belong to the thread that made them: TestMain keeps the
// main thread, and the tests send their work there.
var mainQ = make(chan func())

func TestMain(m *testing.M) {
	if launchedForToast { // COM started the tests for a toast (notify_test.go)
		toastServer()
		os.Exit(0)
	}
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
			// What the host deferred (WebView2 controllers it closes on a
			// later turn) runs before its windows go.
			h.runPosted()
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
	return [4]float32{h.pt(n, r.Left), h.pt(n, r.Top), h.pt(n, r.W()), h.pt(n, r.H())}
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

// An inset grouped list's rows are cards inset by the core's inset
// (16pt), 4pt apart; the core lays them out at the cards' width.
func TestListInsetGrouped(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{Appearance: ui.ListInsetGrouped}, signal.Static([]int{1, 2, 3}), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) }), ui.Style{Padding: ui.All(10)})
			})
	})
	onMain(t, func() {
		l := find(h, ops.KindList)[0].list
		if !l.grouped() || l.inset != 16 || len(l.shown) != 3 {
			fatalf("grouped %v inset %v, %d rows", l.grouped(), l.inset, len(l.shown))
		}
		r := WindowRectIn(l.shown[0].n.hwnd, l.n.hwnd)
		if r.Left != h.px(l.n, 16) || r.Top != h.px(l.n, 16) || r.W() != h.px(l.n, l.width-16)-h.px(l.n, 16) {
			fatalf("the first row is at %+v, the list %v wide", r, l.width)
		}
		if w := find(h, ops.KindText)[0].frame[2]; w != l.width-32 {
			fatalf("the row's text is %v wide, its card %v", w, l.width-32)
		}
		if c0, c2 := l.shown[0].card, l.shown[2].card; c0 != l.rowHeight(0)-cardGap || c2 != l.rowHeight(2) || l.shown[0].sep {
			fatalf("cards %v %v, a separator %v", c0, c2, l.shown[0].sep)
		}
		if gap := l.tops[1] - l.tops[0] - l.shown[0].card; gap != cardGap {
			fatalf("the cards are %v apart", gap)
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
				if y := WindowRectIn(rv.n.hwnd, l.n.hwnd).Top; y < 0 || h.pt(l.n, y) > l.viewport(h) {
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

func TestWindow(t *testing.T) {
	open := signal.New(false)
	closed := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Title: ui.Val("Inspector"), Width: 300, Height: 200, MinWidth: 240, MinHeight: 120,
			OnClose: func() { closed++ },
			Content: func() ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("width ", ui.UseEnvironment().Get().Width) }))
			}})
	})
	onMain(t, func() {
		h.enter(func() { ui.Post(func() { open.Set(true) }) })
		h.runPosted()
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		n := h.wins[0]
		win := n.nav.window
		if Parent(win) != 0 || RootWindow(win) != win || win == h.window {
			fatalf("not a top-level window of its own")
		}
		if s := WindowText(win); s != "Inspector" {
			fatalf("title %q", s)
		}
		if cr := ClientRect(win); cr.W() != h.px(nil, 300) || cr.H() != h.px(nil, 200) {
			fatalf("client %dx%d, want %dx%d", cr.W(), cr.H(), h.px(nil, 300), h.px(nil, 200))
		}
		// Its content lays out at the window's size.
		if s := firstText(n).text; s != "width 300" {
			fatalf("the content says %q", s)
		}
		if RootWindow(firstText(n).hwnd) != win {
			fatalf("the content isn't in the window")
		}
		// Resized by the user: the content follows.
		wr := WindowRect(win)
		SetWindowPos(win, 0, 0, 0, wr.W()+h.px(nil, 100), wr.H(), SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE)
		if s := firstText(n).text; s != "width 400" {
			fatalf("resized, the content says %q", s)
		}
		// Its least size.
		var mi MinMaxInfo
		SendMessage(win, WM_GETMINMAXINFO, 0, uintptr(unsafe.Pointer(&mi)))
		least := Rect{Right: h.px(nil, 240), Bottom: h.px(nil, 120)}
		AdjustWindowRect(&least, uint32(WindowLong(win, GWL_STYLE)), 0, DPI(win))
		if mi.MinTrackSize.X != least.W() || mi.MinTrackSize.Y != least.H() {
			fatalf("least %v, want %dx%d", mi.MinTrackSize, least.W(), least.H())
		}
		// The user closes it: the core closes it.
		SendMessage(win, WM_CLOSE, 0, 0)
		if open.Peek() || closed != 1 || len(h.wins) != 0 || IsWindow(win) {
			fatalf("open %v closed %d windows %d", open.Peek(), closed, len(h.wins))
		}
	})
}

func TestMenuBar(t *testing.T) {
	saved, linted := 0, 0
	open := signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{
				{Title: "File", Items: []ui.MenuItem{
					{Title: "Save", Shortcut: ui.Cmd("s"), OnPress: func() { saved++ }},
					ui.MenuSeparator,
					{Title: "Sort & Filter", Shortcut: ui.Cmd("enter").With(ui.Shift), Checked: true},
				}},
				{Title: "Tools", Items: []ui.MenuItem{{Title: "Lint", Shortcut: ui.Cmd("l"), Disabled: true, OnPress: func() { linted++ }}}},
			})),
			ui.Window(ui.WindowProps{Open: open, Content: func() ui.Node { return ui.StaticText("w") }}),
		)
	})
	onMain(t, func() {
		bar := GetMenu(h.window)
		if bar == 0 || MenuItemCount(bar) != 2 {
			fatalf("no menu bar of 2 menus")
		}
		if s := MenuString(bar, 0); s != "&File" {
			fatalf("menu %q", s)
		}
		file := SubMenu(bar, 0)
		if s := MenuString(file, 0); s != "&Save\tCtrl+S" {
			fatalf("item %q", s)
		}
		if MenuState(file, 1)&MF_SEPARATOR == 0 {
			fatalf("no separator")
		}
		if s := MenuString(file, 2); s != "S&ort && Filter\tCtrl+Shift+Enter" {
			fatalf("item %q", s)
		}
		if MenuState(file, 2)&MF_CHECKED == 0 {
			fatalf("not checked")
		}
		tools := SubMenu(bar, 1)
		if MenuState(tools, 0)&MF_GRAYED == 0 {
			fatalf("Lint isn't grayed")
		}
		// Clicked: WM_COMMAND with the item's ID.
		SendMessage(h.window, WM_COMMAND, menuBase+0, 0)
		if saved != 1 {
			fatalf("Save ran %d times", saved)
		}
		SendMessage(h.window, WM_COMMAND, menuBase+3, 0)
		if linted != 0 {
			fatalf("a disabled item ran")
		}
		// Its shortcut, from the main window and from a Window.
		press := func(hwnd uintptr, vk uintptr) {
			saved := KeyboardState()
			st := saved
			st[VK_CONTROL] = 0x80
			SetKeyboardState(st)
			m := Msg{Hwnd: hwnd, Message: WM_KEYDOWN, WParam: vk}
			h.preTranslate(&m)
			SetKeyboardState(saved)
		}
		press(h.window, 'S')
		if saved != 2 {
			fatalf("Ctrl+S in the main window: Save ran %d times", saved)
		}
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		press(h.wins[0].nav.window, 'S')
		if saved != 3 {
			fatalf("Ctrl+S in a Window: Save ran %d times", saved)
		}
		press(h.window, 'L')
		if linted != 0 {
			fatalf("a disabled item's shortcut ran")
		}
	})
}

func TestMenuBarFollows(t *testing.T) {
	menus := signal.NewFunc([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{{Title: "New"}}}}, func(a, b []ui.MenuBarMenu) bool { return false })
	h, _ := mount(t, func() ui.Node { return ui.MenuBar(menus) })
	onMain(t, func() {
		if GetMenu(h.window) == 0 {
			fatalf("no menu bar")
		}
		h.enter(func() { ui.Post(func() { menus.Set(nil) }) })
		h.runPosted()
		if GetMenu(h.window) != 0 || h.accel != 0 {
			fatalf("a menu bar without menus")
		}
	})
}

func TestWindowOptions(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Title: ui.Val("Notes"), Width: 500, Height: 400, MinWidth: 400, MinHeight: 300})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{{Title: "New"}}}})),
			ui.StaticText("x"))
	})
	onMain(t, func() {
		if s := WindowText(h.window); s != "Notes" {
			fatalf("title %q", s)
		}
		// The client size asked for, the menu bar besides.
		if cr := ClientRect(h.window); cr.W() != h.px(nil, 500) || cr.H() != h.px(nil, 400) {
			fatalf("client %dx%d, want %dx%d", cr.W(), cr.H(), h.px(nil, 500), h.px(nil, 400))
		}
		if e := h.env; e.Width != 500 || e.Height != 400 {
			fatalf("environment %vx%v", e.Width, e.Height)
		}
		// The core laid out at it.
		if f := frame(h, find(h, ops.KindText)[0]); f[2] != 500 {
			fatalf("the root's text is %v wide", f[2])
		}

		var mi MinMaxInfo
		SendMessage(h.window, WM_GETMINMAXINFO, 0, uintptr(unsafe.Pointer(&mi)))
		least := Rect{Right: h.px(nil, 400), Bottom: h.px(nil, 300)}
		AdjustWindowRect(&least, uint32(WindowLong(h.window, GWL_STYLE)), 0, DPI(h.window))
		least.Bottom += int32(SystemMetric(SM_CYMENU, DPI(h.window)))
		if mi.MinTrackSize.X != least.W() || mi.MinTrackSize.Y != least.H() {
			fatalf("least %v, want %dx%d", mi.MinTrackSize, least.W(), least.H())
		}
	})
}

func TestWindowDisposed(t *testing.T) {
	onMain(t, func() {
		h := newHost()
		var r *ui.Root
		h.enter(func() {
			r = ui.Mount(h, func() ui.Node {
				return ui.Window(ui.WindowProps{Open: signal.New(true), Content: func() ui.Node { return ui.StaticText("w") }})
			})
		})
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		win := h.wins[0].nav.window
		h.enter(r.Dispose)
		if IsWindow(win) || len(h.wins) != 0 || len(h.byHwnd) != 0 {
			fatalf("the Window's window outlived its owner")
		}
		DestroyWindow(h.window)
	})
}

// A Window moved to a monitor of another DPI (WM_DPICHANGED) takes the
// size Windows suggests and draws at its new scale: its Environment's
// Scale, its fonts in pixels, its frames; the main window keeps its own.
func TestWindowDPI(t *testing.T) {
	open, more := signal.New(true), signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("Hello"),
			ui.Window(ui.WindowProps{Open: open, Width: 300, Height: 200,
				Content: func() ui.Node {
					return ui.Column(ui.Style{},
						ui.Text(signal.Func(func() string { return fmt.Sprint("scale ", ui.UseEnvironment().Get().Scale) })),
						ui.StaticText("Hello"),
						ui.Show(more, func() ui.Node { return ui.StaticText("Later") }))
				}}))
	})
	onMain(t, func() {
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		n := h.wins[0]
		win := n.nav.window
		var main, inWin, label *node
		for _, x := range find(h, ops.KindText) {
			switch {
			case x.text == "Hello" && RootWindow(x.hwnd) == win:
				inWin = x
			case x.text == "Hello":
				main = x
			default:
				label = x
			}
		}
		if main == nil || inWin == nil || label == nil {
			fatalf("texts not found")
		}
		k0 := h.scale
		height := func(x *node) int32 {
			old := SelectObject(h.measureDC, h.nodeFont(x))
			defer SelectObject(h.measureDC, old)
			return TextMetrics(h.measureDC).Height
		}
		mainHeight := height(main)
		if label.text != fmt.Sprint("scale ", k0) || height(inWin) != mainHeight {
			fatalf("before: %q, font %d px (main %d)", label.text, height(inWin), mainHeight)
		}

		// To a monitor at 1.5 times the DPI.
		dpi := h.dpi * 3 / 2
		k := dpiScale(dpi)
		wr := WindowRect(win)
		suggested := Rect{Left: wr.Left, Top: wr.Top, Right: wr.Left + wr.W()*3/2, Bottom: wr.Top + wr.H()*3/2}
		SendMessage(win, WM_DPICHANGED, MakeLong(uint16(dpi), uint16(dpi)), uintptr(unsafe.Pointer(&suggested)))
		if got := WindowRect(win); got != suggested {
			fatalf("the window is %v, Windows suggested %v", got, suggested)
		}
		if n.k != k || inWin.k != k || label.k != k {
			fatalf("scales %v %v %v, want %v", n.k, inWin.k, label.k, k)
		}
		if n.nav.env.Scale != k || label.text != fmt.Sprint("scale ", k) {
			fatalf("the environment's scale is %v, the content says %q", n.nav.env.Scale, label.text)
		}
		if got, want := float32(height(inWin)), float32(mainHeight)*1.5; got < want-2 || got > want+2 {
			fatalf("the window's text is %v px high, want about %v", got, want)
		}
		r := WindowRectIn(inWin.hwnd, Parent(inWin.hwnd))
		if got, want := r.W(), h.px(inWin, inWin.frame[0]+inWin.frame[2])-h.px(inWin, inWin.frame[0]); got != want || inWin.frame[2] <= 0 {
			fatalf("the window's text is %d px wide, its frame %v points: want %d", got, inWin.frame, want)
		}
		if got, want := ClientRect(win).W(), ClientRect(n.hwnd).W(); got != want {
			fatalf("the content is %d px wide in a %d px window", want, got)
		}
		if w, want := n.nav.env.Width, float32(ClientRect(win).W())/k; w != want {
			fatalf("the window is %v points wide, want %v", w, want)
		}

		// The main window keeps its scale.
		if h.scale != k0 || h.env.Scale != k0 || main.k != 0 || height(main) != mainHeight {
			fatalf("main: scale %v env %v k %v font %d px", h.scale, h.env.Scale, main.k, height(main))
		}
		mr := WindowRectIn(main.hwnd, Parent(main.hwnd))
		if got, want := mr.W(), h.px(nil, main.frame[0]+main.frame[2])-h.px(nil, main.frame[0]); got != want {
			fatalf("the main text is %d px wide, want %d", got, want)
		}

		// A view the window's content gets later takes its scale.
		h.enter(func() { ui.Post(func() { more.Set(true) }) })
		h.runPosted()
		var later *node
		for _, x := range find(h, ops.KindText) {
			if x.text == "Later" {
				later = x
			}
		}
		if later == nil || later.k != k || height(later) != height(inWin) {
			fatalf("a text shown later in the window isn't at its scale")
		}

		h.enter(func() { ui.Post(func() { open.Set(false) }) })
		h.runPosted()
		if len(h.wins) != 0 {
			fatalf("still open")
		}
	})
}

// The main window comes back where the user left it: its placement is
// saved (in a temporary file here: a test keeps none of its own) and
// restored over WindowOptions' size; one off every monitor comes back
// on the nearest, and the least size holds.
func TestPlacement(t *testing.T) {
	if placementFile() != "" {
		t.Fatal("a test would keep the window's placement")
	}
	path := filepath.Join(t.TempDir(), placementName)
	ui.SetWindowOptions(ui.WindowOptions{Width: 500, Height: 400, MinWidth: 400, MinHeight: 300})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	app := func() ui.Node {
		return ui.StaticText("Hello")
	}
	h, _ := mount(t, app)
	var want Rect
	onMain(t, func() {
		h.placementPath = path
		work := WorkArea(h.window)
		want = Rect{Left: work.Left + 40, Top: work.Top + 30}
		want.Right, want.Bottom = want.Left+h.px(nil, 600), want.Top+h.px(nil, 500)
		// Not before it has shown.
		h.savePlacement(h.mainCtl)
		if _, err := os.Stat(path); err == nil {
			fatalf("saved before it showed")
		}
		h.mainCtl.placed = true
		// The user moves and sizes it: saved once done.
		h.placementMessage(h.mainCtl, WM_ENTERSIZEMOVE, 0)
		SetWindowPos(h.window, 0, want.Left, want.Top, want.W(), want.H(), SWP_NOZORDER|SWP_NOACTIVATE)
		if _, err := os.Stat(path); err == nil {
			fatalf("saved during the drag")
		}
		h.placementMessage(h.mainCtl, WM_EXITSIZEMOVE, 0)
		if _, err := os.Stat(path); err != nil {
			fatalf("not saved: %v", err)
		}
	})

	// The next launch: where it was, not WindowOptions' size.
	h2, _ := mount(t, app)
	onMain(t, func() {
		h2.placementPath = path
		if !h2.restorePlacement(h2.mainCtl) || h2.mainCtl.restoreMax {
			fatalf("not restored, or restored maximized")
		}
		if got := WindowRect(h2.window); got != want {
			fatalf("restored at %v, want %v", got, want)
		}
	})

	// A file an older host wrote: the main window's alone.
	write := func(p placement) {
		b, _ := json.Marshal(p)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		h2.states = nil // read again
	}
	// Left maximized.
	write(placement{X: want.Left, Y: want.Top, Width: want.W(), Height: want.H(), DPI: h2.dpi, Maximized: true})
	onMain(t, func() {
		if !h2.restorePlacement(h2.mainCtl) || !h2.mainCtl.restoreMax || h2.mainCtl.normal != want {
			fatalf("not restored maximized at %v (%v)", want, h2.mainCtl.normal)
		}
	})
	// On a monitor that's gone: on the nearest, whole; and never smaller
	// than the least size.
	write(placement{X: -100000, Y: -100000, Width: 20, Height: 20, DPI: h2.dpi})
	onMain(t, func() {
		h2.restorePlacement(h2.mainCtl)
		got := WindowRect(h2.window)
		work := MonitorWorkArea(got)
		if got.Left < work.Left || got.Top < work.Top || got.Right > work.Right || got.Bottom > work.Bottom {
			fatalf("restored at %v, off the work area %v", got, work)
		}
		var mi MinMaxInfo
		SendMessage(h2.window, WM_GETMINMAXINFO, 0, uintptr(unsafe.Pointer(&mi)))
		if got.W() < mi.MinTrackSize.X || got.H() < mi.MinTrackSize.Y {
			fatalf("restored %dx%d, under the least %v", got.W(), got.H(), mi.MinTrackSize)
		}
	})
	if c := clampTo(Rect{Left: 900, Top: 100, Right: 1300, Bottom: 400}, Rect{Right: 1000, Bottom: 800}); c != (Rect{Left: 600, Top: 100, Right: 1000, Bottom: 400}) {
		t.Fatalf("mostly off the work area: clamped to %v", c)
	}
	if c := clampTo(Rect{Left: 100, Top: 100, Right: 500, Bottom: 400}, Rect{Right: 1000, Bottom: 800}); c != (Rect{Left: 100, Top: 100, Right: 500, Bottom: 400}) {
		t.Fatalf("on the work area: moved to %v", c)
	}
}

// A menu with a destructive item (a MenuBar item, or an action in a
// popup menu) is owner-drawn, all its items, the destructive one's
// title red, in light and dark; its access keys are still the system's.
func TestDestructiveMenu(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.MenuBar(signal.Static([]ui.MenuBarMenu{
			{Title: "File", Items: []ui.MenuItem{{Title: "Open"}}},
			{Title: "Edit", Items: []ui.MenuItem{
				{Title: "Undo", Shortcut: ui.Cmd("z")},
				ui.MenuSeparator,
				{Title: "Delete All", Shortcut: ui.Cmd("delete"), Destructive: true},
			}},
		}))
	})
	onMain(t, func() {
		bar := GetMenu(h.window)
		file, edit := SubMenu(bar, 0), SubMenu(bar, 1)
		if MenuItemType(file, 0)&MFT_OWNERDRAW != 0 {
			fatalf("a menu without a destructive item is owner-drawn")
		}
		for i := range 3 {
			if MenuItemType(edit, i)&MFT_OWNERDRAW == 0 {
				fatalf("Edit's item %d isn't owner-drawn", i)
			}
		}
		if MenuItemType(edit, 1)&MF_SEPARATOR == 0 {
			fatalf("the separator isn't one")
		}
		if it := h.owned[MenuItemData(edit, 2)]; it == nil || !it.destructive || h.owned[MenuItemData(edit, 0)].destructive {
			fatalf("Delete All isn't the destructive item")
		}

		// Measured as a themed menu's items, a separator shorter.
		measure := func(i int) MeasureItem {
			mi := MeasureItem{CtlType: ODT_MENU, ItemData: MenuItemData(edit, i)}
			if SendMessage(h.window, WM_MEASUREITEM, 0, uintptr(unsafe.Pointer(&mi))) == 0 {
				fatalf("item %d not measured", i)
			}
			return mi
		}
		item, sep := measure(0), measure(1)
		if item.ItemHeight == 0 || item.ItemWidth == 0 || sep.ItemHeight == 0 || sep.ItemHeight >= item.ItemHeight {
			fatalf("measured %v, the separator %v", item, sep)
		}
		if measure(2).ItemWidth != item.ItemWidth {
			fatalf("the items aren't one width")
		}

		// Drawn: the destructive title red, the others not.
		draw := func(i int) (red, text int) {
			w, ht := int32(item.ItemWidth)+60, int32(item.ItemHeight)
			dc := CreateCompatibleDC(0)
			bmp, px := DIB(w, ht)
			old := SelectObject(dc, bmp)
			d := DrawItem{CtlType: ODT_MENU, ItemData: MenuItemData(edit, i), DC: dc, Item: Rect{Right: w, Bottom: ht}}
			SendMessage(h.window, WM_DRAWITEM, 0, uintptr(unsafe.Pointer(&d)))
			bg, r := h.menuBackground(), h.theme.red
			for p := 0; p+3 < len(px); p += 4 {
				b, g, rr := int(px[p]), int(px[p+1]), int(px[p+2])
				near := func(c rgba) bool {
					return abs32(int32(rr)-int32(c.r)) < 24 && abs32(int32(g)-int32(c.g)) < 24 && abs32(int32(b)-int32(c.b)) < 24
				}
				switch {
				case near(r):
					red++
				case !near(bg):
					text++
				}
			}
			SelectObject(dc, old)
			DeleteObject(bmp)
			DeleteDC(dc)
			return red, text
		}
		for _, dark := range []bool{false, true} {
			h.setDark(dark)
			if red, _ := draw(2); red < 20 {
				fatalf("dark %v: the destructive item has %d red pixels", dark, red)
			}
			if red, text := draw(0); red > 0 || text < 20 {
				fatalf("dark %v: Undo has %d red pixels, %d of text", dark, red, text)
			}
		}

		// Its items keep their text, so the system takes their access keys;
		// a key none takes leaves the menu open.
		if s := MenuString(edit, 2); s != "&Delete All\tCtrl+Del" {
			fatalf("the destructive item's text is %q", s)
		}
		if r := SendMessage(h.window, WM_MENUCHAR, MakeLong('x', MF_POPUP), edit); r != 0 {
			fatalf("WM_MENUCHAR x: %#x, want it ignored", r)
		}

		// A popup menu's actions: owner-drawn with a destructive one only.
		m, om := h.actionMenu([]action{{title: "Star"}, {title: "Delete…", role: ops.AlertButtonRoleDestructive}, {title: "Cancel", role: ops.AlertButtonRoleCancel}}, false)
		if om == nil || MenuItemCount(m) != 2 || MenuItemType(m, 0)&MFT_OWNERDRAW == 0 || !h.owned[MenuItemData(m, 1)].destructive {
			fatalf("the popup menu with Delete… isn't owner-drawn")
		}
		DestroyMenu(m)
		h.forget(om)
		if len(h.owned) != 3 {
			fatalf("%d owner-drawn items kept, want the menu bar's 3", len(h.owned))
		}
		m, om = h.actionMenu([]action{{title: "Star"}, {title: "Reply"}}, false)
		if om != nil || MenuItemType(m, 0)&MFT_OWNERDRAW != 0 {
			fatalf("a popup menu without a destructive action is owner-drawn")
		}
		DestroyMenu(m)
	})
}

// setLocale tells the host what the core's Locale command does.
func setLocale(h *Host, rtl bool, tag string) {
	flags := byte(0)
	if rtl {
		flags = 1
	}
	h.enter(func() { h.command(h.root, ops.CmdLocale, append([]byte{flags}, tag...)) })
}

// Right to left, the top-level window is mirrored for its title bar and
// menu bar, and nothing in it: frames stay physical.
func TestLocaleMirrorsOnlyTheTopLevel(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16)}, ui.StaticText("a"))
	})
	onMain(t, func() {
		text := find(h, ops.KindText)[0]
		before, at := frame(h, text), ClientToScreen(text.hwnd, Point{})
		setLocale(h, true, "ar")
		if !h.rtl || h.locale != "ar" {
			fatalf("rtl %v, locale %q", h.rtl, h.locale)
		}
		ex := WindowLong(h.window, GWL_EXSTYLE)
		if ex&WS_EX_LAYOUTRTL == 0 || ex&WS_EX_NOINHERITLAYOUT == 0 {
			fatalf("the window's extended style is %#x", ex)
		}
		for _, n := range []*node{h.root, text} {
			if WindowLong(n.hwnd, GWL_EXSTYLE)&WS_EX_LAYOUTRTL != 0 {
				fatalf("%s is mirrored", n.kind)
			}
		}
		// The root still fills the client area, in its own coordinates.
		if r, c := WindowRectIn(h.root.hwnd, h.window), ClientRect(h.window); r != c {
			fatalf("the root is at %+v in a client area of %+v", r, c)
		}
		if f, p := frame(h, text), ClientToScreen(text.hwnd, Point{}); f != before || p != at {
			fatalf("the text moved: %v at %+v, was %v at %+v", f, p, before, at)
		}
		// A hit test finds it where it shows.
		if w := windowAt(h.window, Point{X: at.X + 1, Y: at.Y + 1}); w != text.hwnd {
			fatalf("the window at the text is %#x, not the text's %#x", w, text.hwnd)
		}
		setLocale(h, false, "en")
		if WindowLong(h.window, GWL_EXSTYLE)&(WS_EX_LAYOUTRTL|WS_EX_NOINHERITLAYOUT) != 0 {
			fatalf("left to right again, its extended style is %#x", WindowLong(h.window, GWL_EXSTYLE))
		}
		if f := frame(h, text); f != before {
			fatalf("left to right again, the text is at %v, was %v", f, before)
		}
	})
}

func TestTextAlignFollowsDirection(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("a"),
			ui.WithDirection(ui.LeftToRight, func() ui.Node { return ui.StaticText("b") }),
			ui.TextField(ui.TextFieldProps{Value: signal.New("kept")}),
		)
	})
	onMain(t, func() {
		var a, b *node
		for _, n := range find(h, ops.KindText) {
			if n.text == "a" {
				a = n
			} else {
				b = n
			}
		}
		field := find(h, ops.KindTextField)[0]
		if f := h.textFormat(a); f&DT_RIGHT != 0 || f&DT_RTLREADING != 0 {
			fatalf("left to right: %#x", f)
		}
		before := field.ctl
		setLocale(h, true, "ar")
		// On the right, reading as its letters do: Latin ones left to right.
		if f := h.textFormat(a); f&DT_RIGHT == 0 || f&DT_RTLREADING != 0 {
			fatalf("Start, right to left: %#x", f)
		}
		latin := a.utext
		a.utext = UTF16Units("مرحبا")
		if f := h.textFormat(a); f&DT_RIGHT == 0 || f&DT_RTLREADING == 0 {
			fatalf("Start, right to left, in Arabic: %#x", f)
		}
		a.utext = latin
		a.textAlign = int32(ops.TextAlignEnd)
		if f := h.textFormat(a); f&DT_RIGHT != 0 {
			fatalf("End, right to left: %#x", f)
		}
		a.textAlign = int32(ops.TextAlignCenter)
		if f := h.textFormat(a); f&DT_CENTER == 0 || f&DT_RIGHT != 0 {
			fatalf("Center, right to left: %#x", f)
		}
		// The view whose LayoutDirection says otherwise keeps it.
		if f := h.textFormat(b); f&DT_RIGHT != 0 || f&DT_RTLREADING != 0 {
			fatalf("a left-to-right view in a right-to-left app: %#x", f)
		}
		// An EDIT takes its reading order as it's made: a new one, with its
		// text, the one before gone.
		if field.ctl == before || IsWindow(before) || h.byHwnd[field.ctl] != field {
			fatalf("the field's EDIT wasn't made again")
		}
		if WindowLong(field.ctl, GWL_STYLE)&ES_RIGHT == 0 || WindowLong(field.ctl, GWL_EXSTYLE)&WS_EX_RTLREADING == 0 {
			fatalf("the field's styles: %#x %#x", WindowLong(field.ctl, GWL_STYLE), WindowLong(field.ctl, GWL_EXSTYLE))
		}
		if s := WindowText(field.ctl); s != "kept" {
			fatalf("the field's text is %q", s)
		}
		// End goes the other way, in an EDIT made again; the same alignment
		// again doesn't make one.
		end := &ops.Value{Int: int64(ops.TextAlignEnd)}
		h.enter(func() { h.setCommon(field, ops.PropTextAlign, end) })
		if WindowLong(field.ctl, GWL_STYLE)&ES_RIGHT != 0 || WindowText(field.ctl) != "kept" {
			fatalf("End, right to left: %#x, %q", WindowLong(field.ctl, GWL_STYLE), WindowText(field.ctl))
		}
		now := field.ctl
		h.enter(func() { h.setCommon(field, ops.PropTextAlign, end) })
		if field.ctl != now {
			fatalf("the same alignment made the EDIT again")
		}
	})
}

func TestHeaderMirrors(t *testing.T) {
	var nav *ui.Navigator
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.StaticText("Home")
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	onMain(t, func() {
		h.enter(func() {
			ui.Post(func() {
				nav.Push(func() ui.Node { return ui.StaticText("Detail") },
					ui.ScreenOptions{Title: ui.Val("Detail"), Actions: []ui.Action{{Title: ui.Val("Save")}}})
			})
		})
		h.runPosted()
		header := find(h, ops.KindStack)[0].nav.bar
		w := ClientRect(header.hwnd).W()
		if r := WindowRectIn(header.nav.back, header.hwnd); r.Left != h.px(header, 6) {
			fatalf("left to right, the back button is at %+v", r)
		}
		if s := WindowText(header.nav.back); s != "Back" {
			fatalf("the back button says %q", s)
		}
		setLocale(h, true, "ar")
		if r := WindowRectIn(header.nav.back, header.hwnd); r.Right != w-h.px(header, 6) {
			fatalf("right to left, the back button is at %+v in %d", r, w)
		}
		if g, _ := header.nav.headerButton(header.nav.back, h.isRTL(header)); g != 0xE72A {
			fatalf("right to left, the back arrow is %#x", g)
		}
		if r := WindowRectIn(header.nav.buttons[0], header.hwnd); r.Left != h.px(header, 8) {
			fatalf("right to left, the bar button is at %+v", r)
		}
		if s := WindowText(header.nav.back); s == "Back" || s != l10n.Back("ar") {
			fatalf("in Arabic, the back button says %q", s)
		}
		setLocale(h, false, "en")
		if r := WindowRectIn(header.nav.back, header.hwnd); r.Left != h.px(header, 6) {
			fatalf("left to right again, the back button is at %+v", r)
		}
	})
}

func TestTabsMirror(t *testing.T) {
	selected := signal.New(0)
	page := func() ui.Node { return ui.StaticText("p") }
	h, _ := mount(t, func() ui.Node {
		return ui.Tabs(ui.TabsProps{Selected: selected},
			ui.Tab{Title: ui.Val("One"), Content: page},
			ui.Tab{Title: ui.Val("Two"), Content: page})
	})
	onMain(t, func() {
		tabs := find(h, ops.KindTabs)[0]
		bar := tabs.nav.bar
		if r := h.tabRects(bar); r[0].Left >= r[1].Left {
			fatalf("left to right, the tabs are at %+v", r)
		}
		setLocale(h, true, "ar")
		r := h.tabRects(bar)
		if r[0].Left <= r[1].Left || r[0].Right != ClientRect(bar.hwnd).W()-h.px(bar, 8) {
			fatalf("right to left, the tabs are at %+v", r)
		}
		// The next tab is to the left.
		SendMessage(bar.hwnd, WM_KEYDOWN, VK_LEFT, 0)
		if tabs.selected != 1 {
			fatalf("Left selected tab %d", tabs.selected)
		}
	})
}

func TestGridColumnsRunFromTheRight(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{Columns: 2, Gap: 10}, signal.Static([]int{1, 2}), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint(item.Get()) }))
			})
	})
	onMain(t, func() {
		l := find(h, ops.KindList)[0].list
		rv := l.shown[0]
		first := func() Rect { return WindowRectIn(rv.cells[0].n.hwnd, rv.n.hwnd) }
		if first().Left != 0 {
			fatalf("left to right, the first column is at %+v", first())
		}
		setLocale(h, true, "ar")
		l.flush(h)
		rv = l.shown[0]
		if r, second := first(), WindowRectIn(rv.cells[1].n.hwnd, rv.n.hwnd); r.Right != ClientRect(rv.n.hwnd).W() || second.Left != 0 {
			fatalf("right to left, the columns are at %+v and %+v", r, second)
		}
	})
}

func TestSliderMirrors(t *testing.T) {
	value := signal.New(0.25)
	h, _ := mount(t, func() ui.Node { return ui.Slider(ui.SliderProps{Value: value}) })
	onMain(t, func() {
		s := find(h, ops.KindSlider)[0]
		if p := SendMessage(s.ctl, TBM_GETPOS, 0, 0); p != sliderSteps/4 {
			fatalf("left to right, the trackbar is at %d", p)
		}
		setLocale(h, true, "ar")
		if p := SendMessage(s.ctl, TBM_GETPOS, 0, 0); p != sliderSteps-sliderSteps/4 {
			fatalf("right to left, the trackbar is at %d", p)
		}
		// The thumb dragged: the value is read from the right.
		SendMessage(s.ctl, TBM_SETPOS, 1, sliderSteps-sliderSteps/10)
		h.sliderMoved(s)
		if s.value != 0.1 {
			fatalf("the value is %v", s.value)
		}
	})
}

// A horizontal Scroll right to left starts at its trailing end and keeps its
// distance from it, its content at its origin, and its offsets physical.
func TestHorizontalScrollKeepsItsEnd(t *testing.T) {
	width := signal.New[float32](300)
	var gotX float32
	h, _ := mount(t, func() ui.Node {
		return ui.WithDirection(ui.RightToLeft, func() ui.Node {
			return ui.Scroll(ui.ScrollProps{Horizontal: true, Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(40)},
				OnScroll: func(x, y float32) { gotX = x }},
				ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style { return ui.Style{Width: ui.Pt(width.Get()), Height: ui.Pt(40)} })}))
		})
	})
	setWidth := func(w float32) {
		h.enter(func() { ui.Post(func() { width.Set(w) }) })
		h.runPosted()
	}
	onMain(t, func() {
		s := find(h, ops.KindScroll)[0]
		content := s.children[0]
		if s.contentW != 300 || s.scrollX != 200 {
			fatalf("content %v at %v, want its end showing", s.contentW, s.scrollX)
		}
		if content.frame[0] != 0 {
			fatalf("the content's frame is at %v, not the origin", content.frame[0])
		}
		if r := WindowRectIn(content.hwnd, s.hwnd); r.Left != h.px(s, -200) {
			fatalf("the content's window is at %+v", r)
		}
		h.scrollToOffset(s, 150, 0) // physical
		if s.scrollX != 150 || s.scrollEnd != 50 || gotX != 150 {
			fatalf("at %v, %v from its end, OnScroll %v", s.scrollX, s.scrollEnd, gotX)
		}
		setWidth(400)
		if s.scrollX != 250 || s.scrollEnd != 50 || gotX != 250 {
			fatalf("grown: at %v, %v from its end, OnScroll %v", s.scrollX, s.scrollEnd, gotX)
		}
		setWidth(120)
		if s.scrollX != 0 {
			fatalf("shrunk: at %v", s.scrollX)
		}
	})
}

// The other direction's end of a left-to-right Scroll stays where it is.
func TestHorizontalScrollLeftToRightStartsAtZero(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Scroll(ui.ScrollProps{Horizontal: true, Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(40)}},
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(300), Height: ui.Pt(40)}}))
	})
	onMain(t, func() {
		if s := find(h, ops.KindScroll)[0]; s.scrollX != 0 {
			fatalf("at %v", s.scrollX)
		}
	})
}

func TestSymbolsMirror(t *testing.T) {
	for _, c := range []struct {
		name string
		rtl  bool
		want string
	}{
		{"chevron.forward", false, "chevron.right"},
		{"chevron.forward", true, "chevron.left"},
		{"chevron.backward", false, "chevron.left"},
		{"chevron.backward", true, "chevron.right"},
		{"chevron.right", true, "chevron.right"},
		{"chevron.left", true, "chevron.left"},
		{"arrow.uturn.forward", false, "arrow.uturn.forward"},
		{"arrow.uturn.forward", true, "arrow.uturn.backward"},
		{"forward.fill", true, "backward.fill"},
		{"star", true, "star"},
	} {
		if got, want := directedGlyph(c.name, c.rtl), symbols[c.want]; got != want || got == 0 {
			t.Errorf("%s (rtl %v): %#x, want %s's %#x", c.name, c.rtl, got, c.want, want)
		}
	}
	if g := directedGlyph("nothing.forward", true); g != 0 {
		t.Errorf("a symbol there's no glyph for: %#x", g)
	}
}

func TestEnvironmentLanguages(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("a") })
	onMain(t, func() {
		if h.env.FirstWeekday > 7 || h.env.HourCycle != 0 && h.env.HourCycle != 12 && h.env.HourCycle != 24 {
			fatalf("first weekday %d, hour cycle %d", h.env.FirstWeekday, h.env.HourCycle)
		}
		if h.env.Languages == "" {
			fatalf("no languages")
		}
	})
	if s := joinLanguages("de", []string{"en-US", "de", "fr"}); s != "de,en-US,fr" {
		t.Errorf("joined %q", s)
	}
	if s := joinLanguages("", []string{"en-US", "fr"}); s != "en-US,fr" {
		t.Errorf("joined %q", s)
	}
}

// The language the app chose is kept in a file of the app's data, comes
// first in Languages at once, and goes as it follows the system again.
func TestSaveLocale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", localeName)
	h, _ := mount(t, func() ui.Node { return ui.StaticText("a") })
	onMain(t, func() {
		h.localePath = path
		system := h.env.Languages
		h.enter(func() { h.command(h.root, ops.CmdSaveLocale, []byte("ar")) })
		if b, err := os.ReadFile(path); err != nil || string(b) != "ar" {
			fatalf("kept %q, %v", b, err)
		}
		if got := readSavedLocale(path); got != "ar" {
			fatalf("read %q", got)
		}
		if h.env.Languages != joinLanguages("ar", h.intl.languages) || h.env.Languages[:2] != "ar" {
			fatalf("languages %q", h.env.Languages)
		}
		h.enter(func() { h.command(h.root, ops.CmdSaveLocale, nil) })
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			fatalf("kept after following the system again: %v", err)
		}
		if h.env.Languages != system {
			fatalf("languages %q, was %q", h.env.Languages, system)
		}
		// What isn't a language isn't read.
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("not a language tag at all"), 0o644)
		if got := readSavedLocale(path); got != "" {
			fatalf("read %q", got)
		}
	})
}

func TestWindowTitleCommand(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("a") })
	onMain(t, func() {
		h.enter(func() { h.command(h.root, ops.CmdWindowTitle, []byte("مرحبا")) })
		if s := WindowText(h.window); s != "مرحبا" {
			fatalf("title %q", s)
		}
		h.enter(func() { h.command(h.root, ops.CmdWindowTitle, nil) })
		if s := WindowText(h.window); s != h.appName {
			fatalf("title %q, the app's name %q", s, h.appName)
		}
	})
}

// A menu bar made right to left, and again as the direction changes.
func TestMenuBarMirrors(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{{Title: "New"}}}})),
			ui.StaticText("x"))
	})
	onMain(t, func() {
		bar := GetMenu(h.window)
		setLocale(h, true, "ar")
		now := GetMenu(h.window)
		if now == 0 || now == bar {
			fatalf("the menu bar wasn't made again")
		}
		if MenuItemType(SubMenu(now, 0), 0)&MF_RIGHTORDER == 0 {
			fatalf("its item's type is %#x", MenuItemType(SubMenu(now, 0), 0))
		}
	})
}

// A screen's search field is in its stack's header: typing writes the
// screen's Text, Enter submits, and code's text shows in it.
func TestHeaderSearch(t *testing.T) {
	q := signal.New("")
	submitted := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("Home") }, ui.ScreenOptions{
			Title:  ui.Val("Home"),
			Search: &ui.Search{Text: q, OnSubmit: func() { submitted++ }},
		})
	})
	onMain(t, func() {
		header := find(h, ops.KindStack)[0].nav.bar
		f := header.nav.field
		if f == nil || !shown(f.hwnd) {
			fatalf("no search field in the header")
		}
		SetWindowText(f.ctl, "cats")
		if q.Peek() != "cats" {
			fatalf("typed: Text %q", q.Peek())
		}
		SendMessage(f.ctl, WM_KEYDOWN, VK_RETURN, 0)
		if submitted != 1 {
			fatalf("Enter: submitted %d times", submitted)
		}
		h.enter(func() { ui.Post(func() { q.Set("dogs") }) }) // flushed as the app's code is
		h.runPosted()
		if WindowText(f.ctl) != "dogs" {
			fatalf("code's text: %q", WindowText(f.ctl))
		}
	})
}

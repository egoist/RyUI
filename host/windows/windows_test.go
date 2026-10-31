//go:build windows && amd64

package windows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

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
	ui.SetWindowOptions(ui.WindowOptions{Title: "Notes", Width: 500, Height: 400, MinWidth: 400, MinHeight: 300})
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
		h.savePlacement()
		if _, err := os.Stat(path); err == nil {
			fatalf("saved before it showed")
		}
		h.placed = true
		// The user moves and sizes it: saved once done.
		h.placementMessage(WM_ENTERSIZEMOVE, 0)
		SetWindowPos(h.window, 0, want.Left, want.Top, want.W(), want.H(), SWP_NOZORDER|SWP_NOACTIVATE)
		if _, err := os.Stat(path); err == nil {
			fatalf("saved during the drag")
		}
		h.placementMessage(WM_EXITSIZEMOVE, 0)
		if _, err := os.Stat(path); err != nil {
			fatalf("not saved: %v", err)
		}
	})

	// The next launch: where it was, not WindowOptions' size.
	h2, _ := mount(t, app)
	onMain(t, func() {
		h2.placementPath = path
		if h2.restorePlacement() {
			fatalf("restored maximized")
		}
		if got := WindowRect(h2.window); got != want {
			fatalf("restored at %v, want %v", got, want)
		}
	})

	write := func(p placement) {
		b, _ := json.Marshal(p)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Left maximized.
	write(placement{X: want.Left, Y: want.Top, Width: want.W(), Height: want.H(), DPI: h2.dpi, Maximized: true})
	onMain(t, func() {
		if !h2.restorePlacement() || h2.normal != want {
			fatalf("not restored maximized at %v (%v)", want, h2.normal)
		}
	})
	// On a monitor that's gone: on the nearest, whole; and never smaller
	// than the least size.
	write(placement{X: -100000, Y: -100000, Width: 20, Height: 20, DPI: h2.dpi})
	onMain(t, func() {
		h2.restorePlacement()
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

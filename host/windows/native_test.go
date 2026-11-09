//go:build windows && amd64

package windows

import (
	"testing"
	"time"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/native"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// testButton is an app's native view: a Win32 button made in its
// parent, whose "title" prop it shows, and which emits "clicked" from
// the BN_CLICKED its parent gets.
type testButton struct {
	ctx       native.Context
	hwnd      uintptr
	destroyed bool
}

func (b *testButton) Handle() uintptr { return b.hwnd }

func (b *testButton) Set(name string, v any) {
	if s, ok := v.(string); ok && name == "title" {
		SetWindowText(b.hwnd, s)
	}
}

func (b *testButton) Measure(w, h float32) (float32, float32) { return 120, 32 }
func (b *testButton) Destroy()                                { b.destroyed = true }

func (b *testButton) ParentMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	if m == WM_COMMAND && lp == b.hwnd && HiWord(wp) == 0 { // BN_CLICKED
		b.ctx.Emit("clicked", map[string]any{"n": 1})
		return 0, true
	}
	return 0, false
}

// A native view the app registers: made in its node's view, filling it,
// its props set, its control's notifications its events; destroyed with
// the node. A module the app registers answers ui.Call.
func TestNativeView(t *testing.T) {
	var made *testButton
	native.RegisterView("TestButton", func(ctx native.Context) native.View {
		made = &testButton{ctx: ctx, hwnd: CreateWindow(0, "BUTTON", "", WS_CHILD|WS_VISIBLE|BS_PUSHBUTTON, 0, 0, 0, 0, ctx.Parent(), 1)}
		return made
	})
	native.RegisterModule("testEcho", native.Module{
		"echo": func(args []byte, reply func([]byte, error)) { reply(append([]byte("echo:"), args...), nil) },
	})
	title := signal.New("One")
	shown := signal.New(true)
	clicks := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Show(shown, func() ui.Node {
				return ui.Native("TestButton", ui.NativeProps{
					Props: map[string]any{"title": signal.Accessor[string](title)},
					On:    map[string]func(ui.NativeEvent){"clicked": func(e ui.NativeEvent) { clicks += int(e.Int("n")) }},
				})
			}))
	})
	onMain(t, func() {
		if made == nil {
			fatalf("the factory wasn't called")
		}
		n := find(h, ops.KindNative)[0]
		if f := frame(h, n); f[2] != 120 || f[3] != 32 {
			fatalf("frame %v, want 120x32", f)
		}
		if Parent(made.hwnd) != n.hwnd || ClientRect(made.hwnd).W() != ClientRect(n.hwnd).W() {
			fatalf("the app's window doesn't fill its node's")
		}
		if WindowText(made.hwnd) != "One" {
			fatalf("title %q", WindowText(made.hwnd))
		}
		h.enter(func() { ui.Post(func() { title.Set("Two") }) })
		h.runPosted()
		if WindowText(made.hwnd) != "Two" {
			fatalf("title after a change %q", WindowText(made.hwnd))
		}
		SendMessage(made.hwnd, BM_CLICK, 0, 0)
		if clicks != 1 {
			fatalf("clicks %d", clicks)
		}
		if b, err, _ := await(h, "testEcho", "echo", []byte("hi")); err != nil || string(b) != "echo:hi" {
			fatalf("the app's module: %q, %v", b, err)
		}
		h.enter(func() { ui.Post(func() { shown.Set(false) }) })
		h.runPosted()
		if !made.destroyed {
			fatalf("not destroyed with its node")
		}
	})
}

// F5 refreshes the refreshable Scroll on screen: OnRefresh, and the
// spinner until Refreshing goes false.
func TestRefreshKey(t *testing.T) {
	refreshing := signal.New(false)
	pulls := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Scroll(ui.ScrollProps{Refreshing: refreshing, OnRefresh: func() { pulls++; refreshing.Set(true) }},
			ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(2000)}}))
	})
	onMain(t, func() {
		showHost(h)
		n := find(h, ops.KindScroll)[0]
		m := Msg{Hwnd: n.hwnd, Message: WM_KEYDOWN, WParam: vkF5}
		if !h.keyMessage(&m) || pulls != 1 {
			fatalf("F5: %d refreshes", pulls)
		}
		h.runPosted()
		if n.refresh == nil || !n.refresh.refreshing || n.refresh.spinner == nil || !IsWindowVisible(n.refresh.spinner.hwnd) {
			fatalf("no spinner while refreshing")
		}
		h.enter(func() { ui.Post(func() { refreshing.Set(false) }) })
		h.runPosted()
		if n.refresh.refreshing || IsWindowVisible(n.refresh.spinner.hwnd) {
			fatalf("the spinner stayed")
		}
	})
}

// A view's shadow is drawn by the view it's in, around it: dark below
// the card (its offset), nothing far away.
func TestShadow(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(40), Align: ui.AlignStart},
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(60), Background: ui.RGB(255, 255, 255), CornerRadius: 8,
				Shadow: ui.Shadow{Color: ui.RGBA(0, 0, 0, 90), Radius: 6, Y: 4}}}))
	})
	onMain(t, func() {
		showHost(h)
		var card *node
		for _, n := range find(h, ops.KindView) {
			if len(n.shadow) > 0 {
				card = n
			}
		}
		if card == nil {
			fatalf("no view with a shadow")
		}
		parent := card.parent
		px := ownPixels(h, parent)
		w := ClientRect(parent.hwnd).W()
		k := h.scaleOf(parent)
		alpha := func(x, y float32) byte { return px[(int(y*k)*int(w)+int(x*k))*4+3] }
		if a := alpha(90, 40+60+3); a == 0 { // just under the card
			fatalf("no shadow under the card")
		}
		if a := alpha(5, 5); a != 0 {
			fatalf("shadow far from the card (alpha %d)", a)
		}
	})
}

// A view with a transition comes in from its transition's frame during
// an animation, and going out keeps its window until the animation ends.
func TestTransitions(t *testing.T) {
	shown := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Show(shown, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(40), Background: ui.Red, Transition: ui.SlideUp}},
					ui.StaticText("toast"))
			}))
	})
	onMain(t, func() {
		showHost(h)
		toggle := func(v bool) {
			h.enter(func() { ui.Post(func() { ui.Animate(func() { shown.Set(v) }) }) })
			h.runPosted()
		}
		toggle(true)
		var toast *node
		for _, n := range find(h, ops.KindView) {
			if n.transition != nil {
				toast = n
			}
		}
		if toast == nil || h.anim == nil || h.anim.tweens[toast] == nil {
			fatalf("the toast didn't come in with its transition")
		}
		if tw := h.anim.tweens[toast]; tw.from == tw.to {
			fatalf("it came in from where it ends")
		}
		finish := func() {
			h.anim.start = h.anim.start.Add(-time.Hour)
			h.anim.step(h)
		}
		finish()
		hwnd := toast.hwnd
		toggle(false)
		if h.nodes[toast.id] != nil || h.exiting[hwnd] == nil || !IsWindow(hwnd) || !IsWindowVisible(hwnd) {
			fatalf("the toast left at once")
		}
		finish()
		if IsWindow(hwnd) || len(h.exiting) != 0 {
			fatalf("the toast stayed after its animation")
		}
	})
}

// A view with a key destroyed during an animation waits in its window,
// where it was, for the one inserted with its key, which starts at its
// place and size; the old one goes as the animation ends.
func TestMatch(t *testing.T) {
	big := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Show(big, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(150), Background: ui.Blue, Match: "card"}})
			}, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(40), Height: ui.Pt(30), Background: ui.Blue, Match: "card"}})
			}))
	})
	onMain(t, func() {
		showHost(h)
		var small *node
		for _, n := range find(h, ops.KindView) {
			if n.match == "card" {
				small = n
			}
		}
		old := small.hwnd
		h.enter(func() { ui.Post(func() { ui.Animate(func() { big.Set(true) }) }) })
		h.runPosted()
		if Parent(old) != h.window || h.exiting[old] == nil {
			fatalf("the old view didn't wait in the window")
		}
		var large *node
		for _, n := range find(h, ops.KindView) {
			if n.match == "card" {
				large = n
			}
		}
		tw := h.anim.tweens[large]
		near := func(a, b float32) bool { return a-b < 0.5 && b-a < 0.5 }
		if tw == nil || !near(tw.from[2], 40) || !near(tw.from[3], 30) || !near(tw.to[2], 200) {
			fatalf("the new view's tween %+v", tw)
		}
		h.anim.start = h.anim.start.Add(-time.Hour)
		h.anim.step(h)
		if IsWindow(old) {
			fatalf("the old view stayed")
		}
		if f := frame(h, large); f[2] != 200 || f[3] != 150 {
			fatalf("the new view ended at %v", f)
		}
	})
}

// UI Automation: each view answers WM_GETOBJECT with its provider,
// which names a pressable row by its texts, a text by its text, says a
// header is a heading and a decoration is skipped, and invokes a press;
// an adjustable view takes the arrow keys.
func TestAccessibility(t *testing.T) {
	presses, adjusted := 0, 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Gap: 8},
			ui.View(ui.ViewProps{OnPress: func() { presses++ }, Hint: ui.Val("Opens it"), Style: ui.Style{Height: ui.Pt(40)}},
				ui.StaticText("Inbox"), ui.StaticText("3 new")),
			ui.View(ui.ViewProps{Role: ui.RoleHeader, Label: ui.Val("Today"), Style: ui.Style{Height: ui.Pt(20)}}),
			ui.View(ui.ViewProps{Decorative: true, Style: ui.Style{Height: ui.Pt(20)}}, ui.StaticText("deco")),
			ui.View(ui.ViewProps{Label: ui.Val("Volume"), OnAdjust: func(by int) { adjusted += by }, Style: ui.Style{Height: ui.Pt(20)}}),
		)
	})
	onMain(t, func() {
		showHost(h)
		row := viewListening(h, ops.EventPress)
		root := int64(UiaRootObjectID)
		if r := SendMessage(row.hwnd, WM_GETOBJECT, 0, uintptr(root)); r == 0 || row.uia == 0 {
			fatalf("no provider for WM_GETOBJECT")
		}
		acc := func(n *node) accessible { return accessible{h: h, n: n} }
		if name := acc(row).Property(UIA_NamePropertyId); name != "Inbox, 3 new" {
			fatalf("the row's name %v", name)
		}
		if ct := acc(row).Property(UIA_ControlTypePropertyId); ct != int32(UIA_ButtonControlTypeId) {
			fatalf("the row's control type %v", ct)
		}
		if help := acc(row).Property(UIA_HelpTextPropertyId); help != "Opens it" {
			fatalf("the row's help %v", help)
		}
		acc(row).Invoke()
		if presses != 1 {
			fatalf("Invoke: %d presses", presses)
		}
		for _, n := range h.nodes {
			switch {
			case n.a11yLabel == "Today":
				if acc(n).Property(UIA_HeadingLevelPropertyId) != int32(HeadingLevel1) {
					fatalf("the header isn't a heading")
				}
			case n.kind == ops.KindText && n.text == "deco":
				if acc(n).Property(UIA_IsControlElementPropertyId) != false {
					fatalf("a decoration's text isn't skipped")
				}
			case n.kind == ops.KindText && n.text == "Inbox":
				if acc(n).Property(UIA_NamePropertyId) != "Inbox" {
					fatalf("a text's name")
				}
			case n.adjustable:
				SendMessage(n.hwnd, WM_KEYDOWN, VK_UP, 0)
				SendMessage(n.hwnd, WM_KEYDOWN, VK_UP, 0)
				SendMessage(n.hwnd, WM_KEYDOWN, VK_DOWN, 0)
				if adjusted != 1 {
					fatalf("adjusted by %d, want 1", adjusted)
				}
			}
		}
	})
}

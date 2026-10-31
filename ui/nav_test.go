package ui_test

import (
	"fmt"
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func TestStackPushPopDisposesTheScreen(t *testing.T) {
	ticks := signal.New(0)
	runs := 0
	var nav *ui.Navigator
	detail := func() ui.Node {
		signal.Effect(func() { ticks.Get(); runs++ })
		return ui.Column(ui.Style{},
			ui.Text(signal.Func(func() string { return fmt.Sprint("tick ", ticks.Get()) })),
			ui.Show(signal.Func(func() bool { return ticks.Get() > 5 }), func() ui.Node { return ui.StaticText("many") }),
		)
	}
	h, r := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.Button(ui.ButtonProps{Title: ui.Val("Open"), OnPress: func() {
				nav.Push(detail, ui.ScreenOptions{Title: ui.Val("Detail")})
			}})
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	stack := h.OfKind(ops.KindStack)[0]
	if len(stack.Children) != 1 || stack.Children[0].String(ops.PropScreenTitle) != "Home" {
		t.Fatalf("stack:\n%s", stack.Tree())
	}
	h.Tap(h.One(ops.KindButton, "Open"))
	if len(stack.Children) != 2 || nav.Depth() != 2 || stack.Children[1].Text() != "tick 0" {
		t.Fatalf("after push:\n%s", stack.Tree())
	}
	// The screen fills the stack, as the host sized it.
	if f := stack.Children[1].Children[0].Frame; f[2] != 390 {
		t.Fatalf("detail text frame %v", f)
	}

	// Pop from code: the screen's effects stop and its views go.
	views := len(h.Views)
	nav.Pop()
	r.Flush()
	if len(stack.Children) != 1 || nav.Depth() != 1 || len(h.Views) != views-2 {
		t.Fatalf("after pop: %d children, %d views (was %d)\n%s", len(stack.Children), len(h.Views), views, stack.Tree())
	}
	runs = 0
	ticks.Set(10)
	r.Flush()
	if runs != 0 {
		t.Fatal("an effect of a popped screen ran")
	}

	// The user's back gesture: the host pops, then reports.
	h.Tap(h.One(ops.KindButton, "Open"))
	h.Back(stack)
	runs = 0
	ticks.Set(11)
	r.Flush()
	if runs != 0 || nav.Depth() != 1 || len(h.OfKind(ops.KindScreen)) != 1 {
		t.Fatalf("after back: runs %d depth %d screens %d", runs, nav.Depth(), len(h.OfKind(ops.KindScreen)))
	}
}

// A Scroll filling a screen is the screen's height, its content taller:
// filling it keeps what makes it a Scroll (its overflow).
func TestScreenScroll(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			var kids []ui.Node
			for range 60 {
				kids = append(kids, ui.StaticText("line"))
			}
			return ui.Scroll(ui.ScrollProps{}, kids...)
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	h.Dispatch(ops.Event{ID: h.OfKind(ops.KindScreen)[0].ID, Kind: ops.EventEnvironment, Env: h.Env})
	scroll := h.OfKind(ops.KindScroll)[0]
	if f := scroll.Frame; f[3] != h.Env.Height {
		t.Fatalf("scroll frame %v, want the screen's height %v", f, h.Env.Height)
	}
	if f := scroll.Children[0].Frame; f[3] <= h.Env.Height {
		t.Fatalf("content frame %v", f)
	}
}

func TestScreenSafeAreaUsesTheScreensEnvironment(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			return ui.SafeArea(ui.Style{}, ui.StaticText("hi"))
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	screen := h.OfKind(ops.KindScreen)[0]
	env := h.Env
	env.SafeTop = 100 // under a navigation bar
	h.Dispatch(ops.Event{ID: screen.ID, Kind: ops.EventEnvironment, Env: env})
	if f := h.One(ops.KindText, "hi").Frame; f[1] != 100 {
		t.Fatalf("text frame %v", f)
	}
}

func TestScreenActions(t *testing.T) {
	saved := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("x") },
			ui.ScreenOptions{Title: ui.Val("Edit"), Actions: []ui.Action{{Title: "Save", OnPress: func() { saved++ }}}})
	})
	screen := h.OfKind(ops.KindScreen)[0]
	if got := screen.Actions(ops.PropScreenActions); len(got) != 1 || got[0] != "Save" {
		t.Fatal(got)
	}
	h.Choose(screen, 0)
	if saved != 1 {
		t.Fatal("bar button didn't run")
	}
}

func TestTabs(t *testing.T) {
	sel := signal.New(0)
	h, r := mount(t, func() ui.Node {
		return ui.Tabs(ui.TabsProps{Selected: sel},
			ui.Tab{Title: "Home", Icon: ui.IconHome, Content: func() ui.Node { return ui.StaticText("home page") }},
			ui.Tab{Title: "Settings", Icon: ui.IconSettings, Content: func() ui.Node { return ui.StaticText("settings page") }},
		)
	})
	tabs := h.OfKind(ops.KindTabs)[0]
	if len(tabs.Children) != 2 || tabs.Children[1].String(ops.PropScreenIcon) != "gearshape" || tabs.Children[1].Text() != "settings page" {
		t.Fatalf("tabs:\n%s", tabs.Tree())
	}
	h.Reset()
	h.Select(tabs, 1)
	if sel.Peek() != 1 || h.Count(ops.OpSetProp) != 0 { // the user's choice isn't echoed
		t.Fatalf("selected %d\n%s", sel.Peek(), h.Log())
	}
	sel.Set(0)
	r.Flush()
	if tabs.Props[ops.PropTabsSelected].Int != 0 {
		t.Fatal("selecting from code")
	}
}

func TestSheet(t *testing.T) {
	open := signal.New(false)
	runs, dismissed := 0, 0
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Sheet(ui.SheetProps{Open: open, Detents: ui.DetentMedium | ui.DetentLarge, Grabber: true,
				OnDismiss: func() { dismissed++ },
				Content: func() ui.Node {
					signal.Effect(func() { open.Get(); runs++ })
					return ui.SafeArea(ui.Style{Padding: ui.All(16)}, ui.StaticText("in the sheet"))
				}}),
		)
	})
	if len(h.Presented()) != 0 {
		t.Fatal("presented while closed")
	}
	open.Set(true)
	r.Flush()
	ps := h.Presented()
	if len(ps) != 1 || ps[0].Kind != ops.KindSheet || ps[0].Text() != "in the sheet" || ps[0].Props[ops.PropSheetDetents].Int != 3 {
		t.Fatalf("presented %v", ps)
	}
	// Closing from code destroys it.
	open.Set(false)
	r.Flush()
	if len(h.Presented()) != 0 || len(h.OfKind(ops.KindSheet)) != 0 {
		t.Fatal("sheet left after close")
	}
	// The user's dismissal sets Open false.
	open.Set(true)
	r.Flush()
	h.Dismiss(h.Presented()[0])
	if open.Peek() || dismissed != 1 || len(h.OfKind(ops.KindSheet)) != 0 {
		t.Fatalf("open %v dismissed %d", open.Peek(), dismissed)
	}
	runs = 0
	open.Set(true)
	open.Set(false)
	r.Flush()
	if runs != 1 { // the new content's effect ran once; the old one's never
		t.Fatalf("runs %d", runs)
	}
}

func TestAlert(t *testing.T) {
	deleted := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Delete"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "Delete?", Message: "This can't be undone.", Actions: []ui.Action{
				{Title: "Cancel", Role: ui.ActionCancel},
				{Title: "Delete", Role: ui.ActionDestructive, OnPress: func() { deleted++ }},
			}})
		}})
	})
	h.Tap(h.One(ops.KindButton, "Delete"))
	ps := h.Presented()
	if len(ps) != 1 || ps[0].String(ops.PropAlertTitle) != "Delete?" || fmt.Sprint(ps[0].Actions(ops.PropAlertButtons)) != "[Cancel Delete]" {
		t.Fatalf("presented %v", ps)
	}
	h.Choose(ps[0], 1)
	if deleted != 1 || len(h.OfKind(ops.KindAlert)) != 0 {
		t.Fatalf("deleted %d, alerts %d", deleted, len(h.OfKind(ops.KindAlert)))
	}
	h.Tap(h.One(ops.KindButton, "Delete"))
	h.Dismiss(h.Presented()[0])
	if deleted != 1 || len(h.OfKind(ops.KindAlert)) != 0 {
		t.Fatal("dismissal")
	}
}

func TestMenuAndButtonMenu(t *testing.T) {
	var picked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{},
			ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{{Title: "Copy", OnPress: func() { picked = append(picked, "copy") }}}},
				ui.StaticText("hold me")),
			ui.Button(ui.ButtonProps{Title: ui.Val("More"), Menu: []ui.Action{
				{Title: "Share", OnPress: func() { picked = append(picked, "share") }},
				{Title: "Remove", Role: ui.ActionDestructive, OnPress: func() { picked = append(picked, "remove") }},
			}}),
		)
	})
	menu := h.One(ops.KindText, "hold me").Parent
	if menu.Props[ops.PropMenuTrigger].Int != int64(ops.MenuTriggerLongPress) {
		t.Fatal("trigger")
	}
	h.Choose(menu, 0)
	h.Choose(h.One(ops.KindButton, "More"), 1)
	if fmt.Sprint(picked) != "[copy remove]" {
		t.Fatal(picked)
	}
}

func TestPullToRefresh(t *testing.T) {
	refreshing := signal.New(false)
	var done func()
	h, r := mount(t, func() ui.Node {
		return ui.Scroll(ui.ScrollProps{
			Refreshing: refreshing,
			OnRefresh: func() {
				refreshing.Set(true)
				done = func() { refreshing.Set(false) }
			},
		}, ui.StaticText("content"))
	})
	sc := h.OfKind(ops.KindScroll)[0]
	if !sc.Bool(ops.PropScrollRefreshable) {
		t.Fatal("not refreshable")
	}
	h.Reset()
	h.PullToRefresh(sc)
	if h.Count(ops.OpSetProp) != 0 { // the host already shows its spinner
		t.Fatalf("\n%s", h.Log())
	}
	done()
	r.Flush()
	if sc.Bool(ops.PropScrollRefreshing) || h.Count(ops.OpSetProp) != 1 {
		t.Fatalf("\n%s", h.Log())
	}
	// An OnRefresh that finishes at once ends the spinner at once.
	h2 := fake.New()
	r2 := ui.Mount(h2, func() ui.Node {
		return ui.Scroll(ui.ScrollProps{OnRefresh: func() {}}, ui.StaticText("x"))
	})
	defer r2.Dispose()
	sc2 := h2.OfKind(ops.KindScroll)[0]
	h2.Reset()
	h2.PullToRefresh(sc2)
	if h2.Count(ops.OpSetProp) != 1 || sc2.Bool(ops.PropScrollRefreshing) {
		t.Fatalf("\n%s", h2.Log())
	}
}

func TestHaptic(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Buzz"), OnPress: func() { ui.Haptic(ui.HapticSuccess) }})
	})
	h.Reset()
	h.Tap(h.One(ops.KindButton, "Buzz"))
	cs := h.Commands(ops.CmdHaptic)
	if len(cs) != 1 || cs[0].ID != h.Root().ID || cs[0].Value.Bytes[0] != byte(ops.HapticSuccess) {
		t.Fatal(cs)
	}
}

func TestScreenTitleFollowsItsSignal(t *testing.T) {
	selected := signal.New(0)
	var nav *ui.Navigator
	h, r := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.StaticText("home")
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	nav.Push(func() ui.Node { return ui.StaticText("detail") }, ui.ScreenOptions{
		Title: signal.Func(func() string { return fmt.Sprint(selected.Get(), " selected") }),
	})
	r.Flush()
	screen := h.OfKind(ops.KindScreen)[1]
	selected.Set(3)
	r.Flush()
	if got := screen.String(ops.PropScreenTitle); got != "3 selected" {
		t.Fatal(got)
	}
	nav.Pop()
	r.Flush()
	h.Reset()
	selected.Set(4)
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 { // the popped screen's title effect is gone
		t.Fatalf("\n%s", h.Log())
	}
}

// A growing view around a Scroll is held to the screen (its minimum
// height is 0, as React Native's flex: 1), so the Scroll scrolls and an
// overlay pinned to the wrapper's bottom shows above the safe area.
func TestGrowingWrapperAroundScroll(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			env := ui.UseEnvironment()
			var rows []ui.Node
			for range 100 {
				rows = append(rows, ui.StaticText("row"))
			}
			return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}},
				ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, rows...),
				ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
					return ui.Style{Absolute: true, Left: ui.Pt(20), Right: ui.Pt(20), Bottom: ui.Pt(env.Get().SafeBottom + 12), Height: ui.Pt(48), Background: ui.Red}
				})}))
		}, ui.ScreenOptions{})
	})
	h.Dispatch(ops.Event{ID: h.OfKind(ops.KindScreen)[0].ID, Kind: ops.EventEnvironment, Env: h.Env})
	if f := h.OfKind(ops.KindScroll)[0].Frame; f[3] != h.Env.Height {
		t.Fatalf("scroll frame %v, want the screen's height", f)
	}
	var toast *fake.View
	for _, v := range h.OfKind(ops.KindView) {
		if v.Props[ops.PropBackground].Int != 0 {
			toast = v
		}
	}
	if f := toast.Frame; f[1]+f[3] != h.Env.Height-h.Env.SafeBottom-12 {
		t.Fatalf("toast frame %v", f)
	}
}

// A screen's search field: its placeholder, typing writes Text, code's
// text is sent.
func TestScreenSearch(t *testing.T) {
	q := signal.New("")
	submitted := 0
	h, r := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("list") },
			ui.ScreenOptions{Title: ui.Val("Inbox"), Search: &ui.Search{Text: q, OnSubmit: func() { submitted++ }}})
	})
	s := h.OfKind(ops.KindScreen)[0]
	if s.String(ops.PropScreenSearch) != "Search" {
		t.Fatalf("placeholder %q", s.String(ops.PropScreenSearch))
	}
	h.Dispatch(ops.Event{ID: s.ID, Kind: ops.EventTextChanged, Str: "ada"})
	h.Dispatch(ops.Event{ID: s.ID, Kind: ops.EventSubmit})
	if q.Peek() != "ada" || submitted != 1 {
		t.Fatalf("q %q submitted %d", q.Peek(), submitted)
	}
	q.Set("")
	r.Flush()
	if s.String(ops.PropScreenSearchText) != "" {
		t.Fatal("code's text not sent")
	}
}

// ZoomFrom names the view the pushed screen zooms out of; the screen
// below keeps its key.
func TestZoomFrom(t *testing.T) {
	var nav *ui.Navigator
	h, r := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(80), Match: "photo-1"}})
		}, ui.ScreenOptions{Title: ui.Val("Photos")})
	})
	nav.Push(func() ui.Node { return ui.StaticText("photo") }, ui.ScreenOptions{ZoomFrom: "photo-1"})
	r.Flush()
	screens := h.OfKind(ops.KindScreen)
	if len(screens) != 2 || screens[1].String(ops.PropScreenZoomFrom) != "photo-1" || screens[0].String(ops.PropScreenZoomFrom) != "" {
		t.Fatalf("\n%s", h.Log())
	}
}

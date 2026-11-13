package ui_test

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
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
			ui.ScreenOptions{Title: ui.Val("Edit"), Actions: []ui.Action{{Title: ui.Val("Save"), OnPress: func() { saved++ }}}})
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
			ui.Tab{Title: ui.Val("Home"), Icon: ui.IconHome, Content: func() ui.Node { return ui.StaticText("home page") }},
			ui.Tab{Title: ui.Val("Settings"), Icon: ui.IconSettings, Content: func() ui.Node { return ui.StaticText("settings page") }},
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

// A tab's badge is its page's Badge, sent again alone as it changes,
// and no more once the tabs are gone.
func TestTabBadges(t *testing.T) {
	unread := signal.New(3)
	shown := signal.New(true)
	h, r := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			return ui.Tabs(ui.TabsProps{},
				ui.Tab{Title: ui.Val("Inbox"), Badge: signal.Func(func() string {
					if n := unread.Get(); n > 0 {
						return strconv.Itoa(n)
					}
					return ""
				}), Content: func() ui.Node { return ui.StaticText("inbox") }},
				ui.Tab{Title: ui.Val("Archive"), Content: func() ui.Node { return ui.StaticText("archive") }},
				ui.Tab{Title: ui.Val("Settings"), Badge: ui.Val("New"), Content: func() ui.Node { return ui.StaticText("settings") }},
			)
		})
	})
	tabs := h.OfKind(ops.KindTabs)[0]
	if got := tabs.Badges(); !slices.Equal(got, []string{"3", "", "New"}) {
		t.Fatalf("badges %q", got)
	}
	if _, sent := tabs.Children[1].Props[ops.PropScreenBadge]; sent {
		t.Fatal("a tab without a badge was sent one")
	}
	h.Reset()
	unread.Set(12)
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 || h.Count(ops.OpCreate) != 0 || tabs.Badges()[0] != "12" {
		t.Fatalf("a badge changing:\n%s", h.Log())
	}
	unread.Set(0)
	r.Flush()
	if tabs.Badges()[0] != "" {
		t.Fatal("the badge stayed")
	}
	shown.Set(false)
	r.Flush()
	h.Reset()
	unread.Set(5)
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("a gone tab's badge is still sent:\n%s", h.Log())
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

// A full-screen sheet: FullScreen goes before Present, without Detents
// or Grabber; its environment is the screen's own, so its safe area pads
// its content under the status bar and above the home indicator; going
// back dismisses it unless OnBack keeps it; closing disposes its content.
func TestSheetFullScreen(t *testing.T) {
	open := signal.New(false)
	keep := false
	var env ui.Environment
	runs, cleanups, dismissed := 0, 0, 0
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Sheet(ui.SheetProps{Open: open, FullScreen: true, Detents: ui.DetentMedium, Grabber: true,
				OnDismiss: func() { dismissed++ },
				Content: func() ui.Node {
					e := ui.UseEnvironment()
					signal.Effect(func() {
						env = e.Get()
						runs++
						signal.OnCleanup(func() { cleanups++ })
					})
					ui.OnBack(func() bool { return keep })
					return ui.SafeArea(ui.Style{Padding: ui.All(16)}, ui.StaticText("covers the screen"))
				}}),
		)
	})
	h.Ops = nil
	open.Set(true)
	r.Flush()
	ps := h.Presented()
	if len(ps) != 1 || ps[0].Kind != ops.KindSheet || !ps[0].Bool(ops.PropSheetFullScreen) {
		t.Fatalf("presented %v", ps)
	}
	s := ps[0]
	if _, ok := s.Props[ops.PropSheetDetents]; ok {
		t.Fatal("a full-screen sheet's Detents sent")
	}
	if _, ok := s.Props[ops.PropSheetGrabber]; ok {
		t.Fatal("a full-screen sheet's Grabber sent")
	}
	full, present := -1, -1
	for i, op := range h.Ops {
		if op.ID != s.ID {
			continue
		}
		if op.Code == ops.OpSetProp && op.Prop == ops.PropSheetFullScreen {
			full = i
		}
		if op.Code == ops.OpCommand && op.Cmd == ops.CmdPresent {
			present = i
		}
	}
	if full < 0 || present < 0 || full > present {
		t.Fatalf("FullScreen at op %d, Present at %d\n%s", full, present, h.Log())
	}

	// The host reports the screen's own environment: its whole size and
	// safe area.
	h.ReportSheet(s)
	r.Flush()
	if env.Width != 390 || env.Height != 844 || env.SafeTop != 47 || env.SafeBottom != 34 {
		t.Fatalf("its environment %+v", env)
	}
	text := h.One(ops.KindText, "covers the screen")
	if text.Frame[1] != 47+16 {
		t.Fatalf("the content at y %v, not under the status bar", text.Frame[1])
	}
	area := text.Parent
	if area.Frame[2] != 390 || area.Frame[3] != 844 {
		t.Fatalf("the content %v, not the screen's size", area.Frame)
	}
	// Rotated: the host reports it again.
	h.Env = ui.Environment{Width: 844, Height: 390, SafeLeft: 47, SafeRight: 47, SafeBottom: 21, Scale: 3, FontScale: 1}
	h.ReportSheet(s)
	r.Flush()
	if env.Width != 844 || env.SafeLeft != 47 || env.SafeTop != 0 || text.Frame[0] != 47+16 {
		t.Fatalf("rotated: %+v, the text at %v", env, text.Frame)
	}

	// Going back (Android's back, Escape): OnBack keeps it, or it's dismissed.
	keep = true
	h.Dismiss(s)
	r.Flush()
	if !open.Peek() || dismissed != 0 || len(h.OfKind(ops.KindSheet)) != 1 {
		t.Fatal("an intercepted back closed it")
	}
	keep = false
	h.Dismiss(s)
	r.Flush()
	if open.Peek() || dismissed != 1 || len(h.OfKind(ops.KindSheet)) != 0 {
		t.Fatalf("back: open %v, dismissed %d", open.Peek(), dismissed)
	}
	if cleanups != runs {
		t.Fatalf("its content's effects: %d runs, %d cleaned up", runs, cleanups)
	}

	// Closed from code: gone, its content disposed, nothing left listening.
	open.Set(true)
	r.Flush()
	h.ReportSheet(h.Presented()[0])
	open.Set(false)
	r.Flush()
	if len(h.Presented()) != 0 || len(h.OfKind(ops.KindSheet)) != 0 || cleanups != runs {
		t.Fatalf("after closing: %d presented, %d runs, %d cleanups", len(h.Presented()), runs, cleanups)
	}
}

// A sheet that isn't full screen is told the height under the status
// bar, and no safe area at its top.
func TestSheetEnvironment(t *testing.T) {
	var env ui.Environment
	h, r := mount(t, func() ui.Node {
		return ui.Sheet(ui.SheetProps{Open: signal.New(true), Content: func() ui.Node {
			e := ui.UseEnvironment()
			signal.Effect(func() { env = e.Get() })
			return ui.StaticText("sheet")
		}})
	})
	s := h.Presented()[0]
	if s.Bool(ops.PropSheetFullScreen) {
		t.Fatal("FullScreen sent for a sheet")
	}
	h.ReportSheet(s)
	r.Flush()
	if env.Height != 844-47-10 || env.SafeTop != 0 || env.SafeBottom != 34 {
		t.Fatalf("its environment %+v", env)
	}
}

func TestAlert(t *testing.T) {
	deleted := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Delete"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "Delete?", Message: "This can't be undone.", Actions: []ui.Action{
				{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
				{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() { deleted++ }},
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

// An alert still up as its root goes (unmounted, reloaded) goes with
// it: the hosts keep a view until it's destroyed.
func TestAlertGoesWithItsRoot(t *testing.T) {
	h := fake.New()
	r := ui.Mount(h, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Ask"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "Sure?"})
		}})
	})
	h.Tap(h.One(ops.KindButton, "Ask"))
	if len(h.Presented()) != 1 {
		t.Fatal("no alert")
	}
	r.Dispose()
	if len(h.Views) != 0 {
		t.Fatalf("%d views left\n%s", len(h.Views), h.Log())
	}
}

func TestMenuAndButtonMenu(t *testing.T) {
	var picked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{},
			ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{{Title: ui.Val("Copy"), OnPress: func() { picked = append(picked, "copy") }}}},
				ui.StaticText("hold me")),
			ui.Button(ui.ButtonProps{Title: ui.Val("More"), Menu: []ui.Action{
				{Title: ui.Val("Share"), OnPress: func() { picked = append(picked, "share") }},
				{Title: ui.Val("Remove"), Role: ui.ActionDestructive, OnPress: func() { picked = append(picked, "remove") }},
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
					return ui.Style{Absolute: true, Left: ui.Pt(20), Right: ui.Pt(20), Bottom: ui.Pt(env.Get().SafeBottom + 12), Height: ui.Pt(48), Background: colors.Red}
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

// A menu's items can be disabled and checked, open submenus and group
// sections; they're numbered depth first, and follow their signals.
func TestRichMenu(t *testing.T) {
	var picked []string
	sort := signal.New("name")
	locked := signal.New(true)
	pick := func(s string) func() { return func() { picked = append(picked, s) } }
	h, r := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("More"), Menu: []ui.Action{
			ui.MenuSection(ui.Val("Sort by"),
				ui.Action{Title: ui.Val("Name"), Checked: signal.Func(func() bool { return sort.Get() == "name" }), OnPress: func() { sort.Set("name") }},
				ui.Action{Title: ui.Val("Date"), Checked: signal.Func(func() bool { return sort.Get() == "date" }), OnPress: func() { sort.Set("date") }},
			),
			ui.Submenu(ui.Val("Move to"), ui.IconFolder,
				ui.Action{Title: ui.Val("Inbox"), OnPress: pick("inbox")},
				ui.Submenu(ui.Val("More"), "", ui.Action{Title: ui.Val("Archive"), OnPress: pick("archive")}),
			),
			{Title: ui.Val("Delete"), Role: ui.ActionDestructive, Disabled: locked, OnPress: pick("delete")},
		}})
	})
	b := h.One(ops.KindButton, "More")
	want := []ops.ActionItem{
		{Index: 0, Title: "Sort by", Flags: ops.MenuSubmenu | ops.MenuInline, Items: []ops.ActionItem{
			{Index: 1, Title: "Name", Flags: ops.MenuChecked},
			{Index: 2, Title: "Date"},
		}},
		{Index: 3, Title: "Move to", Icon: "folder", Flags: ops.MenuSubmenu, Items: []ops.ActionItem{
			{Index: 4, Title: "Inbox"},
			{Index: 5, Title: "More", Flags: ops.MenuSubmenu, Items: []ops.ActionItem{{Index: 6, Title: "Archive"}}},
		}},
		{Index: 7, Title: "Delete", Role: ops.AlertButtonRoleDestructive, Flags: ops.MenuDisabled},
	}
	if got := b.ActionItems(ops.PropMenu); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	// Numbered depth first: a nested item runs; a section, a submenu and a
	// disabled item don't.
	h.Choose(b, b.ActionNamed(ops.PropMenu, "Archive"))
	h.Choose(b, 4)
	h.Choose(b, 3)
	h.Choose(b, 0)
	h.Choose(b, 7)
	if fmt.Sprint(picked) != "[archive inbox]" {
		t.Fatal(picked)
	}
	// The checkmark follows the choice: one prop set.
	h.Reset()
	h.Choose(b, 2)
	items := b.ActionItems(ops.PropMenu)
	if sort.Peek() != "date" || h.Count(ops.OpSetProp) != 1 || items[0].Items[0].Is(ops.MenuChecked) || !items[0].Items[1].Is(ops.MenuChecked) {
		t.Fatalf("\n%s", h.Log())
	}
	// Enabled, Delete runs.
	locked.Set(false)
	r.Flush()
	h.Choose(b, 7)
	if fmt.Sprint(picked) != "[archive inbox delete]" {
		t.Fatal(picked)
	}
	// What changes nothing the menu shows sends nothing.
	h.Reset()
	locked.Set(false)
	sort.Set("date")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("\n%s", h.Log())
	}
}

// A live menu's re-encoding that comes out the same allocates nothing.
func TestMenuUpdateAllocs(t *testing.T) {
	if signal.Debug {
		t.Skip("asuka_debug's main-thread checks allocate")
	}
	n := signal.New(0)
	odd := signal.Func(func() bool { return n.Get()%2 == 1 })
	h, r := mount(t, func() ui.Node {
		return ui.Menu(ui.MenuProps{Actions: []ui.Action{
			{Title: ui.Val("Even"), Checked: signal.Func(func() bool { return !odd.Get() })},
			ui.Submenu(ui.Val("More"), "", ui.Action{Title: ui.Val("Odd"), Checked: odd}),
		}}, ui.StaticText("x"))
	})
	i := 0
	allocs := testing.AllocsPerRun(200, func() {
		i += 2 // the same parity: the same menu
		h.Reset()
		n.Set(i)
		r.Flush()
	})
	if allocs != 0 || h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("%v allocations, %d props", allocs, h.Count(ops.OpSetProp))
	}
}

// An alert's buttons follow Disabled while it shows, and stop following
// once it's gone; it shows no children.
func TestAlertDisabledButton(t *testing.T) {
	name := signal.New("")
	saved := 0
	h, r := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Save"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "Save?", Actions: []ui.Action{
				{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
				{Title: ui.Val("Save"), Disabled: signal.Func(func() bool { return name.Get() == "" }), OnPress: func() { saved++ },
					Children: []ui.Action{{Title: ui.Val("not shown")}}},
			}})
		}})
	})
	h.Tap(h.One(ops.KindButton, "Save"))
	a := h.Presented()[0]
	items := a.ActionItems(ops.PropAlertButtons)
	if len(items) != 2 || !items[1].Is(ops.MenuDisabled) || items[1].Items != nil {
		t.Fatalf("%+v", items)
	}
	name.Set("notes.txt")
	r.Flush()
	if a.ActionItems(ops.PropAlertButtons)[1].Is(ops.MenuDisabled) {
		t.Fatal("still disabled")
	}
	h.Choose(a, 1)
	if saved != 1 || len(h.OfKind(ops.KindAlert)) != 0 {
		t.Fatal(saved)
	}
	// Gone, it follows nothing.
	h.Reset()
	name.Set("")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("\n%s", h.Log())
	}
}

// An alert's fields start with their Text, typing writes it, and the
// buttons' Disabled follows it while the alert shows; return in the last
// field presses the default button once it's enabled. Gone, nothing
// follows the fields.
func TestAlertFields(t *testing.T) {
	name := signal.New("Untitled")
	pass := signal.New("")
	var created []string
	h, r := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("New"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "New folder", Fields: []ui.AlertField{
				{Text: name, Placeholder: "Name", ContentType: ui.ContentName},
				{Text: pass, Placeholder: "Password", Secure: true, Keyboard: ui.KeyboardEmail},
			}, Actions: []ui.Action{
				{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
				{Title: ui.Val("Create"), Disabled: signal.Func(func() bool { return name.Get() == "" || pass.Get() == "" }),
					OnPress: func() { created = append(created, name.Peek()+":"+pass.Peek()) }},
			}})
		}})
	})
	h.Tap(h.One(ops.KindButton, "New"))
	a := h.Presented()[0]
	fs := a.AlertFields()
	want := []ops.AlertField{
		{Placeholder: "Name", Text: "Untitled", ContentType: "name"},
		{Placeholder: "Password", Flags: ops.AlertFieldSecure, Keyboard: ops.KeyboardEmail},
	}
	if !reflect.DeepEqual(fs, want) || !a.Listens[ops.EventFieldChanged] {
		t.Fatalf("%+v", fs)
	}
	if !a.ActionItems(ops.PropAlertButtons)[1].Is(ops.MenuDisabled) {
		t.Fatal("Create enabled with no password")
	}
	if h.ReturnInAlert(a) {
		t.Fatal("return pressed a disabled button")
	}
	// Typing writes Text, and the button follows: one Buttons prop per
	// change of Disabled, none for typing that doesn't change it.
	h.Reset()
	h.TypeInAlert(a, 1, "s")
	if pass.Peek() != "s" || a.ActionItems(ops.PropAlertButtons)[1].Is(ops.MenuDisabled) || h.Count(ops.OpSetProp) != 1 {
		t.Fatalf("%q\n%s", pass.Peek(), h.Log())
	}
	h.Reset()
	h.TypeInAlert(a, 1, "secret")
	if pass.Peek() != "secret" || h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("%q\n%s", pass.Peek(), h.Log())
	}
	h.TypeInAlert(a, 0, "")
	if name.Peek() != "" || !a.ActionItems(ops.PropAlertButtons)[1].Is(ops.MenuDisabled) {
		t.Fatal("Create enabled with no name")
	}
	h.TypeInAlert(a, 0, "Notes")
	if a.AlertFields()[0].Text != "Notes" {
		t.Fatal(a.AlertFields())
	}
	if !h.ReturnInAlert(a) || len(created) != 1 || created[0] != "Notes:secret" || len(h.OfKind(ops.KindAlert)) != 0 {
		t.Fatal(created)
	}
	// Gone, it follows nothing.
	h.Reset()
	name.Set("")
	pass.Set("")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("\n%s", h.Log())
	}
}

// An action sheet shows no fields; an alert without fields sends none,
// nor listens for FieldChanged.
func TestAlertFieldsNotSent(t *testing.T) {
	text := signal.New("x")
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Go"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "Sheet", ActionSheet: true, Fields: []ui.AlertField{{Text: text}}})
			ui.Alert(ui.AlertProps{Title: "Plain"})
		}})
	})
	h.Tap(h.One(ops.KindButton, "Go"))
	for _, a := range h.Presented() {
		if _, ok := a.Props[ops.PropAlertFields]; ok || a.Listens[ops.EventFieldChanged] {
			t.Fatalf("%s: fields sent", a.String(ops.PropAlertTitle))
		}
	}
}

// A screen's bar button can open a menu, and be disabled.
func TestScreenMenuButton(t *testing.T) {
	var picked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("home") }, ui.ScreenOptions{Title: ui.Val("Home"), Actions: []ui.Action{
			{Title: ui.Val("Add"), Icon: ui.IconPlus, Children: []ui.Action{
				{Title: ui.Val("Note"), OnPress: func() { picked = append(picked, "note") }},
				{Title: ui.Val("Folder"), OnPress: func() { picked = append(picked, "folder") }},
			}},
			{Title: ui.Val("Edit"), Disabled: ui.Val(true), OnPress: func() { picked = append(picked, "edit") }},
		}})
	})
	s := h.OfKind(ops.KindScreen)[0]
	items := s.ActionItems(ops.PropScreenActions)
	if len(items) != 2 || len(items[0].Items) != 2 || items[1].Index != 3 || !items[1].Is(ops.MenuDisabled) {
		t.Fatalf("%+v", items)
	}
	h.Choose(s, 2)
	h.Choose(s, 3)
	if fmt.Sprint(picked) != "[folder]" {
		t.Fatal(picked)
	}
}

// A screen's Toolbar and Primary are sent with their own props, follow
// their accessors, and report their presses apart from the bar buttons'.
func TestScreenToolbarAndPrimary(t *testing.T) {
	var picked []string
	pick := func(s string) func() { return func() { picked = append(picked, s) } }
	empty := signal.New(true)
	title := signal.New("Compose")
	var nav *ui.Navigator
	h, r := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.StaticText("inbox")
		}, ui.ScreenOptions{
			Title:   ui.Val("Inbox"),
			Actions: []ui.Action{{Title: ui.Val("Edit"), OnPress: pick("edit")}},
			Toolbar: []ui.Action{
				{Title: ui.Val("Filter"), Icon: "line.3.horizontal.decrease", OnPress: pick("filter")},
				{Title: ui.Val("Sort"), Icon: "arrow.up.arrow.down", Children: []ui.Action{
					{Title: ui.Val("Date"), OnPress: pick("date")},
					{Title: ui.Val("Sender"), OnPress: pick("sender")},
				}},
				{Title: ui.Val("Delete all"), Disabled: empty, OnPress: pick("delete")},
			},
			Primary:         &ui.Action{Title: title, Icon: ui.IconPlus, OnPress: pick("compose")},
			PrimaryExtended: true,
		})
	})
	s := h.OfKind(ops.KindScreen)[0]
	items := s.ToolbarItems()
	if len(items) != 3 || items[1].Title != "Sort" || len(items[1].Items) != 2 || items[2].Index != 4 || !items[2].Is(ops.MenuDisabled) {
		t.Fatalf("toolbar %+v", items)
	}
	if p := s.PrimaryItem(); p == nil || p.Title != "Compose" || p.Icon != string(ui.IconPlus) || !s.Bool(ops.PropScreenPrimaryExtended) {
		t.Fatalf("primary %+v", p)
	}
	// Each press reaches its own action: the bar's, the toolbar's (one
	// in a menu, and not a disabled one), the primary's.
	h.Choose(s, 0)
	for _, title := range []string{"Filter", "Sender", "Delete all", "Sort"} {
		if !h.PressToolbar(s, title) {
			t.Fatalf("no %s", title)
		}
	}
	h.PressPrimary(s)
	if fmt.Sprint(picked) != "[edit filter sender compose]" {
		t.Fatal(picked)
	}
	// They follow their accessors: one prop each, nothing else.
	h.Reset()
	empty.Set(false)
	title.Set("New message")
	r.Flush()
	if h.Count(ops.OpSetProp) != 2 || s.ToolbarItems()[2].Is(ops.MenuDisabled) || s.PrimaryItem().Title != "New message" {
		t.Fatalf("%d props\n%s", h.Count(ops.OpSetProp), h.Log())
	}
	h.PressToolbar(s, "Delete all")
	if picked[len(picked)-1] != "delete" {
		t.Fatal(picked)
	}

	// A pushed screen's live toolbar goes with it.
	disabled := signal.New(false)
	nav.Push(func() ui.Node { return ui.StaticText("detail") }, ui.ScreenOptions{
		Toolbar: []ui.Action{{Title: ui.Val("Reply"), Disabled: disabled}},
		Primary: &ui.Action{Title: ui.Val("Forward"), Children: []ui.Action{{Title: ui.Val("As attachment"), OnPress: pick("attach")}}},
	})
	r.Flush()
	detail := h.OfKind(ops.KindScreen)[1]
	if detail.Bool(ops.PropScreenPrimaryExtended) || !h.PressPrimaryItem(detail, "As attachment") || picked[len(picked)-1] != "attach" {
		t.Fatalf("detail primary %+v, %v", detail.PrimaryItem(), picked)
	}
	nav.Pop()
	r.Flush()
	h.Reset()
	disabled.Set(true)
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("a popped screen's toolbar was sent:\n%s", h.Log())
	}
}

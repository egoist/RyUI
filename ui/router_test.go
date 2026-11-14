package ui_test

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// titles are a stack's screens' titles, bottom first.
func titles(stack *fake.View) []string {
	var out []string
	for _, s := range stack.Children {
		out = append(out, s.String(ops.PropScreenTitle))
	}
	return out
}

func history(t *testing.T, h *fake.Host) *fake.History {
	t.Helper()
	hi := h.History(nil)
	if hi == nil {
		t.Fatal("no history: the router sent no Location")
	}
	return hi
}

func expectHistory(t *testing.T, h *fake.Host, at int, hrefs ...string) {
	t.Helper()
	hi := history(t, h)
	if !slices.Equal(hi.Hrefs(), hrefs) || hi.Index != at {
		t.Fatalf("history %q at %d, want %q at %d", hi.Hrefs(), hi.Index, hrefs, at)
	}
}

// chats is a stack of a list, a chat and its info, as a messaging app's.
func chats(disposed *[]string) []ui.Route {
	screen := func(name string) func(p ui.Params) ui.Node {
		return func(p ui.Params) ui.Node {
			label := name + " " + p.Get("id")
			signal.OnCleanup(func() { *disposed = append(*disposed, label) })
			return ui.StaticText(label)
		}
	}
	title := func(p ui.Params) ui.ScreenOptions { return ui.ScreenOptions{Title: ui.Val("Chat " + p.Get("id"))} }
	return []ui.Route{
		{Path: "/", Layout: ui.LayoutStack, Children: []ui.Route{
			{Screen: screen("list"), Title: ui.Val("Chats")},
			{Path: "chat/:id", Screen: screen("chat"), Options: title},
			{Path: "chat/:id/info", Screen: screen("info"), Title: ui.Val("Info")},
		}},
	}
}

func TestRouterDeepLinkBuildsTheStack(t *testing.T) {
	var gone []string
	h, _ := mount(t, func() ui.Node {
		return ui.Router(ui.RouterProps{Initial: "/chat/7/info"}, chats(&gone)...)
	})
	stack := h.OfKind(ops.KindStack)[0]
	// The stack's first screen under the one linked to, so back goes
	// somewhere, and the screens up its path.
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 7", "Info"}) {
		t.Fatalf("screens %q", got)
	}
	if !stack.Bool(ops.PropStackRouted) {
		t.Fatal("the stack isn't marked routed")
	}
	expectHistory(t, h, 0, "/chat/7/info")
	var saved struct {
		Sel string `json:"sel"`
		E   []struct {
			N struct {
				E []struct{ R, H string }
			}
		}
	}
	if err := json.Unmarshal(history(t, h).Current().State, &saved); err != nil || len(saved.E) != 1 || len(saved.E[0].N.E) != 3 || saved.E[0].N.E[2].H != "/chat/7/info" {
		t.Fatalf("state %s (%v)", history(t, h).Current().State, err)
	}
}

func TestRouterNavigatePushBackReplace(t *testing.T) {
	var gone []string
	rt := ui.NewRouterHandle()
	h, r := mount(t, func() ui.Node {
		return ui.Router(ui.RouterProps{Handle: rt}, chats(&gone)...)
	})
	stack := h.OfKind(ops.KindStack)[0]
	expectHistory(t, h, 0, "/")

	rt.Navigate("/chat/7")
	r.Flush()
	rt.Navigate("/chat/7/info")
	r.Flush()
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 7", "Info"}) {
		t.Fatalf("screens %q", got)
	}
	expectHistory(t, h, 2, "/", "/chat/7", "/chat/7/info")

	// Navigate to a screen in the stack goes back to it; Push pushes
	// another.
	rt.Navigate("/chat/7")
	r.Flush()
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 7"}) || !slices.Equal(gone, []string{"info 7"}) {
		t.Fatalf("screens %q, disposed %q", got, gone)
	}
	expectHistory(t, h, 1, "/", "/chat/7", "/chat/7/info")
	rt.Push("/chat/7")
	r.Flush()
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 7", "Chat 7"}) {
		t.Fatalf("screens %q", got)
	}
	expectHistory(t, h, 2, "/", "/chat/7", "/chat/7")

	// Replace: the top screen goes (disposed), the history doesn't grow.
	gone = nil
	rt.Replace("/chat/8")
	r.Flush()
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 7", "Chat 8"}) || !slices.Equal(gone, []string{"chat 7"}) {
		t.Fatalf("screens %q, disposed %q", got, gone)
	}
	expectHistory(t, h, 2, "/", "/chat/7", "/chat/8")

	// The user's back gesture pops: the history goes back.
	gone = nil
	h.Back(stack)
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 7"}) || !slices.Equal(gone, []string{"chat 8"}) {
		t.Fatalf("screens %q, disposed %q", got, gone)
	}
	expectHistory(t, h, 1, "/", "/chat/7", "/chat/8")
	if !rt.CanGoBack() || !rt.Back() {
		t.Fatal("can't go back")
	}
	r.Flush()
	expectHistory(t, h, 0, "/", "/chat/7", "/chat/8")
	if rt.CanGoBack() || rt.Back() {
		t.Fatal("went back from the first screen")
	}
	if rt.Href().Get() != "/" {
		t.Fatalf("href %q", rt.Href().Get())
	}
}

func TestRouterBrowserBackAndForward(t *testing.T) {
	var gone []string
	rt := ui.NewRouterHandle()
	h, r := mount(t, func() ui.Node {
		return ui.Router(ui.RouterProps{Handle: rt}, chats(&gone)...)
	})
	stack := h.OfKind(ops.KindStack)[0]
	rt.Navigate("/chat/1")
	rt.Navigate("/chat/1/info")
	r.Flush()
	expectHistory(t, h, 2, "/", "/chat/1", "/chat/1/info")

	// The browser's back: the entry's state comes back, the top pops.
	gone = nil
	if !h.GoBack(nil) {
		t.Fatal("no back")
	}
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 1"}) || !slices.Equal(gone, []string{"info 1"}) {
		t.Fatalf("screens %q, disposed %q", got, gone)
	}
	h.GoBack(nil)
	if got := titles(stack); !slices.Equal(got, []string{"Chats"}) {
		t.Fatalf("screens %q", got)
	}
	// Forward twice builds them again, from the entries' states.
	h.GoForward(nil)
	h.GoForward(nil)
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 1", "Info"}) {
		t.Fatalf("screens %q", got)
	}
	expectHistory(t, h, 2, "/", "/chat/1", "/chat/1/info")
	// An entry with no state (a link pasted, an entry from before) builds
	// the state from its href.
	hi := history(t, h)
	hi.Entries[0].State = nil
	h.Go(nil, -2)
	if got := titles(stack); !slices.Equal(got, []string{"Chats"}) {
		t.Fatalf("screens %q", got)
	}
}

// Tabs that hold stacks: each tab keeps its stack, a link goes to its
// tab, and the history follows react-navigation's: leaving the first tab
// pushes, between the others replaces, back to the first goes back.
func TestRouterTabsOfStacks(t *testing.T) {
	rt := ui.NewRouterHandle()
	var gone []string
	screen := func(name string) func(ui.Params) ui.Node {
		return func(p ui.Params) ui.Node {
			label := name + p.Get("id")
			signal.OnCleanup(func() { gone = append(gone, label) })
			return ui.StaticText(label)
		}
	}
	routes := []ui.Route{{Layout: ui.LayoutTabs, Children: []ui.Route{
		{Path: "/inbox", Title: ui.Val("Inbox"), Icon: ui.IconMail, Layout: ui.LayoutStack, Children: []ui.Route{
			{Screen: screen("inbox"), Title: ui.Val("Inbox")},
			{Path: ":id", Screen: screen("message"), Title: ui.Val("Message")},
		}},
		{Path: "/search", Title: ui.Val("Search"), Screen: screen("search")},
		{Path: "/settings", Title: ui.Val("Settings"), Layout: ui.LayoutStack, Children: []ui.Route{
			{Screen: screen("settings"), Title: ui.Val("Settings")},
			{Path: "about", Screen: screen("about"), Title: ui.Val("About")},
		}},
	}}, {Path: "/", Redirect: "/inbox"}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt, Initial: "/settings/about"}, routes...) })
	tabs := h.OfKind(ops.KindTabs)[0]
	stacks := h.OfKind(ops.KindStack)
	if len(stacks) != 2 || tabs.Int(ops.PropTabsSelected) != 2 {
		t.Fatalf("%d stacks, tab %d", len(stacks), tabs.Int(ops.PropTabsSelected))
	}
	inbox, settings := stacks[0], stacks[1]
	if !slices.Equal(titles(inbox), []string{"Inbox"}) || !slices.Equal(titles(settings), []string{"Settings", "About"}) {
		t.Fatalf("inbox %q settings %q", titles(inbox), titles(settings))
	}
	expectHistory(t, h, 0, "/settings/about")

	// The user picks the inbox: the first tab, a back.
	h.Select(tabs, 0)
	expectHistory(t, h, 0, "/inbox")
	rt.Navigate("/inbox/3")
	r.Flush()
	expectHistory(t, h, 1, "/inbox", "/inbox/3")
	// To another tab: a push; the inbox keeps its stack.
	h.Select(tabs, 1)
	expectHistory(t, h, 2, "/inbox", "/inbox/3", "/search")
	h.Select(tabs, 2)
	expectHistory(t, h, 2, "/inbox", "/inbox/3", "/settings/about")
	if !slices.Equal(titles(inbox), []string{"Inbox", "Message"}) {
		t.Fatalf("inbox %q", titles(inbox))
	}
	// The browser's back goes to the inbox tab, as it was.
	h.GoBack(nil)
	if tabs.Int(ops.PropTabsSelected) != 0 || !slices.Equal(titles(inbox), []string{"Inbox", "Message"}) {
		t.Fatalf("tab %d inbox %q", tabs.Int(ops.PropTabsSelected), titles(inbox))
	}
	// A redirect: "/" is the inbox.
	rt.Navigate("/")
	r.Flush()
	if !slices.Equal(titles(inbox), []string{"Inbox"}) || rt.Href().Get() != "/inbox" {
		t.Fatalf("inbox %q at %q", titles(inbox), rt.Href().Get())
	}
	// Navigating to a tab's screen switches to it.
	rt.Navigate("/settings")
	r.Flush()
	if tabs.Int(ops.PropTabsSelected) != 2 || !slices.Equal(titles(settings), []string{"Settings"}) {
		t.Fatalf("tab %d settings %q", tabs.Int(ops.PropTabsSelected), titles(settings))
	}
	if !slices.Contains(gone, "about") {
		t.Fatalf("about wasn't disposed: %q", gone)
	}
}

func TestRouterSheets(t *testing.T) {
	rt := ui.NewRouterHandle()
	routes := []ui.Route{{Path: "/", Layout: ui.LayoutStack, Children: []ui.Route{
		{Screen: func(ui.Params) ui.Node { return ui.StaticText("home") }, Title: ui.Val("Home")},
		{Path: "compose", Sheet: &ui.SheetRoute{Detents: ui.DetentMedium}, Layout: ui.LayoutStack, Children: []ui.Route{
			{Screen: func(ui.Params) ui.Node { return ui.StaticText("to") }, Title: ui.Val("New message")},
			{Path: "body", Screen: func(ui.Params) ui.Node { return ui.StaticText("body") }, Title: ui.Val("Body")},
		}},
	}}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt}, routes...) })
	rt.Navigate("/compose/body")
	r.Flush()
	sheets := h.OfKind(ops.KindSheet)
	if len(sheets) != 1 || !sheets[0].Bool(ops.PropSheetRouted) || sheets[0].Int(ops.PropSheetDetents) != int64(ui.DetentMedium) {
		t.Fatalf("sheets %d", len(sheets))
	}
	inner := h.OfKind(ops.KindStack)[1]
	if !slices.Equal(titles(inner), []string{"New message", "Body"}) {
		t.Fatalf("the sheet's stack %q", titles(inner))
	}
	expectHistory(t, h, 1, "/", "/compose/body")
	// Back pops the sheet's stack first, then dismisses it.
	h.Back(inner)
	expectHistory(t, h, 1, "/", "/compose")
	h.Dismiss(sheets[0])
	if len(h.OfKind(ops.KindSheet)) != 0 {
		t.Fatal("the sheet stayed")
	}
	expectHistory(t, h, 0, "/", "/compose")
	// Forward presents it again.
	h.GoForward(nil)
	if len(h.OfKind(ops.KindSheet)) != 1 {
		t.Fatal("no sheet")
	}
	// Navigating to the stack's own screen closes it.
	rt.Navigate("/")
	r.Flush()
	if len(h.OfKind(ops.KindSheet)) != 0 || rt.Href().Get() != "/" {
		t.Fatalf("sheets %d at %q", len(h.OfKind(ops.KindSheet)), rt.Href().Get())
	}
}

// A guard sends a signed-out user to /login, with where they were going,
// and once they've signed in, there; signing out goes back.
func TestRouterGuardsAndRedirects(t *testing.T) {
	rt := ui.NewRouterHandle()
	signedIn := signal.New(false)
	var gone []string
	screen := func(name string) func(ui.Params) ui.Node {
		return func(p ui.Params) ui.Node {
			signal.OnCleanup(func() { gone = append(gone, name) })
			return ui.StaticText(name + p.Get("id"))
		}
	}
	routes := []ui.Route{
		{Path: "/login", Screen: screen("login"), Guard: func(to ui.Href) ui.Href {
			if signedIn.Get() {
				if next := to.Query().Get("next"); next != "" {
					return ui.Href(next)
				}
				return "/"
			}
			return ""
		}},
		{Guard: func(to ui.Href) ui.Href {
			if !signedIn.Get() {
				return ui.Href("/login").With("next", string(to))
			}
			return ""
		}, Children: []ui.Route{
			{Path: "/", Layout: ui.LayoutStack, Children: []ui.Route{
				{Screen: screen("home"), Title: ui.Val("Home")},
				{Path: "notes/:id", Screen: screen("note"), Title: ui.Val("Note")},
			}},
		}},
	}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt, Initial: "/notes/4"}, routes...) })
	if !has(h, "login") || rt.Href().Get() != "/login?next=%2Fnotes%2F4" {
		t.Fatalf("at %q:\n%s", rt.Href().Get(), h.Root().Tree())
	}
	signedIn.Set(true)
	r.Flush()
	stack := h.OfKind(ops.KindStack)
	if len(stack) != 1 || !slices.Equal(titles(stack[0]), []string{"Home", "Note"}) || rt.Href().Get() != "/notes/4" || !slices.Equal(gone, []string{"login"}) {
		t.Fatalf("at %q, disposed %q:\n%s", rt.Href().Get(), gone, h.Root().Tree())
	}
	expectHistory(t, h, 0, "/notes/4")
	gone = nil
	signedIn.Set(false)
	r.Flush()
	if !has(h, "login") || len(h.OfKind(ops.KindStack)) != 0 || !slices.Contains(gone, "home") || !slices.Contains(gone, "note") {
		t.Fatalf("at %q, disposed %q:\n%s", rt.Href().Get(), gone, h.Root().Tree())
	}
	// A link the guard keeps out goes to the login too.
	rt.Navigate("/notes/9")
	r.Flush()
	if rt.Href().Get() != "/login?next=%2Fnotes%2F9" {
		t.Fatalf("at %q", rt.Href().Get())
	}
}

// Links of the app's scheme, universal links and notifications' URLs go
// to their route; others to OnOpenURL; the launch URL is the router's
// first.
func TestRouterDeepLinks(t *testing.T) {
	rt := ui.NewRouterHandle()
	var gone, opened []string
	h := fake.New()
	// Before the app mounts: its launch URL.
	ui.Dispatch(ops.Event{Kind: ops.EventIncomingURL, Str: "chats://chat/5/info"})
	r := ui.Mount(h, func() ui.Node {
		ui.OnOpenURL(func(u string) { opened = append(opened, u) })
		return ui.Router(ui.RouterProps{Handle: rt}, chats(&gone)...)
	})
	t.Cleanup(r.Dispose)
	stack := h.OfKind(ops.KindStack)[0]
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 5", "Info"}) || len(opened) != 0 {
		t.Fatalf("screens %q, opened %q", got, opened)
	}
	h.OpenURL("https://chats.example.com/chat/6")
	if got := titles(stack); !slices.Equal(got, []string{"Chats", "Chat 5", "Info", "Chat 6"}) {
		t.Fatalf("screens %q", got)
	}
	h.OpenURL("chats://")
	if got := titles(stack); !slices.Equal(got, []string{"Chats"}) {
		t.Fatalf("screens %q", got)
	}
	// Every path matches this router's catch-all? No: an href no route
	// takes is the not-found screen's, which a link doesn't go to.
	h.OpenURL("chats://add?text=hi")
	if !slices.Equal(opened, []string{"chats://add?text=hi"}) {
		t.Fatalf("opened %q", opened)
	}
}

// Where a window was left (a browser's address and history entry, a
// desktop window's last run) is where the router starts: the screens
// under the top come back too.
func TestRouterRestoresTheState(t *testing.T) {
	rt := ui.NewRouterHandle()
	var gone []string
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt}, chats(&gone)...) })
	rt.Navigate("/chat/2")
	rt.Push("/chat/3")
	rt.Navigate("/chat/3/info")
	r.Flush()
	saved := h.Locations["main"]
	if saved.Href != "/chat/3/info" {
		t.Fatalf("saved %q", saved.Href)
	}
	r.Dispose()

	h2 := fake.New()
	h2.Locations = map[string]fake.Saved{"main": saved}
	r2 := ui.Mount(h2, func() ui.Node { return ui.Router(ui.RouterProps{}, chats(&gone)...) })
	t.Cleanup(r2.Dispose)
	if got := titles(h2.OfKind(ops.KindStack)[0]); !slices.Equal(got, []string{"Chats", "Chat 2", "Chat 3", "Info"}) {
		t.Fatalf("screens %q", got)
	}
	// An address whose state was another's (the address edited): its href
	// wins.
	h3 := fake.New()
	h3.Locations = map[string]fake.Saved{"main": {Href: "/chat/9", State: saved.State}}
	r3 := ui.Mount(h3, func() ui.Node { return ui.Router(ui.RouterProps{}, chats(&gone)...) })
	t.Cleanup(r3.Dispose)
	if got := titles(h3.OfKind(ops.KindStack)[0]); !slices.Equal(got, []string{"Chats", "Chat 9"}) {
		t.Fatalf("screens %q", got)
	}
}

func TestRouterSplitView(t *testing.T) {
	rt := ui.NewRouterHandle()
	text := func(s string) func(ui.Params) ui.Node {
		return func(p ui.Params) ui.Node { return ui.StaticText(s + p.Get("id")) }
	}
	routes := []ui.Route{{Layout: ui.LayoutSplit, Title: ui.Val("Mail"), Children: []ui.Route{
		{Path: "/inbox", Title: ui.Val("Inbox"), Icon: ui.IconMail, Layout: ui.LayoutStack, Children: []ui.Route{
			{Screen: text("inbox"), Title: ui.Val("Inbox")},
			{Path: ":id", Screen: text("mail"), Title: ui.Val("Mail")},
		}},
		{Title: ui.Val("Folders"), Children: []ui.Route{
			{Path: "/work", Screen: text("work"), Title: ui.Val("Work")},
		}},
	}}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt, Initial: "/inbox/4"}, routes...) })
	split := h.OfKind(ops.KindSplit)[0]
	detail := h.OfKind(ops.KindStack)[0]
	if got := titles(detail); !slices.Equal(got, []string{"Inbox", "Mail"}) || split.Int(ops.PropSplitSelected) != 0 {
		t.Fatalf("detail %q", got)
	}
	sections := ops.DecodeSidebar(split.Props[ops.PropSplitItems].Bytes)
	if len(sections) != 2 || sections[1].Title != "Folders" || sections[1].Items[0].Title != "Work" {
		t.Fatalf("sidebar %+v", sections)
	}
	h.Select(split, 1)
	if got := titles(detail); !slices.Equal(got, []string{"Work"}) || rt.Href().Get() != "/work" {
		t.Fatalf("detail %q at %q", got, rt.Href().Get())
	}
	expectHistory(t, h, 1, "/inbox/4", "/work")
	h.GoBack(nil)
	if got := titles(detail); !slices.Equal(got, []string{"Inbox", "Mail"}) || split.Int(ops.PropSplitSelected) != 0 {
		t.Fatalf("detail %q", got)
	}
	rt.Navigate("/inbox")
	r.Flush()
	if got := titles(detail); !slices.Equal(got, []string{"Inbox"}) {
		t.Fatalf("detail %q", got)
	}
}

// A screen whose OnBack takes the user's going back keeps the browser
// where it was.
func TestRouterBrowserBackAsksOnBack(t *testing.T) {
	rt := ui.NewRouterHandle()
	dirty := true
	asked := 0
	routes := []ui.Route{{Path: "/", Layout: ui.LayoutStack, Children: []ui.Route{
		{Screen: func(ui.Params) ui.Node { return ui.StaticText("home") }, Title: ui.Val("Home")},
		{Path: "edit", Title: ui.Val("Edit"), Screen: func(ui.Params) ui.Node {
			ui.OnBack(func() bool { asked++; return dirty })
			return ui.StaticText("editor")
		}},
	}}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt}, routes...) })
	rt.Navigate("/edit")
	r.Flush()
	h.GoBack(nil)
	stack := h.OfKind(ops.KindStack)[0]
	if asked != 1 || !slices.Equal(titles(stack), []string{"Home", "Edit"}) {
		t.Fatalf("asked %d, screens %q", asked, titles(stack))
	}
	expectHistory(t, h, 1, "/", "/edit")
	dirty = false
	h.GoBack(nil)
	if asked != 2 || !slices.Equal(titles(stack), []string{"Home"}) {
		t.Fatalf("asked %d, screens %q", asked, titles(stack))
	}
	expectHistory(t, h, 0, "/", "/edit")
}

// A screen pushed with a Navigator in a routed stack has its stack's
// address, a history entry of its own, and goes with the browser's back.
func TestRouterNavigatorPushInARoutedStack(t *testing.T) {
	var nav *ui.Navigator
	routes := []ui.Route{{Path: "/", Layout: ui.LayoutStack, Children: []ui.Route{
		{Screen: func(ui.Params) ui.Node { nav = ui.UseNavigator(); return ui.StaticText("home") }, Title: ui.Val("Home")},
	}}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{}, routes...) })
	nav.Push(func() ui.Node { return ui.StaticText("closure") }, ui.ScreenOptions{Title: ui.Val("Closure")})
	r.Flush()
	expectHistory(t, h, 1, "/", "/")
	h.GoBack(nil)
	if got := titles(h.OfKind(ops.KindStack)[0]); !slices.Equal(got, []string{"Home"}) {
		t.Fatalf("screens %q", got)
	}
}

// SetQuery keeps the screen, which follows it; the entry's replaced.
func TestRouterSetQuery(t *testing.T) {
	rt := ui.NewRouterHandle()
	builds := 0
	routes := []ui.Route{{Path: "/search", Screen: func(p ui.Params) ui.Node {
		builds++
		return ui.Text(signal.Func(func() string { return "q=" + p.Get("q") }))
	}}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt, Initial: "/search?q=go"}, routes...) })
	if !has(h, "q=go") {
		t.Fatal(h.Root().Tree())
	}
	rt.SetQuery(url.Values{"q": {"asuka"}})
	r.Flush()
	if !has(h, "q=asuka") || builds != 1 {
		t.Fatalf("builds %d:\n%s", builds, h.Root().Tree())
	}
	expectHistory(t, h, 0, "/search?q=asuka")
	rt.Navigate("/search?q=signals")
	r.Flush()
	if !has(h, "q=signals") || builds != 1 {
		t.Fatalf("builds %d", builds)
	}
}

func TestRouterNotFound(t *testing.T) {
	rt := ui.NewRouterHandle()
	h, r := mount(t, func() ui.Node {
		return ui.Router(ui.RouterProps{Handle: rt, Initial: "/nope/x"}, ui.Route{Path: "/", Screen: func(ui.Params) ui.Node { return ui.StaticText("home") }})
	})
	if !has(h, "This screen doesn't exist.") || !has(h, "/nope/x") {
		t.Fatal(h.Root().Tree())
	}
	h.Tap(h.One(ops.KindButton, "Go home"))
	r.Flush()
	if !has(h, "home") || has(h, "/nope/x") {
		t.Fatal(h.Root().Tree())
	}
}

// Disposing the router disposes every screen it made, and a flush while
// it shows allocates nothing.
func TestRouterOwnersAndAllocations(t *testing.T) {
	rt := ui.NewRouterHandle()
	var gone []string
	n := signal.New(0)
	routes := chats(&gone)
	routes[0].Children[0].Screen = func(ui.Params) ui.Node {
		return ui.Text(signal.Func(func() string {
			if n.Get()%2 == 0 {
				return "even"
			}
			return "odd"
		}))
	}
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt, Initial: "/chat/1/info"}, routes...) })
	if !signal.Debug {
		i := 0
		if a := testing.AllocsPerRun(200, func() {
			i++
			h.Reset()
			n.Set(i)
			r.Flush()
		}); a != 0 {
			t.Fatalf("%v allocations per update", a)
		}
	}
	rt.Push("/chat/2")
	r.Flush()
	r.Dispose()
	slices.Sort(gone)
	if !slices.Equal(gone, []string{"chat 1", "chat 2", "info 1"}) {
		t.Fatalf("disposed %q", gone)
	}
	for _, v := range h.Views {
		if v.ID&ops.HostIDBit == 0 {
			t.Fatalf("a view outlived the router: %s %d", v.Kind, v.ID)
		}
	}
}

func TestHrefOf(t *testing.T) {
	if got := ui.HrefOf("/chat/:id", 7); got != "/chat/7" {
		t.Fatal(got)
	}
	if got := ui.HrefOf("/files/*path", "docs/a b.md"); got != "/files/docs/a%20b.md" {
		t.Fatal(got)
	}
	if got := ui.Href("/login").With("next", "/chat/7"); got != "/login?next=%2Fchat%2F7" || got.Query().Get("next") != "/chat/7" || got.Path() != "/login" {
		t.Fatal(got)
	}
	defer func() {
		if e := recover(); e == nil || !strings.Contains(e.(string), "1 values for 2 parameters") {
			t.Fatalf("panic %v", e)
		}
	}()
	pattern := "/a/:x/:y" // a variable: asuka vet reports a constant's count
	ui.HrefOf(pattern, 1)
}

// Catch-alls and parameters: the most specific route matches.
func TestRouterMatchesTheMostSpecific(t *testing.T) {
	rt := ui.NewRouterHandle()
	show := func(s string) func(ui.Params) ui.Node {
		return func(p ui.Params) ui.Node { return ui.StaticText(s + ":" + p.Get("path") + p.Get("id")) }
	}
	routes := []ui.Route{{Layout: ui.LayoutSlot, Children: []ui.Route{
		{Path: "/files/*path", Screen: show("files")},
		{Path: "/files/new", Screen: show("new")},
		{Path: "/files/:id/edit", Screen: show("edit")},
		{Path: "/*path", Screen: show("mine")},
	}}}
	h, r := mount(t, func() ui.Node { return ui.Router(ui.RouterProps{Handle: rt, Initial: "/files/new"}, routes...) })
	for href, want := range map[ui.Href]string{
		"/files/new": "new:", "/files/a/b": "files:a/b", "/files/3/edit": "edit:3", "/files": "files:", "/x/y": "mine:x/y",
	} {
		rt.Navigate(href)
		r.Flush()
		if !has(h, want) {
			t.Fatalf("%s: not %q:\n%s", href, want, h.Root().Tree())
		}
	}
}

// has reports whether a view's text is s.
func has(h *fake.Host, s string) bool {
	for _, v := range h.Views {
		if v.Text() == s {
			return true
		}
	}
	return false
}

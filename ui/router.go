package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/host"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// Routing: every screen has an address (an Href), as on the web. A
// Router's routes map path patterns to screens, under navigators that
// show them (a Stack, Tabs, a SplitView, a Slot) and sheets presented
// over them. Its navigation state is a tree: each navigator's screens
// (a stack's, bottom first) or chosen one (a tab, a destination, a
// slot's), and the sheets over it. The state serializes (state's JSON),
// so the web keeps it in each history entry and a desktop window keeps
// it with where it was; the focused screen's href is the address. An
// href builds a state too: the screen it names, with the screens under it
// a stack starts with (its Initial), so going back from a link goes
// somewhere.
//
// Navigating is done on a description of the state (navDesc), made from
// the live one; the live state is then reconciled to it, as a view diff:
// entries kept (by key, or by route and parameters when the description
// came from outside: a history entry, a reload), the others popped,
// pushed, presented or dismissed. Whatever changed the state (the
// Router's methods, the user's back, a tab chosen, a Navigator's push or
// pop), the new state's address goes to the host with how its history
// moved: a push (it got deeper), a back, or a replace, as
// react-navigation's linking tells them apart.

// Layout is how a Route shows its Children.
type Layout uint8

const (
	// LayoutGroup has no navigator of its own: its children join its
	// parent's, under its Path and its Guard; in a split view its Title
	// makes them a section of the sidebar.
	LayoutGroup Layout = iota
	// LayoutStack is a navigation stack (Stack): its children are its
	// screens, the Initial one at the bottom.
	LayoutStack
	// LayoutTabs is Tabs: each child a tab, a screen or a stack of its own.
	LayoutTabs
	// LayoutSplit is a SplitView: each child a destination of its sidebar,
	// a screen or a stack (the detail's), its groups the sidebar's sections.
	LayoutSplit
	// LayoutSlot shows one child at a time, filling its place, with no bar
	// of its own: the app's top level when it has a signed-out part (a
	// login screen) and a signed-in one (its tabs).
	LayoutSlot
)

// Route maps a path pattern to a screen, or holds routes under a
// navigator (Layout) or a group.
//
// Its Path is relative to its parent's unless it starts with "/":
// segments separated by "/", ":name" a parameter (one segment), "*name" a
// catch-all (the rest of the path, none or more segments; last). "" is
// the parent's own path: its index.
//
//	ui.Router(ui.RouterProps{},
//		ui.Route{Path: "/", Redirect: "/chats"},
//		ui.Route{Path: "/chats", Layout: ui.LayoutStack, Children: []ui.Route{
//			{Screen: chats, Title: ui.Val("Chats")},
//			{Path: ":id", Screen: chat},
//		}},
//	)
type Route struct {
	Path string
	// Screen builds the route's screen: p has its parameters.
	Screen func(p Params) Node
	// Options configures its screen in a stack (its bar's title, buttons,
	// search), from its parameters; nil: Title alone.
	Options func(p Params) ScreenOptions
	// Title is the screen's bar's (unless Options gives one), a tab's, a
	// destination's, a split view's sidebar's, a sidebar section's, and in
	// a browser the page's.
	Title signal.Accessor[string]
	// Icon and Badge are a tab's or a destination's.
	Icon  Icon
	Badge signal.Accessor[string]
	// Sheet presents the route in a sheet over its navigator (a stack's
	// top screen, the tabs) instead of pushing or choosing it: a screen, or
	// a navigator of its own (a Stack, for a sheet of several steps).
	Sheet *SheetRoute
	// Layout makes the route a navigator of its Children; LayoutGroup
	// (the zero value) only groups them.
	Layout   Layout
	Children []Route
	// Initial is the child a navigator starts with: a stack's first
	// screen, under the one a link names (expo-router's
	// initialRouteName), the tab or destination shown first. It's the
	// child's Path ("" its index); its parameters come from the screen
	// linked to, by name. Unset: the first child.
	Initial string
	// Redirect sends whoever comes to the route's path to another href,
	// with the route's parameters filled in ("/chat/:id/info"), replacing
	// it: a screen that moved, an index that's another screen.
	Redirect string
	// Guard is asked before its screens (and its children's) show: "" lets
	// the user in; an href sends them there instead (a signed-out user to
	// "/login", with where they were going: to.Path(), ui.Href.With). It
	// can read signals: when what it reads changes, the screens shown are
	// asked again, and those it no longer lets in are left.
	Guard func(to Href) Href
}

// SheetRoute is how a route's sheet is presented, as SheetProps.
type SheetRoute struct {
	Detents    Detent
	Grabber    bool
	FullScreen bool
}

// RouterProps configures a Router.
type RouterProps struct {
	// Initial is where the router starts when nothing else says (a link
	// the app was opened with, the address its window was left at): "/"
	// if empty.
	Initial Href
	// NotFound is the screen for an href no route takes (nil: a plain one
	// saying so, with a way home). Params' "path" is the href's path.
	NotFound func(p Params) Node
	// Handle, if set, is the router's handle (NewRouterHandle), to
	// navigate it from outside it: a menu bar's command, a test. UseRouter
	// under it returns the same.
	Handle *RouterHandle
}

// Router shows its routes' screens, from the address the app is at. It
// starts at the link the app was opened with (a link of its scheme, a
// universal or app link, a notification's), else where its window was
// left (a browser's address and history; a desktop window's last, as it
// keeps where it was), else at Initial. Links the app is opened with
// later go to it too, and those no route takes to ui.OnOpenURL. A
// window's first Router has its address; components under it navigate
// with UseRouter, Navigate, a Link or a ViewProps' Href.
func Router(p RouterProps, routes ...Route) Node {
	r := current()
	win := windowCtx.Use()
	if win == nil {
		win = r.win
	}
	rt := &router{r: r, win: win, props: p, screens: map[*stackScreen]*entry{}, stacks: map[*stackState]*navInst{}, version: signal.New(0)}
	rt.api = p.Handle
	if rt.api == nil {
		rt.api = &RouterHandle{}
	}
	rt.api.rt = rt
	rt.compile(routes)
	if win.router == nil {
		win.router = rt
		rt.addressed = true
	}
	var n Node
	routerCtx.Provide(rt.api, func() {
		rt.owner = signal.CurrentOwner()
		d := rt.start()
		rt.inst = newInst(rt.tree, rt.owner)
		rt.applying++
		n = rt.view(rt.inst, d)
		rt.applying--
		rt.watchGuards()
	})
	rt.last = rt.snapshot()
	rt.lastHref = rt.descHref(rt.last)
	rt.lastState = rt.encode(rt.last)
	rt.href = signal.New(rt.lastHref)
	rt.listen()
	rt.send(ops.LocationActionReplace, 0)
	routers = append(routers, rt)
	signal.OnCleanup(func() {
		rt.dead = true
		routers = slices.DeleteFunc(routers, func(x *router) bool { return x == rt })
		if win.router == rt {
			win.router = nil
		}
		if e := rt.windowEl(); e != nil && e.h != nil && rt.addressed {
			e.h.location = nil
		}
	})
	return n
}

// routers are the mounted Routers, the first mounted first: links the app
// is opened with go to the first that takes them.
var routers []*router

var routerCtx = signal.NewContext[*RouterHandle](nil)
var routeCtx = signal.NewContext[*entry](nil)

// RouterHandle navigates a Router by href.
type RouterHandle struct{ rt *router }

// NewRouterHandle makes a handle for a Router (RouterProps.Handle), to
// navigate it from outside it. Its methods panic until the Router
// mounts.
func NewRouterHandle() *RouterHandle { return &RouterHandle{} }

func (h *RouterHandle) router() *router {
	if h.rt == nil {
		panic("ui: a RouterHandle used before its Router mounted")
	}
	return h.rt
}

// UseRouter returns the Router the calling component is in (nil if none).
// Call it while building the component.
func UseRouter() *RouterHandle { return routerCtx.Use() }

// UseParams returns the parameters of the route whose screen the calling
// component is in (none outside a Router's screens). Call it while
// building the component.
func UseParams() Params { return Params{routeCtx.Use()} }

// activeRouter is the Router new navigation goes to from outside a
// Router's components: the window's of the view whose event is handled,
// else the main window's, else the first mounted.
func activeRouter() *router {
	if e := eventEl; e != nil && !e.dead {
		if w := e.window(); w != nil && w.h != nil && w.h.win != nil && w.h.win.router != nil {
			return w.h.win.router
		}
	}
	for _, rt := range routers {
		if rt.win.main {
			return rt
		}
	}
	if len(routers) > 0 {
		return routers[0]
	}
	return nil
}

func routerFor(h *RouterHandle) *router {
	if h != nil {
		return h.router()
	}
	if h := routerCtx.Use(); h != nil {
		return h.rt
	}
	rt := activeRouter()
	if rt == nil {
		panic("ui: navigating by href with no Router mounted")
	}
	return rt
}

// Navigate goes to href in the Router the calling code belongs to: a
// component's (UseRouter's), else the window's of the event being
// handled, else the main window's. See RouterHandle.Navigate.
func Navigate(href Href) { routerFor(nil).navigate(href, modeNavigate) }

// Navigate goes to href: the tabs, destinations and slots on the way to
// its screen switch to it; in its stack, an earlier screen with the same
// address is gone back to, else it's pushed; a sheet's is presented. Its
// query reaches a screen that's kept (Params.Query follows it).
func (h *RouterHandle) Navigate(href Href) { h.router().navigate(href, modeNavigate) }

// Push pushes href's screen in its stack even when one with the same
// address is there.
func (h *RouterHandle) Push(href Href) { h.router().navigate(href, modePush) }

// Replace shows href's screen in place of the current one, which is
// disposed: the history doesn't grow (a browser's entry is replaced).
func (h *RouterHandle) Replace(href Href) { h.router().navigate(href, modeReplace) }

// Back goes back as the user's back would, without asking OnBack: the
// newest sheet is dismissed, else the innermost stack on screen pops.
// It reports whether there was somewhere to go back to.
func (h *RouterHandle) Back() bool { return h.router().back() }

// CanGoBack reports whether Back would go back.
func (h *RouterHandle) CanGoBack() bool {
	d := h.router().snapshot()
	return backDesc(d)
}

// Dismiss dismisses the newest sheet routed over the screens, reporting
// whether there was one.
func (h *RouterHandle) Dismiss() bool { return h.router().dismiss() }

// SetQuery sets the shown screen's query parameters, keeping the screen:
// a search's text, a filter, in the address (a browser's entry replaced).
func (h *RouterHandle) SetQuery(q url.Values) { h.router().setQuery(q) }

// Href is the address shown: the focused screen's href. It's an accessor:
// a link or a sidebar can follow it.
func (h *RouterHandle) Href() signal.Accessor[Href] { return h.router().href }

// Takes reports whether a route of the router takes href (redirects and
// guards aside); the not-found screen doesn't count.
func (h *RouterHandle) Takes(href Href) bool { return h.router().takes(href) }

type navMode uint8

const (
	modeNavigate navMode = iota
	modePush
	modeReplace
)

// --- The routes ---

// rnode is a compiled route.
type rnode struct {
	r       *Route
	layout  Layout
	leaf    bool
	nav     *rnode   // the navigator it's a member of (nil: the tree's root)
	members []*rnode // a navigator's: its children, groups flattened
	slots   []*rnode // a navigator's members that aren't sheets: its screens, tabs, destinations
	index   int      // its place among nav.members
	slot    int      // its place among nav.slots (-1: a sheet)
	segs    []seg    // its full pattern
	pattern string   // as text
	key     string   // its key among its navigator's members (saved states)
	guards  []func(Href) Href
	initial *rnode // a navigator's first slot
	section *rnode // a split's destination: the group (sidebar section) it's in
	order   int    // a leaf's place among the leaves, as declared
}

func (n *rnode) sheet() bool { return n.r.Sheet != nil }

func (n *rnode) what() string {
	switch {
	case n.leaf:
		return "screen " + n.pattern
	case n.layout == LayoutStack:
		return "stack " + n.pattern
	case n.layout == LayoutTabs:
		return "tabs " + n.pattern
	case n.layout == LayoutSplit:
		return "split view " + n.pattern
	}
	return "slot " + n.pattern
}

func (rt *router) compile(routes []Route) {
	root := &rnode{r: &Route{}, layout: LayoutSlot, pattern: "/"}
	rt.tree = root
	var walk func(nav *rnode, prefix []seg, guards []func(Href) Href, section *rnode, rs []Route)
	walk = func(nav *rnode, prefix []seg, guards []func(Href) Href, section *rnode, rs []Route) {
		for i := range rs {
			rr := &rs[i]
			segs := parsePattern(rr.Path)
			if !strings.HasPrefix(rr.Path, "/") {
				segs = append(slices.Clone(prefix), segs...)
			}
			for j, s := range segs {
				if s.kind == segRest && j != len(segs)-1 {
					panic(fmt.Sprintf("ui: Router: route %q: a catch-all must be last", rr.Path))
				}
			}
			g := guards
			if rr.Guard != nil {
				g = append(slices.Clone(guards), rr.Guard)
			}
			n := &rnode{r: rr, layout: rr.Layout, nav: nav, segs: segs, pattern: patternText(segs), guards: g, section: section}
			leaf := rr.Screen != nil || rr.Redirect != ""
			switch {
			case leaf:
				if rr.Layout != LayoutGroup || len(rr.Children) > 0 {
					panic(fmt.Sprintf("ui: Router: route %s has a Screen (or a Redirect) and Children: a screen's route has none", n.pattern))
				}
				n.leaf = true
				n.order = len(rt.leaves)
				rt.member(nav, n)
				rt.leaves = append(rt.leaves, n)
			case rr.Layout == LayoutGroup:
				if rr.Sheet != nil {
					panic(fmt.Sprintf("ui: Router: group %s is a Sheet: give it a Layout (LayoutStack) to make it one", n.pattern))
				}
				sec := section
				if nav.layout == LayoutSplit {
					sec = n
				}
				walk(nav, segs, g, sec, rr.Children)
			default:
				rt.member(nav, n)
				walk(n, segs, g, nil, rr.Children)
			}
		}
	}
	walk(root, nil, nil, nil, routes)
	nf := &rnode{r: &Route{Path: "/*path", Screen: rt.notFoundScreen}, leaf: true, nav: root,
		segs: []seg{{segRest, "path"}}, pattern: "/*path", order: len(rt.leaves)}
	rt.member(root, nf)
	rt.leaves = append(rt.leaves, nf)
	rt.notFound = nf
	slices.SortStableFunc(rt.leaves, func(a, b *rnode) int {
		if rankLess(a.segs, b.segs) {
			return -1
		}
		if rankLess(b.segs, a.segs) {
			return 1
		}
		return a.order - b.order
	})
	rt.check(root)
}

// member adds n to nav's members.
func (rt *router) member(nav, n *rnode) {
	n.index = len(nav.members)
	nav.members = append(nav.members, n)
	n.slot = -1
	if !n.sheet() {
		n.slot = len(nav.slots)
		nav.slots = append(nav.slots, n)
	}
	n.key = n.pattern
	for _, m := range nav.members[:n.index] {
		if m.key == n.key {
			n.key = fmt.Sprintf("%s~%d", n.pattern, n.index)
		}
	}
}

// check finds each navigator's first slot and panics at shapes the
// navigators can't show.
func (rt *router) check(n *rnode) {
	if n.leaf {
		return
	}
	if len(n.slots) == 0 && n != rt.tree {
		panic(fmt.Sprintf("ui: Router: %s has no routes to show (only sheets, or none)", n.what()))
	}
	n.initial = nil
	if n.r.Initial != "" {
		want := parsePattern(n.r.Initial)
		if !strings.HasPrefix(n.r.Initial, "/") {
			want = append(slices.Clone(n.segs), want...)
		}
		for _, m := range n.slots {
			if patternText(m.segs) == patternText(want) {
				n.initial = m
			}
		}
		if n.initial == nil {
			panic(fmt.Sprintf("ui: Router: %s's Initial %q is none of its routes", n.what(), n.r.Initial))
		}
	} else if len(n.slots) > 0 {
		n.initial = n.slots[0]
	}
	for _, m := range n.members {
		if !m.sheet() {
			switch n.layout {
			case LayoutStack:
				if !m.leaf {
					panic(fmt.Sprintf("ui: Router: %s is in a stack: a stack's routes are screens, or sheets (Sheet) of any kind", m.what()))
				}
			case LayoutTabs, LayoutSplit:
				kind := "a tab"
				if n.layout == LayoutSplit {
					kind = "a destination"
				}
				if !m.leaf && m.layout != LayoutStack {
					panic(fmt.Sprintf("ui: Router: %s is %s: a tab or a destination is a screen or a stack", m.what(), kind))
				}
				if m.leaf && countParams(m.segs) > 0 {
					panic(fmt.Sprintf("ui: Router: %s is %s with parameters: a tab's or a destination's screen has none (put it in a stack)", m.what(), kind))
				}
			}
		}
		rt.check(m)
	}
}

// notFoundScreen is the screen for an href no route takes, unless the app
// gives its own.
func (rt *router) notFoundScreen(p Params) Node {
	if f := rt.props.NotFound; f != nil {
		return f(p)
	}
	return SafeArea(Style{Padding: All(24), Gap: 12, Align: AlignCenter, Justify: JustifyCenter, Grow: 1},
		StaticText("This screen doesn't exist."),
		StaticText("/"+p.Get("path"), Style{Color: colors.SecondaryLabel}),
		Button(ButtonProps{Title: Val("Go home"), Href: "/"}),
	)
}

// link is a step from a navigator to one of its members, on the way to a
// screen.
type link struct{ nav, member *rnode }

func chainOf(l *rnode) []link {
	var c []link
	for m := l; m.nav != nil; m = m.nav {
		c = append(c, link{m.nav, m})
	}
	slices.Reverse(c)
	return c
}

// target is a screen an href resolved to.
type target struct {
	leaf   *rnode
	params map[string]string
	query  string
}

func (t *target) href() Href { return leafHref(t.leaf, t.params, t.query) }

func leafHref(l *rnode, params map[string]string, query string) Href {
	p, _ := fillPattern(l.segs, params)
	return joinHref(p, query)
}

// match is the leaf an href's path matches, most specific first.
func (rt *router) match(h Href) (*rnode, map[string]string, string) {
	path, q := hrefParts(h)
	for _, l := range rt.leaves {
		if p, ok := match(l.segs, path); ok {
			return l, p, q
		}
	}
	return rt.notFound, map[string]string{"path": strings.Join(path, "/")}, q
}

func (rt *router) takes(h Href) bool {
	l, _, _ := rt.match(h)
	return l != rt.notFound
}

// resolve is the screen h names after redirects and guards.
func (rt *router) resolve(h Href) target {
	from := h
	for range 32 {
		l, p, q := rt.match(h)
		if l.r.Redirect != "" {
			to, ok := fillPattern(parsePattern(l.r.Redirect), p)
			if !ok {
				to = Href(l.r.Redirect).Path()
			}
			rq := Href(l.r.Redirect).Query().Encode()
			if rq == "" {
				rq = q
			}
			h = joinHref(to, rq)
			continue
		}
		t := target{l, p, q}
		if to := rt.denied(l, t.href()); to != "" {
			h = to
			continue
		}
		return t
	}
	panic(fmt.Sprintf("ui: Router: redirects and guards go round in a loop from %q (now at %q)", from, h))
}

// denied is where l's guards send the user instead of h ("" lets them in).
func (rt *router) denied(l *rnode, h Href) Href {
	for _, g := range l.guards {
		if to := signal.Untrack(func() Href { return g(h) }); to != "" && to != h {
			return to
		}
	}
	return ""
}

// --- The state, described ---

// navDesc describes a navigator's state.
type navDesc struct {
	node    *rnode
	sel     int          // tabs, a split view, a slot: the slot shown
	entries []*entryDesc // a stack's, bottom first; tabs': one per slot (nil: not made); a split's and a slot's: the shown one, then a split's detail screens pushed with a Navigator
	sheets  []*entryDesc // over it, the oldest first
}

// entryDesc describes a screen of a navigator, or a navigator in it.
type entryDesc struct {
	key    uint64 // the live entry it describes (0: one to make)
	member *rnode // nil: a screen pushed with a Navigator, which has no route
	params map[string]string
	query  string
	child  *navDesc // a navigator's
}

func (d *navDesc) clone() *navDesc {
	if d == nil {
		return nil
	}
	c := &navDesc{node: d.node, sel: d.sel}
	for _, e := range d.entries {
		c.entries = append(c.entries, e.clone())
	}
	for _, e := range d.sheets {
		c.sheets = append(c.sheets, e.clone())
	}
	return c
}

func (e *entryDesc) clone() *entryDesc {
	if e == nil {
		return nil
	}
	c := *e
	c.child = e.child.clone()
	return &c
}

// content is the entry a navigator shows under its sheets: a stack's top,
// the shown tab's, a split's or a slot's.
func (d *navDesc) content() *entryDesc {
	switch d.node.layout {
	case LayoutStack:
		if n := len(d.entries); n > 0 {
			return d.entries[n-1]
		}
	case LayoutTabs:
		if d.sel >= 0 && d.sel < len(d.entries) {
			return d.entries[d.sel]
		}
	default:
		if n := len(d.entries); n > 0 {
			return d.entries[n-1]
		}
	}
	return nil
}

// focus is the entry focused: the newest sheet, else the content.
func (d *navDesc) focus() *entryDesc {
	if n := len(d.sheets); n > 0 {
		return d.sheets[n-1]
	}
	return d.content()
}

// routedFocus is the focused entry that has a route: a screen pushed with
// a Navigator has its stack's address under it.
func (d *navDesc) routedFocus() *entryDesc {
	if n := len(d.sheets); n > 0 {
		return d.sheets[n-1]
	}
	if d.node.layout == LayoutTabs {
		return d.content()
	}
	for i := len(d.entries) - 1; i >= 0; i-- {
		if d.entries[i] != nil && d.entries[i].member != nil {
			return d.entries[i]
		}
	}
	return nil
}

// histLen is how much history the navigator stands for: a stack's
// screens, a tab or a destination other than the first one more than the
// first (react-navigation's "firstRoute"), and its sheets.
func (d *navDesc) histLen() int {
	n := len(d.sheets)
	switch d.node.layout {
	case LayoutStack:
		n += len(d.entries)
	default:
		n++
		if d.node.initial != nil && d.sel != d.node.initial.slot {
			n++
		}
		if d.node.layout == LayoutSplit && len(d.entries) > 1 {
			n += len(d.entries) - 1
		}
	}
	return n
}

func sameEntry(a, b *entryDesc) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.key != 0 && b.key != 0 {
		return a.key == b.key
	}
	return a.member != nil && a.member == b.member && maps.Equal(a.params, b.params)
}

// historyDelta is how the history moved from o to n: more than 0 a push
// (deeper), less a back by as many, 0 a replace. It's decided where the
// focused screens part: by the navigator's history length there, as
// react-navigation's linking does.
func historyDelta(o, n *navDesc) int {
	for o != nil && n != nil && o.node == n.node {
		ol, nl := o.histLen(), n.histLen()
		if ol != nl {
			return nl - ol
		}
		of, nf := o.focus(), n.focus()
		if of == nil || nf == nil || !sameEntry(of, nf) {
			return 0
		}
		o, n = of.child, nf.child
	}
	return 0
}

// descHref is the address of the state: its focused routed screen's.
func (rt *router) descHref(d *navDesc) Href {
	for d != nil {
		f := d.routedFocus()
		if f == nil || f.member == nil {
			return Href(d.node.pattern)
		}
		if f.member.leaf {
			return leafHref(f.member, f.params, f.query)
		}
		d = f.child
	}
	return "/"
}

// fresh describes navigator nav as a link to chain's screen (none: its
// default) builds it: the stack's Initial screen under the one linked
// to, the other tabs at their first screens.
func (rt *router) fresh(nav *rnode, chain []link, t *target) *navDesc {
	d := &navDesc{node: nav}
	var m *rnode
	var rest []link
	if len(chain) > 0 {
		m, rest = chain[0].member, chain[1:]
	}
	switch nav.layout {
	case LayoutStack:
		if (m == nil || m.sheet() || m != nav.initial) && nav.initial != nil {
			if e := rt.freshEntry(nav.initial, nil, t); e != nil {
				d.entries = append(d.entries, e)
			}
		}
		// The screens up its path, as the stack has them: /inbox/:id
		// under /inbox/:id/reply.
		if m != nil {
			var up []*rnode
			for _, s := range nav.slots {
				if s != nav.initial && s != m && s.leaf && isPrefix(s.segs, m.segs) {
					up = append(up, s)
				}
			}
			slices.SortStableFunc(up, func(a, b *rnode) int { return len(a.segs) - len(b.segs) })
			for _, s := range up {
				if e := rt.freshEntry(s, nil, t); e != nil {
					d.entries = append(d.entries, e)
				}
			}
		}
		if m != nil && !m.sheet() {
			d.entries = append(d.entries, rt.freshEntry(m, rest, t))
		}
	case LayoutTabs:
		d.sel = nav.initial.slot
		if m != nil && !m.sheet() {
			d.sel = m.slot
		}
		d.entries = make([]*entryDesc, len(nav.slots))
		for i, s := range nav.slots {
			if s == m {
				d.entries[i] = rt.freshEntry(s, rest, t)
			} else {
				d.entries[i] = rt.freshEntry(s, nil, nil)
			}
		}
	default:
		s, r, tt := nav.initial, []link(nil), (*target)(nil)
		if m != nil && !m.sheet() {
			s, r, tt = m, rest, t
		}
		if s != nil {
			d.sel = s.slot
			if e := rt.freshEntry(s, r, tt); e != nil {
				d.entries = []*entryDesc{e}
			}
		}
	}
	if m != nil && m.sheet() {
		d.sheets = []*entryDesc{rt.freshEntry(m, rest, t)}
	}
	return d
}

// isPrefix reports whether pattern a is a proper prefix of pattern b.
func isPrefix(a, b []seg) bool {
	if len(a) == 0 || len(a) >= len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// freshEntry describes member m as a link to rest's screen builds it; a
// screen under the one linked to (t's) takes its parameters from it, by
// name, and isn't made if it needs one t hasn't or a guard keeps the user
// out.
func (rt *router) freshEntry(m *rnode, rest []link, t *target) *entryDesc {
	e := &entryDesc{member: m}
	if !m.leaf {
		e.child = rt.fresh(m, rest, t)
		return e
	}
	if t != nil && t.leaf == m {
		e.params, e.query = t.params, t.query
		return e
	}
	if m.r.Redirect != "" {
		return nil
	}
	for _, s := range m.segs {
		if s.kind == segStatic {
			continue
		}
		v, ok := "", false
		if t != nil {
			v, ok = t.params[s.text]
		}
		if !ok && s.kind == segParam {
			return nil
		}
		if e.params == nil {
			e.params = map[string]string{}
		}
		e.params[s.text] = v
	}
	if rt.denied(m, leafHref(m, e.params, "")) != "" {
		return nil
	}
	return e
}

// step changes d, the state of chain's first navigator, to show t.
func (rt *router) step(d *navDesc, chain []link, t *target, mode navMode) {
	m, rest := chain[0].member, chain[1:]
	if m.sheet() {
		for i, s := range d.sheets {
			if s.member != m || (m.leaf && (mode == modePush || !maps.Equal(s.params, t.params))) {
				continue
			}
			d.sheets = d.sheets[:i+1]
			switch {
			case !m.leaf:
				rt.step(s.child, rest, t, mode)
			case mode == modeReplace:
				d.sheets[i] = rt.freshEntry(m, nil, t)
			default:
				s.query = t.query
			}
			return
		}
		e := rt.freshEntry(m, rest, t)
		if mode == modeReplace && len(rest) == 0 && len(d.sheets) > 0 {
			d.sheets[len(d.sheets)-1] = e
		} else {
			d.sheets = append(d.sheets, e)
		}
		return
	}
	if len(d.sheets) > 0 {
		d.sheets = nil // going to the navigator's own screens closes what's over them
		if mode == modeReplace {
			mode = modeNavigate // the sheet was what's replaced
		}
	}
	switch d.node.layout {
	case LayoutStack:
		n := len(d.entries)
		switch mode {
		case modeNavigate:
			for i := n - 1; i >= 0; i-- {
				if e := d.entries[i]; e != nil && e.member == m && maps.Equal(e.params, t.params) {
					d.entries = d.entries[:i+1]
					e.query = t.query
					return
				}
			}
			d.entries = append(d.entries, rt.freshEntry(m, nil, t))
		case modePush:
			d.entries = append(d.entries, rt.freshEntry(m, nil, t))
		case modeReplace:
			if n > 0 {
				d.entries[n-1] = rt.freshEntry(m, nil, t)
			} else {
				d.entries = append(d.entries, rt.freshEntry(m, nil, t))
			}
		}
	case LayoutTabs:
		d.sel = m.slot
		rt.stepInto(&d.entries[m.slot], m, rest, t, mode)
	default:
		if d.sel != m.slot || len(d.entries) == 0 {
			d.sel = m.slot
			d.entries = []*entryDesc{rt.freshEntry(m, rest, t)}
			return
		}
		d.entries = d.entries[:1] // a split's detail screens pushed with a Navigator go
		rt.stepInto(&d.entries[0], m, rest, t, mode)
	}
}

// stepInto goes on to t in a tab's, a destination's or a slot's entry.
func (rt *router) stepInto(p **entryDesc, m *rnode, rest []link, t *target, mode navMode) {
	e := *p
	if e == nil || e.member != m || (m.leaf && (mode == modeReplace || !maps.Equal(e.params, t.params))) {
		*p = rt.freshEntry(m, rest, t)
		return
	}
	if m.leaf {
		e.query = t.query
		return
	}
	rt.step(e.child, rest, t, mode)
}

// backDesc goes back in d: the innermost first, the newest sheet, else
// the stack's top screen; false when there's nowhere to go.
func backDesc(d *navDesc) bool {
	if f := d.focus(); f != nil && f.child != nil && backDesc(f.child) {
		return true
	}
	if n := len(d.sheets); n > 0 {
		d.sheets = d.sheets[:n-1]
		return true
	}
	switch d.node.layout {
	case LayoutStack:
		if n := len(d.entries); n > 1 {
			d.entries = d.entries[:n-1]
			return true
		}
	case LayoutSplit:
		if n := len(d.entries); n > 1 {
			d.entries = d.entries[:n-1]
			return true
		}
	}
	return false
}

// --- The live state ---

type router struct {
	r         *Root
	win       *WindowHandle
	api       *RouterHandle
	props     RouterProps
	tree      *rnode
	leaves    []*rnode // most specific first
	notFound  *rnode
	owner     *signal.Owner
	inst      *navInst
	addressed bool // the window's address is this router's
	dead      bool

	screens  map[*stackScreen]*entry
	stacks   map[*stackState]*navInst
	nextKey  uint64
	applying int // the router is changing the views: the stacks' and sheets' reports are its own

	last      *navDesc // the state whose address the host was last sent
	lastHref  Href
	lastState []byte
	href      *signal.Signal[Href]
	version   *signal.Signal[int] // counts the state's changes: the guards are asked of the screens shown now
}

// newInst is a navigator shown, its entries made under owner: the sheets
// it presents go with it.
func newInst(node *rnode, owner *signal.Owner) *navInst {
	in := &navInst{node: node, owner: owner}
	signal.RunWithOwner(owner, func() {
		signal.OnCleanup(func() {
			for _, s := range slices.Backward(in.sheets) {
				s.sheet.close()
			}
			in.sheets = nil
		})
	})
	return in
}

// navInst is a navigator shown.
type navInst struct {
	node    *rnode
	owner   *signal.Owner // what its entries are made under
	entries []*entry      // as navDesc's
	sel     int
	sheets  []*entry

	st     *stackState         // a stack's, or a split view's detail stack
	selSig *signal.Signal[int] // tabs', a split view's
	pages  []*slotView         // tabs'
	slot   *slotView           // a slot's
	split  *navInst            // a split view's stack destination: the split view, whose detail stack it shares
	shared bool                // a split view's stack destination
}

// entry is a screen shown, or a navigator in one.
type entry struct {
	key     uint64
	member  *rnode
	params  map[string]string
	query   *signal.Signal[string]
	nav     *navInst // the navigator it's in
	child   *navInst
	screen  *stackScreen
	sheet   *sheetView
	dispose func() // a split view's stack destination's owner
}

func (e *entry) href() Href {
	if e.member == nil || !e.member.leaf {
		return ""
	}
	return leafHref(e.member, e.params, e.query.Peek())
}

func (rt *router) newEntry(nav *navInst, d *entryDesc) *entry {
	k := d.key
	if k == 0 {
		rt.nextKey++
		k = rt.nextKey
	}
	return &entry{key: k, member: d.member, params: d.params, query: newQuery(d.query), nav: nav}
}

func sameLive(e *entry, d *entryDesc) bool {
	if e == nil || d == nil {
		return e == nil && d == nil
	}
	if d.key != 0 {
		return e.key == d.key
	}
	return e.member != nil && e.member == d.member && maps.Equal(e.params, d.params)
}

// snapshot describes the live state.
func (rt *router) snapshot() *navDesc { return snapshotNav(rt.inst) }

func snapshotNav(n *navInst) *navDesc {
	d := &navDesc{node: n.node, sel: n.sel}
	for _, e := range n.entries {
		d.entries = append(d.entries, snapshotEntry(e))
	}
	for _, e := range n.sheets {
		d.sheets = append(d.sheets, snapshotEntry(e))
	}
	return d
}

func snapshotEntry(e *entry) *entryDesc {
	if e == nil {
		return nil
	}
	d := &entryDesc{key: e.key, member: e.member, params: e.params}
	if e.query != nil {
		d.query = e.query.Peek()
	}
	if e.child != nil {
		d.child = snapshotNav(e.child)
	}
	return d
}

// slotView is a place whose one child the router sets: a tab's page, a
// slot.
type slotView struct {
	f       *element
	owner   *signal.Owner
	shown   *element
	dispose func()
}

func newSlotView() *slotView {
	sv := &slotView{f: newFragment(), owner: signal.CurrentOwner()}
	signal.OnCleanup(func() {
		if sv.dispose != nil {
			sv.dispose()
			sv.dispose = nil
		}
	})
	return sv
}

func (sv *slotView) node() Node { return Node{sv.f} }

func (sv *slotView) clear() {
	if sv.shown != nil {
		sv.f.removeKid(sv.shown, true)
		sv.shown = nil
	}
	if sv.dispose != nil {
		sv.dispose()
		sv.dispose = nil
	}
}

func (sv *slotView) set(build func() Node) {
	sv.clear()
	signal.RunWithOwner(sv.owner, func() {
		signal.Root(func(d func()) {
			sv.dispose = d
			if n := withRoot(sv.f.r, build); n.e != nil {
				fill(n)
				sv.shown = n.e
				sv.f.appendKid(n.e)
			}
		})
	})
}

// view makes inst's views, showing d.
func (rt *router) view(inst *navInst, d *navDesc) Node {
	n := inst.node
	switch n.layout {
	case LayoutStack:
		node, st := newStack(nil, ScreenOptions{}, nil)
		rt.ownStack(st, inst)
		rt.reconcile(inst, d)
		return node
	case LayoutTabs:
		inst.selSig = signal.New(d.sel)
		inst.pages = make([]*slotView, len(n.slots))
		inst.entries = make([]*entry, len(n.slots))
		tabs := make([]Tab, len(n.slots))
		for i, s := range n.slots {
			tabs[i] = Tab{Title: s.r.Title, Icon: s.r.Icon, Badge: s.r.Badge, Content: func() Node {
				sv := newSlotView()
				inst.pages[i] = sv
				return sv.node()
			}}
		}
		node := Tabs(TabsProps{Selected: inst.selSig, chosen: func(i int) { rt.chose(inst, i) }}, tabs...)
		rt.reconcile(inst, d)
		return node
	case LayoutSplit:
		inst.selSig = signal.New(d.sel)
		var sections []SidebarSection
		for _, s := range n.slots {
			if len(sections) == 0 || sections[len(sections)-1].group != s.section {
				var title signal.Accessor[string]
				if s.section != nil {
					title = s.section.r.Title
				}
				sections = append(sections, SidebarSection{Title: title, group: s.section})
			}
			sec := &sections[len(sections)-1]
			sec.Destinations = append(sec.Destinations, Destination{Title: s.r.Title, Icon: s.r.Icon, Badge: s.r.Badge})
		}
		node := SplitView(SplitViewProps{Title: n.r.Title, Sections: sections, Selected: inst.selSig, routed: &splitRouting{
			stack:  func(st *stackState) { rt.ownStack(st, inst) },
			chosen: func(i int) { rt.chose(inst, i) },
		}})
		rt.reconcile(inst, d)
		return node
	default:
		inst.slot = newSlotView()
		rt.reconcile(inst, d)
		return inst.slot.node()
	}
}

// ownStack makes st a routed stack of inst's: its screens pushed and
// popped with a Navigator, and the user's pops, are the router's to know.
func (rt *router) ownStack(st *stackState, inst *navInst) {
	inst.st = st
	rt.stacks[st] = inst
	st.e.r.batch.SetBool(st.e.id, ops.PropStackRouted, true)
	st.pushed = func(s *stackScreen) {
		if rt.applying > 0 || rt.dead {
			return
		}
		in := rt.stacks[st]
		if in == nil {
			return
		}
		e := &entry{key: rt.key(), nav: in, screen: s}
		rt.screens[s] = e
		in.entries = append(in.entries, e)
		rt.changed()
	}
	st.removed = func(s *stackScreen) {
		e := rt.screens[s]
		delete(rt.screens, s)
		if rt.applying > 0 || rt.dead || e == nil {
			return
		}
		in := e.nav
		in.entries = slices.DeleteFunc(in.entries, func(x *entry) bool { return x == e })
		rt.changed()
	}
}

func (rt *router) key() uint64 {
	rt.nextKey++
	return rt.nextKey
}

// screen builds a leaf entry's screen.
func (rt *router) screen(e *entry) Node {
	var n Node
	routeCtx.Provide(e, func() { n = e.member.r.Screen(Params{e}) })
	return n
}

// options are a leaf entry's screen's, made where its navigator's views
// are (UseRouter works there).
func (rt *router) options(inst *navInst, e *entry) ScreenOptions {
	var o ScreenOptions
	if f := e.member.r.Options; f != nil {
		signal.RunWithOwner(inst.owner, func() {
			routeCtx.Provide(e, func() { o = f(Params{e}) })
		})
	}
	if o.Title == nil {
		o.Title = e.member.r.Title
	}
	return o
}

// content builds what an entry shows in a tab's page, a slot or a sheet:
// a screen, or its navigator's views.
func (rt *router) content(nav *navInst, e *entry, d *entryDesc) Node {
	if e.member.leaf {
		return rt.screen(e)
	}
	e.child = newInst(e.member, signal.CurrentOwner())
	return rt.view(e.child, d.child)
}

// reconcile changes inst's views to show d.
func (rt *router) reconcile(inst *navInst, d *navDesc) {
	rt.applying++
	defer func() { rt.applying-- }()
	// The sheets that go, first: the newest first.
	k := 0
	for k < len(inst.sheets) && k < len(d.sheets) && sameLive(inst.sheets[k], d.sheets[k]) {
		k++
	}
	for i := len(inst.sheets) - 1; i >= k; i-- {
		inst.sheets[i].sheet.close()
	}
	inst.sheets = inst.sheets[:k]
	switch inst.node.layout {
	case LayoutStack:
		rt.reconcileStack(inst, d.entries)
	case LayoutTabs:
		for i := range inst.node.slots {
			var want *entryDesc
			if i < len(d.entries) {
				want = d.entries[i]
			}
			rt.reconcileSlot(inst, inst.pages[i], &inst.entries[i], want)
		}
		inst.sel = d.sel
		inst.selSig.Set(d.sel)
	case LayoutSplit:
		rt.reconcileSplit(inst, d)
	default:
		var have *entry
		if len(inst.entries) > 0 {
			have = inst.entries[0]
		}
		var want *entryDesc
		if len(d.entries) > 0 {
			want = d.entries[0]
		}
		rt.reconcileSlot(inst, inst.slot, &have, want)
		inst.entries = inst.entries[:0]
		if have != nil {
			inst.entries = append(inst.entries, have)
		}
		inst.sel = d.sel
	}
	for i := 0; i < k; i++ {
		rt.update(inst.sheets[i], d.sheets[i])
	}
	for _, sd := range d.sheets[k:] {
		e := rt.newEntry(inst, sd)
		rt.present(inst, e, sd)
		inst.sheets = append(inst.sheets, e)
	}
}

// update brings a kept entry to d: its query, its navigator's state.
func (rt *router) update(e *entry, d *entryDesc) {
	if e.query != nil {
		e.query.Set(d.query)
	}
	if e.child != nil && d.child != nil {
		rt.reconcile(e.child, d.child)
	}
}

// reconcileStack changes a stack's screens to want's: those both have
// kept, the rest of the old popped (the lower ones first, the top last,
// so one pop animates) or, when there are new ones, gone under them.
func (rt *router) reconcileStack(inst *navInst, want []*entryDesc) {
	k := 0
	for k < len(inst.entries) && k < len(want) && sameLive(inst.entries[k], want[k]) {
		k++
	}
	for i := 0; i < k; i++ {
		rt.update(inst.entries[i], want[i])
	}
	old := slices.Clone(inst.entries[k:])
	inst.entries = inst.entries[:k]
	for _, d := range want[k:] {
		if d == nil || d.member == nil {
			continue // a screen a Navigator pushed: it can't be made again
		}
		e := rt.newEntry(inst, d)
		rt.pushScreen(inst, e)
		inst.entries = append(inst.entries, e)
	}
	for _, e := range old {
		if e.screen != nil && slices.Contains(inst.st.screens, e.screen) {
			inst.st.remove(e.screen)
		}
		delete(rt.screens, e.screen)
	}
}

func (rt *router) pushScreen(inst *navInst, e *entry) {
	e.nav = inst
	s := inst.st.push(func() Node { return rt.screen(e) }, rt.options(inst, e))
	e.screen = s
	rt.screens[s] = e
}

// reconcileSlot changes what a tab's page or a slot shows to want.
func (rt *router) reconcileSlot(inst *navInst, sv *slotView, have **entry, want *entryDesc) {
	if *have != nil && sameLive(*have, want) {
		rt.update(*have, want)
		return
	}
	*have = nil
	sv.clear()
	if want == nil {
		return
	}
	e := rt.newEntry(inst, want)
	*have = e
	sv.set(func() Node { return rt.content(inst, e, want) })
}

// reconcileSplit changes a split view's destination and its detail
// stack's screens to d's.
func (rt *router) reconcileSplit(inst *navInst, d *navDesc) {
	inst.sel = d.sel
	inst.selSig.Set(d.sel)
	st := inst.st
	var want *entryDesc
	if len(d.entries) > 0 {
		want = d.entries[0]
	}
	if len(inst.entries) > 0 && sameLive(inst.entries[0], want) {
		dest := inst.entries[0]
		if dest.child != nil {
			rt.reconcile(dest.child, want.child)
			return
		}
		rt.update(dest, want)
		// A screen destination's detail screens pushed with a Navigator:
		// those want doesn't keep go.
		k := 1
		for k < len(inst.entries) && k < len(d.entries) && sameLive(inst.entries[k], d.entries[k]) {
			k++
		}
		for _, e := range slices.Backward(inst.entries[k:]) {
			if e.screen != nil && slices.Contains(st.screens, e.screen) {
				st.remove(e.screen)
			}
			delete(rt.screens, e.screen)
		}
		inst.entries = inst.entries[:k]
		return
	}
	// Another destination: its first screen replaces the stack's at once,
	// and what the last one presented goes with it.
	for _, e := range inst.entries {
		if e.dispose != nil {
			e.dispose()
		}
	}
	if len(st.screens) > 0 {
		st.e.r.batch.Command(st.e.id, ops.CmdReplace, nil)
	}
	for len(st.screens) > 0 {
		s := st.screens[len(st.screens)-1]
		delete(rt.screens, s)
		st.remove(s)
	}
	rt.stacks[st] = inst
	inst.entries = inst.entries[:0]
	if want == nil {
		return
	}
	e := rt.newEntry(inst, want)
	inst.entries = append(inst.entries, e)
	if e.member.leaf {
		rt.pushScreen(inst, e)
		return
	}
	// A stack destination: its screens are the detail stack's; its sheets
	// go as another destination is chosen.
	var o *signal.Owner
	signal.RunWithOwner(inst.owner, func() { o = signal.NewOwner() })
	e.dispose = o.Dispose
	child := newInst(e.member, o)
	child.st, child.shared, child.split = st, true, inst
	e.child = child
	rt.stacks[st] = child
	rt.reconcile(child, want.child)
}

// present shows a sheet entry.
func (rt *router) present(inst *navInst, e *entry, d *entryDesc) {
	sr := e.member.r.Sheet
	p := SheetProps{Detents: sr.Detents, Grabber: sr.Grabber, FullScreen: sr.FullScreen}
	e.sheet = showSheet(rt.r, rt.win, inst.owner, p, true, func() { rt.dismissed(e) }, func() Node {
		return rt.content(inst, e, d)
	})
}

// dismissed is the user's dismissal of a sheet entry (or going back from
// it): it and those over it go.
func (rt *router) dismissed(e *entry) {
	if rt.applying > 0 || rt.dead {
		return
	}
	in := e.nav
	i := slices.Index(in.sheets, e)
	if i < 0 {
		return
	}
	for _, s := range slices.Backward(in.sheets[i:]) {
		s.sheet.close()
	}
	in.sheets = in.sheets[:i]
	rt.changed()
}

// chose is the user's choice of a tab or a destination.
func (rt *router) chose(inst *navInst, i int) {
	if rt.applying > 0 || rt.dead || i < 0 || i >= len(inst.node.slots) {
		return
	}
	d := snapshotNav(inst)
	d.sel = i
	d.sheets = nil
	switch inst.node.layout {
	case LayoutTabs:
		if d.entries[i] == nil {
			d.entries[i] = rt.freshEntry(inst.node.slots[i], nil, nil)
		}
	default:
		if e := inst.entries; len(e) == 0 || e[0].member != inst.node.slots[i] {
			d.entries = nil
			if e := rt.freshEntry(inst.node.slots[i], nil, nil); e != nil {
				d.entries = []*entryDesc{e}
			}
		}
	}
	rt.reconcile(inst, d)
	rt.changed()
}

// --- Navigating ---

func (rt *router) navigate(h Href, mode navMode) {
	if rt.dead {
		return
	}
	t := rt.resolve(h)
	d := rt.snapshot()
	rt.step(d, chainOf(t.leaf), &t, mode)
	rt.apply(d)
	rt.changed()
}

func (rt *router) apply(d *navDesc) {
	signal.Untrack(func() struct{} {
		rt.reconcile(rt.inst, d)
		return struct{}{}
	})
}

func (rt *router) back() bool {
	d := rt.snapshot()
	if !backDesc(d) {
		return false
	}
	rt.apply(d)
	rt.changed()
	return true
}

func (rt *router) dismiss() bool {
	d := rt.snapshot()
	var deepest *navDesc
	for n := d; n != nil; {
		if len(n.sheets) > 0 {
			deepest = n
		}
		f := n.focus()
		if f == nil {
			break
		}
		n = f.child
	}
	if deepest == nil {
		return false
	}
	deepest.sheets = deepest.sheets[:len(deepest.sheets)-1]
	rt.apply(d)
	rt.changed()
	return true
}

func (rt *router) setQuery(q url.Values) {
	d := rt.snapshot()
	n := d
	for n != nil {
		f := n.routedFocus()
		if f == nil {
			return
		}
		if f.member.leaf {
			f.query = q.Encode()
			break
		}
		n = f.child
	}
	rt.apply(d)
	rt.changed()
}

// redo is the guards keeping the user out of screens shown: those go
// (with what's over them in their stacks), the rest staying as they are
// (the other tabs' stacks), and the router goes where the focused
// screen's href resolves to now (where the guard sends the user),
// replacing it in the history.
func (rt *router) redo() {
	if rt.dead {
		return
	}
	d := rt.snapshot()
	rt.prune(d)
	t := rt.resolve(rt.lastHref)
	rt.step(d, chainOf(t.leaf), &t, modeNavigate)
	rt.apply(d)
	rt.changedAs(ops.LocationActionReplace)
}

// prune drops from d the screens a guard keeps the user out of now: in a
// stack, with those above it; a tab's, a destination's or a slot's
// whole entry.
func (rt *router) prune(d *navDesc) {
	denied := func(e *entryDesc) bool {
		return e != nil && e.member != nil && e.member.leaf && rt.denied(e.member, leafHref(e.member, e.params, e.query)) != ""
	}
	cut := func(list []*entryDesc) []*entryDesc {
		for i, e := range list {
			if denied(e) {
				return list[:i]
			}
		}
		return list
	}
	d.sheets = cut(d.sheets)
	switch d.node.layout {
	case LayoutStack:
		d.entries = cut(d.entries)
	case LayoutTabs:
		for i, e := range d.entries {
			if denied(e) {
				d.entries[i] = nil
			}
		}
	default:
		if len(d.entries) > 0 && denied(d.entries[0]) {
			d.entries = nil
		} else {
			d.entries = cut(d.entries)
		}
	}
	for _, list := range [][]*entryDesc{d.entries, d.sheets} {
		for _, e := range list {
			if e != nil && e.child != nil {
				rt.prune(e.child)
			}
		}
	}
}

// watchGuards asks the guards again when what they read changes.
func (rt *router) watchGuards() {
	if !rt.hasGuards(rt.tree) {
		return
	}
	first := true
	signal.RenderEffect(func() {
		rt.version.Get()
		denied := false
		var walk func(n *navInst)
		walk = func(n *navInst) {
			for _, list := range [][]*entry{n.entries, n.sheets} {
				for _, e := range list {
					if e == nil || e.member == nil || denied {
						continue
					}
					if e.member.leaf {
						h := e.href()
						for _, g := range e.member.guards {
							if to := g(h); to != "" && to != h {
								denied = true
							}
						}
					}
					if e.child != nil {
						walk(e.child)
					}
				}
			}
		}
		walk(rt.inst)
		if first {
			first = false
			return
		}
		if denied {
			signal.Untrack(func() struct{} { rt.redo(); return struct{}{} })
		}
	})
}

func (rt *router) hasGuards(n *rnode) bool {
	if len(n.guards) > 0 {
		return true
	}
	for _, m := range n.members {
		if rt.hasGuards(m) {
			return true
		}
	}
	return false
}

// --- The address ---

// changed tells the host of the state's new address, if it changed, with
// how the history moved.
func (rt *router) changed() { rt.changedAs(-1) }

// changedAs is changed, the history moving as action says (-1: as the
// state did).
func (rt *router) changedAs(action ops.LocationAction) {
	if rt.applying > 0 || rt.dead || rt.last == nil {
		return
	}
	now := rt.snapshot()
	href := rt.descHref(now)
	state := rt.encode(now)
	delta := historyDelta(rt.last, now)
	rt.last = now
	rt.version.Update(func(n int) int { return n + 1 })
	if href == rt.lastHref && bytes.Equal(state, rt.lastState) && delta == 0 {
		return
	}
	rt.lastHref, rt.lastState = href, state
	rt.href.Set(href)
	switch {
	case action >= 0:
		rt.send(action, 0)
	case delta > 0:
		rt.send(ops.LocationActionPush, 0)
	case delta < 0:
		rt.send(ops.LocationActionBack, int32(delta))
	default:
		rt.send(ops.LocationActionReplace, 0)
	}
}

func (rt *router) windowEl() *element { return rt.win.el() }

func (rt *router) send(action ops.LocationAction, delta int32) {
	if !rt.addressed {
		return
	}
	e := rt.windowEl()
	if e == nil {
		return
	}
	l := ops.Location{Action: action, Delta: delta, Href: string(rt.lastHref), State: rt.lastState, Window: rt.win.key()}
	e.r.batch.CommandArgs(e.id, ops.CmdLocation, func(b []byte) []byte { return ops.AppendLocation(b, l) })
}

// listen takes the user's going back and forward in a browser's history.
func (rt *router) listen() {
	if !rt.addressed {
		return
	}
	e := rt.windowEl()
	if e == nil {
		return
	}
	e.handlers().location = rt.located
	e.listen(ops.EventLocation)
}

// located is the user going back or forward in the history, to an entry
// of the router's (href, with the state it kept there, if any).
func (rt *router) located(b []byte) {
	h, state, ok := ops.DecodeLocationEvent(b)
	if !ok || rt.dead {
		return
	}
	d := rt.decode(state, Href(h))
	if d == nil {
		t := rt.resolve(Href(h))
		d = rt.fresh(rt.tree, chainOf(t.leaf), &t)
	}
	// Going back from a screen or a sheet with OnBack asks it first: when
	// it takes it, the user stays, and the history goes forward again.
	if s := rt.poppedScope(d); s != nil && len(s.fns) > 0 {
		for i := len(s.fns) - 1; i >= 0; i-- {
			if i < len(s.fns) && (*s.fns[i])() {
				rt.send(ops.LocationActionForward, 0)
				return
			}
		}
	}
	rt.apply(d)
	rt.changed()
}

// poppedScope is the back scope of the one screen or sheet that going to
// d leaves, if d is a back by one from the state shown.
func (rt *router) poppedScope(d *navDesc) *backScope {
	if historyDelta(rt.last, d) != -1 {
		return nil
	}
	o, n := rt.inst, d
	for o != nil && n != nil {
		if len(o.sheets) == len(n.sheets)+1 {
			return o.sheets[len(o.sheets)-1].sheet.back
		}
		var of *entry
		if len(o.sheets) > 0 {
			of = o.sheets[len(o.sheets)-1]
		} else {
			switch o.node.layout {
			case LayoutStack:
				if len(o.entries) == len(n.entries)+1 {
					if s := o.entries[len(o.entries)-1].screen; s != nil {
						return s.back
					}
					return nil
				}
				if len(o.entries) > 0 {
					of = o.entries[len(o.entries)-1]
				}
			case LayoutTabs:
				if o.sel < len(o.entries) {
					of = o.entries[o.sel]
				}
			default:
				if len(o.entries) > 0 {
					of = o.entries[0]
				}
			}
		}
		nf := n.focus()
		if of == nil || nf == nil || of.child == nil || nf.child == nil {
			return nil
		}
		o, n = of.child, nf.child
	}
	return nil
}

// --- The state, saved ---

// savedNav is a navigator's state as the host keeps it (JSON): entries
// by their route's key and their href, so that it reads back after the
// routes change (a reload of edited code), as far as they still match.
type savedNav struct {
	Sel     string        `json:"sel,omitempty"`
	Entries []*savedEntry `json:"e,omitempty"`
	Sheets  []*savedEntry `json:"s,omitempty"`
}

type savedEntry struct {
	Route string    `json:"r"`
	Href  string    `json:"h,omitempty"`
	Nav   *savedNav `json:"n,omitempty"`
}

func (rt *router) encode(d *navDesc) []byte {
	b, err := json.Marshal(saveNav(d))
	if err != nil {
		return nil
	}
	return b
}

func saveNav(d *navDesc) *savedNav {
	s := &savedNav{}
	if d.node.layout != LayoutStack && d.sel >= 0 && d.sel < len(d.node.slots) {
		s.Sel = d.node.slots[d.sel].key
	}
	for _, e := range d.entries {
		if se := saveEntry(e); se != nil {
			s.Entries = append(s.Entries, se)
		}
	}
	for _, e := range d.sheets {
		if se := saveEntry(e); se != nil {
			s.Sheets = append(s.Sheets, se)
		}
	}
	return s
}

func saveEntry(e *entryDesc) *savedEntry {
	if e == nil || e.member == nil {
		return nil // a Navigator's screen: it can't be made again
	}
	s := &savedEntry{Route: e.member.key}
	if e.member.leaf {
		s.Href = string(leafHref(e.member, e.params, e.query))
	} else {
		s.Nav = saveNav(e.child)
	}
	return s
}

// decode reads a saved state back, as far as the routes still match it;
// nil when it can't be (none, another app's, a guard now keeps the user
// out of a screen it has) or isn't href's (the address edited).
func (rt *router) decode(state []byte, h Href) *navDesc {
	if len(state) == 0 {
		return nil
	}
	var s savedNav
	if json.Unmarshal(state, &s) != nil {
		return nil
	}
	d := rt.loadNav(rt.tree, &s)
	if d == nil {
		return nil
	}
	if h != "" {
		got, want := rt.descHref(d), h
		gp, gq := hrefParts(got)
		wp, wq := hrefParts(want)
		if !slices.Equal(gp, wp) || !queryEqual(gq, wq) {
			return nil
		}
	}
	return d
}

func (rt *router) loadNav(n *rnode, s *savedNav) *navDesc {
	if s == nil {
		return nil
	}
	d := &navDesc{node: n}
	byKey := func(k string) *rnode {
		for _, m := range n.members {
			if m.key == k {
				return m
			}
		}
		return nil
	}
	switch n.layout {
	case LayoutStack:
		for _, se := range s.Entries {
			m := byKey(se.Route)
			if m == nil || m.sheet() {
				break
			}
			e := rt.loadEntry(m, se)
			if e == nil {
				break
			}
			d.entries = append(d.entries, e)
		}
	case LayoutTabs:
		d.sel = n.initial.slot
		if m := byKey(s.Sel); m != nil && !m.sheet() {
			d.sel = m.slot
		}
		d.entries = make([]*entryDesc, len(n.slots))
		for i, m := range n.slots {
			d.entries[i] = rt.freshEntry(m, nil, nil)
		}
		for _, se := range s.Entries {
			if m := byKey(se.Route); m != nil && !m.sheet() {
				if e := rt.loadEntry(m, se); e != nil {
					d.entries[m.slot] = e
				}
			}
		}
	default:
		m := byKey(s.Sel)
		if m == nil || m.sheet() {
			m = n.initial
		}
		if m == nil {
			break
		}
		d.sel = m.slot
		var e *entryDesc
		if len(s.Entries) > 0 && s.Entries[0].Route == m.key {
			e = rt.loadEntry(m, s.Entries[0])
		}
		if e == nil {
			e = rt.freshEntry(m, nil, nil)
		}
		if e != nil {
			d.entries = []*entryDesc{e}
		}
	}
	for _, se := range s.Sheets {
		m := byKey(se.Route)
		if m == nil || !m.sheet() {
			break
		}
		e := rt.loadEntry(m, se)
		if e == nil {
			break
		}
		d.sheets = append(d.sheets, e)
	}
	return d
}

func (rt *router) loadEntry(m *rnode, s *savedEntry) *entryDesc {
	if !m.leaf {
		c := rt.loadNav(m, s.Nav)
		if c == nil {
			return nil
		}
		return &entryDesc{member: m, child: c}
	}
	path, q := hrefParts(Href(s.Href))
	params, ok := match(m.segs, path)
	if !ok || m.r.Redirect != "" {
		return nil
	}
	if rt.denied(m, leafHref(m, params, q)) != "" {
		return nil
	}
	return &entryDesc{member: m, params: params, query: q}
}

// start is the state the router starts in: a link the app was opened
// with that it takes, else where its window was left (the host's, then
// the last run's under asuka dev), else its Initial.
func (rt *router) start() *navDesc {
	// Kept across asuka dev's reloads, whichever way it starts.
	key := "router:" + rt.win.key() + ":" + rt.tree.members[0].pattern
	saved, kept := signal.KeepValue(key, func() []byte { return rt.lastState })
	if pendingURL != "" && rt.addressed {
		if h, ok := toHref(pendingURL); ok && rt.takes(h) {
			pendingURL = ""
			return rt.build(h)
		}
	}
	if rt.addressed {
		if l, ok := rt.r.host.(host.Locator); ok {
			if wk := rt.win.key(); wk != "" {
				if h, state, ok := l.Location(wk); ok {
					if d := rt.decode(state, Href(h)); d != nil {
						return d
					}
					if h != "" {
						return rt.build(Href(h))
					}
				}
			}
		}
	}
	if kept {
		if d := rt.decode(saved, ""); d != nil {
			return d
		}
	}
	h := rt.props.Initial
	if h == "" {
		h = "/"
	}
	return rt.build(h)
}

// build is the state an href builds from nothing.
func (rt *router) build(h Href) *navDesc {
	t := rt.resolve(h)
	return rt.fresh(rt.tree, chainOf(t.leaf), &t)
}

// take navigates to a URL the app was opened with, if a route takes it.
func (rt *router) take(raw string) bool {
	h, ok := toHref(raw)
	if !ok || !rt.takes(h) {
		return false
	}
	rt.navigate(h, modeNavigate)
	return true
}

// routeURL gives a URL the app was opened with to the first Router that
// takes it, the main window's first.
func routeURL(raw string) bool {
	for _, main := range []bool{true, false} {
		for _, rt := range slices.Clone(routers) {
			if rt.win.main == main && !rt.dead && rt.take(raw) {
				return true
			}
		}
	}
	return false
}

// --- Links ---

// NavLinkProps configures a NavLink.
type NavLinkProps struct {
	Href Href
	// Push pushes the screen even when one with its address is in its
	// stack; Replace replaces the current one (RouterHandle's Push and
	// Replace). Neither: Navigate.
	Push, Replace bool
	// Label is what screen readers say (nil: its content's text).
	Label signal.Accessor[string]
	Style Styler
}

// NavLink is pressable content that goes to Href in the Router it's in:
// a link, as screen readers say. A ViewProps' or ButtonProps' Href does
// the same for a view or a button; a rich text's link (ui.Link) to an
// href of the app's ("/chat/7") goes there too.
func NavLink(p NavLinkProps, children ...Node) Node {
	rt := UseRouter()
	href, mode := p.Href, modeNavigate
	switch {
	case p.Replace:
		mode = modeReplace
	case p.Push:
		mode = modePush
	}
	return View(ViewProps{Style: p.Style, Label: p.Label, Role: RoleLink,
		OnPress: func() { routerFor(rt).navigate(href, mode) }}, children...)
}

// hrefPress is OnPress for a view or a button with an Href: press, then
// going there, in the Router it's in.
func hrefPress(press func(), href Href) func() {
	rt := UseRouter()
	return func() {
		if press != nil {
			press()
		}
		routerFor(rt).navigate(href, modeNavigate)
	}
}

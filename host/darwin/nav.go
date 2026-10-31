//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
)

// Navigation on a Mac:
//
//   - A Stack shows its top Screen, which fills it; pushes and pops
//     slide. The window's toolbar is the innermost shown stack's: a back
//     button, the top screen's bar buttons and search field, and the
//     screen's title as the window's.
//   - Tabs are an NSTabView, its pages the Screens.
//   - A Sheet is a sheet window over the app's; Escape dismisses it.
//   - An Alert is an NSAlert, shown as a sheet.
//
// Screens and sheets are sized by the host, which reports each one's
// Environment.
type nav struct {
	env      ops.Environment
	reported bool

	// Screen.
	backGesture bool
	search      string
	searchText  string
	tabItem     ID

	// Tabs.
	setting bool

	// Sheet and Alert.
	window ID
	alert  ID
	order  []int // an alert's buttons' indexes, in the alert's order
}

func (h *Host) createNav(n *node) bool {
	switch n.kind {
	case ops.KindStack:
		n.view = initView("NuiView")
		h.layer(n)
		Send(Send(n.view, "layer"), "setMasksToBounds:", 1)
	case ops.KindScreen:
		n.view = initView("NuiView")
		n.nav = &nav{backGesture: true}
		return true
	case ops.KindTabs:
		v := initView("NSTabView")
		Send(v, "setDelegate:", uintptr(h.target))
		Send(v, "setTabViewType:", 0) // top tabs, bezeled
		n.view = v
	case ops.KindSheet:
		n.view = initView("NuiView")
	case ops.KindAlert:
	default:
		return false
	}
	n.nav = &nav{}
	if n.view != 0 {
		Send(n.view, "setAutoresizingMask:", 0)
	}
	return true
}

// sizedByHost reports whether the host sizes n (a screen, a sheet's
// content), not the core.
func (nv *nav) sizedByHost(n *node) bool {
	return n.kind == ops.KindScreen || n.kind == ops.KindSheet
}

func (h *Host) insertNav(p, c *node, index int) bool {
	switch {
	case p.kind == ops.KindStack && c.kind == ops.KindScreen:
		i := min(index, len(p.children))
		top := h.topScreen(p)
		p.children = append(p.children, nil)
		copy(p.children[i+1:], p.children[i:])
		p.children[i] = c
		c.parent = p
		b := MsgRect(p.view, Sel("bounds"))
		MsgSetRect(c.view, selSetFrame, b)
		if c.background == 0 {
			h.withAppearance(func() {
				Send(h.layer(c), "setBackgroundColor:", cgColor(Send(Class("NSColor"), "windowBackgroundColor")))
			})
		}
		if i == len(p.children)-1 {
			Send(p.view, "addSubview:", uintptr(c.view))
			if top != nil {
				h.slide(p, c, top, true)
			}
		} else {
			// Below the top: shown when the screens above it are popped.
			Send(c.view, "setHidden:", 1)
			Send(p.view, "addSubview:positioned:relativeTo:", uintptr(c.view), nsWindowBelow, uintptr(p.children[i+1].view))
		}
		h.reportScreen(c, b.Size)
		h.updateWindowChrome()
		return true
	case p.kind == ops.KindTabs && c.kind == ops.KindScreen:
		i := min(index, len(p.children))
		p.children = append(p.children, nil)
		copy(p.children[i+1:], p.children[i:])
		p.children[i] = c
		c.parent = p
		it := Send(Send(Class("NSTabViewItem"), "alloc"), "initWithIdentifier:", uintptr(NumberInt(int(c.id))))
		Send(it, "setLabel:", uintptr(String(c.title)))
		Send(it, "setView:", uintptr(c.view))
		c.nav.tabItem = it
		p.nav.setting = true
		Send(p.view, "insertTabViewItem:atIndex:", uintptr(it), uintptr(i))
		if p.selected < len(p.children) {
			Send(p.view, "selectTabViewItemAtIndex:", uintptr(p.selected))
		}
		p.nav.setting = false
		h.layoutScreens(p)
		h.updateWindowChrome()
		return true
	}
	return false
}

func (h *Host) removeNav(p, c *node) bool {
	if p.kind == ops.KindTabs && c.nav != nil && c.nav.tabItem != 0 {
		p.nav.setting = true
		Send(p.view, "removeTabViewItem:", uintptr(c.nav.tabItem))
		p.nav.setting = false
		Release(c.nav.tabItem)
		c.nav.tabItem = 0
		h.updateWindowChrome()
		return true
	}
	if p.kind == ops.KindStack {
		Send(c.view, "removeFromSuperview")
		h.updateWindowChrome()
		return true
	}
	return false
}

// destroyNav destroys a screen, sheet or alert; false: an ordinary
// destroy.
func (h *Host) destroyNav(n *node) bool {
	if n.nav == nil {
		return false
	}
	switch n.kind {
	case ops.KindScreen:
		p := n.parent
		if p != nil && p.kind == ops.KindStack {
			wasTop := h.topScreen(p) == n
			for i, x := range p.children {
				if x == n {
					p.children = append(p.children[:i], p.children[i+1:]...)
					break
				}
			}
			n.parent = nil
			below := h.topScreen(p)
			if wasTop && below != nil {
				Send(below.view, "setHidden:", 0)
			}
			if wasTop && !n.dying && below != nil && Send(p.view, "window") != 0 {
				// Popped by the app: it slides out, then goes.
				h.slide(p, below, n, false)
			} else {
				h.release(n)
			}
			for _, c := range n.children {
				c.parent = nil
			}
			h.updateWindowChrome()
			return true
		}
		if p != nil {
			h.remove(p, n)
		}
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		return true
	case ops.KindSheet:
		n.dying = true
		if n.nav.window != 0 {
			w := n.nav.window
			Send(h.window, "endSheet:", uintptr(w))
			Send(w, "orderOut:", 0)
			delete(h.byView, Send(w, "contentView"))
			Release(w)
			n.nav.window = 0
		}
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		return true
	case ops.KindAlert:
		n.dying = true
		if a := n.nav.alert; a != 0 {
			if w := Send(a, "window"); SendBool(w, "isVisible") {
				Send(h.window, "endSheet:", uintptr(w))
			}
			Release(a)
			n.nav.alert = 0
		}
		return true
	}
	// A stack or tabs: an ordinary view.
	defer h.afterCore(h.updateWindowChrome)
	return false
}

func (h *Host) topScreen(stack *node) *node {
	for i := len(stack.children) - 1; i >= 0; i-- {
		if c := stack.children[i]; !c.dying {
			return c
		}
	}
	return nil
}

// slide animates a push (in comes from the right over out, which moves
// a little left) or a pop (out leaves to the right), as macOS navigates.
func (h *Host) slide(stack, in, out *node, push bool) {
	b := MsgRect(stack.view, Sel("bounds"))
	w := b.Size.Width
	if Send(stack.view, "window") == 0 || h.env.ReduceMotion || w <= 0 {
		Send(out.view, "setHidden:", Bool(push))
		if !push {
			h.release(out)
		}
		return
	}
	at := func(x float64) Rect { return Rect{Origin: Point{X: x, Y: 0}, Size: b.Size} }
	if push {
		MsgSetRect(in.view, selSetFrame, at(w))
	} else {
		MsgSetRect(in.view, selSetFrame, at(-w/3))
		Send(in.view, "setHidden:", 0)
		Send(stack.view, "addSubview:positioned:relativeTo:", uintptr(out.view), nsWindowAbove, uintptr(in.view))
	}
	inView, outView := in.view, out.view
	anim := NewBlock(func(_ objc.Block, ctx ID) {
		MsgSetFloat(ctx, Sel("setDuration:"), 0.3)
		Send(ctx, "setAllowsImplicitAnimation:", 1)
		Send(ctx, "setTimingFunction:", uintptr(Send(Class("CAMediaTimingFunction"), "functionWithName:", uintptr(String("easeInEaseOut")))))
		MsgSetRect(Send(inView, "animator"), selSetFrame, at(0))
		if push {
			MsgSetRect(Send(outView, "animator"), selSetFrame, at(-w/3))
		} else {
			MsgSetRect(Send(outView, "animator"), selSetFrame, at(w))
		}
	})
	Retain(outView)
	done := NewBlock(func(_ objc.Block) {
		if push {
			if n := h.byView[outView]; n != nil && h.topScreen(stack) != n {
				Send(outView, "setHidden:", 1)
				MsgSetRect(outView, selSetFrame, at(0))
			}
		} else if h.byView[outView] == out {
			h.release(out)
		}
		Release(outView)
	})
	Send(Class("NSAnimationContext"), "runAnimationGroup:completionHandler:", uintptr(anim), uintptr(done))
}

// layoutScreens sizes a stack's screens, or a tabs' pages, to it and
// reports their environments.
func (h *Host) layoutScreens(n *node) {
	switch n.kind {
	case ops.KindStack:
		b := MsgRect(n.view, Sel("bounds"))
		for _, c := range n.children {
			MsgSetRect(c.view, selSetFrame, b)
			h.reportScreen(c, b.Size)
		}
	case ops.KindTabs:
		r := MsgRect(n.view, Sel("contentRect"))
		for _, c := range n.children {
			h.reportScreen(c, r.Size)
		}
	}
}

// layoutNav sizes every host-sized view again (the window resized).
func (h *Host) layoutNav() {
	for _, n := range h.nodes {
		switch n.kind {
		case ops.KindStack, ops.KindTabs:
			h.layoutScreens(n)
		case ops.KindSheet:
			if n.nav.window != 0 {
				h.reportScreen(n, MsgRect(Send(n.nav.window, "contentView"), Sel("bounds")).Size)
			}
		}
	}
}

// reportScreen sends a screen's or sheet's environment if it changed.
func (h *Host) reportScreen(n *node, s Size) {
	if n.nav == nil || s.Width <= 0 {
		return
	}
	env := h.env
	env.Width, env.Height = float32(s.Width), float32(s.Height)
	env.SafeTop, env.SafeRight, env.SafeBottom, env.SafeLeft = 0, 0, 0, 0
	if n.nav.reported && env == n.nav.env {
		return
	}
	n.nav.env, n.nav.reported = env, true
	h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: env})
}

func (h *Host) setNavCommon(n *node, p ops.PropID, v *ops.Value) {}

func (h *Host) setNavProp(n *node, p ops.PropID, v *ops.Value) {
	switch n.kind {
	case ops.KindScreen:
		switch p {
		case ops.PropScreenTitle:
			n.title = v.Str
			if n.nav.tabItem != 0 {
				Send(n.nav.tabItem, "setLabel:", uintptr(String(v.Str)))
			}
		case ops.PropScreenActions:
			n.actions = decodeActions(v.Bytes)
		case ops.PropScreenBackGesture:
			n.nav.backGesture = v.Int != 0
		case ops.PropScreenIcon:
			n.icon = v.Str
			if n.nav.tabItem != 0 && RespondsTo(n.nav.tabItem, "setImage:") {
				Send(n.nav.tabItem, "setImage:", uintptr(symbolImage(v.Str)))
			}
		case ops.PropScreenSearch:
			n.nav.search = v.Str
		case ops.PropScreenSearchText:
			n.nav.searchText = v.Str
			if f := h.chrome.searchField; f != 0 && h.chrome.screen == n {
				Send(f, "setStringValue:", uintptr(String(v.Str)))
			}
			return
		}
		h.updateWindowChrome()
	case ops.KindTabs:
		if p == ops.PropTabsSelected {
			n.selected = int(v.Int)
			if n.selected < len(n.children) {
				n.nav.setting = true
				Send(n.view, "selectTabViewItemAtIndex:", uintptr(n.selected))
				n.nav.setting = false
			}
			h.updateWindowChrome()
		}
	case ops.KindSheet:
		if p == ops.PropSheetDetents {
			n.detents = v.Int
		}
	case ops.KindAlert:
		switch p {
		case ops.PropAlertTitle:
			n.title = v.Str
		case ops.PropAlertMessage:
			n.message = v.Str
		case ops.PropAlertButtons:
			n.actions = decodeActions(v.Bytes)
		case ops.PropAlertStyle:
			n.alertStyle = int32(v.Int)
		}
	}
}

func (h *Host) tabSelected(tv, item ID) {
	n := h.byView[tv]
	if n == nil || n.nav == nil || n.nav.setting {
		return
	}
	i := SendInt(tv, "indexOfTabViewItem:", uintptr(item))
	n.selected = i
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
	h.layoutScreens(n)
	h.updateWindowChrome()
}

// present shows a sheet or an alert.
func (h *Host) present(n *node) {
	switch n.kind {
	case ops.KindSheet:
		if n.nav.window != 0 {
			return
		}
		cs := MsgRect(h.content, Sel("bounds")).Size
		size := Size{Width: min(max(cs.Width-80, 320), 640), Height: max(cs.Height-100, 240)}
		if n.detents == 1 { // medium only
			size.Height = max(cs.Height/2, 240)
		}
		w := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
			Rect{Size: size}, styleTitled|styleResizable, backingBuffered, false)
		Send(w, "setReleasedWhenClosed:", 0)
		content := initView("NuiView")
		Send(content, "setAutoresizesSubviews:", 0)
		Send(w, "setContentView:", uintptr(content))
		Release(content)
		Send(w, "setDelegate:", uintptr(h.target))
		MsgSetRect(n.view, selSetFrame, Rect{Size: size})
		Send(content, "addSubview:", uintptr(n.view))
		h.byView[content] = n
		n.nav.window = w
		n.presented = true
		h.reportScreen(n, size)
		Send(h.window, "beginSheet:completionHandler:", uintptr(w), 0)
	case ops.KindAlert:
		a := New("NSAlert")
		Send(a, "setMessageText:", uintptr(String(n.title)))
		Send(a, "setInformativeText:", uintptr(String(n.message)))
		// The default button is the first, rightmost; cancel goes last.
		var order []int
		for i, b := range n.actions {
			if b.role != ops.AlertButtonRoleCancel {
				order = append(order, i)
			}
		}
		for i, b := range n.actions {
			if b.role == ops.AlertButtonRoleCancel {
				order = append(order, i)
			}
		}
		for _, i := range order {
			b := n.actions[i]
			btn := Send(a, "addButtonWithTitle:", uintptr(String(b.title)))
			switch b.role {
			case ops.AlertButtonRoleCancel:
				Send(btn, "setKeyEquivalent:", uintptr(String("\x1b")))
			case ops.AlertButtonRoleDestructive:
				if RespondsTo(btn, "setHasDestructiveAction:") {
					Send(btn, "setHasDestructiveAction:", 1)
				}
			}
		}
		if len(order) == 0 {
			Send(a, "addButtonWithTitle:", uintptr(String("OK")))
		}
		n.nav.alert = a
		n.nav.order = order
		id := n.id
		done := NewBlock(func(_ objc.Block, resp int) {
			n := h.nodes[id]
			if n == nil || n.dying {
				return
			}
			i := resp - 1000 // NSAlertFirstButtonReturn
			if i >= 0 && i < len(n.nav.order) {
				h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(n.nav.order[i])})
			} else {
				h.emit(n, ops.Event{Kind: ops.EventDismissed})
			}
		})
		Send(a, "beginSheetModalForWindow:completionHandler:", uintptr(h.window), uintptr(done))
	}
}

// cancel is Escape: a sheet's user dismissal.
func (h *Host) cancel(view ID) {
	w := Send(view, "window")
	for _, n := range h.nodes {
		if n.kind == ops.KindSheet && n.nav.window == w && !n.dying {
			h.emit(n, ops.Event{Kind: ops.EventDismissed})
			return
		}
	}
}

// chrome is what the window's title and toolbar show: the innermost
// shown stack's top screen.
type chrome struct {
	stack, screen *node
	back          bool
	actions       []action
	search        string
	searchField   ID
	ids           []string
	toolbar       ID
}

// activeStack finds the innermost stack on screen: through each stack's
// top screen and each tabs' selected page.
func (h *Host) activeStack() *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		switch n.kind {
		case ops.KindStack:
			found = n
			if t := h.topScreen(n); t != nil {
				walk(t)
			}
			return
		case ops.KindTabs:
			if n.selected < len(n.children) {
				walk(n.children[n.selected])
			}
			return
		case ops.KindSheet, ops.KindAlert:
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if h.root != nil {
		walk(h.root)
	}
	return found
}

func (h *Host) updateWindowChrome() {
	if h.window == 0 {
		return
	}
	if h.depth > 0 {
		if !h.chromeQueued {
			h.chromeQueued = true
			h.afterCore(func() {
				h.chromeQueued = false
				WithPool(h.updateWindowChromeNow)
			})
		}
		return
	}
	WithPool(h.updateWindowChromeNow)
}

func (h *Host) updateWindowChromeNow() {
	c := &h.chrome
	stack := h.activeStack()
	var screen *node
	if stack != nil {
		screen = h.topScreen(stack)
	}
	title := h.appName
	if screen != nil && screen.title != "" {
		title = screen.title
	} else if t := h.selectedTab(); t != nil && t.title != "" {
		title = t.title
	}
	Send(h.window, "setTitle:", uintptr(String(title)))
	c.stack, c.screen = stack, screen
	var ids []string
	if stack != nil {
		live := 0
		for _, s := range stack.children {
			if !s.dying {
				live++
			}
		}
		c.back = live > 1
		if c.back {
			ids = append(ids, "nui.back")
		}
		ids = append(ids, "NSToolbarFlexibleSpaceItem")
		c.actions, c.search = nil, ""
		if screen != nil {
			c.actions, c.search = screen.actions, screen.nav.search
		}
		if c.search != "" {
			ids = append(ids, "nui.search")
		}
		for i := range c.actions {
			ids = append(ids, "nui.action."+itoa(i))
		}
	}
	if stack == nil {
		if c.toolbar != 0 {
			Send(h.window, "setToolbar:", 0)
			Release(c.toolbar)
			c.toolbar = 0
			h.environmentChanged()
		}
		return
	}
	if c.toolbar == 0 {
		t := Send(Send(Class("NSToolbar"), "alloc"), "initWithIdentifier:", uintptr(String("nui")))
		Send(t, "setDelegate:", uintptr(h.target))
		Send(t, "setDisplayMode:", 2) // icons
		Send(t, "setAllowsUserCustomization:", 0)
		c.toolbar = t
		if RespondsTo(h.window, "setToolbarStyle:") {
			Send(h.window, "setToolbarStyle:", 3) // unified: the title beside the back button
		}
		Send(h.window, "setToolbar:", uintptr(t))
		h.environmentChanged()
	}
	// The items again: they're the screen's.
	t := c.toolbar
	for SendInt(Send(t, "items"), "count") > 0 {
		Send(t, "removeItemAtIndex:", 0)
	}
	c.searchField = 0
	c.ids = ids
	for i, id := range ids {
		Send(t, "insertItemWithItemIdentifier:atIndex:", uintptr(String(id)), uintptr(i))
	}
}

func (h *Host) selectedTab() *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if n.kind == ops.KindTabs && n.selected < len(n.children) {
			found = n.children[n.selected]
			walk(found)
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if h.root != nil {
		walk(h.root)
	}
	return found
}

// toolbarItem makes the item for an identifier of the chrome's.
func (h *Host) toolbarItem(ident string) ID {
	c := &h.chrome
	item := func() ID {
		it := Send(Send(Class("NSToolbarItem"), "alloc"), "initWithItemIdentifier:", uintptr(String(ident)))
		return Autorelease(it)
	}
	switch {
	case ident == "nui.back":
		it := item()
		Send(it, "setImage:", uintptr(symbolImage("chevron.left")))
		Send(it, "setLabel:", uintptr(String("Back")))
		Send(it, "setToolTip:", uintptr(String("Back")))
		Send(it, "setTarget:", uintptr(h.target))
		Send(it, "setAction:", uintptr(Sel("back:")))
		if RespondsTo(it, "setNavigational:") {
			Send(it, "setNavigational:", 1)
		}
		if RespondsTo(it, "setBordered:") {
			Send(it, "setBordered:", 1)
		}
		return it
	case ident == "nui.search":
		var it ID
		if HasClass("NSSearchToolbarItem") {
			it = Autorelease(Send(Send(Class("NSSearchToolbarItem"), "alloc"), "initWithItemIdentifier:", uintptr(String(ident))))
			f := Send(it, "searchField")
			c.searchField = f
		} else {
			it = item()
			f := initView("NSSearchField")
			MsgSetRect(f, selSetFrame, Rect{Size: Size{Width: 200, Height: 22}})
			Send(it, "setView:", uintptr(f))
			Release(f)
			c.searchField = f
		}
		f := c.searchField
		Send(f, "setDelegate:", uintptr(h.target))
		Send(f, "setPlaceholderString:", uintptr(String(c.search)))
		if c.screen != nil {
			Send(f, "setStringValue:", uintptr(String(c.screen.nav.searchText)))
		}
		return it
	case len(ident) > len("nui.action.") && ident[:len("nui.action.")] == "nui.action.":
		i := atoi(ident[len("nui.action."):])
		if i < 0 || i >= len(c.actions) {
			return 0
		}
		a := c.actions[i]
		it := item()
		Send(it, "setLabel:", uintptr(String(a.title)))
		Send(it, "setToolTip:", uintptr(String(a.title)))
		if img := symbolImage(a.icon); img != 0 {
			Send(it, "setImage:", uintptr(img))
		} else if RespondsTo(it, "setTitle:") {
			Send(it, "setTitle:", uintptr(String(a.title)))
		}
		if RespondsTo(it, "setBordered:") {
			Send(it, "setBordered:", 1)
		}
		Send(it, "setTag:", uintptr(i))
		Send(it, "setTarget:", uintptr(h.target))
		Send(it, "setAction:", uintptr(Sel("barItem:")))
		return it
	}
	return 0
}

// barItem is a screen's bar button.
func (h *Host) barItem(it ID) {
	s := h.chrome.screen
	if s == nil || !RespondsTo(it, "tag") {
		return
	}
	h.emit(s, ops.Event{Kind: ops.EventAction, Int: int64(SendInt(it, "tag"))})
}

// back pops the innermost shown stack's top screen, as its back button.
func (h *Host) back() {
	stack := h.activeStack()
	if stack == nil {
		return
	}
	top := h.topScreen(stack)
	live := 0
	for _, s := range stack.children {
		if !s.dying {
			live++
		}
	}
	if top == nil || live < 2 {
		return
	}
	top.dying = true
	below := h.topScreen(stack)
	if below != nil {
		h.slide(stack, below, top, false)
		// The core destroys it; its views go once the slide is done.
		h.byView[top.view] = top
	}
	h.updateWindowChrome()
	h.emit(stack, ops.Event{Kind: ops.EventPopped, Int: int64(top.id)})
}

func (h *Host) searchChanged(field ID) {
	c := &h.chrome
	if field != c.searchField || c.screen == nil {
		return
	}
	s := GoString(Send(field, "stringValue"))
	c.screen.nav.searchText = s
	h.emit(c.screen, ops.Event{Kind: ops.EventTextChanged, Str: s})
}

func (h *Host) searchCommand(control ID, cmd SEL) bool {
	c := &h.chrome
	if control != c.searchField || c.screen == nil {
		return false
	}
	if cmd == Sel("insertNewline:") {
		h.emit(c.screen, ops.Event{Kind: ops.EventSubmit})
		return true
	}
	return false
}

// navMethods are NuiTarget's toolbar delegate methods and Escape.
func navMethods() []objc.MethodDef {
	return []objc.MethodDef{
		Method("toolbar:itemForItemIdentifier:willBeInsertedIntoToolbar:", func(self ID, _ SEL, t, ident ID, insert bool) ID {
			return theHost.toolbarItem(GoString(ident))
		}),
		Method("toolbarDefaultItemIdentifiers:", func(self ID, _ SEL, t ID) ID { return theHost.toolbarIDs() }),
		Method("toolbarAllowedItemIdentifiers:", func(self ID, _ SEL, t ID) ID { return theHost.toolbarIDs() }),
		Method("cancelOperation:", func(self ID, _ SEL, sender ID) {
			if w := Send(Send(Class("NSApplication"), "sharedApplication"), "keyWindow"); w != 0 {
				theHost.cancel(Send(w, "contentView"))
			}
		}),
		Method("windowShouldClose:", func(self ID, _ SEL, w ID) bool {
			if w == theHost.window {
				return true
			}
			theHost.cancel(Send(w, "contentView"))
			return false
		}),
	}
}

func (h *Host) toolbarIDs() ID {
	arr := Send(Class("NSMutableArray"), "array")
	for _, id := range h.chrome.ids {
		Send(arr, "addObject:", uintptr(String(id)))
	}
	return arr
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

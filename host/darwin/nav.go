//go:build darwin && !ios

package darwin

import (
	"slices"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

// Navigation on a Mac:
//
//   - A Stack shows its top Screen, which fills it; pushes and pops
//     slide. The window's toolbar is the innermost shown stack's: a back
//     button, the top screen's bar buttons and search field, and the
//     screen's title as the window's.
//   - Tabs are an NSTabView, its pages the Screens.
//   - A Sheet is a sheet window over the app's; Escape dismisses it. A
//     FullScreen one is as big as all of its window's content under the
//     title bar and toolbar, and follows the window as it resizes: a
//     Mac has no full-screen modal of its own, and a sheet is how AppKit
//     takes a window for a task; so does Escape.
//   - An Alert is an NSAlert, shown as a sheet.
//
// Screens and sheets are sized by the host, which reports each one's
// Environment.
type nav struct {
	env      ops.Environment
	reported bool

	// Stack: Replace's, until its next insert: changes happen at once.
	instant bool

	// Screen.
	backGesture bool
	search      string
	searchText  string
	tabItem     ID
	// Its Toolbar's and Primary's items, numbered as their Action events
	// are (ops.ScreenToolbarActions, ops.ScreenPrimaryAction + the item's
	// number): the window's toolbar shows them after its bar buttons.
	toolbar, primary []action
	badge            string // a tab's

	// Tabs.
	setting  bool
	reversed bool // its items are in the reverse order of its children

	// Sheet and Alert; on, the node of the window it's declared in (its
	// Window prop: the root, a Window node), which it goes on.
	window   ID
	on       ops.NodeID
	deferred bool // presenting waits for its window to open
	full     bool // a Sheet's FullScreen: as big as its window's content
	alert    ID
	order    []int // an alert's buttons' indexes, in the alert's order
	fields   []ops.AlertField
	inputs   []ID // its fields' NSTextFields, while it shows
	defBtn   ID   // its default button, which return in its last field presses

	// Window: its options (Config), a flyout's anchor (Flyout), and the
	// document it shows: edited, its file.
	cfg    ops.WindowConfig
	flyout ops.Flyout
	edited bool
	file   string
}

func (h *Host) createNav(n *node) bool {
	switch n.kind {
	case ops.KindStack:
		n.view = initView("AsukaView")
		h.layer(n)
		Send(Send(n.view, "layer"), "setMasksToBounds:", 1)
	case ops.KindScreen:
		n.view = initView("AsukaView")
		n.nav = &nav{backGesture: true}
		return true
	case ops.KindTabs:
		v := initView("NSTabView")
		Send(v, "setDelegate:", uintptr(h.target))
		Send(v, "setTabViewType:", 0) // top tabs, bezeled
		n.view = v
	case ops.KindSheet, ops.KindWindow:
		n.view = initView("AsukaView")
	case ops.KindPopover:
		n.view = initView("AsukaView")
		n.popover = &popover{}
	case ops.KindAlert:
	case ops.KindSnackbar:
		n.snack = &snack{}
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
	return n.kind == ops.KindScreen || n.kind == ops.KindSheet || n.kind == ops.KindWindow || n.kind == ops.KindPopover
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
			if top != nil && !p.nav.instant {
				h.slide(p, c, top, true)
			}
			p.nav.instant = false
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
		it := Send(Send(Class("AsukaTabViewItem"), "alloc"), "initWithIdentifier:", uintptr(NumberInt(int(c.id))))
		Send(it, "setLabel:", uintptr(String(tabLabel(c))))
		Send(it, "setView:", uintptr(c.view))
		c.nav.tabItem = it
		p.nav.setting = true
		Send(p.view, "insertTabViewItem:atIndex:", uintptr(it), uintptr(h.tabIndex(p, i, len(p.children))))
		h.selectTab(p)
		p.nav.setting = false
		h.layoutScreens(p)
		h.updateWindowChrome()
		return true
	}
	return false
}

// tabLabel is a tab's label: its title, and its badge after it in
// parentheses, as Mac apps count things in a tab's or a window's title
// ("Inbox (3)"). NSTabView draws its tabs itself, its items' labels
// alone (it never calls an item's drawLabel:inRect:), so a badge can't
// be drawn beside it.
func tabLabel(n *node) string {
	if n.nav == nil || n.nav.badge == "" {
		return n.title
	}
	return n.title + " (" + n.nav.badge + ")"
}

// registerTabClasses defines AsukaTabViewItem, a tab whose title for
// VoiceOver says its badge after its title, a count as so many items
// ("Inbox, 3 items"), rather than its label's parentheses.
func registerTabClasses() {
	DefineClass("AsukaTabViewItem", "NSTabViewItem", nil, []objc.MethodDef{
		Method("accessibilityAttributeValue:", func(self ID, _ SEL, attr ID) ID {
			if GoString(attr) == "AXTitle" {
				id := ops.NodeID(SendInt(Send(self, "identifier"), "integerValue"))
				if n := theHost.nodes[id]; n != nil && n.nav != nil && n.nav.badge != "" {
					return String(l10n.Badged(theHost.locale, n.title, n.nav.badge))
				}
			}
			return SendSuper(self, "AsukaTabViewItem", "accessibilityAttributeValue:", uintptr(attr))
		}),
	})
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
			if wasTop && !n.dying && below != nil && !p.nav.instant && Send(p.view, "window") != 0 {
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
			if p := Send(w, "sheetParent"); p != 0 {
				Send(p, "endSheet:", uintptr(w))
			}
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
	case ops.KindPopover:
		n.dying = true
		h.closePopover(n)
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		return true
	case ops.KindWindow:
		n.dying = true
		if w := n.nav.window; w != 0 {
			delete(h.wins, w)
			Send(w, "setDelegate:", 0)
			if p := Send(w, "sheetParent"); p != 0 {
				Send(p, "endSheet:", uintptr(w))
			}
			p := Send(w, "parentWindow")
			if p != 0 {
				Send(p, "removeChildWindow:", uintptr(w))
			}
			key := SendBool(w, "isKeyWindow")
			Send(w, "orderOut:", 0)
			Send(w, "close")
			Release(w)
			n.nav.window = 0
			if key && p != 0 {
				h.regainKey(p)
			}
		}
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		return true
	case ops.KindAlert:
		n.dying = true
		h.forgetAlertFields(n)
		if a := n.nav.alert; a != 0 {
			if w := Send(a, "window"); SendBool(w, "isVisible") {
				if p := Send(w, "sheetParent"); p != 0 {
					Send(p, "endSheet:", uintptr(w))
				}
			}
			Release(a)
			n.nav.alert = 0
		}
		return true
	case ops.KindSnackbar:
		n.dying = true
		h.destroySnack(n)
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
// a little left) or a pop (out leaves to the right), as macOS navigates;
// the other way round where the stack reads right to left.
func (h *Host) slide(stack, in, out *node, push bool) {
	b := MsgRect(stack.view, Sel("bounds"))
	w := b.Size.Width
	away := w // where a screen coming on a push starts, and one going on a pop ends
	if h.isRTL(stack) {
		away = -w
	}
	if Send(stack.view, "window") == 0 || h.env.ReduceMotion || w <= 0 {
		Send(out.view, "setHidden:", Bool(push))
		if !push {
			h.release(out)
		}
		return
	}
	at := func(x float64) Rect { return Rect{Origin: Point{X: x, Y: 0}, Size: b.Size} }
	if push {
		MsgSetRect(in.view, selSetFrame, at(away))
	} else {
		MsgSetRect(in.view, selSetFrame, at(-away/3))
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
			MsgSetRect(Send(outView, "animator"), selSetFrame, at(-away/3))
		} else {
			MsgSetRect(Send(outView, "animator"), selSetFrame, at(away))
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
		case ops.KindSheet, ops.KindWindow:
			if n.nav.window != 0 {
				if n.nav.full {
					h.fitCover(n)
				}
				b := MsgRect(Send(n.nav.window, "contentView"), Sel("bounds"))
				MsgSetRect(n.view, selSetFrame, b)
				h.reportScreen(n, b.Size)
			}
		}
	}
	h.placeSnacks()
}

// reportScreen sends a screen's or sheet's environment if it changed.
func (h *Host) reportScreen(n *node, s Size) {
	if n.nav == nil || s.Width <= 0 {
		return
	}
	env := h.env
	env.Width, env.Height = float32(s.Width), float32(s.Height)
	env.SafeTop, env.SafeRight, env.SafeBottom, env.SafeLeft = 0, 0, 0, 0
	if n.kind == ops.KindWindow {
		env.SafeTop = h.wins[n.nav.window].safeTop()
	}
	// Its window's display's scale: a Window on another display than the
	// main window's has its own.
	if w := Send(n.view, "window"); w != 0 {
		if k := MsgFloat(w, Sel("backingScaleFactor")); k > 0 {
			env.Scale = float32(k)
		}
	}
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
				Send(n.nav.tabItem, "setLabel:", uintptr(String(tabLabel(n))))
			}
		case ops.PropScreenBadge:
			// The tab's label alone changes.
			n.nav.badge = v.Str
			if n.nav.tabItem != 0 {
				Send(n.nav.tabItem, "setLabel:", uintptr(String(tabLabel(n))))
			}
			return
		case ops.PropScreenActions:
			n.actions = decodeActions(v.Bytes)
		case ops.PropScreenToolbar:
			n.nav.toolbar = decodeActionsFrom(v.Bytes, ops.ScreenToolbarActions)
		case ops.PropScreenPrimary:
			n.nav.primary = decodeActionsFrom(v.Bytes, ops.ScreenPrimaryAction)
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
			for _, w := range h.allWins() {
				if f := w.chrome.searchField; f != 0 && w.chrome.screen == n {
					Send(f, "setStringValue:", uintptr(String(v.Str)))
				}
			}
			return
		}
		h.updateWindowChrome()
	case ops.KindTabs:
		if p == ops.PropTabsSelected {
			n.selected = int(v.Int)
			n.nav.setting = true
			h.selectTab(n)
			n.nav.setting = false
			h.updateWindowChrome()
		}
	case ops.KindSheet:
		switch p {
		case ops.PropSheetDetents:
			n.detents = v.Int
		case ops.PropSheetWindow:
			n.nav.on = ops.NodeID(v.Int)
		case ops.PropSheetFullScreen:
			n.nav.full = v.Int != 0
		}
	case ops.KindPopover:
		h.setPopoverProp(n, p, v)
	case ops.KindWindow:
		switch p {
		case ops.PropWindowTitle:
			n.title = v.Str
			if w := h.wins[n.nav.window]; w != nil {
				w.base = v.Str
				h.updateWindowChrome()
			}
		case ops.PropWindowConfig:
			n.nav.cfg = ops.DecodeWindowConfig(v.Bytes)
		case ops.PropWindowFlyout:
			n.nav.flyout, _ = ops.DecodeFlyout(v.Bytes)
			if x := h.wins[n.nav.window]; x != nil {
				x.flyout = n.nav.flyout
				if SendBool(x.window, "isVisible") {
					h.placeFlyout(x)
				}
			}
		case ops.PropWindowEdited:
			n.nav.edited = v.Int != 0
			h.applyDocument(n)
		case ops.PropWindowFile:
			n.nav.file = v.Str
			h.applyDocument(n)
		}
	case ops.KindAlert:
		switch p {
		case ops.PropAlertTitle:
			n.title = v.Str
		case ops.PropAlertMessage:
			n.message = v.Str
		case ops.PropAlertButtons:
			n.actions = decodeActions(v.Bytes)
			h.updateAlertButtons(n)
		case ops.PropAlertStyle:
			n.alertStyle = int32(v.Int)
		case ops.PropAlertWindow:
			n.nav.on = ops.NodeID(v.Int)
		case ops.PropAlertFields:
			n.nav.fields = ops.DecodeAlertFields(v.Bytes)
		}
	case ops.KindSnackbar:
		h.setSnackProp(n, p, v)
	}
}

func (h *Host) tabSelected(tv, item ID) {
	n := h.byView[tv]
	if n == nil || n.nav == nil || n.nav.setting {
		return
	}
	i := -1
	for k, c := range n.children {
		if c.nav != nil && c.nav.tabItem == item {
			i = k
		}
	}
	if i < 0 {
		return
	}
	n.selected = i
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
	h.layoutScreens(n)
	h.updateWindowChrome()
}

// onNode is the node of the window a sheet or an alert is declared in
// (its Window prop): the root, or a Window node; the root without one.
func (h *Host) onNode(n *node) *node {
	if x := h.nodes[n.nav.on]; x != nil && !x.dying && (x.kind == ops.KindWindow || h.main != nil && x == h.main.root) {
		return x
	}
	if h.main != nil {
		return h.main.root
	}
	return nil
}

// onWin is the window a sheet or an alert goes on: the one it's
// declared in, whichever is in use; nil while it isn't open yet (a
// Window opening in the same batch, after it).
func (h *Host) onWin(n *node) *win {
	x := h.onNode(n)
	if x == nil {
		return h.main
	}
	if x.kind == ops.KindWindow {
		return h.wins[x.nav.window]
	}
	return h.main
}

// present shows a sheet or an alert, on the window it's declared in;
// once the batch is applied if that window isn't open yet.
func (h *Host) present(n *node) {
	if n.kind == ops.KindSheet || n.kind == ops.KindAlert || n.kind == ops.KindSnackbar {
		if w := h.onWin(n); w == nil || w.window == 0 {
			if n.nav.deferred {
				return // its window never opened
			}
			n.nav.deferred = true
			h.afterCore(func() {
				if h.nodes[n.id] == n && !n.dying {
					h.present(n)
				}
			})
			return
		}
	}
	switch n.kind {
	case ops.KindSheet:
		if n.nav.window != 0 {
			return
		}
		over := h.onWin(n)
		cs := MsgRect(over.content, Sel("bounds")).Size
		size := Size{Width: min(max(cs.Width-80, 320), 640), Height: max(cs.Height-100, 240)}
		if n.detents == 1 { // medium only
			size.Height = max(cs.Height/2, 240)
		}
		style := uint(styleTitled | styleResizable)
		if n.nav.full {
			// Its size is its window's, so the user doesn't resize it.
			size, style = coverSize(over.window), styleTitled
		}
		w := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
			Rect{Size: size}, style, backingBuffered, false)
		Send(w, "setReleasedWhenClosed:", 0)
		content := initView("AsukaView")
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
		Send(over.window, "beginSheet:completionHandler:", uintptr(w), 0)
	case ops.KindWindow:
		h.openWindow(n)
	case ops.KindPopover:
		h.presentPopover(n)
	case ops.KindSnackbar:
		h.presentSnack(n)
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
			if b.is(ops.MenuDisabled) {
				Send(btn, "setEnabled:", 0)
			}
		}
		if len(order) == 0 {
			Send(a, "addButtonWithTitle:", uintptr(String(l10n.OK(h.locale))))
		}
		n.nav.alert = a
		n.nav.order = order
		if n.alertStyle == int32(ops.AlertStyleAlert) && len(n.nav.fields) > 0 {
			h.addAlertFields(n, a)
		}
		id := n.id
		done := NewBlock(func(_ objc.Block, resp int) {
			n := h.nodes[id]
			if n != nil {
				h.forgetAlertFields(n)
			}
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
		Send(a, "beginSheetModalForWindow:completionHandler:", uintptr(h.onWin(n).window), uintptr(done))
		if len(n.nav.inputs) > 0 {
			h.alertDefaultButton(n, a)
			Send(Send(a, "window"), "makeFirstResponder:", uintptr(n.nav.inputs[0]))
		}
	}
}

// alertField is a text field of an alert showing: its node and index.
type alertField struct {
	id    ops.NodeID
	index int
}

// alertFieldWidth is an alert's fields' width, in points: as wide as the
// alert's text (NSAlert's own is about this wide).
const alertFieldWidth = 228

// addAlertFields gives an alert its text fields: a column of
// NSTextFields (NSSecureTextFields for passwords) as its accessory view,
// the first the window's first responder, Tab going from one to the
// next. Typing reports FieldChanged; return presses the default button,
// the first whose role is Default, which takes the key equivalent.
func (h *Host) addAlertFields(n *node, a ID) {
	const height, gap = 22, 8
	count := len(n.nav.fields)
	total := float64(count*height + (count-1)*gap)
	box := Send(Send(Class("NSView"), "alloc"), "initWithFrame:", 0)
	MsgSetRect(box, selSetFrame, Rect{Size: Size{Width: alertFieldWidth, Height: total}})
	if h.alertFields == nil {
		h.alertFields = map[ID]alertField{}
	}
	n.nav.inputs = n.nav.inputs[:0]
	for i, f := range n.nav.fields {
		cls := "NSTextField"
		if f.Secure() {
			cls = "NSSecureTextField"
		}
		tf := Send(Send(Class(cls), "alloc"), "initWithFrame:", 0)
		// From the top: a view's y is from its bottom.
		MsgSetRect(tf, selSetFrame, Rect{Origin: Point{Y: total - float64((i+1)*height+i*gap)}, Size: Size{Width: alertFieldWidth, Height: height}})
		Send(tf, "setStringValue:", uintptr(String(f.Text)))
		Send(tf, "setPlaceholderString:", uintptr(String(f.Placeholder)))
		Send(tf, "setAccessibilityLabel:", uintptr(String(f.Placeholder)))
		Send(tf, "setUsesSingleLineMode:", 1)
		Send(Send(tf, "cell"), "setScrollable:", 1)
		if RespondsTo(tf, "setContentType:") && f.ContentType != "" {
			if t := contentType(f.ContentType); t != 0 {
				Send(tf, "setContentType:", uintptr(t))
			}
		}
		Send(tf, "setDelegate:", uintptr(h.target))
		Send(box, "addSubview:", uintptr(tf))
		h.alertFields[tf] = alertField{id: n.id, index: i}
		n.nav.inputs = append(n.nav.inputs, tf)
		Release(tf)
	}
	for i, tf := range n.nav.inputs {
		Send(tf, "setNextKeyView:", uintptr(n.nav.inputs[(i+1)%count]))
	}
	Send(a, "setAccessoryView:", uintptr(box))
	Release(box)
	Send(a, "layout")
	Send(Send(a, "window"), "setInitialFirstResponder:", uintptr(n.nav.inputs[0]))
}

// alertDefaultButton gives return to an alert's default button, the
// first whose role is Default, wherever it is among them (NSAlert gives
// it to the first, as it shows).
func (h *Host) alertDefaultButton(n *node, a ID) {
	def := -1
	for i := range n.actions {
		if n.actions[i].role == ops.AlertButtonRoleDefault {
			def = i
			break
		}
	}
	btns := Send(a, "buttons")
	for k, i := range n.nav.order {
		btn := Send(btns, "objectAtIndex:", uintptr(k))
		switch {
		case i == def:
			Send(btn, "setKeyEquivalent:", uintptr(String("\r")))
			n.nav.defBtn = btn
		case GoString(Send(btn, "keyEquivalent")) == "\r":
			Send(btn, "setKeyEquivalent:", uintptr(String("")))
		}
	}
}

// forgetAlertFields forgets an alert's fields, gone or going.
func (h *Host) forgetAlertFields(n *node) {
	for _, tf := range n.nav.inputs {
		Send(tf, "setDelegate:", 0)
		delete(h.alertFields, tf)
	}
	n.nav.inputs = nil
	n.nav.defBtn = 0
}

// alertFieldChanged reports the user's typing in an alert's field; false
// if field isn't one.
func (h *Host) alertFieldChanged(field ID) bool {
	f, ok := h.alertFields[field]
	if !ok {
		return false
	}
	if n := h.nodes[f.id]; n != nil && !n.dying {
		s := GoString(Send(field, "stringValue"))
		h.emit(n, ops.Event{Kind: ops.EventFieldChanged, Bytes: ops.AppendFieldChanged(nil, f.index, s)})
	}
	return true
}

// alertFieldCommand handles return in an alert's field: before the last
// field it goes on to the next; in the last it presses the default
// button, if it's enabled. ok is false if field isn't one.
func (h *Host) alertFieldCommand(field ID, cmd SEL) (handled, ok bool) {
	f, ok := h.alertFields[field]
	if !ok {
		return false, false
	}
	if cmd != Sel("insertNewline:") {
		return false, true
	}
	n := h.nodes[f.id]
	if n == nil || n.dying {
		return true, true
	}
	if f.index+1 < len(n.nav.inputs) {
		Send(Send(field, "window"), "makeFirstResponder:", uintptr(n.nav.inputs[f.index+1]))
		return true, true
	}
	if b := n.nav.defBtn; b != 0 && SendBool(b, "isEnabled") {
		Send(b, "performClick:", 0)
	}
	return true, true
}

// coverSize is the size of a full-screen sheet over window w: all of
// its content under the title bar and toolbar (contentLayoutRect), where
// AppKit hangs a sheet.
func coverSize(w ID) Size {
	return MsgRect(w, Sel("contentLayoutRect")).Size
}

// fitCover sizes a full-screen sheet's window to its window's content
// again (that window resized; AppKit keeps the sheet where it hangs).
func (h *Host) fitCover(n *node) {
	w := n.nav.window
	p := Send(w, "sheetParent")
	if p == 0 {
		return
	}
	want := coverSize(p)
	if MsgRect(Send(w, "contentView"), Sel("bounds")).Size == want {
		return
	}
	MsgSetSize(w, Sel("setContentSize:"), want)
}

// updateAlertButtons shows a presented alert's buttons' new titles and
// whether they're enabled.
func (h *Host) updateAlertButtons(n *node) {
	a := n.nav.alert
	if a == 0 || n.dying {
		return
	}
	btns := Send(a, "buttons")
	count := int(SendInt(btns, "count"))
	for k, i := range n.nav.order {
		if k >= count || i >= len(n.actions) {
			break
		}
		b := &n.actions[i]
		btn := Send(btns, "objectAtIndex:", uintptr(k))
		Send(btn, "setTitle:", uintptr(String(b.title)))
		Send(btn, "setEnabled:", Bool(!b.is(ops.MenuDisabled)))
	}
}

// cancel is Escape: a sheet's user dismissal.
func (h *Host) cancel(view ID) {
	w := Send(view, "window")
	for _, n := range h.nodes {
		if n.kind == ops.KindSheet && n.nav.window == w && !n.dying {
			if n.interceptBack {
				h.emit(n, ops.Event{Kind: ops.EventBack})
				return
			}
			h.emit(n, ops.Event{Kind: ops.EventDismissed})
			return
		}
	}
	// Left by the content: a flyout or a panel closes, as menus do.
	h.escapeFloating(w)
}

// win is one of the app's windows: the main one, holding the root, or a
// Window's. Each has its own chrome: its title and toolbar are its
// innermost shown stack's.
type win struct {
	window, content ID
	root            *node  // what it shows: the root, or the Window node
	base            string // its title when no screen names it
	chrome          chrome

	// Its options and state (winctl.go).
	cfg        ops.WindowConfig
	state      ops.WindowState // as last reported
	stateSent  bool
	shown      bool // has shown: what it opens as (zoomed, full screen) is done
	configured bool // its options are applied
	child      ID   // the window it's a child window of
	sheetOf    ID   // the window it's a sheet of (Modal)

	// A flyout's and a panel's (flyout.go): a flyout's anchor; placed, a
	// panel the app placed (not centered on the pointer's display as it
	// shows); showOnFit, a window sized to its content that shows once
	// it's sized (fitted).
	flyout            ops.Flyout
	placed            bool
	showOnFit, fitted bool
}

// chrome is what a window's title and toolbar show: the innermost shown
// stack's top screen.
type chrome struct {
	stack, screen *node
	back          bool
	actions       []action
	search        string
	searchField   ID
	ids           []string
	toolbar       ID
	menus         []ID // the bar buttons' menus (+1), registered in byMenu
}

// winOf returns the window w is (nil: not one of the app's).
func (h *Host) winOf(w ID) *win {
	if h.main != nil && h.main.window == w {
		return h.main
	}
	return h.wins[w]
}

// keyWin is the window the user is working in: the key window if it's
// one of the app's, else the main one.
func (h *Host) keyWin() *win {
	app := Send(Class("NSApplication"), "sharedApplication")
	for _, sel := range []string{"keyWindow", "mainWindow"} {
		if w := h.winOf(Send(app, sel)); w != nil {
			return w
		}
	}
	// The app isn't active: its front window that shows (a document
	// app's main window stays hidden).
	if h.main == nil || !SendBool(h.main.window, "isVisible") {
		for _, nw := range ArrayItems(Send(app, "orderedWindows")) {
			if w := h.winOf(nw); w != nil && SendBool(nw, "isVisible") {
				return w
			}
		}
	}
	return h.main
}

func (h *Host) allWins() []*win {
	out := make([]*win, 0, len(h.wins)+1)
	if h.main != nil {
		out = append(out, h.main)
	}
	for _, w := range h.wins {
		out = append(out, w)
	}
	return out
}

// activeStack finds the innermost stack on screen under root: through
// each stack's top screen and each tabs' selected page.
func (h *Host) activeStack(root *node) *node {
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
		case ops.KindSheet, ops.KindAlert, ops.KindWindow, ops.KindPopover:
			if n != root {
				return
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if root != nil {
		walk(root)
	}
	return found
}

func (h *Host) updateWindowChrome() {
	if h.main == nil {
		return
	}
	if h.depth > 0 {
		if !h.chromeQueued {
			h.chromeQueued = true
			h.afterCore(func() {
				h.chromeQueued = false
				WithPool(h.updateChromes)
			})
		}
		return
	}
	WithPool(h.updateChromes)
}

func (h *Host) updateChromes() {
	for _, w := range h.allWins() {
		h.updateChrome(w)
	}
}

func (h *Host) updateChrome(w *win) {
	c := &w.chrome
	stack := h.activeStack(w.root)
	var screen *node
	if stack != nil {
		screen = h.topScreen(stack)
	}
	title := w.base
	if screen != nil && screen.title != "" {
		title = screen.title
	} else if t := h.selectedTab(w.root); t != nil && t.title != "" {
		title = t.title
	}
	Send(w.window, "setTitle:", uintptr(String(title)))
	c.stack, c.screen = stack, screen
	var ids []string
	if stack != nil {
		c.back = h.liveScreens(stack) > 1
		if c.back {
			ids = append(ids, "asuka.back")
		}
		ids = append(ids, "NSToolbarFlexibleSpaceItem")
		c.actions, c.search = nil, ""
		if screen != nil {
			// SwiftUI's bottomBar is iOS's: a Mac's toolbar has the screen's
			// Toolbar after its bar buttons, and its Primary last, prominent.
			c.actions, c.search = screen.actions, screen.nav.search
			if len(screen.nav.toolbar) > 0 || len(screen.nav.primary) > 0 {
				c.actions = slices.Concat(screen.actions, screen.nav.toolbar, screen.nav.primary)
			}
		}
		if c.search != "" {
			ids = append(ids, "asuka.search")
		}
		for i := range c.actions {
			ids = append(ids, "asuka.action."+itoa(i))
		}
		if h.toolbarReversed() {
			slices.Reverse(ids)
		}
	}
	if stack == nil {
		if c.toolbar != 0 {
			Send(w.window, "setToolbar:", 0)
			Release(c.toolbar)
			c.toolbar = 0
			h.releaseBarMenus(c)
			h.environmentChanged()
		}
		return
	}
	if c.toolbar == 0 {
		// Its own identifier: toolbars that share one keep the same items, which
		// the windows' don't.
		t := Send(Send(Class("NSToolbar"), "alloc"), "initWithIdentifier:", uintptr(String("asuka."+itoa(int(w.window)))))
		Send(t, "setDelegate:", uintptr(h.target))
		Send(t, "setDisplayMode:", 2) // icons
		Send(t, "setAllowsUserCustomization:", 0)
		c.toolbar = t
		if RespondsTo(w.window, "setToolbarStyle:") {
			Send(w.window, "setToolbarStyle:", 3) // unified: the title beside the back button
		}
		Send(w.window, "setToolbar:", uintptr(t))
		h.environmentChanged()
	}
	// The items again: they're the screen's.
	t := c.toolbar
	for SendInt(Send(t, "items"), "count") > 0 {
		Send(t, "removeItemAtIndex:", 0)
	}
	h.releaseBarMenus(c)
	c.searchField = 0
	c.ids = ids
	for i, id := range ids {
		Send(t, "insertItemWithItemIdentifier:atIndex:", uintptr(String(id)), uintptr(i))
	}
}

func (h *Host) liveScreens(stack *node) int {
	live := 0
	for _, s := range stack.children {
		if !s.dying {
			live++
		}
	}
	return live
}

func (h *Host) selectedTab(root *node) *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if n.kind == ops.KindTabs && n.selected < len(n.children) {
			found = n.children[n.selected]
			walk(found)
			return
		}
		if n != root && (n.kind == ops.KindSheet || n.kind == ops.KindWindow) {
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if root != nil {
		walk(root)
	}
	return found
}

// toolbarWin is the window a toolbar is in.
func (h *Host) toolbarWin(t ID) *win {
	for _, w := range h.allWins() {
		if w.chrome.toolbar == t {
			return w
		}
	}
	return nil
}

// toolbarItem makes the item for an identifier of a window's chrome.
func (h *Host) toolbarItem(w *win, ident string) ID {
	c := &w.chrome
	item := func() ID {
		it := Send(Send(Class("NSToolbarItem"), "alloc"), "initWithItemIdentifier:", uintptr(String(ident)))
		return Autorelease(it)
	}
	switch {
	case ident == "asuka.back":
		it := item()
		back := l10n.Back(h.locale)
		Send(it, "setImage:", uintptr(symbolImage(h.backSymbol())))
		Send(it, "setLabel:", uintptr(String(back)))
		Send(it, "setToolTip:", uintptr(String(back)))
		Send(it, "setTarget:", uintptr(h.target))
		Send(it, "setAction:", uintptr(Sel("back:")))
		// A navigational item goes on the app's leading side, wherever it's
		// put: not where the host orders the toolbar itself.
		if RespondsTo(it, "setNavigational:") && !h.toolbarReversed() {
			Send(it, "setNavigational:", 1)
		}
		if RespondsTo(it, "setBordered:") {
			Send(it, "setBordered:", 1)
		}
		return it
	case ident == "asuka.search":
		var it ID
		if HasClass("NSSearchToolbarItem") {
			it = Autorelease(Send(Send(Class("NSSearchToolbarItem"), "alloc"), "initWithItemIdentifier:", uintptr(String(ident))))
			c.searchField = Send(it, "searchField")
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
		Send(f, "setUserInterfaceLayoutDirection:", direction(h.rtl))
		if h.rtl {
			Send(f, "setAlignment:", alignRight)
		}
		Send(f, "setPlaceholderString:", uintptr(String(c.search)))
		if c.screen != nil {
			Send(f, "setStringValue:", uintptr(String(c.screen.nav.searchText)))
		}
		return it
	case len(ident) > len("asuka.action.") && ident[:len("asuka.action.")] == "asuka.action.":
		i := atoi(ident[len("asuka.action."):])
		if i < 0 || i >= len(c.actions) {
			return 0
		}
		a := &c.actions[i]
		var it ID
		if a.is(ops.MenuSubmenu) && len(a.items) > 0 && HasClass("NSMenuToolbarItem") {
			// A bar button with children opens them as a menu.
			it = Autorelease(Send(Send(Class("NSMenuToolbarItem"), "alloc"), "initWithItemIdentifier:", uintptr(String(ident))))
			m := h.makeMenuOf(a.items, "menuItem:")
			Send(it, "setMenu:", uintptr(m))
			Send(it, "setShowsIndicator:", 1)
			c.menus = append(c.menus, m)
			if c.screen != nil {
				h.byMenu[m] = c.screen
			}
		} else {
			it = item()
			Send(it, "setTarget:", uintptr(h.target))
			Send(it, "setAction:", uintptr(Sel("barItem:")))
		}
		Send(it, "setLabel:", uintptr(String(a.title)))
		Send(it, "setToolTip:", uintptr(String(a.title)))
		if img := h.barSymbol(a.icon); img != 0 {
			Send(it, "setImage:", uintptr(img))
		} else if RespondsTo(it, "setTitle:") {
			Send(it, "setTitle:", uintptr(String(a.title)))
		}
		if RespondsTo(it, "setBordered:") {
			Send(it, "setBordered:", 1)
		}
		Send(it, "setTag:", uintptr(a.index))
		if a.index >= ops.ScreenPrimaryAction && RespondsTo(it, "setStyle:") {
			// The screen's main action: tinted with the accent (macOS 26's
			// NSToolbarItemStyleProminent).
			Send(it, "setStyle:", 1)
		}
		if a.is(ops.MenuDisabled) {
			// Dimmed, and kept so: validation would enable it again.
			Send(it, "setAutovalidates:", 0)
			Send(it, "setEnabled:", 0)
		}
		return it
	}
	return 0
}

// releaseBarMenus lets go of the menus of a window's bar buttons.
func (h *Host) releaseBarMenus(c *chrome) {
	for _, m := range c.menus {
		delete(h.byMenu, m)
		Release(m)
	}
	c.menus = c.menus[:0]
}

// barItem is a screen's bar button.
func (h *Host) barItem(it ID) {
	if !RespondsTo(it, "toolbar") {
		return
	}
	w := h.toolbarWin(Send(it, "toolbar"))
	if w == nil || w.chrome.screen == nil {
		return
	}
	h.emit(w.chrome.screen, ops.Event{Kind: ops.EventAction, Int: int64(SendInt(it, "tag"))})
}

// senderWin is the window an action came from: a toolbar item's, else
// the one the user works in (a menu item, a shortcut).
func (h *Host) senderWin(sender ID) *win {
	if sender != 0 && RespondsTo(sender, "toolbar") {
		if w := h.toolbarWin(Send(sender, "toolbar")); w != nil {
			return w
		}
	}
	return h.keyWin()
}

// back pops the innermost shown stack's top screen in w, as its back
// button.
func (h *Host) back(w *win) {
	if w == nil {
		return
	}
	stack := h.activeStack(w.root)
	if stack == nil {
		return
	}
	top := h.topScreen(stack)
	if top == nil || h.liveScreens(stack) < 2 {
		return
	}
	if top.interceptBack {
		h.emit(top, ops.Event{Kind: ops.EventBack})
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

// canGoBack reports whether w's back button would pop (the Back menu
// item's state).
func (h *Host) canGoBack(w *win) bool {
	if w == nil {
		return false
	}
	stack := h.activeStack(w.root)
	return stack != nil && h.liveScreens(stack) > 1
}

func (h *Host) searchWin(field ID) *win {
	for _, w := range h.allWins() {
		if w.chrome.searchField == field && field != 0 {
			return w
		}
	}
	return nil
}

func (h *Host) searchChanged(field ID) {
	w := h.searchWin(field)
	if w == nil || w.chrome.screen == nil {
		return
	}
	s := GoString(Send(field, "stringValue"))
	w.chrome.screen.nav.searchText = s
	h.emit(w.chrome.screen, ops.Event{Kind: ops.EventTextChanged, Str: s})
}

func (h *Host) searchCommand(control ID, cmd SEL) bool {
	w := h.searchWin(control)
	if w == nil || w.chrome.screen == nil {
		return false
	}
	if cmd == Sel("insertNewline:") {
		h.emit(w.chrome.screen, ops.Event{Kind: ops.EventSubmit})
		return true
	}
	return false
}

// navMethods are AsukaTarget's toolbar delegate methods, Escape and the
// windows' closing.
func navMethods() []objc.MethodDef {
	return []objc.MethodDef{
		Method("toolbar:itemForItemIdentifier:willBeInsertedIntoToolbar:", func(self ID, _ SEL, t, ident ID, insert bool) ID {
			if w := theHost.toolbarWin(t); w != nil {
				return theHost.toolbarItem(w, GoString(ident))
			}
			return 0
		}),
		Method("toolbarDefaultItemIdentifiers:", func(self ID, _ SEL, t ID) ID { return theHost.toolbarIDs(t) }),
		Method("toolbarAllowedItemIdentifiers:", func(self ID, _ SEL, t ID) ID { return theHost.toolbarIDs(t) }),
		Method("cancelOperation:", func(self ID, _ SEL, sender ID) {
			if w := Send(Send(Class("NSApplication"), "sharedApplication"), "keyWindow"); w != 0 {
				theHost.cancel(Send(w, "contentView"))
			}
		}),
		Method("windowShouldClose:", func(self ID, _ SEL, w ID) bool {
			h := theHost
			if h.main != nil && w == h.main.window {
				// The core asks the app first, and closes it if it may.
				if h.root != nil && h.root.listening(ops.EventCloseRequest) {
					h.emit(h.root, ops.Event{Kind: ops.EventCloseRequest})
					return false
				}
				return true
			}
			// A Window's or a sheet's: the core closes it, asking the app
			// first if it wants to be.
			if x := h.wins[w]; x != nil {
				if x.root.listening(ops.EventCloseRequest) {
					h.emit(x.root, ops.Event{Kind: ops.EventCloseRequest})
				} else {
					h.emit(x.root, ops.Event{Kind: ops.EventDismissed})
				}
				return false
			}
			h.cancel(Send(w, "contentView"))
			return false
		}),
	}
}

func (h *Host) toolbarIDs(t ID) ID {
	arr := Send(Class("NSMutableArray"), "array")
	if w := h.toolbarWin(t); w != nil {
		for _, id := range w.chrome.ids {
			Send(arr, "addObject:", uintptr(String(id)))
		}
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

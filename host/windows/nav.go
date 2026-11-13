//go:build windows

package windows

import (
	"math"

	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Navigation on Windows:
//
//   - A Stack shows its top Screen under a header bar of its own: a back
//     button (Alt+Left too) while there's a screen to go back to, the
//     screen's title, and its bar buttons on the right. Screens change
//     without a transition, so there's no slide to mirror.
//   - Tabs are a bar of tabs on top (as WinUI's NavigationView at the
//     top: an icon and a title each, an accent pill under the selected
//     one), the selected page under it.
//   - A Sheet is a modal window over the app's (its owner is disabled
//     meanwhile); closing it or Escape dismisses it. A FullScreen one is
//     a borderless window over all of its window's content (under its
//     title bar, which stays its window's), following it as it moves
//     and resizes: as Fluent's full-size ContentDialog takes a window;
//     Escape dismisses it too.
//   - An Alert is a task dialog (TaskDialogIndirect): an action sheet's
//     buttons are command links. One with text fields is a dialog of the
//     host's own, as WinUI's ContentDialog (alertdialog.go).
//   - A Window is a top-level window of its own (appwindow.go).
//
// Sheets and alerts open over the window in use. Screens, sheets and
// Windows are sized by the host, which reports each one's Environment.
type nav struct {
	env      ops.Environment
	reported bool

	// Screen.
	backGesture bool
	search      string
	searchText  string

	// Stack, Tabs: the bar above the screens.
	bar *node

	// A stack's header.
	stack   *node
	back    uintptr
	buttons []uintptr // the screen's bar buttons
	screen  *node     // the screen whose title and buttons it shows
	field   *node     // its search field (search.go), once a screen had one

	// A tab bar.
	tabs *node
	hot  int

	// A screen's Toolbar and Primary, numbered within each (their Action
	// events add ops.ScreenToolbarActions, ops.ScreenPrimaryAction); a
	// stack's command bar showing its top screen's (foot), and that bar's
	// state (cmd).
	toolbar, primary []action
	foot             *node
	cmd              *cmdBar

	// Sheet, Window: its window; a sheet's owner. Sheet, Alert: the
	// window it's declared in (its Window prop: the root's or a Window
	// node's ID), which it goes on.
	window uintptr
	owner  uintptr
	on     ops.NodeID
	full   bool // a Sheet's FullScreen: over all of its window's content

	// Alert: its task dialog's window, while it shows; its text fields,
	// and with them the host's own dialog (alertdialog.go).
	dialog uintptr
	fields []ops.AlertField
	dlg    *alertDialog

	// Popover: what it points at, and its content's size (points).
	anchor     ops.NodeID
	popW, popH float32

	// Window: its content's size when it opens, and its least, in points;
	// its options (Config), and its window as the app controls it.
	size, minSize [2]float32
	cfg           ops.WindowConfig
	ctl           *wctl
	fitted        bool       // size is its content's, WindowAction Fit's, which came before it opened
	flyout        ops.Flyout // a flyout's anchor (its Flyout prop), if hasFlyout
	hasFlyout     bool
	// edited: the document it shows has unsaved changes (a * before its
	// title, as Notepad marks it).
	edited bool
}

const (
	headerHeight = 48
	tabBarHeight = 48
)

func (h *Host) createNav(n *node) bool {
	switch n.kind {
	case ops.KindStack:
		n.hwnd = h.makeView(0)
		n.nav = &nav{}
		n.nav.bar = h.makeHeader(n)
	case ops.KindScreen:
		n.hwnd = h.makeView(0)
		n.nav = &nav{backGesture: true}
	case ops.KindTabs:
		n.hwnd = h.makeView(0)
		n.nav = &nav{}
		n.nav.bar = h.makeTabBar(n)
	case ops.KindSheet, ops.KindWindow, ops.KindPopover:
		n.hwnd = h.makeView(0)
		n.nav = &nav{}
	case ops.KindAlert:
		n.nav = &nav{}
	case ops.KindSnackbar:
		n.hwnd = h.makeView(0)
		ShowWindow(n.hwnd, SW_HIDE) // until it's presented, placed
		n.nav = &nav{}
		n.snack = &snack{}
	default:
		return false
	}
	return true
}

// sizedByHost reports whether the host sizes n (a screen, a sheet's
// content), not the core.
func (nv *nav) sizedByHost(n *node) bool {
	return n.kind == ops.KindScreen || n.kind == ops.KindSheet || n.kind == ops.KindWindow || n.kind == ops.KindPopover
}

func (h *Host) insertNav(p, c *node, index int) bool {
	if c.kind != ops.KindScreen || p.kind != ops.KindStack && p.kind != ops.KindTabs {
		return false
	}
	i := min(index, len(p.children))
	top := h.topScreen(p)
	p.children = append(p.children, nil)
	copy(p.children[i+1:], p.children[i:])
	p.children[i] = c
	c.parent = p
	SetParent(c.hwnd, p.hwnd)
	h.adopt(p, c)
	SetWindowPos(c.hwnd, HWND_BOTTOM, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	switch p.kind {
	case ops.KindStack:
		if top != nil && h.topScreen(p) == c {
			ShowWindow(top.hwnd, SW_HIDE)
		}
		if h.topScreen(p) != c {
			ShowWindow(c.hwnd, SW_HIDE)
		}
	case ops.KindTabs:
		if i != p.selected {
			ShowWindow(c.hwnd, SW_HIDE)
		}
		Invalidate(p.nav.bar.hwnd)
	}
	h.layoutScreens(p)
	h.updateHeader(p)
	return true
}

func (h *Host) removeNav(p, c *node) bool {
	if p.kind != ops.KindStack && p.kind != ops.KindTabs {
		return false
	}
	if c.hwnd != 0 {
		ShowWindow(c.hwnd, SW_HIDE)
		SetParent(c.hwnd, h.park)
	}
	h.showScreens(p)
	h.updateHeader(p)
	return true
}

// destroyNav destroys a screen, sheet or alert; false: an ordinary
// destroy.
func (h *Host) destroyNav(n *node) bool {
	if n.kind == ops.KindStack && n.nav != nil && n.nav.foot != nil {
		// Its command bar goes with its window.
		f := n.nav.foot
		n.nav.foot = nil
		for _, b := range append(f.nav.cmd.buttons, f.nav.cmd.more, f.nav.cmd.primary) {
			delete(h.byHwnd, b)
		}
		delete(h.byHwnd, f.hwnd)
		delete(h.nodes, f.id)
	}
	if n.nav == nil || n.kind == ops.KindStack || n.kind == ops.KindTabs {
		return false
	}
	switch n.kind {
	case ops.KindScreen:
		p := n.parent
		if p != nil {
			for i, x := range p.children {
				if x == n {
					p.children = append(p.children[:i], p.children[i+1:]...)
					break
				}
			}
			n.parent = nil
		}
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		if p != nil {
			h.showScreens(p)
			h.layoutScreens(p)
			h.updateHeader(p)
		}
		return true
	case ops.KindSheet, ops.KindWindow, ops.KindPopover:
		n.dying = true
		switch n.kind {
		case ops.KindSheet:
			h.closeSheet(n)
		case ops.KindPopover:
			h.closePopover(n)
		default:
			h.closeWindow(n)
		}
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		return true
	case ops.KindAlert:
		n.dying = true
		h.closeAlertDialog(n)
		return true
	case ops.KindSnackbar:
		n.dying = true
		h.destroySnack(n)
		return true
	}
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

// showScreens shows a stack's top screen, or the tabs' selected page,
// and hides the others.
func (h *Host) showScreens(p *node) {
	var shown *node
	switch p.kind {
	case ops.KindStack:
		shown = h.topScreen(p)
	case ops.KindTabs:
		if p.selected < len(p.children) {
			shown = p.children[p.selected]
		}
	}
	for _, c := range p.children {
		if c.hwnd == 0 {
			continue
		}
		if c == shown {
			ShowWindow(c.hwnd, SW_SHOWNA)
		} else {
			ShowWindow(c.hwnd, SW_HIDE)
		}
	}
}

// barHeight is the height of the bar above a stack's or tabs' screens.
func (h *Host) barHeight(p *node) float32 {
	switch p.kind {
	case ops.KindStack:
		if h.headerShown(p) {
			return headerHeight
		}
	case ops.KindTabs:
		return tabBarHeight
	}
	return 0
}

// layoutScreens sizes a stack's screens, or a tabs' pages, to it under
// its bar, and reports their environments.
func (h *Host) layoutScreens(n *node) {
	if n.hwnd == 0 || n.nav == nil {
		return
	}
	r := ClientRect(n.hwnd)
	bar := h.px(n, h.barHeight(n))
	if b := n.nav.bar; b != nil && b.hwnd != 0 {
		SetWindowPos(b.hwnd, HWND_TOP, 0, 0, r.W(), bar, SWP_NOACTIVATE|SWP_NOCOPYBITS)
		if n.kind == ops.KindStack {
			h.layoutHeader(b)
		}
	}
	// A stack's command bar, under its screens.
	foot := h.px(n, h.cmdBarHeightOf(n))
	if f := n.nav.foot; f != nil && f.hwnd != 0 && foot > 0 {
		SetWindowPos(f.hwnd, HWND_TOP, 0, max(r.H()-foot, 0), r.W(), foot, SWP_NOACTIVATE|SWP_NOCOPYBITS)
		h.layoutCommandBar(f)
	}
	ch := max(r.H()-bar-foot, 0)
	for _, c := range n.children {
		if c.hwnd == 0 {
			continue
		}
		SetWindowPos(c.hwnd, 0, 0, bar, r.W(), ch, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		h.reportScreen(c, h.pt(n, r.W()), h.pt(n, ch))
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
				r := ClientRect(n.nav.window)
				h.reportScreen(n, h.pt(n, r.W()), h.pt(n, r.H()))
			}
		}
	}
	h.placeSnacks()
}

// reportScreen sends a screen's or sheet's environment if it changed.
func (h *Host) reportScreen(n *node, w, ht float32) {
	if n.nav == nil || w <= 0 {
		return
	}
	env := h.env
	env.Width, env.Height = w, ht
	env.Scale = h.scaleOf(n)
	env.SafeTop, env.SafeRight, env.SafeBottom, env.SafeLeft = 0, 0, 0, 0
	if n.kind == ops.KindWindow {
		env.SafeTop = n.nav.ctl.safeTop() // under a hidden title bar's buttons
	}
	if n.nav.reported && env == n.nav.env {
		return
	}
	n.nav.env, n.nav.reported = env, true
	h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: env})
}

func (h *Host) setNavProp(n *node, p ops.PropID, v *ops.Value) {
	switch n.kind {
	case ops.KindScreen:
		switch p {
		case ops.PropScreenTitle:
			n.title = v.Str
		case ops.PropScreenActions:
			n.actions = decodeActions(v.Bytes)
		case ops.PropScreenToolbar:
			n.nav.toolbar = decodeActions(v.Bytes)
		case ops.PropScreenPrimary:
			n.nav.primary = decodeActions(v.Bytes)
		case ops.PropScreenBackGesture:
			n.nav.backGesture = v.Int != 0
		case ops.PropScreenIcon:
			n.icon = v.Str
		case ops.PropScreenBadge:
			// The tab bar draws it again, and nothing else changes.
			n.badge = v.Str
			if s := n.parent; s != nil && s.kind == ops.KindTabs {
				Invalidate(s.nav.bar.hwnd)
			}
			return
		case ops.PropScreenSearch:
			n.nav.search = v.Str
		case ops.PropScreenSearchText:
			n.nav.searchText = v.Str
		}
		if s := n.parent; s != nil {
			h.updateHeader(s)
			if s.kind == ops.KindTabs {
				Invalidate(s.nav.bar.hwnd)
			}
		}
	case ops.KindTabs:
		if p == ops.PropTabsSelected {
			n.selected = int(v.Int)
			h.showScreens(n)
			h.layoutScreens(n)
			Invalidate(n.nav.bar.hwnd)
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
			if n.nav.window != 0 {
				SetWindowText(n.nav.window, h.windowTitle(n))
			}
		case ops.PropWindowConfig:
			c := ops.DecodeWindowConfig(v.Bytes)
			n.nav.cfg = c
			n.nav.size = [2]float32{c.Width, c.Height}
			n.nav.minSize = [2]float32{c.MinWidth, c.MinHeight}
		case ops.PropWindowFlyout:
			if f, ok := ops.DecodeFlyout(v.Bytes); ok {
				n.nav.flyout, n.nav.hasFlyout = f, true
				if c := n.nav.ctl; c != nil && n.nav.window != 0 {
					h.placeFlyout(c) // flyout.go
				}
			}
		case ops.PropWindowEdited:
			n.nav.edited = v.Int != 0
			if n.nav.window != 0 {
				SetWindowText(n.nav.window, h.windowTitle(n))
			}
		}
	case ops.KindAlert:
		switch p {
		case ops.PropAlertTitle:
			n.title = v.Str
		case ops.PropAlertMessage:
			n.message = v.Str
		case ops.PropAlertButtons:
			n.actions = decodeActions(v.Bytes)
			// Showing, its buttons follow Disabled (a task dialog's titles
			// are its own once it's made).
			for i, a := range n.actions {
				EnableTaskButton(n.nav.dialog, alertButtonID(i, a), !a.is(ops.MenuDisabled))
			}
			h.updateAlertDialog(n) // or its own dialog's
		case ops.PropAlertFields:
			n.nav.fields = ops.DecodeAlertFields(v.Bytes)
		case ops.PropAlertStyle:
			n.alertStyle = int32(v.Int)
		case ops.PropAlertWindow:
			n.nav.on = ops.NodeID(v.Int)
		}
	case ops.KindSnackbar:
		h.setSnackProp(n, p, v)
	}
}

// A stack's header.

func (h *Host) makeHeader(stack *node) *node {
	h.nextCell++
	b := newNode(ops.HostIDBit|h.nextCell, kindHeader)
	b.hwnd = h.makeView(0)
	b.nav = &nav{stack: stack}
	b.k = stack.k
	b.dir = stack.dir
	SetParent(b.hwnd, stack.hwnd)
	b.nav.back = CreateWindow(0, "BUTTON", l10n.Back(h.locale), WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, b.hwnd, 1)
	SendMessage(b.nav.back, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
	h.add(b)
	h.byHwnd[b.nav.back] = b
	return b
}

func (h *Host) headerShown(stack *node) bool {
	s := h.topScreen(stack)
	if s == nil {
		return false
	}
	return s.title != "" || len(s.actions) > 0 || s.nav.search != "" || h.liveScreens(stack) > 1
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

// updateHeader shows the top screen's title and buttons in its stack's
// header (once the core has returned: it reshapes the header's
// controls).
func (h *Host) updateHeader(stack *node) {
	if stack.kind != ops.KindStack || stack.nav == nil {
		return
	}
	b := stack.nav.bar
	screen := h.topScreen(stack)
	b.nav.screen = screen
	shown := h.headerShown(stack)
	if b.hwnd == 0 {
		return
	}
	var actions []action
	if screen != nil {
		actions = screen.actions
	}
	for len(b.nav.buttons) > len(actions) {
		k := len(b.nav.buttons) - 1
		btn := b.nav.buttons[k]
		delete(h.byHwnd, btn)
		DestroyWindow(btn)
		b.nav.buttons = b.nav.buttons[:k]
	}
	for len(b.nav.buttons) < len(actions) {
		btn := CreateWindow(0, "BUTTON", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, b.hwnd, uintptr(len(b.nav.buttons)+2))
		SendMessage(btn, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
		h.byHwnd[btn] = b
		b.nav.buttons = append(b.nav.buttons, btn)
	}
	for i, a := range actions {
		SetWindowText(b.nav.buttons[i], a.title)
		EnableWindow(b.nav.buttons[i], !a.is(ops.MenuDisabled))
		Invalidate(b.nav.buttons[i])
	}
	h.updateSearch(b, screen)
	h.updateCommandBar(stack)
	if h.liveScreens(stack) > 1 {
		ShowWindow(b.nav.back, SW_SHOWNA)
	} else {
		ShowWindow(b.nav.back, SW_HIDE)
	}
	if shown {
		ShowWindow(b.hwnd, SW_SHOWNA)
	} else {
		ShowWindow(b.hwnd, SW_HIDE)
	}
	h.layoutScreens(stack)
	Invalidate(b.hwnd)
}

// layoutHeader places the back button on the left and the bar buttons
// on the right (the other way round, right to left), 40 points square, or
// as wide as their titles.
func (h *Host) layoutHeader(b *node) {
	r := ClientRect(b.hwnd)
	size := h.px(b, 36)
	y := (r.H() - size) / 2
	rtl := h.isRTL(b)
	across := func(x, w int32) int32 {
		if rtl {
			return r.W() - x - w
		}
		return x
	}
	SetWindowPos(b.nav.back, 0, across(h.px(b, 6), h.px(b, 40)), y, h.px(b, 40), size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	x := r.W() - h.px(b, 8)
	screen := b.nav.screen
	for i := len(b.nav.buttons) - 1; i >= 0; i-- {
		w := h.px(b, 40)
		if screen != nil && i < len(screen.actions) && symbolGlyph(screen.actions[i].icon) == 0 {
			dc := h.measureDC
			old := SelectObject(dc, h.bodyFont(b))
			tw, _ := TextExtent(dc, UTF16Units(screen.actions[i].title))
			SelectObject(dc, old)
			w = tw + h.px(b, 24)
		}
		x -= w
		SetWindowPos(b.nav.buttons[i], 0, across(x, w), y, w, size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		x -= h.px(b, 4)
	}
	h.layoutSearch(b, x, across)
}

// headerButton is what a header's button shows: the back arrow, or a
// bar button's icon (Segoe Fluent Icons) or title.
func (nv *nav) headerButton(ctl uintptr, rtl bool) (uint16, string) {
	if ctl == nv.back {
		if rtl {
			return 0xE72A, "" // Forward: an arrow to the right, the way back goes
		}
		return 0xE72B, "" // Back
	}
	for i, b := range nv.buttons {
		if b == ctl && nv.screen != nil && i < len(nv.screen.actions) {
			a := nv.screen.actions[i]
			if g := directedGlyph(a.icon, rtl); g != 0 {
				return g, ""
			}
			return 0, a.title
		}
	}
	return 0, ""
}

func (h *Host) headerCommand(b *node, ctl uintptr) {
	if ctl == b.nav.back {
		h.popStack(b.nav.stack)
		return
	}
	screen := b.nav.screen
	for i, btn := range b.nav.buttons {
		if btn != ctl || screen == nil || i >= len(screen.actions) {
			continue
		}
		a := &screen.actions[i]
		if a.is(ops.MenuSubmenu) {
			// A bar button with children opens them as a menu, below it.
			h.popUpActions(screen, btn, b, a.items)
			return
		}
		h.emit(screen, ops.Event{Kind: ops.EventAction, Int: a.index})
	}
}

// paintHeader draws a hairline under the bar (g), and the screen's
// title beside the back button in Fluent's Subtitle (20, semibold) (dc).
func (h *Host) paintHeader(b *node, dc uintptr, g Graphics, r Rect) {
	stack := b.nav.stack
	screen := b.nav.screen
	k := h.scaleOf(b)
	if g != 0 {
		br := SolidBrush(h.theme.separator.argb())
		g.FillRect(br, 0, float32(r.H())-max(k, 1), float32(r.W()), max(k, 1))
		br.Close()
		return
	}
	if screen == nil || screen.title == "" {
		return
	}
	x := h.px(b, 16)
	if h.liveScreens(stack) > 1 {
		x = h.px(b, 52)
	}
	old := SelectObject(dc, h.font(h.titleSpec(b)))
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, h.theme.label.ref())
	rtl := h.isRTL(b)
	tr := Rect{Left: x, Top: 0, Right: r.Right - h.px(b, 16), Bottom: r.Bottom}
	if rtl {
		tr = flipIn(tr, r)
	}
	edges := b.nav.buttons
	if f := b.nav.field; f != nil && f.hwnd != 0 && IsWindowVisible(f.hwnd) {
		edges = append(edges[:len(edges):len(edges)], f.hwnd)
	}
	for _, btn := range edges {
		br := WindowRectIn(btn, b.hwnd)
		if rtl {
			tr.Left = max(tr.Left, br.Right+h.px(b, 8))
		} else {
			tr.Right = min(tr.Right, br.Left-h.px(b, 8))
		}
	}
	DrawText(dc, UTF16Units(screen.title), &tr, DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS|startSide(rtl))
	SelectObject(dc, old)
}

// popStack is the back button: the stack's top screen is popped (the
// core destroys it, told with Popped).
func (h *Host) popStack(stack *node) {
	top := h.topScreen(stack)
	if top == nil || h.liveScreens(stack) < 2 {
		return
	}
	if top.interceptBack {
		h.emit(top, ops.Event{Kind: ops.EventBack})
		return
	}
	top.dying = true
	h.showScreens(stack)
	h.updateHeader(stack)
	h.emit(stack, ops.Event{Kind: ops.EventPopped, Int: int64(top.id)})
}

// back pops the innermost shown stack that can pop in a window (Alt+Left):
// under root, the root or a sheet or Window.
func (h *Host) back(root *node) {
	if s := h.activeStack(root); s != nil {
		h.popStack(s)
	}
}

// activeStack finds the innermost stack on screen under root that can
// pop: through each stack's top screen and each tabs' selected page.
func (h *Host) activeStack(root *node) *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		switch n.kind {
		case ops.KindStack:
			if h.liveScreens(n) > 1 {
				found = n
			}
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

// A tab bar.

func (h *Host) makeTabBar(tabs *node) *node {
	h.nextCell++
	b := newNode(ops.HostIDBit|h.nextCell, kindTabBar)
	b.hwnd = h.makeView(WS_TABSTOP)
	b.nav = &nav{tabs: tabs, hot: -1}
	b.k = tabs.k
	b.dir = tabs.dir
	b.host = tabBarMessage
	SetParent(b.hwnd, tabs.hwnd)
	h.add(b)
	return b
}

// tabRects are the tabs' places in the bar: each its icon and title,
// padded, from the left (the right, right to left).
func (h *Host) tabRects(b *node) []Rect {
	tabs := b.nav.tabs
	r := ClientRect(b.hwnd)
	dc := h.measureDC
	old := SelectObject(dc, h.bodyFont(b))
	defer SelectObject(dc, old)
	var out []Rect
	x := h.px(b, 8)
	for _, s := range tabs.children {
		tw, _ := TextExtent(dc, UTF16Units(s.title))
		w := tw + h.px(b, 24)
		if symbolGlyph(s.icon) != 0 {
			w += h.px(b, 16+8)
		}
		if bw, _ := h.badgeSize(b, s.badge); bw > 0 {
			w += bw + h.px(b, 6)
		}
		out = append(out, Rect{Left: x, Top: h.px(b, 6), Right: x + w, Bottom: r.H() - h.px(b, 6)})
		x += w + h.px(b, 4)
	}
	if h.isRTL(b) {
		for i := range out {
			out[i] = flipIn(out[i], r)
		}
	}
	return out
}

func tabBarMessage(h *Host, b *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	tabs := b.nav.tabs
	at := func() int {
		p := Point{X: XParam(lp), Y: YParam(lp)}
		for i, r := range h.tabRects(b) {
			if p.X >= r.Left && p.X < r.Right && p.Y >= 0 {
				return i
			}
		}
		return -1
	}
	switch m {
	case WM_MOUSEMOVE:
		if i := at(); i != b.nav.hot {
			b.nav.hot = i
			TrackLeave(b.hwnd)
			Invalidate(b.hwnd)
		}
		return 0, true
	case WM_MOUSELEAVE:
		b.nav.hot = -1
		Invalidate(b.hwnd)
		return 0, true
	case WM_LBUTTONDOWN:
		if i := at(); i >= 0 {
			h.selectTab(tabs, i)
		}
		return 0, true
	case WM_GETDLGCODE:
		return DLGC_WANTARROWS, true
	case WM_KEYDOWN:
		// The arrows go the way they point: the next tab is to the left,
		// right to left.
		step := 1
		if h.isRTL(b) {
			step = -1
		}
		switch wp {
		case VK_LEFT:
			h.selectTab(tabs, tabs.selected-step)
			h.announceChoice(b)
			return 0, true
		case VK_RIGHT:
			h.selectTab(tabs, tabs.selected+step)
			h.announceChoice(b)
			return 0, true
		}
	}
	return 0, false
}

func (h *Host) selectTab(tabs *node, i int) {
	if i < 0 || i >= len(tabs.children) || i == tabs.selected {
		return
	}
	tabs.selected = i
	h.showScreens(tabs)
	h.layoutScreens(tabs)
	Invalidate(tabs.nav.bar.hwnd)
	h.emit(tabs, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}

// paintTabBar draws the tabs: under the mouse, a highlight, and under
// the selected one an accent pill (g); the titles, the selected one's in
// the text color and the others' secondary (dc); a hairline under the
// bar.
func (h *Host) paintTabBar(b *node, dc uintptr, g Graphics, r Rect) {
	tabs := b.nav.tabs
	k := h.scaleOf(b)
	p := &h.theme
	rects := h.tabRects(b)
	if g != 0 {
		for i := range tabs.children {
			if i >= len(rects) {
				break
			}
			tr := rects[i]
			if i == b.nav.hot {
				g.FillRoundRect(p.subtleHot.argb(), float32(tr.Left), float32(tr.Top), float32(tr.W()), float32(tr.H()), 4*k)
			}
			if s := tabs.children[i]; s.badge != "" {
				h.paintBadge(b, 0, g, h.tabBadge(b, s, tr), s.badge)
			}
			if i == tabs.selected {
				pw, ph := 16*k, 3*k
				cx := float32(tr.Left+tr.Right) / 2
				g.FillRoundRect(p.accent.argb(), cx-pw/2, float32(tr.Bottom)-ph, pw, ph, ph/2)
				if b.focused && h.keyboardCues(b.hwnd) {
					g.StrokeRoundRect(p.label.argb(), 2*k, float32(tr.Left), float32(tr.Top), float32(tr.W()), float32(tr.H()), 4*k)
				}
			}
		}
		br := SolidBrush(p.separator.argb())
		g.FillRect(br, 0, float32(r.H())-max(k, 1), float32(r.W()), max(k, 1))
		br.Close()
		return
	}
	font := h.bodyFont(b)
	SetBkMode(dc, TRANSPARENT)
	rtl := h.isRTL(b)
	for i, s := range tabs.children {
		if i >= len(rects) {
			break
		}
		tr := rects[i]
		color := p.secondary
		if i == tabs.selected {
			color = p.label
		}
		SetTextColor(dc, color.over(p.bg).ref())
		x := tr.Left + h.px(b, 12)
		if glyph := directedGlyph(s.icon, rtl); glyph != 0 {
			old := SelectObject(dc, h.iconFont(b, 16*h.fontScale()))
			gr := Rect{Left: x, Top: tr.Top, Right: x + h.px(b, 16), Bottom: tr.Bottom}
			if rtl {
				gr = flipIn(gr, tr)
			}
			DrawText(dc, []uint16{glyph}, &gr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
			SelectObject(dc, old)
			x += h.px(b, 16+8)
		}
		old := SelectObject(dc, font)
		lr := Rect{Left: x, Top: tr.Top, Right: tr.Right, Bottom: tr.Bottom}
		if rtl {
			lr = flipIn(lr, tr)
		}
		DrawText(dc, UTF16Units(s.title), &lr, DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|startSide(rtl))
		SelectObject(dc, old)
		if s.badge != "" {
			h.paintBadge(b, dc, 0, h.tabBadge(b, s, tr), s.badge)
		}
	}
}

// tabBadge is the place of a tab's badge: at its trailing end, inside its
// padding, after its title (WinUI's InfoBadge on a NavigationViewItem at
// the top).
func (h *Host) tabBadge(b, s *node, tr Rect) Rect {
	rtl := h.isRTL(b)
	end := tr.Right - h.px(b, 12)
	if rtl {
		end = tr.Left + h.px(b, 12)
	}
	return h.badgeAt(b, s.badge, end, tr.Top, tr.Bottom, rtl)
}

// Sheets.

// present shows a sheet or an alert.
func (h *Host) present(n *node) {
	switch n.kind {
	case ops.KindSheet:
		if n.nav.window != 0 {
			return
		}
		if h.presentOn(n.nav.on) == 0 {
			// Its Window opens once the batch is applied (a child
			// Window, after its parent): the sheet goes on it then.
			id := n.id
			h.afterCore(func() {
				if p := h.nodes[id]; p != nil && !p.dying && p.nav.window == 0 {
					h.openSheet(p)
				}
			})
			return
		}
		h.openSheet(n)
	case ops.KindWindow:
		// A child of a Window comes before its parent in the batch that
		// opens them both (the core presents a Window once its content,
		// the child among it, is built): it opens once the batch is
		// applied, owned by its parent's window.
		if p := h.nodes[n.nav.cfg.Parent]; n.nav.cfg.Flags&(ops.WindowChild|ops.WindowFlyout) != 0 && p != nil && p.kind == ops.KindWindow && p.nav.window == 0 {
			id := n.id
			h.afterCore(func() {
				if n := h.nodes[id]; n != nil && !n.dying {
					h.openWindow(n)
				}
			})
			return
		}
		h.openWindow(n)
	case ops.KindPopover:
		id := n.id
		h.afterCore(func() { // its anchor, made with it, is placed once the batch is
			if p := h.nodes[id]; p != nil && !p.dying {
				h.openPopover(p)
			}
		})
	case ops.KindAlert:
		id := n.id
		h.afterCore(func() { h.showAlert(id) })
	case ops.KindSnackbar:
		id := n.id
		h.afterCore(func() { // its window, opened in the same batch, is shown once it's applied
			if s := h.nodes[id]; s != nil && !s.dying {
				h.presentSnack(s)
			}
		})
	}
}

// presentOn is the window a sheet or an alert declared in window node
// id (its Window prop) goes on: the main window's for the root, a Window
// node's own; over that, the top sheet shown on it, which is what takes
// the user's input there. 0: a Window not open yet. An ID of no Window
// (the root's, none): the main window.
func (h *Host) presentOn(id ops.NodeID) uintptr {
	w := h.window
	if t := h.nodes[id]; t != nil && t.kind == ops.KindWindow && !t.dying {
		if w = t.nav.window; w == 0 {
			return 0
		}
	}
	return h.topSheet(w)
}

// openSheet shows a sheet as a modal window centered over the window
// it's declared in (presentOn: the main one, a Window, or the sheet
// shown over it): as wide as fits (320 to 560 points), as tall as it
// less a margin, or half of it for a medium sheet.
func (h *Host) openSheet(n *node) {
	owner := h.presentOn(n.nav.on)
	if owner == 0 {
		owner = h.window // its Window never opened
	}
	dpi := DPI(owner)
	k := dpiScale(dpi)
	cr := ClientRect(owner)
	cw, ch := float32(cr.W())/k, float32(cr.H())/k
	w := min(max(cw-80, 320), 560)
	ht := max(ch-100, 240)
	if n.detents == 1 { // medium only
		ht = max(ch/2, 240)
	}
	style := uint32(WS_POPUP | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_CLIPCHILDREN)
	r := frameRect(w, ht, style, dpi)
	wr := WindowRect(owner)
	x := wr.Left + (wr.W()-r.W())/2
	y := wr.Top + (wr.H()-r.H())/2
	if n.nav.full {
		// No frame: it's its window's content, in pixels where that is.
		style = WS_POPUP | WS_CLIPCHILDREN
		r = h.coverRect(owner)
		x, y = r.Left, r.Top
	}
	win := CreateWindow(h.topLevelStyle(), classWindow, h.appName, style, x, y, r.W(), r.H(), owner, 0)
	if d := DPI(win); d != dpi && !n.nav.full {
		// It opened on a monitor of another DPI.
		dpi, k = d, dpiScale(d)
		r = frameRect(w, ht, style, dpi)
		SetWindowPos(win, 0, 0, 0, r.W(), r.H(), SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE)
	}
	sw := newNode(0, kindSheetWindow)
	sw.hwnd = win
	sw.nav = &nav{}
	sw.parent = n
	sw.k = k
	h.byHwnd[win] = sw
	h.acceptDrops(win)
	SetDarkTitleBar(win, h.dark)
	n.nav.window, n.nav.owner = win, owner
	n.presented = true
	if n.nav.full {
		SetCornerPreference(win, dwmwcpDoNotRound)
		h.roundCover(n)
	}
	SetParent(n.hwnd, win)
	h.rescale(n.hwnd, k)
	cr = ClientRect(win)
	SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.sheets = append(h.sheets, n)
	h.reportScreen(n, h.pt(n, cr.W()), h.pt(n, cr.H()))
	EnableWindow(owner, false)
	if snapshotting() {
		ShowWindow(win, SW_SHOWNOACTIVATE)
	} else {
		ShowWindow(win, SW_SHOW)
	}
}

func (h *Host) sheetMessage(sw *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	n := sw.parent
	switch m {
	case WM_MOVE, WM_WINDOWPOSCHANGED:
		h.fitCovers(sw.hwnd)
	}
	switch m {
	case WM_SIZE:
		h.fitCovers(sw.hwnd)
		if n != nil && n.nav.full {
			h.roundCover(n)
		}
		if n != nil && n.hwnd != 0 {
			cr := ClientRect(sw.hwnd)
			SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
			h.reportScreen(n, h.pt(n, cr.W()), h.pt(n, cr.H()))
		}
		h.placeSnacks()
		return 0, true
	case WM_ERASEBKGND:
		FillColor(wp, ClientRect(sw.hwnd), h.theme.bg.ref())
		return 1, true
	case WM_PRINTCLIENT:
		FillColor(wp, ClientRect(sw.hwnd), h.theme.bg.ref())
		return 0, true
	case WM_CLOSE:
		h.cancelSheet(sw)
		return 0, true
	case WM_DPICHANGED:
		r := *Native[Rect](lp)
		if n != nil && n.nav.full {
			r = h.coverRect(n.nav.owner) // where its window's content is, not where Windows suggests
		}
		h.windowDPIChanged(sw, int(LoWord(wp)), r)
		return 0, true
	case WM_ACTIVATE:
		if LoWord(wp) != 0 {
			h.inUse = sw.hwnd
		}
		return 0, false
	}
	return 0, false
}

// dwmwcpDoNotRound is DWMWA_WINDOW_CORNER_PREFERENCE's square corners:
// a full-screen sheet's top meets its window's title bar.
const dwmwcpDoNotRound = 1

// coverRect is where a full-screen sheet over owner goes, in screen
// pixels: all of owner's client area under its title bar (a hidden
// title bar's strip, with its buttons, stays the owner's). The client
// area's corners in either direction: a mirrored window's origin is on
// the right.
func (h *Host) coverRect(owner uintptr) Rect {
	cr := ClientRect(owner)
	top := int32(0)
	if c := h.ctls[owner]; c != nil {
		top = int32(math.Round(float64(c.safeTop() * dpiScale(DPI(owner)))))
	}
	a := ClientToScreen(owner, Point{X: 0, Y: min(top, cr.H())})
	b := ClientToScreen(owner, Point{X: cr.W(), Y: cr.H()})
	return Rect{Left: min(a.X, b.X), Top: min(a.Y, b.Y), Right: max(a.X, b.X), Bottom: max(a.Y, b.Y)}
}

// fitCovers puts the full-screen sheets over owner where its content is
// again (it moved, it resized, it came back from being minimized).
func (h *Host) fitCovers(owner uintptr) {
	if owner == 0 || IsIconic(owner) {
		return
	}
	for _, s := range h.sheets {
		if !s.nav.full || s.nav.owner != owner || s.nav.window == 0 {
			continue
		}
		r := h.coverRect(owner)
		if WindowRect(s.nav.window) != r {
			SetWindowPos(s.nav.window, 0, r.Left, r.Top, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE)
		}
	}
}

// roundCovers rounds the full-screen sheets over owner again (it was
// maximized, or restored).
func (h *Host) roundCovers(owner uintptr) {
	for _, s := range h.sheets {
		if s.nav.full && s.nav.owner == owner {
			h.roundCover(s)
		}
	}
}

// roundCover gives a full-screen sheet its window's bottom corners:
// Windows 11 rounds a window's, which a rectangle over its content would
// stick out of; none for a maximized or full-screen one. A rounded
// rectangle whose top corners are above it, out of its region.
func (h *Host) roundCover(n *node) {
	w, owner := n.nav.window, n.nav.owner
	if w == 0 {
		return
	}
	c := h.ctls[owner]
	if !Windows11() || IsZoomed(owner) || c != nil && c.full != nil {
		SetWindowRgn(w, 0)
		return
	}
	r := WindowRect(w)
	d := int32(math.Round(16 * float64(dpiScale(DPI(w))))) // an 8 point radius, as DWM's
	SetWindowRgn(w, CreateRoundRectRgn(0, -d, r.W()+1, r.H()+1, d, d))
}

// cancelSheet is the user dismissing a sheet (its close button,
// Escape): Dismissed, and the core destroys it.
func (h *Host) cancelSheet(sw *node) {
	if n := sw.parent; n != nil && !n.dying {
		if n.interceptBack {
			h.emit(n, ops.Event{Kind: ops.EventBack})
			return
		}
		h.emit(n, ops.Event{Kind: ops.EventDismissed})
	}
}

func (h *Host) closeSheet(n *node) {
	win := n.nav.window
	if win == 0 {
		return
	}
	for i, s := range h.sheets {
		if s == n {
			h.sheets = append(h.sheets[:i], h.sheets[i+1:]...)
			break
		}
	}
	// The owner comes back before the sheet goes, so it's activated.
	EnableWindow(n.nav.owner, true)
	SetParent(n.hwnd, h.park)
	h.forgetTree(win)
	DestroyWindow(win)
	n.nav.window = 0
}

// taskDialog runs a task dialog (a test's own, which doesn't wait).
var taskDialog = TaskDialog

// showAlert shows an alert as a task dialog and reports the button
// chosen. It's a modal loop, so it runs once the core has returned.
func (h *Host) showAlert(id ops.NodeID) {
	n := h.nodes[id]
	if n == nil || n.dying {
		return
	}
	if len(n.nav.fields) > 0 && n.alertStyle == int32(ops.AlertStyleAlert) {
		h.openAlertDialog(n) // a task dialog holds no fields
		return
	}
	var buttons []TaskButton
	cancel := -1
	for i, a := range n.actions {
		if a.role == ops.AlertButtonRoleCancel {
			cancel = i
		}
		buttons = append(buttons, TaskButton{ID: alertButtonID(i, a), Title: a.title, Disabled: a.is(ops.MenuDisabled)})
	}
	flags := uint32(TDF_SIZE_TO_CONTENT)
	if cancel >= 0 {
		flags |= TDF_ALLOW_DIALOG_CANCELLATION
	}
	if n.alertStyle == int32(ops.AlertStyleActionSheet) {
		flags |= TDF_USE_COMMAND_LINKS
	}
	if len(buttons) == 0 {
		buttons = []TaskButton{{ID: 100, Title: l10n.OK(h.locale)}}
	}
	if h.rtl {
		flags |= TDF_RTL_LAYOUT
	}
	owner := h.presentOn(n.nav.on) // the window it's declared in
	if owner == 0 {
		owner = h.window
	}
	title, content := n.title, n.message
	h.modal++
	got := taskDialog(owner, h.appName, title, content, buttons, buttons[0].ID, flags, func(hwnd uintptr) {
		if n := h.nodes[id]; n != nil {
			n.nav.dialog = hwnd
		}
	})
	h.modal--
	n = h.nodes[id]
	if n == nil || n.dying {
		return
	}
	switch {
	case got == IDCANCEL && cancel >= 0:
		h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(cancel)})
	case got >= 100 && got-100 < len(n.actions):
		h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(got - 100)})
	default:
		h.emit(n, ops.Event{Kind: ops.EventDismissed})
	}
}

// alertButtonID is the task dialog button's ID of an alert's button i:
// the cancel one's IDCANCEL, so Escape and the close button take it.
func alertButtonID(i int, a action) int {
	if a.role == ops.AlertButtonRoleCancel {
		return IDCANCEL
	}
	return 100 + i
}

// titleSpec is a header's title's font: Title2, in its window.
func (h *Host) titleSpec(b *node) fontSpec {
	s := h.spec(int32(ops.TextStyleTitle2), 0, 0, "")
	s.k = h.scaleOf(b)
	return s
}

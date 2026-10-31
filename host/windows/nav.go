//go:build windows && amd64

package windows

import (
	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// Navigation on Windows:
//
//   - A Stack shows its top Screen under a header bar of its own: a back
//     button (Alt+Left too) while there's a screen to go back to, the
//     screen's title, and its bar buttons on the right.
//   - Tabs are a bar of tabs on top (as WinUI's NavigationView at the
//     top: an icon and a title each, an accent pill under the selected
//     one), the selected page under it.
//   - A Sheet is a modal window over the app's (its owner is disabled
//     meanwhile); closing it or Escape dismisses it.
//   - An Alert is a task dialog (TaskDialogIndirect): an action sheet's
//     buttons are command links.
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

	// Stack, Tabs: the bar above the screens.
	bar *node

	// A stack's header.
	stack   *node
	back    uintptr
	buttons []uintptr // the screen's bar buttons
	screen  *node     // the screen whose title and buttons it shows

	// A tab bar.
	tabs *node
	hot  int

	// Sheet.
	window uintptr
	owner  uintptr
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
	case ops.KindSheet:
		n.hwnd = h.makeView(0)
		n.nav = &nav{}
	case ops.KindAlert:
		n.nav = &nav{}
	default:
		return false
	}
	return true
}

// sizedByHost reports whether the host sizes n (a screen, a sheet's
// content), not the core.
func (nv *nav) sizedByHost(n *node) bool {
	return n.kind == ops.KindScreen || n.kind == ops.KindSheet
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
	case ops.KindSheet:
		n.dying = true
		h.closeSheet(n)
		for _, c := range n.children {
			c.parent = nil
		}
		h.release(n)
		return true
	case ops.KindAlert:
		n.dying = true
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
	bar := h.px(h.barHeight(n))
	if b := n.nav.bar; b != nil && b.hwnd != 0 {
		SetWindowPos(b.hwnd, HWND_TOP, 0, 0, r.W(), bar, SWP_NOACTIVATE|SWP_NOCOPYBITS)
		if n.kind == ops.KindStack {
			h.layoutHeader(b)
		}
	}
	for _, c := range n.children {
		if c.hwnd == 0 {
			continue
		}
		SetWindowPos(c.hwnd, 0, 0, bar, r.W(), max(r.H()-bar, 0), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		h.reportScreen(c, h.pt(r.W()), h.pt(max(r.H()-bar, 0)))
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
				r := ClientRect(n.nav.window)
				h.reportScreen(n, h.pt(r.W()), h.pt(r.H()))
			}
		}
	}
}

// reportScreen sends a screen's or sheet's environment if it changed.
func (h *Host) reportScreen(n *node, w, ht float32) {
	if n.nav == nil || w <= 0 {
		return
	}
	env := h.env
	env.Width, env.Height = w, ht
	env.SafeTop, env.SafeRight, env.SafeBottom, env.SafeLeft = 0, 0, 0, 0
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
		case ops.PropScreenBackGesture:
			n.nav.backGesture = v.Int != 0
		case ops.PropScreenIcon:
			n.icon = v.Str
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

// A stack's header.

func (h *Host) makeHeader(stack *node) *node {
	h.nextCell++
	b := newNode(ops.HostIDBit|h.nextCell, kindHeader)
	b.hwnd = h.makeView(0)
	b.nav = &nav{stack: stack}
	SetParent(b.hwnd, stack.hwnd)
	b.nav.back = CreateWindow(0, "BUTTON", "Back", WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, b.hwnd, 1)
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
	return s.title != "" || len(s.actions) > 0 || h.liveScreens(stack) > 1
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
		Invalidate(b.nav.buttons[i])
	}
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
// on the right, 40 points square, or as wide as their titles.
func (h *Host) layoutHeader(b *node) {
	r := ClientRect(b.hwnd)
	size := h.px(36)
	y := (r.H() - size) / 2
	SetWindowPos(b.nav.back, 0, h.px(6), y, h.px(40), size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	x := r.W() - h.px(8)
	screen := b.nav.screen
	for i := len(b.nav.buttons) - 1; i >= 0; i-- {
		w := h.px(40)
		if screen != nil && i < len(screen.actions) && symbolGlyph(screen.actions[i].icon) == 0 {
			dc := h.measureDC
			old := SelectObject(dc, h.bodyFont())
			tw, _ := TextExtent(dc, UTF16Units(screen.actions[i].title))
			SelectObject(dc, old)
			w = tw + h.px(24)
		}
		x -= w
		SetWindowPos(b.nav.buttons[i], 0, x, y, w, size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		x -= h.px(4)
	}
}

// headerButton is what a header's button shows: the back arrow, or a
// bar button's icon (Segoe Fluent Icons) or title.
func (nv *nav) headerButton(ctl uintptr) (uint16, string) {
	if ctl == nv.back {
		return 0xE72B, "" // Back
	}
	for i, b := range nv.buttons {
		if b == ctl && nv.screen != nil && i < len(nv.screen.actions) {
			a := nv.screen.actions[i]
			if g := symbolGlyph(a.icon); g != 0 {
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
	for i, btn := range b.nav.buttons {
		if btn == ctl && b.nav.screen != nil {
			h.emit(b.nav.screen, ops.Event{Kind: ops.EventAction, Int: int64(i)})
		}
	}
}

// paintHeader draws a hairline under the bar (g), and the screen's
// title beside the back button in Fluent's Subtitle (20, semibold) (dc).
func (h *Host) paintHeader(b *node, dc uintptr, g Graphics, r Rect) {
	stack := b.nav.stack
	screen := b.nav.screen
	k := h.scale
	if g != 0 {
		br := SolidBrush(h.theme.separator.argb())
		g.FillRect(br, 0, float32(r.H())-max(k, 1), float32(r.W()), max(k, 1))
		br.Close()
		return
	}
	if screen == nil || screen.title == "" {
		return
	}
	x := h.px(16)
	if h.liveScreens(stack) > 1 {
		x = h.px(52)
	}
	old := SelectObject(dc, h.font(h.spec(int32(ops.TextStyleTitle2), 0, 0, "")))
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, h.theme.label.ref())
	right := r.Right - h.px(16)
	for _, btn := range b.nav.buttons {
		right = min(right, WindowRectIn(btn, b.hwnd).Left-h.px(8))
	}
	tr := Rect{Left: x, Top: 0, Right: right, Bottom: r.Bottom}
	DrawText(dc, UTF16Units(screen.title), &tr, DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS)
	SelectObject(dc, old)
}

// popStack is the back button: the stack's top screen is popped (the
// core destroys it, told with Popped).
func (h *Host) popStack(stack *node) {
	top := h.topScreen(stack)
	if top == nil || h.liveScreens(stack) < 2 {
		return
	}
	top.dying = true
	h.showScreens(stack)
	h.updateHeader(stack)
	h.emit(stack, ops.Event{Kind: ops.EventPopped, Int: int64(top.id)})
}

// back pops the innermost shown stack that can pop (Alt+Left).
func (h *Host) back() {
	if s := h.activeStack(); s != nil {
		h.popStack(s)
	}
}

// activeStack finds the innermost stack on screen that can pop: through
// each stack's top screen and each tabs' selected page.
func (h *Host) activeStack() *node {
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
		case ops.KindSheet, ops.KindAlert:
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	if len(h.sheets) > 0 {
		walk(h.sheets[len(h.sheets)-1])
	} else if h.root != nil {
		walk(h.root)
	}
	return found
}

// A tab bar.

func (h *Host) makeTabBar(tabs *node) *node {
	h.nextCell++
	b := newNode(ops.HostIDBit|h.nextCell, kindTabBar)
	b.hwnd = h.makeView(WS_TABSTOP)
	b.nav = &nav{tabs: tabs, hot: -1}
	b.host = tabBarMessage
	SetParent(b.hwnd, tabs.hwnd)
	h.add(b)
	return b
}

// tabRects are the tabs' places in the bar: each its icon and title,
// padded, from the left.
func (h *Host) tabRects(b *node) []Rect {
	tabs := b.nav.tabs
	r := ClientRect(b.hwnd)
	dc := h.measureDC
	old := SelectObject(dc, h.bodyFont())
	defer SelectObject(dc, old)
	var out []Rect
	x := h.px(8)
	for _, s := range tabs.children {
		tw, _ := TextExtent(dc, UTF16Units(s.title))
		w := tw + h.px(24)
		if symbolGlyph(s.icon) != 0 {
			w += h.px(16 + 8)
		}
		out = append(out, Rect{Left: x, Top: h.px(6), Right: x + w, Bottom: r.H() - h.px(6)})
		x += w + h.px(4)
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
		switch wp {
		case VK_LEFT:
			h.selectTab(tabs, tabs.selected-1)
			return 0, true
		case VK_RIGHT:
			h.selectTab(tabs, tabs.selected+1)
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
	k := h.scale
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
	font := h.bodyFont()
	SetBkMode(dc, TRANSPARENT)
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
		x := tr.Left + h.px(12)
		if glyph := symbolGlyph(s.icon); glyph != 0 {
			old := SelectObject(dc, h.iconFont(16*h.fontScale()))
			gr := Rect{Left: x, Top: tr.Top, Right: x + h.px(16), Bottom: tr.Bottom}
			DrawText(dc, []uint16{glyph}, &gr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
			SelectObject(dc, old)
			x += h.px(16 + 8)
		}
		old := SelectObject(dc, font)
		lr := Rect{Left: x, Top: tr.Top, Right: tr.Right, Bottom: tr.Bottom}
		DrawText(dc, UTF16Units(s.title), &lr, DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		SelectObject(dc, old)
	}
}

// Sheets.

// present shows a sheet or an alert.
func (h *Host) present(n *node) {
	switch n.kind {
	case ops.KindSheet:
		if n.nav.window != 0 {
			return
		}
		h.openSheet(n)
	case ops.KindAlert:
		id := n.id
		h.afterCore(func() { h.showAlert(id) })
	}
}

// openSheet shows a sheet as a modal window centered over the app's:
// as wide as fits (320 to 560 points), as tall as the app's less a
// margin, or half of it for a medium sheet.
func (h *Host) openSheet(n *node) {
	owner := h.window
	if len(h.sheets) > 0 {
		owner = h.sheets[len(h.sheets)-1].nav.window
	}
	cr := ClientRect(owner)
	cw, ch := h.pt(cr.W()), h.pt(cr.H())
	w := min(max(cw-80, 320), 560)
	ht := max(ch-100, 240)
	if n.detents == 1 { // medium only
		ht = max(ch/2, 240)
	}
	style := uint32(WS_POPUP | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_CLIPCHILDREN)
	r := Rect{Right: h.px(w), Bottom: h.px(ht)}
	AdjustWindowRect(&r, style, 0, h.dpi)
	wr := WindowRect(owner)
	x := wr.Left + (wr.W()-r.W())/2
	y := wr.Top + (wr.H()-r.H())/2
	win := CreateWindow(0, classWindow, h.appName, style, x, y, r.W(), r.H(), owner, 0)
	sw := newNode(0, kindSheetWindow)
	sw.hwnd = win
	sw.nav = &nav{}
	sw.parent = n
	h.byHwnd[win] = sw
	SetDarkTitleBar(win, h.dark)
	n.nav.window, n.nav.owner = win, owner
	n.presented = true
	SetParent(n.hwnd, win)
	cr = ClientRect(win)
	SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.sheets = append(h.sheets, n)
	h.reportScreen(n, h.pt(cr.W()), h.pt(cr.H()))
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
	case WM_SIZE:
		if n != nil && n.hwnd != 0 {
			cr := ClientRect(sw.hwnd)
			SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
			h.reportScreen(n, h.pt(cr.W()), h.pt(cr.H()))
		}
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
	}
	return 0, false
}

// cancelSheet is the user dismissing a sheet (its close button,
// Escape): Dismissed, and the core destroys it.
func (h *Host) cancelSheet(sw *node) {
	if n := sw.parent; n != nil && !n.dying {
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

// showAlert shows an alert as a task dialog and reports the button
// chosen. It's a modal loop, so it runs once the core has returned.
func (h *Host) showAlert(id ops.NodeID) {
	n := h.nodes[id]
	if n == nil || n.dying {
		return
	}
	var buttons []TaskButton
	cancel := -1
	for i, a := range n.actions {
		bid := 100 + i
		if a.role == ops.AlertButtonRoleCancel {
			cancel, bid = i, IDCANCEL
		}
		buttons = append(buttons, TaskButton{ID: bid, Title: a.title})
	}
	flags := uint32(TDF_SIZE_TO_CONTENT)
	if cancel >= 0 {
		flags |= TDF_ALLOW_DIALOG_CANCELLATION
	}
	if n.alertStyle == int32(ops.AlertStyleActionSheet) {
		flags |= TDF_USE_COMMAND_LINKS
	}
	if len(buttons) == 0 {
		buttons = []TaskButton{{ID: 100, Title: "OK"}}
	}
	owner := h.window
	if len(h.sheets) > 0 {
		owner = h.sheets[len(h.sheets)-1].nav.window
	}
	title, content := n.title, n.message
	h.modal++
	got := TaskDialog(owner, h.appName, title, content, buttons, buttons[0].ID, flags)
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

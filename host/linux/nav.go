//go:build linux && !android

package linux

import (
	"strconv"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

// Navigation with libadwaita's widgets: a Stack is an AdwNavigationView
// (its pages slide, a swipe or Alt+Left goes back), each Screen a page;
// Tabs an AdwViewStack with a view switcher (in the header bar on a wide
// window, a bar at the bottom of a narrow one); a Sheet an AdwDialog (a
// floating dialog, a bottom sheet in a narrow window), a FullScreen one
// an AdwDialog as big as its window, which libadwaita shows over all of
// it as it shows a dialog that doesn't fit (on a phone, in a narrow
// window), following the window as it resizes; an Alert an
// AdwAlertDialog; a Window a window of its own.
//
// Each window has one header bar, as GNOME's apps do: the innermost stack
// on screen's back button, title, bar buttons and search field are in it
// (chrome), or the tabs' switcher, or the window's title.

type nav struct {
	view     Ptr        // a Stack's AdwNavigationView; Tabs' AdwViewStack
	page     Ptr        // a Screen's AdwNavigationPage, or its AdwViewStackPage in Tabs
	dialog   Ptr        // a Sheet's AdwDialog, an Alert's AdwAlertDialog
	window   ops.NodeID // a Sheet's or an Alert's Window: the window it's declared in (the root, a Window node)
	bar      Ptr        // Tabs' AdwViewSwitcherBar
	switcher Ptr        // Tabs' AdwViewSwitcher, for the header bar
	narrow   bool
	popping  bool // the host is popping (no Popped)
	gone     bool // the user popped or dismissed it
	search   *searchField
	pending  int // Selected, before its pages are in
	searchPh string
	fields   []ops.AlertField // an Alert's text fields
	rows     []Ptr            // their entry rows, once it shows
	searchTx string
	badge    string // a tab's (Screen's Badge)
	detents  int64
	full     bool // a Sheet's FullScreen: as big as its window
	envW     float32
	envH     float32
	// A Screen's Toolbar and Primary (numbered as their Action events
	// are), and the bottom bar showing them (toolbar.go).
	toolbar, primary []action
	bottom           *screenBar
	toast            Ptr                  // a Snackbar's AdwToast (snackbar.go)
	duration         ops.SnackbarDuration // a Snackbar's
}

// isLayoutRoot reports whether n is a root the host sizes, whose
// Environment it reports.
func (n *node) isLayoutRoot() bool {
	switch n.kind {
	case ops.KindScreen, ops.KindSheet, ops.KindWindow, ops.KindPopover:
		return true
	}
	return false
}

// rootAllocated: a screen, sheet, window or popover was sized; the core
// lays it out at that size.
func (h *Host) rootAllocated(n *node, width, height int32) {
	if n.nav == nil {
		n.nav = &nav{}
	}
	w, ht := float32(width), float32(height)
	if n.nav.envW == w && n.nav.envH == ht {
		return
	}
	n.nav.envW, n.nav.envH = w, ht
	h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: h.rootEnv(n)})
}

// rootEnv is a host-sized root's environment: its window's, at its size.
func (h *Host) rootEnv(n *node) ops.Environment {
	env := h.env
	if w := h.winOf(n); w != nil && w != h.main {
		env = h.environment(w)
	}
	if n.nav != nil {
		env.Width, env.Height = n.nav.envW, n.nav.envH
	}
	return env
}

// rootsEnvChanged reports the environment again to every root the host
// sizes (the style or the text size changed).
func (h *Host) rootsEnvChanged() {
	for _, n := range h.nodes {
		if n.isLayoutRoot() && n.nav != nil && (n.nav.envW > 0 || n.nav.envH > 0) {
			h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: h.rootEnv(n)})
		}
	}
}

// winOf is the window a node is in.
func (h *Host) winOf(n *node) *win {
	for p := n; p != nil; p = p.parent {
		if p.win != nil {
			return p.win
		}
		if h.main != nil && p == h.main.root {
			return h.main
		}
	}
	if n.widget != 0 {
		if r := WidgetGetRoot(n.widget); r != 0 {
			if w := h.wins[r]; w != nil {
				return w
			}
		}
	}
	return h.main
}

func (h *Host) createNav(n *node) bool {
	switch n.kind {
	case ops.KindStack:
		v := AdwNavigationViewNew()
		n.nav = &nav{view: v}
		h.setInner(n, v)
		Connect(v, "popped", func(page uintptr) { h.pagePopped(n, page) })
		Connect(v, "notify::visible-page", func(uintptr) { h.queueChrome() })
	case ops.KindScreen:
		n.nav = &nav{}
	case ops.KindTabs:
		box := BoxNew(OrientationVertical, 0)
		stack := AdwViewStackNew()
		WidgetSetVExpand(stack, true)
		bar := AdwViewSwitcherBarNew()
		AdwViewSwitcherBarSetStack(bar, stack)
		BoxAppend(box, stack)
		BoxAppend(box, bar)
		sw := AdwViewSwitcherNew()
		ObjectRefSink(sw)
		AdwViewSwitcherSetStack(sw, stack)
		AdwViewSwitcherSetPolicy(sw, 1) // wide
		n.nav = &nav{view: stack, bar: bar, switcher: sw}
		h.setInner(n, box)
		Connect(stack, "notify::visible-child", func(uintptr) {
			if n.updating {
				return
			}
			i := h.tabIndex(n, AdwViewStackGetVisibleChild(stack))
			if i >= 0 && i != n.selected {
				n.selected = i
				h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
			}
			h.queueChrome()
		})
	case ops.KindSheet:
		d := AdwDialogNew()
		ObjectRefSink(d)
		n.nav = &nav{dialog: d}
		AdwDialogSetChild(d, n.widget)
		Connect(d, "closed", func() {
			if n.nav.popping {
				return
			}
			n.nav.gone = true
			h.emit(n, ops.Event{Kind: ops.EventDismissed})
		})
		Connect(d, "close-attempt", func() {
			h.emit(n, ops.Event{Kind: ops.EventBack})
		})
	case ops.KindAlert, ops.KindSnackbar:
		n.nav = &nav{}
	case ops.KindWindow:
		w := h.newWin(AdwWindowNew())
		w.node = n
		n.win = w
		n.nav = &nav{}
		w.setRoot(h, n)
		Connect(w.window, "close-request", func() bool {
			if w.closing {
				return false
			}
			// The core closes it, or asks the app first (CloseRequest).
			if n.listening(ops.EventCloseRequest) {
				h.emit(n, ops.Event{Kind: ops.EventCloseRequest})
			} else {
				h.emit(n, ops.Event{Kind: ops.EventDismissed})
			}
			return true
		})
	default:
		return false
	}
	return true
}

func (h *Host) setNavProp(n *node, p ops.PropID, v *ops.Value) bool {
	switch n.kind {
	case ops.KindScreen:
		switch p {
		case ops.PropScreenTitle:
			n.title = v.Str
			if n.nav.page != 0 {
				if n.parent != nil && n.parent.kind == ops.KindTabs {
					AdwViewStackPageSetTitle(n.nav.page, v.Str)
					h.labelTabs(n.parent)
				} else {
					AdwNavigationPageSetTitle(n.nav.page, v.Str)
				}
			}
		case ops.PropScreenActions:
			n.actions = decodeMenu(v.Bytes)
		case ops.PropScreenToolbar:
			n.nav.toolbar = decodeMenuFrom(v.Bytes, ops.ScreenToolbarActions)
			h.setScreenBar(n)
		case ops.PropScreenPrimary:
			n.nav.primary = decodeMenuFrom(v.Bytes, ops.ScreenPrimaryAction)
			h.setScreenBar(n)
		case ops.PropScreenIcon:
			n.icon = v.Str
			if n.nav.page != 0 && n.parent != nil && n.parent.kind == ops.KindTabs {
				AdwViewStackPageSetIconName(n.nav.page, h.iconName(v.Str))
			}
		case ops.PropScreenBadge:
			n.nav.badge = v.Str
			if n.nav.page != 0 && n.parent != nil && n.parent.kind == ops.KindTabs {
				h.showTabBadge(n)
				h.labelTabs(n.parent)
			}
		case ops.PropScreenSearch:
			n.nav.searchPh = v.Str
		case ops.PropScreenSearchText:
			n.nav.searchTx = v.Str
			if s := n.nav.search; s != nil {
				s.setText(v.Str)
			}
		case ops.PropScreenBackGesture:
			if n.nav.page != 0 && n.parent != nil && n.parent.kind == ops.KindStack {
				AdwNavigationPageSetCanPop(n.nav.page, v.Int != 0 && !n.interceptBack)
			}
		}
		h.queueChrome()
	case ops.KindTabs:
		if p == ops.PropTabsSelected {
			n.selected = int(v.Int)
			h.selectTab(n)
		}
	case ops.KindSheet:
		switch p {
		case ops.PropSheetDetents:
			n.nav.detents = v.Int
			h.sizeSheet(n)
		case ops.PropSheetWindow:
			n.nav.window = ops.NodeID(v.Int)
		case ops.PropSheetFullScreen:
			n.nav.full = v.Int != 0
			if n.nav.full {
				// Floating, so a narrow window doesn't make it a bottom
				// sheet, which shows the window above it.
				AdwDialogSetPresentationMode(n.nav.dialog, dialogFloating)
			}
			h.sizeSheet(n)
		}
	case ops.KindAlert:
		switch p {
		case ops.PropAlertTitle:
			n.title = v.Str
		case ops.PropAlertMessage:
			n.message = v.Str
		case ops.PropAlertButtons:
			n.actions = decodeMenu(v.Bytes)
			if d := n.nav.dialog; d != 0 && !n.nav.gone {
				// Showing: its buttons follow their titles and Disabled.
				for i, a := range n.actions {
					id := strconv.Itoa(i)
					AdwAlertDialogSetResponseLabel(d, id, a.title)
					AdwAlertDialogSetResponseEnabled(d, id, !a.is(ops.MenuDisabled))
				}
			}
		case ops.PropAlertStyle:
			n.alertStyle = int32(v.Int)
		case ops.PropAlertWindow:
			n.nav.window = ops.NodeID(v.Int)
		case ops.PropAlertFields:
			n.nav.fields = ops.DecodeAlertFields(v.Bytes)
		}
	case ops.KindSnackbar:
		h.setSnackProp(n, p, v)
	case ops.KindWindow:
		w := n.win
		switch p {
		case ops.PropWindowTitle:
			w.name = v.Str
			h.updateTitle(w)
		case ops.PropWindowConfig:
			h.configure(w, ops.DecodeWindowConfig(v.Bytes))
		case ops.PropWindowFlyout:
			h.setFlyout(w, v.Bytes)
		case ops.PropWindowEdited:
			w.edited = v.Int != 0
			h.updateTitle(w)
		case ops.PropWindowFile:
			w.file = v.Str
			h.updateTitle(w)
		}
	default:
		return false
	}
	return true
}

// insertNav puts a screen in its stack (a push) or tabs, a sheet's
// content in it.
func (h *Host) insertNav(p, c *node, index int) bool {
	switch p.kind {
	case ops.KindSplit:
		h.insertSplit(p, c, index)
		return true
	case ops.KindStack:
		h.addChild(p, c, index)
		title := c.title
		page := AdwNavigationPageNew(c.pageChild(), title)
		c.nav.page = page
		AdwNavigationPageSetTag(page, strconv.FormatUint(uint64(c.id), 10))
		if c.interceptBack {
			AdwNavigationPageSetCanPop(page, false)
		}
		if AdwNavigationViewGetVisiblePage(p.nav.view) == 0 || h.replacing(p) {
			// The first screen, or a Replace: at once.
			h.replacePages(p)
		} else {
			AdwNavigationViewPush(p.nav.view, page)
		}
		h.queueChrome()
		return true
	case ops.KindTabs:
		h.addChild(p, c, index)
		name := strconv.FormatUint(uint64(c.id), 10)
		page := AdwViewStackAddTitledWithIcon(p.nav.view, c.widget, name, c.title, h.iconName(c.icon))
		c.nav.page = page
		if c.nav.badge != "" {
			h.showTabBadge(c)
		}
		h.labelTabs(p)
		h.selectTab(p)
		h.queueChrome()
		return true
	}
	return false
}

// addChild records c as p's child at index.
func (h *Host) addChild(p, c *node, index int) {
	i := min(index, len(p.children))
	p.children = append(p.children, nil)
	copy(p.children[i+1:], p.children[i:])
	p.children[i] = c
	c.parent = p
}

func (h *Host) replacing(p *node) bool { return p.nav.pending == -2 }

// replacePages makes the stack's pages its screens, at once.
func (h *Host) replacePages(p *node) {
	pages := make([]Ptr, 0, len(p.children))
	for _, c := range p.children {
		if c.nav != nil && c.nav.page != 0 && !c.nav.gone {
			pages = append(pages, c.nav.page)
		}
	}
	p.nav.popping = true
	if len(pages) > 0 {
		AdwNavigationViewReplace(p.nav.view, &pages[0], int32(len(pages)))
	} else {
		AdwNavigationViewReplace(p.nav.view, nil, 0)
	}
	p.nav.popping = false
	p.nav.pending = 0
}

func (h *Host) removeNav(p, c *node) bool {
	switch p.kind {
	case ops.KindSplit:
		h.removeSplit(p, c)
		return true
	case ops.KindStack:
		h.dropPage(p, c)
		return true
	case ops.KindTabs:
		if c.nav != nil && c.nav.page != 0 {
			AdwViewStackRemove(p.nav.view, c.widget)
			c.nav.page = 0
			h.labelTabs(p)
		}
		h.queueChrome()
		return true
	}
	return false
}

// showTabBadge shows a tab's badge on its page, which the view switchers
// show: a count as its badge-number, beside the page's icon, other text
// as needs-attention's dot; either way the page needs attention, so the
// badge takes the accent color, as GNOME's apps count unread items.
func (h *Host) showTabBadge(n *node) {
	num, isNum := ops.BadgeNumber(n.nav.badge)
	if !isNum || num < 0 {
		num = 0
	}
	AdwViewStackPageSetBadgeNumber(n.nav.page, uint32(num))
	AdwViewStackPageSetNeedsAttention(n.nav.page, n.nav.badge != "")
}

// labelTabs labels the view switchers' buttons, a tab's each, for a
// screen reader: its title and its badge, a count as so many items
// ("Inbox, 3 items"). The switchers' own labels say the title alone.
func (h *Host) labelTabs(tabs *node) {
	if tabs.nav == nil {
		return
	}
	toggle := Call(Sym("gtk_toggle_button_get_type"))
	for _, sw := range []Ptr{tabs.nav.switcher, tabs.nav.bar} {
		var buttons []Ptr
		var walk func(w Ptr)
		walk = func(w Ptr) {
			for c := WidgetGetFirstChild(w); c != 0; c = WidgetGetNextSibling(c) {
				if TypeCheckInstanceIsA(c, toggle) {
					buttons = append(buttons, c)
					continue
				}
				walk(c)
			}
		}
		if sw != 0 {
			walk(sw)
		}
		var pages []*node
		for _, c := range tabs.children {
			if c.nav != nil && c.nav.page != 0 {
				pages = append(pages, c)
			}
		}
		if len(buttons) != len(pages) {
			continue // the switcher makes its buttons as it's shown
		}
		for i, b := range buttons {
			setA11yString(b, a11yPropLabel, l10n.Badged(h.locale, pages[i].title, pages[i].nav.badge))
		}
	}
}

// dropPage takes a screen's page out of its stack: popped (animated) when
// it's the top, else at once.
func (h *Host) dropPage(p, c *node) {
	if c.nav == nil || c.nav.page == 0 {
		return
	}
	page := c.nav.page
	c.nav.page = 0
	if c.nav.gone {
		return // the user popped it
	}
	if AdwNavigationViewGetVisiblePage(p.nav.view) == page {
		p.nav.popping = true
		AdwNavigationViewPop(p.nav.view)
		p.nav.popping = false
	} else {
		h.replacePages(p)
	}
	h.queueChrome()
}

// pagePopped: the user went back (the back button, a swipe, Escape), or
// the host popped.
func (h *Host) pagePopped(stack *node, page Ptr) {
	h.queueChrome()
	if stack.nav.popping {
		return
	}
	for _, c := range stack.children {
		if c.nav != nil && c.nav.page == page {
			c.nav.gone = true
			h.emit(stack, ops.Event{Kind: ops.EventPopped, Int: int64(c.id)})
			return
		}
	}
}

// destroyNav handles the destruction of a navigation node: a sheet or
// alert dismissed, a window closed.
func (h *Host) destroyNav(n *node) bool {
	switch n.kind {
	case ops.KindSheet:
		if d := n.nav.dialog; d != 0 {
			n.nav.popping = true
			if !n.nav.gone {
				AdwDialogForceClose(d)
			}
			AdwDialogSetChild(d, 0)
			ObjectUnref(d)
			n.nav.dialog = 0
		}
	case ops.KindAlert:
		if d := n.nav.dialog; d != 0 {
			n.nav.popping = true
			if !n.nav.gone {
				AdwDialogForceClose(d)
			}
			n.nav.dialog = 0
		}
	case ops.KindSnackbar:
		h.destroySnack(n)
	case ops.KindWindow:
		if w := n.win; w != nil {
			h.saveWindow(w)
			h.dropWindowCSS(w)
			w.closing = true
			h.dropDesk(w)
			if w.root != nil && WidgetGetParent(w.root.widget) == w.content {
				WidgetUnparent(w.root.widget)
			}
			delete(h.wins, w.window)
			WindowDestroy(w.window)
			if h.activeWin == w {
				h.activeWin = nil
			}
			n.win = nil
		}
	case ops.KindTabs:
		if n.nav.switcher != 0 {
			ObjectUnref(n.nav.switcher)
			n.nav.switcher = 0
		}
	}
	if n.kind == ops.KindScreen && n.parent != nil && n.parent.kind == ops.KindStack {
		p := n.parent
		h.dropPage(p, n)
		for i, x := range p.children {
			if x == n {
				p.children = append(p.children[:i], p.children[i+1:]...)
				break
			}
		}
		n.parent = nil
		for _, c := range n.children {
			c.parent = nil
		}
		h.releaseScreenBar(n)
		h.release(n)
		return true
	}
	return false
}

// present shows a sheet or alert over the window it's declared in (its
// Window prop), or opens a Window or a popover.
func (h *Host) present(n *node) {
	switch n.kind {
	case ops.KindSheet:
		h.sizeSheet(n)
		AdwDialogPresent(n.nav.dialog, h.declaredWin(n).content)
	case ops.KindAlert:
		h.presentAlert(n, h.declaredWin(n).content)
	case ops.KindSnackbar:
		h.presentSnack(n)
	case ops.KindWindow:
		h.openWindow(n)
	case ops.KindPopover:
		h.presentPopover(n)
	}
}

// sizeCovers sizes the full-screen sheets on w again (it resized).
func (h *Host) sizeCovers(w *win) {
	for _, n := range h.nodes {
		if n.kind == ops.KindSheet && n.nav != nil && n.nav.full && n.nav.dialog != 0 && h.declaredWin(n) == w {
			h.sizeSheet(n)
		}
	}
}

// declaredWin is the window a sheet or an alert is declared in (its
// Window prop: the main window for the root, a Window node's), whichever
// window is in use; the main window if it has none, or that one closed.
func (h *Host) declaredWin(n *node) *win {
	if n.nav != nil {
		if x := h.nodes[n.nav.window]; x != nil {
			if x.win != nil && !x.win.closing {
				return x.win
			}
			if h.main != nil && x == h.main.root {
				return h.main
			}
		}
	}
	return h.main
}

// dialogFloating is AdwDialogPresentationMode's floating dialog.
const dialogFloating = 1

// sizeSheet sizes a sheet's dialog: as wide as a dialog of libadwaita's
// (up to 600 points), most of the window's height, half of it for a
// medium-only sheet; a full-screen one, as big as its whole window.
func (h *Host) sizeSheet(n *node) {
	d := n.nav.dialog
	if d == 0 {
		return
	}
	w := h.declaredWin(n)
	if n.nav.full {
		ww, wh := int32(Call(FnWidgetGetWidth, w.window)), int32(Call(FnWidgetGetHeight, w.window))
		if ww <= 0 || wh <= 0 { // not shown yet: its default size
			WindowGetDefaultSize(w.window, &ww, &wh)
		}
		AdwDialogSetContentWidth(d, max(ww, 1))
		AdwDialogSetContentHeight(d, max(wh, 1))
		return
	}
	ww, wh := w.envW, w.envH
	if ww == 0 {
		ww, wh = 800, 600
	}
	height := wh * 0.9
	if n.nav.detents == 1 { // medium only
		height = wh * 0.5
	}
	AdwDialogSetContentWidth(d, windowSize(min(600, ww)))
	AdwDialogSetContentHeight(d, windowSize(max(min(height, wh), 200)))
}

// presentAlert shows an AdwAlertDialog of the alert's buttons; a button
// sends Action with its index, Escape (with no cancel button) Dismissed.
func (h *Host) presentAlert(n *node, parent Ptr) {
	d := AdwAlertDialogNew(n.title, n.message)
	n.nav.dialog = d
	closeID := "close"
	suggested := false
	for i, a := range n.actions {
		id := strconv.Itoa(i)
		AdwAlertDialogAddResponse(d, id, a.title)
		if a.is(ops.MenuDisabled) {
			AdwAlertDialogSetResponseEnabled(d, id, false)
		}
		switch a.role {
		case ops.AlertButtonRoleDestructive:
			AdwAlertDialogSetResponseAppearance(d, id, ResponseDestructive)
		case ops.AlertButtonRoleCancel:
			closeID = id
		default:
			if !suggested && n.alertStyle == int32(ops.AlertStyleAlert) {
				AdwAlertDialogSetResponseAppearance(d, id, ResponseSuggested)
				AdwAlertDialogSetDefaultResponse(d, id)
				suggested = true
			}
		}
	}
	if len(n.actions) == 0 {
		AdwAlertDialogAddResponse(d, "0", "OK")
		closeID = "0"
	}
	AdwAlertDialogSetCloseResponse(d, closeID)
	var first Ptr
	if n.alertStyle == int32(ops.AlertStyleAlert) && len(n.nav.fields) > 0 {
		first = h.alertFields(n, d)
	}
	Connect(d, "response", func(resp uintptr) {
		n.nav.gone = true
		id := GoString(resp)
		if i, err := strconv.Atoi(id); err == nil && i < len(n.actions) {
			h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(i)})
			return
		}
		h.emit(n, ops.Event{Kind: ops.EventDismissed})
	})
	AdwDialogPresent(d, parent)
	if first != 0 {
		AdwDialogSetFocus(d, first)
	}
}

// alertFields gives an alert its text fields, as libadwaita's dialogs
// have them: a boxed list of entry rows (password entry rows for
// passwords), each titled with its placeholder, under the message. Typing
// reports FieldChanged; activating a row before the last goes on to the
// next, and the last activates the default response (the suggested one,
// the first whose role is Default) if it's enabled. It returns the first
// row, which takes the focus.
func (h *Host) alertFields(n *node, d Ptr) Ptr {
	list := ListBoxNew()
	ListBoxSetSelectionMode(list, SelectionNone)
	WidgetAddCSSClass(list, "boxed-list")
	rows := make([]Ptr, len(n.nav.fields))
	for i, f := range n.nav.fields {
		var row Ptr
		if f.Secure() {
			row = AdwPasswordEntryRowNew()
		} else {
			row = AdwEntryRowNew()
			purpose, hints := alertFieldPurpose(f)
			AdwEntryRowSetInputPurpose(row, purpose)
			AdwEntryRowSetInputHints(row, hints)
		}
		SetStringProperty(row, "title", f.Placeholder)
		EditableSetText(row, f.Text)
		rows[i] = row
		ListBoxAppend(list, row)
	}
	for i, row := range rows {
		i, row := i, row
		Connect(row, "changed", func() {
			if n.nav.gone || h.nodes[n.id] != n {
				return
			}
			h.emit(n, ops.Event{Kind: ops.EventFieldChanged, Bytes: ops.AppendFieldChanged(nil, i, EditableGetText(row))})
		})
		if i+1 < len(rows) {
			next := rows[i+1]
			Connect(row, "entry-activated", func() { WidgetGrabFocus(next) })
		} else {
			AdwEntryRowSetActivatesDefault(row, true)
		}
	}
	AdwAlertDialogSetExtraChild(d, list)
	n.nav.rows = rows
	return rows[0]
}

// alertFieldPurpose is what an alert's field tells the input method: its
// keyboard and content type, as a TextField's (applyPurpose).
func alertFieldPurpose(f ops.AlertField) (purpose, hints int32) {
	purpose = purposeFree
	switch f.Keyboard {
	case ops.KeyboardNumber:
		purpose = purposeDigits
	case ops.KeyboardDecimal:
		purpose = purposeNumber
	case ops.KeyboardEmail:
		purpose = purposeEmail
	case ops.KeyboardURL:
		purpose = purposeURL
	case ops.KeyboardPhone:
		purpose = purposePhone
	}
	hints = hintUppercaseSentences
	switch f.ContentType {
	case "oneTimeCode":
		purpose, hints = purposePIN, hintNoSpellcheck
	case "email":
		purpose, hints = purposeEmail, hintNoSpellcheck
	case "phone":
		purpose = purposePhone
	case "name", "givenName", "familyName":
		purpose, hints = purposeName, hintUppercaseWords
	case "cardNumber":
		purpose, hints = purposeDigits, hintNoSpellcheck
	case "postalCode":
		hints = hintNoSpellcheck
	case "url":
		purpose, hints = purposeURL, hintNoSpellcheck
	case "username":
		hints = hintNoSpellcheck
	}
	if purpose == purposeEmail || purpose == purposeURL {
		hints = hintNoSpellcheck
	}
	return purpose, hints
}

// Tabs.

func (h *Host) tabIndex(tabs *node, child Ptr) int {
	for i, c := range tabs.children {
		if c.widget == child {
			return i
		}
	}
	return -1
}

func (h *Host) selectTab(n *node) {
	if n.selected < 0 || n.selected >= len(n.children) {
		return
	}
	c := n.children[n.selected]
	n.updating = true
	AdwViewStackSetVisibleChild(n.nav.view, c.widget)
	n.updating = false
	h.queueChrome()
}

// tabsResized puts the switcher in the header (wide) or at the bottom
// (narrow, under 550 points: libadwaita's breakpoint for it).
func (h *Host) tabsResized(n *node) {
	narrow := n.frame[2] < 550
	if narrow != n.nav.narrow {
		n.nav.narrow = narrow
		h.queueChrome()
	}
	AdwViewSwitcherBarSetReveal(n.nav.bar, narrow)
}

// Windows.

// openWindow shows a Window node's window at its size. Where it goes is
// the desktop's to say, unless the app says: GTK 4 has no way to place a
// window (Wayland leaves it to the compositor, and GTK 4 dropped
// gtk_window_move on X11 too), and GNOME's and KDE's window managers
// place a new window themselves, where it covers the fewest others,
// cascading when it must; on X11 the host moves one placed at X, Y,
// centered, a panel or a flyout itself (deskwin.go). One sized to its
// content shows once it knows its size.
//
// One opened hidden (WindowConfig's WindowHidden) is laid out at its size
// first, its environment reported as if shown, and shown by a Show or
// Focus.
func (h *Host) openWindow(n *node) {
	w := n.win
	if !w.configured {
		h.configure(w, ops.WindowConfig{})
	}
	WindowSetApplication(w.window, h.app)
	if w.hidden {
		var dw, dh int32
		WindowGetDefaultSize(w.window, &dw, &dh)
		h.rootAllocated(n, dw, max(dh-w.headerRoom(), 0))
		return
	}
	h.showWin(w, true)
}

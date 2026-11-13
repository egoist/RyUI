//go:build linux && !android

package linux

import (
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A SplitView is libadwaita's AdwNavigationSplitView, as GNOME's split
// apps are: a sidebar page (its own header bar, with the sidebar's title,
// over a .navigation-sidebar GtkListBox of the destinations, their
// sections' titles over them) beside a content page holding the detail
// Stack under the window's header bar (chrome.go moves it there, so the
// detail's back button, title and bar buttons are over the detail). Under
// 500 points wide it collapses into one page at a time: the sidebar, then
// the chosen destination's content, with a back button to the sidebar.
// Choosing a destination sends Selected.

const splitCollapseWidth = 500

type split struct {
	view        Ptr // AdwNavigationSplitView
	sidePage    Ptr
	contentPage Ptr
	content     Ptr // the content page's AdwToolbarView
	title       Ptr // the sidebar's AdwWindowTitle
	box         Ptr // the sidebar's GtkListBox
	rows        []sidebarRow
	widgets     []Ptr // the list box's rows, as rows
	selected    int
	updating    bool
	detail      *node
}

// sidebarRow is a section's title or a destination (its index among all
// the destinations).
type sidebarRow struct {
	title, icon string
	group       bool
	index       int
}

// headerHome is where each window's header bar is: in its own toolbar
// view, or a split view's content page's.
var headerHome = map[*win]Ptr{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindSplit {
			return false
		}
		h.makeSplit(n)
		return true
	})
	propHooks = append(propHooks, splitProp)
	frameHooks = append(frameHooks, splitFrame)
	releaseHooks = append(releaseHooks, releaseSplit)
}

func (h *Host) makeSplit(n *node) {
	s := &split{selected: -1}
	n.ext = s
	s.view = AdwNavigationSplitViewNew()
	AdwNavigationSplitViewSetMinSidebarWidth(s.view, 200)
	AdwNavigationSplitViewSetMaxSidebarWidth(s.view, 280)

	side := AdwToolbarViewNew()
	bar := AdwHeaderBarNew()
	s.title = AdwWindowTitleNew("", "")
	AdwHeaderBarSetTitleWidget(bar, s.title)
	AdwToolbarViewAddTopBar(side, bar)
	s.box = ListBoxNew()
	WidgetAddCSSClass(s.box, "navigation-sidebar")
	ListBoxSetSelectionMode(s.box, SelectionSingle)
	scroll := ScrolledWindowNew()
	ScrolledWindowSetPolicy(scroll, PolicyNever, PolicyAutomatic)
	ScrolledWindowSetChild(scroll, s.box)
	AdwToolbarViewSetContent(side, scroll)
	s.sidePage = AdwNavigationPageNew(side, "")

	s.content = AdwToolbarViewNew()
	s.contentPage = AdwNavigationPageNew(s.content, "")
	AdwNavigationSplitViewSetSidebar(s.view, s.sidePage)
	AdwNavigationSplitViewSetContent(s.view, s.contentPage)
	h.setInner(n, s.view)

	Connect(s.box, "row-selected", func(row uintptr) {
		if s.updating || row == 0 {
			return
		}
		h.sidebarChose(n, row)
	})
	Connect(s.box, "row-activated", func(row uintptr) {
		// Collapsed, the chosen destination (even the one shown) shows.
		if AdwNavigationSplitViewGetCollapsed(s.view) {
			AdwNavigationSplitViewSetShowContent(s.view, true)
		}
	})
}

func splitProp(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
	s, ok := n.ext.(*split)
	if !ok || n.kind != ops.KindSplit {
		return false
	}
	switch p {
	case ops.PropSplitItems:
		s.rows = decodeSidebar(v.Bytes)
		h.fillSidebar(s)
	case ops.PropSplitSelected:
		s.selected = int(v.Int)
		h.showSidebarSelection(s)
	case ops.PropSplitTitle:
		AdwWindowTitleSetTitle(s.title, v.Str)
		AdwNavigationPageSetTitle(s.sidePage, v.Str)
	default:
		return false
	}
	return true
}

func decodeSidebar(b []byte) []sidebarRow {
	r := reader{b: b}
	var rows []sidebarRow
	flat := 0
	for range int(r.u32()) {
		if !r.ok(4) {
			break
		}
		if t := r.str(); t != "" {
			rows = append(rows, sidebarRow{title: t, group: true})
		}
		for range int(r.u32()) {
			if !r.ok(4) {
				break
			}
			title := r.str()
			rows = append(rows, sidebarRow{title: title, icon: r.str(), index: flat})
			flat++
		}
	}
	return rows
}

// fillSidebar makes the list box's rows again: a section's title (not
// selectable), a destination's icon and title.
func (h *Host) fillSidebar(s *split) {
	s.updating = true
	defer func() { s.updating = false }()
	for _, w := range s.widgets {
		ListBoxRemove(s.box, w)
	}
	s.widgets = s.widgets[:0]
	for i, r := range s.rows {
		row := ListBoxRowNew()
		if r.group {
			l := LabelNew(r.title)
			LabelSetXAlign(l, 0)
			WidgetAddCSSClass(l, "caption-heading")
			WidgetAddCSSClass(l, "dim-label")
			if i > 0 {
				WidgetSetMarginTop(l, 12)
			}
			WidgetSetMarginStart(l, 6)
			ListBoxRowSetChild(row, l)
			ListBoxRowSetSelectable(row, false)
			ListBoxRowSetActivatable(row, false)
			WidgetSetFocusable(row, false)
		} else {
			box := BoxNew(OrientationHorizontal, 12)
			if name := h.iconName(r.icon); name != "" {
				BoxAppend(box, ImageNewFromIconName(name))
			}
			l := LabelNew(r.title)
			LabelSetXAlign(l, 0)
			LabelSetEllipsize(l, EllipsizeEnd)
			WidgetSetHExpand(l, true)
			BoxAppend(box, l)
			ListBoxRowSetChild(row, box)
		}
		ListBoxAppend(s.box, row)
		s.widgets = append(s.widgets, row)
	}
	h.showSidebarSelection(s)
}

// showSidebarSelection selects the chosen destination's row (none: no
// row), and titles the content page with it.
func (h *Host) showSidebarSelection(s *split) {
	s.updating = true
	defer func() { s.updating = false }()
	for r, row := range s.rows {
		if !row.group && row.index == s.selected && r < len(s.widgets) {
			if ListBoxGetSelectedRow(s.box) != s.widgets[r] {
				ListBoxSelectRow(s.box, s.widgets[r])
			}
			AdwNavigationPageSetTitle(s.contentPage, row.title)
			return
		}
	}
	ListBoxUnselectAll(s.box)
}

// sidebarChose is the user choosing a row (a click, the keyboard).
func (h *Host) sidebarChose(n *node, row Ptr) {
	s := n.ext.(*split)
	for r, w := range s.widgets {
		if w != row || r >= len(s.rows) || s.rows[r].group {
			continue
		}
		AdwNavigationPageSetTitle(s.contentPage, s.rows[r].title)
		if AdwNavigationSplitViewGetCollapsed(s.view) {
			AdwNavigationSplitViewSetShowContent(s.view, true)
		}
		if i := s.rows[r].index; i != s.selected {
			s.selected = i
			h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
		}
		return
	}
}

// insertSplit puts the detail Stack in the content page, under the
// window's header bar.
func (h *Host) insertSplit(p, c *node, index int) {
	s := p.ext.(*split)
	h.addChild(p, c, index)
	if c.widget == 0 {
		return
	}
	s.detail = c
	h.detach(c.widget)
	AdwToolbarViewSetContent(s.content, c.widget)
	h.queueChrome()
}

func (h *Host) removeSplit(p, c *node) {
	s := p.ext.(*split)
	if s.detail == c {
		s.detail = nil
		AdwToolbarViewSetContent(s.content, 0)
	}
	h.queueChrome()
}

// splitFrame: the detail is sized by its page, not the core; a split
// view narrower than 500 points collapses.
func splitFrame(h *Host, n *node, f [4]float32) bool {
	if p := n.parent; p != nil && p.kind == ops.KindSplit {
		return true
	}
	if s, ok := n.ext.(*split); ok && n.kind == ops.KindSplit {
		collapsed := f[2] < splitCollapseWidth
		if collapsed != AdwNavigationSplitViewGetCollapsed(s.view) {
			AdwNavigationSplitViewSetCollapsed(s.view, collapsed)
		}
	}
	return false
}

// placeHeader puts a window's header bar over the split view's content
// on screen in it (sp), or back at its top (nil).
func (h *Host) placeHeader(w *win, sp *node) {
	home := headerHome[w]
	if home == 0 {
		home = w.toolbar
	}
	target := w.toolbar
	if sp != nil && h.nodes[sp.id] == sp { // not one gone
		if s, ok := sp.ext.(*split); ok {
			target = s.content
		}
	}
	if home == target {
		return
	}
	ObjectRef(w.header)
	AdwToolbarViewRemove(home, w.header)
	AdwToolbarViewAddTopBar(target, w.header)
	ObjectUnref(w.header)
	if target == w.toolbar {
		delete(headerHome, w)
	} else {
		headerHome[w] = target
	}
}

// releaseSplit gives the window its header bar back, and lets go of the
// detail, as the split view goes.
func releaseSplit(h *Host, n *node) {
	s, ok := n.ext.(*split)
	if !ok || n.kind != ops.KindSplit {
		return
	}
	for w, home := range headerHome {
		if home == s.content {
			h.placeHeader(w, nil)
		}
	}
	if s.detail != nil {
		AdwToolbarViewSetContent(s.content, 0)
		s.detail = nil
	}
}

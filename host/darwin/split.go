//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

// A SplitView is an NSSplitViewController: a sidebar item (the source
// list's material, collapsible) holding a source-list NSTableView of the
// destinations, their sections as group rows, and a content item holding
// the detail Stack, which fills it.

type split struct {
	ctl, table ID
	detail     ID // the content item's view, the Stack's superview
	rows       []sidebarRow
	selected   int // the chosen destination, among them all
	reloading  bool
}

// sidebarRow is a section's title (group) or a destination (index among
// all the destinations).
type sidebarRow struct {
	title, icon, badge string
	group              bool
	index              int
}

func registerSplitClasses() {
	DefineClass("AsukaSidebarSource", "NSObject", []string{"NSTableViewDataSource", "NSTableViewDelegate"}, []objc.MethodDef{
		Method("numberOfRowsInTableView:", func(self ID, _ SEL, t ID) int {
			if s := theHost.splitOf(t); s != nil {
				return len(s.rows)
			}
			return 0
		}),
		Method("tableView:isGroupRow:", func(self ID, _ SEL, t ID, row int) bool {
			s := theHost.splitOf(t)
			return s != nil && row < len(s.rows) && s.rows[row].group
		}),
		Method("tableView:shouldSelectRow:", func(self ID, _ SEL, t ID, row int) bool {
			s := theHost.splitOf(t)
			return s != nil && row < len(s.rows) && !s.rows[row].group
		}),
		Method("tableView:viewForTableColumn:row:", func(self ID, _ SEL, t, col ID, row int) ID {
			s := theHost.splitOf(t)
			if s == nil || row >= len(s.rows) {
				return 0
			}
			cell := Send(initView("AsukaSidebarCell"), "autorelease")
			theHost.fillSidebarCell(cell, s.rows[row], theHost.isRTL(theHost.splits[t]))
			return cell
		}),
		Method("tableViewSelectionDidChange:", func(self ID, _ SEL, note ID) {
			theHost.sidebarChose(Send(note, "object"))
		}),
	})
	// A row's view: its symbol, title and badge, which it lays out
	// itself as it's sized, from its leading edge.
	DefineClass("AsukaSidebarCell", "NSTableCellView", nil, []objc.MethodDef{
		Method("resizeSubviewsWithOldSize:", func(self ID, _ SEL, old Size) { layoutSidebarCell(self) }),
	})
}

// sidebarBadgeID marks a row's badge among its subviews.
const sidebarBadgeID = "asuka.badge"

// fillSidebarCell shows a row in its view (a new one, or one shown
// already, in place): a destination's symbol, title and badge, or a
// section's title in the group row's style.
func (h *Host) fillSidebarCell(cell ID, r sidebarRow, rtl bool) {
	Send(cell, "setUserInterfaceLayoutDirection:", direction(rtl))
	label := Send(cell, "textField")
	if label == 0 {
		label = Send(Class("NSTextField"), "labelWithString:", uintptr(String("")))
		Send(label, "setLineBreakMode:", 4) // truncate the tail
		Send(cell, "addSubview:", uintptr(label))
		Send(cell, "setTextField:", uintptr(label))
	}
	Send(label, "setStringValue:", uintptr(String(r.title)))
	Send(label, "setAlignment:", 4) // natural: the way its text reads
	if r.group {
		Send(label, "setFont:", uintptr(MsgFloat2ID(Class("NSFont"), Sel("systemFontOfSize:weight:"), 11, 0.3)))
		Send(label, "setTextColor:", uintptr(Send(Class("NSColor"), "secondaryLabelColor")))
	}
	iv := Send(cell, "imageView")
	if img := symbolImage(r.icon); img != 0 && !r.group {
		if iv == 0 {
			iv = initView("NSImageView")
			Send(iv, "setContentTintColor:", uintptr(Send(Class("NSColor"), "controlAccentColor")))
			Send(cell, "addSubview:", uintptr(iv))
			Send(cell, "setImageView:", uintptr(iv))
			Release(iv)
		}
		Send(iv, "setImage:", uintptr(img))
	} else if iv != 0 {
		Send(iv, "removeFromSuperview")
		Send(cell, "setImageView:", 0)
	}
	// The badge: a count at the trailing end in the secondary color, as
	// Mail counts a mailbox's unread messages; VoiceOver reads it after
	// the title, a count as so many items.
	badge := sidebarBadge(cell)
	if r.badge != "" && !r.group {
		if badge == 0 {
			badge = Send(Class("NSTextField"), "labelWithString:", uintptr(String("")))
			Send(badge, "setIdentifier:", uintptr(String(sidebarBadgeID)))
			Send(badge, "setFont:", uintptr(MsgFloat2ID(Class("NSFont"), Sel("monospacedDigitSystemFontOfSize:weight:"), MsgFloat(Class("NSFont"), Sel("systemFontSize")), 0)))
			Send(badge, "setTextColor:", uintptr(Send(Class("NSColor"), "secondaryLabelColor")))
			Send(cell, "addSubview:", uintptr(badge))
		}
		Send(badge, "setStringValue:", uintptr(String(r.badge)))
		Send(badge, "setAccessibilityLabel:", uintptr(String(l10n.Badge(h.locale, r.badge))))
	} else if badge != 0 {
		Send(badge, "removeFromSuperview")
	}
	layoutSidebarCell(cell)
}

func sidebarBadge(cell ID) ID {
	for _, v := range ArrayItems(Send(cell, "subviews")) {
		if id := Send(v, "identifier"); id != 0 && GoString(id) == sidebarBadgeID {
			return v
		}
	}
	return 0
}

// layoutSidebarCell places a row's symbol at its leading edge, its badge
// at its trailing edge and its title between them, mirrored right to
// left.
func layoutSidebarCell(cell ID) {
	b := MsgRect(cell, Sel("bounds"))
	w, ht := b.Size.Width, b.Size.Height
	rtl := SendInt(cell, "userInterfaceLayoutDirection") == 1
	place := func(v ID, x, width, height float64) {
		if rtl {
			x = w - x - width
		}
		MsgSetRect(v, selSetFrame, Rect{Origin: Point{X: x, Y: (ht - height) / 2}, Size: Size{Width: max(width, 0), Height: height}})
	}
	x, end := 4.0, w-4
	if iv := Send(cell, "imageView"); iv != 0 {
		place(iv, 4, 18, 16)
		x = 28
	}
	if badge := sidebarBadge(cell); badge != 0 {
		s := MsgSize(badge, Sel("fittingSize"))
		end = w - 8 - s.Width
		place(badge, end, s.Width, s.Height)
		end -= 6
	}
	if label := Send(cell, "textField"); label != 0 {
		place(label, x, end-x, MsgSize(label, Sel("intrinsicContentSize")).Height)
	}
}

func (h *Host) makeSplit(n *node) ID {
	s := &split{selected: -1}
	n.split = s
	src := New("AsukaSidebarSource")
	h.sidebarSource = src
	table := initView("NSTableView")
	col := Send(Send(Class("NSTableColumn"), "alloc"), "initWithIdentifier:", uintptr(String("item")))
	Send(table, "addTableColumn:", uintptr(col))
	Release(col)
	Send(table, "setHeaderView:", 0)
	Send(table, "setStyle:", 3) // NSTableViewStyleSourceList
	Send(table, "setRowSizeStyle:", 2)
	Send(table, "setDataSource:", uintptr(src))
	Send(table, "setDelegate:", uintptr(src))
	s.table = table
	scroll := initView("NSScrollView")
	Send(scroll, "setDocumentView:", uintptr(table))
	Send(scroll, "setDrawsBackground:", 0)
	Send(scroll, "setHasVerticalScroller:", 1)
	Send(scroll, "setAutohidesScrollers:", 1)
	sideVC := New("NSViewController")
	Send(sideVC, "setView:", uintptr(scroll))
	Release(scroll)
	s.detail = initView("AsukaView")
	contentVC := New("NSViewController")
	Send(contentVC, "setView:", uintptr(s.detail))
	ctl := New("NSSplitViewController")
	side := Send(Class("NSSplitViewItem"), "sidebarWithViewController:", uintptr(sideVC))
	content := Send(Class("NSSplitViewItem"), "splitViewItemWithViewController:", uintptr(contentVC))
	MsgSetFloat(side, Sel("setMinimumThickness:"), 180)
	MsgSetFloat(side, Sel("setMaximumThickness:"), 320)
	Send(ctl, "addSplitViewItem:", uintptr(side))
	Send(ctl, "addSplitViewItem:", uintptr(content))
	Release(sideVC)
	Release(contentVC)
	s.ctl = ctl
	h.splits[table] = n
	return Send(ctl, "view")
}

func (h *Host) splitOf(table ID) *split {
	if n := h.splits[table]; n != nil {
		return n.split
	}
	return nil
}

func (h *Host) setSplitProp(n *node, p ops.PropID, v *ops.Value) {
	s := n.split
	switch p {
	case ops.PropSplitItems:
		rows := decodeSidebar(v.Bytes)
		if sameRows(s.rows, rows) {
			// As many sections and items: the rows that changed change
			// in place, keeping the selection, the scroll and VoiceOver's
			// focus.
			rtl := h.isRTL(n)
			for r := range rows {
				if rows[r] == s.rows[r] {
					continue
				}
				s.rows[r] = rows[r]
				if cell := Send(s.table, "viewAtColumn:row:makeIfNecessary:", 0, uintptr(r), 0); cell != 0 {
					h.fillSidebarCell(cell, rows[r], rtl)
				}
			}
			return
		}
		s.rows = rows
		s.reloading = true
		Send(s.table, "reloadData")
		s.reloading = false
		h.showSidebarSelection(s)
	case ops.PropSplitSelected:
		s.selected = int(v.Int)
		h.showSidebarSelection(s)
	case ops.PropSplitTitle:
		Send(s.ctl, "setTitle:", uintptr(String(v.Str)))
	}
}

func decodeSidebar(b []byte) []sidebarRow {
	var rows []sidebarRow
	flat := 0
	for _, sec := range ops.DecodeSidebar(b) {
		if sec.Title != "" {
			rows = append(rows, sidebarRow{title: sec.Title, group: true})
		}
		for _, it := range sec.Items {
			rows = append(rows, sidebarRow{title: it.Title, icon: it.Icon, badge: it.Badge, index: flat})
			flat++
		}
	}
	return rows
}

// sameRows reports whether a and b are as many rows, each a group or a
// destination as before: they change in place.
func sameRows(a, b []sidebarRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].group != b[i].group || a[i].index != b[i].index {
			return false
		}
	}
	return true
}

// showSidebarSelection selects the chosen destination's row.
func (h *Host) showSidebarSelection(s *split) {
	for r, row := range s.rows {
		if !row.group && row.index == s.selected {
			if SendInt(s.table, "selectedRow") != r {
				s.reloading = true
				set := Send(Class("NSIndexSet"), "indexSetWithIndex:", uintptr(r))
				Send(s.table, "selectRowIndexes:byExtendingSelection:", uintptr(set), 0)
				s.reloading = false
			}
			return
		}
	}
}

// sidebarChose is the user choosing a row.
func (h *Host) sidebarChose(table ID) {
	n := h.splits[table]
	if n == nil || n.split.reloading {
		return
	}
	s := n.split
	r := SendInt(table, "selectedRow")
	if r < 0 || r >= len(s.rows) || s.rows[r].group || s.rows[r].index == s.selected {
		return
	}
	s.selected = s.rows[r].index
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(s.selected)})
}

// insertSplit puts the detail Stack in the content item, filling it.
func (h *Host) insertSplit(p, c *node, index int) bool {
	if p.split == nil {
		return false
	}
	p.children = append(p.children, nil)
	copy(p.children[min(index, len(p.children)-1)+1:], p.children[min(index, len(p.children)-1):])
	p.children[min(index, len(p.children)-1)] = c
	c.parent = p
	b := MsgRect(p.split.detail, Sel("bounds"))
	MsgSetRect(c.view, selSetFrame, b)
	Send(c.view, "setAutoresizingMask:", 18) // width and height follow
	Send(p.split.detail, "addSubview:", uintptr(c.view))
	return true
}

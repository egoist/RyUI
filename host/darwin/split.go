//go:build darwin && !ios

package darwin

import (
	"encoding/binary"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
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
	title, icon string
	group       bool
	index       int
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
			return sidebarCell(s.rows[row])
		}),
		Method("tableViewSelectionDidChange:", func(self ID, _ SEL, note ID) {
			theHost.sidebarChose(Send(note, "object"))
		}),
	})
}

// sidebarCell is a row's view: a destination's symbol and title, or a
// section's title in the group row's style.
func sidebarCell(r sidebarRow) ID {
	cell := initView("NSTableCellView")
	label := Send(Class("NSTextField"), "labelWithString:", uintptr(String(r.title)))
	Send(label, "setTranslatesAutoresizingMaskIntoConstraints:", 1)
	Send(label, "setLineBreakMode:", 4) // truncate the tail
	x := 4.0
	if r.group {
		Send(label, "setFont:", uintptr(MsgFloat2ID(Class("NSFont"), Sel("systemFontOfSize:weight:"), 11, 0.3)))
		Send(label, "setTextColor:", uintptr(Send(Class("NSColor"), "secondaryLabelColor")))
	} else if img := symbolImage(r.icon); img != 0 {
		iv := initView("NSImageView")
		Send(iv, "setImage:", uintptr(img))
		Send(iv, "setContentTintColor:", uintptr(Send(Class("NSColor"), "controlAccentColor")))
		MsgSetRect(iv, selSetFrame, Rect{Origin: Point{X: 4, Y: 4}, Size: Size{Width: 18, Height: 16}})
		Send(cell, "addSubview:", uintptr(iv))
		Send(cell, "setImageView:", uintptr(iv))
		Release(iv)
		x = 28
	}
	MsgSetRect(label, selSetFrame, Rect{Origin: Point{X: x, Y: 3}, Size: Size{Width: 200 - x, Height: 18}})
	Send(label, "setAutoresizingMask:", 2) // its width follows the cell's
	Send(cell, "addSubview:", uintptr(label))
	Send(cell, "setTextField:", uintptr(label))
	return Send(cell, "autorelease")
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
		s.rows = decodeSidebar(v.Bytes)
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
	le := binary.LittleEndian
	i := 0
	u32 := func() int {
		if i+4 > len(b) {
			i = len(b)
			return 0
		}
		v := int(le.Uint32(b[i:]))
		i += 4
		return v
	}
	str := func() string {
		n := min(u32(), len(b)-i)
		s := string(b[i : i+n])
		i += n
		return s
	}
	var rows []sidebarRow
	flat := 0
	for range u32() {
		if t := str(); t != "" {
			rows = append(rows, sidebarRow{title: t, group: true})
		}
		for range u32() {
			title := str()
			rows = append(rows, sidebarRow{title: title, icon: str(), index: flat})
			flat++
		}
	}
	return rows
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

//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A SplitView is WinUI's NavigationView, left: a pane of the
// destinations (drawn by the host, its section headers and items, the
// selected one on a subtle fill with an accent pill at its leading edge)
// beside the detail Stack, which fills the rest.

const paneWidth = 280

type splitPane struct {
	split    *node
	rows     []paneRow
	selected int
	hot      int
}

type paneRow struct {
	title, icon, badge string
	header             bool
	index              int // a destination's, among them all
}

func (h *Host) makeSplit(n *node) {
	n.hwnd = h.makeView(0)
	h.nextCell++
	b := newNode(ops.HostIDBit|h.nextCell, kindSidebar)
	b.hwnd = h.makeView(WS_TABSTOP)
	b.pane = &splitPane{split: n, selected: -1, hot: -1}
	b.k, b.dir = n.k, n.dir
	b.host = paneMessage
	SetParent(b.hwnd, n.hwnd)
	h.add(b)
	n.paneNode = b
}

func (h *Host) setSplitProp(n *node, p ops.PropID, v *ops.Value) {
	pane := n.paneNode.pane
	switch p {
	case ops.PropSplitItems:
		pane.rows = decodePane(v.Bytes)
	case ops.PropSplitSelected:
		pane.selected = int(v.Int)
	case ops.PropSplitTitle:
		n.title = v.Str
	}
	Invalidate(n.paneNode.hwnd)
}

func decodePane(b []byte) []paneRow {
	var rows []paneRow
	flat := 0
	for _, sec := range ops.DecodeSidebar(b) {
		if sec.Title != "" {
			rows = append(rows, paneRow{title: sec.Title, header: true})
		}
		for _, it := range sec.Items {
			rows = append(rows, paneRow{title: it.Title, icon: it.Icon, badge: it.Badge, index: flat})
			flat++
		}
	}
	return rows
}

// layoutSplit puts the pane at the leading edge and the detail beside it.
func (h *Host) layoutSplit(n *node) {
	if n.paneNode == nil {
		return
	}
	r := ClientRect(n.hwnd)
	pw := min(h.px(n, paneWidth), r.W())
	px, dx := int32(0), pw
	if h.isRTL(n) {
		px, dx = r.W()-pw, 0
	}
	SetWindowPos(n.paneNode.hwnd, 0, px, 0, pw, r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	for _, c := range n.children {
		if c.hwnd != 0 {
			SetWindowPos(c.hwnd, 0, dx, 0, r.W()-pw, r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
			h.resized(c)
		}
	}
}

// paneRects are the rows' places, in pixels: items 40 high, section
// headers 36, under the title's 48.
func (h *Host) paneRects(b *node) []Rect {
	r := ClientRect(b.hwnd)
	y := h.px(b, 48)
	if b.pane.split.title == "" {
		y = h.px(b, 8)
	}
	out := make([]Rect, len(b.pane.rows))
	for i, row := range b.pane.rows {
		ht := h.px(b, 40)
		if row.header {
			ht = h.px(b, 36)
		}
		out[i] = Rect{Left: h.px(b, 4), Top: y, Right: r.W() - h.px(b, 4), Bottom: y + ht}
		y += ht + h.px(b, 2)
	}
	return out
}

func paneMessage(h *Host, b *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	pane := b.pane
	at := func() int {
		p := Point{X: XParam(lp), Y: YParam(lp)}
		for i, r := range h.paneRects(b) {
			if p.Y >= r.Top && p.Y < r.Bottom && !pane.rows[i].header {
				return i
			}
		}
		return -1
	}
	switch m {
	case WM_MOUSEMOVE:
		if i := at(); i != pane.hot {
			pane.hot = i
			TrackLeave(b.hwnd)
			Invalidate(b.hwnd)
		}
		return 0, true
	case WM_MOUSELEAVE:
		pane.hot = -1
		Invalidate(b.hwnd)
		return 0, true
	case WM_LBUTTONDOWN:
		if i := at(); i >= 0 {
			h.choosePane(b, pane.rows[i].index)
		}
		return 0, true
	case WM_GETDLGCODE:
		return DLGC_WANTARROWS, true
	case WM_KEYDOWN:
		switch wp {
		case VK_UP:
			h.choosePane(b, pane.selected-1)
			h.announceChoice(b)
			return 0, true
		case VK_DOWN:
			h.choosePane(b, pane.selected+1)
			h.announceChoice(b)
			return 0, true
		}
	}
	return 0, false
}

func (h *Host) choosePane(b *node, i int) {
	pane := b.pane
	n := 0
	for _, r := range pane.rows {
		if !r.header {
			n++
		}
	}
	if i < 0 || i >= n || i == pane.selected {
		return
	}
	pane.selected = i
	Invalidate(b.hwnd)
	h.emit(pane.split, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}

// paintPane draws the rows: the hovered and the selected on a subtle
// fill, the selected with an accent pill at its leading edge (g); the
// title, the headers and the items' icons and titles (dc).
func (h *Host) paintPane(b *node, dc uintptr, g Graphics, r Rect) {
	pane := b.pane
	k := h.scaleOf(b)
	p := &h.theme
	rects := h.paneRects(b)
	rtl := h.isRTL(b)
	if g != 0 {
		br := SolidBrush(p.separator.argb())
		x := float32(r.W()) - max(k, 1)
		if rtl {
			x = 0
		}
		g.FillRect(br, x, 0, max(k, 1), float32(r.H()))
		br.Close()
		for i, row := range pane.rows {
			if row.header {
				continue
			}
			tr := rects[i]
			sel := row.index == pane.selected
			if sel || i == pane.hot {
				c := p.subtleHot
				if sel {
					c = p.subtlePressed
				}
				g.FillRoundRect(c.argb(), float32(tr.Left), float32(tr.Top), float32(tr.W()), float32(tr.H()), 4*k)
			}
			if sel {
				pw, ph := 3*k, 16*k
				x := float32(tr.Left)
				if rtl {
					x = float32(tr.Right) - pw
				}
				g.FillRoundRect(p.accent.argb(), x, float32(tr.Top+tr.Bottom)/2-ph/2, pw, ph, pw/2)
			}
			if row.badge != "" {
				h.paintBadge(b, 0, g, h.paneBadge(b, row, tr), row.badge)
			}
		}
		return
	}
	SetBkMode(dc, TRANSPARENT)
	if t := pane.split.title; t != "" {
		old := SelectObject(dc, h.font(fontSpec{size: 14, weight: int32(ops.FontWeightSemibold), k: k}))
		SetTextColor(dc, p.label.over(p.bg).ref())
		tr := Rect{Left: h.px(b, 16), Top: 0, Right: r.W() - h.px(b, 16), Bottom: h.px(b, 48)}
		if rtl {
			tr = flipIn(tr, r)
		}
		DrawText(dc, UTF16Units(t), &tr, DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS|startSide(rtl))
		SelectObject(dc, old)
	}
	body := h.bodyFont(b)
	for i, row := range pane.rows {
		tr := rects[i]
		x := tr.Left + h.px(b, 12)
		if row.header {
			old := SelectObject(dc, h.font(fontSpec{size: 12, weight: int32(ops.FontWeightSemibold), k: k}))
			SetTextColor(dc, p.secondary.over(p.bg).ref())
			lr := Rect{Left: x, Top: tr.Top, Right: tr.Right, Bottom: tr.Bottom}
			if rtl {
				lr = flipIn(lr, tr)
			}
			DrawText(dc, UTF16Units(row.title), &lr, DT_BOTTOM|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS|startSide(rtl))
			SelectObject(dc, old)
			continue
		}
		SetTextColor(dc, p.label.over(p.bg).ref())
		if glyph := directedGlyph(row.icon, rtl); glyph != 0 {
			old := SelectObject(dc, h.iconFont(b, 16*h.fontScale()))
			gr := Rect{Left: x, Top: tr.Top, Right: x + h.px(b, 16), Bottom: tr.Bottom}
			if rtl {
				gr = flipIn(gr, tr)
			}
			DrawText(dc, []uint16{glyph}, &gr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
			SelectObject(dc, old)
		}
		x += h.px(b, 16+12)
		old := SelectObject(dc, body)
		end := tr.Right - h.px(b, 8)
		if bw, _ := h.badgeSize(b, row.badge); bw > 0 {
			end -= bw + h.px(b, 8)
		}
		lr := Rect{Left: x, Top: tr.Top, Right: end, Bottom: tr.Bottom}
		if rtl {
			lr = flipIn(lr, tr)
		}
		DrawText(dc, UTF16Units(row.title), &lr, DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS|startSide(rtl))
		SelectObject(dc, old)
		if row.badge != "" {
			h.paintBadge(b, dc, 0, h.paneBadge(b, row, tr), row.badge)
		}
	}
}

// paneBadge is the place of an item's badge: at its trailing end, as
// WinUI's NavigationViewItem puts an InfoBadge.
func (h *Host) paneBadge(b *node, row paneRow, tr Rect) Rect {
	rtl := h.isRTL(b)
	end := tr.Right - h.px(b, 12)
	if rtl {
		end = tr.Left + h.px(b, 12)
	}
	return h.badgeAt(b, row.badge, end, tr.Top, tr.Bottom, rtl)
}

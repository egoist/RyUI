//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
)

// Badges on tabs and on the navigation pane's items are drawn as
// Fluent's InfoBadge (WinUI's attention style): a pill of the accent
// color, 16 points high and at least as wide, its value in caption text
// on the accent. Narrator hears one after its tab's or item's title, a
// count as so many items (a11y.go).

// badgeFont is a badge's text: caption, 11 points, scaled with the
// system's text size.
func (h *Host) badgeFont(n *node) uintptr {
	return h.font(fontSpec{size: 11 * h.fontScale(), k: h.scaleOf(n)})
}

// badgeSize is a badge's pill in pixels (0, 0: none).
func (h *Host) badgeSize(n *node, badge string) (w, ht int32) {
	if badge == "" {
		return 0, 0
	}
	dc := h.measureDC
	old := SelectObject(dc, h.badgeFont(n))
	tw, _ := TextExtent(dc, UTF16Units(badge))
	SelectObject(dc, old)
	ht = h.px(n, 16*h.fontScale())
	return max(ht, tw+h.px(n, 8)), ht
}

// badgeAt is the pill of a badge whose trailing edge is end, centered
// on the row from top to bottom (right to left, end is its left edge).
func (h *Host) badgeAt(n *node, badge string, end, top, bottom int32, rtl bool) Rect {
	w, ht := h.badgeSize(n, badge)
	y := (top + bottom - ht) / 2
	if rtl {
		return Rect{Left: end, Top: y, Right: end + w, Bottom: y + ht}
	}
	return Rect{Left: end - w, Top: y, Right: end, Bottom: y + ht}
}

// paintBadge draws a badge in r: its pill (g), or its text (dc).
func (h *Host) paintBadge(n *node, dc uintptr, g Graphics, r Rect, badge string) {
	p := &h.theme
	if g != 0 {
		ht := float32(r.H())
		g.FillRoundRect(p.accent.argb(), float32(r.Left), float32(r.Top), float32(r.W()), ht, ht/2)
		return
	}
	old := SelectObject(dc, h.badgeFont(n))
	SetTextColor(dc, p.onAccent.over(p.accent).ref())
	DrawText(dc, UTF16Units(badge), &r, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	SelectObject(dc, old)
}

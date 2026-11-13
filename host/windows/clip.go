//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
)

// Rounded clips. A child window ignores its parent's drawing, so a view
// that clips with a corner radius is shaped by a window region
// (SetWindowRgn), which clips its children's pixels too: the pixels its
// rounded rectangle covers whole. A region has no partial pixels, so
// the corners' edge pixels, outside it, are the parent's, and the parent
// draws them antialiased: the clipping view's tree drawn there and mixed
// with what's behind it by how much of each pixel the rounded rectangle
// covers. When something in the clipping view paints over its corners,
// the parent draws them again.

// clipRadius is n's rounded clip's radius in pixels, 0 for none.
func (h *Host) clipRadius(n *node, w, ht int32) float64 {
	if !n.clip || n.cornerRadius <= 0 || w <= 0 || ht <= 0 {
		return 0
	}
	return min(float64(n.cornerRadius*h.scaleOf(n)), float64(w)/2, float64(ht)/2)
}

// shapeClip shapes n's window to its rounded clip, or lets it go of one:
// as Clip, the corner radius or the size change.
func (h *Host) shapeClip(n *node) {
	if n.hwnd == 0 {
		return
	}
	r := ClientRect(n.hwnd)
	rc := h.clipRadius(n, r.W(), r.H())
	if rc <= 0 {
		if n.clipRects != nil {
			n.clipRects = nil
			h.roundClips--
			if n.pict == nil || !n.pict.shaped {
				SetWindowRgn(n.hwnd, 0)
			}
		}
		return
	}
	rects := clipRows(r.W(), r.H(), rc, nil)
	if n.clipRects == nil {
		h.roundClips++
	} else if n.clipR == rc && sameRects(n.clipRects, rects) {
		return
	}
	n.clipRects, n.clipR = rects, rc
	if n.pict != nil && n.pict.shaped {
		return // shaped to nothing while it's rotated (picture.go)
	}
	SetWindowRgn(n.hwnd, RegionOf(rects))
	if p := Parent(n.hwnd); p != 0 {
		RedrawRect(p, WindowRectIn(n.hwnd, p)) // its corners' edges
	}
}

func sameRects(a, b []Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// clipRows are the rows of the pixels a w by ht rounded rectangle of
// radius rc covers whole (coverage 1), runs of rows of the same width
// merged.
func clipRows(w, ht int32, rc float64, rects []Rect) []Rect {
	z := int32(math.Ceil(rc - 0.5)) // rows whose pixel centers are above the corners' centers
	z = min(z, ht/2)
	inset := func(y int32) int32 {
		dy := rc - (float64(y) + 0.5)
		s := (rc-0.5)*(rc-0.5) - dy*dy
		var x float64
		if s < 0 {
			x = math.Ceil(rc - 0.5)
		} else {
			x = math.Ceil(rc - 0.5 - math.Sqrt(s))
		}
		return int32(min(max(x, 0), float64(w)/2))
	}
	add := func(x0, y0, x1, y1 int32) {
		if x1 <= x0 || y1 <= y0 {
			return
		}
		if l := len(rects); l > 0 {
			p := &rects[l-1]
			if p.Left == x0 && p.Right == x1 && p.Bottom == y0 {
				p.Bottom = y1
				return
			}
		}
		rects = append(rects, Rect{Left: x0, Top: y0, Right: x1, Bottom: y1})
	}
	for y := range z {
		x := inset(y)
		add(x, y, w-x, y+1)
	}
	add(0, z, w, ht-z)
	for y := ht - z; y < ht; y++ {
		x := inset(ht - 1 - y)
		add(x, y, w-x, y+1)
	}
	return rects
}

// clipCoverage is how much of the pixel at (x, y) (pixels from the
// view's top left) a w by ht rounded rectangle of radius rc covers.
func clipCoverage(x, y int32, w, ht int32, rc float64) float64 {
	px, py := float64(x)+0.5, float64(y)+0.5
	var dx, dy float64
	switch {
	case px < rc:
		dx = rc - px
	case px > float64(w)-rc:
		dx = px - (float64(w) - rc)
	default:
		return 1
	}
	switch {
	case py < rc:
		dy = rc - py
	case py > float64(ht)-rc:
		dy = py - (float64(ht) - rc)
	default:
		return 1
	}
	return min(max(rc-math.Hypot(dx, dy)+0.5, 0), 1)
}

// paintClipCorners draws the corners' edge pixels of n's children that
// clip rounded, into dc (n's client coordinates).
func (h *Host) paintClipCorners(n *node, hwnd, dc uintptr) {
	if h.roundClips == 0 {
		return
	}
	for _, c := range n.children {
		if c.clipRects != nil && c.hwnd != 0 && c != h.clipOff && c.pict == nil && IsWindowVisible(c.hwnd) && Parent(c.hwnd) == hwnd {
			h.paintCorners(c, hwnd, dc)
		}
	}
}

// corners are the squares of c's window's corners, in its parent's
// client coordinates.
func corners(c *node, at Rect) [4]Rect {
	z := int32(math.Ceil(c.clipR))
	return [4]Rect{
		{Left: at.Left, Top: at.Top, Right: at.Left + z, Bottom: at.Top + z},
		{Left: at.Right - z, Top: at.Top, Right: at.Right, Bottom: at.Top + z},
		{Left: at.Left, Top: at.Bottom - z, Right: at.Left + z, Bottom: at.Bottom},
		{Left: at.Right - z, Top: at.Bottom - z, Right: at.Right, Bottom: at.Bottom},
	}
}

// paintCorners draws c's tree into its corners' squares, over what dc has
// there, mixed by how much its rounded rectangle covers each pixel.
func (h *Host) paintCorners(c *node, parent, dc uintptr) {
	box, ok := ClipBox(dc)
	if !ok {
		return
	}
	at := WindowRectIn(c.hwnd, parent)
	w, ht := at.W(), at.H()
	z := int32(math.Ceil(c.clipR))
	if z <= 0 {
		return
	}
	var mem, bmp uintptr
	var px, behind []byte
	for _, q := range corners(c, at) {
		if !overlaps(q, box) {
			continue
		}
		if mem == 0 {
			mem = CreateCompatibleDC(dc)
			bmp, px = DIB(z, z)
			if bmp == 0 {
				DeleteDC(mem)
				return
			}
			SelectObject(mem, bmp)
			behind = make([]byte, len(px))
			defer func() { DeleteObject(bmp); DeleteDC(mem) }()
		}
		BitBlt(mem, 0, 0, z, z, dc, q.Left, q.Top)
		copy(behind, px)
		saved := SaveDC(mem)
		OffsetViewport(mem, -q.Left, -q.Top)
		off := h.clipOff
		h.clipOff = c // drawn whole: its region would leave out the edge
		h.paintTree(c.hwnd, parent, mem)
		h.clipOff = off
		RestoreDC(mem, saved)
		for y := range z {
			for x := range z {
				i := int(y*z+x) * 4
				cov := clipCoverage(q.Left-at.Left+x, q.Top-at.Top+y, w, ht, c.clipR)
				if cov >= 1 {
					continue
				}
				for j := range 3 {
					px[i+j] = uint8(float64(behind[i+j])*(1-cov) + float64(px[i+j])*cov + 0.5)
				}
				px[i+3] = 255
			}
		}
		BitBlt(dc, q.Left, q.Top, z, z, mem, 0, 0)
	}
}

// clipTree narrows dc's clip to n's rounded clip, if it has one, while
// paintTree draws n's tree (dc's origin at n's).
func (h *Host) clipTree(n *node, dc uintptr) {
	if n.clipRects == nil || n == h.clipOff {
		return
	}
	o := ViewportOrigin(dc)
	rects := make([]Rect, len(n.clipRects))
	for i, r := range n.clipRects {
		rects[i] = Rect{Left: r.Left + o.X, Top: r.Top + o.Y, Right: r.Right + o.X, Bottom: r.Bottom + o.Y}
	}
	if rgn := RegionOf(rects); rgn != 0 {
		IntersectClipRegion(dc, rgn)
		DeleteObject(rgn)
	}
}

// cornersPainted has the parents of the rounded clips a window is in
// draw the corners it painted over again.
func (h *Host) cornersPainted(hwnd uintptr) {
	if h.roundClips == 0 {
		return
	}
	var r Rect
	for w := hwnd; w != 0; w = Parent(w) {
		c := h.byHwnd[w]
		if c == nil || c.hwnd != w || c.kind == kindSheetWindow || c.kind == kindWindowFrame || c.kind == kindPopoverWindow || c.kind == kindAlertWindow || w == h.window {
			return
		}
		if c.clipRects == nil {
			continue
		}
		p := Parent(w)
		if p == 0 {
			return
		}
		if r == (Rect{}) {
			r = WindowRect(hwnd)
		}
		at := WindowRectIn(w, p)
		o := MapPoint(0, p, Point{})
		painted := Rect{Left: r.Left + o.X, Top: r.Top + o.Y, Right: r.Right + o.X, Bottom: r.Bottom + o.Y}
		for _, q := range corners(c, at) {
			if overlaps(q, painted) {
				RedrawRect(p, q)
			}
		}
	}
}

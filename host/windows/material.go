//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Style.Material: a translucent background that blurs what's behind,
// Fluent's acrylic in a child window (DWM's backdrops are top-level
// windows' only). The view draws what's behind it, as any view that isn't
// opaque does, then blurs it (three box blurs, a Gaussian's likeness) and
// tints it light or dark, under its own drawing. As what's behind it
// paints, it draws again.

// materialLook is a material's blur (points, each box's half width) and
// tint's opacity.
func materialLook(m ops.Material) (blur float32, tint float32) {
	switch m {
	case ops.MaterialUltraThin:
		return 6, 0.3
	case ops.MaterialThin:
		return 7, 0.45
	case ops.MaterialThick:
		return 8, 0.8
	case ops.MaterialBar:
		return 8, 0.7
	}
	return 8, 0.6 // Regular
}

func (h *Host) setMaterial(n *node, m ops.Material) {
	if (m != ops.MaterialNone) != (n.material != ops.MaterialNone) {
		if m != ops.MaterialNone {
			h.materials++
		} else {
			h.materials--
		}
	}
	n.material = m
	h.redraw(n)
}

// paintMaterial blurs and tints what dc has behind n (w by ht pixels,
// its client area), inside its rounded corners.
func (h *Host) paintMaterial(n *node, dc uintptr, w, ht int32) {
	box, ok := ClipBox(dc)
	if !ok || w <= 0 || ht <= 0 {
		return
	}
	k := h.scaleOf(n)
	blur, tint := materialLook(n.material)
	b := int32(math.Ceil(float64(blur * k)))
	// What's drawn, and around it what blurs into it.
	area := Rect{Left: max(box.Left-3*b, 0), Top: max(box.Top-3*b, 0), Right: min(box.Right+3*b, w), Bottom: min(box.Bottom+3*b, ht)}
	aw, ah := area.W(), area.H()
	if aw <= 0 || ah <= 0 {
		return
	}
	mem := CreateCompatibleDC(dc)
	bmp, px := DIB(aw, ah)
	if bmp == 0 {
		DeleteDC(mem)
		return
	}
	old := SelectObject(mem, bmp)
	BitBlt(mem, 0, 0, aw, ah, dc, area.Left, area.Top)
	behind := append([]byte(nil), px...)
	tmp := h.blurBuf(len(px))
	for range 3 {
		boxBlur(px, tmp, int(aw), int(ah), int(b), true)
		boxBlur(tmp, px, int(aw), int(ah), int(b), false)
	}
	c := rgba{28, 28, 28, 255}
	if !h.dark {
		c, tint = rgba{252, 252, 252, 255}, min(tint+0.1, 1)
	}
	radius := float64(min(n.cornerRadius*k, float32(w)/2, float32(ht)/2))
	tb, tg, tr := float64(c.b)*float64(tint), float64(c.g)*float64(tint), float64(c.r)*float64(tint)
	for y := range ah {
		for x := range aw {
			i := int(y*aw+x) * 4
			cov := 1.0
			if radius > 0 {
				cov = clipCoverage(area.Left+x, area.Top+y, w, ht, radius) // clip.go
			}
			bl := float64(px[i])*(1-float64(tint)) + tb
			gr := float64(px[i+1])*(1-float64(tint)) + tg
			rd := float64(px[i+2])*(1-float64(tint)) + tr
			px[i] = uint8(float64(behind[i])*(1-cov) + bl*cov + 0.5)
			px[i+1] = uint8(float64(behind[i+1])*(1-cov) + gr*cov + 0.5)
			px[i+2] = uint8(float64(behind[i+2])*(1-cov) + rd*cov + 0.5)
			px[i+3] = 255
		}
	}
	BitBlt(dc, area.Left, area.Top, aw, ah, mem, 0, 0)
	SelectObject(mem, old)
	DeleteObject(bmp)
	DeleteDC(mem)
}

// blurBuf is a buffer of n bytes for the blur, reused.
func (h *Host) blurBuf(n int) []byte {
	if cap(h.blurTmp) < n {
		h.blurTmp = make([]byte, n)
	}
	return h.blurTmp[:n]
}

// boxBlur blurs src into dst (BGRA, w by ht) with a box of half width r,
// along rows or columns; the edges' pixels extend past them.
func boxBlur(src, dst []byte, w, ht, r int, rows bool) {
	if r <= 0 {
		copy(dst, src)
		return
	}
	n, lines, step, stride := w, ht, 4, w*4
	if !rows {
		n, lines, step, stride = ht, w, w*4, 4
	}
	d := 2*r + 1
	for l := range lines {
		base := l * stride
		for c := range 3 {
			at := func(i int) int { return int(src[base+min(max(i, 0), n-1)*step+c]) }
			sum := 0
			for i := -r; i <= r; i++ {
				sum += at(i)
			}
			for i := range n {
				dst[base+i*step+c] = uint8((sum + d/2) / d)
				sum += at(i+r+1) - at(i-r)
			}
		}
	}
}

// materialsBehind draws again the materials over a window that painted:
// what's behind them changed.
func (h *Host) materialsBehind(hwnd uintptr) {
	if h.materials == 0 {
		return
	}
	r := WindowRect(hwnd)
	for _, m := range h.nodes {
		if m.material == ops.MaterialNone || m.hwnd == 0 || m.hwnd == hwnd || !IsWindowVisible(m.hwnd) {
			continue
		}
		if IsChild(m.hwnd, hwnd) || IsChild(hwnd, m.hwnd) || !overlaps(WindowRect(m.hwnd), r) || !above(m.hwnd, hwnd) {
			continue
		}
		RedrawWindow(m.hwnd, RDW_INVALIDATE|RDW_ALLCHILDREN)
	}
}

// above reports whether window a is drawn over window b: of the windows
// they're in, the ones that share a parent, a's comes before b's.
func above(a, b uintptr) bool {
	var as []uintptr
	for w := a; w != 0; w = Parent(w) {
		as = append(as, w)
	}
	for w := b; w != 0; w = Parent(w) {
		p := Parent(w)
		for i, x := range as {
			if i+1 < len(as) && as[i+1] == p && p != 0 {
				// x and w are siblings: x above w if it comes first.
				for s := NextWindow(w, GW_HWNDPREV); s != 0; s = NextWindow(s, GW_HWNDPREV) {
					if s == x {
						return true
					}
				}
				return false
			}
		}
	}
	return false
}

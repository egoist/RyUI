//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Pictures: what a child window can't be, a view of the host's (a
// picture, above the view in its parent, taking no clicks) draws: a
// rotated view, turned about its center, and a view with child windows
// at an opacity between 0 and 1, faded as a whole (opacity.go). The
// picture draws what's behind it, then the view's tree as it paints,
// turned or blended. A rotated view's own window is shaped to nothing
// meanwhile, so neither it nor what's in it shows or takes clicks: a
// turned view is drawn, not used. A faded one stays, under its picture,
// and takes clicks. What's in it painting, or moving, draws the picture
// again.

const kindPicture ops.Kind = 0xF100

// picture is a view's picture.
type picture struct {
	pic    *node   // the picture's view
	deg    float32 // its turn, degrees clockwise
	shaped bool    // the view's window is shaped to nothing
}

// updatePicture gives n a picture if it's rotated or faded with child
// windows, or lets it go.
func (h *Host) updatePicture(n *node) {
	if n.hwnd == 0 || n.kind == kindPicture {
		return
	}
	deg := float32(math.Mod(float64(n.xf.rot), 360))
	fade := h.faded(n)
	if deg == 0 && !fade {
		if n.pict != nil {
			h.unpicture(n)
		}
		return
	}
	if n.pict == nil {
		h.nextCell++
		p := newNode(ops.HostIDBit|h.nextCell, kindPicture)
		p.hwnd = h.makeView(0)
		p.k = n.k
		p.host = pictureMessage
		n.pict = &picture{pic: p}
		h.add(p)
		if h.pictures == nil {
			h.pictures = map[*node]*node{}
		}
		h.pictures[p] = n
		if AfterPaint == nil {
			AfterPaint = h.picturesStale
		}
		h.redraw(n) // its own drawing, unfaded: the picture fades it
	}
	n.pict.deg = deg
	if shaped := deg != 0; shaped != n.pict.shaped {
		n.pict.shaped = shaped
		if shaped {
			SetWindowRgn(n.hwnd, CreateRectRgn(0, 0, 0, 0))
		} else {
			h.restoreShape(n)
		}
	}
	h.placePicture(n)
}

// restoreShape gives n's window its own shape again: its rounded clip's,
// or none.
func (h *Host) restoreShape(n *node) {
	var rgn uintptr
	if n.clipRects != nil {
		rgn = RegionOf(n.clipRects) // clip.go
	}
	SetWindowRgn(n.hwnd, rgn)
}

// unpicture lets n's picture go and shows n again.
func (h *Host) unpicture(n *node) {
	p := n.pict
	n.pict = nil
	h.dropPicture(p.pic)
	if n.hwnd != 0 {
		if p.shaped {
			h.restoreShape(n)
		}
		h.redraw(n)
	}
}

// dropPicture lets a picture's view go.
func (h *Host) dropPicture(p *node) {
	delete(h.pictures, p)
	delete(h.nodes, p.id)
	h.release(p)
}

// placePicture places n's picture over n (its turned bounds), just
// above it.
func (h *Host) placePicture(n *node) {
	if n.pict == nil || n.hwnd == 0 {
		return
	}
	p := n.pict.pic
	parent := Parent(n.hwnd)
	if Parent(p.hwnd) != parent {
		SetParent(p.hwnd, parent)
	}
	b := WindowRectIn(n.hwnd, parent)
	if n.pict.deg != 0 {
		b = turnedBounds(b, n.pict.deg)
	}
	after := NextWindow(n.hwnd, GW_HWNDPREV)
	if after == 0 {
		after = HWND_TOP
	}
	if after == p.hwnd {
		SetWindowPos(p.hwnd, 0, b.Left, b.Top, b.W(), b.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	} else {
		SetWindowPos(p.hwnd, after, b.Left, b.Top, b.W(), b.H(), SWP_NOACTIVATE|SWP_NOCOPYBITS)
	}
	vis := IsWindowVisible(n.hwnd)
	if vis != IsWindowVisible(p.hwnd) {
		if vis {
			ShowWindow(p.hwnd, SW_SHOWNA)
		} else {
			ShowWindow(p.hwnd, SW_HIDE)
		}
	}
	Invalidate(p.hwnd)
}

// turnedBounds is the rectangle around r turned deg degrees about its
// center, a pixel more for the antialiased edge.
func turnedBounds(r Rect, deg float32) Rect {
	cx, cy := float64(r.Left+r.Right)/2, float64(r.Top+r.Bottom)/2
	w, ht := float64(r.W())/2, float64(r.H())/2
	a := float64(deg) * math.Pi / 180
	c, s := math.Abs(math.Cos(a)), math.Abs(math.Sin(a))
	hw, hh := w*c+ht*s, w*s+ht*c
	return Rect{
		Left: int32(math.Floor(cx-hw)) - 1, Top: int32(math.Floor(cy-hh)) - 1,
		Right: int32(math.Ceil(cx+hw)) + 1, Bottom: int32(math.Ceil(cy+hh)) + 1,
	}
}

// pictureMessage paints a picture: what's behind it (not its view),
// then its view's tree.
func pictureMessage(h *Host, p *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_PAINT:
		var ps PaintStruct
		dc := BeginPaint(p.hwnd, &ps)
		r := ClientRect(p.hwnd)
		if r.W() > 0 && r.H() > 0 {
			pb, mem := BeginBufferedPaint(dc, &r)
			if mem == 0 {
				mem = dc
			}
			h.paintPicture(p, mem, true)
			EndBufferedPaint(pb)
		}
		EndPaint(p.hwnd, &ps)
		return 0, true
	case WM_PRINTCLIENT:
		h.paintPicture(p, wp, true)
		return 0, true
	}
	return 0, false
}

// paintPicture draws picture p into dc (its client coordinates): what's
// behind it, if behind, then its view's tree, turned or faded.
func (h *Host) paintPicture(p *node, dc uintptr, behind bool) {
	n := h.pictures[p]
	if n == nil || n.hwnd == 0 || n.pict == nil {
		return
	}
	if behind {
		skip := h.skipTree
		h.skipTree = n.hwnd // the picture draws it
		h.paintBehind(p.hwnd, dc)
		h.skipTree = skip
	}
	parent := Parent(n.hwnd)
	at := WindowRectIn(n.hwnd, parent)
	pr := WindowRectIn(p.hwnd, parent)
	w, ht := at.W(), at.H()
	if w <= 0 || ht <= 0 {
		return
	}
	if n.pict.deg == 0 {
		h.paintFaded(n, parent, dc, Rect{Left: at.Left - pr.Left, Top: at.Top - pr.Top, Right: at.Right - pr.Left, Bottom: at.Bottom - pr.Top}, at)
		return
	}
	px := h.treePicture(n, parent, at)
	if px == nil {
		return
	}
	if a := n.opacity; a < 1 {
		for i := range px {
			px[i] = uint8(float32(px[i])*a + 0.5)
		}
	}
	b := NewBitmap(w, ht, px)
	if b == nil {
		return
	}
	defer b.Close()
	g := NewGraphics(dc)
	defer g.Close()
	cx, cy := float32(at.Left+at.Right)/2-float32(pr.Left), float32(at.Top+at.Bottom)/2-float32(pr.Top)
	g.Translate(cx, cy)
	g.Rotate(n.pict.deg)
	g.DrawImage(b, -float32(w)/2, -float32(ht)/2, float32(w), float32(ht), 0, 0, float32(w), float32(ht))
}

// drawTree draws n's tree into dc, n's top left at (x, y) of it, as it
// paints.
func (h *Host) drawTree(n *node, parent, dc uintptr, at Rect, x, y int32) {
	saved := SaveDC(dc)
	OffsetViewport(dc, x-at.Left, y-at.Top)
	was := h.drawing
	h.drawing = n
	h.paintTree(n.hwnd, parent, dc)
	h.drawing = was
	RestoreDC(dc, saved)
}

// paintFaded draws n's tree over what dc has at r (n's place in it) and
// blends it with that at n's opacity.
func (h *Host) paintFaded(n *node, parent, dc uintptr, r, at Rect) {
	w, ht := r.W(), r.H()
	mem := CreateCompatibleDC(dc)
	bmp, px := DIB(w, ht)
	if bmp == 0 {
		DeleteDC(mem)
		return
	}
	old := SelectObject(mem, bmp)
	BitBlt(mem, 0, 0, w, ht, dc, r.Left, r.Top)
	h.drawTree(n, parent, mem, at, 0, 0)
	for i := 3; i < len(px); i += 4 {
		px[i] = 255
	}
	AlphaBlend(dc, r.Left, r.Top, w, ht, mem, 0, 0, w, ht, alpha255(n.opacity))
	SelectObject(mem, old)
	DeleteObject(bmp)
	DeleteDC(mem)
}

// treePicture is n's tree drawn as it would show, unturned, w by ht
// pixels of premultiplied BGRA: drawn over black and over white, which
// tells each pixel's coverage, so what's behind it doesn't show.
func (h *Host) treePicture(n *node, parent uintptr, at Rect) []byte {
	w, ht := at.W(), at.H()
	draw := func(fill uint8) []byte {
		mem := CreateCompatibleDC(0)
		bmp, px := DIB(w, ht)
		if bmp == 0 {
			DeleteDC(mem)
			return nil
		}
		old := SelectObject(mem, bmp)
		for i := range px {
			px[i] = fill
		}
		h.drawTree(n, parent, mem, at, 0, 0)
		out := append([]byte(nil), px...)
		SelectObject(mem, old)
		DeleteObject(bmp)
		DeleteDC(mem)
		return out
	}
	black, white := draw(0), draw(255)
	if black == nil || white == nil {
		return nil
	}
	for i := 0; i+3 < len(black); i += 4 {
		// Over black a pixel is its color times its coverage; over white
		// that plus the white left showing.
		a := 255 - (int(white[i+1]) - int(black[i+1]))
		a = min(max(a, 0), 255)
		black[i+3] = uint8(a)
		for j := range 3 {
			black[i+j] = uint8(min(int(black[i+j]), a))
		}
	}
	return black
}

// treeInPicture is whether paintTree leaves n's tree to its picture: it
// has one, and the picture isn't drawing it.
func (h *Host) treeInPicture(n *node) bool {
	return n.pict != nil && h.drawing != n
}

// picturesStale draws again the pictures of the views hwnd is in: it
// painted.
func (h *Host) picturesStale(hwnd uintptr) {
	if len(h.pictures) == 0 {
		return
	}
	for w := hwnd; w != 0; w = Parent(w) {
		n := h.byHwnd[w]
		if n == nil || n.kind == kindSheetWindow || n.kind == kindWindowFrame || n.kind == kindPopoverWindow || n.kind == kindAlertWindow || w == h.window {
			return
		}
		if n.hwnd == w && n.pict != nil {
			Invalidate(n.pict.pic.hwnd)
		}
	}
}

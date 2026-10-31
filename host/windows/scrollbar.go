//go:build windows && amd64

package windows

import (
	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// A Scroll's or List's scroll bar is an indicator over its content, as
// WinUI's (and iOS's, Android's, macOS's) are, not a Win32 scroll bar,
// which would take its width from the content the core laid out: a
// thin rounded thumb, a window of its own shaped by a region so the
// content shows around it. It shows while the view scrolls and while
// the mouse moves over it, grows under the mouse, and drags.
type scrollBar struct {
	n          *node // the host node of the thumb's window
	view       *node // the Scroll or List
	horizontal bool
	visible    bool
	dragging   bool
	dragAt     int32
	dragFrom   float32
	thumb      Rect
}

const timerBars = 5

func (h *Host) barOf(n *node) *scrollBar {
	if n.bar == nil {
		h.nextCell++
		t := newNode(ops.HostIDBit|h.nextCell, kindScrollBar)
		t.hwnd = h.makeView(0)
		t.k = n.k
		SetParent(t.hwnd, n.hwnd)
		ShowWindow(t.hwnd, SW_HIDE)
		t.host = scrollBarMessage
		n.bar = &scrollBar{n: t, view: n}
		t.sbar = n.bar
		h.add(t)
	}
	return n.bar
}

// extent is a scroll view's content, viewport and position along its
// axis, in points.
func (h *Host) extent(n *node) (content, view, pos float32, horizontal bool) {
	r := ClientRect(n.hwnd)
	if n.list != nil {
		l := n.list
		if len(l.tops) > 0 {
			content = l.tops[len(l.tops)-1]
		}
		return content, h.pt(n, r.H()), l.offset, false
	}
	if n.horizontal {
		return n.contentW, h.pt(n, r.W()), n.scrollX, true
	}
	return n.contentH, h.pt(n, r.H()), n.scrollY, false
}

// updateBars places a scroll view's thumb at its position, showing it
// if it's been scrolling or under the mouse lately.
func (h *Host) updateBars(n *node) {
	if n.hwnd == 0 {
		return
	}
	content, view, pos, horizontal := h.extent(n)
	if content <= view+0.5 || view <= 0 {
		if n.bar != nil {
			n.bar.visible = false
			ShowWindow(n.bar.n.hwnd, SW_HIDE)
		}
		return
	}
	b := h.barOf(n)
	b.horizontal = horizontal
	if !b.visible {
		return
	}
	r := ClientRect(n.hwnd)
	thick := h.px(n, 4)
	if b.n.hot || b.dragging {
		thick = h.px(n, 8)
	}
	inset := h.px(n, 2)
	along := r.H()
	if horizontal {
		along = r.W()
	}
	length := max(int32(float32(along)*view/content), h.px(n, 24))
	at := int32(float32(along-length) * pos / (content - view))
	var t Rect
	if horizontal {
		t = Rect{Left: at, Top: r.H() - thick - inset, Right: at + length, Bottom: r.H() - inset}
	} else {
		t = Rect{Left: r.W() - thick - inset, Top: at, Right: r.W() - inset, Bottom: at + length}
	}
	if t != b.thumb {
		b.thumb = t
		SetWindowRgn(b.n.hwnd, CreateRoundRectRgn(0, 0, t.W()+1, t.H()+1, thick, thick))
	}
	SetWindowPos(b.n.hwnd, HWND_TOP, t.Left, t.Top, t.W(), t.H(), SWP_NOACTIVATE|SWP_SHOWWINDOW|SWP_NOCOPYBITS)
}

// showBars shows a scroll view's thumb for a moment (it scrolled, or
// the mouse moved over it).
func (h *Host) showBars(n *node) {
	content, view, _, _ := h.extent(n)
	if content <= view+0.5 {
		return
	}
	b := h.barOf(n)
	SetTimer(n.hwnd, timerBars, 1200)
	if b.visible {
		return
	}
	b.visible = true
	h.updateBars(n)
}

// hideBars hides the thumb once the mouse has left it alone.
func (h *Host) hideBars(n *node) {
	KillTimer(n.hwnd, timerBars)
	b := n.bar
	if b == nil || b.n.hot || b.dragging {
		return
	}
	b.visible = false
	ShowWindow(b.n.hwnd, SW_HIDE)
}

// mouseOver shows the thumbs of the scroll views the mouse moves in.
func (h *Host) mouseOver(hwnd uintptr) {
	for w := hwnd; w != 0 && w != h.window; w = Parent(w) {
		if n := h.byHwnd[w]; n != nil && n.hwnd == w && (n.kind == ops.KindScroll || n.list != nil) {
			h.showBars(n)
		}
	}
}

func scrollBarMessage(h *Host, t *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	b := t.sbar
	n := b.view
	switch m {
	case wmNCHitTest:
		return 1, true // HTCLIENT
	case WM_MOUSEMOVE:
		if !t.hot {
			t.hot = true
			TrackLeave(t.hwnd)
			b.thumb = Rect{}
			h.updateBars(n)
		}
		if b.dragging {
			p := ClientToScreen(t.hwnd, Point{X: XParam(lp), Y: YParam(lp)})
			at := p.Y
			if b.horizontal {
				at = p.X
			}
			content, view, _, _ := h.extent(n)
			r := ClientRect(n.hwnd)
			along := r.H()
			if b.horizontal {
				along = r.W()
			}
			d := h.pt(n, at-b.dragAt) * content / max(h.pt(n, along), 1)
			_ = view
			if b.horizontal {
				h.scrollToOffset(n, b.dragFrom+d, n.scrollY)
			} else {
				h.scrollToOffset(n, n.scrollX, b.dragFrom+d)
			}
		}
		return 0, true
	case WM_MOUSELEAVE:
		t.hot = false
		b.thumb = Rect{}
		h.updateBars(n)
		SetTimer(n.hwnd, timerBars, 1200)
		return 0, true
	case WM_LBUTTONDOWN:
		SetCapture(t.hwnd)
		b.dragging = true
		p := ClientToScreen(t.hwnd, Point{X: XParam(lp), Y: YParam(lp)})
		_, _, pos, _ := h.extent(n)
		b.dragAt, b.dragFrom = p.Y, pos
		if b.horizontal {
			b.dragAt = p.X
		}
		return 0, true
	case WM_LBUTTONUP, WM_CAPTURECHANGED:
		if b.dragging {
			b.dragging = false
			if Capture() == t.hwnd {
				ReleaseCapture()
			}
			SetTimer(n.hwnd, timerBars, 1200)
		}
		return 0, true
	case WM_ERASEBKGND:
		return 1, true
	case WM_PAINT:
		var ps PaintStruct
		dc := BeginPaint(t.hwnd, &ps)
		c := hex(0x8A8A8A)
		if h.dark {
			c = hex(0x9A9A9A)
		}
		if t.hot || b.dragging {
			c = hex(0x6E6E6E)
			if h.dark {
				c = hex(0xCECECE)
			}
		}
		FillColor(dc, ClientRect(t.hwnd), c.ref())
		EndPaint(t.hwnd, &ps)
		return 0, true
	}
	return 0, false
}

//go:build linux && !android

package linux

import (
	"math"

	"github.com/ebitengine/purego"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// AsukaView: the host's widget class. A node's widget draws what the
// node's style gives it (snapshot) and places its children at their
// frames (size_allocate); it asks for no size of its own (measure), as
// the core lays everything out.

var (
	viewType        GType
	widgetClass     Ptr // GtkWidgetClass, AsukaView's parent class
	FnQueueAllocate uintptr
)

func registerClasses() {
	if viewType != 0 {
		return
	}
	FnQueueAllocate = Sym("gtk_widget_queue_allocate")
	widgetType := Call(Sym("gtk_widget_get_type"))
	snapshot := purego.NewCallback(func(widget, snap uintptr) uintptr {
		theHost.snapshotView(widget, snap)
		return 0
	})
	allocate := purego.NewCallback(func(widget uintptr, w, h, baseline int32) uintptr {
		theHost.allocateView(widget, w, h)
		return 0
	})
	measure := purego.NewCallback(func(widget uintptr, orientation, forSize int32, min, nat, minB, natB uintptr) uintptr {
		m, nt := theHost.measureView(widget, orientation)
		*At[int32](min) = m
		*At[int32](nat) = nt
		if minB != 0 {
			*At[int32](minB) = -1
		}
		if natB != 0 {
			*At[int32](natB) = -1
		}
		return 0
	})
	// A container widget unparents its children as it's disposed (GTK
	// 4's rule), then disposes as a widget.
	dispose := purego.NewCallback(func(obj uintptr) uintptr {
		for c := Call(FnWidgetFirstChild, obj); c != 0; c = Call(FnWidgetFirstChild, obj) {
			WidgetUnparent(c)
		}
		Call(Vfunc(widgetClass, OffObjectDispose), obj)
		return 0
	})
	viewType = Subclass(widgetType, "AsukaView", func(class, parent Ptr) {
		widgetClass = parent
		SetVfunc(class, OffObjectDispose, dispose)
		SetVfunc(class, OffWidgetSnapshot, snapshot)
		SetVfunc(class, OffWidgetSizeAllocate, allocate)
		SetVfunc(class, OffWidgetMeasure, measure)
		css := CString("asuka-view")
		Call(Sym("gtk_widget_class_set_css_name"), class, Addr(css))
	})
}

// newView makes a AsukaView the host holds a reference to.
func newView() Ptr {
	w := NewObject(viewType)
	ObjectRefSink(w)
	return w
}

// isView reports whether w is a AsukaView.
func isView(w Ptr) bool { return TypeCheckInstanceIsA(w, viewType) }

// measureView is a AsukaView's size request: none, but for a scroll's
// content, whose frame is what scrolls.
func (h *Host) measureView(w Ptr, orientation int32) (min, nat int32) {
	n := h.byWidget[w]
	if n == nil || n.parent == nil || n.parent.scroll == nil {
		return 0, 0
	}
	// Its minimum too: a viewport scrolls its child's minimum size
	// (GtkScrollable's default policy).
	s := n.frame[3]
	if orientation == OrientationHorizontal {
		s = n.frame[2]
	}
	v := int32(math.Ceil(float64(s)))
	return v, v
}

// allocateView places a view's children: the core's nodes at their
// frames, its control filling it less the inset.
func (h *Host) allocateView(w Ptr, width, height int32) {
	if win := h.winByContent(w); win != nil {
		win.allocated(h, width, height)
		return
	}
	n := h.byWidget[w]
	if n == nil {
		return
	}
	if n.isLayoutRoot() {
		h.rootAllocated(n, width, height)
	}
	for c := Call(FnWidgetFirstChild, w); c != 0; c = Call(FnWidgetNextSibling, c) {
		// A node's view; or one going out (a transition, a match: no
		// parent any more), at the frame it goes with.
		if cn := h.byWidget[c]; cn != nil && (cn.parent == n || cn.parent == nil && cn.placed) {
			h.allocateChild(cn)
			continue
		}
		if c == n.inner {
			l, t := n.inset[3], n.inset[0]
			iw := max(int32(width)-int32(math.Round(float64(n.inset[1]+l))), 0)
			ih := max(int32(height)-int32(math.Round(float64(n.inset[0]+n.inset[2]))), 0)
			allocate(c, iw, ih, l, t, nil)
			continue
		}
		if IsPopover(c) {
			Call(FnPopoverPresent, c) // a native: sized and placed by itself
			continue
		}
		allocate(c, width, height, 0, 0, nil)
	}
}

// allocateChild places a node's view at its frame, with its transform.
func (h *Host) allocateChild(c *node) {
	f := c.frame
	var t *transform
	if c.transform.set {
		t = &c.transform
	}
	if a := h.frameNow(c); a != nil {
		f = *a
	}
	if p := c.parent; p != nil && p.kind == ops.KindRichText {
		h.placeInline(p, c, &f) // an inline component: where the text puts it
	}
	allocate(c.widget, int32(math.Ceil(float64(f[2]))), int32(math.Ceil(float64(f[3]))), f[0], f[1], t)
	if c.scroll != nil {
		// The content may have grown since: the viewport measures it.
	}
}

// allocate gives w a size and its place (fractional points through the
// transform, as GTK's sizes are whole), with t about its center.
func allocate(w Ptr, width, height int32, x, y float32, t *transform) {
	p := Point{X: x, Y: y}
	tr := Call(FnTransformTranslate, 0, Addr(&p))
	if t != nil {
		cx, cy := float32(width)/2, float32(height)/2
		c := Point{X: cx + t.tx, Y: cy + t.ty}
		tr = Call(FnTransformTranslate, tr, Addr(&c))
		if t.rotation != 0 {
			tr = TransformRotate(tr, t.rotation)
		}
		if t.sx != 1 || t.sy != 1 {
			tr = TransformScale(tr, t.sx, t.sy)
		}
		back := Point{X: -cx, Y: -cy}
		tr = Call(FnTransformTranslate, tr, Addr(&back))
	}
	// gtk_widget_allocate takes the transform.
	Call(FnWidgetAllocate, w, uintptr(max(width, 0)), uintptr(max(height, 0)), ^uintptr(0), tr)
}

// size is the size a node's view draws at: its frame's, once it has one
// (an allocation is whole points), else its allocation.
func (h *Host) size(n *node, w Ptr) (float32, float32) {
	if n != nil && n.placed && !n.isLayoutRoot() {
		return n.frame[2], n.frame[3]
	}
	return float32(Call(FnWidgetGetWidth, w)), float32(Call(FnWidgetGetHeight, w))
}

// snapshotView draws a view: its shadow, background (or gradient), its
// children (clipped to its shape when it clips) and its border over them,
// as a layer draws its border.
func (h *Host) snapshotView(w, snap Ptr) {
	n := h.byWidget[w]
	if n == nil {
		if win := h.winByContent(w); win != nil {
			bg := h.theme.bg
			r := Rect{W: float32(Call(FnWidgetGetWidth, w)), H: float32(Call(FnWidgetGetHeight, w))}
			Call(FnSnapshotAppendColor, snap, Addr(&bg), Addr(&r))
		}
		h.snapshotChildren(w, snap)
		return
	}
	width, height := h.size(n, w)
	bounds := Rect{W: width, H: height}
	rr := Rounded(bounds, n.cornerRadius)
	if n.shadow.color != 0 {
		if c, ok := h.resolve(n.shadow.color); ok && c.A > 0 {
			SnapshotAppendOutsetShadow(snap, &rr, &c, n.shadow.dx, n.shadow.dy, 0, n.shadow.blur)
		}
	}
	rounded := n.cornerRadius > 0
	if len(n.gradient) > 0 {
		h.drawGradient(n, snap, bounds, &rr)
	} else if c, ok := h.background(n); ok && c.A > 0 {
		if rounded {
			Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
		}
		Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&bounds))
		if rounded {
			Call(FnSnapshotPop, snap)
		}
	}
	// A video's corners round it, as its layer's do on macOS.
	clip := n.clip || n.kind == ops.KindScroll || n.kind == ops.KindVideo && rounded
	if clip {
		if rounded {
			Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
		} else {
			Call(FnSnapshotPushClip, snap, Addr(&bounds))
		}
	}
	h.snapshotContent(n, w, snap, bounds)
	h.snapshotChildren(w, snap)
	if clip {
		Call(FnSnapshotPop, snap)
	}
	if n.borderWidth > 0 {
		if c, ok := h.resolve(n.borderColor); ok && c.A > 0 {
			bw := [4]float32{n.borderWidth, n.borderWidth, n.borderWidth, n.borderWidth}
			cs := [4]RGBA{c, c, c, c}
			Call(FnSnapshotAppendBorder, snap, Addr(&rr), Addr(&bw), Addr(&cs))
		}
	}
}

func (h *Host) snapshotChildren(w, snap Ptr) {
	for c := Call(FnWidgetFirstChild, w); c != 0; c = Call(FnWidgetNextSibling, c) {
		Call(FnWidgetSnapshotChild, w, c, snap)
	}
}

// background is the color behind a node's content: its Background, or a
// material's translucent surface (GTK can't blur what's behind a view).
func (h *Host) background(n *node) (RGBA, bool) {
	if n.background != 0 {
		return h.resolve(n.background)
	}
	if n.material != 0 {
		c := h.theme.card
		c.A = []float32{0, 0.6, 0.7, 0.8, 0.9, 0.85}[min(int(n.material), 5)]
		return c, true
	}
	return RGBA{}, false
}

func (h *Host) drawGradient(n *node, snap Ptr, bounds Rect, rr *RoundedRect) {
	stops := make([]ColorStop, len(n.gradient))
	last := float32(0)
	for i, s := range n.gradient {
		c, _ := h.resolve(s.color)
		off := min(max(s.location, last), 1) // GSK wants them in order
		stops[i] = ColorStop{Offset: off, Color: c}
		last = off
	}
	start := Point{X: n.gradientLine[0] * bounds.W, Y: n.gradientLine[1] * bounds.H}
	end := Point{X: n.gradientLine[2] * bounds.W, Y: n.gradientLine[3] * bounds.H}
	if start == end {
		end.Y += 1
	}
	if n.cornerRadius > 0 {
		Call(FnSnapshotPushRoundedClip, snap, Addr(rr))
	}
	Call(FnSnapshotAppendLinearGradient, snap, Addr(&bounds), Addr(&start), Addr(&end), Addr(&stops[0]), uintptr(len(stops)))
	if n.cornerRadius > 0 {
		Call(FnSnapshotPop, snap)
	}
}

// queueDraw redraws a node's view.
func queueDraw(n *node) {
	if n.widget != 0 {
		Call(FnWidgetQueueDraw, n.widget)
	}
}

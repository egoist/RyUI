//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A Shape is a RyUIShapeView whose layer has a CAShapeLayer filling it,
// its path in the view's (flipped) points.
type shape struct {
	layer        ID
	fill, stroke ops.Color
	lineWidth    float64
}

func registerShapeClass(flipped objc.MethodDef) {
	DefineClass("RyUIShapeView", "NSView", nil, []objc.MethodDef{
		flipped,
		Method("hitTest:", func(self ID, _ SEL, p Point) ID { return 0 }),
	})
}

var (
	cgPathCreateMutable func() uintptr
	cgPathRelease       func(p uintptr)
	cgPathMove          func(p, t uintptr, x, y float64)
	cgPathLine          func(p, t uintptr, x, y float64)
	cgPathQuad          func(p, t uintptr, cx, cy, x, y float64)
	cgPathCubic         func(p, t uintptr, c1x, c1y, c2x, c2y, x, y float64)
	cgPathClose         func(p uintptr)
	cgPathRect          func(p, t uintptr, r Rect)
	cgPathRoundedRect   func(p, t uintptr, r Rect, w, h float64)
	cgPathEllipse       func(p, t uintptr, r Rect)
	cgPathArc           func(p, t uintptr, x, y, r, start, end float64, clockwise bool)
	cgLoaded            bool
)

func loadCG() {
	if cgLoaded {
		return
	}
	cgLoaded = true
	lib := CoreGraphics()
	purego.RegisterLibFunc(&cgPathCreateMutable, lib, "CGPathCreateMutable")
	purego.RegisterLibFunc(&cgPathRelease, lib, "CGPathRelease")
	purego.RegisterLibFunc(&cgPathMove, lib, "CGPathMoveToPoint")
	purego.RegisterLibFunc(&cgPathLine, lib, "CGPathAddLineToPoint")
	purego.RegisterLibFunc(&cgPathQuad, lib, "CGPathAddQuadCurveToPoint")
	purego.RegisterLibFunc(&cgPathCubic, lib, "CGPathAddCurveToPoint")
	purego.RegisterLibFunc(&cgPathClose, lib, "CGPathCloseSubpath")
	purego.RegisterLibFunc(&cgPathRect, lib, "CGPathAddRect")
	purego.RegisterLibFunc(&cgPathRoundedRect, lib, "CGPathAddRoundedRect")
	purego.RegisterLibFunc(&cgPathEllipse, lib, "CGPathAddEllipseInRect")
	purego.RegisterLibFunc(&cgPathArc, lib, "CGPathAddArc")
}

// path builds a CGPath (+1) from the wire format: u8 PathOp, then its
// f32 arguments.
func path(b []byte) uintptr {
	loadCG()
	p := cgPathCreateMutable()
	i := 0
	f := func() float64 {
		if i+4 > len(b) {
			i = len(b) + 1
			return 0
		}
		v := float64(f32(b, i))
		i += 4
		return v
	}
	rect := func() Rect {
		x, y, w, h := f(), f(), f(), f()
		return Rect{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
	}
	for i < len(b) {
		op := ops.PathOp(b[i])
		i++
		switch op {
		case ops.PathOpMove:
			x, y := f(), f()
			cgPathMove(p, 0, x, y)
		case ops.PathOpLine:
			x, y := f(), f()
			cgPathLine(p, 0, x, y)
		case ops.PathOpQuad:
			cx, cy, x, y := f(), f(), f(), f()
			cgPathQuad(p, 0, cx, cy, x, y)
		case ops.PathOpCubic:
			c1x, c1y, c2x, c2y, x, y := f(), f(), f(), f(), f(), f()
			cgPathCubic(p, 0, c1x, c1y, c2x, c2y, x, y)
		case ops.PathOpClose:
			cgPathClose(p)
		case ops.PathOpRect:
			cgPathRect(p, 0, rect())
		case ops.PathOpRoundedRect:
			r := rect()
			rad := min(f(), r.Size.Width/2, r.Size.Height/2)
			cgPathRoundedRect(p, 0, r, rad, rad)
		case ops.PathOpEllipse:
			cgPathEllipse(p, 0, rect())
		case ops.PathOpArc:
			x, y, rad, start, end, cw := f(), f(), f(), f(), f(), f()
			// The layer is flipped (y down), as UIKit's: the screen's
			// clockwise is Core Graphics' flag false.
			cgPathArc(p, 0, x, y, rad, start, end, cw == 0)
		default:
			return p
		}
	}
	return p
}

func (s *shape) ensure(h *Host, n *node) ID {
	if s.layer == 0 {
		l := h.layer(n)
		s.layer = New("CAShapeLayer")
		Send(s.layer, "setFillColor:", 0)
		Send(s.layer, "setStrokeColor:", 0)
		Send(l, "addSublayer:", uintptr(s.layer))
	}
	return s.layer
}

func (s *shape) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	l := s.ensure(h, n)
	switch p {
	case ops.PropShapePath:
		cp := path(v.Bytes)
		s.set(h, "path", ID(cp))
		cgPathRelease(cp)
	case ops.PropShapeFill:
		s.fill = ops.Color(v.Int)
		h.withAppearance(func() { s.colors(h, n) })
	case ops.PropShapeStroke:
		s.stroke = ops.Color(v.Int)
		h.withAppearance(func() { s.colors(h, n) })
	case ops.PropShapeLineWidth:
		MsgSetFloat(l, Sel("setLineWidth:"), v.Float)
	case ops.PropShapeLineCap:
		Send(l, "setLineCap:", uintptr(String([]string{"butt", "round", "square"}[min(max(v.Int, 0), 2)])))
	case ops.PropShapeLineJoin:
		Send(l, "setLineJoin:", uintptr(String([]string{"miter", "round", "bevel"}[min(max(v.Int, 0), 2)])))
	case ops.PropShapeTrimStart:
		s.set(h, "strokeStart", NumberFloat(v.Float))
	case ops.PropShapeTrimEnd:
		s.set(h, "strokeEnd", NumberFloat(v.Float))
	case ops.PropShapeDash:
		if len(v.Bytes) < 8 {
			Send(l, "setLineDashPattern:", 0)
			break
		}
		MsgSetFloat(l, Sel("setLineDashPhase:"), float64(f32(v.Bytes, 0)))
		arr := Send(Class("NSMutableArray"), "array")
		for i := 4; i+4 <= len(v.Bytes); i += 4 {
			Send(arr, "addObject:", uintptr(NumberFloat(float64(f32(v.Bytes, i)))))
		}
		Send(l, "setLineDashPattern:", uintptr(arr))
	}
}

// set sets a shape layer's key, animated inside an Animate (a view's
// animator doesn't reach a sublayer's).
func (s *shape) set(h *Host, key string, value ID) {
	k := String(key)
	if h.animating {
		a := Send(Class("CABasicAnimation"), "animationWithKeyPath:", uintptr(k))
		from := Send(s.layer, "presentationLayer")
		if from == 0 {
			from = s.layer
		}
		Send(a, "setFromValue:", uintptr(Send(from, "valueForKeyPath:", uintptr(k))))
		Send(a, "setToValue:", uintptr(value))
		MsgSetFloat(a, Sel("setDuration:"), h.anim.duration)
		Send(s.layer, "addAnimation:forKey:", uintptr(a), uintptr(k))
	}
	Send(Class("CATransaction"), "begin")
	Send(Class("CATransaction"), "setDisableActions:", 1)
	Send(s.layer, "setValue:forKeyPath:", uintptr(value), uintptr(k))
	Send(Class("CATransaction"), "commit")
}

func (s *shape) colors(h *Host, n *node) {
	if s.layer == 0 {
		return
	}
	Send(s.layer, "setFillColor:", cgColor(h.nsColor(s.fill)))
	Send(s.layer, "setStrokeColor:", cgColor(h.nsColor(s.stroke)))
}

func (s *shape) resized(n *node) {
	if s.layer == 0 {
		return
	}
	Send(Class("CATransaction"), "begin")
	Send(Class("CATransaction"), "setDisableActions:", 1)
	MsgSetRect(s.layer, selSetFrame, Rect{Size: n.frame.Size})
	Send(Class("CATransaction"), "commit")
}

//go:build darwin && !ios

package darwin

import (
	"math"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// A Canvas is a AsukaCanvasView whose drawRect: replays the canvas's
// display list with Core Graphics, in the view's appearance (semantic
// colors follow dark mode), its coordinates flipped as the core's are.

var (
	cgSave, cgRestore            func(c uintptr)
	cgTranslate, cgScale         func(c uintptr, x, y float64)
	cgRotate, cgSetAlpha         func(c uintptr, v float64)
	cgSetLineWidth               func(c uintptr, w float64)
	cgSetLineCap, cgSetLineJoin  func(c uintptr, v int32)
	cgSetLineDash                func(c uintptr, phase float64, lengths *float64, count uintptr)
	cgAddPath                    func(c, p uintptr)
	cgClip, cgFillPath, cgStroke func(c uintptr)
	cgSetFill, cgSetStroke       func(c, color uintptr)
	cgGradientCreate             func(space, colors uintptr, locations *float64) uintptr
	cgGradientRelease            func(g uintptr)
	cgDrawLinear                 func(c, g uintptr, start, end Point, options uint32)
	cgColorSpaceSRGB             uintptr
	cgContextLoaded              bool
)

func loadCGContext() {
	if cgContextLoaded {
		return
	}
	cgContextLoaded = true
	loadCG()
	lib := CoreGraphics()
	for name, fn := range map[string]any{
		"CGContextSaveGState": &cgSave, "CGContextRestoreGState": &cgRestore,
		"CGContextTranslateCTM": &cgTranslate, "CGContextScaleCTM": &cgScale,
		"CGContextRotateCTM": &cgRotate, "CGContextSetAlpha": &cgSetAlpha,
		"CGContextSetLineWidth": &cgSetLineWidth, "CGContextSetLineCap": &cgSetLineCap,
		"CGContextSetLineJoin": &cgSetLineJoin, "CGContextSetLineDash": &cgSetLineDash,
		"CGContextAddPath": &cgAddPath, "CGContextClip": &cgClip,
		"CGContextFillPath": &cgFillPath, "CGContextStrokePath": &cgStroke,
		"CGContextSetFillColorWithColor": &cgSetFill, "CGContextSetStrokeColorWithColor": &cgSetStroke,
		"CGGradientCreateWithColors": &cgGradientCreate, "CGGradientRelease": &cgGradientRelease,
		"CGContextDrawLinearGradient": &cgDrawLinear,
	} {
		purego.RegisterLibFunc(fn, lib, name)
	}
	cgColorSpaceSRGB = uintptr(Send(Send(Class("NSColorSpace"), "sRGBColorSpace"), "CGColorSpace"))
}

func registerCanvasClass(flipped objc.MethodDef) {
	DefineClass("AsukaCanvasView", "NSView", nil, []objc.MethodDef{
		flipped, keepsShadow("AsukaCanvasView"),
		Method("hitTest:", func(self ID, _ SEL, p Point) ID { return 0 }),
		Method("drawRect:", func(self ID, _ SEL, dirty Rect) {
			if n := theHost.byView[self]; n != nil {
				theHost.drawCanvas(n)
			}
		}),
	})
}

func (h *Host) drawCanvas(n *node) {
	b := n.canvas
	if len(b) == 0 {
		return
	}
	loadCGContext()
	gc := Send(Class("NSGraphicsContext"), "currentContext")
	if gc == 0 {
		return
	}
	c := uintptr(Send(gc, "CGContext"))
	h.withAppearance(func() { h.replayCanvas(c, b) })
}

func (h *Host) replayCanvas(c uintptr, b []byte) {
	i := 0
	ok := func(n int) bool { return i+n <= len(b) }
	f := func() float64 {
		if !ok(4) {
			i = len(b)
			return 0
		}
		v := float64(f32(b, i))
		i += 4
		return v
	}
	u32 := func() int {
		if !ok(4) {
			i = len(b)
			return 0
		}
		v := int(le.Uint32(b[i:]))
		i += 4
		return v
	}
	color := func() ID {
		if !ok(8) {
			i = len(b)
			return 0
		}
		v := ops.Color(le.Uint64(b[i:]))
		i += 8
		return h.nsColor(v)
	}
	path := func() uintptr {
		n := min(u32(), len(b)-i)
		p := path(b[i : i+n])
		i += n
		return p
	}
	for i < len(b) {
		op := ops.CanvasOp(b[i])
		i++
		switch op {
		case ops.CanvasOpSave:
			cgSave(c)
		case ops.CanvasOpRestore:
			cgRestore(c)
		case ops.CanvasOpTranslate:
			x, y := f(), f()
			cgTranslate(c, x, y)
		case ops.CanvasOpScale:
			x, y := f(), f()
			cgScale(c, x, y)
		case ops.CanvasOpRotate:
			cgRotate(c, f()*math.Pi/180) // flipped: clockwise on screen
		case ops.CanvasOpAlpha:
			cgSetAlpha(c, f())
		case ops.CanvasOpClip:
			p := path()
			cgAddPath(c, p)
			cgClip(c)
			cgPathRelease(p)
		case ops.CanvasOpFill:
			col := color()
			p := path()
			cgSetFill(c, cgColor(col))
			cgAddPath(c, p)
			cgFillPath(c)
			cgPathRelease(p)
		case ops.CanvasOpStroke:
			col := color()
			w := f()
			if !ok(2) {
				return
			}
			capv, join := int32(b[i]), int32(b[i+1])
			i += 2
			nd := u32()
			phase := f()
			dash := make([]float64, 0, nd)
			for range nd {
				dash = append(dash, f())
			}
			p := path()
			cgSave(c)
			cgSetStroke(c, cgColor(col))
			cgSetLineWidth(c, w)
			cgSetLineCap(c, capv)
			cgSetLineJoin(c, join)
			if len(dash) > 0 {
				cgSetLineDash(c, phase, &dash[0], uintptr(len(dash)))
			}
			cgAddPath(c, p)
			cgStroke(c)
			cgRestore(c)
			cgPathRelease(p)
		case ops.CanvasOpGradient:
			n := u32()
			cols := make([]ID, 0, n)
			locs := make([]float64, 0, n)
			for range n {
				cols = append(cols, ID(cgColor(color())))
				locs = append(locs, f())
			}
			s := Point{X: f(), Y: f()}
			e := Point{X: f(), Y: f()}
			p := path()
			if len(cols) > 0 && cgColorSpaceSRGB != 0 {
				g := cgGradientCreate(cgColorSpaceSRGB, uintptr(Array(cols...)), &locs[0])
				if g != 0 {
					cgSave(c)
					cgAddPath(c, p)
					cgClip(c)
					cgDrawLinear(c, g, s, e, 3) // draws before its start and after its end
					cgRestore(c)
					cgGradientRelease(g)
				}
			}
			cgPathRelease(p)
		case ops.CanvasOpText:
			col := color()
			size := f()
			if !ok(2) {
				return
			}
			weight, align := int32(b[i]), ops.TextAlign(b[i+1])
			i += 2
			x, y := f(), f()
			n := min(u32(), len(b)-i)
			s := string(b[i : i+n])
			i += n
			font := h.font(0, max(weight, int32(ops.FontWeightRegular)), float32(size), "")
			attrs := Dict(String("NSFont"), font, String("NSColor"), col)
			str := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(String(s)), uintptr(attrs))
			w := MsgSize(str, Sel("size")).Width
			switch align {
			case ops.TextAlignCenter:
				x -= w / 2
			case ops.TextAlignEnd:
				x -= w
			}
			MsgSetPoint(str, Sel("drawAtPoint:"), Point{X: x, Y: y - MsgFloat(font, Sel("ascender"))})
			Release(str)
		default:
			return
		}
	}
}

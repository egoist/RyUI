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
	cgDrawRadial                 func(c, g uintptr, start Point, startRadius float64, end Point, endRadius float64, options uint32)
	cgConcat                     func(c uintptr, t cgAffine)
	cgEOClip, cgEOFill           func(c uintptr)
	cgStrokedPath                func(c uintptr)
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
		"CGContextDrawRadialGradient": &cgDrawRadial, "CGContextConcatCTM": &cgConcat,
		"CGContextEOClip": &cgEOClip, "CGContextEOFillPath": &cgEOFill,
		"CGContextReplacePathWithStrokedPath": &cgStrokedPath,
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

// cgAffine is Core Graphics' CGAffineTransform.
type cgAffine struct{ A, B, C, D, Tx, Ty float64 }

func (h *Host) replayCanvas(c uintptr, b []byte) {
	i := 0
	evenOdd := false // FillRule's, kept with each Save
	var rules []bool
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
	clip := func(p uintptr) {
		cgAddPath(c, p)
		if evenOdd {
			cgEOClip(c)
		} else {
			cgClip(c)
		}
	}
	gradient := func() uintptr {
		n := u32()
		cols := make([]ID, 0, n)
		locs := make([]float64, 0, n)
		for range n {
			cols = append(cols, ID(cgColor(color())))
			locs = append(locs, f())
		}
		if len(cols) == 0 || cgColorSpaceSRGB == 0 {
			return 0
		}
		return cgGradientCreate(cgColorSpaceSRGB, uintptr(Array(cols...)), &locs[0])
	}
	strokeStyle := func() bool {
		w := f()
		if !ok(2) {
			return false
		}
		capv, join := int32(b[i]), int32(b[i+1])
		i += 2
		nd := u32()
		phase := f()
		dash := make([]float64, 0, nd)
		for range nd {
			dash = append(dash, f())
		}
		cgSetLineWidth(c, w)
		cgSetLineCap(c, capv)
		cgSetLineJoin(c, join)
		if len(dash) > 0 {
			cgSetLineDash(c, phase, &dash[0], uintptr(len(dash)))
		}
		return true
	}
	for i < len(b) {
		op := ops.CanvasOp(b[i])
		i++
		switch op {
		case ops.CanvasOpSave:
			cgSave(c)
			rules = append(rules, evenOdd)
		case ops.CanvasOpRestore:
			cgRestore(c)
			if len(rules) > 0 {
				evenOdd = rules[len(rules)-1]
				rules = rules[:len(rules)-1]
			}
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
			clip(p)
			cgPathRelease(p)
		case ops.CanvasOpFill:
			col := color()
			p := path()
			cgSetFill(c, cgColor(col))
			cgAddPath(c, p)
			if evenOdd {
				cgEOFill(c)
			} else {
				cgFillPath(c)
			}
			cgPathRelease(p)
		case ops.CanvasOpTransform:
			t := cgAffine{f(), f(), f(), f(), f(), f()}
			cgConcat(c, t)
		case ops.CanvasOpFillRule:
			if ok(1) {
				evenOdd = b[i] == 1
				i++
			}
		case ops.CanvasOpRadialGradient:
			g := gradient()
			center, r := Point{X: f(), Y: f()}, f()
			focus := Point{X: f(), Y: f()}
			p := path()
			if g != 0 {
				cgSave(c)
				clip(p)
				cgDrawRadial(c, g, focus, 0, center, r, 2) // and past its end
				cgRestore(c)
				cgGradientRelease(g)
			}
			cgPathRelease(p)
		case ops.CanvasOpStrokeGradient:
			cgSave(c)
			if !strokeStyle() || !ok(1) {
				cgRestore(c)
				return
			}
			radial := b[i] == 1
			i++
			g := gradient()
			var s, e Point
			var r float64
			if radial {
				e, r, s = Point{X: f(), Y: f()}, f(), Point{X: f(), Y: f()}
			} else {
				s, e = Point{X: f(), Y: f()}, Point{X: f(), Y: f()}
			}
			p := path()
			cgAddPath(c, p)
			cgStrokedPath(c)
			cgClip(c)
			if g != 0 {
				if radial {
					cgDrawRadial(c, g, s, 0, e, r, 2)
				} else {
					cgDrawLinear(c, g, s, e, 3)
				}
				cgGradientRelease(g)
			}
			cgRestore(c)
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
					clip(p)
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

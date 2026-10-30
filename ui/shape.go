package ui

import (
	"encoding/binary"
	"math"
	"slices"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// Path is an outline: lines, curves and shapes, in points from the top
// left of the Shape that draws it. Its methods add to it and return it,
// to chain: new(ui.Path).MoveTo(0, 0).LineTo(w, h).
type Path struct{ b []byte }

func (p *Path) op(o ops.PathOp, args ...float32) *Path {
	p.b = append(p.b, byte(o))
	for _, a := range args {
		p.b = binary.LittleEndian.AppendUint32(p.b, math.Float32bits(a))
	}
	return p
}

// MoveTo starts a figure at x, y.
func (p *Path) MoveTo(x, y float32) *Path { return p.op(ops.PathOpMove, x, y) }

// LineTo draws a line to x, y.
func (p *Path) LineTo(x, y float32) *Path { return p.op(ops.PathOpLine, x, y) }

// QuadTo draws a quadratic curve to x, y, bent toward cx, cy.
func (p *Path) QuadTo(cx, cy, x, y float32) *Path { return p.op(ops.PathOpQuad, cx, cy, x, y) }

// CubicTo draws a cubic curve to x, y, bent toward c1 then c2.
func (p *Path) CubicTo(c1x, c1y, c2x, c2y, x, y float32) *Path {
	return p.op(ops.PathOpCubic, c1x, c1y, c2x, c2y, x, y)
}

// Arc draws an arc of the circle at cx, cy of radius r, from the angle
// start to end (radians, clockwise from the positive x axis, the screen's
// way), going clockwise or not; from the figure's last point, a line
// joins it.
func (p *Path) Arc(cx, cy, r, start, end float32, clockwise bool) *Path {
	cw := float32(0)
	if clockwise {
		cw = 1
	}
	return p.op(ops.PathOpArc, cx, cy, r, start, end, cw)
}

// Close closes the figure with a line back to its start.
func (p *Path) Close() *Path { return p.op(ops.PathOpClose) }

// Rect adds a rectangle, a figure of its own.
func (p *Path) Rect(x, y, w, h float32) *Path { return p.op(ops.PathOpRect, x, y, w, h) }

// RoundedRect adds a rectangle with corners of radius r.
func (p *Path) RoundedRect(x, y, w, h, r float32) *Path {
	return p.op(ops.PathOpRoundedRect, x, y, w, h, r)
}

// Ellipse adds the ellipse in the rectangle (a circle in a square).
func (p *Path) Ellipse(x, y, w, h float32) *Path { return p.op(ops.PathOpEllipse, x, y, w, h) }

// LineCap is how a stroke's open ends look.
type LineCap = ops.LineCap

const (
	CapButt   = ops.LineCapButt
	CapRound  = ops.LineCapRound
	CapSquare = ops.LineCapSquare
)

// LineJoin is how a stroke's corners look.
type LineJoin = ops.LineJoin

const (
	JoinMiter = ops.LineJoinMiter
	JoinRound = ops.LineJoinRound
	JoinBevel = ops.LineJoinBevel
)

// ShapeProps configures a Shape.
type ShapeProps struct {
	// Path is the outline for the shape's size, in points (SwiftUI's
	// Shape.path(in:)): called when it's laid out, when its size changes,
	// and when the signals it reads change.
	Path func(width, height float32) *Path
	// Fill and Stroke color it inside and along the outline (zero: not).
	Fill, Stroke Color
	LineWidth    float32 // the stroke's; 0: 1
	LineCap      LineCap
	LineJoin     LineJoin
	// Trim is the part of the outline drawn, from TrimStart to TrimEnd (0
	// to 1; nil: all of it): a progress ring, a line that draws itself.
	// Changing it in Animate animates it.
	TrimStart, TrimEnd signal.Accessor[float32]
	Style              Styler
}

// Shape draws an outline: filled, stroked or both (SwiftUI's Shape and
// Path; Canvas.drawPath). It takes the size its Style gives it (a width
// and height, an aspect ratio, Grow); with none, it's empty.
func Shape(p ShapeProps) Node {
	e := newElement(ops.KindShape, true)
	e.setStyle(p.Style, false)
	b := e.r.batch
	if p.Fill != 0 {
		b.SetColor(e.id, ops.PropShapeFill, p.Fill)
	}
	if p.Stroke != 0 {
		b.SetColor(e.id, ops.PropShapeStroke, p.Stroke)
		w := p.LineWidth
		if w <= 0 {
			w = 1
		}
		b.SetFloat(e.id, ops.PropShapeLineWidth, float64(w))
	}
	if p.LineCap != CapButt {
		b.SetEnum(e.id, ops.PropShapeLineCap, int32(p.LineCap))
	}
	if p.LineJoin != JoinMiter {
		b.SetEnum(e.id, ops.PropShapeLineJoin, int32(p.LineJoin))
	}
	bind(p.TrimStart, func(v float32) { e.r.batch.SetFloat(e.id, ops.PropShapeTrimStart, float64(v)) })
	bind(p.TrimEnd, func(v float32) { e.r.batch.SetFloat(e.id, ops.PropShapeTrimEnd, float64(v)) })
	if p.Path != nil {
		// Its size is known once it's laid out (OnLayout's pass), and the
		// path is made again for each new one.
		size := signal.New([2]float32{})
		e.handlers().layout = func(f Frame) { size.Set([2]float32{f.Width, f.Height}) }
		var sent []byte
		signal.RenderEffect(func() {
			s := size.Get()
			if s[0] <= 0 || s[1] <= 0 {
				return
			}
			path := p.Path(s[0], s[1])
			if path == nil || slices.Equal(path.b, sent) {
				return
			}
			sent = slices.Clone(path.b)
			e.r.batch.SetBytes(e.id, ops.PropShapePath, sent)
		})
	}
	return Node{e}
}

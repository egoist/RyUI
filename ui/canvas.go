package ui

import (
	"encoding/binary"
	"math"
	"slices"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// Graphics is what a Canvas draws with: each call records a drawing
// command, in points from the canvas's top left (y down), and the host
// replays them natively (Core Graphics, Android's Canvas, GDI+). It's
// only valid while the draw function runs.
type Graphics struct {
	b     []byte
	paths []*Path // Path's, reused from draw to draw
	np    int
	w, h  float32
}

// Size is the canvas's size, in points.
func (g *Graphics) Size() (width, height float32) { return g.w, g.h }

// Path returns an empty path to build and draw, for this draw only: the
// canvas reuses its memory the next time, so drawing allocates nothing
// once it has drawn as much before.
func (g *Graphics) Path() *Path {
	if g.np == len(g.paths) {
		g.paths = append(g.paths, &Path{})
	}
	p := g.paths[g.np]
	g.np++
	p.b = p.b[:0]
	return p
}

func (g *Graphics) op(o ops.CanvasOp) { g.b = append(g.b, byte(o)) }

func (g *Graphics) f32(v ...float32) {
	for _, f := range v {
		g.b = binary.LittleEndian.AppendUint32(g.b, math.Float32bits(f))
	}
}

func (g *Graphics) color(c Color) { g.b = binary.LittleEndian.AppendUint64(g.b, uint64(c)) }

func (g *Graphics) path(p *Path) {
	var b []byte
	if p != nil {
		b = p.b
	}
	g.b = binary.LittleEndian.AppendUint32(g.b, uint32(len(b)))
	g.b = append(g.b, b...)
}

// Save remembers the transform, clip and opacity, for Restore.
func (g *Graphics) Save() { g.op(ops.CanvasOpSave) }

// Restore goes back to what the last Save remembered.
func (g *Graphics) Restore() { g.op(ops.CanvasOpRestore) }

// Translate moves what's drawn after by x, y points.
func (g *Graphics) Translate(x, y float32) { g.op(ops.CanvasOpTranslate); g.f32(x, y) }

// Scale scales what's drawn after, around the current origin.
func (g *Graphics) Scale(x, y float32) { g.op(ops.CanvasOpScale); g.f32(x, y) }

// Rotate turns what's drawn after by degrees, clockwise, around the
// current origin.
func (g *Graphics) Rotate(degrees float32) { g.op(ops.CanvasOpRotate); g.f32(degrees) }

// Opacity multiplies the opacity of what's drawn after (until Restore).
func (g *Graphics) Opacity(a float32) { g.op(ops.CanvasOpAlpha); g.f32(a) }

// Clip limits what's drawn after to inside p (until Restore).
func (g *Graphics) Clip(p *Path) { g.op(ops.CanvasOpClip); g.path(p) }

// Fill fills p with c, which may be a semantic color (Label, Separator):
// it follows dark mode.
func (g *Graphics) Fill(p *Path, c Color) {
	g.op(ops.CanvasOpFill)
	g.color(c)
	g.path(p)
}

// StrokeStyle is how a line is drawn.
type StrokeStyle struct {
	Width float32 // 0: 1
	Cap   LineCap
	Join  LineJoin
	// Dash dashes it: lengths drawn and skipped in turn; nil: solid.
	Dash      []float32
	DashPhase float32
}

// Stroke draws p's outline in c.
func (g *Graphics) Stroke(p *Path, c Color, s StrokeStyle) {
	w := s.Width
	if w <= 0 {
		w = 1
	}
	g.op(ops.CanvasOpStroke)
	g.color(c)
	g.f32(w)
	g.b = append(g.b, byte(s.Cap), byte(s.Join))
	g.b = binary.LittleEndian.AppendUint32(g.b, uint32(len(s.Dash)))
	g.f32(s.DashPhase)
	g.f32(s.Dash...)
	g.path(p)
}

// FillGradient fills p with a linear gradient, its Start and End in
// points of the canvas (both zero: p's top to its bottom is the
// caller's to say, so it's the canvas's top to bottom).
func (g *Graphics) FillGradient(p *Path, gr Gradient) {
	g.op(ops.CanvasOpGradient)
	g.b = binary.LittleEndian.AppendUint32(g.b, uint32(len(gr.Colors)))
	for i, c := range gr.Colors {
		loc := float32(0)
		switch {
		case i < len(gr.Locations):
			loc = gr.Locations[i]
		case len(gr.Colors) > 1:
			loc = float32(i) / float32(len(gr.Colors)-1)
		}
		g.color(c)
		g.f32(loc)
	}
	s, e := gr.Start, gr.End
	if s == (Point{}) && e == (Point{}) {
		e = Point{Y: g.h}
	}
	g.f32(s.X, s.Y, e.X, e.Y)
	g.path(p)
}

// TextStyle is how Graphics.Text draws.
type TextStyle struct {
	Size   float32 // points; 0: 17 (the body text's)
	Weight fonts.Weight
	Color  Color // 0: Label
	// Align is where x is on the text: its left edge (TextStart), its
	// center, or its right edge (TextEnd), physically, whatever the
	// language's direction.
	Align TextAlign
}

// Text draws s on one line, its baseline at y. Its size doesn't follow
// Dynamic Type: a canvas's drawing is laid out by its own numbers.
func (g *Graphics) Text(s string, x, y float32, t TextStyle) {
	size, c := t.Size, t.Color
	if size <= 0 {
		size = 17
	}
	if c == 0 {
		c = colors.Label
	}
	g.op(ops.CanvasOpText)
	g.color(c)
	g.f32(size)
	g.b = append(g.b, byte(t.Weight), byte(t.Align))
	g.f32(x, y)
	g.b = binary.LittleEndian.AppendUint32(g.b, uint32(len(s)))
	g.b = append(g.b, s...)
}

// Canvas draws what draw draws (SwiftUI's Canvas, Compose's Canvas): for
// what no view is, a chart, a gauge, a signature, a game's board. draw is
// called with the canvas's size once it's laid out, and again when its
// size changes and when the signals it reads change; only a drawing that
// differs from the last is sent. The canvas takes the size its style
// gives it (a width and height, Grow, an aspect ratio); with none, it's
// empty. Wrap it in a View for presses and gestures.
//
//	ui.Canvas(func(g *ui.Graphics) {
//		w, h := g.Size()
//		p := g.Path().Ellipse(0, 0, w, h)
//		g.Fill(p, colors.Hex(0x34c759))
//	}, ui.Style{Width: ui.Pt(80), Height: ui.Pt(80)})
func Canvas(draw func(g *Graphics), style ...Styler) Node {
	e := newElement(ops.KindCanvas, true)
	e.setStyle(firstStyle(style), false)
	size := signal.New([2]float32{})
	e.handlers().layout = func(f Frame) { size.Set([2]float32{f.Width, f.Height}) }
	g := &Graphics{}
	var sent []byte
	signal.RenderEffect(func() {
		s := size.Get()
		if s[0] <= 0 || s[1] <= 0 {
			return
		}
		g.b, g.np, g.w, g.h = g.b[:0], 0, s[0], s[1]
		draw(g)
		if sent != nil && slices.Equal(g.b, sent) {
			return
		}
		sent = append(sent[:0], g.b...)
		e.r.batch.SetBytes(e.id, ops.PropCanvasCommands, sent)
	})
	return Node{e}
}

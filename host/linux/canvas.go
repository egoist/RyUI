//go:build linux && !android

package linux

import (
	"encoding/binary"
	"math"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Canvas's display list, replayed by its view with GtkSnapshot. The
// commands are read once, as they're set, into GSK objects (paths,
// strokes, transforms, Pango layouts); a frame appends them, resolving
// the colors in the theme then (semantic colors follow dark mode), with
// raw calls and no allocation. Clip is a fill clip (gtk_snapshot_push_fill)
// popped at the Restore that ends it; Alpha multiplies the colors drawn
// after it, as Core Graphics' context alpha does.

type canvasView struct {
	cmds []canvasCmd
	// A frame's state, kept for the next.
	saves []canvasSave
}

type canvasSave struct {
	alpha  float32
	pushes int // fill clips pushed before the Save
}

type canvasCmd struct {
	op     ops.CanvasOp
	path   Ptr // Clip, Fill, Stroke, Gradient
	stroke Ptr // Stroke's GskStroke
	tr     Ptr // Translate, Scale, Rotate: a GskTransform
	color  ops.Color
	alpha  float32 // Alpha
	// Gradient: its stops' colors and locations (in order), the line, the
	// path's bounds; cs is the frame's resolved stops.
	stops  []gradientStop
	cs     []ColorStop
	from   Point
	to     Point
	bounds Rect
	// Text: its layout and its top left.
	layout Ptr
	at     Point
}

var canvases = map[*node]*canvasView{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindCanvas {
			return false
		}
		canvases[n] = &canvasView{}
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		c := canvases[n]
		if c == nil || p != ops.PropCanvasCommands {
			return false
		}
		if len(c.cmds) == 0 && n.widget != 0 {
			WidgetSetCanTarget(n.widget, false) // clicks go to what's under it
		}
		c.free()
		c.cmds = h.compileCanvas(n, v.Bytes, c.cmds[:0])
		queueDraw(n)
		return true
	})
	drawHooks = append(drawHooks, func(h *Host, n *node, snap Ptr, bounds Rect) bool {
		c := canvases[n]
		if c == nil {
			return false
		}
		h.replayCanvas(c, snap)
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if c := canvases[n]; c != nil {
			c.free()
			delete(canvases, n)
		}
	})
}

// free lets go of the GSK objects and layouts the commands hold.
func (c *canvasView) free() {
	for i := range c.cmds {
		x := &c.cmds[i]
		if x.path != 0 {
			Call(Draw.FnPathUnref, x.path)
		}
		if x.stroke != 0 {
			Call(Draw.FnStrokeFree, x.stroke)
		}
		if x.tr != 0 {
			TransformUnref(x.tr)
		}
		if x.layout != 0 {
			ObjectUnref(x.layout)
		}
		*x = canvasCmd{}
	}
	c.cmds = c.cmds[:0]
}

// compileCanvas reads the display list into commands.
func (h *Host) compileCanvas(n *node, b []byte, out []canvasCmd) []canvasCmd {
	i := 0
	ok := func(c int) bool { return i+c <= len(b) }
	f := func() float32 {
		if !ok(4) {
			i = len(b)
			return 0
		}
		v := math.Float32frombits(binary.LittleEndian.Uint32(b[i:]))
		i += 4
		if v != v {
			return 0
		}
		return v
	}
	u32 := func() int {
		if !ok(4) {
			i = len(b)
			return 0
		}
		v := int(binary.LittleEndian.Uint32(b[i:]))
		i += 4
		return v
	}
	color := func() ops.Color {
		if !ok(8) {
			i = len(b)
			return 0
		}
		c := ops.Color(binary.LittleEndian.Uint64(b[i:]))
		i += 8
		return c
	}
	path := func() Ptr {
		c := min(u32(), len(b)-i)
		p := buildPath(b[i : i+c])
		i += c
		return p
	}
	for i < len(b) {
		op := ops.CanvasOp(b[i])
		i++
		cmd := canvasCmd{op: op}
		switch op {
		case ops.CanvasOpSave, ops.CanvasOpRestore:
		case ops.CanvasOpTranslate:
			p := Point{X: f(), Y: f()}
			cmd.tr = Call(FnTransformTranslate, 0, Addr(&p))
		case ops.CanvasOpScale:
			x, y := f(), f()
			cmd.tr = Draw.TransformScale(0, x, y)
		case ops.CanvasOpRotate:
			cmd.tr = Draw.TransformRotate(0, f()) // y down: clockwise on screen
		case ops.CanvasOpAlpha:
			cmd.alpha = min(max(f(), 0), 1)
		case ops.CanvasOpClip:
			cmd.path = path()
			if cmd.path == 0 {
				cmd.path = emptyPath() // clips everything away
			}
		case ops.CanvasOpFill:
			cmd.color = color()
			cmd.path = path()
		case ops.CanvasOpStroke:
			cmd.color = color()
			w := f()
			if !ok(2) {
				return out
			}
			capv, join := int32(min(b[i], 2)), int32(min(b[i+1], 2))
			i += 2
			nd := u32()
			phase := f()
			dash := make([]float32, 0, min(nd, 64))
			for range nd {
				if !ok(4) {
					break
				}
				dash = append(dash, f())
			}
			cmd.path = path()
			cmd.stroke = newStroke(w, capv, join, dash, phase)
		case ops.CanvasOpGradient:
			c := u32()
			for range c {
				if !ok(12) {
					break
				}
				cmd.stops = append(cmd.stops, gradientStop{color: color(), location: f()})
			}
			cmd.from = Point{X: f(), Y: f()}
			cmd.to = Point{X: f(), Y: f()}
			if cmd.from == cmd.to {
				cmd.to.Y += 1
			}
			cmd.path = path()
			if cmd.path != 0 && !Draw.PathGetBounds(cmd.path, &cmd.bounds) {
				Call(Draw.FnPathUnref, cmd.path)
				cmd.path = 0
			}
			cmd.cs = make([]ColorStop, max(len(cmd.stops), 2))
		case ops.CanvasOpText:
			cmd.color = color()
			size := f()
			if !ok(2) {
				return out
			}
			weight, align := ops.FontWeight(b[i]), ops.TextAlign(b[i+1])
			i += 2
			x, y := f(), f()
			l := min(u32(), len(b)-i)
			s := string(b[i : i+l])
			i += l
			cmd.layout, cmd.at = h.canvasText(n, s, size, weight, align, x, y)
		default:
			return out
		}
		out = append(out, cmd)
	}
	return out
}

// emptyPath is a path with no area, for a clip of nothing.
func emptyPath() Ptr { return PathParse("M 0 0 Z") }

// canvasText lays a text out for a canvas: its layout, and its top left
// for its baseline at x, y and its alignment there.
func (h *Host) canvasText(n *node, s string, size float32, weight ops.FontWeight, align ops.TextAlign, x, y float32) (Ptr, Point) {
	if s == "" || n.widget == 0 {
		return 0, Point{}
	}
	l := WidgetCreatePangoLayout(n.widget, s)
	attrs := AttrListNew()
	if size <= 0 {
		size = 17
	}
	AttrListInsert(attrs, AttrSizeNewAbsolute(int32(size*1024+0.5)))
	wt := int32(400)
	if weight > 0 && int(weight) < len(weights) {
		wt = weights[weight]
	}
	AttrListInsert(attrs, AttrWeightNew(wt))
	LayoutSetAttributes(l, attrs)
	AttrListUnref(attrs)
	var w, ht int32
	LayoutGetPixelSize(l, &w, &ht)
	at := Point{X: x, Y: y - float32(LayoutGetBaseline(l))/1024}
	switch align {
	case ops.TextAlignCenter:
		at.X -= float32(w) / 2
	case ops.TextAlignEnd:
		at.X -= float32(w)
	}
	return l, at
}

// replayCanvas draws the commands.
func (h *Host) replayCanvas(c *canvasView, snap Ptr) {
	if len(c.cmds) == 0 {
		return
	}
	Call(FnSnapshotSave, snap)
	alpha := float32(1)
	pushes := 0
	c.saves = c.saves[:0]
	resolve := func(col ops.Color) (RGBA, bool) {
		v, ok := h.resolve(col)
		v.A *= alpha
		return v, ok && col != 0 && v.A > 0
	}
	for i := range c.cmds {
		x := &c.cmds[i]
		switch x.op {
		case ops.CanvasOpSave:
			c.saves = append(c.saves, canvasSave{alpha: alpha, pushes: pushes})
			pushes = 0
			Call(FnSnapshotSave, snap)
		case ops.CanvasOpRestore:
			if len(c.saves) == 0 {
				continue
			}
			for ; pushes > 0; pushes-- {
				Call(FnSnapshotPop, snap)
			}
			Call(FnSnapshotRestore, snap)
			s := c.saves[len(c.saves)-1]
			c.saves = c.saves[:len(c.saves)-1]
			alpha, pushes = s.alpha, s.pushes
		case ops.CanvasOpTranslate, ops.CanvasOpScale, ops.CanvasOpRotate:
			if x.tr != 0 {
				Call(Draw.FnSnapshotTransform, snap, x.tr)
			}
		case ops.CanvasOpAlpha:
			alpha *= x.alpha
		case ops.CanvasOpClip:
			Call(FnSnapshotPushFill, snap, x.path, FillWinding)
			pushes++
		case ops.CanvasOpFill:
			if col, ok := resolve(x.color); ok && x.path != 0 {
				Call(FnSnapshotAppendFill, snap, x.path, FillWinding, Addr(&col))
			}
		case ops.CanvasOpStroke:
			if col, ok := resolve(x.color); ok && x.path != 0 {
				Call(FnSnapshotAppendStroke, snap, x.path, x.stroke, Addr(&col))
			}
		case ops.CanvasOpGradient:
			h.canvasGradient(x, snap, alpha)
		case ops.CanvasOpText:
			if col, ok := resolve(x.color); ok && x.layout != 0 {
				Call(FnSnapshotSave, snap)
				Call(FnSnapshotTranslate, snap, Addr(&x.at))
				Call(FnSnapshotAppendLayout, snap, x.layout, Addr(&col))
				Call(FnSnapshotRestore, snap)
			}
		}
	}
	for {
		for ; pushes > 0; pushes-- {
			Call(FnSnapshotPop, snap)
		}
		if len(c.saves) == 0 {
			break
		}
		Call(FnSnapshotRestore, snap)
		pushes = c.saves[len(c.saves)-1].pushes
		c.saves = c.saves[:len(c.saves)-1]
	}
	Call(FnSnapshotRestore, snap)
}

// canvasGradient fills a path with a linear gradient, padded before its
// start and after its end.
func (h *Host) canvasGradient(x *canvasCmd, snap Ptr, alpha float32) {
	if x.path == 0 || len(x.stops) == 0 {
		return
	}
	last := float32(0)
	for i, s := range x.stops {
		c, _ := h.resolve(s.color)
		c.A *= alpha
		off := min(max(s.location, last), 1) // GSK wants them in order
		x.cs[i] = ColorStop{Offset: off, Color: c}
		last = off
	}
	n := len(x.stops)
	if n == 1 {
		x.cs[1] = ColorStop{Offset: 1, Color: x.cs[0].Color}
		x.cs[0].Offset = 0
		n = 2
	}
	Call(FnSnapshotPushFill, snap, x.path, FillWinding)
	Call(FnSnapshotAppendLinearGradient, snap, Addr(&x.bounds), Addr(&x.from), Addr(&x.to), Addr(&x.cs[0]), uintptr(n))
	Call(FnSnapshotPop, snap)
}

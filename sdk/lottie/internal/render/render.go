package render

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// Fit is how an animation fills its box: ui.ImageMode's.
type Fit = ops.ImageMode

// OverrideKind is what an Override changes.
type OverrideKind uint8

const (
	OverrideColor       OverrideKind = iota // the fills' and strokes' color under the keypath
	OverrideOpacity                         // the opacity of the layer, group or paint the keypath names (0 to 1)
	OverrideStrokeWidth                     // the strokes' width under the keypath
)

// Override changes what a keypath names: names of layers, groups and
// shapes from the animation's root, separated by dots, with * for any one
// name and ** for any number of them (lottie-ios' and lottie-android's
// keypaths).
type Override struct {
	Keypath []string
	Kind    OverrideKind
	Color   [4]float64 // RGBA, 0 to 1
	Value   float64
}

// ParseKeypath splits a keypath at its dots.
func ParseKeypath(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ".")
}

// match reports whether pattern names path exactly (prefix: or one of
// its ancestors).
func match(pattern, path []string, prefix bool) bool {
	if len(pattern) == 0 {
		return len(path) == 0 || prefix
	}
	switch pattern[0] {
	case "**":
		for i := 0; i <= len(path); i++ {
			if match(pattern[1:], path[i:], prefix) {
				return true
			}
		}
		return false
	case "*":
		return len(path) > 0 && match(pattern[1:], path[1:], prefix)
	}
	return len(path) > 0 && path[0] == pattern[0] && match(pattern[1:], path[1:], prefix)
}

// Renderer draws an animation, frame by frame.
type Renderer struct {
	A         *Animation
	Overrides []Override

	g       *ui.Graphics
	evenOdd bool
	rules   []bool
	names   []string // the keypath down to what's drawn
	buf     [8]float64
}

func (r *Renderer) save() {
	r.g.Save()
	r.rules = append(r.rules, r.evenOdd)
}

func (r *Renderer) restore() {
	r.g.Restore()
	if n := len(r.rules); n > 0 {
		r.evenOdd = r.rules[n-1]
		r.rules = r.rules[:n-1]
	}
}

func (r *Renderer) setEvenOdd(on bool) {
	if r.evenOdd != on {
		r.evenOdd = on
		r.g.EvenOdd(on)
	}
}

// Box is where the animation goes in a box of w×h at fit: its scale on
// each axis and its offset.
func (a *Animation) Box(w, h float64, fit Fit) Matrix {
	aw, ah := a.Width, a.Height
	if aw <= 0 || ah <= 0 || w <= 0 || h <= 0 {
		return Identity
	}
	sx, sy := w/aw, h/ah
	switch fit {
	case ops.ImageModeFill:
		sx = max(sx, sy)
		sy = sx
	case ops.ImageModeStretch:
	case ops.ImageModeCenter:
		sx, sy = 1, 1
	default:
		sx = min(sx, sy)
		sy = sx
	}
	return translate((w-aw*sx)/2, (h-ah*sy)/2).Mul(scale(sx, sy))
}

// Draw draws the animation at frame t (its own frames: In to Out) in a
// box of w×h points at fit, clipped to the box.
func (r *Renderer) Draw(g *ui.Graphics, t float64, w, h float64, fit Fit) {
	r.g, r.evenOdd, r.rules, r.names = g, false, r.rules[:0], r.names[:0]
	r.save()
	g.Clip(g.Path().Rect(0, 0, float32(w), float32(h)))
	r.transform(r.A.Box(w, h, fit))
	r.comp(r.A.layers, t, 1)
	r.restore()
	r.g = nil
}

func (r *Renderer) transform(m Matrix) {
	if m != Identity {
		r.g.Transform(float32(m.A), float32(m.B), float32(m.C), float32(m.D), float32(m.E), float32(m.F))
	}
}

// comp draws a composition's layers at its frame t, the last at the back.
func (r *Renderer) comp(layers []*Layer, t, alpha float64) {
	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		if l.Hidden || l.IsMatte == 1 || t < l.In || t >= l.Out {
			continue
		}
		r.layer(l, t, alpha)
	}
}

func (l *Layer) time(t float64) float64 { return (t - l.Start) / l.Stretch }

// world is a layer's transform in its composition at t: its parents'
// and its own.
func (r *Renderer) world(l *Layer, t float64) Matrix {
	m := Identity
	for p := l; p != nil; p = p.parent {
		lt := p.time(t)
		turn := 0.0
		if p.AutoOrient == 1 {
			turn = p.orient(lt)
		}
		m = p.Transform.matrix(lt, turn).Mul(m)
	}
	return m
}

// orient is the angle, in degrees, a layer that turns along its path
// (auto-orient) turns to at t.
func (l *Layer) orient(t float64) float64 {
	a := l.Transform.Position.Vec(t, Vec{})
	b := l.Transform.Position.Vec(t+0.05, Vec{})
	if a == b {
		a = l.Transform.Position.Vec(t-0.05, Vec{})
		b = l.Transform.Position.Vec(t, Vec{})
	}
	if a == b {
		return 0
	}
	return math.Atan2(b.Y-a.Y, b.X-a.X) * 180 / math.Pi
}

func (r *Renderer) layer(l *Layer, t, alpha float64) {
	lt := l.time(t)
	r.names = append(r.names, l.Name)
	defer func() { r.names = r.names[:len(r.names)-1] }()
	o := alpha * l.Transform.opacity(lt)
	if v, ok := r.opacityOverride(); ok {
		o = alpha * v
	}
	if o <= 0 {
		return
	}
	m := r.world(l, t)
	r.save()
	defer r.restore()
	if l.matte != nil && !r.clipMatte(l.matte, l.Matte, t) {
		return
	}
	r.transform(m)
	if len(l.Masks) > 0 && !r.clipMasks(l, lt) {
		return
	}
	switch l.Type {
	case layerShape:
		r.shapes(l, lt, o)
	case layerSolid:
		c := parseHex(l.Solid)
		c[3] *= o
		r.setEvenOdd(false)
		r.g.Fill(r.g.Path().Rect(0, 0, float32(l.SolidW), float32(l.SolidH)), uiColor(c))
	case layerPrecomp:
		if l.comp == nil {
			return
		}
		w, h := l.W, l.H
		if w <= 0 {
			w, h = l.comp.W, l.comp.H
		}
		if w > 0 && h > 0 {
			r.setEvenOdd(false)
			r.g.Clip(r.g.Path().Rect(0, 0, float32(w), float32(h)))
		}
		ct := lt
		if l.TimeRemap != nil && l.TimeRemap.set {
			ct = l.TimeRemap.Scalar(lt, 0) * r.A.FrameRate
		}
		r.comp(l.comp.Layers, ct, o)
	}
}

// big is a rectangle past any drawing: what's outside a path, filled
// even-odd with it.
const big = 1e5

// clipMatte clips to the matte layer's outlines (in its own transform),
// or outside them for an inverted matte; false if nothing shows.
func (r *Renderer) clipMatte(matte *Layer, mode int, t float64) bool {
	inverted := mode == 2 || mode == 4
	visible := !matte.Hidden && t >= matte.In && t < matte.Out
	var p Path
	if visible {
		p = r.outline(matte, matte.time(t))
	}
	if len(p.Contours) == 0 {
		return inverted
	}
	m := r.world(matte, t)
	r.transform(m)
	gp := r.g.Path()
	if inverted {
		gp.Rect(-big, -big, 2*big, 2*big)
	}
	appendPath(gp, clockwise(p))
	r.setEvenOdd(inverted)
	r.g.Clip(gp)
	r.transform(m.Inverse())
	return true
}

// outline is what a matte layer covers, in its own space: its fills'
// outlines; a solid's rectangle; a precomposition's bounds.
func (r *Renderer) outline(l *Layer, t float64) Path {
	switch l.Type {
	case layerShape:
		sc := r.scene(l, t)
		var out Path
		for i := range sc.els {
			e := &sc.els[i]
			if e.kind != elPaint || (e.it.Type != "fl" && e.it.Type != "gf") {
				continue
			}
			for _, gi := range sc.geomFor(i) {
				out.Contours = append(out.Contours, sc.els[gi].path.Contours...)
			}
		}
		return out
	case layerSolid:
		return rectPath(0, 0, l.SolidW, l.SolidH)
	case layerPrecomp:
		w, h := l.W, l.H
		if w <= 0 && l.comp != nil {
			w, h = l.comp.W, l.comp.H
		}
		return rectPath(0, 0, w, h)
	}
	return Path{}
}

func rectPath(x, y, w, h float64) Path {
	if w <= 0 || h <= 0 {
		return Path{}
	}
	line := func(a, b Vec) Cubic { return Cubic{a, a, b, b} }
	p0, p1, p2, p3 := Vec{x, y}, Vec{x + w, y}, Vec{x + w, y + h}, Vec{x, y + h}
	return Path{Contours: []Contour{{Closed: true, Segs: []Cubic{line(p0, p1), line(p1, p2), line(p2, p3), line(p3, p0)}}}}
}

// clipMasks clips to a layer's masks: the added ones' union, cut by the
// subtracted and intersected ones; false if nothing shows.
func (r *Renderer) clipMasks(l *Layer, t float64) bool {
	var add Path
	adds := 0
	for _, m := range l.Masks {
		if m.Mode == "n" || m.Mode == "" {
			continue
		}
		d := m.Path.At(t)
		d.Closed = true
		c := d.contour()
		if len(c.Segs) == 0 {
			continue
		}
		p := Path{Contours: []Contour{c}}
		switch m.Mode {
		case "s": // subtract: outside it (inverted: inside it)
			r.clipPath(p, !m.Inverted)
		case "i": // intersect
			r.clipPath(p, m.Inverted)
		default: // add, and lighten, darken, difference
			if m.Inverted {
				r.clipPath(p, true)
				continue
			}
			add.Contours = append(add.Contours, clockwise(p).Contours...)
			adds++
		}
	}
	if adds > 0 {
		r.clipPath(add, false)
	}
	return true
}

// clipPath clips to p, or outside it.
func (r *Renderer) clipPath(p Path, outside bool) {
	gp := r.g.Path()
	if outside {
		gp.Rect(-big, -big, 2*big, 2*big)
	}
	appendPath(gp, p)
	r.setEvenOdd(outside)
	r.g.Clip(gp)
}

// appendPath adds p's contours to gp.
func appendPath(gp *ui.Path, p Path) {
	for _, c := range p.Contours {
		if len(c.Segs) == 0 {
			continue
		}
		s0 := c.Segs[0].P0
		gp.MoveTo(float32(s0.X), float32(s0.Y))
		for _, s := range c.Segs {
			if s.straight() {
				gp.LineTo(float32(s.P3.X), float32(s.P3.Y))
			} else {
				gp.CubicTo(float32(s.P1.X), float32(s.P1.Y), float32(s.P2.X), float32(s.P2.Y), float32(s.P3.X), float32(s.P3.Y))
			}
		}
		if c.Closed {
			gp.Close()
		}
	}
}

// The shape engine: a layer's items flattened in order (the first at the
// front), each paint painting and each modifier changing the shapes
// before it in its group and the groups inside that, as After Effects
// and lottie-web do.

type elKind uint8

const (
	elGeom elKind = iota
	elPaint
	elModifier
)

type group struct {
	parent  *group
	m       Matrix  // to the layer's space
	opacity float64 // its own and its ancestors'
	names   int     // len(Renderer.names) inside it
}

type el struct {
	kind  elKind
	it    *item
	g     *group
	path  Path // a shape's, in the layer's space
	names []string
}

type scene struct {
	els    []el
	groups []*group
}

func within(g, ancestor *group) bool {
	for ; g != nil; g = g.parent {
		if g == ancestor {
			return true
		}
	}
	return false
}

// geomFor are the shapes element i (a paint or a modifier) applies to.
func (sc *scene) geomFor(i int) []int {
	var out []int
	g := sc.els[i].g
	for j := 0; j < i; j++ {
		if sc.els[j].kind == elGeom && within(sc.els[j].g, g) {
			out = append(out, j)
		}
	}
	return out
}

// scene evaluates a shape layer's items at t: its shapes in the layer's
// space, modified.
func (r *Renderer) scene(l *Layer, t float64) *scene {
	sc := &scene{}
	root := &group{m: Identity, opacity: 1, names: len(r.names)}
	r.flatten(sc, l.Shapes, root, t)
	// Modifiers, from the first: each shape's nearest first.
	for i := range sc.els {
		e := &sc.els[i]
		if e.kind != elModifier {
			continue
		}
		geoms := sc.geomFor(i)
		switch e.it.Type {
		case "tm":
			s := e.it.S.Scalar(t, 0) / 100
			end := e.it.E.Scalar(t, 100) / 100
			off := e.it.O.Scalar(t, 0) / 360
			paths := make([]Path, len(geoms))
			for k, gi := range geoms {
				paths[k] = sc.els[gi].path
			}
			paths = trim(paths, s, end, off, e.it.TrimMode == 2)
			for k, gi := range geoms {
				sc.els[gi].path = paths[k]
			}
		case "rd":
			rad := e.it.R.Scalar(t, 0)
			for _, gi := range geoms {
				p := &sc.els[gi].path
				out := Path{Contours: make([]Contour, len(p.Contours))}
				for k, c := range p.Contours {
					out.Contours[k] = roundCorners(c, rad)
				}
				*p = out
			}
		}
	}
	return sc
}

// flatten adds a group's items to the scene, in order.
func (r *Renderer) flatten(sc *scene, items []*item, g *group, t float64) {
	for i, it := range items {
		if it.Hidden {
			continue
		}
		switch it.Type {
		case "gr":
			r.names = append(r.names, it.Name)
			child := &group{parent: g, m: g.m, opacity: g.opacity, names: len(r.names)}
			for _, c := range it.Items {
				if c.Type == "tr" && !c.Hidden {
					child.m = g.m.Mul(c.transform.matrix(t, 0))
					child.opacity = g.opacity * c.transform.opacity(t)
				}
			}
			if v, ok := r.opacityOverride(); ok {
				child.opacity = g.opacity * v
			}
			sc.groups = append(sc.groups, child)
			r.flatten(sc, it.Items, child, t)
			r.names = r.names[:len(r.names)-1]
		case "sh", "rc", "el", "sr":
			p := Path{Contours: []Contour{r.shape(it, t)}}
			sc.els = append(sc.els, el{kind: elGeom, it: it, g: g, path: p.transformed(g.m)})
		case "fl", "st", "gf", "gs":
			names := append(append([]string(nil), r.names...), it.Name)
			sc.els = append(sc.els, el{kind: elPaint, it: it, g: g, names: names})
		case "tm", "rd":
			sc.els = append(sc.els, el{kind: elModifier, it: it, g: g})
		case "rp":
			// The items before it, again and again: each copy a group of
			// them, transformed once more than the last.
			r.repeat(sc, items[:i], it, g, t)
			return
		}
	}
}

// repeat adds a repeater's copies of items.
func (r *Renderer) repeat(sc *scene, items []*item, rp *item, g *group, t float64) {
	// What came before the repeater was added already: take it back out,
	// to add it once per copy.
	n := 0
	for k := len(sc.els) - 1; k >= 0; k-- {
		if !within(sc.els[k].g, g) {
			break
		}
		n++
	}
	sc.els = sc.els[:len(sc.els)-n]
	copies := int(math.Max(0, math.Round(rp.Copies.Scalar(t, 1))))
	offset := rp.O.Scalar(t, 0)
	tr := rp.RepeatTr
	if tr == nil {
		tr = &transform{}
	}
	start, end := tr.StartOpacity.Scalar(t, 100)/100, tr.EndOpacity.Scalar(t, 100)/100
	step := func(k float64) Matrix {
		// The repeater's transform k times over (k may be fractional).
		a := tr.Anchor.Vec(t, Vec{})
		p := tr.Position.Vec(t, Vec{})
		s := tr.Scale.Vec(t, Vec{100, 100})
		rot := tr.Rotation.Scalar(t, 0)
		whole := math.Floor(k)
		one := translate(p.X, p.Y).Mul(translate(a.X, a.Y)).Mul(rotate(rot)).Mul(scale(s.X/100, s.Y/100)).Mul(translate(-a.X, -a.Y))
		m := Identity
		for range int(math.Abs(whole)) {
			if whole > 0 {
				m = one.Mul(m)
			} else {
				m = one.Inverse().Mul(m)
			}
		}
		if f := k - whole; f > 0 {
			part := translate(p.X*f, p.Y*f).Mul(translate(a.X, a.Y)).Mul(rotate(rot * f)).
				Mul(scale(math.Pow(s.X/100, f), math.Pow(s.Y/100, f))).Mul(translate(-a.X, -a.Y))
			m = part.Mul(m)
		}
		return m
	}
	order := make([]int, copies)
	for k := range order {
		order[k] = k
	}
	if rp.TrimMode != 2 { // composite above: the last copy in front
		for a, b := 0, len(order)-1; a < b; a, b = a+1, b-1 {
			order[a], order[b] = order[b], order[a]
		}
	}
	for _, k := range order {
		op := start
		if copies > 1 {
			op = start + (end-start)*float64(k)/float64(copies-1)
		}
		c := &group{parent: g, m: g.m.Mul(step(float64(k) + offset)), opacity: g.opacity * op, names: g.names}
		sc.groups = append(sc.groups, c)
		r.flatten(sc, items, c, t)
	}
}

// shape is a shape item's outline at t, in its group's space.
func (r *Renderer) shape(it *item, t float64) Contour {
	var d shapeData
	switch it.Type {
	case "sh":
		d = it.Path.At(t)
	case "rc":
		d = rectShape(it.P.Vec(t, Vec{}), it.S.Vec(t, Vec{}), it.R.Scalar(t, 0))
	case "el":
		d = ellipseShape(it.P.Vec(t, Vec{}), it.S.Vec(t, Vec{}))
	case "sr":
		d = starShape(it, t)
	}
	c := d.contour()
	if it.Direction == 3 && it.Type != "sh" {
		c = reversed(c)
	}
	return c
}

// rectShape is a rectangle as lottie-web makes it: from its top right,
// clockwise, its corners rounded by r.
func rectShape(p, s Vec, r float64) shapeData {
	w, h := s.X/2, s.Y/2
	r = min(r, w, h)
	d := shapeData{Closed: true}
	add := func(v, in, out Vec) {
		d.V = append(d.V, v)
		d.In = append(d.In, in.Sub(v))
		d.Out = append(d.Out, out.Sub(v))
	}
	if r <= 0 {
		for _, v := range []Vec{{p.X + w, p.Y - h}, {p.X + w, p.Y + h}, {p.X - w, p.Y + h}, {p.X - w, p.Y - h}} {
			add(v, v, v)
		}
		return d
	}
	c := r * (1 - 0.5519)
	l, rt, tp, bt := p.X-w, p.X+w, p.Y-h, p.Y+h
	add(Vec{rt, tp + r}, Vec{rt, tp + r}, Vec{rt, tp + r})
	add(Vec{rt, bt - r}, Vec{rt, bt - r}, Vec{rt, bt - c})
	add(Vec{rt - r, bt}, Vec{rt - c, bt}, Vec{rt - r, bt})
	add(Vec{l + r, bt}, Vec{l + r, bt}, Vec{l + c, bt})
	add(Vec{l, bt - r}, Vec{l, bt - c}, Vec{l, bt - r})
	add(Vec{l, tp + r}, Vec{l, tp + r}, Vec{l, tp + c})
	add(Vec{l + r, tp}, Vec{l + c, tp}, Vec{l + r, tp})
	add(Vec{rt - r, tp}, Vec{rt - r, tp}, Vec{rt - c, tp})
	d.Out[len(d.Out)-1] = Vec{rt - c, tp}.Sub(Vec{rt - r, tp})
	d.In[0] = Vec{rt, tp + c}.Sub(Vec{rt, tp + r})
	return d
}

// ellipseShape is an ellipse as lottie-web makes it: from its top,
// clockwise.
func ellipseShape(p, s Vec) shapeData {
	const k = 0.5519
	w, h := s.X/2, s.Y/2
	cw, ch := w*k, h*k
	return shapeData{
		Closed: true,
		V:      []Vec{{p.X, p.Y - h}, {p.X + w, p.Y}, {p.X, p.Y + h}, {p.X - w, p.Y}},
		In:     []Vec{{-cw, 0}, {0, -ch}, {cw, 0}, {0, ch}},
		Out:    []Vec{{cw, 0}, {0, ch}, {-cw, 0}, {0, -ch}},
	}
}

// starShape is a star or a polygon, as lottie-web makes one.
func starShape(it *item, t float64) shapeData {
	pts := math.Floor(it.Points.Scalar(t, 5))
	if pts < 2 {
		pts = 2
	}
	p := it.P.Vec(t, Vec{})
	rot := it.R.Scalar(t, 0) * math.Pi / 180
	or, os := it.OuterR.Scalar(t, 0), it.OuterRnd.Scalar(t, 0)/100
	ir, is := it.InnerR.Scalar(t, 0), it.InnerRnd.Scalar(t, 0)/100
	star := it.StarType != 2
	n := int(pts)
	if star {
		n *= 2
	}
	angle := 2 * math.Pi / float64(n)
	cur := -math.Pi/2 + rot
	d := shapeData{Closed: true}
	long := true
	for range n {
		rad, rnd := or, os
		if star && !long {
			rad, rnd = ir, is
		}
		perim := 2 * math.Pi * rad / float64(n*2)
		if !star {
			perim = 2 * math.Pi * rad / float64(n*4)
		}
		x, y := rad*math.Cos(cur), rad*math.Sin(cur)
		var ox, oy float64
		if l := math.Hypot(x, y); l > 0 {
			ox, oy = y/l, -x/l
		}
		v := Vec{x + p.X, y + p.Y}
		d.V = append(d.V, v)
		d.In = append(d.In, Vec{ox * perim * rnd, oy * perim * rnd})
		d.Out = append(d.Out, Vec{-ox * perim * rnd, -oy * perim * rnd})
		long = !long
		cur += angle
	}
	return d
}

// shapes draws a shape layer at t: its paints from the back.
func (r *Renderer) shapes(l *Layer, t, alpha float64) {
	sc := r.scene(l, t)
	for i := len(sc.els) - 1; i >= 0; i-- {
		e := &sc.els[i]
		if e.kind != elPaint {
			continue
		}
		geoms := sc.geomFor(i)
		if len(geoms) == 0 {
			continue
		}
		r.paint(sc, e, geoms, t, alpha)
	}
}

// paint draws the shapes a paint paints, in its group's space (a
// stroke's width is its group's).
func (r *Renderer) paint(sc *scene, e *el, geoms []int, t, alpha float64) {
	it := e.it
	o := alpha * e.g.opacity * it.O.Scalar(t, 100) / 100
	for _, ov := range r.Overrides {
		if ov.Kind == OverrideOpacity && match(ov.Keypath, e.names, false) {
			o = alpha * e.g.opacity * ov.Value
		}
	}
	if o <= 0 {
		return
	}
	n := 0
	for _, gi := range geoms {
		n += len(sc.els[gi].path.Contours)
	}
	if n == 0 {
		return // trimmed away
	}
	inv := e.g.m.Inverse()
	gp := r.g.Path()
	for _, gi := range geoms {
		appendPath(gp, sc.els[gi].path.transformed(inv))
	}
	r.save()
	defer r.restore()
	r.transform(e.g.m)
	var c [4]float64
	if it.Type == "fl" || it.Type == "st" {
		c = r.color(it, e.names, t)
		c[3] *= o
	}
	switch it.Type {
	case "fl":
		r.setEvenOdd(it.FillRule == 2)
		r.g.Fill(gp, uiColor(c))
	case "gf":
		r.setEvenOdd(it.FillRule == 2)
		colors, locs := r.stops(it, t, o)
		s, en := it.S.Vec(t, Vec{}), it.E.Vec(t, Vec{})
		if it.GradType == 2 {
			r.g.FillRadialGradient(gp, radial(it, t, s, en, colors, locs))
		} else {
			if s == en {
				en.X += 0.01
			}
			r.g.FillGradient(gp, ui.Gradient{Colors: colors, Locations: locs, Start: pt(s), End: pt(en)})
		}
	case "st", "gs":
		st := r.strokeStyle(it, e.names, t)
		if st.Width <= 0 {
			return
		}
		if it.Type == "st" {
			r.g.Stroke(gp, uiColor(c), st)
			return
		}
		colors, locs := r.stops(it, t, o)
		s, en := it.S.Vec(t, Vec{}), it.E.Vec(t, Vec{})
		if it.GradType == 2 {
			r.g.StrokeRadialGradient(gp, st, radial(it, t, s, en, colors, locs))
		} else {
			if s == en {
				en.X += 0.01
			}
			r.g.StrokeGradient(gp, st, ui.Gradient{Colors: colors, Locations: locs, Start: pt(s), End: pt(en)})
		}
	}
}

func pt(v Vec) ui.Point { return ui.Point{X: float32(v.X), Y: float32(v.Y)} }

// radial is a radial gradient from s to the circle through en, its focus
// the highlight's (a fraction of the radius, at an angle).
func radial(it *item, t float64, s, en Vec, colors []ui.Color, locs []float32) ui.RadialGradient {
	rad := en.Sub(s).Len()
	gr := ui.RadialGradient{Colors: colors, Locations: locs, Center: pt(s), Radius: float32(rad)}
	if hl := it.Highlight.Scalar(t, 0) / 100; hl != 0 {
		hl = max(-0.99, min(0.99, hl))
		ang := math.Atan2(en.Y-s.Y, en.X-s.X) + it.HAngle.Scalar(t, 0)*math.Pi/180
		f := pt(Vec{s.X + math.Cos(ang)*rad*hl, s.Y + math.Sin(ang)*rad*hl})
		gr.Focus = &f
	}
	return gr
}

// color is a fill's or stroke's color at t (an override's, if one names
// it).
func (r *Renderer) color(it *item, names []string, t float64) [4]float64 {
	c := [4]float64{0, 0, 0, 1}
	v := it.Color.At(t, r.buf[:0])
	big := false
	for i := 0; i < len(v) && i < 4; i++ {
		c[i] = v[i]
		if v[i] > 1 {
			big = true
		}
	}
	if big { // an old file's 0 to 255
		for i := range 3 {
			c[i] /= 255
		}
		if c[3] > 1 {
			c[3] /= 255
		}
	}
	for _, ov := range r.Overrides {
		if ov.Kind == OverrideColor && match(ov.Keypath, names, true) {
			c = ov.Color
		}
	}
	return c
}

func (r *Renderer) strokeStyle(it *item, names []string, t float64) ui.StrokeStyle {
	w := it.Width.Scalar(t, 1)
	for _, ov := range r.Overrides {
		if ov.Kind == OverrideStrokeWidth && match(ov.Keypath, names, true) {
			w = ov.Value
		}
	}
	st := ui.StrokeStyle{Width: float32(w)}
	switch it.Cap {
	case 2:
		st.Cap = ui.CapRound
	case 3:
		st.Cap = ui.CapSquare
	}
	switch it.Join {
	case 2:
		st.Join = ui.JoinRound
	case 3:
		st.Join = ui.JoinBevel
	}
	for _, d := range it.Dashes {
		v := float32(d.Value.Scalar(t, 0))
		switch d.Kind {
		case "o":
			st.DashPhase = v
		default:
			st.Dash = append(st.Dash, v)
		}
	}
	if len(st.Dash) > 0 {
		all := float32(0)
		for _, v := range st.Dash {
			all += v
		}
		if all <= 0 {
			st.Dash = nil
		}
	}
	return st
}

// stops are a gradient's colors at t: its color stops and its opacity
// stops merged, at opacity o.
func (r *Renderer) stops(it *item, t, o float64) ([]ui.Color, []float32) {
	v := it.Stops.K.At(t, nil)
	n := it.Stops.Count
	if n <= 0 || len(v) < n*4 {
		return []ui.Color{uiColor([4]float64{0, 0, 0, o})}, nil
	}
	type stop struct{ at, r, g, b float64 }
	cs := make([]stop, n)
	for i := range n {
		cs[i] = stop{v[i*4], v[i*4+1], v[i*4+2], v[i*4+3]}
	}
	var as [][2]float64
	for i := n * 4; i+1 < len(v); i += 2 {
		as = append(as, [2]float64{v[i], v[i+1]})
	}
	at := map[float64]bool{}
	var locs []float64
	for _, c := range cs {
		if !at[c.at] {
			at[c.at] = true
			locs = append(locs, c.at)
		}
	}
	for _, a := range as {
		if !at[a[0]] {
			at[a[0]] = true
			locs = append(locs, a[0])
		}
	}
	sort.Float64s(locs)
	colorAt := func(x float64) (float64, float64, float64) {
		if x <= cs[0].at {
			return cs[0].r, cs[0].g, cs[0].b
		}
		for i := 1; i < len(cs); i++ {
			if x <= cs[i].at {
				a, b := cs[i-1], cs[i]
				f := 0.0
				if b.at > a.at {
					f = (x - a.at) / (b.at - a.at)
				}
				return a.r + (b.r-a.r)*f, a.g + (b.g-a.g)*f, a.b + (b.b-a.b)*f
			}
		}
		l := cs[len(cs)-1]
		return l.r, l.g, l.b
	}
	alphaAt := func(x float64) float64 {
		if len(as) == 0 {
			return 1
		}
		if x <= as[0][0] {
			return as[0][1]
		}
		for i := 1; i < len(as); i++ {
			if x <= as[i][0] {
				a, b := as[i-1], as[i]
				f := 0.0
				if b[0] > a[0] {
					f = (x - a[0]) / (b[0] - a[0])
				}
				return a[1] + (b[1]-a[1])*f
			}
		}
		return as[len(as)-1][1]
	}
	colors := make([]ui.Color, len(locs))
	out := make([]float32, len(locs))
	for i, x := range locs {
		cr, cg, cb := colorAt(x)
		colors[i] = uiColor([4]float64{cr, cg, cb, alphaAt(x) * o})
		out[i] = float32(x)
	}
	return colors, out
}

// opacityOverride is an opacity override naming exactly what r.names
// names.
func (r *Renderer) opacityOverride() (float64, bool) {
	v, ok := 0.0, false
	for _, ov := range r.Overrides {
		if ov.Kind == OverrideOpacity && match(ov.Keypath, r.names, false) {
			v, ok = ov.Value, true
		}
	}
	return v, ok
}

func uiColor(c [4]float64) ui.Color {
	b := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	return ops.RGBA(b(c[0]), b(c[1]), b(c[2]), b(c[3]))
}

// parseHex reads a solid layer's color, "#rrggbb".
func parseHex(s string) [4]float64 {
	s = strings.TrimPrefix(s, "#")
	if len(s) < 6 {
		return [4]float64{0, 0, 0, 1}
	}
	v, err := strconv.ParseUint(s[:6], 16, 32)
	if err != nil {
		return [4]float64{0, 0, 0, 1}
	}
	return [4]float64{float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255, 1}
}

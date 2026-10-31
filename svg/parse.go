package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/egoist/ryui/ui"
)

const svgNS = "http://www.w3.org/2000/svg"

// elem is an element of the markup: its name, attributes (by local
// name; style's declarations over them) and children.
type elem struct {
	name  string
	attrs map[string]string
	kids  []*elem
}

func (e *elem) attr(k string) (string, bool) {
	v, ok := e.attrs[k]
	return strings.TrimSpace(v), ok
}

// read parses the markup into elements, keeping those of SVG's
// namespace (or of none).
func read(markup []byte) (*elem, error) {
	d := xml.NewDecoder(bytes.NewReader(markup))
	d.Entity = xml.HTMLEntity
	var root *elem
	var stack []*elem
	skip := 0 // depth inside an element of another namespace
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("svg: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if skip > 0 || t.Name.Space != "" && t.Name.Space != svgNS {
				skip++
				continue
			}
			e := &elem{name: t.Name.Local, attrs: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				// An unprefixed attribute wins over a prefixed one
				// (href over xlink:href).
				if _, ok := e.attrs[a.Name.Local]; ok && a.Name.Space != "" {
					continue
				}
				e.attrs[a.Name.Local] = a.Value
			}
			if st, ok := e.attrs["style"]; ok {
				for _, decl := range strings.Split(st, ";") {
					k, v, ok := strings.Cut(decl, ":")
					if !ok {
						continue
					}
					v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
					e.attrs[strings.TrimSpace(k)] = v
				}
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, e)
			} else if root == nil {
				root = e
			}
			stack = append(stack, e)
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil || root.name != "svg" {
		return nil, errors.New("svg: no <svg> element")
	}
	return root, nil
}

// paintKind is what a fill or stroke property says.
type paintKind uint8

const (
	paintNone paintKind = iota
	paintColor
	paintCurrent
)

type paint struct {
	kind  paintKind
	color ui.Color
}

// state is what an element inherits, and its transform.
type state struct {
	m                 matrix
	fill, stroke      paint
	fillOp, strokeOp  float64
	opacity           float64 // the product of the ancestors' opacity
	width             float64
	cap               ui.LineCap
	join              ui.LineJoin
	color             ui.Color // the color property, for currentColor
	hasColor, visible bool
}

// parser turns elements into layers.
type parser struct {
	ids    map[string]*elem
	layers []Layer
	depth  int
}

// Parse parses SVG markup into an Icon. It returns an error for markup
// that isn't XML or has no <svg> element; what it doesn't support it
// skips.
func Parse(markup []byte) (*Icon, error) {
	root, err := read(markup)
	if err != nil {
		return nil, err
	}
	p := &parser{ids: map[string]*elem{}}
	p.index(root)
	st := state{
		m:      identity,
		fill:   paint{kind: paintCurrent}, // SVG's is black: the icon's color
		fillOp: 1, strokeOp: 1, opacity: 1,
		width: 1, visible: true,
	}
	ic := &Icon{}
	vb, hasVB := numbers(root.attrs["viewBox"], 4)
	w, hasW := length(root.attrs["width"])
	h, hasH := length(root.attrs["height"])
	p.group(root, st)
	ic.Layers = p.layers
	switch {
	case hasVB && vb[2] > 0 && vb[3] > 0:
		ic.ViewBox = [4]float32{float32(vb[0]), float32(vb[1]), float32(vb[2]), float32(vb[3])}
	case hasW && hasH && w > 0 && h > 0:
		ic.ViewBox = [4]float32{0, 0, float32(w), float32(h)}
	default:
		// Neither: the drawing's bounds.
		var box [4]float64
		seen := false
		for i := range ic.Layers {
			ic.Layers[i].Path.bounds(&box, &seen)
		}
		if !seen || box[2] <= box[0] || box[3] <= box[1] {
			return nil, errors.New("svg: no viewBox, width or height, and nothing drawn")
		}
		ic.ViewBox = [4]float32{float32(box[0]), float32(box[1]), float32(box[2] - box[0]), float32(box[3] - box[1])}
	}
	ic.Width, ic.Height = ic.ViewBox[2], ic.ViewBox[3]
	if hasW && w > 0 {
		ic.Width = float32(w)
		if !hasH {
			ic.Height = ic.Width * ic.ViewBox[3] / ic.ViewBox[2]
		}
	}
	if hasH && h > 0 {
		ic.Height = float32(h)
		if !hasW {
			ic.Width = ic.Height * ic.ViewBox[2] / ic.ViewBox[3]
		}
	}
	return ic, nil
}

// MustParse is Parse for markup known to be good (an embedded icon): it
// panics on an error.
func MustParse(markup string) *Icon {
	ic, err := Parse([]byte(markup))
	if err != nil {
		panic(err)
	}
	return ic
}

func (p *parser) index(e *elem) {
	if id, ok := e.attr("id"); ok && id != "" {
		if _, dup := p.ids[id]; !dup {
			p.ids[id] = e
		}
	}
	for _, k := range e.kids {
		p.index(k)
	}
}

// inherit applies an element's presentation attributes to what its
// parent passed down.
func inherit(e *elem, st state) state {
	if v, ok := e.attr("color"); ok {
		if c, ok := parseColor(v); ok && c.kind == paintColor {
			st.color, st.hasColor = c.color, true
		}
	}
	if v, ok := e.attr("fill"); ok {
		if c, ok := parsePaint(v); ok {
			st.fill = c
		}
	}
	if v, ok := e.attr("stroke"); ok {
		if c, ok := parsePaint(v); ok {
			st.stroke = c
		}
	}
	// fill and stroke are inherited as specified: currentColor takes the
	// color property where it's used. A color set here resolves it.
	if v, ok := e.attr("stroke-width"); ok {
		if w, ok := length(v); ok && w >= 0 {
			st.width = w
		}
	}
	if v, ok := e.attr("fill-opacity"); ok {
		st.fillOp = alpha(v, st.fillOp)
	}
	if v, ok := e.attr("stroke-opacity"); ok {
		st.strokeOp = alpha(v, st.strokeOp)
	}
	if v, ok := e.attr("opacity"); ok {
		// A group's opacity is folded into what it draws: overlapping
		// shapes inside it show through each other, which a real group
		// opacity wouldn't.
		st.opacity *= alpha(v, 1)
	}
	if v, ok := e.attr("stroke-linecap"); ok {
		switch v {
		case "butt":
			st.cap = ui.CapButt
		case "round":
			st.cap = ui.CapRound
		case "square":
			st.cap = ui.CapSquare
		}
	}
	if v, ok := e.attr("stroke-linejoin"); ok {
		switch v {
		case "miter", "miter-clip", "arcs":
			st.join = ui.JoinMiter
		case "round":
			st.join = ui.JoinRound
		case "bevel":
			st.join = ui.JoinBevel
		}
	}
	if v, ok := e.attr("visibility"); ok {
		st.visible = v != "hidden" && v != "collapse"
	}
	if v, ok := e.attr("transform"); ok {
		st.m = st.m.mul(parseTransform(v))
	}
	// fill-rule (evenodd) isn't supported: ui.Path fills by the nonzero
	// rule. clip-path, mask and filter are ignored too.
	return st
}

// group draws an element's children.
func (p *parser) group(e *elem, st state) {
	st = inherit(e, st)
	for _, k := range e.kids {
		p.element(k, st)
	}
}

func (p *parser) element(e *elem, st state) {
	if v, _ := e.attr("display"); v == "none" {
		return
	}
	switch e.name {
	case "g", "a", "switch":
		p.group(e, st)
	case "svg":
		// A nested viewport: its x and y move it; its own viewBox and
		// clipping are ignored.
		x, _ := length(e.attrs["x"])
		y, _ := length(e.attrs["y"])
		st.m = st.m.mul(matrix{1, 0, 0, 1, x, y})
		p.group(e, st)
	case "use":
		p.use(e, st)
	case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
		p.shape(e, st)
	}
	// Anything else (defs, symbol, clipPath, mask, gradients, patterns,
	// markers, text, image, style, title) draws nothing.
}

// use draws the element it refers to, moved by its x and y.
func (p *parser) use(e *elem, st state) {
	href, _ := e.attr("href")
	ref := p.ids[strings.TrimPrefix(href, "#")]
	if !strings.HasPrefix(href, "#") || ref == nil || p.depth > 16 {
		return
	}
	st = inherit(e, st)
	x, _ := length(e.attrs["x"])
	y, _ := length(e.attrs["y"])
	st.m = st.m.mul(matrix{1, 0, 0, 1, x, y})
	p.depth++
	defer func() { p.depth-- }()
	if ref.name == "symbol" {
		p.group(ref, st)
		return
	}
	p.element(ref, st)
}

// shape draws a basic shape or a path as a layer.
func (p *parser) shape(e *elem, st state) {
	st = inherit(e, st)
	if !st.visible {
		return
	}
	var path Path
	b := builder{p: &path, m: st.m}
	num := func(k string) float64 { v, _ := length(e.attrs[k]); return v }
	switch e.name {
	case "path":
		b.pathData(e.attrs["d"])
	case "rect":
		w, h := num("width"), num("height")
		if w <= 0 || h <= 0 {
			return
		}
		rx, hasRx := length(e.attrs["rx"])
		ry, hasRy := length(e.attrs["ry"])
		if !hasRx || rx < 0 {
			rx, hasRx = 0, false
		}
		if !hasRy || ry < 0 {
			ry, hasRy = 0, false
		}
		switch {
		case hasRx && !hasRy:
			ry = rx
		case hasRy && !hasRx:
			rx = ry
		}
		b.rect(num("x"), num("y"), w, h, rx, ry)
	case "circle":
		r := num("r")
		if r <= 0 {
			return
		}
		b.ellipse(num("cx"), num("cy"), r, r)
	case "ellipse":
		rx, ry := num("rx"), num("ry")
		if rx <= 0 || ry <= 0 {
			return
		}
		b.ellipse(num("cx"), num("cy"), rx, ry)
	case "line":
		b.moveTo(num("x1"), num("y1"))
		b.lineTo(num("x2"), num("y2"))
		st.fill = paint{} // a line has no inside
	case "polyline", "polygon":
		b.points(e.attrs["points"], e.name == "polygon")
	}
	if path.Empty() {
		return
	}
	l := Layer{
		Path:      path,
		Fill:      st.resolve(st.fill, st.fillOp),
		Stroke:    st.resolve(st.stroke, st.strokeOp),
		LineWidth: float32(st.width * st.m.scale()),
		LineCap:   st.cap,
		LineJoin:  st.join,
	}
	if l.LineWidth <= 0 {
		l.Stroke = Paint{}
	}
	if l.Stroke.None() {
		l.LineWidth, l.LineCap, l.LineJoin = 0, ui.CapButt, ui.JoinMiter
	}
	if l.Fill.None() && l.Stroke.None() {
		return
	}
	// Strokes painted alike are one layer, one native view (a Lucide
	// icon is one). Fills aren't merged: their outlines would wind
	// together, and translucent ones would no longer overlap.
	if n := len(p.layers); n > 0 {
		last := &p.layers[n-1]
		if last.Fill.None() && l.Fill.None() && last.Stroke == l.Stroke && l.Stroke.opaque() &&
			last.LineWidth == l.LineWidth && last.LineCap == l.LineCap && last.LineJoin == l.LineJoin {
			last.Path.cmds = append(last.Path.cmds, l.Path.cmds...)
			last.Path.args = append(last.Path.args, l.Path.args...)
			return
		}
	}
	p.layers = append(p.layers, l)
}

// resolve makes a property's paint a layer's, with its opacity folded in.
func (st *state) resolve(c paint, op float64) Paint {
	op *= st.opacity
	if c.kind == paintCurrent && st.hasColor {
		c = paint{kind: paintColor, color: st.color}
	}
	if op <= 0 {
		return Paint{}
	}
	switch c.kind {
	case paintColor:
		r, g, b, a, _ := c.color.RGBA()
		a = uint8(math.Round(float64(a) * min(op, 1)))
		if a == 0 {
			return Paint{}
		}
		return Paint{Color: ui.RGBA(r, g, b, a)}
	case paintCurrent:
		return Paint{Current: true, Opacity: float32(min(op, 1))}
	}
	return Paint{}
}

// parsePaint parses a fill or stroke: none, a color, currentColor, or a
// url() (a gradient or pattern: painted with the icon's color, or the
// fallback color after it).
func parsePaint(v string) (paint, bool) {
	if strings.HasPrefix(v, "url(") {
		if i := strings.IndexByte(v, ')'); i >= 0 {
			if c, ok := parseColor(strings.TrimSpace(v[i+1:])); ok {
				return c, true
			}
		}
		return paint{kind: paintCurrent}, true
	}
	return parseColor(v)
}

var named = map[string]uint32{
	"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000,
	"blue": 0x0000ff, "yellow": 0xffff00, "orange": 0xffa500, "purple": 0x800080,
	"gray": 0x808080, "grey": 0x808080, "silver": 0xc0c0c0, "navy": 0x000080,
	"teal": 0x008080, "maroon": 0x800000, "lime": 0x00ff00, "aqua": 0x00ffff,
	"cyan": 0x00ffff, "fuchsia": 0xff00ff, "magenta": 0xff00ff, "olive": 0x808000,
	"pink": 0xffc0cb, "brown": 0xa52a2a, "gold": 0xffd700,
	"darkgray": 0xa9a9a9, "darkgrey": 0xa9a9a9, "lightgray": 0xd3d3d3, "lightgrey": 0xd3d3d3,
}

// parseColor parses none, currentColor, #rgb, #rgba, #rrggbb,
// #rrggbbaa, rgb() and rgba(), and a few color names.
func parseColor(v string) (paint, bool) {
	v = strings.TrimSpace(v)
	lv := strings.ToLower(v)
	switch lv {
	case "none", "transparent":
		return paint{}, true
	case "currentcolor":
		return paint{kind: paintCurrent}, true
	case "":
		return paint{}, false
	}
	if c, ok := named[lv]; ok {
		return paint{kind: paintColor, color: ui.Hex(c)}, true
	}
	if strings.HasPrefix(lv, "#") {
		h := lv[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for i := range len(h) {
				b.WriteByte(h[i])
				b.WriteByte(h[i])
			}
			h = b.String()
		}
		if c, ok := ui.ParseHex(h); ok {
			return paint{kind: paintColor, color: c}, true
		}
		return paint{}, false
	}
	if strings.HasPrefix(lv, "rgb") {
		open, end := strings.IndexByte(lv, '('), strings.LastIndexByte(lv, ')')
		if open < 0 || end < open {
			return paint{}, false
		}
		f := strings.Fields(strings.NewReplacer(",", " ", "/", " ").Replace(lv[open+1 : end]))
		if len(f) != 3 && len(f) != 4 {
			return paint{}, false
		}
		var ch [4]uint8
		ch[3] = 255
		for i, s := range f {
			pct := strings.HasSuffix(s, "%")
			x, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
			if err != nil {
				return paint{}, false
			}
			switch {
			case i == 3 && pct:
				x = x / 100 * 255
			case i == 3:
				x *= 255
			case pct:
				x = x / 100 * 255
			}
			ch[i] = uint8(math.Round(min(max(x, 0), 255)))
		}
		return paint{kind: paintColor, color: ui.RGBA(ch[0], ch[1], ch[2], ch[3])}, true
	}
	return paint{}, false
}

// alpha parses an opacity, a number or a percentage, clamped to 0..1.
func alpha(v string, def float64) float64 {
	pct := strings.HasSuffix(v, "%")
	x, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "%")), 64)
	if err != nil {
		return def
	}
	if pct {
		x /= 100
	}
	return min(max(x, 0), 1)
}

// length parses a length in user units: a number, "px" and the like
// ignored; a percentage isn't one.
func length(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasSuffix(v, "%") {
		return 0, false
	}
	s := scanner{s: v}
	return s.num()
}

// numbers parses n numbers (a viewBox).
func numbers(v string, n int) ([]float64, bool) {
	s := scanner{s: v}
	out := make([]float64, n)
	return out, s.nums(out)
}

// parseTransform parses a transform list: matrix, translate, scale,
// rotate (around a point, given one), skewX and skewY. It stops at what
// it can't read.
func parseTransform(v string) matrix {
	m := identity
	s := scanner{s: v}
	for {
		s.skip()
		start := s.i
		for s.i < len(s.s) && (s.s[s.i] >= 'a' && s.s[s.i] <= 'z' || s.s[s.i] >= 'A' && s.s[s.i] <= 'Z') {
			s.i++
		}
		name := s.s[start:s.i]
		if name == "" {
			return m
		}
		for s.i < len(s.s) && (s.s[s.i] == ' ' || s.s[s.i] == '\t' || s.s[s.i] == '\n' || s.s[s.i] == '\r') {
			s.i++
		}
		if s.i >= len(s.s) || s.s[s.i] != '(' {
			return m
		}
		s.i++
		var a []float64
		for {
			x, ok := s.num()
			if !ok {
				break
			}
			a = append(a, x)
		}
		s.skip()
		if s.i >= len(s.s) || s.s[s.i] != ')' {
			return m
		}
		s.i++
		arg := func(i int, def float64) float64 {
			if i < len(a) {
				return a[i]
			}
			return def
		}
		var t matrix
		switch name {
		case "matrix":
			if len(a) != 6 {
				return m
			}
			t = matrix{a[0], a[1], a[2], a[3], a[4], a[5]}
		case "translate":
			t = matrix{1, 0, 0, 1, arg(0, 0), arg(1, 0)}
		case "scale":
			sx := arg(0, 1)
			t = matrix{sx, 0, 0, arg(1, sx), 0, 0}
		case "rotate":
			sin, cos := math.Sincos(arg(0, 0) * math.Pi / 180)
			t = matrix{cos, sin, -sin, cos, 0, 0}
			if len(a) == 3 {
				cx, cy := a[1], a[2]
				t = matrix{1, 0, 0, 1, cx, cy}.mul(t).mul(matrix{1, 0, 0, 1, -cx, -cy})
			}
		case "skewX":
			t = matrix{1, 0, math.Tan(arg(0, 0) * math.Pi / 180), 1, 0, 0}
		case "skewY":
			t = matrix{1, math.Tan(arg(0, 0) * math.Pi / 180), 0, 1, 0, 0}
		default:
			return m
		}
		m = m.mul(t)
	}
}

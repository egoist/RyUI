// Package render draws Lottie animations with ui.Graphics, where the
// platform has no Lottie player to call (Windows and Linux): it reads a
// Lottie file (Bodymovin's JSON; a dotLottie's), evaluates its layers at
// a frame, and draws their shapes as the players do.
//
// It draws shape, solid, null and precomposition layers: rectangles,
// ellipses, polystars and paths; fills and strokes, solid or gradient
// (linear and radial, with a highlight's focus), dashed; trim paths,
// round corners and repeaters; groups' and layers' transforms (anchor,
// position, separate or along a curve, scale, rotation, skew, opacity),
// parenting, time remapping and stretch; keyframes with bezier easing,
// holds and spatial curves. Masks (add, subtract, intersect, inverted)
// and track mattes (alpha and luma, inverted) clip by their outlines.
//
// It doesn't draw: image and text layers, expressions, effects, layer
// styles, blend modes, 3D, merge paths (it draws the paths as they are),
// offset paths, pucker and bloat, twist, zig zag; a mask's opacity,
// feather and expansion, or a matte's own transparency (a matte clips by
// its layer's filled outlines; a precomposition matte by its bounds).
package render

import (
	"encoding/json"
	"errors"
)

// Animation is a Lottie animation, read.
type Animation struct {
	Width, Height float64
	FrameRate     float64
	In, Out       float64 // its first frame and the frame after its last
	Markers       []Marker
	layers        []*Layer
	assets        map[string]*asset
}

// Marker is a named range of an animation's frames.
type Marker struct {
	Name            string
	Start, Duration float64 // frames
}

type asset struct {
	ID     string   `json:"id"`
	Layers []*Layer `json:"layers"`
	W      float64  `json:"w"`
	H      float64  `json:"h"`
}

// Layer types.
const (
	layerPrecomp = 0
	layerSolid   = 1
	layerImage   = 2
	layerNull    = 3
	layerShape   = 4
	layerText    = 5
)

// Layer is one of a composition's layers.
type Layer struct {
	Name       string    `json:"nm"`
	Type       int       `json:"ty"`
	Index      *int      `json:"ind"`
	Parent     *int      `json:"parent"`
	In         float64   `json:"ip"`
	Out        float64   `json:"op"`
	Start      float64   `json:"st"`
	Stretch    float64   `json:"sr"`
	Transform  transform `json:"ks"`
	Shapes     []*item   `json:"shapes"`
	Ref        string    `json:"refId"`
	W          float64   `json:"w"`
	H          float64   `json:"h"`
	TimeRemap  *Value    `json:"tm"`
	Solid      string    `json:"sc"`
	SolidW     float64   `json:"sw"`
	SolidH     float64   `json:"sh"`
	Masks      []mask    `json:"masksProperties"`
	Matte      int       `json:"tt"` // 1 alpha, 2 inverted alpha, 3 luma, 4 inverted luma
	IsMatte    int       `json:"td"`
	MatteRef   *int      `json:"tp"` // the matte layer's index (newer files)
	Hidden     bool      `json:"hd"`
	AutoOrient int       `json:"ao"`

	parent *Layer
	matte  *Layer
	comp   *asset
}

// transform is a layer's or a group's.
type transform struct {
	Anchor   Value `json:"a"`
	Position Value `json:"p"`
	Scale    Value `json:"s"`
	Rotation Value `json:"r"`
	RotZ     Value `json:"rz"`
	Opacity  Value `json:"o"`
	Skew     Value `json:"sk"`
	SkewAxis Value `json:"sa"`
	// A repeater's: its copies' opacity, from the first to the last.
	StartOpacity Value `json:"so"`
	EndOpacity   Value `json:"eo"`
}

// matrix is the transform at t: translate(-anchor), scale, skew, rotate
// (by turn degrees more), translate(position), as After Effects composes
// them.
func (tr *transform) matrix(t, turn float64) Matrix {
	a := tr.Anchor.Vec(t, Vec{})
	p := tr.Position.Vec(t, Vec{})
	s := tr.Scale.Vec(t, Vec{100, 100})
	r := tr.Rotation.Scalar(t, 0) + tr.RotZ.Scalar(t, 0) + turn
	m := translate(p.X, p.Y).Mul(rotate(r))
	if tr.Skew.set {
		m = m.Mul(skew(tr.Skew.Scalar(t, 0), tr.SkewAxis.Scalar(t, 0)))
	}
	return m.Mul(scale(s.X/100, s.Y/100)).Mul(translate(-a.X, -a.Y))
}

func (tr *transform) opacity(t float64) float64 { return tr.Opacity.Scalar(t, 100) / 100 }

type mask struct {
	Mode     string     `json:"mode"`
	Path     ShapeValue `json:"pt"`
	Opacity  Value      `json:"o"`
	Inverted bool       `json:"inv"`
}

// item is a shape layer's item: a shape, a paint, a modifier, a group or
// a group's transform.
type item struct {
	Type   string  `json:"ty"`
	Name   string  `json:"nm"`
	Hidden bool    `json:"hd"`
	Items  []*item `json:"it"` // a group's

	// Shapes.
	Path      ShapeValue `json:"ks"` // sh
	Direction int        `json:"-"`  // d: 3, counterclockwise
	P         Value      `json:"p"`  // rc, el, sr: the center
	S         Value      `json:"s"`  // rc, el: the size; gradients: the start point; tm: start
	R         Value      `json:"r"`  // rc: the corner radius; sr: rotation; rd: the radius; fl: (an int) the fill rule
	StarType  int        `json:"sy"` // 1 star, 2 polygon
	Points    Value      `json:"pt"`
	InnerR    Value      `json:"ir"`
	InnerRnd  Value      `json:"is"`
	OuterR    Value      `json:"or"`
	OuterRnd  Value      `json:"os"`

	// Paints.
	Color      Value      `json:"c"` // fl, st
	O          Value      `json:"o"` // opacity; tm: the offset; rp: the copies' offset
	Width      Value      `json:"w"`
	Cap        int        `json:"lc"`
	Join       int        `json:"lj"`
	MiterLimit float64    `json:"ml"`
	Dashes     []dash     `json:"-"` // st, gs: read from "d"
	GradType   int        `json:"t"` // gf, gs: 1 linear, 2 radial
	E          Value      `json:"e"` // gradients: the end point; tm: end
	Stops      gradStops  `json:"g"`
	Highlight  Value      `json:"h"`
	HAngle     Value      `json:"a"`
	FillRule   int        `json:"-"`
	TrimMode   int        `json:"m"`  // tm: 1 simultaneously, 2 individually; rp: 1 above, 2 below
	Copies     Value      `json:"-"`  // rp: read from "c"
	RepeatTr   *transform `json:"tr"` // rp

	// A group's transform (ty "tr").
	transform
}

type dash struct {
	Kind  string `json:"n"` // d: dash, g: gap, o: offset
	Value Value  `json:"v"`
}

type gradStops struct {
	Count int   `json:"p"`
	K     Value `json:"k"`
}

func (it *item) UnmarshalJSON(b []byte) error {
	type plain item
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*it = item(p)
	// Keys whose meaning depends on the item's type.
	var extra struct {
		D json.RawMessage `json:"d"`
		R json.RawMessage `json:"r"`
		C json.RawMessage `json:"c"`
	}
	json.Unmarshal(b, &extra)
	switch it.Type {
	case "st", "gs":
		json.Unmarshal(extra.D, &it.Dashes)
	case "fl", "gf":
		var rule int
		if json.Unmarshal(extra.R, &rule) == nil {
			it.FillRule = rule
		}
		it.R = Value{}
	case "rp":
		json.Unmarshal(extra.C, &it.Copies)
		it.Color = Value{}
	}
	if it.Type != "st" && it.Type != "gs" {
		var d int
		if json.Unmarshal(extra.D, &d) == nil {
			it.Direction = d
		}
	}
	if it.Type == "tr" {
		json.Unmarshal(b, &it.transform)
	}
	return nil
}

type rawAnimation struct {
	W       float64  `json:"w"`
	H       float64  `json:"h"`
	Fr      float64  `json:"fr"`
	Ip      float64  `json:"ip"`
	Op      float64  `json:"op"`
	Layers  []*Layer `json:"layers"`
	Assets  []*asset `json:"assets"`
	Markers []struct {
		Name     string  `json:"cm"`
		Time     float64 `json:"tm"`
		Duration float64 `json:"dr"`
	} `json:"markers"`
}

// ErrNotLottie is a file that isn't a Lottie animation.
var ErrNotLottie = errors.New("lottie: not a Lottie animation")

// Parse reads a Lottie animation: Bodymovin's JSON, or a dotLottie
// (ParseDotLottie, for one of its animations by ID).
func Parse(data []byte) (*Animation, error) {
	if isZip(data) {
		return ParseDotLottie(data, "")
	}
	var r rawAnimation
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.W <= 0 || r.H <= 0 || r.Op <= r.Ip {
		return nil, ErrNotLottie
	}
	if r.Fr <= 0 {
		r.Fr = 30
	}
	a := &Animation{Width: r.W, Height: r.H, FrameRate: r.Fr, In: r.Ip, Out: r.Op, layers: r.Layers, assets: map[string]*asset{}}
	for _, m := range r.Markers {
		a.Markers = append(a.Markers, Marker{Name: m.Name, Start: m.Time, Duration: m.Duration})
	}
	for _, as := range r.Assets {
		if as.Layers != nil {
			a.assets[as.ID] = as
		}
	}
	link(a, a.layers)
	for _, as := range a.assets {
		link(a, as.Layers)
	}
	return a, nil
}

// link connects a composition's layers to their parents, mattes and
// precompositions.
func link(a *Animation, layers []*Layer) {
	byIndex := map[int]*Layer{}
	for _, l := range layers {
		if l.Index != nil {
			byIndex[*l.Index] = l
		}
		if l.Stretch == 0 {
			l.Stretch = 1
		}
		if l.Type == layerPrecomp {
			l.comp = a.assets[l.Ref]
		}
	}
	for i, l := range layers {
		if l.Parent != nil {
			if p := byIndex[*l.Parent]; p != nil && p != l {
				l.parent = p
			}
		}
		if l.Matte != 0 {
			switch {
			case l.MatteRef != nil:
				l.matte = byIndex[*l.MatteRef]
			case i > 0:
				l.matte = layers[i-1]
			}
		}
	}
}

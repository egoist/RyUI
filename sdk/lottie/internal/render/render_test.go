package render

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

func success(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../../../examples/gallery/assets/success.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse(t *testing.T) {
	a, err := Parse(success(t))
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 200 || a.Height != 200 || a.FrameRate != 30 || a.In != 0 || a.Out != 90 {
		t.Fatalf("%+v", a)
	}
	if len(a.Markers) != 3 || a.Markers[1] != (Marker{"check", 16, 20}) {
		t.Fatalf("markers %+v", a.Markers)
	}
	var shine, sparkles *Layer
	for _, l := range a.layers {
		switch l.Name {
		case "Shine":
			shine = l
		case "Sparkles":
			sparkles = l
		}
	}
	if shine == nil || shine.matte == nil || shine.matte.Name != "Circle Matte" {
		t.Fatal("the shine's matte isn't linked")
	}
	if sparkles == nil || sparkles.comp == nil || len(sparkles.comp.Layers) != 1 {
		t.Fatal("the precomposition isn't linked")
	}
	if _, err := Parse([]byte(`{"w":0}`)); err != ErrNotLottie {
		t.Errorf("not an animation: %v", err)
	}
}

func TestValues(t *testing.T) {
	var v Value
	json.Unmarshal([]byte(`{"a":1,"k":[{"t":0,"s":[0],"o":{"x":[0.42],"y":[0]},"i":{"x":[0.58],"y":[1]}},{"t":10,"s":[100],"h":1},{"t":20,"s":[50]}]}`), &v)
	if got := v.Scalar(5, -1); math.Abs(got-50) > 1e-6 {
		t.Errorf("ease-in-out's middle: %v", got)
	}
	if got := v.Scalar(2, -1); got >= 20 || got <= 0 {
		t.Errorf("ease-in-out starts slow: %v", got)
	}
	if got := v.Scalar(15, -1); got != 100 {
		t.Errorf("a hold keyframe: %v", got)
	}
	if v.Scalar(-5, -1) != 0 || v.Scalar(25, -1) != 50 {
		t.Error("before the first, after the last")
	}
	// A position along its curve (to, ti): half way along it is the
	// arc's middle, not the chord's.
	var p Value
	json.Unmarshal([]byte(`{"a":1,"k":[{"t":0,"s":[0,0],"to":[0,-50],"ti":[0,-50],"o":{"x":0,"y":0},"i":{"x":1,"y":1}},{"t":10,"s":[100,0]}]}`), &p)
	if mid := p.Vec(5, Vec{}); math.Abs(mid.X-50) > 1 || mid.Y > -30 {
		t.Errorf("a spatial curve's middle: %v", mid)
	}
	// A split position.
	var s Value
	json.Unmarshal([]byte(`{"s":true,"x":{"a":0,"k":7},"y":{"a":1,"k":[{"t":0,"s":[0]},{"t":10,"s":[10]}]}}`), &s)
	if got := s.Vec(5, Vec{}); got != (Vec{7, 5}) {
		t.Errorf("a split position: %v", got)
	}
	var e easing = easing{0.42, 0, 0.58, 1}
	if e.at(0.5) < 0.499 || e.at(0.5) > 0.501 || linearEasing.at(0.3) != 0.3 {
		t.Error("easing")
	}
}

func line(a, b Vec) Cubic { return Cubic{a, a, b, b} }

func TestTrim(t *testing.T) {
	p := Path{Contours: []Contour{{Segs: []Cubic{line(Vec{0, 0}, Vec{100, 0})}}}}
	q := Path{Contours: []Contour{{Segs: []Cubic{line(Vec{0, 10}, Vec{100, 10})}}}}
	out := trim([]Path{p, q}, 0.25, 0.75, 0, false)
	for _, o := range out {
		s := o.Contours[0].Segs[0]
		if math.Abs(s.P0.X-25) > 0.01 || math.Abs(s.P3.X-75) > 0.01 {
			t.Fatalf("simultaneously: %v", s)
		}
	}
	// Individually: the two as one run of 200; 0 to 0.75 is all the
	// first and half the second.
	out = trim([]Path{p, q}, 0, 0.75, 0, true)
	if l, _ := out[0].Contours[0].length(); math.Abs(l-100) > 0.01 {
		t.Errorf("the first: %v", l)
	}
	if l, _ := out[1].Contours[0].length(); math.Abs(l-50) > 0.01 {
		t.Errorf("the second: %v", l)
	}
	// Past the end of a closed contour: around through its start.
	sq := rectPath(0, 0, 10, 10)
	out = trim([]Path{sq}, 0.9, 1.1, 0, false)
	if l, _ := out[0].Contours[0].length(); math.Abs(l-8) > 0.01 || len(out[0].Contours) != 1 {
		t.Errorf("around: %v in %d", l, len(out[0].Contours))
	}
	if e := trim([]Path{p}, 0.5, 0.5, 0, false); len(e[0].Contours) != 0 {
		t.Error("an empty trim")
	}
}

func TestRoundCorners(t *testing.T) {
	c := roundCorners(rectPath(0, 0, 100, 100).Contours[0], 10)
	if len(c.Segs) != 8 {
		t.Fatalf("%d segments", len(c.Segs))
	}
	for _, s := range c.Segs {
		for _, p := range []Vec{s.P0, s.P3} {
			if (p.X == 0 || p.X == 100) && (p.Y == 0 || p.Y == 100) {
				t.Fatalf("a corner left sharp: %v", p)
			}
		}
	}
	if a := signedArea(c); a < 9900 || a > 9990 {
		t.Errorf("area %v", a)
	}
}

func TestMatch(t *testing.T) {
	path := []string{"Layer", "Group", "Fill 1"}
	for _, c := range []struct {
		pattern string
		prefix  bool
		want    bool
	}{
		{"Layer.Group.Fill 1", false, true},
		{"Layer", false, false},
		{"Layer", true, true},
		{"**.Fill 1", false, true},
		{"*.Group.*", false, true},
		{"Layer.*", false, false},
		{"Layer.*", true, true},
		{"Other", true, false},
		{"**", false, true},
	} {
		if got := match(ParseKeypath(c.pattern), path, c.prefix); got != c.want {
			t.Errorf("%q (prefix %v): %v", c.pattern, c.prefix, got)
		}
	}
}

func TestDotLottie(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	small := `{"w":10,"h":20,"fr":24,"ip":0,"op":12,"layers":[]}`
	other := `{"w":30,"h":30,"fr":60,"ip":0,"op":60,"layers":[]}`
	add("manifest.json", `{"animations":[{"id":"small"},{"id":"other"}]}`)
	add("animations/small.json", small)
	add("a/other.json", other) // dotLottie 2's folder
	z.Close()
	a, err := Parse(buf.Bytes())
	if err != nil || a.Width != 10 || a.FrameRate != 24 {
		t.Fatalf("the first: %+v %v", a, err)
	}
	a, err = ParseDotLottie(buf.Bytes(), "other")
	if err != nil || a.Width != 30 || a.FrameRate != 60 {
		t.Fatalf("by ID: %+v %v", a, err)
	}
	if _, err := ParseDotLottie(buf.Bytes(), "none"); err == nil {
		t.Error("a missing ID")
	}
}

// drawn is what Draw draws at frame f, as a Canvas's commands, through
// the fake host.
func drawn(t *testing.T, r *Renderer, f float64) []byte {
	t.Helper()
	h := fake.New()
	root := ui.Mount(h, func() ui.Node {
		return ui.Canvas(func(g *ui.Graphics) {
			w, ht := g.Size()
			r.Draw(g, f, float64(w), float64(ht), ops.ImageModeFit)
		}, ui.Style{Width: ui.Pt(200), Height: ui.Pt(200)})
	})
	t.Cleanup(root.Dispose)
	return h.OfKind(ops.KindCanvas)[0].Props[ops.PropCanvasCommands].Bytes
}

// census counts a display list's commands, and gathers its fills' and
// strokes' colors.
type census struct {
	count  map[ops.CanvasOp]int
	colors []ops.Color
}

func count(t *testing.T, b []byte) census {
	c := census{count: map[ops.CanvasOp]int{}}
	i := 0
	u32 := func() int { v := int(binary.LittleEndian.Uint32(b[i:])); i += 4; return v }
	skip := func(n int) { i += n }
	path := func() { n := u32(); skip(n) }
	stops := func() { n := u32(); skip(n * 12) }
	strokeStyle := func() { skip(4 + 2); n := u32(); skip(4 + 4*n) }
	for i < len(b) {
		op := ops.CanvasOp(b[i])
		i++
		c.count[op]++
		switch op {
		case ops.CanvasOpSave, ops.CanvasOpRestore:
		case ops.CanvasOpTranslate, ops.CanvasOpScale:
			skip(8)
		case ops.CanvasOpRotate, ops.CanvasOpAlpha:
			skip(4)
		case ops.CanvasOpTransform:
			skip(24)
		case ops.CanvasOpFillRule:
			skip(1)
		case ops.CanvasOpClip:
			path()
		case ops.CanvasOpFill:
			c.colors = append(c.colors, ops.Color(binary.LittleEndian.Uint64(b[i:])))
			skip(8)
			path()
		case ops.CanvasOpStroke:
			c.colors = append(c.colors, ops.Color(binary.LittleEndian.Uint64(b[i:])))
			skip(8)
			strokeStyle()
			path()
		case ops.CanvasOpGradient:
			stops()
			skip(16)
			path()
		case ops.CanvasOpRadialGradient:
			stops()
			skip(20)
			path()
		case ops.CanvasOpStrokeGradient:
			strokeStyle()
			kind := b[i]
			skip(1)
			stops()
			if kind == 1 {
				skip(20)
			} else {
				skip(16)
			}
			path()
		default:
			t.Fatalf("op %v at %d", op, i-1)
		}
	}
	return c
}

// The success animation, drawn: the gradient circle, the ring's and the
// check's strokes (trimmed: none at the start), the glow's mask and the
// shine's matte as clips, the precomposition's stars once they show.
func TestDraw(t *testing.T) {
	a, err := Parse(success(t))
	if err != nil {
		t.Fatal(err)
	}
	r := &Renderer{A: a}
	at0 := count(t, drawn(t, r, 0))
	if at0.count[ops.CanvasOpStroke] != 0 {
		t.Errorf("at frame 0, the trims hide the strokes: %d", at0.count[ops.CanvasOpStroke])
	}
	at40 := count(t, drawn(t, r, 40))
	if n := at40.count[ops.CanvasOpStroke]; n != 2 {
		t.Errorf("at frame 40, the ring and the check: %d strokes", n)
	}
	if at40.count[ops.CanvasOpRadialGradient] != 1 {
		t.Error("the circle's radial gradient")
	}
	// The draw's clip to its box, the glow's mask, the shine's matte, the
	// precomposition's bounds.
	if n := at40.count[ops.CanvasOpClip]; n != 4 {
		t.Errorf("%d clips", n)
	}
	// Eight stars (the repeater's copies), the glow, the shine.
	if n := at40.count[ops.CanvasOpFill]; n != 10 {
		t.Errorf("%d fills", n)
	}
	if at40.count[ops.CanvasOpTransform] == 0 || at40.count[ops.CanvasOpSave] != at40.count[ops.CanvasOpRestore] {
		t.Error("transforms, and saves restored")
	}
	// An override colors the ring's stroke (under its layer), the rest
	// as they were.
	green := uiColor([4]float64{0.13, 0.70, 0.42, 1})
	blue := ops.RGBA(10, 132, 255, 255)
	if !contains(at40.colors, green) {
		t.Fatalf("the ring's own green: %v", at40.colors)
	}
	r.Overrides = []Override{{Keypath: ParseKeypath("Ring"), Kind: OverrideColor, Color: [4]float64{10.0 / 255, 132.0 / 255, 1, 1}}}
	over := count(t, drawn(t, r, 40))
	if !contains(over.colors, blue) || contains(over.colors, green) {
		t.Errorf("overridden: %v", over.colors)
	}
	// An opacity override hides the check.
	r.Overrides = []Override{{Keypath: ParseKeypath("Check"), Kind: OverrideOpacity, Value: 0}}
	if n := count(t, drawn(t, r, 40)).count[ops.CanvasOpStroke]; n != 1 {
		t.Errorf("with the check hidden: %d strokes", n)
	}
}

func contains(cs []ops.Color, c ops.Color) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

func TestBox(t *testing.T) {
	a := &Animation{Width: 200, Height: 100}
	if m := a.Box(100, 100, ops.ImageModeFit); m.A != 0.5 || m.D != 0.5 || m.F != 25 {
		t.Errorf("fit %+v", m)
	}
	if m := a.Box(100, 100, ops.ImageModeFill); m.A != 1 || m.E != -50 {
		t.Errorf("fill %+v", m)
	}
	if m := a.Box(100, 100, ops.ImageModeStretch); m.A != 0.5 || m.D != 1 {
		t.Errorf("stretch %+v", m)
	}
	if m := a.Box(100, 100, ops.ImageModeCenter); m.A != 1 || m.E != -50 || m.F != 0 {
		t.Errorf("center %+v", m)
	}
}

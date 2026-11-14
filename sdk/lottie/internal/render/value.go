package render

import (
	"encoding/json"
	"math"
	"sort"
)

// Value is an animated property of a number or a few (a position, a
// color): its value, or keyframes it moves between.
type Value struct {
	static []float64
	keys   []keyframe
	// split is a position animated by each axis on its own.
	split *[2]Value
	// set says the file gave the property (an absent one keeps its default).
	set bool
}

type keyframe struct {
	t      float64
	s, e   []float64 // its start value, and its end (the next's start, if absent)
	hold   bool
	ease   []easing // per dimension (or one for all)
	to, ti []float64
	path   *spatial // a position's curve to the next keyframe
}

// easing is a keyframe's timing: a cubic Bézier from (0, 0) to (1, 1)
// through (x1, y1) and (x2, y2), as CSS's cubic-bezier.
type easing struct{ x1, y1, x2, y2 float64 }

var linearEasing = easing{0, 0, 1, 1}

// at is the eased progress at x (0 to 1): y where the curve's x is x.
func (e easing) at(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	if e.x1 == e.y1 && e.x2 == e.y2 {
		return x
	}
	bez := func(t, p1, p2 float64) float64 {
		u := 1 - t
		return 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t
	}
	// Newton's method, then bisection where it doesn't converge.
	t := x
	for range 8 {
		d := bez(t, e.x1, e.x2) - x
		if math.Abs(d) < 1e-7 {
			return bez(t, e.y1, e.y2)
		}
		u := 1 - t
		slope := 3*u*u*e.x1 + 6*u*t*(e.x2-e.x1) + 3*t*t*(1-e.x2)
		if math.Abs(slope) < 1e-6 {
			break
		}
		t -= d / slope
	}
	lo, hi := 0.0, 1.0
	t = x
	for range 40 {
		v := bez(t, e.x1, e.x2)
		if math.Abs(v-x) < 1e-7 {
			break
		}
		if v < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(t, e.y1, e.y2)
}

// spatial is a position keyframe's curve, sampled by length.
type spatial struct {
	c    Cubic
	lens []float64 // cumulative, at lengthSteps+1 points
}

func newSpatial(c Cubic) *spatial {
	s := &spatial{c: c, lens: make([]float64, lengthSteps+1)}
	prev := c.P0
	for i := 1; i <= lengthSteps; i++ {
		p := c.at(float64(i) / lengthSteps)
		s.lens[i] = s.lens[i-1] + p.Sub(prev).Len()
		prev = p
	}
	return s
}

// at is the point at fraction f of the curve's length.
func (s *spatial) at(f float64) Vec {
	total := s.lens[lengthSteps]
	if total == 0 {
		return lerpVec(s.c.P0, s.c.P3, f)
	}
	d := f * total
	i := sort.SearchFloat64s(s.lens, d)
	if i <= 0 {
		return s.c.P0
	}
	if i > lengthSteps {
		return s.c.P3
	}
	seg := s.lens[i] - s.lens[i-1]
	t := float64(i-1) / lengthSteps
	if seg > 0 {
		t += (d - s.lens[i-1]) / seg / lengthSteps
	}
	return s.c.at(t)
}

// rawValue is a property as the file has it: a, k (a value or
// keyframes), and a split position's x and y.
type rawValue struct {
	A json.RawMessage `json:"a"`
	K json.RawMessage `json:"k"`
	S bool            `json:"s"`
	X *rawValue       `json:"x"`
	Y *rawValue       `json:"y"`
}

type rawKey struct {
	T  float64         `json:"t"`
	S  json.RawMessage `json:"s"`
	E  json.RawMessage `json:"e"`
	H  int             `json:"h"`
	I  *rawEase        `json:"i"`
	O  *rawEase        `json:"o"`
	To []float64       `json:"to"`
	Ti []float64       `json:"ti"`
}

type rawEase struct {
	X json.RawMessage `json:"x"`
	Y json.RawMessage `json:"y"`
}

// numbers reads a number or an array of numbers.
func numbers(b json.RawMessage) []float64 {
	if len(b) == 0 {
		return nil
	}
	var f float64
	if json.Unmarshal(b, &f) == nil {
		return []float64{f}
	}
	var fs []float64
	if json.Unmarshal(b, &fs) == nil {
		return fs
	}
	return nil
}

// UnmarshalJSON reads a property.
func (v *Value) UnmarshalJSON(b []byte) error {
	var r rawValue
	if err := json.Unmarshal(b, &r); err != nil {
		// A bare number or array.
		v.static, v.set = numbers(b), true
		return nil
	}
	v.set = true
	if r.S && r.X != nil && r.Y != nil {
		var x, y Value
		bx, _ := json.Marshal(r.X)
		by, _ := json.Marshal(r.Y)
		x.UnmarshalJSON(bx)
		y.UnmarshalJSON(by)
		v.split = &[2]Value{x, y}
		return nil
	}
	if s := numbers(r.K); s != nil {
		v.static = s
		return nil
	}
	var keys []rawKey
	if err := json.Unmarshal(r.K, &keys); err != nil {
		return nil
	}
	for _, k := range keys {
		kf := keyframe{t: k.T, s: numbers(k.S), e: numbers(k.E), hold: k.H == 1, to: k.To, ti: k.Ti}
		if k.I != nil && k.O != nil {
			ix, iy, ox, oy := numbers(k.I.X), numbers(k.I.Y), numbers(k.O.X), numbers(k.O.Y)
			n := max(len(ix), len(iy), len(ox), len(oy))
			for d := range n {
				kf.ease = append(kf.ease, easing{pick(ox, d), pick(oy, d), pick(ix, d), pick(iy, d)})
			}
		}
		v.keys = append(v.keys, kf)
	}
	v.prepare()
	return nil
}

func pick(v []float64, i int) float64 {
	switch {
	case len(v) == 0:
		return 0
	case i < len(v):
		return v[i]
	}
	return v[len(v)-1]
}

// prepare fills in what each keyframe leaves to the next: its end value,
// and a position's curve.
func (v *Value) prepare() {
	for i := range v.keys {
		k := &v.keys[i]
		if k.e == nil && i+1 < len(v.keys) {
			k.e = v.keys[i+1].s
		}
		if k.s == nil && i > 0 {
			k.s = v.keys[i-1].e
		}
		if len(k.to) >= 2 && len(k.ti) >= 2 && len(k.s) >= 2 && len(k.e) >= 2 && (k.to[0] != 0 || k.to[1] != 0 || k.ti[0] != 0 || k.ti[1] != 0) {
			p0, p3 := Vec{k.s[0], k.s[1]}, Vec{k.e[0], k.e[1]}
			k.path = newSpatial(Cubic{p0, p0.Add(Vec{k.to[0], k.to[1]}), p3.Add(Vec{k.ti[0], k.ti[1]}), p3})
		}
	}
}

// At is the property at frame t, into out (its length, the value's
// dimensions).
func (v *Value) At(t float64, out []float64) []float64 {
	out = out[:0]
	if v.split != nil {
		x := v.split[0].Scalar(t, 0)
		y := v.split[1].Scalar(t, 0)
		return append(out, x, y)
	}
	if v.keys == nil {
		return append(out, v.static...)
	}
	ks := v.keys
	if t <= ks[0].t || len(ks) == 1 {
		return append(out, ks[0].s...)
	}
	i := sort.Search(len(ks), func(i int) bool { return ks[i].t > t }) - 1
	if i >= len(ks)-1 {
		last := ks[len(ks)-1]
		if last.s != nil {
			return append(out, last.s...)
		}
		return append(out, ks[len(ks)-2].e...)
	}
	k, next := &ks[i], &ks[i+1]
	if k.hold || k.e == nil {
		return append(out, k.s...)
	}
	span := next.t - k.t
	x := 0.0
	if span > 0 {
		x = (t - k.t) / span
	}
	if k.path != nil {
		p := k.path.at(k.easeAt(0).at(x))
		return append(out, p.X, p.Y)
	}
	for d := range k.s {
		e := k.easeAt(d).at(x)
		out = append(out, k.s[d]+(pick(k.e, d)-k.s[d])*e)
	}
	return out
}

func (k *keyframe) easeAt(d int) easing {
	switch {
	case len(k.ease) == 0:
		return linearEasing
	case d < len(k.ease):
		return k.ease[d]
	}
	return k.ease[len(k.ease)-1]
}

// Scalar is a one-number property at t (def if absent).
func (v *Value) Scalar(t, def float64) float64 {
	if !v.set {
		return def
	}
	var buf [4]float64
	o := v.At(t, buf[:0])
	if len(o) == 0 {
		return def
	}
	return o[0]
}

// Vec is a two-number property at t (def if absent).
func (v *Value) Vec(t float64, def Vec) Vec {
	if !v.set {
		return def
	}
	var buf [4]float64
	o := v.At(t, buf[:0])
	switch len(o) {
	case 0:
		return def
	case 1:
		return Vec{o[0], o[0]}
	}
	return Vec{o[0], o[1]}
}

// animated reports whether v changes over time.
func (v *Value) animated() bool {
	return len(v.keys) > 1 || v.split != nil && (v.split[0].animated() || v.split[1].animated())
}

// ShapeValue is an animated path.
type ShapeValue struct {
	static shapeData
	keys   []shapeKey
	set    bool
}

type shapeKey struct {
	t    float64
	s, e *shapeData
	hold bool
	ease easing
}

type rawShape struct {
	I [][]float64 `json:"i"`
	O [][]float64 `json:"o"`
	V [][]float64 `json:"v"`
	C bool        `json:"c"`
}

func (r *rawShape) data() *shapeData {
	d := &shapeData{Closed: r.C}
	vec := func(p []float64) Vec {
		if len(p) < 2 {
			return Vec{}
		}
		return Vec{p[0], p[1]}
	}
	for i, v := range r.V {
		d.V = append(d.V, vec(v))
		if i < len(r.I) {
			d.In = append(d.In, vec(r.I[i]))
		} else {
			d.In = append(d.In, Vec{})
		}
		if i < len(r.O) {
			d.Out = append(d.Out, vec(r.O[i]))
		} else {
			d.Out = append(d.Out, Vec{})
		}
	}
	return d
}

// shapes reads a keyframe's shape: an object, or an array of one.
func shapes(b json.RawMessage) *shapeData {
	if len(b) == 0 {
		return nil
	}
	var one rawShape
	if json.Unmarshal(b, &one) == nil {
		return one.data()
	}
	var many []rawShape
	if json.Unmarshal(b, &many) == nil && len(many) > 0 {
		return many[0].data()
	}
	return nil
}

func (v *ShapeValue) UnmarshalJSON(b []byte) error {
	var r rawValue
	if err := json.Unmarshal(b, &r); err != nil {
		return nil
	}
	v.set = true
	if d := shapes(r.K); d != nil && len(r.K) > 0 && r.K[0] == '{' {
		v.static = *d
		return nil
	}
	var keys []rawKey
	if err := json.Unmarshal(r.K, &keys); err != nil {
		return nil
	}
	for _, k := range keys {
		sk := shapeKey{t: k.T, s: shapes(k.S), e: shapes(k.E), hold: k.H == 1, ease: linearEasing}
		if k.I != nil && k.O != nil {
			sk.ease = easing{pick(numbers(k.O.X), 0), pick(numbers(k.O.Y), 0), pick(numbers(k.I.X), 0), pick(numbers(k.I.Y), 0)}
		}
		v.keys = append(v.keys, sk)
	}
	for i := range v.keys {
		if v.keys[i].e == nil && i+1 < len(v.keys) {
			v.keys[i].e = v.keys[i+1].s
		}
		if v.keys[i].s == nil && i > 0 {
			v.keys[i].s = v.keys[i-1].e
		}
	}
	return nil
}

// At is the path at t.
func (v *ShapeValue) At(t float64) shapeData {
	if v.keys == nil {
		return v.static
	}
	ks := v.keys
	first := ks[0]
	if t <= first.t || len(ks) == 1 {
		if first.s != nil {
			return *first.s
		}
		return shapeData{}
	}
	i := sort.Search(len(ks), func(i int) bool { return ks[i].t > t }) - 1
	if i >= len(ks)-1 {
		if l := ks[len(ks)-1]; l.s != nil {
			return *l.s
		}
		if e := ks[len(ks)-2].e; e != nil {
			return *e
		}
		return shapeData{}
	}
	k, next := ks[i], ks[i+1]
	if k.s == nil {
		return shapeData{}
	}
	if k.hold || k.e == nil {
		return *k.s
	}
	x := 0.0
	if next.t > k.t {
		x = (t - k.t) / (next.t - k.t)
	}
	f := k.ease.at(x)
	a, b := k.s, k.e
	n := min(len(a.V), len(b.V))
	out := shapeData{Closed: a.Closed, V: make([]Vec, n), In: make([]Vec, n), Out: make([]Vec, n)}
	for j := range n {
		out.V[j] = lerpVec(a.V[j], b.V[j], f)
		out.In[j] = lerpVec(at(a.In, j), at(b.In, j), f)
		out.Out[j] = lerpVec(at(a.Out, j), at(b.Out, j), f)
	}
	return out
}

package ui

import (
	"math"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// Node is what components return: a piece of the UI tree. The zero Node
// is nothing, and containers skip it.
type Node struct{ e *element }

type handlers struct {
	press, longPress    func()
	text                func(string)
	toggled             func(bool)
	value               func(float64)
	submit, focus, blur func()
	popped, selected    func(int64)
	action              func(int64)
	dismissed, refresh  func()
	pan, pinch          func(Gesture)
	swipe               func(SwipeDirection)
	loaded              func(w, h float32)
	failed              func(string)
	native              func([]byte)
	link                func(string)
	scrolled            func(x, y float32)
	navigated, message  func(string)
	ended               func()
}

// element is a node of the UI tree. Real elements have a native view;
// flattened containers only take part in layout; fragments (Show, For,
// Dynamic) have neither, their children splicing into their parent.
type element struct {
	r      *Root
	id     ops.NodeID
	kind   ops.Kind
	real   bool
	frag   bool
	cell   bool // a list cell: the host made its view
	inline bool // a RichText's inline component: a layout root of its own
	dead   bool
	parent *element
	kids   []*element
	box    *layout.Node

	nativeKids int        // native children inserted into this view
	sent       [4]float32 // the last SetFrame
	kidsDirty  bool
	frameQ     bool

	style    Style
	extraPad Edges // SafeArea's insets
	fill     bool  // the root's child: fills the screen
	// adjust is what the component sets on the layout style its Style
	// gives (a Scroll's overflow), kept whenever that's derived again.
	adjust  func(*layout.Style)
	h       *handlers
	list    listImpl
	text    string // Text's text or Button's title, for the measure cache
	variant int32

	// Host-sized layout roots (screens, tab pages, sheets) have their own
	// environment, which the host reports.
	env       *signal.Signal[host.Environment]
	hostSized bool
	measured  bool // the host measures its content
	laidSize  layout.Size
}

var (
	elems  = map[ops.NodeID]*element{}
	nextID ops.NodeID
	cur    *Root
)

func current() *Root {
	if cur == nil {
		panic("ui: components must be made inside Mount (or a Show, For or List row)")
	}
	return cur
}

// withRoot makes r the root new elements belong to while f runs.
func withRoot(r *Root, f func() Node) Node {
	prev := cur
	cur = r
	defer func() { cur = prev }()
	return f()
}

func newElement(kind ops.Kind, real bool) *element {
	r := current()
	e := &element{r: r, kind: kind, sent: [4]float32{nan32, nan32, nan32, nan32}}
	e.box = layout.New()
	e.box.Context = e
	if real {
		e.makeView()
	}
	return e
}

func newFragment() *element {
	return &element{r: current(), frag: true}
}

var nan32 = float32(math.NaN())

func (e *element) makeView() {
	e.real = true
	nextID++
	e.id = nextID
	elems[e.id] = e
	e.r.batch.Create(e.id, e.kind)
}

// remeasure marks the element's content as changed.
func (e *element) remeasure() {
	if e.box != nil {
		e.box.MarkDirty()
	}
}

// layoutParent is the element whose box holds e's box.
func (e *element) layoutParent() *element {
	p := e.parent
	for p != nil && p.frag {
		p = p.parent
	}
	return p
}

func (e *element) markKids() {
	l := e
	for l != nil && l.frag {
		l = l.parent
	}
	if l == nil || l.kidsDirty {
		return
	}
	l.kidsDirty = true
	l.r.kidsQ = append(l.r.kidsQ, l)
}

// relink rebuilds e's box's children from its element children,
// expanding fragments.
func (e *element) relink() {
	buf := e.r.boxBuf[:0]
	var collect func(k *element)
	collect = func(k *element) {
		if k.hostSized || k.inline {
			return // a layout root of its own
		}
		if k.frag {
			for _, c := range k.kids {
				collect(c)
			}
			return
		}
		buf = append(buf, k.box)
	}
	for _, k := range e.kids {
		collect(k)
	}
	e.box.SetChildren(buf)
	clear(buf)
	e.r.boxBuf = buf[:0]
}

// nativeParent is the nearest ancestor with a view.
func (e *element) nativeParent() *element {
	for p := e.parent; p != nil; p = p.parent {
		if p.real {
			return p
		}
	}
	return nil
}

// eachNative calls f for e if it has a view, else for the views of its
// descendants that have no view between them and e, in order.
func (e *element) eachNative(f func(*element)) {
	if e.real {
		f(e)
		return
	}
	for _, k := range e.kids {
		k.eachNative(f)
	}
}

// nativeIndex counts the views under p (not crossing into views) that
// come before target.
func nativeIndex(p, target *element) (int, bool) {
	n := 0
	var walk func(e *element) bool
	walk = func(e *element) bool {
		for _, k := range e.kids {
			if k == target {
				return true
			}
			if k.real {
				n++
				continue
			}
			if walk(k) {
				return true
			}
		}
		return false
	}
	return n, walk(p)
}

// attachNative inserts the views of e into its native parent.
func (e *element) attachNative() {
	p := e.nativeParent()
	if p == nil {
		return
	}
	var idx int
	if e.parent == p && len(p.kids) > 0 && p.kids[len(p.kids)-1] == e {
		idx = p.nativeKids
	} else {
		idx, _ = nativeIndex(p, e)
	}
	b := e.r.batch
	e.eachNative(func(d *element) {
		b.Insert(p.id, d.id, idx)
		idx++
		p.nativeKids++
		d.queueFrame()
	})
}

// detachNative removes the views of e from its native parent.
func (e *element) detachNative() {
	p := e.nativeParent()
	if p == nil {
		return
	}
	b := e.r.batch
	e.eachNative(func(d *element) {
		b.Remove(p.id, d.id)
		p.nativeKids--
	})
}

func (e *element) appendKid(k *element) {
	if k == nil {
		return
	}
	k.parent = e
	e.kids = append(e.kids, k)
	e.markKids()
	k.attachNative()
}

// removeKid takes k out of e; with destroy, its views are destroyed
// (they leave their parents with them).
func (e *element) removeKid(k *element, destroy bool) {
	if destroy {
		if p := k.nativeParent(); p != nil {
			k.eachNative(func(*element) { p.nativeKids-- })
		}
		k.destroy()
	} else {
		k.detachNative()
	}
	for i, x := range e.kids {
		if x == k {
			copy(e.kids[i:], e.kids[i+1:])
			e.kids[len(e.kids)-1] = nil
			e.kids = e.kids[:len(e.kids)-1]
			break
		}
	}
	k.parent = nil
	e.markKids()
}

// destroy destroys the views of e's subtree.
func (e *element) destroy() {
	if e.dead {
		return
	}
	e.dead = true
	if e.real && !e.cell {
		e.r.batch.Destroy(e.id)
		delete(elems, e.id)
	}
	for _, k := range e.kids {
		k.destroy()
	}
}

func (e *element) queueFrame() {
	if !e.frameQ {
		e.frameQ = true
		e.r.frameQ = append(e.r.frameQ, e)
	}
}

// emitFrame sends e's frame relative to its native parent, snapped to
// the pixel grid, if it changed.
func (e *element) emitFrame() {
	e.frameQ = false
	if e.dead || !e.real || e.box == nil || e.hostSized {
		return
	}
	x, y := e.box.X, e.box.Y
	for p := e.parent; p != nil && !p.real; p = p.parent {
		if p.box != nil {
			x += p.box.X
			y += p.box.Y
		}
	}
	s := e.r.scale
	sx, sy := snap(x, s), snap(y, s)
	f := [4]float32{sx, sy, snap(x+e.box.W, s) - sx, snap(y+e.box.H, s) - sy}
	if e.measured {
		// Snapping the edges can make content the host measured a pixel
		// smaller, and a label a pixel narrower wraps: round its size up.
		f[2] = max(f[2], ceilPx(e.box.W, s))
		f[3] = max(f[3], ceilPx(e.box.H, s))
	}
	if f != e.sent {
		e.sent = f
		e.r.batch.SetFrame(e.id, f[0], f[1], f[2], f[3])
	}
}

func snap(v, scale float32) float32 {
	return float32(math.Round(float64(v*scale))) / scale
}

// ceilPx rounds a size up to whole pixels, ignoring float noise.
func ceilPx(v, scale float32) float32 {
	return float32(math.Ceil(float64(v*scale)-1e-3)) / scale
}

// setStyle applies a static or dynamic style. text: the view draws text.
func (e *element) setStyle(st Styler, text bool) {
	if st == nil {
		st = Style{}
	}
	s, f := st.styler()
	if f == nil {
		e.style = s
		e.box.SetStyle(e.layoutStyle(&s))
		e.applyVisual(&s, nil, text)
		return
	}
	first := true
	signal.RenderEffect(func() {
		s := f()
		if first {
			first = false
			e.style = s
			e.box.SetStyle(e.layoutStyle(&s))
			if !e.real && e.flattenable() && s.visual() {
				e.makeReal()
			}
			e.applyVisual(&s, nil, text)
			return
		}
		prev := e.style
		e.style = s
		if layoutPart(s) != layoutPart(prev) {
			e.box.SetStyle(e.layoutStyle(&s))
		}
		if !e.real && s.visual() {
			e.makeReal()
			e.applyVisual(&s, nil, text)
			return
		}
		if e.applyVisual(&s, &prev, text) {
			e.remeasure()
		}
	})
}

func (e *element) layoutStyle(s *Style) layout.Style {
	l := s.layoutStyle(e.extraPad)
	if e.fill && s.Height.IsAuto() && s.Grow == 0 {
		l.FlexGrow = 1
	}
	if e.adjust != nil {
		e.adjust(&l)
	}
	// A view that grows into its parent's leftover space can be held to
	// its share: its automatic minimum sizes are 0, as React Native's
	// flex: 1 is (in CSS, min-height: 0), so that a growing view around a
	// Scroll scrolls rather than growing to the Scroll's content.
	if l.FlexGrow > 0 {
		if l.MinWidth.IsAuto() {
			l.MinWidth = layout.Pt(0)
		}
		if l.MinHeight.IsAuto() {
			l.MinHeight = layout.Pt(0)
		}
	}
	return l
}

// layoutPart is the part of s that layout reads.
func layoutPart(s Style) Style {
	s.Background, s.BorderColor, s.Color = 0, 0, 0
	s.CornerRadius, s.Opacity = 0, 0
	s.Hidden = false
	s.Font, s.TextAlign, s.Lines = Font{}, 0, 0
	s.Transform, s.Shadow, s.Gradient, s.Transition, s.Material = Transform{}, Shadow{}, nil, Transition{}, 0 // drawn, not laid out
	return s
}

// flattenable: a plain container with no events or accessibility props.
func (e *element) flattenable() bool {
	return e.kind == ops.KindView && e.h == nil && !e.frag && !e.cell
}

// makeReal gives a flattened container a view and moves its children's
// views into it.
func (e *element) makeReal() {
	p := e.nativeParent()
	if p != nil {
		e.real = false
		for _, k := range e.kids {
			k.detachNative()
		}
	}
	e.makeView()
	idx := 0
	for _, k := range e.kids {
		k.eachNative(func(d *element) {
			e.r.batch.Insert(e.id, d.id, idx)
			idx++
			d.queueFrame()
		})
	}
	e.nativeKids = idx
	if p != nil {
		e.attachNative()
	}
	e.queueFrame()
}

// envCtx passes the nearest environment (the root's, a screen's, a
// sheet's) down to components such as SafeArea.
var envCtx = signal.NewContext[*signal.Signal[host.Environment]](nil)

func envEqual(a, b host.Environment) bool { return a == b }

// newProvider makes a host-sized layout root with its own environment,
// starting from the root's until the host reports.
func newProvider(kind ops.Kind) *element {
	e := newElement(kind, true)
	e.hostSized = true
	e.env = signal.NewFunc(e.r.envV, envEqual)
	st := layout.DefaultStyle
	st.FlexDirection = layout.Column
	st.Width, st.Height = layout.Pct(100), layout.Pct(100)
	e.box.SetStyle(st)
	e.r.provs = append(e.r.provs, e)
	return e
}

// fill makes n fill its provider, as the root's child does.
func fill(n Node) {
	if n.e != nil {
		n.e.fill = true
		if !n.e.frag {
			n.e.box.SetStyle(n.e.layoutStyle(&n.e.style))
		}
	}
}

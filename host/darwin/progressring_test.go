//go:build darwin && !ios

package darwin

import (
	"fmt"
	"math"
	"testing"

	"github.com/ebitengine/purego"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// ringCase is a ProgressRing of a size, in the Style's Color or AppKit's,
// with a value or without.
type ringCase struct {
	side          float32
	color         bool
	indeterminate bool
}

func (c ringCase) String() string {
	return fmt.Sprintf("%v color=%v indeterminate=%v", c.side, c.color, c.indeterminate)
}

// native is whether AppKit's own indicator draws it: one of its sizes,
// in its colors.
func (c ringCase) native() bool { return !c.color && (c.side == 16 || c.side == 32) }

// A ProgressRing is AppKit's spinning NSProgressIndicator at its sizes
// (16 and 32 points) in its colors, and drawn as AppKit draws it at any
// other size or in the Style's Color: the circle's track and arc as wide
// as AppKit's stroke at that size, as large as the frame; the spinner's
// spokes. Either way the AsukaRing is what VoiceOver reads, as AppKit's
// indicator.
func TestProgressRing(t *testing.T) {
	var cases []ringCase
	for _, side := range []float32{16, 24, 32, 64} {
		for _, color := range []bool{false, true} {
			for _, ind := range []bool{false, true} {
				cases = append(cases, ringCase{side, color, ind})
			}
		}
	}
	done := signal.New(0.25)
	h, _ := mount(t, func() ui.Node {
		var rings []ui.Node
		for _, c := range cases {
			st := ui.Style{Width: ui.Pt(c.side)}
			if c.color {
				st.Color = colors.Pink
			}
			var v signal.Accessor[float64] = done
			if c.indeterminate {
				v = nil
			}
			rings = append(rings, ui.ProgressRing(v, st))
		}
		return ui.Column(ui.Style{Gap: 4, Padding: ui.All(12), Align: ui.AlignStart}, rings...)
	})
	onMain(func() {
		rs := ringsInOrder(h)
		if len(rs) != len(cases) {
			fatalf(t, "%d rings", len(rs))
		}
		for i, c := range cases {
			n := rs[i]
			checkRing(t, h, n, c, 0.25)
		}
		shot(h, "progress-ring")

		// A new value: AppKit's reads it at once without animating
		// (before it shows); the drawn arc ends there.
		h.enter(func() { ui.Post(func() { done.Set(2) }) })
		h.runPosted()
		spin(func() bool { return MsgFloat(rs[0].ring.native, Sel("doubleValue")) == 1 })
		for i, c := range cases {
			checkRing(t, h, rs[i], c, 1)
		}
	})
}

// ringsInOrder is the host's rings, top to bottom.
func ringsInOrder(h *Host) []*node {
	rs := find(h, ops.KindProgressRing)
	for i := range rs {
		for j := i + 1; j < len(rs); j++ {
			if rs[j].frame.Origin.Y < rs[i].frame.Origin.Y {
				rs[i], rs[j] = rs[j], rs[i]
			}
		}
	}
	return rs
}

// checkRing checks what n shows: AppKit's indicator or the drawn ring,
// its size, its value and what VoiceOver reads.
func checkRing(t *testing.T, h *Host, n *node, c ringCase, value float64) {
	t.Helper()
	r := n.ring
	if cls := GoString(Send(Send(n.view, "className"), "description")); cls != "AsukaRing" {
		fatalf(t, "%v: a %s", c, cls)
	}
	if s := n.frame.Size; s.Width != float64(c.side) || s.Height != float64(c.side) {
		fatalf(t, "%v: frame %+v", c, s)
	}
	if c.native() {
		if r.native == 0 || r.track != 0 || r.spinner != 0 {
			fatalf(t, "%v: not AppKit's indicator alone", c)
		}
		v := r.native
		cs := 0
		if c.side == 16 {
			cs = 1
		}
		if SendInt(v, "style") != 1 || SendInt(v, "controlSize") != cs {
			fatalf(t, "%v: style %d, control size %d", c, SendInt(v, "style"), SendInt(v, "controlSize"))
		}
		if f := MsgRect(v, Sel("frame")); f.Size.Width != float64(c.side) || f.Size.Height != float64(c.side) || f.Origin != (Point{}) {
			fatalf(t, "%v: AppKit's indicator at %+v", c, f)
		}
		if SendBool(v, "isIndeterminate") != c.indeterminate {
			fatalf(t, "%v: indeterminate %v", c, SendBool(v, "isIndeterminate"))
		}
		if !c.indeterminate && MsgFloat(v, Sel("doubleValue")) != value {
			fatalf(t, "%v: value %v", c, MsgFloat(v, Sel("doubleValue")))
		}
		if SendBool(v, "isAccessibilityElement") {
			fatalf(t, "%v: AppKit's indicator is read besides the ring", c)
		}
	} else {
		if r.native != 0 {
			fatalf(t, "%v: AppKit's indicator", c)
		}
		side := float64(c.side)
		if c.indeterminate {
			if r.spinner == 0 || r.track != 0 {
				fatalf(t, "%v: no spinner", c)
			}
			if SendInt(r.spinner, "instanceCount") != spinnerSpokes {
				fatalf(t, "%v: %d spokes", c, SendInt(r.spinner, "instanceCount"))
			}
			if f := MsgRect(r.spinner, Sel("frame")); f.Size.Width != side || f.Size.Height != side {
				fatalf(t, "%v: spinner %+v", c, f)
			}
			if w := MsgFloat(r.spoke, Sel("lineWidth")); w != side/8 {
				fatalf(t, "%v: spoke %v wide", c, w)
			}
			// The spoke reaches the edge, round end included.
			if b := pathBox(r.spoke); math.Abs(b.Origin.Y-side/8/2) > 1e-9 || b.Origin.X != side/2 {
				fatalf(t, "%v: spoke %+v", c, b)
			}
			if Send(r.spoke, "animationForKey:", uintptr(String("asuka.spin"))) == 0 {
				fatalf(t, "%v: the spinner doesn't turn", c)
			}
		} else {
			if r.track == 0 || r.arc == 0 || r.spinner != 0 {
				fatalf(t, "%v: no circle", c)
			}
			w := ringStroke(side)
			want := map[float32]float64{16: 5, 20: 5, 24: 5, 32: 5, 48: 7.5, 64: 10}[c.side]
			if w != want {
				fatalf(t, "%v: stroke %v, want %v", c, w, want)
			}
			for _, l := range []ID{r.track, r.arc} {
				if MsgFloat(l, Sel("lineWidth")) != w || GoString(Send(l, "lineCap")) != "round" {
					fatalf(t, "%v: stroke %v, cap %s", c, MsgFloat(l, Sel("lineWidth")), GoString(Send(l, "lineCap")))
				}
				// The stroke's outer edge is the frame's.
				b := pathBox(l)
				if math.Abs(b.Size.Width+w-side) > 1e-6 || math.Abs(b.Origin.X-w/2) > 1e-6 {
					fatalf(t, "%v: circle %+v", c, b)
				}
			}
			if e := MsgFloat(r.arc, Sel("strokeEnd")); e != value {
				fatalf(t, "%v: arc ends at %v", c, e)
			}
		}
		// Its colors: the Style's, or AppKit's as read from its indicator
		// (gray: the test's window isn't active).
		arc, track := r.arc, r.track
		if c.indeterminate {
			arc = r.spoke
		}
		got := cgRGBA(Send(arc, "strokeColor"))
		if c.color {
			if wantC := rgba(h.nsColor(colors.Pink)); got != wantC {
				fatalf(t, "%v: arc %v, want the Color's %v", c, got, wantC)
			}
		} else if !r.look.ok || got[0] != got[1] || got[1] != got[2] || got[3] == 0 {
			fatalf(t, "%v: arc %v, not AppKit's gray (read %v)", c, got, r.look.ok)
		}
		if track != 0 {
			if tc := cgRGBA(Send(track, "strokeColor")); tc[3] <= 0 || tc[3] > 0.2 {
				fatalf(t, "%v: track %v", c, tc)
			}
		}
	}
	checkRingA11y(t, n, c, value)
}

// checkRingA11y checks the ring reads as AppKit's own indicator does: the
// same role, description, value, minimum and maximum, and nothing else
// read in it.
func checkRingA11y(t *testing.T, n *node, c ringCase, value float64) {
	t.Helper()
	ref := newIndicator(c.indeterminate, value)
	defer Release(ref)
	for _, attr := range []string{"AXRole", "AXRoleDescription", "AXValue", "AXMinValue", "AXMaxValue"} {
		want, got := axString(ref, attr), axString(n.view, attr)
		if got != want {
			fatalf(t, "%v: %s %q, AppKit's %q", c, attr, got, want)
		}
	}
	if !SendBool(n.view, "isAccessibilityElement") {
		fatalf(t, "%v: not read", c)
	}
	if kids := Send(n.view, "accessibilityChildren"); kids != 0 && SendInt(kids, "count") != 0 {
		fatalf(t, "%v: %d elements in it", c, SendInt(kids, "count"))
	}
}

func axString(v ID, attr string) string {
	x := Send(v, "accessibilityAttributeValue:", uintptr(String(attr)))
	if x == 0 {
		return "nil"
	}
	return GoString(Send(x, "description"))
}

// pathBox is a shape layer's path's bounds.
func pathBox(l ID) Rect {
	loadRingCG()
	return cgPathBox(uintptr(Send(l, "path")))
}

var (
	cgPathBox   func(p uintptr) Rect
	cgComponent func(c uintptr) *[4]float64
	cgCount     func(c uintptr) uintptr
)

func loadRingCG() {
	if cgPathBox == nil {
		lib := CoreGraphics()
		purego.RegisterLibFunc(&cgPathBox, lib, "CGPathGetPathBoundingBox")
		purego.RegisterLibFunc(&cgComponent, lib, "CGColorGetComponents")
		purego.RegisterLibFunc(&cgCount, lib, "CGColorGetNumberOfComponents")
	}
}

// cgRGBA is a CGColor's components, red, green, blue and alpha (gray and
// alpha spread).
func cgRGBA(c ID) [4]float64 {
	loadRingCG()
	if c == 0 {
		return [4]float64{}
	}
	p := cgComponent(uintptr(c))
	if cgCount(uintptr(c)) == 2 {
		return [4]float64{p[0], p[0], p[0], p[1]}
	}
	return *p
}

// rgba is an NSColor's sRGB components, as cgRGBA reads its CGColor.
func rgba(c ID) [4]float64 { return cgRGBA(Send(c, "CGColor")) }

// The ring switches between AppKit's indicator and the drawn one as its
// size and its Color change, keeping its value and what VoiceOver reads.
func TestProgressRingSwitches(t *testing.T) {
	side := signal.New[float32](32)
	color := signal.New[ui.Color](0)
	done := signal.New(0.5)
	busy := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(12), Align: ui.AlignStart},
			ui.Show(signal.Func(func() bool { return !busy.Get() }), func() ui.Node {
				return ui.ProgressRing(done, ui.StyleFunc(func() ui.Style {
					return ui.Style{Width: ui.Pt(side.Get()), Color: color.Get()}
				}))
			}),
		)
	})
	set := func(f func()) {
		h.enter(func() { ui.Post(f) })
		h.runPosted()
	}
	onMain(func() {
		n := find(h, ops.KindProgressRing)[0]
		view := n.view
		step := func(s float32, colored bool, v float64) {
			t.Helper()
			if n.view != view {
				fatal(t, "the ring's view changed")
			}
			spin(func() bool { return n.ring.native == 0 || MsgFloat(n.ring.native, Sel("doubleValue")) == v })
			checkRing(t, h, n, ringCase{side: s, color: colored}, v)
		}
		step(32, false, 0.5)
		set(func() { side.Set(48) })
		step(48, false, 0.5)
		set(func() { side.Set(16) })
		step(16, false, 0.5)
		set(func() { color.Set(colors.Pink) })
		step(16, true, 0.5)
		set(func() { done.Set(0.75) })
		step(16, true, 0.75)
		set(func() { color.Set(0) })
		step(16, false, 0.75)
		set(func() { side.Set(32); done.Set(0.25) })
		step(32, false, 0.25)
		set(func() { side.Set(20) })
		step(20, false, 0.25)
		set(func() { side.Set(32) })
		step(32, false, 0.25)
		if n.ring.ref != 0 {
			fatal(t, "AppKit's unseen indicator outlived the drawn ring")
		}
	})
}

// A value change animates as AppKit's animator does, a quarter second,
// on the drawn arc as on AppKit's indicator; not with Reduce Motion.
func TestProgressRingAnimates(t *testing.T) {
	done := signal.New(0.25)
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Padding: ui.All(12), Gap: 8, Align: ui.AlignStart},
			ui.ProgressRing(done),
			ui.ProgressRing(done, ui.Style{Width: ui.Pt(48)}),
		)
	})
	onMain(func() {
		var native, drawn *node
		for _, n := range find(h, ops.KindProgressRing) {
			if n.ring.native != 0 {
				native = n
			} else {
				drawn = n
			}
		}
		if native == nil || drawn == nil {
			fatal(t, "not one of each")
		}
		if h.reduceMotion() {
			t.Skip("Reduce Motion is on")
		}
		h.enter(func() { ui.Post(func() { done.Set(0.75) }) })
		h.runPosted()
		a := Send(drawn.ring.arc, "animationForKey:", uintptr(String("strokeEnd")))
		if a == 0 || MsgFloat(a, Sel("duration")) != ringValueDuration {
			fatal(t, "the arc doesn't animate as AppKit's")
		}
		if MsgFloat(drawn.ring.arc, Sel("strokeEnd")) != 0.75 {
			fatal(t, "the arc doesn't end at the value")
		}
		if v := MsgFloat(native.ring.native, Sel("doubleValue")); v == 0.75 {
			fatal(t, "AppKit's indicator jumped")
		}
		if !spin(func() bool { return MsgFloat(native.ring.native, Sel("doubleValue")) == 0.75 }) {
			fatal(t, "AppKit's indicator doesn't get there")
		}
	})
}

// Disposing a ring lets go of what it drew and of AppKit's indicators.
func TestProgressRingDisposes(t *testing.T) {
	shown := signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(12)},
			ui.Show(shown, func() ui.Node {
				return ui.Row(ui.Style{Gap: 8},
					ui.ProgressRing(signal.New(0.5)),
					ui.ProgressRing(signal.New(0.5), ui.Style{Width: ui.Pt(48)}),
					ui.ProgressRing(nil, ui.Style{Width: ui.Pt(48)}),
				)
			}),
		)
	})
	onMain(func() {
		var rings []*progressRing
		for _, n := range find(h, ops.KindProgressRing) {
			rings = append(rings, n.ring)
		}
		if len(rings) != 3 {
			fatalf(t, "%d rings", len(rings))
		}
		h.enter(func() { ui.Post(func() { shown.Set(false) }) })
		h.runPosted()
		if len(find(h, ops.KindProgressRing)) != 0 {
			fatal(t, "rings left")
		}
		for _, r := range rings {
			if r.native != 0 || r.ref != 0 || r.track != 0 || r.arc != 0 || r.spinner != 0 || r.spoke != 0 {
				fatalf(t, "a ring holds on: %+v", r)
			}
		}
	})
}

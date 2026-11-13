//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A view that clips and casts a shadow clips its children in a view of
// their own, so its layer keeps its shadow; without the shadow they go
// back, and the view clips itself again.
func TestClipShadow(t *testing.T) {
	shadow, more := signal.New(true), signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			s := ui.Style{Width: ui.Pt(60), Height: ui.Pt(60), Clip: true, CornerRadius: 12}
			if shadow.Get() {
				s.Shadow = ui.Shadow{Color: ui.RGB(0, 0, 0), Radius: 8}
			}
			return s
		})},
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(100), Background: ui.RGB(255, 0, 0)}}),
			ui.Show(more, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(10), Height: ui.Pt(10), Background: ui.RGB(0, 0, 255)}})
			}),
		)
	})
	onMain(func() {
		var p *node
		for _, n := range find(h, ops.KindView) {
			if n.clip {
				p = n
			}
		}
		if p == nil {
			fatal(t, "no clipping view")
		}
		inClip := func() bool {
			for _, c := range p.children {
				if Send(c.view, "superview") != p.clipView {
					return false
				}
			}
			return true
		}
		l := Send(p.view, "layer")
		if p.clipView == 0 || !inClip() {
			fatal(t, "the children aren't in a clip view")
		}
		cl := Send(p.clipView, "layer")
		if SendBool(l, "masksToBounds") || !SendBool(cl, "masksToBounds") || MsgFloat(cl, Sel("cornerRadius")) != 12 {
			fatalf(t, "masks: the view's %v, the clip view's %v (radius %v)", SendBool(l, "masksToBounds"), SendBool(cl, "masksToBounds"), MsgFloat(cl, Sel("cornerRadius")))
		}
		if f := MsgRect(p.clipView, Sel("frame")); f.Size.Width != 60 || f.Size.Height != 60 {
			fatalf(t, "the clip view is %+v", f)
		}
		if Send(l, "shadowPath") == 0 {
			fatal(t, "no shadow path")
		}
		// A child added later goes in the clip view too.
		h.enter(func() { ui.Post(func() { more.Set(true) }) })
		h.runPosted()
		if len(p.children) != 2 || !inClip() {
			fatalf(t, "%d children, in the clip view: %v", len(p.children), inClip())
		}
		h.enter(func() { ui.Post(func() { shadow.Set(false) }) })
		h.runPosted()
		if p.clipView != 0 {
			fatal(t, "the clip view stayed without a shadow")
		}
		for _, c := range p.children {
			if Send(c.view, "superview") != p.view {
				fatal(t, "a child didn't come back")
			}
		}
		if !SendBool(l, "masksToBounds") {
			fatal(t, "the view doesn't clip itself again")
		}
	})
}

// AppKit sets a layer-backed view's layer's shadow from the view's own
// (none) as it moves into a window: Style.Shadow holds.
func TestShadowInWindow(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(60), Height: ui.Pt(30), Background: ui.RGB(255, 0, 0),
			Shadow: ui.Shadow{Color: ui.RGB(0, 0, 0), Radius: 8, Y: 4}}})
	})
	onMain(func() {
		var n *node
		for _, x := range find(h, ops.KindView) {
			if x.hasShadow() {
				n = x
			}
		}
		l := Send(n.view, "layer")
		opacity := func() float64 {
			return MsgFloat(Send(l, "valueForKey:", uintptr(String("shadowOpacity"))), Sel("doubleValue"))
		}
		if opacity() != 1 {
			fatalf(t, "shadow opacity %v once in the window", opacity())
		}
		sup := Send(n.view, "superview")
		Send(n.view, "removeFromSuperview")
		Send(sup, "addSubview:", uintptr(n.view))
		if o := opacity(); o != 1 || MsgSize(l, Sel("shadowOffset")).Height != 4 {
			fatalf(t, "shadow opacity %v, offset %+v after moving in again", o, MsgSize(l, Sel("shadowOffset")))
		}
	})
}

// Style.Transform holds on the layer (AppKit sets the layer's own
// transform), and ui.Animate animates it.
func TestTransformHolds(t *testing.T) {
	scale := signal.New(float32(0.5))
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Width: ui.Pt(60), Height: ui.Pt(60), Background: ui.RGB(255, 0, 0), Transform: ui.Transform{Scale: scale.Get()}}
		})})
	})
	onMain(func() {
		var n *node
		for _, x := range find(h, ops.KindView) {
			if x.transform != nil {
				n = x
			}
		}
		l := Send(n.view, "layer")
		Send(h.window, "display") // AppKit sets the layer's transform here
		a := Send(l, "animationForKey:", uintptr(transformKey()))
		if a == 0 || n.held.M11 != 0.5 || n.held.M41 != 15 {
			fatalf(t, "held %+v by %v", n.held, a)
		}
		h.enter(func() { ui.Post(func() { ui.Animate(func() { scale.Set(1) }) }) })
		h.runPosted()
		a = Send(l, "animationForKey:", uintptr(transformKey()))
		if d := MsgFloat(a, Sel("duration")); n.held.M11 != 1 || d < 0.2 {
			fatalf(t, "animated to %+v in %vs", n.held, d)
		}
	})
}

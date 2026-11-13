//go:build linux && !android

package linux

import (
	"testing"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/native"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// testScale is an app's native view: a GtkScale whose "value" prop it
// shows, and which emits "changed".
type testScale struct {
	ctx       native.Context
	scale     Ptr
	set       []string
	destroyed bool
}

func (s *testScale) Handle() uintptr { return s.scale }

func (s *testScale) Set(name string, v any) {
	s.set = append(s.set, name)
	if f, ok := v.(float64); ok && name == "value" {
		RangeSetValue(s.scale, f)
	}
}

func (s *testScale) Measure(w, h float32) (float32, float32) { return 160, 24 }
func (s *testScale) Destroy()                                { s.destroyed = true }

// A native view the app registers in Go: made by kind, its props set and
// followed, its events handled, measured by its Measure and filling its
// node's view; destroyed with the node.
func TestNativeView(t *testing.T) {
	var made *testScale
	native.RegisterView("TestScale", func(ctx native.Context) native.View {
		made = &testScale{ctx: ctx, scale: ScaleNewWithRange(OrientationHorizontal, 0, 1, 0.01)}
		return made
	})
	value := signal.New(0.25)
	shown := signal.New(true)
	var got []float64
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Show(shown, func() ui.Node {
				return ui.Native("TestScale", ui.NativeProps{
					Props: map[string]any{"value": signal.Accessor[float64](value), "label": "Volume"},
					On:    map[string]func(ui.NativeEvent){"changed": func(e ui.NativeEvent) { got = append(got, e.Float("value")) }},
				})
			}))
	})
	var n *node
	settle(t, h, func() bool {
		ns := find(h, ops.KindNative)
		if len(ns) == 1 && ns[0].placed {
			n = ns[0]
		}
		return n != nil
	})
	onMain(func() {
		if made == nil {
			fatalf(t, "the factory wasn't called")
		}
		if made.ctx.Parent() != n.widget {
			fatalf(t, "the context's parent isn't the node's view")
		}
		if n.frame[2] != 160 || n.frame[3] != 24 {
			fatalf(t, "the native view's frame %v, want 160x24", n.frame)
		}
		if n.inner != made.scale || WidgetGetParent(made.scale) != n.widget {
			fatalf(t, "the app's widget isn't in its node's view")
		}
		if v := RangeGetValue(made.scale); v != 0.25 {
			fatalf(t, "value %v", v)
		}
	})
	settle(t, h, func() bool {
		_, _, w, ht := allocation(made.scale)
		return w == 160 && ht == 24
	})
	ui.Post(func() { value.Set(0.75) })
	settle(t, h, func() bool { return RangeGetValue(made.scale) == 0.75 })
	onMain(func() {
		h.enter(func() { made.ctx.Emit("changed", map[string]any{"value": 0.5}) })
	})
	settle(t, h, func() bool { return len(got) == 1 && got[0] == 0.5 })
	ui.Post(func() { shown.Set(false) })
	settle(t, h, func() bool { return made.destroyed })
}

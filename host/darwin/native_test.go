//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// testSlider is an app's native view: an NSSlider whose "value" prop it
// shows, and which emits "changed".
type testSlider struct {
	ctx       native.Context
	slider    ID
	set       []string
	destroyed bool
}

func (s *testSlider) Handle() uintptr { return uintptr(s.slider) }

func (s *testSlider) Set(name string, v any) {
	s.set = append(s.set, name)
	if f, ok := v.(float64); ok && name == "value" {
		MsgSetFloat(s.slider, Sel("setDoubleValue:"), f)
	}
}

func (s *testSlider) Measure(w, h float32) (float32, float32) { return 160, 24 }
func (s *testSlider) Destroy()                                { s.destroyed = true }

// A native view the app registers in Go: made by kind, its props set and
// followed, its events handled, measured by its Measure and filling its
// node's view; destroyed with the node.
func TestNativeView(t *testing.T) {
	var made *testSlider
	native.RegisterView("TestSlider", func(ctx native.Context) native.View {
		made = &testSlider{ctx: ctx, slider: initView("NSSlider")}
		return made
	})
	value := signal.New(0.25)
	shown := signal.New(true)
	var got []float64
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Show(shown, func() ui.Node {
				return ui.Native("TestSlider", ui.NativeProps{
					Props: map[string]any{"value": signal.Accessor[float64](value), "label": "Volume"},
					On:    map[string]func(ui.NativeEvent){"changed": func(e ui.NativeEvent) { got = append(got, e.Float("value")) }},
				})
			}))
	})
	onMain(func() {
		if made == nil {
			fatal(t, "the factory wasn't called")
		}
		n := find(h, ops.KindNative)[0]
		if f := MsgRect(n.view, Sel("frame")); f.Size.Width != 160 || f.Size.Height != 24 {
			fatalf(t, "the native view's frame %+v, want 160x24", f.Size)
		}
		if Send(made.slider, "superview") != n.view || MsgRect(made.slider, Sel("frame")).Size.Width != 160 {
			fatal(t, "the app's view doesn't fill its node's")
		}
		if v := MsgFloat(made.slider, Sel("doubleValue")); v != 0.25 {
			fatalf(t, "value %v", v)
		}
		inCore(h, func() { value.Set(0.75) })
		if v := MsgFloat(made.slider, Sel("doubleValue")); v != 0.75 {
			fatalf(t, "value after a change %v", v)
		}
		h.enter(func() { made.ctx.Emit("changed", map[string]any{"value": 0.5}) })
		if len(got) != 1 || got[0] != 0.5 {
			fatalf(t, "events %v", got)
		}
		inCore(h, func() { shown.Set(false) })
		if !made.destroyed {
			fatal(t, "not destroyed with its node")
		}
	})
}

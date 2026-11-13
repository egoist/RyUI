//go:build darwin && !ios

package darwin

import (
	"fmt"
	"math"
	"os"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ops"
)

// ui.Native on a Mac: the app's Go registers its views with package
// native (an NSView it makes through purego). The node's view is a
// AsukaView holding the app's, which fills it as the core places it. Its
// props go to the view's Set, its events come back with the context's
// Emit, and it's measured by its Measure, or its fittingSize.

// builtinViews are the host's own native views (asuka/sdk/maps' map,
// asuka/sdk/camera's preview), by kind, each file adding its own (from its
// init). They win over the app's of the same kind.
var builtinViews = map[string]func(native.Context) native.View{}

type nativeView struct {
	kind string
	view native.View
}

// setNativeProp sets a Native node's Type (making the app's view) or one
// of its props.
func (h *Host) setNativeProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropNativeType:
		if n.native != nil {
			return
		}
		n.native = &nativeView{kind: v.Str}
		f := builtinViews[v.Str]
		if f == nil {
			f = native.ViewFactory(v.Str)
		}
		if f == nil {
			fmt.Fprintf(os.Stderr, "asuka: no native view %q is registered (package native)\n", v.Str)
			return
		}
		id := n.id
		nv := f(native.NewContext(uintptr(n.view), func(name string, values map[string]any) {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventNative, Bytes: native.EncodeEvent(name, values)})
			}
		}))
		if nv == nil {
			return
		}
		n.native.view = nv
		if v := ID(nv.Handle()); v != 0 {
			MsgSetRect(v, selSetFrame, MsgRect(n.view, Sel("bounds")))
			Send(v, "setAutoresizingMask:", 18) // width and height sizable
			Send(n.view, "addSubview:", uintptr(v))
		}
	case ops.PropNativeProp:
		if n.native == nil || n.native.view == nil {
			return
		}
		if name, val, ok := native.DecodeProp(v.Bytes); ok {
			n.native.view.Set(name, val)
		}
	}
}

// measureNative is a native view's size for the layout's offer.
func (h *Host) measureNative(n *node, w, ht float32) Size {
	if n.native == nil || n.native.view == nil {
		return Size{}
	}
	if m, ok := n.native.view.(native.Measurer); ok {
		mw, mh := m.Measure(w, ht)
		return Size{Width: float64(mw), Height: float64(mh)}
	}
	v := ID(n.native.view.Handle())
	if v == 0 {
		return Size{}
	}
	return MsgSize(v, Sel("fittingSize"))
}

// destroyNative lets the app's view go.
func (h *Host) destroyNative(n *node) {
	if n.native == nil || n.native.view == nil {
		return
	}
	if d, ok := n.native.view.(native.Destroyer); ok {
		d.Destroy()
	}
	n.native.view = nil
}

func nanIfUnbounded(f float32) float32 {
	if !finite(f) {
		return float32(math.NaN())
	}
	return f
}

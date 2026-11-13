//go:build linux && !android

package linux

import (
	"fmt"
	"math"
	"os"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ops"
)

// ui.Native on Linux: the app's Go registers its views with package
// native, each a GtkWidget it makes (through purego). The widget is the
// node's control: its view holds it and keeps it filling it as the core
// places it. Its props go to the view's Set, its events come back with
// the context's Emit, and it's measured by its Measure, or as GTK
// measures the widget.

type nativeView struct {
	kind string
	view native.View
}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindNative {
			return false
		}
		n.ext = &nativeView{} // the app's view comes with Type
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if n.kind != ops.KindNative {
			return false
		}
		h.setNativeProp(n, p, v)
		return true
	})
	measureHooks = append(measureHooks, func(h *Host, n *node, c layout.Constraints) (layout.Size, bool) {
		nv, ok := n.ext.(*nativeView)
		if !ok || nv.view == nil {
			return layout.Size{}, n.kind == ops.KindNative
		}
		m, ok := nv.view.(native.Measurer)
		if !ok {
			return layout.Size{}, false // the widget's own size
		}
		w, ht := c.Width, c.Height
		if w != w && finite(c.AvailW) {
			w = c.AvailW
		}
		mw, mh := m.Measure(nanIfUnbounded(w), nanIfUnbounded(ht))
		return layout.Size{W: mw, H: mh}, true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if nv, ok := n.ext.(*nativeView); ok && nv.view != nil {
			if d, ok := nv.view.(native.Destroyer); ok {
				d.Destroy()
			}
			nv.view = nil
		}
	})
}

// setNativeProp sets a Native node's Type (making the app's view) or one
// of its props.
func (h *Host) setNativeProp(n *node, p ops.PropID, v *ops.Value) {
	nv, _ := n.ext.(*nativeView)
	if nv == nil {
		return
	}
	switch p {
	case ops.PropNativeType:
		if nv.kind != "" {
			return
		}
		nv.kind = v.Str
		f := native.ViewFactory(v.Str)
		if f == nil {
			f = builtinViews[v.Str] // the host's own: asuka.camera, asuka.map...
		}
		if f == nil {
			fmt.Fprintf(os.Stderr, "asuka: no native view %q is registered (package native)\n", v.Str)
			return
		}
		id := n.id
		view := f(native.NewContext(n.widget, func(name string, values map[string]any) {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventNative, Bytes: native.EncodeEvent(name, values)})
			}
		}))
		if view == nil {
			return
		}
		nv.view = view
		if w := view.Handle(); w != 0 {
			if WidgetGetParent(w) == n.widget {
				n.inner = w // the app put it in its parent itself
				Call(FnQueueAllocate, n.widget)
			} else {
				h.detach(w)
				h.setInner(n, w) // the node's view takes it (and its floating reference)
			}
		}
	case ops.PropNativeProp:
		if nv.view == nil {
			return
		}
		if name, val, ok := native.DecodeProp(v.Bytes); ok {
			nv.view.Set(name, val)
		}
	}
}

func nanIfUnbounded(f float32) float32 {
	if !finite(f) {
		return float32(math.NaN())
	}
	return f
}

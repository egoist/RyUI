//go:build windows

package windows

import (
	"fmt"
	"math"
	"os"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/native"
	"github.com/egoist/ryui/ops"
)

// ui.Native on Windows: the app's Go registers its views with package
// native, each a child window it makes (in the context's Parent, the
// node's view). The view holds it and keeps it filling it (it's the
// node's control); the messages its controls send their parent go to the
// app's view if it's a native.ParentMessenger. Its props go to its Set,
// its events come back with the context's Emit, and it's measured by its
// Measure (nothing else measures a child window).

type nativeView struct {
	kind string
	view native.View
}

func (h *Host) setNativeProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropNativeType:
		if n.native != nil {
			return
		}
		n.native = &nativeView{kind: v.Str}
		f := builtinViews[v.Str] // the host's own (more.go)
		if f == nil {
			f = native.ViewFactory(v.Str)
		}
		if f == nil {
			fmt.Fprintf(os.Stderr, "ryui: no native view %q is registered (package native)\n", v.Str)
			return
		}
		id := n.id
		nv := f(native.NewContext(n.hwnd, func(name string, values map[string]any) {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventNative, Bytes: native.EncodeEvent(name, values)})
			}
		}))
		if nv == nil {
			return
		}
		n.native.view = nv
		if c := nv.Handle(); c != 0 {
			if Parent(c) != n.hwnd {
				SetParent(c, n.hwnd)
			}
			n.ctl = c
			EnableWindow(c, !n.disabled)
			h.layoutField(n)
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

// measureNative is a native view's size, in points, for the layout's
// offer.
func (h *Host) measureNative(n *node, w, ht float32) (float32, float32) {
	if n.native == nil || n.native.view == nil {
		return 0, 0
	}
	if m, ok := n.native.view.(native.Measurer); ok {
		return m.Measure(nanIfUnbounded(w), nanIfUnbounded(ht))
	}
	return 0, 0
}

func nanIfUnbounded(f float32) float32 {
	if f != f || math.IsInf(float64(f), 0) || f > 1e30 || f < -1e30 {
		return float32(math.NaN())
	}
	return f
}

// nativeMessage gives the app's view what its controls tell their parent.
func (h *Host) nativeMessage(n *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	if n.native == nil || n.native.view == nil {
		return 0, false
	}
	switch m {
	case WM_COMMAND, WM_NOTIFY, WM_HSCROLL, WM_VSCROLL, WM_CTLCOLOREDIT, WM_CTLCOLORSTATIC, WM_CTLCOLORBTN, WM_CTLCOLORLISTBOX:
		if pm, ok := n.native.view.(native.ParentMessenger); ok {
			return pm.ParentMessage(m, wp, lp)
		}
	}
	return 0, false
}

// destroyNative lets the app's view go (its window goes with the node's).
func (h *Host) destroyNative(n *node) {
	if n.native == nil || n.native.view == nil {
		return
	}
	if d, ok := n.native.view.(native.Destroyer); ok {
		d.Destroy()
	}
	n.native.view = nil
}

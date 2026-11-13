//go:build linux && !android

package gtk

import "unsafe"

// What host/linux's Selectable calls: the words of labels' layouts and
// the boxes of their selected ranges (it draws a selection across
// labels), the primary selection, GTK's own words for its menu.

var Sel struct {
	LayoutIndexToPos            func(l Ptr, index int32, r *PangoRectangle)
	LayoutGetLogAttrsReadonly   func(l Ptr, n *int32) Ptr
	DisplayGetPrimaryClipboard  func(d Ptr) Ptr
	EventControllerGetEventTime func(c Ptr) uint32
	Dgettext                    func(domain, msgid string) Ptr
}

func init() {
	bindings = append(bindings,
		binding{&Sel.LayoutIndexToPos, "pango_layout_index_to_pos"},
		binding{&Sel.LayoutGetLogAttrsReadonly, "pango_layout_get_log_attrs_readonly"},
		binding{&Sel.DisplayGetPrimaryClipboard, "gdk_display_get_primary_clipboard"},
		binding{&Sel.EventControllerGetEventTime, "gtk_event_controller_get_current_event_time"},
		binding{&Sel.Dgettext, "g_dgettext"},
	)
	raws = append(raws,
		struct {
			p    *uintptr
			name string
		}{&FnLayoutClipRegion, "gdk_pango_layout_get_clip_region"},
		struct {
			p    *uintptr
			name string
		}{&FnRegionNumRectangles, "cairo_region_num_rectangles"},
		struct {
			p    *uintptr
			name string
		}{&FnRegionGetRectangle, "cairo_region_get_rectangle"},
		struct {
			p    *uintptr
			name string
		}{&FnRegionDestroy, "cairo_region_destroy"},
	)
}

// Drawing a selection (raw: a draw's).
var FnLayoutClipRegion, FnRegionNumRectangles, FnRegionGetRectangle, FnRegionDestroy uintptr

// LayoutRangeBoxes calls f with the boxes (pixels, from the layout's
// origin) of a byte range of a layout's text, line by line.
func LayoutRangeBoxes(layout Ptr, start, end int32, f func(IntRect)) {
	if FnLayoutClipRegion == 0 || start >= end {
		return
	}
	ranges := [2]int32{start, end}
	reg := Call(FnLayoutClipRegion, layout, 0, 0, Addr(&ranges[0]), 1)
	if reg == 0 {
		return
	}
	n := int(int32(Call(FnRegionNumRectangles, reg)))
	for i := 0; i < n; i++ {
		var r IntRect
		Call(FnRegionGetRectangle, reg, uintptr(i), Addr(&r))
		f(r)
	}
	Call(FnRegionDestroy, reg)
}

// Translated is GTK's own translation of one of its strings (its menus'
// "_Copy"), in the user's language.
func Translated(msgid string) string {
	if Sel.Dgettext == nil {
		return msgid
	}
	return GoString(Sel.Dgettext("gtk40", msgid))
}

// Pango's PangoLogAttr bits (a guint of bit fields, one per character
// and one after the last).
const (
	LogAttrWhite     = 1 << 3
	LogAttrWordStart = 1 << 5
	LogAttrWordEnd   = 1 << 6
)

// LogAttrs are a layout's PangoLogAttrs as their bits, one per character
// and one after the last (Pango's memory: valid until the layout
// changes).
func LogAttrs(layout Ptr) []uint32 {
	if Sel.LayoutGetLogAttrsReadonly == nil {
		return nil
	}
	var n int32
	p := Sel.LayoutGetLogAttrsReadonly(layout, &n)
	if p == 0 || n <= 0 {
		return nil
	}
	return unsafe.Slice(At[uint32](p), int(n))
}

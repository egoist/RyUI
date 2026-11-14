//go:build linux && !android

package gtk

import (
	"github.com/ebitengine/purego"
)

// What host/linux's drawing and interaction call (shapes, canvases,
// shaders, page dots, paging, pull to refresh, drag and drop): in Draw,
// a namespace of their own, bound by Load like the rest. The Fn fields
// are raw pointers for Call, used per frame.
var Draw struct {
	FnSnapshotTransform          uintptr // gtk_snapshot_transform(snap, GskTransform*)
	FnSnapshotAppendScaledTexure uintptr // gtk_snapshot_append_scaled_texture(snap, texture, filter, rect*)
	FnPathUnref                  uintptr
	FnStrokeFree                 uintptr

	// Paths and strokes.
	PathGetBounds         func(p Ptr, r *Rect) bool
	PathIsEmpty           func(p Ptr) bool
	PathMeasureGetLength  func(m Ptr) float32
	PathMeasureGetPoint   func(m Ptr, distance float32, result Ptr) bool
	PathBuilderAddSegment func(b Ptr, p Ptr, start, end Ptr)
	StrokeSetMiterLimit   func(s Ptr, l float32)
	TransformScale        func(t Ptr, x, y float32) Ptr
	TransformRotate       func(t Ptr, degrees float32) Ptr

	// A canvas's affine transforms (through a graphene matrix), radial
	// gradients and gradient strokes.
	GrapheneMatrixAlloc          func() Ptr
	GrapheneMatrixInitFrom2D     func(m Ptr, xx, yx, xy, yy, x0, y0 float64) Ptr
	GrapheneMatrixFree           func(m Ptr)
	TransformMatrix              func(t, m Ptr) Ptr
	SnapshotAppendRadialGradient func(s Ptr, bounds *Rect, center *Point, hradius, vradius, start, end float32, stops *ColorStop, n uintptr)
	SnapshotPushStroke           func(s, path, stroke Ptr)

	// Textures.
	TextureDownload func(t Ptr, data *byte, stride uintptr)

	// Widgets.
	WidgetRemoveTickCallback func(w Ptr, id uint32)

	// Drag sources.
	DragSourceNew                 func() Ptr
	DragSourceSetActions          func(s Ptr, actions uint32)
	DragSourceSetIcon             func(s, paintable Ptr, x, y int32)
	ContentProviderNewForValue    func(v *Value) Ptr
	ContentProviderNewForBytes    func(mime string, bytes Ptr) Ptr
	ContentProviderNewUnion       func(providers *Ptr, n uintptr) Ptr
	ContentProviderRefFormats     func(p Ptr) Ptr
	ContentFormatsGetMIMETypes    func(f Ptr, n *uintptr) Ptr
	ContentFormatsContainGType    func(f Ptr, t GType) bool
	ContentFormatsContainMIMEType func(f Ptr, mime string) bool
	ContentFormatsUnref           func(f Ptr)
	FileListGetType               func() GType
	FileListNewFromArray          func(files *Ptr, n uintptr) Ptr
	FileListGetFiles              func(l Ptr) Ptr
	FileNewForPath                func(path string) Ptr
	FileGetPath                   func(f Ptr) Ptr
	FileGetURI                    func(f Ptr) Ptr
	SListFree                     func(l Ptr)
	ValueTakeBoxed                func(v *Value, p Ptr)
	DragGetSelectedAction         func(d Ptr) uint32
	PaintableGetCurrentImage      func(p Ptr) Ptr

	// Drop targets.
	DropTargetAsyncNew  func(formats Ptr, actions uint32) Ptr
	DropGetFormats      func(d Ptr) Ptr
	DropGetActions      func(d Ptr) uint32
	DropGetDrag         func(d Ptr) Ptr
	DropFinish          func(d Ptr, action uint32)
	DropReadValueAsync  func(d Ptr, t GType, priority int32, cancellable, cb, data Ptr)
	DropReadValueFinish func(d, res Ptr, err *Ptr) Ptr
}

// GDK's drag actions.
const (
	ActionCopy = 1 << 0
	ActionMove = 1 << 1
	ActionLink = 1 << 2
)

func init() {
	d := &Draw
	raw := func(p *uintptr, name string) struct {
		p    *uintptr
		name string
	} {
		return struct {
			p    *uintptr
			name string
		}{p, name}
	}
	raws = append(raws,
		raw(&d.FnSnapshotTransform, "gtk_snapshot_transform"),
		raw(&d.FnSnapshotAppendScaledTexure, "gtk_snapshot_append_scaled_texture"),
		raw(&d.FnPathUnref, "gsk_path_unref"),
		raw(&d.FnStrokeFree, "gsk_stroke_free"),
	)
	bindings = append(bindings,
		binding{&d.PathGetBounds, "gsk_path_get_bounds"},
		binding{&d.PathIsEmpty, "gsk_path_is_empty"},
		binding{&d.PathMeasureGetLength, "gsk_path_measure_get_length"},
		binding{&d.PathMeasureGetPoint, "gsk_path_measure_get_point"},
		binding{&d.PathBuilderAddSegment, "gsk_path_builder_add_segment"},
		binding{&d.StrokeSetMiterLimit, "gsk_stroke_set_miter_limit"},
		binding{&d.TransformScale, "gsk_transform_scale"},
		binding{&d.TransformRotate, "gsk_transform_rotate"},
		binding{&d.GrapheneMatrixAlloc, "graphene_matrix_alloc"},
		binding{&d.GrapheneMatrixInitFrom2D, "graphene_matrix_init_from_2d"},
		binding{&d.GrapheneMatrixFree, "graphene_matrix_free"},
		binding{&d.TransformMatrix, "gsk_transform_matrix"},
		binding{&d.SnapshotAppendRadialGradient, "gtk_snapshot_append_radial_gradient"},
		binding{&d.SnapshotPushStroke, "gtk_snapshot_push_stroke"},
		binding{&d.TextureDownload, "gdk_texture_download"},
		binding{&d.WidgetRemoveTickCallback, "gtk_widget_remove_tick_callback"},
		binding{&d.DragSourceNew, "gtk_drag_source_new"},
		binding{&d.DragSourceSetActions, "gtk_drag_source_set_actions"},
		binding{&d.DragSourceSetIcon, "gtk_drag_source_set_icon"},
		binding{&d.ContentProviderNewForValue, "gdk_content_provider_new_for_value"},
		binding{&d.ContentProviderNewForBytes, "gdk_content_provider_new_for_bytes"},
		binding{&d.ContentProviderNewUnion, "gdk_content_provider_new_union"},
		binding{&d.ContentProviderRefFormats, "gdk_content_provider_ref_formats"},
		binding{&d.ContentFormatsGetMIMETypes, "gdk_content_formats_get_mime_types"},
		binding{&d.ContentFormatsContainGType, "gdk_content_formats_contain_gtype"},
		binding{&d.ContentFormatsContainMIMEType, "gdk_content_formats_contain_mime_type"},
		binding{&d.ContentFormatsUnref, "gdk_content_formats_unref"},
		binding{&d.FileListGetType, "gdk_file_list_get_type"},
		binding{&d.FileListNewFromArray, "gdk_file_list_new_from_array"},
		binding{&d.FileListGetFiles, "gdk_file_list_get_files"},
		binding{&d.FileNewForPath, "g_file_new_for_path"},
		binding{&d.FileGetPath, "g_file_get_path"},
		binding{&d.FileGetURI, "g_file_get_uri"},
		binding{&d.SListFree, "g_slist_free"},
		binding{&d.ValueTakeBoxed, "g_value_take_boxed"},
		binding{&d.DragGetSelectedAction, "gdk_drag_get_selected_action"},
		binding{&d.PaintableGetCurrentImage, "gdk_paintable_get_current_image"},
		binding{&d.DropTargetAsyncNew, "gtk_drop_target_async_new"},
		binding{&d.DropGetFormats, "gdk_drop_get_formats"},
		binding{&d.DropGetActions, "gdk_drop_get_actions"},
		binding{&d.DropGetDrag, "gdk_drop_get_drag"},
		binding{&d.DropFinish, "gdk_drop_finish"},
		binding{&d.DropReadValueAsync, "gdk_drop_read_value_async"},
		binding{&d.DropReadValueFinish, "gdk_drop_read_value_finish"},
	)
}

// MIMETypes are a GdkContentFormats' MIME types.
func MIMETypes(formats Ptr) []string {
	if formats == 0 || Draw.ContentFormatsGetMIMETypes == nil {
		return nil
	}
	var n uintptr
	p := Draw.ContentFormatsGetMIMETypes(formats, &n)
	out := make([]string, 0, n)
	for i := uintptr(0); i < n && p != 0; i++ {
		out = append(out, GoString(*At[Ptr](p + i*8)))
	}
	return out
}

// SList is a GSList's data pointers.
func SList(l Ptr) []Ptr {
	var out []Ptr
	for ; l != 0; l = *At[Ptr](l + 8) { // GSList: data, next
		out = append(out, *At[Ptr](l))
	}
	return out
}

var cbShortcut uintptr

// ShortcutAction is a GtkCallbackAction running f, which reports whether
// it handled the shortcut (a GtkShortcutFunc).
func ShortcutAction(f func() bool) Ptr {
	if cbShortcut == 0 {
		cbShortcut = purego.NewCallback(func(widget, args, data uintptr) uintptr {
			if f, ok := handlers[data].(func() bool); ok {
				return Bool(f())
			}
			return 0
		})
	}
	id := register(f)
	return CallbackActionNew(cbShortcut, id, cbDestroy)
}

// PathPoint is GskPathPoint: opaque, 16-byte aligned.
type PathPoint struct{ b [80]byte }

// Ptr is the point's aligned address.
func (p *PathPoint) Ptr() Ptr {
	a := Addr(&p.b[0])
	return (a + 15) &^ 15
}

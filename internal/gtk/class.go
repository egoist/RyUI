//go:build linux && !android

package gtk

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

// GType is a GObject type.
type GType = uintptr

// Class offsets of GTK 4's (ABI-stable) class structs on 64-bit systems.
const (
	OffObjectDispose      = 40  // GObjectClass.dispose
	OffObjectFinalize     = 48  // GObjectClass.finalize
	OffWidgetSizeAllocate = 200 // GtkWidgetClass.size_allocate
	OffWidgetRequestMode  = 224 // GtkWidgetClass.get_request_mode
	OffWidgetMeasure      = 232 // GtkWidgetClass.measure
	OffWidgetGrabFocus    = 248 // GtkWidgetClass.grab_focus
	OffWidgetFocus        = 256 // GtkWidgetClass.focus
	OffWidgetSnapshot     = 320 // GtkWidgetClass.snapshot
	OffWidgetContains     = 328 // GtkWidgetClass.contains
)

// TypeQuery is GTypeQuery.
type TypeQuery struct {
	Type         GType
	Name         Ptr
	ClassSize    uint32
	InstanceSize uint32
}

// Subclass registers a type deriving from parent, with no fields of its
// own, whose class init sets vfuncs: init gets the class struct and the
// parent class (to chain up). Main thread, once per name.
func Subclass(parent GType, name string, init func(class, parentClass Ptr)) GType {
	var q TypeQuery
	Call(FnTypeQuery, parent, Addr(&q))
	cb := purego.NewCallback(func(class, data uintptr) uintptr {
		init(class, Call(FnTypeClassPeekParent, class))
		return 0
	})
	n := CString(name)
	return Call(FnTypeRegisterStaticSimple, parent, Addr(n), uintptr(q.ClassSize), cb, uintptr(q.InstanceSize), 0, 0)
}

// SetVfunc writes a function pointer into a class struct.
func SetVfunc(class Ptr, offset uintptr, f uintptr) {
	*At[uintptr](class + offset) = f
}

// Vfunc reads a function pointer from a class struct.
func Vfunc(class Ptr, offset uintptr) uintptr { return *At[uintptr](class + offset) }

// NewObject is g_object_new(t, NULL).
func NewObject(t GType) Ptr { return Call(FnObjectNew, t, 0) }

var _ = unsafe.Pointer(nil)

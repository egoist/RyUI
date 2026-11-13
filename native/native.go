// Package native is an app's own native code on the desktop hosts, which
// run the app's Go in their process: views shown with ui.Native and
// modules called with ui.Call, written in Go against the platform (AppKit
// through purego's objc package on macOS; Win32 through syscall on
// Windows; GTK 4 through purego on Linux). The mobile hosts take Swift
// and Kotlin instead (ios/*.swift, android/*.kt), the web JavaScript
// (web/*.js).
//
// Register in an init function of a file built for the platform:
//
//	//go:build darwin && !ios
//
//	func init() {
//		native.RegisterView("Stepper", func(ctx native.Context) native.View { return newStepper(ctx) })
//		native.RegisterModule("haptics", native.Module{"tap": tap})
//	}
//
// Everything here runs on the main thread, as the host does.
package native

import (
	"encoding/binary"
	"math"
	"sort"
	"sync"
)

// View is a native view a host shows for ui.Native.
type View interface {
	// Handle is the platform's view: an NSView (its Objective-C object) on
	// macOS, a child window (its HWND) on Windows, a GtkWidget on Linux.
	// The host puts it in a view of its own, which the core places, and
	// keeps it filling it.
	Handle() uintptr
	// Set sets one of the view's props (NativeProps.Props): a bool, an
	// int64, a float64 or a string. It's called once for each prop as the
	// view is made, and again as each changes.
	Set(name string, value any)
}

// Measurer is a View with a natural size. Width and height are what the
// layout offers, in points; NaN for either: as wide or tall as it likes.
// A View that isn't one is measured as the platform measures it (an
// NSView's fittingSize; a GtkWidget's natural size on Linux; nothing on
// Windows), or takes its size from its style.
type Measurer interface {
	Measure(width, height float32) (w, h float32)
}

// Destroyer is a View that frees what it holds when the core destroys
// it. The host destroys its Handle after.
type Destroyer interface {
	Destroy()
}

// ParentMessenger is a Windows View that hears the messages its window's
// controls send their parent (WM_COMMAND, WM_NOTIFY, WM_HSCROLL,
// WM_VSCROLL, WM_CTLCOLOR*), which go to the host's view it's in: a
// control's notifications. handled false lets the host answer.
type ParentMessenger interface {
	ParentMessage(msg uint32, wparam, lparam uintptr) (result uintptr, handled bool)
}

// Context is what a view gets as it's made.
type Context struct {
	parent uintptr
	emit   func(name string, values map[string]any)
}

// NewContext is for hosts: a context of the host's view the native view
// goes in, whose Emit sends the view's events.
func NewContext(parent uintptr, emit func(name string, values map[string]any)) Context {
	return Context{parent: parent, emit: emit}
}

// Parent is the host's view the native view goes in: an NSView on macOS,
// a window (HWND) on Windows, where a child window must have its parent
// as it's made (CreateWindowEx's hWndParent, with WS_CHILD), a GtkWidget
// on Linux.
func (c Context) Parent() uintptr { return c.parent }

// Emit sends an event to the view's handler of that name
// (NativeProps.On): its values are bools, ints, int64s, float32s,
// float64s or strings, read with NativeEvent's Bool, Int, Float and
// String.
func (c Context) Emit(name string, values map[string]any) {
	if c.emit != nil {
		c.emit(name, values)
	}
}

// Module is a native module's methods, by name: each gets the call's
// arguments (ui.Call's bytes) and answers once, now or later, on the main
// thread.
type Module map[string]func(args []byte, reply func(result []byte, err error))

var (
	mu      sync.Mutex
	views   = map[string]func(Context) View{}
	modules = map[string]Module{}
)

// RegisterView registers a native view kind: ui.Native(kind, ...) makes
// one with factory.
func RegisterView(kind string, factory func(Context) View) {
	mu.Lock()
	defer mu.Unlock()
	views[kind] = factory
}

// RegisterModule registers a native module: ui.Call(name, method, ...)
// runs its methods. The host's own modules of that name win.
func RegisterModule(name string, m Module) {
	mu.Lock()
	defer mu.Unlock()
	modules[name] = m
}

// ViewFactory is for hosts: the factory of a kind, or nil.
func ViewFactory(kind string) func(Context) View {
	mu.Lock()
	defer mu.Unlock()
	return views[kind]
}

// ModuleNamed is for hosts: the module of a name, or nil.
func ModuleNamed(name string) Module {
	mu.Lock()
	defer mu.Unlock()
	return modules[name]
}

// The wire format of ui.Native's props and events (ops' Native.Prop and
// the Native event): string name, u8 type, value.
const (
	typeBool   = 1
	typeInt    = 2
	typeFloat  = 3
	typeString = 4
)

// DecodeProp is for hosts: a Native.Prop's name and value.
func DecodeProp(b []byte) (name string, value any, ok bool) {
	r := reader{b: b}
	name = r.str()
	switch r.u8() {
	case typeBool:
		value = r.u8() != 0
	case typeInt:
		value = int64(r.u64())
	case typeFloat:
		value = math.Float64frombits(r.u64())
	case typeString:
		value = r.str()
	default:
		return "", nil, false
	}
	return name, value, !r.bad
}

// EncodeProp is for hosts: a prop in DecodeProp's format, for native
// code of another language (nil for a value of another type).
func EncodeProp(name string, value any) []byte {
	le := binary.LittleEndian
	b := append(le.AppendUint32(nil, uint32(len(name))), name...)
	switch v := value.(type) {
	case bool:
		t := byte(0)
		if v {
			t = 1
		}
		return append(b, typeBool, t)
	case int64:
		return le.AppendUint64(append(b, typeInt), uint64(v))
	case float64:
		return le.AppendUint64(append(b, typeFloat), math.Float64bits(v))
	case string:
		b = le.AppendUint32(append(b, typeString), uint32(len(v)))
		return append(b, v...)
	}
	return nil
}

// EncodeEvent is for hosts: a Native event's payload (its values in
// their keys' order).
func EncodeEvent(name string, values map[string]any) []byte {
	le := binary.LittleEndian
	str := func(b []byte, s string) []byte { return append(le.AppendUint32(b, uint32(len(s))), s...) }
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := str(nil, name)
	b = le.AppendUint32(b, 0)
	n := 0
	for _, k := range keys {
		var v []byte
		switch x := values[k].(type) {
		case bool:
			t := byte(0)
			if x {
				t = 1
			}
			v = []byte{typeBool, t}
		case int:
			v = le.AppendUint64([]byte{typeInt}, uint64(x))
		case int32:
			v = le.AppendUint64([]byte{typeInt}, uint64(x))
		case int64:
			v = le.AppendUint64([]byte{typeInt}, uint64(x))
		case float32:
			v = le.AppendUint64([]byte{typeFloat}, math.Float64bits(float64(x)))
		case float64:
			v = le.AppendUint64([]byte{typeFloat}, math.Float64bits(x))
		case string:
			v = str([]byte{typeString}, x)
		default:
			continue // not a value an event carries
		}
		b = append(str(b, k), v...)
		n++
	}
	le.PutUint32(b[4+len(name):], uint32(n))
	return b
}

// DecodeEvent is for hosts: a Native event's name and values, from its
// payload (EncodeEvent's format, which native code of another language
// writes too). ok is false for a malformed payload.
func DecodeEvent(b []byte) (name string, values map[string]any, ok bool) {
	r := reader{b: b}
	name = r.str()
	n := int(r.u32())
	values = make(map[string]any, min(n, 64))
	for i := 0; i < n && !r.bad; i++ {
		k := r.str()
		switch r.u8() {
		case typeBool:
			values[k] = r.u8() != 0
		case typeInt:
			values[k] = int64(r.u64())
		case typeFloat:
			values[k] = math.Float64frombits(r.u64())
		case typeString:
			values[k] = r.str()
		default:
			return "", nil, false
		}
	}
	if r.bad {
		return "", nil, false
	}
	return name, values, true
}

type reader struct {
	b   []byte
	bad bool
}

func (r *reader) take(n int) []byte {
	if r.bad || n < 0 || len(r.b) < n {
		r.bad = true
		return make([]byte, max(n, 0))
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() byte    { return r.take(1)[0] }
func (r *reader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *reader) u64() uint64 { return binary.LittleEndian.Uint64(r.take(8)) }
func (r *reader) str() string { return string(r.take(int(r.u32()))) }

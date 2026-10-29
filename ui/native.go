package ui

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// NativeProps configures a native view.
type NativeProps struct {
	Style Styler
	// Props are the view's named values: a bool, int, float64 or string,
	// or a signal.Accessor of one, which updates the view as it changes.
	Props map[string]any
	// On handles the view's events, by name.
	On map[string]func(NativeEvent)
}

// NativeEvent is an event of a native view: its name and named values.
type NativeEvent struct {
	Name   string
	values map[string]any
}

func (e NativeEvent) Bool(key string) bool         { v, _ := e.values[key].(bool); return v }
func (e NativeEvent) Int(key string) int64         { v, _ := e.values[key].(int64); return v }
func (e NativeEvent) Float(key string) float64     { v, _ := e.values[key].(float64); return v }
func (e NativeEvent) String(key string) string     { v, _ := e.values[key].(string); return v }
func (e NativeEvent) Has(key string) bool          { _, ok := e.values[key]; return ok }
func (e NativeEvent) Values() map[string]any       { return e.values }
func (e NativeEvent) GoString() string             { return fmt.Sprintf("%s%v", e.Name, e.values) }
func (e NativeEvent) Value(key string) (any, bool) { v, ok := e.values[key]; return v, ok }

// Native shows a view of a kind the app registers with each host: in
// Swift, NuiRuntime.registerNative; in Kotlin, NuiRuntime.registerNative
// (see the native example). The host measures it.
//
//	ui.Native("Segmented", ui.NativeProps{
//		Props: map[string]any{"items": "Day,Week,Month", "selected": sel},
//		On:    map[string]func(ui.NativeEvent){"changed": func(e ui.NativeEvent) { ... }},
//	})
func Native(kind string, p NativeProps) Node {
	e := newElement(ops.KindNative, true)
	id := e.id
	e.r.batch.SetString(id, ops.PropNativeType, kind)
	for name, v := range p.Props {
		bindNative(e, name, v)
	}
	if len(p.On) > 0 {
		on := p.On
		e.handlers().native = func(b []byte) {
			ev, err := decodeNativeEvent(b)
			if err != nil {
				return
			}
			if f := on[ev.Name]; f != nil {
				f(ev)
			}
		}
		e.listen(ops.EventNative)
	}
	e.measure()
	e.setStyle(p.Style, false)
	return Node{e}
}

func bindNative(e *element, name string, v any) {
	set := func(val any) {
		e.r.batch.SetBytes(e.id, ops.PropNativeProp, appendNativeValue(nil, name, val))
		e.remeasure()
	}
	switch a := v.(type) {
	case bool, int, int64, float64, float32, string:
		set(a)
	case signal.Accessor[bool]:
		bind(a, func(x bool) { set(x) })
	case signal.Accessor[int]:
		bind(a, func(x int) { set(x) })
	case signal.Accessor[int64]:
		bind(a, func(x int64) { set(x) })
	case signal.Accessor[float64]:
		bind(a, func(x float64) { set(x) })
	case signal.Accessor[string]:
		bind(a, func(x string) { set(x) })
	default:
		panic(fmt.Sprintf("ui.Native: prop %q is a %T; use a bool, int, float64 or string, or an accessor of one", name, v))
	}
}

// Value types on the wire.
const (
	nativeBool   = 1
	nativeInt    = 2
	nativeFloat  = 3
	nativeString = 4
)

func appendNativeString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

func appendNativeValue(b []byte, name string, v any) []byte {
	b = appendNativeString(b, name)
	switch x := v.(type) {
	case bool:
		t := byte(0)
		if x {
			t = 1
		}
		b = append(b, nativeBool, t)
	case int:
		b = append(b, nativeInt)
		b = binary.LittleEndian.AppendUint64(b, uint64(x))
	case int64:
		b = append(b, nativeInt)
		b = binary.LittleEndian.AppendUint64(b, uint64(x))
	case float32:
		b = append(b, nativeFloat)
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(float64(x)))
	case float64:
		b = append(b, nativeFloat)
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(x))
	case string:
		b = append(b, nativeString)
		b = appendNativeString(b, x)
	}
	return b
}

// encodeNativeEvent is what hosts send (for tests).
func encodeNativeEvent(name string, values map[string]any) []byte {
	b := appendNativeString(nil, name)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(values)))
	for k, v := range values {
		b = appendNativeValue(b, k, v)
	}
	return b
}

func decodeNativeEvent(b []byte) (NativeEvent, error) {
	r := nativeReader{b: b}
	ev := NativeEvent{Name: r.str()}
	n := int(r.u32())
	ev.values = make(map[string]any, n)
	for i := 0; i < n && r.err == nil; i++ {
		k := r.str()
		switch r.u8() {
		case nativeBool:
			ev.values[k] = r.u8() != 0
		case nativeInt:
			ev.values[k] = int64(r.u64())
		case nativeFloat:
			ev.values[k] = math.Float64frombits(r.u64())
		case nativeString:
			ev.values[k] = r.str()
		default:
			r.err = fmt.Errorf("ui: bad native value type")
		}
	}
	return ev, r.err
}

type nativeReader struct {
	b   []byte
	err error
}

func (r *nativeReader) take(n int) []byte {
	if r.err != nil || len(r.b) < n {
		r.err = fmt.Errorf("ui: short native event")
		return make([]byte, n)
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *nativeReader) u8() byte    { return r.take(1)[0] }
func (r *nativeReader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *nativeReader) u64() uint64 { return binary.LittleEndian.Uint64(r.take(8)) }
func (r *nativeReader) str() string { return string(r.take(int(r.u32()))) }

package fake

import (
	"encoding/binary"
	"math"

	"github.com/egoist/asuka/ops"
)

// Native views' values and events, on ui.Native's wire: a name (u32
// length, bytes), a type (1 bool, 2 int, 3 float, 4 string, 5 bytes) and
// the value.

// setNative keeps a PropNativeProp's value in v.Native.
func (v *View) setNative(b []byte) {
	name, b, ok := nativeString(b)
	if !ok || len(b) < 1 {
		return
	}
	t, b := b[0], b[1:]
	var val any
	switch t {
	case 1:
		if len(b) < 1 {
			return
		}
		val = b[0] != 0
	case 2, 3:
		if len(b) < 8 {
			return
		}
		u := binary.LittleEndian.Uint64(b)
		if t == 2 {
			val = int64(u)
		} else {
			val = math.Float64frombits(u)
		}
	case 4, 5:
		s, _, ok := nativeString(b)
		if !ok {
			return
		}
		if t == 4 {
			val = s
		} else {
			val = []byte(s)
		}
	default:
		return
	}
	if v.Native == nil {
		v.Native = map[string]any{}
	}
	v.Native[name] = val
}

func nativeString(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	if len(b) < 4+n {
		return "", nil, false
	}
	return string(b[4 : 4+n]), b[4+n:], true
}

// NativeType is a native view's registered kind ("" for another view).
func (v *View) NativeType() string { return v.String(ops.PropNativeType) }

// NativeString is a native view's string value named name ("" if none).
func (v *View) NativeString(name string) string { s, _ := v.Native[name].(string); return s }

// EmitNative sends a native view's event (its ui.NativeProps.On
// handler's), as its host's view does: values are bools, ints, int64s,
// float64s, strings or []bytes.
func (h *Host) EmitNative(v *View, name string, values map[string]any) {
	h.listening(v, ops.EventNative)
	le := binary.LittleEndian
	str := func(b []byte, s string) []byte { return append(le.AppendUint32(b, uint32(len(s))), s...) }
	b := str(nil, name)
	b = le.AppendUint32(b, uint32(len(values)))
	for k, x := range values {
		b = str(b, k)
		switch x := x.(type) {
		case bool:
			t := byte(0)
			if x {
				t = 1
			}
			b = append(b, 1, t)
		case int:
			b = le.AppendUint64(append(b, 2), uint64(x))
		case int64:
			b = le.AppendUint64(append(b, 2), uint64(x))
		case float64:
			b = le.AppendUint64(append(b, 3), math.Float64bits(x))
		case string:
			b = str(append(b, 4), x)
		case []byte:
			b = str(append(b, 5), string(x))
		default:
			panic("fake: EmitNative: a value of an unknown type")
		}
	}
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventNative, Bytes: b})
}

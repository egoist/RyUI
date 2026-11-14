//go:build linux && !android

package gtk

import (
	"runtime"
	"unsafe"
)

// Byte arrays ("ay") in GVariants, as []byte: VBytes makes a
// bytestring (its NUL kept), VariantGo a []any of bytes; BlueZ's values
// (host/linux/bluetooth*.go) are binary, and a notification's value
// comes often.

var fnVariantNewFixedArray, fnVariantGetFixedArray uintptr

// byteType is G_VARIANT_TYPE_BYTE: a GVariantType is its type string.
var byteType = [2]byte{'y', 0}

func fixedArrays() bool {
	if fnVariantNewFixedArray == 0 {
		fnVariantNewFixedArray = Sym("g_variant_new_fixed_array")
		fnVariantGetFixedArray = Sym("g_variant_get_fixed_array")
	}
	return fnVariantNewFixedArray != 0 && fnVariantGetFixedArray != 0
}

var fnVariantNewUint16 uintptr

// VU16 is a "q".
func VU16(n uint16) Ptr {
	if fnVariantNewUint16 == 0 {
		fnVariantNewUint16 = Sym("g_variant_new_uint16")
	}
	return Call(fnVariantNewUint16, uintptr(n))
}

// VByteArray is an "ay" of b (a floating reference, as the V* builders').
func VByteArray(b []byte) Ptr {
	if !fixedArrays() {
		return VArray("y")
	}
	var p Ptr
	if len(b) > 0 {
		p = Addr(&b[0])
	}
	v := Call(fnVariantNewFixedArray, Addr(&byteType[0]), p, uintptr(len(b)), 1)
	runtime.KeepAlive(b)
	return v
}

// VariantBytes is an "ay"'s bytes (a copy); nil for another type.
func VariantBytes(v Ptr) []byte {
	if v == 0 || !fixedArrays() || GoString(Sys.VariantGetTypeString(v)) != "ay" {
		return nil
	}
	var n uintptr
	p := Call(fnVariantGetFixedArray, v, Addr(&n), 1)
	if p == 0 || n == 0 {
		return []byte{}
	}
	return append([]byte(nil), unsafe.Slice(At[byte](p), n)...)
}

// VariantGoBytes is VariantGo with byte arrays as []byte.
func VariantGoBytes(v Ptr) any {
	if v == 0 {
		return nil
	}
	switch Sys.VariantClassify(v) {
	case 'v':
		c := Sys.VariantGetVariant(v)
		defer Sys.VariantUnref(c)
		return VariantGoBytes(c)
	case 'm':
		if Sys.VariantNChildren(v) == 0 {
			return nil
		}
		c := Sys.VariantGetChildValue(v, 0)
		defer Sys.VariantUnref(c)
		return VariantGoBytes(c)
	case 'a':
		ts := GoString(Sys.VariantGetTypeString(v))
		if ts == "ay" {
			return VariantBytes(v)
		}
		n := Sys.VariantNChildren(v)
		if len(ts) > 2 && ts[1] == '{' {
			m := make(map[string]any, n)
			for i := uintptr(0); i < n; i++ {
				e := Sys.VariantGetChildValue(v, i)
				k, val := Sys.VariantGetChildValue(e, 0), Sys.VariantGetChildValue(e, 1)
				key := VariantGo(k)
				ks, ok := key.(string)
				if !ok {
					ks = modFmt(key)
				}
				m[ks] = VariantGoBytes(val)
				Sys.VariantUnref(k)
				Sys.VariantUnref(val)
				Sys.VariantUnref(e)
			}
			return m
		}
		out := make([]any, n)
		for i := uintptr(0); i < n; i++ {
			c := Sys.VariantGetChildValue(v, i)
			out[i] = VariantGoBytes(c)
			Sys.VariantUnref(c)
		}
		return out
	case '(', '{':
		n := Sys.VariantNChildren(v)
		out := make([]any, n)
		for i := uintptr(0); i < n; i++ {
			c := Sys.VariantGetChildValue(v, i)
			out[i] = VariantGoBytes(c)
			Sys.VariantUnref(c)
		}
		return out
	}
	return VariantGo(v)
}

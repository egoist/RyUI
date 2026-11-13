//go:build windows

package windows

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// WinRT, as far as the host needs it (the share UI, toasts, location):
// HSTRINGs, activation factories, IIDs of parameterized interfaces, and
// delegates whose Invoke calls a Go function. WinRT objects are COM
// objects whose interfaces start with IInspectable's three methods
// (indexes 3 to 5), so an interface's own methods start at 6.

var (
	combase                   = systemDLL("combase.dll")
	procWindowsCreateString   = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString   = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRaw   = combase.NewProc("WindowsGetStringRawBuffer")
	procRoGetActivationFactor = combase.NewProc("RoGetActivationFactory")
	procRoActivateInstance    = combase.NewProc("RoActivateInstance")
)

// HString is a WinRT string, which the caller deletes (Delete).
type HString uintptr

// NewHString makes an HSTRING of s.
func NewHString(s string) HString {
	u, _ := syscall.UTF16FromString(s)
	var h uintptr
	if len(u) > 1 {
		procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	}
	return HString(h)
}

// Delete lets the string go.
func (h HString) Delete() {
	if h != 0 {
		procWindowsDeleteString.Call(uintptr(h))
	}
}

// String reads an HSTRING.
func (h HString) String() string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := procWindowsGetStringRaw.Call(uintptr(h), uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(native(p)), n))
}

// WinRTAvailable is whether this Windows has WinRT (8 and later).
func WinRTAvailable() bool { return Has(procRoGetActivationFactor) }

// ActivationFactory is runtime class class's factory, as iid (a
// reference the caller releases).
func ActivationFactory(class string, iid GUID) (uintptr, error) {
	if !WinRTAvailable() {
		return 0, errors.New("this Windows has no WinRT")
	}
	hs := NewHString(class)
	defer hs.Delete()
	var f uintptr
	if hr, _, _ := procRoGetActivationFactor.Call(uintptr(hs), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&f))); Failed(hr) || f == 0 {
		return 0, fmt.Errorf("%s: %#x", class, uint32(hr))
	}
	return f, nil
}

// ActivateInstance makes an instance of runtime class class with its
// default constructor: its IInspectable, which the caller releases.
func ActivateInstance(class string) (uintptr, error) {
	if !WinRTAvailable() {
		return 0, errors.New("this Windows has no WinRT")
	}
	hs := NewHString(class)
	defer hs.Delete()
	var obj uintptr
	if hr, _, _ := procRoActivateInstance.Call(uintptr(hs), uintptr(unsafe.Pointer(&obj))); Failed(hr) || obj == 0 {
		return 0, fmt.Errorf("%s: %#x", class, uint32(hr))
	}
	return obj, nil
}

// ParamIID is the IID of a parameterized interface (a delegate such as
// TypedEventHandler<A, B>, an IAsyncOperation<T>), from its signature as
// WinRT's type system writes it: a version 5 UUID of the signature in
// WinRT's namespace.
func ParamIID(signature string) GUID {
	ns := []byte{0x11, 0xf4, 0x7a, 0xd5, 0x7b, 0x73, 0x42, 0xc0, 0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee}
	sum := sha1.Sum(append(ns, signature...))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	g := GUID{Data1: binary.BigEndian.Uint32(b[0:]), Data2: binary.BigEndian.Uint16(b[4:]), Data3: binary.BigEndian.Uint16(b[6:])}
	copy(g.Data4[:], b[8:])
	return g
}

// Signature is a GUID as WinRT signatures write it: lower case, in
// braces.
func (g GUID) Signature() string {
	return strings.ToLower(fmt.Sprintf("{%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x}",
		g.Data1, g.Data2, g.Data3, g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3], g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7]))
}

var (
	iidIAgile     = NewGUID("94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90")
	delegatesMu   sync.Mutex
	delegates     = map[uintptr]*delegate{}
	delegateVtbl  [4]uintptr
	delegateSetup sync.Once
)

// delegate is a WinRT delegate: IUnknown and Invoke(sender, args).
type delegate struct {
	vtbl *[4]uintptr
	refs int32
	iid  GUID
	fn   func(a, b uintptr)
}

func delegateRef(this uintptr, delta int32) int32 {
	delegatesMu.Lock()
	defer delegatesMu.Unlock()
	d := delegates[this]
	if d == nil {
		return 0
	}
	d.refs += delta
	if d.refs <= 0 {
		delete(delegates, this)
		return 0
	}
	return d.refs
}

// NewDelegate makes a delegate of interface iid holding one reference,
// which the caller releases once it has handed it on. Invoke calls fn
// with its two arguments (an event's sender and arguments, an async
// operation and its status).
func NewDelegate(iid GUID, fn func(a, b uintptr)) uintptr {
	delegateSetup.Do(func() {
		delegateVtbl = [4]uintptr{
			syscall.NewCallback(func(this, riid, out uintptr) uintptr {
				g := *(*GUID)(native(riid))
				delegatesMu.Lock()
				d := delegates[this]
				delegatesMu.Unlock()
				if d == nil || g != d.iid && g != iidIUnknown && g != iidIAgile {
					*(*uintptr)(native(out)) = 0
					return 0x80004002 // E_NOINTERFACE
				}
				*(*uintptr)(native(out)) = this
				delegateRef(this, 1)
				return 0
			}),
			syscall.NewCallback(func(this uintptr) uintptr { return uintptr(delegateRef(this, 1)) }),
			syscall.NewCallback(func(this uintptr) uintptr { return uintptr(delegateRef(this, -1)) }),
			syscall.NewCallback(func(this, a, b uintptr) uintptr {
				delegatesMu.Lock()
				d := delegates[this]
				delegatesMu.Unlock()
				if d != nil {
					d.fn(a, b)
				}
				return 0
			}),
		}
	})
	d := &delegate{vtbl: &delegateVtbl, refs: 1, iid: iid, fn: fn}
	p := uintptr(unsafe.Pointer(d))
	delegatesMu.Lock()
	delegates[p] = d
	delegatesMu.Unlock()
	return p
}

// RuntimeClassSig is a runtime class's signature, for ParamIID:
// rc(name;{default interface}).
func RuntimeClassSig(name string, def GUID) string {
	return "rc(" + name + ";" + def.Signature() + ")"
}

var iidIInspectable = NewGUID("AF86E2E0-B12D-4C6A-9C5A-D7AA65101E90")

// An object is a WinRT object of Go's: IInspectable and its methods,
// whose callbacks are made once per vtable layout.
type object struct {
	vtbl *[16]uintptr
	refs int32
	iids []GUID
	impl any
}

var (
	objectsMu sync.Mutex
	objects   = map[uintptr]*object{}
)

func objectOf(this uintptr) *object {
	objectsMu.Lock()
	defer objectsMu.Unlock()
	return objects[this]
}

func objectRef(this uintptr, delta int32) int32 {
	objectsMu.Lock()
	defer objectsMu.Unlock()
	o := objects[this]
	if o == nil {
		return 0
	}
	o.refs += delta
	if o.refs <= 0 {
		delete(objects, this)
		if r, ok := o.impl.(interface{ release() }); ok {
			defer r.release()
		}
		return 0
	}
	return o.refs
}

// inspectableVtbl is IUnknown's and IInspectable's methods, the first six
// of an object's vtable.
func inspectableVtbl() [6]uintptr {
	return [6]uintptr{
		syscall.NewCallback(func(this, riid, out uintptr) uintptr {
			g := *(*GUID)(native(riid))
			o := objectOf(this)
			ok := o != nil && (g == iidIUnknown || g == iidIInspectable || g == iidIAgile)
			for _, x := range o.iids {
				ok = ok || g == x
			}
			if !ok {
				*(*uintptr)(native(out)) = 0
				return 0x80004002 // E_NOINTERFACE
			}
			*(*uintptr)(native(out)) = this
			objectRef(this, 1)
			return 0
		}),
		syscall.NewCallback(func(this uintptr) uintptr { return uintptr(objectRef(this, 1)) }),
		syscall.NewCallback(func(this uintptr) uintptr { return uintptr(objectRef(this, -1)) }),
		syscall.NewCallback(func(this, count, iids uintptr) uintptr { // GetIids
			*(*uint32)(native(count)) = 0
			*(*uintptr)(native(iids)) = 0
			return 0
		}),
		syscall.NewCallback(func(this, name uintptr) uintptr { // GetRuntimeClassName
			*(*uintptr)(native(name)) = 0
			return 0
		}),
		syscall.NewCallback(func(this, level uintptr) uintptr { // GetTrustLevel
			*(*uint32)(native(level)) = 0
			return 0
		}),
	}
}

func newObject(vtbl *[16]uintptr, iid GUID, impl any) uintptr {
	o := &object{vtbl: vtbl, refs: 1, iids: []GUID{iid}, impl: impl}
	p := uintptr(unsafe.Pointer(o))
	objectsMu.Lock()
	objects[p] = o
	objectsMu.Unlock()
	return p
}

// items is an IIterable<IStorageItem> of storage items, and its
// iterators.
type items struct{ list []uintptr }

func (it *items) release() {
	for _, x := range it.list {
		Release(x)
	}
}

type itemsIterator struct {
	of *items
	at int
}

var (
	iidIterableItems = ParamIID("pinterface({faa585ea-6214-4217-afda-7f46de5869b3};" + iidStorageItem.Signature() + ")")
	iidIteratorItems = ParamIID("pinterface({6a79e863-4300-459a-9966-cbb660963ee1};" + iidStorageItem.Signature() + ")")
	iterableVtbl     [16]uintptr
	iteratorVtbl     [16]uintptr
	iterSetup        sync.Once
)

// newItems makes an IIterable<IStorageItem> of storage files (each's
// IStorageItem taken), holding one reference.
func newItems(files []uintptr) uintptr {
	iterSetup.Do(func() {
		base := inspectableVtbl()
		copy(iterableVtbl[:], base[:])
		iterableVtbl[6] = syscall.NewCallback(func(this, out uintptr) uintptr { // First
			o := objectOf(this)
			if o == nil {
				return 0x80004005
			}
			*(*uintptr)(native(out)) = newObject(&iteratorVtbl, iidIteratorItems, &itemsIterator{of: o.impl.(*items)})
			objectRef(this, 1) // the iterator holds the iterable
			return 0
		})
		copy(iteratorVtbl[:], base[:])
		cur := func(this uintptr) (*itemsIterator, bool) {
			o := objectOf(this)
			if o == nil {
				return nil, false
			}
			i := o.impl.(*itemsIterator)
			return i, i.at < len(i.of.list)
		}
		iteratorVtbl[6] = syscall.NewCallback(func(this, out uintptr) uintptr { // get_Current
			i, ok := cur(this)
			if !ok {
				return 0x8000000B // E_BOUNDS
			}
			x := i.of.list[i.at]
			AddRef(x)
			*(*uintptr)(native(out)) = x
			return 0
		})
		iteratorVtbl[7] = syscall.NewCallback(func(this, out uintptr) uintptr { // get_HasCurrent
			_, ok := cur(this)
			*(*uint8)(native(out)) = boolByte(ok)
			return 0
		})
		iteratorVtbl[8] = syscall.NewCallback(func(this, out uintptr) uintptr { // MoveNext
			i, ok := cur(this)
			if ok {
				i.at++
			}
			_, ok = cur(this)
			*(*uint8)(native(out)) = boolByte(ok)
			return 0
		})
		iteratorVtbl[9] = syscall.NewCallback(func(this, capacity, out, actual uintptr) uintptr { // GetMany
			i, _ := cur(this)
			n := 0
			for i != nil && n < int(capacity) && i.at < len(i.of.list) {
				x := i.of.list[i.at]
				AddRef(x)
				*(*uintptr)(unsafe.Add(native(out), n*int(unsafe.Sizeof(uintptr(0))))) = x
				i.at++
				n++
			}
			*(*uint32)(native(actual)) = uint32(n)
			return 0
		})
	})
	it := &items{}
	for _, f := range files {
		if x := QueryInterface(f, &iidStorageItem); x != 0 {
			it.list = append(it.list, x)
		}
	}
	return newObject(&iterableVtbl, iidIterableItems, it)
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

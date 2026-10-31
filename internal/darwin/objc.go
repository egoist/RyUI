//go:build darwin && !ios

// Package darwin drives the Objective-C runtime and AppKit from Go with
// purego, no cgo: the toolkit host/darwin is written in. Ported from
// MyGo's internal/darwin.
//
// Messages whose arguments are all integers or pointers go through Send
// (purego.SyscallN, no reflection). Those with float or struct arguments
// or results use the typed Msg* functions, which purego builds once.
package darwin

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// ID is an Objective-C object.
type ID = objc.ID

// SEL is a selector.
type SEL = objc.SEL

// Point, Size and Rect are CoreGraphics' structs (64-bit).
type Point struct{ X, Y float64 }
type Size struct{ Width, Height float64 }
type Rect struct {
	Origin Point
	Size   Size
}

// AffineTransform is CGAffineTransform.
type AffineTransform struct{ A, B, C, D, Tx, Ty float64 }

// Range is NSRange.
type Range struct{ Location, Length uint }

// EdgeInsets is NSEdgeInsets.
type EdgeInsets struct{ Top, Left, Bottom, Right float64 }

var (
	msgSendAddr      uintptr
	msgSendSuperAddr uintptr

	// Typed variants of objc_msgSend, for float and struct arguments.
	MsgRect         func(obj ID, sel SEL) Rect
	MsgSetRect      func(obj ID, sel SEL, r Rect)
	MsgSetRectBool  func(obj ID, sel SEL, r Rect, b bool)
	MsgRectToRect   func(obj ID, sel SEL, r Rect) Rect
	MsgRectToSize   func(obj ID, sel SEL, r Rect) Size
	MsgRectIDToRect func(obj ID, sel SEL, r Rect, a ID) Rect
	MsgFloat        func(obj ID, sel SEL) float64
	MsgSetFloat     func(obj ID, sel SEL, v float64)
	MsgSize         func(obj ID, sel SEL) Size
	MsgSetSize      func(obj ID, sel SEL, s Size)
	MsgSizeToSize   func(obj ID, sel SEL, s Size) Size
	MsgSizeUintRect func(obj ID, sel SEL, s Size, opts uint, ctx ID) Rect
	MsgPoint        func(obj ID, sel SEL) Point
	MsgSetPoint     func(obj ID, sel SEL, p Point)
	MsgPointID      func(obj ID, sel SEL, p Point) ID
	MsgPointFromID  func(obj ID, sel SEL, p Point, view ID) Point
	MsgIDToPoint    func(obj ID, sel SEL, a ID) Point
	MsgIDToSize     func(obj ID, sel SEL, a ID) Size
	MsgIDToRect     func(obj ID, sel SEL, a ID) Rect
	MsgFloatFromID  func(obj ID, sel SEL, a ID) float64
	MsgPointFromInt func(obj ID, sel SEL, i int) Point
	MsgLineFragment func(obj ID, sel SEL, i int, r uintptr) Rect
	MsgGlyphAtPoint func(obj ID, sel SEL, p Point, c ID, frac *float64) int
	msgSuperPoint   func(sup ID, sel SEL, p Point) ID
	MsgRangeToRect  func(obj ID, sel SEL, loc, length uint, container ID) Rect
	MsgRangeRange   func(obj ID, sel SEL, loc, length uint, actual uintptr) Range
	MsgInitRect     func(obj ID, sel SEL, r Rect) ID
	MsgInitRectID   func(obj ID, sel SEL, r Rect, a ID) ID
	MsgRectToID     func(obj ID, sel SEL, r Rect) ID
	MsgRectIDVoid   func(obj ID, sel SEL, r Rect, a ID)
	MsgInitWindow   func(obj ID, sel SEL, r Rect, style uint, backing uint, deferFlag bool) ID
	MsgColor        func(cls ID, sel SEL, r, g, b, a float64) ID
	MsgFloatID      func(obj ID, sel SEL, v float64) ID
	MsgFloat2ID     func(obj ID, sel SEL, a, b float64) ID
	MsgIDFloatID    func(obj ID, sel SEL, a ID, v float64) ID
	MsgFloatIDID    func(obj ID, sel SEL, v float64, a ID) ID
	MsgSetFloatInt  func(obj ID, sel SEL, v float64, i int)
	MsgFloatInt     func(obj ID, sel SEL, i int) float64
	MsgSetEdges     func(obj ID, sel SEL, e EdgeInsets)
	MsgSetTransform func(obj ID, sel SEL, t AffineTransform)
	MsgOtherEvent   func(cls ID, sel SEL, typ uint, loc Point, flags uint, ts float64, wn int, ctx ID, subtype int16, d1, d2 int) ID
	MsgPopUpMenu    func(menu ID, sel SEL, item ID, loc Point, view ID) bool
	MsgDateSince    func(cls ID, sel SEL, seconds float64) ID
	MsgTimer        func(cls ID, sel SEL, interval float64, repeats bool, block ID) ID
	MsgDrawRect     func(obj ID, sel SEL, r Rect, op uint, fraction float64)
	MsgDrawAt       func(obj ID, sel SEL, p Point)
	MsgDrawInRect   func(obj ID, sel SEL, r Rect, attrs ID)
	MsgScrollPoint  func(obj ID, sel SEL, p Point)
	MsgRectRowCol   func(obj ID, sel SEL, row int) Rect

	poolPushFn, poolPopFn, mainNPFn uintptr

	libObjC, libFoundation, libCF, libCG, libAppKit uintptr
	loadOnce                                        sync.Once
)

func mustDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Sprintf("ryui: cannot load %s: %v", path, err))
	}
	return h
}

func mustDlsym(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(fmt.Sprintf("ryui: %v", err))
	}
	return p
}

// Dlopen opens a framework (optional ones: 0 when it's missing).
func Dlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return 0
	}
	return h
}

// Libraries, for RegisterLibFunc.
func AppKit() uintptr         { return libAppKit }
func CoreGraphics() uintptr   { return libCG }
func CoreFoundation() uintptr { return libCF }
func Foundation() uintptr     { return libFoundation }

// Load opens the frameworks and prepares the typed message functions.
func Load() {
	loadOnce.Do(func() {
		libObjC = mustDlopen("/usr/lib/libobjc.A.dylib")
		libFoundation = mustDlopen("/System/Library/Frameworks/Foundation.framework/Foundation")
		libCF = mustDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		libCG = mustDlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		libAppKit = mustDlopen("/System/Library/Frameworks/AppKit.framework/AppKit")
		Dlopen("/System/Library/Frameworks/QuartzCore.framework/QuartzCore")
		Dlopen("/System/Library/Frameworks/CoreText.framework/CoreText")

		msgSendAddr = mustDlsym(libObjC, "objc_msgSend")
		msgSendSuperAddr = mustDlsym(libObjC, "objc_msgSendSuper")
		// Methods returning structs larger than 16 bytes use another entry
		// point on amd64.
		stret := msgSendAddr
		if runtime.GOARCH == "amd64" {
			stret = mustDlsym(libObjC, "objc_msgSend_stret")
		}
		for _, f := range []struct {
			fn   any
			addr uintptr
		}{
			{&MsgRect, stret}, {&MsgRectToRect, stret}, {&MsgRectIDToRect, stret}, {&MsgSizeUintRect, stret}, {&MsgRectRowCol, stret},
			{&MsgSetRect, msgSendAddr}, {&MsgSetRectBool, msgSendAddr}, {&MsgRectToSize, msgSendAddr},
			{&MsgFloat, msgSendAddr}, {&MsgSetFloat, msgSendAddr},
			{&MsgSize, msgSendAddr}, {&MsgSetSize, msgSendAddr}, {&MsgSizeToSize, msgSendAddr},
			{&MsgPoint, msgSendAddr}, {&MsgSetPoint, msgSendAddr}, {&MsgPointID, msgSendAddr}, {&MsgPointFromID, msgSendAddr}, {&MsgIDToPoint, msgSendAddr}, {&MsgIDToSize, msgSendAddr}, {&MsgIDToRect, stret}, {&MsgFloatFromID, msgSendAddr},
			{&MsgPointFromInt, msgSendAddr}, {&MsgLineFragment, stret}, {&MsgGlyphAtPoint, msgSendAddr}, {&msgSuperPoint, msgSendSuperAddr},
			{&MsgRangeToRect, stret}, {&MsgRangeRange, msgSendAddr},
			{&MsgInitRect, msgSendAddr}, {&MsgInitRectID, msgSendAddr}, {&MsgRectToID, msgSendAddr}, {&MsgRectIDVoid, msgSendAddr}, {&MsgInitWindow, msgSendAddr}, {&MsgColor, msgSendAddr},
			{&MsgFloatID, msgSendAddr}, {&MsgFloat2ID, msgSendAddr}, {&MsgIDFloatID, msgSendAddr}, {&MsgFloatIDID, msgSendAddr},
			{&MsgSetFloatInt, msgSendAddr}, {&MsgFloatInt, msgSendAddr},
			{&MsgSetEdges, msgSendAddr}, {&MsgSetTransform, msgSendAddr},
			{&MsgOtherEvent, msgSendAddr}, {&MsgPopUpMenu, msgSendAddr}, {&MsgDateSince, msgSendAddr}, {&MsgTimer, msgSendAddr},
			{&MsgDrawRect, msgSendAddr}, {&MsgDrawAt, msgSendAddr}, {&MsgDrawInRect, msgSendAddr}, {&MsgScrollPoint, msgSendAddr},
		} {
			purego.RegisterFunc(f.fn, f.addr)
		}
		poolPushFn = mustDlsym(libObjC, "objc_autoreleasePoolPush")
		poolPopFn = mustDlsym(libObjC, "objc_autoreleasePoolPop")
		mainNPFn = mustDlsym(purego.RTLD_DEFAULT, "pthread_main_np")
		loadCF()
	})
}

var (
	selMu      sync.RWMutex
	selCache   = map[string]SEL{}
	clsCache   = map[string]ID{}
	superCache = map[string]ID{}
)

// Sel returns the selector for name, cached.
func Sel(name string) SEL {
	selMu.RLock()
	s, ok := selCache[name]
	selMu.RUnlock()
	if ok {
		return s
	}
	s = objc.RegisterName(name)
	selMu.Lock()
	selCache[name] = s
	selMu.Unlock()
	return s
}

// Class returns the class named name, cached. A missing class panics: a
// programming error (check HasClass for newer systems' classes).
func Class(name string) ID {
	selMu.RLock()
	c, ok := clsCache[name]
	selMu.RUnlock()
	if ok {
		return c
	}
	c = ID(objc.GetClass(name))
	if c == 0 {
		panic("ryui: Objective-C class " + name + " not found")
	}
	selMu.Lock()
	clsCache[name] = c
	selMu.Unlock()
	return c
}

// HasClass reports whether a class exists.
func HasClass(name string) bool { return objc.GetClass(name) != 0 }

// Send sends a message whose arguments are integers or pointers. Go
// pointers converted in the call (uintptr(unsafe.Pointer(&x))) are kept
// alive and unmoved until it returns (go:uintptrescapes).
//
//go:uintptrescapes
func Send(obj ID, selector string, args ...uintptr) ID {
	return SendSel(obj, Sel(selector), args...)
}

// SendSel is Send with a selector looked up already.
//
//go:uintptrescapes
func SendSel(obj ID, s SEL, args ...uintptr) ID {
	var a [12]uintptr
	a[0], a[1] = uintptr(obj), uintptr(s)
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSendAddr, a[:n+2]...)
	return ID(r)
}

// SendBool sends a message returning BOOL.
//
//go:uintptrescapes
func SendBool(obj ID, selector string, args ...uintptr) bool {
	return byte(Send(obj, selector, args...)) != 0
}

// SendInt sends a message returning NSInteger.
//
//go:uintptrescapes
func SendInt(obj ID, selector string, args ...uintptr) int {
	return int(Send(obj, selector, args...))
}

type objcSuper struct {
	receiver   ID
	superClass ID
}

// superclass returns the superclass of a class, cached.
func superclass(name string) ID {
	selMu.RLock()
	c, ok := superCache[name]
	selMu.RUnlock()
	if ok {
		return c
	}
	c = ID(objc.Class(Class(name)).SuperClass())
	selMu.Lock()
	superCache[name] = c
	selMu.Unlock()
	return c
}

// SendSuper calls the superclass's implementation of a method that
// className overrides. The superclass is className's, not the object's
// class's, which the runtime may replace (key-value observing does).
//
//go:uintptrescapes
func SendSuper(self ID, className string, selector string, args ...uintptr) ID {
	var pin runtime.Pinner
	defer pin.Unpin()
	sup := &objcSuper{receiver: self, superClass: superclass(className)}
	pin.Pin(sup)
	var a [10]uintptr
	a[0], a[1] = uintptr(unsafe.Pointer(sup)), uintptr(Sel(selector))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSendSuperAddr, a[:n+2]...)
	return ID(r)
}

// Super returns the objc_super for calling className's superclass with
// self through a typed function registered on SuperAddr (for float and
// struct arguments), pinned until pin is unpinned.
func Super(self ID, className string, pin *runtime.Pinner) ID {
	sup := &objcSuper{receiver: self, superClass: superclass(className)}
	pin.Pin(sup)
	return ID(uintptr(unsafe.Pointer(sup)))
}

// SendSuperPointID calls the superclass's method that takes a point and
// returns an object (hitTest:).
func SendSuperPointID(self ID, className, selector string, p Point) ID {
	var pin runtime.Pinner
	defer pin.Unpin()
	return msgSuperPoint(Super(self, className, &pin), Sel(selector), p)
}

// SuperAddr is objc_msgSendSuper, for typed super calls.
func SuperAddr() uintptr { return msgSendSuperAddr }

// MsgSendAddr is objc_msgSend, for typed calls.
func MsgSendAddr() uintptr { return msgSendAddr }

// RespondsTo reports whether obj answers selector.
func RespondsTo(obj ID, selector string) bool {
	return obj != 0 && SendBool(obj, "respondsToSelector:", uintptr(Sel(selector)))
}

// Bool is a BOOL argument.
func Bool(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func Retain(obj ID) ID {
	if obj == 0 {
		return 0
	}
	return Send(obj, "retain")
}

func Release(obj ID) {
	if obj != 0 {
		Send(obj, "release")
	}
}

func Autorelease(obj ID) ID {
	if obj == 0 {
		return 0
	}
	return Send(obj, "autorelease")
}

// New makes an owned (+1) instance of a class with alloc and init.
func New(className string) ID {
	return Send(Send(Class(className), "alloc"), "init")
}

// IsMainThread reports whether this is the process's main thread.
func IsMainThread() bool {
	Load()
	r, _, _ := purego.SyscallN(mainNPFn)
	return int32(r) != 0
}

// WithPool runs fn inside an autorelease pool, on one thread.
func WithPool(fn func()) {
	Load()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(poolPushFn)
	defer purego.SyscallN(poolPopFn, pool)
	fn()
}

const utf8Encoding = 4

// String returns an autoreleased NSString.
func String(s string) ID {
	b := unsafe.StringData(s)
	str := Send(Send(Class("NSString"), "alloc"), "initWithBytes:length:encoding:",
		uintptr(unsafe.Pointer(b)), uintptr(len(s)), utf8Encoding)
	runtime.KeepAlive(s)
	return Autorelease(str)
}

// GoString copies an NSString.
func GoString(str ID) string {
	if str == 0 {
		return ""
	}
	p := Send(str, "UTF8String")
	if p == 0 {
		return ""
	}
	n := SendInt(str, "lengthOfBytesUsingEncoding:", utf8Encoding)
	return string(unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n))
}

// Data returns an autoreleased NSData with a copy of b.
func Data(b []byte) ID {
	var p unsafe.Pointer
	if len(b) > 0 {
		p = unsafe.Pointer(&b[0])
	}
	d := Send(Class("NSData"), "dataWithBytes:length:", uintptr(p), uintptr(len(b)))
	runtime.KeepAlive(b)
	return d
}

// Bytes copies an NSData's contents.
func Bytes(data ID) []byte {
	if data == 0 {
		return nil
	}
	n := SendInt(data, "length")
	if n == 0 {
		return []byte{}
	}
	p := Send(data, "bytes")
	return append([]byte(nil), unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)...)
}

// URL is an NSURL (0 if s isn't one).
func URL(s string) ID { return Send(Class("NSURL"), "URLWithString:", uintptr(String(s))) }

// FileURL is a file NSURL.
func FileURL(path string) ID {
	return Send(Class("NSURL"), "fileURLWithPath:", uintptr(String(path)))
}

// Array returns an autoreleased NSArray.
func Array(objs ...ID) ID {
	arr := Send(Class("NSMutableArray"), "arrayWithCapacity:", uintptr(len(objs)))
	for _, o := range objs {
		Send(arr, "addObject:", uintptr(o))
	}
	return arr
}

// ArrayItems returns an NSArray's objects.
func ArrayItems(arr ID) []ID {
	if arr == 0 {
		return nil
	}
	n := SendInt(arr, "count")
	out := make([]ID, n)
	for i := range n {
		out[i] = Send(arr, "objectAtIndex:", uintptr(i))
	}
	return out
}

// Dict returns an autoreleased NSDictionary of keys and values.
func Dict(kv ...ID) ID {
	d := Send(Class("NSMutableDictionary"), "dictionaryWithCapacity:", uintptr(len(kv)/2))
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != 0 {
			Send(d, "setObject:forKey:", uintptr(kv[i+1]), uintptr(kv[i]))
		}
	}
	return d
}

func NumberInt(v int) ID { return Send(Class("NSNumber"), "numberWithLongLong:", uintptr(v)) }

func NumberFloat(v float64) ID {
	return MsgFloatID(Class("NSNumber"), Sel("numberWithDouble:"), v)
}

func NumberBool(v bool) ID { return Send(Class("NSNumber"), "numberWithBool:", Bool(v)) }

// NewBlock makes a block backed by a Go function, whose first parameter
// is the objc.Block.
func NewBlock(fn any) objc.Block { return objc.NewBlock(fn) }

// CallBlock invokes a block the system handed over.
func CallBlock(block uintptr, args ...uintptr) {
	lit := *(**[3]uintptr)(unsafe.Pointer(&block))
	var a [8]uintptr
	a[0] = block
	n := copy(a[1:], args)
	purego.SyscallN(lit[2], a[:n+1]...)
}

// DefineClass registers an Objective-C class.
func DefineClass(name, super string, protocols []string, methods []objc.MethodDef) ID {
	var ps []*objc.Protocol
	for _, p := range protocols {
		if proto := objc.GetProtocol(p); proto != nil {
			ps = append(ps, proto)
		}
	}
	c, err := objc.RegisterClass(name, objc.GetClass(super), ps, nil, methods)
	if err != nil {
		panic(fmt.Sprintf("ryui: cannot register class %s: %v", name, err))
	}
	selMu.Lock()
	clsCache[name] = ID(c)
	selMu.Unlock()
	return ID(c)
}

// Method is a method of a class DefineClass makes.
func Method(selector string, fn any) objc.MethodDef {
	return objc.MethodDef{Cmd: Sel(selector), Fn: fn}
}

// Const reads an exported object constant (an NSString like
// NSFontAttributeName).
func Const(lib uintptr, name string) ID {
	p, err := purego.Dlsym(lib, name)
	if err != nil || p == 0 {
		return 0
	}
	return **(**ID)(unsafe.Pointer(&p))
}

// Float reads an exported CGFloat constant (NSFontWeightBold).
func FloatConst(lib uintptr, name string) float64 {
	p, err := purego.Dlsym(lib, name)
	if err != nil || p == 0 {
		return 0
	}
	return **(**float64)(unsafe.Pointer(&p))
}

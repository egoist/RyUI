//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/asuka/native"
)

// Packages' native code in Swift (//asuka:native, //asuka:view): the CLI
// builds it, with Asuka's Swift for it (platform/macos), into a library
// in the app's bundle, Contents/Frameworks/AsukaNatives.dylib. The
// host loads it as it starts and calls its functions through purego, so
// nothing here needs cgo: its views are made, set, measured and destroyed
// by the library's functions, their events come back through a function
// of the host's (a callback); and typed modules' calls go straight to it
// from any goroutine (package native's Backend), their answers and Go
// callbacks' calls coming back through two more, from any thread.

// swiftLib is the library's name in Contents/Frameworks.
const swiftLib = "AsukaNatives.dylib"

var swift struct {
	loaded  bool
	make    func(kind string, id uint64) uintptr
	set     func(id uint64, p unsafe.Pointer, n int)
	measure func(id uint64, w, h float64, out *[2]float64)
	destroy func(id uint64)
	free    func(p unsafe.Pointer)

	// Typed modules' functions, called with purego.SyscallN on their
	// addresses (no reflection: a call allocates nothing of its own).
	newObj, call, start, cancel, release uintptr
	malloc                               func(n uintptr) unsafe.Pointer

	nextID   uint64
	contexts map[uint64]native.Context
}

// swiftLibPath is where the app's library is: in its bundle, beside its
// executable's directory.
func swiftLibPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return filepath.Join(filepath.Dir(exe), "..", "Frameworks", swiftLib)
}

// loadSwiftNatives loads the app's Swift library, if it has one, and
// makes it typed modules' backend. Main thread, before the root is
// mounted.
func loadSwiftNatives(path string) error {
	if swift.loaded || path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil // an app with no Swift
	}
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return fmt.Errorf("asuka: cannot load the app's Swift (%s): %v", path, err)
	}
	var boot func(emit, done, invoke uintptr)
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&boot, "asuka_natives_boot"},
		{&swift.make, "asuka_natives_make"},
		{&swift.set, "asuka_natives_set"},
		{&swift.measure, "asuka_natives_measure"},
		{&swift.destroy, "asuka_natives_destroy"},
		{&swift.free, "asuka_natives_free"},
	} {
		if _, err := purego.Dlsym(lib, f.name); err != nil {
			return fmt.Errorf("asuka: the app's Swift (%s) has no %s: rebuild it with this version of Asuka", path, f.name)
		}
		purego.RegisterLibFunc(f.fn, lib, f.name)
	}
	for _, f := range []struct {
		p    *uintptr
		name string
	}{
		{&swift.newObj, "asuka_native_new"},
		{&swift.call, "asuka_native_call"},
		{&swift.start, "asuka_native_start"},
		{&swift.cancel, "asuka_native_cancel"},
		{&swift.release, "asuka_native_release"},
	} {
		if *f.p, err = purego.Dlsym(lib, f.name); err != nil {
			return fmt.Errorf("asuka: the app's Swift (%s) has no %s: rebuild it with this version of Asuka", path, f.name)
		}
	}
	libc, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}
	purego.RegisterLibFunc(&swift.malloc, libc, "malloc")
	swift.loaded = true
	swift.contexts = map[uint64]native.Context{}
	boot(purego.NewCallback(swiftEmit), purego.NewCallback(swiftDone), purego.NewCallback(swiftInvoke))
	native.SetBackend(swiftBackend{})
	return nil
}

// swiftBackend is package native's Backend: the library's typed modules.
type swiftBackend struct{}

// The buffers passed are Go's heap memory (pooled), which doesn't move;
// big is the library's.

func (swiftBackend) New(m *native.Module, iface uint32, out []byte) []byte {
	var big unsafe.Pointer
	n, _, _ := purego.SyscallN(swift.newObj, uintptr(unsafe.Pointer(unsafe.StringData(m.Swift))), uintptr(len(m.Swift)), uintptr(iface),
		uintptr(unsafe.Pointer(unsafe.SliceData(out))), uintptr(len(out)), uintptr(unsafe.Pointer(&big)))
	runtime.KeepAlive(m)
	runtime.KeepAlive(out)
	return swiftAnswer(out, int(n), big)
}

func (swiftBackend) Call(h uint64, method uint32, in, out []byte) []byte {
	var big unsafe.Pointer
	n, _, _ := purego.SyscallN(swift.call, uintptr(h), uintptr(method), uintptr(unsafe.Pointer(unsafe.SliceData(in))), uintptr(len(in)),
		uintptr(unsafe.Pointer(unsafe.SliceData(out))), uintptr(len(out)), uintptr(unsafe.Pointer(&big)))
	runtime.KeepAlive(in)
	runtime.KeepAlive(out)
	return swiftAnswer(out, int(n), big)
}

func (swiftBackend) Start(h uint64, method uint32, in []byte, call uint64) {
	purego.SyscallN(swift.start, uintptr(h), uintptr(method), uintptr(unsafe.Pointer(unsafe.SliceData(in))), uintptr(len(in)), uintptr(call))
	runtime.KeepAlive(in)
}

func (swiftBackend) Cancel(call uint64) { purego.SyscallN(swift.cancel, uintptr(call)) }
func (swiftBackend) Release(h uint64)   { purego.SyscallN(swift.release, uintptr(h)) }

// swiftAnswer is an answer the library wrote: in out, else in memory of
// its own (big), copied and freed.
func swiftAnswer(out []byte, n int, big unsafe.Pointer) []byte {
	if big == nil {
		return out[:n]
	}
	b := append([]byte(nil), unsafe.Slice((*byte)(big), n)...)
	swift.free(big)
	return b
}

// swiftDone answers a started call (a purego callback, from any thread).
func swiftDone(call uint64, p *byte, n int) {
	native.Done(call, unsafe.Slice(p, n))
}

// swiftInvoke calls a Go callback (a purego callback, from any thread):
// its answer goes into memory the library frees (none: 0).
func swiftInvoke(id uint64, p *byte, n int, out *unsafe.Pointer) int {
	a := native.Invoke(id, unsafe.Slice(p, n))
	if a == nil {
		*out = nil
		return 0
	}
	m := swift.malloc(uintptr(max(len(a), 1)))
	copy(unsafe.Slice((*byte)(m), len(a)), a)
	*out = m
	return len(a)
}

// swiftView is a view of a package's Swift, by the id the library knows
// it by.
type swiftView struct {
	id   uint64
	view uintptr
}

func newSwiftView(kind string, ctx native.Context) native.View {
	swift.nextID++
	id := swift.nextID
	swift.contexts[id] = ctx
	v := swift.make(kind, id)
	if v == 0 {
		delete(swift.contexts, id)
		return nil
	}
	return &swiftView{id: id, view: v}
}

// Handle implements native.View: the Swift view's NSView.
func (v *swiftView) Handle() uintptr { return v.view }

// Set implements native.View: the prop, in ui.Native's wire format.
func (v *swiftView) Set(name string, value any) {
	b := native.EncodeProp(name, value)
	if len(b) == 0 {
		return
	}
	swift.set(v.id, unsafe.Pointer(&b[0]), len(b))
}

// Measure implements native.Measurer: the view's sizeThatFits (by
// default its fittingSize).
func (v *swiftView) Measure(w, h float32) (float32, float32) {
	var out [2]float64
	swift.measure(v.id, float64(w), float64(h), &out)
	return float32(out[0]), float32(out[1])
}

// Destroy implements native.Destroyer.
func (v *swiftView) Destroy() {
	swift.destroy(v.id)
	delete(swift.contexts, v.id)
}

// swiftEmit is the library's way to send a view's event (a purego
// callback, on the main thread).
func swiftEmit(id uint64, p *byte, n int) {
	ctx, ok := swift.contexts[id]
	if !ok || p == nil {
		return
	}
	name, values, ok := native.DecodeEvent(unsafe.Slice(p, n))
	if !ok {
		return
	}
	ctx.Emit(name, values)
}

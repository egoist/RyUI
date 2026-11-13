//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/asuka/native"
)

// An app's native code in Swift (its macos/*.swift): the CLI compiles it,
// with Asuka's Swift for it (platform/macos), into a library in the
// app's bundle, Contents/Frameworks/AsukaAppNatives.dylib. The host loads
// it as it starts and calls its functions through purego, so nothing
// here needs cgo. What the app's AsukaAppNatives registers there joins
// package native's registry: its views are made, set, measured and
// destroyed by the library's functions, its events and its modules'
// answers come back through two functions of the host's (callbacks).

// swiftLib is the library's name in Contents/Frameworks.
const swiftLib = "AsukaAppNatives.dylib"

var swift struct {
	loaded  bool
	make    func(kind string, id uint64) uintptr
	set     func(id uint64, p unsafe.Pointer, n int)
	measure func(id uint64, w, h float64, out *[2]float64)
	destroy func(id uint64)
	call    func(module, method string, p unsafe.Pointer, n int, call uint64)
	free    func(p *byte)

	nextID   uint64
	contexts map[uint64]native.Context
	nextCall uint64
	replies  map[uint64]func([]byte, error)
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
// registers what it registers. Main thread, before the root is mounted.
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
	var boot func(emit, reply uintptr) *byte
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&boot, "asuka_natives_boot"},
		{&swift.make, "asuka_natives_make"},
		{&swift.set, "asuka_natives_set"},
		{&swift.measure, "asuka_natives_measure"},
		{&swift.destroy, "asuka_natives_destroy"},
		{&swift.call, "asuka_natives_call"},
		{&swift.free, "asuka_natives_free"},
	} {
		if _, err := purego.Dlsym(lib, f.name); err != nil {
			return fmt.Errorf("asuka: the app's Swift (%s) has no %s: rebuild it with this version of Asuka", path, f.name)
		}
		purego.RegisterLibFunc(f.fn, lib, f.name)
	}
	swift.loaded = true
	swift.contexts = map[uint64]native.Context{}
	swift.replies = map[uint64]func([]byte, error){}
	list := boot(purego.NewCallback(swiftEmit), purego.NewCallback(swiftReply))
	if list == nil {
		return nil
	}
	defer swift.free(list)
	n := binary.LittleEndian.Uint32(unsafe.Slice(list, 4))
	kinds, modules, err := parseSwiftRegistry(unsafe.Slice(list, n)[4:])
	if err != nil {
		return err
	}
	for _, kind := range kinds {
		native.RegisterView(kind, func(ctx native.Context) native.View { return newSwiftView(kind, ctx) })
	}
	for name, methods := range modules {
		m := native.Module{}
		for _, method := range methods {
			m[method] = func(args []byte, reply func([]byte, error)) { swiftCall(name, method, args, reply) }
		}
		native.RegisterModule(name, m)
	}
	return nil
}

// parseSwiftRegistry reads what the library's boot registered: u32
// count, then for each a u8 (1: a view kind, 2: a module method) and its
// names.
func parseSwiftRegistry(b []byte) (kinds []string, modules map[string][]string, err error) {
	bad := errors.New("asuka: the app's Swift library answered a malformed registry")
	take := func(n int) []byte {
		if n < 0 || len(b) < n {
			err = bad
			return nil
		}
		v := b[:n]
		b = b[n:]
		return v
	}
	u32 := func() int {
		v := take(4)
		if v == nil {
			return 0
		}
		return int(binary.LittleEndian.Uint32(v))
	}
	str := func() string { return string(take(u32())) }
	modules = map[string][]string{}
	count := u32()
	for i := 0; i < count && err == nil; i++ {
		t := take(1)
		if t == nil {
			break
		}
		switch t[0] {
		case 1:
			kinds = append(kinds, str())
		case 2:
			name, method := str(), str()
			modules[name] = append(modules[name], method)
		default:
			err = bad
		}
	}
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(kinds)
	return kinds, modules, nil
}

// swiftView is a view of the app's Swift, by the id the library knows it
// by.
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

// swiftCall runs a module's method in the library.
func swiftCall(module, method string, args []byte, reply func([]byte, error)) {
	swift.nextCall++
	id := swift.nextCall
	swift.replies[id] = reply
	var p unsafe.Pointer
	if len(args) > 0 {
		p = unsafe.Pointer(&args[0])
	}
	swift.call(module, method, p, len(args), id)
}

// swiftReply is the library's answer to a call (a purego callback, on the
// main thread): ok 1 with the result, 0 with the error's message.
func swiftReply(id uint64, ok int32, p *byte, n int) {
	reply, found := swift.replies[id]
	if !found {
		return
	}
	delete(swift.replies, id)
	var b []byte
	if p != nil && n > 0 {
		b = append([]byte(nil), unsafe.Slice(p, n)...)
	}
	if ok == 0 {
		reply(nil, errors.New(string(b)))
		return
	}
	reply(b, nil)
}

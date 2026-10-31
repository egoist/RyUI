//go:build darwin && !ios

package darwin

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	cfRunLoopGetMain      func() uintptr
	cfRunLoopSourceCreate func(alloc uintptr, order int, ctx unsafe.Pointer) uintptr
	cfRunLoopAddSource    func(rl, src, mode uintptr)
	kCFRunLoopCommonModes uintptr

	// Called by address on every Signal: RegisterFunc's wrappers use
	// reflection and allocate.
	cfRunLoopSourceSignalFn, cfRunLoopWakeUpFn uintptr
	cfReleaseFn                                uintptr
)

func loadCF() {
	purego.RegisterLibFunc(&cfRunLoopGetMain, libCF, "CFRunLoopGetMain")
	purego.RegisterLibFunc(&cfRunLoopSourceCreate, libCF, "CFRunLoopSourceCreate")
	purego.RegisterLibFunc(&cfRunLoopAddSource, libCF, "CFRunLoopAddSource")
	cfRunLoopSourceSignalFn = mustDlsym(libCF, "CFRunLoopSourceSignal")
	cfRunLoopWakeUpFn = mustDlsym(libCF, "CFRunLoopWakeUp")
	cfReleaseFn = mustDlsym(libCF, "CFRelease")
	p := mustDlsym(libCF, "kCFRunLoopCommonModes")
	kCFRunLoopCommonModes = **(**uintptr)(unsafe.Pointer(&p))
}

// CFRelease releases a Core Foundation object.
func CFRelease(obj uintptr) {
	if obj != 0 {
		purego.SyscallN(cfReleaseFn, obj)
	}
}

// MainSource is a run loop source on the main run loop, in every common
// mode (so it fires while menus track, windows resize and modal sessions
// run). Signal it from any thread; its function runs on the main thread.
type MainSource struct {
	loop, src uintptr
}

var sourceFns []func()

var sourceCallback = purego.NewCallback(func(info uintptr) {
	WithPool(sourceFns[info])
})

// NewMainSource makes a source that runs fn. Main thread.
func NewMainSource(fn func()) *MainSource {
	Load()
	sourceFns = append(sourceFns, fn)
	var ctx [10]uintptr // CFRunLoopSourceContext: info is the 2nd field, perform the last
	ctx[1] = uintptr(len(sourceFns) - 1)
	ctx[9] = sourceCallback
	s := &MainSource{loop: cfRunLoopGetMain()}
	s.src = cfRunLoopSourceCreate(0, 0, unsafe.Pointer(&ctx[0]))
	cfRunLoopAddSource(s.loop, s.src, kCFRunLoopCommonModes)
	return s
}

// Signal makes the source fire. Safe from any goroutine.
func (s *MainSource) Signal() {
	purego.SyscallN(cfRunLoopSourceSignalFn, s.src)
	purego.SyscallN(cfRunLoopWakeUpFn, s.loop)
}

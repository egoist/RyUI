//go:build ios || android

package mobile

/*
#cgo android LDFLAGS: -landroid -llog
#include <stdlib.h>
#include "asuka_host.h"

static asuka_host_callbacks asuka_cb;

static void asuka_set_callbacks(asuka_host_callbacks *cb) { asuka_cb = *cb; }
static void asuka_call_apply(const uint8_t *buf, size_t len) { asuka_cb.apply(buf, len); }
static void asuka_call_measure(uint32_t id, float w, float h, float aw, float ah, float *ow, float *oh) {
	asuka_cb.measure(id, w, h, aw, ah, ow, oh);
}
static void asuka_call_post(void) { asuka_cb.post(); }
static int asuka_has_native(void) { return asuka_cb.native_new != NULL; }
static size_t asuka_call_native_new(const uint8_t *cls, size_t n, uint32_t iface, uint8_t *out, size_t cap, uint8_t **big) {
	return asuka_cb.native_new(cls, n, iface, out, cap, big);
}
static size_t asuka_call_native_call(uint64_t h, uint32_t m, const uint8_t *in, size_t n, uint8_t *out, size_t cap, uint8_t **big) {
	return asuka_cb.native_call(h, m, in, n, out, cap, big);
}
static void asuka_call_native_start(uint64_t h, uint32_t m, const uint8_t *in, size_t n, uint64_t call) {
	asuka_cb.native_start(h, m, in, n, call);
}
static void asuka_call_native_cancel(uint64_t call) { asuka_cb.native_cancel(call); }
static void asuka_call_native_release(uint64_t h) { asuka_cb.native_release(h); }
static void asuka_call_native_free(void *p) { asuka_cb.native_free(p); }
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"github.com/egoist/asuka/host"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/crashrun"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

type mobileHost struct {
	env    host.Environment
	mu     sync.Mutex
	posted []func()
}

var (
	theHost = &mobileHost{env: host.Environment{Scale: 1, FontScale: 1}}
	started bool
	// core takes the host's events and list requests: the app's, in
	// process, unless a player forwards them (SetCore).
	core host.Core = ui.Core()
)

// Host returns the platform host (for a player).
func Host() host.Host { return theHost }

// SetCore sends the host's events and list requests to c instead of the
// app's core (a player, which forwards them to a core elsewhere).
func SetCore(c host.Core) { core = c }

func (h *mobileHost) Apply(b *ops.Batch) {
	buf := b.Encode()
	C.asuka_call_apply((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
}

func (h *mobileHost) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	var w, hh C.float
	C.asuka_call_measure(C.uint32_t(id), C.float(c.Width), C.float(c.Height), C.float(c.AvailW), C.float(c.AvailH), &w, &hh)
	return layout.Size{W: float32(w), H: float32(hh)}
}

func (h *mobileHost) Post(f func()) {
	h.mu.Lock()
	h.posted = append(h.posted, f)
	h.mu.Unlock()
	C.asuka_call_post()
}

func (h *mobileHost) Environment() host.Environment { return h.env }

//export asuka_start
func asuka_start(cb *C.asuka_host_callbacks) {
	if started {
		return
	}
	started = true
	C.asuka_set_callbacks(cb)
	if C.asuka_has_native() != 0 {
		native.SetBackend(mobileNative{})
	}
	boot.Runner = func(root any) {
		f, ok := root.(func() ui.Node)
		if !ok {
			panic(fmt.Sprintf("asuka: app.Run wants a func() ui.Node, got %T", root))
		}
		ui.Mount(theHost, f)
	}
	if Main == nil {
		fmt.Fprintln(os.Stderr, "asuka: no main function; build the app with the asuka CLI")
		return
	}
	Main()
}

//export asuka_attach
func asuka_attach() {
	// A new Activity took over the existing views (Android): tell it
	// which view is the root again.
	if !started {
		return
	}
	if r, ok := core.(interface{ Reattach() }); ok {
		r.Reattach()
	} else {
		ui.Reattach()
	}
}

//export asuka_event
func asuka_event(buf *C.uint8_t, n C.size_t) {
	data := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n))
	ev, err := ops.DecodeEvent(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "asuka:", err)
		return
	}
	if ev.Kind == ops.EventEnvironment {
		theHost.env = ev.Env
	}
	if started {
		core.Dispatch(ev)
	}
}

//export asuka_list_count
func asuka_list_count(list C.uint32_t) C.int64_t {
	return C.int64_t(core.ListCount(ops.NodeID(list)))
}

//export asuka_list_bind
func asuka_list_bind(list C.uint32_t, index C.int64_t, cell C.uint32_t) {
	core.ListBind(ops.NodeID(list), int(index), ops.NodeID(cell))
}

//export asuka_list_unbind
func asuka_list_unbind(list C.uint32_t, cell C.uint32_t) {
	core.ListUnbind(ops.NodeID(list), ops.NodeID(cell))
}

// asuka_crash_start begins this run's crash record, if the app reports
// crashes (asuka/sdk/crash): Go's crash output, and the run's context, with
// info, the host's JSON of the device and the app (model, system,
// systemVersion, appVersion, build) and, if it knows, whether the app
// starts in the foreground (foreground). The host calls it as early as it
// can, before asuka_start, from any thread. It returns the directory the
// host writes its native reports in (inbox), "" if the app doesn't report
// crashes: the host installs nothing then. The caller frees it.
//
//export asuka_crash_start
func asuka_crash_start(info *C.char) *C.char {
	var i struct {
		crashrun.Info
		Foreground *bool `json:"foreground"`
	}
	if info != nil {
		json.Unmarshal([]byte(C.GoString(info)), &i)
	}
	dir := crashrun.Start(i.Info)
	if i.Foreground != nil {
		crashrun.Foreground(*i.Foreground)
	}
	return C.CString(dir)
}

// asuka_crash_notify says the host wrote a report in the inbox (MetricKit's
// diagnostics, Android's record of how the last runs ended): asuka/sdk/crash
// reads it, for OnReport. Any thread.
//
//export asuka_crash_notify
func asuka_crash_notify() { crashrun.Notify() }

// asuka_setenv sets an environment variable for the Go code: Android
// hands a library the environment the process started with, so a
// variable the host sets later (ASUKA_DATA, the app's files directory) is
// given here.
//
//export asuka_setenv
func asuka_setenv(key, value *C.char) {
	os.Setenv(C.GoString(key), C.GoString(value))
}

//export asuka_run_posted
func asuka_run_posted() {
	h := theHost
	h.mu.Lock()
	fs := h.posted
	h.posted = nil
	h.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// mobileNative is package native's Backend: the host's typed modules
// (Swift on iOS, Kotlin through JNI on Android), called from any
// goroutine.
type mobileNative struct{}

func cbytes(b []byte) *C.uint8_t { return (*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(b))) }

// answer is an answer the host wrote: in out, else in memory of its own
// (big), copied and freed.
func answer(out []byte, n C.size_t, big *C.uint8_t) []byte {
	if big == nil {
		return out[:n]
	}
	b := C.GoBytes(unsafe.Pointer(big), C.int(n))
	C.asuka_call_native_free(unsafe.Pointer(big))
	return b
}

func (mobileNative) New(m *native.Module, iface uint32, out []byte) []byte {
	cls := m.Swift
	if runtime.GOOS == "android" {
		cls = m.Kotlin
	}
	var big *C.uint8_t
	n := C.asuka_call_native_new((*C.uint8_t)(unsafe.Pointer(unsafe.StringData(cls))), C.size_t(len(cls)), C.uint32_t(iface), cbytes(out), C.size_t(len(out)), &big)
	return answer(out, n, big)
}

func (mobileNative) Call(h uint64, method uint32, in, out []byte) []byte {
	var big *C.uint8_t
	n := C.asuka_call_native_call(C.uint64_t(h), C.uint32_t(method), cbytes(in), C.size_t(len(in)), cbytes(out), C.size_t(len(out)), &big)
	return answer(out, n, big)
}

func (mobileNative) Start(h uint64, method uint32, in []byte, call uint64) {
	C.asuka_call_native_start(C.uint64_t(h), C.uint32_t(method), cbytes(in), C.size_t(len(in)), C.uint64_t(call))
}

func (mobileNative) Cancel(call uint64) { C.asuka_call_native_cancel(C.uint64_t(call)) }
func (mobileNative) Release(h uint64)   { C.asuka_call_native_release(C.uint64_t(h)) }

// asuka_native_done answers a started call (the host, from any thread).
//
//export asuka_native_done
func asuka_native_done(call C.uint64_t, p *C.uint8_t, n C.size_t) {
	native.Done(uint64(call), unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n)))
}

// asuka_native_invoke calls a Go callback (the host, from any thread):
// its answer into *out, which the host frees (none: 0).
//
//export asuka_native_invoke
func asuka_native_invoke(id C.uint64_t, p *C.uint8_t, n C.size_t, out **C.uint8_t) C.size_t {
	a := native.Invoke(uint64(id), unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n)))
	if a == nil {
		*out = nil
		return 0
	}
	m := (*C.uint8_t)(C.malloc(C.size_t(max(len(a), 1))))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(m)), len(a)), a)
	*out = m
	return C.size_t(len(a))
}

//go:build ios || android

package mobile

/*
#cgo android LDFLAGS: -landroid -llog
#include "nui_host.h"

static nui_host_callbacks nui_cb;

static void nui_set_callbacks(nui_host_callbacks *cb) { nui_cb = *cb; }
static void nui_call_apply(const uint8_t *buf, size_t len) { nui_cb.apply(buf, len); }
static void nui_call_measure(uint32_t id, float w, float h, float aw, float ah, float *ow, float *oh) {
	nui_cb.measure(id, w, h, aw, ah, ow, oh);
}
static void nui_call_post(void) { nui_cb.post(); }
*/
import "C"

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/internal/boot"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/ui"
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
	C.nui_call_apply((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
}

func (h *mobileHost) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	var w, hh C.float
	C.nui_call_measure(C.uint32_t(id), C.float(c.Width), C.float(c.Height), C.float(c.AvailW), C.float(c.AvailH), &w, &hh)
	return layout.Size{W: float32(w), H: float32(hh)}
}

func (h *mobileHost) Post(f func()) {
	h.mu.Lock()
	h.posted = append(h.posted, f)
	h.mu.Unlock()
	C.nui_call_post()
}

func (h *mobileHost) Environment() host.Environment { return h.env }

//export nui_start
func nui_start(cb *C.nui_host_callbacks) {
	if started {
		return
	}
	started = true
	C.nui_set_callbacks(cb)
	boot.Runner = func(root any) {
		f, ok := root.(func() ui.Node)
		if !ok {
			panic(fmt.Sprintf("nui: app.Run wants a func() ui.Node, got %T", root))
		}
		ui.Mount(theHost, f)
	}
	if Main == nil {
		fmt.Fprintln(os.Stderr, "nui: no main function; build the app with the nui CLI")
		return
	}
	Main()
}

//export nui_attach
func nui_attach() {
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

//export nui_event
func nui_event(buf *C.uint8_t, n C.size_t) {
	data := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n))
	ev, err := ops.DecodeEvent(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nui:", err)
		return
	}
	if ev.Kind == ops.EventEnvironment {
		theHost.env = ev.Env
	}
	if started {
		core.Dispatch(ev)
	}
}

//export nui_list_count
func nui_list_count(list C.uint32_t) C.int64_t {
	return C.int64_t(core.ListCount(ops.NodeID(list)))
}

//export nui_list_bind
func nui_list_bind(list C.uint32_t, index C.int64_t, cell C.uint32_t) {
	core.ListBind(ops.NodeID(list), int(index), ops.NodeID(cell))
}

//export nui_list_unbind
func nui_list_unbind(list C.uint32_t, cell C.uint32_t) {
	core.ListUnbind(ops.NodeID(list), ops.NodeID(cell))
}

//export nui_run_posted
func nui_run_posted() {
	h := theHost
	h.mu.Lock()
	fs := h.posted
	h.posted = nil
	h.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

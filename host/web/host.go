//go:build js && wasm

// Package web is RyUI's web host: the app's Go code compiled to
// WebAssembly, its views the page's DOM, made and driven by the web
// platform's JavaScript host (platform/web/src/host.js), as the Swift and
// Kotlin hosts drive UIKit and Android's views.
//
// The core crosses into JavaScript once per batch: Apply encodes the
// batch (ops.Batch.Encode) and hands JavaScript its place in the
// WebAssembly memory, which it decodes in place with the generated
// decoder (platform/web/src/ops.js). Measure is a synchronous call, its
// constraints and answer in a small shared buffer, so it allocates
// nothing. Events come back encoded, as from the mobile hosts.
//
// The page's one thread is the main thread: the core runs in the
// callbacks JavaScript makes into Go (events, list requests, posted
// work), and Run keeps the program alive between them.
package web

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"syscall/js"
	"unsafe"

	"github.com/egoist/ryui/host"
	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/internal/l10n"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// Host is the web host's Go side.
type Host struct {
	js   js.Value // the JavaScript host (platform/web/src/host.js)
	core host.Core
	env  host.Environment

	// io is shared with JavaScript (its address goes over once, in
	// connect): Apply's batch address and length; Measure's node, its
	// constraints and the answer. Float32 lanes, but for the first two
	// u32s.
	io [8]uint32

	applyFn, measureFn, postFn js.Value
	evbuf                      []byte

	postMu sync.Mutex
	posted []func()
	queued bool

	funcs []js.Func // kept for the life of the program
}

var theHost *Host

// Run mounts root on the page and serves it until the page goes away.
// Call it from main (app.Run does).
func Run(root func() ui.Node) {
	h := newHost()
	h.restoreState()
	h.enter(func() { ui.Mount(h, root) })
	h.js.Call("started")
	select {} // the page's callbacks run the app from here on
}

func newHost() *Host {
	name := os.Getenv("RYUI_WEB_HOST")
	if name == "" {
		name = "ryuiHost"
	}
	jh := js.Global().Get(name)
	if jh.IsUndefined() {
		panic("ryui: no web host on this page (" + name + "); load the app with platform/web's loader, as ryui dev -platform web and ryui build -platform web do")
	}
	h := &Host{js: jh}
	theHost = h
	h.applyFn = jh.Get("apply").Call("bind", jh)
	h.measureFn = jh.Get("measure").Call("bind", jh)
	h.postFn = jh.Get("post").Call("bind", jh)
	h.connect()
	if ev, err := ops.DecodeEvent(h.bytes(jh.Call("environment"))); err == nil {
		h.env = ev.Env
	}
	return h
}

// connect gives JavaScript the core's entry points and the shared
// buffer's address.
func (h *Host) connect() {
	core := js.Global().Get("Object").New()
	fn := func(name string, f func(args []js.Value) any) {
		jf := js.FuncOf(func(_ js.Value, args []js.Value) any { return f(args) })
		h.funcs = append(h.funcs, jf)
		core.Set(name, jf)
	}
	fn("event", func(args []js.Value) any {
		ev, err := ops.DecodeEvent(h.bytes(args[0]))
		if err != nil {
			fmt.Fprintln(os.Stderr, "ryui:", err)
			return nil
		}
		if ev.Kind == ops.EventEnvironment {
			h.env = ev.Env
		}
		if h.core != nil {
			h.enter(func() { h.core.Dispatch(ev) })
		}
		return nil
	})
	fn("listCount", func(args []js.Value) any {
		n := 0
		h.enter(func() { n = h.core.ListCount(ops.NodeID(args[0].Int())) })
		return n
	})
	fn("listBind", func(args []js.Value) any {
		h.enter(func() { h.core.ListBind(ops.NodeID(args[0].Int()), args[1].Int(), ops.NodeID(uint32(args[2].Int()))) })
		return nil
	})
	fn("listUnbind", func(args []js.Value) any {
		h.enter(func() { h.core.ListUnbind(ops.NodeID(args[0].Int()), ops.NodeID(uint32(args[1].Int()))) })
		return nil
	})
	fn("runPosted", func([]js.Value) any {
		h.runPosted()
		return nil
	})
	// ryui dev: the page is about to load the new build.
	fn("saveState", func([]js.Value) any {
		var s string
		h.enter(func() { s = encodeState(signal.KeptState()) })
		return s
	})
	// Pure lookups, which JavaScript may make while Go is calling it.
	fn("words", func(args []js.Value) any {
		loc := args[0].String()
		b, _ := json.Marshal(map[string]string{
			"ok": l10n.OK(loc), "back": l10n.Back(loc), "search": l10n.Search(loc),
			"increase": l10n.Increase(loc), "decrease": l10n.Decrease(loc),
		})
		return string(b)
	})
	fn("shaderGLSL", func(args []js.Value) any {
		if boot.ShaderGLSL == nil {
			return nil
		}
		code := make([]byte, args[0].Length())
		js.CopyBytesToGo(code, args[0])
		src, err := boot.ShaderGLSL(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ryui:", err)
			return nil
		}
		return src
	})
	h.js.Call("connect", core, uintptr(unsafe.Pointer(&h.io[0])))
}

// bytes copies a Uint8Array into the host's event buffer.
func (h *Host) bytes(v js.Value) []byte {
	n := v.Length()
	if cap(h.evbuf) < n {
		h.evbuf = make([]byte, n, max(n, 2*cap(h.evbuf)))
	}
	b := h.evbuf[:n]
	js.CopyBytesToGo(b, v)
	return b
}

// enter runs f, a call into the core. The goroutine JavaScript called
// in on is the main thread while it runs (ryui_debug's checks).
func (h *Host) enter(f func()) {
	signal.BindThread()
	f()
}

// Connect implements host.Connector.
func (h *Host) Connect(c host.Core) { h.core = c }

// Environment implements host.Host.
func (h *Host) Environment() host.Environment { return h.env }

// Apply implements host.Host: one call into JavaScript, which reads the
// encoded batch out of the WebAssembly memory.
func (h *Host) Apply(b *ops.Batch) {
	buf := b.Encode()
	if len(buf) == 0 {
		return
	}
	h.io[0] = uint32(uintptr(unsafe.Pointer(&buf[0])))
	h.io[1] = uint32(len(buf))
	h.applyFn.Invoke()
}

// Measure implements host.Host: the constraints go in the shared buffer
// (NaN: free; +Inf max-content, -Inf min-content), and JavaScript writes
// the size there.
func (h *Host) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	f := (*[8]float32)(unsafe.Pointer(&h.io[0]))
	h.io[2] = uint32(id)
	f[3], f[4], f[5], f[6] = c.Width, c.Height, c.AvailW, c.AvailH
	h.measureFn.Invoke()
	return layout.Size{W: f[3], H: f[4]}
}

// Post implements host.Host: f runs on the page's thread once the code
// running now returns to the page. Safe from any goroutine.
func (h *Host) Post(f func()) {
	h.postMu.Lock()
	h.posted = append(h.posted, f)
	first := !h.queued
	h.queued = true
	h.postMu.Unlock()
	if first {
		h.postFn.Invoke()
	}
}

func (h *Host) runPosted() {
	h.postMu.Lock()
	fs := h.posted
	h.posted = nil
	h.queued = false
	h.postMu.Unlock()
	for _, f := range fs {
		h.enter(f)
	}
}

// restoreState gives the app's signals the values the last build saved
// (ryui dev: the page keeps them in its session storage across the
// reload to the new build).
func (h *Host) restoreState() {
	v := h.js.Call("savedState")
	if v.Type() != js.TypeString {
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(v.String()), &m); err != nil {
		fmt.Fprintln(os.Stderr, "ryui: restoring state:", err)
		return
	}
	saved := make(map[string][]byte, len(m))
	for k, v := range m {
		saved[k] = v
	}
	signal.KeepState(saved)
}

func encodeState(vals map[string][]byte) string {
	m := make(map[string]json.RawMessage, len(vals))
	for k, v := range vals {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ryui: saving state:", err)
		return ""
	}
	return string(b)
}

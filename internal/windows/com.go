//go:build windows && amd64

package windows

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// COM, as WebView2 needs it: calls through vtables, and handler objects
// whose one method calls a Go function.

// ComCall calls method i of the COM interface obj. Pointers converted to
// uintptr in the call stay valid until it returns (go:uintptrescapes).
//
//go:uintptrescapes
func ComCall(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(native(obj))
	fn := *(*uintptr)(unsafe.Add(native(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	var a [16]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

// Failed reports whether an HRESULT is an error.
func Failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func AddRef(obj uintptr) {
	if obj != 0 {
		ComCall(obj, 1)
	}
}

func Release(obj uintptr) {
	if obj != 0 {
		ComCall(obj, 2)
	}
}

// GUID is a COM interface ID.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// NewGUID parses a GUID written as xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.
func NewGUID(s string) GUID {
	h := strings.ReplaceAll(s, "-", "")
	p := func(i, n int) uint64 {
		v, _ := strconv.ParseUint(h[i:i+n], 16, 64)
		return v
	}
	g := GUID{Data1: uint32(p(0, 8)), Data2: uint16(p(8, 4)), Data3: uint16(p(12, 4))}
	for i := range 8 {
		g.Data4[i] = byte(p(16+2*i, 2))
	}
	return g
}

// QueryInterface returns obj's implementation of iid (a reference the
// caller releases), or 0.
func QueryInterface(obj uintptr, iid *GUID) uintptr {
	if obj == 0 {
		return 0
	}
	var out uintptr
	if Failed(ComCall(obj, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))) {
		return 0
	}
	return out
}

// TakeString reads a string COM allocated (CoTaskMemAlloc) and frees it.
func TakeString(p uintptr) string {
	s := GoStr(p)
	if p != 0 {
		ole32.NewProc("CoTaskMemFree").Call(p)
	}
	return s
}

// A handler is a COM object with one method besides IUnknown, as
// WebView2's event and completion handlers are. They share one vtable,
// whose callbacks are made once; each routes Invoke to its Go function,
// and stays reachable through handlers until COM releases it.
type handler struct {
	vtbl *[4]uintptr
	refs int32
	fn   func(a, b uintptr)
}

var (
	handlerVtbl [4]uintptr
	handlerOnce sync.Once
	handlersMu  sync.Mutex
	handlers    = map[uintptr]*handler{}
)

func handlerRef(this uintptr, delta int32) int32 {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	h := handlers[this]
	if h == nil {
		return 0
	}
	h.refs += delta
	if h.refs <= 0 {
		delete(handlers, this)
		return 0
	}
	return h.refs
}

// NewHandler makes a handler holding one reference, which the caller
// releases once it has handed it to COM.
func NewHandler(fn func(a, b uintptr)) uintptr {
	handlerOnce.Do(func() {
		handlerVtbl = [4]uintptr{
			syscall.NewCallback(func(this, riid, out uintptr) uintptr {
				*(*uintptr)(native(out)) = this
				handlerRef(this, 1)
				return 0
			}),
			syscall.NewCallback(func(this uintptr) uintptr { return uintptr(handlerRef(this, 1)) }),
			syscall.NewCallback(func(this uintptr) uintptr { return uintptr(handlerRef(this, -1)) }),
			syscall.NewCallback(func(this, a, b uintptr) uintptr {
				handlersMu.Lock()
				h := handlers[this]
				handlersMu.Unlock()
				if h != nil {
					h.fn(a, b)
				}
				return 0
			}),
		}
	})
	h := &handler{vtbl: &handlerVtbl, refs: 1, fn: fn}
	p := uintptr(unsafe.Pointer(h))
	handlersMu.Lock()
	handlers[p] = h
	handlersMu.Unlock()
	return p
}

// WithHandler passes a new handler to call, which takes its own
// reference if it keeps it, and drops the caller's.
func WithHandler(fn func(a, b uintptr), call func(h uintptr) uintptr) uintptr {
	h := NewHandler(fn)
	defer Release(h)
	return call(h)
}

// WebView2.

// webView2Runtime finds the Microsoft Edge WebView2 Runtime's client
// library as WebView2Loader.dll does: EdgeUpdate records where each
// channel's runtime is, per machine and per user.
func webView2Runtime() (string, error) {
	if dir := os.Getenv("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"); dir != "" {
		return filepath.Join(dir, "EBWebView", "x64", "EmbeddedBrowserWebView.dll"), nil
	}
	for _, channel := range []string{
		"{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}", // Stable
		"{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}", // Beta
		"{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}", // Dev
		"{65C35B14-6C1D-4122-AC46-7148CC9D6497}", // Canary
	} {
		for _, root := range []uintptr{0x80000002, hkeyCurrentUser} { // HKLM, HKCU
			dir := regString(root, `SOFTWARE\Microsoft\EdgeUpdate\ClientState\`+channel, "EBWebView")
			if dir == "" {
				continue
			}
			dll := filepath.Join(dir, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
			if _, err := os.Stat(dll); err == nil {
				return dll, nil
			}
		}
	}
	return "", errors.New("the Microsoft Edge WebView2 Runtime isn't installed (https://go.microsoft.com/fwlink/p/?LinkId=2124703)")
}

// regString reads a string value from the 32-bit view of the registry,
// where EdgeUpdate records its clients.
func regString(root uintptr, path, name string) string {
	var key uintptr
	if r, _, _ := procRegOpenKeyExW.Call(root, Str(path), 0, 0x0001|0x0200, uintptr(unsafe.Pointer(&key))); r != 0 { // KEY_QUERY_VALUE | KEY_WOW64_32KEY
		return ""
	}
	defer procRegCloseKey.Call(key)
	var typ, size uint32
	if r, _, _ := procRegQueryValueExW.Call(key, Str(name), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size))); r != 0 || size == 0 {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if r, _, _ := procRegQueryValueExW.Call(key, Str(name), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// CreateWebView2Environment starts making the WebView2 environment
// (its data in userData); done gets it on the main thread, or an error.
func CreateWebView2Environment(userData string, done func(env uintptr, err error)) error {
	dll, err := webView2Runtime()
	if err != nil {
		return err
	}
	proc := syscall.NewLazyDLL(dll).NewProc("CreateWebViewEnvironmentWithOptionsInternal")
	if err := proc.Find(); err != nil {
		return fmt.Errorf("an unsupported WebView2 Runtime at %s: %w", dll, err)
	}
	completed := func(hr, env uintptr) {
		if Failed(hr) || env == 0 {
			done(0, fmt.Errorf("creating the WebView2 environment failed (HRESULT %#08x)", uint32(hr)))
			return
		}
		AddRef(env)
		done(env, nil)
	}
	hr := WithHandler(completed, func(h uintptr) uintptr {
		r, _, _ := proc.Call(1, 0, Str(userData), 0, h)
		return r
	})
	if Failed(hr) {
		return fmt.Errorf("creating the WebView2 environment failed (HRESULT %#08x)", uint32(hr))
	}
	return nil
}

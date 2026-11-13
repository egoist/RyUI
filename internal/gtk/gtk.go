//go:build linux && !android

// Package gtk drives GTK 4, libadwaita and the GLib stack under them from
// Go with purego, no cgo: the toolkit host/linux is written in.
//
// Calls whose arguments are all integers or pointers can go through Call
// (purego.SyscallN, no reflection): the hot paths (allocating frames,
// drawing) do. The rest are typed functions purego builds once (Load),
// which use reflection per call; float arguments need them.
//
// GTK is single-threaded: everything here but IdleAdd and the main
// context's wakeup runs on the main thread, the one Run locks.
package gtk

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Ptr is a C pointer: a GObject, a struct, a string.
type Ptr = uintptr

var (
	libGLib, libGObject, libGIO, libGTK, libAdw uintptr
	libPango, libPangoCairo, libCairo           uintptr
	loadOnce                                    sync.Once
	loadErr                                     error
)

// sonames are the libraries, tried in order (a distribution's name, then
// the development link's).
var sonames = map[*uintptr][]string{
	&libGLib:       {"libglib-2.0.so.0", "libglib-2.0.so"},
	&libGObject:    {"libgobject-2.0.so.0", "libgobject-2.0.so"},
	&libGIO:        {"libgio-2.0.so.0", "libgio-2.0.so"},
	&libGTK:        {"libgtk-4.so.1", "libgtk-4.so"},
	&libAdw:        {"libadwaita-1.so.0", "libadwaita-1.so"},
	&libPango:      {"libpango-1.0.so.0", "libpango-1.0.so"},
	&libPangoCairo: {"libpangocairo-1.0.so.0", "libpangocairo-1.0.so"},
	&libCairo:      {"libcairo.so.2", "libcairo.so"},
}

// Load opens the libraries and binds the functions. It fails when GTK 4
// or libadwaita isn't installed (the error says which).
func Load() error {
	loadOnce.Do(func() {
		for _, lib := range []*uintptr{&libGLib, &libGObject, &libGIO, &libCairo, &libPango, &libPangoCairo, &libGTK, &libAdw} {
			var err error
			for _, name := range sonames[lib] {
				*lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
				if err == nil {
					break
				}
			}
			if *lib == 0 {
				loadErr = fmt.Errorf("asuka: %s isn't installed (%v): the Linux host needs GTK 4.14 and libadwaita 1.5 or later", sonames[lib][0], err)
				return
			}
		}
		bind()
	})
	return loadErr
}

// MustLoad is Load, panicking on failure.
func MustLoad() {
	if err := Load(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// sym is a library's symbol, 0 if it has none (an older version).
func sym(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0
	}
	return p
}

// Sym is a symbol in any of the libraries Load opened (0: none).
func Sym(name string) uintptr {
	for _, lib := range []uintptr{libAdw, libGTK, libGIO, libGObject, libGLib, libPango, libPangoCairo, libCairo} {
		if p := sym(lib, name); p != 0 {
			return p
		}
	}
	return 0
}

// Has reports whether the libraries have a function (a newer GTK's or
// libadwaita's).
func Has(name string) bool { return Sym(name) != 0 }

// fn binds a typed function to a symbol; one that's missing stays nil.
func fn(ptr any, name string) {
	p := Sym(name)
	if p == 0 {
		return
	}
	purego.RegisterFunc(ptr, p)
}

// Call calls a C function with integer and pointer arguments, without
// reflection. The result is its integer or pointer result.
func Call(f uintptr, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(f, args...)
	return r
}

// Bool is a gboolean.
func Bool(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// CString is s as a NUL-terminated C string in Go memory: valid while the
// result is reachable (keep it in a variable over the call).
func CString(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// GoString copies a NUL-terminated C string.
func GoString(p Ptr) string {
	if p == 0 {
		return ""
	}
	b := At[byte](p)
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(b), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(b, n))
}

// GoStringFree copies a C string GLib allocated, and frees it.
func GoStringFree(p Ptr) string {
	s := GoString(p)
	if p != 0 {
		Free(p)
	}
	return s
}

// GoBytes copies n bytes at p.
func GoBytes(p Ptr, n int) []byte {
	if p == 0 || n <= 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(At[byte](p), n)...)
}

// At is the C memory at p as a *T (go vet's unsafeptr check accepts this
// form of the conversion).
func At[T any](p Ptr) *T { return *(**T)(unsafe.Pointer(&p)) }

// Addr is the address of a Go value, for a C call that reads or writes
// it during the call.
func Addr[T any](v *T) Ptr { return uintptr(unsafe.Pointer(v)) }

// mainThread is the OS thread GTK runs on.
var mainThread int = -1

// LockMain locks the calling goroutine to its OS thread and makes it
// GTK's main thread. Call it from an init in the package main imports
// (the main goroutine).
func LockMain() {
	runtime.LockOSThread()
}

// IsMainThread reports whether this is the thread GLib's default main
// context belongs to.
func IsMainThread() bool {
	if MainContextIsOwner == nil {
		return true
	}
	return MainContextIsOwner(MainContextDefault())
}

// EditableGetText is an editable's text (gtk_editable_get_text: the
// widget's own memory, copied).
func EditableGetText(e Ptr) string { return GoString(Call(FnEditableGetText, e)) }

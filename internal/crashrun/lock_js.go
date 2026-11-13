//go:build js && wasm

package crashrun

import (
	"os"
	"syscall/js"
)

// In a browser the run's record is kept with the web host
// (platform/web/src/crash.js): Go's runtime can't write its crash output
// to a file there (debug.SetCrashOutput needs dup, which js/wasm hasn't),
// so the host keeps what the runtime prints and writes it to the run's
// .crash file as the program ends with a crash. A run that lives is one
// the host of a page marks so (in the origin's localStorage, which every
// page of it shares, as it shares the app's files).

// crash is the web host's crash.js, nil without one (a test).
func crash() js.Value {
	name := os.Getenv("RYUI_WEB_HOST")
	if name == "" {
		name = "ryuiHost"
	}
	h := js.Global().Get(name)
	if h.Type() != js.TypeObject {
		return js.Undefined()
	}
	return h.Get("crash")
}

// lock tells the host this run's crash output is f: it writes Go's crash
// there, and marks the run alive while the page lives.
func lock(f *os.File) {
	if c := crash(); c.Type() == js.TypeObject {
		c.Call("begin", f.Name())
	}
}

// Alive reports whether the run whose crash output is path still runs:
// a page has it marked.
func Alive(path string) bool {
	c := crash()
	return c.Type() == js.TypeObject && c.Call("alive", path).Truthy()
}

// redirectStderr: the host keeps what the runtime prints.
func redirectStderr(path string) {}

// Lock: a page's Go code runs on its one thread, and each page reads its
// own copy of the app's files.
func Lock(path string) (unlock func()) { return func() {} }

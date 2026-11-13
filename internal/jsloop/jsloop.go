// Package jsloop knows when a WebAssembly program in a browser (js/wasm)
// runs in one of the page's callbacks: an event, posted work, a list's
// request. The page waits for the callback to return, so the goroutine
// it runs on can't wait for anything the page must do first (a worker's
// answer): Go's runtime would find every goroutine asleep. A package
// that would wait for the page (asuka/sqlite) asks InCallback first and
// fails instead.
//
// host/web brackets the callbacks that run the app's code with Enter and
// Leave. Other goroutines may run meanwhile (while the page calls back
// into Go during a callback, Go runs them before it returns to the page)
// and wait as they like: InCallback is about the calling goroutine. Off
// js/wasm nothing calls Enter, and InCallback is false.
package jsloop

// entered are the goroutines in a callback, innermost last.
var entered []uintptr

// Enter notes that a callback from the page begins, on this goroutine.
// Main thread.
func Enter() { entered = append(entered, current()) }

// Leave notes that it returns.
func Leave() { entered = entered[:len(entered)-1] }

// InCallback reports whether the calling goroutine runs a callback of
// the page's, which the page waits for.
func InCallback() bool {
	if len(entered) == 0 {
		return false
	}
	g := current()
	for _, e := range entered {
		if e == g {
			return true
		}
	}
	return false
}

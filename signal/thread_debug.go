//go:build nui_debug

package signal

import (
	"bytes"
	"runtime"
)

var mainGoroutine uint64

// BindThread makes the calling goroutine the main thread: signals
// written from another one panic.
func BindThread() { mainGoroutine = goroutineID() }

func checkThread() {
	if mainGoroutine != 0 && goroutineID() != mainGoroutine {
		panic("signal: Set off the main thread; use ui.Post")
	}
}

func goroutineID() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	var id uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// Debug reports whether this is a nui_debug build (with main-thread checks).
const Debug = true

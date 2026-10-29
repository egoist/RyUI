//go:build !nui_debug

package signal

func checkThread() {}

// BindThread makes the calling goroutine the main thread. Builds with the
// nui_debug tag then panic when a signal is written from another one.
func BindThread() {}

// Debug reports whether this is a nui_debug build (with main-thread checks).
const Debug = false

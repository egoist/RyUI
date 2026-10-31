//go:build !ryui_debug

package signal

func checkThread() {}

// BindThread makes the calling goroutine the main thread. Builds with the
// ryui_debug tag then panic when a signal is written from another one.
func BindThread() {}

// Debug reports whether this is a ryui_debug build (with main-thread checks).
const Debug = false

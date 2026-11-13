//go:build wasm

package jsloop

// current is the calling goroutine's g (runtime's), which tells
// goroutines apart: g_wasm.s.
func current() uintptr

//go:build !wasm

package jsloop

// current is 0 where nothing enters (not js/wasm).
func current() uintptr { return 0 }

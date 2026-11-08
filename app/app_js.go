//go:build js && wasm

package app

import "github.com/egoist/ryui/host/web"

// A browser: the page's DOM, in this program (WebAssembly).
func init() { desktop = web.Run }

//go:build js && wasm

package web

import (
	"syscall/js"

	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/internal/crashrun"
)

// startCrash begins the run's crash record (ryui/crash), if the app
// reports crashes: the page's crash.js keeps what Go's runtime prints of
// a crash and writes it to the run's .crash file as the program ends,
// and writes an uncaught JavaScript error (the host's, or the app's
// web/*.js) to the inbox as an exception, telling Go (crashNotify) so
// OnReport hears of it at once.
func startCrash(h *Host) {
	c := h.js.Get("crash")
	if c.Type() != js.TypeObject {
		return
	}
	i := crashrun.Info{System: "Web", AppVersion: boot.Version, Build: boot.Build}
	if v := c.Call("info"); v.Type() == js.TypeObject {
		i.Model = str(v.Get("model"))
		i.SystemVersion = str(v.Get("systemVersion"))
	}
	crashrun.Start(i)
}

func str(v js.Value) string {
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}

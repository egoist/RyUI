//go:build linux && !android

package linux

import (
	"os"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/crashrun"
)

// startCrash begins the run's crash record (asuka/sdk/crash), if the app
// reports crashes: Go's crash output, which has a native crash too (a
// signal in GTK's code, an abort of GLib's: the runtime's handler prints
// it, with every goroutine, before the process ends). Before GTK loads,
// so a crash as it does is kept.
//
// No handler of the app's own for those signals: Go's runtime has them,
// and replacing its handlers would break Go's own. The system's record
// (systemd-coredump, apport) is kept as ever.
func startCrash() {
	crashrun.Start(linuxInfo())
}

// linuxInfo is the computer and the app: its model (the firmware's
// product name), the distribution and its version (the host's, in a
// Flatpak), asuka.json's version.
func linuxInfo() crashrun.Info {
	i := crashrun.Info{System: "Linux", AppVersion: boot.Version, Build: boot.Build}
	if b, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
		i.Model = strings.TrimSpace(string(b))
	}
	if v := osVersion(); v != "linux" {
		i.SystemVersion = v
	}
	return i
}

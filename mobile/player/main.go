//go:build ios || android

// Command player is RyUI's player app's library: in place of an app, it
// connects to the app's Go code running under `ryui dev` on the
// development machine, and shows its views (see internal/remote).
package main

import (
	"os"
	"strings"

	"github.com/egoist/ryui/internal/remote"
	"github.com/egoist/ryui/mobile"
)

// DefaultAddr is where the player finds `ryui dev`: on the simulator, the
// Mac's loopback; on Android, `adb reverse` maps it to the Mac.
const DefaultAddr = "127.0.0.1:7461"

// configuredAddr is the address ryui dev gave the player: the
// environment on iOS (simctl passes it), a file in the app's private
// files on Android, where apps don't get one.
func configuredAddr() string {
	if a := os.Getenv("RYUI_REMOTE"); a != "" {
		return a
	}
	if cmd, err := os.ReadFile("/proc/self/cmdline"); err == nil {
		pkg := strings.TrimRight(strings.SplitN(string(cmd), "\x00", 2)[0], "\x00")
		if b, err := os.ReadFile("/data/data/" + pkg + "/files/ryui/remote"); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return DefaultAddr
}

func init() {
	mobile.Main = func() {
		addr := configuredAddr()
		x := remote.NewProxy(mobile.Host())
		// Android reports the environment before starting the core.
		if e := mobile.Host().Environment(); e.Width > 0 {
			x.SetEnvironment(e)
		}
		mobile.SetCore(x)
		go x.Connect(addr)
	}
}

func main() {}

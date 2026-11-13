//go:build ios || android

// Command player is Asuka's player app's library: in place of an app, it
// connects to the app's Go code running under `asuka dev` on the
// development machine, and shows its views (see internal/remote).
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/internal/remote"
	"github.com/egoist/asuka/mobile"
	"github.com/egoist/asuka/mobile/shareddir"
)

// DefaultAddr is where the player finds `asuka dev`: on the simulator, the
// Mac's loopback; on Android, `adb reverse` maps it to the Mac.
const DefaultAddr = "127.0.0.1:7461"

// configuredAddr is the address asuka dev gave the player: the
// environment on iOS (simctl passes it), a file in the app's private
// files on Android, where apps don't get one.
//
// On iOS the player's share extension (its share sheet) has no
// environment from simctl: the player leaves the address in their App
// Group for it.
func configuredAddr() string {
	shared := ""
	if d := shareddir.Dir(); d != "" {
		shared = filepath.Join(d, "asuka", "remote")
	}
	if a := os.Getenv("ASUKA_REMOTE"); a != "" {
		if shared != "" {
			os.MkdirAll(filepath.Dir(shared), 0o700)
			os.WriteFile(shared, []byte(a), 0o600)
		}
		return a
	}
	if shared != "" {
		if b, err := os.ReadFile(shared); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	if cmd, err := os.ReadFile("/proc/self/cmdline"); err == nil {
		pkg := strings.TrimRight(strings.SplitN(string(cmd), "\x00", 2)[0], "\x00")
		pkg, _, _ = strings.Cut(pkg, ":") // its share sheet's process, "pkg:share"
		if b, err := os.ReadFile("/data/data/" + pkg + "/files/asuka/remote"); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return DefaultAddr
}

func init() {
	mobile.Main = func() {
		addr := configuredAddr()
		x := remote.NewProxy(mobile.Host())
		// A share sheet's process (the player's share extension, its
		// share activity): its session shows the app's sheet.
		x.Sheet = os.Getenv("ASUKA_SHARE_SHEET") == "1"
		// Android reports the environment before starting the core.
		if e := mobile.Host().Environment(); e.Width > 0 {
			x.SetEnvironment(e)
		}
		mobile.SetCore(x)
		go x.Connect(addr)
	}
}

func main() {}

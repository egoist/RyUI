//go:build darwin && !ios

package darwin

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/asuka/internal/crashrun"
	. "github.com/egoist/asuka/internal/darwin"
)

// startCrash begins the run's crash record (asuka/sdk/crash), if the app
// reports crashes, and subscribes to MetricKit's diagnostics (macOS 12
// and later): its native crashes, hangs, and CPU and disk exceptions,
// delivered on a later launch, which asuka/sdk/crash reads into reports.
//
// No handler of uncaught Objective-C exceptions: AppKit catches those on
// the main thread itself (and logs them), and MetricKit reports the ones
// that end the app.
func startCrash() {
	dir := crashrun.Start(macInfo())
	if dir == "" {
		return
	}
	if Dlopen("/System/Library/Frameworks/MetricKit.framework/MetricKit") == 0 || !HasClass("MXMetricManager") {
		return
	}
	inbox := filepath.Join(dir, "inbox")
	cls := DefineClass("AsukaMetricSubscriber", "NSObject", []string{"MXMetricManagerSubscriber"}, []objc.MethodDef{
		// On a queue of MetricKit's.
		Method("didReceiveDiagnosticPayloads:", func(self ID, _ SEL, payloads ID) {
			WithPool(func() {
				for _, p := range ArrayItems(payloads) {
					writeInbox(inbox, "metrickit-", Bytes(Send(p, "JSONRepresentation")))
				}
			})
			crashrun.Notify()
		}),
	})
	sub := Send(Send(cls, "alloc"), "init") // kept for the process's life
	Send(Send(Class("MXMetricManager"), "sharedManager"), "addSubscriber:", uintptr(sub))
}

// writeInbox writes b as a new file of the inbox, whole or not at all.
func writeInbox(dir, prefix string, b []byte) {
	if len(b) == 0 {
		return
	}
	var id [16]byte
	rand.Read(id[:])
	path := filepath.Join(dir, prefix+hex.EncodeToString(id[:])+".json")
	os.MkdirAll(dir, 0o700)
	if os.WriteFile(path+".tmp", b, 0o600) == nil {
		os.Rename(path+".tmp", path)
	}
}

// macInfo is the Mac and the app, for crash reports: the model's
// identifier (MacBookPro18,3), macOS's version, the bundle's.
func macInfo() crashrun.Info {
	i := crashrun.Info{System: "macOS"}
	i.Model, _ = syscall.Sysctl("hw.model")
	i.SystemVersion, _ = syscall.Sysctl("kern.osproductversion")
	if i.SystemVersion == "" {
		v := strings.TrimPrefix(GoString(Send(Send(Class("NSProcessInfo"), "processInfo"), "operatingSystemVersionString")), "Version ")
		i.SystemVersion, _, _ = strings.Cut(v, " (")
	}
	info := Send(Send(Class("NSBundle"), "mainBundle"), "infoDictionary")
	if info != 0 {
		if v := Send(info, "objectForKey:", uintptr(String("CFBundleShortVersionString"))); v != 0 {
			i.AppVersion = GoString(v)
		}
		if v := Send(info, "objectForKey:", uintptr(String("CFBundleVersion"))); v != 0 {
			i.Build = GoString(v)
		}
	}
	return i
}

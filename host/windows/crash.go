//go:build windows

package windows

import (
	"strconv"
	"syscall"
	"unsafe"

	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/internal/crashrun"
	. "github.com/egoist/ryui/internal/windows"
)

// startCrash begins the run's crash record (ryui/crash), if the app
// reports crashes: Go's crash output, and with no console what the
// runtime prints to standard error, where it reports a Windows exception
// it can't handle (crashrun).
//
// No unhandled exception filter of the app's own: Go's runtime has the
// process's last exception handler (a vectored continue handler), which
// prints the exception and the goroutines and exits before a filter
// (SetUnhandledExceptionFilter) would run, and replacing it would break
// Go's own. Windows Error Reporting keeps its record as ever.
func startCrash() {
	crashrun.Start(windowsInfo())
}

// windowsInfo is the PC and the app: its model (the firmware's product
// name), Windows's version and build, ryui.json's version.
func windowsInfo() crashrun.Info {
	return crashrun.Info{
		Model:         registryString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName"),
		System:        "Windows",
		SystemVersion: "10.0." + strconv.FormatUint(uint64(Build()), 10),
		AppVersion:    boot.Version,
		Build:         boot.Build,
	}
}

// registryString reads a string value under HKEY_LOCAL_MACHINE, "" if
// there's none.
func registryString(key, name string) string {
	var k syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, syscall.StringToUTF16Ptr(key), 0, syscall.KEY_READ, &k) != nil {
		return ""
	}
	defer syscall.RegCloseKey(k)
	var typ, n uint32
	p := syscall.StringToUTF16Ptr(name)
	if syscall.RegQueryValueEx(k, p, nil, &typ, nil, &n) != nil || typ != syscall.REG_SZ || n < 2 {
		return ""
	}
	buf := make([]uint16, n/2)
	if syscall.RegQueryValueEx(k, p, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

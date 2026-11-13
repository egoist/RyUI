//go:build windows

package windows

import (
	"sort"
	"syscall"
	"unsafe"
)

// The monitors (asuka/sdk/screen's displays): their rectangles in pixels,
// work areas, DPIs, names and refresh rates.

var (
	shcore = systemDLL("shcore.dll")

	procEnumDisplayMonitors         = user32.NewProc("EnumDisplayMonitors")
	procEnumDisplayDevicesW         = user32.NewProc("EnumDisplayDevicesW")
	procEnumDisplaySettingsW        = user32.NewProc("EnumDisplaySettingsW")
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
	procGetDpiForMonitor            = shcore.NewProc("GetDpiForMonitor")
)

// Monitor is a monitor attached to the desktop.
type Monitor struct {
	Device  string // its GDI name: \\.\DISPLAY1
	Name    string // its friendly name: "DELL U2720Q"; "" if none
	Bounds  Rect   // in pixels, on the virtual screen
	Work    Rect   // its work area, less the taskbar
	DPI     int    // its effective DPI (96: 100%)
	Hz      float32
	Primary bool
}

// monitorInfoEx is MONITORINFOEXW.
type monitorInfoEx struct {
	MonitorInfo
	Device [32]uint16
}

const monitorInfoPrimary = 1

var (
	enumMonitorsProc uintptr
	enumMonitorsTo   *[]uintptr
)

// Monitors lists the monitors attached to the desktop, the primary one
// first, the others by their GDI names. Main thread.
func Monitors() []Monitor {
	if enumMonitorsProc == 0 {
		enumMonitorsProc = syscall.NewCallback(func(m, dc, r, l uintptr) uintptr {
			*enumMonitorsTo = append(*enumMonitorsTo, m)
			return 1
		})
	}
	var handles []uintptr
	enumMonitorsTo = &handles
	procEnumDisplayMonitors.Call(0, 0, enumMonitorsProc, 0)
	enumMonitorsTo = nil
	paths := displayPaths()
	out := make([]Monitor, 0, len(handles))
	for _, h := range handles {
		var mi monitorInfoEx
		mi.Size = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi))); r == 0 {
			continue
		}
		m := Monitor{
			Device:  syscall.UTF16ToString(mi.Device[:]),
			Bounds:  mi.Monitor,
			Work:    mi.Work,
			DPI:     monitorDPI(h),
			Primary: mi.Flags&monitorInfoPrimary != 0,
		}
		p, ok := paths[m.Device]
		m.Name, m.Hz = p.name, p.hz
		if m.Name == "" {
			if ok && p.internal {
				m.Name = "Built-in Display"
			} else {
				m.Name = adapterMonitorName(m.Device)
			}
		}
		if m.Hz == 0 {
			m.Hz = displayFrequency(m.Device)
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		return out[i].Device < out[j].Device
	})
	return out
}

// monitorDPI is a monitor's effective DPI (MDT_EFFECTIVE_DPI), the
// system's before Windows 8.1.
func monitorDPI(h uintptr) int {
	if Has(procGetDpiForMonitor) {
		var x, y uint32
		if r, _, _ := procGetDpiForMonitor.Call(h, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r == 0 && x != 0 {
			return int(x)
		}
	}
	return DPI(0)
}

// displayPath is what the display configuration says of a source's
// target: its friendly name (EDID's), whether it's the built-in panel,
// its refresh rate.
type displayPath struct {
	name     string
	internal bool
	hz       float32
}

const (
	qdcOnlyActivePaths       = 2
	dcGetSourceName          = 1
	dcGetTargetName          = 2
	pathInfoSize             = 72
	modeInfoSize             = 64
	outputTechnologyInternal = 0x80000000 // DISPLAYCONFIG_OUTPUT_TECHNOLOGY_INTERNAL
)

// displayPaths reads the active display paths (QueryDisplayConfig), by
// their sources' GDI names. Windows 7 and later; none where it fails.
func displayPaths() map[string]displayPath {
	out := map[string]displayPath{}
	if !Has(procQueryDisplayConfig) {
		return out
	}
	var np, nm uint32
	var paths, modes []byte
	for range 3 { // the configuration can change between the calls
		if r, _, _ := procGetDisplayConfigBufferSizes.Call(qdcOnlyActivePaths, uintptr(unsafe.Pointer(&np)), uintptr(unsafe.Pointer(&nm))); r != 0 || np == 0 {
			return out
		}
		paths = make([]byte, np*pathInfoSize)
		modes = make([]byte, max(nm, 1)*modeInfoSize)
		r, _, _ := procQueryDisplayConfig.Call(qdcOnlyActivePaths, uintptr(unsafe.Pointer(&np)), uintptr(unsafe.Pointer(&paths[0])),
			uintptr(unsafe.Pointer(&nm)), uintptr(unsafe.Pointer(&modes[0])), 0)
		if r == 0 {
			break
		}
		if r != 122 { // ERROR_INSUFFICIENT_BUFFER
			return out
		}
		np = 0
	}
	le := func(b []byte, off int) uint32 {
		return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
	}
	for i := range int(np) {
		p := paths[i*pathInfoSize : (i+1)*pathInfoSize]
		// DISPLAYCONFIG_PATH_INFO: the source (adapter LUID, ID) at 0, the
		// target (adapter LUID, ID, ..., output technology at 36, refresh
		// rate's numerator and denominator at 48) at 20.
		var src [21]uint32 // DISPLAYCONFIG_SOURCE_DEVICE_NAME: 84 bytes
		src[0], src[1], src[2], src[3], src[4] = dcGetSourceName, 84, le(p, 0), le(p, 4), le(p, 8)
		if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&src[0]))); r != 0 {
			continue
		}
		gdi := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&src[5])), 32))
		var tgt [105]uint32 // DISPLAYCONFIG_TARGET_DEVICE_NAME: 420 bytes
		tgt[0], tgt[1], tgt[2], tgt[3], tgt[4] = dcGetTargetName, 420, le(p, 20), le(p, 24), le(p, 28)
		var dp displayPath
		if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&tgt[0]))); r == 0 {
			// monitorFriendlyDeviceName at 36, 64 characters.
			dp.name = syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&tgt[9])), 64))
		}
		dp.internal = le(p, 36) == outputTechnologyInternal
		if num, den := le(p, 48), le(p, 52); den != 0 {
			dp.hz = float32(int(float64(num)/float64(den)*100+0.5)) / 100
		}
		if _, seen := out[gdi]; !seen { // a clone's second target: the first's
			out[gdi] = dp
		}
	}
	return out
}

// adapterMonitorName is the monitor's description from its adapter's
// devices ("Generic PnP Monitor"), where the configuration has no name.
func adapterMonitorName(device string) string {
	var dd [210]uint32 // DISPLAY_DEVICEW: 840 bytes
	dd[0] = 840
	if r, _, _ := procEnumDisplayDevicesW.Call(Str(device), 0, uintptr(unsafe.Pointer(&dd[0])), 0); r == 0 {
		return ""
	}
	// DeviceString: 128 characters after cb and DeviceName's 32.
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(&dd[0]), 68)), 128))
}

// displayFrequency is the device's current mode's refresh rate (0: the
// hardware's default, or not known).
func displayFrequency(device string) float32 {
	var dm [55]uint32                                                                                                // DEVMODEW: 220 bytes
	*(*uint16)(unsafe.Add(unsafe.Pointer(&dm[0]), 68)) = 220                                                         // dmSize
	if r, _, _ := procEnumDisplaySettingsW.Call(Str(device), ^uintptr(0), uintptr(unsafe.Pointer(&dm[0]))); r == 0 { // ENUM_CURRENT_SETTINGS
		return 0
	}
	hz := dm[184/4] // dmDisplayFrequency
	if hz <= 1 {
		return 0
	}
	return float32(hz)
}

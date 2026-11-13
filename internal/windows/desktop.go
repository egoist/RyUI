//go:build windows

package windows

import "unsafe"

// What asuka/power, asuka/appearance and asuka/dock read and ask: the
// battery and the power source, the user's idle time, the notifications
// of sleep, power changes and the session locking; the accent and the
// accessibility settings; asking for the user's attention.

var (
	wtsapi32 = systemDLL("wtsapi32.dll")

	procGetTickCount                      = kernel32.NewProc("GetTickCount")
	procGetLastInputInfo                  = user32.NewProc("GetLastInputInfo")
	procRegisterPowerSettingNotification  = user32.NewProc("RegisterPowerSettingNotification")
	procRegisterSuspendResumeNotification = user32.NewProc("RegisterSuspendResumeNotification")
	procWTSRegisterSessionNotification    = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procFlashWindowEx                     = user32.NewProc("FlashWindowEx")
	procDwmGetColorizationColor           = dwmapi.NewProc("DwmGetColorizationColor")
	procGetCurrentProcessId               = kernel32.NewProc("GetCurrentProcessId")
)

// Messages and their codes.
const (
	WM_SYSCOLORCHANGE              = 0x0015
	WM_DISPLAYCHANGE               = 0x007E
	WM_POWERBROADCAST              = 0x0218
	WM_WTSSESSION_CHANGE           = 0x02B1
	WM_DWMCOLORIZATIONCOLORCHANGED = 0x0320

	PBT_APMSUSPEND           = 0x4
	PBT_APMRESUMESUSPEND     = 0x7
	PBT_APMPOWERSTATUSCHANGE = 0xA
	PBT_APMRESUMEAUTOMATIC   = 0x12
	PBT_POWERSETTINGCHANGE   = 0x8013

	WTS_SESSION_LOCK   = 0x7
	WTS_SESSION_UNLOCK = 0x8
)

// PowerStatus is SYSTEM_POWER_STATUS.
type PowerStatus struct {
	ACLine          uint8 // 0 offline (on the battery), 1 online, 255 unknown
	BatteryFlag     uint8 // 8 charging, 128 no battery, 255 unknown
	BatteryPercent  uint8 // 0 to 100, 255 unknown
	SystemStatus    uint8 // 1: battery saver (energy saver) on
	BatteryLifeTime uint32
	BatteryFullTime uint32
}

// SystemPowerStatus is the power source and the battery now.
func SystemPowerStatus() (PowerStatus, bool) {
	var s PowerStatus
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s)))
	return s, r != 0
}

// IdleMillis is how long since the user's last input in the session
// (keyboard, mouse, touch, pen), in milliseconds; ok false if Windows
// doesn't say.
func IdleMillis() (ms uint32, ok bool) {
	in := struct{ Size, Time uint32 }{Size: 8}
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&in))); r == 0 {
		return 0, false
	}
	now, _, _ := procGetTickCount.Call()
	return uint32(now) - in.Time, true // both wrap at 49.7 days
}

var (
	// GUID_ACDC_POWER_SOURCE, GUID_BATTERY_PERCENTAGE_REMAINING,
	// GUID_POWER_SAVING_STATUS (battery saver) and
	// GUID_ENERGY_SAVER_STATUS (Windows 11's energy saver).
	powerSettings = []GUID{
		NewGUID("5D3E9A59-E9D5-4B00-A6BD-FF34FF516548"),
		NewGUID("A7AD8041-B45A-4CAE-87A3-EECBB468A9E1"),
		NewGUID("E00958C0-C213-4ACE-AC77-FECCED2EEEA5"),
		NewGUID("550E8400-E29B-41D4-A716-446655440000"),
	}
)

// WatchPower has hwnd sent WM_POWERBROADCAST for the power source, the
// battery's level and battery or energy saver changing
// (PBT_POWERSETTINGCHANGE), and for sleeping and waking on systems with
// modern standby too (RegisterSuspendResumeNotification); and
// WM_WTSSESSION_CHANGE as the session locks and unlocks.
func WatchPower(hwnd uintptr) {
	for i := range powerSettings {
		procRegisterPowerSettingNotification.Call(hwnd, uintptr(unsafe.Pointer(&powerSettings[i])), 0) // DEVICE_NOTIFY_WINDOW_HANDLE
	}
	if Has(procRegisterSuspendResumeNotification) {
		procRegisterSuspendResumeNotification.Call(hwnd, 0)
	}
	if Has(procWTSRegisterSessionNotification) {
		procWTSRegisterSessionNotification.Call(hwnd, 0) // NOTIFY_FOR_THIS_SESSION
	}
}

// HighContrast reports whether a contrast theme is on (HCF_HIGHCONTRASTON).
func HighContrast() bool {
	hc := struct {
		Size, Flags uint32
		Scheme      uintptr
	}{}
	hc.Size = uint32(unsafe.Sizeof(hc))
	if r, _, _ := procSystemParametersInfoW.Call(0x42, uintptr(hc.Size), uintptr(unsafe.Pointer(&hc)), 0); r == 0 { // SPI_GETHIGHCONTRAST
		return false
	}
	return hc.Flags&1 != 0
}

// TransparencyOff reports whether the user turned transparency effects
// off (Settings, Personalization, Colors).
func TransparencyOff() bool {
	v, ok := RegDWORD(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "EnableTransparency")
	return ok && v == 0
}

// AccentColor is the system's accent color (Settings, Personalization,
// Colors) as 0x00BBGGRR: the accent palette's, else DWM's colorization
// color.
func AccentColor() (uint32, bool) {
	if p, ok := AccentPalette(); ok {
		return p[3], true
	}
	if Has(procDwmGetColorizationColor) {
		var c uint32
		var opaque int32
		if hr, _, _ := procDwmGetColorizationColor.Call(uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&opaque))); !Failed(hr) {
			// 0xAARRGGBB.
			return c>>16&0xff | c&0xff00 | (c&0xff)<<16, true
		}
	}
	return 0, false
}

// flashInfo is FLASHWINFO.
type flashInfo struct {
	Size    uint32
	Hwnd    uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

// FlashWindowEx's flags.
const (
	FLASHW_STOP      = 0
	FLASHW_CAPTION   = 0x1
	FLASHW_TRAY      = 0x2
	FLASHW_ALL       = 0x3
	FLASHW_TIMERNOFG = 0xC
)

// FlashWindow flashes a window's taskbar button (and title bar) as flags
// say, count times (0: until FLASHW_TIMERNOFG's foreground or a stop).
func FlashWindow(hwnd uintptr, flags, count uint32) {
	fi := flashInfo{Hwnd: hwnd, Flags: flags, Count: count}
	fi.Size = uint32(unsafe.Sizeof(fi))
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

// ForegroundIsOurs reports whether the foreground window is this
// process's: the app is the one in use.
func ForegroundIsOurs() bool {
	fg := ForegroundWindow()
	if fg == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&pid)))
	me, _, _ := procGetCurrentProcessId.Call()
	return pid == uint32(me)
}

//go:build windows

package windows

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"unsafe"
)

// The app's badge: for an app with a package identity, its tile's and
// taskbar button's badge (BadgeUpdateManager); for one without, which
// has no badge of the system's, an overlay icon on its taskbar button
// (ITaskbarList3::SetOverlayIcon) with the count drawn on it, as Teams
// and Outlook do.

var (
	clsidTaskbarList       = NewGUID("56FDF344-FD6D-11D0-958A-006097C9A090")
	iidTaskbarList3        = NewGUID("EA1AFB91-9E28-4B86-90E9-9E9F8A5EEFAF")
	iidBadgeManagerStatics = NewGUID("33400FAA-6DD5-4105-AEBC-9B50FCA492DA")
	iidBadgeFactory        = NewGUID("EDF255CE-0618-4D59-948A-5A61040C52F9")

	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procRegisterWindowMsg  = user32.NewProc("RegisterWindowMessageW")
	procCurrentPackageName = kernel32.NewProc("GetCurrentPackageFullName")

	taskbarList uintptr
)

// TaskbarButtonCreated is the message a window gets once its taskbar
// button is made (again, when Explorer restarts): its overlay is set
// again then.
func TaskbarButtonCreated() uint32 {
	r, _, _ := procRegisterWindowMsg.Call(Str("TaskbarButtonCreated"))
	return uint32(r)
}

// Packaged reports whether the process has a package identity (MSIX).
func Packaged() bool {
	if !Has(procCurrentPackageName) {
		return false
	}
	var n uint32
	r, _, _ := procCurrentPackageName.Call(uintptr(unsafe.Pointer(&n)), 0)
	return r != 15700 // APPMODEL_ERROR_NO_PACKAGE
}

// SetPackageBadge sets a packaged app's badge to n (0: none).
func SetPackageBadge(n int) error {
	st, err := ActivationFactory("Windows.UI.Notifications.BadgeUpdateManager", iidBadgeManagerStatics)
	if err != nil {
		return err
	}
	defer Release(st)
	var up uintptr
	if hr := ComCall(st, 6, uintptr(unsafe.Pointer(&up))); Failed(hr) || up == 0 { // CreateBadgeUpdaterForApplication
		return fmt.Errorf("badge: %#x", uint32(hr))
	}
	defer Release(up)
	if n <= 0 {
		ComCall(up, 7) // Clear
		return nil
	}
	doc, err := xmlDocument(`<badge value="` + strconv.Itoa(n) + `"/>`)
	if err != nil {
		return err
	}
	defer Release(doc)
	f, err := ActivationFactory("Windows.UI.Notifications.BadgeNotification", iidBadgeFactory)
	if err != nil {
		return err
	}
	defer Release(f)
	var b uintptr
	if hr := ComCall(f, 6, doc, uintptr(unsafe.Pointer(&b))); Failed(hr) || b == 0 { // CreateBadgeNotification
		return fmt.Errorf("badge: %#x", uint32(hr))
	}
	defer Release(b)
	if hr := ComCall(up, 6, b); Failed(hr) { // Update
		return fmt.Errorf("badge: %#x", uint32(hr))
	}
	return nil
}

// SetTaskbarBadge shows n on hwnd's taskbar button, an overlay icon for
// a display of dpi (0: none); label is what screen readers say of it.
func SetTaskbarBadge(hwnd uintptr, n, dpi int, label string) error {
	if err := initTaskbar(); err != nil {
		return err
	}
	var icon uintptr
	if n > 0 {
		icon = BadgeIcon(n, int32(16*max(dpi, 96)/96))
		defer procDestroyIcon.Call(icon)
	}
	if hr := ComCall(taskbarList, 18, hwnd, icon, Str(label)); Failed(hr) { // SetOverlayIcon
		return fmt.Errorf("the taskbar's overlay: %#x", uint32(hr))
	}
	return nil
}

// initTaskbar makes the taskbar list, once.
func initTaskbar() error {
	if taskbarList == 0 {
		taskbarList = CoCreate(&clsidTaskbarList, &iidTaskbarList3)
		if taskbarList == 0 {
			return errors.New("no taskbar")
		}
		ComCall(taskbarList, 3) // HrInit
	}
	return nil
}

// Taskbar progress states (TBPFLAG).
const (
	TBPF_NOPROGRESS    = 0
	TBPF_INDETERMINATE = 0x1
	TBPF_NORMAL        = 0x2
	TBPF_ERROR         = 0x4
	TBPF_PAUSED        = 0x8
)

// SetTaskbarProgress shows a progress on hwnd's taskbar button: state a
// TBPF_ flag, value 0 to 1 (but for no progress and an indeterminate
// one).
func SetTaskbarProgress(hwnd uintptr, state uint32, value float64) error {
	if err := initTaskbar(); err != nil {
		return err
	}
	if hr := ComCall(taskbarList, 10, hwnd, uintptr(state)); Failed(hr) { // SetProgressState
		return fmt.Errorf("the taskbar's progress: %#x", uint32(hr))
	}
	if state != TBPF_NOPROGRESS && state != TBPF_INDETERMINATE {
		const total = 10000
		done := uint64(math.Round(min(max(value, 0), 1) * total))
		if hr := ComCall(taskbarList, 9, hwnd, uintptr(done), total); Failed(hr) { // SetProgressValue
			return fmt.Errorf("the taskbar's progress: %#x", uint32(hr))
		}
	}
	return nil
}

// BadgeText is how a badge writes n: "99+" past 99.
func BadgeText(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// BadgeIcon draws n, white on Windows's red, in a circle size pixels
// across (a rounded bar for more digits): an icon the caller destroys.
func BadgeIcon(n int, size int32) uintptr {
	px := BadgePixels(n, size)
	bmp, bits := DIB(size, size)
	if bmp == 0 {
		return 0
	}
	defer DeleteObject(bmp)
	copy(bits, px)
	mask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	defer DeleteObject(mask)
	info := struct {
		Icon        int32
		X, Y        uint32
		Mask, Color uintptr
	}{Icon: 1, Mask: mask, Color: bmp}
	r, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return r
}

// BadgePixels is BadgeIcon's picture: size×size BGRA pixels, not
// premultiplied, top row first.
func BadgePixels(n int, size int32) []byte {
	text := UTF16Units(BadgeText(n))
	s := int(size)
	// The text's coverage: white on black, antialiased, in GDI.
	dc := CreateCompatibleDC(0)
	defer DeleteDC(dc)
	tb, tpx := DIB(size, size)
	if tb == 0 {
		return make([]byte, s*s*4)
	}
	defer DeleteObject(tb)
	old := SelectObject(dc, tb)
	h := int32(math.Round(float64(size) * 0.78))
	if len(text) > 1 {
		h = int32(math.Round(float64(size) * 0.66))
	}
	if len(text) > 2 {
		h = int32(math.Round(float64(size) * 0.52))
	}
	lf := LogFont{Height: -h, Weight: 700, Quality: 4} // ANTIALIASED_QUALITY: gray, not ClearType's colors
	copy(lf.FaceName[:], UTF16Units("Segoe UI"))
	font := CreateFont(&lf)
	oldFont := SelectObject(dc, font)
	SetBkMode(dc, 1) // TRANSPARENT
	SetTextColor(dc, RGB(255, 255, 255))
	r := Rect{Right: size, Bottom: size}
	DrawText(dc, text, &r, 0x25) // DT_CENTER | DT_VCENTER | DT_SINGLELINE
	SelectObject(dc, oldFont)
	DeleteObject(font)
	SelectObject(dc, old)

	// The shape: a circle, or a pill as wide as the image for more digits
	// (the image is square: the pill is as tall as the text needs).
	out := make([]byte, s*s*4)
	cx, cy, rad := float64(s)/2, float64(s)/2, float64(s)/2
	halfW := 0.0
	if len(text) > 1 {
		rad = float64(s) * 0.36
		halfW = float64(s)/2 - rad
	}
	const red, green, blue = 0xC4, 0x2B, 0x1C // Windows 11's critical red
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			dx := math.Max(math.Abs(px-cx)-halfW, 0)
			d := math.Hypot(dx, py-cy)
			a := math.Min(math.Max(rad-d+0.5, 0), 1)
			if a == 0 {
				continue
			}
			i := (y*s + x) * 4
			t := float64(tpx[i+1]) / 255 // the text's coverage
			out[i] = byte(math.Round(blue + (255-blue)*t))
			out[i+1] = byte(math.Round(green + (255-green)*t))
			out[i+2] = byte(math.Round(red + (255-red)*t))
			out[i+3] = byte(math.Round(a * 255))
		}
	}
	return out
}

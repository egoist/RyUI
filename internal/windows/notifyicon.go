//go:build windows

package windows

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// The notification area's icons (Shell_NotifyIcon), icons drawn from
// pixels, and the app's jump list's tasks (ICustomDestinationList).

var (
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procExcludeClipRect  = gdi32.NewProc("ExcludeClipRect")
)

// Shell_NotifyIcon's messages and NOTIFYICONDATA's flags.
const (
	NIM_ADD        = 0
	NIM_MODIFY     = 1
	NIM_DELETE     = 2
	NIM_SETVERSION = 4

	NIF_MESSAGE   = 0x01
	NIF_ICON      = 0x02
	NIF_TIP       = 0x04
	NIF_SHOWTIP   = 0x80
	NIN_SELECT    = 0x0400 // WM_USER
	NIN_KEYSELECT = 0x0401

	NOTIFYICON_VERSION_4 = 4

	WM_NULL = 0x0000

	// The submenu arrow's states (vsstyle.h).
	MSM_NORMAL   = 1
	MSM_DISABLED = 2

	TPM_RIGHTALIGN  = 0x0008
	TPM_BOTTOMALIGN = 0x0020
)

// notifyIconData is NOTIFYICONDATAW.
type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        GUID
	BalloonIcon     uintptr
}

// NotifyIcon is an icon in the notification area: the window that gets
// its messages, its ID there, the message its events come as, its icon
// and tooltip.
type NotifyIcon struct {
	Wnd     uintptr
	ID      uint32
	Message uint32
	Icon    uintptr
	Tip     string
}

func (n *NotifyIcon) data() *notifyIconData {
	d := &notifyIconData{Wnd: n.Wnd, ID: n.ID, Flags: NIF_MESSAGE | NIF_ICON | NIF_TIP | NIF_SHOWTIP, CallbackMessage: n.Message, Icon: n.Icon}
	d.Size = uint32(unsafe.Sizeof(*d))
	tip := UTF16Units(n.Tip)
	if len(tip) > len(d.Tip)-1 {
		tip = tip[:len(d.Tip)-1]
	}
	copy(d.Tip[:], tip)
	return d
}

// Add puts the icon in the notification area, its events as
// NOTIFYICON_VERSION_4 sends them: the event in lParam's low word, the
// anchor's screen point in wParam.
func (n *NotifyIcon) Add() bool {
	d := n.data()
	r, _, _ := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(d)))
	if r == 0 {
		return false
	}
	d.Version = NOTIFYICON_VERSION_4
	procShellNotifyIconW.Call(NIM_SETVERSION, uintptr(unsafe.Pointer(d)))
	return true
}

// Modify sets the icon's picture and tooltip.
func (n *NotifyIcon) Modify() bool {
	r, _, _ := procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(n.data())))
	return r != 0
}

// Delete takes the icon out of the notification area.
func (n *NotifyIcon) Delete() {
	d := &notifyIconData{Wnd: n.Wnd, ID: n.ID}
	d.Size = uint32(unsafe.Sizeof(*d))
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(d)))
}

// RegisterWindowMessage is a message's number by its name, the same in
// every process.
func RegisterWindowMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMsg.Call(Str(name))
	return uint32(r)
}

// SystemUsesLightTheme reports whether the taskbar and Start are light
// (Personalization's "Choose your default Windows mode").
func SystemUsesLightTheme() bool {
	v, ok := RegDWORD(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "SystemUsesLightTheme")
	return ok && v != 0
}

// ExcludeClipRect takes r out of dc's clip region.
func ExcludeClipRect(dc uintptr, r Rect) {
	procExcludeClipRect.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
}

// IconFromPixels makes an icon of w×h BGRA pixels, not premultiplied,
// top row first: an icon the caller destroys (DestroyIcon).
func IconFromPixels(px []byte, w, h int32) uintptr {
	if len(px) < int(w)*int(h)*4 {
		return 0
	}
	bmp, bits := DIB(w, h)
	if bmp == 0 {
		return 0
	}
	defer DeleteObject(bmp)
	copy(bits, px)
	mask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	defer DeleteObject(mask)
	info := struct {
		Icon        int32
		X, Y        uint32
		Mask, Color uintptr
	}{Icon: 1, Mask: mask, Color: bmp}
	r, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return r
}

// DestroyIcon frees an icon made by IconFromPixels.
func DestroyIcon(icon uintptr) {
	if icon != 0 {
		procDestroyIcon.Call(icon)
	}
}

// GlyphCoverage draws a glyph of the font face, size pixels square and
// centered, antialiased in gray: how much of each pixel it covers (0 to
// 255), top row first.
func GlyphCoverage(glyph uint16, face string, size int32) []byte {
	out := make([]byte, int(size)*int(size))
	dc := CreateCompatibleDC(0)
	if dc == 0 {
		return out
	}
	defer DeleteDC(dc)
	bmp, px := DIB(size, size)
	if bmp == 0 {
		return out
	}
	defer DeleteObject(bmp)
	old := SelectObject(dc, bmp)
	lf := LogFont{Height: -size, Weight: 400, Quality: 4} // ANTIALIASED_QUALITY: gray, not ClearType's colors
	copy(lf.FaceName[:31], UTF16Units(face))
	font := CreateFont(&lf)
	oldFont := SelectObject(dc, font)
	SetBkMode(dc, 1) // TRANSPARENT
	SetTextColor(dc, RGB(255, 255, 255))
	r := Rect{Right: size, Bottom: size}
	DrawText(dc, []uint16{glyph}, &r, 0x25|0x800) // DT_CENTER | DT_VCENTER | DT_SINGLELINE | DT_NOPREFIX
	SelectObject(dc, oldFont)
	DeleteObject(font)
	SelectObject(dc, old)
	for i := range out {
		out[i] = px[i*4+1]
	}
	return out
}

// JumpTask is a task of the app's jump list: a link that starts Path
// with Args, titled Title; a Separator is a line between tasks.
type JumpTask struct {
	Title, Path, Args string
	Separator         bool
}

var (
	clsidDestinationList     = NewGUID("77F10CF0-3DB5-4966-B520-B7C54FD35ED6")
	iidCustomDestinationList = NewGUID("6332DEBF-87B5-4670-90C0-5E57B408A49E")
	clsidObjectCollection    = NewGUID("2D3468C1-36A7-43B6-AC24-D3F02FD9607A") // EnumerableObjectCollection
	iidObjectCollection      = NewGUID("5632B1A4-E38A-400A-928A-D4CD63230295")
	iidObjectArray           = NewGUID("92CA9DCD-5622-4BBA-A805-5E9F541BD8C9")
	pkeyTitleFmt             = NewGUID("F29F85E0-4FF9-1068-AB91-08002B27B3D9")
)

// SetJumpListTasks replaces the Tasks of the app's jump list (its
// taskbar button's and Start menu entry's) with tasks; none removes
// them. Main thread (COM initialized).
func SetJumpListTasks(tasks []JumpTask) error {
	dl := CoCreate(&clsidDestinationList, &iidCustomDestinationList)
	if dl == 0 {
		return errors.New("jump list: no ICustomDestinationList")
	}
	defer Release(dl)
	if len(tasks) == 0 {
		ComCall(dl, 10, 0) // DeleteList: the app's own
		return nil
	}
	var slots uint32
	var removed uintptr
	if hr := ComCall(dl, 4, uintptr(unsafe.Pointer(&slots)), uintptr(unsafe.Pointer(&iidObjectArray)), uintptr(unsafe.Pointer(&removed))); Failed(hr) { // BeginList
		return fmt.Errorf("jump list: BeginList %#x", uint32(hr))
	}
	Release(removed) // what the user removed: tasks can't be
	abort := func(err error) error {
		ComCall(dl, 11) // AbortList
		return err
	}
	oc := CoCreate(&clsidObjectCollection, &iidObjectCollection)
	if oc == 0 {
		return abort(errors.New("jump list: no object collection"))
	}
	defer Release(oc)
	for _, t := range tasks {
		link, err := taskLink(t)
		if err != nil {
			return abort(err)
		}
		hr := ComCall(oc, 5, link) // AddObject
		Release(link)              // the collection holds its own
		if Failed(hr) {
			return abort(fmt.Errorf("jump list: AddObject %#x", uint32(hr)))
		}
	}
	if hr := ComCall(dl, 7, oc); Failed(hr) { // AddUserTasks
		return abort(fmt.Errorf("jump list: AddUserTasks %#x", uint32(hr)))
	}
	if hr := ComCall(dl, 8); Failed(hr) { // CommitList
		return abort(fmt.Errorf("jump list: CommitList %#x", uint32(hr)))
	}
	return nil
}

// taskLink is a jump list task's shell link: a separator
// (System.AppUserModel.IsDestListSeparator), or a link titled
// (System.Title) to its program with its arguments and its icon.
func taskLink(t JumpTask) (uintptr, error) {
	link := CoCreate(&clsidShellLink, &iidShellLinkW)
	if link == 0 {
		return 0, errors.New("jump list: no shell links")
	}
	ps := QueryInterface(link, &iidPropertyStore)
	if ps == 0 {
		Release(link)
		return 0, errors.New("jump list: no property store")
	}
	defer Release(ps)
	type propKey struct {
		Fmt GUID
		PID uint32
	}
	type propVariant struct {
		VT       uint16
		_        [3]uint16
		Val, Pad uintptr
	}
	if t.Separator {
		key := propKey{pkeyAppUserModelFmt, 6}
		v := propVariant{VT: 11, Val: 0xFFFF} // VT_BOOL, VARIANT_TRUE
		ComCall(ps, 6, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v)))
	} else {
		ComCall(link, 20, Str(t.Path))    // SetPath
		ComCall(link, 11, Str(t.Args))    // SetArguments: the jump list shows only links with some
		ComCall(link, 17, Str(t.Path), 0) // SetIconLocation: the program's icon
		ComCall(link, 7, Str(t.Title))    // SetDescription: its tooltip
		title := UTF16(t.Title)
		key := propKey{pkeyTitleFmt, 2}
		v := propVariant{VT: 31, Val: uintptr(unsafe.Pointer(title))} // VT_LPWSTR
		if hr := ComCall(ps, 6, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v))); Failed(hr) {
			Release(link)
			return 0, fmt.Errorf("jump list: the title %#x", uint32(hr))
		}
		runtime.KeepAlive(title)
	}
	ComCall(ps, 7) // Commit
	return link, nil
}

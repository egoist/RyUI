//go:build windows

package windows

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// More of the system, for the host's modules: known folders, a window's
// display affinity (kept out of screen captures), the power source, the
// windows of the process as they show, the clipboard's image and its
// changes, a folder's changes, and what keeps the system awake.

var (
	procSHGetKnownFolderPath        = shell32.NewProc("SHGetKnownFolderPath")
	procSetWindowDisplayAffinity    = user32.NewProc("SetWindowDisplayAffinity")
	procGetSystemPowerStatus        = kernel32.NewProc("GetSystemPowerStatus")
	procSetWinEventHook             = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent              = user32.NewProc("UnhookWinEvent")
	procGetClipboardSequenceNumber  = user32.NewProc("GetClipboardSequenceNumber")
	procIsClipboardFormatAvailable  = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData            = user32.NewProc("GetClipboardData")
	procFindFirstChangeNotification = kernel32.NewProc("FindFirstChangeNotificationW")
	procFindNextChangeNotification  = kernel32.NewProc("FindNextChangeNotification")
	procFindCloseChangeNotification = kernel32.NewProc("FindCloseChangeNotification")
	procWaitForMultipleObjects      = kernel32.NewProc("WaitForMultipleObjects")
	procSetEvent                    = kernel32.NewProc("SetEvent")
	procCloseHandle                 = kernel32.NewProc("CloseHandle")
)

// Known folders (KNOWNFOLDERID).
var (
	FolderDownloads   = NewGUID("374DE290-123F-4565-9164-39C4925E467B")
	FolderPictures    = NewGUID("33E28130-4E1E-4676-835A-98395C3BC3BB")
	FolderVideos      = NewGUID("18989B1D-99B5-455B-841C-AB7C74E4DDFC")
	FolderMusic       = NewGUID("4BD8D571-6D19-48D3-BE97-422220080E43")
	FolderScreenshots = NewGUID("B7BEDE81-DF94-4682-A7D8-57A52620B86F")
)

// KnownFolder is a known folder's path (made if it isn't there yet).
func KnownFolder(id GUID) (string, error) {
	var p uintptr
	const kfCreate = 0x00008000
	hr, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&id)), kfCreate, 0, uintptr(unsafe.Pointer(&p)))
	if Failed(hr) || p == 0 {
		return "", syscall.Errno(hr & 0xffff)
	}
	s := GoStr(p)
	procCoTaskMemFree.Call(p)
	return s, nil
}

// ExcludeFromCapture keeps a top-level window out of screenshots and
// screen recordings (WDA_EXCLUDEFROMCAPTURE, Windows 10 2004; before, it
// shows black: WDA_MONITOR), or lets them see it again.
func ExcludeFromCapture(hwnd uintptr, exclude bool) bool {
	if !Has(procSetWindowDisplayAffinity) {
		return false
	}
	a := uintptr(0) // WDA_NONE
	if exclude {
		a = 0x11 // WDA_EXCLUDEFROMCAPTURE
		if Build() < 19041 {
			a = 0x01 // WDA_MONITOR
		}
	}
	r, _, _ := procSetWindowDisplayAffinity.Call(hwnd, a)
	return r != 0
}

// DisplayAffinity is a window's display affinity (0x11: kept out of
// captures).
func DisplayAffinity(hwnd uintptr) uint32 {
	var a uint32
	user32.NewProc("GetWindowDisplayAffinity").Call(hwnd, uintptr(unsafe.Pointer(&a)))
	return a
}

// OnACPower reports whether the PC runs on its power adapter (a desktop
// always does).
func OnACPower() bool {
	var s struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag uint8
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return true
	}
	return s.ACLineStatus != 0 // 1 online, 255 unknown
}

// TopLevel is the top-level window hwnd is in.
func TopLevel(hwnd uintptr) uintptr {
	r, _, _ := procGetAncestor.Call(hwnd, 2) // GA_ROOT
	return r
}

var (
	shownMu    sync.Mutex
	shownHooks = map[uintptr]func(uintptr){}
	shownProc  = sync.OnceValue(func() uintptr {
		return syscall.NewCallback(func(hook, event, hwnd, object, child, thread, t uintptr) uintptr {
			if int32(object) != 0 || int32(child) != 0 { // OBJID_WINDOW, CHILDID_SELF
				return 0
			}
			shownMu.Lock()
			f := shownHooks[hook]
			shownMu.Unlock()
			if f != nil {
				f(hwnd)
			}
			return 0
		})
	})
)

// WatchWindowsShown calls shown with each window of the process as it
// shows (on the thread that called it, from its message loop), until
// the returned function is called.
func WatchWindowsShown(shown func(hwnd uintptr)) (stop func()) {
	const eventObjectShow = 0x8002
	pid := uint32(os.Getpid())
	h, _, _ := procSetWinEventHook.Call(eventObjectShow, eventObjectShow, 0, shownProc(), uintptr(pid), 0, 0) // WINEVENT_OUTOFCONTEXT
	if h == 0 {
		return func() {}
	}
	shownMu.Lock()
	shownHooks[h] = shown
	shownMu.Unlock()
	return func() {
		procUnhookWinEvent.Call(h)
		shownMu.Lock()
		delete(shownHooks, h)
		shownMu.Unlock()
	}
}

// ClipboardSequence is the clipboard's change count.
func ClipboardSequence() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

func clipboardHas(cf uint16) bool {
	r, _, _ := procIsClipboardFormatAvailable.Call(uintptr(cf))
	return r != 0
}

var (
	cfPNG = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("PNG") })
	cfURL = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("UniformResourceLocatorW") })
)

const (
	cfDIB   = 8
	cfDIBV5 = 17
	cfHDROP = 15
	cfText  = 13 // CF_UNICODETEXT
)

// ClipboardHas reports whether the clipboard has text, an image, a URL
// (a link or files).
func ClipboardHas() (text, img, url bool) {
	return clipboardHas(cfText), clipboardHas(cfPNG()) || clipboardHas(cfDIB) || clipboardHas(cfDIBV5),
		clipboardHas(cfURL()) || clipboardHas(cfHDROP)
}

// openClipboard opens the clipboard, waiting a little for another app
// that has it open (the clipboard history reads what's just copied).
func openClipboard(owner uintptr) bool {
	for i := 0; i < 20; i++ {
		if r, _, _ := procOpenClipboard.Call(owner); r != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ClipboardImage is the clipboard's image: its PNG, as copied, or the
// bitmap it has, as a PNG. nil: none.
func ClipboardImage(owner uintptr) []byte {
	if !openClipboard(owner) {
		return nil
	}
	defer procCloseClipboard.Call()
	read := func(cf uint16) []byte {
		g, _, _ := procGetClipboardData.Call(uintptr(cf))
		if g == 0 {
			return nil
		}
		return globalBytes(g, 1<<30)
	}
	if clipboardHas(cfPNG()) {
		if b := read(cfPNG()); len(b) > 8 {
			return b
		}
	}
	for _, cf := range []uint16{cfDIBV5, cfDIB} {
		if !clipboardHas(cf) {
			continue
		}
		if img := DecodeDIB(read(cf)); img != nil {
			var buf bytes.Buffer
			if png.Encode(&buf, img) == nil {
				return buf.Bytes()
			}
		}
	}
	return nil
}

// DecodeDIB reads a packed DIB (BITMAPINFOHEADER or V5, then its masks
// or palette, then its pixels): 24 and 32 bits, as most images on the
// clipboard are.
func DecodeDIB(b []byte) image.Image {
	if len(b) < 40 {
		return nil
	}
	size := int(binary.LittleEndian.Uint32(b))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:])))
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	comp := binary.LittleEndian.Uint32(b[16:])
	used := int(binary.LittleEndian.Uint32(b[32:]))
	if w <= 0 || h == 0 || size < 40 || size > len(b) || (bpp != 24 && bpp != 32) {
		return nil
	}
	topDown := h < 0
	if topDown {
		h = -h
	}
	off := size + used*4
	if comp == 3 && size == 40 { // BI_BITFIELDS: three masks after the header
		off += 12
	}
	stride := (w*bpp/8 + 3) &^ 3
	if off+stride*h > len(b) {
		return nil
	}
	// Alpha counts only if some pixel has it (many apps leave it 0).
	hasAlpha := false
	if bpp == 32 {
		for y := 0; y < h && !hasAlpha; y++ {
			row := b[off+y*stride:]
			for x := 0; x < w; x++ {
				if row[x*4+3] != 0 {
					hasAlpha = true
					break
				}
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := h - 1 - y
		if topDown {
			sy = y
		}
		row := b[off+sy*stride:]
		for x := 0; x < w; x++ {
			var c color.NRGBA
			if bpp == 32 {
				p := row[x*4:]
				c = color.NRGBA{p[2], p[1], p[0], 255}
				if hasAlpha {
					c.A = p[3]
				}
			} else {
				p := row[x*3:]
				c = color.NRGBA{p[2], p[1], p[0], 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// SetClipboardImage puts an image on the clipboard: as a bitmap (CF_DIB,
// which every app reads, and Windows makes the others of) and, if given,
// the file's PNG (for those that keep transparency).
func SetClipboardImage(owner uintptr, img image.Image, pngData []byte) error {
	if !openClipboard(owner) {
		return errors.New("the clipboard is in use")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	put := func(cf uint16, b []byte) bool {
		g, _, _ := procGlobalAlloc.Call(0x0002, uintptr(len(b))) // GMEM_MOVEABLE
		if g == 0 {
			return false
		}
		p, _, _ := procGlobalLock.Call(g)
		copy(unsafe.Slice((*byte)(native(p)), len(b)), b)
		procGlobalUnlock.Call(g)
		r, _, _ := procSetClipboardData.Call(uintptr(cf), g)
		return r != 0
	}
	if !put(cfDIB, EncodeDIB(img)) {
		return errors.New("the clipboard took no image")
	}
	if len(pngData) > 0 {
		put(cfPNG(), pngData)
	}
	return nil
}

// EncodeDIB is img as a packed 32-bit DIB, bottom-up.
func EncodeDIB(img image.Image) []byte {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	b := make([]byte, 40+w*h*4)
	binary.LittleEndian.PutUint32(b, 40)
	binary.LittleEndian.PutUint32(b[4:], uint32(w))
	binary.LittleEndian.PutUint32(b[8:], uint32(h))
	binary.LittleEndian.PutUint16(b[12:], 1)
	binary.LittleEndian.PutUint16(b[14:], 32)
	binary.LittleEndian.PutUint32(b[20:], uint32(w*h*4))
	for y := 0; y < h; y++ {
		row := b[40+(h-1-y)*w*4:]
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA)
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.B, c.G, c.R, c.A
		}
	}
	return b
}

// WatchFolder calls changed (on a goroutine of its own) each time a file
// is added to dir, renamed or removed in it, until the returned function
// is called.
func WatchFolder(dir string, changed func()) (stop func(), err error) {
	const fileName = 0x1 // FILE_NOTIFY_CHANGE_FILE_NAME
	ch, _, e := procFindFirstChangeNotification.Call(Str(dir), 0, fileName)
	if ch == 0 || ch == ^uintptr(0) {
		return nil, e
	}
	quit, _, _ := procCreateEventW.Call(0, 1, 0, 0)
	go func() {
		hs := [2]uintptr{ch, quit}
		for {
			r, _, _ := procWaitForMultipleObjects.Call(2, uintptr(unsafe.Pointer(&hs[0])), 0, 0xFFFFFFFF)
			if r != 0 { // WAIT_OBJECT_0: the folder; else quit, or failed
				break
			}
			changed()
			if r, _, _ := procFindNextChangeNotification.Call(ch); r == 0 {
				break
			}
		}
		procFindCloseChangeNotification.Call(ch)
		procCloseHandle.Call(quit)
	}()
	var once sync.Once
	return func() { once.Do(func() { procSetEvent.Call(quit) }) }, nil
}

// Execution states asked for: the system kept awake (background work),
// the display too (KeepAwake). The thread's state is their sum.
var execState struct {
	mu              sync.Mutex
	system, display bool
}

// KeepSystemAwake keeps the system from sleeping while on (the display
// may turn off), for work in progress.
func KeepSystemAwake(on bool) {
	execState.mu.Lock()
	execState.system = on
	applyExecState()
	execState.mu.Unlock()
}

func applyExecState() {
	const esContinuous, esSystemRequired, esDisplayRequired = 0x80000000, 0x1, 0x2
	flags := uintptr(esContinuous)
	if execState.system || execState.display {
		flags |= esSystemRequired
	}
	if execState.display {
		flags |= esDisplayRequired
	}
	procSetThreadExecutionState.Call(flags)
}

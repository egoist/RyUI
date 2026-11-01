//go:build windows && amd64

package windows

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// OLE drag and drop: a window's drop target (IDropTarget) and a drag's
// source (IDropSource) are COM objects of Go's; the data a drag carries
// is the shell's data object (SHCreateDataObject), and the shell's drag
// image helper shows the dragged view under the pointer.

var (
	procOleInitialize            = ole32.NewProc("OleInitialize")
	procRegisterDragDrop         = ole32.NewProc("RegisterDragDrop")
	procRevokeDragDrop           = ole32.NewProc("RevokeDragDrop")
	procDoDragDrop               = ole32.NewProc("DoDragDrop")
	procReleaseStgMedium         = ole32.NewProc("ReleaseStgMedium")
	procCoCreateInstance         = ole32.NewProc("CoCreateInstance")
	procCoTaskMemAlloc           = ole32.NewProc("CoTaskMemAlloc")
	procCoTaskMemFree            = ole32.NewProc("CoTaskMemFree")
	procSHCreateDataObject       = shell32.NewProc("SHCreateDataObject")
	procDragQueryFileW           = shell32.NewProc("DragQueryFileW")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGlobalSize               = kernel32.NewProc("GlobalSize")
)

const (
	DROPEFFECT_NONE = 0
	DROPEFFECT_COPY = 1
	DROPEFFECT_MOVE = 2
	DROPEFFECT_LINK = 4

	DRAGDROP_S_DROP   = 0x00040100
	DRAGDROP_S_CANCEL = 0x00040101

	CF_UNICODETEXT = 13
	CF_HDROP       = 15

	MK_RBUTTON = 0x0002
	MK_MBUTTON = 0x0010

	dragdropSUseDefaultCursors = 0x00040102
	eNoInterface               = 0x80004002
	ePointer                   = 0x80004003
	eUnexpected                = 0x8000FFFF

	tymedHGlobal    = 1
	tymedIStream    = 4
	dvAspectContent = 1
)

var (
	iidIUnknown          = NewGUID("00000000-0000-0000-C000-000000000046")
	iidIDataObject       = NewGUID("0000010E-0000-0000-C000-000000000046")
	iidIDropSource       = NewGUID("00000121-0000-0000-C000-000000000046")
	iidIDropTarget       = NewGUID("00000122-0000-0000-C000-000000000046")
	clsidDragDropHelper  = NewGUID("4657278A-411B-11D2-839A-00C04FD918D0")
	iidIDropTargetHelper = NewGUID("4657278B-411B-11D2-839A-00C04FD918D0")
	iidIDragSourceHelper = NewGUID("DE5BF786-477A-11D2-839D-00C04FD918D0")
)

// OleInitialize readies OLE (drag and drop) on the calling thread, which
// must be a single-threaded apartment's (CoInitializeEx made it one).
func OleInitialize() bool {
	hr, _, _ := procOleInitialize.Call(0)
	return !Failed(hr)
}

// WindowThread is the thread a window belongs to.
func WindowThread(hwnd uintptr) uint32 {
	r, _, _ := procGetWindowThreadProcessId.Call(hwnd, 0)
	return uint32(r)
}

// WindowFromPoint is the window at a point on the screen (a visible,
// enabled one, through those that let the mouse through).
func WindowFromPoint(p Point) uintptr {
	r, _, _ := procWindowFromPoint.Call(packPoint(p))
	return r
}

// packPoint is a POINT passed by value: 8 bytes, in one register.
func packPoint(p Point) uintptr { return uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32 }

// unpackPoint is a POINTL received by value.
func unpackPoint(v uintptr) Point { return Point{X: int32(uint32(v)), Y: int32(uint32(v >> 32))} }

// RegisterClipboardFormat is the ID of a named clipboard format.
func RegisterClipboardFormat(name string) uint16 {
	r, _, _ := procRegisterClipboardFormatW.Call(Str(name))
	return uint16(r)
}

// CoCreate makes a COM object of an in-process class, as its interface
// iid (a reference the caller releases), or 0.
func CoCreate(clsid, iid *GUID) uintptr {
	var out uintptr
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, 1, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) // CLSCTX_INPROC_SERVER
	if Failed(hr) {
		return 0
	}
	return out
}

// COM objects of Go's. Each is a vtable pointer in memory Go doesn't
// manage (CoTaskMemAlloc: it never moves, and COM may hold it as long as
// it likes); its vtable is a package variable, its Go side found by its
// address in comObjects, which keeps it reachable until its last
// reference is released. Their methods run on the thread that made
// them, an STA's, as COM calls them there.

type comObject struct {
	refs int32
	iid  *GUID // the interface it implements besides IUnknown
	impl any
}

var (
	comMu      sync.Mutex
	comObjects = map[uintptr]*comObject{}
	comOnce    sync.Once

	// The vtables: IUnknown's three methods, then the interface's.
	dropTargetVtbl [7]uintptr
	dropSourceVtbl [5]uintptr
)

func comInit() {
	comOnce.Do(func() {
		qi := syscall.NewCallback(comQueryInterface)
		addRef := syscall.NewCallback(func(this uintptr) uintptr { return uintptr(comRef(this, 1)) })
		release := syscall.NewCallback(func(this uintptr) uintptr { return uintptr(comRef(this, -1)) })
		dropTargetVtbl = [7]uintptr{qi, addRef, release,
			syscall.NewCallback(dropTargetDragEnter),
			syscall.NewCallback(dropTargetDragOver),
			syscall.NewCallback(dropTargetDragLeave),
			syscall.NewCallback(dropTargetDrop),
		}
		dropSourceVtbl = [5]uintptr{qi, addRef, release,
			syscall.NewCallback(dropSourceQueryContinueDrag),
			syscall.NewCallback(dropSourceGiveFeedback),
		}
	})
}

// newComObject makes a COM object holding one reference.
func newComObject(vtbl *uintptr, iid *GUID, impl any) uintptr {
	comInit()
	p, _, _ := procCoTaskMemAlloc.Call(unsafe.Sizeof(uintptr(0)))
	if p == 0 {
		return 0
	}
	*(*uintptr)(native(p)) = uintptr(unsafe.Pointer(vtbl))
	comMu.Lock()
	comObjects[p] = &comObject{refs: 1, iid: iid, impl: impl}
	comMu.Unlock()
	return p
}

// comRef adds delta to an object's references and returns them; the
// object goes with its last.
func comRef(this uintptr, delta int32) int32 {
	comMu.Lock()
	o := comObjects[this]
	if o == nil {
		comMu.Unlock()
		return 0
	}
	o.refs += delta
	refs := o.refs
	if refs <= 0 {
		delete(comObjects, this)
	}
	comMu.Unlock()
	if refs <= 0 {
		procCoTaskMemFree.Call(this)
		return 0
	}
	return refs
}

func comImpl(this uintptr) any {
	comMu.Lock()
	defer comMu.Unlock()
	if o := comObjects[this]; o != nil {
		return o.impl
	}
	return nil
}

func comQueryInterface(this, riid, out uintptr) uintptr {
	if out == 0 {
		return ePointer
	}
	*(*uintptr)(native(out)) = 0
	if riid == 0 {
		return eNoInterface
	}
	iid := *(*GUID)(native(riid))
	comMu.Lock()
	o := comObjects[this]
	ok := o != nil && (iid == iidIUnknown || iid == *o.iid)
	if ok {
		o.refs++
	}
	comMu.Unlock()
	if !ok {
		return eNoInterface
	}
	*(*uintptr)(native(out)) = this
	return 0
}

// DropTarget is a window's drop target's Go side: IDropTarget's methods,
// each given the effects the source allows and returning the one the
// drop would have (DROPEFFECT_NONE: not here). pt is on the screen, in
// pixels.
type DropTarget interface {
	DragEnter(data uintptr, keys uint32, pt Point, allowed uint32) uint32
	DragOver(keys uint32, pt Point, allowed uint32) uint32
	DragLeave()
	Drop(data uintptr, keys uint32, pt Point, allowed uint32) uint32
}

// NewDropTargetObject makes an IDropTarget calling t, holding one
// reference, which the caller releases (Release).
func NewDropTargetObject(t DropTarget) uintptr {
	return newComObject(&dropTargetVtbl[0], &iidIDropTarget, t)
}

// RegisterDropTarget makes hwnd, a top-level window, take drops through
// t, until RevokeDropTarget (before it's destroyed).
func RegisterDropTarget(hwnd uintptr, t DropTarget) bool {
	obj := NewDropTargetObject(t)
	if obj == 0 {
		return false
	}
	hr, _, _ := procRegisterDragDrop.Call(hwnd, obj)
	comRef(obj, -1) // RegisterDragDrop keeps its own
	return !Failed(hr)
}

// RevokeDropTarget undoes RegisterDropTarget.
func RevokeDropTarget(hwnd uintptr) { procRevokeDragDrop.Call(hwnd) }

func dropTargetOf(this uintptr) DropTarget {
	t, _ := comImpl(this).(DropTarget)
	return t
}

// The effect is an in-out DWORD: the effects allowed in, the one chosen
// out.
func dropTargetDragEnter(this, data, keys, pt, effect uintptr) uintptr {
	t := dropTargetOf(this)
	if t == nil || effect == 0 {
		return eUnexpected
	}
	e := (*uint32)(native(effect))
	*e = t.DragEnter(data, uint32(keys), unpackPoint(pt), *e)
	return 0
}

func dropTargetDragOver(this, keys, pt, effect uintptr) uintptr {
	t := dropTargetOf(this)
	if t == nil || effect == 0 {
		return eUnexpected
	}
	e := (*uint32)(native(effect))
	*e = t.DragOver(uint32(keys), unpackPoint(pt), *e)
	return 0
}

func dropTargetDragLeave(this uintptr) uintptr {
	if t := dropTargetOf(this); t != nil {
		t.DragLeave()
	}
	return 0
}

func dropTargetDrop(this, data, keys, pt, effect uintptr) uintptr {
	t := dropTargetOf(this)
	if t == nil || effect == 0 {
		return eUnexpected
	}
	e := (*uint32)(native(effect))
	*e = t.Drop(data, uint32(keys), unpackPoint(pt), *e)
	return 0
}

// The drag source: Escape, or another button, cancels; letting go of the
// left button drops; the system's cursors show the effect.

func dropSourceQueryContinueDrag(this, escape, keys uintptr) uintptr {
	switch {
	case uint32(escape) != 0 || keys&(MK_RBUTTON|MK_MBUTTON) != 0: // a BOOL: its register's upper half is anything
		return DRAGDROP_S_CANCEL
	case keys&MK_LBUTTON == 0:
		return DRAGDROP_S_DROP
	}
	return 0
}

func dropSourceGiveFeedback(this, effect uintptr) uintptr { return dragdropSUseDefaultCursors }

// DoDragDrop runs a drag of data with the left button (a modal loop,
// until it's dropped or cancelled), allowing effects. hr is
// DRAGDROP_S_DROP or DRAGDROP_S_CANCEL (or an error), effect the drop's.
func DoDragDrop(data uintptr, allowed uint32) (hr uintptr, effect uint32) {
	src := newComObject(&dropSourceVtbl[0], &iidIDropSource, struct{}{})
	if src == 0 {
		return eUnexpected, 0
	}
	defer comRef(src, -1)
	hr, _, _ = procDoDragDrop.Call(data, src, uintptr(allowed), uintptr(unsafe.Pointer(&effect)))
	return hr, effect
}

// Data objects.

type formatEtc struct {
	Format uint16
	Ptd    uintptr
	Aspect uint32
	Index  int32
	Tymed  uint32
}

type stgMedium struct {
	Tymed         uint32
	Handle        uintptr
	UnkForRelease uintptr
}

// NewDataObject makes an empty data object (the shell's, which takes
// any format), a reference the caller releases.
func NewDataObject() uintptr {
	var out uintptr
	hr, _, _ := procSHCreateDataObject.Call(0, 0, 0, 0, uintptr(unsafe.Pointer(&iidIDataObject)), uintptr(unsafe.Pointer(&out)))
	if Failed(hr) {
		return 0
	}
	return out
}

// SetData puts b in a data object as format cf, in global memory the
// data object then owns.
func SetData(data uintptr, cf uint16, b []byte) bool {
	g, _, _ := procGlobalAlloc.Call(0x0002, uintptr(max(len(b), 1))) // GMEM_MOVEABLE
	if g == 0 {
		return false
	}
	if len(b) > 0 {
		p, _, _ := procGlobalLock.Call(g)
		if p == 0 {
			procGlobalFree.Call(g)
			return false
		}
		copy(unsafe.Slice((*byte)(native(p)), len(b)), b)
		procGlobalUnlock.Call(g)
	}
	fe := formatEtc{Format: cf, Aspect: dvAspectContent, Index: -1, Tymed: tymedHGlobal}
	sm := stgMedium{Tymed: tymedHGlobal, Handle: g}
	if Failed(ComCall(data, 7, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&sm)), 1)) { // fRelease: it's the data object's
		procGlobalFree.Call(g)
		return false
	}
	return true
}

// HasData reports whether a data object has format cf in global memory.
func HasData(data uintptr, cf uint16) bool {
	fe := formatEtc{Format: cf, Aspect: dvAspectContent, Index: -1, Tymed: tymedHGlobal}
	return ComCall(data, 5, uintptr(unsafe.Pointer(&fe))) == 0
}

// getData gets format cf (its item index, -1 for all) from a data
// object, in one of the media tymed; the caller releases the medium
// (procReleaseStgMedium).
func getData(data uintptr, cf uint16, index int32, tymed uint32) (stgMedium, bool) {
	fe := formatEtc{Format: cf, Aspect: dvAspectContent, Index: index, Tymed: tymed}
	var sm stgMedium
	if Failed(ComCall(data, 3, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&sm)))) {
		return sm, false
	}
	if sm.Tymed&tymed == 0 {
		procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&sm)))
		return sm, false
	}
	return sm, true
}

// globalBytes copies global memory, at most limit bytes (-1: all).
func globalBytes(g uintptr, limit int64) []byte {
	n, _, _ := procGlobalSize.Call(g)
	size := int64(n)
	if limit >= 0 && limit < size {
		size = limit
	}
	p, _, _ := procGlobalLock.Call(g)
	if p == 0 {
		return nil
	}
	b := make([]byte, size)
	copy(b, unsafe.Slice((*byte)(native(p)), size))
	procGlobalUnlock.Call(g)
	return b
}

// GetData copies format cf out of a data object.
func GetData(data uintptr, cf uint16) ([]byte, bool) {
	sm, ok := getData(data, cf, -1, tymedHGlobal)
	if !ok {
		return nil, false
	}
	b := globalBytes(sm.Handle, -1)
	procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&sm)))
	return b, true
}

// UTF16Bytes decodes UTF-16 text in bytes, up to its NUL.
func UTF16Bytes(b []byte) string {
	u := unsafe.Slice((*uint16)(unsafe.Pointer(unsafe.SliceData(b))), len(b)/2)
	for i, c := range u {
		if c == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}

// GetText is a data object's text in format cf, UTF-16 (CF_UNICODETEXT,
// UniformResourceLocatorW) or, ansi, 8-bit (UniformResourceLocator).
func GetText(data uintptr, cf uint16, ansi bool) (string, bool) {
	b, ok := GetData(data, cf)
	if !ok {
		return "", false
	}
	if ansi {
		for i, c := range b {
			if c == 0 {
				b = b[:i]
				break
			}
		}
		return string(b), true
	}
	return UTF16Bytes(b), true
}

// DroppedFiles are the paths in a data object's CF_HDROP.
func DroppedFiles(data uintptr) []string {
	sm, ok := getData(data, CF_HDROP, -1, tymedHGlobal)
	if !ok {
		return nil
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&sm)))
	n, _, _ := procDragQueryFileW.Call(sm.Handle, 0xFFFFFFFF, 0, 0)
	paths := make([]string, 0, n)
	for i := uintptr(0); i < n; i++ {
		l, _, _ := procDragQueryFileW.Call(sm.Handle, i, 0, 0)
		if l == 0 {
			continue
		}
		buf := make([]uint16, l+1)
		procDragQueryFileW.Call(sm.Handle, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		paths = append(paths, syscall.UTF16ToString(buf))
	}
	return paths
}

// HDrop is CF_HDROP's data for paths: a DROPFILES of wide strings, each
// NUL-terminated, the list too.
func HDrop(paths []string) []byte {
	b := make([]byte, 20, 20+64*len(paths))
	b[0] = 20 // pFiles: the paths' offset
	b[16] = 1 // fWide
	for _, p := range paths {
		for _, u := range UTF16z(p) {
			b = append(b, byte(u), byte(u>>8))
		}
	}
	return append(b, 0, 0)
}

// UTF16Data is text as UTF-16 bytes, NUL-terminated (CF_UNICODETEXT).
func UTF16Data(s string) []byte {
	u := UTF16z(s)
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

// Virtual files: files a drag carries as their contents, not as paths
// (FileGroupDescriptorW and FileContents: Outlook's attachments, a
// browser's images).

// VirtualFile is one of a drag's virtual files.
type VirtualFile struct {
	Name string // its path in the drag, \-separated
	Size int64  // -1: unknown
	Dir  bool
}

var (
	cfFileGroupDescriptorW = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("FileGroupDescriptorW") })
	cfFileContents         = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("FileContents") })
)

// VirtualFiles are a data object's virtual files.
func VirtualFiles(data uintptr) []VirtualFile {
	b, ok := GetData(data, cfFileGroupDescriptorW())
	if !ok || len(b) < 4 {
		return nil
	}
	const size = 592 // FILEDESCRIPTORW
	n := int(le32(b))
	var out []VirtualFile
	for i := 0; i < n && 4+(i+1)*size <= len(b); i++ {
		d := b[4+i*size : 4+(i+1)*size]
		flags, attrs := le32(d), le32(d[36:])
		f := VirtualFile{Name: UTF16Bytes(d[72:]), Size: -1}
		if flags&0x40 != 0 { // FD_FILESIZE
			f.Size = int64(le32(d[64:]))<<32 | int64(le32(d[68:]))
		}
		f.Dir = flags&0x4 != 0 && attrs&0x10 != 0 // FD_ATTRIBUTES, FILE_ATTRIBUTE_DIRECTORY
		out = append(out, f)
	}
	return out
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// SaveVirtualFile writes a data object's virtual file i (whose size is
// size, -1 if unknown) to path, and returns its size.
func SaveVirtualFile(data uintptr, i int, size int64, path string) (int64, error) {
	sm, ok := getData(data, cfFileContents(), int32(i), tymedHGlobal|tymedIStream)
	if !ok {
		return 0, errors.New("the file's contents aren't there")
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&sm)))
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	var n int64
	if sm.Tymed == tymedHGlobal {
		b := globalBytes(sm.Handle, size)
		var w int
		w, err = f.Write(b)
		n = int64(w)
	} else {
		// IStream::Read until it ends.
		buf := make([]byte, 64<<10)
		for {
			var got uint32
			hr := ComCall(sm.Handle, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&got)))
			if got > 0 {
				if _, err = f.Write(buf[:got]); err != nil {
					break
				}
				n += int64(got)
			}
			if Failed(hr) {
				err = errors.New("reading the file's contents failed")
				break
			}
			if got == 0 || hr != 0 { // S_FALSE: the end
				break
			}
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// The shell's drag image helper.

// SetDragImage gives a drag of data an image: bmp, a 32-bit top-down DIB
// of w by h pixels (premultiplied alpha), with the pointer at offset in
// it. The helper owns bmp if it takes it (true); otherwise it's still
// the caller's.
func SetDragImage(data, bmp uintptr, w, h int32, offset Point) bool {
	helper := CoCreate(&clsidDragDropHelper, &iidIDragSourceHelper)
	if helper == 0 {
		return false
	}
	defer Release(helper)
	img := struct {
		W, H     int32
		Offset   Point
		Bitmap   uintptr
		ColorKey uint32
	}{w, h, offset, bmp, 0xFFFFFFFF} // CLR_NONE
	return !Failed(ComCall(helper, 3, uintptr(unsafe.Pointer(&img)), data)) // InitializeFromBitmap
}

// DropHelper is the shell's IDropTargetHelper: a drop target tells it of
// the drag, and it shows the drag's image over the window.
type DropHelper uintptr

// NewDropHelper makes one, or 0.
func NewDropHelper() DropHelper {
	return DropHelper(CoCreate(&clsidDragDropHelper, &iidIDropTargetHelper))
}

func (d DropHelper) Enter(hwnd, data uintptr, pt Point, effect uint32) {
	if d != 0 {
		ComCall(uintptr(d), 3, hwnd, data, uintptr(unsafe.Pointer(&pt)), uintptr(effect))
	}
}

func (d DropHelper) Leave() {
	if d != 0 {
		ComCall(uintptr(d), 4)
	}
}

func (d DropHelper) Over(pt Point, effect uint32) {
	if d != 0 {
		ComCall(uintptr(d), 5, uintptr(unsafe.Pointer(&pt)), uintptr(effect))
	}
}

func (d DropHelper) Drop(data uintptr, pt Point, effect uint32) {
	if d != 0 {
		ComCall(uintptr(d), 6, data, uintptr(unsafe.Pointer(&pt)), uintptr(effect))
	}
}

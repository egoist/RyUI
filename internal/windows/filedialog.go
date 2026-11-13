//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// The common item dialogs (IFileOpenDialog, IFileSaveDialog: Vista's,
// Explorer's look), the shell's view of a folder with items selected
// (SHOpenFolderAndSelectItems), and the Recycle Bin (IFileOperation).

var (
	clsidFileOpenDialog = NewGUID("dc1c5a9c-e88a-4dde-a5a1-60f82a20aef7")
	iidIFileOpenDialog  = NewGUID("d57c7288-d4ad-4768-be02-9d969532d960")
	clsidFileSaveDialog = NewGUID("c0b4e2f3-ba21-4773-8dba-335ec946eb8b")
	iidIFileSaveDialog  = NewGUID("84bccd23-5fde-4cdb-aea4-af64b83d78ab")
	iidIShellItem       = NewGUID("43826d1e-e718-42ee-bc55-a1e261c37bfe")
	iidIOleWindow       = NewGUID("00000114-0000-0000-c000-000000000046")

	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	procSHParseDisplayName          = shell32.NewProc("SHParseDisplayName")
	procSHOpenFolderAndSelectItems  = shell32.NewProc("SHOpenFolderAndSelectItems")
)

// IFileDialog's methods (after IModalWindow's Show), IFileOpenDialog's,
// IShellItem's and IShellItemArray's.
const (
	fdShow                = 3
	fdSetFileTypes        = 4
	fdSetFileTypeIndex    = 5
	fdSetOptions          = 9
	fdGetOptions          = 10
	fdSetFolder           = 12
	fdSetFileName         = 15
	fdSetTitle            = 17
	fdSetOkButtonLabel    = 18
	fdGetResult           = 20
	fdSetDefaultExtension = 22
	fodGetResults         = 27

	siGetDisplayName = 5
	siaGetCount      = 7
	siaGetItemAt     = 8

	sigdnFileSysPath = 0x80058000
)

// FILEOPENDIALOGOPTIONS.
const (
	FOS_OVERWRITEPROMPT  = 0x2
	FOS_STRICTFILETYPES  = 0x4
	FOS_NOCHANGEDIR      = 0x8
	FOS_PICKFOLDERS      = 0x20
	FOS_FORCEFILESYSTEM  = 0x40
	FOS_ALLOWMULTISELECT = 0x200
	FOS_PATHMUSTEXIST    = 0x800
	FOS_FILEMUSTEXIST    = 0x1000
	FOS_FORCESHOWHIDDEN  = 0x10000000
)

// errCancelled is HRESULT_FROM_WIN32(ERROR_CANCELLED): the user closed
// the dialog.
const errCancelled = 0x800704C7

// FileDialogOptions say what FileDialog shows.
type FileDialogOptions struct {
	Save     bool // the save dialog (else the open dialog)
	Folders  bool // the open dialog picks folders
	Multiple bool
	Hidden   bool // shows hidden files
	Title    string
	Button   string // the OK button's label
	Folder   string // the folder it starts in
	Name     string // the file name in the name field (Save: suggested; Open: selected)
	Filters  []FileFilter
	// DefaultExt is added to a name the user types without one (Save;
	// no dot): the chosen filter's own, as Windows does, once it's set.
	DefaultExt string
	// Created (if not nil) is called with the dialog (an IFileDialog)
	// before it shows: tests answer it.
	Created func(dlg uintptr)
}

// FileDialog shows the open or save dialog, modal to owner, and returns
// the paths chosen; none: cancelled. It runs a modal loop: posted work
// and the owner's messages go on meanwhile. Main thread (an STA's).
func FileDialog(owner uintptr, o FileDialogOptions) ([]string, error) {
	clsid, iid := &clsidFileOpenDialog, &iidIFileOpenDialog
	if o.Save {
		clsid, iid = &clsidFileSaveDialog, &iidIFileSaveDialog
	}
	dlg := CoCreate(clsid, iid)
	if dlg == 0 {
		return nil, errors.New("the file dialog isn't available")
	}
	defer Release(dlg)

	var opts uint32
	ComCall(dlg, fdGetOptions, uintptr(unsafe.Pointer(&opts)))
	opts |= FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST | FOS_NOCHANGEDIR
	switch {
	case o.Save:
		opts |= FOS_OVERWRITEPROMPT
	case o.Folders:
		opts |= FOS_PICKFOLDERS
	default:
		opts |= FOS_FILEMUSTEXIST
	}
	if o.Multiple && !o.Save {
		opts |= FOS_ALLOWMULTISELECT
	}
	if o.Hidden {
		opts |= FOS_FORCESHOWHIDDEN
	}
	ComCall(dlg, fdSetOptions, uintptr(opts))
	if o.Title != "" {
		ComCall(dlg, fdSetTitle, uintptr(unsafe.Pointer(UTF16(o.Title))))
	}
	if o.Button != "" {
		ComCall(dlg, fdSetOkButtonLabel, uintptr(unsafe.Pointer(UTF16(o.Button))))
	}
	if o.Folder != "" {
		if folder := ShellItem(o.Folder); folder != 0 {
			ComCall(dlg, fdSetFolder, folder)
			Release(folder)
		}
	}
	if o.Name != "" {
		ComCall(dlg, fdSetFileName, uintptr(unsafe.Pointer(UTF16(o.Name))))
	}
	// COMDLG_FILTERSPEC: {pszName, pszSpec} pairs, which the dialog
	// copies.
	if len(o.Filters) > 0 && !o.Folders {
		type filterSpec struct{ name, spec *uint16 }
		specs := make([]filterSpec, len(o.Filters))
		for i, f := range o.Filters {
			specs[i] = filterSpec{UTF16(f.Name), UTF16(f.Patterns)}
		}
		ComCall(dlg, fdSetFileTypes, uintptr(len(specs)), uintptr(unsafe.Pointer(&specs[0])))
		ComCall(dlg, fdSetFileTypeIndex, 1)
		runtime.KeepAlive(specs)
	}
	if o.Save && o.DefaultExt != "" {
		ComCall(dlg, fdSetDefaultExtension, uintptr(unsafe.Pointer(UTF16(o.DefaultExt))))
	}
	if o.Created != nil {
		o.Created(dlg)
	}

	if hr := showDialog(dlg, owner); Failed(hr) {
		if hr == errCancelled {
			return nil, nil
		}
		return nil, fmt.Errorf("the file dialog failed (0x%x)", uint32(hr))
	}
	if o.Save {
		var item uintptr
		if Failed(ComCall(dlg, fdGetResult, uintptr(unsafe.Pointer(&item)))) || item == 0 {
			return nil, nil
		}
		defer Release(item)
		if p := itemPath(item); p != "" {
			return []string{p}, nil
		}
		return nil, errors.New("the file chosen isn't in the file system")
	}
	var items uintptr
	if Failed(ComCall(dlg, fodGetResults, uintptr(unsafe.Pointer(&items)))) || items == 0 {
		return nil, nil
	}
	defer Release(items)
	var n uint32
	ComCall(items, siaGetCount, uintptr(unsafe.Pointer(&n)))
	var paths []string
	for i := uint32(0); i < n; i++ {
		var item uintptr
		if !Failed(ComCall(items, siaGetItemAt, uintptr(i), uintptr(unsafe.Pointer(&item)))) && item != 0 {
			if p := itemPath(item); p != "" {
				paths = append(paths, p)
			}
			Release(item)
		}
	}
	return paths, nil
}

// showDialog is IModalWindow's Show. Its modal loop dispatches messages
// to Go, which can move the goroutine's stack: nothing of it is passed.
func showDialog(dlg, owner uintptr) uintptr { return ComCall(dlg, fdShow, owner) }

// FileDialogWindow is a file dialog's window once it shows (its
// IOleWindow's), else 0.
func FileDialogWindow(dlg uintptr) uintptr {
	ow := QueryInterface(dlg, &iidIOleWindow)
	if ow == 0 {
		return 0
	}
	defer Release(ow)
	var hwnd uintptr
	if Failed(ComCall(ow, 3, uintptr(unsafe.Pointer(&hwnd)))) {
		return 0
	}
	return hwnd
}

// SetFileDialogName sets the name in a file dialog's name field.
func SetFileDialogName(dlg uintptr, name string) {
	ComCall(dlg, fdSetFileName, uintptr(unsafe.Pointer(UTF16(name))))
}

// ShellItem is the shell's item for a path (an IShellItem the caller
// releases), or 0.
func ShellItem(path string) uintptr {
	var item uintptr
	r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(UTF16(path))), 0,
		uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item)))
	if Failed(r) {
		return 0
	}
	return item
}

func itemPath(item uintptr) string {
	var p uintptr
	if Failed(ComCall(item, siGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&p)))) {
		return ""
	}
	return TakeString(p)
}

// RevealInFolder opens the item's folder in Explorer with the item
// selected (a window already showing the folder is used). It waits for
// Explorer: call it off the main thread; it runs on a thread of its own.
func RevealInFolder(path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED
	defer procCoUninitialize.Call()
	var pidl uintptr
	r, _, _ := procSHParseDisplayName.Call(uintptr(unsafe.Pointer(UTF16(path))), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if Failed(r) || pidl == 0 {
		return fmt.Errorf("no item at %s (0x%x)", path, uint32(r))
	}
	defer procCoTaskMemFree.Call(pidl)
	// No children: pidl is the item, selected in its folder.
	if r, _, _ := procSHOpenFolderAndSelectItems.Call(pidl, 0, 0, 0); Failed(r) {
		return fmt.Errorf("couldn't show %s in its folder (0x%x)", path, uint32(r))
	}
	return nil
}

// AlertBeep plays the user's default sound (MessageBeep(MB_OK)).
func AlertBeep() { procMessageBeep.Call(0) }

// The Recycle Bin, through IFileOperation, as Explorer's delete: Windows
// deletes an item for good when it can't recycle it (one too big for the
// Recycle Bin, or on a drive without one, as network shares and many
// removable drives are), and with no UI nothing asks first; a progress
// sink refuses those deletes, so Trash fails instead.

var (
	clsidFileOperation            = NewGUID("3ad05575-8857-4850-9277-11b85bdb8e09")
	iidIFileOperation             = NewGUID("947aab5f-0a5c-4c13-b4d6-4bf7836fc9f8")
	iidIFileOperationProgressSink = NewGUID("04b0f1a7-9490-44bc-96e1-4296a31252e2")
	trashSinkVtbl                 [19]uintptr
	trashSinkOnce                 sync.Once
)

const (
	fofSilent                  = 0x0004
	fofNoConfirmation          = 0x0010
	fofAllowUndo               = 0x0040
	fofNoConfirmMkdir          = 0x0200
	fofNoErrorUI               = 0x0400
	fofxRecycleOnDelete        = 0x00080000
	fofxAddUndoRecord          = 0x20000000
	tsfDeleteRecycleIfPossible = 0x80
	eAbort                     = 0x80004004

	foSetOperationFlags       = 5
	foDeleteItem              = 18
	foPerformOperations       = 21
	foGetAnyOperationsAborted = 22
)

// trashSink is an IFileOperationProgressSink's Go side: PreDeleteItem
// refuses a delete that wouldn't recycle.
type trashSink struct{ refused bool }

// ErrNotRecyclable: the item can't go to the Recycle Bin (Trash).
var ErrNotRecyclable = errors.New("it can't go to the Recycle Bin: it would be deleted for good")

func initTrashSink() {
	comInit()
	cb := syscall.NewCallback
	ok0 := cb(func(this uintptr) uintptr { return 0 })
	ok1 := cb(func(this, a uintptr) uintptr { return 0 })
	ok2 := cb(func(this, a, b uintptr) uintptr { return 0 })
	ok3 := cb(func(this, a, b, c uintptr) uintptr { return 0 })
	ok4 := cb(func(this, a, b, c, d uintptr) uintptr { return 0 })
	ok5 := cb(func(this, a, b, c, d, e uintptr) uintptr { return 0 })
	ok6 := cb(func(this, a, b, c, d, e, f uintptr) uintptr { return 0 })
	ok7 := cb(func(this, a, b, c, d, e, f, g uintptr) uintptr { return 0 })
	// IUnknown's three are every Go COM object's (ole.go).
	trashSinkVtbl = [19]uintptr{
		dropTargetVtbl[0], dropTargetVtbl[1], dropTargetVtbl[2],
		ok0, // StartOperations
		ok1, // FinishOperations(hr)
		ok3, // PreRenameItem
		ok5, // PostRenameItem
		ok4, // PreMoveItem
		ok6, // PostMoveItem
		ok4, // PreCopyItem
		ok6, // PostCopyItem
		// PreDeleteItem(flags, item)
		cb(func(this, flags, item uintptr) uintptr {
			if uint32(flags)&tsfDeleteRecycleIfPossible == 0 {
				if s, _ := comImpl(this).(*trashSink); s != nil {
					s.refused = true
				}
				return eAbort
			}
			return 0
		}),
		ok4,           // PostDeleteItem
		ok3,           // PreNewItem
		ok7,           // PostNewItem
		ok2,           // UpdateProgress
		ok0, ok0, ok0, // ResetTimer, PauseTimer, ResumeTimer
	}
}

// Trash moves the item at path to the Recycle Bin, or fails
// (ErrNotRecyclable) where it would be deleted for good. It waits for
// the shell: call it off the main thread; it runs on a thread of its own.
func Trash(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return fmt.Errorf("no file at %s", path)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED
	defer procCoUninitialize.Call()
	trashSinkOnce.Do(initTrashSink)

	item := ShellItem(path)
	if item == 0 {
		return fmt.Errorf("no item at %s", path)
	}
	defer Release(item)
	op := CoCreate(&clsidFileOperation, &iidIFileOperation)
	if op == 0 {
		return errors.New("IFileOperation isn't available")
	}
	defer Release(op)
	s := &trashSink{}
	sink := newComObject(&trashSinkVtbl[0], &iidIFileOperationProgressSink, s)
	defer Release(sink)
	flags := uintptr(fofSilent | fofNoConfirmation | fofNoErrorUI | fofNoConfirmMkdir | fofAllowUndo | fofxRecycleOnDelete | fofxAddUndoRecord)
	if r := ComCall(op, foSetOperationFlags, flags); Failed(r) {
		return fmt.Errorf("IFileOperation.SetOperationFlags (0x%x)", uint32(r))
	}
	if r := ComCall(op, foDeleteItem, item, sink); Failed(r) {
		return fmt.Errorf("IFileOperation.DeleteItem (0x%x)", uint32(r))
	}
	r := ComCall(op, foPerformOperations)
	var aborted int32
	ComCall(op, foGetAnyOperationsAborted, uintptr(unsafe.Pointer(&aborted)))
	switch {
	case s.refused:
		return ErrNotRecyclable
	case Failed(r):
		return fmt.Errorf("couldn't move %s to the Recycle Bin (0x%x)", path, uint32(r))
	case aborted != 0:
		return fmt.Errorf("couldn't move %s to the Recycle Bin", path)
	}
	return nil
}

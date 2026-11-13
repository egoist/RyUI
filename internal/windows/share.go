//go:build windows

package windows

import (
	"errors"
	"fmt"
	"unsafe"
)

// Windows's share UI (the Share flyout, Windows 10 and later) for a
// desktop window: the DataTransferManager for the window, through
// IDataTransferManagerInterop, fills a DataPackage when asked
// (DataRequested) and shows it.

var (
	iidDataTransferManagerInterop = NewGUID("3A3DCD6C-3EAB-43DC-BCDE-45671CE800C8")
	iidDataTransferManager        = NewGUID("A5CAEE9B-8708-49D1-8D36-67D25A8DA00C")
	iidDataRequestedEventArgs     = NewGUID("CB8BA807-6AC5-43C9-8AC5-9BA232163182")
	iidUriFactory                 = NewGUID("44A9796F-723E-4FDF-A218-033E75B0C084")
	iidStorageFileStatics         = NewGUID("5984C710-DAF2-43C8-8BB4-A4D3EACFD03F")
	iidStorageFile                = NewGUID("FA3F6186-4214-428C-A64C-14C9AC7315EA")
	iidStorageItem                = NewGUID("4207A996-CA2F-42F7-BDE8-8B10457A7F30")

	// TypedEventHandler<DataTransferManager, DataRequestedEventArgs>.
	iidDataRequestedHandler = ParamIID("pinterface({9de1c534-6ae1-11e0-84e1-18a905bcc53f};" +
		RuntimeClassSig("Windows.ApplicationModel.DataTransfer.DataTransferManager", iidDataTransferManager) + ";" +
		RuntimeClassSig("Windows.ApplicationModel.DataTransfer.DataRequestedEventArgs", iidDataRequestedEventArgs) + ")")
	// AsyncOperationCompletedHandler<StorageFile>.
	iidStorageFileCompleted = ParamIID("pinterface({fcdcf02c-e5d8-4478-915a-4d90b74b83a5};" +
		RuntimeClassSig("Windows.Storage.StorageFile", iidStorageFile) + ")")
)

// Share is what the share UI offers: a title (required by it), text, a
// link, files (paths).
type Share struct {
	Title, Text, Link string
	Files             []string
}

// sharing is the share in progress: one at a time, per process.
var sharing struct {
	dtm   uintptr
	token int64
	share Share
	files []uintptr // its StorageFiles
}

// ShowShare shows Windows's share UI over hwnd (a top-level window) for
// s. Files are looked up first (StorageFile, asynchronously): done is
// called once the UI shows, or with why it can't.
func ShowShare(hwnd uintptr, s Share, done func(error)) {
	if s.Title == "" {
		s.Title = " "
	}
	if len(s.Files) == 0 {
		done(showShare(hwnd, s, nil))
		return
	}
	statics, err := ActivationFactory("Windows.Storage.StorageFile", iidStorageFileStatics)
	if err != nil {
		done(err)
		return
	}
	files := make([]uintptr, len(s.Files))
	left := len(s.Files)
	failed := false
	for i, path := range s.Files {
		hs := NewHString(path)
		var op uintptr
		hr := ComCall(statics, 6, uintptr(hs), uintptr(unsafe.Pointer(&op))) // GetFileFromPathAsync
		hs.Delete()
		if Failed(hr) || op == 0 {
			Release(statics)
			for _, f := range files {
				Release(f)
			}
			done(fmt.Errorf("%s: %#x", path, uint32(hr)))
			return
		}
		h := NewDelegate(iidStorageFileCompleted, func(op, status uintptr) {
			var f uintptr
			if status == 1 { // Completed
				ComCall(op, 8, uintptr(unsafe.Pointer(&f))) // IAsyncOperation.GetResults
			}
			if f == 0 && !failed {
				failed = true
				done(fmt.Errorf("%s: not found", path))
			}
			files[i] = f
			left--
			if left == 0 && !failed {
				done(showShare(hwnd, s, files))
			} else if left == 0 {
				for _, f := range files {
					Release(f)
				}
			}
		})
		ComCall(op, 6, h) // put_Completed
		Release(h)
		Release(op)
	}
	Release(statics)
}

func showShare(hwnd uintptr, s Share, files []uintptr) error {
	interop, err := ActivationFactory("Windows.ApplicationModel.DataTransfer.DataTransferManager", iidDataTransferManagerInterop)
	if err != nil {
		return err
	}
	defer Release(interop)
	if sharing.dtm != 0 {
		ComCall(sharing.dtm, 7, uintptr(sharing.token)) // remove_DataRequested
		Release(sharing.dtm)
		sharing.dtm = 0
	}
	for _, f := range sharing.files {
		Release(f)
	}
	var dtm uintptr
	if hr := ComCall(interop, 3, hwnd, uintptr(unsafe.Pointer(&iidDataTransferManager)), uintptr(unsafe.Pointer(&dtm))); Failed(hr) || dtm == 0 {
		return fmt.Errorf("the share UI: %#x", uint32(hr))
	}
	sharing.dtm, sharing.share, sharing.files = dtm, s, files
	h := NewDelegate(iidDataRequestedHandler, func(sender, args uintptr) { fillShare(args) })
	hr := ComCall(dtm, 6, h, uintptr(unsafe.Pointer(&sharing.token))) // add_DataRequested
	Release(h)
	if Failed(hr) {
		return fmt.Errorf("the share UI: %#x", uint32(hr))
	}
	if hr := ComCall(interop, 4, hwnd); Failed(hr) { // ShowShareUIForWindow
		return fmt.Errorf("the share UI: %#x", uint32(hr))
	}
	return nil
}

// fillShare fills the DataPackage the share UI asks for.
func fillShare(args uintptr) {
	s := sharing.share
	var req, pkg, props uintptr
	if ComCall(args, 6, uintptr(unsafe.Pointer(&req))); req == 0 { // get_Request
		return
	}
	defer Release(req)
	if ComCall(req, 6, uintptr(unsafe.Pointer(&pkg))); pkg == 0 { // get_Data
		return
	}
	defer Release(pkg)
	if ComCall(pkg, 7, uintptr(unsafe.Pointer(&props))); props != 0 { // get_Properties
		t := NewHString(s.Title)
		ComCall(props, 7, uintptr(t)) // put_Title
		t.Delete()
		Release(props)
	}
	if s.Text != "" {
		t := NewHString(s.Text)
		ComCall(pkg, 16, uintptr(t)) // SetText
		t.Delete()
	}
	if s.Link != "" {
		if f, err := ActivationFactory("Windows.Foundation.Uri", iidUriFactory); err == nil {
			t := NewHString(s.Link)
			var uri uintptr
			ComCall(f, 6, uintptr(t), uintptr(unsafe.Pointer(&uri))) // CreateUri
			t.Delete()
			Release(f)
			if uri != 0 {
				ComCall(pkg, 17, uri) // SetUri
				Release(uri)
			}
		}
	}
	if len(sharing.files) > 0 {
		it := newItems(sharing.files)
		ComCall(pkg, 22, it) // SetStorageItemsReadOnly
		Release(it)
	}
}

// ErrNoShareUI is the share UI's absence (before Windows 10).
var ErrNoShareUI = errors.New("this Windows has no share UI")

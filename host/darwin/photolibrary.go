//go:build darwin && !ios

package darwin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/photos on a Mac, with PhotoKit as on iOS ("photoLibrary"):
// "current" and "request" answer what's allowed, "list" JSON (id, width,
// height, created in ms, screenshot), "thumbnail" a JPEG's path in the
// caches and "export" JSON (path, name). macOS has no limited library
// (nor its picker): "manage" does nothing. Before the user has allowed
// it, "list" answers none and the others "not allowed", without asking:
// Request asks, with the app's NSPhotoLibraryUsageDescription.
//
// asuka/library ("library": "save"): movies and pictures go to Photos
// (after asking to add to it, NSPhotoLibraryAddUsageDescription),
// answering with the asset's identifier; downloads to ~/Downloads and
// music to ~/Music, under the folder asked, answering with the file's
// path.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["photoLibrary"] = h.photoLibraryModule()
		m["library"] = h.libraryModule()
	})
}

// PHAccessLevel.
const (
	phlAddOnly   = 1
	phlReadWrite = 2
)

// Seams: what asks the system, which tests replace.
var (
	// phlStatus is PhotoKit's authorization status for an access level.
	phlStatus = func(level int) int {
		if !phlLoad() {
			return 1
		}
		return SendInt(Class("PHPhotoLibrary"), "authorizationStatusForAccessLevel:", uintptr(level))
	}
	// phlRequest asks the user, then calls done on the main thread with
	// the status.
	phlRequest = func(level int, done func(int)) {
		if !phlLoad() {
			done(1)
			return
		}
		var b objc.Block
		b = NewBlock(func(_ objc.Block, status int) {
			theHost.Post(func() {
				b.Release()
				done(status)
			})
		})
		Send(Class("PHPhotoLibrary"), "requestAuthorizationForAccessLevel:handler:", uintptr(level), uintptr(b))
	}
	// libDir is where library.save puts a collection that isn't Photos':
	// Downloads (0) and Music (2).
	libDir = func(collection int) (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if collection == 2 {
			return filepath.Join(home, "Music"), nil
		}
		return filepath.Join(home, "Downloads"), nil
	}
)

var phlRequestImage func(obj ID, sel SEL, asset ID, size Size, mode int, opts ID, handler ID) int32

var phlLoaded = sync.OnceValue(func() bool {
	if framework("Photos") == 0 || !HasClass("PHPhotoLibrary") {
		return false
	}
	purego.RegisterFunc(&phlRequestImage, MsgSendAddr())
	return true
})

func phlLoad() bool { return phlLoaded() }

// phlAccess is a status as asuka/photos' wire names it.
func phlAccess(s int) string {
	switch s {
	case 3:
		return "allowed"
	case 4:
		return "limited"
	case 1, 2:
		return "denied"
	}
	return "notAsked"
}

func phlReadable() bool {
	s := phlStatus(phlReadWrite)
	return s == 3 || s == 4
}

func phlAsset(id string) (ID, error) {
	a := Send(Send(Class("PHAsset"), "fetchAssetsWithLocalIdentifiers:options:", uintptr(Array(String(id))), 0), "firstObject")
	if a == 0 {
		return 0, errors.New("no such photo")
	}
	return a, nil
}

// phlThumbName is a thumbnail's file name: the asset's and its edit's,
// hashed, and the size.
func phlThumbName(id string, modified float64, size int) string {
	sum := sha256.Sum256([]byte(id + strconv.FormatFloat(modified, 'f', -1, 64)))
	return fmt.Sprintf("%s-%d.jpg", hex.EncodeToString(sum[:16]), size)
}

// phlCacheDir is a folder of the app's caches, made.
func phlCacheDir(name string) (string, error) {
	f, err := cacheFile(name, "")
	if err != nil {
		return "", err
	}
	return filepath.Dir(f), nil
}

// phlJPEG writes an NSImage as a JPEG.
func phlJPEG(img ID, path string, quality float64) error {
	tiff := Send(img, "TIFFRepresentation")
	if tiff == 0 {
		return errors.New("no thumbnail")
	}
	rep := Send(Class("NSBitmapImageRep"), "imageRepWithData:", uintptr(tiff))
	if rep == 0 {
		return errors.New("no thumbnail")
	}
	props := Dict(Const(AppKit(), "NSImageCompressionFactor"), NumberFloat(quality))
	data := Send(rep, "representationUsingType:properties:", 3, uintptr(props)) // NSBitmapImageFileTypeJPEG
	if data == 0 {
		return errors.New("no thumbnail")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, Bytes(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (h *Host) photoLibraryModule() map[string]method {
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			reply([]byte(phlAccess(phlStatus(phlReadWrite))), nil)
		},
		"request": func(_ []byte, reply func([]byte, error)) {
			if s := phlStatus(phlReadWrite); s != 0 {
				reply([]byte(phlAccess(s)), nil)
				return
			}
			phlRequest(phlReadWrite, func(s int) { reply([]byte(phlAccess(s)), nil) })
		},
		// macOS has no limited library to manage.
		"manage": func(_ []byte, reply func([]byte, error)) { reply(nil, nil) },
		"list": func(args []byte, reply func([]byte, error)) {
			if !phlReadable() {
				reply([]byte("[]"), nil)
				return
			}
			o := struct {
				Screenshots bool `json:"screenshots"`
				Offset      int  `json:"offset"`
				Limit       int  `json:"limit"`
			}{Limit: 100}
			json.Unmarshal(args, &o)
			h.offMain(func() ([]byte, error) {
				opts := Autorelease(New("PHFetchOptions"))
				Send(opts, "setSortDescriptors:", uintptr(Array(Send(Class("NSSortDescriptor"), "sortDescriptorWithKey:ascending:", uintptr(String("creationDate")), 0))))
				if o.Screenshots {
					// PHAssetMediaSubtypePhotoScreenshot, written in: the
					// format's arguments are variadic.
					Send(opts, "setPredicate:", uintptr(Send(Class("NSPredicate"), "predicateWithFormat:", uintptr(String("(mediaSubtype & 4) != 0")))))
				}
				found := Send(Class("PHAsset"), "fetchAssetsWithMediaType:options:", 1, uintptr(opts)) // image
				n := SendInt(found, "count")
				out := []map[string]any{}
				for i := max(o.Offset, 0); i < min(o.Offset+o.Limit, n); i++ {
					a := Send(found, "objectAtIndex:", uintptr(i))
					var created int64
					if d := Send(a, "creationDate"); d != 0 {
						created = int64(MsgFloat(d, Sel("timeIntervalSince1970")) * 1000)
					}
					out = append(out, map[string]any{
						"id": GoString(Send(a, "localIdentifier")), "width": SendInt(a, "pixelWidth"), "height": SendInt(a, "pixelHeight"),
						"created": created, "screenshot": SendInt(a, "mediaSubtypes")&4 != 0,
					})
				}
				return json.Marshal(out)
			}, reply)
		},
		"thumbnail": func(args []byte, reply func([]byte, error)) {
			if !phlReadable() {
				reply(nil, errNotAllowed)
				return
			}
			o := struct {
				ID   string `json:"id"`
				Size int    `json:"size"`
			}{Size: 300}
			json.Unmarshal(args, &o)
			h.offMain(func() ([]byte, error) {
				a, err := phlAsset(o.ID)
				if err != nil {
					return nil, err
				}
				var stamp float64
				if d := Send(a, "modificationDate"); d != 0 {
					stamp = MsgFloat(d, Sel("timeIntervalSince1970"))
				}
				dir, err := phlCacheDir("asuka-photo-thumbs")
				if err != nil {
					return nil, err
				}
				path := filepath.Join(dir, phlThumbName(GoString(Send(a, "localIdentifier")), stamp, o.Size))
				if _, err := os.Stat(path); err == nil {
					return []byte(path), nil
				}
				po := Autorelease(New("PHImageRequestOptions"))
				Send(po, "setDeliveryMode:", 1) // highQualityFormat
				Send(po, "setResizeMode:", 1)   // fast
				Send(po, "setNetworkAccessAllowed:", 1)
				Send(po, "setSynchronous:", 1)
				var image ID
				got := NewBlock(func(_ objc.Block, img, info ID) { image = Retain(img) })
				defer got.Release()
				sz := float64(o.Size)
				phlRequestImage(Send(Class("PHImageManager"), "defaultManager"), Sel("requestImageForAsset:targetSize:contentMode:options:resultHandler:"),
					a, Size{Width: sz, Height: sz}, 1 /* aspectFill */, po, ID(got))
				if image == 0 {
					return nil, errors.New("no thumbnail")
				}
				defer Release(image)
				if err := phlJPEG(image, path, 0.8); err != nil {
					return nil, err
				}
				return []byte(path), nil
			}, reply)
		},
		"export": func(args []byte, reply func([]byte, error)) {
			if !phlReadable() {
				reply(nil, errNotAllowed)
				return
			}
			var o struct {
				ID string `json:"id"`
			}
			json.Unmarshal(args, &o)
			a, err := phlAsset(o.ID)
			if err != nil {
				reply(nil, err)
				return
			}
			const photo, fullSize = 1, 5 // PHAssetResourceType
			rs := ArrayItems(Send(Class("PHAssetResource"), "assetResourcesForAsset:", uintptr(a)))
			find := func(t int) ID {
				for _, r := range rs {
					if SendInt(r, "type") == t {
						return r
					}
				}
				return 0
			}
			// The photo as edited, if it was.
			r := find(fullSize)
			if r == 0 {
				r = find(photo)
			}
			if r == 0 && len(rs) > 0 {
				r = rs[0]
			}
			if r == 0 {
				reply(nil, errors.New("no such photo"))
				return
			}
			name := GoString(Send(r, "originalFilename"))
			if p := find(photo); p != 0 {
				name = GoString(Send(p, "originalFilename"))
			}
			ext := strings.TrimPrefix(filepath.Ext(GoString(Send(r, "originalFilename"))), ".")
			if ext == "" {
				ext = "jpg"
			}
			path, err := cacheFile("asuka-photo-exports", "."+ext)
			if err != nil {
				reply(nil, err)
				return
			}
			wo := Autorelease(New("PHAssetResourceRequestOptions"))
			Send(wo, "setNetworkAccessAllowed:", 1)
			var done objc.Block
			done = NewBlock(func(_ objc.Block, e ID) {
				var err error
				if e != 0 {
					err = errorText(e, "couldn't export the photo")
				}
				h.Post(func() {
					done.Release()
					if err != nil {
						reply(nil, err)
						return
					}
					b, err := json.Marshal(map[string]string{"path": path, "name": name})
					reply(b, err)
				})
			})
			Send(Send(Class("PHAssetResourceManager"), "defaultManager"), "writeDataForAssetResource:toFile:options:completionHandler:",
				uintptr(r), uintptr(FileURL(path)), uintptr(wo), uintptr(done))
		},
	}
}

// libArgs is library.save's JSON.
type libArgs struct {
	Path       string `json:"path"`
	Collection int    `json:"collection"`
	Name       string `json:"name"`
	Folder     string `json:"folder"`
	MIME       string `json:"mime"`
	Move       bool   `json:"move"`
}

// libSaveFile copies (or moves) src into dir/folder as name, numbered
// "name (1).ext"... if it's taken, and answers the new path.
func libSaveFile(src, dir, folder, name string, move bool) (string, error) {
	if folder != "" {
		dir = filepath.Join(dir, folder)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	dst := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dst); os.IsNotExist(err) {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
	if move {
		if err := os.Rename(src, dst); err == nil {
			return dst, nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if move {
		os.Remove(src)
	}
	return dst, nil
}

func (h *Host) libraryModule() map[string]method {
	return map[string]method{
		"save": func(args []byte, reply func([]byte, error)) {
			var o libArgs
			if err := json.Unmarshal(args, &o); err != nil || o.Path == "" {
				reply(nil, errors.New("bad arguments"))
				return
			}
			if _, err := os.Stat(o.Path); err != nil {
				reply(nil, errors.New("no file at "+o.Path))
				return
			}
			name := o.Name
			if name == "" {
				name = filepath.Base(o.Path)
			}
			if o.Collection == 1 || o.Collection == 3 {
				libSavePhotos(o, name, reply)
				return
			}
			dir, err := libDir(o.Collection)
			if err != nil {
				reply(nil, err)
				return
			}
			h.offMain(func() ([]byte, error) {
				p, err := libSaveFile(o.Path, dir, o.Folder, name, o.Move)
				return []byte(p), err
			}, reply)
		},
	}
}

// libSavePhotos adds a video (1) or picture (3) to Photos.
func libSavePhotos(o libArgs, name string, reply func([]byte, error)) {
	save := func(status int) {
		if status != 3 && status != 4 {
			reply(nil, errNotAllowed)
			return
		}
		var mu sync.Mutex
		var id string
		changes := NewBlock(func(_ objc.Block) {
			r := Send(Class("PHAssetCreationRequest"), "creationRequestForAsset")
			opts := Autorelease(New("PHAssetResourceCreationOptions"))
			Send(opts, "setOriginalFilename:", uintptr(String(name)))
			Send(opts, "setShouldMoveFile:", Bool(o.Move))
			kind := uintptr(1) // PHAssetResourceTypePhoto
			if o.Collection == 1 {
				kind = 2 // video
			}
			Send(r, "addResourceWithType:fileURL:options:", kind, uintptr(FileURL(o.Path)), uintptr(opts))
			if p := Send(r, "placeholderForCreatedAsset"); p != 0 {
				mu.Lock()
				id = GoString(Send(p, "localIdentifier"))
				mu.Unlock()
			}
		})
		var done objc.Block
		done = NewBlock(func(_ objc.Block, ok bool, e ID) {
			var err error
			if !ok {
				err = errorText(e, "couldn't save")
			}
			theHost.Post(func() {
				changes.Release()
				done.Release()
				if err != nil {
					reply(nil, err)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				reply([]byte(id), nil)
			})
		})
		Send(Send(Class("PHPhotoLibrary"), "sharedPhotoLibrary"), "performChanges:completionHandler:", uintptr(changes), uintptr(done))
	}
	if s := phlStatus(phlAddOnly); s != 0 {
		save(s)
		return
	}
	phlRequest(phlAddOnly, save)
}

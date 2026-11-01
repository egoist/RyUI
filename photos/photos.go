// Package photos reads the user's photo library, with their permission:
// PhotoKit on iOS, MediaStore on Android. It's for an app whose screen is
// the library, as a gallery or an editor is. To let the user pick a few
// photos, device.PickPhotos asks for no permission: prefer it where it'll
// do. Google Play asks an app that reads the library why it does, in the
// Play Console.
//
//	go func() {
//		if a, _ := photos.Request(); a == photos.Denied {
//			return
//		}
//		assets, _ := photos.List(photos.Query{Screenshots: true, Limit: 60})
//		thumb, _ := photos.Thumbnail(assets[0].ID, 300) // a file, for ui.Image
//	}()
//
// Every call waits for the platform: not on the main thread.
package photos

import (
	"encoding/json"
	"time"

	"github.com/egoist/ryui/ui"
)

// Access is what the user has allowed.
type Access uint8

const (
	NotAsked Access = iota
	Denied
	Limited // only the photos the user chose (iOS 14, Android 14)
	Allowed
)

func access(s string) Access {
	switch s {
	case "allowed":
		return Allowed
	case "limited":
		return Limited
	case "denied":
		return Denied
	}
	return NotAsked
}

// Asset is a photo in the library.
type Asset struct {
	ID         string `json:"id"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	CreatedMs  int64  `json:"created"`
	Screenshot bool   `json:"screenshot"`
}

// Created is when it was taken.
func (a Asset) Created() time.Time { return time.UnixMilli(a.CreatedMs) }

// Query says which photos: newest first, from Offset, at most Limit (0: 100).
type Query struct {
	// Screenshots keeps only those. iOS knows what's a screenshot; on
	// Android it's what's in a Screenshots folder.
	Screenshots   bool
	Offset, Limit int
}

// File is a photo as a file: a copy in the app's caches.
type File struct {
	Path string `json:"path"`
	Name string `json:"name"` // its name in the library, as IMG_0042.HEIC
}

// Current is what the user has allowed, without asking.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Current() (Access, error) {
	b, err := ui.CallWait("photoLibrary", "current", nil)
	return access(string(b)), err
}

// Request asks the user the first time, and says what they've allowed.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request() (Access, error) {
	b, err := ui.CallWait("photoLibrary", "request", nil)
	return access(string(b)), err
}

// ManageLimited lets a user who allowed only some photos change which:
// iOS's limited library picker, Android's permission prompt. It returns
// once that's closed; list again.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func ManageLimited() error {
	_, err := ui.CallWait("photoLibrary", "manage", nil)
	return err
}

// List returns the photos q asks for.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func List(q Query) ([]Asset, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	args, _ := json.Marshal(map[string]any{"screenshots": q.Screenshots, "offset": q.Offset, "limit": q.Limit})
	b, err := ui.CallWait("photoLibrary", "list", args)
	if err != nil {
		return nil, err
	}
	var as []Asset
	err = json.Unmarshal(b, &as)
	return as, err
}

// Thumbnail is a JPEG file of the photo, at least size pixels on its short
// side, for ui.Image; kept in the app's caches for the next time.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Thumbnail(id string, size int) (string, error) {
	args, _ := json.Marshal(map[string]any{"id": id, "size": size})
	b, err := ui.CallWait("photoLibrary", "thumbnail", args)
	return string(b), err
}

// Export copies the photo's full image, as it is in the library (HEIC,
// JPEG, PNG), to a file. iCloud photos download first.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Export(id string) (File, error) {
	args, _ := json.Marshal(map[string]any{"id": id})
	b, err := ui.CallWait("photoLibrary", "export", args)
	if err != nil {
		return File{}, err
	}
	var f File
	err = json.Unmarshal(b, &f)
	return f, err
}

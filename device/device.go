// Package device is what the platform offers apps beyond views: what
// device it is, its in-app browser, its photo picker, the clipboard's
// text, keeping the screen on, its document picker.
package device

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/egoist/ryui/ui"
)

// Info is the device and the app.
type Info struct {
	Model         string `json:"model"`         // "iPhone", "Pixel 9"
	System        string `json:"system"`        // "iOS", "Android"
	SystemVersion string `json:"systemVersion"` // "26.0", "16"
	AppVersion    string `json:"appVersion"`    // ryui.json's version
	Build         string `json:"build"`         // ryui.json's build
}

// GetInfo is the device and the app. Not on the main thread.
func GetInfo() (Info, error) {
	var i Info
	b, err := ui.CallWait("device", "info", nil)
	if err == nil {
		err = json.Unmarshal(b, &i)
	}
	return i, err
}

// Browse shows a web page in the app: SFSafariViewController, a Custom
// Tab. (ui.OpenURL leaves the app for the browser.) Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Browse(url string) {
	ui.Call("browser", "open", []byte(url), nil)
}

// PickPhotos shows the system's photo picker (PHPicker, Android's Photo
// Picker: no permission asked), for up to limit images; done gets their
// files, which ui.Image shows, or none if the user cancelled. Main
// thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func PickPhotos(limit int, done func(paths []string, err error)) {
	if limit < 1 {
		limit = 1
	}
	if limit > 255 {
		limit = 255
	}
	ui.Call("photos", "pick", []byte{byte(limit)}, func(b []byte, err error) {
		var paths []string
		if s := string(b); s != "" {
			paths = strings.Split(s, "\n")
		}
		done(paths, err)
	})
}

// Clipboard reads the clipboard's text (ok false: it has none); ui.Copy
// writes it. iOS asks the user the first time an app reads what another
// app put there. Not on the main thread.
func Clipboard() (text string, ok bool, err error) {
	b, err := ui.CallWait("device", "clipboard", nil)
	if err != nil || len(b) == 0 || b[0] == 0 {
		return "", false, err
	}
	return string(b[1:]), true, nil
}

// KeepAwake keeps the screen on (the system's idle timer off) while on is
// true: a video, a recipe, a navigation screen. Main thread.
func KeepAwake(on bool) {
	arg := []byte{0}
	if on {
		arg[0] = 1
	}
	ui.Call("device", "keepAwake", arg, nil)
}

// File is a file the user picked.
type File struct {
	Path string `json:"path"` // a copy in the app's caches: the app's to keep or move
	Name string `json:"name"` // its name where it was ("Invoice.pdf")
	Type string `json:"type"` // its MIME type, if known ("application/pdf")
	Size int64  `json:"size"` // bytes
}

// FileOptions are what PickFiles offers.
type FileOptions struct {
	// Types are MIME types ("application/pdf", "image/*", "text/csv");
	// none: any file.
	Types []string
	// Multiple lets the user pick several.
	Multiple bool
}

// PickFiles shows the system's document picker (iOS's Files browser,
// Android's storage picker: no permission asked) and calls done with
// copies of the files picked, or none if the user cancelled. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func PickFiles(o FileOptions, done func(files []File, err error)) {
	ui.Call("files", "pick", encodeFileOptions(o), func(b []byte, err error) {
		var files []File
		if err == nil && len(b) > 0 {
			err = json.Unmarshal(b, &files)
		}
		done(files, err)
	})
}

// encodeFileOptions: u8 multiple, then the types, a line each.
func encodeFileOptions(o FileOptions) []byte {
	b := []byte{0}
	if o.Multiple {
		b[0] = 1
	}
	return append(b, strings.Join(o.Types, "\n")...)
}

// ClipboardKinds says what the clipboard holds, without reading it (iOS
// doesn't ask the user for this): to show a Paste button only when
// there's something to paste.
type ClipboardKinds struct {
	Text, Image, URL bool
}

// ClipboardHas says what the clipboard holds. Not on the main thread.
func ClipboardHas() (ClipboardKinds, error) {
	b, err := ui.CallWait("clipboard", "has", nil)
	if err != nil || len(b) < 3 {
		return ClipboardKinds{}, err
	}
	return ClipboardKinds{Text: b[0] == 1, Image: b[1] == 1, URL: b[2] == 1}, nil
}

// ClipboardImage reads the clipboard's image into a file in the app's
// caches, in the format the copying app gave it (PNG, JPEG, HEIC...; its
// extension says), and reports its path (ok false: it has none). iOS asks the user the first time
// the app reads what another app put there. Not on the main thread.
func ClipboardImage() (path string, ok bool, err error) {
	b, err := ui.CallWait("clipboard", "image", nil)
	if err != nil || len(b) == 0 {
		return "", false, err
	}
	return string(b), true, nil
}

// CopyImage puts the image file at path (PNG, JPEG, HEIC...) on the
// clipboard, for pasting in another app; done (if not nil) gets whether
// it went. Main thread.
func CopyImage(path string, done func(err error)) {
	ui.Call("clipboard", "copyImage", []byte(path), func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}

var (
	clipMu       sync.Mutex
	clipWatchers = map[*func()]bool{}
	clipRunning  bool
)

// OnClipboardChange calls f, on the main thread, each time the clipboard
// changes while the app is in the foreground (another app's copy is seen
// when the app comes back, on iOS), until stop. Read it then with
// Clipboard, ClipboardImage or ClipboardHas.
func OnClipboardChange(f func()) (stop func()) {
	fp := &f
	clipMu.Lock()
	clipWatchers[fp] = true
	start := !clipRunning
	clipRunning = true
	clipMu.Unlock()
	if start {
		go followClipboard()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			clipMu.Lock()
			delete(clipWatchers, fp)
			last := len(clipWatchers) == 0
			clipMu.Unlock()
			if last {
				ui.Post(func() { ui.Call("clipboard", "stop", nil, nil) })
			}
		})
	}
}

// followClipboard waits for each change ("next" answers at the next).
func followClipboard() {
	for {
		_, err := ui.CallWait("clipboard", "next", nil)
		clipMu.Lock()
		fs := make([]func(), 0, len(clipWatchers))
		for f := range clipWatchers {
			fs = append(fs, *f)
		}
		// Stopped as another began watching: wait on for that one.
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			clipRunning = false
			clipMu.Unlock()
			return
		}
		clipMu.Unlock()
		if err != nil {
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f()
			}
		})
	}
}

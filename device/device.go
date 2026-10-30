// Package device is what the platform offers apps beyond views: what
// device it is, its in-app browser, its photo picker, the clipboard's
// text, keeping the screen on.
package device

import (
	"encoding/json"
	"strings"

	"github.com/egoist/nui/ui"
)

// Info is the device and the app.
type Info struct {
	Model         string `json:"model"`         // "iPhone", "Pixel 9"
	System        string `json:"system"`        // "iOS", "Android"
	SystemVersion string `json:"systemVersion"` // "26.0", "16"
	AppVersion    string `json:"appVersion"`    // nui.json's version
	Build         string `json:"build"`         // nui.json's build
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
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
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
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
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

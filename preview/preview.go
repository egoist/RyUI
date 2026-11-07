// Package preview shows files with the system's viewer, as Mail shows
// an attachment: Quick Look on iOS and the Mac (PDFs, images, video,
// audio, Office and iWork documents, text, 3D models), the app the user
// opens such files with on Android and Windows.
//
// iOS presents Quick Look over the app's screen, with its share and
// Markup buttons; the user swipes between the files. The Mac shows Quick
// Look's panel, with its arrows between the files and its own buttons.
// Android has no system viewer: it opens the file in the app the user
// picks (a PDF in Drive's viewer, an image in Photos), through the app's
// FileProvider, with the right to read it and no other; one file at a
// time, as every Android mail app opens attachments. Windows opens the
// file in its default app, as Explorer does.
package preview

import (
	"encoding/json"
	"errors"
	"mime"
	"path/filepath"
	"strings"

	"github.com/egoist/ryui/ui"
)

// ErrNoViewer: nothing on the device can show the file (Android: no app
// opens its type; iOS and the Mac: Quick Look can't read it; Windows: no
// app is set for its extension).
var ErrNoViewer = errors.New("preview: nothing can show this file")

// Options say how Show shows the files.
type Options struct {
	// Start is the index of the file shown first. Android and Windows,
	// which show one file, show that one.
	Start int
	// From is the view the viewer grows from (iOS's Quick Look): usually
	// what the user tapped. Default: none.
	From ui.Node
	// Edit lets the user mark up PDFs and images with Markup, which saves
	// into the files: the app's own (iOS; elsewhere the viewer's app
	// edits as it does).
	Edit bool
}

// Show shows the files at sources with the system's viewer: a file's
// path, an asset's (as ui.Image takes it), or an http(s) URL, which the
// host downloads to the app's caches first (keeping its name, or giving
// it the extension of the type the server says). done (if not nil) is
// called on the main thread once the viewer is closed (iOS, the Mac),
// once the user is back in the app (Android), or once the file is
// handed to its app (Windows, which doesn't say when it's closed); with
// ErrNoViewer if nothing can show it, or another error (no such file, a
// download that failed). Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Show(sources []string, o Options, done func(err error)) {
	if len(sources) == 0 {
		if done != nil {
			done(errors.New("preview: Show with no files"))
		}
		return
	}
	ui.Call("preview", "show", encode(sources, o), func(_ []byte, err error) {
		if err != nil && err.Error() == "no viewer" {
			err = ErrNoViewer
		}
		if done != nil {
			done(err)
		}
	})
}

// file is a file to show, as the hosts take it.
type file struct {
	Source string `json:"source"`
	Type   string `json:"type,omitempty"` // MIME type, from the extension
}

func encode(sources []string, o Options) []byte {
	start := o.Start
	if start < 0 || start >= len(sources) {
		start = 0
	}
	files := make([]file, len(sources))
	for i, s := range sources {
		t, _, _ := strings.Cut(mime.TypeByExtension(filepath.Ext(s)), ";")
		files[i] = file{Source: ui.AssetURL(s), Type: t}
	}
	b, _ := json.Marshal(map[string]any{"files": files, "start": start, "from": ui.ViewID(o.From), "edit": o.Edit})
	return b
}

// Package recent keeps the documents the user opened lately where the
// system offers them again (NSDocumentController's recent documents,
// SHAddToRecentDocs, GtkRecentManager): the app's Open Recent menu and
// its Dock menu on macOS, the Recent category of its jump list on
// Windows, the user's recently used files on Linux (the file chooser's
// and the file manager's Recent). Choosing one of them opens the file
// with the app: ui.OnOpenFiles gets it, as any file the app is opened
// with.
//
//	ui.OnOpenFiles(func(paths []string) {
//		for _, p := range paths {
//			openDocument(p)
//			recent.Add(p, nil)
//		}
//	})
//
// Record each document the user opens or saves, including one chosen in
// the app's own file dialog. A phone has no recent documents of an app's
// (iOS's Files and Android's file pickers keep their own): there each
// call does nothing and List is empty.
package recent

import (
	"encoding/json"
	"path/filepath"

	"github.com/egoist/asuka/ui"
)

// Add records path as a document the user opened: first among the recent
// documents (a path already there moves up). A relative path is taken
// from the working directory. done, if not nil, gets what failed, on the
// main thread. Main thread.
//
// macOS: the app's recent documents (noteNewRecentDocumentURL:), which
// the File menu's Open Recent and the Dock menu list, ten at most, and
// keep across launches. Windows: the shell's recent items for the app
// (SHAddToRecentDocs), which its jump list's Recent shows for the file
// types the app opens (asuka.json's fileTypes), and the app's own list,
// which List returns. Linux: the user's recently used files
// (~/.local/share/recently-used.xbel), as the app's.
func Add(path string, done func(err error)) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	ui.Call("recent", "add", []byte(path), func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}

// Clear removes the app's recent documents, as the Open Recent menu's
// Clear Menu does: the app's own on macOS, the jump list's on Windows,
// those the app added on Linux (the user's other recent files stay).
// Offer it where the app clears its history. done may be nil. Main
// thread.
func Clear(done func(err error)) {
	ui.Call("recent", "clear", nil, func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}

// List calls done, on the main thread, with the app's recent documents,
// newest first, without those that no longer exist: for an Open Recent
// of the app's own (a menu on Windows, a list on a start screen). Main
// thread.
func List(done func(paths []string, err error)) {
	ui.Call("recent", "list", nil, func(b []byte, err error) {
		var paths []string
		if err == nil && len(b) > 0 {
			err = json.Unmarshal(b, &paths)
		}
		done(paths, err)
	})
}

// Package library saves files where the user, and other apps, find them
// (expo-media-library's saveToLibraryAsync): a video to the gallery, a
// song to the music, anything to Downloads. Android: the MediaStore
// collections (from Android 10; before, the shared folders). iOS: the
// Photos library for videos and pictures, else the app's Documents,
// which the Files app shows.
//
// An app that imports it is given what it needs: Android's storage
// permission before Android 10, iOS's purpose string for adding to
// Photos (nui.json's ios.usage.NSPhotoLibraryAddUsageDescription says
// it better), and its Documents in Files.
package library

import (
	"encoding/json"
	"mime"
	"path/filepath"

	"github.com/egoist/nui/ui"
)

// Collection is where a file goes.
type Collection uint8

const (
	Downloads Collection = iota // Android's Downloads; iOS's Files
	Movies                      // the gallery's videos; iOS's Photos
	Music                       // music players' songs; iOS's Files
	Pictures                    // the gallery's pictures; iOS's Photos
)

// Options say where and as what a file is saved.
type Options struct {
	Collection Collection
	// Name is the file's name there (default: the file's own). A name
	// taken gets a number.
	Name string
	// Folder is a folder of the collection's for the app's files
	// ("Downbit"); empty: the collection's own.
	Folder string
	// MIME is its type (default: from its extension).
	MIME string
	// Move removes the file once it's saved (a download in the app's
	// caches): no second copy.
	Move bool
}

// Save copies the file at path into o.Collection, off the main thread
// (a large video takes a while), then calls done on the main thread with
// where it went: a content:// URI (Android), a Photos asset's identifier
// or the file in Documents (iOS). It asks for what it needs the first
// time. Main thread.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Save(path string, o Options, done func(location string, err error)) {
	if o.Name == "" {
		o.Name = filepath.Base(path)
	}
	if o.MIME == "" {
		o.MIME = mime.TypeByExtension(filepath.Ext(o.Name))
		if o.MIME == "" {
			o.MIME = "application/octet-stream"
		}
	}
	args, _ := json.Marshal(map[string]any{
		"path": path, "collection": int(o.Collection), "name": o.Name, "folder": o.Folder, "mime": o.MIME, "move": o.Move,
	})
	ui.Call("library", "save", args, func(b []byte, err error) {
		if done != nil {
			done(string(b), err)
		}
	})
}

// Package shell does what a desktop's file manager does with a file:
// shows it in its folder (Reveal), opens it in its app (Open), moves it
// to the trash (Trash), and sounds the system's alert (Beep).
//
// macOS goes through NSWorkspace (Finder), Windows through the shell
// (Explorer, the Recycle Bin), Linux through GIO and the file manager's
// D-Bus interface (org.freedesktop.FileManager1: Files, Dolphin, Nemo,
// Thunar...). Phones have no file manager of these, and the web no
// files of the user's: there Reveal and Trash answer ErrUnsupported, Open
// shows the file in a new tab on the web (what the browser shows:
// images, video, audio, PDFs, text), and Beep does nothing.
//
// asuka/preview shows a file with the system's viewer (Quick Look on a
// Mac, the file's app elsewhere); Open is its app on a Mac too, and
// opens a folder in the file manager.
package shell

import (
	"errors"
	"strings"

	"github.com/egoist/asuka/ui"
)

// ErrUnsupported: the platform can't do it (Reveal and Trash on phones
// and the web, Open on phones).
var ErrUnsupported = errors.New("shell: not on this platform")

// ErrNoApp: no app is set to open the file's type (Open).
var ErrNoApp = errors.New("shell: no app opens this file")

// Reveal shows the file or folder at path in the file manager, selected
// in a window of its folder: Finder, Explorer (a window already showing
// the folder is used), or the file manager the desktop has (its
// FileManager1 interface; without one, the folder opens in its app,
// unselected). done (if not nil) gets an error if there's nothing at
// path. Main thread.
func Reveal(path string, done func(err error)) {
	call("reveal", path, done)
}

// Open opens the file at path in the app the user opens its type with,
// as a double click in the file manager does, and a folder in the file
// manager. done (if not nil) is called once it's handed over, with
// ErrNoApp if no app opens it (Windows asks the user which app first, as
// Explorer does) or another error (nothing at path). Main thread.
func Open(path string, done func(err error)) {
	call("open", path, done)
}

// Trash moves the file or folder at path to the trash (the Recycle Bin
// on Windows), where the user can put it back. It never deletes: where
// the item can't go to the trash (a volume without one: a network share,
// many removable drives; on Windows also an item too big for the Recycle
// Bin), done gets an error and the item stays. Main thread.
func Trash(path string, done func(err error)) {
	call("trash", path, done)
}

// Beep sounds the system's alert sound, as a refused key press does.
// Phones and the web have none: nothing happens there. Main thread.
func Beep() {
	ui.Call("shell", "beep", nil, nil)
}

func call(method, path string, done func(error)) {
	if path == "" {
		if done != nil {
			done(errors.New("shell: no path"))
		}
		return
	}
	ui.Call("shell", method, []byte(path), func(_ []byte, err error) {
		if done != nil {
			done(hostError(err))
		}
	})
}

// hostError maps the hosts' answers: "unsupported" (and a phone host,
// which has no shell module), "no app".
func hostError(err error) error {
	if err == nil {
		return nil
	}
	switch s := err.Error(); {
	case s == "unsupported" || strings.HasPrefix(s, "no module method shell."):
		return ErrUnsupported
	case s == "no app":
		return ErrNoApp
	}
	return err
}

//go:build darwin && !ios

package darwin_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/document"
	"github.com/egoist/asuka/host/darwin"
	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// until runs what the host posts (the documents' reads and writes come
// back so) until cond holds.
func until(t *testing.T, h *darwin.Host, what string, cond func() bool) {
	t.Helper()
	for range 500 {
		ok := false
		darwin.OnMain(func() {
			h.RunPosted()
			ok = cond()
		})
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

// A document app on a Mac: its main window stays hidden and each document
// has an NSWindow of its own, titled after it, with the dot in its close
// button while it's edited and its file in the title bar; the File menu
// has its items with Open Recent and Close where it puts them (Close
// closing the window in use as its close button does: saved in place).
func TestDocumentWindows(t *testing.T) {
	t.Setenv("ASUKA_DATA", t.TempDir())
	keep := boot.KeepRunning
	t.Cleanup(func() { boot.KeepRunning = keep; ui.SetWindowOptions(ui.WindowOptions{}) })
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	os.WriteFile(p, []byte("hello"), 0o644)

	docs := document.New(document.Options[string]{
		Types: []document.Type{{Name: "Plain Text", Extensions: []string{"txt"}}},
		Read:  func(r io.Reader) (string, error) { b, err := io.ReadAll(r); return string(b), err },
		Write: func(w io.Writer, s string) error { _, err := io.WriteString(w, s); return err },
		View: func(d *document.Document[string]) ui.Node {
			return ui.TextField(ui.TextFieldProps{Value: d.Content(), Multiline: true})
		},
	})
	ui.SetWindowOptions(ui.WindowOptions{Hidden: true})
	h := darwin.Mount(t, docs.Root)
	until(t, h, "an untitled document's window", func() bool { return len(h.Windows()) == 1 })
	darwin.OnMain(func() {
		if SendBool(h.MainWindow(), "isVisible") {
			darwin.Fatal(t, "the main window shows")
		}
		w := h.Windows()[0]
		if got := GoString(Send(w, "title")); got != "Untitled" || SendBool(w, "isDocumentEdited") {
			darwin.Fatalf(t, "title %q, edited %v", got, SendBool(w, "isDocumentEdited"))
		}
		// The File menu.
		bar := Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu")
		file := Send(Send(bar, "itemAtIndex:", 1), "submenu")
		var titles []string
		for i := range SendInt(file, "numberOfItems") {
			it := Send(file, "itemAtIndex:", uintptr(i))
			if !SendBool(it, "isSeparatorItem") && !SendBool(it, "isAlternate") {
				titles = append(titles, GoString(Send(it, "title")))
			}
		}
		want := "New Open… Open Recent Close Save Duplicate Save As… Rename… Move To… Revert to Saved"
		if got := strings.Join(titles, " "); got != want {
			darwin.Fatalf(t, "File menu %q", got)
		}
		closeItem := Send(file, "itemWithTitle:", uintptr(String("Close")))
		if SEL(Send(closeItem, "action")) != Sel("performClose:") || GoString(Send(closeItem, "keyEquivalent")) != "w" {
			darwin.Fatal(t, "Close isn't performClose: with ⌘W")
		}
		if Send(Send(file, "itemWithTitle:", uintptr(String("Open Recent"))), "submenu") != darwin.RecentMenu() {
			darwin.Fatal(t, "Open Recent isn't the host's")
		}
	})
	// A file Finder opens: a window of its own, its file in the title bar.
	darwin.OnMain(func() { h.OpenFile(p) })
	until(t, h, "the file's window", func() bool { return len(h.Windows()) == 2 })
	var doc *document.Document[string]
	var nw ID
	darwin.OnMain(func() {
		for _, w := range h.Windows() {
			if GoString(Send(w, "title")) == "notes.txt" {
				nw = w
			}
		}
		if nw == 0 {
			darwin.Fatal(t, "no window titled notes.txt")
		}
		real, _ := filepath.EvalSymlinks(p)
		if u := Send(nw, "representedURL"); u == 0 || (GoString(Send(u, "path")) != p && GoString(Send(u, "path")) != real) {
			darwin.Fatal(t, "the window doesn't represent the file")
		}
		for _, d := range docs.Documents().Get() {
			if d.Path() == p {
				doc = d
			}
		}
		h.Do(func() { doc.Set("hello, world") })
		if !SendBool(nw, "isDocumentEdited") {
			darwin.Fatal(t, "no dot in the close button of an edited document")
		}
		if out := os.Getenv("ASUKA_CONFORMANCE_OUT"); out != "" {
			darwin.Snapshot(nw, filepath.Join(out, "document-edited.png"))
		}
		// Close (⌘W) as AppKit sends it to the window: saved in place,
		// then closed.
		Send(nw, "performClose:", 0)
	})
	until(t, h, "the window closed, its document saved", func() bool {
		b, _ := os.ReadFile(p)
		return len(h.Windows()) == 1 && string(b) == "hello, world"
	})
	// The Dock icon clicked with no window: a new untitled document.
	darwin.OnMain(func() {
		for _, w := range h.Windows() {
			Send(w, "performClose:", 0)
		}
	})
	until(t, h, "no window", func() bool { return len(h.Windows()) == 0 })
	darwin.OnMain(func() {
		h.Reopen()
		if SendBool(h.MainWindow(), "isVisible") {
			darwin.Fatal(t, "the hidden main window came back")
		}
	})
	until(t, h, "a new document's window", func() bool { return len(h.Windows()) == 1 })
}

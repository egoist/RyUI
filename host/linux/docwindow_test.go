//go:build linux && !android

package linux_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/asuka/document"
	"github.com/egoist/asuka/host/linux"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/fileurl"
	"github.com/egoist/asuka/ui"
)

// A document app on Linux: the main window stays hidden, each document
// has a window of its own with the app's main menu, titled "name - App"
// with a * while edited and its folder as the subtitle; the menu's Close
// closes the window in use, which saves it in place; the app quits with
// its last document's window.
func TestDocumentWindows(t *testing.T) {
	t.Setenv("ASUKA_DATA", t.TempDir())
	quits := 0
	saved := boot.Quit
	boot.Quit = func() { quits++ }
	t.Cleanup(func() { boot.Quit = saved; ui.SetWindowOptions(ui.WindowOptions{}) })
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	docs := document.New(document.Options[string]{
		Types:      []document.Type{{Name: "Plain Text", Extensions: []string{"txt"}}},
		Read:       func(r io.Reader) (string, error) { b, err := io.ReadAll(r); return string(b), err },
		Write:      func(w io.Writer, s string) error { _, err := io.WriteString(w, s); return err },
		NoUntitled: true,
		View: func(d *document.Document[string]) ui.Node {
			return ui.TextField(ui.TextFieldProps{Value: d.Content(), Multiline: true})
		},
	})
	ui.SetWindowOptions(ui.WindowOptions{Hidden: true})
	h := linux.Mount(t, docs.Root)
	linux.OnMain(func() {
		if h.MainVisible() {
			linux.Fatalf(t, "the main window shows")
		}
		h.OpenFiles(fileurl.Join([]string{p}))
	})
	app := filepath.Base(os.Args[0])
	linux.Settle(t, h, func() bool {
		ws := h.Windows()
		return len(ws) == 1 && ws[0].Title == "notes.txt - "+app && ws[0].Menu
	})
	linux.OnMain(func() {
		if got := h.Windows()[0].Subtitle; got != dir && got[0] != '~' {
			linux.Fatalf(t, "subtitle %q", got)
		}
		d := docs.Documents().Get()[0]
		ui.Post(func() { d.Set("hello, world") })
	})
	linux.Settle(t, h, func() bool { return h.Windows()[0].Title == "*notes.txt - "+app })
	linux.OnMain(func() {
		if err := h.ChooseMenuItem("Close"); err != nil {
			linux.Fatalf(t, "%v", err)
		}
	})
	linux.Settle(t, h, func() bool {
		b, _ := os.ReadFile(p)
		return len(h.Windows()) == 0 && string(b) == "hello, world" && quits == 1
	})
}

//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// Launch Services' files and links, together: the files reach OnOpenFiles
// at once, as paths, the link OnOpenURL.
func TestOpenURLs(t *testing.T) {
	var files [][]string
	var urls []string
	h, _ := mount(t, func() ui.Node {
		ui.OnOpenFiles(func(ps []string) { files = append(files, ps) })
		ui.OnOpenURL(func(u string) { urls = append(urls, u) })
		return ui.StaticText("x")
	})
	onMain(func() {
		link := Send(Class("NSURL"), "URLWithString:", uintptr(String("myapp://note/1")))
		Send(h.target, "application:openURLs:", 0, uintptr(Array(FileURL("/tmp/a b.md"), FileURL("/tmp/c.md"), link)))
	})
	if fmt.Sprint(files) != "[[/tmp/a b.md /tmp/c.md]]" || fmt.Sprint(urls) != "[myapp://note/1]" {
		t.Fatalf("files %q, urls %q", files, urls)
	}
}

// A quit asks OnQuit's handlers once, which can cancel it, but not as the
// session ends.
func TestShouldTerminate(t *testing.T) {
	cancel := true
	var asked []bool
	// app.OnQuit's handlers, as package app sets them (which imports
	// this one).
	defer func(f func(bool) bool) { boot.QuitRequested = f }(boot.QuitRequested)
	boot.QuitRequested = func(ending bool) bool {
		asked = append(asked, ending)
		return cancel && !ending
	}
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	defer func() { quitState.allowed, quitState.ending = false, false }()
	onMain(func() {
		if r := h.shouldTerminate(); r != terminateCancel {
			fatalf(t, "a canceled quit answered %d", r)
		}
		Send(h.target, "workspaceWillPowerOff:", 0)
		if r := h.shouldTerminate(); r != terminateNow {
			fatalf(t, "the session's end answered %d", r)
		}
		cancel = false
		if r := h.shouldTerminate(); r != terminateNow {
			fatalf(t, "an allowed quit answered %d", r)
		}
	})
	// Allowed once, the quit only finishes: the handlers aren't asked
	// again.
	if fmt.Sprint(asked) != "[false true]" {
		t.Fatalf("asked %v", asked)
	}
}

// Recent documents: newest first, without those gone, in the File
// menu's Open Recent, where choosing one opens it; Clear Menu empties
// them.
func TestRecentDocuments(t *testing.T) {
	dir := t.TempDir()
	a, b, gone := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md"), filepath.Join(dir, "gone.md")
	for _, p := range []string{a, b, gone} {
		os.WriteFile(p, []byte("x"), 0o644)
	}
	dir, _ = filepath.EvalSymlinks(dir) // /private/var: what AppKit answers
	a, b, gone = filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md"), filepath.Join(dir, "gone.md")
	var files [][]string
	h, _ := mount(t, func() ui.Node {
		ui.OnOpenFiles(func(ps []string) { files = append(files, ps) })
		return ui.StaticText("x")
	})
	onMain(func() {
		callNow(t, h, "recent", "clear", nil)
		for _, p := range []string{gone, a, b, a} {
			if _, err, _ := callNow(t, h, "recent", "add", []byte(p)); err != nil {
				fatal(t, err)
			}
		}
		os.Remove(gone)
		out, err, _ := callNow(t, h, "recent", "list", nil)
		var paths []string
		json.Unmarshal(out, &paths)
		if err != nil || fmt.Sprint(paths) != fmt.Sprint([]string{a, b}) {
			fatalf(t, "list = %q, %v; want [a b]", paths, err)
		}
		// The File menu's Open Recent.
		file := Send(Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu"), "itemAtIndex:", 1)
		recentItem := Send(Send(file, "submenu"), "itemWithTitle:", uintptr(String(h.menuWord("Open Recent"))))
		if recentItem == 0 {
			fatal(t, "no Open Recent in the File menu")
		}
		m := Send(recentItem, "submenu")
		Send(Send(m, "delegate"), "menuNeedsUpdate:", uintptr(m))
		var titles []string
		for i := 0; i < SendInt(m, "numberOfItems"); i++ {
			titles = append(titles, GoString(Send(Send(m, "itemAtIndex:", uintptr(i)), "title")))
		}
		if strings.Join(titles, "|") != "a.md|b.md||"+h.menuWord("Clear Menu") {
			fatalf(t, "Open Recent: %q", titles)
		}
		Send(m, "performActionForItemAtIndex:", 1)
		if fmt.Sprint(files) != fmt.Sprint([][]string{{b}}) {
			fatalf(t, "choosing b.md opened %q", files)
		}
		Send(m, "performActionForItemAtIndex:", uintptr(SendInt(m, "numberOfItems")-1))
		out, _, _ = callNow(t, h, "recent", "list", nil)
		if string(out) != "[]" {
			fatalf(t, "after Clear Menu: %s", out)
		}
	})
}

// A run without a bundle (go test) can't be a login item; whether it
// opened at login is known once it has launched.
func TestLoginItem(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if _, err, _ := callNow(t, h, "login", "set", []byte{1}); err == nil || !strings.Contains(err.Error(), "bundle") {
			fatalf(t, "set without a bundle: %v", err)
		}
		if out, err, _ := callNow(t, h, "login", "enabled", nil); err != nil || string(out) != "\x00" {
			fatalf(t, "enabled = %q, %v", out, err)
		}
		launched := loginState.launched
		loginState.launched = false
		defer func() { loginState.launched = launched }()
		out, _, answered := callNow(t, h, "login", "openedAtLogin", nil)
		if answered {
			fatal(t, "answered before the app launched")
		}
		var got []byte
		loginState.waiting[0] = func(b []byte, err error) { got = b }
		Send(h.target, "appLaunched:", 0)
		if string(got) != "\x00" || out != nil {
			fatalf(t, "openedAtLogin = %q", got)
		}
	})
}

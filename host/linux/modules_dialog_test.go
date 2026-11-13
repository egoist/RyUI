//go:build linux && !android

package linux

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/dialog's dialogs are GTK's file chooser (without portals),
// modal to the window and set up as asked; the test answers each as the
// user would (a file chosen, a name typed, Cancel) and reads the answer.
func TestFileDialogs(t *testing.T) {
	if !strings.Contains(os.Getenv("GDK_DEBUG"), "no-portals") {
		t.Skip("run with GDK_DEBUG=no-portals: GTK's own dialog, which the test can answer")
	}
	h, _ := mount(t, emptyRoot)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.Mkdir(filepath.Join(dir, "Sub"), 0o755)

	const (
		cancel = 0xFFFFFFFA // GTK_RESPONSE_CANCEL
		accept = 0xFFFFFFFD // GTK_RESPONSE_ACCEPT
	)
	// show calls the method, waits for the chooser, readies it (off the
	// main thread: it may wait for the chooser), answers it with
	// response, and waits for the method's answer.
	show := func(kind string, a map[string]any, ready func(d Ptr), response uintptr) callResult {
		var r callResult
		args, _ := json.Marshal(a)
		onMain(func() {
			h.module("dialog")[kind](args, func(b []byte, err error) { r = callResult{b, err, true} })
		})
		var d Ptr
		settle(t, h, func() bool { d = fileChooser(); return d != 0 && WidgetGetMapped(d) })
		ready(d)
		onMain(func() { Call(Sym("gtk_dialog_response"), d, response) })
		for i := 0; i < 5000 && !waitDone(&r); i++ {
			time.Sleep(time.Millisecond)
		}
		if !r.done {
			t.Fatalf("dialog.%s never answered", kind)
		}
		settle(t, h, func() bool { return fileChooser() == 0 })
		return r
	}
	folderOf := func(d Ptr) string {
		f := Call(Sym("gtk_file_chooser_get_current_folder"), d)
		if f == 0 {
			return ""
		}
		defer ObjectUnref(f)
		return GoStringFree(Sys.FileGetPath(f))
	}
	// choose selects path once the chooser has loaded its folder.
	choose := func(d Ptr, path string) {
		settle(t, h, func() bool {
			f := Sys.FileNewForPath(path)
			Call(Sym("gtk_file_chooser_set_file"), d, f, 0)
			ObjectUnref(f)
			got := Call(Sym("gtk_file_chooser_get_file"), d)
			if got == 0 {
				return false
			}
			ObjectUnref(got)
			return true
		})
	}

	// Open, cancelled: nothing; the chooser the window's, set up as asked.
	r := show("open", map[string]any{"title": "Import", "button": "Import", "start": dir, "multiple": true,
		"filters": []map[string]any{{"name": "Text", "types": []string{"text/plain"}, "extensions": []string{"md"}}}},
		func(d Ptr) {
			onMain(func() {
				if Call(Sym("gtk_window_get_transient_for"), d) != h.main.window {
					fatalf(t, "the chooser isn't the window's")
				}
				if title := GoString(Call(Sym("gtk_window_get_title"), d)); title != "Import" {
					fatalf(t, "the title: %q", title)
				}
				if Call(Sym("gtk_file_chooser_get_select_multiple"), d)&1 == 0 || Call(Sym("gtk_file_chooser_get_action"), d) != 0 {
					fatalf(t, "the chooser doesn't open several files")
				}
				if f := Call(Sym("gtk_file_chooser_get_filter"), d); f == 0 || GoString(Call(Sym("gtk_file_filter_get_name"), f)) != "Text" {
					fatalf(t, "the chooser's filter")
				}
			})
			settle(t, h, func() bool { return folderOf(d) == dir })
		}, cancel)
	if r.err != nil || len(r.b) != 0 {
		t.Fatalf("open, cancelled: %q, %v", r.b, r.err)
	}

	// Open, a file chosen: its own path (not a copy), in JSON.
	r = show("open", map[string]any{"start": dir}, func(d Ptr) { choose(d, filepath.Join(dir, "a.txt")) }, accept)
	var paths []string
	if r.err != nil || json.Unmarshal(r.b, &paths) != nil || len(paths) != 1 || paths[0] != filepath.Join(dir, "a.txt") {
		t.Fatalf("open: %q, %v", r.b, r.err)
	}

	// Save: the name suggested, in the folder; the path chosen.
	r = show("save", map[string]any{"start": dir, "name": "Notes.md"}, func(d Ptr) {
		onMain(func() {
			if Call(Sym("gtk_file_chooser_get_action"), d) != 1 { // GTK_FILE_CHOOSER_ACTION_SAVE
				fatalf(t, "not a save dialog")
			}
			if name := GoStringFree(Call(Sym("gtk_file_chooser_get_current_name"), d)); name != "Notes.md" {
				fatalf(t, "the name: %q", name)
			}
		})
		settle(t, h, func() bool { return folderOf(d) == dir })
	}, accept)
	if r.err != nil || string(r.b) != filepath.Join(dir, "Notes.md") {
		t.Fatalf("save: %q, %v", r.b, r.err)
	}

	// Folder: the folder it's in, chosen as it is (nothing selected in it).
	sub := filepath.Join(dir, "Sub")
	r = show("folder", map[string]any{"start": sub}, func(d Ptr) {
		onMain(func() {
			if Call(Sym("gtk_file_chooser_get_action"), d) != 2 { // GTK_FILE_CHOOSER_ACTION_SELECT_FOLDER
				fatalf(t, "not a folder dialog")
			}
		})
		settle(t, h, func() bool { return folderOf(d) == sub })
	}, accept)
	paths = nil
	if r.err != nil || json.Unmarshal(r.b, &paths) != nil || len(paths) != 1 || paths[0] != sub {
		t.Fatalf("folder: %q, %v", r.b, r.err)
	}
}

// asuka/shell: Reveal asks the file manager (a test's, on the session
// bus) to show the item; a missing file fails; Trash moves a file to
// the trash, and fails (leaving it) where its volume has none.
func TestShell(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	dir := t.TempDir()
	missing := []byte(filepath.Join(dir, "missing.txt"))
	for _, m := range []string{"reveal", "open", "trash"} {
		if _, err := callModule(t, h, "shell", m, missing); err == nil || !strings.Contains(err.Error(), "missing.txt") {
			t.Fatalf("%s of a missing file: %v", m, err)
		}
	}
	if _, err := callModule(t, h, "shell", "beep", nil); err != nil {
		t.Fatal(err)
	}

	// Reveal, through the test's FileManager1.
	if exec.Command("python3", "-c", "from gi.repository import Gio").Run() != nil {
		t.Log("no python3-gi for the test's file manager: Reveal not tested")
	} else {
		server := exec.Command("python3", "testdata/filemanager.py")
		in, _ := server.StdinPipe()
		out, _ := server.StdoutPipe()
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { in.Close(); server.Wait() }()
		lines := bufio.NewScanner(out)
		if !lines.Scan() || lines.Text() != "ready" {
			t.Fatalf("the file manager: %q", lines.Text())
		}
		path := filepath.Join(dir, "a b.txt")
		os.WriteFile(path, nil, 0o644)
		if _, err := callModule(t, h, "shell", "reveal", []byte(path)); err != nil {
			t.Fatal(err)
		}
		if !lines.Scan() || lines.Text() != "show file://"+strings.ReplaceAll(path, " ", "%20") {
			t.Fatalf("the file manager was asked: %q", lines.Text())
		}
	}

	// Trash, in a folder of the user's (a tmpfs /tmp may have no trash).
	home, _ := os.UserHomeDir()
	inHome, err := os.MkdirTemp(home, "asuka-trash-test")
	if err != nil {
		t.Skip("no home folder:", err)
	}
	defer os.RemoveAll(inHome)
	name := filepath.Base(inHome) + ".txt"
	path := filepath.Join(inHome, name)
	os.WriteFile(path, []byte("x"), 0o644)
	_, err = callModule(t, h, "shell", "trash", []byte(path))
	_, statErr := os.Lstat(path)
	switch {
	case err != nil && statErr != nil:
		t.Fatalf("trash failed (%v), yet the file's gone: %v", err, statErr)
	case err != nil:
		t.Logf("no trash here: %v", err)
	case !errors.Is(statErr, os.ErrNotExist):
		t.Fatalf("still there after Trash: %v", statErr)
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		trashed := filepath.Join(data, "Trash", "files", name)
		if _, err := os.Lstat(trashed); err != nil {
			t.Fatalf("not in the trash: %v", err)
		}
		os.Remove(trashed)
		os.Remove(filepath.Join(data, "Trash", "info", name+".trashinfo"))
	}
}

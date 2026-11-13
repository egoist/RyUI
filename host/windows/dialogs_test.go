//go:build windows

package windows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ui"
)

// asuka/dialog's dialogs are the common item dialogs, modal to the
// window; the test answers each as the user would (a name typed, OK or
// Cancel pressed) once it shows.
func TestFileDialogs(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.Mkdir(filepath.Join(dir, "Sub"), 0o755)
	t.Cleanup(func() { dialogCreated = nil })

	// answer acts on the next dialog once it's on screen, and notes its
	// owner.
	var owner uintptr
	answer := func(act func(dlg, hwnd uintptr)) {
		dialogCreated = func(dlg uintptr) {
			var stop func()
			stop = ownTimer(50, func() {
				hwnd := FileDialogWindow(dlg)
				if hwnd == 0 || !IsWindowVisible(hwnd) {
					return
				}
				stop()
				owner = NextWindow(hwnd, 4) // GW_OWNER
				act(dlg, hwnd)
			})
		}
	}
	call := func(kind string, a map[string]any) ([]byte, error) {
		args, _ := json.Marshal(a)
		b, err, answered := await(h, "dialog", kind, args)
		if !answered {
			fatalf("dialog.%s never answered", kind)
		}
		return b, err
	}
	press := func(id uintptr) func(dlg, hwnd uintptr) {
		return func(_, hwnd uintptr) { PostMessage(hwnd, WM_COMMAND, id, 0) }
	}
	onMain(t, func() {
		// Open, cancelled: nothing, modal to the window.
		answer(press(IDCANCEL))
		b, err := call("open", map[string]any{"title": "Import", "start": dir, "multiple": true,
			"filters": []map[string]any{{"name": "Text", "types": []string{"text/plain"}, "extensions": []string{"md"}}}})
		if err != nil || len(b) != 0 {
			fatalf("open, cancelled: %q, %v", b, err)
		}
		if owner != h.window {
			fatalf("the open dialog's owner is %x, not the window (%x)", owner, h.window)
		}

		// Open, a file named: its path, in JSON.
		answer(func(dlg, hwnd uintptr) {
			SetFileDialogName(dlg, filepath.Join(dir, "a.txt"))
			PostMessage(hwnd, WM_COMMAND, 1, 0) // IDOK
		})
		b, err = call("open", map[string]any{"start": dir})
		var paths []string
		if err != nil || json.Unmarshal(b, &paths) != nil || len(paths) != 1 || !strings.EqualFold(paths[0], filepath.Join(dir, "a.txt")) {
			fatalf("open: %q, %v", b, err)
		}

		// Open, starting at a file: its name in the field, so OK opens it.
		answer(press(1))
		b, err = call("open", map[string]any{"start": filepath.Join(dir, "a.txt")})
		paths = nil
		if err != nil || json.Unmarshal(b, &paths) != nil || len(paths) != 1 || !strings.EqualFold(paths[0], filepath.Join(dir, "a.txt")) {
			fatalf("open, from a file: %q, %v", b, err)
		}

		// Save, a name typed without an extension: the filter's added.
		answer(func(dlg, hwnd uintptr) {
			SetFileDialogName(dlg, "Notes")
			PostMessage(hwnd, WM_COMMAND, 1, 0)
		})
		b, err = call("save", map[string]any{"start": dir, "name": "Untitled.md",
			"filters": []map[string]any{{"name": "Markdown", "extensions": []string{"md"}}}})
		if err != nil || !strings.EqualFold(string(b), filepath.Join(dir, "Notes.md")) {
			fatalf("save: %q, %v", b, err)
		}

		// Folder: the folder named.
		answer(func(dlg, hwnd uintptr) {
			SetFileDialogName(dlg, filepath.Join(dir, "Sub"))
			PostMessage(hwnd, WM_COMMAND, 1, 0)
		})
		b, err = call("folder", map[string]any{"start": dir})
		paths = nil
		if err != nil || json.Unmarshal(b, &paths) != nil || len(paths) != 1 || !strings.EqualFold(paths[0], filepath.Join(dir, "Sub")) {
			fatalf("folder: %q, %v", b, err)
		}
	})
}

// The filters: each filter's extensions and its MIME types', the first
// extension the default.
func TestDialogFilters(t *testing.T) {
	fs, def := dialogFilters([]dialogFilter{
		{Name: "Images", Types: []string{"image/*"}},
		{Name: "Docs", Types: []string{"application/pdf"}, Extensions: []string{"md"}},
		{Name: "All files", Extensions: []string{"*"}},
		{Name: "Empty"},
	})
	if len(fs) != 3 || fs[0].Name != "Images" || !strings.HasPrefix(fs[0].Patterns, "*.png;*.jpg") || def != "png" {
		t.Fatalf("filters %+v, default %q", fs, def)
	}
	if fs[1].Patterns != "*.md;*.pdf" || fs[2].Patterns != "*.*" {
		t.Fatalf("filters %+v", fs)
	}
	if _, def := dialogFilters([]dialogFilter{{Name: "All", Types: []string{"*/*"}}}); def != "" {
		t.Fatalf("any file's default extension: %q", def)
	}
}

// asuka/shell: a missing file fails; Trash moves a file to the Recycle
// Bin (or says it can't, never deleting it).
func TestShell(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	onMain(t, func() {
		missing := []byte(filepath.Join(dir, "missing.txt"))
		for _, m := range []string{"reveal", "open", "trash"} {
			if _, err, _ := await(h, "shell", m, missing); err == nil || !strings.Contains(err.Error(), "missing.txt") {
				fatalf("%s of a missing file: %v", m, err)
			}
		}
		if _, err, _ := await(h, "shell", "beep", nil); err != nil {
			fatalf("beep: %v", err)
		}
		path := filepath.Join(dir, "trash me.txt")
		os.WriteFile(path, []byte("x"), 0o644)
		_, err, answered := await(h, "shell", "trash", []byte(path))
		_, statErr := os.Lstat(path)
		switch {
		case !answered:
			fatalf("trash never answered")
		case err == ErrNotRecyclable:
			if statErr != nil {
				fatalf("not recyclable, yet gone: %v", statErr)
			}
		case err != nil:
			fatalf("trash: %v", err)
		case !os.IsNotExist(statErr):
			fatalf("still there after Trash: %v", statErr)
		}
	})
}

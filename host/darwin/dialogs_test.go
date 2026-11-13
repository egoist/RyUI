//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// asuka/dialog's panels are sheets on the window, set up as asked; the
// test ends each as the user would (endSheet:returnCode:, OK or Cancel)
// and reads the answer.
func TestFileDialogs(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() { Send(h.keyWin().window, "orderFront:", 0) }) // a sheet needs its window on screen
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.Mkdir(filepath.Join(dir, "Sub"), 0o755)

	// show calls the dialog's method, waits for its sheet, checks it,
	// ends it with code, and waits for the answer.
	show := func(kind string, a map[string]any, check func(p ID), code int) ([]byte, error) {
		var out []byte
		var err error
		onMain(func() {
			args, _ := json.Marshal(a)
			answered := false
			h.module("dialog")[kind](args, func(b []byte, e error) { out, err, answered = b, e, true })
			w := h.keyWin().window
			if !spin(func() bool { return answered || Send(w, "attachedSheet") != 0 }) || answered {
				fatalf(t, "dialog.%s: no sheet on the window (answered %q, %v)", kind, out, err)
			}
			p := Send(w, "attachedSheet")
			if !panelsOpen[p] {
				fatalf(t, "dialog.%s: the sheet isn't its panel", kind)
			}
			check(p)
			Send(w, "endSheet:returnCode:", uintptr(p), uintptr(code))
			if !spin(func() bool { return answered }) {
				fatalf(t, "dialog.%s: no answer", kind)
			}
			spin(func() bool { return Send(w, "attachedSheet") == 0 })
		})
		return out, err
	}
	str := func(p ID, sel string) string { return GoString(Send(p, sel)) }
	dirOf := func(p ID) string {
		d, _ := filepath.EvalSymlinks(GoString(Send(Send(p, "directoryURL"), "path")))
		return d
	}

	// Open, cancelled: nothing.
	b, err := show("open", map[string]any{"title": "Import", "button": "Import", "start": dir, "multiple": true, "hidden": true,
		"filters": []map[string]any{{"name": "Text", "types": []string{"text/plain"}, "extensions": []string{"md"}}}},
		func(p ID) {
			if !HasClass("NSOpenPanel") || !SendBool(p, "isKindOfClass:", uintptr(Class("NSOpenPanel"))) ||
				!SendBool(p, "canChooseFiles") || SendBool(p, "canChooseDirectories") || !SendBool(p, "allowsMultipleSelection") ||
				!SendBool(p, "showsHiddenFiles") || str(p, "prompt") != "Import" || str(p, "message") != "Import" || dirOf(p) != dir {
				fatalf(t, "the open panel: prompt %q, message %q, in %q", str(p, "prompt"), str(p, "message"), dirOf(p))
			}
			types := ArrayItems(Send(p, "allowedContentTypes"))
			var ids []string
			for _, ty := range types {
				ids = append(ids, str(ty, "identifier"))
			}
			if len(ids) != 2 || ids[0] != "public.plain-text" {
				fatalf(t, "the open panel's types: %v", ids)
			}
		}, 0)
	if err != nil || len(b) != 0 {
		t.Fatalf("open, cancelled: %q, %v", b, err)
	}

	// Open, starting at a file: in its folder, the file chosen.
	b, err = show("open", map[string]any{"start": filepath.Join(dir, "a.txt")}, func(p ID) {
		// The panel selects it once it has loaded the folder.
		if !spin(func() bool { return len(ArrayItems(Send(p, "URLs"))) == 1 }) || dirOf(p) != dir {
			fatalf(t, "the open panel, from a file: nothing selected, in %q", dirOf(p))
		}
	}, 1)
	var opened []string
	if err != nil || json.Unmarshal(b, &opened) != nil || len(opened) != 1 {
		t.Fatalf("open, from a file: %q, %v", b, err)
	}
	if got, _ := filepath.EvalSymlinks(opened[0]); got != filepath.Join(dir, "a.txt") {
		t.Fatalf("open, from a file: %q, not a.txt", opened)
	}

	// Save, chosen: the folder and the name as the panel has them.
	b, err = show("save", map[string]any{"start": filepath.Join(dir, "Old.txt"), "name": "Notes.md",
		"filters": []map[string]any{{"name": "Markdown", "extensions": []string{"md"}}}},
		func(p ID) {
			if str(p, "nameFieldStringValue") != "Notes.md" || dirOf(p) != dir || !SendBool(p, "canCreateDirectories") {
				fatalf(t, "the save panel: %q in %q", str(p, "nameFieldStringValue"), dirOf(p))
			}
		}, 1)
	if got, _ := filepath.EvalSymlinks(filepath.Dir(string(b))); err != nil || got != dir || filepath.Base(string(b)) != "Notes.md" {
		t.Fatalf("save: %q, %v", b, err)
	}

	// Folder, chosen: the folder selected, in JSON.
	b, err = show("folder", map[string]any{"start": dir}, func(p ID) {
		if SendBool(p, "canChooseFiles") || !SendBool(p, "canChooseDirectories") || SendBool(p, "allowsMultipleSelection") {
			fatalf(t, "the folder panel")
		}
		Send(p, "setDirectoryURL:", uintptr(FileURL(filepath.Join(dir, "Sub"))))
	}, 1)
	var paths []string
	if err != nil || json.Unmarshal(b, &paths) != nil || len(paths) != 1 {
		t.Fatalf("folder: %q, %v", b, err)
	}
	if got, _ := filepath.EvalSymlinks(paths[0]); !strings.HasPrefix(got, dir) {
		t.Fatalf("folder: %q, not in %s", paths, dir)
	}
}

// With no window on screen, the panel is one of its own.
func TestFileDialogNoWindow(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		Send(h.keyWin().window, "orderOut:", 0)
		answered := false
		var out []byte
		h.module("dialog")["folder"]([]byte(`{}`), func(b []byte, err error) { out, answered = b, true })
		var p ID
		if !spin(func() bool {
			for q := range panelsOpen {
				p = q
			}
			return p != 0 && SendBool(p, "isVisible")
		}) {
			fatal(t, "no panel")
		}
		if Send(p, "sheetParent") != 0 {
			fatal(t, "a sheet on a window that isn't on screen")
		}
		Send(p, "cancel:", 0)
		if !spin(func() bool { return answered }) || len(out) != 0 {
			fatalf(t, "cancelled: %q (answered %v)", out, answered)
		}
	})
}

// asuka/shell: a missing file fails, a type no app opens is "no app",
// and Trash moves the file to the Trash.
func TestShell(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	onMain(func() {
		missing := []byte(filepath.Join(dir, "missing.txt"))
		for _, m := range []string{"reveal", "open", "trash"} {
			if _, err := await(h, "shell", m, missing); err == nil || !strings.Contains(err.Error(), "missing.txt") {
				fatalf(t, "%s of a missing file: %v", m, err)
			}
		}
		odd := filepath.Join(dir, "x.asuka-no-app-opens-this")
		os.WriteFile(odd, nil, 0o644)
		if _, err := await(h, "shell", "open", []byte(odd)); err == nil || err.Error() != "no app" {
			fatalf(t, "open of a type no app opens: %v", err)
		}
		if _, err := await(h, "shell", "beep", nil); err != nil {
			fatal(t, err)
		}
	})

	name := "asuka-trash-test-" + randomName() + ".txt"
	path := filepath.Join(dir, name)
	os.WriteFile(path, []byte("x"), 0o644)
	var err error
	onMain(func() { _, err = await(h, "shell", "trash", []byte(path)) })
	if err != nil {
		t.Fatal(err)
	}
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		t.Fatalf("still there after Trash: %v", e)
	}
	home, _ := os.UserHomeDir()
	trashed := filepath.Join(home, ".Trash", name)
	if _, e := os.Lstat(trashed); e != nil {
		t.Logf("not in the Trash (another volume's, or no access): %v", e)
	}
	os.Remove(trashed)
}

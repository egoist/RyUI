package update

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fileTree returns the files and links of dir (content, or "->target"),
// leaving out the FileLists, which listOf reads.
func fileTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == FileList {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, _ := os.Readlink(p)
			tree[relPath(dir, p)] = "->" + link
			return nil
		}
		b, err := os.ReadFile(p)
		tree[relPath(dir, p)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func listOf(t *testing.T, file string) []string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	return ParseFileList(b)
}

// The Windows layout: a folder of the .exe and its assets, whose entries
// are replaced, as a Mac can test it (renames, a FileList).
func TestSwapFolder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		installed map[string]string
		list      []string // nil: an app installed before versions had one
		update    map[string]string
		want      map[string]string
		wantList  []string
	}{{
		name:      "removes what the update lacks",
		installed: map[string]string{"App.exe": "v1", "old.txt": "old", "assets/icon.png": "icon", "keep.txt": "same"},
		list:      []string{"App.exe", "keep.txt", "old.txt", "assets", "gone.txt"},
		update:    map[string]string{"App.exe": "v2", "keep.txt": "same", "new.txt": "new", "assets/icon.png": "icon 2"},
		want:      map[string]string{"App.exe": "v2", "keep.txt": "same", "new.txt": "new", "assets/icon.png": "icon 2"},
		wantList:  []string{"App.exe", "assets", "keep.txt", "new.txt"},
	}, {
		// The user's files, and an installer's uninstaller, are in no list.
		name:      "leaves what no list names",
		installed: map[string]string{"App.exe": "v1", "old.txt": "old", "data/notes": "mine", "Uninstall.exe": "u"},
		list:      []string{"App.exe", "old.txt"},
		update:    map[string]string{"App.exe": "v2"},
		want:      map[string]string{"App.exe": "v2", "data/notes": "mine", "Uninstall.exe": "u"},
		wantList:  []string{"App.exe"},
	}, {
		name:      "removes nothing without a list",
		installed: map[string]string{"App.exe": "v1", "old.txt": "old"},
		update:    map[string]string{"App.exe": "v2", "new.txt": "new"},
		want:      map[string]string{"App.exe": "v2", "old.txt": "old", "new.txt": "new"},
		wantList:  []string{"App.exe", "new.txt"},
	}, {
		// The same entry on Windows and macOS, two on Linux.
		name:      "an entry renamed in case",
		installed: map[string]string{"App.exe": "v1", "Readme.txt": "old"},
		list:      []string{"App.exe", "Readme.txt"},
		update:    map[string]string{"App.exe": "v2", "readme.txt": "new"},
		want:      map[string]string{"App.exe": "v2", "readme.txt": "new"},
		wantList:  []string{"App.exe", "readme.txt"},
	}, {
		// The running executable stays, whatever the new version calls its
		// own: shortcuts and Relaunch start it.
		name:      "keeps the running executable",
		installed: map[string]string{"App.exe": "v1"},
		list:      []string{"App.exe"},
		update:    map[string]string{"Renamed.exe": "v2"},
		want:      map[string]string{"App.exe": "v1", "Renamed.exe": "v2"},
		wantList:  []string{"Renamed.exe"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, tc.installed)
			if tc.list != nil {
				if err := WriteFileList(root, tc.list); err != nil {
					t.Fatal(err)
				}
			}
			in, err := Locate("windows", filepath.Join(root, "App.exe"), noEnv)
			if err != nil {
				t.Fatal(err)
			}
			if in.Kind != Folder || in.List != FileList {
				t.Fatalf("Locate = %+v", in)
			}
			work, err := in.MakeWork()
			if err != nil {
				t.Fatal(err)
			}
			tree := filepath.Join(work, "app")
			for name, content := range tc.update {
				p := filepath.Join(tree, filepath.FromSlash(name))
				os.MkdirAll(filepath.Dir(p), 0o755)
				os.WriteFile(p, []byte(content), 0o644)
			}
			if err := in.Swap(tree); err != nil {
				t.Fatal(err)
			}
			os.RemoveAll(work)
			if got := fileTree(t, root); !maps.Equal(got, tc.want) {
				t.Errorf("the folder holds %v, want %v", got, tc.want)
			}
			if got := listOf(t, filepath.Join(root, FileList)); !slices.Equal(got, tc.wantList) {
				t.Errorf("its list is %q, want %q", got, tc.wantList)
			}
			// Nothing put aside stays (Windows would keep a running .exe:
			// CleanLeftovers).
			if olds, _ := filepath.Glob(filepath.Join(root, ".*.old-*")); len(olds) > 0 {
				t.Errorf("left %v", olds)
			}
		})
	}
}

// The Linux layout: an install prefix that other programs share, whose
// app files alone are replaced.
func TestSwapPrefix(t *testing.T) {
	prefix := writeTree(t, map[string]string{
		"bin/notes":                                    "v1",
		"bin/other-program":                            "theirs",
		"share/notes/assets/a.png":                     "a1",
		"share/notes/assets/gone.png":                  "gone",
		"share/applications/com.example.notes.desktop": "entry 1",
		"share/applications/org.other.desktop":         "theirs",
		"share/icons/hicolor/64x64/apps/com.example.notes.png": "icon 1",
	})
	if err := WriteFileList(filepath.Join(prefix, "share/notes", FileList), []string{
		"bin/notes", "share/notes/assets/a.png", "share/notes/assets/gone.png",
		"share/applications/com.example.notes.desktop", "share/icons/hicolor/64x64/apps/com.example.notes.png",
	}); err != nil {
		t.Fatal(err)
	}
	in, err := Locate("linux", filepath.Join(prefix, "bin", "notes"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != Prefix || in.List != "share/notes/"+FileList || in.Work != filepath.Join(in.Root, "share", "notes") {
		t.Fatalf("Locate = %+v", in)
	}
	if err := in.Writable(); err != nil {
		t.Fatal(err)
	}
	work, err := in.MakeWork()
	if err != nil {
		t.Fatal(err)
	}
	// The tarball unpacks to notes/bin/notes...
	extracted := writeTreeIn(t, work, map[string]string{
		"x/notes/bin/notes":                                    "v2",
		"x/notes/share/notes/assets/a.png":                     "a2",
		"x/notes/share/notes/assets/b.png":                     "b2",
		"x/notes/share/notes/" + FileList:                      "bin/notes\n",
		"x/notes/share/applications/com.example.notes.desktop": "entry 2",
		"x/notes/share/mime/packages/com.example.notes.xml":    "mime 2",
	})
	tree, err := in.Tree(filepath.Join(extracted, "x"))
	if err != nil || filepath.Base(tree) != "notes" {
		t.Fatalf("Tree = %s, %v", tree, err)
	}
	if exe := in.ExeIn(tree); exe != filepath.Join(tree, "bin", "notes") {
		t.Errorf("ExeIn = %s", exe)
	}
	if err := in.Swap(tree); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"bin/notes":                                    "v2",
		"bin/other-program":                            "theirs",
		"share/notes/assets/a.png":                     "a2",
		"share/notes/assets/b.png":                     "b2",
		"share/applications/com.example.notes.desktop": "entry 2",
		"share/applications/org.other.desktop":         "theirs",
		"share/mime/packages/com.example.notes.xml":    "mime 2",
	}
	got := fileTree(t, prefix)
	for p := range got {
		if strings.HasPrefix(p, "share/notes/"+WorkPrefix) {
			delete(got, p)
		}
	}
	if !maps.Equal(got, want) {
		t.Errorf("the prefix holds %v, want %v", got, want)
	}
	wantList := []string{"bin/notes", "share/applications/com.example.notes.desktop", "share/mime/packages/com.example.notes.xml", "share/notes/assets/a.png", "share/notes/assets/b.png"}
	if got := listOf(t, filepath.Join(prefix, "share/notes", FileList)); !slices.Equal(got, wantList) {
		t.Errorf("its list is %q, want %q", got, wantList)
	}
	in.CleanLeftovers()
	if ws, _ := filepath.Glob(filepath.Join(in.Work, WorkPrefix+"*")); len(ws) > 0 {
		t.Errorf("CleanLeftovers left %v", ws)
	}
}

// A swap that fails part way puts the old version back, whole.
func TestSwapUndo(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a directory the user can't write")
	}
	installed := map[string]string{
		"bin/notes":                "v1",
		"share/notes/assets/a.png": "a1",
		"share/notes/z/locked":     "z1",
	}
	prefix := writeTree(t, installed)
	WriteFileList(filepath.Join(prefix, "share/notes", FileList), []string{"bin/notes", "share/notes/assets/a.png", "share/notes/z/locked"})
	in, err := Locate("linux", filepath.Join(prefix, "bin", "notes"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	work, _ := in.MakeWork()
	tree := writeTreeIn(t, work, map[string]string{"bin/notes": "v2", "share/notes/assets/a.png": "a2", "share/notes/z/locked": "z2"})
	// The last unit can't be replaced.
	locked := filepath.Join(prefix, "share/notes/z")
	os.Chmod(locked, 0o555)
	defer os.Chmod(locked, 0o755)
	if err := in.Swap(tree); err == nil {
		t.Fatal("the swap succeeded")
	}
	os.RemoveAll(work)
	if got := fileTree(t, prefix); !maps.Equal(got, installed) {
		t.Errorf("after a failed swap the prefix holds %v, want %v", got, installed)
	}
}

func TestLocate(t *testing.T) {
	// macOS: the bundle around Contents/MacOS/<exe>.
	apps := t.TempDir()
	bundle := filepath.Join(apps, "Notes.app")
	writeTreeIn(t, apps, map[string]string{"Notes.app/Contents/MacOS/Notes": "exe", "Notes.app/Contents/Info.plist": ""})
	in, err := Locate("darwin", filepath.Join(bundle, "Contents", "MacOS", "Notes"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if real, _ := filepath.EvalSymlinks(bundle); in.Kind != Bundle || in.Root != real || in.Work != filepath.Dir(real) {
		t.Errorf("darwin: %+v", in)
	}
	if err := in.Writable(); err != nil {
		t.Error(err)
	}
	if _, err := Locate("darwin", "/private/var/folders/xy/T/AppTranslocation/ABC/d/Notes.app/Contents/MacOS/Notes", noEnv); err != ErrTranslocated {
		t.Errorf("translocated: %v", err)
	}
	writeTreeIn(t, apps, map[string]string{"Store.app/Contents/MacOS/Store": "exe", "Store.app/Contents/_MASReceipt/receipt": ""})
	if _, err := Locate("darwin", filepath.Join(apps, "Store.app/Contents/MacOS/Store"), noEnv); err != ErrManaged {
		t.Errorf("Mac App Store: %v", err)
	}
	if _, err := Locate("darwin", filepath.Join(apps, "notes"), noEnv); err != ErrNotInstalled {
		t.Errorf("a bare binary: %v", err)
	}

	// Linux: a Flatpak, a Snap, a distribution's package, go run.
	prefix := writeTree(t, map[string]string{"bin/notes": "", "share/notes/assets/x": ""})
	exe := filepath.Join(prefix, "bin", "notes")
	for env, want := range map[string]error{"FLATPAK_ID": ErrManaged, "SNAP": ErrManaged, "APPIMAGE": ErrManaged} {
		getenv := func(k string) string {
			if k == env {
				return "x"
			}
			return ""
		}
		if _, err := Locate("linux", exe, getenv); err != want {
			t.Errorf("%s: %v", env, err)
		}
	}
	if _, err := Locate("linux", filepath.Join(prefix, "notes"), noEnv); err != ErrNotInstalled {
		t.Errorf("not in bin: %v", err)
	}
	bare := writeTree(t, map[string]string{"bin/tool": ""})
	if _, err := Locate("linux", filepath.Join(bare, "bin", "tool"), noEnv); err != ErrNotInstalled {
		t.Errorf("no share/<exe>: %v", err)
	}

	// A directory the user can't write.
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		ro := writeTree(t, map[string]string{"App.exe": ""})
		in, _ := Locate("windows", filepath.Join(ro, "App.exe"), noEnv)
		os.Chmod(ro, 0o555)
		defer os.Chmod(ro, 0o755)
		if err := in.Writable(); err != ErrNotWritable {
			t.Errorf("read only: %v", err)
		}
	}
}

func noEnv(string) string { return "" }

// writeTreeIn writes files into dir, returning it.
func writeTreeIn(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

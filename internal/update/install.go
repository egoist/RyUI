package update

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// How an installed app is laid out, which is what an update replaces.
// asuka build makes each:
//
//   - Bundle (macOS): Name.app, replaced whole once the app has quit
//     (StageBundle), so that the Dock keeps its tile and the code
//     signature is the new version's, sealed as it was built.
//   - Folder (Windows): a folder holding Name.exe and assets, with a
//     FileList; its entries are replaced while the app runs (Swap): the
//     running .exe can be renamed, not deleted, so it's put aside and
//     removed at the next launch (CleanLeftovers).
//   - Prefix (Linux): the install prefix of the build's tarball, such as
//     ~/.local, which other programs share: bin/<exe>, share/<exe>/ (its
//     assets and FileList), share/applications/<id>.desktop, its icons,
//     metainfo and MIME types. Only the app's files are replaced, one by
//     one, each by a rename (a running program keeps its old file).
type Kind uint8

const (
	Bundle Kind = iota + 1
	Folder
	Prefix
)

// Errors of Locate: why an app can't update itself.
var (
	// ErrManaged: a package manager, Flatpak, Snap, the Mac App Store or
	// the Microsoft Store installed the app, and updates it.
	ErrManaged = errors.New("a package manager or an app store installed the app, and updates it")
	// ErrTranslocated: macOS runs the app from a read-only copy (App
	// Translocation, for an app opened where it was downloaded, still
	// quarantined): the user moves it to Applications first.
	ErrTranslocated = errors.New("macOS runs the app from a read-only copy: move it to the Applications folder, then open it again")
	// ErrNotWritable: the user can't write where the app is installed (an
	// administrator's install, a disk image).
	ErrNotWritable = errors.New("the app can't write where it's installed")
	// ErrNotInstalled: the executable isn't in a layout asuka build makes
	// (go run, a bare binary).
	ErrNotInstalled = errors.New("the app isn't laid out as asuka build makes it (a bundle, a folder, an install prefix)")
)

// Install is where and how an app is installed.
type Install struct {
	Kind Kind
	// Root is what an update replaces: the bundle, the folder, the prefix.
	Root string
	// Exe is the running executable, its links resolved.
	Exe string
	// List is the FileList's path in Root, with slashes ("" for a bundle).
	List string
	// Work is where updates are made, beside or inside Root, on its
	// volume: renamed into place, they never cross one.
	Work string
}

// Locate finds how the app whose executable is exe is installed on goos,
// reading the environment with getenv. The result's directories aren't
// checked for writing: Writable does that.
func Locate(goos, exe string, getenv func(string) string) (*Install, error) {
	if exe == "" {
		return nil, ErrNotInstalled
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	exe = filepath.Clean(exe)
	switch goos {
	case "darwin":
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if !strings.HasSuffix(bundle, ".app") || filepath.Base(filepath.Dir(exe)) != "MacOS" {
			return nil, ErrNotInstalled
		}
		if strings.Contains(bundle, "/AppTranslocation/") {
			return nil, ErrTranslocated
		}
		if _, err := os.Stat(filepath.Join(bundle, "Contents", "_MASReceipt")); err == nil {
			return nil, ErrManaged
		}
		return &Install{Kind: Bundle, Root: bundle, Exe: exe, Work: filepath.Dir(bundle)}, nil
	case "windows":
		dir := filepath.Dir(exe)
		if strings.Contains(strings.ToLower(dir), `\windowsapps\`) {
			return nil, ErrManaged // MSIX: the Store, App Installer
		}
		return &Install{Kind: Folder, Root: dir, Exe: exe, List: FileList, Work: dir}, nil
	case "linux":
		if getenv("FLATPAK_ID") != "" || getenv("SNAP") != "" || getenv("APPIMAGE") != "" {
			return nil, ErrManaged
		}
		if _, err := os.Stat("/.flatpak-info"); err == nil {
			return nil, ErrManaged
		}
		bin := filepath.Dir(exe)
		name := filepath.Base(exe)
		if filepath.Base(bin) != "bin" {
			return nil, ErrNotInstalled
		}
		prefix := filepath.Dir(bin)
		share := filepath.Join(prefix, "share", name)
		if fi, err := os.Stat(share); err != nil || !fi.IsDir() {
			return nil, ErrNotInstalled
		}
		switch prefix {
		case "/usr", "/", "/app", "/snap":
			return nil, ErrManaged // the system's packages
		}
		return &Install{Kind: Prefix, Root: prefix, Exe: exe, List: "share/" + name + "/" + FileList, Work: share}, nil
	}
	return nil, ErrNotInstalled
}

// Writable reports whether the user can make an update in Work and put it
// in place: ErrNotWritable when not.
func (in *Install) Writable() error {
	dirs := []string{in.Work}
	if in.Kind == Prefix {
		dirs = append(dirs, filepath.Dir(in.Exe))
	}
	for _, d := range dirs {
		probe, err := os.MkdirTemp(d, ".asuka-probe-")
		if err != nil {
			return ErrNotWritable
		}
		os.Remove(probe)
	}
	if in.Kind == Bundle {
		// The bundle itself moves: its parent and the bundle's own entry.
		if fi, err := os.Stat(in.Root); err != nil || fi.Mode().Perm()&0o200 == 0 {
			return ErrNotWritable
		}
	}
	return nil
}

// Units returns what an update replaces in a tree made for in (an
// extracted archive, or a delta applied): a Folder's entries, a Prefix's
// files and links, paths with slashes, without FileLists.
func (in *Install) Units(tree string) ([]string, error) {
	var units []string
	switch in.Kind {
	case Folder:
		entries, err := os.ReadDir(tree)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Name() != FileList {
				units = append(units, e.Name())
			}
		}
	case Prefix:
		err := filepath.WalkDir(tree, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() == FileList {
				return err
			}
			units = append(units, relPath(tree, p))
			return nil
		})
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("a bundle is replaced whole")
	}
	if len(units) == 0 {
		return nil, errors.New("the update is empty")
	}
	return units, nil
}

// Swap puts the update in tree (Units' tree) in place of the version
// installed at in.Root. It replaces the units tree has, and removes those
// that the installed version's FileList names and tree lacks; what no list
// names stays (the user's files). Replaced and removed units are renamed
// aside first, and deleted once every unit of tree is in place; if one
// can't be, those placed go back and the others are put back, so the app
// is the old version, whole. The running executable is never removed,
// whatever the new version calls its own (Relaunch starts it); when it's
// replaced, the old one is put aside (Windows lets a running .exe be
// renamed, not deleted) and removed at the next launch (CleanLeftovers).
// The new FileList, of tree's units, goes in place with them.
func (in *Install) Swap(tree string) error {
	units, err := in.Units(tree)
	if err != nil {
		return err
	}
	ours := map[string]bool{}
	for _, u := range units {
		ours[u] = true
	}
	suffix := ".old-" + strconv.Itoa(os.Getpid())
	var moved, placed [][2]string // renamed: from, to
	undo := func() {
		for i := len(placed) - 1; i >= 0; i-- {
			if os.Rename(placed[i][1], placed[i][0]) != nil {
				_ = os.RemoveAll(placed[i][1])
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Rename(moved[i][1], moved[i][0])
		}
	}
	moveAside := func(dst string) error {
		aside := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+suffix)
		_ = os.RemoveAll(aside) // a leftover of the same pid
		if err := os.Rename(dst, aside); err != nil {
			return err
		}
		moved = append(moved, [2]string{dst, aside})
		return nil
	}
	running := in.runningUnit()

	// The units that the replaced version installed and tree lacks go
	// first, so that one tree has under another case, which is the same
	// file on Windows and macOS, is aside before tree's takes its place.
	// One that can't be moved stays listed, for the next update.
	var left []string
	for _, u := range in.installed() {
		dst := filepath.Join(in.Root, filepath.FromSlash(u))
		if ours[u] || u == running {
			continue
		}
		if _, err := os.Lstat(dst); err != nil {
			continue
		}
		if err := moveAside(dst); err != nil {
			log.Printf("asuka: update: leaving %s, which the update no longer has: %v", dst, err)
			left = append(left, u)
		}
	}
	list := filepath.Join(tree, filepath.FromSlash(in.List))
	if err := WriteFileList(list, append(slices.Clone(units), left...)); err != nil {
		undo()
		return err
	}

	for _, u := range append(units, in.List) {
		src, dst := filepath.Join(tree, filepath.FromSlash(u)), filepath.Join(in.Root, filepath.FromSlash(u))
		if _, err := os.Lstat(dst); err == nil {
			if err := moveAside(dst); err != nil {
				undo()
				return fmt.Errorf("replacing %s: %w", dst, err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			undo()
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			undo()
			return fmt.Errorf("replacing %s: %w", dst, err)
		}
		placed = append(placed, [2]string{src, dst})
	}
	for _, m := range moved {
		_ = os.RemoveAll(m[1]) // the running .exe on Windows stays: CleanLeftovers
	}
	return nil
}

// installed returns the units the installed version's FileList names:
// none without one (an app built before it had one).
func (in *Install) installed() []string {
	b, err := os.ReadFile(filepath.Join(in.Root, filepath.FromSlash(in.List)))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("asuka: update: removing none of the files the update no longer has: %v", err)
		}
		return nil
	}
	return ParseFileList(b)
}

// runningUnit returns the unit the app runs from, or "".
func (in *Install) runningUnit() string {
	root := in.Root
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	rel, err := filepath.Rel(root, in.Exe)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if in.Kind == Folder {
		rel, _, _ = strings.Cut(rel, "/")
	}
	return rel
}

// CleanLeftovers removes what an update put aside and couldn't remove
// while the version before ran (Windows keeps a running .exe), and the
// work directories of updates that were stopped (the app quit while one
// downloaded). Run at launch.
func (in *Install) CleanLeftovers() {
	if in.Kind == Bundle {
		olds, _ := filepath.Glob(filepath.Join(in.Work, "."+filepath.Base(in.Root)+".update-*"))
		for _, p := range olds {
			// A staged update waits in its work directory for the app to
			// quit (StageBundle): its helper, while it runs, removes its
			// own.
			if b, err := os.ReadFile(filepath.Join(p, stagedFile)); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && processAlive(pid) {
					continue
				}
			}
			_ = os.RemoveAll(p)
		}
		return
	}
	dirs := []string{filepath.Dir(in.Exe), in.Root}
	if in.Kind == Prefix {
		dirs = append(dirs, in.Work)
	}
	for _, d := range slices.Compact(dirs) {
		olds, _ := filepath.Glob(filepath.Join(d, ".*.old-*"))
		works, _ := filepath.Glob(filepath.Join(d, WorkPrefix+"*"))
		for _, p := range append(olds, works...) {
			_ = os.RemoveAll(p)
		}
	}
}

// stagedFile, in the work directory of a staged update, holds its
// helper's pid: CleanLeftovers leaves the update while the helper waits.
const stagedFile = "staged"

// WorkPrefix starts the names of the directories updates are made in,
// in Install.Work (MakeWork).
const WorkPrefix = ".asuka-update-"

// MakeWork makes a directory for an update in Work.
func (in *Install) MakeWork() (string, error) {
	pattern := WorkPrefix
	if in.Kind == Bundle {
		pattern = "." + filepath.Base(in.Root) + ".update-"
	}
	dir, err := os.MkdirTemp(in.Work, pattern)
	if err != nil {
		return "", fmt.Errorf("can't write where the app is installed: %w", err)
	}
	return dir, nil
}

// Tree returns the root of the app in dir, an extracted archive: the
// bundle (its one entry, Name.app); a Linux tarball's one directory (it
// unpacks to <exe>/bin/<exe>...); a Windows folder's entries as they are.
func (in *Install) Tree(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	switch in.Kind {
	case Bundle:
		if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".app") || !entries[0].IsDir() {
			return "", errors.New("the update holds no app bundle")
		}
		return filepath.Join(dir, entries[0].Name()), nil
	case Prefix:
		if len(entries) == 1 && entries[0].IsDir() {
			if _, err := os.Stat(filepath.Join(dir, entries[0].Name(), "bin")); err == nil {
				return filepath.Join(dir, entries[0].Name()), nil
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "bin")); err != nil {
			return "", errors.New("the update holds no bin directory")
		}
	}
	return dir, nil
}

// ExeIn returns the path of the running executable's counterpart in a tree
// (Tree's), or "" when the update has none of its name: a Folder's or a
// Prefix's must keep it, which Relaunch starts.
func (in *Install) ExeIn(tree string) string {
	u := in.runningUnit()
	if u == "" || in.Kind == Bundle {
		return ""
	}
	if in.Kind == Folder {
		u = path.Base(filepath.ToSlash(in.Exe))
	}
	p := filepath.Join(tree, filepath.FromSlash(u))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

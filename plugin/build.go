package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Building a plugin's libraries: its build tool writes each target's with
// StoreBuild, with the SourceHash it built from; asuka plugin publish
// refuses a build whose sources have changed since.

// BuildPath is where a plugin's build of a file goes: build/<target>/<name>
// in dir, the manifest's directory. It isn't checked in.
func BuildPath(dir, target, name string) string {
	return filepath.Join(dir, "build", target, name)
}

// SourceHash is the SHA-256 of what a plugin's libraries are built from:
// the files of m.Sources (a directory's files whole, in order), or, with
// none, every file in dir but the build, the manifest and tests.
func SourceHash(dir string, m *Manifest) (string, error) {
	var files []string
	add := func(root string) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == "build" || d.Name() == "testdata" || (strings.HasPrefix(d.Name(), ".") && p != root) {
					return filepath.SkipDir
				}
				return nil
			}
			if rel == ManifestFile || strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			files = append(files, rel)
			return nil
		})
	}
	if len(m.Sources) == 0 {
		if err := add(dir); err != nil {
			return "", err
		}
	}
	for _, s := range m.Sources {
		if err := add(filepath.Join(dir, filepath.FromSlash(s))); err != nil {
			return "", fmt.Errorf("plugin %s: source %s: %w", m.Name, s, err)
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s %d\n", f, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sourceSuffix names the file beside a build that says its SourceHash.
const sourceSuffix = ".source"

// StoreBuild writes a plugin's build of a library's file for target, and
// the SourceHash it was built from.
func StoreBuild(dir, target, name string, data []byte, source string) error {
	p := BuildPath(dir, target, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o755); err != nil {
		return err
	}
	return os.WriteFile(p+sourceSuffix, []byte(source+"\n"), 0o644)
}

// BuildSource is the SourceHash a build of a file was built from ("" if
// it isn't built, or says none).
func BuildSource(dir, target, name string) string {
	b, err := os.ReadFile(BuildPath(dir, target, name) + sourceSuffix)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

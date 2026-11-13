package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	m, err := Parse([]byte(`{"name": "x", "libraries": [{"name": "lx", "files": {"darwin": {"name": "liblx.dylib"}}}]}`))
	if err != nil || m.Name != "x" {
		t.Fatal(m, err)
	}
	if l, ok := m.Library("lx"); !ok || l.Files["darwin"].Name != "liblx.dylib" {
		t.Fatal(l, ok)
	}
	if _, ok := m.Library("nope"); ok {
		t.Error("a library that isn't there")
	}
	for _, bad := range []string{`{`, `{"libraries": []}`, `{"name": "x", "libraries": [{"files": {}}]}`,
		`{"name": "x", "libraries": [{"name": "l", "files": {"darwin": {}}}]}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if Target("darwin", "amd64") != "darwin" || Target("linux", "arm64") != "linux-arm64" || Target("js", "wasm") != "wasm" || Target("ios", "arm64") != "ios" {
		t.Error("Target")
	}
}

// The source hash is of the sources' contents and names: not the build,
// the manifest or tests, and not what the manifest's sources leave out.
func TestSourceHash(t *testing.T) {
	dir := t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	write("csrc/a.c", "int a;")
	write("tool/main.go", "package main")
	write("x.go", "package x")
	m := &Manifest{Name: "x", Sources: []string{"csrc", "tool"}}
	h, err := SourceHash(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	same := func(what string) {
		t.Helper()
		if h2, _ := SourceHash(dir, m); h2 != h {
			t.Errorf("%s changed the hash", what)
		}
	}
	write("x.go", "package x // changed")
	write("build/darwin/lib.dylib", "built")
	write(ManifestFile, "{}")
	write("csrc/a_test.go", "package a")
	same("what isn't a source")
	write("csrc/a.c", "int b;")
	if h2, _ := SourceHash(dir, m); h2 == h {
		t.Error("a source's change didn't change the hash")
	}
	all, err := SourceHash(dir, &Manifest{Name: "x"})
	if err != nil || all == h {
		t.Error("no sources: every file", err)
	}
}

func TestStoreBuild(t *testing.T) {
	dir := t.TempDir()
	if err := StoreBuild(dir, "linux-amd64", "liba.so", []byte("lib"), "abc"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(BuildPath(dir, "linux-amd64", "liba.so")); string(b) != "lib" {
		t.Fatal(string(b))
	}
	if BuildSource(dir, "linux-amd64", "liba.so") != "abc" || BuildSource(dir, "linux-arm64", "liba.so") != "" {
		t.Error("BuildSource")
	}
}

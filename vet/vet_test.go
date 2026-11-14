package vet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/asuka/vet"
	"golang.org/x/tools/go/analysis/analysistest"
)

// module copies testdata into a module of its own that uses this
// checkout of Asuka, so the tests run against its real API.
func module(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod := strings.Replace(string(gomod), "module github.com/egoist/asuka", "module example.com/vettest", 1)
	mod += "\nrequire github.com/egoist/asuka v0.0.0\n\nreplace github.com/egoist/asuka => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReads(t *testing.T) {
	analysistest.Run(t, module(t), vet.Reads, "./reads", "./msgs")
}

func TestMainThread(t *testing.T) {
	dir := module(t)
	// The memo's writes are the other analyzer's.
	analysistest.Run(t, dir, vet.MainThread, "./writes/goroutines")
}

func TestMemoWrites(t *testing.T) {
	analysistest.Run(t, module(t), vet.MemoWrites, "./writes/memos")
}

func TestRoutes(t *testing.T) {
	analysistest.Run(t, module(t), vet.Routes, "./routes/app", "./routes/open")
}

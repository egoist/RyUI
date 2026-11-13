package vet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egoist/asuka/vet"
	"golang.org/x/tools/go/analysis/unitchecker"
)

// TestMain is also go vet's -vettool for TestRepoIsClean: this binary,
// run with ASUKA_VETTOOL set.
func TestMain(m *testing.M) {
	if os.Getenv("ASUKA_VETTOOL") == "1" {
		unitchecker.Main(vet.Analyzers...)
	}
	os.Exit(m.Run())
}

// Asuka's own packages and examples follow its rules: anything the
// analyzers report there is a mistake of theirs or of the code.
func TestRepoIsClean(t *testing.T) {
	if testing.Short() {
		t.Skip("vets the whole repository")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	// This system's, Windows' (whose host is amd64's) and Linux's files.
	for _, env := range [][]string{nil, {"GOOS=windows", "GOARCH=amd64"}, {"GOOS=linux"}} {
		c := exec.Command("go", "vet", "-vettool="+exe, "./...")
		c.Dir = root
		c.Env = append(append(os.Environ(), "ASUKA_VETTOOL=1", "CGO_ENABLED=0"), env...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Errorf("%v go vet: %v\n%s", env, err, out)
		}
	}
}

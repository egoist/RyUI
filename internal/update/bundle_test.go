//go:build unix

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// waitFor waits, a while at most, for cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A staged bundle replaces the app's once the app has exited, not
// before, and the helper relaunches it when asked, then removes its work.
func TestStageBundle(t *testing.T) {
	apps := t.TempDir()
	target := filepath.Join(apps, "Notes.app")
	writeTreeIn(t, apps, map[string]string{"Notes.app/Contents/MacOS/Notes": "v1", "Notes.app/Contents/Resources/old": "old"})
	in, err := Locate("darwin", filepath.Join(target, "Contents/MacOS/Notes"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	work, err := in.MakeWork()
	if err != nil {
		t.Fatal(err)
	}
	writeTreeIn(t, work, map[string]string{"app/Notes.app/Contents/MacOS/Notes": "v2"})
	bundle, err := in.Tree(filepath.Join(work, "app"))
	if err != nil {
		t.Fatal(err)
	}

	// The app: a process that runs until it's told to quit.
	app := exec.Command("/bin/sh", "-c", "read x")
	stdin, _ := app.StdinPipe()
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	s, err := StageBundle(work, bundle, in.Root, app.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	relaunched := filepath.Join(t.TempDir(), "relaunched")
	if err := s.Relaunch([]string{"/bin/sh", "-c", `cat "$1/Contents/MacOS/Notes" > "$2"`, "sh", in.Root, relaunched}); err != nil {
		t.Fatal(err)
	}
	// While the app runs, a launch cleans no staged update up.
	in.CleanLeftovers()
	if _, err := os.Stat(work); err != nil {
		t.Fatal("CleanLeftovers removed the staged update")
	}
	time.Sleep(200 * time.Millisecond)
	if b, _ := os.ReadFile(filepath.Join(target, "Contents/MacOS/Notes")); string(b) != "v1" {
		t.Fatalf("the bundle was replaced while the app ran: %q", b)
	}

	stdin.Close() // the app quits
	app.Wait()
	waitFor(t, "the relaunch", func() bool {
		b, _ := os.ReadFile(relaunched)
		return string(b) == "v2"
	})
	if got := fileTree(t, target); len(got) != 1 || got["Contents/MacOS/Notes"] != "v2" {
		t.Errorf("the bundle holds %v", got)
	}
	waitFor(t, "the work directory to go", func() bool {
		_, err := os.Stat(work)
		return os.IsNotExist(err)
	})
}

// A stale staged update, whose helper is gone, is cleaned up at launch.
func TestCleanStaleStagedBundle(t *testing.T) {
	apps := t.TempDir()
	writeTreeIn(t, apps, map[string]string{"Notes.app/Contents/MacOS/Notes": "v1"})
	in, _ := Locate("darwin", filepath.Join(apps, "Notes.app/Contents/MacOS/Notes"), noEnv)
	work, _ := in.MakeWork()
	os.WriteFile(filepath.Join(work, stagedFile), []byte("999999"), 0o644)
	in.CleanLeftovers()
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Error("a stale staged update stayed")
	}
}

func TestSetArgs(t *testing.T) {
	out, err := exec.Command("/bin/sh", "-c", SetArgs([]string{"a b", "it's", `"q"`, "$HOME"})+`printf '%s|' "$@"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := `a b|it's|"q"|$HOME|`; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if got := OpenArgs("/Applications/Notes.app", []string{"-x"}); len(got) != 4 || got[2] != "--args" {
		t.Errorf("OpenArgs = %q", got)
	}
}

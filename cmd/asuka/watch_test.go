package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The dev loops wake as the system reports a change, and changed decides.
func TestWatcherWakesOnAChange(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	t0 := time.Now().Add(-time.Hour)
	os.WriteFile(src, []byte("package main\n"), 0o644)
	os.Chtimes(src, t0, t0)
	w := newWatcher(dir)
	if w.changed() {
		t.Fatal("changed with nothing changed")
	}
	os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644)
	select {
	case <-w.wake:
	case <-time.After(10 * time.Second): // a hang guard
		t.Fatal("no wake for a change")
	}
	if !w.changed() || w.changed() {
		t.Fatal("a change: noticed once")
	}
}

func TestOutermostRoots(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	got := outermost([]string{filepath.Join(a, "app"), a, filepath.Join(dir, "ab"), a})
	if want := []string{a, filepath.Join(dir, "ab")}; !slices.Equal(got, want) {
		t.Fatal(got, want)
	}
}

// buildDevRun builds testdata/devrun: a run that waits for asuka dev
// (devstate.Wait) and logs what happens to it.
func buildDevRun(t *testing.T) (bin, logPath string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "devrun")
	out, err := exec.Command("go", "build", "-o", bin, "./testdata/devrun").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return bin, filepath.Join(dir, "log")
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return strings.Fields(strings.ReplaceAll(string(b), " ", ":"))
}

// until waits for cond: a hang guard, not a timing assertion.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("waiting for", what)
		}
	}
}

// A reload starts the new run while the old one runs; the old one stops
// once the new one waits, and the new one goes on once the old one has
// stopped (and saved its state).
func TestRestartStartsTheNextRunBehindTheLast(t *testing.T) {
	bin, logPath := buildDevRun(t)
	p := &hostProcess{app: &goApp{dir: t.TempDir()}, bin: bin}
	run := func(name string, more ...string) error {
		p.env = append([]string{"DEVRUN_LOG=" + logPath, "DEVRUN_NAME=" + name}, more...)
		return p.restart()
	}
	if err := run("one"); err != nil {
		t.Fatal(err)
	}
	until(t, "the first run to go on", func() bool { return slices.Contains(readLog(t, logPath), "go:one") })
	if err := run("two"); err != nil {
		t.Fatal(err)
	}
	until(t, "the second run to go on", func() bool { return slices.Contains(readLog(t, logPath), "go:two") })
	want := []string{"start:one", "go:one", "start:two", "stop:one", "go:two"}
	if got := readLog(t, logPath); !slices.Equal(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	// A build that quits as it starts: the last one keeps running.
	if err := run("three", "DEVRUN_QUIT=1"); err == nil {
		t.Fatal("no error for a run that quit")
	}
	select {
	case <-p.done():
		t.Fatal("the running one stopped for a build that quit")
	default:
	}
	p.stop()
	want = append(want, "start:three", "stop:two")
	if got := readLog(t, logPath); !slices.Equal(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
}

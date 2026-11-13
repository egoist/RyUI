package app

import (
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/egoist/asuka/internal/boot"
)

// Relaunch quits as Quit does, and relaunches only a quit that goes on:
// not one a handler canceled, nor the session ending; a staged update's
// helper relaunches when there is one.
func TestRelaunch(t *testing.T) {
	defer func(q func(), s func([]string) bool) { boot.Quit, boot.StagedRelaunch = q, s }(boot.Quit, boot.StagedRelaunch)
	var relaunched [][]string
	boot.StagedRelaunch = func(args []string) bool {
		relaunched = append(relaunched, args)
		return true
	}
	cancel, ending := false, false
	stop := OnQuit(func(e *QuitEvent) {
		if cancel {
			e.Cancel()
		}
	})
	defer stop()
	boot.Quit = func() { boot.QuitRequested(ending) }

	Relaunch()
	if len(relaunched) != 1 || !slices.Equal(relaunched[0], os.Args[1:]) {
		t.Fatalf("relaunched %q, want once with %q", relaunched, os.Args[1:])
	}
	cancel = true
	Relaunch()
	cancel = false
	Quit() // a later quit doesn't relaunch what was canceled
	if len(relaunched) != 1 {
		t.Fatalf("a canceled quit relaunched: %q", relaunched)
	}
	ending = true
	Relaunch()
	if len(relaunched) != 1 {
		t.Fatalf("the session's end relaunched: %q", relaunched)
	}

	boot.Quit = nil
	Relaunch() // a phone's, a browser's: nothing
	if relaunching.Load() {
		t.Fatal("a relaunch waits where the app can't quit")
	}
}

// A relaunched process waits for the one that started it to exit.
func TestWaitExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep")
	}
	p := exec.Command("sleep", "60")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		waitExit(p.Process.Pid, time.Minute)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("waitExit returned while the process ran")
	case <-time.After(100 * time.Millisecond):
	}
	p.Process.Kill()
	p.Wait()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("waitExit still waits for a process that exited")
	}
}

func TestBundleOf(t *testing.T) {
	if b := bundleOf("/Applications/Notes.app/Contents/MacOS/Notes"); b != "/Applications/Notes.app" {
		t.Errorf("bundleOf = %q", b)
	}
	if b := bundleOf("/usr/local/bin/notes"); b != "" {
		t.Errorf("bundleOf = %q", b)
	}
}

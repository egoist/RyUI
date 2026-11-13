package update

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// fakeUpdater replaces what the updater does outside the main thread: the
// feed answers what found says, installing reports progress then installs
// (or fails with installErr), timers are the test's.
type fakeUpdater struct {
	mu         sync.Mutex
	found      *Release
	checkErr   error
	installErr error
	checks     int
	installs   []string
	relaunches int
	timers     []time.Duration
	fire       []func()
	clock      time.Time
}

// tick lets timer i's time pass, and fires it.
func (f *fakeUpdater) tick(i int) {
	f.clock = f.clock.Add(f.timers[i])
	f.fire[i]()
}

func setup(t *testing.T) (*fake.Host, *fakeUpdater) {
	t.Helper()
	f := &fakeUpdater{}
	saved := []any{checkFn, installFn, supportedFn, relaunchFn, afterFunc, prefsDir, now}
	f.clock = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	now = func() time.Time { return f.clock }
	t.Cleanup(func() {
		now = saved[6].(func() time.Time)
		checkFn = saved[0].(func(context.Context) (*Release, error))
		installFn = saved[1].(func(*Release, context.Context, func(int64, int64)) error)
		supportedFn = saved[2].(func() error)
		relaunchFn = saved[3].(func())
		afterFunc = saved[4].(func(time.Duration, func()) func() bool)
		prefsDir = saved[5].(func() (string, error))
		ctl = controller{}
		store.loaded, store.file, store.p = false, "", prefs{}
	})
	ctl = controller{}
	store.loaded, store.file, store.p = false, "", prefs{}
	dir := t.TempDir()
	prefsDir = func() (string, error) { return dir, nil }
	checkFn = func(ctx context.Context) (*Release, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.checks++
		return f.found, f.checkErr
	}
	installFn = func(r *Release, ctx context.Context, progress func(int64, int64)) error {
		progress(r.Size/2, r.Size)
		progress(r.Size, r.Size)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.installErr != nil {
			return f.installErr
		}
		f.installs = append(f.installs, r.Version)
		return nil
	}
	supportedFn = func() error { return nil }
	relaunchFn = func() { f.relaunches++ }
	afterFunc = func(d time.Duration, fn func()) func() bool {
		f.timers = append(f.timers, d)
		f.fire = append(f.fire, fn)
		return func() bool { return true }
	}
	return fake.New(), f
}

// settle runs what goroutines post until cond holds (bounded: a broken
// updater fails rather than hangs).
func settle(t *testing.T, h *fake.Host, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		h.RunPosted()
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s (state %+v)", what, ctl.state.Peek())
}

func phase() Phase { return ctl.state.Peek().Phase }

// tap presses the button titled title in the update window.
func tap(t *testing.T, h *fake.Host, title string) {
	t.Helper()
	h.Tap(h.One(ops.KindButton, title))
	h.RunPosted()
}

func mountUpdater(h *fake.Host) *ui.Root {
	r := ui.Mount(h, func() ui.Node {
		return ui.Column(ui.Style{}, Window(Options{}), ui.StaticText("app"))
	})
	r.Flush()
	return r
}

// A check in the background soon after launch finds a release: the window
// offers it with its notes; Skip This Version keeps the next background
// check quiet about it, but not the user's check, and a newer one is
// offered again.
func TestUpdaterOffersAndSkips(t *testing.T) {
	h, f := setup(t)
	r := mountUpdater(h)
	defer r.Dispose()
	if len(f.timers) != 1 || f.timers[0] != firstCheck {
		t.Fatalf("the first check is in %v, want %v", f.timers, firstCheck)
	}
	f.found = &Release{Version: "1.1.0", Notes: "## Faster\n\nEverything is faster.", Size: 100}
	f.tick(0)
	settle(t, h, "the offer", func() bool { return phase() == Available })
	r.Flush()
	if !ctl.open.Peek() {
		t.Fatal("the window didn't open for a release found in the background")
	}
	if !richTextHas(h, "Everything is faster.") {
		t.Error("the window doesn't show the release notes")
	}
	tap(t, h, "Skip This Version")
	r.Flush()
	if ctl.open.Peek() || phase() != Idle || readPrefs().SkippedVersion != "1.1.0" {
		t.Fatalf("after Skip: open %v, %+v, prefs %+v", ctl.open.Peek(), ctl.state.Peek(), readPrefs())
	}
	if LastCheck().IsZero() {
		t.Error("the check wasn't recorded")
	}

	// The next background check: the same version, skipped.
	ctl.begin(false)
	settle(t, h, "the check", func() bool { return f.checks == 2 && phase() == Idle })
	if ctl.open.Peek() {
		t.Fatal("a skipped version was offered again")
	}
	// The user's check offers it.
	CheckForUpdates()
	settle(t, h, "the user's check", func() bool { return phase() == Available })
	tap(t, h, "Remind Me Later")
	if ctl.open.Peek() || phase() != Idle {
		t.Fatal("Remind Me Later didn't close the window")
	}
	// A newer version is offered, skipped or not.
	f.found = &Release{Version: "1.2.0", Size: 100}
	ctl.begin(false)
	settle(t, h, "the newer one", func() bool { return phase() == Available })
}

// Install Update shows the download's progress, then offers to relaunch.
func TestUpdaterInstallsAndRelaunches(t *testing.T) {
	h, f := setup(t)
	r := mountUpdater(h)
	defer r.Dispose()
	f.found = &Release{Version: "2.0.0", Size: 4 << 20}
	CheckForUpdates()
	settle(t, h, "the offer", func() bool { return phase() == Available })
	r.Flush()
	tap(t, h, "Install Update")
	settle(t, h, "the install", func() bool { return phase() == Ready })
	r.Flush()
	if len(f.installs) != 1 || f.installs[0] != "2.0.0" {
		t.Fatalf("installed %v", f.installs)
	}
	if len(h.Find(ops.KindText, "Ready to Relaunch")) != 1 {
		t.Error("the window doesn't say it's ready")
	}
	tap(t, h, "Relaunch Now")
	if f.relaunches != 1 {
		t.Fatal("Relaunch Now didn't relaunch")
	}
	// Installed: no more checks until the app relaunches.
	before := len(f.timers)
	ctl.schedule()
	if len(f.timers) != before {
		t.Error("a check was scheduled with an update installed")
	}
}

// With "Automatically download and install updates" checked, a release
// found in the background is installed without a window.
func TestUpdaterAutomaticDownloads(t *testing.T) {
	h, f := setup(t)
	r := mountUpdater(h)
	defer r.Dispose()
	f.found = &Release{Version: "1.0.1", Size: 10}
	CheckForUpdates()
	settle(t, h, "the offer", func() bool { return phase() == Available })
	r.Flush()
	h.Flip(h.OfKind(ops.KindToggle)[0]) // the checkbox
	h.RunPosted()
	tap(t, h, "Remind Me Later")
	if !AutomaticDownloads() {
		t.Fatal("the checkbox wasn't kept")
	}
	f.found = &Release{Version: "1.0.2", Size: 10}
	ctl.begin(false)
	settle(t, h, "the quiet install", func() bool { return phase() == Ready })
	if ctl.open.Peek() {
		t.Error("the window opened for an automatic install")
	}
	if len(f.installs) != 1 || f.installs[0] != "1.0.2" {
		t.Errorf("installed %v", f.installs)
	}
}

// The user's check says when the app is up to date, when it failed, and
// when the app can't update itself; the background's says nothing.
func TestUpdaterStatus(t *testing.T) {
	h, f := setup(t)
	r := mountUpdater(h)
	defer r.Dispose()

	CheckForUpdates()
	settle(t, h, "up to date", func() bool { return phase() == UpToDate })
	r.Flush()
	if len(h.Find(ops.KindText, "You’re up to date!")) != 1 {
		t.Error("the window doesn't say the app is up to date")
	}
	tap(t, h, "OK")

	f.checkErr = errors.New("offline")
	ctl.begin(false)
	settle(t, h, "a quiet failure", func() bool { return f.checks == 2 && phase() == Idle })
	if ctl.open.Peek() {
		t.Error("a background check's failure opened the window")
	}
	// It's tried again sooner than the interval.
	if last := f.timers[len(f.timers)-1]; last > time.Hour {
		t.Errorf("after a failure, the next check is in %v", last)
	}
	CheckForUpdates()
	settle(t, h, "the failure", func() bool { return phase() == Failed })
	r.Flush()
	if len(h.Find(ops.KindText, "Update Error!")) != 1 || len(h.Find(ops.KindText, "offline")) != 1 {
		t.Error("the window doesn't say the check failed, and why")
	}
	tap(t, h, "OK")

	supportedFn = func() error { return ErrManaged }
	CheckForUpdates()
	h.RunPosted()
	r.Flush()
	if phase() != Unavailable || len(h.Find(ops.KindText, "Updates Unavailable")) != 1 {
		t.Errorf("an app a package manager installed: %+v", ctl.state.Peek())
	}
	tap(t, h, "OK")
	// Nor does it check in the background.
	before := len(f.timers)
	ctl.schedule()
	if len(f.timers) != before {
		t.Error("an app that can't update itself schedules checks")
	}
}

// A failed install says so, and closing the window during a download
// cancels it.
func TestUpdaterInstallFails(t *testing.T) {
	h, f := setup(t)
	r := mountUpdater(h)
	defer r.Dispose()
	f.found = &Release{Version: "3.0.0", Size: 10}
	f.installErr = errors.New("the update is not signed with the app's key")
	CheckForUpdates()
	settle(t, h, "the offer", func() bool { return phase() == Available })
	r.Flush()
	tap(t, h, "Install Update")
	settle(t, h, "the failure", func() bool { return phase() == Failed })
	r.Flush()
	if s := ctl.state.Peek(); !s.Installing || len(h.Find(ops.KindText, "An error occurred while installing the update. Please try again later.")) != 1 {
		t.Errorf("the window doesn't say the install failed: %+v", s)
	}
}

// The background checks stop with the Window, and the next is an interval
// after the last that succeeded.
func TestUpdaterSchedule(t *testing.T) {
	h, f := setup(t)
	r := mountUpdater(h)
	f.tick(0)
	settle(t, h, "the check", func() bool { return f.checks == 1 && phase() == Idle && len(f.timers) == 2 })
	// An hour at most: a computer asleep stops timers, not the clock.
	if f.timers[1] != wakeEvery {
		t.Errorf("the next check's timer is in %v", f.timers[1])
	}
	SetAutomaticChecks(false)
	if AutomaticChecks() {
		t.Fatal("automatic checks stayed on")
	}
	n := len(f.timers)
	ctl.schedule()
	if len(f.timers) != n {
		t.Error("a check was scheduled with automatic checks off")
	}
	SetAutomaticChecks(true)
	r.Dispose()
	if ctl.running {
		t.Error("the updater runs without its Window")
	}
}

// richTextHas reports whether a RichText (Markdown's paragraphs) says s.
func richTextHas(h *fake.Host, s string) bool {
	for _, v := range h.OfKind(ops.KindRichText) {
		for _, p := range v.Rich() {
			if p.Text() == s {
				return true
			}
		}
	}
	return false
}

func TestFormatSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 KB", 1: "1 KB", 1536: "2 KB", 5 << 20: "5.0 MB", 12_900_000: "12.3 MB"} {
		if got := formatSize(n); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestMenuItem(t *testing.T) {
	if it := MenuItem(); it.Title != "Check for Updates…" || it.OnPress == nil {
		t.Errorf("MenuItem = %+v", it)
	}
	if m := AppMenu(); len(m.Items) != 1 || m.Title == "" {
		t.Errorf("AppMenu = %+v", m)
	}
}

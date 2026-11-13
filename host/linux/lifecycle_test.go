//go:build linux && !android

package linux

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/asuka/crash"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/crashrun"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/store"
	"github.com/egoist/asuka/ui"
)

// lifecycles records the Lifecycle events the core gets, as an app's
// app.OnLifecycle does, until the test ends.
func lifecycles(t *testing.T) *[]ops.Lifecycle {
	var got []ops.Lifecycle
	n := len(boot.Lifecycle)
	boot.Lifecycle = append(boot.Lifecycle, func(s int) { got = append(got, ops.Lifecycle(s)) })
	t.Cleanup(func() { onMain(func() { boot.Lifecycle = boot.Lifecycle[:n] }) })
	return &got
}

// The app goes to the background as its window stops showing (hidden,
// minimized, suspended) and comes back as it shows again, once each; a
// memory warning is one each time GLib's monitor warns.
func TestLifecycle(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	got := lifecycles(t)
	want := func(w ...ops.Lifecycle) {
		t.Helper()
		settle(t, h, func() bool { return len(*got) >= len(w) })
		onMain(func() {
			if len(*got) != len(w) {
				fatalf(t, "lifecycle %v, want %v", *got, w)
			}
			for i := range w {
				if (*got)[i] != w[i] {
					fatalf(t, "lifecycle %v, want %v", *got, w)
				}
			}
		})
	}
	onMain(func() { WidgetSetVisible(h.main.window, false) })
	want(ops.LifecycleBackground)
	onMain(func() { WindowPresent(h.main.window) })
	want(ops.LifecycleBackground, ops.LifecycleForeground)

	// Minimized, then suspended (the compositor's state; the test's X
	// server has no window manager to minimize it, so the state is the
	// test's): a notify of the surface's state is what GTK sends.
	state := uint32(0)
	prev := toplevelState
	toplevelState = func(Ptr) uint32 { return state }
	t.Cleanup(func() { toplevelState = prev })
	notifyState := func(s uint32) {
		onMain(func() {
			state = s
			Call(Sym("g_object_notify"), NativeGetSurface(h.main.window), Addr(CString("state")))
		})
	}
	notifyState(toplevelMinimized)
	want(ops.LifecycleBackground, ops.LifecycleForeground, ops.LifecycleBackground)
	notifyState(toplevelSuspended | toplevelMinimized)
	notifyState(0)
	want(ops.LifecycleBackground, ops.LifecycleForeground, ops.LifecycleBackground, ops.LifecycleForeground)
	notifyState(1 << 6) // focused: shown
	want(ops.LifecycleBackground, ops.LifecycleForeground, ops.LifecycleBackground, ops.LifecycleForeground)

	onMain(func() {
		h.watchMemory()
		m := Call(Sym("g_memory_monitor_dup_default"))
		Call(Sym("g_signal_emit_by_name"), m, Addr(CString("low-memory-warning")), 50)
		ObjectUnref(m)
	})
	want(ops.LifecycleBackground, ops.LifecycleForeground, ops.LifecycleBackground, ops.LifecycleForeground, ops.LifecycleMemoryWarning)
}

// A value set just before the user closes the app's last window is on
// disk as the window goes: Background writes the store then.
func TestQuitWritesStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ASUKA_DATA", dir)
	h, _ := mount(t, emptyRoot)
	got := lifecycles(t)
	var other *win
	onMain(func() {
		// A second window the app has open: closing the main one leaves
		// it showing, so the app isn't in the background yet.
		other = h.newWin(AdwWindowNew())
		WindowPresent(other.window)
	})
	settle(t, h, func() bool { return WidgetGetMapped(other.window) })
	onMain(func() {
		if err := store.Set("draft", "last words"); err != nil {
			fatalf(t, "%v", err)
		}
		main := h.main.window
		WindowClose(main) // as the user does: close-request, then destroyed
		if h.wins[main] != nil {
			fatalf(t, "the closed window is still the app's")
		}
		if len(*got) != 0 {
			fatalf(t, "lifecycle %v with a window showing", *got)
		}
		WindowDestroy(other.window)
		if len(*got) != 1 || (*got)[0] != ops.LifecycleBackground {
			fatalf(t, "lifecycle %v as the last window closed, want [Background]", *got)
		}
		b, err := os.ReadFile(filepath.Join(dir, "store.json"))
		if err != nil || !strings.Contains(string(b), "last words") {
			fatalf(t, "the store as the app closed: %q, %v", b, err)
		}
		// Quitting after (a signal, the session ending): nothing again.
		h.lifecycle(ops.LifecycleBackground)
		h.quitting = true
		h.updateLifecycle()
		if len(*got) != 1 {
			fatalf(t, "lifecycle %v", *got)
		}
	})
}

// The child of TestCrashReport: an app whose host started its crash
// record, which panics on a goroutine (go), or crashes in C (native: an
// abort in GLib's code, as a g_error is).
func init() {
	kind := os.Getenv("ASUKA_LINUX_CRASH_CHILD")
	if kind == "" {
		return
	}
	boot.Version, boot.Build = "2.1", "9"
	startCrash()
	crash.Breadcrumb("opened the editor")
	if kind == "native" {
		if err := Load(); err != nil {
			os.Exit(3)
		}
		Call(Sym("abort"))
	}
	done := make(chan bool)
	go func() {
		var m map[string]int
		m["x"]++
		done <- true
	}()
	<-done
	os.Exit(0)
}

// The Linux host keeps the app's crashes (asuka/crash): a Go panic, and a
// crash in C, each the next run's report with the run's context, the
// computer's and the app's.
func TestCrashReport(t *testing.T) {
	for _, kind := range []string{"go", "native"} {
		t.Run(kind, func(t *testing.T) {
			data := t.TempDir()
			t.Setenv("ASUKA_DATA", data)
			crashrun.Reset(data)
			t.Cleanup(func() { crashrun.Reset("") })
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "ASUKA_LINUX_CRASH_CHILD="+kind, "ASUKA_DATA="+data)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the child didn't crash:\n%s", out)
			}
			startCrash() // this run's record, to read the last's
			if !crashrun.Started() {
				t.Fatal("not started")
			}
			rs, err := crash.Reports()
			if err != nil {
				t.Fatal(err)
			}
			if len(rs) != 1 {
				t.Fatalf("%d reports, want 1:\n%s", len(rs), out)
			}
			r := rs[0]
			if !r.Fatal || len(r.Threads) == 0 {
				t.Fatalf("report %+v", r)
			}
			switch kind {
			case "go":
				if r.Kind != crash.Go || r.Message != "panic: assignment to entry in nil map" {
					t.Fatalf("report %s %q", r.Kind, r.Message)
				}
			case "native":
				if r.Kind != crash.Native || r.Message != "SIGABRT: abort" || !strings.Contains(r.Text, "purego") {
					t.Fatalf("report %s %q:\n%s", r.Kind, r.Message, r.Text)
				}
			}
			c := r.Context
			if c.System != "Linux" || c.SystemVersion == "" || c.AppVersion != "2.1" || c.Build != "9" || c.PID != cmd.Process.Pid {
				b, _ := json.Marshal(c)
				t.Fatalf("context %s", b)
			}
			if len(c.Breadcrumbs) != 1 || c.Breadcrumbs[0].Message != "opened the editor" {
				t.Fatalf("breadcrumbs %+v", c.Breadcrumbs)
			}
		})
	}
}

// A tapped notification's URL goes to ui.OnOpenURL, as on the other
// platforms, not to the user's browser.
func TestNotificationURL(t *testing.T) {
	var opened []string
	h, _ := mount(t, func() ui.Node {
		ui.OnOpenURL(func(u string) { opened = append(opened, u) })
		return emptyRoot()
	})
	onMain(func() {
		notifyBackend = "gio"
		h.installNotifications()
		notes.fdo = false
	})
	defer func() { notifyBackend = "" }()
	if _, err := callModule(t, h, "notify", "schedule", []byte(`{"id":"u","title":"Reply","url":"asukatest://chat/7"}`)); err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		Call(Sym("g_action_group_activate_action"), h.app, Addr(CString(notifyAction)), VStr(`{"id":"u"}`))
	})
	settle(t, h, func() bool { return len(opened) > 0 })
	onMain(func() {
		if len(opened) != 1 || opened[0] != "asukatest://chat/7" {
			fatalf(t, "opened %v", opened)
		}
	})
	// The app hears the tap too (notify.OnAction), with the URL.
	b, err := callModule(t, h, "notify", "nextAction", nil)
	if err != nil || !strings.Contains(string(b), `"url":"asukatest://chat/7"`) {
		t.Fatalf("the tap's response: %s, %v", b, err)
	}
	callModule(t, h, "notify", "removeAll", nil)
}

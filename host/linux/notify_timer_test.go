//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/notifyspec"
)

// The timer's unit name and systemd-run's arguments.
func TestNotifyTimerArgs(t *testing.T) {
	if u := notifyUnit("com.example.Notes", "chat-42"); u != `asuka-com.example.Notes-chat\x2d42` {
		t.Fatalf("unit %q", u)
	}
	if u := notifyUnit("com.example.Notes", ".a b/é"); u != `asuka-com.example.Notes-\x2ea\x20b\x2f\xc3\xa9` {
		t.Fatalf("unit %q", u)
	}
	if u := notifyUnit("com.example.Notes", strings.Repeat("x", 300)); len(u) > 200 || !strings.HasPrefix(u, "asuka-com.example.Notes-") {
		t.Fatalf("a long ID's unit %q", u)
	}
	due := time.Date(2026, 11, 13, 18, 30, 5, 0, time.FixedZone("CET", 3600))
	got := notifyTimerArgs("u", "/opt/notes/bin/notes", "chat-42", "/home/a/.local/share/notes", due)
	want := []string{"--user", "--quiet", "--collect", "--unit=u", "--on-calendar=2026-11-13 17:30:05 UTC",
		"--timer-property=AccuracySec=1s", "--timer-property=RemainAfterElapse=no",
		"--setenv=ASUKA_NOTIFY=chat-42", "--setenv=ASUKA_DATA=/home/a/.local/share/notes", "/opt/notes/bin/notes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q\nwant %q", got, want)
	}
}

// A notification due later is stored and given a systemd timer (not the
// app's), which cancel stops; if systemd-run fails, the app's timer
// shows it.
func TestNotifySystemTimer(t *testing.T) {
	t.Setenv("ASUKA_DATA", t.TempDir())
	var mu sync.Mutex
	var ran []string
	fail := false
	oldTimers, oldRun := systemdTimers, userSystemd
	onMain(func() {
		systemdTimers = func() bool { return true }
		userSystemd = func(name string, args ...string) error {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, name+" "+strings.Join(args, " "))
			if fail && name == "systemd-run" {
				return errors.New("no user manager")
			}
			return nil
		}
	})
	defer onMain(func() { systemdTimers, userSystemd = oldTimers, oldRun })
	h, _ := mount(t, emptyRoot)
	onMain(func() {
		notifyBackend = "gio"
		h.installNotifications()
	})
	defer func() { notifyBackend = "" }()
	commands := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), ran...)
	}
	if _, err := callModule(t, h, "notify", "schedule", []byte(`{"id":"later","title":"Stand up","data":{"k":"v"},"seconds":90}`)); err != nil {
		t.Fatal(err)
	}
	var st storedNote
	b, err := os.ReadFile(notifyStorePath("later"))
	if err != nil || json.Unmarshal(b, &st) != nil || st.Spec.Title != "Stand up" || st.Spec.Data["k"] != "v" || st.Due < time.Now().Add(88*time.Second).UnixMilli() {
		t.Fatalf("stored %s %v", b, err)
	}
	unit := ""
	onMain(func() { unit = notifyUnit(h.appID, "later") })
	c := commands()
	if len(c) != 2 || c[0] != "systemctl --user stop "+unit+".timer" || !strings.HasPrefix(c[1], "systemd-run --user --quiet --collect --unit="+unit+" --on-calendar=") ||
		!strings.Contains(c[1], "--setenv=ASUKA_NOTIFY=later") {
		t.Fatalf("ran %q", c)
	}
	onMain(func() {
		if notes.timers["later"] != 0 {
			fatalf(t, "the app's timer too")
		}
	})
	// Due: not showing, not removed by remove.
	callModule(t, h, "notify", "remove", []byte(`["later"]`))
	if !systemDue("later") {
		t.Fatal("remove took a due notification")
	}
	if _, err := callModule(t, h, "notify", "cancel", []byte("later")); err != nil {
		t.Fatal(err)
	}
	if systemDue("later") {
		t.Fatal("still stored after cancel")
	}
	settle(t, h, func() bool {
		c := commands()
		return len(c) == 3 && c[2] == "systemctl --user stop "+unit+".timer"
	})

	// systemd-run fails: the app's timer.
	mu.Lock()
	fail = true
	mu.Unlock()
	if _, err := callModule(t, h, "notify", "schedule", []byte(`{"id":"x","title":"X","seconds":90}`)); err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		if notes.timers["x"] == 0 || systemDue("x") {
			fatalf(t, "no fallback: timer %d, stored %v", notes.timers["x"], systemDue("x"))
		}
		h.withdraw("x")
	})
}

// A run with ASUKA_NOTIFY: the notification shows through the
// freedesktop server, and a tap launches the app with it (here: a test's
// launcher), or is the running app's; closed, the run ends. With the app
// running, it's handed over and shown as one due.
func TestNotifyHeadless(t *testing.T) {
	if exec.Command("python3", "-c", "from gi.repository import Gio").Run() != nil {
		t.Skip("no python3-gi for the test's notification server")
	}
	t.Setenv("ASUKA_DATA", t.TempDir())
	server := exec.Command("python3", "testdata/notifications.py")
	in, _ := server.StdinPipe()
	out, _ := server.StdoutPipe()
	if err := server.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { in.Close(); server.Wait() }()
	ready := make([]byte, 6)
	if _, err := io.ReadFull(out, ready); err != nil || string(ready) != "ready\n" {
		t.Fatalf("the server: %q %v", ready, err)
	}
	var launched []string
	oldLaunch := notifyLaunch
	onMain(func() {
		notifyLaunch = func(target string) error { launched = append(launched, target); return nil }
	})
	defer onMain(func() {
		notifyLaunch = oldLaunch
		notes.fdo = false
	})
	h, _ := mount(t, emptyRoot)
	store := func(id, title string) {
		b, _ := json.Marshal(storedNote{Spec: mustSpec(`{"id":"` + id + `","title":"` + title + `","url":"myapp://n/` + id + `","data":{"k":"` + id + `"}}`)})
		os.MkdirAll(filepath.Dir(notifyStorePath(id)), 0o755)
		os.WriteFile(notifyStorePath(id), b, 0o600)
	}
	shownIDs := func() []uint32 {
		v, _ := fdoCall(t, "Shown", nil).([]any)
		var ids []uint32
		if len(v) > 0 {
			for _, x := range v[0].([]any) {
				ids = append(ids, x.([]any)[0].(uint32))
			}
		}
		return ids
	}

	// The app isn't running (the host's ID has no owner here): shown,
	// then tapped.
	store("n1", "One")
	code := -1
	onMain(func() { h.fireDue("n1", func(c int) { code = c }) })
	var ids []uint32
	for i := 0; i < 2000 && len(ids) == 0; i++ {
		ids = shownIDs()
	}
	if len(ids) != 1 || systemDue("n1") {
		t.Fatalf("shown %v; still stored %v", ids, systemDue("n1"))
	}
	fdoCall(t, "Invoke", func() Ptr { return VTuple(VU32(ids[0]), VStr("default")) })
	settle(t, h, func() bool { return code == 0 && len(launched) == 1 })
	var tap notifyTarget
	if json.Unmarshal([]byte(launched[0]), &tap) != nil || tap.ID != "n1" || tap.Action != "" || tap.Shown == nil ||
		tap.Shown.Title != "One" || tap.Shown.Data["k"] != "n1" || tap.Shown.URL != "myapp://n/n1" {
		t.Fatalf("launched with %s", launched[0])
	}

	// Closed without a tap: the run ends, nothing launched.
	store("n2", "Two")
	code = -1
	onMain(func() { h.fireDue("n2", func(c int) { code = c }) })
	ids = nil
	for i := 0; i < 2000 && len(ids) == 0; i++ {
		ids = shownIDs()
	}
	if len(ids) != 1 {
		t.Fatalf("shown %v", ids)
	}
	onMain(func() {
		conn, _ := Bus(BusSession)
		DBusCall(conn, fdoDest, fdoPath, fdoIface, "CloseNotification", VTuple(VU32(ids[0])), -1, func(Ptr, error) {})
	})
	settle(t, h, func() bool { return code == 0 })
	if len(launched) != 1 {
		t.Fatalf("a closed notification launched the app: %q", launched)
	}

	// Cancelled since: nothing shows.
	code = -1
	onMain(func() { h.fireDue("gone", func(c int) { code = c }) })
	settle(t, h, func() bool { return code == 0 })

	// The app runs (a unique application of the test's owns its ID):
	// it's handed the notification, which it shows as due.
	store("n3", "Three")
	code = -1
	onMain(func() {
		app := Call(Sym("g_application_new"), Addr(CString("dev.asuka.HostTestUnique")), 0)
		var gerr Ptr
		if !ApplicationRegister(app, 0, &gerr) {
			fatalf(t, "register: %v", GError(gerr))
		}
		addNotifyActions(app)
		h.appID = "dev.asuka.HostTestUnique"
		notes.headlessTap, notes.headlessClosed = nil, nil
		h.fireDue("n3", func(c int) { code = c })
	})
	settle(t, h, func() bool {
		return code == 0 && notes.showing["n3"] != nil && notes.showing["n3"].shown.Title == "Three"
	})
}

func mustSpec(s string) (sp notifyspec.Spec) {
	json.Unmarshal([]byte(s), &sp)
	return sp
}

// systemd itself (the user's manager, where it runs): the arguments make
// a timer that fires once at its time, running the program with
// ASUKA_NOTIFY, and is gone after; stopped, it's gone.
func TestNotifySystemdReal(t *testing.T) {
	notifyUnique = true
	ok := systemdTimers()
	notifyUnique = false
	if !ok {
		t.Skip("no systemd user manager")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "app")
	outFile := filepath.Join(dir, "fired")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$ASUKA_NOTIFY $ASUKA_DATA\" > "+outFile+"\n"), 0o755)
	unit := notifyUnit("dev.asuka.HostTest", "real-"+filepath.Base(dir))
	defer userSystemd("systemctl", "--user", "stop", unit+".timer")
	due := time.Now().Add(2 * time.Second)
	if err := userSystemd("systemd-run", notifyTimerArgs(unit, script, "real one", dir, due)...); err != nil {
		t.Fatal(err)
	}
	if err := userSystemd("systemctl", "--user", "is-active", "--quiet", unit+".timer"); err != nil {
		t.Fatalf("the timer isn't active: %v", err)
	}
	var b []byte
	for i := 0; i < 300; i++ { // until it fires (a second's accuracy)
		if b, _ = os.ReadFile(outFile); len(b) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if strings.TrimSpace(string(b)) != "real one "+dir {
		t.Fatalf("ran with %q", b)
	}
	for i := 0; i < 100 && userSystemd("systemctl", "--user", "is-active", "--quiet", unit+".timer") == nil; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if userSystemd("systemctl", "--user", "is-active", "--quiet", unit+".timer") == nil {
		t.Fatal("the timer stays after it fired")
	}
	// One due later, stopped.
	if err := userSystemd("systemd-run", notifyTimerArgs(unit, script, "later", dir, time.Now().Add(time.Hour))...); err != nil {
		t.Fatal(err)
	}
	if err := userSystemd("systemctl", "--user", "stop", unit+".timer"); err != nil {
		t.Fatal(err)
	}
	if userSystemd("systemctl", "--user", "is-active", "--quiet", unit+".timer") == nil {
		t.Fatal("the timer runs after stop")
	}
}

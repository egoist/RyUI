//go:build linux && !android

package linux

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/notifyspec"
)

// Notifications due later fire whether the app runs or not, as iOS's and
// Android's do: the system keeps them, a systemd user timer each
// (systemd-run --user --on-calendar, the time in UTC, to the second),
// which runs the app's executable with ASUKA_NOTIFY set to the
// notification's ID. The notification itself waits in the app's data
// (notify/<hex of its ID>.json): there while it's due, removed as it
// fires or is cancelled (and its timer stopped).
//
// Run with ASUKA_NOTIFY, the app shows no window and starts no GTK
// (notifyHeadless): if it's running (its ID's name on the session bus:
// a unique app), it hands the notification over (org.freedesktop.
// Application's ActivateAction, "asuka-notify-due"), which shows it as
// one due while it runs (OnPresent and all); else it shows it through
// the freedesktop notification server and waits, outside the app's
// name, until it's closed (it quits) or the user taps it or a button
// (ActionInvoked): then it hands the tap to the app if it runs since
// ("asuka-notify"), else launches the app in a scope of its own
// (systemd-run --user --scope) with ASUKA_NOTIFY_ACTION, the tap, which
// the app answers as it starts, as a tap while it runs.
//
// The timers are used when the app is unique (an app ID of its own, not
// a asuka dev, test or automation run), outside a sandbox (a Flatpak
// can't reach systemd), and the user's systemd runs; else a timer of the
// app's shows it while it runs, as before. asuka/notify has no repeating
// schedules: a timer is one time (OnCalendar), gone once it fires
// (RemainAfterElapse=no).

// notifyUnique: the app owns its ID on the session bus (run sets it).
var notifyUnique bool

// systemdTimers reports whether notifications due later are systemd's
// (tests replace it).
var systemdTimers = func() bool {
	if !notifyUnique || os.Getenv("FLATPAK_ID") != "" {
		return false
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(rt, "systemd", "private"))
	return err == nil
}

// userSystemd runs systemctl or systemd-run (tests replace it). Off the
// main thread.
var userSystemd = func(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// storedNote is a notification due later, in the app's data.
type storedNote struct {
	Spec  notifyspec.Spec `json:"spec"`
	Image string          `json:"image,omitempty"`
	Due   int64           `json:"due"` // ms since 1970
}

func notifyStorePath(id string) string {
	return filepath.Join(dataDir(), "notify", hex.EncodeToString([]byte(id))+".json")
}

// notifyUnit is the timer's unit name (without its suffix): the app's
// and the notification's IDs, escaped as systemd-escape does (a byte
// outside letters, digits, ':', '_' and '.' as \xNN), hashed if long.
func notifyUnit(appID, id string) string {
	esc := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '_' || c == '.' && i > 0 {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, `\x%02x`, c)
			}
		}
		return b.String()
	}
	u := "asuka-" + esc(appID) + "-" + esc(id)
	if len(u) > 200 { // systemd's names are 255 bytes at most, with ".timer"
		sum := sha256.Sum256([]byte(id))
		u = "asuka-" + esc(appID) + "-" + hex.EncodeToString(sum[:16])
	}
	return u
}

// notifyTimerArgs are systemd-run's arguments for a notification's
// timer: at due (UTC, to the second, within a second), running exe with
// ASUKA_NOTIFY (and the app's data directory, which it reads it from).
func notifyTimerArgs(unit, exe, id, data string, due time.Time) []string {
	return []string{
		"--user", "--quiet", "--collect",
		"--unit=" + unit,
		"--on-calendar=" + due.UTC().Format("2006-01-02 15:04:05") + " UTC",
		"--timer-property=AccuracySec=1s",
		"--timer-property=RemainAfterElapse=no",
		"--setenv=ASUKA_NOTIFY=" + id,
		"--setenv=ASUKA_DATA=" + data,
		exe,
	}
}

// scheduleSystemTimer stores a notification and makes its timer; done
// (main thread) gets the error, if the system couldn't take it.
func (h *Host) scheduleSystemTimer(s notifyspec.Spec, image string, done func(error)) {
	due := time.Now().Add(time.Duration(s.Seconds * float64(time.Second)))
	if due.Nanosecond() != 0 { // OnCalendar is to the second: not early
		due = due.Truncate(time.Second).Add(time.Second)
	}
	unit, path, data := notifyUnit(h.appID, s.ID), notifyStorePath(s.ID), dataDir()
	b, _ := json.Marshal(storedNote{Spec: s, Image: image, Due: due.UnixMilli()})
	go func() {
		exe, err := os.Executable()
		if err == nil {
			err = os.MkdirAll(filepath.Dir(path), 0o755)
		}
		if err == nil {
			err = os.WriteFile(path, b, 0o600)
		}
		if err == nil {
			userSystemd("systemctl", "--user", "stop", unit+".timer") // one of the same ID: replaced
			err = userSystemd("systemd-run", notifyTimerArgs(unit, exe, s.ID, data, due)...)
			if err != nil {
				os.Remove(path)
			}
		}
		h.Post(func() { done(err) })
	}()
}

// systemDue reports whether a notification waits for its systemd timer.
func systemDue(id string) bool {
	_, err := os.Stat(notifyStorePath(id))
	return err == nil
}

// cancelSystemTimer forgets a notification due (in this run or an
// earlier one), and stops its timer.
func (h *Host) cancelSystemTimer(id string) {
	path := notifyStorePath(id)
	if _, err := os.Stat(path); err != nil {
		return
	}
	os.Remove(path)
	unit := notifyUnit(h.appID, id)
	go userSystemd("systemctl", "--user", "stop", unit+".timer")
}

// The application's actions a timer's notification reaches it by: one
// due (the stored notification's JSON), and a tap (notifyTarget's).
const notifyDueAction = "asuka-notify-due"

// installNotifyActions adds the application's notification actions, as
// it starts: a run that hasn't used asuka/notify yet is handed what's
// due and what's tapped.
func (h *Host) installNotifyActions() {
	if notes.actionsAdded {
		return
	}
	notes.actionsAdded = true
	addNotifyActions(h.app)
}

// addNotifyActions adds the notification actions to an application.
func addNotifyActions(app Ptr) {
	t := VariantTypeNew("s")
	a := Sys.SimpleActionNew(notifyAction, t)
	Connect(a, "activate", func(param uintptr) {
		if param == 0 {
			return
		}
		s, _ := VariantGo(param).(string)
		var target notifyTarget
		if json.Unmarshal([]byte(s), &target) == nil && theHost != nil {
			theHost.notificationActivated(target) // the app's host (a test's newest)
		}
	})
	ActionMapAddAction(app, a)
	ObjectUnref(a)
	due := Sys.SimpleActionNew(notifyDueAction, t)
	Sys.VariantTypeFree(t)
	Connect(due, "activate", func(param uintptr) {
		if param == 0 || theHost == nil {
			return
		}
		s, _ := VariantGo(param).(string)
		var st storedNote
		if json.Unmarshal([]byte(s), &st) != nil || st.Spec.ID == "" {
			return
		}
		h := theHost
		if h.installNotifications() == nil {
			st.Spec.Seconds = 0
			h.deliver(st.Spec, st.Image)
		}
	})
	ActionMapAddAction(app, due)
	ObjectUnref(due)
}

// The app's object on the session bus (GApplication's: its ID, '.' a
// '/', '-' a '_').
func appObjectPath(appID string) string {
	return "/" + strings.NewReplacer(".", "/", "-", "_").Replace(appID)
}

// appRunning asks the session bus whether the app owns its name: done
// gets it, on the main thread.
func appRunning(conn Ptr, appID string, done func(bool)) {
	DBusCall(conn, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "NameHasOwner",
		VTuple(VStr(appID)), -1, func(r Ptr, err error) {
			a, _ := VariantGo(r).([]any)
			has := false
			if err == nil && len(a) > 0 {
				has, _ = a[0].(bool)
			}
			done(has)
		})
}

// activateRemote activates one of the running app's actions, with a
// string.
func activateRemote(conn Ptr, appID, action, param string, done func(error)) {
	DBusCall(conn, appID, appObjectPath(appID), "org.freedesktop.Application", "ActivateAction",
		VTuple(VStr(action), VArray("v", VVariant(VStr(param))), VDict(map[string]Ptr{})), -1,
		func(_ Ptr, err error) { done(err) })
}

// notifyLaunch starts the app with a notification's tap, in a scope of
// its own (the timer's service stops as this process quits); tests
// replace it.
var notifyLaunch = func(target string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command("systemd-run", "--user", "--scope", "--quiet", "--collect", exe)
	cmd.Env = append(withoutEnv(os.Environ(), "ASUKA_NOTIFY"), "ASUKA_NOTIFY_ACTION="+target)
	if err := cmd.Start(); err != nil {
		cmd = exec.Command(exe) // no systemd-run: a child, as the last resort
		cmd.Env = append(withoutEnv(os.Environ(), "ASUKA_NOTIFY"), "ASUKA_NOTIFY_ACTION="+target)
		return cmd.Start()
	}
	go cmd.Wait()
	return nil
}

func withoutEnv(env []string, name string) []string {
	out := env[:0:0]
	for _, e := range env {
		if !strings.HasPrefix(e, name+"=") {
			out = append(out, e)
		}
	}
	return out
}

// notifyHeadless is a run with ASUKA_NOTIFY: it shows the notification
// due and returns the exit code once it's done with it.
func (h *Host) notifyHeadless(id string) int {
	code := -1
	h.fireDue(id, func(c int) { code = c })
	for code < 0 {
		MainContextIteration(0, true)
	}
	return code
}

// fireDue shows the stored notification id (see notifyHeadless); done
// gets the exit code, on the main thread, once it's closed or tapped.
func (h *Host) fireDue(id string, done func(int)) {
	path := notifyStorePath(id)
	b, err := os.ReadFile(path)
	if err != nil { // cancelled since
		done(0)
		return
	}
	os.Remove(path)
	var st storedNote
	if json.Unmarshal(b, &st) != nil {
		done(1)
		return
	}
	conn, err := Bus(BusSession)
	if err != nil {
		fmt.Fprintln(os.Stderr, "asuka/notify:", err)
		done(1)
		return
	}
	appRunning(conn, h.appID, func(running bool) {
		if running {
			activateRemote(conn, h.appID, notifyDueAction, string(b), func(err error) {
				if err != nil {
					fmt.Fprintln(os.Stderr, "asuka/notify:", err)
					done(1)
					return
				}
				done(0)
			})
			return
		}
		h.showHeadless(conn, st, done)
	})
}

// showHeadless shows a notification through the freedesktop server and
// waits for it to be closed or tapped.
func (h *Host) showHeadless(conn Ptr, st storedNote, done func(int)) {
	notes.fdo = true
	if err := h.installFdo(); err != nil {
		done(1)
		return
	}
	s := st.Spec
	shown := shownOfSpec(s)
	finished, tapped := false, false
	finish := func(c int) {
		if !finished {
			finished = true
			notes.headlessTap, notes.headlessClosed = nil, nil
			done(c)
		}
	}
	notes.headlessClosed = func(nid string) {
		if nid == s.ID && !tapped { // a tap closes it too: it's the tap's
			finish(0)
		}
	}
	notes.headlessTap = func(t notifyTarget) {
		if t.ID != s.ID || tapped {
			return
		}
		tapped = true
		t.Shown = &shown
		tb, _ := json.Marshal(t)
		appRunning(conn, h.appID, func(running bool) {
			if running {
				activateRemote(conn, h.appID, notifyAction, string(tb), func(error) { finish(0) })
				return
			}
			if err := notifyLaunch(string(tb)); err != nil {
				fmt.Fprintln(os.Stderr, "asuka/notify:", err)
				finish(1)
				return
			}
			finish(0)
		})
	}
	if s.Badge > 0 {
		h.setBadge(s.Badge)
	}
	h.fdoNotify(s, st.Image, false, func(ok bool) {
		if !ok {
			fmt.Fprintln(os.Stderr, "asuka/notify:", errors.New("no notification server"))
			finish(1)
			return
		}
		notes.showing[s.ID] = &shownNote{shown: shown}
	})
}

// notifyActionAtStart is ASUKA_NOTIFY_ACTION, a tap the app was launched
// with: answered once the root is mounted.
func (h *Host) notifyActionAtStart() {
	s := os.Getenv("ASUKA_NOTIFY_ACTION")
	if s == "" {
		return
	}
	os.Unsetenv("ASUKA_NOTIFY_ACTION")
	var t notifyTarget
	if json.Unmarshal([]byte(s), &t) == nil && t.ID != "" {
		h.afterCore(func() { h.notificationActivated(t) })
	}
}

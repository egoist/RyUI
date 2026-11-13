//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// The personal data modules (contacts, calendar, photos, library) never
// ask the user here: their status and requests are replaced, so the
// tests check the arguments, the encodings, the alerts and the paths
// taken when the user hasn't allowed it.

// spin runs the main run loop until cond holds (a safety net bounds it,
// not what passes).
func spin(cond func() bool) bool {
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for i := 0; !cond() && i < 2000; i++ {
		until := MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSinceNow:"), 0.01)
		if ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1); ev != 0 {
			Send(app, "sendEvent:", uintptr(ev))
		}
	}
	return cond()
}

// answerSheet calls a module's method that shows an alert over the
// window, answers the alert with code (1000 the first button: Add; 1001
// Cancel), and waits for the method's answer.
func answerSheet(t *testing.T, h *Host, module, m string, args []byte, code int) ([]byte, error) {
	t.Helper()
	var out []byte
	var err error
	answered := false
	h.module(module)[m](args, func(b []byte, e error) { out, err, answered = b, e, true })
	w := h.keyWin().window
	if !spin(func() bool { return answered || Send(w, "attachedSheet") != 0 }) || answered {
		fatalf(t, "%s.%s: no alert over the window (answered %q, %v)", module, m, out, err)
	}
	sheet := Send(w, "attachedSheet")
	Send(w, "endSheet:returnCode:", uintptr(sheet), uintptr(code))
	if !spin(func() bool { return answered }) {
		fatal(t, module+"."+m+": no answer after the alert")
	}
	spin(func() bool { return Send(w, "attachedSheet") == 0 })
	return out, err
}

func TestContacts(t *testing.T) {
	status, asked := 2, 0
	oldStatus, oldRequest := contactsStatus, contactsRequest
	contactsStatus = func() int { return status }
	contactsRequest = func(done func()) {
		asked++
		if status == 0 {
			status = 3
		}
		done()
	}
	t.Cleanup(func() { contactsStatus, contactsRequest = oldStatus, oldRequest })

	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		call := func(m string, args []byte) ([]byte, error) { return await(h, "contacts", m, args) }
		if b, err := call("request", nil); err != nil || string(b) != "\x01" || asked != 0 {
			fatalf(t, "request, denied: %q, %v (asked %d)", b, err, asked)
		}
		if _, err := call("list", []byte(`{"name":"","limit":0}`)); !errors.Is(err, errNotAllowed) {
			fatalf(t, "list, denied: %v", err)
		}
		// Denied, Add: not saved, "not allowed"; Cancel: u8 0.
		args := []byte(`{"givenName":"Ada","familyName":"Lovelace","phones":[{"label":"","value":"+44 20 7946 0000"}],"emails":[{"label":"work","value":"ada@example.com"}]}`)
		if _, err := answerSheet(t, h, "contacts", "add", args, 1000); !errors.Is(err, errNotAllowed) {
			fatalf(t, "add, denied: %v", err)
		}
		if b, err := answerSheet(t, h, "contacts", "add", args, 1001); err != nil || string(b) != "\x00" {
			fatalf(t, "add, cancelled: %q, %v", b, err)
		}
		if _, err := call("add", []byte("{")); err == nil {
			fatal(t, "add with bad JSON answered no error")
		}
		status = 0
		if b, err := call("request", nil); err != nil || string(b) != "\x03" || asked != 1 {
			fatalf(t, "request, not asked: %q, %v (asked %d)", b, err, asked)
		}

		// A contact as add builds it, encoded as iOS encodes one.
		var o contactsArgs
		json.Unmarshal([]byte(`{"givenName":"Ada","familyName":"Lovelace","organization":"Analytical Engines","jobTitle":"Programmer",
			"phones":[{"label":"","value":"+44 20 7946 0000"}],"emails":[{"label":"work","value":"ada@example.com"}],"birthday":"1815-12-10"}`), &o)
		c := contactsJSON(contactsBuild(o))
		for k, want := range map[string]string{"givenName": "Ada", "familyName": "Lovelace", "organization": "Analytical Engines",
			"jobTitle": "Programmer", "birthday": "1815-12-10", "note": ""} {
			if c[k] != want {
				t.Errorf("contact's %s: %q, want %q", k, c[k], want)
			}
		}
		if n, _ := c["name"].(string); !strings.Contains(n, "Ada") || !strings.Contains(n, "Lovelace") {
			t.Errorf("contact's name: %q", n)
		}
		phones, _ := c["phones"].([]map[string]string)
		if len(phones) != 1 || phones[0]["value"] != "+44 20 7946 0000" || phones[0]["label"] == "" {
			t.Errorf("contact's phones: %v (want a mobile number, its label localized)", phones)
		}
		emails, _ := c["emails"].([]map[string]string)
		if len(emails) != 1 || emails[0]["value"] != "ada@example.com" || emails[0]["label"] != "work" {
			t.Errorf("contact's emails: %v", emails)
		}
		// Only a name: it's the given name; a birthday without a year.
		c = contactsJSON(contactsBuild(contactsArgs{Name: "Grace", Birthday: "--12-09"}))
		if c["givenName"] != "Grace" || c["birthday"] != "--12-09" {
			t.Errorf("a name only and a birthday without a year: %q, %q", c["givenName"], c["birthday"])
		}
	})

	title, info := contactsAlertText(contactsArgs{GivenName: "Ada", FamilyName: "Lovelace", Organization: "Analytical Engines",
		Phones: []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		}{{"mobile", "123"}}})
	if title != "Ada Lovelace" || info != "Analytical Engines\nmobile: 123" {
		t.Errorf("the alert: %q, %q", title, info)
	}
	if b := contactsBirthday(nsUndefined, 3, 4); b != "--03-04" {
		t.Errorf("a birthday without a year: %q", b)
	}
	if b := contactsBirthday(2000, nsUndefined, 4); b != "" {
		t.Errorf("a birthday without a month: %q", b)
	}
}

func TestCalendar(t *testing.T) {
	status := 2
	var asked []bool
	oldStatus, oldRequest := calStatus, calRequest
	calStatus = func() int { return status }
	calRequest = func(writeOnly bool, done func()) {
		asked = append(asked, writeOnly)
		done()
	}
	t.Cleanup(func() { calStatus, calRequest = oldStatus, oldRequest })

	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		call := func(m string, args []byte) ([]byte, error) { return await(h, "calendar", m, args) }
		if b, err := call("request", nil); err != nil || string(b) != "\x01" {
			fatalf(t, "request, denied: %q, %v", b, err)
		}
		if _, err := call("calendars", nil); !errors.Is(err, errNotAllowed) {
			fatalf(t, "calendars, denied: %v", err)
		}
		if _, err := call("events", []byte(`{"from":0,"to":1000,"calendars":[]}`)); !errors.Is(err, errNotAllowed) {
			fatalf(t, "events, denied: %v", err)
		}
		status = 4 // write only: still no reading
		if b, _ := call("request", nil); string(b) != "\x02" {
			fatalf(t, "request, write only: %q", b)
		}
		if _, err := call("events", []byte(`{"from":0,"to":1000}`)); !errors.Is(err, errNotAllowed) {
			fatalf(t, "events, write only: %v", err)
		}
		args := []byte(`{"title":"Launch","location":"Cupertino","startMs":1718035200000,"endMs":1718038800000,"allDay":false}`)
		if b, err := answerSheet(t, h, "calendar", "add", args, 1001); err != nil || string(b) != "\x00" {
			fatalf(t, "add, cancelled: %q, %v", b, err)
		}
		// Not asked yet, Add: write access asked for, refused.
		status = 0
		asked = nil
		if _, err := answerSheet(t, h, "calendar", "add", args, 1000); !errors.Is(err, errNotAllowed) || len(asked) != 1 || !asked[0] {
			fatalf(t, "add, refused: %v (asked %v: want write only once)", err, asked)
		}
		if _, err := call("add", []byte("[")); err == nil {
			fatal(t, "add with bad JSON answered no error")
		}

		ny, _ := time.LoadLocation("America/New_York")
		start, end := time.Date(2024, 6, 10, 15, 0, 0, 0, ny), time.Date(2024, 6, 10, 16, 0, 0, 0, ny)
		title, info := calAlertText(calArgs{Title: "Launch", Location: "Cupertino"}, start, end)
		if title != "Launch" || !strings.HasSuffix(info, "\nCupertino") || len(info) <= len("\nCupertino") {
			t.Errorf("the alert: %q, %q", title, info)
		}
	})

	// All-day events: their days at UTC midnight on the wire, the end the
	// day after the last; and back.
	ny, _ := time.LoadLocation("America/New_York")
	s, e := calEventMs(time.Date(2024, 3, 10, 0, 0, 0, 0, ny), time.Date(2024, 3, 11, 23, 59, 59, 0, ny), true, ny)
	if want := time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC).UnixMilli(); s != want {
		t.Errorf("all-day start: %v, want %v", time.UnixMilli(s).UTC(), time.UnixMilli(want).UTC())
	}
	if want := time.Date(2024, 3, 12, 0, 0, 0, 0, time.UTC).UnixMilli(); e != want {
		t.Errorf("all-day end: %v, want %v", time.UnixMilli(e).UTC(), time.UnixMilli(want).UTC())
	}
	a, b := time.Date(2024, 3, 10, 9, 0, 0, 0, ny), time.Date(2024, 3, 10, 10, 0, 0, 0, ny)
	if s, e := calEventMs(a, b, false, ny); s != a.UnixMilli() || e != b.UnixMilli() {
		t.Errorf("timed event: %d, %d", s, e)
	}
	st, en := calAddTimes(s, e, true, ny, time.Now())
	if !st.Equal(time.Date(2024, 3, 10, 0, 0, 0, 0, ny)) || !en.Equal(time.Date(2024, 3, 11, 23, 59, 59, 0, ny)) {
		t.Errorf("all-day add: %v to %v", st, en)
	}
	st, en = calAddTimes(b.UnixMilli(), a.UnixMilli(), false, ny, time.Now())
	if !st.Equal(b) || !en.Equal(b.Add(time.Hour)) {
		t.Errorf("an end before the start: %v to %v, want an hour", st, en)
	}
	now := time.Date(2024, 3, 10, 9, 41, 0, 0, ny)
	st, en = calAddTimes(0, 0, false, ny, now)
	if !st.Equal(time.Date(2024, 3, 10, 10, 0, 0, 0, ny)) || !en.Equal(st.Add(time.Hour)) {
		t.Errorf("no start: %v to %v, want the next hour", st, en)
	}
}

func TestPhotoLibrary(t *testing.T) {
	status := 0
	asked := 0
	oldStatus, oldRequest := phlStatus, phlRequest
	phlStatus = func(level int) int { return status }
	phlRequest = func(level int, done func(int)) {
		asked++
		status = 2
		done(status)
	}
	t.Cleanup(func() { phlStatus, phlRequest = oldStatus, oldRequest })

	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if !phlLoad() { // PhotoKit, and the thumbnail's typed message
			fatal(t, "PhotoKit didn't load")
		}
		call := func(m string, args []byte) ([]byte, error) { return await(h, "photoLibrary", m, args) }
		for s, want := range map[int]string{0: "notAsked", 1: "denied", 2: "denied", 3: "allowed", 4: "limited"} {
			status = s
			if b, err := call("current", nil); err != nil || string(b) != want {
				t.Errorf("current, status %d: %q, %v; want %s", s, b, err, want)
			}
		}
		status = 0
		if b, err := call("request", nil); err != nil || string(b) != "denied" || asked != 1 {
			fatalf(t, "request: %q, %v (asked %d)", b, err, asked)
		}
		if b, err := call("request", nil); string(b) != "denied" || asked != 1 {
			fatalf(t, "request again: %q, %v (asked %d times, want once)", b, err, asked)
		}
		if b, err := call("list", []byte(`{"offset":0,"limit":10}`)); err != nil || string(b) != "[]" {
			fatalf(t, "list, denied: %q, %v", b, err)
		}
		for _, m := range []string{"thumbnail", "export"} {
			if _, err := call(m, []byte(`{"id":"x","size":100}`)); !errors.Is(err, errNotAllowed) {
				t.Errorf("%s, denied: %v", m, err)
			}
		}
		if _, err := call("manage", nil); err != nil {
			t.Errorf("manage: %v", err)
		}
	})
	if a, b := phlThumbName("id/L0/001", 1, 300), phlThumbName("id/L0/001", 2, 300); a == b || !strings.HasSuffix(a, "-300.jpg") {
		t.Errorf("thumbnails' names: %q, %q (an edit makes another)", a, b)
	}
}

func TestLibrary(t *testing.T) {
	dir := t.TempDir()
	status := 2
	oldDir, oldStatus := libDir, phlStatus
	libDir = func(c int) (string, error) {
		if c == 2 {
			return filepath.Join(dir, "Music"), nil
		}
		return filepath.Join(dir, "Downloads"), nil
	}
	phlStatus = func(level int) int {
		if level != phlAddOnly {
			t.Errorf("library.save asked for access level %d, want add only", level)
		}
		return status
	}
	t.Cleanup(func() { libDir, phlStatus = oldDir, oldStatus })
	src := filepath.Join(t.TempDir(), "report.txt")
	os.WriteFile(src, []byte("hello"), 0o644)

	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		save := func(o map[string]any) (string, error) {
			args, _ := json.Marshal(o)
			b, err := await(h, "library", "save", args)
			return string(b), err
		}
		p, err := save(map[string]any{"path": src, "collection": 0, "folder": "Reports"})
		if err != nil || p != filepath.Join(dir, "Downloads", "Reports", "report.txt") {
			fatalf(t, "save to Downloads: %q, %v", p, err)
		}
		p, err = save(map[string]any{"path": src, "collection": 0, "folder": "Reports"})
		if err != nil || p != filepath.Join(dir, "Downloads", "Reports", "report (1).txt") {
			fatalf(t, "save again: %q, %v (want it numbered)", p, err)
		}
		p, err = save(map[string]any{"path": src, "collection": 2, "name": "song.txt", "move": true})
		if err != nil || p != filepath.Join(dir, "Music", "song.txt") {
			fatalf(t, "move to Music: %q, %v", p, err)
		}
		if b, _ := os.ReadFile(p); string(b) != "hello" {
			t.Errorf("the moved file holds %q", b)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Errorf("moved, the source is still there: %v", err)
		}
		os.WriteFile(src, []byte("again"), 0o644)
		if _, err := save(map[string]any{"path": src, "collection": 3}); !errors.Is(err, errNotAllowed) {
			t.Errorf("a picture, Photos denied: %v", err)
		}
		if _, err := save(map[string]any{"path": filepath.Join(dir, "missing.png"), "collection": 0}); err == nil {
			t.Error("a missing file: no error")
		}
		if _, err := await(h, "library", "save", []byte("{")); err == nil {
			t.Error("bad arguments: no error")
		}
	})
}

// pick shows the system's contact picker (a popover, out of the app's
// process: no permission), and answers "" once it closes without a pick.
func TestContactsPick(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		Send(h.main.window, "orderFront:", 0)
		defer Send(h.main.window, "orderOut:", 0)
		windows := func() int { return len(ArrayItems(Send(Send(Class("NSApplication"), "sharedApplication"), "windows"))) }
		before := windows()
		answered := false
		var out []byte
		var err error
		h.module("contacts")["pick"](nil, func(b []byte, e error) { out, err, answered = b, e, true })
		if !spin(func() bool { return answered || windows() > before }) || answered {
			fatalf(t, "pick: no picker shown (answered %q, %v)", out, err)
		}
		Send(contactsPicker, "close")
		if !spin(func() bool { return answered }) {
			fatal(t, "pick: no answer once the picker closed")
		}
		if err != nil || len(out) != 0 {
			t.Errorf("pick, closed: %q, %v; want \"\"", out, err)
		}
	})
}

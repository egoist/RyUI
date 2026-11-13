//go:build linux && !android

package linux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
)

// A vCard as EDS writes one: its fields as asuka/sdk/contacts' JSON.
func TestContactOfVCard(t *testing.T) {
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:pas-id-1\r\nFN:Ada Lovelace\r\nN:Lovelace;Ada;;;\r\n" +
		"ORG:Analytical Engines\\, Ltd;Research\r\nTITLE:Countess\r\nitem1.TEL;TYPE=CELL;X-EVOLUTION-UI-SLOT=1:+44 1\r\n" +
		"TEL;TYPE=WORK,VOICE:+44 2\r\nEMAIL;TYPE=HOME:ada@example.com\r\nBDAY:18151210\r\nNOTE:First line\\nsecond\\; a long\r\n  folded line\r\nEND:VCARD\r\n"
	c := contactOfVCard(card)
	b, _ := json.Marshal(c)
	want := `{"birthday":"1815-12-10","emails":[{"label":"home","value":"ada@example.com"}],"familyName":"Lovelace","givenName":"Ada",` +
		`"id":"pas-id-1","jobTitle":"Countess","name":"Ada Lovelace","note":"First line\nsecond; a long folded line","organization":"Analytical Engines, Ltd",` +
		`"phones":[{"label":"mobile","value":"+44 1"},{"label":"work","value":"+44 2"}]}`
	if string(b) != want {
		t.Fatalf("%s\nwant %s", b, want)
	}
	for in, out := range map[string]string{"--1210": "--12-10", "--12-10": "--12-10", "1815-12-10": "1815-12-10", "1815-12-10T00:00:00Z": "1815-12-10", "x": ""} {
		if got := vBirthday(in); got != out {
			t.Errorf("vBirthday(%q) = %q, want %q", in, got, out)
		}
	}
	e := eventOfInstance("cal", EDSInstance{ICal: "BEGIN:VEVENT\r\nUID:e1\r\nSUMMARY:Tea\\, then talk\r\nLOCATION:Room 1\r\nURL:https://example.com/a;b\r\n" +
		"BEGIN:VALARM\r\nDESCRIPTION:Reminder\r\nEND:VALARM\r\nEND:VEVENT\r\n", StartMs: 1, EndMs: 2})
	if e["id"] != "e1" || e["title"] != "Tea, then talk" || e["location"] != "Room 1" || e["url"] != "https://example.com/a;b" || e["notes"] != "" {
		t.Fatalf("event %v", e)
	}
}

// Evolution Data Server's own (the VM's, a session of the test's): an
// address book and a calendar of the test's beside the built-in ones,
// read, picked from, added to.
func TestEDS(t *testing.T) {
	if LoadEBook() != nil || LoadECal() != nil {
		t.Skip("no evolution-data-server")
	}
	h, _ := mount(t, emptyRoot)
	// EDS's services start on this session's bus with a home of the
	// test's, where its sources are (unless something started them).
	home := t.TempDir()
	cfg := filepath.Join(home, "config")
	src := filepath.Join(cfg, "evolution", "sources")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "asuka-test-book.source"), []byte("[Data Source]\nDisplayName=Asuka Test Book\nEnabled=true\nParent=local-stub\n\n[Address Book]\nBackendName=local\n"), 0o644)
	os.WriteFile(filepath.Join(src, "asuka-test-cal.source"), []byte("[Data Source]\nDisplayName=Asuka Test Calendar\nEnabled=true\nParent=local-stub\n\n[Calendar]\nBackendName=local\nColor=#62A0EA\nSelected=true\n"), 0o644)
	running := false
	onMain(func() {
		conn, _ := Bus(BusSession)
		done := false
		appRunning(conn, "org.gnome.evolution.dataserver.Sources5", func(r bool) { running, done = r, true })
		for !done {
			MainContextIteration(0, true)
		}
		DBusCall(conn, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "UpdateActivationEnvironment",
			VTuple(VArray("{ss}", VEntry(VStr("HOME"), VStr(home)), VEntry(VStr("XDG_CONFIG_HOME"), VStr(cfg)),
				VEntry(VStr("XDG_DATA_HOME"), VStr(filepath.Join(home, "data"))), VEntry(VStr("XDG_CACHE_HOME"), VStr(filepath.Join(home, "cache"))))),
			-1, func(Ptr, error) { done = false })
		for done {
			MainContextIteration(0, true)
		}
	})
	if running {
		t.Skip("evolution-data-server already runs in this session")
	}
	confirmed := 0
	oldConfirm := edsConfirm
	edsConfirm = func(h *Host, title, body, add string, done func(bool)) {
		confirmed++
		t.Logf("asked: %q %q [%s]", title, body, add)
		done(true)
	}
	defer func() { edsConfirm = oldConfirm }()
	call := func(mod, name string, args []byte) []byte {
		t.Helper()
		b, err := callModule(t, h, mod, name, args)
		if err != nil {
			t.Fatalf("%s.%s: %v", mod, name, err)
		}
		return b
	}

	// Contacts.
	if b := call("contacts", "request", nil); string(b) != "\x03" {
		t.Fatalf("contacts.request: %q", b)
	}
	if b := call("contacts", "add", []byte(`{"givenName":"Ada","familyName":"Lovelace","organization":"Engines","phones":[{"label":"mobile","value":"+44 1"}],"emails":[{"label":"home","value":"ada@example.com"}],"birthday":"1815-12-10"}`)); string(b) != "\x01" || confirmed != 1 {
		t.Fatalf("contacts.add: %q (asked %d)", b, confirmed)
	}
	if _, err := EDSAddContact("asuka-test-book", vCard(contactArgs{GivenName: "Grace", FamilyName: "Hopper", Emails: []struct{ Label, Value string }{{"work", "grace@example.com"}}})); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	json.Unmarshal(call("contacts", "list", []byte(`{}`)), &list)
	if len(list) != 2 || list[0]["name"] != "Ada Lovelace" || list[1]["name"] != "Grace Hopper" {
		t.Fatalf("list: %v", list)
	}
	ada := list[0]
	if ada["id"] == "" || ada["organization"] != "Engines" || ada["birthday"] != "1815-12-10" ||
		fmt.Sprint(ada["phones"]) != "[map[label:mobile value:+44 1]]" || fmt.Sprint(ada["emails"]) != "[map[label:home value:ada@example.com]]" {
		t.Fatalf("Ada: %v", ada)
	}
	json.Unmarshal(call("contacts", "list", []byte(`{"name":"hop"}`)), &list)
	if len(list) != 1 || list[0]["name"] != "Grace Hopper" {
		t.Fatalf("list hop: %v", list)
	}
	json.Unmarshal(call("contacts", "list", []byte(`{"limit":1}`)), &list)
	if len(list) != 1 {
		t.Fatalf("limit 1: %v", list)
	}

	// The picker: a search hides the others; a row answers its contact.
	picked := callAsync(h, "contacts", "pick", nil)
	settle(t, h, func() bool { return contactPicking != nil && len(contactPicking.rows) == 2 })
	onMain(func() { EditableSetText(contactPicking.search, "grace") })
	settle(t, h, func() bool {
		return !WidgetGetVisible(contactPicking.rows[0]) && WidgetGetVisible(contactPicking.rows[1])
	})
	onMain(func() { WidgetActivate(contactPicking.rows[1]) })
	b, err := awaitResult(t, "picked", picked)
	var c map[string]any
	if err != nil || json.Unmarshal(b, &c) != nil || c["name"] != "Grace Hopper" {
		t.Fatalf("picked %s %v", b, err)
	}
	// Cancelled: nothing.
	picked = callAsync(h, "contacts", "pick", nil)
	settle(t, h, func() bool { return contactPicking != nil })
	onMain(func() { contactPicking.finish(-1) })
	if b, err := awaitResult(t, "cancelled", picked); err != nil || len(b) != 0 {
		t.Fatalf("cancelled: %q %v", b, err)
	}

	// Calendars.
	if b := call("calendar", "request", nil); string(b) != "\x03" {
		t.Fatalf("calendar.request: %q", b)
	}
	var cals []map[string]any
	json.Unmarshal(call("calendar", "calendars", nil), &cals)
	var test map[string]any
	for _, c := range cals {
		if c["id"] == "asuka-test-cal" {
			test = c
		}
	}
	if len(cals) < 2 || test == nil || test["title"] != "Asuka Test Calendar" || test["color"] != "#62a0ea" || test["writable"] != true {
		t.Fatalf("calendars: %v", cals)
	}
	start := time.Date(2026, 12, 24, 18, 30, 0, 0, time.UTC)
	add := func(o string) {
		t.Helper()
		if b := call("calendar", "add", []byte(o)); string(b) != "\x01" {
			t.Fatalf("calendar.add %s: %q", o, b)
		}
	}
	add(fmt.Sprintf(`{"title":"Dinner","location":"Home","notes":"Bring wine","url":"https://example.com/d","calendarId":"asuka-test-cal","startMs":%d,"endMs":%d}`,
		start.UnixMilli(), start.Add(2*time.Hour).UnixMilli()))
	day := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	add(fmt.Sprintf(`{"title":"Holiday","calendarId":"asuka-test-cal","allDay":true,"startMs":%d,"endMs":%d}`, day.UnixMilli(), day.AddDate(0, 0, 2).UnixMilli()))
	add(fmt.Sprintf(`{"title":"Default's","startMs":%d,"endMs":%d}`, start.UnixMilli(), start.Add(time.Hour).UnixMilli()))
	// A repeating one: each occurrence.
	if _, err := EDSAddEvent("asuka-test-cal", "BEGIN:VEVENT\r\nUID:standup@asuka\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261221T090000Z\r\nDTEND:20261221T091500Z\r\n"+
		"RRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:Standup\r\nEND:VEVENT\r\n"); err != nil {
		t.Fatal(err)
	}
	var evs []map[string]any
	from, to := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	json.Unmarshal(call("calendar", "events", []byte(fmt.Sprintf(`{"from":%d,"to":%d,"calendars":["asuka-test-cal"]}`, from.UnixMilli(), to.UnixMilli()))), &evs)
	var got []string
	for _, e := range evs {
		got = append(got, fmt.Sprintf("%s %s %s %v", e["title"], time.UnixMilli(int64(e["startMs"].(float64))).UTC().Format("01-02T15:04"),
			time.UnixMilli(int64(e["endMs"].(float64))).UTC().Format("01-02T15:04"), e["allDay"]))
	}
	want := []string{"Standup 12-21T09:00 12-21T09:15 false", "Standup 12-22T09:00 12-22T09:15 false", "Standup 12-23T09:00 12-23T09:15 false",
		"Dinner 12-24T18:30 12-24T20:30 false", "Holiday 12-25T00:00 12-27T00:00 true"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, e := range evs {
		if e["title"] == "Dinner" && (e["location"] != "Home" || e["notes"] != "Bring wine" || e["url"] != "https://example.com/d" || e["calendarId"] != "asuka-test-cal" || e["id"] == "") {
			t.Fatalf("Dinner: %v", e)
		}
	}
	// All calendars: the default's too.
	json.Unmarshal(call("calendar", "events", []byte(fmt.Sprintf(`{"from":%d,"to":%d}`, from.UnixMilli(), to.UnixMilli()))), &evs)
	if len(evs) != 6 {
		t.Fatalf("all calendars: %d events", len(evs))
	}
}

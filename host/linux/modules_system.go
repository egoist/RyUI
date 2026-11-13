//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/native"
)

// More of the system's modules on Linux, as macOS answers them where
// Linux has nothing of its own: asuka/sdk/background (a desktop app keeps
// running out of sight: a task holds off the system's suspending),
// asuka/sdk/periodic (tasks on a timer while the app runs, as macOS's
// scheduler runs them), asuka/sdk/splash (no launch screen to hold),
// asuka/sdk/screen (no orientation or bars; capture can't be prevented;
// screenshots seen as they're saved), asuka/sdk/widget (none), and the
// personal data Linux keeps no store of an app can read: Sign in with
// Apple, the photo library, and contacts and calendars on a system
// without Evolution Data Server (modules_eds.go reads and writes its
// own; without it, adding one opens a vCard or iCalendar file in the
// user's app for it).

// builtinViews are the host's own native views (ui.Native's kinds the
// repo's packages use: asuka.camera, asuka.map), made as an app's are.
var builtinViews = map[string]func(native.Context) native.View{
	// Sign in with Apple's button: none (as on a Mac without
	// AuthenticationServices): the node shows nothing.
	"asuka.appleSignIn": func(native.Context) native.View { return nil },
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["background"] = h.backgroundModule()
		m["periodic"] = h.periodicModule()
		m["splash"] = map[string]method{"hold": answerNil, "hide": answerNil}
		m["screen"] = h.screenModule()
		m["widget"] = map[string]method{
			"set": func(_ []byte, reply func([]byte, error)) {
				reply(nil, errors.New("not available: widgets are iOS's and Android's"))
			},
		}
		// asuka/sdk/liveactivity: an iPhone's and an Android phone's; none here.
		noActivities := func(_ []byte, reply func([]byte, error)) {
			reply(nil, errors.New("not available: live activities are iOS's and Android's"))
		}
		m["liveActivity"] = map[string]method{
			"enabled": func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
			"start":   noActivities,
			"update":  answerNil,
			"end":     answerNil,
			"running": func(_ []byte, reply func([]byte, error)) { reply([]byte("[]"), nil) },
			"next":    noActivities,
			"stop":    answerNil,
		}
		notAvailable := func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not available")) }
		m["appleAuth"] = map[string]method{
			"available": func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
			"signIn":    notAvailable,
			"state":     notAvailable,
		}
		m["photoLibrary"] = h.photoLibraryModule()
		m["contacts"] = h.edsContacts(h.contactsModule())
		m["calendar"] = h.edsCalendar(h.calendarModule())
		m["transcribe"] = map[string]method{
			// No speech recognizer is part of a Linux desktop.
			"check": notAvailable,
			"file":  notAvailable,
		}
	})
}

// answerNil answers at once, with nothing.
func answerNil(_ []byte, reply func([]byte, error)) { reply(nil, nil) }

// Background.

// bgTasks are asuka/sdk/background's tasks: a Linux app keeps running out of
// sight, so a task only keeps the system from suspending (the session's
// inhibitor, through the portal in a sandbox) until the last one ends.
// There's no notification to show: "update" does nothing, and "next"
// (actions tapped) waits until the last task ends, then fails "ended".
var bgTasks struct {
	ids     map[uint32]bool
	cookie  uint32
	next    []func([]byte, error)
	inhibit func(on bool) // a seam for the tests: the inhibitor
}

func (h *Host) backgroundModule() map[string]method {
	id := func(args []byte) uint32 {
		var o struct {
			ID uint32 `json:"id"`
		}
		json.Unmarshal(args, &o)
		return o.ID
	}
	if bgTasks.inhibit == nil {
		bgTasks.inhibit = func(on bool) {
			if on && bgTasks.cookie == 0 {
				var win Ptr
				if h.main != nil {
					win = h.main.window
				}
				bgTasks.cookie = ApplicationInhibit(h.app, win, 4, "Finishing a task") // GTK_APPLICATION_INHIBIT_SUSPEND
			} else if !on && bgTasks.cookie != 0 {
				ApplicationUninhibit(h.app, bgTasks.cookie)
				bgTasks.cookie = 0
			}
		}
	}
	return map[string]method{
		"start": func(args []byte, reply func([]byte, error)) {
			if bgTasks.ids == nil {
				bgTasks.ids = map[uint32]bool{}
			}
			bgTasks.ids[id(args)] = true
			bgTasks.inhibit(true)
			reply(nil, nil)
		},
		"update": answerNil,
		"end": func(args []byte, reply func([]byte, error)) {
			endBackgroundTask(id(args))
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if len(bgTasks.ids) == 0 {
				reply(nil, errors.New("ended"))
				return
			}
			bgTasks.next = append(bgTasks.next, reply)
		},
	}
}

func endBackgroundTask(id uint32) {
	if !bgTasks.ids[id] {
		return
	}
	delete(bgTasks.ids, id)
	if len(bgTasks.ids) > 0 {
		return
	}
	bgTasks.inhibit(false)
	ws := bgTasks.next
	bgTasks.next = nil
	for _, w := range ws {
		w(nil, errors.New("ended"))
	}
}

// Periodic.

// asuka/sdk/periodic on Linux: a task runs about every interval while the
// app runs (in the background too: a Linux app keeps running out of
// sight), as macOS's NSBackgroundActivityScheduler runs it; nothing
// launches the app for it. "schedule" (string name, u32 seconds, u8
// flags: 1 network, 2 charging) and "cancel" (the name) keep the app's
// tasks in its config folder; when one is due, "next" hands Go the task
// (string name, u32 seconds it has), which it reports with "done"
// (string name, u8 ok). A task that wants power waits while the computer
// runs on its battery, one that wants the network while it has none
// (tried again a minute later). Due times are wall-clock times, checked
// at least every five minutes, so a suspend doesn't push them back.

type periodicTask struct {
	Every    float64 `json:"every"`
	Network  bool    `json:"network"`
	Charging bool    `json:"charging"`
	Last     float64 `json:"last"` // unix seconds it last ran (or was scheduled)
}

var periodicState struct {
	loaded  bool
	timer   uint32
	due     []string
	waiting []func([]byte, error)
	running map[string]bool
	retry   map[string]time.Time // deferred: tried again then
	// Seams for the tests.
	now      func() time.Time
	charging func() bool
	online   func() bool
	file     string
}

func (h *Host) periodicFile() string {
	if f := periodicState.file; f != "" {
		return f
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), "asuka-config")
	}
	return filepath.Join(dir, h.appID, "asuka-periodic.json")
}

func (h *Host) periodicTasks() map[string]periodicTask {
	ts := map[string]periodicTask{}
	if b, err := os.ReadFile(h.periodicFile()); err == nil {
		json.Unmarshal(b, &ts)
	}
	return ts
}

func (h *Host) setPeriodicTasks(ts map[string]periodicTask) {
	b, _ := json.Marshal(ts)
	path := h.periodicFile()
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, b, 0o644)
}

func (h *Host) periodicModule() map[string]method {
	s := &periodicState
	if s.now == nil {
		s.now = time.Now
		s.charging = onACPower
		s.online = func() bool {
			if m := Sys.NetworkMonitorGetDefault(); m != 0 {
				return Sys.NetworkMonitorAvailable(m)
			}
			return true
		}
	}
	if s.running == nil {
		s.running, s.retry = map[string]bool{}, map[string]time.Time{}
	}
	return map[string]method{
		"schedule": func(args []byte, reply func([]byte, error)) {
			name, rest := readString(args)
			if name == "" || len(rest) < 5 {
				reply(nil, errors.New("bad args"))
				return
			}
			secs := binary.LittleEndian.Uint32(rest)
			flags := rest[4]
			ts := h.periodicTasks()
			last := ts[name].Last
			if last == 0 {
				last = float64(s.now().Unix())
			}
			ts[name] = periodicTask{Every: float64(secs), Network: flags&1 != 0, Charging: flags&2 != 0, Last: last}
			h.setPeriodicTasks(ts)
			s.loaded = true
			h.periodicTick()
			reply(nil, nil)
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			name := string(args)
			ts := h.periodicTasks()
			delete(ts, name)
			h.setPeriodicTasks(ts)
			delete(s.retry, name)
			for i, n := range s.due {
				if n == name {
					s.due = append(s.due[:i], s.due[i+1:]...)
					break
				}
			}
			h.periodicTick()
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if !s.loaded {
				s.loaded = true
				h.periodicTick()
			}
			if len(s.due) > 0 {
				name := s.due[0]
				s.due = s.due[1:]
				reply(h.periodicRun(name), nil)
				return
			}
			s.waiting = append(s.waiting, reply)
		},
		"done": func(args []byte, reply func([]byte, error)) {
			name, _ := readString(args)
			if s.running[name] {
				delete(s.running, name)
				// Ran (or failed): next after an interval, as a scheduler's
				// finished activity.
				ts := h.periodicTasks()
				if t, kept := ts[name]; kept {
					t.Last = float64(s.now().Unix())
					ts[name] = t
					h.setPeriodicTasks(ts)
				}
				h.periodicTick()
			}
			reply(nil, nil)
		},
	}
}

// periodicRun is "next"'s answer: the name and the seconds it has.
func (h *Host) periodicRun(name string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	b = append(b, name...)
	budget := uint32(25)
	if h.periodicTasks()[name].Charging {
		budget = 240
	}
	return binary.LittleEndian.AppendUint32(b, budget)
}

// periodicTick runs the tasks that are due and sets the timer for the
// next.
func (h *Host) periodicTick() {
	s := &periodicState
	if s.timer != 0 {
		SourceRemove(s.timer)
		s.timer = 0
	}
	now := s.now()
	ts := h.periodicTasks()
	next := 5 * time.Minute
	for name, t := range ts {
		if s.running[name] || inList(s.due, name) {
			continue
		}
		at := time.Unix(int64(t.Last), 0).Add(time.Duration(max(t.Every, 60)) * time.Second)
		if r, ok := s.retry[name]; ok && r.After(at) {
			at = r
		}
		if wait := at.Sub(now); wait > 0 {
			next = min(next, wait)
			continue
		}
		if t.Charging && !s.charging() || t.Network && !s.online() {
			s.retry[name] = now.Add(time.Minute)
			next = min(next, time.Minute)
			continue
		}
		delete(s.retry, name)
		s.running[name] = true
		if len(s.waiting) > 0 {
			w := s.waiting[0]
			s.waiting = s.waiting[1:]
			w(h.periodicRun(name), nil)
		} else {
			s.due = append(s.due, name)
		}
	}
	if len(ts) == 0 {
		return
	}
	s.timer = TimeoutAdd(uint32(max(next, time.Second)/time.Millisecond), func() bool {
		s.timer = 0
		h.periodicTick()
		return false
	})
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// onACPower reports whether the computer runs on mains power: a power
// supply of its says so, or it has no battery (a desktop).
func onACPower() bool {
	dirs, _ := filepath.Glob("/sys/class/power_supply/*")
	battery := false
	for _, d := range dirs {
		typ, _ := os.ReadFile(filepath.Join(d, "type"))
		switch strings.TrimSpace(string(typ)) {
		case "Mains", "USB":
			if on, _ := os.ReadFile(filepath.Join(d, "online")); strings.TrimSpace(string(on)) == "1" {
				return true
			}
		case "Battery":
			if scope, _ := os.ReadFile(filepath.Join(d, "scope")); strings.TrimSpace(string(scope)) != "Device" {
				battery = true // not a mouse's
			}
		}
	}
	return !battery
}

// Screen.

// screenState is asuka/sdk/screen's on Linux. Locking the orientation and the
// bars' style are a phone's: a desktop's display doesn't turn with the
// window, and it has no status or navigation bar. Nothing keeps a
// window out of the desktop's own screenshots and recordings (Wayland
// already keeps other apps from capturing it): PreventCapture does
// nothing. Screenshots are seen as the desktop saves them: a new image
// in the Pictures folder's Screenshots (GNOME's, KDE's), or one named a
// screenshot in Pictures.
var screenState struct {
	monitors    []Ptr
	handlers    []uint64
	screenshots []func([]byte, error)
	dirs        []string // a seam for the tests: the folders watched
}

func (h *Host) screenModule() map[string]method {
	s := &screenState
	return map[string]method{
		"lock":           answerNil,
		"bars":           answerNil,
		"preventCapture": answerNil,
		"screenshot": func(_ []byte, reply func([]byte, error)) {
			if err := watchScreenshots(); err != nil {
				reply(nil, err)
				return
			}
			s.screenshots = append(s.screenshots, reply)
		},
		"stopScreenshots": func(_ []byte, reply func([]byte, error)) {
			ws := s.screenshots
			s.screenshots = nil
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			for i, m := range s.monitors {
				Disconnect(m, s.handlers[i])
				Dev.FileMonitorCancel(m)
				ObjectUnref(m)
			}
			s.monitors, s.handlers = nil, nil
			reply(nil, nil)
		},
	}
}

// screenshotDirs are the folders screenshots are saved in.
func screenshotDirs() []string {
	if d := screenState.dirs; d != nil {
		return d
	}
	pics := GoString(Sys.GetUserSpecialDir(4)) // G_USER_DIRECTORY_PICTURES
	if pics == "" {
		home, _ := os.UserHomeDir()
		pics = filepath.Join(home, "Pictures")
	}
	return []string{filepath.Join(pics, "Screenshots"), pics}
}

func watchScreenshots() error {
	s := &screenState
	if s.monitors != nil {
		return nil
	}
	for i, dir := range screenshotDirs() {
		f := Sys.FileNewForPath(dir)
		var gerr Ptr
		m := Dev.FileMonitorDirectory(f, 0, 0, &gerr)
		ObjectUnref(f)
		if m == 0 {
			if gerr != 0 {
				ErrorFree(gerr)
			}
			continue
		}
		inScreenshots := i == 0
		id := Connect(m, "changed", func(file, other, event uintptr) {
			const created, movedIn = 3, 9 // G_FILE_MONITOR_EVENT_CREATED, _MOVED_IN
			if event != created && event != movedIn {
				return
			}
			if !isScreenshot(filepath.Base(GoStringFree(Sys.FileGetPath(file))), inScreenshots) {
				return
			}
			ws := s.screenshots
			s.screenshots = nil
			for _, w := range ws {
				w(nil, nil)
			}
		})
		s.monitors = append(s.monitors, m)
		s.handlers = append(s.handlers, id)
	}
	if s.monitors == nil {
		return errors.New("not available")
	}
	return nil
}

// isScreenshot reports whether a new file is a screenshot: an image in
// the Screenshots folder, or one named so in Pictures.
func isScreenshot(name string, inScreenshots bool) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
	default:
		return false
	}
	return inScreenshots || strings.Contains(strings.ToLower(name), "screenshot")
}

// The photo library.

// photoLibraryModule is asuka/sdk/photos' library's: a Linux desktop has no
// photo library an app can read (photos are files: asuka/sdk/photos' Pick
// and asuka/sdk/device's file picker reach them). Access is denied, and
// listing fails saying so.
func (h *Host) photoLibraryModule() map[string]method {
	denied := func(_ []byte, reply func([]byte, error)) { reply([]byte("denied"), nil) }
	none := func(_ []byte, reply func([]byte, error)) {
		reply(nil, errors.New("not available: Linux has no photo library (pick photos with asuka/sdk/photos' Pick)"))
	}
	return map[string]method{
		"current":   denied,
		"request":   denied,
		"manage":    answerNil,
		"list":      none,
		"thumbnail": none,
		"export":    none,
	}
}

// Contacts and calendars.

// contactsModule is asuka/sdk/contacts' without Evolution Data Server
// (modules_eds.go has its address books): no address book, no contact
// picker: access is denied (the device doesn't allow it), listing fails
// "not allowed", picking "not available". Adding a contact opens it, as a vCard, in the user's
// contacts app (GNOME Contacts imports it), which says nothing back.
func (h *Host) contactsModule() map[string]method {
	return map[string]method{
		"request": func(_ []byte, reply func([]byte, error)) { reply([]byte{1}, nil) },
		"list":    func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not allowed")) },
		"pick": func(_ []byte, reply func([]byte, error)) {
			reply(nil, errors.New("not available: Linux has no contact picker"))
		},
		"add": func(args []byte, reply func([]byte, error)) {
			var c contactArgs
			if json.Unmarshal(args, &c) != nil {
				reply(nil, errors.New("bad args"))
				return
			}
			h.openDocument(".vcf", vCard(c), reply)
		},
	}
}

// contactArgs is a contact as asuka/sdk/contacts adds it.
type contactArgs struct {
	Name, GivenName, FamilyName, Organization, JobTitle, Birthday, Note string
	Phones, Emails                                                      []struct{ Label, Value string }
}

// vCard is a contact as a vCard (3.0).
func vCard(c contactArgs) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	name := c.Name
	if name == "" {
		name = strings.TrimSpace(c.GivenName + " " + c.FamilyName)
	}
	given, family := c.GivenName, c.FamilyName
	if given == "" && family == "" {
		given = c.Name
	}
	fmt.Fprintf(&b, "N:%s;%s;;;\r\nFN:%s\r\n", vEscape(family), vEscape(given), vEscape(name))
	if c.Organization != "" {
		fmt.Fprintf(&b, "ORG:%s\r\n", vEscape(c.Organization))
	}
	if c.JobTitle != "" {
		fmt.Fprintf(&b, "TITLE:%s\r\n", vEscape(c.JobTitle))
	}
	for _, p := range c.Phones {
		fmt.Fprintf(&b, "TEL%s:%s\r\n", vType(p.Label), vEscape(p.Value))
	}
	for _, e := range c.Emails {
		fmt.Fprintf(&b, "EMAIL%s:%s\r\n", vType(e.Label), vEscape(e.Value))
	}
	if c.Birthday != "" {
		fmt.Fprintf(&b, "BDAY:%s\r\n", c.Birthday)
	}
	if c.Note != "" {
		fmt.Fprintf(&b, "NOTE:%s\r\n", vEscape(c.Note))
	}
	b.WriteString("END:VCARD\r\n")
	return b.String()
}

// calendarModule is asuka/sdk/calendar's without Evolution Data Server
// (modules_eds.go has its calendars): an app may add events but not
// read them, as iOS's write-only access: "request" answers write
// only, reading fails "not allowed". Adding an event opens it, as an
// iCalendar file, in the user's calendar app (GNOME Calendar asks where
// to import it), which says nothing back: not saved, as Android's
// calendar apps.
func (h *Host) calendarModule() map[string]method {
	notAllowed := func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not allowed")) }
	return map[string]method{
		"request":   func(_ []byte, reply func([]byte, error)) { reply([]byte{2}, nil) },
		"calendars": notAllowed,
		"events":    notAllowed,
		"add": func(args []byte, reply func([]byte, error)) {
			var e struct {
				Title, Location, Notes, URL string
				StartMs                     int64 `json:"startMs"`
				EndMs                       int64 `json:"endMs"`
				AllDay                      bool  `json:"allDay"`
			}
			if json.Unmarshal(args, &e) != nil {
				reply(nil, errors.New("bad args"))
				return
			}
			h.openDocument(".ics", iCalendar(e.Title, e.Location, e.Notes, e.URL, e.StartMs, e.EndMs, e.AllDay, time.Now()), reply)
		},
	}
}

// iCalendar is an event as an iCalendar file.
func iCalendar(title, location, notes, link string, startMs, endMs int64, allDay bool, now time.Time) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Asuka//EN\r\n" +
		vEvent(randomName()+"@asuka", title, location, notes, link, startMs, endMs, allDay, now) + "END:VCALENDAR\r\n"
}

// vEvent is an event as an iCalendar VEVENT: times in UTC, an all-day
// one's days (UTC on the wire) dates, its end the day after the last.
func vEvent(uid, title, location, notes, link string, startMs, endMs int64, allDay bool, now time.Time) string {
	var b strings.Builder
	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&b, "UID:%s\r\nDTSTAMP:%s\r\n", uid, now.UTC().Format("20060102T150405Z"))
	start, end := time.UnixMilli(startMs).UTC(), time.UnixMilli(endMs).UTC()
	if allDay {
		fmt.Fprintf(&b, "DTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\n", start.Format("20060102"), end.Format("20060102"))
	} else {
		fmt.Fprintf(&b, "DTSTART:%s\r\nDTEND:%s\r\n", start.Format("20060102T150405Z"), end.Format("20060102T150405Z"))
	}
	fmt.Fprintf(&b, "SUMMARY:%s\r\n", vEscape(title))
	if location != "" {
		fmt.Fprintf(&b, "LOCATION:%s\r\n", vEscape(location))
	}
	if notes != "" {
		fmt.Fprintf(&b, "DESCRIPTION:%s\r\n", vEscape(notes))
	}
	if link != "" {
		fmt.Fprintf(&b, "URL:%s\r\n", link)
	}
	b.WriteString("END:VEVENT\r\n")
	return b.String()
}

// vEscape escapes a vCard's or iCalendar's text value.
func vEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
}

// vType is a vCard property's TYPE for a label ("mobile": CELL).
func vType(label string) string {
	switch strings.ToLower(label) {
	case "mobile", "cell", "iphone":
		return ";TYPE=CELL"
	case "home":
		return ";TYPE=HOME"
	case "work":
		return ";TYPE=WORK"
	}
	return ""
}

// openDocument writes text to a file in the caches and opens it in the
// user's app for its type; the reply is u8 0 (the app says nothing).
func (h *Host) openDocument(ext, text string, reply func([]byte, error)) {
	dir := filepath.Join(h.modCacheDir(), "asuka-documents")
	path := filepath.Join(dir, randomName()+ext)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		reply(nil, err)
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		reply(nil, err)
		return
	}
	openDocumentHook(h, path, func(err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		reply([]byte{0}, nil)
	})
}

// openDocumentHook opens a file in its app (a seam for the tests).
var openDocumentHook = func(h *Host, path string, done func(error)) { h.launchFile(path, done) }

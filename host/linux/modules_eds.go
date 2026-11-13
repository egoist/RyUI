//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/l10n"
)

// asuka/contacts and asuka/calendar with Evolution Data Server (EDS), the
// store GNOME's apps share (GNOME Contacts and Calendar, Evolution, the
// online accounts' address books and calendars): the module's methods
// as macOS's (host/darwin/contacts.go, calendar.go).
//
// EDS asks nothing (a desktop app reads what the user's session does),
// so access is full when it has an address book (contacts) or a
// calendar (calendars), else it's as without EDS (modules_system.go:
// contacts denied, calendars write only, adding opens a file). "list"
// reads every enabled address book's contacts (vCards, read in Go),
// sorted by name; "pick" is a dialog over the window listing them with
// a search field (as AppKit's picker); "add" asks with an alert (the
// contact's name, phones and emails) and saves it in the default address
// book. "calendars" are the enabled calendars (their color, account,
// whether they're writable), "events" each occurrence in the range
// (EDS expands repeating ones), "add" asks with an alert (the title,
// the dates, the place) and saves the event in its calendar, or the
// default. asuka/calendar has no edit or remove: none here either.

// edsConfirm asks with an alert over the window: its Add button (the
// default) or Cancel (Escape) calls done with whether it was Add (tests
// replace it).
var edsConfirm = func(h *Host, title, body, add string, done func(bool)) {
	d := AdwAlertDialogNew(title, body)
	AdwAlertDialogAddResponse(d, "cancel", l10n.Cancel(h.locale))
	AdwAlertDialogAddResponse(d, "add", add)
	AdwAlertDialogSetResponseAppearance(d, "add", ResponseSuggested)
	AdwAlertDialogSetDefaultResponse(d, "add")
	AdwAlertDialogSetCloseResponse(d, "cancel")
	Connect(d, "response", func(resp uintptr) { done(GoString(resp) == "add") })
	AdwDialogPresent(d, h.dialogParent())
}

// offMain runs f off the main thread and replies with what it answers.
func (h *Host) offMain(f func() ([]byte, error), reply func([]byte, error)) {
	go func() {
		b, err := f()
		h.Post(func() { reply(b, err) })
	}()
}

// edsHas reports whether EDS has a source of the extension (off the main
// thread; tests replace it).
var edsHas = func(ext string) bool {
	load := LoadEBook
	if ext == EDSCalendar {
		load = LoadECal
	}
	if load() != nil {
		return false
	}
	srcs, err := EDSSources(ext)
	return err == nil && len(srcs) > 0
}

// edsOr is a method that's EDS's when it has a source of ext, else
// fallback's.
func (h *Host) edsOr(ext string, eds, fallback method) method {
	return func(args []byte, reply func([]byte, error)) {
		go func() {
			ok := edsHas(ext)
			h.Post(func() {
				if ok {
					eds(args, reply)
				} else if fallback != nil {
					fallback(args, reply)
				} else {
					reply(nil, errors.New("not available"))
				}
			})
		}()
	}
}

// Contacts.

func (h *Host) edsContacts(fallback map[string]method) map[string]method {
	return map[string]method{
		"request": h.edsOr(EDSAddressBook, func(_ []byte, reply func([]byte, error)) { reply([]byte{3}, nil) }, fallback["request"]),
		"list": h.edsOr(EDSAddressBook, func(args []byte, reply func([]byte, error)) {
			var o struct {
				Name  string `json:"name"`
				Limit int    `json:"limit"`
			}
			json.Unmarshal(args, &o)
			h.offMain(func() ([]byte, error) {
				cs, err := edsAllContacts(o.Name)
				if err != nil {
					return nil, err
				}
				if o.Limit > 0 && len(cs) > o.Limit {
					cs = cs[:o.Limit]
				}
				return json.Marshal(cs)
			}, reply)
		}, fallback["list"]),
		"pick": h.edsOr(EDSAddressBook, func(_ []byte, reply func([]byte, error)) {
			go func() {
				cs, err := edsAllContacts("")
				h.Post(func() {
					if err != nil {
						reply(nil, err)
						return
					}
					h.pickContact(cs, reply)
				})
			}()
		}, fallback["pick"]),
		"add": h.edsOr(EDSAddressBook, func(args []byte, reply func([]byte, error)) {
			var c contactArgs
			if json.Unmarshal(args, &c) != nil {
				reply(nil, errors.New("contacts.add: bad arguments"))
				return
			}
			title, body := contactAlertText(c)
			edsConfirm(h, title, body, l10n.AddToContacts(h.locale), func(ok bool) {
				if !ok {
					reply([]byte{0}, nil)
					return
				}
				h.offMain(func() ([]byte, error) {
					if _, err := EDSAddContact("", vCard(c)); err != nil {
						return nil, err
					}
					return []byte{1}, nil
				}, reply)
			})
		}, fallback["add"]),
	}
}

// edsAllContacts are the contacts of every enabled address book whose
// name has name (all: ""), sorted by name. Off the main thread.
func edsAllContacts(name string) ([]map[string]any, error) {
	srcs, err := EDSSources(EDSAddressBook)
	if err != nil {
		return nil, err
	}
	sexp := `(contains "x-evolution-any-field" "")`
	if name != "" {
		q := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
		sexp = `(or (contains "full_name" ` + q + `) (contains "given_name" ` + q + `) (contains "family_name" ` + q + `) (contains "nickname" ` + q + `))`
	}
	out := []map[string]any{}
	var firstErr error
	for _, s := range srcs {
		cards, err := EDSContacts(s.UID, sexp)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // an account offline: the others
		}
		for _, c := range cards {
			out = append(out, contactOfVCard(c))
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i]["name"].(string)) < strings.ToLower(out[j]["name"].(string))
	})
	return out, nil
}

// vProp is a vCard's or iCalendar's property: its name (upper case, no
// group), its parameters' values by name (a bare one is a TYPE), its
// value (unescaped for text; components split on unescaped ';').
type vProp struct {
	name   string
	params map[string][]string
	raw    string
}

// vProps reads a vCard's or iCalendar's lines (unfolded).
func vProps(s string) []vProp {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n ", "")
	s = strings.ReplaceAll(s, "\n\t", "")
	var out []vProp
	for _, line := range strings.Split(s, "\n") {
		// The name and parameters end at the first ':' outside quotes.
		colon, quoted := -1, false
		for i := 0; i < len(line); i++ {
			if line[i] == '"' {
				quoted = !quoted
			} else if line[i] == ':' && !quoted {
				colon = i
				break
			}
		}
		if colon < 0 {
			continue
		}
		head := strings.Split(line[:colon], ";")
		p := vProp{name: strings.ToUpper(head[0]), params: map[string][]string{}, raw: line[colon+1:]}
		if i := strings.LastIndexByte(p.name, '.'); i >= 0 {
			p.name = p.name[i+1:]
		}
		for _, kv := range head[1:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				k, v = "TYPE", kv
			}
			k = strings.ToUpper(k)
			for _, x := range strings.Split(v, ",") {
				p.params[k] = append(p.params[k], strings.Trim(x, `"`))
			}
		}
		out = append(out, p)
	}
	return out
}

// text is a text value, unescaped.
func (p vProp) text() string { return vUnescape(p.raw) }

// parts are a structured value's components (N, ORG), unescaped.
func (p vProp) parts() []string {
	var out []string
	var b strings.Builder
	esc := false
	for _, r := range p.raw {
		switch {
		case esc:
			b.WriteRune('\\')
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == ';':
			out = append(out, vUnescape(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(out, vUnescape(b.String()))
}

func vUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	esc := false
	for _, r := range s {
		if esc {
			switch r {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteRune(r)
			}
			esc = false
		} else if r == '\\' {
			esc = true
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// vLabel is a phone's or an email's label, of its TYPEs: the label
// asuka/contacts has on the other platforms ("mobile", "home", "work"...),
// "" for none.
func vLabel(types []string) string {
	has := map[string]bool{}
	for _, t := range types {
		has[strings.ToUpper(t)] = true
	}
	switch {
	case has["CELL"]:
		return "mobile"
	case has["FAX"] && has["HOME"]:
		return "home fax"
	case has["FAX"] && has["WORK"]:
		return "work fax"
	case has["FAX"]:
		return "fax"
	case has["PAGER"]:
		return "pager"
	case has["IPHONE"]:
		return "iPhone"
	case has["MAIN"] || has["PREF"] && has["MAIN"]:
		return "main"
	case has["HOME"]:
		return "home"
	case has["WORK"]:
		return "work"
	case has["OTHER"]:
		return "other"
	}
	return ""
}

// vBirthday is a BDAY as asuka/contacts has it: YYYY-MM-DD, or --MM-DD
// without a year; "" if it's not a date.
func vBirthday(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, 'T'); i >= 0 {
		s = s[:i]
	}
	d := strings.ReplaceAll(s, "-", "")
	switch {
	case strings.HasPrefix(s, "--") && len(d) == 4:
		return "--" + d[:2] + "-" + d[2:]
	case len(d) == 8:
		return d[:4] + "-" + d[4:6] + "-" + d[6:]
	}
	return ""
}

// contactOfVCard is a contact as asuka/contacts' JSON.
func contactOfVCard(card string) map[string]any {
	c := map[string]any{"id": "", "name": "", "givenName": "", "familyName": "", "organization": "", "jobTitle": "",
		"birthday": "", "note": ""}
	phones, emails := []map[string]string{}, []map[string]string{}
	for _, p := range vProps(card) {
		switch p.name {
		case "UID":
			c["id"] = p.text()
		case "FN":
			c["name"] = p.text()
		case "N":
			n := p.parts()
			c["familyName"] = n[0]
			if len(n) > 1 {
				c["givenName"] = n[1]
			}
		case "ORG":
			c["organization"] = p.parts()[0]
		case "TITLE":
			c["jobTitle"] = p.text()
		case "TEL":
			phones = append(phones, map[string]string{"label": vLabel(p.params["TYPE"]), "value": p.text()})
		case "EMAIL":
			emails = append(emails, map[string]string{"label": vLabel(p.params["TYPE"]), "value": p.text()})
		case "BDAY":
			c["birthday"] = vBirthday(p.text())
		case "NOTE":
			c["note"] = p.text()
		}
	}
	if c["name"] == "" {
		c["name"] = strings.TrimSpace(c["givenName"].(string) + " " + c["familyName"].(string))
		if c["name"] == "" {
			c["name"] = c["organization"]
		}
	}
	c["phones"], c["emails"] = phones, emails
	return c
}

// contactAlertText is the add alert's title and body: the name, then
// the organization, phones and emails, a line each.
func contactAlertText(o contactArgs) (title, body string) {
	title = strings.TrimSpace(o.GivenName + " " + o.FamilyName)
	if title == "" {
		title = o.Name
	}
	if title == "" {
		title = o.Organization
	}
	var lines []string
	if o.Organization != "" && title != o.Organization {
		org := o.Organization
		if o.JobTitle != "" {
			org = o.JobTitle + " · " + org
		}
		lines = append(lines, org)
	}
	item := func(label, v string) string {
		if label == "" {
			return v
		}
		return label + ": " + v
	}
	for _, p := range o.Phones {
		lines = append(lines, item(p.Label, p.Value))
	}
	for _, e := range o.Emails {
		lines = append(lines, item(e.Label, e.Value))
	}
	if o.Birthday != "" {
		lines = append(lines, o.Birthday)
	}
	return title, strings.Join(lines, "\n")
}

// contactPicker is the pick dialog.
type contactPicker struct {
	dialog   Ptr
	list     Ptr
	search   Ptr
	rows     []Ptr
	contacts []map[string]any
	reply    func([]byte, error)
}

var contactPicking *contactPicker

// pickContact shows the contacts in a dialog over the window, a search
// field over them: a row's activation answers its contact, Cancel or
// Escape nothing.
func (h *Host) pickContact(cs []map[string]any, reply func([]byte, error)) {
	contactPicking.finish(-1) // one at a time
	p := &contactPicker{contacts: cs, reply: reply}
	p.dialog = ObjectRefSink(AdwDialogNew())
	AdwDialogSetContentWidth(p.dialog, 420)
	AdwDialogSetContentHeight(p.dialog, 560)
	tv := AdwToolbarViewNew()
	bar := AdwHeaderBarNew()
	AdwHeaderBarSetShowStartTitleButtons(bar, false)
	AdwHeaderBarSetShowEndTitleButtons(bar, false)
	cancel := ButtonNew()
	ButtonSetLabel(cancel, l10n.Cancel(h.locale))
	AdwHeaderBarPackStart(bar, cancel)
	p.search = SearchEntryNew()
	SearchEntrySetPlaceholder(p.search, l10n.Search(h.locale))
	WidgetSetHExpand(p.search, true)
	AdwHeaderBarSetTitleWidget(bar, p.search)
	AdwToolbarViewAddTopBar(tv, bar)
	p.list = ListBoxNew()
	WidgetAddCSSClass(p.list, "navigation-sidebar")
	for _, c := range cs {
		row := AdwActionRowNew()
		AdwPreferencesRowSetUseMarkup(row, false)
		AdwPreferencesRowSetTitle(row, c["name"].(string))
		if sub := contactSubtitle(c); sub != "" {
			AdwActionRowSetSubtitle(row, sub)
		}
		ListBoxRowSetActivatable(row, true)
		ListBoxAppend(p.list, row)
		p.rows = append(p.rows, row)
	}
	sw := ScrolledWindowNew()
	ScrolledWindowSetPolicy(sw, PolicyNever, PolicyAutomatic)
	WidgetSetVExpand(sw, true)
	ScrolledWindowSetChild(sw, p.list)
	AdwToolbarViewSetContent(tv, sw)
	AdwDialogSetChild(p.dialog, tv)
	Connect(p.list, "row-activated", func(row uintptr) { p.finish(int(ListBoxRowGetIndex(row))) })
	Connect(p.search, "search-changed", func() { p.filter(EditableGetText(p.search)) })
	Connect(cancel, "clicked", func() { p.finish(-1) })
	Connect(p.dialog, "closed", func() { p.finish(-1) })
	contactPicking = p
	AdwDialogPresent(p.dialog, h.dialogParent())
	WidgetGrabFocus(p.search)
}

// contactSubtitle is a row's second line: the first phone, else email,
// else the organization.
func contactSubtitle(c map[string]any) string {
	for _, k := range []string{"phones", "emails"} {
		if l, _ := c[k].([]map[string]string); len(l) > 0 {
			return l[0]["value"]
		}
	}
	return c["organization"].(string)
}

// filter shows the rows whose contact has q in its name, organization,
// phones or emails.
func (p *contactPicker) filter(q string) {
	q = strings.ToLower(strings.TrimSpace(q))
	for i, row := range p.rows {
		c := p.contacts[i]
		hay := []string{c["name"].(string), c["organization"].(string)}
		for _, k := range []string{"phones", "emails"} {
			l, _ := c[k].([]map[string]string)
			for _, it := range l {
				hay = append(hay, it["value"])
			}
		}
		WidgetSetVisible(row, q == "" || strings.Contains(strings.ToLower(strings.Join(hay, "\n")), q))
	}
}

// finish closes the dialog and answers the contact at i (-1: none).
func (p *contactPicker) finish(i int) {
	if p == nil || contactPicking != p {
		return
	}
	contactPicking = nil
	AdwDialogForceClose(p.dialog)
	ObjectUnref(p.dialog)
	if i < 0 || i >= len(p.contacts) {
		p.reply(nil, nil)
		return
	}
	b, err := json.Marshal(p.contacts[i])
	p.reply(b, err)
}

// Calendars.

func (h *Host) edsCalendar(fallback map[string]method) map[string]method {
	return map[string]method{
		"request": h.edsOr(EDSCalendar, func(_ []byte, reply func([]byte, error)) { reply([]byte{3}, nil) }, fallback["request"]),
		"calendars": h.edsOr(EDSCalendar, func(_ []byte, reply func([]byte, error)) {
			h.offMain(func() ([]byte, error) {
				srcs, err := EDSSources(EDSCalendar)
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, s := range srcs {
					ro, err := EDSCalendarReadonly(s.UID)
					out = append(out, map[string]any{"id": s.UID, "title": s.Name, "color": strings.ToLower(s.Color),
						"source": s.Account, "writable": err == nil && !ro})
				}
				return json.Marshal(out)
			}, reply)
		}, fallback["calendars"]),
		"events": h.edsOr(EDSCalendar, func(args []byte, reply func([]byte, error)) {
			var o struct {
				From      float64  `json:"from"`
				To        float64  `json:"to"`
				Calendars []string `json:"calendars"`
			}
			json.Unmarshal(args, &o)
			h.offMain(func() ([]byte, error) {
				return edsEvents(time.UnixMilli(int64(o.From)), time.UnixMilli(int64(o.To)), o.Calendars)
			}, reply)
		}, fallback["events"]),
		"add": h.edsOr(EDSCalendar, func(args []byte, reply func([]byte, error)) {
			var o struct {
				Title      string `json:"title"`
				Location   string `json:"location"`
				Notes      string `json:"notes"`
				URL        string `json:"url"`
				CalendarID string `json:"calendarId"`
				StartMs    int64  `json:"startMs"`
				EndMs      int64  `json:"endMs"`
				AllDay     bool   `json:"allDay"`
			}
			if json.Unmarshal(args, &o) != nil {
				reply(nil, errors.New("calendar.add: bad arguments"))
				return
			}
			start, end := calAddTimes(o.StartMs, o.EndMs, o.AllDay, time.Now())
			title := o.Title
			if title == "" {
				title = l10n.NewEvent(h.locale)
			}
			body := h.eventDates(start, end, o.AllDay)
			if o.Location != "" {
				body += "\n" + o.Location
			}
			edsConfirm(h, title, body, l10n.AddToCalendar(h.locale), func(ok bool) {
				if !ok {
					reply([]byte{0}, nil)
					return
				}
				ev := vEvent(randomName()+"@asuka", o.Title, o.Location, o.Notes, o.URL, start, end, o.AllDay, time.Now())
				h.offMain(func() ([]byte, error) {
					if _, err := EDSAddEvent(o.CalendarID, ev); err != nil {
						return nil, err
					}
					return []byte{1}, nil
				}, reply)
			})
		}, fallback["add"]),
	}
}

// calAddTimes is "add"'s start and end (ms) as the event is saved: no
// start, the next hour (all day: today); an end not after the start an
// hour after it (all day: the day after).
func calAddTimes(startMs, endMs int64, allDay bool, now time.Time) (int64, int64) {
	if startMs <= 0 {
		if allDay {
			t := now.Local()
			startMs = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		} else {
			startMs = now.Truncate(time.Hour).Add(time.Hour).UnixMilli()
		}
		endMs = startMs
	}
	if allDay {
		day := func(ms int64) int64 {
			u := time.UnixMilli(ms).UTC()
			return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		}
		startMs, endMs = day(startMs), day(endMs)
		if endMs <= startMs {
			endMs = startMs + 24*3600*1000
		}
		return startMs, endMs
	}
	if endMs <= startMs {
		endMs = startMs + 3600*1000
	}
	return startMs, endMs
}

// eventDates are an event's dates as the user's locale writes them
// (GLib's %x), with its times (the user's clock).
func (h *Host) eventDates(startMs, endMs int64, allDay bool) string {
	format := func(ms int64, f string) string {
		var dt Ptr
		if allDay { // the days, wherever the user is
			u := time.UnixMilli(ms).UTC()
			dt = DateTimeNewLocal(int32(u.Year()), int32(u.Month()), int32(u.Day()), 0, 0, 0)
		} else {
			dt = DateTimeNewFromUnixLocal(ms / 1000)
		}
		defer DateTimeUnref(dt)
		return strings.TrimSpace(GoStringFree(DateTimeFormat(dt, f)))
	}
	clock := "%H:%M"
	if h.hourCycle == 12 {
		clock = "%l:%M %p"
	}
	if allDay {
		last := endMs - 24*3600*1000
		if last <= startMs {
			return format(startMs, "%x")
		}
		return format(startMs, "%x") + " – " + format(last, "%x")
	}
	s, e := format(startMs, "%x"), format(endMs, "%x")
	if s == e {
		return s + ", " + format(startMs, clock) + " – " + format(endMs, clock)
	}
	return s + ", " + format(startMs, clock) + " – " + e + ", " + format(endMs, clock)
}

// edsEvents are the occurrences between from and to in the calendars
// (all: none given), sorted by start, as asuka/calendar's JSON. Off the
// main thread.
func edsEvents(from, to time.Time, calendars []string) ([]byte, error) {
	srcs, err := EDSSources(EDSCalendar)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range calendars {
		want[id] = true
	}
	out := []map[string]any{}
	var firstErr error
	for _, s := range srcs {
		if len(want) > 0 && !want[s.UID] {
			continue
		}
		insts, err := EDSEvents(s.UID, from, to)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, in := range insts {
			out = append(out, eventOfInstance(s.UID, in))
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["startMs"].(int64) < out[j]["startMs"].(int64) })
	return json.Marshal(out)
}

// eventOfInstance is an occurrence as asuka/calendar's JSON: the VEVENT's
// text properties, the occurrence's times.
func eventOfInstance(calendar string, in EDSInstance) map[string]any {
	e := map[string]any{"id": "", "calendarId": calendar, "title": "", "location": "", "notes": "", "url": "",
		"startMs": in.StartMs, "endMs": in.EndMs, "allDay": in.AllDay}
	depth := 0
	for _, p := range vProps(in.ICal) {
		switch p.name {
		case "BEGIN":
			depth++
			continue
		case "END":
			depth--
			continue
		}
		if depth != 1 { // the event's own, not an alarm's
			continue
		}
		switch p.name {
		case "UID":
			e["id"] = p.text()
		case "SUMMARY":
			e["title"] = p.text()
		case "LOCATION":
			e["location"] = p.text()
		case "DESCRIPTION":
			e["notes"] = p.text()
		case "URL":
			e["url"] = p.raw
		}
	}
	return e
}

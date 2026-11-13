//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
)

// asuka/calendar on a Mac, with EventKit as on iOS: "request" answers u8
// access (not asked, denied, write only, full), "calendars" and "events"
// (JSON: from, to in ms, calendars) JSON, "add" (an event's JSON) u8
// saved. macOS has no event editor for apps (EventKitUI is iOS's): "add"
// asks with an alert over the window (the title, its dates, its place)
// and saves it on Add, which needs only write access (macOS 14). macOS
// asks once, with the app's NSCalendarsFullAccessUsageDescription
// (NSCalendarsWriteOnlyAccessUsageDescription for adding only).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["calendar"] = h.calendarModule() })
}

// Seams: what asks the system, which tests replace.
var (
	// calStatus is EventKit's authorization status for events.
	calStatus = func() int {
		if !calLoad() {
			return 1
		}
		return SendInt(Class("EKEventStore"), "authorizationStatusForEntityType:", 0)
	}
	// calRequest asks for full access (write only: for adding), then calls
	// done on the main thread.
	calRequest = func(writeOnly bool, done func()) {
		var b objc.Block
		b = NewBlock(func(_ objc.Block, ok bool, err ID) {
			theHost.Post(func() {
				b.Release()
				done()
			})
		})
		s := calStore()
		switch {
		case writeOnly && RespondsTo(s, "requestWriteOnlyAccessToEventsWithCompletion:"):
			Send(s, "requestWriteOnlyAccessToEventsWithCompletion:", uintptr(b))
		case RespondsTo(s, "requestFullAccessToEventsWithCompletion:"):
			Send(s, "requestFullAccessToEventsWithCompletion:", uintptr(b))
		default:
			Send(s, "requestAccessToEntityType:completion:", 0, uintptr(b))
		}
	}
)

var calStoreObj ID

func calLoad() bool { return framework("EventKit") != 0 && HasClass("EKEventStore") }

func calStore() ID {
	if calStoreObj == 0 {
		calStoreObj = New("EKEventStore")
	}
	return calStoreObj
}

// calAccess is the status as asuka/calendar's Access: not asked, denied,
// write only, full.
func calAccess() byte {
	switch calStatus() {
	case 3: // fullAccess (authorized before macOS 14)
		return 3
	case 4: // writeOnly
		return 2
	case 0:
		return 0
	}
	return 1
}

// calDate is an NSDate of a time.
func calDate(t time.Time) ID {
	return MsgDateSince(Class("NSDate"), Sel("dateWithTimeIntervalSince1970:"), float64(t.UnixNano())/1e9)
}

// calTime is an NSDate's time.
func calTime(d ID) time.Time {
	if d == 0 {
		return time.Time{}
	}
	s := MsgFloat(d, Sel("timeIntervalSince1970"))
	return time.Unix(0, int64(s*1e9))
}

// calEventMs is an event's start and end as the wire has them: all-day
// events' days at UTC midnight (as Android keeps them), their end the
// day after the last (EventKit's is the last day's end).
func calEventMs(start, end time.Time, allDay bool, loc *time.Location) (int64, int64) {
	if !allDay {
		return start.UnixMilli(), end.UnixMilli()
	}
	day := func(t time.Time) time.Time {
		t = t.In(loc)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return day(start).UnixMilli(), day(end).AddDate(0, 0, 1).UnixMilli()
}

// calAddTimes is "add"'s start and end as EventKit takes them: all-day
// days (UTC on the wire) the same days here, the end day inclusive; an
// end before the start an hour after it (the start's day, all day). No
// start: the next hour.
func calAddTimes(startMs, endMs int64, allDay bool, loc *time.Location, now time.Time) (time.Time, time.Time) {
	start, end := time.UnixMilli(startMs), time.UnixMilli(endMs)
	if startMs <= 0 {
		start = now.In(loc).Truncate(time.Hour).Add(time.Hour)
		end = start
	}
	if allDay {
		local := func(t time.Time) time.Time {
			u := t.UTC()
			return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, loc)
		}
		if startMs > 0 {
			start, end = local(start), local(end)
		} else {
			t := now.In(loc)
			start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			end = start
		}
		if end.After(start) {
			return start, end.Add(-time.Second)
		}
		return start, start
	}
	if end.After(start) {
		return start, end
	}
	return start, start.Add(time.Hour)
}

// calHex is an NSColor as #rrggbb in sRGB.
func calHex(c ID) string {
	if c == 0 {
		return ""
	}
	c = Send(c, "colorUsingColorSpace:", uintptr(Send(Class("NSColorSpace"), "sRGBColorSpace")))
	if c == 0 {
		return ""
	}
	ch := func(sel string) int {
		v := MsgFloat(c, Sel(sel))
		return int(min(max(v, 0), 1) * 255)
	}
	return fmt.Sprintf("#%02x%02x%02x", ch("redComponent"), ch("greenComponent"), ch("blueComponent"))
}

func calEventJSON(e ID) map[string]any {
	all := SendBool(e, "isAllDay")
	s, en := calEventMs(calTime(Send(e, "startDate")), calTime(Send(e, "endDate")), all, time.Local)
	url := ""
	if u := Send(e, "URL"); u != 0 {
		url = GoString(Send(u, "absoluteString"))
	}
	cal := ""
	if c := Send(e, "calendar"); c != 0 {
		cal = GoString(Send(c, "calendarIdentifier"))
	}
	return map[string]any{
		"id": GoString(Send(e, "eventIdentifier")), "calendarId": cal,
		"title": GoString(Send(e, "title")), "location": GoString(Send(e, "location")), "notes": GoString(Send(e, "notes")), "url": url,
		"startMs": s, "endMs": en, "allDay": all,
	}
}

// calArgs is "add"'s JSON.
type calArgs struct {
	Title      string `json:"title"`
	Location   string `json:"location"`
	Notes      string `json:"notes"`
	URL        string `json:"url"`
	CalendarID string `json:"calendarId"`
	StartMs    int64  `json:"startMs"`
	EndMs      int64  `json:"endMs"`
	AllDay     bool   `json:"allDay"`
}

// calAlertText is the add alert's message and informative text: the
// title, then the dates (as the user's locale writes a range) and the
// place.
func calAlertText(o calArgs, start, end time.Time) (title, info string) {
	title = o.Title
	if title == "" {
		title = l10n.NewEvent(hostLocale())
	}
	f := New("NSDateIntervalFormatter")
	defer Release(f)
	Send(f, "setDateStyle:", 3) // NSDateIntervalFormatterLongStyle
	if o.AllDay {
		Send(f, "setTimeStyle:", 0)
	} else {
		Send(f, "setTimeStyle:", 1) // short
	}
	info = GoString(Send(f, "stringFromDate:toDate:", uintptr(calDate(start)), uintptr(calDate(end))))
	if o.Location != "" {
		info += "\n" + o.Location
	}
	return title, info
}

// calSave saves the event, once the user said Add: write access is
// enough.
func calSave(o calArgs, start, end time.Time, reply func([]byte, error)) {
	save := func() {
		if a := calAccess(); a < 2 {
			reply(nil, errNotAllowed)
			return
		}
		WithPool(func() {
			s := calStore()
			e := Send(Class("EKEvent"), "eventWithEventStore:", uintptr(s))
			Send(e, "setTitle:", uintptr(String(o.Title)))
			if o.Location != "" {
				Send(e, "setLocation:", uintptr(String(o.Location)))
			}
			if o.Notes != "" {
				Send(e, "setNotes:", uintptr(String(o.Notes)))
			}
			if o.URL != "" {
				if u := URL(o.URL); u != 0 {
					Send(e, "setURL:", uintptr(u))
				}
			}
			Send(e, "setAllDay:", Bool(o.AllDay))
			Send(e, "setStartDate:", uintptr(calDate(start)))
			Send(e, "setEndDate:", uintptr(calDate(end)))
			var c ID
			if o.CalendarID != "" {
				c = Send(s, "calendarWithIdentifier:", uintptr(String(o.CalendarID)))
			}
			if c == 0 {
				c = Send(s, "defaultCalendarForNewEvents")
			}
			if c == 0 {
				reply(nil, errors.New("no calendar to add to"))
				return
			}
			Send(e, "setCalendar:", uintptr(c))
			var err ID
			if !SendBool(s, "saveEvent:span:commit:error:", uintptr(e), 0, 1, uintptr(unsafe.Pointer(&err))) {
				reply(nil, errorText(err, "couldn't save the event"))
				return
			}
			reply([]byte{1}, nil)
		})
	}
	if calStatus() == 0 {
		calRequest(true, save)
		return
	}
	save()
}

func (h *Host) calendarModule() map[string]method {
	return map[string]method{
		"request": func(_ []byte, reply func([]byte, error)) {
			if calAccess() == 3 {
				reply([]byte{3}, nil)
				return
			}
			if !calLoad() {
				reply([]byte{calAccess()}, nil)
				return
			}
			calRequest(false, func() { reply([]byte{calAccess()}, nil) })
		},
		"calendars": func(_ []byte, reply func([]byte, error)) {
			if calAccess() != 3 {
				reply(nil, errNotAllowed)
				return
			}
			out := []map[string]any{}
			for _, c := range ArrayItems(Send(calStore(), "calendarsForEntityType:", 0)) {
				src := ""
				if s := Send(c, "source"); s != 0 {
					src = GoString(Send(s, "title"))
				}
				out = append(out, map[string]any{
					"id": GoString(Send(c, "calendarIdentifier")), "title": GoString(Send(c, "title")),
					"color": calHex(Send(c, "color")), "source": src, "writable": SendBool(c, "allowsContentModifications"),
				})
			}
			b, err := json.Marshal(out)
			reply(b, err)
		},
		"events": func(args []byte, reply func([]byte, error)) {
			if calAccess() != 3 {
				reply(nil, errNotAllowed)
				return
			}
			var o struct {
				From      float64  `json:"from"`
				To        float64  `json:"to"`
				Calendars []string `json:"calendars"`
			}
			json.Unmarshal(args, &o)
			s := calStore()
			var cals ID
			if len(o.Calendars) > 0 {
				want := map[string]bool{}
				for _, id := range o.Calendars {
					want[id] = true
				}
				var cs []ID
				for _, c := range ArrayItems(Send(s, "calendarsForEntityType:", 0)) {
					if want[GoString(Send(c, "calendarIdentifier"))] {
						cs = append(cs, c)
					}
				}
				cals = Retain(Array(cs...))
			}
			from, to := time.UnixMilli(int64(o.From)), time.UnixMilli(int64(o.To))
			h.offMain(func() ([]byte, error) {
				defer Release(cals)
				out := []map[string]any{}
				// EventKit's predicate spans at most four years: in steps.
				for start := from; start.Before(to); {
					end := start.AddDate(4, 0, 0)
					if end.After(to) {
						end = to
					}
					p := Send(s, "predicateForEventsWithStartDate:endDate:calendars:", uintptr(calDate(start)), uintptr(calDate(end)), uintptr(cals))
					for _, e := range ArrayItems(Send(s, "eventsMatchingPredicate:", uintptr(p))) {
						// An event over a step's edge comes in both: once.
						if calTime(Send(e, "startDate")).Before(start) && !start.Equal(from) {
							continue
						}
						out = append(out, calEventJSON(e))
					}
					start = end
				}
				sort.SliceStable(out, func(i, j int) bool { return out[i]["startMs"].(int64) < out[j]["startMs"].(int64) })
				return json.Marshal(out)
			}, reply)
		},
		"add": func(args []byte, reply func([]byte, error)) {
			var o calArgs
			if err := json.Unmarshal(args, &o); err != nil {
				reply(nil, errors.New("calendar.add: bad arguments"))
				return
			}
			if !calLoad() {
				reply(nil, errors.New("no calendar"))
				return
			}
			start, end := calAddTimes(o.StartMs, o.EndMs, o.AllDay, time.Local, time.Now())
			title, info := calAlertText(o, start, end)
			h.personalConfirm(title, info, l10n.AddToCalendar(h.locale), func(ok bool) {
				if !ok {
					reply([]byte{0}, nil)
					return
				}
				calSave(o, start, end, reply)
			})
		},
	}
}

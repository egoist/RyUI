// Package calendar reads the user's calendars and offers the system's
// event editor (expo-calendar): EventKit on iOS, CalendarContract on
// Android.
//
// Add needs no permission: the user saves the event in the system's own
// editor. Calendars and Events read the user's calendars, with their
// permission (Request); an app that imports this package is given what it
// needs (iOS's calendar purpose strings, which ryui.json's ios.usage says
// better; Android's READ_CALENDAR).
package calendar

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/egoist/ryui/ui"
)

// Calendar is one of the user's calendars.
type Calendar struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Color is "#rrggbb".
	Color string `json:"color"`
	// Source is its account ("iCloud", "you@gmail.com").
	Source string `json:"source"`
	// Writable: events can be added to it.
	Writable bool `json:"writable"`
}

// Event is an event, or one occurrence of a repeating one.
type Event struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendarId"`
	Title      string    `json:"title"`
	Location   string    `json:"location"`
	Notes      string    `json:"notes"`
	URL        string    `json:"url"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	// AllDay: Start and End are days, not times: midnight UTC of the
	// first day, and of the day after the last (exclusive).
	AllDay bool `json:"allDay"`
}

// Access is the app's access to the calendars.
type Access uint8

const (
	NotAsked Access = iota
	Denied
	// WriteOnly (iOS 17): the app may add events, not read them.
	WriteOnly
	Full
)

// ErrNotAllowed: the app doesn't have access (Request).
var ErrNotAllowed = errors.New("calendar: not allowed")

// Request asks the user for full access to their calendars (the first
// time; after, it's their answer then, or what they've set in Settings
// since). Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request() (Access, error) {
	b, err := ui.CallWait("calendar", "request", nil)
	if err != nil || len(b) == 0 {
		return NotAsked, err
	}
	return Access(b[0]), nil
}

// Calendars are the user's event calendars. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Calendars() ([]Calendar, error) {
	b, err := ui.CallWait("calendar", "calendars", nil)
	if err != nil {
		return nil, mapErr(err)
	}
	var cs []Calendar
	err = json.Unmarshal(b, &cs)
	return cs, err
}

// Events are the events between from and to (each occurrence of a
// repeating one), in the calendars given (none: all), sorted by start.
// Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Events(from, to time.Time, calendarIDs ...string) ([]Event, error) {
	args, _ := json.Marshal(map[string]any{"from": from.UnixMilli(), "to": to.UnixMilli(), "calendars": calendarIDs})
	b, err := ui.CallWait("calendar", "events", args)
	if err != nil {
		return nil, mapErr(err)
	}
	var wire []struct {
		Event
		StartMs int64 `json:"startMs"`
		EndMs   int64 `json:"endMs"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		return nil, err
	}
	out := make([]Event, len(wire))
	for i, w := range wire {
		out[i] = w.Event
		out[i].Start, out[i].End = time.UnixMilli(w.StartMs), time.UnixMilli(w.EndMs)
		if w.AllDay { // days: in UTC on the wire, as the platforms keep them
			out[i].Start, out[i].End = out[i].Start.UTC(), out[i].End.UTC()
		}
	}
	return out, nil
}

// Add shows the system's event editor, filled in with e (its CalendarID,
// if set, the calendar), where the user saves the event or cancels; done
// gets whether it was saved (iOS; Android's calendar apps don't say: false).
// No permission is asked (iOS 17 and later; before, it asks for access).
// Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Add(e Event, done func(saved bool, err error)) {
	args, _ := json.Marshal(map[string]any{
		"title": e.Title, "location": e.Location, "notes": e.Notes, "url": e.URL, "calendarId": e.CalendarID,
		"startMs": e.Start.UnixMilli(), "endMs": e.End.UnixMilli(), "allDay": e.AllDay,
	})
	ui.Call("calendar", "add", args, func(b []byte, err error) {
		if done != nil {
			done(err == nil && len(b) > 0 && b[0] == 1, mapErr(err))
		}
	})
}

func mapErr(err error) error {
	if err != nil && err.Error() == "not allowed" {
		return ErrNotAllowed
	}
	return err
}

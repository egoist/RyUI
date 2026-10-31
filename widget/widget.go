// Package widget fills the app's home screen widgets (expo-widgets): the
// kinds ryui.json's widgets declare, each shown with the platform's own
// widget template (WidgetKit on iOS, home screen and lock screen; an
// App Widget on Android), and what they show, set from the app's Go code.
//
//	"widgets": [{"kind": "today", "name": "Today", "description": "What's next today"}]
//
//	widget.Set("today", widget.Entry{Title: "Next", Value: "3 tasks", Body: "Call Ana at 4", URL: "myapp://today"})
//
// A widget shows the entry set last (or, of a timeline, the one whose
// time has come) until the app sets another: whether or not the app runs.
// Tapping it opens the app, with its URL for ui.OnOpenURL.
package widget

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/ryui/ui"
)

// Entry is what a widget shows. Each field is optional; the template
// leaves out what's empty, and lays out what's there for the widget's
// size.
type Entry struct {
	// Title is its heading, small, in the tint (with Symbol before it).
	Title string `json:"title,omitempty"`
	// Value is its large figure: "72°", "3 tasks", "$1,204".
	Value string `json:"value,omitempty"`
	// Body is its text, as many lines as the size allows.
	Body string `json:"body,omitempty"`
	// Caption is small text at the bottom.
	Caption string `json:"caption,omitempty"`
	// Symbol is an SF Symbol name, as ui.Symbol takes it (Android: RyUI's
	// Material set for the same names).
	Symbol ui.Icon `json:"symbol,omitempty"`
	// Progress, with ShowProgress, is a bar (a ring on the lock screen),
	// from 0 to 1.
	Progress     float64 `json:"progress,omitempty"`
	ShowProgress bool    `json:"showProgress,omitempty"`
	// Until counts down to a time, live, without the app ("12:34"), and
	// then up from it.
	Until time.Time `json:"-"`
	// Tint colors the title, the symbol and the progress (ui.RGB, ui.Hex;
	// default: the app's accent).
	Tint ui.Color `json:"-"`
	// URL is what tapping it opens the app with (one of its scheme's).
	URL string `json:"url,omitempty"`
	// At is when it shows, in a timeline (zero: now).
	At time.Time `json:"-"`
}

// wire is an Entry with its times in ms since 1970 and its tint as
// #rrggbbaa.
type wire struct {
	Entry
	UntilMs int64  `json:"untilMs,omitempty"`
	AtMs    int64  `json:"atMs,omitempty"`
	Tint    string `json:"tint,omitempty"`
}

// Set sets what the widgets of kind show: an entry, or a timeline of them
// (each from its At on), and updates the widgets on the home screen. It's
// kept until set again, across launches. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Set(kind string, entries ...Entry) {
	ui.Call("widget", "set", encode(kind, entries), nil)
}

// Clear empties the widgets of kind: they show their placeholder. Main
// thread.
func Clear(kind string) { Set(kind) }

func encode(kind string, entries []Entry) []byte {
	entries = slices.Clone(entries)
	slices.SortStableFunc(entries, func(a, b Entry) int { return a.At.Compare(b.At) })
	ws := make([]wire, len(entries))
	for i, e := range entries {
		ws[i] = wire{Entry: e}
		if !e.Until.IsZero() {
			ws[i].UntilMs = e.Until.UnixMilli()
		}
		if !e.At.IsZero() {
			ws[i].AtMs = e.At.UnixMilli()
		}
		if r, g, b, a, ok := e.Tint.RGBA(); ok {
			ws[i].Tint = fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, a)
		}
	}
	b, _ := json.Marshal(map[string]any{"kind": kind, "entries": ws})
	return b
}

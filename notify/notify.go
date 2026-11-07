// Package notify is local notifications: shown now or at a time the app
// sets, whether or not it's running then; tapping one opens the app with
// its URL (ui.OnOpenURL gets it). The user allows them first (Request).
// An app that imports it is given the permission it needs on Android.
//
// What a messaging app needs is here too, for ryui/push's notifications
// as well: actions and inline replies (SetCategories, OnAction), threads
// that group a chat's notifications, Android's channels (SetChannels),
// messages in conversations (Notification.From), the app icon's badge
// (SetBadge), the notifications showing (Showing, Remove, RemoveThread),
// and which ones show while the app is open (Present).
package notify

import (
	"encoding/binary"
	"encoding/json"
	"time"

	"github.com/egoist/ryui/internal/notifyspec"
	"github.com/egoist/ryui/ui"
)

// Notification is what to show, and when.
type Notification struct {
	// ID names it, to replace or Cancel it: scheduling one with an ID
	// that's waiting or showing replaces that one. A message's
	// (From) is its conversation's ID by default, so on Android a
	// conversation is one notification, its messages in it.
	ID    string
	Title string
	Body  string
	// URL is what the app is opened with when the user taps it (one of
	// its scheme's, ryui.json's scheme): ui.OnOpenURL gets it.
	URL string
	// After is how long from now it shows (at least a second; Show shows
	// it now).
	After time.Duration
	// Subtitle is a line under the title (iOS), Android's sub text beside
	// the app's name.
	Subtitle string
	// Data are the app's own keys, given back with it in Showing,
	// OnAction and Present.
	Data map[string]string
	// Category is a category's ID (SetCategories): the actions it shows.
	Category string
	// Thread groups it with the others of the thread: iOS's
	// threadIdentifier, an Android group, whose summary notification is
	// kept as its notifications come and go. A message's is its
	// conversation's ID by default.
	Thread string
	// Summary is the line under an Android group's summary notification
	// ("3 new messages"). iOS writes its own (since iOS 15).
	Summary string
	// Channel is the Android channel it's in (SetChannels); "" or one
	// that isn't is the default channel. iOS has no channels.
	Channel string
	// Badge sets the app icon's badge to it when it shows (iOS, macOS;
	// on Android it's the notification's number, which launchers that
	// badge may show); 0 leaves it.
	Badge int
	// Silent shows it without its sound.
	Silent bool
	// Image is a picture shown with it, as ui.Image's sources: an
	// asset's path, a file's, or an http(s) URL it's downloaded from
	// (iOS's attachment, Android's big picture).
	Image string

	// From's.
	sender       *Person
	conversation Conversation
}

// Request asks the user to allow notifications (the first time; after,
// it's their answer then, or what they've set in Settings since), and
// reports whether they're allowed. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request() (allowed bool, err error) {
	b, err := ui.CallWait("notify", "request", nil)
	return err == nil && len(b) > 0 && b[0] == 1, err
}

// Schedule sets n to show after n.After (at least a second). Not on the
// main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Schedule(n Notification) error {
	return post(n, max(n.After, time.Second))
}

// Show shows n now; with its ID, it replaces the one showing (a
// conversation's next message). Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Show(n Notification) error {
	return post(n, 0)
}

func post(n Notification, after time.Duration) error {
	s := n.spec()
	s.Seconds = after.Seconds()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = ui.CallWait("notify", "schedule", b)
	return err
}

// spec is n on the wire.
func (n Notification) spec() notifyspec.Spec {
	s := notifyspec.Spec{
		ID: n.ID, Title: n.Title, Subtitle: n.Subtitle, Body: n.Body, URL: n.URL, Data: n.Data,
		Category: n.Category, Thread: n.Thread, Summary: n.Summary, Channel: n.Channel,
		Badge: n.Badge, Silent: n.Silent, Image: n.Image,
	}
	if n.sender != nil {
		s.Sender, s.Conversation = wireMessage(*n.sender, n.conversation)
		if s.ID == "" {
			s.ID = s.Conversation.ID
		}
		if s.Thread == "" {
			s.Thread = s.Conversation.ID
		}
		if s.Title == "" {
			s.Title = n.sender.Name
			if n.conversation.Group && n.conversation.Title != "" {
				s.Title = n.conversation.Title
			}
		}
	}
	return s
}

// Cancel takes away the notification id: waiting, or shown. Main thread.
func Cancel(id string) {
	ui.Call("notify", "cancel", []byte(id), nil)
}

// SetBadge sets the number on the app's icon; 0 takes it away. Main
// thread.
//
// iOS and macOS show it as set. Android has no badge of its own:
// launchers that badge count the app's notifications (or show a dot), so
// it goes as they're read (Remove, RemoveThread); SetBadge does nothing
// there, and Notification.Badge is the number a launcher may show.
func SetBadge(n int) {
	ui.Call("notify", "badge", binary.LittleEndian.AppendUint32(nil, uint32(max(n, 0))), nil)
}

// Delivered is a notification of the app's that shows, or showed: in
// Showing, OnAction's Response and Present.
type Delivered struct {
	ID       string
	Title    string
	Subtitle string
	Body     string
	URL      string
	Data     map[string]string
	Category string
	Thread   string
	// Time is when it showed.
	Time time.Time
	// Remote: a push's (ryui/push), whose Data are its payload's.
	Remote bool
}

func delivered(s notifyspec.Shown) Delivered {
	d := Delivered{ID: s.ID, Title: s.Title, Subtitle: s.Subtitle, Body: s.Body, URL: s.URL, Data: s.Data,
		Category: s.Category, Thread: s.Thread, Remote: s.Remote}
	if s.Time != 0 {
		d.Time = time.UnixMilli(s.Time)
	}
	return d
}

// Showing are the app's notifications showing (in Notification Center,
// the notification shade), its pushes' too, newest first. Not on the
// main thread.
func Showing() ([]Delivered, error) {
	b, err := ui.CallWait("notify", "showing", nil)
	if err != nil {
		return nil, err
	}
	var ss []notifyspec.Shown
	if err := json.Unmarshal(b, &ss); err != nil {
		return nil, err
	}
	out := make([]Delivered, len(ss))
	for i, s := range ss {
		out[i] = delivered(s)
	}
	return out, nil
}

// Remove takes away the notifications ids that show. Main thread.
func Remove(ids ...string) {
	b, _ := json.Marshal(ids)
	ui.Call("notify", "remove", b, nil)
}

// RemoveThread takes away the notifications of thread that show: a
// chat's, once it's read. Main thread.
func RemoveThread(thread string) {
	ui.Call("notify", "removeThread", []byte(thread), nil)
}

// RemoveAll takes away all the app's notifications that show (not the
// ones waiting: Cancel them). Main thread.
func RemoveAll() {
	ui.Call("notify", "removeAll", nil, nil)
}

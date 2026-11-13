package notify

import (
	"time"

	"github.com/egoist/asuka/internal/notifyspec"
)

// Person is who sent a message, or the user (Conversation.Me).
type Person struct {
	// ID names them for the system, the same each time: a contact's,
	// an account's.
	ID   string
	Name string
	// Image is their picture, as ui.Image's sources: an asset's path, a
	// file's, or an http(s) URL it's downloaded from.
	Image string
	// Bot: an automated sender (Android shows it as one).
	Bot bool
}

// Conversation is where a message was said.
type Conversation struct {
	// ID names it, the same each time: the system keeps its
	// notifications together, and on Android its shortcut. "": a 1:1
	// conversation with the sender, whose ID it is.
	ID string
	// Title is a group's name.
	Title string
	// Group: more than two people are in it.
	Group bool
	// Image is a group's picture, as Person's.
	Image string
	// Me is the user as the conversation shows them (Android shows their
	// replies under it); its Name defaults to "You".
	Me Person
	// Earlier are messages said before this one, oldest first, which
	// Android shows above it. Without them, a message adds to its
	// conversation's notification that shows.
	Earlier []Message
}

// Message is one said in a conversation.
type Message struct {
	// From is who said it; the zero Person is the user.
	From Person
	Text string
	Time time.Time
}

// From makes n a message: sender said n.Body in c. It shows as the
// platform shows messages, with the sender's picture: iOS's
// communication notification (an INSendMessageIntent the app donates),
// Android's MessagingStyle in the shade's Conversations, with the
// conversation's shortcut. n's ID and Thread default to the
// conversation's ID, its Title to the sender's name (a group's title, for
// a group).
//
// iOS needs the Communication Notifications capability on the App ID: an
// app that calls From is given its entitlement (and Info.plist's
// NSUserActivityTypes); a release build's profile must have it. Without
// it, a message shows as a plain notification. macOS shows a message as
// a plain notification, its sender's name its title.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func (n Notification) From(sender Person, c Conversation) Notification {
	n.sender = &sender
	n.conversation = c
	return n
}

// wireMessage is a message's sender and conversation on the wire. The
// push package has its own copy (it can't reach these fields).
func wireMessage(sender Person, c Conversation) (*notifyspec.Person, *notifyspec.Conversation) {
	s := wirePerson(sender)
	wc := &notifyspec.Conversation{ID: c.ID, Title: c.Title, Group: c.Group, Image: c.Image}
	if wc.ID == "" {
		wc.ID = firstOf(sender.ID, sender.Name, "asuka")
	}
	if c.Me != (Person{}) {
		wc.Me = wirePerson(c.Me)
	}
	for _, m := range c.Earlier {
		wm := notifyspec.Message{Text: m.Text}
		if m.From != (Person{}) {
			wm.Sender = wirePerson(m.From)
		}
		if !m.Time.IsZero() {
			wm.Time = m.Time.UnixMilli()
		}
		wc.Earlier = append(wc.Earlier, wm)
	}
	return s, wc
}

func wirePerson(p Person) *notifyspec.Person {
	return &notifyspec.Person{ID: p.ID, Name: p.Name, Image: p.Image, Bot: p.Bot}
}

func firstOf(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

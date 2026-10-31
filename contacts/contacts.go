// Package contacts reads the user's contacts and offers the system's
// contact screens (expo-contacts): Contacts and ContactsUI on iOS,
// ContactsContract on Android.
//
// Pick and Add need no permission: the user picks, or saves, in the
// system's own screen. List reads the address book, with the user's
// permission (Request); an app that imports this package is given what
// it needs (iOS's NSContactsUsageDescription, which ryui.json's
// ios.usage says better; Android's READ_CONTACTS).
package contacts

import (
	"encoding/json"
	"errors"

	"github.com/egoist/ryui/ui"
)

// Contact is a person or a company in the address book.
type Contact struct {
	ID           string `json:"id"`
	Name         string `json:"name"` // as the platform shows it
	GivenName    string `json:"givenName"`
	FamilyName   string `json:"familyName"`
	Organization string `json:"organization"`
	JobTitle     string `json:"jobTitle"`
	Phones       []Item `json:"phones"`
	Emails       []Item `json:"emails"`
	// Birthday is "YYYY-MM-DD", or "--MM-DD" without a year; "" if unknown.
	Birthday string `json:"birthday"`
	// Note is Android's; iOS gives it only to apps Apple grants an
	// entitlement to.
	Note string `json:"note"`
}

// Item is a labeled value: a phone number, an email address.
type Item struct {
	// Label is the platform's, localized ("mobile", "work", "home"), or
	// the user's own.
	Label string `json:"label"`
	Value string `json:"value"`
}

// Access is the app's access to the address book.
type Access uint8

const (
	NotAsked Access = iota
	Denied          // the user said no, or the device doesn't allow it
	Limited         // iOS 18: the contacts the user chose
	Full
)

// ErrNotAllowed: the app doesn't have access (Request).
var ErrNotAllowed = errors.New("contacts: not allowed")

// Request asks the user for access to their contacts (the first time;
// after, it's their answer then, or what they've set in Settings since).
// Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request() (Access, error) {
	b, err := ui.CallWait("contacts", "request", nil)
	if err != nil || len(b) == 0 {
		return NotAsked, err
	}
	return Access(b[0]), nil
}

// Query narrows List.
type Query struct {
	// Name keeps the contacts whose name has it (the platform's matching:
	// by word prefixes, ignoring case and accents).
	Name string
	// Limit is the most to return (0: all).
	Limit int
}

// List reads the contacts, sorted as the platform sorts them. Without
// access, ErrNotAllowed. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func List(q Query) ([]Contact, error) {
	args, _ := json.Marshal(map[string]any{"name": q.Name, "limit": q.Limit})
	b, err := ui.CallWait("contacts", "list", args)
	if err != nil {
		if err.Error() == "not allowed" {
			return nil, ErrNotAllowed
		}
		return nil, err
	}
	var cs []Contact
	err = json.Unmarshal(b, &cs)
	return cs, err
}

// Pick shows the system's contact picker and calls done with the contact
// the user picked, or nil if they cancelled. No permission is asked: the
// app sees only the one picked. (On Android, without access, the contact
// has its name only; with it, all of it.) Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Pick(done func(c *Contact, err error)) {
	ui.Call("contacts", "pick", nil, func(b []byte, err error) {
		if err != nil || len(b) == 0 {
			done(nil, err)
			return
		}
		var c Contact
		if err := json.Unmarshal(b, &c); err != nil {
			done(nil, err)
			return
		}
		done(&c, nil)
	})
}

// Add shows the system's new contact screen, filled in with c, where the
// user edits it and saves it, or cancels; done gets whether it was saved
// (iOS; on Android, the contacts app's answer). No permission is asked.
// Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Add(c Contact, done func(saved bool, err error)) {
	args, _ := json.Marshal(c)
	ui.Call("contacts", "add", args, func(b []byte, err error) {
		if done != nil {
			done(err == nil && len(b) > 0 && b[0] == 1, err)
		}
	})
}

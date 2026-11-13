package main

import (
	"testing"

	"github.com/egoist/asuka/uitest"
)

func TestOpenStarAndDelete(t *testing.T) {
	a := uitest.Mount(t, App)
	a.Expect("Inbox", "Settings", "Message 20")
	a.ExpectNo("Message 1") // further down the list

	a.Tap("Message 20") // the row takes the tap
	a.Expect("Linus", "Message 20", "This is a message. Open it to see the detail screen, with bar buttons, a sheet, a menu and an alert.")
	a.ExpectNo("Message 19") // the inbox is under the detail screen

	a.In(uitest.Toolbar).Tap("Star") // a bar button says Star too
	a.Expect("Unstar")

	a.Tap("More") // a button's menu
	a.Tap("Delete…")
	a.Expect("Delete this message?")
	a.Tap("Cancel")
	a.ExpectNo("Delete this message?")

	a.Tap("Delete") // the toolbar's, which asks too
	a.Tap("Delete") // the alert's: it's the only one in reach now
	a.Expect("Message 19")
	a.ExpectNo("Message 20")
}

func TestNote(t *testing.T) {
	a := uitest.Mount(t, App)
	a.Tap("Message 18")
	a.Expect("No note yet.")
	a.Tap("Add a note")
	a.Type("Write a note", "Call back")
	a.Submit("")
	a.ExpectNo("Write a note")
	a.Expect("Note: Call back")
}

func TestReplyAsksBeforeDiscarding(t *testing.T) {
	a := uitest.Mount(t, App)
	a.Tap("Message 17")
	a.Tap("Write a Reply")
	a.Expect("To: Barbara", "Covers the screen")
	a.Type("Message", "On my way")
	a.Back() // a draft: it asks
	a.Expect("Discard this reply?")
	a.Tap("Keep Editing")
	a.Expect("To: Barbara")
	a.Tap("Cancel")
	a.Tap("Discard")
	a.ExpectNo("To: Barbara")
}

func TestRefreshBringsAMessage(t *testing.T) {
	a := uitest.Mount(t, App)
	a.ExpectNo("Message 21")
	a.Refresh("Message 20")
	a.WaitFor("Message 21") // after the pretend fetch's goroutine posts it
}

func TestComposeAndBadge(t *testing.T) {
	a := uitest.Mount(t, App)
	a.Tap("Compose") // the primary action
	a.Expect("Message 21")
	a.ScrollTo("Message 1")
}

func TestSettings(t *testing.T) {
	a := uitest.Mount(t, App)
	a.Tap("Settings")
	a.ExpectNo("Message 20")
	a.Toggle("Haptics")
	a.Tap("Sign In…")
	a.Type("Name", "ada")
	if !a.Disabled("Sign In") { // until both are filled in
		t.Fatal("Sign In is enabled with no password")
	}
	a.Type("Password", "secret")
	a.Tap("Sign In")
	a.Expect("Signed in as ada")
	a.Tap("About")
	a.Expect("Native widgets, driven by Go and fine-grained signals.")
	a.Back()
	a.Expect("Signed in as ada")
}

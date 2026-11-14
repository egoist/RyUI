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
	a.Tap("About")
	a.Expect("Native widgets, driven by Go and fine-grained signals.")
	a.Back()
	a.Expect("Not signed in")
}

// Every screen has an address: the inbox's, a message's, its reply's;
// the tabs' and the stacks' moves are the history's.
func TestAddresses(t *testing.T) {
	a := uitest.Mount(t, App)
	if a.Href() != "/inbox" { // "/" redirects to it
		t.Fatalf("at %q", a.Href())
	}
	a.Tap("Message 19")
	a.Tap("Write a Reply")
	a.Expect("To: Grace")
	if a.Href() != "/inbox/19/reply" {
		t.Fatalf("at %q", a.Href())
	}
	a.Tap("Cancel") // no draft: it closes
	a.ExpectNo("To: Grace")
	a.Tap("Settings")
	a.Tap("About")
	hrefs, at := a.History()
	if a.Href() != "/settings/about" || at != 3 || hrefs[1] != "/inbox/19" {
		t.Fatalf("at %q, history %q at %d", a.Href(), hrefs, at)
	}
	// The browser's back goes back through the tabs and stacks.
	a.GoBack()
	a.Expect("Not signed in")
	a.GoBack()
	a.Expect("Message 19", "No note yet.")
	a.GoForward()
	a.GoForward()
	a.Expect("Native widgets, driven by Go and fine-grained signals.")
	// A link to another tab's screen.
	a.Tap("Read the newest message")
	a.Expect("Message 20", "No note yet.")
	if a.Href() != "/inbox/20" {
		t.Fatalf("at %q", a.Href())
	}
}

// A link of the app's scheme, or a browser opened at an address, shows
// its screen with the inbox under it: back goes there.
func TestDeepLinks(t *testing.T) {
	a := uitest.Mount(t, App)
	a.OpenURL("asukanav://inbox/18")
	a.Expect("Message 18", "No note yet.")
	a.Back()
	a.Expect("Message 20", "Message 19")

	b := uitest.Mount(t, App, uitest.At("/inbox/17/reply"))
	b.Expect("To: Barbara", "Covers the screen")
	b.Tap("Cancel")
	b.Expect("Message 17", "No note yet.")
	b.Back()
	b.Expect("Message 20")

	c := uitest.Mount(t, App, uitest.At("/inbox/99"))
	c.Expect("There's no message 99.")
	c.Tap("Back to the inbox")
	c.Expect("Message 20")

	d := uitest.Mount(t, App, uitest.At("/nowhere"))
	d.Expect("This screen doesn't exist.")
	d.Tap("Go home")
	d.Expect("Message 20")
}

// The account screen is a signed-in user's: the others sign in first,
// in a sheet, and are taken there after; signing out leaves it.
func TestSignInGuard(t *testing.T) {
	a := uitest.Mount(t, App)
	a.Tap("Settings")
	a.Tap("Account")
	a.Expect("Sign in to see it.")
	if a.Href() != "/settings/sign-in?next=%2Fsettings%2Faccount" {
		t.Fatalf("at %q", a.Href())
	}
	a.Tap("Sign In…")
	a.Type("Name", "ada")
	if !a.Disabled("Sign In") { // until both are filled in
		t.Fatal("Sign In is enabled with no password")
	}
	a.Type("Password", "secret")
	a.Tap("Sign In")
	a.Expect("Signed in as ada", "Sign Out")
	a.ExpectNo("Sign in to see it.")
	if a.Href() != "/settings/account" {
		t.Fatalf("at %q", a.Href())
	}
	a.Tap("Sign Out")
	a.Expect("Sign in to see it.")
	a.ExpectNo("Sign Out")
	a.Dismiss()
	a.Expect("Not signed in")
}

//go:build e2e

package e2e

import (
	"testing"
	"time"
)

// alertFields is the same test on each platform: the navigation example's
// sign-in alert (in the sheet its account screen's guard sends a
// signed-out user to) has a name and a password field, its Sign In
// disabled until both are filled in; return goes from the name to the
// password, does nothing there while Sign In is disabled, then signs in,
// and the account screen shows.
func alertFields(t *testing.T, s screen) {
	waitFor(t, s, []string{"Message 20"})
	s.command("tab Settings")
	waitFor(t, s, []string{"Not signed in", "Account"})
	s.tap("Account")
	waitFor(t, s, []string{"Sign in to see it.", "Sign In…"})
	s.tap("Sign In…")
	waitFor(t, s, []string{"Sign in", "Enter your account's name and password.", "Name", "Password"})
	s.command("type ada")
	s.command("submit") // on to the password
	s.command("submit") // Sign In is disabled: nothing
	time.Sleep(300 * time.Millisecond)
	waitFor(t, s, []string{"Enter your account's name and password.", "ada"})
	s.command("type secret")
	s.command("submit")
	waitFor(t, s, []string{"Signed in as ada", "Sign Out"}, "Enter your account's name and password.", "Sign in to see it.")
}

func TestAlertFieldsWeb(t *testing.T) { alertFields(t, webApp(t, "navigation")) }

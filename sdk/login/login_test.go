package login

import (
	"errors"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ui"
)

// Set, Enabled and OpenedAtLogin speak the login module's protocol: a
// status byte, 2 for a login item the user must still allow.
func TestLogin(t *testing.T) {
	h := fake.New()
	state := byte(0)
	h.Modules = map[string]func(string, []byte) ([]byte, error){
		"login": func(method string, args []byte) ([]byte, error) {
			switch method {
			case "set":
				if args[0] == 1 {
					state = 2 // registered; the user hasn't allowed it yet
				} else {
					state = 0
				}
				return []byte{state}, nil
			case "enabled":
				return []byte{state}, nil
			case "openedAtLogin":
				return []byte{1}, nil
			}
			return nil, errors.New("no method")
		},
	}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	var setErr error
	var on, atLogin bool
	Set(true, func(err error) { setErr = err })
	Enabled(func(v bool, err error) { on = v })
	OpenedAtLogin(func(v bool) { atLogin = v })
	r.Flush()
	h.RunPosted()
	if !errors.Is(setErr, ErrNeedsApproval) || on || !atLogin {
		t.Fatalf("Set: %v, Enabled: %v, OpenedAtLogin: %v", setErr, on, atLogin)
	}
	state = 1
	Enabled(func(v bool, err error) { on = v })
	Set(false, func(err error) { setErr = err })
	r.Flush()
	h.RunPosted()
	if setErr != nil || !on {
		t.Fatalf("Set(false): %v; Enabled before it: %v", setErr, on)
	}
}

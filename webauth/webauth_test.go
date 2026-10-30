package webauth_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/ui"
	"github.com/egoist/nui/webauth"
)

func TestStart(t *testing.T) {
	h := fake.New()
	var started []byte
	canceled := false
	h.Modules = map[string]func(string, []byte) ([]byte, error){
		"webAuth": func(method string, args []byte) ([]byte, error) {
			switch method {
			case "start":
				started = args
				if args[4] == 0 { // the second, which the user closes
					return nil, errors.New("canceled")
				}
				return []byte("myapp://done?code=1"), nil
			case "cancel":
				canceled = true
				return nil, nil
			}
			return nil, errors.New("no method")
		},
	}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("hi") })
	defer r.Dispose()

	var got string
	webauth.Start(webauth.Options{URL: "https://example.com/auth", CallbackScheme: "myapp", Ephemeral: true},
		func(cb string, err error) { got = cb })
	r.Flush()
	h.RunPosted()
	if got != "myapp://done?code=1" {
		t.Fatalf("callback %q", got)
	}
	// u32 id, u8 flags, then the URL and the scheme.
	le := binary.LittleEndian
	if started[4] != 1 || string(started[9:9+le.Uint32(started[5:])]) != "https://example.com/auth" {
		t.Fatalf("args %v", started)
	}

	var gotErr error
	s := webauth.Start(webauth.Options{URL: "http://example.com"}, func(_ string, err error) { gotErr = err })
	s.Cancel()
	r.Flush()
	h.RunPosted()
	if !errors.Is(gotErr, webauth.ErrCanceled) || !canceled {
		t.Fatalf("canceled: %v, cancel called %v", gotErr, canceled)
	}
}

// Package webauth signs in on the web in the system's own sign-in
// sheet: ASWebAuthenticationSession on iOS (SwiftUI's
// webAuthenticationSession), a Custom Tab on Android. It's the browser's,
// so the user's sessions there, passkeys and password autofill work in
// it, and providers that refuse an app's own web view (Google) don't
// refuse it.
//
// It ends in one of two ways. With a CallbackScheme, when the page goes
// to a URL of that scheme (the app's own, "myapp://signed-in?code=…"):
// done gets that URL. On Android the scheme must be the app's (ryui.json's
// "scheme"). Without one, the app ends it: an OAuth sign-in that
// redirects to a server of the app's on the loopback interface
// (http://localhost:port/callback, RFC 8252) gets the code there and
// calls Cancel.
package webauth

import (
	"encoding/binary"
	"errors"

	"github.com/egoist/ryui/ui"
)

// Options configures a sign-in.
type Options struct {
	URL string // where it starts: an http(s) URL
	// CallbackScheme ends it when the page goes to a URL of this scheme,
	// which done gets. Empty: it ends when the app cancels it, or the
	// user closes it.
	CallbackScheme string
	// Ephemeral signs in without the browser's cookies, and keeps none
	// (iOS's prefersEphemeralWebBrowserSession; Android's ephemeral
	// Custom Tab, where the browser has one).
	Ephemeral bool
}

// ErrCanceled is a sign-in the user closed, or that Cancel ended.
var ErrCanceled = errors.New("webauth: canceled")

// Session is a sign-in under way.
type Session struct{ id uint32 }

var next uint32

// Start shows the sign-in; done gets the callback URL, or an error
// (ErrCanceled when it was closed). Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Start(o Options, done func(callback string, err error)) *Session {
	next++
	s := &Session{id: next}
	args := binary.LittleEndian.AppendUint32(nil, s.id)
	flags := byte(0)
	if o.Ephemeral {
		flags |= 1
	}
	args = append(args, flags)
	args = appendString(args, o.URL)
	args = appendString(args, o.CallbackScheme)
	ui.Call("webAuth", "start", args, func(b []byte, err error) {
		if done == nil {
			return
		}
		if err != nil {
			if err.Error() == "canceled" {
				err = ErrCanceled
			}
			done("", err)
			return
		}
		done(string(b), nil)
	})
	return s
}

// Cancel ends the sign-in, closing its sheet; done gets ErrCanceled.
// Main thread.
func (s *Session) Cancel() {
	ui.Call("webAuth", "cancel", binary.LittleEndian.AppendUint32(nil, s.id), nil)
}

func appendString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

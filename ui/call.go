package ui

import (
	"encoding/binary"
	"errors"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// calls are the Calls waiting for their Result, by call id.
var (
	calls    = map[uint32]func([]byte, error){}
	nextCall uint32
)

// Call runs method of a native module (the host's own, such as
// "secureStore" or "photos", or one the app registers in Swift and
// Kotlin), with args; done gets its result, on the main thread. Main
// thread.
//
// Packages wrap the built-in modules (ryui/secure); an app's own modules
// take whatever bytes it gives them, JSON for instance.
func Call(module, method string, args []byte, done func(result []byte, err error)) {
	r := activeRoot()
	if r == nil {
		if done != nil {
			done(nil, errors.New("ui: Call before Mount"))
		}
		return
	}
	nextCall++
	id := nextCall
	if done != nil {
		calls[id] = done
	}
	r.batch.CommandArgs(r.el.id, ops.CmdCall, func(b []byte) []byte {
		le := binary.LittleEndian
		b = le.AppendUint32(b, id)
		b = le.AppendUint32(b, uint32(len(module)))
		b = append(b, module...)
		b = le.AppendUint32(b, uint32(len(method)))
		b = append(b, method...)
		return append(b, args...)
	})
}

// CallWait is Call from a goroutine: it waits for the result. Never on
// the main thread, which the result comes on.
func CallWait(module, method string, args []byte) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	Post(func() {
		Call(module, method, args, func(b []byte, err error) { ch <- result{b, err} })
		flushAll()
	})
	r := <-ch
	return r.b, r.err
}

// result delivers a Result event to its Call.
func result(b []byte) {
	if len(b) < 5 {
		return
	}
	id := binary.LittleEndian.Uint32(b)
	done := calls[id]
	if done == nil {
		return
	}
	delete(calls, id)
	if b[4] == 0 {
		done(nil, errors.New(string(b[5:])))
		return
	}
	done(append([]byte(nil), b[5:]...), nil)
}

// urlHandlers get the URLs the app is opened with; pendingURL is one
// that came before any handler (the URL the app launched with).
var (
	urlHandlers []*func(string)
	pendingURL  string
)

// OnOpenURL calls f with each URL the app is opened with: its scheme
// (ryui.json's "scheme", myapp://...), or a universal or app link
// (ryui.json's "links"), at launch or since. A URL that came before any
// handler (the one the app launched with) goes to the first. It stops
// with the component's owner. Main thread.
func OnOpenURL(f func(url string)) {
	p := &f
	urlHandlers = append(urlHandlers, p)
	signal.OnCleanup(func() {
		for i, h := range urlHandlers {
			if h == p {
				urlHandlers = append(urlHandlers[:i], urlHandlers[i+1:]...)
				return
			}
		}
	})
	if pendingURL != "" {
		u := pendingURL
		pendingURL = ""
		f(u)
	}
}

func incoming(url string) {
	if len(urlHandlers) == 0 {
		pendingURL = url
		return
	}
	hs := make([]*func(string), len(urlHandlers))
	copy(hs, urlHandlers)
	for _, h := range hs {
		(*h)(url)
	}
}

// shareHandlers get the text other apps share with this one; pendingShare
// is what came before any handler (the share that launched the app).
var (
	shareHandlers []*func(string)
	pendingShare  []string
)

// OnShare calls f with what another app shares with this one: a link or
// text sent from its share sheet ("Share" in YouTube, a browser), which
// offers this app with ryui.json's "shareTarget". What came before any
// handler (the share that launched the app) goes to the first. It stops
// with the component's owner. Main thread.
//
// On iOS a share extension takes the share and opens the app with it;
// on Android the share (ACTION_SEND) opens the app.
func OnShare(f func(text string)) {
	p := &f
	shareHandlers = append(shareHandlers, p)
	signal.OnCleanup(func() {
		for i, h := range shareHandlers {
			if h == p {
				shareHandlers = append(shareHandlers[:i], shareHandlers[i+1:]...)
				return
			}
		}
	})
	pending := pendingShare
	pendingShare = nil
	for _, t := range pending {
		f(t)
	}
}

func sharedText(text string) {
	if len(shareHandlers) == 0 {
		pendingShare = append(pendingShare, text)
		return
	}
	hs := make([]*func(string), len(shareHandlers))
	copy(hs, shareHandlers)
	for _, h := range hs {
		(*h)(text)
	}
}

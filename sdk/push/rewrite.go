package push

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/notifyspec"
	"github.com/egoist/asuka/sdk/notify"
	"github.com/egoist/asuka/ui"
)

var (
	rewriteMu sync.Mutex
	rewriter  func(*Message) error
	serving   bool
)

// Rewrite sets f to change each push before it shows: decrypt an
// end-to-end encrypted payload, set its title and body, attach an image
// (Image, downloaded from its URL), make it a message in a conversation
// (From) with the sender's picture, set its thread and category. Call it
// in main, before app.Run. f runs on its own goroutine, not the main
// thread; an error (or a panic) shows the push as it came.
//
// iOS: a Notification Service Extension, which asuka build makes for an
// app that calls Rewrite, runs the app's Go code in a process of its own
// for each push with "mutable-content": 1 in its aps (and an alert, which
// the push shows if the rewrite fails): main runs, then f, but not the
// root component, so keep main light. It has about 30 seconds and 24 MB;
// what the app keeps (asuka/sdk/store, files in store.Dir, asuka/sdk/secure) is the
// same there. A rewrite can't hide a push on iOS.
//
// Android: FCM's data messages are rewritten in the app's process (the Go
// code starts without a screen if the app isn't running), within about
// ten seconds, then shown; a data message whose rewrite leaves no title
// or body doesn't show. A notification message (FCM's "notification")
// that comes while the app is in the background is shown by Firebase,
// without a rewrite: send data messages.
//
// Under asuka dev's player there's no extension: pushes show as they came.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka).
//
//go:noinline
func Rewrite(f func(m *Message) error) {
	rewriteMu.Lock()
	rewriter = f
	start := !serving && f != nil
	serving = serving || start
	rewriteMu.Unlock()
	if start {
		boot.AfterMount(func() { go serveRewrites() })
	}
}

// serveRewrites runs the rewrites the host hands over ("nextRewrite"
// answers with one), each on its goroutine, answering each ("rewritten").
func serveRewrites() {
	for {
		b, err := ui.CallWait("push", "nextRewrite", nil)
		if err != nil {
			if err.Error() == "stopped" {
				continue
			}
			rewriteMu.Lock()
			serving = false
			rewriteMu.Unlock()
			return
		}
		var req struct {
			Token   string  `json:"token"`
			Message Message `json:"message"`
		}
		if json.Unmarshal(b, &req) != nil {
			continue
		}
		go func() {
			out := notifyspec.Rewritten{Token: req.Token}
			m := req.Message
			if rewriteSafely(&m) == nil {
				out.OK = true
				out.Spec = m.spec()
			}
			b, _ := json.Marshal(out)
			ui.CallWait("push", "rewritten", b)
		}()
	}
}

func rewriteSafely(m *Message) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("push: the rewrite panicked")
		}
	}()
	rewriteMu.Lock()
	f := rewriter
	rewriteMu.Unlock()
	if f == nil {
		return errors.New("push: no rewrite")
	}
	return f(m)
}

// spec is the rewritten message as the notification to show.
func (m *Message) spec() notifyspec.Spec {
	s := notifyspec.Spec{
		ID: m.ID, Title: m.Title, Subtitle: m.Subtitle, Body: m.Body, Data: m.Data, URL: m.Data["url"],
		Category: m.Category, Thread: m.Thread, Summary: m.Summary, Channel: m.Channel, Badge: m.Badge, Image: m.Image,
	}
	if m.sender != nil {
		p := m.sender
		s.Sender = &notifyspec.Person{ID: p.ID, Name: p.Name, Image: p.Image, Bot: p.Bot}
		c := m.conversation
		wc := &notifyspec.Conversation{ID: c.ID, Title: c.Title, Group: c.Group, Image: c.Image}
		if wc.ID == "" {
			wc.ID = p.ID
			if wc.ID == "" {
				wc.ID = p.Name
			}
		}
		if c.Me != (notify.Person{}) {
			wc.Me = &notifyspec.Person{ID: c.Me.ID, Name: c.Me.Name, Image: c.Me.Image, Bot: c.Me.Bot}
		}
		for _, e := range c.Earlier {
			we := notifyspec.Message{Text: e.Text}
			if e.From != (notify.Person{}) {
				we.Sender = &notifyspec.Person{ID: e.From.ID, Name: e.From.Name, Image: e.From.Image, Bot: e.From.Bot}
			}
			if !e.Time.IsZero() {
				we.Time = e.Time.UnixMilli()
			}
			wc.Earlier = append(wc.Earlier, we)
		}
		s.Conversation = wc
		s.ID = wc.ID // on Android, the conversation's one notification
		if s.Thread == "" {
			s.Thread = wc.ID
		}
		if s.Title == "" {
			s.Title = p.Name
			if c.Group && c.Title != "" {
				s.Title = c.Title
			}
		}
	}
	return s
}

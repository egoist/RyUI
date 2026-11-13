// Package push is remote notifications (expo-notifications' push): the
// device's token, which the app's server sends notifications to through
// Apple's (APNs) or Google's (Firebase Cloud Messaging) service, and the
// messages that come.
//
// iOS: the app's App ID needs the Push Notifications capability for a
// release build (the profile's aps-environment); an app that imports this
// package is given the entitlement. Android: Firebase's, configured by
// the project's google-services.json, which ryui.json's
// android.googleServices names; an app that imports this package is built
// with Firebase Cloud Messaging and POST_NOTIFICATIONS.
//
// A notification shows whether or not the app runs (in the foreground
// too, as ryui/notify's Present says); tapping it opens the app, with its
// data's "url", if it has one, for ui.OnOpenURL, as ryui/notify's do.
// ryui/notify's categories, actions (OnAction), threads, channels and
// badge are a push's too; Rewrite changes a push before it shows.
//
// The web: Web Push, through the app's service worker, with the app's
// VAPID key (ryui.json's web.vapidPublicKey); ryui/push/webpush is the
// server's side (docs/web.md).
package push

import (
	"encoding/json"
	"sync"

	"github.com/egoist/ryui/notify"
	"github.com/egoist/ryui/ui"
)

// Registration is the device's address for the app's server.
type Registration struct {
	// Token is the device's token: APNs' (hex) on iOS, FCM's on Android;
	// on the web, the browser's push subscription as its JSON (endpoint
	// and keys: ryui/push/webpush sends to it). It can change (a restore,
	// a reinstall): register at each launch, and send it to the server
	// when it's not the one it has.
	Token string `json:"token"`
	// Service is "apns", "fcm" or "webpush".
	Service string `json:"service"`
	// Allowed: the user lets the app show notifications. Without it, the
	// token still takes data messages (silent pushes); on the web there's
	// no token without it (Register fails).
	Allowed bool `json:"allowed"`
}

// Register asks the user to allow notifications (the first time; after,
// it's their answer then, or what they've set in Settings since), then
// registers the device with APNs or FCM and reports its token. Not on the
// main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Register() (Registration, error) {
	var r Registration
	b, err := ui.CallWait("push", "register", nil)
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	return r, err
}

// Message is a notification that came.
type Message struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Data are the payload's own keys (iOS: the keys beside "aps";
	// Android: the message's data).
	Data map[string]string `json:"data"`
	// Subtitle, Category, Thread, Channel, Image and Badge are the
	// payload's (iOS: aps's subtitle, category, thread-id and badge;
	// Android: the notification's channel_id and image, or a data
	// message's "subtitle", "category", "thread", "channel", "image" and
	// "badge"). A rewrite (Rewrite) sets them for the notification shown,
	// as ryui/notify's Notification fields of the same names.
	Subtitle string `json:"subtitle"`
	Category string `json:"category"`
	Thread   string `json:"thread"`
	Channel  string `json:"channel"`
	Image    string `json:"image"`
	Badge    int    `json:"badge"`
	// Summary is the thread's summary text, as a Notification's.
	Summary string `json:"summary"`

	// From's.
	sender       *notify.Person
	conversation notify.Conversation
}

// From makes the message that shows a message in a conversation, as
// notify.Notification's From does: sender said m.Body in c. In a
// rewrite (Rewrite), to show a chat's push as one, with the sender's
// picture. On Android it shows in the conversation's notification (its
// ID is the conversation's), under the messages there.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func (m *Message) From(sender notify.Person, c notify.Conversation) {
	m.sender = &sender
	m.conversation = c
}

// OnReceive calls f, on the main thread, with each message that comes
// while the app runs (shown as a notification too, if it has a title or
// body), until stop.
func OnReceive(f func(Message)) (stop func()) { return received.watch(f) }

// OnOpen calls f, on the main thread, with each notification the user
// taps, until stop: the one that launched the app too, if f is set as the
// app starts (in its root component).
func OnOpen(f func(Message)) (stop func()) { return opened.watch(f) }

var (
	received = &stream{method: "nextReceived", watchers: map[*func(Message)]bool{}}
	opened   = &stream{method: "nextOpened", watchers: map[*func(Message)]bool{}}
)

// stream waits on the host for one kind of message: "next..." answers
// with the next one (the host keeps them until asked), "stop" fails the
// waiting ones with "stopped".
type stream struct {
	method   string
	mu       sync.Mutex
	watchers map[*func(Message)]bool
	running  bool
}

func (s *stream) watch(f func(Message)) (stop func()) {
	fp := &f
	s.mu.Lock()
	s.watchers[fp] = true
	start := !s.running
	s.running = true
	s.mu.Unlock()
	if start {
		go s.follow()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.watchers, fp)
			last := len(s.watchers) == 0
			s.mu.Unlock()
			if last {
				ui.Post(func() { ui.Call("push", "stop", []byte(s.method), nil) })
			}
		})
	}
}

func (s *stream) follow() {
	for {
		b, err := ui.CallWait("push", s.method, nil)
		s.mu.Lock()
		fs := make([]func(Message), 0, len(s.watchers))
		for f := range s.watchers {
			fs = append(fs, *f)
		}
		// Stopped as another began watching: wait on for that one.
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			s.running = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		var m Message
		if err != nil || json.Unmarshal(b, &m) != nil {
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f(m)
			}
		})
	}
}

package push_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/internal/notifyspec"
	"github.com/egoist/ryui/notify"
	"github.com/egoist/ryui/push"
	"github.com/egoist/ryui/ui"
)

// Register answers the host's token; messages the host keeps reach
// OnReceive and OnOpen, each its own, and the poll stops with the last
// watcher.
func TestRegisterAndMessages(t *testing.T) {
	h := fake.New()
	queues := map[string][]string{
		"nextReceived": {`{"id":"1","title":"Hi","body":"there","data":{"k":"v"}}`},
		"nextOpened":   {`{"id":"2","data":{"url":"myapp://x"}}`},
	}
	stops := 0
	h.Modules = map[string]func(string, []byte) ([]byte, error){"push": func(m string, args []byte) ([]byte, error) {
		switch m {
		case "register":
			return []byte(`{"token":"abc","service":"apns","allowed":true}`), nil
		case "stop":
			stops++
			return nil, nil
		}
		q := queues[m]
		if len(q) == 0 {
			// As a host's "stop": the poll asks again while watched.
			return nil, errors.New("stopped")
		}
		queues[m] = q[1:]
		return []byte(q[0]), nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	wait := func(what string, cond func() bool) {
		t.Helper()
		for range 5000 { // a safety net, not what passes
			h.RunPosted()
			r.Flush()
			if cond() {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("waiting for %s", what)
	}

	var reg push.Registration
	var regErr error
	done := make(chan bool, 1)
	go func() { reg, regErr = push.Register(); done <- true }()
	wait("register", func() bool { return len(done) == 1 })
	if regErr != nil || reg != (push.Registration{Token: "abc", Service: "apns", Allowed: true}) {
		t.Fatalf("register %+v %v", reg, regErr)
	}

	var got, opened []push.Message
	var stopReceive, stopOpen func()
	ui.Post(func() {
		stopReceive = push.OnReceive(func(m push.Message) { got = append(got, m) })
		stopOpen = push.OnOpen(func(m push.Message) { opened = append(opened, m) })
	})
	wait("messages", func() bool { return len(got) == 1 && len(opened) == 1 })
	if got[0].Title != "Hi" || got[0].Data["k"] != "v" || opened[0].Data["url"] != "myapp://x" {
		t.Fatalf("received %+v, opened %+v", got, opened)
	}
	stopReceive()
	stopOpen()
	wait("stop", func() bool { return stops == 2 })
}

// Rewrite gets each push the host hands over and answers with the
// notification to show; an error shows it as it came.
func TestRewrite(t *testing.T) {
	h := fake.New()
	queue := []string{
		`{"token":"r1","message":{"id":"m1","title":"New message","body":"","data":{"box":"c2VjcmV0"},"category":"message"}}`,
		`{"token":"r2","message":{"id":"m2","data":{"fail":"1"}}}`,
	}
	answers := map[string]notifyspec.Rewritten{}
	h.Modules = map[string]func(string, []byte) ([]byte, error){"push": func(m string, args []byte) ([]byte, error) {
		switch m {
		case "nextRewrite":
			if len(queue) == 0 {
				return nil, errors.New("empty")
			}
			q := queue[0]
			queue = queue[1:]
			return []byte(q), nil
		case "rewritten":
			var r notifyspec.Rewritten
			if err := json.Unmarshal(args, &r); err != nil {
				t.Error(err)
			}
			answers[r.Token] = r
		}
		return nil, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	push.Rewrite(func(m *push.Message) error {
		if m.Data["fail"] != "" {
			return errors.New("can't decrypt")
		}
		m.Body = "decrypted: " + m.Data["box"]
		m.Image = "https://example.com/p.jpg"
		m.From(notify.Person{ID: "u7", Name: "Ana"}, notify.Conversation{ID: "chat-42", Title: "Climbing", Group: true})
		return nil
	})
	for range 5000 { // a safety net, not what passes
		h.RunPosted()
		r.Flush()
		if len(answers) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	a1, a2 := answers["r1"], answers["r2"]
	if !a1.OK || a1.Spec.Body != "decrypted: c2VjcmV0" || a1.Spec.Title != "New message" || a1.Spec.Category != "message" ||
		a1.Spec.ID != "chat-42" || a1.Spec.Thread != "chat-42" || a1.Spec.Sender == nil || a1.Spec.Sender.Name != "Ana" ||
		a1.Spec.Conversation == nil || !a1.Spec.Conversation.Group || a1.Spec.Image != "https://example.com/p.jpg" || a1.Spec.Data["box"] != "c2VjcmV0" {
		t.Fatalf("r1 %+v", a1)
	}
	if a2.OK || a2.Token != "r2" {
		t.Fatalf("r2 %+v", a2)
	}
}

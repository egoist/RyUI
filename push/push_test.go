package push_test

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/ryui/host/fake"
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

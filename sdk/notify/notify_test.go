package notify_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/notifyspec"
	"github.com/egoist/asuka/sdk/notify"
	"github.com/egoist/asuka/ui"
)

type call struct {
	method string
	args   []byte
}

// mount mounts a root on a fake host whose "notify" module records each
// call, answers "next..." from queues (then "empty", which ends a
// chain), and answers the rest with answer's, or nothing.
func mount(t *testing.T, queues map[string][]string, answer func(string, []byte) []byte) (*fake.Host, *ui.Root, *[]call, func(string, func() bool)) {
	t.Helper()
	h := fake.New()
	var calls []call
	h.Modules = map[string]func(string, []byte) ([]byte, error){"notify": func(m string, args []byte) ([]byte, error) {
		calls = append(calls, call{m, args})
		if q, ok := queues[m]; ok {
			if len(q) == 0 {
				return nil, errors.New("empty")
			}
			queues[m] = q[1:]
			return []byte(q[0]), nil
		}
		if answer != nil {
			return answer(m, args), nil
		}
		return nil, nil
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
	return h, r, &calls, wait
}

func find(calls []call, method string) []call {
	var out []call
	for _, c := range calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// Show sends the whole notification, now; a message's ID, thread and
// title come from its conversation and sender.
func TestShowMessage(t *testing.T) {
	_, _, calls, wait := mount(t, nil, nil)
	done := make(chan error, 1)
	n := notify.Notification{Body: "See you at six", Category: "message", Data: map[string]string{"chat": "42"}, Image: "https://example.com/a.jpg"}.
		From(notify.Person{ID: "u7", Name: "Ana", Image: "/tmp/ana.png"},
			notify.Conversation{ID: "chat-42", Title: "Climbing", Group: true, Earlier: []notify.Message{
				{From: notify.Person{ID: "u8", Name: "Bo"}, Text: "Who's in?", Time: time.UnixMilli(1000)},
				{Text: "Me", Time: time.UnixMilli(2000)},
			}})
	go func() { done <- notify.Show(n) }()
	wait("show", func() bool { return len(done) == 1 })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cs := find(*calls, "schedule")
	if len(cs) != 1 {
		t.Fatalf("calls %v", *calls)
	}
	var s notifyspec.Spec
	if err := json.Unmarshal(cs[0].args, &s); err != nil {
		t.Fatal(err)
	}
	if s.Seconds != 0 || s.ID != "chat-42" || s.Thread != "chat-42" || s.Title != "Climbing" || s.Body != "See you at six" ||
		s.Category != "message" || s.Data["chat"] != "42" || s.Image != "https://example.com/a.jpg" {
		t.Fatalf("spec %+v", s)
	}
	if s.Sender == nil || s.Sender.ID != "u7" || s.Sender.Image != "/tmp/ana.png" {
		t.Fatalf("sender %+v", s.Sender)
	}
	c := s.Conversation
	if c == nil || !c.Group || len(c.Earlier) != 2 || c.Earlier[0].Sender.Name != "Bo" || c.Earlier[0].Time != 1000 ||
		c.Earlier[1].Sender != nil || c.Earlier[1].Text != "Me" || c.Me != nil {
		t.Fatalf("conversation %+v", c)
	}

	// Scheduled: at least a second; a 1:1 conversation is the sender's.
	go func() {
		done <- notify.Schedule(notify.Notification{Body: "hi"}.From(notify.Person{ID: "u9", Name: "Cy"}, notify.Conversation{}))
	}()
	wait("schedule", func() bool { return len(done) == 1 })
	<-done
	s = notifyspec.Spec{}
	json.Unmarshal(find(*calls, "schedule")[1].args, &s)
	if s.Seconds != 1 || s.ID != "u9" || s.Title != "Cy" || s.Conversation.ID != "u9" || s.Conversation.Group {
		t.Fatalf("spec %+v", s)
	}
}

// Categories, channels, the badge and the removals are what the host is
// sent.
func TestSettings(t *testing.T) {
	_, _, calls, wait := mount(t, nil, nil)
	ui.Post(func() {
		notify.SetCategories(notify.Category{ID: "message", Actions: []notify.Action{
			{ID: "reply", Title: "Reply", Icon: "arrowshape.turn.up.left", Reply: &notify.Reply{Button: "Send", Placeholder: "Message"}},
			{ID: "read", Title: "Mark as Read"},
			{ID: "delete", Title: "Delete", Destructive: true, Unlock: true},
			{ID: "open", Title: "Open", Open: true},
		}})
		notify.SetChannelGroups(notify.ChannelGroup{ID: "work", Name: "Work"})
		notify.SetChannels(notify.Channel{ID: "messages", Name: "Messages", Importance: notify.ImportanceHigh, Group: "work"},
			notify.Channel{ID: "digest", Name: "Digest", Importance: notify.ImportanceLow, Silent: true, NoBadge: true})
		notify.SetBadge(3)
		notify.Remove("a", "b")
		notify.RemoveThread("chat-42")
		notify.RemoveAll()
	})
	wait("calls", func() bool { return len(find(*calls, "removeAll")) == 1 })
	var cats []struct {
		ID      string
		Actions []struct {
			ID, Title, Icon           string
			Destructive, Unlock, Open bool
			Reply                     *struct{ Button, Placeholder string }
		}
	}
	json.Unmarshal(find(*calls, "categories")[0].args, &cats)
	if len(cats) != 1 || len(cats[0].Actions) != 4 || cats[0].Actions[0].Reply == nil || cats[0].Actions[0].Reply.Button != "Send" ||
		!cats[0].Actions[2].Destructive || !cats[0].Actions[2].Unlock || !cats[0].Actions[3].Open || cats[0].Actions[1].Reply != nil {
		t.Fatalf("categories %s", find(*calls, "categories")[0].args)
	}
	if got := string(find(*calls, "channels")[0].args); got != `[{"id":"messages","name":"Messages","importance":1,"group":"work"},{"id":"digest","name":"Digest","importance":2,"silent":true,"noBadge":true}]` {
		t.Fatalf("channels %s", got)
	}
	if got := string(find(*calls, "channelGroups")[0].args); got != `[{"id":"work","name":"Work"}]` {
		t.Fatalf("groups %s", got)
	}
	if got := find(*calls, "badge")[0].args; string(got) != "\x03\x00\x00\x00" {
		t.Fatalf("badge %v", got)
	}
	if got := string(find(*calls, "remove")[0].args); got != `["a","b"]` {
		t.Fatalf("remove %s", got)
	}
	if got := string(find(*calls, "removeThread")[0].args); got != "chat-42" {
		t.Fatalf("removeThread %s", got)
	}
}

// Showing decodes the host's list.
func TestShowing(t *testing.T) {
	_, _, _, wait := mount(t, nil, func(m string, _ []byte) []byte {
		if m == "showing" {
			return []byte(`[{"id":"1","title":"Ana","body":"hi","thread":"chat-42","category":"message","data":{"k":"v"},"time":5000,"remote":true}]`)
		}
		return nil
	})
	var got []notify.Delivered
	done := make(chan bool, 1)
	go func() { got, _ = notify.Showing(); done <- true }()
	wait("showing", func() bool { return len(done) == 1 })
	if len(got) != 1 || got[0].Thread != "chat-42" || got[0].Data["k"] != "v" || !got[0].Remote || !got[0].Time.Equal(time.UnixMilli(5000)) {
		t.Fatalf("showing %+v", got)
	}
}

// A response that waited reaches the watchers; it's done when they've
// returned and each Defer is done, and the chain stops with the last.
func TestOnAction(t *testing.T) {
	queues := map[string][]string{"nextAction": {
		`{"token":"t1","action":"reply","reply":"On my way","seconds":30,"notification":{"id":"chat-42","thread":"chat-42","data":{"chat":"42"}}}`,
		`{"token":"t2","action":"","seconds":30,"notification":{"id":"x","url":"myapp://x"}}`,
	}}
	_, _, calls, wait := mount(t, queues, nil)
	var got []notify.Response
	var finish func()
	var stop func()
	ui.Post(func() {
		stop = notify.OnAction(func(r notify.Response) {
			got = append(got, r)
			if r.Action == "reply" {
				ctx, done := r.Defer()
				if _, ok := ctx.Deadline(); !ok {
					t.Error("no deadline")
				}
				finish = done
			}
		})
	})
	wait("responses", func() bool { return len(got) == 2 && len(find(*calls, "actionDone")) == 1 })
	if got[0].Reply != "On my way" || got[0].Notification.Data["chat"] != "42" || got[1].Action != "" || got[1].Notification.URL != "myapp://x" {
		t.Fatalf("responses %+v", got)
	}
	// t2 is done; t1 waits for its Defer.
	if d := find(*calls, "actionDone"); string(d[0].args) != "t2" {
		t.Fatalf("done %s", d[0].args)
	}
	go func() { finish(); finish() }()
	wait("deferred", func() bool { return len(find(*calls, "actionDone")) == 2 })
	if d := find(*calls, "actionDone"); string(d[1].args) != "t1" {
		t.Fatalf("done %s", d[1].args)
	}
	ui.Post(stop)
	wait("stop", func() bool { return len(find(*calls, "stop")) == 1 })
	if s := find(*calls, "stop"); string(s[0].args) != "nextAction" {
		t.Fatalf("stop %s", s[0].args)
	}
}

// Present answers each notification the host asks about with f's
// presentation; nil tells the host to show them all.
func TestPresent(t *testing.T) {
	queues := map[string][]string{"nextPresent": {
		`{"token":"p1","notification":{"id":"1","thread":"chat-42"}}`,
		`{"token":"p2","notification":{"id":"2","thread":"chat-7"}}`,
	}}
	_, _, calls, wait := mount(t, queues, nil)
	ui.Post(func() {
		notify.Present(func(n notify.Delivered) notify.Presentation {
			if n.Thread == "chat-42" {
				return notify.Hide
			}
			return notify.Quiet
		})
	})
	wait("presented", func() bool { return len(find(*calls, "presented")) == 2 })
	p := find(*calls, "presented")
	if string(p[0].args) != "\x02p1" || string(p[1].args) != "\x01p2" {
		t.Fatalf("presented %q %q", p[0].args, p[1].args)
	}
	if on := find(*calls, "present"); len(on) != 1 || on[0].args[0] != 1 {
		t.Fatalf("present %v", on)
	}
	ui.Post(func() { notify.Present(nil) })
	wait("off", func() bool { return len(find(*calls, "present")) == 2 })
	if off := find(*calls, "present"); off[1].args[0] != 0 {
		t.Fatalf("present %v", off)
	}
}

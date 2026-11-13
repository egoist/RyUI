package follow_test

import (
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/ui"
)

// mount mounts a root on a fake host whose module m follows answers with
// f, and returns a pump that runs it until cond holds.
func mount(t *testing.T, f *follow.Fake) (dispose func(), pump func(what string, cond func() bool)) {
	h := fake.New()
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"followed": f.Answer}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	return r.Dispose, func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); { // a safety net, not what passes
			h.RunPosted()
			r.Flush()
			if cond() {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("waiting for %s; calls %v", what, f.Calls())
	}
}

// A watch that stops while its "next" waits, and whose stop is never
// answered (its root went first), doesn't hold up the next watch: that
// one follows of its own.
func TestWatchAfterUnansweredStop(t *testing.T) {
	m := &follow.Module{Name: "followed"}

	first := follow.NewFake([]byte{1})
	dispose, pump := mount(t, first)
	var got []byte
	stop := m.Watch(func(b []byte) { got = b })
	pump("the state", func() bool { return len(got) == 1 && got[0] == 1 })
	pump("a next waiting", func() bool { return first.Waiting() == 1 })
	stop()
	dispose() // before the posted stop runs: the waiting next is never answered

	second := follow.NewFake([]byte{2})
	dispose, pump = mount(t, second)
	t.Cleanup(dispose)
	got = nil
	stop = m.Watch(func(b []byte) { got = b })
	t.Cleanup(stop)
	pump("the second root's state", func() bool { return len(got) == 1 && got[0] == 2 })
	pump("a next waiting on the second", func() bool { return second.Waiting() == 1 })
	second.Set([]byte{3})
	pump("the change", func() bool { return len(got) == 1 && got[0] == 3 })
}

// Stopping and watching again at once (the same root) follows on, with
// one "next" waiting, not two.
func TestWatchAgainAtOnce(t *testing.T) {
	m := &follow.Module{Name: "followed"}
	f := follow.NewFake([]byte{1})
	dispose, pump := mount(t, f)
	t.Cleanup(dispose)
	var got []byte
	stop := m.Watch(func(b []byte) { got = b })
	pump("the state", func() bool { return len(got) == 1 && got[0] == 1 })
	pump("a next waiting", func() bool { return f.Waiting() == 1 })
	stop()
	got = nil
	stop = m.Watch(func(b []byte) { got = b })
	t.Cleanup(stop)
	pump("the state again", func() bool { return len(got) == 1 && got[0] == 1 })
	pump("one next waiting", func() bool { return f.Waiting() == 1 })
	f.Set([]byte{4})
	pump("the change", func() bool { return len(got) == 1 && got[0] == 4 })
	if n := f.Waiting(); n > 1 {
		t.Fatalf("%d nexts waiting", n)
	}
}

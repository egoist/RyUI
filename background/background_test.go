package background_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/ryui/background"
	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ui"
)

// A task starts, updates and ends through the host's module; an action
// tapped on its notification reaches its handler, and the poll for them
// stops with the last task.
func TestTask(t *testing.T) {
	h := fake.New()
	var calls []string
	var last map[string]any
	// The fake host answers at once, on the main thread: "next" gives a
	// tap if there's one, else says none came (as a host's "ended"), and
	// the poll asks again.
	var taps [][]byte
	nexts := 0
	h.Modules = map[string]func(string, []byte) ([]byte, error){"background": func(m string, args []byte) ([]byte, error) {
		if m == "next" {
			nexts++
		}
		if m != "next" {
			calls = append(calls, m)
		}
		switch m {
		case "next":
			if len(taps) == 0 {
				return nil, errEnded
			}
			b := taps[0]
			taps = taps[1:]
			return b, nil
		case "end":
		default:
			json.Unmarshal(args, &last)
		}
		return nil, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	var tapped string
	var task *background.Task
	ui.Post(func() {
		task = background.Start(background.Options{Title: "Downloading", Progress: -1,
			Actions: []background.Action{{ID: "pause", Title: "Pause"}}}, func(a string) { tapped = a })
	})
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
		t.Fatalf("waiting for %s; calls %v, %d polls", what, calls, nexts)
	}
	wait("start", func() bool { return task != nil && len(calls) > 0 })
	if last["title"] != "Downloading" || last["progress"] != -1.0 {
		t.Fatalf("start %v", last)
	}
	id := last["id"].(float64) // the task's, from its start
	ui.Post(func() { taps = append(taps, []byte(fmt.Sprintf(`{"id":%v,"action":"pause"}`, id))) })
	wait("the tap", func() bool { return tapped == "pause" })
	ui.Post(func() { task.Update(background.Options{Title: "Downloading", Text: "50%", Progress: 0.5}) })
	wait("update", func() bool { return last["text"] == "50%" })
	ui.Post(func() { task.End() })
	wait("end", func() bool {
		n := 0
		for _, c := range calls {
			if c == "end" {
				n++
			}
		}
		return n == 1
	})
}

type ended struct{}

func (ended) Error() string { return "ended" }

var errEnded error = ended{}

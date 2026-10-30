// Package background keeps the app working with it out of sight: a
// download, an upload, an export, which the user started and which takes
// minutes. Android stops a backgrounded app within minutes; a Task keeps
// it running (a foreground service, of the data sync type), shown in an
// ongoing notification with its progress and its buttons (Pause,
// Cancel), and the CPU awake. iOS gives a backgrounded app about 30
// seconds more (a background task), and shows no notification.
//
// An app that imports it is given what it needs: Android's foreground
// service and notification permissions, and the service. Google Play
// asks, in its console, what the app's data sync service is for.
package background

import (
	"encoding/json"
	"sync"

	"github.com/egoist/nui/ui"
)

// Options are what a Task's notification shows.
type Options struct {
	Title, Text string
	// Progress is how far it is, 0 to 1; negative: under way, how far
	// unknown.
	Progress float64
	// Actions are its notification's buttons (Android shows three).
	Actions []Action
}

// Action is a button of a Task's notification.
type Action struct{ ID, Title string }

// Task is work that keeps the app running until it ends.
type Task struct {
	id       uint32
	onAction func(string)
}

var (
	mu      sync.Mutex
	tasks   = map[uint32]*Task{}
	nextID  uint32
	polling bool
)

// Start starts a task: the app keeps running, with its notification
// showing o, until End. onAction (if not nil) gets the ID of an Action
// the user taps, on the main thread. Start it while the app is on
// screen, as the user asks for the work: Android doesn't let a
// background app start one. Main thread.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Start(o Options, onAction func(action string)) *Task {
	mu.Lock()
	nextID++
	t := &Task{id: nextID, onAction: onAction}
	tasks[t.id] = t
	poll := !polling
	polling = true
	mu.Unlock()
	ui.Call("background", "start", encode(t.id, o), nil)
	if poll {
		go follow()
	}
	return t
}

// Update shows o in the task's notification instead (its progress as it
// goes). Main thread.
func (t *Task) Update(o Options) {
	ui.Call("background", "update", encode(t.id, o), nil)
}

// End ends the task, and takes its notification down: the app may be
// stopped again once none is left. Main thread.
func (t *Task) End() {
	mu.Lock()
	_, live := tasks[t.id]
	delete(tasks, t.id)
	mu.Unlock()
	if live {
		b, _ := json.Marshal(map[string]any{"id": t.id})
		ui.Call("background", "end", b, nil)
	}
}

func encode(id uint32, o Options) []byte {
	type action struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	as := make([]action, len(o.Actions))
	for i, a := range o.Actions {
		as[i] = action{a.ID, a.Title}
	}
	b, _ := json.Marshal(map[string]any{"id": id, "title": o.Title, "text": o.Text, "progress": o.Progress, "actions": as})
	return b
}

// follow asks the host for the actions tapped ("next" answers with one),
// while a task lives.
func follow() {
	for {
		b, err := ui.CallWait("background", "next", nil)
		var a struct {
			ID     uint32 `json:"id"`
			Action string `json:"action"`
		}
		mu.Lock()
		// "ended": the host's last task went (as another may start).
		if len(tasks) == 0 || err != nil && err.Error() != "ended" {
			polling = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		if err != nil || json.Unmarshal(b, &a) != nil {
			continue
		}
		ui.Post(func() {
			mu.Lock()
			t := tasks[a.ID]
			mu.Unlock()
			if t != nil && t.onAction != nil {
				t.onAction(a.Action)
			}
		})
	}
}

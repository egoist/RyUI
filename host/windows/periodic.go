//go:build windows

package windows

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/egoist/ryui/internal/windows"
)

// ryui/periodic on Windows: timers while the app runs (out of sight too:
// a Windows app keeps running), as a Mac's NSBackgroundActivityScheduler
// runs them; nothing launches the app. "schedule" (string name, u32
// seconds, u8 flags: 1 network, 2 charging) and "cancel" (the name) keep
// the app's tasks in a file of the app's data, with when each last ran;
// when one is due, "next" hands Go the task (string name, u32 seconds it
// has), which it reports with "done" (string name, u8 ok). A task that
// wants power waits while the PC runs on its battery, one that wants the
// network while it has none: it's tried again a few minutes later.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["periodic"] = h.periodicModule() })
}

type periodicTask struct {
	Every    float64 `json:"every"` // seconds
	Network  bool    `json:"network"`
	Charging bool    `json:"charging"`
	Last     int64   `json:"last"` // Unix seconds
}

var periodicState struct {
	loaded  bool
	timers  map[string]*time.Timer
	due     []string
	waiting []func([]byte, error)
	running map[string]bool
}

// periodicRetry is how long a task that can't run now waits.
const periodicRetry = 5 * time.Minute

// periodicFile is where the tasks are kept: the app's data. A test
// replaces it.
var periodicFile = func(h *Host) string {
	d := os.Getenv("RYUI_DATA")
	if d == "" {
		d = filepath.Join(os.Getenv("LOCALAPPDATA"), h.appName)
	}
	return filepath.Join(d, "ryui-periodic.json")
}

func (h *Host) periodicTasks() map[string]periodicTask {
	ts := map[string]periodicTask{}
	if b, err := os.ReadFile(periodicFile(h)); err == nil {
		json.Unmarshal(b, &ts)
	}
	return ts
}

func (h *Host) setPeriodicTasks(ts map[string]periodicTask) {
	b, _ := json.Marshal(ts)
	p := periodicFile(h)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, b, 0o644)
}

func (h *Host) periodicModule() map[string]asyncMethod {
	s := &periodicState
	return map[string]asyncMethod{
		"schedule": func(args []byte, reply func([]byte, error)) {
			name, rest := readString(args)
			if name == "" || len(rest) < 5 {
				reply(nil, errors.New("bad args"))
				return
			}
			secs := binary.LittleEndian.Uint32(rest)
			flags := rest[4]
			h.periodicLoad()
			ts := h.periodicTasks()
			last := ts[name].Last
			if last == 0 {
				last = time.Now().Unix()
			}
			ts[name] = periodicTask{Every: float64(secs), Network: flags&1 != 0, Charging: flags&2 != 0, Last: last}
			h.setPeriodicTasks(ts)
			h.periodicSchedule(name, ts[name])
			reply(nil, nil)
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			name := string(args)
			ts := h.periodicTasks()
			delete(ts, name)
			h.setPeriodicTasks(ts)
			periodicUnschedule(name)
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			h.periodicLoad()
			if len(s.due) > 0 {
				name := s.due[0]
				s.due = s.due[1:]
				reply(h.periodicRun(name), nil)
				return
			}
			s.waiting = append(s.waiting, reply)
		},
		"done": func(args []byte, reply func([]byte, error)) {
			name, rest := readString(args)
			h.periodicFinished(name, len(rest) > 0 && rest[0] == 1)
			reply(nil, nil)
		},
	}
}

// periodicLoad schedules the tasks kept from the app's last runs, once.
func (h *Host) periodicLoad() {
	s := &periodicState
	if s.loaded {
		return
	}
	s.loaded = true
	for name, t := range h.periodicTasks() {
		h.periodicSchedule(name, t)
	}
}

// periodicRun is "next"'s answer: the name and the seconds it has.
func (h *Host) periodicRun(name string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	b = append(b, name...)
	budget := uint32(25)
	if h.periodicTasks()[name].Charging {
		budget = 240
	}
	return binary.LittleEndian.AppendUint32(b, budget)
}

// periodicSchedule sets the task's timer for when it's next due: its
// interval after it last ran (at once, if that's past).
func (h *Host) periodicSchedule(name string, t periodicTask) {
	every := time.Duration(max(t.Every, 60) * float64(time.Second))
	h.periodicAfter(name, time.Until(time.Unix(t.Last, 0).Add(every)))
}

func (h *Host) periodicAfter(name string, d time.Duration) {
	s := &periodicState
	if s.timers == nil {
		s.timers, s.running = map[string]*time.Timer{}, map[string]bool{}
	}
	if t := s.timers[name]; t != nil {
		t.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(max(d, time.Second), func() {
		h.Post(func() {
			if s.timers[name] == t {
				delete(s.timers, name)
				h.periodicFire(name)
			}
		})
	})
	s.timers[name] = t
}

func periodicUnschedule(name string) {
	s := &periodicState
	if t := s.timers[name]; t != nil {
		t.Stop()
		delete(s.timers, name)
	}
	for i, n := range s.due {
		if n == name {
			s.due = append(s.due[:i], s.due[i+1:]...)
			break
		}
	}
	delete(s.running, name)
}

// periodicFire is a task's turn: it goes to Go, unless what it wants
// isn't there now.
func (h *Host) periodicFire(name string) {
	s := &periodicState
	t, kept := h.periodicTasks()[name]
	switch {
	case !kept:
		return
	case t.Charging && !OnACPower(), t.Network && !h.online(), s.running[name]:
		h.periodicAfter(name, periodicRetry)
		return
	}
	s.running[name] = true
	if len(s.waiting) > 0 {
		w := s.waiting[0]
		s.waiting = s.waiting[1:]
		w(h.periodicRun(name), nil)
		return
	}
	s.due = append(s.due, name)
}

// periodicFinished is a task reported done: it's due again an interval
// after (after it ran, if it went well; else from now).
func (h *Host) periodicFinished(name string, ok bool) {
	s := &periodicState
	if !s.running[name] {
		return
	}
	delete(s.running, name)
	ts := h.periodicTasks()
	t, kept := ts[name]
	if !kept {
		return
	}
	if ok {
		t.Last = time.Now().Unix()
		ts[name] = t
		h.setPeriodicTasks(ts)
		h.periodicSchedule(name, t)
		return
	}
	h.periodicAfter(name, time.Duration(max(t.Every, 60)*float64(time.Second)))
}

// online reports whether the PC has a network (ryui/network's list,
// started for it: unknown counts as online).
func (h *Host) online() bool {
	if h.startNetwork() != nil || h.svc.net == nil {
		return true
	}
	return h.svc.net.State().Connected
}

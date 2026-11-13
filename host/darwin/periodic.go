//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
)

// ryui/periodic on a Mac: NSBackgroundActivityScheduler, which runs a
// task about every interval while the app runs (in the background too:
// a Mac app keeps running out of sight), when the system has the time
// and power for it; unlike iOS's BGTaskScheduler it doesn't launch the
// app. "schedule" (string name, u32 seconds, u8 flags: 1 network, 2
// charging) and "cancel" (the name) keep the app's tasks (in
// UserDefaults, as iOS's host does) and a scheduler for each; when one
// fires, "next" hands Go the task (string name, u32 seconds it has),
// which it reports with "done" (string name, u8 ok). A task that wants
// power waits while the Mac runs on its battery, one that wants the
// network while it has none (deferred: the scheduler tries again soon).

const periodicKey = "ryui.periodic"

type periodicTask struct {
	Every    float64 `json:"every"`
	Network  bool    `json:"network"`
	Charging bool    `json:"charging"`
	Last     float64 `json:"last"`
}

var periodicState struct {
	loaded     bool
	schedulers map[string]ID
	blocks     map[string]objc.Block
	due        []string
	waiting    []func([]byte, error)
	running    map[string]uintptr // a task's name: the scheduler's completion handler (copied)
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["periodic"] = h.periodicModule() })
}

func periodicTasks() map[string]periodicTask {
	ts := map[string]periodicTask{}
	d := Send(Send(Class("NSUserDefaults"), "standardUserDefaults"), "dataForKey:", uintptr(String(periodicKey)))
	if d != 0 {
		json.Unmarshal(Bytes(d), &ts)
	}
	return ts
}

func setPeriodicTasks(ts map[string]periodicTask) {
	b, _ := json.Marshal(ts)
	Send(Send(Class("NSUserDefaults"), "standardUserDefaults"), "setObject:forKey:", uintptr(Data(b)), uintptr(String(periodicKey)))
}

func (h *Host) periodicModule() map[string]method {
	s := &periodicState
	return map[string]method{
		"schedule": func(args []byte, reply func([]byte, error)) {
			name, rest := readString(args)
			if name == "" || len(rest) < 5 {
				reply(nil, errors.New("bad args"))
				return
			}
			secs := binary.LittleEndian.Uint32(rest)
			flags := rest[4]
			ts := periodicTasks()
			last := ts[name].Last
			if last == 0 {
				last = float64(time.Now().Unix())
			}
			ts[name] = periodicTask{Every: float64(secs), Network: flags&1 != 0, Charging: flags&2 != 0, Last: last}
			setPeriodicTasks(ts)
			h.periodicLoad()
			h.periodicSchedule(name, ts[name])
			reply(nil, nil)
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			name := string(args)
			ts := periodicTasks()
			delete(ts, name)
			setPeriodicTasks(ts)
			periodicUnschedule(name)
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			h.periodicLoad()
			if len(s.due) > 0 {
				name := s.due[0]
				s.due = s.due[1:]
				reply(periodicRun(name), nil)
				return
			}
			s.waiting = append(s.waiting, reply)
		},
		"done": func(args []byte, reply func([]byte, error)) {
			name, rest := readString(args)
			periodicFinished(name, len(rest) > 0 && rest[0] == 1)
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
	for name, t := range periodicTasks() {
		h.periodicSchedule(name, t)
	}
}

// periodicRun is "next"'s answer: the name and the seconds it has.
func periodicRun(name string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	b = append(b, name...)
	budget := uint32(25)
	if periodicTasks()[name].Charging {
		budget = 240
	}
	return binary.LittleEndian.AppendUint32(b, budget)
}

// NSBackgroundActivityResult.
const (
	activityFinished = 1
	activityDeferred = 2
)

func (h *Host) periodicSchedule(name string, t periodicTask) {
	s := &periodicState
	periodicUnschedule(name)
	if s.schedulers == nil {
		s.schedulers, s.blocks, s.running = map[string]ID{}, map[string]objc.Block{}, map[string]uintptr{}
	}
	id := GoString(Send(Send(Class("NSBundle"), "mainBundle"), "bundleIdentifier"))
	if id == "" {
		id = "dev.ryui." + h.appName
	}
	sch := Send(Send(Class("NSBackgroundActivityScheduler"), "alloc"), "initWithIdentifier:", uintptr(String(id+".periodic."+name)))
	Send(sch, "setRepeats:", 1)
	MsgSetFloat(sch, Sel("setInterval:"), max(t.Every, 60))
	MsgSetFloat(sch, Sel("setTolerance:"), max(t.Every/4, 30))
	const utility = 0x11 // NSQualityOfServiceUtility
	Send(sch, "setQualityOfService:", utility)
	// Called on a queue of its own: the handler's copied, and the task
	// goes to the main thread.
	b := NewBlock(func(_ objc.Block, completion uintptr) {
		done := uintptr(Retain(Send(ID(completion), "copy")))
		h.Post(func() { periodicFire(name, done) })
	})
	s.schedulers[name] = sch
	s.blocks[name] = b
	Send(sch, "scheduleWithBlock:", uintptr(b))
}

func periodicUnschedule(name string) {
	s := &periodicState
	if sch := s.schedulers[name]; sch != 0 {
		Send(sch, "invalidate")
		Release(sch)
		delete(s.schedulers, name)
		s.blocks[name].Release()
		delete(s.blocks, name)
	}
	for i, n := range s.due {
		if n == name {
			s.due = append(s.due[:i], s.due[i+1:]...)
			break
		}
	}
	if done := s.running[name]; done != 0 {
		delete(s.running, name)
		CallBlock(done, activityFinished)
		Release(ID(done))
	}
}

// periodicFire is a scheduler's turn: the task goes to Go, unless what it
// wants isn't there now.
func periodicFire(name string, done uintptr) {
	s := &periodicState
	t, kept := periodicTasks()[name]
	finish := func(result uintptr) {
		CallBlock(done, result)
		Release(ID(done))
	}
	switch {
	case !kept:
		finish(activityFinished)
		return
	case t.Charging && !onACPower(), t.Network && !theHost.online():
		finish(activityDeferred)
		return
	case s.running[name] != 0: // still running from the last turn
		finish(activityDeferred)
		return
	}
	s.running[name] = done
	if len(s.waiting) > 0 {
		w := s.waiting[0]
		s.waiting = s.waiting[1:]
		w(periodicRun(name), nil)
		return
	}
	s.due = append(s.due, name)
}

func periodicFinished(name string, ok bool) {
	s := &periodicState
	done := s.running[name]
	if done == 0 {
		return
	}
	delete(s.running, name)
	if ok {
		ts := periodicTasks()
		if t, kept := ts[name]; kept {
			t.Last = float64(time.Now().Unix())
			ts[name] = t
			setPeriodicTasks(ts)
		}
	}
	CallBlock(done, activityFinished)
	Release(ID(done))
}

// online reports whether the Mac has a network path (ryui/network's
// monitor, started for it: unknown yet counts as online).
func (h *Host) online() bool {
	if h.startNetwork() != nil || len(h.net.path) == 0 {
		return true
	}
	return h.net.path[0] == 1
}

var powerSources struct {
	loaded        bool
	copyInfo      func() uintptr
	providingType func(blob uintptr) uintptr
}

// onACPower reports whether the Mac runs on its power adapter (a desktop
// Mac always does).
func onACPower() bool {
	p := &powerSources
	if !p.loaded {
		p.loaded = true
		if lib := Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit"); lib != 0 {
			purego.RegisterLibFunc(&p.copyInfo, lib, "IOPSCopyPowerSourcesInfo")
			purego.RegisterLibFunc(&p.providingType, lib, "IOPSGetProvidingPowerSourceType")
		}
	}
	if p.copyInfo == nil {
		return true
	}
	blob := p.copyInfo()
	if blob == 0 {
		return true
	}
	defer CFRelease(blob)
	t := GoString(ID(p.providingType(blob)))
	return t == "" || t == "AC Power"
}

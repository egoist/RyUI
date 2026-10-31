// Package periodic runs the app's Go code now and then while it isn't on
// screen (expo-background-task): to sync, to fetch what's new, to clean
// up. The system decides when, from the interval the app asks for, the
// battery, the network and how the user uses the app; it may wait long
// past the interval, and the user can turn it off (iOS's Background App
// Refresh).
//
// iOS: BGTaskScheduler (a refresh task, a processing task for work that
// wants power), about 30 seconds for each run. Android: WorkManager's
// periodic work (every 15 minutes at least), up to 10 minutes each. An
// app that imports it is given what each needs (iOS's background modes
// and task identifiers; Android's WorkManager).
//
// When the system runs a task, the app's Go code starts without a screen
// (main runs, and the root component, which must call Handle), runs the
// task, and stops; if the user opens the app then, its screen appears.
package periodic

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/egoist/ryui/ui"
)

// Options are a task's.
type Options struct {
	// Every is how often, at least (default and minimum: 15 minutes).
	Every time.Duration
	// Network runs it only online.
	Network bool
	// Charging runs it only while charging (iOS: a processing task, which
	// may run for minutes, usually at night).
	Charging bool
}

var (
	mu       sync.Mutex
	handlers = map[string]func(context.Context) error{}
	running  bool
)

// Handle sets the code for the task name. Call it in the root component,
// for each task the app schedules: a run that finds no handler does
// nothing. Main thread.
func Handle(name string, run func(ctx context.Context) error) {
	mu.Lock()
	handlers[name] = run
	start := !running
	running = true
	mu.Unlock()
	if start {
		go serve()
	}
}

// Schedule asks the system to run the task name now and then, as o
// says; scheduling it again changes it. It holds across launches until
// Cancel. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Schedule(name string, o Options) {
	every := max(o.Every, 15*time.Minute)
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	b = append(b, name...)
	b = binary.LittleEndian.AppendUint32(b, uint32(every/time.Second))
	var flags byte
	if o.Network {
		flags |= 1
	}
	if o.Charging {
		flags |= 2
	}
	ui.Call("periodic", "schedule", append(b, flags), nil)
}

// Cancel stops the task name. Main thread.
func Cancel(name string) {
	ui.Call("periodic", "cancel", []byte(name), nil)
}

// serve runs the tasks the host hands over ("next" answers with a task's
// name and the seconds it has), reporting each one done.
func serve() {
	for {
		b, err := ui.CallWait("periodic", "next", nil)
		if err != nil {
			mu.Lock()
			running = false
			mu.Unlock()
			return
		}
		name, budget, ok := decodeRun(b)
		if !ok {
			continue
		}
		mu.Lock()
		run := handlers[name]
		mu.Unlock()
		result := byte(1)
		if run != nil {
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			if runSafely(ctx, run) != nil {
				result = 0
			}
			cancel()
		}
		done := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
		ui.CallWait("periodic", "done", append(append(done, name...), result))
	}
}

// runSafely runs a task; a panic is its failure.
func runSafely(ctx context.Context, run func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("periodic: the task panicked")
		}
	}()
	return run(ctx)
}

// decodeRun: string name, u32 seconds.
func decodeRun(b []byte) (string, time.Duration, bool) {
	if len(b) < 8 {
		return "", 0, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	if len(b) < 4+n+4 {
		return "", 0, false
	}
	secs := binary.LittleEndian.Uint32(b[4+n:])
	// A margin before the system's deadline, to report it done.
	return string(b[4 : 4+n]), max(time.Duration(secs)*time.Second-3*time.Second, time.Second), true
}

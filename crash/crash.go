// Package crash keeps reports of the app's crashes, for the app to send
// where it likes: its own server, Sentry, Crashlytics. A report is made
// of each Go panic and fatal error, on any goroutine (Go's crash output,
// runtime/debug.SetCrashOutput, from before main), of each native crash
// (iOS: MetricKit's diagnostics and uncaught Objective-C and Swift
// exceptions; Android: uncaught JVM exceptions, and the system's record
// of native crashes, ANRs and low-memory kills; macOS: MetricKit's; in a
// browser, where the web host keeps Go's crash output, uncaught
// JavaScript errors, which the page survives), and of errors the app
// records (Record). The app reads them on its next
// launch (Reports, OnReport), sends them, and deletes them (Delete).
//
//	crash.OnReport(func(r crash.Report) {
//		go func() {
//			if send(r) == nil { // the app's own upload
//				crash.Delete(r.ID)
//			}
//		}()
//	})
//	crash.Set("user", userID)
//	crash.Breadcrumb("opened the cart")
//
// What the app was doing is in each report's Context: its version, the
// device, how long it had run, whether it was in the foreground, its last
// breadcrumbs and its values. They're written to the app's data
// directory as they change, so a crash can't lose them.
//
// Nothing is uploaded: Asuka has no backend. A Report is JSON (its fields'
// tags), the frames of its threads what a crash service needs to
// symbolicate native ones (docs/device.md says how, for Sentry and
// Crashlytics).
package crash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/crashrun"
	"github.com/egoist/asuka/ops"
)

// An app that imports the package keeps its runs' records: the hosts
// start this run's as the app starts (crashrun.Start).
func init() {
	crashrun.Enabled = true
	boot.Lifecycle = append(boot.Lifecycle, func(state int) {
		switch ops.Lifecycle(state) {
		case ops.LifecycleForeground:
			crashrun.Foreground(true)
			Breadcrumb("app: foreground")
		case ops.LifecycleBackground:
			crashrun.Foreground(false)
			Breadcrumb("app: background")
		case ops.LifecycleMemoryWarning:
			Breadcrumb("app: memory warning")
		}
	})
	crashrun.OnNotify(wake)
}

// MaxBreadcrumbs is how many breadcrumbs a report keeps, the last ones
// (Sentry's default). Set it before adding any.
var MaxBreadcrumbs = 100

// MaxReports is how many reports are kept until the app deletes them:
// past it, the oldest go. Set it before reading any.
var MaxReports = 50

// Kind is what crashed.
type Kind string

const (
	// Go is a Go panic that wasn't recovered, on any goroutine, or a fatal
	// error of Go's runtime (a concurrent map write, a deadlock, running
	// out of memory, a signal in Go code).
	Go Kind = "go"
	// Exception is an uncaught exception: Objective-C's or Swift's
	// (NSException) on iOS, a JVM Throwable on Android, a JavaScript
	// error in a browser (not fatal: the page goes on).
	Exception Kind = "exception"
	// Native is a native crash: a signal (SIGSEGV, SIGABRT...), a Mach
	// exception, a Windows exception; on the desktops, a signal Go's
	// runtime reports as it arrived in native code.
	Native Kind = "native"
	// Hang is the app not answering: an ANR on Android (fatal), a hang of
	// the main thread on iOS (MetricKit's: not fatal, unless the system's
	// watchdog ended the app), a slow launch.
	Hang Kind = "hang"
	// OutOfMemory is the system ending the app for its memory (Android's
	// low-memory kill, iOS's memory limit).
	OutOfMemory Kind = "oom"
	// Resource is the app using too much CPU or writing too much to disk
	// (MetricKit's CPU and disk-write exceptions; Android's excessive
	// resource use, fatal).
	Resource Kind = "resource"
	// NonFatal is an error the app recorded (Record).
	NonFatal Kind = "nonfatal"
)

// Report is one crash, or one error the app recorded.
type Report struct {
	// ID names the report, for Delete; the same crash always has the same
	// ID, so a service can tell a report sent twice.
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Fatal: the app ended (all crashes; a hang or a resource exception it
	// survived isn't).
	Fatal bool `json:"fatal"`
	// Time is when it happened (for a MetricKit diagnostic, which doesn't
	// say, the end of the period it was in).
	Time time.Time `json:"time"`
	// Message is a line saying what happened: "panic: runtime error: index
	// out of range [5] with length 3", "NSInvalidArgumentException:
	// -[NSNull length]: unrecognized selector", "SIGSEGV".
	Message string `json:"message"`
	// Text is the whole report as the platform gives it: Go's crash
	// output, an exception's stack trace, a tombstone, an ANR's traces.
	Text string `json:"text"`
	// Threads are the stacks, read from Text, the crashed thread
	// (goroutine) first.
	Threads []Thread `json:"threads,omitempty"`
	// Context is what the app was doing.
	Context Context `json:"context"`
}

// Thread is a thread's (or a goroutine's) stack.
type Thread struct {
	Name    string  `json:"name"` // "goroutine 1 [running]", "main"
	Crashed bool    `json:"crashed,omitempty"`
	Frames  []Frame `json:"frames"` // the innermost first
}

// Frame is a call in a stack. A Go frame has its function, file and line;
// a native one its binary and address, for a service to symbolicate with
// the app's symbols (the dSYM, the native debug symbols).
type Frame struct {
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	// Binary is the image the code is in ("MyApp", "libasuka.so"), BinaryID
	// its UUID (Mach-O) or build ID (ELF).
	Binary   string `json:"binary,omitempty"`
	BinaryID string `json:"binaryId,omitempty"`
	// Address is the instruction's, Offset its distance from the start of
	// the binary (which was loaded at Address-Offset); 0 if not known.
	Address uint64 `json:"address,omitempty"`
	Offset  uint64 `json:"offset,omitempty"`
}

// Context is what the app was doing when it crashed.
type Context struct {
	AppVersion    string `json:"appVersion,omitempty"`    // asuka.json's version
	Build         string `json:"build,omitempty"`         // asuka.json's build
	Model         string `json:"model,omitempty"`         // "iPhone17,1", "Pixel 9"; on the web, the browser's system ("macOS 15.1")
	System        string `json:"system,omitempty"`        // "iOS", "Android", "macOS", "Windows", "Linux", "Web"
	SystemVersion string `json:"systemVersion,omitempty"` // "26.0", "16"; on the web, the browser ("Chrome 131")
	Arch          string `json:"arch,omitempty"`          // "arm64", "amd64"
	// Process is "app", or "share" in the app's share sheet.
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
	// Started is when the app's process started, Uptime how long it had
	// run (0: not known).
	Started time.Time     `json:"started,omitzero"`
	Uptime  time.Duration `json:"uptime,omitempty"`
	// Foreground: the app was on screen.
	Foreground bool `json:"foreground"`
	// Breadcrumbs are the last messages the app added (Breadcrumb), and
	// its lifecycle's changes; Values are the values it set (Set).
	Breadcrumbs []Crumb           `json:"breadcrumbs,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
}

// Crumb is a breadcrumb.
type Crumb struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// Breadcrumb adds msg to what reports say the app last did ("opened
// Settings", "sync: 3 changes"), with the time: a report has the last
// MaxBreadcrumbs. It's written at once, without waiting for the disk.
// Any goroutine.
func Breadcrumb(msg string) { crashrun.Breadcrumb(msg, MaxBreadcrumbs) }

// Set sets a value reports have ("user": an ID, "screen": "cart"); ""
// removes it. Any goroutine.
func Set(key, value string) { crashrun.Set(key, value, MaxBreadcrumbs) }

// Record makes a report of err, which the app handled (Kind NonFatal),
// with the calling goroutine's stack and the context: it's read with the
// crashes (Reports, OnReport), at once. Any goroutine.
func Record(err error) {
	if err == nil || !crashrun.Started() {
		return
	}
	now := time.Now()
	stack := make([]byte, 64<<10)
	for {
		n := runtime.Stack(stack, false)
		if n < len(stack) {
			stack = stack[:n]
			break
		}
		stack = make([]byte, 2*len(stack))
	}
	run, info, fg, vals, crumbs := crashrun.Snapshot()
	msg := err.Error()
	r := Report{
		ID:      "nonfatal-" + strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.Itoa(run.PID),
		Kind:    NonFatal,
		Time:    now,
		Message: fmt.Sprintf("%T: %s", err, msg),
		Text:    fmt.Sprintf("%T: %s\n\n%s", err, msg, stack),
		Threads: goThreads(string(stack)),
	}
	if len(r.Threads) > 0 {
		r.Threads[0].Crashed = true
		r.Threads[0].Frames = trimRecord(r.Threads[0].Frames)
	}
	r.Context = contextOf(run, info, fg, vals, crumbs, now)
	if err := save(crashrun.Dir(), r); err != nil {
		return
	}
	wake()
}

// trimRecord drops Record's own frames (runtime.Stack, Record) from the
// top of the stack.
func trimRecord(fs []Frame) []Frame {
	for i, f := range fs {
		if f.Function == "github.com/egoist/asuka/crash.Record" {
			return fs[i+1:]
		}
	}
	return fs
}

// Reports are the reports kept: the crashes of earlier runs (and of the
// app's share sheet), and the errors recorded, the oldest first. What the
// platform hands over late (MetricKit's diagnostics, a moment after
// launch) is in a later call; OnReport hears of it. Not on the main
// thread: it reads files.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Reports() ([]Report, error) {
	if !crashrun.Started() {
		return nil, errNotStarted
	}
	return collect(crashrun.Dir(), crashrun.ID())
}

var errNotStarted = errors.New("crash: the app's crash reports aren't kept (no host started them, or its data directory can't be written)")

// Delete forgets report id, once the app has sent it. Any goroutine.
func Delete(id string) error {
	if id == "" || filepath.Base(id) != id {
		return fmt.Errorf("crash: no report %q", id)
	}
	err := os.Remove(filepath.Join(crashrun.Dir(), "reports", id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

var (
	subMu   sync.Mutex
	subs    = map[*func(Report)]map[string]bool{} // each subscriber's delivered IDs
	polling bool
	wakeCh  = make(chan struct{}, 1)
)

// OnReport calls f, on the main thread, with each report kept, now and
// as more come (MetricKit's diagnostics, a recorded error), each once,
// until stop: where the app sends its reports. Call it once the app is
// mounted (in its root component). Main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func OnReport(f func(Report)) (stop func()) {
	fp := &f
	subMu.Lock()
	subs[fp] = map[string]bool{}
	start := !polling
	polling = true
	subMu.Unlock()
	if start {
		go deliver()
	}
	wake()
	var once sync.Once
	return func() {
		once.Do(func() {
			subMu.Lock()
			delete(subs, fp)
			subMu.Unlock()
		})
	}
}

// wake has deliver read the reports again.
func wake() {
	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

// deliver reads the reports each time it's woken (OnReport, Record, a
// host saying it wrote one: crashrun.Notify), and hands each subscriber
// those it hasn't had.
func deliver() {
	for range wakeCh {
		if !subscribed() {
			continue
		}
		rs, err := Reports()
		if err != nil {
			continue
		}
		postReports(rs)
	}
}

func subscribed() bool {
	subMu.Lock()
	defer subMu.Unlock()
	return len(subs) > 0
}

func postReports(rs []Report) {
	if !subscribed() {
		return
	}
	postMain(func() {
		subMu.Lock()
		type call struct {
			f func(Report)
			r Report
		}
		var calls []call
		for fp, seen := range subs {
			for _, r := range rs {
				if !seen[r.ID] {
					seen[r.ID] = true
					calls = append(calls, call{*fp, r})
				}
			}
		}
		subMu.Unlock()
		for _, c := range calls {
			c.f(c.r)
		}
	})
}

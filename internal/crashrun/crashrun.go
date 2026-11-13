// Package crashrun keeps the record of this run that ryui/crash reports
// from: Go's crash output (runtime/debug.SetCrashOutput) and the run's
// context, the device and the app, its breadcrumbs and values, written
// as they change so they're on disk before a crash. The hosts start it as
// the app starts, before the app's main; ryui/crash reads what earlier
// runs left.
//
// Only an app that imports ryui/crash keeps a record (Enabled): for the
// others, Start does nothing, and the hosts install nothing native.
//
// The directory (Dir) has:
//
//	runs/<run>.crash    Go's crash output: empty unless the run crashed
//	runs/<run>.ctx      the run's context, a JSON object a line (Line)
//	runs/<run>.stderr   Windows, without a console: the runtime's standard error
//	inbox/<name>.json   what a host wrote of a native crash, for ryui/crash to read
//	reports/<id>.json   the reports, as ryui/crash returns them
//
// A run holds a lock on its .crash file while it lives (flock; on Windows
// the open file), so a process reading the directory tells a run that
// ended from one still going (the app and its share sheet, at once).
package crashrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/ryui/store"
)

// Enabled is set by ryui/crash as it initializes: the app reports
// crashes.
var Enabled bool

// Info is the device and the app, as a host knows them.
type Info struct {
	Model         string `json:"model,omitempty"`         // "iPhone17,1", "Pixel 9", "MacBookPro18,3"; on the web, the browser's system ("macOS 15.1")
	System        string `json:"system,omitempty"`        // "iOS", "Android", "macOS", "Windows", "Web"
	SystemVersion string `json:"systemVersion,omitempty"` // "26.0", "16"; on the web, the browser ("Chrome 131")
	AppVersion    string `json:"appVersion,omitempty"`    // ryui.json's version
	Build         string `json:"build,omitempty"`         // ryui.json's build
}

// Run is the first line of a run's context.
type Run struct {
	PID     int       `json:"pid"`
	Start   time.Time `json:"start"`
	Process string    `json:"process"` // "app", or "share" (the app's share sheet)
	Arch    string    `json:"arch"`    // GOARCH
}

// Line is a line of a run's context: one of its fields set.
type Line struct {
	Run  *Run  `json:"run,omitempty"`
	Info *Info `json:"info,omitempty"`
	// A breadcrumb: its message, and when (Unix milliseconds).
	Crumb string `json:"b,omitempty"`
	Time  int64  `json:"t,omitempty"`
	// A value set (Value nil: deleted).
	Key   string  `json:"k,omitempty"`
	Value *string `json:"v,omitempty"`
	// Whether the app is in the foreground.
	Foreground *bool `json:"fg,omitempty"`
	// A report was made of the run, of this kind (ryui/crash, after the
	// run): what a host reports again of the same crash is left out.
	Reported string `json:"reported,omitempty"`
}

// started is when the Go runtime started: as the process did, near
// enough (on iOS and Android, as the host loads the app's library).
var started = time.Now()

var (
	mu       sync.Mutex
	on       bool
	dir      string
	run      string
	out      *os.File // the crash output, locked while the run lives
	ctx      *os.File // the context, appended to
	appended int      // lines appended since the context was last written whole
	info     Info
	head     Run
	crumbs   []Line // the last breadcrumbs, oldest first
	values   = map[string]string{}
	fg       bool
	notify   []func()
)

// Dir is the directory crash reports are kept in: crashes in the app's
// data directory (ryui/store's Dir).
func Dir() string { return filepath.Join(store.Dir(), "crashes") }

// ID is this run's: its start in Unix milliseconds and its process ID, ""
// before Start.
func ID() string {
	mu.Lock()
	defer mu.Unlock()
	return run
}

// RunID is the ID of the run of process pid that started at t.
func RunID(t time.Time, pid int) string {
	return strconv.FormatInt(t.UnixMilli(), 10) + "-" + strconv.Itoa(pid)
}

// Start begins this run's record, if the app reports crashes, as soon as
// a host can: before the app's main on iOS and Android, as Run is called
// on a desktop. i is the device and the app (a later call adds what it
// knows to an earlier one's). It returns the directory a host writes its
// native reports in (inbox), "" if the app doesn't report crashes. Any
// thread.
func Start(i Info) string {
	if !Enabled {
		return ""
	}
	mu.Lock()
	defer mu.Unlock()
	if !on {
		on = true
		begin()
	}
	merge(&info, i)
	if i != (Info{}) && ctx != nil {
		writeLine(Line{Info: &info})
	}
	if ctx == nil {
		return "" // the directory can't be written: nothing is kept
	}
	return dir
}

// begin opens the run's files and points Go's crash output at them. mu
// is held.
func begin() {
	dir = Dir()
	runs := filepath.Join(dir, "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		return
	}
	os.MkdirAll(filepath.Join(dir, "inbox"), 0o700)
	head = Run{PID: os.Getpid(), Start: started, Process: process(), Arch: runtime.GOARCH}
	run = RunID(started, head.PID)
	base := filepath.Join(runs, run)
	f, err := os.OpenFile(base+".crash", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	lock(f)
	out = f
	// Every goroutine's stack, not only the one that crashed (it only
	// raises what GOTRACEBACK says).
	debug.SetTraceback("all")
	debug.SetCrashOutput(f, debug.CrashOptions{})
	redirectStderr(base + ".stderr")
	c, err := os.OpenFile(base+".ctx", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	ctx = c
	writeLine(Line{Run: &head})
}

// process is "share" in the app's share sheet's process (an iOS share
// extension, Android's :share process), "app" otherwise.
func process() string {
	if os.Getenv("RYUI_SHARE_SHEET") == "1" || strings.Contains(os.Args[0], ".appex/") {
		return "share"
	}
	if runtime.GOOS == "android" {
		if b, err := os.ReadFile("/proc/self/cmdline"); err == nil {
			name, _, _ := strings.Cut(string(b), "\x00")
			if _, p, ok := strings.Cut(name, ":"); ok && p != "" {
				return p
			}
		}
	}
	return "app"
}

func merge(dst *Info, src Info) {
	set := func(d *string, s string) {
		if s != "" {
			*d = s
		}
	}
	set(&dst.Model, src.Model)
	set(&dst.System, src.System)
	set(&dst.SystemVersion, src.SystemVersion)
	set(&dst.AppVersion, src.AppVersion)
	set(&dst.Build, src.Build)
}

// Breadcrumb adds msg to the run's last breadcrumbs, keeping max of them.
// It's appended to the context file, a write without a sync (what's
// written survives the process), which is written whole again once it
// has twice as many lines as it needs. Any goroutine.
func Breadcrumb(msg string, max int) {
	mu.Lock()
	defer mu.Unlock()
	if !on || ctx == nil || max <= 0 {
		return
	}
	l := Line{Crumb: msg, Time: time.Now().UnixMilli()}
	crumbs = append(crumbs, l)
	if n := len(crumbs) - max; n > 0 {
		crumbs = append(crumbs[:0], crumbs[n:]...)
	}
	writeLine(l)
	compact(max)
}

// Set sets key's value in the run's context ("" deletes it). Any
// goroutine.
func Set(key, value string, max int) {
	mu.Lock()
	defer mu.Unlock()
	if !on || ctx == nil {
		return
	}
	if old, ok := values[key]; ok && old == value || !ok && value == "" {
		return
	}
	l := Line{Key: key}
	if value == "" {
		delete(values, key)
	} else {
		values[key] = value
		l.Value = &value
	}
	writeLine(l)
	compact(max)
}

// Foreground notes whether the app is in the foreground. Any goroutine.
func Foreground(v bool) {
	mu.Lock()
	defer mu.Unlock()
	if !onRun() || fg == v {
		return
	}
	fg = v
	writeLine(Line{Foreground: &fg})
}

func onRun() bool { return on && ctx != nil }

// writeLine appends l to the context. mu is held.
func writeLine(l Line) {
	b, err := json.Marshal(l)
	if err != nil {
		return
	}
	ctx.Write(append(b, '\n'))
	appended++
}

// compact writes the context whole, with only what it holds now, once
// it has twice the lines it needs: so the file stays small however many
// breadcrumbs the run adds. mu is held.
func compact(max int) {
	if appended <= 2*(max+len(values))+8 {
		return
	}
	var b []byte
	add := func(l Line) {
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	add(Line{Run: &head})
	if info != (Info{}) {
		add(Line{Info: &info})
	}
	if fg {
		add(Line{Foreground: &fg})
	}
	for k, v := range values {
		add(Line{Key: k, Value: &v})
	}
	for _, c := range crumbs {
		add(c)
	}
	path := ctx.Name()
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil || os.Rename(tmp, path) != nil {
		os.Remove(tmp)
		return
	}
	c, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	ctx.Close()
	ctx = c
	appended = len(crumbs) + len(values) + 3
}

// Snapshot is the run's context as it is now: its first line, the
// device, whether in the foreground, its values and its breadcrumbs.
func Snapshot() (r Run, i Info, foreground bool, vals map[string]string, last []Line) {
	mu.Lock()
	defer mu.Unlock()
	vals = make(map[string]string, len(values))
	for k, v := range values {
		vals[k] = v
	}
	if head.PID == 0 {
		head = Run{PID: os.Getpid(), Start: started, Process: process(), Arch: runtime.GOARCH}
	}
	return head, info, fg, vals, append([]Line(nil), crumbs...)
}

// OnNotify calls f, on any goroutine, when a host notes that it wrote a
// report (Notify).
func OnNotify(f func()) {
	mu.Lock()
	defer mu.Unlock()
	notify = append(notify, f)
}

// Notify says a host wrote a report in the inbox (MetricKit's
// diagnostics, Android's record of how the last runs ended; on mobile
// through ryui_crash_notify): ryui/crash reads it. Any goroutine.
func Notify() {
	mu.Lock()
	fs := append([]func(){}, notify...)
	mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// Started reports whether the run's record is kept.
func Started() bool {
	mu.Lock()
	defer mu.Unlock()
	return onRun()
}

// reset forgets the run (tests): the next Start begins one in d.
func reset() {
	if out != nil {
		debug.SetCrashOutput(nil, debug.CrashOptions{})
		out.Close()
	}
	if ctx != nil {
		ctx.Close()
	}
	on, run, out, ctx, appended, info, head, crumbs, fg = false, "", nil, nil, 0, Info{}, Run{}, nil, false
	values = map[string]string{}
}

// Reset forgets the run and starts the next in d (tests, in ryui/crash
// and here).
func Reset(d string) {
	mu.Lock()
	defer mu.Unlock()
	reset()
	if d != "" {
		os.Setenv("RYUI_DATA", d)
	}
}

package crash

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/ryui/internal/crashrun"
)

// An uncaught exception, as the iOS host (NSSetUncaughtExceptionHandler)
// or the Android host (Thread.setDefaultUncaughtExceptionHandler) writes
// it as the app ends, or the web host an uncaught JavaScript error, which
// the page survives: inbox/exception-<unique>.json.
type exceptionFile struct {
	Time   int64  `json:"time"` // Unix milliseconds
	PID    int    `json:"pid"`
	Thread string `json:"thread"` // the thread it was thrown on
	Name   string `json:"name"`   // NSInvalidArgumentException, java.lang.IllegalStateException
	Reason string `json:"reason"`
	Stack  string `json:"stack"` // callStackSymbols, a line each; a Throwable's stack trace, its causes too
	// Frames are the stack's return addresses, with their binaries (iOS).
	Frames []Frame `json:"frames"`
	// Threads are the other threads' stacks, as text (Android).
	Threads []struct {
		Name  string `json:"name"`
		Stack string `json:"stack"`
	} `json:"threads"`
	// Fatal: the app ended (missing: it did; the web's are false).
	Fatal *bool `json:"fatal"`
	// Run is the ID of the run it was in, when the host knows it: the
	// web's, written as the run goes on (the run may be this one).
	Run string `json:"run"`
}

func exceptionRecords(id string, b []byte) []record {
	var e exceptionFile
	if json.Unmarshal(b, &e) != nil {
		return nil
	}
	t := time.UnixMilli(e.Time)
	msg := e.Name
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	text := msg + "\n\n" + e.Stack
	crashed := Thread{Name: e.Thread, Crashed: true, Frames: e.Frames}
	if len(crashed.Frames) == 0 {
		crashed.Frames = jvmFrames(e.Stack)
	}
	threads := []Thread{crashed}
	for _, th := range e.Threads {
		text += "\n\n\"" + th.Name + "\"\n" + th.Stack
		threads = append(threads, Thread{Name: th.Name, Frames: jvmFrames(th.Stack)})
	}
	fatal := e.Fatal == nil || *e.Fatal
	return []record{{
		Report: Report{ID: id, Kind: Exception, Fatal: fatal, Time: t, Message: msg, Text: text, Threads: threads},
		pid:    e.PID, run: e.Run, exact: true,
	}}
}

// jvmFrames reads a JVM stack trace's frames ("\tat a.B.c(B.kt:12)"),
// the exception's own, before any "Caused by:".
func jvmFrames(s string) []Frame {
	var out []Frame
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "Caused by:") {
			break
		}
		if f, ok := jvmFrame(l); ok {
			out = append(out, f)
		}
	}
	return out
}

// jvmFrame reads "at a.B.c(B.kt:12)", "at a.B.c(Native Method)", and an
// ANR trace's "native: #00 pc 000000000004a1c0  /system/lib64/libc.so
// (syscall+28) (BuildId: 1a2b)".
func jvmFrame(l string) (Frame, bool) {
	if rest, ok := strings.CutPrefix(l, "at "); ok {
		fn, loc, _ := strings.Cut(rest, "(")
		loc = strings.TrimSuffix(loc, ")")
		f := Frame{Function: fn}
		if file, line, ok := strings.Cut(loc, ":"); ok {
			f.File = file
			f.Line, _ = strconv.Atoi(line)
		} else if loc != "Native Method" && loc != "Unknown Source" {
			f.File = loc
		}
		return f, true
	}
	if rest, ok := strings.CutPrefix(l, "native: "); ok {
		return nativeFrame(rest)
	}
	return Frame{}, false
}

// nativeFrame reads an Android native frame: "#00 pc 000000000004a1c0
// /data/app/.../libryui.so (main.f+12) (BuildId: 1a2b)".
func nativeFrame(l string) (Frame, bool) {
	f := strings.Fields(l)
	if len(f) < 4 || !strings.HasPrefix(f[0], "#") || f[1] != "pc" {
		return Frame{}, false
	}
	pc, err := strconv.ParseUint(f[2], 16, 64)
	if err != nil {
		return Frame{}, false
	}
	fr := Frame{Binary: f[3], Offset: pc}
	rest := strings.Join(f[4:], " ")
	if i := strings.Index(rest, "(BuildId: "); i >= 0 {
		fr.BinaryID = strings.TrimSuffix(strings.TrimSpace(rest[i+len("(BuildId: "):]), ")")
		rest = rest[:i]
	}
	if fn, ok := strings.CutPrefix(strings.TrimSpace(rest), "("); ok {
		fn = strings.TrimSuffix(fn, ")")
		if i := strings.LastIndexByte(fn, '+'); i > 0 {
			fn = fn[:i]
		}
		fr.Function = fn
	}
	return fr, true
}

// MetricKit's diagnostic payload (MXDiagnosticPayload's
// jsonRepresentation), as the iOS and macOS hosts write each they get:
// inbox/metrickit-<unique>.json.
type mxPayload struct {
	Begin   string         `json:"timeStampBegin"`
	End     string         `json:"timeStampEnd"`
	Crashes []mxDiagnostic `json:"crashDiagnostics"`
	Hangs   []mxDiagnostic `json:"hangDiagnostics"`
	CPU     []mxDiagnostic `json:"cpuExceptionDiagnostics"`
	Disk    []mxDiagnostic `json:"diskWriteExceptionDiagnostics"`
	Launch  []mxDiagnostic `json:"appLaunchDiagnostics"`
}

type mxDiagnostic struct {
	Tree mxTree `json:"callStackTree"`
	Meta mxMeta `json:"diagnosticMetaData"`
}

type mxMeta struct {
	AppVersion        string `json:"appVersion"`
	AppBuild          string `json:"appBuildVersion"`
	OSVersion         string `json:"osVersion"` // "iPhone OS 26.0 (23A341)"
	DeviceType        string `json:"deviceType"`
	PID               int    `json:"pid"` // iOS 17, macOS 14
	ExceptionType     *int   `json:"exceptionType"`
	ExceptionCode     *int   `json:"exceptionCode"`
	Signal            *int   `json:"signal"`
	TerminationReason string `json:"terminationReason"`
	VMRegion          string `json:"virtualMemoryRegionInfo"`
	ObjCException     *struct {
		Name    string `json:"exceptionName"`
		Message string `json:"composedMessage"`
		Class   string `json:"className"`
	} `json:"objectiveCexceptionReason"`
	HangDuration   string `json:"hangDuration"`
	CPUTime        string `json:"totalCPUTime"`
	SampledTime    string `json:"totalSampledTime"`
	WritesCaused   string `json:"writesCaused"`
	LaunchDuration string `json:"launchDuration"`
}

type mxTree struct {
	PerThread bool `json:"callStackPerThread"`
	Stacks    []struct {
		Attributed bool      `json:"threadAttributed"`
		Roots      []mxFrame `json:"callStackRootFrames"`
	} `json:"callStacks"`
}

type mxFrame struct {
	UUID    string    `json:"binaryUUID"`
	Offset  uint64    `json:"offsetIntoBinaryTextSegment"`
	Samples int       `json:"sampleCount"`
	Name    string    `json:"binaryName"`
	Address uint64    `json:"address"`
	Sub     []mxFrame `json:"subFrames"`
}

func metricKitRecords(id string, b []byte) []record {
	var p mxPayload
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	end := mxTime(p.End)
	if end.IsZero() {
		end = mxTime(p.Begin)
	}
	var out []record
	add := func(d mxDiagnostic, kind Kind, fatal bool, msg string) {
		rec := record{pid: d.Meta.PID}
		rec.Report = Report{ID: fmt.Sprintf("%s-%d", id, len(out)), Kind: kind, Fatal: fatal, Time: end, Message: msg}
		rec.Threads = d.Tree.threads()
		rec.Text = mxText(msg, d, rec.Threads, p)
		rec.info = crashrun.Info{Model: d.Meta.DeviceType, AppVersion: d.Meta.AppVersion, Build: d.Meta.AppBuild}
		rec.info.System, rec.info.SystemVersion = mxOS(d.Meta.OSVersion)
		out = append(out, rec)
	}
	for _, d := range p.Crashes {
		kind, msg := mxCrash(d.Meta)
		add(d, kind, true, msg)
		if kind == Exception || d.Meta.Signal != nil && *d.Meta.Signal == sigabrt {
			// The exception the app's handler reported, or the Go crash
			// that aborted (or the abort in native code Go's runtime
			// reported): the same crash.
			out[len(out)-1].unlessReported = []Kind{Exception, Go, Native}
		}
	}
	for _, d := range p.Hangs {
		add(d, Hang, false, "Hang: the main thread was busy for "+d.Meta.HangDuration)
	}
	for _, d := range p.CPU {
		add(d, Resource, false, fmt.Sprintf("CPU: %s in %s", d.Meta.CPUTime, d.Meta.SampledTime))
	}
	for _, d := range p.Disk {
		add(d, Resource, false, "Disk writes: "+d.Meta.WritesCaused)
	}
	for _, d := range p.Launch {
		add(d, Hang, false, "Slow launch: "+d.Meta.LaunchDuration)
	}
	return out
}

// mxTime reads MetricKit's "2025-08-27 17:41:00" (the device's time zone).
func mxTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// mxOS splits "iPhone OS 26.0 (23A341)" or "macOS 15.1 (24B83)" into the
// system and its version.
func mxOS(s string) (string, string) {
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	i := strings.LastIndexByte(s, ' ')
	if i < 0 {
		return "", s
	}
	sys := s[:i]
	switch sys {
	case "iPhone OS":
		sys = "iOS"
	case "Mac OS X", "Mac OS", "macOS":
		sys = "macOS"
	}
	return sys, s[i+1:]
}

var machExceptions = map[int]string{
	1: "EXC_BAD_ACCESS", 2: "EXC_BAD_INSTRUCTION", 3: "EXC_ARITHMETIC", 4: "EXC_EMULATION",
	5: "EXC_SOFTWARE", 6: "EXC_BREAKPOINT", 7: "EXC_SYSCALL", 8: "EXC_MACH_SYSCALL",
	9: "EXC_RPC_ALERT", 10: "EXC_CRASH", 11: "EXC_RESOURCE", 12: "EXC_GUARD", 13: "EXC_CORPSE_NOTIFY",
}

var signals = map[int]string{
	1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT", 7: "SIGEMT",
	8: "SIGFPE", 9: "SIGKILL", 10: "SIGBUS", 11: "SIGSEGV", 12: "SIGSYS", 13: "SIGPIPE", 14: "SIGALRM", 15: "SIGTERM",
}

// mxCrash says what a crash diagnostic was: an exception, the watchdog
// ending a hung app, the memory limit, or a signal.
func mxCrash(m mxMeta) (Kind, string) {
	if e := m.ObjCException; e != nil && (e.Name != "" || e.Message != "") {
		msg := e.Name
		if e.Message != "" {
			msg += ": " + e.Message
		}
		return Exception, msg
	}
	var what []string
	if m.ExceptionType != nil {
		name := machExceptions[*m.ExceptionType]
		if name == "" {
			name = fmt.Sprintf("exception %d", *m.ExceptionType)
		}
		what = append(what, name)
	}
	if m.Signal != nil {
		name := signals[*m.Signal]
		if name == "" {
			name = fmt.Sprintf("signal %d", *m.Signal)
		}
		if len(what) > 0 {
			name = "(" + name + ")"
		}
		what = append(what, name)
	}
	msg := strings.Join(what, " ")
	if msg == "" {
		msg = "Crash"
	}
	reason := strings.ToLower(m.TerminationReason)
	switch {
	case strings.Contains(reason, "8badf00d"):
		return Hang, "Watchdog: the app didn't answer in time (" + m.TerminationReason + ")"
	case strings.Contains(reason, "jetsam") || strings.Contains(reason, "per-process-limit") || strings.Contains(reason, "memory limit"):
		return OutOfMemory, "Memory limit: " + m.TerminationReason
	}
	return Native, msg
}

// threads reads a call stack tree: a stack each thread (a crash's), or
// samples merged into a tree (a hang's, a CPU or disk exception's),
// whose heaviest path is a stack. The root frame is the innermost.
func (t mxTree) threads() []Thread {
	var out []Thread
	for i, s := range t.Stacks {
		th := Thread{Name: fmt.Sprintf("Thread %d", i), Crashed: s.Attributed}
		if !t.PerThread {
			th.Name = fmt.Sprintf("Samples %d", i)
		}
		var at []mxFrame = s.Roots
		for len(at) > 0 {
			f := at[0]
			for _, g := range at[1:] {
				if g.Samples > f.Samples {
					f = g
				}
			}
			fr := Frame{Binary: f.Name, BinaryID: f.UUID, Address: f.Address, Offset: f.Offset}
			th.Frames = append(th.Frames, fr)
			at = f.Sub
		}
		out = append(out, th)
	}
	// The crashed thread first.
	for i, th := range out {
		if th.Crashed && i > 0 {
			copy(out[1:i+1], out[:i])
			out[0] = th
			break
		}
	}
	return out
}

// mxText is a diagnostic as text, a crash log's way: what happened, the
// stacks, and the binaries' UUIDs (for symbolicating with the dSYMs).
func mxText(msg string, d mxDiagnostic, threads []Thread, p mxPayload) string {
	var b strings.Builder
	fmt.Fprintln(&b, msg)
	m := d.Meta
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	line("App", strings.TrimSpace(m.AppVersion+" ("+m.AppBuild+")"))
	line("OS", m.OSVersion)
	line("Device", m.DeviceType)
	if m.ExceptionCode != nil {
		line("Exception code", fmt.Sprintf("%#x", *m.ExceptionCode))
	}
	line("Termination reason", m.TerminationReason)
	line("Memory region", m.VMRegion)
	if m.ObjCException != nil {
		line("Exception class", m.ObjCException.Class)
	}
	line("Period", strings.TrimSpace(p.Begin+" – "+p.End))
	images := map[string]string{}
	var order []string
	for _, th := range threads {
		b.WriteString("\n" + th.Name)
		if th.Crashed {
			b.WriteString(" Crashed")
		}
		b.WriteString(":\n")
		for i, f := range th.Frames {
			fmt.Fprintf(&b, "%-3d %-30s 0x%016x %s + %d\n", i, f.Binary, f.Address, f.Binary, f.Offset)
			if _, ok := images[f.BinaryID]; !ok && f.BinaryID != "" {
				images[f.BinaryID] = f.Binary
				order = append(order, f.BinaryID)
			}
		}
	}
	if len(order) > 0 {
		b.WriteString("\nBinary Images:\n")
		for _, id := range order {
			fmt.Fprintf(&b, "%s %s\n", id, images[id])
		}
	}
	return b.String()
}

// How an Android process ended, from ActivityManager's
// getHistoricalProcessExitReasons (Android 11 and later), as the host
// writes each it hasn't before, on the next launch:
// inbox/exit-<time>-<pid>.json, and its trace in a .trace beside it.
type exitFile struct {
	Time        int64  `json:"time"` // Unix milliseconds
	PID         int    `json:"pid"`
	Reason      int    `json:"reason"` // ApplicationExitInfo.REASON_*
	Status      int    `json:"status"`
	Importance  int    `json:"importance"`
	Description string `json:"description"`
	PSS         int64  `json:"pss"` // kB
	RSS         int64  `json:"rss"` // kB
	Process     string `json:"process"`
}

const sigabrt = 6

// ApplicationExitInfo's reasons.
const (
	exitLowMemory      = 3
	exitCrash          = 4
	exitCrashNative    = 5
	exitANR            = 6
	exitInitFailure    = 7
	exitExcessiveUsage = 9
)

func exitRecords(id string, b, trace []byte) []record {
	var e exitFile
	if json.Unmarshal(b, &e) != nil {
		return nil
	}
	rec := record{pid: e.PID, exact: true}
	fg := e.Importance > 0 && e.Importance <= 100 // IMPORTANCE_FOREGROUND
	rec.foreground = &fg
	r := Report{ID: id, Fatal: true, Time: time.UnixMilli(e.Time)}
	desc := e.Description
	switch e.Reason {
	case exitCrashNative:
		r.Kind = Native
		signal := e.Status
		if t, ok := decodeTombstone(trace); ok {
			r.Message, r.Text, r.Threads = t.message(), t.text(), t.stacks()
			signal = int(t.signal.number)
		} else {
			r.Message = "Native crash"
			if e.Status > 0 {
				if s := signals[e.Status]; s != "" {
					r.Message = s
				}
			}
			r.Text = r.Message + "\n" + desc + "\n" + string(trace)
		}
		if signal == sigabrt {
			// Go's runtime in a shared library ends a crash it reported
			// with abort: the same crash.
			rec.unlessReported = []Kind{Go}
		}
	case exitCrash:
		// The app's own handler reports the exception, with its stack:
		// this is for one it didn't see.
		r.Kind = Exception
		r.Message = firstOf(desc, "Uncaught exception")
		r.Text = r.Message
		rec.unlessReported = []Kind{Exception}
	case exitANR:
		r.Kind = Hang
		r.Message = "ANR: " + firstOf(desc, "the app didn't answer")
		r.Text = r.Message + "\n\n" + string(trace)
		r.Threads = anrThreads(string(trace))
	case exitLowMemory:
		r.Kind = OutOfMemory
		r.Message = fmt.Sprintf("Low memory: the system ended the app (%d MB in use)", e.PSS/1024)
		r.Text = r.Message + "\n" + desc
	case exitExcessiveUsage:
		r.Kind = Resource
		r.Message = "Excessive resource use: " + firstOf(desc, "the system ended the app")
		r.Text = r.Message
	case exitInitFailure:
		r.Kind = Native
		r.Message = "The app failed to start: " + firstOf(desc, "initialization failure")
		r.Text = r.Message
	default:
		return nil
	}
	rec.Report = r
	return []record{rec}
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// anrThreads reads an ANR's traces (the Java threads' dump): each thread
// a header ("main" prio=5 tid=1 Native), its frames ("at ...", "native:
// #00 pc ..."); the main thread first, which didn't answer.
func anrThreads(s string) []Thread {
	var out []Thread
	var cur *Thread
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "----- end") {
			break // the process's dump is done (others may follow)
		}
		if strings.HasPrefix(t, "\"") {
			name := t[1:]
			if i := strings.IndexByte(name, '"'); i >= 0 {
				name = name[:i]
			}
			out = append(out, Thread{Name: name})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if t == "" {
			cur = nil
			continue
		}
		if f, ok := jvmFrame(t); ok {
			cur.Frames = append(cur.Frames, f)
		}
	}
	for i, th := range out {
		if th.Name == "main" {
			out[i].Crashed = true
			if i > 0 {
				copy(out[1:i+1], out[:i])
				out[0] = th
				out[0].Crashed = true
			}
			break
		}
	}
	return out
}

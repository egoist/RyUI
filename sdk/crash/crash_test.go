package crash

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/crashrun"
	"github.com/egoist/asuka/ui"
)

// start begins a run whose record is in a directory of the test's.
func start(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	crashrun.Reset(d)
	t.Cleanup(func() { crashrun.Reset("") })
	if crashrun.Start(crashrun.Info{Model: "iPhone17,1", System: "iOS", SystemVersion: "26.0", AppVersion: "1.2", Build: "7"}) == "" {
		t.Fatal("not started")
	}
	return crashrun.Dir()
}

// The child of TestGoCrash: a run that adds a breadcrumb and a value,
// then panics on a goroutine.
func init() {
	if os.Getenv("ASUKA_CRASH_CHILD") != "1" {
		return
	}
	crashrun.Start(crashrun.Info{AppVersion: "1.2", Build: "7", System: "iOS"})
	Breadcrumb("opened the cart")
	Set("user", "42")
	crashrun.Foreground(true)
	done := make(chan bool)
	go func() {
		checkout(nil)
		done <- true
	}()
	<-done
	os.Exit(0)
}

func checkout(m map[string]int) { m["items"]++ } // a nil map: panics

// A Go panic on a goroutine ends the process; its crash output, written
// by the runtime (SetCrashOutput), is the next run's report, with every
// goroutine and the context the run wrote before.
func TestGoCrash(t *testing.T) {
	dir := start(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "ASUKA_CRASH_CHILD=1", "ASUKA_DATA="+filepath.Dir(dir))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the child didn't crash:\n%s", out)
	}
	rs, err := Reports()
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		t.Fatalf("%d reports, want 1", len(rs))
	}
	r := rs[0]
	if r.Kind != Go || !r.Fatal || r.Message != "panic: assignment to entry in nil map" {
		t.Fatalf("report %s %v %q", r.Kind, r.Fatal, r.Message)
	}
	if !strings.Contains(r.Text, "goroutine ") || !strings.Contains(r.Text, "crash.checkout") {
		t.Fatalf("text:\n%s", r.Text)
	}
	if len(r.Threads) < 2 || !r.Threads[0].Crashed || r.Threads[0].Frames[0].Function != "github.com/egoist/asuka/sdk/crash.checkout" {
		t.Fatalf("threads %+v", r.Threads)
	}
	if f := r.Threads[0].Frames[0]; !strings.HasSuffix(f.File, "crash_test.go") || f.Line == 0 {
		t.Fatalf("frame %+v", f)
	}
	c := r.Context
	if c.PID != cmd.Process.Pid || c.AppVersion != "1.2" || c.Build != "7" || !c.Foreground || c.Values["user"] != "42" || c.Process != "app" {
		t.Fatalf("context %+v", c)
	}
	if len(c.Breadcrumbs) != 1 || c.Breadcrumbs[0].Message != "opened the cart" || c.Uptime <= 0 {
		t.Fatalf("breadcrumbs %+v, uptime %v", c.Breadcrumbs, c.Uptime)
	}
	// Read once: the crash output is gone, the report kept until deleted.
	if _, err := os.Stat(filepath.Join(dir, "runs", crashrun.RunID(time.UnixMilli(c.Started.UnixMilli()), c.PID)+".crash")); err == nil {
		t.Fatal("the crash output is still there")
	}
	if rs, _ := Reports(); len(rs) != 1 || rs[0].ID != r.ID {
		t.Fatalf("again: %v", rs)
	}
	if err := Delete(r.ID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := Reports(); len(rs) != 0 {
		t.Fatalf("after Delete: %v", rs)
	}
	b, _ := json.Marshal(r)
	var back Report
	if err := json.Unmarshal(b, &back); err != nil || back.ID != r.ID || back.Context.Values["user"] != "42" {
		t.Fatalf("JSON: %v %+v", err, back)
	}
}

// A run that's still going (its crash output locked) isn't read: the
// app and its share sheet run at once.
func TestRunningNotRead(t *testing.T) {
	dir := start(t)
	id := crashrun.ID()
	crash := filepath.Join(dir, "runs", id+".crash")
	if !crashrun.Alive(crash) {
		t.Fatal("this run isn't alive")
	}
	// As another process's: the collector only skips its own run by ID.
	c := &collector{dir: dir, current: "other", runs: listRuns(dir)}
	c.goCrashes()
	if !exists(crash) {
		t.Fatal("a running run's crash output was removed")
	}
}

// Breadcrumbs are appended, a line each, and the file written whole
// again when it's twice what it needs: it stays small, with the last.
func TestBreadcrumbsTrimmed(t *testing.T) {
	dir := start(t)
	Set("screen", "home")
	for i := range 1000 {
		crashrun.Breadcrumb(fmt.Sprintf("step %d", i), 10)
	}
	Set("screen", "cart")
	path := filepath.Join(dir, "runs", crashrun.ID()+".ctx")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		lines++
	}
	f.Close()
	if lines > 2*(10+1)+8+4 {
		t.Fatalf("%d lines", lines)
	}
	rc := readContext(path)
	MaxBreadcrumbs = 10
	defer func() { MaxBreadcrumbs = 100 }()
	crumbs := lastCrumbs(rc.crumbs)
	if len(crumbs) != 10 || crumbs[9].Message != "step 999" || crumbs[0].Message != "step 990" {
		t.Fatalf("crumbs %v", crumbs)
	}
	if rc.values["screen"] != "cart" || rc.run.PID != os.Getpid() || rc.info.AppVersion != "1.2" {
		t.Fatalf("context %+v", rc)
	}
}

// writeRun makes the record of an earlier run of process pid that ended,
// with a breadcrumb.
func writeRun(t *testing.T, dir string, start time.Time, pid int, crumb string) {
	t.Helper()
	runs := filepath.Join(dir, "runs")
	os.MkdirAll(runs, 0o700)
	var b []byte
	for _, l := range []crashrun.Line{
		{Run: &crashrun.Run{PID: pid, Start: start, Process: "app", Arch: "arm64"}},
		{Info: &crashrun.Info{AppVersion: "1.1", Build: "6", Model: "iPhone16,2", System: "iOS", SystemVersion: "18.0"}},
		{Crumb: crumb, Time: start.Add(time.Second).UnixMilli()},
	} {
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	if err := os.WriteFile(filepath.Join(runs, crashrun.RunID(start, pid)+".ctx"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func inbox(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "inbox"), 0o700)
	if err := os.WriteFile(filepath.Join(dir, "inbox", name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A MetricKit payload as iOS writes it (jsonRepresentation): a crash,
// whose stack is a frame each thread down its subFrames, and a hang.
const metricKitPayload = `{
  "timeStampBegin": "2026-11-06 10:00:00",
  "timeStampEnd": "2026-11-06 10:59:00",
  "crashDiagnostics": [{
    "version": "1.0.0",
    "callStackTree": {
      "callStackPerThread": true,
      "callStacks": [
        {"threadAttributed": false, "callStackRootFrames": [
          {"binaryUUID": "11111111-1111-1111-1111-111111111111", "offsetIntoBinaryTextSegment": 100, "sampleCount": 1, "binaryName": "libsystem_kernel.dylib", "address": 7000000100}
        ]},
        {"threadAttributed": true, "callStackRootFrames": [
          {"binaryUUID": "70B89F27-1634-3580-A695-57CDB41D7743", "offsetIntoBinaryTextSegment": 165304, "sampleCount": 1, "binaryName": "Gallery", "address": 4295131576,
           "subFrames": [{"binaryUUID": "70B89F27-1634-3580-A695-57CDB41D7743", "offsetIntoBinaryTextSegment": 2000, "sampleCount": 1, "binaryName": "Gallery", "address": 4294969296}]}
        ]}
      ]
    },
    "diagnosticMetaData": {
      "appBuildVersion": "6", "appVersion": "1.1", "osVersion": "iPhone OS 18.0 (22A3354)", "deviceType": "iPhone16,2",
      "exceptionType": 1, "exceptionCode": 0, "signal": 11, "terminationReason": "Namespace SIGNAL, Code 0xb", "pid": 812
    }
  }],
  "hangDiagnostics": [{
    "callStackTree": {"callStackPerThread": false, "callStacks": [{"threadAttributed": true, "callStackRootFrames": [
      {"binaryUUID": "70B89F27-1634-3580-A695-57CDB41D7743", "offsetIntoBinaryTextSegment": 9, "sampleCount": 2, "binaryName": "Gallery", "address": 4294967305},
      {"binaryUUID": "70B89F27-1634-3580-A695-57CDB41D7743", "offsetIntoBinaryTextSegment": 50, "sampleCount": 8, "binaryName": "Gallery", "address": 4294967346}
    ]}]},
    "diagnosticMetaData": {"hangDuration": "2.5 sec", "appVersion": "1.1", "appBuildVersion": "6", "osVersion": "iPhone OS 18.0 (22A3354)", "deviceType": "iPhone16,2"}
  }]
}`

// MetricKit's diagnostics are reports with the context of the run they
// were in (by its process ID), frames a dSYM symbolicates.
func TestMetricKit(t *testing.T) {
	dir := start(t)
	began := time.Date(2026, 11, 6, 10, 30, 0, 0, time.Local)
	writeRun(t, dir, began, 812, "tapped Pay")
	writeRun(t, dir, began.Add(-time.Hour), 813, "another run")
	inbox(t, dir, "metrickit-A1.json", []byte(metricKitPayload))
	rs, err := Reports()
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 {
		t.Fatalf("%d reports: %+v", len(rs), rs)
	}
	var crashed, hang Report
	for _, r := range rs {
		switch r.Kind {
		case Native:
			crashed = r
		case Hang:
			hang = r
		}
	}
	if crashed.ID != "metrickit-A1-0" || crashed.Message != "EXC_BAD_ACCESS (SIGSEGV)" || !crashed.Fatal {
		t.Fatalf("crash %q %q", crashed.ID, crashed.Message)
	}
	th := crashed.Threads[0]
	if !th.Crashed || len(th.Frames) != 2 || th.Frames[0] != (Frame{Binary: "Gallery", BinaryID: "70B89F27-1634-3580-A695-57CDB41D7743", Address: 4295131576, Offset: 165304}) {
		t.Fatalf("crashed thread %+v", th)
	}
	if !strings.Contains(crashed.Text, "70B89F27-1634-3580-A695-57CDB41D7743 Gallery") || !strings.Contains(crashed.Text, "Namespace SIGNAL") {
		t.Fatalf("text:\n%s", crashed.Text)
	}
	c := crashed.Context
	if c.PID != 812 || len(c.Breadcrumbs) != 1 || c.Breadcrumbs[0].Message != "tapped Pay" || c.AppVersion != "1.1" || c.System != "iOS" || c.SystemVersion != "18.0" || c.Uptime != 0 {
		t.Fatalf("context %+v", c)
	}
	if hang.Fatal || hang.Message != "Hang: the main thread was busy for 2.5 sec" || hang.Threads[0].Frames[0].Offset != 50 {
		t.Fatalf("hang %+v", hang)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "inbox")); len(entries) != 0 {
		t.Fatalf("inbox left: %v", entries)
	}
}

// An uncaught exception is reported by the host's handler, with its
// stack; the crash the system reports of it after (MetricKit's SIGABRT,
// Android's REASON_CRASH) is left out.
func TestExceptionReportedOnce(t *testing.T) {
	dir := start(t)
	began := time.Date(2026, 11, 6, 10, 30, 0, 0, time.Local)
	writeRun(t, dir, began, 812, "tapped Pay")
	exc := map[string]any{
		"time": began.Add(time.Minute).UnixMilli(), "pid": 812, "thread": "main",
		"name": "NSInvalidArgumentException", "reason": "-[NSNull length]: unrecognized selector",
		"stack":  "0   CoreFoundation  0x0000000180 __exceptionPreprocess + 164\n1   Gallery  0x0000000100 main + 12",
		"frames": []map[string]any{{"binary": "Gallery", "binaryId": "70B89F27-1634-3580-A695-57CDB41D7743", "address": 4295131576, "offset": 165304, "function": "main"}},
	}
	b, _ := json.Marshal(exc)
	inbox(t, dir, "exception-E1.json", b)
	rs, _ := Reports()
	if len(rs) != 1 || rs[0].Kind != Exception || rs[0].Message != "NSInvalidArgumentException: -[NSNull length]: unrecognized selector" {
		t.Fatalf("reports %+v", rs)
	}
	if rs[0].Threads[0].Frames[0].BinaryID == "" || rs[0].Context.Uptime != time.Minute {
		t.Fatalf("exception %+v", rs[0])
	}
	// MetricKit's report of the same crash, later.
	abort := strings.Replace(metricKitPayload, `"exceptionType": 1, "exceptionCode": 0, "signal": 11`, `"exceptionType": 10, "exceptionCode": 0, "signal": 6`, 1)
	inbox(t, dir, "metrickit-A2.json", []byte(abort))
	rs, _ = Reports()
	kinds := map[Kind]int{}
	for _, r := range rs {
		kinds[r.Kind]++
	}
	if len(rs) != 2 || kinds[Exception] != 1 || kinds[Hang] != 1 {
		t.Fatalf("after MetricKit: %v", kinds)
	}
	// Even once the app has sent and deleted it.
	Delete("exception-E1")
	exit, _ := json.Marshal(map[string]any{"time": began.Add(time.Minute).UnixMilli(), "pid": 812, "reason": exitCrash, "importance": 100})
	inbox(t, dir, "exit-1-812.json", exit)
	rs, _ = Reports()
	for _, r := range rs {
		if r.Kind == Exception {
			t.Fatalf("reported again: %+v", r)
		}
	}
}

// pb builds protocol buffers for the tombstone's test.
type pb []byte

func (p pb) varint(field int, v uint64) pb {
	p = binary.AppendUvarint(p, uint64(field)<<3)
	return binary.AppendUvarint(p, v)
}

func (p pb) bytes(field int, b []byte) pb {
	p = binary.AppendUvarint(p, uint64(field)<<3|2)
	p = binary.AppendUvarint(p, uint64(len(b)))
	return append(p, b...)
}

func (p pb) str(field int, s string) pb { return p.bytes(field, []byte(s)) }

// An Android native crash's tombstone (protocol buffers) is read: the
// signal, the abort message, each thread's frames, the crashed first.
func TestNativeCrashAndroid(t *testing.T) {
	dir := start(t)
	began := time.Date(2026, 11, 6, 10, 30, 0, 0, time.Local)
	writeRun(t, dir, began, 4242, "synced")
	frame := pb(nil).varint(1, 0x4a1c0).varint(2, 0x7a00004a1c0).str(4, "main.render").varint(5, 12).
		str(6, "/data/app/~~x/dev.asuka.gallery/lib/arm64/libasuka.so").str(8, "a1b2c3")
	main := pb(nil).varint(1, 4242).str(2, "dev.asuka.gallery").bytes(4, frame)
	other := pb(nil).varint(1, 4250).str(2, "RenderThread").bytes(4, pb(nil).varint(1, 0x10).str(6, "/system/lib64/libc.so"))
	sig := pb(nil).varint(1, 11).str(2, "SIGSEGV").varint(3, 1).str(4, "SEGV_MAPERR").varint(8, 1).varint(9, 0)
	ts := pb(nil).varint(1, 1).str(2, "google/pixel:16/x:user/release-keys").varint(5, 4242).varint(6, 4242).
		str(9, "dev.asuka.gallery").bytes(10, sig).str(15, "").
		bytes(16, pb(nil).varint(1, 4250).bytes(2, other)).
		bytes(16, pb(nil).varint(1, 4242).bytes(2, main)).varint(20, 61)
	exit, _ := json.Marshal(map[string]any{"time": began.Add(time.Minute).UnixMilli(), "pid": 4242, "reason": exitCrashNative, "status": 11, "importance": 100})
	inbox(t, dir, "exit-9-4242.trace", ts)
	inbox(t, dir, "exit-9-4242.json", exit)
	rs, _ := Reports()
	if len(rs) != 1 {
		t.Fatalf("%d reports", len(rs))
	}
	r := rs[0]
	if r.Kind != Native || r.Message != "SIGSEGV (SEGV_MAPERR)" || !r.Context.Foreground || r.Context.Breadcrumbs[0].Message != "synced" {
		t.Fatalf("report %+v", r)
	}
	if len(r.Threads) != 2 || r.Threads[0].Name != "dev.asuka.gallery" || !r.Threads[0].Crashed {
		t.Fatalf("threads %+v", r.Threads)
	}
	f := r.Threads[0].Frames[0]
	if f.Function != "main.render" || f.Offset != 0x4a1c0 || f.BinaryID != "a1b2c3" || !strings.HasSuffix(f.Binary, "libasuka.so") {
		t.Fatalf("frame %+v", f)
	}
	if !strings.Contains(r.Text, "#00 pc 000000000004a1c0  /data/app/~~x/dev.asuka.gallery/lib/arm64/libasuka.so (main.render+12) (BuildId: a1b2c3)") {
		t.Fatalf("text:\n%s", r.Text)
	}
}

// Go's runtime in a shared library ends a crash it printed with abort:
// the system's record of that SIGABRT (Android's tombstone, MetricKit's
// crash) is the same crash, not another. A SIGABRT of a run with no Go
// report is one.
func TestGoAbortReportedOnce(t *testing.T) {
	dir := start(t)
	began := time.Date(2026, 11, 6, 10, 30, 0, 0, time.Local)
	writeRun(t, dir, began, 5150, "tapped Send")
	crashOut := "panic: assignment to entry in nil map\n\ngoroutine 7 [running]:\nmain.send()\n\t/app/main.go:12 +0x1c\ncreated by main.App in goroutine 1\n"
	if err := os.WriteFile(filepath.Join(dir, "runs", crashrun.RunID(began, 5150)+".crash"), []byte(crashOut), 0o600); err != nil {
		t.Fatal(err)
	}
	sig := pb(nil).varint(1, 6).str(2, "SIGABRT").varint(3, -6&0xffffffff).str(4, "SI_TKILL")
	thread := pb(nil).varint(1, 5150).str(2, "dev.asuka.chat").bytes(4, pb(nil).varint(1, 0x10).str(6, "/apex/com.android.runtime/lib64/bionic/libc.so"))
	ts := pb(nil).varint(5, 5150).varint(6, 5150).bytes(10, sig).bytes(16, pb(nil).varint(1, 5150).bytes(2, thread))
	exit, _ := json.Marshal(map[string]any{"time": began.Add(time.Minute).UnixMilli(), "pid": 5150, "reason": exitCrashNative, "status": 6, "importance": 100})
	inbox(t, dir, "exit-3-5150.trace", ts)
	inbox(t, dir, "exit-3-5150.json", exit)
	abort := strings.Replace(metricKitPayload, `"exceptionType": 1, "exceptionCode": 0, "signal": 11`, `"exceptionType": 10, "exceptionCode": 0, "signal": 6`, 1)
	abort = strings.Replace(abort, `"pid": 812`, `"pid": 5150`, -1)
	inbox(t, dir, "metrickit-G1.json", []byte(abort))
	rs, _ := Reports()
	kinds := map[Kind]int{}
	for _, r := range rs {
		kinds[r.Kind]++
	}
	if kinds[Go] != 1 || kinds[Native] != 0 {
		t.Fatalf("reports %v", kinds)
	}
	// Another run's abort, which no Go crash explains, is reported.
	other := began.Add(time.Hour)
	writeRun(t, dir, other, 5151, "opened")
	exit, _ = json.Marshal(map[string]any{"time": other.Add(time.Minute).UnixMilli(), "pid": 5151, "reason": exitCrashNative, "status": 6, "importance": 100})
	inbox(t, dir, "exit-4-5151.json", exit)
	rs, _ = Reports()
	n := 0
	for _, r := range rs {
		if r.Kind == Native && r.Context.PID == 5151 && r.Message == "SIGABRT" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the other run's abort: %+v", rs)
	}
}

// An ANR's traces give the main thread's stack, Java and native.
func TestANR(t *testing.T) {
	dir := start(t)
	trace := `
----- pid 4242 at 2026-11-06 10:31:00.000 -----
Cmd line: dev.asuka.gallery

"main" prio=5 tid=1 Sleeping
  | group="main" sCount=1
  at java.lang.Thread.sleep(Native method)
  at dev.asuka.AsukaRuntime.apply(AsukaRuntime.kt:120)
  native: #00 pc 000000000009ab3c  /apex/com.android.runtime/lib64/bionic/libc.so (nanosleep+12) (BuildId: ff00)

"RenderThread" prio=7 tid=12 Native
  at android.os.MessageQueue.nativePollOnce(Native method)

----- end 4242 -----
`
	exit, _ := json.Marshal(map[string]any{"time": time.Now().UnixMilli(), "pid": 4242, "reason": exitANR, "description": "Input dispatching timed out", "importance": 100})
	inbox(t, dir, "exit-10-4242.trace", []byte(trace))
	inbox(t, dir, "exit-10-4242.json", exit)
	rs, _ := Reports()
	if len(rs) != 1 || rs[0].Kind != Hang || !rs[0].Fatal || rs[0].Message != "ANR: Input dispatching timed out" {
		t.Fatalf("reports %+v", rs)
	}
	th := rs[0].Threads[0]
	if th.Name != "main" || !th.Crashed || len(th.Frames) != 3 {
		t.Fatalf("main %+v", th)
	}
	if th.Frames[1] != (Frame{Function: "dev.asuka.AsukaRuntime.apply", File: "AsukaRuntime.kt", Line: 120}) ||
		th.Frames[2] != (Frame{Function: "nanosleep", Binary: "/apex/com.android.runtime/lib64/bionic/libc.so", BinaryID: "ff00", Offset: 0x9ab3c}) {
		t.Fatalf("frames %+v", th.Frames)
	}
}

// A JVM exception's stack trace is read into frames, the exception's own.
func TestJVMException(t *testing.T) {
	dir := start(t)
	exc, _ := json.Marshal(map[string]any{
		"time": time.Now().UnixMilli(), "pid": 99, "thread": "main", "name": "java.lang.IllegalStateException", "reason": "no cart",
		"stack":   "java.lang.IllegalStateException: no cart\n\tat dev.app.Cart.open(Cart.kt:12)\n\tat dev.asuka.AsukaRuntime.apply(Unknown Source)\nCaused by: java.io.IOException\n\tat x.Y.z(Y.java:1)",
		"threads": []map[string]string{{"name": "asuka-crash", "stack": "\tat java.lang.Object.wait(Native Method)"}},
	})
	inbox(t, dir, "exception-J1.json", exc)
	rs, _ := Reports()
	if len(rs) != 1 || rs[0].Message != "java.lang.IllegalStateException: no cart" {
		t.Fatalf("reports %+v", rs)
	}
	fs := rs[0].Threads[0].Frames
	if len(fs) != 2 || fs[0] != (Frame{Function: "dev.app.Cart.open", File: "Cart.kt", Line: 12}) || fs[1].File != "" {
		t.Fatalf("frames %+v", fs)
	}
	if len(rs[0].Threads) != 2 || rs[0].Threads[1].Name != "asuka-crash" || rs[0].Context.PID != 99 {
		t.Fatalf("threads %+v", rs[0].Threads)
	}
}

type cartError struct{ id string }

func (e *cartError) Error() string { return "cart " + e.id + " is gone" }

// Record makes a non-fatal report at once, with the caller's stack and
// the context as it is.
func TestRecord(t *testing.T) {
	start(t)
	Breadcrumb("opened the cart")
	Set("user", "7")
	Record(&cartError{"c1"})
	Record(nil)
	rs, err := Reports()
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		t.Fatalf("%d reports", len(rs))
	}
	r := rs[0]
	if r.Kind != NonFatal || r.Fatal || r.Message != "*crash.cartError: cart c1 is gone" {
		t.Fatalf("report %+v", r)
	}
	if fn := r.Threads[0].Frames[0].Function; fn != "github.com/egoist/asuka/sdk/crash.TestRecord" {
		t.Fatalf("top frame %q", fn)
	}
	c := r.Context
	if c.PID != os.Getpid() || c.Values["user"] != "7" || c.Breadcrumbs[len(c.Breadcrumbs)-1].Message != "opened the cart" || c.AppVersion != "1.2" {
		t.Fatalf("context %+v", c)
	}
}

// Past MaxReports, the oldest go.
func TestMaxReports(t *testing.T) {
	start(t)
	MaxReports = 3
	defer func() { MaxReports = 50 }()
	for i := range 5 {
		Record(fmt.Errorf("error %d", i))
	}
	rs, _ := Reports()
	if len(rs) != 3 || !strings.HasSuffix(rs[0].Message, "error 2") {
		t.Fatalf("reports %v", rs)
	}
}

// OnReport hands the app each report on the main thread, those kept and
// those that come later (a host's, when it says so), each once.
func TestOnReport(t *testing.T) {
	dir := start(t)
	Record(errors.New("first"))
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	var got []string
	stop := OnReport(func(r Report) { got = append(got, r.Message) })
	defer stop()
	waitFor(t, h, func() bool { return len(got) == 1 })
	exc, _ := json.Marshal(map[string]any{"time": time.Now().UnixMilli(), "pid": 1, "name": "E", "reason": "late", "stack": ""})
	inbox(t, dir, "exception-L1.json", exc)
	crashrun.Notify()
	waitFor(t, h, func() bool { return len(got) == 2 })
	if got[0] != "*errors.errorString: first" || got[1] != "E: late" {
		t.Fatalf("got %q", got)
	}
	// Each once: waking again finds nothing new.
	wake()
	Record(errors.New("third"))
	waitFor(t, h, func() bool { return len(got) == 3 })
	if got[2] != "*errors.errorString: third" {
		t.Fatalf("got %q", got)
	}
}

// waitFor runs the host's posted work until cond holds.
func waitFor(t *testing.T, h *fake.Host, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("never happened")
		}
		h.RunPosted()
		time.Sleep(time.Millisecond)
	}
}

// Go's tracebacks: nested panics, GOTRACEBACK=system's headers, "created
// by" frames, a Windows exception the runtime printed.
func TestGoTraceback(t *testing.T) {
	out := `panic: first [recovered]
	panic: second

goroutine 7 gp=0xc000102000 m=4 mp=0xc000180008 [running]:
main.(*Cart).open(0xc00001, {0x10, 0x2})
	/src/app/cart.go:40 +0x1c
created by main.main in goroutine 1
	/src/app/main.go:12 +0x5c

goroutine 1 [chan receive]:
main.main()
	/src/app/main.go:14 +0x90
`
	r := goReport(out)
	if r.Message != "panic: first [recovered]" || len(r.Threads) != 2 {
		t.Fatalf("%q %+v", r.Message, r.Threads)
	}
	th := r.Threads[0]
	if th.Name != "goroutine 7 [running]" || len(th.Frames) != 2 ||
		th.Frames[0] != (Frame{Function: "main.(*Cart).open", File: "/src/app/cart.go", Line: 40}) ||
		th.Frames[1] != (Frame{Function: "main.main", File: "/src/app/main.go", Line: 12}) {
		t.Fatalf("thread %+v", th)
	}
	stderr := []byte("some log\nException 0xc0000005 0x0 0x0 0x7ffb12345678\nPC=0x7ffb12345678\n\ngoroutine 1 [syscall, locked to thread]:\nsyscall.SyscallN(0x7ffb)\n\tC:/go/src/runtime/syscall_windows.go:519 +0x46\n")
	i := windowsException(stderr)
	if i != len("some log\n") {
		t.Fatalf("at %d", i)
	}
	w := windowsReport(string(stderr[i:]))
	if w.Kind != Native || w.Message != "Exception 0xc0000005 (EXCEPTION_ACCESS_VIOLATION)" || w.Threads[0].Frames[0].Function != "syscall.SyscallN" {
		t.Fatalf("windows %+v", w)
	}
	if windowsException([]byte("Exception 0x1 in a log line\n")) >= 0 {
		t.Fatal("a log line taken for an exception")
	}
	// A signal in native code (an abort in a library called through
	// purego) is a native crash; one in Go code isn't.
	n := goReport("SIGABRT: abort\nPC=0xffffb95574b8 m=0 sigcode=18446744073709551610\nsignal arrived during cgo execution\n\ngoroutine 1 [syscall, locked to thread]:\nruntime.cgocall(0x5dea20, 0xce82699e580)\n\t/usr/local/go/src/runtime/cgocall.go:167 +0x44\n")
	if n.Kind != Native || n.Message != "SIGABRT: abort" || len(n.Threads) != 1 {
		t.Fatalf("native %+v", n)
	}
	if g := goReport("panic: runtime error: invalid memory address or nil pointer dereference\n[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x1]\n\ngoroutine 1 [running]:\nmain.main()\n\t/src/app/main.go:3 +0x1\n\nsignal arrived during cgo execution\n"); g.Kind != Go {
		t.Fatalf("go %+v", g)
	}
}

// Without a started run, Reports says why.
func TestNotStarted(t *testing.T) {
	crashrun.Reset(t.TempDir())
	t.Cleanup(func() { crashrun.Reset("") })
	if _, err := Reports(); err == nil {
		t.Fatal("no error")
	}
	Breadcrumb("nothing kept") // no panic
	Record(errors.New("x"))
}

// An uncaught JavaScript error, as the web host writes it while the page
// goes on: a report at once, not fatal, with this run's context.
func TestWebException(t *testing.T) {
	dir := start(t)
	Breadcrumb("opened the map")
	Set("screen", "map")
	exc, _ := json.Marshal(map[string]any{
		"time": time.Now().UnixMilli(), "pid": 12345, "thread": "main", "name": "TypeError", "reason": "x is undefined",
		"stack":  "    at set (http://localhost/web/natives.js:3:9)",
		"frames": []map[string]any{{"function": "set", "file": "http://localhost/web/natives.js", "line": 3}},
		"fatal":  false, "run": crashrun.ID(),
	})
	inbox(t, dir, "exception-web-W1.json", exc)
	rs, err := Reports()
	if err != nil || len(rs) != 1 {
		t.Fatalf("reports %+v, %v", rs, err)
	}
	r := rs[0]
	if r.Kind != Exception || r.Fatal || r.Message != "TypeError: x is undefined" {
		t.Fatalf("report %+v", r)
	}
	if fs := r.Threads[0].Frames; len(fs) != 1 || fs[0] != (Frame{Function: "set", File: "http://localhost/web/natives.js", Line: 3}) {
		t.Fatalf("frames %+v", fs)
	}
	c := r.Context
	if c.Values["screen"] != "map" || len(c.Breadcrumbs) != 1 || c.Breadcrumbs[0].Message != "opened the map" || c.System != "iOS" {
		t.Fatalf("context %+v", c)
	}
	// This run's files are left as they were: it goes on.
	if _, err := os.Stat(filepath.Join(dir, "runs", crashrun.ID()+".ctx")); err != nil {
		t.Fatal("this run's context is gone:", err)
	}
}

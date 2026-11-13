package crash

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/ryui/internal/crashrun"
)

// keepRuns is how long a run's context is kept once it has ended: a
// host may report its crash later (MetricKit, on a launch days after).
const keepRuns = 7 * 24 * time.Hour

// collect turns what the runs that ended and the hosts left in dir into
// reports, and returns the reports kept, the oldest first. current is
// this run's ID: its files are left alone. One process at a time.
func collect(dir, current string) ([]Report, error) {
	unlock := crashrun.Lock(filepath.Join(dir, "collect.lock"))
	defer unlock()
	reportsDir := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reportsDir, 0o700); err != nil {
		return nil, err
	}
	c := &collector{dir: dir, current: current, runs: listRuns(dir)}
	c.kept = readReports(reportsDir)
	c.goCrashes()
	c.inbox()
	c.pruneRuns()
	return c.result(), nil
}

type collector struct {
	dir, current string
	runs         []*runFile
	kept         []Report
}

// runFile is a run's files, its ID's start and PID, and its context once
// read.
type runFile struct {
	id    string
	start time.Time
	pid   int
	ctx   *runContext
}

func listRuns(dir string) []*runFile {
	ents, _ := os.ReadDir(filepath.Join(dir, "runs"))
	seen := map[string]*runFile{}
	var runs []*runFile
	for _, e := range ents {
		name := e.Name()
		id := strings.TrimSuffix(name, filepath.Ext(name))
		if seen[id] != nil || strings.HasSuffix(name, ".tmp") {
			continue
		}
		ms, pid, ok := strings.Cut(id, "-")
		t, err1 := strconv.ParseInt(ms, 10, 64)
		p, err2 := strconv.Atoi(pid)
		if !ok || err1 != nil || err2 != nil {
			continue
		}
		r := &runFile{id: id, start: time.UnixMilli(t), pid: p}
		seen[id] = r
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].start.Before(runs[j].start) })
	return runs
}

func (c *collector) path(r *runFile, ext string) string {
	return filepath.Join(c.dir, "runs", r.id+ext)
}

// context reads r's context file (once).
func (c *collector) context(r *runFile) *runContext {
	if r.ctx == nil {
		r.ctx = readContext(c.path(r, ".ctx"))
	}
	return r.ctx
}

// goCrashes makes a report of each run that ended with Go's crash output
// (or, on Windows, an exception the runtime printed), and drops the
// crash output of each that ended.
func (c *collector) goCrashes() {
	for _, r := range c.runs {
		crashPath := c.path(r, ".crash")
		if r.id == c.current {
			continue
		}
		st, err := os.Stat(crashPath)
		if err != nil || crashrun.Alive(crashPath) {
			continue
		}
		out, _ := os.ReadFile(crashPath)
		stderr, _ := os.ReadFile(c.path(r, ".stderr"))
		var rep *Report
		if len(bytes.TrimSpace(out)) > 0 {
			rep = goReport(string(out))
		} else if i := windowsException(stderr); i >= 0 {
			rep = windowsReport(string(stderr[i:]))
		}
		if rep != nil {
			rep.ID = "go-" + r.id
			rep.Time = st.ModTime()
			c.attach(rep, r)
			if c.save(*rep) == nil {
				c.mark(r, rep.Kind)
			} else {
				continue // kept for the next try
			}
		}
		os.Remove(crashPath)
		os.Remove(c.path(r, ".stderr"))
	}
}

// inbox makes reports of what the hosts wrote of native crashes.
func (c *collector) inbox() {
	in := filepath.Join(c.dir, "inbox")
	ents, _ := os.ReadDir(in)
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		path := filepath.Join(in, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var recs []record
		switch {
		case strings.HasPrefix(base, "exception-"):
			recs = exceptionRecords(base, b)
		case strings.HasPrefix(base, "metrickit-"):
			recs = metricKitRecords(base, b)
		case strings.HasPrefix(base, "exit-"):
			trace, _ := os.ReadFile(filepath.Join(in, base+".trace"))
			recs = exitRecords(base, b, trace)
		}
		ok := true
		for _, rec := range recs {
			if c.add(rec) != nil {
				ok = false
			}
		}
		if ok {
			os.Remove(path)
			os.Remove(filepath.Join(in, base+".trace"))
		}
	}
}

// record is a report made of what a host wrote, before its context: the
// process it was in, and what's left out if the run was already
// reported.
type record struct {
	Report
	pid int
	// exact: Time is when it happened (not a period's end).
	exact bool
	// unlessReported: left out if the run already has a report of one
	// of these kinds. A process dies once: the exception the app's own
	// handler reported, or the Go crash whose abort ends it, the system
	// reports again as a crash.
	unlessReported []Kind
	// foreground, if set, is what the system says of it.
	foreground *bool
	// info is the device and the app as the record says (MetricKit's).
	info crashrun.Info
	// run is the ID of the run it was in, if the host said: that run's,
	// this one's too (what the web host reports as the page goes on).
	run string
}

// add saves rec's report, with the context of the run it was in.
func (c *collector) add(rec record) error {
	r := c.match(rec.pid, rec.Time)
	if rec.run != "" {
		r = c.byID(rec.run)
	}
	if r != nil {
		for _, k := range rec.unlessReported {
			if c.context(r).reported[k] || c.pending(r, k) {
				return nil
			}
		}
	}
	rep := rec.Report
	c.attach(&rep, r)
	if !rec.exact {
		rep.Context.Uptime = 0
	}
	if rec.foreground != nil {
		rep.Context.Foreground = *rec.foreground
	}
	ctxInfo(&rep.Context, rec.info, r == nil)
	if rep.Context.PID == 0 {
		rep.Context.PID = rec.pid
	}
	if err := c.save(rep); err != nil {
		return err
	}
	if r != nil && r.id != c.current {
		c.mark(r, rep.Kind)
	}
	return nil
}

// byID is the run of an ID, this one too; nil if its files are gone.
func (c *collector) byID(id string) *runFile {
	for _, r := range c.runs {
		if r.id == id {
			return r
		}
	}
	return nil
}

// pending reports whether a report of kind from run r is kept.
func (c *collector) pending(r *runFile, kind Kind) bool {
	for _, k := range c.kept {
		if k.Kind == kind && k.Context.PID == r.pid && k.Context.Started.UnixMilli() == r.start.UnixMilli() {
			return true
		}
	}
	return false
}

// match is the run of process pid (0: not known) that was running at t:
// the last to start before it. Not this one: a host reports what ended
// before.
func (c *collector) match(pid int, t time.Time) *runFile {
	var best *runFile
	for _, r := range c.runs {
		if r.id == c.current || pid != 0 && r.pid != pid {
			continue
		}
		if !t.IsZero() && r.start.After(t.Add(time.Second)) {
			continue
		}
		if best == nil || r.start.After(best.start) {
			best = r
		}
	}
	return best
}

// attach gives rep run r's context (nil: none known).
func (c *collector) attach(rep *Report, r *runFile) {
	if r == nil {
		return
	}
	rc := c.context(r)
	ctx := Context{PID: r.pid, Started: r.start, Process: rc.run.Process, Arch: rc.run.Arch,
		Foreground: rc.foreground, Values: rc.values}
	if !rc.run.Start.IsZero() {
		ctx.Started = rc.run.Start
	}
	ctxInfo(&ctx, rc.info, true)
	ctx.Breadcrumbs = lastCrumbs(rc.crumbs)
	if !rep.Time.IsZero() && rep.Time.After(ctx.Started) {
		ctx.Uptime = rep.Time.Sub(ctx.Started)
	}
	rep.Context = ctx
}

// ctxInfo fills ctx's device and app from i: all of it if over, else
// what ctx doesn't say.
func ctxInfo(ctx *Context, i crashrun.Info, over bool) {
	set := func(d *string, s string) {
		if s != "" && (over || *d == "") {
			*d = s
		}
	}
	set(&ctx.Model, i.Model)
	set(&ctx.System, i.System)
	set(&ctx.SystemVersion, i.SystemVersion)
	set(&ctx.AppVersion, i.AppVersion)
	set(&ctx.Build, i.Build)
}

// mark notes in r's context that a report of kind was made of it.
func (c *collector) mark(r *runFile, kind Kind) {
	rc := c.context(r)
	if rc.reported[kind] {
		return
	}
	rc.reported[kind] = true
	f, err := os.OpenFile(c.path(r, ".ctx"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(crashrun.Line{Reported: string(kind)})
	f.Write(append(b, '\n'))
}

func (c *collector) save(r Report) error {
	if err := save(c.dir, r); err != nil {
		return err
	}
	for i, k := range c.kept {
		if k.ID == r.ID {
			c.kept[i] = r
			return nil
		}
	}
	c.kept = append(c.kept, r)
	return nil
}

// save writes r in dir's reports, whole or not at all.
func save(dir string, r Report) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	reports := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reports, 0o700); err != nil {
		return err
	}
	path := filepath.Join(reports, r.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readReports(dir string) []Report {
	ents, _ := os.ReadDir(dir)
	var out []Report
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r Report
		if json.Unmarshal(b, &r) != nil || r.ID == "" {
			os.Remove(filepath.Join(dir, e.Name())) // not a report: never one
			continue
		}
		out = append(out, r)
	}
	return out
}

// pruneRuns drops the contexts of runs that ended a while ago: what a
// host reports of them so late comes without one.
func (c *collector) pruneRuns() {
	now := time.Now()
	for _, r := range c.runs {
		if r.id == c.current || exists(c.path(r, ".crash")) {
			continue // running, or its crash not read yet
		}
		ctx := c.path(r, ".ctx")
		if st, err := os.Stat(ctx); err != nil || now.Sub(st.ModTime()) > keepRuns {
			os.Remove(ctx)
			os.Remove(ctx + ".tmp")
		}
	}
	// What a host left that no reader took (a record it couldn't parse).
	in := filepath.Join(c.dir, "inbox")
	ents, _ := os.ReadDir(in)
	for _, e := range ents {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > keepRuns {
			os.Remove(filepath.Join(in, e.Name()))
		}
	}
}

// result is the reports kept, the oldest first, past MaxReports the
// oldest deleted.
func (c *collector) result() []Report {
	rs := c.kept
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Time.Before(rs[j].Time) })
	if n := len(rs) - max(MaxReports, 1); n > 0 {
		for _, r := range rs[:n] {
			os.Remove(filepath.Join(c.dir, "reports", r.ID+".json"))
		}
		rs = rs[n:]
	}
	return append([]Report(nil), rs...)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runContext is a run's context file, read.
type runContext struct {
	run        crashrun.Run
	info       crashrun.Info
	foreground bool
	values     map[string]string
	crumbs     []crashrun.Line
	reported   map[Kind]bool
}

func readContext(path string) *runContext {
	rc := &runContext{reported: map[Kind]bool{}}
	f, err := os.Open(path)
	if err != nil {
		return rc
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var l crashrun.Line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // a line cut short as the process ended
		}
		switch {
		case l.Run != nil:
			rc.run = *l.Run
		case l.Info != nil:
			rc.info = *l.Info
		case l.Foreground != nil:
			rc.foreground = *l.Foreground
		case l.Key != "":
			if rc.values == nil {
				rc.values = map[string]string{}
			}
			if l.Value == nil {
				delete(rc.values, l.Key)
			} else {
				rc.values[l.Key] = *l.Value
			}
		case l.Reported != "":
			rc.reported[Kind(l.Reported)] = true
		case l.Time != 0:
			rc.crumbs = append(rc.crumbs, l)
		}
	}
	if len(rc.values) == 0 {
		rc.values = nil
	}
	return rc
}

// lastCrumbs are the last MaxBreadcrumbs of ls.
func lastCrumbs(ls []crashrun.Line) []Crumb {
	if n := len(ls) - MaxBreadcrumbs; n > 0 {
		ls = ls[n:]
	}
	var out []Crumb
	for _, l := range ls {
		out = append(out, Crumb{Time: time.UnixMilli(l.Time), Message: l.Crumb})
	}
	return out
}

// contextOf is this run's context, now (Record).
func contextOf(run crashrun.Run, info crashrun.Info, fg bool, vals map[string]string, crumbs []crashrun.Line, at time.Time) Context {
	ctx := Context{PID: run.PID, Started: run.Start, Process: run.Process, Arch: run.Arch, Foreground: fg}
	if len(vals) > 0 {
		ctx.Values = vals
	}
	ctxInfo(&ctx, info, true)
	ctx.Breadcrumbs = lastCrumbs(crumbs)
	if at.After(run.Start) {
		ctx.Uptime = at.Sub(run.Start)
	}
	return ctx
}

// Package uitest tests an app's UI in `go test`, with no device, window
// or display: it mounts the app's components on the fake host
// (host/fake), which keeps the views the core makes, and drives them as
// a user would, by what the screen shows.
//
//	func TestCounter(t *testing.T) {
//		a := uitest.Mount(t, Counter)
//		a.Tap("Add")
//		a.Tap("Add")
//		a.Expect("Count: 2")
//	}
//
// The app runs as on a phone (iOS, 390×844 points) unless options say
// otherwise. Everything runs on the test's goroutine, which is the main
// thread: a test writes signals directly (then Settle), and what the
// app's goroutines Post runs when the test settles or waits (Until,
// WaitFor). Views have frames, laid out by the core's own layout, but
// the fake host measures text by a fixed rule (8 points a character,
// 20-point lines), not with the platform's fonts: assert where things
// are relative to each other, not to the point.
//
// What the screen shows is what a user could see: the top screen of
// each navigation stack, the selected tab, the sheets, alerts and
// popovers presented, a List's rows that fit its height (the fake host
// binds them as a collection view would; ScrollTo binds further), and
// nothing hidden. Taps go to the topmost layer: an open menu, else the
// top alert, sheet or popover, else the windows. A test fails, with
// what the screen shows, when what it names isn't there or is there
// more than once.
//
// Tests that mount an app don't run in parallel (t.Parallel): the app's
// core is one per process, as on a device.
package uitest

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

var update = flag.Bool("uitest.update", false, "uitest: rewrite the snapshots MatchSnapshot compares")

// App is an app mounted for a test.
type App struct {
	// Host is the fake host the app is mounted on: its views, the ops it
	// was sent, and what no method here covers (drags, keys to a given
	// view, modules' replies).
	Host *fake.Host
	// Root is the mounted root.
	Root *ui.Root

	*session
	scope Scope // what In narrowed the screen to
}

// session is the state an App and its scopes (In) share.
type session struct {
	t       testing.TB
	timeout time.Duration
	lists   map[ops.NodeID]*listState
	sheets  map[ops.NodeID]bool // the sheets whose environment was reported
	menu    *openMenu
	focused *fake.View
	clock   time.Duration // the last frame's time
}

// Mount mounts root as the app's screen for the test, settled: what it
// shows, its lists bound. When the test ends, it unmounts it and fails
// the test if a native view outlives it (an owner left undisposed).
func Mount(t testing.TB, root func() ui.Node, opts ...Option) *App {
	t.Helper()
	c := config{timeout: 10 * time.Second}
	for _, o := range opts {
		o(&c)
	}
	h := fake.New()
	h.Env.Platform = ops.PlatformIOS
	if c.platform != ops.PlatformUnknown && c.platform != ops.PlatformIOS {
		h.Env.Platform = c.platform
		if c.platform != ops.PlatformAndroid { // a desktop window
			h.Env.Width, h.Env.Height = 800, 600
			h.Env.SafeTop, h.Env.SafeBottom = 0, 0
			h.Env.Scale = 2
		} else {
			h.Env.Width, h.Env.Height = 412, 915 // a Pixel's, in dp
			h.Env.SafeTop, h.Env.SafeBottom = 24, 24
			h.Env.Scale = 2.625
		}
	}
	for _, f := range c.env {
		f(&h.Env)
	}
	for _, f := range c.setup {
		f(h)
	}
	a := &App{Host: h, session: &session{
		t: t, timeout: c.timeout,
		lists: map[ops.NodeID]*listState{}, sheets: map[ops.NodeID]bool{},
	}}
	a.Root = ui.Mount(h, root)
	t.Cleanup(func() { a.unmount(c.languages) })
	a.Settle()
	return a
}

// unmount disposes the root and checks that every view the core made
// went with it.
func (a *App) unmount(languages bool) {
	a.Root.Dispose()
	a.Host.RunPosted()
	var left []*fake.View
	for _, v := range a.Host.Views {
		if v.ID&ops.HostIDBit == 0 {
			left = append(left, v)
		}
	}
	if len(left) > 0 {
		slices.SortFunc(left, func(x, y *fake.View) int { return int(x.ID) - int(y.ID) })
		var b strings.Builder
		for _, v := range left {
			fmt.Fprintf(&b, "  %s %d %q\n", v.Kind, v.ID, v.Text())
		}
		a.t.Errorf("uitest: %d views outlived the app (an owner left undisposed):\n%s", len(left), b.String())
	}
	if languages {
		// The app's language follows the next host's again, as
		// ui's own tests leave it.
		ui.SetLocale("")
		ui.Mount(fake.New(), func() ui.Node { return ui.Column(ui.Style{}) }).Dispose()
	}
}

// Option configures Mount.
type Option func(*config)

type config struct {
	platform  ops.Platform
	env       []func(*ui.Environment)
	setup     []func(*fake.Host)
	timeout   time.Duration
	languages bool
}

// Platform runs the app as on p: ui.PlatformAndroid on a Pixel's screen
// (412×915), a desktop platform in an 800×600 window. iOS is the
// default, on an iPhone's screen (390×844).
func Platform(p ui.PlatformKind) Option { return func(c *config) { c.platform = p } }

// Size sets the window's size, in points.
func Size(width, height float32) Option {
	return Environment(func(e *ui.Environment) { e.Width, e.Height = width, height })
}

// Dark runs the app in dark mode.
func Dark() Option { return Environment(func(e *ui.Environment) { e.Dark = true }) }

// FontScale sets the user's text size (Dynamic Type, Android's font
// scale): 1 is the default.
func FontScale(s float32) Option {
	return Environment(func(e *ui.Environment) { e.FontScale = s })
}

// Languages sets the languages the user reads, most preferred first
// (BCP 47 tags joined by commas: "ar", "de-CH,en"), and the locale of
// their regional formats, the first's: the app shows its catalog's
// translation, right to left for a language written so.
func Languages(tags string) Option {
	return func(c *config) {
		c.languages = true
		c.env = append(c.env, func(e *ui.Environment) {
			e.Languages = tags
			e.Locale, _, _ = strings.Cut(tags, ",")
		})
	}
}

// Environment changes the environment the app starts in (after
// Platform's): the safe area, a screen reader, Reduce Motion, the time
// zone...
func Environment(f func(*ui.Environment)) Option {
	return func(c *config) { c.env = append(c.env, f) }
}

// Setup prepares the fake host before the app mounts: its Modules
// (what a native package's calls answer), its MeasureFunc.
func Setup(f func(h *fake.Host)) Option {
	return func(c *config) { c.setup = append(c.setup, f) }
}

// Timeout bounds how long Until and WaitFor wait for the app's
// goroutines (10 seconds by default). Nothing else waits.
func Timeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// Settle runs what the app's goroutines posted, flushes what the test
// wrote to signals, binds the rows lists show, and repeats until
// nothing changes. Every action settles on its own; call it after
// writing a signal from the test.
func (a *App) Settle() {
	a.t.Helper()
	for range 1000 {
		a.Host.RunPosted()
		a.Root.Flush()
		changed := a.reportSheets()
		changed = a.syncLists() || changed
		if !changed && a.Host.Pending() == 0 {
			a.dropStale()
			return
		}
	}
	a.t.Fatalf("uitest: the app never settles (it posts or rebinds forever)")
}

// dropStale forgets a menu or field whose view is gone.
func (a *App) dropStale() {
	if a.menu != nil && a.Host.Views[a.menu.v.ID] != a.menu.v {
		a.menu = nil
	}
	if a.focused != nil && a.Host.Views[a.focused.ID] != a.focused {
		a.focused = nil
	}
}

// reportSheets reports each newly presented sheet's environment, as a
// phone's host sizes it, so its content lays out.
func (a *App) reportSheets() bool {
	p := a.Host.Env.Platform
	if p != ops.PlatformIOS && p != ops.PlatformAndroid {
		return false
	}
	changed := false
	for id := range a.sheets {
		if a.Host.Views[id] == nil {
			delete(a.sheets, id)
		}
	}
	for _, v := range a.Host.Presented() {
		if v.Kind == ops.KindSheet && !a.sheets[v.ID] {
			a.sheets[v.ID] = true
			a.Host.ReportSheet(v)
			changed = true
		}
	}
	return changed
}

// Until settles and waits for the app's goroutines, posting, until cond
// holds; it fails the test if it doesn't within the timeout. cond runs
// on the main thread.
func (a *App) Until(what string, cond func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(a.timeout)
	for {
		a.Settle()
		if cond() {
			return
		}
		left := time.Until(deadline)
		if left <= 0 || !a.Host.WaitPost(left) {
			a.Settle()
			if cond() {
				return
			}
			a.t.Fatalf("uitest: waited %v for %s\n%s", a.timeout, what, a.screenFor())
		}
	}
}

// WaitFor waits until the screen shows text (see Until).
func (a *App) WaitFor(text string) {
	a.t.Helper()
	a.Until(fmt.Sprintf("%q to show", text), func() bool { return a.Has(text) })
}

// WaitGone waits until the screen no longer shows text (see Until).
func (a *App) WaitGone(text string) {
	a.t.Helper()
	a.Until(fmt.Sprintf("%q to go", text), func() bool { return !a.Has(text) })
}

// Frames runs n frames of the display, 1/60 s apart: what ui.OnFrame
// animates frame by frame advances (the clock starts at 0).
func (a *App) Frames(n int) {
	a.t.Helper()
	r := a.Host.Root()
	for range n {
		a.clock += time.Second / 60
		a.Host.Dispatch(ops.Event{ID: r.ID, Kind: ops.EventFrame, Float: a.clock.Seconds()})
	}
	a.Settle()
}

// SetEnvironment changes the environment as the system does (a
// rotation, dark mode, the text size) and settles.
func (a *App) SetEnvironment(f func(*ui.Environment)) {
	a.t.Helper()
	e := a.Host.Env
	f(&e)
	a.Host.SetEnvironment(e)
	clear(a.sheets) // reported again, at the new size
	a.Settle()
}

// OpenURL opens the app with a URL (a link, its scheme's), as the
// system does: ui.OnOpenURL gets it.
func (a *App) OpenURL(u string) {
	a.t.Helper()
	a.Host.OpenURL(u)
	a.Settle()
}

// Snapshot describes what the screen shows: each layer's views, with
// their kinds, frames, texts and states, one a line, indented by depth.
// MatchSnapshot compares it with a file.
func (a *App) Snapshot() string {
	var b strings.Builder
	for i, l := range a.layers() {
		if i > 0 {
			b.WriteByte('\n')
		}
		a.describe(&b, l, 0)
	}
	if a.menu != nil {
		b.WriteString("\nMenu\n")
		for _, it := range a.menu.items() {
			fmt.Fprintf(&b, "  %s%s\n", strings.Repeat("  ", it.depth), describeItem(it.ActionItem))
		}
	}
	return b.String()
}

// MatchSnapshot compares Snapshot with testdata/<test>/<name>.txt
// (subtests in directories of their own), failing on a difference; with
// -uitest.update, it writes the file instead.
func (a *App) MatchSnapshot(name string) {
	a.t.Helper()
	got := a.Snapshot()
	path := filepath.Join("testdata", filepath.FromSlash(a.t.Name()), name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			a.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			a.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		a.t.Fatalf("uitest: %v (go test -uitest.update writes it)", err)
	}
	if string(want) != got {
		a.t.Errorf("uitest: the screen differs from %s (go test -uitest.update rewrites it):\n%s", path, lineDiff(string(want), got))
	}
}

// lineDiff shows the lines of want and got that differ, from the first
// that does.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	i := 0
	for i < len(w) && i < len(g) && w[i] == g[i] {
		i++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "from line %d:\n", i+1)
	for j := i; j < len(w) && j < i+20; j++ {
		fmt.Fprintf(&b, "- %s\n", w[j])
	}
	for j := i; j < len(g) && j < i+20; j++ {
		fmt.Fprintf(&b, "+ %s\n", g[j])
	}
	return b.String()
}

// screenFor is what the screen shows, for a failure's message.
func (a *App) screenFor() string {
	s := a.Snapshot()
	return "the screen:\n" + s
}

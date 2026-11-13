//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/sdk/appearance"
	"github.com/egoist/asuka/sdk/power"
	"github.com/egoist/asuka/sdk/screen"
	"github.com/egoist/asuka/ui"
)

// settle runs the main run loop a moment: what's posted lands.
func settle() {
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for range 20 {
		until := MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSinceNow:"), 0.01)
		if ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1); ev != 0 {
			Send(app, "sendEvent:", uintptr(ev))
		}
	}
}

// next starts a "next" and reports its answer once it comes.
func next(h *Host, module string, last []byte) func() (b []byte, err error, done bool) {
	var b []byte
	var err error
	done := false
	h.module(module)["next"](last, func(r []byte, e error) { b, err, done = r, e, true })
	return func() ([]byte, error, bool) { return b, err, done }
}

// The displays are NSScreen's, the primary first, in points from its top
// left; "next" waits while they're the ones given, and answers once
// AppKit says they changed into others.
func TestDisplaysModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		b, err := await(h, "displays", "current", nil)
		var ds []display
		if err != nil || json.Unmarshal(b, &ds) != nil || len(ds) == 0 {
			fatalf(t, "displays.current: %s, %v", b, err)
		}
		t.Logf("displays: %s", b)
		p := ds[0]
		if !p.Primary || p.Bounds.X != 0 || p.Bounds.Y != 0 || p.Bounds.Width <= 0 || p.Scale < 1 || p.ID == "" {
			fatalf(t, "the primary display: %+v", p)
		}
		// The menu bar is over the work area's top.
		if w := p.WorkArea; w.Y < p.Bounds.Y || w.Y+w.Height > p.Bounds.Y+p.Bounds.Height || w.Width > p.Bounds.Width {
			fatalf(t, "work area %+v of %+v", w, p.Bounds)
		}
		for _, d := range ds[1:] {
			if d.Primary {
				fatalf(t, "two primaries: %s", b)
			}
		}

		// The screens as read (the Dock may move the work area meanwhile).
		read := displaysWatch.read
		defer func() { displaysWatch.read = read }()
		now := b
		displaysWatch.read = func() []byte { return now }
		post := func() {
			Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "postNotificationName:object:",
				uintptr(String("NSApplicationDidChangeScreenParametersNotification")), 0)
			settle()
		}

		// Unchanged: it waits, through a notification that changes nothing.
		got := next(h, "displays", b)
		post()
		if _, _, done := got(); done {
			fatal(t, "next answered without a change")
		}
		// Changed: it answers with what's there.
		now = []byte("[]")
		post()
		if r, err, done := got(); !done || err != nil || string(r) != "[]" {
			fatalf(t, "next after a change: %s, %v, %v", r, err, done)
		}
		// What Go last had isn't what's there: it answers at once.
		if r, err, done := next(h, "displays", b)(); !done || err != nil || string(r) != "[]" {
			fatalf(t, "next, behind: %s, %v, %v", r, err, done)
		}
		got = next(h, "displays", now)
		await(h, "displays", "stop", nil)
		if _, err, done := got(); !done || err == nil || err.Error() != "stopped" {
			fatalf(t, "stop: %v, %v", err, done)
		}
	})
}

// The power state is six bytes; sleep and wake come as events, queued
// between two "next"s while Go watches, and not after "stop".
func TestPowerModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		s, err := await(h, "power", "current", nil)
		if err != nil || len(s) != 6 || s[2] != 255 && s[2] > 100 || s[5] > 3 {
			fatalf(t, "power.current: %v, %v", s, err)
		}
		if s[0] == 0 && s[2] != 255 {
			fatalf(t, "a level without a battery: %v", s)
		}
		t.Logf("power: %v", s)
		b, err := await(h, "power", "idle", nil)
		if err != nil || len(b) != 8 || binary.LittleEndian.Uint64(b) > 1<<40 {
			fatalf(t, "power.idle: %v, %v", b, err)
		}

		// The state as read (the battery may change meanwhile).
		read := powerWatch.read
		defer func() { powerWatch.read = read }()
		powerWatch.read = func() []byte { return s }
		got := next(h, "power", s)
		settle()
		if _, _, done := got(); done {
			fatal(t, "next answered without a change")
		}
		ws := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "notificationCenter")
		post := func(name string) {
			Send(ws, "postNotificationName:object:", uintptr(String(name)), 0)
			settle()
		}
		post("NSWorkspaceWillSleepNotification")
		r, err, done := got()
		if !done || err != nil || len(r) != 7 || r[6] != powerSuspend {
			fatalf(t, "sleep: %v, %v, %v", r, err, done)
		}
		// Woken while Go was between two "next"s: kept for the next one.
		post("NSWorkspaceDidWakeNotification")
		powerWatch.event(powerLock)
		got = next(h, "power", r[:6])
		if r, _, done := got(); !done || string(r[6:]) != string([]byte{powerResume, powerLock}) {
			fatalf(t, "queued: %v, %v", r, done)
		}
		await(h, "power", "stop", nil)
		powerWatch.event(powerUnlock)
		got = next(h, "power", r[:6])
		settle()
		if r, _, done := got(); done {
			fatalf(t, "an event kept after stop: %v", r)
		}
		await(h, "power", "stop", nil)
	})
}

// The appearance is the accent in sRGB and the two display options.
func TestAppearanceModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		b, err := await(h, "appearance", "current", nil)
		if err != nil || len(b) != 6 || b[0] != 1 || b[4] > 1 || b[5] > 1 {
			fatalf(t, "appearance.current: %v, %v", b, err)
		}
		t.Logf("appearance: %v", b)
		read := appearanceWatch.read
		defer func() { appearanceWatch.read = read }()
		appearanceWatch.read = func() []byte { return b }
		got := next(h, "appearance", b)
		Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "postNotificationName:object:",
			uintptr(String("NSSystemColorsDidChangeNotification")), 0)
		settle()
		if _, _, done := got(); done {
			fatal(t, "next answered without a change")
		}
		await(h, "appearance", "stop", nil)
	})
}

// The progress is a view over the Dock tile's icon, taken off with none;
// an indeterminate one slides; attention is asked only while the app
// isn't active, and cancelled.
func TestDockModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		progress := func(state byte, v float64) {
			await(h, "dock", "progress", binary.LittleEndian.AppendUint64([]byte{state}, math.Float64bits(v)))
		}
		tile := dockTile()
		progress(progressNormal, 0.5)
		if Send(tile, "contentView") == 0 || dockBar.fill == 0 {
			fatal(t, "no progress on the tile")
		}
		track, fill := MsgRect(dockBar.track, Sel("frame")), MsgRect(dockBar.fill, Sel("frame"))
		if math.Abs(fill.Size.Width-track.Size.Width/2) > 1 {
			fatalf(t, "half: fill %v of track %v", fill.Size, track.Size)
		}
		progress(progressIndeterminate, 0)
		if dockBar.timer == 0 {
			fatal(t, "indeterminate: no timer")
		}
		x := MsgRect(dockBar.fill, Sel("frame")).Origin.X
		runUntil(func() bool { return MsgRect(dockBar.fill, Sel("frame")).Origin.X != x })
		if MsgRect(dockBar.fill, Sel("frame")).Origin.X == x {
			fatal(t, "indeterminate: it doesn't slide")
		}
		progress(progressError, 0.3)
		if dockBar.timer != 0 {
			fatal(t, "an error's bar slides")
		}
		progress(progressNone, 0)
		if Send(tile, "contentView") != 0 || dockBar.view != 0 {
			fatal(t, "the progress stayed")
		}

		b, err := await(h, "dock", "attention", []byte{0})
		if err != nil || len(b) != 4 {
			fatalf(t, "attention: %v, %v", b, err)
		}
		active := SendBool(Send(Class("NSApplication"), "sharedApplication"), "isActive")
		if id := binary.LittleEndian.Uint32(b); active && id != 0 {
			fatalf(t, "attention asked while active: %d", id)
		}
		if _, err := await(h, "dock", "cancelAttention", b); err != nil {
			fatal(t, "cancelAttention:", err)
		}
	})
}

// Through the packages: the displays, the power and the appearance come
// to an app's watchers, on the main thread.
func TestDesktopPackages(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		var ds []screen.Display
		var p *power.State
		var a *appearance.Settings
		var stops []func()
		inCore(h, func() {
			stops = append(stops,
				screen.WatchDisplays(func(d []screen.Display) { ds = d }),
				power.Watch(func(s power.State) { p = &s }),
				appearance.Watch(func(s appearance.Settings) { a = &s }))
		})
		if !runUntil(func() bool { h.runPosted(); return len(ds) > 0 && p != nil && a != nil }) {
			fatalf(t, "displays %v, power %v, appearance %v", ds, p, a)
		}
		if d, ok := screen.Primary(ds); !ok || d.Scale < 1 || d.Bounds.Width <= 0 {
			fatalf(t, "the primary display: %+v", d)
		}
		if _, _, _, _, ok := a.Accent.RGBA(); !ok {
			fatalf(t, "no accent: %+v", a)
		}
		inCore(h, func() {
			for _, s := range stops {
				s()
			}
		})
		runUntil(func() bool {
			h.runPosted()
			return len(displaysWatch.waiting)+len(powerWatch.waiting)+len(appearanceWatch.waiting) == 0
		})
		if n := len(displaysWatch.waiting) + len(powerWatch.waiting) + len(appearanceWatch.waiting); n != 0 {
			fatalf(t, "%d still waiting after stop", n)
		}
	})
}

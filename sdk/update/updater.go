package update

import (
	"context"
	"time"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Phase is where the updater is: what its window shows.
type Phase uint8

const (
	// Idle: nothing under way; the window is closed.
	Idle Phase = iota
	// Checking asks the feed.
	Checking
	// UpToDate: the check found nothing newer (said when the user
	// asked).
	UpToDate
	// Available: State.Release is out; the window offers it.
	Available
	// Downloading State.Release: State.Downloaded of State.Total bytes.
	Downloading
	// Installing: downloaded, being checked and put in place.
	Installing
	// Ready: State.Release is installed and runs after Relaunch, or at the
	// next launch.
	Ready
	// Failed: the check or the install failed (State.Err).
	Failed
	// Unavailable: the app can't update itself (State.Err: Supported's).
	Unavailable
)

// State is the updater's: what Use returns, for an interface of the app's
// own (a badge on the settings, a banner).
type State struct {
	Phase Phase
	// Release is the version offered, downloading, or installed.
	Release *Release
	// Downloaded and Total are the download's bytes while Downloading.
	Downloaded, Total int64
	// Err is why the updater Failed, or is Unavailable.
	Err error
	// User reports whether the user asked (CheckForUpdates): the window
	// shows every phase then, as it does an update found in the
	// background.
	User bool
	// Installing reports whether Failed is the install's, not the check's.
	Installing bool
}

// Options configure the updater (Window).
type Options struct {
	// Interval is the time between checks in the background: a day when
	// zero. The first is soon after launch, so a version that came out
	// since the app last ran shows as it opens.
	Interval time.Duration
	// DisableAutomaticChecks turns checks in the background off until
	// SetAutomaticChecks turns them on (or the user did before), for an
	// app that asks the user first, or that only checks from its menu.
	DisableAutomaticChecks bool
	// Icon is an image of the app, in the window: an asset's path, as
	// ui.Image takes it. None when empty.
	Icon string
}

// firstCheck is how long after the updater starts it first checks, and
// retryAfterFailure the longest a check that failed waits for the next.
var (
	firstCheck        = 10 * time.Second
	retryAfterFailure = time.Hour
)

// Replaced by tests.
var (
	checkFn   = Check
	installFn = func(r *Release, ctx context.Context, progress func(downloaded, total int64)) error {
		return r.Install(ctx, progress)
	}
	supportedFn = Supported
	relaunchFn  = Relaunch
	post        = ui.Post
	now         = time.Now
	afterFunc   = func(d time.Duration, f func()) (stop func() bool) { return time.AfterFunc(d, f).Stop }
)

// controller is the updater: its state, on the main thread.
type controller struct {
	opts    Options
	state   *signal.Signal[State]
	open    *signal.Signal[bool]
	running bool // a Window is mounted: background checks are scheduled
	cancel  context.CancelFunc
	session int // counts sessions: a goroutine's result for an older one is dropped
	stop    func() bool
	failed  time.Time // the last background check that failed
	checked bool      // a check began since launch
}

var ctl controller

// init makes the signals, on the main thread, before anything reads them.
func (c *controller) init() {
	if c.state == nil {
		c.state = signal.New(State{})
		c.open = signal.New(false)
	}
}

// Use returns the updater's state, for an interface of the app's own;
// Window shows it in the update window. Main thread.
func Use() signal.Accessor[State] {
	ctl.init()
	return ctl.state
}

// CheckForUpdates checks for updates as the user asked, from a menu item
// or a button: the update window (Window) shows at once, says when the
// app is up to date or can't update itself, and offers a version the user
// skipped too. While a check or download is under way, it brings its
// window back. Main thread.
func CheckForUpdates() {
	c := &ctl
	c.init()
	s := c.state.Peek()
	switch s.Phase {
	case Checking, Available, Downloading, Installing, Ready:
		s.User = true
		c.state.Set(s)
		c.open.Set(true)
		return
	}
	c.begin(true)
}

// start begins the background checks of a mounted Window.
func (c *controller) start(o Options) {
	c.init()
	c.opts = o
	if c.opts.Interval <= 0 {
		c.opts.Interval = 24 * time.Hour
	}
	c.running = true
	c.schedule()
}

// end stops them, as the Window goes.
func (c *controller) end() {
	c.running = false
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
}

// schedule arms the next check in the background: soon after launch, then
// an interval after the last that succeeded (an hour after one that
// failed, at most).
func (c *controller) schedule() {
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
	if !c.running || !AutomaticChecks() || supportedFn() != nil {
		return
	}
	if s := c.state.Peek(); s.Phase != Idle && s.Phase != UpToDate && s.Phase != Failed {
		return // one is under way, or offered, or installed
	}
	t := now()
	due := t.Add(firstCheck)
	if c.checked {
		due = LastCheck().Add(c.opts.Interval)
		if due.After(t.Add(c.opts.Interval)) { // the clock was set back
			due = t.Add(c.opts.Interval)
		}
		if retry := c.failed.Add(min(c.opts.Interval, retryAfterFailure)); !c.failed.IsZero() && retry.After(due) {
			due = retry
		}
		due = maxTime(due, t.Add(firstCheck))
	}
	// A computer asleep stops timers, not the clock: the timer wakes at
	// least hourly to compare with the clock.
	c.stop = afterFunc(min(due.Sub(t), wakeEvery), func() {
		post(func() {
			c.stop = nil
			if !now().Before(due) {
				c.begin(false)
			} else {
				c.schedule()
			}
		})
	})
}

// wakeEvery is the longest a scheduled check's timer sleeps.
var wakeEvery = time.Hour

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// begin starts a check: the user's (the window shows) or the
// background's.
func (c *controller) begin(user bool) {
	if c.cancel != nil {
		c.cancel()
	}
	c.checked = true
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
	if err := supportedFn(); err != nil {
		if user {
			c.state.Set(State{Phase: Unavailable, Err: err, User: true})
			c.open.Set(true)
		}
		return
	}
	c.session++
	session := c.session
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.state.Set(State{Phase: Checking, User: user})
	if user {
		c.open.Set(true)
	}
	go func() {
		r, err := checkFn(ctx)
		post(func() {
			if c.session == session && ctx.Err() == nil {
				c.handleCheck(r, err, user)
			}
		})
	}()
}

// handleCheck takes a check's outcome.
func (c *controller) handleCheck(r *Release, err error, user bool) {
	if err != nil {
		c.failed = now()
		if user {
			c.state.Set(State{Phase: Failed, Err: err, User: true})
		} else {
			c.state.Set(State{})
		}
		c.schedule()
		return
	}
	c.failed = time.Time{}
	t := now()
	skipped := ""
	changePrefs(func(p *prefs) {
		p.LastCheck = t
		skipped = p.SkippedVersion
	})
	switch {
	case r == nil && user:
		c.state.Set(State{Phase: UpToDate, User: true})
	case r == nil, !user && r.Version == skipped:
		c.state.Set(State{})
		c.open.Set(false)
		c.schedule()
	case !user && AutomaticDownloads():
		c.install(r, false)
	default:
		c.state.Set(State{Phase: Available, Release: r, User: user})
		c.open.Set(true)
	}
}

// install downloads and installs r; the window shows its progress when
// it's open.
func (c *controller) install(r *Release, user bool) {
	c.session++
	session := c.session
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.state.Set(State{Phase: Downloading, Release: r, Total: r.Size, User: user})
	go func() {
		err := installFn(r, ctx, func(downloaded, total int64) {
			post(func() {
				if c.session != session {
					return
				}
				s := c.state.Peek()
				if s.Phase != Downloading {
					return
				}
				s.Downloaded, s.Total = downloaded, total
				if downloaded >= total {
					s.Phase = Installing
				}
				c.state.Set(s)
			})
		})
		post(func() {
			if c.session != session || ctx.Err() != nil {
				return
			}
			s := c.state.Peek()
			if err != nil {
				c.failed = now()
				c.state.Set(State{Phase: Failed, Release: r, Err: err, User: s.User, Installing: true})
				if s.User || c.open.Peek() {
					c.open.Set(true)
				}
				return
			}
			c.state.Set(State{Phase: Ready, Release: r, User: s.User})
		})
	}()
}

// The answers of the update window.

// Install installs the release offered.
func (c *controller) installOffered(automatic bool) {
	s := c.state.Peek()
	if s.Phase != Available || s.Release == nil {
		return
	}
	SetAutomaticDownloads(automatic)
	c.install(s.Release, true)
}

// skip skips the release offered: background checks don't offer it again,
// but offer the next one.
func (c *controller) skip(automatic bool) {
	s := c.state.Peek()
	if s.Release != nil {
		v := s.Release.Version
		changePrefs(func(p *prefs) { p.SkippedVersion = v; p.AutomaticDownloads = automatic })
	}
	c.close()
}

// remindLater closes the window: the next check offers the release again.
func (c *controller) remindLater(automatic bool) {
	SetAutomaticDownloads(automatic)
	c.close()
}

// close closes the window and ends what it shows: a check or a download
// under way is canceled; an installed update stays (it runs at the next
// launch).
func (c *controller) close() {
	s := c.state.Peek()
	if c.cancel != nil && (s.Phase == Checking || s.Phase == Downloading || s.Phase == Installing) {
		c.cancel()
		c.cancel = nil
		c.session++
	}
	c.open.Set(false)
	if s.Phase == Ready {
		s.User = false
		c.state.Set(s)
		return // installed: nothing more to check until the app relaunches
	}
	c.state.Set(State{})
	c.schedule()
}

func (c *controller) relaunch() {
	c.open.Set(false)
	relaunchFn()
}

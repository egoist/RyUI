package nowplaying

import "time"

// SetClock replaces the clock and the Player's status timer for a test,
// until restore: tick runs the timers due.
func SetClock(at func() time.Time) (tick func(), restore func()) {
	oldNow, oldAfter := now, after
	type timer struct {
		at   time.Time
		f    func()
		dead bool
	}
	var timers []*timer
	now = at
	after = func(d time.Duration, f func()) func() {
		t := &timer{at: at().Add(d), f: f}
		timers = append(timers, t)
		return func() { t.dead = true }
	}
	tick = func() {
		due := timers
		timers = nil
		for _, t := range due {
			switch {
			case t.dead:
			case !at().Before(t.at):
				t.f()
			default:
				timers = append(timers, t)
			}
		}
	}
	return tick, func() { now, after = oldNow, oldAfter }
}

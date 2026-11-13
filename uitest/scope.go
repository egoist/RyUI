package uitest

import (
	"math"
	"slices"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
)

// Scope is a part of the screen, for In: where to look when a text
// shows in more than one place (a bar button and a toolbar item both
// titled Star).
type Scope struct {
	region region
	under  *fake.View
	nextTo string
}

type region uint8

const (
	anywhere region = iota
	inContent
	inNavigationBar
	inToolbar
	inTabBar
)

var (
	// Content is what the screens, sheets, alerts and popovers show,
	// not their bars.
	Content = Scope{region: inContent}
	// NavigationBar is the screens' navigation bars: their titles and
	// bar buttons (ScreenOptions' Title and Actions).
	NavigationBar = Scope{region: inNavigationBar}
	// Toolbar is the screens' bottom toolbars and primary actions
	// (ScreenOptions' Toolbar and Primary).
	Toolbar = Scope{region: inToolbar}
	// TabBar is the tab bars' tabs.
	TabBar = Scope{region: inTabBar}
)

// Under is the part of the screen in v (v and what's in it; a List's
// rows are in it): a.In(uitest.Under(a.Row("Ada"))) is the list's row
// showing Ada.
func Under(v *fake.View) Scope { return Scope{under: v} }

// NextTo is what's next to the view showing label, as a user reads a
// row or a form: on its line, the nearest; else below it, the nearest.
// a.In(uitest.NextTo("Ada")).Tap("Remove") taps the Remove beside Ada,
// whether or not a view holds the two (a Row makes none of its own).
func NextTo(label string) Scope { return Scope{nextTo: label} }

// In returns the app narrowed to s: what it finds and taps is in s.
// It shares everything else with a (an open menu, the field typed into
// last).
func (a *App) In(s Scope) *App {
	return &App{Host: a.Host, Root: a.Root, session: a.session, scope: s}
}

// besides keeps, of targets, those next to the scope's label (NextTo):
// for each view showing it, the nearest.
func (a *App) besides(layers []*fake.View, ts []target) []target {
	label := a.scope.nextTo
	if label == "" || len(ts) == 0 {
		return ts
	}
	free := &App{Host: a.Host, Root: a.Root, session: a.session}
	var views []*fake.View
	for _, t := range ts {
		if t.how == byView && !slices.Contains(views, t.v) {
			views = append(views, t.v)
		}
	}
	var keep []*fake.View
	for _, l := range free.targets(layers, label) {
		if l.how != byView {
			continue
		}
		var others []*fake.View
		for _, v := range views {
			if v != l.v {
				others = append(others, v)
			}
		}
		if v := nextTo(l.v, others); v != nil {
			keep = append(keep, v)
		}
	}
	var out []target
	for _, t := range ts {
		if t.how == byView && slices.Contains(keep, t.v) {
			out = append(out, t)
		}
	}
	return out
}

// regionOf is the part of the screen a target is in.
func regionOf(t target) region {
	switch t.how {
	case byTitle:
		return inNavigationBar
	case byTab:
		return inTabBar
	case byItem:
		if t.v.Kind == ops.KindScreen {
			if t.i >= ops.ScreenToolbarActions {
				return inToolbar
			}
			return inNavigationBar
		}
	}
	return inContent
}

// inScope reports whether a target is in a's scope.
func (a *App) inScope(t target) bool {
	if r := a.scope.region; r != anywhere && regionOf(t) != r {
		return false
	}
	if u := a.scope.under; u != nil && !a.contains(u, t.v) {
		return false
	}
	return true
}

// contains reports whether v is u or in it: through its parents, and
// from a list's row to the list.
func (a *App) contains(u, v *fake.View) bool {
	for v != nil {
		if v == u {
			return true
		}
		if v.Parent == nil && v.Kind == fake.KindCell {
			_, v = a.cellOf(v)
			continue
		}
		v = v.Parent
	}
	return false
}

// top returns the view at the top of v's tree (the root, a sheet, a
// list's cell) and v's frame in its coordinates.
func top(v *fake.View) (*fake.View, [4]float32) {
	f := v.Frame
	for v.Parent != nil {
		v = v.Parent
		f[0] += v.Frame[0]
		f[1] += v.Frame[1]
	}
	return v, f
}

// nextTo picks the control of those cs that's next to the view showing
// a label, as a user would read it: on its line, the nearest (after it,
// in reading order, before one before it); else below it, the nearest
// (a form's label over its field). Only controls in the same tree (the
// screen, a sheet, a list's row) count. It returns nil if none is, or
// two are as near.
func nextTo(label *fake.View, cs []*fake.View) *fake.View {
	lt, lf := top(label)
	var best *fake.View
	bestScore, tie := math.Inf(1), false
	for _, c := range cs {
		ct, cf := top(c)
		if ct != lt {
			continue
		}
		var score float64
		switch {
		case cf[1] < lf[1]+lf[3] && lf[1] < cf[1]+cf[3]: // on the label's line
			after := cf[0] - (lf[0] + lf[2])
			before := lf[0] - (cf[0] + cf[2])
			if after >= 0 || before < 0 {
				score = math.Abs(float64(after))
			} else {
				score = 1e4 + float64(before)
			}
		case cf[1] >= lf[1]+lf[3]: // below it
			score = 1e5 + float64(cf[1]-(lf[1]+lf[3]))
		default:
			continue
		}
		switch {
		case score < bestScore:
			best, bestScore, tie = c, score, false
		case score == bestScore:
			tie = true
		}
	}
	if tie {
		return nil
	}
	return best
}

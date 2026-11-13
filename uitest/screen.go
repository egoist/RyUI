package uitest

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
)

// The screen as a user sees it: the layers (the root, then what's
// presented over it), the views in each that show, and the texts they
// show.

// layers are the root and the views presented over it (sheets, alerts,
// popovers, windows, a snackbar), bottom to top.
func (a *App) layers() []*fake.View {
	var out []*fake.View
	if r := a.Host.Root(); r != nil {
		out = append(out, r)
	}
	return append(out, a.Host.Presented()...)
}

// modal reports whether a presented view takes every touch until it
// goes.
func modal(v *fake.View) bool {
	return v.Kind == ops.KindSheet || v.Kind == ops.KindAlert || v.Kind == ops.KindPopover
}

// touchable are the layers a touch reaches: the top modal one (and a
// snackbar over it), else all of them.
func (a *App) touchable() []*fake.View {
	ls := a.layers()
	top := -1
	for i, l := range ls {
		if modal(l) {
			top = i
		}
	}
	if top < 0 {
		return ls
	}
	out := []*fake.View{ls[top]}
	for _, l := range ls[top+1:] {
		if !modal(l) {
			out = append(out, l)
		}
	}
	return out
}

// walk calls f on each view under v that shows, depth first, parents
// first; f returning false skips the view's children.
func (a *App) walk(v *fake.View, f func(v *fake.View) bool) {
	if v.Bool(ops.PropHidden) {
		return
	}
	if !f(v) {
		return
	}
	switch v.Kind {
	case ops.KindStack:
		if n := len(v.Children); n > 0 {
			a.walk(v.Children[n-1], f)
		}
		return
	case ops.KindTabs:
		if i := int(v.Int(ops.PropTabsSelected)); i >= 0 && i < len(v.Children) {
			a.walk(v.Children[i], f)
		}
		return
	}
	for _, c := range v.Children {
		a.walk(c, f)
	}
	if v.Kind == ops.KindList {
		if st := a.lists[v.ID]; st != nil {
			for _, i := range st.indices() {
				a.walk(st.cells[i], f)
			}
		}
	}
}

// visible returns every view that shows, in the given layers.
func (a *App) visible(layers []*fake.View) []*fake.View {
	var out []*fake.View
	for _, l := range layers {
		a.walk(l, func(v *fake.View) bool { out = append(out, v); return true })
	}
	return out
}

// target is something on the screen a text names: a view showing it,
// or an item (a tab, a bar button, an alert's button, a menu's item)
// that a view shows.
type target struct {
	v    *fake.View
	how  how
	i    int64 // the item: its number, or a tab's, a segment's index
	text string
}

type how uint8

const (
	byView     how = iota // the view's own text: a Text, a Button's title, a field's
	byTitle               // a Screen's title, in its navigation bar
	byTab                 // a Tabs' tab: its Screen's title
	bySegment             // a Segmented's segment
	byItem                // an item of a menu: a Screen's bar buttons, toolbar or primary action, an alert's buttons
	byField               // an alert's text field, by its placeholder
	bySnackbar            // a snackbar's message
	bySnackAct            // a snackbar's action
)

// targets are what shows text, in the layers.
func (a *App) targets(layers []*fake.View, text string) []target {
	var out []target
	add := func(v *fake.View, h how, i int64) {
		if t := (target{v, h, i, text}); a.inScope(t) {
			out = append(out, t)
		}
	}
	items := func(v *fake.View, p ops.PropID, base int64) {
		for _, it := range ops.FlattenActions(v.ActionItems(p)) {
			if it.Title == text {
				add(v, byItem, base+int64(it.Index))
			}
		}
	}
	for _, v := range a.visible(layers) {
		for _, s := range shows(v) {
			if s == text {
				add(v, byView, 0)
				break
			}
		}
		switch v.Kind {
		case ops.KindScreen:
			if v.String(ops.PropScreenTitle) == text && (v.Parent == nil || v.Parent.Kind != ops.KindTabs) {
				add(v, byTitle, 0)
			}
			items(v, ops.PropScreenActions, 0)
			items(v, ops.PropScreenToolbar, ops.ScreenToolbarActions)
			if p := v.PrimaryItem(); p != nil && p.Title == text {
				add(v, byItem, ops.ScreenPrimaryAction)
			}
		case ops.KindTabs:
			for i, s := range v.Children {
				if s.String(ops.PropScreenTitle) == text {
					add(v, byTab, int64(i))
				}
			}
		case ops.KindSegmented:
			for i, s := range v.Strings(ops.PropSegmentedSegments) {
				if s == text {
					add(v, bySegment, int64(i))
				}
			}
		case ops.KindAlert:
			items(v, ops.PropAlertButtons, 0)
			for i, f := range v.AlertFields() {
				if f.Placeholder == text {
					add(v, byField, int64(i))
				}
			}
		case ops.KindSnackbar:
			if v.String(ops.PropSnackbarMessage) == text {
				add(v, bySnackbar, 0)
			}
			if v.String(ops.PropSnackbarAction) == text {
				add(v, bySnackAct, 0)
			}
		}
	}
	return a.besides(layers, out)
}

// shows are the texts a view shows of its own (not its children's).
func shows(v *fake.View) []string {
	var out []string
	add := func(s string) {
		if s != "" {
			out = append(out, s)
		}
	}
	switch v.Kind {
	case ops.KindText:
		add(v.String(ops.PropTextText))
	case ops.KindButton:
		add(v.String(ops.PropButtonTitle))
	case ops.KindTextField:
		add(v.String(ops.PropTextFieldValue))
		add(v.String(ops.PropTextFieldPlaceholder))
	case ops.KindRichText, ops.KindSelectable:
		for _, p := range v.Rich() {
			add(p.Text())
		}
	case ops.KindAlert:
		add(v.String(ops.PropAlertTitle))
		add(v.String(ops.PropAlertMessage))
	case ops.KindPicker:
		if items, i := v.Strings(ops.PropPickerItems), int(v.Int(ops.PropPickerSelected)); i >= 0 && i < len(items) {
			add(items[i])
		}
	}
	add(v.String(ops.PropA11yLabel))
	return out
}

// Texts returns the texts the screen shows, in the order a screen
// reader would read them: each layer's views depth first.
func (a *App) Texts() []string {
	var out []string
	for _, l := range a.layers() {
		a.walk(l, func(v *fake.View) bool {
			out = append(out, a.targetsOf(v)...)
			return true
		})
	}
	return out
}

// targetsOf are the texts v shows: its own, its navigation bar's, its
// tabs', its items'.
func (a *App) targetsOf(v *fake.View) []string {
	out := shows(v)
	titles := func(p ops.PropID) {
		for _, it := range ops.FlattenActions(v.ActionItems(p)) {
			if it.Title != "" {
				out = append(out, it.Title)
			}
		}
	}
	switch v.Kind {
	case ops.KindScreen:
		if t := v.String(ops.PropScreenTitle); t != "" && (v.Parent == nil || v.Parent.Kind != ops.KindTabs) {
			out = append(out, t)
		}
		titles(ops.PropScreenActions)
		titles(ops.PropScreenToolbar)
		if p := v.PrimaryItem(); p != nil && p.Title != "" {
			out = append(out, p.Title)
		}
	case ops.KindTabs:
		for _, s := range v.Children {
			if t := s.String(ops.PropScreenTitle); t != "" {
				out = append(out, t)
			}
		}
	case ops.KindSegmented:
		out = append(out, v.Strings(ops.PropSegmentedSegments)...)
	case ops.KindAlert:
		titles(ops.PropAlertButtons)
	case ops.KindSnackbar:
		if s := v.String(ops.PropSnackbarMessage); s != "" {
			out = append(out, s)
		}
		if s := v.String(ops.PropSnackbarAction); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Has reports whether the screen shows text, exactly, anywhere: a
// text, a title, a field's text or placeholder, an accessibility label,
// a tab, a bar button, an alert's button, an open menu's item.
func (a *App) Has(text string) bool {
	if a.menu != nil {
		for _, it := range a.menu.items() {
			if it.Title == text {
				return true
			}
		}
	}
	return len(a.targets(a.layers(), text)) > 0
}

// Expect fails the test unless the screen shows each text.
func (a *App) Expect(texts ...string) {
	a.t.Helper()
	for _, s := range texts {
		if !a.Has(s) {
			a.t.Fatalf("uitest: the screen doesn't show %q\n%s", s, a.screenFor())
		}
	}
}

// ExpectNo fails the test if the screen shows any of texts.
func (a *App) ExpectNo(texts ...string) {
	a.t.Helper()
	for _, s := range texts {
		if a.Has(s) {
			a.t.Fatalf("uitest: the screen shows %q\n%s", s, a.screenFor())
		}
	}
}

// Get returns the view that shows text (a Text, a Button, a field, a
// Screen for its title or its bar's buttons, a Tabs for a tab), failing
// unless exactly one does.
func (a *App) Get(text string) *fake.View {
	a.t.Helper()
	ts := a.targets(a.layers(), text)
	var vs []*fake.View
	for _, t := range ts {
		if !slices.Contains(vs, t.v) {
			vs = append(vs, t.v)
		}
	}
	switch len(vs) {
	case 0:
		a.t.Fatalf("uitest: the screen doesn't show %q\n%s", text, a.screenFor())
	case 1:
		return vs[0]
	}
	a.t.Fatalf("uitest: %d views show %q\n%s", len(vs), text, a.screenFor())
	return nil
}

// All returns the views of kind k that show, in the order they show.
func (a *App) All(k ops.Kind) []*fake.View {
	var out []*fake.View
	for _, v := range a.visible(a.layers()) {
		if v.Kind == k {
			out = append(out, v)
		}
	}
	return out
}

// describe writes v and what shows under it, for Snapshot.
func (a *App) describe(b *strings.Builder, v *fake.View, depth int) {
	if v.Bool(ops.PropHidden) {
		return
	}
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(kindName(v))
	f := v.Frame
	fmt.Fprintf(b, " %s,%s %sx%s", num(f[0]), num(f[1]), num(f[2]), num(f[3]))
	for _, s := range a.targetsOf(v) {
		fmt.Fprintf(b, " %q", s)
	}
	state(b, v)
	b.WriteByte('\n')
	var kids []*fake.View
	switch v.Kind {
	case ops.KindStack:
		if n := len(v.Children); n > 0 {
			kids = v.Children[n-1:]
		}
	case ops.KindTabs:
		if i := int(v.Int(ops.PropTabsSelected)); i >= 0 && i < len(v.Children) {
			kids = v.Children[i : i+1]
		}
	default:
		kids = v.Children
	}
	for _, c := range kids {
		a.describe(b, c, depth+1)
	}
	if v.Kind == ops.KindList {
		if st := a.lists[v.ID]; st != nil {
			for _, i := range st.indices() {
				a.describe(b, st.cells[i], depth+1)
			}
		}
	}
}

// num writes a frame's coordinate to the hundredth of a point: frames
// are snapped to the device's pixels, 2.625 to a point on a Pixel.
func num(f float32) string {
	return strconv.FormatFloat(math.Round(float64(f)*100)/100, 'f', -1, 64)
}

func kindName(v *fake.View) string {
	if v.Kind == fake.KindCell {
		return "Cell"
	}
	return v.Kind.String()
}

// state writes what a control shows besides text: on, a value, the
// selection, disabled.
func state(b *strings.Builder, v *fake.View) {
	switch v.Kind {
	case ops.KindToggle:
		if v.Bool(ops.PropToggleOn) {
			b.WriteString(" on")
		} else {
			b.WriteString(" off")
		}
	case ops.KindSlider:
		fmt.Fprintf(b, " =%g", v.Float(ops.PropSliderValue))
	case ops.KindStepper:
		fmt.Fprintf(b, " =%g", v.Float(ops.PropStepperValue))
	case ops.KindProgress:
		if v.Bool(ops.PropProgressIndeterminate) {
			b.WriteString(" indeterminate")
		} else {
			fmt.Fprintf(b, " =%g", v.Float(ops.PropProgressValue))
		}
	case ops.KindTabs:
		fmt.Fprintf(b, " selected=%d", v.Int(ops.PropTabsSelected))
	case ops.KindSegmented:
		fmt.Fprintf(b, " selected=%d", v.Int(ops.PropSegmentedSelected))
	case ops.KindTextField:
		if v.Bool(ops.PropTextFieldSecure) {
			b.WriteString(" secure")
		}
	}
	if v.Bool(ops.PropSelected) {
		b.WriteString(" selected")
	}
	if v.Bool(ops.PropDisabled) {
		b.WriteString(" disabled")
	}
}

func describeItem(it ops.ActionItem) string {
	s := fmt.Sprintf("%q", it.Title)
	if it.Is(ops.MenuChecked) {
		s += " checked"
	}
	if it.Is(ops.MenuDisabled) {
		s += " disabled"
	}
	return s
}

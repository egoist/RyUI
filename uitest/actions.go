package uitest

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
)

// What a user does, named by what the screen shows. Each action settles.

// openMenu is a menu showing: a view's Menu, or a Screen's bar button's,
// toolbar's or primary action's, whose items Tap chooses.
type openMenu struct {
	v    *fake.View
	prop ops.PropID
	base int64 // added to an item's number: the Action event's
	sub  int   // the submenu showing (its number), or -1: the whole menu
}

type menuItem struct {
	ops.ActionItem
	depth int
}

// items are the menu's items that show: its own, or a submenu's.
func (m *openMenu) items() []menuItem {
	all := m.v.ActionItems(m.prop)
	if m.prop == ops.PropScreenPrimary && len(all) > 0 { // item 0 is the button
		all = all[0].Items
	}
	if m.sub >= 0 {
		for _, it := range ops.FlattenActions(all) {
			if it.Index == m.sub {
				all = it.Items
				break
			}
		}
	}
	var out []menuItem
	var walk func(items []ops.ActionItem, depth int)
	walk = func(items []ops.ActionItem, depth int) {
		for _, it := range items {
			out = append(out, menuItem{it, depth})
			if it.Is(ops.MenuInline) {
				walk(it.Items, depth+1)
			}
		}
	}
	walk(all, 0)
	return out
}

// Tap taps what shows text: a button (its title or accessibility
// label), a text in something that takes a press (a row, a pressable
// view), a toggle, a tab, a segment, a navigation bar's button, an
// alert's button, a snackbar's action, an open menu's item. A view with
// a menu opens it; a text field takes the focus (Type types into the
// field tapped last). It fails if nothing shows text, or more than one
// thing that takes the tap does, or it's disabled.
func (a *App) Tap(text string) {
	a.t.Helper()
	if a.menu != nil {
		a.chooseMenuItem(text)
		return
	}
	t := a.one(text, func(t target) bool { return t.how != bySnackbar && t.how != byTitle && t.how != byField })
	switch t.how {
	case byView:
		a.press(t.v, text, false)
	case byTab:
		a.Host.Select(t.v, int(t.i))
	case bySegment:
		a.Host.Select(t.v, int(t.i))
	case byItem:
		a.chooseItem(t.v, t.i, text)
	case bySnackAct:
		a.Host.PressSnackbarAction(t.v)
	}
	a.Settle()
}

// LongPress long-presses what shows text: a view that takes a long
// press, or one with a context menu, which opens.
func (a *App) LongPress(text string) {
	a.t.Helper()
	t := a.one(text, func(t target) bool { return t.how == byView })
	a.press(t.v, text, true)
	a.Settle()
}

// one finds the one target in reach that shows text and passes ok: in
// the top layer that has any.
func (a *App) one(text string, ok func(target) bool) target {
	a.t.Helper()
	var found []target
	for _, t := range a.targets(a.touchable(), text) {
		if ok(t) {
			found = append(found, t)
		}
	}
	// A view inside a pressable one names it once: a Text in a Button,
	// in a row.
	found = dedupe(a, found)
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		if a.Has(text) {
			a.t.Fatalf("uitest: %q shows, but under %s, out of reach\n%s", text, a.topName(), a.screenFor())
		}
		a.t.Fatalf("uitest: the screen doesn't show %q\n%s", text, a.screenFor())
	}
	a.t.Fatalf("uitest: %d things show %q; %s\n%s", len(found), text, narrow(found), a.screenFor())
	return target{}
}

// narrow says how to tell targets apart: the scopes they're in, if they
// differ.
func narrow(ts []target) string {
	names := map[region]string{
		inContent: "uitest.Content", inNavigationBar: "uitest.NavigationBar",
		inToolbar: "uitest.Toolbar", inTabBar: "uitest.TabBar",
	}
	var in []string
	for _, t := range ts {
		if n := names[regionOf(t)]; !slices.Contains(in, n) {
			in = append(in, n)
		}
	}
	if len(in) > 1 {
		return "narrow it with a.In(" + strings.Join(in, "), a.In(") + ")"
	}
	return "narrow it with a.In(uitest.Under(view)), or give it an accessibility label"
}

// Disabled reports whether what shows text is disabled: a control (or a
// view around it), a bar button, an alert's button, an open menu's item.
// It fails unless exactly one thing shows text.
func (a *App) Disabled(text string) bool {
	a.t.Helper()
	if a.menu != nil {
		for _, it := range a.menu.items() {
			if it.Title == text {
				return it.Is(ops.MenuDisabled)
			}
		}
	}
	var found []target
	for _, t := range a.targets(a.layers(), text) {
		if t.how != bySnackbar && t.how != byTitle {
			found = append(found, t)
		}
	}
	found = dedupe(a, found)
	if len(found) != 1 {
		a.t.Fatalf("uitest: %d things show %q\n%s", len(found), text, a.screenFor())
	}
	t := found[0]
	switch t.how {
	case byItem:
		prop, base := ops.PropScreenActions, int64(0)
		switch {
		case t.v.Kind == ops.KindAlert:
			prop = ops.PropAlertButtons
		case t.i >= ops.ScreenPrimaryAction:
			prop, base = ops.PropScreenPrimary, ops.ScreenPrimaryAction
		case t.i >= ops.ScreenToolbarActions:
			prop, base = ops.PropScreenToolbar, ops.ScreenToolbarActions
		}
		for _, it := range ops.FlattenActions(t.v.ActionItems(prop)) {
			if int64(it.Index) == t.i-base {
				return it.Is(ops.MenuDisabled)
			}
		}
		return false
	case byView:
		if p := a.pressable(t.v, false); p != nil {
			return disabled(p)
		}
	}
	return disabled(t.v)
}

// dedupe drops targets that press the same thing.
func dedupe(a *App, ts []target) []target {
	var out []target
	seen := map[[2]any]bool{}
	for _, t := range ts {
		k := [2]any{t.v, t.i}
		if t.how == byView {
			if p := a.pressable(t.v, false); p != nil {
				k = [2]any{p, int64(-1)}
			}
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	return out
}

func (a *App) topName() string {
	ls := a.touchable()
	if len(ls) > 0 && ls[0] != a.Host.Root() {
		return "the " + ls[0].Kind.String() + " over it"
	}
	return "what's over it"
}

// pressable is the view a touch on v goes to: v or the nearest around
// it that takes it, a list's row; nil if none does.
func (a *App) pressable(v *fake.View, long bool) *fake.View {
	var idle *fake.View // a button that does nothing, unless a menu around it opens
	for p := v; p != nil; p = p.Parent {
		if long {
			if p.Listens[ops.EventLongPress] || hasMenu(p, ops.MenuTriggerLongPress) {
				return p
			}
			continue
		}
		switch {
		case p.Listens[ops.EventPress], hasMenu(p, ops.MenuTriggerTap):
			return p
		case p.Kind == ops.KindToggle, p.Kind == ops.KindTextField:
			return p
		case p.Kind == ops.KindButton && idle == nil:
			idle = p
		case p.Kind == fake.KindCell && p.Listens[ops.EventRowSelected]:
			return p
		}
	}
	return idle
}

func hasMenu(v *fake.View, trigger ops.MenuTrigger) bool {
	_, ok := v.Props[ops.PropMenu]
	return ok && ops.MenuTrigger(v.Int(ops.PropMenuTrigger)) == trigger
}

// disabled reports whether v or a view around it is disabled.
func disabled(v *fake.View) bool {
	for p := v; p != nil; p = p.Parent {
		if p.Bool(ops.PropDisabled) {
			return true
		}
	}
	return false
}

func (a *App) press(v *fake.View, text string, long bool) {
	a.t.Helper()
	p := a.pressable(v, long)
	if p == nil {
		what := "a tap"
		if long {
			what = "a long press"
		}
		a.t.Fatalf("uitest: nothing at %q takes %s (%s)\n%s", text, what, v.Kind, a.screenFor())
	}
	if disabled(p) {
		a.t.Fatalf("uitest: %q is disabled\n%s", text, a.screenFor())
	}
	if long {
		if p.Listens[ops.EventLongPress] {
			a.Host.LongPress(p)
		} else {
			a.menu = &openMenu{v: p, prop: ops.PropMenu, sub: -1}
		}
		return
	}
	switch {
	case p.Kind == ops.KindTextField:
		a.focused = p
	case p.Kind == ops.KindToggle:
		a.Host.Flip(p)
	case hasMenu(p, ops.MenuTriggerTap) && !p.Listens[ops.EventPress]:
		a.menu = &openMenu{v: p, prop: ops.PropMenu, sub: -1}
	case p.Kind == fake.KindCell:
		_, list := a.cellOf(p)
		on := true
		if list != nil && list.Bool(ops.PropListSelecting) {
			on = !p.Bool(ops.PropSelected) // a selecting list's tap toggles the row
		}
		a.Host.SelectRow(p, on)
	case p.Listens[ops.EventPress]:
		a.Host.Click(p)
	}
}

// chooseItem presses the item numbered i (base included) of a screen's
// bar buttons, toolbar or primary action, or an alert's buttons: one
// with a submenu opens it.
func (a *App) chooseItem(v *fake.View, i int64, text string) {
	a.t.Helper()
	prop, base := ops.PropScreenActions, int64(0)
	switch {
	case v.Kind == ops.KindAlert:
		prop = ops.PropAlertButtons
	case i >= ops.ScreenPrimaryAction:
		prop, base = ops.PropScreenPrimary, ops.ScreenPrimaryAction
	case i >= ops.ScreenToolbarActions:
		prop, base = ops.PropScreenToolbar, ops.ScreenToolbarActions
	}
	n := int(i - base)
	for _, it := range ops.FlattenActions(v.ActionItems(prop)) {
		if it.Index != n {
			continue
		}
		if it.Is(ops.MenuDisabled) {
			a.t.Fatalf("uitest: %q is disabled\n%s", text, a.screenFor())
		}
		if prop == ops.PropScreenPrimary && n == 0 && len(it.Items) > 0 {
			a.menu = &openMenu{v: v, prop: prop, base: base, sub: -1}
			return
		}
		if it.Is(ops.MenuSubmenu) && !it.Is(ops.MenuInline) {
			a.menu = &openMenu{v: v, prop: prop, base: base, sub: n}
			return
		}
	}
	a.Host.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventAction, Int: i})
}

// chooseMenuItem taps the open menu's item: a submenu opens; another
// closes the menu, chosen.
func (a *App) chooseMenuItem(text string) {
	a.t.Helper()
	m := a.menu
	var titles []string
	for _, it := range m.items() {
		titles = append(titles, it.Title)
		if it.Title != text || it.Is(ops.MenuInline) {
			continue
		}
		if it.Is(ops.MenuDisabled) {
			a.t.Fatalf("uitest: the menu's %q is disabled", text)
		}
		if it.Is(ops.MenuSubmenu) {
			m.sub = it.Index
			return
		}
		a.menu = nil
		n := int64(it.Index)
		if m.prop == ops.PropScreenPrimary {
			n++ // the button is item 0, its menu's after it
		}
		if !m.v.Listens[ops.EventAction] {
			a.t.Fatalf("uitest: the menu at %d (%s) doesn't listen for its actions", m.v.ID, m.v.Kind)
		}
		a.Host.Dispatch(ops.Event{ID: m.v.ID, Kind: ops.EventAction, Int: m.base + n})
		a.Settle()
		return
	}
	a.t.Fatalf("uitest: the open menu has no %q: it has %s", text, quoteAll(titles))
}

func quoteAll(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}

// Type replaces the text of the field label names, as typing does: a
// field showing it (its placeholder, its text, its accessibility label),
// an alert's field (its placeholder), or the one field next to a text
// (a form's label). With label "", it types into the field tapped last.
func (a *App) Type(label, text string) {
	a.t.Helper()
	if label == "" {
		if a.focused == nil {
			a.t.Fatalf("uitest: Type with no label: tap a field first")
		}
		a.Host.Type(a.focused, text)
		a.Settle()
		return
	}
	if alert, i, ok := a.alertField(label); ok {
		a.Host.TypeInAlert(alert, i, text)
		a.Settle()
		return
	}
	f := a.near(label, ops.KindTextField)
	if disabled(f) {
		a.t.Fatalf("uitest: the field %q is disabled", label)
	}
	a.focused = f
	a.Host.Type(f, text)
	a.Settle()
}

func (a *App) alertField(label string) (*fake.View, int, bool) {
	for _, t := range a.targets(a.touchable(), label) {
		if t.how == byField {
			return t.v, int(t.i), true
		}
	}
	return nil, 0, false
}

// Submit presses return in the field label names (see Type; "": the
// field typed into last).
func (a *App) Submit(label string) {
	a.t.Helper()
	if label != "" {
		if alert, _, ok := a.alertField(label); ok {
			if !a.Host.ReturnInAlert(alert) {
				a.t.Fatalf("uitest: the alert's default button is disabled")
			}
			a.Settle()
			return
		}
		a.focused = a.near(label, ops.KindTextField)
	}
	if a.focused == nil {
		a.t.Fatalf("uitest: Submit with no label: type into a field first")
	}
	a.Host.Submit(a.focused)
	a.Settle()
}

// Toggle flips the switch (or checkbox) label names: one with that
// accessibility label, or the one next to a text.
func (a *App) Toggle(label string) {
	a.t.Helper()
	v := a.near(label, ops.KindToggle)
	if disabled(v) {
		a.t.Fatalf("uitest: the switch %q is disabled", label)
	}
	a.Host.Flip(v)
	a.Settle()
}

// Slide moves the slider label names (see Toggle) to value.
func (a *App) Slide(label string, value float64) {
	a.t.Helper()
	v := a.near(label, ops.KindSlider)
	if disabled(v) {
		a.t.Fatalf("uitest: the slider %q is disabled", label)
	}
	a.Host.Slide(v, value)
	a.Settle()
}

// Select chooses option in the picker, wheel or segmented control that
// has it.
func (a *App) Select(option string) {
	a.t.Helper()
	var found []*fake.View
	var index []int
	for _, v := range a.visible(a.touchable()) {
		var items []string
		switch v.Kind {
		case ops.KindPicker:
			items = v.Strings(ops.PropPickerItems)
		case ops.KindWheel:
			items = v.Strings(ops.PropWheelItems)
		case ops.KindSegmented:
			items = v.Strings(ops.PropSegmentedSegments)
		default:
			continue
		}
		for i, s := range items {
			if s == option {
				found = append(found, v)
				index = append(index, i)
				break
			}
		}
	}
	if len(found) != 1 {
		a.t.Fatalf("uitest: %d pickers have %q\n%s", len(found), option, a.screenFor())
	}
	if disabled(found[0]) {
		a.t.Fatalf("uitest: the picker with %q is disabled", option)
	}
	a.Host.Select(found[0], index[0])
	a.Settle()
}

// near returns the view of kind k that label names: one of kind k that
// shows label itself (its accessibility label, a field's placeholder),
// else the one next to a view that shows it, as a user reads a form:
// on its line, or below it.
func (a *App) near(label string, k ops.Kind) *fake.View {
	a.t.Helper()
	ts := a.targets(a.touchable(), label)
	for _, t := range ts {
		if t.how == byView && t.v.Kind == k {
			return t.v
		}
	}
	var cs []*fake.View
	for _, v := range a.visible(a.touchable()) {
		if v.Kind == k && (a.scope.under == nil || a.contains(a.scope.under, v)) {
			cs = append(cs, v)
		}
	}
	var found []*fake.View
	for _, t := range ts {
		if t.how != byView {
			continue
		}
		// A view labelled for the control it holds (ui.ViewProps.Label).
		var in []*fake.View
		for _, c := range cs {
			if c != t.v && a.contains(t.v, c) {
				in = append(in, c)
			}
		}
		c := nextTo(t.v, cs)
		if len(in) == 1 {
			c = in[0]
		}
		if c != nil && !slices.Contains(found, c) {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		a.t.Fatalf("uitest: no %s next to %q\n%s", k, label, a.screenFor())
	}
	a.t.Fatalf("uitest: %d %ss next to %q\n%s", len(found), k, label, a.screenFor())
	return nil
}

// Swipe swipes the list row showing text to reveal its actions and
// presses the one titled action: a trailing one (swiping toward the
// start), or a leading one.
func (a *App) Swipe(text, action string) {
	a.t.Helper()
	t := a.one(text, func(t target) bool { return t.how == byView })
	cell, _ := a.cellOf(t.v)
	if cell == nil {
		a.t.Fatalf("uitest: %q isn't in a list's row", text)
	}
	if i := cell.ActionNamed(ops.PropSwipeActions, action); i >= 0 {
		a.Host.Dispatch(ops.Event{ID: cell.ID, Kind: ops.EventAction, Int: int64(i)})
	} else if i := cell.ActionNamed(ops.PropLeadingSwipeActions, action); i >= 0 {
		a.Host.Dispatch(ops.Event{ID: cell.ID, Kind: ops.EventAction, Int: -1 - int64(i)})
	} else {
		a.t.Fatalf("uitest: the row %q has no swipe action %q: it has %s", text, action,
			quoteAll(append(cell.Actions(ops.PropSwipeActions), cell.Actions(ops.PropLeadingSwipeActions)...)))
	}
	a.Settle()
}

// Back goes back as the user's back gesture or button does (Android's
// back, Escape): an open menu closes; else the top screen of the deepest
// stack in reach pops; else the top sheet, alert or popover is
// dismissed (ui.OnBack in it can keep it); else, on Android, the root
// takes Back if it intercepts it. It fails if there's nothing to go
// back from.
func (a *App) Back() {
	a.t.Helper()
	if a.menu != nil {
		a.menu = nil
		return
	}
	var stack *fake.View
	for _, v := range a.visible(a.touchable()) {
		if v.Kind == ops.KindStack && len(v.Children) > 1 {
			stack = v
		}
	}
	ls := a.touchable()
	switch {
	case stack != nil:
		a.Host.Back(stack)
	case len(ls) > 0 && modal(ls[0]):
		a.Host.Dismiss(ls[0]) // what it shows can take it (ui.OnBack)
	case a.Host.Env.Platform == ops.PlatformAndroid && a.Host.BackFromRoot():
	default:
		a.t.Fatalf("uitest: nothing to go back from\n%s", a.screenFor())
	}
	a.Settle()
}

// Dismiss dismisses what's on top as the user does (a swipe down, a tap
// outside, Escape): an open menu, else the top alert, sheet or popover.
func (a *App) Dismiss() {
	a.t.Helper()
	if a.menu != nil {
		a.menu = nil
		return
	}
	ls := a.touchable()
	if len(ls) == 0 || !modal(ls[0]) {
		a.t.Fatalf("uitest: nothing to dismiss\n%s", a.screenFor())
	}
	a.Host.Dismiss(ls[0])
	a.Settle()
}

// Key presses a keyboard shortcut, such as "primary+n" ("primary": ⌘
// on Apple's platforms, Ctrl elsewhere), "shift+tab", "escape", "ctrl+k"
// or "alt+up": the nearest view around the field typed into last (or
// the top layer) whose ui.Keys has it takes it. It reports whether one
// did.
func (a *App) Key(shortcut string) bool {
	a.t.Helper()
	parts := strings.Split(shortcut, "+")
	var mods uint8
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "primary", "cmd", "mod":
			mods |= 1
		case "shift":
			mods |= 2
		case "alt", "option", "opt":
			mods |= 4
		case "ctrl", "control":
			mods |= 8
		default:
			a.t.Fatalf("uitest: unknown modifier %q in %q", m, shortcut)
		}
	}
	from := a.focused
	if from == nil {
		if ls := a.touchable(); len(ls) > 0 && modal(ls[0]) {
			from = ls[0]
		}
	}
	took := a.Host.Key(from, parts[len(parts)-1], mods)
	a.Settle()
	return took
}

// Refresh pulls the scroll view or list that shows text to refresh it.
func (a *App) Refresh(text string) {
	a.t.Helper()
	t := a.one(text, func(t target) bool { return t.how == byView })
	v := t.v
	if cell, list := a.cellOf(v); cell != nil {
		v = list
	}
	for p := v; p != nil; p = p.Parent {
		if (p.Kind == ops.KindScroll || p.Kind == ops.KindList) && p.Listens[ops.EventRefresh] {
			a.Host.PullToRefresh(p)
			a.Settle()
			return
		}
	}
	a.t.Fatalf("uitest: nothing around %q refreshes", text)
}

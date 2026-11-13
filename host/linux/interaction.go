//go:build linux && !android

package linux

import (
	"strconv"
	"strings"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// A screen reader's actions on a view (A11yActions) and adjustable views
// (A11yAdjustable). GTK has no accessible actions of an app's own (none
// in GtkAccessible to 4.22), but its AT-SPI bridge gives a widget that
// has action groups of its own the Action interface, listing their
// enabled actions without a parameter by their names (the name, the
// localized name and the description are all the action's
// "group.name"; no key binding) and doing one with DoAction. So a view's
// actions are an action group of its own, "action", each named after
// its title as GAction names may be (ASCII letters and digits, a space a
// hyphen: "action.Mark-as-read"; a title with none of those its number,
// "action.2"): Orca lists them (its actions list) and does them. The
// view says it has a popup (GTK_ACCESSIBLE_PROPERTY_HAS_POPUP, AT-SPI's
// has-popup state) and its key (KEY_SHORTCUTS, "Shift+F10"), and its
// actions are its context menu, under their titles (a secondary click, a
// long press on a touchscreen, the Menu key, Shift+F10); the view is a
// tab stop so the keyboard reaches it. As on Windows, an adjustable view
// is a slider to screen readers and takes the arrow keys (up and the
// reading direction's forward one step up, the others down), each
// sending Adjust.

const (
	a11yPropHasPopup     = 2 // GTK_ACCESSIBLE_PROPERTY_HAS_POPUP (boolean)
	a11yPropKeyShortcuts = 3 // GTK_ACCESSIBLE_PROPERTY_KEY_SHORTCUTS
)

type a11yView struct {
	actions    []string
	names      []string // the actions' GAction names
	group      Ptr      // their group, the view's "action"
	adjustable bool
	keys       Ptr // its key controller
	secondary  Ptr // its secondary click
	long       Ptr // its long press (touch)
}

var a11yViews = map[*node]*a11yView{}

func init() {
	commonHooks = append(commonHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		switch p {
		case ops.PropA11yActions:
			a := h.a11yView(n)
			a.actions = ops.DecodeStrings(v.Bytes)
			h.exposeA11yActions(n, a)
		case ops.PropA11yAdjustable:
			a := h.a11yView(n)
			a.adjustable = v.Int != 0
			if a.adjustable && n.widget != 0 {
				setRole(n.widget, "slider")
			}
		default:
			return false
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) { delete(a11yViews, n) })
}

// a11yView is n's state, its controllers added.
func (h *Host) a11yView(n *node) *a11yView {
	a := a11yViews[n]
	if a != nil || n.widget == 0 {
		if a == nil {
			a = &a11yView{}
		}
		return a
	}
	a = &a11yView{}
	a11yViews[n] = a
	w := n.widget
	WidgetSetFocusable(w, true)
	WidgetSetFocusOnClick(w, false)
	WidgetAddCSSClass(w, "ryui-focus-ring")
	a.keys = EventControllerKeyNew()
	Connect(a.keys, "key-pressed", func(keyval, keycode, state uint32) bool {
		return h.a11yKey(n, a, keyval, state)
	})
	WidgetAddController(w, a.keys)
	a.secondary = GestureClickNew()
	GestureSingleSetButton(a.secondary, 3)
	Connect(a.secondary, "pressed", func(count int, x, y float64) {
		if h.actionsMenu(n, x, y) {
			GestureSetState(a.secondary, EventSequenceClaimed)
		}
	})
	WidgetAddController(w, a.secondary)
	a.long = GestureLongPressNew()
	GestureSingleSetTouchOnly(a.long, true)
	Connect(a.long, "pressed", func(x, y float64) {
		if n.events == nil || n.events.long == 0 {
			h.actionsMenu(n, x, y)
		}
	})
	WidgetAddController(w, a.long)
	return a
}

// a11yKey: the arrow keys adjust an adjustable view; the Menu key and
// Shift+F10 show a view's actions.
func (h *Host) a11yKey(n *node, a *a11yView, keyval, state uint32) bool {
	if a.adjustable && state&(ModControl|ModAlt|ModSuper) == 0 {
		var by int64
		switch keyval {
		case keyUp, keyKPUp:
			by = 1
		case keyDown, keyKPDown:
			by = -1
		case keyRight, keyKPRight, keyLeft, keyKPLeft:
			by = 1
			if (keyval == keyLeft || keyval == keyKPLeft) != n.rtl {
				by = -1
			}
		}
		if by != 0 {
			h.emit(n, ops.Event{Kind: ops.EventAdjust, Int: by})
			return true
		}
	}
	const keyMenu, keyF10 = 0xff67, 0xffc7
	if keyval == keyMenu || keyval == keyF10 && state&ModShift != 0 {
		return h.actionsMenu(n, -1, -1)
	}
	return false
}

// actionsMenu shows a view's actions as a popover menu: at x, y in it,
// or (x < 0, from the keyboard) under it. The action chosen sends
// A11yAction. A view with a context menu of its own (Menu) shows that.
func (h *Host) actionsMenu(n *node, x, y float64) bool {
	a := a11yViews[n]
	if a == nil || len(a.actions) == 0 || n.widget == 0 || len(n.menu) > 0 {
		return false
	}
	model := MenuNew()
	for i, title := range a.actions {
		if i < len(a.names) {
			item := MenuItemNew(title, "action."+a.names[i])
			MenuAppendItem(model, item)
			ObjectUnref(item)
		}
	}
	pop := PopoverMenuNewFromModel(model)
	ObjectUnref(model)
	WidgetSetParent(pop, n.widget)
	if x >= 0 {
		r := IntRect{X: int32(x), Y: int32(y), W: 1, H: 1}
		PopoverSetPointingTo(pop, &r)
		PopoverSetHasArrow(pop, false)
		if n.rtl {
			WidgetSetHAlign(pop, AlignEnd)
		} else {
			WidgetSetHAlign(pop, AlignStart)
		}
	} else {
		PopoverSetPosition(pop, 3) // GTK_POS_BOTTOM
	}
	Connect(pop, "closed", func() {
		IdleAdd(PriorityDefaultIdle, func() bool {
			WidgetUnparent(pop)
			return false
		})
	})
	h.popups = append(h.popups, pop)
	PopoverPopup(pop)
	return true
}

// exposeA11yActions makes a view's actions its "action" group (what
// AT-SPI lists and does), and says it has them.
func (h *Host) exposeA11yActions(n *node, a *a11yView) {
	if n.widget == 0 {
		return
	}
	a.names = a11yActionNames(a.actions)
	a.group = 0
	if len(a.actions) == 0 {
		WidgetInsertActionGroup(n.widget, "action", 0)
		AccessibleResetProperty(n.widget, a11yPropHasPopup)
		AccessibleResetProperty(n.widget, a11yPropKeyShortcuts)
		return
	}
	if n.kind == ops.KindView && !a.adjustable {
		// GTK names no generic view (ARIA's rule): a group, so its label
		// is read with its actions.
		setRole(n.widget, "group")
	}
	group := SimpleActionGroupNew()
	id := n.id
	for i, name := range a.names {
		act := SimpleActionNew(name, 0)
		i := i
		Connect(act, "activate", func(param uintptr) {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventA11yAction, Int: int64(i)})
			}
		})
		ActionMapAddAction(group, act)
		ObjectUnref(act)
	}
	WidgetInsertActionGroup(n.widget, "action", group) // the view holds it
	a.group = group
	ObjectUnref(group)
	var val Value
	ValueInit(&val, TypeBoolean)
	ValueSetBoolean(&val, true)
	prop := int32(a11yPropHasPopup)
	AccessibleUpdateProperty(n.widget, 1, &prop, &val)
	ValueUnset(&val)
	setA11yString(n.widget, a11yPropKeyShortcuts, "Shift+F10")
}

// a11yActionNames are titles as GAction names (g_action_name_is_valid:
// ASCII letters, digits, '-' and '.'), each its own: a space or a dot
// is a hyphen, other characters go; a title left with none its number.
func a11yActionNames(titles []string) []string {
	names := make([]string, len(titles))
	seen := map[string]bool{}
	for i, t := range titles {
		var b strings.Builder
		hyphen := false
		for _, r := range strings.TrimSpace(t) {
			switch {
			case r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'):
				b.WriteRune(r)
				hyphen = false
			case r == ' ' || r == '.' || r == '-' || r == '_' || r == '/':
				if b.Len() > 0 && !hyphen {
					b.WriteByte('-')
					hyphen = true
				}
			}
		}
		name := strings.TrimSuffix(b.String(), "-")
		if name == "" {
			name = strconv.Itoa(i + 1)
		}
		for base, k := name, 2; seen[name]; k++ {
			name = base + "-" + strconv.Itoa(k)
		}
		seen[name] = true
		names[i] = name
	}
	return names
}

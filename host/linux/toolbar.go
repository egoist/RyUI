//go:build linux && !android

package linux

import (
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A screen's Toolbar and Primary action are a GtkActionBar at the bottom
// of its page, as GNOME's apps put a view's actions (the HIG's action
// bar): the page's child is an AdwToolbarView with the screen's view as
// its content and the action bar as its bottom bar, so the bar slides
// with the page and the screen's view is sized above it. The Toolbar's
// buttons are packed at the start, the Primary action at the end as a
// suggested-action button with its icon and title.

// screenBar is a screen's bottom bar: its AdwToolbarView, its GtkActionBar
// and the buttons in it.
type screenBar struct {
	view    Ptr // the AdwToolbarView (a reference of the host's)
	bar     Ptr // its GtkActionBar
	packed  []Ptr
	toolbar []action // numbered as their Action events are (ops.ScreenToolbarActions + the number)
	primary []action // ops.ScreenPrimaryAction + the number
}

// pageChild is what a screen's page shows: its view, or the toolbar view
// around it once it has a bottom bar.
func (n *node) pageChild() Ptr {
	if n.nav != nil && n.nav.bottom != nil {
		return n.nav.bottom.view
	}
	return n.widget
}

// setScreenBar shows a screen's Toolbar and Primary in its bottom bar,
// which is made with the first, and hidden (sliding down) while there are
// none.
func (h *Host) setScreenBar(n *node) {
	b := n.nav.bottom
	if b == nil {
		if len(n.nav.toolbar) == 0 && len(n.nav.primary) == 0 {
			return
		}
		b = &screenBar{view: AdwToolbarViewNew(), bar: ActionBarNew()}
		ObjectRefSink(b.view)
		AdwToolbarViewAddBottomBar(b.view, b.bar)
		// The view goes in the toolbar view, which takes its place in the page.
		ObjectRef(n.widget)
		if p := WidgetGetParent(n.widget); p != 0 {
			h.detach(n.widget)
		}
		AdwToolbarViewSetContent(b.view, n.widget)
		ObjectUnref(n.widget)
		n.nav.bottom = b
		if n.nav.page != 0 && n.parent != nil && n.parent.kind == ops.KindStack {
			AdwNavigationPageSetChild(n.nav.page, b.view)
		}
	}
	for _, w := range b.packed {
		ActionBarRemove(b.bar, w)
	}
	b.packed = b.packed[:0]
	b.toolbar, b.primary = n.nav.toolbar, n.nav.primary
	for i := range b.toolbar {
		w := h.barButton(n, &b.toolbar[i], false)
		ActionBarPackStart(b.bar, w)
		b.packed = append(b.packed, w)
	}
	if len(b.primary) > 0 {
		w := h.barButton(n, &b.primary[0], true)
		ActionBarPackEnd(b.bar, w)
		b.packed = append(b.packed, w)
	}
	ActionBarSetRevealed(b.bar, len(b.packed) > 0)
}

// barButton makes a button of an action bar: its icon (its title as its
// tooltip and what a screen reader says), else its title; one with
// children opens them as a menu. The primary one is a suggested-action
// button with its icon beside its title.
func (h *Host) barButton(screen *node, a *action, primary bool) Ptr {
	id := screen.id
	var b Ptr
	if a.is(ops.MenuSubmenu) {
		b = MenuButtonNew()
		if a.icon != "" && !primary {
			MenuButtonSetIconName(b, h.iconName(a.icon))
			WidgetSetTooltipText(b, a.title)
		} else {
			MenuButtonSetLabel(b, a.title)
		}
		h.insertMenuActions(b, "asukatool", id, a.items)
		model := h.menuModel(a.items, "asukatool.item")
		MenuButtonSetMenuModel(b, model)
		ObjectUnref(model)
	} else {
		b = ButtonNew()
		switch {
		case a.icon != "" && primary:
			c := ButtonContentNew()
			ButtonContentSetIconName(c, h.iconName(a.icon))
			ButtonContentSetLabel(c, a.title)
			ButtonSetChild(b, c)
		case a.icon != "":
			ButtonSetIconName(b, h.iconName(a.icon))
			WidgetSetTooltipText(b, a.title)
		default:
			ButtonSetLabel(b, a.title)
		}
		index := a.index
		Connect(b, "clicked", func() {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventAction, Int: index})
			}
		})
	}
	setA11yString(b, a11yPropLabel, a.title)
	switch {
	case primary:
		WidgetAddCSSClass(b, "suggested-action")
	case a.role == ops.AlertButtonRoleDestructive:
		WidgetAddCSSClass(b, "destructive-action")
	}
	WidgetSetSensitive(b, !a.is(ops.MenuDisabled))
	return b
}

// releaseScreenBar lets go of a screen's toolbar view.
func (h *Host) releaseScreenBar(n *node) {
	if n.nav == nil || n.nav.bottom == nil {
		return
	}
	b := n.nav.bottom
	n.nav.bottom = nil
	if n.widget != 0 && WidgetGetParent(n.widget) == b.view {
		AdwToolbarViewSetContent(b.view, 0)
	}
	ObjectUnref(b.view)
}

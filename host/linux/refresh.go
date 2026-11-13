//go:build linux && !android

package linux

import (
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Pull to refresh (OnRefresh) on a Scroll, the desktop's way, as on
// Windows: F5 or Ctrl+R refreshes the refreshable Scroll or List the
// focus is in, or else the innermost one on screen in the window. While
// it refreshes, a spinner floats over the top of its content, on a card
// (libadwaita's), until Refreshing goes false. A List's own are in
// list.go; this finds them too.

type refresher struct {
	refreshing bool
	shortcut   Ptr // the window-wide shortcut controller on its view
	spinner    Ptr // the card with the spinner, over the scroll view
}

var refreshers = map[*node]*refresher{}

func init() {
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if n.scroll == nil {
			return false
		}
		switch p {
		case ops.PropScrollRefreshable:
			h.setRefreshable(n, v.Int != 0)
		case ops.PropScrollRefreshing:
			h.setRefreshing(n, v.Int != 0)
		default:
			return false
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) { delete(refreshers, n) })
}

func (h *Host) setRefreshable(n *node, on bool) {
	r := refreshers[n]
	if !on {
		if r != nil {
			h.setRefreshing(n, false)
			if r.shortcut != 0 && n.widget != 0 {
				WidgetRemoveController(n.widget, r.shortcut)
			}
			delete(refreshers, n)
		}
		return
	}
	if r != nil || n.widget == 0 {
		return
	}
	r = &refresher{}
	refreshers[n] = r
	sc := ShortcutControllerNew()
	ShortcutControllerSetScope(sc, 2) // GTK_SHORTCUT_SCOPE_GLOBAL: anywhere in its window
	for _, key := range []string{"F5", "<Control>r"} {
		act := ShortcutAction(func() bool { return h.refreshKey(n) })
		ShortcutControllerAddShortcut(sc, ShortcutNew(ShortcutTriggerParseString(key), act))
	}
	WidgetAddController(n.widget, sc)
	r.shortcut = sc
}

// refreshKey is F5 or Ctrl+R in the window of from (one of the
// refreshable views in it): it refreshes the one the focus is in, or the
// innermost one on screen.
func (h *Host) refreshKey(from *node) bool {
	if from.widget == 0 {
		return false
	}
	root := WidgetGetRoot(from.widget)
	if root == 0 {
		return false
	}
	// The focus's: the innermost refreshable view around it.
	for f := WindowGetFocus(root); f != 0; f = WidgetGetParent(f) {
		if n := h.byWidget[f]; n != nil && h.isRefreshable(n) {
			return h.startRefresh(n)
		}
	}
	// Else the innermost one on screen: the deepest refreshable view in
	// the window that's mapped.
	var best *node
	depth := -1
	for n := range h.refreshables() {
		if n.widget == 0 || !WidgetGetMapped(n.widget) || WidgetGetRoot(n.widget) != root {
			continue
		}
		d := 0
		for w := WidgetGetParent(n.widget); w != 0; w = WidgetGetParent(w) {
			d++
		}
		if d > depth {
			best, depth = n, d
		}
	}
	return best != nil && h.startRefresh(best)
}

// refreshables are the refreshable Scrolls and Lists.
func (h *Host) refreshables() map[*node]bool {
	out := map[*node]bool{}
	for n := range refreshers {
		out[n] = true
	}
	for _, n := range h.nodes {
		if l, ok := n.ext.(*list); ok && n.kind == ops.KindList && l.refreshable {
			out[n] = true
		}
	}
	return out
}

func (h *Host) isRefreshable(n *node) bool {
	if refreshers[n] != nil {
		return true
	}
	l, ok := n.ext.(*list)
	return ok && n.kind == ops.KindList && l.refreshable
}

// startRefresh refreshes n, as the user asked: Refresh, the spinner
// shown until the core says it's done.
func (h *Host) startRefresh(n *node) bool {
	if l, ok := n.ext.(*list); ok && n.kind == ops.KindList {
		return l.refresh(h)
	}
	r := refreshers[n]
	if r == nil || r.refreshing || !n.listening(ops.EventRefresh) {
		return false
	}
	h.setRefreshing(n, true)
	h.emit(n, ops.Event{Kind: ops.EventRefresh})
	return true
}

// setRefreshing shows the spinner over the scroll's top, or takes it
// away.
func (h *Host) setRefreshing(n *node, on bool) {
	r := refreshers[n]
	if r == nil || r.refreshing == on || n.widget == 0 {
		return
	}
	r.refreshing = on
	if on && r.spinner == 0 {
		box := BoxNew(OrientationHorizontal, 0)
		WidgetAddCSSClass(box, "card")
		var sp Ptr
		if AdwSpinnerNew != nil {
			sp = AdwSpinnerNew()
		} else {
			sp = SpinnerNew()
			SpinnerSetSpinning(sp, true)
		}
		WidgetSetSizeRequest(sp, 24, 24)
		for _, f := range []func(Ptr, int32){WidgetSetMarginTop, WidgetSetMarginBottom, WidgetSetMarginStart, WidgetSetMarginEnd} {
			f(sp, 8)
		}
		BoxAppend(box, sp)
		WidgetSetHAlign(box, AlignCenter)
		WidgetSetVAlign(box, AlignStart)
		WidgetSetMarginTop(box, 12)
		WidgetSetCanTarget(box, false)
		WidgetSetParent(box, n.widget) // after the scrolled window: over it
		r.spinner = box
	}
	if r.spinner != 0 {
		WidgetSetVisible(r.spinner, on)
		if AdwSpinnerNew == nil {
			SpinnerSetSpinning(WidgetGetFirstChild(r.spinner), on)
		}
	}
}

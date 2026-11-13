//go:build linux && !android

package linux

import (
	"slices"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// chrome is what a window's header bar shows: the innermost stack on
// screen's back button (while there's a screen to go back to), its top
// screen's title, bar buttons and search, the tabs' switcher on a wide
// window, and the app's menu (MenuBar) as GNOME's main menu.
type chrome struct {
	back     bool
	title    string
	actions  []action
	screen   *node
	stack    *node
	tabs     *node // tabs whose switcher is in the header
	split    *node // a split view whose content the header is over
	search   string
	menus    int // the app menus' version
	packed   []Ptr
	searchBt Ptr
}

func (c *chrome) same(o *chrome) bool {
	return c.back == o.back && c.title == o.title && slices.Equal(c.actions, o.actions) &&
		c.screen == o.screen && c.stack == o.stack && c.tabs == o.tabs && c.search == o.search && c.menus == o.menus &&
		c.split == o.split
}

// queueChrome updates the header bars once the batch (or the core's
// call) is done.
func (h *Host) queueChrome() {
	if h.chromeQueued {
		return
	}
	h.chromeQueued = true
	h.afterCore(func() {
		h.chromeQueued = false
		h.updateChrome()
	})
}

func (h *Host) updateChrome() {
	if h.main != nil {
		h.updateWinChrome(h.main)
	}
	for _, w := range h.wins {
		if w != h.main {
			h.updateWinChrome(w)
		}
	}
}

// find walks what's on screen from n: a stack's top screen, the tabs'
// selected one, a node's children.
func (h *Host) find(n *node, c *chrome) {
	if n == nil {
		return
	}
	switch n.kind {
	case ops.KindStack:
		if len(n.children) == 0 {
			return
		}
		top := n.children[len(n.children)-1]
		for i := len(n.children) - 1; i >= 0; i-- {
			if x := n.children[i]; x.nav != nil && x.nav.page != 0 && !x.nav.gone {
				top = x
				break
			}
		}
		depth := 0
		for _, x := range n.children {
			if x.nav != nil && x.nav.page != 0 && !x.nav.gone {
				depth++
			}
		}
		c.stack, c.screen = n, top
		c.back = depth > 1
		c.title = top.title
		c.actions = top.actions
		c.search = top.nav.searchPh
		h.find1(top, c)
		return
	case ops.KindTabs:
		if !n.nav.narrow && c.stack == nil {
			c.tabs = n
		}
		if n.selected >= 0 && n.selected < len(n.children) {
			s := n.children[n.selected]
			if c.screen == nil {
				c.title = s.title
				c.actions = s.actions
				c.screen = s
				c.search = s.nav.searchPh
			}
			h.find1(s, c)
		}
		return
	case ops.KindSheet, ops.KindWindow, ops.KindAlert, ops.KindPopover:
		return // their own
	case ops.KindSplit:
		// The detail's chrome, in a header bar over it (split.go).
		h.find1(n, c)
		c.split = n
		return
	}
	h.find1(n, c)
}

func (h *Host) find1(n *node, c *chrome) {
	for _, x := range n.children {
		if x.hidden {
			continue
		}
		inner := chrome{}
		h.find(x, &inner)
		if inner.stack != nil {
			tabs := c.tabs
			*c = inner
			if c.tabs == nil && !c.back {
				c.tabs = tabs
			}
			return
		}
		if inner.split != nil && c.split == nil {
			c.split = inner.split
		}
		if inner.tabs != nil && c.tabs == nil {
			c.tabs = inner.tabs
			if c.screen == nil {
				c.title, c.actions, c.screen, c.search = inner.title, inner.actions, inner.screen, inner.search
			}
		}
	}
}

func (h *Host) updateWinChrome(w *win) {
	var c chrome
	h.find(w.root, &c)
	if w.node != nil {
		for _, x := range w.node.children {
			h.find(x, &c)
		}
	}
	if w == h.main {
		c.menus = h.menusVersion
	}
	h.placeHeader(w, c.split)
	if c.same(&w.chrome) {
		return
	}
	for _, x := range w.chrome.packed {
		AdwHeaderBarRemove(w.header, x)
	}
	c.packed = nil
	if c.tabs != nil && c.tabs.nav.switcher != 0 && !c.back {
		AdwHeaderBarSetTitleWidget(w.header, c.tabs.nav.switcher)
	} else {
		AdwHeaderBarSetTitleWidget(w.header, w.title)
	}
	if c.back {
		b := ButtonNew()
		ButtonSetIconName(b, "go-previous-symbolic")
		WidgetSetTooltipText(b, "Back")
		stack := c.stack
		Connect(b, "clicked", func() { h.goBack(stack) })
		AdwHeaderBarPackStart(w.header, b)
		c.packed = append(c.packed, b)
	}
	if w == h.main && h.appMenu != 0 {
		mb := MenuButtonNew()
		MenuButtonSetIconName(mb, "open-menu-symbolic")
		MenuButtonSetMenuModel(mb, h.appMenu)
		WidgetSetTooltipText(mb, "Main Menu")
		AdwHeaderBarPackEnd(w.header, mb)
		c.packed = append(c.packed, mb)
	}
	screen := c.screen
	for i := len(c.actions) - 1; i >= 0; i-- {
		a := c.actions[i]
		b := ButtonNew()
		if a.icon != "" {
			ButtonSetIconName(b, h.iconName(a.icon))
			WidgetSetTooltipText(b, a.title)
		} else {
			ButtonSetLabel(b, a.title)
		}
		if a.role == ops.AlertButtonRoleDestructive {
			WidgetAddCSSClass(b, "destructive-action")
		}
		i := i
		Connect(b, "clicked", func() {
			if screen != nil {
				h.emit(screen, ops.Event{Kind: ops.EventAction, Int: int64(i)})
			}
		})
		AdwHeaderBarPackEnd(w.header, b)
		c.packed = append(c.packed, b)
	}
	if c.search != "" && screen != nil {
		bt := h.searchFor(w, screen)
		AdwHeaderBarPackEnd(w.header, bt)
		c.packed = append(c.packed, bt)
	} else {
		h.hideSearch(w)
	}
	w.chrome = c
	h.updateTitle(w)
}

// goBack is the header's back button, or Alt+Left: the stack's top
// screen asks the core when it takes going back itself, else it's
// popped.
func (h *Host) goBack(stack *node) bool {
	if stack == nil || len(stack.children) < 2 {
		return false
	}
	top := stack.children[len(stack.children)-1]
	if top.interceptBack {
		h.emit(top, ops.Event{Kind: ops.EventBack})
		return true
	}
	AdwNavigationViewPop(stack.nav.view)
	return true
}

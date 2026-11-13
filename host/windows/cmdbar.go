//go:build windows

package windows

import (
	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A screen's Toolbar and Primary on Windows: a command bar under its
// stack's screens (Fluent's CommandBar at the bottom of a page). The
// Toolbar's buttons go from the leading edge, each its icon (Segoe Fluent
// Icons) with its title beside it (the command bar's labels on the right);
// those that don't fit go in the menu of a More options button (…) after
// them. The Primary action is an accent button at the trailing end. The
// bar shows while the stack's top screen has either, and the screens are
// sized above it. Each is a BUTTON, which a screen reader names by its
// text, and Tab reaches.

// cmdBarHeight is the command bar's height in points.
const cmdBarHeight = 48

// cmdBar is a stack's command bar's state (its node's nav.cmd).
type cmdBar struct {
	stack   *node
	screen  *node     // the screen whose toolbar it shows
	buttons []uintptr // the toolbar's, in order
	fit     int       // how many of them fit before the overflow button
	more    uintptr   // More options (…)
	primary uintptr
}

func (h *Host) makeCommandBar(stack *node) *node {
	h.nextCell++
	b := newNode(ops.HostIDBit|h.nextCell, kindCommandBar)
	b.hwnd = h.makeView(0)
	b.nav = &nav{stack: stack, cmd: &cmdBar{stack: stack}}
	b.k = stack.k
	b.dir = stack.dir
	SetParent(b.hwnd, stack.hwnd)
	c := b.nav.cmd
	c.more = h.cmdButton(b, l10n.MoreOptions(h.locale))
	c.primary = h.cmdButton(b, "")
	h.add(b)
	return b
}

// cmdButton makes a button of a command bar.
func (h *Host) cmdButton(b *node, title string) uintptr {
	btn := CreateWindow(0, "BUTTON", title, WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, b.hwnd, 1)
	SendMessage(btn, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
	h.byHwnd[btn] = b
	return btn
}

// cmdBarShown reports whether a stack's top screen has a toolbar or a
// primary action for its command bar.
func (h *Host) cmdBarShown(stack *node) bool {
	s := h.topScreen(stack)
	return s != nil && s.nav != nil && (len(s.nav.toolbar) > 0 || len(s.nav.primary) > 0)
}

// cmdBarHeightOf is the height of a stack's command bar, in points (0:
// none).
func (h *Host) cmdBarHeightOf(stack *node) float32 {
	if stack.kind == ops.KindStack && h.cmdBarShown(stack) {
		return cmdBarHeight
	}
	return 0
}

// updateCommandBar shows the top screen's toolbar and primary action in
// its stack's command bar, made with the first.
func (h *Host) updateCommandBar(stack *node) {
	shown := h.cmdBarShown(stack)
	f := stack.nav.foot
	if f == nil {
		if !shown {
			return
		}
		f = h.makeCommandBar(stack)
		stack.nav.foot = f
	}
	c := f.nav.cmd
	screen := h.topScreen(stack)
	c.screen = screen
	var toolbar, primary []action
	if screen != nil && screen.nav != nil {
		toolbar, primary = screen.nav.toolbar, screen.nav.primary
	}
	for len(c.buttons) > len(toolbar) {
		k := len(c.buttons) - 1
		delete(h.byHwnd, c.buttons[k])
		DestroyWindow(c.buttons[k])
		c.buttons = c.buttons[:k]
	}
	for len(c.buttons) < len(toolbar) {
		c.buttons = append(c.buttons, h.cmdButton(f, ""))
	}
	for i, a := range toolbar {
		SetWindowText(c.buttons[i], a.title)
		EnableWindow(c.buttons[i], !a.is(ops.MenuDisabled))
		Invalidate(c.buttons[i])
	}
	if len(primary) > 0 {
		SetWindowText(c.primary, primary[0].title)
		EnableWindow(c.primary, !primary[0].is(ops.MenuDisabled))
		ShowWindow(c.primary, SW_SHOWNA)
		Invalidate(c.primary)
	} else {
		ShowWindow(c.primary, SW_HIDE)
	}
	if shown {
		ShowWindow(f.hwnd, SW_SHOWNA)
	} else {
		ShowWindow(f.hwnd, SW_HIDE)
	}
	Invalidate(f.hwnd)
}

// cmdButtonWidth is a command bar button's width in pixels: its icon and
// title, padded as Fluent's AppBarButton with its label on the right.
func (h *Host) cmdButtonWidth(b *node, a *action) int32 {
	dc := h.measureDC
	old := SelectObject(dc, h.bodyFont(b))
	tw, _ := TextExtent(dc, UTF16Units(a.title))
	SelectObject(dc, old)
	w := tw + h.px(b, 24)
	if symbolGlyph(a.icon) != 0 {
		w += h.px(b, 16+8)
	}
	return w
}

// layoutCommandBar places the toolbar's buttons from the leading edge,
// the ones that fit, then the overflow button if some don't, and the
// primary action at the trailing edge.
func (h *Host) layoutCommandBar(b *node) {
	c := b.nav.cmd
	r := ClientRect(b.hwnd)
	size := h.px(b, 40)
	y := (r.H() - size) / 2
	rtl := h.isRTL(b)
	across := func(x, w int32) int32 {
		if rtl {
			return r.W() - x - w
		}
		return x
	}
	var toolbar, primary []action
	if s := c.screen; s != nil && s.nav != nil {
		toolbar, primary = s.nav.toolbar, s.nav.primary
	}
	end := r.W() - h.px(b, 8)
	if len(primary) > 0 {
		w := h.cmdButtonWidth(b, &primary[0])
		end -= w
		SetWindowPos(c.primary, 0, across(end, w), y, w, size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		end -= h.px(b, 8)
	}
	moreW := h.px(b, 40)
	widths := make([]int32, len(toolbar))
	total := int32(0)
	for i := range toolbar {
		widths[i] = h.cmdButtonWidth(b, &toolbar[i])
		total += widths[i] + h.px(b, 4)
	}
	x := h.px(b, 8)
	c.fit = len(toolbar)
	if x+total > end {
		// What fits before the overflow button.
		room := end - moreW - h.px(b, 4) - x
		c.fit = 0
		for c.fit < len(toolbar) && widths[c.fit]+h.px(b, 4) <= room {
			room -= widths[c.fit] + h.px(b, 4)
			c.fit++
		}
	}
	for i, btn := range c.buttons {
		if i >= c.fit {
			ShowWindow(btn, SW_HIDE)
			continue
		}
		SetWindowPos(btn, 0, across(x, widths[i]), y, widths[i], size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS|SWP_SHOWWINDOW)
		x += widths[i] + h.px(b, 4)
	}
	if c.fit < len(toolbar) {
		SetWindowPos(c.more, 0, across(x, moreW), y, moreW, size, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS|SWP_SHOWWINDOW)
	} else {
		ShowWindow(c.more, SW_HIDE)
	}
}

// commandBarButton is what a command bar's button shows: a toolbar
// button's or the primary action's icon and title, the overflow button's
// More glyph.
func (c *cmdBar) button(ctl uintptr, rtl bool) (uint16, string) {
	s := c.screen
	if s == nil || s.nav == nil {
		return 0, ""
	}
	if ctl == c.more {
		return 0xE712, "" // More
	}
	if ctl == c.primary && len(s.nav.primary) > 0 {
		a := &s.nav.primary[0]
		return directedGlyph(a.icon, rtl), a.title
	}
	for i, b := range c.buttons {
		if b == ctl && i < len(s.nav.toolbar) {
			a := &s.nav.toolbar[i]
			return directedGlyph(a.icon, rtl), a.title
		}
	}
	return 0, ""
}

// commandBarCommand is a click on a command bar's button: its Action
// (ops.ScreenToolbarActions or ops.ScreenPrimaryAction + the item's
// number), or its menu (its children; the overflow's: the toolbar's
// buttons that don't fit).
func (h *Host) commandBarCommand(b *node, ctl uintptr) {
	c := b.nav.cmd
	s := c.screen
	if s == nil || s.nav == nil {
		return
	}
	pressed := func(a *action, base int64, btn uintptr) {
		if a.is(ops.MenuSubmenu) {
			h.popUpActionsFrom(s, btn, b, a.items, base)
			return
		}
		h.emit(s, ops.Event{Kind: ops.EventAction, Int: base + a.index})
	}
	switch {
	case ctl == c.more:
		if c.fit < len(s.nav.toolbar) {
			h.popUpActionsFrom(s, ctl, b, s.nav.toolbar[c.fit:], ops.ScreenToolbarActions)
		}
	case ctl == c.primary:
		if len(s.nav.primary) > 0 {
			pressed(&s.nav.primary[0], ops.ScreenPrimaryAction, ctl)
		}
	default:
		for i, btn := range c.buttons {
			if btn == ctl && i < len(s.nav.toolbar) {
				pressed(&s.nav.toolbar[i], ops.ScreenToolbarActions, ctl)
			}
		}
	}
}

// paintCommandBar draws a hairline over the bar (g).
func (h *Host) paintCommandBar(b *node, g Graphics, r Rect) {
	if g == 0 {
		return
	}
	k := h.scaleOf(b)
	br := SolidBrush(h.theme.separator.argb())
	g.FillRect(br, 0, 0, float32(r.W()), max(k, 1))
	br.Close()
}

// tapCommandBar presses the command bar's button with this title (the
// automation's tap), the overflow's items aside.
func (h *Host) tapCommandBar(b *node, text string) bool {
	c := b.nav.cmd
	s := c.screen
	if s == nil || s.nav == nil || !IsWindowVisible(b.hwnd) {
		return false
	}
	if len(s.nav.primary) > 0 && s.nav.primary[0].title == text {
		SendMessage(c.primary, BM_CLICK, 0, 0)
		return true
	}
	for i, a := range s.nav.toolbar {
		if a.title == text && i < len(c.buttons) {
			if i < c.fit {
				SendMessage(c.buttons[i], BM_CLICK, 0, 0)
			} else if !a.is(ops.MenuSubmenu) && !a.is(ops.MenuDisabled) {
				h.emit(s, ops.Event{Kind: ops.EventAction, Int: ops.ScreenToolbarActions + a.index})
			}
			return true
		}
	}
	return false
}

//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A screen's search field (ScreenOptions.Search) is in its stack's
// header, as WinUI puts an AutoSuggestBox in a page's command bar: a text
// box (the host's own TextField, drawn as Fluent's) at the header's
// trailing side before the bar buttons, up to 280 points wide, with the
// search glyph inside it. Typing is the screen's TextChanged, Enter its
// Submit; SearchText sets its text. The header has one field, shown for
// the top screens that ask for one.

const (
	searchWidth = 280 // points, at most
	searchIcon  = 32  // points the glyph takes at its trailing side
)

// updateSearch shows the screen's search field in its header b, or
// hides it.
func (h *Host) updateSearch(b *node, screen *node) {
	want := screen != nil && screen.nav.search != ""
	f := b.nav.field
	if !want {
		if f != nil && f.hwnd != 0 {
			if Focus() == f.ctl {
				SetFocus(b.nav.stack.hwnd) // the focus doesn't stay in a hidden field
			}
			ShowWindow(f.hwnd, SW_HIDE)
		}
		return
	}
	if f == nil {
		h.nextCell++
		f = newNode(ops.HostIDBit|h.nextCell, ops.KindTextField)
		f.k, f.dir = b.k, b.dir
		f.searchOf = b
		f.inset = searchInset(h.isRTL(b))
		h.makeField(f)
		h.byHwnd[f.hwnd] = f
		SetParent(f.hwnd, b.hwnd)
		b.nav.field = f
	}
	if f.placeholder != screen.nav.search {
		f.placeholder = screen.nav.search
		SendMessage(f.ctl, EM_SETCUEBANNER, 1, Str(f.placeholder))
	}
	if f.text != screen.nav.searchText {
		f.text = screen.nav.searchText
		f.settingText = true
		SetWindowText(f.ctl, f.text)
		f.settingText = false
	}
	ShowWindow(f.hwnd, SW_SHOWNA)
}

// searchInset is the room the glyph takes in the field (top, right,
// bottom, left points), at its trailing side.
func searchInset(rtl bool) [4]float32 {
	if rtl {
		return [4]float32{0, 0, 0, searchIcon - 10}
	}
	return [4]float32{0, searchIcon - 10, 0, 0}
}

// layoutSearch places the field before the bar buttons, which end at x
// (pixels from the leading edge, as layoutHeader goes); across mirrors x
// right to left.
func (h *Host) layoutSearch(b *node, x int32, across func(x, w int32) int32) {
	f := b.nav.field
	if f == nil || f.hwnd == 0 {
		return
	}
	// The title keeps 120 points, after the back button.
	start := h.px(b, 16+120)
	if h.liveScreens(b.nav.stack) > 1 {
		start += h.px(b, 36)
	}
	w := min(h.px(b, searchWidth), x-h.px(b, 4)-start)
	if w < h.px(b, 120) {
		w = max(x-h.px(b, 4)-h.px(b, 52), 1) // a narrow window: the field over the title
	}
	ht := h.px(b, 32)
	r := ClientRect(b.hwnd)
	f.dir = b.dir
	f.inset = searchInset(h.isRTL(b))
	SetWindowPos(f.hwnd, 0, across(x-h.px(b, 4)-w, w), (r.H()-ht)/2, w, ht, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.layoutField(f)
}

// paintSearchIcon draws the search glyph at the field's trailing side.
func (h *Host) paintSearchIcon(f *node, dc uintptr, r Rect) {
	rtl := h.isRTL(f)
	gr := Rect{Left: r.Right - h.px(f, searchIcon), Top: r.Top, Right: r.Right - h.px(f, 4), Bottom: r.Bottom}
	if rtl {
		gr = flipIn(gr, r)
	}
	old := SelectObject(dc, h.iconFont(f, 14*h.fontScale()))
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, h.theme.secondary.ref())
	DrawText(dc, []uint16{0xE721}, &gr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX) // Search
	SelectObject(dc, old)
}

// searchEvent is an event of a header's search field: typing is the
// header's screen's TextChanged, Enter its Submit.
func (h *Host) searchEvent(f *node, ev ops.Event) {
	screen := f.searchOf.nav.screen
	if screen == nil {
		return
	}
	switch ev.Kind {
	case ops.EventTextChanged:
		screen.nav.searchText = ev.Str
		h.emit(screen, ev)
	case ops.EventSubmit:
		h.emit(screen, ev)
	}
}

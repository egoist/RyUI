//go:build windows

package windows

import (
	"time"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A DatePicker is the date and time picker control (SysDateTimePick32):
// the locale's short date, its time, or both, its calendar dropping down.
// Inline (DatePickerProps.Inline), a date is a month calendar in place
// (SysMonthCal32, as WinUI's CalendarView), with a time picker under it
// for a date and time; a time alone stays the time picker, Windows having
// no clock to show in place.

// makeDateControls makes n's controls for its mode and Inline, again
// when they change.
func (h *Host) makeDateControls(n *node) {
	calendar := n.dateInline && ops.DateMode(n.dateMode) != ops.DateModeTime
	if n.ctl != 0 {
		isCalendar := ClassName(n.ctl) == "SysMonthCal32"
		if isCalendar == calendar {
			if !calendar {
				h.dateFormat(n)
			} else {
				h.makeTimeUnder(n)
			}
			return
		}
		h.dropDateControls(n)
	}
	if calendar {
		h.makeControlIn(n, "SysMonthCal32", WS_TABSTOP|MCS_NOTODAY, 0)
		h.makeTimeUnder(n)
	} else {
		h.makeControlIn(n, "SysDateTimePick32", WS_TABSTOP|DTS_SHORTDATEFORMAT, 0)
		h.dateFormat(n)
	}
	h.byHwnd[n.ctl] = n
	h.themeControl(n)
	h.applyDateRange(n)
	h.applyDate(n)
	h.layoutField(n)
}

// makeTimeUnder makes or drops the time picker under a calendar: one for
// a date and time.
func (h *Host) makeTimeUnder(n *node) {
	want := ops.DateMode(n.dateMode) == ops.DateModeDateTime
	switch {
	case want && n.ctl2 == 0:
		n.ctl2 = CreateWindow(0, "SysDateTimePick32", "", WS_CHILD|WS_VISIBLE|WS_CLIPSIBLINGS|WS_TABSTOP|DTS_TIMEFORMAT, 0, 0, 0, 0, n.hwnd, 2)
		SendMessage(n.ctl2, WM_SETFONT, h.nodeFont(n), 0)
		SendMessage(n.ctl2, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
		SendMessage(n.ctl2, DTM_SETFORMATW, 0, Str(LocaleInfo("", localeSShortTime)))
		EnableWindow(n.ctl2, !n.disabled)
		h.themeDateField(n, n.ctl2)
		h.byHwnd[n.ctl2] = n
		h.applyDate(n)
		h.layoutField(n)
	case !want && n.ctl2 != 0:
		delete(h.byHwnd, n.ctl2)
		DestroyWindow(n.ctl2)
		n.ctl2 = 0
		h.layoutField(n)
	}
}

func (h *Host) dropDateControls(n *node) {
	for _, c := range []uintptr{n.ctl, n.ctl2} {
		if c != 0 {
			delete(h.byHwnd, c)
			DestroyWindow(c)
		}
	}
	n.ctl, n.ctl2 = 0, 0
}

// applyDate shows n's value in its controls.
func (h *Host) applyDate(n *node) {
	if n.ctl == 0 {
		return
	}
	ms := int64(n.value)
	if ms == 0 {
		ms = time.Now().UnixMilli()
	}
	st := systemTime(ms)
	if n.ctl2 != 0 || ClassName(n.ctl) == "SysMonthCal32" {
		SendMessage(n.ctl, MCM_SETCURSEL, 0, Ptr(&st))
		if n.ctl2 != 0 {
			SendMessage(n.ctl2, DTM_SETSYSTEMTIME, GDT_VALID, Ptr(&st))
		}
		return
	}
	SendMessage(n.ctl, DTM_SETSYSTEMTIME, GDT_VALID, Ptr(&st))
}

// applyDateRange bounds n's controls' choice by Min and Max.
func (h *Host) applyDateRange(n *node) {
	var r [2]SystemTime
	flags := uintptr(0)
	if n.min != 0 {
		r[0], flags = systemTime(int64(n.min)), flags|GDTR_MIN
	}
	if n.max != 0 {
		r[1], flags = systemTime(int64(n.max)), flags|GDTR_MAX
	}
	switch {
	case n.ctl == 0:
	case ClassName(n.ctl) == "SysMonthCal32":
		SendMessage(n.ctl, MCM_SETRANGE, flags, Ptr(&r[0]))
		if n.ctl2 != 0 {
			SendMessage(n.ctl2, DTM_SETRANGE, flags, Ptr(&r[0]))
		}
	default:
		SendMessage(n.ctl, DTM_SETRANGE, flags, Ptr(&r[0]))
	}
}

// datePicked reports a choice in one of n's controls: a calendar's day
// keeps the value's time of day, a time picker's time its day.
func (h *Host) datePicked(n *node, from uintptr, st SystemTime) {
	if n.dateInline && n.ctl != 0 && from != 0 {
		cur := time.UnixMilli(int64(n.value)).Local()
		if n.value == 0 {
			cur = time.Now()
		}
		picked := time.UnixMilli(fromSystemTime(st)).Local()
		switch {
		case from == n.ctl && ClassName(n.ctl) == "SysMonthCal32":
			// The calendar: its day, the value's time.
			picked = time.Date(picked.Year(), picked.Month(), picked.Day(), cur.Hour(), cur.Minute(), cur.Second(), 0, time.Local)
		case from == n.ctl2:
			// The time under it: its time, the value's day.
			picked = time.Date(cur.Year(), cur.Month(), cur.Day(), picked.Hour(), picked.Minute(), picked.Second(), 0, time.Local)
		}
		st = systemTime(picked.UnixMilli())
	}
	ms := fromSystemTime(st)
	if float64(ms) == n.value {
		return // a calendar moving to another month keeps its selection
	}
	n.value = float64(ms)
	h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: float64(ms)})
}

const timeUnderGap = 8 // points between the calendar and the time

// measureDate is n's size in points: the picker's ideal size; a
// calendar's least, with the time picker under it.
func (h *Host) measureDate(n *node) (w, ht float32) {
	if n.ctl != 0 && ClassName(n.ctl) == "SysMonthCal32" {
		var r Rect
		SendMessage(n.ctl, MCM_GETMINREQRECT, 0, Ptr(&r))
		w, ht = h.pt(n, r.W()), h.pt(n, r.H())
		if n.ctl2 != 0 {
			ht += timeUnderGap + 32
		}
		return w, ht
	}
	var sz struct{ Cx, Cy int32 }
	SendMessage(n.ctl, DTM_GETIDEALSIZE, 0, Ptr(&sz))
	return max(h.pt(n, sz.Cx)+8, 100), max(h.pt(n, sz.Cy), 32)
}

// layoutDate places a picker's controls: the picker filling its view, or
// the calendar on top and the time under it, at its leading edge.
func (h *Host) layoutDate(n *node, r Rect) {
	if n.ctl2 == 0 {
		SetWindowPos(n.ctl, 0, 0, 0, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		return
	}
	th := h.px(n, 32)
	ch := max(r.H()-th-h.px(n, timeUnderGap), 1)
	SetWindowPos(n.ctl, 0, 0, 0, r.W(), ch, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	tw := min(r.W(), h.px(n, 160))
	x := int32(0)
	if h.isRTL(n) {
		x = r.W() - tw
	}
	SetWindowPos(n.ctl2, 0, x, r.H()-th, tw, th, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
}

// themeDate follows dark mode on n's controls. A month calendar has no
// dark visual style: dark, it's drawn classic in the dark palette's
// colors (Fluent's layer and text, the title and the days' names in the
// accent's); light, in its visual style. The date picker's drop-down
// calendar likewise (DTN_DROPDOWN). A date and time picker's field, which
// has no dark style either, is drawn light and turned dark: its grays
// become the dark field's, the text light on it (darkField).
func (h *Host) themeDate(n *node) {
	if n.ctl == 0 {
		return
	}
	if ClassName(n.ctl) == "SysMonthCal32" {
		h.themeCalendar(n.ctl)
	} else {
		h.themeDateField(n, n.ctl)
	}
	if n.ctl2 != 0 {
		h.themeDateField(n, n.ctl2)
	}
}

// themeCalendar colors a month calendar for the app's dark mode.
func (h *Host) themeCalendar(cal uintptr) {
	AllowDark(cal, h.dark)
	if !h.dark {
		SetWindowTheme(cal, "")
		return
	}
	DisableTheme(cal)
	p := &h.theme
	for part, c := range map[uintptr]rgba{
		MCSC_BACKGROUND: p.layer.over(p.bg), MCSC_MONTHBK: p.layer.over(p.bg), MCSC_TEXT: p.label,
		MCSC_TITLEBK: p.accent, MCSC_TITLETEXT: p.onAccent, MCSC_TRAILINGTEXT: p.tertiary.over(p.bg),
	} {
		SendMessage(cal, MCM_SETCOLOR, part, uintptr(c.ref()))
	}
	Invalidate(cal)
}

// themeDateField draws a date and time picker's field dark, while the app
// is, its text antialiased in grays (ClearType's colored edges don't turn
// dark).
func (h *Host) themeDateField(n *node, dtp uintptr) {
	AllowDark(dtp, h.dark)
	s := h.nodeSpec(n)
	s.gray = h.dark
	SendMessage(dtp, WM_SETFONT, h.font(s), 0)
	Subclass(dtp, func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
		if m == WM_PAINT && h.dark {
			h.darkField(hwnd)
			return 0
		}
		return DefSubclass(hwnd, m, w, l)
	})
	Invalidate(dtp)
}

// darkField paints a date and time picker as it draws itself, then turns
// it dark: white is the dark field's background, black its text, what's
// between mixed. The selected part's highlight stays.
func (h *Host) darkField(hwnd uintptr) {
	var ps PaintStruct
	dc := BeginPaint(hwnd, &ps)
	defer EndPaint(hwnd, &ps)
	r := ClientRect(hwnd)
	if r.W() <= 0 || r.H() <= 0 {
		return
	}
	mem := CreateCompatibleDC(dc)
	bmp, px := DIB(r.W(), r.H())
	if bmp == 0 {
		DeleteDC(mem)
		return
	}
	old := SelectObject(mem, bmp)
	DefSubclass(hwnd, WM_PRINTCLIENT, mem, 0x4|0x8) // PRF_CLIENT | PRF_ERASEBKGND
	bg, fg := h.theme.fieldBg.over(h.theme.bg), h.theme.label
	hl := SysColor(13) // COLOR_HIGHLIGHT
	for i := 0; i+3 < len(px); i += 4 {
		b, g, rd := int(px[i]), int(px[i+1]), int(px[i+2])
		if absInt(b-int(hl>>16&0xFF))+absInt(g-int(hl>>8&0xFF))+absInt(rd-int(hl&0xFF)) < 60 {
			continue // the selected part's highlight
		}
		t := 1 - float64(b+g+rd)/(3*255) // how much text
		mix := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
		px[i], px[i+1], px[i+2] = mix(bg.b, fg.b), mix(bg.g, fg.g), mix(bg.r, fg.r)
	}
	BitBlt(dc, 0, 0, r.W(), r.H(), mem, 0, 0)
	SelectObject(mem, old)
	DeleteObject(bmp)
	DeleteDC(mem)
}

// dateDropDown themes the calendar a date picker dropped down.
func (h *Host) dateDropDown(dtp uintptr) {
	if cal := SendMessage(dtp, DTM_GETMONTHCAL, 0, 0); cal != 0 {
		h.themeCalendar(cal)
		if h.dark {
			SetDarkTitleBar(Parent(cal), true)
		}
	}
}

func absInt(v int) int { return max(v, -v) }

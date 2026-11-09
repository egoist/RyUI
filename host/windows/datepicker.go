//go:build windows && amd64

package windows

import (
	"time"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
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

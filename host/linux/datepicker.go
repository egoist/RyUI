//go:build linux && !android

package linux

import (
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Date pickers: a button showing the date (and time), opening a popover
// of GTK's calendar and hour and minute spin buttons, as GNOME's apps
// pick dates; Inline shows them in place. The value is the instant, in
// the local time zone.

type datePicker struct {
	mode     ops.DateMode
	inline   bool
	ms       int64
	min, max int64
	button   Ptr
	cal      Ptr
	hour     Ptr
	minute   Ptr
	updating bool
}

func (h *Host) makeDatePicker(n *node) {
	n.date = &datePicker{ms: time.Now().UnixMilli()}
	h.buildDatePicker(n)
}

func (h *Host) buildDatePicker(n *node) {
	d := n.date
	box := BoxNew(OrientationVertical, 12)
	d.cal, d.hour, d.minute = 0, 0, 0
	if d.mode != ops.DateModeTime {
		d.cal = CalendarNew()
		BoxAppend(box, d.cal)
		Connect(d.cal, "day-selected", func() { h.dateChanged(n) })
	}
	if d.mode != ops.DateModeDate {
		row := BoxNew(OrientationHorizontal, 6)
		WidgetSetHAlign(row, AlignCenter)
		top := 23.0
		if h.hourCycle == 12 {
			top = 23 // a 24-hour spin: GTK's spin buttons have no AM/PM
		}
		d.hour = SpinButtonNewWithRange(0, top, 1)
		d.minute = SpinButtonNewWithRange(0, 59, 1)
		SpinButtonSetNumeric(d.hour, true)
		SpinButtonSetNumeric(d.minute, true)
		SetBoolProperty(d.hour, "wrap", true)
		SetBoolProperty(d.minute, "wrap", true)
		colon := LabelNew(":")
		BoxAppend(row, d.hour)
		BoxAppend(row, colon)
		BoxAppend(row, d.minute)
		BoxAppend(box, row)
		Connect(d.hour, "value-changed", func() { h.dateChanged(n) })
		Connect(d.minute, "value-changed", func() { h.dateChanged(n) })
	}
	if d.inline {
		d.button = 0
		h.setInner(n, box)
	} else {
		pop := PopoverNew()
		PopoverSetChild(pop, box)
		b := MenuButtonNew()
		MenuButtonSetPopover(b, pop)
		WidgetSetHAlign(b, AlignStart)
		WidgetSetVAlign(b, AlignCenter)
		d.button = b
		h.setInner(n, b)
	}
	h.showDate(n)
}

func (h *Host) setDateProp(n *node, p ops.PropID, v *ops.Value) {
	d := n.date
	switch p {
	case ops.PropDatePickerValue:
		d.ms = v.Int
		h.showDate(n)
	case ops.PropDatePickerMode:
		if ops.DateMode(v.Int) != d.mode {
			d.mode = ops.DateMode(v.Int)
			h.buildDatePicker(n)
		}
	case ops.PropDatePickerMin:
		d.min = v.Int
	case ops.PropDatePickerMax:
		d.max = v.Int
	case ops.PropDatePickerInline:
		if (v.Int != 0) != d.inline {
			d.inline = v.Int != 0
			h.buildDatePicker(n)
		}
	}
}

// showDate puts the value in the picker's controls and label.
func (h *Host) showDate(n *node) {
	d := n.date
	t := time.UnixMilli(d.ms)
	d.updating = true
	if d.cal != 0 {
		dt := DateTimeNewLocal(int32(t.Year()), int32(t.Month()), int32(t.Day()), 12, 0, 0)
		CalendarSelectDay(d.cal, dt)
		DateTimeUnref(dt)
	}
	if d.hour != 0 {
		SpinButtonSetValue(d.hour, float64(t.Hour()))
		SpinButtonSetValue(d.minute, float64(t.Minute()))
	}
	d.updating = false
	if d.button != 0 {
		MenuButtonSetLabel(d.button, h.formatDate(n, t))
	}
}

// formatDate is the instant as the user's locale writes it (strftime's
// %x and %X, through GLib).
func (h *Host) formatDate(n *node, t time.Time) string {
	dt := DateTimeNewFromUnixLocal(t.Unix())
	defer DateTimeUnref(dt)
	f := "%x"
	timeF := "%H:%M"
	if h.hourCycle == 12 {
		timeF = "%l:%M %p"
	}
	switch n.date.mode {
	case ops.DateModeTime:
		f = timeF
	case ops.DateModeDateTime:
		f = "%x " + timeF
	}
	return GoStringFree(DateTimeFormat(dt, f))
}

// dateChanged reads the controls into the value (within Min and Max)
// and reports it.
func (h *Host) dateChanged(n *node) {
	d := n.date
	if d.updating {
		return
	}
	t := time.UnixMilli(d.ms)
	y, mo, day := t.Date()
	hr, mi := t.Hour(), t.Minute()
	if d.cal != 0 {
		dt := CalendarGetDate(d.cal)
		y, mo, day = int(DateTimeGetYear(dt)), time.Month(DateTimeGetMonth(dt)), int(DateTimeGetDayOfMonth(dt))
		DateTimeUnref(dt)
	}
	if d.hour != 0 {
		hr, mi = int(SpinButtonGetValue(d.hour)), int(SpinButtonGetValue(d.minute))
	}
	ms := time.Date(y, mo, day, hr, mi, t.Second(), 0, time.Local).UnixMilli()
	if d.min != 0 && ms < d.min {
		ms = d.min
	}
	if d.max != 0 && ms > d.max {
		ms = d.max
	}
	if ms == d.ms {
		return
	}
	d.ms = ms
	h.showDate(n)
	h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: float64(ms)})
}

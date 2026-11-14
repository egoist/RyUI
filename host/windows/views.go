//go:build windows

package windows

import (
	"math"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// makeView makes a window of the host's own class, which draws itself.
// It waits in the parking window until it's inserted.
func (h *Host) makeView(style uint32) uintptr {
	return CreateWindow(WS_EX_CONTROLPARENT|WS_EX_NOPARENTNOTIFY, classView, "",
		WS_CHILD|WS_VISIBLE|WS_CLIPCHILDREN|WS_CLIPSIBLINGS|style, 0, 0, 0, 0, h.park, 0)
}

// makeControl makes a common control inside a view of the host's: the
// view is what the core places, and the control never changes parent
// (some controls send their notifications to the parent they were made
// in).
func (h *Host) makeControl(n *node, class string, style, exStyle uint32) {
	n.hwnd = h.makeView(0)
	h.makeControlIn(n, class, style, exStyle)
}

// makeControlIn makes the control inside n's view.
func (h *Host) makeControlIn(n *node, class string, style, exStyle uint32) {
	n.ctl = CreateWindow(exStyle, class, "", WS_CHILD|WS_VISIBLE|WS_CLIPSIBLINGS|style, 0, 0, 0, 0, n.hwnd, 1)
	if n.ctl == 0 {
		fail("couldn't make a %s (%s)", n.kind, class)
	}
	SendMessage(n.ctl, WM_SETFONT, h.nodeFont(n), 0)
	// Focus rectangles only after the keyboard is used, as in dialogs.
	SendMessage(n.ctl, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
}

func newNode(id ops.NodeID, kind ops.Kind) *node {
	return &node{id: id, kind: kind, opacity: 1, selected: 0}
}

// create makes the native view of a kind.
func (h *Host) create(id ops.NodeID, kind ops.Kind) {
	n := newNode(id, kind)
	if h.createNav(n) {
		h.add(n)
		return
	}
	switch kind {
	case ops.KindView, ops.KindSelectable, ops.KindText, ops.KindSymbol, ops.KindShape, ops.KindCanvas, ops.KindShader,
		ops.KindProgress, ops.KindProgressRing, ops.KindActivity, ops.KindImage:
		n.hwnd = h.makeView(0)
		switch kind {
		case ops.KindShape:
			n.shape = &shape{lineWidth: 1, trimEnd: 1}
		case ops.KindProgress:
			n.max = 1
		case ops.KindImage:
			n.image = &image{phMode: -1, fade: 1}
		}
	case ops.KindSegmented, ops.KindPageControl:
		n.hwnd = h.makeView(WS_TABSTOP)
	case ops.KindSplit:
		h.makeSplit(n)
	case ops.KindAccessory:
		// A field's bar over the keyboard: never shown, with no on-screen
		// keyboard to put it on (it waits in the parking window).
		n.hwnd = h.makeView(0)
	case ops.KindButton:
		h.makeControl(n, "BUTTON", BS_PUSHBUTTON|WS_TABSTOP, 0)
	case ops.KindToggle:
		h.makeControl(n, "BUTTON", BS_AUTOCHECKBOX|WS_TABSTOP, 0)
	case ops.KindColorPicker:
		h.makeControl(n, "BUTTON", BS_PUSHBUTTON|WS_TABSTOP, 0)
	case ops.KindSlider:
		h.makeControl(n, "msctls_trackbar32", TBS_HORZ|TBS_NOTICKS|0x40|WS_TABSTOP, 0) // TBS_FIXEDLENGTH
		SendMessage(n.ctl, TBM_SETRANGEMIN, 0, 0)
		SendMessage(n.ctl, TBM_SETRANGEMAX, 0, sliderSteps)
		SendMessage(n.ctl, TBM_SETLINESIZE, 0, sliderSteps/100)
		SendMessage(n.ctl, TBM_SETPAGESIZE, 0, sliderSteps/10)
		n.max = 1
		Subclass(n.ctl, func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
			return h.sliderMessage(hwnd, m, w, l)
		})
	case ops.KindStepper:
		n.hwnd = h.makeView(0)
		n.min, n.max, n.step = 0, 100, 1
		for i := range 2 {
			b := CreateWindow(0, "BUTTON", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, n.hwnd, uintptr(i+1))
			SendMessage(b, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
			if i == 0 {
				n.ctl = b
			} else {
				n.ctl2 = b
				h.byHwnd[b] = n
			}
		}
		h.nameStepper(n)
	case ops.KindWheel, ops.KindPicker:
		h.makeControl(n, "COMBOBOX", comboStyle, h.wheelExStyle(n))
		if kind == ops.KindPicker {
			n.selected = -1 // none, till the core says
		}
		h.setupCombo(n) // drawn as WinUI's ComboBox (combo.go)
	case ops.KindDatePicker:
		n.hwnd = h.makeView(0)
		h.makeDateControls(n)
	case ops.KindTextField:
		h.makeField(n)
	case ops.KindScroll:
		n.hwnd = h.makeView(0)
	case ops.KindList:
		n.list = newList(h, n)
	case ops.KindRichText:
		h.makeRich(n)
	case ops.KindWebView, ops.KindVideo:
		h.makeWeb(n)
	case ops.KindNative:
		n.hwnd = h.makeView(0) // the app's window goes in it (native.go)
	default:
		fail("can't show %s yet", kind)
	}
	if n.hwnd == 0 {
		fail("couldn't make a %s", kind)
	}
	h.add(n)
	h.themeControl(n)
}

// themeControl gives a control the system's dark or light visual style
// where it has one (scroll bars, a combo box's list's).
func (h *Host) themeControl(n *node) {
	theme := "Explorer"
	if h.dark {
		theme = "DarkMode_Explorer"
	}
	switch n.kind {
	case ops.KindScroll, ops.KindList:
		if n.hwnd != 0 {
			AllowDark(n.hwnd, h.dark)
			SetWindowTheme(n.hwnd, theme)
		}
	case ops.KindWheel, ops.KindPicker:
		if n.ctl != 0 {
			AllowDark(n.ctl, h.dark)
			if h.dark {
				SetWindowTheme(n.ctl, "DarkMode_CFD")
			} else {
				SetWindowTheme(n.ctl, "")
			}
			h.styleComboList(n) // combo.go
			Invalidate(n.ctl)
		}
	case ops.KindTextField:
		if n.ctl != 0 && n.multiline {
			AllowDark(n.ctl, h.dark)
			SetWindowTheme(n.ctl, theme)
		}
	case ops.KindRichText:
		if n.ctl != 0 {
			AllowDark(n.ctl, h.dark)
			SetWindowTheme(n.ctl, theme)
			n.rich.invalidate(h, n)
		}
	case ops.KindDatePicker:
		h.themeDate(n) // datepicker.go
	}
}

func (n *node) stepButtons() []uintptr { return []uintptr{n.ctl, n.ctl2} }

// wheelExStyle is a combo box's direction: right to left, its text on the
// right and its arrow on the left.
func (h *Host) wheelExStyle(n *node) uint32 {
	if h.isRTL(n) {
		return WS_EX_RIGHT | WS_EX_RTLREADING | WS_EX_LEFTSCROLLBAR
	}
	return 0
}

// selectPicker selects a Picker's chosen item in its combo box; with none
// (out of range) the box is empty, and shows its label.
func (h *Host) selectPicker(n *node) {
	i := n.selected
	if i < 0 || i >= len(n.items) {
		i = -1
	}
	SendMessage(n.ctl, CB_SETCURSEL, uintptr(i), 0)
	Invalidate(n.ctl)
}

// labelPicker shows a Picker's label in its combo box while none is chosen
// (the box draws it, in the secondary text color, where the combo box's
// cue banner would be), and names it for screen readers (its own text is
// the chosen item).
func (h *Host) labelPicker(n *node) {
	SendMessage(n.ctl, CB_SETCUEBANNER, 0, Str(n.pickerLabel))
	SetAccessibleName(n.ctl, n.pickerLabel)
	Invalidate(n.ctl)
}

// remakeWheel makes a combo box again in its direction: a combo box takes
// its extended styles as it's made.
func (h *Host) remakeWheel(n *node) {
	old := n.ctl
	if old == 0 {
		return
	}
	h.makeControlIn(n, "COMBOBOX", comboStyle, h.wheelExStyle(n))
	h.byHwnd[n.ctl] = n
	h.forgetTree(old)
	DestroyWindow(old)
	h.byHwnd[n.ctl] = n
	h.setupCombo(n)
	for _, s := range n.items {
		SendMessage(n.ctl, CB_ADDSTRING, 0, Str(s))
	}
	if n.kind == ops.KindPicker {
		h.selectPicker(n)
		h.labelPicker(n)
	} else {
		SendMessage(n.ctl, CB_SETCURSEL, uintptr(n.selected), 0)
	}
	if n.disabled {
		EnableWindow(n.ctl, false)
	}
	h.themeControl(n)
	h.layoutField(n)
}

const sliderSteps = 10000

func (h *Host) setOwn(n *node, p ops.PropID, v *ops.Value) {
	switch n.kind {
	case ops.KindView:
		switch p {
		case ops.PropViewDropAccept:
			n.dropAccept = ops.DecodeStrings(v.Bytes)
		case ops.PropViewDragItems:
			n.dragItems = ops.DecodeDragItems(v.Bytes)
		}
	case ops.KindText:
		if p == ops.PropTextText {
			n.text = v.Str
			n.utext = UTF16Units(v.Str)
			h.redraw(n)
		}
	case ops.KindButton:
		switch p {
		case ops.PropButtonTitle:
			n.title = v.Str
			SetWindowText(n.ctl, v.Str)
		case ops.PropButtonVariant:
			n.variant = int32(v.Int)
		case ops.PropButtonIcon:
			n.icon = v.Str
		}
		h.redrawControl(n)
	case ops.KindTextField:
		h.setField(n, p, v)
	case ops.KindToggle:
		switch p {
		case ops.PropToggleOn:
			n.on = v.Int != 0
			SendMessage(n.ctl, BM_SETCHECK, uintptr(v.Int), 0)
		case ops.PropToggleCheckbox:
			n.checkbox = v.Int != 0
		}
		h.redrawControl(n)
	case ops.KindSlider:
		switch p {
		case ops.PropSliderValue:
			n.value = v.Float
		case ops.PropSliderMin:
			n.min = v.Float
		case ops.PropSliderMax:
			n.max = v.Float
		}
		h.applySlider(n)
	case ops.KindStepper:
		switch p {
		case ops.PropStepperValue:
			n.value = v.Float
		case ops.PropStepperMin:
			n.min = v.Float
		case ops.PropStepperMax:
			n.max = v.Float
		case ops.PropStepperStep:
			n.step = v.Float
		}
	case ops.KindScroll:
		if p == ops.PropScrollHorizontal {
			n.horizontal = v.Int != 0
			h.updateScroll(n)
		}
		if p == ops.PropScrollPaging {
			n.paging = v.Int != 0
			h.hideBars(n)
		}
		switch p {
		case ops.PropScrollRefreshable:
			h.setRefreshable(n, v.Int != 0)
		case ops.PropScrollRefreshing:
			h.setRefreshing(n, v.Int != 0)
		}
	case ops.KindSplit:
		h.setSplitProp(n, p, v)
	case ops.KindShader:
		h.setShaderProp(n, p, v)
	case ops.KindCanvas:
		if p == ops.PropCanvasCommands {
			n.canvas = append(n.canvas[:0], v.Bytes...)
			h.redraw(n)
		}
	case ops.KindPageControl:
		switch p {
		case ops.PropPageControlCount:
			n.pages = max(int(v.Int), 0)
		case ops.PropPageControlPage:
			n.selected = int(v.Int)
		}
		h.redraw(n)
		// Pull to refresh: not on Windows.
	case ops.KindList:
		n.list.setProp(h, n, p, v)
	case ops.KindActivity:
		if p == ops.PropActivityAnimating {
			n.animating = v.Int != 0
			h.spin(n)
			h.redraw(n)
		}
	case ops.KindProgress:
		switch p {
		case ops.PropProgressValue:
			n.value = v.Float
		case ops.PropProgressIndeterminate:
			n.indeterminate = v.Int != 0
			h.spin(n)
		}
		h.redraw(n)
	case ops.KindProgressRing:
		switch p {
		case ops.PropProgressRingValue:
			old := n.value
			n.value = min(max(v.Float, 0), 1)
			// The arc moves to it, as WinUI's ring eases between values;
			// at once when it isn't shown, or motion is reduced.
			if h.env.ReduceMotion || n.hwnd == 0 || !IsWindowVisible(n.hwnd) {
				n.shown = n.value
			}
			RaiseRangeValueChanged(n.uia, old*100, n.value*100)
		case ops.PropProgressRingIndeterminate:
			n.indeterminate = v.Int != 0
		}
		h.spin(n)
		h.redraw(n)
	case ops.KindImage:
		h.setImageProp(n, p, v)
	case ops.KindSymbol:
		if p == ops.PropSymbolName {
			n.symbol = v.Str
			if symbolGlyph(v.Str) == 0 && v.Str != "" {
				warnSymbol(v.Str)
			}
			h.redraw(n)
		}
	case ops.KindSegmented:
		switch p {
		case ops.PropSegmentedSegments:
			n.items = decodeStrings(v.Bytes)
		case ops.PropSegmentedSelected:
			n.selected = int(v.Int)
		}
		h.redraw(n)
	case ops.KindWheel:
		switch p {
		case ops.PropWheelItems:
			n.items = decodeStrings(v.Bytes)
			SendMessage(n.ctl, CB_RESETCONTENT, 0, 0)
			for _, s := range n.items {
				SendMessage(n.ctl, CB_ADDSTRING, 0, Str(s))
			}
			SendMessage(n.ctl, CB_SETCURSEL, uintptr(n.selected), 0)
		case ops.PropWheelSelected:
			n.selected = int(v.Int)
			SendMessage(n.ctl, CB_SETCURSEL, uintptr(v.Int), 0)
		}
		Invalidate(n.ctl)
	case ops.KindPicker:
		switch p {
		case ops.PropPickerItems:
			n.items = decodeStrings(v.Bytes)
			SendMessage(n.ctl, CB_RESETCONTENT, 0, 0)
			for _, s := range n.items {
				SendMessage(n.ctl, CB_ADDSTRING, 0, Str(s))
			}
			h.selectPicker(n)
		case ops.PropPickerSelected:
			n.selected = int(v.Int)
			h.selectPicker(n)
		case ops.PropPickerLabel:
			n.pickerLabel = v.Str
			h.labelPicker(n)
		}
	case ops.KindDatePicker:
		switch p {
		case ops.PropDatePickerValue:
			n.value = float64(v.Int)
			h.applyDate(n)
		case ops.PropDatePickerMode:
			n.dateMode = int32(v.Int)
			h.makeDateControls(n)
		case ops.PropDatePickerInline:
			n.dateInline = v.Int != 0
			h.makeDateControls(n)
		case ops.PropDatePickerMin, ops.PropDatePickerMax:
			if p == ops.PropDatePickerMin {
				n.min = float64(v.Int)
			} else {
				n.max = float64(v.Int)
			}
			h.applyDateRange(n)
		}
	case ops.KindColorPicker:
		switch p {
		case ops.PropColorPickerValue:
			n.colorValue = ops.Color(v.Int)
		case ops.PropColorPickerTitle:
			n.title = v.Str
		}
		h.redrawControl(n)
	case ops.KindRichText:
		n.rich.setProp(h, n, p, v)
	case ops.KindShape:
		n.shape.setProp(h, n, p, v)
	case ops.KindWebView, ops.KindVideo:
		n.web.setProp(h, n, p, v)
	case ops.KindNative:
		h.setNativeProp(n, p, v)
	default:
		h.setNavProp(n, p, v)
	}
}

func (h *Host) redrawControl(n *node) {
	if n.ctl != 0 {
		Invalidate(n.ctl)
	}
	if n.ctl2 != 0 {
		Invalidate(n.ctl2)
	}
}

// applySlider moves the trackbar to the value, in sliderSteps steps
// between Min and Max.
func (h *Host) applySlider(n *node) {
	pos := 0.0
	if n.max > n.min {
		pos = (n.value - n.min) / (n.max - n.min) * sliderSteps
	}
	pos = min(max(pos, 0), sliderSteps)
	if h.isRTL(n) {
		pos = sliderSteps - pos
	}
	SendMessage(n.ctl, TBM_SETPOS, 1, uintptr(int(math.Round(pos))))
	Invalidate(n.ctl)
}

func (h *Host) sliderMoved(n *node) {
	pos := float64(SendMessage(n.ctl, TBM_GETPOS, 0, 0))
	if h.isRTL(n) {
		pos = sliderSteps - pos
	}
	v := n.min + pos/sliderSteps*(n.max-n.min)
	Invalidate(n.ctl)
	if v == n.value {
		return
	}
	n.value = v
	h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: v})
}

// sliderMessage is a trackbar's subclass. Right to left the trackbar stays
// as it is, its position the value's mirror (a mirrored control would have
// its custom draw mirrored, which GDI+ doesn't follow), so its keys that mean
// the value's ends swap; the arrows move the thumb the way they point.
func (h *Host) sliderMessage(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if m == WM_KEYDOWN && (w == VK_HOME || w == VK_END) {
		if n := h.byHwnd[hwnd]; n != nil && h.isRTL(n) {
			w = VK_HOME + VK_END - w
		}
	}
	return DefSubclass(hwnd, m, w, l)
}

// dateFormat sets a date picker's format: the locale's short date, its
// time without seconds, or both.
const localeSShortDate, localeSShortTime = 0x1F, 0x79

func (h *Host) dateFormat(n *node) {
	date := LocaleInfo("", localeSShortDate)
	tm := LocaleInfo("", localeSShortTime)
	var f string
	switch ops.DateMode(n.dateMode) {
	case ops.DateModeTime:
		f = tm
	case ops.DateModeDateTime:
		f = date + " " + tm
	default:
		f = date
	}
	SendMessage(n.ctl, DTM_SETFORMATW, 0, Str(f))
}

func systemTime(ms int64) SystemTime {
	t := time.UnixMilli(ms).Local()
	return SystemTime{Year: uint16(t.Year()), Month: uint16(t.Month()), DayOfWeek: uint16(t.Weekday()), Day: uint16(t.Day()),
		Hour: uint16(t.Hour()), Minute: uint16(t.Minute()), Second: uint16(t.Second()), Milliseconds: uint16(t.Nanosecond() / 1e6)}
}

func fromSystemTime(st SystemTime) int64 {
	t := time.Date(int(st.Year), time.Month(st.Month), int(st.Day), int(st.Hour), int(st.Minute), int(st.Second), int(st.Milliseconds)*1e6, time.Local)
	return t.UnixMilli()
}

// controlCommand is a control's WM_COMMAND notification.
func (h *Host) controlCommand(n *node, ctl uintptr, code uint32) {
	switch n.kind {
	case ops.KindButton:
		if code == BN_CLICKED {
			if len(n.menu) > 0 && n.menuTrigger == int32(ops.MenuTriggerTap) {
				h.popUp(n, n.hwnd, true)
				return
			}
			h.emit(n, ops.Event{Kind: ops.EventPress})
		}
	case ops.KindToggle:
		if code == BN_CLICKED {
			n.on = SendMessage(ctl, BM_GETCHECK, 0, 0) == 1
			Invalidate(ctl)
			h.emit(n, ops.Event{Kind: ops.EventToggled, Bool: n.on})
		}
	case ops.KindStepper:
		if code == BN_CLICKED {
			step := n.step
			if step == 0 {
				step = 1
			}
			if ctl == n.ctl {
				step = -step
			}
			v := min(max(n.value+step, n.min), n.max)
			if v != n.value {
				n.value = v
				h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: v})
			}
		}
	case ops.KindColorPicker:
		if code == BN_CLICKED {
			h.pickColor(n)
		}
	case ops.KindTextField:
		switch code {
		case EN_CHANGE:
			if n.settingText {
				return
			}
			s := WindowText(ctl)
			if n.richText {
				s = visibleText(ctl) // not a link's hidden field instruction
			}
			if n.multiline {
				s = strings.ReplaceAll(s, "\r\n", "\n")
			}
			if n.richText {
				s = strings.ReplaceAll(s, "\r", "\n") // a rich edit's lines end in \r
			}
			n.text = s
			h.emit(n, ops.Event{Kind: ops.EventTextChanged, Str: s})
			h.richEdited(n)

		case EN_SETFOCUS:
			n.focused = true
			h.redraw(n)
			h.emit(n, ops.Event{Kind: ops.EventFocus})
		case EN_KILLFOCUS:
			n.focused = false
			h.redraw(n)
			h.emit(n, ops.Event{Kind: ops.EventBlur})
		}
	case ops.KindWheel, ops.KindPicker:
		h.comboCommand(n, ctl, code) // combo.go
	case kindHeader:
		h.headerCommand(n, ctl)
	case kindCommandBar:
		h.commandBarCommand(n, ctl)
	case kindAlertWindow:
		h.alertDialogCommand(n, ctl, code)
	case ops.KindSnackbar:
		if code == BN_CLICKED {
			h.snackClicked(n, ctl)
		}
	case ops.KindRichText:
		// EN_CHANGE and the like: a read-only rich edit's own.
	}
}

// notify is a control's WM_NOTIFY notification.
func (h *Host) notify(n *node, hdr *NMHdr, lp uintptr) (uintptr, bool) {
	switch hdr.Code {
	case enSelChange:
		if n.richText {
			h.reportRichFormats(n)
			h.checkSelection(n)
			return 0, true
		}
	case NM_CUSTOMDRAW:
		cd := Native[NMCustomDraw](lp)
		switch n.kind {
		case ops.KindButton, ops.KindColorPicker, ops.KindStepper, kindHeader, kindCommandBar, kindAlertWindow, ops.KindSnackbar:
			return h.drawButton(n, cd), true
		case ops.KindToggle:
			return h.drawToggle(n, cd), true
		case ops.KindSlider:
			return h.drawSlider(n, cd), true
		}
	case DTN_DATETIMECHANGE:
		if n.kind == ops.KindDatePicker {
			ch := Native[NMDateTimeChange](lp)
			if ch.Flags == GDT_VALID {
				h.datePicked(n, hdr.HwndFrom, ch.St)
			}
			return 0, true
		}
	case DTN_DROPDOWN:
		if n.kind == ops.KindDatePicker {
			h.dateDropDown(hdr.HwndFrom) // datepicker.go
			return 0, true
		}
	case MCN_SELCHANGE:
		if n.kind == ops.KindDatePicker {
			h.datePicked(n, hdr.HwndFrom, Native[NMSelChange](lp).Start)
			return 0, true
		}
	case enRequestResize:
		if n.rich != nil {
			n.rich.requested = Native[struct {
				Hdr NMHdr
				Rc  Rect
			}](lp).Rc
		}
		return 0, true
	}
	return 0, false
}

// controlColors answers a control's WM_CTLCOLOR*: the colors of its text
// and background, which follow the theme.
func (h *Host) controlColors(n *node, dc uintptr, m uint32) uintptr {
	switch n.kind {
	case ops.KindTextField:
		bg := h.theme.fieldBg
		if n.focused {
			bg = h.theme.fieldFocusBg
		}
		if n.plain {
			bg = h.backdrop(n) // the text alone, on what's behind it
		}
		SetTextColor(dc, h.textColor(n, h.theme.label).over(bg).ref())
		SetBkColor(dc, bg.ref())
		return h.brush(bg)
	case kindAlertWindow:
		return h.alertDialogColors(dc) // its title and message
	case ops.KindWheel, ops.KindPicker:
		// Its list's, should it ask here (combo.go answers it first).
		bg := h.comboListBg()
		SetTextColor(dc, h.comboListText().ref())
		SetBkColor(dc, bg.ref())
		return h.brush(bg)
	}
	bg := h.backdrop(n)
	SetBkColor(dc, bg.ref())
	SetTextColor(dc, h.theme.label.ref())
	return h.brush(bg)
}

// listen adds what an event needs on the native side.
func (h *Host) listen(n *node, e ops.EventKind) {
	if n.hwnd == 0 {
		return
	}
	if e == ops.EventWindowState {
		h.listenWindow(n, e) // winctl.go
		return
	}
	switch e {
	case ops.EventPress:
		if n.kind == ops.KindView {
			// A pressable view is a tab stop that Enter and Space press.
			SetWindowLong(n.hwnd, GWL_STYLE, WindowLong(n.hwnd, GWL_STYLE)|WS_TABSTOP)
		}
	case ops.EventPinch, ops.EventRotate:
		// A touch screen's zoom and rotate (rotate is off by default).
		AllowGestures(n.hwnd, true, n.listening(ops.EventRotate))
	}
}

// viewMessage handles the messages of a view of the host's class.
func (h *Host) viewMessage(n *node, hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	if n.host != nil {
		if r, ok := n.host(h, n, m, wp, lp); ok {
			return r, true
		}
	}
	switch m {
	case WM_GETOBJECT:
		if r, ok := h.getObject(n, hwnd, wp, lp); ok {
			return r, true
		}
	case wmNCHitTest:
		if !n.interactive() || n.kind == kindHeader && h.underCaption(hwnd) {
			// Through to what's under it: under a hidden title bar, the
			// window's caption (a header's empty room too; its buttons are
			// windows of their own).
			return htTransparent, true
		}
	case WM_ERASEBKGND:
		return 1, true
	case WM_PAINT:
		h.paintWindow(n, hwnd)
		return 0, true
	case WM_PRINTCLIENT:
		h.paint(n, hwnd, wp, ClientRect(hwnd))
		return 0, true
	case WM_SIZE:
		if n.clip {
			h.shapeClip(n)
		}
		if n.kind == ops.KindStepper {
			h.layoutStepper(n)
		} else if n.ctl != 0 {
			h.layoutField(n)
		}
		return 0, false
	case WM_GESTURE:
		if h.touchGesture(n, hwnd, lp) {
			return 0, true
		}
	case WM_MOUSEWHEEL, WM_MOUSEHWHEEL:
		if m == WM_MOUSEWHEEL && wp&MK_CONTROL != 0 && h.wheelPinch(n, hwnd, int32(int16(HiWord(wp))), lp) {
			return 0, true
		}
		if n.kind == ops.KindScroll && n.paging {
			if h.pageWheel(n, m == WM_MOUSEHWHEEL, int32(int16(HiWord(wp)))) {
				return 0, true
			}
			return 0, false
		}
		if n.kind == ops.KindScroll || n.list != nil {
			if h.wheel(n, m == WM_MOUSEHWHEEL, int32(int16(HiWord(wp)))) {
				return 0, true
			}
		}
		return 0, false
	case WM_VSCROLL, WM_HSCROLL:
		if lp == 0 && (n.kind == ops.KindScroll || n.list != nil) {
			h.scrollBar(n, m == WM_HSCROLL, int(LoWord(wp)))
			return 0, true
		}
	case WM_SETCURSOR:
		return 0, false
	case WM_LBUTTONDOWN, WM_LBUTTONDBLCLK:
		if h.selectClick(n) {
			return 0, true // a row of a selecting list: selected or deselected
		}
		h.mouseDown(n, hwnd, lp)
		return 0, true
	case WM_MOUSEMOVE:
		h.mouseMove(n, hwnd, wp, lp)
		return 0, true
	case WM_LBUTTONUP:
		h.mouseUp(n, hwnd, lp)
		return 0, true
	case WM_MOUSELEAVE:
		if n.hot {
			n.hot = false
			h.redraw(n)
		}
		return 0, true
	case WM_CAPTURECHANGED:
		if n.tracking && lp != hwnd && !n.releasing {
			h.cancelPress(n)
		}
		return 0, true
	case WM_RBUTTONUP:
		if n.pressable() {
			h.secondaryClick(n, hwnd)
			return 0, true
		}
	case WM_CONTEXTMENU:
		if len(n.menu) == 0 && h.actionsMenu(n, hwnd, lp) {
			return 0, true // its screen reader actions
		}
		if n.pressable() {
			h.secondaryClick(n, hwnd)
			return 0, true
		}
		if n.cell != nil && h.rowMenu(n.cell) {
			return 0, true // DefWindowProc brought it up from the row's content
		}
	case WM_TIMER:
		switch wp {
		case timerLongPress:
			KillTimer(hwnd, timerLongPress)
			if n.tracking && n.pressed {
				n.longFired = true
				h.cancelPress(n)
				h.longPress(n, hwnd)
			}
		case timerBars:
			h.hideBars(n)
		case timerSpin:
			n.phase += 1.0 / 60 / 1.5
			if n.kind == ops.KindProgressRing && n.shown != n.value {
				// Eases to the value: a quarter of the way each frame
				// (about a fifth of a second), then there.
				n.shown += (n.value - n.shown) / 4
				if math.Abs(n.value-n.shown) < 0.002 {
					n.shown = n.value
				}
				if !n.indeterminate {
					h.spin(n) // stops once it's there
				}
			}
			Invalidate(hwnd)
		case timerShader:
			h.renderShader(n)
		case timerPinch:
			h.endWheelPinch(n, hwnd)
		}
		return 0, true
	case WM_SETFOCUS:
		n.focused = true
		Invalidate(hwnd)
		return 0, false
	case WM_KILLFOCUS:
		n.focused = false
		if n.spaceDown {
			// Focus going while Space is held: no press.
			n.spaceDown = false
			h.setPressed(n, false)
		}
		Invalidate(hwnd)
		return 0, false
	case WM_GETDLGCODE:
		code := uintptr(0)
		if n.kind == ops.KindSegmented || n.adjustable {
			code |= DLGC_WANTARROWS
		}
		if lp != 0 {
			msg := Native[Msg](lp)
			if msg.Message == WM_KEYDOWN && (msg.WParam == VK_RETURN || msg.WParam == VK_SPACE) && (n.pressable() || n.kind == ops.KindSegmented) {
				code |= DLGC_WANTALLKEYS
			}
			if msg.Message == WM_CHAR && msg.WParam == ' ' && n.pressable() {
				code |= DLGC_WANTCHARS // Space's character: the press's, not a mnemonic
			}
		}
		return code, true
	case WM_KEYDOWN:
		if h.key(n, hwnd, wp) {
			return 0, true
		}
	case WM_KEYUP:
		if wp == VK_SPACE && n.spaceDown {
			// Space let go of: the press fires.
			n.spaceDown = false
			h.endPress(n, hwnd, n.pressable())
			return 0, true
		}
	case WM_CHAR:
		if wp == ' ' && n.pressable() {
			return 0, true
		}
	case WM_UPDATEUISTATE:
		Invalidate(hwnd)
		return 0, false
	}
	return 0, false
}

const (
	wmNCHitTest   = 0x0084
	htTransparent = ^uintptr(0) // HTTRANSPARENT: -1
)

func (h *Host) key(n *node, hwnd uintptr, vk uintptr) bool {
	if h.adjustKey(n, vk) {
		return true
	}
	switch {
	case n.kind == ops.KindSegmented:
		if h.isRTL(n) && (vk == VK_LEFT || vk == VK_RIGHT) {
			vk = VK_LEFT + VK_RIGHT - vk // the arrows go the way they point
		}
		switch vk {
		case VK_LEFT, VK_UP:
			h.selectSegment(n, n.selected-1)
			return true
		case VK_RIGHT, VK_DOWN:
			h.selectSegment(n, n.selected+1)
			return true
		}
	case n.pressable():
		switch vk {
		case VK_RETURN:
			// Enter presses at once.
			h.press(n, hwnd)
			return true
		case VK_SPACE:
			// Space presses while it's held (its repeats too), and fires as
			// it's let go of (WM_KEYUP).
			if !n.spaceDown {
				n.spaceDown = true
				h.setPressed(n, true)
			}
			return true
		}
	case n.list != nil || n.kind == ops.KindScroll:
		page := float32(ClientRect(hwnd).H()) / h.scaleOf(n)
		switch vk {
		case VK_UP:
			h.scrollBy(n, 0, -40)
		case VK_DOWN:
			h.scrollBy(n, 0, 40)
		case VK_PRIOR:
			h.scrollBy(n, 0, -page)
		case VK_NEXT:
			h.scrollBy(n, 0, page)
		case VK_HOME:
			h.scrollBy(n, 0, -1e9)
		case VK_END:
			h.scrollBy(n, 0, 1e9)
		default:
			return false
		}
		return true
	}
	return false
}

// The mouse on a view: a press is a click inside it (pressed while the
// mouse is over it, again as it comes back on); held, a long press;
// dragged, a drag and drop (a view with DragItems) or else a pan, either
// of which cancels the press. A secondary click is a long press too, or
// shows the view's context menu.

func (h *Host) mouseDown(n *node, hwnd, lp uintptr) {
	if n.kind == ops.KindSegmented {
		h.selectSegment(n, h.segmentAt(n, XParam(lp)))
		SetFocus(hwnd)
		return
	}
	if n.kind == ops.KindPageControl {
		h.selectPip(n, h.pipAt(n, XParam(lp)))
		return
	}
	if !n.tracksPress() && !n.draggable() && !n.listening(ops.EventPan) && !n.listening(ops.EventSwipe) {
		return
	}
	if n.disabled {
		return
	}
	SetCapture(hwnd)
	n.tracking = n.tracksPress()
	n.longFired, n.panning = false, false
	n.pressAt = Point{X: XParam(lp), Y: YParam(lp)}
	n.panLast, n.panTime = n.pressAt, time.Now().UnixNano()
	if n.listening(ops.EventLongPress) || len(n.menu) > 0 && n.menuTrigger == int32(ops.MenuTriggerLongPress) {
		SetTimer(hwnd, timerLongPress, 500)
	}
	h.setPressed(n, n.tracking)
}

func (h *Host) mouseMove(n *node, hwnd, wp, lp uintptr) {
	if n.pressable() && !n.hot {
		n.hot = true
		TrackLeave(hwnd)
		if !n.drawsStates() {
			h.redraw(n)
		}
	}
	if Capture() != hwnd || wp&MK_LBUTTON == 0 {
		return
	}
	p := Point{X: XParam(lp), Y: YParam(lp)}
	if n.panning {
		h.pan(n, ops.GestureStateChanged, p)
		return
	}
	dpi := h.dpiOf(n)
	far := abs32(p.X-n.pressAt.X) > int32(SystemMetric(68, dpi)) || abs32(p.Y-n.pressAt.Y) > int32(SystemMetric(69, dpi)) // SM_CXDRAG, SM_CYDRAG
	switch {
	case far && n.draggable():
		h.startDrag(n, hwnd)
		return
	case far && (n.listening(ops.EventPan) || n.listening(ops.EventSwipe)):
		n.panning = true
		h.cancelPress(n)
		h.pan(n, ops.GestureStateBegan, p)
		return
	}
	if n.tracking {
		in := h.inside(hwnd, p)
		if !in {
			KillTimer(hwnd, timerLongPress)
		}
		h.setPressed(n, in)
	}
}

func (h *Host) mouseUp(n *node, hwnd, lp uintptr) {
	pressed, panning := n.tracking && n.pressed, n.panning
	n.tracking, n.panning = false, false
	if Capture() == hwnd {
		n.releasing = true
		ReleaseCapture()
		n.releasing = false
	}
	KillTimer(hwnd, timerLongPress)
	p := Point{X: XParam(lp), Y: YParam(lp)}
	if panning {
		h.pan(n, ops.GestureStateEnded, p)
		return
	}
	h.endPress(n, hwnd, pressed && !n.longFired && n.pressable() && h.inside(hwnd, p))
}

func (h *Host) press(n *node, hwnd uintptr) {
	if len(n.menu) > 0 && n.menuTrigger == int32(ops.MenuTriggerTap) && !n.listening(ops.EventPress) {
		h.popUp(n, hwnd, false)
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventPress})
}

func (h *Host) longPress(n *node, hwnd uintptr) {
	if len(n.menu) > 0 && n.menuTrigger == int32(ops.MenuTriggerLongPress) {
		h.popUp(n, hwnd, false)
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventLongPress})
}

func (h *Host) secondaryClick(n *node, hwnd uintptr) {
	if len(n.menu) > 0 {
		h.popUp(n, hwnd, false)
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventLongPress})
}

func (h *Host) inside(hwnd uintptr, p Point) bool {
	r := ClientRect(hwnd)
	return p.X >= 0 && p.Y >= 0 && p.X < r.Right && p.Y < r.Bottom
}

// pan reports a drag as a pan (points, from where it started; velocity
// from the last move), and its end as a swipe when it was quick.
func (h *Host) pan(n *node, state ops.GestureState, p Point) {
	now := time.Now().UnixNano()
	dt := float32(now-n.panTime) / 1e9
	g := ops.Gesture{State: state, X: h.pt(n, p.X), Y: h.pt(n, p.Y), Scale: 1,
		TranslationX: h.pt(n, p.X-n.pressAt.X), TranslationY: h.pt(n, p.Y-n.pressAt.Y)}
	if dt > 0 {
		g.VelocityX, g.VelocityY = h.pt(n, p.X-n.panLast.X)/dt, h.pt(n, p.Y-n.panLast.Y)/dt
	}
	n.panLast, n.panTime = p, now
	h.emit(n, ops.Event{Kind: ops.EventPan, G: g})
	if state == ops.GestureStateEnded && n.listening(ops.EventSwipe) {
		const fast = 300 // points per second
		dx, dy := g.TranslationX, g.TranslationY
		var dir int64
		switch {
		case abs(dx) > abs(dy) && dx > 30 && g.VelocityX > fast:
			dir = 1 << 0 // right
		case abs(dx) > abs(dy) && dx < -30 && g.VelocityX < -fast:
			dir = 1 << 1 // left
		case dy < -30 && g.VelocityY < -fast:
			dir = 1 << 2 // up
		case dy > 30 && g.VelocityY > fast:
			dir = 1 << 3 // down
		}
		if dir != 0 {
			h.emit(n, ops.Event{Kind: ops.EventSwipe, Int: dir})
		}
	}
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// spin runs the timer of an activity ring, an indeterminate progress
// bar or ring, or a ring moving to its value, while it animates.
func (h *Host) spin(n *node) {
	if n.hwnd == 0 {
		return
	}
	if n.animating || n.indeterminate || n.kind == ops.KindProgressRing && n.shown != n.value {
		SetTimer(n.hwnd, timerSpin, 16)
	} else {
		KillTimer(n.hwnd, timerSpin)
	}
}

// pickColor shows the color dialog for a color picker, once the core
// has returned (it's a modal loop).
func (h *Host) pickColor(n *node) {
	id := n.id
	h.afterCore(func() {
		n := h.nodes[id]
		if n == nil {
			return
		}
		c, _ := h.resolve(n.colorValue)
		h.modal++
		ref, ok := ChooseColor(RootWindow(n.hwnd), c.ref())
		h.modal--
		if !ok {
			return
		}
		v := ops.RGBA(uint8(ref), uint8(ref>>8), uint8(ref>>16), 255)
		n.colorValue = v
		h.redrawControl(n)
		h.emit(n, ops.Event{Kind: ops.EventColorChanged, Int: int64(v)})
	})
}

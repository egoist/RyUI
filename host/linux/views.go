//go:build linux && !android

package linux

import (
	"math"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// create makes a node's view and, for a control, the control in it.
func (h *Host) create(id ops.NodeID, kind ops.Kind) {
	n := &node{id: id, kind: kind, opacity: 1, widget: newView(), selected: -1}
	switch kind {
	case ops.KindText:
		l := LabelNew("")
		LabelSetWrap(l, true)
		LabelSetWrapMode(l, WrapWordChar)
		LabelSetXAlign(l, 0)
		LabelSetYAlign(l, 0)
		h.setInner(n, l)
	case ops.KindButton:
		b := ButtonNew()
		h.setInner(n, b)
		n.handlers = append(n.handlers, Connect(b, "clicked", func() { h.pressed(n) }))
		h.styleButton(n)
	case ops.KindTextField:
		h.makeField(n)
	case ops.KindToggle:
		h.makeToggle(n, false)
	case ops.KindSlider:
		s := ScaleNewWithRange(OrientationHorizontal, 0, 1, 0.001)
		ScaleSetDrawValue(s, false)
		n.max = 1
		h.setInner(n, s)
		n.handlers = append(n.handlers, Connect(s, "value-changed", func() {
			if n.updating {
				return
			}
			n.value = RangeGetValue(s)
			h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: n.value})
		}))
	case ops.KindScroll:
		h.makeScroll(n, false)
	case ops.KindActivity:
		var s Ptr
		if AdwSpinnerNew != nil {
			s = AdwSpinnerNew()
		} else {
			s = SpinnerNew()
		}
		h.setInner(n, s)
	case ops.KindProgress:
		p := ProgressBarNew()
		WidgetSetVAlign(p, AlignCenter)
		h.setInner(n, p)
	case ops.KindImage, ops.KindSymbol:
		// Drawn by the view (image.go, symbols.go).
	case ops.KindSegmented:
		box := BoxNew(OrientationHorizontal, 0)
		WidgetAddCSSClass(box, "linked")
		h.setInner(n, box)
	case ops.KindWheel:
		d := DropDownNewFromStrings(nil)
		h.setInner(n, d)
		n.handlers = append(n.handlers, Connect(d, "notify::selected", func(uintptr) {
			if n.updating {
				return
			}
			n.selected = int(DropDownGetSelected(d))
			h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(n.selected)})
		}))
	case ops.KindStepper:
		h.makeStepper(n)
	case ops.KindColorPicker:
		h.makeColorPicker(n)
	case ops.KindDatePicker:
		h.makeDatePicker(n)
	default:
		if !h.createMore(n) {
			// A kind this host has no view for yet shows nothing, but
			// keeps its children.
		}
	}
	h.add(n)
	if n.widget != 0 && n.inner == 0 && n.kind != ops.KindView {
		WidgetSetCanTarget(n.widget, true)
	}
}

// setInner makes c the control in n's view.
func (h *Host) setInner(n *node, c Ptr) {
	if n.inner != 0 {
		for _, id := range n.handlers {
			Disconnect(n.inner, id)
		}
		n.handlers = nil
		WidgetUnparent(n.inner)
	}
	n.inner = c
	if c != 0 {
		WidgetSetParent(c, n.widget)
		Call(FnQueueAllocate, n.widget)
	}
}

func (h *Host) pressed(n *node) {
	if n.menu != nil && ops.MenuTrigger(n.menuTrigger) == ops.MenuTriggerTap {
		h.popupMenu(n, -1, -1)
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventPress})
}

func (h *Host) setOwn(n *node, p ops.PropID, v *ops.Value) {
	switch n.kind {
	case ops.KindText:
		if p == ops.PropTextText {
			n.text = v.Str
			LabelSetText(n.inner, v.Str)
			h.updateA11y(n)
		}
	case ops.KindButton:
		switch p {
		case ops.PropButtonTitle:
			n.title = v.Str
		case ops.PropButtonVariant:
			n.variant = int32(v.Int)
		case ops.PropButtonIcon:
			n.icon = v.Str
		}
		h.styleButton(n)
	case ops.KindTextField:
		h.setFieldProp(n, p, v)
	case ops.KindToggle:
		switch p {
		case ops.PropToggleOn:
			n.value = float64(v.Int)
			h.setToggle(n)
		case ops.PropToggleCheckbox:
			h.makeToggle(n, v.Int != 0)
		}
	case ops.KindSlider:
		n.updating = true
		switch p {
		case ops.PropSliderValue:
			n.value = v.Float
		case ops.PropSliderMin:
			n.min = v.Float
		case ops.PropSliderMax:
			n.max = v.Float
		}
		if n.max > n.min {
			RangeSetRange(n.inner, n.min, n.max)
			RangeSetIncrements(n.inner, (n.max-n.min)/100, (n.max-n.min)/10)
		}
		RangeSetValue(n.inner, n.value)
		n.updating = false
	case ops.KindScroll:
		h.setScrollProp(n, p, v)
	case ops.KindActivity:
		if p == ops.PropActivityAnimating {
			on := v.Int != 0
			if AdwSpinnerNew != nil {
				WidgetSetVisible(n.inner, on)
			} else {
				SpinnerSetSpinning(n.inner, on)
			}
		}
	case ops.KindProgress:
		switch p {
		case ops.PropProgressValue:
			n.value = v.Float
			if n.min == 0 { // not indeterminate
				ProgressBarSetFraction(n.inner, min(max(v.Float, 0), 1))
			}
		case ops.PropProgressIndeterminate:
			h.setIndeterminate(n, v.Int != 0)
		}
	case ops.KindImage:
		h.setImageProp(n, p, v)
	case ops.KindSymbol:
		if p == ops.PropSymbolName {
			n.symbol = v.Str
			h.styleSymbol(n)
		}
	case ops.KindSegmented:
		switch p {
		case ops.PropSegmentedSegments:
			n.items = ops.DecodeStrings(v.Bytes)
			h.buildSegments(n)
		case ops.PropSegmentedSelected:
			n.selected = int(v.Int)
			h.selectSegment(n)
		}
	case ops.KindWheel:
		n.updating = true
		switch p {
		case ops.PropWheelItems:
			n.items = ops.DecodeStrings(v.Bytes)
			model := StringListNew(nil)
			for _, s := range n.items {
				StringListAppend(model, s)
			}
			DropDownSetModel(n.inner, model)
			ObjectUnref(model)
			if n.selected >= 0 {
				DropDownSetSelected(n.inner, uint32(n.selected))
			}
		case ops.PropWheelSelected:
			n.selected = int(v.Int)
			DropDownSetSelected(n.inner, uint32(max(n.selected, 0)))
		}
		n.updating = false
	case ops.KindStepper:
		h.setStepperProp(n, p, v)
	case ops.KindColorPicker:
		h.setColorPickerProp(n, p, v)
	case ops.KindDatePicker:
		h.setDateProp(n, p, v)
	default:
		h.setOwnMore(n, p, v)
	}
}

// Buttons.

// styleButton sets a button's content (its title, its icon) and its
// libadwaita style: Filled is .suggested-action, Plain .flat in the accent
// color, Tinted an accent wash, Gray and Bordered the standard button, a
// destructive one's text red.
func (h *Host) styleButton(n *node) {
	b := n.inner
	if b == 0 {
		return
	}
	if n.icon != "" {
		c := ButtonContentNew()
		ButtonContentSetIconName(c, h.iconName(n.icon))
		ButtonContentSetLabel(c, n.title)
		ButtonSetChild(b, c)
		if n.title == "" {
			ButtonSetIconName(b, h.iconName(n.icon))
		}
	} else {
		ButtonSetLabel(b, n.title)
	}
	for _, c := range []string{"suggested-action", "flat", "asuka-tinted", "asuka-plain", "pill"} {
		WidgetRemoveCSSClass(b, c)
	}
	switch ops.ButtonVariant(n.variant) {
	case ops.ButtonVariantFilled, ops.ButtonVariantGlassProminent:
		WidgetAddCSSClass(b, "suggested-action")
	case ops.ButtonVariantPlain:
		WidgetAddCSSClass(b, "flat")
		WidgetAddCSSClass(b, "asuka-plain")
	case ops.ButtonVariantTinted:
		WidgetAddCSSClass(b, "asuka-tinted")
	}
	// The button's label takes the node's font and color, when it sets
	// them.
	if l := buttonLabel(b); l != 0 && (n.color != 0 || n.fontSize > 0 || n.textStyle != 0 || n.fontWeight != 0 || n.fontFamily != "") {
		h.setLabelAttrs(n, l)
	}
}

// buttonLabel is the GtkLabel a button shows its title in.
func buttonLabel(b Ptr) Ptr {
	labelType := Call(Sym("gtk_label_get_type"))
	var find func(w Ptr) Ptr
	find = func(w Ptr) Ptr {
		for c := WidgetGetFirstChild(w); c != 0; c = WidgetGetNextSibling(c) {
			if TypeCheckInstanceIsA(c, labelType) {
				return c
			}
			if l := find(c); l != 0 {
				return l
			}
		}
		return 0
	}
	return find(b)
}

// Toggles.

func (h *Host) makeToggle(n *node, checkbox bool) {
	if n.inner != 0 && checkbox == n.secure {
		return
	}
	n.secure = checkbox // the kind of control it is
	var t Ptr
	if checkbox {
		t = CheckButtonNew()
		h.setInner(n, t)
		n.handlers = append(n.handlers, Connect(t, "toggled", func() {
			if n.updating {
				return
			}
			on := CheckButtonGetActive(t)
			n.value = b2f(on)
			h.emit(n, ops.Event{Kind: ops.EventToggled, Bool: on})
		}))
	} else {
		t = SwitchNew()
		WidgetSetHAlign(t, AlignStart)
		WidgetSetVAlign(t, AlignCenter)
		h.setInner(n, t)
		n.handlers = append(n.handlers, Connect(t, "notify::active", func(uintptr) {
			if n.updating {
				return
			}
			on := SwitchGetActive(t)
			n.value = b2f(on)
			h.emit(n, ops.Event{Kind: ops.EventToggled, Bool: on})
		}))
	}
	h.setToggle(n)
}

func (h *Host) setToggle(n *node) {
	n.updating = true
	if n.secure {
		CheckButtonSetActive(n.inner, n.value != 0)
	} else {
		SwitchSetActive(n.inner, n.value != 0)
	}
	n.updating = false
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Progress.

func (h *Host) setIndeterminate(n *node, on bool) {
	if on == (n.min != 0) {
		return
	}
	if on {
		n.min = 1
		ProgressBarSetPulseStep(n.inner, 0.1)
		w := n.inner
		AddTickCallback(w, func(Ptr) bool {
			if n.min == 0 || n.inner != w {
				return false
			}
			if t := time.Now(); t.Sub(n.pulsed) > 100*time.Millisecond {
				n.pulsed = t
				ProgressBarPulse(w)
			}
			return true
		})
	} else {
		n.min = 0
		ProgressBarSetFraction(n.inner, min(max(n.value, 0), 1))
	}
}

// Segmented controls: linked toggle buttons in a group.

func (h *Host) buildSegments(n *node) {
	box := n.inner
	for c := WidgetGetFirstChild(box); c != 0; {
		next := WidgetGetNextSibling(c)
		BoxRemove(box, c)
		c = next
	}
	var first Ptr
	for i, s := range n.items {
		b := ToggleButtonNew()
		ButtonSetLabel(b, s)
		WidgetSetHExpand(b, true)
		if first == 0 {
			first = b
		} else {
			ToggleButtonSetGroup(b, first)
		}
		i := i
		Connect(b, "toggled", func() {
			if n.updating || !ToggleButtonGetActive(b) {
				return
			}
			n.selected = i
			h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
		})
		BoxAppend(box, b)
	}
	h.selectSegment(n)
}

func (h *Host) selectSegment(n *node) {
	n.updating = true
	i := 0
	for c := WidgetGetFirstChild(n.inner); c != 0; c = WidgetGetNextSibling(c) {
		ToggleButtonSetActive(c, i == n.selected)
		i++
	}
	n.updating = false
}

// Steppers: minus and plus buttons, linked, as GNOME's spin buttons
// show them.

func (h *Host) makeStepper(n *node) {
	box := BoxNew(OrientationHorizontal, 0)
	WidgetAddCSSClass(box, "linked")
	minus := ButtonNew()
	ButtonSetIconName(minus, "list-remove-symbolic")
	plus := ButtonNew()
	ButtonSetIconName(plus, "list-add-symbolic")
	BoxAppend(box, minus)
	BoxAppend(box, plus)
	n.step = 1
	n.max = math.Inf(1)
	n.min = math.Inf(-1)
	h.setInner(n, box)
	step := func(d float64) {
		v := min(max(n.value+d*n.step, n.min), n.max)
		if v == n.value {
			return
		}
		n.value = v
		h.updateStepper(n)
		h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: v})
	}
	Connect(minus, "clicked", func() { step(-1) })
	Connect(plus, "clicked", func() { step(1) })
}

func (h *Host) setStepperProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropStepperValue:
		n.value = v.Float
	case ops.PropStepperMin:
		n.min = v.Float
	case ops.PropStepperMax:
		n.max = v.Float
	case ops.PropStepperStep:
		n.step = v.Float
		if n.step <= 0 {
			n.step = 1
		}
	}
	h.updateStepper(n)
}

func (h *Host) updateStepper(n *node) {
	minus := WidgetGetFirstChild(n.inner)
	plus := WidgetGetNextSibling(minus)
	WidgetSetSensitive(minus, n.value > n.min)
	WidgetSetSensitive(plus, n.value < n.max)
}

// Color pickers: GTK's color dialog button.

func (h *Host) makeColorPicker(n *node) {
	d := ColorDialogNew()
	b := ColorDialogButtonNew(d) // takes the dialog
	WidgetSetHAlign(b, AlignStart)
	WidgetSetVAlign(b, AlignCenter)
	h.setInner(n, b)
	n.handlers = append(n.handlers, Connect(b, "notify::rgba", func(uintptr) {
		if n.updating {
			return
		}
		c := *At[RGBA](ColorDialogButtonGetRGBA(b))
		v := ops.RGBA(u8(c.R), u8(c.G), u8(c.B), u8(c.A))
		h.emit(n, ops.Event{Kind: ops.EventColorChanged, Int: int64(uint32(v))})
	}))
}

func u8(f float32) uint8 { return uint8(min(max(f, 0), 1)*255 + 0.5) }

func (h *Host) setColorPickerProp(n *node, p ops.PropID, v *ops.Value) {
	b := n.inner
	switch p {
	case ops.PropColorPickerValue:
		c, ok := h.resolve(ops.Color(v.Int))
		if !ok {
			return
		}
		n.updating = true
		ColorDialogButtonSetRGBA(b, &c)
		n.updating = false
	case ops.PropColorPickerAlpha:
		d := Ptr(0)
		var val Value
		ValueInit(&val, TypeObject)
		ObjectGetProperty(b, "dialog", &val)
		d = ValueGetObject(&val)
		if d != 0 {
			ColorDialogSetWithAlpha(d, v.Int != 0)
		}
		ValueUnset(&val)
	case ops.PropColorPickerTitle:
		var val Value
		ValueInit(&val, TypeObject)
		ObjectGetProperty(b, "dialog", &val)
		if d := ValueGetObject(&val); d != 0 {
			ColorDialogSetTitle(d, v.Str)
		}
		ValueUnset(&val)
	}
}

// Symbols: SF Symbol names as the icon theme's symbolic icons, sized by
// the text props, colored by Color (GTK recolors symbolic icons with the
// widget's color).

func (h *Host) styleSymbol(n *node) {
	n.icon = h.iconName(n.symbol)
	queueDraw(n)
}

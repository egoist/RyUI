//go:build darwin && !ios

package darwin

import (
	"math"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

func initView(cls string) ID {
	return MsgInitRect(Send(Class(cls), "alloc"), Sel("initWithFrame:"), Rect{})
}

// create makes the native view of a kind.
func (h *Host) create(id ops.NodeID, kind ops.Kind) {
	n := &node{id: id, kind: kind}
	if h.createNav(n) {
		h.add(n)
		return
	}
	var v ID
	switch kind {
	case ops.KindView, ops.KindSelectable:
		v = initView("RyUIView")
	case ops.KindText:
		v = initView("RyUILabel")
		Send(v, "setEditable:", 0)
		Send(v, "setSelectable:", 0)
		Send(v, "setBezeled:", 0)
		Send(v, "setDrawsBackground:", 0)
		Send(v, "setLineBreakMode:", 0)
		cell := Send(v, "cell")
		Send(cell, "setWraps:", 1)
		Send(cell, "setScrollable:", 0)
		Send(v, "setFont:", uintptr(styleFont(0)))
		Send(v, "setTextColor:", uintptr(Send(Class("NSColor"), "labelColor")))
	case ops.KindButton:
		v = initView("NSButton")
		Send(v, "setBezelStyle:", 1) // push
		Send(v, "setButtonType:", 7) // momentary push in
		Send(v, "setTitle:", uintptr(String("")))
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindTextField:
		v = h.makeField(n, false)
	case ops.KindToggle:
		v = initView("NSSwitch")
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindSlider:
		v = initView("NSSlider")
		MsgSetFloat(v, Sel("setMinValue:"), 0)
		MsgSetFloat(v, Sel("setMaxValue:"), 1)
		Send(v, "setContinuous:", 1)
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
		n.max = 1
	case ops.KindStepper:
		v = initView("NSStepper")
		MsgSetFloat(v, Sel("setMaxValue:"), 100)
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindScroll:
		v = initView("NSScrollView")
		Send(v, "setHasVerticalScroller:", 1)
		Send(v, "setAutohidesScrollers:", 1)
		Send(v, "setDrawsBackground:", 0)
		Send(Send(v, "contentView"), "setDrawsBackground:", 0)
		Send(v, "setAutomaticallyAdjustsContentInsets:", 0)
	case ops.KindList:
		n.list = newList(h, n)
		v = n.list.scroll
	case ops.KindActivity:
		v = initView("NSProgressIndicator")
		Send(v, "setStyle:", 1) // spinning
		Send(v, "setIndeterminate:", 1)
		Send(v, "setDisplayedWhenStopped:", 0)
		Send(v, "setControlSize:", 0)
	case ops.KindProgress:
		v = initView("NSProgressIndicator")
		Send(v, "setStyle:", 0) // bar
		Send(v, "setIndeterminate:", 0)
		MsgSetFloat(v, Sel("setMinValue:"), 0)
		MsgSetFloat(v, Sel("setMaxValue:"), 1)
	case ops.KindImage:
		v = initView("RyUIImageView")
		Send(v, "setImageScaling:", 3) // proportionally up or down
		Send(v, "setImageFrameStyle:", 0)
		Send(v, "setEditable:", 0)
		Send(v, "setAnimates:", 1)
	case ops.KindSymbol:
		v = initView("RyUIImageView")
		Send(v, "setImageScaling:", 0)
		Send(v, "setContentTintColor:", uintptr(Send(Class("NSColor"), "labelColor")))
	case ops.KindSegmented:
		v = initView("NSSegmentedControl")
		Send(v, "setTrackingMode:", 0) // select one
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindWheel:
		// macOS picks one of many from a pop-up button.
		v = MsgInitRect(Send(Class("NSPopUpButton"), "alloc"), Sel("initWithFrame:"), Rect{})
		Send(v, "setPullsDown:", 0)
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindDatePicker:
		v = initView("NSDatePicker")
		Send(v, "setDatePickerStyle:", 0)       // text field and stepper
		Send(v, "setDatePickerElements:", 0xe0) // year, month, day
		Send(v, "setBezeled:", 1)
		Send(v, "setDrawsBackground:", 1)
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindColorPicker:
		v = initView("NSColorWell")
		if RespondsTo(v, "setColorWellStyle:") {
			Send(v, "setColorWellStyle:", 1) // minimal: a swatch that opens the picker
		}
		Send(v, "setTarget:", uintptr(h.target))
		Send(v, "setAction:", uintptr(Sel("act:")))
	case ops.KindRichText:
		n.rich = newRich()
		v = n.rich.makeView()
	case ops.KindShape:
		n.shape = &shape{lineWidth: 1}
		v = initView("RyUIShapeView")
	case ops.KindWebView:
		v = h.makeWeb(n)
	case ops.KindVideo:
		v = h.makeVideo(n)
	case ops.KindNative:
		fail("can't show native views (ui.Native) yet: they're the app's Swift and Kotlin")
	default:
		fail("can't show %s yet", kind)
	}
	if v == 0 {
		fail("couldn't make a %s", kind)
	}
	n.view = v
	Send(v, "setAutoresizingMask:", 0)
	Send(v, "setTranslatesAutoresizingMaskIntoConstraints:", 1)
	h.add(n)
}

// makeField makes a TextField's view: a field, a secure field, or (made
// multiline) a text view in a scroll view.
func (h *Host) makeField(n *node, secure bool) ID {
	cls := "RyUITextField"
	if secure {
		cls = "RyUISecureField"
	}
	v := initView(cls)
	Send(v, "setBezeled:", 1)
	Send(v, "setBezelStyle:", 1) // rounded
	Send(v, "setEditable:", 1)
	Send(v, "setSelectable:", 1)
	Send(v, "setDelegate:", uintptr(h.target))
	Send(Send(v, "cell"), "setScrollable:", 1)
	Send(Send(v, "cell"), "setUsesSingleLineMode:", 1)
	Send(v, "setFont:", uintptr(styleFont(0)))
	return v
}

// replaceView swaps n's view for v, keeping its place, frame and props
// that matter (a field made secure or multiline).
func (h *Host) replaceView(n *node, v ID) {
	old := n.view
	super := Send(old, "superview")
	MsgSetRect(v, selSetFrame, n.frame)
	Send(v, "setAutoresizingMask:", 0)
	if super != 0 {
		Send(super, "replaceSubview:with:", uintptr(old), uintptr(v))
	}
	delete(h.byView, old)
	Release(old)
	n.view = v
	h.byView[v] = n
	h.applyFont(n)
	h.applyColor(n)
}

func (h *Host) setOwn(n *node, p ops.PropID, v *ops.Value) {
	switch n.kind {
	case ops.KindText:
		if p == ops.PropTextText {
			n.text = v.Str
			Send(n.view, "setStringValue:", uintptr(String(v.Str)))
		}
	case ops.KindButton:
		switch p {
		case ops.PropButtonTitle:
			n.title = v.Str
			h.buttonTitle(n)
		case ops.PropButtonVariant:
			n.variant = int32(v.Int)
			h.buttonVariant(n)
		case ops.PropButtonIcon:
			n.icon = v.Str
			if img := symbolImage(v.Str); img != 0 {
				Send(n.view, "setImage:", uintptr(img))
				Send(n.view, "setImagePosition:", 7) // leading
			} else {
				Send(n.view, "setImage:", 0)
			}
			h.buttonTitle(n)
		}
	case ops.KindTextField:
		h.setField(n, p, v)
	case ops.KindToggle:
		if p == ops.PropToggleOn {
			Send(n.view, "setState:", uintptr(v.Int))
		}
	case ops.KindSlider:
		switch p {
		case ops.PropSliderValue:
			MsgSetFloat(n.view, Sel("setDoubleValue:"), v.Float)
		case ops.PropSliderMin:
			MsgSetFloat(n.view, Sel("setMinValue:"), v.Float)
		case ops.PropSliderMax:
			MsgSetFloat(n.view, Sel("setMaxValue:"), v.Float)
		}
	case ops.KindStepper:
		switch p {
		case ops.PropStepperValue:
			MsgSetFloat(n.view, Sel("setDoubleValue:"), v.Float)
		case ops.PropStepperMin:
			MsgSetFloat(n.view, Sel("setMinValue:"), v.Float)
		case ops.PropStepperMax:
			MsgSetFloat(n.view, Sel("setMaxValue:"), v.Float)
		case ops.PropStepperStep:
			MsgSetFloat(n.view, Sel("setIncrement:"), v.Float)
		}
	case ops.KindScroll:
		if p == ops.PropScrollHorizontal {
			Send(n.view, "setHasHorizontalScroller:", Bool(v.Int != 0))
			Send(n.view, "setHasVerticalScroller:", Bool(v.Int == 0))
		}
		// Pull to refresh: not on macOS.
	case ops.KindList:
		n.list.setProp(h, n, p, v)
	case ops.KindActivity:
		if p == ops.PropActivityAnimating {
			if v.Int != 0 {
				Send(n.view, "startAnimation:", 0)
			} else {
				Send(n.view, "stopAnimation:", 0)
			}
		}
	case ops.KindProgress:
		switch p {
		case ops.PropProgressValue:
			MsgSetFloat(n.view, Sel("setDoubleValue:"), v.Float)
		case ops.PropProgressIndeterminate:
			Send(n.view, "setIndeterminate:", Bool(v.Int != 0))
			if v.Int != 0 {
				Send(n.view, "startAnimation:", 0)
			} else {
				Send(n.view, "stopAnimation:", 0)
			}
		}
	case ops.KindImage:
		h.setImageProp(n, p, v)
	case ops.KindSymbol:
		if p == ops.PropSymbolName {
			n.symbol = v.Str
			h.applySymbol(n)
		}
	case ops.KindSegmented:
		switch p {
		case ops.PropSegmentedSegments:
			n.items = decodeStrings(v.Bytes)
			Send(n.view, "setSegmentCount:", uintptr(len(n.items)))
			for i, s := range n.items {
				Send(n.view, "setLabel:forSegment:", uintptr(String(s)), uintptr(i))
			}
			Send(n.view, "setSelectedSegment:", uintptr(n.selected))
		case ops.PropSegmentedSelected:
			n.selected = int(v.Int)
			Send(n.view, "setSelectedSegment:", uintptr(v.Int))
		}
	case ops.KindWheel:
		switch p {
		case ops.PropWheelItems:
			n.items = decodeStrings(v.Bytes)
			Send(n.view, "removeAllItems")
			for _, s := range n.items {
				// addItemWithTitle: drops duplicate titles; menu items don't.
				Send(Send(n.view, "menu"), "addItemWithTitle:action:keyEquivalent:", uintptr(String(s)), 0, uintptr(String("")))
			}
			Send(n.view, "selectItemAtIndex:", uintptr(n.selected))
		case ops.PropWheelSelected:
			n.selected = int(v.Int)
			Send(n.view, "selectItemAtIndex:", uintptr(v.Int))
		}
	case ops.KindDatePicker:
		switch p {
		case ops.PropDatePickerValue:
			Send(n.view, "setDateValue:", uintptr(dateMillis(v.Int)))
		case ops.PropDatePickerMode:
			el := uintptr(0xe0)
			switch ops.DateMode(v.Int) {
			case ops.DateModeTime:
				el = 0x0c // hour, minute
			case ops.DateModeDateTime:
				el = 0xec
			}
			Send(n.view, "setDatePickerElements:", el)
		case ops.PropDatePickerMin:
			if v.Int != 0 {
				Send(n.view, "setMinDate:", uintptr(dateMillis(v.Int)))
			} else {
				Send(n.view, "setMinDate:", 0)
			}
		case ops.PropDatePickerMax:
			if v.Int != 0 {
				Send(n.view, "setMaxDate:", uintptr(dateMillis(v.Int)))
			} else {
				Send(n.view, "setMaxDate:", 0)
			}
		}
	case ops.KindColorPicker:
		switch p {
		case ops.PropColorPickerValue:
			if c := h.nsColor(ops.Color(v.Int)); c != 0 {
				Send(n.view, "setColor:", uintptr(c))
			}
		case ops.PropColorPickerAlpha:
			Send(Send(Class("NSColorPanel"), "sharedColorPanel"), "setShowsAlpha:", Bool(v.Int != 0))
		}
	case ops.KindRichText:
		n.rich.setProp(h, n, p, v)
	case ops.KindShape:
		n.shape.setProp(h, n, p, v)
	case ops.KindWebView:
		n.web.setProp(h, n, p, v)
	case ops.KindVideo:
		n.video.setProp(h, n, p, v)
	default:
		h.setNavProp(n, p, v)
	}
}

func dateMillis(ms int64) ID {
	return MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSince1970:"), float64(ms)/1000)
}

// buttonTitle sets a button's title, colored for a plain or tinted
// button (the bezel can't take a color for the text).
func (h *Host) buttonTitle(n *node) {
	title := String(n.title)
	var color ID
	switch ops.ButtonVariant(n.variant) {
	case ops.ButtonVariantPlain:
		color = h.nsColor(n.color)
		if color == 0 {
			color = h.accentColor()
		}
	case ops.ButtonVariantTinted:
		color = h.accentColor()
	}
	if color == 0 && n.color != 0 {
		color = h.nsColor(n.color)
	}
	if color == 0 {
		Send(n.view, "setTitle:", uintptr(title))
		return
	}
	attrs := Dict(
		Const(AppKit(), "NSForegroundColorAttributeName"), color,
		Const(AppKit(), "NSFontAttributeName"), Send(n.view, "font"),
	)
	s := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(title), uintptr(attrs))
	Send(n.view, "setAttributedTitle:", uintptr(Autorelease(s)))
}

// buttonVariant maps a variant to a macOS button: Plain is borderless
// text in the accent color, Filled the accent-colored default button,
// the others push buttons.
func (h *Host) buttonVariant(n *node) {
	v := n.view
	switch ops.ButtonVariant(n.variant) {
	case ops.ButtonVariantPlain:
		Send(v, "setBordered:", 0)
		Send(v, "setBezelColor:", 0)
	case ops.ButtonVariantFilled:
		Send(v, "setBordered:", 1)
		Send(v, "setBezelStyle:", 1)
		if n.background == 0 {
			Send(v, "setBezelColor:", uintptr(h.accentColor()))
		}
	default:
		Send(v, "setBordered:", 1)
		Send(v, "setBezelStyle:", 1)
	}
	h.buttonTitle(n)
}

func (h *Host) setField(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropTextFieldValue:
		n.text = v.Str
		if n.tracking != 0 {
			Send(n.tracking, "setString:", uintptr(String(v.Str)))
		} else {
			Send(n.view, "setStringValue:", uintptr(String(v.Str)))
		}
	case ops.PropTextFieldPlaceholder:
		n.placeholder = v.Str
		if n.tracking == 0 {
			Send(n.view, "setPlaceholderString:", uintptr(String(v.Str)))
		} else if RespondsTo(n.tracking, "setPlaceholderString:") {
			Send(n.tracking, "setPlaceholderString:", uintptr(String(v.Str)))
		}
	case ops.PropTextFieldSecure:
		secure := v.Int != 0
		if secure == SendBool(n.view, "isKindOfClass:", uintptr(Class("NSSecureTextField"))) || n.tracking != 0 {
			return
		}
		nv := h.makeField(n, secure)
		Send(nv, "setStringValue:", uintptr(String(n.text)))
		Send(nv, "setPlaceholderString:", uintptr(String(n.placeholder)))
		h.replaceView(n, nv)
	case ops.PropTextFieldMultiline:
		if v.Int != 0 && n.tracking == 0 {
			h.makeMultiline(n)
		}
	case ops.PropTextFieldAutocorrect:
		if n.tracking != 0 {
			Send(n.tracking, "setAutomaticSpellingCorrectionEnabled:", Bool(ops.Autocorrect(v.Int) != ops.AutocorrectNo))
		}
	case ops.PropTextFieldContentType:
		if RespondsTo(n.view, "setContentType:") && v.Str != "" {
			if t := contentType(v.Str); t != 0 {
				Send(n.view, "setContentType:", uintptr(t))
			}
		}
	}
}

// contentType maps an AutoFill hint to NSTextContentType.
func contentType(s string) ID {
	var name string
	switch s {
	case "username":
		name = "NSTextContentTypeUsername"
	case "password", "newPassword":
		name = "NSTextContentTypePassword"
	case "oneTimeCode":
		name = "NSTextContentTypeOneTimeCode"
	default:
		return 0
	}
	return Const(AppKit(), name)
}

// makeMultiline makes a TextField a text view in a scroll view: return
// is a new line, and it grows with its text up to its line limit.
func (h *Host) makeMultiline(n *node) {
	sv := initView("NSScrollView")
	Send(sv, "setHasVerticalScroller:", 1)
	Send(sv, "setAutohidesScrollers:", 1)
	Send(sv, "setBorderType:", 2) // bezel
	tv := initView("NSTextView")
	Send(tv, "setRichText:", 0)
	Send(tv, "setDelegate:", uintptr(h.target))
	Send(tv, "setVerticallyResizable:", 1)
	Send(tv, "setHorizontallyResizable:", 0)
	Send(tv, "setAutoresizingMask:", 2) // width sizable
	Send(Send(tv, "textContainer"), "setWidthTracksTextView:", 1)
	MsgSetSize(tv, Sel("setTextContainerInset:"), Size{Width: 2, Height: 4})
	Send(tv, "setAllowsUndo:", 1)
	Send(sv, "setDocumentView:", uintptr(tv))
	Send(tv, "setString:", uintptr(String(n.text)))
	n.tracking = tv
	n.multiline = true
	h.replaceView(n, sv)
	h.byView[tv] = n
}

func (h *Host) textViewNode(tv ID) *node {
	n := h.byView[tv]
	if n == nil || n.tracking != tv {
		return nil
	}
	return n
}

func (h *Host) textViewChanged(tv ID) {
	n := h.textViewNode(tv)
	if n == nil {
		return
	}
	s := GoString(Send(tv, "string"))
	n.text = s
	h.emit(n, ops.Event{Kind: ops.EventTextChanged, Str: s})
}

func (h *Host) textChanged(field ID) {
	n := h.byView[field]
	if n == nil || n.kind != ops.KindTextField {
		h.searchChanged(field)
		return
	}
	s := GoString(Send(field, "stringValue"))
	n.text = s
	h.emit(n, ops.Event{Kind: ops.EventTextChanged, Str: s})
}

// textCommand handles return in a field: Submit.
func (h *Host) textCommand(control, tv ID, cmd SEL) bool {
	n := h.byView[control]
	if n == nil {
		return h.searchCommand(control, cmd)
	}
	if cmd == Sel("insertNewline:") {
		h.emit(n, ops.Event{Kind: ops.EventSubmit})
		return n.listening(ops.EventSubmit)
	}
	return false
}

// act is a control's action.
func (h *Host) act(sender ID) {
	n := h.byView[sender]
	if n == nil {
		h.barItem(sender)
		return
	}
	switch n.kind {
	case ops.KindButton:
		if n.menu != 0 && n.menuTrigger == int32(ops.MenuTriggerTap) {
			h.popUp(n)
			return
		}
		h.emit(n, ops.Event{Kind: ops.EventPress})
	case ops.KindToggle:
		h.emit(n, ops.Event{Kind: ops.EventToggled, Bool: SendInt(sender, "state") != 0})
	case ops.KindSlider, ops.KindStepper:
		h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: MsgFloat(sender, Sel("doubleValue"))})
	case ops.KindSegmented:
		n.selected = SendInt(sender, "selectedSegment")
		h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(n.selected)})
	case ops.KindWheel:
		n.selected = SendInt(sender, "indexOfSelectedItem")
		h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(n.selected)})
	case ops.KindDatePicker:
		t := MsgFloat(Send(sender, "dateValue"), Sel("timeIntervalSince1970"))
		h.emit(n, ops.Event{Kind: ops.EventValueChanged, Float: math.Round(t * 1000)})
	case ops.KindColorPicker:
		h.emit(n, ops.Event{Kind: ops.EventColorChanged, Int: int64(h.goColor(Send(sender, "color")))})
	}
}

// goColor is an NSColor as an RGBA ops.Color.
func (h *Host) goColor(c ID) ops.Color {
	c = Send(c, "colorUsingColorSpace:", uintptr(Send(Class("NSColorSpace"), "sRGBColorSpace")))
	if c == 0 {
		return 0
	}
	ch := func(s string) uint8 {
		return uint8(math.Round(min(max(MsgFloat(c, Sel(s)), 0), 1) * 255))
	}
	return ops.RGBA(ch("redComponent"), ch("greenComponent"), ch("blueComponent"), ch("alphaComponent"))
}

// listen adds what an event needs on the native side: gesture
// recognizers on views, scroll observation.
func (h *Host) listen(n *node, e ops.EventKind) {
	if e == ops.EventScrolled && (n.kind == ops.KindScroll || n.kind == ops.KindList) && !n.scroll {
		n.scroll = true
		clip := Send(n.view, "contentView")
		Send(clip, "setPostsBoundsChangedNotifications:", 1)
		center := Send(Class("NSNotificationCenter"), "defaultCenter")
		Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("boundsChanged:")),
			uintptr(String("NSViewBoundsDidChangeNotification")), uintptr(clip))
		return
	}
	if n.kind != ops.KindView || n.view == 0 {
		return
	}
	add := func(cls, action string) ID {
		g := Send(Send(Class(cls), "alloc"), "initWithTarget:action:", uintptr(h.target), uintptr(Sel(action)))
		Send(n.view, "addGestureRecognizer:", uintptr(g))
		Release(g)
		return g
	}
	switch e {
	case ops.EventPress:
		add("NSClickGestureRecognizer", "click:")
		Send(n.view, "setAccessibilityElement:", 1)
		Send(n.view, "setAccessibilityRole:", uintptr(String("AXButton")))
	case ops.EventLongPress:
		g := add("NSPressGestureRecognizer", "longPress:")
		MsgSetFloat(g, Sel("setMinimumPressDuration:"), 0.5)
		// A secondary click is a Mac's long press.
		r := add("NSClickGestureRecognizer", "longPress:")
		Send(r, "setButtonMask:", 2)
	case ops.EventPan:
		add("NSPanGestureRecognizer", "pan:")
	case ops.EventPinch:
		add("NSMagnificationGestureRecognizer", "magnify:")
	case ops.EventRotate:
		add("NSRotationGestureRecognizer", "rotate:")
	}
}

// gesture reports a recognizer's gesture.
func (h *Host) gesture(g ID, e ops.EventKind) {
	view := Send(g, "view")
	n := h.byView[view]
	if n == nil {
		return
	}
	state := SendInt(g, "state") // possible, began, changed, ended, cancelled, failed
	switch e {
	case ops.EventPress:
		if state == 3 {
			h.emit(n, ops.Event{Kind: ops.EventPress})
		}
		return
	case ops.EventLongPress:
		if state == 1 || state == 3 && SendBool(g, "isKindOfClass:", uintptr(Class("NSClickGestureRecognizer"))) {
			if n.menu != 0 && n.menuTrigger == int32(ops.MenuTriggerLongPress) {
				return // its context menu shows
			}
			h.emit(n, ops.Event{Kind: ops.EventLongPress})
		}
		return
	}
	var gs ops.Gesture
	switch state {
	case 1:
		gs.State = ops.GestureStateBegan
	case 2:
		gs.State = ops.GestureStateChanged
	case 3:
		gs.State = ops.GestureStateEnded
	default:
		gs.State = ops.GestureStateCancelled
	}
	loc := MsgIDToPoint(g, Sel("locationInView:"), view)
	gs.X, gs.Y = float32(loc.X), float32(loc.Y)
	gs.Scale = 1
	switch e {
	case ops.EventPan:
		t := MsgIDToPoint(g, Sel("translationInView:"), view)
		vel := MsgIDToPoint(g, Sel("velocityInView:"), view)
		gs.TranslationX, gs.TranslationY = float32(t.X), float32(t.Y)
		gs.VelocityX, gs.VelocityY = float32(vel.X), float32(vel.Y)
	case ops.EventPinch:
		gs.Scale = float32(1 + MsgFloat(g, Sel("magnification")))
	case ops.EventRotate:
		// AppKit's is counterclockwise; RyUI's clockwise.
		gs.Rotation = -float32(MsgFloat(g, Sel("rotation")))
	}
	h.emit(n, ops.Event{Kind: e, G: gs})
}

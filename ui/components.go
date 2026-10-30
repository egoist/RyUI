package ui

import (
	"encoding/binary"
	"time"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// Role is what a view is to assistive technologies.
type Role = ops.Role

const (
	RoleNone   = ops.RoleNone
	RoleButton = ops.RoleButton
	RoleHeader = ops.RoleHeader
	RoleImage  = ops.RoleImage
	RoleLink   = ops.RoleLink
	RoleText   = ops.RoleText
	RoleSearch = ops.RoleSearch
)

// ViewProps configures a View.
type ViewProps struct {
	Style       Styler
	OnPress     func()
	OnLongPress func()
	// Gestures, with the platform's recognizers: a pan (drag), a pinch,
	// and swipes in any direction.
	OnPan   func(Gesture)
	OnPinch func(Gesture)
	OnSwipe func(SwipeDirection)
	// OnRotate is two fingers turning: Gesture.Rotation is the angle, in
	// radians clockwise, since they began.
	OnRotate func(Gesture)
	// Accessibility. A view with any of these is an accessibility
	// element; plain containers never appear in the accessibility tree.
	Label string
	Hint  string
	Role  Role
	// Decorative hides the view, and everything in it, from screen
	// readers: a decoration, or what its neighbors already say.
	Decorative bool
	// OnLayout is called with the view's frame once it's laid out, and
	// again each time layout changes it (SwiftUI's GeometryReader, React
	// Native's onLayout): to size something else by it, or to know where
	// it is. It runs after the frame is applied; what it writes is laid
	// out in turn.
	OnLayout func(Frame)
	// Actions are what a screen reader offers to do with the view
	// (VoiceOver's rotor, TalkBack's actions menu): what a swipe or a
	// long press does for a sighted user, say ("Delete", "Reply").
	Actions []A11yAction
	// OnAdjust makes the view adjustable, as a slider is: a screen
	// reader's swipe up or down (TalkBack's volume keys) steps it, by 1
	// or -1. Give it a Label, and a Hint or value text that says where
	// it is.
	OnAdjust func(by int)
}

// A11yAction is a screen reader's action on a view.
type A11yAction struct {
	Name string
	Do   func()
}

// Frame is a view's place in its parent and its size, in points.
type Frame struct {
	X, Y, Width, Height float32
}

// Val is a value that never changes, for a prop that takes an accessor:
// Title: ui.Val("Save"). It costs nothing at run time: the prop is sent
// once and no effect is made. Leaving a prop nil means its zero value.
func Val[T any](v T) signal.Accessor[T] { return signal.Static(v) }

// bind sets a prop from an accessor: once for a static one, else from a
// render effect that sends only real changes.
func bind[T comparable](a signal.Accessor[T], apply func(T)) {
	if a == nil {
		return
	}
	if v, ok := signal.StaticValue(a); ok {
		apply(v)
		return
	}
	var last T
	first := true
	signal.RenderEffect(func() {
		v := a.Get()
		if !first && v == last {
			return
		}
		first, last = false, v
		apply(v)
	})
}

func (e *element) listen(ev ops.EventKind) { e.r.batch.Listen(e.id, ev) }

func (e *element) handlers() *handlers {
	if e.h == nil {
		e.h = &handlers{}
	}
	return e.h
}

func firstStyle(s []Styler) Styler {
	if len(s) > 0 {
		return s[0]
	}
	return nil
}

func addKids(e *element, kids []Node) {
	for _, k := range kids {
		if k.e != nil {
			e.appendKid(k.e)
		}
	}
}

// View is a container. Without visual props, events or accessibility
// props it makes no native view: its children are placed in the nearest
// ancestor that has one.
func View(p ViewProps, children ...Node) Node {
	real := p.OnPress != nil || p.OnLongPress != nil || p.Label != "" || p.Hint != "" || p.Role != RoleNone || p.Decorative || p.OnLayout != nil ||
		len(p.Actions) > 0 || p.OnAdjust != nil ||
		p.OnPan != nil || p.OnPinch != nil || p.OnSwipe != nil || p.OnRotate != nil
	if s, ok := p.Style.(Style); ok && s.visual() {
		real = true
	}
	e := newElement(ops.KindView, real)
	if real {
		h := e.handlers()
		h.press, h.longPress = p.OnPress, p.OnLongPress
		if h.press != nil {
			e.listen(ops.EventPress)
		}
		if h.longPress != nil {
			e.listen(ops.EventLongPress)
		}
		if p.Label != "" {
			e.r.batch.SetString(e.id, ops.PropA11yLabel, p.Label)
		}
		if p.Hint != "" {
			e.r.batch.SetString(e.id, ops.PropA11yHint, p.Hint)
		}
		if p.Role != RoleNone {
			e.r.batch.SetEnum(e.id, ops.PropA11yRole, int32(p.Role))
		}
		if p.Decorative {
			e.r.batch.SetBool(e.id, ops.PropA11yHidden, true)
		}
		h.layout = p.OnLayout
		if len(p.Actions) > 0 {
			var b []byte
			b = binary.LittleEndian.AppendUint32(b, uint32(len(p.Actions)))
			for _, a := range p.Actions {
				b = binary.LittleEndian.AppendUint32(b, uint32(len(a.Name)))
				b = append(b, a.Name...)
				h.a11yActions = append(h.a11yActions, a.Do)
			}
			e.r.batch.SetBytes(e.id, ops.PropA11yActions, b)
			e.listen(ops.EventA11yAction)
		}
		if p.OnAdjust != nil {
			h.adjust = p.OnAdjust
			e.r.batch.SetBool(e.id, ops.PropA11yAdjustable, true)
			e.listen(ops.EventAdjust)
		}
		e.listenGestures(&p)
	}
	e.setStyle(p.Style, false)
	addKids(e, children)
	return Node{e}
}

func withDirection(st Styler, d Direction) Styler {
	if st == nil {
		return Style{Direction: d}
	}
	s, f := st.styler()
	if f == nil {
		s.Direction = d
		return s
	}
	return StyleFunc(func() Style {
		s := f()
		s.Direction = d
		return s
	})
}

// Column stacks its children top to bottom.
func Column(style Styler, children ...Node) Node {
	return View(ViewProps{Style: withDirection(style, ColumnDir)}, children...)
}

// Row stacks its children leading to trailing.
func Row(style Styler, children ...Node) Node {
	return View(ViewProps{Style: withDirection(style, RowDir)}, children...)
}

// Environment is what the platform says about where a component shows:
// its size, the safe area's insets, the keyboard, dark mode, the text
// size, the locale and time zone.
type Environment = host.Environment

// UseEnvironment is the environment of the screen (sheet, tab, or root)
// the component is in, as a signal: read it in an effect or a
// StyleFunc, and it follows rotation, the keyboard, dark mode.
func UseEnvironment() signal.Accessor[Environment] {
	if s := envCtx.Use(); s != nil {
		return s
	}
	return current().env
}

// SafeArea is a Column padded by the safe area insets (the notch, the
// home indicator, system bars) and the keyboard, on top of its style's
// padding.
func SafeArea(style Styler, children ...Node) Node {
	n := Column(style)
	e := n.e
	envSig := envCtx.Use()
	if envSig == nil {
		envSig = e.r.env
	}
	signal.RenderEffect(func() {
		env := envSig.Get()
		pad := Edges{env.SafeTop, env.SafeRight, max(env.SafeBottom, env.Keyboard), env.SafeLeft}
		if pad == e.extraPad {
			return
		}
		e.extraPad = pad
		e.box.SetStyle(e.layoutStyle(&e.style))
	})
	addKids(e, children)
	return n
}

// Text shows text. It wraps by default; Style.Lines limits it.
func Text(text signal.Accessor[string], style ...Styler) Node {
	e := newElement(ops.KindText, true)
	e.measure()
	e.setStyle(firstStyle(style), true)
	bind(text, func(v string) {
		e.text = v
		e.r.batch.SetString(e.id, ops.PropTextText, v)
		e.remeasure()
	})
	return Node{e}
}

// StaticText is a Text whose text never changes.
func StaticText(text string, style ...Styler) Node { return Text(signal.Static(text), style...) }

// ButtonVariant is a button's look: the platform's styles (UIButton
// configurations, Material buttons).
type ButtonVariant = ops.ButtonVariant

const (
	ButtonPlain    = ops.ButtonVariantPlain
	ButtonFilled   = ops.ButtonVariantFilled
	ButtonTinted   = ops.ButtonVariantTinted
	ButtonGray     = ops.ButtonVariantGray
	ButtonBordered = ops.ButtonVariantBordered
)

// ButtonProps configures a Button.
type ButtonProps struct {
	Title   signal.Accessor[string]
	OnPress func()
	// Menu shows these actions when the button is tapped (instead of
	// OnPress).
	Menu    []Action
	Variant ButtonVariant
	// Icon is a symbol before the title (see Symbol); a button with an
	// icon and no title is the icon alone.
	Icon Icon
	// Label is what VoiceOver and TalkBack read: the title by default.
	// Give an icon-only button one (the platforms name a few symbols
	// themselves, in English on Android).
	Label    string
	Disabled signal.Accessor[bool]
	Style    Styler
}

// Button is the platform's button.
func Button(p ButtonProps) Node {
	e := newElement(ops.KindButton, true)
	e.measure()
	e.setStyle(p.Style, true)
	if p.Variant != ButtonPlain {
		e.variant = int32(p.Variant)
		e.r.batch.SetEnum(e.id, ops.PropButtonVariant, int32(p.Variant))
	}
	if p.Icon != "" {
		e.r.batch.SetString(e.id, ops.PropButtonIcon, string(p.Icon))
	}
	if p.Label != "" {
		e.r.batch.SetString(e.id, ops.PropA11yLabel, p.Label)
	}
	bind(p.Title, func(v string) {
		e.text = string(p.Icon) + "\x00" + v // the measure cache's key
		e.r.batch.SetString(e.id, ops.PropButtonTitle, v)
		e.remeasure()
	})
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.OnPress != nil {
		e.handlers().press = p.OnPress
		e.listen(ops.EventPress)
	}
	if len(p.Menu) > 0 {
		menu := p.Menu
		e.r.batch.SetBytes(e.id, ops.PropMenu, encodeActions(menu))
		e.handlers().action = func(i int64) { runAction(menu, i) }
		e.listen(ops.EventAction)
	}
	return Node{e}
}

// Keyboard types, return keys and text field options.
type (
	KeyboardType   = ops.Keyboard
	ReturnKey      = ops.ReturnKey
	Autocapitalize = ops.Autocapitalize
	Autocorrect    = ops.Autocorrect
)

const (
	KeyboardDefault = ops.KeyboardDefault
	KeyboardNumber  = ops.KeyboardNumber
	KeyboardDecimal = ops.KeyboardDecimal
	KeyboardEmail   = ops.KeyboardEmail
	KeyboardURL     = ops.KeyboardURL
	KeyboardPhone   = ops.KeyboardPhone

	ReturnDefault = ops.ReturnKeyDefault
	ReturnDone    = ops.ReturnKeyDone
	ReturnGo      = ops.ReturnKeyGo
	ReturnNext    = ops.ReturnKeyNext
	ReturnSearch  = ops.ReturnKeySearch
	ReturnSend    = ops.ReturnKeySend

	CapitalizeSentences  = ops.AutocapitalizeSentences
	CapitalizeNone       = ops.AutocapitalizeNone
	CapitalizeWords      = ops.AutocapitalizeWords
	CapitalizeCharacters = ops.AutocapitalizeCharacters

	AutocorrectDefault = ops.AutocorrectDefault
	AutocorrectYes     = ops.AutocorrectYes
	AutocorrectNo      = ops.AutocorrectNo
)

// TextFieldProps configures a TextField.
type TextFieldProps struct {
	// Value is the field's text. The widget owns its text: typing writes
	// Value; writing Value from code replaces the text.
	Value          *signal.Signal[string]
	Placeholder    signal.Accessor[string]
	Keyboard       KeyboardType
	ReturnKey      ReturnKey
	Secure         bool
	Autocapitalize Autocapitalize
	Autocorrect    Autocorrect
	ContentType    string // AutoFill hint: "username", "password", "email", ...
	// Multiline makes it lines of text (a message, a comment): return is
	// a new line, not OnSubmit, and it grows with its text up to Style's
	// Lines lines (0: no limit), then scrolls.
	Multiline bool
	OnSubmit  func()
	OnFocus   func()
	OnBlur    func()
	Disabled  signal.Accessor[bool]
	Style     Styler
}

// TextField is a text input: a line, or lines with Multiline.
func TextField(p TextFieldProps) Node {
	e := newElement(ops.KindTextField, true)
	e.measure()
	b := e.r.batch
	id := e.id
	if p.Multiline {
		b.SetBool(id, ops.PropTextFieldMultiline, true)
	}
	e.setStyle(p.Style, true)
	if p.Keyboard != KeyboardDefault {
		b.SetEnum(id, ops.PropTextFieldKeyboard, int32(p.Keyboard))
	}
	if p.ReturnKey != ReturnDefault {
		b.SetEnum(id, ops.PropTextFieldReturnKey, int32(p.ReturnKey))
	}
	if p.Secure {
		b.SetBool(id, ops.PropTextFieldSecure, true)
	}
	if p.Autocapitalize != CapitalizeSentences {
		b.SetEnum(id, ops.PropTextFieldAutocapitalize, int32(p.Autocapitalize))
	}
	if p.Autocorrect != AutocorrectDefault {
		b.SetEnum(id, ops.PropTextFieldAutocorrect, int32(p.Autocorrect))
	}
	if p.ContentType != "" {
		b.SetString(id, ops.PropTextFieldContentType, p.ContentType)
	}
	bind(p.Placeholder, func(v string) { e.r.batch.SetString(id, ops.PropTextFieldPlaceholder, v) })
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(id, ops.PropDisabled, v) })
	h := e.handlers()
	if p.Value != nil {
		// The core remembers what the widget last reported and sends the
		// value only when the signal differs from it, so typing never
		// round-trips and IME composition is never disturbed.
		reported, known := "", false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetString(id, ops.PropTextFieldValue, v)
			if p.Multiline {
				e.remeasure()
			}
		})
		h.text = func(s string) {
			reported, known = s, true
			p.Value.Set(s)
			if p.Multiline {
				e.remeasure() // its height is its text's
			}
		}
		e.listen(ops.EventTextChanged)
	} else if p.Multiline {
		h.text = func(string) { e.remeasure() }
		e.listen(ops.EventTextChanged)
	}
	if p.OnSubmit != nil {
		h.submit = p.OnSubmit
		e.listen(ops.EventSubmit)
	}
	if p.OnFocus != nil {
		h.focus = p.OnFocus
		e.listen(ops.EventFocus)
	}
	if p.OnBlur != nil {
		h.blur = p.OnBlur
		e.listen(ops.EventBlur)
	}
	return Node{e}
}

// Focus asks a TextField for the keyboard.
func Focus(n Node) {
	if n.e != nil && n.e.real {
		n.e.r.batch.Command(n.e.id, ops.CmdFocus, nil)
	}
}

// Blur dismisses a TextField's keyboard.
func Blur(n Node) {
	if n.e != nil && n.e.real {
		n.e.r.batch.Command(n.e.id, ops.CmdBlur, nil)
	}
}

// ToggleProps configures a Toggle.
type ToggleProps struct {
	Value    *signal.Signal[bool]
	Disabled signal.Accessor[bool]
	Style    Styler
}

// Toggle is a switch.
func Toggle(p ToggleProps) Node {
	e := newElement(ops.KindToggle, true)
	e.measure()
	e.setStyle(p.Style, false)
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Value != nil {
		reported, known := false, false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetBool(e.id, ops.PropToggleOn, v)
		})
		e.handlers().toggled = func(v bool) {
			reported, known = v, true
			p.Value.Set(v)
		}
		e.listen(ops.EventToggled)
	}
	return Node{e}
}

// SliderProps configures a Slider.
type SliderProps struct {
	Value    *signal.Signal[float64]
	Min, Max float64 // 0 and 0: 0 to 1
	Disabled signal.Accessor[bool]
	Style    Styler
}

// Slider picks a value in a range.
func Slider(p SliderProps) Node {
	e := newElement(ops.KindSlider, true)
	e.measure()
	e.setStyle(p.Style, false)
	lo, hi := p.Min, p.Max
	if lo == 0 && hi == 0 {
		hi = 1
	}
	e.r.batch.SetFloat(e.id, ops.PropSliderMin, lo)
	e.r.batch.SetFloat(e.id, ops.PropSliderMax, hi)
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Value != nil {
		reported, known := 0.0, false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetFloat(e.id, ops.PropSliderValue, v)
		})
		e.handlers().value = func(v float64) {
			reported, known = v, true
			p.Value.Set(v)
		}
		e.listen(ops.EventValueChanged)
	}
	return Node{e}
}

// Symbol is a symbol: an SF Symbol on iOS, the Material Symbol for it on
// Android (the Icon constants and more; see Icon). It's sized as text of
// the Style's Font is (a text style, so it follows the user's text
// size) and colored with its Color (the label color by default).
func Symbol(name Icon, style ...Styler) Node {
	e := newElement(ops.KindSymbol, true)
	e.measure()
	e.text = string(name)
	e.setStyle(firstStyle(style), true)
	e.r.batch.SetString(e.id, ops.PropSymbolName, string(name))
	return Node{e}
}

// SegmentedProps configures a Segmented control.
type SegmentedProps struct {
	Segments []string
	// Selected is the selected segment's index: the user's choice writes
	// it.
	Selected *signal.Signal[int]
	Disabled signal.Accessor[bool]
	Style    Styler
}

// Segmented is one of a few options side by side: UIKit's segmented
// control, Material's segmented buttons.
func Segmented(p SegmentedProps) Node {
	e := newElement(ops.KindSegmented, true)
	e.measure()
	e.setStyle(p.Style, false)
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(p.Segments)))
	for _, s := range p.Segments {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	e.r.batch.SetBytes(e.id, ops.PropSegmentedSegments, b)
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Selected != nil {
		reported, known := 0, false
		signal.RenderEffect(func() {
			v := p.Selected.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetInt(e.id, ops.PropSegmentedSelected, int64(v))
		})
		e.handlers().selected = func(i int64) {
			reported, known = int(i), true
			p.Selected.Set(int(i))
		}
		e.listen(ops.EventSelected)
	}
	return Node{e}
}

// Progress is a progress bar: value is done, from 0 to 1; a nil value is
// under way, how far unknown. Its Style's Color is its fill (the accent
// color by default).
func Progress(value signal.Accessor[float64], style ...Styler) Node {
	e := newElement(ops.KindProgress, true)
	e.measure()
	e.setStyle(firstStyle(style), true)
	if value == nil {
		e.r.batch.SetBool(e.id, ops.PropProgressIndeterminate, true)
		return Node{e}
	}
	bind(value, func(v float64) { e.r.batch.SetFloat(e.id, ops.PropProgressValue, min(max(v, 0), 1)) })
	return Node{e}
}

// DateMode is what a DatePicker picks.
type DateMode = ops.DateMode

const (
	DateOnly    = ops.DateModeDate
	TimeOnly    = ops.DateModeTime
	DateAndTime = ops.DateModeDateTime
)

// DatePickerProps configures a DatePicker.
type DatePickerProps struct {
	// Value is the chosen instant: the user's choice writes it. Dates and
	// times are shown in the device's time zone.
	Value *signal.Signal[time.Time]
	Mode  DateMode
	// Min and Max bound the choice (zero: no bound).
	Min, Max time.Time
	Disabled signal.Accessor[bool]
	Style    Styler
}

// DatePicker picks a date, a time or both, the platform's way: UIKit's
// compact date picker (its popover calendar and wheels); on Android
// buttons showing the date and time that open Material's pickers.
func DatePicker(p DatePickerProps) Node {
	e := newElement(ops.KindDatePicker, true)
	e.measure()
	e.setStyle(p.Style, false)
	b := e.r.batch
	if p.Mode != DateOnly {
		b.SetEnum(e.id, ops.PropDatePickerMode, int32(p.Mode))
	}
	if !p.Min.IsZero() {
		b.SetInt(e.id, ops.PropDatePickerMin, p.Min.UnixMilli())
	}
	if !p.Max.IsZero() {
		b.SetInt(e.id, ops.PropDatePickerMax, p.Max.UnixMilli())
	}
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Value != nil {
		var reported int64
		known := false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if v.IsZero() {
				v = time.Now()
			}
			ms := v.UnixMilli()
			if known && ms == reported {
				return
			}
			reported, known = ms, true
			e.r.batch.SetInt(e.id, ops.PropDatePickerValue, ms)
			e.remeasure() // its text is the date's
		})
		e.handlers().value = func(f float64) {
			ms := int64(f)
			reported, known = ms, true
			p.Value.Set(time.UnixMilli(ms))
			e.remeasure()
		}
		e.listen(ops.EventValueChanged)
	}
	return Node{e}
}

// Activity is a spinner.
func Activity(style ...Styler) Node {
	e := newElement(ops.KindActivity, true)
	e.measure()
	e.setStyle(firstStyle(style), false)
	e.r.batch.SetBool(e.id, ops.PropActivityAnimating, true)
	return Node{e}
}

// ScrollProps configures a Scroll.
type ScrollProps struct {
	Style        Styler // the scroll view's own box
	ContentStyle Styler // the box its children are stacked in
	Horizontal   bool
	// OnRefresh enables pull to refresh (UIRefreshControl,
	// SwipeRefreshLayout). Set Refreshing true while refreshing; the
	// spinner hides when it's false.
	OnRefresh  func()
	Refreshing signal.Accessor[bool]
	// OnScroll is called as it scrolls, with its content offset: points
	// from its content's top left.
	OnScroll func(x, y float32)
}

// Scroll scrolls its children along one axis. Give it a size (or Grow)
// so it has something to scroll within.
func Scroll(p ScrollProps, children ...Node) Node {
	e := newElement(ops.KindScroll, true)
	e.adjust = func(ls *layout.Style) {
		ls.FlexDirection = layout.Column
		if p.Horizontal {
			ls.FlexDirection = layout.Row
			ls.OverflowX = layout.OverflowScroll
		} else {
			ls.OverflowY = layout.OverflowScroll
		}
		ls.AlignItems = layout.AlignStretch
	}
	e.setStyle(p.Style, false)
	if p.Horizontal {
		e.r.batch.SetBool(e.id, ops.PropScrollHorizontal, true)
	}
	dir := ColumnDir
	if p.Horizontal {
		dir = RowDir
	}
	refreshable(e, p.OnRefresh, p.Refreshing, ops.PropScrollRefreshable, ops.PropScrollRefreshing)
	onScroll(e, p.OnScroll)
	content := newElement(ops.KindView, true)
	content.adjust = func(cs *layout.Style) { cs.FlexShrink = 0 } // the content keeps its size; that's the point
	content.setStyle(withDirection(p.ContentStyle, dir), false)
	addKids(content, children)
	e.appendKid(content)
	return Node{e}
}

// WheelProps configures a Wheel.
type WheelProps struct {
	Items []string
	// Selected is the selected item's index: the user's choice writes it.
	Selected *signal.Signal[int]
	Disabled signal.Accessor[bool]
	Style    Styler
}

// Wheel picks one of many options on a spinning wheel (SwiftUI's
// Picker(.wheel): UIPickerView; Android's NumberPicker).
func Wheel(p WheelProps) Node {
	e := newElement(ops.KindWheel, true)
	e.measure()
	e.setStyle(p.Style, false)
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(p.Items)))
	for _, s := range p.Items {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	e.r.batch.SetBytes(e.id, ops.PropWheelItems, b)
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Selected != nil {
		reported, known := 0, false
		signal.RenderEffect(func() {
			v := p.Selected.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetInt(e.id, ops.PropWheelSelected, int64(v))
		})
		e.handlers().selected = func(i int64) {
			reported, known = int(i), true
			p.Selected.Set(int(i))
		}
		e.listen(ops.EventSelected)
	}
	return Node{e}
}

// StepperProps configures a Stepper.
type StepperProps struct {
	// Value is the value: the user's steps write it.
	Value    *signal.Signal[float64]
	Min, Max float64 // 0 and 0: 0 to 100
	Step     float64 // 0: 1
	Disabled signal.Accessor[bool]
	Style    Styler
}

// Stepper steps a value with minus and plus buttons (SwiftUI's Stepper:
// UIStepper; Android has none, so two Material icon buttons). Show the
// value beside it: it has no label of its own.
func Stepper(p StepperProps) Node {
	e := newElement(ops.KindStepper, true)
	e.measure()
	e.setStyle(p.Style, false)
	lo, hi, step := p.Min, p.Max, p.Step
	if lo == 0 && hi == 0 {
		hi = 100
	}
	if step <= 0 {
		step = 1
	}
	b := e.r.batch
	b.SetFloat(e.id, ops.PropStepperMin, lo)
	b.SetFloat(e.id, ops.PropStepperMax, hi)
	b.SetFloat(e.id, ops.PropStepperStep, step)
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Value != nil {
		var reported float64
		known := false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetFloat(e.id, ops.PropStepperValue, v)
		})
		e.handlers().value = func(v float64) {
			reported, known = v, true
			p.Value.Set(v)
		}
		e.listen(ops.EventValueChanged)
	}
	return Node{e}
}

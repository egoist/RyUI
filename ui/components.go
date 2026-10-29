package ui

import (
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
	// Accessibility. A view with any of these is an accessibility
	// element; plain containers never appear in the accessibility tree.
	Label string
	Hint  string
	Role  Role
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
	real := p.OnPress != nil || p.OnLongPress != nil || p.Label != "" || p.Hint != "" || p.Role != RoleNone ||
		p.OnPan != nil || p.OnPinch != nil || p.OnSwipe != nil
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
	Menu     []Action
	Variant  ButtonVariant
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
	bind(p.Title, func(v string) {
		e.text = v
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
	OnSubmit       func()
	OnFocus        func()
	OnBlur         func()
	Disabled       signal.Accessor[bool]
	Style          Styler
}

// TextField is a single-line text input.
func TextField(p TextFieldProps) Node {
	e := newElement(ops.KindTextField, true)
	e.measure()
	e.setStyle(p.Style, true)
	b := e.r.batch
	id := e.id
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
		})
		h.text = func(s string) {
			reported, known = s, true
			p.Value.Set(s)
		}
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
}

// Scroll scrolls its children along one axis. Give it a size (or Grow)
// so it has something to scroll within.
func Scroll(p ScrollProps, children ...Node) Node {
	e := newElement(ops.KindScroll, true)
	e.setStyle(p.Style, false)
	ls := e.box.Style
	ls.FlexDirection = layout.Column
	if p.Horizontal {
		ls.FlexDirection = layout.Row
		ls.OverflowX = layout.OverflowScroll
		e.r.batch.SetBool(e.id, ops.PropScrollHorizontal, true)
	} else {
		ls.OverflowY = layout.OverflowScroll
	}
	ls.AlignItems = layout.AlignStretch
	e.box.SetStyle(ls)
	dir := ColumnDir
	if p.Horizontal {
		dir = RowDir
	}
	refreshable(e, p.OnRefresh, p.Refreshing, ops.PropScrollRefreshable, ops.PropScrollRefreshing)
	content := newElement(ops.KindView, true)
	content.setStyle(withDirection(p.ContentStyle, dir), false)
	cs := content.box.Style
	cs.FlexShrink = 0 // the content keeps its size; that's the point
	content.box.SetStyle(cs)
	addKids(content, children)
	e.appendKid(content)
	return Node{e}
}

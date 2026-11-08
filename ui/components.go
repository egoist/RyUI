package ui

import (
	"bytes"
	"encoding/binary"
	"time"

	"github.com/egoist/ryui/host"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
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
	Label signal.Accessor[string]
	Hint  signal.Accessor[string]
	Role  Role
	// Decorative hides the view, and everything in it, from screen
	// readers: a decoration, or what its neighbors already say.
	Decorative bool
	// Tooltip is a short text the platform shows by the view while a
	// pointer rests on it (an iPad's pointer, a mouse), and on Android
	// as it's long pressed (when it has no OnLongPress): what an icon
	// does ("Add to favorites"). Touch-only users never see it, so it
	// can't be the only place something is said.
	Tooltip signal.Accessor[string]
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

	// Interaction states. A style follows them most simply through its
	// variants (Style.Pressed, Hovered, Dragging, DragOver); other views'
	// styles, and anything else, through Interaction; and what isn't a
	// style through the handlers. Each is reported once per change, and
	// leaving it when the view goes away while in it. A view that
	// reports its pressed state draws its own: the platform's highlight
	// (Android's ripple, Windows' hover and pressed fills) is left off.
	//
	// Interaction is the view's states as values, for what follows them:
	// its children's styles (a group's hover), its siblings' (a peer's),
	// a Text. See Interaction.
	Interaction *Interaction
	// OnPressedChange reports the view pressed (CSS's :active): true as
	// a finger, or the mouse's primary button, goes down on it (a moment
	// later in a scrolling container, as the platform's own views do, so
	// a scroll doesn't flash it); false as it lifts, moves off the view
	// (back on, it's pressed again), or a scroll, another gesture, a long
	// press or a drag takes the touch over. A press held, with no
	// OnPress, is a hold (to record, say).
	OnPressedChange func(pressed bool)
	// OnHoverChange reports a pointer over the view (CSS's :hover): a
	// mouse, a trackpad, an iPad's pointer, a hovering Apple Pencil or
	// stylus. It stays over while on the view's children. A touch
	// screen alone never hovers.
	OnHoverChange func(hovered bool)

	// Drag makes the view draggable, within the app and to other apps.
	Drag *DragSource
	// Drop makes the view take drops (CSS's drag-over is its
	// OnOverChange).
	Drop *DropTarget
}

// A11yAction is a screen reader's action on a view.
type A11yAction struct {
	Name signal.Accessor[string]
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

// hasText reports whether a says anything: an accessor that can, or a constant
// that isn't empty. A label left out isn't a label of nothing.
func hasText(a signal.Accessor[string]) bool {
	if a == nil {
		return false
	}
	if v, ok := signal.StaticValue(a); ok {
		return v != ""
	}
	return true
}

// bindStrings sends a, encoded as a list of strings, and again when it
// changes.
func bindStrings(a signal.Accessor[[]string], apply func([]byte)) {
	encode := func(list []string) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(len(list)))
		for _, s := range list {
			b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
			b = append(b, s...)
		}
		return b
	}
	if a == nil {
		apply(encode(nil))
		return
	}
	if v, ok := signal.StaticValue(a); ok {
		apply(encode(v))
		return
	}
	var last []byte
	signal.RenderEffect(func() {
		if b := encode(a.Get()); !bytes.Equal(b, last) {
			last = b
			apply(b)
		}
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
	real := p.OnPress != nil || p.OnLongPress != nil || hasText(p.Label) || hasText(p.Hint) || hasText(p.Tooltip) || p.Role != RoleNone || p.Decorative || p.OnLayout != nil ||
		len(p.Actions) > 0 || p.OnAdjust != nil ||
		p.OnPan != nil || p.OnPinch != nil || p.OnSwipe != nil || p.OnRotate != nil || p.interactive()
	if s, ok := p.Style.(Style); ok && (s.visual() || s.hasVariants()) {
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
		if hasText(p.Label) {
			bind(p.Label, func(v string) { e.r.batch.SetString(e.id, ops.PropA11yLabel, v) })
		}
		if hasText(p.Hint) {
			bind(p.Hint, func(v string) { e.r.batch.SetString(e.id, ops.PropA11yHint, v) })
		}
		if hasText(p.Tooltip) {
			bind(p.Tooltip, func(v string) { e.r.batch.SetString(e.id, ops.PropTooltip, v) })
		}
		if p.Role != RoleNone {
			e.r.batch.SetEnum(e.id, ops.PropA11yRole, int32(p.Role))
		}
		if p.Decorative {
			e.r.batch.SetBool(e.id, ops.PropA11yHidden, true)
		}
		h.layout = p.OnLayout
		if len(p.Actions) > 0 {
			for _, a := range p.Actions {
				h.a11yActions = append(h.a11yActions, a.Do)
			}
			names := make([]signal.Accessor[string], len(p.Actions))
			for i, a := range p.Actions {
				names[i] = a.Name
			}
			bindStrings(signal.Func(func() []string {
				out := make([]string, len(names))
				for i, n := range names {
					if n != nil {
						out[i] = n.Get()
					}
				}
				return out
			}), func(b []byte) { e.r.batch.SetBytes(e.id, ops.PropA11yActions, b) })
			e.listen(ops.EventA11yAction)
		}
		if p.OnAdjust != nil {
			h.adjust = p.OnAdjust
			e.r.batch.SetBool(e.id, ops.PropA11yAdjustable, true)
			e.listen(ops.EventAdjust)
		}
		e.listenGestures(&p)
		e.listenInteraction(&p)
	}
	e.setStyle(e.variantStyle(p.Style, p.Interaction), false)
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

// Platform is the system the app runs on, the environment's Platform:
// for the few places an app follows each platform's own design (a
// section's heading, a card's corners) where RyUI has no view that does
// it for it.
type Platform = ops.Platform

const (
	PlatformIOS     = ops.PlatformIOS
	PlatformAndroid = ops.PlatformAndroid
	PlatformMacOS   = ops.PlatformMacOS
	PlatformWindows = ops.PlatformWindows
)

// SizeClass is how much room the window gives the app along one axis, as
// the platform classes it (the environment's WidthClass and
// HeightClass): lay out side by side what fits, a sidebar beside the
// content or a list beside its detail, when the width isn't compact.
// iOS reports UIKit's size classes, compact or regular (as Expanded);
// Android, Material's window size classes, which have Medium between
// (600 to 840 dp wide), where Material still shows one pane. macOS and
// Windows have none of their own: theirs are Material's, from the size.
type SizeClass = ops.SizeClass

const (
	SizeClassCompact  = ops.SizeClassCompact
	SizeClassMedium   = ops.SizeClassMedium
	SizeClassExpanded = ops.SizeClassExpanded
)

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
		pad := Edges{Top: env.SafeTop, Right: env.SafeRight, Bottom: max(env.SafeBottom, env.Keyboard), Left: env.SafeLeft}
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
	// ButtonGlass is Liquid Glass (iOS 26's glass configuration): a
	// capsule, a circle around an icon alone. Before iOS 26, and on the
	// other platforms, it's Gray's look (Material's tonal button).
	ButtonGlass = ops.ButtonVariantGlass
	// ButtonGlassProminent is tinted Liquid Glass, for the primary
	// action (prominentGlass); elsewhere Filled's look.
	ButtonGlassProminent = ops.ButtonVariantGlassProminent
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
	Label signal.Accessor[string]
	// Tooltip is shown while a pointer rests on the button (and as it's
	// long pressed on Android): an icon-only button's name, mostly.
	Tooltip  signal.Accessor[string]
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
	if hasText(p.Label) {
		bind(p.Label, func(v string) { e.r.batch.SetString(e.id, ops.PropA11yLabel, v) })
	}
	if hasText(p.Tooltip) {
		bind(p.Tooltip, func(v string) { e.r.batch.SetString(e.id, ops.PropTooltip, v) })
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
		bindActions(menu, func(b []byte) { e.r.batch.SetBytes(e.id, ops.PropMenu, b) })
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
	// Plain draws the field without its border, background and inset:
	// its text alone (and placeholder), for a field in a card or a form
	// of the app's own, as Notes' and Reminders' are.
	Plain    bool
	OnSubmit func()
	OnFocus  func()
	OnBlur   func()
	Disabled signal.Accessor[bool]
	Style    Styler
	// Selection is the cursor (Start == End) or the selected text, as
	// byte offsets into Value's text. The user's moves (and typing)
	// write it; writing it moves the cursor or the selection, as a
	// mention picker or a formatting button does. It needs Value.
	Selection *signal.Signal[TextRange]
	// PasteTypes are the MIME types (patterns, as "image/*") of content
	// OnPaste takes: pasted, or put in by the keyboard (a GIF keyboard's
	// images, stickers), it goes to OnPaste instead of into the text,
	// and the edit menu offers Paste for it. Text that isn't of these
	// types pastes as usual.
	PasteTypes []string
	// OnPaste is called with what was pasted of PasteTypes: images and
	// files are the app's own copies, in its caches, as a share's are.
	OnPaste func(items []SharedItem)
	// Rich makes it a text area of styled text (a mail's body, a
	// formatted message): bold, italic, underline, strikethrough,
	// monospaced code, links (and colors, kept as they are). The user's
	// typing and formatting write it (iOS's B/I/U edit menu; ToggleFormat
	// and SetLink from a toolbar); writing it replaces the text. Value,
	// if given too, follows its plain text. Inline components aren't
	// editable and are left out. It makes the field Multiline.
	Rich *signal.Signal[Rich]
	// OnFormats is called with the styles where the cursor is, or that
	// all the selection has, as they change: a toolbar's buttons show
	// them.
	OnFormats func(TextFormats)
	// Accessory is a bar shown on top of the keyboard while the field is
	// focused: a character count and Done, formatting buttons, quick
	// replies (UIKit's inputAccessoryView, SwiftUI's keyboard toolbar). It's
	// as wide as the window and as tall as its content, moves with the
	// keyboard (on iOS with its interactive dismissal too), and is built
	// with the field and disposed with it. Desktops, with no on-screen
	// keyboard, don't show it.
	Accessory func() Node
}

// TextRange is a span of a TextField's text, as byte offsets into it
// (text[Start:End]); Start == End is the cursor.
type TextRange struct{ Start, End int }

// InsertText puts text into a TextField as if it were typed: in place of
// the selection, or at the cursor, undoably (an emoji picker's, a
// mention's). The field reports the change as typing does.
func InsertText(field Node, text string) {
	if e := field.e; e != nil && e.kind == ops.KindTextField {
		e.r.batch.Command(e.id, ops.CmdInsertText, []byte(text))
	}
}

// utf16Offset is the UTF-16 offset of byte offset b in s (as UIKit and
// Android count), b clamped to s and moved back to a character's start.
func utf16Offset(s string, b int) int {
	n := 0
	for i, r := range s {
		if i >= b {
			break
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// byteOffset is the byte offset in s of UTF-16 offset u (clamped).
func byteOffset(s string, u int) int {
	n := 0
	for i, r := range s {
		if n >= u {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(s)
}

// TextField is a text input: a line, or lines with Multiline.
func TextField(p TextFieldProps) Node {
	if p.Rich != nil {
		p.Multiline = true
	}
	e := newElement(ops.KindTextField, true)
	e.measure()
	b := e.r.batch
	id := e.id
	if p.Multiline {
		b.SetBool(id, ops.PropTextFieldMultiline, true)
	}
	if p.Plain {
		b.SetBool(id, ops.PropTextFieldPlain, true)
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
	var richSent func(string)
	if p.Value != nil {
		// The core remembers what the widget last reported and sends the
		// value only when the signal differs from it, so typing never
		// round-trips and IME composition is never disturbed.
		reported, known := "", false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if known && v == reported || p.Rich != nil {
				// A Rich field's text is Rich's: Value follows it.
				return
			}
			reported, known = v, true
			e.r.batch.SetString(id, ops.PropTextFieldValue, v)
			if p.Multiline {
				e.remeasure()
			}
		})
		if p.Rich != nil {
			// What's sent as Rich is the field's text: Value says it too.
			richSent = func(s string) {
				reported, known = s, true
				p.Value.Set(s)
			}
		}
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
	if p.Rich != nil {
		richField(e, p.Rich, richSent)
	}
	if p.OnFormats != nil {
		onFormats := p.OnFormats
		h.formats = func(b []byte) { onFormats(decodeFormats(b)) }
		e.listen(ops.EventFormats)
	}
	if p.Selection != nil && p.Value != nil {
		textSelection(e, p.Value, p.Selection)
	}
	if len(p.PasteTypes) > 0 && p.OnPaste != nil {
		b := binary.LittleEndian.AppendUint32(nil, uint32(len(p.PasteTypes)))
		for _, t := range p.PasteTypes {
			b = binary.LittleEndian.AppendUint32(b, uint32(len(t)))
			b = append(b, t...)
		}
		e.r.batch.SetBytes(id, ops.PropTextFieldPasteTypes, b)
		onPaste := p.OnPaste
		h.pasted = func(b []byte) {
			if items := decodeShared(b); len(items) > 0 {
				onPaste(items)
			}
		}
		e.listen(ops.EventPasted)
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
	if p.Accessory != nil {
		accessory(e, p.Accessory) // after Multiline, which can make the field's view again
	}
	return Node{e}
}

// textSelection keeps a field's selection and sel in step: the user's
// moves write sel, and sel's changes move the field's, except to where
// the field last said it was (so a move never round-trips). Offsets are
// UTF-16 on the wire, bytes of the text in Go; the host reports a move
// after the text change that made it.
func textSelection(e *element, value *signal.Signal[string], sel *signal.Signal[TextRange]) {
	id := e.id
	var reported [2]uint32
	known := false
	signal.RenderEffect(func() {
		r := sel.Get()
		text := signal.Untrack(value.Get)
		start, end := min(max(r.Start, 0), len(text)), min(max(r.End, 0), len(text))
		if end < start {
			start, end = end, start
		}
		u := [2]uint32{uint32(utf16Offset(text, start)), uint32(utf16Offset(text, end))}
		if known && u == reported {
			return
		}
		reported, known = u, true
		var b [8]byte
		binary.LittleEndian.PutUint32(b[:], u[0])
		binary.LittleEndian.PutUint32(b[4:], u[1])
		e.r.batch.SetBytes(id, ops.PropTextFieldSelection, b[:])
	})
	e.handlers().selection = func(b []byte) {
		if len(b) < 8 {
			return
		}
		u := [2]uint32{binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint32(b[4:])}
		reported, known = u, true
		text := value.Peek()
		sel.Set(TextRange{byteOffset(text, int(u[0])), byteOffset(text, int(u[1]))})
	}
	e.listen(ops.EventSelectionChanged)
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
	// Segments are what they say; they can follow the app's language.
	Segments signal.Accessor[[]string]
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
	first := true
	bindStrings(p.Segments, func(b []byte) {
		e.r.batch.SetBytes(e.id, ops.PropSegmentedSegments, b)
		if !first {
			e.remeasure()
		}
		first = false
	})
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
	// Inline shows iOS's calendar and time in place, for a sheet of its
	// own, rather than behind compact buttons. Android keeps its buttons,
	// which open Material's pickers.
	Inline   bool
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
	if p.Inline {
		b.SetBool(e.id, ops.PropDatePickerInline, true)
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
	// Paging stops it only at whole pages: a carousel, an onboarding's
	// steps, a feed of full-screen videos (iOS's isPagingEnabled; on
	// Android a swipe snaps to the next page, or back). Each child of its
	// content is a page, made exactly as big as the scroll view along its
	// axis and as its content across (ContentStyle's direction, wrapping
	// and gaps don't apply). Its scroll bars hide: a PageControl says
	// where it is.
	Paging bool
	// Page, with Paging, is the page shown, from the first in the
	// reading direction (right to left, the rightmost): the user's swipes
	// write it as they settle, and code's writes scroll there, animated.
	// When the scroll view's size changes (a rotation), it stays on it.
	Page *signal.Signal[int]
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
			// Right to left, the content is laid out from the right, but it
			// sits at the scroll view's origin, where a platform's scroll
			// view wants it: the host starts at its end, not the layout.
			if e.rtl() {
				ls.Direction = layout.DirLTR
			}
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
	content.adjust = func(cs *layout.Style) {
		cs.FlexShrink = 0 // the content keeps its size; that's the point
		if p.Horizontal && content.rtl() {
			cs.Direction = layout.DirRTL
			if cs.MinWidth.IsAuto() {
				cs.MinWidth = layout.Pct(100) // short content hugs the right
			}
		}
	}
	content.setStyle(withDirection(p.ContentStyle, dir), false)
	addKids(content, children)
	e.appendKid(content)
	if p.Paging {
		paging(e, content, p.Horizontal, p.Page)
	}
	return Node{e}
}

// paging makes a Scroll stop at whole pages, and keeps page and the
// page it shows in step.
func paging(e, content *element, horizontal bool, page *signal.Signal[int]) {
	e.r.batch.SetBool(e.id, ops.PropScrollPaging, true)
	// Each of its content's children is a page, exactly as big as the
	// scroll view along its axis: a track each of a grid that flows along
	// it, made again when the scroll view's size changes.
	pageSize := float32(0)
	adjust := content.adjust
	content.adjust = func(cs *layout.Style) {
		adjust(cs)
		if pageSize <= 0 {
			return
		}
		cs.Display = layout.DisplayGrid
		if horizontal {
			cs.GridAutoFlow = layout.FlowColumn
			cs.GridAutoColumns = []layout.TrackSize{layout.Px(pageSize)}
		} else {
			cs.GridAutoFlow = layout.FlowRow
			cs.GridAutoRows = []layout.TrackSize{layout.Px(pageSize)}
		}
	}
	along := func(w, h float32) float32 {
		if horizontal {
			return w
		}
		return h
	}
	size := signal.New(float32(0))
	e.handlers().layout = func(f Frame) { size.Set(along(f.Width, f.Height)) }
	signal.RenderEffect(func() {
		if s := size.Get(); s != pageSize {
			pageSize = s
			content.box.SetStyle(content.layoutStyle(&content.style))
		}
	})
	if page == nil {
		return
	}
	// logical turns a page from the physical start (left, top) into one
	// from the reading start, and back: right to left, horizontally, the
	// first is the rightmost.
	logical := func(i int) int {
		if !horizontal || !content.rtl() {
			return i
		}
		return max(len(content.box.Children())-1-i, 0)
	}
	shown, laid := -1, float32(0) // the page the host shows, and the size it was laid out at
	signal.RenderEffect(func() {
		pg, sz := page.Get(), size.Get()
		if sz <= 0 || (pg == shown && sz == laid) {
			return
		}
		animated := laid == sz && shown >= 0 // a size change keeps the page, at once
		shown, laid = pg, sz
		off := float32(logical(pg)) * sz
		x, y := off, float32(0)
		if !horizontal {
			x, y = 0, off
		}
		ScrollTo(Node{e}, x, y, animated)
	})
	e.handlers().paged = func(i int64) {
		pg := logical(int(i))
		shown = pg
		page.Set(pg)
	}
	e.listen(ops.EventPaged)
}

// PageControlProps configures a PageControl.
type PageControlProps struct {
	// Count is how many pages there are.
	Count signal.Accessor[int]
	// Page is the current page: tapping a dot (or the control's sides on
	// iOS), or a screen reader's adjusting, writes it. Share it with a
	// Paging Scroll's Page to move both.
	Page  *signal.Signal[int]
	Style Styler
}

// PageControl is a row of dots, one per page, with the current one
// highlighted: UIPageControl on iOS, which has its own Liquid Glass look
// on iOS 26. Android, macOS and Windows have no such control, and get the
// same dots, drawn in their theme's colors. VoiceOver and TalkBack say
// "page 2 of 5", and adjust it.
func PageControl(p PageControlProps) Node {
	e := newElement(ops.KindPageControl, true)
	e.measure()
	e.setStyle(p.Style, false)
	bind(p.Count, func(n int) {
		e.r.batch.SetInt(e.id, ops.PropPageControlCount, int64(n))
		e.remeasure()
	})
	if p.Page != nil {
		reported, known := 0, false
		signal.RenderEffect(func() {
			v := p.Page.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetInt(e.id, ops.PropPageControlPage, int64(v))
		})
		e.handlers().selected = func(i int64) {
			reported, known = int(i), true
			p.Page.Set(int(i))
		}
		e.listen(ops.EventSelected)
	}
	return Node{e}
}

// WheelProps configures a Wheel.
type WheelProps struct {
	// Items are what they say; they can follow the app's language.
	Items signal.Accessor[[]string]
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
	first := true
	bindStrings(p.Items, func(b []byte) {
		e.r.batch.SetBytes(e.id, ops.PropWheelItems, b)
		if !first {
			e.remeasure()
		}
		first = false
	})
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

// ColorPickerProps configures a ColorPicker.
type ColorPickerProps struct {
	// Value is the color: the user's choice writes it (an RGBA color).
	Value *signal.Signal[Color]
	// Alpha lets the user choose an opacity too.
	Alpha bool
	// Title is the picker's (default: the platform's).
	Title    signal.Accessor[string]
	Disabled signal.Accessor[bool]
	Style    Styler
}

// ColorPicker is a swatch of a color that opens the platform's picker
// (SwiftUI's ColorPicker: UIColorWell and the system's color picker;
// Android has none, so a Material dialog of the palette Google's apps
// offer, with an opacity slider for Alpha). Show its label beside it.
func ColorPicker(p ColorPickerProps) Node {
	e := newElement(ops.KindColorPicker, true)
	e.measure()
	e.setStyle(p.Style, false)
	b := e.r.batch
	if p.Alpha {
		b.SetBool(e.id, ops.PropColorPickerAlpha, true)
	}
	bind(p.Title, func(v string) { e.r.batch.SetString(e.id, ops.PropColorPickerTitle, v) })
	bind(p.Disabled, func(v bool) { e.r.batch.SetBool(e.id, ops.PropDisabled, v) })
	if p.Value != nil {
		var reported Color
		known := false
		signal.RenderEffect(func() {
			v := p.Value.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetColor(e.id, ops.PropColorPickerValue, v)
		})
		e.handlers().color = func(c Color) {
			reported, known = c, true
			p.Value.Set(c)
		}
		e.listen(ops.EventColorChanged)
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

// accessory builds a field's bar over the keyboard: a layout root of its
// own, as wide as the root and as tall as its content, in the root's
// environment.
func accessory(field *element, build func() Node) {
	r := field.r
	a := newElement(ops.KindAccessory, true)
	a.hostSized, a.fits, a.fullWidth = true, true, true
	a.env = r.env
	st := layout.DefaultStyle
	st.FlexDirection = layout.Column
	st.Direction = a.layoutDirection()
	a.box.SetStyle(st)
	r.provs = append(r.provs, a)
	// Built under the field's owner, and gone with it.
	if n := build(); n.e != nil {
		a.appendKid(n.e)
	}
	r.batch.SetInt(field.id, ops.PropTextFieldAccessory, int64(a.id))
	signal.OnCleanup(a.destroy)
}

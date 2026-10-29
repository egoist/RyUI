package ui

import (
	"encoding/binary"

	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// ActionRole is how a button of an alert, a menu item or a bar button
// is shown.
type ActionRole = ops.AlertButtonRole

const (
	ActionDefault     = ops.AlertButtonRoleDefault
	ActionCancel      = ops.AlertButtonRoleCancel
	ActionDestructive = ops.AlertButtonRoleDestructive
)

// Action is a button of an alert, a menu item or a bar button.
type Action struct {
	Title string
	Role  ActionRole
	// Icon is a symbol beside the title in a menu; a navigation bar
	// shows it instead of the title (which VoiceOver and TalkBack read).
	Icon    Icon
	OnPress func()
}

func encodeActions(as []Action) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(as)))
	for _, a := range as {
		b = append(b, byte(a.Role))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(a.Title)))
		b = append(b, a.Title...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(a.Icon)))
		b = append(b, a.Icon...)
	}
	return b
}

func runAction(as []Action, i int64) {
	if i >= 0 && int(i) < len(as) && as[i].OnPress != nil {
		as[i].OnPress()
	}
}

// Icon is a symbol (ui.Symbol, tabs, buttons, actions): an SF Symbol
// name. iOS draws any; Android draws the common ones, as the Material
// Symbols for them: the constants here, and more (see
// hosts/android/gen_symbols.go). A name with ".fill" is the filled
// symbol.
type Icon string

const (
	IconHome      Icon = "house"
	IconSearch    Icon = "magnifyingglass"
	IconSettings  Icon = "gearshape"
	IconPerson    Icon = "person"
	IconPeople    Icon = "person.2"
	IconStar      Icon = "star"
	IconHeart     Icon = "heart"
	IconList      Icon = "list.bullet"
	IconMenu      Icon = "line.3.horizontal"
	IconFilter    Icon = "line.3.horizontal.decrease"
	IconMore      Icon = "ellipsis"
	IconPlus      Icon = "plus"
	IconMinus     Icon = "minus"
	IconClose     Icon = "xmark"
	IconCheck     Icon = "checkmark"
	IconBack      Icon = "chevron.left"
	IconForward   Icon = "chevron.right"
	IconUp        Icon = "chevron.up"
	IconDown      Icon = "chevron.down"
	IconRefresh   Icon = "arrow.clockwise"
	IconTrash     Icon = "trash"
	IconEdit      Icon = "pencil"
	IconCompose   Icon = "square.and.pencil"
	IconShare     Icon = "square.and.arrow.up"
	IconDownload  Icon = "square.and.arrow.down"
	IconSend      Icon = "paperplane"
	IconBell      Icon = "bell"
	IconMail      Icon = "envelope"
	IconPhone     Icon = "phone"
	IconMessage   Icon = "message"
	IconCamera    Icon = "camera"
	IconPhoto     Icon = "photo"
	IconMic       Icon = "mic"
	IconSpeaker   Icon = "speaker.wave.2"
	IconPlay      Icon = "play"
	IconPause     Icon = "pause"
	IconCalendar  Icon = "calendar"
	IconClock     Icon = "clock"
	IconLocation  Icon = "location"
	IconPin       Icon = "mappin"
	IconCart      Icon = "cart"
	IconBookmark  Icon = "bookmark"
	IconLock      Icon = "lock"
	IconEye       Icon = "eye"
	IconInfo      Icon = "info.circle"
	IconWarning   Icon = "exclamationmark.triangle"
	IconHelp      Icon = "questionmark.circle"
	IconFolder    Icon = "folder"
	IconDocument  Icon = "doc"
	IconCopy      Icon = "doc.on.doc"
	IconLink      Icon = "link"
	IconAttach    Icon = "paperclip"
	IconGlobe     Icon = "globe"
	IconLightMode Icon = "sun.max"
	IconDarkMode  Icon = "moon"
	IconSignOut   Icon = "rectangle.portrait.and.arrow.right"
)

// Filled returns the icon's filled symbol (name.fill).
func (i Icon) Filled() Icon { return i + ".fill" }

// growByDefault gives a container that fills its space (Stack, Tabs,
// List) Grow 1 unless its style sizes it.
func growByDefault(e *element) {
	e.adjust = func(ls *layout.Style) {
		if ls.Height.IsAuto() && ls.FlexGrow == 0 {
			ls.FlexGrow = 1
		}
	}
	e.box.SetStyle(e.layoutStyle(&e.style))
}

// ScreenOptions configures a screen of a Stack.
type ScreenOptions struct {
	Title signal.Accessor[string]
	// Actions are the navigation bar's buttons (the top app bar's on
	// Android).
	Actions       []Action
	NoBackGesture bool
}

// Navigator pushes and pops the screens of the Stack it came from.
type Navigator struct{ st *stackState }

var navCtx = signal.NewContext[*Navigator](nil)

// UseNavigator returns the navigator of the Stack the calling component
// is in. Call it while building the component.
func UseNavigator() *Navigator {
	n := navCtx.Use()
	if n == nil {
		panic("ui: UseNavigator outside a Stack")
	}
	return n
}

// Push shows screen on top, with the platform's push animation.
func (n *Navigator) Push(screen func() Node, o ScreenOptions) { n.st.push(screen, o) }

// Pop removes the top screen (not the first), disposing it.
func (n *Navigator) Pop() {
	if len(n.st.screens) > 1 {
		n.st.remove(n.st.screens[len(n.st.screens)-1])
	}
}

// Depth returns how many screens the stack holds.
func (n *Navigator) Depth() int { return len(n.st.screens) }

type stackScreen struct {
	el      *element
	dispose func()
	actions []Action
}

type stackState struct {
	e       *element
	nav     *Navigator
	owner   *signal.Owner
	screens []*stackScreen
}

// Stack is a navigation stack (UINavigationController; on Android a
// view stack with a top app bar, transitions and predictive back). It
// starts with root; components in it push more with UseNavigator. Each
// screen is an owner: popping it, by code or by the user's back gesture,
// disposes it.
func Stack(root func() Node, o ScreenOptions, style ...Styler) Node {
	e := newElement(ops.KindStack, true)
	e.setStyle(firstStyle(style), false)
	growByDefault(e)
	st := &stackState{e: e, owner: signal.CurrentOwner()}
	st.nav = &Navigator{st}
	e.handlers().popped = func(id int64) {
		if n := len(st.screens); n > 1 && st.screens[n-1].el.id == ops.NodeID(id) {
			st.remove(st.screens[n-1])
		}
	}
	e.listen(ops.EventPopped)
	st.push(root, o)
	signal.OnCleanup(func() {
		for _, s := range st.screens {
			s.dispose()
		}
	})
	return Node{e}
}

func (st *stackState) push(build func() Node, o ScreenOptions) {
	r := st.e.r
	s := &stackScreen{actions: o.Actions}
	withRoot(r, func() Node {
		s.el = newProvider(ops.KindScreen)
		b := r.batch
		id := s.el.id
		if len(o.Actions) > 0 {
			b.SetBytes(id, ops.PropScreenActions, encodeActions(o.Actions))
			s.el.handlers().action = func(i int64) { runAction(s.actions, i) }
			s.el.listen(ops.EventAction)
		}
		if o.NoBackGesture {
			b.SetBool(id, ops.PropScreenBackGesture, false)
		}
		signal.RunWithOwner(st.owner, func() {
			signal.Root(func(dispose func()) {
				s.dispose = dispose
				bind(o.Title, func(v string) { r.batch.SetString(id, ops.PropScreenTitle, v) })
				navCtx.Provide(st.nav, func() {
					envCtx.Provide(s.el.env, func() {
						if n := build(); n.e != nil {
							fill(n)
							s.el.appendKid(n.e)
						}
					})
				})
			})
		})
		return Node{}
	})
	st.screens = append(st.screens, s)
	st.e.appendKid(s.el) // the host pushes it
}

// remove pops s: its effects stop and its views are destroyed (the host
// animates the pop of the top screen).
func (st *stackState) remove(s *stackScreen) {
	for i, x := range st.screens {
		if x == s {
			st.screens = append(st.screens[:i], st.screens[i+1:]...)
			break
		}
	}
	s.dispose()
	st.e.removeKid(s.el, true)
}

// Tab is a page of Tabs.
type Tab struct {
	Title   string
	Icon    Icon
	Content func() Node
}

// TabsProps configures Tabs.
type TabsProps struct {
	// Selected is the shown tab's index; the user's choice writes it.
	Selected *signal.Signal[int]
	Style    Styler
}

// Tabs shows one page at a time with a tab bar (UITabBarController;
// Material's NavigationBarView on Android).
func Tabs(p TabsProps, tabs ...Tab) Node {
	e := newElement(ops.KindTabs, true)
	e.setStyle(p.Style, false)
	growByDefault(e)
	r := e.r
	owner := signal.CurrentOwner()
	var disposes []func()
	for _, t := range tabs {
		page := newProvider(ops.KindScreen)
		if t.Title != "" {
			r.batch.SetString(page.id, ops.PropScreenTitle, t.Title)
		}
		if t.Icon != "" {
			r.batch.SetString(page.id, ops.PropScreenIcon, string(t.Icon))
		}
		build := t.Content
		signal.RunWithOwner(owner, func() {
			signal.Root(func(dispose func()) {
				disposes = append(disposes, dispose)
				envCtx.Provide(page.env, func() {
					if build == nil {
						return
					}
					if n := withRoot(r, build); n.e != nil {
						fill(n)
						page.appendKid(n.e)
					}
				})
			})
		})
		e.appendKid(page)
	}
	if p.Selected != nil {
		reported, known := 0, false
		signal.RenderEffect(func() {
			v := p.Selected.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			r.batch.SetInt(e.id, ops.PropTabsSelected, int64(v))
		})
		e.handlers().selected = func(i int64) {
			reported, known = int(i), true
			p.Selected.Set(int(i))
		}
		e.listen(ops.EventSelected)
	}
	signal.OnCleanup(func() {
		for _, d := range disposes {
			d()
		}
	})
	return Node{e}
}

// Detent is a height a sheet can rest at.
type Detent int

const (
	DetentMedium Detent = 1 << iota
	DetentLarge
)

// SheetProps configures a Sheet.
type SheetProps struct {
	// Open shows the sheet while true; the user dismissing it sets it
	// false.
	Open    *signal.Signal[bool]
	Content func() Node
	Detents Detent // 0: large
	Grabber bool
	// OnDismiss runs when the user dismisses the sheet.
	OnDismiss func()
}

// Sheet presents Content in a modal sheet while Open is true (a sheet
// presentation with detents on iOS, a BottomSheetDialog on Android). Its
// content is built when it opens and disposed when it closes. The node
// it returns takes no space; put it anywhere.
func Sheet(p SheetProps) Node {
	f := newFragment()
	r := f.r
	owner := signal.CurrentOwner()
	var sheet *element
	var dispose func()
	closeSheet := func() {
		if sheet == nil {
			return
		}
		dispose()
		sheet.destroy()
		sheet, dispose = nil, nil
	}
	signal.RenderEffect(func() {
		open := p.Open.Get()
		if open == (sheet != nil) {
			return
		}
		if !open {
			closeSheet()
			return
		}
		withRoot(r, func() Node {
			sheet = newProvider(ops.KindSheet)
			if p.Detents != 0 {
				r.batch.SetInt(sheet.id, ops.PropSheetDetents, int64(p.Detents))
			}
			if p.Grabber {
				r.batch.SetBool(sheet.id, ops.PropSheetGrabber, true)
			}
			sheet.handlers().dismissed = func() {
				p.Open.Set(false)
				if p.OnDismiss != nil {
					p.OnDismiss()
				}
			}
			sheet.listen(ops.EventDismissed)
			s := sheet
			signal.RunWithOwner(owner, func() {
				signal.Root(func(d func()) {
					dispose = d
					envCtx.Provide(s.env, func() {
						if n := p.Content(); n.e != nil {
							fill(n)
							s.appendKid(n.e)
						}
					})
				})
			})
			r.batch.Command(s.id, ops.CmdPresent, nil)
			return Node{}
		})
	})
	signal.OnCleanup(closeSheet)
	return Node{f}
}

// activeRoot is the root new imperative UI (alerts, haptics) belongs to.
func activeRoot() *Root {
	if cur != nil {
		return cur
	}
	if len(roots) == 0 {
		panic("ui: no mounted root")
	}
	return roots[len(roots)-1]
}

// AlertProps configures an Alert.
type AlertProps struct {
	Title, Message string
	Actions        []Action // none: a single OK
	ActionSheet    bool     // an action sheet rather than an alert
}

// Alert shows an alert now (UIAlertController; a Material dialog on
// Android). Pressing a button dismisses it and runs the button's
// OnPress.
func Alert(p AlertProps) {
	r := activeRoot()
	actions := p.Actions
	if len(actions) == 0 {
		actions = []Action{{Title: "OK", Role: ActionCancel}}
	}
	withRoot(r, func() Node {
		e := newElement(ops.KindAlert, true)
		b := r.batch
		if p.Title != "" {
			b.SetString(e.id, ops.PropAlertTitle, p.Title)
		}
		if p.Message != "" {
			b.SetString(e.id, ops.PropAlertMessage, p.Message)
		}
		b.SetBytes(e.id, ops.PropAlertButtons, encodeActions(actions))
		if p.ActionSheet {
			b.SetEnum(e.id, ops.PropAlertStyle, int32(ops.AlertStyleActionSheet))
		}
		h := e.handlers()
		h.action = func(i int64) {
			e.destroy()
			runAction(actions, i)
		}
		h.dismissed = func() { e.destroy() }
		e.listen(ops.EventAction)
		e.listen(ops.EventDismissed)
		b.Command(e.id, ops.CmdPresent, nil)
		return Node{}
	})
}

// MenuProps configures a Menu.
type MenuProps struct {
	Actions []Action
	// LongPress shows the menu on a long press (a context menu) rather
	// than a tap.
	LongPress bool
	Style     Styler
}

// Menu shows a menu of actions when its content is tapped or long
// pressed (UIMenu; PopupMenu on Android).
func Menu(p MenuProps, children ...Node) Node {
	e := newElement(ops.KindView, true)
	e.setStyle(p.Style, false)
	e.r.batch.SetBytes(e.id, ops.PropMenu, encodeActions(p.Actions))
	if p.LongPress {
		e.r.batch.SetEnum(e.id, ops.PropMenuTrigger, int32(ops.MenuTriggerLongPress))
	}
	actions := p.Actions
	e.handlers().action = func(i int64) { runAction(actions, i) }
	e.listen(ops.EventAction)
	addKids(e, children)
	return Node{e}
}

// HapticKind is a kind of haptic feedback.
type HapticKind = ops.Haptic

const (
	HapticSelection = ops.HapticSelection
	HapticLight     = ops.HapticLight
	HapticMedium    = ops.HapticMedium
	HapticHeavy     = ops.HapticHeavy
	HapticSuccess   = ops.HapticSuccess
	HapticWarning   = ops.HapticWarning
	HapticError     = ops.HapticError
)

// Haptic plays haptic feedback (UIFeedbackGenerator; Android's
// performHapticFeedback).
func Haptic(kind HapticKind) {
	r := activeRoot()
	r.batch.Command(r.el.id, ops.CmdHaptic, []byte{byte(kind)})
}

// refreshable wires pull to refresh on a Scroll or List.
func refreshable(e *element, onRefresh func(), refreshing signal.Accessor[bool], propOn, propRefreshing ops.PropID) {
	if onRefresh == nil {
		return
	}
	b := e.r.batch
	b.SetBool(e.id, propOn, true)
	reported := false // what the host shows
	signal.RenderEffect(func() {
		v := false
		if refreshing != nil {
			v = refreshing.Get()
		}
		if v != reported {
			reported = v
			e.r.batch.SetBool(e.id, propRefreshing, v)
		}
	})
	e.handlers().refresh = func() {
		reported = true // the host shows its spinner
		onRefresh()
		if refreshing == nil || !signal.Untrack(refreshing.Get) {
			reported = false
			e.r.batch.SetBool(e.id, propRefreshing, false)
		}
	}
	e.listen(ops.EventRefresh)
}

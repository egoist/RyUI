package ui

import (
	"bytes"
	"encoding/binary"
	"slices"

	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
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
	// Title is what it says; it can follow the app's language, or anything
	// else: Title: locales.Live.Delete().
	Title signal.Accessor[string]
	Role  ActionRole
	// Icon is a symbol beside the title in a menu; a navigation bar
	// shows it instead of the title (which VoiceOver and TalkBack read).
	Icon    Icon
	OnPress func()
	// Disabled dims the item, which can't be chosen: a menu item, an
	// alert's button, a bar button (UIMenuElement's .disabled; a disabled
	// MenuItem on Android). A swipe action ignores it.
	Disabled signal.Accessor[bool]
	// Checked shows a checkmark by a menu item: the choice in force, or an
	// option that's on (UIMenuElement.State.on; a checkable MenuItem on
	// Android).
	Checked signal.Accessor[bool]
	// Children make the item a submenu, shown when it's chosen (a nested
	// UIMenu; a SubMenu on Android). A bar button with children opens
	// them as a menu (SwiftUI's Menu in a toolbar). Its OnPress isn't
	// called. Alerts and swipe actions don't show children.
	Children []Action
	// Inline shows the Children in place, as a section of the menu between
	// dividers, under the Title if there is one (SwiftUI's Section in a
	// Menu; UIMenu's displayInline; a group of items with dividers on
	// Android).
	Inline bool
}

// Submenu is an item that opens a menu of children.
func Submenu(title signal.Accessor[string], icon Icon, children ...Action) Action {
	return Action{Title: title, Icon: icon, Children: children}
}

// MenuSection is a section of a menu: children shown in place, between
// dividers, under title (nil: none).
func MenuSection(title signal.Accessor[string], children ...Action) Action {
	return Action{Title: title, Children: children, Inline: true}
}

// encodeActions encodes as as a Menu prop (ops.AppendActions).
func encodeActions(as []Action) []byte { return appendActions(nil, as, 0, actionDepth) }

// appendActions appends as, encoded as ops.AppendActions does, reading
// each item's accessors (in an effect, following them). Children deeper
// than limit aren't sent (an alert's limit is 0: it shows none).
func appendActions(b []byte, as []Action, depth, limit int) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(as)))
	for i := range as {
		a := &as[i]
		title := ""
		if a.Title != nil {
			title = a.Title.Get()
		}
		var flags ops.MenuFlags
		if a.Disabled != nil && a.Disabled.Get() {
			flags |= ops.MenuDisabled
		}
		if a.Checked != nil && a.Checked.Get() {
			flags |= ops.MenuChecked
		}
		sub := (len(a.Children) > 0 || a.Inline) && depth < limit
		if sub {
			flags |= ops.MenuSubmenu
			if a.Inline {
				flags |= ops.MenuInline
			}
		}
		b = append(b, byte(a.Role))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(title)))
		b = append(b, title...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(a.Icon)))
		b = append(b, a.Icon...)
		b = append(b, byte(flags))
		if sub {
			b = appendActions(b, a.Children, depth+1, limit)
		}
	}
	return b
}

// appendSwipeActions appends a List cell's swipe actions, flat: a swipe
// shows neither children nor a disabled look, so each item's number is
// its place.
func appendSwipeActions(b []byte, as []Action) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(as)))
	for i := range as {
		a := &as[i]
		title := ""
		if a.Title != nil {
			title = a.Title.Get()
		}
		b = append(b, byte(a.Role))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(title)))
		b = append(b, title...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(a.Icon)))
		b = append(b, a.Icon...)
		b = append(b, 0)
	}
	return b
}

// runSwipeAction runs the OnPress of a swipe action, by its place.
func runSwipeAction(as []Action, i int64) {
	if i >= 0 && int(i) < len(as) && as[i].OnPress != nil {
		as[i].OnPress()
	}
}

// actionDepth bounds the menus' nesting, as hosts read it (ops'
// menuDepth): deeper children aren't sent.
const actionDepth = 16

// staticActions reports whether nothing as shows can change.
func staticActions(as []Action) bool {
	for i := range as {
		a := &as[i]
		if !staticOrNil(a.Title) || !staticOrNil(a.Disabled) || !staticOrNil(a.Checked) || !staticActions(a.Children) {
			return false
		}
	}
	return true
}

func staticOrNil[T any](a signal.Accessor[T]) bool {
	if a == nil {
		return true
	}
	_, ok := signal.StaticValue(a)
	return ok
}

// bindActions sends as, and again when what they show changes (a title,
// Disabled, Checked, at any depth up to limit).
func bindActions(as []Action, limit int, apply func([]byte)) {
	if staticActions(as) {
		apply(appendActions(nil, as, 0, limit))
		return
	}
	var last, buf []byte
	sent := false
	signal.RenderEffect(func() {
		buf = appendActions(buf[:0], as, 0, limit)
		if sent && bytes.Equal(buf, last) {
			return
		}
		sent = true
		apply(buf) // the batch copies it
		last, buf = buf, last
	})
}

// actionAt returns the item numbered i in as, depth first (a submenu or
// section before its children, up to limit), or nil.
func actionAt(as []Action, i int64, limit int) *Action {
	n := int64(0)
	var find func(as []Action, depth int) *Action
	find = func(as []Action, depth int) *Action {
		for k := range as {
			a := &as[k]
			if n == i {
				return a
			}
			n++
			if (len(a.Children) > 0 || a.Inline) && depth < limit {
				if f := find(a.Children, depth+1); f != nil {
					return f
				}
			}
		}
		return nil
	}
	if i < 0 {
		return nil
	}
	return find(as, 0)
}

// runAction runs the OnPress of the item numbered i (as sent with
// limit), unless it's disabled or opens children.
func runAction(as []Action, i int64, limit int) {
	a := actionAt(as, i, limit)
	if a == nil || a.OnPress == nil || ((len(a.Children) > 0 || a.Inline) && limit > 0) {
		return
	}
	if a.Disabled != nil && signal.Untrack(a.Disabled.Get) {
		return // a host's menu was out of date
	}
	a.OnPress()
}

// Icon is a symbol (ui.Symbol, tabs, buttons, actions): an SF Symbol
// name. iOS draws any; Android draws the common ones, as the Material
// Symbols for them: the constants here, and more (see
// platform/android/gen_symbols.go). A name with ".fill" is the filled
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
	IconBack      Icon = "chevron.backward" // mirrors right to left, with the reading direction
	IconForward   Icon = "chevron.forward"  // likewise
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
	IconQRCode    Icon = "qrcode"
	IconVideo     Icon = "video"
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
	// Search puts a search field in the bar (SwiftUI's searchable).
	Search *Search
	// ZoomFrom is the Style.Match key of a view on the screen below (a
	// photo in a grid, a card): the screen zooms out of it as it's pushed,
	// and back into it as it's popped (SwiftUI's
	// navigationTransition(.zoom); iOS before 18 pushes as usual).
	ZoomFrom string
	// LargeTitle shows the title large at the top of the screen, collapsing
	// into the bar as it scrolls: iOS's way for an app's main screens
	// (SwiftUI's navigationBarTitleDisplayMode(.large)). Material's top app
	// bar has its own look, and Android keeps it.
	LargeTitle bool
	// BarColor is the top app bar's color on Android while this screen is
	// on top, so the bar can continue a background the screen draws itself
	// (nil, or 0: the surface). iOS's bar is clear at the top of a screen,
	// over the screen, and needs no help.
	BarColor signal.Accessor[Color]
	// Toolbar are buttons in a bar at the bottom of the screen (SwiftUI's
	// ToolbarItem(placement: .bottomBar)): the navigation controller's
	// toolbar on iOS, with flexible space between them; Material's
	// BottomAppBar on Android, as icon buttons, the ones that don't fit in
	// its overflow menu. A disabled one is dimmed, one with Children opens
	// them as a menu, and they follow their accessors as Actions do. The
	// desktops put them where their apps put such buttons: in the window's
	// toolbar on macOS (SwiftUI's bottomBar is iOS's only), in a command
	// bar under the screen on Windows, in a GtkActionBar at the bottom of
	// the page on Linux (libadwaita's AdwToolbarView), in a bar at the
	// bottom on the web.
	Toolbar []Action
	// Primary is the screen's main action, such as compose or add:
	// Material's FloatingActionButton at the bottom end of the screen on
	// Android, docked in the BottomAppBar when there's a Toolbar. iOS has no
	// floating button: it's the trailing item of the bottom toolbar, which
	// the screen gets for it if it has no Toolbar, prominent on iOS 26
	// (Mail's Compose). A prominent item of the window's toolbar on macOS,
	// an accent button on Windows, a suggested-action button on Linux, a
	// floating button on the web. Its Icon is what the button shows, its
	// Title what screen readers say; Children make it open a menu.
	Primary *Action
	// PrimaryExtended makes Primary Android's ExtendedFloatingActionButton:
	// its Title beside its Icon, shrinking to the icon as the screen's
	// content scrolls down and extending again as it scrolls up. The other
	// platforms show Primary as they always do (the web's floating button
	// with its title too).
	PrimaryExtended bool
}

// Search is a screen's search field.
type Search struct {
	// Text is what's typed: the user's typing writes it, code's is shown.
	Text        *signal.Signal[string]
	Placeholder signal.Accessor[string] // nil: "Search", in the app's language
	OnSubmit    func()
	// Button keeps the field out of the way until it's asked for, for a
	// screen with its own controls where iOS would put it: a button on iOS
	// 26, and before that, hidden above the content until it's pulled down.
	// Android's is a search action in the bar already.
	Button bool
}

// search wires a screen's search field.
func search(e *element, s *Search) {
	if s == nil {
		return
	}
	ph := s.Placeholder
	if ph == nil {
		ph = signal.Func(func() string { return l10n.Search(Locale()) })
	}
	bind(ph, func(v string) { e.r.batch.SetString(e.id, ops.PropScreenSearch, v) })
	if s.Button {
		e.r.batch.SetBool(e.id, ops.PropScreenSearchButton, true)
	}
	h := e.handlers()
	if s.Text != nil {
		reported, known := "", false
		signal.RenderEffect(func() {
			v := s.Text.Get()
			if known && v == reported {
				return
			}
			reported, known = v, true
			e.r.batch.SetString(e.id, ops.PropScreenSearchText, v)
		})
		h.text = func(v string) {
			reported, known = v, true
			s.Text.Set(v)
		}
		e.listen(ops.EventTextChanged)
	}
	if s.OnSubmit != nil {
		h.submit = s.OnSubmit
		e.listen(ops.EventSubmit)
	}
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
	back    *backScope // where the user's going back from it is taken (nil: the stack's first screen)
	actions []Action
	toolbar []Action
	primary []Action // ScreenOptions.Primary, a menu of one
}

// action runs the item an Action event on the screen names: of its bar
// buttons, its Toolbar or its Primary (ops.ScreenToolbarActions,
// ops.ScreenPrimaryAction).
func (s *stackScreen) action(i int64) {
	switch {
	case i >= ops.ScreenPrimaryAction:
		runAction(s.primary, i-ops.ScreenPrimaryAction, actionDepth)
	case i >= ops.ScreenToolbarActions:
		runAction(s.toolbar, i-ops.ScreenToolbarActions, actionDepth)
	default:
		runAction(s.actions, i, actionDepth)
	}
}

type stackState struct {
	e       *element
	nav     *Navigator
	owner   *signal.Owner
	screens []*stackScreen
	// pushed and removed tell a Router's stack of the screens pushed and
	// removed, by code (a Navigator, the Router itself) or by the user.
	pushed, removed func(*stackScreen)
}

// Stack is a navigation stack (UINavigationController; on Android a
// view stack with a top app bar, transitions and predictive back). It
// starts with root; components in it push more with UseNavigator. Each
// screen is an owner: popping it, by code or by the user's back gesture,
// disposes it.
func Stack(root func() Node, o ScreenOptions, style ...Styler) Node {
	n, _ := newStack(root, o, firstStyle(style))
	return n
}

func newStack(root func() Node, o ScreenOptions, style Styler) (Node, *stackState) {
	e := newElement(ops.KindStack, true)
	e.setStyle(style, false)
	growByDefault(e)
	st := &stackState{e: e, owner: signal.CurrentOwner()}
	st.nav = &Navigator{st}
	e.handlers().popped = func(id int64) {
		if n := len(st.screens); n > 1 && st.screens[n-1].el.id == ops.NodeID(id) {
			st.remove(st.screens[n-1])
		}
	}
	e.listen(ops.EventPopped)
	if root != nil {
		st.push(root, o)
	}
	signal.OnCleanup(func() {
		for _, s := range st.screens {
			s.dispose()
		}
	})
	return Node{e}, st
}

// reset makes root the stack's only screen: its screens are disposed, the
// last first, and root is pushed in an empty stack, which the host shows
// with no push.
func (st *stackState) reset(root func() Node, o ScreenOptions) {
	st.e.r.batch.Command(st.e.id, ops.CmdReplace, nil)
	for len(st.screens) > 0 {
		st.remove(st.screens[len(st.screens)-1])
	}
	st.push(root, o)
}

func (st *stackState) push(build func() Node, o ScreenOptions) *stackScreen {
	r := st.e.r
	s := &stackScreen{actions: o.Actions, toolbar: o.Toolbar}
	if o.Primary != nil {
		s.primary = []Action{*o.Primary}
	}
	withRoot(r, func() Node {
		s.el = newProvider(ops.KindScreen)
		b := r.batch
		id := s.el.id
		if len(o.Actions) > 0 || len(o.Toolbar) > 0 || o.Primary != nil {
			s.el.handlers().action = s.action
			s.el.listen(ops.EventAction)
		}
		if o.Primary != nil && o.PrimaryExtended {
			b.SetBool(id, ops.PropScreenPrimaryExtended, true)
		}
		if o.NoBackGesture {
			b.SetBool(id, ops.PropScreenBackGesture, false)
		}
		if o.ZoomFrom != "" {
			b.SetString(id, ops.PropScreenZoomFrom, o.ZoomFrom)
		}
		if o.LargeTitle {
			b.SetBool(id, ops.PropScreenLargeTitle, true)
		}
		signal.RunWithOwner(st.owner, func() {
			signal.Root(func(dispose func()) {
				s.dispose = dispose
				if len(o.Actions) > 0 {
					bindActions(o.Actions, actionDepth, func(v []byte) { r.batch.SetBytes(id, ops.PropScreenActions, v) })
				}
				if len(o.Toolbar) > 0 {
					bindActions(o.Toolbar, actionDepth, func(v []byte) { r.batch.SetBytes(id, ops.PropScreenToolbar, v) })
				}
				if len(s.primary) > 0 {
					bindActions(s.primary, actionDepth, func(v []byte) { r.batch.SetBytes(id, ops.PropScreenPrimary, v) })
				}
				bind(o.Title, func(v string) { r.batch.SetString(id, ops.PropScreenTitle, v) })
				bind(o.BarColor, func(v Color) { r.batch.SetColor(id, ops.PropScreenBarColor, v) })
				search(s.el, o.Search)
				content := func() {
					envCtx.Provide(s.el.env, func() {
						if n := build(); n.e != nil {
							fill(n)
							s.el.appendKid(n.e)
						}
					})
				}
				navCtx.Provide(st.nav, func() {
					if len(st.screens) == 0 {
						content() // going back from the first screen leaves the stack: the scope around it
						return
					}
					s.back = newBackScope(s.el, func() {
						if slices.Contains(st.screens, s) {
							st.remove(s)
						}
					})
					backCtx.Provide(s.back, content)
				})
			})
		})
		return Node{}
	})
	st.screens = append(st.screens, s)
	st.e.appendKid(s.el) // the host pushes it
	if st.pushed != nil {
		st.pushed(s)
	}
	return s
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
	if st.removed != nil {
		st.removed(s)
	}
}

// Tab is a page of Tabs.
type Tab struct {
	Title   signal.Accessor[string]
	Icon    Icon
	Content func() Node
	// Badge is shown on the tab (SwiftUI's badge on a tab item, Compose's
	// BadgedBox on a NavigationBarItem): usually a count, "3", though any
	// short text works; empty or nil, none. It follows the accessor,
	// updating the tab alone. Screen readers say it with the tab's title,
	// a count as so many items ("Inbox, 3 items"):
	//
	//   - iOS: UITabBarItem's badgeValue, the system's red badge.
	//   - Android: the navigation bar item's BadgeDrawable, its number
	//     (999+ past 999, as Material shortens it) or its text; TalkBack
	//     says Material's words ("3 new notifications").
	//   - macOS: in the tab's label, "Inbox (3)": NSTabView draws its
	//     tabs' labels alone.
	//   - Windows: Fluent's InfoBadge on the tab.
	//   - Linux: AdwViewStackPage's badge-number for a count, shown by the
	//     view switcher; other text marks the page as needing attention.
	//   - Web: a badge on the tab, part of its accessible name.
	Badge signal.Accessor[string]
}

// TabsProps configures Tabs.
type TabsProps struct {
	// Selected is the shown tab's index; the user's choice writes it.
	Selected *signal.Signal[int]
	Style    Styler

	// chosen tells a Router of the user's choice, after Selected.
	chosen func(int)
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
		pageID := page.id
		signal.RunWithOwner(owner, func() {
			bind(t.Title, func(v string) { r.batch.SetString(pageID, ops.PropScreenTitle, v) })
		})
		if t.Icon != "" {
			r.batch.SetString(page.id, ops.PropScreenIcon, string(t.Icon))
		}
		if t.Badge != nil {
			badge := t.Badge
			signal.RunWithOwner(owner, func() {
				bind(badge, func(v string) { r.batch.SetString(pageID, ops.PropScreenBadge, v) })
			})
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
			if p.chosen != nil {
				p.chosen(int(i))
			}
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
	// FullScreen covers the whole screen instead (SwiftUI's
	// fullScreenCover, Material's full-screen dialog), for a task that
	// takes the user's attention until it's done or given up: Detents
	// and Grabber don't apply, and the user can't swipe it away or tap
	// outside it; it closes when Open goes false. Going back (Android's
	// back, Escape on a desktop or the web) dismisses it as it does a
	// sheet, unless OnBack intercepts it. Its Environment is the screen's
	// own: the whole size, and the safe area of the status bar, the
	// notch, the home indicator or navigation bar, and the keyboard.
	FullScreen bool
	// OnDismiss runs when the user dismisses the sheet.
	OnDismiss func()
}

// Sheet presents Content in a modal sheet while Open is true (a sheet
// presentation with detents on iOS, a BottomSheetDialog on Android). Its
// content is built when it opens and disposed when it closes. The node
// it returns takes no space; put it anywhere. On a desktop it's a sheet
// of the window it's declared in (a Window's content: that Window, open
// or opening; else the main window), whichever window is in use, and a
// key in it goes on to that window's keys.
//
// A FullScreen sheet is a UIViewController presented .fullScreen on iOS
// (it slides up over everything); a full-screen Dialog on Android, edge
// to edge, sliding up, with predictive back; on macOS a sheet as big as
// its window's content, under its toolbar, following the window as it
// resizes; on Windows a borderless window over its window's content,
// following it, as a ContentDialog in Fluent's full-size form; on Linux
// an AdwDialog filling its window, as libadwaita shows a dialog that
// doesn't fit; on the web a modal <dialog> over the whole page (or over
// a Window's panel, for a sheet declared in one).
func Sheet(p SheetProps) Node {
	f := newFragment()
	r := f.r
	owner := signal.CurrentOwner()
	win := windowCtx.Use() // the window it's declared in, as UseKeys'
	if win == nil {
		win = r.win
	}
	var shown *sheetView
	closeSheet := func() {
		if shown != nil {
			shown.close()
			shown = nil
		}
	}
	signal.RenderEffect(func() {
		open := p.Open.Get()
		if open == (shown != nil) {
			return
		}
		if !open {
			closeSheet()
			return
		}
		shown = showSheet(r, win, owner, p, false, func() {
			p.Open.Set(false)
			if p.OnDismiss != nil {
				p.OnDismiss()
			}
		}, p.Content)
	})
	signal.OnCleanup(closeSheet)
	return Node{f}
}

// sheetView is a sheet shown: its element, its content's owner and where
// the user's going back from it is taken.
type sheetView struct {
	el      *element
	dispose func()
	back    *backScope
}

// showSheet presents a sheet of p's on win, its content built under
// owner; dismissed is the user's dismissal (and going back's, when no
// OnBack takes it). A Router's sheet is routed.
func showSheet(r *Root, win *WindowHandle, owner *signal.Owner, p SheetProps, routed bool, dismissed func(), content func() Node) *sheetView {
	v := &sheetView{}
	withRoot(r, func() Node {
		sheet := newProvider(ops.KindSheet)
		v.el = sheet
		sheet.up = win.el()
		if sheet.up == nil {
			sheet.up = r.el
		}
		r.batch.SetInt(sheet.id, ops.PropSheetWindow, int64(sheet.up.id))
		if p.FullScreen {
			r.batch.SetBool(sheet.id, ops.PropSheetFullScreen, true)
		} else {
			if p.Detents != 0 {
				r.batch.SetInt(sheet.id, ops.PropSheetDetents, int64(p.Detents))
			}
			if p.Grabber {
				r.batch.SetBool(sheet.id, ops.PropSheetGrabber, true)
			}
		}
		if routed {
			r.batch.SetBool(sheet.id, ops.PropSheetRouted, true)
		}
		sheet.handlers().dismissed = func() {
			if !sheet.dead {
				dismissed()
			}
		}
		sheet.listen(ops.EventDismissed)
		signal.RunWithOwner(owner, func() {
			signal.Root(func(d func()) {
				v.dispose = d
				v.back = newBackScope(sheet, func() {
					if !sheet.dead {
						sheet.h.dismissed() // as if the user dismissed it
					}
				})
				backCtx.Provide(v.back, func() {
					envCtx.Provide(sheet.env, func() {
						if n := content(); n.e != nil {
							sheet.appendKid(onlyKid(n))
						}
					})
				})
			})
		})
		r.batch.Command(sheet.id, ops.CmdPresent, nil)
		return Node{}
	})
	return v
}

// close dismisses the sheet: its content is disposed, its view destroyed.
func (v *sheetView) close() {
	if v.el.dead {
		return
	}
	v.dispose()
	v.el.destroy()
}

// PopoverProps configures a Popover.
type PopoverProps struct {
	// Open shows the popover while true; the user dismissing it (a tap
	// outside) sets it false.
	Open    *signal.Signal[bool]
	Content func() Node
	// MaxWidth is how wide it may be, in points (0: 320); it's as wide as
	// its content otherwise, and as tall, up to most of the screen.
	MaxWidth  float32
	OnDismiss func()
	// Style is the anchor's: the view the popover points at, which holds
	// Popover's children.
	Style Styler
}

// Popover shows Content in a bubble pointing at its children while Open
// is true: a few options, a detail, a small form, over the screen
// (SwiftUI's popover). It's iPadOS's and iOS's popover, a popover on an
// iPhone too (UIPopoverPresentationController, not adapted into a
// sheet); a PopupWindow with Material's surface and elevation on
// Android; NSPopover on macOS; a popup window on Windows. It's as big as
// its content, up to MaxWidth wide. Its content is built when it opens
// and disposed when it closes; OnBack in it takes its dismissal.
func Popover(p PopoverProps, children ...Node) Node {
	anchor := newElement(ops.KindView, true)
	anchor.setStyle(p.Style, false)
	addKids(anchor, children)
	r := anchor.r
	owner := signal.CurrentOwner()
	var pop *element
	var dispose func()
	closePop := func() {
		if pop == nil {
			return
		}
		dispose()
		pop.destroy()
		pop, dispose = nil, nil
	}
	signal.RenderEffect(func() {
		open := p.Open.Get()
		if open == (pop != nil) {
			return
		}
		if !open {
			closePop()
			return
		}
		withRoot(r, func() Node {
			pop = newProvider(ops.KindPopover)
			pop.fits = true
			pop.maxW = p.MaxWidth
			if pop.maxW <= 0 {
				pop.maxW = 320
			}
			st := pop.box.Style
			st.Width, st.Height = layout.Auto, layout.Auto
			pop.box.SetStyle(st)
			r.batch.SetInt(pop.id, ops.PropPopoverAnchor, int64(anchor.id))
			pop.up = anchor
			s := pop
			s.handlers().dismissed = func() {
				p.Open.Set(false)
				if p.OnDismiss != nil {
					p.OnDismiss()
				}
			}
			s.listen(ops.EventDismissed)
			signal.RunWithOwner(owner, func() {
				signal.Root(func(d func()) {
					dispose = d
					scope := newBackScope(s, func() {
						if pop == s {
							s.h.dismissed()
						}
					})
					backCtx.Provide(scope, func() {
						envCtx.Provide(s.env, func() {
							if n := p.Content(); n.e != nil {
								s.appendKid(n.e)
							}
						})
					})
				})
			})
			r.batch.Command(s.id, ops.CmdPresent, nil)
			return Node{}
		})
	})
	signal.OnCleanup(closePop)
	return Node{anchor}
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

// alertWindow is the element of the window an Alert shown now goes on:
// that of the view whose event is being handled, else of the component
// building (the window it's in), else the root's.
func alertWindow(r *Root) *element {
	if e := eventEl; e != nil && e.r == r && !e.dead {
		if w := e.window(); w != nil && !w.dead {
			return w
		}
	}
	if w := windowCtx.Use(); w != nil && w.r == r {
		if e := w.el(); e != nil {
			return e
		}
	}
	return r.el
}

// AlertProps configures an Alert.
type AlertProps struct {
	Title, Message string
	Actions        []Action // none: a single OK
	ActionSheet    bool     // an action sheet rather than an alert
	// Fields are text fields under the message, for a name or a password
	// (SwiftUI's TextField and SecureField in an alert;
	// UIAlertController's addTextField; TextInputLayouts in a Material
	// dialog; an NSAlert's accessory view; AdwEntryRows in an
	// AdwAlertDialog). The first has the focus, with the keyboard up, and
	// return in the last presses the default action: the first whose
	// role is ActionDefault, unless it's disabled (UIAlertController's
	// preferredAction). An action sheet shows none.
	Fields []AlertField
}

// AlertField is a text field of an Alert.
type AlertField struct {
	// Text is the field's text: it starts with Text's value, and typing
	// writes it as the user types, so an action's Disabled can follow it
	// (OK disabled while the name is empty), which the alert shows while
	// it's up. Writing it while the alert shows doesn't change the field.
	Text        *signal.Signal[string]
	Placeholder string
	// Secure hides what's typed: a password (SecureField; a
	// UITextField's isSecureTextEntry; NSSecureTextField;
	// AdwPasswordEntryRow; an input of type password).
	Secure   bool
	Keyboard KeyboardType
	// ContentType says what the field holds, for the system's AutoFill,
	// as TextFieldProps' does: one of the Content constants.
	ContentType string
}

// encodeAlertFields encodes fs as an Alert's Fields prop, with each
// field's starting text.
func encodeAlertFields(fs []AlertField) []byte {
	out := make([]ops.AlertField, len(fs))
	for i := range fs {
		f := &fs[i]
		of := ops.AlertField{Placeholder: f.Placeholder, Keyboard: f.Keyboard, ContentType: f.ContentType}
		if f.Text != nil {
			of.Text = f.Text.Peek()
		}
		if f.Secure {
			of.Flags |= ops.AlertFieldSecure
		}
		out[i] = of
	}
	return ops.AppendAlertFields(nil, out)
}

// Alert shows an alert now (UIAlertController; a Material dialog on
// Android). Pressing a button dismisses it and runs the button's
// OnPress. On a desktop it goes on the window of the view whose event
// shows it (a button's OnPress in a Window: that Window), else of the
// component calling it, else the main window.
func Alert(p AlertProps) {
	r := activeRoot()
	actions := p.Actions
	if len(actions) == 0 {
		actions = []Action{{Title: Val(l10n.OK(Locale())), Role: ActionCancel}}
	}
	withRoot(r, func() Node {
		e := newElement(ops.KindAlert, true)
		b := r.batch
		b.SetInt(e.id, ops.PropAlertWindow, int64(alertWindow(r).id))
		if p.Title != "" {
			b.SetString(e.id, ops.PropAlertTitle, p.Title)
		}
		if p.Message != "" {
			b.SetString(e.id, ops.PropAlertMessage, p.Message)
		}
		// Its buttons follow their titles and Disabled while it shows: an
		// owner of its own, under the root's, disposed as it goes.
		var owner *signal.Owner
		signal.RunWithOwner(r.owner, func() {
			owner = signal.NewOwner()
			signal.RunWithOwner(owner, func() {
				bindActions(actions, 0, func(v []byte) {
					if !e.dead {
						r.batch.SetBytes(e.id, ops.PropAlertButtons, v)
					}
				})
				// Still up as the root goes (unmounted, reloaded): it
				// goes too, as a sheet does with its owner.
				signal.OnCleanup(e.destroy)
			})
		})
		if p.ActionSheet {
			b.SetEnum(e.id, ops.PropAlertStyle, int32(ops.AlertStyleActionSheet))
		}
		h := e.handlers()
		if fields := p.Fields; len(fields) > 0 && !p.ActionSheet {
			b.SetBytes(e.id, ops.PropAlertFields, encodeAlertFields(fields))
			h.fieldChanged = func(v []byte) {
				i, text, ok := ops.DecodeFieldChanged(v)
				if !ok || i < 0 || i >= len(fields) || e.dead {
					return
				}
				if t := fields[i].Text; t != nil {
					t.Set(text)
				}
			}
			e.listen(ops.EventFieldChanged)
		}
		h.action = func(i int64) {
			owner.Dispose()
			e.destroy()
			runAction(actions, i, 0)
		}
		h.dismissed = func() {
			owner.Dispose()
			e.destroy()
		}
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
	bindActions(p.Actions, actionDepth, func(b []byte) { e.r.batch.SetBytes(e.id, ops.PropMenu, b) })
	if p.LongPress {
		e.r.batch.SetEnum(e.id, ops.PropMenuTrigger, int32(ops.MenuTriggerLongPress))
	}
	actions := p.Actions
	e.handlers().action = func(i int64) { runAction(actions, i, actionDepth) }
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
	if r := activeRoot(); r != nil {
		r.batch.Command(r.el.id, ops.CmdHaptic, []byte{byte(kind)})
	}
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

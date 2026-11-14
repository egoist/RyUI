# Navigation

An app's structure is made of stacks of screens, tabs, sheets, alerts
and menus, and on a desktop, windows and the menu bar. Each one is the
platform's own:

- **iOS**: a `UINavigationController`, a `UITabBarController`, sheet
  presentations, `UIAlertController`, `UIMenu`.
- **Android**: a top app bar with Material transitions and predictive
  back, Material's bottom navigation bar, a `BottomSheetDialog`, Material
  dialogs, `PopupMenu`.
- **macOS**: the window's toolbar (the back button, the screen's bar
  buttons and search field) and title, an `NSTabView`, sheet windows,
  `NSAlert`, `NSMenu`, and the menu bar.
- **Windows**: a header bar over the screen, a tab bar as WinUI's
  `NavigationView` at the top, modal windows, task dialogs, popup menus,
  and the window's menu bar, drawn as Windows 11's.
- **Linux**: libadwaita's: the window's header bar, `AdwNavigationView`,
  a view switcher, `AdwDialog`, `AdwAlertDialog`, GTK's popover menus,
  and the main menu.
- **The web**: a header bar of the host's, `<dialog>`s and popovers, and
  the browser's back button (see [the web](web.md)).

This page covers them, along with going back, routes (every screen at
an address, as on the web), and the links and shares that open the
app. [`examples/navigation`](../examples/navigation) has
all of them; [`examples/notes`](../examples/notes) has a menu bar and a
window.

## Stacks

`ui.Stack` is a navigation stack. It starts with a root screen, and
components in it push more with the navigator from `ui.UseNavigator`.

```go
func App() ui.Node {
	return ui.Stack(Notes, ui.ScreenOptions{Title: ui.Val("Notes")})
}

func Notes() ui.Node {
	nav := ui.UseNavigator()
	return ui.List(ui.ListProps{}, notes, func(n note) int { return n.ID },
		func(n signal.Accessor[note], _ signal.Accessor[int]) ui.Node {
			return ui.View(ui.ViewProps{
				OnPress: func() {
					v := n.Get() // in a handler: not tracked
					nav.Push(func() ui.Node { return Detail(v) }, ui.ScreenOptions{Title: ui.Val(v.Text)})
				},
				Style: ui.Style{Padding: ui.Symmetric(12, 20)},
			}, ui.Text(signal.Func(func() string { return n.Get().Text })))
		})
}

func Detail(n note) ui.Node {
	nav := ui.UseNavigator()
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
		ui.StaticText(n.Text),
		ui.Button(ui.ButtonProps{Title: ui.Val("Done"), OnPress: nav.Pop}),
	)
}
```

- `nav.Push(screen, options)` shows a screen on top, with the
  platform's push animation.
- `nav.Pop()` removes the top screen. It never pops the root.
- `nav.Depth()` is how many screens the stack holds.

Call `ui.UseNavigator` while the component is being built, as above, and
keep the navigator for its handlers. It panics outside a stack.

On iOS and Android each stack has its own bar, and so it does on
Windows (a header bar the host draws over the screen, as WinUI's) and
on the web. On a Mac and on Linux a window has one bar, as their apps
do, and it belongs to the innermost stack on screen (in tabs, the shown
tab's): its back button, its top screen's title, bar buttons and search
field. On a Mac that's the window's toolbar and title; on Linux the
window's `AdwHeaderBar`. Pushes and pops slide on macOS, Linux and the
web; on Windows, screens change without a transition.

A stack fills the space it's given. Each screen is an
[owner](concepts.md): popping it, by code or by the user's back gesture,
disposes it, stopping its effects. A screen pushed again starts fresh,
so keep what must outlive it in a signal outside it, or in
[storage](data.md).

## Screen options

`ui.ScreenOptions` configures a screen of a stack, given to `Stack` for
the root and to `Push` for the others.

- `Title` is the navigation bar's title (the top app bar's on Android,
  the header bar's on Windows, Linux and the web; on a Mac, the window's
  title; in a browser, the page's title too). It's an accessor, so it
  can change.
- `Actions` are the bar's buttons.
- `NoBackGesture` turns the back gesture off for the screen (see
  [Back](#back)).
- `Search` puts a search field in the bar.
- `ZoomFrom` zooms the screen out of a view on the screen below.
- `LargeTitle` shows the title large at the top of the screen on iOS,
  collapsing into the bar as it scrolls. Android's top app bar, the
  desktops' bars and the web's stay as they are. The bar follows the
  first scroll view that takes up most of the screen, so something can be
  behind it.
- `BarColor` is the top app bar's color on Android, and the header's on
  the web, to continue a background the screen draws itself. iOS's bar is
  clear at the top of a screen already; the desktops' bars keep their
  own.
- `Toolbar` are buttons in a bar at the bottom of the screen, and
  `Primary` is the screen's main action (see [Toolbar and primary
  action](#toolbar-and-primary-action)); `PrimaryExtended` makes it
  Android's extended floating action button.

### Bar buttons

Each action is a `ui.Action`. With an `Icon`, the bar shows the icon
instead of the title, and screen readers read the title (on a desktop,
it's the icon's tooltip too).

```go
nav.Push(func() ui.Node { return Detail(n) }, ui.ScreenOptions{
	Title: ui.Val(n.Text),
	Actions: []ui.Action{
		{Title: ui.Val("Share"), Icon: ui.IconShare, OnPress: func() { ui.Share(n.Text) }},
		{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() { nav.Pop() }},
	},
})
```

On Android, actions go in the top app bar as there's room, and the rest
go in its overflow menu. A long press on an icon shows its title. On a
Mac they're toolbar items, on Windows buttons at the header bar's end,
and on Linux buttons in the header bar.

### Toolbar and primary action

`Toolbar` puts buttons in a bar at the bottom of the screen, as SwiftUI's
`ToolbarItem(placement: .bottomBar)`, and `Primary` is the screen's main
action, such as compose or add: Material's floating action button.
They're `ui.Action`s, as the bar's buttons are: an `Icon` is what the
button shows and its `Title` what screen readers say, `Disabled` dims
one, `Children` make one open a menu, and their accessors are followed.

```go
nav.Push(func() ui.Node { return Message(m) }, ui.ScreenOptions{
	Title: ui.Val(m.From),
	Toolbar: []ui.Action{
		{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: remove},
		{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", Children: []ui.Action{
			{Title: ui.Val("Reply"), OnPress: reply},
			{Title: ui.Val("Forward"), OnPress: forward},
		}},
		{Title: ui.Val("Mark as unread"), Icon: "envelope", Disabled: unread},
	},
	Primary: &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: compose},
})
```

- iOS: the navigation controller's toolbar, with flexible space between
  the buttons, shown while the screen is on top and sliding with pushes
  and pops. iOS has no floating button, so `Primary` is the toolbar's
  trailing item, prominent on iOS 26 (Mail's Compose); a screen with only
  a `Primary` gets the toolbar for it. The screen's safe area takes the
  toolbar, as UIKit's does. A pushed screen with a `Toolbar` hides its
  tabs' tab bar (from iOS 18), as a message in Mail does: iOS 26's
  floating tab bar would lie over the toolbar.
- Android: Material 3's `BottomAppBar`, the buttons as icon buttons from
  its start, those that don't fit in its overflow menu. `Primary` is a
  `FloatingActionButton` at the bottom end of the screen, or docked in
  the bar (with no shadow) when there's a `Toolbar`. With
  `PrimaryExtended` it's an `ExtendedFloatingActionButton`, its title
  beside its icon, which shrinks to the icon as the screen's content
  scrolls down and extends as it scrolls up (above the bar when there's
  one). Both are the screen's own, so they go with it in pushes, pops and
  predictive back; the bar takes the navigation bar under it, and the
  screen's safe area takes the bar. A pushed screen with a `Toolbar`
  hides its tabs' navigation bar, as Material has it.
- macOS: SwiftUI's bottom bar is iOS's, so a Mac shows the `Toolbar`'s
  buttons in the window's toolbar after the bar's, and `Primary` last,
  prominent (macOS 26's accent-tinted toolbar item).
- Windows: a command bar under the screen (Fluent's `CommandBar`): the
  buttons with their icons and titles from the leading edge, the ones
  that don't fit in a More options (…) menu, and `Primary` as an accent
  button at the trailing end.
- Linux: a `GtkActionBar` at the bottom of the page, in an
  `AdwToolbarView` (GNOME's action bar): the buttons at its start,
  `Primary` at its end as a `suggested-action` button with its icon and
  title.
- The web: a bar at the bottom of the stack (the buttons that don't fit
  in a More options menu), and `Primary` a floating button over the
  bottom trailing corner; an extended one shows its title, folding to its
  icon as the content scrolls down.

A press reports its action to the screen; `host/fake`'s `PressToolbar`
and `PressPrimary` do it in tests.

### Search

`Search` puts a search field in the bar, as SwiftUI's `searchable` does.
Typing writes `Text`, and writing `Text` from code changes the field.
`Placeholder` is "Search" when it's empty, and `OnSubmit` runs on the
keyboard's search key, or Enter on a desktop.

```go
query := signal.New("")
shown := signal.Func(func() []note {
	var out []note
	for _, n := range notes.Get() {
		if strings.Contains(strings.ToLower(n.Text), strings.ToLower(query.Get())) {
			out = append(out, n)
		}
	}
	return out
})
return ui.Stack(func() ui.Node {
	return ui.List(ui.ListProps{}, shown, func(n note) int { return n.ID },
		func(n signal.Accessor[note], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return n.Get().Text }), ui.Style{Padding: ui.Symmetric(12, 20)})
		})
}, ui.ScreenOptions{Title: ui.Val("Notes"), Search: &ui.Search{Text: query, Placeholder: ui.Val("Search notes")}})
```

On iOS it's a `UISearchController` under the title, which stays shown
as the screen scrolls. On Android it's Material's pattern: a search
button in the top app bar that expands into the field. `Button: true` keeps
it out of the way on iOS too, for a screen with its own controls at the bottom:
a button in the bar on iOS 26, and before that, hidden above the content until
it's pulled down.

On a desktop and on the web, the field is in the screen's bar:

- **macOS**: a search field in the toolbar (`NSSearchToolbarItem`).
- **Windows**: a Fluent text box with the search glyph, before the bar
  buttons, as WinUI puts a search box in a command bar.
- **Linux**: a search button in the header bar, and GTK's search bar
  under it, as GNOME's apps search; typing anywhere in the window opens
  it.
- **The web**: a field under the header (a button in it, with
  `Button: true`).

### Zooming from a view

`ZoomFrom` is the `Style.Match` key of a view on the screen below, such
as a photo in a grid or a card. The screen zooms out of that view as
it's pushed, and back into it as it's popped. See
[shared elements](motion.md#shared-elements).

```go
key := fmt.Sprintf("photo-%d", id)
return ui.View(ui.ViewProps{
	Label: ui.Val("Photo"),
	Style: ui.Style{AspectRatio: 1, Match: key},
	OnPress: func() {
		nav.Push(func() ui.Node { return photo(id) }, ui.ScreenOptions{Title: ui.Val("Photo"), ZoomFrom: key})
	},
})
```

On iOS this is the system's zoom transition (iOS 18 and later; before
that, the screen pushes as usual). Android does its own zoom, and
popping with the predictive back gesture goes back into the view. The
desktops and the web push and pop as usual.
[`examples/gallery`](../examples/gallery)'s photo grid does this.

## Back

Each platform goes back its own way, and Asuka leaves it so:

- **iOS**: the bar's back button, and a swipe from the screen's leading
  edge. `NoBackGesture` turns the swipe off. The back button still pops.
- **Android**: the system back, by the button or the gesture, and the
  up arrow in the top app bar. Back pops the innermost stack on screen
  that has a screen to go back to: in tabs, the shown tab's stack. With
  the predictive back gesture, the top screen shrinks and follows the
  finger, showing the one below. When no stack has a screen to pop,
  back does what it does in any app, and leaves it.
  `NoBackGesture` turns off the predictive animation, but back still
  pops the screen.
- **macOS**: the toolbar's back button, and View ▸ Back (⌘[), which is
  off while there's no screen to go back to. Escape dismisses a sheet.
- **Windows**: the header bar's back button, and Alt+Left, which pops
  the innermost stack of the window it's pressed in. Escape dismisses a
  sheet.
- **Linux**: libadwaita's: the header bar's back button, Alt+Left,
  Escape, and a swipe on a touchpad or touch screen. `NoBackGesture`
  turns the swipe off; the back button still pops.
- **The web**: the header's back button, the browser's back button (and
  a phone's back gesture), Escape, Alt+Left (⌘[ on a Mac), and a swipe
  from the leading edge on a touch screen. See [the web](web.md).

`NoBackGesture` changes nothing on macOS and Windows, which have no
back gesture. Whichever way the user goes back, the screen is disposed,
as with `nav.Pop()`. Back closes an open sheet or dialog first.

### Taking back yourself

`ui.OnBack` lets a component decide, when the user goes back, whether
to go: to ask before throwing away a draft, or to close something of
the screen's own first (a search, an expanded panel). The handler
returns true when it took the back; when every handler returns false,
going back goes on.

```go
func editor() ui.Node {
	dirty := signal.New(false)
	nav := ui.UseNavigator()
	ui.OnBack(func() bool {
		if !dirty.Peek() {
			return false // nothing to lose: the screen pops
		}
		ui.Alert(ui.AlertProps{
			Title: "Discard changes?",
			Actions: []ui.Action{
				{Title: ui.Val("Keep editing"), Role: ui.ActionCancel},
				{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() {
					dirty.Set(false)
					nav.Pop()
				}},
			},
		})
		return true
	})
	// ...
}
```

It belongs to where the component is built, and goes with it:

- **A pushed screen**: its back button, Android's back, the edge swipe
  on iOS, ⌘[ on a Mac, Alt+Left on Windows and Linux, the browser's
  back. While the screen has a handler, iOS's and Linux's swipe is off
  on it (the swipe can't be asked, only stopped, as with UIKit's own
  apps) and Android doesn't preview the pop. iOS's back button is the
  screen's `backAction`.
- **A sheet**: a swipe down, a tap outside, Android's back, Escape on a
  desktop or in a browser, a sheet window's close button on Windows. A
  full-screen sheet has only Android's back and Escape.
  While it has a handler, the sheet can't be swiped away: trying asks
  the handler (iOS's `isModalInPresentation`; a sheet that isn't
  cancelable on Android). When the handler lets it go, the sheet closes
  and `OnDismiss` runs, as if the user had dismissed it.
- **Elsewhere** (outside any screen or sheet, or on a stack's first
  screen, which back leaves): Android's back with no screen to pop,
  where the app would otherwise close, and the browser's back, where
  the page would go back. iOS and the desktops have no such back.

The latest handler is asked first. `nav.Pop()` and closing a sheet from
code don't ask: only the user's going back does.

## Split views

`ui.SplitView` is an app's top level when it has many destinations:
a sidebar of them, and the chosen one's screens beside it (SwiftUI's
`NavigationSplitView`).

```go
ui.SplitView(ui.SplitViewProps{Title: ui.Val("Mail"), Sections: []ui.SidebarSection{
	{Destinations: []ui.Destination{
		{Title: ui.Val("Inbox"), Icon: ui.IconMail, Content: inbox},
		{Title: ui.Val("Sent"), Icon: "paperplane", Content: sent},
	}},
	{Title: ui.Val("Folders"), Destinations: folders},
}})
```

Each destination's `Content` is the first screen of a stack: it pushes
with `ui.UseNavigator`, and choosing another destination replaces the
stack's screens with its first, at once, disposing them. `Selected` is
the chosen destination, counted across the sections; the user's choice
writes it, and writing it shows another.

| | Compact (a phone) | Wide |
|---|---|---|
| iOS | the split view collapsed into a stack, as iOS does it: the sidebar is the first screen, an inset grouped list; a row pushes its destination, and back returns to the list | `UISplitViewController`'s sidebar beside the content (Liquid Glass on iOS 26), which the user can hide |
| Android | Material's modal navigation drawer, opened by the menu button of the destination's first screen (or a swipe from the edge), closed by choosing or by back | a standard drawer, standing open beside the content (840dp and wider) |
| macOS | | `NSSplitViewController`, its sidebar a source list the user can collapse |
| Windows | | WinUI's `NavigationView` pane: the destinations beside the content, the chosen one with an accent pill |
| Linux | under 500 points wide, libadwaita's collapsed split view: the sidebar, then the chosen destination's screens, with a back button to the sidebar | `AdwNavigationSplitView`: a sidebar page of its own, its title in its own header bar, beside the content under the window's |
| The web | a drawer, opened from the detail's header | a sidebar beside the detail |

A destination's `Badge` is shown at its row's trailing end, as Mail
counts a mailbox's unread messages (SwiftUI's `badge` on a sidebar row):
usually a count, though any short text works, and empty or nil for none.
It's an accessor, so it can follow your data; the row changes in place,
keeping the sidebar's selection, scroll and focus.

```go
{Title: ui.Val("Inbox"), Icon: ui.IconMail, Content: inbox,
	Badge: signal.Func(func() string { return strconv.Itoa(unread.Get()) })},
```

| | A destination's badge |
|---|---|
| iOS | a label accessory at the cell's trailing end, in the secondary color |
| Android | a counter at the drawer item's end, as Gmail's |
| macOS | the source list's count, in the secondary color, as Mail's mailboxes |
| Windows | Fluent's `InfoBadge` at the pane item's trailing end |
| Linux | a dimmed count at the row's end |
| The web | a count at the row's end |

Screen readers say it with the row's title, a count as so many items:
"Inbox, 3 items" (Android: Material's badge words, "3 new
notifications").

## Tabs

`ui.Tabs` shows one page at a time, with a tab bar:

- **iOS**: a `UITabBarController`.
- **Android**: Material's bottom navigation bar.
- **macOS**: an `NSTabView`, its tabs on top.
- **Windows**: a bar of tabs on top, as WinUI's top `NavigationView`: an
  icon and a title each, an accent pill under the selected one; the
  arrow keys move between them.
- **Linux**: an `AdwViewStack`, its view switcher in the header bar on a
  wide window and a bar at the bottom of a narrow one (under 550
  points), as GNOME's apps have it.
- **The web**: a tab bar at the bottom.

Each `ui.Tab` has a `Title`, an `Icon` and its `Content`.
`Selected` is the shown tab's index. Choosing a tab writes it, and
writing it switches tabs.

```go
tab := signal.New(0)
return ui.Tabs(ui.TabsProps{Selected: tab},
	ui.Tab{Title: ui.Val("Notes"), Icon: ui.IconList, Content: func() ui.Node {
		return ui.Stack(Notes, ui.ScreenOptions{Title: ui.Val("Notes")})
	}},
	ui.Tab{Title: ui.Val("Settings"), Icon: ui.IconSettings, Content: func() ui.Node {
		return ui.Stack(Settings, ui.ScreenOptions{Title: ui.Val("Settings")})
	}},
)
```

A tab's `Badge` is shown on it (SwiftUI's `badge` on a tab item,
Compose's `BadgedBox` on a `NavigationBarItem`): usually a count, "3",
though any short text works, and empty or nil for none. It's an
accessor: as it changes, the tab alone changes.

```go
ui.Tab{Title: ui.Val("Inbox"), Icon: ui.IconMail, Content: inbox,
	Badge: signal.Func(func() string {
		if n := unread.Get(); n > 0 {
			return strconv.Itoa(n)
		}
		return ""
	})},
```

| | A tab's badge |
|---|---|
| iOS | `UITabBarItem`'s `badgeValue`, the system's red badge |
| Android | the item's `BadgeDrawable`: a count as its number (999+ past 999), other text as its text |
| macOS | in the tab's label, "Inbox (3)": `NSTabView` draws its tabs' labels alone |
| Windows | Fluent's `InfoBadge` at the tab's trailing end |
| Linux | a count as the page's `badge-number`, beside its icon in the view switcher; other text as `needs-attention`'s dot |
| The web | a red badge at the tab's icon |

Screen readers say it with the tab's title, a count as so many items:
"Inbox, 3 items" (the platform's own words on iOS and Android).

Give each tab its own `Stack` so that it has a bar and keeps its own
history, as the platforms' apps do. Every tab's content is built when
the `Tabs` is, and kept while you switch between them. Tab icons are
symbols, like a button's: see [Symbols and icons](components.md#symbols-and-icons).
On Android an icon must be one that Android has.

## Sheets

`ui.Sheet` presents content in a modal sheet while `Open` is true:

- **iOS**: a sheet presentation with detents.
- **Android**: a `BottomSheetDialog`.
- **macOS**: a sheet window, attached to the window in use.
- **Windows**: a modal window over the window in use, which is disabled
  meanwhile.
- **Linux**: an `AdwDialog`: a floating dialog, or a bottom sheet in a
  narrow window.
- **The web**: a modal `<dialog>`, a bottom sheet on a narrow window and
  a centered card on a wide one.

The node it returns takes no space, so put it anywhere in the screen
that opens it.

```go
open := signal.New(false)
note := signal.New("")
return ui.Column(nil,
	ui.Button(ui.ButtonProps{Title: ui.Val("Add a note"), OnPress: func() { open.Set(true) }}),
	ui.Sheet(ui.SheetProps{
		Open:    open,
		Detents: ui.DetentMedium | ui.DetentLarge,
		Grabber: true,
		Content: func() ui.Node {
			draft := signal.New(note.Peek())
			return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
				ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Write a note")}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
					note.Set(draft.Peek())
					open.Set(false)
				}}),
			)
		},
		OnDismiss: func() { fmt.Println("dismissed, not saved") },
	}),
)
```

- `Detents` are the heights it can rest at: `ui.DetentMedium` (half the
  screen), `ui.DetentLarge` (all of it), or both, which starts at
  medium and lets the user drag it up. With none, it's large. When the
  keyboard shows, a sheet that can be large goes large. On a desktop a
  medium-only sheet is half the window's height, and any other most of
  it; the user resizes a sheet window on a Mac.
- `Grabber` shows the handle at its top (iOS, Android, and the web's
  bottom sheet).
- `Content` is built when it opens and disposed when it closes, so state
  made in it starts fresh each time.

### Full-screen sheets

`FullScreen` covers the whole screen instead, for a task that takes the
user's attention until it's done or given up: composing a message,
signing in, editing a photo (SwiftUI's `fullScreenCover`, Material's
full-screen dialog). `Detents` and `Grabber` don't apply, and nothing
outside it can be tapped or swiped away: it closes when `Open` goes
false, so give it its own Cancel and Done. Android's back, and Escape on
a desktop or in a browser, dismiss it as they do a sheet, unless
`ui.OnBack` keeps it (a draft to discard first).

```go
ui.Sheet(ui.SheetProps{Open: composing, FullScreen: true, Content: func() ui.Node {
	draft := signal.New("")
	ui.OnBack(func() bool { return draft.Peek() != "" }) // back doesn't lose a draft
	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Row(ui.Style{Align: ui.AlignCenter},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), Variant: ui.ButtonPlain, OnPress: func() { composing.Set(false) }}),
			ui.StaticText("New Message", ui.Style{Grow: 1, TextAlign: ui.TextCenter}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Send"), Variant: ui.ButtonFilled, OnPress: send})),
		ui.TextField(ui.TextFieldProps{Value: draft, Multiline: true, Style: ui.Style{Grow: 1}}),
	)
}})
```

Its environment is the screen's own: on a phone its whole size, and the
safe area of the status bar, the notch, the home indicator or
navigation bar, and the keyboard, so `SafeArea` keeps its content clear
of them as it does the root's.

| | What it is |
|---|---|
| iOS | the controller presented `.fullScreen`: it slides up over everything, the screen under it gone until it closes |
| Android | a full-screen `Dialog` in a theme of its own, edge to edge (the status and navigation bars' insets, the cutout, the keyboard), sliding up with Material's emphasized easing; predictive back shrinks it toward the edge it's swiped from, its corners rounding, unless `OnBack` intercepts |
| macOS | a sheet as big as all of its window's content under the title bar and toolbar, following the window as it resizes, which the user doesn't resize: a Mac has no full-screen modal, and a sheet is how AppKit gives a window over to one task |
| Windows | a borderless window over all of its window's content (the title bar stays the window's, which is disabled meanwhile), following it as it moves and resizes, its bottom corners rounded as Windows 11 rounds the window's: Fluent's full-size `ContentDialog` |
| Linux | a floating `AdwDialog` as big as its window, which libadwaita shows over all of it, as it shows a dialog that doesn't fit (on a phone, in a narrow window); it follows the window as it resizes |
| The web | a modal `<dialog>` over the whole page (focus stays in it; Escape and the browser's back dismiss it), or over a Window panel's content for a sheet declared in one; it slides up |

The user dismisses a sheet by swiping it down, on Android with back
or a tap outside it, and on a desktop or in a browser with Escape (on
Windows, its window's close button too; on the web, a click outside).
That sets `Open` to false and calls `OnDismiss`.
Setting `Open` to false from code closes it without calling
`OnDismiss`. So `OnDismiss` means "closed without your say", such as a
draft left unsaved.

## Popovers

`ui.Popover` shows content in a bubble pointing at a view, over the
screen, while `Open` is true: a few options, a detail, a small form.
Its children are what it points at:

```go
open := signal.New(false)
return ui.Popover(ui.PopoverProps{Open: open, MaxWidth: 280, Content: func() ui.Node {
	return ui.Column(ui.Style{Padding: ui.All(16), Gap: 8},
		ui.StaticText("Shared with 3 people", ui.Style{Font: fonts.Headline}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Manage"), OnPress: manage}))
}}, ui.Button(ui.ButtonProps{Icon: "person.2", Label: ui.Val("Sharing"), OnPress: func() { open.Set(true) }}))
```

It's as big as its content, no wider than `MaxWidth` (320 by default),
text wrapping to it, and no taller than most of the screen. A tap
outside dismisses it, setting `Open` false and calling `OnDismiss`, as a
sheet's; `ui.OnBack` in it can keep it.

| | The view | Where |
|---|---|---|
| iOS | `UIPopoverPresentationController`, a popover on iPhone too (not adapted into a sheet), Liquid Glass on iOS 26 | where UIKit fits it, its arrow on the anchor |
| Android | a `PopupWindow` on Material's container surface, 16dp corners, elevated | below the anchor, centered (above it without room) |
| macOS | `NSPopover`, transient | below the anchor, where AppKit fits it |
| Windows | a popup window, light dismissed, Windows 11's corners (WinUI's Flyout) | below the anchor, centered (above it without room) |
| Linux | a `GtkPopover`; Escape closes it too | below the anchor, where GTK finds room |
| The web | a popover | next to the anchor |

## Alerts and action sheets

`ui.Alert` shows an alert now:

- **iOS**: a `UIAlertController`.
- **Android**: a Material dialog.
- **macOS**: an `NSAlert`, shown as a sheet over the window in use.
- **Windows**: a task dialog over the window in use.
- **Linux**: an `AdwAlertDialog`.
- **The web**: a modal `<dialog>`.

Pressing a button closes the alert and runs that button's `OnPress`.
With no `Actions`, it has a single OK.

```go
ui.Alert(ui.AlertProps{
	Title:   "Delete this note?",
	Message: "You can't undo this.",
	Actions: []ui.Action{
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
		{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: remove},
	},
})
```

An action's `Role` says what it is:

- `ui.ActionDefault` is an ordinary button.
- `ui.ActionCancel` is the way out. iOS puts it in its own place, and
  Android makes it the dialog's negative button. On a desktop, Escape
  presses it: on a Mac it's the last button, after the default one.
- `ui.ActionDestructive` is red: in iOS's style, in Material's error
  color on Android, AppKit's destructive button on a Mac, and
  libadwaita's destructive response on Linux.

On Android, the last other button is the dialog's positive button, at
the end, and a second one is neutral. With more than two buttons other
than Cancel, the dialog shows them as a list. On a Mac the first button
other than Cancel is the default, which Return presses; on Linux the
first `ui.ActionDefault` one is, in the accent color.

`ActionSheet` shows the actions as an action sheet, a list of choices
coming up from the bottom on iOS:

```go
ui.Alert(ui.AlertProps{
	Title:       "Photo",
	ActionSheet: true,
	Actions: []ui.Action{
		{Title: ui.Val("Take a photo"), OnPress: func() {}},
		{Title: ui.Val("Choose from library"), OnPress: func() {}},
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
	},
})
```

On Android an action sheet is a dialog with the actions as a list, and
its `Message` isn't shown. On Windows its buttons are a task dialog's
command links; on Linux it's an alert dialog with no default button; on
a Mac it's an alert as any other; on the web, a dialog whose buttons are
a list. An alert closed some other way, such as back or a tap outside it
on Android, runs no button's `OnPress`.

### Text fields

An alert can ask for text, a name or a password, as SwiftUI's alert
with a `TextField` and a `SecureField` does. Its `Fields` show under
the message. Each field's `Text` starts with its value and is written
as the user types, so an action's `Disabled` can follow it: here,
Sign In stays disabled until both fields are filled in, and the alert
updates the button while it's up.

```go
user, pass := signal.New(""), signal.New("")
ui.Alert(ui.AlertProps{
	Title: "Sign in",
	Fields: []ui.AlertField{
		{Text: user, Placeholder: "Name", ContentType: ui.ContentUsername},
		{Text: pass, Placeholder: "Password", Secure: true, ContentType: ui.ContentPassword},
	},
	Actions: []ui.Action{
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
		{Title: ui.Val("Sign In"), OnPress: signIn,
			Disabled: signal.Func(func() bool { return user.Get() == "" || pass.Get() == "" })},
	},
})
```

A field also takes a `Keyboard` and a `ContentType` for AutoFill, as
`TextFieldProps` does. `Secure` hides what's typed. The first field has
the focus, with the keyboard up. Return in a field moves to the next
one. In the last field it presses the default action, which is the
first `ui.ActionDefault` one (UIAlertController's `preferredAction`),
but only while that action is enabled. Writing a field's `Text` while
the alert is up doesn't change what the field shows. An action sheet
shows no fields.

| Platform | The fields |
| --- | --- |
| iOS | `UIAlertController`'s text fields; the default action is its `preferredAction`, in bold |
| Android | outlined `TextInputLayout`s in the Material dialog (a password's with its toggle); the IME's action goes to the next field, and from the last presses the default action's button |
| macOS | `NSTextField`s and an `NSSecureTextField` as the `NSAlert`'s accessory view; Tab moves between them |
| Windows | a dialog of the host's own, laid out as WinUI's `ContentDialog` (a task dialog can't hold fields): the title, the message, Fluent text boxes, then the buttons with the default one in the accent color; Tab, Enter and Escape work as in a dialog, and Narrator reads each field by its placeholder |
| Linux | a boxed list of `AdwEntryRow`s and `AdwPasswordEntryRow`s, the `AdwAlertDialog`'s extra child |
| The web | inputs in the `<dialog>`, labeled by their placeholders, with `type=password`, `inputmode` and `autocomplete` |

## Snackbars

`ui.Snackbar` shows a brief message at the bottom of the window now,
with an optional action such as Undo, and returns a func that dismisses
it:

- **iOS**: UIKit has none, so the host draws one as iOS draws its own
  brief messages: a capsule in the system material (Liquid Glass on iOS
  26), centered above the tab bar or toolbar and the keyboard, on the
  topmost presented content. It springs in and out (it fades with Reduce
  Motion) and a swipe down dismisses it.
- **Android**: Material's `Snackbar`, which a swipe dismisses, above the
  screen's bottom navigation bar or floating action button (its anchor)
  and the keyboard; over a sheet, at the bottom of the sheet.
- **macOS**: an overlay at the bottom of the window in an
  `NSVisualEffectView`, with a close button.
- **Windows**: an overlay at the bottom of the window drawn as Fluent's
  InfoBar, with a close button.
- **Linux**: an `AdwToast` in the window's `AdwToastOverlay`.
- **The web**: an element with role `status` at the bottom of the
  window (a popover, in the top layer), with a close button; a swipe
  down dismisses it.

```go
ui.Snackbar(ui.SnackbarProps{
	Message: "Note deleted",
	Action:  &ui.Action{Title: ui.Val("Undo"), OnPress: restore},
	OnDismiss: func() { deleteForGood() },
})
```

`Message` is shown once, so it's a string, as an alert's is. The
action's `Title` follows the app's language while it shows; its
`Role`, `Icon`, `Disabled` and `Children` aren't shown. Pressing it
dismisses the snackbar and runs its `OnPress`. `OnDismiss` runs when it
goes any other way: its time ran out, the user swiped or closed it,
another snackbar replaced it, or the func `ui.Snackbar` returned was
called. So after an Undo, `OnDismiss` doesn't run: it's where what the
action could undo is done for good.

`Duration` is how long it shows: `ui.SnackbarShort` (the default, about
four seconds, Material's), `ui.SnackbarLong` (about ten) or
`ui.SnackbarIndefinite` (until it's dismissed or its action pressed).
Hosts lengthen it as the platform recommends for accessibility: Android
by `AccessibilityManager.getRecommendedTimeoutMillis`, which keeps one
with an action longer while TalkBack is on; on iOS, macOS and Linux,
which have no recommended timeout, one with an action stays until it's
dismissed while a screen reader is on, as Material does without one;
Windows also takes its "Dismiss notifications after" setting. On the
desktops and the web its time waits while the mouse is over it or the
keyboard's focus is in it. Screen readers announce it as it shows and
reach its buttons.

Only one shows at a time: a new snackbar replaces the one showing, as
Android's does (the old one's `OnDismiss` runs first). On a desktop it
goes on the window an alert would: that of the view whose event shows
it, else of the component calling it, else the main window.

## Menus

A menu shows a list of `ui.Action`s, the same type as alert buttons and
bar buttons, with an optional `Icon` beside each title. There are two:

- A `ui.Button` with `Menu` opens it when tapped.
- `ui.Menu` puts one on any content: on a tap, or with `LongPress` as a
  context menu.

On iOS it's a `UIMenu`, and on Android a `PopupMenu`. On a Mac it's an
`NSMenu`, on Windows a popup menu (a destructive item red, as on a Mac),
on Linux a `GtkPopoverMenu`, and on the web a popover. On the desktops
and the web a context menu opens with a right-click (a secondary
click), and on Windows, Linux and the web with a long press too. Both are in
[Components](components.md#menus). Rows of a `List` have swipe actions
too: see [Lists](components.md#lists).

## Windows

A desktop app has a main window, which holds what `app.Run` shows. To
give it a title, a size to open at and a least size, start the app with
`app.RunWindow`:

```go
func main() {
	app.RunWindow(ui.WindowOptions{Width: 420, Height: 640, MinWidth: 320, MinHeight: 360}, App)
}
```

Sizes are in points, of the window's content. With none, the main window
opens at 800 by 600 points, centered, and the user can't make it smaller
than 320 by 320. `Title` is the app's name when it's empty; it's an
accessor, so it can follow the app's language. The window comes back
where the user left it (see [below](#where-windows-were)), which wins
over `Width`, `Height`, `X` and `Y`. iOS and Android ignore the options.

`ui.Window` opens another window while `Open` is true, as `ui.Sheet`
opens a sheet:

```go
stats := signal.New(false)
return ui.Column(nil,
	ui.Button(ui.ButtonProps{Title: ui.Val("Statistics"), OnPress: func() { stats.Set(true) }}),
	ui.Window(ui.WindowProps{
		Open:  stats,
		Title: ui.Val("Statistics"),
		Width: 280, Height: 140,
		Content: func() ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprintf("%d notes", len(notes.Get())) }))
		},
	}),
)
```

- `Content` is built when the window opens and disposed when it closes.
  `ui.UseEnvironment` in it is the window's: its size follows the user
  resizing it, and its scale is its display's.
- The user closing it sets `Open` to false and calls `OnClose`. Setting
  `Open` to false from code closes it without calling `OnClose`.
- It opens at 800 by 600 points unless it says otherwise, cascaded
  from the window in use on a Mac and on Windows. On Linux the desktop
  places it, as GTK 4 leaves to it (GNOME and KDE put a new window where
  it covers the fewest others). Stacks in it have their own bar, as in the main
  window. Sheets and alerts open over the window the user is working in.

| | A window |
|---|---|
| macOS | an `NSWindow`, with its own toolbar and title (its innermost stack's) |
| Windows | a top-level window, with its own title and taskbar button |
| Linux | an `AdwWindow`, with its own header bar |
| The web | a panel in the page, which the user can move and resize |

A phone has no windows: opening one on iOS or Android panics. A
component that offers one checks `ui.Platform()` first,
or opens it only from what only a desktop has, as the menu bar below.

### Options

`WindowOptions` (the main window's) and `WindowProps` (a `Window`'s)
take the same options for how a window opens:

| Option | |
|---|---|
| `Width`, `Height` | its content's size, in points |
| `MinWidth`, `MinHeight`, `MaxWidth`, `MaxHeight` | the least and most the user can size it to; 0: the host's least, no most |
| `X`, `Y` | its frame's top left on the screen, in points from the main display's top left; with both 0 the main window is centered and a `Window` cascades (`Center` centers it) |
| `DisableResize`, `DisableMinimize`, `DisableMaximize`, `DisableClose` | take the user's abilities away: resizing it, and its buttons |
| `AlwaysOnTop` | keeps it above other apps' windows |
| `TitleBar` | `ui.TitleBarHidden` lays the content out under the title bar, to the window's top edge, the window buttons over it |
| `Background`, `Transparent` | the window's color behind its content, or none, so what's behind it shows where the content draws nothing |
| `Hidden` | opens it without showing it: `Show` shows it |
| `SkipTaskbar` | leaves it out of the taskbar (Windows) and the Window menu (macOS) |
| `Maximized`, `Fullscreen` | opens it so |
| `StateKey` | where its place is kept, see [below](#where-windows-were) |
| `FitContent` | sizes it to its content, as that changes, within its least and most size ([The desktop](desktop.md#windows-sized-to-their-content)) |
| `Panel` | a launcher's or a palette's window: floating over every app, taking the keyboard without bringing the app forward, closed by Escape or a click elsewhere ([Panels](desktop.md#panels-and-launchers)) |
| `ClickThrough` | lets the mouse go through it to what's behind ([Click-through](desktop.md#click-through-windows)) |
| `Flyout` | a `Window`'s: hangs it from a tray icon or a rectangle of its parent ([Flyouts](desktop.md#flyouts)) |
| `Child`, `Modal` | a `Window`'s: see [below](#child-and-modal-windows) |
| `MenuBar`, `Edited`, `File` | a `Window`'s that shows a document: the app's menu bar in it (Windows, Linux), the dot in a Mac's close button or a `*` before the title, the file's icon in a Mac's title bar; [`asuka/sdk/document`](desktop.md#documents) sets them |

With a hidden title bar the content goes to the top edge, under the
window's buttons (a Mac's traffic lights, Windows' caption buttons, the
header bar's on Linux), and the window's environment has the title
bar's height as `SafeTop`: a `ui.SafeArea` keeps clear of the buttons,
and a background, a sidebar or a picture can go under them:

```go
app.RunWindow(ui.WindowOptions{TitleBar: ui.TitleBarHidden, Background: colors.RGB(0x1e, 0x1e, 0x1e)}, func() ui.Node {
	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16)}, editor())
})
```

The strip under the buttons still drags the window, where the content
takes no clicks. On Windows the main window has no menu bar under a
hidden title bar; its shortcuts still work.

A window that shows only once it's ready opens `Hidden` and shows from
code: its content is built, laid out and drawn meanwhile, so it shows
whole, never empty. An app that starts in the background (a tray icon's)
starts its main window hidden and shows it from its menu.

### Controlling a window

`ui.UseWindow()` is the handle of the window a component is in: the
`Window` whose content it's in, or the main window. `ui.MainWindow()` is
the main window's from anywhere on the main thread (a tray icon's menu,
a global shortcut). A `Window`'s handle from outside it is one the app
makes (`ui.NewWindowHandle()`) and gives it as `Handle`.

```go
w := ui.UseWindow()
ui.Button(ui.ButtonProps{Title: ui.Val("Full Screen"), OnPress: func() { w.SetFullscreen(true) }})
ui.Text(signal.Func(func() string {
	if w.Focused().Get() {
		return "Typing here"
	}
	return "In the background"
}))
```

- `Show` and `Hide` show and hide it; hidden, it stays open, its content
  living on. `Focus` shows it if it's hidden or minimized, brings it to
  the front with the keyboard in it, and brings the app to the front:
  how a tray icon's menu brings a window back (Windows lets an app come
  to the front after the user's input to it, as a click on its tray icon
  is). On a closed `Window`, `Show` and `Focus` open it.
- `Minimize`, `Maximize` and `Restore` (from either); `SetFullscreen`.
- `SetSize` (its content's, in points), `SetPosition` (its frame's top
  left, from the main display's top left), `Center`.
- `SetAlwaysOnTop` keeps it above other apps' windows, or not.
- `SetClickThrough` lets the mouse go through it, or not.
- `Close` closes it as the user does, asking first (below); `Destroy`
  closes it without asking. The main window closing quits the app.
- Its state is signals that follow the user's changes as well as the
  app's: `Visible`, `Focused`, `Minimized`, `Maximized`, `Fullscreen`,
  `AlwaysOnTop`, `ClickThrough`, `Position` and `Size`. Read them in an
  effect or a `StyleFunc`. A minimized window, or one behind others, is
  still visible.

The methods act on the window at the flush, as signal writes do. On a
phone they do nothing, and the state reads visible and focused.

### Asking before a window closes

`OnCloseRequest` is asked when the user closes the window (its close
button, ⌘W, Alt+F4, the Dock's or taskbar's menu) or the app calls
`Close`: returning false keeps it open, to ask about unsaved changes
first. Each component that has something to lose asks for itself, while
it's mounted:

```go
w := ui.UseWindow()
w.OnCloseRequest(func() bool {
	if !doc.Dirty() {
		return true
	}
	ui.Alert(ui.AlertProps{Title: "Save changes?", Actions: []ui.Action{
		{Title: ui.Val("Save"), OnPress: func() { doc.Save(); w.Destroy() }},
		{Title: ui.Val("Don't Save"), Role: ui.ActionDestructive, OnPress: w.Destroy},
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
	}})
	return false
})
```

Every function asked must agree; `WindowProps.OnCloseRequest` and
`WindowOptions.OnCloseRequest` are asked too. `Destroy`, once the user
has chosen, closes it without asking again. On the web the main window
is the tab: leaving the page or closing the tab asks too, and a refusal
keeps the page behind the browser's own "Leave site?" question (a page
can't word it). Quitting the app (⌘Q) doesn't ask the windows.

### Where windows were

Windows come back where the user left them, across launches: the main
window always (under the key `"main"`, or its `StateKey`), a `Window`
when it has a `StateKey`. Give each kind of window its own
(`"inspector"`, `"preferences"`). Where it was wins over its `Width`,
`Height`, `X` and `Y`, which stay what the first launch opens with.

| | What's kept |
|---|---|
| macOS | its frame, with AppKit's frame autosave (in the app's defaults) |
| Windows | where it was and its size, in the screen's pixels and the DPI they were at, and whether it was maximized; put back whole on a monitor that's there |
| Linux | its size and whether it was maximized: a Wayland app can't place its window |
| The web | a `Window` panel's place and size, and whether it was maximized, in the page's `localStorage` |

The desktop hosts keep theirs in `asuka-window.json` in the app's data
directory (Windows, Linux) or the app's defaults (macOS). Tests and
automation runs keep none.

### Child and modal windows

A `Window` with `Child` is a child of the window it's declared in: the
main window, or the `Window` whose content has it. It stays above that
window, and closes with it (its owner is disposed with that window's
content). `Modal` makes it a child that blocks that window until it
closes:

```go
ui.Window(ui.WindowProps{Open: prefs, Modal: true, Title: ui.Val("Preferences"), Width: 480, Height: 360,
	Content: preferences})
```

| | `Child` | `Modal` |
|---|---|---|
| macOS | a child window, ordered above its parent and moving with it | a sheet of its parent |
| Windows | owned by its parent: above it, minimized with it | owned, its parent disabled while it's open |
| Linux | transient for its parent, destroyed with it | transient and modal |
| The web | a panel as any other | a panel over a backdrop that takes the page's clicks |

### What each desktop can do

| | macOS | Windows | Linux (GTK 4) | The web: the tab | The web: a `Window` |
|---|---|---|---|---|---|
| Show, hide, focus | yes | yes | yes | focus only | yes |
| Minimize | yes | yes | yes (Wayland never reports it back) | no | to its title bar |
| Maximize, restore | zoom | yes | yes | no | over the app's area |
| Full screen | yes | yes | yes | the Fullscreen API | the Fullscreen API |
| Size, least size | yes | yes | yes | no | yes |
| Most size | yes | yes | no: GTK 4 has none | no | yes |
| Position, center | yes | yes | X11; Wayland: no, the compositor places windows | no (reads the browser window's) | yes |
| Always on top | yes | yes | X11 | no | above the other panels |
| Hidden title bar | yes | yes | yes | no | yes |
| Background, transparent | yes | background; transparent falls back to it | yes (transparent with a compositor) | background | yes |
| Skip the taskbar | the Window menu | yes | X11 | no | no |
| Panel, flyout | yes | yes | X11: kept above, placed; Wayland: no place, nothing above | no | yes |
| Fit its content | yes | yes | yes | no | yes |
| Click-through | yes | yes | yes | no | to the page |

## The menu bar and keyboard shortcuts

`ui.MenuBar` gives a desktop app its menus, and their items their
keyboard shortcuts:

```go
ui.MenuBar(signal.Static([]ui.MenuBarMenu{
	{Title: "File", Items: []ui.MenuItem{
		{Title: "New Note", Shortcut: ui.Cmd("n"), OnPress: add},
		ui.MenuSeparator,
		{Title: "Export…", Shortcut: ui.Cmd("e").With(ui.Shift), OnPress: export},
	}},
	{Title: "Edit", Items: []ui.MenuItem{
		{Title: "Clear Completed", Shortcut: ui.Cmd("k").With(ui.Shift), Destructive: true, OnPress: clear},
	}},
}))
```

- A `ui.MenuItem` has a `Title`, a `Shortcut` and its `OnPress`;
  `Disabled`, `Checked` and `Destructive` (in red) change how it shows.
  `ui.MenuSeparator` is a line between groups of items.
- A `ui.Shortcut` is a `Key` and its `Mods`. `ui.Cmd(key)` is the key
  with the platform's command key: ⌘ on a Mac, Ctrl elsewhere.
  `.With(ui.Shift)`, `ui.Alt` (a Mac's Option) and `ui.Control` add
  modifiers. A key is a character (`"n"`, `","`, `"="`) or a name:
  `enter`, `escape`, `tab`, `space`, `delete`, `backspace`, `up`, `down`,
  `left`, `right`, `home`, `end`, `pageup`, `pagedown`, `f1` to `f12`.
- The menus follow their accessor: an item turned off or checked, a menu
  that comes and goes. While several `MenuBar`s are mounted, the last one
  shows; when it's disposed, the one before does. The node it returns
  takes no space; put it anywhere.

Each desktop shows them its own way:

- **macOS**: the menu bar, beside the Mac's own menus: the app's menu
  (About, Hide, Quit), File, Edit (Undo, Cut, Copy, Paste...), View
  (Back, Enter Full Screen) and Window. A menu titled File, Edit, View,
  Window or Help adds its items to that one; the others go between View
  and Window.
- **Windows**: a menu bar on the main window, its shortcuts working in
  every window of the app. Each menu and item gets an access key
  (Alt+F), and each item its shortcut after its title ("Ctrl+Shift+K").
- **Linux**: GNOME's apps have no menu bar, but a main menu: the header
  bar's last button. Each of the app's menus is a submenu of it (one
  menu's items are the main menu's own), and the shortcuts work in
  every window.
- **The web**: the shortcuts work in the page; it shows no menu bar.

iOS and Android ignore `MenuBar`, so an app can have one wherever it
runs. Its items are commands, so keep each reachable from the
interface too, for a phone.

## Toasts

Asuka has no toast component, since iOS and Android have no standard one
that apps share. Make one from a view placed over the screen,
shown with `ui.Show` and brought in with a
[transition](motion.md#enter-and-exit). The gallery's toast does this, and
calls `ui.Announce` so that screen readers say it too. See
[`examples/gallery`](../examples/gallery).

## Routes

With a `ui.Router`, every screen has an address, an `ui.Href` such as
`/inbox/18` or `/search?q=go`, as a web page has. The app declares its
routes once, path patterns mapped to screens under the navigators that
show them, and navigates by address as well as with the navigator:

```go
func App() ui.Node {
	return ui.Router(ui.RouterProps{},
		ui.Route{Path: "/", Redirect: "/inbox"},
		ui.Route{Layout: ui.LayoutTabs, Children: []ui.Route{
			{Path: "/inbox", Title: ui.Val("Inbox"), Icon: ui.IconMail, Layout: ui.LayoutStack, Children: []ui.Route{
				{Screen: inbox, Title: ui.Val("Inbox")},
				{Path: ":id", Screen: message, Options: messageOptions},
				{Path: ":id/reply", Screen: reply, Sheet: &ui.SheetRoute{FullScreen: true}},
			}},
			{Path: "/settings", Title: ui.Val("Settings"), Icon: ui.IconSettings, Layout: ui.LayoutStack, Children: []ui.Route{
				{Screen: settings, Title: ui.Val("Settings")},
				{Path: "about", Screen: about, Title: ui.Val("About")},
			}},
		}},
	)
}

func message(p ui.Params) ui.Node {
	id := p.Int("id")
	// ...
	return ui.Button(ui.ButtonProps{Title: ui.Val("Reply"), Href: ui.HrefOf("/inbox/:id/reply", id)})
}
```

What an address does then comes for free: the web's address bar, back
and forward show it, a reload or a pasted link opens it, a link of the
app's scheme or domains goes to it, and a desktop window opens where it
was left. [`examples/navigation`](../examples/navigation) is routed.

### Patterns and parameters

A route's `Path` is relative to its parent's, unless it starts with `/`.
Its segments are matched one by one:

- `chat` matches itself;
- `:id` is a parameter, any one segment: `p.Get("id")`, `p.Int("id")`;
- `*path` is a catch-all, the rest of the path, none or more segments
  (last in a pattern): `p.Get("path")` is `docs/a.md`,
  `p.Segments("path")` `["docs", "a.md"]`;
- `""` is the parent's own path, its index.

When several routes match, the most specific wins, as in expo-router:
segment by segment, a static one before a parameter before a
catch-all. `/files/new` takes `/files/new`; `/files/:id` takes
`/files/3`; `/files/*path` the rest.

A screen gets its `ui.Params`: the path's parameters, then the query's
(`p.Get("q")` of `/search?q=go`). The path's never change for a screen;
its query can (`RouterHandle.SetQuery`, or navigating to the same
screen with another query), and `p.Get` and `p.Query()` in an effect
follow it. `ui.UseParams()` gives them to a component under the screen.

### Navigators and sheets

A route with `Children` is a navigator of them, by its `Layout`, or a
group:

- `ui.LayoutStack`: a `Stack`. Its children are its screens, and sheets.
- `ui.LayoutTabs`: `Tabs`. Each child is a tab (`Title`, `Icon`,
  `Badge`), a screen or a stack of its own.
- `ui.LayoutSplit`: a `SplitView`. Each child is a destination of the
  sidebar, a screen or a stack (the detail's); a group's `Title` makes
  its children a section.
- `ui.LayoutSlot`: one child at a time, filling its place with no bar:
  an app's signed-out and signed-in parts.
- No layout: a group, whose children join its parent's navigator under
  its path and its guard.

A route with a `Sheet` is presented in a sheet over its navigator (its
stack's top screen, the tabs) rather than pushed or chosen: one screen,
or a navigator of its own (a stack, for a sheet of several steps).
Going back dismisses it; so does navigating to a screen of the
navigator under it.

A screen's bar comes from its route's `Options(p)` (`ui.ScreenOptions`,
made from its parameters: a message's sender as its title), else its
`Title`. The router's top level is a slot of the routes given it, with
the not-found route last.

### Navigating

`ui.UseRouter()` (while building a component) gives the `RouterHandle`
of the router it's in; `ui.Navigate(href)` uses the one the calling code
belongs to.

- `Navigate(href)` goes to the screen: the tabs, destinations and slots
  on the way switch to it; in its stack, an earlier screen with the same
  address is gone back to, else it's pushed; a sheet's is presented.
- `Push(href)` always pushes, even a screen whose address is there.
- `Replace(href)` shows the screen in place of the current one (a
  sheet's place too), which is disposed; the history doesn't grow.
- `Back()` goes back as the user's back would (without asking `OnBack`):
  the newest sheet, else the innermost stack on screen. `CanGoBack()`
  says whether it would.
- `Dismiss()` dismisses the newest sheet.
- `SetQuery(q)` changes the shown screen's query, keeping the screen.
- `Href()` is the address shown, an accessor a sidebar or a link can
  follow; `Takes(href)` says whether a route takes an href.

A view or a button with an `Href` goes there when it's pressed (after its
`OnPress`), and `ui.NavLink` is content that does, which screen readers
call a link. In a router, a rich text's link to one of the app's hrefs
(`ui.Link("the inbox", "/inbox")`) goes there too. `RouterProps.Handle`
(`ui.NewRouterHandle()`) navigates a router from outside it: a menu
bar's command, a test.

The navigator keeps working in a routed stack: `UseNavigator().Push`
pushes a screen that has no route. It has its stack's address under it
and a history entry of its own (the browser's back pops it), but a
reload or a link can't bring it back.

Every screen is an owner, as everywhere: a screen a route pops, replaces
or leaves (another tab's screen doesn't) is disposed.

### The screens under a link

A link builds the screens it stands for, so back from it goes somewhere
sensible: in each stack on the way, the stack's `Initial` screen (its
first child unless it names another, as expo-router's
`initialRouteName`), then the stack's screens up the link's path
(`/inbox/:id` under `/inbox/:id/reply`), then the one linked to; the
other tabs at their first screens. Their parameters come from the
link's, by name; one that needs a parameter the link hasn't, or that a
guard keeps the user out of, isn't made.

### Redirects, guards and not found

A route with a `Redirect` sends whoever comes to its path to another
href, with its parameters filled in, replacing it: `{Path: "/",
Redirect: "/inbox"}`, `{Path: "/m/:id", Redirect: "/inbox/:id"}`.

A `Guard` is asked before its screens (and its children's) show: it
returns `""` to let the user in, or where to send them instead. It can
read signals: when what it read changes, the screens shown are asked
again, and those it no longer lets in go (with what's over them in their
stacks), the router going where the guard sends the user. A signed-out
user is sent to the login with where they were going, and back there
after:

```go
ui.Route{Path: "/login", Screen: login, Guard: func(to ui.Href) ui.Href {
	if signedIn.Get() {
		if next := to.Query().Get("next"); next != "" {
			return ui.Href(next) // where they were going
		}
		return "/"
	}
	return ""
}},
ui.Route{Guard: func(to ui.Href) ui.Href { // a group: everything else
	if !signedIn.Get() {
		return ui.Href("/login").With("next", string(to))
	}
	return ""
}, Children: app},
```

An href no route takes shows `RouterProps.NotFound` (a plain screen with
a way home if it's nil), its `path` parameter the href's path. A
catch-all route of the app's own (`/*path`) takes them instead.

### Where it starts, and where it's kept

A router starts at the link the app was opened with, if it takes it;
else where its window was left; else at `RouterProps.Initial` (`/`).
Its whole state (each stack's screens, each tab's, the sheets) is kept
with the address, as JSON, and read back as far as the routes still
match it:

| | Where it's kept |
|---|---|
| iOS, Android | in the app while it runs; not across launches (as a native app's navigation isn't) |
| macOS, Windows, Linux | with where each window was: the main window's always, a `Window`'s with a `StateKey`, in `asuka-routes.json` in the app's data directory; it opens there next time |
| The web | the address bar, and each history entry: back, forward and a reload come back to the screens they stand for (see [the web](web.md#addresses-and-history)) |
| `asuka dev` | every platform: a reload of new code comes back to the screens it showed |

A window's first router has its address; one in a `Window` has that
window's.

### Typed links

Go can't check a string at compile time, so `asuka vet` (which `asuka
dev` and `asuka build` run) does: its `routes` analyzer reports a
constant href no route of the app's takes, an `Href`, a `Navigate`'s or
a `Redirect`'s, which would show the not-found screen, and a
`ui.HrefOf` given more or fewer values than its pattern has
parameters. `ui.HrefOf("/inbox/:id", id)` builds an href from a route's
pattern, escaping what it's given; the pattern is checked too. A link
in a package that sees no routes is checked in the package that declares
them. It says nothing when the routes can't all be known from the code
(a function builds some) or the app has a catch-all of its own.

## Deep links

An app opens for URLs of its own scheme, and for https links to its own
domains, when `asuka.json` lists them:

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "scheme": "notes",
  "links": ["notes.example.com"]
}
```

- `scheme` opens the app for `notes://anything`: on iOS and Android,
  on a Mac (the app's `Info.plist` declares it), and on Linux once the
  app is installed (its desktop file handles the scheme; a link opened
  while the app runs goes to it, as GNOME's apps are one instance each),
  and on Windows (the app registers it for the user as it starts, under
  `HKEY_CURRENT_USER\Software\Classes`, again if it has moved; a link
  opened while the app runs goes to it). An app with an `id` runs once
  on Linux and Windows: launched again, it comes to the front. `asuka
  dev` registers no scheme on Linux or Windows, and its runs don't hand
  themselves to an installed copy of the app: try links with a build.
- `links` opens it for `https://notes.example.com/...`: universal links
  on iOS, App Links on Android. The domain must say so too. It serves
  `apple-app-site-association` for iOS and `assetlinks.json` for
  Android, as each platform's docs describe. The desktops don't open
  for them.

With a [router](#routes), these links need no code: the app's scheme
(`notes://inbox/18`, its host the first segment, as expo-router reads
it, so `/inbox/18`), a universal or app link's path, and a
notification's URL go to their route, at launch or while the app runs.
`ui.OnOpenURL` gets the URLs no route takes.

`ui.OnOpenURL` gets each URL the app is opened with, at launch or while
it runs. The URL that launched the app comes before any handler exists,
so it goes to the first handler. The handler stops with its component's
owner. Route from it with the navigator, or by setting a signal.

```go
ui.OnOpenURL(func(raw string) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	if u.Host == "add" {
		fmt.Println("add", u.Query().Get("text"))
	}
})
```

[`examples/notes`](../examples/notes) adds a note for
`asukanotes://add?text=...`. A notification's URL arrives the same way
when the user taps it, on every platform (see [Device](device.md)). On
the web, a router's addresses are the page's: a link to the app's site
opens its screen, and opening the page doesn't call `OnOpenURL`.

## Shares from other apps

With `"shareTarget": true` in `asuka.json`, the app shows in other apps'
share sheets, for text and links, such as "Share" in a browser or in
YouTube; `{"types": ["image/*", ...]}` takes files too (see
[Device](device.md#shares-from-other-apps-uionshare)). `ui.OnShare` gets
what's shared. A share that launched the app goes to the first handler,
as with `OnOpenURL`.

```go
ui.OnShare(func(items []ui.SharedItem) {
	for _, it := range items {
		fmt.Println("shared:", it.Kind, it.Text, it.Path)
	}
})
```

On iOS the app comes with a share extension, which opens the app with
what's shared, as Android opens it: the extension shows no screen of
its own. What's shared is often a title and a link together (a page's
title, a text, and its URL, a link, from Safari).
[`examples/downloader`](../examples/downloader) looks for the link. The
desktops and the web aren't share targets: `OnShare` is never called
there.

To show a sheet of your app's over the app that shares, without opening
yours, see `app.ShareSheet` in
[Device](device.md#the-apps-own-share-sheet-appsharesheet). To share
from your app instead, `ui.Share(text)` shows the system's share sheet,
and `ui.ShareFiles` shares files: the share menu on a Mac, the share UI
on Windows, the browser's on the web. Linux has no share sheet: they
copy what's shared to the clipboard, and say so in a toast.

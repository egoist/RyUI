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

This page covers them, along with going back, and the links and shares
that open the app. [`examples/navigation`](../examples/navigation) has
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

Each platform goes back its own way, and RyUI leaves it so:

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
  desktop or in a browser, a sheet window's close button on Windows.
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
		ui.StaticText("Shared with 3 people", ui.Style{Font: ui.Headline}),
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
opens at 800 by 600 points, and the user can't make it smaller than 320
by 320. `Title` is the app's name when it's empty; it's an accessor, so
it can follow the app's language. The window comes back at the size the
user left it, which wins over `Width` and `Height`: on a Mac its frame,
on Windows where it was and whether it was maximized, on Linux its size
and whether it was maximized (a Wayland app can't place its window).
iOS and Android ignore the options.

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
component that offers one checks `ui.UseEnvironment().Platform` first,
or opens it only from what only a desktop has, as the menu bar below.

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

RyUI has no toast component, since iOS and Android have no standard one
that apps share. Make one from a view placed over the screen,
shown with `ui.Show` and brought in with a
[transition](motion.md#enter-and-exit). The gallery's toast does this, and
calls `ui.Announce` so that screen readers say it too. See
[`examples/gallery`](../examples/gallery).

## Deep links

An app opens for URLs of its own scheme, and for https links to its own
domains, when `ryui.json` lists them:

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
  on Linux and Windows: launched again, it comes to the front. `ryui
  dev` registers no scheme on Linux or Windows, and its runs don't hand
  themselves to an installed copy of the app: try links with a build.
- `links` opens it for `https://notes.example.com/...`: universal links
  on iOS, App Links on Android. The domain must say so too. It serves
  `apple-app-site-association` for iOS and `assetlinks.json` for
  Android, as each platform's docs describe. The desktops don't open
  for them.

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
`ryuinotes://add?text=...`. A notification's URL arrives the same way
when the user taps it, on every platform (see [Device](device.md)). A
web app has no links of its own: its page's address doesn't change as it
navigates, and opening it doesn't call `OnOpenURL`.

## Shares from other apps

With `"shareTarget": true` in `ryui.json`, the app shows in other apps'
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

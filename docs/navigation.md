# Navigation

An app's structure is made of stacks of screens, tabs, sheets, alerts
and menus. Each one is the platform's own: a `UINavigationController`
on iOS, a top app bar with Material transitions and predictive back on
Android. This page covers them, along with the back gesture, and the
links and shares that open the app. [`examples/navigation`](../examples/navigation)
has all of them.

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

A stack fills the space it's given. Each screen is an
[owner](concepts.md): popping it, by code or by the user's back gesture,
disposes it, stopping its effects. A screen pushed again starts fresh,
so keep what must outlive it in a signal outside it, or in
[storage](data.md).

## Screen options

`ui.ScreenOptions` configures a screen of a stack, given to `Stack` for
the root and to `Push` for the others.

- `Title` is the navigation bar's title (the top app bar's on Android).
  It's an accessor, so it can change.
- `Actions` are the bar's buttons.
- `NoBackGesture` turns the back gesture off for the screen (see
  [Back](#back)).
- `Search` puts a search field in the bar.
- `ZoomFrom` zooms the screen out of a view on the screen below.

### Bar buttons

Each action is a `ui.Action`. With an `Icon`, the bar shows the icon
instead of the title, and VoiceOver and TalkBack read the title.

```go
nav.Push(func() ui.Node { return Detail(n) }, ui.ScreenOptions{
	Title: ui.Val(n.Text),
	Actions: []ui.Action{
		{Title: "Share", Icon: ui.IconShare, OnPress: func() { ui.Share(n.Text) }},
		{Title: "Delete", Role: ui.ActionDestructive, OnPress: func() { nav.Pop() }},
	},
})
```

On Android, actions go in the top app bar as there's room, and the rest
go in its overflow menu. A long press on an icon shows its title.

### Search

`Search` puts a search field in the bar, as SwiftUI's `searchable` does.
Typing writes `Text`, and writing `Text` from code changes the field.
`Placeholder` is "Search" when it's empty, and `OnSubmit` runs on the
keyboard's search key.

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
}, ui.ScreenOptions{Title: ui.Val("Notes"), Search: &ui.Search{Text: query, Placeholder: "Search notes"}})
```

On iOS it's a `UISearchController` under the title, which stays shown
as the screen scrolls. On Android it's Material's pattern: a search
button in the top app bar that expands into the field.

### Zooming from a view

`ZoomFrom` is the `Style.Match` key of a view on the screen below, such
as a photo in a grid or a card. The screen zooms out of that view as
it's pushed, and back into it as it's popped. See
[shared elements](motion.md#shared-elements).

```go
key := fmt.Sprintf("photo-%d", id)
return ui.View(ui.ViewProps{
	Label: "Photo",
	Style: ui.Style{AspectRatio: 1, Match: key},
	OnPress: func() {
		nav.Push(func() ui.Node { return photo(id) }, ui.ScreenOptions{Title: ui.Val("Photo"), ZoomFrom: key})
	},
})
```

On iOS this is the system's zoom transition (iOS 18 and later; before
that, the screen pushes as usual). Android does its own zoom, and
popping with the predictive back gesture goes back into the view.
[`examples/gallery`](../examples/gallery)'s photo grid does this.

## Back

Each platform goes back its own way, and nui leaves it so:

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

Whichever way the user goes back, the screen is disposed, as with
`nav.Pop()`. Back closes an open sheet or dialog first.

## Tabs

`ui.Tabs` shows one page at a time, with a tab bar: a
`UITabBarController` on iOS, Material's bottom navigation bar on
Android. Each `ui.Tab` has a `Title`, an `Icon` and its `Content`.
`Selected` is the shown tab's index. Tapping a tab writes it, and
writing it switches tabs.

```go
tab := signal.New(0)
return ui.Tabs(ui.TabsProps{Selected: tab},
	ui.Tab{Title: "Notes", Icon: ui.IconList, Content: func() ui.Node {
		return ui.Stack(Notes, ui.ScreenOptions{Title: ui.Val("Notes")})
	}},
	ui.Tab{Title: "Settings", Icon: ui.IconSettings, Content: func() ui.Node {
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

`ui.Sheet` presents content in a modal sheet while `Open` is true. On
iOS it's a sheet presentation with detents, and on Android a
`BottomSheetDialog`. The node it returns takes no space, so put it
anywhere in the screen that opens it.

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
  keyboard shows, a sheet that can be large goes large.
- `Grabber` shows the handle at its top.
- `Content` is built when it opens and disposed when it closes, so state
  made in it starts fresh each time.

The user dismisses a sheet by swiping it down, or on Android with back
or a tap outside it. That sets `Open` to false and calls `OnDismiss`.
Setting `Open` to false from code closes it without calling
`OnDismiss`. So `OnDismiss` means "closed without your say", such as a
draft left unsaved.

## Alerts and action sheets

`ui.Alert` shows an alert now: a `UIAlertController` on iOS, a Material
dialog on Android. Pressing a button closes the alert and runs that
button's `OnPress`. With no `Actions`, it has a single OK.

```go
ui.Alert(ui.AlertProps{
	Title:   "Delete this note?",
	Message: "You can't undo this.",
	Actions: []ui.Action{
		{Title: "Cancel", Role: ui.ActionCancel},
		{Title: "Delete", Role: ui.ActionDestructive, OnPress: remove},
	},
})
```

An action's `Role` says what it is:

- `ui.ActionDefault` is an ordinary button.
- `ui.ActionCancel` is the way out. iOS puts it in its own place, and
  Android makes it the dialog's negative button.
- `ui.ActionDestructive` is red: in iOS's style, and in Material's
  error color on Android.

On Android, the last other button is the dialog's positive button, at
the end, and a second one is neutral. With more than two buttons other
than Cancel, the dialog shows them as a list.

`ActionSheet` shows the actions as an action sheet, a list of choices
coming up from the bottom on iOS:

```go
ui.Alert(ui.AlertProps{
	Title:       "Photo",
	ActionSheet: true,
	Actions: []ui.Action{
		{Title: "Take a photo", OnPress: func() {}},
		{Title: "Choose from library", OnPress: func() {}},
		{Title: "Cancel", Role: ui.ActionCancel},
	},
})
```

On Android an action sheet is a dialog with the actions as a list, and
its `Message` isn't shown. An alert closed some other way, such as back
or a tap outside it on Android, runs no button's `OnPress`.

## Menus

A menu shows a list of `ui.Action`s, the same type as alert buttons and
bar buttons, with an optional `Icon` beside each title. There are two:

- A `ui.Button` with `Menu` opens it when tapped.
- `ui.Menu` puts one on any content: on a tap, or with `LongPress` as a
  context menu.

On iOS it's a `UIMenu`, and on Android a `PopupMenu`. Both are in
[Components](components.md#menus). Rows of a `List` have swipe actions
too: see [Lists](components.md#lists).

## Toasts

nui has no toast component, since neither platform has a standard one
that apps share. Make one from a view placed over the screen,
shown with `ui.Show` and brought in with a
[transition](motion.md#enter-and-exit). The gallery's toast does this, and
calls `ui.Announce` so that screen readers say it too. See
[`examples/gallery`](../examples/gallery).

## Deep links

An app opens for URLs of its own scheme, and for https links to its own
domains, when `nui.json` lists them:

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "scheme": "notes",
  "links": ["notes.example.com"]
}
```

- `scheme` opens the app for `notes://anything`.
- `links` opens it for `https://notes.example.com/...`: universal links
  on iOS, App Links on Android. The domain must say so too. It serves
  `apple-app-site-association` for iOS and `assetlinks.json` for
  Android, as each platform's docs describe.

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
`nuinotes://add?text=...`. A notification's URL arrives the same way
(see [Device](device.md)).

## Shares from other apps

With `"shareTarget": true` in `nui.json`, the app shows in other apps'
share sheets, for text and links, such as "Share" in a browser or in
YouTube. `ui.OnShare` gets what's shared. A share that launched the app
goes to the first handler, as with `OnOpenURL`.

```go
ui.OnShare(func(text string) {
	fmt.Println("shared:", text)
})
```

On iOS the app comes with a share extension, which opens the app with
what's shared, as Android opens it: the extension shows no screen of
its own. What's shared is often a title and a link together (a page's
title and its URL from Safari, a line each), so look for the link in the
text. [`examples/downloader`](../examples/downloader) does
this.

To share from your app instead, `ui.Share(text)` shows the system's
share sheet.

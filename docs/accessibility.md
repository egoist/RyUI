# Accessibility

Asuka shows the platform's own widgets, so most of what screen readers,
the keyboard and the text size settings need is there without code. This
page covers what you get for free, and the props for the rest: labels,
roles, grouping, custom actions, adjustable views, announcements, the
keyboard, text size and Reduce Motion, and how to test it.

Each host gives the same props to its platform's accessibility API, and
the screen readers read them from there:

| Platform | Accessibility API | Screen readers |
| --- | --- | --- |
| iOS | UIAccessibility | VoiceOver |
| Android | Android's accessibility framework (`AccessibilityNodeInfo`) | TalkBack |
| macOS | NSAccessibility | VoiceOver |
| Windows | UI Automation | Narrator, NVDA |
| Linux | GTK's accessibility, over AT-SPI | Orca |
| The web | ARIA roles and attributes on the page's elements | the browser's: VoiceOver, NVDA, TalkBack... |

## What you get without asking

- **Controls are the platform's.** `Button`, `Toggle`, `Slider`,
  `TextField`, `Segmented`, `Stepper`, `DatePicker` and the rest are
  UIKit's, Material's, AppKit's, Win32's, GTK's and the browser's own
  controls, read and operated as in any app.
- **Pressable views are buttons.** A `View` with `OnPress` is one
  element:
  - iOS: a button read as the texts and labeled images in it, joined;
    a double tap presses it.
  - Android: a clickable view, whose texts TalkBack reads together.
  - macOS: a button (`AXButton`).
  - Windows: a button named by the texts in it, joined, which a screen
    reader presses (UI Automation's Invoke).
  - Linux: a button.
  - The web: an element with the `button` role.

  On Windows, Linux and the web the keyboard reaches it too (see
  [The keyboard](#the-keyboard)).
- **Text scales.** Every font, the semantic ones (`fonts.Body`,
  `fonts.Headline`...), a fixed size (`fonts.System(15)`) and your own fonts,
  follows the user's text size: Dynamic Type, Android's font scale,
  Windows' text size, GNOME's text scaling, the browser's text size.
  When the user changes it, Asuka measures the text again and lays out
  what it moves. A Mac has no text size setting.
- **Colors adapt.** Semantic colors (`colors.Label`, `colors.SecondaryLabel`,
  `colors.Background`, `colors.Accent`...) are the platform's (see
  [Layout and style](layout.md)): they follow dark mode, on iOS and
  macOS Increase Contrast, and on Linux GNOME's High Contrast.
- **Layout-only views stay out of the way.** A `View`, `Row` or `Column`
  with no events and no accessibility props makes no native view, so it
  never shows in the accessibility tree.
- **Pictures are quiet until described.** An `Image` without a `Label`,
  a `Symbol` and a `Shape` are hidden from screen readers. On Linux and
  the web, a common symbol is the exception: it's read by an English
  name (Back, Archive) unless it's labeled.

## Labels and hints

`Label` is what a screen reader reads for a view. `Hint` says what
using it does, when the label doesn't.

```go
ui.Row(ui.Style{Gap: 12},
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled, OnPress: send}),
	ui.Image(ui.ImageProps{Source: ui.Val("https://picsum.photos/id/1015/600/400"), Label: ui.Val("A river in a valley"),
		Style: ui.Style{AspectRatio: 3.0 / 2}}),
	ui.View(ui.ViewProps{Label: ui.Val("Favorite"), Role: ui.RoleImage},
		ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: colors.Red})),
)
```

- A `Button` reads its title. Give an icon-only button a `Label`: the
  platforms name only a few symbols themselves, and Android only in
  English.
- An `Image`'s `Label` describes it; without one it's decorative. An
  icon from [`asuka/svg`](../svg) takes a `Label` the same way.
- A `Symbol` has no label of its own: wrap it in a `View` with a
  `Label` and `ui.RoleImage` when it means something.
- `Hint` is iOS's accessibility hint, read after a pause (users can turn
  hints off), and Android's state description, read with the label. On
  macOS it's the view's help (`accessibilityHelp`), on Windows its help
  text, on Linux its accessible description, and on the web its
  `aria-description`. Keep it short: "Opens the message".

`Label`, `Hint`, `Role` and `Decorative` are set once, when the view is
made. A label that must change is a different view: make it in a
`Show` or `Dynamic` branch, or leave `Label` out of a pressable view so
its texts, which do change, are read.

## Roles

`Role` says what a view is:

| Role | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| `ui.RoleNone` | nothing (the default) | nothing | nothing | nothing | nothing | nothing |
| `ui.RoleButton` | button trait | read as a button | `AXButton` | Button | `button` | `button` |
| `ui.RoleLink` | link trait | read as a button | `AXLink` | Hyperlink | `link` | `link` |
| `ui.RoleHeader` | header trait | heading | `AXHeading` | Text, heading level 1 | `heading` | `heading` |
| `ui.RoleImage` | image trait | read as an image | `AXImage` | Image | `img` | `img` |
| `ui.RoleText` | static text trait | nothing | `AXStaticText` | Text | `label` | nothing |
| `ui.RoleSearch` | search field trait | nothing | `AXTextField` | Edit | `search` | `searchbox` |

Windows' are UI Automation's control types, Linux's GTK's accessible
roles, the web's ARIA roles. A pressable `View` is a button already. A
view with a `Role` is an element of its own on iOS and macOS, so give it
a `Label` too, or it's read as nothing but its role.

## Grouping

A screen reader moves through elements one by one. A row of three texts
is three stops; often it should be one. Two ways:

- **Make it pressable.** A `View` with `OnPress` is one element, read as
  what's in it. This is the usual row:

```go
func Row(m message, open func()) ui.Node {
	return ui.View(ui.ViewProps{
		OnPress: open,
		Hint:    ui.Val("Opens the message"),
		Style:   ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.Symmetric(12, 20)},
	},
		ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: colors.SecondaryLabel})),
		ui.Column(ui.Style{Grow: 1, Gap: 2},
			ui.StaticText(m.from, ui.Style{Font: fonts.Headline}),
			ui.StaticText(m.subject, ui.Style{Color: colors.SecondaryLabel}),
		),
	)
}
```

- **Give it a `Label`.** A `View` with a `Label` is read as its label.
  On iOS and macOS what's in it isn't read separately; on Android the
  label is the view's content description; on Windows it's the view's
  name, on Linux its accessible label, on the web its `aria-label`. Say what the eye takes in at a
  glance, in words: "Night Drive by Ada, 4 minutes", not "4:00".

```go
ui.View(ui.ViewProps{Label: fmt.Sprintf("%s by %s, %d minutes", title, artist, minutes),
	Style: ui.Style{Direction: ui.RowDir, Gap: 8}},
	ui.StaticText(title, ui.Style{Font: fonts.Headline}),
	ui.StaticText(artist, ui.Style{Color: colors.SecondaryLabel}),
	ui.StaticText(fmt.Sprintf("%d:00", minutes), ui.Style{Color: colors.SecondaryLabel}),
)
```

## Decoration

`Decorative` hides a view and everything in it from screen readers:
an ornament, an icon next to a text that says the same, a checkmark in a
toast that reads "Saved".

```go
ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Green}))
```

## Headings

Screen reader users jump between headings (VoiceOver's rotor, TalkBack's
navigation by headings, NVDA's and Orca's H key, a browser screen
reader's headings list). Mark a section's title as one: a `RichText`
with `Role: ui.RoleHeader`, or a `View` with a `Label` and the role.
`Text` takes no role. [`asuka/markdown`](../markdown) marks its headings
already.

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Recent")}), Role: ui.RoleHeader,
	Style: ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}}),
ui.View(ui.ViewProps{Label: ui.Val("Settings"), Role: ui.RoleHeader},
	ui.StaticText("Settings", ui.Style{Font: fonts.Title2})),
```

A `RichText`'s links are elements of their own, which a screen reader
finds and activates.

## Custom actions

What a sighted user does with a swipe, a long press or a small button
inside a row, a screen reader user reaches through actions: VoiceOver's
Actions rotor, swiping up or down; TalkBack's actions menu. `Actions`
lists them on a view:

```go
ui.View(ui.ViewProps{
	Actions: []ui.A11yAction{{Name: ui.Val("Reply"), Do: reply}, {Name: ui.Val("Archive"), Do: archive}},
	Style:   ui.Style{Padding: ui.Symmetric(12, 20)},
}, ui.Text(subject))
```

A view with actions is one element, read as its texts. Where they go:

- **iOS** and **macOS**: the view's custom actions
  (`UIAccessibilityCustomAction`, `NSAccessibilityCustomAction`), in
  VoiceOver's Actions rotor on iOS and its Actions menu on a Mac
  (VO-Command-Space).
- **Android**: TalkBack's actions menu.
- **Windows**: the view's context menu, which the keyboard opens too
  (Shift+F10, the Menu key) and the mouse with a right-click. The view
  becomes a tab stop, so the keyboard reaches it.
- **Linux**: the view's actions over AT-SPI, which Orca lists and does,
  and its context menu (a secondary click, the Menu key, Shift+F10). The
  view becomes a tab stop.
- **The web**: ARIA has no custom actions, so they aren't offered
  there. Give a view the same commands another way too (a button, a
  `Menu`) when the app runs in a browser.

A `List`'s `SwipeActions` are offered to screen readers for you:
UIKit's list offers its swipe actions to VoiceOver itself, and on
Android Asuka adds them to the row as TalkBack actions. Don't list them
again in `Actions`.

```go
ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
		}
	},
}, items, key, row)
```

A `Menu`'s actions, and buttons with a `Menu`, are the platform's menus,
accessible as they are.

## Adjustable views

`OnAdjust` makes a view adjustable, as a slider is: VoiceOver's swipe up
and down, and TalkBack's (or the volume keys), step it by 1 or -1. Use
it for a control of your own that picks a value; a `Slider` or
`Stepper` already is one.

On a Mac the view is an incrementor (`AXIncrementor`), which VoiceOver
steps with VO-Up and VO-Down. On Windows, Linux and the web it's a tab
stop that the arrow keys step: up and right by 1, down and left by -1
(on Windows and Linux, left and right swap in a right-to-left
language). Linux and the web give it the slider role.

```go
func Rating() ui.Node {
	stars := signal.New(3)
	var icons []ui.Node
	for i := range 5 {
		icons = append(icons, ui.Symbol(ui.IconStar.Filled(), ui.StyleFunc(func() ui.Style {
			if i < stars.Get() {
				return ui.Style{Color: colors.Orange}
			}
			return ui.Style{Color: colors.TertiaryLabel}
		})))
	}
	return ui.View(ui.ViewProps{
		OnAdjust: func(by int) { stars.Set(min(max(stars.Peek()+by, 0), 5)) },
		Style:    ui.Style{Direction: ui.RowDir, Gap: 4, Align: ui.AlignCenter},
	}, append(icons, ui.Text(signal.Func(func() string { return fmt.Sprintf("Rating: %d of 5", stars.Get()) })))...)
}
```

The view is read as its text, which says where it is and changes as it
steps. It has no `Label`, which would be read instead and can't change.
Clamp the value yourself: the screen reader keeps asking.

## Announcements

`ui.Announce` has the screen reader, if one is on, read a text: what
changed where focus isn't. A toast, a count of search results, a save
that finished.

```go
ui.Animate(func() { shown.Set(true) })
ui.Announce("Saved") // a screen reader wouldn't notice the toast
```

It's `UIAccessibility`'s announcement on iOS,
`announceForAccessibility` on Android, an announcement request on
macOS, a UI Automation notification on Windows, GTK's announcement on
Linux (GTK 4.14 and later), and a polite `aria-live` region on the web.
Without a screen reader, it does nothing. Don't announce what the
focused view says already.

## Moving focus

`ui.AccessibilityFocus(node)` moves the screen reader's focus to a view,
for when the screen changed under the user and what matters is
somewhere else: the error under a form that failed to submit, the first
result of a search, the title of a panel that slid in.

```go
errText := ui.Text(message)
// ...
OnPress: func() {
	if err := signIn(); err != nil {
		message.Set(err.Error())
		ui.AccessibilityFocus(errText)
	}
},
```

It's sent with the next flush, once the view exists and has its frame:
`UIAccessibility`'s screen-changed notification with the view on iOS,
`ACTION_ACCESSIBILITY_FOCUS` on Android, a layout change naming the view
on macOS and the focus event on Windows. A container with no view of
its own focuses its first view. On those four, without a screen reader
it does nothing, and it never moves the keyboard's focus (that's
`ui.Focus`).

On Linux and the web it moves the keyboard's focus to the view, and the
screen reader follows it: GTK and the page have no other way to point a
screen reader at a view.

## Is a screen reader on

`ui.UseEnvironment()`'s `ScreenReader` is true while a screen reader is
on, and follows it being turned on or off:

- iOS and macOS: VoiceOver.
- Android: TalkBack.
- Windows: the system's screen reader flag, which Narrator and other
  screen readers set.
- Linux: GNOME's screen reader setting (Orca).
- The web: never. Browsers don't say whether a screen reader is on.

Use it for what works differently by ear, not to hide things: keep a toast up until
it's dismissed rather than for three seconds, or turn a carousel that
advances by itself into one that waits.

## Text size

Text scales with no code, so what's left is layout that still works at
large sizes:

- Don't fix the height of what holds text. Let it grow; use `MinHeight`
  for a minimum.
- Avoid `Lines: 1` on text that matters: at large sizes it's cut short.
- Put long rows in a `Scroll` or a `List`.

`ui.UseEnvironment()`'s `FontScale` is the user's text size, 1 being the
default: Dynamic Type's Large, Android's default font scale, Windows'
100% (Settings, Accessibility, Text size), GNOME's scaling of 1, the
browser's default text size (16 CSS pixels). On a Mac it's always 1.
Read it in a `StyleFunc` to change a layout at large sizes, as iOS's
own apps stack a row's label and value:

```go
func Adaptive() ui.Node {
	env := ui.UseEnvironment()
	return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		if env.Get().FontScale > 1.5 {
			return ui.Style{Direction: ui.ColumnDir, Gap: 4}
		}
		return ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}
	})},
		ui.StaticText("Wi-Fi", ui.Style{Grow: 1}),
		ui.StaticText("Home", ui.Style{Color: colors.SecondaryLabel}),
	)
}
```

## Reduce Motion

`ui.UseEnvironment()`'s `ReduceMotion` is iOS's and macOS's Reduce
Motion, Android's animations turned off, Windows' animation effects
off, GNOME's animations off, or the browser's `prefers-reduced-motion`.
On iOS Asuka makes transitions dissolve instead of moving; elsewhere its
animations end at once. See [Motion](motion.md) for what's left to you.

## The keyboard

On Windows, Linux and the web, Asuka's views take the keyboard as the
platform's own controls do:

- **Tab** moves between the controls and the pressable views, in the
  views' order; Shift+Tab goes back.
- **Enter** and **Space** press a focused pressable view, as a button.
- A focused view shows a focus ring: Fluent's on Windows, an outline in
  the accent on Linux, the browser's on the web.
- A view with `Actions` or `OnAdjust` is a tab stop: its context menu
  opens with Shift+F10 or the Menu key (Windows, Linux), and the arrow
  keys step it.

`ui.Focus` and `ui.Blur` move the keyboard's focus to a field and away
(see [Components](components.md)).

## Testing with a screen reader

Try each screen with the screen reader on, eyes off the screen if you
can: can you reach everything, does each stop say what it is, does
every swipe and drag have an action? On a desktop, try it with the
keyboard alone too.

- **VoiceOver** runs on an iPhone: Settings, Accessibility, VoiceOver.
  Set the Accessibility Shortcut (a triple click of the side button) to
  turn it on and off. The simulator has no VoiceOver; Xcode's
  Accessibility Inspector (Xcode, Open Developer Tool) shows each
  element's label, traits and actions there.
- **TalkBack** runs on an Android phone, or an emulator with Google
  Play, from the Android Accessibility Suite: Settings, Accessibility,
  TalkBack.
- **VoiceOver on a Mac**: Command-F5, or System Settings,
  Accessibility, VoiceOver. Accessibility Inspector (Xcode, Open
  Developer Tool) inspects the macOS app's elements directly.
- **Narrator** on Windows: Ctrl+Windows+Enter. NVDA is a free download.
  Accessibility Insights for Windows shows each element's UI Automation
  name, control type and patterns.
- **Orca** on Linux: Super+Alt+S in GNOME, or Settings, Accessibility,
  Screen Reader. Accerciser shows the AT-SPI tree.
- **The web**: the browser's developer tools show the accessibility
  tree (Chrome's and Firefox's Accessibility panels); try VoiceOver with
  Safari, NVDA with Chrome or Firefox.
- **Text size**: iOS's Settings, Accessibility, Display & Text Size,
  Larger Text, or Xcode's Environment Overrides on the simulator;
  Android's Settings, Display, Font size; Windows' Settings,
  Accessibility, Text size; GNOME's Settings, Accessibility, Large Text;
  the browser's text size. Try the largest.
- **Reduce Motion**: iOS's Settings, Accessibility, Motion; Android's
  Settings, Accessibility, Remove animations; macOS's System Settings,
  Accessibility, Display, Reduce motion; Windows' Settings,
  Accessibility, Visual effects, Animation effects; GNOME's Settings,
  Accessibility, Reduce Animation; the system's setting for a
  browser.

`asuka dev` keeps running as you change these: the app follows each
setting as it changes.

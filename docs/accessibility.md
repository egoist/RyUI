# Accessibility

RyUI shows the platform's own widgets, so most of what VoiceOver,
TalkBack and the text size settings need is there without code. This
page covers what you get for free, and the props for the rest: labels,
roles, grouping, custom actions, adjustable views, announcements, text
size and Reduce Motion, and how to test it.

## What you get without asking

- **Controls are the platform's.** `Button`, `Toggle`, `Slider`,
  `TextField`, `Segmented`, `Stepper`, `DatePicker` and the rest are
  UIKit's and Material's views, read and operated as in any app.
- **Pressable views are buttons.** A `View` with `OnPress` is one
  element: on iOS, a button read as the texts and labeled images in it,
  joined, and a double tap presses it; on Android, a clickable view,
  whose texts TalkBack reads together.
- **Text scales.** `Text` is `UILabel` and `TextView`, and every font
  follows Dynamic Type and Android's font scale: the semantic ones
  (`ui.Body`, `ui.Headline`...), a fixed size (`ui.System(15)`) and your
  own fonts too. When the user changes the size, RyUI measures the text
  again and lays out what it moves.
- **Colors adapt.** Semantic colors (`ui.Label`, `ui.SecondaryLabel`,
  `ui.SystemBackground`, `ui.Accent`...) are UIKit's semantic colors and
  Material 3's color roles: they follow dark mode, and on iOS, Increase
  Contrast.
- **Layout-only views stay out of the way.** A `View`, `Row` or `Column`
  with no events and no accessibility props makes no native view, so it
  never shows in the accessibility tree.
- **Pictures are quiet until described.** An `Image` without a `Label`,
  a `Symbol` and a `Shape` are hidden from screen readers.

## Labels and hints

`Label` is what a screen reader reads for a view. `Hint` says what
using it does, when the label doesn't.

```go
ui.Row(ui.Style{Gap: 12},
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: "Send", Variant: ui.ButtonFilled, OnPress: send}),
	ui.Image(ui.ImageProps{Source: ui.Val("https://picsum.photos/id/1015/600/400"), Label: "A river in a valley",
		Style: ui.Style{AspectRatio: 3.0 / 2}}),
	ui.View(ui.ViewProps{Label: "Favorite", Role: ui.RoleImage},
		ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: ui.Red})),
)
```

- A `Button` reads its title. Give an icon-only button a `Label`: the
  platforms name only a few symbols themselves, and Android only in
  English.
- An `Image`'s `Label` describes it; without one it's decorative. An
  icon from [`ryui/svg`](../svg) takes a `Label` the same way.
- A `Symbol` has no label of its own: wrap it in a `View` with a
  `Label` and `ui.RoleImage` when it means something.
- `Hint` is iOS's accessibility hint, read after a pause (users can turn
  hints off), and Android's state description, read with the label.
  Keep it short: "Opens the message".

`Label`, `Hint`, `Role` and `Decorative` are set once, when the view is
made. A label that must change is a different view: make it in a
`Show` or `Dynamic` branch, or leave `Label` out of a pressable view so
its texts, which do change, are read.

## Roles

`Role` says what a view is:

| Role | iOS | Android |
| --- | --- | --- |
| `ui.RoleNone` | nothing (the default) | nothing |
| `ui.RoleButton` | button trait | read as a button |
| `ui.RoleLink` | link trait | read as a button |
| `ui.RoleHeader` | header trait | heading |
| `ui.RoleImage` | image trait | read as an image |
| `ui.RoleText` | static text trait | nothing |
| `ui.RoleSearch` | search field trait | nothing |

A pressable `View` is a button already. A view with a `Role` is an
element of its own on iOS, so give it a `Label` too, or it's read as
nothing but its role.

## Grouping

A screen reader moves through elements one by one. A row of three texts
is three stops; often it should be one. Two ways:

- **Make it pressable.** A `View` with `OnPress` is one element, read as
  what's in it. This is the usual row:

```go
func Row(m message, open func()) ui.Node {
	return ui.View(ui.ViewProps{
		OnPress: open,
		Hint:    "Opens the message",
		Style:   ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.Symmetric(12, 20)},
	},
		ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: ui.SecondaryLabel})),
		ui.Column(ui.Style{Grow: 1, Gap: 2},
			ui.StaticText(m.from, ui.Style{Font: ui.Headline}),
			ui.StaticText(m.subject, ui.Style{Color: ui.SecondaryLabel}),
		),
	)
}
```

- **Give it a `Label`.** A `View` with a `Label` is read as its label.
  On iOS what's in it isn't read separately; on Android the label is
  the view's content description. Say what the eye takes in at a
  glance, in words: "Night Drive by Ada, 4 minutes", not "4:00".

```go
ui.View(ui.ViewProps{Label: fmt.Sprintf("%s by %s, %d minutes", title, artist, minutes),
	Style: ui.Style{Direction: ui.RowDir, Gap: 8}},
	ui.StaticText(title, ui.Style{Font: ui.Headline}),
	ui.StaticText(artist, ui.Style{Color: ui.SecondaryLabel}),
	ui.StaticText(fmt.Sprintf("%d:00", minutes), ui.Style{Color: ui.SecondaryLabel}),
)
```

## Decoration

`Decorative` hides a view and everything in it from screen readers:
an ornament, an icon next to a text that says the same, a checkmark in a
toast that reads "Saved".

```go
ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconCheck, ui.Style{Color: ui.Green}))
```

## Headings

Screen reader users jump between headings (VoiceOver's rotor, TalkBack's
navigation by headings). Mark a section's title as one: a `RichText`
with `Role: ui.RoleHeader`, or a `View` with a `Label` and the role.
`Text` takes no role. [`ryui/markdown`](../markdown) marks its headings
already.

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Recent")}), Role: ui.RoleHeader,
	Style: ui.Style{Font: ui.Title2.Weighted(ui.Bold)}}),
ui.View(ui.ViewProps{Label: "Settings", Role: ui.RoleHeader},
	ui.StaticText("Settings", ui.Style{Font: ui.Title2})),
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
	Actions: []ui.A11yAction{{Name: "Reply", Do: reply}, {Name: "Archive", Do: archive}},
	Style:   ui.Style{Padding: ui.Symmetric(12, 20)},
}, ui.Text(subject))
```

A view with actions is one element, read as its texts.

A `List`'s `SwipeActions` are offered to screen readers for you:
UIKit's list offers its swipe actions to VoiceOver itself, and on
Android RyUI adds them to the row as TalkBack actions. Don't list them
again in `Actions`.

```go
ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
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

```go
func Rating() ui.Node {
	stars := signal.New(3)
	var icons []ui.Node
	for i := range 5 {
		icons = append(icons, ui.Symbol(ui.IconStar.Filled(), ui.StyleFunc(func() ui.Style {
			if i < stars.Get() {
				return ui.Style{Color: ui.Orange}
			}
			return ui.Style{Color: ui.TertiaryLabel}
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

It's `UIAccessibility`'s announcement on iOS and
`announceForAccessibility` on Android. Without a screen reader, it does
nothing. Don't announce what the focused view says already.

## Text size

Text scales with no code, so what's left is layout that still works at
large sizes:

- Don't fix the height of what holds text. Let it grow; use `MinHeight`
  for a minimum.
- Avoid `Lines: 1` on text that matters: at large sizes it's cut short.
- Put long rows in a `Scroll` or a `List`.

`ui.UseEnvironment()`'s `FontScale` is the user's text size, 1 being the
default (Dynamic Type's Large, Android's default font scale). Read it in
a `StyleFunc` to change a layout at large sizes, as iOS's own apps stack
a row's label and value:

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
		ui.StaticText("Home", ui.Style{Color: ui.SecondaryLabel}),
	)
}
```

## Reduce Motion

`ui.UseEnvironment()`'s `ReduceMotion` is iOS's Reduce Motion, or
Android's animations turned off. On iOS RyUI makes transitions dissolve
instead of moving; on Android every animation already ends at once. See
[Motion](motion.md#reduce-motion) for what's left to you.

## Testing with VoiceOver and TalkBack

Try each screen with the screen reader on, eyes off the screen if you
can: can you reach everything, does each stop say what it is, does
every swipe and drag have an action?

- **VoiceOver** runs on an iPhone: Settings, Accessibility, VoiceOver.
  Set the Accessibility Shortcut (a triple click of the side button) to
  turn it on and off. The simulator has no VoiceOver; Xcode's
  Accessibility Inspector (Xcode, Open Developer Tool) shows each
  element's label, traits and actions there.
- **TalkBack** runs on an Android phone, or an emulator with Google
  Play, from the Android Accessibility Suite: Settings, Accessibility,
  TalkBack.
- **Text size**: iOS's Settings, Accessibility, Display & Text Size,
  Larger Text, or Xcode's Environment Overrides on the simulator;
  Android's Settings, Display, Font size. Try the largest.
- **Reduce Motion**: iOS's Settings, Accessibility, Motion; Android's
  Settings, Accessibility, Remove animations.

`ryui dev` keeps running as you change these: the app follows each
setting as it changes.

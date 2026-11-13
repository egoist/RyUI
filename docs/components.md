# Components

A component is a Go function that returns a `ui.Node`. This page covers
the views package `ui` gives you, grouped by what they do, and the
packages next to it for Markdown and SVG. Each one is the platform's own
widget where the platform has one: UIKit on iOS, Android Views and
Material 3 on Android, AppKit on macOS, Win32's controls drawn as
Windows 11's (Fluent) on Windows, GTK 4 and libadwaita on Linux, the
page's elements in a browser. How views are sized and placed is in
[Layout](layout.md); screens, tabs and sheets are in
[Navigation](navigation.md).

Most props that can change take a `signal.Accessor`: pass a signal, a
`signal.Func`, or `ui.Val(v)` for a value that never changes. A control
that the user changes (a field, a toggle) takes a `*signal.Signal`
instead, and writes it. See [Concepts](concepts.md).

## Each platform's widgets

| Component | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Text` | `UILabel` | `TextView` | `NSTextField` (a label) | drawn by the host's view | `GtkLabel` | the page's text |
| `Button` | `UIButton` | `MaterialButton` | `NSButton` | `BUTTON`, drawn as Fluent's | `GtkButton` | `<button>` |
| `TextField` | `UITextField`; `UITextView` multiline | `EditText` | `NSTextField`, `NSSecureTextField`; `NSTextView` multiline | `EDIT` in a frame drawn as Fluent's | `GtkEntry`, `GtkPasswordEntry`; `GtkTextView` multiline | `<input>`; `<textarea>` multiline |
| `Toggle` | `UISwitch` | `MaterialSwitch` | `NSSwitch` | a `BUTTON` check box drawn as a toggle switch | `GtkSwitch` | `<input type="checkbox" switch>` |
| `Slider` | `UISlider` | Material `Slider` | `NSSlider` | a trackbar | `GtkScale` | `<input type="range">` |
| `Segmented` | `UISegmentedControl` | Material segmented buttons | `NSSegmentedControl` | drawn, as the Windows Community Toolkit's | linked `GtkToggleButton`s | buttons in a radio group |
| `Stepper` | `UIStepper` | two Material icon buttons | `NSStepper` | two buttons | linked − and + buttons | two buttons |
| `Wheel` | `UIPickerView` | `NumberPicker` | `NSPopUpButton` | a drop-down `COMBOBOX` | `GtkDropDown` | `<select>` |
| `ColorPicker` | `UIColorWell` | a swatch and a Material dialog | `NSColorWell` and the color panel | a swatch and the color dialog (`ChooseColor`) | `GtkColorDialogButton` | `<input type="color">` |
| `DatePicker` | `UIDatePicker` | buttons opening Material's pickers | `NSDatePicker` | the date and time picker (`SysDateTimePick32`) | a button opening `GtkCalendar` and spin buttons | `<input type="date">`, `time`, `datetime-local` |
| `Progress` | `UIProgressView` | `LinearProgressIndicator` | `NSProgressIndicator` | drawn as Fluent's | `GtkProgressBar` | `<progress>` |
| `Activity` | `UIActivityIndicatorView` | `CircularProgressIndicator` | `NSProgressIndicator`, spinning | Fluent's progress ring, drawn | `AdwSpinner` (`GtkSpinner` before libadwaita 1.6) | a spinner, drawn |
| `Image` | `UIImageView` | `ImageView` | `NSImageView` | drawn with GDI+ | drawn from GDK textures | `<img>` |
| `Symbol` | SF Symbols | Material Symbols | SF Symbols | Segoe Fluent Icons | the icon theme's, else Material Symbols | Material Symbols |
| `Video` | `AVPlayerViewController` | Media3's player | `AVPlayerView` | a `<video>` in WebView2 | `GtkVideo` (GStreamer) | `<video>` |
| `WebView` | `WKWebView` | `WebView` | `WKWebView` | WebView2 | `WebKitWebView` (WebKitGTK 6.0) | `<iframe>` |
| `Scroll` | `UIScrollView` | `NestedScrollView` | `NSScrollView` | the host's view, Fluent's scroll indicator | `GtkScrolledWindow` | a scrolling element |
| `List` | `UICollectionView` | `RecyclerView` | rows the host recycles, in an `NSScrollView` | rows the host recycles | rows the host recycles, in a `GtkScrolledWindow` | rows the host recycles |

The sections below say where a platform behaves differently.

## Text

### Text and StaticText

`ui.Text` shows a line or paragraph of text. It is the platform's label,
and wraps by default; `Style.Lines` limits how many lines it takes, and
cuts the rest with an ellipsis. `ui.StaticText` is a `Text` whose text
never changes.

```go
count := signal.New(0)
label := signal.Func(func() string { return fmt.Sprintf("%d unread", count.Get()) })
return ui.Column(ui.Style{Gap: 8},
	ui.StaticText("Inbox", ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}),
	ui.Text(label, ui.Style{Color: colors.SecondaryLabel}),
	ui.StaticText("A long description that stops after two lines.", ui.Style{Lines: 2}),
)
```

The style's `Font`, `Color`, `TextAlign` and `Lines` are the text's.
Text styles (`fonts.Body`, `fonts.Headline`, `fonts.Footnote`...) follow the
text size the user picked.

### RichText

`ui.RichText` shows styled text: a `ui.Rich`, a list of spans. A `"\n"`
in a span starts a new paragraph. A span's fields left zero come from
the `RichText`'s style.

```go
ui.RichText(ui.RichTextProps{
	Text: ui.Val(ui.Rich{
		ui.Strong("asuka"), ui.Plain(" draws "), ui.Em("native"), ui.Plain(" views. Run "),
		ui.Code("asuka dev"), ui.Plain(", or read "), ui.Link("the docs", "https://example.com/docs"),
		ui.Plain(".\nA second paragraph, "), ui.Plain("in red").Colored(colors.Red), ui.Plain("."),
	}),
	OnLink: ui.OpenURL,
})
```

- `Plain`, `Strong`, `Em`, `Code` and `Link` make spans. `Colored`,
  `Highlighted` and `Weighted` return a span with a color, a background
  or a weight.
- A `ui.Span` has the rest: `Font`, `Italic`, `Mono`, `Underline`,
  `Strike`, `Background` (drawn on each line the span covers, with
  rounded corners).
- A tapped link calls `OnLink` with its URL. Without `OnLink`, links
  do nothing.
- `Role: ui.RoleHeader` makes it a heading to screen readers.

On iOS and Android each paragraph is typeset by the platform's engine
(CoreText, StaticLayout) and cached by its content and width: appending
to a long text, a reply streaming in, typesets only the paragraph that
changed. On macOS it's a read-only `NSTextView` (TextKit), on Windows a
read-only rich edit (`RICHEDIT50W`), on Linux a `GtkLabel` styled with
Pango's attributes, and in a browser spans in paragraphs.
[`examples/richtext`](../examples/richtext) streams one.

For plain text, use `Text`: it is the platform's label, with its native
behavior.

**Inline views.** `ui.Embed` puts a component in the text: it sits on
the baseline and wraps with the words. Its key keeps the same component,
and its state, from one `Rich` to the next.

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{
	ui.Plain("Status: "),
	ui.Embed("badge", func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Background: colors.Green, CornerRadius: 6, Padding: ui.Symmetric(2, 6)}},
			ui.StaticText("live", ui.Style{Font: fonts.Caption, Color: colors.RGB(255, 255, 255)}))
	}),
	ui.Plain(" since this morning."),
})})
```

### Selectable text

`ui.Selectable` makes the `RichText` views inside it selectable, as one
text across all of them: a long press selects a word, the platform's
handles extend the selection, and its edit menu copies it. With a
mouse, a drag selects and the platform's copy shortcut copies. Linux
and the web select across the texts as one too; on macOS and Windows
each text selects on its own.

```go
ui.Selectable(ui.SelectableProps{Style: ui.Style{Gap: 12}},
	ui.RichText(ui.RichTextProps{Text: ui.Val(question), Role: ui.RoleHeader}),
	ui.RichText(ui.RichTextProps{Text: ui.Val(answer)}),
)
```

`Text` is not selectable; use a `RichText` in a `Selectable`. To copy
something without a selection, `ui.Copy(text)` puts it on the clipboard.

### Markdown

Package `asuka/markdown` shows Markdown as native views, built from
`RichText`. It parses as text is appended: only the block being written
parses again, and only its views update.

```go
markdown.View(markdown.Props{Text: reply, Streaming: writing})
```

`Streaming` says the text is still being written, so a half-written
`**bold` shows as bold rather than as asterisks. The view is one
selection by default (`NotSelectable` turns that off); links open with
`ui.OpenURL` unless you give `OnLink`; `Theme` changes fonts, colors and
spacing; `Code` shows code blocks your own way. It reads CommonMark and
GitHub's extensions. See [`examples/markdown`](../examples/markdown).

## Buttons

`ui.Button` is the platform's button: a `UIButton` configuration on
iOS, a Material button on Android, an `NSButton` on macOS, a `BUTTON`
drawn as Fluent's on Windows, a `GtkButton` on Linux, a `<button>` in a
browser.

```go
saving := signal.New(false)
return ui.Row(ui.Style{Gap: 12, Wrap: true},
	ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, Disabled: saving,
		OnPress: func() { saving.Set(true) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonTinted}),
	ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: ui.Val("Delete"), Variant: ui.ButtonGray}),
)
```

- `Title` is its text; `Icon` a symbol before it (see
  [Symbols](#symbols-and-icons)). A button with an icon and no title is
  the icon alone.
- `Label` is what VoiceOver and TalkBack read, the title by default.
  Give an icon-only button one.
- `Tooltip` is a short text shown while a pointer rests on it: an
  iPad's pointer (`UIToolTipInteraction`), a mouse on Android, where a
  long press shows it too (`tooltipText`), and on macOS, Windows, Linux
  and the web (the element's `title`). Give an icon-only button one, its
  name. `ViewProps.Tooltip`
  is the same for any view. A touch screen alone never shows it on iOS,
  so it can't be the only place something is said.
- `Disabled` disables it, the platform's way.
- `Style.Font` and `Style.Color` change its text and tint.

`Variant` is its look:

| Variant | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| `ButtonPlain` (default) | plain | text button | borderless, in the accent color | subtle, in the accent color | flat, in the accent color | plain |
| `ButtonFilled` | filled | filled button | push button in the accent color | accent button | suggested action | filled |
| `ButtonTinted` | tinted | tonal button | push button, its title in the accent color | a wash of the accent | a wash of the accent | tinted |
| `ButtonGray` | gray | tonal button | push button | standard button | standard button | gray |
| `ButtonBordered` | bordered | outlined button | push button | standard button | standard button | outlined |
| `ButtonGlass` | Liquid Glass (`glass()`, iOS 26; gray before) | tonal button | as `ButtonGray` | as `ButtonGray` | standard button | as `ButtonGray` |
| `ButtonGlassProminent` | tinted Liquid Glass (`prominentGlass()`, iOS 26; filled before) | filled button | as `ButtonFilled` | as `ButtonFilled` | suggested action | as `ButtonFilled` |

The glass buttons are capsules, and a glass button with an icon alone is
a circle: a sheet's close button, as iOS 26's own.

### Menus

A button with `Menu` shows its actions when it's tapped, instead of
calling `OnPress` (a `UIMenu` on iOS, a `PopupMenu` on Android, an
`NSMenu` on macOS, a pop-up menu on Windows, a `GtkPopoverMenu` on
Linux, a popover in a browser). Each
`ui.Action` has a `Title`, an optional `Icon` beside it, a `Role`, and
`OnPress`. `ui.ActionDestructive` shows it in red.

```go
ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: []ui.Action{
	{Title: ui.Val("Copy"), Icon: ui.IconCopy, OnPress: func() { ui.Copy("https://go.dev") }},
	{Title: ui.Val("Share"), Icon: ui.IconShare, OnPress: func() { ui.Share("https://go.dev") }},
	{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {}},
}})
```

`ui.Menu` puts a menu on any content. With `LongPress` it's a context
menu, shown on a long press; with a mouse (macOS, Windows, Linux, the
web), on a secondary click:

```go
ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
	{Title: ui.Val("Reply"), Icon: ui.IconMessage},
	{Title: ui.Val("Delete"), Role: ui.ActionDestructive},
}, Style: ui.Style{Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12}},
	ui.StaticText("Long-press for a menu"),
)
```

## Text fields

`ui.TextField` is a text input. `Value` is its text: typing writes the
signal, and writing the signal from code replaces the text.

```go
email := signal.New("")
password := signal.New("")
var pw ui.Node
pw = ui.TextField(ui.TextFieldProps{Value: password, Placeholder: ui.Val("Password"),
	Secure: true, ContentType: "password", ReturnKey: ui.ReturnGo,
	OnSubmit: func() { ui.Blur(pw) }})
em := ui.TextField(ui.TextFieldProps{Value: email, Placeholder: ui.Val("Email"),
	Keyboard: ui.KeyboardEmail, ContentType: "email", Autocapitalize: ui.CapitalizeNone,
	Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnNext, OnSubmit: func() { ui.Focus(pw) }})
return ui.Column(ui.Style{Gap: 12}, em, pw)
```

- `Secure` hides what's typed, for passwords.
- `Keyboard`: `KeyboardDefault`, `KeyboardNumber`, `KeyboardDecimal`,
  `KeyboardEmail`, `KeyboardURL`, `KeyboardPhone`.
- `ReturnKey` is the return key's label: `ReturnDefault`, `ReturnDone`,
  `ReturnGo`, `ReturnNext`, `ReturnSearch`, `ReturnSend`. Pressing it
  calls `OnSubmit`.
- `ContentType` says what it holds, for the system's AutoFill:
  `ui.ContentUsername`, `ContentPassword`, `ContentNewPassword` (the
  platform offers a strong password), `ContentOneTimeCode` (the code a
  message just brought shows above the keyboard: UIKit's `oneTimeCode`,
  Android's SMS code hint), `ContentEmail`, `ContentPhone`,
  `ContentName`, `ContentAddress`, `ContentPostalCode`,
  `ContentCardNumber`... Each host names it as its platform does.
- `Autocapitalize`: `CapitalizeSentences` (the default),
  `CapitalizeNone`, `CapitalizeWords`, `CapitalizeCharacters`.
  `Autocorrect`: `AutocorrectDefault`, `AutocorrectYes`,
  `AutocorrectNo`.
- `OnFocus` and `OnBlur` follow the keyboard. `ui.Focus(field)` asks for
  it, `ui.Blur(field)` dismisses it.

A desktop has no on-screen keyboard to change, so `Keyboard` and
`ReturnKey` mostly don't show there; Return still calls `OnSubmit`. On
macOS `ContentType` is the field's AutoFill content type. On Windows
`KeyboardNumber` takes digits only. On Linux `Keyboard`, `ContentType`,
`Autocapitalize` and `Autocorrect` tell the input method what the field
takes (GTK's input purpose and hints). In a browser they're the input's
`inputmode`, `enterkeyhint`, `autocomplete`, `autocapitalize` and
`spellcheck`, which a phone's keyboard follows.

**Lines of text.** `Multiline` makes it a text area, for a message or a
comment: return starts a new line instead of calling `OnSubmit`, and it
grows with its text up to `Style.Lines` lines (0: no limit), then
scrolls.

```go
message := signal.New("")
return ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd},
	ui.TextField(ui.TextFieldProps{Value: message, Placeholder: ui.Val("Message"), Multiline: true,
		Style: ui.Style{Grow: 1, Lines: 5}}),
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled,
		Disabled: signal.Func(func() bool { return message.Get() == "" }),
		OnPress:  func() { message.Set("") }}),
)
```

**The text alone.** `Plain` drops the field's border, background and
inset: its text and placeholder, for a field in a card or a form of
your own, as Notes' and Reminders' are.

```go
ui.TextField(ui.TextFieldProps{Value: note, Placeholder: ui.Val("Note"), Multiline: true, Plain: true})
```

A `SafeArea` is padded by the keyboard, so a field at the bottom of one
stays above it.

## Controls

Each control takes the signal it changes, and a `Disabled` accessor.
None of them has a label of its own: put a `Text` beside it.

```go
return ui.Column(ui.Style{Gap: 16},
	ui.Row(ui.Style{Align: ui.AlignCenter},
		ui.StaticText("Wi-Fi", ui.Style{Grow: 1}),
		ui.Toggle(ui.ToggleProps{Value: wifi})),
	ui.Slider(ui.SliderProps{Value: volume}),
	ui.Segmented(ui.SegmentedProps{Segments: ui.Val([]string{"Day", "Week", "Month"}), Selected: period}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%.0f cups", cups.Get()) }), ui.Style{Grow: 1}),
		ui.Stepper(ui.StepperProps{Value: cups, Min: 1, Max: 8})),
	ui.Wheel(ui.WheelProps{Items: ui.Val(sizes), Selected: size}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Tint", ui.Style{Grow: 1}),
		ui.ColorPicker(ui.ColorPickerProps{Value: tint, Alpha: true, Title: ui.Val("Tint")})),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Starts", ui.Style{Grow: 1}),
		ui.DatePicker(ui.DatePickerProps{Value: when, Mode: ui.DateAndTime, Min: time.Now()})),
)
```

- **Toggle** is a switch, `*signal.Signal[bool]`. With `Checkbox` it's a
  checkbox: Android's `MaterialCheckBox`, a Mac's and Windows' check
  box, a `GtkCheckButton`, a browser's check box, and on iOS, which has
  none, a circle that fills with a checkmark, as Reminders' does.
- **Slider** picks a `float64` from `Min` to `Max`; with both 0, from 0
  to 1.
- **Segmented** is one of a few options side by side, `Selected` the
  index: UIKit's segmented control, Material's segmented buttons.
  Windows has none: the host draws one, as the Windows Community
  Toolkit's `Segmented`.
- **Stepper** steps a `float64` with minus and plus, by `Step` (default
  1), from `Min` to `Max` (both 0: 0 to 100). It's a `UIStepper` on
  iOS and an `NSStepper` on macOS; elsewhere it's two buttons (Material
  icon buttons on Android, linked buttons on Linux, as GNOME's spin
  buttons show them).
- **Wheel** picks one of many `Items` on a spinning wheel:
  `UIPickerView`, Android's `NumberPicker`. The desktop and the web
  have no wheel: it's a pop-up menu of the items there (`NSPopUpButton`,
  a drop-down combo box, `GtkDropDown`, `<select>`).
- **ColorPicker** is a swatch that opens the platform's picker, writing
  a `ui.Color`. `Alpha` lets the user pick an opacity, `Title` names the
  picker. On iOS it's `UIColorWell` and the system's picker; Android has
  none, so it's a Material dialog of a palette, with an opacity slider
  for `Alpha`. On macOS it's an `NSColorWell` opening the color panel,
  on Linux a `GtkColorDialogButton`, in a browser `<input
  type="color">`. Windows' color dialog has no opacity, so `Alpha` does
  nothing there.
- **DatePicker** picks a `time.Time`, shown in the device's time zone.
  `Mode` is `ui.DateOnly` (the default), `ui.TimeOnly` or
  `ui.DateAndTime`; `Min` and `Max` bound it. A zero `Value` shows the
  current time.

  | | Compact (default) | `Inline` |
  |---|---|---|
  | iOS | UIKit's compact date picker | the calendar and time in place, for a sheet of its own |
  | Android | buttons showing the date and time, opening Material's pickers | the same buttons |
  | macOS | `NSDatePicker`'s text field and stepper | its calendar and clock |
  | Windows | the date and time picker, its calendar dropping down | a month calendar, a time picker under it for `DateAndTime` (`TimeOnly` stays compact) |
  | Linux | a button opening a popover of GTK's calendar and hour and minute spin buttons | the calendar and spin buttons in place |
  | Web | `<input type="date">`, `"time"`, `"datetime-local"` | the same input |

## Progress and activity

`ui.Progress` is a progress bar: its value from 0 to 1, or `nil` when
how far is unknown. Its style's `Color` is its fill. `ui.Activity` is a
spinner.

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Progress(done),
	ui.Progress(nil),
	ui.Activity(),
)
```

## Images

`ui.Image` shows a picture. The platform loads and decodes it off the
main thread, and caches it in memory. iOS and Android keep an image that
came over http(s) on disk too, across launches; the desktop hosts keep
it in memory only, and a browser in its own cache. Windows decodes PNG,
JPEG and GIF; Linux those and what GDK's loaders read.

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Image(ui.ImageProps{Source: ui.Val("logo.png"), Style: ui.Style{Width: ui.Pt(64)}}),
	ui.Image(ui.ImageProps{
		Source:      ui.Val("https://picsum.photos/id/1015/600/400"),
		Placeholder: "placeholder.png",
		Mode:        ui.ImageFill,
		Label:       ui.Val("A river in a valley"),
		Style:       ui.Style{AspectRatio: 3.0 / 2, CornerRadius: 12, Clip: true},
	}),
)
```

- `Source` is an http(s) URL, a path in the app's `assets` directory
  (`"logo.png"` is `assets/logo.png`), or an absolute path: a file the
  app wrote, a photo the user picked.
- `Mode` is how it fills its box: `ImageFit` (the default, all of it,
  letterboxed), `ImageFill` (covers the box, cropped), `ImageStretch`
  (the box's aspect ratio), `ImageCenter` (its natural size, centered).
- `Placeholder` is shown until `Source` loads, and if it fails. Use an
  asset, which is there at once. Give the image a size so the
  placeholder has room.
- `Label` describes it to screen readers; without one, the image is
  decorative.
- `OnLoad` gets its natural size once it's loaded; `OnError` a message
  when it can't be.

Until it has loaded, an image without a size is empty. Then it takes its
natural size, one point per pixel, no wider than the space it has. With
one side set (a `Width`, or `Grow` in a row) it keeps its aspect ratio;
`AspectRatio` sets one before it loads.

## Symbols and icons

`ui.Symbol` shows a symbol. It's an SF Symbol on iOS and macOS, the
Material Symbol for it on Android and the web, the Segoe Fluent Icons
glyph for it on Windows (Segoe MDL2 Assets before Windows 11), and on
Linux the icon theme's symbolic icon for it (Adwaita's), else the
Material Symbol. It's sized as text in its style's `Font`, so
it follows the text size, and colored with its `Color` (the label color
by default).

```go
return ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
	ui.Symbol(ui.IconBell),
	ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: colors.Yellow, Font: fonts.Title}),
	ui.Symbol(ui.Icon("archivebox"), ui.Style{Color: colors.SecondaryLabel}),
)
```

A `ui.Icon` is an SF Symbol name. Buttons, menu items, bar buttons and
tabs take one too. The `Icon` constants are the common ones:
`IconHome`, `IconSearch`, `IconSettings`, `IconPlus`, `IconClose`,
`IconTrash`, `IconShare`, `IconMore` and about fifty more.
`icon.Filled()` is its filled form (`"star.fill"`).

iOS and macOS draw any SF Symbol. The others draw a curated set: the
constants and more, mapped to Material Symbols in
[`platform/android/gen_symbols.go`](../platform/android/gen_symbols.go)
(the web's and Linux's are generated from it), and to Segoe Fluent
Icons' glyphs on Windows. A name outside the set shows nothing there
(the host logs a warning), so check a name you write as a string, like
`"archivebox"`, on each platform you ship. On Linux a name ending in
`-symbolic` is an icon theme's name, used as it is.

### SVG icons

For icons of your own, package `asuka/svg` draws SVG with the platform's
vector drawing; nothing is rasterized. `svg.MustParse` reads the markup
(`svg.Parse` returns an error instead), and `svg.View` draws it.

```go
var download = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>`)

func downloadIcon() ui.Node {
	return svg.View(svg.Props{Icon: download, Color: ui.Val(colors.Accent), Label: ui.Val("Download"),
		Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
}
```

`Color` paints what the SVG paints with `currentColor`, and fills it
doesn't give; it's `colors.Label`, the text color, by default. Without a
width or height in its `Style`, the view is the SVG's own size; with
one, the other keeps the aspect ratio. Keep an icon in a file with
`//go:embed` and parse it once, in a package variable.

It reads what icon sets use (Lucide, Material, Heroicons, Feather):
paths, basic shapes, groups, `use`, fills, strokes and transforms. It
skips text, gradients (painted with the icon's color), clipping, masks,
filters and `fill-rule="evenodd"`.

## Video

`ui.Video` plays a video with the platform's player and its controls:
`AVPlayerViewController` on iOS, Media3's ExoPlayer on Android,
`AVPlayerView` on macOS, a `GtkVideo` (GStreamer) on Linux, a
`<video>` in a browser. Win32 has no video view, so on Windows it's a
`<video>` in WebView2, with Edge's controls. `ui.Play`, `ui.Pause` and
`ui.Seek` drive it.

```go
var v ui.Node
v = ui.Video(ui.VideoProps{
	Source:   ui.Val("https://example.com/clip.mp4"),
	AutoPlay: true, Muted: true, Loop: true,
	Style: ui.Style{CornerRadius: 12},
})
return ui.Column(ui.Style{Gap: 12}, v,
	ui.Button(ui.ButtonProps{Title: ui.Val("Start over"), OnPress: func() { ui.Seek(v, 0); ui.Play(v) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Pause"), OnPress: func() { ui.Pause(v) }}),
)
```

- `Source` is an http(s) URL (a file, an HLS stream) or a file in the
  app's assets.
- `Mode` is `ui.ImageFit` (letterboxed, the default) or `ui.ImageFill`.
- `HideControls` hides the platform's controls: play, the timeline, full
  screen, and on iOS AirPlay and picture in picture.
- `OnEnd` is called each time it plays to its end; `OnError` if it
  can't play.

It sizes itself as an `Image` does: empty until the video's size is
known, unless its style gives it one, then keeping the video's aspect
ratio. `Seek` takes seconds from the start.

## Web pages

`ui.WebView` shows a web page in the platform's web view, with its
scrolling, zooming and selection: `WKWebView` on iOS and macOS,
Android's `WebView`, WebView2 (Edge's runtime) on Windows, WebKitGTK
6.0's `WebKitWebView` on Linux. Without WebKitGTK installed, it shows
nothing on Linux and calls `OnError` once, saying what to install. In a
browser it's an `<iframe>`, which can do all of what follows only for
pages of the app's own site; see [the web](web.md#web-views). With no
height and no `Grow` it grows to fill the space it's given.

```go
state := signal.New(ui.WebState{})
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	URL:       ui.Val("https://go.dev"),
	Block:     []string{"doubleclick.net", "/ads/"},
	OnState:   func(s ui.WebState) { state.Set(s) },
	OnMessage: func(m string) { fmt.Println("the page says", m) },
	OnNewWindow: func(url string) {
		ui.OpenURL(url)
	},
})
return ui.Column(ui.Style{Grow: 1},
	ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 12), Align: ui.AlignCenter},
		ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: ui.Val("Back"),
			Disabled: signal.Func(func() bool { return !state.Get().CanGoBack }),
			OnPress:  func() { ui.WebBack(web) }}),
		ui.Text(signal.Func(func() string { return state.Get().Title }), ui.Style{Grow: 1, Lines: 1}),
		ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Reload"), OnPress: func() { ui.WebReload(web) }}),
	),
	ui.Show(signal.Func(func() bool { return state.Get().Loading }), func() ui.Node {
		return ui.Progress(signal.Func(func() float64 { return float64(state.Get().Progress) }))
	}),
	web,
)
```

What it shows:

- `URL` is an http(s) URL, or a file in the app's assets (its relative
  links are the assets' too). `HTML` is markup to show instead.
- `UserAgent` is what pages are fetched with, and
  `navigator.userAgent`; empty, the platform's.
- `Preload` is script run before each page's own, in every frame: to
  hook `fetch`, or set globals.
- `Block` keeps requests from loading, as an ad blocker does. Each entry
  is a host, which matches its subdomains too (`"doubleclick.net"`), or,
  with a `/`, a part of the URL (`"/ads/"`).

`Preload`, `UserAgent` and `Block` are set when the view is made.

What it reports:

- `OnLoad` gets the URL of each page that has loaded; `OnError` a
  message when one couldn't.
- `OnState` follows the page, as an address bar does: a `ui.WebState`
  with its `URL`, `Title`, `Loading`, `Progress` (0 to 1), `CanGoBack`
  and `CanGoForward`.
- `OnNewWindow` gets the URL the page would open in a new window
  (`window.open`, a `target="_blank"` link). Without it, that page loads
  in this view.
- `OnDownload` gets a response the page can't show, a file to save
  (a link to a `.zip`, a `Content-Disposition: attachment`), as a
  `ui.WebDownload` with its `URL`, `MIME`, `Filename` and `Size` (-1 when
  unknown). It doesn't download it.
- `OnRequest` gets the URL of each request the page makes (scripts,
  images, media, `fetch`). **Not on iOS and macOS**: `WKWebView`
  doesn't let an app see them.

`ui.WebBack`, `ui.WebForward`, `ui.WebReload` and `ui.WebStop` move
through its history, load the page again, or stop it loading.

### Talking to the page

The page posts to Go with `window.asuka.postMessage(text)`, which calls
`OnMessage`. Go runs script in the page with `ui.EvalJS`, which returns
nothing, or `ui.EvalJSResult`, which calls back with the value as JSON:
the last statement's, or what a returned promise resolves to (`null` for
`undefined`), or the error it threw.

```go
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	HTML: ui.Val(`<button onclick="asuka.postMessage('tapped')">Tap</button>`),
	OnMessage: func(m string) {
		ui.EvalJSResult(web, "document.title", func(v json.RawMessage, err error) {
			fmt.Println(m, string(v), err)
		})
	},
})
```

`ui.WebCookies(url, done)` gets the cookies web views have for a URL, as
a `Cookie` header: what the user signed in to in a page, for your own
requests.

[`examples/downloader`](../examples/downloader) uses all of this: a
browser with an address bar, a preload script, blocking and downloads.

## Shapes

`ui.Shape` draws an outline, filled, stroked or both. `Path` makes the
outline for the shape's size; it runs again when the size changes, and
when signals it reads change. A shape takes the size its style gives it
(a width and height, an aspect ratio, `Grow`); with none, it's empty.

```go
ui.Shape(ui.ShapeProps{
	Path: func(w, h float32) *ui.Path {
		return new(ui.Path).MoveTo(w/2, 0).LineTo(w, h).LineTo(0, h).Close()
	},
	Fill:  colors.Orange,
	Style: ui.Style{Width: ui.Pt(48), Height: ui.Pt(48)},
})
```

A `ui.Path` is in points from the shape's top left. Its methods add to
it and return it, to chain: `MoveTo`, `LineTo`, `QuadTo`, `CubicTo`,
`Arc` (angles in radians, clockwise from the x axis), `Close`, and whole
figures, `Rect`, `RoundedRect` and `Ellipse`.

`Stroke` and `LineWidth` (default 1) draw the outline, with a `LineCap`
(`CapButt`, `CapRound`, `CapSquare`) and a `LineJoin` (`JoinMiter`,
`JoinRound`, `JoinBevel`). `TrimStart` and `TrimEnd` draw part of it,
from 0 to 1: a progress ring, a line that draws itself. Changing them in
`ui.Animate` animates them (see [Motion](motion.md)).
`Dash` dashes the stroke: lengths in points, drawn and skipped in turn
(`[]float32{6, 4}`; an odd count repeats), as SwiftUI's
`StrokeStyle(dash:)` does. `DashPhase` is how far into the pattern it
starts. [`examples/usage`](../examples/usage) draws a forecast with
one.

```go
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

func progressRing(done signal.Accessor[float32]) ui.Node {
	box := ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pct(100), Height: ui.Pct(100)}
	return ui.View(ui.ViewProps{Label: ui.Val("Progress"), Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.SecondaryBackground, LineWidth: 8, Style: box}),
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.Accent, LineWidth: 8, LineCap: ui.CapRound,
			TrimEnd: done, Style: box}),
	)
}
```

### Canvas

`ui.Canvas` draws what the app draws: a chart, a gauge, a signature, a
game's board, whatever no view is. Its function draws with a
`*ui.Graphics`, in points from its top left:

```go
ui.Canvas(func(g *ui.Graphics) {
	w, h := g.Size()
	g.Fill(g.Path().RoundedRect(0, 0, w, h, 12), colors.SecondaryBackground)
	g.Fill(g.Path().RoundedRect(0, 0, w*level.Get(), h, 12), colors.Accent)
	g.Text(fmt.Sprintf("%.0f%%", level.Get()*100), w/2, h/2+6, ui.TextStyle{Weight: fonts.Semibold, Align: ui.TextCenter})
}, ui.Style{Height: ui.Pt(36)})
```

- `g.Path()` is a path to build (as a Shape's), reused from draw to draw.
- `Fill`, `Stroke` (a `StrokeStyle`: width, caps, joins, dashes) and
  `FillGradient` paint a path; `Text` draws a line of text, its baseline
  at y, its left edge, center or right edge at x.
- `Save` and `Restore` bracket `Translate`, `Scale`, `Rotate` (degrees,
  clockwise), `Clip` and `Opacity`.
- Colors can be semantic (`colors.Label`, `colors.Separator`, `colors.Accent`):
  they follow dark mode.

It draws once it's laid out, and again when its size changes or the
signals the function read change, like an effect. Each draw records a
display list, sent only when it differs from the last, and the platform
replays it: Core Graphics on iOS and macOS, Android's `Canvas`, GDI+ on
Windows, GTK's snapshots (GSK) on Linux, a 2D `<canvas>` in a browser.
Recording allocates nothing once the canvas has drawn as much
before. A canvas has no presses of its own: wrap it in a `ui.View` with
`OnPress` or `OnPan`, which report points in its coordinates. It isn't
read by screen readers: give the view around it a `Label`.

### Charts

`asuka/chart` draws charts on a Canvas: bars, lines, areas and points
over a value axis, with the platform's text for their labels (measured
by it, scaled with the user's text size).

```go
chart.View(chart.Props{
	Series: ui.Val([]chart.Series{
		{Name: "Steps", Kind: chart.Bar, Values: steps},
		{Name: "Goal", Kind: chart.Line, Values: goal, Color: colors.Hex(0xff9500)},
	}),
	Labels: ui.Val([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}),
	Label:  ui.Val("Steps this week"),
	Style:  ui.Style{Height: ui.Pt(200)},
})
```

- A series has a value per label; `NaN` leaves a gap. Bar series stand
  side by side; areas, lines and points go over them. `Smooth` draws a
  monotone curve, which never overshoots its values.
- The value axis fits the values, rounded out to whole ticks (and down
  to zero for bars and areas), unless `Min` and `Max` fix it; `Format`
  writes its labels (`2.5k` by default).
- Colors default to the app's accent, then the system's.
- Screen readers read the chart as one image: its `Label`, then each
  series' values, whole.

### Shaders

`ui.Shader` is a box a GPU program draws, under its children: flowing
gradients, noise, glows, ripples. The program is a Kage shader
(Ebitengine's language: Go's syntax), which package `shader` compiles
once, as a package's variable:

```go
var glow = shader.MustCompile(`package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	d := distance(pos/rectSize(), vec2(0.5))
	g := smoothstep(0.5, 0, d) * (0.8 + 0.2*sin(Time*2))
	return vec4(Tint.rgb*g, 1) * Tint.a
}`)

ui.Shader(ui.ShaderProps{
	Program:  glow,
	Uniforms: ui.Val([]ui.Uniform{ui.ColorUniform("Tint", colors.Hex(0x38bdf8))}),
	Animated: true,
	Style:    ui.Style{Height: ui.Pt(200), CornerRadius: 16, Clip: true, Background: colors.Hex(0x0b1030)},
})
```

- `Fragment` returns the color of a pixel, premultiplied, red, green,
  blue and alpha from 0 to 1. `pos` is the pixel's center in points
  from the box's top left, `dst` the same in pixels; `rectSize()` is
  the box's size in points and `pixelScale()` the pixels a point.
- The language has `bool`, `int`, `float`, `vec2` to `vec4`, `ivec2` to
  `ivec4`, `mat2` to `mat4` and arrays of them, GLSL's built-in functions
  (`sin`, `mix`, `smoothstep`, `dot`...), functions of its own, `if` and
  `for`. Exported variables are uniforms.
- `Uniforms` sets them by name: a scalar's one number, a vector's
  components, a matrix's columns in turn. A change draws the box again;
  a uniform left out is 0.
- `var Time float` is the seconds since the shader showed. `Animated`
  runs it: the box draws at every frame the display draws, with nothing
  running in Go.
- `shader.Compile` reports a shader's errors with their lines, and what
  of it Android can't run, on every platform alike: AGSL is GLSL ES
  1.00's language, so no unsigned integers, bitwise operations or
  integer remainders (`mod` of floats does), derivatives, or loops
  between values that aren't constants.

It's Metal on iOS and macOS (a `CAMetalLayer`), AGSL on Android (a
`RuntimeShader`) and WebGL 2 in a browser (the program translated to
GLSL ES 3.00). Windows and Linux run the same program on the CPU,
across the cores and off the main thread: a frame shows when it's
ready, and a large or animated box renders at a fraction of its
resolution, scaled up. Give the box a `Background` that stands in until
the first frame.

## Containers

`ui.View` holds other views. `ui.Column` and `ui.Row` are views that
stack their children top to bottom and leading to trailing. `ui.SafeArea`
is a column padded by the notch, the system bars and the keyboard. How
they size and place what's in them is in [Layout](layout.md).

A view with no look, events or accessibility props makes no native view
of its own: its children go in the nearest one that does. So nesting
rows and columns costs nothing.

```go
open := signal.New(false)
return ui.View(ui.ViewProps{
	OnPress:     func() { open.Set(!open.Peek()) },
	OnLongPress: func() { ui.Haptic(ui.HapticMedium) },
	Label:       ui.Val("Ada, 2 unread"),
	Style:       ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12},
},
	ui.Symbol(ui.IconPerson.Filled()),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada", ui.Style{Font: fonts.Headline}),
		ui.StaticText("2 unread", ui.Style{Color: colors.SecondaryLabel}),
	),
)
```

`ViewProps` has:

- `OnPress` and `OnLongPress`.
- Gestures, with the platform's recognizers: `OnPan`, `OnPinch`,
  `OnRotate` (each given a `ui.Gesture`) and `OnSwipe` (a
  `ui.SwipeDirection`). See [Motion](motion.md).
- Interaction states. The view's own style follows them through its
  variants (`Style.Pressed`, `Hovered`, `Dragging`, `DragOver`). Other
  views follow them through `Interaction` (a `ui.Interaction`, read
  with `Style.When` or in a `StyleFunc`). Side effects go in
  `OnPressedChange` and `OnHoverChange`. Drag and drop: `Drag` (a
  `ui.DragSource`) and `Drop` (a `ui.DropTarget`). See
  [Motion](motion.md#pressed-and-hover-states).
- `OnLayout`, called with the view's `ui.Frame` once it's laid out and
  each time that changes.
- Accessibility: `Label`, `Hint`, `Role`, `Decorative`, `Actions` and
  `OnAdjust`. See [Accessibility](accessibility.md).

### Scroll

`ui.Scroll` scrolls its children along one axis. Give it a size, or
`Grow`, so it has something to scroll within. `Style` is the scroll
view's box; `ContentStyle` is the box its children are stacked in.

```go
down := signal.New(false)
var scroll ui.Node
scroll = ui.Scroll(ui.ScrollProps{
	Style:    ui.Style{Grow: 1},
	OnScroll: func(_, y float32) { down.Set(y > 300) },
}, ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16}, content...))
return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, scroll,
	ui.Show(down, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Right: ui.Pt(20), Bottom: ui.Pt(20)}},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Back to top"), Variant: ui.ButtonFilled,
				OnPress: func() { ui.ScrollTo(scroll, 0, 0, true) }}))
	}))
```

- `OnScroll` is called as it scrolls, with its content offset in points.
- `ui.ScrollTo(scroll, x, y, animated)` scrolls to an offset (past the
  end is the end); `ui.ScrollToEnd(scroll, animated)` to its bottom, or
  a horizontal one's trailing edge. Both work on a `List` too.
- `Horizontal` scrolls sideways:

```go
ui.Scroll(ui.ScrollProps{Horizontal: true, ContentStyle: ui.Style{Gap: 8, Padding: ui.Symmetric(0, 16)}}, kids...)
```

**Pages.** `Paging` makes it rest only at whole pages: a carousel, an
onboarding's steps, a feed of full-screen videos. Each child of its
content is a page, as big as the scroll view along its axis. `Page` is
the page shown: a swipe writes it as it settles, and writing it scrolls
there. `ui.PageControl` shows where it is, a dot per page, and moves it
too when one is tapped:

```go
page := signal.New(0)
return ui.Column(ui.Style{Gap: 8},
	ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(200)}},
		card(1), card(2), card(3)),
	ui.PageControl(ui.PageControlProps{Count: ui.Val(3), Page: page}))
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| Paging | `isPagingEnabled` | each swipe snaps to the next page, or back | settles on the nearest page when a swipe ends | a page per wheel notch (FlipView's) | a page per wheel notch; a touchpad's or touchscreen's swipe settles on a page; the arrow keys, Page Up and Page Down move a page | CSS scroll snapping |
| `PageControl` | `UIPageControl` | dots in the theme's primary color (Material has none) | dots in the accent color | pips (PipsPager's) | dots drawn as libadwaita's, the current one larger and darker | dots, the current one in the label color |

Pages count from the reading start: right to left, the first is the
rightmost. When the scroll view's size changes (a rotation), it stays on
its page. Screen readers say "page 2 of 3" on the dots and adjust them.

**Pull to refresh.** `OnRefresh` turns it on (`UIRefreshControl`,
`SwipeRefreshLayout`). Set `Refreshing` true while you refresh; the
spinner hides when it turns false. Without `Refreshing`, it hides as
soon as `OnRefresh` returns. On macOS a trackpad's or Magic Mouse's
scroll pulls it past its top, as on iOS. Windows and Linux refresh the
desktop's way: F5 (on Linux, Ctrl+R too) refreshes the scroll view the
focus is in, or else the innermost one on screen, a spinner over its
top meanwhile. In a browser a touch pulls it down.

```go
refreshing := signal.New(false)
return ui.Scroll(ui.ScrollProps{
	Style:      ui.Style{Grow: 1},
	Refreshing: refreshing,
	OnRefresh: func() {
		refreshing.Set(true)
		go func() {
			time.Sleep(time.Second) // fetch
			ui.Post(func() { refreshing.Set(false) })
		}()
	},
}, ui.StaticText("Pull down"))
```

**A bar on the keyboard.** `Accessory` is a bar shown on top of the
keyboard while the field is focused: a count and Done, formatting
buttons, quick replies (SwiftUI's keyboard toolbar). It's as wide as the
window and as tall as its content, built with the field and disposed
with it.

```go
var field ui.Node
field = ui.TextField(ui.TextFieldProps{Value: note, Multiline: true, Accessory: func() ui.Node {
	return ui.Row(ui.Style{Padding: ui.Symmetric(6, 16), Align: ui.AlignCenter, Material: ui.MaterialBar},
		ui.Text(signal.Func(func() string { return fmt.Sprint(len(note.Get()), " characters") }), ui.Style{Grow: 1}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Done"), OnPress: func() { ui.Blur(field) }}))
}})
```

On iOS it's the field's `inputAccessoryView`: it moves with the
keyboard, through its interactive dismissal too. Android has none of its
own: the bar goes in the field's window (a sheet's too) at the top of the
keyboard, following it frame by frame. macOS, Windows and Linux, with no
on-screen keyboard, don't show it; a browser shows it only on a touch
screen.

**Styled text.** `Rich` makes a field a text area of styled text, for a
mail's body or a formatted message: bold, italic, underline,
strikethrough, monospaced code and links (colors and text styles a span
has are kept as they are). It's the same `ui.Rich` a `RichText` shows.
The user's typing and formatting write it; writing it replaces the text.
`ui.ToggleFormat(field, ui.FormatBold)` turns a style on for the
selection unless all of it has it, then off (`FormatText` sets it either
way); with no selection, it's what's typed next. `ui.SetLink(field, url)`
links the selection ("" unlinks; at the cursor, the link it's in).
`OnFormats` reports the styles where the cursor is, or that all the
selection has, for a toolbar to show.

```go
body := signal.NewFunc(ui.Rich{ui.Plain("Hi,\n\nThe "), ui.Strong("1.0"), ui.Plain(" build is ready.")}, nil)
formats := signal.New(ui.TextFormats{})
var field ui.Node
field = ui.TextField(ui.TextFieldProps{Rich: body, Plain: true, OnFormats: formats.Set, Style: ui.Style{Grow: 1}})
bold := ui.Button(ui.ButtonProps{Icon: "bold", Label: ui.Val("Bold"),
	Style:   ui.StyleFunc(func() ui.Style { if formats.Get().Has(ui.FormatBold) { return ui.Style{Background: colors.SecondaryBackground} }; return ui.Style{} }),
	OnPress: func() { ui.ToggleFormat(field, ui.FormatBold) }})
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| The view | `UITextView`, its attributed text | `EditText`, its spans | `NSTextView`, rich | a rich edit (`RICHEDIT50W`), through RTF | `GtkTextView`, its buffer's tags | a `contenteditable` element |
| The user's formatting | the edit menu's B/I/U | a hardware keyboard's Ctrl+B, Ctrl+I, Ctrl+U | the Format menu, ⌘B ⌘I ⌘U | Ctrl+B, Ctrl+I, Ctrl+U | Ctrl+B, Ctrl+I, Ctrl+U | the browser's ⌘B ⌘I ⌘U (Ctrl elsewhere) |
| Undo of formatting | yes | yes: the field's own history (Android's keeps text only), Ctrl+Z, Ctrl+Shift+Z, Ctrl+Y | yes | yes | GTK's history (Ctrl+Z, Ctrl+Shift+Z), which keeps text only | yes: the page's editing commands |

[`examples/mail`](../examples/mail) has a composer with a toolbar.

**Padding.** A text's or a field's `Padding` (and `BorderWidth`) is
inside its frame, around its content, as in CSS: its background fills
it, its text is drawn inside it, and a field's editing happens inside it.

## Lists

`ui.List` shows a row per item in the platform's recycling list
(`UICollectionView`, `RecyclerView`). The desktop and the web have no
list that holds rows of any view, so there the host recycles rows
itself, in the platform's scroll view. Rows are made only for the cells
on screen; a cell scrolled away is reused for another item, and only the
props that differ are updated. [`examples/list10k`](../examples/list10k)
has ten thousand rows.

```go
messages := signal.NewFunc(fetch(0, 30), nil)
return ui.List(ui.ListProps{}, messages, func(m message) int { return m.ID }, row)
```

```go
func row(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
	return ui.Column(ui.Style{Gap: 2, Padding: ui.Symmetric(12, 20)},
		ui.Text(signal.Func(func() string { return m.Get().From }), ui.Style{Font: fonts.Headline}),
		ui.Text(signal.Func(func() string { return m.Get().Subject }), ui.Style{Color: colors.SecondaryLabel}),
	)
}
```

It takes the items, a key for each, and a function that builds a row
from an item and its index, both accessors. Keys say which item is
which from one list to the next: a changed item updates its row, a moved
one moves it, and the platform animates the difference. Read the item in
a `signal.Func`, as above, so a reused row follows its new item.

A list fills the space it's given: with no height and no `Grow`, it
grows. It draws the platform's separators between rows.

`ListProps` has:

- `SwipeActions`, the actions of row `i`, revealed by swiping it. On
  iOS they are its trailing actions, a destructive one red, the first
  run by a full swipe. On Android a full swipe runs the first, over its
  color and icon, as Material's swipe to dismiss. On macOS, Windows,
  Linux and the web they're the row's context menu (a secondary click;
  on Linux and the web a long press too), leading then trailing. Give the
  row the same as accessibility `Actions`, for screen readers.
- `OnEndReached`, called when the last rows are about to show, once for
  each length the list has: load more there.
- `OnRefresh` and `Refreshing`, pull to refresh as on a `Scroll` (F5 on
  Windows and Linux).
- `OnScroll`, as on a `Scroll`.
- `Appearance`, its look. `ui.ListPlain` is rows edge to edge, apart by
  separators; `ui.ListInsetGrouped` is the platform's grouped list
  (libadwaita's boxed lists on Linux);
  `ui.ListBare` has no separators and nothing behind its rows, so what's
  behind the list shows through, for rows that draw their own cards. A bare
  list's rows leave their own space, and can still be swiped.

```go
messages := signal.NewFunc(fetch(0, 30), nil)
remove := func(i int) {
	messages.Update(func(ms []message) []message { return slices.Delete(slices.Clone(ms), i, i+1) })
}
list := ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
			{Title: ui.Val("Archive"), Icon: "archivebox", OnPress: func() { remove(i) }},
		}
	},
	OnEndReached: func() {
		ms := messages.Peek()
		messages.Set(append(slices.Clone(ms), fetch(len(ms), 30)...))
	},
}, messages, func(m message) int { return m.ID }, row)
```

`LeadingSwipeActions` are revealed by swiping the other way, from the
row's leading edge (Mail's Unread, Pin): the first is at that edge, and
a full swipe's. iOS tints them; Android shows them as Material's
filled buttons.

`ui.ScrollToItem(list, index, animated)` scrolls a list so an item
shows; `ScrollTo` and `ScrollToEnd` work too.

### Chats: a list anchored at its bottom

`Anchor: ui.AnchorBottom` is a chat's list: it opens at its last row,
rows that don't fill it sit at its bottom, and while it's at its end it
stays there: as rows are added at the end, as they change height, and
as the list gets shorter (the keyboard). The user scrolling up leaves
the end; scrolling back to it, or `ui.ScrollToEnd`, sticks again.
`OnStartReached` is called when the first rows are about to show, once
for each first item: load older messages there.

Whatever the anchor, rows added or removed above the ones on screen
don't move those (React Native's `maintainVisibleContentPosition`):
older messages loaded at the top, or an item removed above, leave the
user reading what they were. Only at the edge the list is anchored to
does it show the new rows: at the top of a list read from the top (a
new mail), at the end of a chat.

```go
msgs := signal.NewFunc(latest(40), nil)
var list ui.Node
list = ui.List(ui.ListProps{
	Anchor:         ui.AnchorBottom,
	OnStartReached: func() { go loadOlder() }, // then ui.Post prepends them
}, msgs, func(m message) int { return m.ID }, bubble)
send := func(text string) {
	msgs.Set(append(slices.Clone(msgs.Peek()), message{Text: text}))
	ui.ScrollToEnd(list, true) // one's own message shows, wherever the list was
}
```

[`examples/chat`](../examples/chat) is a whole conversation.

### Sections

`Section` puts rows in sections: rows next to each other with the same
key are one, headed by `Header`'s view (a day in a chat, a letter in
contacts, Today and Yesterday in a mailbox). `Header` gets the key as an
accessor: a header is a row of its own, made as it comes into view and
reused for another section after.

```go
ui.List(ui.ListProps{
	Section: func(i int) string { return day(msgs.Peek()[i].At) },
	Header: func(day signal.Accessor[string]) ui.Node {
		return ui.Text(day, ui.Style{Font: fonts.Footnote.Weighted(fonts.Semibold), Padding: ui.Symmetric(6, 20)})
	},
}, msgs, key, row)
```

A plain or bare list's header stays at the top while its section's rows
scroll under it, and the next header pushes it up (UITableView's plain
style; on Android, which has none, a cell over the list, bound to the
section at the top; on the desktop and the web, the header row
itself). A plain list's header is on the list's background; a bare
list's draws its own (a chat's pill over the messages). An inset grouped
list's sections are groups of their own, each under its header, which
scrolls with it.

### Selecting rows

`Selecting` puts a list in selection mode (Mail's Edit): each row shows
the platform's selection control, a tap selects or deselects it instead
of pressing it, and rows don't swipe. `Selected` says which rows are
(it's followed in an effect, so a selection kept in signals shows as it
changes), and `OnSelect` hears the user's choices; what the app doesn't
take goes back. The rows are laid out narrower by the control's room.

```go
selecting := signal.New(false)
chosen := signal.NewFunc(map[int]bool{}, nil) // by message ID
ui.List(ui.ListProps{
	Selecting: selecting,
	Selected:  func(i int) bool { return chosen.Get()[msgs.Peek()[i].ID] },
	OnSelect: func(i int, v bool) {
		m := maps.Clone(chosen.Peek())
		if v {
			m[msgs.Peek()[i].ID] = true
		} else {
			delete(m, msgs.Peek()[i].ID)
		}
		chosen.Set(m)
	},
	OnSelectBegin: func(i int) { selecting.Set(true) },
}, msgs, key, row)
```

| | iOS | Android | macOS, Windows, Linux | Web |
|---|---|---|---|---|
| The control | UIKit's edit mode circles | a Material checkbox at the row's end | the row highlighted (the accent's selection color; a WinUI list's pill on Windows) | a circle at the row's leading edge that fills with a checkmark, the row tinted |
| `OnSelectBegin` | a two-finger pan over rows, which goes on to select the rows it crosses | a long press (Gmail's) | — | — |
| Screen readers | each row selectable | each row a checkbox, its text the label | — | each row selected or not (`aria-selected`) |

### Grids

`Columns` makes a list a grid (a `LazyVGrid`, a `GridLayoutManager`):
items fill rows of that many, each as wide as the others, `Gap` apart
across and down. A row is as tall as its tallest item. A grid has no
separators and no swipe actions.

```go
ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
	func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
		return ui.Image(ui.ImageProps{
			Source: signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
			Mode:   ui.ImageFill,
			Style:  ui.Style{AspectRatio: 1},
		})
	})
```

### List or For in a Scroll

`ui.For` makes a row per item too, keyed the same way, but it makes all
of them, as plain views among their siblings. Use it in a `Scroll` for
a short list that's part of a page: a few tags, a form's rows, a
section under a header.

```go
ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.All(20), Gap: 8}},
	ui.StaticText("Tags", ui.Style{Font: fonts.Headline}),
	ui.For(tags, func(t string) string { return t }, func(t signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
		return ui.Text(t)
	}),
)
```

Use `List` when the items are many or unbounded (a feed, an inbox,
search results, photos), or you want swipe actions, separators, loading
more at the end, or a grid. A `List` scrolls by itself: don't put it in
a `Scroll`.

## Your own views

When a platform has a widget Asuka doesn't, write it yourself and show it
with `ui.Native`: in Swift on iOS, Kotlin on Android, Go on macOS,
Windows and Linux (AppKit, Win32 and GTK through package `native`), and
JavaScript on the web. See [Native code](native.md).

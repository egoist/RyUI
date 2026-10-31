# Components

A component is a Go function that returns a `ui.Node`. This page covers
the views package `ui` gives you, grouped by what they do, and the
packages next to it for Markdown and SVG. Each one is the platform's own
widget where the platform has one (UIKit on iOS, Material 3 on Android).
How views are sized and placed is in [Layout](layout.md); screens, tabs
and sheets are in [Navigation](navigation.md).

Most props that can change take a `signal.Accessor`: pass a signal, a
`signal.Func`, or `ui.Val(v)` for a value that never changes. A control
that the user changes (a field, a toggle) takes a `*signal.Signal`
instead, and writes it. See [Concepts](concepts.md).

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
	ui.StaticText("Inbox", ui.Style{Font: ui.Title2.Weighted(ui.Bold)}),
	ui.Text(label, ui.Style{Color: ui.SecondaryLabel}),
	ui.StaticText("A long description that stops after two lines.", ui.Style{Lines: 2}),
)
```

The style's `Font`, `Color`, `TextAlign` and `Lines` are the text's.
Text styles (`ui.Body`, `ui.Headline`, `ui.Footnote`...) follow the
text size the user picked.

### RichText

`ui.RichText` shows styled text: a `ui.Rich`, a list of spans. A `"\n"`
in a span starts a new paragraph. A span's fields left zero come from
the `RichText`'s style.

```go
ui.RichText(ui.RichTextProps{
	Text: ui.Val(ui.Rich{
		ui.Strong("ryui"), ui.Plain(" draws "), ui.Em("native"), ui.Plain(" views. Run "),
		ui.Code("ryui dev"), ui.Plain(", or read "), ui.Link("the docs", "https://example.com/docs"),
		ui.Plain(".\nA second paragraph, "), ui.Plain("in red").Colored(ui.Red), ui.Plain("."),
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

Each paragraph is typeset by the platform's engine (CoreText,
StaticLayout) and cached by its content and width. Appending to a long
text, a reply streaming in, typesets only the paragraph that changed.
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
		return ui.View(ui.ViewProps{Style: ui.Style{Background: ui.Green, CornerRadius: 6, Padding: ui.Symmetric(2, 6)}},
			ui.StaticText("live", ui.Style{Font: ui.Caption, Color: ui.RGB(255, 255, 255)}))
	}),
	ui.Plain(" since this morning."),
})})
```

### Selectable text

`ui.Selectable` makes the `RichText` views inside it selectable, as one
text across all of them: a long press selects a word, the platform's
handles extend the selection, and its edit menu copies it.

```go
ui.Selectable(ui.SelectableProps{Style: ui.Style{Gap: 12}},
	ui.RichText(ui.RichTextProps{Text: ui.Val(question), Role: ui.RoleHeader}),
	ui.RichText(ui.RichTextProps{Text: ui.Val(answer)}),
)
```

`Text` is not selectable; use a `RichText` in a `Selectable`. To copy
something without a selection, `ui.Copy(text)` puts it on the clipboard.

### Markdown

Package `ryui/markdown` shows Markdown as native views, built from
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
iOS, a Material button on Android.

```go
saving := signal.New(false)
return ui.Row(ui.Style{Gap: 12, Wrap: true},
	ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, Disabled: saving,
		OnPress: func() { saving.Set(true) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonTinted}),
	ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: "Delete", Variant: ui.ButtonGray}),
)
```

- `Title` is its text; `Icon` a symbol before it (see
  [Symbols](#symbols-and-icons)). A button with an icon and no title is
  the icon alone.
- `Label` is what VoiceOver and TalkBack read, the title by default.
  Give an icon-only button one.
- `Disabled` disables it, the platform's way.
- `Style.Font` and `Style.Color` change its text and tint.

`Variant` is its look:

| Variant | iOS | Android |
| --- | --- | --- |
| `ButtonPlain` (default) | plain | text button |
| `ButtonFilled` | filled | filled button |
| `ButtonTinted` | tinted | tonal button |
| `ButtonGray` | gray | tonal button |
| `ButtonBordered` | bordered | outlined button |

### Menus

A button with `Menu` shows its actions when it's tapped, instead of
calling `OnPress` (a `UIMenu` on iOS, a `PopupMenu` on Android). Each
`ui.Action` has a `Title`, an optional `Icon` beside it, a `Role`, and
`OnPress`. `ui.ActionDestructive` shows it in red.

```go
ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: "More", Menu: []ui.Action{
	{Title: "Copy", Icon: ui.IconCopy, OnPress: func() { ui.Copy("https://go.dev") }},
	{Title: "Share", Icon: ui.IconShare, OnPress: func() { ui.Share("https://go.dev") }},
	{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {}},
}})
```

`ui.Menu` puts a menu on any content. With `LongPress` it's a context
menu, shown on a long press:

```go
ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
	{Title: "Reply", Icon: ui.IconMessage},
	{Title: "Delete", Role: ui.ActionDestructive},
}, Style: ui.Style{Padding: ui.All(16), Background: ui.SecondaryBackground, CornerRadius: 12}},
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
- `ContentType` is an AutoFill hint: `"username"`, `"password"`,
  `"email"`, `"name"`...
- `Autocapitalize`: `CapitalizeSentences` (the default),
  `CapitalizeNone`, `CapitalizeWords`, `CapitalizeCharacters`.
  `Autocorrect`: `AutocorrectDefault`, `AutocorrectYes`,
  `AutocorrectNo`.
- `OnFocus` and `OnBlur` follow the keyboard. `ui.Focus(field)` asks for
  it, `ui.Blur(field)` dismisses it.

**Lines of text.** `Multiline` makes it a text area, for a message or a
comment: return starts a new line instead of calling `OnSubmit`, and it
grows with its text up to `Style.Lines` lines (0: no limit), then
scrolls.

```go
message := signal.New("")
return ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd},
	ui.TextField(ui.TextFieldProps{Value: message, Placeholder: ui.Val("Message"), Multiline: true,
		Style: ui.Style{Grow: 1, Lines: 5}}),
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: "Send", Variant: ui.ButtonFilled,
		Disabled: signal.Func(func() bool { return message.Get() == "" }),
		OnPress:  func() { message.Set("") }}),
)
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
	ui.Segmented(ui.SegmentedProps{Segments: []string{"Day", "Week", "Month"}, Selected: period}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%.0f cups", cups.Get()) }), ui.Style{Grow: 1}),
		ui.Stepper(ui.StepperProps{Value: cups, Min: 1, Max: 8})),
	ui.Wheel(ui.WheelProps{Items: sizes, Selected: size}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Tint", ui.Style{Grow: 1}),
		ui.ColorPicker(ui.ColorPickerProps{Value: tint, Alpha: true, Title: "Tint"})),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Starts", ui.Style{Grow: 1}),
		ui.DatePicker(ui.DatePickerProps{Value: when, Mode: ui.DateAndTime, Min: time.Now()})),
)
```

- **Toggle** is a switch, `*signal.Signal[bool]`.
- **Slider** picks a `float64` from `Min` to `Max`; with both 0, from 0
  to 1.
- **Segmented** is one of a few options side by side, `Selected` the
  index: UIKit's segmented control, Material's segmented buttons.
- **Stepper** steps a `float64` with minus and plus, by `Step` (default
  1), from `Min` to `Max` (both 0: 0 to 100). It's a `UIStepper` on
  iOS; Android has none, so it's two Material icon buttons.
- **Wheel** picks one of many `Items` on a spinning wheel:
  `UIPickerView`, Android's `NumberPicker`.
- **ColorPicker** is a swatch that opens the platform's picker, writing
  a `ui.Color`. `Alpha` lets the user pick an opacity, `Title` names the
  picker. On iOS it's `UIColorWell` and the system's picker; Android has
  none, so it's a Material dialog of a palette, with an opacity slider
  for `Alpha`.
- **DatePicker** picks a `time.Time`, shown in the device's time zone.
  `Mode` is `ui.DateOnly` (the default), `ui.TimeOnly` or
  `ui.DateAndTime`; `Min` and `Max` bound it. On iOS it's UIKit's
  compact date picker; on Android, buttons showing the date and time
  that open Material's pickers. A zero `Value` shows the current time.

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
main thread, and caches it: in memory, and on disk across launches when
it came over http(s).

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Image(ui.ImageProps{Source: ui.Val("logo.png"), Style: ui.Style{Width: ui.Pt(64)}}),
	ui.Image(ui.ImageProps{
		Source:      ui.Val("https://picsum.photos/id/1015/600/400"),
		Placeholder: "placeholder.png",
		Mode:        ui.ImageFill,
		Label:       "A river in a valley",
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

`ui.Symbol` shows a symbol. It's an SF Symbol on iOS, and the Material
Symbol for it on Android. It's sized as text in its style's `Font`, so
it follows the text size, and colored with its `Color` (the label color
by default).

```go
return ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
	ui.Symbol(ui.IconBell),
	ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: ui.Yellow, Font: ui.Title}),
	ui.Symbol(ui.Icon("archivebox"), ui.Style{Color: ui.SecondaryLabel}),
)
```

A `ui.Icon` is an SF Symbol name. Buttons, menu items, bar buttons and
tabs take one too. The `Icon` constants are the common ones:
`IconHome`, `IconSearch`, `IconSettings`, `IconPlus`, `IconClose`,
`IconTrash`, `IconShare`, `IconMore` and about fifty more.
`icon.Filled()` is its filled form (`"star.fill"`).

iOS draws any SF Symbol. Android draws a curated set: the constants and
more, mapped to Material Symbols in
[`hosts/android/gen_symbols.go`](../hosts/android/gen_symbols.go). A
name outside that set shows nothing on Android (it logs a warning), so
check a name you write as a string, like `"archivebox"`, on both.

### SVG icons

For icons of your own, package `ryui/svg` draws SVG with the platform's
vector drawing; nothing is rasterized. `svg.MustParse` reads the markup
(`svg.Parse` returns an error instead), and `svg.View` draws it.

```go
var download = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>`)

func downloadIcon() ui.Node {
	return svg.View(svg.Props{Icon: download, Color: ui.Val(ui.Accent), Label: "Download",
		Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
}
```

`Color` paints what the SVG paints with `currentColor`, and fills it
doesn't give; it's `ui.Label`, the text color, by default. Without a
width or height in its `Style`, the view is the SVG's own size; with
one, the other keeps the aspect ratio. Keep an icon in a file with
`//go:embed` and parse it once, in a package variable.

It reads what icon sets use (Lucide, Material, Heroicons, Feather):
paths, basic shapes, groups, `use`, fills, strokes and transforms. It
skips text, gradients (painted with the icon's color), clipping, masks,
filters and `fill-rule="evenodd"`.

## Video

`ui.Video` plays a video with the platform's player
(`AVPlayerViewController`, Media3's ExoPlayer) and its controls.
`ui.Play`, `ui.Pause` and `ui.Seek` drive it.

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

`ui.WebView` shows a web page in the platform's web view (`WKWebView`,
Android's `WebView`), with its scrolling, zooming and selection. With
no height and no `Grow` it grows to fill the space it's given.

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
		ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: "Back",
			Disabled: signal.Func(func() bool { return !state.Get().CanGoBack }),
			OnPress:  func() { ui.WebBack(web) }}),
		ui.Text(signal.Func(func() string { return state.Get().Title }), ui.Style{Grow: 1, Lines: 1}),
		ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: "Reload", OnPress: func() { ui.WebReload(web) }}),
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
  images, media, `fetch`). **Android only**: iOS doesn't let an app see
  them.

`ui.WebBack`, `ui.WebForward`, `ui.WebReload` and `ui.WebStop` move
through its history, load the page again, or stop it loading.

### Talking to the page

The page posts to Go with `window.ryui.postMessage(text)`, which calls
`OnMessage`. Go runs script in the page with `ui.EvalJS`, which returns
nothing, or `ui.EvalJSResult`, which calls back with the value as JSON:
the last statement's, or what a returned promise resolves to (`null` for
`undefined`), or the error it threw.

```go
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	HTML: ui.Val(`<button onclick="ryui.postMessage('tapped')">Tap</button>`),
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
	Fill:  ui.Orange,
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
	return ui.View(ui.ViewProps{Label: "Progress", Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: ui.SecondaryBackground, LineWidth: 8, Style: box}),
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: ui.Accent, LineWidth: 8, LineCap: ui.CapRound,
			TrimEnd: done, Style: box}),
	)
}
```

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
	Label:       "Ada, 2 unread",
	Style:       ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.All(16), Background: ui.SecondaryBackground, CornerRadius: 12},
},
	ui.Symbol(ui.IconPerson.Filled()),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada", ui.Style{Font: ui.Headline}),
		ui.StaticText("2 unread", ui.Style{Color: ui.SecondaryLabel}),
	),
)
```

`ViewProps` has:

- `OnPress` and `OnLongPress`.
- Gestures, with the platform's recognizers: `OnPan`, `OnPinch`,
  `OnRotate` (each given a `ui.Gesture`) and `OnSwipe` (a
  `ui.SwipeDirection`). See [Motion](motion.md).
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
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: "Back to top", Variant: ui.ButtonFilled,
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

**Pull to refresh.** `OnRefresh` turns it on (`UIRefreshControl`,
`SwipeRefreshLayout`). Set `Refreshing` true while you refresh; the
spinner hides when it turns false. Without `Refreshing`, it hides as
soon as `OnRefresh` returns.

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

## Lists

`ui.List` shows a row per item in the platform's recycling list
(`UICollectionView`, `RecyclerView`). Rows are made only for the cells
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
		ui.Text(signal.Func(func() string { return m.Get().From }), ui.Style{Font: ui.Headline}),
		ui.Text(signal.Func(func() string { return m.Get().Subject }), ui.Style{Color: ui.SecondaryLabel}),
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
  color and icon, as Material's swipe to dismiss. Give the row the same
  as accessibility `Actions`, for screen readers.
- `OnEndReached`, called when the last rows are about to show, once for
  each length the list has: load more there.
- `OnRefresh` and `Refreshing`, pull to refresh as on a `Scroll`.
- `OnScroll`, as on a `Scroll`.

```go
messages := signal.NewFunc(fetch(0, 30), nil)
remove := func(i int) {
	messages.Update(func(ms []message) []message { return slices.Delete(slices.Clone(ms), i, i+1) })
}
list := ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
			{Title: "Archive", Icon: "archivebox", OnPress: func() { remove(i) }},
		}
	},
	OnEndReached: func() {
		ms := messages.Peek()
		messages.Set(append(slices.Clone(ms), fetch(len(ms), 30)...))
	},
}, messages, func(m message) int { return m.ID }, row)
```

`ui.ScrollToItem(list, index, animated)` scrolls a list so an item
shows; `ScrollTo` and `ScrollToEnd` work too.

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
	ui.StaticText("Tags", ui.Style{Font: ui.Headline}),
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

When a platform has a widget RyUI doesn't, write it in Swift and Kotlin
and show it with `ui.Native`. See [Native code](native.md).

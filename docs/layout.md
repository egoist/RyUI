# Layout and style

A `ui.Style` says how a view is laid out and how it looks. Layout is CSS
flexbox, in points; the look is colors, corners, borders, shadows and
text. This page covers both, and styles that change. The layout is the
same on every platform: RyUI computes it in Go and places the platform's
views where it says.

## Platform defaults

A style you don't set keeps the platform's default. An app that sets no
style looks as the platform's own apps do, with their fonts, colors,
dark mode and text sizes:

- **iOS**: UIKit's.
- **Android**: Material 3's.
- **macOS**: AppKit's.
- **Windows**: Windows 11's (WinUI's Fluent look, which the host draws
  on Win32's controls).
- **Linux**: GNOME's (GTK 4 and libadwaita).
- **The web**: the browser's controls, with a light and dark palette of
  RyUI's own around them (see [the web](web.md)).

Set only what you mean to change, and prefer the semantic colors and
fonts below to fixed ones, so the app keeps following the platform.

## Style

`ui.Style` is a plain struct. Components take it as their style, a
`Styler`: a `Style`, or a `ui.StyleFunc` for one that changes (see
[Styles that change](#styles-that-change)).

```go
ui.Text(title, ui.Style{Font: ui.Headline, Color: ui.SecondaryLabel})
ui.Button(ui.ButtonProps{Title: ui.Val("Follow"), Style: ui.Style{Shrink: ui.NoShrink}})
ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1), Background: ui.Separator}})
```

Visual props apply where the native widget supports them: a button
takes `Color` as its tint, a view ignores `Font`.

## Stacking views

`ui.Column` stacks its children top to bottom, `ui.Row` leading to
trailing. `ui.View` is a container whose direction is its style's
`Direction`: `ui.ColumnDir`, the default, or `ui.RowDir`. A nil style is
an empty one.

```go
ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12},
	Avatar(),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada Lovelace", ui.Style{Font: ui.Headline}),
		ui.StaticText("Notes on the Analytical Engine", ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel, Lines: 1}),
	),
	ui.Button(ui.ButtonProps{Title: ui.Val("Follow"), Style: ui.Style{Shrink: ui.NoShrink}}),
)
```

A container with only layout props makes no native view: its children
are placed in the nearest one that has one. It costs nothing to nest
them.

## Flexbox

Layout follows CSS flexbox with React Native's defaults: column
direction, and sizes that include padding and borders.

- **`Gap`**: the space between children, and between lines when they
  wrap.
- **`Justify`** places the children along the main axis:
  `ui.JustifyStart` (the default), `ui.JustifyCenter`, `ui.JustifyEnd`,
  `ui.JustifySpaceBetween`, `ui.JustifySpaceAround`,
  `ui.JustifySpaceEvenly`.
- **`Align`** places them across it: `ui.AlignStretch` (the default:
  they fill the cross axis), `ui.AlignStart`, `ui.AlignCenter`,
  `ui.AlignEnd`, `ui.AlignBaseline`.
- **`AlignSelf`** overrides the parent's `Align` for one child;
  `ui.AlignAuto`, the zero value, keeps it.
- **`Wrap`** lets children flow onto more lines when they don't fit.
- **`Grow`** is a child's share of the space left along its parent's
  main axis. A growing child can also shrink below its content's size,
  so a growing view around a `Scroll` scrolls rather than taking the
  scroll's full height.
- **`Shrink`** is how much a child gives up when the line overflows. 0
  is the default, 1: text wraps, controls keep their minimum size.
  `ui.NoShrink` keeps the child's size.
- **`Basis`** is a child's starting size along the main axis; Auto, the
  default, is its content's.

```go
// Two boxes sharing the width, one twice the other.
ui.Row(ui.Style{Gap: 8, Height: ui.Pt(60)},
	ui.View(ui.ViewProps{Style: ui.Style{Grow: 2, Background: ui.SecondaryBackground}}),
	ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Background: ui.SecondaryBackground}}),
)

// A label and a value at either end.
ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter},
	ui.StaticText("Total"),
	ui.StaticText("$42", ui.Style{Font: ui.Title2}),
)

// Chips that wrap onto more lines.
ui.Row(ui.Style{Wrap: true, Gap: 8},
	ui.StaticText("Go"), ui.StaticText("iOS"), ui.StaticText("Android"),
)
```

## Sizes

Lengths are a `ui.Dim`:

- `ui.Pt(v)`: points: iOS's and macOS's points, Android's dp, a pixel
  at 100% scaling on Windows (1/96 inch), GTK's logical pixels on
  Linux, and CSS pixels on the web.
- `ui.Pct(v)`: a percentage of the parent's size along the same axis.
- `ui.Auto`, the zero value: the content's size, or what flexbox gives.

`Width` and `Height` set the size; `MinWidth`, `MinHeight`, `MaxWidth`
and `MaxHeight` bound it. `AspectRatio` is width over height: with one
side known, it sets the other.

```go
ui.View(ui.ViewProps{Style: ui.Style{
	Width:       ui.Pct(50),
	MinWidth:    ui.Pt(120),
	MaxWidth:    ui.Pt(320),
	AspectRatio: 16.0 / 9,
	Background:  ui.Gray,
}})
```

Text and controls measure themselves, with the platform's own
measuring: give them a size only to change it.

A point is as many pixels as the display's scale: 2 or 3 on a phone, 2
on a Retina Mac, the display's scaling on Windows and Linux (1.5 at
150%), the browser's device pixel ratio on the web. `ui.UseEnvironment()`'s
`Scale` says which. A desktop window moved to a display of another
scale is drawn at that display's scale; its layout, in points, stays as
it was.

## Padding and margin

`Padding` and `Margin` are `ui.Edges`, per side, in points:

- `ui.All(v)`: v on every side.
- `ui.Symmetric(v, h)`: v at the top and bottom, h at the left and
  right.
- `ui.Edges{Top: 24}`: each side by name.

```go
ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Margin: ui.Edges{Top: 24}},
	ui.StaticText("Padded"),
)
```

In a language written right to left, a `Row` runs from the right, `Justify`
and `Align` follow it, and the side a line of text starts on is the right.
`Left` and `Right` stay where they say, as in CSS. To put a space on the side
text starts or ends on, say `Start` or `End`:

```go
ui.Style{Padding: ui.Edges{Start: 16, End: 8}} // 16 on the left in English, on the right in Arabic
```

See [Languages](i18n.md).

## Absolute positioning

`Absolute` takes a view out of the flow; `Top`, `Right`, `Bottom` and
`Left` place it in its parent. Here a badge sits on the corner of an
avatar:

```go
ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(44), Height: ui.Pt(44)}},
	Avatar(),
	ui.View(ui.ViewProps{Style: ui.Style{
		Absolute: true, Top: ui.Pt(-4), Right: ui.Pt(-4),
		Width: ui.Pt(18), Height: ui.Pt(18), CornerRadius: 9,
		Background: ui.Red, Align: ui.AlignCenter, Justify: ui.JustifyCenter,
	}}, Badge()),
)
```

Later children are drawn over earlier ones.

## Grid

With `Grid` set, a container lays out its children in a CSS grid. Its
tracks come from package `layout`: `layout.Fr`, `layout.Px`,
`layout.Repeat`, `layout.Minmax`... A child spans tracks with
`GridColumn` and `GridRow`.

```go
ui.View(ui.ViewProps{Style: ui.Style{Gap: 8, Grid: &ui.Grid{
	Columns: []layout.GridTrack{layout.Repeat(3, layout.Fr(1))},
}}},
	Avatar(), Avatar(), Avatar(), Avatar(),
	ui.View(ui.ViewProps{Style: ui.Style{GridColumn: layout.Placement{Start: layout.Span(3)}, Height: ui.Pt(20), Background: ui.Gray}}),
)
```

For a grid of many items that scrolls, use a `ui.List` with `Columns`
(see [Components](components.md)).

## The safe area, the keyboard and the window

On a phone, the root fills the screen, under the status bar, the notch
and the home indicator. `ui.SafeArea` is a `Column` padded by the safe
area's insets, on top of its own padding. Its bottom padding is the
keyboard's height while the keyboard is up, so what's at its bottom (a
text field, a send button) stays above it.

```go
ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12},
	ui.StaticText("Todos", ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
	ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, rows...),
	composer,
)
```

The insets are those of the screen it's in: in a stack screen, a tab or
a sheet, that screen's. `ui.UseEnvironment` gives them, and the
keyboard's height, for placing things yourself (see
[Concepts](concepts.md)).

On macOS, Windows and Linux the root fills the window's content, below
its title bar (Linux's header bar). A desktop window has no safe area
and no on-screen keyboard over it: the insets and the keyboard's height
are 0, and `ui.SafeArea` is a `Column` with its own padding. Use it
anyway; the same screen then fits a phone too.

The window is 800 by 600 points unless the app says otherwise. Give
its size, and the least the user can make it, with `app.RunWindow`:

```go
func main() {
	app.RunWindow(ui.WindowOptions{Width: 960, Height: 640, MinWidth: 480, MinHeight: 360}, App)
}
```

A phone ignores these options. As the user resizes the window, the
environment's `Width` and `Height` change and layout runs again: there's
nothing to handle. To change the layout at a size (a sidebar that shows
when there's room), read the environment's `WidthClass` in a
`StyleFunc` or a `Show`, as on a tablet.

In a browser, an app that fills the page takes the device's safe area
(`env(safe-area-inset-*)`, on a phone with a notch) and the on-screen
keyboard's height from the visual viewport. An app embedded in a part of
a page has neither.

## Scrolling

`ui.Scroll` scrolls its children, vertically or, with `Horizontal`,
sideways. Give it a size, or `Grow: 1`, so it has something to scroll
within. `Style` is the scroll view's own box; `ContentStyle` is the box
its children stack in, where padding and gaps go.

```go
ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.All(16), Gap: 12}},
	ui.StaticText("Long content"),
)
```

Pull to refresh and scroll positions are in
[Components](components.md), and long lists in
[Lists](components.md).

## Colors

A `ui.Color` is one of:

- **The platform's default**: the zero value, a prop left unset.
- **A semantic color**, resolved by the platform, so it follows dark
  mode:
  - text: `ui.Label`, `ui.SecondaryLabel`, `ui.TertiaryLabel`;
  - backgrounds: `ui.SystemBackground`, `ui.SecondaryBackground`,
    `ui.GroupedBackground`, and `ui.SecondaryGroupedBackground` for
    a grouped list's rows on it (iOS's inset grouped cells);
  - `ui.Separator`, `ui.Accent` (the app's accent), `ui.OnAccent` (what
    reads on it: an icon on a button filled with the accent), `ui.Clear`
    (transparent);
  - the system's hues: `ui.Red`, `ui.Orange`, `ui.Yellow`, `ui.Green`,
    `ui.Blue`, `ui.Purple`, `ui.Pink`, `ui.Gray`.
- **A fixed color**: `ui.RGB(r, g, b)`, `ui.RGBA(r, g, b, a)`,
  `ui.Hex(0x7c3aed)`, or `ui.ParseHex("#7c3aed")` (also `"#rrggbbaa"`),
  which reports whether it parsed.

Where the semantic colors come from:

| Platform | Semantic colors |
| --- | --- |
| iOS | UIKit's (`labelColor`, `systemBackground`, `systemRed`...) |
| Android | Material 3's color roles |
| macOS | AppKit's (`labelColor`, `windowBackgroundColor`, `controlBackgroundColor`, `separatorColor`, `systemRedColor`...) |
| Windows | Windows 11's theme colors (WinUI's Fluent theme resources), light or dark |
| Linux | libadwaita's named colors (`window_fg_color`, `window_bg_color`, `card_bg_color`, `destructive_color`...), as the theme defines them, high contrast included |
| The web | RyUI's palette, as CSS variables, light and dark |

On the desktops the grouped backgrounds are the plain ones:
`ui.GroupedBackground` is the window's background, and
`ui.SecondaryGroupedBackground` a card's, as `ui.SecondaryBackground` is.

```go
ui.Column(ui.Style{Padding: ui.All(16), Gap: 4, Background: ui.SystemBackground, CornerRadius: 12},
	ui.StaticText("Title", ui.Style{Font: ui.Headline}),
	ui.StaticText("Details", ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}),
)
```

`c.Opacity(a)` is a color drawn at an opacity from 0 to 1 (SwiftUI's
`.opacity`, Compose's `copy(alpha =)`). A semantic color stays one, so
`ui.Accent.Opacity(0.15)` is a tint of the accent in light and dark
alike: a chip's background, a chart's fill, a track.

`Color` is the text's color, or a control's tint.

## Backgrounds, borders and shadows

- **`Background`**: the view's background color.
- **`CornerRadius`**: rounds its corners, in points.
- **`BorderWidth`** and **`BorderColor`**: a border, drawn inside the
  box. It insets the content, as padding does.
- **`Shadow`**: a drop shadow, cast by the view's shape (its bounds,
  rounded by `CornerRadius`), not by what's drawn in it: `Color` (none
  without one), `Radius` (a blur radius, as CSS's and SwiftUI's: twice
  the blur's σ), and an offset, `X` and `Y`. A view that clips its
  children (`Clip`) still casts it, as a rounded card that clips its
  image does. Android draws its elevation shadow instead: the radius is
  the elevation, the color tints it, and the light sets the offset.
  macOS, Windows, Linux and the web draw it as iOS does, with the same
  blur and offset.
- **`Gradient`**: a linear gradient over the background.
  `ui.LinearGradient(colors...)` goes from top to bottom; a
  `ui.Gradient` sets `Start` and `End` as `ui.Point`s, in units of the
  view's size ((0, 0) its top leading corner, (1, 1) its bottom trailing
  one), and `Locations`, one per color. Android draws it along the
  nearest of eight directions; the other platforms along the line you
  give.
- **`Material`**: a translucent background that blurs what's behind it,
  iOS's materials: `ui.MaterialUltraThin`, `ui.MaterialThin`,
  `ui.MaterialRegular`, `ui.MaterialThick`, `ui.MaterialBar`.
  - iOS: `UIVisualEffectView`'s blurs.
  - macOS: an `NSVisualEffectView` behind the view's content, blurring
    what's behind it in the window.
  - Windows: Fluent's acrylic, which the host draws: what's behind the
    view, blurred and tinted light or dark.
  - The web: CSS's `backdrop-filter`, a blur and saturation.
  - Android and Linux, which can't blur behind a view, show the surface
    color (Linux: libadwaita's card color), translucent.
- **`Clip`**: clips the children to the view's bounds and corners.

```go
ui.View(ui.ViewProps{Style: ui.Style{
	Height:       ui.Pt(120),
	CornerRadius: 16,
	Gradient:     ui.LinearGradient(ui.Hex(0x6366f1), ui.Hex(0xec4899)),
	Shadow:       ui.Shadow{Color: ui.RGBA(0, 0, 0, 80), Radius: 16, Y: 8},
}})

ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(12), BorderWidth: 1, BorderColor: ui.Separator, CornerRadius: 8, Clip: true}},
	ui.StaticText("Bordered"),
)

// Left to right.
ui.View(ui.ViewProps{Style: ui.Style{
	Height:   ui.Pt(80),
	Gradient: &ui.Gradient{Colors: []ui.Color{ui.Blue, ui.Purple}, Start: ui.Point{X: 0, Y: 0.5}, End: ui.Point{X: 1, Y: 0.5}},
}})
```

## Opacity and hiding

`Opacity` is from 0 to 1, but its zero value means opaque, as an unset
prop does: `Opacity: 0.5` is half transparent, `Opacity: 0` is fully
opaque. To hide a view, set `Hidden`. A hidden view still takes its
space in the layout; to take it out, and its views with it, use
`ui.Show` (see [Concepts](concepts.md)).

## Transforms

`Transform` moves, scales and rotates a view as it's drawn
(`TranslateX`, `TranslateY`, `Scale`, `ScaleX`, `ScaleY`, `Rotate` in
degrees); layout doesn't see it. Every platform draws it. Transforms, `Transition` and `Match` are
for animation: see [Motion](motion.md).

## Text

`Font` sets a text's font:

- **Semantic fonts**, the platform's text styles, scaled with the
  user's text size: `ui.LargeTitle`, `ui.Title`, `ui.Title2`,
  `ui.Title3`, `ui.Headline`, `ui.Subheadline`, `ui.Body`, `ui.Callout`,
  `ui.Footnote`, `ui.Caption`, `ui.Caption2`.
- **`ui.System(size)`**: the system font at a size, in points.
- **`.Weighted(w)`** gives a font a weight: `ui.Ultralight`, `ui.Thin`,
  `ui.Light`, `ui.Regular`, `ui.Medium`, `ui.Semibold`, `ui.Bold`,
  `ui.Heavy`, `ui.Black`.

`Color` is the text's color, `TextAlign` its alignment (`ui.TextStart`,
`ui.TextCenter`, `ui.TextEnd`, `ui.TextJustify`), and `Lines` the most
lines it shows; 0, the default, is no limit. Text wraps by default.

```go
ui.StaticText("Brand", ui.Style{Color: ui.RGB(124, 58, 237), Font: ui.Title.Weighted(ui.Bold)})
ui.StaticText("A long paragraph that stops after two lines.", ui.Style{Font: ui.Body, Lines: 2, TextAlign: ui.TextCenter})
ui.StaticText("Fixed size", ui.Style{Font: ui.System(20).Weighted(ui.Medium)})
```

For text with several styles in it, links and inline views, see
[RichText](components.md).

The text styles are iOS's names; each platform maps them to its own
type ramp, so `ui.Body` is the size the platform's apps use for body
text:

| Platform | Text styles | `ui.Body` |
| --- | --- | --- |
| iOS | UIKit's text styles, in San Francisco | 17 |
| Android | Material 3's type scale (`ui.Body` is Body Large), in Roboto | 16 |
| macOS | AppKit's text styles (`NSFont.preferredFont(forTextStyle:)`), in San Francisco | 13 |
| Windows | Windows 11's type ramp, in Segoe UI Variable (Segoe UI before Windows 11) | 14 |
| Linux | libadwaita's type classes (`.title-1`, `.heading`, `.caption`...), relative to the desktop's interface font | the interface font's size |
| The web | RyUI's ramp, in the system's font (`system-ui`) | 16 |

On Windows the styles take the nearest of the ramp's: `ui.LargeTitle` is
Title Large (40), `ui.Title` Title (28), `ui.Title2` Subtitle (20),
`ui.Headline` Body Strong (14, semibold), `ui.Footnote` and `ui.Caption`
Caption (12). On Linux, with GNOME's 11 pt interface font, `ui.LargeTitle`
is `.title-1` (181%, heavy), `ui.Title` `.title-2`, `ui.Headline`
`.heading` (bold), and `ui.Caption` `.caption` (82%).

What scales with the user's text size:

- **iOS** (Dynamic Type) and **Android** (font scale): every font,
  semantic, fixed and custom.
- **Windows**: every font, by Settings, Accessibility, Text size.
- **Linux**: every font, by the desktop's text scaling (GNOME's Large
  Text, GTK's font DPI). The interface font the styles are relative to
  is the desktop's too (GTK's `gtk-font-name`).
- **The web**: every font, by the browser's text size: fonts are set in
  `rem`.
- **macOS** has no text size setting: fonts stay as they are.

`ui.UseEnvironment()`'s `FontScale` is that size, 1 by default (see
[Accessibility](accessibility.md)).

## Custom fonts

Put font files (`.ttf`, `.otf`) in the app's `assets/fonts`. A font's
`Family`, or `ui.Custom(family, font)`, picks one:

- a file's name: `assets/fonts/Inter-Bold.ttf` is `"Inter-Bold"`;
- a family of files: `"Inter"` is `Inter-Regular`, `Inter-Bold`... as
  the font's weight says;
- a generic family: `"serif"`, `"monospace"`, `"rounded"`. On iOS and
  macOS they're the system font's designs (New York, SF Mono, SF
  Rounded); on Windows Georgia and Cascadia Mono; on Linux and Android
  the system's serif and monospace families; on the web `ui-serif`,
  `ui-monospace` and `ui-rounded`, where the browser has them. Android,
  Windows and Linux have no rounded one, and show the system font;
- a family the system has, such as `"Georgia"`.

A custom font scales as its text style does. One the platform doesn't
have shows in the system font. Every platform loads the files in
`assets/fonts` for the app alone: they aren't installed for other apps.

```go
ui.StaticText("Inter Bold", ui.Style{Font: ui.Custom("Inter-Bold", ui.Title)})
ui.StaticText("Inter", ui.Style{Font: ui.Custom("Inter", ui.Body).Weighted(ui.Semibold)})
ui.StaticText("Code", ui.Style{Font: ui.Custom("monospace", ui.Footnote)})
```

## Dark mode

Semantic colors change with dark mode on their own: an app that uses
them needs no code for it. A fixed color stays as it is; to pick one per
mode, read `Dark` from the environment in a `StyleFunc`:

```go
env := ui.UseEnvironment()
ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
	s := ui.Style{Height: ui.Pt(80), CornerRadius: 12, Background: ui.RGB(255, 255, 255)}
	if env.Get().Dark {
		s.Background = ui.RGB(28, 28, 30)
	}
	return s
})})
```

## Styles that change

A `ui.StyleFunc` returns a style, and runs again when a signal it read
changes. The new style is compared with the last: layout runs again only
if a layout prop changed, and only the visual props that differ are
sent to the view.

```go
ui.View(ui.ViewProps{
	OnPress: func() { selected.Set(!selected.Peek()) },
	Style: ui.StyleFunc(func() ui.Style {
		s := ui.Style{Padding: ui.All(12), CornerRadius: 10, Background: ui.SecondaryBackground}
		if selected.Get() {
			s.Background = ui.Accent
			s.BorderWidth = 2
			s.BorderColor = ui.Label
		}
		return s
	}),
}, ui.StaticText("Tap me"))
```

Make the change inside `ui.Animate` and it animates (see
[Motion](motion.md)).

## The accent color

`ui.Accent`, and the controls that use it, take the app's accent color
from `ryui.json`:

```json
{ "accent": "#7c3aed" }
```

It's the tint on iOS, and the seed of the Material 3 color scheme on
Android (Android 12 and later). With `"android": {"dynamicColor": true}`,
Android takes the colors from the user's wallpaper where the system has
them, as Material apps do. On macOS, Windows and Linux it replaces the
system's accent in RyUI's controls and colors; on the web it's the
controls' `accent-color` too.

Without an accent, the platform's is used. On macOS and Windows that's
the accent the user picked (macOS's System Settings, Appearance;
Windows' Settings, Personalization, Colors), on Linux libadwaita's
blue, and on the web a blue of RyUI's. See [Shipping](shipping.md).

## Light and dark

The app follows the system's appearance: iOS's and macOS's Dark Mode,
Android's dark theme, Windows' app mode (Settings, Personalization,
Colors), GNOME's dark style, the browser's `prefers-color-scheme`.
`ui.SetColorScheme` shows it light or dark whatever the system's is, as
an app's own appearance setting does (SwiftUI's
`preferredColorScheme`; on Android, AppCompat's night mode; on macOS,
the app's `NSAppearance`; on Linux, libadwaita's style manager), and
`ui.ColorSchemeSystem` follows the system again. The
semantic colors, the platform's controls and the system bars follow,
without remaking the app, and `ui.UseEnvironment()`'s `Dark` says which
it is. Keep the choice with [`store`](data.md) and set it as the app
starts:

```go
var scheme = store.Signal("colorScheme", int(ui.ColorSchemeSystem))

func App() ui.Node {
	ui.SetColorScheme(ui.ColorScheme(scheme.Peek()))
	// ...
}
```

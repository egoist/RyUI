# nui — design

nui (working name) is a UI framework in Go that renders with each
platform's own widgets. **Mobile comes first: iOS and Android.** Desktop
(macOS, Windows, Linux) comes after mobile's v0 is done, on the same core
(see "Desktop, later"). Apps are written in Go. The goals, in order:

1. **Fast to build with**: fast compiles, a quick edit-and-run loop on
   the simulator and the emulator, a small API.
2. **Best performance**: an update touches only the native widgets it
   changes, and crosses into native code once per batch.

Non-goals: drawing widgets ourselves (that is MyGo's native UI; the one
planned exception is rich text, see "Rich text"), web views, pixel-identical looks across platforms, stability across a
large audience before 1.0. Desktop is not a non-goal, only later: no
core API may assume a phone (one window, touch only) where desktop would
need something else; such APIs get a desktop-shaped extension point now
and an implementation later.

## Decisions

| Question | Decision | Why |
|---|---|---|
| Platforms, in order | iOS, Android; then macOS, Windows, Linux | Mobile is the focus. The core and `ops` are platform-neutral, so desktop hosts are added later without changing apps. |
| Language | Go | Fast compiles; GC and closures make UI code quick to write; MyGo's purego/Objective-C experience carries over. The cost per frame is in the platform toolkits, so a faster core language would not show. |
| Retained or immediate | Retained | Native widgets are retained objects that own focus, selection, IME, scroll and accessibility state. Rebuilding a description every frame would mean diffing it against them. |
| Reactivity | Fine-grained signals (Solid-like), no virtual tree | A signal write re-runs only the effects that read it, and each effect emits exactly the property change it causes. |
| Threads | Core runs on the platform's main thread | Native callbacks that need answers synchronously (list data sources, text field delegates, measurement, layout inside the keyboard's animation) call straight into the core. Frameworks that run logic and layout off the main thread (React Native, Texture, Litho, NSH) do it so slow or garbage-collected app logic can't drop frames, and pay with asynchronous interop: blank cells, input races, layout a frame late. Fabric added synchronous layout and Flutter merged its UI and platform threads (3.29) for that reason; SwiftUI and Compose stay on main. nui's logic is compiled Go with fine-grained updates, so the main thread suffices; slow work runs on goroutines that post results back. The place off-main work does pay, preparing long, rich or streaming text, is handled without moving the core (see "Rich text"). |
| Core ↔ native contract | Batched mount operations (`ops`) | One crossing per batch. The same stream works in process and over a socket. |
| Layout | Our own engine in Go: CSS block, flexbox and grid (Taffy-like, checked against Chrome), measured by the host | One layout engine for every platform, with the semantics web and React Native developers know; native widgets report their own sizes. Designed for our tree and for zero allocation, not ported from Yoga or Taffy. |
| iOS host | Swift + UIKit, with Go linked as a `c-archive` | UIKit subclasses and delegates are trivial in Swift. Not SwiftUI: it is a second reconciler. |
| Android host | Kotlin + Android Views (Material Components 3), with Go loaded as a `c-shared` library | Views are imperative targets for mount ops; JNI is crossed once per batch. Not Compose: it is a second reconciler. |
| Text and controls | The platform's own views (`UILabel`, `TextView`, ...), measured by the real widget on the main thread | Native behavior for free: accessibility, Dynamic Type and font scale, selection, truncation, line breaking. What's measured is what's drawn. The alternative, laying text out off the main thread with CoreText / `StaticLayout` and drawing it in a custom view (as NSH does, because its core runs off the main thread), would re-implement all of that. The cost: text is measured on the main thread, and each widget's measuring quirks are ours. Mitigations: per-node layout caches, and a shared measure cache keyed by content (below). If profiling ever shows main-thread text measuring hurting (List cells), measure with CoreText / `StaticLayout` while still drawing with the platform view. |
| cgo | Allowed only in `mobile/` (build tags `ios`, `android`) | Go's `c-archive` and `c-shared` build modes need it. Everything else builds with `CGO_ENABLED=0`, so desktop hosts can stay cgo-free later. |

Open decisions, with the default to use until they are settled:

- Minimum versions: **iOS 16, Android API 26 (8.0)**.
- Android widgets: Material Components 3 (decided in M2).
- Module path: `github.com/egoist/nui`.

## Architecture

```
app code (Go)            components, signals
─────────────────────────────────────────────────────────────
signal/                  Signal, Memo, Effect, owners, batching
ui/                      node tree, components, flush scheduler
layout/                  flexbox over the node tree
ops/                     mount operations, binary encoding
─────────────────────────────────────────────────────────────  Host interface
mobile/ (C ABI, cgo) ──► hosts/ios (Swift)   hosts/android (Kotlin)
host/fake                                  tests: records ops
host/darwin                                macOS: AppKit in process, Go (purego)
host/windows                               Windows: Win32 in process, Go (syscall)
host/linux                                 later: desktop, in process, Go
```

The core never imports a host. A mobile app is a Go library: `mobile/`
adapts the C ABI to `host.Host`, and the Swift/Kotlin host app calls its
exported functions.

### The Host interface

```go
package host

type Host interface {
	// Apply applies a batch of mount operations. Main thread.
	Apply(b *ops.Batch)
	// Measure returns the size of a native view's content: c gives the
	// known width/height (NaN when free) and the space available on each
	// axis (definite, MaxContentSpace or MinContentSpace). Main thread,
	// synchronous, only for views that exist (applied in an earlier Apply).
	Measure(id ops.NodeID, c layout.Constraints) layout.Size
	// Post schedules f on the main thread, safe from any goroutine.
	Post(f func())
	// Environment returns the root view's size, safe area insets, the
	// keyboard's frame, scale, appearance (dark/light), locale and text
	// size (Dynamic Type, Android font scale).
	Environment() Environment
}
```

Events come from the host into `ui` through one entry point
(`ui.Dispatch(ev ops.Event)`), on the main thread. Hosts that call into
the core implement `host.Connector`; `ui.Mount` hands them a `host.Core`
(`Dispatch`, `ListCount`, `ListBind`, `ListUnbind`).

## signal

Port the algorithm of [alien-signals](https://github.com/stackblitz/alien-signals)
(push dirty flags, pull values; glitch-free, no unnecessary
recomputation), with Solid's ownership model.

```go
package signal

type Signal[T any] struct{ /* … */ }
func New[T comparable](v T) *Signal[T]                 // skips equal writes
func NewFunc[T any](v T, equal func(a, b T) bool) *Signal[T]
func (s *Signal[T]) Get() T                             // tracks
func (s *Signal[T]) Peek() T                            // doesn't track
func (s *Signal[T]) Set(v T)
func (s *Signal[T]) Update(f func(T) T)

type Memo[T any] struct{ /* … */ }
func NewMemo[T comparable](f func() T) *Memo[T]         // lazy, cached
func (m *Memo[T]) Get() T

// Accessor is what components take: a signal, a memo or a func.
type Accessor[T any] interface{ Get() T }
func Func[T any](f func() T) Accessor[T]
func Static[T any](v T) Accessor[T]

func Effect(f func())               // runs at flush, after memos
func RenderEffect(f func())         // runs now; ui's prop effects
func NewOwner() *Owner              // disposed with the current owner
func RunWithOwner(o *Owner, f func())
func BindThread()                   // nui_debug: the main thread
func Untrack[T any](f func() T) T
func Batch(f func())                // one flush for all writes in f

// Owners
func Root(f func(dispose func()))
func OnCleanup(f func())
type Context[T any] struct{ /* … */ }
func NewContext[T any](def T) *Context[T]
func (c *Context[T]) Provide(v T, f func())
func (c *Context[T]) Use() T
```

Rules:

- Every effect and memo belongs to the owner that was current when it was
  made. Disposing an owner disposes its children, runs their cleanups,
  and unsubscribes them. The GC frees memory but never stops a
  subscribed effect: disposal is explicit and must be tested.
- Writes during an event handler are batched until the handler returns.
  `Set` is legal only on the main thread; goroutines use `ui.Post`.
  Builds with the `nui_debug` tag check this and panic.
- Effects run in creation order within a flush: render effects (those
  `ui` makes for props) before user effects (`signal.Effect`).
- Writing a signal inside a memo is a panic. Writing inside an effect
  schedules another pass in the same flush, at most 100 passes, then a
  panic naming the effect (an infinite loop).
- No reflection, no `interface{}` boxing on the hot path, no allocation
  per write in the steady state.

Stores for nested state (a signal per struct field, as Solid's
`createStore`) come later, generated by `go generate` since generics
cannot reach fields.

## ui

### Components

A component is a Go function that runs **once** and returns a node.
Props that can change are `signal.Accessor[T]`; event handlers are plain
funcs.

```go
func Counter() ui.Node {
	count := signal.New(0)
	return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
		ui.Text(signal.Func(func() string {
			return fmt.Sprintf("Count: %d", count.Get())
		})),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Add"),
			OnPress: func() { count.Update(func(n int) int { return n + 1 }) },
		}),
		ui.Show(signal.Func(func() bool { return count.Get() > 10 }),
			func() ui.Node { return ui.StaticText("That's a lot") }),
	)
}

// On mobile the host calls main once at launch; app.Run mounts the root.
func main() { app.Run(Counter) }
```

The root fills the screen; `ui.SafeArea` (below) keeps content out of
the notch, the home indicator and the keyboard.

**Props.** A prop that can change after mount is an accessor; one that
can't (a sheet's detents, an action's title, a tab's icon) is a plain
value. A nil accessor is the zero value (`false`, `""`, 0): it sends
nothing and the host keeps its default, so a literal false is just left
out. A constant is `ui.Val(v)`, which the core spots: it's sent once
and makes no effect. A style is a `Style` or a `StyleFunc`, one effect
per element whose result is diffed (layout fields relayout only when
they change; visual fields send only what changed), not a signal per
field.

Go has no compile step to hide this, as Solid's JSX compiler does when
it turns `refreshing={r()}` into a getter, and we don't add one: `go
build`, `go test`, gopls and the debugger would all see code that means
something else. Planned instead: a vet-style analyzer
(`golang.org/x/tools/go/analysis`) that flags a signal read in a
component's body outside any effect, which runs once and never updates
(Solid's most common bug, too), and shortcuts for the common reactive
cases. A JSX-like syntax, if ever, would be a separate file type that
generates Go (as templ does), once the component API has settled.

### Control flow

- `Show(when Accessor[bool], then func() Node, else ...func() Node)`: makes
  and disposes the branch's owner as `when` changes.
- `For[T any, K comparable](items Accessor[[]T], key func(T) K, row func(item Accessor[T], index Accessor[int]) Node)`:
  keyed; a row's owner lives as long as its key. A moved row moves its
  native view; a changed item updates that row's `item` signal and
  nothing else.
- `Switch` / `Match`, `Dynamic(f func() Node)`.

### v0 components

| Component | iOS | Android |
|---|---|---|
| `View` / `Row` / `Column` | UIView | ViewGroup (no layout of its own) |
| `SafeArea` | insets from `safeAreaInsets` and the keyboard's frame | insets from `WindowInsetsCompat` (system bars, cutout, IME), edge to edge |
| `Text` | UILabel | TextView |
| `Button` | UIButton (`UIButton.Configuration`) | MaterialButton |
| `TextField` | UITextField | EditText (TextInputLayout optional) |
| `Toggle` | UISwitch | MaterialSwitch |
| `Slider` | UISlider | Slider (Material) |
| `Image` | UIImageView | ImageView |
| `Scroll` | UIScrollView | NestedScrollView / HorizontalScrollView |
| `List` | UICollectionView (list layout) | RecyclerView |
| `Refresh` (pull to refresh, on `Scroll`/`List`) | UIRefreshControl | SwipeRefreshLayout |
| `Stack` (navigation) | UINavigationController | own view stack with transitions, `OnBackPressedDispatcher` and predictive back |
| `Tabs` | UITabBarController | BottomNavigationView |
| `Alert` | UIAlertController (alert, action sheet) | MaterialAlertDialogBuilder |
| `Sheet` | sheet presentation (detents) | BottomSheetDialog |
| `Menu` (context, button menus) | UIMenu / UIContextMenuInteraction | PopupMenu |
| `Activity` (spinner) | UIActivityIndicatorView | CircularProgressIndicator |

A component a host can't show yet panics in that host with its name, so
gaps are found, never silently drawn wrong. `host/fake` supports all of
them.

Gestures on any `View`: `OnPress`, `OnLongPress`, and later pan, swipe
and pinch, mapped to UIGestureRecognizers and Android's
`GestureDetector`. Haptics: `ui.Haptic(kind)` (UIFeedbackGenerator,
`performHapticFeedback`).

### Navigation

`ui.Stack` holds a stack of screens; `ui.UseNavigator()` (a context) has
`Push(func() ui.Node, ui.ScreenOptions)` and `Pop()`. A screen is an
owner: popping it disposes it. `ScreenOptions` has the title, the bar's
buttons and whether the back gesture is allowed. The host animates
pushes, pops and the interactive back gesture natively; a gesture the
user finishes tells the core with `Event(stack, popped)`, which disposes
the screen, and one they cancel changes nothing.

How the kinds map (ops/schema.go has the wire contract):

- A **Screen** (a stack's screen, a tab's page) and a **Sheet** are
  *environment providers*: the host sizes them and reports their own
  `Environment` (size, safe area, keyboard over them); the core lays each
  out as a separate root, and `SafeArea` inside reads the nearest one.
  A screen under a navigation bar has no top safe area; a page above a
  tab bar has no bottom one.
- An **Insert** into a Stack pushes (animated once it's on screen); a
  **Destroy** of its top screen pops, animated. The host keeps the dying
  screen's views until the animation ends: the Destroys of its
  descendants that follow are no-ops. A pop the user makes is the same,
  started by the host: it animates, sends `Popped`, and the core
  destroys the screen.
- A **Sheet** and an **Alert** aren't inserted anywhere: `Present` shows
  them, Destroy dismisses them, the user's dismissal sends `Dismissed`,
  an alert's button sends `Action` (and isn't a dismissal).
- iOS: `UINavigationController` (its interactive pop gesture),
  `UITabBarController`, sheet presentation controllers with detents,
  `UIAlertController`, `UIMenu` (button primary action, context menu
  interaction), `UIRefreshControl`, feedback generators.
- Android: a `MaterialToolbar` over the screens, with Material motion
  (emphasized easing, slide and fade); back and **predictive back**
  through an `OnBackPressedCallback`, which follows the gesture (the top
  screen shrinks and follows the finger) and pops the innermost shown
  stack that can pop (the hidden tabs' stacks keep their screens); a
  `BottomNavigationView` (SF Symbol names map to Material icons); a
  `BottomSheetDialog` (medium is half expanded; the keyboard expands it,
  as iOS does); `MaterialAlertDialogBuilder` (an action sheet, or more
  than two buttons besides cancel, is a list); `PopupMenu`;
  `SwipeRefreshLayout` around the scroll view or list; haptic feedback
  constants.

### Style

`ui.Style` is a plain struct (static), or a `ui.StyleFunc` that runs in
an effect and sends only what changed.

Layout props map to CSS flexbox with React Native's defaults (column
direction, border-box): `Width`, `Height`, `Min*`, `Max*` (`ui.Pt`,
`ui.Pct`, Auto), `Grow`, `Shrink` (0 means CSS's default 1; `NoShrink`),
`Basis`, `Direction`, `Justify`, `Align`, `AlignSelf`, `Wrap`, `Gap`,
`Padding`, `Margin`, `Absolute` with `Top`/`Right`/`Bottom`/`Left`,
`AspectRatio`, and `Grid` (columns, rows, auto tracks, flow) with
`GridColumn`/`GridRow` placement. `BorderWidth` insets the content, as
the native border is drawn inside the view.

Visual props, applied where the native widget supports them and ignored
elsewhere (documented per host): `Background`, `CornerRadius`, `Border`,
`Shadow`, `Opacity`, `Hidden`, `Font` (size, weight, family), `Color`,
`TextAlign`, `LineLimit`. Defaults are the platform's own; an app that
sets nothing looks native.

Colors and fonts can be semantic (`ui.Label`, `ui.SecondaryLabel`,
`ui.Accent`, `ui.Body`, `ui.Title`), resolved by the host (UIKit's
semantic colors and text styles; Material 3's color roles and type
scale), so dark mode and Dynamic Type / font scale need no app code.

`Font.Family` (`ui.Custom("Inter-Bold", ui.Title)`) takes a font file
of the app's, by its name in `assets/fonts`; a family of them (`Inter`:
`Inter-Regular`, `Inter-Bold`..., picked by `Weight`); a generic family
(`serif`, `monospace`, `rounded`: on iOS the system font's designs); or
a system family. It scales as its text style does. Under `nui dev` the
core sends a font file's URL instead, since the player hasn't the app's
assets: the host loads it, then sends `FontLoaded`, and the core
measures text again (its measure cache cleared).

### Native escape hatch

`ui.Native(kind string, props ui.NativeProps) ui.Node` makes a view of a
kind the app registers with each host. Props are named bools, ints,
floats and strings (or accessors of them); events come back by name with
named values. Maps, video, camera previews, web views and platform-only
controls go through this.

An app's native code lives beside its Go: `ios/*.swift` is compiled
with the iOS host (same module), `android/*.kt` is built into the
Android app. Each registers its kinds from a well-known class the host
looks up at start: `@objc(NuiAppNatives)` conforming to
`NuiNativeModule` in Swift, `object dev.nui.app.NuiAppNatives :
NuiNativeModule` in Kotlin, calling `registerNative(kind) { ctx -> view }`.
A native view implements `set(name, value)` and sends events with
`ctx.emit(name, values)`; the host measures it as it measures any view.
On the wire it's one kind (`Native`) whose `Type` prop, sent right
after `Create`, picks the factory.

### Images, gestures, animation

- `ui.Image` takes an http(s) URL or a path in the app's `assets/`
  (copied into the iOS bundle; pushed beside the library on Android).
  The host fetches, decodes (Android subsamples to the screen) and
  prepares it off the main thread, caches it in memory, and reports
  `Loaded` with its natural size: one point per pixel, as on the web.
  The core sizes the image itself: natural size, no wider than the space
  it has, the other side from the aspect ratio; a style's `AspectRatio`
  overrides the image's, so a box keeps its size before and after
  loading.
- Gestures are `View` props (`OnPan`, `OnPinch`, `OnSwipe`) with the
  platform's recognizers on iOS (pan and pinch recognize together) and a
  touch listener on Android that claims the touch once it moves past the
  touch slop: it cancels the view's press and stops the parent
  intercepting. Android's `ScaleGestureDetector` isn't used: it ignores
  pinches narrower than about 27mm.
- `ui.Animate(f)` (or `AnimateWith` a duration and curve) brackets the
  next flush's two batches with `AnimateBegin`/`AnimateEnd`; the host
  changes frames, opacity, background and corner radius inside UIView
  animations, and with animators on Android (a frame animates by
  interpolating the layout bounds; views that weren't on screen don't
  animate in).

### Accessibility

Native widgets bring their own. `View` takes `Label`, `Hint`, `Role`
(button, header, image, …) and `Decorative` (hidden from screen readers
with what's in it), which hosts map to UIKit's accessibility properties
and Android's `AccessibilityNodeInfo` (through `ViewCompat`). Layout-only
views (below) never appear in the accessibility tree.

- A pressable `View` is one element, a button, read as its contents'
  labels joined (VoiceOver; TalkBack merges a clickable view's text
  itself).
- `ui.Announce` has the screen reader say what changed where focus
  isn't (a toast, a count of results).
- A List row's swipe actions are its accessibility actions on Android
  (UIKit's list offers its contextual actions to VoiceOver itself).
- `Environment.ReduceMotion` is iOS's Reduce Motion, or Android's
  animations turned off; on iOS, `Transition`s then dissolve instead of
  moving, as UIKit's do (Android's animator scale already makes every
  animation instant).
- Text follows Dynamic Type and Android's font scale, custom fonts too.

## Flush

Writes mark effects dirty; the scheduler flushes once:

- at the end of each event dispatched from the host (synchronously, so
  the host shows the result in the same run loop turn);
- after `ui.Post` callbacks;
- never on a timer.

A flush:

1. Runs dirty effects. Prop effects append `SetProp` ops; `Show`/`For`
   append create, insert, remove and destroy ops.
2. `Apply` the batch: native views now exist with their props.
3. Lays out dirty subtrees (`layout`), calling `Measure` for leaves whose
   props or constraints changed (cached per node and constraints).
4. Appends `SetFrame` for frames that changed and `Apply`s that batch.

That is two crossings per flush, whatever changed.

A host may call back into the core while it applies a batch: Android
lays a list out at once when its frame is set, which binds cells, and a
bind flushes. Ops written meanwhile go into a spare batch, so the nested
flush applies only its own; the batch being applied is never sent twice.
Events a host raises while it applies (a screen reporting its new size
during layout) wait in the host until `Apply` returns.

**View flattening** (as React Native's Fabric): a `View`, `Row` or
`Column` with no visual props, no events and no accessibility props makes
no native view. Its children are placed in the nearest real ancestor,
offset by its frame. Events and accessibility props are fixed at
creation; a `StyleFunc` that gains visual props makes its container real
and the flush moves its children into the new view (it then stays real).
`Show`, `For` and `Dynamic` are fragments: no box and no view, their
children splice into the parent's layout and native children.

## layout

Our own engine implementing CSS **block**, **flexbox** and **grid**
layout (in the spirit of Taffy, not a port of it or of Yoga), in points
(iOS points, Android dp; hosts convert to pixels).

- **Model.** A node is computed with known sizes (NaN when free), a
  percentage basis and the available space per axis: definite,
  max-content or min-content. A run either only sizes the node or also
  places its subtree. Leaves (text, controls) are measured by the host
  with the same inputs: `sizeThatFits:` on iOS, `View.measure` with
  `MeasureSpec`s on Android; min-content is the widest word.
- **Algorithms.** Flexbox per CSS Flexible Box §9 as browsers implement
  it (automatic minimum sizes, the freeze loop, intrinsic main sizes,
  wrap and wrap-reverse, baselines, auto margins, absolute children at
  their static positions). Grid per CSS Grid §8 and §12 (explicit and
  implicit tracks, `repeat` with `auto-fill`/`auto-fit`, `minmax`,
  `fit-content`, `fr`, sparse and dense auto-placement, spanning items,
  track alignment; lines clamped to ±10000 as browsers do). Block per
  CSS 2 (auto widths fill, vertical margins collapse between siblings,
  through empty boxes and with the parent's edges). Not yet: RTL,
  writing modes, floats, named grid lines and areas, sizing keywords
  (`min-content` etc. as widths), `safe` alignment.
- **Incremental.** Each node caches its final layout and eight
  measurements keyed by inputs; `MarkDirty` clears a node's caches and
  its ancestors'. A clean node asked the same thing answers at once, so
  changing one Text measures that Text and re-places only the boxes on
  the way up; nodes whose size is forced by tight inputs are never
  measured. A layout reports the nodes whose frame changed, so the flush
  sends only those `SetFrame`s.
- **Shared measure cache.** `ui` keeps one per root (two generations
  of up to 1024 entries), keyed by content: the kind, the text or
  title, the font, the line limit, the variant, the font scale and the
  constraints. Nodes showing the same thing (recycled list rows,
  repeated labels) ask the host once.
- **Performance.** All scratch memory (flex items and lines, grid
  tracks and items, block items) lives in a reusable `layout.Pass`:
  steady-state layout allocates nothing. Frames are snapped to the
  pixel grid by `ui` when it sends them: edges round to the nearest
  pixel, except that a measured leaf's size rounds up (rounding both
  edges can take a pixel off a label, and a label a pixel narrower
  wraps), as Yoga does for text.
- **Correctness** is defined by Chrome: `layout/testdata/css` holds
  Taffy's Chrome-generated fixtures (block, flex, grid; border-box,
  left to right), run as table tests; `known_failures.txt` lists the ones
  that don't match yet (mostly the unsupported features above).

## ops

The only contract between core and hosts. Its schema is written once in
Go (`ops/schema.go`); `go generate ./ops` writes the Go constants, the
Swift decoder (`hosts/ios`) and the Kotlin decoder (`hosts/android`),
each with event encoders.
Mobile hosts read the binary encoding; `host/fake` (and desktop hosts
later) read the batch's Go structs directly.

```
NodeID   uint32, made by the core, never reused in a process
Kind     uint16 (View, Text, Button, … then registered Native kinds)
PropID   uint16 per kind, from the schema

Create(id, kind)            Destroy(id)
Insert(parent, child, index)  Remove(parent, child)
SetProp(id, prop, value)    value: bool | int64 | float64 | string |
                            color | edges | enum | bytes
SetFrame(id, x, y, w, h)    relative to the native parent, in points
SetRoot(id)                 the view that fills the window
Listen(id, event)  Unlisten(id, event)
Command(id, cmd, args)      focus, blur, scrollTo, list updates,
                            push, pop, present, dismiss, …
```

Binary encoding: little-endian, a length-prefixed buffer of ops, strings
as UTF-8 with their length. The Go side reuses one buffer per batch (no
allocation per op).

Events from hosts:

```
Event(id, kind, payload)    press, longPress, textChanged(string),
                            valueChanged, toggled(bool), scrolled(x, y),
                            refresh, focus, blur, submit, popped,
                            dismissed, back (Android, unhandled)
Event(root, environment)    root size, safe area insets, keyboard overlap,
                            scale, appearance, text size, locale
Event(root, lifecycle)      foreground, background, memory warning
```

Synchronous requests from hosts (they return a value, so they are
function calls, not events): `ListCount(id)`, `ListBind(id, index,
cellID)`, `ListUnbind(id, cellID)`.

### Lists

`ui.List(items, key, row)` has `For`'s signature, backed by
UICollectionView and RecyclerView. Because the core is on the main
thread, the data source calls straight in:

- `ListBind` with a cell the host made: the core makes the row's owner
  under the cell (or reuses the one the cell last held, setting its
  `item` and `index` signals, which updates only changed props),
  flushes, and returns. Recycling a cell costs the props that differ.
- Row heights come from the core's layout of the row at the list's
  width (self-sizing cells measure through `ListBind`'s flush).
- Changes to `items` send `Command(list, listUpdate, item IDs)`: a
  stable int64 per key, in order. iOS applies them as an
  `NSDiffableDataSourceSnapshot` (once the call into Go has returned, so
  binding never re-enters a flush); Android diffs them with `DiffUtil`
  (likewise after the call returns) and dispatches item ranges and moves
  to the RecyclerView adapter. Bound rows whose item changed are updated
  in place by the core.
- Cells are views the host makes; their IDs have the high bit set. A
  cell's `SetFrame` carries the row's height.

### Text input

`ui.TextField(ui.TextFieldProps{Value: text})` where `text` is a
`*signal.Signal[string]`. The widget owns its text. Its edits write the
signal; the core remembers the last value the native side reported and
sends `SetProp` only when the signal differs from it, so typing never
round-trips and IME composition is never disturbed. Props for the
keyboard: type (text, number, email, URL, phone), return key, secure
entry, autocapitalization, autocorrection, content type (for AutoFill).

### Lifecycle and state

`app.OnLifecycle(func(app.State))` reports foreground and background.
On Android the Go core and the views live in the process, not the
Activity: views are made with the app's Material theme
(`ContextThemeWrapper` over the application). The Activity handles
rotation, size, dark mode, font scale and density itself
(`configChanges`); any other configuration change remakes it, and the
new Activity takes over the existing tree: `nui_attach` resends the
root (`ui.Reattach`) and every frame, with no app state lost. Android
views resolve theme colors and text sizes when they're made, so after a
dark mode, font scale or density change the host makes its text and
Material views again, replaying the props it was sent and keeping the
user's state (text, selection, checked, value). There's one Activity
(`singleTask`), and whichever instance comes to the front takes the
views. Process death loses state, as in any native app; saving state to
the device for that is later, under a name that says storage (say
`signal.Saved(key, initial)` over `UserDefaults` / `SharedPreferences`),
not to be confused with keeping state across `nui dev` reloads (below),
which needs no code.

## mobile

`mobile/` (build tags `ios || android`, cgo) exports a C ABI:

```c
void      nui_start(nui_host_callbacks*);   // apply, measure, post; calls main
void      nui_attach(void);                 // mount the tree again (new Activity)
void      nui_event(const uint8_t*, size_t);
int64_t   nui_list_count(uint32_t list);
void      nui_list_bind(uint32_t list, int64_t index, uint32_t cell);
void      nui_list_unbind(uint32_t list, uint32_t cell);
void      nui_run_posted(void);             // the host's main-thread hop
```

All calls come on the main thread. `post` from Go calls the host's
callback, which hops with `DispatchQueue.main.async` / `Handler.post`
and calls `nui_run_posted`. The header Swift and Kotlin compile against
is `hosts/ios/Sources/CNui/include/nui.h`.

An app is a `main` package whose `main` calls `app.Run(root)`. The CLI
adds a file to it with `go build -overlay` that imports `mobile` and
sets `mobile.Main = main`; `nui_start` calls it, and `app.Run` mounts the
root on the mobile host.

- iOS: `GOOS=ios GOARCH=arm64 CGO_ENABLED=1 go build
  -buildmode=c-archive`, with the SDK's clang (iphonesimulator for the
  simulator, arm64 and x86_64; iphoneos and an XCFramework for devices
  in M6). `hosts/ios` is a Swift package with the decoder (`NuiOps`),
  the UIKit host (`NuiHost`) and the C header (`CNui`); `App/` holds the
  `@main` app delegate. The CLI embeds them.
- Android: `GOOS=android CGO_ENABLED=1 go build -buildmode=c-shared` per
  ABI (arm64-v8a, x86_64; armeabi-v7a optional) with the NDK's clang,
  linked with 16 KB pages (`-z max-page-size=16384`) as new devices
  require. `mobile/jni_android.c` is the JNI side of the same C ABI:
  Kotlin's `dev.nui.NuiNative` calls in; the core calls
  `NuiRuntime.apply(ByteBuffer)` (a direct buffer over the encoded
  batch, one call per batch), `measure` and `post` back.
  `hosts/android` is a Gradle project: the `nui` library (decoder,
  Views host, JNI bindings) and `app`, the shell app the CLI builds per
  app (application ID and name passed as Gradle properties).

The CLI does these builds itself (no gomobile).

## CLI (`cmd/nui`)

```
nui new <dir>                     a project from the template
nui dev -ios|-android <dir>       build, run, rebuild and restart on save
nui build -ios|-android <dir>     a release build for the stores
nui dev -mac <dir>                build and run on this Mac, rerun on save
nui build -mac <dir>              a universal .app, signed (-sign; default ad hoc)
nui dev -windows <dir>            build and run on this PC, rerun on save
nui build -windows <dir>          a folder: the .exe (icon, manifest, version) and its assets
```

**An app's metadata** is `nui.json` beside its Go code, all optional:
name, ID (bundle ID and application ID), version and build number, a
1024×1024 icon, orientations (the phone's: an iPad or a large Android
screen turns the app any way, for split screen), the iOS team (and
whether it's iPhone only), provisioning profile, usage
descriptions and extra Info.plist keys, the Android permissions and
upload key. `nui dev` and `nui build` read it alike.

**The accent color** (`"accent": "#7c3aed"`) is the one color an app's
brand sets on native controls; the controls are the platform's, so
nui has no theme of its own over them (`Style` changes one view). It
travels with the Go code, not the shell: the CLI builds it in
(`-ldflags -X`, `internal/boot`), and each root, as it mounts, sends
the `Accent` command before any view is made. That way the dev player,
which runs any app, takes each one's, and drops it between apps. On iOS
it's the windows' tint color, which UIKit's controls and `ui.Accent`
take. On Android it's the seed of a Material 3 color scheme
(`SchemeContent`, light and dark, as Material's `DynamicColors` makes
it) on Android 12 and later; before that, Material's baseline colors,
with nui's own uses of the accent (links, `ui.Accent`) taking the color
itself. `"android": {"dynamicColor": true}` takes the wallpaper's colors
instead, where the system has them. The theme context is made again
and the views that resolved theme colors are rebuilt, as on a dark mode
change.

**`nui build -ios`** makes an `.ipa`, still without an Xcode project:
the host compiled optimized (no debug automation), the Go archive for
`arm64` devices, linked with dead stripping; an Info.plist with the
keys App Store validation checks (`DT*` from the SDK and Xcode,
`ITSAppUsesNonExemptEncryption`); the icon as an asset catalog compiled
by `actool`; then signing as Xcode would: the installed provisioning
profile that fits the app ID best (exact before prefix before `*`) for
the distribution (`app-store` by default, `ad-hoc`, `development`),
unexpired, whose certificate's identity is in the keychain; its
entitlements, with a wildcard app ID made the app's; `codesign` with
the identity's SHA-1, verified. Uploading is Transporter's or `altool`'s.
`-simulator` makes the same release build for the simulator.

**`nui build -android`** makes an `.aab` for Google Play and an `.apk`:
the Go code as a stripped `.so` for arm64-v8a, armeabi-v7a and x86_64,
the assets, launcher icons (legacy per density, and adaptive: the icon
inside the safe zone over its corner color), the permissions, the
version; Kotlin shrunk by R8 (the classes JNI and reflection reach are
kept); signed with the upload key named in `nui.json` or `NUI_KEYSTORE`,
its passwords from the environment (never on a command line). A
release build never loads a library from the app's files, as debug
builds do in development; cleartext HTTP (the player's assets) is
debug-only, as is iOS's local networking exception.

**`nui dev` runs the app's Go code on the Mac, and its views in a
player** (`internal/remote`): the platform host with a small Go library
(`mobile/player`) in place of an app, built and installed once and
shared by every app. A change rebuilds the Go code for the Mac and
restarts that process, under a second; the player reconnects and shows
the new code, with no install. The app's logs are the process's output.

- **The protocol** is the host interface over a socket, synchronous both
  ways, with calls nesting as they do in process: while a side waits
  for its reply it serves the other's requests (a list binding cells in
  the middle of a batch, a measure during layout). The core takes no
  events while it waits for a player (hosts never send them in the
  middle of an `Apply`); one that arrives meanwhile is served when the
  call returns. Both ends are Go, so the protocol is tested in Go, with
  the fake host as the player. A call that gets no reply in 10s gives
  up rather than hang a main thread.
- **Several players at once**: each connection is a session with its
  own root, multiplexed on the core's one loop (the app on an iPhone
  and an Android phone together). A reconnecting player clears the last
  session's views first.
- **State survives a reload** without code, as React's Fast Refresh
  keeps hooks' state: the process saves every app signal's value as it
  stops, and the next run gives it back to the signal made at the same
  place: the same call path (function names), type and position among
  the live signals of that type there. A signal made by nui itself
  isn't kept, nor a value JSON would change (funcs, unexported fields).
  Adding a signal before others of its type made at the same place, or
  changing its type, resets those. Navigation isn't kept (screens are
  closures): a reload starts at the root screen. Off in a built app,
  where `signal.New` pays one nil check.
- **Reaching the Mac**: the simulator shares its loopback; an Android
  emulator reaches it at `10.0.2.2`, directly (`adb reverse` adds a
  round trip's worth of latency to every call); a device goes through
  `adb reverse`. Assets are served over HTTP from the Mac
  (`boot.AssetBase`).
- **Not in the player**: an app's own native code (`ios/`, `android/`),
  which only an app built with it can show. Such apps, `-once` and
  `-embed` build the app with its Go code, as before: `nui dev`
  rebuilds the library and relaunches on the iOS simulator (no Xcode
  project: `swiftc` compiles the embedded host, with the app's Swift,
  into one object, cached; each change rebuilds the archive, links,
  ad-hoc signs and reinstalls with `xcrun simctl`, in about a second)
  or an Android device or emulator (Gradle builds the shell app, with
  the app's Kotlin, once per host version and app ID; each change
  rebuilds the `.so`, pushes it into the app's private files with `adb`
  and `run-as`, where the debug shell app loads it from, and restarts
  the app: about a second and a half).

## Repository layout

```
nui/
  signal/            reactivity
  ui/                components, nodes, flush, flattening, navigation
  layout/            flexbox
  ops/               schema, Go encoder; generate writes Swift/Kotlin decoders
  host/              Host interface, Environment
    fake/            records ops, measures by fixed rules; for tests
  mobile/            C ABI (cgo, ios||android)
  hosts/ios/         Swift package
  hosts/android/     Kotlin/Gradle library
  app/               app.Root, lifecycle
  cmd/nui/           CLI
  template/          what `nui new` copies
  examples/counter/  examples/todo/  examples/list10k/  examples/navigation/
```

Desktop adds `host/darwin` and `internal/darwin` (macOS),
`host/windows` and `internal/windows` (Windows), and `host/linux` later.

## Testing

- `signal`: unit tests for glitch-freedom (diamonds), laziness of memos,
  disposal (no effect runs after its owner is disposed), batching, and
  the panics. Benchmarks from js-reactivity-benchmark's cases.
- `layout`: Chrome's results on Taffy's CSS fixtures as table tests,
  plus counts (measures, changed frames) and allocations.
- `ui` with `host/fake`: assert the op streams. Performance is asserted
  by counts, never time:
  - one `Set` on a signal one `Text` reads gives exactly one `SetProp`,
    at most one `Measure`, and only the `SetFrame`s of boxes that moved;
  - `For` moving one of 1,000 rows gives one `Remove` and one `Insert`;
  - a `List` of 10,000 items binds only the cells the host asks for;
  - rebinding a recycled cell to an item that differs in one field
    gives one `SetProp`;
  - a flattened container makes no native view;
  - popping a screen disposes every owner it made;
  - steady-state flushes allocate nothing (`testing.AllocsPerRun`).
- `host/darwin` (macOS): tests mount components on the real AppKit
  host, on the main thread (`TestMain` keeps it), and check the views
  (their text, frames, clicks, list binding, navigation, disposal).
- `host/windows` (Windows), likewise on real Win32 windows: a click
  through the BUTTON's `BM_CLICK`, typing into the EDIT, a 10,000-row
  list binding only what shows, a stack's back button disposing the
  screen, and disposal destroying every window.
- `ops`: the Go encoder and the generated Swift and Kotlin decoders
  round-trip the same golden batches (`swift test --package-path
  hosts/ios`; `./gradlew :nui:testDebugUnitTest` in `hosts/android`).
- Hosts: GUI tests (`e2e/`, build tag `e2e`, in CI on a simulator and
  an emulator) build `examples/` with `nui dev -once` and drive them
  through the native views. Debug builds take automation commands (tap,
  type, submit, back, tab, rotate, dump: what the screen shows), on iOS from a file in the
  app's container on the `dev.nui.automation` Darwin notification
  (`hosts/ios/Sources/NuiHost/Automation.swift`), on Android from a
  `dev.nui.automation` broadcast (`Automation.kt`; the dump read with
  `run-as`). So both platforms run the same test, and neither needs
  UI Automator's single connection, which device tools hold. Each test
  waits for the screen to show what it expects, bounded so a broken app
  fails.
- Tests never pass or fail on wall-clock time: wait for the condition,
  count the work.

## Milestones

Each one ends with its examples running and its tests in CI.

- **M0, core without UI** (done): `signal`, `ui` with `host/fake`,
  `layout`, `ops` with the Go encoder. Everything above under `go test`.
- **M1, iOS** (done): Swift codegen for `ops`, `mobile/`, `hosts/ios`,
  `nui new`, `nui dev -ios` on the simulator. Counter, todo (`For`,
  `TextField`, `Toggle`, `SafeArea` with the keyboard) and list10k
  (`List`) run. Not yet: XCUITest GUI tests and CI on macOS runners.
- **M2, Android** (done): Kotlin codegen, `hosts/android`,
  `nui dev -android` on the emulator, Activity recreation
  (`nui_attach`). The same examples run unchanged; live rotation, dark
  mode, font scale and the keyboard (insets, animated) work. Not yet:
  Espresso GUI tests.
- **M3, mobile app structure** (done): `Stack` with native back
  gestures (iOS swipe, Android predictive back), `Tabs`, `Sheet` (detents,
  grabber, the keyboard), `Alert` and action sheets, `Menu` (buttons,
  tap and long press), pull to refresh, haptics; the navigation example
  runs on both. Not yet: gestures beyond press and long press (pan,
  pinch, swipe: moved to M4).
- **M4, the rest of mobile v0** (done): gestures (pan, pinch, swipe),
  `ui.Native` with the app's Swift and Kotlin, native animations
  (`ui.Animate`), images loaded and decoded off the main thread by the
  host, with a cache; the media example runs on both. (`Activity`, the
  spinner, came with M1.)
- **M5, dev without reinstalling** (done): the socket transport and the
  player apps (iOS simulator, Android emulator and devices); app state
  survives a reload with no code. Not yet: physical iOS devices (the
  player would need signing and the Mac's address).
- **M6, shipping** (done): `nui.json`, `nui build -ios` (signed `.ipa`;
  `-simulator`), `nui build -android` (`.aab` and `.apk`). Not yet:
  uploading, screenshots and store listings (Transporter and the Play
  Console do those).
- **Split screen** (done): an iOS build is an iPad app too
  (`UIDeviceFamily` 1 and 2; `ios.iphoneOnly` leaves iPad out), in every
  orientation there, as Split View, Slide Over and Stage Manager need;
  the Android Activity is resizable and handles size changes live
  (`configChanges`). Both report the window's size, not the screen's, as
  the environment, so the root lays out again at any size the user
  gives the app, and its size classes (`Environment.WidthClass`,
  `HeightClass`): UIKit's on iOS (compact; regular as expanded),
  Material's window size classes on Android (compact, medium,
  expanded), each platform's own, so an app that branches on them
  follows each one's conventions. A host that reports none (macOS,
  Windows) gets Material's, which the core works out from the size.
- **Rich text** (done): `ui.RichText` (spans, per-line backgrounds,
  links, per-paragraph layout cache prepared off the main thread, inline
  components), links as accessibility elements, `ui.Selectable`
  (selection across texts), and the `markdown` package (incremental,
  streaming). See "Rich text".
- **M7, toward a production release** (in progress). Done:
  - `ui.Symbol` and icons on buttons and actions: SF Symbols on iOS; on
    Android their Material Symbols, a curated set generated by
    `hosts/android/gen_symbols.go` with TalkBack labels.
  - Multiline `TextField`, growing to `Lines` lines, then scrolling.
  - `ui.Segmented`, `ui.Progress` and `ui.DatePicker`, each the
    platform's control (Material's pickers on Android).
  - List swipe actions, `OnEndReached` and `ui.ScrollToItem`.
  - `ui.OnFrame`: a callback each vsync (CADisplayLink at up to 120 Hz,
    Choreographer) while one is registered, for animation the app
    computes, as TimelineView(.animation) and withFrameNanos.
  - `ui.Video` (AVPlayerViewController; Media3's ExoPlayer and
    PlayerView): its natural size, the platform's controls, AutoPlay,
    Loop, Muted, `OnEnd`; `ui.Play`, `ui.Pause`, `ui.Seek`.
  - `ui.WebView` (WKWebView, Android's WebView): a URL, an asset or
    markup; `OnLoad`, `OnError`; the page posts to Go with
    `window.nui.postMessage`, the same on both, and `ui.EvalJS` runs
    script in it.
  - Grids: `ListProps.Columns` and `Gap` (a compositional grid on iOS,
    a GridLayoutManager on Android); the core lays each cell out at
    exactly the column width the host gives it.
  - `nui/store` (small values kept across launches, `store.Signal`),
    `ui.Copy`, `ui.Share`.
  - The app's accent color (`nui.json`).
  - The device's time zone as Go's `time.Local`.
  - Motion: `Style.Transform`, shadows, gradients, `Style.Material`
    (blur), repeating animations, enter and exit `Transition`s.
  - `nui/sqlite` (pure Go, live queries with `sqlite.Watch`), native
    modules (`ui.Call`: `nui/secure`, the keychain and Keystore;
    `nui/device`: info, an in-app browser, the photo picker).
  - A search field in the navigation bar (`ScreenOptions.Search`).
  - Incoming links: `nui.json`'s `scheme` and `links`, `ui.OnOpenURL`.
  - Custom fonts (`Font.Family`).
  - Accessibility: pressable views as buttons, `Decorative`,
    `ui.Announce`, swipe actions for TalkBack, Reduce Motion.
  - Scroll position: `OnScroll` on `Scroll` and `List`, `ui.ScrollTo`
    and `ui.ScrollToEnd`. Android posts its scroll events to the next
    turn of the main loop: a scroll runs while the view draws, and the
    core changing views then stops it.
  - GUI tests on CI (`e2e/`): the counter; notes (a text field,
    SQLite, a List, a relaunch); navigation (a push, a sheet with a
    field, back, tabs), on both platforms.
  - Images fetched over http(s) kept on disk across launches (256 MB,
    least recently used first; not a `no-store` response, nor nui dev's
    assets from this machine), and `ImageProps.Placeholder`.
  - `examples/gallery` shows each; `examples/notes` is SQLite's.

  docs/parity.md has what nui has against SwiftUI and Expo;
  docs/device-checklist.md, what's left to check on real devices.

  Next:
  - More accessibility: adjustable custom controls, and checking every component with VoiceOver and
    TalkBack on a device.
  - More GUI tests: gestures, alerts and menus, the gallery.
- **Then desktop** (below), only when it's called for: until then the
  work is maturing iOS and Android.
- **macOS** (in progress): `host/darwin`, AppKit in the app's process
  through purego (`internal/darwin`, MyGo's Objective-C runtime layer),
  no cgo; `app.Run` runs it on macOS. `nui dev -mac` (rebuild and rerun
  on save, signals' state kept) and `nui build -mac` (a universal,
  signed `.app` with its icon and assets). The examples run. Done: the
  v0 components, navigation (below), rich text (an `NSTextView`), shapes,
  images, symbols, gestures, menus, animation, `WebView`, `Video`; tests
  in `host/darwin` drive real AppKit views. Native modules: `secureStore`
  (the login Keychain, which an app signed ad hoc can use), `device`
  (info, clipboard, keep awake), `browser` (the user's browser), `photos`
  (an open panel). Screen reader actions and adjustable views
  (`NSAccessibilityCustomAction`, increment and decrement); enter and
  exit transitions (the frame carries the scale and offset). Windows,
  the menu bar and shortcuts (below): each `ui.Window` an `NSWindow`
  with its own toolbar and title (its innermost stack's), cascaded from
  the window in use; sheets and alerts go over the window in use. The
  app's menus join the Mac's own: one titled File, Edit, View, Window or
  Help adds its items to it, the others go between View and Window; a
  disabled item is off through `validateMenuItem:`, as is Back with no
  screen to go back from. Not yet: `ui.Native`, the other modules (network,
  location, notifications, camera, audio…), matched views (`Match`),
  notarization.
- **Windows** (in progress): `host/windows`, Win32 and the common
  controls (version 6) in the app's process through `syscall`
  (`internal/windows`, much of it MyGo's), no cgo, no WinUI; `app.Run`
  runs it on Windows (amd64). `nui dev -windows` (rebuild and rerun on
  save, signals' state kept) and `nui build -windows` (a folder of the
  `.exe`, with its icon, manifest and version built in, and its
  assets; from any system). The examples run (counter, todo, list10k,
  navigation, gallery, richtext, markdown, notes, media but its native
  view, usage, downloader), light and dark, at any DPI. Done: the v0
  components, navigation, rich text (RichEdit), shapes, images, symbols
  (Segoe Fluent Icons), gestures (press, long press and right-click,
  pan and swipe with the mouse), menus, animation of frames, `WebView`
  and `Video` (WebView2), the accent and color scheme; windows, the
  menu bar and shortcuts (below): each `ui.Window` a top-level window of its
  own, the app's menus an `HMENU` on the main window with an
  accelerator table that works in all of them, `WindowOptions` the main
  window's title, client size and least size; tests in `host/windows`
  drive real windows. Not yet: `ui.Native`, the native modules (`ui.Call`),
  screen reader props beyond the controls' own (labels, roles,
  actions, announcements: UI Automation), shadows, transforms,
  materials, opacity between 0 and 1 for a view's children, enter/exit
  transitions and matched views, pinch and rotate, pull to refresh,
  `ScreenOptions.Search`, a dark date
  picker, the share sheet (Share copies the text), arm64 builds, code
  signing.

## Rich text

Platform labels can't express everything a rich text model needs: span
backgrounds shaped per line (radius, box-decoration-break), inline
components that wrap with the line, selection that crosses paragraphs
(markdown, chat transcripts), and cheap updates to long streaming text
(an append re-typesets the whole string on the main thread).
`ui.RichText` owns its text model and its view; `ui.Text` stays a
platform label, with native accessibility, Dynamic Type and behavior.

**Built:**

- The model is `ui.Rich`, a list of `ui.Span`s (text, font fields that
  default to the view's, italic, monospaced, color, underline,
  strikethrough, a background, a link); a `"\n"` starts a paragraph.
  Helpers: `Plain`, `Strong`, `Em`, `Code`, `Link`, `.Colored`,
  `.Highlighted`, `.Weighted`. The view's `Style` (font, color, align,
  lines) is the spans' default, through the common text props.
- On the wire it's one prop: a table of the spans' styles, then the
  paragraphs' runs, each a style index and text. The core sends it only
  when the encoding changed.
- Each host typesets with its platform's engine (CoreText's typesetter
  on iOS, `StaticLayout` on Android) and draws in a view of nui's own
  exactly the layout it measured: backgrounds per line fragment with
  rounded corners (fitted to the text, not past a line's end), then the
  text, then underlines and strikethroughs (CoreText draws neither).
  Line limits truncate the last line with an ellipsis; Android takes
  the text appearance's line height, as `TextView` does.
- Layouts are cached per paragraph by its bytes (runs and their
  styles), the view's text props, the width and the environment (text
  size, dark mode), so appending to a streaming reply typesets only the
  paragraph being written.
- Dynamic Type: the core measures as soon as the text size changes,
  before UIKit hands views their new traits, so iOS measures and draws
  with the app's content size category and the view's other traits.
- Links are hit-tested on the typeset lines and reported as `Link`.

- Links are accessibility elements of their own, so a screen reader
  finds and activates each: `UIAccessibilityElement`s on iOS (the text
  becomes a container of the whole text and its links),
  `ExploreByTouchHelper` virtual views on Android.
- Layouts are prepared off the main thread. When a text's content
  changes, the paragraphs not in the cache start typesetting on
  background threads (CoreText's typesetter and `StaticLayout` are
  thread-safe) at the width the view was last laid out at. The main
  thread, measuring moments later, takes a layout that's done, waits for
  one in flight (the rest of its work, not all of it), or typesets it
  itself. The attributed or spanned strings are built on the main thread
  (they read the view's tint and theme); the line breaking, the work,
  isn't. Texts set in one flush typeset in parallel.
- Inline components: `ui.Embed(key, build)` is a span that's a component
  (an image, a badge, a button), sitting on the baseline and wrapping
  with the words. It's built once for its key, a child view of the
  RichText and a layout root of its own, laid out at its max-content
  size (at most the screen's width). In the text it's U+FFFC with a flag.
  Its size has to reach the host before the host measures the text, and
  the host can measure the component's leaves only once their props are
  applied. So a flush lays out inline components after applying the
  props and before the layout that measures the texts. When a size
  changed, the sizes (`RichText.Inlines`: node ids and sizes) go out in
  a crossing of their own; otherwise there's none. The host reserves
  each placeholder (a `CTRunDelegate`, a `ReplacementSpan`) and puts the
  child there: its frame's size is the core's, its origin the text's.
- `ui.Selectable` makes the RichTexts inside it one text (joined by
  newlines) with one selection, as the platform selects:
  - iOS: `UITextInteraction` (loupe, handles, the edit menu with Copy and
    Select All) on a `UITextInput` overlay that answers with the texts'
    geometry. The overlay is hidden from VoiceOver, so the texts and
    links stay accessible. Touches on a text in a scroll view within
    (a code block) are the scroll view's.
  - Android: as a `TextView` selects. A long press selects a word and a
    drag goes on word by word. The theme's handles move the ends, which
    never cross. The floating action mode has Copy and Select all, and a
    tap clears. Each text draws its part of the selection in the theme's
    highlight color. A scroll view's taking the gesture over cancels the
    long press.
  - Copying leaves out the inline components.
- The `markdown` package renders Markdown with incremental parsing, the
  parser vendored from MyGo (`markdown/parser`). `Document.Set` reparses
  only from the last block that changed, `SetStreaming` heals what a
  stream leaves open (an unclosed emphasis, a link being written), and
  each block has a revision. The view is a `ui.For` over blocks keyed by
  start and kind, each with comparable descriptors, so a streaming reply
  updates only the RichText of the block being written. Paragraphs,
  headings, lists (tight and loose, ordered, tasks), quotes, code, math
  and HTML blocks (horizontally scrolled; tabs expanded to 4 columns),
  tables, thematic breaks and footnotes. Images alone in a paragraph are
  images; in text they're inline components, a linked one following its
  link. `Props.Code` replaces code blocks' rendering (for highlighting).
  The view is selectable unless `NotSelectable`; links open with
  `ui.OpenURL` unless `OnLink` says otherwise.
  `markdown.Theme`, as MyGo's, sets fonts, colors and spacing per kind
  of block and inline; what it leaves zero is `DefaultTheme`'s. Its
  fonts are text styles (they follow the user's text size, a field left
  zero keeps the default's) and its colors the platform's semantic ones
  (dark mode needs nothing), so a view with no theme looks native.

**Not yet:** selection for screen reader users (VoiceOver and TalkBack
read the texts and links but don't select across them); bidirectional
text in selection geometry (the rectangles assume left-to-right lines).

## Desktop, later

After M6, on the same core, `ops` and app code:

| Platform | Host | Notes |
|---|---|---|
| macOS | Go, AppKit through purego (`host/darwin`) | No cgo. `internal/darwin` is MyGo's Objective-C runtime layer (classes made at runtime, blocks, `WithPool`, a main run loop source for `Post`). In progress (see Milestones). |
| Windows | Go, Win32 with common controls v6 through `syscall` (`host/windows`) | No cgo; `internal/windows` is the Win32, GDI, GDI+, uxtheme, DWM and WebView2 layer. Not WinUI 3: it needs the Windows App SDK beside the app, and XAML is a second reconciler. `List` is core-virtualized: no Win32 list hosts arbitrary rows. In progress (see Milestones). |
| Linux | Go, GTK 4 through purego (`host/linux`) | libadwaita optional. |

Desktop hosts run in process: `app.Run` locks the main OS thread and
runs the platform's event loop there, and reads the batch's Go structs
without encoding. purego callbacks are made once per signature and
routed by user data, never per view.

How the macOS host maps nui (`host/darwin`):

- One window holds the root, its content a flipped view; the
  environment is the content's size, the display's scale, dark mode,
  Reduce Motion, locale and time zone (no safe area, keyboard or text
  size). Frames are set directly: AppKit's coordinates are flipped to
  nui's (top left) by flipped container views.
- Controls are AppKit's: `NSTextField` (labels and fields; secure ones
  swap to `NSSecureTextField`, multiline ones to an `NSTextView`),
  `NSButton` (Plain is borderless text in the accent color, Filled the
  accent bezel), `NSSwitch`, `NSSlider`, `NSStepper`,
  `NSSegmentedControl`, `NSPopUpButton` for `Wheel`, `NSDatePicker`,
  `NSColorWell`, `NSProgressIndicator`, `NSImageView`, `NSScrollView`.
  Menus are `NSMenu`s: a button's pops up, a view's `LongPress` menu is
  its context menu (a secondary click). A secondary click is also a
  long press.
- A `Stack` shows its top screen and slides on push and pop; the
  window's toolbar belongs to the innermost stack on screen (a back
  button, the top screen's bar buttons and search field) and its title
  is the screen's. `Tabs` is an `NSTabView`; a `Sheet` a sheet window
  (Escape dismisses it); an `Alert` an `NSAlert` shown as a sheet.
- `List` is core-virtualized, as on Windows: a scroll view whose rows
  the host places, binds through `ListBind` and recycles as it
  scrolls, heights from the cells' frames (estimated until bound).
  `NSTableView` would want row heights from a callback returning a
  float, which purego can't make.
- `RichText` is a non-editable `NSTextView` (TextKit lays it out and
  draws it, links, selection and accessibility come with it); outside
  a `Selectable` it takes clicks only on links. Inline components are
  attachments of their size, their views placed at the attachment.
- Messages with float or struct arguments go through purego's typed
  functions (`internal/darwin`'s `Msg*`), which use reflection: setting a
  frame allocates. The rest go through `SyscallN`.
- Debug automation: `NUI_SNAPSHOT=file.png` renders the window to a PNG
  once it has shown, without taking focus (`NUI_DUMP=1` writes the view
  tree beside it), then quits.

How the Windows host maps nui (`host/windows`):

- One thread: the main goroutine is locked to the main thread (the
  package's `init`), which runs the message loop; `Post` queues work and
  posts a message to a message-only window. One window procedure, made
  once with `syscall.NewCallback`, serves every window class and routes
  by window to its node; controls are subclassed (`SetWindowSubclass`)
  through one callback too. `IsDialogMessage` gives Tab and arrow key
  navigation. A control that would run a modal loop (a menu, a task
  dialog, the color dialog) runs it once the core has returned.
- Every real node is a child window. The app's window holds the root;
  the environment is its client area's size in points (DIPs), its DPI
  as the scale (per-monitor DPI awareness v2), the text size
  (Accessibility's `TextScaleFactor`, as FontScale), dark mode (the
  app mode in the registry; the title bar through DWM, menus and scroll
  bars through uxtheme's app mode), animations off as Reduce Motion,
  the locale, and the time zone as an IANA name (Windows's ICU).
  `Environment.Platform` is `PlatformWindows`: the environment's
  platform bits go from two to three (bits 2-4), which the mobile hosts,
  sending only two, still decode.
- Views draw themselves (a window class of the host's) into a buffered
  DC: first what's behind them where they aren't opaque (the parent and
  the siblings they overlap, drawn again into the DC, as
  `DrawThemeParentBackground` would but recursively), then the
  background, border, corner radius, gradient (GDI+, antialiased,
  blended in sRGB), then the content; text with GDI (`DrawText`, which
  also measures it, with the same font: min-content is the widest
  word). Views that take no mouse events are `HTTRANSPARENT`, so a
  click on a text reaches the pressable row under it. A pressable view
  shows Fluent's hover and pressed fills and takes Enter and Space.
- Controls are the common controls, drawn as Windows 11 draws its own
  (WinUI's Fluent styles) through custom draw, so they look current in
  light and dark mode and keep their keyboard, focus and accessibility:
  `BUTTON` (Filled is the accent button, Plain accent text, Tinted an
  accent wash, Gray and Bordered the standard button), a check box
  drawn as Fluent's toggle switch (Win32 has no switch), the trackbar
  as Fluent's slider, `EDIT` in a frame the host draws (secure with a
  password character, multiline with return, Number with `ES_NUMBER`),
  `COMBOBOX` for `Wheel`, the date and time picker, two buttons for
  `Stepper`, a swatch opening the color dialog for `ColorPicker`. The
  segmented control, progress bar and ring are the host's own views,
  drawn as Fluent's. Each control sits in a view of the host's, which
  the core places: some controls send their notifications to the parent
  they were made in, so a control never changes parent.
- Fonts are Segoe UI Variable (Segoe UI before Windows 11), its optical
  size picked by the size (Small below 12.5, Display from 20); the text
  styles are the Windows 11 type ramp's nearest: LargeTitle is Title
  Large (40 semibold), Title is Title (28 semibold), Title2 Subtitle (20
  semibold), Title3 Body Large (18), Headline Body Strong (14
  semibold), Body and Callout Body (14), Subheadline 13, Footnote and
  Caption Caption (12), Caption2 11. Monospace is Cascadia Mono, serif
  Georgia; the app's fonts are added for the process. Symbols are SF
  Symbol names drawn as Segoe Fluent Icons glyphs, a curated set (one
  with no glyph shows nothing and is reported once).
- A `Stack` shows its top screen under a header bar of its own: a back
  button (Alt+Left too) while there's a screen to go back to, the
  screen's title, its bar buttons. `Tabs` are a bar on top, as WinUI's
  top `NavigationView`. A `Sheet` is a modal window over the window in
  use (the one activated last: the main one, a `Window`, a sheet; the
  owner disabled meanwhile; closing it or Escape dismisses it), half
  the window's height for a medium-only sheet. An `Alert` is a task
  dialog over the window in use; an action sheet's buttons are command
  links. A menu is a
  popup menu: a button's drops below it, a view's `LongPress` menu is
  its context menu (a right-click, or a long press).
- A `Window` is a top-level window of its own (resizable, its own title
  and taskbar button; 480 by 600 points if the app gives no size),
  placed cascaded from the window in use. Its content, the node's
  child, is laid out at its client size, reported as its
  `Environment` when it opens and as it's resized; its least size is
  `WM_GETMINMAXINFO`'s. Closing it sends `Dismissed`, and the core
  destroys it (with the sheets over it); it shows once the main window
  does. Stacks in it have their header bar as anywhere, and Alt+Left
  pops the innermost stack of the window it's pressed in.
- Scale is per top-level window: the main one, each `Window` and each
  sheet has its DPI's (a `Window` or sheet opens at the DPI of the
  monitor it opens on). Each node knows its window's scale (`k`, 0 for
  the main window's), which it takes when it's inserted into a view of
  another window's, as list rows and cells, header and tab bars and
  scroll thumbs take their view's; points become pixels, fonts are made
  (cached by their size in pixels), text is measured, controls are
  sized and custom-drawn controls and scroll bars draw at it, and the
  window's `Environment.Scale` is it (the root's for the main window, the
  `Window`'s or `Sheet`'s, and their screens'). On `WM_DPICHANGED` a
  window takes the size Windows suggests and its views their new scale:
  fonts set again, frames placed again in pixels, lists tiled again,
  rich text formatted again; its environment is reported, and the core
  measures what's in it again (its measure cache is keyed by the scale
  of the element's nearest provider, so two windows at two scales never
  share a measurement) and snaps its frames to its pixels. The other
  windows keep theirs.
- The app's menus (`MenuBar`) are a menu bar on the main window only,
  as Windows apps' secondary windows (a tool window, an inspector) have
  none; their shortcuts work in every window of the app. The bar is
  made again (`CreateMenu`, `SetMenu`) each time the core sends the
  menus, none while it sends none: each title and item gets an access
  key (its first letter no other took: Alt+F), an item its shortcut
  after a tab ("Ctrl+Shift+K", "Ctrl+Enter", "F5"; Primary and Control
  are both Ctrl), a disabled one grayed, a checked one checked
  and a destructive one red (below). The shortcuts are an
  accelerator table (`CreateAcceleratorTable`), translated in the
  message loop against the window the key is for: a character is its
  virtual key on the keyboard's layout (`VkKeyScan`, with Shift if it
  takes it), the named keys theirs; disabled items have none. A chosen
  item, clicked or by its shortcut, comes as `WM_COMMAND` (ID 0x100 and
  up) and goes to the core as `Action` on the root. Popup menus follow
  the app's dark mode through uxtheme's app mode; the bar, which
  Windows draws light whatever the theme, the window draws dark while
  the app is (uxtheme's undocumented `WM_UAHDRAWMENU` and
  `WM_UAHDRAWMENUITEM`, as Notepad++ and MyGo do, with Windows 11's
  rounded backplates). A menu bar set before the window shows keeps
  its client size.
- A destructive item (a `MenuItem` with `Destructive`, an `Action` with
  `ActionDestructive` in a popup menu) is red, Fluent's critical text
  color, as macOS's (its title; its shortcut stays the menu's color).
  A Win32 menu can't color an item, so a menu with a destructive item
  is owner-drawn (`MFT_OWNERDRAW`, `WM_MEASUREITEM`, `WM_DRAWITEM`),
  all its items: one owner-drawn item takes the visual style off the
  menu's others, which the system then draws classic. They're drawn as
  the system draws a themed menu's, with the visual style's Menu parts
  (Windows 11's `ImmersiveStart::Menu`, `DarkMode_ImmersiveStart::Menu`;
  as the system's, light menus' separators are the plain `Menu`
  class's and dark menus' check marks and disabled gray
  `DarkMode::Menu`'s) at the owner window's DPI: the menu font, the
  height and margins (the check mark's), the check mark, the separator,
  the shortcut column (the submenu arrow's margins to its right),
  disabled gray; selected, the rounded backplate (`MPI_HOT`) under the
  mouse and a hairline rounded outline from the keyboard (the item's
  `ODS_NOFOCUSRECT` says which). The menu window would get the classic
  frame, so while such a menu is open a thread CBT hook subclasses the
  menu windows made (`#32768`): the frame is painted the menu's
  background, and DWM rounds the corners (`DWMWCP_ROUNDSMALL`) and
  draws the border in Windows 11's menu border color. Measured against
  the system's own menu at 192 DPI, light and dark, the menu is the
  same size and its items' pixels the same but the outline's corners.
  The items keep their text, so access keys, first letters and arrows
  work as in any menu; a key no item takes leaves it open
  (`WM_MENUCHAR`, `MNC_IGNORE`).
- `WindowOptions` set the main window's title, its client size
  (centered in the work area) and its least size (320 points square if
  the app gives none).
- The main window comes back where the user left it, as macOS's frame
  autosave has it: its normal rectangle (screen pixels, and the DPI it
  was at) and whether it was maximized, in `nui-window.json` in the
  app's data directory (`NUI_DATA`, Local AppData\<name>; a file rather
  than the registry, so it goes with the app's other data, and `nui
  dev` keeps it in its work directory). It's saved when a move or
  resize ends (`WM_EXITSIZEMOVE`), on maximize and restore and keyboard
  snaps (`WM_SIZE`, `WM_MOVE` outside a drag), and on close, not on
  every pixel of a drag; and restored before the window shows, over
  `WindowOptions`' size (their least size still holds): moved to its
  monitor first, so the window takes that DPI, then sized (scaled if
  that monitor's DPI is another now), and put whole on the nearest
  monitor's work area (`MonitorFromRect`) if it's off the monitors or
  mostly off one. The host keeps the normal rectangle itself, in screen
  coordinates (`GetWindowPlacement`'s are workspace coordinates). Tests
  (`go test`) and automation runs (`NUI_SNAPSHOT`, `NUI_SCRIPT`) keep
  none, and the data directory is made by what writes there.
- `List` is core-virtualized, as on macOS: rows the host places at
  their offsets in the list's window (one `DeferWindowPos`), binds
  through `ListBind` and recycles. Scroll bars are an indicator over the
  content, as WinUI's: a thin thumb (a window shaped by a region) shown
  while scrolling and under the mouse, which grows and drags; Win32's
  own scroll bars would take their width from the content the core laid
  out.
- `RichText` is a read-only rich edit (msftedit): spans are character
  formats, links the host's own (colored, hit-tested and clicked by the
  host, as the control draws its links in a color of its own), inline
  components transparent pictures of their size (`EM_INSERTIMAGE`) with
  their views placed on them; its background is sampled from what's
  behind it (a rich edit can't be transparent). Selection across texts
  in a `Selectable` isn't one selection yet: each text selects its own.
- `WebView` is WebView2 (the runtime found in EdgeUpdate's registry
  keys, as its loader finds it, so no loader DLL ships); the app's
  assets are served from `https://nui.assets/`; pages follow the app's
  dark mode. `Video` is a WebView2 page of a `<video>`, with Edge's
  controls: Win32 has no video view with controls.
- Frames are set with `SetWindowPos`, without copying the old pixels
  (a view's pixels depend on what's behind it). `ui.Animate` steps the
  frames on a timer; opacity between 0 and 1 fades a view's own drawing
  but not its child windows (a child window can't be translucent without
  the app's manifest).
- The host builds for amd64 only: GDI+ takes floats, which Go's Windows
  calls pass in the registers on amd64 but not arm64 (an amd64 build
  runs on Windows on Arm, emulated).
- Debug automation: `NUI_SNAPSHOT=file.png` writes the window (and the
  top sheet's, `file.sheet.png`, and each `Window`'s, `file.window.png`,
  `file.window2.png`…) with `PrintWindow`, and a menu open, `file.menu.png`,
  from the screen, once it has shown, without
  taking focus, then quits (`NUI_DUMP=1`: the view tree and the menu
  bar beside it); `NUI_SCRIPT` runs steps first (`tap:<text>`,
  `type:<text>`, `submit`, `back`, `escape`, `scroll:<y>`,
  `resize:<w>,<h>`, `key:<shortcut>` such as `key:ctrl+shift+k`,
  `key:` also goes to a menu open (`key:down`, `key:d`), `menu:<item
  title>`, `open:<menu title>` (a menu bar's menu, shown below its title;
  `escape` closes it), `close` (the window in use, or the last
  `Window`), `dpi:<dpi>` (that window as if moved to a monitor at that
  DPI: `WM_DPICHANGED`, the title bar Windows draws keeping its own),
  `wait:<duration>`, `snap:<file>`), through the controls'
  own messages and the message loop's key handling;
  `NUI_COLOR_SCHEME` is light or dark whatever the system's.

Desktop needs what mobile doesn't: several windows, the app's menu bar
and keyboard shortcuts, hover and right-click, resizable layouts. To
keep that additive, the core already treats the mobile screen as one
root with an environment (size, insets) that can change at any time,
and never assumes a single root in its own data structures.

**Windows, the menu bar and shortcuts** (`ui/window.go`):

- The main window holds the root (`app.Run`); `app.RunWindow(opts,
  root)` gives it a title and a size and least size
  (`ui.WindowOptions`, sent to the root as `WindowOptions` when it
  mounts). A size the user chose, remembered by the host (macOS, Windows), wins
  over it.

- `ui.Window(WindowProps{Open, Title, Width, Height, MinWidth,
  MinHeight, Content, OnClose})` is a window of its own while `Open` is
  true, built as a `Sheet` is: a `Window` node, a host-sized layout root
  whose `Environment` the host reports (it follows the user resizing
  it; its Scale is the window's, which on desktop can differ from the
  main window's: the core measures and snaps the frames in it at that
  scale), `Present` opens it, `Destroy` closes it, and the user closing it
  sends `Dismissed`, which sets `Open` false. Its content's owner lives
  as long as the window is open. Mobile hosts can't show one (it panics
  there, as any kind a host lacks).
- `ui.MenuBar(menus)` gives the app its menus, with keyboard shortcuts
  (`ui.Shortcut{Key, Mods}`, `ui.Cmd("s").With(ui.Shift)`; `Primary` is
  ⌘ on macOS and Ctrl on Windows). It's a fragment that follows its
  accessor: the core encodes the menus into one `MenuBar` command to
  the root and sends it only when the encoding changed; the host makes
  its menu bar again, and a chosen item (a click or its shortcut) comes
  back as `Action` on the root with the item's index among all the
  menus' items. The last `MenuBar` mounted shows; disposed, the one
  before. Shortcuts are menu items' because that's how both platforms
  route them (key equivalents, accelerators), and how users find them.
  Mobile hosts ignore it.

## References

- Solid: owners, `createRoot`, `mapArray` (`For`), `createStore`.
- alien-signals: the propagation algorithm to port.
- Taffy: CSS block, flexbox and grid in Rust; its Chrome-generated
  fixtures define our layout tests.
- The CSS specs: Flexible Box Layout, Grid Layout, CSS 2 visual
  formatting.
- React Native's Fabric: mount items, view flattening, synchronous
  layout; react-navigation's native stack for `Stack`'s behavior.
- Flutter: merging the UI and platform threads for synchronous interop.
- MyGo (`github.com/egoist/mygo`): purego and Objective-C patterns,
  main-thread loop, incremental layout, CLI packaging and signing.

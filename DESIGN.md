# Asuka — design

Asuka (working name) is a UI framework in Go that renders with each
platform's own widgets. **Mobile comes first: iOS and Android.** Desktop
(macOS, Windows, Linux) comes after mobile's v0 is done, on the same core
(see "Desktop, later"). Apps are written in Go. The goals, in order:

1. **Fast to build with**: fast compiles, a quick edit-and-run loop on
   the simulator and the emulator, a small API.
2. **Best performance**: an update touches only the native widgets it
   changes, and crosses into native code once per batch.

Non-goals: drawing widgets ourselves (that is MyGo's native UI; the one
planned exception is rich text, see "Rich text"), web views (an app in a phone's web view; the browser is a platform of its own, see "The web"), pixel-identical looks across platforms, stability across a
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
| Threads | Core runs on the platform's main thread | Native callbacks that need answers synchronously (list data sources, text field delegates, measurement, layout inside the keyboard's animation) call straight into the core. Frameworks that run logic and layout off the main thread (React Native, Texture, Litho) do it so slow or garbage-collected app logic can't drop frames, and pay with asynchronous interop: blank cells, input races, layout a frame late. Fabric added synchronous layout and Flutter merged its UI and platform threads (3.29) for that reason; SwiftUI and Compose stay on main. Asuka's logic is compiled Go with fine-grained updates, so the main thread suffices; slow work runs on goroutines that post results back. The place off-main work does pay, preparing long, rich or streaming text, is handled without moving the core (see "Rich text"). |
| Core ↔ native contract | Batched mount operations (`ops`) | One crossing per batch. The same stream works in process and over a socket. |
| Layout | Our own engine in Go: CSS block, flexbox and grid (Taffy-like, checked against Chrome), measured by the host | One layout engine for every platform, with the semantics web and React Native developers know; native widgets report their own sizes. Designed for our tree and for zero allocation, not ported from Yoga or Taffy. |
| iOS host | Swift + UIKit, with Go linked as a `c-archive` | UIKit subclasses and delegates are trivial in Swift. Not SwiftUI: it is a second reconciler. |
| Android host | Kotlin + Android Views (Material Components 3), with Go loaded as a `c-shared` library | Views are imperative targets for mount ops; JNI is crossed once per batch. Not Compose: it is a second reconciler, it measures only inside its own layout pass (the core measures synchronously during a flush), and it applies changes on the next frame. Views aren't deprecated, but new Material and Jetpack UI (Material 3 Expressive, adaptive scaffolds) often lands in Compose first or only. A component that exists only in Compose is mounted as a `ComposeView` for that one node, driven by the node's props and measured with `ComposeView.measure`; the core and the ops don't change. Not in list rows: each `ComposeView` is its own composition. Revisit only if Material Components is deprecated. |
| Text and controls | The platform's own views (`UILabel`, `TextView`, ...), measured by the real widget on the main thread | Native behavior for free: accessibility, Dynamic Type and font scale, selection, truncation, line breaking. What's measured is what's drawn. The alternative, laying text out off the main thread with CoreText / `StaticLayout` and drawing it in a custom view (as frameworks whose core runs off the main thread do), would re-implement all of that. The cost: text is measured on the main thread, and each widget's measuring quirks are ours. Mitigations: per-node layout caches, and a shared measure cache keyed by content (below). If profiling ever shows main-thread text measuring hurting (List cells), measure with CoreText / `StaticLayout` while still drawing with the platform view. |
| Web | The DOM, from a JavaScript host (`platform/web`) the app's Go code drives as WebAssembly (`host/web`) | As the mobile hosts: the core hands over each batch encoded, decoded in place out of the WebAssembly memory by a generated decoder, one crossing per batch; driving the DOM from Go through `syscall/js` would cross per property. Each view is an element at the frame the core laid out, so layout is Asuka's, as on every platform; leaves are measured by the real elements. Not a canvas: the browser's text, controls, focus, autofill and accessibility come with its elements. |
| cgo | Allowed only in `mobile/` (build tags `ios`, `android`) | Go's `c-archive` and `c-shared` build modes need it. Everything else builds with `CGO_ENABLED=0`, so desktop hosts can stay cgo-free later. |

Open decisions, with the default to use until they are settled:

- Minimum versions: **iOS 16, Android API 33 (13)**: Android 13 brought RuntimeShader (ui.Shader) and per-app languages, and what came before it is a small share of devices.
- Android widgets: Material Components 3 (decided in M2), with the
  Material 3 Expressive theme and styles (Material Components 1.14).
- Module path: `github.com/egoist/asuka`.

## Architecture

```
app code (Go)            components, signals
─────────────────────────────────────────────────────────────
signal/                  Signal, Memo, Effect, owners, batching
ui/                      node tree, components, flush scheduler
layout/                  flexbox over the node tree
ops/                     mount operations, binary encoding
─────────────────────────────────────────────────────────────  Host interface
mobile/ (C ABI, cgo) ──► platform/ios (Swift)   platform/android (Kotlin)
host/fake                                  tests: records ops
host/darwin                                macOS: AppKit in process, Go (purego)
host/windows                               Windows: Win32 in process, Go (syscall)
host/web (js/wasm) ──► platform/web (JavaScript)   the browser: the DOM
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
func BindThread()                   // asuka_debug: the main thread
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
  Builds with the `asuka_debug` tag check this and panic.
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
something else. Instead, `asuka vet` (package `vet`, analyzers on
`golang.org/x/tools/go/analysis`, which `asuka dev` and `asuka build` run
as warnings) reports a signal read in a component's body outside any
effect, which runs once and never updates (Solid's most common bug,
too), when what it reads goes into the tree; with shortcuts for the
common reactive cases planned. A JSX-like syntax, if ever, would be a
separate file type that generates Go (as templ does), once the
component API has settled.

**`asuka vet`** reports only what's certain from the code, as the
compiler would; a check that cries wolf gets turned off. Its analyzers:

- `reads`: what's built is a function returning a `Node` (or `[]Node`)
  and the func literals given to what runs them once, untracked: `Show`'s
  branches, `For`'s and `List`'s rows, a screen, a tab's or a sheet's
  `Content`. Not `Dynamic`'s, which builds again when what it read
  changes, nor the components it runs. A read is a `Get` of a signal, a
  memo or an accessor, a call that says the app's language (`ui.Locale`,
  a catalog's messages, and what calls them, in any package: a fact of
  the analysis), and a call of the component's own closure that reads.
  It's reported when its value goes into the tree (a node's argument,
  `ui.Val`, a `Style`, an `if` or a `range`, what's returned), followed
  through expressions and local variables, so a read for an action, an
  event handler's capture or a new signal's first value isn't. Not
  followed: a function that reads a signal, whose callers often use what
  can't change (a record's ID); a field that can't change (the
  environment's `Platform`); a `For` row's item that can't (a pointer,
  or its own key).
- `mainthread`: a `Set` or `Update` run by a `go` statement (directly,
  through what runs right there, as `Batch`, or through the package's
  functions that write), not inside `ui.Post`.
- `memowrites`: a write inside a memo's function, which panics.

`asuka` is its own `-vettool`: run by `go vet` (`-V=full`, `-flags`, a
`.cfg`), it serves the analyzers by `unitchecker`'s protocol, so `asuka
vet` is `go vet -vettool=asuka` with go vet's caching, build flags and
facts across packages; dev and build read its `-json`.

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
a system family. It scales as its text style does. Under `asuka dev` the
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
looks up at start: `@objc(AsukaAppNatives)` conforming to
`AsukaNativeModule` in Swift, `object dev.asuka.app.AsukaAppNatives :
AsukaNativeModule` in Kotlin, calling `registerNative(kind) { ctx -> view }`.
A native view implements `set(name, value)` and sends events with
`ctx.emit(name, values)`; the host measures it as it measures any view.
On the desktop the app's Go registers views and modules with package
`native`. On the Mac its Swift can too: `macos/*.swift` is compiled with
`platform/macos` (the iOS names, an `NSView`) into
`Contents/Frameworks/AsukaAppNatives.dylib`, which `host/darwin` loads
at start through purego and drives through C functions (`@_cdecl`):
make, set (the wire's prop bytes), measure, destroy, call; events and
replies come back through two purego callbacks. What it registers joins
package `native`'s registry, so the rest of the host doesn't know the
difference, and nothing needs cgo.
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

### Interaction states, drag and drop

A view's interaction states (pressed, hovered, dragged, a drag over a
drop target) are reported by the host as events, and the core turns
them into what styles read (`ui/interaction.go`). There are three
layers, each built on the one below:

- **Variants**, for the view's own style: `Style.Pressed`, `Hovered`,
  `Dragging` and `DragOver` (`*Style`). They are CSS's `:active` and
  `:hover`, and Tailwind's `active:` and `hover:`. While the view is in
  a state, its variant is laid over the style. Merging rules:
  - A variant replaces only the fields it sets (non-zero).
  - `Font` is merged field by field.
  - Variants apply in the order Hovered, Pressed, Dragging, DragOver.
  - A View whose style (or `StyleFunc`) has variants wraps it in an
    effect that applies them. A flattened container whose
    `StyleFunc` comes to have variants gets a view of its own.
  - Other components ignore variants.
- **`Interaction`** (`ViewProps.Interaction`), the view's states as
  values. Compose has `InteractionSource` and Flutter has
  `WidgetStatesController` for the same thing.
  - `it.Pressed()`, `Hovered()`, `Dragging()` and `DragOver()` read
    signals.
  - Other views follow the state with `Style.When(it.Hovered, ...)` and
    `Unless`, and anything else can read it in a `StyleFunc`, a `Text`
    or a `Show`. That covers Tailwind's group-hover and peer-hover.
  - Variants use the view's `Interaction` (or one of its own made for
    them).
  - Given to several views, a state is true while any of them is in it.
    Each view's changes are counted, and a disposed view is taken out.
- **Handlers**, for side effects: `OnPressedChange`, `OnHoverChange`,
  a `DropTarget`'s `OnOverChange` and a `DragSource`'s `OnPhase`.

A view listens for a state only from when something first reads it,
whether a variant or a read of its `Interaction`.

- A host may get the `Listen` long after `Create`, and every host
  handles that.
- A view that nothing asks for its pressed state doesn't report it, and
  keeps the platform's highlight.
- This is exact, not an approximation: a read that never happened can't
  have changed any output.

`When` takes any `func() bool`, so styles follow ordinary signals the
same way. Because a zero field means unset, a state can't clear a field.
`Unless` sets the field in the other state instead.

`ViewProps.Drag` (a `DragSource`) and `Drop` (a `DropTarget`) set up drag
and drop. The core reports each state change once, and keeps the states
last reported. A view disposed while in a state reports leaving it, to
its handlers and its `Interaction`, so what follows it elsewhere doesn't
stay stuck.

The wire (ops/schema.go): events `Pressed` and `Hover` (bool),
`DragOver` (a gesture payload: Began, Changed, Cancelled, and X, Y),
`Drop` (bytes: x, y, the source view, flags, items) and `Dragging` (a
`DragPhase`); `View` props `DropAccept` (MIME patterns) and `DragItems`
(the items, each with its kind, text, path, name, MIME type and whether
it carries an app's value). `ops/item.go` has the codecs and
`ops.MatchMIME`, which the Go hosts share. What every host does:

- **Pressed** (a `View` listening for `Pressed`). True as the primary
  pointer goes down on the view (a touch, the mouse's primary button);
  only the innermost: a touch on a descendant that is pressable itself
  (`Press`, `LongPress`, `Pressed`, a tap menu) or a control (a button,
  a switch, a field) doesn't press its ancestors, as UIKit's cells and
  Android's clickable views behave. In a scrolling ancestor it comes
  after the platform's tap delay (UIScrollView's 150 ms, Android's tap
  timeout), so a scroll doesn't flash it; a tap released sooner reports
  true, then false a moment later (Android's pressed-state duration,
  about 64 ms; iOS likewise), so the look shows. False as it lifts;
  as it moves off the view (back on, while still down, true again;
  iOS keeps a 20-point margin, as UIControl's tracking does, Android
  its touch slop); as a
  scroll, another gesture (a pan, a swipe), a long press or a drag
  takes the touch (the press then doesn't fire); as the app goes to the
  background or the view leaves the window. `Press` fires only for a
  release while pressed, so the two agree. Only the first touch counts.
  A view with `Pressed` and no `Press` (a hold to record) tracks presses
  without becoming a button for screen readers. Windows: Space held on
  a focused pressable view presses it, its release fires `Press`.
- **Hover** (a `View` listening for `Hover`). True as a pointer (a
  mouse, a trackpad, an iPad's pointer, a hovering pencil or stylus)
  comes inside the view's bounds; it stays true over its descendants,
  as CSS's `:hover` does, and goes false as the pointer leaves the
  bounds, the window or the app, or the view is hidden or removed.
  iOS: `UIHoverGestureRecognizer`, recognizing with the others. Android:
  the container's `dispatchHoverEvent`, which sees the pointer over its
  children too (its `onHoverEvent` gets an exit as a hoverable child
  takes the pointer). macOS: an `NSTrackingArea` (entered and exited,
  in the visible rect, active in the active app), checked against the
  mouse as it's added, so a view that appears under the pointer is
  hovered. Windows: tracked from the message loop: each mouse move finds
  the views under the cursor, the window it's over and its ancestors,
  and `TrackMouseEvent`'s leave clears them as it leaves the app's
  windows.
- A view that reports its pressed or hover state draws its own: the
  host leaves off its own highlight (Android's ripple foreground,
  Windows' hover and pressed fills; the focus ring stays).
- **Drop targets** (`DropAccept`). A drag is taken if any of its items'
  MIME types matches a pattern (`ops.MatchMIME`: exact, `type/*`, `*/*`
  or `*`, without regard to case or parameters). An item's type: a text
  `text/plain`, a link `text/uri-list`, a file's from its UTI or
  extension (unknown: `application/octet-stream`), image data its
  image type, an item dragged from the app its `DragItems` MIME type
  (the core fills it in). Only the innermost target under the pointer
  that takes a drag gets it. `DragOver` Began as a taken drag comes
  over the view, Changed as it moves (its place in the view, in
  points), Cancelled as it leaves, ends or is dropped. The platform
  shows a move for the app's own drags, a copy for other apps'. A drop
  from the app sends `Drop` at once, with its source view and no items
  (the core has them, Values and all, as they were when the drag
  began). Another app's drop loads the items the target takes, and
  only those, then sends `Drop` with them: texts, links (as
  `ShareKindLink`), files; on iOS and Android the files (and image data)
  are copied into the app's caches, as a share's are (flag 1: the app
  owns them), on macOS and Windows they're the files themselves; a
  macOS file promise is received into the caches.
- **Drag sources** (`DragItems`). Touch: a long press lifts the view
  (`UIDragInteraction`, enabled on iPhone too; Android's long click
  starting `startDragAndDrop`), and the long press then fires no
  `LongPress`. Mouse: a move with the primary button down past the
  system's drag distance, which cancels the press (`Pressed` false, no
  `Press`) and comes before a pan. The drag shows the view's image.
  Other apps get each item's text (plain text), link (a URL) or file
  (iOS: the file's provider; Android: a `FileProvider` URI with global
  read; macOS: a file URL; Windows: `CF_HDROP`); an item that's only a
  value goes nowhere else, and a drag of only values is kept to the
  app. The session carries its source view's ID in a private type (iOS's
  local context, Android's local state, a private pasteboard type or
  clipboard format with the process's ID on desktop), which a drop in
  the same process sends as its source. `Dragging` Began as it lifts,
  then exactly one of Dropped (an operation other than none) or
  Cancelled. Windows lets a drag that carries files copy or link only
  (never move them, Explorer included), so the app's own targets show
  a copy for those; its drops take virtual files too (Outlook's
  attachments, a browser's images), copied into the app's folder.
- With presses the view's own, iOS's `Press` follows UIControl (a
  release within the margin, not a tap recognizer's 10 points), and a
  macOS view's tap menu opens on a click's release, as on Windows.

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
  through empty boxes and with the parent's edges), each in either
  `direction`. Not yet: writing modes, floats, named grid lines and areas, sizing keywords
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
  either direction), run as table tests, with what they don't cover in
  `rtl_chrome.xml` and `ltr_chrome.xml`, each rendered by Chrome; `known_failures.txt` lists the ones
  that don't match yet (mostly the unsupported features above).

## ops

The only contract between core and hosts. Its schema is written once in
Go (`ops/schema.go`); `go generate ./ops` writes the Go constants, the
Swift decoder (`platform/ios`), the Kotlin decoder (`platform/android`)
and the JavaScript decoder (`platform/web/src/ops.js`), each with event
encoders.
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
- `ListProps.Appearance: ui.ListInsetGrouped` is each platform's grouped
  list: UIKit's inset grouped list; Material 3 Expressive's segmented
  list on Android (each row its own container, 2dp apart, the group's
  ends rounded, on the surface container); a System Settings group on
  macOS; Windows 11's settings cards. The core owns the inset, from the
  platform's margins (iOS's layout margins, 16 or 20; Material's window
  margins, 16 or 24; 20 on macOS, 16 on Windows) and the width class,
  lays the rows out at the list's width less it on each side, and sends
  it (`Inset`); the hosts inset the rows by exactly that, not by their
  own margins, so what's measured is what's shown.
- Changes to `items` send `Command(list, listUpdate, item IDs)`: a
  stable int64 per key, in order. iOS applies them as an
  `NSDiffableDataSourceSnapshot` (once the call into Go has returned, so
  binding never re-enters a flush); Android diffs them with `DiffUtil`
  (likewise after the call returns) and dispatches item ranges and moves
  to the RecyclerView adapter. Bound rows whose item changed are updated
  in place by the core.
- Cells are views the host makes; their IDs have the high bit set. A
  cell's `SetFrame` carries the row's height.
- Sections (`ListProps.Section`) follow the item IDs in the same
  `ListUpdate`: each section's first item and an ID of its own (stable
  while its key lasts, as an item's). A section's header is a cell too,
  bound with `ListBind`'s index `-1 - section`, so no new entry point
  crosses the C ABI or the dev socket. A plain list's header sticks at
  the top: a pinned supplementary view on iOS (iOS 26's scroll edge
  effect goes behind the pinned one only:
  `UIScrollEdgeElementContainerInteraction`, whose region takes
  touches); on Android, which has no sticky headers, a cell of its own
  over the RecyclerView, in a frame that is the list's view.
- The anchor (`AnchorBottom`, a chat's) and keeping what's on screen in
  place as rows are added above are the host's, which knows where rows
  are: iOS remembers the first visible row and its offset before a
  snapshot applies and puts it back after, and pins the end in
  `layoutSubviews` while the user hasn't scrolled off it; Android's
  `LinearLayoutManager` keeps its anchor row itself (`stackFromEnd` for
  the bottom); the desktop hosts, which tile their own rows, keep the
  row at the top as rows bind and change height. `OnStartReached` is the
  core's, from the rows it's asked to bind.
- Selecting rows: the core sends `Selecting` and each cell's `Selected`;
  the host shows its control, reports the user's choices
  (`RowSelected`), and the room the control takes from the rows
  (`RowInset`), at which the core lays them out again.

### Text input

`ui.TextField(ui.TextFieldProps{Value: text})` where `text` is a
`*signal.Signal[string]`. The widget owns its text. Its edits write the
signal; the core remembers the last value the native side reported and
sends `SetProp` only when the signal differs from it, so typing never
round-trips and IME composition is never disturbed. Props for the
keyboard: type (text, number, email, URL, phone), return key, secure
entry, autocapitalization, autocorrection, content type (for AutoFill).

The selection (`TextFieldProps.Selection`) is kept the same way: the
host reports its moves (`SelectionChanged`, after the text change that
caused them) and the core sends one only when the signal differs from
what was reported. The wire counts UTF-16 units, as UIKit and Android
do; the core converts to byte offsets of the Go string. `InsertText`
replaces the selection through the platform's own text input (UITextInput's
`insertText`, `Editable.replace`, the field editor's `insertText:`,
`EM_REPLACESEL`), so it's undoable and reported as typing. Content of
`PasteTypes` that's pasted, put in by a keyboard or dropped goes to the
app as files (`Pasted`, its items as `Shared`'s) instead of into the
text.

A Rich field's styled text (`TextFieldProps.Rich`) goes to the host in
RichText's Content encoding and comes back in the same, canonically
(styles in the order runs first use them, runs of one style merged), so
the core tells the user's edit from what it sent, as with Value. Each
host keeps a run's style as it was sent beside the platform's
attributes (an attribute of its own, a span; Windows's rich edit has
neither, and goes through RTF, `internal/rtf`), so what the user didn't
change comes back as it was: bold, italic, underline, strikethrough,
monospace and links are read from what the text has. Formatting is
undoable with the typing around it: UIKit's, AppKit's and the rich
edit's undo cover it; Android's covers text only (and puts deleted text
back unstyled), so an Android rich field keeps its own history, the
text with its spans and the selection before each change, typing
grouped as editors do.

A measured view's padding and border are inside its frame
(`ContentInset`, sent when it isn't zero): the host draws its content
inside them (a label's text rect, a field's text rects, a text view's
container inset, view padding on Android) and measures its content
alone.

### Lifecycle and state

`app.OnLifecycle(func(app.State))` reports foreground and background.
On Android the Go core and the views live in the process, not the
Activity: views are made with the app's Material theme
(`ContextThemeWrapper` over the application). The Activity handles
rotation, size, dark mode, font scale and density itself
(`configChanges`); any other configuration change remakes it, and the
new Activity takes over the existing tree: `asuka_attach` resends the
root (`ui.Reattach`) and every frame, with no app state lost. Android
views resolve theme colors and text sizes when they're made, so after a
dark mode, font scale or density change the host makes its text and
Material views again, replaying the props it was sent and keeping the
user's state (text, selection, checked, value). There's one Activity
(`singleTask`), and whichever instance comes to the front takes the
views. Process death loses state, as in any native app; saving state to
the device for that is later, under a name that says storage (say
`signal.Saved(key, initial)` over `UserDefaults` / `SharedPreferences`),
not to be confused with keeping state across `asuka dev` reloads (below),
which needs no code.

The core can also start without a screen: when the system runs a
background task (`asuka/periodic`: BGTaskScheduler, WorkManager) with the
app closed, the host starts the core headless (`startHeadless`): `main`
runs, the root component mounts into views no window holds yet, and the
task's Go code runs. A screen that comes later (the user opens the app)
takes that root as a recreated Activity does (`asuka_attach` on Android;
the root controller mounts the existing root view on iOS).

Packages that need a framework or library of their own cost only the
apps that use them. On iOS, the host's Swift for them is compiled under a
flag (`ASUKA_MAPS`...) set when the app's Go archive has the package's
symbol (`iosFeatures`). On Android, each is an optional Gradle module
(`asuka-maps`, `asuka-push`...) built in for the apps that import its
package (`featurePermissions`), whose `AsukaPlugin` registers its methods
and views by name at start.

What runs outside the app's process is built from `asuka.json`. iOS's
widget extension is WidgetKit's, so SwiftUI's (the one place the host
uses it): one template (platform/ios/Widget) with the app's kinds,
reading the entries the app's Go code sets (`asuka/widget`) from the App
Group they share; it runs no Go. The shortcuts are App Intents the CLI
generates and links into the executable, their metadata extracted as
Xcode does (swiftc's constant values, appintentsmetadataprocessor); each
opens the app with its URL. On Android, the CLI generates an App Widget
provider for each kind (over the host's `AsukaWidgetProvider`, with
RemoteViews) and the launcher's static shortcuts.

Two extensions run the app's Go code, from the app's own symbols rather
than `asuka.json`: the share sheet (`app.ShareSheet`) and the
Notification Service Extension (`push.Rewrite`). With either, the app's
Go code and the host are one dynamic framework (AsukaApp.framework) that
the app and the extension link, their data in the App Group they share.
The extension starts the core headless with a root of its own
(`ASUKA_SHARE_SHEET`, `ASUKA_NOTIFICATION_SERVICE`). Android runs both in
the app's package: the share sheet's activity, and the push rewrite in
Firebase's service, starting the core headless as a background task does.
Notifications' responses work the same way: a background action starts
the core headless (iOS's center delegate, Android's broadcast receiver)
and hands Go the response, keeping the process until Go reports it done.

## mobile

`mobile/` (build tags `ios || android`, cgo) exports a C ABI:

```c
void      asuka_start(asuka_host_callbacks*);   // apply, measure, post; calls main
void      asuka_attach(void);                 // mount the tree again (new Activity)
void      asuka_event(const uint8_t*, size_t);
int64_t   asuka_list_count(uint32_t list);
void      asuka_list_bind(uint32_t list, int64_t index, uint32_t cell);
void      asuka_list_unbind(uint32_t list, uint32_t cell);
void      asuka_run_posted(void);             // the host's main-thread hop
```

All calls come on the main thread. `post` from Go calls the host's
callback, which hops with `DispatchQueue.main.async` / `Handler.post`
and calls `asuka_run_posted`. The header Swift and Kotlin compile against
is `platform/ios/Sources/CAsuka/include/asuka.h`.

An app is a `main` package whose `main` calls `app.Run(root)`. The CLI
adds a file to it with `go build -overlay` that imports `mobile` and
sets `mobile.Main = main`; `asuka_start` calls it, and `app.Run` mounts the
root on the mobile host.

- iOS: `GOOS=ios GOARCH=arm64 CGO_ENABLED=1 go build
  -buildmode=c-archive`, with the SDK's clang (iphonesimulator for the
  simulator, arm64 and x86_64; iphoneos and an XCFramework for devices
  in M6). `platform/ios` is a Swift package with the decoder (`AsukaOps`),
  the UIKit host (`AsukaHost`) and the C header (`CAsuka`); `App/` holds the
  `@main` app delegate. The CLI embeds them.
- Android: `GOOS=android CGO_ENABLED=1 go build -buildmode=c-shared` per
  ABI (arm64-v8a, x86_64; armeabi-v7a optional) with the NDK's clang,
  linked with 16 KB pages (`-z max-page-size=16384`) as new devices
  require. `mobile/jni_android.c` is the JNI side of the same C ABI:
  Kotlin's `dev.asuka.AsukaNative` calls in; the core calls
  `AsukaRuntime.apply(ByteBuffer)` (a direct buffer over the encoded
  batch, one call per batch), `measure` and `post` back.
  `platform/android` is a Gradle project: the `asuka` library (decoder,
  Views host, JNI bindings) and `app`, the shell app the CLI builds per
  app (application ID and name passed as Gradle properties).

The CLI does these builds itself (no gomobile).

## CLI (`cmd/asuka`)

```
asuka new <dir>                             a project from the template
asuka dev --platform ios|android <dir>       build, run, rebuild and restart on save
asuka build --platform ios|android <dir>     a release build for the stores
asuka publish --platform ios|android <dir>   the release build, uploaded: TestFlight, App Review; a Play track
asuka dev --platform mac <dir>               build and run on this Mac, rerun on save
asuka build --platform mac <dir>             a universal .app, signed (--sign; default ad hoc), its profile embedded (--profile); --appstore: a .pkg
asuka dev --platform windows <dir>           build and run on this PC, rerun on save
asuka build --platform windows <dir>         a folder: the .exe (icon, manifest, version; signed with ASUKA_WINDOWS_CERT) and its assets (--arch arm64)
asuka dev --platform web <dir>               serve it to the browser, rebuild and reload on save
asuka build --platform web <dir>             a static site: WebAssembly, the web host, a manifest
asuka dev|build <dir>                       no --platform: this computer's, on macOS (mac) and Windows (windows)
asuka generate <dir>                        the typed functions of the app's catalogs
asuka i18n init|add|check                   start, add languages to, and check the catalogs
asuka vet [packages]                        what builds but runs wrong: a signal read once, written off the main thread
```

**An app's metadata** is `asuka.json` beside its Go code, all optional:
name, ID (bundle ID and application ID), version and build number, a
1024×1024 icon, orientations (the phone's: an iPad or a large Android
screen turns the app any way, for split screen), the iOS team (and
whether it's iPhone only), provisioning profile, usage
descriptions and extra Info.plist keys, the Android permissions and
upload key. `asuka dev` and `asuka build` read it alike.

**The accent color** (`"accent": "#7c3aed"`) is the one color an app's
brand sets on native controls; the controls are the platform's, so
Asuka has no theme of its own over them (`Style` changes one view). It
travels with the Go code, not the shell: the CLI builds it in
(`-ldflags -X`, `internal/boot`), and each root, as it mounts, sends
the `Accent` command before any view is made. That way the dev player,
which runs any app, takes each one's, and drops it between apps. On iOS
it's the windows' tint color, which UIKit's controls and `ui.Accent`
take. On Android it's the seed of a Material 3 color scheme
(`SchemeContent`, light and dark, as Material's `DynamicColors` makes
it) on Android 12 and later; before that, Material's baseline colors,
with Asuka's own uses of the accent (links, `ui.Accent`) taking the color
itself. `"android": {"dynamicColor": true}` takes the wallpaper's colors
instead, where the system has them. The theme context is made again
and the views that resolved theme colors are rebuilt, as on a dark mode
change.

**`asuka build --platform ios`** makes an `.ipa`, still without an Xcode project:
the host compiled optimized (no debug automation), the Go archive for
`arm64` devices, linked with dead stripping; an Info.plist with the
keys App Store validation checks (`DT*` from the SDK and Xcode,
`ITSAppUsesNonExemptEncryption`); the icon as an asset catalog compiled
by `actool`; then signing as Xcode would: the installed provisioning
profile that fits the app ID best (exact before prefix before `*`) for
the distribution (`app-store` by default, `ad-hoc`, `development`),
unexpired, whose certificate's identity is in the keychain; its
entitlements, with a wildcard app ID made the app's; `codesign` with
the identity's SHA-1, verified. `--simulator` makes the same release build
for the simulator.

**`asuka build --platform android`** makes an `.aab` for Google Play (no
`.apk`: Play makes each device's from it, with only that device's ABI):
the Go code as a stripped `.so` for arm64-v8a, armeabi-v7a and x86_64,
the assets, launcher icons (legacy per density, and adaptive: the icon
inside the safe zone over its corner color), the permissions, the
version; Kotlin shrunk by R8 (the classes JNI and reflection reach are
kept); signed with the upload key named in `asuka.json` or `ASUKA_KEYSTORE`,
its passwords from the environment (never on a command line). A
release build never loads a library from the app's files, as debug
builds do in development; cleartext HTTP (the player's assets) is
debug-only, as is iOS's local networking exception.

**`asuka publish --platform ios`** and **`--platform android`** make the release build and
upload it, through the stores' APIs from Go (no altool, no Gradle
plugin), so a release is one command, in CI too. Everything the store
can refuse is asked first, before the build: the app (App Store Connect
by bundle ID; Play by package, in an edit opened to look and deleted),
whether the version still takes builds (approved or released: no), the
TestFlight groups by name, and the store's highest build number. The
build is numbered one above it (and at least `asuka.json`'s `build`), so
`build` needn't be raised by hand; `--build` sets one. iOS: a JWT signed
with the App Store Connect API key (ES256, renewed every 10 minutes);
the `.ipa` sent as `buildUploads` says, in parts, a few at a time, each
retried; then the upload and the build polled until Apple has processed
it (its warnings and errors printed), What to Test set, the build given
to the groups (an internal group with every build skipped; an external
one sends it to beta review), and with `--review` the App Store version
made or found, given the build, its What's New (each language's, which
an update needs) and submitted. Android: an OAuth token from the service
account's signed assertion (RS256); the `.aab` in a resumable upload,
chunk by chunk, resumed from where Play says it got to; a release on the
track (`completed`, `inProgress` with `--rollout`, or a draft, which an
app never published takes only); the edit committed without cancelling
changes in review unless asked, and, if Play holds changes for the
Console, committed unsent. Requests are retried when rate limited
(`Retry-After`) and, where safe to repeat, when the server or the
network fails.

**`asuka dev` runs the app's Go code on the Mac, and its views in a
player** (`internal/remote`): the platform host with a small Go library
(`mobile/player`) in place of an app, built and installed once and
shared by every app. A change rebuilds the Go code for the Mac and
restarts that process, under a second; the player reconnects and shows
the new code, with no install. The app's logs are the process's output.

- **Only in that process**: `asuka dev` links `app/remote` into it (an
  `-overlay` file in the app's main package), and with it the socket
  and the assets' HTTP server. An app's own builds (on the device, for
  release, on a desktop) carry none of that code, which is half of a
  small app's binary. The desktop hosts keep state across reloads with
  `internal/devstate`, which needs no network.

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
  the live signals of that type there. A signal made by Asuka itself
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
  which only an app built with it can show. Such apps, `--once` and
  `--embed` build the app with its Go code, as before: `asuka dev`
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
asuka/
  signal/            reactivity
  ui/                components, nodes, flush, flattening, navigation
  layout/            flexbox
  ops/               schema, Go encoder; generate writes Swift/Kotlin decoders
  i18n/              languages, catalogs, ICU messages, CLDR's formats
  vet/               asuka vet's analyzers
  host/              Host interface, Environment
    fake/            records ops, measures by fixed rules; for tests
  mobile/            C ABI (cgo, ios||android)
  platform/ios/      Swift package
  platform/android/  Kotlin/Gradle library
  app/               app.Root, lifecycle
    remote/          what asuka dev links in to serve a player
  cmd/asuka/           CLI
  template/          what `asuka new` copies
  examples/counter/  examples/todo/  examples/list10k/  examples/navigation/
  conformance/       what every host draws for a Style, measured on its pixels
```

The web adds `host/web` (the Go side, `js && wasm`) and `platform/web`
(the JavaScript host: `src/`, with the generated `ops.js` and
`symbols.js`, `gen_symbols.go`, and the decoder's tests).

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
- `ops`: the Go encoder and the generated Swift, Kotlin and JavaScript
  decoders round-trip the same golden batches (`swift test --package-path
  platform/ios`; `./gradlew :asuka:testDebugUnitTest` in `platform/android`;
  `node --test platform/web`).
- Hosts: GUI tests (`e2e/`, build tag `e2e`, in CI on a simulator and
  an emulator) build `examples/` with `asuka dev --once` and drive them
  through the native views. Debug builds take automation commands (tap,
  type, submit, back, tab, rotate, dump: what the screen shows), on iOS from a file in the
  app's container on the `dev.asuka.automation` Darwin notification
  (`platform/ios/Sources/AsukaHost/Automation.swift`), on Android from a
  `dev.asuka.automation` broadcast (`Automation.kt`; the dump read with
  `run-as`). So both platforms run the same test, and neither needs
  UI Automator's single connection, which device tools hold. Each test
  waits for the screen to show what it expects, bounded so a broken app
  fails.
- Rendering (`conformance`): each host draws one sheet of cases
  (a background's edges, rounded corners, borders, opacity and group
  opacity, clips, shadows, gradients, transforms) between two
  registration markers; its runner captures the screen, and the cases
  measure the pixels: where an edge is to the device pixel, a shadow's
  Gaussian σ, a color's mix. They assert measurements with tolerances,
  not pixels against a reference, so one set holds for every host at
  any scale, as Graphite's backend conformance suite does. Runners:
  `host/darwin` (Core Animation's own renderer, `CARenderer`, into a
  Metal texture: NSView's `cacheDisplayInRect` leaves out transforms and
  shadows), `host/windows` (`PrintWindow`), and `e2e` on the simulator,
  the emulator and headless Chrome at 1×, 1.5× and 2× (the DevTools
  protocol's screenshots). A host that documents lacking a feature
  (Android's elevation shadows) skips its cases, with the reason.
- Tests never pass or fail on wall-clock time: wait for the condition,
  count the work.

## Milestones

Each one ends with its examples running and its tests in CI.

- **M0, core without UI** (done): `signal`, `ui` with `host/fake`,
  `layout`, `ops` with the Go encoder. Everything above under `go test`.
- **M1, iOS** (done): Swift codegen for `ops`, `mobile/`, `platform/ios`,
  `asuka new`, `asuka dev --platform ios` on the simulator. Counter, todo (`For`,
  `TextField`, `Toggle`, `SafeArea` with the keyboard) and list10k
  (`List`) run. Not yet: XCUITest GUI tests and CI on macOS runners.
- **M2, Android** (done): Kotlin codegen, `platform/android`,
  `asuka dev --platform android` on the emulator, Activity recreation
  (`asuka_attach`). The same examples run unchanged; live rotation, dark
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
- **M6, shipping** (done): `asuka.json`, `asuka build --platform ios` (signed `.ipa`;
  `--simulator`), `asuka build --platform android` (`.aab`), `asuka
  publish` (the build uploaded through the stores' APIs: TestFlight's
  groups and beta review, App Review; a Play track, staged). Not yet:
  screenshots and store listings (App Store Connect and the Play Console
  do those).
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
    `platform/android/gen_symbols.go` with TalkBack labels.
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
    `window.asuka.postMessage`, the same on both, and `ui.EvalJS` runs
    script in it.
  - Grids: `ListProps.Columns` and `Gap` (a compositional grid on iOS,
    a GridLayoutManager on Android); the core lays each cell out at
    exactly the column width the host gives it.
  - `asuka/store` (small values kept across launches, `store.Signal`),
    `ui.Copy`, `ui.Share`.
  - The app's accent color (`asuka.json`).
  - The device's time zone as Go's `time.Local`.
  - Motion: `Style.Transform`, shadows, gradients, `Style.Material`
    (blur), repeating animations, enter and exit `Transition`s.
  - `asuka/sqlite` (pure Go, live queries with `sqlite.Watch`), native
    modules (`ui.Call`: `asuka/secure`, the keychain and Keystore;
    `asuka/device`: info, an in-app browser, the photo picker).
  - A search field in the navigation bar (`ScreenOptions.Search`).
  - Incoming links: `asuka.json`'s `scheme` and `links`, `ui.OnOpenURL`.
  - Shares from other apps: `asuka.json`'s `shareTarget` (`true`, or
    `{"types": [...], "max": n}`), `ui.OnShare` (a list of
    `SharedItem`s: texts, links and files). Android takes `ACTION_SEND`
    and `ACTION_SEND_MULTIPLE` in the app's Activity, with intent
    filters for the types, and copies the files to the app's caches off
    the main thread. iOS has a share extension (`platform/ios/Share`, a
    `.appex` in the app's PlugIns, built by the CLI with no Go in it),
    whose activation rule comes from the types: it takes texts, web
    links and files, then opens the app with them in a URL of a scheme
    of the app's (`asukashare.<app ID>`; the items as JSON), through the
    application at the end of its responder chain, as an extension has
    no `UIApplication.shared`. Files go through the App Group
    `group.<app ID>`: the extension copies them there, the app moves
    them to its caches. The host turns that URL into the `Shared` event
    (its items in bytes), so both platforms open the app, as the user
    expects of a "Download" or "Save" target. The extension has its own
    bundle ID (`<app ID>.share`) and, on devices, its own provisioning
    profile, which has the App Group when the app takes files.
  - The app's own share sheet: `app.ShareSheet(screen)`. The sheet is
    a root of its own in a process of its own, the share extension's
    model on both: `ui.ShareSheetRoot` shows the screen once the
    `Shared` event comes to its root (`OnShare`'s handlers are the
    root's they were made in), and `ShareClose` (done, cancelled, open
    the app) ends it. The process says it's a sheet's with
    `ASUKA_SHARE_SHEET`; `app.Run` mounts the sheet then. iOS: the host
    and the app's Go code are a dynamic framework,
    `Frameworks/AsukaApp.framework` (Go's runtime starts as it loads);
    the app's executable is a main that starts the host's delegate
    (`AsukaAppDelegate`), the extension's links the framework and starts
    at `NSExtensionMain`, its principal class
    `AsukaShareSheetController`, which collects the items
    (`AsukaShareCollector`, shared with the standalone extension). An
    app without a sheet stays one executable.
    The app's data is in the App Group for both (`store.Dir`, from the
    host through `mobile/shareddir`, a cgo call, since `store.Signal`s
    in package variables read it as packages initialize), moved there
    once; the keychain group is the app's in both. Android:
    `AsukaShareActivity`, in a `:share` process, translucent, a
    `BottomSheetBehavior` sheet; the process ends with the sheet.
    `asuka dev`: a share sheet's player tells the core (`mShareSheet`)
    and its session mounts the sheet; on iOS the player's extension
    finds the address in the App Group. The CLI looks for
    `app.ShareSheet`'s symbol (in the archive on iOS; in the function
    names a release's `.so` keeps on Android).
  - Sharing files: `ui.ShareFiles` (expo-sharing's `shareAsync`), the
    `share` module's `files`: `UIActivityViewController` with an item
    source for the type, anchored to a view on iPad; Android's chooser
    through the FileProvider, answered when the user is back; the Mac's
    sharing service picker.
  - Custom fonts (`Font.Family`).
  - Accessibility: pressable views as buttons, `Decorative`,
    `ui.Announce`, swipe actions for TalkBack, Reduce Motion.
  - Scroll position: `OnScroll` on `Scroll` and `List`, `ui.ScrollTo`
    and `ui.ScrollToEnd`. Android posts its scroll events to the next
    turn of the main loop: a scroll runs while the view draws, and the
    core changing views then stops it.
  - GUI tests on CI (`e2e/`): the counter; notes (a text field,
    SQLite, a List, a relaunch); navigation (a push, a sheet with a
    field, back, tabs); chat (a list anchored at its bottom staying at
    its end as messages come), on both platforms. `ASUKA_DEVICE` picks
    one of several simulators or emulators.
  - Images fetched over http(s) kept on disk across launches (256 MB,
    least recently used first; not a `no-store` response, nor asuka dev's
    assets from this machine), and `ImageProps.Placeholder`.
  - `examples/gallery` shows each; `examples/notes` is SQLite's.

  - Lists for chats and mail: `Anchor` (a chat's, at its bottom),
    `OnStartReached`, rows added above what shows leaving it in place,
    sections with headers that stick, selecting rows (`Selecting`,
    `Selected`, `OnSelect`, `OnSelectBegin`), leading swipe actions;
    desktop rows' swipe actions in their context menus.
  - Text fields: `Selection`, `ui.InsertText`, pasted and keyboard
    content (`PasteTypes`, `OnPaste`); on iOS the keyboard followed
    frame by frame through its interactive dismissal.
  - Rich text fields (`TextFieldProps.Rich`: bold, italic, underline,
    strikethrough, code, links; `ToggleFormat`, `SetLink`, `OnFormats`),
    on every host (Windows through RTF, `internal/rtf`).
  - A text's and a field's padding inside their frames (`ContentInset`).
  - `examples/chat` and `examples/mail`.

  - More mobile APIs: `ui.OnBack` (screens, sheets, popovers and
    the app take the user's going back: `navigationItem.backAction`,
    `isModalInPresentation`, Android's back callbacks, `BackExit`);
    `Environment.ScreenReader` and `ui.AccessibilityFocus`; `ui.Catch`
    (an error boundary: `signal.Catch` owners recover their effects'
    panics, effects outside every Catch run with no recover); tooltips;
    `ScrollProps.Paging` and `ui.PageControl`; `ui.Popover` (a
    content-sized layout root, `fits`); `TextFieldProps.Accessory` (a
    full-width content-sized root: `inputAccessoryView`, and a bar on the
    keyboard on Android); `ui.Canvas` (a display list, re-recorded with no
    allocation) and `asuka/chart` on it; `ui.Shader` (a Kage shader, Go's
    syntax, compiled in Go by package `shader`: Metal on iOS and macOS,
    AGSL on Android, package interp's CPU on Windows, and GLSL kept for
    Linux); `ui.SplitView`
    (`UISplitViewController`, collapsing into a stack on iPhone; Material's
    navigation drawer; `Replace` resets a stack at once); springs with
    `Damping` and `Velocity`, `Delay` and `OnDone`; `ToggleProps.Checkbox`;
    named `ContentType`s; and embedding in an existing app
    (`asuka build --platform ios --xcframework`, `asuka build --platform android --aar`).

  docs/parity.md has what Asuka has against SwiftUI and Expo;
  docs/device-checklist.md, what's left to check on real devices.

  Next:
  - More accessibility: adjustable custom controls, and checking every component with VoiceOver and
    TalkBack on a device.
  - More GUI tests: gestures, alerts and menus, the gallery.
- **Then desktop** (below), on the same core and `ops`: macOS, Windows,
  Linux and the web, each its own host, done.
- **macOS** (done): `host/darwin`, AppKit in the app's process
  through purego (`internal/darwin`, MyGo's Objective-C runtime layer),
  no cgo; `app.Run` runs it on macOS. `asuka dev --platform mac` (an `.app` like a
  release's, its assets read in place, its privacy prompts its own;
  rebuild and rerun on save, signals' state kept) and `asuka build --platform mac` (a universal,
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
  screen to go back from. Swipes, the inline date picker; the WebView's
  blocked requests, downloads and state, `EvalJSResult`, `WebCookies`
  and asuka/js; device's files and clipboard modules; network
  (NWPathMonitor), location (Core Location), speech
  (AVSpeechSynthesizer), audio (AVPlayer), biometric (Touch ID), review;
  matched views (`Match`); pull to refresh (a trackpad's elastic pull
  past the top); `ui.Native` and the app's modules in Go (package
  `native`, below) or Swift (`macos/*.swift`, loaded from the bundle);
  notarization (`asuka build --platform mac --notarize`).
  Every module the packages call answers (below): the camera (its view,
  capture, recording, codes; `Take` a sheet of the host's), audio
  recording, contacts, calendars, the photo library and saving to it,
  scan, vision, transcribe, imageedit, push (APNs), background, periodic
  (`NSBackgroundActivityScheduler`), Sign in with Apple and its button,
  webauth, maps (`MKMapView`), tracking, App Attest, screen capture;
  sensors, widgets and the age range aren't a Mac's ("not available").
  The CLI gives a Mac app its purpose strings and its hardened runtime's
  entitlements for them, and its provisioning profile (`--profile`,
  `ASUKA_MAC_PROFILE`, asuka.json's `mac.profile`, else the best match
  installed for an app that needs one): embedded as
  `Contents/embedded.provisionprofile`, the app signed with its app ID,
  team and what its packages need of the profile's entitlements (push's
  `com.apple.developer.aps-environment`, Sign in with Apple, App Attest,
  detected from the imports as on iOS), with the identity the profile
  allows; without one the build says what won't work. `--appstore`: the
  app sandboxed, signed with a Mac App Store profile, in a `.pkg`
  (`productbuild`, Mac Installer Distribution). Not yet: `ZoomFrom`'s
  zoom (a push slides), beginning a List's selection with a gesture
  (`RowSelectBegin`), a development profile for `asuka dev`.
- **Windows** (done): `host/windows`, Win32 and the common
  controls (version 6) in the app's process through `syscall`
  (`internal/windows`, much of it MyGo's), no cgo, no WinUI; `app.Run`
  runs it on Windows (amd64 and arm64). `asuka dev --platform windows` (rebuild and rerun on
  save, signals' state kept) and `asuka build --platform windows` (a folder of the
  `.exe`, with its icon, manifest and version built in, signed and
  timestamped with `ASUKA_WINDOWS_CERT` (by the first timestamp server
  that answers, `ASUKA_WINDOWS_TIMESTAMP`'s or DigiCert's, Sectigo's,
  GlobalSign's; signtool on Windows, osslsigncode elsewhere), and its
  assets; for amd64, or
  `--arch arm64`; from any system). The examples run (counter, todo, list10k,
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
  drive real windows. Pinch and rotate (touch, the touchpad, Ctrl and
  the wheel), the header's search field, the inline date picker, the
  native modules: every module Asuka's packages call answers (below:
  implemented where Windows has it, an answer of the package's protocol
  saying it hasn't where it hasn't; `TestEveryModuleAnswers` checks);
  `ui.Native` and the app's modules in Go (package `native`); screen
  readers through UI Automation (below); shadows (drawn by the parent);
  enter and exit transitions and matched views (the frame carries them);
  pull to refresh (F5). Rounded clips (a window region, the corners
  antialiased by the parent), opacity between 0 and 1 for a view with
  child windows and rotations (pictures, below), translations and
  scales (the frame, and the scale of what's in it), materials, the
  dark date picker, the share UI (text, links and files); the native
  modules share, network (the Network List Manager), notify (toasts),
  speech (SAPI), audio (MCI) and location (WinRT's Geolocator); native
  arm64 builds; code signing; the camera (Media Foundation's capture
  engine: its view, photos, video, `Take`), audio recording (AAC),
  webauth (WebView2), Windows Hello for biometrics, vision (Windows'
  OCR and face detector), imageedit, the Pictures folder as the photo
  library, background and periodic tasks, screen capture prevention;
  contacts and calendars (a packaged app's) add through the user's app;
  toasts' buttons, replies and background actions (the app's COM
  activator), the taskbar's badge, place names (OpenStreetMap's
  Nominatim), codes in images and the camera's frames (`internal/barcode`,
  in Go), a map view of OpenStreetMap's tiles.
- **Web** (done): `host/web` and `platform/web` (below); `app.Run`
  runs it on `js/wasm`. `asuka dev --platform web` (serve, rebuild and reload on
  save, signals' state kept; the app's output and the page's errors in
  the terminal) and `asuka build --platform web` (a static site). The examples run,
  light and dark, left to right and right to left, at any size;
  GUI tests (`e2e`, `-run Web`) drive them in headless Chrome. Done: the
  v0 components, navigation (the browser's back included), rich text and rich text fields (an editable element; formatting through the browser's editing, so its undo covers it),
  shapes, canvas, shaders (WebGL 2), images, symbols, video,
  gestures, drag and drop, menus, animation, transitions, matched views,
  the app's files kept (IndexedDB, large files in chunks), `asuka/sqlite`
  (SQLite's WebAssembly build, below; live queries), the native modules
  a browser has (`platform/web/src/modules`: the secure store under a
  non-extractable WebCrypto key, device, clipboard, pickers, share,
  preview, print, compose, notifications, Web Push, network, location, sensors, speech, audio, recording, the camera and
  `camera.View`, image editing, codes, WebAuthn for biometrics, web
  auth; the others answer that the web hasn't them), web views (all of
  it for the app's own origin; another site's page loads, and talks
  through `postMessage` if it cooperates), the app's own native views and
  modules (`web/*.js`), several apps on one page; the app's service
  worker (`platform/web/src/sw.js`, which the CLI ships with every web
  app, named by its digest, beside `index.html`): notifications with
  their actions and inline replies that stay and answer after the page
  closes (a tap opens a page, which gets the response as it starts),
  `asuka/push` as Web Push with the app's VAPID key (`web.vapidPublicKey`;
  the subscription is the token; the page in front rewrites and presents
  a push), `asuka/push/webpush` for servers (RFC 8291's encryption and RFC
  8292's VAPID tokens, in Go's standard library). Scheduled
  notifications: browsers have no API to show one at a time with no page
  open (Notification Triggers were abandoned), so they show at their
  time while any page of the app is open (kept in Cache Storage, shown
  once under a Web Lock), else as the next page opens, or when the worker
  wakes first (a push, periodic background sync where Chrome grants it).
- **Linux** (done): `host/linux`, GTK 4 and libadwaita in the app's
  process through purego (`internal/gtk`), no cgo; `app.Run` runs it on
  Linux (amd64 and arm64; GTK 4.14 and libadwaita 1.5 or later). `asuka
  dev --platform linux` (on Linux: rebuild and rerun on save, signals'
  state kept; `ASUKA_LINUX_RUN` runs it on another machine) and `asuka
  build --platform linux` (from any system: an install prefix with the
  binary, its assets, its desktop file, icons and AppStream metainfo, a
  `.tar.gz`, and a Flatpak manifest on GNOME's runtime). The examples
  run; tests in `host/linux` drive real GTK windows under Xvfb, and GUI
  tests (`e2e`, `-run Linux`) run the examples. Done: the v0
  components, navigation (libadwaita's navigation view, view stack and
  switcher, dialogs, alert dialogs, the split view), lists
  (core-virtualized), rich text and rich fields, shapes, canvas,
  shaders (on the CPU), images, symbols (the icon theme's, or Material
  Symbols as symbolic icons), gestures, drag and drop, menus and the
  main menu with its shortcuts, windows, animation, transitions,
  matched views, the app's links (its scheme), `WebView` (WebKitGTK
  6.0), `Video` (GStreamer), `ui.Native` and the app's modules in Go,
  and every native module the packages call, through the desktop
  portals where there's one: the secure store (libsecret), pickers,
  notifications, network, location, speech (Speech Dispatcher), audio,
  recording, the camera (the camera portal, PipeWire), print, compose,
  preview, library, webauth (WebKitGTK), image editing, codes (ZBar)
  and text (Tesseract) in images, maps (libshumate), sensors
  (iio-sensor-proxy), background and periodic tasks, contacts and
  calendars (Evolution Data Server), notifications due later (systemd
  user timers, shown whether the app runs or not); the others answer
  that Linux hasn't them. One selection across a `Selectable`'s texts
  is the host's (GTK selects in one label at a time). An app's own
  accessible actions are AT-SPI actions as GTK allows (its bridge lists
  a widget's action groups: "action.Mark-as-read"), and the context
  menu; GTK (to 4.22) has no API for named accessible actions.

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
  on iOS, `StaticLayout` on Android) and draws in a view of Asuka's own
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
read the texts and links but don't select across them).

## Languages

An app says its messages in the language of the people who use it, and is
laid out the way that language reads. docs/i18n.md is the guide; this is how
it's built. Package `i18n` and the catalog generator (`internal/i18ngen`) are
MyGo's, whose data comes from a pinned release of CLDR.

- **Catalogs** are ARB files of ICU MessageFormat, one per language, in
  `locales/`. `asuka generate` (and `dev`, `build`, as a catalog changes)
  writes `locales/catalog_gen.go`, a package with a typed function per message,
  which embeds and registers the catalogs. `locales.Unread(3)` says a string;
  `locales.Live.Unread(count)` is an accessor over accessors, for props;
  `locales.In("de").Unread(3)` says another language.
- **The language shown** is the process's, as `time.Local` is: not a root's,
  since a message has no context to read one from. `ui/locale.go` matches the
  environment's `Languages` (with what `ui.SetLocale` chose, first) against
  the languages the app has, by CLDR's distances, and falls back to the app's
  own: a reader of Chinese sees an English-only app in English, as on UIKit.
- **Following it.** A component runs once, so a message has to be read in an
  effect to follow the language, and it must read like any other function:
  a goroutine may say one too. A signal read can't do both, since subscribing
  touches `activeSub`, which is the main thread's. So `signal.Ambient` only
  counts a read, atomically; the main thread settles the count each time the
  running computation changes, subscribing the one that ran. A goroutine's read
  may subscribe whatever ran at that moment, which then runs once more at the
  next language change: harmless, and rare. A change costs the props whose text
  changed, as any signal does.
- **What can't follow.** A string read outside an effect stays what it was:
  `ui.StaticText(locales.Title())` doesn't change with the language. Every
  prop that shows text is therefore an accessor (tabs, actions, labels,
  placeholders, segments), and what a call shows once (an alert, a
  notification) takes a string.
- **Regional formats** are a setting of their own: the environment's `Locale`,
  `FirstWeekday` and `HourCycle`, with `ui.FormatLocale()` putting them
  together with the language (`en-GB-u-fw-mon-hc-h23`), as Apple's
  `Locale.current` does.
- **Asuka's own words** (`internal/l10n`) are the few the platforms don't supply:
  an alert's only button, a search field's placeholder, a desktop back button.
  They're a library's catalog an app overrides with keys `asuka.<key>`.

**Right to left.** The layout engine implements CSS `direction`, against
Chrome's results on Taffy's fixtures. The core sets it on each layout root
(the root, screens, sheets, cells, inline components); the rest inherit.
`Row`, `Justify` and `Align` follow it. `Edges.Left` and `Right` are physical,
as in CSS; `Start` and `End` mirror. Frames are always physical, so **a host
never mirrors what the core places**: it only mirrors what it draws itself.

What every host does:

- **Reports** `Languages` (BCP 47, most preferred first, the app's own language
  first if the system or `SaveLocale` keeps one), `Locale`, `FirstWeekday`
  and `HourCycle`, again as they change.
- **`Locale`** (a command, as the root mounts and when it changes; flags, then
  the tag): the platform's views go that way, whatever the system's language,
  for what they lay out themselves: a navigation bar and its back button, push
  and pop transitions, the tab bar, a list's swipe actions, a text's natural
  alignment (`TextStart` is the side the language starts on, and is the default),
  a slider's direction, `RichText`'s paragraphs. Where the platform lets it, it
  also says the platform's own words in the language.
- **`LayoutDirection`** (a common prop): the same for one view, for what
  `ui.WithDirection` builds.
- **A horizontal `Scroll` or `List`** starts at its trailing end right to left,
  and keeps its distance from that end as its content's width changes. The
  content is at the scroll view's origin, laid out from the right within it.
  `OnScroll` and `ScrollToOffset` stay physical.
- **Symbols** whose names say a direction by `forward` or `backward` mirror
  (`chevron.forward`); `chevron.left` doesn't, since it means left.
- **`SaveLocale`** keeps the language: AppleLanguages of the app's defaults
  (iOS, macOS), which Settings shows and UIKit and AppKit read at launch;
  `LocaleManager` (Android), or the host's own preferences on Windows. Android declares its languages in `locales_config`, iOS and macOS
  in `CFBundleLocalizations` and `.lproj`s: `asuka build` writes them from the
  catalogs, with the app's name and purpose strings in each.

Under `asuka dev` the player runs every app, so `boot.SaveLocale` keeps the
choice beside the kept state on the Mac, not in the player.

What each host does by its own means:

- **iOS**: `semanticContentAttribute` through the appearance and a walk of what
  exists, with a window trait override, since the back chevron follows traits
  (iOS 17 and later). The walk waits until Go has returned: a collection view
  asks for its cells as its direction changes. UIKit's own words stay in the
  process's language until the next launch.
- **Android**: the themed context carries the locale and direction, views are
  made again as for a dark mode change, and `AsukaActivity` overrides
  `getResources` and `getTheme` for Material's pickers, which are `DialogFragment`s
  on the Activity's context. It takes no recreation.
- **macOS**: `setUserInterfaceLayoutDirection:` per view. `NSApplication`'s
  direction is read once, so the host sets `NSForceRightToLeftWritingDirection` in
  the argument domain before it makes the app, from the language it will show. A
  change of direction while running leaves the traffic lights, the title, a
  vertical scroller's side and an alert's button order until the next launch. The
  standard menus' words come from AppKit's own tables.
- **Windows**: `WS_EX_LAYOUTRTL | WS_EX_NOINHERITLAYOUT` on top-level windows only.
  Each has one child that fills its client area, so below it every window and DC
  stays left to right, and frames stay physical, while Windows mirrors the title
  and menu bars. The date picker, which isn't custom drawn, doesn't mirror.

A line of text reads as its own letters do (its first strong character), and its
side comes from the view's direction, on every host; only a `WithDirection` forces
a reading order. So a message in a right-to-left language has to say how it starts
when an argument or a number comes first: `i18n` then begins it with a
right-to-left mark (CLDR's Arabic percent begins with a left-to-right one).

## Desktop, later

After M6, on the same core, `ops` and app code:

| Platform | Host | Notes |
|---|---|---|
| macOS | Go, AppKit through purego (`host/darwin`) | No cgo. `internal/darwin` is MyGo's Objective-C runtime layer (classes made at runtime, blocks, `WithPool`, a main run loop source for `Post`). In progress (see Milestones). |
| Windows | Go, Win32 with common controls v6 through `syscall` (`host/windows`) | No cgo; `internal/windows` is the Win32, GDI, GDI+, uxtheme, DWM and WebView2 layer. Not WinUI 3: it needs the Windows App SDK beside the app, and XAML is a second reconciler. `List` is core-virtualized: no Win32 list hosts arbitrary rows. In progress (see Milestones). |
| Linux | Go, GTK 4 and libadwaita through purego (`host/linux`) | No cgo; `internal/gtk` binds GTK 4.14, libadwaita 1.5 and the GLib stack (Ubuntu 24.04, Debian 13, Fedora 40 and later). libadwaita, which every GNOME app links, gives navigation, dialogs and the header bar, and the look an app that sets no style should have. In progress (see Milestones). |

Desktop hosts run in process: `app.Run` locks the main OS thread and
runs the platform's event loop there, and reads the batch's Go structs
without encoding. purego callbacks are made once per signature and
routed by user data, never per view.

How the macOS host maps Asuka (`host/darwin`):

- One window holds the root, its content a flipped view; the
  environment is the content's size, the display's scale, dark mode,
  Reduce Motion, locale and time zone (no safe area, keyboard or text
  size). Frames are set directly: AppKit's coordinates are flipped to
  Asuka's (top left) by flipped container views.
- Controls are AppKit's: `NSTextField` (labels and fields; secure ones
  swap to `NSSecureTextField`, multiline ones to an `NSTextView`),
  `NSButton` (Plain is borderless text in the accent color, Filled the
  accent bezel), `NSSwitch`, `NSSlider`, `NSStepper`,
  `NSSegmentedControl`, `NSPopUpButton` for `Wheel`, `NSDatePicker`
  (its calendar and clock in place for `Inline`),
  `NSColorWell`, `NSProgressIndicator`, `NSImageView`, `NSScrollView`.
  Menus are `NSMenu`s: a button's pops up, a view's `LongPress` menu is
  its context menu (a secondary click). A secondary click is also a
  long press. A swipe is a pan that ends fast (a drag with the mouse), a
  trackpad's two-finger flick (across always; up and down where no
  scroll view around scrolls that way, the scroll still going to it), or
  a three-finger swipe where the system leaves those to apps.
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
- `WebView` is a `WKWebView`, as on iOS: `Block` a content rule list
  (the page loads once it's compiled, so its first requests are blocked
  too), a response it can't show a `Download`, its state observed (KVO)
  and reported once a turn; the `web` module runs script for its value
  (`callAsyncJavaScript`) and reads the cookies, the `js` module runs
  asuka/js's scripts in a web view that's never shown.
- `RichText` is a non-editable `NSTextView` (TextKit lays it out and
  draws it, links, selection and accessibility come with it); outside
  a `Selectable` it takes clicks only on links. Inline components are
  attachments of their size, their views placed at the attachment.
- Messages with float or struct arguments go through purego's typed
  functions (`internal/darwin`'s `Msg*`), which use reflection: setting a
  frame allocates. The rest go through `SyscallN`.
- Pull to refresh: a trackpad's (or Magic Mouse's) scroll pulled past
  a Scroll's or List's top, as its elastic overscroll lets it, and let
  go of beyond 60 points; a spinner above the content (the scroll view's
  top content inset) while it refreshes. Matched views move the old view
  into its window's content view and the frames carry the motion.
- `ui.Native` and the app's modules: the app's Go registers them with
  package `native` (`RegisterView`, `RegisterModule`): a view is an
  `NSView` the app makes through purego's objc package, put in the
  node's view and filling it; on Windows a child window made in the
  context's `Parent`, its controls' notifications given to it. On the
  Mac the app's Swift registers into the same registry (`host/darwin/
  swift.go`: the library the CLI compiles from `macos/*.swift`). The
  mobile hosts keep Swift and Kotlin, the web JavaScript.
- Visual props are the views' layers'. The layers under the window's
  flipped content view have y down: gradients' points, shadows' offsets
  and transforms are given so. AppKit owns a layer-backed view's
  layer's transform and shadow: it sets the transform to the identity
  as the window displays, so `Style.Transform` is held by an animation
  that never ends (from what's drawn, so `ui.Animate` animates it), and
  it sets the shadow from the view's own (none) as the view moves into
  a window, so the host sets it again then. A shadow is cast by the
  view's shape (`shadowPath`), as on iOS; a view that clips and casts
  one clips its children in a view of their own that fills it, as on
  iOS: a layer that masks to its bounds masks its own shadow too.
- The packages' modules are iOS's, the same methods and replies, through
  the Mac's frameworks (each file of `host/darwin` registers its own):
  AVFoundation for the camera (the view's layer is the capture session's
  preview; photo, movie and metadata outputs) and `AVAudioRecorder`,
  with no audio session; Contacts, EventKit and PhotoKit; Vision,
  NaturalLanguage and Speech, off the main thread; MapKit;
  AuthenticationServices; UserNotifications and the application
  delegate for APNs. What iOS shows that AppKit hasn't, the host shows
  itself: `camera.Take` is a sheet with a live preview, and
  `contacts.Add` and `calendar.Add` ask in an alert over the window, then
  save. A Mac has no back camera ("back" is the built-in one), no motion
  sensors, no widget extension; a task in the background holds an
  activity (App Nap off: a Mac app runs out of sight), periodic ones run
  while the app does; `PreventCapture` keeps its windows out of
  screenshots and recordings (`NSWindowSharingNone`) and `OnScreenshot`
  sees the files Spotlight finds (`kMDItemIsScreenCapture`).
- Debug automation: `ASUKA_SNAPSHOT=file.png` renders the window to a PNG
  once it has shown, without taking focus (`ASUKA_DUMP=1` writes the view
  tree beside it), then quits.

How the Windows host maps Asuka (`host/windows`):

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
- A view that clips with a corner radius is shaped by a window region
  (`SetWindowRgn`), which clips its child windows too: the pixels its
  rounded rectangle covers whole. Its corners' edge pixels, outside the
  region, are its parent's, which draws them antialiased: the view's
  tree drawn there and mixed with what's behind by how much of each
  pixel the rounded rectangle covers. A window in it painting over a
  corner has the parent draw that corner again.
- What a child window can't be, a picture draws: a view of the host's
  above the view, taking no clicks, that draws what's behind it, then
  the view's tree as it paints. A view with child windows at an opacity
  between 0 and 1 is drawn blended with what's behind it, as one
  picture (overlapping children don't show through each other); the
  view stays under the picture and takes clicks and the keyboard. A
  view without child windows fades its own drawing. (A layered child
  window can be translucent, but the windows in it aren't painted.) A
  rotated view is drawn turned (GDI+), its tree taken over black and
  over white for its coverage; its own window is shaped to nothing
  meanwhile, so it's drawn, not used. A picture is drawn again as
  what's in the view paints, or moves.
- `Style.Transform`'s translation moves the view's window; its scale
  sizes the window about its center and multiplies the scale of what's
  in it (`k`, as a window's DPI does: fonts, frames, controls and
  drawing at the scaled size, still windows that take clicks); a scale
  that differs between the axes sizes the window by each and what's in
  it by their geometric mean. They step with `ui.Animate`'s frames.
- `Style.Material` is drawn as Fluent's acrylic in the view (DWM's
  backdrops are top-level windows' only): what's behind it, blurred
  (three box blurs, a Gaussian's likeness) and tinted light or dark. A
  view under it painting draws it again.
- Controls are the common controls, drawn as Windows 11 draws its own
  (WinUI's Fluent styles) through custom draw, so they look current in
  light and dark mode and keep their keyboard, focus and accessibility:
  `BUTTON` (Filled is the accent button, Plain accent text, Tinted an
  accent wash, Gray and Bordered the standard button), a check box
  drawn as Fluent's toggle switch (Win32 has no switch), the trackbar
  as Fluent's slider, `EDIT` in a frame the host draws (secure with a
  password character, multiline with return, Number with `ES_NUMBER`),
  `COMBOBOX` for `Wheel`, the date and time picker (inline, a month
  calendar, with a time picker under it for a date and time), two buttons for
  `Stepper`, a swatch opening the color dialog for `ColorPicker`. The
  segmented control, progress bar and ring are the host's own views,
  drawn as Fluent's. Each control sits in a view of the host's, which
  the core places: some controls send their notifications to the parent
  they were made in, so a control never changes parent. The month
  calendar has no dark visual style: dark, it's drawn classic in the dark
  palette's colors (its title and days' names in the accent's), the date
  picker's drop-down one likewise; the date and time picker's field,
  which has none either, paints itself light (its text antialiased in
  grays) and is turned dark, white to the dark field's background and
  black to its text.
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
  screen's title, its bar buttons, and its search field
  (`ScreenOptions.Search`: a Fluent text box with the search glyph
  before the buttons, up to 280 points wide, as WinUI puts an
  AutoSuggestBox in a command bar; typing is the screen's TextChanged,
  Enter its Submit). `Tabs` are a bar on top, as WinUI's
  top `NavigationView`. A `Sheet` is a modal window over the window in
  use (the one activated last: the main one, a `Window`, a sheet; the
  owner disabled meanwhile; closing it or Escape dismisses it), half
  the window's height for a medium-only sheet. An `Alert` is a task
  dialog over the window in use; an action sheet's buttons are command
  links. A menu is a
  popup menu: a button's drops below it, a view's `LongPress` menu is
  its context menu (a right-click, or a long press).
- A `Window` is a top-level window of its own (resizable, its own title
  and taskbar button; 800 by 600 points if the app gives no size),
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
  (centered in the work area; 800 by 600 points if the app gives none)
  and its least size (320 points square if the app gives none).
- The main window comes back where the user left it, as macOS's frame
  autosave has it: its normal rectangle (screen pixels, and the DPI it
  was at) and whether it was maximized, in `asuka-window.json` in the
  app's data directory (`ASUKA_DATA`, Local AppData\<name>; a file rather
  than the registry, so it goes with the app's other data, and `asuka
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
  (`go test`) and automation runs (`ASUKA_SNAPSHOT`, `ASUKA_SCRIPT`) keep
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
- Pinches and rotations: a touch screen's two fingers (`WM_GESTURE`'s
  zoom and rotate, rotate asked for with `SetGestureConfig` as a view
  listens), and a precision touchpad's pinch, which Windows sends as the
  wheel with Ctrl (so is a mouse's Ctrl and wheel, 10% a notch: began
  with the first, ended once the wheel rests for 250 ms).
- The native modules (`ui.Call`): `secureStore` in the Credential
  Manager (the user's generic credentials, `<app>/<key>`, up to 2560
  bytes a value), `device` (info, the clipboard's text, keeping the
  display on), `browser`, `photos` and `files` (the open file dialog,
  the files copied to the caches), `web` (`Runtime.evaluate` through
  WebView2's DevTools protocol, awaiting a promise; `Network.getCookies`)
  and `js` (a blank page in a WebView2 that's never shown); `share`
  (Windows's share UI, `IDataTransferManagerInterop` and the
  `DataTransferManager` of the window in use: text, a link, files as
  `StorageFile`s through an `IIterable` of Go's; `ui.Share` too),
  `network` (the Network List Manager: connectivity and its changes
  through a connection point, the connection's adapter's type, the cost
  as metered and constrained), `speech` (SAPI's `ISpVoice`, its events
  posted to the message window; the pitch SAPI's XML's), `audio` (an MCI
  `mpegvideo` device per player, a play's end `MM_MCINOTIFY`), `notify`
  (toasts, `ToastNotificationManager`; scheduled ones
  `ScheduledToastNotification`; the app registers its AppUserModelID, a
  Start menu shortcut with it and the user's
  `Classes\AppUserModelId`, the first time, and a COM activator, a CLSID
  of its AppUserModelID's whose `LocalServer32` is its executable,
  named by the shortcut's `ToastActivatorCLSID` and the key's
  `CustomActivator`: its class object, registered as the app starts,
  gets `INotificationActivationCallback.Activate` with the toast's or
  button's arguments, JSON carrying the notification, and the reply's
  text; started by COM for a click (`-ToastActivated`, taken out of
  `os.Args` before the app sees them) the app keeps its window hidden
  for a background action and quits once Go is done with it; `showing`
  is `ToastNotificationHistory`'s toasts, read back from their XML; the
  badge an overlay icon on the taskbar button,
  `ITaskbarList3::SetOverlayIcon`, the count drawn, or
  `BadgeUpdateManager`'s for a packaged app) and `location` (WinRT's
  `Geolocator`, `RequestAccessAsync` first; place names from
  OpenStreetMap's Nominatim, a request at a time and a second apart,
  with a User-Agent naming Asuka, answers cached, as geocode-glib asks
  it). WinRT is called from Go
  (`internal/windows/winrt.go`): HSTRINGs, activation factories, the IIDs
  of parameterized interfaces computed from their signatures, delegates
  whose `Invoke` calls Go (on the thread WinRT calls them on, posted to
  the main thread).
- More modules, each file its own (`moduleHooks`, as on macOS; their
  windows of a class of their own): `audioRecord` (waveIn, encoded as it
  records by Media Foundation's sink writer to AAC in MPEG-4, or ADTS
  when streaming; 44.1 kHz, Windows's AAC encoder's), `camera` (Media
  Foundation's capture engine: the preview sink draws into the view's
  window, filling it; photos through the photo sink, JPEG; videos
  through the record sink, H.264 and AAC; `take` a dialog of the host's
  with a live preview, Take Photo and Cancel, as on a Mac; with scan
  on, the preview sink's sample callback gives the frames in RGB32,
  which the view draws itself and reads codes in four times a second), `webAuth` (a window over the one in use,
  which waits as for a sheet, with a WebView2 in it: the app's profile,
  or a throwaway one for Ephemeral; it ends when a navigation's scheme
  is the callback's), `biometric` (Windows Hello:
  `UserConsentVerifier` through its interop, over the window; the
  sensor's kind from the Windows Biometric Framework), `imageEdit` (WIC
  decodes, upright from the EXIF orientation; Go does the actions and
  writes; a video's frame from Media Foundation's source reader),
  `vision` (Windows.Media.Ocr's lines and words, FaceAnalysis's faces,
  `internal/barcode`'s codes) and `scan.file` (`internal/barcode`, pure
  Go: QR codes, Data Matrix, EAN, UPC, Code 128 and Code 39, found at any
  angle; Windows has no reader for apps), `photoLibrary` (the
  Pictures folder, no permission to ask; screenshots are its
  Screenshots folder's), `library` (the known folders), `clipboard`
  (its image as PNG, from `PNG` or a DIB; copying one; changes by its
  sequence number), `background` (`ES_SYSTEM_REQUIRED` while a task
  runs), `periodic` (timers while the app runs, the tasks kept in its
  data), `screen` (`WDA_EXCLUDEFROMCAPTURE` on every window, those that
  show later through a WinEvent hook; screenshots seen as files appear
  in the Screenshots folder), `tracking` (the advertising ID's setting),
  `contacts` and `calendar` (Windows gives the stores to packaged apps
  only: denied; adding opens a vCard or an iCalendar file in the user's
  app), and Apple's, Google's and packaged apps' modules (`appleAuth`,
  `ageRange`, `integrity`, `push`, `widget`, `review`), `splash`,
  `sensors` and `transcribe` answer what they would on a device without
  them. `asuka.map` is a view the host draws (Windows's MapControl is
  UWP's and needs a Bing Maps key): OpenStreetMap's raster tiles,
  two downloads at a time with a User-Agent naming Asuka, kept a week in
  the caches, the attribution on the map; dragged, zoomed with the
  wheel, a pinch, a double click and the keys; pins with a callout; the
  region reported once the user has moved it.
- Screen readers (Narrator, NVDA) read the host's views through UI
  Automation: each view of the host's own drawing answers `WM_GETOBJECT`
  with a provider of Go's (`IRawElementProviderSimple`, made once per
  view, on top of its window's host provider) giving its name (Label; a
  text's text; a pressable view's texts), help text (Hint, else
  Tooltip), control type (Role; a pressable view a button; a header a
  heading), whether it's skipped (Decorative, with what's in it), and the
  Invoke pattern for a view that presses. `Announce` raises a UI
  Automation notification. A view's screen reader actions are its
  context menu (Shift+F10, the menu key, a right-click); an adjustable
  view takes the arrow keys: UI Automation's RangeValue pattern sets a
  double, which a callback of Go's (`syscall.NewCallback`) can't take.
- Shadows are drawn by the view a view's window is in, under its
  children, around the view's shape: a child window can't draw outside
  itself. They're concentric rounded rectangles from 3σ outside the
  shape to 3σ inside it, whose compounded alpha follows a
  Gaussian-blurred edge of σ half the Radius, as the other platforms'
  shadows. Transitions and matched views move the frame, as on macOS,
  stepped with the animation; their fades fade the view as opacity
  does. A view going out keeps its window, and those in it, until it's
  gone.
- Pull to refresh is F5, the Windows way: the refreshable Scroll or List
  the focus is in, or else the innermost one on screen; a spinner floats
  at the top of its content while it refreshes.
- A WebView2 controller is closed, and its window destroyed, on a later
  turn of the message loop, never inside one of its own callbacks (a
  DevTools call's answer, a page's event the app destroys the view in):
  WebView2 frees what the callback is running in (seen with the Runtime
  156's `CdpHelper`). DevTools answers come on a later turn too.
- `WebView` is WebView2 (the runtime found in EdgeUpdate's registry
  keys, as its loader finds it, so no loader DLL ships); the app's
  assets are served from `https://asuka.assets/`; pages follow the app's
  dark mode. `Video` is a WebView2 page of a `<video>`, with Edge's
  controls: Win32 has no video view with controls.
- Frames are set with `SetWindowPos`, without copying the old pixels
  (a view's pixels depend on what's behind it). `ui.Animate` steps the
  frames on a timer, and transforms with them.
- The host builds for amd64 and arm64. GDI+ takes floats: Go's Windows
  calls pass them in the registers on amd64 (each in its position's), but
  on arm64 a C function takes floats in the vector registers, which
  Go's calls don't load; there a trampoline in Go's assembly (no cgo),
  called through `syscall.SyscallN` with a frame, loads the integer and
  float arguments and jumps to the function. A `RECT` by value goes in
  two registers on arm64 (by reference on x64). WebView2's client
  library is the runtime's of the process's architecture.
- Debug automation: `ASUKA_SNAPSHOT=file.png` writes the window (and the
  top sheet's, `file.sheet.png`, and each `Window`'s, `file.window.png`,
  `file.window2.png`…) with `PrintWindow`, and a menu open, `file.menu.png`,
  from the screen, once it has shown, without
  taking focus, then quits (`ASUKA_DUMP=1`: the view tree and the menu
  bar beside it); `ASUKA_SCRIPT` runs steps first (`tap:<text>`,
  `type:<text>`, `submit`, `back`, `escape`, `scroll:<y>`,
  `resize:<w>,<h>`, `key:<shortcut>` such as `key:ctrl+shift+k`,
  `key:` also goes to a menu open (`key:down`, `key:d`), `menu:<item
  title>`, `open:<menu title>` (a menu bar's menu, shown below its title;
  `escape` closes it), `close` (the window in use, or the last
  `Window`), `dpi:<dpi>` (that window as if moved to a monitor at that
  DPI: `WM_DPICHANGED`, the title bar Windows draws keeping its own),
  `wait:<duration>`, `snap:<file>`), through the controls'
  own messages and the message loop's key handling;
  `ASUKA_COLOR_SCHEME` is light or dark whatever the system's.

How the Linux host maps Asuka (`host/linux`):

- GTK 4 and libadwaita in the app's process through purego, no cgo
  (`internal/gtk`: typed functions purego builds once, and raw function
  pointers called through `SyscallN` on the hot paths, allocating frames
  and drawing, so they use no reflection). `app.Run` makes an
  `AdwApplication` (its ID asuka.json's `id`, `boot.ID`; unique, so a
  second launch activates the first, but in tests and automation runs)
  and runs GLib's main loop on the main thread. `Post` queues the
  function and adds an idle source (`g_idle_add`, thread-safe). Signals
  are connected through one C callback per signature, made once,
  routed by the data pointer to the connection's Go function.
- Every node's view is a `AsukaView`, a `GtkWidget` subclass the host
  registers (`g_type_register_static_simple`, its class's vfuncs set from
  Go at GTK 4's stable offsets): `size_allocate` places its children at
  the frames the core gave them (whole sizes, the fractional origin in
  the allocation's transform, with `Style.Transform` about the center),
  `snapshot` draws the node's shadow, background or gradient, its own
  content (an image, a symbol, a shape, a canvas), its children (clipped
  to its rounded shape when it clips) and its border over them, as a
  layer does, and `measure` asks for nothing (the core lays out), but a
  scroll's content asks for its frame, which the viewport scrolls. A
  control (a `GtkLabel`, `GtkButton`, `GtkEntry`...) is the only child
  of its node's view, filling it less the content inset. A widget is
  measured by itself (`gtk_widget_measure`), a text by a Pango layout of
  the label's (as wide as its widest line, min-content its widest
  word). Opacity is the widget's, so it fades its children too.
- The environment is the window's content's size, its surface's scale
  (fractional), the desktop's text scaling (GTK's Xft DPI over 96, as
  FontScale), libadwaita's dark style, animations off as Reduce Motion,
  GNOME's screen reader setting and clock format, the locale (LC_ALL,
  LC_TIME, LANG), the languages (LANGUAGE) and the time zone
  (/etc/localtime). `Environment.Platform` is `PlatformLinux`.
- Colors are libadwaita's named colors (`window_bg_color`,
  `card_bg_color`, `accent_color`...), read from the theme for the
  current style; asuka.json's accent is the theme's accent, through a
  style sheet of the host's. The text styles are libadwaita's type
  classes relative to the interface font (gtk-font-name): LargeTitle is
  `.title-1`, Title `.title-2`, Title2 `.title-3`, Title3 `.title-4`,
  Headline `.heading`, Footnote and Caption `.caption`. Text is styled
  with Pango attributes (no style sheet per view). The app's fonts are
  fontconfig's application fonts.
- Controls are GTK's and libadwaita's: Filled is `.suggested-action`,
  Plain `.flat`, Tinted a wash of the accent; `GtkSwitch` and
  `GtkCheckButton`, `GtkScale`, `GtkEntry` and `GtkPasswordEntry` (a
  `GtkTextView` for Multiline), a linked box of toggle buttons for
  `Segmented`, `GtkDropDown` for `Wheel`, linked minus and plus buttons
  for `Stepper`, `GtkColorDialogButton`, a date button opening
  `GtkCalendar` and hour and minute spin buttons, `GtkProgressBar`,
  libadwaita's spinner. Images are decoded off the main thread by Go's
  decoders (others by GDK's loaders) into textures the view draws at its
  mode, tinted through a mask. Symbols are the icon theme's symbolic
  icons where freedesktop or Adwaita has a match for the SF Symbol name,
  else the Material Symbol (as Android and the web draw them), written
  once as a symbolic SVG into an icon directory the theme searches, so
  every place that takes an icon name (buttons, menus, tabs) takes them.
- Text fields: a Multiline field's `GtkTextView` is styled as libadwaita
  styles an entry (filled, rounded, the accent's focus ring), its
  placeholder a dim label shown while it's empty, its font a tag under
  its text; it's measured by a Pango layout of its text as the view lays
  it out. A Rich field's styles are a text tag per style (Ctrl+B, I and
  U toggle formats); typed text takes the style before it. Selection
  offsets cross as UTF-16 (the core's) and characters (GTK's).
  PasteTypes: a paste of files, an image (as PNG) or data of a type the
  field takes (GDK's clipboard formats) is read asynchronously into the
  app's caches and reported with Pasted instead of going in. Autocapitalize,
  Autocorrect and ContentType are the input method's purpose and hints;
  ReturnKey labels a touch keyboard's key, so does nothing here.
- `RichText` is a `GtkLabel` styled with Pango attributes: the label's
  layout draws what a layout of the same text and attributes measured.
  Links are the host's (colored and underlined as a label's, hit-tested
  in the layout, a hand over them, a click claimed); inline components
  are shape attributes of their size on the baseline, their views placed
  where the layout puts them. A `Selectable` selects across its texts:
  GTK keeps one label's selection at a time (a label that selects takes
  the primary selection from the one that had it), so the selection is
  the host's. A drag on the `Selectable`'s view (claimed in the capture
  phase) maps the pointer to a text and a character through each
  label's Pango layout, each text between the ends in reading order has
  its part, drawn under its label (GDK's clip region of the range) in
  libadwaita's selection color; a double click selects words, a triple
  click paragraphs, Shift+click extends, Ctrl+A selects all, and Ctrl+C
  and the context menu's Copy (GTK's words) copy the parts, a paragraph
  each. A press on a link, a control or a pressable view is theirs.
- Each window has one header bar (an `AdwHeaderBar` in an
  `AdwToolbarView`), as GNOME's apps do: the innermost stack on screen's
  back button, its top screen's title, bar buttons and search (a search
  button, and GTK's search bar under the header, which typing in the
  window opens), or the tabs' view switcher; the app's menus
  (`MenuBar`) are its main menu, the last button, their shortcuts the
  application's accelerators in every window. A `Stack` is an
  `AdwNavigationView` (its pages slide; a swipe, the back button,
  Alt+Left and Escape pop, with `Popped`), `Tabs` an `AdwViewStack` whose
  `AdwViewSwitcher` is in the header bar on a wide window and an
  `AdwViewSwitcherBar` at the bottom of a narrow one (under 550 points),
  a `Sheet` an `AdwDialog` (a floating dialog, a bottom sheet in a
  narrow window), an `Alert` an `AdwAlertDialog` (the first default
  button suggested, a destructive one red, Escape the cancel button), a
  `Window` an `AdwWindow` of its own. Screens, sheets and windows are
  sized by GTK: each reports its `Environment` as it's allocated.
- Menus are `GMenu` models in `GtkPopoverMenu`s, their items actions of
  a group on the view: a button's drops from it, a view's LongPress menu
  is its context menu at the pointer (a secondary click, a long press).
- Lifecycle, as on a Mac: Background while no window of the app's
  shows (hidden, or its GdkToplevel minimized or suspended), and as it
  quits (its last window closing, GtkApplication's `query-end` as the
  session ends, `shutdown`, or SIGTERM, SIGINT and SIGHUP, which quit it
  as closing it does), so `asuka/store` writes what's pending; GLib's
  memory monitor's warnings are MemoryWarning. Crash reports: Go's crash
  output (`crashrun`), which has a signal in native code too (Native).
  Windows are placed by the desktop: GTK 4 has no way to place one.
- Input is GTK's event controllers, added as the core listens: a click
  (Press, Pressed; Enter and Space press a focused pressable view, which
  Tab reaches), a long press or a secondary click (LongPress), a drag
  (Pan; one that ends fast a Swipe, as a touchscreen's flick is), zoom
  and rotate gestures (touchscreens, touchpads; Ctrl and the wheel
  pinch too), motion (Hover).
- Animation: GTK doesn't animate a widget's allocation, so between
  AnimateBegin and AnimateEnd the frames, opacity and transforms the
  core sets are stepped on the frame clock with the core's curve (CSS's
  cubic béziers, a damped spring); a view's first frame doesn't animate;
  enter and exit transitions start from, and go out to, the
  transition's state (a view going out stays until it has, with the
  views in it). `Style.Match` notes where the keyed views are as an
  animated batch starts destroying; the old view's widget waits on top
  of its top view (a screen's, the root's), then moves to the new one's
  place, fading, as the new one grows from its old place.
- Drawing is GSK's, in the view's snapshot: a `Shape`'s outline is a
  `GskPath` (from its SVG form), filled and stroked (`GskStroke`), its
  trim a `GskPathMeasure`'s segment, stepped on the frame clock inside
  an animation; a `Canvas`'s commands become paths, strokes,
  transforms and Pango layouts as they're set, replayed each frame with
  raw calls (Clip a fill clip, Alpha multiplying the colors). A `Shader`
  runs on the CPU (package interp), as on Windows, into a
  `GdkMemoryTexture`; animated, on the view's frame clock. A
  `PageControl` is drawn as libadwaita's carousel dots, measured, and
  takes clicks and the arrow keys. A paging `Scroll` has no scroll bar
  and settles each swipe (a wheel's notch, a touchpad's or a
  touchscreen's swipe, the arrow keys) on a page, with `Paged`. Pull to
  refresh is F5 or Ctrl+R (the focus's refreshable Scroll or List, else
  the innermost on screen), a spinner on a card over its top while it
  refreshes.
- Drag and drop is GTK's: a `GtkDragSource` (its files a `GdkFileList`,
  its texts a string, its links `text/uri-list`, and a private type of
  the app's) on a view with `DragItems`, a `GtkDropTargetAsync` on one
  with `DropAccept` (the app's own drag known by `gdk_drop_get_drag`;
  another app's text and URIs read as it comes over: files by their
  names' types, other URIs links). GTK has no accessible actions of an
  app's (to 4.22), but its AT-SPI bridge gives a widget with action
  groups of its own the Action interface, listing their actions by
  name: a view's `A11yActions` are its group "action", named after their
  titles as GAction names may be ("action.Mark-as-read"), which Orca
  lists and does; the view is a group (so its label is read), has a
  popup (has-popup) and the key Shift+F10, and the actions are its
  context menu (a secondary click, the Menu key, Shift+F10). An
  adjustable view is a slider to screen readers and takes the arrow
  keys.
- The main window comes back at the size the user left it, and
  maximized if it was (a Wayland app can't place its window), in
  `asuka-window.json` in the app's data directory: `ASUKA_DATA`, which the
  host sets to the XDG data directory's folder for the app (Flatpak's
  own).
- Screen readers (Orca) read GTK's accessibility tree: a view's role (a
  pressable view a button, a header a heading), label and description.
- Debug automation, as on macOS and Windows: `ASUKA_SCRIPT` runs steps,
  then `ASUKA_SNAPSHOT=file.png` renders each window to a PNG
  (`ASUKA_DUMP=1`: the view tree beside it) and quits.
- Tests (`go test ./host/linux`) drive real GTK windows; they need a
  display and a session bus (`xvfb-run dbus-run-session`), and skip
  without GTK.

Desktop needs what mobile doesn't: several windows, the app's menu bar
and keyboard shortcuts, hover and right-click, resizable layouts. To
keep that additive, the core already treats the mobile screen as one
root with an environment (size, insets) that can change at any time,
and never assumes a single root in its own data structures.

**Windows, the menu bar and shortcuts** (`ui/window.go`):

- The main window holds the root (`app.Run`); `app.RunWindow(opts,
  root)` gives it a title and a size and least size
  (`ui.WindowOptions`, sent to the root as `WindowOptions` when it
  mounts). With none, a desktop host opens it at 800 by 600 points, a
  desktop window's size (Electron's default), not a phone's. A size the
  user chose, remembered by the host (macOS, Windows), wins over it.

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

**Files, one instance, quitting, recent documents and login**
(`ui/call.go`, `app/quit.go`, `app/dirs.go`, `recent`, `login`):

- `asuka.json`'s `fileTypes` (name, extensions, MIME types, UTI,
  `conformsTo`, role, icon) are declared by each build
  (`cmd/asuka/filetypes.go`): macOS and iOS `CFBundleDocumentTypes` by
  UTI, the app's own types (a UTI under its ID) exported, others the
  system may not know imported, iOS opening in place; Android
  `ACTION_VIEW` filters; Windows a ProgID per type and the extensions'
  `OpenWithProgids` (and `Applications\<exe>\SupportedTypes`), which the
  host writes under `HKCU\Software\Classes` as it starts, from
  `boot.FileTypes` (internal/filetype, `-ldflags -X`), the icons in the
  executable; Linux the desktop file's `MimeType` (and `%U`), a
  shared-mime-info package for the app's own types and their icons; the
  web manifest's `file_handlers`.
- Files reach the core with no event of their own: `IncomingURL`'s
  string carries `file://` URLs, one per line (internal/fileurl; a URL
  has no raw line break), so the files opened together arrive together.
  `ui.incoming` splits them: the paths go to `ui.OnOpenFiles`, the other
  URLs to `ui.OnOpenURL`; both are owner-scoped and keep what came before
  any handler for the first. macOS: `application:openURLs:` (Finder,
  Open With, the Dock icon, the File menu's Open Recent). Windows and
  Linux: the command line's files that exist (taken from the launch's
  working directory) and `file://` URIs, with the scheme's URLs
  (`fileurl.FromArgs`); Linux's GApplication `open` too. The web: the
  launch queue's files, read into the app's caches.
- One instance: an app with an ID runs once on Windows (a mutex of the
  session; the second launch sends its working directory and arguments
  through `WM_COPYDATA`) and Linux (a unique GApplication with
  `HANDLES_COMMAND_LINE`: the second launch's command line over D-Bus),
  except under automation and `asuka dev`, or with `"singleInstance":
  false` (`boot.MultipleInstances`). The running one comes to the front,
  `app.OnSecondInstance(args, dir)` gets the arguments, and their files
  and URLs come as above. macOS's Launch Services runs an app once by
  itself, and passes files as Apple Events, not arguments.
- Quitting: `app.Quit` (`boot.Quit`, set by a desktop host) quits as the
  user's Quit does. Every quit asks `boot.QuitRequested` (package app:
  `app.OnQuit`'s handlers, with a `QuitEvent` they can `Cancel`) once;
  one they allowed only finishes. macOS asks in
  `applicationShouldTerminate:` (⌘Q, the Dock, the last window, SIGINT
  and SIGTERM, which send `terminate:`), with `SessionEnding` after
  `NSWorkspaceWillPowerOffNotification`; Windows on the main window's
  `WM_CLOSE`, Ctrl+C and `WM_QUERYENDSESSION`; Linux on the last
  window's `close-request`, SIGTERM, SIGINT and SIGHUP, and `query-end`.
  The session's end can't be canceled; a second signal exits at once.
  Phones and the browser never quit themselves (`boot.Quit` is nil).
- `app.DataDir`, `ConfigDir`, `CacheDir` and `LogDir` are
  internal/appdir's, each platform's place for the app (`store.Dir` is
  DataDir).
- `asuka/recent` (module `recent`: `add`, `clear`, `list`):
  NSDocumentController's recent documents on macOS, in an Open Recent
  menu the host fills as it opens (in the File menu once the app
  declares file types, has recent documents or adds one; choosing one
  goes through `openURLs`); on Windows `SHAddToRecentDocs` (the jump
  list's Recent) and the app's own list in its data directory, cleared
  with `IApplicationDestinations::RemoveAllDestinations`; on Linux the
  user's `recently-used.xbel`, through GBookmarkFile, as the app's (its
  application name), clearing only the app's. Phones and the web answer
  nothing.
- `asuka/login` (module `login`: `set`, `enabled`, `openedAtLogin`):
  SMAppService's `mainAppService` on macOS 13 (a bundle only;
  `requiresApproval` is `ErrNeedsApproval`), the opening Apple Event's
  `keyAELaunchedAsLogInItem` for `openedAtLogin`; Windows the HKCU `Run`
  value (and Task Manager's `StartupApproved` for `enabled`); Linux an
  XDG autostart entry, or in a Flatpak the Background portal's
  `RequestBackground`. The Windows and Linux entries start the app with
  `boot.LoginArg`, which the host takes out of `os.Args` in its init.

## The web

The app's Go code is a WebAssembly program (`GOOS=js GOARCH=wasm`,
no cgo), the page's DOM its views. As the mobile hosts, the host is the
platform's language, JavaScript (`platform/web/src`, plain ES modules the
CLI ships as they are), and Go's side (`host/web`) carries batches and
calls:

- `Apply` encodes the batch (`ops.Batch.Encode`) and calls the host once;
  the host copies it out of the WebAssembly memory (a call back into Go
  meanwhile may grow the memory, which detaches the old buffer) and
  decodes it with `ops.js`. `Measure` and `Apply` pass their arguments in
  a buffer the two sides share (its address given once), so they
  allocate nothing. Events come back encoded, as the mobile hosts send
  them. `Post` queues the function and asks the page for a task
  (`MessageChannel`).
- The page has one thread: the core runs in the callbacks the page makes
  into Go (events, list requests, posted work), and `Run` keeps the
  program alive between them. The goroutine a callback runs on is the
  main thread while it does (`signal.BindThread`). The host never calls
  into Go while Go is calling it: events raised meanwhile (a field's
  focus as the core focuses it) and work that would call back (a list
  tiling) wait until Go returns, as on the other hosts.
- Each view is an absolutely positioned element at its frame; children
  are placed from the frame's corner, so a border is drawn inside the
  frame over the content (an inset outline), as a layer's is. A measured
  leaf is measured by its own element at the constraints (max-content,
  min-content, or wrapped at the width; a wrapped text as wide as its
  widest line, as UIKit and Android measure), moved into a measuring
  element when it's in a part of the page that isn't displayed.
- Semantic colors are CSS variables with light and dark values; the
  app's accent is the controls' `accent-color`. Text sizes are in `rem`,
  so they follow the browser's text size (`FontScale`).
- Navigation draws its own bars, as the Windows host's: a `Stack`'s
  header (back, title, bar buttons, search), `Tabs`' bottom bar, a
  `SplitView`'s sidebar (a drawer below 700 points). Sheets and alerts
  are modal `<dialog>`s (the top layer, what's under them inert);
  popovers and menus are `popover` elements. The browser's back goes
  back in the app: a history entry stands for it while there's a dialog
  to close or a stack to pop, and its `popstate` goes back (closing the
  newest dialog, or popping the innermost stack, or `ui.OnBack`'s Back).
- `List` is core-virtualized, as on the desktop hosts, in a scrolling
  element: rows placed by the host with transforms, bound through
  `ListBind` as they come into view, recycled as they leave; a row's
  height from its cells' frames, estimated until bound; a chat's list
  kept at its end; swipe actions as the row's context menu.
- `Shader`: the core's code carries no GLSL, so the host asks Go for it
  (`internal/shadercode`'s `GLSL`, linked only in apps with a shader,
  through `boot.ShaderGLSL`), compiles it with WebGL 2, and sets the
  uniforms' lanes by their GLSL types.
- Files: Go's `os` on `js/wasm` calls Node's `fs` API; the page gives
  it a file system in memory that answers at once (Go waits for each
  callback), the app's data directory (`/data/<name>`, `ASUKA_DATA`)
  loaded from IndexedDB before Go starts and written back as it changes
  (a large file in chunks of 64 KiB, only those changed; each data
  directory's changes in one IndexedDB transaction).
- SQLite: `modernc.org/sqlite` has no `js/wasm` build, so `asuka/sqlite`
  has a second driver there (`sqlite/driver_js.go`) over SQLite's own
  WebAssembly build (`platform/web/sqlite`, vendored from sqlite.org;
  the CLI ships it with the apps whose wasm has `sqlite.Open`, and the
  loader loads it before Go starts, since `Open` is synchronous).
  `platform/web/src/sqlite.js` drives it from Go synchronously: one call
  per statement (arguments read from Go's memory, the answer written
  into it), a query's rows a chunk at a time, statements cached by SQL.
  Its VFS keeps databases as files of the page's file system, so the
  data directory's persistence keeps them, and Go's `os` sees them;
  each call runs to its end before the page does anything else, so a
  kept state is always one SQLite's rollback journal recovers from.
  The tables commits write (the preupdate and commit hooks, the
  authorizer) and queries read come back with each answer, for the live
  queries, as modernc's hooks give them natively. One connection per
  file (no shared memory, so no write-ahead logging; the page can't
  block on a lock): locks between two opens of a file are the VFS's,
  and a statement that finds the file locked is retried from Go, which
  lets the other run.
- Native modules: `platform/web/src/modules` (`system.js`,
  `hardware.js`, `web.js`), each method decoding the Go package's bytes
  as the native hosts do; what a browser can't do answers an error that
  says so.
- Web views: a page of the app's origin is fetched and written into a
  fresh `<iframe>` (its URL set to its own), with `window.asuka` and
  `Preload` before its scripts, so the host has its script, events,
  `Block` and navigation; another site's is a sandboxed frame that only
  loads, unless it answers Asuka's `postMessage` protocol (docs/web.md).
- Symbols are the Android host's Material Symbols, as SVG paths
  (`platform/web/gen_symbols.go` reads its drawables).
- `asuka dev --platform web` serves the page, the host's files (a checkout's, so
  changing them reloads the page too), the build and the assets, and
  keeps a connection to the page (server-sent events): a new build
  reloads it (the signals' values saved in its session storage first),
  a failed one shows its errors over it, and the GUI tests' automation
  commands go through it. `asuka build --platform web` names the files by their
  content's digest, so only `index.html` and the manifest need fetching
  again.

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

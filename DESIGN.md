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
| `Tab.Badge` | UITabBarItem.badgeValue | BadgeDrawable (getOrCreateBadge) |
| `Destination.Badge` | UICellAccessory.label | a counter (MenuItem's action view) |
| `Alert` | UIAlertController (alert, action sheet; `addTextField` for its fields) | MaterialAlertDialogBuilder (its fields: TextInputLayouts) |
| `Snackbar` | the host's own: a capsule in the system material (Liquid Glass on iOS 26) | Snackbar (Material), in an overlay CoordinatorLayout |
| `Sheet` | sheet presentation (detents) | BottomSheetDialog |
| `Sheet` (`FullScreen`) | `.fullScreen` presentation | full-screen Dialog (Material's full-screen dialog) |
| `Menu` (context, button menus) | UIMenu / UIContextMenuInteraction | PopupMenu |
| `Activity` (spinner) | UIActivityIndicatorView | CircularProgressIndicator |
| `Picker` (a menu of options) | UIButton with a menu (`changesSelectionAsPrimaryAction`) | exposed dropdown menu (TextInputLayout, MaterialAutoCompleteTextView) |
| `ProgressRing` | drawn (SwiftUI's circular Gauge's look); UIActivityIndicatorView without a value | CircularProgressIndicator |

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
- A Screen's **Toolbar** and **Primary** (encoded as menus) are its
  bottom bar and main action, which the host shows as its platform does:
  UINavigationController's toolbar (the primary action its prominent
  trailing item), Material's BottomAppBar and FloatingActionButton (the
  screen's own views, so they go with it in transitions), the window's
  toolbar on a Mac, a command bar on Windows, a GtkActionBar in an
  AdwToolbarView on Linux, a bar and a floating button on the web. The
  bar is in the screen's bottom safe area (iOS, Android), or the screen
  is sized above it (the desktops, the web). Their
  Action events carry ops.ScreenToolbarActions or ScreenPrimaryAction
  plus the item's number, apart from the bar buttons'.
- An **Insert** into a Stack pushes (animated once it's on screen); a
  **Destroy** of its top screen pops, animated. The host keeps the dying
  screen's views until the animation ends: the Destroys of its
  descendants that follow are no-ops. A pop the user makes is the same,
  started by the host: it animates, sends `Popped`, and the core
  destroys the screen.
- A **Sheet** and an **Alert** aren't inserted anywhere: `Present` shows
  them, Destroy dismisses them, the user's dismissal sends `Dismissed`,
  an alert's button sends `Action` (and isn't a dismissal). A sheet's
  `FullScreen` comes before its `Present`, which shows it as it was: it
  covers the screen (a desktop window's content), and its Environment
  is the screen's own, safe area and keyboard included. An alert's
  `Fields` (ops/alert.go) are text fields: the user's typing sends
  `FieldChanged` (the field's index, its text), which writes the
  field's signal, and its buttons' `Disabled` follow; return in the last
  presses the default button (the first `Default` one) if it's enabled.
- A **Snackbar** isn't inserted either: `Present` shows it at the bottom of its
  window, Destroy takes it out, animated. Its action sends `Action`;
  its time running out or the user's swipe sends `Dismissed`; the core
  destroys it after either. The core keeps one per root: showing
  another destroys the one showing first. Its timer is the host's
  (the platform's recommended timeouts), never the core's.
- iOS: `UINavigationController` (its interactive pop gesture),
  `UITabBarController`, sheet presentation controllers with detents,
  `UIAlertController`, `UIMenu` (button primary action, context menu
  interaction; nested and inline menus, disabled and checked items),
  `UIRefreshControl`, feedback generators.
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

A style's values live in `ui` when they're layout's (`ui.AlignCenter`,
`ui.JustifySpaceBetween`, `ui.TextCenter`: prefixed by their type) and
in their own packages when they're open-ended sets: `colors` and
`fonts`, so `ui` keeps its names for components and a color can't be
read as a weight (`fonts.Black`, `colors.Red`).

Colors and fonts can be semantic (`colors.Label`, `colors.SecondaryLabel`,
`colors.Accent`, `fonts.Body`, `fonts.Title`), resolved by the host
(UIKit's semantic colors and text styles; Material 3's color roles and
type scale), so dark mode and Dynamic Type / font scale need no app
code.

`Font.Family` (`fonts.Custom("Inter-Bold", fonts.Title)`) takes a font file
of the app's, by its name in `assets/fonts`; a family of them (`Inter`:
`Inter-Regular`, `Inter-Bold`..., picked by `Weight`); a generic family
(`serif`, `monospace`, `rounded`: on iOS the system font's designs); or
a system family. It scales as its text style does. Under `asuka dev` the
core sends a font file's URL instead, since the player hasn't the app's
assets: the host loads it, then sends `FontLoaded`, and the core
measures text again (its measure cache cleared).

### Native packages

Platform code an app needs that Asuka doesn't wrap lives in **native
packages** (package `native`, `internal/nativegen`, `asuka native`): a
Go package declares a module, `//asuka:native` on an interface, or a
view, `//asuka:view` on a `<View>Props` struct. Each platform
implements it in its own language, in a project of that platform's own
beside the Go:

- `Package.swift` (sources in `swift/`) for iOS and macOS;
- a Gradle library module in `android/`;
- `web/<pkg>.js`;
- Go for Windows and Linux.

The generator writes the typed glue on every side:

- Go: `<pkg>_native.go`, with `NewT`, a client per interface, the view's
  constructor, and `implementT` for Go implementations.
- Swift: protocols, structs and enums, a provider class
  `@objc(AsukaModule_<path>)`.
- Kotlin: interfaces, data classes, `object AsukaModule`, in the package
  `KotlinPackage(path)`.
- JavaScript: frozen enums and a provider registered by import path.

The platform side writes only `makeT` functions. The types are the
scalars, strings, bytes, times and durations, slices, `map[string]T`,
`*T`, the package's structs and string or integer enums, native
interfaces (as parameters or results), and funcs (notifications, run on
the main thread with `ui.Post`; questions, run synchronously).

Values cross as one buffer each way, in a fixed little-endian layout
(`native.Writer`/`Reader`, pooled), with no reflection or JSON:

- the arguments in declaration order;
- the answer's status byte: ok, a declared error by index, a platform
  error (domain, code, message), unsupported, or canceled.

A call runs synchronously on the calling goroutine's thread, through the
host's transport (`native.Backend`):

- iOS: C function pointers in `asuka_host_callbacks`;
- Android: JNI into `dev.asuka.AsukaModules`;
- macOS: the Swift library's `@_cdecl` functions through purego;
- the web: the JavaScript host.

`//asuka:mainthread` hops to the platform's main thread. A first
`context.Context` makes a method asynchronous (Swift `async`, Kotlin
`suspend`, JavaScript with an `AbortSignal`): Go's `CallContext` blocks
on a channel that `native.Done` answers, and cancels the platform's task
when the context is done. Objects are handles. Go releases them with
`runtime.AddCleanup` or `io.Closer`'s `Close`, and the platform's last
reference goes on its main thread.

A view is `ui.Native` of the kind `native.ViewKind` names (the module's
Swift class or Kotlin class or import path, `#`, the view), each prop
its encoded bytes (the wire's bytes value). Accessor props are effects
owned by the node. Each host finds a `#` kind through the module's
provider, `makeView`, and, on the desktop, first in package `native`'s
registry, where `implementT` puts a Go view.

`ui.Native` is the mechanism under it: a node whose `Type` prop, sent
right after `Create`, picks the factory, with props of bools, ints,
floats, strings and bytes, and events back by name. The hosts' own
native views (`asuka.camera`, `asuka.map`) use it directly.

The CLI generates the app's own module's native packages before each
build (others must ship current generated code, which is checked by
hash). It builds:

- iOS: one `xcodebuild` of a root package depending on the materialized
  host package `asuka` (the iOS host and the app's feature defines) and
  the app's native packages, its objects merged with `clang -r`, its
  frameworks embedded and its bundles copied;
- macOS: the same per architecture, into the Swift library, then lipo;
- Android: each module included as `:native-<pkg>` (`-PasukaNativeModules`),
  given the SDK levels and `:asuka`;
- the web: each `web/` shipped at `packages/<pkg>/`, imported before the
  app starts.

A package's `Info.plist`, `iOS.entitlements` and `macOS.entitlements`
merge into the app's: usage strings defer to asuka.json, arrays join,
and other keys are set if absent. Android's manifest merger takes the
module's `AndroidManifest.xml`.

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

### Keys

A hardware keyboard's keys, on a desktop, an iPad, a Chromebook, a
phone with one, and in a browser: what a view does with them while the
keyboard's focus is in it (`ViewProps.Keys`, `TextFieldProps.Keys`, a
`KeyMap` of `Shortcut` to func), and what the window does
(`UseKeys`).

- Declared, not asked: a view's keys are a prop (`Keys`: each key's
  name, a lower case character or a named key, and its modifiers, as the
  menu bar's), and the host routes a key going down (and each repeat) on
  its own: from the focused view out through its ancestors, the nearest
  with the key takes it, and the host sends `Key` with its index. So no
  host waits on Go to know whether a key is taken (the platforms want to
  know as the key goes down: a text field's caret, the menus' key
  equivalents), and it works the same with the app's Go elsewhere
  (`asuka dev` on a phone).
- The nearest first, as the responder chains go (AppKit's, UIKit's,
  the DOM's bubbling): a field's own Keys, then the views around it, then
  the window's (`UseKeys`: the root's, or a Window node's, Keys prop).
  A key goes out from the focused node through its parents in the
  core's tree. A node the core leaves without a parent, but which shows
  inside another, goes on where it shows: a List's row (a cell the host
  made and bound) to its List; a Sheet's node to the window it's
  declared in (its `Window` prop: the root's or a Window node's ID, the
  window it's presented on, as `UseKeys` in it are that window's), a
  Popover's to the root of what its anchor is in (a window's root, or a
  sheet's node, for a popover in a sheet), so its way ends at that
  window's root and its `UseKeys`; on a phone (one window), the root. A Window node is an end: its window's own keys (its
  `UseKeys`), never the main window's; the root is one too. With
  nothing focused, a key starts at the root of the window that has the
  keyboard: the root, a Window node, or a sheet's or a popover's node
  (then out, as above). A key a view takes doesn't reach the focused
  control (Up and Down in a search field move a selection, not its
  caret) nor the menu bar.
- Matching: the key as it types without modifiers (a character, lower
  case: Shift is a modifier like the others), and exactly the declared
  modifiers. Never while an input method composes text (marked text):
  its keys are its own.
- `UseKeys`' maps are merged per window, the last one called first for a
  key; each goes with its component's owner, and a view's with the view.
- Hosts: macOS, a local event monitor for key down: the first
  responder's view (a field's field editor: its field), its Asuka node
  (the window's root node with none: the root, a Window's, a sheet's or
  a popover's), out (a cell to its list, a sheet's node to the window
  it's declared in, a popover's to its anchor's window), ahead of the
  field editor and `performKeyEquivalent:`. Windows, the message loop's
  WM_KEYDOWN and WM_SYSKEYDOWN before TranslateMessage, IsDialogMessage
  and the accelerators: the focused HWND's node, out (a cell to its
  list, a sheet's node to the window it's declared in, a popover's to
  the window it covers, its owner). Linux, a key controller on each window, and each popover, in
  the capture phase: the focus widget's node, out (a list's cells are
  the host's views in it; a sheet's node to the window it's declared
  in, a popover's to its anchor's window). The web, a keydown listener
  on the window in the capture phase, ahead of the menu bar's:
  `event.target`'s node (the page's body: the root; a dialog's own
  element: its node), out (a cell to its list, a sheet's node to the
  window it's declared in, a popover's to its anchor's window), `preventDefault` and `stopImmediatePropagation`. iOS,
  `UIKeyCommand`s from the root view controller, which UIKit asks
  through the responder chain as a key goes down: the first responder's
  node's keys, out (a cell's to its List, a sheet's or a popover's node
  to the root); with no first responder, from the presented sheet's or
  popover's node, else the root. Each is
  `wantsPriorityOverSystemBehavior`, so they come ahead of a text
  field's caret, and they repeat while held. Android, the activity's
  `dispatchKeyEvent` (and a sheet's or popover's window, at its screen;
  with nothing focused there, its unhandled key, from its node): the
  focused view's node, out (a cell to its List, a sheet's or popover's
  node to the root); Ctrl is Primary, as Control.

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
                            dismissed, back (Android, unhandled),
                            windowState, closeRequest (desktop)
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
background task (`asuka/sdk/periodic`: BGTaskScheduler, WorkManager) with the
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
reading the entries the app's Go code sets (`asuka/sdk/widget`) from the App
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

### Plugins

A plugin is a Go package with a manifest beside it, `asuka-plugin.json`
(package `plugin`), naming the native libraries it brings: for each
target (`darwin`, universal; `ios`, `ios-simulator`; `android-<goarch>`;
`linux-<goarch>`; `windows-<goarch>`; `wasm`) a file, published to the
plugin's GitHub release gzipped and named for its hash, with the
SHA-256 of both. Only the manifest is checked in. The plugin's Go code
loads its library through purego or `syscall`, from the path
`plugin.LibraryPath` gives it: where the CLI shipped it with the app;
else, in `go run`, `go test` and `asuka dev` on a desktop, the plugin's
checkout's build (`build/<target>` beside the manifest) or the
published file, downloaded once into the user's cache and verified. A
shipped app (`asuka_bundled`) and a phone's never download.

The CLI finds an app's plugins among its dependencies for the build's
platform (`go list -deps`, a manifest beside the package) and ships each
library's file where `LibraryPath` looks: `Contents/Frameworks` on macOS
(signed with the app), beside the `.exe` on Windows, `lib/<exe>` of the
prefix on Linux, a framework of its own on iOS (named for the library;
an Info.plist, a privacy manifest from the binary's imports, signed with
the app), beside `libasuka.so` in each Android ABI, and `lib/` of the
host's files on the web (the page's `libs` option names each file's
URL): all in one place, so two plugins with a file of the same name for
a platform don't build. A desktop build that ships any builds with `asuka_bundled`.
A plugin builds its libraries with its own tool (they need what they
need: SQLite, Xcode, the NDK and Zig), which writes each with
`plugin.StoreBuild` and the hash of the plugin's sources (the manifest's
`sources`); `asuka plugin publish` uploads them, refusing a build older
than its sources, downloads each back to check it, and records them in
the manifest.

A plugin's name, which goes in those paths, is letters, digits, `-`, `_` and `.`; two of an app's plugins can't
share one.

### SQLite

`asuka/sdk/sqlite` is a plugin: SQLite's own C, one pinned release
built with the same options for every platform by
`sdk/sqlite/internal/build` (Xcode, the NDK, Zig). With it, one library of Asuka's
(`sdk/sqlite/csrc/asq.c`) and one entry point: `asq_call(op, conn, arg, in,
inLen, out, outCap)`, every argument pointer-sized. A call is a whole
statement: its SQL and arguments in, its answer (a query's columns and
first rows, as many as fit the buffer) out; `next` brings the rest a
chunk at a time, and an answer too long for the buffer is kept for the
caller to take with a bigger one. Statements are cached by SQL in C
(64 per connection). The tables a call's commits wrote (the authorizer,
the preupdate and commit hooks) and its queries read come back with
each answer, so the live queries need no callbacks from C into Go.

Go crosses into C once per call, never per column, which is what makes
purego as fast as cgo here (a crossing costs more than a row): macOS,
Linux, iOS and Android load the library with `dlopen` through purego
(the mobile build modes' cgo is purego's way to `dlopen`, not
Asuka's), Windows with `LoadLibrary` through `syscall`, and the web
calls the same C built to WebAssembly from JavaScript. Every symbol but
`asq_call` and `asq_version` is hidden, so a system SQLite in the same
process (GTK may load one) never clashes. The CLI ships it with the app
as any plugin's library ([Plugins](#plugins)).

A database has one writer connection and, for a file, up to three
readers (write-ahead logging), handed out by the Go side. Writes take
turns on the writer: a semaphore that times out as SQLite's busy timeout
does, so a write inside another's transaction fails with `ErrBusy`
rather than hanging. Rows scan into structs by a plan made once per
type (field offsets and kinds), written in place; no reflection per row.

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

An app's plugins' libraries ([Plugins](#plugins)) ship with it: on iOS a
framework each in `Frameworks` (`asqlite.framework` for SQLite: an
Info.plist, a privacy manifest of its own, signed with the app; not
linked, `dlopen`ed by its path from the executable's directory, or
`../../Frameworks` from an extension), and an XCFramework each beside
`AsukaApp.xcframework` for an app embedded in another; on Android each
beside `libasuka.so` in each ABI's `jniLibs`, loaded by name from the
APK. `asuka dev --once` pushes them with the Go library to the app's
`files/asuka`, and package plugin looks first beside the file its own
code was mapped from (`/proc/self/maps`).

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
asuka publish --platform mac|windows|linux   the desktop build, its signed update (dist/updates), uploaded: a GitHub release, S3
asuka update keygen                         the Ed25519 key pair that signs desktop updates
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
it's the windows' tint color, which UIKit's controls and `colors.Accent`
take. On Android it's the seed of a Material 3 color scheme
(`SchemeContent`, light and dark, as Material's `DynamicColors` makes
it) on Android 12 and later; before that, Material's baseline colors,
with Asuka's own uses of the accent (links, `colors.Accent`) taking the color
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

- **Noticing a change**: the system says when a file under the app's
  module changes (`asuka/sdk/watch`: FSEvents, inotify,
  ReadDirectoryChangesW; 25 ms for a burst to end), and a scan of the
  files' modification times decides whether it needs a build; where the
  system can't watch (too many files, no way to), `asuka dev` polls.
  Development builds leave out DWARF (`-ldflags=-w`), much of the
  linker's time; tracebacks and `runtime.Callers` use the symbol table.
- **The next run starts behind the last** (`internal/devstate`'s
  `Wait`): `asuka dev` starts the new build while the old one runs, and
  it waits on a control connection (`ASUKA_CONTROL`: it connects, sends
  its process ID, and waits for a byte), before it makes a window,
  listens or reads its kept state. Once it waits, the old run is stopped
  (its connection closed: it saves its state and exits), and the new one
  is let go on and takes the state back. So its start (the runtime, its
  packages' `init`, the frameworks loaded, and on macOS the system's
  check of a program it hasn't seen run, a few hundred milliseconds
  unless the terminal is a developer tool) happens behind the old one;
  a build that quits as it starts leaves the old one running. A run that
  takes over 10 s to wait doesn't hold the old one longer; one run
  elsewhere (`ASUKA_LINUX_RUN`) is stopped and started in turn.
- **The player's port outlives the runs**: `asuka dev` listens, and each
  run takes the listener (and the assets' one) as an inherited
  descriptor (`ASUKA_REMOTE_FD`, `ASUKA_ASSETS_FD`), so the player,
  reconnecting as its connection ends, waits in the queue for the next
  run instead of being refused and trying again.

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
- **A stuck player is reported**: a session whose player connected 10s
  ago and hasn't had the app mounted (it never sent its environment)
  makes the core ask it for its goroutines' stacks (`mStacks`), which
  the player answers on its connection's reader, not its main thread,
  which is likely what's stuck. The run says so, with what to do
  (relaunch the player, `--no-hot`), and writes the stacks beside the
  kept state. The GUI tests (`e2e`, `-run Player`) install the player,
  reload after an edit and relaunch it against the running code, and
  fail on that report.
- **Several players at once**: each connection is a session with its
  own root, multiplexed on the core's one loop (the app on an iPhone
  and an Android phone together). A reconnecting player clears the last
  session's views first.
- **State survives a reload** without code, as React's Fast Refresh
  keeps hooks' state: the process saves every app signal's value as it
  stops, and the next run gives it back to the signal made at the same
  place: the same call path (function names), name, type and position
  among the live signals of that name and type there. A signal made by
  Asuka itself isn't kept, nor a value JSON would change (funcs,
  unexported fields). Changing a signal's type resets it. Navigation
  isn't kept (screens are closures): a reload starts at the root
  screen. Off in a built app, where `signal.New` pays one nil check.
- **Places are named from the source** (`signal.KeepPlaces`): Go names
  closures by their order in their function (`Screen.func2`), so adding
  one, a button's handler, renumbers those after it. `asuka dev` reads
  the app's own packages as it builds them (`go/ast`; `go list` once,
  and again only for an import it hadn't seen) and adds a table to the
  build's overlay: each function literal's lines, depth and place among
  its siblings, named by what it is in its function (the variable or
  field it's assigned to, the call and argument it's passed as, a
  `go`, `defer` or `return`: `Screen>body`, `Screen>ui.Show#1`, a second
  of a name `~2`), and each `signal.New` and `NewFunc` by the variable
  or field it's made for. The run finds a frame's literal by its file
  and line, at the depth its runtime name says past its function's
  (`.funcN`, `.N` as inlined, `-rangeN` a range-over-func loop's body,
  no literal), and of siblings on one line by their place; what the
  table can't tell keeps the runtime's name. A file that says
  `//asuka:reset` has its signals start over when it's edited (its
  digest is in their names).
- **Reaching the Mac**: the simulator shares its loopback; an Android
  emulator reaches it at `10.0.2.2`, directly (`adb reverse` adds a
  round trip's worth of latency to every call); a device goes through
  `adb reverse`. Assets are served over HTTP from the Mac
  (`boot.AssetBase`).
- **Not in the player**: an app's own native code (`ios/`, `android/`),
  which only an app built with it can show. Such apps, `--once` and
  `--no-hot` build the app with its Go code, as before: `asuka dev`
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
  colors/            semantic colors and RGB/Hex, for a Style
  fonts/             semantic text styles, weights, families, for a Style
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
  sdk/               the packages beyond the core: what the platform offers
                     (biometric, camera, push, tray…) over native modules,
                     and libraries over ui (chart, markdown, svg, store…);
                     sdk/sqlite is a plugin (SQLite's own C, no cgo)
  plugin/            plugins' manifests, fetching and linking
  native/            native packages' runtime: the codec, modules, views
  internal/nativegen/ asuka native's generator (Go, Swift, Kotlin, JavaScript)
  cmd/asuka/         CLI
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
- Apps: `uitest` mounts an app's components on `host/fake` in its own
  `go test` and drives them by what the screen shows (tap, type, back,
  a row's swipe action), as a user would: only what shows counts (a
  stack's top screen, the selected tab, a list's rows that fit, which
  it binds as a collection view does), and taps reach only the top
  modal layer. It settles after each action (posted work, flushes, list
  binding), waits on the app's goroutines through the host's posts
  (never a sleep), and on cleanup fails the test if a view outlives
  the root. Text is measured by the fake host's rules, so frames hold
  relative to each other, not to the point. `Snapshot` describes the
  screen for golden files (`-uitest.update`).
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
  - Menus as SwiftUI's: an `Action` can be `Disabled` or `Checked`,
    open a submenu (`Children`) or be a titled section of its menu
    (`Inline`), each following its signals; items are numbered depth
    first (ops/actions.go), the same encoding for menus, bar buttons
    (which open their children as a menu), alerts (whose buttons follow
    `Disabled` while they show) and swipe actions (flat). UIMenu's
    attributes, state and inline menus; Android's checkable items,
    `SubMenu`s and groups with dividers; NSMenu's states, submenus and
    section headers; Win32 menus with an owner-drawn header; GMenu
    sections, submenus and stateful actions; the ARIA menu pattern.
  - `ui.Segmented`, `ui.Progress` and `ui.DatePicker`, each the
    platform's control (Material's pickers on Android).
  - `ui.ProgressRing`: circular progress with a value, or without one
    (the platform's indeterminate indicator). It is Material's
    CircularProgressIndicator, NSProgressIndicator's spinning style and
    Fluent's ProgressRing. iOS and Linux, which have no determinate
    ring, draw one in their own style. The web draws an SVG ring. A
    Mac's NSProgressIndicator draws only at 16 and 32 points and only
    in the accent color, so at any other size or in a Color the macOS
    host draws AppKit's circle and spinner itself, in the colors read
    from AppKit's own drawing.
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
  - `asuka/sdk/store` (small values kept across launches, `store.Signal`),
    `ui.Copy`, `ui.Share`.
  - The app's accent color (`asuka.json`).
  - The device's time zone as Go's `time.Local`.
  - Motion: `Style.Transform`, shadows, gradients, `Style.Material`
    (blur), repeating animations, enter and exit `Transition`s.
  - `asuka/sdk/sqlite` (SQLite's C, no cgo; live queries with `sqlite.Watch`), native
    modules (`ui.Call`: `asuka/sdk/secure`, the keychain and Keystore;
    `asuka/sdk/device`: info, an in-app browser, the photo picker).
  - A search field in the navigation bar (`ScreenOptions.Search`).
  - A screen's bottom toolbar and primary action (`ScreenOptions.Toolbar`,
    `Primary`, `PrimaryExtended`; the Screen's Toolbar and Primary props,
    encoded as menus, whose Action events add ops.ScreenToolbarActions and
    ops.ScreenPrimaryAction to the item's number): the navigation
    controller's toolbar (the primary item prominent, the tab bar hidden
    under a pushed screen's), Material's `BottomAppBar` and
    `FloatingActionButton` (docked, or extended and shrinking as the
    content scrolls), the window's toolbar on a Mac, a Fluent command bar,
    a `GtkActionBar` in an `AdwToolbarView`, a bar and a floating button
    on the web.
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
    allocation) and `asuka/sdk/chart` on it; `ui.Shader` (a Kage shader, Go's
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
  the window in use; a sheet and an alert go over the window they're
  declared in (their `Window` prop: an alert's, the window of the view
  whose event shows it), whichever is in use. The
  app's menus join the Mac's own: one titled File, Edit, View, Window or
  Help adds its items to it, the others go between View and Window; a
  disabled item is off through `validateMenuItem:`, as is Back with no
  screen to go back from. Swipes, the inline date picker; the WebView's
  blocked requests, downloads and state, `EvalJSResult`, `WebCookies`
  and asuka/sdk/js; device's files and clipboard modules; network
  (NWPathMonitor), location (Core Location), speech
  (AVSpeechSynthesizer), audio (AVPlayer), biometric (Touch ID), review;
  matched views (`Match`); pull to refresh (a trackpad's elastic pull
  past the top); native packages in Swift (loaded from the bundle) or Go (package
  `native`, below);
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
  native packages' views and modules in Go (package `native`); screen
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
  the app's files kept (IndexedDB, large files in chunks), `asuka/sdk/sqlite`
  (its C built to WebAssembly, in a worker, below; live queries), the native modules
  a browser has (`platform/web/src/modules`: the secure store under a
  non-extractable WebCrypto key, device, clipboard, pickers, share,
  preview, print, compose, notifications, Web Push, network, location, sensors, speech, audio, recording, the camera and
  `camera.View`, image editing, codes, WebAuthn for biometrics, web
  auth; the others answer that the web hasn't them), web views (all of
  it for the app's own origin; another site's page loads, and talks
  through `postMessage` if it cooperates), native packages' views and
  modules (their `web/`), several apps on one page; the app's service
  worker (`platform/web/src/sw.js`, which the CLI ships with every web
  app, named by its digest, beside `index.html`): notifications with
  their actions and inline replies that stay and answer after the page
  closes (a tap opens a page, which gets the response as it starts),
  `asuka/sdk/push` as Web Push with the app's VAPID key (`web.vapidPublicKey`;
  the subscription is the token; the page in front rewrites and presents
  a push), `asuka/sdk/push/webpush` for servers (RFC 8291's encryption and RFC
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
  6.0), `Video` (GStreamer), native packages' views and modules in Go,
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
  `NSSegmentedControl`, `NSPopUpButton` for `Wheel` and `Picker`, `NSDatePicker`
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
  is the screen's. `Tabs` is an `NSTabView` (a tab's badge in its label,
  "Inbox (3)", since it draws its tabs' labels alone; VoiceOver hears
  "Inbox, 3 items"); a `Sheet` a sheet window
  (Escape dismisses it); an `Alert` an `NSAlert` shown as a sheet (its
  fields `NSTextField`s, the accessory view, return pressing the default
  button); a
  `Snackbar` an overlay at the bottom of the window's content (an
  `NSVisualEffectView`, its time stopped under the mouse, announced
  through `NSAccessibilityPostNotificationWithUserInfo`).
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
  asuka/sdk/js's scripts in a web view that's never shown.
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
- Native packages: their Swift is built into the app's Swift library
  (`host/darwin/swift.go` loads it through purego and calls its
  `@_cdecl` entry points with `SyscallN`). A package's Go view
  (`implementT`) registers with package `native`: an `NSView` made
  through purego's objc package, put in the node's view and filling
  it; on Windows a child window made in the context's `Parent`, its
  controls' notifications given to it.
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
- Windows' options are `NSWindow`'s, set as it opens (`winctl.go`):
  its style mask (no resize, close or minimize button), the zoom button
  turned off, the content's least and most size, the floating level for
  always on top, out of the Window menu for skipping the taskbar (a Mac
  has none), a background color, or a clear and non-opaque window for
  transparent. A hidden title bar is a full-size content view under a
  transparent, title-less title bar: the traffic lights stay where they
  are, and `SafeTop` is the content's height less its
  `contentLayoutRect`'s (a toolbar in the title bar adds its own). A
  child `Window` is a child window of its parent's (ordered above it,
  moving with it), a modal one a sheet of it. Places are the frame's top
  left from the main display's top left, y down, flipped from AppKit's.
  The actions are AppKit's (`orderOut:`, `makeKeyAndOrderFront:` with
  the app activated and unhidden for `Focus`, `miniaturize:`, `zoom:`,
  `toggleFullScreen:`, `center`, `setContentSize:`,
  `setFrameTopLeftPoint:`, `setLevel:`), and the state is read back from
  the window as its delegate hears it move, resize, miniaturize, become
  or stop being key, and enter or leave full screen, and as the app
  activates, hides or unhides: visible is shown or minimized (a Mac's
  `isVisible` is false for a minimized window), maximized is zoomed. A
  window's frame comes back with AppKit's frame autosave, under
  `asuka <app>` for the main window's key (where it was kept before
  keys) and `asuka <app> <key>` for the others', restored as it opens
  (`setFrameUsingName:`). The user closing a window the core asks about
  (`windowShouldClose:`) leaves it open and sends `CloseRequest`;
  `WindowAction` Close closes the main window past that question, and
  the app quits once its last window has.
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
  a drop-down list `COMBOBOX` for `Wheel` and `Picker`, drawn as WinUI
  3's `ComboBox` (it paints its own box, its list's items owner-drawn on
  the flyout surface with the chosen item's accent pill, the list's
  window padded and rounded by DWM; a contrast theme's system colors;
  a Picker's label drawn while none is chosen and its Dynamic
  Annotation name), the date and time picker (inline, a month
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
  top `NavigationView`; a tab's or a pane item's badge is Fluent's
  `InfoBadge`, drawn, and the bar's or pane's UI Automation name is
  the chosen one's title and badge. A `Sheet` is a modal window over the window
  it's declared in (its `Window` prop: the main one, a `Window`, or the
  sheet shown over it; the owner disabled meanwhile; closing it or
  Escape dismisses it), half the window's height for a medium-only
  sheet. An `Alert` is a task dialog over the window it's declared in,
  likewise; an action sheet's buttons are command
  links. An alert with text fields is a dialog of the host's own, as
  WinUI's `ContentDialog` (alertdialog.go): its title and message,
  the host's Fluent text boxes, its buttons in a band below with the
  default one in the accent color; Tab, Enter and Escape as a dialog's,
  the owner disabled meanwhile. A `Snackbar` is a child window at the
  bottom of that window, drawn as Fluent's InfoBar, with BUTTON controls
  for its action and close, raised with a UI Automation notification. A
  menu is a
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
- Windows' options (the main window's, a `Window`'s `Config`) are applied
  before the window shows (`winctl.go`): the abilities taken away are
  window styles (no `WS_THICKFRAME` and maximize box for a window that
  can't be resized, no `WS_MINIMIZEBOX` or `WS_MAXIMIZEBOX`; one that
  can't be closed has its close button and the system menu's Close
  grayed, and the user's `WM_CLOSE` and `SC_CLOSE` ignored), the most
  size `WM_GETMINMAXINFO`'s as the least is, always on top
  `HWND_TOPMOST`, a place the visible frame's top left
  (`DWMWA_EXTENDED_FRAME_BOUNDS`) in points at the window's scale. Left
  out of the taskbar, a window is owned by the host's hidden window:
  Windows gives owned windows no taskbar button, which holds as it shows
  again and as Explorer restarts (`ITaskbarList::DeleteTab` would have to
  be redone then); it stays in Alt+Tab. A child `Window` is owned by its
  parent's window (above it, minimized and hidden with it), a modal one
  disables its owner while it's open, as a sheet does, and enables it
  again before it's destroyed, so the owner is activated. A background
  fills behind the content wherever the host paints the window's
  background. Transparent isn't: every view is a child window, composed
  over the top-level window's opaque surface, so a transparent window
  shows its background color.
- A hidden title bar is MyGo's: `WM_NCCALCSIZE` gives the client area
  the whole window (Windows 11 keeps a pixel at the top, without which it
  opens no snap layouts; a maximized window fits its monitor's work
  area); the caption buttons are a layered child window drawn as Windows
  11's, in Segoe Fluent Icons (Segoe MDL2 Assets on Windows 10),
  answering `HTMINBUTTON`, `HTMAXBUTTON` and `HTCLOSE`, so snap layouts
  open over maximize (at the top left, close first, right to left); an
  invisible top edge answers `HTTOP` and its corners, so the window
  resizes from the top; and the top 32 points answer `HTCAPTION` where
  the views let the mouse through (a view that takes no input, a stack's
  header bar, answer `HTTRANSPARENT`): the strip drags, a double click
  maximizes, a secondary click opens the system menu. `SafeTop` is 32
  (0 in full screen). The main window has no menu bar under a hidden
  title bar (its shortcuts still work): Windows would draw it under a
  caption that isn't there.
- The actions are `ShowWindow`'s (`SW_SHOWNOACTIVATE` while
  snapshotting), `SetWindowPos`'s, and for `Focus` a restore from
  minimized, then `BringWindowToTop` and `SetForegroundWindow`, which
  Windows' foreground lock allows after the user's input to the app (a
  click on its tray icon, in its windows). Full screen takes the caption
  and frame off and covers the monitor, keeping the placement to put
  back. A size or place restores a maximized or minimized window first.
  The state is reported on `WM_SIZE`, `WM_MOVE`, `WM_WINDOWPOSCHANGED`
  and `WM_ACTIVATE`; a minimized window keeps its last place and size.
  The user's `WM_CLOSE` (the close button, Alt+F4, the taskbar's menu)
  on a window the core asks about sends `CloseRequest` instead of
  `Dismissed`, or of quitting for the main window; `WindowAction` Close
  takes the main window's closing path (its placement saved, destroyed,
  the app quits).
- Each window comes back where the user left it, as macOS's frame
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
  coordinates (`GetWindowPlacement`'s are workspace coordinates). Each
  is kept by its key (the main window's `main`, a `Window`'s
  `StateKey`), a `Window`'s saved as it closes and restored over its
  options, within its least and most size. Tests (`go test`) and
  automation runs (`ASUKA_SNAPSHOT`, `ASUKA_SCRIPT`) keep none, and the
  data directory is made by what writes there.
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
  `Segmented`, `GtkDropDown` for `Wheel` and `Picker`, linked minus and plus buttons
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
  a tab's badge its page's `badge-number` (a count) or `needs-attention`,
  a `Sheet` an `AdwDialog` (a floating dialog, a bottom sheet in a
  narrow window), an `Alert` an `AdwAlertDialog` (the first default
  button suggested, a destructive one red, Escape the cancel button;
  its fields a boxed list of `AdwEntryRow`s, its extra child),
  a `Snackbar` an `AdwToast` in the `AdwToastOverlay` around the
  window's content (its button-label and button-clicked, its timeout),
  each presented on the window it's declared in (its `Window` prop), a
  `Window` an `AdwWindow` of its own. Screens, sheets and windows are
  sized by GTK: each reports its `Environment` as it's allocated.
- Menus are `GMenu` models in `GtkPopoverMenu`s, their items actions of
  a group on the view: a button's drops from it, a view's LongPress menu
  is its context menu at the pointer (a secondary click, a long press).
- Lifecycle, as on a Mac: Background while no window of the app's
  shows (hidden, or its GdkToplevel minimized or suspended), and as it
  quits (its last window closing, GtkApplication's `query-end` as the
  session ends, `shutdown`, or SIGTERM, SIGINT and SIGHUP, which quit it
  as closing it does), so `asuka/sdk/store` writes what's pending; GLib's
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
- Each window comes back at the size the user left it, and maximized if
  it was (a Wayland app can't place its window), by its key in
  `asuka-window.json` in the app's data directory: `ASUKA_DATA`, which the
  host sets to the XDG data directory's folder for the app (Flatpak's
  own). It's saved as the window closes and as the app shuts down.
- Windows' options are GTK's, set before the window shows
  (`winctl.go`): its content's size (the default size, the header bar's
  height on top) and least (the content's size request), not resizable,
  not deletable, the header bar's decoration layout without its minimize
  or maximize button (the window manager's shortcuts still do them),
  maximized or full screen, hidden until a `Show`, a background color or
  none (the window's own CSS provider, above the app's; transparent with
  a compositor). A hidden title bar extends the content to the top edge
  under a flat, title-less header bar whose window buttons stay;
  `SafeTop` is its height. A child is transient for its parent and
  destroyed with it, a modal one modal too. GTK 4 has no most size, no
  way to place a window or center it (Wayland leaves it to the
  compositor, and GTK 4 dropped moving one on X11 too), and none to keep
  one on top or out of the taskbar: under X11 the host does those itself
  through Xlib on the surface's window (`deskwin.go`: `XMoveWindow`,
  EWMH's `_NET_WM_STATE_ABOVE`, GDK's skip-taskbar hint); under Wayland
  they do nothing. The position reported is 0, 0. The actions are GTK's
  (`gtk_window_present`, `_minimize`, `_maximize`, `_fullscreen`, a new
  default size); the state follows `notify::visible`, `is-active`,
  `maximized`, `fullscreened`, the surface's state (minimized: X11 only,
  as Wayland never tells an app) and the content's allocation. A window
  the core asks about keeps itself open on `close-request` and sends
  `CloseRequest`; `WindowAction` Close closes the main window on the next
  idle turn (the core answers while GTK is still emitting the request).
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
  root)` gives it its options (`ui.WindowOptions`, sent to the root as
  `WindowOptions` when it mounts). With none, a desktop host opens it at
  800 by 600 points, a desktop window's size (Electron's default), not a
  phone's, centered. Where the user left it, remembered by the host,
  wins over its size and place.
- `ui.Window(WindowProps{Open, Title, Width, Height, ..., Content,
  OnClose})` is a window of its own while `Open` is true, built as a
  `Sheet` is: a `Window` node, a host-sized layout root whose
  `Environment` the host reports (it follows the user resizing
  it; its Scale is the window's, which on desktop can differ from the
  main window's: the core measures and snaps the frames in it at that
  scale), `Present` opens it (unless it's to open hidden), `Destroy`
  closes it, and the user closing it sends `Dismissed`, which sets `Open`
  false (or `CloseRequest`, while something asks). Its content's owner
  lives as long as the window is open, so a `Window` declared in it
  closes with it. Mobile hosts can't show one (it panics there, as any
  kind a host lacks).
- A window's options, the main window's and a `Window`'s alike, are one
  encoding (`ops.WindowConfig`: the `WindowOptions` command's args, the
  `Window` kind's `Config` prop), set before it opens and read by one
  decoder in each desktop host (`ops.DecodeWindowConfig`, and the web
  host's): its content's size, least and most size, its place (its
  frame's top left in points from the main display's top left, or
  centered), abilities taken away (resizing, the minimize, maximize and
  close buttons), always on top, a hidden title bar (`TitleBarHidden`:
  the content under it, the window buttons over the content, the title
  bar's height reported as the window's environment's `SafeTop`, so
  `SafeArea` keeps clear of a Mac's traffic lights as of a phone's
  notch), a background or none (transparent), hidden, maximized or full
  screen as it opens, left out of the taskbar, a state key, and a
  `Window`'s parent (the node of the window it's declared in: a child
  stays above it, a modal one blocks it). The options that change later
  are the handle's actions, not props, as a window's place and size are
  the user's as much as the app's.
- `ui.UseWindow()` is the handle (`*ui.WindowHandle`) of the window the
  component is in, from a context each window provides (the root the
  main window's, each `Window` its own), and `ui.MainWindow()` the main
  window's from anywhere: a tray icon's menu brings the window back with
  `Focus`. Its actions are one `WindowAction` command (u8 action, two
  floats for a size or a place) to the root or the `Window` node: show,
  hide, focus, minimize, maximize, restore, full screen, center, size,
  place, always on top, close. Its state is signals (`Visible`,
  `Focused`, `Minimized`, `Maximized`, `Fullscreen`, `AlwaysOnTop`,
  `Position`, `Size`), set from the host's `WindowState` event (the
  frame's top left, the content's size, six bits), which a host sends
  only while the core listens: a handle asked for (`UseWindow`,
  `MainWindow`, a `Window`'s `Handle`) listens; the rest cost nothing.
  The host reports at once as the core starts listening, then on every
  change, the user's or the app's, deduplicated; equal values don't
  re-run readers, as any signal's. Mobile hosts have no windows: the
  handle sends nothing there.
- Closing asks the app: `OnCloseRequest` (`WindowProps`',
  `WindowOptions`', and any component's through the handle, for as long
  as its owner lives) makes the core listen for `CloseRequest` on the
  window. A host whose window the core listens on reports the user's
  closing with it and keeps the window open; the core asks every
  function, and closes the window if all agree: a `Window` as the user's
  closing does (`Open` false, `OnClose`), the main window with
  `WindowAction` Close, which the host does as its own closing would
  (the app quits). `Destroy` closes without asking, what a refusal's
  dialog calls once the user has chosen. Quitting the app as a whole
  (⌘Q, the session ending) doesn't ask the windows.
- Where windows were is the host's, by the key the app gives (`StateKey`;
  the main window's is `main` when it gives none, a `Window`'s isn't kept
  without one): a Mac's frame autosave, the Windows host's rectangle in
  pixels with its DPI and whether it was maximized, the Linux host's
  size and whether it was maximized, in one `asuka-window.json` per app
  (`internal/winstate`, which reads the file an older host kept for the
  main window alone as the main window's), and on the web a panel's
  place in `localStorage`.

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
- A `MenuItem` with a `Submenu` opens a menu of those items. Items are
  numbered depth first (a submenu's items after the item that opens it;
  separators and submenus count), and `Action` carries that number. A
  `MenuItem` with a `Role` is the host's: `MenuRoleQuit` (`ui.MenuQuit`)
  quits the app as the system quits it ("Quit Notes", ⌘Q on a Mac), its
  title the host's unless the item has one, and is never reported;
  `MenuRoleClose` closes the window in use as its close button does
  (⌘W, Ctrl+W); `MenuRoleOpenRecent` is a Mac's Open Recent submenu,
  the host's, and elsewhere the item's own (Documents, below).

**Menus, one encoding** (`ops/menu.go`): the menu bar, a tray icon's
menu and the Dock menu are the same bytes: a menu is u32 count, then per
item u8 flags (separator, disabled, checked, destructive, submenu), its
title, its shortcut's key and modifiers, u8 role, and with the submenu
flag the submenu, a menu. `ui` encodes (`ui.EncodeMenu`, for packages
that show menus through a module) and finds an item by its number
(`ui.MenuItemAt`); the desktop hosts decode with `ops.DecodeMenu` and
`ops.DecodeMenuBar`, which number the items; the web host reads the
menu bar's shortcuts with the same layout.

**The tray, the Dock menu, background apps** (`tray`, `app`):

- `tray.Show(tray.Props{Image, Symbol, Template, Title, Tooltip, Menu,
  OnClick, ID})`, called in a component, shows an icon in the tray while
  its owner lives: the menu bar on macOS, the notification area on
  Windows, the panel's status icons on Linux. It follows its accessors;
  disposed, the icon goes. `tray.DockMenu(items)` adds items to the app
  icon's menu, the last mounted showing, as `MenuBar`. Neither is a
  node: they talk to the host through a module, `tray`, as other
  packages do (`ui.Call`): `set` sends an icon whole (its strings and its
  menu, only when its encoding changed), `remove` takes it away, `dock`
  sets the Dock menu, and `next`, asked again each time it answers,
  reports a click or a chosen item (an icon's id, 0 for the Dock menu,
  and the item's number). The asking runs on the main thread, a `Call`
  from the last one's answer, while an icon or a Dock menu is mounted;
  the last one going sends `stop`. A host without the module (phones,
  the web) answers with an error, and the package stops asking: there,
  `Show` and `DockMenu` do nothing and `tray.Supported` is false.
- An icon is an `Image` (an asset or a file, drawn 16 points tall) or a
  `Symbol` (an SF Symbol name, drawn as each host draws `ui.Symbol`),
  and a template when it's a Symbol or `Template` says so: black on
  transparent, which the platform colors as its own icons. Without
  `OnClick` a click opens the menu; with it, a click is `OnClick` and the
  menu opens on a secondary click.
- macOS: an `NSStatusItem` (variable length; `ID` its autosave name,
  so it comes back where the user ⌘-dragged it), its button the template
  image and the title in the menu bar's font with monospaced digits;
  without `OnClick` its menu is the item's own, with it the button's
  action opens the menu on a right or Control click (the menu set on the
  item for one `performClick:`). Its menu and the Dock's enable items as
  the app says (no validation) and show no shortcuts. The Dock menu is
  the application delegate's `applicationDockMenu:`, made when the Dock
  asks.
- Windows: `Shell_NotifyIconW` (`NOTIFYICON_VERSION_4`), the icons a
  hidden top-level window's (a message-only window doesn't get
  `TaskbarCreated`, on which every icon is added again). An Image is
  fitted to `SM_CXSMICON` at the system DPI; a Symbol is its Segoe
  Fluent Icons glyph, its gray antialiasing the alpha; a Symbol or a
  Template is white on a dark taskbar and black on a light one
  (`SystemUsesLightTheme`, drawn again on `ImmersiveColorSet`). The
  tooltip is `szTip`; Windows shows no title. `NIN_SELECT` and
  `NIN_KEYSELECT` are `OnClick`, else the menu; `WM_CONTEXTMENU` the
  menu, made by the menu bar's code (submenus, checks, destructive items
  owner-drawn), opened at the event's anchor after the tray window comes
  to the foreground, `WM_NULL` posted after it. `ID` is unused: an
  `NIF_GUID` is tied to the executable's path. The Dock menu is the
  jump list's Tasks (`ICustomDestinationList`): each top-level item a
  shell link to the executable with `--asuka-dock-item=<number>`, titled
  by `System.Title`; separators `IsDestListSeparator` links; submenus,
  disabled items and roles left out (a jump list can't show them); none
  deletes the list; tests and automation and dev runs don't touch the
  user's. The argument is taken out of `os.Args`; a running app (one
  with asuka.json's `id` runs once) is handed `asuka-dock-item:<n>`, and
  reports the item without coming to the front. Submenus in the menu bar
  are `MF_POPUP`s, with access keys per level; an owner-drawn item that
  opens one draws the visual style's arrow (`MENU_POPUPSUBMENU`) and
  clips it out (`ExcludeClipRect`) so the system's classic one doesn't
  draw over it. Quit destroys the main window once the core has
  returned (Lifecycle Background first), which ends the loop and removes
  the icons. Keeping running, `WM_CLOSE` hides the main window.
- Linux: GTK 4 has no tray, and the GTK 3 appindicator libraries can't
  load in a GTK 4 process, so the host speaks their protocol itself
  through GDBus (`internal/gtk/dbusobject.go`: objects exported with one
  vtable of callbacks, made once, routed by user data). Each icon is a
  StatusNotifierItem on a session bus connection of its own
  (`/StatusNotifierItem`), its menu a `com.canonical.dbusmenu` object
  (`/MenuBar`), registered with `org.kde.StatusNotifierWatcher` and again
  when that name gets a new owner; removing it closes its connection,
  whose name going every watcher follows. A Symbol is `IconName`, with
  `IconThemePath` the host's directory of Material symbols; an Image is
  `IconPixmap` (ARGB32, big-endian, at its size and 48 down to 16), a
  Template white while libadwaita's style is dark; the title is
  `XAyatanaLabel` (GNOME's AppIndicator extension shows it). Without
  `OnClick`, `ItemIsMenu` has the panel open the menu on a click; with
  it, `Activate` is the click. The dbusmenu's items have labels (`_`
  doubled), enabled, a checkmark toggle and `children-display` for a
  submenu; a destructive item is set apart by separators, as the
  host's GMenus set it apart; each change bumps the layout's revision
  (LayoutUpdated); a "clicked" Event chooses the item. KDE Plasma, Xfce,
  Cinnamon, MATE, Budgie and GNOME with the AppIndicator extension
  (Ubuntu's default) show it. The Dock menu is the launcher's quicklist:
  a dbusmenu at `/org/asuka/Quicklist`, named by
  `com.canonical.Unity.LauncherEntry.Update` for
  `application://<id>.desktop` (Ubuntu's Dock, Dash to Dock, Plank,
  KDE's task manager). Keeping running holds the `GApplication`; closing
  the main window hides it. The menu bar's submenus are GMenu submenus;
  Quit takes Ctrl+Q and quits as SIGTERM does.
- `app.KeepRunning()` keeps a desktop app running when its last window
  closes: closing the main window hides it, its root mounted, until the
  app shows it again (on macOS also the Dock icon, clicked with no window
  showing: `applicationShouldHandleReopen:hasVisibleWindows:`; on Linux,
  the app launched again, `activate`). `app.RunInBackground()` (and asuka.json's
  `"background": true`, which sets it with `-X` and, on macOS, puts
  `LSUIElement` in the Info.plist so the Dock icon never shows) also
  leaves the main window hidden at launch, and on macOS makes the app an
  accessory (no Dock icon, not in ⌘Tab; a `Window` it opens activates
  it). Windows and Linux have no app without a taskbar button but one
  without a window showing: that's theirs. Automation runs
  (`ASUKA_SNAPSHOT`) still show the main window; on macOS they write
  each tray icon's button beside it (`file.tray.png`) and, with
  `ASUKA_DUMP`, the icons' and the Dock's menus in the dump.

**Flyouts, panels, windows sized to their content, click-through**
(`ui`, `tray`, `apps`; `internal/flyout`):

- Kinds of `Window`, not kinds of their own: `ops.WindowConfig`'s flags
  `WindowFlyout`, `WindowPanel`, `WindowFitContent` and
  `WindowClickThrough` (bits 14 to 17), set before the window opens
  (`WindowProps`' `Flyout`, `Panel`, `FitContent`, `ClickThrough`; the
  main window's `WindowOptions` take all but `Flyout`). A panel also
  sets always on top, skip the taskbar, no minimize or maximize; a
  flyout skip the taskbar and its `Parent`, the window it's declared in.
- A flyout's anchor is the `Window` kind's `Flyout` prop (`ops.Flyout`:
  u32 tray icon, f32 x, y, width, height of a rectangle of its parent's
  content, u8 `Placement`, f32 gap), sent before `Present` and again as
  its accessor changes (placed again). A tray icon is `asuka/sdk/tray`'s id
  for it (the "set" call's), which each host's tray module keys its
  icons by: `tray.Show` returns an `Icon`, whose `Anchor` is that id.
  `internal/flyout.Place` is one placement for the desktop hosts: on
  the side the placement names, flipped to the other side where that
  has no room and the other has, lined up along it (flipped to the other
  end), slid into the work area, shrunk to it, as GTK places menus.
- A flyout and a panel take the keyboard as they show, and close as the
  user's closing does (CloseRequest, else Dismissed; the main window's
  close, which hides it in an app that keeps running) on an Escape the
  content leaves and as they lose the keyboard, unless a sheet, an
  alert or a modal `Window` of theirs shows: a modal `Window` whose
  parent is a flyout or a panel is a window above it, centered on it,
  not a sheet hanging from it, and gives it the keyboard back as it
  closes.
- Fit: a window sized to its content is a layout root the core lays out
  at its content's size, not the host's (`layoutFitWindow`: width auto
  within the least and most width, a second pass at the most width if
  it reaches it, so text wraps there; height auto within the least and
  most height), and sends `WindowAction Fit` (its size, as `Size`'s
  args) in the frames' batch whenever that size changes, once at the
  least as it opens. The host sizes the window in the same crossing: a
  flyout placed again, other windows keeping their top left; a window
  that opens shown with it shows on its first Fit, so never at another
  size first. The environment the host then reports doesn't lay the
  content out again (the content isn't laid out at it). The tab on the
  web isn't fitted.
- Click-through is `WindowAction` ClickThrough and NotClickThrough, and
  the `WindowState` bit 64 (`WindowHandle.ClickThrough`).
- macOS: flyouts and panels are `AsukaPanel`s, `NSPanel`s made
  nonactivating and borderless from the start (the window server learns
  that a window never activates its app only from the style it's made
  with, and forgets it if the style is set again: `configure` leaves
  their style alone; the main window, made before its options, is made
  again as one as `WindowOptions` says so), clear under an
  `NSVisualEffectView` (a menu's material for a flyout, a popover's for
  a panel) unless the app gives a background, the content's layer
  rounded, the window's shadow. They can be key, never main:
  `makeKeyAndOrderFront:` leaves the app the user is in frontmost (NSApp
  reads active meanwhile; the workspace's frontmost application
  doesn't change). A panel joins all Spaces as a full-screen auxiliary,
  out of the window cycle, at the floating level; a tray flyout is at
  the level of menus, under the copy of the status item the user
  clicked (the click's place less its place in the item's window, as
  the window isn't always where the copy shows: one per display's menu
  bar), or the item's window where it's on a display, or the pointer; a
  rectangle's flyout is its parent's child window. They close on
  `cancelOperation:` (the content's views', a field's, the panel's), on
  `windowDidResignKey:` once the key window settles (posted), and on a
  global monitor's mouse-down outside the app's windows (an inactive
  app's windows get those events too). Click-through is
  `ignoresMouseEvents`.
- Windows: flyouts and panels are borderless popup tool windows of a
  class with a drop shadow, topmost, owned by the host's hidden window
  (a rectangle's flyout by its parent), with Windows 11's rounded
  corners; a tray flyout is placed against `Shell_NotifyIconGetRect`
  (the cursor where Windows says nothing), in the monitor's work area at
  its DPI. They take the foreground, close on an Escape the content
  leaves and on `WA_INACTIVE` to a window not their own (checked once
  activation settles, posted); a press on the tray icon leaves a flyout
  to the icon's click, which toggles it, and a click on the parent while
  a flyout has the keyboard only closes it. No system backdrop: every
  view is an opaque child window, which would hide it. Click-through is
  `WS_EX_LAYERED | WS_EX_TRANSPARENT`, opaque.
- Linux: flyouts and panels are undecorated (their header bar hidden),
  take the keyboard with `gtk_window_present` and close on an Escape the
  content leaves (a bubbling key controller on the window) and on
  `notify::is-active` going false, unless a window transient for them
  shows. GTK 4 dropped placing windows and keeping them above, so under
  X11 the host does it through Xlib on the surface's window: a utility
  window out of the taskbar and pager, `_NET_WM_STATE_ABOVE` (and
  `STICKY` for a panel) asked as EWMH says, `XMoveWindow` before it
  maps: a panel on the pointer's display a third down, a flyout against
  its parent's rectangle (`XTranslateCoordinates`) or where its tray
  icon was clicked (StatusNotifierItem's `Activate` x, y, where the
  panel says: KDE Plasma, GNOME's AppIndicator extension), in the work
  area; and with it a window's `X`, `Y`, `SetPosition`, `Center` and
  `AlwaysOnTop`. Under Wayland the compositor places an ordinary window
  and keeps it in the taskbar (an `xdg_popup` needs a parent surface,
  which a tray flyout hasn't); a panel is an overlay layer-shell surface
  where the app runs with gtk4-layer-shell (`LD_PRELOAD`: it must load
  before libwayland-client) and the compositor speaks wlr-layer-shell
  (KDE Plasma, Sway, Hyprland, COSMIC; not GNOME). A tray flyout closed
  by the click on its icon (which took its keyboard) remembers it a
  moment, so that click doesn't open it again. Fit is the content's
  size request and the default size; click-through an empty input
  region (`gdk_surface_set_input_region`), set again after each frame
  while on.
- The web: a flyout and a panel are window panels without a title bar,
  above the others, placed with a port of `Place` (a tray anchor: the
  page's top right), closed on Escape and a press outside them;
  click-through is `pointer-events: none` on the panel, the page under
  it taking the clicks.
- `asuka/sdk/apps` lists the apps installed (module "apps": "list" answers
  JSON, "icon" draws a PNG in the caches and answers its path, "open"),
  sorted and deduplicated in Go; `Search` ranks them in Go. macOS reads
  the application folders (`displayNameAtPath:`, bundle identifiers,
  `iconForFile:`, `openApplicationAtURL:`), Windows the shell's
  Applications folder (`FOLDERID_AppsFolder`, `IShellItemImageFactory`,
  `ShellExecute`), Linux the XDG desktop entries in Go (the theme's
  icons drawn by GTK, `g_app_info_launch_uris_async` with the display's
  launch context). Phones and the web answer `ErrUnsupported`.

**Displays, power, the Dock and the system's appearance** (`screen`,
`power`, `dock`, `appearance`):

- Packages over modules, not ops: what the system says about the
  computer isn't a view's. Each state the app follows is a module with
  "current", "next" and "stop" (`internal/follow`): a goroutine asks
  "next" with the state it last got, which the host answers once its
  state is another (at once for none), and hands the answer to the
  watchers on the main thread; the last watcher's stop sends "stop",
  which answers the waiting "next" with "stopped". Events (`power`'s
  sleep and lock) ride the same answer, after the state: the host
  queues them from the first "next" until "stop", so none is lost
  between two. Each package has `Watch` and `Use` (a signal, stopped
  with its owner); a host reads its state again as the platform says it
  changed, and the desktop ones as each "next" comes too. `host/fake`
  answers later as hosts do (`AsyncModules`), and `follow.Fake` is such
  a module's host side for tests.
- `screen.Displays` (Electron's `screen`): each display's ID, name,
  bounds and work area in points in the desktop's coordinates (the
  origin at the primary display's top left, y down), scale, refresh
  rate and whether it's the primary; `Primary`, `Nearest` and
  `Matching` pick one. Where displays have different scales (Windows),
  each one's origin is its pixels over the least scale of the displays
  and it goes on in its own, so displays never overlap. JSON on the
  wire, the primary first, the same bytes for the same displays (what
  "next" compares).
- `power`: whether there's a battery and the computer runs on it, its
  level and charging, Low Power Mode, the thermal state; `OnSuspend`,
  `OnResume`, `OnLock`, `OnUnlock`; `IdleTime`, since the user's last
  input in any app (`ErrIdleUnknown` where the system doesn't say).
  Keeping the display on stays `device.KeepAwake`, working out of sight
  `asuka/sdk/background`.
- `dock.SetProgress` shows a task's progress on the app's icon (Normal,
  Indeterminate, Paused, Error); `dock.RequestAttention` asks the user
  to come to the app (Informational, Critical), and does nothing while
  the app is in use. The icon's count stays `notify.SetBadge`.
- `appearance`: the system's accent color (0 where it has none),
  Increase Contrast and Reduce Transparency, beside what
  `Environment` carries (dark mode, Reduce Motion, the text size).
- macOS: the displays are `NSScreen`'s (the first has the menu bar;
  AppKit's bottom-left coordinates flipped; `visibleFrame` the work
  area; the CGDirectDisplayID the ID; `localizedName`,
  `maximumFramesPerSecond`), read again at
  `NSApplicationDidChangeScreenParametersNotification`. Power is IOKit's
  power sources (`IOPSCopyPowerSourcesInfo`: the internal battery's
  capacity and charging, what the Mac runs on; followed through
  `IOPSNotificationCreateRunLoopSource`), `NSProcessInfo`'s Low Power
  Mode and thermal state and their notifications, `NSWorkspace`'s
  sleep and wake, the lock screen loginwindow's distributed
  notifications (`com.apple.screenIsLocked`, `screenIsUnlocked`), the
  idle time Quartz's (`CGEventSourceSecondsSinceLastEventType`, any
  input). The progress is drawn on the Dock tile, under its badge: the
  tile's content view is the app's icon with a bar over its bottom, two
  `NSBox`es (an `NSProgressIndicator` doesn't draw in a tile, which
  renders offscreen), the fill the accent, red for an error, yellow
  paused; an indeterminate one is a third of the bar sliding across,
  the tile drawn again 15 times a second. Attention is
  `requestUserAttention:` (critical bounces until the app is active).
  The accent is `controlAccentColor` in sRGB, the options `NSWorkspace`'s
  accessibility display options, followed through
  `NSSystemColorsDidChangeNotification` and
  `NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification`.
- Windows: one hidden top-level window of the modules' class, never
  shown, hears what Windows broadcasts to top-level windows only (a
  message-only window hears none): `WM_DISPLAYCHANGE`,
  `WM_SETTINGCHANGE`, `WM_SYSCOLORCHANGE`,
  `WM_DWMCOLORIZATIONCOLORCHANGED`, `WM_POWERBROADCAST`; the power and
  session notifications are registered for it. Displays are the
  monitors (`EnumDisplayMonitors`, `GetMonitorInfo`), each at its
  effective DPI (`GetDpiForMonitor`), named by the display
  configuration's EDID name ("Built-in Display" for a laptop's panel
  without one, else its adapter's description), at its path's refresh
  rate; read again on a display, setting or DPI change, and once more a
  moment later, as changes settle. Power is `GetSystemPowerStatus` (Low
  Power: battery saver, Windows 11's energy saver; no thermal state),
  read again on `PBT_APMPOWERSTATUSCHANGE` and the power settings
  registered (the power source, the battery's level, the savers). Sleep
  and waking are `PBT_APMSUSPEND` and `PBT_APMRESUMEAUTOMATIC`, which
  `RegisterSuspendResumeNotification` sends on PCs with modern standby
  too, each told once; locking is `WM_WTSSESSION_CHANGE`
  (`WTSRegisterSessionNotification`); the idle time `GetLastInputInfo`'s.
  The progress is the main window's taskbar button's (`ITaskbarList3`,
  paused yellow, error red), set again when Explorer makes the button
  again (`TaskbarButtonCreated`). Attention is `FlashWindowEx` on the
  window in use: once for an informational request (the button stays
  lit until the app is used), the button and the title bar until the
  window comes to the foreground for a critical one. The accent is the
  accent palette's own entry (else DWM's colorization color), a contrast
  theme is Increase Contrast, transparency effects off Reduce
  Transparency.
- Linux: the displays are GDK's monitors (`gdk_display_get_monitors`,
  followed through `items-changed` and each monitor's geometry, scale
  and refresh rate): the geometry is in application pixels, which are
  points, the scale fractional (GTK 4.14), the ID the connector (DP-1),
  the name the monitor's description. GTK 4 has no primary monitor: on
  X11 it's the X server's and the work area the window manager's
  (`gdk_x11_monitor_get_workarea`; a panel moving has no signal of
  GDK's, so it's seen at the next change); on Wayland the compositor
  keeps both, so the work area is the bounds and the primary is GNOME's
  (Mutter's `DisplayConfig`, read again on `MonitorsChanged`), else the
  monitor at the layout's origin. Power is UPower (`OnBattery`, the
  display device's presence, percentage and charging), GIO's power
  profile monitor (the power saver profile, through the portal in a
  sandbox), logind's `PrepareForSleep` for sleep and wake, and the
  session's `LockedHint` with the screen saver's `ActiveChanged`
  (GNOME's, KDE's) for the lock, one locked flag between them; the idle
  time is Mutter's idle monitor's, else KDE's screen saver's, else not
  known; no thermal state. The progress is the launcher API's
  (`com.canonical.Unity.LauncherEntry`, as notify's badge, each
  `Update` the whole entry: count, progress, urgent), which Dash to
  Dock, Ubuntu's dock and Plasma's task manager draw (indeterminate a
  full bar, paused and error as normal); GNOME's own dash shows none.
  Attention sets the entry's urgent flag and, on X11, the window's
  urgency hint; on Wayland, where GTK 4 has no urgency, it presents the
  window without an activation token, which GNOME and KDE don't grant
  but show as a notification that the app is ready, and which can't be
  taken back. It ends as one of the app's windows becomes active. The
  appearance is the settings portal's `org.freedesktop.appearance`
  (`accent-color`, `contrast`, followed by `SettingChanged`),
  libadwaita's high contrast without it; Linux desktops have no Reduce
  Transparency.
- The web: one display, `window.screen`'s (its work area `availLeft`,
  `availTop`, `availWidth`, `availHeight` where the browser gives them,
  its scale `devicePixelRatio`), or every display, with their places,
  labels and which is primary, from the Window Management API
  (`getScreenDetails`, Chrome) once the user has granted it: the host
  never asks. A page isn't told a refresh rate (0). Power is the Battery
  Status API (Chrome), which reports a computer without a battery as
  one charging, full and never discharging: read as none. A page is told
  of no sleep, lock, Low Power Mode or heat, and not the idle time (the
  Idle Detection API asks the user). The accent is CSS's `AccentColor`
  where the browser has it, the options `prefers-contrast: more` and
  `prefers-reduced-transparency: reduce`. A page has no icon: `dock`
  does nothing.
- Phones answer with what they have. The displays are the device's own
  screen first, then any it mirrors or extends to (AirPlay, a cable,
  Android's secondary displays), in a row after it, each in points as
  it's turned now, its work area its bounds (the bars are the app's
  safe area). Power is `UIDevice`'s battery or the sticky battery
  broadcast, Low Power Mode or Battery Saver, the thermal state or
  status; the screen locks as protected data goes (iOS, with a
  passcode) or as it goes off on a secure lock screen (Android), and
  unlocks as protected data comes back or at the user-present broadcast.
  A phone suspends an app as it goes to the background, not as it
  sleeps: no Suspend or Resume, and no idle time. iOS has no system
  accent; Android's is Material You's, as the system's dynamic primary
  (accent1 at tone 40 light, 80 dark), its contrast the contrast level
  (Android 14) or high contrast text, no Reduce Transparency. `dock`
  does nothing.

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
- `asuka/sdk/recent` (module `recent`: `add`, `clear`, `list`):
  NSDocumentController's recent documents on macOS, in an Open Recent
  menu the host fills as it opens (in the File menu once the app
  declares file types, has recent documents or adds one; choosing one
  goes through `openURLs`); on Windows `SHAddToRecentDocs` (the jump
  list's Recent) and the app's own list in its data directory, cleared
  with `IApplicationDestinations::RemoveAllDestinations`; on Linux the
  user's `recently-used.xbel`, through GBookmarkFile, as the app's (its
  application name), clearing only the app's. Phones and the web answer
  nothing.
- `asuka/sdk/login` (module `login`: `set`, `enabled`, `openedAtLogin`):
  SMAppService's `mainAppService` on macOS 13 (a bundle only;
  `requiresApproval` is `ErrNeedsApproval`), the opening Apple Event's
  `keyAELaunchedAsLogInItem` for `openedAtLogin`; Windows the HKCU `Run`
  value (and Task Manager's `StartupApproved` for `enabled`); Linux an
  XDG autostart entry, or in a Flatpak the Background portal's
  `RequestBackground`. The Windows and Linux entries start the app with
  `boot.LoginArg`, which the host takes out of `os.Args` in its init.

**Global shortcuts** (`hotkey`): keys that reach the app while another
app is in front (a launcher's panel, start and stop a recording).

- `hotkey.Register(Props{Shortcut, Description, OnPress})` registers a
  `ui.Shortcut` while the calling component's owner lives; disposed (or
  `Unregister`), the keys are let go. The shortcut follows its accessor:
  the old keys go as the new ones are taken, and a Shortcut without a
  Key registers nothing (a setting the user cleared). `OnPress` runs on
  the main thread at each press; a key held down doesn't repeat it, and
  only the press is reported (Windows tells no release).
- `Status()` (off, pending, active, failed) and `Err()` are signals:
  `ErrTaken` (another app has the keys, or this one already does),
  `ErrNotAllowed` (the user declined, or the desktop won't bind them for
  an app it can't place), `ErrNotAvailable` (phones, the web, a Wayland
  desktop without the portal), `ErrKey` (no such key).
- One module, `hotkey`: `register` (id, modifiers, key, description),
  `unregister`, and a long poll, `next`, answered with the ids pressed
  since; one runs while any shortcut is active, `stop` ends it. Phones and
  the web have no module, which the package reports as `ErrNotAvailable`.
- macOS: Carbon's `RegisterEventHotKey` through purego, as every Mac
  app's global shortcut (no Accessibility permission), registered
  exclusively; the system's own shortcuts (`CopySymbolicHotKeys`) are
  taken. A character is the key that types it on the keyboard's layout
  (`UCKeyTranslate`). Another app's non-exclusive registration of the
  same keys isn't reported: both get the press.
- Windows: `RegisterHotKey` on a message window, `MOD_NOREPEAT`; it fails
  while another app has the keys. The press lets the app take the
  foreground. Keys map as the menu bar's accelerators do.
- Linux, Wayland: apps can't see other apps' keys; the desktop binds
  them through the XDG desktop portal's GlobalShortcuts (GNOME 48, KDE
  Plasma 6, Hyprland). A session binds its shortcuts once, so each change
  binds them all in a new session (named after the app), with their
  descriptions and preferred triggers; the desktop may ask the user to
  confirm or pick other keys, remembers them by the shortcut's id, and
  knows the app by its desktop entry. `Activated` is a press; its
  activation token is the display's while `OnPress` runs, so a window
  it shows takes the focus. Linux, X11: `XGrabKey` on the root window
  (in Caps Lock's and Num Lock's states too), taken while another client
  has the keys; presses come through GDK's `xevent`. Neither (GNOME
  before 48, XWayland alone, whose grabs see only X11 windows' keys):
  `ErrNotAvailable`.

**File dialogs and the file manager** (`dialog`, `shell`):

- `dialog.Open`, `dialog.Save` and `dialog.Folder` give the user's own
  paths, not copies (`device.PickFiles` gives copies, as a phone's
  document picker does): the app reads and writes the files where they
  are. Their options are a title, the button's label, where the dialog
  starts (a folder, or a file in it: selected on macOS and Windows, or
  its name suggested), filters (`dialog.Filter`: MIME types as an
  input's accept takes them, `image/*` a family, or extensions, or
  both), several files or folders, hidden files, and `From`, a view in
  the window the dialog belongs to (default: the one in use). They're module calls (`dialog`:
  `open`, `save`, `folder`, the options as JSON), answered with the
  paths (JSON) or the path once the user has chosen; nothing is a
  cancel.
- macOS: `NSOpenPanel` and `NSSavePanel` as sheets on the window (on
  the sheet it has, if it has one: a sheet on a window with one waits
  for it to close), the title the panel's message (a sheet has no
  title bar), the filters `UTType`s (a Mac's panel has no type menu: it
  takes any of them; an extension no app declares is a dynamic type of
  its own); with no window on screen, a panel of its own. Windows:
  `IFileOpenDialog` and `IFileSaveDialog` (`FOS_PICKFOLDERS` for
  folders), modal to the window, run once the core has returned (a
  modal loop); a filter's types are its extensions and its MIME types'
  (the registry's; a family's are the ones Windows's own apps open),
  and a name typed without an extension gets the chosen filter's.
  Linux: `GtkFileDialog` (`open`, `open_multiple`, `save`,
  `select_folder`, `select_multiple_folders`), modal to the window; in
  a sandbox it's the desktop's file chooser portal, and the paths are
  the document portal's. Each asks before Save replaces a file.
- The web has no paths: a browser gives a page files, never where they
  are. `Open` and `Folder` copy what's picked into the app's caches
  (`/tmp/shared`, `/tmp/folders`: a folder with all it holds), through
  the File System Access API's dialogs (`showOpenFilePicker`,
  `showDirectoryPicker`: Chrome, Edge) or, without them, the file input
  (`webkitdirectory` for a folder). `Save` gives a path in the caches
  (`/tmp/saved/<n>/<name>`) whose file, each time the app has written
  it (closed a file it opened to write, or renamed one over it: fs.js's
  `onWritten`), goes to the file the user chose with
  `showSaveFilePicker`, or is downloaded by its name where the browser
  has no such dialog. A path a dialog gave is where a later one starts
  (`startIn`, the handle behind it).
- Phones: iOS and Android have no such dialogs. `Open` is the document
  picker, through `device.PickFiles` (a phone's host has no `dialog`
  module: its "no module method" answer sends the package there), and
  gives its copies; `Save` and `Folder` are `dialog.ErrUnsupported`.
- `shell.Reveal` shows a file selected in its folder: Finder
  (`activateFileViewerSelectingURLs:`), Explorer
  (`SHOpenFolderAndSelectItems`, on a thread of its own: a window already
  showing the folder is used), the file manager's
  `org.freedesktop.FileManager1.ShowItems` over D-Bus (Files, Dolphin,
  Nemo, Thunar; without one, the folder opens). `shell.Open` opens a
  file in its app and a folder in the file manager (`NSWorkspace`,
  `ShellExecuteEx`, `GtkFileLauncher`; `ErrNoApp` for a type no app
  opens, though Windows asks first, as Explorer does); on the web,
  preview's new tab. `shell.Trash` moves an item to the trash and never
  deletes: `NSFileManager`'s `trashItemAtURL:` (off the main thread),
  GIO's `g_file_trash_async`, which fail on a volume without a trash,
  and on Windows `IFileOperation` with `FOFX_RECYCLEONDELETE` (on a
  thread of its own) and a progress sink whose `PreDeleteItem` refuses a
  delete that wouldn't recycle (too big for the Recycle Bin, a drive
  without one), which Windows would otherwise do without asking.
  `shell.Beep` is `NSBeep`, `MessageBeep(MB_OK)`, `gdk_display_beep`.
  Phones and the web have no file manager: Reveal and Trash are
  `shell.ErrUnsupported` there (Open too on phones: `preview.Show`
  shows a file), Beep nothing.

**Watching files** (`watch`): changes to a file or a directory, and with
`Recursive` the directories in it; every platform, phones and the web
too. MyGo's `WatchFiles`, ported.

- No module and no host: the package calls each system from Go, in
  the app's process (on iOS and Android, the Go library's), without
  cgo. `watch.New(ctx, path, Options)` from any goroutine, `Next` for
  each batch, `Close`; `watch.Path(path accessor, Options, f)` in a
  component: the watch starts on a goroutine, follows its accessor,
  stops with its owner, `f` runs on the main thread (`ui.Post`), and the
  next batch waits until it has; `Err()`, `Watching()` and `Root()` are
  signals.
- A watch is a snapshot and a comparison, not a relay of the system's
  events: it keeps each entry's identity (device and inode; volume and
  file ID on Windows, read when the stat is taken), size, modification
  time and mode, and where the system says something changed (a
  notice: a directory, or an entry in one, with a kind that's only a
  hint), it lists or stats again, once the notices stop for `Debounce`
  (50 ms; never more than `MaxDelay`, 500 ms). The differences are the
  events: `Create`, `Write`, `Remove`, `Rename` (paired by identity when
  exactly one entry has it before and after: any platform, any
  distance within the tree; a directory renamed is one event),
  `Attrib` (the mode, with `Attributes`), and `Overflow` first in a
  batch the system dropped changes for, after which it looked at
  everything. A file replaced at its path by another file (an atomic
  save) is a `Write`, never a removal and a creation, and the new file
  is watched. A file is watched through its directory. The watched
  directory removed, renamed or replaced ends the watch with a
  `Remove` of `.`. The watch doesn't look again until `Next` took the
  last batch, so a slow reader gets one batch of what changed meanwhile;
  notices that come while it looks wait (spilled, up to `MaxEntries`,
  then a look at everything).
- It never leaves the tree: entries are stat'ed and directories
  opened relative to their directory (`os.Root`), symlinks and
  junctions are entries, not followed, and each target the system
  watches is checked to be what the scan saw. What it can't read is
  left out, not an error; `Ignore` (`watch.Patterns`, gitignore-like)
  leaves out entries, and a directory it ignores is never opened.
  `MaxEntries` (65536 on macOS, 8192 elsewhere) bounds a tree.
- macOS: FSEvents through purego, a stream per directory target with
  `kFSEventStreamCreateFlagFileEvents` and `WatchRoot`, on one serial
  dispatch queue, one callback for the process; a stream covers its
  tree, so directories below need none. Dropped events
  (`MustScanSubDirs`, user or kernel drops) are an overflow.
- iOS: kqueue's `EVFILT_VNODE` on an `O_EVTONLY` descriptor per
  directory and file, an `EVFILT_USER` event to wake it (macOS runs it
  too, and the tests run every case through both).
- Linux and Android: inotify, one instance for the process (watch
  descriptors, not instances, are what a user has many of), a watch
  descriptor per directory, `IN_Q_OVERFLOW` an overflow. inotify says a
  directory was removed (`IN_DELETE_SELF`) only once nothing holds it
  open, and the watch does: a directory's watch watches its parent too,
  which says so at once, when it may.
- Windows: `ReadDirectoryChangesW` on a completion port, one handle for
  a recursive tree (its directories stay renamable), its parent's too
  (a directory's own handle says nothing when it's renamed), the
  buffer's overflow (`ERROR_NOTIFY_ENUM_DIR`, or nothing returned) an
  overflow. `FILE_NOTIFY_CHANGE_ATTRIBUTES` only with `Attributes`.
- The web: Go's `os` on js/wasm calls the page's `fs`, fs.js's file
  system in memory, whose `watch` is Node's `fs.watch` (rename and
  change, by name, recursive), told after the call that changed it;
  its entries have inode numbers of their own, kept as they're renamed.
  Under Node (`go test`), Node's `fs.watch`.

**Now Playing and the media keys** (`nowplaying`), on every platform
(phones first): what the app plays in the system's media controls, and
their commands back.

- `nowplaying.Show(Props)` in a component: accessors for the title,
  artist, album, artwork (a source, as `Image`'s), duration, state
  (`Paused`, `Playing`, `Stopped`), position and rate, read in one render
  effect; handlers for play, pause, toggle, stop, next, previous, seek
  (to a position) and the skips (by `SkipInterval`, 15 s). A handler
  enables its command; play, pause and toggle stand in for each other;
  seek needs a duration. Several mounted: the last shows (a stack). The
  last one disposed clears the controls once the flush is over (a posted
  check), so a component replaced by another (a track's, in a `Dynamic`)
  doesn't drop the app out of them in between.
- The position is published with the state and the rate, and the host
  takes its time on receipt, so the system advances it; the package
  sends it again only when it's half a second off where the system has
  it while playing (a seek, a stall), or changes while paused. A
  position signal written from a polled status costs nothing.
- `Props.Player`, an `*audio.Player`: its `Status` is read once a
  second (a timer, `ui.Post`) and fills the state, position and
  duration; play, pause, stop, seek and, with a `SkipInterval`, the
  skips run on it unless their handlers are set, and show at once (the
  status assumed until the next read, which a read already in flight
  can't overwrite).
- One module, `nowPlaying` (internal/nowplayingspec): `set` (the info as
  JSON, with the enabled commands as bits), `clear`, and a long poll,
  `next`, answered with the commands since (u8 command, f64 value each);
  `stop` ends it. Hosts drop commands while no poll follows, or that
  aren't enabled.
- iOS and macOS: `MPNowPlayingInfoCenter` (the playback rate 0 while
  paused; `playbackState` on the Mac) and `MPRemoteCommandCenter`'s
  commands, their targets added once, enabled per `set`; the artwork an
  `MPMediaItemArtwork` of the image loaded off the main thread (on the
  Mac its handler decodes the bytes it holds, so the image goes with the
  artwork). iOS puts the audio session in the playback category; the CLI
  adds the `audio` background mode for an app whose archive has
  `nowplaying.Show` (iosFeatures, `ASUKA_NOWPLAYING`). The Mac's through
  purego (host/darwin), which also routes the keyboard's media keys.
- Android: a platform `MediaSession` (its `PlaybackState` actions the
  commands; the skips also custom actions, which fill the previous and
  next slots of Android 13's media controls), with a `MediaStyle`
  notification; `AsukaMediaService`, a foreground service of the
  `mediaPlayback` type, runs while playing and detaches the notification
  when paused (Android 14's rules; a start the system refuses in the
  background leaves the notification alone). The build declares it and
  its permissions only for apps that import the package.
- Windows: `ISystemMediaTransportControlsInterop::GetForWindow` on the
  app's window (or a hidden one of the host's), its buttons (fast
  forward and rewind the skips) and timeline, delegates that post to
  the main thread; the artwork read off it into a stream. Linux: an MPRIS
  player (`org.mpris.MediaPlayer2` and `.Player`) on a session bus
  connection of its own, its name
  `org.mpris.MediaPlayer2.<app>.instance<pid>`; `Position` extrapolated
  as it's read, `Seeked` on jumps, a new track id when the track
  changes, `Seek` a skip or a seek; `clear` closes the connection, so
  the name goes. The web: the Media Session API (`metadata`,
  `playbackState`, action handlers, `setPositionState`).

**Updates** (`update`, `internal/update`, `app/relaunch.go`,
`cmd/asuka/updates.go`): a desktop app updates itself as Sparkle's do,
from releases `asuka build` signs; MyGo's updater, ported.

- The format (`internal/update`, shared by the app and the CLI): a
  manifest per target (`darwin-universal`, `windows-amd64`,
  `linux-arm64`...), `update-<target>.json`, or a prerelease's channel's
  `update-<target>-beta.json` (a version's channel is its prerelease's
  letters): the version, Markdown notes, the date, the archive's URL,
  size and Ed25519 signature of its SHA-256, the deltas from earlier
  versions and the archives before (for the next build's deltas).
  Versions compare as semantic versioning orders them. An archive is a
  gzipped tar of the app as installed; a delta (Sparkle's kind) lists the
  new app's tree, each file a copy of the old app's (by path or by
  content), a bsdiff patch of it (suffix array by qsufsort), or new
  (DEFLATE), and every file it makes is checked against its size and
  SHA-256, so a delta applied to a changed app fails and the archive is
  downloaded instead.
- The feed (`buildFeed`, `-ldflags -X` with the public key and the
  target, only in `asuka build`'s builds): a static manifest URL (S3, a
  CDN), GitHub's latest release (or, with a tag prefix or a channel, the
  newest tagged release the API lists), or a custom server's URL with
  `{target}`, `{version}` and `{channel}` that answers a manifest or
  204. `update.SetSource` puts code of the app's own in its place.
  Requests are HTTPS (or the loopback), through the environment's
  proxy, retried on 429 and 5xx.
- Where an app is installed (`internal/update.Locate`, pure file
  operations, tested on any system for each layout): a Mac bundle
  (`Contents/MacOS/<exe>`; App Translocation and the Mac App Store's
  receipt refused), a Windows folder (MSIX's `WindowsApps` refused), a
  Linux prefix (`<prefix>/bin/<exe>` with `share/<exe>/`; Flatpak, Snap,
  AppImage, `/usr` refused). Updates are made in `Work`, on the
  install's volume, and put in place by renames.
- macOS: the new bundle is checked (`codesign --verify --deep --strict`,
  the running app's team ID and identifier, quarantine taken off) and
  left to a helper, `/bin/sh` in a session of its own, that waits for
  the app's pid to exit, swaps the bundles (the old one back if the new
  can't be moved in), registers it with Launch Services and, when
  `app.Relaunch` asked (`boot.StagedRelaunch`), opens it with
  `/usr/bin/open`: after the app quits, so the Dock keeps its tile.
- Windows and Linux (`Install.Swap`): the units of the new tree (a
  folder's entries; a prefix's files and links) replace the installed
  ones, those the old version's `.asuka-files` names and the new lacks go,
  what no list names stays (the user's files, other programs of
  `~/.local`); everything moved aside first and put back if one rename
  fails. The running `.exe` is renamed aside (Windows lets it), removed at
  the next launch (`CleanLeftovers`). Linux then runs the desktop's cache
  tools.
- `app.Relaunch` quits as `app.Quit` does and, once the quit goes on,
  starts what starts the app after this process exits: on the Mac a
  detached shell that waits, then `open`s the bundle; elsewhere the
  executable (its path from launch: Linux's `/proc` names the replaced
  file), with `ASUKA_RELAUNCH_WAIT`, which package `app`'s init waits on
  (`kill(pid, 0)`; `OpenProcess` and `WaitForSingleObject`) before the
  host takes the single instance.
- The updater (`update.Window`): its state a signal on the main thread
  (`Use`), checks and downloads in goroutines that `ui.Post` back, a
  timer that wakes at least hourly to compare with the clock (a computer
  asleep stops timers); the window's view follows the phase (a memo, so
  progress doesn't rebuild it), its release notes `asuka/sdk/markdown`, its
  words `internal/l10n`'s. Preferences in `update.json` in the data
  directory.
- The CLI: `asuka update keygen`; `asuka build` writes `dist/updates`
  (the Linux tarball is the archive; a Windows folder and a Linux prefix
  get `.asuka-files`), deltas from the published manifest's versions
  (downloaded and verified); `asuka publish --platform mac|windows|linux`
  uploads to the tag's GitHub release (`gh`) or S3 (SigV4 in Go), the
  manifests last. Phones answer `ErrUnsupported`; the web build writes
  `version.json`, which `update.Check` reads past caches, and `Relaunch`
  reloads the page.

**Documents** (`document`): document-based apps, as NSDocument and
NSDocumentController, SwiftUI's DocumentGroup and MyGo's documents make
them, built on the desktop's own pieces (`ui.Window`, `MenuBar`,
`OnOpenFiles`, `app.OnQuit`, `recent`, `dialog`) rather than ops of
their own.

- `document.New(Options[T]{Types, Read, Write, View, ...})` describes
  one kind of document whose content is a `T`; `Run` runs the app with
  it (`app.RunWindow` with the main window hidden), `Root` is its root
  for an app that runs itself. A `Document[T]`'s content is a signal
  (`Content()`): the view binds it (a text field's `Value`), and an
  effect owned by the document marks it edited as it changes, unless
  `Equal` finds it equal to the file's. Reading the file (open, revert,
  another app's change) sets it without editing. A mutable content says
  `Changed()`, and `Snapshot` copies it before a write.
- Everything is on the main thread. Each document's reads and writes are
  jobs run one at a time: a job starts on the main thread (where it
  takes its snapshot of the content), works on a goroutine, and comes
  back with `ui.Post` before the next starts, so no lock guards a
  document. Questions are callbacks (alerts, dialogs): a close or a quit
  that must ask is refused, then done again once answered.
- Files are written as NSDocument and GIO write them: a sibling file,
  flushed, renamed over the old one (its permissions, owner and group
  kept; on a Mac its extended attributes, Finder's tags; on Windows
  `ReplaceFileW`, which keeps its security, streams and creation time);
  a file with other hard links, or in a folder the user can't write,
  in place. Before writing, the file's stamp (modification time, size)
  is compared with what the document read: a changed file puts it in
  conflict instead.
- Desktop: one `ui.Window` per document (`For` over the open ones), its
  `StateKey` the file's, with the `Edited` and `File` props (the
  Window kind's): a Mac's `setDocumentEdited:` and `setRepresentedURL:`,
  a `*` before the title on Windows, Linux and the web, the file's
  folder as a GNOME title's subtitle; the title is the name, and "name -
  App" on Windows and Linux. `WindowMenuBar` (a `WindowConfig` flag)
  gives the window the app's menu bar where it's per window (Windows'
  HMENU, Linux's main menu). The File menu is the package's `MenuBar`
  (with `Options.Menus` beside it), its items enabled for the document
  in use (the window focused last); two `MenuRole`s are the hosts':
  `MenuRoleClose` closes the window in use as its close button does
  (`performClose:`, `WM_CLOSE`, `gtk_window_close`), and on a Mac
  replaces the host's Close Window; `MenuRoleOpenRecent` is a Mac's
  NSDocumentController submenu, in its place, and elsewhere the item's
  submenu, which the package fills from `recent.List`.
- Autosave (`AutosaveInPlace`, the default): a document with a file it
  can write is saved a moment after the edits stop (5 s, at most 30 s
  apart), when its window loses the focus and when it closes; others
  (untitled, read-only, in conflict, deleted, or `AutosaveOff`) keep a
  recovery copy a second after the edits stop, in `Recovery/<kind>` of
  `app.DataDir`, in a session a lock file says is alive. A launch claims
  the sessions no instance holds and opens their documents, edited.
- Closing: an unedited document closes; an edited one is saved in place
  or asks (`OnCloseRequest` false; the answer `Destroy`s the window).
  Quitting (`app.OnQuit`): documents are saved in place or kept in their
  copies, the quit canceled meanwhile and asked for again; with
  `AskOnQuit`, a Mac asks whether to review them, then each, Windows and
  Linux in one question naming them. The session's end saves at once,
  on the main thread. A Mac's app keeps running with no window
  (`boot.KeepRunning`) and its Dock icon makes a document
  (`boot.Reopen`, which `applicationShouldHandleReopen:` asks); Windows'
  and Linux's quit with their last document.
- Files other apps change: each open file is watched (asuka/sdk/watch's
  watch of it, through its folder, so a file an editor replaces stays
  watched; where there's none, or once its folder went, a poll of its
  stamp every two seconds) and looked at when its window
  comes to the front: an unedited document reads it again, an edited one
  is in conflict; a file gone is looked for by its identity (a Mac:
  anywhere on its volume, `/.vol/<dev>/<ino>` and `F_GETPATH`; elsewhere
  its folder, by inode or Windows' file index), else the document is
  Deleted.
- Phones and the web: one document at a time in the root's `Stack`: a
  list of the app's documents (its documents folder: iOS's container's
  Documents), and a document's screen pushed over it. Every document has
  a file from the start, is saved in place (also as the app goes to the
  background), and closes as its screen goes; files from elsewhere are
  copied in; a crash's copies are written back quietly (beside the file,
  "(Recovered)", where it changed).

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
  are modal `<dialog>`s (the top layer, what's under them inert); one
  declared in a `Window` (its `Window` prop) is centered over that
  Window's panel while it shows, and moves with it;
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
  directory's changes in one IndexedDB transaction). Tabs share it: a
  tab writes holding the directory's Web Lock, then names the files on a
  `BroadcastChannel`, and the others read them again (one a tab changed
  and hasn't written stays its own, written whole: last writer wins,
  never a mix of chunks).
- SQLite: `asuka/sdk/sqlite`'s C (SQLite and `sdk/sqlite/csrc/asq.c`), built to
  WebAssembly by `sdk/sqlite/internal/build` (zig, `wasm32-wasi`, `-Os`) with
  `sdk/sqlite/csrc/asq_wasm.c` as its system (`SQLITE_OS_OTHER`, one thread:
  a VFS, the time zone and randomness imported from JavaScript, no WASI
  imports): the plugin's `wasm` file, which the CLI ships with the apps
  that use it, in `lib/` (as any plugin's, [Plugins](#plugins)), named in
  the page's `libs` option; the page starts the worker when it has
  `asqlite.wasm`. It runs in a worker of the
  page's (`platform/web/src/sqlite-worker.js`; `sqlite-engine.js` the
  module, `sqlite-vfs.js` its files, `sqlite-store.js` where they're
  kept). Go's side (`sdk/sqlite/lib_js.go`) posts each call to the worker
  through `sqlite.js` and waits on a channel; the page writes the answer
  into Go's buffer (one too long is kept page-side for `TAKE`, answered
  at once) and wakes the goroutine. The page's callbacks can't wait (the
  page waits for them), so a call on the goroutine a callback runs on
  fails at once: `host/web` brackets them with `internal/jsloop`, which
  tells that goroutine by its `g` (other goroutines run while the page
  calls back into Go, and may wait). The data directory's databases are
  files of the origin private file system, read and written in place
  through sync access handles; without them, IndexedDB in 64 KiB chunks,
  written once each call returns. One connection per file, a rollback
  journal. Tabs take turns as wa-sqlite's `OPFSCoopSyncVFS` has them: a
  tab uses a database only holding its Web Lock (its handles open), and
  keeps it until another tab asks on a `BroadcastChannel` and it isn't
  in a transaction; a statement that needs it meanwhile gets
  `SQLITE_BUSY` at once while the lock comes (SQLite never sleeps there),
  and Go runs it again (`library.waits` is false). Between the page's own
  connections to a file, the VFS locks as the unix VFS does between
  processes. A database the page's files kept (before) moves to the
  worker as it's first opened.
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

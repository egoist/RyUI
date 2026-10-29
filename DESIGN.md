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
planned exception is rich text, see "Rich text, later"), web views, pixel-identical looks across platforms, stability across a
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
| Threads | Core runs on the platform's main thread | Native callbacks that need answers synchronously (list data sources, text field delegates, measurement, layout inside the keyboard's animation) call straight into the core. Frameworks that run logic and layout off the main thread (React Native, Texture, Litho, NSH) do it so slow or garbage-collected app logic can't drop frames, and pay with asynchronous interop: blank cells, input races, layout a frame late. Fabric added synchronous layout and Flutter merged its UI and platform threads (3.29) for that reason; SwiftUI and Compose stay on main. nui's logic is compiled Go with fine-grained updates, so the main thread suffices; slow work runs on goroutines that post results back. The place off-main work does pay, preparing long, rich or streaming text, is handled without moving the core (see "Rich text, later"). |
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
host/darwin, host/windows, host/linux      later: desktop, in process, Go
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

### Native escape hatch

`ui.Native(kind string, props ui.NativeProps) ui.Node` makes a view of a
kind the app registers with each host (a Swift `NuiViewFactory`, a
Kotlin `NuiViewFactory`). Props and events pass as typed values through
the same ops. Maps, video, camera previews, web views and
platform-only controls go through this.

### Accessibility

Native widgets bring their own. `View` takes `Label`, `Hint`, `Role`
(button, header, image, …) and `Hidden`, which hosts map to UIKit's
accessibility properties and Android's `AccessibilityNodeInfo` (through
`ViewCompat`). Layout-only views (below) never appear in the
accessibility tree.

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
user's state (text, selection, checked, value). Process death loses
state, as in any native app; restoring it is later
(`signal.Persist(key)`).

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
nui build -platform ios|android <dir>   .ipa via xcodebuild, .apk/.aab via Gradle
```

`nui dev` rebuilds the Go library and relaunches on the iOS simulator
(no Xcode project: `swiftc` compiles the embedded host once into one
object, cached across apps; each change rebuilds the archive, links,
ad-hoc signs and reinstalls with `xcrun simctl`, in about a second) or
an Android device or emulator (Gradle builds the shell app once per
host version and app ID, and it's installed once; each change rebuilds
the `.so`, pushes it into the app's private files with `adb` and
`run-as`, where the debug shell app loads it from, and restarts the app:
about a second and a half) or the
Android emulator (`adb`; the AAR is built once, the `.so` is replaced
and the app restarted). It should take a few seconds. Then (M5) a
socket transport carries ops and events between a Go process on the
Mac and a prebuilt player app on the simulator, emulator or a device,
so that a change shows without reinstalling; the synchronous requests
(`Measure`, `ListBind`) become round trips, fast enough over localhost
and USB for development.

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

Desktop adds `host/darwin`, `host/windows`, `host/linux` and
`internal/darwin` later.

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
- `ops`: the Go encoder and the generated Swift and Kotlin decoders
  round-trip the same golden batches (`swift test --package-path
  hosts/ios`; `./gradlew :nui:testDebugUnitTest` in `hosts/android`).
- Hosts: GUI tests on the iOS simulator (XCUITest) and the Android
  emulator (Espresso / UI Automator), in CI, driving `examples/` and
  checking the native views' properties. Until then, debug iOS builds
  take automation commands (tap, type, submit, switch, scroll) from a
  file in the app's container, triggered by the `dev.nui.automation`
  Darwin notification (see `hosts/ios/Sources/NuiHost/Automation.swift`).
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
- **M4, the rest of mobile v0**: gestures (pan, pinch, swipe, with the
  platform recognizers), `ui.Native`, native animations
  (`ui.Animate(func(){…})` makes the frames and props it changes
  animate with `UIView.animate` and `ViewPropertyAnimator`), images
  loaded and decoded off the main thread by the host (with a cache),
  `Activity`.
- **M5, dev without reinstalling**: the socket transport and player
  apps; signals marked `signal.Persist(key)` survive `nui dev` restarts.
- **M6, shipping**: signing, `.ipa` and `.aab` builds, store metadata.
- **Rich text** (when the measurements below say so, or when an app
  needs what platform labels can't do): see "Rich text, later".
- **Then desktop** (below).

## Rich text, later

Platform labels can't express everything a rich text model needs: span
backgrounds shaped per line (radius, box-decoration-break), inline
components that wrap with the line, selection that crosses paragraphs
(markdown, chat transcripts), and cheap updates to long streaming text
(an append re-typesets the whole string on the main thread).
`ui.RichText` will own its text model (spans, inline nodes, line
decorations, a selection model across texts) and its view:

- Typeset with each platform's engine (CoreText or TextKit on Apple,
  `StaticLayout` on Android), drawn by a view of nui's own that draws
  exactly the prepared layout, so measure and draw agree. Selection on
  iOS through `UITextInput` and `UITextInteraction` (UIKit's loupe,
  handles and edit menu, nui's ranges).
- Prepared off the main thread as soon as the content changes (the
  typesetters are thread-safe), cached by content and width, so the
  core's synchronous measure is normally a cache hit; a miss prepares
  on the spot, so correctness never waits on another thread. Color-only
  changes don't re-typeset; appends re-typeset only the changed
  paragraph.
- `ui.Text` stays a platform label: native accessibility, Dynamic Type
  and behavior.

It's gated on numbers, not guesses: before building it, add frame-time
measurements (frames dropped while flinging list10k, and while
streaming a long reply into a Text) and build it when main-thread text
work misses the frame budget, or when an app needs rich text.

## Desktop, later

After M6, on the same core, `ops` and app code:

| Platform | Host | Notes |
|---|---|---|
| macOS | Go, AppKit through purego (`host/darwin`) | No cgo. Port MyGo's `internal/darwin` (Objective-C runtime, `withPool`, classes made at runtime, blocks) and main-thread loop (`loop.go`). |
| Windows | Go, Win32 with common controls v6 through `syscall` (`host/windows`) | Open: WinUI 3 instead (needs the Windows App SDK). `List` there is core-virtualized: no Win32 list hosts arbitrary rows. |
| Linux | Go, GTK 4 through purego (`host/linux`) | libadwaita optional. |

Desktop hosts run in process: `app.Run` locks the main OS thread and
runs the platform's event loop there, and reads the batch's Go structs
without encoding. purego callbacks are made once per signature and
routed by user data, never per view.

Desktop needs what mobile doesn't: `ui.Window` (several windows, title,
size, close requests), the app's menu bar and keyboard shortcuts,
hover and right-click, resizable layouts. To keep that additive, the
core already treats the mobile screen as one root with an environment
(size, insets) that can change at any time, and never assumes a single
root in its own data structures.

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

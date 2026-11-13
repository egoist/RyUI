# 0004: asuka test, the same UI tests on the platforms' real views

- **Status**: Draft
- **Created**: 2026-11-14
- **Related**: #10 (`uitest`), `e2e/`, `internal/remote`

## Summary

`asuka test --platform ios|android|mac|windows|linux|web` runs an app's
`uitest` tests, unchanged, against the platform's real views: on the
simulator, an emulator or a phone, in the app's own window on a
desktop, in headless Chrome. The test stays a Go test on the computer
(on a phone, through the same transport `asuka dev` uses). It finds
things through a shadow of the view tree, as it does on the fake host
today, and asks the platform's host to do what a user does: tap, type,
go back. Without `--platform`, `asuka test` brings the app's generated
code up to date (catalogs, native packages' glue) and runs `go test`.

## Motivation

`uitest` (#10) tests an app's behavior with no device, on the fake
host. What it can't tell you:

- **Text measured by the platform.** The fake host measures text at 8
  points a character, so a label that wraps or truncates on an iPhone,
  or a layout that only overflows in German, passes.
- **The platform's own behavior.** A real `UINavigationController`
  popping, a Material dialog's buttons, a `UICollectionView` asking
  for the rows it shows, the keyboard covering a field: the fake host
  models these, and a model can be wrong.
- **What it looks like.** No screenshot of the failure.

Today the answer is `e2e/`: Asuka's own GUI tests, which build an
example with `asuka dev --once` and drive it by text through each
debug host's automation (a notification on iOS, a broadcast on
Android, the dev server on the web, a file on Linux). That works, but
it's a second harness for Asuka's examples only, its tests can't run
on the fake host, and they can only read the screen as text.

Writing the tests twice, in Go for the fake host and in XCUITest and
Espresso for the platforms, defeats the point of one app in Go. Appium
and similar drivers sit outside the app and are slow. The app's Go code
already runs on the computer while its views run on the device, in
`asuka dev`; a test can run there too.

## Design

### What doesn't change

The test. `uitest.Mount`, the queries (`Expect`, `Get`, `In`), the
actions (`Tap`, `Type`, `Back`) and their rules (only what shows
counts, taps reach the top modal layer) stay as they are. A test runs on
the fake host under `go test`, and on a platform under `asuka test
--platform`. Tests that assert frames to the point, or snapshots with
frames, are fake-host tests (see [Open questions](#open-questions)).

### A tee host: the shadow and the real one

On a platform, `uitest.Mount` mounts on a host that is two:

- **The shadow**: the fake host, as today, fed every batch. Queries
  read it, so finding a view by its text works the same everywhere and
  needs nothing from the platform per query.
- **The real host**: the platform's, which makes the views, measures
  them (so layout is the platform's) and reports events.

Apply goes to both; Measure goes to the real host only (the shadow
keeps the sizes the real host gave); events come from the real host
and go to the core, and the shadow follows them as the fake host's
helpers do today: `Toggled` sets the switch's `On`, `TextChanged` its
field's `Value`, `Popped` removes the screen, `Selected` the selection.
Widget-owned state (a field's text, a switch) is the widget's, so the
shadow must learn it from the events, never assume it.

List cells are the real host's. When the real host asks the core to
bind a cell (`ListBind`), the tee adopts that cell into the shadow
before passing the call on (a new `fake.Host.AdoptCell(list, cell)`),
so the core's ops into the cell apply to both. `uitest`'s own binding
(the rows that fit, `ScrollTo`) is off: the platform decides what
shows, and `ScrollTo` asks it to scroll.

### Actions go through the platform

On the fake host, an action is an event sent to the core. On a real
host it can't be: dispatching `Popped` without the
`UINavigationController` popping, or `TextChanged` without the
`UITextField` holding the text, leaves the platform and the core
disagreeing. So on a platform, `uitest` finds the target in the shadow
(as today) and asks the host to act on that node:

| `uitest` | The host does |
| --- | --- |
| `Tap`, `LongPress` | activates the view: the control's action (`sendActions`, `performClick`, `element.click()`), or its gesture recognizer |
| `Type`, `Submit` | sets the text through the field's input path, as typing does, then return |
| `Toggle`, `Slide`, `Select` | changes the control as a user does, so it reports |
| `Back`, `Dismiss` | the system's back (the navigation controller's pop, `onBackPressed`, Escape), the sheet's dismissal |
| `Swipe`, `Refresh` | the row's swipe action, the refresh control |
| `Key` | the key, to the focused view |

The events come back as the user's would, and both the core and the
shadow follow them. This is the debug hosts' automation (`tap`, `type`,
`back` in `Automation.swift`, `Automation.kt`, `automation.js`,
`host/linux/automation.go`), addressed by node ID instead of by text,
over the connection the test already has instead of a notification or
a broadcast.

### Settling on a real platform

The fake host is idle when nothing is posted. A real one also has
transitions in flight (a push, a sheet presenting), images loading and
the keyboard moving. In test mode the host:

- turns animations off (`UIView.setAnimationsEnabled(false)` and
  `CATransaction`'s duration; Android's animator duration scale, as
  Espresso recommends; the desktop and web hosts' own transitions), so
  a push or a pop is done when the call returns;
- answers an idle query: no transition, no pending layout, the main
  run loop drained. `Settle` asks after the core's own work is done.

What isn't the platform's to finish (a request, a timer) is the app's,
and `WaitFor` waits for it as it does now: on the app's posts, bounded,
never a sleep.

### Per platform

**iOS and Android**: the test binary on the computer, the views in the
app's player, over `internal/remote`. `asuka test --platform ios`
builds the player as `asuka dev` does (the app's native packages and
plugins in it), boots the simulator (or uses the Android device `asuka
dev` would), and runs `go test` with the address the player dials
(`ASUKA_REMOTE`, as for `asuka dev`). The tee's real host is
`remote.Host`. Each `Mount` mounts a new root on the one session;
disposing it destroys its views, and the player's `Proxy` clears
whatever a crashed test binary left when the next one attaches. The
connection gains three methods: perform an action on a node, the idle
query, and a capture of the screen as a PNG.

**macOS, Windows, Linux**: in process. The tee's real host is
`host/darwin`, `host/windows` or `host/linux`, which need the main
thread; `uitest.Main(m)` keeps it, as `host/darwin`'s own tests do
(`TestMain` runs `m.Run` on a goroutine and the main thread serves the
tests' work). `asuka test` adds that `TestMain` to a package that has
none, through the build overlay `asuka dev` already uses; a package
with its own calls `uitest.Main`. On Linux without a display, `asuka
test` runs under `xvfb-run -a dbus-run-session`.

**The web**: the app's Go runs in the browser, so the test does too:
`GOOS=js GOARCH=wasm go test -exec` with a runner that loads the test
binary in headless Chrome beside `platform/web` (the approach
wasmbrowsertest takes), with `host/web` as the real host. The
transport `asuka dev` uses for phones doesn't fit the browser: the
player's calls into the core nest synchronously (a list binding cells
while the core applies a batch), and the page can't block on a socket.

### On failure

A failing check's message has the shadow's screen, as now, and on a
platform a PNG of the window beside it: `testdata/failures/<test>.png`
(the path in the message). `MatchSnapshot` on a platform writes the
capture to `<name>.<platform>.png` and compares nothing in pixels;
pixels are the platform's, and Asuka's conformance tests measure them.

### asuka test

```sh
asuka test [packages] [go test flags]               # generate, then go test: the fake host
asuka test --platform ios [packages] [flags]        # the simulator (--device, as asuka dev)
asuka test --platform android [packages] [flags]    # the device asuka dev picks
asuka test --platform mac|windows|linux [packages]  # this computer, in process
asuka test --platform web [packages]                # headless Chrome (ASUKA_CHROME: its path)
```

Without `--platform` it regenerates what `asuka dev` and `asuka build`
do (the catalogs' functions, native packages' glue) for the packages'
apps, so a test never runs stale generated code, then runs `go test`
with the flags as given. With `--platform` it also passes `-p 1`: one
device, one test binary at a time.

### e2e

Once the platforms run `uitest` tests, `e2e/`'s per-example tests
(counter, notes, chat, navigation, languages) become the examples' own
`uitest` tests, run on every platform by one CI job each. What `e2e/`
keeps is what isn't an app's test: the conformance captures, the
updater, crash reporting.

## Phases

1. `asuka test` without `--platform`: generation and `go test`.
2. The tee host in `uitest`, the shadow following events, and
   `fake.Host.AdoptCell`; tested against the fake host as the real one.
3. iOS: the player's test mode (animations off, idle, capture) and
   actions by node ID; `asuka test --platform ios`; the navigation
   example's tests passing on the simulator.
4. Android, on the same transport.
5. macOS in process with `uitest.Main`, then Windows and Linux.
6. The web, through `go test -exec` in headless Chrome.
7. `e2e/`'s example tests moved to the examples.

## Open questions

- **Real hit testing.** Activating a node by ID taps a button even if
  another view covers it. Tapping its center through the platform's
  event path (`UIApplication.sendEvent`, `MotionEvent`s, CDP's
  `Input.dispatchMouseEvent`) would catch that, at the cost of
  scrolling it into view first and of flakier taps. Start with
  activation, add coordinates where it proves needed?
- **Options a device can't honor.** `Size` on a phone, `Platform(Android)`
  on the simulator. Some can be applied (dark mode and the text size as
  trait overrides, the app's language through `ui.SetLocale`); the rest
  skip the test with the reason, rather than fail it?
- **Which player.** `asuka dev` builds a player per app, with its
  native packages and plugins. A package that isn't an app (a library
  of components) needs one with what it imports: built from the test
  binary's imports?
- **Frames.** Tests that assert exact frames pass on the fake host and
  fail on a device, by design. Should `uitest` say so (a frame read on
  a platform warns), or is the docs' "relative, not to the point"
  enough?
- **Several devices.** `-p 1` serializes. Several simulators (one per
  package) would parallelize, at the cost of booting them.
- **Speed.** A test binary per package, each attaching to the player:
  measure it with the navigation example before promising it for every
  save; it's meant for CI and before merging.

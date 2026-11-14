# 0005: Devtools: debugging, inspecting and profiling an app

- **Status**: Draft
- **Created**: 2026-11-14
- **Related**: 0001, 0002, 0004, `internal/remote`, `asuka vet`,
  `signal`'s `asuka_debug` tag

## Summary

`asuka dev` gets the tools Expo's developers take for granted, built for
an app in Go:

- **An error overlay.** A panic, or a build that fails, shows on the
  device or window over the app, with its stack, the lines in your
  source, and Reload. Today they print in the terminal only.
- **A debugger.** `asuka dev --debug` runs the app's Go code under
  Delve, and editors attach to one address that stays the same across
  reloads, with their breakpoints kept.
- **An inspector.** A page that `asuka dev` serves shows the app's view
  tree, each view's props, style and frame, where in the source it was
  made, and which signals it reads. Pick a view by tapping it on the
  device; the view under the cursor is outlined there.
- **Signals and flushes.** Why a view updated (which write, through
  which memos), and what each flush cost: effects run, ops, measures,
  layout passes, crossings.
- **Network and logs.** The app's HTTP requests and its logs, in
  Chrome DevTools' own Network and Console panels, through the parts of
  the Chrome DevTools Protocol they use.
- **Profiles.** pprof and `runtime/trace` from the running app, with
  the flush's phases as regions, and the frames the platform dropped.

None of it is in a release build: it's compiled in only in development,
behind a build tag, so the hot paths stay as they are.

## Motivation

What a developer has today, against Expo:

| | Expo (React Native) | Asuka today |
|---|---|---|
| Errors | LogBox: a red screen with the stack and the source, warnings as toasts | the terminal; the player or window shows the app as it was, or nothing |
| A build that fails | Metro's error on the device | the terminal; the app keeps running the last build |
| Breakpoints | the React Native DevTools debugger (Hermes, through CDP) | none documented |
| The view tree | React DevTools' components, props, state, the element picker | `ASUKA_DUMP=1` on the desktop: the tree as text, once |
| Why it re-rendered | React DevTools' profiler, "why did this render" | none |
| Network | the DevTools Network panel | none |
| Performance | the perf monitor overlay, the profiler | `bench/` for Asuka itself; the player's goroutine stacks (`mStacks`) when it seems stuck |
| Mistakes that build | the linter | `asuka vet`; `asuka_debug` panics on a write off the main thread |

An app in Go has one thing Expo's don't: in `asuka dev`, on a phone as
on a desktop, the app's Go code is an ordinary native process on the
developer's computer (the player only shows its views). Go's own tools,
Delve, pprof and the execution tracer, work on it as they do on any Go
program. What's missing is the part that knows about Asuka: the views,
the signals, the flush, the player across the socket, and a process
that's replaced on every save.

## Where the Go code runs

What each tool can reach depends on where the app's Go code runs:

| Mode | The Go code | Delve | Everything else |
|---|---|---|---|
| `asuka dev`, a phone (the player) | a process on the computer | ✓ | ✓ |
| `asuka dev`, the Mac, Windows, Linux | the app's process on the computer | ✓ | ✓ |
| `asuka dev`, the web | WebAssembly in the page | ✗: Go emits no DWARF for wasm | ✓ (the page connects back to `asuka dev`) |
| a built debug app on Android (`--no-hot`, native Kotlin) | the app's process on the device | possible, see [Open questions](#open-questions) | ✓, through `adb reverse` |
| a built debug app on iOS | the app's process on the device or simulator | ✗: Delve doesn't run on iOS | ✓, through the simulator's network or the device's tunnel |

So the player mode, which is the default, gets everything, and the
other modes get all but the debugger.

## Design

### The devtools server

A built-in package, `internal/devtools`, linked into the app's Go
process only by `asuka dev` (through a build tag, `asuka_devtools`, and
the overlay `asuka dev` already gives `go build`). It opens a WebSocket
to `asuka dev`, which serves:

- `http://localhost:7463/`: the devtools page (below).
- `ws://localhost:7463/cdp`: a Chrome DevTools Protocol endpoint for the
  domains this RFC implements (Network, Runtime's console, Log), so
  Chrome's own DevTools frontend shows them. `asuka dev` prints the
  `devtools://devtools/bundled/inspector.html?ws=...` link, as React
  Native's new debugger does.
- `ws://localhost:7463/asuka`: Asuka's own protocol, JSON and versioned,
  for the inspector, signals and flushes, so an editor extension can use
  it too.

The app connects to `asuka dev`, not the other way round, so the
address is the same in every mode: the computer's own processes reach
localhost, the page reaches the dev server it came from, a built
Android app reaches it through `adb reverse`, and a built iOS app
through the simulator's network or the device's tunnel (the same way
the player does). Across a reload, `asuka dev` keeps the frontend's
connection and attaches it to the new process, so the inspector, the
network log and Chrome's panels don't need opening again; the network
log and the console mark the reload.

`internal/devtools` never blocks the main thread. It collects on the
main thread into buffers it reuses and sends from a goroutine; the
inspector's questions ("this view's props") reach the main thread with
`ui.Post` and are answered from there.

### The error overlay

Asuka's equivalent of LogBox, drawn by each host with its own views:

- **A panic** that nothing caught (`ui.Catch` still catches the ones it
  owns, as in release) in an event handler, an effect or a `ui.Post`
  callback: in a devtools build, the core recovers it at the dispatch
  boundary instead of letting it end the process, disposes nothing, and
  shows the overlay with the panic's value and the main goroutine's
  stack, Asuka's own frames folded, the app's frames with their source
  lines. Reload starts the app again; Dismiss goes back to the app, as
  it was before the handler ran. A panic on another goroutine still
  ends the process (Go can't recover it from outside); `asuka dev`
  shows that one's stack in the overlay as the next run starts.
- **A build that fails**: today the app keeps running the last build
  and the terminal shows the errors. The overlay shows them too, with
  each error's file and line, and goes away when a build succeeds.
- **Warnings**: what `asuka_debug` panics on today (a signal written off
  the main thread) and the runtime's view of what `asuka vet` finds (a
  signal read where a component builds its views and never again)
  become warnings: a line at the bottom of the screen, which opens the
  overlay.

A source line opens in the developer's editor: the overlay sends
`asuka dev` the file and line, and `asuka dev` runs `$ASUKA_EDITOR` (or
`code -g`, `goland`, `zed` when it finds them).

The overlay is a new pair of ops the hosts implement, `DevError` and
`DevErrorClear`, in `ops/schema.go` with every other op (fake host
first), never in a release build. It isn't built from the app's own
views, so it shows even when the app's tree is the thing that's broken.

### The debugger

`asuka dev --debug` builds the Go code with `-gcflags=all=-N -l` and
runs it under Delve, headless (`dlv exec --headless --api-version=2`),
instead of running it directly. An editor attaches to Delve's DAP
server through `asuka dev`, at one address that doesn't change:

- **Reloads.** Every save starts a new process, with a new Delve. `asuka
  dev` is a DAP proxy: it keeps the editor's connection, remembers the
  breakpoints the editor set (`setBreakpoints`,
  `setFunctionBreakpoints`), sets them in each new process before it
  continues, and tells the editor a new process started. The editor's
  session lasts as long as `asuka dev` does.
- **A paused main thread.** A breakpoint in a handler or an effect
  stops the main thread with the app's Go code. On the desktop that
  freezes the window, as any native debugger does. With the player,
  the core's calls would time out after ten seconds and the player
  would drop the session. While Delve has the process stopped, `asuka
  dev` tells the player (a new message on the dev transport,
  `mPaused`), which stops counting the timeout and dims the app under
  a banner, "Paused in the debugger".
- **Editors.** `asuka dev --debug` prints a VS Code `launch.json` entry
  (`"request": "attach", "mode": "remote"`) and GoLand's settings, and
  `asuka new` can write the first.
- **What runs where.** Delve debugs the Go code; the host's Swift and
  Kotlin are Xcode's and Android Studio's to debug, attached to the
  player as to any app.

### The inspector

The devtools page shows the app's tree as the core has it: each
element, the component that made it, its kind (a `Text`, a `List`, a
native package's view), its props and style as they are now, and the
frame the layout gave it. Selecting an element shows:

- **Where it was made**: the file and line of the `ui.Text(...)` call,
  and the chain of component functions above it. In a devtools build,
  `ui` records the caller's program counter as each element is made
  (one `runtime.Callers` per element, which a release build doesn't
  do), and the page resolves it to a function, a file and a line.
- **Its props and style**, the accessors' current values, and which of
  them follow signals.
- **Its layout**: the frame, padding, margins and gaps, the flex values
  that placed it, and the size its measure returned, with each layout
  value outlined on the device as in a browser's inspector.
- **Its signals**: what each of its effects read last time it ran, with
  the signals' names where `signal.KeepPlaces` knows them (the names
  `asuka dev` already reads from the source to keep values across
  reloads), and their values. A signal's value can be changed from the
  page, which writes it on the main thread.

On the device:

- **Highlight**: the element under the page's cursor is outlined over
  the app, with its frame's size.
- **Pick**: a button in the page (or a shake, or Ctrl-Shift-I on a
  desktop) puts the app in picking mode: the next tap selects the view
  under it in the page instead of reaching the app. The core finds the
  view from its own frames, so the hosts don't hit-test.

Both are ops too, `DevHighlight` and `DevPick`, drawn by each host above
everything, the app's sheets and alerts included.

The devtools page is an Asuka app itself, built for the web host and
served by `asuka dev`, so it needs nothing installed and runs wherever
`asuka dev` runs. The same app runs as a desktop window
(`asuka devtools`) for those who'd rather.

### Signals and flushes

The page's Updates tab records, while it's on:

- **Each flush**: what started it (the signal written, and where:
  the handler's file and line), the memos that recomputed and the ones
  that didn't need to, the effects that ran, the ops emitted (by kind),
  the measures, the layout passes and the views laid out again, and the
  two crossings. Counted, as Asuka's tests count work, with times
  beside the counts.
- **Why a view updated**: from a view's props, the write that caused
  it, through the memos between them.
- **Updates on the device**: views flash briefly as their props change,
  as React DevTools' "highlight updates" does.

The signal graph has what this needs: every link knows its dep and its
sub. Recording it is behind the build tag; with recording off, a
devtools build costs one branch per flush.

### Network

In a devtools build, `internal/devtools` wraps `http.DefaultTransport`
before the app's `init` runs, so every request made through
`http.DefaultClient`, `http.Get` or a client without its own Transport
is recorded: the method, URL, headers, body (up to a size it says),
timing, the response, and the stack that made it. They're shown in
Chrome DevTools' Network panel through CDP's Network domain (request
will be sent, response received, data received, loading finished or
failed, and `Network.getResponseBody`). `devtools.Transport(rt)` wraps a
transport of the app's own, and compiles to `rt` itself in a release.

The hosts' own loads, `ui.Image`'s URLs, a `Video`, a `WebView`'s pages,
aren't Go's: the hosts report `ui.Image`'s to the core in a devtools
build, as one more event, and they show in the same panel marked as the
platform's. A web view's traffic is the web view's own inspector's
(Safari's, Chrome's `chrome://inspect`).

### Logs

`log` and `fmt.Print` still print in the terminal. In a devtools build,
`internal/devtools` also sets `slog`'s default handler to one that keeps
each record's level, attributes and source, and sends it to Chrome's
Console panel (`Runtime.consoleAPICalled`) and the page, where they
filter by level and by package. The host's own logs (Swift's `print`,
logcat's `asuka` tag) go there as well, for a built app.

### Profiles

- **pprof**: the devtools server answers `/debug/pprof/` (CPU, heap,
  goroutines, allocations, blocking), and `asuka dev` prints the
  `go tool pprof` command for each.
- **Traces**: the page records a `runtime/trace` around an interaction.
  In a devtools build, the flush marks its phases as regions (effects,
  props batch, layout, frames batch), so `go tool trace` shows where a
  slow frame's time went, on the main thread and in the goroutines it
  waited on.
- **Dropped frames**: the hosts report the frames the platform dropped
  (`CADisplayLink`'s timestamps on iOS, `FrameMetrics` on Android, the
  compositor's on the desktop, `requestAnimationFrame` on the web)
  beside the flushes that ran in them.
- **A performance overlay**: an optional line over the app: frames a
  second, flushes a second, ops and effects in the last flush, the Go
  heap.

## Milestones

Each is useful on its own, in this order:

1. **The error overlay**: panics and builds that fail, with the source
   and Reload; `DevError` in every host.
2. **The debugger**: `asuka dev --debug`, the DAP proxy that keeps
   breakpoints across reloads, the player's paused state.
3. **The inspector**: the tree, props, style, frames, where each view
   was made, highlight and pick, opening the source in an editor.
4. **Signals and flushes**: the Updates tab and the device's update
   flashes.
5. **Network and logs**: CDP's Network and Console in Chrome DevTools.
6. **Profiles**: pprof, traces with the flush's regions, dropped frames,
   the performance overlay.
7. **Built apps**: the same tools in a built debug app on a device and
   in the browser, over `adb reverse`, the device's tunnel and the dev
   server's socket.

## Testing

As the rest of Asuka: on the fake host, counting work, never timing it.

- The overlay's ops for a panic in a handler, an effect and a
  `ui.Post`; the app as it was after Dismiss; nothing disposed.
- The inspector's tree and props against the fake host's tree, the
  caller's file and line for elements made in a test file.
- A flush's record against the counts the existing tests already
  assert.
- The DAP proxy against a recorded DAP session: breakpoints set again
  in a second process.
- The network log for requests through `httptest`.
- A release build has none of it: a test that builds an example
  without the tag and finds no `internal/devtools` symbol, and the
  existing allocation benchmarks unchanged with the tag off.
- `e2e`: the overlay shows on the simulator, the emulator and in
  headless Chrome after a handler panics.

## Alternatives

- **Chrome DevTools for everything.** React Native's new debugger uses
  CDP for everything, but its Elements panel expects a DOM, and Asuka's
  tree isn't one. Network and Console fit CDP; the tree and the signals
  get their own page.
- **Building on React DevTools' frontend.** Its components tree is
  close to what Asuka would show, but its protocol is React's fibers
  and hooks; adapting signals to it would show them as something
  they're not.
- **A native devtools app only.** A desktop window is nicer than a
  browser tab for some, but needs installing on each system. The page,
  being an Asuka app, can be both.
- **Debugging the app on the device.** Delve doesn't run on iOS, and on
  Android only with a binary pushed into a debuggable app's sandbox.
  The player mode, where the Go code is on the computer, is the
  default, so the debugger starts there.

## Open questions

- **Delve on Android.** Delve supports `linux/arm64`. `asuka dev` could
  push it into a debuggable built app's files with `adb`, start it with
  `run-as`, and forward its port. Worth it for apps whose native code
  only runs in a built app?
- **Recovering panics in dev only.** Recovering at the dispatch boundary
  in development but not in a release means a bug behaves differently
  in the two. React Native does the same (a red screen in development,
  a crash in a release). Should the overlay say so, or should Dismiss
  be left out so the developer always reloads?
- **Requests outside `http.DefaultTransport`.** Libraries that build
  their own transport (gRPC, some SDKs) aren't seen. Is `devtools.
  Transport` enough, or should `asuka dev` rewrite `http.Transport`'s
  `RoundTrip` through the overlay, as it rewrites other code today?
- **Editing styles live.** Changing a style in the inspector and seeing
  it on the device is easy; writing it back to the source isn't, since
  styles are Go expressions. Edits could stay in the session only.
- **The web.** Without DWARF for WebAssembly there's no debugger in the
  browser. Running the app's Go code on the computer and only its views
  in the page, as the player does on a phone, would give the web Delve
  too; that's a second web mode, and its own RFC.

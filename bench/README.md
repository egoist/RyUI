# Benchmarks: nui against SwiftUI and Jetpack Compose

The same app, three times: `nui/` (nui, on iOS and Android), `swiftui/`
(SwiftUI) and `compose/` (Jetpack Compose), each written the way its
framework's own documentation would have it. Each runs one schedule of
scenarios, frame by frame, and a meter reports what each costs.

```sh
go run ./bench/run -ios               # nui and SwiftUI, on a simulator of the benchmarks' own
go run ./bench/run -android           # nui and Compose, on the running emulator or device
go run ./bench/run -ios -android -runs 7
```

The runner builds release builds of each, installs them, runs each
`-runs` times (interleaved, after one run of each to warm up), and
writes the medians to `results/ios.md` and `results/android.md` (and
every run's numbers to the `.json` beside them).

## The scenarios

At set frames after launch (the same frame numbers in every app):

| scenario | what happens | what's reported |
|---|---|---|
| startup | the app launches and shows its first screen | ms from the process's start to the second frame |
| memory-idle | the first screen | memory, and its peak since launch |
| mount | 1,000 rows (a label and a number) appear in a scroll view (not lazy: `ScrollView`/`VStack`, `Column`/`verticalScroll`, `ui.Scroll`) | the main thread's time over the frames it takes |
| memory-mount | with the 1,000 rows | memory, and its peak |
| update-one | one row's number changes, a different row each frame, for 240 frames | the main thread's time a frame: mean, 95th percentile; dropped frames |
| update-all | every row's number changes each frame, for 120 frames (they all show one counter) | the same |
| scroll | a lazy list of 10,000 two-line rows (`List`, `LazyColumn`, `ui.List`) scrolls 8 points a frame, for 300 frames | the same |
| memory-list | after the scroll | memory, and its peak |

State is each framework's idiomatic fine-grained kind: a signal per row
(nui), an `@Observable` per row (SwiftUI's Observation), a
`mutableIntStateOf` per row (Compose); the shared counter is read by
each row, so a change to it updates the rows, not the screen.

## The meter

The same file in every app on a platform (`nui/ios/Meter.swift`,
`nui/android/Meter.kt`, which the Compose app links to):

- **iOS**: a frame's cost is the main thread's busy time between two
  vsyncs: the run loop's time awake, from a `CFRunLoopObserver`'s
  `AfterWaiting` to its `BeforeWaiting`. That holds the framework's
  update, layout and Core Animation's commit (drawing layers' contents
  included). The render server's work isn't counted. A dropped frame is
  a vsync the display link missed. Memory is `phys_footprint`, what iOS
  counts against an app and kills it for, and its peak since launch.
- **Android**: a frame's cost is what `FrameMetrics` count on the main
  thread: input, animation (where nui's frame events and Compose's
  recomposition run), measure and layout, recording the drawing, and
  the sync with the render thread. Each frame's whole time (with the
  render thread and the GPU) is reported too. A dropped frame missed its
  deadline. Memory is the proportional set size, and the resident set's
  high-water mark (`VmHWM`) for the peak.

nui and SwiftUI are driven by a display link (nui's `ui.OnFrame`),
Compose by `withFrameNanos`; each changes its state at the start of the
frame, as an animation would.

## Fairness

- Release builds: Swift `-O`; Go optimized; R8 on Android, not
  debuggable. Both Android apps are compiled ahead of time in full
  (`cmd package compile -m speed`) after installing, so neither runs on
  the interpreter or depends on a profile (Compose ships baseline
  profiles; nui's host doesn't yet).
- iOS runs on a simulator only the benchmarks use (`nui-bench`): UI
  automation turns accessibility on, and with it UIKit does a great deal
  of work for every label it draws (on the simulator the device panel
  used during development had, one frame of `update-all` took 24 s, in
  `AXUIContextDrawingAnnotation`). On Android, the runner warns if an
  accessibility service is on.
- A simulator and an emulator are not devices: the numbers compare the
  frameworks with each other on the same machine; they don't say how
  fast either is on a phone. Emulators' GPUs especially are slow, which
  the frames' whole times and dropped frames on Android mostly show. Run
  on devices for numbers to quote.

## Results

The latest are in `results/`. The first runs, before nui was optimized
for them (iPhone 17 Pro simulator; Pixel emulator, Android 17; Apple
M5):

iOS (main thread, ms; memory, MB):

| | nui | SwiftUI |
|---|---:|---:|
| startup | 315 | 337 |
| mount | 124 | 164 |
| update-one, a frame | 3.43 | 4.80 |
| update-all, a frame | 33.7 | 64.9 |
| scroll, a frame | 1.05 | 1.23 |
| memory with 1,000 rows (peak) | 113 (119) | 92.9 (98.8) |
| memory after the list (peak) | 35.7 (242) | 21.8 (125) |

Android:

| | nui | Compose |
|---|---:|---:|
| startup | 367 | 405 |
| mount | 595 | 201 |
| update-one, a frame | 7.85 | 24.6 |
| update-all, a frame | 32.1 | 121 |
| scroll, a frame | 1.27 | 1.18 |
| memory with 1,000 rows (peak) | 84.5 (174) | 66.5 (155) |
| memory after the list (peak) | 86.6 (188) | 74.2 (179) |

`go test ./ui -bench .` runs the same mount and update scenarios against
the fake host: the core's share alone (signals, effects, ops, layout).

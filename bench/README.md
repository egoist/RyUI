# Benchmarks: RyUI against SwiftUI and Jetpack Compose

The same app, three times: `ryui/` (RyUI, on iOS and Android), `swiftui/`
(SwiftUI) and `compose/` (Jetpack Compose), each written the way its
framework's own documentation would have it. Each runs one schedule of
scenarios, frame by frame, and a meter reports what each costs.

```sh
go run ./bench/run -ios               # ryui and SwiftUI, on a simulator of the benchmarks' own
go run ./bench/run -android           # ryui and Compose, on the running emulator or device
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
(RyUI), an `@Observable` per row (SwiftUI's Observation), a
`mutableIntStateOf` per row (Compose); the shared counter is read by
each row, so a change to it updates the rows, not the screen.

## The meter

The same file in every app on a platform (`ryui/ios/Meter.swift`,
`ryui/android/Meter.kt`, which the Compose app links to):

- **iOS**: a frame's cost is the main thread's busy time between two
  vsyncs: the run loop's time awake, from a `CFRunLoopObserver`'s
  `AfterWaiting` to its `BeforeWaiting`. That holds the framework's
  update, layout and Core Animation's commit (drawing layers' contents
  included). The render server's work isn't counted. A dropped frame is
  a vsync the display link missed. Memory is `phys_footprint`, what iOS
  counts against an app and kills it for, and its peak since launch.
- **Android**: the same: the main thread's busy time between two
  vsyncs, from the looper's message logging, which holds the
  Choreographer's frame (input, animation, where RyUI's frame events and
  Compose's recomposition run, measure and layout, recording the
  drawing) and whatever else runs; a frame that draws nothing (an update
  out of sight) counts too. `FrameMetrics` give each drawn frame's whole
  time (with the render thread and the GPU) and whether it missed its
  deadline. Memory is the proportional set size, and the resident set's
  high-water mark (`VmHWM`) for the peak.

RyUI and SwiftUI are driven by a display link (RyUI's `ui.OnFrame`),
Compose by `withFrameNanos`; each changes its state at the start of the
frame, as an animation would.

## Fairness

- Release builds: Swift `-O`; Go optimized; R8 on Android, not
  debuggable. Both Android apps are compiled ahead of time in full
  (`cmd package compile -m speed`) after installing, so neither runs on
  the interpreter or depends on a profile (Compose ships baseline
  profiles; RyUI's host doesn't yet).
- iOS runs on a simulator only the benchmarks use (`ryui-bench`): UI
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

The latest are in `results/` (iPhone 17 Pro simulator; Pixel emulator,
Android 17; an Apple M5). Main thread ms; memory MB, its peak in
parentheses.

| | RyUI, iOS | SwiftUI | RyUI, Android | Compose |
|---|---:|---:|---:|---:|
| startup | 307 | 356 | 319 | 327 |
| mount, 1,000 rows | 88 | 163 | 539 | 444 |
| update-one, a frame | 2.15 | 4.91 | 2.74 | 26.8 |
| update-all, a frame | 6.39 | 62.4 | 14.6 | 125 |
| scroll, a frame | 1.02 | 1.14 | 4.03 | 4.19 |
| memory, idle | 14.8 | 15.1 | 43.3 | 32.3 |
| memory, the rows | 36.1 (36.7) | 93.2 (99.1) | 72.8 (162) | 66.7 (156) |
| memory, after the list | 35.0 (45.2) | 21.4 (125) | 74.9 (169) | 74.5 (182) |

What changed RyUI, from the first runs:

| | first | now |
|---|---|---|
| iOS startup | 1,269 ms: every host linked WebKit, AVKit, PhotosUI... | 307 ms: the CLI links what the app's Go code calls |
| iOS update-all, a frame | 33.7 ms | 6.39 ms: a Scroll takes rows out of sight out of the view tree (Core Animation drew every label, on screen or not) |
| iOS memory with the rows (peak) | 113 (242) MB | 36.1 (36.7) MB: the same |
| Android mount | 595 ms | about 270 ms on the first meter (539 on this one, which counts every message): texts without AppCompat's font loading, appearances resolved once |
| Android update-all, a frame | 32 ms | 14.6 ms: a Scroll takes rows out of sight out of the tree |
| Android startup | 367 ms | 319 ms: the Go library loads on a thread of its own as the app starts |

Where RyUI still trails: Android's mount (making Views costs more than
Compose's layout nodes) and idle memory there (the Go runtime and its
library, about 11 MB), and iOS's memory after the list (the Go heap's
pages, which the runtime gives back slowly; RyUI frees them when the app
goes to the background or gets a memory warning).

`go test ./ui -bench .` runs the same mount and update scenarios against
the fake host: the core's share alone (signals, effects, ops, layout).

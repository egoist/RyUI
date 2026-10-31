# Concepts

A nui app is Go functions that build its views once, and signals that
hold its state. This page covers how they fit: the root component,
signals and what they update, showing and repeating views, owners and
cleanup, the main thread, and the environment.

## An app

`app.Run` takes the root component and mounts it as the app's screen:

```go
func App() ui.Node {
	return ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12},
		ui.StaticText("Hello", ui.Style{Font: ui.LargeTitle}),
		Greeting("Ada"),
	)
}

func Greeting(name string) ui.Node {
	return ui.StaticText("Hello, " + name)
}

func main() { app.Run(App) }
```

The root fills the screen, under the notch and the home indicator;
`ui.SafeArea` keeps its content clear of them and of the keyboard (see
[Layout and style](layout.md#the-safe-area-and-the-keyboard)). On iOS
and Android the platform starts the app and calls `main`; `app.Run`
mounts the root and returns.

## Components

A component is a Go function that returns a `ui.Node`. It's an ordinary
function: it takes whatever parameters it needs, and calls other
components to make its children.

A component **runs once**. It builds its views and returns; it isn't
called again when state changes. What changes afterwards is only what
read a signal, and only that.

Props that can change take a `signal.Accessor[T]`: a signal, a memo or
a `signal.Func`. A prop that never changes is `ui.Val(v)`, which is sent
once and costs nothing more. A nil accessor is the zero value (`false`,
`""`, 0), so a prop you'd set to false is left out. Event handlers, such
as `OnPress`, are plain funcs.

```go
ui.Button(ui.ButtonProps{
	Title:    ui.Val("Save"),
	Disabled: saving, // a *signal.Signal[bool]
	OnPress:  save,
})
```

## Signals

A signal holds a value. Package `signal` makes them:

- `signal.New(v)` makes a signal of a comparable type. A write equal to
  the current value is skipped.
- `signal.NewFunc(v, equal)` makes one with your own equality, for
  values `==` can't compare (slices, maps, structs holding them). With a
  nil `equal`, every write notifies.
- `Get` reads the value, and subscribes what's reading it: an effect, a
  prop, a memo.
- `Peek` reads it without subscribing. Use it in event handlers, which
  aren't updated anyway.
- `Set` writes it; `Update(f)` sets it to `f` of the current value.

```go
count := signal.New(0)
count.Update(func(n int) int { return n + 1 })

tags := signal.NewFunc([]string{"go"}, nil)
tags.Update(func(t []string) []string { return append(slices.Clone(t), "ios") })
```

Signals are made in components, as local variables, or as package
variables for state the whole app shares.

### Derived values

`signal.Func` makes an accessor that computes its value from others. It
runs each time it's read, and what it reads is tracked by whoever reads
it:

```go
first := signal.New("Ada")
last := signal.New("Lovelace")
full := signal.Func(func() string { return first.Get() + " " + last.Get() })
```

`signal.NewMemo` caches the value: it computes on the first `Get`, and
again only when something it read changed. Use it when the computation
costs something or many things read it:

```go
left := signal.NewMemo(func() int {
	n := 0
	for _, t := range todos.Get() {
		if !t.Done {
			n++
		}
	}
	return n
})
```

`signal.NewMemoFunc` is a memo with your own equality, as `NewFunc` is
for signals. A memo must not write signals: `Set` inside one panics.

## What updates

Each prop given an accessor becomes an effect of its own. When a signal
it read changes, that effect runs again and sends that one prop to the
native view; nothing else runs.

```go
func Score() ui.Node {
	score := signal.New(0)
	return ui.Column(nil,
		// Read once, when Score runs: this text never changes.
		ui.StaticText(fmt.Sprintf("Started at %d", score.Get())),
		// Read in an accessor: this text follows score.
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%d points", score.Get()) })),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Score"),
			OnPress: func() { score.Update(func(n int) int { return n + 1 }) },
		}),
	)
}
```

The first text is the most common mistake: a signal read in the
component's body is read once, as the component runs once. Read it
inside an accessor, a `ui.StyleFunc` or an effect.

A `ui.StyleFunc` is a style that changes. It runs in one effect for its
view; what it returns is compared with the last, and only the props that
differ are sent (layout runs again only if a layout prop changed). See
[Layout and style](layout.md#styles-that-change).

## Effects

`signal.Effect(f)` runs `f`, and again whenever what it read changes:
for work beyond the views, such as saving, logging or starting a
request. Made while a component builds, or in an event handler, it runs
once that's done; otherwise at once.

`signal.OnCleanup(f)` inside an effect runs `f` before the effect runs
again, and when it's disposed. Here a search waits until typing pauses:

```go
signal.Effect(func() {
	q := strings.TrimSpace(query.Get())
	t := time.AfterFunc(300*time.Millisecond, func() {
		ui.Post(func() { results.Set(search(q)) })
	})
	signal.OnCleanup(func() { t.Stop() })
})
```

An effect may write signals: the effects that write dirties run again
in the same flush. If effects keep dirtying each other for 100 passes,
it panics, naming one of them.

`signal.RenderEffect(f)` runs `f` immediately, and again on changes,
before user effects. It's what `ui` uses for props; an app rarely needs
it.

`signal.Untrack(f)` calls `f` without subscribing to what it reads. It
returns `f`'s value: use it to read an accessor that has no `Peek`.

```go
signal.Effect(func() {
	q := query.Get() // runs again when query changes
	who := signal.Untrack(func() string { return full.Get() }) // but not when full does
	fmt.Println(who, "searched for", q)
})
```

`signal.Batch(f)` runs `f` and then the effects its writes dirtied, once.
Event handlers and `ui.Post` already batch their writes, and the views
are updated once the handler returns; `Batch` is for writes made
elsewhere.

## Showing and repeating

`ui.Show` shows a branch while its condition is true, and the other
branch, if given, while it's false:

```go
ui.Show(signedIn,
	func() ui.Node { return ui.StaticText("Welcome back") },
	func() ui.Node { return SignIn(signedIn) },
)

ui.Show(signal.Func(func() bool { return len(items.Get()) == 0 }),
	func() ui.Node { return ui.StaticText("Nothing yet") })
```

A branch is built when it's shown and disposed when it goes: its
effects stop and its views are destroyed. Show rebuilds only when the
condition flips, not each time it's computed.

`ui.For` shows a row per item of a slice, keyed:

```go
ui.For(items, func(it Item) int { return it.ID }, func(item signal.Accessor[Item], index signal.Accessor[int]) ui.Node {
	return ui.Text(signal.Func(func() string {
		return fmt.Sprintf("%d. %s", index.Get()+1, item.Get().Name)
	}))
})
```

A row lives as long as its key is in the slice. A new key builds a row;
a removed one disposes it; a moved item moves its row's views. When an
item with the same key changes, its row isn't rebuilt: its `item` signal
is set, and only what reads it updates. Give each item a stable key (an
ID, not its index).

`ui.Dynamic` shows what a function returns, and rebuilds it all when
anything it read changes:

```go
ui.Dynamic(func() ui.Node {
	switch mode.Get() {
	case "grid":
		return GridView()
	default:
		return ListView()
	}
})
```

Everything the function reads is tracked, including reads in the bodies
of the components it calls, and each change rebuilds the whole subtree.
Prefer `Show` and `For`.

### Long lists

`For` builds every row and keeps them all. For long or unbounded lists
(a feed, thousands of rows), use `ui.List`: the same arguments, backed by
the platform's recycling list, which builds rows only for what's on
screen. See [Components](components.md#lists).

## Owners and cleanup

Every effect and memo belongs to an owner: the one that was current
when it was made. The app's root has one; each `Show` branch, `For` row,
`Dynamic` build, stack screen, tab and sheet has its own, under its
parent's. Disposing an owner stops its effects, disposes the owners
under it, and runs its cleanups, the last registered first.

Owners are disposed when:

- a `Show` switches branches: the old branch's;
- a `For` item's key leaves the slice: its row's;
- a `Dynamic` rebuilds: the previous build's;
- a screen is popped (the back button, a swipe, `Navigator.Pop`): the
  screen's;
- a sheet closes: its content's.

Go's garbage collector frees memory, but it never stops an effect that
is still subscribed: stopping is the owner's job. `signal.OnCleanup` in
a component's body runs when the component's owner is disposed. Use it
to stop what the component started:

```go
func Clock() ui.Node {
	now := signal.New(time.Now().Format("15:04:05"))
	ticker := time.NewTicker(time.Second)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case t := <-ticker.C:
				ui.Post(func() { now.Set(t.Format("15:04:05")) })
			case <-done:
				return
			}
		}
	}()
	signal.OnCleanup(func() {
		ticker.Stop()
		close(done)
	})
	return ui.Text(now, ui.Style{Font: ui.Title.Weighted(ui.Semibold)})
}
```

Shown with `ui.Show(showClock, Clock)`, the ticker stops when the clock
is hidden, and a new one starts when it's shown again.

## The main thread

Components, event handlers, effects and every signal write run on the
platform's main thread. Don't block it: a slow request or a big file
belongs in a goroutine.

A goroutine doesn't touch signals or views itself. It hands a function
to `ui.Post`, which runs it on the main thread, then updates the views:

```go
load := func() {
	loading.Set(true)
	go func() {
		msg := "Done"
		resp, err := http.Get("https://example.com")
		if err != nil {
			msg = err.Error()
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		ui.Post(func() {
			status.Set(msg)
			loading.Set(false)
		})
	}()
}
```

`ui.Call` calls a native module from the main thread, and gives the
result to a callback, on the main thread. `ui.CallWait` is the same from
a goroutine: it waits for the result. Never call `CallWait` on the main
thread, which the result has to come on. See [Native code](native.md).

```go
go func() {
	b, err := ui.CallWait("myModule", "hello", []byte(`{"name":"Ada"}`))
	ui.Post(func() {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set(string(b))
	})
}()
```

## The environment

`ui.UseEnvironment()` returns what the platform says about where a
component shows, as an accessor: read it in an accessor, a `StyleFunc`
or an effect, and it follows rotation, the keyboard, dark mode and the
text size. It's the environment of the screen the component is in (a
stack screen, a tab, a sheet, or the root). Call it while the component
builds.

```go
env := ui.UseEnvironment()
orientation := signal.Func(func() string {
	if e := env.Get(); e.Width > e.Height {
		return "Landscape"
	}
	return "Portrait"
})
```

Its fields:

- `Width`, `Height`: the screen's size, in points. It's the app's
  window, not the device: in split screen (iPad's Split View, Stage
  Manager, Android's split screen and desktop windows) it's the app's
  part, and it changes as the user resizes it.
- `SafeTop`, `SafeRight`, `SafeBottom`, `SafeLeft`: the safe area's
  insets.
- `Keyboard`: how much of the bottom the keyboard covers.
- `Scale`: pixels per point.
- `FontScale`: the user's text size (Dynamic Type, Android's font
  scale); 1 is the default.
- `Dark`: dark mode is on.
- `ReduceMotion`: the user asked for less motion (iOS's Reduce Motion,
  Android's animations off). See [Motion](motion.md).
- `Platform`: `ui.PlatformIOS` or `ui.PlatformAndroid`, the system the
  app runs on (the device's, also in `nui dev`, where the Go code runs
  on the Mac). The platform's views look as they should by themselves;
  use it for what an app draws itself, to follow each platform's
  design, as [`examples/usage`](../examples/usage) draws UIKit's inset
  grouped lists on iOS and Material 3's on Android.
- `WidthClass`, `HeightClass`: how much room the window gives the app
  along each axis, as the platform classes it: `ui.SizeClassCompact`,
  `ui.SizeClassMedium` or `ui.SizeClassExpanded`. Branch on them, not
  on a width of your own, to put a sidebar beside the content or a
  list beside its detail. iOS reports UIKit's size classes, compact or
  regular (as Expanded); Android, Material's window size classes
  (compact under 600 dp wide, medium under 840, expanded; compact under
  480 dp tall, medium under 900). So an iPad mini in portrait, which
  UIKit calls regular, is Expanded on iOS, and a tablet the same size
  is Medium on Android, where Material still shows one pane: each
  follows its platform. macOS and Windows have none of their own, and
  take Material's.
- `Locale`: the user's locale.
- `TimeZone`: the device's time zone, by its IANA name. nui makes it
  Go's `time.Local`, so `time.Now().Format` shows the device's time.

Semantic colors and fonts follow dark mode and the text size on their
own; read the environment for what they don't cover.

```go
env := ui.UseEnvironment()
ui.Show(signal.Func(func() bool { return env.Get().WidthClass == ui.SizeClassExpanded }),
	func() ui.Node { return ui.Row(ui.Style{Grow: 1}, sidebar(), detail()) },
	func() ui.Node { return list() })
```

## Values that persist

Signals live as long as the app runs. `store.Signal(key, def)` makes a
signal whose value is kept across launches: it starts with the kept
value, or `def`, and each value it's set to is saved.

```go
units := store.Signal("units", "metric")
```

It's for small values, such as settings; see [Data](data.md) for the
store and for databases. Under `nui dev`, every signal's value is also
kept across reloads, with no code (see
[Getting started](getting-started.md#run-it)).

# Testing

Your app's interface is Go functions, so `go test` can test it: package
`uitest` mounts your components without a device, a window or a
display, and drives them as a user would, by what the screen shows.

```go
package main

import (
	"testing"

	"github.com/egoist/asuka/uitest"
)

func TestCounter(t *testing.T) {
	a := uitest.Mount(t, Counter)
	a.Tap("Add")
	a.Tap("Add")
	a.Expect("Count: 2")
}
```

```sh
go test ./...
```

It runs anywhere Go does, in CI too, with no simulator or emulator.
For the platforms' real views, see [testing on the
platforms](#testing-on-the-platforms) below.

## How it works

`uitest.Mount` mounts the component on Asuka's fake host
(`host/fake`), which keeps the views the core makes instead of
making the platform's. Everything else is your app as it runs: its
signals and effects, the core's layout, navigation, lists binding
their rows, sheets and alerts. The test's goroutine is the main thread.

The app runs as on an iPhone (iOS, 390 × 844 points) unless you say
otherwise:

```go
uitest.Mount(t, App, uitest.Platform(ui.PlatformAndroid))  // a Pixel's screen
uitest.Mount(t, App, uitest.Platform(ui.PlatformMacOS))    // an 800 × 600 window
uitest.Mount(t, App, uitest.Size(1024, 1366), uitest.Dark())
uitest.Mount(t, App, uitest.Languages("ar"))               // its translation, right to left
uitest.Mount(t, App, uitest.FontScale(1.5))
uitest.Mount(t, App, uitest.Environment(func(e *ui.Environment) { e.ScreenReader = true }))
```

What isn't the platform's is how text is measured: 8 points a
character on 20-point lines, whatever the font. Frames come from the
core's real layout, so where views are relative to each other holds;
their sizes to the point don't.

## What the screen shows

Tests name things by the text a user sees: a text, a button's title, a
field's text or placeholder, an accessibility label, a screen's title,
a tab, a bar button, a toolbar item, an alert's button, an open menu's
item. Text matches exactly.

```go
a.Expect("Inbox", "Message 20")   // fails the test unless each shows
a.ExpectNo("Message 1")           // fails it if any does
if a.Has("Signed in") { ... }
a.Texts()                         // everything that shows, in reading order
a.Get("Add")                      // the view showing it: its Kind, Frame, props
a.All(ops.KindToggle)             // the views of a kind that show
a.Disabled("Save")                // whether it can't be tapped
```

Only what a user could see counts:

- the top screen of each stack (the screens under it don't show);
- the selected tab's page, and every tab's title in the tab bar;
- the sheets, alerts and popovers presented, over the rest;
- a list's rows that fit its height: Asuka binds a list's rows as the
  platform asks for them, and so does the fake host, as a collection
  view would; `ScrollTo` binds further;
- nothing hidden.

When a check fails, the message has the screen: each view that shows,
with its kind, frame and texts, so you can see what was there instead.

## Doing what a user does

Each action settles before it returns: what it changed is laid out,
lists are bound, and what the app's goroutines had posted has run.

```go
a.Tap("Add")                  // a button, a row, a tab, a bar button, an alert's button
a.LongPress("Message")        // a long press, or a context menu
a.Type("Email", "ada@example.com")  // the field with that placeholder or label
a.Submit("Email")             // return in it ("": the field typed into last)
a.Toggle("Notifications")     // the switch beside that label
a.Slide("Volume", 0.8)
a.Select("Week")              // in a segmented control, a picker or a wheel
a.Swipe("Message 3", "Delete")  // a list row's swipe action
a.Refresh("Message 20")       // pull to refresh what shows it
a.ScrollTo("Message 1")       // scroll the lists until one shows it
a.Back()                      // the back gesture, Android's back, Escape
a.Dismiss()                   // a swipe down, a tap outside
a.Key("primary+n")            // a keyboard shortcut: ⌘N on a Mac, Ctrl+N elsewhere
a.OpenURL("myapp://note/3")
```

A tap goes where a finger would. A text in a row taps the row, and a
text in a button taps the button. A view with a menu opens it, and the
next `Tap` chooses from it. While a sheet, an alert or a popover is up,
only it takes taps, and tapping what's under it fails the test.

Controls without text of their own (switches, sliders, fields with no
placeholder) are found by their label as a user reads a form: the
control on the label's line, or the nearest one below it. A view with
an accessibility label (`ui.ViewProps.Label`) names the control in it.

`Back` closes an open menu, or pops the top screen. With no screen to
pop, it dismisses the sheet, alert or popover on top, which
`ui.OnBack` can turn into a question:

```go
a.Type("Message", "On my way")
a.Back()
a.Expect("Discard this reply?")
```

## When a text shows twice

A test fails if what it taps shows in more than one place: a bar
button and a toolbar item both titled Star, a Remove button on every
row. `In` narrows the screen, and the failure says to which part:

```go
a.In(uitest.Toolbar).Tap("Star")        // not the navigation bar's
a.In(uitest.NavigationBar).Tap("Edit")
a.In(uitest.Content).Has("Inbox")        // the screen's own views, not its title
a.In(uitest.NextTo("Ada")).Tap("Remove") // the Remove on Ada's line
a.In(uitest.Under(a.Row("Ada"))).Tap("Remove")  // in the list row showing Ada
```

`NextTo` goes by where things are, so it works where no view holds the
two: a `ui.Row` makes no view of its own.

## Waiting for the app

Code that works in a goroutine (a request, a timer) posts its result
with `ui.Post`; the test runs what was posted when it settles. To wait
for work that isn't done yet:

```go
a.Tap("Refresh")
a.WaitFor("Loaded 3 items")    // until it shows
a.WaitGone("Loading…")         // until it doesn't
a.Until("the upload to finish", func() bool { return uploads.Peek() == 0 })
```

Waiting blocks until the app posts, then checks again. It fails after
10 seconds, or the `uitest.Timeout` you give `Mount`. Nothing in
`uitest` sleeps.

`ui.OnFrame` animations advance a frame at a time with `a.Frames(n)`, at
60 a second. A signal the test writes shows after `a.Settle()`:

```go
items.Set(nil)
a.Settle()
a.Expect("No items")
```

## Platform services and native packages

Asuka's packages that ask the platform (`asuka/sdk/...`: the camera,
location, notifications) call the host, which in a test is the fake
one: with nothing to answer them, they get an error, as on a platform
without the service. `Setup` gives the fake host answers by module
name (`h.Modules`, `h.AsyncModules`) before the app mounts, as
Asuka's own tests do.

A [native package](native.md)'s module is a Go interface, so the
simplest test gives your components the interface and the test its
own implementation:

```go
type fakeBattery struct{ level float64 }

func (b fakeBattery) Level() (float64, error) { return b.level, nil }
func (b fakeBattery) Watch(ctx context.Context, changed func(float64)) error { return nil }

func TestLowBattery(t *testing.T) {
	a := uitest.Mount(t, func() ui.Node { return Status(fakeBattery{0.1}) })
	a.Expect("Battery low")
}
```

`a.Host` is the fake host itself, for what the methods here don't
cover: the ops it was sent, drags, a key to a given view.

## Snapshots

`a.Snapshot()` describes what the screen shows, a view a line: its
kind, frame, texts and state (a switch on, a slider's value, disabled).
`MatchSnapshot` compares it with a file in `testdata`:

```go
a.MatchSnapshot("signed-in")   // testdata/TestSettings/signed-in.txt
```

```
View 0,0 390x844
  Text 16,16 358x20 "Count: 0"
  Button 16,44 358x44 "Add"
  Button 16,96 358x44 "Reset" disabled
```

`go test -uitest.update` writes the files; review the change with
`git diff` and commit them. A snapshot catches a view gone missing, a
change in what the screen says, and a layout that moved.

## Leaks

When the test ends, `uitest` unmounts the app and fails the test if a
view outlives it: an effect or a view made outside the owner that
should have disposed it (see [Concepts](concepts.md)).

## Testing on the platforms

`uitest` tests your app's behavior: what shows, and what happens when
the user acts. How each platform draws it is the platform's, and
Asuka's own tests check that each host draws what the core sends (see
[Development](development.md#tests)). To see your app on the
platforms:

- `asuka dev --platform ios|android|web` runs it on the simulator, an
  emulator or a phone, or in a browser, and reloads it as you save.
- On a desktop, `ASUKA_SNAPSHOT=out.png` writes the window to a PNG
  once it has shown, without taking the focus, then quits
  (`ASUKA_DUMP=1` writes its view tree beside it). On Windows and
  Linux, `ASUKA_SCRIPT` runs steps first (`tap:Add`, `type:hello`):
  see [Development](development.md#snapshots-and-scripts).
- On Linux, under Xvfb (`xvfb-run -a dbus-run-session`), that works
  with no display, in CI.

Tests can't run in parallel (`t.Parallel`) once they mount an app: an
app's core is one per process, as on a device.

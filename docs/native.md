# Native code

An app can bring native code of its own, on every platform: native
views, shown in Go with `ui.Native`, and native modules, whose methods
Go calls with `ui.Call`. The Go side is the same everywhere; each
platform's side is written in its own language: Swift on iOS, Kotlin on
Android, Swift or Go on macOS, Go on Windows and Linux, JavaScript on the
web.

## When you need it

Asuka's components and packages cover what most apps show and use. Write
native code when you need a platform view Asuka doesn't have (a map, a
vendor's SDK view, a control only one platform has), or a platform API
no Asuka package wraps. The [comparison with SwiftUI and Expo](parity.md)
lists what Asuka has.

An app needs native code only for the platforms it runs on, and only
where it uses it. A view of a kind no host registered shows nothing
there (below), so one platform's view can come before the others'.

## Where it lives

An app's native code lives beside its Go:

| Platform | Language | Files | Registered by |
| --- | --- | --- | --- |
| iOS | Swift (UIKit) | `ios/*.swift` | `AsukaAppNatives`, an `@objc` class |
| Android | Kotlin (Android Views) | `android/*.kt` | `dev.asuka.app.AsukaAppNatives`, an `object` |
| macOS | Swift (AppKit) | `macos/*.swift` | `AsukaAppNatives`, an `@objc` class |
| macOS | Go (AppKit through purego's `objc`) | `*.go` built for `darwin && !ios` | package `native`, from an `init` |
| Windows | Go (Win32 through `syscall`) | `*.go` built for `windows` | package `native`, from an `init` |
| Linux | Go (GTK 4 through purego) | `*.go` built for `linux && !android` | package `native`, from an `init` |
| Web | JavaScript (the DOM) | `web/*.js` | `asuka.registerNative`, `asuka.registerModule` |

Only the files directly in `ios/`, `android/` and `macos/` are taken,
not those in directories under them. On the desktop the native
code is part of the app's Go, in its package or any package it imports.
Release builds (`asuka build`) include all of it.

The web's side is covered in [the web](web.md#native-code). The rest of
this page covers the others.

## Native views in Go

`ui.Native` shows a view of a kind the app registers with each host, by
name:

```go
func App() ui.Node {
	count := signal.New(3)
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
		ui.Native("Stepper", ui.NativeProps{
			Props: map[string]any{"value": count, "max": 10},
			On: map[string]func(ui.NativeEvent){
				"changed": func(e ui.NativeEvent) { count.Set(int(e.Int("value"))) },
			},
		}),
		ui.Text(signal.Func(func() string { return fmt.Sprint("Count: ", count.Get()) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Reset"), OnPress: func() { count.Set(0) }}),
	)
}
```

`ui.NativeProps` has three fields:

- `Props`: the view's named values. Each is a `bool`, `int`, `int64`,
  `float32`, `float64` or `string`, or a `signal.Accessor` of a `bool`,
  `int`, `int64`, `float64` or `string` (a signal, a `signal.Func`),
  which sets the prop again each time it changes. Any other type panics.
- `On`: handlers for the view's events, by name.
- `Style`: its style, as any view's.

A handler gets a `ui.NativeEvent`: its `Name`, and its values, read by
key with `Bool`, `Int` (an `int64`), `Float` (a `float64`) and `String`.
Each returns the zero value when the key is missing or holds another
type. `Has` says whether a key is there, `Value` returns a value and
whether it's there, and `Values` returns them all.

The host measures the native view as it measures its own views, and
again each time a prop changes: `sizeThatFits` on iOS, the view's own
measuring on Android, its `Measure` (below) or the platform's measuring
on the desktop. A view with no natural size of its own needs one from
its style (`Height`, `Grow`). In a column, a view stretches to the
column's width unless its style says otherwise (`AlignSelf:
ui.AlignStart` keeps its own).

If no view of the kind is registered, the host logs `no native view
"Stepper" is registered` and shows nothing in its place.

## Native modules

`ui.Call` runs a method of a native module: the host's own, or one the
app registers.

```go
func Call(module, method string, args []byte, done func(result []byte, err error))
func CallWait(module, method string, args []byte) ([]byte, error)
```

- `Call` is for the main thread (an event handler, an effect). `done`
  gets the result, or the error, on the main thread; with a nil `done`
  the result is dropped.
- `CallWait` is for a goroutine: it waits for the result. Never call it
  on the main thread, where the result comes.

The arguments and the result are bytes, whatever the module and your Go
code agree on: JSON is fine. Asuka's packages wrap the built-in modules
the same way: `asuka/secure` calls the `secureStore` module with
`CallWait` and with `Call`.

`Call` fails with an error when the module has no such method (`no
module method battery.level` on iOS and Android, `asuka: battery.level
isn't on macOS yet` on the desktop), when it's called before the app is
mounted, or when the method replies with an error: the Swift error's
`localizedDescription`, the Kotlin exception's message, or the Go
error's `Error()`.

A method gets the call's bytes and a reply, which it calls once; a
second reply is ignored. Each platform's section below shows its
method's type, and where its reply can come from.

Give your modules names of their own. The host's modules (`device`,
`secureStore`, `camera`...) have names Asuka's packages call: on iOS and
Android a module of the same name as one of the host's replaces it; on
the desktop the host's wins.

## iOS: Swift

A native view is a class that conforms to `AsukaNativeView`:

```swift
@MainActor public protocol AsukaNativeView: AnyObject {
    var view: UIView { get }
    func set(_ name: String, _ value: AsukaNativeValue)
}
```

- `view` is the UIKit view shown. The host's own view holds it and
  sizes it to fill.
- `set` gets each prop, by name, as it's set or changes.

A `AsukaNativeValue` is one of `.bool(Bool)`, `.int(Int64)`,
`.float(Double)` and `.string(String)`. Its `bool`, `int`, `float` and
`string` properties read it; `int` and `float` convert from each other,
and a value of another type reads as `false`, `0` or `""`. Go's integer
types come as `.int`, its floats as `.float`.

The factory that makes the view gets a `AsukaNativeContext`; its `emit`
sends an event to the Go handler of that name:

```swift
ctx.emit("changed", ["value": .int(3)])
```

A view with no `On` handlers sends no events; an event with no handler
of its name is dropped.

The host looks for one class as it starts, before your Go code runs,
and calls it to register the app's views and modules: a class named
`AsukaAppNatives` in Objective-C, an `NSObject` conforming to
`AsukaNativeModule`, whose `register` calls
`AsukaRuntime.shared.registerNative(kind) { ctx in ... }` for each view
and `AsukaRuntime.shared.registerModule(name, methods)` for each module:

```swift
@objc(AsukaAppNatives) final class AppNatives: NSObject, AsukaNativeModule {
    @MainActor static func register() {
        AsukaRuntime.shared.registerNative("Stepper") { ctx in Stepper(ctx) }
    }
}
```

`ios/*.swift` is compiled with the iOS host, in the same Swift module:
no import of Asuka's host is needed.

A module's method is a `AsukaModuleMethod`:

```swift
public typealias AsukaModuleMethod = (_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) -> Void
```

It's called on the main thread. Its reply can come from any thread: the
host takes it to the main thread, and Go gets it there. Work that takes
time (the disk, the network) belongs on another thread, replying from
it when it's done. Conform an error to `LocalizedError` for Go to get a
message of yours.

## Android: Kotlin

A native view implements `AsukaNativeView`, in the package `dev.asuka`:

```kotlin
interface AsukaNativeView {
    val view: View
    fun set(name: String, value: Any)
}
```

`set`'s value is a `Boolean`, `Long`, `Double` or `String`: Go's
integer types come as `Long`, its floats as `Double`.

The factory gets a `AsukaNativeContext`. Its `context` is the app's
themed context (the Material 3 theme): make views with it, so they look
as Asuka's do. Its `emit(name, values)` sends an event, with values that
are `Boolean`, `Int` or `Long` (an `Int` in Go), `Float` or `Double` (a
`Float`); anything else is sent as its `toString()`.

The host loads one object by name as it starts, before your Go code
runs: `object AsukaAppNatives` in the package `dev.asuka.app`,
implementing `AsukaNativeModule`. Its `register` gets the `AsukaRuntime`:

```kotlin
package dev.asuka.app

import dev.asuka.AsukaNativeModule
import dev.asuka.AsukaRuntime

object AsukaAppNatives : AsukaNativeModule {
    override fun register(runtime: AsukaRuntime) {
        runtime.registerNative("Stepper") { ctx -> Stepper(ctx) }
    }
}
```

Write the file's `package dev.asuka.app` line as shown: the CLI copies
`android/*.kt` into the app's sources, and the host loads
`dev.asuka.app.AsukaAppNatives` by that name. Release builds keep it
through R8.

A module's method is a `AsukaModuleMethod`, registered with
`runtime.registerModule(name, methods)` (an extension function: `import
dev.asuka.registerModule`):

```kotlin
typealias AsukaModuleMethod = (args: ByteArray, reply: (Result<ByteArray>) -> Unit) -> Unit
```

It's called on the main thread, and its reply can come from any thread,
as on iOS. An exception the method throws is its error.

On both iOS and Android there is one `AsukaAppNatives` an app: it
registers every view and module. The views and modules themselves can
be in other files.

## macOS: Swift

On the Mac, an app's native code can be Swift, as on iOS: use it for an
API only Swift has (a Swift-only framework, a Swift package's types) or
to share what you know from iOS. The names are iOS's; the view is an
`NSView`:

```swift
@MainActor public protocol AsukaNativeView: AnyObject {
    var view: NSView { get }
    func set(_ name: String, _ value: AsukaNativeValue)
    func sizeThatFits(_ size: CGSize) -> CGSize // default: view.fittingSize
    func destroy()                              // default: nothing
}
```

- `view`, `set`, `AsukaNativeValue` and the `AsukaNativeContext`'s `emit`
  are as on iOS.
- `sizeThatFits` is the view's size for what the layout offers
  (`CGFloat.greatestFiniteMagnitude` for either: as wide or tall as it
  likes). AppKit views have no `sizeThatFits` of their own, so by
  default it's the view's `fittingSize`; give one to a view whose size
  depends on its width.
- `destroy` frees what the view holds as the core destroys it.

Register from a class named `AsukaAppNatives` in Objective-C, with
`AsukaRuntime.shared.registerNative` and `registerModule`, exactly as on
iOS; a module's method is the same `AsukaModuleMethod`, called on the
main thread and replying from any thread. A view's action needs an
Objective-C target: make the view an `NSObject` and its action an
`@objc` method (the complete example below does).

How it runs:

- The CLI compiles `macos/*.swift` with Asuka's Swift for it
  (`platform/macos`), in one module, into a library in the app's bundle,
  `Contents/Frameworks/AsukaAppNatives.dylib`. No import of Asuka is
  needed. The host loads it as it starts, before your Go code mounts,
  and calls it through purego: the app's Go still builds with no cgo.
- `asuka dev` and `asuka build` compile it, with Xcode's command line
  tools (`xcode-select --install`; Xcode has them). A release build is
  universal (arm64 and x86_64), and signed with the app. A plain `go run
  .` makes no bundle, so it has no Swift: its views show nothing.
- It targets macOS 11, the apps' minimum. Swift concurrency
  (`async`, actors) needs a runtime macOS 11 doesn't have: when the
  library uses it, the build puts Swift's copy beside it, as Xcode does.
- Swift and Go native code can live in one app. A kind or module
  registered in both is Swift's, which registers last. The host's own
  modules of a name win over either.

## macOS, Windows and Linux: Go

The desktop hosts run the app's Go in their own process, so an app's
native code there can be Go too (on the Mac, Swift is the other way,
above): AppKit through purego's `objc` package on
macOS, Win32 through `syscall` on Windows, GTK 4 through purego on
Linux. None of it needs cgo. It registers its views and modules with
package `native`, from an `init` in a file built only for its platform:

```go
//go:build darwin && !ios

package main

import "github.com/egoist/asuka/native"

func init() {
	native.RegisterView("Stepper", newStepper)
	native.RegisterModule("battery", native.Module{"level": batteryLevel})
}
```

Use these build constraints, or the same as file name suffixes where
they fit (`_windows.go`):

- macOS: `darwin && !ios`. An iOS build has `GOOS=ios`, which satisfies
  `darwin` too.
- Windows: `windows`.
- Linux: `linux && !android`. An Android build has `GOOS=android`, which
  satisfies `linux` too.

`RegisterView`'s factory gets a `native.Context` and returns a
`native.View`:

```go
type View interface {
	Handle() uintptr
	Set(name string, value any)
}
```

- `Handle` is the platform's view: an `NSView` on macOS, a child window
  (its `HWND`) on Windows, a `GtkWidget` on Linux. The host puts it in a
  view of its own, which the core places, and keeps it filling it.
- `Set` gets each prop (`NativeProps.Props`): a `bool`, `int64`,
  `float64` or `string`. Go's integer types come as `int64`, its floats
  as `float64`. It's called once for each prop as the view is made, and
  again as each changes.

The context's `Parent` is the host's view the native view goes in: an
`NSView` on macOS, an `HWND` on Windows, a `GtkWidget` on Linux. A
Windows child window must be made in it (`CreateWindowEx`'s
`hWndParent`, with `WS_CHILD`). On macOS and Linux, leave the view
without a parent: the host adds it (on Linux, taking its floating
reference). The context's `Emit(name, values)` sends an event to the
view's handler of that name: its values are `bool`s, `int`s, `int64`s,
`float32`s, `float64`s or `string`s; others are dropped.

A view can implement more:

- `native.Measurer`, `Measure(width, height float32) (w, h float32)`:
  its natural size, in points, for what the layout offers (`NaN` for
  either: as wide or tall as it likes). A view that isn't one is
  measured by its `fittingSize` on macOS and by GTK's natural size on
  Linux; on Windows nothing measures a child window, so it takes its
  size from its style.
- `native.Destroyer`, `Destroy()`: frees what the view holds when the
  core destroys it. The host destroys its `Handle` after: on macOS it
  removes the `NSView` (release what you made with `new`), on Windows
  the window goes with its parent, on Linux the widget with its parent.
- `native.ParentMessenger` (Windows),
  `ParentMessage(msg uint32, wparam, lparam uintptr) (uintptr, bool)`:
  a child window's controls report to their parent, the host's window,
  with `WM_COMMAND`, `WM_NOTIFY`, `WM_HSCROLL`, `WM_VSCROLL` and
  `WM_CTLCOLOR*`; those go to the view. `handled` false lets the host
  answer.

A module is a `native.Module`, its methods by name:

```go
type Module map[string]func(args []byte, reply func(result []byte, err error))
```

A method is called on the main thread, and replies once, now or later,
on the main thread. Work that takes time belongs on a goroutine, which
replies with `ui.Post(func() { reply(result, err) })`. The host's own
modules of a name win over the app's.

Everything here runs on the main thread, as the host does: the
factories, `Set`, `Measure`, `Destroy`, and the platform's callbacks
(an Objective-C action, a window's message, a GTK signal). Kinds that
start with `asuka.` are Asuka's own (`asuka.map`, `asuka.camera`): name
yours otherwise.

Three things differ from Swift and Kotlin:

- **A callback is Go.** An AppKit control's action needs an object of
  a class of your own (`objc.RegisterClass`); a GTK signal needs a C
  callback (`purego.NewCallback`), and purego's callbacks are never
  freed, so make one and route by the widget; Win32 controls report to
  their parent, which the host passes on (`ParentMessenger`).
- **Libraries are the host's.** The host has loaded AppKit and GTK:
  AppKit's classes are there by name (`objc.GetClass`), and
  `purego.Dlopen` of a library it has (`libgtk-4.so.1`) returns it.
- **Errors are Go errors**: `reply(nil, err)`.

Only the desktop hosts read what package `native` holds. When `asuka
dev` runs an app in the iOS or Android player, its Go runs on your
computer, built for it, with that platform's files: their `init`
registers, and nothing uses it.

## A complete example

A `Stepper` view and a `battery` module, on each platform: iOS's
`UIStepper`, Android's `NumberPicker`, an `NSStepper` on macOS, an
up-down control on Windows, a `GtkSpinButton` on Linux. `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Battery is what the app's battery module reports.
type Battery struct {
	Level    float64 `json:"level"` // 0 to 1
	Charging bool    `json:"charging"`
}

// ReadBattery asks the module; done runs on the main thread.
func ReadBattery(done func(Battery, error)) {
	ui.Call("battery", "level", nil, func(b []byte, err error) {
		var v Battery
		if err == nil {
			err = json.Unmarshal(b, &v)
		}
		done(v, err)
	})
}

func App() ui.Node {
	count := signal.New(3)
	status := signal.New("")
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
		ui.Native("Stepper", ui.NativeProps{
			Props: map[string]any{"value": count, "max": 10},
			On: map[string]func(ui.NativeEvent){
				"changed": func(e ui.NativeEvent) { count.Set(int(e.Int("value"))) },
			},
			Style: ui.Style{AlignSelf: ui.AlignStart}, // its own size, not the column's width
		}),
		ui.Text(signal.Func(func() string { return fmt.Sprint("Count: ", count.Get()) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Battery"), OnPress: func() {
			ReadBattery(func(b Battery, err error) {
				if err != nil {
					status.Set(err.Error())
					return
				}
				status.Set(fmt.Sprintf("%.0f%%, charging: %v", b.Level*100, b.Charging))
			})
		}}),
		ui.Text(status),
	)
}

func main() { app.Run(App) }
```

From a goroutine, the same with `CallWait`, and `ui.Post` back to the
main thread to set a signal:

```go
go func() {
	b, err := ui.CallWait("battery", "level", nil)
	var v Battery
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	ui.Post(func() {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set(fmt.Sprintf("%.0f%%", v.Level*100))
	})
}()
```

### iOS

`ios/Natives.swift`:

```swift
import UIKit

@objc(AsukaAppNatives) final class AppNatives: NSObject, AsukaNativeModule {
    @MainActor static func register() {
        AsukaRuntime.shared.registerNative("Stepper") { ctx in Stepper(ctx) }
        AsukaRuntime.shared.registerModule("battery", [
            "level": { _, reply in
                let device = UIDevice.current
                device.isBatteryMonitoringEnabled = true
                guard device.batteryLevel >= 0 else {
                    reply(.failure(BatteryError()))
                    return
                }
                let info: [String: Any] = [
                    "level": Double(device.batteryLevel),
                    "charging": device.batteryState == .charging || device.batteryState == .full,
                ]
                reply(Result { try JSONSerialization.data(withJSONObject: info) })
            },
        ])
    }
}

struct BatteryError: LocalizedError {
    var errorDescription: String? { "no battery level (the simulator has none)" }
}

/// A UIStepper: props "value" and "max"; event "changed" with "value".
@MainActor final class Stepper: AsukaNativeView {
    let stepper = UIStepper()
    var view: UIView { stepper }
    private var wanted = 0.0

    init(_ ctx: AsukaNativeContext) {
        stepper.addAction(UIAction { [weak self] _ in
            guard let self else { return }
            wanted = stepper.value
            ctx.emit("changed", ["value": .int(Int64(stepper.value))])
        }, for: .valueChanged)
    }

    func set(_ name: String, _ value: AsukaNativeValue) {
        switch name {
        case "value": wanted = value.float
        case "max": stepper.maximumValue = value.float
        default: return
        }
        // Props come in no set order: max may follow value.
        stepper.value = wanted
    }
}
```

### Android

`android/Natives.kt`:

```kotlin
package dev.asuka.app

import android.content.Context
import android.os.BatteryManager
import android.view.View
import android.widget.NumberPicker
import dev.asuka.AsukaModuleMethod
import dev.asuka.AsukaNativeContext
import dev.asuka.AsukaNativeModule
import dev.asuka.AsukaNativeView
import dev.asuka.AsukaRuntime
import dev.asuka.registerModule
import org.json.JSONObject

object AsukaAppNatives : AsukaNativeModule {
    override fun register(runtime: AsukaRuntime) {
        runtime.registerNative("Stepper") { ctx -> Stepper(ctx) }
        val level: AsukaModuleMethod = { _, reply ->
            val bm = runtime.app.getSystemService(Context.BATTERY_SERVICE) as BatteryManager
            val info = JSONObject()
                .put("level", bm.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY) / 100.0)
                .put("charging", bm.isCharging)
            reply(Result.success(info.toString().toByteArray()))
        }
        runtime.registerModule("battery", mapOf("level" to level))
    }
}

/** A NumberPicker: props "value" and "max"; event "changed" with "value". */
class Stepper(ctx: AsukaNativeContext) : AsukaNativeView {
    private val picker = NumberPicker(ctx.context)
    override val view: View get() = picker
    private var wanted = 0

    init {
        picker.setOnValueChangedListener { _, _, v ->
            wanted = v
            ctx.emit("changed", mapOf("value" to v.toLong()))
        }
    }

    override fun set(name: String, value: Any) {
        when (name) {
            "value" -> wanted = (value as Long).toInt()
            "max" -> picker.maxValue = (value as Long).toInt()
            else -> return
        }
        // Props come in no set order: max may follow value.
        picker.value = wanted
    }
}
```

### macOS in Swift

`macos/Natives.swift`. The stepper is its own action's target, so it's
an `NSObject`; the battery comes from IOKit's power sources.

```swift
import AppKit
import IOKit.ps

@objc(AsukaAppNatives) final class AppNatives: NSObject, AsukaNativeModule {
    @MainActor static func register() {
        AsukaRuntime.shared.registerNative("Stepper") { ctx in Stepper(ctx) }
        AsukaRuntime.shared.registerModule("battery", [
            "level": { _, reply in
                let info = IOPSCopyPowerSourcesInfo().takeRetainedValue()
                let sources = IOPSCopyPowerSourcesList(info).takeRetainedValue() as [CFTypeRef]
                for source in sources {
                    guard let d = IOPSGetPowerSourceDescription(info, source)?.takeUnretainedValue() as? [String: Any],
                          d[kIOPSTypeKey] as? String == kIOPSInternalBatteryType,
                          let now = d[kIOPSCurrentCapacityKey] as? Int,
                          let max = d[kIOPSMaxCapacityKey] as? Int, max > 0
                    else { continue }
                    let battery: [String: Any] = [
                        "level": Double(now) / Double(max),
                        "charging": d[kIOPSIsChargingKey] as? Bool ?? false,
                    ]
                    reply(Result { try JSONSerialization.data(withJSONObject: battery) })
                    return
                }
                reply(.failure(BatteryError()))
            },
        ])
    }
}

struct BatteryError: LocalizedError {
    var errorDescription: String? { "this Mac has no battery" }
}

/// An NSStepper: props "value" and "max"; event "changed" with "value".
@MainActor final class Stepper: NSObject, AsukaNativeView {
    let stepper = NSStepper()
    var view: NSView { stepper }
    private let ctx: AsukaNativeContext
    private var wanted = 0.0

    init(_ ctx: AsukaNativeContext) {
        self.ctx = ctx
        super.init()
        stepper.target = self
        stepper.action = #selector(changed)
    }

    // The control's action: the user stepped it.
    @objc private func changed() {
        wanted = stepper.doubleValue
        ctx.emit("changed", ["value": .int(Int64(stepper.integerValue))])
    }

    func set(_ name: String, _ value: AsukaNativeValue) {
        switch name {
        case "value": wanted = value.float
        case "max": stepper.maxValue = value.float
        default: return
        }
        // Props come in no set order: max may follow value.
        stepper.doubleValue = wanted
    }
}
```

### macOS in Go

The same in Go instead, `natives_darwin.go`. The steppers share one
target, an object of a class the app registers, whose `changed:` finds
the stepper by its sender. The battery comes from IOKit's power
sources, whose dictionaries are `NSDictionary`s.

```go
//go:build darwin && !ios

package main

import (
	"encoding/json"
	"errors"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/asuka/native"
)

func init() {
	native.RegisterView("Stepper", newStepper)
	native.RegisterModule("battery", native.Module{"level": batteryLevel})
}

var sel = objc.RegisterName

// stepper is an NSStepper: props "value" and "max"; event "changed" with
// "value".
type stepper struct {
	ctx    native.Context
	view   objc.ID
	wanted int64
}

// steppers are the steppers shown, by their NSStepper: the action's
// sender.
var steppers = map[objc.ID]*stepper{}

// target is the steppers' target: an object of a class of the app's,
// whose changed: is their action.
var target objc.ID

func newStepper(ctx native.Context) native.View {
	if target == 0 {
		cls, err := objc.RegisterClass("AppStepperTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{
			Cmd: sel("changed:"),
			Fn: func(self objc.ID, cmd objc.SEL, sender objc.ID) {
				if s := steppers[sender]; s != nil {
					s.wanted = objc.Send[int64](sender, sel("integerValue"))
					s.ctx.Emit("changed", map[string]any{"value": s.wanted})
				}
			},
		}})
		if err != nil {
			panic(err)
		}
		target = objc.ID(cls).Send(sel("new"))
	}
	v := objc.ID(objc.GetClass("NSStepper")).Send(sel("new"))
	v.Send(sel("setTarget:"), target)
	v.Send(sel("setAction:"), sel("changed:"))
	s := &stepper{ctx: ctx, view: v}
	steppers[v] = s
	return s
}

func (s *stepper) Handle() uintptr { return uintptr(s.view) }

func (s *stepper) Set(name string, value any) {
	n, _ := value.(int64)
	switch name {
	case "value":
		s.wanted = n
	case "max":
		s.view.Send(sel("setMaxValue:"), float64(n))
	default:
		return
	}
	// Props come in no set order: max may follow value.
	s.view.Send(sel("setIntegerValue:"), s.wanted)
}

// Destroy lets the NSStepper go: new made it, its superview holds it
// until the host removes it.
func (s *stepper) Destroy() {
	delete(steppers, s.view)
	s.view.Send(sel("release"))
}

// IOKit's power sources, and CoreFoundation's CFRelease.
var (
	copyPowerSourcesInfo func() uintptr
	copyPowerSourcesList func(blob uintptr) uintptr
	powerSourceInfo      func(blob, source uintptr) uintptr
	cfRelease            func(uintptr)
)

func batteryLevel(_ []byte, reply func([]byte, error)) {
	if copyPowerSourcesInfo == nil {
		iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			reply(nil, err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			reply(nil, err)
			return
		}
		purego.RegisterLibFunc(&copyPowerSourcesInfo, iokit, "IOPSCopyPowerSourcesInfo")
		purego.RegisterLibFunc(&copyPowerSourcesList, iokit, "IOPSCopyPowerSourcesList")
		purego.RegisterLibFunc(&powerSourceInfo, iokit, "IOPSGetPowerSourceDescription")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	}
	blob := copyPowerSourcesInfo()
	defer cfRelease(blob)
	list := objc.ID(copyPowerSourcesList(blob)) // a CFArray: an NSArray
	defer cfRelease(uintptr(list))
	for i := range objc.Send[int](list, sel("count")) {
		info := objc.ID(powerSourceInfo(blob, uintptr(list.Send(sel("objectAtIndex:"), i)))) // an NSDictionary
		get := func(key string) objc.ID {
			k := objc.ID(objc.GetClass("NSString")).Send(sel("alloc")).Send(sel("initWithUTF8String:"), key)
			defer k.Send(sel("release"))
			return info.Send(sel("objectForKey:"), k)
		}
		cur, max := get("Current Capacity"), get("Max Capacity")
		if cur == 0 || max == 0 {
			continue
		}
		level := objc.Send[float64](cur, sel("doubleValue")) / objc.Send[float64](max, sel("doubleValue"))
		charging := objc.Send[bool](get("Is Charging"), sel("boolValue"))
		reply(json.Marshal(map[string]any{"level": level, "charging": charging}))
		return
	}
	reply(nil, errors.New("this Mac has no battery"))
}
```

### Windows

`natives_windows.go`. The up-down control reports a change to its
parent with `WM_VSCROLL`, which the host passes to `ParentMessage`.
Nothing measures a child window, so it has a `Measure`.

```go
//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"syscall"
	"unsafe"

	"github.com/egoist/asuka/native"
)

func init() {
	native.RegisterView("Stepper", newStepper)
	native.RegisterModule("battery", native.Module{"level": batteryLevel})
}

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	createWindowEx       = user32.NewProc("CreateWindowExW")
	sendMessage          = user32.NewProc("SendMessageW")
	getSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
)

const (
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wmVScroll     = 0x0115
	sbThumbPos    = 4
	udmSetRange32 = 0x0400 + 111
	udmSetPos32   = 0x0400 + 113
	udmGetPos32   = 0x0400 + 114
)

// stepper is an up-down control: props "value" and "max"; event
// "changed" with "value".
type stepper struct {
	ctx    native.Context
	hwnd   uintptr
	wanted int64
	max    int64
}

func newStepper(ctx native.Context) native.View {
	class, _ := syscall.UTF16PtrFromString("msctls_updown32")
	// A child window is made in its parent: the context's.
	hwnd, _, _ := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0,
		wsChild|wsVisible, 0, 0, 0, 0, ctx.Parent(), 0, 0, 0)
	return &stepper{ctx: ctx, hwnd: hwnd, max: 100}
}

func (s *stepper) Handle() uintptr { return s.hwnd }

func (s *stepper) Set(name string, value any) {
	n, _ := value.(int64)
	switch name {
	case "value":
		s.wanted = n
	case "max":
		s.max = n
	default:
		return
	}
	// Props come in no set order: max may follow value.
	sendMessage.Call(s.hwnd, udmSetRange32, 0, uintptr(s.max))
	sendMessage.Call(s.hwnd, udmSetPos32, 0, uintptr(s.wanted))
}

// Measure is the stepper's size, in points: a child window has none the
// host can ask for.
func (s *stepper) Measure(w, h float32) (float32, float32) { return 20, 32 }

// ParentMessage hears what the up-down tells its parent: WM_VSCROLL, as
// its value changes.
func (s *stepper) ParentMessage(msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	if msg != wmVScroll || lparam != s.hwnd || wparam&0xffff != sbThumbPos {
		return 0, false
	}
	pos, _, _ := sendMessage.Call(s.hwnd, udmGetPos32, 0, 0)
	s.ctx.Emit("changed", map[string]any{"value": int64(int32(pos))})
	return 0, true
}

func batteryLevel(_ []byte, reply func([]byte, error)) {
	var st struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag byte
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	if r, _, err := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		reply(nil, err)
		return
	}
	if st.BatteryFlag&128 != 0 || st.BatteryLifePercent > 100 { // no battery, or unknown
		reply(nil, errors.New("this PC has no battery"))
		return
	}
	reply(json.Marshal(map[string]any{
		"level":    float64(st.BatteryLifePercent) / 100,
		"charging": st.BatteryFlag&8 != 0,
	}))
}
```

### Linux

`natives_linux.go`. One C callback serves every spin button. GTK emits
`value-changed` for a value set in code too, so the view ignores its own.

```go
//go:build linux && !android

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ebitengine/purego"
	"github.com/egoist/asuka/native"
)

func init() {
	native.RegisterView("Stepper", newStepper)
	native.RegisterModule("battery", native.Module{"level": batteryLevel})
}

// GTK 4 and GObject, which the host has loaded.
var (
	spinButtonNew   func(min, max, step float64) uintptr
	spinButtonSet   func(spin uintptr, value float64)
	spinButtonGet   func(spin uintptr) int32
	spinButtonRange func(spin uintptr, min, max float64)
	signalConnect   func(instance uintptr, signal string, handler, data, destroy uintptr, flags uint32) uint64
	onValueChanged  uintptr // the callback every stepper's value-changed calls
	steppers        = map[uintptr]*stepper{}
)

func loadGTK() {
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	gobject, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&spinButtonNew, gtk, "gtk_spin_button_new_with_range")
	purego.RegisterLibFunc(&spinButtonSet, gtk, "gtk_spin_button_set_value")
	purego.RegisterLibFunc(&spinButtonGet, gtk, "gtk_spin_button_get_value_as_int")
	purego.RegisterLibFunc(&spinButtonRange, gtk, "gtk_spin_button_set_range")
	purego.RegisterLibFunc(&signalConnect, gobject, "g_signal_connect_data")
	// One callback for all: purego's callbacks are never freed.
	onValueChanged = purego.NewCallback(func(spin, data uintptr) uintptr {
		if s := steppers[spin]; s != nil && !s.setting {
			s.ctx.Emit("changed", map[string]any{"value": int64(spinButtonGet(spin))})
		}
		return 0
	})
}

// stepper is a GtkSpinButton: props "value" and "max"; event "changed"
// with "value".
type stepper struct {
	ctx     native.Context
	spin    uintptr
	wanted  int64
	max     int64
	setting bool // GTK emits value-changed for a value set in code too
}

func newStepper(ctx native.Context) native.View {
	if spinButtonNew == nil {
		loadGTK()
	}
	s := &stepper{ctx: ctx, spin: spinButtonNew(0, 100, 1), max: 100}
	signalConnect(s.spin, "value-changed", onValueChanged, 0, 0, 0)
	steppers[s.spin] = s
	return s
}

func (s *stepper) Handle() uintptr { return s.spin }

func (s *stepper) Set(name string, value any) {
	n, _ := value.(int64)
	switch name {
	case "value":
		s.wanted = n
	case "max":
		s.max = n
	default:
		return
	}
	// Props come in no set order: max may follow value.
	s.setting = true
	spinButtonRange(s.spin, 0, float64(s.max))
	spinButtonSet(s.spin, float64(s.wanted))
	s.setting = false
}

// Destroy forgets the stepper; the host destroys the widget.
func (s *stepper) Destroy() { delete(steppers, s.spin) }

func batteryLevel(_ []byte, reply func([]byte, error)) {
	dirs, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	for _, d := range dirs {
		capacity, err := os.ReadFile(filepath.Join(d, "capacity"))
		if err != nil {
			continue
		}
		percent, err := strconv.Atoi(strings.TrimSpace(string(capacity)))
		if err != nil {
			continue
		}
		status, _ := os.ReadFile(filepath.Join(d, "status"))
		charging := strings.TrimSpace(string(status)) == "Charging"
		reply(json.Marshal(map[string]any{"level": float64(percent) / 100, "charging": charging}))
		return
	}
	reply(nil, errors.New("this computer has no battery"))
}
```

Three things the example shows:

- **Props come in no set order.** A view whose props depend on each
  other (a value and its range) keeps what it was given and applies it
  again as the others come.
- **A change from the user goes to Go, and back.** The handler sets the
  signal, which sets the prop to the value the view already shows.
  Setting a control's value in code doesn't fire its own action or
  listener on iOS, Android, macOS or Windows, so this doesn't loop.
  GTK's signals do fire for a change made in code: guard them, as the
  Linux view does.
- **Errors carry a message.** In Swift, conform the error to
  `LocalizedError`; in Go, it's the error's `Error()`.

For another, see [`examples/media`](../examples/media): its native view
screen shows a segmented control, `UISegmentedControl` on iOS,
Material's segmented buttons on Android, `NSSegmentedControl` on macOS,
a tab control drawn as buttons on Windows, linked toggle buttons on
Linux, and radio buttons on the web (`ios/Natives.swift`,
`android/Natives.kt`, `macos/Natives.swift`, `natives_windows.go`,
`natives_linux.go`, `web/natives.js`).

## Developing native code

**On the desktop**, native code in Go is part of the app, built with the
rest: `asuka dev` builds and runs it, and on each save builds it again
and runs the new one, its signals' values kept, as for any Go change. A
Mac app's `macos/*.swift` is compiled again as it changes, and the app
runs again the same way. See
[Development](development.md#on-the-desktop).

**On iOS and Android**, `asuka dev` can't run an app with native code in
its player, which holds only Asuka's own: for the platform whose
directory the app has (`ios/`, `android/`), it builds the app with its
Go code and native code, and rebuilds and reinstalls it on each save,
Swift and Kotlin included. Its signals' values aren't kept across those
reloads. See [Development](development.md#on-a-phone-the-player-and-built-apps).

**On the web**, `asuka dev -platform web` reloads the page as a script in
`web/` changes, its signals' values kept; see
[the web](web.md#native-code).

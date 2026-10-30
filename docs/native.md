# Native code

An app can bring Swift and Kotlin of its own: native views, shown in Go
with `ui.Native`, and native modules, whose methods Go calls with
`ui.Call`. This page covers both, on each platform, with a complete
example.

## When you need it

nui's components and packages cover what most apps show and use. Write
native code when you need a platform view nui doesn't have (a map, a
vendor's SDK view, a control only one platform has), or a platform API
no nui package wraps. The [comparison with SwiftUI and Expo](parity.md)
lists what nui has.

An app's native code lives beside its Go:

- `ios/*.swift`, compiled with the iOS host, in the same Swift module:
  no import of nui's host is needed.
- `android/*.kt`, built into the Android app.

Only the files directly in `ios/` and `android/` are taken, not those in
directories under them. Release builds (`nui build`) include them too.

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
measuring on Android. A view with no natural size of its own needs one
from its style (`Height`, `Grow`).

If no view of the kind is registered, the host logs `no native view
"Stepper" is registered` and shows nothing in its place.

## The Swift side

A native view is a class that conforms to `NuiNativeView`:

```swift
@MainActor public protocol NuiNativeView: AnyObject {
    var view: UIView { get }
    func set(_ name: String, _ value: NuiNativeValue)
}
```

- `view` is the UIKit view shown. The host's own view holds it and
  sizes it to fill.
- `set` gets each prop, by name, as it's set or changes.

A `NuiNativeValue` is one of `.bool(Bool)`, `.int(Int64)`,
`.float(Double)` and `.string(String)`. Its `bool`, `int`, `float` and
`string` properties read it; `int` and `float` convert from each other,
and a value of another type reads as `false`, `0` or `""`. Go's integer
types come as `.int`, its floats as `.float`.

The factory that makes the view gets a `NuiNativeContext`; its `emit`
sends an event to the Go handler of that name:

```swift
ctx.emit("changed", ["value": .int(3)])
```

A view with no `On` handlers sends no events; an event with no handler
of its name is dropped.

## The Kotlin side

A native view implements `NuiNativeView`, in the package `dev.nui`:

```kotlin
interface NuiNativeView {
    val view: View
    fun set(name: String, value: Any)
}
```

`set`'s value is a `Boolean`, `Long`, `Double` or `String`: Go's
integer types come as `Long`, its floats as `Double`.

The factory gets a `NuiNativeContext`. Its `context` is the app's
themed context (the Material 3 theme): make views with it, so they look
as nui's do. Its `emit(name, values)` sends an event, with values that
are `Boolean`, `Int` or `Long` (an `Int` in Go), `Float` or `Double` (a
`Float`); anything else is sent as its `toString()`.

## Registering

Each host looks for one class by name as it starts, before your Go code
runs, and calls it to register the app's views and modules.

On iOS, a class named `NuiAppNatives` in Objective-C, an `NSObject`
conforming to `NuiNativeModule`, whose `register` calls
`NuiRuntime.shared.registerNative(kind) { ctx in ... }`:

```swift
@objc(NuiAppNatives) final class AppNatives: NSObject, NuiNativeModule {
    @MainActor static func register() {
        NuiRuntime.shared.registerNative("Stepper") { ctx in Stepper(ctx) }
    }
}
```

On Android, an `object NuiAppNatives` in the package `dev.nui.app`,
implementing `NuiNativeModule`; its `register` gets the `NuiRuntime`:

```kotlin
package dev.nui.app

import dev.nui.NuiNativeModule
import dev.nui.NuiRuntime

object NuiAppNatives : NuiNativeModule {
    override fun register(runtime: NuiRuntime) {
        runtime.registerNative("Stepper") { ctx -> Stepper(ctx) }
    }
}
```

Write the file's `package dev.nui.app` line as shown: the CLI copies
`android/*.kt` into the app's sources, and the host loads
`dev.nui.app.NuiAppNatives` by that name. Release builds keep it through
R8.

There is one `NuiAppNatives` an app: it registers every view and
module. The views and modules themselves can be in other files.

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
code agree on: JSON is fine. nui's packages wrap the built-in modules
the same way: `nui/secure` calls the `secureStore` module with
`CallWait` and with `Call`.

`Call` fails with an error when the module has no such method (`no
module method battery.level`), when it's called before the app is
mounted, or when the method replies with an error: the Swift error's
`localizedDescription`, or the Kotlin exception's message.

A method gets the call's bytes and a reply, which it calls once (a
second reply is ignored):

```swift
public typealias NuiModuleMethod = (_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) -> Void
```

```kotlin
typealias NuiModuleMethod = (args: ByteArray, reply: (Result<ByteArray>) -> Unit) -> Unit
```

Register a module's methods by name with `registerModule`, from
`NuiAppNatives`: `NuiRuntime.shared.registerModule(name, methods)` in
Swift, `runtime.registerModule(name, methods)` in Kotlin (an extension
function: `import dev.nui.registerModule`). A module of the same name
as one of the host's (`device`, `secureStore`, `camera`...) replaces
it, so give yours names of their own.

**Threading.** A method is called on the main thread. Its reply can come
from any thread: the host takes it to the main thread, and Go gets it
there. Work that takes time (the disk, the network) belongs on another
thread, replying from it when it's done; never block the main thread
waiting for it. On Android, an exception the method throws is its
error.

## A complete example

A `Stepper` view (iOS's `UIStepper`, Android's `NumberPicker`) and a
`battery` module. `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
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

`ios/Natives.swift`:

```swift
import UIKit

@objc(NuiAppNatives) final class AppNatives: NSObject, NuiNativeModule {
    @MainActor static func register() {
        NuiRuntime.shared.registerNative("Stepper") { ctx in Stepper(ctx) }
        NuiRuntime.shared.registerModule("battery", [
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
@MainActor final class Stepper: NuiNativeView {
    let stepper = UIStepper()
    var view: UIView { stepper }
    private var wanted = 0.0

    init(_ ctx: NuiNativeContext) {
        stepper.addAction(UIAction { [weak self] _ in
            guard let self else { return }
            wanted = stepper.value
            ctx.emit("changed", ["value": .int(Int64(stepper.value))])
        }, for: .valueChanged)
    }

    func set(_ name: String, _ value: NuiNativeValue) {
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

`android/Natives.kt`:

```kotlin
package dev.nui.app

import android.content.Context
import android.os.BatteryManager
import android.view.View
import android.widget.NumberPicker
import dev.nui.NuiModuleMethod
import dev.nui.NuiNativeContext
import dev.nui.NuiNativeModule
import dev.nui.NuiNativeView
import dev.nui.NuiRuntime
import dev.nui.registerModule
import org.json.JSONObject

object NuiAppNatives : NuiNativeModule {
    override fun register(runtime: NuiRuntime) {
        runtime.registerNative("Stepper") { ctx -> Stepper(ctx) }
        val level: NuiModuleMethod = { _, reply ->
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
class Stepper(ctx: NuiNativeContext) : NuiNativeView {
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

Three things the example shows:

- **Props come in no set order.** A view whose props depend on each
  other (a value and its range) keeps what it was given and applies it
  again as the others come.
- **A change from the user goes to Go, and back.** The handler sets the
  signal, which sets the prop to the value the view already shows.
  Setting a control's value in code doesn't fire its own change action
  or listener, on either platform, so this doesn't loop.
- **Errors carry a message.** On iOS, conform the error to
  `LocalizedError` for Go to get a message of yours.

For another, see [`examples/media`](../examples/media): its
`ios/Natives.swift` and `android/Natives.kt` make a segmented control,
`UISegmentedControl` on iOS and Material's segmented buttons on
Android.

## Developing native code

`nui dev` can't run an app with native code in its player, which holds
only nui's own: for the platform whose directory the app has (`ios/`,
`android/`), it builds the app with its Go code and native code, and
rebuilds and reinstalls it on each save, Swift and Kotlin included. Its
signals' values aren't kept across those reloads. See
[Development](development.md#the-player-and-built-apps).

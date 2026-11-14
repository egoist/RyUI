# Bluetooth

`asuka/sdk/bluetooth` talks to Bluetooth Low Energy devices: heart rate
straps, scales, sensors, locks, a board of your own. It does what
react-native-ble-plx and Web Bluetooth do, with each platform's own API:
Core Bluetooth on iOS and macOS; Android's `BluetoothLeScanner`,
`BluetoothGatt` and `BluetoothGattServer`; `Windows.Devices.Bluetooth` on
Windows; BlueZ over D-Bus on Linux; Web Bluetooth in a browser that has
it.

As a **central**, the app finds devices, connects to them, and reads,
writes and subscribes to their characteristics. As a **peripheral**, it
advertises and serves services of its own, for other devices to connect
to. [examples/bluetooth](../examples/bluetooth) does both: it scans,
connects to the device tapped and follows its battery and heart rate, and
it can be a heart rate monitor itself.

## The adapter and the permission

```go
state := bluetooth.Use() // a signal: Unknown, Unsupported, NotAsked, Denied, Off, On

go func() {
	s, err := bluetooth.Request() // asks the first time
	...
}()
```

`Current` is the state now, without asking; `Request` asks for the
permission the first time, then reports the state; `Watch` and `Use`
follow it as Bluetooth is turned on and off and the user allows the app.
`NotAsked` means the app hasn't asked yet, `Denied` that the user said
no: they allow it in the app's settings.

| Platform | What `Request` asks |
| --- | --- |
| iOS, macOS | The system's prompt, with `NSBluetoothAlwaysUsageDescription` (asuka.json's `ios.usage`; the package gives a generic one) |
| Android | The nearby devices permission (`BLUETOOTH_SCAN`, `BLUETOOTH_CONNECT`, `BLUETOOTH_ADVERTISE`), and the location permission for an app whose scans find where the user is (`bluetooth.location`, below) |
| Windows | Access to the radio (Settings' privacy page can refuse it) |
| Linux | Nothing: BlueZ decides. A Flatpak without access to `org.bluez` is `Denied` |
| The web | Nothing: the browser asks for each device, in its chooser |

Ask as the user does what needs it, once the app is on screen, not at
launch.

## Finding devices

```go
stop := bluetooth.Scan(bluetooth.ScanOptions{
	Services:   []bluetooth.UUID{bluetooth.HeartRate},
	Duplicates: true, // each advertisement, to follow the RSSI
}, func(a bluetooth.Advertisement, err error) {
	if err != nil {
		status.Set(err.Error()) // couldn't start, or stopped: off, not allowed
		return
	}
	found.Set(append(found.Peek(), a))
})
```

`Scan` runs until `stop`, or until the component it started in goes: a
screen's scan stops with the screen. Each `Advertisement` has the
`Device` (its `ID` and `Name`), the RSSI, the local name, the services,
the service data, the manufacturer data by company identifier, the
transmit power and whether it's connectable. Without `Duplicates`, a
scan hears each device once. If it can't scan, or the platform stops it
(Bluetooth turned off), the callback gets the error, once, and nothing
after: `ErrOff`, `ErrDenied`, `ErrUnsupported`, and on the web
`ErrNotAvailable`.

UUIDs are Go values: `bluetooth.UUID16(0x180d)`, `bluetooth.MustParseUUID("6e400001-b5a3-f393-e0a9-e50e24dcca9e")`,
and the package's names for the common services and characteristics
(`HeartRate`, `Battery`, `BatteryLevel`, `DeviceInformation`...).

### The system's chooser

```go
go func() {
	d, err := bluetooth.Choose(bluetooth.ChooseOptions{
		Filters:          []bluetooth.Filter{{Services: []bluetooth.UUID{bluetooth.HeartRate}}},
		OptionalServices: []bluetooth.UUID{bluetooth.Battery},
	})
	if errors.Is(err, bluetooth.ErrCanceled) {
		return
	}
	...
}()
```

`Choose` shows the system's list of nearby devices for the user to pick
one: the browser's on the web (a page's only way to find one, and to be
allowed it: call it as the user taps), Android's companion device dialog
(which also lets the app reconnect from the background), and Windows'
device picker. iOS, macOS and Linux have none for apps: `ErrNotAvailable`,
and the app scans there.

### Devices it knows

`Known(ids...)` finds the devices of IDs the app kept from before, to
connect without scanning, and `ConnectedDevices(services...)` those the
system is connected to already (for itself, or another app). A
`Device.ID` is the platform's: a UUID Core Bluetooth made for the app on
iOS and macOS, the address on Android, Windows and Linux, the site's own
ID on the web. Keep it to find the device again on the same platform.

## Connecting

```go
func DeviceScreen(d bluetooth.Device) ui.Node {
	c := bluetooth.Connect(d, bluetooth.ConnectOptions{Timeout: 20 * time.Second})
	bpm := signal.New(0)
	go func() {
		ch, err := c.Characteristic(bluetooth.HeartRate, bluetooth.HeartRateMeasurement)
		if err != nil {
			return
		}
		ui.Post(func() {
			ch.Subscribe(func(v []byte, err error) {
				if err == nil {
					bpm.Set(heartRate(v))
				}
			})
		})
	}()
	return ui.Text(signal.Func(func() string { return c.State().Get().String() }))
}
```

`Connect` returns at once with a `Connection` that's `Connecting`, then
`Connected`; it stays until `Close`, the owner's disposal (going back
from the screen above disconnects), or the device's going. `State` is a
signal; `Wait` waits for the connection from a goroutine, `Done` is
closed when it ends, and `Err` says why: a `*DisconnectError` with its
`Reason` (the device ended it, it went out of range, the link failed,
Bluetooth turned off, the app closed it) and the platform's code,
`ErrTimeout` after `ConnectOptions.Timeout`, or what it couldn't connect
with. A lost connection isn't made again: connect again, with
`AutoConnect` to wait for the device to come back.

What waits for the device isn't on the main thread: call it from a
goroutine. Each waits for the connection first, and fails with its error
if it doesn't come:

- `Services` discovers the services, their characteristics and their
  descriptors, once (again when the device says they changed);
  `Service` and `Characteristic` find one by UUID, or `ErrNotFound`.
- A `Characteristic`'s `Read`, `Write` (with a response) and
  `WriteWithoutResponse` (as fast as the link takes them); a
  `Descriptor`'s `Read` and `Write`. A device's refusal is an
  `*ATTRequestError`: `errors.Is(err, bluetooth.WriteNotPermitted)`.
- `MTU`, and `RequestMTU` (Android asks the device; elsewhere the system
  has agreed on one: it's returned), with the longest value a write
  takes; `RSSI`; `SetPriority` (Android's connection priority).

`Subscribe` is on the main thread, as a scan is: it turns notifications
(or indications, with `SubscribeOptions.Indicate`) on, calls back with
each value, and stops with its owner, `stop`, or the connection. If it
can't subscribe, or the connection ends, the callback gets the error.

## Being a peripheral

```go
level := &bluetooth.LocalCharacteristic{
	UUID: bluetooth.BatteryLevel, Properties: bluetooth.Read | bluetooth.Notify, Permissions: bluetooth.Readable,
	OnRead: func(r bluetooth.ServerRequest) ([]byte, error) { return []byte{battery()}, nil },
}
bluetooth.Serve([]*bluetooth.LocalService{{UUID: bluetooth.Battery, Characteristics: []*bluetooth.LocalCharacteristic{level}}},
	func(err error) { status.Set(err.Error()) })
bluetooth.Advertise(bluetooth.AdvertiseOptions{LocalName: "Thermo", Services: []bluetooth.UUID{bluetooth.Battery}},
	func(err error) { status.Set(err.Error()) })
...
level.Notify([]byte{battery()}) // to the centrals subscribed
```

`Serve` serves the services until `Close` or the owner's disposal: a
characteristic answers reads with its fixed `Value`, or its `OnRead`
(the whole value: the platform reads a long one in parts); takes writes
with `OnWrite`; and says who subscribes with `OnSubscribe`. An error
from them is the ATT error the central gets (`bluetooth.ReadNotPermitted`,
`InvalidAttributeValueLength`...). `Advertise` advertises until `stop` or
the owner's disposal. Both call back with the error if the platform
can't, or stops: the web has no peripheral role (`ErrNotAvailable`).

## In the background

An app that keeps using Bluetooth with its screen away says so in
`asuka.json`:

```json
{
  "bluetooth": {
    "background": ["central", "peripheral"],
    "location": false
  }
}
```

- `"central"`: iOS's `bluetooth-central` background mode, with state
  restoration: iOS keeps the app's connections and its scan, and
  relaunches it when they have something; `OnRestore` gives the app what
  it restored, to connect again (immediate) and scan again. A scan in
  the background needs `Services` there, and hears each device once.
  On Android, a scan with `ScanOptions.Background` is one the system
  runs with a `PendingIntent`: it goes on after the app's process is
  gone, and what it hears then comes to `OnRestore` when the app runs
  again, until `StopBackgroundScan`. Android pauses a scan without
  services while the screen is off.
- `"peripheral"`: iOS's `bluetooth-peripheral` mode: advertising and
  serving go on in the background (iOS advertises less often there, the
  services in its overflow area).
- `"location"`: the app finds where the user is from its scans (beacons).
  By default an Android app's scans say they don't (`neverForLocation`),
  which needs no location permission but leaves some beacons out; with
  it, `Request` asks for the location permission too.

The desktops and the web use Bluetooth only while the app runs.

## What the build adds

An app that imports the package is given what it needs, and no more:

| Platform | What |
| --- | --- |
| iOS | `NSBluetoothAlwaysUsageDescription` (say yours in `ios.usage`); Core Bluetooth linked, for an app whose Go code calls the package; the background modes of `bluetooth.background` |
| Android | `BLUETOOTH_SCAN` (with `neverForLocation` unless `bluetooth.location`), `BLUETOOTH_CONNECT`, `BLUETOOTH_ADVERTISE`; `android.hardware.bluetooth_le` and the companion device feature, not required; the background scan's receiver for `"central"` |
| macOS | The purpose string, and the hardened runtime's `com.apple.security.device.bluetooth` entitlement |
| Linux | The Flatpak's `--system-talk-name=org.bluez` |

In `asuka dev`, the player has all of them, background modes too.

## Each platform

| | iOS, macOS | Android | Windows | Linux | The web |
| --- | --- | --- | --- | --- | --- |
| Scan | ✓ | ✓ | ✓ | ✓ | Only behind Chrome's experimental flag (`requestLEScan`); `Choose` |
| Chooser | — | Companion device dialog | Device picker | — | ✓ `requestDevice` |
| Advertise | Name and services only, always connectable; one advertisement (several are merged) | Services, service and manufacturer data; the device's own name, not `LocalName` | Manufacturer data; services through a served service | ✓ | — |
| GATT server | ✓ | ✓ | ✓ | ✓ | — |
| `RequestMTU` | The system's | ✓ | The system's | The system's | — |
| Connection's RSSI | ✓ | ✓ | — | While discovering | — |
| `SetPriority` | Nothing | ✓ | Windows 11's connection parameters | Nothing | Nothing |
| Disconnection reason | ✓ | ✓ | Off or unknown | BlueZ 5.82 and later | Unknown |
| Background | Modes, state restoration | `PendingIntent` scan | — | — | — |

What else differs:

- **iOS and macOS.** `SubscribeOptions.Indicate` is Core Bluetooth's
  choice, not the app's. A local characteristic's descriptors can only be
  `UserDescription` and `PresentationFormat`. A long write comes whole,
  and refused one part refuses it all. The simulator has no Bluetooth:
  `Unsupported`. A Mac app run with `go run` asks for its terminal.
- **Android.** Asuka's Android is 13 and later, so the permissions
  before Android 12's aren't needed. A chooser filter matches on its
  first service only, and each pick makes a companion association the
  app keeps. Android allows 5 scan starts in 30 seconds: Asuka merges
  the app's scans into one and spaces its restarts.
- **Windows.** The app's own advertisements can only carry manufacturer
  data: services are advertised by serving them. A long write to the
  app's server comes in parts, each at its `ServerRequest.Offset`. Some
  services (HID) are the system's.
- **Linux.** BlueZ's `bluetoothd` on the system bus. A long write to
  the app's server comes in parts, as on Windows; a subscription doesn't
  say which central, so `Notify` goes to every subscriber.
- **The web.** Safari and Firefox have no Web Bluetooth: `Unsupported`.
  In Chrome and Edge, a page uses only the services named in the
  chooser's filters and `OptionalServices`, and some (a blocklist) not
  at all. No MTU or RSSI, no peripheral role. A `Device.ID` is the
  site's, and `Known` finds a device of an earlier visit only where the
  browser has `getDevices`.

## Testing

The fake host simulates an adapter and peripherals with their services,
so an app's [UI tests](testing.md) find, connect and read without a
device:

```go
func TestHeartRate(t *testing.T) {
	var strap *fake.Peripheral
	a := uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) {
		strap = &fake.Peripheral{ID: "AA:BB:CC:00:00:01", Name: "Polar H10", RSSI: -58, Advertised: []string{"180d"},
			Services: []*fake.Service{{UUID: "180d", Characteristics: []*fake.Characteristic{
				{UUID: "2a37", Properties: int(bluetooth.Notify)},
			}}}}
		h.Bluetooth().Add(strap)
	}))
	a.Tap("Scan")
	a.WaitFor("Polar H10")
	a.Tap("Polar H10")
	a.Until("subscribed", func() bool { return strap.Characteristic("180d", "2a37").Subscribed })
	strap.Notify("180d", "2a37", []byte{0, 61})
	a.WaitFor("61 bpm")
}
```

`fake.Bluetooth` also turns the adapter off (`SetState`), takes a device
away (`Remove`, `Disconnect`), picks in the chooser (`Chooser`), and
plays a central against the app's server (`CentralRead`, `CentralWrite`,
`CentralSubscribe`, what it `Notified`).

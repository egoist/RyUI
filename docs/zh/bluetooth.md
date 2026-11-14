# 蓝牙

`asuka/sdk/bluetooth` 用来和低功耗蓝牙（Bluetooth Low Energy）设备通信：心率带、体重秤、各类传感器、门锁，或者你自己做的开发板。它提供的能力和 react-native-ble-plx、Web Bluetooth 相当，并且在每个平台上都调用平台自己的 API：iOS 和 macOS 上是 Core Bluetooth；Android 上是 `BluetoothLeScanner`、`BluetoothGatt` 和 `BluetoothGattServer`；Windows 上是 `Windows.Devices.Bluetooth`；Linux 上通过 D-Bus 调用 BlueZ；浏览器里则是 Web Bluetooth（前提是浏览器支持）。

作为**中心设备（central）**，应用可以发现设备、连接设备，并读写和订阅设备的特征（characteristic）。作为**外围设备（peripheral）**，应用可以广播自己，并提供自己的服务供其他设备连接。[examples/bluetooth](../../examples/bluetooth) 两者都演示了：它会扫描附近的设备，连接你点中的那个，持续显示它的电量和心率；它自己也可以充当一个心率监测器。

## 适配器与权限

```go
state := bluetooth.Use() // 一个 signal：Unknown、Unsupported、NotAsked、Denied、Off、On

go func() {
	s, err := bluetooth.Request() // 第一次调用时请求权限
	...
}()
```

`Current` 返回当前状态，不会请求权限；`Request` 在第一次调用时请求权限，然后返回状态；`Watch` 和 `Use` 会跟随状态变化，比如用户打开或关闭蓝牙、给应用授权。`NotAsked` 表示应用还没请求过权限，`Denied` 表示用户拒绝了：用户需要到应用的设置里允许。

| 平台 | `Request` 请求的是什么 |
| --- | --- |
| iOS、macOS | 系统的授权弹窗，文案取自 `NSBluetoothAlwaysUsageDescription`（asuka.json 的 `ios.usage`；包会提供一条通用文案） |
| Android | “附近的设备”权限（`BLUETOOTH_SCAN`、`BLUETOOTH_CONNECT`、`BLUETOOTH_ADVERTISE`）；如果应用要通过扫描确定用户位置（见下文的 `bluetooth.location`），还会请求位置权限 |
| Windows | 对无线电的访问权限（可以在“设置”的隐私页面里拒绝） |
| Linux | 不请求：由 BlueZ 决定。没有 `org.bluez` 访问权限的 Flatpak 会是 `Denied` |
| Web | 不请求：浏览器会在它的设备选择器里逐个设备询问 |

请在用户真正要用到蓝牙的时候、应用已经显示在屏幕上之后再请求权限，不要在启动时就请求。

## 发现设备

```go
stop := bluetooth.Scan(bluetooth.ScanOptions{
	Services:   []bluetooth.UUID{bluetooth.HeartRate},
	Duplicates: true, // 每条广播都上报，以便跟踪 RSSI
}, func(a bluetooth.Advertisement, err error) {
	if err != nil {
		status.Set(err.Error()) // 无法开始，或者被停止：蓝牙关闭、没有权限
		return
	}
	found.Set(append(found.Peek(), a))
})
```

`Scan` 会一直运行，直到调用 `stop`，或者启动它的组件被销毁：在某个页面里开始的扫描会随页面一起停止。每个 `Advertisement` 包含 `Device`（它的 `ID` 和 `Name`）、RSSI、本地名称、服务、服务数据、按厂商标识符区分的厂商数据、发射功率，以及是否可连接。不设 `Duplicates` 时，每个设备只会上报一次。如果无法扫描，或者平台中途停止了扫描（比如蓝牙被关闭），回调会收到一次错误，之后不再被调用：`ErrOff`、`ErrDenied`、`ErrUnsupported`，在 Web 上是 `ErrNotAvailable`。

UUID 是 Go 的值：`bluetooth.UUID16(0x180d)`、`bluetooth.MustParseUUID("6e400001-b5a3-f393-e0a9-e50e24dcca9e")`，包里也为常用的服务和特征起好了名字（`HeartRate`、`Battery`、`BatteryLevel`、`DeviceInformation` ……）。

### 系统的设备选择器

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

`Choose` 会弹出系统的附近设备列表，让用户选一个：在 Web 上是浏览器的选择器（这是网页发现设备、并获得使用许可的唯一方式，所以要在用户点击时调用）；在 Android 上是配套设备（companion device）对话框，它还允许应用之后在后台重新连接；在 Windows 上是系统的设备选取器。iOS、macOS 和 Linux 没有提供给应用的选择器：会返回 `ErrNotAvailable`，在这些平台上请改用扫描。

### 已知的设备

`Known(ids...)` 根据应用之前保存的 ID 找回设备，无需扫描就能连接；`ConnectedDevices(services...)` 返回系统已经连接着的设备（系统自己连的，或者其他应用连的）。`Device.ID` 由平台决定：iOS 和 macOS 上是 Core Bluetooth 为这个应用生成的 UUID，Android、Windows 和 Linux 上是设备地址，Web 上是网站自己的 ID。保存好它，就能在同一平台上再次找到这个设备。

## 连接

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

`Connect` 会立即返回一个处于 `Connecting` 状态的 `Connection`，连上后变为 `Connected`。连接会一直保持，直到调用 `Close`、owner 被销毁（在上面的例子里，从这个页面返回就会断开），或者设备离开。`State` 是一个 signal；在 goroutine 里可以用 `Wait` 等待连接建立；连接结束时 `Done` 会被关闭，`Err` 说明原因：可能是带 `Reason` 的 `*DisconnectError`（设备主动断开、超出范围、链路故障、蓝牙被关闭、应用自己关闭了连接），附带平台的错误码；也可能是超过 `ConnectOptions.Timeout` 后的 `ErrTimeout`，或者连接失败时的错误。断开的连接不会自动重连：请重新连接；设置 `AutoConnect` 可以一直等到设备回来。

需要等待设备响应的操作不能在主线程上调用，请在 goroutine 里调用。它们都会先等待连接建立；如果连接没有建立，就返回连接的错误：

- `Services` 发现设备的服务、服务下的特征以及特征的描述符（descriptor），只发现一次（设备声明服务变化后会重新发现）；`Service` 和 `Characteristic` 按 UUID 查找，找不到时返回 `ErrNotFound`。
- `Characteristic` 的 `Read`、`Write`（带响应）和 `WriteWithoutResponse`（链路能承受多快就写多快）；`Descriptor` 的 `Read` 和 `Write`。设备拒绝请求时返回 `*ATTRequestError`：`errors.Is(err, bluetooth.WriteNotPermitted)`。
- `MTU` 和 `RequestMTU`（Android 会向设备申请；其他平台由系统自行协商，返回协商结果），以及一次写入能写的最大长度；`RSSI`；`SetPriority`（Android 的连接优先级）。

`Subscribe` 和扫描一样在主线程上调用：它会打开通知（设置 `SubscribeOptions.Indicate` 则为指示），每收到一个值就回调一次，并在 owner 销毁、调用 `stop` 或连接结束时停止。如果无法订阅，或者连接结束，回调会收到错误。

## 作为外围设备

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
level.Notify([]byte{battery()}) // 发给已订阅的中心设备
```

`Serve` 会一直提供这些服务，直到调用 `Close` 或 owner 被销毁。特征用固定的 `Value` 或者 `OnRead` 回答读取请求（返回完整的值：较长的值由平台分段读取）；用 `OnWrite` 接收写入；用 `OnSubscribe` 得知谁订阅了。这些回调返回的错误就是中心设备收到的 ATT 错误（`bluetooth.ReadNotPermitted`、`InvalidAttributeValueLength` ……）。`Advertise` 会一直广播，直到调用 `stop` 或 owner 被销毁。如果平台无法提供服务或广播，或者中途停止，两者都会把错误交给回调：Web 没有外围设备角色（`ErrNotAvailable`）。

## 在后台

如果应用在离开屏幕后还要继续使用蓝牙，需要在 `asuka.json` 里声明：

```json
{
  "bluetooth": {
    "background": ["central", "peripheral"],
    "location": false
  }
}
```

- `"central"`：iOS 的 `bluetooth-central` 后台模式，并启用状态恢复：iOS 会替应用保持连接和扫描，在有事件时重新启动应用；`OnRestore` 会告诉应用恢复了哪些内容，以便重新连接（会立即完成）和重新扫描。在 iOS 后台扫描必须指定 `Services`，并且每个设备只会上报一次。在 Android 上，设置了 `ScanOptions.Background` 的扫描由系统通过 `PendingIntent` 执行：应用进程结束后它仍会继续，期间扫到的结果会在应用下次运行时交给 `OnRestore`，直到调用 `StopBackgroundScan`。屏幕关闭时，Android 会暂停没有指定服务的扫描。
- `"peripheral"`：iOS 的 `bluetooth-peripheral` 模式：广播和服务在后台继续运行（iOS 在后台广播得更少，服务 UUID 会放进溢出区域）。
- `"location"`：应用会通过扫描确定用户的位置（比如信标）。默认情况下，Android 应用的扫描会声明自己不用于定位（`neverForLocation`），这样不需要位置权限，但会漏掉部分信标；打开这一项后，`Request` 还会请求位置权限。

桌面端和 Web 只在应用运行时使用蓝牙。

## 构建时添加的内容

导入这个包的应用会得到它需要的东西，不多也不少：

| 平台 | 内容 |
| --- | --- |
| iOS | `NSBluetoothAlwaysUsageDescription`（在 `ios.usage` 里写上你自己的文案）；Go 代码调用了这个包的应用才会链接 Core Bluetooth；`bluetooth.background` 声明的后台模式 |
| Android | `BLUETOOTH_SCAN`（除非设置了 `bluetooth.location`，否则带 `neverForLocation`）、`BLUETOOTH_CONNECT`、`BLUETOOTH_ADVERTISE`；`android.hardware.bluetooth_le` 和配套设备功能，均不是必需的；`"central"` 时还有后台扫描的接收器 |
| macOS | 授权文案，以及加固运行时（hardened runtime）的 `com.apple.security.device.bluetooth` entitlement |
| Linux | Flatpak 的 `--system-talk-name=org.bluez` |

在 `asuka dev` 中，播放器应用包含上面所有这些，后台模式也不例外。

## 各平台的情况

| | iOS、macOS | Android | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- |
| 扫描 | ✓ | ✓ | ✓ | ✓ | 只能在打开 Chrome 实验性功能开关后使用（`requestLEScan`）；或用 `Choose` |
| 设备选择器 | — | 配套设备对话框 | 设备选取器 | — | ✓ `requestDevice` |
| 广播 | 只有名称和服务，总是可连接；只有一个广播（多个会合并） | 服务、服务数据和厂商数据；名称是设备自己的，不是 `LocalName` | 厂商数据；服务通过所提供的服务来广播 | ✓ | — |
| GATT 服务端 | ✓ | ✓ | ✓ | ✓ | — |
| `RequestMTU` | 系统决定 | ✓ | 系统决定 | 系统决定 | — |
| 连接的 RSSI | ✓ | ✓ | — | 仅在发现设备期间 | — |
| `SetPriority` | 无作用 | ✓ | Windows 11 的连接参数 | 无作用 | 无作用 |
| 断开原因 | ✓ | ✓ | 只有“关闭”或“未知” | BlueZ 5.82 及以后 | 未知 |
| 后台 | 后台模式、状态恢复 | `PendingIntent` 扫描 | — | — | — |

其他差异：

- **iOS 和 macOS。** 用通知还是指示由 Core Bluetooth 决定，`SubscribeOptions.Indicate` 不起作用。本地特征的描述符只能是 `UserDescription` 和 `PresentationFormat`。长写入会完整地一次交给应用，只要拒绝其中一部分，整个写入都会被拒绝。iOS 模拟器没有蓝牙：状态是 `Unsupported`。用 `go run` 运行的 Mac 应用会以它所在的终端的名义请求权限。
- **Android。** Asuka 支持的 Android 版本从 13 开始，所以不需要 Android 12 之前的那些权限。设备选择器的过滤条件只会匹配第一个服务，并且每次选择都会建立一个应用持有的配套设备关联。Android 限制每 30 秒最多启动 5 次扫描：Asuka 会把应用的多个扫描合并成一个，并拉开重启的间隔。
- **Windows。** 应用自己的广播只能携带厂商数据：服务要通过提供这些服务来广播。发给应用服务端的长写入会分段到达，每段带有自己的 `ServerRequest.Offset`。有些服务（HID）归系统所有。
- **Linux。** 使用系统总线上 BlueZ 的 `bluetoothd`。发给应用服务端的长写入和 Windows 一样分段到达；订阅时 BlueZ 不会说明是哪个中心设备，所以 `Notify` 会发给所有订阅者。
- **Web。** Safari 和 Firefox 没有 Web Bluetooth：状态是 `Unsupported`。在 Chrome 和 Edge 中，网页只能使用选择器过滤条件和 `OptionalServices` 中列出的服务，有些服务（在黑名单上的）完全不能用。没有 MTU 和 RSSI，也没有外围设备角色。`Device.ID` 是网站自己的 ID；只有浏览器支持 `getDevices` 时，`Known` 才能找回之前访问时的设备。

## 测试

模拟宿主（fake host）可以模拟一个蓝牙适配器和带有服务的外围设备，所以应用的 [UI 测试](testing.md) 不需要真实设备就能发现、连接和读取：

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

`fake.Bluetooth` 还可以关闭适配器（`SetState`）、移走设备（`Remove`、`Disconnect`）、在设备选择器里代用户选择（`Chooser`），以及扮演中心设备来测试应用的服务端（`CentralRead`、`CentralWrite`、`CentralSubscribe`，以及它收到的 `Notified`）。

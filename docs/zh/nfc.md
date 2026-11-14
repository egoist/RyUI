# NFC 标签

`asuka/sdk/nfc` 用来读写 NFC 标签：标签上的 NDEF 消息（链接、文本、应用自己的数据）、标签本身（ID 和技术类型），以及发给标签的原始命令。它做的事和 react-native-nfc-manager、Web NFC 一样，只是在每个平台上都用平台自己的 API：iOS 上是带系统面板的 Core NFC，Android 上是读卡器模式（reader mode），Web 上是 Chrome for Android 的 Web NFC。Mac、PC 和 Linux 会如实回答它们没有 NFC。

```go
s := nfc.Start(nfc.Options{Alert: "Hold your iPhone near the tag."}, func(t *nfc.Tag) {
	if u, ok := t.Message.URI(); ok {
		link.Set(u)
	}
	s.Stop()
})
```

[examples/nfc](../../examples/nfc) 读取标签上的链接，也能把链接写进标签。

## 有没有 NFC，开没开

`nfc.Check` 告诉你设备有没有 NFC（`Supported`）以及它是否打开（`Enabled`），不能在主线程上调用。`nfc.Use` 把同样的信息作为 signal 提供给组件，在组件存活期间跟随 Android 上的开关变化；`nfc.Watch` 则在每次变化时调用一个函数。

```go
state := nfc.Use()
ui.Show(signal.Func(func() bool { s := state.Get(); return s.Supported && !s.Enabled }), func() ui.Node {
	return ui.Button(ui.ButtonProps{Title: ui.Val("Turn NFC On"), OnPress: func() { go nfc.OpenSettings() }})
})
```

iPhone 从 iPhone 7 起就有 NFC，而且没有开关，所以在 iPhone 上 `Enabled` 和 `Supported` 相同。iPad 和 iOS 模拟器没有 NFC。在 Android 上，`OpenSettings` 会打开系统的 NFC 设置，让用户把它打开；在其他平台上它返回 `ErrNotAvailable`。浏览器在会话发现 NFC 关着（`ErrDisabled`）之前，没法知道它是否关闭。

## 读取标签：会话

`nfc.Start` 开始一个寻找标签的会话，每找到一个标签，就在主线程上调用它的函数。如果在组件里调用（组件函数体或 effect 中），会话会随组件的 owner 一起结束；如果在事件处理函数里调用，会话会一直持续，直到 `Stop`、`Fail`，或者（在 iOS 上）用户关掉面板或者超时。同一时间只有一个会话：开始新会话会结束之前的那个。

会话结束时会调用 `Options.OnEnd`：如果是应用自己结束的，参数为 `nil`；用户关掉 iOS 的面板时为 `nfc.ErrCanceled`；iOS 的六十秒到期后为 `nfc.ErrTimeout`；会话没能开始时则是 `ErrNotAvailable`、`ErrDisabled` 或 `ErrDenied`。

`Tag` 记录了标签被发现时提供的信息：它的 `ID`、`Techs`、NDEF 状态（`NDEF`：`Format`、`Capacity`、`CanMakeReadOnly`，以及 Android 的 `Type`）和它的 `Message`（没有读取时为 nil）；平台提供的话，还有对应技术的细节（`ISO7816`、`FeliCa`、`ISO15693`、`MIFARE`、`NfcA`、`NfcB`、`Barcode`、`MaxTransceive`）。

### 一次一个标签

会话一次只把一个标签交给应用，并在应用处理它期间一直持有它：再次读取、写入、发送命令都可以。处理完后，`Next` 放开这个标签，继续寻找下一个。应用持有一个标签时靠近的其他标签，会等到 `Next` 之后再交给应用。在 iOS 上，`Next` 会重新开始轮询，所以仍然靠着手机的标签会被再次发现；Android 只在标签靠近的那一刻才会发现它。

```go
nfc.Start(nfc.Options{OnEnd: ended}, func(t *nfc.Tag) {
	count.Update(func(n int) int { return n + 1 })
	s.Next() // 下一个标签
})
```

### iOS 的系统面板

在 iOS 上，会话会显示系统的面板。`Options.Alert` 是寻找标签时面板上显示的文字，`SetAlert` 可以修改它（比如 `"Writing…"`）。`Stop` 会让面板带着对勾和当前提示文字关闭；`Fail("Couldn't write the tag.")` 则让面板显示这条失败信息后关闭。Android 和 Web 不显示面板：应用自己展示界面，提示文字在那里不起作用。

`Options.Poll` 决定使用哪种会话。不设置时，iOS 使用 `NFCNDEFReaderSession` 读取带 NDEF 消息的标签：这样找到的标签没有 ID，也不能接收原始命令。设置之后（`nfc.PollA`、`PollB`、`PollF`、`PollV`、`PollPACE` 或 `PollAll`），iOS 改用 `NFCTagReaderSession` 轮询这些类型的标签，可以拿到 ID，也能发送命令。ISO 7816 卡需要在 asuka.json 的 `nfc.iso7816` 里列出它的应用，FeliCa 卡需要在 `nfc.felica` 里列出系统代码：iOS 不会发现其他的卡。

### Android 的读卡器模式

在 Android 上，会话就是为前台页面开启的读卡器模式（`enableReaderMode`）：标签会交给这个应用而不是其他应用，Android 也不会显示自己的标签界面。当某个 Activity 回到前台时，会为它重新开启。`Options.Poll` 对应读卡器模式的标志（不设置则是所有类型）；`SkipNDEF` 让系统在发现标签时不读取 NDEF 消息（`FLAG_READER_SKIP_NDEF_CHECK`，对只接收命令的标签更快）；`NoSound` 关掉平台的提示音，方便应用使用自己的反馈（`ui.Haptic`）；`PresenceDelay` 是 Android 检查标签是否还在的时间间隔。

## NDEF 消息和记录

NDEF 消息就是 `nfc.Message`：由多条 `Record` 组成，每条记录都有 `TNF`、`Type`、`ID` 和 `Payload`。下面这些函数用来创建应用最常用的记录：

| 创建 | 读取 | 说明 |
| --- | --- | --- |
| `nfc.URI("https://example.com")` | `r.URI()` | 链接（任何 URI：`tel:`、`mailto:`、`geo:`），像各平台一样缩写前缀 |
| `nfc.Text("Hello", "en")` | `r.Text()` | 文本及其语言（可读取 UTF-8 或 UTF-16） |
| `nfc.MIME("text/vcard", data)` | `r.MediaType()` | 某种 MIME 类型的数据 |
| `nfc.External("example.com", "ticket", data)` | `r.ExternalType()` | 应用自定义类型（`example.com:ticket`）的数据 |
| `nfc.SmartPoster(nfc.Poster{...})` | `r.SmartPoster()` | 智能海报：带有多语言标题、操作、图标、大小和类型的链接 |
| `nfc.AndroidApp("com.example.app")` | | Android 应用记录（AAR）：Android 会为这个标签打开指定的应用 |

`Message.URI` 是第一条记录的 URI（URI 记录、绝对 URI，或者智能海报中的链接），也就是 Android 打开应用时使用的那个；`Message.Text` 是第一条文本。`Message.Encode` 和 `nfc.Decode` 负责消息与标签上实际字节之间的转换（`Decode` 会把分块的记录拼接起来）；空标签上的消息是一条空记录，解码后是一个空的 `Message`。

## 写入标签和设为只读

应用持有标签时，`Write` 用新消息替换标签上的消息，`MakeReadOnly` 则永久锁定标签：之后谁也不能再写它。这两个调用都要等待标签响应，所以要在 goroutine 里调用，再回到主线程处理会话。

```go
nfc.Start(nfc.Options{Alert: "Hold your iPhone near a tag to write it."}, func(t *nfc.Tag) {
	if !t.NDEF.Writable() {
		s.Fail("This tag can't be written.")
		return
	}
	s.SetAlert("Writing…")
	go func() {
		err := t.Write(nfc.Message{nfc.URI("https://example.com/t/42")})
		ui.Post(func() {
			if err != nil {
				s.Fail("Couldn't write the tag.")
				return
			}
			s.Stop()
		})
	}()
})
```

对已锁定的标签，`Write` 返回 `ErrReadOnly`；消息超过 `NDEF.Capacity` 时返回 `ErrTooLarge`；`Formatable` 的标签（还不是 NDEF 格式，即 Android 的 `NdefFormatable`）会在写入时被格式化。`Read` 重新读取消息，`Status` 重新查询 NDEF 状态。标签离开或被放开之后，这些调用会返回 `ErrTagLost`。

## 标签本身：ID、技术类型和命令

`Tag.Techs` 是标签具备的技术类型。两个平台都会报告 `nfc.NDEF`、`ISO7816`（ISO 7816 卡，即 ISO 14443-4）、`MIFARE`（ISO 14443-3A 标签：NTAG、MIFARE Ultralight、DESFire）、`FeliCa` 和 `ISO15693`；Android 还会报告自己的类型（`NfcA`、`NfcB`、`NfcF`、`NfcV`、`IsoDep`、`MifareClassic`、`MifareUltralight`、`NDEFFormatable`、`NfcBarcode`）。`ID` 是平台给出的标签标识符（Android 上 NFC-V 的 ID 是低字节在前）。

`Transceive` 发送原始命令并返回响应。iOS 需要设置了 `Poll` 的会话；Web 不支持（`ErrNotSupported`）。

| 技术类型 | 发送的内容 |
| --- | --- |
| `ISO7816` | 一条 APDU；响应以状态字结尾（`Command` 会解析它们） |
| `MIFARE` | 一个 ISO 14443-3A 帧，不含 CRC（由平台添加） |
| `FeliCa` | 一条命令，不含长度字节（由平台添加，并从响应中去掉） |
| `ISO15693` | 一个请求：标志、命令代码，然后是参数（寻址命令的 UID 也在其中）；响应是它的标志，后面跟着数据 |
| Android 的 `NfcA`、`NfcB`、`NfcF`、`NfcV`、`IsoDep`、`MifareUltralight`、`MifareClassic` | 按 Android 对应类接受的格式发送帧（`NfcF` 包含长度字节） |

```go
r, err := t.Command(nfc.SelectAID(aid)) // 按 AID 执行 SELECT
if err == nil && r.OK() {
	r, err = t.Command(nfc.APDU{CLA: 0x00, INS: 0xB0, P1: 0, P2: 0, Le: 256}) // READ BINARY
}
```

`nfc.APDU` 会根据数据和 `Le` 的需要，把命令编码成短格式或扩展格式；`Response` 包含 `Data`、`SW1` 和 `SW2`（`OK` 表示 90 00）。

MIFARE Classic 的扇区只能在 Android 上读写（iOS 不支持 MIFARE Classic）：先用扇区的密钥 A 或 B 调用 `ClassicAuth`，然后用 `ClassicRead` 和 `ClassicWrite` 读写 16 字节的块；值块则用 `ClassicValue`（`Increment`、`Decrement`、`Restore`、`Transfer`）。

## 能打开应用的标签

应用没有打开时，标签也能打开它，或者把它带到前台：

- **iOS** 会在后台读取标签（iPhone XS 及更新的机型），当标签的第一条记录是某个应用的通用链接时，提示用户打开该应用：也就是 asuka.json 的 `links` 中、域名提供了 apple-app-site-association 文件的链接。这个链接会像其他链接一样交给 `ui.OnOpenURL`。
- **Android** 会为 asuka.json 中 `nfc.ndef`（NDEF_DISCOVERED，按消息的第一条记录匹配：URI、MIME 类型或外部类型）和 `nfc.tech`（TECH_DISCOVERED，按技术类型匹配）列出的标签打开应用。带 URI 的标签交给 `ui.OnOpenURL`；其他标签作为 `Tag` 交给 `nfc.OnOpen`，标签还在附近时，应用依然可以读写它、给它发命令。属于应用 `links` 的 https 标签还会作为 App Link 打开应用。

```go
ui.OnOpenURL(func(u string) { link.Set(u) })
nfc.OnOpen(func(t *nfc.Tag) {
	if mt, ok := t.Message[0].MediaType(); ok && mt == "application/vnd.example.ticket" {
		ticket.Set(string(t.Message[0].Payload))
	}
})
```

在设置处理函数之前到达的标签（比如启动应用的那个标签）会一直等到第一个处理函数出现。`nfc.OnOpen` 会随组件的 owner 一起停止，也可以调用它返回的函数停止。会话运行期间到达的标签交给会话，而不是这里。

## asuka.json 与权限

导入了 `asuka/sdk/nfc` 的应用会获得 Android 的 `NFC` 权限和 `uses-feature android.hardware.nfc`（非必需：没有 NFC 的手机也能安装），以及 iOS 上的 `NFCReaderUsageDescription`（在 `ios.usage` 里写你自己的说明）和 Core NFC 的 entitlement `com.apple.developer.nfc.readersession.formats`（`NDEF` 和 `TAG`）。发布构建的 App ID 需要开启 NFC Tag Reading 能力：如果描述文件缺少它，`asuka build` 会提示你。

asuka.json 的 `nfc` 用来补充应用的标签所需的配置：

```json
"nfc": {
  "iso7816": ["D2760000850101"],
  "felica": ["8008"],
  "ndef": [{ "uri": "https://example.com/t/" }, { "mime": "application/vnd.example.ticket" }, { "external": "example.com:ticket" }],
  "tech": [["IsoDep"], ["NfcA", "MifareUltralight"]],
  "required": true
}
```

| 键 | 说明 |
| --- | --- |
| `iso7816` | iOS 发现卡片时依次选择的 ISO 7816 应用（AID，十六进制），写入 Info.plist 的 `com.apple.developer.nfc.readersession.iso7816.select-identifiers` |
| `felica` | iOS 轮询的 FeliCa 系统代码（4 位十六进制数），写入 `com.apple.developer.nfc.readersession.felica.systemcodes` |
| `ndef` | 在 Android 上按第一条记录打开应用的标签：`uri`（scheme、主机和路径前缀）、`mime`（文本记录是 `text/plain`）、`external`（`domain:type`） |
| `tech` | 在 Android 上按技术类型打开应用的标签：每个列表里是标签必须全部具备的类型（`android.nfc.tech` 的类） |
| `required` | 应用只面向带 NFC 的手机：Google Play 不会向其他设备提供它 |

这些配置只会加到导入了这个包的应用里。见 [asuka.json](asuka-json.md#nfc)。

## 在 asuka dev 中

iOS 模拟器和 Android 模拟器都没有 NFC：`Check` 会报告不支持。在 Android 手机上，开发 App 带有 `NFC` 权限，所以在 `asuka dev` 的热重载模式下，会话、读取、写入和命令都能用；能打开应用的标签则不行（过滤器只存在于应用自己的构建里：请用 `--no-hot` 测试，`asuka dev` 也会提示）。在 iPhone 上，开发 App 的描述文件需要开启 NFC Tag Reading 能力；ISO 7816 卡和 FeliCa 则需要应用自己的构建（它们的 AID 和系统代码写在应用的 Info.plist 里），所以配置了 `nfc.iso7816` 或 `nfc.felica` 的应用，`asuka dev` 会构建成应用本身来运行，而不使用热重载模式。Web NFC 需要安全上下文，而 `--lan` 的地址不是：运行 `asuka dev --platform web`，把端口转发到手机（`adb reverse tcp:<port> tcp:<port>`），再在手机的 Chrome 里打开 `http://localhost:<port>`。

## 各平台上的表现

| 平台 | 做法 |
| --- | --- |
| iOS | Core NFC（iOS 16）：不设置 `Poll` 时用 `NFCNDEFReaderSession`，设置时用 `NFCTagReaderSession`；系统面板及其提示文字；ISO 7816、MIFARE（Ultralight、Plus、DESFire）、FeliCa 和 ISO 15693 命令；通过通用链接在后台读取标签。不支持 MIFARE Classic，没有 NFC 开关，应用自定义类型的标签也不能打开应用 |
| Android | 为前台的 Activity 开启读卡器模式；支持 `android.nfc.tech` 的所有技术类型，在手机的 NFC 控制器能读取时（NXP 的控制器）支持 MIFARE Classic 的扇区；NDEF_DISCOVERED 和 TECH_DISCOVERED 能打开应用；支持 `OpenSettings` |
| Web | Web NFC 的 `NDEFReader`（Chrome for Android，需要 https）：读写 NDEF、`MakeReadOnly`（Chrome 100）、标签的序列号。不支持原始命令，查不到容量（`-1`），标签也不能打开页面；`Read` 返回读取时的消息。其他浏览器没有 Web NFC：返回 `ErrNotAvailable` |
| macOS、Windows、Linux | `ErrNotAvailable`：`Check` 报告不支持 |

桌面端的 USB NFC 读卡器是智能卡读卡器（PC/SC）：它们能读卡片的 APDU，但没法以一种通用于各种读卡器的方式读取标签上的 NDEF 消息（每家厂商的命令都不一样），也没有可以配合会话面板的刷卡流程。所以 Asuka 没有支持它们；需要的话，可以通过原生包访问（`winscard.dll`、pcsc-lite、CryptoTokenKit）。

## 用模拟标签测试

`host/fake` 的 `NFC` 为应用的界面测试（见[测试](testing.md)）模拟手机的 NFC：`Tap` 把标签靠近正在运行的会话，`Cancel` 和 `Timeout` 像 iOS 那样结束会话，`Remove` 把标签拿开，`Open` 用一个标签打开应用，`SetEnabled` 可以关闭 NFC。`fake.NFCTag` 保存着它的 NDEF 消息字节，应用写入后这些字节会随之改变。

```go
func TestRead(t *testing.T) {
	var n *fake.NFC
	a := uitest.Mount(t, Tags, uitest.Setup(func(h *fake.Host) { n = h.NFC() }))
	a.Tap("Read a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	n.Tap(&fake.NFCTag{NDEF: nfc.Message{nfc.URI("https://example.com/hello")}.Encode()})
	a.WaitFor("https://example.com/hello")
}
```

`n.Session` 是正在运行的会话，包括它的 `Alert`、`Poll` 和其他选项；标签的 `Transceive` 和 `Classic` 负责回答发给它的命令。

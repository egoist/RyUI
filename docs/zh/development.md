# 开发

`asuka dev` 会运行你的应用，并在你保存时重新加载。它可以运行在 iOS 模拟器或 iPhone、Android 模拟器或手机、你的 Mac、PC 或 Linux 电脑上，也可以运行在浏览器里。本页介绍它在各个平台上是怎么做到的、重载时会保留什么，以及设备、资源和日志、哪些内容无法重载、`asuka vet`、你可能遇到的问题，还有 Asuka 自己的测试和基准测试。

## asuka dev

```sh
asuka dev [flags] [dir]                     # 当前电脑：macOS、Windows 或 Linux
asuka dev --platform ios [flags] [dir]       # iOS 模拟器
asuka dev --platform ios --device iphone [dir]  # 连接着的 iPhone（或 iPad）
asuka dev --platform android [flags] [dir]   # Android 模拟器或手机
asuka dev --platform web [flags] [dir]       # 浏览器
```

`--platform` 可以是 `ios`、`android`、`mac`、`windows`、`linux` 和 `web` 之一；`asuka build` 也接受同样的参数。不指定时，应用会为你当前使用的电脑运行（或构建）。每次只能运行一个平台：要同时跑两个平台，就开两个终端。

`dir` 是应用所在的目录，默认是当前目录，里面应该是一个调用了 `app.Run` 的 `main` 包。可用的参数有：

- `--platform`：见上文。
- `--lang`：本次运行使用某种语言（如 `de`、`zh-Hans`），或者使用伪区域设置：`en-XA`（带重音符号且更长）或 `ar-XB`（从右到左）。`--dir rtl` 可以让任何应用按从右到左布局。参见 [多语言](i18n.md)。
- `--once`：只构建并运行一次，不监听文件变化。在桌面端，应用退出时 `asuka dev` 也随之退出；在手机上，它会构建包含 Go 代码的应用，安装并启动，然后直接退出，不显示日志；在浏览器中，它会为应用提供服务。
- `-v`：打印 Asuka 执行的命令。
- `--no-vet`：构建时不同时运行 [`asuka vet`](#asuka-vet)。

在手机上还有：

- `--device`：在 iOS 上可以是模拟器的名称或 UDID（默认使用已启动的 iPhone 模拟器，没有的话就选一个较新的 iPhone Pro 并启动它），也可以是已连接的 iPhone 或 iPad 的名称或 UDID，或者写 `iphone`，表示当前连接着的那一台（参见 [设备](#设备)）；在 Android 上是 adb 序列号或 AVD 名称（默认使用 adb 列出的第一台设备，没有的话就启动第一个 AVD）。
- `--team`：在 iPhone 上运行时，为播放器和应用签名的 Apple 开发者团队。
- `--no-hot`：不使用热重载：构建一个把 Go 代码包含在内的应用，每次保存都重新构建并重新安装，应用从头启动。

在浏览器中还有 `--port`、`--lan`（向局域网提供服务，供手机浏览器访问）和 `--no-open`，详见 [Web](web.md#asuka-dev---platform-web)。

## 在桌面端

在 macOS、Windows 和 Linux 上，应用的 Go 代码和平台宿主是同一个 Go 程序，以 `CGO_ENABLED=0` 为当前电脑构建。`asuka dev` 会构建并运行它；每次保存时都会重新构建，在当前应用仍显示在屏幕上的同时启动新版本。新版本启动后，Asuka 会停止正在运行的应用，旧应用在退出时保存 signal 的值，新版本接着运行并把这些值恢复过来（参见 [重载时会保留什么](#重载时会保留什么)）。一次重载耗时和 `go build` 差不多。构建失败时会打印错误并保留正在运行的应用；新版本一启动就退出（比如 `init` 中发生 panic）时也是如此。

在 macOS 上，系统会在程序第一次运行时对它进行检查，这可能让每次重载多出几百毫秒；出现这种情况时 Asuka 会提示你。在“系统设置 > 隐私与安全性 > 开发者工具”中添加你的终端应用，就能关闭对它所运行程序的检查（如果你用的是 tmux 或其他终端复用器，也要把复用器加进去）。

应用直接从原位置读取 `assets/`，所以修改资源后不需要复制。应用的数据（`store.Dir()` 以及其中的 SQLite 数据库）存放在 Asuka 缓存中该应用自己的 `data` 目录里（参见 [从头开始](#常见问题)），而不是已安装应用的数据目录，所以 `asuka dev` 和发布版本的数据互不共享。应用的输出（`fmt.Println`、`log`、panic）会打印到终端。

**macOS。** 在 Mac 上，`asuka dev` 会从 Asuka 缓存中的一个 `.app` 运行应用，这个 `.app` 和发布版本的一样：有 `Info.plist`（其中的名称、bundle ID、URL scheme、权限用途说明和语言都来自 `asuka.json`）和图标，并使用临时签名（ad hoc）。因此在系统看来，它就是这个应用本身，和从访达打开时一样：程序坞（Dock）显示的是它的名称和图标，通知归它自己所有，隐私权限提示（相机、定位、通讯录）也是以它的名义、带着它的用途说明弹出的，而不是以终端的名义。需要描述文件（provisioning profile）授权的包（推送、通过 Apple 登录、App Attest）只能在用描述文件签名的发布构建中使用，参见 [发布](shipping.md#macos)。

**Windows。** `asuka dev` 只能在 Windows 本机上运行；`asuka build --platform windows` 则可以在任何系统上构建。开发构建是一个控制台程序，所以输出会打印到终端；发布版本没有控制台。重载时，Asuka 通过它与每次运行保持的连接通知应用停止（Windows 无法向进程发送 Ctrl-C），应用在退出时保存状态。

**Linux。** 在 Linux 上，`asuka dev` 基于 GTK 4 和 libadwaita 运行应用，因此需要先安装它们（参见 [快速上手](getting-started.md#准备工作)），应用运行在你会话的显示服务器上，Wayland 或 X11 都可以。和发布版本一样，应用会使用 `asuka.json` 中的名称、ID、版本和强调色；Asuka 通过与每次运行保持的连接（或 SIGINT）让它停止，应用在退出时保存状态。在 Mac 或 PC 上，`asuka dev --platform linux` 能为 Linux 构建应用，但无法在本机运行它，除非 `ASUKA_LINUX_RUN` 指定了一条命令，能在一台带显示器、并且能以相同路径访问本机文件的 Linux 机器上运行程序。这通常是一台虚拟机，例如 `ASUKA_LINUX_RUN="orb -m ubuntu"`（OrbStack）或 `ASUKA_LINUX_RUN="limactl shell default"`（Lima）。Asuka 会针对那台机器的架构构建应用，并用这条命令运行它。

## 在手机上：播放器应用与构建的应用

iOS 开发需要 Mac；Android 开发需要 Mac、Windows 电脑或 Linux。默认情况下，`asuka dev` 在你的电脑上运行 Go 代码，视图则显示在**播放器应用**里。播放器是一个名为 Asuka 的应用（`dev.asuka.player`），它就是一个不含任何应用的平台宿主。播放器只需构建和安装一次，所有应用共用，只有它本身变化时（Asuka 发布了新版本）才会重新安装。Go 代码和播放器之间通过 socket 通信。

保存时，Asuka 会为你的电脑重新构建 Go 代码并重启它，大约一秒钟：新的进程启动时，旧进程仍在为播放器服务；旧进程停止后播放器会重新连接，并显示新代码的效果。整个过程不需要安装任何东西。

如果使用了 `--no-hot` 或 `--once`，或者应用用到了含平台代码的原生包（有 `Package.swift` 或 `android/`，参见 [原生代码](native.md)），Asuka 就会改为构建应用本身：像发布构建一样把 Go 代码打包进去，并使用应用自己的名称和 ID。

- **iOS**：宿主连同原生包的 Swift 代码只构建一次，之后只在有变化时才重新构建；每次保存都会重新构建 Go 代码、链接应用、为模拟器签名，并用 `xcrun simctl` 重新安装；在 iPhone 上则改为用开发证书签名，并用 `xcrun devicectl` 重新安装。
- **Android**：Gradle 构建应用（包括其中的 Kotlin 代码），宿主、Kotlin 代码和清单文件的每个版本只构建一次；每次保存都会重新构建 Go 库，用 `adb` 推送到应用的私有文件目录（调试版应用会从那里加载它），然后重启应用。

播放器拥有所有 Asuka 包可能需要的权限，并带有它自己的权限用途说明。`asuka.json` 中为应用本身配置的内容（名称、ID、URL `scheme`、`links`、`ios.usage`）只有构建出来的应用才具备：要测试这些，请使用 `--no-hot`。`accent` 强调色在播放器中同样生效。

## 重载时会保留什么

重载会保留应用中 signal 的值，你不需要为此写任何代码；播放器、桌面端和浏览器都是如此。应用停止时会把这些值保存到 `ASUKA_STATE` 环境变量指定的文件中（`asuka dev` 会把它设为 Asuka 缓存中该应用目录下的 `state.json`），下一次运行时，每个值会还给在相同位置创建的 signal。所谓相同位置，指的是相同的调用路径（创建它的那些函数，而不是行号）、相同的名称和相同的类型。

这些位置的名称来自你的源代码，Asuka 在构建时会读取源码：闭包按它在所属函数中的角色命名（赋给的变量或字段、作为参数传入的调用、`go` 或 `defer`），signal 则按它所赋给的变量或字段命名。因此，在 signal 周围编辑代码不会让它丢失值，比如在创建它的闭包之前添加一个处理函数，或者在它之前再添加一个同类型的 signal：

```go
func Counter() ui.Node {
	onReset := func() { ... }  // 新增的：count 的值仍会保留
	step := signal.New(1)      // 新增的：同上
	count := signal.New(0)
	...
}
```

以下内容不会保留：

- 导航状态：重载后从根页面开始。
- Asuka 自己创建的 signal，以及 JSON 无法表示的值（函数、未导出的字段）。
- 类型发生了变化的 signal。
- 在带有 `//asuka:reset` 注释的文件中创建、且该文件被编辑过的 signal：如果你希望某个页面在修改时从头开始，就把它放在这样的文件里。
- 被重命名的 signal，或者被移到另一个函数或闭包里的 signal，都会从头开始；在同一位置以相同名称创建的多个 signal（比如在循环里），按创建顺序区分。

构建出来的手机应用（`--no-hot`、`--once`、使用了原生代码）每次重载都从头启动。

Go 代码写入的文件（`asuka/sdk/store`、`asuka/sdk/sqlite`）在使用播放器时和在桌面端一样，都位于你的电脑上：`store.Dir()` 是 Asuka 缓存中该应用自己的 `data` 目录，每个平台一份，就像两台设备各有各的数据一样。删除它就能让应用在没有数据的状态下重新开始。在浏览器中，这些文件属于页面（存放在 IndexedDB），参见 [Web](web.md#应用的文件)。

## 重载

Asuka 会监听应用所在的目录及其模块的目录，依靠系统的文件变化通知（FSEvents、inotify、ReadDirectoryChangesW；不支持时改用轮询）。监听的内容包括：

- Go 文件和 `go.mod`（不含 `_test.go`）；
- Swift 和 Kotlin 文件；
- 语言目录（catalog）；
- `assets/` 下的文件；
- 使用 `--platform web` 时，还包括应用的脚本（`web/*.js`），以及在 Asuka 仓库的检出中 Web 宿主的脚本（`platform/web/src`）：这些文件变化时页面会直接重新加载，不需要构建。

以 `.` 开头的目录以及 `testdata`、`build` 和 `node_modules` 会被跳过。构建失败时会打印错误并保留正在运行的应用，下次保存时再重试。

`asuka.json` 的字段只在 `asuka dev` 启动时读取。修改之后，需要停止并重新运行它；它会重新构建并重新安装有变化的部分。

在手机上，原生代码只有在构建出来的应用中才能重载，Asuka 会在每次保存时重新构建并重新安装。Kotlin 代码变化时会重新运行 Gradle，比修改 Go 代码要慢。桌面应用中用 Go 写的原生代码和其余代码一样重载；Mac 应用中原生包的 Swift 代码会在变化时重新构建（一两秒），然后应用重新运行，signal 的值会保留下来。

## 设备

**你的电脑**：在 Mac、PC 或 Linux 上直接运行 `asuka dev` 即可。

**iOS 模拟器**与 Mac 共享网络，所以播放器可以直接连接到 `asuka dev`。

**Android 模拟器**通过 `10.0.2.2` 访问你的电脑，播放器就连接到这个地址。

**通过 USB 连接的 Android 手机**：在开发者选项中打开 USB 调试，连上电脑并同意弹出的授权提示，然后确认 `adb devices` 能列出它。`asuka dev --platform android` 使用 adb 列出的第一台设备，不论是手机还是模拟器；连接了多台设备时，用 `--device` 加序列号指定。手机通过 `adb reverse` 访问你的电脑，Asuka 会为 7461 端口（应用）和 7462 端口（资源）设置好转发。开发时，Asuka 只为 64 位手机和模拟器构建（arm64-v8a、x86_64）。

**iPhone**（或 iPad），通过 USB 或 Wi-Fi 连接，需要 Xcode 15 或更新版本：把它连上 Mac，解锁后点“信任”；再在它的“设置 › 隐私与安全性”中打开“开发者模式”（手机连接过 Xcode 之后才会出现这个选项，打开后手机会重启）。之后运行 `asuka dev --platform ios --device iphone` 就会在它上面运行；连着多台设备时，用 `--device` 加上设备名称或 UDID 指定（`xcrun devicectl list devices` 可以列出它们）。在 Xcode 的 Devices 窗口里与这台 Mac 配对过的手机，拔掉线后也能通过 Wi-Fi 使用。

- **签名**：播放器和应用都由你的团队用开发证书签名。团队依次取自 `--team`、`ASUKA_TEAM_ID`、`asuka.json` 的 `ios.team`，都没有的话就用钥匙串里 Apple Development 证书所属的团队。第一次运行时，Asuka 会让 Xcode 生成所需的开发描述文件（`xcodebuild -allowProvisioningUpdates`），顺带把这台手机注册到团队，并打开应用用到的功能（推送、App Groups 等）。这一步使用 Xcode “设置 › 账户”中登录的 Apple ID；如果设置了 `ASUKA_ASC_KEY_ID`（或 `ios.appStoreConnect`），则改用 [App Store Connect API 密钥](shipping.md)。团队已有的描述文件如果合适（包括通配符描述文件），就直接使用。
- **播放器**在手机上属于你的团队自己，bundle ID 是 `dev.asuka.player.` 加团队 ID，因为一个 App ID 只能属于一个团队。它的分享面板、小组件和各项功能取决于团队拥有的功能：免费团队没有推送和 App Groups，播放器就不带这些功能，并会提示你。
- **连接到 Mac**：通过 USB 时，由 Mac 经数据线主动连接播放器（和 Xcode 一样通过 usbmuxd），完全不走网络。没有数据线时，播放器会连接 Mac 在局域网上的地址，以及它的 Bonjour 名称（Mac 换了地址也能找到）：手机必须和 Mac 在同一个网络里，而且 iOS 会询问一次是否允许播放器查找本地网络中的设备，请选择允许；之后也可以在手机的“设置 › App › asuka › 本地网络”中打开。如果播放器一直没有连上，Asuka 会告诉你该检查什么。无论哪种方式，每次保存重启 Go 代码后，播放器都会自动重新连接。
- **构建出来的应用**（`--no-hot`、`--once`、含原生代码的应用）也按同样的方式签名，使用应用自己的 ID；每次保存都会重新构建、签名，并用 `xcrun devicectl` 重新安装和启动应用。

**手机浏览器**：`asuka dev --platform web --lan` 会向局域网提供应用，并打印可以打开的地址。

## 资源

在桌面端，应用直接从原位置读取 `assets/`。使用播放器时，应用的 `assets/` 目录仍留在你的电脑上：Go 进程通过 HTTP 提供这些文件，播放器从那里加载 `ui.Image` 的资源路径以及 `assets/fonts` 中的字体。在浏览器中，则由开发服务器提供这些资源。修改任何资源都会让应用重新加载。

构建出来的手机应用会把资源打包在应用内部：iOS 上每次构建时复制进应用；Android 上在资源有变化时推送到应用的私有文件目录。

## 日志

- **在桌面端和播放器中**，终端会显示 Go 进程的输出：`fmt.Println`、`log`、panic。
- **在浏览器中**，终端还会显示应用的输出和页面的错误。
- **iOS 上构建的应用**：应用的输出（Go 和 Swift 的 `print`）在模拟器上通过模拟器的控制台显示，在 iPhone 上通过 `devicectl` 的控制台显示。
- **Android 上构建的应用**：查看应用的 logcat。Android 会丢弃进程的标准输出，所以 Go 代码的输出会写到 logcat，标签为 `asuka`。
- 在手机上使用 `--once` 时不会显示日志：请用“控制台”应用（Console.app）或 `adb logcat` 查看。

## 应用切到后台

在使用播放器时离开应用（比如去相机应用拍照，或者去浏览器登录）不会结束会话。播放器在后台时，系统可能会把它冻结，所以 Go 代码等待它回应的时间会从通常的十秒延长到最多十分钟。当 Android 切断后台应用的网络时，会话会保留三分钟；播放器在这段时间内回来，就能接着之前的会话继续，页面也保持原样。如果播放器回来得更晚，应用会从根页面重新启动。

停止 `asuka dev`（Ctrl-C）时也会停止播放器，这样它就不会加入下一个会话（那可能是另一个应用的会话）。在桌面端，停止 `asuka dev` 会停止应用。

## asuka vet

```sh
asuka vet [flags] [packages]
```

`asuka vet` 用来找出那些能编译、能运行，但行为不对的错误。它会报告：

- **reads**：在组件构建视图时读取了 signal，导致显示的内容永远不会更新，例如在组件函数体中写 `ui.StaticText(fmt.Sprint(count.Get()))` 或 `if open.Get() { ... }`。参见 [什么会更新](concepts.md#哪些部分会更新)。
- **mainthread**：在 goroutine 中写入了 signal，而不是在 `ui.Post` 中写入。参见 [主线程](concepts.md#主线程)。
- **memowrites**：在 memo 内部写入了 signal，这会导致 panic。
- **routes**：没有任何路由能处理的常量 href（拼错的链接、指向已经挪走的路由），它会显示“未找到”页面；以及传给 `ui.HrefOf` 的值的个数不对。参见[类型安全的链接](navigation.md#类型安全的链接)。

```
$ asuka vet
main.go:14:23: count.Get() is read once, while App builds its views, so the constant made from it never changes: give the prop signal.Func(func() T { ... }) instead, or use count.Peek() to read it once on purpose
```

它只报告确定有问题的地方，所以每条报告都值得修复。每条消息都会给出修复方法，以及如何表明你是有意这样写的：有意只读取一次时用 `Peek`，对访问器则用 `signal.Untrack`，这两种写法它都不会报告。

默认检查的包是 `./...`，和 `go vet` 的规则一样。参数也与 `go vet` 一致（`-tags=...`、`-json`），另外每项检查都有一个对应的参数：`-reads=false` 会关闭 reads 检查。只要报告了问题，`asuka vet` 就以状态码 1 退出，便于在 CI 中使用。它与 `go vet -vettool=$(which asuka)` 运行的检查相同，但后者不会运行 `go vet` 自己的检查。

`asuka dev` 会在启动时和每次保存后在后台运行它，并把新发现的问题作为警告打印出来；`asuka build` 会在构建前运行它。两者都不会因为发现问题而中止。它与构建并行运行，耗时一两秒；`--no-vet` 可以在这两个命令中关闭它。

## 快照与脚本

桌面应用支持几个环境变量，可以让应用显示某个状态并截取下来，方便你肉眼检查改动或在 CI 中使用。在应用目录中用 `go run .` 运行应用：

```sh
ASUKA_SNAPSHOT=shot.png go run .                  # 窗口显示后把它保存为 PNG，然后退出
ASUKA_SNAPSHOT=shot.png ASUKA_DUMP=1 go run .      # 同时把视图树写入 shot.png.txt
ASUKA_SCRIPT="tap:Add;tap:Add" ASUKA_SNAPSHOT=shot.png go run .   # Windows、Linux：先执行这些步骤
```

- `ASUKA_SNAPSHOT` 会在窗口显示后（以及脚本执行完后）把窗口保存为 PNG，然后退出；设置 `ASUKA_SNAPSHOT_STAY=1` 可以让应用继续运行。
- `ASUKA_DUMP` 会在截图旁边写出视图树：每个视图的类型、frame（位置和尺寸）以及显示的内容。
- `ASUKA_SCRIPT`（Windows 和 Linux）会在截图前执行一系列以 `;` 分隔的步骤：`tap:<text>`（点按显示该文字的视图：按钮标题、文本、标签页）、`type:<text>`（在获得焦点的输入框中输入）、`back`、`key:<shortcut>`（如 `ctrl+shift+k`）、`menu:<item>`（菜单栏中的菜单项）、`escape`、`close`（关闭当前窗口）、`wait:<duration>`（如 `500ms`）和 `snap:<file>`。每个宿主还支持另外几个步骤，参见各自的 `automation.go`。

## 常见问题

- **`no iOS simulator SDK (install Xcode)`**：安装 Xcode，打开一次，然后在它的 Components 设置中添加 iOS 模拟器运行时。`no iPhone simulator; add one in Xcode` 和 `no simulator "..."` 表示没有该名称的模拟器：可以用 `xcrun simctl list devices` 列出所有模拟器。
- **iPhone 没有准备好**：`... isn't paired with this Mac`（连上数据线，解锁后点“信任”）、`... isn't reachable`（连上数据线，或在 Mac 所在的 Wi-Fi 下唤醒它）、`... has Developer Mode off`（“设置 › 隐私与安全性 › 开发者模式”）、`... is locked`（解锁手机）。明明连着手机却提示 `no iPhone or iPad is connected` 时，检查 `xcrun devicectl list devices` 能否列出它。
- **`Xcode couldn't make the development profiles`**：在 Xcode 中登录团队的 Apple ID（Xcode › 设置 › 账户），或者设置 App Store Connect API 密钥；如果 bundle ID 已经属于另一个团队，就需要在 `asuka.json` 里换一个 `id`。`the keychain has Apple Development certificates of N teams`：用 `--team` 选一个团队。
- **`the player hasn't connected yet`**：使用 USB 时，检查数据线并解锁手机；使用 Wi-Fi 时，让手机连上 Mac 所在的网络，允许播放器访问本地网络，并在 Mac 的防火墙中允许 `asuka` 接受连接（“系统设置 › 网络 › 防火墙”）。
- **`no Android SDK: install Android Studio or set ANDROID_HOME`**：Asuka 会依次在 `ANDROID_HOME`、`ANDROID_SDK_ROOT` 中查找，然后是 Mac 上的 `~/Library/Android/sdk`、Windows 上的 `%LOCALAPPDATA%\Android\Sdk` 和 Linux 上的 `~/Android/Sdk`。
- **`no Android NDK`**：在 Android Studio 的 SDK Manager 中安装一个（SDK Tools 下的 NDK）。Asuka 会选用最新的、带有你这台电脑可用版本的 NDK（任何 Windows 电脑上都是 `windows-x86_64`，Arm 电脑也一样）。
- **`no Android device or emulator`**：在 Android Studio 的 Device Manager 中创建一个 AVD，或者连接一部手机。对于 Asuka 自己启动的模拟器，它最多会等四分钟让模拟器完成启动（超时会报 `the emulator didn't boot`）。
- **Java**：Gradle 需要 JDK：优先使用 `JAVA_HOME`，否则使用 Android Studio 自带的 JDK，无论 Android Studio 是用安装程序还是 JetBrains Toolbox 装的（在 Windows 上，位于 Program Files 或 `%LOCALAPPDATA%\Programs` 下）。
- **第一次 Android 构建很慢**：Gradle 要下载它自己和 Android 的库，这只发生一次。
- **`the player connected ... and hasn't shown the app`**：播放器连接上了你的 Go 代码，但始终没有显示应用，因为它的主线程卡住了。Asuka 会把播放器的 goroutine 堆栈保存到 Asuka 缓存中该应用目录下的 `player-stacks.txt`（路径会打印出来），提交 bug 时请附上它。重新启动播放器，或者用 `--no-hot` 运行，构建一个包含 Go 代码的应用。
- **手机上装有发布构建**：为了安装调试版，由 `asuka build` 安装的应用（签名密钥不同）会被卸载，其数据也会一起删除。
- **`asuka dev --platform windows runs the app on this PC`**：`asuka dev` 只能在 Windows 上运行 Windows 应用。在其他系统上可以用 `asuka build --platform windows` 构建，再把生成的文件夹复制到 PC 上。
- **Linux 上提示 `... isn't installed`**：宿主需要 GTK 4.14 和 libadwaita 1.5 或更高版本（`the Linux host needs GTK 4.14 and libadwaita 1.5 or later`），某些包还需要各自的库：`ui.WebView` 需要 WebKitGTK 6.0，视频和音频需要 GStreamer，`asuka/sdk/secure` 需要 libsecret。错误信息会指出缺少的是哪个，用发行版的包管理器安装即可。
- **Linux 应用打不开**：它需要一个显示服务器。通过 SSH 或在 CI 中运行时，请在 Xvfb 下运行（`xvfb-run -a`），对于需要 D-Bus 的功能，还要加上会话总线（`dbus-run-session`）。
- **CLI 与应用使用的 Asuka 版本**：CLI 自带宿主的 Swift 和 Kotlin 代码，而应用的 `go.mod` 决定了 Asuka 的 Go 代码版本。请安装与应用所需版本一致的 CLI。
- **从头开始**：Asuka 的构建产物、缓存、保留的状态以及各应用的开发数据都存放在 Asuka 的缓存目录中：Mac 上是 `~/Library/Caches/asuka`，Linux 上是 `~/.cache/asuka`，Windows 上是 `%LocalAppData%\asuka`。删除它就能在没有任何状态和数据的情况下全部重新构建。

## 测试

如果要在 `go test` 里测试你的应用界面，不需要设备或显示器，请看[测试](testing.md)。Asuka 自己的仓库会通过各平台的真实视图来测试每个宿主。

```sh
go test ./...                                      # 核心，基于 host/fake
go test ./host/darwin                              # macOS：真实的 AppKit 视图
go test ./host/windows                             # Windows：真实的窗口
xvfb-run -a dbus-run-session go test ./host/linux  # Linux：真实的 GTK 控件
swift test --package-path platform/ios             # Swift 宿主的解码器
node --test platform/web                           # Web 宿主的解码器
```

`go test ./host/darwin -run Conformance` 和 `go test ./host/windows -run Conformance` 会在窗口截图中测量宿主为某个样式绘制的效果（边缘、圆角、边框、阴影、渐变），检查是否正确；下面的 e2e 测试在 iOS 模拟器、Android 模拟器和无头 Chrome 上做同样的检查。

### GUI 测试

`e2e/` 中的测试会在每个平台上运行示例，通过调试版宿主的自动化接口驱动：

```sh
go test -tags e2e ./e2e -run IOS       # 已启动的模拟器，没有则用较新的 iPhone
go test -tags e2e ./e2e -run Android   # asuka dev 选中的设备
go test -tags e2e ./e2e -run Web       # 无头 Chrome（ASUKA_CHROME：它的路径）
xvfb-run -a dbus-run-session go test -tags e2e ./e2e -run Linux  # GTK 4，在 Linux 上
```

每个测试先用 `asuka dev --once` 构建一个示例（包含 Go 代码的调试构建），然后点按、输入并读取屏幕内容：iOS 上通过通知，Android 上通过广播，Web 上通过开发服务器（`platform/web/src/automation.js`），Linux 上通过 `ASUKA_COMMANDS` 指定目录中的文件（`host/linux/automation.go`）。计数器、笔记、聊天、导航和多语言这几个示例都有测试，各平台的测试内容相同。测试会等待屏幕显示出预期的内容，最多等一分钟，而不是固定等待一段时间。

iOS 测试需要 Xcode（`xcrun`）；Android 测试需要 `asuka dev --platform android` 所需的 SDK 和 NDK（`adb` 优先从 `PATH` 中找，找不到就用 SDK 里的），在 Mac、PC 或 Linux 上都能运行；Linux 测试需要一个显示服务器（CI 中用 Xvfb 的）和一个会话总线。缺少这些条件时，相应的测试会被跳过。macOS 和 Windows 目前还没有 e2e 测试，它们的宿主测试会直接驱动真实的窗口。

## 基准测试

`bench/` 把 Asuka 与 SwiftUI 和 Jetpack Compose 进行对比：用每种框架编写同一个应用，在发布构建中逐帧计时。

```sh
go run ./bench/run -ios        # 在 iOS 模拟器上运行 asuka 和 SwiftUI
go run ./bench/run -android    # 在正在运行的 Android 模拟器或设备上运行 asuka 和 Compose
```

`-runs` 设置每种实现运行的次数（默认 5 次），中位数结果写入 `bench/results`。[`bench/README.md`](../../bench/README.md) 介绍了测试场景、测量方法和最新结果。

## asuka.json

`asuka dev` 和 `asuka build` 读取的是同一个 `asuka.json`。每个键的说明参见 [asuka.json 参考](asuka-json.md)，发布时它们如何配合参见 [发布](shipping.md#asukajson)。

# 快速上手

使用 Asuka 需要先装好 Go，再为你要运行的每个平台准备好对应的工具。之后，一条命令就能创建应用，另一条命令就能运行它，并且每次保存都会自动重新加载。

## 准备工作

所有平台都需要 **Go 1.26** 或更高版本。此外，按你要支持的平台准备：

- **iOS**：一台装有 Xcode 的 Mac，以及 iOS 模拟器或一部 iPhone。
- **Android**：一台 Mac、一台 Windows 10 或 11 电脑（x64 或 Arm）或者 Linux 电脑，装好 Android Studio，并在它的 SDK Manager 中安装 NDK（SDK Tools → NDK）；再从 Device Manager 创建一个 Android 模拟器，或者准备一部开启了 USB 调试的手机。Asuka 会在 SDK 的默认安装位置或 `ANDROID_HOME` 中查找 SDK，并使用 Android Studio 自带的 Java。在 Windows 上也只需要这些：Asuka 直接用 NDK 自带的 clang 编译 Go 代码，用 `gradlew.bat` 运行 Gradle。在 Arm 电脑上，NDK 和 Android 模拟器都是 x64 程序，会以模拟方式运行，模拟器需要使用 x86_64 系统镜像。
- **macOS**：一台运行 macOS 11 或更高版本的 Mac。只是运行应用的话，不需要别的东西；发布构建，以及自带 Swift 代码（`macos/*.swift`）的应用，需要 Xcode 命令行工具（`xcode-select --install`），装了 Xcode 就已经包含了。
- **Windows**：一台运行 Windows 10 或 11 的 PC，x64 或 Arm 均可。不需要别的东西：Asuka 直接从 Go 调用 Win32。`ui.WebView` 和 `ui.Video` 依赖 Microsoft Edge WebView2 Runtime，Windows 11 自带，大多数 Windows 10 电脑上也已经有了。
- **Linux**：GTK 4.14 和 libadwaita 1.5 或更高版本（Ubuntu 24.04、Fedora 40、Debian 13 及之后的版本都已自带），Wayland 和 X11 均可：

  ```sh
  sudo apt install libgtk-4-1 libadwaita-1-0   # Debian、Ubuntu
  sudo dnf install gtk4 libadwaita             # Fedora
  sudo pacman -S gtk4 libadwaita               # Arch
  ```

  `ui.WebView` 需要 WebKitGTK 6.0（`libwebkitgtk-6.0-4`），`ui.Video` 和音频需要 GStreamer；如果某个包依赖的库没有安装，它会返回一个错误，告诉你缺的是哪个库。
- **Web**：一个浏览器就够了。应用只靠 Go 就能编译成 WebAssembly。

桌面端的宿主调用平台接口时不需要 cgo，因此 `asuka build` 在任何系统上都能构建 Windows 或 Linux 应用；Mac 应用则需要在 Mac 上构建。

## 安装命令行工具

```sh
go install github.com/egoist/asuka/cmd/asuka@latest
```

## 创建应用

```sh
asuka new hello
cd hello
```

`hello` 目录里有：

- `main.go`：应用本身，即一个返回界面的函数，交给 `app.Run` 运行。
- `asuka.json`：应用的名称、ID（同时用作 bundle ID、application ID 和桌面应用 ID）和版本号，以及[各应用商店和各平台](shipping.md)需要的其他信息。
- `go.mod`。

## 运行

```sh
asuka dev                      # 在本机运行：Mac、PC 或 Linux
asuka dev --platform ios        # iOS 模拟器
asuka dev --platform ios --device iphone  # 连接着的 iPhone
asuka dev --platform android    # Android 模拟器或手机
asuka dev --platform web        # 你的浏览器
```

保存 Go 文件后，应用片刻间就会重新加载，signal 里的值都会保留下来。应用的输出（`fmt.Println`、panic）会显示在终端里。

- **在电脑上运行**（`asuka dev`，或 `--platform mac`、`windows`、`linux`）时，应用和它的宿主是同一个 Go 程序：Asuka 构建它、在窗口中运行它，每次保存后再重新构建并运行。
- **在手机上运行**时，第一次会先构建并安装 Asuka 的播放器应用，由它来显示你的应用的视图；你的 Go 代码则在电脑上运行，并与播放器通信，所以保存后无需重新安装就能重新加载。`--device` 用来指定 iOS 模拟器或 iPhone（名称或 UDID，写 `iphone` 则表示连接着的那台）或 Android 设备（adb 序列号或 AVD 名称）。`--no-hot` 会像发布构建那样，把 Go 代码直接编进应用；`--once` 则把代码编进应用后只启动一次。
- **在浏览器中运行**时，Asuka 会在 `http://localhost:8790/` 上启动服务并自动打开页面，每次保存都会刷新。加上 `--lan` 可以把应用开放给局域网，方便用手机浏览器试用。详见 [Web](web.md)。

要在 iPhone 真机上运行，先把它连上 Mac，解锁后点“信任”，在“设置 › 隐私与安全性”中打开“开发者模式”，并在 Xcode 中添加你的 Apple ID（“设置 › 账户”）。然后运行 `asuka dev --platform ios --device iphone`：第一次它会用你的团队为播放器签名并安装，之后每次保存都会重新加载，USB 和 Wi-Fi 都可以。参见 [设备](development.md#设备)。

更多内容见[开发](development.md)：重新加载时会保留什么、设备、日志，以及你可能遇到的问题。

## 修改应用

`asuka new` 生成的 `main.go` 是一个计数器：

```go
func App() ui.Node {
	count := signal.New(0)
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Set(count.Peek() + 1) }}),
	)
}
```

- **signal** 保存应用的状态：用 `signal.New` 创建，用 `Get` 读取（并在值变化时得到更新），用 `Set` 写入。`signal.Func` 则是由其他 signal 计算出来的值。
- **组件**是返回 `ui.Node` 的 Go 函数。组件只运行一次：之后会变化的，只有那些读取了 signal 的部分。
- **样式**基于 flexbox，以点（pt）为单位：`Grow`、`Gap`、`Padding`、`Align`、`Justify`，以及外观相关的 `Background`、`CornerRadius`、`Shadow` 等。不设置任何样式的应用，看起来就和该平台上的其他应用一样。

同一份代码就是所有平台上的应用。在桌面端，`app.RunWindow` 可以设置主窗口的标题和尺寸，`ui.Window` 和 `ui.MenuBar` 则用来添加窗口和菜单栏（见[导航](navigation.md)）。

## 应用的文件结构

- `assets/`：图片和其他文件，例如 `ui.Image(ui.ImageProps{Source: ui.Val("photo.jpg")})`；字体放在 `assets/fonts` 中。
- `locales/`：应用在各语言下的文案（见[多语言](i18n.md)）。
- 你自己的原生包：声明了模块或视图的 Go 包，由各平台在自己的项目中实现（Swift package、Gradle 模块、JavaScript，桌面端则用 Go）。`asuka native new ./battery` 可以创建一个；详见[原生代码](native.md)。

## 下一步

- [`examples`](../../examples)：[`gallery`](../../examples/gallery) 展示了所有组件；[`notes`](../../examples/notes) 用 SQLite 保存笔记；[`navigation`](../../examples/navigation) 演示了导航栈、标签页和弹出面板。
- [发布](shipping.md)：为 App Store、Google Play、Mac、Windows、Linux 和 Web 构建应用。

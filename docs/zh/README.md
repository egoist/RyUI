# Asuka 文档

<!-- repository-only:start -->
[English](../README.md) · [日本語](../ja/README.md)
<!-- repository-only:end -->

Asuka 让你用 Go 编写应用，并在 iOS、Android、macOS、Windows、Linux 和 Web 上以各平台的原生控件呈现。应用的界面就是一些返回视图的 Go 函数：Asuka 用各平台自己的控件把它们显示出来；某个 signal 变化时，只有读取了它的视图才会更新。

```go
func Counter() ui.Node {
	count := signal.New(0)
	label := signal.Func(func() string {
		return fmt.Sprintf("Count: %d", count.Get())
	})
	return ui.SafeArea(ui.Style{Align: ui.AlignCenter, Justify: ui.JustifyCenter},
		ui.Text(label),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Add"),
			Variant: ui.ButtonFilled,
			OnPress: func() { count.Set(count.Peek() + 1) },
		}),
	)
}

func main() { app.Run(Counter) }
```

## 工作原理

你的应用完全是 Go 写的：组件、状态以及其余所有代码。Asuka 的核心负责运行组件、对视图进行布局（flexbox 和 grid，用 Go 实现），然后把发生变化的部分按帧打成一个批次，发给所在平台的**宿主（host）**。宿主再去创建和更新平台自己的视图：

| 平台 | 宿主 | 使用的视图 |
| --- | --- | --- |
| iOS | Swift，应用的 Go 代码编译成库嵌入其中 | UIKit |
| Android | Kotlin，应用的 Go 代码编译成库嵌入其中 | Android Views 和 Material 3 |
| macOS | Go，与应用在同一进程中（通过 purego 调用 AppKit） | AppKit |
| Windows | Go，与应用在同一进程中（通过 `syscall` 调用 Win32） | Win32 和通用控件，绘制成 Windows 11 的样式 |
| Linux | Go，与应用在同一进程中（通过 purego 调用 GTK） | GTK 4 和 libadwaita |
| Web | JavaScript，应用的 Go 代码编译为 WebAssembly | 页面的 DOM 和浏览器自带的控件 |

一个文本输入框，在各平台上分别就是 `UITextField`、`EditText`、`NSTextField`、`EDIT` 控件、`GtkEntry` 或 `<input>`；页面的导航栈则分别对应 `UINavigationController`、带返回手势的 Material 顶部应用栏、Mac 窗口中带返回按钮的工具栏，或 libadwaita 的导航视图。因此，一个不设置任何样式的应用，看起来就和该平台上的其他应用一样，无障碍、文本输入和滚动也都和它们表现一致。桌面端的宿主直接从 Go 调用系统库，不需要 cgo，所以 Windows 或 Linux 应用只靠 Go 就能构建，而且可以在任何系统上构建。

状态保存在 signal 里。组件只运行一次；signal 变化时，只有读取了它的那些属性会被重新发送，不需要对整棵树做 diff。

同一个应用可以在所有平台上运行。在手机上，它铺满整个屏幕；在桌面端，它是一个用户可以调整大小的窗口，还可以打开更多窗口（`ui.Window`，手机上没有这个概念），并拥有带键盘快捷键的菜单栏（`ui.MenuBar`，在手机上会被忽略）。如果某个包提供的功能在某个平台上并不存在（比如 Mac 上的传感器或主屏幕小组件），它在那个平台上会告诉你“不可用”，应用可以据此检查，在缺少这项功能时照常运行。

Asuka 需要 Go 1.26 或更高版本。应用可运行在 iOS 16、Android 13（API 33）、macOS 11、Windows 10 及以上版本，以及装有 GTK 4.14 和 libadwaita 1.5 或更高版本的 Linux 上，也可以在当前主流浏览器中运行。开发时，`asuka dev` 会运行你的应用，并在你保存时重新加载：可以跑在 iOS 模拟器、Android 模拟器或手机上（Go 代码在你的电脑上运行），也可以直接跑在电脑上，或者跑在浏览器里。

## 指南

- [快速上手](getting-started.md)：安装命令行工具，创建一个应用，并在手机、模拟器、电脑或浏览器上运行它。
- [核心概念](concepts.md)：组件、signal、owner、主线程和环境。
- [布局与样式](layout.md)：flexbox、尺寸、安全区域、颜色、文字、阴影和渐变。
- [组件](components.md)：文本、按钮、输入框、图片、列表、视频、网页视图和绘图。
- [导航](navigation.md)：导航栈、标签页、弹出面板、警告框和菜单，窗口和菜单栏，打开应用的链接，以及从其他应用分享进来的内容。
- [动效与手势](motion.md)：动画、过渡、共享元素、手势、按键和触感反馈。
- [无障碍](accessibility.md)：标签、角色、自定义操作、播报，以及可缩放的文字。
- [多语言](i18n.md)：支持复数的翻译文案，按用户习惯显示的数字和日期，从右到左的布局，以及切换语言试用。
- [数据](data.md)：跨启动保留的值、支持实时查询的 SQLite、安全存储，以及监听文件变化。
- [设备与服务](device.md)：各平台上的相机、扫码、照片与文件、预览、打印与 PDF、邮件和短信编辑界面、定位、通知与推送、音频、正在播放与录音、网络、媒体库、后台任务、登录与生物识别、屏幕与启动屏、地图、应用商店服务、传感器、图片编辑、语音、通讯录与日历、主屏幕小组件与快捷方式，以及崩溃报告。
- [应用内购买](purchases.md)：通过 App Store、Google Play 和 Microsoft Store 销售消耗型、非消耗型商品和订阅，处理交易、恢复购买、在服务器上验证，以及测试。
- [NFC 标签](nfc.md)：读写标签上的 NDEF 消息、标签的 ID 和原始命令、iOS 的系统面板与 Android 的读卡器模式、Web NFC，以及能打开应用的标签。
- [蓝牙](bluetooth.md)：低功耗蓝牙设备的扫描或选择、连接、读写和订阅；应用作为外围设备进行广播和提供服务；后台运行；以及各平台上的情况。
- [桌面端](desktop.md)：托盘图标和程序坞（Dock）菜单、后台运行的应用、全局快捷键、文件对话框和文件管理器、打开文件、单实例、退出、最近文档、开机启动、更新、显示器、电源，以及应用图标上的进度。
- [插件](plugins.md)：为各平台预先构建好、随应用一起发布的原生库包，SQLite 就是其中之一。
- [原生代码](native.md)：原生包——在 Go 里声明带类型的模块和视图，在各平台自己的项目里实现：iOS 和 macOS 用 Swift，Android 用 Kotlin，Web 用 JavaScript，Windows 和 Linux 用 Go。
- [开发](development.md)：深入介绍各平台上的 `asuka dev`、设备、`asuka vet`、测试和常见问题。
- [测试](testing.md)：在 `go test` 里测试应用的界面，不需要设备或显示器：点击、输入并检查屏幕上显示的内容，还有快照和等待应用的 goroutine。
- [打包与发布](shipping.md)：面向 App Store 和 Google Play 的构建、签名并公证的 Mac 应用、Windows 文件夹、Linux 压缩包与 Flatpak 清单、桌面应用的签名更新，以及 `asuka.json` 中对应用的描述。
- [Web](web.md)：同一个应用以 WebAssembly 加页面 DOM 的方式在浏览器中运行；`asuka dev --platform web`、可部署的静态站点、嵌入到其他页面，以及目前还不支持的功能。

## 参考

- [asuka.json](asuka-json.md)：应用 `asuka.json` 里的每一个键，它的类型、默认值，以及在各个平台上的作用。
- [与 SwiftUI 和 Expo 对比](parity.md)：Asuka 已经有什么，还缺什么。
- [Asuka 与 MyGo](mygo.md)：平台原生控件与框架自绘界面、各自能运行在哪里（MyGo 的手机支持即将推出），以及什么时候该选哪个。
- Go API: `go doc -all github.com/egoist/asuka/ui`，以及模块中的其他包。

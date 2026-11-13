# 桌面端

除了窗口和菜单栏（见[导航](navigation.md#窗口)），桌面端还有许多手机上没有的东西：托盘图标和程序坞（Dock）菜单、浮出面板、在后台常驻的应用、全局快捷键、面板与启动器、查找已安装的应用、鼠标穿透窗口、文件对话框与文件管理器、应用打开的文件、单实例、退出、最近使用的文档、基于文档的应用、登录时启动、自动更新、“正在播放”与媒体键、显示器、电源、应用图标上的进度，以及系统外观。如果想跟踪文件和文件夹的变化（比如应用正在展示的文件夹，或其他应用保存的文档），请参阅[监视文件](data.md#监视文件asukasdkwatch)，这项功能在手机和 Web 上也有。

上面每一项都是一个包，或是 `ui`、`app` 中的一个函数，在 Mac、Windows 和 Linux 上都能用。手机或浏览器上没有某项功能时，会明确告诉你不支持（返回 `ErrUnsupported`、`ErrNotAvailable`，或者 `Supported()` 为 false），这样应用就可以只在支持的平台上提供它。

## 托盘图标与程序坞菜单

在组件里调用 `tray.Show`，会在这个组件挂载期间往托盘里放一个图标：在 Mac 上是菜单栏里的 `NSStatusItem`，在 Windows 上是通知区域里的图标，在 Linux 上是 StatusNotifierItem（KDE、Xfce、Cinnamon、MATE、Budgie 都支持，GNOME 需要安装 AppIndicator 扩展）。

```go
tray.Show(tray.Props{
	Symbol:  ui.Val("moon.zzz"),
	Title:   remaining, // 一个 signal：在图标旁显示 "25:00"
	Tooltip: ui.Val("Focus timer"),
	Menu: signal.Static([]ui.MenuItem{
		{Title: "Start", OnPress: start},
		{Title: "Length", Submenu: []ui.MenuItem{
			{Title: "25 minutes", OnPress: func() { length.Set(25) }},
			{Title: "50 minutes", OnPress: func() { length.Set(50) }},
		}},
		ui.MenuSeparator,
		ui.MenuQuit,
	}),
})
```

它的 `Image` 或 `Symbol`（在 Mac 上是模板图像，会跟随菜单栏的外观变化）、`Title`（Mac 上，以及会显示标签文字的 Linux 面板上可见）、`Tooltip` 和 `Menu` 都会随各自的 signal 更新；owner 被销毁时，图标也随之消失。没有设置 `OnClick` 时，点击图标会打开菜单；设置了 `OnClick` 时，左键点击执行 `OnClick`，右键点击才打开菜单。

菜单和 `ui.MenuBar` 的菜单一样，支持 `Submenu` 和 `ui.MenuQuit`。`tray.DockMenu(items)` 在 Mac 上往程序坞图标的菜单里添加菜单项；在 Windows 上，这些菜单项会成为跳转列表（jump list）里的“任务”，点击后由正在运行的应用处理；在 Linux 上则成为启动器的快捷列表（quicklist）。

`examples/tray` 是一个常驻在菜单栏里的计时器。

## 浮出面板

浮出面板（flyout）是一种从托盘图标上“垂下来”的窗口，就像 Mac 菜单栏附加功能弹出的面板，或者 Windows 通知区域弹出的面板那样；它也可以从某个窗口中的一块矩形区域弹出。它就是一个带有 `Flyout` 的 `ui.Window`：

```go
open := signal.New(false)
icon := tray.Show(tray.Props{
	Symbol:  ui.Val("bolt.fill"),
	OnClick: func() { open.Set(!open.Peek()) },
	Menu:    ui.Val([]ui.MenuItem{ui.MenuQuit}), // 右键点击时显示
})
ui.Window(ui.WindowProps{
	Open:       open,
	Flyout:     &ui.Flyout{Anchor: icon.Anchor(), Placement: ui.PlacementBottom},
	FitContent: true, MinWidth: 260, MaxHeight: 480,
	Content:    panel,
})
```

- 它没有窗口边框，也没有任务栏按钮。在 Mac 上，如果没有设置 `Background`，它会在内容背后显示菜单那种材质效果，带圆角和阴影，所以这时不要再给内容设置背景；Windows 11 会给它加上圆角，并在窗口底色上投下阴影；Linux 两者都不画。
- `Placement` 决定它出现在锚点的哪一侧，以及如何与锚点对齐（`PlacementBottom`：在下方，居中对齐）。如果显示器上那一侧放不下，它会先翻到另一侧（Mac 菜单栏图标的下方、屏幕底部任务栏图标的上方），再平移到显示器范围之内，最后缩小以适应显示器。`Gap` 让它和锚点之间保持一段距离。
- `Anchor` 跟随它的访问器：`ui.Anchor{X, Y, Width, Height}` 表示声明这个浮出面板的窗口中的一块矩形，单位是点，从窗口内容区的左上角算起（可以取自某个视图的 frame，通过 `OnLayout` 获得）；这种情况下，浮出面板是那个窗口的子窗口，会跟着它移动。
- 它一出现就接收键盘输入，但不会把应用切到前台。当用户点击别处、切换到其他应用，或按下内容没有处理的 Escape 时，它会关闭，效果和用户主动关闭它一样（`Open` 变为 false、调用 `OnClose`，或先询问 `OnCloseRequest`）。
- 在它上面弹出对话框：在它的内容里声明一个 `Modal` 的 `ui.Window`，这个窗口会显示在浮出面板之上，并以它为中心居中；对话框关闭之前，浮出面板会保持打开，对话框关闭后键盘输入回到浮出面板。在它上面弹出 `Alert` 时，它同样会保持打开。
- `FitContent` 让它的大小适应内容（见下文），内容变大或变小时，会重新依锚点摆放。

| | 托盘浮出面板 | 依据什么定位 |
|---|---|---|
| macOS | 一个无边框、不激活应用的 `NSPanel`，处在菜单的窗口层级，背后是菜单的材质 | 用户点击的那个状态栏项的副本（每块显示器的菜单栏上都有一份） |
| Windows | 一个无边框、置顶的工具窗口，带 Windows 11 的圆角 | 图标所在的矩形（`Shell_NotifyIconGetRect`），并考虑任务栏位于哪条边 |
| Linux | 一个没有窗口装饰、不出现在任务栏里的窗口 | 在 X11 下，依据面板报告的图标点击位置；在 Wayland 下，由于不允许应用自行摆放窗口，由桌面决定位置 |
| Web | 页面中的一个面板；锚点为托盘图标时位于页面右上角 | 它所在窗口中的一块矩形，位于页面内 |

`examples/tray` 的图标会打开一个这样的浮出面板，里面是计时器，上面还会弹出一个对话框。

## 大小适应内容的窗口

`FitContent`（`WindowProps` 和 `WindowOptions` 都有）让窗口的大小适应它显示的内容，并随内容变化而调整：比如一个浮出面板里的节点列表不断变长，或者一个启动器的搜索结果时多时少。窗口的宽度取内容中最宽一行的宽度（max-content），并限制在 `MinWidth` 与 `MaxWidth` 之间，达到 `MaxWidth` 时文字就会换行；高度取内容在这个宽度下的高度，并限制在 `MinHeight` 与 `MaxHeight` 之间，超出时由其中的 `Scroll` 负责滚动。核心在布局每一帧时都会按这个尺寸布局内容，窗口也在同一轮里采用这个尺寸，所以窗口一出现就是正确的大小，不会先以别的尺寸出现。浮出面板会重新依锚点摆放；普通窗口或面板则保持顶边不动，向下伸展。用户无法手动调整它的大小。在 Web 上，主窗口就是标签页，它总是铺满整个页面。

## 后台应用

`app.KeepRunning()` 让应用在最后一个窗口关闭后继续运行：窗口只是被隐藏，调用 `ui.MainWindow().Show()` 就能把它重新显示出来（在 Mac 上，点击程序坞图标也可以）。`app.RunInBackground()`，或者在 `asuka.json` 中设置 `"background": true`，还会让应用启动时不显示任何窗口，在 Mac 上也不显示程序坞图标，适合只住在菜单栏或托盘里的应用。这两个函数都要在 `app.Run` 之前调用。

## 全局快捷键

`hotkey.Register` 注册的按键，即使其他应用在前台也能被你的应用收到，有效期与注册它的组件相同：

```go
k := hotkey.Register(hotkey.Props{
	Shortcut:    ui.Val(ui.Cmd("space").With(ui.Alt)),
	Description: ui.Val("Show the launcher"),
	OnPress:     func() { ui.MainWindow().Focus() },
})
```

每次按下快捷键时，`OnPress` 都会在主线程上执行；快捷键本身跟随它的访问器（比如用户在设置里自选的按键）。`k.Status()` 和 `k.Err()` 会告诉你它是已生效、等待中，还是失败了以及失败的原因：`ErrTaken`（这组按键已被其他应用占用）、`ErrNotAllowed`（用户拒绝了，或者桌面环境不认识这个应用）、`ErrNotAvailable`、`ErrKey`。

- **macOS**：使用 Carbon 的热键，不需要辅助功能权限。系统自带的快捷键会被视为已占用。按下快捷键不会把应用切到前台，需要的话请对某个窗口调用 `Focus`。
- **Windows**：使用 `RegisterHotKey`；按下快捷键后，应用可以把自己切到前台。
- **Linux**：在 X11 下，由应用自己抓取按键。在 Wayland 下，由桌面通过 GlobalShortcuts portal（GNOME 48、KDE Plasma 6）绑定按键：桌面可能会请用户确认或换一组按键，并且要求应用已经和它的 desktop entry 一起安装。`OnPress` 中显示的窗口会凭这次按键的激活令牌（activation token）获得焦点。
- 手机和 Web 上没有全局快捷键（`ErrNotAvailable`）。

## 面板与启动器

面板是启动器、命令面板或快速输入框所用的窗口，就像 Spotlight 和 Raycast 那样：用全局快捷键从任何地方都能呼出；它浮在所有应用的窗口之上，出现在用户当前所在的空间（Space）或虚拟桌面上，也能盖在全屏应用上面；它不出现在任务栏、程序坞的窗口列表和应用切换器中；在 Mac 上，它接收键盘输入却不会把应用切到前台，因此用户原本所在的应用会保留自己的菜单栏，面板消失后键盘输入也会回到那个应用。它就是一个设置了 `Panel` 的窗口，通常作为启动器的主窗口，搭配在后台运行的应用使用：

```go
func main() {
	app.RunInBackground()
	app.RunWindow(ui.WindowOptions{Panel: true, Hidden: true, FitContent: true,
		MinWidth: 640, MaxWidth: 640, MaxHeight: 440}, Launcher)
}

func Launcher() ui.Node {
	w := ui.UseWindow()
	w.OnCloseRequest(func() bool { w.Hide(); return false }) // 按 Escape 或点击别处时
	hotkey.Register(hotkey.Props{
		Shortcut: ui.Val(ui.Shortcut{Key: "space", Mods: ui.Alt}),
		OnPress:  func() { w.Show() },
	})
	// ...
}
```

- 它没有窗口边框。在 Mac 上，如果没有设置 `Background`，它会在内容背后显示弹出框（popover）的材质效果，带圆角和阴影；Windows 11 会给它加上圆角；Linux 把它画成普通窗口的内容。
- 没有设置 `X` 和 `Y`（或 `StateKey`）时，它每次显示都会出现在鼠标指针所在的显示器上，水平居中，位于从上往下三分之一处；`SetPosition` 和 `Center` 则会把它固定到某个位置。配合 `FitContent` 时，随着结果出现，它会从顶边向下伸展。
- 当用户按下内容没有处理的 Escape（在文本框里按的也算），或者它失去键盘焦点（点击了别处、切换到其他应用）时，它会关闭，效果和用户主动关闭它一样。更好的做法是改为隐藏它（如上面的 `OnCloseRequest`），这样它下次能立刻以原样出现；何况对于保持运行的应用，主窗口关闭时本来也只是被隐藏。
- `Show` 显示它并让它接收键盘输入；`Focus` 的效果相同，两者都不会把应用切到前台。

| | 面板 |
|---|---|
| macOS | 一个带 nonactivating 样式的 `NSPanel`，出现在每个空间中并能盖在全屏应用上（`canJoinAllSpaces`、`fullScreenAuxiliary`），不参与窗口轮换，浮动显示；永远不会成为主窗口 |
| Windows | 一个无边框、置顶的工具窗口，属于一个隐藏窗口（因此没有任务栏按钮），带 Windows 11 的圆角；它会抢到前台，而全局快捷键的那次按键恰好允许它这样做 |
| Linux | 一个没有窗口装饰的窗口；在 X11 下是一个保持置顶的工具窗口，出现在每个工作区，不出现在任务栏和工作区切换器中；在 Wayland 下，如果应用通过 gtk4-layer-shell（`LD_PRELOAD`）运行，且合成器支持该协议（KDE Plasma、Sway、Hyprland、COSMIC），它是一个 overlay 层的 layer-shell surface，否则就是一个普通窗口（由 GNOME 决定位置，也不会让它保持置顶） |
| Web | 页面上方的一个面板，位于从上往下三分之一处 |

`examples/launcher` 是一个自制的 Spotlight：按 ⌥Space（Alt+Space）会弹出一个带搜索框的面板，随着输入列出已安装的应用及其图标，按 Enter 打开第一个。

## 查找应用

`asuka/sdk/apps` 会像启动器那样列出并搜索已安装的应用：

```go
apps.List(func(list []apps.App, err error) { installed.Set(list) })
results := apps.Search(installed.Get(), query.Get())
apps.Icon(results[0], 32, func(path string, err error) { icon.Set(path) }) // 一个 PNG，可用于 ui.Image
apps.Open(results[0], nil)
```

- `List` 返回每个应用的 `Name`（与系统中显示的一致，使用用户的语言）、`ID` 和 `Path`，按名称排序；它在主线程之外读取磁盘，再回到主线程给出结果。每次显示面板时都重新列一遍，因为期间可能有应用被安装或卸载。
- `Search` 按用户输入的内容为应用排序，忽略大小写和重音符号，优先级依次为：名称完全相同、名称以输入开头、某个单词以输入开头或首字母缩写匹配（"vsc" 匹配 Visual Studio Code）、名称中任意位置包含输入，最后是名称中按顺序包含输入的各个字母。
- `Icon` 把应用图标按指定的尺寸（单位是点）绘制成 PNG 文件，存在应用的缓存目录中供下次复用，并返回它的路径。
- `Open` 启动这个应用，如果它已在运行，就把它切到前台。请先隐藏面板，这样在新打开的应用接管键盘之前，键盘输入会留在用户原本所在的应用里。

| | 查找位置 | ID | 图标 | 打开方式 |
|---|---|---|---|---|
| macOS | `/Applications`、`/System/Applications`、`~/Applications` 以及它们下一级的文件夹，外加 Finder | bundle identifier | workspace 为它提供的图标 | `NSWorkspace` |
| Windows | “开始”菜单中的应用（shell 的 Applications 文件夹），包括打包应用 | AppUserModelID | shell 为它提供的图像 | shell |
| Linux | 菜单中会显示的 desktop entry（XDG 的，以及 Flatpak 和 Snap 的），依据 `OnlyShowIn`、`NoDisplay`、`TryExec` 筛选 | desktop file id | 从图标主题中取 entry 指定的图标 | GIO，并使用显示器的启动上下文 |

手机和 Web 上返回 `apps.ErrUnsupported`。

## 鼠标穿透窗口

`ClickThrough`（或 `ui.UseWindow().SetClickThrough(true)`）让鼠标穿过窗口，作用到它背后的东西上，比如另一个应用的窗口或桌面。它适合做悬浮层、HUD 或桌面宠物，通常再配上 `Transparent` 和 `AlwaysOnTop`。`ClickThrough()` 会以 signal 的形式跟随它的状态。开启期间，窗口完全不接收点击、悬停和滚轮事件；要让用户操作窗口，就把它关掉。

```go
ui.Window(ui.WindowProps{Open: hud, Transparent: true, AlwaysOnTop: true, ClickThrough: true,
	Width: 240, Height: 80, Content: meters})
```

它在 Mac 上是 `ignoresMouseEvents`，在 Windows 上是带 `WS_EX_TRANSPARENT` 的分层窗口，在 Linux 上是一个空的输入区域（`gdk_surface_set_input_region`）。在 Web 上，页面中的窗口会让点击穿透到页面上；但没有办法让点击穿出浏览器。

## 文件对话框

`asuka/sdk/dialog` 显示系统自带的文件对话框，给你的是用户文件的真实路径，而不是副本：

```go
dialog.Save(dialog.SaveOptions{
	Name:    "Notes.md",
	Filters: []dialog.Filter{{Name: "Markdown", Extensions: []string{"md"}}},
	From:    view, // 对话框所属的窗口
}, func(path string, err error) {
	if path != "" {
		os.WriteFile(path, export(), 0o644)
	}
})
```

有三种对话框：`dialog.Open`（用 `Multiple` 可以选多个文件，`Filters` 可以按 MIME 类型如 `image/*` 或按扩展名筛选）、`dialog.Save`（可以给一个建议的 `Name`；覆盖已有文件前对话框会先询问）和 `dialog.Folder`。`Title`、`Button`、`Start`（一个文件夹，或其中的一个文件）和 `From` 用来配置它们。用户取消时，既没有路径也没有错误。

在 Mac 上，它们以弹出面板（sheet）的形式附着在窗口上；在 Windows 上是通用项目对话框（common item dialog），对所属窗口模态；在 Linux 上是 GTK 的文件对话框，在 Flatpak 中则走桌面的 portal。在浏览器中，Open 和 Folder 会把选中的内容复制到应用的文件里；Save 给出的文件会被写入用户选择的文件（Chrome、Edge），或者作为下载保存。在 iOS 和 Android 上，Open 是系统的文档选择器（得到的是副本，和 `device.PickFiles` 一样），而 Save 和 Folder 返回 `dialog.ErrUnsupported`。

## 文件管理器

`asuka/sdk/shell` 能对文件做文件管理器会做的那些事：`shell.Reveal` 在文件所在的文件夹中选中并显示它（Finder、资源管理器，或桌面环境的文件管理器）；`shell.Open` 用用户为该类型设置的应用打开文件（没有这样的应用时返回 `shell.ErrNoApp`），对文件夹则用文件管理器打开；`shell.Trash` 把文件移到废纸篓或回收站，永远不会直接彻底删除（做不到时就返回失败）；`shell.Beep` 播放系统提示音。手机和 Web 上没有文件管理器：Reveal 和 Trash 返回 `shell.ErrUnsupported`，而在 Web 上 Open 会在新标签页中显示文件。如果要在应用内显示文件的内容，请使用 `asuka/sdk/preview`。

## 打开文件

在 `asuka.json` 的 `fileTypes` 中声明应用能打开的文件类型（见[发布](shipping.md)），然后用 `ui.OnOpenFiles` 处理它们：

```go
ui.OnOpenFiles(func(paths []string) {
	for _, p := range paths {
		open(p)
	}
})
```

一起打开的文件会在同一次调用中以绝对路径的形式送达，来源包括：在 Finder、资源管理器或“文件”应用中双击、“打开方式”、把文件拖到程序坞图标上、选择一个最近使用的文档，以及在 Windows 和 Linux 上通过命令行传入的文件。应用启动时就带着的文件，会等到第一个处理函数注册后再送达。在 iOS 上，路径指向文档的原始位置，在应用退出前都可以读取；在 Android 和 Web 上，路径指向应用在缓存目录中保存的副本。

## 单实例

在 Windows 和 Linux 上，设置了 `id` 的应用只会运行一个实例：再次启动它时，会把正在运行的那个切到前台，`app.OnSecondInstance(func(args []string, dir string))` 会收到这次启动的参数和工作目录，其中的文件和链接会分别交给 `ui.OnOpenFiles` 和 `ui.OnOpenURL`。如果希望每次都启动一个新实例，就在 `asuka.json` 中设置 `"singleInstance": false`。Mac 总是只运行一个实例，并且只传递文件，不传递参数。

## 退出

`app.Quit()` 退出应用，效果和用户选择“退出”相同。无论退出是怎么触发的（⌘Q、程序坞、关闭最后一个窗口、Ctrl+C 或 SIGTERM、注销），`app.OnQuit(func(e *app.QuitEvent))` 都会先执行，调用 `e.Cancel()` 可以让应用继续运行，以便询问用户是否保存未保存的更改。如果设置了 `e.SessionEnding`，退出就无法取消：请立即保存，不要弹出对话框。关闭单个窗口时，会先通过该窗口的 `OnCloseRequest` 询问（见[导航](navigation.md#窗口)）。

`app.DataDir`、`ConfigDir`、`CacheDir` 和 `LogDir` 是应用自己的几个文件夹，在第一次请求时创建。

`app.Relaunch()` 以同样的方式退出（处理函数可以取消它），并在应用退出后重新启动它，适用于更新之后，或者修改了某个在启动时才生效的设置之后。

## 自动更新

把 `update.Window(update.Options{})` 放在应用的根组件里，就得到一个 Sparkle 式的更新器：它会在启动后不久以及之后每天检查一次新版本；有新版本时显示更新说明，并提供“安装更新”“跳过此版本”和“稍后提醒”三个选项；下载时显示进度条（如果有针对当前版本的增量包，就只下载变化的部分）；校验新版本是否用应用的密钥签名；安装完成后提供“立即重新启动”。`update.AppMenu()` 会在 Mac 上的“关于”之后加入“检查更新…”菜单项（在其他平台上则放在“帮助”菜单中）。如果想自己做更新界面，可以使用 `update.Check`、`Release.Install` 和 `update.Relaunch`。

`asuka update keygen` 生成密钥对；`asuka.json` 中的 `updates` 指定发布的位置（GitHub Releases、S3 存储桶、任意 HTTPS 目录，或者你自己的服务器）；`asuka publish --platform mac`（或 `windows`、`linux`）负责构建、签名和上传（见[发布](shipping.md#自动更新)）。Mac 应用的新 bundle 会等应用退出后再替换；Windows 和 Linux 则在应用运行时就替换它的文件。通过系统软件包、Flatpak、Snap 或应用商店安装的应用由它们负责更新，`update.Supported()` 会反映这一点；手机上返回 `ErrUnsupported`。在 Web 上，如果页面比网站上的版本旧，会提示用户重新加载。

## 最近使用的文档

用户每打开或保存一个文档，就调用一次 `recent.Add(path, nil)`。Mac 会把它们列在“文件”菜单的“最近使用”和程序坞菜单中，Windows 列在跳转列表中，Linux 则记入用户的最近文件。用户选中其中一个时，会通过 `ui.OnOpenFiles` 打开它。`recent.List` 按从新到旧的顺序返回它们，并跳过已经不存在的文件；`recent.Clear` 清除本应用自己添加的记录。

## 基于文档的应用

`asuka/sdk/document` 用来编写每个窗口各显示一个文件的应用，就像文本编辑器或绘图应用那样：NSDocument 给 Mac 应用提供的能力，在所有平台上都有了。描述好文档的类型，然后运行它：

```go
func main() {
	document.New(document.Options[string]{
		Types: []document.Type{{Name: "Plain Text", Extensions: []string{"txt"}}},
		Read: func(r io.Reader) (string, error) {
			b, err := io.ReadAll(r)
			return string(b), err
		},
		Write: func(w io.Writer, text string) error {
			_, err := io.WriteString(w, text)
			return err
		},
		Equal: func(a, b string) bool { return a == b },
		View: func(doc *document.Document[string]) ui.Node {
			return ui.TextField(ui.TextFieldProps{Value: doc.Content(), Multiline: true, Plain: true})
		},
	}).Run()
}
```

在 `asuka.json` 的 `fileTypes` 中列出同样的类型，系统才会把你的应用作为这些文件的打开方式之一。`examples/textedit` 就是这个编辑器，另外加了一行状态栏，以及一个在文件被其他应用修改时出现的提示条。

文档的 `Content()` 是一个 signal：视图绑定它，修改它就等于编辑了文档——除非 `Equal` 判断它和文件中的内容相同（比如撤销回原样，或者打了一个字又删掉）。重新读取文件时也会设置它。如果应用是原地修改内容的（比如内容是指向某个模型的指针），就要调用 `doc.Changed()` 告知这一点，并提供 `Snapshot`，用来在写入之前复制一份内容。`doc.State()` 是一个 signal，包含文档的名称、文件，以及它是否已编辑、只读、有冲突或已被删除。

用户在桌面端会得到这些：

- **每个文档一个窗口**：窗口以文件命名（还没有文件时叫“Untitled”"Untitled 2"），并在用户上次放置该文件窗口的位置打开。有未保存的更改时，Mac 会在关闭按钮里显示一个圆点，并在标题栏显示文件图标；Windows 和 Linux 会在标题前加一个 `*`，标题末尾是应用名，GNOME 还会在标题下方显示文件所在的文件夹。
- **“文件”菜单**：新建、打开…、最近使用、关闭、保存、另存为…，在 Mac 上还有复制、重新命名…、移到… 和复原到上次存储的版本（其他平台上叫“还原”），都带有对应的快捷键，作用于当前使用的窗口。`Options.Menus` 可以在它旁边添加应用自己的菜单；如果其中有一个标题为 File 的菜单，它的菜单项会并入“文件”菜单。Windows 和 Linux 会在每个文档窗口里显示这些菜单。
- **自动保存**：有文件的文档会在用户停止输入片刻后、窗口失去焦点时以及窗口关闭时自动保存，就像 Mac 应用那样，从不询问。设置 `AutosaveOff` 后，改为在用户要求时才保存，并在关闭窗口时询问，就像传统的 Windows 应用那样。
- **只在无法自动保存时才询问**：关闭一个未命名的文档（"Do you want to keep this new document?"），关闭一个文件是只读的或已被其他应用修改的文档（"Save Anyway""Revert"），或者在 `AutosaveOff` 下关闭任何有改动的文档。
- **恢复副本**：用户停止输入一秒后，未保存的更改会被复制到应用的数据目录中。无论是崩溃、强制退出还是正常退出，都不会丢失任何内容：下次启动时会重新打开这些文档，并保留其中的修改。退出时默认就这样保留文档，就像 VS Code 那样；设置 `AskOnQuit` 后则改为询问（在 Mac 上是“You have 3 documents with unsaved changes. Do you want to review these changes before quitting?”；在 Windows 和 Linux 上是一个列出这些文档的问题）。会话结束时来不及询问：所有内容会立即被保存或保留下来。
- **从外部打开的文件**：从 Finder、资源管理器、程序坞、命令行、“最近使用”或“打开…”打开文件时，如果这个文档已经打开，就会切换到它的窗口。
- **被其他应用修改的文件**：没有编辑过的文档会重新读取文件；编辑过的文档会在下次保存时询问。文件被重命名时会继续跟踪它（在 Mac 上，用户把它移到同一磁盘的任何地方都能跟踪到）；文件被删除后，下次保存会重新写出它。

在 Mac 上，基于文档的应用在没有窗口时也会保持运行，点击程序坞图标会新建一个文档；在 Windows 和 Linux 上，最后一个文档窗口关闭时应用就会退出。设置 `NoUntitled` 后，应用启动时什么都不打开，而不是新建一个未命名文档。

在手机和浏览器上，同一时间只有一个文档：应用打开后先显示它的文档列表（在 iOS 上是它在“文件”应用中的文件夹），带有新建和打开…，打开某个文档时，文档页面会推入到列表之上。每个文档从创建那一刻起就有对应的文件，并在用户编辑时、应用进入后台时以及用户返回时自动保存，就像使用 UIDocument 的应用那样：从不询问。来自其他地方的文件会先被复制到应用的文档中。

询问对话框和菜单会使用应用的语言（目前支持英语、德语、法语、西班牙语、日语和中文）：要支持其他语言或修改文案，在应用的消息目录（catalog）中添加以 `document.<key>` 为键的条目即可。

只有 Mac 上的 NSDocument 才有的这些功能没有提供：版本（复原到某个版本）、锁定、在标题栏弹出框中重命名，以及本身是文件夹的文档。

## 登录时启动

把 `login.Set(true, done)` 作为一个设置项提供给用户，并用 `login.Enabled` 读回当前状态。在 macOS 13 及以上，它是一个登录项（仅限打包好的应用；在用户于“系统设置”中允许之前，会返回 `login.ErrNeedsApproval`）；在 Windows 上是一个 Run 注册表项；在 Linux 上是一个 autostart 条目（在 Flatpak 中通过 Background portal）。`login.OpenedAtLogin` 可以区分这次启动是否是登录时的自动启动，这样应用就可以在后台悄悄启动。

## “正在播放”与媒体键

`nowplaying.Show` 把应用正在播放的内容放进系统的媒体控件中，这些控件上的按钮以及键盘上的媒体键（播放/暂停、下一首、上一首）都会交给应用的处理函数：在 Mac 上是控制中心和菜单栏里的“正在播放”；在 Windows 上是音量浮出面板和快速设置中的媒体控件（通过应用窗口的 SystemMediaTransportControls）；在 Linux 上是一个 MPRIS 播放器，可以由 GNOME 和 KDE 的媒体控件以及 `playerctl` 控制。它和手机上用的是同一个包，在手机上对应锁屏界面的媒体控件（见[设备与服务](device.md#正在播放与媒体键asukasdknowplaying)）：

```go
music := audio.Load("music/spring.mp3")
signal.OnCleanup(music.Release)
nowplaying.Show(nowplaying.Props{
	Title:  ui.Val("Spring"),
	Artist: ui.Val("Vivaldi"),
	Player: music, // 从它获取播放状态和进度；播放、暂停、跳转都作用在它上面
	OnNext: playNext,
})
```

## 显示器、电源与应用图标

`screen.UseDisplays()` 跟踪所有显示器：每块显示器的位置、工作区、缩放比例、刷新率，以及哪块是主显示器；`Nearest` 和 `Matching` 用来挑出某个点或某个窗口所在的那块显示器。Wayland 没有工作区的概念，在那里工作区就是显示器的整个范围。

`power.Use()` 跟踪电池状态、低电量模式和散热状态。`power.OnSuspend`、`OnResume`、`OnLock` 和 `OnUnlock` 会在电脑睡眠、唤醒以及屏幕锁定、解锁时通知你，`power.IdleTime()` 则给出用户已经离开了多久。手机上有锁定和解锁，但没有睡眠和空闲时间；Web 上只有电池状态。

`dock.SetProgress` 在程序坞图标、任务栏按钮，或支持 Unity launcher API 的 Linux dock 上显示任务进度（GNOME 自带的 dash 不显示）。`dock.RequestAttention` 会在应用处于后台时让图标跳动或闪烁。图标上的数字角标则由 `notify.SetBadge` 设置。

`appearance.Use()` 提供系统的强调色、“增强对比度”和“降低透明度”设置，供应用自己绘制的内容使用；Asuka 显示的视图本身已经会跟随这些设置。

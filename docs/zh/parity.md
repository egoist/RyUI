# Asuka 与 SwiftUI、Expo 的功能对照

下面每一行都是应用经常需要的一项功能，分别列出 SwiftUI 和 Expo 的做法，以及 Asuka 提供的对应能力。除非该行另有说明，✓ 表示 Asuka 支持的所有平台（iOS、Android、Mac、Windows、Linux 和 Web）都能用；如果某个平台本身不具备某项能力（比如 Mac 没有传感器，浏览器没有主屏幕小组件），对应的包会如实告诉调用方不支持。至于浏览器做不到手机能做的事时各个包会如何替代，见 [Web](web.md#原生模块)。

SwiftUI 也能在 Mac 上运行。Expo 支持 iOS、Android 和 Web；React Native 则借助微软的 React Native for Windows and macOS 覆盖 Windows 和 Mac，不过这不在 Expo 的范围内。Asuka 与桌面端常用框架（Electron、Tauri、Wails）的对比，见[与桌面端框架对比](#与桌面端框架对比)；桌面应用在窗口之外还需要哪些能力，见[桌面应用的能力](#桌面应用的能力)；与自己绘制 UI 的 MyGo 的对比，见 [Asuka 与 MyGo](mygo.md)。

✓ 已支持 · ◐ 部分支持（会注明缺什么） · ✗ 尚未支持

## 布局

| | SwiftUI | Expo (React Native) | Asuka |
|---|---|---|---|
| 堆叠、间距、内边距、对齐 | `HStack`、`VStack`、`Spacer` | flexbox | ✓ `Row`、`Column`、`Style`（flexbox：`Grow`、`Gap`、`Align`、`Justify`、`Wrap`） |
| 覆盖层、绝对定位 | `ZStack`、`.overlay` | `position: absolute` | ✓ `Style.Absolute`、`Top`/`Left`/... |
| 滚动 | `ScrollView` | `ScrollView` | ✓ `Scroll`（纵向、横向；移出视野的行会离开视图树） |
| 惰性列表、单元格复用 | `List`、`LazyVStack` | `FlatList` | ✓ `List`（用平台自己的：UICollectionView、RecyclerView；在 Mac、Windows、Linux 和 Web 上，由宿主为可见范围内的行绑定数据并复用） |
| 网格 | `LazyVGrid` | `FlatList numColumns` | ✓ `ListProps.Columns`、`Gap` |
| 聊天列表：从末尾开始显示，往上加载更早的消息 | `defaultScrollAnchor(.bottom)`、`scrollPosition` | `FlatList inverted`、`maintainVisibleContentPosition` | ✓ `ListProps.Anchor`、`OnStartReached`；在可见内容上方插入行时，可见内容保持不动，所有列表都是如此 |
| 分区、吸顶标题 | `List` 里的 `Section` | `SectionList`、`stickySectionHeadersEnabled` | ✓ `ListProps.Section`、`Header`（在 plain 和 bare 列表里吸顶） |
| 选择行 | `List(selection:)`、`EditButton` | 社区方案 | ✓ `ListProps.Selecting`、`Selected`、`OnSelect`、`OnSelectBegin`（iOS 的编辑模式和双指拖动；Android 的复选框和长按） |
| 安全区域、键盘 | 自动处理 | `SafeAreaView`、`KeyboardAvoidingView` | ✓ `SafeArea`、`UseEnvironment().Keyboard` |
| 读取视图的尺寸 | `GeometryReader` | `onLayout` | ✓ `ViewProps.OnLayout` |
| 宽高比、最小/最大尺寸 | `.aspectRatio`、`.frame(min:max:)` | 样式 | ✓ `Style` |

## 控件

| | SwiftUI | Expo | Asuka |
|---|---|---|---|
| 文本、带样式的文本、链接 | `Text`、`AttributedString` | `Text` | ✓ `Text`、`RichText`（文本片段、链接、行内视图、文本选择）、`asuka/sdk/markdown` |
| 按钮（变体、图标、菜单） | `Button`、`Menu` | `Pressable` | ✓ `Button`（filled、tinted、gray、bordered；图标；菜单）、`Menu`、上下文菜单；菜单项可以禁用、打勾、放进子菜单或带标题的分区（`Action.Disabled`、`Checked`、`Children`、`Inline`；`ui.Submenu`、`ui.MenuSection`），导航栏按钮也能弹出菜单 |
| 文本框 | `TextField`、`SecureField`、`TextEditor` | `TextInput` | ✓ `TextField`（密码输入、键盘类型、回车键类型、多行） |
| 光标、在光标处插入 | `TextSelection` | `selection`、`onSelectionChange` | ✓ `TextFieldProps.Selection`、`ui.InsertText` |
| 富文本编辑（比如邮件正文） | 配合 `AttributedString` 的 `TextEditor`（iOS 26） | 社区方案（10tap、pell） | ✓ `TextFieldProps.Rich`、`ToggleFormat`、`SetLink`、`OnFormats` |
| 键盘上方的工具条 | `.toolbar { ToolbarItemGroup(placement: .keyboard) }` | `InputAccessoryView`（iOS） | ✓ `TextFieldProps.Accessory`（两个平台都支持） |
| 粘贴的图片、键盘里的 GIF 和贴纸 | `onPasteCommand`、`PasteButton` | 社区方案（`onPaste`） | ✓ `TextFieldProps.PasteTypes`、`OnPaste`（也支持 Android 的 `commitContent`） |
| 开关、滑块 | `Toggle`、`Slider` | 社区方案 | ✓ `Toggle`、`Slider` |
| 分段控件 | `Picker(.segmented)` | 社区方案 | ✓ `Segmented` |
| 日期和时间 | `DatePicker` | 社区方案 | ✓ `DatePicker`（日期、时间，或两者都选） |
| 进度条、加载指示器 | `ProgressView` | `ActivityIndicator` | ✓ `Progress`、`ProgressRing`（圆形，有值无值都行）、`Activity` |
| 滚轮选择器、菜单选择器、步进器 | `Picker`、`Stepper` | 社区方案 | ✓ `Wheel`、`Picker`（iOS 和 Mac 上是弹出按钮，Android 上是 Material 的 exposed dropdown menu）、`Stepper` |
| 颜色选择器 | `ColorPicker` | 社区方案 | ✓ `ColorPicker`（iOS 上用系统的选择器；Android 没有系统选择器，就用一个 Material 对话框展示 Google 的调色板，支持不透明度） |
| 图标 | SF Symbols | `@expo/vector-icons` | ✓ `Symbol`（iOS 和 Mac 上用 SF Symbols；同样的名字在 Android 和 Web 上对应 Material Symbols，在 Windows 上对应 Segoe Fluent Icons，在 Linux 上对应图标主题里的图标或 Material Symbols：这些是精选出来的一组图标） |
| 图片（远程、缓存、占位图） | `AsyncImage` | `expo-image` | ✓ `Image`（内存和磁盘缓存、`Placeholder`） |
| 视频 | `VideoPlayer` | `expo-video` | ✓ `Video`（iOS 和 Mac 上用 AVKit，Android 上用 Media3，Windows 上用 WebView2，Linux 上用 GStreamer，Web 上用 `<video>`） |
| 网页内容 | `WKWebView` | `react-native-webview` | ✓ `WebView`（iOS 和 Mac 上是 WKWebView，Android 上是系统的 WebView，Windows 上是 WebView2，Linux 上是 WebKitGTK；支持 URL、HTML、双向消息、先于页面脚本运行的脚本、拦截请求、页面状态和历史记录、下载、新窗口、cookie、获取脚本的返回值；Android 上还能逐个处理请求；在 Web 上，应用自己站点的页面和通过 `postMessage` 配合的页面能用上以上全部功能，其他站点的页面则只能加载）；`asuka/sdk/js` 可以在屏幕之外运行 JavaScript |
| 地图 | `Map` | `expo-maps` | ✓ `asuka/sdk/maps`（Apple 地图；Google 地图需要 API 密钥：支持区域、标记、用户位置、点按；Linux 上用 libshumate 的 OpenStreetMap 地图；Windows 上由宿主自己绘制 OpenStreetMap 的瓦片） |
| 自定义绘制 | `Shape`、`Path`、`Canvas` | `react-native-svg`、Skia | ✓ `Shape` 和 `Path`（填充、描边、线帽、连接样式、虚线、可做动画的 trim）；`Canvas`（路径、渐变、文本、变换、裁剪、不透明度）；以原生方式绘制的 SVG 图标（`asuka/sdk/svg`） |
| 图表 | Swift Charts | `victory-native`、社区方案 | ✓ `asuka/sdk/chart`（柱状图、折线图、面积图、散点图） |
| 着色器 | `.colorEffect(ShaderLibrary...)` | Skia 的 runtime effect | ✓ `Shader`（用 Kage 编写，语法与 Go 相同：iOS 和 Mac 上用 Metal，Android 上用 AGSL，Web 上用 WebGL 2，Windows 上用 Direct3D 11，Linux 上用 OpenGL） |
| 调用原生代码 | `UIViewRepresentable` | 原生模块 | ✓ 原生包：在 Go 里声明带类型的模块和视图，在 iOS 和 macOS 上用 Swift 实现，在 Android 上用 Kotlin，在 Web 上用 JavaScript，在 Windows 和 Linux 上用 Go（[原生代码](native.md)） |
| 分页、页面指示点 | `.scrollTargetBehavior(.paging)`、`TabView(.page)` | `react-native-pager-view` | ✓ `ScrollProps.Paging`、`PageControl` |
| 复选框 | `Toggle`（`.checkbox`，macOS） | 社区方案 | ✓ `ToggleProps.Checkbox` |

## 导航与呈现

| | SwiftUI | Expo (expo-router) | Asuka |
|---|---|---|---|
| 带原生推入、弹出和返回手势的导航栈 | `NavigationStack` | Stack | ✓ `Stack`、`UseNavigator` |
| 标签页 | `TabView` | Tabs | ✓ `Tabs` |
| 侧边栏、抽屉 | `NavigationSplitView` | Drawer | ✓ `SplitView`（iPhone 上是导航栈，Android 上是抽屉；Mac 上是 `NSSplitViewController`，Windows 上是导航窗格，Linux 上是 libadwaita 的分栏视图） |
| 标签页和侧边栏行上的角标 | `badge` | `tabBarBadge`、`drawerItem` 计数 | ✓ `Tab.Badge`、`Destination.Badge`（`badgeValue`、`BadgeDrawable`、抽屉计数；Mac 上 `NSTabView` 的标签文字、源列表的计数；Fluent 的 `InfoBadge`；libadwaita 的 `badge-number`） |
| 导航栏按钮、标题、搜索 | `.toolbar`、`.searchable` | header 选项 | ✓ `ScreenOptions`（`Actions`、`Search`） |
| 底部工具栏、主操作（FAB） | `ToolbarItem(placement: .bottomBar)` | — | ✓ `ScreenOptions`（`Toolbar`、`Primary`、`PrimaryExtended`）：UIKit 的工具栏加一个醒目的末尾按钮，Material 的 `BottomAppBar` 和（扩展）FAB，Mac 上是窗口工具栏，Windows 上是命令栏，Linux 上是 `GtkActionBar`，Web 上是一个底栏加一个浮动按钮 |
| 带停靠高度的弹出面板 | `.sheet`、`.presentationDetents` | modal | ✓ `Sheet`（`Detents`、`Grabber`） |
| 全屏模态 | `.fullScreenCover` | `presentation: "fullScreenModal"` | ✓ 设置了 `FullScreen` 的 `Sheet`（iOS 的 `.fullScreen`；Material 的全屏对话框，铺满屏幕边缘，支持预测性返回；桌面端是盖住整个窗口内容的弹出面板或窗口；Web 上是盖住整个页面的 `<dialog>`） |
| 拦截返回、阻止关闭 | `.navigationBarBackButtonHidden`、`.interactiveDismissDisabled` | `BackHandler`、`usePreventRemove` | ✓ `OnBack`（页面、弹出面板、整个应用） |
| 气泡弹窗 | `.popover` | — | ✓ `Popover`（在 iPhone 上也是 popover） |
| 警告框、操作表 | `.alert`、`.confirmationDialog` | `Alert` | ✓ `Alert`（两种样式都支持；警告框可以带输入框：`Fields`，密码框用 `Secure`，按钮通过 `Disabled` 跟随输入内容） |
| Snackbar、toast（带“撤销”的简短提示） | — | 社区方案（react-native-paper 的 `Snackbar`） | ✓ `Snackbar`（Android 上是 Material 的 Snackbar，Linux 上是 `AdwToast`；iOS、macOS、Windows 和 Web 上按平台的样式绘制） |
| 深度链接、通用链接和 App Links | `.onOpenURL` | Linking | ✓ `OnOpenURL`、`asuka.json` 里的 scheme 和 links |
| 下拉刷新、滑动操作 | `.refreshable`、`.swipeActions` | `RefreshControl` | ✓（两侧都支持：`SwipeActions`、`LeadingSwipeActions`） |

## 动画

| | SwiftUI | Expo (Reanimated) | Asuka |
|---|---|---|---|
| 为变化添加动画 | `withAnimation` | `withTiming`、`withSpring` | ✓ `Animate`、`AnimateWith`（缓动、线性、弹簧动画；重复、自动反向） |
| 变换 | `.rotationEffect`、`.scaleEffect`、`.offset` | `transform` | ✓ `Style.Transform` |
| 进入和退出过渡 | `.transition` | entering/exiting | ✓ `Style.Transition`（`Fade`、`Pop`、`SlideUp`，也可以自定义） |
| 逐帧动画 | `TimelineView(.animation)` | `useFrameCallback` | ✓ `OnFrame` |
| 阴影、渐变、模糊 | `.shadow`、`LinearGradient`、材质 | 样式、expo-linear-gradient、expo-blur | ✓ `Shadow`、`LinearGradient`、`Material` |
| 手势 | 点按、长按、拖动、缩放、旋转 | Gesture Handler | ✓ 点按、长按、平移、捏合、旋转、轻扫 |
| 硬件键盘的按键 | `.onKeyPress`、`.keyboardShortcut` | `onKeyPress`（`TextInput` 上的；React Native for Windows and macOS 上是 `onKeyDown`） | ✓ `ViewProps.Keys`、`TextFieldProps.Keys`（从获得焦点的视图逐层向外传递，先于控件和菜单处理）、`UseKeys`（整个窗口的） |
| 按下和悬停状态 | `ButtonStyle` 的 `isPressed`、`.onHover` | `Pressable` 的 `pressed`、`onHoverIn`/`onHoverOut` | ✓ `Style.Pressed`、`Hovered` 变体；用 `Interaction` 跟随分组（group）或相邻元素（peer）的状态（`Style.When`）；`OnPressedChange`、`OnHoverChange` |
| 拖放 | `.draggable`、`.dropDestination` | （没有内置） | ✓ `Drag`、`Drop`（应用内可以拖放 Go 值；与其他应用之间可以拖放文本、链接和文件；`Dragging` 和 `DragOver` 变体） |
| 共享元素过渡 | `matchedGeometryEffect`、`.navigationTransition(.zoom)` | shared transitions | ✓ `Style.Match`（一个视图从另一个视图的位置和大小中展开）、`ScreenOptions.ZoomFrom`（推入的页面从某个视图中缩放展开：iOS 18 的缩放过渡；Material 的容器变换） |
| 触感反馈 | `sensoryFeedback` | `expo-haptics` | ✓ `Haptic` |
| 减弱动态效果 | `accessibilityReduceMotion` | `useReducedMotion` | ✓ `Environment.ReduceMotion`（iOS 的过渡改为淡入淡出） |

## 数据与服务

| | SwiftUI / Apple | Expo | Asuka |
|---|---|---|---|
| 状态 | `@State`、`@Observable` | hooks | ✓ signal（细粒度：一次写入只更新读取了它的地方） |
| 持久化少量数据 | `@AppStorage` | AsyncStorage | ✓ `asuka/sdk/store` |
| SQLite | SwiftData / GRDB | `expo-sqlite` | ✓ `asuka/sdk/sqlite`（迁移、带类型的查询、事务、在主线程之外写入、实时查询：读取的表被写入时会重新运行，参数变化时也会，见 `WatchWith`；浏览器里也能用，基于 SQLite 的 WebAssembly 版本） |
| 安全存储 | Keychain | `expo-secure-store` | ✓ `asuka/sdk/secure`（在 Web 上：加密后存进 IndexedDB，密钥由浏览器保管、无法导出；并不是真正的钥匙串） |
| 文件、网络 | Foundation | `expo-file-system`、fetch | ✓ Go 的 `os`、`net/http` |
| 照片 | `PhotosPicker` | `expo-image-picker` | ✓ `device.PickPhotos` |
| 文档 | `UIDocumentPickerViewController` | `expo-document-picker` | ✓ `device.PickFiles`（MIME 类型、多选） |
| 相机 | AVFoundation | `expo-camera` | ✓ `asuka/sdk/camera`：`Take`（系统相机；在 Mac、Windows 和 Linux 上是一个带实时预览的对话框；Windows 上用 Media Foundation 的采集引擎；Linux 上用相机 portal，或通过 GStreamer 调用 Video4Linux）、`View`（实时预览，前置或后置摄像头）、`Capture`；`asuka/sdk/camera/record` 可以录制带声音的视频 |
| 条形码、二维码 | AVFoundation、Vision | `expo-camera` 的扫描功能 | ✓ `asuka/sdk/scan`：可以在 `camera.View` 里实时识别（`Scan: scan.Codes(...)`），也可以识别图片文件（`scan.File`）；Android 上用 ML Kit，只会打包进导入了它的应用；Linux 上用 ZBar；Windows 上用 Asuka 自己用 Go 写的识别器（`internal/barcode`：QR、Data Matrix、EAN、UPC、Code 128、Code 39） |
| 定位 | Core Location | `expo-location` | ✓ `asuka/sdk/location`（地名查询：Windows 上用 OpenStreetMap 的 Nominatim）；“使用期间”和“始终”两级权限，精确或大致位置；后台定位更新和地理围栏（相当于 `expo-task-manager` 的任务：iOS 和 Android 上会为这些事件启动应用；系统不提供时，在应用运行期间根据位置计算） |
| 翻译、复数、区域格式 | String Catalogs、FormatStyle | `expo-localization`、i18next | ✓ `asuka/i18n`、翻译目录（[多语言](i18n.md)） |
| 从右到左布局 | `layoutDirection` | `I18nManager` | ✓ 跟随语言，也可用 `ui.WithDirection` 设置 |
| 本地通知 | UserNotifications | `expo-notifications` | ✓ `asuka/sdk/notify`（立即或定时发送，支持图片；iOS、Android、macOS、Linux；Windows 上是 toast 通知，通过应用的 COM 激活器支持按钮和回复；Web 上通过应用的 service worker 发送，所以页面关闭后通知仍然保留、仍能响应操作；定时通知在应用有页面打开时会按时显示，否则等下次打开页面时再显示：浏览器无法预约通知） |
| 推送通知 | APNs | `expo-notifications` | ✓ `asuka/sdk/push`（iOS 和 macOS 上用 APNs，另有 Firebase Cloud Messaging，Web 上用基于 VAPID 的 Web Push：获取令牌、接收消息、处理点按；`asuka/sdk/push/webpush` 可以从 Go 服务器发送 Web Push） |
| 通知操作、内联回复 | `UNNotificationCategory`、`UNTextInputNotificationAction` | `setNotificationCategoryAsync` | ✓ `notify.SetCategories`、`OnAction`（后台操作会运行应用的 Go 代码，应用没在运行时会以无界面方式启动；`Response.Defer`；Android 上回复会被添加回对话里；在 Web 上用 service worker 的通知操作，Chrome 支持的地方还有内联回复） |
| 通知分组、当前显示的通知、角标 | `threadIdentifier`、`getDeliveredNotifications`、`setBadgeCount` | `getPresentedNotificationsAsync`、`setBadgeCountAsync` | ✓ `Thread`（Android：一个通知组，并保留它的摘要通知）、`notify.Showing`、`Remove`、`RemoveThread`、`SetBadge`（Android：启动器根据通知显示的角标） |
| 前台通知 | `willPresent` | `setNotificationHandler` | ✓ `notify.Present`（横幅、静默、隐藏） |
| Android 通知渠道 | — | `setNotificationChannelAsync` | ✓ `notify.SetChannels`、`SetChannelGroups`；推送的 `channel_id`（iOS 没有渠道） |
| 对话类消息 | 通讯通知（`INSendMessageIntent`） | 社区方案 | ✓ `Notification.From`、`push.Message.From`：iOS 的通讯通知（只给用到它的应用添加 entitlement），Android 的 MessagingStyle 和对话快捷方式；macOS 上显示为普通通知；不支持气泡 |
| 在推送显示前修改内容 | Notification Service Extension | 社区方案（`expo-notifications` 不支持） | ✓ `push.Rewrite`：iOS 上在扩展里运行应用的 Go 代码（由 `asuka build` 构建并签名），Android 的数据消息则在应用进程里处理 |
| 剪贴板、分享面板、浏览器 | `UIPasteboard`、`ShareLink`、`SFSafariViewController` | `expo-clipboard`、`expo-sharing`、`expo-web-browser` | ✓ `Copy`、`device.Clipboard`（文本；图片用 `CopyImage`、`ClipboardImage`；`ClipboardHas`、`OnClipboardChange`）、`Share`（文本）、`ShareFiles`（文件，支持 expo-sharing 那样的 MIME 类型、UTI、标题和 iPad 锚点；分享面板关闭时才返回）、`device.Browse` |
| 预览文件 | `QLPreviewController`、`.quickLookPreview` | `react-native-file-viewer` | ✓ `asuka/sdk/preview`（iOS 和 Mac 上用 Quick Look，支持多个文件和标记（Markup）；Android 上交给查看器应用；Windows 和 Linux 上用默认应用打开） |
| 打印、从 HTML 生成 PDF | `UIPrintInteractionController`、`UIPrintPageRenderer` | `expo-print` | ✓ `asuka/sdk/print`（可以打印文件、HTML 或 WebView，使用系统的打印对话框并拿到结果；`PDF`：自动分页、按地区选择纸张、页边距） |
| 邮件和短信编辑界面 | `MFMailComposeViewController`、`MFMessageComposeViewController` | `expo-mail-composer`、`expo-sms` | ✓ `asuka/sdk/compose`（收件人、主题、HTML 正文、附件；iOS 上能知道用户最后做了什么；Android 上只列出邮件应用；Mac 上支持“信息”） |
| 保持屏幕常亮、设备信息 | `isIdleTimerDisabled`、`UIDevice` | `expo-keep-awake`、`expo-device` | ✓ `device.KeepAwake`、`device.GetInfo` |
| 网络状态 | `NWPathMonitor` | `expo-network` | ✓ `asuka/sdk/network`（是否在线、Wi-Fi 还是蜂窝网络、是否按流量计费、流量节省程序 / 低数据模式） |
| 保存到相册、下载目录 | `PHPhotoLibrary` | `expo-media-library` | ✓ `asuka/sdk/library`（Android 上是 MediaStore 的各个集合；iOS 上是“照片”，或“文件”里的 Documents） |
| 后台任务 | `beginBackgroundTask`、`BGTaskScheduler` | `expo-task-manager`、`expo-background-task` | ✓ `asuka/sdk/background`：Android 会让应用保持运行，进度和按钮显示在一条通知里；iOS 大约给 30 秒；Mac 会让应用保持运行（关闭 App Nap），Linux 和 Windows 会阻止系统休眠。`asuka/sdk/periodic`：由系统不定期运行的任务，以无界面方式启动应用的 Go 代码（BGTaskScheduler、WorkManager；Mac 上在应用运行期间用 `NSBackgroundActivityScheduler`；Linux 和 Windows 上在应用运行期间用定时器） |
| 接收其他应用的分享 | Share extension | `expo-sharing`（`useIncomingShare`） | ✓ `ui.OnShare`：文本、链接、图片、视频、音频和文件，单个或多个，附带名称、MIME 类型和大小（由 `asuka.json` 的 `shareTarget` 指定类型和数量上限，并据此生成 Android 的 intent filter 和 iOS 的激活规则；iOS 上由分享扩展打开应用，文件通过 App Group 传递）；`app.ShareSheet`：在发起分享的应用上方显示应用自己的界面（iOS 上是它的分享扩展；Android 上是运行在独立进程里的底部面板），数据与应用共享；不解析链接内容（请用 `net/http`） |
| 音频 | AVAudioPlayer、AVAudioRecorder | `expo-audio` | ✓ `asuka/sdk/audio`（加载、播放、暂停、跳转、音量、循环）；`asuka/sdk/audio/record`（语音或音乐音质、暂停、电平表） |
| 正在播放、锁屏控制、媒体键 | `MPNowPlayingInfoCenter`、`MPRemoteCommandCenter` | `expo-audio` 的锁屏控制、社区方案（`react-native-track-player`） | ✓ `asuka/sdk/nowplaying`（标题、艺人、专辑、封面、时长、由系统推进的播放位置，以及播放、暂停、下一首、上一首、跳转、快进快退、停止，设置了哪些处理函数就启用哪些控制；会自动跟随 `audio.Player`；Android 上用 MediaSession 和媒体播放服务；Mac 上是控制中心，Windows 上是媒体浮出控件，Linux 上是 MPRIS，Web 上是 Media Session API） |
| 通过 Apple 登录 | `SignInWithAppleButton` | `expo-apple-authentication` | ✓ `asuka/sdk/appleauth`（Apple 的登录按钮、凭据及其状态；iOS 和 macOS） |
| 面容 ID、指纹 | LocalAuthentication | `expo-local-authentication` | ✓ `asuka/sdk/biometric`（Windows 上用 Windows Hello；Web 上通过 WebAuthn 调用平台认证器） |
| 应用商店评分 | `requestReview` | `expo-store-review` | ✓ `asuka/sdk/review`（App Store 的评分弹窗，Play 的应用内评价） |
| 跟踪权限 | App Tracking Transparency | `expo-tracking-transparency` | ✓ `asuka/sdk/tracking` |
| 应用完整性 | App Attest | `expo-app-integrity` | ✓ `asuka/sdk/integrity`（App Attest、Play Integrity） |
| 年龄段 | Declared Age Range | `expo-age-range` | ✓ `asuka/sdk/agerange`（Declared Age Range、Play Age Signals） |
| 传感器 | Core Motion | `expo-sensors` | ✓ `asuka/sdk/sensors`（加速度计、陀螺仪、磁力计、带姿态和朝向的设备运动、气压计、光线、计步器；两个平台上坐标轴和单位一致；Mac 和 Windows 没有传感器；Linux 上是 iio-sensor-proxy 提供的光线和方向） |
| 图片编辑 | Core Graphics、ImageIO | `expo-image-manipulator` | ✓ `asuka/sdk/imageedit`（缩放、裁剪、旋转、翻转、填充、像素化；可以读入 HEIC，输出 JPEG 或 PNG，方向摆正，并去掉原图的元数据；Linux 上用 GdkPixbuf 的解码器，Windows 上用 WIC 的解码器） |
| 识别图片里的文字、人脸和条码 | Vision | 无 | ✓ `asuka/sdk/vision`，会给出每一项的位置；Android 上用 ML Kit，只会打包进导入了它的应用；Linux 上用 ZBar 识别条码、Tesseract 识别文字，不支持人脸；Windows 上用系统 OCR 识别文字和人脸，用 Asuka 自己的识别器识别条码 |
| 照片图库 | PhotoKit | `expo-media-library` | ✓ `asuka/sdk/photos`：权限、列出照片、缩略图、原始文件（Windows 上是“图片”文件夹） |
| 视频缩略图 | `AVAssetImageGenerator` | `expo-video-thumbnails` | ✓ `imageedit.VideoFrame` |
| 文字转语音 | `AVSpeechSynthesizer` | `expo-speech` | ✓ `asuka/sdk/speech`（朗读队列、声音、音调、语速、在某个词处暂停） |
| 通讯录 | Contacts、ContactsUI | `expo-contacts` | ✓ `asuka/sdk/contacts`（联系人选择器和新建联系人界面不需要权限；有权限后可以访问通讯录；Windows 上添加联系人会打开一个 vCard，无法访问通讯录） |
| 日历 | EventKit、EventKitUI | `expo-calendar` | ✓ `asuka/sdk/calendar`（事件编辑器不需要权限；有权限后可以访问日历和事件；Windows 上添加事件会打开一个 iCalendar 文件，无法访问日历） |
| 主屏幕小组件 | WidgetKit | `expo-widgets` | ✓ `asuka/sdk/widget` 和 `asuka.json` 的 `widgets`（所有尺寸共用一个模板，iOS 上还有锁屏小组件；时间线、倒计时、点按时打开 URL） |
| 实时活动 | ActivityKit | `expo-live-activity` | ✓ `asuka/sdk/liveactivity`（iOS：锁屏和灵动岛；Android：常驻通知，即 Android 16 的实时更新；倒计时、进度、提醒，以及由服务器推送来更新和启动） |
| App Intents、快捷指令 | App Intents、App Shortcuts | — | ✓ `asuka.json` 的 `shortcuts`（iOS 上是带 Siri 短语的 App Shortcuts，会出现在聚焦搜索和“快捷指令”里；Android 上是启动器快捷方式） |
| 自定义字体 | `.font(.custom)` | `expo-font` | ✓ `Font.Family` |
| 生命周期 | `scenePhase` | AppState | ✓ `app.OnLifecycle` |
| 启动屏 | `UILaunchScreen` | `expo-splash-screen` | ✓ `asuka.json` 的 `splash`；`asuka/sdk/splash` 可以在应用加载期间让它一直显示 |
| 状态栏、导航栏 | `statusBarHidden`、`preferredStatusBarStyle` | `expo-status-bar`、`expo-navigation-bar` | ✓ `asuka/sdk/screen` 的 `SetBars` |
| 锁定屏幕方向 | `supportedInterfaceOrientations` | `expo-screen-orientation` | ✓ `asuka/sdk/screen` 的 `Lock` |
| 截屏 | `isCaptured`、`userDidTakeScreenshotNotification` | `expo-screen-capture` | ✓ `asuka/sdk/screen` 的 `PreventCapture`（Android 上截图是空白的；iOS 上录屏时内容会被遮住；macOS 和 Windows 上截图里不包含这些窗口）、`OnScreenshot`（Linux 和 Windows 上通过 Pictures/Screenshots 里新出现的图片来判断） |
| 崩溃报告 | MetricKit、Crashlytics、Sentry | Sentry、Crashlytics（`@react-native-firebase/crashlytics`） | ✓ `asuka/sdk/crash`：Go 的 panic 和致命错误、未捕获的异常、MetricKit 记录的崩溃和卡顿、Android 的原生崩溃和 ANR，附带面包屑和自定义值；报告发到哪里由应用决定（没有内置服务）；`asuka build` 会生成 dSYM 和原生调试符号 |

## 外观与无障碍

| | SwiftUI | Expo | Asuka |
|---|---|---|---|
| 深色模式、语义色、强调色 | 自动处理 | `useColorScheme` | ✓ 语义色、`asuka.json` 的强调色（Android 上用 Material 3 动态取色；Mac 上 AppKit 自己的控件保留用户设置的强调色） |
| 动态字体、字体缩放 | 自动处理 | `allowFontScaling` | ✓ 文本样式和自定义字体都会跟着缩放 |
| 屏幕阅读器的标签、角色、隐藏 | `.accessibilityLabel`... | `accessibilityLabel`... | ✓ `Label`、`Hint`、`Role`、`Decorative`；可点按的视图会被识别为按钮 |
| 播报 | `AccessibilityNotification.Announcement` | `announceForAccessibility` | ✓ `Announce` |
| 移动屏幕阅读器的焦点 | `@AccessibilityFocusState` | `setAccessibilityFocus` | ✓ `AccessibilityFocus` |
| 屏幕阅读器是否开启 | `voiceOverEnabled` | `isScreenReaderEnabled` | ✓ `Environment.ScreenReader` |
| 自定义操作、可调节控件 | `.accessibilityAction`、`.accessibilityAdjustableAction` | `accessibilityActions` | ✓ `ViewProps.Actions`、`OnAdjust`（List 的滑动操作也会成为 TalkBack 的操作） |

## 工具链

| | Xcode / Expo | Asuka |
|---|---|---|
| 实时重载并保留状态 | 预览、Fast Refresh | ✓ `asuka dev`：保存即重载，并保留 signal 的值（在手机上，Go 代码运行在你的电脑上；在桌面端，应用会重新构建并运行；在浏览器里，页面会重新加载） |
| 发布构建和签名 | Xcode、EAS Build | ✓ `asuka build`：`.ipa`、`.aab`、经过签名和公证的通用 `.app`（或 Mac App Store 用的 `.pkg`）、包含已签名 `.exe` 的 Windows 文件夹、附带 `.tar.gz` 和 Flatpak 清单的 Linux 安装前缀、静态网站（[发布](shipping.md)） |
| 上传到应用商店 | Xcode、EAS Submit | ✓ `asuka publish`：TestFlight、App 审核、Google Play 的各个发布轨道；暂不支持 Mac App Store |
| GUI 测试 | XCUITest、Detox、Maestro | ✓ `e2e/`：在 iOS 模拟器、Android 模拟器、Xvfb 下的 Linux 和无头 Chrome 里运行示例；Mac 和 Windows 的宿主测试会驱动真实的 AppKit 视图和 Win32 窗口 |
| 基准测试 | Instruments | ✓ `bench/`（与 SwiftUI 和 Compose 对比） |
| 嵌入现有应用 | — | brownfield（React Native 的方案） | ✓ `asuka build --platform ios --xcframework`、`asuka build --platform android --aar` |
| OTA 更新 | — | EAS Update：✗ |

## 与桌面端框架对比

在 Mac、Windows 和 Linux 上，很多应用是用 web view 包一层 HTML 做出来的。这些框架和 Asuka 的对比如下：

| | Electron | Tauri | Wails | React Native for Windows and macOS | Asuka |
|---|---|---|---|---|---|
| 平台 | Mac、Windows、Linux | Mac、Windows、Linux；iOS 和 Android（Tauri 2） | Mac、Windows、Linux | Windows、Mac（配合 React Native 的 iOS 和 Android） | Mac、Windows、Linux、iOS、Android、Web |
| 界面由什么呈现 | Chromium 里的 HTML 和 CSS | 系统 web view（WKWebView、WebView2、WebKitGTK）里的 HTML 和 CSS | 系统 web view 里的 HTML 和 CSS | 平台的原生视图 | 平台的原生控件：AppKit；Win32 和通用控件，按 Windows 11 的样子绘制；GTK 4 和 libadwaita |
| 应用使用的语言 | JavaScript（页面之外还有 Node.js） | Rust，加上页面里的 JavaScript | Go，加上页面里的 JavaScript | JavaScript | Go |
| 随应用分发的内容 | Chromium 和 Node.js | 应用的二进制文件（web view 用系统自带的） | 应用的二进制文件（同上） | 一个 JavaScript 引擎和 React Native 的运行时 | 应用的 Go 二进制文件（Linux 上的 GTK 4 和 libadwaita 由系统提供）；除非应用显示了 `WebView`，否则不带 web view |

web view 应用在所有系统上都长得一样，完全按它的 HTML 和 CSS 呈现，并且能用上 Web 生态里的各种库。Asuka 应用则和各平台的原生应用看起来一样（不设置任何样式的应用，在 Mac 上就是一个 Mac 应用，在 Linux 上就是一个 GNOME 应用，在 Windows 上就是一个 Windows 11 应用），也能获得它们的无障碍支持、文本输入和菜单，同时所有平台共用一套用 Go 写的布局引擎。桌面宿主在应用自己的进程里运行 Go 代码，不借助 cgo 就能调用平台接口，所以 `asuka build` 在任何系统上都能构建 Windows 和 Linux 版本。

## 桌面应用的能力

桌面应用在窗口和菜单栏之外还需要哪些能力，这里与 Electron 和 MyGo 做个对比（见[桌面端](desktop.md)）。除非该行另有说明，✓ 表示 Mac、Windows 和 Linux 都支持。

| | Electron | MyGo | Asuka |
|---|---|---|---|
| 控制窗口 | `BrowserWindow` | `Window` | ✓ `ui.UseWindow`、`ui.MainWindow`：显示、隐藏、聚焦、最小化、最大化、全屏、大小、位置、置顶，窗口状态以 signal 形式提供；`OnCloseRequest`；按 `StateKey` 记住窗口位置；子窗口和模态窗口；隐藏标题栏（Wayland 上无法设置位置、居中和置顶，这些由合成器决定；X11 上通过 Xlib 实现） |
| 托盘图标 | `Tray` | 托盘图标 | ✓ `tray.Show`（Mac 上是 `NSStatusItem`；Windows 上是通知区域；Linux 上是 StatusNotifierItem）：图标或符号、标题、悬停提示、菜单、点击 |
| 子菜单、退出菜单项 | `MenuItem.submenu`、roles | roles、子菜单 | ✓ `MenuItem.Submenu`、`ui.MenuQuit`，可用于菜单栏、托盘菜单和程序坞菜单 |
| 程序坞菜单、跳转列表任务 | `app.dock.setMenu`、`setUserTasks` | 程序坞菜单、跳转列表 | ✓ `tray.DockMenu`（程序坞菜单、跳转列表里的“任务”、Linux 启动器的快捷列表） |
| 在后台运行的应用 | `app.dock.hide`，以及在 `window-all-closed` 时不退出 | 后台应用 | ✓ `app.KeepRunning`、`app.RunInBackground`、`asuka.json` 的 `background` |
| Flyout：从托盘图标弹出的窗口 | 由应用自己把无边框的 `BrowserWindow` 摆到 `tray.getBounds()` 的位置 | flyout | ✓ `WindowProps.Flyout`，从 `tray.Icon.Anchor()` 或所在窗口的某个矩形区域弹出：在显示器范围内摆放，放不下时翻转方向，点击别处或按 Escape 关闭，上面还可以再打开模态对话框（Linux 上只有 X11 能定位） |
| 启动器面板 | `type: 'panel'`（macOS） | 面板 | ✓ `Panel`：浮在所有应用和所有桌面空间（Space）之上，在 Mac 上能接收键盘输入而不激活应用，按 Escape 或点击别处时关闭（Linux 的 Wayland 下就是普通窗口） |
| 窗口大小随内容变化 | 由应用自己调用 `setContentSize` | `FitContent` | ✓ `FitContent`：内容变化时，由核心的布局来决定窗口大小 |
| 点击穿透窗口 | `setIgnoreMouseEvents` | `SetIgnoreMouseEvents`、`SetInputRegion` | ✓ `ClickThrough`、`WindowHandle.SetClickThrough`（作用于整个窗口） |
| 列出已安装的应用（用于启动器） | 无 | `InstalledApps`、`FileIcon`、`LaunchApp` | ✓ `asuka/sdk/apps` 的 `List`、`Search`、`Icon`、`Open` |
| 全局快捷键 | `globalShortcut` | 全局快捷键 | ✓ `hotkey.Register`（Mac 上用 Carbon 的热键；Windows 上用 `RegisterHotKey`；Linux 上用 X11 的按键抓取或 Wayland 的 GlobalShortcuts portal） |
| 打开、保存、选择文件夹对话框 | `dialog.showOpenDialog`、`showSaveDialog` | 对话框 | ✓ `asuka/sdk/dialog` 的 `Open`、`Save`、`Folder`（Mac 上以 sheet 显示；Web 上用 File System Access 选择器或文件输入框；手机上只支持 `Open`） |
| 在文件夹中显示、用默认应用打开、移到废纸篓 | `shell.showItemInFolder`、`openPath`、`trashItem` | shell | ✓ `asuka/sdk/shell` 的 `Reveal`、`Open`、`Trash`、`Beep` |
| 文件关联 | `fileAssociations`（electron-builder）、`open-file` | 文件关联 | ✓ `asuka.json` 的 `fileTypes`、`ui.OnOpenFiles`（iOS、Android 和已安装的 Web 应用也支持） |
| 单实例，以及处理再次启动 | `requestSingleInstanceLock`、`second-instance` | 单实例 | ✓ `app.OnSecondInstance`、`asuka.json` 的 `singleInstance`（Mac 上传过来的是文件，而不是命令行参数） |
| 退出，以及退出前询问 | `app.quit`、`before-quit` | 退出 | ✓ `app.Quit`、`app.OnQuit`（`Cancel`、`SessionEnding`） |
| 应用的各类目录 | `app.getPath` | 目录 | ✓ `app.DataDir`、`ConfigDir`、`CacheDir`、`LogDir`（所有平台都可用） |
| 最近使用的文档 | `app.addRecentDocument` | 最近使用的文档 | ✓ `asuka/sdk/recent`（Mac 上的“打开最近使用”、Windows 的跳转列表、Linux 上用户的最近文件） |
| 基于文档的应用 | — | 文档 | ✓ `asuka/sdk/document`：每个文件一个窗口、File 菜单、自动保存、恢复副本、关闭和退出时询问是否保存、处理被其他应用改动的文件（手机和 Web 上一次只能打开一个文档，原地保存） |
| 登录时启动 | `app.setLoginItemSettings` | 登录时启动 | ✓ `asuka/sdk/login`（Mac 上用 `SMAppService`，需要 macOS 13+；Windows 上用注册表的 Run 键；Linux 上用 XDG autostart 或 Background portal） |
| 监视文件 | Node 的 `fs.watch`（chokidar） | `WatchFiles`、watch 插件 | ✓ `asuka/sdk/watch` 的 `Path`、`New`（FSEvents、inotify、`ReadDirectoryChangesW`；也支持 iOS 的 kqueue、Android 的 inotify，以及 Web 上应用自己的文件）：重命名会成对报告，原子保存视为一次写入，短时间内的连续变化会合并 |
| 显示器 | `screen` | 显示器 | ✓ `screen.UseDisplays`、`Nearest`、`Matching`（Wayland 上拿不到工作区域；手机和 Web 上返回各自的屏幕） |
| 睡眠、唤醒、锁屏、空闲时间、电池 | `powerMonitor` | 电源 | ✓ `asuka/sdk/power`（手机上支持电池、低电量模式和锁屏；Web 上只有电池） |
| 应用图标上的进度和提醒 | `setProgressBar`、`flashFrame`、`dock.bounce` | 程序坞和任务栏 | ✓ `dock.SetProgress`、`RequestAttention`（Linux 上需要 dock 支持 Unity launcher API） |
| 强调色、增强对比度、降低透明度 | `systemPreferences` | 强调色和无障碍设置 | ✓ `asuka/sdk/appearance`（Linux 上没有降低透明度） |
| 正在播放和媒体键 | （无：只能用页面里的 `navigator.mediaSession`） | 正在播放 | ✓ `asuka/sdk/nowplaying`（Mac 上用 `MPNowPlayingInfoCenter`；Windows 上用窗口的 SystemMediaTransportControls；Linux 上是一个 MPRIS 播放器） |
| 自动更新：签名发布、增量更新、Sparkle 那样的更新窗口 | `autoUpdater`（Squirrel）、electron-updater | 更新器及其插件 | ✓ `asuka/sdk/update`（`Window`、`CheckForUpdates`、`Check`、`Install`、`Relaunch`；Ed25519 签名、增量更新、更新渠道；可以发布到 GitHub releases、S3 存储桶、任意 HTTPS 目录，或者你自己的服务器；`asuka publish --platform mac\|windows\|linux`）；通过系统包管理器、Flatpak 或应用商店安装的应用由它们负责更新；Web 则重新加载页面 |

MyGo 有而 Asuka 还没有的：文档方面，缺少版本（Mac 上的“浏览所有版本”）和锁定；点击穿透窗口方面，缺少输入区域（也就是窗口里仍然接收点击的那部分：Asuka 的窗口要么整个让鼠标穿过，要么完全不穿过）。

## 尚未支持

按应用最常缺的程度排序：带参数和实体的 App Intents（目前的快捷指令都不能带参数）、使用应用自定义布局的小组件（目前只有一个模板），以及 OTA 更新（即 EAS Update 那样的功能；Asuka 应用是编译好的 Go 代码，而应用商店不允许应用自行替换它）。Mac 上还缺：`ScreenOptions.ZoomFrom` 的缩放效果（推入时改为滑动）、用手势开始列表的多选，以及用 `asuka publish` 上传到 Mac App Store（目前请用 Transporter 上传 `.pkg`）。

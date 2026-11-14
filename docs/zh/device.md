# 设备与服务

Asuka 提供了一系列包，让应用用上视图之外的设备能力：相机、条码、图像内容识别、照片和文件、文件预览、打印和 PDF、邮件和短信编辑界面、定位、通知和推送、声音、正在播放（Now Playing）和录音、网络状态、共享的媒体库、后台任务、接收其他应用的分享、JavaScript、登录和生物识别、屏幕、地图、应用商店提供的服务，以及崩溃报告。本页逐一介绍这些包：它们能做什么、API 怎么用、会给应用带来哪些权限，以及在各个平台上的表现。

每个包在所有平台上都是同一套 API，iOS 和 Android 几乎支持其中的全部功能。在 macOS、Windows 和 Linux 上，宿主会用桌面系统自己的服务来实现各个包；在浏览器中则用 Web API。如果某个平台根本没有对应的能力，包的表现就和在一台缺少该功能的手机上一样：返回 `ErrNotAvailable`，返回一个说明缺了什么的错误，或者调用了也什么都不做。每个包的小节都会说明它能做什么、在各平台上分别靠什么实现。每个桌面平台和浏览器也各有专门的一节（[在 Mac 上](#在-mac-上)、[在 Windows 上](#在-windows-上)、[在 Linux 上](#在-linux-上)、[在浏览器中](#在浏览器中)），介绍应用在那里需要做哪些配置，并用一张表列出它与 iOS 和 Android 的不同之处。

## 权限与用途说明

应用导入某个包时，会自动获得这个包所需的一切：Android 清单里的权限、iOS Info.plist 里的用途说明（purpose string），以及平台代码本身。没有导入的应用就不会带上这些，所以一个照片应用不会去申请麦克风权限。Mac 应用会得到同样的用途说明，以及它们所需的 entitlement；Windows、Linux 和 Web 则不需要声明任何东西（见下文各自的小节）。

| 包 | Android | iOS |
| --- | --- | --- |
| `asuka/sdk/camera` | `CAMERA` | `NSCameraUsageDescription` |
| `asuka/sdk/camera/record` | `RECORD_AUDIO` | `NSMicrophoneUsageDescription` |
| `asuka/sdk/location` | `ACCESS_COARSE_LOCATION`、`ACCESS_FINE_LOCATION`；调用了 `RequestAlways` 的应用另有 `ACCESS_BACKGROUND_LOCATION`；调用了 `StartUpdates` 的另有 `FOREGROUND_SERVICE_LOCATION` 和一个前台服务；调用了 `StartUpdates` 或 `Monitor` 的另有 `RECEIVE_BOOT_COMPLETED`、一个广播接收器和 Play 服务的定位库 | `NSLocationWhenInUseUsageDescription`；调用了 `RequestAlways` 的应用另有 `NSLocationAlwaysAndWhenInUseUsageDescription`；调用了 `StartUpdates` 的另有 `location` 后台模式；调用了 `RequestPrecise` 的另有 `NSLocationTemporaryUsageDescriptionDictionary` |
| `asuka/sdk/notify` | `POST_NOTIFICATIONS` | |
| `asuka/sdk/network` | `ACCESS_NETWORK_STATE` | |
| `asuka/sdk/library` | `WRITE_EXTERNAL_STORAGE`（Android 9 及更早版本） | `NSPhotoLibraryAddUsageDescription`；应用的 Documents 目录会出现在“文件”App 中 |
| `asuka/sdk/background` | `FOREGROUND_SERVICE`、`FOREGROUND_SERVICE_DATA_SYNC`、`POST_NOTIFICATIONS`、`WAKE_LOCK`；一个前台服务 | |
| `asuka/sdk/scan` | ML Kit 的条码模型（约 2 MB） | |
| `asuka/sdk/vision` | ML Kit 的文字、人脸和条码模型（约 30 MB） | |
| `asuka/sdk/photos` | `READ_MEDIA_IMAGES`、`READ_MEDIA_VISUAL_USER_SELECTED`；`READ_EXTERNAL_STORAGE`（Android 12 及更早版本） | `NSPhotoLibraryUsageDescription` |
| `asuka/sdk/push` | Firebase Cloud Messaging、`POST_NOTIFICATIONS` | `aps-environment` entitlement；`remote-notification` 后台模式 |
| `asuka/sdk/appleauth` | | 通过 Apple 登录（Sign in with Apple）的 entitlement |
| `asuka/sdk/biometric` | `USE_BIOMETRIC` | `NSFaceIDUsageDescription` |
| `asuka/sdk/audio/record` | `RECORD_AUDIO`、`FOREGROUND_SERVICE_MICROPHONE`；一个前台服务 | `NSMicrophoneUsageDescription` |
| `asuka/sdk/nowplaying` | `FOREGROUND_SERVICE`、`FOREGROUND_SERVICE_MEDIA_PLAYBACK`；一个前台服务 | `audio` 后台模式（仅限调用了 `nowplaying.Show` 的应用） |
| `asuka/sdk/liveactivity` | `POST_NOTIFICATIONS`、`POST_PROMOTED_NOTIFICATIONS` | `NSSupportsLiveActivities` 和小组件扩展（仅限调用了它的应用） |
| `asuka/sdk/transcribe` | `RECORD_AUDIO` | `NSSpeechRecognitionUsageDescription` |
| `asuka/sdk/screen` | `DETECT_SCREEN_CAPTURE` | |
| `asuka/sdk/tracking` | | `NSUserTrackingUsageDescription` |
| `asuka/sdk/review` | Play 的应用内评价库 | |
| `asuka/sdk/integrity` | Play Integrity | App Attest 的 entitlement |
| `asuka/sdk/agerange` | Play Age Signals | Declared Age Range 的 entitlement |
| `asuka/sdk/purchase` | Play Billing、`com.android.vending.BILLING` | StoreKit 2（仅限调用了它的应用）；不需要 entitlement |
| `asuka/sdk/maps` | Google 的 Maps SDK | |
| `asuka/sdk/periodic` | WorkManager | `fetch` 和 `processing` 后台模式；各任务的标识符 |
| `asuka/sdk/sensors` | `ACTIVITY_RECOGNITION`（计步器） | `NSMotionUsageDescription`（计步器） |
| `asuka/sdk/speech` | 文字转语音引擎的可见性声明（`<queries>`） | |
| `asuka/sdk/contacts` | `READ_CONTACTS`（用于 `List`） | `NSContactsUsageDescription` |
| `asuka/sdk/calendar` | `READ_CALENDAR`（用于 `Calendars`、`Events`） | `NSCalendarsFullAccessUsageDescription`、`NSCalendarsUsageDescription` |
| `asuka/sdk/compose` | 处理 `mailto:` 和 `smsto:` 的应用的可见性声明（`<queries>`） | `LSApplicationQueriesSchemes` 中的 `mailto` |

`asuka/sdk/device`、`asuka/sdk/audio`、`asuka/sdk/splash`、`asuka/sdk/imageedit`、`asuka/sdk/widget`、`asuka/sdk/js`、`asuka/sdk/secure`、`asuka/sdk/preview` 和 `asuka/sdk/print` 不需要任何权限。

要在发布构建中使用某个 entitlement，必须先在应用的 App ID 上开启对应的 capability（在 developer.apple.com 的 Identifiers 页面），这样描述文件（provisioning profile）里才会包含它。如果描述文件缺了某一项，`asuka build --platform ios` 会停下来并告诉你缺的是什么；模拟器构建则所有 entitlement 都有。

Asuka 写入的用途说明是通用的（"To take a photo."）。iOS 和 macOS 向用户请求权限时会显示这段文字，所以最好在 `asuka.json` 的 `ios.usage` 里写上你自己的说明：

```json
{
  "ios": {
    "usage": {
      "NSCameraUsageDescription": "To photograph your receipts.",
      "NSLocationWhenInUseUsageDescription": "To find stores near you."
    }
  }
}
```

每个包都是在第一次真正需要时才询问用户，而不是在应用启动时。需要等待的调用（等用户回应、等定位结果、等平台处理）在下文中会标注“不能在主线程上调用”：请在 goroutine 中调用它们，再用 `ui.Post` 把结果送回主线程（见[主线程](concepts.md#主线程)）。其余的调用都接受一个回调，回调会在主线程上执行。

## 在 Mac 上

Mac 应用（无论是 `asuka dev` 还是 `asuka build` 生成的）的 Info.plist 里有同样的用途说明（定位的那条还会额外写成 `NSLocationUsageDescription`）。它的签名带有 hardened runtime 为这些请求所需的 entitlement（相机、麦克风、定位、通讯录、日历、照片）；缺了它们，Developer ID 构建的应用就拿不到相应的权限。推送的 `aps-environment`、通过 Apple 登录和 App Attest 这几项 entitlement 只能由描述文件授予：对于用到它们的应用，`asuka build` 会嵌入一个描述文件（优先取 `asuka.json` 的 `mac.profile`，其次是 `ASUKA_MAC_PROFILE`，最后是本机已安装的最佳匹配项）。没有描述文件时，应用照样能构建，CLI 会告诉你哪些功能将无法使用：在 Mac 上，注册推送和通过 Apple 登录都会失败，App Attest 会报告不受支持。`asuka dev` 使用 ad hoc 签名，不带描述文件，所以开发时也是同样的情况。Mac App Store 构建（`--appstore`）运行在沙盒中，可以访问网络以及用户选择的文件。详见[发布](shipping.md)。

这些包在 macOS 上的工作方式与 iOS 相同，用的也是同一套框架（AVFoundation、PhotoKit、Core Location、MapKit、Vision、Speech、UserNotifications、AuthenticationServices），只有下面这些 Mac 与之不同的地方例外：

| 包 | 在 Mac 上 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` 和 `PickFiles`：一个以 sheet 形式附着在窗口上的打开面板（无需权限），选中的文件会复制到应用的缓存目录。`Browse` 在用户的浏览器中打开页面。`GetInfo`：`Model` 为 `"Mac"`，`System` 为 `"macOS"` |
| `asuka/sdk/camera` | `Take`：在窗口上弹出一个面板，带实时预览、“拍照”和“取消”按钮（AppKit 没有相机界面）。`View` 的“后置”摄像头就是内置摄像头（Mac 没有后置摄像头），或者外接的那个；在其中扫码需要 macOS 13 |
| `asuka/sdk/audio/record` | 没有音频会话：其他应用的声音会照常播放；`Background` 不起作用，因为 Mac 应用在后台本来就能录音 |
| `asuka/sdk/nowplaying` | 和 iOS 一样使用 `MPNowPlayingInfoCenter` 和 `MPRemoteCommandCenter`：对应控制中心和菜单栏里的“正在播放”、键盘上的媒体键以及 AirPods。Mac 不需要音频会话，也不需要后台模式 |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | `Add` 会在窗口上弹出一个警告框来确认（macOS 没有新建联系人或日程的界面），然后保存；保存前会先请求访问权限 |
| `asuka/sdk/photos` | 和 iOS 一样使用 PhotoKit；Mac 没有“有限访问”的照片库，所以 `ManageLimited` 什么也不做 |
| `asuka/sdk/library` | 照片和视频存入“照片”App；`Downloads` 和 `Music` 分别存到用户的 `~/Downloads` 和 `~/Music` 下指定的文件夹 |
| `asuka/sdk/biometric` | Touch ID 的提示框，或者输入用户密码（设置了 `NoPasscode` 时除外） |
| `asuka/sdk/push` | 和 iOS 一样使用 APNs；不支持 `Rewrite`（它需要通知服务扩展）。需要描述文件（见上文） |
| `asuka/sdk/appleauth` | 和 iOS 一样，使用 Apple 提供的按钮和面板。需要描述文件（见上文） |
| `asuka/sdk/integrity` | 和 iOS 一样使用 App Attest。需要描述文件（见上文）：没有的话 `Supported` 为 false |
| `asuka/sdk/background` | 任务会阻止 App Nap 拖慢应用；没有通知，所以 `Update` 不起作用 |
| `asuka/sdk/periodic` | `NSBackgroundActivityScheduler`：任务只在应用运行期间执行（系统不会为了它们启动应用）；需要接通电源的任务，会在 Mac 使用电池时等待 |
| `asuka/sdk/screen` | `Lock` 和 `SetBars` 什么也不做；`PreventCapture` 让应用的窗口不出现在截图和录屏中；`OnScreenshot` 能收到 Spotlight 找到的截图 |
| `asuka/sdk/transcribe` | 只在本机识别；应用的 bundle 需要它的用途说明，这一项由 `asuka build` 写入 |
| `asuka/sdk/sensors` | Mac 没有运动传感器：`Available` 为 false，监听收不到任何数据，`StepsBetween` 以 `ErrNotAvailable` 失败 |
| `ui.Haptic` | 触控板的反馈（需要 Force Touch 触控板）：`HapticSelection` 对应“对齐”反馈，其他类型对应“级别变化”反馈 |
| `asuka/sdk/widget`、`asuka/sdk/liveactivity`、`asuka/sdk/agerange` | Mac 上不支持：`widget.Set` 什么也不做，`liveactivity.Start` 和 `agerange.Request` 以 `ErrNotAvailable` 失败 |
| `asuka/sdk/purchase` | StoreKit 2，编译进应用的 Swift 库；导入了这个包的应用，构建时会生成这个库（见[应用内购买](purchases.md)） |
| `asuka/sdk/splash` | Mac 应用没有启动屏：`Hold` 和 `Hide` 什么也不做 |
| `ui.OnShare`、`app.ShareSheet`、快捷操作 | 仅限 iOS 和 Android：Mac 应用不会出现在其他应用的分享菜单里，`asuka.json` 的 `shortcuts` 也不会被添加 |

## 在 Windows 上

Windows 应用不需要声明任何东西：`asuka build --platform windows` 不会写入任何 capability，Windows 也不要求桌面应用申请权限。不过，“设置”里的隐私开关（隐私和安全性）对相机、麦克风和定位依然有效：如果某个开关是关着的，相机和录制会以 `"not allowed"` 失败，定位则以 `location.ErrDenied` 失败。

这些包在 Windows 上通过系统自己的服务实现（Media Foundation、WIC、WinRT、Windows Hello、SAPI、凭据管理器），只有下面这些 Windows 与之不同的地方例外；Windows 完全不具备的能力，包的表现就和在缺少该功能的设备上一样：

| 包 | 在 Windows 上 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` 和 `PickFiles`：窗口上弹出的打开对话框，选中的文件会复制到应用的缓存目录。`Browse` 在用户的浏览器中打开页面。剪贴板里的图片是 PNG。`KeepAwake` 让显示器保持常亮。`GetInfo`：`Model` 为 `"PC"`，`System` 为 `"Windows"`，`SystemVersion` 为 `"10.0.<build>"` |
| `asuka/sdk/secure` | 凭据管理器中用户的普通凭据，名为 `<app>/<key>`（每个值最多 2560 字节） |
| `asuka/sdk/camera` | Media Foundation 的捕获引擎。`Take`：窗口上弹出一个对话框，带实时预览、“拍照”和“取消”按钮（对应 Enter 和 Escape 键）。`View` 选用 Windows 标为前置或后置的摄像头，找不到就用第一个；设置了 `Scan` 时，它每秒取四帧，用 Asuka 自带的识别器读取其中的条码（与 `asuka/sdk/scan` 的 `File` 相同）。“设置”中的相机开关关闭时："not allowed" |
| `asuka/sdk/camera/record` | MPEG-4 封装（`.mp4`）的 H.264 和 AAC，使用默认麦克风，除非设置了静音 |
| `asuka/sdk/audio` | MCI（DirectShow 的播放功能）；URL 会先下载到应用的缓存目录 |
| `asuka/sdk/audio/record` | 默认麦克风，44.1 kHz 的 AAC（语音为单声道，音乐为立体声）；`Background` 不起作用 |
| `asuka/sdk/nowplaying` | 应用窗口的 SystemMediaTransportControls（没有窗口的应用会用一个自己的隐藏窗口）：对应 Windows 10 的音量浮出面板、Windows 11 快速设置里的媒体控件、锁屏以及键盘上的媒体键。跳过操作对应它的快进和快退按钮；它没有切换操作（播放/暂停键发送的是播放或暂停） |
| `asuka/sdk/speech` | SAPI 的语音，即系统已安装的那些（设置 > 时间和语言 > 语音） |
| `asuka/sdk/network` | 网络列表管理器（Network List Manager）：Wi-Fi、蜂窝网络或有线网络；按流量计费的连接为 `Metered`，接近或超出流量上限时为 `Constrained` |
| `asuka/sdk/location` | WinRT 的 Geolocator。`PlaceName` 查询 OpenStreetMap 的 Nominatim（Windows 自带的地理编码器需要 Bing Maps 密钥）：每秒最多发一次请求，查询结果保存在应用的缓存目录中。区域监控用 `GeofenceMonitor`，仅在应用运行期间有效；后台定位更新根据位置计算 |
| `asuka/sdk/biometric` | Windows Hello 的提示框；`Check` 返回的类型就是传感器的类型；Hello 总会提供 PIN 选项，取消按钮的文字也由它自己决定 |
| `asuka/sdk/webauth` | 在应用窗口之上打开一个内嵌 WebView2 的窗口（使用应用的 Web 配置文件；设置 `Ephemeral` 时使用一个用完即弃的配置文件） |
| `asuka/sdk/js` | 一个从不显示的 WebView2 |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | Windows 只向打包（packaged）应用开放通讯录和日历：`Request` 会返回拒绝；`Add` 会在用户的联系人或日历应用中打开一个 vCard 或日程文件（`saved` 为 false，因为系统不会告知结果） |
| `asuka/sdk/photos` | “图片”文件夹（无需申请权限）；截图就是其中“屏幕截图”文件夹里的图片 |
| `asuka/sdk/library` | 用户的“下载”“视频”“音乐”和“图片”文件夹下指定的文件夹 |
| `asuka/sdk/vision` | Windows 的 OCR（支持哪些语言取决于“设置 > 时间和语言”中安装了哪些 OCR 语言）及其人脸检测器；条码的识别方式与 `asuka/sdk/scan` 相同 |
| `asuka/sdk/scan` | Windows 没有供应用使用的条码识别器，因此 Asuka 自带了一个用 Go 写的：能识别 QR 码、Data Matrix、EAN-13 和 EAN-8、UPC-A 和 UPC-E、Code 128 和 Code 39（不支持 Aztec、PDF417、Code 93、Codabar 和 ITF） |
| `asuka/sdk/imageedit` | 用 Windows 的编解码器读取图片（装了相应扩展时支持 HEIC 和 WebP）；视频帧通过 Media Foundation 获取 |
| `asuka/sdk/maps` | Windows 没有供桌面应用使用的地图控件（它自带的那个需要 Bing Maps 密钥），所以由宿主自己绘制 OpenStreetMap 的瓦片：每次下载两张，在应用的缓存目录中保留一周，地图上标注“© OpenStreetMap contributors”；支持拖动、滚轮或双指缩放、双击，以及 + 和 - 键。Satellite 和 Hybrid 显示的也是街道图（没有免费的卫星影像），`ShowsUser` 不显示任何东西，这两点与 Linux 相同 |
| `asuka/sdk/background` | 任务会阻止电脑进入睡眠；没有通知，所以 `Update` 不起作用 |
| `asuka/sdk/periodic` | 应用运行期间用定时器执行（系统不会为了它们启动应用）；需要接通电源的任务，会在电脑使用电池时等待 |
| `asuka/sdk/screen` | `Lock` 和 `SetBars` 什么也不做；`PreventCapture` 让应用的窗口不出现在截图和录屏中；`OnScreenshot` 能收到保存在“屏幕截图”文件夹里的截图（Windows 键 + Print Screen、截图工具） |
| `asuka/sdk/tracking` | 没有提示框：“设置”中的广告 ID 开着就是允许，否则就是拒绝 |
| `asuka/sdk/push`、`asuka/sdk/widget`、`asuka/sdk/review`、`asuka/sdk/integrity` | 这些能力只属于打包（应用商店）应用，或者依赖 Apple 和 Google 的服务：`push.Register` 会失败，`widget.Set` 什么也不做，`review.Request` 不显示任何东西，`integrity.Supported` 为 false |
| `asuka/sdk/liveactivity` | 这是手机的能力：`Enabled` 为 false，`Start` 以 `ErrNotAvailable` 失败 |
| `asuka/sdk/purchase` | Microsoft Store，仅限从商店安装的应用；否则 `Available` 为 false，其余调用以 `ErrNotAvailable` 失败（见[应用内购买](purchases.md)） |
| `asuka/sdk/appleauth`、`asuka/sdk/agerange`、`asuka/sdk/transcribe`、`asuka/sdk/sensors` | Windows 上不支持：返回不可用，与在缺少这些功能的设备上一样 |
| `ui.Haptic` | 电脑没有触感反馈：什么也不做 |
| `asuka/sdk/splash` | Windows 应用没有启动屏：`Hold` 和 `Hide` 什么也不做 |
| `ui.OnShare`、`app.ShareSheet`、快捷操作 | 仅限 iOS 和 Android |

## 在 Linux 上

Linux 应用同样不需要声明任何东西。凡是宿主通过 XDG 桌面门户（portal）来做的事情（文件、相机、定位、通知、邮件、打印、打开文件和链接、阻止系统休眠），都由门户按它自己的方式询问用户，而且在 Flatpak 沙盒内外都能正常工作。`asuka build --platform linux` 生成的 Flatpak 清单只允许应用访问显示、GPU、网络和门户。宿主不经门户、直接访问的那些服务（Speech Dispatcher、提供通讯录和日历的 Evolution Data Server、提供连接类型的 NetworkManager、iio-sensor-proxy、Video4Linux 摄像头），需要你在清单里加上开放它们的 `finish-args`；否则包的表现就和在没有这项服务的系统上一样。定时通知需要沙盒外的 systemd：在 Flatpak 中，它只会在应用运行时显示（见[各平台的情况](#各平台的情况)）。

Linux 宿主访问桌面服务时，有门户就走门户，此外还用到 GLib、GTK，以及桌面系统通常自带的一些库。每个库都在第一次用到时才加载：如果系统里没有，就会返回一个说明缺了什么的错误。

| 包 | 在 Linux 上 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` 和 `PickFiles`：GTK 的文件对话框（在沙盒中是门户提供的），选中的文件会复制到应用的缓存目录。剪贴板里的图片及剪贴板的变化通过 GDK 的剪贴板获取。`Browse` 在用户的浏览器中打开页面。`KeepAwake` 阻止会话进入空闲状态。`GetInfo`：`Model` 为 `"PC"`，`System` 为 `"Linux"`，`SystemVersion` 为发行版的版本（取自 `/etc/os-release`） |
| `asuka/sdk/secure` | 密钥环（通过 libsecret：GNOME Keyring、KWallet、KeePassXC），每个键存为一项，标签为应用名；没有 libsecret 或密钥环时，调用会失败并说明原因 |
| `asuka/sdk/network` | GNetworkMonitor（在沙盒中是门户提供的）；Wi-Fi、以太网还是蜂窝网络，由 NetworkManager 提供；从不报告受限 |
| `asuka/sdk/location` | 定位门户（背后是 GeoClue，由它询问用户），没有门户时直接使用 GeoClue；装了 geocode-glib 时，`PlaceName` 用它来查询。GeoClue 没有地理围栏：区域和后台定位更新都在应用运行期间根据位置计算 |
| `asuka/sdk/speech` | Speech Dispatcher（没在运行时会启动它）：使用它的语音、语速、音调和音量 |
| `asuka/sdk/audio`、`asuka/sdk/audio/record` | GStreamer：用 AAC 编码器（FDK、libav 的等等）录音，默认音源即麦克风 |
| `asuka/sdk/nowplaying` | 有内容显示时，在会话总线上注册一个 MPRIS 播放器，名为 `org.mpris.MediaPlayer2.<app>.instance<pid>`：对应 GNOME 和 KDE 的媒体控件与锁屏、媒体键以及 `playerctl`。如果应用支持跳过，MPRIS 的 `Seek` 就是跳过，否则是跳转播放位置；封面图是一个 `file://` 或 http(s) URL |
| `asuka/sdk/library` | 用户的文件夹（XDG 的 Pictures、Videos、Music、Downloads）下指定的文件夹 |
| `ui.ShareFiles` | 没有分享面板：文件会复制到剪贴板，并弹出一条提示（toast） |
| `asuka/sdk/camera`、`asuka/sdk/camera/record` | 优先用相机门户（只询问用户一次；使用 PipeWire 的摄像头，需要 `gstreamer1.0-pipewire`），否则用第一个 Video4Linux 摄像头，二者都通过 GStreamer。`Take`：窗口上弹出一个对话框，带实时预览、“拍照”和“取消”按钮。`View`：镜像显示的预览画面，填满它的区域；前置和后置是同一个摄像头（电脑的摄像头本来就朝着用户）。视频优先录成 MPEG-4（x264 或 OpenH264 提供的 H.264，加上 AAC），否则录成 WebM；扫码需要 GStreamer 的 `zbar` 元素。没有摄像头时为 `ErrNoCamera`；被拒绝时为 "not allowed" |
| `asuka/sdk/scan`、`asuka/sdk/vision` | 用 ZBar（`libzbar0`）识别条码：不支持 Aztec 和 Data Matrix。用 Tesseract（`libtesseract5` 加上对应语言的数据）识别文字；没有人脸检测，`Faces` 始终为空。没有 ZBar 时，`scan.File` 以 "not available" 失败；`vision.Analyze` 会跳过没有安装的部分 |
| `asuka/sdk/imageedit` | 用 GdkPixbuf 的加载器解码（装了相应加载器时支持 HEIF 和 WebP），并摆正方向；各项编辑操作用 Go 完成；视频帧来自 GStreamer |
| `asuka/sdk/webauth` | 窗口上弹出一个内嵌 WebKitGTK 网页视图的对话框（设置 `Ephemeral` 时使用独立的会话）；关闭对话框即取消 |
| `asuka/sdk/js` | 一个从不显示的 WebKitGTK 网页视图 |
| `asuka/sdk/maps` | 用 libshumate（`libshumate-1.0-1`）显示 OpenStreetMap 瓦片地图；Satellite 和 Hybrid 显示的也是街道图；没有用户位置图层。没有 libshumate 时，视图会显示相应说明，并调用 `OnError` |
| `asuka/sdk/background` | 任务会阻止系统挂起；没有通知，`Update` 不起作用 |
| `asuka/sdk/periodic` | 应用运行期间按定时器执行任务（系统不会为了它们启动应用）；需要接通电源的任务会在使用电池时等待，需要网络的任务会在离线时等待 |
| `asuka/sdk/screen` | `Lock`、`SetBars` 和 `PreventCapture` 什么也不做；`OnScreenshot` 能收到 Pictures/Screenshots 中新增的图片（GNOME 和 KDE 都把截图存在这里） |
| `asuka/sdk/sensors` | iio-sensor-proxy（适用于二合一设备、平板）：光线传感器（单位为 lux），以及由加速度计得出的方向，表示为沿朝上那条轴的 1 g，随设备转动而变化。没有其他传感器：`Available` 为 false，`StepsBetween` 以 `ErrNotAvailable` 失败 |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | Evolution Data Server 中的通讯录和日历（来自 GNOME 通讯录、GNOME 日历和在线账户）：有它时访问权限为 `Full`；`Pick` 是一个带搜索框的联系人对话框；`Add` 先确认，再保存到默认通讯录或该日程所属的日历。没有它时：通讯录的访问权限为 `Denied`，日历为 `WriteOnly`；`Add` 会在用户的联系人或日历应用中打开一个 vCard 或 iCalendar 文件（`saved` 为 false）；`contacts.Pick` 会失败 |
| `asuka/sdk/photos` 的照片库、`asuka/sdk/appleauth`、`asuka/sdk/transcribe`、`asuka/sdk/widget`、`asuka/sdk/liveactivity` | Linux 上都没有：照片库的访问权限为 `Denied`；`appleauth.Available` 为 false，它的按钮什么也不显示；`transcribe` 和 `liveactivity.Start` 以 `ErrNotAvailable` 失败；`widget.Set` 什么也不做。`asuka/sdk/splash` 也没有启动屏可以保持 |
| `asuka/sdk/purchase` | 没有应用商店：`Available` 为 false，其余调用以 `ErrNotAvailable` 失败 |
| `asuka/sdk/biometric`、`asuka/sdk/integrity`、`asuka/sdk/agerange`、`asuka/sdk/push` | Linux 上都没有：返回不可用或不受支持。`asuka/sdk/review` 什么也不做；`asuka/sdk/tracking` 总是返回允许 |
| `asuka/sdk/crash` | Go 的崩溃输出，其中也包括原生代码中的崩溃（GTK 代码里收到的信号、GLib 的 abort），这类崩溃报告为 `Native`；不会读取系统自己的崩溃记录（systemd-coredump、apport） |
| `ui.Haptic` | 桌面设备没有触感反馈：什么也不做 |
| `ui.OnShare`、`app.ShareSheet`、快捷操作 | 仅限 iOS 和 Android |

## 在浏览器中

网页不需要声明任何东西：某个包第一次需要用到相机、麦克风、定位或通知时，浏览器会询问用户。这些 API 大多要求页面处于安全上下文，也就是通过 `https` 或从 `localhost` 提供（`asuka dev --platform web` 的地址满足要求，`--lan` 的地址则不满足）。这些包调用的都是 Web API；详细说明见 [Web](web.md) 一页，那里也介绍了用于通知和 Web Push 的 service worker。

| 包 | 在浏览器中 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` 和 `PickFiles`：浏览器的文件选择器，选中的文件会复制到缓存中。`Browse`：打开新标签页。剪贴板：可以读取应用自己复制的内容，在用户允许页面读取剪贴板时，也能读取其他应用复制的内容。`KeepAwake`：使用 Screen Wake Lock。`GetInfo` 的信息来自 user agent（Chromium 的 client hints 能提供系统版本和 Android 手机的型号） |
| `asuka/sdk/secure` | 值经过加密（AES-GCM）后存在 IndexedDB 中，密钥由浏览器保管、无法导出；这并不是钥匙串（见 [Web](web.md)） |
| `asuka/sdk/camera`、`asuka/sdk/camera/record` | `Take` 在手机上打开相机，否则显示一个带实时画面和快门的对话框；`View` 是放在 `<video>` 中的摄像头画面，支持 `Capture` 和录制，浏览器有 `BarcodeDetector` 时还能扫码 |
| `asuka/sdk/scan`、`asuka/sdk/vision` | 用 `BarcodeDetector` 识别条码；人脸和文字只在浏览器支持时才能识别 |
| `asuka/sdk/imageedit` | 用 canvas 处理：输出 JPEG 或 PNG；远程图片需要 CORS |
| `asuka/sdk/photos` | 网页无法读取照片库：访问权限为 `Denied`（要选照片请用 `device.PickPhotos`） |
| `asuka/sdk/library` | `Save` 会把文件下载下来 |
| `asuka/sdk/location` | Geolocation API；Web 上没有 `PlaceName`（浏览器没有地理编码器）。区域和后台定位更新根据位置计算，只在页面打开期间有效 |
| `asuka/sdk/network` | 是否在线取自 `navigator.onLine`；浏览器提供相关信息时（Android 上的 Chrome）能区分 Wi-Fi、蜂窝网络和以太网，蜂窝网络视为按流量计费，开启省流量模式（Data Saver）时视为受限 |
| `asuka/sdk/notify`、`asuka/sdk/push` | 通过应用的 service worker 使用 Notification API 和 Web Push（`push.Register` 的 `Service` 为 `"webpush"`）；应用没有任何页面打开时，定时通知无法显示 |
| `asuka/sdk/audio`、`asuka/sdk/audio/record` | 每个播放器对应一个 audio 元素（受浏览器的自动播放规则约束：要在用户点按时开始播放）；录音用 `MediaRecorder`，浏览器支持时录成 MPEG-4 封装的 AAC，否则录成 WebM 封装的 Opus |
| `asuka/sdk/nowplaying` | Media Session API（`navigator.mediaSession`）：对应浏览器的媒体控件、媒体键和手机锁屏，在页面播放声音时显示。它没有切换操作（按键发送的是播放或暂停）；浏览器不支持的操作（有些浏览器不支持停止）会被略去 |
| `asuka/sdk/speech` | `speechSynthesis`，使用浏览器提供的语音 |
| `asuka/sdk/sensors` | Generic Sensor API（Chrome），或设备运动和方向事件（Safari 会在用户点按时询问），坐标轴和单位与 Android 一致；没有气压计和计步器 |
| `asuka/sdk/biometric` | 通过 WebAuthn 使用平台验证器（Touch ID、Face ID、Windows Hello、Android 的屏幕锁定）：第一次 `Authenticate` 会为该站点创建一个通行密钥（passkey）。提示框由浏览器提供，所以 `Reason` 和 `Cancel` 不会显示 |
| `asuka/sdk/webauth` | 一个弹出窗口，回到与应用同源的页面时即完成（自定义 scheme 无法打开页面） |
| `asuka/sdk/js` | 一个隐藏的沙盒 iframe |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | 浏览器支持 Contact Picker 时（Android 上的 Chrome）可以用 `contacts.Pick`；`Add` 会下载一个 vCard 或 `.ics` 文件；Web 上无法读取通讯录和日历 |
| `asuka/sdk/screen` | `Lock` 只在全屏时或已安装的应用中有效；隐藏系统栏就是进入全屏；没有 `PreventCapture` 和 `OnScreenshot` |
| `asuka/sdk/background` | 页面打开期间一直在运行；任务不会带来额外效果，也没有通知 |
| `asuka/sdk/tracking` | 没有提示框：浏览器发送了 Global Privacy Control 或 Do Not Track 时为 `Denied`，否则为 `Allowed` |
| `ui.Haptic` | 浏览器支持时（Android 上的 Chrome）使用 `navigator.vibrate` |
| `asuka/sdk/review`、`asuka/sdk/integrity`、`asuka/sdk/agerange`、`asuka/sdk/appleauth`、`asuka/sdk/periodic`、`asuka/sdk/widget`、`asuka/sdk/liveactivity`、`asuka/sdk/transcribe` | Web 上不支持：返回一个说明这一点的错误，或者该包的 `ErrNotAvailable` |
| `asuka/sdk/purchase` | 在 Trusted Web Activity 里通过 Digital Goods API 使用 Google Play；其他情况下没有应用商店（`ErrNotAvailable`） |
| `asuka/sdk/maps` | 和 Windows 一样，由宿主绘制 OpenStreetMap 的瓦片；页面获得定位权限后，`ShowsUser` 才会生效 |
| `asuka/sdk/crash` | Go 的崩溃（宿主会保留运行时的输出）和未捕获的 JavaScript 错误，都作为非致命的 `Exception` 报告 |
| `asuka/sdk/splash` | `Hold` 和 `Hide` 什么也不做 |
| `ui.OnShare`、`app.ShareSheet`、快捷操作 | 仅限 iOS 和 Android |

## 相机：asuka/sdk/camera

`camera.Take` 会打开系统相机（iOS 的相机界面、Android 的相机应用），拍完后把照片文件（一个 JPEG）传给 `done`；如果用户取消了，传入的是 `""`。在没有摄像头的设备上（比如 iOS 模拟器），错误是 `camera.ErrNoCamera`。桌面系统没有相机界面，所以在那里 `Take` 会在窗口上弹出一个 Asuka 自己的对话框，带实时预览、“拍照”和“取消”按钮；浏览器则会在手机上打开相机，否则显示一个类似的对话框。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Take a photo"), OnPress: func() {
	camera.Take(func(path string, err error) {
		switch {
		case errors.Is(err, camera.ErrNoCamera):
			status.Set("This device has no camera")
		case err != nil:
			status.Set(err.Error())
		case path != "":
			photo.Set(path) // 用 ui.Image 显示它
		}
	})
}})
```

如果想做一个应用自己的拍摄界面，可以用 `camera.View` 实时显示摄像头拍到的画面，并让它填满自己的区域（iOS 和 Mac 上是 AVFoundation 的预览，Android 上是 CameraX 的 `PreviewView`，Windows 上是 Media Foundation 的预览，Linux 上是 GStreamer 的预览，浏览器中是一个 `<video>`），再用 `camera.Capture` 从中拍照：

```go
camera.View(camera.ViewProps{
	Mode:    camera.Video,
	Facing:  camera.Back,
	Style:   ui.Style{Grow: 1},
	OnError: func(message string) { status.Set(message) },
}),
ui.Button(ui.ButtonProps{Title: ui.Val("Photo"), OnPress: func() {
	camera.Capture(func(path string, err error) {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set("Saved " + path)
	})
}}),
```

`ViewProps`：

- `Facing`：`camera.Back`（默认）或 `camera.Front`。
- `Mode`：`camera.Photo`（默认），或者 `camera.Video`，用于配合 `asuka/sdk/camera/record` 录像，这种模式下也能拍照。
- `Style`：视图本身的样式，决定它占据的区域。
- `OnError`：摄像头无法显示时调用：用户拒绝时为 `"not allowed"`，没有摄像头时为 `"no camera"`。
- `Scan`：识别画面中的条码；见 `asuka/sdk/scan`。

视图第一次显示时会请求摄像头权限；只要视图还在屏幕上，摄像头就保持开启。同一时间只显示一个相机视图：`Capture` 和录制使用的都是屏幕上的那一个，如果没有，就会以 `"no camera view"` 失败。

`Take` 和 `Capture` 生成的文件放在应用的缓存目录里，系统随时可能清理：想保留的话，把文件移到 `store.Dir()`，或者用 [`asuka/sdk/library`](#保存到媒体库asukasdklibrary) 存进照片库。

**Android**：由于 `asuka/sdk/camera` 声明了相机权限（视图需要它），Android 要求调用相机应用时也必须拥有这项权限。因此 `Take` 会先请求权限，用户拒绝时以 `"not allowed"` 失败。

## 录制视频：asuka/sdk/camera/record

`asuka/sdk/camera/record` 从屏幕上那个 `Mode` 为 `camera.Video` 的 `camera.View` 录制视频。`record.Start` 会一直录到调用 `record.Stop` 为止，设置了 `MaxDuration` 的话则最多录到这个时长，然后把视频文件传给 `done`。文件放在应用的缓存目录中：iOS 和 Mac 上是 `.mov`，Android 和 Windows 上是 `.mp4`，Linux 上视可用的编码器而定，优先 `.mp4`，否则为 `.webm`。

```go
ui.Button(ui.ButtonProps{
	Title: signal.Func(func() string {
		if recording.Get() {
			return "Stop"
		}
		return "Record"
	}),
	OnPress: func() {
		if recording.Peek() {
			record.Stop()
			return
		}
		recording.Set(true)
		record.Start(record.Options{MaxDuration: time.Minute}, func(path string, err error) {
			recording.Set(false)
			if err != nil {
				status.Set(err.Error())
				return
			}
			status.Set("Recorded " + path) // 用 ui.Video 播放它
		})
	},
})
```

录像时也会录下声音，第一次录制时会请求麦克风权限；如果用户拒绝，视频就没有声音。使用 `record.Options{Mute: true}` 则录制无声视频，也不会请求权限。同一时间只能有一个录制在进行。

麦克风权限和对应的用途说明是随这个包带进来的，而不是随 `asuka/sdk/camera`：只拍照的应用不会带上它们。

## 条码和二维码：asuka/sdk/scan

`asuka/sdk/scan` 用来识别条码和二维码，既可以实时识别 `camera.View` 画面中的，也可以识别图片文件中的。iOS 和 Mac 用 AVFoundation 和 Vision 识别；Android 用 ML Kit，只有导入了 `asuka/sdk/scan` 的应用才会打包它；Windows 上用 Asuka 自带的、以 Go 编写的识别器；Linux 用 ZBar；浏览器用 `BarcodeDetector`。并不是每个识别器都支持所有类型，详见[在 Windows 上](#在-windows-上)和[在 Linux 上](#在-linux-上)。

`scan.Codes` 用来生成视图的 `Scan`。它识别你指定的类型（不指定则识别所有类型）；每当有条码进入画面，就在主线程上调用传入的函数。条码一直留在画面中时只调用一次，离开画面一秒后再次出现时会再次调用。

```go
camera.View(camera.ViewProps{
	Style: ui.Style{Grow: 1},
	Scan: scan.Codes(func(c scan.Code) {
		found.Set(fmt.Sprintf("%s: %s", c.Kind, c.Value))
		ui.Haptic(ui.HapticSuccess)
	}, scan.QR, scan.EAN13),
})
```

`scan.File` 识别一张图片（拍的或者选的照片）中的条码，并把结果传给 `done`（一个都没有时就是空的）：

```go
scan.File(path, func(codes []scan.Code, err error) {
	switch {
	case err != nil:
		found.Set(err.Error())
	case len(codes) == 0:
		found.Set("No code in it")
	default:
		found.Set(codes[0].Value)
	}
}, scan.QR)
```

`scan.Code` 包含 `Kind`（类型）和 `Value`（码中的文本）。支持的类型有：`QR`、`Aztec`、`DataMatrix`、`PDF417`、`EAN13`、`EAN8`、`UPCA`、`UPCE`、`Code128`、`Code39`、`Code93`、`Codabar`、`ITF`。

**iOS** 会把 UPC-A 识别成前面带一个 0 的 EAN-13；如果你指定了 `UPCA`，Asuka 会把它还原成 UPC-A。

## 识别图像内容：asuka/sdk/vision

`vision.Analyze(path, options)` 在图片中查找文字、人脸和条码，并给出它们各自的位置。识别完全在设备本地进行：iOS 和 Mac 上用 Apple 的 Vision；Android 上用 ML Kit 内置的模型，只有导入了 `asuka/sdk/vision` 的应用才会打包这些模型；Windows 上用系统的 OCR 和人脸检测器；Linux 上用 Tesseract（不支持人脸）；浏览器中则取决于它的 Shape Detection API 支持哪些。不能在主线程上调用。

```go
go func() {
	r, err := vision.Analyze(photo, vision.Options{Languages: []string{"zh-Hans", "en-US"}})
	if err != nil {
		return
	}
	ui.Post(func() {
		for _, l := range r.Lines {
			fmt.Println(l.Text, l.Box)
			for _, w := range l.Words {
				fmt.Println(" ", w.Text, w.Box, w.Start, w.End)
			}
		}
		fmt.Println(len(r.Faces), "faces,", len(r.Codes), "codes")
	})
}()
```

- `Box` 用相对于图片尺寸的比例表示，从左上角算起，并以摆正后的图片为准：因此它与缩放比例无关，既可以用于叠在图片上的视图，也可以用于 `asuka/sdk/imageedit` 的像素坐标。
- `Line` 包含 `Text`、`Box` 和 `Words`。`Word` 是平台分词得到的一个词（中文和日文基于词典分词），或者一个标点符号。`Start` 和 `End` 是它在整行文本中的位置，以 rune 计。
- `Code` 包含 `Kind`（名称与 `asuka/sdk/scan` 一致）、`Value` 和 `Box`。
- 用 `Options` 可以关掉不需要的识别（`NoText`、`NoFaces`、`NoCodes`）。`Languages` 是文字可能使用的语言，可能性最大的排在最前，仅对 iOS 有效：Vision 只能很好地识别第一个语言所属的文字体系，所以如果文字中可能出现中文，就把中文放在最前面。它的中文模型也能识别拉丁字母。Android 则始终能识别中文和拉丁文字。

**iOS** 会把识别出的一组内容作为一个整体给出位置框：中文行里共用同一个框的几个词，会按各自的宽度分摊这个框，所以这种情况下单个词的框只是估算值。Vision 给出的条码框有时只是它扫过的那一条线，Asuka 会把它扩展到覆盖整个条码。**Android** 上每个词的框由其中各个字符的框合成。

OCR 会先把图片缩放到它自己的处理尺寸，所以在很长的截图里会漏掉小字：可以先用 `imageedit.Crop` 切成几段，再逐段分析。

## 照片库：asuka/sdk/photos

`asuka/sdk/photos` 在用户许可后读取用户的照片库：iOS 和 Mac 上用 PhotoKit，Android 上用 MediaStore，Windows 上读取“图片”文件夹。Linux 和 Web 上没有应用可以读取的照片库，访问权限为 `Denied`。这个包适合以照片库为主界面的应用。如果只是想让用户挑几张照片，用 `device.PickPhotos` 就够了，而且它不需要任何权限：能满足需求时请优先使用它。Google Play 会要求读取照片库的应用在 Play 管理中心说明理由。所有调用都需要等待，不能在主线程上调用。

```go
go func() {
	if a, _ := photos.Request(); a == photos.Denied {
		return
	}
	assets, _ := photos.List(photos.Query{Screenshots: true, Limit: 60})
	thumb, _ := photos.Thumbnail(assets[0].ID, 300) // 一个文件，可交给 ui.Image 显示
	full, _ := photos.Export(assets[0].ID)          // 原图，以文件形式导出
	fmt.Println(thumb, full.Path, full.Name)
}()
```

- `photos.Current()` 不询问用户，直接告诉你当前的授权状态：`NotAsked`、`Denied`、`Limited`（只能访问用户选定的照片：iOS 14、Android 14 起）或 `Allowed`。`photos.Request()` 会在第一次调用时询问用户。`photos.ManageLimited()` 让处于 `Limited` 状态的用户修改允许访问的照片。
- `photos.List(query)` 按从新到旧的顺序返回，从 `Offset` 开始，最多 `Limit` 个。每个 `Asset` 包含 `ID`、`Width`、`Height`、`Created()` 和 `Screenshot`。设置 `Screenshots` 后只返回截图：iOS 本身知道哪些是截图；Android 上则以 Screenshots 文件夹中的图片为准。
- `photos.Thumbnail(id, size)` 返回一个短边至少 `size` 像素的 JPEG，并保存在应用的缓存目录中，下次直接复用。
- `photos.Export(id)` 按照片库中的原始格式（HEIC、JPEG、PNG）复制完整的图片，并带上文件名。iCloud 中的照片会先下载。

## 照片、浏览器与剪贴板：asuka/sdk/device

`asuka/sdk/device` 汇集了平台在视图之外提供的各种能力。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Pick photos"), OnPress: func() {
	device.PickPhotos(3, func(paths []string, err error) {
		if err == nil && len(paths) > 0 {
			photos.Set(paths)
		}
	})
}}),
ui.Button(ui.ButtonProps{Title: ui.Val("Help"), OnPress: func() {
	device.Browse("https://example.com/help")
}}),
```

- `device.PickPhotos(limit, done)` 显示系统的照片选择器（iOS 上是 PHPicker，Android 上是 Photo Picker；桌面上是打开对话框，浏览器中是文件选择器），最多可选 `limit` 张图片（1 到 255）。它不需要任何权限：由用户自己挑选，应用只能看到被选中的图片。`done` 会收到这些图片的文件（可以直接交给 `ui.Image` 显示），用户取消时则为空。这些文件是放在应用临时目录或缓存目录中的副本，要保留的话请把它移走。
- `device.Browse(url)` 在应用内、覆盖在当前界面之上显示一个网页（SFSafariViewController、Custom Tab）。URL 必须是 http 或 https。而 `ui.OpenURL` 则会离开应用、跳转到浏览器。在桌面上，它会在用户的浏览器中打开页面；在浏览器中，则打开一个新标签页。
- `device.Clipboard()` 读取剪贴板里的文本；没有文本时 `ok` 为 false。不能在主线程上调用。应用第一次读取别的应用放进剪贴板的内容时，iOS 会询问用户。写入剪贴板请用 `ui.Copy`。
- `device.KeepAwake(on)` 在 `on` 为 true 时让屏幕保持常亮，适合视频、菜谱、地图这类场景。需要常亮的界面关闭时，记得把它关掉。
- `device.PickFiles(options, done)` 显示系统的文档选择器（iOS 的“文件”浏览器、Android 的存储选择器、桌面上的打开对话框、浏览器中的文件选择器；都不需要权限）。`Types` 是 MIME 类型（`"application/pdf"`、`"image/*"`；不填则可以选任何文件），`Multiple` 允许用户选择多个文件。`done` 会为每个文件收到一个 `device.File`：`Path` 是复制到应用缓存目录中的副本，另外还有 `Name`、`Type` 和 `Size`；用户取消时则为空。
- `device.ClipboardHas()` 不读取剪贴板，就能告诉你里面装的是 `Text`、`Image` 还是 `URL`（iOS 不会因此询问用户）：这样你可以只在有东西可粘贴时才显示“粘贴”按钮。`device.ClipboardImage()` 把剪贴板里的图片读到一个文件中，格式与复制时相同；`device.CopyImage(path, done)` 把一个图片文件放进剪贴板。`device.OnClipboardChange(f)` 在剪贴板内容变化时调用 `f`，直到调用 `stop` 为止。
- `device.GetInfo()` 返回设备和应用的信息：`Model`（"iPhone"、"Pixel 9"、"Mac"、"PC"）、`System`（"iOS"、"Android"、"macOS"、"Windows"、"Linux"）、`SystemVersion`，以及 `AppVersion` 和 `Build`，即 `asuka.json` 中的 `version` 和 `build`。不能在主线程上调用。

```go
func Player() ui.Node {
	info := signal.New("")
	go func() {
		if i, err := device.GetInfo(); err == nil {
			ui.Post(func() { info.Set(i.Model + ", " + i.System + " " + i.SystemVersion) })
		}
	}()
	device.KeepAwake(true)
	signal.OnCleanup(func() { device.KeepAwake(false) })
	return ui.Text(info)
}
```

## 网页登录：asuka/sdk/webauth

`webauth.Start` 在系统自带的登录面板中显示登录页面：iOS 上是 `ASWebAuthenticationSession`（也就是 SwiftUI 的 `webAuthenticationSession`），Android 上是 Custom Tab。这个面板属于浏览器，所以用户已有的登录会话、通行密钥和密码自动填充都能用，那些拒绝应用内 `WebView` 的登录服务（比如 Google 登录）也不会拒绝它。做 OAuth 时请用它，而不是 `ui.WebView`。

Mac 上同样有 `ASWebAuthenticationSession`。Windows 和 Linux 没有系统级的登录面板，宿主会在窗口之上用自己的网页视图（WebView2、WebKitGTK）显示页面。这个视图使用应用的 Web 配置文件，但没有用户浏览器中的登录会话；拒绝内嵌网页视图的登录服务，在这里同样会拒绝。浏览器则会在弹出窗口中打开页面，页面回到与应用同源的地址时即完成（自定义 scheme 无法打开页面）。

```go
s := webauth.Start(webauth.Options{URL: authorizeURL, CallbackScheme: "myapp"},
	func(callback string, err error) {
		if errors.Is(err, webauth.ErrCanceled) {
			return // 用户关掉了面板
		}
		// callback 形如 myapp://…?code=…：接下来用其中的 code 换取令牌。
	})
```

登录流程有两种结束方式：

- **通过回调 scheme。** 页面跳转到 `CallbackScheme`（应用自己的 scheme）下的 URL 时，面板关闭，`done` 收到这个 URL。在 Android 上，这个 scheme 必须就是应用的 scheme，即 asuka.json 中的 `scheme`；URL 会交给 `done`，而不是 `ui.OnOpenURL`。
- **由应用自己结束。** 不设置 scheme 时，页面会重定向到一个应用能收到通知的地方：如果 OAuth 服务器允许重定向到回环地址（`http://localhost:port/callback`，见 RFC 8252），它就会把 code 发给应用在该端口上运行的服务器，再由这个服务器调用 `s.Cancel()` 关闭面板。在 Android 上，取消会让应用回到标签页之上。

用户关闭面板，或者调用了 `Cancel` 时，`done` 会收到 `webauth.ErrCanceled`。`Ephemeral` 会请求一个私密会话，不带浏览器中的 cookie（iOS 的 `prefersEphemeralWebBrowserSession`；Android 的 Custom Tab 会保留 cookie）。[`examples/usage`](../../examples/usage) 就是这样借助一个回环服务器登录 Claude 的。

## 复制、分享和打开：ui.Copy、ui.Share、ui.OpenURL

`ui` 中有三个调用可以把文本交给系统处理，它们都在主线程上运行。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Copy link"), OnPress: func() { ui.Copy("https://example.com/s/42") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() { ui.Share("https://example.com/s/42") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Open in browser"), OnPress: func() { ui.OpenURL("https://example.com") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Call"), OnPress: func() { ui.OpenURL("tel:+15555550100") }}),
```

- `ui.Copy(text)` 把文本放进剪贴板。
- `ui.Share(text)` 显示系统的分享面板，用户可以把文本发给某个应用、联系人，或者通过 AirDrop、Nearby Share 发送。URL 会以链接的形式分享。Mac 显示它的共享服务选择器，Windows 显示它的分享界面，浏览器在支持时显示 Web Share 面板；Linux 没有分享面板，所以会把文本复制到剪贴板并弹出一条提示（不支持 Web Share 的浏览器也是这样复制的）。
- `ui.OpenURL(url)` 请系统打开一个 URL：网址在浏览器中打开，其他 scheme（`tel:`、`mailto:`、其他应用的 scheme）由能处理它的应用打开。

`ui.ShareFiles(paths, options, done)` 显示用于分享文件的面板：发送一段视频、把 PDF 存到“文件”、用 AirDrop 传照片。面板消失后，无论用户有没有分享，`done` 都会在主线程上运行；如果面板无法显示（比如文件不存在），它的 error 会说明原因。

```go
var share ui.Node
share = ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() {
	ui.ShareFiles([]string{report}, ui.ShareOptions{MIME: "application/pdf", Title: "Send the report", From: share}, func(err error) {
		if err != nil {
			status.Set(err.Error())
		}
	})
}})
```

`ShareOptions` 与 expo-sharing 的选项一致：

| 选项 | |
| --- | --- |
| `MIME` | 文件的类型，分享面板上的应用据此判断能否处理。Android 会把 intent 的类型设为它。在 iOS 上，`UTI` 为空时用它作为类型。默认：根据第一个文件的扩展名推断。 |
| `UTI` | iOS：文件的统一类型标识符（Uniform Type Identifier，如 `com.adobe.pdf`），优先级高于 `MIME`。 |
| `Title` | 面板的标题：Android 上是选择器的标题，在 iOS 上则作为邮件的主题。 |
| `From` | 面板以弹出框（popover）形式出现时（iPad、Mac）所指向的视图，通常就是触发分享的那个按钮。默认：屏幕中央。 |

在 iOS 上，如果文件的扩展名与它的类型对不上，会像 expo-sharing 那样换一个相符的文件名再分享：作为 JPEG 分享的 `image.pdf` 会以 `image.pdf.jpeg` 的名字发出，它是指向原文件的链接，而不是副本。在 Android 上，文件通过应用的 FileProvider 传递；不在应用自身目录中的文件，会先复制到缓存目录。Mac 上用的是共享服务选择器。Windows 上用的是分享界面（界面一出现就会调用 `done`，因为 Windows 不会告知它何时关闭）。Linux 没有分享面板：文件会放进剪贴板，与分享文本时一样。浏览器能用 Web Share 面板分享文件时就用它，否则会逐个下载这些文件。

如果想让应用自己能通过 URL 被打开，请参阅[发布](shipping.md#asukajson)中 `asuka.json` 的 `scheme` 和 `links`，以及 `ui.OnOpenURL`。

## 预览文件：asuka/sdk/preview

`preview.Show(sources, options, done)` 用系统的查看器显示文件，就像邮件应用显示附件那样：PDF、图片、视频、音频、Office 和 iWork 文档、文本都可以。来源可以是文件路径、资源路径（与 `ui.Image` 接受的格式相同），也可以是 http(s) URL；对于 URL，宿主会先把文件下载到应用的缓存目录，沿用原来的文件名（或者根据服务器返回的类型补上扩展名）。在主线程上调用。

```go
var open ui.Node
open = ui.Button(ui.ButtonProps{Title: ui.Val("Open the invoice"), OnPress: func() {
	preview.Show([]string{invoice, "docs/terms.pdf"}, preview.Options{From: open}, func(err error) {
		if errors.Is(err, preview.ErrNoViewer) {
			status.Set("Nothing on this device opens it")
		}
	})
}})
```

| 选项 | |
| --- | --- |
| `Start` | 最先显示的文件的索引。 |
| `From` | 查看器从哪个视图展开（iOS），通常是用户刚点按的那个。 |
| `Edit` | 允许用户用“标记”（Markup）功能在 PDF 和图片上标注，并保存回文件（iOS）。 |

查看器关闭后，`done` 会在主线程上运行：如果没有任何东西能显示这些文件，会收到 `preview.ErrNoViewer`；也可能收到其他错误（文件不存在、下载失败）。

| | 查看器 | `done` 的调用时机 |
| --- | --- | --- |
| iOS | 覆盖在界面上的快速查看（Quick Look，`QLPreviewController`）：可以左右滑动切换文件，支持分享、标记和搜索 | 查看器关闭时 |
| Android | 对 `Start` 指定的文件发起 `ACTION_VIEW`，在用户为该类型选择的应用（Drive 的 PDF 查看器、相册）中打开；文件通过应用的 FileProvider 传递，只授予读取这一个文件的权限 | 用户返回应用时 |
| macOS | 快速查看面板（`QLPreviewPanel`），用其中的箭头在文件之间切换 | 面板关闭时 |
| Windows | 文件的默认应用（`ShellExecuteEx`），或者“你要如何打开这个文件？”对话框 | 交给系统之后 |
| Linux | 文件的默认应用（`GtkFileLauncher`，在沙盒中由门户打开） | 交给系统之后 |
| Web | 新标签页，适用于浏览器能直接显示的内容（图片、视频、音频、文本、PDF、HTML）；其他类型返回 `ErrNoViewer` | 打开之后 |

Android 没有系统级的查看器，也没有能在多个文件之间翻页的查看器：它只打开一个文件，就像 Android 上的邮件应用逐个打开附件那样。

## 打印与 PDF：asuka/sdk/print

`asuka/sdk/print` 可以为文件、HTML 标记或 WebView 中的页面显示系统的打印对话框，其中包含可用的打印机和各种选项（份数、页码范围、纸张、方向；Android 和 Mac 上还有“存储为 PDF”）；它还能把 HTML 排版成分页的 PDF。除了 `PDF`，其他调用都在主线程上运行，对话框关闭后 `done` 会收到一个 `print.Result`：

```go
var printButton ui.Node
printButton = ui.Button(ui.ButtonProps{Title: ui.Val("Print"), OnPress: func() {
	print.HTML(receipt, print.Options{Job: "Receipt", From: printButton}, func(r print.Result, err error) {
		if err == nil && r == print.Printed {
			status.Set("Sent to the printer")
		}
	})
}})
```

- `print.File(source, options, done)` 打印 PDF 或图片（可以是路径、资源或 http(s) URL，与 `preview.Show` 接受的一样）。其他类型会以 `print.ErrUnsupported` 失败，但 Windows 和 Linux 例外（在那里由打印机决定接受什么，即 CUPS 能打印的内容）。
- `print.HTML(markup, options, done)` 打印 HTML 标记，分页方式与浏览器打印网页相同：CSS 的 `@media print` 和 `break-before` 都会生效。图片和样式要用绝对 URL（`https:`、`data:`）。
- `print.WebView(web, options, done)` 打印 `ui.WebView` 中显示的内容。

`Options` 包括：`Job`，即打印队列中的任务名（默认依次取文件名、页面标题、应用名）；`Orientation`，对话框初始的纸张方向（`print.Portrait`、`print.Landscape`）；以及 `From`，对话框以弹出框形式出现时（iPad）所指向的视图。

`Result` 可能是 `print.Printed`（任务已进入打印机队列，或已从对话框存储为 PDF）、`print.Cancelled`，或者在平台不告知结果时为 `print.Unknown`。打印任务失败会返回错误。`print.ErrUnavailable` 表示设备无法打印。

| | 对话框 | 文件 | HTML、WebView | 结果 |
| --- | --- | --- | --- | --- |
| iOS | `UIPrintInteractionController`，iPad 上以弹出框显示 | PDF 和图片（`printingItem`） | WebKit 的打印格式化器 | 已打印或已取消 |
| Android | `PrintManager` 的打印界面 | PDF 原样写入；图片缩放到页面大小并摆正方向（与 `PrintHelper` 的做法相同） | WebView 的打印适配器 | 已打印、已取消或失败（取决于任务状态） |
| macOS | `NSPrintOperation` 的面板，以 sheet 形式附着在窗口上，带预览 | PDFKit（PDF；图片放在一页上） | WebKit 的打印操作 | 已打印或已取消 |
| Windows | WebView2 提供：HTML 用系统打印对话框，WebView 用浏览器的打印预览 | 交给文件关联的应用打印（shell 的 `print` 动作），不限类型 | WebView2 | 未知 |
| Linux | GTK 的打印对话框（打印文件时用打印门户的对话框） | 直接交给打印机（CUPS：PDF、图片） | WebKitGTK 的打印操作 | 已打印或已取消 |
| Web | 浏览器的打印对话框 | 在浏览器的查看器中打印 PDF，以及图片 | 页面自己的网页视图 | 未知 |

在 Android 和 iOS 上，宽大于高的图片默认以横向开始，这与它们打印照片时的行为一致。

### 用 HTML 生成 PDF

`print.PDF(markup, options)` 把 HTML 标记排版成分页的文档，排版效果与 `print.HTML` 打印出来的相同，只是不弹出对话框。它会在应用的缓存目录中写出一个 PDF，文件名取自页面的 `<title>`（如 "Receipt.pdf"；没有标题时为 "Document.pdf"），并返回文件路径，方便你拿去分享、发邮件或移到别处。PDF 中的文字仍然是文字，可以搜索，也可以选中。不能在主线程上调用。

```go
go func() {
	path, err := print.PDF(receipt, print.PDFOptions{Page: print.A4, Margins: print.Uniform(48)})
	ui.Post(func() {
		if err == nil {
			ui.ShareFiles([]string{path}, ui.ShareOptions{Title: "Receipt"}, nil)
		}
	})
}()
```

| 选项 | |
| --- | --- |
| `Page` | 纸张大小，以点（pt）为单位：`print.A3`、`A4`、`A5`、`Letter`、`Legal`，或者自定义尺寸。默认：用户所在地区通用的纸张，美洲使用 Letter 的国家和地区为 Letter（依据 CLDR 的纸张尺寸数据），其他地方为 A4。 |
| `Landscape` | 将页面转为横向，即以较长的一边作为宽度。 |
| `Margins` | 每页内容四周的留白，以点为单位。默认：半英寸（36 点）；`print.Uniform(0)` 表示不留边距。 |

iOS 用 WebKit 排版（`UIPrintPageRenderer` 配合网页视图的打印格式化器），Android 用 WebView 的打印适配器，Mac 用 WebKit 的打印操作，Windows 用 WebView2（Chromium 的 `Page.printToPDF`），Linux 用 WebKitGTK（打印到文件）。浏览器只能通过打印对话框生成 PDF：所以在浏览器中 `PDF` 会失败，你可以改用 `print.HTML`，让用户自己选择“存储为 PDF”。

## 邮件与短信：asuka/sdk/compose

`asuka/sdk/compose` 会打开系统的邮件和短信编辑界面，预先填好内容，再交给用户发送。在主线程上调用；`done` 会收到一个 `compose.Result`：`compose.Sent`、`Saved`（存为草稿）、`Cancelled`，或者在邮件交给了其他应用、对方不告知结果时为 `Unknown`。

```go
compose.Mail(compose.MailMessage{
	To:      []string{"support@example.com"},
	Subject: "Order 1042",
	Body:    "<p>Hello,</p><p>The receipt is attached.</p>",
	HTML:    true,
	Attachments: []compose.Attachment{{Source: receiptPDF, Name: "Receipt 1042.pdf"}},
}, func(r compose.Result, err error) {
	if errors.Is(err, compose.ErrUnavailable) {
		status.Set("Set up a mail account to send it")
	}
})

compose.Message(compose.TextMessage{To: []string{"+15555550100"}, Body: "On my way"}, nil)
```

`MailMessage` 包含 `To`、`CC` 和 `BCC` 三组地址，以及 `Subject`、`Body`（`HTML` 为 true 时是 HTML 标记）和 `Attachments`。`TextMessage` 包含 `To`（电话号码或 iMessage 地址）、`Subject`（在“信息”开启了主题栏时使用，否则放在正文前面）、`Body` 和 `Attachments`。`Attachment` 包含 `Source`（路径、资源或 http(s) URL）、`Type`（默认根据扩展名推断）以及收件人看到的 `Name`（默认是文件名）。如果邮件无法承载 HTML（`mailto:` 链接、Simple MAPI），正文会改为纯文本发送，转换方式与 `compose.PlainText` 相同。

`compose.Check()` 告诉你设备能做哪些事（不能在主线程上调用）：`Mail` 表示有邮件账户或邮件应用；`MailComposer` 表示有 iOS 自带的邮件编辑界面，它能告知用户最终做了什么，也支持附件；`Text` 表示能发短信；`TextAttachments` 表示短信能带附件。`CanSendMail`、`CanSendText` 和 `CanSendAttachments` 各自只读取其中一项。设备做不到的调用会以 `compose.ErrUnavailable` 失败。

| | 邮件 | 短信 | 结果 |
| --- | --- | --- | --- |
| iOS | 覆盖在应用之上的“邮件”编辑界面（`MFMailComposeViewController`）；没有配置“邮件”账户时，用 `mailto:` 链接交给用户设置的邮件应用（不带附件） | “信息”的编辑界面（`MFMessageComposeViewController`） | 已发送、已保存、已取消；使用链接时为未知 |
| Android | 只交给邮件应用：`ACTION_SENDTO` 加 `mailto:`；带附件时用 `ACTION_SEND`，并以 `mailto:` 作为 selector，文件通过应用的 FileProvider 传递；HTML 正文既以 span 的形式放进 `EXTRA_TEXT`，也放进 `EXTRA_HTML_TEXT` | 默认短信应用：`smsto:` 加 `sms_body`；带附件时用 `ACTION_SEND` | 未知，在用户返回应用后回调 |
| macOS | 用 `mailto:` 链接交给用户的邮件应用；带附件时用邮件共享服务（`NSSharingService`），它不支持抄送和密送（设置了会以 `ErrUnavailable` 失败） | “信息”（`NSSharingService`） | 未知，或已取消 |
| Windows | `mailto:` 链接；带附件时用 Simple MAPI（`MAPISendMailW`：支持 Outlook、Thunderbird；新版 Outlook 和 Windows 的“邮件”应用不支持，会返回 `ErrUnavailable`） | `ErrUnavailable` | 未知，或已取消（MAPI） |
| Linux | 邮件门户（邮件应用的编辑界面，带上附件），其次是 `xdg-email`，最后是 `mailto:` 链接 | `ErrUnavailable` | 未知，或已取消（门户） |
| Web | `mailto:` 链接；带附件时用 Web Share 面板 | `sms:` 链接；带附件时用 Web Share 面板 | 未知 |

iOS 模拟器上既没有“邮件”账户，也没有“信息”：请在真机上试用这些编辑界面。

## 定位：asuka/sdk/location

`asuka/sdk/location` 提供设备的当前位置，以及移动过程中的位置变化。它需要用户许可，并会在第一次使用时请求。iOS 和 Mac 上用 Core Location；Android 上用系统的位置管理器，不依赖 Google Play 服务；Windows 上用 WinRT 的 Geolocator，Linux 上用定位门户（或 GeoClue），浏览器中用 Geolocation API。它默认请求“使用应用期间”的定位权限；应用不在前台时，它也能在后台持续跟踪设备位置，并替应用监控区域（相当于 expo-location 的后台定位更新和地理围栏），见下文。

`location.Current()` 返回设备当前的位置。不能在主线程上调用：它要先等用户回应，再等定位结果。如果用户没有允许，错误为 `location.ErrDenied`。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Where am I?"), OnPress: func() {
	go func() {
		p, err := location.Current()
		ui.Post(func() {
			switch {
			case errors.Is(err, location.ErrDenied):
				where.Set("Allow location in Settings")
			case err != nil:
				where.Set(err.Error())
			default:
				where.Set(fmt.Sprintf("%.4f, %.4f (within %.0f m)", p.Latitude, p.Longitude, p.Accuracy))
			}
		})
	}()
}})
```

`location.Watch(f)` 会在设备移动时，把每个新位置传给 `f`（在主线程上调用），第一个就是当前所在的位置，直到调用 `stop` 为止。它请求权限的方式与 `Current` 相同；如果用户拒绝，`f` 就不会被调用。可以让它随组件一起停止：

```go
func Track() ui.Node {
	here := signal.New(location.Position{})
	stop := location.Watch(func(p location.Position) { here.Set(p) })
	signal.OnCleanup(stop)
	return ui.Text(signal.Func(func() string {
		p := here.Get()
		return fmt.Sprintf("%.5f, %.5f", p.Latitude, p.Longitude)
	}))
}
```

`location.PlaceName(lat, lon)` 返回某个地点的名称（"Mission District, San Francisco"），由平台的地理编码器提供，而地理编码器要请求自己的服务器：所以只在用户确实需要地名时才调用。不能在主线程上调用。Windows 上会查询 OpenStreetMap 的 Nominatim，Linux 上在装了 geocode-glib 时使用它；浏览器没有地理编码器，在那里会失败。

`location.Position` 包含 `Latitude`、`Longitude`、`Accuracy`（单位为米：设备所在范围的半径）、`Altitude`（海拔，单位为米；未知时为 0）和 `Time`。

**Android**：如果最近一分钟内有过定位结果，`Current` 会直接返回它；否则最多等待 10 秒获取新的定位，仍然没有的话就返回最后一次已知的位置，或者以 `"no location"` 失败。

### 权限级别

用户可以只在使用应用期间允许定位（`WhenInUse`），也可以始终允许（`Always`）。区域监控、显著位置变化，以及应用没有运行时的事件，都需要 `Always`。用户还可以选择给出精确位置还是大致位置（误差在几公里以内：即 iOS 关闭了“精确位置”，或 Android 上选了“大致位置”）。

- `location.Request()` 在第一次调用时请求 `WhenInUse`（`Current` 和 `Watch` 也是这样请求的），返回用户作出选择后的 `location.Authorization`：其中 `Level` 是级别（`NotAsked`、`Denied`、`Restricted`、`WhenInUse`、`Always`），`Precise` 表示是否为精确位置，`Off` 表示整台设备的定位服务是否已关闭。
- `location.RequestAlways()` 请求 `Always`；如果用户还没允许 `WhenInUse`，会先请求它。iOS 只会弹出一次询问；Android 11 及以后的版本会把用户带到应用的定位设置页面，让用户选择“始终允许”。
- `location.RequestPrecise(purpose)` 向只允许了大致位置的用户请求精确位置。在 iOS 上，这只在应用离开前台之前有效（临时完全精度），`purpose` 指定向用户展示的理由，是 asuka.json 中 `ios.locationPurposes` 的一个键（其中已经有 `"default"`，内容是 "To show exactly where you are."）；在 Android 上，用户的选择会一直有效。
- `location.Authorized()` 返回当前的授权状态，不会询问用户；`location.UseAuthorization()` 把它作为 signal 提供给组件使用，`location.WatchAuthorization(f)` 则在每次变化时调用 `f`（包括用户在“设置”中的修改）。`location.OpenSettings()` 打开系统中这个应用的定位设置：一旦变成 `Denied`，就只能由用户在那里修改了。

这些调用都要等待用户回应，不能在主线程上调用。

```go
go func() {
	a, err := location.RequestAlways()
	if err == nil && a.Level != location.Always {
		ui.Post(func() { status.Set("Choose Always in Settings to be told at the shop") })
	}
}()
```

调用了 `RequestAlways` 的应用会获得 `Always` 的用途说明（可以用 `ios.usage.NSLocationAlwaysAndWhenInUseUsageDescription` 写得更贴切），以及 Android 的 `ACCESS_BACKGROUND_LOCATION`。Google Play 会在管理中心询问应用为什么需要这个权限，所以只有调用了它的应用才会获得。

### 后台定位更新

`location.StartUpdates(o)` 开始一组在应用离开前台后仍会继续的定位更新，它在应用重新启动后依然有效，直到调用 `location.StopUpdates()` 为止；`location.Updating()` 返回它的选项以及是否正在运行。`location.OnUpdates(f)` 接收这些更新：每次在主线程上收到一个 `location.Batch`，里面是一批位置。请在应用显示在屏幕上时开始更新。不能在主线程上调用。

```go
err := location.StartUpdates(location.Updates{
	Accuracy: location.AccuracyHigh, // Navigation、Balanced（100 米）、Low（1 公里）、Passive
	Distance: 20,                    // 两次更新之间相隔的米数
	Activity: location.ActivityFitness,
	Defer:    time.Minute, // 一批最多晚一分钟送达：应用被唤醒的次数更少
	Notification: location.Notification{Title: "Tracking your run"}, // Android 前台服务的通知
})
```

`Significant` 只接收显著的位置变化（约 500 米），几乎不耗电：在 iOS 上，有 `Always` 授权时，即使用户关掉了应用，这些变化也会启动它。`Interval` 只用于 Android（默认 10 秒）；`AutoPause` 和 `Indicator` 只用于 iOS（设备不动时暂停；状态栏上的蓝色指示器）。

**iOS**：调用了 `StartUpdates` 的应用会获得 `location` 后台模式。对于 `AccuracyHigh` 和 `AccuracyNavigation`，用 iOS 17 的 `CLLocationUpdate.liveUpdates` 加上一个 `CLBackgroundActivitySession`（这样 `WhenInUse` 就够用，同时会显示指示器）；其余情况用开启了 `allowsBackgroundLocationUpdates` 的 `CLLocationManager`，或它的显著位置变化服务。`Defer` 由宿主实现：iOS 自己已经不再提供批量送达。

**Android**：用 Fused Location Provider（Google Play 服务；没有 Play 服务的设备上用系统的位置管理器），结果送到一个 `PendingIntent`，所以应用没有运行时也能收到更新；`Defer` 就是它的批量送达。设置了 `Notification` 时，更新期间会运行一个定位类型的前台服务，应用不在前台时更新也能按设定的频率送达，有 `WhenInUse` 就够了。不设置的话，Android 对后台应用每小时只给几次更新，而且需要 `Always`。设备重启或应用更新会清除这些请求，之后会重新发起。

### 区域

`location.Monitor(r)` 替应用监控一个圆形区域：它的 `Enter`、`Exit`，以及（平台支持时的）`Dwell` 转换，会作为 `location.Crossing`（区域的 ID、转换类型 `Kind`、发生的地点和时间）交给 `location.OnCrossing(f)` 的处理函数；应用在后台时也会收到，在 iOS 和 Android 上应用没有运行时也会收到。如果开始监控时设备已经在区域内，会收到一个 `Enter`。区域在应用重新启动后依然有效；用同一个 ID 再次监控会修改那个区域。`location.StopMonitoring(id)`、`location.StopMonitoringAll()` 和 `location.Monitored()` 的作用如其名。在 iOS 和 Android 上需要 `Always`（否则返回 `location.ErrNeedsAlways`），在 Android 上还需要精确位置。不能在主线程上调用。

```go
err := location.Monitor(location.Region{
	ID: "shop", Latitude: 35.6812, Longitude: 139.7671, Radius: 150,
	Events: location.Enter | location.Dwell, Dwell: 5 * time.Minute,
})
```

各个系统都限制了一个应用能监控的区域数量；超过限制时，`Monitor` 返回 `location.ErrTooManyRegions`。`location.Supported()` 说明当前平台上系统能做什么：是否由系统自己送达更新、监控区域（否则会在应用运行期间根据位置计算），它的 `MaxRegions` 和 `MaxRadius`，是否会有 `Dwell`，以及事件能否启动应用。区域半径在约 100 米以上时才比较可靠。

| | 区域 | 最多 | Dwell | 应用没有运行时 |
| --- | --- | --- | --- | --- |
| iOS | `CLMonitor`（iOS 17），之前的版本用 `CLCircularRegion` | 20 | 无 | 启动应用 |
| Android | Play 服务的 `GeofencingClient`（没有 Play 服务时用位置管理器的接近提醒） | 100 | 有 | 启动应用 |
| macOS | Core Location 的区域监控 | 20 | 无 | 不会：仅在应用运行期间 |
| Windows | `Windows.Devices.Geolocation.Geofencing` 的 `GeofenceMonitor` | 没有明确限制 | 无 | 不会：仅在应用运行期间（后台任务的触发器需要打包的应用） |
| Linux | 根据位置计算（GeoClue 没有地理围栏） | 无限制 | 有 | 不会：仅在应用运行期间 |
| Web | 根据位置计算 | 无限制 | 有 | 不会：仅在页面打开期间 |

通过计算得到的区域，只有在某个位置的精度足以判断时才会改变状态：位置在半径以内、并且精度不低于半径时，判定为在区域内；连它最近的一点都在区域外时，判定为在区域外。GeoClue 根据 Wi-Fi 或 IP 地址得到的位置，对小区域来说可能太粗。在桌面平台和 Web 上，后台定位更新也用同样的方式计算（距离过滤、`Defer` 的批量送达），数据来自应用运行期间的位置：桌面应用本来就会在后台继续运行；页面则只在打开期间。

### 应用没有运行时的事件

当一个更新或区域转换到来而应用没有运行时，iOS 和 Android 会在后台、不带界面地启动应用，就像 `asuka/sdk/periodic` 那样：`main` 会运行，根组件也会挂载，然后事件交给处理函数。请在应用启动时设置 `OnUpdates` 和 `OnCrossing`，放在 `main` 中（`app.Run` 之前）或根组件中；在任何处理函数设置之前到来的事件，会等到第一个处理函数。每个事件都会保存下来，直到某个处理函数返回为止（iOS 上存在应用的 Application Support 中，Android 上存在它的 files 目录中），所以应用被结束时恰好到来的事件，会在下次启动时再交给应用。处理函数发生 panic 时会记录日志，它的事件照样算作处理完毕。

系统在后台给应用的时间很短：iOS 上约 30 秒，Android 上约 10 秒（运行着前台服务时没有限制）。`Batch.Defer()` 和 `Crossing.Defer()` 可以让应用继续运行，完成处理函数返回后仍在进行的工作，比如把位置发送到服务器，直到调用 `done` 或者 context 到期为止：

```go
func main() {
	location.OnUpdates(func(b location.Batch) {
		ctx, done := b.Defer()
		go func() {
			defer done()
			upload(ctx, b.Positions)
		}()
	})
	location.OnCrossing(func(c location.Crossing) {
		_, done := c.Defer()
		go func() { // Show 需要等待：不能在主线程上调用
			defer done()
			notify.Show(notify.Notification{Title: "Welcome back", Body: c.Region})
		}()
	})
	app.Run(App)
}
```

`Position` 还有 `Speed`（米每秒）和 `Course`（与正北方向的夹角，单位为度），未知时为负数。

在 `asuka dev` 的播放器中，播放器拥有所有权限和后台模式；播放器与应用的 Go 代码保持连接期间，事件会送到 Go 代码。要看到应用为了某个事件被启动，请单独运行应用（`asuka dev --once`，或者构建出来运行）。

### 用 uitest 测试

`host/fake` 的 `Location()` 在测试中模拟一部手机的定位服务：用户如何回应（`Answer`、`AnswerPrecise`），设备在哪里（`Move`），用户在“设置”中允许了什么（`Allow`），经过了哪些区域（`Move`，或者用 `Cross` 模拟系统报告的转换，包括 `RegionDwell`），以及还没处理完的事件（`Pending`）。在应用挂载之前排队的事件，就相当于系统为它们启动了应用。`Computed` 让它表现得像桌面平台或网页，后台部分由 Go 包自己计算。

```go
a := uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) {
	loc := h.Location()
	loc.Answer = fake.LocationAlways
	loc.Level = fake.LocationAlways
	loc.Cross("shop", fake.RegionEnter) // 系统为它启动了应用
}))
a.WaitFor("At the shop")
```

## 通知：asuka/sdk/notify

`asuka/sdk/notify` 用来显示本地通知，可以立即显示，也可以在应用设定的时间显示，到时应用是否在运行都没关系。点按通知会用它的 URL 打开应用。它还提供了消息类应用所需的各种功能，同样适用于 `asuka/sdk/push` 的通知：操作按钮和直接在通知里输入的回复、会话分组（thread）、Android 的通知渠道、对话中的消息、角标、当前显示的通知，以及应用打开时哪些通知要显示。

```go
go func() {
	ok, err := notify.Request()
	if err != nil || !ok {
		return
	}
	err = notify.Schedule(notify.Notification{
		ID:    "water",
		Title: "Water the plants",
		Body:  "It's been a day.",
		URL:   "myapp://plants",
		After: 24 * time.Hour,
	})
	if err != nil {
		fmt.Println(err)
	}
}()
```

- `notify.Request()` 第一次调用时会请求用户允许通知，并返回通知是否已被允许：可能是用户当时的回答，也可能是之后在“设置”中修改过的结果。不能在主线程上调用。
- `notify.Schedule(n)` 安排 `n` 在 `n.After` 之后显示（至少一秒）；`notify.Show(n)` 则立即显示。如果 `ID` 与某条等待中或正在显示的通知相同，就会替换掉它。不能在主线程上调用。
- `notify.Cancel(id)` 移除一条通知，无论它还在等待还是已经显示。在主线程上调用。

`Notification` 的 `URL` 应该使用应用自己的 scheme（`asuka.json` 中的 `scheme`）：用户点按通知时，`ui.OnOpenURL` 会收到这个 URL。其他字段如下：

| 字段 | 作用 |
|---|---|
| `Subtitle` | 标题下方的一行（iOS）；在 Android 上是应用名旁边的子文本（sub text） |
| `Data` | 应用自己的键值数据，会在 `Showing`、`OnAction` 和 `Present` 中原样返回 |
| `Category` | 某个类别的 ID，决定显示哪些操作按钮（见下文） |
| `Thread` | 把它和同一会话的其他通知归为一组：iOS 上是 thread，Android 上是一个通知组，组的摘要通知由 Asuka 维护 |
| `Summary` | Android 通知组摘要下方的那行文字（"3 new messages"）；iOS 会自己生成 |
| `Channel` | 它所属的 Android 渠道（见下文）；iOS 没有渠道 |
| `Badge` | 通知显示时应用图标上的数字（iOS、macOS）；在 Android 上是通知的数字 |
| `Silent` | 不发出声音 |
| `Image` | 一张图片，来源与 `ui.Image` 相同（资源、文件、http(s) URL）：在 iOS 上作为附件，在 Android 上作为大图（big picture） |

**iOS** 在应用打开时也会以横幅形式显示通知，除非 `Present` 另有指定。**Android** 用非精确闹钟（inexact alarm）发出定时通知（因此不需要精确闹钟权限），所以通知可能会比设定的时间稍晚一点出现。

### 操作与回复

类别（category）代表一种通知以及它的按钮。应用的类别要在启动时、在根组件中设置好，并且要赶在引用这些类别的通知到来之前（推送也一样：iOS 上是 `aps.category`，Android 上是 data 中的 `"category"`）。类别设置在应用多次启动之间会一直保留。

```go
notify.SetCategories(notify.Category{ID: "message", Actions: []notify.Action{
	{ID: "reply", Title: "Reply", Icon: "arrowshape.turn.up.left",
		Reply: &notify.Reply{Button: "Send", Placeholder: "Message"}},
	{ID: "read", Title: "Mark as Read"},
	{ID: "delete", Title: "Delete", Destructive: true, Unlock: true},
	{ID: "open", Title: "Open", Open: true},
}})

notify.OnAction(func(r notify.Response) {
	switch r.Action {
	case "": // 点按了通知本身
		openChat(r.Notification.Thread)
	case "reply":
		_, done := r.Defer() // 发送需要一点时间
		go func() {
			defer done()
			api.Send(r.Notification.Thread, r.Reply)
		}()
	case "read":
		notify.RemoveThread(r.Notification.Thread)
	}
})
```

- `Action` 包含 `ID` 和 `Title`，以及 `Icon`（一个 SF Symbol；iOS 15 和 macOS 12 会显示它，Android 显示操作时不带图标）、`Destructive`（iOS 会把它显示成红色）、`Unlock`（先解锁设备：iOS，以及 Android 12 及更高版本）、`Open`（打开应用）和 `Reply`（一个文本框，可以设置发送按钮的标题和占位文字）。
- `notify.OnAction(f)` 会把每个响应传给 `f`（在主线程上调用），直到调用 `stop` 为止。响应可能是点按通知本身（`Action` 为 `""`），也可能是某个操作；`Reply` 是用户输入的文本，`Notification` 是被响应的那条通知（也包括 `asuka/sdk/push` 的通知）。在设置任何 `f` 之前就到达的响应（启动或唤醒应用的那一个）会等到第一个 `f` 设置好再传过去，所以请在应用启动时就设置它。
- 没有设置 `Open` 的操作会在后台运行：应用的 Go 代码会执行（如果应用没在运行，会在没有界面的情况下启动它：先执行 `main`，再执行根组件，根组件中必须调用 `OnAction`），但应用不会切换到前台。如果 `f` 返回后还有工作要做，就在 `f` 中调用 `r.Defer()`，并在工作完成时调用 `done`：系统会让应用一直运行到那时，或者运行到 `ctx` 的截止时间为止。这个截止时间比系统自己的时限稍早一些（iOS 约 30 秒，Android 约 10 秒）。

**Android** 会像 iOS 一样，在后台操作结束后移除对应的通知，除非应用在处理完成前再次显示了它（调用 `Show`，并使用同一个 `ID`）。用户回复之后，Android 要求重新发布这条通知（在此之前，通知上的加载动画会一直转）：对于对话类通知，Asuka 会以用户的名字把回复追加进去；其他通知则直接移除。

### 会话分组与当前显示的通知

`Thread` 把一个聊天的通知归为一组：iOS 会把它们叠在一起，Android 会把它们放进一个带摘要通知的组里，并由 Asuka 随着通知的增减来维护这个摘要。

- `notify.Showing()` 按从新到旧的顺序，列出应用当前显示着的通知（在通知中心或通知栏中），包括推送来的通知：每个 `Delivered` 包含 `ID`、`Title`、`Subtitle`、`Body`、`URL`、`Data`、`Category`、`Thread`、`Time`，推送通知还有 `Remote`。不能在主线程上调用。
- `notify.Remove(ids...)`、`notify.RemoveThread(thread)`（适用于已读的聊天）和 `notify.RemoveAll()` 用来移除通知。在主线程上调用。

### 应用打开时显示哪些通知

```go
notify.Present(func(n notify.Delivered) notify.Presentation {
	if n.Thread == openChat.Peek() {
		return notify.Hide // 这个聊天正显示在屏幕上
	}
	return notify.Banner
})
```

`notify.Present(f)` 让 `f` 决定应用在前台时收到的每条通知该如何显示：`Banner`（和应用关闭时一样）、`Quiet`（只进入通知中心或通知栏，没有横幅也没有声音），或者 `Hide`。`f` 会在通知到达时于主线程上运行，并且必须立即给出结果：宿主最多只等一秒，超时就直接显示。传入 `nil` 则恢复为全部显示。在主线程上调用。

### 对话中的消息

消息会以平台展示消息的方式显示，并带上发送者的头像：在 iOS 上是通讯通知（communication notification），在 Android 上是对话（显示在通知栏的“对话”区域，并附带该对话的快捷方式）。

```go
ana := notify.Person{ID: "u7", Name: "Ana", Image: "https://example.com/ana.jpg"}
chat := notify.Conversation{ID: "climbing", Title: "Climbing", Group: true}
go notify.Show(notify.Notification{Body: "Who's in for Saturday?", Category: "message"}.From(ana, chat))
```

- `n.From(sender, c)` 把 `n` 变成一条消息：`sender` 在 `c` 中说了 `n.Body`。它的 `ID` 和 `Thread` 默认为对话的 `ID`（一对一对话的 ID 就是发送者的 ID），`Title` 默认为发送者的名字（群组则为群组的标题）。
- `Person` 包含 `ID`（每次都要相同）、`Name`、`Image`（来源与 `ui.Image` 相同）和 `Bot`。`Conversation` 包含 `ID`、群组的 `Title` 和 `Image`、`Group`、`Me`（用户本人在对话中显示的身份；Android 用它来标注用户自己的回复，默认为 "You"），以及 `Earlier`（之前的消息）。
- **Android** 把一个对话显示为一条通知，所有消息都在其中：新消息会追加到当前显示着的那条对话通知里（或者由 `Earlier` 设定它前面的内容）。**iOS** 则把每条消息显示为一条独立的通知，归入该对话的分组。
- **iOS** 需要在 App ID 上开启 Communication Notifications capability：调用了 `From` 的应用会自动获得相应的 entitlement 以及 Info.plist 中的 `NSUserActivityTypes`（`INSendMessageIntent`），发布构建的描述文件也必须包含这项 capability。否则消息会以普通通知的形式显示。**macOS** 则总是把消息显示为普通通知。

### Android 的通知渠道

渠道（channel）代表一类通知，用户可以在应用的设置中关闭某个渠道或把它设为静音。Android 按照通知所在渠道的声音、振动和重要程度来显示它；iOS 没有渠道，会忽略这些设置。

```go
notify.SetChannelGroups(notify.ChannelGroup{ID: "chats", Name: "Chats"})
notify.SetChannels(
	notify.Channel{ID: "messages", Name: "Messages", Importance: notify.ImportanceHigh, Group: "chats"},
	notify.Channel{ID: "digest", Name: "Daily digest", Importance: notify.ImportanceLow, NoBadge: true},
)
```

- `notify.SetChannels(cs...)` 会创建渠道，为之前创建过的渠道更新名称，并删除之前由它创建、但这次不在 `cs` 中的渠道。渠道的 `Importance`（`ImportanceDefault`；`ImportanceHigh`：悬浮通知；`ImportanceLow`：没有声音；`ImportanceMin`）、`Silent`、`NoVibration` 和 `NoBadge` 只在创建时生效，之后就交由用户来修改。`notify.SetChannelGroups(gs...)` 对渠道组做同样的事（删除一个组会连同删除其中的渠道）。在主线程上调用。
- 没有指定 `Channel`、或者指定的渠道不存在的通知，会归入默认渠道（"Notifications"），这个渠道始终保留。推送通过 FCM 的 `android.notification.channel_id` 指定渠道，数据消息则通过 `"channel"` 指定。

### 角标

`notify.SetBadge(n)` 设置应用图标上的数字；传入 `0` 则去掉角标。**iOS** 和 **macOS**（显示在程序坞上）会照你设定的数字显示。**Android** 没有自己的角标：支持角标的启动器会统计应用的通知数量（或者只显示一个圆点），所以通知被移除后（`Remove`、`RemoveThread`）角标会随之减少，而 `SetBadge` 什么也不做；通知的 `Badge` 是启动器可能会显示的数字。设置了 `NoBadge` 的渠道中的通知不计入角标。

### 各平台的情况

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| 显示、定时、取消 | ✓ | ✓ | ✓ | ✓（toast） | ✓（定时：用 systemd 用户定时器，无论应用是否运行） | ✓（定时：仅在应用有页面打开时） |
| 操作、回复 | ✓ | ✓ | ✓ | ✓（最多五个按钮） | 支持操作（回复会打开应用） | 浏览器显示按钮时可用（Chrome、Edge：两个）；支持内联回复的浏览器可以回复 |
| 后台操作 | ✓（约 25 秒） | ✓（约 9 秒） | ✓ | ✓（约 25 秒；会为此启动应用） | 仅在应用运行时 | 在应用的页面中运行（由 worker 打开一个页面） |
| 会话分组 | ✓ | ✓（一个组及其摘要） | ✓ | 不分组；支持 `RemoveThread` | ✓ | 不分组；支持 `RemoveThread` |
| 渠道 | 忽略 | ✓ | 忽略 | 忽略 | 忽略 | 忽略 |
| 对话 | ✓（通讯通知） | ✓（MessagingStyle、快捷方式） | 普通通知 | 普通通知 | 普通通知 | 普通通知，以发送者头像作为图标 |
| 角标 | ✓ | 由启动器决定 | ✓（程序坞） | 任务栏按钮上的叠加图标 | 启动器 API 提供的角标（Dash to Dock、Plasma） | 已安装应用的角标（`navigator.setAppBadge`） |
| 列出、移除 | ✓ | ✓ | ✓ | ✓（通知中心中的） | 仅限应用本次运行中显示过的 | ✓（service worker 显示的） |
| Present | ✓ | ✓ | ✓ | ✓（定时通知除外） | ✓ | ✓ |

**macOS** 需要应用的 bundle，而 `asuka dev` 和 `asuka build` 都是从 bundle 启动应用的：单独运行的可执行文件（`go run`）没有 bundle ID，这些调用会失败，并返回说明原因的错误。**Windows** 显示 toast 通知，可以立即显示，也可以定时显示，支持图片，类别中的操作显示为按钮，回复显示为文本框。应用第一次使用通知时会注册自己：在开始菜单中创建一个快捷方式，带上应用的 AppUserModelID（即 asuka.json 中的 `id`），并在用户的注册表中登记一个 COM 激活器（也就是应用的可执行文件）。这样，点按通知或按钮时才能把响应送到应用；如果应用没在运行，Windows 会替它启动。如果是后台操作，应用会保持不可见，并在 `OnAction` 处理完后退出。角标是画在应用任务栏按钮上的一个数字（打包应用则使用系统的角标）。无论 `Present` 怎么设定，Windows 都会显示定时的 toast。**Linux** 上，如果应用有 desktop 文件（或运行在沙盒中），通知会以 GNotification 的形式发送，否则直接发给桌面的通知服务器（GNOME Shell 会拒绝没有 desktop 文件的应用）；应用运行期间，点按通知和按钮都能传到应用。定时通知是一个 systemd 用户定时器（`systemd-run --user`）：到时它会以无窗口的方式运行应用的可执行文件来显示通知，如果应用正在运行就交给应用处理，用户点按时则启动应用；没有 systemd 时（Flatpak、`asuka dev`），定时通知只在应用运行时显示。**浏览器**通过应用的 service worker 显示通知，`asuka/sdk/push` 则通过 Web Push 实现：详见 [Web](web.md)。

## 播放声音：asuka/sdk/audio

`asuka/sdk/audio` 用平台自带的播放器播放音效和音乐：iOS 和 Mac 上是 AVPlayer，Android 上是 Media3 的 ExoPlayer，Windows 上是 MCI，Linux 上是 GStreamer 的 playbin，浏览器里则是一个 audio 元素（浏览器要求用户先在页面上点按或点击过，才允许播放）。音源可以是应用 `assets/` 里的文件、应用自己写入的文件，也可以是一个 http(s) URL，指向文件或流都行。

`audio.Play(source)` 从头播放一次声音，播完就自动释放，适合提示音、按键音这类场景。需要更多控制时，用 `audio.Load` 创建一个 `*audio.Player`：

```go
func Sounds() ui.Node {
	music := audio.Load("music/theme.m4a")
	music.SetLoop(true)
	music.SetVolume(0.5)
	signal.OnCleanup(music.Release)
	return ui.Column(nil,
		ui.Button(ui.ButtonProps{Title: ui.Val("Ding"), OnPress: func() { audio.Play("sounds/ding.wav") }}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Play"), OnPress: music.Play}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Pause"), OnPress: music.Pause}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Restart"), OnPress: func() { music.Seek(0); music.Play() }}),
	)
}
```

`Player` 的方法都要在主线程上调用：`Play` 从当前位置开始播放（如果已经播完，则从头开始）；`Pause` 暂停；`Seek(seconds)` 跳到指定的秒数；`SetVolume` 设置音量，范围 0 到 1，默认为 1；`SetRate` 设置播放速度，范围 0.25 到 4，音调保持不变；`SetLoop` 设置是否循环；`Status(done)` 给出播放状态，包括 `Position`、`Duration`、`Playing` 和 `Ended`，想跟踪播放进度的话，每秒读几次即可；`Release` 停止播放，并释放它占用的资源。播放器不再需要时，记得释放它。

和音乐类、游戏类应用一样，即使打开了 iOS 的静音开关，声音也会照常播放，而且不会和其他应用的音频混在一起播放。

### 正在播放与媒体键：asuka/sdk/nowplaying

`asuka/sdk/nowplaying` 把应用正在播放的内容显示在系统展示媒体信息的地方，并把那里的控制按钮发出的命令转交给应用。这些地方包括：iOS 上的锁屏、控制中心、灵动岛和 CarPlay；Android 上的通知栏、锁屏、Android Auto 和 Wear OS；Mac 上的控制中心和菜单栏；Windows 上的媒体浮出控件；Linux 上 GNOME 和 KDE 的媒体控件；以及浏览器自己的媒体控件。键盘和耳机上的媒体键发出的也是同样的命令。`Show` 在组件中调用，会跟随传入的访问器更新，并在组件的 owner 被销毁时随之消失：

```go
func Episode(ep *Episode) ui.Node {
	state := signal.New(nowplaying.Paused)
	position := signal.New(0.0) // 单位为秒：播放位置跳变时设置
	nowplaying.Show(nowplaying.Props{
		Title:          ui.Val(ep.Title),
		Artist:         ui.Val(ep.Show),
		Artwork:        ui.Val(ep.Cover), // 资源文件、本地文件或 http(s) URL
		Duration:       ui.Val(ep.Seconds),
		State:          state,
		Position:       position,
		OnPlay:         func() { player.Play(); state.Set(nowplaying.Playing) },
		OnPause:        func() { player.Pause(); state.Set(nowplaying.Paused) },
		OnSeek:         func(at float64) { player.Seek(at); position.Set(at) },
		OnSkipForward:  func(s float64) { skip(s) },
		OnSkipBackward: func(s float64) { skip(-s) },
		SkipInterval:   30,
	})
	// ...
}
```

会显示出来的有：`Title`、`Artist`、`Album`、`Artwork`（边长 512 像素以上的正方形图片在哪里都清晰）、`Duration`（直播流设为 0，此时无法拖动进度）、`State`（`Playing`、`Paused` 或 `Stopped`）、`Position` 和 `Rate`。播放期间，系统会按播放速度自行推进进度，因此只有在播放位置发生跳变（拖动进度、卡顿）或者状态、速度改变时，这个包才会重新发送 `Position`。也就是说，即便 `Position` 跟随的是一个每秒读取好几次的播放器状态，也不会带来额外开销。

设置了处理函数的命令才会启用，其余的会变灰或者干脆不显示。`OnPlay`、`OnPause` 和 `OnTogglePlayPause` 可以互相替代（耳机上只有一个键，按下就切换播放和暂停；锁屏上则只显示播放或暂停其中一个按钮），所以只要设置了任意一个，三者就都会启用。`OnSeek` 要求 `Duration` 已知。`OnSkipForward` 和 `OnSkipBackward` 按 `SkipInterval` 快进或快退（默认 15 秒）；iOS 和 Mac 会用它们替换“下一首”和“上一首”按钮，Android 则在没有设置 `OnNext` 或 `OnPrevious` 时，把它们放到这两个按钮的位置上。

如果用的是 `asuka/sdk/audio` 的播放器，把它交给 `Player`，剩下的事就都不用管了：状态、位置和时长都从播放器获取（每秒读一次它的状态；对它执行的命令会立即反映出来），而播放、暂停、停止、跳转以及快进快退（需设置 `SkipInterval`），只要没有设置对应的处理函数，就会直接作用在这个播放器上：

```go
music := audio.Load("music/spring.mp3")
signal.OnCleanup(music.Release)
nowplaying.Show(nowplaying.Props{
	Title:   ui.Val("Spring"),
	Artist:  ui.Val("Vivaldi"),
	Artwork: ui.Val("covers/spring.jpg"),
	Player:  music,
	OnNext:  playNext,
})
```

可以同时挂载多个：显示的是最后挂载的那个，它消失后，再显示它前面的那个。如果在同一次更新中，一个被另一个替换（比如当前曲目的组件让位给下一首的组件），系统的媒体控件在切换过程中会始终归这个应用所有，不会中途丢失。

在 iOS 上，应用会获得 `audio` 后台模式，因此锁屏后仍能继续播放；它的音频会话使用 playback 类别，而 iOS 显示的正是音频会话正在播放的那个应用。在 Android 上，它是一个带有媒体样式通知的 MediaSession（媒体会话的通知不需要通知权限）；播放期间，一个媒体播放类型的前台服务会让应用在后台保持运行，这是 Android 14 的要求；暂停后通知仍然保留，用户可以把它划掉。这两项配置都只会由 `asuka build` 添加到用到这个包的应用里。`examples/player` 就用它来播放一张专辑。

## 网络状态：asuka/sdk/network

`asuka/sdk/network` 反映设备的网络状况：是否联网，走的是 Wi-Fi 还是蜂窝网络，以及连接是否计费。你可以用它实现“仅在 Wi-Fi 下下载大文件”，或者提示用户应用当前处于离线状态。它在 iOS 和 Mac 上基于 NWPathMonitor，在 Android 上基于 ConnectivityManager，在 Windows 上基于 Network List Manager，在 Linux 上基于 GNetworkMonitor 和 NetworkManager，在浏览器中基于 `navigator.onLine`；浏览器只有在能拿到信息的地方（Android 上的 Chrome）才会告诉你连接类型。

`network.Use()` 以 signal 的形式提供网络状态，供组件使用：只要组件还在，它就会跟随网络的变化。

```go
func Status() ui.Node {
	net := network.Use()
	return ui.Text(signal.Func(func() string {
		n := net.Get()
		if !n.Connected {
			return "Offline"
		}
		if n.Metered || n.Constrained {
			return n.Type.String() + ": large downloads wait for Wi-Fi"
		}
		return n.Type.String()
	}))
}
```

`network.State` 包含：

- `Connected`：是否已联网。
- `Type`：`network.None`、`WiFi`、`Cellular`、`Ethernet` 或 `Other`（只有 VPN、蓝牙等）。它的 `String` 分别是 `"none"`、`"wifi"`、`"cellular"`、`"ethernet"` 和 `"other"`。
- `Metered`：连接会产生费用或有流量限制，比如移动数据或手机热点。
- `Constrained`：用户要求应用少用流量（iOS 的低数据模式、Android 的流量节省程序、浏览器的省流量模式）；在 Windows 上，也可能是连接接近或超出了流量上限。在 Linux 上永远为 false。

在平台第一次给出结果之前，`Use` 的值是已连接（`Connected`），`Type` 为 `Other`。

`network.Current()` 返回当前的网络状态（不要在主线程上调用）。`network.Watch(f)` 会先在主线程上用当前的网络状态调用一次 `f`，之后每次变化都再调用一次，直到调用 `stop` 为止。

## 保存到媒体库：asuka/sdk/library

`asuka/sdk/library` 把文件保存到用户和其他应用都能找到的地方：视频存进相册，歌曲存进音乐，任何文件都可以存进“下载”。

```go
library.Save(file, library.Options{Collection: library.Movies, Folder: "My App", Move: true},
	func(where string, err error) {
		if err != nil {
			status.Set("Couldn't save: " + err.Error())
			return
		}
		status.Set("Saved")
	})
```

`library.Save(path, o, done)` 在主线程之外把文件复制到 `o.Collection`（大视频可能要花一些时间），完成后在主线程上调用 `done`，告诉你文件存到了哪里。第一次保存时，它会请求所需的权限。`done` 可以为 nil。

`library.Options`：

- `Collection`：`library.Downloads`（默认）、`Movies`、`Music` 或 `Pictures`。
- `Name`：文件保存后的名字（默认沿用原名）。如果重名，会自动加上编号。
- `Folder`：在该集合中专门存放应用文件的文件夹；留空则直接放进集合本身。
- `MIME`：文件类型（默认根据扩展名判断）。
- `Move`：保存成功后删除原文件。适合下载到应用缓存里的文件，免得多留一份副本。

各集合对应的位置：

| 集合 | Android | iOS | macOS | Windows、Linux |
| --- | --- | --- | --- | --- |
| `Downloads` | “下载”（Downloads） | 应用的 Documents，在“文件”App 中可见 | `~/Downloads` | 用户的“下载”文件夹 |
| `Movies` | 相册中的视频 | “照片” | “照片” | 用户的“视频”文件夹 |
| `Music` | “音乐”（Music） | 应用的 Documents，在“文件”App 中可见 | `~/Music` | 用户的“音乐”文件夹 |
| `Pictures` | 相册中的图片 | “照片” | “照片” | 用户的“图片”文件夹 |

在 Windows 和 Linux 上，以及在 Mac 的 `~/Downloads` 和 `~/Music` 中，文件会放在用户对应文件夹下的 `Folder` 里，回调拿到的是文件路径；Linux 上的这些文件夹遵循 XDG 规范。浏览器则不管是哪个集合，都会直接下载文件，文件会出现在浏览器存放下载内容的位置。

**Android** 从 Android 10 开始使用 MediaStore 的集合，不需要任何权限，回调拿到的是一个 `content://` URI。在 Android 10 之前，它会先请求存储权限，再写入共享文件夹。

**iOS** 会请求向“照片”添加内容的权限（仅限添加：应用无法读取照片图库），回调拿到的是该资源的标识符；`Folder` 在这里不起作用。其他集合会保存到应用 Documents 下的 `Folder` 中，回调拿到的是文件路径；Asuka 会让 Documents 在“文件”App 中可见。

## 后台任务：asuka/sdk/background

`asuka/sdk/background` 让应用在不可见时继续工作，比如用户发起的、要花上几分钟的下载、上传或导出。

`background.Start` 启动一个任务：在调用 `End` 之前，应用会一直运行，并显示一条展示其 `Options` 的通知。用户点按通知上的按钮时，`onAction` 会在主线程上收到这个按钮的 ID。`task.Update` 用来显示新的选项，比如随着工作推进更新进度。请在应用处于前台、用户请求这项工作时启动任务。

```go
var cancelled atomic.Bool
task := background.Start(background.Options{
	Title:    "Exporting",
	Progress: -1,
	Actions:  []background.Action{{ID: "cancel", Title: "Cancel"}},
}, func(action string) {
	if action == "cancel" {
		cancelled.Store(true)
	}
})
go func() {
	for i := 1; i <= 10 && !cancelled.Load(); i++ {
		time.Sleep(time.Second) // 实际要做的工作
		p := float64(i) / 10
		ui.Post(func() {
			task.Update(background.Options{Title: ui.Val("Exporting"), Text: fmt.Sprintf("%.0f%%", p*100), Progress: p,
				Actions: []background.Action{{ID: "cancel", Title: "Cancel"}}})
		})
	}
	ui.Post(func() {
		task.End()
		if cancelled.Load() {
			status.Set("Cancelled")
		}
	})
}()
```

`background.Options`：

- `Title` 和 `Text`。
- `Progress`：0 到 1；负数表示正在进行，但不知道进度。
- `Actions`：通知上的按钮，每个按钮由 `ID` 和 `Title` 组成。

`Start`、`Update` 和 `End` 都在主线程上调用，实际的工作放到 goroutine 里去做。每次 `Update` 都会替换全部选项，所以别忘了把 `Actions` 也重新传一遍。[`examples/downloader`](../../examples/downloader) 在任务中执行下载，并支持暂停、继续和取消。

**Android** 会在几分钟内停掉转入后台的应用。任务会让应用在一个前台服务（数据同步类型）中继续运行，每个任务对应一条常驻通知，显示进度和最多三个按钮，同时保持 CPU 唤醒。在 Android 13 及以上版本，如果用户还没有允许通知，启动任务时会请求通知权限；不管用户是否允许，工作都会继续。Android 不允许处于后台的应用启动任务。Android 15 规定数据同步服务每天最多运行六小时。Google Play 会在 Play Console 中询问应用的数据同步前台服务是做什么用的：上传之前请先完成声明。

**iOS** 会在任务存续期间，给转入后台的应用多留大约 30 秒，并且不显示通知：`Update` 在 iOS 上什么也不做，`onAction` 也永远不会被调用。时间用完后，iOS 会结束任务，工作可能因此中断。

**桌面端**的应用在不可见时本来就会继续运行：任务的作用是防止系统降低它的速度或把它挂起（Mac 上的 App Nap，Windows 和 Linux 上的睡眠），同样不显示通知：`Update` 什么也不做，`onAction` 也永远不会被调用。**浏览器**中，只要页面开着就会一直运行，有没有任务都一样。

## 接收其他应用的分享：ui.OnShare

当其他应用通过分享面板把内容分享给这个应用时，`ui.OnShare(f)` 会把这些内容交给 `f`：比如来自 YouTube 或浏览器的链接、备忘录里的一段文字、相册里的照片、“文件”里的 PDF。这是 iOS 和 Android 才有的功能：在桌面端和浏览器中，应用不会出现在其他应用的分享菜单里，`f` 也不会被调用。`asuka.json` 的 `shareTarget` 决定应用可以接收哪些内容：

```json
{ "shareTarget": true }
```

这样写表示接收文本和链接。写成对象时还能接收文件，按 MIME 类型指定，一次最多 `max` 个（默认 1 个）：

```json
{ "shareTarget": { "types": ["text", "image/*", "video/*"], "max": 10 } }
```

类型可以是 `"text"`（文本和网页链接），也可以是 MIME 类型：`"image/*"`、`"video/*"`、`"audio/*"`、`"application/pdf"`，`"*/*"` 表示任意文件。

```go
func App() ui.Node {
	link := signal.New("")
	photos := signal.New([]string(nil))
	ui.OnShare(func(items []ui.SharedItem) {
		for _, it := range items {
			switch it.Kind {
			case ui.SharedLink:
				link.Set(it.Text)
			case ui.SharedImage:
				photos.Set(append(photos.Peek(), it.Path))
			}
		}
	})
	return ui.Text(link)
}
```

每个 `ui.SharedItem` 都有一个 `Kind`：`SharedText`、`SharedLink`、`SharedImage`、`SharedVideo`、`SharedAudio` 或 `SharedFile`。文本或链接放在 `Text` 里。文件则位于 `Path`，那是应用缓存中属于应用自己的一份副本：要么把它移到应用存放数据的地方，要么用完后删掉。文件还带有 `Name`（它在来源应用中的名字，如 `IMG_0042.HEIC`）、`MIME` 类型和 `Size`（未知时为 -1）。各项的顺序与来源应用给出的顺序一致。从浏览器分享的网页会变成两项：标题（一条文本）和链接。

在设置任何处理函数之前就到达的分享（比如启动应用的那一次）会交给第一个处理函数。处理函数会在组件的 owner 销毁时停止。如果想为分享的内容显示应用的某个页面，又不打开应用本身，请看下面的分享面板；如果要在应用里打开某个页面，请看[导航](navigation.md)。

**Android** 会根据所列类型生成的 intent filter，带着分享的内容直接打开应用本身（`ACTION_SEND`；`max` 大于 1 时还有 `ACTION_SEND_MULTIPLE`）。在 `OnShare` 拿到文件之前，应用会趁着访问权限还在，先把文件复制到自己的缓存中。

**iOS** 上，应用里包含一个分享扩展，它会像 Android 那样带着分享的内容打开应用，自己不显示任何界面。扩展的激活规则由所列类型生成：文本、链接、图片、视频和任意文件使用 Apple 提供的键，`max` 依然有效；其他类型（音频、PDF）则对附件类型做谓词判断，这时 `max` 不起作用。扩展会接收传给它的任何内容，所以请检查 `Kind` 和 `MIME`。文件通过 App Group（`group.<app ID>`）从扩展传给应用，真机构建所用的描述文件必须包含这个 App Group（见[发布](shipping.md#ios)）。

在 `asuka dev` 下，播放器应用会接收任何内容的分享，不管应用的 `shareTarget` 怎么设置。在 Android 上，文件路径指向的是设备上的位置，而不是运行 Go 代码的那台 Mac：要测试文件分享，请用 `asuka dev --platform android --once`。

链接指向的是什么（网页、图片还是视频），需要应用自己用 `net/http` 去弄清楚，相当于用 Go 实现 expo-sharing 的 “resolved” 负载。

### 应用自己的分享面板：app.ShareSheet

默认情况下，分享会打开应用。`app.ShareSheet(screen)` 则改为在发起分享的应用上方显示应用自己的一个页面，就像“保存到 Raindrop”或“添加到备忘录”那样：用户保存后，就回到原来所在的地方。请在 `app.Run` 之前调用它。

```go
func ShareSheet(s *ui.ShareSheet) ui.Node {
	note := signal.New("")
	return ui.Column(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Text(signal.Static(s.Items[0].Text)),
		ui.TextField(ui.TextFieldProps{Value: note, Placeholder: ui.Val("Note"), Multiline: true}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), OnPress: s.Cancel}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
				save(s.Items, note.Peek())
				s.Close()
			}}),
		),
	)
}

func main() {
	app.ShareSheet(ShareSheet)
	app.Run(App)
}
```

`s.Items` 是分享过来的内容，和 `OnShare` 收到的一样。面板有三种结束方式：`s.Close()`（完成）、`s.Cancel()`，以及 `s.OpenApp()`——它会像没有面板时那样，带着这些内容打开应用，交给应用的 `OnShare`。在内容全部到达之前（比如视频还在复制），面板会显示一个加载指示器。

面板是一个独立的根，运行在独立的进程中，这与 iOS 运行分享扩展的方式一致。在 **iOS** 上，它就是应用的分享扩展，由 iOS 以弹出面板的形式显示，运行的是应用的 Go 代码和宿主，二者都来自扩展与应用共用的那个 framework；在 **Android** 上，它是运行在 `:share` 进程中的一个 Activity，以 Material 底部面板的形式显示在另一个应用上方（向下拖动、点按面板外部或按返回键都会取消）。进程随面板一起结束：`asuka/sdk/store` 中尚未写入的内容会在关闭时写入，`app.OnLifecycle` 会收到 `Background`。文件的 `Path` 只在面板关闭前有效：应用要保留的内容，请先复制出来。

应用保存的数据在面板里同样可用：`asuka/sdk/store`、`store.Dir()` 中的 SQLite 文件（`sqlite.Open("app.db")`）以及 `asuka/sdk/secure`。在 Android 上，两个进程共享应用的文件。在 iOS 上，应用第一次带着面板运行时，会把数据一次性迁移到 App Group（`group.<app ID>`），它的钥匙串组也会与扩展共享。应用在运行期间不会得知面板写入了什么：等它回到前台时再检查一遍（`app.OnLifecycle`、`db.Changed()`）。

CLI 发现应用的 Go 代码调用了 `app.ShareSheet` 时，就会构建这个面板，而不是那个只负责打开应用的扩展。此时在 iOS 上，应用的 Go 代码和宿主会打包成应用里的一个 framework，即 `Frameworks/AsukaApp.framework`，由应用和扩展共同加载；Xcode 在应用和扩展之间共享代码也是这样做的，所以加上面板并不会让应用变大。在 `asuka dev` 下，播放器应用的分享面板运行的同样是应用自己的面板，代码也在 Mac 上执行。[`examples/bookmarks`](../../examples/bookmarks) 就是一个类似 Raindrop 的面板：显示网页的标题、描述和图片，还可以添加备注、选择收藏夹和标签。

## 运行 JavaScript：asuka/sdk/js

`asuka/sdk/js` 在屏幕之外、用平台自带的引擎运行 JavaScript：在一个从不显示的网页视图里（iOS 和 Mac 上是 WKWebView，Android 上是 WebView，Windows 上是 WebView2，Linux 上是 WebKitGTK），在浏览器中则是一个隐藏的沙箱 iframe。它适用于那些只有 JavaScript 版本的代码，比如某个网站的播放器里用来给视频 URL 签名的函数。

`js.Eval(ctx, script)` 在一个全新的独立页面中运行脚本（语句的执行方式与 `<script>` 中相同），并以 JSON 返回它的值：也就是最后一条语句的值；如果脚本返回一个 promise，则是 promise 解析出的结果（`undefined` 会变成 `null`）。抛出的错误或被 reject 的 promise 会作为错误返回，类型为 `*js.Error`。脚本运行完，页面也就销毁了：一个脚本设置的东西，下一个脚本看不到。不要在主线程上调用。

```go
go func() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := js.Eval(ctx, `const xs = [1, 2, 3].map(x => x * 2); xs`)
	fmt.Println(string(v), err) // [2,4,6] <nil>

	s, err := js.EvalString(ctx, `new Promise(done => setTimeout(() => done("later"), 100))`)
	fmt.Println(s, err) // later <nil>
}()
```

如果脚本的值是字符串，可以直接用 `js.EvalString`，它就是为这种情况准备的 `Eval`。脚本最多可以运行到 `ctx` 的截止时间；如果 `ctx` 没有截止时间，则最多运行 `js.DefaultTimeout`（一分钟）。超时的脚本会以 `"timeout"` 失败。在 Android 上，抛出的错误的消息里会带有调用栈。

如果要在用户能看到的页面里运行脚本，请使用 `ui.WebView`（见[网页](components.md#网页)）。

## 推送通知：asuka/sdk/push

`push.Register()` 会（在第一次调用时）请求用户允许通知，向 Apple（APNs）或 Google（Firebase Cloud Messaging）的推送服务注册设备，然后返回设备的 `Token`、所用的 `Service`（`"apns"`、`"fcm"`），以及通知是否已获允许（`Allowed`）。把这个令牌发给你的服务器，由服务器向它发送通知；令牌可能会变，所以每次启动都要注册一次。不要在主线程上调用。Mac 和 iOS 一样通过 APNs 注册（需要描述文件，见[在 Mac 上](#在-mac-上)）。在 Web 上用的是 Web Push（服务为 `"webpush"`，令牌就是浏览器的推送订阅，见 [Web](web.md)）。Windows 和 Linux 没有可供应用使用的推送服务：`Register` 在这两个平台上会失败。

```go
func App() ui.Node {
	push.OnOpen(func(m push.Message) { openThread(m.Data["thread"]) })
	go func() {
		if r, err := push.Register(); err == nil {
			api.SaveDeviceToken(r.Service, r.Token)
		}
	}()
	return home()
}
```

- `push.OnReceive(f)` 接收应用运行期间到达的每一条消息。如果消息带有标题或正文，它也会以通知的形式显示出来，应用在前台时也是如此。数据消息（静默推送）只会送到这里。
- `push.OnOpen(f)` 接收用户点按的每一条通知；只要在应用启动时就设置好 `f`，启动应用的那条通知也会包括在内。和 `asuka/sdk/notify` 的通知一样，消息数据中的 `"url"` 也会交给 `ui.OnOpenURL`。
- `push.Message` 包含 `ID`、`Title`、`Body` 和 `Data`（iOS 上是负载中 `aps` 以外的键；Android 上是消息的 data）。

iOS 的发布构建需要在 App ID 上开启 Push Notifications 功能；在模拟器上也能拿到令牌。Android 需要一个 Firebase 项目：把 Android 应用添加进去，下载它的 `google-services.json`，然后在 `asuka.json` 中指定这个文件：

```json
{ "android": { "googleServices": "google-services.json" } }
```

Asuka 会像 Google 的 Gradle 插件那样，把其中的配置写进应用。缺少这个文件时，`Register` 会失败，并告诉你该怎么做。

### 推送中的类别、分组和渠道

推送可以使用 `asuka/sdk/notify` 的类别（及其操作和 `notify.OnAction`）、分组、渠道、角标和 `Present`，只需在负载中指定：

- **iOS**：`aps` 中的 `category`、`thread-id` 和 `badge`，与 APNs 的定义一致。
- **Android**：通知消息中的 `android.notification.channel_id` 和 `image`；数据消息中的 `"title"`、`"body"`、`"subtitle"`、`"category"`、`"thread"`、`"summary"`、`"channel"`、`"image"`、`"badge"` 和 `"url"`。带有 `"title"` 或 `"body"` 的数据消息会显示为通知（由 Asuka 构建，包括其中的操作）；两者都没有的则是静默消息（只交给 `OnReceive`）。应用在后台时收到的通知消息由 Firebase 显示，既没有操作也不会分组：需要这些功能时，请改发数据消息。

`push.Message` 也包含这些字段：`Subtitle`、`Category`、`Thread`、`Channel`、`Image`、`Badge`、`Summary`。

### 在推送显示之前改写它

`push.Rewrite(f)` 让应用的 Go 代码在每条推送显示之前修改它：解密端到端加密的负载，设置标题和正文，附上图片，把它变成一条带有发送者头像的会话消息，设置分组和类别。请在 `main` 中、`app.Run` 之前调用它。

```go
func main() {
	push.Rewrite(func(m *push.Message) error {
		text, from, err := decrypt(m.Data["box"]) // 用存放在 asuka/sdk/secure 中的密钥解密
		if err != nil {
			return err // 推送按原样显示
		}
		m.Body = text
		m.Category = "message"
		m.From(notify.Person{ID: from.ID, Name: from.Name, Image: from.Avatar},
			notify.Conversation{ID: m.Data["chat"]})
		return nil
	})
	app.Run(App)
}
```

`f` 在单独的 goroutine 中运行，不在主线程上。返回错误或发生 panic 时，推送会按原样显示。`m.Image` 会被下载下来，作为附件显示；`m.From` 把推送变成一条消息，作用和 `notify.Notification` 的 `From` 相同。

**iOS** 在 Notification Service Extension 中改写推送，`asuka build` 会为调用了 `push.Rewrite` 的应用生成这个扩展：对于 `aps` 中带有 `"mutable-content": 1`（以及 `alert`，改写失败时显示的就是它）的每条推送，扩展都会在独立的进程中运行应用的 Go 代码。先运行 `main`，再运行 `f`；根组件不会运行，所以 `main` 要尽量轻量。iOS 给扩展大约 30 秒时间和 24 MB 内存，超时后会按原样显示推送。应用保存的数据在扩展里同样可用：数据（`asuka/sdk/store`、`store.Dir` 中的文件）存放在两者共享的 App Group 中，钥匙串项（`asuka/sdk/secure`）存放在应用的钥匙串组中。发布构建需要为扩展的 bundle ID（应用的 ID 加上 `.notification`）准备一个描述文件（可以用 `asuka.json` 的 `ios.notificationProfile` 指定；默认使用已安装的描述文件中最匹配的那个），并开启 App Groups 功能、加入应用的组。在 iOS 上，改写无法把推送隐藏起来。

**Android** 在应用的进程中改写 FCM 的数据消息：如果应用没有在运行，Go 代码会在没有界面的情况下启动，在大约十秒内完成改写，然后显示通知。如果改写后的数据消息既没有标题也没有正文，就不会显示。

在 `asuka dev` 的播放器应用中没有扩展（应用的 Go 代码运行在电脑上）：推送会按原样显示。用 `asuka dev --platform ios --once` 或正式构建运行时则有扩展。模拟器只会对来自 APNs 的推送运行扩展，`xcrun simctl push` 发出的推送不会触发它。

## 通过 Apple 登录：asuka/sdk/appleauth

`appleauth.Button` 是 Apple 官方的登录按钮（标题、标志和配色都是 Apple 的，并使用用户的语言），`appleauth.SignIn` 会弹出系统的登录面板。如果应用的账号支持用其他公司的账号登录，App Store 就要求它同时提供“通过 Apple 登录”。

```go
appleauth.Button(appleauth.ButtonProps{
	Type:  appleauth.ContinueButton,
	Style: appleauth.Automatic, // 浅色模式下为黑色，深色模式下为白色
	OnPress: func() {
		go func() {
			c, err := appleauth.SignIn(appleauth.Options{Scopes: appleauth.FullName | appleauth.Email, Nonce: nonce})
			if err == nil {
				api.SignInWithApple(c.IdentityToken, c.AuthorizationCode)
			}
		}()
	},
})
```

`Credential` 的 `User` 是账号的稳定 ID；把 `IdentityToken`（由 Apple 签名的 JWT）和 `AuthorizationCode` 发给你的服务器，由服务器向 Apple 验证。`Email`、`GivenName` 和 `FamilyName` 只在用户第一次登录这个应用时提供：务必在那时保存下来。`ErrCanceled` 表示用户关闭了登录面板。`appleauth.CredentialState(user)` 告诉你用户是否仍在这个应用上使用“通过 Apple 登录”（如果是 `Revoked`，就让用户退出登录）。Android 上没有这项功能：`Button` 什么也不显示，`Available` 为 false，`SignIn` 会以 `ErrNotAvailable` 失败；Windows、Linux 和 Web 上也是如此。在这些平台上，可以通过网页完成“通过 Apple 登录”（`asuka/sdk/webauth`）。Mac 和 iOS 一样支持这项功能，但需要描述文件（见[在 Mac 上](#在-mac-上)）。

## 面容 ID 与指纹：asuka/sdk/biometric

`biometric.Authenticate(options)` 显示系统的身份验证提示（Face ID、Touch ID、Android 的 BiometricPrompt；Mac 上是 Touch ID，Windows 上是 Windows Hello，浏览器中是通行密钥的 WebAuthn 提示）。如果确认是设备的主人，就返回 nil；否则返回 `ErrCanceled`、`ErrNotAvailable`（既没有生物识别也没有设置设备密码，或者已被锁定）或 `ErrFailed`。不要在主线程上调用。

```go
go func() {
	err := biometric.Authenticate(biometric.Options{Reason: "Unlock your notes"})
	ui.Post(func() { unlocked.Set(err == nil) })
}()
```

- `Reason` 说明为什么要验证（在 iOS 上显示为副标题，在 Android 上显示为标题）；`Cancel` 是取消按钮的标题。
- 用户也可以改用设备的密码、PIN 或图案，除非设置了 `NoPasscode`，即只允许生物识别（在 Android 上还要求是强生物识别，也就是 Class 3）。
- `biometric.Check()` 告诉你设备具备哪些能力：它的 `Kind`（`Face`、`Fingerprint`、`Iris`、`Optic`）、是否已录入（`Enrolled`），以及是否设置了密码（`Passcode`）。

Linux 没有用来验证设备主人的系统提示：`Check` 会报告什么都没有，`Authenticate` 会以 `ErrNotAvailable` 失败。

## 录音：asuka/sdk/audio/record

`record.Start(options, done)` 从麦克风录音，直到调用 `record.Stop`（或达到 `MaxDuration`），然后把录好的文件交给 `done`：文件格式是 MPEG-4 容器中的 AAC（`.m4a`），存放在应用的缓存中，可以直接用 `asuka/sdk/audio` 播放。录音在 iOS 和 Mac 上用的是 AVAudioRecorder，在 Windows 上是 Media Foundation，在 Linux 上是 GStreamer，在浏览器中是 `MediaRecorder`（无法录制 AAC 时改用 WebM 封装的 Opus）。第一次录音时会请求麦克风权限；如果被拒绝，`done` 会收到 `"not allowed"`。

```go
record.Start(record.Options{Quality: record.Voice, MaxDuration: time.Minute}, func(path string, err error) {
	if err == nil {
		memo.Set(path)
	}
})
// 稍后
record.Stop()
```

`Voice` 是单声道、64 kbit/s；`Music` 是立体声、128 kbit/s。`Pause` 和 `Resume` 在同一个文件上暂停和继续录制。在 goroutine 中每秒调用几次 `record.Level()`，可以拿到 0 到 1 之间的音量电平，用来做音量表。在主线程上调用 `record.CurrentStatus(done)`，可以拿到电平、当前是否真的在录入声音，以及已经录了多长时间：计时器应该显示的就是这个时长，因为系统可能会暂停录音（比如来电），而 iOS 会在系统允许时自动继续。在 iOS 上，录音期间音频会话会切换为同时播放和录制（其他应用的声音会停下），录完后再恢复原样。

`Options.Stream` 录制的是 ADTS 流（`.aac`），它的每一帧都是完整的，所以即使应用崩溃或手机突然关机，已经录下的内容也不会丢；而 MPEG-4 文件在关闭之前是无法读取的。给它指定一个 `Path`，崩溃之后就能再找到这个文件；然后用 `record.Finalize(stream, dst)` 把它转成适合长期保存的 `.m4a`，转换后的文件可以正常拖动进度。无论录音是正常停止还是因崩溃中断，都可以这样处理。

`Options.Background` 让录音在应用不可见、屏幕锁定时继续进行。iOS 需要在 `asuka.json` 中加上 `"ios": {"backgroundModes": ["audio"]}`；Android 会运行一个麦克风类型的前台服务，并显示一条常驻通知（`BackgroundTitle`），上面带有停止按钮。Google Play 会询问这个服务是做什么用的：上传之前请先完成声明。桌面应用在不可见时本来就会继续录音，所以 `Background` 在桌面端不起作用。

## 语音转文字：asuka/sdk/transcribe

`transcribe.File(path, options)` 在设备上把录音转成文字，声音不会离开设备。如果设备做不到，它会返回 `ErrNotAvailable`，而不会转而求助服务器。不要在主线程上调用。

```go
go func() {
	r, err := transcribe.File(path, transcribe.Options{Language: "en-US"})
	ui.Post(func() {
		if err == nil {
			text.Set(r.Text)
		}
	})
}()
```

`Result` 包含 `Text`、`Language` 和 `Segments`。`Segments` 是逐个单词的列表，每个单词带有 `Start` 和 `Duration`（单位为秒），只在 iOS 和 Android 14 上提供。没有人声的录音会得到空的 `Text`。`transcribe.Check(language)` 告诉你设备能做什么，不会向用户请求任何权限。iOS 使用 Speech 框架（模型内置于 iOS 中，但并非每个模拟器运行时都有）；第一次使用时会请求语音识别权限。Android 的设备端识别器需要 Android 13，还需要对应语言的模型，缺少时会自动下载（此时返回 `ErrDownloading`：请稍后再试）。Mac 同样使用 Speech 框架，并且只在设备上识别。Windows、Linux 和 Web 没有可供应用使用的设备端识别器：`File` 会以 `ErrNotAvailable` 失败。

## 屏幕：asuka/sdk/screen

```go
screen.Lock(screen.Landscape) // 视频播放器
screen.SetBars(screen.Bars{Style: screen.Light, HideStatus: true})
screen.PreventCapture(true) // 票券上的二维码
stop := screen.OnScreenshot(func() { note.Set("That screenshot shows your balance") })
```

- `screen.Lock(o)` 把应用转到方向 `o` 并保持不变：`Portrait`、`PortraitUp`、`Landscape`、`LandscapeLeft`、`LandscapeRight`；`Any` 表示解除锁定。只能使用 `asuka.json` 允许的方向。处于分屏浏览（Split View）或台前调度（Stage Manager）中的 iPad，以及 Android 16 的大屏设备，会忽略这个设置。
- `screen.SetBars(bars)` 设置状态栏的 `Style`（`Auto` 跟随深色模式；`Light` 是浅色图标，适合下方是深色画面的情况；`Dark` 是深色图标），还可以隐藏状态栏（`HideStatus`）或 Android 的导航栏（`HideNavigation`；从屏幕边缘滑动会让它们短暂出现）。安全区域会随之更新。
- `screen.PreventCapture(on)` 让应用不出现在截图和录屏中。Android 会把窗口设为安全窗口：截图和录屏都是黑屏，最近任务里也显示为空白。iOS 无法阻止截图：它只能在屏幕被录制、镜像或通过 AirPlay 投放时把应用遮住。
- `screen.OnScreenshot(f)` 在用户截屏时调用 `f`（iOS；Android 14 及以上），直到调用 `stop`。

桌面窗口既没有方向，也没有系统栏：`Lock` 和 `SetBars` 在桌面端什么也不做。在 Mac 和 Windows 上（Linux 不行），`PreventCapture` 会让应用的窗口不出现在截图和录屏中；`OnScreenshot` 会在截图保存时得到通知（Mac 上借助聚焦搜索，Windows 和 Linux 上则通过“屏幕截图”文件夹）。浏览器只有在全屏或作为已安装的应用运行时才能锁定方向，靠进入全屏来隐藏系统栏，而且既不支持 `PreventCapture`，也不支持 `OnScreenshot`。

这些设置作用于整个应用，而不是某个页面：页面如果改了其中某项，离开时要把它恢复原样（`signal.OnCleanup`）。

## 启动屏：asuka/sdk/splash

`asuka.json` 的 `splash` 就是启动屏：一张图片（正方形 PNG，比如 logo），宽 `size` 点（默认 120，最大 160），居中显示在纯色背景上，浅色和深色模式可以分别设置。

```json
{ "splash": { "image": "logo.png", "size": 96, "background": "#4A63B5", "darkBackground": "#1B2440" } }
```

它从用户点按图标开始显示，一直到应用的第一帧出现为止。在 iOS 上，先显示系统的启动屏，然后在应用上方覆盖一份一模一样的副本，直到第一帧准备好；在 Android 12 及以上是系统的启动画面（图片位于图标的圆形区域中央），更早的版本则用窗口背景来显示。桌面端和 Web 没有启动屏：`Hold` 和 `Hide` 在这些平台上什么也不做。如果想在应用加载期间一直显示启动屏，就在启动时（在根组件中）调用 `splash.Hold()`，加载完成后再调用 `splash.Hide()`：

```go
func App() ui.Node {
	splash.Hold()
	go func() {
		load()
		ui.Post(splash.Hide)
	}()
	return home()
}
```

## 地图：asuka/sdk/maps

`maps.View` 是一个地图视图：iOS 和 Mac 上是 Apple 地图，Android 上是 Google 地图，Windows 和浏览器中是 OpenStreetMap 的瓦片（由宿主绘制），Linux 上则使用 libshumate。

```go
region := signal.New(maps.Region{Center: maps.Coordinate{Lat: 37.77, Lng: -122.42}, LatDelta: 0.05, LngDelta: 0.05})
maps.View(maps.Props{
	Region:         region,
	Markers:        ui.Val([]maps.Marker{{ID: "hq", At: maps.Coordinate{Lat: 37.79, Lng: -122.39}, Title: "HQ"}}),
	OnRegionChange: func(r maps.Region) { region.Set(r) },
	OnMarkerPress:  func(id string) { show(id) },
	Style:          ui.Style{Grow: 1},
})
```

- `Region` 是地图显示的区域，由中心点和横跨的经纬度数组成；设置它会让地图移过去。用户移动地图之后，`OnRegionChange` 会收到新的区域。
- `Markers` 是地图上的标记，可以带 `Title`、`Subtitle` 和 `Color`；`OnMarkerPress` 收到被点按标记的 `ID`，`OnPress` 则收到用户在地图上点按的位置。
- `Type` 可以是 `Standard`、`Satellite`、`Hybrid` 或 `Muted`（iOS）；`ShowsUser` 显示用户的位置，需要定位权限（先导入 `asuka/sdk/location` 并请求权限）；`NoInteraction` 让地图变成一张静态预览。
- 地图本身没有尺寸：要么给它设置尺寸，要么用 `Grow`。

Android 需要一个启用了 Maps SDK for Android 的 Google Maps Platform API 密钥，写在 `asuka.json` 中：`{"android": {"mapsKey": "..."}}`。没有密钥时，地图上会显示相应提示，`OnError` 也会收到原因。在 Windows、Linux 和 Web 上，`Satellite` 和 `Hybrid` 显示的都是街道图（没有免费的卫星影像）；`ShowsUser` 在 Windows 和 Linux 上不显示任何内容，在浏览器中，只要页面获得了定位权限，就会显示用户的位置。在 iOS 和 Web 上，`Muted` 显示的是更素净的街道图。Linux 需要安装 libshumate（`libshumate-1.0-1`）。瓦片按照 OpenStreetMap 使用政策的要求从 `tile.openstreetmap.org` 获取，并在地图上标注其署名；Web 应用也可以从自己的服务器获取瓦片（见 [Web](web.md)）。

## 后台刷新：asuka/sdk/periodic

`asuka/sdk/periodic` 会在应用不在前台时，时不时运行一下应用的 Go 代码，比如同步数据、拉取新内容。`Handle` 在根组件中设置任务要执行的代码；`Schedule` 请求系统去运行它。

```go
func App() ui.Node {
	periodic.Handle("sync", func(ctx context.Context) error {
		return syncInbox(ctx)
	})
	periodic.Schedule("sync", periodic.Options{Every: time.Hour, Network: true})
	return home()
}
```

什么时候运行由系统决定，依据是 `Every`（至少 15 分钟）、电量、网络以及用户使用应用的习惯；实际间隔可能比设定的长得多，用户也可以关掉这项功能（iOS 的“后台 App 刷新”）。设置 `Network` 后只在联网时运行，设置 `Charging` 后只在充电时运行。`ctx` 会在系统规定的截止时间之前结束：iOS 上大约 25 秒（设置了 `Charging` 时属于处理任务，可以有几分钟），Android 上为 9 分钟（WorkManager）。返回错误时，Android 会稍后重试。

当系统在应用关闭的情况下运行任务时，应用的 Go 代码会在没有界面的情况下启动：先运行 `main`，再运行根组件（它会调用 `Handle`），然后执行任务。如果这时用户打开了应用，界面会正常出现。`Cancel(name)` 用来停止一个任务。

桌面系统不会为了执行任务而启动应用：在 macOS（`NSBackgroundActivityScheduler`）、Windows 和 Linux 上，任务只在应用运行期间按定时器执行，需要耗电的任务在使用电池时会推迟。浏览器在页面关闭后不会运行任何任务：`Schedule` 在浏览器中会失败。

## 应用商店服务：asuka/sdk/review、asuka/sdk/tracking、asuka/sdk/integrity、asuka/sdk/agerange

`review.Request(done)` 请求应用商店弹出评分提示（App Store 的评分提示、Google Play 的应用内评价）。是否显示由商店按照自己的配额决定，而且不会告诉你结果；请在用户顺利完成某件事之后再请求，不要通过按钮触发。如果想做一个“给我们评分”按钮，请用 `ui.OpenURL(review.AppStoreURL(id))` 或 `review.PlayStoreURL(id)`。

`tracking.Request()` 显示 iOS 的 App 跟踪透明度（App Tracking Transparency）提示，应用在读取广告标识符或与广告网络共享数据之前必须先发起这个请求；它返回 `Allowed`、`Denied`、`Restricted` 或 `NotAsked`。`tracking.Current()` 只读取当前状态，不会发起请求。Android 没有这样的提示：结果始终是 `Allowed`。不要在主线程上调用。

`asuka/sdk/integrity` 向你的服务器证明：请求确实来自运行在真实设备上的你的应用。iOS 上用的是 App Attest：`GenerateKey` 生成一个密钥（保存好它的 ID），`AttestKey(id, challenge)` 让 Apple 针对服务器给出的 challenge 为这个密钥出具证明（只需一次），之后每个请求都用 `Assert(id, data)` 签名。Android 上用的是 Play Integrity：启动时调用 `Prepare(cloudProjectNumber)`，之后每个请求调用 `Token(requestHash)`。服务器再拿这些返回结果去 Apple 或 Google 验证。在 iOS 模拟器上，`Supported` 为 false。

`agerange.Request(13, 18)` 在不获取出生日期的前提下询问用户的年龄段，以满足那些要求应用识别未成年用户的法律：它基于 Apple 的 Declared Age Range（iOS 26）和 Play 的 Age Signals。平台可能会请用户本人或其家长同意分享。`Result` 可能是 `Shared`（附带年龄下限 `Lower` 和上限 `Upper`，0 表示不设限）、`Declined`，或者 `VerificationRequired`（Play）；它的 `Source` 说明年龄是怎么得知的。平台无法回答时返回 `ErrNotAvailable`。

手机以外的平台：Mac 支持 App Store 的评分提示、跟踪提示和 App Attest（需要描述文件，见[在 Mac 上](#在-mac-上)），但不支持 Declared Age Range。Windows、Linux 和 Web 没有商店的评分提示（`review.Request` 不显示任何内容；在浏览器中 `done` 会收到错误），没有设备认证（`integrity.Supported` 为 false），也无法获取年龄段（`ErrNotAvailable`）。在这些平台上，跟踪也不需要弹出提示：Windows 在“设置”中开启了广告 ID 时返回 `Allowed`；Linux 返回 `Allowed`；浏览器在发送了全局隐私控制（Global Privacy Control）或 Do Not Track 时返回 `Denied`。

应用内购买 `asuka/sdk/purchase` 有单独的一页：[应用内购买](purchases.md)。

## 运动与传感器：asuka/sdk/sensors

```go
stop := sensors.WatchMotion(50*time.Millisecond, func(m sensors.MotionReading) {
	level.Set(m.Attitude.Roll) // 水平仪
})
```

- `WatchAccelerometer`、`WatchGyroscope` 和 `WatchMagnetometer` 大约每隔给定的间隔给出一个 `Vector`（x、y、z）：加速度的单位是 g，旋转的单位是弧度每秒，磁场的单位是微特斯拉（已校准）。
- `WatchMotion` 给出融合后的设备运动数据：`Gravity` 和用户施加的 `Acceleration`（单位为 g）、`Rotation`，以及 `Attitude`。`Attitude` 包括一个把设备坐标轴转换到东-北-天（East-North-Up）坐标系的 `Matrix`、`Heading`（相对磁北的角度；没有磁力计时为 -1），以及 `Pitch` 和 `Roll`。
- `WatchPressure` 对应气压计（单位为百帕，同时给出海拔变化），`WatchLight` 给出环境光照度，单位为勒克斯（Android），`WatchSteps` 给出开始监听以来的步数。`StepsBetween(from, to)` 读取 iOS 的历史记录（最近一周）；Android 不保留历史。
- `Available(sensor)` 告诉你设备有没有某个传感器；iOS 模拟器和 Android 模拟器上的传感器都很少。Mac 和 PC 没有这些传感器；Linux 在二合一设备和平板上有光线传感器，以及由加速度计得出的屏幕方向（iio-sensor-proxy）；浏览器有 Generic Sensor API 或设备运动事件，但没有气压计和计步器。

两个平台给出的数值是一致的：设备坐标轴采用 Android 和 W3C 的定义（x 指向屏幕右侧，y 指向屏幕上方，z 垂直于屏幕向外）；加速度是加速度计实际测得的值（设备屏幕朝上平放时，读数为 z = +1 g；iOS 的数据会转换成同样的方向）；姿态在两个平台上的计算方式也相同。每秒最多 200 个读数；读数在主线程上送达，按平台交付的节奏分批到来。

## 编辑图片和视频帧：asuka/sdk/imageedit

`imageedit.Edit(source, actions, options)` 读取一张图片（HEIC、JPEG、PNG、WebP，也就是相机和照片选择器给出的那些格式），根据它的方向信息把图片摆正，依次执行各项操作，然后把结果以 JPEG 或 PNG 格式写入应用的缓存。不要在主线程上调用。

```go
go func() {
	r, err := imageedit.Edit(photo, []imageedit.Action{
		imageedit.Resize(1080, 0), // 宽 1080；0 表示保持宽高比
		imageedit.Crop(0, 0, 1080, 1080),
	}, imageedit.Options{Quality: 0.8})
	if err == nil {
		upload(r.Path, r.Width, r.Height)
	}
}()
```

可用的操作有 `Resize`、`Crop`、`Rotate`（顺时针，任意角度）、`FlipHorizontal`、`FlipVertical`、`Fill` 和 `Pixelate`。`Fill(x, y, w, h, r, g, b)` 把一个矩形区域涂成纯色，原来的内容会从文件中彻底抹掉；`Pixelate(x, y, w, h, block)` 给一个区域打上马赛克，每个方块取其覆盖像素的平均值，方块与整张图片对齐，所以相邻的矩形看起来是连成一片的。方块较小时，底下的文字有时还能被辨认出来：必须隐藏的内容请用 `Fill`。写出的文件不带原图的任何元数据：没有位置，也没有相机信息。如果第一个操作是 `Resize`，解码时就会直接解成接近目标的尺寸，处理大照片时能省下不少内存。`imageedit.Size` 无需解码就能读出图片的尺寸。`imageedit.VideoFrame(source, at, actions, options)` 截取视频在某个时间点的画面（用作缩略图或封面），摆正后用同样的方式编辑；如果是远程视频，只会读取截取这一帧所需的那部分。source 和 `ui.Image` 接受的一样：路径、`file://` 或 `http(s)` URL，或者资源文件。

各平台使用自己的编解码器解码：iOS 和 Mac 上是 ImageIO，Android 上用系统自带的，Windows 上是 WIC，Linux 上是 GdkPixbuf 的加载器（装了相应的编解码器或加载器时才支持 HEIC 和 WebP），浏览器中则是 canvas（输出 JPEG 或 PNG；远程图片需要 CORS）。

## 文字转语音：asuka/sdk/speech

`speech.Speak(text, options, done)` 把一段文字加入朗读队列，用平台的语音朗读出来；读完后 `done` 收到 nil，被停止则收到 `ErrStopped`。

```go
speech.Speak("Turn left in 200 meters", speech.Options{Rate: 1.1}, nil)
```

`Options` 可以设置 `Language`（BCP 47 标签）、`Voice`（来自 `speech.Voices()` 的 ID，`Voices()` 会列出每种语音的名称、语言和质量）、`Pitch`、`Rate` 和 `Volume`。`Stop` 清空队列，`Pause` 和 `Resume` 在单词处暂停和继续（Android 本身不支持暂停，但这里同样可用：Asuka 会从暂停的那个单词接着读）。在 iOS 上，朗读时会把其他应用的声音调低，静音开关打开时也照常朗读；Android 同样会调低其他应用的声音。所用的语音在 iOS 和 Mac 上来自 AVSpeechSynthesizer，在 Windows 上来自 SAPI，在 Linux 上来自 Speech Dispatcher，在浏览器中来自 `speechSynthesis`。

## 通讯录：asuka/sdk/contacts

- `contacts.Pick(done)` 显示系统的联系人选择器：用户选中一位联系人，应用只能看到这一位，不需要请求任何权限（在 Android 上，没有权限时只能拿到姓名）。
- `contacts.Add(contact, done)` 显示系统的新建联系人界面，并预先填好信息，由用户来保存；不需要请求权限。
- `contacts.Request()` 请求访问权限，之后 `contacts.List(query)` 可以读取通讯录（按姓名查找，有数量上限），包括姓名、单位、带标签的电话号码和邮箱，以及生日。在 iOS 18 的有限访问模式下，只会列出用户选择共享的联系人。

在 Mac 上用的是 Contacts 框架：`Pick` 是系统的选择器，以弹出框的形式从窗口顶部弹出；`Add` 则在窗口上方弹出一个警告框征求用户同意（macOS 没有新建联系人界面），然后再保存。Linux 读写 Evolution Data Server 的通讯录，详见[在 Linux 上](#在-linux-上)。Windows 只向打包应用开放联系人：`Request` 会报告被拒绝，`Pick` 会失败，`Add` 会在用户的联系人应用中打开一个 vCard。浏览器在支持 Contact Picker 的地方（Android 上的 Chrome）可以选择联系人，`Add` 会下载一个 vCard。

## 日历：asuka/sdk/calendar

- `calendar.Add(event, done)` 显示系统的事件编辑器，并预先填好信息，由用户来保存事件；不需要请求权限（iOS 17 及以上）。iOS 会告诉你事件是否已保存，Android 的日历应用则不会。
- `calendar.Request()` 请求完全访问权限；之后 `calendar.Calendars()` 列出所有日历，`calendar.Events(from, to, ids...)` 列出两个时间点之间的事件，重复事件的每一次都会单独列出。
- 全天事件的 `Start` 和 `End` 以天为单位：分别是第一天的 UTC 零点，以及最后一天的次日 UTC 零点。

在 Mac 上用的是 EventKit：`Add` 在窗口上方弹出一个警告框征求用户同意（macOS 没有供应用使用的事件编辑器），然后再保存。Linux 读写 Evolution Data Server 的日历。Windows 只向打包应用开放日历：`Request` 会报告被拒绝，`Add` 会在用户的日历应用中打开一个 iCalendar 文件（`saved` 为 false，因为它无从得知是否保存了）。浏览器无法读取日历；`Add` 会下载一个 `.ics` 文件。

## 主屏幕小组件：asuka/sdk/widget

`asuka.json` 的 `widgets` 声明应用有哪些小组件；`widget.Set` 则在应用的 Go 代码里为它们填充内容：

```json
{ "widgets": [{ "kind": "today", "name": "Today", "description": "What's next", "sizes": ["small", "medium", "rectangular"] }] }
```

```go
widget.Set("today", widget.Entry{
	Title: "Next", Symbol: "calendar", Value: "3 tasks", Body: "Call Ana at 4",
	Progress: 0.4, ShowProgress: true, Until: meeting, URL: "myapp://today",
})
```

小组件会用平台的模板显示它的 `Entry`，并按尺寸排版：`Title`（带上 `Symbol`，以 `Tint` 着色）、大号的 `Value`、`Body`、`Progress` 进度条、到 `Until` 为止的实时倒计时，以及 `Caption`。在应用设置新的内容之前，它会一直显示当前内容，不管应用是否在运行；点按小组件会用它的 `URL` 打开应用（`ui.OnOpenURL`）。多个条目各自带上 `At`，就构成了一条时间线：每个条目从各自的时间开始显示。`Clear` 用来清空小组件。

尺寸有 `small`、`medium`、`large`，以及 iOS 的 `extraLarge`（iPad）和锁屏上的 `circular`、`rectangular`、`inline`。iOS：CLI 会把一个 WidgetKit 扩展构建进应用，并让它与应用共享一个 App Group（发布构建需要开启 App Groups 功能，还需要为扩展的 ID，即应用的 ID 加上 `.widgets`，准备一个描述文件）。Android：每种小组件对应一个 App Widget，使用 Material You 配色，尺寸按启动器的网格单元计算。桌面端和 Web 没有小组件：`widget.Set` 在这些平台上什么也不做。

## 实时活动：asuka/sdk/liveactivity

实时活动在应用之外显示正在发生的事，并随着事情的进展更新：一份正在配送的外卖、一趟行程、一场比赛的比分、一个计时器。在 iOS 上，它是一个实时活动（Live Activity，iOS 16.2 起）：显示在锁屏和灵动岛上，没有灵动岛的 iPhone 上则以横幅显示。在 Android 上，它是一条常驻通知，Android 16 会把它提升为实时更新（Live Update）：显示在通知栏和锁屏的顶部，状态栏里还有一个胶囊标签。

```go
a, err := liveactivity.Start(liveactivity.Content{
	Title: "Order #1042", Subtitle: "Ana is picking it up", Value: "12 min",
	Symbol: "bag.fill", Until: eta, URL: "myapp://orders/1042",
}, liveactivity.Options{})

// 稍后，在主线程上：
a.Update(liveactivity.Content{Title: "Order #1042", Subtitle: "2 stops away", Value: "4 min", Until: eta},
	liveactivity.Alert{Title: "Almost there", Body: "Ana is 2 stops away"})
a.End(liveactivity.Content{Title: "Order #1042", Subtitle: "Delivered"}, liveactivity.Dismiss{})
```

实时活动用平台的模板显示它的 `Content`，空着的字段不显示：`Title` 和 `Subtitle`、`Value`（大号显示在一侧；在灵动岛的紧凑视图里显示；在 Android 16 上是状态栏里的胶囊标签）、以 `Tint` 着色的 `Symbol`、`Progress` 进度条，以及一个到 `Until` 为止的倒计时，它不需要应用运行也会走。同时设置 `Since`、`Until` 和 `ShowProgress` 时，进度条会自己往前走。点按实时活动会用它的 `URL` 打开应用（`ui.OnOpenURL`）。

`Start` 要在应用显示在屏幕上时调用（两个平台都不允许后台的应用启动实时活动），而且不能在主线程上调用；`Update` 和 `End` 则要在主线程上调用。带上 `Alert` 的更新会点亮屏幕：iOS 会展开灵动岛，并在 Apple Watch 上显示它；Android 会让通知再提醒一次。`End` 之后，最后的内容会一直显示，直到系统把它移走（最多 4 小时），或者到 `Dismiss.At` 为止；`Dismiss.Now` 则立即移走。`Running` 列出仍在运行的实时活动以及它们各自显示的内容（比如应用重新启动之后）；`Enabled` 表示用户是否允许实时活动（Android 会在第一次 `Start` 时请求通知权限）。

`Observe` 会告诉你实时活动发生了什么：某个活动 `Ended`（被用户、系统、服务器或 `End` 结束）、某个活动被服务器 `Started`，以及推送令牌。设置了 `Options.Push` 后，服务器可以在应用不在前台时更新和结束实时活动：把它的 `PushToken` 和 `ID` 发给服务器即可。在 iOS 上，服务器发送一条 `liveactivity` 类型的 APNs 推送，其中的 `content-state` 就是 JSON 形式的 `Content`（在 Go 里用它的 `MarshalJSON` 生成）；在 Android 上，服务器发送一条 Firebase 数据消息，带上 `asuka.liveActivity`（`update` 或 `end`）、`asuka.id` 和 `asuka.content`。服务器也可以启动实时活动：在 iOS 17.2 上用 `PushToStartToken`（attributes 类型为 `AsukaActivityAttributes`，带上新活动的 `id`），在 Android 上把 `asuka.liveActivity` 设为 `start`。具体的消息格式见这个包的文档。推送需要应用同时使用 `asuka/sdk/push`：iOS 上需要推送通知功能，Android 上需要 Firebase。

对于调用了这个包的应用，iOS 构建会加入小组件扩展，其中带有实时活动的模板（和小组件一样用 SwiftUI 编写），并设置 `NSSupportsLiveActivities`；和小组件一样，发布构建需要为扩展的 ID 准备一个描述文件。用户可以在“设置”里为应用关闭实时活动，也可以在 Android 16 的通知设置里关闭提升显示。桌面端和 Web 没有实时活动：`Enabled` 为 false，`Start` 以 `ErrNotAvailable` 失败。

## 快捷方式：Siri、聚焦搜索与启动器

`asuka.json` 的 `shortcuts` 列出系统为应用提供的快捷操作。每个快捷方式都会用它的 URL 打开应用，交给 `ui.OnOpenURL` 处理：

```json
{ "shortcuts": [{ "id": "newNote", "title": "New Note", "url": "notes://new", "symbol": "square.and.pencil", "phrases": ["New note in ${app}"] }] }
```

在 iOS 上，它们是 App Shortcuts：Siri 能听懂它们的短语（每个短语都要包含代表应用名称的 `${app}`），它们还会出现在聚焦搜索、“快捷指令”App 和操作按钮中，用户无需做任何设置（最多十个）。CLI 会把它们编译成 App Intents，并像 Xcode 那样提取它们的元数据。在 Android 上，它们是启动器快捷方式，长按应用图标时出现，图标使用强调色。桌面端和 Web 不会添加它们。

模拟器构建会像 Xcode 那样签名，使用钥匙串中的 Apple Development 身份（属于 `asuka.json` 中指定的团队）：App Intents 只能在带有团队的应用中运行。如果没有团队，构建会采用临时（ad hoc）签名，CLI 会提示快捷方式将无法运行。

## 触感反馈：ui.Haptic

`ui.Haptic(kind)` 播放触感反馈（iOS 上是 UIFeedbackGenerator，Android 上是 `performHapticFeedback`）：`ui.HapticSelection`、`HapticLight`、`HapticMedium`、`HapticHeavy`、`HapticSuccess`、`HapticWarning` 或 `HapticError`。Mac 会通过触控板给出反馈（需要 Force Touch 触控板：`HapticSelection` 对应对齐反馈，其余对应层级变化反馈）；浏览器在支持的地方会振动（Android 上的 Chrome）；Windows 和 Linux 没有触感反馈，什么也不做。

```go
ui.Haptic(ui.HapticSuccess)
```

需要与动画配合的触感反馈，请看[动效与手势](motion.md)。

## 崩溃报告：asuka/sdk/crash

导入了 `asuka/sdk/crash` 的应用会为每次崩溃保留一份报告，并在下次启动时读取，发到哪里由你决定：你自己的服务器、Sentry 或 Crashlytics 都可以。Asuka 自己不会上传任何东西。

```go
func App() ui.Node {
	crash.OnReport(func(r crash.Report) {
		go func() {
			if err := send(r); err == nil { // 由你实现上传：r 是 JSON
				crash.Delete(r.ID)
			}
		}()
	})
	crash.Set("user", userID)
	// ...
}

// 在应用运行过程中的其他地方：
crash.Breadcrumb("opened the cart")
crash.Record(err) // 已经处理过的错误，同样会上报
```

`OnReport(f)` 会在主线程上，先把已保存的每份报告逐一交给 `f`，之后每出现一份新报告（比如 iOS 在启动后稍晚才送达的诊断，或是记录下来的错误）再调用一次，每份报告只调用一次，直到调用它返回的 `stop` 为止。`Reports()` 读取全部报告，最早的排在最前（不要在主线程上调用）。报告会一直保留，直到调用 `Delete(id)`；超过 `MaxReports`（50）份时，最早的会被丢弃。同一份报告每次读取时的 `ID` 都相同，所以服务器能够识别出重复发送的报告。

各平台会上报哪些内容，按 `Kind` 分类如下：

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Go`：任意 goroutine 上未被 recover 的 panic；致命错误（并发写 map、死锁、内存耗尽） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓（宿主会保存运行时的输出） |
| `Exception`：未捕获的异常 | NSException（Objective-C，以及 Swift 中的 `NSException`） | JVM 的 `Throwable` | — | — | — | 未捕获的 JavaScript 错误（非致命） |
| `Native`：信号、Mach 异常或 Windows 异常 | MetricKit | 系统的 tombstone | MetricKit | 运行时的报告 | 运行时的报告（原生代码中的信号） | — |
| `Hang` | MetricKit 报告的卡顿（非致命）、看门狗 | ANR 及其 trace | MetricKit | — | — | — |
| `OutOfMemory` | 超出内存上限（由 MetricKit 报告时） | 因内存不足被系统终止 | — | — | — | — |
| `Resource` | CPU 和磁盘写入异常（非致命） | 资源使用过度 | MetricKit | — | — | — |
| `NonFatal`：`Record(err)` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

Go 的崩溃由 Go 运行时自己保存（`runtime/debug.SetCrashOutput`，在应用启动时、`main` 运行之前设置），包含每个 goroutine 的调用栈。未捕获的异常由宿主的处理函数在应用结束时写下（在它之前设置的处理函数，比如某个崩溃 SDK 的，随后依然会运行）。其余的则由系统在之后的某次启动时交给应用：iOS 和 macOS 的 MetricKit 诊断，以及 Android 对应用进程结束原因的记录（`getHistoricalProcessExitReasons`，Android 11 及以上；原生崩溃的 tombstone 从 Android 12 开始提供）。如果某次崩溃已经由宿主的处理函数报告过，系统又再报告一次（iOS 上未捕获异常导致的 SIGABRT，Android 的 `REASON_CRASH`），那么系统的这份会被排除。

每份报告都带有 `Context`：应用的版本和构建号，设备的型号和系统版本，进程及其已运行的时长，当时是否在前台，以及最近的 `Breadcrumb`（最多 `MaxBreadcrumbs`，即 100 条）和通过 `Set` 设置的值。这些信息一有变化就会写入应用的数据目录，每次追加一行（不调用 sync，所以开销和一次普通写入差不多），等文件涨到实际所需大小的两倍时再整体重写一次。这样一来，不管应用是怎么崩溃的，这些信息在崩溃之前就已经写到磁盘上了。生命周期的变化也会记成面包屑（“app: background”）。

报告的 `Text` 是平台给出的原始内容（Go 的崩溃输出、调用栈、tombstone、ANR 的 trace）；`Threads` 是把这些调用栈解析成的帧，崩溃的线程排在最前面。Go 的帧包含函数、文件和行号。原生帧包含所在的二进制文件、该二进制的 UUID（Mach-O）或 build ID（ELF）、地址以及它在二进制中的偏移，崩溃收集服务正是用这些信息，配合构建时产出的符号来做符号化（见[发布](shipping.md#崩溃报告的符号)）。

**发送到 Sentry 或 Crashlytics。** 对于 Sentry，把每份报告转成一个事件，发到它的 envelope 端点（`https://<host>/api/<project>/envelope/`，使用你 DSN 中的 key）：`exception.values[0]` 的 `type` 为 `Kind`，`value` 为 `Message`，`stacktrace.frames` 为崩溃线程的帧，从最外层开始排列（这是 Sentry 的顺序，与 `Thread` 中的顺序相反），每一帧带上 `function`、`filename` 和 `lineno`，原生帧则带上 `instruction_addr` 和 `package`；再在 `debug_meta.images` 中列出这些帧所在的每个二进制文件：`{"type": "macho", "debug_id": BinaryID, "image_addr": Address-Offset}`（Android 上为 `"elf"`），这样你用 `sentry-cli` 上传的 dSYM 或符号文件才能派上用场。`Context` 放进 `contexts`、`tags` 和 `breadcrumbs`。Crashlytics 没有可以直接发送的公开端点：你可以把报告发到自己的服务器，或者在你自己的原生模块里（[原生代码](native.md)）通过 Crashlytics 的 SDK 把它们记录为非致命错误。不管用哪种方式，App Store 审核都会问到崩溃数据：如果你发送了，就要如实声明（在 `asuka.json` 的 `ios.collected` 中加上 `{"type": "CrashData", "purposes": ["AppFunctionality"]}`）。

**各平台的细节。** iOS 只会把 MetricKit 链接进读取报告的应用（调用了 `crash.Reports` 或 `crash.OnReport` 的应用），并且把诊断交给应用本身，而不是它的分享扩展。带有自己分享面板（`app.ShareSheet`）的应用，会把面板的崩溃和应用自己的放在一起，保存在两者的 App Group 中，任何一方都能读取。从 iOS 17 开始，MetricKit 会给出崩溃的进程 ID，借此可以把崩溃关联到那一次运行的上下文；在此之前，则关联到崩溃之前的最后一次运行。MetricKit 在模拟器上不会送达诊断。Android 不需要额外配置；Android 11 之前没有原生崩溃和 ANR 的记录。macOS：MetricKit 从 macOS 12 开始可用；导致应用退出的未捕获 Objective-C 异常会以 MetricKit 崩溃报告的形式出现（主线程上的这类异常会被 AppKit 捕获并记入日志）。Windows：进程没有处理的每一个异常都由 Go 运行时处理（它是最后一个处理程序）：它会打印出异常和所有 goroutine，然后退出。在没有控制台的情况下（发布构建，以 GUI 程序构建），Asuka 会把运行时打印的内容写入本次运行对应的文件，下次启动时读取，生成一份 `Native` 报告。Asuka 不会设置自己的未处理异常过滤器：Go 的处理程序会先运行，替换它会破坏 Go 自身的异常处理。Windows 错误报告（Windows Error Reporting）照常保留它自己的记录。在浏览器中，Go 运行时无法把崩溃输出写入文件（`SetCrashOutput` 依赖 `dup`），所以宿主会在 Go 运行期间保存运行时打印的内容，等程序因 panic 或致命错误结束时，把它写入本次运行的记录，存放在应用的文件中（IndexedDB），下次启动时就会读取。未捕获的 JavaScript 错误（无论来自宿主还是应用的 `web/*.js`）会立即生成一份 `Exception` 报告，它是非致命的（页面会继续运行），并附带本次运行的上下文；每次运行最多 20 份，同一个错误只报告一次。每个页面都会在同源的 `localStorage` 里标记自己这次运行还活着，这样应用其他开着的页面就不会去动它的记录。

**在 `asuka dev` 下**，Go 代码运行在你的 Mac（或 PC）上：它的崩溃照常打印在终端里，同时也保存在那台电脑上，所以下次运行时 `crash.Reports` 能读到它们。播放器应用自身的原生崩溃不属于你的应用，不会报告给它；应用的正式构建（`asuka dev --once`、`asuka build`）则会报告所有崩溃。

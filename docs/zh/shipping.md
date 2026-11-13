# 打包与发布

`asuka build` 会在应用目录（或你指定的目录）下，为某个平台制作发布构建，输出到该目录的 `dist` 中。`asuka publish` 则负责上传：把 iOS 和 Android 构建传到各自的应用商店，也就是 TestFlight 和 App 审核，或者 Google Play 的某个轨道（见[发布到应用商店](#发布到应用商店)）；把桌面构建的已签名更新传到 GitHub Releases 或 S3 存储桶，已安装的应用会从那里自动更新（见[自动更新](#自动更新)）。

| `--platform` | 产物（位于 `dist`） | 构建环境 | 如何交到用户手里 |
|---|---|---|---|
| `ios` | `<Name>.ipa`，已为 App Store 签名 | 装有 Xcode 的 Mac | TestFlight、App Store（`asuka publish`） |
| `android` | `<Name>.aab`，用你的上传密钥签名 | 装有 JDK、Android SDK 和 NDK 的任意系统 | Google Play（`asuka publish`） |
| `mac` | `<Name>.app`，通用二进制；或 `.pkg`（`--appstore`） | 装有 Xcode 命令行工具的 Mac | 签名并公证后供用户下载；Mac App Store |
| `windows` | `<Name>/`，内含 `.exe` 及其资源 | 任意系统 | zip 压缩包，或你自己制作的安装程序、MSIX |
| `linux` | `linux/`：一个安装前缀目录、一个 `.tar.gz`、一份 Flatpak 清单 | 任意系统 | `.tar.gz`、Flatpak |
| `web` | `web/`，一个静态网站 | 任意系统 | 任意 Web 服务器或 CDN |

不加 `--platform` 时，在 Mac、Windows PC 或 Linux 机器上会为当前这台电脑构建。除了 iOS 和 Android，其他平台的构建都是 `CGO_ENABLED=0` 的纯 Go 构建，不需要 C 工具链。

## asuka.json

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "version": "1.2",
  "build": 7,
  "icon": "icon.png",
  "ios": { "team": "ABCDE12345" },
  "android": { "keystore": "upload.jks", "keyAlias": "upload" }
}
```

所有键都是可选的；[asuka.json 参考](asuka-json.md) 列出了全部的键及其类型和默认值。下面这些对每个平台都生效：

- `name` 是应用的名称：显示在图标下方和 Mac 的菜单栏里，也是 Windows 上的窗口标题、Linux 启动器里显示的名字以及网页的标题。构建产物的文件名也取自它（`Notes.app`、`Notes.exe`）。默认使用目录名。
- `id` 在 iOS 和 Mac 上是 bundle ID；在 Android 和 Linux 上是 application ID（Linux 的 desktop 文件、图标和 D-Bus 名称都用它）；在 Windows 上是 AppUserModelID（通知要用到它）。默认是 `dev.asuka.<directory>`。
- `version` 是用户看到的版本号（默认 `1.0`）；`build` 是已上传构建的序号，每次上传都必须递增（默认 1）。不过 `asuka publish` 会自动把每个构建编号为商店中现有最大编号之后的下一个，所以 `build` 只是起始值。在 Windows 上，二者组成 `.exe` 的文件版本（版本 1.2、构建 7 对应 `1.2.0.7`）；在 Linux 上则是 metainfo 中的 release。
- `icon` 是一张 1024×1024 的正方形 PNG，用于 iOS 时不能有透明区域。每个平台的构建会自动生成该平台需要的各种尺寸：iOS 上是 asset catalog，Android 上是启动器图标，Mac 上是 `.icns`，Windows 上是嵌入 `.exe` 的 `.ico`，Linux 上是 hicolor 主题下 16 到 512 像素的各尺寸图标，Web 上则是 manifest 中的图标、favicon 以及 iOS 主屏幕图标。
- `accent` 是应用的强调色，格式为 `"#rrggbb"`。它在 iOS 上是 tint 色；在 Android 上是 Material 3 配色方案的种子色；在 Mac 上是 `Accent`、填充按钮和链接的颜色（AppKit 自带的控件仍然使用用户选择的强调色）；在 Windows 上代替系统强调色，作为控件的强调色；在 Linux 上是 libadwaita 的强调色；在 Web 上是页面的强调色以及 manifest 的 `theme_color`。不设置时，各平台使用各自的默认强调色。
- `scheme` 让应用可以通过 `myapp://` 这样的 URL 打开，支持 iOS、Android、Mac（`CFBundleURLTypes`）、Linux（desktop 文件中的 `x-scheme-handler`）和 Windows（应用启动时在 `HKEY_CURRENT_USER\Software\Classes` 下为当前用户注册）。`links` 则让指向你自己域名的 https 链接也能打开应用（即 universal links 和 App Links，仅限 iOS 和 Android）。这些 URL 都由 `ui.OnOpenURL` 接收。
- `fileTypes` 是应用能打开的文件类型，打开的文件由 `ui.OnOpenFiles` 接收：

  ```json
  "fileTypes": [
    {"name": "Markdown Document", "extensions": ["md", "markdown"], "role": "viewer"},
    {"name": "Notes Document", "extensions": ["note"], "icon": "note.png"}
  ]
  ```

  `extensions` 是必填项；`name` 默认是第一个扩展名的大写形式加上 "Document"。对于 Asuka 认识的扩展名（md、txt、json、csv、pdf、png、jpg……），`mimeTypes` 和 `uti` 默认采用系统对它的叫法；其他扩展名则视为应用自有的类型：`application/x-<app>-<ext>` 和 `<id>.<ext>`，在 Mac 和 iOS 上会被导出（exported），在 Linux 上会为 shared-mime-info 定义，并且应用会成为这类文件的默认打开方式。`conformsTo` 是导出或导入的类型所遵循的 UTI（默认 `public.data`、`public.content`）；`role` 取 `editor`（默认）或 `viewer`；`icon` 是一张正方形 PNG，在 Mac、Windows 和 Linux 上用作这类文件的图标。各平台的声明方式如下：Mac 和 iOS 写在 Info.plist 中（`CFBundleDocumentTypes`，iOS 上支持原地打开文档）；Android 声明为 intent filter；Windows 在应用启动时为当前用户注册（每种类型一个 ProgID，扩展名的 `OpenWithProgids`，图标打包在 `.exe` 中）；Linux 写在 desktop 文件的 `MimeType` 和 `share/mime/packages/<id>.xml` 中（解压 `.tar.gz` 后需要运行 `update-mime-database` 和 `update-desktop-database`，包管理器和 Flatpak 会自动完成这一步）；Web 写在 manifest 的 `file_handlers` 中（仅限 Chromium 里已安装的应用）。
- `singleInstance` 默认为 `true`：在 Windows 和 Linux 上，设置了 `id` 的应用只会运行一个实例。再次启动时，新进程会把参数交给正在运行的实例（由 `app.OnSecondInstance` 接收，其中的文件交给 `ui.OnOpenFiles`），然后自行退出。设为 `false` 则每启动一次就运行一个新实例。Mac 上则无论如何都只运行一个实例。

按平台划分的键：

- `ios.*` 用于 iOS 构建（见 [iOS](#ios)）；`ios.team` 同时也是 Mac 构建所用的团队，`ios.usage` 里的用途说明也会写进 Mac 的 Info.plist。
- `android.*` 用于 Android 构建（见 [Android](#android)）。
- `mac.profile` 是 Mac 构建的描述文件（provisioning profile），`mac.appStoreProfile` 是 Mac App Store 构建的描述文件，详见 [macOS](#macos)。
- `linux.*` 决定 Linux 构建的 desktop 文件和 AppStream metainfo 的内容，详见 [Linux](#linux)。
- `web.vapidPublicKey` 是应用服务器的 VAPID 公钥，供 Web 上的 `asuka/sdk/push` 使用（即 Web Push，见 [Web](web.md#asukasdkpushweb-push)）。
- Windows 构建只需要上面那些通用的键；它的签名证书通过环境变量提供（见 [Windows](#windows)）。

下面这些用于手机和平板：

- `orientations`：`portrait`、`landscape`（默认两者都支持），只对手机生效。在 iPad 上，应用总是支持所有方向，因为分屏浏览（Split View）、侧拉（Slide Over）和台前调度（Stage Manager）都要求如此；同样，Android 16 在平板和折叠屏设备上也会忽略这项设置。
- `ios.iphoneOnly` 让应用不支持 iPad。默认情况下，应用同时也是 iPad 应用，用户把窗口调成多大都能适应；在 Android 上，应用可以调整大小，以适应分屏和桌面窗口。因此布局时请依据 `ui.UseEnvironment()` 的 `Width` 和 `Height`，它们是窗口的尺寸，而不是设备的尺寸。
- `shareTarget` 让应用出现在其他应用的分享面板里：设为 `true` 可以接收文本和链接，设为 `{"types": ["text", "image/*"], "max": 10}` 还能接收文件（见[设备](device.md#接收其他应用的分享uionshare)）；分享过来的内容由 `ui.OnShare` 接收。在 iOS 上，它是应用里的一个分享扩展，有自己的 bundle ID（应用的 ID 加上 `.share`）。
- `splash` 是启动屏：`{"image": "logo.png", "size": 96, "background": "#ffffff", "darkBackground": "#000000"}`（见[设备](device.md#启动屏asukasdksplash)）。
- `widgets` 是应用的主屏幕小组件，`shortcuts` 是应用提供给 Siri、Spotlight 和启动器的快捷操作（见[设备](device.md#主屏幕小组件asukasdkwidget)）。小组件扩展有自己的 bundle ID（应用的 ID 加上 `.widgets`）：发布时也要为它准备描述文件，可以用 `ios.widgetProfile` 指定。
- `android.googleServices` 是 Firebase 项目的 `google-services.json`，供 `asuka/sdk/push` 使用；`android.mapsKey` 是 Google Maps Platform 的 API 密钥，供 `asuka/sdk/maps` 使用。
- `ios.usage` 提供访问受保护资源时的用途说明（`NSCameraUsageDescription` 等，Mac 也会用到）；`ios.infoPlist` 向 Info.plist 添加任意键；`ios.backgroundModes` 向 `UIBackgroundModes` 追加条目（例如 `"audio"`，用于锁屏时录音和播放）；`android.permissions` 用来添加权限。

应用导入的包会自动带上它们需要的权限和用途说明：`asuka/sdk/camera` 带上相机的，`asuka/sdk/location` 带上定位的。不过默认的说明比较笼统，最好在 `ios.usage` 里写上你自己的。有些包还要求在 App ID 上开启相应的能力（capability）：`asuka/sdk/push` 需要 Push Notifications，`asuka/sdk/appleauth` 需要 Sign in with Apple，`asuka/sdk/integrity` 需要 App Attest，`asuka/sdk/agerange` 需要 Declared Age Range。在 developer.apple.com 上开启之后，重新生成描述文件即可。`asuka build --platform ios`（以及 `--platform mac`）会告诉你描述文件缺了哪些能力。

## iOS

```sh
asuka build --platform ios
```

这条命令会生成一个为 App Store 签名的 `.ipa`。签名使用团队的分发证书（在你的钥匙串里，来自 Xcode 的 Accounts 设置）和对应应用 ID 的描述文件：优先用 `ios.profile` 或 `--profile` 指定的那个，否则从已安装的描述文件中挑选最匹配的。加上 `--distribution ad-hoc` 或 `development` 则改为针对描述文件中登记的设备签名；`--simulator` 会生成一个未签名的 `.app`，供 iOS 模拟器使用。

使用了 `asuka/sdk/sqlite` 的应用会在 `Frameworks` 里带上 SQLite，即 `asqlite.framework`（真机版或模拟器版），和应用一起签名。应用第一次打开数据库时才会加载它。应用的扩展（分享面板、Notification Service Extension）加载的也是应用里的这一份。

设置了 `shareTarget` 时，分享扩展也要签名，用的是同一团队下、针对扩展自身 ID 的描述文件（应用是 `com.example.notes`，扩展就是 `com.example.notes.share`）：优先用 `ios.shareProfile`，否则从已安装的描述文件中挑选最匹配的。通配符描述文件（`com.example.*`）可以同时覆盖两者。你需要在 developer.apple.com 上注册扩展的 ID，并像为应用生成描述文件那样，为它也生成一个。

如果 `shareTarget` 要接收文件，还需要一个 App Group `group.<app ID>`，扩展通过它把文件交给应用。在 developer.apple.com 上注册这个 group，为两个 ID（应用的和扩展的）开启 App Groups 能力并选中它，然后重新生成两者的描述文件。通配符 ID 不能使用 App Groups。如果某个描述文件缺少这个 group，构建会停下来并告诉你。iOS 模拟器不需要这些设置。另外，自带分享面板（`app.ShareSheet`）的应用无论接收什么内容，也都需要这个 group 来存放数据。

`asuka publish --platform ios` 会构建并上传到 App Store Connect（见[发布到应用商店](#发布到应用商店)）；也可以用 Apple 的 Transporter 上传。

**隐私清单。** App Store 要求每个应用都提供隐私清单，为它用到的、Apple 列出的需声明理由的每个 API 说明理由。Asuka 会根据二进制实际用到的 API 自动生成这份清单：Go 运行时会读取时钟和文件时间；清单还声明应用不做追踪、不收集任何数据。如果你的应用会收集数据或进行追踪，就自己写一份 `ios/PrivacyInfo.xcprivacy`，同时把上面那些理由也写进去，Asuka 会改用你的版本。无论应用用的是哪一份，`asqlite.framework` 都带有自己的隐私清单，说明 SQLite 用到的 API（文件时间、磁盘剩余空间）。

## Android

发布版要用你的上传密钥签名。先生成一个（只需一次），并妥善保管：

```sh
keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

在 `asuka.json` 里指定它（`android.keystore`、`android.keyAlias`），并通过环境变量提供密码：

```sh
ASUKA_KEYSTORE_PASSWORD=... asuka build --platform android
```

（如果密钥的密码与 keystore 的不同，还要设置 `ASUKA_KEY_PASSWORD`；在 CI 中，可以用 `ASUKA_KEYSTORE` 和 `ASUKA_KEY_ALIAS` 指定 keystore 和别名。）这会生成一个用于 Google Play 的 `.aab`，支持 64 位和 32 位 ARM 手机以及 x86_64（Android 模拟器、Chromebook），要求 Android 13 及以上。Play 会根据它为每台手机生成只包含该机架构的 APK。使用了 `asuka/sdk/sqlite` 的应用，会在每个架构下的 Go 代码旁边带上 SQLite 的库 `libasqlite.so`，Android 会像加载应用自身的库那样直接从 APK 中加载它（不压缩，按 16 KB 页对齐）。`asuka publish --platform android` 会构建并上传到 Google Play（见[发布到应用商店](#发布到应用商店)）。

## 发布到应用商店

`asuka publish` 会像 `asuka build` 一样制作发布构建，然后通过商店的 API 上传。动手之前，它会先向商店确认那些可能导致失败的条件：应用是否存在、密钥是否有权上传、该版本是否还接受新构建、TestFlight 群组是否存在。这样就不会白白构建一个注定传不上去的包。每个构建的编号都是商店中现有最大编号加一（且不小于 `asuka.json` 中的 `build`），所以两次发布之间不需要手动递增。如果想指定编号，可以用 `--build 42`。

**App Store Connect。** 在 App Store Connect 中（Users and Access › Integrations › App Store Connect API）创建一个具有 App Manager 角色的 API 密钥，下载它的 `.p8` 文件，然后在 `asuka.json` 或环境变量中指定（`ASUKA_ASC_KEY_ID`、`ASUKA_ASC_ISSUER_ID`，以及 `ASUKA_ASC_KEY`，值为 `.p8` 的路径或文件内容）。没有指定 `key` 时，会像 `altool` 那样到 `~/.appstoreconnect/private_keys` 中查找 `AuthKey_<id>.p8`。不要把 `.p8` 提交到仓库里。另外，要先在 App Store Connect 中添加这个应用（Apps › + › New App），并填上它的 bundle ID。

```json
"ios": {
  "appStoreConnect": { "keyId": "ABC123DEF4", "issuerId": "57246542-96fe-1a63-e053-0824d011072a" }
}
```

```sh
asuka publish --platform ios                                  # 上传到 TestFlight：分发给团队内部测试员
asuka publish --platform ios --groups "Friends, QA" --notes "Try the new sync."
asuka publish --platform ios --review --notes @release-notes/ios --release manual
```

它会上传 `.ipa`，等待 Apple 处理完成（通常要几分钟，有时要一个小时甚至更久），并打印 Apple 给出的警告或拒绝原因，然后：

- `--notes` 是 TestFlight 的“测试内容”（What to Test），加上 `--review` 时也是 App Store 的“新功能”（What's New）。可以直接写文本，也可以用 `@file` 指定一个文件（使用应用的主要语言），或者用 `@dir` 指定一个目录，每种语言一个文件，文件名使用 App Store Connect 的语言代码（`en-US.txt`、`ja.txt`）。更新版本时，该版本支持的每种语言都需要提供“新功能”说明。
- `--groups` 按名称把构建分发给 TestFlight 群组。如果是外部群组（团队以外的测试员），构建会先进入 Beta 版审核，所以请先在 TestFlight 中填好测试信息。
- `--review` 会在 App Store 版本（即 `asuka.json` 中的 `version`）不存在时创建它，为它关联这个构建，然后提交审核。首次提交之前，需要先在 App Store Connect 中填好商品页信息、截图和隐私问卷，缺了什么会打印出来。`--release auto` 在审核通过后自动发布，`--release manual` 则等你手动发布。
- `--no-wait` 在 Apple 收到上传后就结束，不再等待处理。

**Google Play。** 在 Google Cloud 中创建一个服务账号，并为它生成 JSON 密钥（IAM › Service accounts › Keys）；所在项目需要开启 Google Play Android Developer API。然后在 Play Console 的 Users and permissions 中邀请这个服务账号的邮箱，并授予它发布该应用的权限。在 `asuka.json`（`"android": {"serviceAccount": "play.json"}`）或 `ASUKA_PLAY_SERVICE_ACCOUNT`（文件路径或 JSON 内容）中指定这个密钥。注意，应用的第一个 `.aab` 只能在 Play Console 中上传：先在那里创建应用并手动上传第一个构建，之后的构建再交给 `asuka publish`。

```sh
asuka publish --platform android                              # 内部测试轨道
asuka publish --platform android --track beta --notes @release-notes/android
asuka publish --platform android --track production --rollout 0.1
```

- `--track` 可以是 `internal`（默认）、`alpha`（封闭测试）、`beta`（开放测试）、`production`，或者某个封闭测试轨道的名称。
- `--rollout 0.1` 把这次发布推送给十分之一的用户，之后可以在 Play Console 中逐步扩大比例。`--draft` 只创建草稿，之后到 Play Console 里发布。应用正式上架之前，Play 只接受草稿，所以这时的发布都会变成草稿。
- `--notes` 是版本说明（最多 500 个字符）：直接写的文本或 `@file` 使用商品详情的默认语言；`@dir` 则每种语言一个文件，文件名使用 Play 的语言代码（`en-US.txt`、`ja-JP.txt`）。
- 如果应用有正在审核中的更改，命令会停下来；加上 `--cancel-review` 可以取消那次审核，把那些更改随这次发布一起提交。如果 Play 暂存着应用的更改等你提交（比如被拒之后），这些更改会被保存下来，由你在 Play Console 中提交。

`--file dist/Notes.ipa`（或 `.aab`）会上传一个之前用 `asuka build` 构建好的包，而不是重新构建。

## macOS

```sh
asuka build --platform mac
```

这条命令在 Mac 上生成 `dist/<Name>.app`：一个通用应用（同时支持 Apple 芯片和 Intel），要求 macOS 11 及以上，包含 Info.plist、图标、语言和资源，采用临时签名（ad hoc），可以在你自己的 Mac 上运行。和 iOS 一样，应用导入的包会从 `ios.usage` 中带上它们的用途说明（定位的用途说明在 Mac 上是 `NSLocationUsageDescription`，取自 `NSLocationWhenInUseUsageDescription`），以及强化运行时（hardened runtime）所需的对应 entitlement（`com.apple.security.device.camera`、`...audio-input`、`...personal-information.location` 等）。缺少这些 entitlement，用 Developer ID 签名的应用就无法访问相机、麦克风或用户数据。如果应用有自己的 Swift 代码（`macos/*.swift`），这些代码会针对两种架构编译进 `Contents/Frameworks`，和应用一起签名（见[原生代码](native.md)）。使用了 `asuka/sdk/sqlite` 的应用，SQLite 的库（`libasqlite.dylib`，包含两种架构）也放在那里，签名方式与应用相同：用 Developer ID 签名时，强化运行时只加载由应用所属团队签名的库，公证服务也会逐个检查（见[库文件](data.md#库文件)）。

要在 Mac App Store 之外分发给别人，需要用你的 Developer ID 签名，再交给 Apple 公证，这样 Gatekeeper 才会放行：

```sh
xcrun notarytool store-credentials asuka   # 只需一次：填入你的 Apple ID、团队和 App 专用密码
asuka build --platform mac --sign "Developer ID Application: Your Name (TEAMID)" --notarize asuka
```

`--notarize` 会把签好名的应用发给 Apple 的公证服务并等待结果（通常几分钟）。如果被拒，它会打印公证服务返回的日志；通过后，它会把公证票据装订（staple）到应用上，这样离线时也能打开。在 CI 中，`--notarize env` 会从环境变量中读取 `ASUKA_APPLE_ID`、`ASUKA_APPLE_PASSWORD`（App 专用密码）和 `ASUKA_TEAM_ID`，而不是使用钥匙串中保存的配置。

推送（`asuka/sdk/push`）、通过 Apple 登录（`asuka/sdk/appleauth`）和 App Attest（`asuka/sdk/integrity`）所需的 entitlement 只有描述文件才能授予。在 developer.apple.com 的 Profiles 中，为应用的 ID 创建一个 macOS Developer ID 描述文件（该 ID 需要开启上述能力），然后下载下来：

```sh
asuka build --platform mac --profile Notes.provisionprofile --notarize asuka
```

描述文件会以 `Contents/embedded.provisionprofile` 的形式放进应用。签名时，应用会从描述文件授予的 entitlement 中取出它的包需要的那些（`com.apple.developer.aps-environment`、`com.apple.developer.applesignin`、`com.apple.developer.devicecheck.appattest-environment`），再加上应用 ID、团队和强化运行时。描述文件缺了什么，构建会告诉你。不加 `--sign` 时，签名身份取钥匙串中该描述文件允许的证书（Developer ID Application）；如果 `--sign` 指定的身份不在描述文件中，会报错。`--profile` 可以是文件路径，也可以是已安装描述文件的名称或 UUID；`ASUKA_MAC_PROFILE` 和 asuka.json 的 `mac.profile` 作用相同。三者都没有指定时，使用了这些包的应用会用已安装的（位于 Xcode 描述文件目录中的）与其 ID 最匹配的描述文件签名；一个都找不到，就不带描述文件签名，此时构建会告诉你哪些功能在 Mac 上无法使用，应用的其余部分照常工作。

`--appstore` 则改为生成用于 Mac App Store 的 `dist/<Name>.pkg`：应用运行在 App 沙盒中（允许访问网络和用户选择的文件），用 Mac App Store 描述文件对应的证书签名（Apple Distribution；描述文件依次取 asuka.json 的 `mac.appStoreProfile`、`--profile`、最佳匹配），安装包则用团队的 Mac Installer Distribution 证书签名。之后用 Transporter 或 `xcrun altool --upload-package` 上传即可；`asuka publish` 目前还不支持上传 Mac 构建。公证只适用于在商店之外分发的应用，所以同时使用 `--notarize` 和 `--appstore` 会报错。

## Windows

```sh
asuka build --platform windows               # x64，Windows on Arm 上以模拟方式运行
asuka build --platform windows --arch arm64  # 在 Arm PC 上原生运行
```

这会生成一个文件夹 `dist/<Name>`，在任何系统上都能构建。文件夹里是 `<Name>.exe` 和应用的资源；使用了 `asuka/sdk/sqlite` 的应用还会带上同一架构的 SQLite 库 `asqlite.dll`（它只依赖每台 Windows 10 和 11 都自带的通用 C 运行时 Universal C Runtime）。`.exe` 是一个 GUI 程序（不会弹出控制台窗口），内置了各种资源：Windows 所需各尺寸的图标、版本信息（来自 `version` 和 `build`），以及一份面向 Windows 10 和 11 的清单（manifest），声明使用通用控件 6、支持每显示器 DPI 感知和 UTF-8。如果应用目录里有自己的 `.syso` 文件，就改用其中的资源。最后，把文件夹打成 zip，或者打包成安装程序、MSIX。

为了让 SmartScreen 知道这个 `.exe` 出自谁手，要用导出为 `.pfx` 的代码签名证书给它签名：

```sh
ASUKA_WINDOWS_CERT=cert.pfx ASUKA_WINDOWS_CERT_PASSWORD=... asuka build --platform windows
```

构建会用 SHA-256 签名，并为签名加盖时间戳，这样证书过期后签名依然有效。时间戳取自第一个响应的 RFC 3161 服务器：先尝试 `ASUKA_WINDOWS_TIMESTAMP` 中列出的服务器（逗号分隔），没有设置的话依次尝试 DigiCert、Sectigo 和 GlobalSign。如果都没有响应，构建会失败并说明原因；设置 `ASUKA_WINDOWS_TIMESTAMP=none` 则不加时间戳（这样签名会随证书一起失效）。在 Windows 上用 `signtool` 签名（来自 Windows SDK；优先用 `PATH` 中的，否则用已安装的最新版本），在其他系统上用 `osslsigncode`（`brew install osslsigncode`、`apt install osslsigncode`），`asqlite.dll` 也会一并签名。不设置 `ASUKA_WINDOWS_CERT` 时，`.exe` 不会被签名。如果证书存放在硬件令牌或云服务（Azure Trusted Signing）中，就要用它们自己的工具签名：构建完成后，用那个工具为文件夹中的 `.exe`（以及 `asqlite.dll`）签名。

会显示通知的应用（`asuka/sdk/notify`）第一次运行时，会在用户的开始菜单里创建一个快捷方式，因为 Windows 要求应用有快捷方式才能发送 toast 通知。快捷方式的 AppUserModelID 取 asuka.json 的 `id`（没有的话就根据应用名称生成一个）。

## Linux

```sh
asuka build --platform linux                # 当前机器的架构，否则为 amd64
asuka build --platform linux --arch arm64   # amd64 或 arm64
```

Linux 构建可以在任何系统上完成，因为宿主无需 cgo 就能调用 GTK 4 和 libadwaita。它会在 `dist/linux` 中生成：

- `<name>/`：一个按安装前缀（install prefix）结构组织的文件夹，以应用名的小写形式命名（`My Notes` 会变成 `my-notes`），其中包括：
  - `bin/my-notes`：应用本身；
  - `share/my-notes/assets`：应用的资源，应用会以 `bin/` 为起点找到它；
  - `lib/my-notes/libasqlite.so`：SQLite 的库，仅限使用了 `asuka/sdk/sqlite` 的应用，同样以 `bin/` 为起点找到（要求 glibc 2.17 或更高版本；它不导出 SQLite 自身的任何符号，所以即使同一进程里还加载了系统的 SQLite，两者也不会冲突）；
  - `share/applications/<id>.desktop`：启动器显示和运行的入口；
  - `share/icons/hicolor/<size>/apps/<id>.png`：`icon` 从 16 到 512 像素的各个尺寸；
  - `share/metainfo/<id>.metainfo.xml`：AppStream metainfo，软件中心展示的就是它。

  可以直接在这个文件夹里运行 `bin/my-notes`，也可以把它安装到某个前缀下，让启动器能找到它：

  ```sh
  tar -xzf my-notes-1.0-linux-amd64.tar.gz --strip-components=1 -C ~/.local
  ```

- `my-notes-<version>-linux-<arch>.tar.gz`：上面那个文件夹的压缩包，用于分发。
- `<id>.yml`：一份 [Flatpak](https://flatpak.org) 清单，基于 GNOME 运行时打包上面那个文件夹（其中的 `bin`、`lib` 和 `share`）：

  ```sh
  flatpak install flathub org.gnome.Platform//50 org.gnome.Sdk//50   # 只需一次
  flatpak-builder --user --install --force-clean build dist/linux/com.example.notes.yml
  flatpak run com.example.notes
  ```

运行应用的机器需要 GTK 4.14 和 libadwaita 1.5 或更高版本（GNOME 运行时 46 及以上都满足）。应用的 ID 就是它的 application ID：desktop 文件、图标、D-Bus 名称（再次启动时会显示第一个实例的窗口）以及数据目录（`~/.local/share/<id>`）都以它命名。ID 只能包含字母、数字、`_` 和 `-`，至少要有两段，并且每一段都不能以数字开头。

desktop 文件和 metainfo 的内容在 `asuka.json` 中设置：

```json
"linux": {
  "summary": "Write things down",
  "description": "Notes, kept on your computer.\n\nA second paragraph.",
  "categories": ["Office"],
  "keywords": ["memo"],
  "license": "MIT",
  "developer": "Example Inc.",
  "homepage": "https://example.com",
  "flatpakRuntime": "50"
}
```

`categories` 取自 [freedesktop.org 的分类表](https://specifications.freedesktop.org/menu-spec/latest/category-registry.html)（默认 `Utility`）；`license` 是一个 SPDX 表达式。Flathub 和软件中心都要求提供简介（summary）、描述、许可证和开发者。

## Web

```sh
asuka build --platform web
```

这会在 `dist/web` 中生成一个静态网站，包括：`index.html`、编译成 WebAssembly 的应用、Web 宿主的脚本、应用的 service worker、应用的资源、带图标的 Web 应用清单（manifest）、gzip 压缩副本，以及供 Cloudflare 和 Netlify 使用的 `_headers` 文件。文件名中带有内容摘要，所以除了 `index.html` 和清单之外，其他文件都可以永久缓存。你可以用任何 Web 服务器或 CDN 托管这个文件夹，但 `.wasm` 必须以 `application/wasm` 类型提供，并且要通过 HTTPS 访问（service worker、Web Push 和安全存储都依赖 HTTPS）。网站里具体有什么、在浏览器中有哪些不同的行为，请参阅 [Web](web.md)。

它还会写出 `version.json`，记录本次构建的版本及其版本说明（取自 `CHANGELOG.md` 中的对应小节）。页面中的 `asuka/sdk/update` 会读取它：如果页面比网站上的版本旧，就会提示用户重新加载（见[自动更新](#自动更新)）。

## 自动更新

桌面应用可以像使用 Sparkle 的 Mac 应用那样自我更新：检查更新源（feed）上有没有新版本，显示版本说明，下载新版本（如果有从当前版本出发的增量包，就只下载变化的部分），验证它确实由你的密钥签名，然后安装并重新启动。手机应用通过应用商店更新；Web 应用则在网站发布新版本后重新加载页面。

### 配置

首先生成用于签名更新的密钥对（只需一次）：

```sh
asuka update keygen
```

它会把私钥 `asuka-update.key` 和公钥 `asuka-update.pub` 写到你的配置目录下的 `asuka/update-keys` 中（可以用 `--out` 换一个位置），并打印出需要添加到 `asuka.json` 的内容。私钥不要放进仓库，而应该保存在密码管理器和 CI 的 secrets 里。已安装的应用只接受由这把私钥签名的更新：一旦丢失，这些应用就再也无法更新；而拿到它的人，则可以向你的用户推送任意代码。

```json
"updates": {
  "publicKey": "…the contents of asuka-update.pub…",
  "github": "you/notes"
}
```

| 键 | 说明 |
|---|---|
| `publicKey` | `asuka-update.pub` 的内容，会编译进应用 |
| `github` | `owner/name`：一个公开仓库，更新文件放在它以 `v1.2.0` 这样的标签命名的 Release 中；应用读取最新 Release 里的更新 |
| `tagPrefix` | Release 标签的前缀（默认 `v`）。换成别的前缀（如 `desktop-v`）后，仓库的最新 Release 就可以留给别的产品（比如一个 CLI）：应用会改为通过 GitHub API 查找带有该前缀的最新 Release |
| `url` | 代替 `github`：一个提供这些文件的 HTTPS 目录（S3 或 R2 存储桶、CDN、你自己的服务器）；应用读取 `url/update-<target>.json` |
| `feed` | 你自己的服务器，应用改为向它询问更新（文件仍然放在 `url`）：见[自建更新服务器](#自建更新服务器) |
| `s3` | 提供 `url` 的存储桶，`asuka publish` 会上传到这里：`bucket`、`prefix`（所在目录）、`region`、`endpoint`（R2、MinIO 的端点；默认是 Amazon S3）、`pathStyle` |
| `privateKey` | 私钥的路径（`~/` 表示主目录），在你自己的机器上构建时使用 |
| `changelog` | 一个 Markdown 文件，其中的 `## 1.2.0`（或 Keep a Changelog 格式的 `## [1.2.0] - 2026-11-13`）小节就是版本说明；默认是 `CHANGELOG.md`（如果存在），此时它必须包含当前版本的小节 |
| `deltas` | 为之前的多少个版本生成增量包（默认 3；0 表示不生成） |

在 CI 和测试中，也可以用环境变量设置这些值：`ASUKA_UPDATE_PRIVATE_KEY`（私钥本身，或私钥文件的路径）、`ASUKA_UPDATE_PUBLIC_KEY` 和 `ASUKA_UPDATE_URL`（优先于 `publicKey` 和 `url`）。

### 在应用中

`update.Window` 是更新窗口，放在应用的根节点里；`update.AppMenu` 会添加“Check for Updates…”菜单项：在 Mac 上位于应用菜单的“关于”之后，在 Windows 上位于帮助菜单中，在 Linux 上位于标题栏的主菜单中。

```go
func Root() ui.Node {
	return ui.Column(ui.Style{},
		update.Window(update.Options{Icon: "logo.png"}), // assets/ 中的一张图片
		ui.MenuBar(ui.Val([]ui.MenuBarMenu{update.AppMenu()})),
		// ...
	)
}
```

它会在启动 10 秒后检查一次，之后每天检查一次（`Options.Interval`）。发现新版本时，窗口会显示版本说明，并提供 **Install Update**、**Remind Me Later** 和 **Skip This Version** 三个按钮（跳过之后，后台检查不会再提示这个版本，但仍会提示下一个版本），以及一个“Automatically download and install updates in the future”选项：勾选后，之后检查到的更新会直接安装、不再询问，并在下次启动时生效。安装过程中会显示下载进度，完成后显示 **Relaunch Now**。`update.CheckForUpdates()` 执行一次由用户主动发起的检查（比如通过菜单项或按钮）：与后台检查不同，这时如果应用已是最新版本或者无法自我更新，窗口也会明确告诉用户。`AutomaticChecks`、`SetAutomaticChecks`、`AutomaticDownloads`、`SetAutomaticDownloads` 和 `LastCheck` 对应用户的这些选择（保存在应用数据目录下的 `update.json` 中），可以用来做设置页面。窗口里的文字由 Asuka 提供，会跟随应用的语言；如果想修改，可以在应用的翻译目录（catalog）中设置 `asuka.updateInstall` 等键。

如果应用想用自己的界面，可以在主线程之外调用这个包的函数，或者读取更新器的状态（`update.Use()`：检查中、有可用更新、下载中及已下载的字节数、准备就绪……）：

```go
go func() {
	r, err := update.Check(ctx) // nil：已是最新版本
	if err != nil || r == nil {
		return
	}
	// r.Version、r.Notes（Markdown）、r.Date、r.Size
	if err := r.Install(ctx, func(done, total int64) { /* 用 ui.Post 更新进度 */ }); err == nil {
		update.Relaunch() // 即 app.Relaunch：先退出（OnQuit 可以取消），再启动新版本
	}
}()
```

`update.Supported()` 会告诉你应用能否自我更新，以及不能的原因：开发构建，或者没有配置 `updates` 的构建（`ErrNotConfigured`）；由系统包管理器、Flatpak、Snap、Mac App Store 或 Microsoft Store 安装的应用（`ErrManaged`：由它们负责更新）；直接在下载位置运行、被 macOS 做了路径随机化（translocation）的 Mac 应用（`ErrTranslocated`：需要用户把它移到“应用程序”文件夹）；所在文件夹用户没有写权限（`ErrNotWritable`）；在手机上（`ErrUnsupported`）。遇到这些情况，窗口不会在后台检查，用户手动检查时则会说明原因。

`update.SetChannel("beta")` 让应用也接收某个渠道的预发布版本，适合那些选择加入 beta 的用户。一个版本属于哪个渠道，取决于其预发布标识中的字母部分（`1.4.0-beta.2` 属于 `beta`）；`"*"` 表示接收所有预发布版本。正式版本在每个渠道都会推送。`update.SetSource` 让应用用自己的代码读取清单和下载文件（例如服务端需要用户令牌时）；签名验证照常进行。

### 构建与发布

配置了 `updates` 并提供了私钥之后，`asuka build --platform mac`、`windows` 或 `linux` 会在构建产物旁边的 `dist/updates` 中写出：

- `notes-1.2.0-<target>.tar.gz`：安装后形态的应用，已签名（对其 SHA-256 做 Ed25519 签名）。在 Mac 上是应用包（bundle），在 Windows 上是文件夹里的全部内容，在 Linux 上就是构建出的那个 `.tar.gz`。target 有 `darwin-universal`、`windows-amd64`、`windows-arm64`、`linux-amd64` 和 `linux-arm64` 几种。
- 从之前已发布的版本到当前版本的增量更新，命名为 `notes-1.1.0-to-1.2.0-<target>.delta`。构建时会按已发布清单中记录的位置下载旧版本（同时验证签名），然后写出差异：哪些文件可以直接从已安装的应用中移动或复制，改动过的文件的 bsdiff 补丁，以及其余文件的完整内容。应用只会下载针对自己当前版本的增量包；如果增量更新失败（比如应用在安装后被改动过），就改为下载完整的归档。第一个版本没有增量包。
- `update-<target>.json`：清单，包含版本号、版本说明、日期，归档和各增量包的 URL、大小和签名，以及之前的各个版本（用于生成下一次的增量包）。预发布版本的清单是 `update-<target>-beta.json`，只有处在该渠道的应用才会读取。

没有私钥时，构建只生成应用本身，不写任何更新文件，并会提示这一点。`--version 1.2.0` 可以构建与 `asuka.json` 中不同的版本（CI 会从标签中取版本号）。Windows 的文件夹和 Linux 的前缀中还会多出一个 `.asuka-files`，列出这个版本安装了哪些文件。这样下次更新时，就能删掉新版本中已不再需要的文件，同时不会碰你自己放进去的任何文件。

`asuka publish` 会构建并上传这些文件，清单最后上传，确保应用读到清单时，清单里引用的文件都已经就位：

```sh
asuka publish --platform mac --sign "Developer ID Application: …" --notarize asuka
asuka publish --platform windows --arch arm64
asuka publish --platform linux --draft
```

- 配置了 `github` 时，上传到该版本标签（`v1.2.0`）对应的 Release，需要先登录 [GitHub CLI](https://cli.github.com)（`gh auth login`，或者设置 `GH_TOKEN`）。如果 Release 还不存在，会先创建为草稿，再发布；预发布版本会以 prerelease 的形式发布，使用自定义的 `tagPrefix` 时则不会被标记为最新（latest）。`--draft` 让 Release 保持草稿状态，等所有平台都上传完再发布（`gh release edit v1.2.0 --draft=false`），因为应用只会检查最新的已发布 Release。
- 配置了 `s3` 时，上传到存储桶，清单会带上 `Cache-Control: no-cache`（因为清单的文件名在各个版本之间保持不变）。凭据取自 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`（以及 `AWS_SESSION_TOKEN`），区域依次取 `s3.region`、`AWS_REGION` 或 `us-east-1`（R2 也接受这个值）。不需要其他任何工具。
- 只配置了 `url` 时，需要你自己把 `dist/updates` 上传过去，同样要最后上传清单，并保留之前版本的文件。

### 自建更新服务器

配置了 `feed` 后，应用会改为向你的服务器询问更新。URL 中的 `{target}`、`{version}`（应用当前运行的版本）和 `{channel}` 会被替换为实际的值：

```json
"updates": {
  "publicKey": "…",
  "url": "https://downloads.example.com/notes",
  "feed": "https://api.example.com/updates?target={target}&version={version}&channel={channel}"
}
```

没有新版本时，服务器返回 `204 No Content`（或 404）；否则返回一份清单，也就是 `asuka build` 写出的那种 JSON：

```json
{
  "version": "1.2.0",
  "notes": "- Faster sync.\n- Fixed the sidebar.",
  "date": "2026-11-13T09:00:00Z",
  "url": "https://downloads.example.com/notes/notes-1.2.0-darwin-universal.tar.gz",
  "size": 9123456,
  "signature": "…base64…",
  "deltas": [
    {"from": "1.1.0", "url": "https://…/notes-1.1.0-to-1.2.0-darwin-universal.delta", "size": 21432, "signature": "…"}
  ]
}
```

`asuka build` 会为 `url` 写出这些文件。你可以原样提供它写出的内容，也可以按自己的需要在多份清单之间挑选（比如分阶段发布，或者按账号分配渠道）。签名都是 `asuka build` 生成的，服务器只负责挑选。

### 各平台如何完成更新

- macOS：新的应用包会先经过检查（代码签名有效，与正在运行的应用属于同一团队且标识符相同，并去掉隔离属性），然后放在应用旁边，等应用退出——不管以什么方式退出。随后由一个辅助进程把它换到旧应用的位置；如果应用请求了重新启动，就通过 Launch Services 打开新版本，这样程序坞（Dock）里只会保留一个图标。应用所在的文件夹必须是用户可写的：管理员用户可以放在 `/Applications`，否则放在 `~/Applications`。
- Windows：在应用运行期间替换文件夹中的各项内容。每一项都先重命名挪到一边，只要有一项替换不了，就全部恢复原状；正在运行的 `.exe` 会被重命名（Windows 允许这样做），并在下次启动时删除。文件夹必须属于当前用户（不能放在 `Program Files` 下）：要么解压在用户目录中，要么由安装程序按用户安装（`%LOCALAPPDATA%\Programs`）。MSIX 包则通过 Microsoft Store 或应用安装程序（App Installer）更新。
- Linux：在 tar 包解压到的前缀（例如 `~/.local`）中，只替换应用自己的文件（`bin/notes`、`share/notes/`、desktop 文件、图标、metainfo 和 MIME 类型），每个文件都通过重命名替换，然后刷新桌面缓存（`update-desktop-database`、`update-mime-database`）。Flatpak、Snap、发行版的软件包或 AppImage 则通过各自的渠道更新，这时应用不会自我更新。

## 嵌入现有应用

Asuka 应用可以嵌入用 Xcode 或 Android Studio 开发的应用中：某个功能用 Go 写一次，就能在原生应用里展示出来。（在 Web 上，构建出的应用运行在你自己页面中的某个元素里，见 [Web](web.md)。）

**iOS。** `asuka build --platform ios --xcframework` 会生成 `dist/AsukaApp.xcframework`，其中包含应用的 Go 代码和 UIKit 宿主，同时支持真机和模拟器，并附带 Swift 模块接口和应用的资源。把它添加到你的 target 中（Embed & Sign），然后像展示任何视图控制器一样展示这个 Asuka 应用：

```swift
import AsukaApp

present(AsukaViewController(), animated: true)
// 也可以 push 它、把它作为一个标签页，或者作为窗口的根视图控制器
```

使用了 `asuka/sdk/sqlite` 的应用，旁边还会生成 `dist/asqlite.xcframework`，也就是 SQLite 的库，同样需要嵌入（Embed & Sign）。Go 代码会从你应用的 `Frameworks` 中加载它；它带有自己的隐私清单。

**Android。** `asuka build --platform android --aar` 会在 `dist/maven` 中生成一个 Maven 仓库，其中包括：宿主库（含有每个 ABI 下应用的 Go 代码）、应用的资源，以及已声明好的 `AsukaActivity`，它的依赖（Material、AndroidX）都写在 POM 里；旁边还有应用用到的可选模块（`asuka/sdk/push`、`asuka/sdk/maps`），如果应用使用了 `asuka/sdk/sqlite`，还有每个 ABI 下的 SQLite 库。添加这个仓库和命令打印出的依赖，然后启动这个 activity：

```kotlin
// settings.gradle.kts
dependencyResolutionManagement { repositories { maven { url = uri("path/to/dist/maven") } } }
// app/build.gradle.kts
dependencies { implementation("dev.asuka.counter:counter:1.0.0") }
// 在任意位置
startActivity(Intent(this, dev.asuka.AsukaActivity::class.java))
```

嵌入的部分就是一个完整的 Asuka 应用，和独立构建时一样只挂载一次。`asuka.json` 中那些针对整个应用的设置（图标、URL scheme、分享目标、小组件、权限说明文字），需要由外层的原生应用在它自己的项目中设置。

## 崩溃报告的符号

原生崩溃的调用栈帧只是应用二进制中的地址；崩溃收集服务（或 App Store Connect、Google Play）要借助构建时的符号，才能把它们还原成函数名和行号。Go 自己的调用栈不需要额外的符号（符号表就在二进制里），所以 Go 的 panic 本身就是可读的。

**iOS。** `asuka build --platform ios` 编译宿主时会带上调试信息，同时保留 Go 代码的调试信息，并像 Xcode 归档那样，为每个二进制（应用本身、带分享面板时的 framework、各个扩展）生成 dSYM，然后剥离二进制中的符号。dSYM 放在 `.ipa`（或模拟器的 `.app`）旁边的 `dist/<Name>.dSYMs` 中，符号也打包进了 `.ipa`（`Symbols/`），因此 App Store Connect 和 Xcode 的 Organizer 显示崩溃时会自动完成符号化。SQLite 的库（`asqlite.framework`）发布时已剥离符号，也没有 dSYM，它的栈帧会显示为 `asq_call`。把 dSYM 上传到你使用的崩溃收集服务：

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes.dSYMs
# Crashlytics 的工具，随它的 SDK 提供：
upload-symbols -gsp GoogleService-Info.plist -p ios dist/Notes.dSYMs
```

**Android。** `asuka build --platform android` 会为 Gradle 保留 Go 库的符号表。Gradle 打包时会剥离这些库的符号，并把符号连同 R8 对 Kotlin 代码的重命名映射一起放进 `.aab`。Google Play 会借助它们符号化原生崩溃和 ANR，并对调用栈进行反混淆。如果要上传到崩溃收集服务，`.aab` 旁边也各有一份：`dist/<Name>-native-debug-symbols.zip` 和 `dist/<Name>-mapping.txt`。

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes-native-debug-symbols.zip
sentry-cli upload-proguard --org ORG --project PROJECT dist/Notes-mapping.txt
```

**Mac、Windows、Linux 和 Web。** 应用是一个单独的 Go 二进制，构建时去掉了符号表和调试信息（`-s -w`）。不过 Go 的调用栈仍然保留函数名和行号，所以 panic 照样可读，也没有需要上传的符号。

请保留每个发布版本的符号，因为崩溃只能用它所在构建的符号来符号化。崩溃报告本身的说明见[设备](device.md#崩溃报告asukasdkcrash)。

## 发布之前

正式发布之前，请先在用户实际使用的环境里运行一遍发布构建：

- 在手机上：iOS 用 `--distribution development` 构建；Android 用 Play 内部测试轨道上的构建（`asuka publish --platform android --track internal`），这样安装的就是 Play 根据 `.aab` 生成的 APK。TestFlight 和 Play 内部测试还能发现模拟器上发现不了的问题。
- 在 Mac 上：在另一台 Mac 上下载签名并公证过的 `.app` 来运行（Gatekeeper 检查的正是下载来的文件）；`spctl --assess --type execute -vv Notes.app` 可以告诉你 Gatekeeper 会如何判定它。
- 在 Windows 和 Linux 上：在一台从未装过这个应用的机器上运行文件夹，或者 `.tar.gz`、Flatpak（Linux 需要 GTK 4.14 和 libadwaita 1.5 或更高版本）。
- 在 Web 上：通过 HTTPS 提供构建好的网站，并在你支持的每个浏览器中测试。

## 在 CI 中

下面这个 GitHub Actions 工作流会在你推送 `v1.2` 这样的标签时构建两个应用：用 `asuka publish` 把 iOS 构建上传到 TestFlight，把 Android 构建上传到 Play 的内部测试轨道；每个构建一上传完，就把 `.ipa` 或 `.aab` 附加到该标签对应的 GitHub Release 草稿上。等你准备好了，再发布这个草稿。（手动运行时，构建只会作为这次运行的 artifact 保存。）这个工作流假定应用位于仓库的根目录。第一次上传请手动完成：应用在 Play 上还没有任何上传记录时，Play 不接受通过 API 上传。

```yaml
# .github/workflows/release.yml
name: release
on:
  push:
    tags: ["v*"]
  workflow_dispatch:
permissions:
  contents: write # 用于把构建附加到标签对应的 Release

jobs:
  release:
    if: startsWith(github.ref, 'refs/tags/')
    runs-on: ubuntu-latest
    steps:
      - run: gh release create "$GITHUB_REF_NAME" --draft --generate-notes --verify-tag
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}

  ios:
    needs: release
    if: ${{ !failure() && !cancelled() }} # 手动运行时也会执行（这时 release 任务会被跳过）
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the distribution certificate and the profile
        env:
          CERTIFICATE: ${{ secrets.IOS_CERTIFICATE }}
          CERTIFICATE_PASSWORD: ${{ secrets.IOS_CERTIFICATE_PASSWORD }}
          PROFILE: ${{ secrets.IOS_PROFILE }}
        run: |
          KC=$RUNNER_TEMP/signing.keychain-db
          PW=$(openssl rand -hex 16)
          security create-keychain -p "$PW" "$KC"
          security set-keychain-settings -lut 3600 "$KC"
          security unlock-keychain -p "$PW" "$KC"
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/certificate.p12
          security import $RUNNER_TEMP/certificate.p12 -k "$KC" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
          security set-key-partition-list -S apple-tool:,apple: -k "$PW" "$KC"
          security list-keychains -d user -s "$KC" $(security list-keychains -d user | tr -d '"')
          echo "$PROFILE" | base64 --decode > $RUNNER_TEMP/app.mobileprovision
          # 如果设置了 shareTarget，还要把扩展的描述文件安装到 asuka 查找的位置：
          # mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
          # echo "$SHARE_PROFILE" | base64 --decode > ~/Library/MobileDevice/Provisioning\ Profiles/share.mobileprovision
      - name: build, and upload to TestFlight
        env:
          ASUKA_ASC_KEY_ID: ${{ secrets.ASC_KEY_ID }}
          ASUKA_ASC_ISSUER_ID: ${{ secrets.ASC_ISSUER_ID }}
          ASUKA_ASC_KEY: ${{ secrets.ASC_KEY }}
        run: asuka publish --platform ios --profile $RUNNER_TEMP/app.mobileprovision --no-wait
      - uses: actions/upload-artifact@v4
        with: { name: ios, path: dist/*.ipa }
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.ipa --clobber

  android:
    needs: release
    if: ${{ !failure() && !cancelled() }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-java@v4
        with: { distribution: temurin, java-version: 17 }
      - uses: android-actions/setup-android@v3
        with: { packages: platform-tools }
      - name: an NDK
        run: sdkmanager "ndk;27.1.12297006"
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the upload key
        env:
          KEYSTORE: ${{ secrets.ANDROID_KEYSTORE }}
        run: echo "$KEYSTORE" | base64 --decode > $RUNNER_TEMP/upload.jks
      - name: build, and upload to Play's internal test track
        run: asuka publish --platform android --track internal
        env:
          ASUKA_KEYSTORE: ${{ runner.temp }}/upload.jks
          ASUKA_KEYSTORE_PASSWORD: ${{ secrets.ANDROID_KEYSTORE_PASSWORD }}
          ASUKA_PLAY_SERVICE_ACCOUNT: ${{ secrets.PLAY_SERVICE_ACCOUNT }}
      - uses: actions/upload-artifact@v4
        with:
          name: android
          path: dist/*.aab
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.aab --clobber
```

这个工作流需要以下 secrets（在 Settings › Secrets and variables › Actions 中添加），文件要先转成 base64（`base64 -i file | pbcopy`）：

- `IOS_CERTIFICATE`：你的 Apple Distribution 证书及其私钥，即从“钥匙串访问”中导出的 `.p12`；`IOS_CERTIFICATE_PASSWORD` 是导出时设置的密码。
- `IOS_PROFILE`：应用的 App Store 描述文件，从 developer.apple.com 下载（如果设置了 `shareTarget`，`SHARE_PROFILE` 是分享扩展的描述文件）。
- `ASC_KEY_ID`、`ASC_ISSUER_ID` 和 `ASC_KEY`（`.p8` 文件的内容）：一个具有 App Manager 角色的 App Store Connect API 密钥，在 Users and Access › Integrations 中创建。
- `ANDROID_KEYSTORE`：上传密钥的 `.jks` 文件；`ANDROID_KEYSTORE_PASSWORD` 是它的密码（如果密钥的密码与之不同，再加上 `ASUKA_KEY_PASSWORD`）。
- `PLAY_SERVICE_ACCOUNT`：Google Cloud 服务账号的 JSON 密钥，该账号需要在 Play Console 的 Users and permissions 中受邀，并拥有发布到测试轨道的权限。

CLI 内置了 iOS 和 Android 宿主，所以安装的 CLI 版本必须与应用 `go.mod` 中的 Asuka 版本一致。每个构建的编号都是商店中现有最大编号加一，所以打标签之前不需要手动递增任何东西；`--no-wait` 让 iOS 任务在 Apple 收到上传后就结束，而不必等到处理完成。App Store 只接受用较新版本 Xcode 构建的包；如果 runner 默认的 Xcode 太旧，可以用 `sudo xcode-select -s /Applications/Xcode_<version>.app` 切换版本。

### 桌面端

把下面这些任务加到同一个工作流中，就能为该标签构建桌面应用，并附加到它的 Release 草稿上：Mac 版用你的 Developer ID 签名并公证；Windows 版构建 x64 和 Arm 两种架构并签名；Linux 版构建 amd64 和 arm64 两种架构。Windows 和 Linux 版都在 Linux 上构建，所以一个 Ubuntu runner 就能全部完成。（这里的应用名为 Notes，产物的文件名取自 `asuka.json` 中的 `name`。）

```yaml
  mac:
    needs: release
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the Developer ID certificate
        env:
          CERTIFICATE: ${{ secrets.MAC_CERTIFICATE }}
          CERTIFICATE_PASSWORD: ${{ secrets.MAC_CERTIFICATE_PASSWORD }}
        run: |
          KC=$RUNNER_TEMP/signing.keychain-db
          PW=$(openssl rand -hex 16)
          security create-keychain -p "$PW" "$KC"
          security set-keychain-settings -lut 3600 "$KC"
          security unlock-keychain -p "$PW" "$KC"
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/certificate.p12
          security import $RUNNER_TEMP/certificate.p12 -k "$KC" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
          security set-key-partition-list -S apple-tool:,apple: -k "$PW" "$KC"
          security list-keychains -d user -s "$KC" $(security list-keychains -d user | tr -d '"')
      - name: build, sign and notarize
        env:
          ASUKA_APPLE_ID: ${{ secrets.APPLE_ID }}
          ASUKA_APPLE_PASSWORD: ${{ secrets.APPLE_APP_PASSWORD }}
          ASUKA_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
        run: asuka build --platform mac --sign "Developer ID Application: Your Name (TEAMID)" --notarize env
      - name: attach to the tag's release
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: |
          ditto -c -k --keepParent dist/Notes.app dist/Notes-mac.zip
          gh release upload "$GITHUB_REF_NAME" dist/Notes-mac.zip --clobber

  windows-linux:
    needs: release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: osslsigncode, and the code signing certificate
        env:
          CERTIFICATE: ${{ secrets.WINDOWS_CERTIFICATE }}
        run: |
          sudo apt-get install -y osslsigncode
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/cert.pfx
      - name: Windows, x64 and Arm
        env:
          ASUKA_WINDOWS_CERT: ${{ runner.temp }}/cert.pfx
          ASUKA_WINDOWS_CERT_PASSWORD: ${{ secrets.WINDOWS_CERTIFICATE_PASSWORD }}
        run: |
          asuka build --platform windows -o dist/x64
          asuka build --platform windows --arch arm64 -o dist/arm64
          (cd dist/x64 && zip -r ../Notes-windows-x64.zip Notes)
          (cd dist/arm64 && zip -r ../Notes-windows-arm64.zip Notes)
      - name: Linux, amd64 and arm64
        run: |
          asuka build --platform linux
          asuka build --platform linux --arch arm64
      - name: attach to the tag's release
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.zip dist/linux/*.tar.gz --clobber
```

它们需要的 secrets：`MAC_CERTIFICATE` 是你的 Developer ID Application 证书及其私钥（一个 `.p12`），`MAC_CERTIFICATE_PASSWORD` 是它的密码；`APPLE_ID`、`APPLE_APP_PASSWORD`（App 专用密码，在 account.apple.com 生成）和 `APPLE_TEAM_ID` 供公证服务使用；`WINDOWS_CERTIFICATE` 是代码签名证书（一个 `.pfx`），`WINDOWS_CERTIFICATE_PASSWORD` 是它的密码。每次 Linux 构建都会生成各自的 `.tar.gz`，而 Flatpak 清单只打包最后构建的那一个，因此只对应那一个架构。

如果[更新](#自动更新)发布在 GitHub 上，把 `asuka build` 换成 `asuka publish`，就会同时把每个平台的更新上传到该标签的 Release 草稿（由 `release` 任务创建），等所有平台都上传完再发布。具体做法是：把私钥添加为 secret（`gh secret set ASUKA_UPDATE_PRIVATE_KEY < asuka-update.key`），为这些任务提供 `ASUKA_UPDATE_PRIVATE_KEY` 和 `GH_TOKEN`，并按标签上的版本号构建：

```yaml
      - name: build, sign, notarize, and upload with its update
        env:
          ASUKA_UPDATE_PRIVATE_KEY: ${{ secrets.ASUKA_UPDATE_PRIVATE_KEY }}
          GH_TOKEN: ${{ github.token }}
          ASUKA_APPLE_ID: ${{ secrets.APPLE_ID }}
          ASUKA_APPLE_PASSWORD: ${{ secrets.APPLE_APP_PASSWORD }}
          ASUKA_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
        run: asuka publish --platform mac --draft --version "${GITHUB_REF_NAME#v}" --sign "Developer ID Application: Your Name (TEAMID)" --notarize env
```

Windows 和 Linux 的任务中则改为：

```yaml
        run: |
          for p in "windows" "windows --arch arm64" "linux" "linux --arch arm64"; do
            asuka publish --platform $p --draft --version "${GITHUB_REF_NAME#v}"
          done
```

最后发布草稿（运行 `gh release edit v1.2.0 --draft=false`，或者在 Release 页面上操作），已安装的应用就会更新到这个版本。release 任务使用的标签由 `tagPrefix` 加版本号组成（`v1.2.0`）；`--version` 让构建的版本与标签保持一致，所以不必去改 `asuka.json` 里的版本号。增量包以已发布的 Release 为基准生成，runner 会下载它（`GH_TOKEN` 可以放宽 GitHub API 的请求频率限制，使用 `tagPrefix` 时会用到这个 API）。

## 多语言

使用了[翻译目录](i18n.md)的应用不需要额外配置：`asuka build` 会向各个应用商店和系统声明应用支持的语言（iOS 和 macOS 上是 `CFBundleLocalizations` 和 `.lproj`，Android 上是 `locales_config`），这样用户就能在系统设置中为应用单独选择语言。Windows、Linux 和 Web 构建不声明语言：应用会根据用户的设置，从翻译目录中挑选语言来显示。应用名称和用途说明也可以翻译，只要在翻译目录中使用 `asuka.appName` 和 `asuka.NSCameraUsageDescription` 这样的键即可（见[打包](i18n.md#系统显示的内容)）。

# 翻译说明

`docs/zh` 是 `docs` 的简体中文版，网站上发布在 `/zh/docs`。本页写给翻译和维护中文文档的人，不会发布到网站。英文页面改动时，同步更新对应的中文页面，标题保持一一对应。

## 原则

**写给中文读者，而不是逐句转写英文。** 英文原文的句式很有特点：大量逗号串联、"the X of Y"、省略主语、名词化。照搬这种结构写出来的中文读起来像机翻。翻译时先读懂一整段在说什么，再用一位熟悉 Go 和移动开发的中文工程师会写的方式重新写出来。可以拆句、合句、调整语序、补上主语或连接词（“因此”“也就是说”“不过”），也可以删掉中文里显得多余的指代。不要增删事实，不要漏掉任何细节、限制条件或例外。

参考的语感：Vue、React（react.dev/zh-hans）、Go 语言中文文档这类社区维护得好的中文技术文档。语气平实、准确、口语化但不随意；称读者为“你”；不用“您”；不用网络流行语。

## 格式

- **结构一一对应**：标题的数量、层级、顺序必须与英文版完全一致（`#`、`##`、`###` ……一个不多一个不少）。网站和链接锚点依赖这一点。标题本身要译成自然的中文。
- 代码块里的代码保持原样；代码里的**注释**译成中文（命令行注释 `# ...`、Go 的 `// ...` 都译），但字符串字面量、标识符、命令、路径、参数不动。
- 行内代码（`` `ui.Button` ``）、API 名、类型名、文件名、命令、环境变量保持英文。
- 链接：文档页之间的链接指向中文页面，锚点用中文标题生成的锚点（GitHub 的规则：小写，去掉标点，空格换成 `-`，如 `parity.md#数据与服务`）。指向仓库其他文件的链接相对于 `docs/zh/`，比英文版多一层 `../`（`../../examples/gallery`）；只有英文版的页面用 `../`（`../device-checklist.md`）。外部链接保持原样。
- `<!-- repository-only:start -->` / `<!-- repository-only:end -->` 这类 HTML 注释原样保留，其中的内容照样翻译。
- 表格保持列数不变，单元格内容翻译。
- 中英文之间、中文与数字之间加一个空格（“在 iOS 16 上”，"用 `asuka dev` 运行"）；行内代码两侧与中文之间也加空格。
- 使用全角中文标点（，。：；？！""（））；但括号内全是英文/代码时可用半角括号加空格。列表项末尾按中文习惯用句号或不用标点，同一列表保持一致。
- 每行不必硬折行；一个段落写成一行即可（英文原文的 72 列折行不需要保留）。
- 粗体、斜体、引用块照原文位置保留。

## 术语表

| 英文 | 中文 | 说明 |
| --- | --- | --- |
| widget | 控件 | "the platform's own widgets" → “平台原生控件” |
| native | 原生 | |
| component | 组件 | |
| view | 视图 | |
| host | 宿主 | 首次出现可写“宿主（host）” |
| core | 核心 | Asuka 的核心 |
| signal | signal | 保留英文，不译；标题里单独出现时首字母大写（`## Signal`）。操作系统和 GTK 的 signal（崩溃时收到的信号、GTK 信号）不是这个概念，照常译作“信号” |
| effect | effect | 保留英文，如“一个 effect” |
| owner | owner | 保留英文；首次出现可解释为“所有者” |
| accessor | 访问器 | |
| props | 属性 / props | 指 Go 结构体字段时用“属性”，指 `XxxProps` 类型时保留英文 |
| style | 样式 | |
| layout | 布局 | |
| flexbox, grid | flexbox、grid | 保留英文 |
| safe area | 安全区域 | |
| screen | 屏幕 / 页面 | 导航栈里的 screen 译“页面” |
| stack (navigation) | 导航栈 | |
| tab | 标签页 | tab bar → 标签栏 |
| sheet | 弹出面板（sheet） | iOS 的 sheet，可首次注明 |
| alert | 警告框 | |
| menu bar | 菜单栏 | |
| toolbar | 工具栏 | |
| main thread | 主线程 | |
| batch | 批次 / 一批 | |
| flush | 刷新（flush） | |
| op | 操作（op） | ops 指协议里的操作时可保留 ops |
| hot reload / reload on save | 热重载 / 保存即重载 | |
| simulator | iOS 模拟器 | 指 Xcode 的 Simulator |
| emulator | Android 模拟器 | 指 Android Emulator；两者都叫模拟器，需要时加平台名区分 |
| player (app) | 播放器应用 | Asuka 的 player，开发时装在设备上的那个 App |
| release build | 发布构建 | |
| sign / signed | 签名 | |
| notarize | 公证 | |
| bundle ID / application ID | bundle ID / application ID | 保留英文 |
| native package | 原生包 | |
| plugin | 插件 | |
| accessibility | 无障碍 | screen reader → 屏幕阅读器 |
| locale | 区域设置 / 语言区域 | |
| right-to-left | 从右到左 | |
| animation | 动画 | spring → 弹簧动画；transition → 过渡；shared element → 共享元素 |
| gesture | 手势 | |
| haptics | 触感反馈 | |
| tray icon | 托盘图标 | |
| Dock | 程序坞（Dock） | |
| notification | 通知 | push → 推送 |
| store (App Store / Google Play) | 应用商店 | |
| the web | Web | "on the web" → “在 Web 上” / “在浏览器中” |
| the desktop | 桌面端 | |
| phone | 手机 | |
| Mac / PC | Mac / PC | 保留 |
| app | 应用 / App | 一般用“应用”；“the player app”等可用 App |

产品名、框架名（UIKit、AppKit、Material 3、GTK、libadwaita、WebView2、SwiftUI、Expo、React Native ……）一律保留英文。

## 示例

> Asuka makes apps in Go with each platform's own widgets, on iOS, Android, macOS, Windows, Linux and the web.

不好：Asuka 用 Go 制作带有每个平台自己的控件的应用，在 iOS、Android、macOS、Windows、Linux 和 web 上。

好：Asuka 让你用 Go 编写应用，并在 iOS、Android、macOS、Windows、Linux 和 Web 上以各平台的原生控件呈现。

> Save a Go file and the app reloads in a moment, the values of its signals kept.

不好：保存一个 Go 文件，应用在一会儿内重新加载，它的信号的值被保留。

好：保存 Go 文件后，应用片刻间就会重新加载，signal 里的值都会保留下来。

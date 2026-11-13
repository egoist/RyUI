# Asuka 与 MyGo

[MyGo](https://mygo.run) 是一个用 Go 开发桌面应用的框架。两者都只用 Go，调用桌面系统时也都不需要 cgo。它们的区别在于应用能运行在哪里，更重要的是由谁来呈现界面：MyGo 自己绘制界面，或者显示一个网页；Asuka 则直接使用平台原生控件。

## 由谁呈现界面

MyGo 的窗口里显示的是以下两者之一：

- **MyGo 自己绘制的界面**：在 GPU 上绘制（Mac 上用 Metal，Windows 上用 Direct3D 11，此外还有 OpenGL），外观由 MyGo 的主题决定。文字由系统的文本引擎渲染，MyGo 还会为它绘制的内容构建一棵无障碍树。
- **一个网页**：显示在系统的 Web 视图中（WKWebView、WebView2、WebKitGTK），网页通过 MyGo 生成的 TypeScript 客户端调用应用的 Go 代码。

Asuka 应用显示的是平台原生控件。一个 `ui.Button`，在 iOS 上是 `UIButton`，在 Android 上是 `MaterialButton`，在 Mac 上是 `NSButton`，在 Windows 上是绘制成 Windows 11 样式的 Win32 `BUTTON`，在 Linux 上是 `GtkButton`，在浏览器中则是 `<button>`；完整列表见[组件](components.md)。Asuka 负责布局（所有平台共用同一个 Go 实现的布局引擎）并设置控件的属性，绘制则交给平台完成。这些控件的行为和平台自己的控件完全一样：文本输入和自动纠错、输入法、屏幕阅读器、菜单、滚动和焦点，都是平台原生的。

由此带来的差异有：

- **外观。** 不设置任何样式的 Asuka 应用，看起来就和各平台上的其他应用一样，而且会随平台的变化而变化（新版 iOS 换了按钮样式，新版 libadwaita 换了开关样式，应用也会跟着变）。MyGo 的窗口则按 MyGo 主题的样子绘制，在每个系统上都一模一样。
- **文本、输入和无障碍。** 在 Asuka 中，这些都由平台自己完成；在 MyGo 中，则由 MyGo 在它绘制的内容之上自行实现。
- **没有对应控件的内容。** MyGo 可以像绘制其他界面一样绘制任何东西。Asuka 则为没有对应控件的内容提供了专门的包，借助平台的绘图能力来实现：`asuka/sdk/chart`、`asuka/sdk/svg`，以及用于着色器的 `ui.Shader`。

## 运行在哪里

| | MyGo | Asuka |
|---|---|---|
| 手机 | — | iOS 和 Android |
| 桌面 | Mac、Windows、Linux | Mac、Windows、Linux |
| 浏览器 | 它自绘的界面，绘制在使用 WebGL 2 的 canvas 上 | 页面的 DOM，由 WebAssembly 驱动（见 [Web](web.md)） |

## 视图如何更新

MyGo 的视图是应用状态的函数，每一帧都会根据状态重新构建。Asuka 的组件只运行一次：它创建视图并把它们和 signal 连接起来；signal 变化时，只有读取了它的视图会被更新，并在该帧的同一个批次中完成（见[核心概念](concepts.md)）。

## 如何选择

以下情况适合用 Asuka：

- 应用面向手机，或者想用一套代码同时覆盖手机、桌面和 Web；
- 应用应当在外观和行为上与各平台的其他应用保持一致：控件、文本输入、无障碍、导航都是平台原生的。

以下情况适合用 MyGo：

- 应用只面向桌面，而且界面是一个 Web 前端：已有的 Web 代码、HTML 和 CSS、富文档，或者只有浏览器才能提供的能力；
- 应用需要一套自己的外观，在每个桌面系统上都绘制得一模一样，而不是遵循各平台的风格：比如编辑器、检查器、游戏。

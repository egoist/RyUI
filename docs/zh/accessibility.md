# 无障碍

Asuka 显示的是平台原生控件，所以屏幕阅读器、键盘操作和文字大小设置所需的大部分支持，不用写代码就已经具备了。本页介绍哪些是自动获得的，以及剩下的部分要用到的属性：标签、角色、分组、自定义操作、可调节视图、播报、键盘、文字大小和减弱动态效果，最后讲讲如何测试。

每个宿主都会把同样的属性交给所在平台的无障碍 API，屏幕阅读器再从那里读取：

| 平台 | 无障碍 API | 屏幕阅读器 |
| --- | --- | --- |
| iOS | UIAccessibility | VoiceOver |
| Android | Android 无障碍框架（`AccessibilityNodeInfo`） | TalkBack |
| macOS | NSAccessibility | VoiceOver |
| Windows | UI Automation | 讲述人（Narrator）、NVDA |
| Linux | GTK 的无障碍支持，经由 AT-SPI | Orca |
| Web | 页面元素上的 ARIA 角色和属性 | 浏览器所用的屏幕阅读器：VoiceOver、NVDA、TalkBack…… |

## 开箱即得的支持

- **控件来自平台。** `Button`、`Toggle`、`Slider`、`TextField`、`Segmented`、`Stepper`、`DatePicker` 等组件，分别就是 UIKit、Material、AppKit、Win32、GTK 和浏览器自己的控件，朗读和操作方式与其他任何应用中一样。
- **可按压的视图就是按钮。** 设置了 `OnPress` 的 `View` 是一个独立的元素：
  - iOS：一个按钮，朗读内容是它里面的文本和带标签的图片拼接起来的结果；双击即可按下。
  - Android：一个可点击的视图，TalkBack 会把它里面的文本连在一起朗读。
  - macOS：一个按钮（`AXButton`）。
  - Windows：一个按钮，名称是它里面的文本拼接而成，屏幕阅读器可以按下它（UI Automation 的 Invoke）。
  - Linux：一个按钮。
  - Web：一个带 `button` 角色的元素。

  在 Windows、Linux 和 Web 上，键盘也能访问到它（见[键盘](#键盘)）。
- **文字会缩放。** 所有字体，包括语义字体（`fonts.Body`、`fonts.Headline`……）、固定字号（`fonts.System(15)`）和你自己的字体，都会跟随用户设置的文字大小：动态字体（Dynamic Type）、Android 的字体缩放、Windows 的文本大小、GNOME 的文字缩放，以及浏览器的文字大小。用户修改设置后，Asuka 会重新测量文字，并重新布局受影响的部分。Mac 没有文字大小设置。
- **颜色会自适应。** 语义颜色（`colors.Label`、`colors.SecondaryLabel`、`colors.Background`、`colors.Accent`……）都是平台自己的颜色（见[布局与样式](layout.md)）：它们会跟随深色模式，在 iOS 和 macOS 上跟随“增强对比度”，在 Linux 上跟随 GNOME 的“高对比度”。
- **只用于布局的视图不会碍事。** 没有事件、也没有无障碍属性的 `View`、`Row` 或 `Column` 不会创建原生视图，因此永远不会出现在无障碍树中。
- **图片在加上描述之前保持沉默。** 没有 `Label` 的 `Image`，以及 `Symbol` 和 `Shape`，对屏幕阅读器都是隐藏的。在 Linux 和 Web 上有一个例外：常见的符号即使没有标签，也会被读出一个英文名称（Back、Archive）。

## 标签与提示

`Label` 是屏幕阅读器为视图朗读的内容。如果光看标签还不清楚使用它会发生什么，就用 `Hint` 来说明。

```go
ui.Row(ui.Style{Gap: 12},
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled, OnPress: send}),
	ui.Image(ui.ImageProps{Source: ui.Val("https://picsum.photos/id/1015/600/400"), Label: ui.Val("A river in a valley"),
		Style: ui.Style{AspectRatio: 3.0 / 2}}),
	ui.View(ui.ViewProps{Label: ui.Val("Favorite"), Role: ui.RoleImage},
		ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: colors.Red})),
)
```

- `Button` 会读出自己的标题。只有图标的按钮要设置 `Label`：平台自己只能为少数符号命名，Android 更是只有英文名称。
- `Image` 的 `Label` 用来描述图片；没有它，图片就被视为装饰。[`asuka/sdk/svg`](../../sdk/svg) 里的图标也同样接受 `Label`。
- `Symbol` 本身没有标签：如果它有实际含义，就把它包进一个带 `Label` 和 `ui.RoleImage` 的 `View`。
- `Hint` 在 iOS 上是无障碍提示，会在停顿片刻后朗读（用户可以关闭提示）；在 Android 上是状态描述，会和标签一起朗读。在 macOS 上它是视图的帮助信息（`accessibilityHelp`），在 Windows 上是帮助文本，在 Linux 上是无障碍描述，在 Web 上是 `aria-description`。尽量简短，比如“打开这条消息”。

`Label`、`Hint`、`Role` 和 `Decorative` 只在视图创建时设置一次。如果标签需要变化，那它就得是另一个视图：在 `Show` 或 `Dynamic` 的分支里创建它；或者不给可按压视图设置 `Label`，让屏幕阅读器朗读它里面会变化的文本。

## 角色

`Role` 说明一个视图是什么：

| 角色 | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| `ui.RoleNone` | 无（默认） | 无 | 无 | 无 | 无 | 无 |
| `ui.RoleButton` | button trait | 读作按钮 | `AXButton` | Button | `button` | `button` |
| `ui.RoleLink` | link trait | 读作按钮 | `AXLink` | Hyperlink | `link` | `link` |
| `ui.RoleHeader` | header trait | 标题 | `AXHeading` | Text，标题级别 1 | `heading` | `heading` |
| `ui.RoleImage` | image trait | 读作图片 | `AXImage` | Image | `img` | `img` |
| `ui.RoleText` | static text trait | 无 | `AXStaticText` | Text | `label` | 无 |
| `ui.RoleSearch` | search field trait | 无 | `AXTextField` | Edit | `search` | `searchbox` |

Windows 一列是 UI Automation 的控件类型，Linux 一列是 GTK 的无障碍角色，Web 一列是 ARIA 角色。可按压的 `View` 本来就是按钮。在 iOS 和 macOS 上，设置了 `Role` 的视图会成为一个独立的元素，所以也要给它加上 `Label`，否则屏幕阅读器只会读出它的角色。

## 分组

屏幕阅读器是逐个元素移动的。一行里有三段文本，就要停三次；而很多时候，这一行应该只停一次。有两种办法：

- **让它可按压。** 带 `OnPress` 的 `View` 是一个元素，朗读的是它里面的内容。常见的列表行就是这样：

```go
func Row(m message, open func()) ui.Node {
	return ui.View(ui.ViewProps{
		OnPress: open,
		Hint:    ui.Val("Opens the message"),
		Style:   ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.Symmetric(12, 20)},
	},
		ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: colors.SecondaryLabel})),
		ui.Column(ui.Style{Grow: 1, Gap: 2},
			ui.StaticText(m.from, ui.Style{Font: fonts.Headline}),
			ui.StaticText(m.subject, ui.Style{Color: colors.SecondaryLabel}),
		),
	)
}
```

- **给它一个 `Label`。** 带 `Label` 的 `View` 会被读作它的标签。在 iOS 和 macOS 上，它里面的内容不会再单独朗读；在 Android 上，标签就是视图的内容描述（content description）；在 Windows 上是视图的名称，在 Linux 上是无障碍标签，在 Web 上是 `aria-label`。用文字说出眼睛一眼能看到的信息，比如写“Night Drive by Ada, 4 minutes”，而不是“4:00”。

```go
ui.View(ui.ViewProps{Label: fmt.Sprintf("%s by %s, %d minutes", title, artist, minutes),
	Style: ui.Style{Direction: ui.RowDir, Gap: 8}},
	ui.StaticText(title, ui.Style{Font: fonts.Headline}),
	ui.StaticText(artist, ui.Style{Color: colors.SecondaryLabel}),
	ui.StaticText(fmt.Sprintf("%d:00", minutes), ui.Style{Color: colors.SecondaryLabel}),
)
```

## 装饰性内容

`Decorative` 会让视图及其中的所有内容对屏幕阅读器隐藏。适用于纯装饰、旁边文字已经说明了含义的图标，或者一个写着“已保存”的 toast 里的对勾。

```go
ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Green}))
```

## 标题

屏幕阅读器用户经常在标题之间跳转（VoiceOver 的转子、TalkBack 的按标题导航、NVDA 和 Orca 的 H 键、浏览器屏幕阅读器的标题列表）。所以请把每个区块的标题标记出来：用设置了 `Role: ui.RoleHeader` 的 `RichText`，或者带 `Label` 和该角色的 `View`。`Text` 不接受角色。[`asuka/sdk/markdown`](../../sdk/markdown) 已经替你标记好了它的标题。

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Recent")}), Role: ui.RoleHeader,
	Style: ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}}),
ui.View(ui.ViewProps{Label: ui.Val("Settings"), Role: ui.RoleHeader},
	ui.StaticText("Settings", ui.Style{Font: fonts.Title2})),
```

`RichText` 里的链接各自是独立的元素，屏幕阅读器可以找到并激活它们。

## 自定义操作

明眼用户通过轻扫、长按或行内的小按钮完成的事情，屏幕阅读器用户要通过“操作”来完成：在 VoiceOver 中是转子里的“操作”，上下轻扫选择；在 TalkBack 中是操作菜单。`Actions` 用来在视图上列出这些操作：

```go
ui.View(ui.ViewProps{
	Actions: []ui.A11yAction{{Name: ui.Val("Reply"), Do: reply}, {Name: ui.Val("Archive"), Do: archive}},
	Style:   ui.Style{Padding: ui.Symmetric(12, 20)},
}, ui.Text(subject))
```

带操作的视图是一个元素，朗读的是它里面的文本。这些操作出现在哪里：

- **iOS** 和 **macOS**：成为视图的自定义操作（`UIAccessibilityCustomAction`、`NSAccessibilityCustomAction`）。在 iOS 上出现在 VoiceOver 转子的“操作”中，在 Mac 上出现在“操作”菜单里（VO-Command-空格键）。
- **Android**：出现在 TalkBack 的操作菜单中。
- **Windows**：成为视图的上下文菜单，键盘（Shift+F10、菜单键）和鼠标右键都能打开它。视图会成为一个 Tab 停靠点，键盘因此可以访问到它。
- **Linux**：通过 AT-SPI 成为视图的操作，Orca 可以列出并执行它们；同时也会成为视图的上下文菜单（辅助点击、菜单键、Shift+F10）。视图会成为一个 Tab 停靠点。
- **Web**：ARIA 没有自定义操作，所以 Web 上不会提供它们。应用在浏览器中运行时，请用别的方式（一个按钮、一个 `Menu`）也提供同样的命令。

`List` 的 `SwipeActions` 会自动提供给屏幕阅读器：UIKit 的列表本身就会把轻扫操作交给 VoiceOver；在 Android 上，Asuka 会把它们作为 TalkBack 操作加到列表行上。不要在 `Actions` 里重复列出它们。

```go
ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
		}
	},
}, items, key, row)
```

`Menu` 的操作以及带 `Menu` 的按钮，用的都是平台自己的菜单，本身就是无障碍的。

## 可调节的视图

`OnAdjust` 让一个视图像滑块一样可以调节：VoiceOver 的上下轻扫，以及 TalkBack 的同类手势（或音量键），会让它增加 1 或减少 1。如果你自己做了一个用于选值的控件，就用它；`Slider` 和 `Stepper` 本来就是可调节的。

在 Mac 上，这个视图是一个增量器（`AXIncrementor`），VoiceOver 用 VO-上箭头和 VO-下箭头来调节它。在 Windows、Linux 和 Web 上，它是一个 Tab 停靠点，用方向键调节：上和右加 1，下和左减 1（在 Windows 和 Linux 上，如果是从右到左的语言，左右会对调）。Linux 和 Web 会给它 slider 角色。

```go
func Rating() ui.Node {
	stars := signal.New(3)
	var icons []ui.Node
	for i := range 5 {
		icons = append(icons, ui.Symbol(ui.IconStar.Filled(), ui.StyleFunc(func() ui.Style {
			if i < stars.Get() {
				return ui.Style{Color: colors.Orange}
			}
			return ui.Style{Color: colors.TertiaryLabel}
		})))
	}
	return ui.View(ui.ViewProps{
		OnAdjust: func(by int) { stars.Set(min(max(stars.Peek()+by, 0), 5)) },
		Style:    ui.Style{Direction: ui.RowDir, Gap: 4, Align: ui.AlignCenter},
	}, append(icons, ui.Text(signal.Func(func() string { return fmt.Sprintf("Rating: %d of 5", stars.Get()) })))...)
}
```

这个视图读出的是它的文本：文本说明了当前的值，并随着调节而变化。它没有设置 `Label`，因为标签会代替文本被朗读，而且无法变化。数值要自己限定在范围内：屏幕阅读器不会替你停下，会一直请求调节。

## 播报

`ui.Announce` 会让屏幕阅读器（如果开着的话）朗读一段文本，用来告知焦点以外发生的变化，比如一条 toast、搜索结果的数量、保存完成。

```go
ui.Animate(func() { shown.Set(true) })
ui.Announce("Saved") // 不播报的话，屏幕阅读器不会注意到这条 toast
```

它在 iOS 上是 `UIAccessibility` 的播报，在 Android 上是 `announceForAccessibility`，在 macOS 上是一个播报请求，在 Windows 上是一条 UI Automation 通知，在 Linux 上是 GTK 的播报（GTK 4.14 及以上），在 Web 上是一个 polite 级别的 `aria-live` 区域。没有屏幕阅读器时，它什么也不做。获得焦点的视图已经在朗读的内容，就不要再播报了。

## 移动焦点

`ui.AccessibilityFocus(node)` 会把屏幕阅读器的焦点移到某个视图上。适用于屏幕内容在用户眼前变了、而重要的东西在别处的情况：表单提交失败后下面的错误信息、搜索的第一条结果、滑入的面板的标题。

```go
errText := ui.Text(message)
// ...
OnPress: func() {
	if err := signIn(); err != nil {
		message.Set(err.Error())
		ui.AccessibilityFocus(errText)
	}
},
```

它会随下一次刷新发出，那时视图已经存在并有了 frame：在 iOS 上是带着该视图的 `UIAccessibility` 屏幕变化通知，在 Android 上是 `ACTION_ACCESSIBILITY_FOCUS`，在 macOS 上是指明该视图的布局变化通知，在 Windows 上是焦点事件。如果目标是一个自身没有原生视图的容器，焦点会落到它的第一个视图上。在这四个平台上，没有屏幕阅读器时它什么也不做，而且永远不会移动键盘焦点（那是 `ui.Focus` 的事）。

在 Linux 和 Web 上，它会把键盘焦点移到该视图，屏幕阅读器随之跟过去：GTK 和网页没有别的办法让屏幕阅读器指向某个视图。

## 屏幕阅读器是否开启

屏幕阅读器开着时，`ui.UseEnvironment()` 的 `ScreenReader` 为 true，并会随着它的开启或关闭而变化：

- iOS 和 macOS：VoiceOver。
- Android：TalkBack。
- Windows：系统的屏幕阅读器标志，讲述人和其他屏幕阅读器会设置它。
- Linux：GNOME 的屏幕阅读器设置（Orca）。
- Web：始终为 false。浏览器不会告诉页面屏幕阅读器是否开着。

用它来处理那些“用耳朵听”时需要不同做法的地方，而不是用来隐藏内容。比如让 toast 一直显示到用户关闭为止，而不是三秒后消失；或者把自动翻页的轮播改成等待用户操作。

## 文字大小

文字无需写代码就会缩放，剩下要做的是让布局在大字号下依然好用：

- 不要固定承载文字的元素的高度。让它自己长高；需要最小高度时用 `MinHeight`。
- 重要的文字避免使用 `Lines: 1`：字号大了它就会被截断。
- 把很长的行放进 `Scroll` 或 `List`。

`ui.UseEnvironment()` 的 `FontScale` 是用户设置的文字大小，1 表示默认值：动态字体的“大”、Android 的默认字体缩放、Windows 的 100%（设置 > 辅助功能 > 文本大小）、GNOME 的缩放比例 1、浏览器的默认文字大小（16 个 CSS 像素）。在 Mac 上它始终为 1。可以在 `StyleFunc` 里读取它，在大字号下调整布局，就像 iOS 自带应用会把一行的标签和值上下堆叠起来那样：

```go
func Adaptive() ui.Node {
	env := ui.UseEnvironment()
	return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		if env.Get().FontScale > 1.5 {
			return ui.Style{Direction: ui.ColumnDir, Gap: 4}
		}
		return ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}
	})},
		ui.StaticText("Wi-Fi", ui.Style{Grow: 1}),
		ui.StaticText("Home", ui.Style{Color: colors.SecondaryLabel}),
	)
}
```

## 减弱动态效果

`ui.UseEnvironment()` 的 `ReduceMotion` 对应 iOS 和 macOS 的“减弱动态效果”、Android 的关闭动画、Windows 的关闭动画效果、GNOME 的关闭动画，或浏览器的 `prefers-reduced-motion`。在 iOS 上，Asuka 会让过渡改为溶解而不是移动；在其他平台上，它的动画会立即结束。还有哪些需要你自己处理，请看[动画与手势](motion.md)。

## 键盘

在 Windows、Linux 和 Web 上，Asuka 的视图会像平台原生控件一样响应键盘：

- **Tab** 在控件和可按压视图之间移动，顺序与视图的顺序一致；Shift+Tab 反向移动。
- **Enter** 和**空格键**会像按钮一样按下获得焦点的可按压视图。
- 获得焦点的视图会显示焦点环：Windows 上是 Fluent 风格的，Linux 上是强调色的轮廓，Web 上是浏览器自己的。
- 带 `Actions` 或 `OnAdjust` 的视图是一个 Tab 停靠点：按 Shift+F10 或菜单键可以打开它的上下文菜单（Windows、Linux），按方向键可以调节它。

`ui.Focus` 和 `ui.Blur` 用来把键盘焦点移到某个输入框上或从上面移开（见[组件](components.md)）。

## 用屏幕阅读器测试

打开屏幕阅读器，把每个页面都试一遍，如果可以的话，眼睛别看屏幕：是不是每样东西都能访问到？焦点每停一处，是否都说清了那是什么？每个轻扫和拖动是否都有对应的操作？在桌面端，再只用键盘试一遍。

- **VoiceOver** 在 iPhone 上运行：设置 > 辅助功能 > VoiceOver。可以把辅助功能快捷键（连按三次侧边按钮）设成开关 VoiceOver。iOS 模拟器里没有 VoiceOver；在模拟器上可以用 Xcode 的 Accessibility Inspector（Xcode > Open Developer Tool）查看每个元素的标签、trait 和操作。
- **TalkBack** 在 Android 手机上运行，也可以在带 Google Play 的 Android 模拟器上运行，它来自 Android 无障碍套件（Android Accessibility Suite）：设置 > 无障碍 > TalkBack。
- **Mac 上的 VoiceOver**：按 Command-F5，或者打开系统设置 > 辅助功能 > VoiceOver。Accessibility Inspector（Xcode > Open Developer Tool）可以直接检查 macOS 应用的元素。
- **Windows 上的讲述人**：Ctrl+Windows+Enter。NVDA 可以免费下载。Accessibility Insights for Windows 能显示每个元素的 UI Automation 名称、控件类型和模式（pattern）。
- **Linux 上的 Orca**：在 GNOME 中按 Super+Alt+S，或者打开设置 > 无障碍 > 屏幕阅读器。Accerciser 可以查看 AT-SPI 树。
- **Web**：浏览器的开发者工具可以查看无障碍树（Chrome 和 Firefox 的无障碍面板）；可以试试 VoiceOver 配 Safari，NVDA 配 Chrome 或 Firefox。
- **文字大小**：iOS 上是设置 > 辅助功能 > 显示与文字大小 > 更大字体，或者在 iOS 模拟器上用 Xcode 的 Environment Overrides；Android 上是设置 > 显示 > 字体大小；Windows 上是设置 > 辅助功能 > 文本大小；GNOME 上是设置 > 无障碍 > 大号文本（Large Text）；浏览器则用它自己的文字大小设置。请试试最大的那一档。
- **减弱动态效果**：iOS 上是设置 > 辅助功能 > 动态效果；Android 上是设置 > 无障碍 > 移除动画；macOS 上是系统设置 > 辅助功能 > 显示 > 减弱动态效果；Windows 上是设置 > 辅助功能 > 视觉效果 > 动画效果；GNOME 上是设置 > 无障碍 > 减少动画（Reduce Animation）；浏览器则跟随系统设置。

修改这些设置时不用重启 `asuka dev`：应用会随每项设置的变化即时更新。

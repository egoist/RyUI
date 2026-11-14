# 组件

组件就是一个返回 `ui.Node` 的 Go 函数。本页按用途分类介绍 `ui` 包提供的各种视图，以及与它配套、用于 Markdown 和 SVG 的几个包。只要平台自己有对应的控件，用的就是平台原生控件：iOS 上是 UIKit，Android 上是 Android Views 和 Material 3，macOS 上是 AppKit，Windows 上是绘制成 Windows 11（Fluent）风格的 Win32 控件，Linux 上是 GTK 4 和 libadwaita，浏览器里则是页面元素。视图如何确定尺寸和位置，请看[布局](layout.md)；页面、标签页和弹出面板（sheet）请看[导航](navigation.md)。

大多数会变化的属性都接受 `signal.Accessor`：可以传入一个 signal、一个 `signal.Func`，如果值永远不变，就用 `ui.Val(v)`。由用户修改的控件（输入框、开关）则接受 `*signal.Signal`，并由控件写入它。详见[核心概念](concepts.md)。

## 各平台的原生控件

| 组件 | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Text` | `UILabel` | `TextView` | `NSTextField`（作为标签） | 由宿主的视图绘制 | `GtkLabel` | 页面文本 |
| `Button` | `UIButton` | `MaterialButton` | `NSButton` | `BUTTON`，绘制成 Fluent 风格 | `GtkButton` | `<button>` |
| `TextField` | `UITextField`；多行时为 `UITextView` | `EditText` | `NSTextField`、`NSSecureTextField`；多行时为 `NSTextView` | `EDIT`，外框绘制成 Fluent 风格 | `GtkEntry`、`GtkPasswordEntry`；多行时为 `GtkTextView` | `<input>`；多行时为 `<textarea>` |
| `Toggle` | `UISwitch` | `MaterialSwitch` | `NSSwitch` | 绘制成拨动开关的 `BUTTON` 复选框 | `GtkSwitch` | `<input type="checkbox" switch>` |
| `Slider` | `UISlider` | Material `Slider` | `NSSlider` | trackbar 控件 | `GtkScale` | `<input type="range">` |
| `Segmented` | `UISegmentedControl` | Material 分段按钮 | `NSSegmentedControl` | 自行绘制，仿照 Windows Community Toolkit 的样式 | 相连的一组 `GtkToggleButton` | 单选组中的一组按钮 |
| `Stepper` | `UIStepper` | 两个 Material 图标按钮 | `NSStepper` | 两个按钮 | 相连的 − 和 + 按钮 | 两个按钮 |
| `Wheel` | `UIPickerView` | `NumberPicker` | `NSPopUpButton` | 下拉式 `COMBOBOX` | `GtkDropDown` | `<select>` |
| `ColorPicker` | `UIColorWell` | 色块加 Material 对话框 | `NSColorWell` 和颜色面板 | 色块加颜色对话框（`ChooseColor`） | `GtkColorDialogButton` | `<input type="color">` |
| `DatePicker` | `UIDatePicker` | 打开 Material 选择器的按钮 | `NSDatePicker` | 日期时间选择器（`SysDateTimePick32`） | 一个按钮，打开 `GtkCalendar` 和微调按钮 | `<input type="date">`、`time`、`datetime-local` |
| `Progress` | `UIProgressView` | `LinearProgressIndicator` | `NSProgressIndicator` | 绘制成 Fluent 风格 | `GtkProgressBar` | `<progress>` |
| `ProgressRing` | 仿照 SwiftUI 的圆形仪表绘制；没有值时是 `UIActivityIndicatorView` | `CircularProgressIndicator` | 16 和 32 点时是旋转样式的 `NSProgressIndicator`；其他尺寸或设置了 `Color` 时，按它的样式自行绘制 | 自行绘制的 Fluent `ProgressRing` | 按 GNOME 的样式绘制；没有值时是 `AdwSpinner` | SVG 圆环 |
| `Activity` | `UIActivityIndicatorView` | `CircularProgressIndicator` | 旋转样式的 `NSProgressIndicator` | 自行绘制的 Fluent 进度环 | `AdwSpinner`（libadwaita 1.6 之前为 `GtkSpinner`） | 自行绘制的加载指示器 |
| `Image` | `UIImageView` | `ImageView` | `NSImageView` | 用 GDI+ 绘制 | 用 GDK 纹理绘制 | `<img>` |
| `lottie.View` | lottie-ios | lottie-android | lottie-ios | 用 `ui.Canvas` 绘制 | 用 `ui.Canvas` 绘制 | lottie-web |
| `Symbol` | SF Symbols | Material Symbols | SF Symbols | Segoe Fluent Icons | 图标主题中的图标，没有则用 Material Symbols | Material Symbols |
| `Video` | `AVPlayerViewController` | Media3 的播放器 | `AVPlayerView` | WebView2 中的 `<video>` | `GtkVideo`（GStreamer） | `<video>` |
| `WebView` | `WKWebView` | `WebView` | `WKWebView` | WebView2 | `WebKitWebView`（WebKitGTK 6.0） | `<iframe>` |
| `Scroll` | `UIScrollView` | `NestedScrollView` | `NSScrollView` | 宿主的视图，配 Fluent 的滚动指示器 | `GtkScrolledWindow` | 可滚动的元素 |
| `List` | `UICollectionView` | `RecyclerView` | 由宿主复用的行，放在 `NSScrollView` 中 | 由宿主复用的行 | 由宿主复用的行，放在 `GtkScrolledWindow` 中 | 由宿主复用的行 |

下面各节会说明平台之间行为不一致的地方。

## 文本

### Text 与 StaticText

`ui.Text` 显示一行或一段文本。它就是平台的标签控件，默认会自动换行；`Style.Lines` 可以限制最多显示几行，超出的部分以省略号截断。`ui.StaticText` 则是文本永远不变的 `Text`。

```go
count := signal.New(0)
label := signal.Func(func() string { return fmt.Sprintf("%d unread", count.Get()) })
return ui.Column(ui.Style{Gap: 8},
	ui.StaticText("Inbox", ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}),
	ui.Text(label, ui.Style{Color: colors.SecondaryLabel}),
	ui.StaticText("A long description that stops after two lines.", ui.Style{Lines: 2}),
)
```

样式中的 `Font`、`Color`、`TextAlign` 和 `Lines` 作用于文本本身。文本样式（`fonts.Body`、`fonts.Headline`、`fonts.Footnote` 等）会跟随用户设置的字号。

### RichText

`ui.RichText` 显示带样式的文本，内容是一个 `ui.Rich`，也就是一组文本片段（span）。片段中的 `"\n"` 会另起一段。片段里没有设置（保持零值）的字段，会沿用 `RichText` 的样式。

```go
ui.RichText(ui.RichTextProps{
	Text: ui.Val(ui.Rich{
		ui.Strong("asuka"), ui.Plain(" draws "), ui.Em("native"), ui.Plain(" views. Run "),
		ui.Code("asuka dev"), ui.Plain(", or read "), ui.Link("the docs", "https://example.com/docs"),
		ui.Plain(".\nA second paragraph, "), ui.Plain("in red").Colored(colors.Red), ui.Plain("."),
	}),
	OnLink: ui.OpenURL,
})
```

- `Plain`、`Strong`、`Em`、`Code` 和 `Link` 用来创建片段。`Colored`、`Highlighted` 和 `Weighted` 返回加上了颜色、背景或字重的片段。
- 其余设置都在 `ui.Span` 上：`Font`、`Italic`、`Mono`、`Underline`、`Strike`、`Background`（在片段覆盖的每一行上绘制，带圆角）。
- 点击链接时，会以链接的 URL 调用 `OnLink`。没有设置 `OnLink` 时，点击链接不会有任何反应。
- 设置 `Role: ui.RoleHeader` 后，屏幕阅读器会把它当作标题。

在 iOS 和 Android 上，每个段落都由平台自己的排版引擎（CoreText、StaticLayout）排版，并按内容和宽度缓存。因此往长文本末尾追加内容时（比如一条流式输出的回复），只有发生变化的那个段落会重新排版。在 macOS 上它是只读的 `NSTextView`（TextKit），在 Windows 上是只读的 rich edit 控件（`RICHEDIT50W`），在 Linux 上是用 Pango 属性设置样式的 `GtkLabel`，在浏览器中则是段落里的一组 span。[`examples/richtext`](../../examples/richtext) 演示了流式显示的效果。

如果只是纯文本，请用 `Text`：它就是平台的标签控件，行为完全是原生的。

**内嵌视图。** `ui.Embed` 可以把一个组件放进文本中：组件坐落在基线上，随文字一起换行。`Rich` 更新时，只要 key 不变，它就还是同一个组件，状态也会保留下来。

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{
	ui.Plain("Status: "),
	ui.Embed("badge", func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Background: colors.Green, CornerRadius: 6, Padding: ui.Symmetric(2, 6)}},
			ui.StaticText("live", ui.Style{Font: fonts.Caption, Color: colors.RGB(255, 255, 255)}))
	}),
	ui.Plain(" since this morning."),
})})
```

### 可选择的文本

`ui.Selectable` 让其中的 `RichText` 视图都可以被选择，而且是把它们当作一整段文本来选：长按选中一个词，拖动平台的选择手柄扩大选区，再用平台的编辑菜单复制。用鼠标时，拖动即可选择，用平台的复制快捷键复制。Linux 和 Web 上同样可以跨多个文本整体选择；macOS 和 Windows 上则是每个文本各选各的。

```go
ui.Selectable(ui.SelectableProps{Style: ui.Style{Gap: 12}},
	ui.RichText(ui.RichTextProps{Text: ui.Val(question), Role: ui.RoleHeader}),
	ui.RichText(ui.RichTextProps{Text: ui.Val(answer)}),
)
```

`Text` 不能被选择；需要可选择的文本时，请把 `RichText` 放进 `Selectable`。如果不经过选区直接复制内容，可以用 `ui.Copy(text)` 把它放到剪贴板。

### Markdown

`asuka/sdk/markdown` 包把 Markdown 显示为原生视图，底层用的是 `RichText`。它会随着文本的追加增量解析：只重新解析正在写的那个块，也只更新这个块对应的视图。

```go
markdown.View(markdown.Props{Text: reply, Streaming: writing})
```

`Streaming` 表示文本还在写入中，这样写了一半的 `**bold` 会显示为粗体，而不是露出星号。整个视图默认是一个整体选区（可以用 `NotSelectable` 关掉）；链接默认用 `ui.OpenURL` 打开，除非你提供了 `OnLink`；`Theme` 用来修改字体、颜色和间距；`Code` 让你按自己的方式显示代码块。它支持 CommonMark 和 GitHub 的扩展语法。参见 [`examples/markdown`](../../examples/markdown)。

## 按钮

`ui.Button` 就是平台的按钮：在 iOS 上是使用配置（configuration）的 `UIButton`，在 Android 上是 Material 按钮，在 macOS 上是 `NSButton`，在 Windows 上是绘制成 Fluent 风格的 `BUTTON`，在 Linux 上是 `GtkButton`，在浏览器中是 `<button>`。

```go
saving := signal.New(false)
return ui.Row(ui.Style{Gap: 12, Wrap: true},
	ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, Disabled: saving,
		OnPress: func() { saving.Set(true) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonTinted}),
	ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: ui.Val("Delete"), Variant: ui.ButtonGray}),
)
```

- `Title` 是按钮的文字；`Icon` 是显示在文字前面的符号（见[符号](#符号与图标)）。有图标而没有标题的按钮只显示图标。
- `Label` 是 VoiceOver 和 TalkBack 朗读的内容，默认就是标题。只有图标的按钮记得设置它。
- `Tooltip` 是指针停在按钮上时显示的简短提示，适用于 iPad 的指针（`UIToolTipInteraction`）、Android 上的鼠标（长按也会显示，即 `tooltipText`），以及 macOS、Windows、Linux 和 Web（元素的 `title`）。只有图标的按钮应当设置一个，内容就是按钮的名称。`ViewProps.Tooltip` 对任意视图起同样的作用。在 iOS 上，只用触摸屏时提示永远不会出现，所以不能把它作为传达某个信息的唯一途径。
- `Disabled` 按平台的方式禁用按钮。
- `Style.Font` 和 `Style.Color` 修改按钮的文字和色调。

`Variant` 决定按钮的外观：

| Variant | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| `ButtonPlain`（默认） | 普通 | 文本按钮 | 无边框，使用强调色 | subtle 样式，使用强调色 | 扁平，使用强调色 | 普通 |
| `ButtonFilled` | 填充 | 填充按钮 | 强调色的 push 按钮 | accent 按钮 | 建议操作（suggested action）样式 | 填充 |
| `ButtonTinted` | 着色 | 色调按钮 | push 按钮，标题使用强调色 | 一层淡淡的强调色 | 一层淡淡的强调色 | 着色 |
| `ButtonGray` | 灰色 | 色调按钮 | push 按钮 | 标准按钮 | 标准按钮 | 灰色 |
| `ButtonBordered` | 带边框 | 描边按钮 | push 按钮 | 标准按钮 | 标准按钮 | 描边 |
| `ButtonGlass` | Liquid Glass（`glass()`，iOS 26；之前为灰色） | 色调按钮 | 同 `ButtonGray` | 同 `ButtonGray` | 标准按钮 | 同 `ButtonGray` |
| `ButtonGlassProminent` | 着色的 Liquid Glass（`prominentGlass()`，iOS 26；之前为填充） | 填充按钮 | 同 `ButtonFilled` | 同 `ButtonFilled` | 建议操作样式 | 同 `ButtonFilled` |

玻璃按钮是胶囊形的；只有图标的玻璃按钮则是圆形，可以用作弹出面板的关闭按钮，和 iOS 26 系统自带的一样。

### 菜单

设置了 `Menu` 的按钮被点击时会弹出其中的操作，而不是调用 `OnPress`（iOS 上是 `UIMenu`，Android 上是 `PopupMenu`，macOS 上是 `NSMenu`，Windows 上是弹出菜单，Linux 上是 `GtkPopoverMenu`，浏览器中是一个 popover）。每个 `ui.Action` 都有 `Title`、一个可选的 `Icon`（显示在标题旁边）、`Role` 和 `OnPress`。`ui.ActionDestructive` 会让它显示为红色。

```go
ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: []ui.Action{
	{Title: ui.Val("Copy"), Icon: ui.IconCopy, OnPress: func() { ui.Copy("https://go.dev") }},
	{Title: ui.Val("Share"), Icon: ui.IconShare, OnPress: func() { ui.Share("https://go.dev") }},
	{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {}},
}})
```

操作可以设为 `Disabled`（变暗，不能选）或 `Checked`（旁边打一个对勾），菜单打开期间两者都会跟随各自的 signal 变化。带 `Children` 的操作是一个子菜单，可以用 `ui.Submenu` 创建。如果同时设置了 `Inline`，它的子项就不再是子菜单，而是菜单里的一个分区：上下用分隔线隔开，顶部显示它的标题（相当于 SwiftUI 在 `Menu` 里放一个 `Section`），可以用 `ui.MenuSection` 创建。分区里有选中项时，它就成了一组单选：

```go
sortBy := signal.New("Name")
sorted := func(by string) ui.Action {
	return ui.Action{Title: ui.Val(by), Checked: signal.Func(func() bool { return sortBy.Get() == by }),
		OnPress: func() { sortBy.Set(by) }}
}
ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: []ui.Action{
	ui.MenuSection(ui.Val("Sort by"), sorted("Name"), sorted("Date")),
	ui.Submenu(ui.Val("Move to"), ui.IconFolder,
		ui.Action{Title: ui.Val("Inbox"), OnPress: moveToInbox},
		ui.Action{Title: ui.Val("Archive"), OnPress: archive},
	),
	{Title: ui.Val("Delete"), Role: ui.ActionDestructive, Disabled: locked, OnPress: remove},
}})
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Disabled` | `.disabled` | 禁用的菜单项 | 禁用的菜单项 | 灰显的菜单项 | 禁用的 action | `disabled`、`aria-disabled` |
| `Checked` | `state = .on` | 勾选的菜单项；分区里的单选显示为单选按钮 | `state = on` | `MF_CHECKED` | 带状态的 action（打勾） | `menuitemcheckbox` |
| `Children` | 嵌套的 `UIMenu` | `SubMenu` | 子菜单 | 子菜单 | GMenu 子菜单 | 子菜单（按指向它的方向键即可展开） |
| `Inline` | `displayInline` 的 `UIMenu` | 用分隔线隔开的分组，标题是一个禁用的菜单项 | 分隔线加分区标题（macOS 14） | 分隔线加自绘的标题 | 带标签的 GMenu 分区 | 夹在分隔线之间、带标题的 `group` |

页面导航栏上的按钮（`ScreenOptions.Actions`）也一样：禁用的按钮会变暗，带 `Children` 的按钮点击后会以菜单形式展开子项（相当于 SwiftUI 在工具栏里放一个 `Menu`）。警告框的按钮同样可以禁用，并在显示期间跟随 `Disabled` 变化（比如等用户填好名字才能点“好”）。滑动操作则两者都不显示。

`ui.Menu` 可以给任意内容加上菜单。设置 `LongPress` 后，它就是长按弹出的上下文菜单；用鼠标时（macOS、Windows、Linux、Web），则在右键点击时弹出：

```go
ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
	{Title: ui.Val("Reply"), Icon: ui.IconMessage},
	{Title: ui.Val("Delete"), Role: ui.ActionDestructive},
}, Style: ui.Style{Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12}},
	ui.StaticText("Long-press for a menu"),
)
```

## 文本输入框

`ui.TextField` 是文本输入框。`Value` 是它的文本：用户输入时会写入这个 signal，而在代码里写入 signal，则会替换输入框中的文本。

```go
email := signal.New("")
password := signal.New("")
var pw ui.Node
pw = ui.TextField(ui.TextFieldProps{Value: password, Placeholder: ui.Val("Password"),
	Secure: true, ContentType: "password", ReturnKey: ui.ReturnGo,
	OnSubmit: func() { ui.Blur(pw) }})
em := ui.TextField(ui.TextFieldProps{Value: email, Placeholder: ui.Val("Email"),
	Keyboard: ui.KeyboardEmail, ContentType: "email", Autocapitalize: ui.CapitalizeNone,
	Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnNext, OnSubmit: func() { ui.Focus(pw) }})
return ui.Column(ui.Style{Gap: 12}, em, pw)
```

- `Secure` 隐藏输入的内容，用于密码。
- `Keyboard`：`KeyboardDefault`、`KeyboardNumber`、`KeyboardDecimal`、`KeyboardEmail`、`KeyboardURL`、`KeyboardPhone`。
- `ReturnKey` 是回车键上显示的文字：`ReturnDefault`、`ReturnDone`、`ReturnGo`、`ReturnNext`、`ReturnSearch`、`ReturnSend`。按下回车键会调用 `OnSubmit`。
- `ContentType` 说明输入框里填的是什么内容，供系统的自动填充使用：`ui.ContentUsername`、`ContentPassword`、`ContentNewPassword`（平台会推荐一个强密码）、`ContentOneTimeCode`（刚收到的短信验证码会出现在键盘上方，即 UIKit 的 `oneTimeCode` 和 Android 的短信验证码提示）、`ContentEmail`、`ContentPhone`、`ContentName`、`ContentAddress`、`ContentPostalCode`、`ContentCardNumber` 等。各个宿主会按自己平台的叫法来设置。
- `Autocapitalize`：`CapitalizeSentences`（默认）、`CapitalizeNone`、`CapitalizeWords`、`CapitalizeCharacters`。`Autocorrect`：`AutocorrectDefault`、`AutocorrectYes`、`AutocorrectNo`。
- `OnFocus` 和 `OnBlur` 跟随键盘的弹出和收起。`ui.Focus(field)` 唤起键盘，`ui.Blur(field)` 收起键盘。

桌面端没有可供切换的屏幕键盘，所以 `Keyboard` 和 `ReturnKey` 在那里基本看不出效果，不过按回车仍然会调用 `OnSubmit`。在 macOS 上，`ContentType` 对应输入框的自动填充内容类型。在 Windows 上，`KeyboardNumber` 只允许输入数字。在 Linux 上，`Keyboard`、`ContentType`、`Autocapitalize` 和 `Autocorrect` 会告诉输入法这个输入框接受什么内容（即 GTK 的 input purpose 和 input hints）。在浏览器中，它们分别对应输入元素的 `inputmode`、`enterkeyhint`、`autocomplete`、`autocapitalize` 和 `spellcheck`，手机键盘会据此调整。

**多行文本。** `Multiline` 把输入框变成文本区域，适合输入消息或评论：回车会换行，而不是调用 `OnSubmit`。它会随文本增高，最多到 `Style.Lines` 行（0 表示不限），超出后开始滚动。

```go
message := signal.New("")
return ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd},
	ui.TextField(ui.TextFieldProps{Value: message, Placeholder: ui.Val("Message"), Multiline: true,
		Style: ui.Style{Grow: 1, Lines: 5}}),
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled,
		Disabled: signal.Func(func() bool { return message.Get() == "" }),
		OnPress:  func() { message.Set("") }}),
)
```

**只保留文字。** `Plain` 去掉输入框的边框、背景和内边距，只留下文字和占位符，适合放进卡片或你自己设计的表单里，就像备忘录和提醒事项中的输入框那样。

```go
ui.TextField(ui.TextFieldProps{Value: note, Placeholder: ui.Val("Note"), Multiline: true, Plain: true})
```

`SafeArea` 会为键盘留出内边距，所以放在它底部的输入框始终位于键盘上方。

## 控件

每个控件都接受它要修改的 signal，以及一个 `Disabled` 访问器。除了 `Picker`，它们都不带标签：需要的话，在旁边放一个 `Text`。

```go
return ui.Column(ui.Style{Gap: 16},
	ui.Row(ui.Style{Align: ui.AlignCenter},
		ui.StaticText("Wi-Fi", ui.Style{Grow: 1}),
		ui.Toggle(ui.ToggleProps{Value: wifi})),
	ui.Slider(ui.SliderProps{Value: volume}),
	ui.Segmented(ui.SegmentedProps{Segments: ui.Val([]string{"Day", "Week", "Month"}), Selected: period}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%.0f cups", cups.Get()) }), ui.Style{Grow: 1}),
		ui.Stepper(ui.StepperProps{Value: cups, Min: 1, Max: 8})),
	ui.Wheel(ui.WheelProps{Items: ui.Val(sizes), Selected: size}),
	ui.Picker(ui.PickerProps{Items: ui.Val(milks), Selected: milk, Label: ui.Val("Milk")}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Tint", ui.Style{Grow: 1}),
		ui.ColorPicker(ui.ColorPickerProps{Value: tint, Alpha: true, Title: ui.Val("Tint")})),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Starts", ui.Style{Grow: 1}),
		ui.DatePicker(ui.DatePickerProps{Value: when, Mode: ui.DateAndTime, Min: time.Now()})),
)
```

- **Toggle** 是开关，绑定一个 `*signal.Signal[bool]`。设置 `Checkbox` 后它会变成复选框：Android 上是 `MaterialCheckBox`，Mac 和 Windows 上是各自的复选框，Linux 上是 `GtkCheckButton`，浏览器中是复选框；iOS 没有复选框，于是用一个选中后填上对勾的圆圈，和提醒事项里的一样。
- **Slider** 在 `Min` 到 `Max` 之间取一个 `float64`；两者都为 0 时，范围是 0 到 1。
- **Segmented** 把几个选项并排摆出来，从中选一个，`Selected` 是选中项的索引。它对应 UIKit 的分段控件和 Material 的分段按钮。Windows 没有这种控件，由宿主自行绘制一个，仿照 Windows Community Toolkit 的 `Segmented`。
- **Stepper** 用减号和加号按 `Step`（默认为 1）增减一个 `float64`，范围从 `Min` 到 `Max`（两者都为 0 时是 0 到 100）。它在 iOS 上是 `UIStepper`，在 macOS 上是 `NSStepper`；在其他平台上则是两个按钮（Android 上是 Material 图标按钮，Linux 上是相连的按钮，和 GNOME 的微调按钮外观一致）。
- **Wheel** 在滚轮上从众多 `Items` 中选一个，对应 `UIPickerView` 和 Android 的 `NumberPicker`。桌面端和 Web 没有滚轮控件，所以在那里它是一个列出所有选项的弹出菜单（`NSPopUpButton`、绘制成 WinUI `ComboBox` 样式的下拉组合框、`GtkDropDown`、`<select>`）。
- **Picker** 从菜单中的众多 `Items` 里选一个，并显示选中的那一项（相当于菜单样式的 SwiftUI `Picker`，以及 Material 的 exposed dropdown menu）。`Selected` 超出范围（-1）表示什么都没选，这时选择器显示它的 `Label`；屏幕阅读器也用这个标签来称呼它。在 Android 上，标签是输入框的标签，浮在选中项上方。`Selected` 这个 signal 可以是 nil，这时选择器自己记住用户的选择。

  | | Picker |
  |---|---|
  | iOS | 一个普通的 `UIButton`，使用强调色并带弹出指示符，菜单里给选中项打勾（`changesSelectionAsPrimaryAction`） |
  | Android | 一个 exposed dropdown menu 样式的描边 `TextInputLayout`，里面是不能输入的 `MaterialAutoCompleteTextView`；TalkBack 会把它读作下拉列表 |
  | macOS | `NSPopUpButton`（标签是一个隐藏的菜单项，只在什么都没选时显示） |
  | Windows | 绘制成 WinUI 3 `ComboBox` 样式的下拉列表组合框（控件的填充色和描边，末端的 V 形箭头；列表在浮出面板上自绘，选中项开头有一条强调色的小竖条，在 Windows 11 上由 DWM 圆角处理）；什么都没选时，用次要文字颜色绘制标签，标签也是它在 UI Automation 中的名称 |
  | Linux | 基于 `GtkStringList` 的 `GtkDropDown`，标签以暗色文字显示在选中项的位置 |
  | Web | `<select>`，第一个选项是隐藏的占位项，显示标签 |
- **ColorPicker** 是一个色块，点击后打开平台的颜色选择器，选中的结果写入一个 `ui.Color`。`Alpha` 允许用户选择不透明度，`Title` 是选择器的标题。iOS 上是 `UIColorWell` 加系统的颜色选择器；Android 没有颜色选择器，所以是一个列出调色板的 Material 对话框，设置了 `Alpha` 时再附带一个不透明度滑块。macOS 上是打开颜色面板的 `NSColorWell`，Linux 上是 `GtkColorDialogButton`，浏览器中是 `<input type="color">`。Windows 的颜色对话框不支持不透明度，因此 `Alpha` 在那里不起作用。
- **DatePicker** 选取一个 `time.Time`，按设备所在的时区显示。`Mode` 可以是 `ui.DateOnly`（默认）、`ui.TimeOnly` 或 `ui.DateAndTime`；`Min` 和 `Max` 限定可选范围。`Value` 为零值时显示当前时间。

  | | 紧凑（默认） | `Inline` |
  |---|---|---|
  | iOS | UIKit 的紧凑型日期选择器 | 直接展开的日历和时间，适合单独放在一个弹出面板里 |
  | Android | 显示日期和时间的按钮，点击后打开 Material 的选择器 | 同样的按钮 |
  | macOS | `NSDatePicker` 的文本框和步进器 | 它的日历和时钟 |
  | Windows | 日期时间选择器，日历以下拉方式展开 | 月历；`DateAndTime` 时下方再加一个时间选择器（`TimeOnly` 仍为紧凑样式） |
  | Linux | 一个按钮，打开包含 GTK 日历以及时、分微调按钮的 popover | 直接展开的日历和微调按钮 |
  | Web | `<input type="date">`、`"time"`、`"datetime-local"` | 同样的输入元素 |

## 进度与加载

`ui.Progress` 是进度条：值从 0 到 1；不知道进度时传 `nil`。样式中的 `Color` 是它的填充色。`ui.ProgressRing` 是圆形的进度条：圆环从顶部开始顺时针填充，值变化时带有动画；传 `nil` 时显示平台自己的不确定进度圆环。`ui.Activity` 是一个加载指示器（spinner）。

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Progress(done),
	ui.Progress(nil),
	ui.ProgressRing(done),
	ui.ProgressRing(done, ui.Style{Width: ui.Pt(24), Color: colors.Green}),
	ui.ProgressRing(nil),
	ui.Activity(),
)
```

圆环有自己的尺寸，每个平台各不相同（iOS 58 点，Android 48dp，Mac 和 Windows 32，Linux 和 Web 32）。样式里只设了一边时，另一边也随之相同，所以圆环始终是正方形。屏幕阅读器会把它读作进度指示器，并读出百分比。

| | 圆环 | 没有值时 |
|---|---|---|
| iOS | UIKit 没有确定进度的圆环，而 SwiftUI 的圆形 `ProgressView` 在 iOS 上会忽略它的值。所以圆环按 SwiftUI 圆形容量 `Gauge` 的样子绘制：强调色的圆头描边，压在 30% 强调色的轨道上，描边宽度是圆环尺寸的十分之一 | `UIActivityIndicatorView` |
| Android | Material 的 `CircularProgressIndicator`，按它的尺寸取 Material 3 对应的规格（extra small、small 或 medium：各自的轨道、内边距和间隙） | 同一个控件，不确定进度模式 |
| macOS | 没有 `Color` 时，是 AppKit 自己尺寸（16 和 32 点）的 `NSProgressIndicator` 确定进度圆环。AppKit 只在这两个尺寸、而且只用强调色绘制它的圆环，所以其他尺寸或设置了 `Color` 时，由宿主绘制填满整个区域的圆环：16 到 32 点之间是 5 点宽的圆头描边（大于 32 点按 32 点圆环的比例，小于 16 点按 16 点的比例），压在 AppKit 的轨道上，颜色用 `Color`，或者用从 AppKit 自己的指示器读出来的颜色（强调色、非活动窗口中的灰色，以及浅色和深色模式）。值的变化会像 AppKit 的 animator 那样用四分之一秒过渡过去，开启“减弱动态效果”时除外。两种情况下，VoiceOver 都会把它读作 AppKit 的指示器 | 16 和 32 点时是它的旋转指示器；其他尺寸则按该尺寸绘制它那 8 根辐条（缩放后带有 AppKit 的模糊效果，它本身不接受颜色），领头的那根用 `Color` |
| Windows | Fluent 的 `ProgressRing`（WinUI 的）：一段没有轨道的圆弧。讲述人会把它读作进度条 | 转动的圆弧，和 `Activity` 一样 |
| Linux | 按 `AdwSpinner` 绘制圆环的方式绘制：15% 文字颜色的轨道，加上强调色的圆弧 | `AdwSpinner`（libadwaita 1.6 之前为 `GtkSpinner`） |
| Web | 一个 SVG 圆环，`role="progressbar"` 并带 `aria-valuenow` | 转动的圆弧；设置了 `prefers-reduced-motion` 时转得更慢 |

## 图片

`ui.Image` 显示一张图片。平台会在主线程之外加载和解码图片，并缓存在内存中。iOS 和 Android 还会把通过 http(s) 获取的图片存到磁盘上，应用重启后依然有效；桌面端宿主只缓存在内存里，浏览器则使用它自己的缓存。

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Image(ui.ImageProps{Source: ui.Val("logo.png"), Style: ui.Style{Width: ui.Pt(64)}}),
	ui.Image(ui.ImageProps{
		Source:      ui.Val("https://picsum.photos/id/1015/600/400"),
		Placeholder: "placeholder.png",
		Mode:        ui.ImageFill,
		Label:       ui.Val("A river in a valley"),
		Style:       ui.Style{AspectRatio: 3.0 / 2, CornerRadius: 12, Clip: true},
	}),
	ui.Image(ui.ImageProps{
		Source:     signal.Func(func() string { return photo.Get().URL }),
		BlurHash:   signal.Func(func() string { return photo.Get().BlurHash }),
		Mode:       ui.ImageFill,
		Transition: 0.4,
		Style:      ui.Style{AspectRatio: 1, CornerRadius: 12},
	}),
)
```

- `Source` 可以是 http(s) URL、应用 `assets` 目录下的路径（`"logo.png"` 即 `assets/logo.png`），也可以是绝对路径，比如应用自己写入的文件，或者用户选取的照片。
- `Mode` 决定图片如何填充它的框：`ImageFit`（默认，完整显示，必要时留出空边）、`ImageFill`（铺满整个框，超出部分裁掉）、`ImageStretch`（拉伸成框的宽高比）、`ImageCenter`（按原始尺寸居中）。
- `Placeholder` 在 `Source` 加载完成之前显示，加载失败时也会显示。占位图请用资源文件（asset），这样能立即显示。记得给图片设定尺寸，让占位图有地方显示。
- `BlurHash` 和 `ThumbHash` 是用应用随图片保存的哈希生成的占位图（[BlurHash](https://blurha.sh)、[ThumbHash](https://evanw.github.io/thumbhash/)，后者为 base64 编码），和 expo-image 的一样。核心会把哈希解码成一张小图，每个哈希只解码一次（最近用过的几百个会被保留），并立即显示在 `Placeholder` 的位置上。BlurHash 本身不带形状信息：它按样式的 `AspectRatio` 解码，没有设置时解码成正方形并拉伸到整个框。ThumbHash 会保留原图的宽高比和透明度。它们都是访问器，因为列表的行在复用时会换成别的图片。
- `PlaceholderMode` 决定占位图如何填充框：`PlaceholderFit`、`PlaceholderFill`、`PlaceholderStretch`、`PlaceholderCenter`；默认和图片本身的方式一样。
- `Transition` 是图片加载完成时淡入所用的秒数：在占位图上交叉淡入，没有占位图时从无到有地淡入。默认只在有占位图时淡入，时长取平台的短动画时长（0.25 秒；Android 上是 `config_shortAnimTime`）；设为 `ui.NoTransition` 则立即显示。
- `Animated` 为 true 时播放动图，为 false 时让它停在当前帧。不设置时，动图会按文件规定的次数播放；但如果用户要求减少动态效果（“减弱动态效果”、Android 关闭动画、`prefers-reduced-motion`），它只显示第一帧。
- `Label` 向屏幕阅读器描述这张图片；不设置时，图片被视为纯装饰。
- `OnLoad` 在图片加载完成后收到它的原始尺寸；`OnError` 在加载失败时收到错误信息。

没有设置尺寸的图片在加载完成前是空的。加载后，它采用原始尺寸，一个像素对应一个点，但宽度不会超过可用空间。如果只确定了一边（设置了 `Width`，或在行中设置了 `Grow`），它会保持宽高比；用 `AspectRatio` 则可以在加载之前就确定宽高比。

每个平台用它自己的解码器解码图片：

| | 能解码的格式 | 动图（GIF、APNG、动态 WebP）的播放 |
|---|---|---|
| iOS | ImageIO：PNG、JPEG、GIF、WebP、HEIC、AVIF | ImageIO 的 `CGAnimateImageDataWithBlock`，按文件规定的时长和循环次数播放（也支持 HEIF 序列） |
| Android | 先用 `BitmapFactory`，再用 `ImageDecoder`：PNG、JPEG、GIF、WebP、HEIF、AVIF | `AnimatedImageDrawable`：GIF 和 WebP（系统支持时也包括 AVIF 序列）；APNG 只显示第一帧 |
| macOS | `NSImage`：PNG、JPEG、GIF、WebP、HEIC、AVIF、SVG | 同 iOS |
| Windows | WIC：PNG、JPEG、GIF、BMP、TIFF、ICO，以及安装了对应编解码器时的 WebP、HEIF 和 AVIF（WebP 的随 Windows 11 自带；HEIF 需要 HEIF 和 HEVC 视频扩展，AVIF 需要 AV1 视频扩展）；WIC 解不了时，用 Go 的 PNG、JPEG 和 GIF 解码器 | 由宿主逐帧播放；动态 WebP 的每一帧用 WIC 的 WebP 编解码器解码 |
| Linux | 先用 Go 的 PNG、JPEG 和 GIF 解码器，再用 GDK 的加载器：WebP、AVIF、HEIF、SVG、JPEG XL 等，视安装情况而定 | 由宿主逐帧播放；动态 WebP 的每一帧用 GDK 的 WebP 加载器解码 |
| Web | 浏览器支持的格式 | 用 WebCodecs 的 `ImageDecoder` 在 canvas 上播放（也支持 AVIF 序列）；没有它的浏览器（Safari）由 `<img>` 自己播放，暂停时显示第一帧 |

只有在 Mac、Linux 和 Web 上，SVG 才能作为图片显示。要在所有平台上显示 SVG 图标，请用 [`asuka/sdk/svg`](#svg-图标) 以矢量方式绘制。

## 符号与图标

`ui.Symbol` 显示一个符号。在 iOS 和 macOS 上它是 SF Symbol，在 Android 和 Web 上是对应的 Material Symbol，在 Windows 上是对应的 Segoe Fluent Icons 字形（Windows 11 之前为 Segoe MDL2 Assets），在 Linux 上是图标主题（Adwaita）中对应的符号图标，没有的话就用 Material Symbol。它按样式中的 `Font` 像文字一样确定大小，因此会跟随字号变化；颜色取自样式中的 `Color`（默认为标签颜色）。

```go
return ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
	ui.Symbol(ui.IconBell),
	ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: colors.Yellow, Font: fonts.Title}),
	ui.Symbol(ui.Icon("archivebox"), ui.Style{Color: colors.SecondaryLabel}),
)
```

`ui.Icon` 就是一个 SF Symbol 名称。按钮、菜单项、栏按钮和标签页也都接受它。`Icon` 常量收录了常用的图标：`IconHome`、`IconSearch`、`IconSettings`、`IconPlus`、`IconClose`、`IconTrash`、`IconShare`、`IconMore`，以及另外五十个左右。`icon.Filled()` 返回它的填充版本（`"star.fill"`）。

iOS 和 macOS 能绘制任意 SF Symbol。其他平台只能绘制一组精选的符号，包括这些常量以及更多：它们在 [`platform/android/gen_symbols.go`](../../platform/android/gen_symbols.go) 中映射到 Material Symbols（Web 和 Linux 的映射由它生成），在 Windows 上映射到 Segoe Fluent Icons 的字形。不在这组里的名称在这些平台上什么也不显示（宿主会记录一条警告），所以像 `"archivebox"` 这样直接用字符串写的名称，要在你发布的每个平台上都检查一遍。在 Linux 上，以 `-symbolic` 结尾的名称会被当作图标主题中的名称，原样使用。

### SVG 图标

如果要用自己的图标，`asuka/sdk/svg` 包可以借助平台的矢量绘图能力绘制 SVG，全程不做栅格化。`svg.MustParse` 解析 SVG 标记（`svg.Parse` 则在出错时返回 error），`svg.View` 负责绘制。

```go
var download = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>`)

func downloadIcon() ui.Node {
	return svg.View(svg.Props{Icon: download, Color: ui.Val(colors.Accent), Label: ui.Val("Download"),
		Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
}
```

`Color` 用来绘制 SVG 中以 `currentColor` 绘制的部分，以及 SVG 没有指定填充的部分；默认是文本颜色 `colors.Label`。如果 `Style` 中没有宽度或高度，视图就采用 SVG 自身的尺寸；只设置了其中一个时，另一个按宽高比计算。建议用 `//go:embed` 把图标放在单独的文件里，并在包级变量中只解析一次。

它支持各图标库（Lucide、Material、Heroicons、Feather）常用的特性：路径、基本形状、分组、`use`、填充、描边和变换。不支持文本、渐变（改用图标的颜色绘制）、裁剪、蒙版、滤镜和 `fill-rule="evenodd"`，遇到时会跳过。

## 视频

`ui.Video` 用平台的播放器及其控件播放视频：iOS 上是 `AVPlayerViewController`，Android 上是 Media3 的 ExoPlayer，macOS 上是 `AVPlayerView`，Linux 上是 `GtkVideo`（GStreamer），浏览器中是 `<video>`。Win32 没有视频视图，所以在 Windows 上它是 WebView2 中的 `<video>`，使用 Edge 的播放控件。用 `ui.Play`、`ui.Pause` 和 `ui.Seek` 控制播放。

```go
var v ui.Node
v = ui.Video(ui.VideoProps{
	Source:   ui.Val("https://example.com/clip.mp4"),
	AutoPlay: true, Muted: true, Loop: true,
	Style: ui.Style{CornerRadius: 12},
})
return ui.Column(ui.Style{Gap: 12}, v,
	ui.Button(ui.ButtonProps{Title: ui.Val("Start over"), OnPress: func() { ui.Seek(v, 0); ui.Play(v) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Pause"), OnPress: func() { ui.Pause(v) }}),
)
```

- `Source` 是 http(s) URL（视频文件或 HLS 流），或者应用 assets 中的文件。
- `Mode` 为 `ui.ImageFit`（默认，留出空边）或 `ui.ImageFill`。
- `HideControls` 隐藏平台的播放控件：播放按钮、进度条、全屏，以及 iOS 上的 AirPlay 和画中画。
- 每次播放到结尾都会调用 `OnEnd`；无法播放时调用 `OnError`。

它的尺寸规则和 `Image` 一样：在得知视频尺寸之前是空的（除非样式给了它尺寸），之后保持视频的宽高比。`Seek` 的参数是从开头算起的秒数。

## Lottie 动画

`asuka/sdk/lottie` 播放 Lottie 动画（从 After Effects 导出的 Lottie JSON（`.json`）或 dotLottie（`.lottie`）文件），和 Expo 中的 lottie-react-native 一样。每个平台都用它自己的 Lottie 播放器来绘制：iOS 和 Mac 上是 lottie-ios，Android 上是 lottie-android，Web 上是 lottie-web。Windows 和 Linux 没有可调用的播放器，这个包会自己用 `ui.Canvas` 绘制动画。

```go
progress := signal.New(0.0)
return ui.Column(ui.Style{Gap: 12},
	lottie.View(lottie.Props{
		Source:   ui.Val("success.json"),
		Loop:     true,
		Progress: progress,
		Overrides: ui.Val([]lottie.Override{
			lottie.Colors("Ring", colors.Hex(0x0a84ff), colors.Hex(0x64d2ff)),
		}),
		Style: ui.Style{Width: ui.Pt(200)},
	}),
	ui.Slider(ui.SliderProps{Value: progress}),
)
```

- `Source` 是动画文件：http(s) URL、应用 `assets` 中的路径或绝对路径；也可以用 `Data` 直接给出文件内容（`go:embed`）。`Animation` 按 ID 选出 dotLottie 中的某个动画。
- `Playing` 为 true 时播放，为 false 时停在当前位置；不设置时，动画加载完就开始播放。`Loop` 让它循环播放；否则播到末尾停下，并调用 `OnComplete`。`Speed` 调整播放速度（负数表示倒放）。
- `Progress` 是当前进度，从 0 到 1。播放时视图会逐帧写入它；应用写入的值会让动画跳到那里：用滑块拖动，或者用手势驱动。在 `ui.Animate` 中写入时，它会按那个动画的时长和曲线移动过去，之后如果正在播放就从那里继续。
- `Segment`（帧，从 `From` 到 `To`）或 `Marker`（按名称指定的某个标记）决定播放哪一段。`OnLoad` 会收到动画的 `Info`：尺寸、帧范围、帧率和标记。
- `Mode` 决定它如何填充自己的框，和图片一样。样式中没有尺寸时，它采用动画的尺寸，但不超过可用宽度；只给出一边时，另一边按动画的宽高比确定。
- `Overrides` 按 keypath 修改动画的颜色和数值，用于主题或深色模式：`lottie.Color`（keypath 下所有填充和描边的颜色）、`lottie.Colors`（浅色模式一种颜色，深色模式另一种）、`lottie.Opacity`、`lottie.StrokeWidth`。keypath 是从根开始的图层、组和形状的名称，用点分隔，`*` 匹配任意一个名称，`**` 匹配任意多个，和 lottie-ios、lottie-android 的写法一样：`"Ring"`、`"Circle.Disc.Fill 1"`、`"**.Stroke 1"`。
- `ReduceMotion` 决定用户要求减少动态效果时它怎么做：默认（`ReduceToEnd`）不播放，显示最后一帧；`ReduceToStart` 显示第一帧；`ReduceIgnore` 照常播放，适合动效本身就是含义的动画。
- `Label` 向屏幕阅读器描述它；不设置时视为纯装饰。

导入这个包的应用会带上这些播放器：一个 Swift 包（lottie-ios 的二进制发行版 lottie-spm）、一个 Gradle 库（`com.airbnb.android:lottie`），以及 lottie-web 的脚本（在第一个视图出现时才加载）；没有导入它的应用不会带上。它是一个[原生包](native.md)，所以在手机上用 `asuka dev` 开发时，带有它的应用会以独立构建的方式运行，而不是在播放器应用里运行。

在 Windows 和 Linux 上，这个包的渲染器能绘制形状、纯色、空对象和预合成图层：矩形、椭圆、星形、多边形和路径；纯色或渐变（线性和径向，支持高光）的填充和描边，以及虚线；修剪路径、圆角和中继器；图层和组的变换（锚点、位置（可分离，可沿曲线运动）、缩放、旋转、倾斜、不透明度）、父子关系、时间重映射和时间伸缩；关键帧的缓动、定格和空间插值。蒙版（相加、相减、相交、反转）和轨道遮罩（Alpha 与亮度，以及反转）按轮廓裁剪。它不绘制图像图层和文本图层，不支持表达式、效果、图层样式、混合模式、3D、合并路径（路径按原样绘制）、偏移路径、收缩和膨胀、扭曲、锯齿；也不支持蒙版的不透明度、羽化和扩展，以及遮罩本身的透明度（遮罩按其图层中填充的轮廓裁剪，预合成遮罩按它的边界裁剪）。Web 上使用的 lottie-web 精简版同样不运行表达式。

## 网页

`ui.WebView` 用平台的 Web 视图显示网页，滚动、缩放和文本选择都是平台原生的：iOS 和 macOS 上是 `WKWebView`，Android 上是 `WebView`，Windows 上是 WebView2（Edge 的运行时），Linux 上是 WebKitGTK 6.0 的 `WebKitWebView`。Linux 上如果没有安装 WebKitGTK，它什么也不显示，并调用一次 `OnError`，告诉你需要安装什么。在浏览器中它是一个 `<iframe>`，下面介绍的功能只有对应用自己站点的页面才能全部使用；参见 [Web](web.md#web-视图)。没有设置高度也没有 `Grow` 时，它会扩展到填满可用空间。

```go
state := signal.New(ui.WebState{})
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	URL:       ui.Val("https://go.dev"),
	Block:     []string{"doubleclick.net", "/ads/"},
	OnState:   func(s ui.WebState) { state.Set(s) },
	OnMessage: func(m string) { fmt.Println("the page says", m) },
	OnNewWindow: func(url string) {
		ui.OpenURL(url)
	},
})
return ui.Column(ui.Style{Grow: 1},
	ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 12), Align: ui.AlignCenter},
		ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: ui.Val("Back"),
			Disabled: signal.Func(func() bool { return !state.Get().CanGoBack }),
			OnPress:  func() { ui.WebBack(web) }}),
		ui.Text(signal.Func(func() string { return state.Get().Title }), ui.Style{Grow: 1, Lines: 1}),
		ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Reload"), OnPress: func() { ui.WebReload(web) }}),
	),
	ui.Show(signal.Func(func() bool { return state.Get().Loading }), func() ui.Node {
		return ui.Progress(signal.Func(func() float64 { return float64(state.Get().Progress) }))
	}),
	web,
)
```

显示什么：

- `URL` 是 http(s) URL，或者应用 assets 中的文件（这时页面里的相对链接也指向 assets）。`HTML` 则是直接显示的标记，用来代替 URL。
- `UserAgent` 是请求页面时使用的 User-Agent，也是 `navigator.userAgent` 的值；留空时使用平台默认值。
- `Preload` 是在每个页面自身的脚本之前、在每个 frame 中运行的脚本，可以用来拦截 `fetch` 或设置全局变量。
- `Block` 阻止请求加载，就像广告拦截器那样。每一项可以是一个主机名，同时匹配它的子域名（`"doubleclick.net"`）；也可以包含 `/`，这时匹配 URL 中的一部分（`"/ads/"`）。

`Preload`、`UserAgent` 和 `Block` 在视图创建时就确定了。

报告什么：

- 每个页面加载完成时，`OnLoad` 会收到它的 URL；页面加载失败时，`OnError` 会收到错误信息。
- `OnState` 像地址栏一样跟踪页面的状态：一个 `ui.WebState`，包含 `URL`、`Title`、`Loading`、`Progress`（0 到 1）、`CanGoBack` 和 `CanGoForward`。
- `OnNewWindow` 收到页面想在新窗口中打开的 URL（`window.open`、`target="_blank"` 的链接）。没有设置它时，那个页面会直接在当前视图中加载。
- `OnDownload` 收到页面无法显示、应当保存为文件的响应（比如指向 `.zip` 的链接、`Content-Disposition: attachment`），形式是一个 `ui.WebDownload`，包含 `URL`、`MIME`、`Filename` 和 `Size`（未知时为 -1）。它本身并不会下载文件。
- `OnRequest` 收到页面发出的每个请求的 URL（脚本、图片、媒体、`fetch`）。**iOS 和 macOS 不支持**：`WKWebView` 不允许应用看到这些请求。

`ui.WebBack`、`ui.WebForward`、`ui.WebReload` 和 `ui.WebStop` 分别用于在历史记录中后退、前进，重新加载页面，以及停止加载。

### 与页面通信

页面通过 `window.asuka.postMessage(text)` 给 Go 发消息，这会调用 `OnMessage`。Go 这边可以用 `ui.EvalJS` 在页面中执行脚本，它不返回任何结果；也可以用 `ui.EvalJSResult`，它会以 JSON 的形式回调结果：最后一条语句的值、返回的 promise 解析出的值（`undefined` 会变成 `null`），或者抛出的错误。

```go
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	HTML: ui.Val(`<button onclick="asuka.postMessage('tapped')">Tap</button>`),
	OnMessage: func(m string) {
		ui.EvalJSResult(web, "document.title", func(v json.RawMessage, err error) {
			fmt.Println(m, string(v), err)
		})
	},
})
```

`ui.EvalJSResult` 借助页面的 `eval` 来等待 promise，而页面的内容安全策略（CSP）如果不允许 `'unsafe-eval'`，就会拦下 `eval`。在这样的页面上，iOS、Android、macOS 和 Linux 会改用 `ui.EvalJS` 的方式执行脚本，这种方式不受 CSP 限制：得到的仍是最后一条语句的值，出错时仍是脚本抛出的错误，只是不会等待 promise（Android 上得到 `{}`，其他平台报错），而且在 iOS 和 macOS 上，对象的键可能换了顺序。Windows 不需要 `eval`，照常等待 promise。在 Web 上，浏览器会对应用在页面里执行的脚本施加页面自己的策略，所以禁止 `eval` 的页面会同时拒绝 `ui.EvalJS` 和 `ui.EvalJSResult`。

`ui.WebCookies(url, done)` 获取 Web 视图中某个 URL 的 cookie，形式是一个 `Cookie` 请求头。这样用户在页面里登录之后，你自己发出的请求也能带上这份登录状态。

[`examples/downloader`](../../examples/downloader) 用到了以上全部功能：一个带地址栏、预加载脚本、请求拦截和下载功能的浏览器。

## 形状

`ui.Shape` 绘制一个轮廓，可以填充、描边，或者两者兼有。`Path` 根据形状的尺寸生成轮廓；尺寸变化时，以及它读取的 signal 变化时，`Path` 都会重新运行。形状的尺寸由样式决定（宽高、宽高比、`Grow`）；都没有设置时，它是空的。

```go
ui.Shape(ui.ShapeProps{
	Path: func(w, h float32) *ui.Path {
		return new(ui.Path).MoveTo(w/2, 0).LineTo(w, h).LineTo(0, h).Close()
	},
	Fill:  colors.Orange,
	Style: ui.Style{Width: ui.Pt(48), Height: ui.Pt(48)},
})
```

`ui.Path` 以点为单位，原点在形状的左上角。它的方法会往路径里添加内容并返回路径本身，方便链式调用：`MoveTo`、`LineTo`、`QuadTo`、`CubicTo`、`Arc`（角度用弧度，从 x 轴起顺时针计算）、`Close`，以及直接添加整个图形的 `Rect`、`RoundedRect` 和 `Ellipse`。

`Stroke` 和 `LineWidth`（默认为 1）用来描出轮廓，可以指定 `LineCap`（`CapButt`、`CapRound`、`CapSquare`）和 `LineJoin`（`JoinMiter`、`JoinRound`、`JoinBevel`）。`TrimStart` 和 `TrimEnd` 只绘制轮廓的一部分，取值 0 到 1，可以做成进度环，或是一条逐渐画出来的线。在 `ui.Animate` 中修改它们会产生动画（见[动效](motion.md)）。`Dash` 把描边变成虚线：一组以点为单位的长度，依次表示画多长、空多长（`[]float32{6, 4}`；个数为奇数时会重复一遍），和 SwiftUI 的 `StrokeStyle(dash:)` 一样。`DashPhase` 表示从图案的哪个位置开始画。[`examples/usage`](../../examples/usage) 就用虚线画了一段预测曲线。

```go
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

func progressRing(done signal.Accessor[float32]) ui.Node {
	box := ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pct(100), Height: ui.Pct(100)}
	return ui.View(ui.ViewProps{Label: ui.Val("Progress"), Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.SecondaryBackground, LineWidth: 8, Style: box}),
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.Accent, LineWidth: 8, LineCap: ui.CapRound,
			TrimEnd: done, Style: box}),
	)
}
```

### Canvas

`ui.Canvas` 用来绘制应用自己画的内容：图表、仪表盘、签名、游戏棋盘，任何没有现成视图可用的东西。它的函数通过 `*ui.Graphics` 绘制，坐标以点为单位，原点在左上角：

```go
ui.Canvas(func(g *ui.Graphics) {
	w, h := g.Size()
	g.Fill(g.Path().RoundedRect(0, 0, w, h, 12), colors.SecondaryBackground)
	g.Fill(g.Path().RoundedRect(0, 0, w*level.Get(), h, 12), colors.Accent)
	g.Text(fmt.Sprintf("%.0f%%", level.Get()*100), w/2, h/2+6, ui.TextStyle{Weight: fonts.Semibold, Align: ui.TextCenter})
}, ui.Style{Height: ui.Pt(36)})
```

- `g.Path()` 返回一个待构建的路径（和 Shape 用的一样），在多次绘制之间复用。
- `Fill`、`Stroke`（接受一个 `StrokeStyle`：线宽、线帽、连接方式、虚线）和 `FillGradient` 用来绘制路径；`FillRadialGradient` 用 `RadialGradient`（圆心、半径，以及渐变起始的焦点）填充路径，`StrokeGradient` 和 `StrokeRadialGradient` 用渐变描边；`Text` 绘制一行文字，基线位于 y，左边缘、中心或右边缘位于 x。
- `Save` 和 `Restore` 成对使用，把 `Translate`、`Scale`、`Rotate`（角度制，顺时针）、`Transform`（任意仿射矩阵，包括倾斜）、`Clip`、`Opacity` 和 `EvenOdd` 包在中间；`EvenOdd` 让填充和裁剪按奇偶规则进行（默认是非零规则）。
- 颜色可以使用语义颜色（`colors.Label`、`colors.Separator`、`colors.Accent`），它们会跟随深色模式变化。

布局完成后，Canvas 会绘制一次；之后尺寸变化，或函数读取过的 signal 变化时，它会重新绘制，就像一个 effect。每次绘制都会记录一个显示列表（display list），只有和上一次不同时才会发送出去，再由平台回放：iOS 和 macOS 上用 Core Graphics，Android 上用 `Canvas`，Windows 上用 GDI+，Linux 上用 GTK 的 snapshot（GSK），浏览器中用 2D `<canvas>`。只要这个 canvas 之前已经画过同样多的内容，记录过程就不会分配内存。Canvas 本身不响应点击：请把它包在设置了 `OnPress` 或 `OnPan` 的 `ui.View` 里，回调给出的坐标就在它的坐标系中。屏幕阅读器不会读取 Canvas：请给外层视图设置 `Label`。

### 图表

`asuka/sdk/chart` 在 Canvas 上绘制图表：基于数值轴的柱状图、折线图、面积图和散点图，标签使用平台的文字绘制（由平台测量，并随用户的字号缩放）。

```go
chart.View(chart.Props{
	Series: ui.Val([]chart.Series{
		{Name: "Steps", Kind: chart.Bar, Values: steps},
		{Name: "Goal", Kind: chart.Line, Values: goal, Color: colors.Hex(0xff9500)},
	}),
	Labels: ui.Val([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}),
	Label:  ui.Val("Steps this week"),
	Style:  ui.Style{Height: ui.Pt(200)},
})
```

- 每个系列为每个标签提供一个值；`NaN` 表示此处留空。柱状系列并排摆放，面积、折线和散点则画在它们上面。`Smooth` 绘制单调曲线，曲线不会越过数据点的值。
- 数值轴会根据数据自动确定范围，并向外取整到完整的刻度（柱状图和面积图还会向下延伸到 0），除非用 `Min` 和 `Max` 固定范围；`Format` 决定刻度标签的格式（默认形如 `2.5k`）。
- 系列的颜色默认先取应用的强调色，之后依次取系统颜色。
- 屏幕阅读器会把整个图表当作一张图片来读：先读 `Label`，再完整读出每个系列的值。

### 着色器

`ui.Shader` 是一个由 GPU 程序绘制的框，绘制在子视图下层，可以用来做流动的渐变、噪声、光晕、波纹等效果。程序是一个 Kage 着色器（Ebitengine 的着色语言，语法和 Go 一样），由 `shader` 包编译，作为包级变量只编译一次：

```go
var glow = shader.MustCompile(`package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	d := distance(pos/rectSize(), vec2(0.5))
	g := smoothstep(0.5, 0, d) * (0.8 + 0.2*sin(Time*2))
	return vec4(Tint.rgb*g, 1) * Tint.a
}`)

ui.Shader(ui.ShaderProps{
	Program:  glow,
	Uniforms: ui.Val([]ui.Uniform{ui.ColorUniform("Tint", colors.Hex(0x38bdf8))}),
	Animated: true,
	Style:    ui.Style{Height: ui.Pt(200), CornerRadius: 16, Clip: true, Background: colors.Hex(0x0b1030)},
})
```

- `Fragment` 返回一个像素的颜色（预乘 alpha），红、绿、蓝和 alpha 的取值都在 0 到 1 之间。`pos` 是像素中心相对于框左上角的位置，单位是点；`dst` 是同一位置，单位是像素。`rectSize()` 是框的尺寸（以点为单位），`pixelScale()` 是每个点对应的像素数。
- 这门语言支持 `bool`、`int`、`float`、`vec2` 到 `vec4`、`ivec2` 到 `ivec4`、`mat2` 到 `mat4` 以及它们的数组，支持 GLSL 的内置函数（`sin`、`mix`、`smoothstep`、`dot` 等）、自定义函数，以及 `if` 和 `for`。导出的变量就是 uniform。
- `Uniforms` 按名称设置 uniform：标量给一个数，向量依次给出各个分量，矩阵则按列依次给出。修改 uniform 会让框重新绘制；没有给出的 uniform 为 0。
- `var Time float` 是着色器显示以来经过的秒数。`Animated` 让着色器持续运行：显示器每绘制一帧，这个框就绘制一次，而 Go 这边什么都不需要运行。
- `shader.Compile` 会报告着色器中的错误及其所在行，并且在所有平台上都会同样指出 Android 无法运行的部分：AGSL 采用的是 GLSL ES 1.00 的语言，因此不支持无符号整数、位运算、整数取余（浮点数的 `mod` 可以用）、导数，以及上下界不是常量的循环。

它在 iOS 和 macOS 上使用 Metal（一个 `CAMetalLayer`），在 Android 上使用 AGSL（一个 `RuntimeShader`），在浏览器中使用 WebGL 2（程序会被翻译成 GLSL ES 3.00），在 Windows 上使用 Direct3D 11（程序会被翻译成 HLSL；没有 GPU 时用系统自带的软件光栅化器 WARP），在 Linux 上使用 OpenGL（程序会被翻译成 GLSL，在 GTK 的上下文中绘制到纹理里，再由 GTK 显示）。在 Windows 上，每一帧都在主线程之外渲染，准备好之后才显示。Linux 上如果没有 GL（没有驱动，或者设置了 `GDK_DISABLE=gl`），同一个程序会改在 CPU 上运行，分摊到多个核心，并且在主线程之外执行；这时较大或带动画的框会以较低的分辨率渲染，再放大显示。请给框设置一个 `Background`，在第一帧出来之前先顶上。

## 容器

`ui.View` 用来容纳其他视图。`ui.Column` 和 `ui.Row` 也是视图，分别把子视图从上到下、从起始边到结束边依次排列。`ui.SafeArea` 是一个列，会为刘海、系统栏和键盘留出内边距。它们如何确定子视图的尺寸和位置，请看[布局](layout.md)。

如果一个视图没有外观、事件和无障碍相关的属性，它就不会生成自己的原生视图，子视图会直接放进最近的、确实有原生视图的那个祖先里。所以多层嵌套行和列不会带来任何开销。

```go
open := signal.New(false)
return ui.View(ui.ViewProps{
	OnPress:     func() { open.Set(!open.Peek()) },
	OnLongPress: func() { ui.Haptic(ui.HapticMedium) },
	Label:       ui.Val("Ada, 2 unread"),
	Style:       ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12},
},
	ui.Symbol(ui.IconPerson.Filled()),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada", ui.Style{Font: fonts.Headline}),
		ui.StaticText("2 unread", ui.Style{Color: colors.SecondaryLabel}),
	),
)
```

`ViewProps` 包括：

- `OnPress` 和 `OnLongPress`。
- 手势，使用平台的手势识别器：`OnPan`、`OnPinch`、`OnRotate`（各自收到一个 `ui.Gesture`）和 `OnSwipe`（收到一个 `ui.SwipeDirection`）。参见[动效](motion.md)。
- 交互状态。视图自己的样式通过样式变体（`Style.Pressed`、`Hovered`、`Dragging`、`DragOver`）响应这些状态；其他视图则通过 `Interaction`（一个 `ui.Interaction`，用 `Style.When` 或在 `StyleFunc` 中读取）来响应。副作用写在 `OnPressedChange` 和 `OnHoverChange` 里。拖放：`Drag`（一个 `ui.DragSource`）和 `Drop`（一个 `ui.DropTarget`）。参见[动效](motion.md#按下与悬停状态)。
- `OnLayout`：视图布局完成后，以及之后每次位置或尺寸变化时，都会以视图的 `ui.Frame` 调用它。
- 无障碍：`Label`、`Hint`、`Role`、`Decorative`、`Actions` 和 `OnAdjust`。参见[无障碍](accessibility.md)。

### Scroll

`ui.Scroll` 沿一个方向滚动它的子视图。你需要给它一个尺寸，或者设置 `Grow`，它才有可供滚动的范围。`Style` 是滚动视图本身的框；`ContentStyle` 是子视图在其中排列的那个框。

```go
down := signal.New(false)
var scroll ui.Node
scroll = ui.Scroll(ui.ScrollProps{
	Style:    ui.Style{Grow: 1},
	OnScroll: func(_, y float32) { down.Set(y > 300) },
}, ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16}, content...))
return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, scroll,
	ui.Show(down, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Right: ui.Pt(20), Bottom: ui.Pt(20)}},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Back to top"), Variant: ui.ButtonFilled,
				OnPress: func() { ui.ScrollTo(scroll, 0, 0, true) }}))
	}))
```

- 滚动过程中会调用 `OnScroll`，参数是内容的偏移量（以点为单位）。
- `ui.ScrollTo(scroll, x, y, animated)` 滚动到指定偏移（超出末尾就停在末尾）；`ui.ScrollToEnd(scroll, animated)` 滚动到底部，横向滚动视图则滚到结束边。两者同样适用于 `List`。
- `Horizontal` 改为横向滚动：

```go
ui.Scroll(ui.ScrollProps{Horizontal: true, ContentStyle: ui.Style{Gap: 8, Padding: ui.Symmetric(0, 16)}}, kids...)
```

**分页。** `Paging` 让滚动只停在整页的位置，适合轮播图、新手引导的各个步骤、全屏视频流等。内容的每个子视图就是一页，在滚动方向上和滚动视图一样大。`Page` 是当前显示的页：滑动停稳时会写入它，反过来写入它也会滚动到对应的页。`ui.PageControl` 用每页一个圆点表示当前位置，点击圆点也能切换页面：

```go
page := signal.New(0)
return ui.Column(ui.Style{Gap: 8},
	ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(200)}},
		card(1), card(2), card(3)),
	ui.PageControl(ui.PageControlProps{Count: ui.Val(3), Page: page}))
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| 分页 | `isPagingEnabled` | 每次滑动吸附到下一页，或回到原来的页 | 滑动结束时停在最近的一页 | 滚轮每滚一格翻一页（同 FlipView） | 滚轮每滚一格翻一页；用触控板或触摸屏滑动后停在某一页；方向键、Page Up 和 Page Down 每次翻一页 | CSS 滚动吸附 |
| `PageControl` | `UIPageControl` | 主题主色的圆点（Material 没有这种控件） | 强调色的圆点 | 圆点指示器（同 PipsPager） | 按 libadwaita 风格绘制的圆点，当前页的圆点更大、颜色更深 | 圆点，当前页使用标签颜色 |

页码从阅读的起始方向开始计算：在从右到左的布局中，第一页在最右边。滚动视图的尺寸变化时（比如旋转屏幕），它会停留在当前页。屏幕阅读器会在圆点上读出“第 2 页，共 3 页”，并且可以调节它们。

**下拉刷新。** 设置 `OnRefresh` 即可开启（`UIRefreshControl`、`SwipeRefreshLayout`）。刷新期间把 `Refreshing` 设为 true；它变回 false 时，加载指示器随之隐藏。如果没有设置 `Refreshing`，`OnRefresh` 一返回，指示器就会隐藏。在 macOS 上，用触控板或妙控鼠标滚动可以把内容拉过顶部，和 iOS 上一样。Windows 和 Linux 采用桌面端的刷新方式：按 F5（Linux 上 Ctrl+R 也可以）会刷新焦点所在的滚动视图，没有的话就刷新屏幕上最内层的那个，刷新期间在它的顶部显示加载指示器。在浏览器中，用手指下拉即可刷新。

```go
refreshing := signal.New(false)
return ui.Scroll(ui.ScrollProps{
	Style:      ui.Style{Grow: 1},
	Refreshing: refreshing,
	OnRefresh: func() {
		refreshing.Set(true)
		go func() {
			time.Sleep(time.Second) // 拉取数据
			ui.Post(func() { refreshing.Set(false) })
		}()
	},
}, ui.StaticText("Pull down"))
```

**键盘上方的工具栏。** `Accessory` 是输入框获得焦点时显示在键盘上方的一条栏，可以放字数统计和“完成”按钮、格式按钮、快捷回复等（类似 SwiftUI 的键盘工具栏）。它和窗口一样宽，高度由内容决定，随输入框一起创建，也随输入框一起销毁。

```go
var field ui.Node
field = ui.TextField(ui.TextFieldProps{Value: note, Multiline: true, Accessory: func() ui.Node {
	return ui.Row(ui.Style{Padding: ui.Symmetric(6, 16), Align: ui.AlignCenter, Material: ui.MaterialBar},
		ui.Text(signal.Func(func() string { return fmt.Sprint(len(note.Get()), " characters") }), ui.Style{Grow: 1}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Done"), OnPress: func() { ui.Blur(field) }}))
}})
```

在 iOS 上，它就是输入框的 `inputAccessoryView`：跟随键盘移动，交互式收起键盘时也一样。Android 没有对应的机制：这条栏会放在输入框所在的窗口里（输入框在弹出面板里时也是如此），贴在键盘顶部，逐帧跟随键盘。macOS、Windows 和 Linux 没有屏幕键盘，因此不会显示它；浏览器只在触摸屏上显示。

**富文本。** `Rich` 把输入框变成编辑带样式文本的文本区域，适合邮件正文或带格式的消息：支持粗体、斜体、下划线、删除线、等宽代码和链接（片段原有的颜色和文本样式会原样保留）。它和 `RichText` 显示的是同一种 `ui.Rich`。用户的输入和格式操作会写入它；写入它则会替换文本。`ui.ToggleFormat(field, ui.FormatBold)` 的规则是：只要选区中还有文字没有这个样式，就给整个选区加上，否则就去掉（`FormatText` 则直接指定开或关）；没有选区时，它作用于接下来输入的文字。`ui.SetLink(field, url)` 给选区加上链接（传入 "" 则移除链接；没有选区时，作用于光标所在的链接）。`OnFormats` 报告光标处的样式，或者整个选区都具有的样式，供工具栏显示当前状态。

```go
body := signal.NewFunc(ui.Rich{ui.Plain("Hi,\n\nThe "), ui.Strong("1.0"), ui.Plain(" build is ready.")}, nil)
formats := signal.New(ui.TextFormats{})
var field ui.Node
field = ui.TextField(ui.TextFieldProps{Rich: body, Plain: true, OnFormats: formats.Set, Style: ui.Style{Grow: 1}})
bold := ui.Button(ui.ButtonProps{Icon: "bold", Label: ui.Val("Bold"),
	Style:   ui.StyleFunc(func() ui.Style { if formats.Get().Has(ui.FormatBold) { return ui.Style{Background: colors.SecondaryBackground} }; return ui.Style{} }),
	OnPress: func() { ui.ToggleFormat(field, ui.FormatBold) }})
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| 视图 | `UITextView` 及其 attributed text | `EditText` 及其 span | 富文本模式的 `NSTextView` | rich edit 控件（`RICHEDIT50W`），通过 RTF 读写 | `GtkTextView` 及其缓冲区中的 tag | `contenteditable` 元素 |
| 用户设置格式 | 编辑菜单中的 B/I/U | 实体键盘的 Ctrl+B、Ctrl+I、Ctrl+U | “格式”菜单，⌘B ⌘I ⌘U | Ctrl+B、Ctrl+I、Ctrl+U | Ctrl+B、Ctrl+I、Ctrl+U | 浏览器的 ⌘B ⌘I ⌘U（其他系统上为 Ctrl） |
| 撤销格式修改 | 支持 | 支持：使用输入框自己的历史记录（Android 自带的只记录文本），Ctrl+Z、Ctrl+Shift+Z、Ctrl+Y | 支持 | 支持 | 使用 GTK 的历史记录（Ctrl+Z、Ctrl+Shift+Z），只记录文本 | 支持：使用页面的编辑命令 |

[`examples/mail`](../../examples/mail) 中有一个带工具栏的邮件编辑器。

**内边距。** 文本或输入框的 `Padding`（以及 `BorderWidth`）位于它的框之内、内容的四周，和 CSS 一样：背景会铺满这部分区域，文字绘制在它的内侧，输入框的编辑区域也在它的内侧。

## 列表

`ui.List` 使用平台的可复用列表（`UICollectionView`、`RecyclerView`），为每个数据项显示一行。桌面端和 Web 没有能把任意视图作为行的列表控件，所以在那里由宿主在平台的滚动视图中自行复用行。只有屏幕上的单元格才会创建行；滚出屏幕的单元格会被复用来显示另一个数据项，并且只更新有差异的属性。[`examples/list10k`](../../examples/list10k) 有一万行。

```go
messages := signal.NewFunc(fetch(0, 30), nil)
return ui.List(ui.ListProps{}, messages, func(m message) int { return m.ID }, row)
```

```go
func row(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
	return ui.Column(ui.Style{Gap: 2, Padding: ui.Symmetric(12, 20)},
		ui.Text(signal.Func(func() string { return m.Get().From }), ui.Style{Font: fonts.Headline}),
		ui.Text(signal.Func(func() string { return m.Get().Subject }), ui.Style{Color: colors.SecondaryLabel}),
	)
}
```

它接受数据项、为每项生成 key 的函数，以及一个根据数据项和索引构建行的函数（数据项和索引都是访问器）。key 用来在列表前后两次变化之间认出每一项：某一项变了，就更新它的行；某一项移动了，它的行也跟着移动；平台会用动画呈现这些差异。像上面那样在 `signal.Func` 中读取数据项，被复用的行才能跟上它新绑定的数据项。

列表会填满分配给它的空间：没有设置高度也没有 `Grow` 时，它会自动扩展。行与行之间会绘制平台的分隔线。

`ListProps` 包括：

- `SwipeActions`：第 `i` 行的操作，滑动该行时显示。在 iOS 上，它们是行的尾部操作，破坏性操作显示为红色，完整滑到底会执行第一个操作。在 Android 上，完整滑动会执行第一个操作，滑动时露出它的颜色和图标，就像 Material 的滑动删除。在 macOS、Windows、Linux 和 Web 上，它们会出现在行的上下文菜单中（右键点击；Linux 和 Web 上长按也可以），先列出前部操作，再列出尾部操作。请把同样的操作也设为行的无障碍 `Actions`，供屏幕阅读器使用。
- `OnEndReached`：最后几行即将显示时调用，对于列表的同一个长度只会触发一次。在这里加载更多数据。
- `OnRefresh` 和 `Refreshing`：下拉刷新，用法与 `Scroll` 相同（Windows 和 Linux 上按 F5）。
- `OnScroll`：与 `Scroll` 相同。
- `Appearance`：列表的外观。`ui.ListPlain` 的行铺满整个宽度，彼此以分隔线隔开；`ui.ListInsetGrouped` 是平台的分组列表（Linux 上是 libadwaita 的 boxed list）；`ui.ListBare` 没有分隔线，行的后面也没有任何背景，列表背后的内容会透出来，适合那些自己绘制卡片的行。bare 列表的行间距由行自己负责，而且行依然可以滑动。

```go
messages := signal.NewFunc(fetch(0, 30), nil)
remove := func(i int) {
	messages.Update(func(ms []message) []message { return slices.Delete(slices.Clone(ms), i, i+1) })
}
list := ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
			{Title: ui.Val("Archive"), Icon: "archivebox", OnPress: func() { remove(i) }},
		}
	},
	OnEndReached: func() {
		ms := messages.Peek()
		messages.Set(append(slices.Clone(ms), fetch(len(ms), 30)...))
	},
}, messages, func(m message) int { return m.ID }, row)
```

`LeadingSwipeActions` 通过反方向滑动显示，也就是从行的起始边滑出（比如邮件里的“未读”和“置顶”）：第一个操作紧贴那条边，也是完整滑动时执行的那个。iOS 会给它们着色；Android 把它们显示为 Material 的填充按钮。

`ui.ScrollToItem(list, index, animated)` 滚动列表，让某一项显示出来；`ScrollTo` 和 `ScrollToEnd` 也同样适用。

### 聊天：锚定在底部的列表

`Anchor: ui.AnchorBottom` 就是聊天界面用的列表：打开时显示最后一行；行数不够填满时，行贴着底部排列；只要停在末尾，就会一直保持在末尾，无论是末尾新增了行、行高发生了变化，还是列表变矮了（键盘弹出）。用户向上滚动就会离开末尾；滚回末尾，或调用 `ui.ScrollToEnd`，又会重新粘住。`OnStartReached` 在最前面几行即将显示时调用，对每个不同的首项只触发一次：在这里加载更早的消息。

无论锚定在哪一端，在屏幕上可见的行之上添加或删除行，都不会让这些可见的行发生位移（即 React Native 的 `maintainVisibleContentPosition`）：无论是在顶部加载了更早的消息，还是删掉了上方的某一项，用户正在看的内容都不会跳走。只有在列表锚定的那一端，新增的行才会直接显示出来：从顶部开始阅读的列表是在顶部（比如来了一封新邮件），聊天列表则是在末尾。

```go
msgs := signal.NewFunc(latest(40), nil)
var list ui.Node
list = ui.List(ui.ListProps{
	Anchor:         ui.AnchorBottom,
	OnStartReached: func() { go loadOlder() }, // 加载完成后，再通过 ui.Post 把它们插到前面
}, msgs, func(m message) int { return m.ID }, bubble)
send := func(text string) {
	msgs.Set(append(slices.Clone(msgs.Peek()), message{Text: text}))
	ui.ScrollToEnd(list, true) // 不管列表当前滚到哪里，都让自己刚发的消息显示出来
}
```

[`examples/chat`](../../examples/chat) 是一个完整的对话界面。

### 分组

`Section` 把行分成若干组：相邻且 key 相同的行属于同一组，组的最上方是 `Header` 返回的视图（比如聊天里的日期、通讯录里的首字母、邮箱里的“今天”和“昨天”）。`Header` 以访问器的形式拿到 key：组头本身也是一行，进入可视区域时才创建，之后可能会被复用给另一个分组。

```go
ui.List(ui.ListProps{
	Section: func(i int) string { return day(msgs.Peek()[i].At) },
	Header: func(day signal.Accessor[string]) ui.Node {
		return ui.Text(day, ui.Style{Font: fonts.Footnote.Weighted(fonts.Semibold), Padding: ui.Symmetric(6, 20)})
	},
}, msgs, key, row)
```

在 plain 或 bare 列表中，组头会停留在顶部，让本组的行从它下面滚过，直到被下一个组头推上去（即 UITableView 的 plain 样式；Android 没有这种机制，所以用的是浮在列表上方的一个单元格，绑定到当前位于顶部的分组；桌面端和 Web 上则就是组头行本身）。plain 列表的组头使用列表的背景；bare 列表的组头自己绘制背景（比如聊天中浮在消息上方的胶囊标签）。inset grouped 列表的每个分组各自成为一组，位于自己的组头之下，组头随分组一起滚动。

### 选择行

`Selecting` 让列表进入选择模式（就像邮件里的“编辑”）：每一行显示平台的选择控件，点击一行会选中或取消选中它，而不再触发它的点击事件，行也不能滑动。`Selected` 指明哪些行被选中（它在一个 effect 中被跟踪，所以保存在 signal 里的选择状态一变，界面就会跟着变），`OnSelect` 接收用户的选择操作；应用没有采纳的选择会恢复原状。行在布局时会变窄，为选择控件让出空间。

```go
selecting := signal.New(false)
chosen := signal.NewFunc(map[int]bool{}, nil) // 以消息 ID 为键
ui.List(ui.ListProps{
	Selecting: selecting,
	Selected:  func(i int) bool { return chosen.Get()[msgs.Peek()[i].ID] },
	OnSelect: func(i int, v bool) {
		m := maps.Clone(chosen.Peek())
		if v {
			m[msgs.Peek()[i].ID] = true
		} else {
			delete(m, msgs.Peek()[i].ID)
		}
		chosen.Set(m)
	},
	OnSelectBegin: func(i int) { selecting.Set(true) },
}, msgs, key, row)
```

| | iOS | Android | macOS、Windows、Linux | Web |
|---|---|---|---|---|
| 选择控件 | UIKit 编辑模式下的圆圈 | 行末尾的 Material 复选框 | 高亮整行（使用强调色对应的选中颜色；Windows 上是 WinUI 列表的胶囊形指示条） | 行起始边的圆圈，选中时填上对勾，整行着色 |
| `OnSelectBegin` | 双指在行上拖动，并继续选中拖过的行 | 长按（同 Gmail） | — | — |
| 屏幕阅读器 | 每一行都可选择 | 每一行是一个复选框，以行的文字作为标签 | — | 每一行标明是否选中（`aria-selected`） |

### 网格

`Columns` 把列表变成网格（类似 `LazyVGrid`、`GridLayoutManager`）：数据项按指定的列数逐行排列，每项宽度相同，横向和纵向都间隔 `Gap`。每一行的高度取其中最高的那一项。网格没有分隔线，也不支持滑动操作。

```go
ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
	func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
		return ui.Image(ui.ImageProps{
			Source: signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
			Mode:   ui.ImageFill,
			Style:  ui.Style{AspectRatio: 1},
		})
	})
```

### 在 Scroll 中用 List 还是 For

`ui.For` 同样为每一项生成一行，key 的用法也一样，但它会一次性生成所有行，作为普通视图和兄弟视图并列。如果 `Scroll` 中只是页面的一小部分需要列表，比如几个标签、表单里的几行、标题下的一个小节，就用 `For`。

```go
ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.All(20), Gap: 8}},
	ui.StaticText("Tags", ui.Style{Font: fonts.Headline}),
	ui.For(tags, func(t string) string { return t }, func(t signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
		return ui.Text(t)
	}),
)
```

如果数据项很多或者数量没有上限（信息流、收件箱、搜索结果、照片），或者你需要滑动操作、分隔线、滚到末尾加载更多、网格布局，就用 `List`。`List` 自己就能滚动，不要把它放进 `Scroll`。

## 自定义视图

如果某个平台有 Asuka 没有提供的控件，你可以自己实现，再用 `ui.Native` 显示出来：iOS 上用 Swift，Android 上用 Kotlin，macOS、Windows 和 Linux 上用 Go（通过 `native` 包调用 AppKit、Win32 和 GTK），Web 上用 JavaScript。参见[原生代码](native.md)。

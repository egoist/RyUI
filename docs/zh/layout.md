# 布局与样式

`ui.Style` 决定一个视图如何布局、长什么样。布局采用 CSS flexbox，单位是点（point）；外观则包括颜色、圆角、边框、阴影和文字。本页会介绍这两部分，以及如何让样式随状态变化。布局在所有平台上都一样：Asuka 在 Go 里算好布局，再把平台的视图摆到对应的位置上。

## 平台默认值

没有设置的样式会沿用平台的默认值。一个完全不设样式的应用，看起来就和这个平台自带的应用一样，字体、颜色、深色模式和文字大小都与之一致：

- **iOS**：UIKit 的风格。
- **Android**：Material 3 的风格。
- **macOS**：AppKit 的风格。
- **Windows**：Windows 11 的风格（WinUI 的 Fluent 外观，由宿主在 Win32 控件上绘制）。
- **Linux**：GNOME 的风格（GTK 4 和 libadwaita）。
- **Web**：浏览器自带的控件，外围配上 Asuka 自己的一套浅色和深色配色（见 [Web](web.md)）。

只设置你真正想改的部分；颜色和字体尽量用下面介绍的语义颜色和语义字体，而不是写死的值，这样应用才能一直跟随平台。

## 样式

`ui.Style` 就是一个普通的结构体。组件接收的样式类型是 `Styler`：可以是一个 `Style`，也可以是一个 `ui.StyleFunc`，用于会变化的样式（见[会变化的样式](#会变化的样式)）。

```go
ui.Text(title, ui.Style{Font: fonts.Headline, Color: colors.SecondaryLabel})
ui.Button(ui.ButtonProps{Title: ui.Val("Follow"), Style: ui.Style{Shrink: ui.NoShrink}})
ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1), Background: colors.Separator}})
```

视觉属性只在原生控件支持的地方生效：按钮会把 `Color` 当作着色（tint），而视图会忽略 `Font`。

## 排列视图

`ui.Column` 把子视图从上到下排列，`ui.Row` 则从行首排到行尾。`ui.View` 是一个容器，排列方向由样式中的 `Direction` 决定：默认是 `ui.ColumnDir`，也可以设为 `ui.RowDir`。样式传 nil 等同于传一个空样式。

```go
ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12},
	Avatar(),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada Lovelace", ui.Style{Font: fonts.Headline}),
		ui.StaticText("Notes on the Analytical Engine", ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel, Lines: 1}),
	),
	ui.Button(ui.ButtonProps{Title: ui.Val("Follow"), Style: ui.Style{Shrink: ui.NoShrink}}),
)
```

只设置了布局属性的容器不会生成原生视图：它的子视图会被直接放进最近的那个有原生视图的祖先里。因此多嵌套几层也没有任何开销。

## Flexbox

布局遵循 CSS flexbox，默认值与 React Native 相同：默认按列排列，尺寸包含内边距和边框。

- **`Gap`**：子视图之间的间距；换行时也是行与行之间的间距。
- **`Justify`** 决定子视图在主轴上的分布：`ui.JustifyStart`（默认）、`ui.JustifyCenter`、`ui.JustifyEnd`、`ui.JustifySpaceBetween`、`ui.JustifySpaceAround`、`ui.JustifySpaceEvenly`。
- **`Align`** 决定子视图在交叉轴上的对齐方式：`ui.AlignStretch`（默认，子视图撑满交叉轴）、`ui.AlignStart`、`ui.AlignCenter`、`ui.AlignEnd`、`ui.AlignBaseline`。
- **`AlignSelf`** 为单个子视图覆盖父视图的 `Align`；零值 `ui.AlignAuto` 表示沿用父视图的设置。
- **`Wrap`** 允许子视图在放不下时折到下一行。
- **`Grow`** 表示子视图在父视图主轴上分得的剩余空间份额。会伸展的子视图同样可以缩到比内容更小，所以把 `Scroll` 包在一个设置了 `Grow` 的视图里时，内容会正常滚动，而不会把这个视图撑到滚动内容的完整高度。
- **`Shrink`** 表示一行放不下时子视图让出多少空间。零值 0 表示使用默认值 1：文字会换行，控件则保持其最小尺寸。`ui.NoShrink` 让子视图保持原有尺寸。
- **`Basis`** 是子视图在主轴上的初始尺寸；默认值 Auto 表示取内容的尺寸。

```go
// 两个盒子分摊宽度，其中一个是另一个的两倍。
ui.Row(ui.Style{Gap: 8, Height: ui.Pt(60)},
	ui.View(ui.ViewProps{Style: ui.Style{Grow: 2, Background: colors.SecondaryBackground}}),
	ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Background: colors.SecondaryBackground}}),
)

// 标签和数值分居两端。
ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter},
	ui.StaticText("Total"),
	ui.StaticText("$42", ui.Style{Font: fonts.Title2}),
)

// 放不下时会折行的标签。
ui.Row(ui.Style{Wrap: true, Gap: 8},
	ui.StaticText("Go"), ui.StaticText("iOS"), ui.StaticText("Android"),
)
```

## 尺寸

长度用 `ui.Dim` 表示：

- `ui.Pt(v)`：以点为单位。对应 iOS 和 macOS 的点、Android 的 dp、Windows 上 100% 缩放时的一个像素（1/96 英寸）、Linux 上 GTK 的逻辑像素，以及 Web 上的 CSS 像素。
- `ui.Pct(v)`：父视图在同一方向上尺寸的百分比。
- `ui.Auto`（零值）：取内容的尺寸，或由 flexbox 分配。

`Width` 和 `Height` 设置尺寸；`MinWidth`、`MinHeight`、`MaxWidth` 和 `MaxHeight` 限定尺寸的范围。`AspectRatio` 是宽高比（宽除以高）：已知一边时，它会算出另一边。

```go
ui.View(ui.ViewProps{Style: ui.Style{
	Width:       ui.Pct(50),
	MinWidth:    ui.Pt(120),
	MaxWidth:    ui.Pt(320),
	AspectRatio: 16.0 / 9,
	Background:  colors.Gray,
}})
```

文字和控件会用平台自己的测量方式算出自身尺寸，只有想改变它们的大小时才需要指定尺寸。

一个点对应多少像素取决于屏幕的缩放比例：手机上是 2 或 3，Retina 屏的 Mac 上是 2，Windows 和 Linux 上取显示器的缩放设置（150% 时是 1.5），Web 上则是浏览器的设备像素比。具体是多少可以看 `ui.UseEnvironment()` 的 `Scale`。把桌面窗口拖到缩放比例不同的显示器上时，窗口会按新显示器的比例绘制，而以点为单位的布局保持不变。

## 内边距与外边距

`Padding` 和 `Margin` 都是 `ui.Edges`，可以分别设置每一边，单位是点：

- `ui.All(v)`：四边都是 v。
- `ui.Symmetric(v, h)`：上下为 v，左右为 h。
- `ui.Edges{Top: 24}`：按名字逐边指定。

```go
ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Margin: ui.Edges{Top: 24}},
	ui.StaticText("Padded"),
)
```

在从右到左书写的语言里，`Row` 从右侧开始排列，`Justify` 和 `Align` 也随之从右侧算起，文字的行首在右边。`Left` 和 `Right` 则和 CSS 一样，说左就是左、说右就是右。如果想把间距放在文字开始或结束的那一侧，请用 `Start` 或 `End`：

```go
ui.Style{Padding: ui.Edges{Start: 16, End: 8}} // 英语里左边是 16，阿拉伯语里则是右边
```

参见[多语言](i18n.md)。

## 绝对定位

`Absolute` 让视图脱离文档流，再用 `Top`、`Right`、`Bottom` 和 `Left` 指定它在父视图中的位置。下面的例子在头像的角上放了一个徽标：

```go
ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(44), Height: ui.Pt(44)}},
	Avatar(),
	ui.View(ui.ViewProps{Style: ui.Style{
		Absolute: true, Top: ui.Pt(-4), Right: ui.Pt(-4),
		Width: ui.Pt(18), Height: ui.Pt(18), CornerRadius: 9,
		Background: colors.Red, Align: ui.AlignCenter, Justify: ui.JustifyCenter,
	}}, Badge()),
)
```

排在后面的子视图会画在前面的子视图之上。

## Grid

设置了 `Grid` 的容器会按 CSS grid 排列子视图。网格轨道由 `layout` 包提供：`layout.Fr`、`layout.Px`、`layout.Repeat`、`layout.Minmax` 等。子视图通过 `GridColumn` 和 `GridRow` 跨越多条轨道。

```go
ui.View(ui.ViewProps{Style: ui.Style{Gap: 8, Grid: &ui.Grid{
	Columns: []layout.GridTrack{layout.Repeat(3, layout.Fr(1))},
}}},
	Avatar(), Avatar(), Avatar(), Avatar(),
	ui.View(ui.ViewProps{Style: ui.Style{GridColumn: layout.Placement{Start: layout.Span(3)}, Height: ui.Pt(20), Background: colors.Gray}}),
)
```

如果是内容很多、需要滚动的网格，请使用带 `Columns` 的 `ui.List`（见[组件](components.md)）。

## 安全区域、键盘与窗口

在手机上，根视图会铺满整个屏幕，延伸到状态栏、刘海和主屏幕指示条下面。`ui.SafeArea` 是一个 `Column`，它在自身内边距的基础上，再加上安全区域的边距。键盘弹出时，它的底部内边距就是键盘的高度，所以放在底部的内容（比如文本框、发送按钮）始终会留在键盘上方。

```go
ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12},
	ui.StaticText("Todos", ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
	ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, rows...),
	composer,
)
```

这些边距取自它所在的那个页面：在导航栈的页面、标签页或弹出面板（sheet）里，就是那个页面的边距。如果要自己摆放内容，可以通过 `ui.UseEnvironment` 拿到这些边距和键盘高度（见[核心概念](concepts.md)）。

在 macOS、Windows 和 Linux 上，根视图铺满窗口的内容区域，位于标题栏（Linux 上是 header bar）下方。桌面窗口没有安全区域，也没有会遮住窗口的屏幕键盘：边距和键盘高度都是 0，`ui.SafeArea` 就是一个只带自身内边距的 `Column`。不过还是建议照样使用它，这样同一个页面也能直接适配手机。

除非应用另行指定，窗口大小是 800×600 点。可以用 `app.RunWindow` 设置窗口的尺寸，以及用户最小能把它缩到多大：

```go
func main() {
	app.RunWindow(ui.WindowOptions{Width: 960, Height: 640, MinWidth: 480, MinHeight: 360}, App)
}
```

手机会忽略这些选项。用户调整窗口大小时，环境里的 `Width` 和 `Height` 会随之变化，布局也会重新计算，你不需要做任何处理。如果想在特定尺寸下改变布局（比如空间足够时才显示侧边栏），可以像在平板上那样，在 `StyleFunc` 或 `Show` 里读取环境的 `WidthClass`。

在浏览器中，铺满整个页面的应用会从视觉视口（visual viewport）获取设备的安全区域（`env(safe-area-inset-*)`，适用于带刘海的手机）和屏幕键盘的高度。嵌在页面某个局部的应用则两者都没有。

## 滚动

`ui.Scroll` 让子视图可以滚动，默认是纵向，设置 `Horizontal` 后改为横向。记得给它一个尺寸或者 `Grow: 1`，让它有一个可以在其中滚动的区域。`Style` 是滚动视图本身的盒子；`ContentStyle` 是子视图排列所在的盒子，内边距和间距都应该设在这里。

```go
ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.All(16), Gap: 12}},
	ui.StaticText("Long content"),
)
```

下拉刷新和滚动位置见[组件](components.md)，长列表见[列表](components.md)。

## 颜色

`ui.Color` 来自 `colors` 包（`github.com/egoist/asuka/colors`），有以下几种：

- **平台默认色**：即零值，也就是不设置该属性。
- **语义颜色**：由平台解析，因此会跟随深色模式变化：
  - 文字：`colors.Label`、`colors.SecondaryLabel`、`colors.TertiaryLabel`；
  - 背景：`colors.Background`、`colors.SecondaryBackground`、`colors.GroupedBackground`，以及用于其上分组列表行的 `colors.SecondaryGroupedBackground`（对应 iOS 的 inset grouped 单元格）；
  - `colors.Separator`、`colors.Accent`（应用的强调色）、`colors.OnAccent`（在强调色上清晰可读的颜色，比如以强调色填充的按钮上的图标）、`colors.Clear`（透明）；
  - 系统色调：`colors.Red`、`colors.Orange`、`colors.Yellow`、`colors.Green`、`colors.Blue`、`colors.Purple`、`colors.Pink`、`colors.Gray`。
- **固定颜色**：`colors.RGB(r, g, b)`、`colors.RGBA(r, g, b, a)`、`colors.Hex(0x7c3aed)`，或者 `colors.ParseHex("#7c3aed")`（也支持 `"#rrggbbaa"`），后者会返回是否解析成功。

各平台的语义颜色来源：

| 平台 | 语义颜色 |
| --- | --- |
| iOS | UIKit 的颜色（`labelColor`、`systemBackground`、`systemRed` 等） |
| Android | Material 3 的颜色角色（color roles） |
| macOS | AppKit 的颜色（`labelColor`、`windowBackgroundColor`、`controlBackgroundColor`、`separatorColor`、`systemRedColor` 等） |
| Windows | Windows 11 的主题色（WinUI 的 Fluent 主题资源），分浅色和深色 |
| Linux | libadwaita 的具名颜色（`window_fg_color`、`window_bg_color`、`card_bg_color`、`destructive_color` 等），取主题中的定义，高对比度主题也包括在内 |
| Web | Asuka 自己的配色，以 CSS 变量的形式提供，分浅色和深色 |

在桌面端，分组背景就是普通背景：`colors.GroupedBackground` 是窗口背景，`colors.SecondaryGroupedBackground` 和 `colors.SecondaryBackground` 一样，是卡片的背景。

```go
ui.Column(ui.Style{Padding: ui.All(16), Gap: 4, Background: colors.Background, CornerRadius: 12},
	ui.StaticText("Title", ui.Style{Font: fonts.Headline}),
	ui.StaticText("Details", ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel}),
)
```

`c.Opacity(a)` 会以 0 到 1 之间的不透明度绘制颜色（相当于 SwiftUI 的 `.opacity`、Compose 的 `copy(alpha =)`）。语义颜色调整不透明度之后仍然是语义颜色，所以 `colors.Accent.Opacity(0.15)` 在浅色和深色模式下都是强调色的一层淡色，适合用作标签的背景、图表的填充或滑轨。

`Color` 是文字的颜色，或者控件的着色。

## 背景、边框与阴影

- **`Background`**：视图的背景色。
- **`CornerRadius`**：圆角半径，单位是点。
- **`BorderWidth`** 和 **`BorderColor`**：边框，画在盒子内侧。它会像内边距一样把内容往里推。
- **`Shadow`**：投影。投影由视图的形状（也就是它的边界，再按 `CornerRadius` 取圆角）产生，与视图里画了什么无关。可设置 `Color`（不设则没有投影）、`Radius`（模糊半径，与 CSS 和 SwiftUI 的含义相同，即模糊 σ 的两倍）以及偏移量 `X` 和 `Y`。裁剪子视图的视图（设置了 `Clip`）照样会投下阴影，就像一张裁剪了图片的圆角卡片那样。Android 上画的是它自己的高度（elevation）阴影：半径对应高度，颜色为阴影着色，偏移量由光源决定。macOS、Windows、Linux 和 Web 则与 iOS 的画法一致，模糊和偏移效果都相同。
- **`Gradient`**：覆盖在背景上的线性渐变。`ui.LinearGradient(colors...)` 从上往下渐变；`ui.Gradient` 可以用 `ui.Point` 设置 `Start` 和 `End`，以视图的尺寸为单位：(0, 0) 是顶部行首的那个角，(1, 1) 是底部行尾的那个角；还可以用 `Locations` 为每个颜色指定一个位置。Android 会沿八个方向中最接近的那个绘制渐变；其他平台则严格沿你给出的线条绘制。
- **`Material`**：一种半透明、会模糊其后内容的背景，即 iOS 的材质（material）：`ui.MaterialUltraThin`、`ui.MaterialThin`、`ui.MaterialRegular`、`ui.MaterialThick`、`ui.MaterialBar`。
  - iOS：`UIVisualEffectView` 的模糊效果。
  - macOS：在视图内容后面放一个 `NSVisualEffectView`，模糊窗口中位于其后的内容。
  - Windows：由宿主绘制的 Fluent 亚克力效果：把视图背后的内容模糊，再叠上浅色或深色的色调。
  - Web：CSS 的 `backdrop-filter`，即模糊加饱和度。
  - Android 和 Linux 无法模糊视图背后的内容，因此显示半透明的表面色（Linux 上是 libadwaita 的卡片颜色）。
- **`Clip`**：把子视图裁剪到视图的边界和圆角之内。

```go
ui.View(ui.ViewProps{Style: ui.Style{
	Height:       ui.Pt(120),
	CornerRadius: 16,
	Gradient:     ui.LinearGradient(colors.Hex(0x6366f1), colors.Hex(0xec4899)),
	Shadow:       ui.Shadow{Color: colors.RGBA(0, 0, 0, 80), Radius: 16, Y: 8},
}})

ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(12), BorderWidth: 1, BorderColor: colors.Separator, CornerRadius: 8, Clip: true}},
	ui.StaticText("Bordered"),
)

// 从左到右。
ui.View(ui.ViewProps{Style: ui.Style{
	Height:   ui.Pt(80),
	Gradient: &ui.Gradient{Colors: []ui.Color{colors.Blue, colors.Purple}, Start: ui.Point{X: 0, Y: 0.5}, End: ui.Point{X: 1, Y: 0.5}},
}})
```

## 不透明度与隐藏

`Opacity` 的取值范围是 0 到 1，但和其他未设置的属性一样，它的零值表示完全不透明：`Opacity: 0.5` 是半透明，而 `Opacity: 0` 反而是完全不透明。要隐藏视图，请设置 `Hidden`。隐藏的视图仍然在布局中占据空间；如果想把它连同其中的视图一起从布局中移除，请使用 `ui.Show`（见[核心概念](concepts.md)）。

## 变换

`Transform` 在绘制时对视图进行平移、缩放和旋转（`TranslateX`、`TranslateY`、`Scale`、`ScaleX`、`ScaleY`，以及以角度为单位的 `Rotate`）；布局感知不到这些变换。所有平台都支持。变换、`Transition` 和 `Match` 主要用于动画，见[动效](motion.md)。

## 文字

`Font` 设置文字的字体，类型是 `fonts` 包（`github.com/egoist/asuka/fonts`）中的 `fonts.Font`：

- **语义字体**：平台的文本样式，会随用户设置的文字大小缩放：`fonts.LargeTitle`、`fonts.Title`、`fonts.Title2`、`fonts.Title3`、`fonts.Headline`、`fonts.Subheadline`、`fonts.Body`、`fonts.Callout`、`fonts.Footnote`、`fonts.Caption`、`fonts.Caption2`。
- **`fonts.System(size)`**：指定字号（单位是点）的系统字体。
- **`.Weighted(w)`** 为字体指定字重：`fonts.Ultralight`、`fonts.Thin`、`fonts.Light`、`fonts.Regular`、`fonts.Medium`、`fonts.Semibold`、`fonts.Bold`、`fonts.Heavy`、`fonts.Black`。

`Color` 是文字颜色，`TextAlign` 是对齐方式（`ui.TextStart`、`ui.TextCenter`、`ui.TextEnd`、`ui.TextJustify`），`Lines` 是最多显示的行数，默认值 0 表示不限制。文字默认会自动换行。

```go
ui.StaticText("Brand", ui.Style{Color: colors.RGB(124, 58, 237), Font: fonts.Title.Weighted(fonts.Bold)})
ui.StaticText("A long paragraph that stops after two lines.", ui.Style{Font: fonts.Body, Lines: 2, TextAlign: ui.TextCenter})
ui.StaticText("Fixed size", ui.Style{Font: fonts.System(20).Weighted(fonts.Medium)})
```

如果一段文字里要混用多种样式、链接或内嵌视图，请看 [RichText](components.md)。

这些文本样式沿用了 iOS 的命名，各平台会把它们映射到自己的字号体系上，所以 `fonts.Body` 就是该平台应用正文所用的大小：

| 平台 | 文本样式 | `fonts.Body` |
| --- | --- | --- |
| iOS | UIKit 的文本样式，字体为 San Francisco | 17 |
| Android | Material 3 的字体比例（type scale，`fonts.Body` 对应 Body Large），字体为 Roboto | 16 |
| macOS | AppKit 的文本样式（`NSFont.preferredFont(forTextStyle:)`），字体为 San Francisco | 13 |
| Windows | Windows 11 的字号体系（type ramp），字体为 Segoe UI Variable（Windows 11 之前为 Segoe UI） | 14 |
| Linux | libadwaita 的字体类（`.title-1`、`.heading`、`.caption` 等），相对于桌面的界面字体 | 界面字体的字号 |
| Web | Asuka 自己的字号体系，使用系统字体（`system-ui`） | 16 |

在 Windows 上，每个样式取字号体系中最接近的一档：`fonts.LargeTitle` 对应 Title Large（40），`fonts.Title` 对应 Title（28），`fonts.Title2` 对应 Subtitle（20），`fonts.Headline` 对应 Body Strong（14，semibold），`fonts.Footnote` 和 `fonts.Caption` 对应 Caption（12）。在 Linux 上，以 GNOME 的 11 pt 界面字体为例，`fonts.LargeTitle` 对应 `.title-1`（181%，heavy），`fonts.Title` 对应 `.title-2`，`fonts.Headline` 对应 `.heading`（bold），`fonts.Caption` 对应 `.caption`（82%）。

哪些字体会随用户设置的文字大小缩放：

- **iOS**（动态字体，Dynamic Type）和 **Android**（字体缩放）：所有字体，包括语义字体、固定字号字体和自定义字体。
- **Windows**：所有字体，取决于“设置 > 辅助功能 > 文本大小”。
- **Linux**：所有字体，取决于桌面的文字缩放（GNOME 的“大号文本”、GTK 的字体 DPI）。各样式所参照的界面字体同样来自桌面设置（GTK 的 `gtk-font-name`）。
- **Web**：所有字体，取决于浏览器的文字大小，因为字体都以 `rem` 为单位。
- **macOS** 没有文字大小的设置，字体保持不变。

`ui.UseEnvironment()` 的 `FontScale` 就是这个缩放值，默认为 1（见[无障碍](accessibility.md)）。

## 自定义字体

把字体文件（`.ttf`、`.otf`）放进应用的 `assets/fonts` 目录。通过字体的 `Family`，或者 `fonts.Custom(family, font)`，来选择要用的字体：

- 文件名：`assets/fonts/Inter-Bold.ttf` 对应 `"Inter-Bold"`；
- 一组文件构成的字体族：`"Inter"` 会根据字重选用 `Inter-Regular`、`Inter-Bold` 等文件；
- 通用字体族：`"serif"`、`"monospace"`、`"rounded"`。在 iOS 和 macOS 上，它们是系统字体的几种设计（New York、SF Mono、SF Rounded）；在 Windows 上是 Georgia 和 Cascadia Mono；在 Linux 和 Android 上是系统的衬线和等宽字体族；在 Web 上是 `ui-serif`、`ui-monospace` 和 `ui-rounded`（前提是浏览器支持）。Android、Windows 和 Linux 没有圆体，会显示系统字体；
- 系统自带的字体族，例如 `"Georgia"`。

自定义字体会和它所基于的文本样式一样缩放。平台上没有的字体会用系统字体显示。所有平台都只为当前应用加载 `assets/fonts` 里的文件，不会把它们安装给其他应用使用。

```go
ui.StaticText("Inter Bold", ui.Style{Font: fonts.Custom("Inter-Bold", fonts.Title)})
ui.StaticText("Inter", ui.Style{Font: fonts.Custom("Inter", fonts.Body).Weighted(fonts.Semibold)})
ui.StaticText("Code", ui.Style{Font: fonts.Custom("monospace", fonts.Footnote)})
```

## 深色模式

语义颜色会自动随深色模式切换，使用它们的应用不需要为此写任何代码。固定颜色则保持不变；如果想为不同模式分别指定颜色，可以在 `StyleFunc` 里读取环境中的 `Dark`：

```go
env := ui.UseEnvironment()
ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
	s := ui.Style{Height: ui.Pt(80), CornerRadius: 12, Background: colors.RGB(255, 255, 255)}
	if env.Get().Dark {
		s.Background = colors.RGB(28, 28, 30)
	}
	return s
})})
```

## 会变化的样式

`ui.StyleFunc` 返回一个样式，当它读取过的 signal 变化时会重新运行。新样式会与上一次的样式比较：只有布局属性变了才会重新布局，而且只有发生变化的视觉属性才会发送给视图。

```go
ui.View(ui.ViewProps{
	OnPress: func() { selected.Set(!selected.Peek()) },
	Style: ui.StyleFunc(func() ui.Style {
		s := ui.Style{Padding: ui.All(12), CornerRadius: 10, Background: colors.SecondaryBackground}
		if selected.Get() {
			s.Background = colors.Accent
			s.BorderWidth = 2
			s.BorderColor = colors.Label
		}
		return s
	}),
}, ui.StaticText("Tap me"))
```

把这类改动放进 `ui.Animate` 里，它就会以动画呈现（见[动效](motion.md)）。

## 强调色

`colors.Accent` 以及用到它的控件，都从 `asuka.json` 读取应用的强调色：

```json
{ "accent": "#7c3aed" }
```

在 iOS 上它是着色（tint）；在 Android 上它是 Material 3 配色方案的种子色（Android 12 及以上）。设置 `"android": {"dynamicColor": true}` 后，只要系统支持，Android 就会像其他 Material 应用一样，从用户的壁纸中取色。在 macOS、Windows 和 Linux 上，它会在 Asuka 的控件和颜色中替换系统的强调色；在 Web 上，它同时也是控件的 `accent-color`。

如果没有设置强调色，就使用平台自己的。在 macOS 和 Windows 上，这是用户自己选的强调色（macOS 的“系统设置 > 外观”；Windows 的“设置 > 个性化 > 颜色”）；在 Linux 上是 libadwaita 的蓝色；在 Web 上是 Asuka 自带的一种蓝色。参见[发布](shipping.md)。

## 浅色与深色

应用会跟随系统的外观：iOS 和 macOS 的深色模式、Android 的深色主题、Windows 的应用模式（“设置 > 个性化 > 颜色”）、GNOME 的深色样式，以及浏览器的 `prefers-color-scheme`。`ui.SetColorScheme` 可以无视系统设置，强制以浅色或深色显示，就像应用内置的外观设置那样（对应 SwiftUI 的 `preferredColorScheme`；Android 上 AppCompat 的夜间模式；macOS 上应用的 `NSAppearance`；Linux 上 libadwaita 的 style manager）；传入 `ui.ColorSchemeSystem` 则恢复跟随系统。语义颜色、平台控件和系统栏都会随之切换，无需重建应用；当前是哪种模式，可以看 `ui.UseEnvironment()` 的 `Dark`。用 [`store`](data.md) 保存用户的选择，并在应用启动时设置：

```go
var scheme = store.Signal("colorScheme", int(ui.ColorSchemeSystem))

func App() ui.Node {
	ui.SetColorScheme(ui.ColorScheme(scheme.Peek()))
	// ...
}
```

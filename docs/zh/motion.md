# 动效与手势

只要平台自带动画机制，Asuka 就直接用它：iOS 上是 UIView 动画，Android 上是 animator，macOS 上是 AppKit 的隐式动画和 Core Animation，浏览器里是 Web Animations API。Windows 和 GTK 没有针对视图 frame（位置与尺寸）的动画，所以 Windows 和 Linux 宿主会自己逐帧推进，用的也是同样的曲线。你只要在 `ui.Animate` 里修改状态，这次修改在屏幕上引起的移动就会以动画呈现。本页依次介绍：为变化添加动画、进入与退出过渡、共享元素、逐帧动画、触摸/鼠标/触控板手势、触感反馈，以及减弱动态效果。

## 为变化添加动画

把写入操作包进 `ui.Animate`，下一次刷新（flush）带来的变化就会以动画呈现：包括布局移动的 frame，以及下文列出的那些视觉属性。

```go
func Details() ui.Node {
	expanded := signal.New(false)
	return ui.Column(ui.Style{Gap: 12, Padding: ui.All(20)},
		ui.Button(ui.ButtonProps{Title: ui.Val("More"), OnPress: func() {
			ui.Animate(func() { expanded.Set(!expanded.Peek()) })
		}}),
		ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			h := float32(80)
			if expanded.Get() {
				h = 240
			}
			return ui.Style{Height: ui.Pt(h), CornerRadius: 16, Background: colors.SecondaryBackground}
		})}),
	)
}
```

方块变高时，它下面的内容也会跟着往下滑：布局只运行一次，每个变了的 frame 都会以动画移到新位置。你不需要自己去给位置做动画。

同样的写入如果不放在 `ui.Animate` 里，就会立刻生效。和所有 signal 写入一样，`ui.Animate` 也在主线程上运行；如果在 goroutine 里，请在 `ui.Post` 中调用它。

## 时长、曲线与重复

`ui.AnimateWith` 接受一个 `ui.Animation`：

- `Duration` 是时长，单位为秒；0 表示 0.25。
- `Curve` 可以是 `ui.EaseInOut`（默认）、`ui.Linear`、`ui.EaseIn`、`ui.EaseOut` 或 `ui.Spring`。弹簧动画是一个带阻尼的弹簧，会在 `Duration` 结束时停稳：iOS 上用的是 UIKit 的弹簧动画，Android、Windows、Linux 和 Web 上则用同一个弹簧的插值器。
- `Damping` 是弹簧的阻尼比：为 1 时不越过终点就停下，小于 1 时会冲过头再弹回来（0 表示 0.85，稍有回弹）。`Velocity` 是弹簧的初速度，以“每秒走完整段变化的几分之几”来表示（即 UIKit 的 `initialSpringVelocity`），用来延续一次快速滑动（fling）的速度。
- `Delay` 是开始前等待的秒数。
- `OnDone` 会在动画逻辑上完成时调用（即 `Delay` 加上每一次重复的 `Duration` 都过去之后），可以在这里接上下一步。
- macOS 上 AppKit 的隐式动画既没有弹簧也没有延迟：回弹明显的弹簧动画会改用一条会冲过头的曲线，延迟则直接跳过。
- `Repeat` 表示再额外重复多少次；`ui.Forever` 会一直重复，直到这些属性再次改变。设置了 `Autoreverses` 时，每一轮都会先倒放回去，再开始下一轮。macOS 和 Windows 不支持重复：动画只运行一次。

```go
func Pulse() ui.Node {
	on := signal.New(false)
	dot := ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		s := ui.Style{Width: ui.Pt(24), Height: ui.Pt(24), CornerRadius: 12, Background: colors.Red}
		if on.Get() {
			s.Transform = ui.Transform{Scale: 1.6}
			s.Opacity = 0.35
		}
		return s
	})})
	start := func() {
		ui.AnimateWith(ui.Animation{Duration: 0.8, Curve: ui.EaseInOut, Repeat: ui.Forever, Autoreverses: true},
			func() { on.Set(true) })
	}
	stop := func() { on.Set(false) } // 不带动画地设置：脉冲随即停止
	return ui.Row(ui.Style{Gap: 16},
		dot,
		ui.Button(ui.ButtonProps{Title: ui.Val("Pulse"), OnPress: start}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Stop"), OnPress: stop}),
	)
}
```

手势结束时，往往接着播放一个弹簧动画。把手势的速度除以剩下的距离，就得到弹簧的 `Velocity`，这样视图就能保持手指甩出去的速度：

```go
OnPan: func(g ui.Gesture) {
	if g.State != ui.GestureEnded {
		drag.Set(g) // 跟随手指
		return
	}
	d := math.Hypot(float64(g.TranslationX), float64(g.TranslationY))
	v := 0.0
	if d > 1 { // 沿返回路径、朝向原位的速度
		v = -(float64(g.VelocityX*g.TranslationX) + float64(g.VelocityY*g.TranslationY)) / d / d
	}
	ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.6, Damping: 0.55, Velocity: float32(v)},
		func() { drag.Set(ui.Gesture{}) })
},
```

前后相接的几步动画可以通过 `OnDone` 串起来。比如一个 toast 提示：滑入、停留一会儿、再消失：

```go
ui.AnimateWith(ui.Animation{OnDone: func() {
	ui.AnimateWith(ui.Animation{Delay: 1.5}, func() { shown.Set(false) })
}}, func() { shown.Set(true) })
```

要停止一个重复动画，只需不带动画地再次设置它的属性。Android 上只有透明度和变换能重复；iOS、Linux 和 Web 上所有能动画的属性都能重复；macOS 和 Windows 则只运行一次。

## 哪些变化会动画

| 变化 | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| frame（位置和尺寸，来自布局） | 是 | 是 | 是 | 是 | 是 | 是 |
| `Opacity` | 是 | 是 | 是 | 否 | 是 | 是 |
| `Transform` | 是 | 是 | 是 | 是 | 是 | 是 |
| `Background`（纯色） | 是 | 是 | 否 | 否 | 否 | 是 |
| `CornerRadius` | 是 | 是 | 是 | 否 | 否 | 否 |
| `Shape` 的 `TrimStart` 和 `TrimEnd` | 是 | 是 | 是 | 否 | 是 | 否 |
| `Shape` 的路径 | 是 | 否 | 是 | 否 | 否 | 否 |

其他属性（`BorderColor`、`Shadow`、`Gradient`、文字颜色和字体）都会立即改变。只有已经在屏幕上的视图才会动画；在 `ui.Animate` 期间新出现的视图不会以动画进入，除非它设置了[过渡](#进入与退出)。

`Opacity` 为 0 是零值，表示不透明，所以无法通过 `Opacity` 把视图动画到完全透明。想让一个视图淡出，就用 `Fade` 过渡把它移除。

## 变换

`Style.Transform` 会在绘制时以视图中心为原点，对它进行平移、缩放和旋转。布局看不到变换，相邻的视图都待在原处不动。

- `TranslateX`、`TranslateY`：偏移量，单位为点。
- `Scale`：同时缩放两个轴（0 表示 1）；`ScaleX`、`ScaleY` 只缩放其中一个轴。
- `Rotate`：角度，顺时针方向。

```go
func Flip() ui.Node {
	flipped := signal.New(false)
	return ui.View(ui.ViewProps{
		OnPress: func() {
			ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { flipped.Set(!flipped.Peek()) })
		},
		Style: ui.StyleFunc(func() ui.Style {
			t := ui.Transform{}
			if flipped.Get() {
				t = ui.Transform{Rotate: 6, Scale: 1.04}
			}
			return ui.Style{Height: ui.Pt(120), CornerRadius: 20, Background: colors.Accent, Transform: t}
		}),
	})
}
```

## 进入与退出

`Style.Transition` 描述的是一个状态：视图在 `ui.Animate` 期间被添加时（比如 `Show` 变为 true，或 `For` 里新增了一项），会从这个状态进入；在 `ui.Animate` 期间被移除时，会退出到这个状态。被移除的视图会一直留在屏幕上，直到退出动画结束，在此期间它不响应触摸。

- `ui.TransitionFade`：淡入淡出。
- `ui.TransitionPop`：淡入的同时从 85% 放大进入（适合弹出框、角标）。
- `ui.TransitionSlideUp`：从下方 24 点处淡入（适合 toast、新增的行）。
- `ui.Transition{...}`：自定义过渡，包括 `Fade`、`Scale`（0 表示不缩放），以及以点为单位的偏移 `X`、`Y`。它们可以组合使用：`ui.Transition{Fade: true, Scale: 0.5, Y: -8}`。

```go
func Toast() ui.Node {
	shown := signal.New(false)
	save := func() {
		ui.Animate(func() { shown.Set(true) })
		ui.Announce("Saved")
		go func() {
			time.Sleep(1500 * time.Millisecond)
			ui.Post(func() { ui.Animate(func() { shown.Set(false) }) })
		}()
	}
	return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}},
		ui.Button(ui.ButtonProps{Title: ui.Val("Save"), OnPress: save}),
		ui.Show(shown, func() ui.Node {
			return ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Left: ui.Pt(20), Right: ui.Pt(20), Bottom: ui.Pt(20),
				Background: colors.SecondaryBackground, CornerRadius: 16, Padding: ui.Symmetric(14, 18),
				Transition: ui.TransitionSlideUp}},
				ui.StaticText("Saved"))
		}),
	)
}
```

过渡只在 `ui.Animate` 内部运行，并沿用它的时长和曲线。同样的 `shown.Set(true)` 如果不在 `ui.Animate` 里，toast 会直接出现。另外，只有父视图已经在屏幕上时，子视图才会以过渡进入。

## 共享元素

`Style.Match` 是一个键，让两个视图彼此对应，作用类似 SwiftUI 的 `matchedGeometryEffect`。在 `ui.Animate` 期间，如果一个带这个键的视图离开、另一个带同样键的视图出现，新视图就会从旧视图的位置和大小长成自己的样子，旧视图则跟着一起移动并淡出。下面的例子里，一行展开成一张卡片，封面和标题随之移动（取自 [`examples/gallery`](../../examples/gallery)）：

```go
func Matched() ui.Node {
	open := signal.New(false)
	toggle := func() {
		ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { open.Set(!open.Peek()) })
	}
	art := func(size float32, radius float32) ui.Style {
		return ui.Style{Width: ui.Pt(size), Height: ui.Pt(size), CornerRadius: radius, Background: colors.Orange, Match: "album"}
	}
	return ui.Show(open, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: toggle, Style: ui.Style{Gap: 12, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: art(240, 24)}),
			ui.StaticText("Night Drive", ui.Style{Font: fonts.Title2, Match: "album-title"}))
	}, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: toggle, Style: ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: art(56, 10)}),
			ui.StaticText("Night Drive", ui.Style{Font: fonts.Headline, Match: "album-title"}))
	})
}
```

- 所有平台都支持，包括桌面端和 Web。
- 两个视图必须在同一次 `ui.Animate` 中变化：一个被移除，一个被添加。
- 键属于整个根节点：同一时刻，每个键只能有一个视图持有。
- 带键离开却找不到对应视图的视图，会直接淡出。

## 缩放进入新页面

`ScreenOptions.ZoomFrom` 指定下层页面上某个视图的 `Match` 键。新页面推入时会从这个视图中放大展开，弹出时再缩回去。`List` 的单元格也可以用，比如下面这个照片网格：

```go
func Photos() ui.Node {
	ids := []int{10, 11, 12, 13, 14, 15}
	return ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
		func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			nav := ui.UseNavigator()
			return ui.View(ui.ViewProps{
				Label: ui.Val("Photo"),
				OnPress: func() {
					i := signal.Untrack(id.Get)
					nav.Push(func() ui.Node { return Photo(i) },
						ui.ScreenOptions{Title: ui.Val("Photo"), ZoomFrom: fmt.Sprintf("photo-%d", i)})
				},
				Style: ui.StyleFunc(func() ui.Style {
					return ui.Style{AspectRatio: 1, Match: fmt.Sprintf("photo-%d", id.Get())}
				}),
			}, ui.Image(ui.ImageProps{
				Source: signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
				Mode:   ui.ImageFill, Style: ui.Style{Grow: 1},
			}))
		})
}
```

- **iOS 18 及以上**使用 UIKit 的缩放过渡，交互式返回手势会把页面缩回到那个视图里。更早的版本按普通方式推入。
- **Android** 使用 Material 的容器变换（container transform）：页面从视图处长到自己的边界，带圆角，两者在靠近视图的地方交叉淡化。预测性返回手势会把它缩回去。
- **macOS、Windows、Linux 和 Web** 按普通方式推入。
- 如果下层页面上没有带这个键的视图，也按普通方式推入。

这里不需要 `ui.Animate`：推入本身就带动画。详见[导航](navigation.md)。

## 自己画出来的图形

`Shape` 的 `TrimStart` 和 `TrimEnd` 决定轮廓绘制出来的那一段，取值从 0 到 1。在 `ui.Animate` 里修改它们，描边就会一点点画出来，适合做进度环、对勾之类的效果。

```go
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

func Ring() ui.Node {
	done := signal.New(float32(0.3))
	return ui.Column(ui.Style{Gap: 12},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.Accent, LineWidth: 8, LineCap: ui.CapRound, TrimEnd: done,
			Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Finish"), OnPress: func() {
			ui.AnimateWith(ui.Animation{Duration: 0.8}, func() { done.Set(1) })
		}}),
	)
}
```

## 逐帧动画

如果运动是算出来的而不是声明出来的（物理模拟、游戏、跟随数值变化的仪表盘），可以用 `ui.OnFrame`：显示器每绘制一帧（每秒 60 或 120 次），它就调用一次你的函数，并传入这一帧的时间。函数里写入的内容会在这一帧显示出来。它会一直运行，直到你调用它返回的 `stop`，或者组件的 owner 被销毁。没有注册任何回调时，宿主不会请求帧。

```go
func Spinner() ui.Node {
	env := ui.UseEnvironment()
	angle := signal.New(float32(0))
	var last time.Duration
	ui.OnFrame(func(t time.Duration) {
		if last != 0 && !env.Get().ReduceMotion {
			a := angle.Peek() + float32((t-last).Seconds()*180) // 每秒转半圈
			angle.Set(float32(math.Mod(float64(a), 360)))
		}
		last = t
	})
	return ui.View(ui.ViewProps{Decorative: true, Style: ui.StyleFunc(func() ui.Style {
		return ui.Style{Transform: ui.Transform{Rotate: angle.Get()}}
	})}, ui.Symbol(ui.IconSettings, ui.Style{Font: fonts.LargeTitle, Color: colors.Accent}))
}
```

运动要根据两帧之间的时间来计算，而不是根据帧数：不同设备的帧率并不相同。

## 手势

`View` 可以接收手势，识别工作交给平台自己的手势识别器：

- `OnPress` 和 `OnLongPress`：轻点，以及按住不放。
- `OnPan`：拖动。
- `OnPinch`：双指张开或捏合。
- `OnRotate`：双指旋转。
- `OnSwipe`：快速轻扫，并带上方向 `ui.SwipeDirection`：`ui.SwipeLeft`、`ui.SwipeRight`、`ui.SwipeUp` 或 `ui.SwipeDown`。

使用鼠标或触控板时，同样这些处理函数会收到桌面端对应的操作：

| 手势 | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- |
| `OnPress` | 单击 | 单击；或在获得焦点的视图上按 Enter、空格 | 单击；或在获得焦点的视图上按 Enter、空格 | 单击；或在获得焦点的视图上按 Enter、空格 |
| `OnLongPress` | 辅助点按，或按住鼠标键不放 | 右键单击，或按住鼠标键不放 | 辅助点击，或按住鼠标键不放 | 右键单击，或按住鼠标键、手指按住不放 |
| `OnPan` | 按住鼠标键拖动 | 按住鼠标键拖动 | 按住鼠标键拖动、在触摸屏或触控板上拖动 | 按下指针拖动 |
| `OnSwipe` | 结束时速度很快的拖动；触控板上的双指轻扫；系统留给应用处理时的三指轻扫 | 结束时速度很快的拖动 | 结束时速度很快的拖动；触摸屏上的轻扫 | 结束时速度很快的拖动 |
| `OnPinch` | 触控板捏合 | 触控板捏合、Ctrl 加滚轮，或触摸屏上的双指操作 | 触控板或触摸屏捏合，或 Ctrl 加滚轮 | 触控板捏合、Ctrl 加滚轮，或触摸屏上的双指操作 |
| `OnRotate` | 触控板旋转 | 触摸屏上的双指旋转 | 触控板或触摸屏旋转 | 触摸屏上的双指旋转 |

很少有人会按住鼠标键不放来看看会发生什么，所以在重要的地方，`OnLongPress` 的功能也要提供另一个入口，比如一个菜单项或一个按钮。在 Windows 和 Linux 上，Ctrl 加滚轮每滚一格捏合 10%，滚轮停下后捏合就结束。在 Mac 触控板上双指轻扫时，视图外围能滚动的内容照样会滚动：只有外围在那个方向上没有可滚动的内容时，才会触发向上或向下的轻扫。

拖动、捏合和旋转在进行过程中，会不断用一个 `ui.Gesture` 调用对应的函数：

- `State`：依次为 `ui.GestureBegan`、`ui.GestureChanged`，最后是 `ui.GestureEnded`；如果触摸被系统接管，则是 `ui.GestureCancelled`。
- `X`、`Y`：触摸点在视图中的位置，单位为点。
- `TranslationX`、`TranslationY`：拖动从手指按下以来移动的距离，因此 `X - TranslationX` 就是按下的位置。
- `VelocityX`、`VelocityY`：拖动的速度，单位为点每秒。
- `Scale`：捏合开始以来的缩放比例（拖动时为 1）。
- `Rotation`：旋转开始以来转过的角度，单位为弧度，顺时针方向。

被拖动的视图通过变换跟随手指，松手后弹回原位：

```go
func Puck() ui.Node {
	drag := signal.New(ui.Gesture{})
	return ui.View(ui.ViewProps{
		OnPan: func(g ui.Gesture) {
			if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
				ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.45}, func() { drag.Set(ui.Gesture{}) })
				return
			}
			drag.Set(g)
		},
		Style: ui.StyleFunc(func() ui.Style {
			g := drag.Get()
			return ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), CornerRadius: 32, Background: colors.Accent,
				Transform: ui.Transform{TranslateX: g.TranslationX, TranslateY: g.TranslationY}}
		}),
	})
}
```

缩放和旋转都是从这次手势开始时算起的，所以要记住上一次手势结束时的值，下一次在它的基础上累加。同一个视图上的多个手势可以同时生效：一边捏合一边旋转。

```go
angle, base := signal.New(float32(0)), float32(0)
scale, baseScale := signal.New(float32(1)), float32(1)
return ui.View(ui.ViewProps{
	Label: ui.Val("A square to turn"),
	OnRotate: func(g ui.Gesture) {
		angle.Set(base + g.Rotation*180/math.Pi)
		if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
			base = angle.Peek()
		}
	},
	OnPinch: func(g ui.Gesture) {
		scale.Set(min(max(baseScale*g.Scale, 0.5), 2))
		if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
			baseScale = scale.Peek()
		}
	},
	Style: ui.StyleFunc(func() ui.Style {
		return ui.Style{Width: ui.Pt(160), Height: ui.Pt(160), CornerRadius: 24, Background: colors.Purple,
			Transform: ui.Transform{Rotate: angle.Get(), Scale: scale.Get()}}
	}),
})
```

拖动、捏合或旋转一旦开始，就独占了这次触摸：外层的滚动视图不会把它抢走，里面可按压的视图也不会收到按压，就像从按钮上开始的拖动不会触发按钮一样。

开着屏幕阅读器时，用户无法完成轻扫或拖动。所以请把它们的功能也作为[无障碍操作](accessibility.md)提供出来。

## 按下与悬停状态

`View` 会在交互状态变化时报告出来，样式可以通过三种方式跟随这些状态。从第一种开始，能满足需求就用它：

1. **变体（variant）**，用于视图自身的外观。`Style.Pressed`、`Hovered`、`Dragging` 和 `DragOver` 会在视图处于对应状态时叠加在其余样式之上。它们相当于 CSS 的 `:active` 和 `:hover`，或者 Tailwind 的 `active:` 和 `hover:`。
2. **`Interaction`**，用于其他视图，以及其他任何需要跟随状态的东西。给视图设置一个 `Interaction`，然后就可以在任何需要的地方读取 `it.Pressed()` 或 `it.Hovered()`，比如 `Style.When`、`StyleFunc`、`Text` 或 `Show`。子视图跟随父视图（相当于 group 的 hover）、兄弟视图跟随相邻视图（相当于 peer 的 hover），都是这么实现的。
3. **处理函数**，用于样式之外的事情，比如开始录音：`OnPressedChange` 和 `OnHoverChange`。

```go
func Card(title string, open func()) ui.Node {
	card := ui.NewInteraction()
	return ui.View(ui.ViewProps{
		OnPress:     open,
		Interaction: card,
		Style: ui.Style{Direction: ui.RowDir, Padding: ui.All(16), CornerRadius: 12,
			Background: colors.SecondaryGroupedBackground,
			Hovered:    &ui.Style{Background: colors.SecondaryBackground},
			Pressed:    &ui.Style{Background: colors.Separator, Transform: ui.Transform{Scale: 0.97}}},
	},
		// 标题跟随卡片的悬停状态：相当于 group 的 hover。
		ui.StaticText(title, ui.Style{Grow: 1}.When(card.Hovered, ui.Style{Color: colors.Accent})),
	)
}
```

各个状态的含义：

- **Pressed**：手指或鼠标主键在视图上按下时为 true。
  - 在可滚动的容器里，它会稍晚一些才变为 true，免得一滚动就闪一下。
  - 手指抬起、移出视图（移回来会再次按下），或者触摸被滚动、长按或其他手势接管时，它就会变回 false。
  - 只有在按下状态中松开，才会触发 `OnPress`。
- **Hovered**：指针位于视图或其子视图上方时为 true。指针可以是鼠标、触控板、iPad 的指针，或者悬停的 Apple Pencil、手写笔。单纯的触摸屏永远不会产生悬停。
- **Dragging**：视图正在被拖动（通过它的 `Drag`）时为 true。
- **DragOver**：有一个会被它的 `Drop` 接收的拖动经过它上方时为 true。

变体和 `When` 的组合规则：

- 变体只设置它写明的字段，其余字段保持样式原有的值。
- `Font` 按字段逐个合并，所以 `Font{Weight: fonts.Bold}` 会保留原来的文字样式。其他字段都是整体替换。
- 变体按 Hovered、Pressed、Dragging、DragOver 的顺序应用，所以按下状态优先于悬停状态。
- `When` 可以链式调用，后面的优先。
- 零值字段表示“未设置”，所以某个状态无法清除一个字段。如果想让某样东西只在某个状态下显示，就用 `Unless` 在另一种状态下设置这个字段：

```go
row := ui.NewInteraction()
ui.View(ui.ViewProps{Interaction: row},
	ui.Text(name),
	// 鼠标悬停在这一行上时，才显示删除按钮。
	ui.View(ui.ViewProps{OnPress: remove, Style: ui.Style{}.Unless(row.Hovered, ui.Style{Hidden: true})},
		ui.Symbol(ui.IconTrash)),
)
```

`When` 和 `Unless` 接受任意 `func() bool`，所以样式也可以跟随任何 signal：`base.When(selected.Get, ui.Style{Background: colors.Accent})`。

视图只追踪有人读取的状态，也就是它的变体用到的，或者从它的 `Interaction` 读取的状态。

- 如果一个视图既没有 `Pressed` 变体，也没有人读取 `Pressed`，它就不会报告按下状态，并保留平台自带的高亮效果。
- 视图一旦报告按下状态，就由它自己负责绘制，平台自带的高亮（Android 的水波纹、Windows 的悬停和按下填充）会被关掉。

每个状态每变化一次只报告一次。视图在某个状态中消失时会退出该状态，因此跟随它的东西不会卡在那里。把同一个 `Interaction` 交给多个视图时，只要其中任何一个处于某个状态，该状态就为 true。

没有 `OnPress` 时，按下不放就只是单纯的按住，比如“按住录音”。录音属于副作用，所以要放在处理函数里：

```go
ui.View(ui.ViewProps{
	OnPressedChange: func(on bool) {
		if on {
			recorder.Start()
		} else {
			recorder.Stop()
		}
	},
	Label: ui.Val("Hold to record"),
	Style:           ui.Style{Padding: ui.All(16), Pressed: &ui.Style{Background: colors.Red}},
}, ...)
```

## 拖放

`ViewProps.Drag` 让视图可以被拖动。在触摸屏上，长按后再移动就能把它拿起来，而且这次长按不会再触发 `OnLongPress`。用鼠标时，按住鼠标键移动就能拖动它。视图的图像会跟着手指或指针移动，既可以在应用内拖动，也可以拖到其他应用里。`ViewProps.Drop` 则让视图可以接收拖放。

一个 `ui.DragItem` 可以是一段文本、一个链接（`Kind: ui.SharedLink`）、一个文件（`Path`），或者应用自己的一个值（`Value`）：

- 在应用内部，放下时拿到的就是原样的条目，连同 Go 值一起。
- 其他应用拿到的是条目的文本、链接或文件。只带有值的条目不会传到其他应用。
- `MIME` 是条目的类型，拖放目标根据它来决定是否接收。文本、链接和文件会自动填好类型。请给 `Value` 设置一个应用专属的类型，这样只有应用自己的目标会接收它。

```go
type Task struct{ Title string; Done bool }

func TaskCard(t *Task) ui.Node {
	return ui.View(ui.ViewProps{
		Drag: &ui.DragSource{
			Items: func() []ui.DragItem {
				return []ui.DragItem{{Value: t, MIME: "application/x-myapp-task", Text: t.Title}}
			},
		},
		// 拖动时变淡。
		Style: ui.Style{Padding: ui.All(12), Dragging: &ui.Style{Opacity: 0.4}},
	}, ui.StaticText(t.Title))
}

func DoneColumn(move func(*Task), cards ...ui.Node) ui.Node {
	return ui.View(ui.ViewProps{
		Drop: &ui.DropTarget{
			Accept: []string{"application/x-myapp-task"},
			OnDrop: func(d ui.Drop) {
				for _, it := range d.Items {
					if t, ok := it.Value.(*Task); ok {
						move(t)
					}
				}
			},
		},
		// 有任务拖到上方时显示描边。
		Style: ui.Style{Gap: 8, Padding: ui.All(8), BorderWidth: 2, BorderColor: colors.Clear,
			DragOver: &ui.Style{BorderColor: colors.Accent}},
	}, cards...)
}
```

拖动源：

- `Items` 在一个 effect 里运行，所以它读取的内容变化时会自动更新。一次拖动携带的是开始拖动那一刻的条目。
- `OnPhase` 在视图被拿起时报告 `ui.DragBegan`。之后要么报告 `ui.DragDropped`，表示有目标接收了它（如果是移动操作，就把被拖动的东西删掉）；要么报告 `ui.DragCancelled`。拖动中的外观请用 `Dragging` 变体或 `Interaction.Dragging`。

拖放目标：

- `Accept` 列出 MIME 类型：可以是精确类型（`"text/plain"`）、一类类型（`"image/*"`）或任意类型（`"*/*"`）。
- 如果一次拖动里没有这些类型的条目，它就根本不会进入这个视图。
- 目标嵌套时，由最内层愿意接收的那个目标获得这次拖动。
- `OnOverChange` 报告拖动进入和离开视图，与 `DragOver` 变体是同一个状态。
- `OnMove` 报告拖动在视图上的位置，可以当作插入点。
- `OnDrop` 拿到它接收的条目以及放下的位置。`Drop.Local` 表示这次拖动来自应用自身。

从其他应用拖进来的内容（访达或“文件”App 里的文件、“照片”里的照片、浏览器里的链接），会等条目加载完成后再送达：

- 在 iOS 和 Android 上，文件会先被复制到应用的缓存目录，此时 `Drop.Copied` 为 true。请把文件移到应用存放它们的地方，或者用完后删掉。
- 在 macOS、Windows 和 Linux 上，`Path` 就是用户的原文件，不要删除它。
- 在浏览器里，放下的文件会被复制到应用的文件中（属于这个页面，存放在内存里），此时 `Drop.Copied` 为 true。

## 按键

键盘焦点位于某个视图（它本身或它里面的视图）时，这个视图可以通过 `Keys` 接收按键：`Keys` 是一个 `KeyMap`，把 `Shortcut` 映射到要执行的操作。按键会从获得焦点的视图开始向外查找，交给最近的那个处理它的视图，而且优先于获得焦点的控件和菜单栏。这样，搜索框的上下方向键就可以在下面的结果中移动：

```go
sel := signal.New(0)
move := func(by int) func() {
	return func() {
		n := len(signal.Untrack(results.Get))
		sel.Set(max(0, min(sel.Peek()+by, n-1)))
	}
}
ui.TextField(ui.TextFieldProps{
	Value:    query,
	Keys:     ui.KeyMap{{Key: "down"}: move(1), {Key: "up"}: move(-1)},
	OnSubmit: func() { open(signal.Untrack(results.Get)[sel.Peek()]) },
})
```

一个按键可以是一个字符，也可以是一个具名按键（`enter`、`escape`、`tab`、`space`、`delete`、`backspace`、`up`、`down`、`left`、`right`、`home`、`end`、`pageup`、`pagedown`、`f1` 到 `f12`），修饰键必须与给定的完全一致：`ui.Cmd("d")`、`{Key: "up", Mods: ui.Alt}`。按住不放会重复触发。不带修饰键的字符会截走视图内输入框的输入，所以请给它加上修饰键，或者把它放在没有输入框的地方。

`ui.UseKeys(ui.KeyMap{...})` 给窗口设置它自己的按键：按键最后才会到达这里；而在没有任何东西获得焦点时，则最先到达这里，比如游戏的方向键或查看器的空格键。它们的生命周期跟随调用 `ui.UseKeys` 的那个组件。

按键来自硬件键盘，也就是桌面电脑、iPad 或 Chromebook 上的键盘，以及浏览器收到的键盘输入。不要让按键成为完成某件事的唯一方式，因为手机通常没有键盘。输入法正在组字时（比如日文或中文输入法），按键归输入法处理。`examples/launcher` 就用上下方向键在搜索结果中移动。

## 触感反馈

`ui.Haptic` 播放触感反馈：`ui.HapticSelection`（数值切换了一格），`ui.HapticLight`、`ui.HapticMedium` 和 `ui.HapticHeavy`（碰撞），以及 `ui.HapticSuccess`、`ui.HapticWarning` 和 `ui.HapticError`（操作结果）。

```go
OnSwipe: func(d ui.SwipeDirection) {
	switch d {
	case ui.SwipeLeft:
		page.Set(page.Peek() + 1)
	case ui.SwipeRight:
		page.Set(max(page.Peek()-1, 0))
	}
	ui.Haptic(ui.HapticSelection)
},
OnLongPress: func() { ui.Haptic(ui.HapticMedium) },
```

在 iOS 上，它们对应 `UIFeedbackGenerator` 的各种反馈。Android 没有完全对应的反馈：Asuka 会从 `performHapticFeedback` 的常量里挑最接近的来播放。成功、警告和错误在 Android 11 及以上对应 confirm 和 reject，更早的版本则用按键或长按的反馈代替。

在配有 Force Touch 触控板的 Mac 上，`ui.HapticSelection` 对应 `NSHapticFeedbackManager` 的 alignment，其他几种对应它的 level change；只有手指放在触控板上时才能感受到。Windows 和 Linux 没有触感反馈，`ui.Haptic` 在那里什么也不做。在浏览器里，如果浏览器支持 `navigator.vibrate`（Android 上的 Chrome），它会短促地振动一下，其他浏览器则什么也不做。

## 减弱动态效果

用户要求减少动态效果时，`ui.UseEnvironment().Get().ReduceMotion` 为 true。用户更改设置时，它也会随之变化：

- **iOS**：设置 > 辅助功能 > 动态效果 > 减弱动态效果。
- **Android**：关闭动画（在开发者选项中，或在无障碍 > 移除动画中）。
- **macOS**：系统设置 > 辅助功能 > 显示 > 减弱动态效果。
- **Windows**：设置 > 辅助功能 > 视觉效果，关闭“动画效果”。
- **Linux**：关闭 GNOME 的动画（设置 > 无障碍 > 减少动画（Reduce Animation）；即 GTK 的 `gtk-enable-animations`）。
- **Web**：浏览器的 `prefers-reduced-motion`，它跟随系统设置。

Asuka 自己会做的处理：

- **iOS**：过渡改为原地溶解，不再移动或缩放，和 UIKit 自己的过渡在这种设置下的表现一致；共享元素（`Match`）也不再飞过去，视图按各自的过渡出现和消失。其他动画照常运行。
- **Android**：系统的动画时长缩放为 0，因此所有动画，无论是 Asuka 的还是平台的，都会立即结束。
- **macOS、Windows、Linux 和 Web**：`ui.Animate` 里的变化会立即生效，过渡和共享元素也一样。导航栈的推入和弹出在 macOS 上不再滑动，在浏览器里改为交叉淡化，在 Linux 上则和关闭动画后的 libadwaita 导航表现一致。

剩下需要你自己处理的，是 Asuka 无法判断是否只起装饰作用的动效：`OnFrame` 循环（就像上面的加载图标那样停下来）、反复播放的脉冲、让某样东西移动很远的动画。读取 `ReduceMotion`，相应地减少这些动效。

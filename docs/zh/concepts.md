# 核心概念

一个 Asuka 应用由两部分组成：只构建一次视图的 Go 函数，以及保存状态的 signal。本页介绍它们如何配合：根组件、signal 及其触发的更新、条件显示与列表渲染、owner 与清理、主线程，以及环境信息。

## 应用

`app.Run` 接收根组件，并把它挂载为应用的界面：

```go
func App() ui.Node {
	return ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12},
		ui.StaticText("Hello", ui.Style{Font: fonts.LargeTitle}),
		Greeting("Ada"),
	)
}

func Greeting(name string) ui.Node {
	return ui.StaticText("Hello, " + name)
}

func main() { app.Run(App) }
```

在手机上，根组件会铺满整个屏幕，一直延伸到刘海和底部的 Home 指示条下方；在桌面端，它会铺满整个窗口。`ui.SafeArea` 能让内容避开遮挡它的东西，包括键盘（见[布局与样式](layout.md#安全区域键盘与窗口)）。

`app.Run` 的运行方式因平台而异：

- **iOS 和 Android**：由平台启动应用，应用的宿主（Swift、Kotlin）加载 Go 代码并调用 `main`；`app.Run` 挂载根组件后就会返回。
- **macOS、Windows 和 Linux**：应用连同宿主是同一个 Go 程序。`app.Run` 会打开主窗口，把根组件挂载进去，然后在主线程上运行平台的事件循环，直到应用退出。请像上面那样在 `main` 中调用它。
- **Web**：页面加载应用的 WebAssembly；`app.Run` 把根组件挂载到页面中。

`app.RunWindow` 相当于带有主窗口选项（标题、尺寸和最小尺寸）的 `app.Run`，这些选项在手机上会被忽略。桌面应用可以用 `ui.Window` 打开更多窗口，也可以用 `ui.MenuBar` 添加菜单栏：详见[导航](navigation.md)。

## 组件

组件就是返回 `ui.Node` 的 Go 函数。它只是一个普通函数：需要什么参数就接收什么参数，通过调用其他组件来创建子元素。

组件**只运行一次**。它构建好视图就返回，状态变化时也不会被再次调用。之后会变化的，只有那些读取了 signal 的部分。

会变化的属性接收一个 `signal.Accessor[T]`，可以是 signal、memo 或 `signal.Func`。永远不变的属性用 `ui.Val(v)`，它只发送一次，之后不再有任何开销。值为 nil 的访问器等同于零值（`false`、`""`、0），所以想设为 false 的属性直接省略就好。`OnPress` 这样的事件处理函数则是普通的 func。

```go
ui.Button(ui.ButtonProps{
	Title:    ui.Val("Save"),
	Disabled: saving, // 一个 *signal.Signal[bool]
	OnPress:  save,
})
```

## Signal

signal 保存一个值，由 `signal` 包创建：

- `signal.New(v)` 创建一个可比较类型的 signal。写入与当前值相等的值时，这次写入会被跳过。
- `signal.NewFunc(v, equal)` 创建一个使用自定义相等判断的 signal，适用于无法用 `==` 比较的值（切片、map，以及包含它们的结构体）。如果 `equal` 为 nil，每次写入都会发出通知。
- `Get` 读取值，并让正在读取它的东西（effect、属性、memo）订阅这个 signal。
- `Peek` 读取值但不订阅。在事件处理函数中用它，因为事件处理函数本来就不会被重新执行。
- `Set` 写入值；`Update(f)` 把值设为 `f` 作用于当前值的结果。

```go
count := signal.New(0)
count.Update(func(n int) int { return n + 1 })

tags := signal.NewFunc([]string{"go"}, nil)
tags.Update(func(t []string) []string { return append(slices.Clone(t), "ios") })
```

signal 可以在组件中作为局部变量创建；对于整个应用共享的状态，也可以定义为包级变量。

### 派生值

`signal.Func` 创建一个由其他值计算得出的访问器。它每次被读取时都会重新计算，而它在计算中读取了哪些 signal，会被读取它的那一方追踪：

```go
first := signal.New("Ada")
last := signal.New("Lovelace")
full := signal.Func(func() string { return first.Get() + " " + last.Get() })
```

`signal.NewMemo` 会缓存计算结果：它在第一次 `Get` 时计算，之后只有当它读取过的值发生变化时才重新计算。当计算有一定开销，或者有很多地方读取它时，就用 memo：

```go
left := signal.NewMemo(func() int {
	n := 0
	for _, t := range todos.Get() {
		if !t.Done {
			n++
		}
	}
	return n
})
```

`signal.NewMemoFunc` 是带自定义相等判断的 memo，就像 `NewFunc` 之于 signal。memo 中不能写入 signal：在 memo 里调用 `Set` 会 panic（`asuka vet` 也会报告这个问题）。

## 哪些部分会更新

每个接收了访问器的属性，都会成为一个独立的 effect。当它读取过的某个 signal 变化时，这个 effect 会重新运行，并把这一个属性发送给原生视图；其他任何东西都不会运行。

```go
func Score() ui.Node {
	score := signal.New(0)
	return ui.Column(nil,
		// 在 Score 运行时读取一次：这段文字永远不会变。
		ui.StaticText(fmt.Sprintf("Started at %d", score.Get())),
		// 在访问器中读取：这段文字会随 score 变化。
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%d points", score.Get()) })),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Score"),
			OnPress: func() { score.Update(func(n int) int { return n + 1 }) },
		}),
	)
}
```

第一段文字正是最常见的错误：组件只运行一次，所以在组件函数体中读取的 signal 也只会被读取一次。应该在访问器、`ui.StyleFunc` 或 effect 中读取 signal。如果你确实只想读取一次，就用 `Peek` 明确表达出来（对于访问器，用 `signal.Untrack`）。

[`asuka vet`](development.md#asuka-vet) 能找出这类问题，`asuka dev` 每次保存时都会运行它。它会报告这样的读取：在组件函数体中，或者在同样只运行一次的 `ui.Show` 分支或 `ui.For` 行中读取 signal，并把读到的值用在构建的视图里。应用显示的语言也是同样的道理：在函数体中直接取出的文案（`locales.Title()`）不会随语言切换而更新，而 `locales.Live.Title()` 会。

`ui.StyleFunc` 是会变化的样式。每个视图的 `StyleFunc` 运行在一个 effect 中；它返回的样式会与上一次比较，只有不同的属性才会被发送（只有布局相关的属性变化时，才会重新布局）。详见[布局与样式](layout.md#会变化的样式)。

## Effect

`signal.Effect(f)` 会运行 `f`，并在它读取过的值变化时再次运行。它用于视图之外的工作，比如保存数据、记录日志或发起请求。如果它是在组件构建期间或事件处理函数中创建的，会等组件构建完成或处理函数返回后再运行；否则立即运行。

在 effect 内部调用 `signal.OnCleanup(f)`，会在 effect 下一次运行之前以及它被销毁时运行 `f`。下面的例子会等用户停止输入后再发起搜索：

```go
signal.Effect(func() {
	q := strings.TrimSpace(query.Get())
	t := time.AfterFunc(300*time.Millisecond, func() {
		ui.Post(func() { results.Set(search(q)) })
	})
	signal.OnCleanup(func() { t.Stop() })
})
```

effect 可以写入 signal：被这些写入标记为“脏”的 effect 会在同一次刷新（flush）中重新运行。如果 effect 之间不停地相互触发，达到 100 轮，就会 panic，并指出其中一个 effect。

`signal.RenderEffect(f)` 会立即运行 `f`，之后在值变化时也会重新运行，而且总在用户的 effect 之前运行。`ui` 就是用它来处理属性的；应用很少需要直接用它。

`signal.Untrack(f)` 调用 `f`，但不订阅 `f` 中读取的任何值。它返回 `f` 的返回值：对于没有 `Peek` 方法的访问器，可以用它来读取。

```go
signal.Effect(func() {
	q := query.Get() // query 变化时会重新运行
	who := signal.Untrack(func() string { return full.Get() }) // 但 full 变化时不会
	fmt.Println(who, "searched for", q)
})
```

`signal.Batch(f)` 运行 `f`，然后把被其中的写入标记为脏的 effect 统一运行一次。事件处理函数和 `ui.Post` 已经自动批量处理写入，视图会在处理函数返回后统一更新一次；`Batch` 用于其他地方的写入。

## 条件显示与列表渲染

`ui.Show` 在条件为真时显示一个分支；如果提供了另一个分支，则在条件为假时显示它：

```go
ui.Show(signedIn,
	func() ui.Node { return ui.StaticText("Welcome back") },
	func() ui.Node { return SignIn(signedIn) },
)

ui.Show(signal.Func(func() bool { return len(items.Get()) == 0 }),
	func() ui.Node { return ui.StaticText("Nothing yet") })
```

分支在显示时才构建，隐藏时就会被销毁：它的 effect 停止运行，视图也被销毁。`Show` 只在条件真假翻转时重新构建，而不是每次重新计算条件时都重建。

`ui.For` 为切片中的每一项显示一行，并按 key 区分：

```go
ui.For(items, func(it Item) int { return it.ID }, func(item signal.Accessor[Item], index signal.Accessor[int]) ui.Node {
	return ui.Text(signal.Func(func() string {
		return fmt.Sprintf("%d. %s", index.Get()+1, item.Get().Name)
	}))
})
```

只要某个 key 还在切片中，对应的行就一直存在。出现新的 key 会构建新行；key 被移除，对应的行就被销毁；某一项移动了位置，它那一行的视图也会跟着移动。当某个 key 相同的项发生变化时，那一行不会重建：只是它的 `item` signal 被重新赋值，只有读取了它的部分会更新。请为每一项提供稳定的 key（比如 ID，而不是索引）。

`ui.Dynamic` 显示一个函数的返回结果，只要函数读取过的任何值变化，就会把整个结果重新构建：

```go
ui.Dynamic(func() ui.Node {
	switch mode.Get() {
	case "grid":
		return GridView()
	default:
		return ListView()
	}
})
```

这个函数读取的所有东西都会被追踪，包括它所调用的组件在函数体中的读取；每次变化都会重建整棵子树。所以应尽量使用 `Show` 和 `For`。

### 出现 panic 时

`ui.Catch` 可以防止界面某一部分的 panic 导致整个应用崩溃：它显示构建函数返回的内容；如果构建过程中发生 panic，或者其中的某个 effect 之后发生 panic，它就改为显示一个备用界面。

```go
ui.Catch(func() ui.Node { return Profile(id) }, func(err any, reset func()) ui.Node {
	log.Printf("profile: %v", err)
	return ui.Column(ui.Style{Gap: 8},
		ui.StaticText("Couldn't show this profile"),
		ui.Button(ui.ButtonProps{Title: ui.Val("Try again"), OnPress: reset}))
})
```

出错的界面会像被隐藏的 `Show` 分支一样被销毁：它的 effect 停止运行，视图也被销毁，包括构建过程中在 panic 之前已经创建的视图。调用 `reset` 会重新构建它。属于某个 `Catch` 的 effect 会在 recover 的保护下运行；不在任何 `Catch` 之内的 effect 则不会，因此也没有任何额外开销。

不归 `Catch` 管的部分，出现 panic 时照样会让应用崩溃，就和没有 `Catch` 一样：事件处理函数（`OnPress`）中的 panic、goroutine 中的 panic，以及备用界面本身的 panic。`Catch` 应该包在屏幕上可能独立出错的部分外面（由服务器数据驱动的控件、插件、富文档），而不是包住整个应用来掩盖 bug。

`signal` 包里的 `signal.Catch` 作用相同，用于没有界面的 effect。

### 长列表

`For` 会构建所有行，并一直保留它们。对于很长或没有上限的列表（信息流、成千上万行），请使用 `ui.List`：参数完全相同，但底层是平台的复用列表，只为屏幕上可见的部分构建行。详见[组件](components.md#列表)。

## owner 与清理

每个 effect 和 memo 都属于一个 owner（所有者），即创建它时的当前 owner。应用的根组件有一个 owner；每个 `Show` 分支、`For` 行、`Dynamic` 的每次构建、导航栈中的页面、标签页和弹出面板，也都在父级 owner 之下拥有自己的 owner。销毁一个 owner 时，会停止它的 effect，销毁它下面的所有 owner，并运行它的清理函数，后注册的先运行。

以下情况会销毁 owner：

- `Show` 切换分支时：旧分支的 owner；
- `For` 中某一项的 key 离开切片时：该行的 owner；
- `Dynamic` 重新构建时：上一次构建的 owner；
- 页面被弹出时（返回按钮、滑动手势、`Navigator.Pop`）：该页面的 owner；
- 弹出面板关闭时：其内容的 owner。

Go 的垃圾回收器负责释放内存，但它永远不会停止一个仍处于订阅状态的 effect：停止 effect 是 owner 的职责。在组件函数体中调用 `signal.OnCleanup`，会在组件的 owner 被销毁时运行。用它来停止组件启动的东西：

```go
func Clock() ui.Node {
	now := signal.New(time.Now().Format("15:04:05"))
	ticker := time.NewTicker(time.Second)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case t := <-ticker.C:
				ui.Post(func() { now.Set(t.Format("15:04:05")) })
			case <-done:
				return
			}
		}
	}()
	signal.OnCleanup(func() {
		ticker.Stop()
		close(done)
	})
	return ui.Text(now, ui.Style{Font: fonts.Title.Weighted(fonts.Semibold)})
}
```

用 `ui.Show(showClock, Clock)` 显示它时，时钟被隐藏后 ticker 就会停止，再次显示时则会启动一个新的 ticker。

## 主线程

组件、事件处理函数、effect 以及所有对 signal 的写入，都运行在平台的主线程上：在 iOS 和 Android 上是 UI 线程；在 macOS、Windows 和 Linux 上是进程的第一个线程，由宿主专门占用；在浏览器中是页面所在的线程。不要阻塞主线程：主线程在等待时，应用无法绘制、滚动或响应输入（桌面系统还会把窗口标记为“未响应”）。耗时的请求或大文件的处理应该放到 goroutine 中。

goroutine 不应该直接操作 signal 或视图。它应该把一个函数交给 `ui.Post`，由后者在主线程上运行这个函数，然后更新视图（如果 goroutine 自己写入了 signal，`asuka vet` 会报告出来）：

```go
load := func() {
	loading.Set(true)
	go func() {
		msg := "Done"
		resp, err := http.Get("https://example.com")
		if err != nil {
			msg = err.Error()
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		ui.Post(func() {
			status.Set(msg)
			loading.Set(false)
		})
	}()
}
```

[原生包](native.md)的方法运行在调用它的那个线程上。对于耗时的方法，请在 goroutine 中调用，或者传给它一个 context，再把结果 post 回主线程：

```go
go func() {
	level, err := b.Level() // 某个原生包的模块
	ui.Post(func() {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set(fmt.Sprintf("%.0f%%", level*100))
	})
}()
```

## 环境

`ui.UseEnvironment()` 以访问器的形式返回平台提供的、关于组件显示环境的信息：在访问器、`StyleFunc` 或 effect 中读取它，它就会随屏幕旋转、键盘、深色模式和文字大小的变化而更新。它反映的是组件所在页面的环境（导航栈中的页面、标签页、弹出面板或根组件）。请在组件构建期间调用它。

```go
env := ui.UseEnvironment()
orientation := signal.Func(func() string {
	if e := env.Get(); e.Width > e.Height {
		return "Landscape"
	}
	return "Portrait"
})
```

它包含以下字段：

- `Width`、`Height`：屏幕的尺寸，单位为点。这指的是应用的窗口，而不是设备：在分屏模式下（iPad 的分屏浏览、台前调度，Android 的分屏），它是应用所占的那部分；在桌面端或浏览器中，它是窗口的内容区域，会随用户调整窗口大小而变化。
- `SafeTop`、`SafeRight`、`SafeBottom`、`SafeLeft`：安全区域的边距。桌面窗口没有安全区域，它们都是 0。
- `Keyboard`：屏幕键盘遮住了底部多少高度。
- `Scale`：每个点对应的像素数：手机上是 2 或 3，Retina 屏的 Mac 上是 2，Windows 和 Linux 上是显示器的缩放比例（150% 时为 1.5）。
- `FontScale`：用户设置的文字大小（iOS 的动态字体、Android 的字体缩放、Windows 的文本大小、GNOME 的文本缩放）；1 是默认值。Mac 没有这样的设置，所以在 Mac 上它始终为 1。
- `Dark`：是否开启了深色模式。
- `ReduceMotion`：用户是否要求减少动态效果（iOS 和 macOS 的“减弱动态效果”、Android 的关闭动画、Windows 的关闭动画效果、GNOME 的关闭动画）。见[动效](motion.md)。
- `ScreenReader`：VoiceOver、TalkBack、讲述人或 Orca 是否已开启。见[无障碍](accessibility.md#屏幕阅读器是否开启)。
- `WidthClass`、`HeightClass`：窗口在每个方向上给应用留出了多少空间，按平台的分类方式表示为 `ui.SizeClassCompact`、`ui.SizeClassMedium` 或 `ui.SizeClassExpanded`。如果要决定是否在内容旁边放一个侧边栏，或者在列表旁边显示详情，应该根据它们来判断，而不是自己定一个宽度阈值。iOS 上报的是 UIKit 的尺寸类别，只有 compact 和 regular 两种（regular 对应 Expanded）；Android 上报的是 Material 的窗口尺寸类别（宽度小于 600 dp 为 compact，小于 840 为 medium，否则为 expanded；高度小于 480 dp 为 compact，小于 900 为 medium）。因此，竖屏的 iPad mini 在 UIKit 看来是 regular，在 iOS 上就是 Expanded；而同样尺寸的平板在 Android 上是 Medium，Material 在这个尺寸下仍然只显示单栏：两者各自遵循自己平台的规范。macOS、Windows、Linux 和 Web 没有自己的尺寸类别，会根据窗口尺寸套用 Material 的划分。
- `Locale`、`Languages`、`FirstWeekday`、`HourCycle`：用户的区域格式所对应的语言区域、用户能阅读的语言、每周从哪一天开始，以及使用 12 小时制还是 24 小时制。大多数应用通过 [`ui.Locale` 和 `ui.FormatLocale`](i18n.md) 来读取它们。
- `TimeZone`：设备的时区，以 IANA 名称表示。Asuka 会把它设为 Go 的 `time.Local`，所以 `time.Now().Format` 显示的就是设备上的时间。

语义颜色和字体会自动跟随深色模式和文字大小变化；它们覆盖不到的部分，再去读取环境信息。

```go
env := ui.UseEnvironment()
ui.Show(signal.Func(func() bool { return env.Get().WidthClass == ui.SizeClassExpanded }),
	func() ui.Node { return ui.Row(ui.Style{Grow: 1}, sidebar(), detail()) },
	func() ui.Node { return list() })
```

### 平台

`ui.Platform()` 返回应用所运行的系统：`ui.PlatformIOS`、`ui.PlatformAndroid`、`ui.PlatformMacOS`、`ui.PlatformWindows`、`ui.PlatformLinux` 或 `ui.PlatformWeb`。它反映的是设备的平台，在 `asuka dev` 下也是如此——即使手机应用的 Go 代码实际运行在你的电脑上。因此，请根据它来做分支判断，而不是根据 `runtime.GOOS` 或构建标签。它永远不会变化，所以只是一个普通的值：不需要访问器，也不需要 effect。

```go
variant := ui.ButtonFilled
if ui.Platform() == ui.PlatformIOS {
	variant = ui.ButtonGlassProminent
}
```

和 `UseEnvironment` 一样，请在组件构建期间调用它：`asuka dev` 可以在一个进程中同时为一部 iPhone 和一部 Android 手机提供服务，它们各有各的平台值。如果事件处理函数或 goroutine 需要用到它，先把它保存在变量里。

平台的原生视图本身就会呈现出应有的样子；`ui.Platform()` 适用于应用自己绘制的部分，好让它们遵循各平台的设计规范。比如 [`examples/usage`](../../examples/usage) 在 iOS 上绘制 UIKit 风格的嵌入式分组列表，在 Android 上则绘制 Material 3 风格的列表。

## 持久化的值

signal 只在应用运行期间存在。`store.Signal(key, def)` 创建一个值会跨启动保留的 signal：它的初始值是之前保存的值，没有的话就是 `def`；之后每次给它设置的值都会被保存下来。

```go
units := store.Signal("units", "metric")
```

它适合保存设置之类的小数据；关于存储和数据库，详见[数据](data.md)。在 `asuka dev` 下，所有 signal 的值都会在重新加载时保留，不需要写任何代码（见[快速上手](getting-started.md#运行)）。

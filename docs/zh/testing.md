# 测试

应用的界面就是一些 Go 函数，所以可以直接用 `go test` 来测试。`uitest` 包会在没有设备、没有窗口、也没有显示器的情况下挂载你的组件，然后像用户一样，根据屏幕上显示的内容去操作它们。

```go
package main

import (
	"testing"

	"github.com/egoist/asuka/uitest"
)

func TestCounter(t *testing.T) {
	a := uitest.Mount(t, Counter)
	a.Tap("Add")
	a.Tap("Add")
	a.Expect("Count: 2")
}
```

```sh
go test ./...
```

只要能运行 Go 的地方就能运行这些测试，CI 里也一样，不需要 iOS 模拟器或 Android 模拟器。如果要测试各平台的真实视图，请看下文的[在各平台上测试](#在各平台上测试)。

## 工作原理

`uitest.Mount` 把组件挂载到 Asuka 的假宿主（`host/fake`）上。假宿主会记下核心创建的视图，但不会真的创建平台的视图。除此之外，一切都和应用真实运行时一样：它的 signal 和 effect、核心的布局、导航、列表绑定行、弹出面板（sheet）和警告框。测试所在的 goroutine 就是主线程。

除非另行指定，应用会像在 iPhone 上一样运行（iOS，390 × 844 点）：

```go
uitest.Mount(t, App, uitest.Platform(ui.PlatformAndroid))  // Pixel 的屏幕
uitest.Mount(t, App, uitest.Platform(ui.PlatformMacOS))    // 一个 800 × 600 的窗口
uitest.Mount(t, App, uitest.Size(1024, 1366), uitest.Dark())
uitest.Mount(t, App, uitest.Languages("ar"))               // 显示阿拉伯语翻译，从右到左
uitest.Mount(t, App, uitest.FontScale(1.5))
uitest.Mount(t, App, uitest.Environment(func(e *ui.Environment) { e.ScreenReader = true }))
```

唯一和平台不同的是文字的测量方式：不管什么字体，每个字符 8 点宽，每行 20 点高。视图的位置来自核心真实的布局计算，所以视图之间的相对位置是可信的，但精确到点的尺寸并不是。

## 屏幕上显示什么

测试通过用户看到的文字来指代界面上的东西：一段文字、按钮的标题、输入框里的文字或占位文字、无障碍标签、页面标题、标签页、导航栏按钮、工具栏项、警告框的按钮、打开的菜单里的菜单项。文字需要完全一致才算匹配。

```go
a.Expect("Inbox", "Message 20")   // 每一项都显示了，否则测试失败
a.ExpectNo("Message 1")           // 只要有一项显示了，测试就失败
if a.Has("Signed in") { ... }
a.Texts()                         // 显示的所有文字，按阅读顺序
a.Get("Add")                      // 显示这段文字的视图：可以查看它的 Kind、Frame 和属性
a.All(ops.KindToggle)             // 显示出来的某一类视图
a.Disabled("Save")                // 它是否被禁用、无法点击
```

只有用户能看到的东西才算数：

- 每个导航栈最上面的页面（下面的页面看不到）；
- 选中的标签页的内容，以及标签栏里每个标签页的标题；
- 已经弹出的弹出面板、警告框和弹出框，它们盖在其他内容之上；
- 列表中能放进它高度的那些行：Asuka 按平台的请求绑定列表的行，假宿主也一样，就像集合视图（collection view）那样；`ScrollTo` 会继续往下绑定；
- 隐藏的东西都不算。

检查失败时，错误信息里会附上当时的屏幕：显示出来的每个视图及其类型、位置尺寸和文字，方便你看清那里实际显示的是什么。

## 像用户一样操作

每个操作都会等界面稳定下来再返回：它引起的变化已经完成布局，列表已经绑定，应用的 goroutine 之前 post 的工作也已经执行完。

```go
a.Tap("Add")                  // 按钮、列表行、标签页、导航栏按钮、警告框的按钮
a.LongPress("Message")        // 长按，或者打开上下文菜单
a.Type("Email", "ada@example.com")  // 占位文字或标签是 Email 的输入框
a.Submit("Email")             // 在这个输入框里按回车（""：最近输入过的那个输入框）
a.Toggle("Notifications")     // 这个标签旁边的开关
a.Slide("Volume", 0.8)
a.Select("Week")              // 分段控件、选择器或滚轮里的选项
a.Swipe("Message 3", "Delete")  // 列表行的滑动操作
a.Refresh("Message 20")       // 下拉刷新显示这段文字的列表
a.ScrollTo("Message 1")       // 滚动列表，直到某个列表显示出这段文字
a.Back()                      // 返回手势、Android 的返回键、Escape
a.Dismiss()                   // 向下轻扫、点击外部
a.Key("primary+n")            // 键盘快捷键：在 Mac 上是 ⌘N，其他平台是 Ctrl+N
a.OpenURL("myapp://note/3")
```

点击会落在手指会落下的地方：点列表行里的文字就是点这一行，点按钮里的文字就是点这个按钮。带菜单的视图被点击后会打开菜单，下一次 `Tap` 就在菜单里选择。当弹出面板、警告框或弹出框显示时，只有它能接收点击，点击它下面的东西会让测试失败。

没有自己文字的控件（开关、滑块、没有占位文字的输入框）通过它的标签来查找，就像用户读表单那样：标签同一行上的控件，或者标签下方最近的那个控件。带无障碍标签（`ui.ViewProps.Label`）的视图，也可以用这个标签指代它里面的控件。

`Back` 会关闭打开的菜单，或者弹出最上面的页面。没有页面可弹出时，它会关闭最上层的弹出面板、警告框或弹出框；用 `ui.OnBack` 可以把这一步变成一次询问：

```go
a.Type("Message", "On my way")
a.Back()
a.Expect("Discard this reply?")
```

## 同一段文字出现在多处

如果要点击的文字在屏幕上出现了不止一处，测试就会失败：比如导航栏按钮和工具栏项都叫 Star，或者每一行都有一个 Remove 按钮。`In` 可以把查找范围缩小到屏幕的某一部分，失败信息也会告诉你该缩小到哪一部分：

```go
a.In(uitest.Toolbar).Tap("Star")        // 工具栏的，不是导航栏的
a.In(uitest.NavigationBar).Tap("Edit")
a.In(uitest.Content).Has("Inbox")        // 页面自己的视图，不包括它的标题
a.In(uitest.NextTo("Ada")).Tap("Remove") // Ada 那一行上的 Remove
a.In(uitest.Under(a.Row("Ada"))).Tap("Remove")  // 显示 Ada 的那一个列表行里的
```

`NextTo` 依据的是元素的位置，所以即使没有一个视图同时包含这两者，它也能用：`ui.Row` 本身不会创建视图。

## 等待应用

在 goroutine 里做的工作（一次请求、一个定时器）会用 `ui.Post` 把结果 post 回来，测试在等界面稳定时会执行这些 post 过来的工作。如果要等还没完成的工作：

```go
a.Tap("Refresh")
a.WaitFor("Loaded 3 items")    // 等到它显示出来
a.WaitGone("Loading…")         // 等到它消失
a.Until("the upload to finish", func() bool { return uploads.Peek() == 0 })
```

等待会一直阻塞到应用 post 了新的工作，然后再检查一次。超过 10 秒（或者你传给 `Mount` 的 `uitest.Timeout`）就会失败。`uitest` 里没有任何地方会 sleep。

`ui.OnFrame` 驱动的动画可以用 `a.Frames(n)` 一帧一帧地推进，每秒 60 帧。测试直接写入的 signal，要在 `a.Settle()` 之后才会显示出来：

```go
items.Set(nil)
a.Settle()
a.Expect("No items")
```

## 平台服务与原生包

Asuka 中需要调用平台能力的包（`asuka/sdk/...`：相机、定位、通知）会调用宿主，在测试里就是假宿主：没有谁应答时，它们会得到一个错误，就像在不支持该服务的平台上一样。`Setup` 可以在应用挂载之前，按模块名给假宿主设置应答（`h.Modules`、`h.AsyncModules`），Asuka 自己的测试就是这么做的。有些服务，假宿主自带一个行为和平台一致的替身：`h.Store()` 是一个应用商店，售卖测试给它的商品，供 `asuka/sdk/purchase` 使用（见[应用内购买](purchases.md#测试购买)）。

[原生包](native.md)的模块是一个 Go 接口，所以最简单的测试方法是让组件接收这个接口，再在测试里传入你自己的实现：

```go
type fakeBattery struct{ level float64 }

func (b fakeBattery) Level() (float64, error) { return b.level, nil }
func (b fakeBattery) Watch(ctx context.Context, changed func(float64)) error { return nil }

func TestLowBattery(t *testing.T) {
	a := uitest.Mount(t, func() ui.Node { return Status(fakeBattery{0.1}) })
	a.Expect("Battery low")
}
```

`a.Host` 就是假宿主本身，可以用来做这里的方法没有覆盖到的事：查看发给它的操作（op）、拖放、把按键发给指定的视图。

## 快照

`a.Snapshot()` 会描述屏幕上显示的内容，每行一个视图：它的类型、位置尺寸、文字和状态（开关打开、滑块的值、已禁用）。`MatchSnapshot` 把它和 `testdata` 里的文件做比较：

```go
a.MatchSnapshot("signed-in")   // testdata/TestSettings/signed-in.txt
```

```
View 0,0 390x844
  Text 16,16 358x20 "Count: 0"
  Button 16,44 358x44 "Add"
  Button 16,96 358x44 "Reset" disabled
```

`go test -uitest.update` 会写入这些文件；用 `git diff` 检查改动，然后提交。快照能发现视图不见了、屏幕上的文字变了，以及布局发生了移动。

## 泄漏

测试结束时，`uitest` 会卸载应用；如果有视图比应用活得更久，测试就会失败。这说明有 effect 或视图是在本应释放它的 owner 之外创建的（见[核心概念](concepts.md)）。

## 在各平台上测试

`uitest` 测试的是应用的行为：屏幕上显示什么，用户操作之后会发生什么。至于每个平台怎么把它画出来，那是平台的事；Asuka 自己的测试会检查每个宿主画出的是不是核心发过去的内容（见[开发](development.md#测试)）。要在各平台上看到你的应用：

- `asuka dev --platform ios|android|web` 会在 iOS 模拟器、Android 模拟器、手机或者浏览器中运行应用，并在你保存时重新加载。
- 在桌面端，`ASUKA_SNAPSHOT=out.png` 会在窗口显示出来之后把它写成一张 PNG，不会抢占焦点，然后退出（`ASUKA_DUMP=1` 会把视图树写在旁边）。在 Windows 和 Linux 上，`ASUKA_SCRIPT` 会先执行一些步骤（`tap:Add`、`type:hello`）：见[开发](development.md#快照与脚本)。
- 在 Linux 上，在 Xvfb 下（`xvfb-run -a dbus-run-session`）不需要显示器也能这样做，CI 里也可以。

一旦测试挂载了应用，就不能并行运行（`t.Parallel`）：和在设备上一样，一个进程里只有一个应用核心。

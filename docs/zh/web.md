# Web

同一个应用也能在浏览器里运行：Go 代码编译成 WebAssembly，视图就是页面的 DOM。Web 和 iOS、Android、Mac、Windows、Linux 一样，是 Asuka 支持的平台之一，用的是同一份 Go 代码。核心创建的每个视图都对应一个元素，摆放在核心布局算出的位置上，原生宿主摆放自己的控件也是这样做的。因此布局用的是 Asuka 自己的实现（用 Go 写的 flexbox 和 grid），在每个平台上都一致；叶子节点则由真实的元素来测量，和原生宿主测量自己的控件一样。控件直接用浏览器自带的（`<button>`、`<input>`、`<select>`、`<progress>`、`<dialog>`），所以键盘操作、焦点、自动填充、拼写检查和屏幕阅读器支持都是现成的。

```sh
asuka dev --platform web examples/counter       # 启动服务并在浏览器中打开；保存即重载
asuka build --platform web examples/counter     # 生成静态网站，位于 examples/counter/dist/web
```

下面介绍 Web 上的各项行为；遇到浏览器做不到、而手机能做到的事情时，也会说明实际会怎样处理。

## asuka dev --platform web

`asuka dev --platform web` 会为浏览器构建应用（`GOOS=js GOARCH=wasm`），在 `http://localhost:8790/`（端口被占用时换一个空闲端口）上提供服务并自动打开。每次保存都会重新构建，示例项目通常远不到一秒就能完成；页面加载新构建时，应用中 signal 的值会保留下来，和其他平台的重载一样。应用的输出（`fmt.Println`、`log`、panic）以及页面上的错误都会打印到终端；如果构建失败，错误信息会覆盖在页面上，直到下一次构建成功为止。

除了 `asuka dev` 本身的参数（`--lang`、`--dir`、`--no-vet`、`-v`），还有这些参数：

- `--port`：服务器端口。
- `--lan`：同时向局域网提供服务，并打印可访问的地址，方便在手机浏览器里打开应用。
- `--no-open`：不自动打开浏览器。
- `--once`：只构建并提供服务，不监听文件变化。

## asuka build --platform web

`asuka build --platform web` 会在 `dist/web` 中生成一个静态网站，包含：

- `index.html` 和 `manifest.webmanifest`（Web 应用清单，有了它浏览器才会提示安装应用；图标取自 `asuka.json` 的 `icon`，同时生成 favicon 和 iOS 主屏幕图标）；
- 应用本身 `app.<digest>.wasm`，构建时去掉了符号表，并使用了 `-trimpath`；
- Web 宿主的文件，放在 `asuka/<digest>/` 中（如果应用导入了 `asuka/sdk/sqlite`，还包括 SQLite 的 WebAssembly 构建，约 0.9 MB，gzip 后 0.4 MB），以及应用自己的脚本（`web/*.js`），放在 `web/<digest>/` 中；
- 应用的 service worker `sw.<digest>.js`，与 `index.html` 放在一起（作用域是网站所在的文件夹）：即使没有打开应用的任何页面，它也能显示应用的通知和推送（见 [通知与 Web Push](#通知与-web-push)）；
- 应用的 `assets/`；
- 每个脚本和 wasm 文件的 gzip 压缩副本（`.gz`），供支持预压缩文件的服务器使用；
- `_headers`，Cloudflare 和 Netlify 会读取它：`index.html` 和清单设为 `no-cache`，以摘要命名的文件则永久缓存；
- `_redirects`，Netlify 会读取它：没有对应文件的路径都返回应用的页面，这样[路由器](#地址与历史记录)的地址才能打开应用。

`index.html` 的 `<base href>` 是网站所在的路径：默认是 `/`，也可以用 `asuka.json` 的 `web.base` 或 `--base` 指定（`https://example.com/app/` 就写 `"/app/"`）。这样页面无论在它下面的哪个地址打开，都能找到自己的文件，路由器的 href 也就是它下面的页面地址（`/chat/7` 对应 `https://example.com/app/chat/7`）。

这个文件夹可以放在任何 Web 服务器或 CDN 上。以内容摘要命名的文件一旦内容变化，文件名也会跟着变，所以可以放心地永久缓存，只有 `index.html` 需要重新获取。`.wasm` 文件要以 `application/wasm` 类型提供。一个计数器应用的 WebAssembly 约 6.5 MB，gzip 后 1.7 MB（用 brotli 压缩是 1.3 MB，CDN 一般可以帮你做）；应用自己的代码和它导入的包会在此基础上增加体积。

## 地址与历史记录

用了 [`ui.Router`](navigation.md#路由) 之后，页面的地址就是当前显示的页面：用 `pushState` 和 `replaceState` 写入网站根路径下的真实路径，而不是 `#` 片段。路由器导航的每一步都是页面历史里的一条记录，并保存着路由器的完整状态（每个导航栈里的页面、每个标签页、所有弹出面板）。因此：

- 浏览器的后退和前进会在导航栈、标签页和弹出面板之间来回，回到每条记录对应的页面；浏览器后退时会先询问页面或弹出面板的 `ui.OnBack`，如果它拦下了这次返回，页面就停在原地；
- 刷新后回到同样的页面，连同栈顶下面的页面一起；粘贴或从别处打开的链接会建出它所代表的整组页面（见[链接下方的页面](navigation.md#链接下方的页面)）；
- 页面标题跟随栈顶页面的标题（`标题 · 应用名`）。

历史记录的变化方式和 react-navigation 的 linking 一致：导航得更深（推入页面、打开弹出面板、切到第一个以外的标签页）会新增一条记录；返回时回到最近一条地址相同的记录（如果历史里从来没有这条记录，比如深度链接建出的页面被一个个弹出，就替换当前记录）；其余情况替换当前记录（`Replace`、重定向、守卫把用户送到别处、在第一个以外的标签页之间切换、`SetQuery`）。应用内的返回按钮、Escape 和 Alt+Left 会弹出导航栈，历史记录也随之后退。`Alert`、应用自己的 `Sheet`，以及不归路由器管理的导航栈，仍然像没有路由器时那样，为浏览器的后退保留自己的一条记录。

嵌在页面某个元素里的应用（见[嵌入](#嵌入)）不会改动页面的地址，它的路由器地址只留在应用自己内部。`Window` 面板里的路由器会把地址和面板的 `StateKey` 一起存在 `localStorage` 里。通知携带的、指向应用自身站点的 URL 会打开对应的页面。

### 让每个地址都能打开

刷新页面或粘贴链接时，浏览器会向服务器请求这个地址本身（`/app/chat/7`），而它并没有对应的文件：服务器必须返回应用的 `index.html`。`asuka dev --platform web` 已经这样做了。静态网站则需要：

| | |
|---|---|
| Netlify | 构建时生成的 `_redirects` |
| Cloudflare Pages | 什么都不用做：没有 `404.html` 的网站会被当作单页应用（它会提示 `_redirects` 里的规则形成了循环，并忽略这条规则） |
| Vercel | `vercel.json`：`{"rewrites": [{"source": "/(.*)", "destination": "/index.html"}]}`（放在网站根路径下） |
| Firebase Hosting | `firebase.json` 的 `hosting.rewrites`：`[{"source": "**", "destination": "/index.html"}]` |
| GitHub Pages | 把 `index.html` 复制一份命名为 `404.html`（任何地址都会以 404 状态返回它）；项目站点要把 `web.base` 设为仓库的路径 |
| nginx | 在网站的 `location /app/` 里写 `try_files $uri /app/index.html;` |
| Caddy | 在 `file_server` 块里写 `try_files {path} /app/index.html` |
| S3 和 CloudFront | 分发的错误页面：403 和 404 都以 200 状态返回 `/index.html` |

## 页面提供的环境信息

Web 应用看到的环境（`ui.UseEnvironment`）来自浏览器：

- `Platform` 是 `ui.PlatformWeb`。
- 尺寸是页面的尺寸（如果应用嵌在某个元素里，就是那个元素的尺寸），会随窗口变化；尺寸类别按宽度取 Material 的定义。
- 安全区域就是页面的安全区域（`env(safe-area-inset-*)`，即手机的刘海和主屏幕指示条，前提是页面设置了 `viewport-fit=cover`）；键盘高度是手机键盘遮住页面的那部分。
- 深色模式取自 `prefers-color-scheme`（或应用调用的 `ui.SetColorScheme`），减弱动态效果取自 `prefers-reduced-motion`，文字大小取自浏览器的默认字号（即 `FontScale`，文字会随之缩放），缩放比例取自显示器的像素比。
- 区域设置、语言、时区、一周的第一天和小时制都取自浏览器（`navigator.languages`、`Intl`）。

## 视图如何对应到页面

- 文本就是浏览器渲染的文本，使用系统字体（`system-ui`：Apple 系统上是 San Francisco，Windows 上是 Segoe UI，Android 上是 Roboto），字号层级采用 Web 的惯例（正文 16）。应用放在 `assets/fonts` 里的字体会按应用给的名字加载（`Font.Family`）。
- `Stack` 在自己的标题栏下面显示栈顶的页面：标题栏里有返回按钮、页面标题、栏按钮和搜索框。push 和 pop 都带滑动动画。浏览器的返回也能用：只要应用里还有可以返回的页面，浏览器的返回按钮（以及手机上的返回手势）就会在应用内返回；Escape、Alt+Left（Mac 上是 ⌘[）以及在触摸屏上从起始边缘滑动也一样。和其他平台一样，这些操作都由 `ui.OnBack` 处理。用了路由器之后，地址栏会显示当前页面，历史记录就是它的导航记录（见[地址与历史记录](#地址与历史记录)）。
- `Tabs` 是底部的标签栏；`SplitView` 是侧边栏加详情区，窗口较窄时侧边栏变成抽屉，由详情区的标题栏打开。
- `Sheet` 是一个模态 `<dialog>`：窄窗口下是底部弹出面板（medium 占一半高度，可以拖动顶部的把手把它拉高或拖走），宽窗口下是居中的卡片。按 Escape、点击外部或向下滑动都能关闭它。`Alert` 也是模态 `<dialog>`，操作表的按钮显示为列表。`Popover` 和菜单是显示在目标旁边的弹出层。`ui.Window` 是页面里的一个面板，用户可以移动它、调整它的大小。
- `List` 由宿主做虚拟化，和 Mac、Windows、Linux 上一样：只有可见范围内（以及上下各半屏）的行会被绑定；分组标题会吸顶；聊天列表会停在末尾。行的滑动操作变成它们的上下文菜单（右键或长按触发）。
- `Image` 是 `<img>`（在主线程之外解码，由浏览器缓存），`Video` 是 `<video>`，`WebView` 是 `<iframe>`（见 [Web 视图](#web-视图)），`Canvas` 是 2D 画布，`Shape` 是 SVG 路径，`Shader` 用 WebGL 2（Kage 程序会被转换成 GLSL ES 3.00），`Symbol` 显示的是 Android 宿主为该 SF Symbol 名称绘制的 Material Symbol。
- `ui.Animate` 用 Web Animations API 为视图的位置、尺寸和视觉属性做动画（弹簧动画用 CSS 的 `linear()` 曲线实现）；进入和退出过渡、匹配视图（`Style.Match`）的效果和 iOS 上一样。
- 点按、长按（以及右键）、悬停、按下状态、拖移、轻扫、捏合（双指，或触控板的捏合）和旋转，用鼠标、触控笔或手指都能操作；拖放用的是浏览器自己的机制，也支持从其他应用拖入文件。
- `MenuBar` 中菜单项的快捷键在页面里同样有效（Mac 上用 ⌘，其他系统用 Ctrl）。不过页面本身不显示菜单栏，而桌面端应用会显示：Mac 上是系统菜单栏，Windows 上是窗口的菜单栏，Linux 上是标题栏里的主菜单。

## 应用的文件

`asuka/sdk/store`、崩溃记录以及应用写在 `store.Dir` 里的文件都会持久保存。具体做法是：页面给 Go 提供一个内存中的文件系统，应用启动前先从浏览器存储（IndexedDB）里把数据目录的文件加载进来；文件变化后稍等片刻再写回去（大文件按 64 KiB 分块，只写变化过的块），页面被隐藏或关闭时也会写回。同一应用的多个标签页共享这些文件：一个标签页写入后，其他标签页会重新读取（如果某个标签页改了一个文件但还没写回，在写回之前它保留自己的版本；最后写入的标签页胜出）。用户拖入、粘贴或选择的文件会被复制到应用的缓存目录（`/tmp`），和 iOS、Android 上一样，然后以路径的形式交给应用。

浏览器会一直保留一个源（origin）的存储，直到用户清除它；或者设备空间不足时，浏览器会把它清理掉（对于没有添加到主屏幕的网站，Safari 在七天没有访问后也会清理）。

## SQLite

`asuka/sdk/sqlite` 的用法和其他平台一样，实时查询也支持。它就是同一个 SQLite 和同一个 Asuka 库，只是编译成了 WebAssembly（`asqlite.wasm`；凡是导入了 `asuka/sdk/sqlite` 的应用，`asuka dev --platform web` 和 `asuka build --platform web` 都会带上它）。它运行在页面的一个 worker 里，worker 把应用数据目录中的数据库保存在浏览器的源私有文件系统（origin private file system）中：数据库不会被整个加载到内存，提交返回时数据就已经落盘。每条语句都要给 worker 发一条消息再等回复，大约 30 微秒：一个包含 1000 次插入的事务约需 30 ms，查询 10,000 行约 3 ms。

与其他平台的区别：

- `Exec`、`Tx`、`Query`、`QueryOne` 和 `Migrate` 需要等待 worker，而页面的主线程不能等待，所以在主线程上调用它们会立即失败，并返回一个说明原因的错误。请改用 `ExecAsync`、`TxAsync` 和实时查询，或者放到 goroutine 里，这在所有平台上都适用；`app.Run` 之前的 `main` 可以等待。
- 日志模式是回滚日志（rollback journal），而不是预写日志（WAL 需要在连接之间共享内存）；而且每个数据库只有一个连接，所以读操作会等正在进行的写操作完成，而不是与之并行。
- 同一应用的多个标签页可以使用同一个数据库。标签页使用数据库时会持有它，其他标签页请求时再释放（在事务之间释放，绝不会在事务中间释放）；如果一条语句发现数据库被别的标签页持有，就会反复重试，直到对方释放为止，最多等待忙等超时的 5 秒，就像 SQLite 等待其他进程的锁一样。一个标签页的实时查询感知不到其他标签页的写入（就像原生平台上感知不到其他进程的写入一样：可以调用 `db.Changed()` 让它们重新运行）。
- 数据库是 worker 的文件，不在页面的文件系统里，所以 Go 的 `os` 看不到它。`db.Serialize()` 能得到它的一份副本（可以作为备份写到任何地方，或者交给 `Restore`）。不在数据目录里的数据库（比如 `/tmp` 下的）只在页面存续期间有效。
- 没有源私有文件系统时（页面不是通过 `https` 或 `localhost` 提供的，比如 `asuka dev --lan` 的地址；某些浏览器的隐私窗口），worker 会把数据库存放在 IndexedDB 中，使用期间放在内存里：提交返回后要稍等片刻才会持久保存。不是安全上下文的页面也没有 Web Locks：两个标签页同时写一个数据库时，可能会丢失对方的写入（控制台会给出提示）。
- 旧版本的 Asuka 把数据库和应用的其他文件一起存放（在 IndexedDB 中），应用第一次打开它时，它会被迁移到 worker 中。

如果你用自己的页面嵌入应用（见下文），需要向 `start` 传入 `libs`，也就是应用所用插件文件的 URL，以文件名为键。对 SQLite 来说就是 `{"asqlite.wasm": "asuka/<digest>/lib/asqlite.wasm"}`，即构建产物中的那个文件。worker 的脚本由宿主提供（`sqlite-worker.js`，与 `asuka.js` 放在一起）。

## 原生模块

Asuka 的包会调用宿主的原生模块（`ui.Call`）。在浏览器中：

- `asuka/sdk/secure`：值用 AES-GCM 加密后存放在 IndexedDB 中，密钥由浏览器保管且无法导出（一个不可提取的 WebCrypto 密钥）。它并不是钥匙串：在应用源中运行的脚本读不到密钥，但可以请求用它解密。它需要 `https`（或 `localhost`）。
- `device`：`GetInfo` 的信息来自 user agent（Chromium 的 client hints 能提供系统版本和 Android 手机型号）；`Clipboard`、剪贴板里的图片（PNG，保存到缓存目录）以及 `OnClipboardChange`（应用自己的复制操作都能收到；其他应用的复制，只有在用户允许页面读取剪贴板时才能收到）；`KeepAwake`（使用 Screen Wake Lock，页面重新显示时会再次获取）；`Browse`（打开新标签页）；`PickPhotos` 和 `PickFiles`（浏览器的文件选择器；选中的文件复制到缓存目录）。
- `ShareFiles`：通过分享面板分享文件（`navigator.share`），不支持时逐个下载。`asuka/sdk/preview`：在新标签页中打开文件（图片、视频、音频、文本、PDF、HTML）；其他类型返回 `ErrNoViewer`。
- `asuka/sdk/print`：可以打印 HTML、文件（PDF 在浏览器的查看器中打开）和应用自己的 Web 视图，使用浏览器的打印对话框；结果总是 `Unknown`（浏览器不会告知结果）。Web 上没有 `PDF`：浏览器只能通过打印对话框生成 PDF（另存为 PDF）。
- `asuka/sdk/compose`：使用 `mailto:` 和 `sms:` 链接；带附件时使用分享面板。
- `asuka/sdk/notify` 和 `asuka/sdk/push`：通过应用的 service worker 使用 Notification API 和 Web Push，详见 [通知与 Web Push](#通知与-web-push)。
- `asuka/sdk/network`：是否在线取自 `navigator.onLine`；在浏览器提供信息的地方（Android 上的 Chrome）能区分 Wi-Fi、蜂窝网络和以太网，蜂窝网络视为按流量计费，开启省流量模式时视为受限。
- `asuka/sdk/location`：使用 Geolocation API；Web 上没有 `PlaceName`（浏览器没有地理编码器）。
- `asuka/sdk/sensors`：使用 Generic Sensor API（Chrome）或设备运动和方向事件（Safari 需要在点按时请求权限），坐标轴和单位与 Android 一致；没有气压计和计步器。
- `asuka/sdk/speech`：使用 `speechSynthesis` 和浏览器自带的语音。
- `asuka/sdk/audio`：每个播放器对应一个 audio 元素（受浏览器自动播放规则限制：要在点按时开始播放）。`asuka/sdk/audio/record`：使用 `MediaRecorder`，浏览器支持时录制为 MPEG-4 封装的 AAC，否则录制为 WebM 封装的 Opus。
- `asuka/sdk/camera`：`Take` 会打开手机的相机，或者弹出一个带实时画面和快门按钮的对话框；`camera.View` 是放在 `<video>` 里的相机画面（支持 `Capture`、`record`，浏览器有 `BarcodeDetector` 时还能扫码）。`asuka/sdk/scan` 和 `asuka/sdk/vision` 的扫码功能使用 `BarcodeDetector`，人脸和文字识别只在浏览器支持时可用。
- `asuka/sdk/imageedit`：使用 canvas（JPEG 或 PNG；远程图片需要支持 CORS）。
- `asuka/sdk/maps`：和 Windows 上一样，由宿主用 OpenStreetMap 的瓦片绘制地图：支持图钉及其标注气泡，可以拖动、滚轮、捏合、双击以及用键盘移动地图，并按 OpenStreetMap 的要求显示署名。Satellite 和 Hybrid 只显示街道图（没有免费的卫星影像）；`ShowsUser` 在页面获得定位权限后显示用户位置（权限由 `asuka/sdk/location` 申请，地图本身从不申请）。瓦片按 `tile.openstreetmap.org` 对网页的使用政策获取（只加载屏幕上可见的部分，使用浏览器缓存，带上 Referer）；如果用量很大，可以给 `start` 传入 `mapTiles`（另一个瓦片服务器的 URL，包含 `{z}`、`{x}` 和 `{y}`），见 [嵌入](#嵌入)。
- `asuka/sdk/crash`：在浏览器中，Go 运行时无法把崩溃输出写进文件，所以宿主会保存它打印的内容，等程序因 panic 或致命错误结束时，把这些内容写入本次运行在应用文件中的记录，下次启动时再上报。未捕获的 JavaScript 错误（宿主的，或者你的 `web/*.js` 里的）会立即作为 `Exception` 报告上报，但不算致命错误（页面会继续运行）。每个页面会在 `localStorage` 里标记自己的运行仍然存活，这样应用的其他已打开页面就不会去动它。
- `asuka/sdk/biometric`：通过 WebAuthn 使用平台认证器（Touch ID、Face ID、Windows Hello、Android 的屏幕锁定），并要求用户验证：第一次调用 `Authenticate` 时会为网站创建一个通行密钥（passkey），以应用名命名。提示框由浏览器提供，所以原因说明和取消按钮的文字不会显示。
- `asuka/sdk/webauth`：使用弹出窗口，回到应用同源的某个页面时即视为完成（自定义 scheme 无法打开页面）。
- `asuka/sdk/screen`：`Lock`（仅在全屏或已安装的应用中有效）；隐藏系统栏就是进入全屏。
- `asuka/sdk/library`：`Save` 会下载文件。`asuka/sdk/contacts`：浏览器支持 Contact Picker 时（Android 上的 Chrome）可以用 `Pick`；`Add` 会下载一个 vCard 文件，`asuka/sdk/calendar` 的 `Add` 则下载一个 `.ics` 文件。
- `asuka/sdk/tracking`：浏览器发送了 Global Privacy Control 或 Do Not Track 时返回 Denied，否则返回 Allowed，不会弹出提示。
- `asuka/sdk/pay`：浏览器支持 Apple Pay 时（Safari）通过 Payment Request API 使用它，商户验证由应用的服务器完成（`OnValidateMerchant`），网站的域名需要在 Apple 注册；其他浏览器通过 Google 的 Web API（从 pay.google.com 加载的 `pay.js`）使用 Google Pay；两者都使用各自的官方按钮。支付面板只在点击时显示。
- 以下功能在 Web 上不可用，调用时会返回说明原因的错误（或包自己的“不可用”结果）：`asuka/sdk/review`、`asuka/sdk/integrity`、`asuka/sdk/agerange`、`asuka/sdk/appleauth`、`asuka/sdk/periodic`、`asuka/sdk/widget`、`asuka/sdk/liveactivity`、`asuka/sdk/photos`（返回 Denied，可以用 `device.PickPhotos` 选择照片）、读取通讯录和日历、`asuka/sdk/transcribe` 的文件转写，以及 `asuka/sdk/screen` 的 `PreventCapture` 和 `OnScreenshot`。

## 通知与 Web Push

每个 Web 应用都有一个 service worker，由 CLI 生成（`asuka dev` 下是 `sw.js`，构建产物中是 `sw.<digest>.js`），页面启动时注册，作用域是应用所在的文件夹。它没有 fetch 处理函数，所以页面的加载方式和没有它时完全一样；它负责显示应用的通知和推送，并响应对它们的点击，无论应用是否有页面处于打开状态。浏览器只会为通过 HTTPS 或 `localhost` 提供的页面运行 service worker（`asuka dev` 的地址可以，`--lan` 的地址不行）。

### asuka/sdk/notify

- `Request` 会弹出浏览器的权限提示；浏览器只在用户点按或点击时才允许请求，所以要在这类事件里调用它。
- `Show` 通过 worker 显示通知，因此页面关闭后通知仍保留按钮，点击也依然有效。（没有 worker 的页面，比如没有注册它的嵌入页面，会显示页面自己的通知，没有按钮，点击也只会传给那个页面。）
- 通知的 ID 就是它的 tag：ID 相同的新通知会替换旧通知，并再次提醒用户，除非设置了 `Silent`。`Subtitle` 显示为正文的第一行，`Image` 是配图（`https` URL 或应用资源），消息发送者的头像（`From`）作为图标。`Badge` 和 `SetBadge` 设置已安装应用的角标（`navigator.setAppBadge`）。
- 分类的操作（`SetCategories`）就是通知上的按钮（Chrome 和 Edge 显示两个，Firefox 和 Safari 不显示）；在支持内联回复的浏览器上（Android 和桌面版的 Chrome），`Reply` 操作是一个文本框，`Response.Reply` 中就是输入的文字；在其他浏览器上它只是一个按钮。分类会为 worker 保存下来，所以指定了分类的推送也会带上这些按钮。
- 点按通知会让应用页面获得焦点、打开通知的 `URL`（`ui.OnOpenURL`），并触发 `OnAction`（`Action ""`）；点按按钮则会带着按钮 ID 触发 `OnAction`，由前台页面处理，没有前台页面时交给应用的任意一个页面。如果没有打开的页面，worker 会打开一个（浏览器只能在页面里运行应用的代码），应用会在启动时收到这个响应，就像应用被点按通知启动时一样：所以要在根组件里设置 `OnAction`。没有 `Open` 的操作不会把已经打开的页面带到前台；但如果没有任何打开的页面，它仍会打开一个并放到前台。`Response.Defer` 在这里无事可做：页面本来就会继续运行。
- 页面在前台时收到的通知（包括推送）如何显示，由 `Present` 询问应用决定：`Banner`、`Quiet`（无声音）或 `Hide`。
- `Showing`、`Remove`、`RemoveThread`、`RemoveAll` 和 `Cancel` 作用于 worker 的通知，包括之前的页面显示的通知和推送通知。
- 这里没有 Android 的通知渠道：相关设置会被接受但忽略，和 iOS 上一样。

#### 定时通知

当应用没有任何页面处于打开状态时，浏览器无法在指定时间显示通知：没有相应的 API（Chrome 的 Notification Triggers 曾经能做到，但已在 2021 年放弃），而 service worker 只在有事件唤醒它时运行，不会按计时器运行。所以 `Schedule` 只能尽浏览器所能：

- 已安排的通知保存在应用的存储（Cache Storage，应用的页面和 worker 都能访问）中，而不是保存在页面里。
- 只要应用有任何一个页面开着，通知就会按时显示：每个页面都为下一条通知设置一个计时器（某个页面安排或取消通知时会告知其他页面），第一个发现通知到期的页面负责显示它，而且只显示一次（通过 Web Lock 保证）。后台页面可能会延迟：浏览器会把隐藏页面的计时器放慢到大约每分钟一次；最晚在页面回到前台时显示。
- 如果通知到期时没有打开的页面，下一个页面打开时会立刻显示它，此时已经过了预定时间。
- 在那之前，worker 会尽力而为：在没有打开页面的情况下，只要有事件唤醒它，它就会显示已到期的通知。这类事件包括推送、点击，以及 Chrome 的定期后台同步（只要有已安排的通知，页面就会申请它）。但 Chrome 只把定期后台同步授予已安装的应用（添加到主屏幕或从地址栏安装），最多大约每 12 小时运行一次，具体时间由浏览器决定（取决于用户与应用的互动程度、电量和网络），其他浏览器则根本不支持：千万不要依赖它。

如果通知必须在应用关闭时也准时显示，请在那个时间从你的服务器发送推送。

### asuka/sdk/push：Web Push

Web Push 需要一对 VAPID 密钥：应用的服务器用私钥为推送签名，浏览器用公钥订阅。公钥写在 `asuka.json` 里：

```json
{ "web": { "vapidPublicKey": "BKpYsAT0ADj2pHQQ..." } }
```

密钥对只需生成一次，私钥保存在服务器上；换一对新密钥就需要重新订阅。可以用 `webpush.GenerateKeys`（见下文）生成，也可以用 `npx web-push generate-vapid-keys`。

`push.Register()` 会请求通知权限（要在点按时调用：浏览器只在这时才允许请求），用公钥为应用的 worker 订阅推送（之前用其他密钥创建的订阅会被替换），并返回：

- `Token`：订阅的 JSON，格式与 `PushSubscription.toJSON()` 相同：`{"endpoint": "https://...", "expirationTime": null, "keys": {"p256dh": "...", "auth": "..."}}`。原样发给你的服务器即可，Web Push 库都能直接使用它。浏览器可能会更换订阅（`pushsubscriptionchange`）：所以和其他平台一样，每次启动时都调用一次注册，如果得到的 Token 和服务器上的不同，就重新发送。
- `Service`：`"webpush"`。
- `Allowed`：true。没有权限时 `Register` 会失败：浏览器只允许会显示推送的网站订阅（`userVisibleOnly`）。

在以下情况下它会失败并说明原因：没有配置 `web.vapidPublicKey`；没有 worker（页面不安全）；浏览器不支持 Web Push。iOS 和 iPadOS 上的 Safari 只为添加到主屏幕的应用提供 Web Push（16.4 及以上版本）。

推送总是显示为一条通知，无论有没有打开的页面。如果应用有打开的页面，页面会先收到它（`push.OnReceive`，每个打开的页面都会收到），可以改写它（`push.Rewrite`，在前台页面中运行，限时约十秒；没有打开的页面时，推送按原样显示），并决定如何显示（`notify.Present`）。点按推送会触发 `push.OnOpen`、`notify.OnAction`（设置了 `Remote`），如果数据中有 `"url"`，还会触发 `ui.OnOpenURL`，和本地通知一样。

#### 服务器发送的内容

载荷是 JSON，针对订阅加密（RFC 8291 的 `aes128gcm`），并附带 VAPID 令牌（RFC 8292）发送，所有 Web Push 库都是这样做的。worker 会读取下面这些键，与其他平台上 `push.Message` 的字段相同：

| 键 | |
| --- | --- |
| `title`、`body`、`subtitle` | 显示的内容（副标题是正文的第一行） |
| `data` | 应用自己的键，即 `push.Message` 的 `Data`（非字符串的值会以 JSON 形式传入）；`"url"` 是点按通知时应用打开的地址（`ui.OnOpenURL`） |
| `id` | 通知的 tag：`id` 相同的推送会替换它 |
| `category` | `notify.SetCategories` 中的某个分类：它的操作就是通知上的按钮 |
| `thread`、`summary` | 即 `push.Message` 的同名字段，供 `notify.RemoveThread` 和应用使用 |
| `image` | 配图：`https` URL 或应用的某个资源 |
| `badge` | 已安装应用的角标 |
| `silent` | 显示时不播放声音 |

```json
{
  "id": "chat-7",
  "title": "Ada",
  "body": "Lunch?",
  "category": "message",
  "thread": "chat-7",
  "data": { "url": "myapp://chat/7", "chat": "7" }
}
```

没有 `title` 或 `body` 的推送是数据消息：打开的页面会收到它（`OnReceive`），但不会显示任何内容。这类推送只应在应用打开时发送：浏览器期望每条推送都显示一条通知；如果没有前台页面、也没有显示通知，Chrome 会显示一条它自己的通知（“此网站已在后台更新”），Safari 则会在几次之后取消订阅。不是 JSON 的载荷会作为正文显示。载荷最大 3993 字节。

在 Go 中，`github.com/egoist/asuka/sdk/push/webpush` 负责加密、签名和发送，只依赖标准库：

```go
import "github.com/egoist/asuka/sdk/push/webpush"

var sender = &webpush.Sender{
	Keys:    webpush.Keys{Public: os.Getenv("VAPID_PUBLIC_KEY"), Private: os.Getenv("VAPID_PRIVATE_KEY")},
	Subject: "mailto:ops@example.com", // 推送服务联系你的方式
}

func notifyChat(ctx context.Context, token string) error {
	sub, err := webpush.ParseSubscription(token) // 应用的 push.Register 返回的 Token
	if err != nil {
		return err
	}
	err = sender.Send(ctx, sub, webpush.Notification{
		ID: "chat-7", Title: "Ada", Body: "Lunch?", Category: "message",
		Data: map[string]string{"url": "myapp://chat/7"},
	}, &webpush.Options{Urgency: "high"})
	if errors.Is(err, webpush.ErrGone) {
		return forgetSubscription(token) // 订阅已结束：用户或浏览器取消了它
	}
	return err
}
```

`Options` 用来设置推送的 `TTL`（浏览器离线时推送服务保留推送的时长，默认四周）、`Urgency` 和 `Topic`（主题相同的后一条推送会替换仍在等待的前一条）。`webpush.Keys.Authorization` 和 `webpush.Encrypt` 是这一过程的两半，适合自己发送请求的服务器使用。

在 Node 中，可以使用 [`web-push`](https://github.com/web-push-libs/web-push) 库：

```js
import webpush from "web-push";

webpush.setVapidDetails("mailto:ops@example.com", process.env.VAPID_PUBLIC_KEY, process.env.VAPID_PRIVATE_KEY);

// token：应用的 push.Register 返回的 Token
await webpush.sendNotification(JSON.parse(token), JSON.stringify({
  title: "Ada", body: "Lunch?", category: "message", data: { url: "myapp://chat/7" },
}), { urgency: "high" });
// 返回 404 或 410（err.statusCode）表示订阅已结束，删掉它即可。
```

## Web 视图

`WebView` 是一个 `<iframe>`，每加载一个页面都会新建一个，所以它有自己独立的历史记录，不会往标签页的历史里添加记录。

应用自己站点的页面（应用的资源、HTML、应用写入的文件，以及与页面同源的任何 URL）可以使用全部功能：宿主用真实的 URL 把它加载到 frame 中，并在页面脚本运行之前注入 `window.asuka.postMessage` 和 `Preload`；支持 `EvalJS` 和 `EvalJSResult`（会等待 promise 完成）；加载、失败和状态事件（URL、标题、`pushState`、进度、前进和后退）；后退、前进、重新加载和停止；页面中的链接、表单和 `location` 变化都通过这个视图加载；`Block`（拦截页面的标记、`fetch`、XHR、`sendBeacon`、WebSocket、EventSource 以及元素的 `src` 和 `href`；拦截时已经发出的请求仍可能发送出去）；`OnRequest`（基于 Resource Timing）；`OnNewWindow`（`window.open`、`target=_blank`）；`OnDownload`（附件、frame 无法显示的类型、`<a download>`）；`WebCookies`（`document.cookie`，不包括 `HttpOnly` 的 cookie）。HTML 中的相对链接相对于应用的资源解析。

其他站点的页面只能加载，因为浏览器会把不同站点隔离开：加载完成后触发 `Navigated`，站点无法访问时触发 `Failed`。如果站点拒绝被嵌入（`X-Frame-Options`、`frame-ancestors`），视图无法感知，浏览器会显示它自己的错误页。视图无法追踪页面里的链接跳转到哪里，也读不到页面标题；无法在页面里运行脚本、对它使用 `Block` 或 `Preload`、报告它的请求、读取它的 cookie 或设置 user agent（脚本和 cookie 相关的调用会返回错误，其他的会警告一次）。frame 处于沙箱中，不允许顶层导航，所以页面无法把应用导航走。

愿意配合的其他站点页面可以通过 `postMessage` 与视图通信（视图只接受来自自己 frame 的消息，并且只向已声明就绪的源发送脚本）：

```js
// 在页面中：向应用发送消息（OnMessage）、页面的 URL 和标题，并处理 EvalJS。
const app = (m) => parent.postMessage({ asuka: m.asuka ?? true, ...m }, "*");
addEventListener("message", async (e) => {
  if (e.source !== parent || !e.data?.asuka) return;
  if (e.data.asuka === "hello") app({ asuka: "ready", url: location.href, title: document.title });
  if (e.data.asuka === "eval") {
    try { app({ asuka: "result", id: e.data.id, value: JSON.stringify(await (0, eval)(e.data.script)) }); }
    catch (err) { app({ asuka: "result", id: e.data.id, error: String(err) }); }
  }
});
app({ asuka: "ready", url: location.href, title: document.title });
app({ text: "Hello from the page" }); // OnMessage
```

`asuka/sdk/js` 在一个隐藏的沙箱 frame 中运行脚本，这个 frame 有自己独立的源，因此脚本访问不到应用的页面和存储。

## 原生代码

[原生包](native.md) 的 JavaScript 放在它的 `web/` 目录中：包括生成的 `<package>.asuka.js` 和包自己的 `<package>.js`，后者导出用于创建模块和视图的函数：

```js
// segmented/web/segmented.js
export function makeSegmented(events) {
  const el = document.createElement("div");
  let items = [], selected = 0;
  const render = () => { /* 每一项一个按钮；点击时调用 events.changed(i) */ };
  return {
    el, // 视图本身，尺寸由核心分配的位置决定
    setItems(v) { items = v; render(); },
    setSelected(v) { selected = v; render(); },
    measure: (c) => [items.length * 72, 32], // 在约束 c 内的尺寸（{width, height, availW, availH}，不受约束时为 NaN）
  };
}
```

每个包的 `web/` 发布在 `asuka/<digest>/packages/<package>/` 下，页面会在应用启动前导入应用用到的所有包。包里的其他文件（辅助脚本、资源）也会一起发布，所以包可以导入它们，或者相对于 `import.meta.url` 获取它们。其中任何文件变化时，`asuka dev` 都会重新加载页面。

`examples/media/segmented` 里有一个原生视图，它的 JavaScript 在 `examples/media/segmented/web/segmented.js`。

## 嵌入

`asuka build --platform web` 生成的页面会让应用占满整个页面。如果要把应用放到你自己页面的某个元素里，加载宿主的加载器，然后在这个元素中启动应用；之后应用会跟随元素的尺寸变化：

```html
<link rel="stylesheet" href="asuka/<digest>/style.css">
<div id="app" style="width: 390px; height: 700px"></div>
<script type="module">
  import { start } from "./asuka/<digest>/asuka.js";
  start({ container: document.getElementById("app"), wasm: "app.<digest>.wasm", name: "com.example.app", serviceWorker: "sw.<digest>.js" });
</script>
```

一个页面上可以运行多个应用，各自放在自己的元素里。`serviceWorker` 用来注册应用的 worker（即构建产物中的 `sw.<digest>.js`，要从它所服务页面所在的文件夹提供）：不传的话，通知就是页面自己的通知，`asuka/sdk/push` 也无法订阅。使用 `asuka/sdk/push` 时还要像 `index.html` 那样传入 `vapidPublicKey`；如果想让 `asuka/sdk/maps` 从你自己的服务器获取瓦片，就传入 `mapTiles`（如 `"https://tiles.example.com/{z}/{x}/{y}.png"`；默认使用 OpenStreetMap 的瓦片）。

## 尚未支持的功能

- 文本框的辅助工具栏只在有屏幕键盘的设备（触摸屏）上显示。
- 用 `asuka/sdk/notify` 安排的通知，在应用没有任何页面打开时无法按时显示：浏览器没有提供定时显示通知的方法（见 [上文](#定时通知)）。

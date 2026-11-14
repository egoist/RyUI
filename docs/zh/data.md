# 数据

应用的数据通常放在这几个地方：零散的小数据用 `asuka/sdk/store`，成批的记录用 `asuka/sdk/sqlite` 存进 SQLite，密钥和密码用 `asuka/sdk/secure` 放进平台的钥匙串，应用自己的文件则直接用 Go 的 `os` 读写。本页会逐一介绍它们，另外还会讲到如何通过 `net/http` 访问网络、如何监视文件变化，以及哪些调用可能会阻塞主线程。

## 数据存在哪里

`store.Dir()` 是应用的数据目录。`asuka/sdk/store` 的文件就放在这里，`sqlite.Open` 用普通名字（而不是路径）打开的数据库也放在这里。你自己的文件也建议放在这个目录下。

| | `store.Dir()` |
|---|---|
| iOS | 应用容器里的 Application Support 目录（如果应用有[分享面板](device.md#应用自己的分享面板appsharesheet)，则位于它的 App Group 中） |
| Android | 应用的 files 目录 |
| macOS | `~/Library/Application Support/<name>` |
| Windows | `%LOCALAPPDATA%\<name>` |
| Linux | `$XDG_DATA_HOME/<id>`（即 `~/.local/share/<id>`；在 Flatpak 中则是沙盒自己的对应目录） |
| Web | `/data/<name>`，位于一个内存文件系统中，页面会把它保存到浏览器的存储（IndexedDB）里 |

`<name>` 是 `asuka.json` 里的 `name`，`<id>` 是其中的 `id`；如果不通过 CLI 而是直接用 `go run` 运行应用，两者都取程序名。这个目录不会预先创建，第一次往里写东西时才会建出来。在 Windows 和 Linux 上，宿主自己也会在这里放几个文件（主窗口的大小和位置、应用当前选择的语言）。在浏览器中，这个目录里的文件会在应用启动前加载好，修改后稍过片刻写回，页面被隐藏时也会写回，详见 [Web](web.md)。

在 `asuka dev` 下，Go 代码运行在你的电脑上（桌面应用如此，iOS 和 Android 在热重载模式下也是如此），此时 `store.Dir()` 指向 `asuka dev` 在电脑上专门准备的目录（位于用户的缓存目录下的 `asuka/` 中，每个应用、每个平台各一个）。这样你开发时随手试出来的数据，就不会和已安装版本的数据混在一起。在热重载模式下，store、数据库和你的文件都在电脑上，而不在设备上。唯一的例外是 `asuka/sdk/secure`：它和发布构建一样使用平台自己的存储（热重载模式下就是设备的钥匙串）。在 Web 上，`asuka dev` 打开的页面把文件保存在浏览器里，按开发服务器的地址区分。

## 小数据：asuka/sdk/store

`asuka/sdk/store` 用来在多次启动之间保存一些小数据，比如设置项、已登录用户的名字、上次打开的标签页。值以 JSON 形式存在同一个文件里（`store.Dir()` 中的 `store.json`），在值改变后稍过片刻于主线程之外写入；应用进入后台时也会写入。各桌面平台和浏览器对“进入后台”的理解不同：在 Mac 上是应用被隐藏、最小化或退出时；在 Windows 上是窗口失去焦点时；在 Linux 上是应用的所有窗口都不可见时（每个窗口都被隐藏、最小化或位于其他工作区），以及应用退出、会话结束或收到 SIGTERM 时；在浏览器中则是页面被隐藏时。它不是数据库：记录很多或很大时，请使用 SQLite。

`store.Signal` 创建一个值会被持久保存的 signal。它的初始值就是之前保存下来的值（第一次则是你给的默认值），之后每次给它设置新值，这个值都会被保存：

```go
func Settings() ui.Node {
	dark := store.Signal("dark", false) // 第一次是 false；之后是上次设置的值
	return ui.Toggle(ui.ToggleProps{Value: dark})
}
```

和其他 signal 一样，它也要在主线程上创建。也可以把它放在包级变量里（`examples/downloader` 中就有 `var wifiOnly = store.Signal("wifiOnly", false)`）。每次调用都会创建一个新 signal，用同一个键创建的两个 signal 并不会互相同步，所以只创建一个，然后在各处共用它。

`Get`、`Set` 和 `Delete` 直接操作值。`Set` 接受任何 `encoding/json` 能编码的值；`Get` 把值解码到一个指针里，如果这个键没有值，或者值无法解码成目标类型，就返回 false：

```go
type Prefs struct {
	Theme    string
	FontSize int
}

store.Set("prefs", Prefs{Theme: "dark", FontSize: 17})

var p Prefs
if !store.Get("prefs", &p) {
	p = Prefs{Theme: "light", FontSize: 15}
}

store.Delete("prefs")
```

`store.Flush()` 会一直等到所有待写入的内容都写完。应用进入后台时 Asuka 会自动调用它，所以只有在测试里你才需要手动调用。

## 记录：sqlite 插件

`asuka/sdk/sqlite` 为应用提供 SQLite 数据库。它是一个[插件](plugins.md)：只要导入它，CLI 就会把它的库和应用一起打包。它就是 SQLite 本身，用的是 SQLite 的 C 源码，同一个版本在每个平台上以相同的方式构建，外加 Asuka 自己的一个小库，让 Go 只需一次调用就能执行完一整条语句，并分块取回结果行。Go 调用它时不需要 cgo：在 macOS、Linux、iOS 和 Android 上通过 purego，在 Windows 上通过 syscall。在浏览器中，它是同一份 C 代码编译成的 WebAssembly（见 [Web](web.md#sqlite)）。[`examples/notes`](../../examples/notes) 用到了它；它是怎么打包发布的，见[库文件](#库文件)。

### 打开与迁移

```go
var db *sqlite.DB

func main() {
	var err error
	db, err = sqlite.Open("notes.db")
	if err != nil {
		panic(err)
	}
	err = db.Migrate(
		// 1
		`create table notes (id integer primary key, text text not null, done bool not null default 0)`,
		// 2
		`alter table notes add column created_at integer not null default 0;
		 create index notes_done on notes (done)`,
	)
	if err != nil {
		panic(err)
	}
	app.Run(App)
}
```

`Open` 接受三种参数：一个名字，表示 `store.Dir()` 里的文件（目录不存在时会自动创建）；一个绝对路径；或者 `":memory:"`。打开时它会开启预写日志（WAL）和外键约束，并把忙等超时（busy timeout）设为 5 秒。

每个数据库有一个负责写入的连接；如果是文件数据库，旁边还有最多三个只读连接，在需要读的时候才打开。写操作轮流使用这个写连接：一个写要等前一个写完，事务会一直占用它直到结束，而等待超过忙等超时的写会以 `ErrBusy` 失败（在 `db.Tx` 的函数里调用 `db.Exec` 也会这样失败，那里应该用 `tx.Exec`）。读操作从不等待写操作，看到的是最近一次提交之后的数据库。`":memory:"` 数据库的读写共用一个连接。

`Migrate` 把数据库结构升级到最新。每个参数代表数据库的一个版本，按顺序排列：其中的 SQL（多条语句用分号分隔）只会在一个事务中执行一次，执行后版本号记录在 `PRAGMA user_version` 里。要修改表结构，就新增一步；已经发布出去的步骤永远不要改，因为已经执行过它的设备不会再执行一遍。

`db.Close()` 关闭数据库。大多数应用在启动时打开一个数据库，然后一直用下去。

### 查询

`sqlite.Query[T]` 执行查询，并把结果行扫描到一个 `T` 的切片里。如果 `T` 是结构体，每一列会填入同名的字段：字段名取它的 `db` 标签，没有标签就取字段本身的名字；匹配时不区分大小写，并忽略列名中的下划线（`created_at` 会填入 `CreatedAt`）。没有对应字段的列会被跳过；标记了 `db:"-"` 的字段永远不会被填充。如果 `T` 不是结构体，则扫描第一列。

```go
type Note struct {
	ID      int64
	Text    string
	Done    bool
	Created int64  `db:"created_at"`
	Draft   string `db:"-"`
}

notes, err := sqlite.Query[Note](db, `select * from notes where done = ? order by id`, false)

n, ok, err := sqlite.QueryOne[Note](db, `select * from notes where id = ?`, 1)

count, _, err := sqlite.QueryOne[int](db, `select count(*) from notes`)
```

`QueryOne` 返回第一行；如果一行都没有，`ok` 为 false。

列的值会尽量转换成字段的类型：整数放进 `string` 字段时变成它的十进制数字；文本放进 `int` 字段时，只要它本身是个整数就可以；整数放进 `bool` 字段时，非零即为 true。放不下的值（实数放进 `int`、300 放进 `int8`）会报错。NULL 会让字段保持零值，指针字段（`*string`）则为 nil。`time.Time` 字段可以从文本读取（`sqlite.TimeFormat` 格式、SQLite 日期时间函数写出的格式，或 RFC 3339），也可以从自 1970 年起的秒数（`unixepoch()`）读取。如果字段的指针类型实现了 `sqlite.Scanner`（比如 `sql.NullString`，或应用自己的类型），就由它自己来扫描值。嵌入结构体的字段视同外层结构体自己的字段。

### 参数

`?` 按顺序依次取参数，`?2` 取第二个参数；`:name`、`@name` 和 `$name` 取 `sqlite.Named` 命名参数：

```go
db.Exec(`update notes set text = :text where id = :id`,
	sqlite.Named("id", 1), sqlite.Named("text", "Buy milk"))
```

参数可以是：整数、浮点数、bool（写成 1 或 0）、字符串、`[]byte`（作为 blob；nil 即 NULL）、`time.Time`（写成 `sqlite.TimeFormat` 格式的文本，即 `2006-01-02 15:04:05.999999999-07:00`，SQLite 的日期函数能读取它，并且在同一时区内按字符串排序就是按时间排序）、nil、指向以上任一类型的指针（nil 即 NULL）、底层类型是以上之一的应用自定义类型（`type Status string`），或者 `driver.Valuer`（`sql.NullString`）。

### 执行与事务

`db.Exec` 执行一条语句（insert、update、delete、create……），或者用分号分隔的多条语句，并返回 `sqlite.Changes`，其中包含 `LastInsertID` 和 `RowsAffected`。`db.Tx` 在一个事务里运行一个函数：函数返回 nil 就提交；返回错误（`Tx` 会把这个错误原样返回）或发生 panic 就回滚。在这个函数里，用 `tx.Exec` 写入，读取时则把 `*sqlite.Tx` 而不是数据库传给 `sqlite.Query` 和 `sqlite.QueryOne`：

```go
err := db.Tx(func(tx *sqlite.Tx) error {
	if _, err := tx.Exec(`update notes set done = 1 where id = ?`, 1); err != nil {
		return err // 回滚
	}
	left, _, err := sqlite.QueryOne[int](tx, `select count(*) from notes where done = 0`)
	if err != nil {
		return err
	}
	fmt.Println(left, "left")
	return nil // 提交
})
```

### 在主线程之外写入

`Exec`、`Tx`、`Query`、`QueryOne` 和 `Migrate` 都会等待磁盘。在主线程上写入时，请改用 `ExecAsync` 和 `TxAsync`：它们在主线程之外执行，完成后再回到主线程调用 `done`。如果 `done` 传 nil，出错时会把错误记到日志里。

```go
db.ExecAsync(`insert into notes (text, created_at) values (?, ?)`, []any{"Buy milk", time.Now().Unix()},
	func(r sqlite.Changes, err error) {
		if err != nil {
			status.Set("Couldn't save: " + err.Error())
			return
		}
		status.Set(fmt.Sprintf("Saved note %d", r.LastInsertID))
	})

db.TxAsync(func(tx *sqlite.Tx) error {
	if _, err := tx.Exec(`delete from notes where done = 1`); err != nil {
		return err
	}
	_, err := tx.Exec(`update notes set text = trim(text)`)
	return err
}, nil)
```

要在主线程上读取数据，可以用实时查询（见下文），或者开一个 goroutine 去查询，再用 `ui.Post` 把结果行送回主线程。

### 实时查询

`sqlite.Watch[T]` 是一个实时查询：它是一个装着结果行的 signal，创建时就会在主线程之外取一次数据，之后每当它读取的某张表上有写操作提交，就再取一次。请在组件里创建它，它会随组件的 owner 一起停止。

```go
func Notes() ui.Node {
	notes := sqlite.Watch[Note](db, `select * from notes order by id desc`)
	return ui.Text(signal.Func(func() string {
		r := notes.Get()
		switch {
		case r.Loading:
			return "Loading…"
		case r.Err != nil:
			return r.Err.Error()
		}
		return fmt.Sprintf("%d notes", len(r.Rows))
	}))
}
```

它的值是一个 `sqlite.Result[T]`：`Rows` 是最新的结果行；`Err` 是查询最近一次遇到的错误；`Loading` 在第一次取数完成之前为 true。把 `Rows` 交给 `ui.List`，列表就会跟着表的内容变化，`examples/notes` 就是这么做的。

实时查询只关心它读取的那些表。它在 SQLite 准备（prepare）这条语句时得知自己读了哪些表，视图背后的表也算在内；之后只要其中某张表有写操作提交，它就会重新执行，触发器和外键动作写入的行也算。写其他的表不会让它重新执行。如果它正在取数时又来了好几次写入，之后只会再多取一次，而不是每次写入各取一次。

`sqlite.WatchWith[T]` 相当于参数会变化的 `Watch`。它的参数来自一个读取 signal 的函数（比如搜索框里的文字、分页大小），参数变化时查询会重新执行，就像数据库变化时一样。新的结果行到来之前，旧的结果会一直保留；如果某次取回的结果对应的参数已经变了，这批结果就会被丢弃。

```go
func Search() ui.Node {
	q := signal.New("")
	notes := sqlite.WatchWith[Note](db, `select * from notes where text like ? order by id`,
		func() []any { return []any{"%" + q.Get() + "%"} })
	return ui.Column(nil,
		ui.TextField(ui.TextFieldProps{Value: q, Placeholder: ui.Val("Search")}),
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%d found", len(notes.Get().Rows)) })),
	)
}
```

会写入数据的查询（`insert ... returning`）和 `Exec` 一样，也会被实时查询察觉到。

### 错误

SQLite 返回的错误是 `*sqlite.Error`，包含结果码（扩展结果码，例如 `2067` 就是 `SQLITE_CONSTRAINT_UNIQUE`）和错误信息。`errors.Is` 会按结果码把它和包里的 `Err` 系列值比对；代表一整类错误的值能匹配这一类下的所有结果码：

```go
_, err := db.Exec(`insert into users (email) values (?)`, email)
switch {
case errors.Is(err, sqlite.ErrConstraintUnique):
	status.Set("That email is taken")
case errors.Is(err, sqlite.ErrConstraint): // 其他任何约束错误
	status.Set("Check the form")
case err != nil:
	status.Set(err.Error()) // sqlite: no such table: users (SQLITE_ERROR)
}
```

### 备份

`db.Serialize()` 返回数据库的字节内容，和它的文件里存的一模一样，你可以把它存到任何地方、分享出去或者上传。`db.Restore(data)` 用这些字节替换整个数据库，并让所有实时查询重新执行一遍。在所有平台上（包括 Web），这都是完整复制一个数据库的方法；在有文件系统的地方，也可以用 `VACUUM INTO 'path'` 把副本写到文件里。

`db.Changed()` 会让所有实时查询重新执行。当数据库发生了它自己察觉不到的变化时（比如另一个进程改了它，或者文件被替换了），就调用它。

### 库文件

所有平台上的 SQLite 都是 3.54.1 版本，内置了 FTS5、R*Tree、Geopoly、数学函数、`dbstat`、`STAT4` 和 `soundex`，不支持加载扩展，临时表保存在内存中（手机上可能没有临时目录）。它就是插件的库 `asqlite`，为每个平台分别构建，发布在插件的 GitHub release 上；插件的清单文件（`sdk/sqlite/asuka-plugin.json`）列出了每个文件及其 SHA-256，CLI 和这个包会从那里把它们下载到用户的缓存目录，只下载一次。所以应用的开发者不需要安装 C 编译器。

- **`asuka build`** 会把这个库和应用一起打包，和其他插件的库一样（见[插件](plugins.md#库放在哪里)）：在 macOS 上放在应用的 `Contents/Frameworks` 中（并随应用一起签名），在 Windows 上放在 `.exe` 旁边，在 Linux 上放在安装前缀的 `lib/<app>` 中，在 iOS 应用里是 `asqlite.framework`，在 APK 里则每种 ABI 各放一份。
- **`go run`、`go test`、`asuka dev`** 在桌面端会从用户的缓存目录加载已发布的库；在手机上，`asuka dev` 会像构建时一样把它放进应用里。嵌入到其他应用中的应用（`asuka build --xcframework`）会附带一个 `asqlite.xcframework`，宿主应用也需要把它一并嵌入（见[发布](shipping.md#嵌入现有应用)）。
- **`ASUKA_SQLITE_LIB`** 可以指定改为加载另一个库（必须是这个插件构建出来的库；包会检查它的 SQLite 版本）。

如果要修改 SQLite 或插件的 C 代码，在仓库的检出目录里：`go generate ./sdk/sqlite` 会构建所有平台的库（Apple 平台用 Xcode，Android 用 NDK，Linux、Windows 和 Web 用 Zig），输出到 `sdk/sqlite/build`，从此这个检出目录里的测试和示例都会使用它；再用 `go run ./cmd/asuka plugin publish sdk/sqlite` 把它发布出去。

## 机密信息：asuka/sdk/secure

`asuka/sdk/secure` 用来保存令牌、密码这类小型机密信息，由平台负责加密。它只适合存机密，不适合存普通数据：其他东西请放在 store 或数据库里。

- **iOS**：存进钥匙串（Keychain），作为应用 bundle ID 下的通用密码；设备首次解锁后即可读取，且只保存在本机。
- **Android**：每个值都用 Android Keystore 中的密钥加密（AES-GCM），这个密钥永远不会离开 Keystore；加密后的值保存在应用私有的 preferences 中。
- **macOS**：存进登录钥匙串，作为应用 bundle ID 下的通用密码（没有 bundle ID 时用应用名），可以在“钥匙串访问”中看到。临时签名（ad hoc）的应用也能使用它，`asuka dev` 运行的应用和没有 Developer ID 的构建都属于这种情况。
- **Windows**：存进凭据管理器，作为当前用户的通用凭据，名称为 `<name>/<key>`，只保存在本机（不会漫游），可以在控制面板的“凭据管理器”中看到。每个值最多 2560 字节。
- **Linux**：通过 libsecret 和 Secret Service（GNOME Keyring、KWallet、KeePassXC）存进用户的密钥环：每个键对应一个条目，使用应用自己的 schema（即应用的 ID），标签为 `<name>: <key>`，放在默认集合中，可以在密钥环应用（GNOME 的“密码和密钥”）里看到。如果没有安装 libsecret 或没有运行密钥环，调用会失败，并在错误中说明原因。
- **Web**：浏览器没有钥匙串。每个值都用 WebCrypto 生成的不可导出密钥加密（AES-GCM，256 位），并和密钥一起保存在应用的 IndexedDB 数据库中。这样任何脚本都读不到密钥的字节，值也不会以明文形式落到磁盘上。不过，运行在应用同源下的任何代码仍然可以让这个密钥替它解密：这是 Web 应用能做到的最好程度，但终究不是钥匙串。它要求使用 https 或 localhost。

在主线程上，`Load`、`Save` 和 `Remove` 接受一个回调，回调同样在主线程上执行：

```go
func Account() ui.Node {
	status := signal.New("")
	secure.Load("token", func(token string, ok bool, err error) {
		switch {
		case err != nil:
			status.Set(err.Error())
		case !ok:
			status.Set("Signed out")
		default:
			status.Set("Signed in")
		}
	})
	return ui.Column(nil,
		ui.Text(status),
		ui.Button(ui.ButtonProps{Title: ui.Val("Sign out"), OnPress: func() {
			secure.Remove("token", func(err error) { status.Set("Signed out") })
		}}),
	)
}

func signIn(token string) {
	secure.Save("token", token, nil) // nil：不需要回调
}
```

在 goroutine 里则使用 `Get`、`Set` 和 `Delete`，它们会等平台处理完再返回。永远不要在主线程上调用它们：

```go
go func() {
	token, ok, err := secure.Get("token")
	if err != nil || !ok {
		return
	}
	// ……使用 token……
}()
```

## 文件与网络

文件操作就用 Go 的 `os`，网络请求就用 Go 的 `net/http`，Asuka 没有另外提供任何东西。文件请放在 `store.Dir()` 里。Asuka 的各个包交给你的文件（拍摄的照片、录制的视频、选中的照片、拖放或粘贴进来的文件）都在应用的缓存目录或临时目录中，系统可能会清理它们：想保留的话，就把文件移到 `store.Dir()` 里。在浏览器中，`os` 操作的是页面的文件系统：`store.Dir()` 会被持久保存，其余部分（`/tmp`、缓存目录）只在页面存续期间存在。浏览器中的 `net/http` 底层是浏览器的 `fetch`，所以跨域请求需要目标源返回相应的 CORS 头。

这两者都会阻塞等待，所以要放在 goroutine 里做，再用 `ui.Post` 把结果送回主线程：

```go
func Avatar() ui.Node {
	path := signal.New("")
	status := signal.New("Downloading…")
	go func() {
		file := filepath.Join(store.Dir(), "avatar.jpg")
		err := download("https://example.com/avatar.jpg", file)
		ui.Post(func() {
			if err != nil {
				status.Set(err.Error())
				return
			}
			status.Set("")
			path.Set(file)
		})
	}()
	return ui.Column(nil,
		ui.Text(status),
		ui.Image(ui.ImageProps{Source: path}),
	)
}

func download(url, file string) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", res.Status)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, res.Body)
	return err
}
```

如果要在应用不可见时让长时间的下载继续进行，请参阅 [`asuka/sdk/background`](device.md#后台任务asukasdkbackground)；如果想知道当前连接是 Wi-Fi 还是按流量计费的网络，请参阅 [`asuka/sdk/network`](device.md#网络状态asukasdknetwork)。[`examples/downloader`](../../examples/downloader) 把这两件事都做了。

## 监视文件：asuka/sdk/watch

`asuka/sdk/watch` 会在文件变化时通知应用，适用于这样一些场景：应用正在展示的文件夹、在编辑器里打开的项目、由另一个程序写入的配置文件、被其他应用保存过的文档。它在所有平台上都能用，包括手机和 Web。

在组件里，`watch.Path` 会在组件存在期间监视一个路径，每当有一批变化，就在主线程上调用你给的函数。在这个函数里重新读取文件即可：

```go
func Folder(dir signal.Accessor[string]) ui.Node {
	names := signal.NewFunc[[]string](nil, slices.Equal)
	reload := func() {
		d := signal.Untrack(dir.Get)
		go func() {
			list := readNames(d) // os.ReadDir，在主线程之外执行
			ui.Post(func() { names.Set(list) })
		}()
	}
	signal.Effect(func() { dir.Get(); reload() }) // dir 变化时也会重新读取
	w := watch.Path(dir, watch.Options{Ignore: watch.Patterns(".*")}, func([]watch.Event) {
		reload()
	})
	return ui.Column(nil,
		ui.Show(signal.Func(func() bool { return w.Err() != nil }), func() ui.Node {
			return ui.Text(signal.Func(func() string { return w.Err().Error() }))
		}),
		ui.For(names, func(n string) string { return n }, func(n signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
			return ui.Text(n)
		}),
	)
}
```

监视会跟随它的访问器：路径一变，旧的监视就停止，并开始监视新路径；组件销毁时监视也随之停止。由于列出一个大文件夹需要一些时间，监视是在一个 goroutine 里启动的：启动完成后 `w.Watching()` 会告诉你，而 `w.Err()` 会给出它没能启动的原因（路径不存在、无法读取、包含的文件太多：`watch.ErrLimit`）。你的函数运行期间，监视会暂时等待，因此一个忙碌的应用会在一批里收到这段时间内积攒的所有变化，而不是收到很多批。

如果不在主线程上，比如在命令行工具或同步循环里，可以用 `watch.New` 在任意 goroutine 中监视，再用 `Next` 逐批取出变化：

```go
w, err := watch.New(ctx, dir, watch.Options{Recursive: true, Ignore: watch.Patterns("node_modules/", ".git/")})
if err != nil {
	return err
}
defer w.Close()
for {
	events, err := w.Next(ctx)
	if err != nil {
		return err // 关闭后，或 dir 被移走后，返回 io.EOF
	}
	for _, e := range events {
		fmt.Println(e) // "create src/a.go", "rename a.txt -> b.txt", ...
	}
}
```

事件的 `Op` 可以是 `Create`、`Write`、`Remove`、`Rename`（附带 `OldPath`）、`Attrib`（权限变化，需开启 `Options.Attributes`）或 `Overflow`；它的 `Path` 是相对于被监视路径的路径，用斜杠分隔，被监视路径本身记为 `.`。监视会把当前看到的状态和之前看到的状态做比较，所以事件描述的是“有什么不同”，而不是导致这些不同的每一个步骤：

- 一个文件被连续写了很多次，只会产生一个 `Write`；其间被创建又被删除的文件则什么事件都不会产生。变化会等到安静 50 毫秒（`Debounce`）后才报告；如果变化持续不断，最多也只等 500 毫秒（`MaxDelay`）。
- 一个文件以编辑器常用的方式保存（先写临时文件，再重命名覆盖原文件），会报告为 `Write`，而不是一次删除加一次创建，并且新文件随后也会继续被监视。
- 一个文件或文件夹在被监视的文件夹内移动，不管移得多远，在所有平台上都报告为 `Rename`：监视是根据文件的身份（设备号和 inode，或 Windows 的文件 ID）而不是名字来识别文件的。移入或移出被监视范围的内容则报告为 `Create` 或 `Remove`。
- 当系统丢弃了一些变化（一次变化太多）时，这一批会以 `Overflow` 开头：监视已经重新查看了所有内容，这一批中剩下的事件就是它查到的结果。
- 当被监视的文件夹（或被监视文件所在的文件夹）被删除或重命名时，最后一批会包含一个针对 `.` 的 `Remove`，然后监视结束。

不开启 `Recursive` 时，对文件夹的监视只报告它直接包含的文件和文件夹，不报告更深层的内容。文件是通过它所在的文件夹来监视的：直接监视文件自己的路径，它被替换、删除和重新出现都会以 `.` 报告。符号链接按其本身报告，从不跟随。`Ignore` 会排除匹配到的内容，并且从不进入被忽略的文件夹：`watch.Patterns` 接受 `.gitignore` 的简单模式（`*.log`、`node_modules/`、`/dist`、`src/**/gen`）。

每个平台都使用各自原生的机制：macOS 上是 FSEvents（一个流就能监视整棵目录树）；iOS 上是 kqueue（每个文件和文件夹都要占用一个描述符，所以在 iOS 上请只监视应用自己的文件夹）；Android 和 Linux 上是 inotify（每个文件夹一个 watch，上限是 `fs.inotify.max_user_watches`）；Windows 上是 `ReadDirectoryChangesW`。在 Web 上，应用的文件就是页面里的文件（见[数据存在哪里](#数据存在哪里)），监视能看到的是应用自己的代码对它们做的修改。监视的范围不会超出被监视的文件夹：读不了的内容会被略过；`MaxEntries`（macOS 上为 65536 个文件和文件夹，其他平台为 8192 个）会让对过大目录树的监视以 `watch.ErrLimit` 停止。[`examples/files`](../../examples/files) 展示了一个实时更新的文件夹。

## 主线程

界面运行在主线程上，在主线程上等待的调用会让界面卡住。[主线程](concepts.md#主线程)一节解释了这条规则；具体到数据相关的调用，可以归纳为：

- **不要在主线程上调用**：`sqlite.Open`、`Migrate`、`Exec`、`Tx`、`Query` 和 `QueryOne`（启动时、调用 `app.Run` 之前除外）；`secure.Get`、`Set` 和 `Delete`；文件和网络调用。
- **要在主线程上调用**：`ExecAsync`、`TxAsync`、`Watch`、`WatchWith`；`secure.Load`、`Save` 和 `Remove`；`store.Signal`（和其他 signal 一样）。
- **在哪里调用都行**：`store.Get`、`Set` 和 `Delete`，它们只读写内存（文件是在主线程之外写入的）。

在 goroutine 里，`ui.Post(f)` 会把 `f` 放到主线程上执行：请在那里设置 signal。

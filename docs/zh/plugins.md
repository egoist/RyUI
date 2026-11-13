# 插件

插件是一个自带库的 Go 包：这些库用 C、Zig 或 Rust 编写，已经为每个平台预先构建好，插件的 Go 代码不借助 cgo 就能调用它们。使用插件只需导入它，命令行工具会在每个平台上把它的库和应用一起打包发布。`asuka/sdk/sqlite` 就是一个插件（见[数据](data.md#记录sqlite-插件)）。

## 使用插件

导入即可：

```go
import "github.com/egoist/asuka/sdk/sqlite"

db, err := sqlite.Open("notes.db")
```

不需要做别的事。`asuka build`（以及在手机上运行的 `asuka dev`）会从应用的依赖中找出插件，并把每个插件对应构建平台的库一起打包。`go run`、`go test` 和在桌面端运行的 `asuka dev` 则会在第一次使用时把库下载到用户的缓存目录，之后都从那里加载。每个文件在使用前都会用插件清单中记录的 SHA-256 进行校验。

### 库放在哪里

| 平台 | 位置 | |
|---|---|---|
| macOS | `Contents/Frameworks` | 随应用一起签名 |
| Windows | `.exe` 旁边 | 通过 `ASUKA_WINDOWS_CERT` 与 `.exe` 一起签名 |
| Linux | 安装前缀下的 `lib/<app>` | 包含在 tar 包和 Flatpak 中 |
| iOS | `Frameworks/<library>.framework` | 带有自己的 Info.plist 和隐私清单，随应用一起签名 |
| Android | 每个 ABI 目录中，与 `libasuka.so` 放在一起 | 不压缩，按 16 KB 页对齐 |
| Web | 宿主文件中的 `lib/` | 由页面的 `libs` 选项指定每个文件的 URL |

一个应用的所有插件的库都放在同一个位置，所以对同一个平台而言，任意两个插件都不能有同名文件；如果有，构建时会告诉你是哪两个。

如果应用被构建成嵌入到另一个应用中的形式（`asuka build --platform ios --xcframework`），每个库都会附带一个 XCFramework，供那个应用一并嵌入。

## 编写插件

插件就是一个旁边带有清单文件 `asuka-plugin.json` 的包：

```json
{
  "name": "sqlite",
  "sources": ["csrc", "internal/build"],
  "libraries": [
    {
      "name": "asqlite",
      "version": "3.54.1",
      "release": "https://github.com/egoist/asuka/releases/download/plugin-sqlite",
      "files": {
        "darwin": {"name": "libasqlite.dylib"},
        "ios": {"name": "asqlite"},
        "ios-simulator": {"name": "asqlite"},
        "android-arm64": {"name": "libasqlite.so"},
        "linux-amd64": {"name": "libasqlite.so"},
        "windows-amd64": {"name": "asqlite.dll"},
        "wasm": {"name": "asqlite.wasm"}
      }
    }
  ]
}
```

- `name` 是插件的名称，命令行工具在输出信息中会用它来指代这个插件。
- `sources` 是构建这些库所用的源文件，路径相对于清单文件：如果某个库在构建之后源文件又有改动，它就不会被发布。不写这一项时，默认是包中除测试以外的所有文件。
- 每个库的 `files` 指定它在各平台上的文件名，可用的平台有：`darwin`（两种 Mac 共用一个文件：arm64 和 x86_64）、`ios` 和 `ios-simulator`、`android-arm64`、`android-arm`、`android-amd64`、`linux-amd64`、`linux-arm64`、`windows-amd64`、`windows-arm64` 以及 `wasm`。Mac 上的文件是 `.dylib`；iOS 上的文件以库名命名（它是对应 framework 的二进制文件，install name 为 `@rpath/<library>.framework/<library>`）。`release` 是这些文件发布到的 GitHub release。`version` 是库自身的版本号，用于它的 iOS framework。

插件的 Go 代码写在清单所在目录下的某个文件中，它嵌入清单，并从 `plugin.LibraryPath` 给出的路径加载库：

```go
//go:embed asuka-plugin.json
var manifest []byte

func load() (uintptr, error) {
	path, err := plugin.LibraryPath(manifest, "asqlite")
	if err != nil {
		return 0, err
	}
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL) // Windows 上用 syscall.LoadLibrary
}
```

库导出的符号要尽量少，只导出它自己的，不要导出它链接进来的东西：进程中可能还有另一份相同来源的代码（比如 GTK 加载的系统 SQLite）。调用时每次完成一整个操作，而不是每个值调用一次函数：从 Go 调用 C 的每一次开销，都比一个小函数本身的工作量还大（sqlite 插件每条语句只调用一次）。

### 构建与发布

插件用它自己的工具来构建库，按库的需要来定：`sdk/sqlite/internal/build` 会用 Xcode、Android NDK 和 Zig 编译 SQLite。它用 `plugin.StoreBuild` 写出每个目标平台的文件，附上插件的 `plugin.SourceHash`，放到清单旁边的 `build/<target>` 目录中。这个目录不纳入版本控制。在仓库的检出副本中，插件自己的测试以及同一仓库中的应用都会直接使用这里的构建产物。

```sh
go generate ./sdk/sqlite                         # 构建所有目标平台
go run ./cmd/asuka plugin publish sdk/sqlite     # 发布
```

`asuka plugin publish` 会把每个文件用 gzip 压缩，并以它的哈希命名（`asqlite-darwin-1982f596dd7c-libasqlite.dylib.gz`）。它通过 `gh` 上传 release 中还没有的文件；如果 release 不存在，就先创建它。上传后，它会把每个文件重新下载回来校验，再把它们记录到清单中——你要提交的正是这个清单。release 中已有的文件永远不会被替换，因此历史上任何一次提交中的清单都始终可用。如果构建产物比插件的源文件旧，发布会中止：先重新构建即可。

## 平台宿主的原生代码

调用平台 API（iOS 和 macOS 上的 Swift、Android 上的 Kotlin、Web 上的 JavaScript）或显示平台视图的包，是[原生包](native.md)。它不需要清单：它的 Swift package 和 Gradle 模块都是自己的，会自行引入各自的依赖。一个包可以同时是插件和原生包。

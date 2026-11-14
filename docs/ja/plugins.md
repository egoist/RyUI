# プラグイン

プラグインは、ライブラリを同梱した Go パッケージです。ライブラリは C、Zig、Rust で書かれ、プラットフォームごとにビルド済みで、プラグインの Go のコードから cgo を使わずに呼び出されます。プラグインを使うにはインポートするだけで、CLI がそのライブラリを、どのプラットフォームでもアプリと一緒に配布します。`asuka/sdk/sqlite` もプラグインの一つです（[データ](data.md#レコードsqlite-プラグイン)）。

## プラグインの使い方

インポートします。

```go
import "github.com/egoist/asuka/sdk/sqlite"

db, err := sqlite.Open("notes.db")
```

ほかに必要なことはありません。`asuka build`（とスマートフォンでの `asuka dev`）は、アプリの依存関係の中からプラグインを見つけ、それぞれのライブラリをビルド対象のプラットフォーム向けに同梱します。`go run`、`go test`、デスクトップでの `asuka dev` は、初回にライブラリをユーザーのキャッシュにダウンロードし、以降はそこから使います。どのファイルも、使う前に、プラグインのマニフェストに記された SHA-256 と照合されます。

### ライブラリの置き場所

| プラットフォーム | 場所 | |
|---|---|---|
| macOS | `Contents/Frameworks` | アプリと一緒に署名される |
| Windows | `.exe` の隣 | `ASUKA_WINDOWS_CERT` を使い、`.exe` と一緒に署名される |
| Linux | インストールプレフィックスの `lib/<app>` | tarball と Flatpak に含まれる |
| iOS | `Frameworks/<library>.framework` | Info.plist とプライバシーマニフェスト付きで、アプリと一緒に署名される |
| Android | 各 ABI の `libasuka.so` の隣 | 非圧縮、16 KB ページサイズ対応 |
| Web | ホストのファイルの `lib/` | ページの `libs` オプションで各ファイルの URL を指定する |

アプリのプラグインのライブラリはすべて一か所に置かれます。そのため、同じプラットフォーム向けに同じ名前のファイルを持つプラグインが二つあってはいけません。その場合は、どの二つが衝突しているかをビルドが示します。

ほかのアプリに組み込むためにビルドしたアプリ（`asuka build --platform ios --xcframework`）には、ライブラリごとの XCFramework が付いてきます。組み込み先のアプリで、それらも埋め込んでください。

## プラグインの作成

プラグインは、`asuka-plugin.json` というマニフェストを隣に置いたパッケージです。

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

- `name` はプラグインの名前で、CLI のメッセージに使われます。
- `sources` は、ライブラリのビルド元で、マニフェストからの相対パスで指定します。ビルドしたあとにソースが変更されたライブラリは公開されません。指定しなければ、パッケージ内のテスト以外のすべてのファイルが対象になります。
- 各ライブラリの `files` には、プラットフォームごとのファイル名を書きます。プラットフォームは `darwin`（両方の Mac、つまり arm64 と x86_64 に共通の一つのファイル）、`ios` と `ios-simulator`、`android-arm64`、`android-arm`、`android-amd64`、`linux-amd64`、`linux-arm64`、`windows-amd64`、`windows-arm64`、`wasm` です。Mac のファイルは `.dylib` です。iOS のファイルにはライブラリ名を付けます（フレームワークのバイナリで、インストール名は `@rpath/<library>.framework/<library>`）。`release` は、ファイルを公開する GitHub のリリースです。`version` はライブラリのバージョンで、iOS のフレームワークに使われます。

プラグインの Go のコードは、マニフェストと同じディレクトリにあるファイルでマニフェストを埋め込み、`plugin.LibraryPath` が返すパスからライブラリを読み込みます。

```go
//go:embed asuka-plugin.json
var manifest []byte

func load() (uintptr, error) {
	path, err := plugin.LibraryPath(manifest, "asqlite")
	if err != nil {
		return 0, err
	}
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL) // Windows では syscall.LoadLibrary
}
```

ライブラリがエクスポートするシンボルは少なくし、ライブラリ自身のものだけにして、リンクしているものは含めないでください。ビルド元と同じものの別のコピーが、同じプロセスに読み込まれているかもしれないからです（GTK が読み込むシステムの SQLite など）。また、値ごとに関数を呼ぶのではなく、ひとまとまりの操作ごとに呼び出してください。Go から C への呼び出しは 1 回ごとに、小さな関数の処理そのものより大きなコストがかかります（sqlite プラグインは、ステートメントごとに 1 回だけ呼び出します）。

### ビルドと公開

プラグインは、ライブラリに合わせた独自のツールでライブラリをビルドします。たとえば `sdk/sqlite/internal/build` は、Xcode、Android NDK、Zig で SQLite をコンパイルします。このツールは、各ターゲットのファイルを、プラグインの `plugin.SourceHash` とともに `plugin.StoreBuild` で、マニフェストの隣の `build/<target>` に書き出します。このディレクトリはリポジトリに含めません。チェックアウトした環境では、プラグイン自身のテストと、同じ場所にあるアプリが、そこにあるビルドを使います。

```sh
go generate ./sdk/sqlite                         # すべてのターゲットをビルド
go run ./cmd/asuka plugin publish sdk/sqlite     # 公開
```

`asuka plugin publish` は、各ファイルを gzip で圧縮し、ハッシュを含む名前を付けます（`asqlite-darwin-1982f596dd7c-libasqlite.dylib.gz`）。リリースにまだないファイルを `gh` でアップロードし、リリースがなければ作成します。アップロードしたファイルはそれぞれダウンロードし直して確認し、マニフェストに記録します。コミットするのは、このマニフェストです。リリースにすでにあるファイルが置き換えられることはないので、過去のどのコミットのマニフェストも動作し続けます。プラグインのソースより古いビルドがあると、公開は中止されます。その場合は、先にビルドし直してください。

## プラットフォームのホスト向けのネイティブコード

プラットフォームの API を（iOS と macOS では Swift、Android では Kotlin、Web では JavaScript で）呼び出すパッケージや、プラットフォームのビューを表示するパッケージは、[ネイティブパッケージ](native.md)です。こちらはマニフェストを必要としません。Swift パッケージと Gradle モジュールをパッケージ自身が持ち、それらが依存関係を取り込みます。一つのパッケージが、プラグインとネイティブパッケージを兼ねることもできます。

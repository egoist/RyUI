# はじめに

Asuka を使うには、Go と、実行したいプラットフォームそれぞれの開発ツールが必要です。準備ができたら、コマンド一つでアプリを作成し、もう一つのコマンドで実行できます。実行中は、ファイルを保存するたびにアプリがリロードされます。

## 必要なもの

どのプラットフォームでも **Go 1.26** 以降が必要です。そのうえで、使いたいプラットフォームに応じて次のものを用意します。

- **iOS**：Xcode と、その iOS シミュレーターをインストールした Mac。
- **Android**：Mac または Linux に Android Studio をインストールし、その SDK Manager から NDK（SDK Tools → NDK）を入れます。さらに、Device Manager で作成したエミュレーターか、USB デバッグを有効にした実機を用意します。Asuka は SDK を標準のインストール先か `ANDROID_HOME` から見つけ、Java は Android Studio に同梱のものを使います。
- **macOS**：macOS 11 以降の Mac。アプリを実行するだけなら、ほかには何も要りません。リリースビルドと、独自の Swift コード（`macos/*.swift`）を持つアプリには Xcode のコマンドラインツール（`xcode-select --install`）を使います。これは Xcode にも含まれています。
- **Windows**：Windows 10 または 11 の PC（x64 または Arm）。ほかには何も要りません。Asuka は Go から直接 Win32 を呼び出します。`ui.WebView` と `ui.Video` は Microsoft Edge WebView2 Runtime を使います。これは Windows 11 には入っており、Windows 10 の PC にもたいてい入っています。
- **Linux**：GTK 4.14 以降と libadwaita 1.5 以降（Ubuntu 24.04、Fedora 40、Debian 13 以降には含まれています）。Wayland と X11 のどちらでも動きます。

  ```sh
  sudo apt install libgtk-4-1 libadwaita-1-0   # Debian、Ubuntu
  sudo dnf install gtk4 libadwaita             # Fedora
  sudo pacman -S gtk4 libadwaita               # Arch
  ```

  `ui.WebView` には WebKitGTK 6.0（`libwebkitgtk-6.0-4`）が、`ui.Video` とオーディオには GStreamer が必要です。必要なライブラリが見つからない場合、パッケージはどのライブラリが足りないかをエラーで知らせます。
- **Web**：ブラウザー。アプリは Go だけで WebAssembly としてビルドされます。

デスクトップのホストは cgo を使わずにプラットフォームの API を呼び出します。そのため `asuka build` を使えば、Windows や Linux のアプリはどのシステムからでも、Mac のアプリは Mac からビルドできます。

## CLI のインストール

```sh
go install github.com/egoist/asuka/cmd/asuka@latest
```

## アプリの作成

```sh
asuka new hello
cd hello
```

`hello` には次のファイルができます。

- `main.go`：アプリ本体です。アプリの UI を返す関数を `app.Run` に渡しています。
- `asuka.json`：アプリの名前、ID（bundle ID、application ID、デスクトップアプリの ID を兼ねます）、バージョンと、[ストアと各プラットフォーム](shipping.md)が求める情報です。
- `go.mod`。

## 実行

```sh
asuka dev                      # このコンピューター：Mac、PC、Linux
asuka dev --platform ios        # iOS シミュレーター
asuka dev --platform android    # Android のエミュレーターまたは実機
asuka dev --platform web        # ブラウザー
```

Go のファイルを保存すると、アプリはすぐにリロードされます。シグナルの値はそのまま残ります。アプリの出力（`fmt.Println` や panic）はターミナルに表示されます。

- **コンピューターで実行する場合**（`asuka dev`、または `--platform mac`、`windows`、`linux`）、アプリとそのホストは一つの Go プログラムになります。Asuka はそれをビルドしてウィンドウで実行し、保存するたびにビルドし直して実行し直します。
- **スマートフォンで実行する場合**、初回の実行で Asuka のプレイヤーアプリがビルドされ、インストールされます。プレイヤーアプリはアプリのビューを表示する役割を担い、Go のコードは手元のコンピューターで動いてプレイヤーアプリと通信します。そのため、保存しても再インストールせずにリロードできます。`--device` で使うシミュレーター（名前または UDID）や Android デバイス（adb のシリアル番号または AVD の名前）を選べます。`--no-hot` を付けると、リリースビルドと同じように Go のコードをアプリに組み込んでビルドします。`--once` を付けると、Go のコードを組み込んでビルドし、一度だけ起動します。
- **ブラウザーで実行する場合**、Asuka はアプリを `http://localhost:8790/` で配信し、ブラウザーで開きます。保存するたびにページがリロードされます。`--lan` を付けるとネットワーク上に配信するので、スマートフォンのブラウザーで試せます。詳しくは [Web](web.md) を参照してください。

iPhone の実機で動かすには、その iPhone 向けに署名したビルドを `asuka build --platform ios --distribution development` で作り、Xcode の「Devices」ウィンドウで `.ipa` をインストールします。

リロードで何が保たれるか、デバイス、ログ、よくある問題など、その先のことは[開発](development.md)で説明しています。

## アプリの変更

`asuka new` が作る `main.go` は、カウンターのアプリです。

```go
func App() ui.Node {
	count := signal.New(0)
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Set(count.Peek() + 1) }}),
	)
}
```

- **シグナル**はアプリの状態を保持します。`signal.New` で作り、`Get` で読み（読んだ側は値が変わると更新されます）、`Set` で書き込みます。`signal.Func` は、ほかのシグナルから計算される値です。
- **コンポーネント**は `ui.Node` を返す Go の関数です。コンポーネントは一度だけ実行されます。その後に変わるのは、シグナルを読んでいる部分だけです。
- **スタイル**は flexbox で、単位はポイントです。`Grow`、`Gap`、`Padding`、`Align`、`Justify` と、見た目（`Background`、`CornerRadius`、`Shadow` など）を指定します。スタイルを何も指定しないアプリは、そのプラットフォームのほかのアプリと同じ見た目になります。

同じコードが、そのままどのプラットフォームのアプリにもなります。デスクトップでは、`app.RunWindow` でメインウィンドウのタイトルとサイズを指定でき、`ui.Window` と `ui.MenuBar` でウィンドウやメニューバーを追加できます（[ナビゲーション](navigation.md)を参照）。

## アプリのファイル構成

- `assets/`：画像などのファイル（`ui.Image(ui.ImageProps{Source: ui.Val("photo.jpg")})` のように使います）と、`assets/fonts` に置くフォント。
- `locales/`：各言語でのアプリのメッセージ（[多言語対応](i18n.md)を参照）。
- 独自のネイティブパッケージ：モジュールやビューを宣言する Go のパッケージで、各プラットフォームがそれぞれのプロジェクト（Swift パッケージ、Gradle モジュール、JavaScript、デスクトップでは Go）で実装します。`asuka native new ./battery` で作成できます。詳しくは[ネイティブコード](native.md)を参照してください。

## 次のステップ

- [`examples`](../../examples)：[`gallery`](../../examples/gallery) ではすべてのコンポーネントを見られます。[`notes`](../../examples/notes) は SQLite にメモを保存するアプリで、[`navigation`](../../examples/navigation) にはスタック、タブ、シートの例があります。
- [配布](shipping.md)：App Store、Google Play、Mac、Windows、Linux、Web 向けのビルド。

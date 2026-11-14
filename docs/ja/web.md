# Web

同じアプリがブラウザーでも動きます。Go のコードは WebAssembly にコンパイルされ、ビューはページの DOM になります。Web は iOS、Android、Mac、Windows、Linux と並ぶ Asuka のプラットフォームの一つで、同じ Go のコードから動きます。コアが作る各ビューは要素になり、コアがレイアウトしたフレームの位置に配置されます。これは、ネイティブの各ホストがウィジェットを配置するのと同じやり方です。そのため、レイアウトは Asuka 自身のもの（Go で実装した flexbox と grid）で、どのプラットフォームでも同じになります。末端のビューのサイズは、ネイティブのホストが自分のウィジェットを測るのと同じく、実際の要素で測ります。コントロールはブラウザー自身の要素（`<button>`、`<input>`、`<select>`、`<progress>`、`<dialog>`）なので、キーボード操作、フォーカス、自動入力、スペルチェック、スクリーンリーダーへの対応も最初から備わっています。

```sh
asuka dev --platform web examples/counter       # サーバーを起動してブラウザーで開き、保存するとリロード
asuka build --platform web examples/counter     # 静的サイトを examples/counter/dist/web に出力
```

以下では、Web で何ができるかを説明します。ブラウザーではスマートフォンと同じことができない場合は、代わりにどうなるかも説明します。

## asuka dev --platform web

`asuka dev --platform web` は、アプリをブラウザー向けにビルドし（`GOOS=js GOARCH=wasm`）、`http://localhost:8790/`（または空いているポート）で配信して、ブラウザーで開きます。保存するたびに再ビルドされ、examples のアプリなら 1 秒もかかりません。ページは新しいビルドを読み込みますが、ほかのプラットフォームのリロードと同じく、アプリのシグナルの値はそのまま残ります。アプリの出力（`fmt.Println`、`log`、panic）とページのエラーはターミナルに表示されます。ビルドに失敗すると、次のビルドが成功するまで、そのエラーがページの上に表示されます。

`asuka dev` 共通のフラグ（`--lang`、`--dir`、`--no-vet`、`-v`）のほかに、次のフラグがあります。

- `--port`：サーバーのポート。
- `--lan`：ローカルネットワークにも配信し、そのアドレスを表示します。スマートフォンのブラウザーでアプリを開けます。
- `--no-open`：ブラウザーを開きません。
- `--once`：ビルドして配信するだけで、ファイルの変更は監視しません。

## asuka build --platform web

`asuka build --platform web` は、`dist/web` に静的サイトを作ります。中身は次のとおりです。

- `index.html` と `manifest.webmanifest`（Web アプリマニフェスト。これがあると、ブラウザーがアプリのインストールを提案します。アイコンは `asuka.json` の `icon` から作られ、ファビコンと iOS のホーム画面アイコンも含まれます）。
- アプリ本体の `app.<digest>.wasm`。シンボルを取り除き（stripped）、`-trimpath` 付きでビルドされます。
- `asuka/<digest>/` 内の Web ホストのファイル（`asuka/sdk/sqlite` をインポートするアプリでは、SQLite の WebAssembly ビルドも含まれます。約 0.9 MB、gzip 圧縮で 0.4 MB です）と、`web/<digest>/` 内のアプリ自身のスクリプト（`web/*.js`）。
- アプリの service worker である `sw.<digest>.js`。`index.html` と同じ場所に置かれ（スコープはサイトのフォルダー）、アプリのページが一つも開いていない間も、アプリの通知とプッシュ通知を表示します（[通知と Web Push](#通知と-web-push)）。
- アプリの `assets/`。
- 各スクリプトと wasm の gzip 圧縮版（`.gz`）。圧縮済みのファイルを配信できるサーバー向けです。
- `_headers`。Cloudflare と Netlify がこれを読みます。`index.html` とマニフェストは `no-cache`、ダイジェストを名前に含むファイルは無期限にキャッシュされるよう指定しています。

このフォルダーは、どんな Web サーバーや CDN からでも配信できます。内容のダイジェストを名前に含むファイルは、内容が変わると名前も変わるので、無期限にキャッシュできます。再取得が必要なのは `index.html` だけです。`.wasm` は `application/wasm` として配信してください。カウンターアプリの WebAssembly は約 6.5 MB、gzip 圧縮で 1.7 MB です（brotli なら 1.3 MB で、この圧縮は CDN に任せられます）。これに、アプリ自身のコードとインポートしたパッケージの分が加わります。

## ページから得られる環境

Web アプリから見える環境（`ui.UseEnvironment`）は、ブラウザーのものです。

- `Platform` は `ui.PlatformWeb` です。
- サイズはページ（アプリを要素の中で動かしている場合はその要素）のサイズで、ウィンドウに合わせて変わります。サイズクラスは Material のもので、幅から決まります。
- セーフエリアはページのもの（`env(safe-area-inset-*)`。ページが `viewport-fit=cover` のときの、スマートフォンのノッチやホームインジケーターの分）です。キーボードは、スマートフォンのキーボードがページを覆っている量です。
- ダークモードは `prefers-color-scheme`（またはアプリの `ui.SetColorScheme`）、視差効果を減らす（Reduce Motion）は `prefers-reduced-motion`、文字サイズはブラウザーのデフォルトのフォントサイズ（`FontScale`。テキストはこれに従います）、スケールはディスプレイのピクセル比から決まります。
- ロケール、言語、タイムゾーン、週の始まりの曜日、時刻の表記（12 時間制か 24 時間制か）は、ブラウザーのもの（`navigator.languages`、`Intl`）です。

## ビューの対応

- テキストはブラウザーのテキストで、システムのフォント（`system-ui`。Apple のシステムでは San Francisco、Windows では Segoe UI、Android では Roboto）を使い、Web 向けのタイプランプ（本文は 16）で表示されます。`assets/fonts` に置いたアプリのフォントは、アプリが指定した名前（`Font.Family`）で読み込まれます。
- `Stack` は、一番上の画面を独自のヘッダーバーの下に表示します。ヘッダーバーには、戻るボタン、画面のタイトル、バーのボタン、検索フィールドが並びます。プッシュとポップはスライドで切り替わります。戻る操作にはブラウザーの機能も使えます。アプリ内に戻る先がある間は、ブラウザーの戻るボタン（とスマートフォンの戻るジェスチャー）でアプリ内を戻ります。Escape、Alt+←（Mac では ⌘[）、タッチスクリーンでの先頭側の端からのスワイプも同じです。ほかのプラットフォームと同じく、これらは `ui.OnBack` で受け取れます。
- `Tabs` は画面下部のタブバーになります。`SplitView` は詳細の横に並ぶサイドバーになり、ウィンドウの幅が狭いときは、詳細のヘッダーから開くドロワーになります。
- `Sheet` はモーダルの `<dialog>` です。幅の狭いウィンドウではボトムシート（medium は高さの半分で、グラバーをドラッグすると引き上げたり閉じたりできます）、幅の広いウィンドウでは中央に置かれたカードになります。Escape、外側のクリック、下方向のスワイプで閉じます。`Alert` もモーダルの `<dialog>` で、アクションシートのボタンはリストになります。`Popover` とメニューは、指し示す対象の隣に表示されるポップオーバーです。`ui.Window` は、ユーザーが移動やサイズ変更をできる、ページ内のパネルになります。
- `List` は、Mac、Windows、Linux と同じくホストが仮想化します。表示中の行（とその前後半画面分）だけがバインドされます。セクションのヘッダーは上部に固定され、チャットのリストは末尾に留まります。行のスワイプアクションは、その行のコンテキストメニュー（右クリックまたは長押し）になります。
- `Image` は `<img>`（メインスレッドの外でデコードされ、ブラウザーがキャッシュします）、`Video` は `<video>`、`WebView` は `<iframe>`（[Web ビュー](#web-ビュー)を参照）、`Canvas` は 2D の canvas、`Shape` は SVG のパス、`Shader` は WebGL 2（Kage のプログラムを GLSL ES 3.00 に変換します）になります。`Symbol` は、SF Symbol の名前に対して Android ホストが描画するのと同じ Material Symbol になります。
- `ui.Animate` は、フレームと見た目のプロパティを Web Animations API でアニメーションします（スプリングは CSS の `linear()` カーブで表現します）。出現・退場のトランジションと共有要素（`Style.Match`）も、iOS と同じように動作します。
- タップやクリック、長押し（と右クリック）、ホバー、押下状態、パン、スワイプ、ピンチ（2 本指、またはトラックパッドのピンチ）、回転は、マウス、ペン、タッチのどれでも動作します。ドラッグ＆ドロップはブラウザーのもので、ほかのアプリからのファイルにも対応します。
- `MenuBar` の項目のショートカットは、ページ内で動作します（Mac では ⌘、それ以外では Ctrl）。ただし、ページには独自のメニューバーを表示しません。デスクトップのアプリでは、アプリのメニューが Mac ではメニューバーに、Windows ではウィンドウのメニューバーに、Linux ではヘッダーバーのメインメニューに表示されます。

## アプリのファイル

`asuka/sdk/store`、クラッシュの記録、アプリが `store.Dir` に書き込んだファイルは保持されます。ページは Go にメモリ上のファイルシステムを提供します。データディレクトリのファイルは、アプリの起動前にブラウザーのストレージ（IndexedDB）から読み込まれ、変更の少し後と、ページが非表示になったり閉じられたりしたときに書き戻されます（大きなファイルは 64 KiB のチャンク単位で、変更のあったチャンクだけが書き込まれます）。アプリのタブどうしは、これらのファイルを共有します。あるタブの書き込みはほかのタブに届き、受け取ったタブはファイルを読み直します（ただし、あるタブが変更してまだ書き戻していないファイルは、書き戻すまでそのタブの内容のままです。つまり、最後に書き込んだタブの内容が残ります）。ユーザーがドロップ、ペースト、選択したものは、iOS や Android と同じくアプリのキャッシュ（`/tmp`）にコピーされ、パスとしてアプリに渡されます。

ブラウザーは、ユーザーが消去するまでオリジンのストレージを保持します。ただし、デバイスの空き容量がなくなると、ブラウザーがそのオリジンのストレージを削除することがあります（Safari は、ホーム画面に追加されていないサイトについては、7 日間アクセスがない場合にも削除します）。

## SQLite

`asuka/sdk/sqlite` は、ライブクエリも含めて、ほかのプラットフォームと同じように動作します。同じ SQLite と同じ Asuka のライブラリを、WebAssembly にビルドしたものです（`asqlite.wasm`。`asuka dev --platform web` と `asuka build --platform web` が、`asuka/sdk/sqlite` をインポートするアプリに同梱します）。SQLite はページのワーカーで動き、アプリのデータディレクトリにあるデータベースを、ブラウザーの Origin Private File System に保存します。データベース全体がメモリに読み込まれることはなく、コミットが戻った時点でディスクに書き込まれています。各ステートメントはワーカーへのメッセージとその応答で、約 30 マイクロ秒かかります。1000 件の INSERT を含むトランザクションは約 30 ms、10,000 行のクエリは 3 ms です。

ほかのプラットフォームとの違いは次のとおりです。

- `Exec`、`Tx`、`Query`、`QueryOne`、`Migrate` はワーカーの応答を待ちますが、ページのメインスレッドでは待つことができません。そのため、メインスレッドで呼ぶと、その旨のエラーですぐに失敗します。どのプラットフォームでもそうするように、`ExecAsync`、`TxAsync`、ライブクエリを使うか、ゴルーチンから呼んでください。`app.Run` より前の `main` では待ってもかまいません。
- ジャーナルは write-ahead logging（接続間で共有するメモリが必要）ではなく、ロールバックジャーナルです。また、データベースへの接続は一つだけです。そのため、読み取りは進行中の書き込みと並行して走るのではなく、書き込みの完了を待ちます。
- アプリの複数のタブで、一つのデータベースを使えます。タブはデータベースを使っている間それを保持し、ほかのタブが求めると手放します（トランザクションの合間に手放し、トランザクションの途中では手放しません）。ほかのタブが保持しているデータベースに対するステートメントは、SQLite がほかのプロセスのロックを待つのと同じく、ビジータイムアウトの 5 秒間、そのタブが手放すまで再実行されます。あるタブのライブクエリには、ほかのタブの書き込みは伝わりません（ネイティブで、ほかのプロセスの書き込みが伝わらないのと同じです。`db.Changed()` で再実行できます）。
- データベースはワーカーのファイルで、ページのファイルシステムにあるわけではありません。そのため、Go の `os` からは見えません。`db.Serialize()` でデータベースのコピーを取得できます（どこにでも書き出せるバックアップとして、あるいは `Restore` に渡すために使えます）。データディレクトリ以外の場所（`/tmp`）にあるデータベースは、ページが開いている間しか残りません。
- Origin Private File System がない場合（`https` や `localhost` から配信されていないページ。たとえば `asuka dev --lan` のページや、一部のブラウザーのプライベートウィンドウ）、ワーカーはデータベースを IndexedDB に保存し、使っている間はメモリ上に置きます。この場合、コミットが永続化されるのは、コミットが戻った少し後です。セキュアコンテキストでないページでは Web Locks も使えないため、二つのタブが一つのデータベースに書き込むと、互いの書き込みが失われることがあります（コンソールにその旨が表示されます）。
- 以前のバージョンの Asuka がアプリのファイルの中（IndexedDB）に保存していたデータベースは、アプリが初めて開いたときにワーカーへ移されます。

アプリを埋め込む独自のページ（後述）では、`start` に `libs` を渡します。`libs` は、アプリのプラグインのファイルの URL を、ファイル名をキーにして並べたものです。SQLite の場合は、ビルドに含まれるファイルを指す `{"asqlite.wasm": "asuka/<digest>/lib/asqlite.wasm"}` になります。ワーカーのスクリプトはホストのもの（`asuka.js` と同じ場所にある `sqlite-worker.js`）です。

## ネイティブモジュール

Asuka のパッケージは、ホストのネイティブモジュールを呼び出します（`ui.Call`）。ブラウザーでは次のようになります。

- `asuka/sdk/secure`：値は、ブラウザーが保持して外に出さない鍵（エクスポートできない WebCrypto の鍵）で暗号化（AES-GCM）され、IndexedDB に保存されます。キーチェーンではありません。アプリのオリジンで動くスクリプトは鍵そのものを読めませんが、その鍵での復号を依頼することはできます。`https`（または `localhost`）が必要です。
- `device`：`GetInfo` はユーザーエージェントから取得します（Chromium のクライアントヒントからは、システムのバージョンと Android スマートフォンの機種がわかります）。`Clipboard`、クリップボードの画像（PNG。キャッシュに保存されます）、`OnClipboardChange`（アプリ自身のコピーと、ユーザーがページにクリップボードの読み取りを許可している場合はほかのアプリのコピー）に対応します。`KeepAwake`（Screen Wake Lock。ページが再び表示されると取得し直します）、`Browse`（新しいタブ）、`PickPhotos` と `PickFiles`（ブラウザーのファイル選択ダイアログ。選んだファイルはキャッシュにコピーされます）にも対応します。
- `ShareFiles`：ファイルを渡して共有シートを開きます（`navigator.share`）。共有シートがなければ、各ファイルをダウンロードします。`asuka/sdk/preview`：ファイルを新しいタブで開きます（画像、動画、音声、テキスト、PDF、HTML）。それ以外の種類は `ErrNoViewer` になります。
- `asuka/sdk/print`：HTML、ファイル（PDF はブラウザーのビューアーで）、アプリ自身の Web ビューを、ブラウザーの印刷ダイアログで印刷します。結果は常に `Unknown` です（ブラウザーが結果を知らせないためです）。`PDF` は Web では使えません。ブラウザーが PDF を作れるのは、印刷ダイアログ（「PDF として保存」）を通したときだけです。
- `asuka/sdk/compose`：`mailto:` と `sms:` のリンクを使います。添付ファイルがあれば共有シートを使います。
- `asuka/sdk/notify` と `asuka/sdk/push`：アプリの service worker を通じて、Notification API と Web Push を使います。[通知と Web Push](#通知と-web-push)を参照してください。
- `asuka/sdk/network`：オンラインかどうかは `navigator.onLine` から取得します。Wi-Fi、モバイル回線、Ethernet の区別は、ブラウザーが教えてくれる場合（Android の Chrome）にわかります。モバイル回線は従量制（metered）、データセーバーは制限付き（constrained）として扱います。
- `asuka/sdk/location`：Geolocation API を使います。`PlaceName` は Web では使えません（ブラウザーにはジオコーダーがないためです）。
- `asuka/sdk/sensors`：Generic Sensor API（Chrome）か、デバイスの motion イベントと orientation イベント（Safari はタップ時に許可を求めます）を使い、Android と同じ軸と単位で値を返します。気圧計と歩数計はありません。
- `asuka/sdk/speech`：`speechSynthesis` と、ブラウザーの音声を使います。
- `asuka/sdk/audio`：プレイヤーごとに audio 要素を一つ使います（ブラウザーの自動再生のルールが適用されるので、タップから再生してください）。`asuka/sdk/audio/record`：`MediaRecorder` を使います。ブラウザーが対応していれば MPEG-4 の AAC で、そうでなければ WebM の Opus で録音します。
- `asuka/sdk/camera`：`Take` は、スマートフォンならカメラを開き、それ以外ではライブのカメラ映像とシャッターボタンのあるダイアログを開きます。`camera.View` は `<video>` に映したカメラ映像です（`Capture`、`record` に対応し、ブラウザーに `BarcodeDetector` があればコードのスキャンにも対応します）。`asuka/sdk/scan` と `asuka/sdk/vision` のコード読み取りは `BarcodeDetector` を使います。顔とテキストの認識は、ブラウザーが対応している場合にだけ使えます。
- `asuka/sdk/imageedit`：canvas を使います（JPEG か PNG。リモートの画像には CORS が必要です）。
- `asuka/sdk/maps`：Windows と同じく、OpenStreetMap のタイルからホストが描画する地図です。ピンとそのコールアウト、ドラッグ、ホイール、ピンチ、ダブルクリック、キーボードによる移動、OpenStreetMap が求める帰属表示に対応します。Satellite と Hybrid は道路地図を表示します（無料の航空写真がないためです）。`ShowsUser` は、ページに位置情報の許可があればユーザーの現在地を表示します（許可を求めるのは `asuka/sdk/location` で、地図が求めることはありません）。タイルは、利用ポリシーが Web ページに求めるとおりに `tile.openstreetmap.org` から取得します（画面に表示する分だけを、ブラウザーのキャッシュを使い、Referer 付きで取得します）。大量に使う場合は、`start` に `mapTiles`（`{z}`、`{x}`、`{y}` を含む別のサーバーの URL）を渡してください（[埋め込み](#埋め込み)）。
- `asuka/sdk/crash`：ブラウザーでは、Go のランタイムがクラッシュ時の出力をファイルに書けません。そこでホストがその出力を保持しておき、プログラムが panic や致命的なエラーで終了すると、アプリのファイルにあるその実行の記録に書き込みます。次回の起動時に、それが報告されます。キャッチされなかった JavaScript のエラー（ホストのものか、アプリの `web/*.js` のもの）は、その場で `Exception` のレポートになりますが、致命的なものとしては扱われません（ページは動き続けます）。ページは `localStorage` に自分の実行が生きていることを記録するので、アプリのほかの開いているページがその実行に手を出すことはありません。
- `asuka/sdk/biometric`：WebAuthn を通じてプラットフォームの認証器（Touch ID、Face ID、Windows Hello、Android の画面ロック）を使い、ユーザー検証を必須にします。最初の `Authenticate` で、アプリの名前を付けたサイトのパスキーが作られます。プロンプトはブラウザーのものなので、理由やキャンセルのテキストは表示されません。
- `asuka/sdk/webauth`：ポップアップを使い、アプリと同じオリジンのページに戻ってきた時点で完了します（カスタムスキームではページを開けないためです）。
- `asuka/sdk/screen`：`Lock` に対応します（全画面表示のとき、またはインストールしたアプリでのみ）。バーを隠すと全画面表示になります。
- `asuka/sdk/library`：`Save` はファイルをダウンロードします。`asuka/sdk/contacts`：`Pick` は、ブラウザーに Contact Picker がある場合（Android の Chrome）に使えます。`Add` は vCard を、`asuka/sdk/calendar` の `Add` は `.ics` ファイルをダウンロードします。
- `asuka/sdk/tracking` は、ブラウザーが Global Privacy Control か Do Not Track を送っていれば Denied を、そうでなければ Allowed を、プロンプトを出さずに返します。
- Web では使えず、その旨のエラー（またはパッケージ独自の「not available」）を返すもの：`asuka/sdk/review`、`asuka/sdk/integrity`、`asuka/sdk/agerange`、`asuka/sdk/appleauth`、`asuka/sdk/periodic`、`asuka/sdk/widget`、`asuka/sdk/liveactivity`、`asuka/sdk/photos`（Denied を返します。写真の選択は `device.PickPhotos` でできます）、連絡先とカレンダーの読み取り、`asuka/sdk/transcribe` のファイル、`asuka/sdk/screen` の `PreventCapture` と `OnScreenshot`。

## 通知と Web Push

Web アプリにはどれも service worker があります。service worker は CLI が書き出し（`asuka dev` では `sw.js`、ビルドでは `sw.<digest>.js`）、ページが起動時にアプリのフォルダーをスコープとして登録します。fetch ハンドラーは持たないので、ページは service worker がない場合と同じように読み込まれます。service worker は、アプリのページが開いているかどうかにかかわらず、アプリの通知とプッシュ通知を表示し、そのクリックに応答します。ブラウザーが service worker を動かすのは、HTTPS か `localhost` から配信されたページだけです（`asuka dev` のアドレスは対象ですが、`--lan` のアドレスは対象外です）。

### asuka/sdk/notify

- `Request` は、ブラウザーのプロンプトで許可を求めます。ブラウザーはタップやクリックのときにしか許可を求めないので、その処理の中から呼び出してください。
- `Show` は service worker を通じて通知を表示するので、ページを閉じた後もボタンは残り、クリックも機能します（service worker のないページ、つまり service worker を登録しない埋め込みページでは、ページ自身の通知がボタンなしで表示され、そのクリックはそのページにしか届きません）。
- 通知の ID は、その通知のタグになります。同じ ID の通知は前の通知を置き換え、`Silent` でなければ改めてユーザーに知らせます。`Subtitle` は本文の 1 行目、`Image` は画像（`https` の URL かアセット）、メッセージの送信者の画像（`From`）はアイコンになります。`Badge` と `SetBadge` は、インストールしたアプリのバッジを設定します（`navigator.setAppBadge`）。
- カテゴリーのアクション（`SetCategories`）は、通知のボタンになります（Chrome と Edge は 2 個まで表示し、Firefox と Safari は表示しません）。`Reply` アクションは、ブラウザーがインライン返信に対応していればテキストフィールドになります（Android とデスクトップの Chrome。入力したテキストは `Response.Reply` に入ります。それ以外のブラウザーではボタンになります）。カテゴリーは service worker のために保存されるので、カテゴリーを指定したプッシュ通知にもボタンが付きます。
- 通知をタップすると、アプリのページにフォーカスが移り、通知の `URL` に移動し（`ui.OnOpenURL`）、`OnAction` が呼ばれます（`Action ""`）。ボタンを押すと、そのボタンの ID を伴って `OnAction` が呼ばれます。呼ばれるのは前面にあるページで、なければアプリのいずれかのページです。ページが一つも開いていなければ、service worker がページを開きます（ブラウザーはアプリのコードをページの中でしか動かさないためです）。アプリは、タップで起動したアプリと同じく、起動時にレスポンスを受け取ります。そのため、`OnAction` はルートコンポーネントで設定してください。`Open` のないアクションは、開いているページを前面に出しません。ただし、ページが一つも開いていなければページを開き、前面に表示します。`Response.Defer` には、完了まで生かしておく対象がありません。ページはそのまま動き続けるためです。
- `Present` は、ページが前面にあるときに届いた通知（プッシュ通知も含みます）をどう表示するかをアプリに尋ねます。`Banner`、`Quiet`（音なし）、`Hide` から選びます。
- `Showing`、`Remove`、`RemoveThread`、`RemoveAll`、`Cancel` は、service worker の通知に対して働きます。以前のページが出した通知やプッシュ通知も対象です。
- Android のチャネルは、ここには存在しません。iOS と同じく、指定しても受け付けたうえで無視されます。

#### スケジュールした通知

アプリのページが一つも開いていない間、ブラウザーは決まった時刻に通知を表示できません。そのための API がなく（これを実現していた Chrome の Notification Triggers は 2021 年に開発が中止されました）、service worker はイベントで起こされたときにしか動かず、タイマーでは動かないためです。そこで `Schedule` は、ブラウザーでできる範囲のことを行います。

- スケジュールした内容は、ページではなくアプリのストア（アプリのページと service worker が共有する Cache Storage）に保存されます。
- アプリのページがどれか一つでも開いている間は、指定した時刻に表示されます。各ページは次の通知のためのタイマーを持ち（通知をスケジュールしたりキャンセルしたりしたページが、ほかのページに知らせます）、最初に期限に気づいたページが一度だけ表示します（Web Lock を使います）。バックグラウンドのページでは遅れることがあります。ブラウザーは、非表示のページのタイマーを 1 分に 1 回程度に間引くためです。遅くとも、ページが再び前面に来たときには表示されます。
- ページが一つも開いていない間に期限が来た通知は、次にページが開いたときに、予定時刻を過ぎた状態ですぐに表示されます。
- それより前にも、ベストエフォートとして、ページが開いていないときに何かが service worker を起こすたびに、service worker が期限の来た通知を表示します。起こすものとは、プッシュ通知、クリック、そして Chrome の Periodic Background Sync です。Periodic Background Sync は、何かがスケジュールされている間、ページが要求します。Chrome がこれを許可するのはインストールされたアプリ（ホーム画面に追加したアプリや、アドレスバーからインストールしたアプリ）だけで、実行されるのは多くても 12 時間に 1 回程度、それもブラウザーの判断次第です（ユーザーがアプリをどれだけ使っているか、バッテリー、ネットワーク）。ほかのブラウザーにはこの機能がありません。決してこれを当てにしないでください。

アプリを閉じていても必ず指定の時刻に表示したい通知は、その時刻にサーバーからプッシュ通知を送ってください。

### asuka/sdk/push：Web Push

Web Push には VAPID の鍵ペアが必要です。アプリのサーバーは秘密鍵でプッシュに署名し、ブラウザーは公開鍵で購読します。公開鍵は `asuka.json` に書きます。

```json
{ "web": { "vapidPublicKey": "BKpYsAT0ADj2pHQQ..." } }
```

鍵ペアは一度だけ作り、秘密鍵はサーバーに置いておきます。鍵ペアを作り直すと、購読もすべてやり直しになります。鍵ペアは `webpush.GenerateKeys`（後述）でも、`npx web-push generate-vapid-keys` でも作れます。

`push.Register()` は、通知の許可を求め（ブラウザーはタップのときにしか尋ねないので、タップから呼び出します）、その鍵でアプリの service worker を購読させ（別の鍵で作った購読は置き換えられます）、次の値を返します。

- `Token`：購読の JSON です。形式は `PushSubscription.toJSON()` が返すものと同じで、`{"endpoint": "https://...", "expirationTime": null, "keys": {"p256dh": "...", "auth": "..."}}` のようになります。これをそのままサーバーに送ってください。Web Push のライブラリはこの形式をそのまま受け取れます。ブラウザーが購読を変えることがあるので（`pushsubscriptionchange`）、ほかのプラットフォームと同じく起動のたびに登録し、サーバーにあるものと違っていれば送ってください。
- `Service`：`"webpush"`。
- `Allowed`：true。許可がなければ `Register` は失敗します。ブラウザーは、プッシュ通知を表示するサイトにしか購読を許さないためです（`userVisibleOnly`）。

`web.vapidPublicKey` がないとき、service worker がないとき（セキュアでないページ）、ブラウザーが Web Push に対応していないときは、理由を示して失敗します。iOS と iPadOS の Safari が Web Push に対応するのは、ホーム画面に追加したアプリだけです（16.4 以降）。

プッシュ通知は、ページが開いているかどうかにかかわらず通知として表示されます。アプリのページが開いていれば、そのページが先に受け取り（`push.OnReceive`。開いているすべてのページが受け取ります）、内容を書き換えたり（`push.Rewrite`。前面にあるページで、約 10 秒以内に実行されます。ページが開いていなければ、届いたままの内容で表示されます）、表示方法を決めたり（`notify.Present`）できます。通知をタップすると、ローカル通知と同じく、`push.OnOpen`、`notify.OnAction`（`Remote` が設定されます）、そしてデータに `"url"` があれば `ui.OnOpenURL` が呼ばれます。

#### サーバーが送る内容

ペイロードは JSON です。どの Web Push ライブラリでもそうするように、購読に合わせて暗号化し（RFC 8291 の `aes128gcm`）、VAPID トークン（RFC 8292）を付けて送ります。service worker は次のキーを読みます。ほかのプラットフォームの `push.Message` と同じキーです。

| キー | | 
| --- | --- |
| `title`、`body`、`subtitle` | 表示される内容（サブタイトルは本文の 1 行目になります） |
| `data` | アプリ独自のキーで、`push.Message` の `Data` になります（文字列でない値は JSON として渡されます）。`"url"` は、タップしたときにアプリに渡して開く URL です（`ui.OnOpenURL`） |
| `id` | 通知のタグ。同じ `id` のプッシュ通知は、前の通知を置き換えます |
| `category` | `notify.SetCategories` で登録したカテゴリーの一つ。そのアクションが通知のボタンになります |
| `thread`、`summary` | `push.Message` の同名のフィールド。`notify.RemoveThread` とアプリのために使います |
| `image` | 画像。`https` の URL か、アプリのアセットの一つ |
| `badge` | インストールしたアプリのバッジ |
| `silent` | 音を鳴らさずに表示します |

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

`title` も `body` もないプッシュは、データメッセージです。開いているページが受け取り（`OnReceive`）、何も表示されません。データメッセージは、アプリが開いている間だけ送ってください。ブラウザーはプッシュのたびに通知が表示されることを期待しています。前面にページがないときに何も表示されないと、Chrome は独自の通知（「This site has been updated in the background」）を表示し、Safari はそれが何度か続くと購読を終了します。JSON でないペイロードは、本文として表示されます。ペイロードは最大 3993 バイトです。

Go からは、`github.com/egoist/asuka/sdk/push/webpush` で暗号化、署名、送信ができます。使うのは標準ライブラリだけです。

```go
import "github.com/egoist/asuka/sdk/push/webpush"

var sender = &webpush.Sender{
	Keys:    webpush.Keys{Public: os.Getenv("VAPID_PUBLIC_KEY"), Private: os.Getenv("VAPID_PRIVATE_KEY")},
	Subject: "mailto:ops@example.com", // プッシュサービスがあなたに連絡するための宛先
}

func notifyChat(ctx context.Context, token string) error {
	sub, err := webpush.ParseSubscription(token) // アプリの push.Register が返した Token
	if err != nil {
		return err
	}
	err = sender.Send(ctx, sub, webpush.Notification{
		ID: "chat-7", Title: "Ada", Body: "Lunch?", Category: "message",
		Data: map[string]string{"url": "myapp://chat/7"},
	}, &webpush.Options{Urgency: "high"})
	if errors.Is(err, webpush.ErrGone) {
		return forgetSubscription(token) // 購読が終了した（ユーザーかブラウザーによって）
	}
	return err
}
```

`Options` では、プッシュの `TTL`（オフラインのブラウザーのために、プッシュサービスがプッシュを保持しておく期間。デフォルトは 4 週間）、`Urgency`、`Topic`（同じトピックの後続のプッシュが、まだ配信待ちのプッシュを置き換えます）を設定します。リクエストを自分で送るサーバー向けには、処理を二つに分けた `webpush.Keys.Authorization` と `webpush.Encrypt` があります。

Node からは、[`web-push`](https://github.com/web-push-libs/web-push) ライブラリを使います。

```js
import webpush from "web-push";

webpush.setVapidDetails("mailto:ops@example.com", process.env.VAPID_PUBLIC_KEY, process.env.VAPID_PRIVATE_KEY);

// token：アプリの push.Register が返した Token
await webpush.sendNotification(JSON.parse(token), JSON.stringify({
  title: "Ada", body: "Lunch?", category: "message", data: { url: "myapp://chat/7" },
}), { urgency: "high" });
// 404 か 410（err.statusCode）なら購読は終了しているので、破棄する。
```

## Web ビュー

`WebView` は `<iframe>` で、ページを読み込むたびに新しく作られます。そのため独自の履歴を持ち、タブの履歴には何も追加しません。

アプリ自身のサイトのページ（アプリのアセット、HTML、アプリが書き出したファイル、ページと同じオリジンの任意の URL）では、すべての機能が使えます。ホストはページを本来の URL でフレームに読み込み、ページのスクリプトより先に `window.asuka.postMessage` と `Preload` を用意します。そのほかに使えるのは次の機能です。`EvalJS` と `EvalJSResult`（Promise は await されます）。読み込み、失敗、状態のイベント（URL、タイトル、`pushState`、進捗、戻る・進む）。戻る、進む、再読み込み、停止。リンク、フォーム、`location` の変更による読み込みは、Web ビューを通じて行われます。`Block`（マークアップ、`fetch`、XHR、`sendBeacon`、WebSocket、EventSource、要素の `src` と `href`。ブロックした時点ですでに始まっていたリクエストは、送信されてしまうことがあります）。`OnRequest`（Resource Timing）。`OnNewWindow`（`window.open`、`target=_blank`）。`OnDownload`（添付ファイル、フレームで表示できない種類のファイル、`<a download>`）。`WebCookies`（`document.cookie`。`HttpOnly` の Cookie は対象外です）。HTML の相対リンクは、アプリのアセットを基準に解決されます。

ほかのサイトのページは、読み込むことしかできません。ブラウザーがサイトどうしを分離しているためです。読み込みが終わると `Navigated`、サイトに接続できないと `Failed` が届きます。サイトがフレーム内での表示を拒否している場合（`X-Frame-Options`、`frame-ancestors`）、Web ビューはそれを検知できず、ブラウザー独自のエラーページが表示されます。ページのリンクの移動先を追ったり、タイトルを読んだりすることはできません。スクリプトの実行、`Block` や `Preload`、リクエストの報告、Cookie の読み取り、ユーザーエージェントの設定もできません（スクリプトと Cookie はエラーを返し、それ以外は一度だけ警告を出します）。フレームはトップレベルナビゲーションを許可しない sandbox になっているので、ページがアプリを別のページに置き換えてしまうことはありません。

ほかのサイトのページでも、協力的なページであれば、`postMessage` を通じて Web ビューとやり取りできます（Web ビューは自分のフレームからのメッセージだけを受け付け、準備ができたと伝えてきたオリジンにだけスクリプトを送ります）。

```js
// ページ側：アプリへのメッセージ（OnMessage）、URL とタイトル、EvalJS。
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

`asuka/sdk/js` は、独自のオリジンを持つ非表示の sandbox フレームでスクリプトを実行します。そのため、スクリプトはアプリのページやストレージにアクセスできません。

## ネイティブコード

[ネイティブパッケージ](native.md)の JavaScript は、その `web/` ディレクトリに置きます。生成される `<package>.asuka.js` と、パッケージ独自の `<package>.js` です。`<package>.js` は、パッケージのモジュールとビューを作る関数をエクスポートします。

```js
// segmented/web/segmented.js
export function makeSegmented(events) {
  const el = document.createElement("div");
  let items = [], selected = 0;
  const render = () => { /* 項目ごとにボタンを一つ作り、クリックで events.changed(i) を呼ぶ */ };
  return {
    el, // ビュー。コアが与えるフレームのサイズになる
    setItems(v) { items = v; render(); },
    setSelected(v) { selected = v; render(); },
    measure: (c) => [items.length * 72, 32], // c の範囲内でのサイズ（c は {width, height, availW, availH}。制約がなければ NaN）
  };
}
```

各パッケージの `web/` は `asuka/<digest>/packages/<package>/` に配置され、ページはアプリの起動前にアプリのパッケージをインポートします。パッケージのほかのファイル（ヘルパーやアセット）も同じ場所に配置されるので、パッケージはそれらを `import.meta.url` からの相対パスでインポートしたり、fetch したりできます。これらのファイルが変わると、`asuka dev` がページをリロードします。

`examples/media/segmented` にはネイティブのビューがあり、その JavaScript は `examples/media/segmented/web/segmented.js` にあります。

## 埋め込み

`asuka build --platform web` が作るページでは、アプリはページ全体に表示されます。独自のページの要素の中でアプリを動かすには、ホストのローダーを読み込み、その要素の中でアプリを起動します。するとアプリは、その要素のサイズに従います。

```html
<link rel="stylesheet" href="asuka/<digest>/style.css">
<div id="app" style="width: 390px; height: 700px"></div>
<script type="module">
  import { start } from "./asuka/<digest>/asuka.js";
  start({ container: document.getElementById("app"), wasm: "app.<digest>.wasm", name: "com.example.app", serviceWorker: "sw.<digest>.js" });
</script>
```

一つのページで、それぞれ別の要素に入れて、複数のアプリを動かせます。`serviceWorker` を渡すと、アプリの service worker（ビルドに含まれる `sw.<digest>.js`。対象とするページがあるフォルダーから配信します）が登録されます。渡さなければ、通知はページ自身のものになり、`asuka/sdk/push` は購読できません。`asuka/sdk/push` を使うなら、`index.html` と同じく `vapidPublicKey` も渡してください。`asuka/sdk/maps` のタイルを自前のサーバーから取得するなら、`mapTiles` を渡します（`"https://tiles.example.com/{z}/{x}/{y}.png"`。デフォルトは OpenStreetMap のタイルです）。

## 未対応の機能

- テキストフィールドのアクセサリーバーは、オンスクリーンキーボードがある場合（タッチスクリーン）にだけ表示されます。
- `asuka/sdk/notify` でスケジュールした通知は、アプリのページが一つも開いていない間は、指定の時刻に表示できません。ブラウザーに通知をスケジュールする手段がないためです（[前述](#スケジュールした通知)）。

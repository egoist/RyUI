# 配布

`asuka build` は、アプリのディレクトリ（または指定したディレクトリ）から、一つのプラットフォーム向けのリリースビルドを作り、そのディレクトリの `dist` に出力します。`asuka publish` は、iOS と Android のビルドをそれぞれのストアにアップロードします。行き先は TestFlight と App 審査、または Google Play のトラックです（[ストアへの公開](#ストアへの公開)を参照）。デスクトップのビルドについては、インストール済みのアプリが更新に使う署名済みのアップデートを、GitHub のリリースか S3 バケットにアップロードします（[自動アップデート](#自動アップデート)を参照）。

| `--platform` | `dist` に作られるもの | ビルドできる環境 | ユーザーへの届け方 |
|---|---|---|---|
| `ios` | `<Name>.ipa`（App Store 向けに署名済み） | Xcode のある Mac | TestFlight、App Store（`asuka publish`） |
| `android` | `<Name>.aab`（アップロード鍵で署名済み） | JDK、Android SDK、NDK があればどのシステムでも | Google Play（`asuka publish`） |
| `mac` | ユニバーサルの `<Name>.app`、または `.pkg`（`--appstore`） | Xcode のコマンドラインツールのある Mac | 署名・公証済みのダウンロード、Mac App Store |
| `windows` | `<Name>/`（`.exe` とそのアセット） | どのシステムでも | zip、または独自のインストーラーや MSIX |
| `linux` | `linux/`：インストールプレフィックス、`.tar.gz`、Flatpak マニフェスト | どのシステムでも | `.tar.gz`、Flatpak |
| `web` | `web/`（静的サイト） | どのシステムでも | 任意の Web サーバーや CDN |

`--platform` を省略すると、Mac、Windows PC、Linux マシンでは、そのコンピューター向けにビルドします。iOS と Android 以外のビルドはすべて `CGO_ENABLED=0` の素の Go なので、C のツールチェーンは必要ありません。

## asuka.json

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "version": "1.2",
  "build": 7,
  "icon": "icon.png",
  "ios": { "team": "ABCDE12345" },
  "android": { "keystore": "upload.jks", "keyAlias": "upload" }
}
```

キーはどれも省略できます。すべてのキーとその型、デフォルト値は [asuka.json リファレンス](asuka-json.md)にまとめてあります。次のキーはすべてのプラットフォームに適用されます。

- `name` はアプリの名前です。アイコンの下、Mac のメニューバー、Windows のウィンドウのタイトル、Linux のランチャー、Web ページのタイトルに表示されます。ビルドされるファイルの名前にもなります（`Notes.app`、`Notes.exe`）。デフォルトはディレクトリの名前です。
- `id` は、iOS と Mac では bundle ID、Android と Linux では application ID（Linux ではデスクトップファイル、アイコン、D-Bus 名に使われます）、Windows では AppUserModelID（トースト通知に使われます）になります。デフォルトは `dev.asuka.<directory>` です。
- `version` はユーザーに見えるバージョンです（デフォルトは `1.0`）。`build` はアップロードしたビルドの通し番号で、ビルドのたびに増やす必要があります（デフォルトは 1）。ただし `asuka publish` は、各ビルドにストア上の最大の番号に続く番号を自動で付けるので、`build` は番号の始まりを決めるだけです。Windows では二つを合わせて `.exe` のファイルバージョンになり（バージョン 1.2、ビルド 7 なら `1.2.0.7`）、Linux では metainfo の release になります。
- `icon` は 1024×1024 の正方形の PNG で、iOS 向けには透過のないものにします。各ビルドは、プラットフォームが必要とするサイズを作ります。iOS ではアセットカタログ、Android ではランチャーアイコン、Mac では `.icns`、Windows では `.exe` に埋め込む `.ico`、Linux では hicolor テーマの 16〜512 ピクセルのアイコン、Web では Web マニフェストのアイコン、favicon、iOS のホーム画面アイコンです。
- `accent` はアプリのアクセントカラーで、`"#rrggbb"` の形式で指定します。iOS では tint、Android では Material 3 のカラースキームのシード、Mac では `Accent`、塗りつぶしボタン、リンクの色（AppKit 自身のコントロールはユーザーが選んだアクセントカラーのままです）、Windows ではシステムのアクセントカラーに代わるコントロールのアクセント、Linux では libadwaita のアクセント、Web ではページのアクセントとマニフェストの `theme_color` になります。指定しなければ、各プラットフォーム自身のアクセントカラーになります。
- `scheme` を指定すると、`myapp://` の URL でアプリが開くようになります。対象は iOS、Android、Mac（`CFBundleURLTypes`）、Linux（デスクトップファイルの `x-scheme-handler`）、Windows（アプリが起動時に `HKEY_CURRENT_USER\Software\Classes` にユーザー単位で登録します）です。`links` を指定すると、自分のドメインへの https リンクでアプリが開きます（ユニバーサルリンク、App Links。iOS と Android）。どちらも `ui.OnOpenURL` で受け取ります。
- `fileTypes` はアプリが開くファイルの種類で、そのファイルは `ui.OnOpenFiles` で受け取ります。

  ```json
  "fileTypes": [
    {"name": "Markdown Document", "extensions": ["md", "markdown"], "role": "viewer"},
    {"name": "Notes Document", "extensions": ["note"], "icon": "note.png"}
  ]
  ```

  `extensions` は必須です。`name` のデフォルトは、最初の拡張子を大文字にして「Document」を付けたものです。`mimeTypes` と `uti` のデフォルトは、Asuka が知っている拡張子（md、txt、json、csv、pdf、png、jpg など）であれば、各システムでの呼び名になります。それ以外の拡張子では、アプリ独自の型 `application/x-<app>-<ext>` と `<id>.<ext>` になります。この型は Mac と iOS ではエクスポートされ、Linux では shared-mime-info に定義され、アプリがそのファイルのデフォルトアプリになります。`conformsTo` は、エクスポートまたはインポートする型が準拠する UTI です（デフォルトは `public.data`、`public.content`）。`role` は `editor`（デフォルト）または `viewer` です。`icon` は正方形の PNG で、Mac、Windows、Linux でそのファイルのアイコンになります。宣言のしかたはプラットフォームごとに異なります。Mac と iOS では Info.plist（`CFBundleDocumentTypes`。iOS ではドキュメントをその場で開きます）に、Android ではインテントフィルターとして宣言します。Windows ではアプリが起動時にユーザー単位で登録します（型ごとの ProgID、拡張子の `OpenWithProgids`、`.exe` に埋め込んだアイコン）。Linux ではデスクトップファイルの `MimeType` と `share/mime/packages/<id>.xml` に書かれます（`.tar.gz` を展開したあとは `update-mime-database` と `update-desktop-database` を実行してください。パッケージマネージャーや Flatpak なら自動で実行されます）。Web ではマニフェストの `file_handlers` に書かれます（Chromium でインストールしたアプリが対象です）。
- `singleInstance` はデフォルトで `true` です。Windows と Linux では、`id` のあるアプリは一つしか起動しません。二回目に起動したアプリは、引数を実行中のアプリに渡して（`app.OnSecondInstance`、ファイルは `ui.OnOpenFiles` に届きます）終了します。`false` にすると、起動した回数だけアプリが立ち上がります。Mac では、どちらの場合もアプリは一つしか起動しません。

プラットフォームごとのキーは次のとおりです。

- `ios.*` は iOS ビルドの設定です（[iOS](#ios)）。`ios.team` は Mac ビルドのチームにもなり、`ios.usage` の利用目的の説明文は Mac の Info.plist にも入ります。
- `android.*` は Android ビルドの設定です（[Android](#android)）。
- `mac.profile` は Mac ビルドのプロビジョニングプロファイル、`mac.appStoreProfile` は Mac App Store 向けのプロビジョニングプロファイルです。[macOS](#macos) を参照してください。
- `linux.*` は、Linux ビルドのデスクトップファイルと AppStream metainfo に書かれる内容です。[Linux](#linux) を参照してください。
- `web.vapidPublicKey` はアプリサーバーの VAPID 公開鍵で、Web で `asuka/sdk/push` を使うとき（Web Push）に必要です（[Web](web.md#asukasdkpushweb-push) を参照）。
- Windows ビルドには、上のキー以外に必要なものはありません。署名用の証明書は環境変数で渡します（[Windows](#windows)）。

次のキーはスマートフォンとタブレット向けです。

- `orientations`：スマートフォンでの画面の向きで、`portrait`、`landscape` を指定します（デフォルトは両方）。iPad では、Split View、Slide Over、ステージマネージャに必要なため、アプリはすべての向きに対応します。Android 16 も同様に、タブレットと折りたたみ端末ではこの設定を無視します。
- `ios.iphoneOnly` を指定すると、iPad を対象から外します。デフォルトではアプリは iPad アプリでもあり、ユーザーがウィンドウに与えたサイズで動きます。Android では、分割画面やデスクトップのウィンドウに合わせてサイズを変えられます。レイアウトは、デバイスのサイズではなく、ウィンドウのサイズである `ui.UseEnvironment()` の `Width` と `Height` をもとに組み立ててください。
- `shareTarget` を指定すると、ほかのアプリの共有シートにアプリが表示されます。`true` ならテキストとリンクを、`{"types": ["text", "image/*"], "max": 10}` ならファイルも受け取ります（[デバイスとサービス](device.md#ほかのアプリからの共有-uionshare)を参照）。受け取ったものは `ui.OnShare` に届きます。iOS ではアプリに含まれる共有拡張機能（Share Extension）になり、独自の bundle ID（アプリの ID に `.share` を付けたもの）を持ちます。
- `splash` は起動画面です。`{"image": "logo.png", "size": 96, "background": "#ffffff", "darkBackground": "#000000"}` のように指定します（[デバイスとサービス](device.md#起動画面-asukasdksplash)を参照）。
- `widgets` はアプリのホーム画面ウィジェット、`shortcuts` は Siri、Spotlight、ランチャー向けのアクションです（[デバイスとサービス](device.md#ホーム画面ウィジェット-asukasdkwidget)を参照）。ウィジェット拡張機能は独自の bundle ID（アプリの ID に `.widgets` を付けたもの）を持ちます。そのためリリースにはこの拡張機能用のプロファイルも必要で、`ios.widgetProfile` で指定できます。
- `android.googleServices` は、`asuka/sdk/push` に使う Firebase プロジェクトの `google-services.json` です。`android.mapsKey` は、`asuka/sdk/maps` に使う Google Maps Platform の API キーです。
- `ios.usage` は保護されたリソースの利用目的の説明文（`NSCameraUsageDescription` など。Mac のものも含みます）を指定し、`ios.infoPlist` は Info.plist にキーを追加します。`ios.backgroundModes` は `UIBackgroundModes` に値を追加します（`"audio"` なら、画面がロックされていても録音・再生できます）。`android.permissions` はパーミッションを追加します。

アプリがインポートしたパッケージは、自分に必要なパーミッションと利用目的の説明文をアプリに追加します。`asuka/sdk/camera` ならカメラ、`asuka/sdk/location` なら位置情報のものです。ただしデフォルトの説明文は汎用的なので、`ios.usage` に自分の言葉で書いてください。パッケージによっては、App ID のケイパビリティも必要です（`asuka/sdk/push` には Push Notifications、`asuka/sdk/appleauth` には Sign in with Apple、`asuka/sdk/integrity` には App Attest、`asuka/sdk/agerange` には Declared Age Range）。developer.apple.com でケイパビリティを有効にしてから、プロファイルを作り直してください。プロファイルに足りないケイパビリティは、`asuka build --platform ios`（と `--platform mac`）が教えてくれます。

## iOS

```sh
asuka build --platform ios
```

このコマンドは、App Store 向けに署名した `.ipa` を作ります。署名には、チームの配布用証明書（Xcode の「Accounts」設定から取得し、キーチェーンに入っているもの）と、アプリの ID 用のプロビジョニングプロファイルを使います。プロファイルは `ios.profile` か `--profile` で指定したもの、指定がなければインストール済みのもののうち最も合うものです。`--distribution ad-hoc` または `development` を指定すると、代わりにプロファイルに登録されたデバイス向けに署名します。`--simulator` を指定すると、シミュレーター用の署名なしの `.app` を作ります。

`asuka/sdk/sqlite` を使うアプリは、`Frameworks` に SQLite を `asqlite.framework`（実機用またはシミュレーター用）として含みます。このフレームワークはアプリと一緒に署名され、アプリが初めてデータベースを開くときに読み込まれます。アプリの拡張機能（共有シート、Notification Service Extension）は、アプリのものを読み込みます。

`shareTarget` を指定すると、共有拡張機能も署名されます。使うのは、アプリと同じチームの、拡張機能自身の ID（`com.example.notes` なら `com.example.notes.share`）用のプロファイルです。`ios.shareProfile` で指定したもの、指定がなければインストール済みのもののうち最も合うものが使われます。ワイルドカードのプロファイル（`com.example.*`）なら、アプリと拡張機能の両方に使えます。developer.apple.com で拡張機能の ID を登録し、アプリのときと同じようにプロファイルを作ってください。

ファイルを受け取る `shareTarget` には、App Group `group.<app ID>` も必要です。拡張機能は、このグループを通してファイルをアプリに渡します。developer.apple.com でグループを登録し、両方の ID（アプリと拡張機能）にそのグループを指定した App Groups ケイパビリティを付けてから、それぞれのプロファイルを作り直してください。ワイルドカードの ID には App Groups を付けられません。プロファイルにグループが含まれていなければ、ビルドはその旨を表示して止まります。シミュレーターでは、こうした準備はどれも必要ありません。独自の共有シート（`app.ShareSheet`）を持つアプリも、受け取るものが何であれ、データを渡すためにこのグループが必要です。

`asuka publish --platform ios` は、ビルドしてから App Store Connect にアップロードします（[ストアへの公開](#ストアへの公開)を参照）。Apple の Transporter でもアップロードできます。

**プライバシーマニフェスト。** App Store ではプライバシーマニフェストが必須で、Apple のリストにある API のうちアプリが使うものについて、それぞれ理由を記載する必要があります。Asuka は、バイナリが使うものをもとにこのマニフェストを書き出します。Go のランタイムは時刻とファイルの日時を読み取ります。マニフェストでは、トラッキングをしないことを宣言し、アプリが収集するデータを `ios.collected` に書いたとおりに記載します。App Store Connect はこれを、アプリのプライバシーラベルと照合します。

```json
"ios": {
  "collected": [{ "type": "EmailAddress", "linked": true, "purposes": ["AppFunctionality"] }]
}
```

`type` と `purposes` は、Apple の名前から接頭辞（`NSPrivacyCollectedDataType`、`NSPrivacyCollectedDataTypePurpose`）を除いたものです。`linked` はデータがユーザーの身元に紐付くことを、`tracking` はデータがユーザーのトラッキングに使われることを示します（[リファレンス](asuka-json.md#ios)を参照）。アプリがトラッキングをする場合や、マニフェストにこれ以上の内容が必要な場合は、独自のマニフェストを `ios/PrivacyInfo.xcprivacy` として書き、上で述べた理由もそこに含めてください。Asuka は代わりにそのファイルを使い、その場合 `ios.collected` は無視されます。なお、アプリのマニフェストがどちらであっても、`asqlite.framework` には SQLite が使うもの（ファイルの日時、ディスクの空き容量）についての独自のマニフェストが含まれています。

## Android

リリースはアップロード鍵で署名します。一度だけ作成し、安全に保管してください。

```sh
keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

鍵を `asuka.json` で指定し（`android.keystore`、`android.keyAlias`）、パスワードを環境変数で渡します。

```sh
ASUKA_KEYSTORE_PASSWORD=... asuka build --platform android
```

（鍵のパスワードがキーストアと異なる場合は `ASUKA_KEY_PASSWORD` も渡します。CI では `ASUKA_KEYSTORE` と `ASUKA_KEY_ALIAS` でキーストアとエイリアスを指定します。）これで Google Play 向けの `.aab` ができます。対象は、64 ビットと 32 ビットの ARM のスマートフォンと x86_64（エミュレーター、Chromebook）で、Android 13 以降です。Play はこの `.aab` から、各スマートフォンのアーキテクチャだけを含む APK を作ります。`asuka/sdk/sqlite` を使うアプリでは、各アーキテクチャの Go コードの隣に SQLite の `libasqlite.so` が入ります。Android は、アプリのライブラリと同じようにこれを APK から読み込みます（非圧縮、16 KB ページ）。`asuka publish --platform android` は、ビルドしてから Google Play にアップロードします（[ストアへの公開](#ストアへの公開)を参照）。

## ストアへの公開

`asuka publish` は、`asuka build` と同じようにリリースビルドを作り、ストアの API でアップロードします。最初に、アップロードを妨げそうなことをストアに確認します（アプリが登録されていて鍵にアップロードの権限があるか、そのバージョンがまだビルドを受け付けるか、TestFlight のグループが存在するか）。そのため、失敗するアップロードのためにわざわざビルドすることはありません。各ビルドの番号はストア上の最大の番号に 1 を足したもの（かつ `asuka.json` の `build` 以上）になるので、リリースのたびに番号を上げる必要はありません。`--build 42` で番号を指定することもできます。

**App Store Connect。** App Store Connect の「ユーザとアクセス」›「統合」›「App Store Connect API」で、「App マネージャ」の役割を持つ API キーを作成し、`.p8` をダウンロードします。そのキーを `asuka.json` か環境変数（`ASUKA_ASC_KEY_ID`、`ASUKA_ASC_ISSUER_ID`、そして `.p8` のパスか中身を入れた `ASUKA_ASC_KEY`）で指定します。`key` を指定しない場合は、`altool` と同じように `~/.appstoreconnect/private_keys` にある `AuthKey_<id>.p8` を探します。`.p8` はリポジトリに入れないでください。また、事前に App Store Connect でアプリを bundle ID とともに追加しておきます（「アプリ」›「+」›「新規 App」）。

```json
"ios": {
  "appStoreConnect": { "keyId": "ABC123DEF4", "issuerId": "57246542-96fe-1a63-e053-0824d011072a" }
}
```

```sh
asuka publish --platform ios                                  # TestFlight へ：チームのテスターに配布
asuka publish --platform ios --groups "Friends, QA" --notes "Try the new sync."
asuka publish --platform ios --review --notes @release-notes/ios --release manual
```

このコマンドは `.ipa` をアップロードし、Apple の処理が終わるのを待ちます（たいていは数分ですが、1 時間以上かかることもあります）。その間、Apple が警告した内容や却下した内容を表示します。そのあと、次のように動きます。

- `--notes` は TestFlight の「テスト内容」になり、`--review` を付けた場合は App Store の「このバージョンの新機能」にもなります。テキストか、アプリのプライマリ言語で書いた `@file` を指定します。あるいは `@dir` を指定し、言語ごとに一つずつファイルを置きます。ファイル名は App Store Connect での言語の名前に合わせます（`en-US.txt`、`ja.txt`）。アップデートでは、そのバージョンのすべての言語に「このバージョンの新機能」が必要です。
- `--groups` は、名前で指定した TestFlight のグループにビルドを配布します。外部グループ（チーム外のテスター）を指定すると、ビルドはベータ版 App 審査に回ります。先に TestFlight でテスト情報を入力しておいてください。
- `--review` は、App Store のバージョン（`asuka.json` の `version`）がなければ作成し、そこにビルドを割り当てて審査に提出します。初回の提出では、先に App Store Connect でストアの掲載情報、スクリーンショット、プライバシーに関する回答を入力しておく必要があります。足りないものは表示されます。`--release auto` は承認されるとすぐにリリースし、`--release manual` はリリースを手動で行うまで待ちます。
- `--no-wait` は、Apple がアップロードを受け取った時点で終了します。

**Google Play。** Google Play Android Developer API を有効にしたプロジェクトで、Google Cloud のサービス アカウントを作成し、JSON キーを発行します（「IAM」›「サービス アカウント」›「キー」）。Play Console の「ユーザーと権限」でそのアカウントのメールアドレスを招待し、アプリをリリースする権限を付与します。キーは `asuka.json`（`"android": {"serviceAccount": "play.json"}`）か `ASUKA_PLAY_SERVICE_ACCOUNT`（パスか JSON そのもの）で指定します。Play は、アプリの最初の `.aab` を Play Console からしか受け付けません。Play Console でアプリを作成し、最初のビルドは手動でアップロードしてください。2 つ目以降のビルドは `asuka publish` でアップロードできます。

```sh
asuka publish --platform android                              # 内部テストトラック
asuka publish --platform android --track beta --notes @release-notes/android
asuka publish --platform android --track production --rollout 0.1
```

- `--track` は `internal`（デフォルト、内部テスト）、`alpha`（クローズドテスト）、`beta`（オープンテスト）、`production`（製品版）、またはクローズドテストのトラックの名前です。
- `--rollout 0.1` は、リリースをユーザーの 10 分の 1 に段階的に公開します。割合は Play Console で引き上げます。`--draft` は下書きを作り、公開は Play Console で行います。アプリが公開されるまで Play は下書きしか受け付けないため、そのあいだリリースは下書きになります。
- `--notes` はリリースノートです（500 文字以内）。テキストか、ストアの掲載情報のデフォルト言語で書いた `@file` を指定します。あるいは `@dir` を指定し、Play での言語の名前を付けたファイルを言語ごとに置きます（`en-US.txt`、`ja-JP.txt`）。
- アプリに審査中の変更があると、処理は止まります。`--cancel-review` を付けると、その審査を取り消し、変更をこのリリースと一緒に送信します。Play がアプリの変更を送信待ちとして保留している場合（却下されたあとなど）は、変更は保存され、Play Console から送信することになります。

`--file dist/Notes.ipa`（または `.aab`）を指定すると、新たにビルドせず、`asuka build` で作ったビルドをアップロードします。

## macOS

```sh
asuka build --platform mac
```

このコマンドは、Mac 上で `dist/<Name>.app` を作ります。Apple シリコンと Intel の両方で動くユニバーサルアプリで、macOS 11 以降に対応し、Info.plist、アイコン、言語、アセットを含み、アドホック署名されています。つまり、自分の Mac で動かせる状態です。アプリがインポートしたパッケージは、iOS と同じく `ios.usage` から利用目的の説明文を追加します（位置情報の説明文は、Mac では `NSLocationWhenInUseUsageDescription` から取った `NSLocationUsageDescription` になります）。あわせて、それに必要な hardened runtime のエンタイトルメント（`com.apple.security.device.camera`、`...audio-input`、`...personal-information.location` など）も追加します。これがないと、Developer ID で署名したアプリは、カメラ、マイク、ユーザーのデータへのアクセスを拒否されます。独自の Swift（`macos/*.swift`）を持つアプリでは、その Swift が両方のアーキテクチャ向けにコンパイルされて `Contents/Frameworks` に入り、アプリと一緒に署名されます（[ネイティブコード](native.md)を参照）。`asuka/sdk/sqlite` を使うアプリでは、SQLite のライブラリ（両方のアーキテクチャに対応した `libasqlite.dylib`）もそこに入り、アプリと同じように署名されます。Developer ID で署名した場合、hardened runtime はアプリと同じチームが署名したライブラリしか読み込まず、公証サービスも一つひとつ確認するからです（[ライブラリ](data.md#ライブラリ)）。

Mac App Store 以外でほかの人に配布するには、Developer ID で署名し、Apple に公証してもらいます。これで Gatekeeper がアプリを開けるようになります。

```sh
xcrun notarytool store-credentials asuka   # 初回のみ：Apple ID、チーム、App 用パスワード
asuka build --platform mac --sign "Developer ID Application: Your Name (TEAMID)" --notarize asuka
```

`--notarize` は、署名したアプリを Apple の公証サービスに送り、結果を待ちます（たいていは数分です）。却下された場合はサービスのログを表示します。最後にチケットをアプリにステープルするので、オフラインでも開けます。CI では `--notarize env` を指定すると、キーチェーンのプロファイルの代わりに、環境変数の `ASUKA_APPLE_ID`、`ASUKA_APPLE_PASSWORD`（App 用パスワード）、`ASUKA_TEAM_ID` を使います。

プッシュ通知（`asuka/sdk/push`）、Sign in with Apple（`asuka/sdk/appleauth`）、App Attest（`asuka/sdk/integrity`）には、プロビジョニングプロファイルでしか付与されないエンタイトルメントが必要です。developer.apple.com（Profiles）で、それらのケイパビリティを有効にしたアプリの ID に対して macOS の Developer ID プロファイルを作成し、ダウンロードしてください。

```sh
asuka build --platform mac --profile Notes.provisionprofile --notarize asuka
```

プロファイルは `Contents/embedded.provisionprofile` としてアプリに入ります。アプリは、プロファイルのエンタイトルメントのうちパッケージが必要とするもの（`com.apple.developer.aps-environment`、`com.apple.developer.applesignin`、`com.apple.developer.devicecheck.appattest-environment`）、アプリの ID とチーム、そして hardened runtime を付けて署名されます。プロファイルに足りないものはビルドが教えてくれます。`--sign` を省略すると、プロファイルが許可する、キーチェーン内の証明書（Developer ID Application）で署名します。プロファイルに含まれない証明書を `--sign` に指定するとエラーになります。`--profile` には、パス、またはインストール済みのプロファイルの名前か UUID を指定します。`ASUKA_MAC_PROFILE` や asuka.json の `mac.profile` でも同じ指定ができます。どれも指定がなければ、これらのパッケージを使うアプリは、アプリの ID に最も合うインストール済みのプロファイル（Xcode のプロファイル用フォルダーにあるもの）で署名されます。それもなければプロファイルなしで署名されます。その場合、Mac で何が動かないかをビルドが表示し、アプリのそれ以外の部分は問題なく動きます。

`--appstore` を指定すると、代わりに Mac App Store 向けの `dist/<Name>.pkg` を作ります。アプリは App Sandbox に入り（ネットワークと、ユーザーが選んだファイルにアクセスできます）、Mac App Store のプロファイルの証明書（Apple Distribution）で署名されます。プロファイルは asuka.json の `mac.appStoreProfile`、なければ `--profile`、それもなければ最も合うものです。パッケージは、チームの Mac Installer Distribution 証明書で署名されます。アップロードには Transporter か `xcrun altool --upload-package` を使ってください。`asuka publish` は、まだ Mac のビルドのアップロードに対応していません。公証はストア外で配布するアプリのためのものなので、`--appstore` と `--notarize` を一緒に指定するとエラーになります。

## Windows

```sh
asuka build --platform windows               # x64。Arm 版 Windows ではエミュレーションで動きます
asuka build --platform windows --arch arm64  # Arm の PC でネイティブに動きます
```

このコマンドは、どのシステムからでも `dist/<Name>` というフォルダーを作ります。中身は `<Name>.exe` とその隣に置かれたアプリのアセットです。`asuka/sdk/sqlite` を使うアプリでは、同じアーキテクチャ向けの SQLite のライブラリ `asqlite.dll` も入ります（このライブラリが必要とするのは、Windows 10 と 11 なら必ずある Universal C Runtime だけです）。`.exe` は GUI プログラム（コンソールウィンドウを開きません）で、リソースが組み込まれています。Windows が使う各サイズのアイコン、バージョン情報（`version` と `build`）、そしてコモンコントロール 6、モニターごとの DPI 対応、UTF-8 を要求する、Windows 10 と 11 向けのマニフェストです。ディレクトリに独自の `.syso` を置いたアプリでは、代わりにそのリソースが使われます。フォルダーを zip にするか、パッケージ（インストーラー、MSIX）にして配布してください。

SmartScreen が発行元を認識できるように、`.pfx` 形式でエクスポートしたコード署名証明書で `.exe` に署名します。

```sh
ASUKA_WINDOWS_CERT=cert.pfx ASUKA_WINDOWS_CERT_PASSWORD=... asuka build --platform windows
```

ビルドは SHA-256 で署名し、証明書の期限が切れたあとも署名が有効であり続けるよう、署名にタイムスタンプを付けます。タイムスタンプには、最初に応答した RFC 3161 サーバーを使います。候補は `ASUKA_WINDOWS_TIMESTAMP` に指定したサーバー（カンマ区切りのリスト）で、指定がなければ DigiCert、Sectigo、GlobalSign のサーバーです。どれも応答しなければ、ビルドはその旨を表示して失敗します。`ASUKA_WINDOWS_TIMESTAMP=none` にすると、タイムスタンプなしで署名します（その場合、署名は証明書の期限とともに無効になります）。署名には、Windows では `signtool`（Windows SDK のもの。`PATH` にあればそれを、なければインストール済みの最新のものを使います）、それ以外のシステムでは `osslsigncode`（`brew install osslsigncode`、`apt install osslsigncode`）を使い、`asqlite.dll` にも同じように署名します。`ASUKA_WINDOWS_CERT` がなければ、`.exe` は署名されません。ハードウェアトークンやクラウドサービス（Azure Trusted Signing）にある証明書では、それぞれのツールで署名します。ビルドのあと、フォルダー内の `.exe`（と `asqlite.dll`）にそのツールで署名してください。

通知を表示するアプリ（`asuka/sdk/notify`）では、初回にユーザーの「スタート」メニューにショートカットが作られます。Windows でアプリがトースト通知を出すには、これが必要です。ショートカットの AppUserModelID は asuka.json の `id`（なければアプリの名前から作ったもの）です。

## Linux

```sh
asuka build --platform linux                # このマシンのアーキテクチャ（それ以外の環境では amd64）
asuka build --platform linux --arch arm64   # amd64 または arm64
```

ホストは cgo を使わずに GTK 4 と libadwaita を呼び出すので、どのシステムからでもビルドできます。`dist/linux` には次のものが作られます。

- `<name>/`：インストールプレフィックスの構成になっているフォルダーで、アプリの名前を小文字にした名前が付きます（`My Notes` なら `my-notes`）。
  - `bin/my-notes`：アプリ本体
  - `share/my-notes/assets`：アセット。アプリは `bin/` からの相対位置でこれを見つけます。
  - `lib/my-notes/libasqlite.so`：SQLite のライブラリ。`asuka/sdk/sqlite` を使うアプリに入り、これも `bin/` から見つけます（glibc 2.17 以降。SQLite 自身のシンボルを公開しないので、同じプロセスにシステムの SQLite があっても衝突しません）。
  - `share/applications/<id>.desktop`：ランチャーが表示し、起動に使うもの
  - `share/icons/hicolor/<size>/apps/<id>.png`：16〜512 ピクセルの各サイズの `icon`
  - `share/metainfo/<id>.metainfo.xml`：AppStream metainfo。ソフトウェアセンターに表示される内容です。

  このフォルダーから `bin/my-notes` を実行するか、ランチャーが見つけられるプレフィックスにインストールします。

  ```sh
  tar -xzf my-notes-1.0-linux-amd64.tar.gz --strip-components=1 -C ~/.local
  ```

- `my-notes-<version>-linux-<arch>.tar.gz`：配布用に、そのフォルダーをまとめたもの
- `<id>.yml`：そのフォルダー（`bin`、`lib`、`share`）を GNOME ランタイム上でパッケージにする [Flatpak](https://flatpak.org) のマニフェスト

  ```sh
  flatpak install flathub org.gnome.Platform//50 org.gnome.Sdk//50   # 初回のみ
  flatpak-builder --user --install --force-clean build dist/linux/com.example.notes.yml
  flatpak run com.example.notes
  ```

アプリを動かす環境には、GTK 4.14 と libadwaita 1.5 以降が必要です（GNOME ランタイム 46 以降に含まれています）。アプリの ID は application ID でもあり、デスクトップファイル、アイコン、D-Bus 名（二回目に起動すると、最初に起動したアプリのウィンドウが表示されます）、データディレクトリ（`~/.local/share/<id>`）の名前になります。ID は英字、数字、`_`、`-` だけからなり、二つ以上の部分に分かれていて、どの部分も数字で始まってはいけません。

デスクトップファイルと metainfo に書く内容は、`asuka.json` で指定します。

```json
"linux": {
  "summary": "Write things down",
  "description": "Notes, kept on your computer.\n\nA second paragraph.",
  "categories": ["Office"],
  "keywords": ["memo"],
  "license": "MIT",
  "developer": "Example Inc.",
  "homepage": "https://example.com",
  "flatpakRuntime": "50"
}
```

`categories` は [freedesktop.org のカテゴリ](https://specifications.freedesktop.org/menu-spec/latest/category-registry.html)から選びます（デフォルトは `Utility`）。`license` は SPDX 式です。Flathub とソフトウェアセンターは、summary、description、license、developer を求めます。

## Web

```sh
asuka build --platform web
```

このコマンドは `dist/web` に静的サイトを作ります。中身は、`index.html`、WebAssembly にしたアプリ、Web ホストのスクリプト、アプリの Service Worker、アセット、アイコン付きの Web アプリマニフェスト、gzip で圧縮したコピー、そして Cloudflare と Netlify 向けの `_headers` ファイルです。ファイル名にはダイジェストが付くので、`index.html` とマニフェスト以外は無期限にキャッシュできます。このフォルダーを任意の Web サーバーや CDN から、`.wasm` を `application/wasm` として、HTTPS で配信してください（Service Worker、Web Push、セキュアストアには HTTPS が必要です）。中身の詳細と、ブラウザーでの動作の違いについては [Web](web.md) を参照してください。

あわせて `version.json` も書き出します。ビルドしたバージョンとそのリリースノート（`CHANGELOG.md` の該当セクション）が入っていて、ページ内の `asuka/sdk/update` がこれを読みます。サイトより古いページは、再読み込みを提案します（[自動アップデート](#自動アップデート)）。

## 自動アップデート

デスクトップアプリは、Sparkle を使う Mac アプリと同じように自分自身を更新します。フィードで新しいバージョンを確認し、リリースノートを表示し、ダウンロードします（実行中のバージョンからの差分があれば、変わった部分だけです）。それから自分の鍵で署名されていることを確かめ、インストールして再起動します。スマートフォンのアプリはストアから更新され、Web アプリのページは新しいサイトがあれば再読み込みされます。

### 準備

アップデートに署名するための鍵ペアを、一度だけ作ります。

```sh
asuka update keygen
```

このコマンドは、秘密鍵 `asuka-update.key` と公開鍵 `asuka-update.pub` を設定ディレクトリの `asuka/update-keys` に書き出し（`--out` で別の場所を指定できます）、`asuka.json` に追加する内容を表示します。秘密鍵はリポジトリに入れず、パスワードマネージャーと CI のシークレットに保管してください。インストール済みのアプリはこの鍵で署名されたアップデートしか受け付けないため、鍵をなくすとアプリを更新できなくなります。また、鍵を手に入れた人は誰でも、ユーザーにコードを配信できてしまいます。

```json
"updates": {
  "publicKey": "…the contents of asuka-update.pub…",
  "github": "you/notes"
}
```

| キー | 内容 |
|---|---|
| `publicKey` | アプリに組み込まれる、`asuka-update.pub` の中身 |
| `github` | `owner/name`：`v1.2.0` のようなタグの付いたリリースにアップデートを置く、公開リポジトリ。アプリは最新リリースのものを読みます |
| `tagPrefix` | リリースのタグの接頭辞（デフォルトは `v`）。`desktop-v` のように別の接頭辞にすると、リポジトリの最新リリースを別のもの（CLI など）のリリースにできます。その場合アプリは、GitHub API でこの接頭辞の付いた最新のリリースを探します |
| `url` | `github` の代わりに指定する、ファイルを配信する HTTPS のディレクトリ（S3 や R2 のバケット、CDN、自分のサーバー）。アプリは `url/update-<target>.json` を読みます |
| `feed` | アプリが代わりに問い合わせる自分のサーバー（ファイルの置き場所は `url` で指定）。[独自のアップデートサーバー](#独自のアップデートサーバー)を参照 |
| `s3` | `url` を配信するバケットで、`asuka publish` のアップロード先：`bucket`、`prefix`（バケット内のディレクトリ）、`region`、`endpoint`（R2 や MinIO のもの。デフォルトは Amazon S3）、`pathStyle` |
| `privateKey` | 秘密鍵のパス（`~/` はホームディレクトリ）。自分のマシンでビルドするときに使います |
| `changelog` | Markdown ファイル。その `## 1.2.0`（または Keep a Changelog 形式の `## [1.2.0] - 2026-11-13`）セクションがリリースノートになります。デフォルトは `CHANGELOG.md` で、このファイルがある場合は、そのバージョンのセクションが必要です |
| `deltas` | 何バージョン前まで差分を作るか（デフォルトは 3、0 なら作りません） |

CI やテストでは、環境変数でも同じ指定ができます。`ASUKA_UPDATE_PRIVATE_KEY`（秘密鍵そのもの、またはそのファイルのパス）と、`ASUKA_UPDATE_PUBLIC_KEY`、`ASUKA_UPDATE_URL`（それぞれ `publicKey` と `url` より優先されます）です。

### アプリ側の実装

`update.Window` はアップデート用のウィンドウで、アプリのルートに置きます。`update.AppMenu` は、アプリのメニューに「アップデートを確認…」を追加します。Mac では「（アプリ名）について」の次に入り、Windows では「ヘルプ」メニュー、Linux ではヘッダーバーのメインメニューに入ります。

```go
func Root() ui.Node {
	return ui.Column(ui.Style{},
		update.Window(update.Options{Icon: "logo.png"}), // assets/ にある画像
		ui.MenuBar(ui.Val([]ui.MenuBarMenu{update.AppMenu()})),
		// ...
	)
}
```

アプリは起動の 10 秒後に確認し、その後は 1 日に 1 回確認します（`Options.Interval`）。新しいバージョンが出ていれば、リリースノートとともに **アップデートをインストール**、**後で通知**、**このバージョンをスキップ** のボタンを表示します（スキップしたバージョンはバックグラウンドの確認では二度と提示されませんが、その次のバージョンは提示されます）。「今後はアップデートを自動的にダウンロードしてインストール」というチェックボックスもあり、これをオンにすると、確認で見つかったアップデートは尋ねずにインストールされ、次回の起動から新しいバージョンが動きます。インストール中はダウンロードの進捗を表示し、終わると **今すぐ再起動** を表示します。`update.CheckForUpdates()` は、ユーザーが求めたときの確認（メニュー項目やボタンから）を行います。このときウィンドウは、アプリが最新であることや、アプリが自分で更新できないことも伝えます。`AutomaticChecks`、`SetAutomaticChecks`、`AutomaticDownloads`、`SetAutomaticDownloads`、`LastCheck` はユーザーの選択（アプリのデータディレクトリの `update.json` に保存されます）で、設定画面を作るのに使えます。ウィンドウの文言は Asuka が用意したもので、アプリの言語で表示されます。文言を変えたい場合は、カタログで `asuka.updateInstall` などのキーを定義します。

アプリ独自の UI を作る場合は、パッケージの関数をメインスレッド以外から呼ぶか、アップデーターの状態（`update.Use()`：確認中、利用可能、ダウンロード中とそのバイト数、準備完了など）を使います。

```go
go func() {
	r, err := update.Check(ctx) // nil なら最新の状態
	if err != nil || r == nil {
		return
	}
	// r.Version、r.Notes（Markdown）、r.Date、r.Size
	if err := r.Install(ctx, func(done, total int64) { /* 進捗を ui.Post で伝える */ }); err == nil {
		update.Relaunch() // app.Relaunch：アプリを終了し（OnQuit で取り消せます）、新しいバージョンを起動します
	}
}()
```

`update.Supported()` は、アプリが自分自身を更新できるかどうかと、できない場合はその理由を返します。更新できないのは、開発ビルドや `updates` のないビルド（`ErrNotConfigured`）、システムのパッケージ・Flatpak・Snap・Mac App Store・Microsoft Store でインストールされたアプリ（`ErrManaged`。更新はそれらが行います）、ダウンロードした場所から実行されていて macOS に App Translocation されている Mac アプリ（`ErrTranslocated`。ユーザーが「アプリケーション」フォルダーに移動する必要があります）、ユーザーが書き込めないフォルダーにあるアプリ（`ErrNotWritable`）、そしてスマートフォン（`ErrUnsupported`）です。こうした場合、ウィンドウはバックグラウンドでは確認せず、ユーザーが確認したときにその理由を伝えます。

`update.SetChannel("beta")` を呼ぶと、ベータ版を選んだユーザーに向けて、そのチャンネルのプレリリースも提示します。バージョンのチャンネルは、プレリリース部分の英字です（`1.4.0-beta.2` なら `beta`）。`"*"` を指定すると、すべてのプレリリースを受け取ります。正式リリースはどのチャンネルでも提示されます。`update.SetSource` を使うと、マニフェストの読み込みとファイルのダウンロードを、アプリ独自のコードで行えます（ユーザーのトークンを必要とするサービスなど）。その場合も、署名はいつもどおり検証されます。

### ビルドと公開

`updates` と秘密鍵があれば、`asuka build --platform mac`、`windows`、`linux` は、ビルドの隣の `dist/updates` に次のものを書き出します。

- `notes-1.2.0-<target>.tar.gz`：インストールされた状態のアプリをまとめて署名したもの（SHA-256 に対する Ed25519 署名）。中身は、Mac ではバンドル、Windows ではフォルダーの中身で、Linux ではビルドの `.tar.gz` そのものです。ターゲットは `darwin-universal`、`windows-amd64`、`windows-arm64`、`linux-amd64`、`linux-arm64` です。
- 以前に公開したバージョンからの差分アップデート（`notes-1.1.0-to-1.2.0-<target>.delta`）。ビルドは、公開済みのマニフェストが示す場所から以前のバージョンを（署名を確かめながら）ダウンロードし、変わった部分を書き出します。内容は、インストール済みのアプリから移動またはコピーするファイル、変更されたファイルの bsdiff パッチ、そして残りのファイルです。アプリは自分のバージョンからの差分だけをダウンロードし、それが失敗したとき（インストール後にアプリが変更されていた場合）はアーカイブ全体をダウンロードします。最初のバージョンには差分はありません。
- `update-<target>.json`：マニフェスト。バージョン、リリースノート、日付、アーカイブと差分の URL・サイズ・署名、そして以前のバージョン（次の差分を作るため）を含みます。プレリリースのマニフェストは `update-<target>-beta.json` で、そのチャンネルのアプリだけが読みます。

秘密鍵がなければ、アプリをビルドするだけでアップデート用のファイルは書き出さず、その旨を表示します。`--version 1.2.0` を指定すると、`asuka.json` とは別のバージョンとしてビルドします（CI ではタグからバージョンを取ります）。Windows のフォルダーと Linux のプレフィックスには `.asuka-files` も入ります。これはそのバージョンがインストールするファイルの一覧です。アップデートの際に、次のバージョンにはもうないファイルを削除し、それ以外のファイルには一切手を付けないために使います。

`asuka publish` はビルドしてから、これらをアップロードします。マニフェストは最後にアップロードするので、ファイルがまだ揃っていないマニフェストをアプリが読むことはありません。

```sh
asuka publish --platform mac --sign "Developer ID Application: …" --notarize asuka
asuka publish --platform windows --arch arm64
asuka publish --platform linux --draft
```

- `github` の場合は、そのバージョンのタグ（`v1.2.0`）のリリースにアップロードします。[GitHub CLI](https://cli.github.com) にサインインしている必要があります（`gh auth login` または `GH_TOKEN`）。リリースがなければ下書きとして作り、そのあと公開します。プレリリースのバージョンならプレリリースとして公開し、独自の `tagPrefix` を使っている場合は最新リリース（latest）にはしません。`--draft` を指定すると下書きのまま残るので、すべてのプラットフォームが揃ってから公開できます（`gh release edit v1.2.0 --draft=false`）。アプリが確認するのは、公開済みの最新リリースです。
- `s3` の場合は、バケットにアップロードします。マニフェストは名前がバージョン間で変わらないので、`Cache-Control: no-cache` を付けます。認証には `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`（と `AWS_SESSION_TOKEN`）を使い、リージョンは `s3.region`、`AWS_REGION`、どちらもなければ `us-east-1`（R2 はこれを受け付けます）です。ほかのツールは必要ありません。
- `url` だけの場合は、`dist/updates` を自分でそこにアップロードしてください。マニフェストは最後にアップロードし、以前のバージョンのファイルも残しておきます。

### 独自のアップデートサーバー

`feed` を指定すると、アプリは代わりに自分のサーバーに問い合わせます。その URL の `{target}`、`{version}`（アプリが実行中のバージョン）、`{channel}` には値が埋め込まれます。

```json
"updates": {
  "publicKey": "…",
  "url": "https://downloads.example.com/notes",
  "feed": "https://api.example.com/updates?target={target}&version={version}&channel={channel}"
}
```

サーバーは、新しいバージョンがなければ `204 No Content`（または 404）を返し、あればマニフェスト、つまり `asuka build` が書き出すのと同じ JSON を返します。

```json
{
  "version": "1.2.0",
  "notes": "- Faster sync.\n- Fixed the sidebar.",
  "date": "2026-11-13T09:00:00Z",
  "url": "https://downloads.example.com/notes/notes-1.2.0-darwin-universal.tar.gz",
  "size": 9123456,
  "signature": "…base64…",
  "deltas": [
    {"from": "1.1.0", "url": "https://…/notes-1.1.0-to-1.2.0-darwin-universal.delta", "size": 21432, "signature": "…"}
  ]
}
```

`asuka build` は `url` 用のファイルを書き出します。書き出されたものをそのまま配信しても、マニフェストを好きなように選んで返してもかまいません（段階的なロールアウト、アカウントごとのチャンネルなど）。署名は `asuka build` が作ったものなので、サーバーはどれを返すかを選ぶだけです。

### 各プラットフォームでの更新の仕組み

- macOS：新しいバンドルは検証されたうえで（コード署名が有効で、実行中のアプリと同じチームと識別子であることを確かめ、検疫属性を取り除きます）、アプリが終了するまでその隣で待機します。アプリがどのように終了しても、ヘルパーが古いバンドルを新しいバンドルに置き換えます。アプリが再起動を求めていた場合は Launch Services でアプリを開くので、Dock のアイコンは一つのままです。アプリのフォルダーは、ユーザーが書き込めるものである必要があります。管理者なら `/Applications`、または `~/Applications` です。
- Windows：アプリの実行中にフォルダーの中身が置き換えられます。各ファイルはまず名前を変えて脇に退避し、一つでも置き換えられなければすべて元に戻します。実行中の `.exe` は名前を変え（Windows はこれを許可しています）、次回の起動時に削除します。フォルダーはユーザーのもの（`Program Files` ではない場所）である必要があります。ユーザーのプロファイル内に展開したものや、インストーラーによるユーザー単位のインストール（`%LOCALAPPDATA%\Programs`）です。MSIX パッケージは、Store か App Installer を通じて更新されます。
- Linux：tarball のプレフィックス（`~/.local` など）にある、そのアプリのファイルだけ（`bin/notes`、`share/notes/`、デスクトップファイル、アイコン、metainfo、MIME タイプ）を一つずつリネームで置き換え、そのあとデスクトップのキャッシュを更新します（`update-desktop-database`、`update-mime-database`）。Flatpak、Snap、ディストリビューションのパッケージ、AppImage はそれぞれの仕組みで更新されるため、その場合アプリは自分自身を更新しません。

## 既存アプリへの組み込み

Asuka のアプリは、Xcode や Android Studio で作ったアプリの中に組み込めます。Go で一度だけ書いた機能を、ネイティブアプリから表示できるということです。（Web では、ビルドしたアプリを自分のページの要素の中で動かせます。[Web](web.md) を参照してください。）

**iOS。** `asuka build --platform ios --xcframework` は `dist/AsukaApp.xcframework` を作ります。中身は、アプリの Go コードと UIKit ホスト（実機とシミュレーターの両方向け）、Swift のモジュールインターフェース、アプリのアセットです。これをターゲットに追加し（Embed & Sign）、ほかのビューコントローラーと同じように Asuka アプリを表示します。

```swift
import AsukaApp

present(AsukaViewController(), animated: true)
// push したり、タブにしたり、ウィンドウのルートにしたりもできます
```

`asuka/sdk/sqlite` を使うアプリでは、その隣に SQLite のライブラリ `dist/asqlite.xcframework` もできます。これも埋め込んでください（Embed & Sign）。Go のコードは、これをアプリの `Frameworks` から読み込みます。このライブラリには独自のプライバシーマニフェストが含まれています。

**Android。** `asuka build --platform android --aar` は `dist/maven` に Maven リポジトリを作ります。中身は、ABI ごとのアプリの Go コードを含むホストライブラリ、アセット、宣言済みの `AsukaActivity` で、依存関係（Material、AndroidX）は POM に記載されています。その隣には、アプリが使うオプションのモジュール（`asuka/sdk/push`、`asuka/sdk/maps`）も置かれ、`asuka/sdk/sqlite` を使うアプリでは ABI ごとの SQLite のライブラリも置かれます。リポジトリと、コマンドが表示する依存関係をプロジェクトに追加し、Activity を起動します。

```kotlin
// settings.gradle.kts
dependencyResolutionManagement { repositories { maven { url = uri("path/to/dist/maven") } } }
// app/build.gradle.kts
dependencies { implementation("dev.asuka.counter:counter:1.0.0") }
// 任意の場所で
startActivity(Intent(this, dev.asuka.AsukaActivity::class.java))
```

組み込まれたアプリは一つの Asuka アプリで、単体のビルドと同じく一度だけマウントされます。`asuka.json` がアプリ全体に対して設定するもの（アイコン、URL スキーム、共有ターゲット、ウィジェット、パーミッションの説明文）は、組み込む側のアプリが自分のプロジェクトで設定します。

## クラッシュレポート用のシンボル

ネイティブのクラッシュのフレームは、アプリのバイナリ内のアドレスです。クラッシュレポートのサービス（あるいは App Store Connect や Google Play）は、ビルドのシンボルを使ってこれを関数名と行番号に変換します。Go 自身のスタックトレースにはシンボルは要りません（必要なテーブルはバイナリの中にあります）。そのため、Go の panic はそのまま読めます。

**iOS。** `asuka build --platform ios` は、ホストをデバッグ情報付きでコンパイルし、Go コードのデバッグ情報も残します。そして Xcode のアーカイブと同じように、各バイナリ（アプリ本体、共有シートがある場合はそのフレームワーク、拡張機能）の dSYM を作り、そのあとバイナリからシンボルを取り除きます。dSYM は `.ipa`（またはシミュレーター用の `.app`）の隣の `dist/<Name>.dSYMs` に置かれ、シンボルは `.ipa` の中（`Symbols/`）にも入ります。そのため、App Store Connect と Xcode の Organizer は、表示するクラッシュをシンボル化できます。SQLite のライブラリ（`asqlite.framework`）は、シンボルを取り除いた状態で dSYM なしで同梱されるので、そのフレームは `asq_call` と表示されます。dSYM は、利用しているクラッシュレポートのサービスにアップロードしてください。

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes.dSYMs
# Crashlytics のツール（SDK に含まれています）：
upload-symbols -gsp GoogleService-Info.plist -p ios dist/Notes.dSYMs
```

**Android。** `asuka build --platform android` は、Go のライブラリのシンボルテーブルを Gradle のために残します。Gradle はパッケージするライブラリからシンボルを取り除き、そのシンボルを、R8 が名前を変えた Kotlin のマッピングとともに `.aab` に入れます。Google Play はこれらを使って、ネイティブのクラッシュと ANR をシンボル化し、スタックトレースの難読化を解除します。クラッシュレポートのサービス向けには、`.aab` の隣にも `dist/<Name>-native-debug-symbols.zip` と `dist/<Name>-mapping.txt` が置かれます。

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes-native-debug-symbols.zip
sentry-cli upload-proguard --org ORG --project PROJECT dist/Notes-mapping.txt
```

**Mac、Windows、Linux、Web。** アプリは一つの Go バイナリで、シンボルテーブルとデバッグ情報なし（`-s -w`）でビルドされます。それでも Go のスタックトレースには関数名と行番号が残るので、panic はそのまま読めます。アップロードするシンボルはありません。

リリースごとのシンボルは保管しておいてください。クラッシュは、それが起きたビルドのシンボルでシンボル化されます。レポートそのものについては[デバイスとサービス](device.md#クラッシュレポート-asukasdkcrash)を参照してください。

## リリース前の確認

リリースする前に、ユーザーが使うのと同じ環境でリリースビルドを動かしてみてください。

- スマートフォン：iOS は `--distribution development` でビルドしたもの、Android は Play の内部テストトラックから入れたもの（`asuka publish --platform android --track internal`）です。後者では、Play が `.aab` から作る APK がインストールされます。TestFlight と Play の内部テストでは、シミュレーターでは確かめられないことも確認できます。
- Mac：署名・公証済みの `.app` を、別の Mac でダウンロードして試します（Gatekeeper が確認するのはダウンロードしたものです）。`spctl --assess --type execute -vv Notes.app` で、Gatekeeper がどう判断するかがわかります。
- Windows と Linux：フォルダー、または `.tar.gz` や Flatpak を、そのアプリを一度も入れたことのないマシンで試します（Linux では GTK 4.14 と libadwaita 1.5 以降のあるマシン）。
- Web：ビルドしたサイトを HTTPS で配信し、サポートする各ブラウザーで試します。

## CI での配布

次の GitHub Actions のワークフローは、`v1.2` のようなタグを push すると両方のアプリをビルドし、`asuka publish` で iOS のビルドを TestFlight に、Android のビルドを Play の内部テストトラックにアップロードします。さらに、そのタグの GitHub リリースの下書きに、`.ipa` と `.aab` をそれぞれアップロードが済みしだい添付します。準備ができたら下書きを公開してください。（手動で実行した場合、ビルドはその実行のアーティファクトとして残るだけです。）アプリがリポジトリのルートにあることを前提としています。最初のアップロードは手動で行ってください。Play は、アプリに一度もアップロードがないうちは API からのアップロードを受け付けません。

```yaml
# .github/workflows/release.yml
name: release
on:
  push:
    tags: ["v*"]
  workflow_dispatch:
permissions:
  contents: write # タグのリリースにビルドを添付するため

jobs:
  release:
    if: startsWith(github.ref, 'refs/tags/')
    runs-on: ubuntu-latest
    steps:
      - run: gh release create "$GITHUB_REF_NAME" --draft --generate-notes --verify-tag
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}

  ios:
    needs: release
    if: ${{ !failure() && !cancelled() }} # 手動で実行して release がスキップされたときも動かす
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the distribution certificate and the profile
        env:
          CERTIFICATE: ${{ secrets.IOS_CERTIFICATE }}
          CERTIFICATE_PASSWORD: ${{ secrets.IOS_CERTIFICATE_PASSWORD }}
          PROFILE: ${{ secrets.IOS_PROFILE }}
        run: |
          KC=$RUNNER_TEMP/signing.keychain-db
          PW=$(openssl rand -hex 16)
          security create-keychain -p "$PW" "$KC"
          security set-keychain-settings -lut 3600 "$KC"
          security unlock-keychain -p "$PW" "$KC"
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/certificate.p12
          security import $RUNNER_TEMP/certificate.p12 -k "$KC" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
          security set-key-partition-list -S apple-tool:,apple: -k "$PW" "$KC"
          security list-keychains -d user -s "$KC" $(security list-keychains -d user | tr -d '"')
          echo "$PROFILE" | base64 --decode > $RUNNER_TEMP/app.mobileprovision
          # shareTarget を使う場合は、拡張機能のプロファイルを asuka が探す場所にインストールする：
          # mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
          # echo "$SHARE_PROFILE" | base64 --decode > ~/Library/MobileDevice/Provisioning\ Profiles/share.mobileprovision
      - name: build, and upload to TestFlight
        env:
          ASUKA_ASC_KEY_ID: ${{ secrets.ASC_KEY_ID }}
          ASUKA_ASC_ISSUER_ID: ${{ secrets.ASC_ISSUER_ID }}
          ASUKA_ASC_KEY: ${{ secrets.ASC_KEY }}
        run: asuka publish --platform ios --profile $RUNNER_TEMP/app.mobileprovision --no-wait
      - uses: actions/upload-artifact@v4
        with: { name: ios, path: dist/*.ipa }
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.ipa --clobber

  android:
    needs: release
    if: ${{ !failure() && !cancelled() }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-java@v4
        with: { distribution: temurin, java-version: 17 }
      - uses: android-actions/setup-android@v3
        with: { packages: platform-tools }
      - name: an NDK
        run: sdkmanager "ndk;27.1.12297006"
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the upload key
        env:
          KEYSTORE: ${{ secrets.ANDROID_KEYSTORE }}
        run: echo "$KEYSTORE" | base64 --decode > $RUNNER_TEMP/upload.jks
      - name: build, and upload to Play's internal test track
        run: asuka publish --platform android --track internal
        env:
          ASUKA_KEYSTORE: ${{ runner.temp }}/upload.jks
          ASUKA_KEYSTORE_PASSWORD: ${{ secrets.ANDROID_KEYSTORE_PASSWORD }}
          ASUKA_PLAY_SERVICE_ACCOUNT: ${{ secrets.PLAY_SERVICE_ACCOUNT }}
      - uses: actions/upload-artifact@v4
        with:
          name: android
          path: dist/*.aab
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.aab --clobber
```

ワークフローが使うシークレット（Settings › Secrets and variables › Actions）は次のとおりです。ファイルは base64 にして登録します（`base64 -i file | pbcopy`）。

- `IOS_CERTIFICATE`：Apple Distribution 証明書とその秘密鍵を、「キーチェーンアクセス」から `.p12` として書き出したもの。`IOS_CERTIFICATE_PASSWORD` は、書き出すときに付けたパスワードです。
- `IOS_PROFILE`：developer.apple.com で取得した、アプリの App Store 用プロビジョニングプロファイル（`shareTarget` を使う場合は、`SHARE_PROFILE` に共有拡張機能のプロファイルを入れます）。
- `ASC_KEY_ID`、`ASC_ISSUER_ID`、`ASC_KEY`（`.p8` の中身）：「ユーザとアクセス」›「統合」で作成した、「App マネージャ」の役割を持つ App Store Connect API キー。
- `ANDROID_KEYSTORE`：アップロード鍵の `.jks`。`ANDROID_KEYSTORE_PASSWORD` はそのパスワードです（鍵のパスワードが異なる場合は `ASUKA_KEY_PASSWORD` も追加します）。
- `PLAY_SERVICE_ACCOUNT`：Google Cloud のサービス アカウントの JSON キー。このアカウントは、Play Console の「ユーザーと権限」で、テストトラックにリリースする権限を付けて招待しておきます。

CLI には iOS と Android のホストが含まれているため、アプリの `go.mod` にある Asuka のバージョンに合わせてインストールします。各ビルドの番号はストア上の最大の番号の次になるので、タグを付ける前に番号を上げる必要はありません。`--no-wait` を付けているので、iOS のジョブは Apple の処理が終わるのを待たず、Apple がアップロードを受け取った時点で終わります。App Store は最近の Xcode で作ったビルドしか受け付けません。ランナーのデフォルトの Xcode が古い場合は、`sudo xcode-select -s /Applications/Xcode_<version>.app` で選んでください。

### デスクトップ

次のジョブを同じワークフローに加えると、タグに対してデスクトップアプリをビルドし、そのリリースの下書きに添付します。Mac 版は Developer ID で署名して公証し、Windows 版は x64 と Arm 向けにビルドして署名し、Linux 版は amd64 と arm64 向けにビルドします。Windows 版と Linux 版はどちらも Linux 上でビルドできるので、一つの Ubuntu ランナーで両方を作れます。（ここではアプリの名前を Notes としています。ファイル名は `asuka.json` の `name` をもとに付けられます。）

```yaml
  mac:
    needs: release
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the Developer ID certificate
        env:
          CERTIFICATE: ${{ secrets.MAC_CERTIFICATE }}
          CERTIFICATE_PASSWORD: ${{ secrets.MAC_CERTIFICATE_PASSWORD }}
        run: |
          KC=$RUNNER_TEMP/signing.keychain-db
          PW=$(openssl rand -hex 16)
          security create-keychain -p "$PW" "$KC"
          security set-keychain-settings -lut 3600 "$KC"
          security unlock-keychain -p "$PW" "$KC"
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/certificate.p12
          security import $RUNNER_TEMP/certificate.p12 -k "$KC" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
          security set-key-partition-list -S apple-tool:,apple: -k "$PW" "$KC"
          security list-keychains -d user -s "$KC" $(security list-keychains -d user | tr -d '"')
      - name: build, sign and notarize
        env:
          ASUKA_APPLE_ID: ${{ secrets.APPLE_ID }}
          ASUKA_APPLE_PASSWORD: ${{ secrets.APPLE_APP_PASSWORD }}
          ASUKA_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
        run: asuka build --platform mac --sign "Developer ID Application: Your Name (TEAMID)" --notarize env
      - name: attach to the tag's release
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: |
          ditto -c -k --keepParent dist/Notes.app dist/Notes-mac.zip
          gh release upload "$GITHUB_REF_NAME" dist/Notes-mac.zip --clobber

  windows-linux:
    needs: release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: osslsigncode, and the code signing certificate
        env:
          CERTIFICATE: ${{ secrets.WINDOWS_CERTIFICATE }}
        run: |
          sudo apt-get install -y osslsigncode
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/cert.pfx
      - name: Windows, x64 and Arm
        env:
          ASUKA_WINDOWS_CERT: ${{ runner.temp }}/cert.pfx
          ASUKA_WINDOWS_CERT_PASSWORD: ${{ secrets.WINDOWS_CERTIFICATE_PASSWORD }}
        run: |
          asuka build --platform windows -o dist/x64
          asuka build --platform windows --arch arm64 -o dist/arm64
          (cd dist/x64 && zip -r ../Notes-windows-x64.zip Notes)
          (cd dist/arm64 && zip -r ../Notes-windows-arm64.zip Notes)
      - name: Linux, amd64 and arm64
        run: |
          asuka build --platform linux
          asuka build --platform linux --arch arm64
      - name: attach to the tag's release
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.zip dist/linux/*.tar.gz --clobber
```

これらのジョブが使うシークレットは次のとおりです。`MAC_CERTIFICATE` は Developer ID Application 証明書とその秘密鍵（`.p12`）、`MAC_CERTIFICATE_PASSWORD` はそのパスワードです。`APPLE_ID`、`APPLE_APP_PASSWORD`（account.apple.com で発行する App 用パスワード）、`APPLE_TEAM_ID` は公証サービスに使います。`WINDOWS_CERTIFICATE` はコード署名証明書（`.pfx`）、`WINDOWS_CERTIFICATE_PASSWORD` はそのパスワードです。Linux のビルドはそれぞれ自分の `.tar.gz` を作ります。一方、Flatpak のマニフェストがパッケージするのは最後にビルドしたもので、そのアーキテクチャ向けになります。

[自動アップデート](#自動アップデート)を GitHub で配信する場合は、`asuka build` の代わりに `asuka publish` を使うと、各プラットフォームのアップデートもタグのリリースの下書き（`release` ジョブが作ったもの）にアップロードされ、すべてのプラットフォームが揃ってから公開できます。秘密鍵をシークレットとして追加し（`gh secret set ASUKA_UPDATE_PRIVATE_KEY < asuka-update.key`）、ジョブに `ASUKA_UPDATE_PRIVATE_KEY` と `GH_TOKEN` を渡して、タグのバージョンでビルドします。

```yaml
      - name: build, sign, notarize, and upload with its update
        env:
          ASUKA_UPDATE_PRIVATE_KEY: ${{ secrets.ASUKA_UPDATE_PRIVATE_KEY }}
          GH_TOKEN: ${{ github.token }}
          ASUKA_APPLE_ID: ${{ secrets.APPLE_ID }}
          ASUKA_APPLE_PASSWORD: ${{ secrets.APPLE_APP_PASSWORD }}
          ASUKA_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
        run: asuka publish --platform mac --draft --version "${GITHUB_REF_NAME#v}" --sign "Developer ID Application: Your Name (TEAMID)" --notarize env
```

Windows と Linux のジョブでは次のようにします。

```yaml
        run: |
          for p in "windows" "windows --arch arm64" "linux" "linux --arch arm64"; do
            asuka publish --platform $p --draft --version "${GITHUB_REF_NAME#v}"
          done
```

最後に下書きを公開すると（`gh release edit v1.2.0 --draft=false`、またはリリースのページから）、インストール済みのアプリがそのバージョンに更新されます。release ジョブのタグは、`tagPrefix` にバージョンを続けたもの（`v1.2.0`）です。`--version` でビルドのバージョンをタグのバージョンに合わせるので、`asuka.json` のバージョンを上げる必要はありません。差分は公開済みのリリースをもとに作られ、ランナーがそのリリースをダウンロードします（`GH_TOKEN` があると、GitHub API のリクエスト数の制限が緩和されます。`tagPrefix` を使う場合は GitHub API を使います）。

## 言語

[カタログ](i18n.md)を持つアプリでは、ほかに何もする必要はありません。`asuka build` がアプリの言語を各ストアとシステムに宣言するので（iOS と macOS では `CFBundleLocalizations` と `.lproj`、Android では `locales_config`）、「設定」でアプリの言語を選べるようになります。Windows、Linux、Web のビルドは何も宣言しません。アプリは、ユーザーの設定に従ってカタログから選ばれた言語で表示されます。アプリの名前と利用目的の説明文も、カタログで `asuka.appName` や `asuka.NSCameraUsageDescription` といったキーを使って翻訳できます（[システムが表示するもの](i18n.md#システムが表示するもの)）。

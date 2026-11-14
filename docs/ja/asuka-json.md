# asuka.json

`asuka.json` はアプリのディレクトリに置くファイルで、プラットフォームとストアが知っておくべきアプリの情報を書きます。アプリの名前と ID、アイコン、アプリで開くリンクとファイル、署名の鍵などです。`asuka dev`、`asuka build`、`asuka publish` がこのファイルを読みます。`asuka dev` は起動時に読むので、変更したら一度止めて実行し直してください。

キーはすべて省略できます。このファイルがないアプリも、デフォルトの値でビルドして実行できます。Asuka が知らないキーは無視されるので、キーが効いていないように見えるときはつづりを確認してください。Asuka が受け付けられない値があると、ビルドは止まり、どのキーに何が求められているかを表示します（`asuka.json: accent "red" (want #rrggbb or #rrggbbaa)`）。パスはアプリのディレクトリからの相対パスです。

このページではすべてのキーを紹介します。リリースのためにキーをどう組み合わせるかは[配布](shipping.md#asukajson)で、各キーの働きの詳細は、下にリンクしたそれぞれのガイドで説明しています。

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "version": "1.2.0",
  "build": 7,
  "icon": "icon.png",
  "accent": "#7c3aed",
  "scheme": "notes",
  "links": ["notes.example.com"],
  "fileTypes": [{ "name": "Notes Document", "extensions": ["note"] }],
  "shareTarget": true,
  "splash": { "image": "logo.png", "background": "#ffffff", "darkBackground": "#000000" },
  "ios": {
    "team": "ABCDE12345",
    "usage": { "NSCameraUsageDescription": "To scan your receipts." }
  },
  "android": { "keystore": "upload.jks", "keyAlias": "upload", "dynamicColor": true },
  "linux": { "summary": "Write things down", "categories": ["Office"], "license": "MIT" },
  "updates": { "publicKey": "…", "github": "you/notes" }
}
```

## アプリ

これらのキーは、そのキーに意味があるすべてのプラットフォームで使われます。

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `name` | 文字列 | ディレクトリの名前 | アプリの名前です。アイコンの下、Mac のメニューバー、Windows のウィンドウのタイトル、Linux のランチャー、Web ページのタイトルに表示されます。ビルドで作られるファイルの名前にもなります（`Notes.app`、`Notes.exe`）。 |
| `id` | 文字列 | `dev.asuka.<directory>` | iOS と Mac では bundle ID、Android と Linux では application ID（Linux ではデスクトップファイル、アイコン、D-Bus の名前、データディレクトリに使われます）、Windows では AppUserModelID です。最初のリリースの前に変えてください。ストアとシステムはこの ID でアプリを識別します。Linux では英字、数字、`_`、`-` だけを使い、二つ以上の部分からなり、どの部分も数字で始まってはいけません。 |
| `version` | 文字列 | `1.0` | ユーザーに見えるバージョンです。 |
| `build` | 整数 | `1` | ビルド番号で、アップロードのたびに大きくする必要があります。`asuka publish` はストアにある最大の番号に 1 を足した番号を自動で付けるので、この値は開始の番号にすぎません。Windows のファイルバージョンは `version` と `build` を合わせたもの（`1.2.0.7`）になります。 |
| `icon` | パス | なし（プラットフォームの汎用アイコン） | 1024×1024 の正方形の PNG です。iOS 向けには透明部分を含めないでください。ビルドごとに、そのプラットフォームが必要とするサイズが作られます。App Store に出すには必須です。 |
| `accent` | 色 | プラットフォームの色 | `"#rrggbb"` または `"#rrggbbaa"`。iOS では tint、Android では Material 3 のカラースキームのシード、Mac では `Accent`、塗りつぶしのボタン、リンクの色（AppKit 自身のコントロールはユーザーの設定した色のまま）、Windows ではコントロールのアクセントカラー、Linux では libadwaita のアクセントカラー、Web ではページとマニフェストの `theme_color` になります。 |
| `orientations` | 文字列の配列 | 両方 | `"portrait"`、`"landscape"`。スマートフォンでアプリが対応する画面の向きです。iPad では Split View とステージマネージャに必要なので、四つの向きすべてに対応します。Android 16 では、タブレットと折りたたみ端末でこの指定は無視されます。 |
| `scheme` | 文字列 | なし | アプリの URL スキームです。iOS、Android、Mac、Windows、Linux で、`notes://` の URL がアプリを開き、`ui.OnOpenURL` がその URL を受け取ります。英字、数字、`+`、`-`、`.` を使い、先頭は英字にします。[ディープリンク](navigation.md#ディープリンク)を参照してください。 |
| `links` | 文字列の配列 | なし | `example.com` のようなドメインで、その https のリンクがアプリを開きます。iOS のユニバーサルリンクと Android の App Links です。ドメイン側で関連付けのファイルを配信します。[ディープリンク](navigation.md#ディープリンク)を参照してください。 |
| `fileTypes` | オブジェクトの配列 | なし | アプリが開くファイルの種類です。[fileTypes](#filetypes) を参照してください。 |
| `shareTarget` | `true` またはオブジェクト | オフ | iOS と Android で、ほかのアプリの共有シートにアプリを表示します。[shareTarget](#sharetarget) を参照してください。 |
| `splash` | オブジェクト | なし | iOS と Android の起動画面です。[splash](#splash) を参照してください。 |
| `widgets` | オブジェクトの配列 | なし | iOS と Android のホーム画面ウィジェットです。[widgets](#widgets) を参照してください。 |
| `shortcuts` | オブジェクトの配列 | なし | iOS と Android で、Siri、Spotlight、ランチャーに出すアクションです。[shortcuts](#shortcuts) を参照してください。 |
| `background` | 真偽値 | `false` | デスクトップで、`app.RunInBackground` と同じように、トレイに常駐するアプリにします。起動時にウィンドウを開かず、ウィンドウがなくても動き続け、Mac では起動時から Dock にアイコンを出しません（`LSUIElement`）。[バックグラウンドのアプリ](desktop.md#バックグラウンドで動くアプリ)を参照してください。 |
| `singleInstance` | 真偽値 | `true` | Windows と Linux で、二回目の起動は引数を実行中のアプリに渡して（`app.OnSecondInstance` に、ファイルは `ui.OnOpenFiles` に）終了します。`false` にすると、起動した回数だけアプリが実行されます。Mac ではどちらの場合もアプリは一つしか実行されません。[単一インスタンス](desktop.md#単一インスタンス)を参照してください。 |
| `i18n` | オブジェクト | `locales`（あれば） | アプリのカタログの場所です。[i18n](#i18n) を参照してください。 |
| `updates` | オブジェクト | なし | デスクトップアプリの署名付きアップデートです。[updates](#updates) を参照してください。 |
| `ios` | オブジェクト | | iOS のビルドの設定と、Mac のビルドの設定の一部です。[ios](#ios) を参照してください。 |
| `mac` | オブジェクト | | Mac のビルドのプロファイルです。[mac](#mac) を参照してください。 |
| `android` | オブジェクト | | Android のビルドの設定です。[android](#android) を参照してください。 |
| `linux` | オブジェクト | | Linux のビルドがアプリについて書き出す情報です。[linux](#linux) を参照してください。 |
| `web` | オブジェクト | | Web のビルドの設定です。[web](#web) を参照してください。 |

Windows のビルドに必要なのはこれだけです。署名の証明書は環境変数で渡します（[配布](shipping.md#windows)を参照）。

## fileTypes

アプリが開くファイルの種類です。ファイルマネージャー、メールの添付ファイル、「このアプリケーションで開く」から開かれたファイルは、`ui.OnOpenFiles` が受け取ります。それぞれの種類は次のようなオブジェクトです。

```json
"fileTypes": [
  { "name": "Markdown Document", "extensions": ["md", "markdown"], "role": "viewer" },
  { "name": "Notes Document", "extensions": ["note"], "icon": "note.png" }
]
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `extensions` | 文字列の配列 | 必須 | ファイル名の拡張子で、ドットは付けません（`"md"`、`"tar.gz"`）。 |
| `name` | 文字列 | 最初の拡張子を大文字にして「Document」を付けたもの | システムが表示するファイルの種類の名前です。 |
| `mimeTypes` | 文字列の配列 | Asuka がその拡張子について知っている MIME タイプ | ファイルの MIME タイプです。Asuka が知らない拡張子では、アプリ独自のタイプ `application/x-<app>-<extension>` になります。 |
| `uti` | 文字列 | 既知の拡張子ならシステムのもの、それ以外は `<id>.<extension>` | Apple のプラットフォームでの Uniform Type Identifier です。アプリの ID の下にある UTI はアプリ独自のタイプとして扱われ、エクスポートされ、アプリがそのファイルの所有者になります。それ以外の、システムが知らないかもしれない UTI はインポートされます。 |
| `conformsTo` | 文字列の配列 | `public.data`、`public.content` | エクスポートまたはインポートするタイプが準拠する UTI です。`public.plain-text` を指定すると、Quick Look とテキストエディターがそのファイルを扱えるようになります。 |
| `role` | 文字列 | `"editor"` | `"editor"` か `"viewer"`。アプリがそのファイルで何をするかを表します（Mac と iOS の `CFBundleTypeRole`）。 |
| `icon` | パス | システムのアイコン、またはアプリのアイコン | 正方形の PNG で、Mac、Windows、Linux でのファイルのアイコンになります。 |

ファイルの種類の宣言方法はプラットフォームごとに異なります。Mac と iOS では Info.plist の `CFBundleDocumentTypes`、Android ではインテントフィルター、Windows ではアプリの起動時にユーザーのレジストリ、Linux ではデスクトップファイルと shared-mime-info のパッケージ、Web ではマニフェストの `file_handlers` で宣言します。[配布](shipping.md#asukajson)と[ファイルを開く](desktop.md#ファイルを開く)を参照してください。

## shareTarget

iOS と Android で、ほかのアプリの共有シートにアプリを表示します。共有されたものは `ui.OnShare` が受け取ります。`true` にするとテキストと Web のリンクを受け取ります。ほかのものも受け取るときはオブジェクトで指定します。

```json
"shareTarget": { "types": ["text", "image/*", "video/*"], "max": 10 }
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `types` | 文字列の配列 | `["text"]` | `"text"`（テキストと Web のリンク）と、MIME タイプで指定するファイル（`"image/*"`、`"video/*"`、`"audio/*"`、`"application/pdf"`、何でも受け取るなら `"*/*"`）。 |
| `max` | 整数 | `1` | 一度に受け取るファイルの数で、1 以上です。 |

iOS では、アプリの中の共有拡張機能（share extension）になります。独自の bundle ID（アプリの ID に `.share` を付けたもの）を持ち、リリースでは `ios.shareProfile` で署名します。ファイルを受け取る場合は、App Group `group.<id>` も必要です。[配布](shipping.md#ios)と[ほかのアプリからの共有](device.md#ほかのアプリからの共有-uionshare)を参照してください。

## splash

iOS と Android の起動画面です。アイコンをタップしてからアプリの最初のフレームが出るまで（または `splash.Hide` を呼ぶまで）、単色の背景の中央に画像を表示します。デスクトップと Web には起動画面がありません。

```json
"splash": { "image": "logo.png", "size": 96, "background": "#4A63B5", "darkBackground": "#1B2440" }
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `image` | パス | なし | 正方形の PNG（ロゴ）。 |
| `size` | 整数 | `120` | 画像の幅をポイント（dp）で指定します。最大は 160 で、これが Android 12 のアイコンの円に収まる最大の大きさです。 |
| `background` | 色 | `"#ffffff"` | ライトモードでの背景色（`"#rrggbb"`）。 |
| `darkBackground` | 色 | `background` | ダークモードでの背景色（`"#rrggbb"`）。 |

[起動画面](device.md#起動画面-asukasdksplash)を参照してください。

## widgets

iOS と Android でのアプリのホーム画面ウィジェットです。中身はアプリの Go のコードから `widget.Set` で埋めます。

```json
"widgets": [{ "kind": "today", "name": "Today", "description": "What's next", "sizes": ["small", "medium", "rectangular"] }]
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `kind` | 文字列 | 必須 | `widget.Set` で使うウィジェットの名前です。英字、数字、`_` を使い、先頭は英字にします。ウィジェットごとに異なる名前にします。 |
| `name` | 文字列 | `kind` | ウィジェットギャラリーに表示される名前です。 |
| `description` | 文字列 | なし | ウィジェットギャラリーに表示される説明です。 |
| `sizes` | 文字列の配列 | `["small", "medium"]` | `small`、`medium`、`large`、そして iOS の `extraLarge`（iPad）と、ロック画面の `circular`、`rectangular`、`inline`。 |

iOS では、ウィジェットは独自の bundle ID（アプリの ID に `.widgets` を付けたもの）を持つ拡張機能になり、リリースでは `ios.widgetProfile` で署名します。[ホーム画面ウィジェット](device.md#ホーム画面ウィジェット-asukasdkwidget)を参照してください。

## shortcuts

システムがアプリのために表示するアクションで、最大 10 個です。iOS の App Shortcuts（Siri、Spotlight、ショートカットアプリ、アクションボタン）と、Android のランチャーのショートカットになります。どれも、`ui.OnOpenURL` に渡される URL でアプリを開きます。

```json
"shortcuts": [{ "id": "newNote", "title": "New Note", "url": "notes://new", "symbol": "square.and.pencil", "phrases": ["New note in ${app}"] }]
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `id` | 文字列 | 必須 | 英字、数字、`_` を使い、先頭は英字にします。ショートカットごとに異なる ID にします。 |
| `title` | 文字列 | 必須 | システムが表示する名前です。 |
| `url` | 文字列 | 必須 | アプリを開く URL です。`scheme` の URL か、`links` のどれかの URL にします。 |
| `symbol` | 文字列 | なし | `ui.Symbol` が受け取るのと同じ、SF Symbol の名前です。 |
| `phrases` | 文字列の配列 | `"<title> in ${app}"` | Siri が聞き取るフレーズで、どれにもアプリの名前を表す `${app}` を含めます。iOS だけで使われます。 |

[ショートカット](device.md#ショートカット-sirispotlightランチャー)を参照してください。

## i18n

アプリのメッセージカタログの場所です。カタログが `locales` ディレクトリにあるアプリには不要です。

```json
"i18n": { "dir": "translations", "source": "ja" }
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `dir` | パス | `locales` | カタログを入れるディレクトリです。カタログは言語ごとに一つで、BCP 47 のタグを名前にします（`en.json`、`pt-BR.json`、`zh-Hans.json`）。 |
| `source` | 文字列 | `en`、またはカタログが一つしかなければその言語 | アプリを書いている言語です。そのカタログにはすべてのメッセージがあり、ほかの言語のカタログはそれにフォールバックします。 |

[多言語対応](i18n.md#カタログ)を参照してください。

## updates

デスクトップアプリのアップデートの設定で、アップデートに署名する鍵と、アップデートを公開する場所を指定します。`asuka update keygen` が鍵ペアを作り、このオブジェクトを出力します。

```json
"updates": { "publicKey": "…the contents of asuka-update.pub…", "github": "you/notes" }
```

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `publicKey` | 文字列 | 必須 | `asuka-update.pub` の中身で、アプリに組み込まれます。アプリは、秘密鍵で署名されたアップデートしか受け付けません。 |
| `github` | 文字列 | | `"owner/name"`。アップデートを置く公開リポジトリで、`v1.2.0` のようなタグを付けたリリースにアップデートを入れます。 |
| `tagPrefix` | 文字列 | `"v"` | リリースのタグの接頭辞です。`"desktop-v"` のように別の接頭辞にすると、リポジトリの最新リリースが別のもののリリースであってもかまいません。その場合、アプリは GitHub API を通じて、この接頭辞のタグを持つ最新のリリースを探します。 |
| `url` | 文字列 | | `github` の代わりに、ファイルを配信する HTTPS のディレクトリ（S3 や R2 のバケット、CDN）を指定します。アプリは `<url>/update-<target>.json` を読みます。 |
| `feed` | 文字列 | なし | アプリが代わりに問い合わせる独自の HTTPS サーバーで、`{target}`、`{version}`、`{channel}` を含む URL で指定します。ファイルの置き場所として `url` も必要です。 |
| `s3` | オブジェクト | なし | `url` を配信するバケットで、`asuka publish` がここにアップロードします。`bucket`（必須）、`prefix`（ディレクトリ。デフォルトはバケットのルート）、`region`（デフォルトは `AWS_REGION`、なければ `AWS_DEFAULT_REGION`、それもなければ `us-east-1`）、`endpoint`（R2 や MinIO のもの。デフォルトは Amazon S3）、`pathStyle`（デフォルトでは、`endpoint` を指定したとき、またはバケット名にドットを含むときに有効）を指定します。 |
| `privateKey` | パス | なし | 秘密鍵のパスで、自分のマシンでビルドするときに使います（`~/` はホームディレクトリです）。 |
| `changelog` | パス | `CHANGELOG.md`（あれば） | Markdown のファイルで、その `## 1.2.0` のセクションがリリースノートになります。 |
| `deltas` | 整数 | `3` | 差分アップデート（変更された部分だけをダウンロードするアップデート）を提供する、さかのぼるバージョンの数です。`0` なら提供しません。 |

`github` と `url` のどちらか一方が必要で、両方は指定できません。[アップデート](shipping.md#自動アップデート)を参照してください。

## ios

iOS のビルドの設定です。`team` は Mac のビルドのチームにもなり、`usage` の利用目的の文字列は Mac の Info.plist にも入ります。

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `team` | 文字列 | なし | アプリに署名する Apple Developer のチーム ID（`ABCDE12345`）です。 |
| `profile` | 文字列 | インストール済みのうち最も合うもの | アプリのプロビジョニングプロファイルで、パス、名前、UUID のいずれかで指定します。`--profile` で上書きできます。 |
| `shareProfile` | 文字列 | インストール済みのうち最も合うもの | 共有拡張機能（`shareTarget`）のプロファイルで、ID `<id>.share` 用です。 |
| `widgetProfile` | 文字列 | インストール済みのうち最も合うもの | ウィジェット拡張機能（`widgets`）のプロファイルで、ID `<id>.widgets` 用です。 |
| `notificationProfile` | 文字列 | インストール済みのうち最も合うもの | Notification Service Extension（`push.Rewrite`）のプロファイルで、ID `<id>.notification` 用です。 |
| `usage` | オブジェクト | 各パッケージのもの | 保護されたリソースの利用目的の文字列を、Info.plist のキーごとに指定します（`{"NSCameraUsageDescription": "To scan your receipts."}`）。アプリがインポートするパッケージが汎用的な文字列を用意していますが、自分で書いてください。 |
| `infoPlist` | オブジェクト | なし | Info.plist に追加する文字列のキーです。 |
| `backgroundModes` | 文字列の配列 | なし | Info.plist の `UIBackgroundModes` に追加します。`"audio"` を指定すると、画面がロックされていても録音と再生ができます。 |
| `iphoneOnly` | 真偽値 | `false` | iPad を対象から外します。iPad ではアプリは iPhone アプリとして、スマートフォンサイズのウィンドウで動きます。 |
| `nonExemptEncryption` | 文字列 | `"no"` | ビルドが暗号化について申告する内容（`ITSAppUsesNonExemptEncryption`）です。これは法的な申告で、内容は開発者が判断します。`"no"`、`"yes"`、または何も申告しない `"ask"` を指定します。`"ask"` の場合は、アップロードのたびに App Store Connect が尋ねます。 |
| `collected` | オブジェクトの配列 | なし | アプリが収集するデータで、プライバシーマニフェストに使います（下記を参照）。 |
| `appStoreConnect` | オブジェクト | | `asuka publish` がアップロードに使う App Store Connect API キーです。`keyId`、`issuerId`（個人キーでは不要）、`key`（`.p8` のパス。デフォルトは、`altool` と同じく `~/.appstoreconnect/private_keys` にある `AuthKey_<keyId>.p8`）を指定します。[公開](shipping.md#ストアへの公開)を参照してください。 |

`collected` の各要素はデータの種類で、Apple の名前から接頭辞を除いたもので指定します。これは Asuka が書き出すプライバシーマニフェストに使われます（アプリ独自の `ios/PrivacyInfo.xcprivacy` があれば、Asuka のマニフェストとこの設定はそれに置き換わります）。

```json
"collected": [{ "type": "EmailAddress", "linked": true, "tracking": false, "purposes": ["AppFunctionality"] }]
```

| キー | 型 | 説明 |
|---|---|---|
| `type` | 文字列 | `"EmailAddress"` は `NSPrivacyCollectedDataTypeEmailAddress` を表します。 |
| `linked` | 真偽値 | ユーザーの身元と結び付けられるかどうか。 |
| `tracking` | 真偽値 | ユーザーのトラッキングに使われるかどうか。 |
| `purposes` | 文字列の配列 | `"AppFunctionality"` は `NSPrivacyCollectedDataTypePurposeAppFunctionality` を表します。 |

[配布](shipping.md#ios)を参照してください。

## mac

Mac のビルドがアプリの署名に使うものです。チームは `ios.team` です。

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `profile` | 文字列 | `ASUKA_MAC_PROFILE`。なければ、プロファイルが必要なアプリではインストール済みのうち最も合うもの | Developer ID（または開発用）のプロビジョニングプロファイルのパス、名前、UUID です。プッシュ通知、Sign in with Apple、App Attest に必要です。 |
| `appStoreProfile` | 文字列 | インストール済みのうち最も合うもの | `--appstore` で使う Mac App Store のプロファイルです。 |

`--profile` と `ASUKA_MAC_PROFILE` で上書きできます。[配布](shipping.md#macos)を参照してください。

## android

Android のビルドの設定です。

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `keystore` | パス | なし | リリースに署名するアップロード鍵のキーストアです。キーストアのパスワードは `ASUKA_KEYSTORE_PASSWORD`、鍵のパスワードは `ASUKA_KEY_PASSWORD`（デフォルトはキーストアと同じ）で渡します。 |
| `keyAlias` | 文字列 | なし | キーストアの中のアップロード鍵のエイリアスです。 |
| `dynamicColor` | 真偽値 | `false` | Material のアプリと同じように、ユーザーの壁紙から色を取ります（Android 12 以降）。それ以外の環境では `accent` がシードになります。 |
| `permissions` | 文字列の配列 | 各パッケージのもの | マニフェストに追加する権限です（`"android.permission.CAMERA"`）。`"<name>:28"` と書くと、Android 9（API 28）までだけ権限を要求します。アプリがインポートするパッケージは、必要な権限を自分で追加します。 |
| `googleServices` | パス | なし | `asuka/sdk/push` で使う、Firebase プロジェクトの `google-services.json` です。 |
| `mapsKey` | 文字列 | なし | `asuka/sdk/maps` で使う、Google Maps Platform の API キー（Maps SDK for Android）です。 |
| `serviceAccount` | パス | なし | `asuka publish` が Google Play へのアップロードに使う、Google Cloud のサービスアカウントの JSON キーです。リポジトリには入れないでください。 |

[配布](shipping.md#android)を参照してください。

## linux

Linux のビルドが、デスクトップファイルと AppStream の metainfo に書き出すアプリの情報です。ランチャー、ソフトウェアセンター、Flathub に表示されます。Flathub とソフトウェアセンターは、概要、説明、ライセンス、開発者を求めます。

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `summary` | 文字列 | なし | アプリが何をするかを一行で書きます。 |
| `description` | 文字列 | なし | 空行で区切った数段落の説明です。 |
| `categories` | 文字列の配列 | `["Utility"]` | [freedesktop.org のメインカテゴリー](https://specifications.freedesktop.org/menu-spec/latest/category-registry.html)：`Utility`、`Office`、`Development`、`Network` など。 |
| `keywords` | 文字列の配列 | なし | ランチャーでアプリを見つけるための追加のキーワードです。 |
| `license` | 文字列 | なし | アプリのライセンスを SPDX 式で書きます（`MIT`、`GPL-3.0-or-later`、`LicenseRef-proprietary`）。 |
| `developer` | 文字列 | なし | 開発者の名前です。 |
| `homepage` | 文字列 | なし | アプリの Web ページです。 |
| `flatpakRuntime` | 文字列 | 最近のもの | Flatpak のマニフェストが要求する GNOME ランタイムのバージョンで、46 以降です。 |

[配布](shipping.md#linux)を参照してください。

## web

Web のビルドの設定です。

| キー | 型 | デフォルト | 説明 |
|---|---|---|---|
| `vapidPublicKey` | 文字列 | なし | アプリのサーバーの VAPID 公開鍵（base64url）です。ブラウザーは、`asuka/sdk/push` の Web Push を購読するときにこれを使います。 |

[Web Push](web.md#asukasdkpushweb-push) を参照してください。

## 環境変数

秘密の情報は `asuka.json` に書かず、環境変数で渡してください。CI では CI のシークレットから渡します。これらの環境変数は、対応するキーを上書きもします。

| 変数 | 説明 |
|---|---|
| `ASUKA_KEYSTORE`、`ASUKA_KEY_ALIAS` | `android.keystore` と `android.keyAlias` を上書きします。 |
| `ASUKA_KEYSTORE_PASSWORD`、`ASUKA_KEY_PASSWORD` | キーストアのパスワードと、それと異なる場合の鍵のパスワードです。 |
| `ASUKA_ASC_KEY_ID`、`ASUKA_ASC_ISSUER_ID`、`ASUKA_ASC_KEY` | `ios.appStoreConnect` を上書きします。`ASUKA_ASC_KEY` は `.p8` のパスかその中身です。 |
| `ASUKA_PLAY_SERVICE_ACCOUNT` | `android.serviceAccount` を上書きします。キーのパスか、その JSON です。 |
| `ASUKA_MAC_PROFILE` | `mac.profile` を上書きします。 |
| `ASUKA_UPDATE_PUBLIC_KEY`、`ASUKA_UPDATE_URL` | `updates.publicKey` と `updates.url` を上書きします（`url` を指定すると `github` は使われなくなります）。 |
| `ASUKA_UPDATE_PRIVATE_KEY` | アップデートの秘密鍵そのもの、またはそのパスで、`updates.privateKey` を上書きします。 |
| `ASUKA_WINDOWS_CERT`、`ASUKA_WINDOWS_CERT_PASSWORD`、`ASUKA_WINDOWS_TIMESTAMP` | Windows のビルドの署名証明書（`.pfx`）、そのパスワード、タイムスタンプサーバーです。これらは `asuka.json` には書けません。[配布](shipping.md#windows)を参照してください。 |

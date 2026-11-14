# デバイスとサービス

Asuka のパッケージを使うと、ビューの外にあるデバイスの機能も扱えます。カメラ、バーコードや QR コード、画像に写っているものの認識、写真とファイル、プレビュー、印刷と PDF、メールとメッセージの作成画面、位置情報、通知とプッシュ通知、サウンド、再生中の情報（Now Playing）と録音、ネットワークの状態、写真やファイルのライブラリへの保存、バックグラウンドでの処理、ほかのアプリからの共有、JavaScript、サインインと生体認証、画面、地図、ストアのサービス、クラッシュレポートです。このページでは、パッケージごとに何ができるか、その API、必要になる権限、プラットフォームごとの動作を説明します。

どのパッケージも、すべてのプラットフォームで同じ API を持ちます。iOS と Android ではほぼすべての機能が使えます。macOS、Windows、Linux では、ホストが各パッケージをデスクトップ自身のサービスで実装し、ブラウザーでは Web の API で実装します。プラットフォームに対応する機能がない場合、パッケージはその機能を持たないスマートフォンと同じように振る舞います。つまり `ErrNotAvailable` を返すか、足りないものを示すエラーを返すか、何もしない呼び出しになります。各パッケージの節では、何ができるか、各プラットフォームが何を使ってそれを実現しているかを説明します。さらに、各デスクトップとブラウザーにはそれぞれ専用の節があり（[Mac の場合](#mac-の場合)、[Windows の場合](#windows-の場合)、[Linux の場合](#linux-の場合)、[ブラウザーの場合](#ブラウザーの場合)）、そこで必要な設定と、iOS や Android との違いを表にまとめています。

## 権限と利用目的の説明文

アプリがパッケージをインポートすると、そのパッケージに必要なものが自動で加わります。Android ではマニフェストの権限、iOS では Info.plist の利用目的の説明文（purpose string）、そしてプラットフォーム側のコードそのものです。インポートしていないパッケージのものは加わらないので、写真アプリがマイクの許可を求めることはありません。Mac アプリにも同じ説明文と、それに必要なエンタイトルメントが加わります。Windows、Linux、Web では何も宣言する必要はありません（下のそれぞれの節を参照してください）。

| パッケージ | Android | iOS |
| --- | --- | --- |
| `asuka/sdk/camera` | `CAMERA` | `NSCameraUsageDescription` |
| `asuka/sdk/camera/record` | `RECORD_AUDIO` | `NSMicrophoneUsageDescription` |
| `asuka/sdk/location` | `ACCESS_COARSE_LOCATION`、`ACCESS_FINE_LOCATION` | `NSLocationWhenInUseUsageDescription` |
| `asuka/sdk/notify` | `POST_NOTIFICATIONS` | |
| `asuka/sdk/network` | `ACCESS_NETWORK_STATE` | |
| `asuka/sdk/library` | `WRITE_EXTERNAL_STORAGE`（Android 9 以前） | `NSPhotoLibraryAddUsageDescription`。アプリの Documents を「ファイル」アプリに表示 |
| `asuka/sdk/background` | `FOREGROUND_SERVICE`、`FOREGROUND_SERVICE_DATA_SYNC`、`POST_NOTIFICATIONS`、`WAKE_LOCK`。フォアグラウンドサービス | |
| `asuka/sdk/scan` | ML Kit のバーコードモデル（約 2 MB） | |
| `asuka/sdk/vision` | ML Kit のテキスト、顔、バーコードのモデル（約 30 MB） | |
| `asuka/sdk/photos` | `READ_MEDIA_IMAGES`、`READ_MEDIA_VISUAL_USER_SELECTED`。`READ_EXTERNAL_STORAGE`（Android 12 以前） | `NSPhotoLibraryUsageDescription` |
| `asuka/sdk/push` | Firebase Cloud Messaging、`POST_NOTIFICATIONS` | `aps-environment` エンタイトルメント。`remote-notification` バックグラウンドモード |
| `asuka/sdk/appleauth` | | Sign in with Apple のエンタイトルメント |
| `asuka/sdk/biometric` | `USE_BIOMETRIC` | `NSFaceIDUsageDescription` |
| `asuka/sdk/audio/record` | `RECORD_AUDIO`、`FOREGROUND_SERVICE_MICROPHONE`。フォアグラウンドサービス | `NSMicrophoneUsageDescription` |
| `asuka/sdk/nowplaying` | `FOREGROUND_SERVICE`、`FOREGROUND_SERVICE_MEDIA_PLAYBACK`。フォアグラウンドサービス | `audio` バックグラウンドモード（`nowplaying.Show` を呼ぶアプリの場合） |
| `asuka/sdk/liveactivity` | `POST_NOTIFICATIONS`、`POST_PROMOTED_NOTIFICATIONS` | `NSSupportsLiveActivities` とウィジェット拡張（呼び出すアプリの場合） |
| `asuka/sdk/transcribe` | `RECORD_AUDIO` | `NSSpeechRecognitionUsageDescription` |
| `asuka/sdk/screen` | `DETECT_SCREEN_CAPTURE` | |
| `asuka/sdk/tracking` | | `NSUserTrackingUsageDescription` |
| `asuka/sdk/review` | Play のアプリ内レビューライブラリ | |
| `asuka/sdk/integrity` | Play Integrity | App Attest のエンタイトルメント |
| `asuka/sdk/agerange` | Play Age Signals | Declared Age Range のエンタイトルメント |
| `asuka/sdk/maps` | Google の Maps SDK | |
| `asuka/sdk/periodic` | WorkManager | `fetch` と `processing` バックグラウンドモード。タスクの識別子 |
| `asuka/sdk/sensors` | `ACTIVITY_RECOGNITION`（歩数計） | `NSMotionUsageDescription`（歩数計） |
| `asuka/sdk/speech` | 音声合成エンジンの可視性（`<queries>`） | |
| `asuka/sdk/contacts` | `READ_CONTACTS`（`List` 用） | `NSContactsUsageDescription` |
| `asuka/sdk/calendar` | `READ_CALENDAR`（`Calendars`、`Events` 用） | `NSCalendarsFullAccessUsageDescription`、`NSCalendarsUsageDescription` |
| `asuka/sdk/compose` | `mailto:` と `smsto:` を扱うアプリの可視性（`<queries>`） | `LSApplicationQueriesSchemes` に `mailto` |

`asuka/sdk/device`、`asuka/sdk/audio`、`asuka/sdk/splash`、`asuka/sdk/imageedit`、`asuka/sdk/widget`、`asuka/sdk/js`、`asuka/sdk/secure`、`asuka/sdk/preview`、`asuka/sdk/print` は何も必要としません。

リリースビルドでエンタイトルメントを使うには、アプリの App ID（developer.apple.com の Identifiers）でそのケイパビリティ（capability）を有効にしておく必要があります。そうすれば、プロビジョニングプロファイルにもそのケイパビリティが含まれます。プロファイルに足りないケイパビリティがあると、`asuka build --platform ios` は何が足りないかを示して止まります。シミュレーター向けのビルドにはすべてが含まれます。

Asuka が書き込む利用目的の説明文は汎用的なもの（"To take a photo."）です。iOS と macOS はユーザーに許可を求めるときにこの文を表示するので、`asuka.json` の `ios.usage` に独自の文を書いておきましょう。

```json
{
  "ios": {
    "usage": {
      "NSCameraUsageDescription": "To photograph your receipts.",
      "NSLocationWhenInUseUsageDescription": "To find stores near you."
    }
  }
}
```

各パッケージがユーザーに許可を求めるのは、起動時ではなく、初めてその機能が必要になったときです。何かを待つ呼び出し（ユーザーの操作、位置情報の測位、プラットフォームの処理）には、以下で「メインスレッド以外から呼ぶ」と書いてあります。これらはゴルーチンから呼び、結果を `ui.Post` でメインスレッドに戻してください（[メインスレッド](concepts.md#メインスレッド)を参照）。それ以外の呼び出しはコールバックを受け取り、そのコールバックはメインスレッドで呼ばれます。

## Mac の場合

Mac アプリ（`asuka dev` と `asuka build`）の Info.plist にも同じ利用目的の説明文が入ります（位置情報の説明文は `NSLocationUsageDescription` としても入ります）。アプリの署名には、許可を求める機能（カメラ、マイク、位置情報、連絡先、カレンダー、写真）に対応する Hardened Runtime のエンタイトルメントが含まれます。これがないと、Developer ID で署名したビルドはその機能へのアクセスを拒否されます。プッシュ通知の `aps-environment`、Sign in with Apple、App Attest は、プロビジョニングプロファイルでしか付与されないエンタイトルメントです。これらを使うアプリには、`asuka build` がプロファイルを埋め込みます（`asuka.json` の `mac.profile`、なければ `ASUKA_MAC_PROFILE`、それもなければインストール済みのプロファイルからもっとも合うもの）。プロファイルがなくてもアプリはビルドでき、CLI が何が動かないかを知らせます。その場合 Mac では、プッシュ通知の登録と Sign in with Apple が失敗し、App Attest は非対応と答えます。`asuka dev` はプロファイルなしでアドホック署名するので、同じことが当てはまります。Mac App Store 向けのビルド（`--appstore`）はサンドボックス化され、ネットワークとユーザーが選んだファイルにアクセスできます。[配布](shipping.md)を参照してください。

macOS でも各パッケージは iOS と同じフレームワーク（AVFoundation、PhotoKit、Core Location、MapKit、Vision、Speech、UserNotifications、AuthenticationServices）を通じて、iOS と同じように動きます。Mac で異なる点は次のとおりです。

| パッケージ | Mac の場合 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` と `PickFiles`：ウィンドウ上のシートとして開くオープンパネル（権限は不要）で、アプリのキャッシュにコピーを作ります。`Browse` はユーザーのブラウザーでページを開きます。`GetInfo`：`Model` は `"Mac"`、`System` は `"macOS"` |
| `asuka/sdk/camera` | `Take`：ウィンドウの上にシートを出し、ライブプレビューと「写真を撮る」「キャンセル」を表示します（AppKit にはカメラ画面がないため）。`View` の「背面」は内蔵カメラ（Mac には背面カメラがないため）か、接続されたカメラです。そこでコードを読み取るには macOS 13 が必要です |
| `asuka/sdk/audio/record` | オーディオセッションはありません。ほかのアプリの音は鳴り続けます。`Background` は何も変えません。Mac アプリはどのみち見えていなくても録音できます |
| `asuka/sdk/nowplaying` | iOS と同じく `MPNowPlayingInfoCenter` と `MPRemoteCommandCenter` を使います。コントロールセンターとメニューバーの「再生中」、キーボードのメディアキー、AirPods で操作できます。Mac ではオーディオセッションもバックグラウンドモードも不要です |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | `Add` はウィンドウ上のアラートで確認してから保存します（macOS には新規連絡先や新規イベントの画面がないため）。保存の前にアクセスの許可を求めます |
| `asuka/sdk/photos` | iOS と同じく PhotoKit です。Mac には限定アクセスのライブラリがないので、`ManageLimited` は何もしません |
| `asuka/sdk/library` | 写真とビデオは「写真」アプリに保存されます。`Downloads` と `Music` は、ユーザーの `~/Downloads` と `~/Music` の中の指定したフォルダーに保存されます |
| `asuka/sdk/biometric` | Touch ID のプロンプト、またはユーザーのパスワード（`NoPasscode` のときを除く） |
| `asuka/sdk/push` | iOS と同じく APNs です。`Rewrite` はありません（通知サービス拡張が必要なため）。プロビジョニングプロファイルが必要です（上記） |
| `asuka/sdk/appleauth` | iOS と同じく Apple のボタンとシートです。プロビジョニングプロファイルが必要です（上記） |
| `asuka/sdk/integrity` | iOS と同じく App Attest です。プロビジョニングプロファイルが必要です（上記）。ない場合 `Supported` は false です |
| `asuka/sdk/background` | タスクの実行中は App Nap によってアプリが遅くなるのを防ぎます。通知は出ないので、`Update` は何も変えません |
| `asuka/sdk/periodic` | `NSBackgroundActivityScheduler` を使います。タスクはアプリの実行中に動きます（タスクのためにアプリが起動されることはありません）。電源を必要とするタスクは、Mac がバッテリーで動いているあいだ待ちます |
| `asuka/sdk/screen` | `Lock` と `SetBars` は何もしません。`PreventCapture` はアプリのウィンドウをスクリーンショットと画面収録から除外します。`OnScreenshot` は Spotlight が見つけたスクリーンショットを通知します |
| `asuka/sdk/transcribe` | デバイス上での認識のみです。アプリのバンドルに利用目的の説明文が必要で、`asuka build` がそれを書き込みます |
| `asuka/sdk/sensors` | Mac にはモーションセンサーがありません。`Available` は false で、監視しても何も届かず、`StepsBetween` は `ErrNotAvailable` で失敗します |
| `ui.Haptic` | トラックパッドのフィードバック（感圧タッチトラックパッド）です。`HapticSelection` では整列（alignment）、それ以外ではレベル変更（level change）のフィードバックになります |
| `asuka/sdk/widget`、`asuka/sdk/liveactivity`、`asuka/sdk/agerange` | Mac にはありません。`widget.Set` は何もせず、`liveactivity.Start` と `agerange.Request` は `ErrNotAvailable` で失敗します |
| `asuka/sdk/splash` | Mac アプリには起動画面がないので、`Hold` と `Hide` は何もしません |
| `ui.OnShare`、`app.ShareSheet`、ショートカット | iOS と Android だけです。Mac アプリはほかのアプリの共有メニューに表示されず、`asuka.json` の `shortcuts` も追加されません |

## Windows の場合

Windows アプリは何も宣言しません。`asuka build --platform windows` はケイパビリティを書き込まず、Windows はデスクトップアプリに権限を求めません。ただし「設定」の「プライバシーとセキュリティ」にあるスイッチは、カメラ、マイク、位置情報に対して有効です。オフになっていると、カメラと録音は `"not allowed"` で、位置情報は `location.ErrDenied` で失敗します。

Windows では、各パッケージは Windows 自身のサービス（Media Foundation、WIC、WinRT、Windows Hello、SAPI、資格情報マネージャー）を通じて動きます。Windows で異なる点は次のとおりです。Windows に対応する機能がない場合、パッケージはその機能を持たないデバイスと同じように振る舞います。

| パッケージ | Windows の場合 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` と `PickFiles`：ウィンドウの上に開くファイルを開くダイアログで、アプリのキャッシュにコピーを作ります。`Browse` はユーザーのブラウザーでページを開きます。クリップボードの画像は PNG です。`KeepAwake` はディスプレイをオンのままにします。`GetInfo`：`Model` は `"PC"`、`System` は `"Windows"`、`SystemVersion` は `"10.0.<build>"` |
| `asuka/sdk/secure` | 資格情報マネージャーにある、ユーザーの汎用資格情報です。名前は `<app>/<key>` です（値は 2560 バイトまで） |
| `asuka/sdk/camera` | Media Foundation のキャプチャエンジンを使います。`Take`：ウィンドウの上にダイアログを出し、ライブプレビューと「写真を撮る」「キャンセル」を表示します（Enter と Escape でも操作できます）。`View` は Windows が前面または背面と呼ぶカメラを選び、なければ最初のカメラを使います。`Scan` を指定すると、Asuka 独自のリーダー（`asuka/sdk/scan` の `File` と同じもの）で毎秒 4 フレームからコードを読み取ります。「設定」でカメラのスイッチがオフのときは "not allowed" です |
| `asuka/sdk/camera/record` | MPEG-4（`.mp4`）の H.264 と AAC です。ミュートされていなければ既定のマイクを使います |
| `asuka/sdk/audio` | MCI（DirectShow の再生）を使います。URL は先にアプリのキャッシュにダウンロードされます |
| `asuka/sdk/audio/record` | 既定のマイクを使い、AAC 44.1 kHz で録音します（音声はモノラル、音楽はステレオ）。`Background` は何も変えません |
| `asuka/sdk/nowplaying` | アプリのウィンドウの SystemMediaTransportControls を使います（ウィンドウのないアプリには専用の隠しウィンドウを用意します）。Windows 10 では音量のポップアップ、Windows 11 ではクイック設定のメディアコントロール、ロック画面、キーボードのメディアキーに表示されます。スキップは早送りと巻き戻しのボタンになります。トグルはありません（再生/一時停止キーは再生か一時停止のどちらかを送ります） |
| `asuka/sdk/speech` | SAPI の音声のうち、インストールされているもの（「設定」の「時刻と言語」>「音声認識」） |
| `asuka/sdk/network` | Network List Manager を使います。Wi-Fi、モバイルデータ通信、有線のいずれかです。コストのかかる接続は `Metered`、データ上限に近いか超えている接続は `Constrained` です |
| `asuka/sdk/location` | WinRT の Geolocator を使います。`PlaceName` は OpenStreetMap の Nominatim に問い合わせます（Windows 自身のジオコーダーには Bing Maps のキーが必要なため）。リクエストは最大で毎秒 1 回で、結果はアプリのキャッシュに保存されます |
| `asuka/sdk/biometric` | Windows Hello のプロンプトです。`Check` が返す種類はセンサーのものです。Hello は常に PIN の入力も選択肢に出し、キャンセルボタンの名前も Hello 自身が決めます |
| `asuka/sdk/webauth` | アプリのウィンドウの上に、WebView2 を入れたウィンドウを開きます（アプリの Web プロファイルを使います。`Ephemeral` では使い捨てのプロファイル） |
| `asuka/sdk/js` | 一度も表示されない WebView2 |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | Windows はパッケージ化されたアプリにしか連絡先とカレンダーを渡しません。`Request` は拒否と答えます。`Add` は vCard かイベントのファイルを、ユーザーの連絡先アプリやカレンダーアプリで開きます（保存されたかどうかは分からないので、`saved` は false です） |
| `asuka/sdk/photos` | ピクチャフォルダーです（求める権限はありません）。スクリーンショットはその中の Screenshots フォルダーのものです |
| `asuka/sdk/library` | ユーザーのダウンロード、ビデオ、ミュージック、ピクチャフォルダーの、指定したフォルダーに保存されます |
| `asuka/sdk/vision` | Windows の OCR（OCR がインストールされている言語のみ。「設定」の「時刻と言語」）と顔検出を使います。コードは `asuka/sdk/scan` と同じ方法で読み取ります |
| `asuka/sdk/scan` | Windows にはアプリ向けのコードリーダーがありません。そのため Go で書かれた Asuka 独自のリーダーが、QR コード、Data Matrix、EAN-13 と EAN-8、UPC-A と UPC-E、Code 128、Code 39 を読み取ります（Aztec、PDF417、Code 93、Codabar、ITF は読めません） |
| `asuka/sdk/imageedit` | Windows のコーデックで読み込みます（HEIC と WebP は、それぞれの拡張機能がインストールされている場合）。ビデオのフレームは Media Foundation で取り出します |
| `asuka/sdk/maps` | Windows にはデスクトップアプリ向けの地図コントロールがありません（Windows 自身のものには Bing Maps のキーが必要です）。そのためホストが OpenStreetMap のタイルを描きます。タイルは 2 枚ずつダウンロードされ、アプリのキャッシュに 1 週間保存されます。地図には「© OpenStreetMap contributors」が表示されます。ドラッグ、ホイール、ピンチ、ダブルクリック、+ と - で操作できます。Satellite と Hybrid は道路地図を表示し（無料の航空写真がないため）、`ShowsUser` は Linux と同じく何も表示しません |
| `asuka/sdk/background` | タスクの実行中は PC がスリープしないようにします。通知は出ないので、`Update` は何も変えません |
| `asuka/sdk/periodic` | アプリの実行中にタイマーで動きます（タスクのためにアプリが起動されることはありません）。電源を必要とするタスクは、PC がバッテリーで動いているあいだ待ちます |
| `asuka/sdk/screen` | `Lock` と `SetBars` は何もしません。`PreventCapture` はアプリのウィンドウをスクリーンショットと画面録画から除外します。`OnScreenshot` は Screenshots フォルダーに保存されたスクリーンショット（Windows キー + Print Screen、Snipping Tool）を通知します |
| `asuka/sdk/tracking` | プロンプトは出ません。「設定」で広告 ID がオンなら許可、オフなら拒否です |
| `asuka/sdk/push`、`asuka/sdk/widget`、`asuka/sdk/review`、`asuka/sdk/integrity` | これらはパッケージ化された（ストアの）アプリか、Apple や Google のものです。`push.Register` は失敗し、`widget.Set` は何もせず、`review.Request` は何も表示せず、`integrity.Supported` は false です |
| `asuka/sdk/liveactivity` | スマートフォンの機能です。`Enabled` は false で、`Start` は `ErrNotAvailable` で失敗します |
| `asuka/sdk/appleauth`、`asuka/sdk/agerange`、`asuka/sdk/transcribe`、`asuka/sdk/sensors` | Windows にはありません。その機能を持たないデバイスと同じく、利用できません |
| `ui.Haptic` | PC には触覚フィードバックがないので、何もしません |
| `asuka/sdk/splash` | Windows アプリには起動画面がないので、`Hold` と `Hide` は何もしません |
| `ui.OnShare`、`app.ShareSheet`、ショートカット | iOS と Android だけです |

## Linux の場合

Linux アプリも何も宣言しません。ホストが XDG デスクトップポータルを通す機能（ファイル、カメラ、位置情報、通知、メール、印刷、ファイルやリンクを開く、システムをスリープさせない）では、ポータルがいつもどおりユーザーに確認します。これは Flatpak のサンドボックスの中でも外でも同じように動きます。`asuka build --platform linux` が書き出す Flatpak マニフェストで、アプリがアクセスできるのはディスプレイ、GPU、ネットワーク、ポータルだけです。ホストがポータルを通さずに使うサービス（Speech Dispatcher、連絡先とカレンダーの Evolution Data Server、接続の種類を調べる NetworkManager、iio-sensor-proxy、Video4Linux のカメラ）を使うには、それらへのアクセスを開く `finish-args` をマニフェストに追加する必要があります。追加しない場合、パッケージはそのサービスがないシステムと同じように振る舞います。後で届く通知にはサンドボックスの外の systemd が必要です。Flatpak の中では、アプリの実行中にしか表示されません（[プラットフォームごとの動作](#プラットフォームごとの動作)を参照）。

Linux のホストは、ポータルがあればポータルを通じて、ほかに GLib と GTK、そしてデスクトップにあるライブラリを通じて、デスクトップのサービスを使います。各ライブラリは初めて使うときに読み込まれます。ライブラリがないシステムでは、足りないものを示すエラーが返ります。

| パッケージ | Linux の場合 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` と `PickFiles`：GTK のファイルダイアログ（サンドボックス内ではポータルのもの）で、アプリのキャッシュにコピーを作ります。クリップボードの画像と変更は GDK のクリップボードを通じて扱います。`Browse` はユーザーのブラウザーでページを開きます。`KeepAwake` はセッションがアイドル状態になるのを防ぎます。`GetInfo`：`Model` は `"PC"`、`System` は `"Linux"`、`SystemVersion` はディストリビューションのバージョン（`/etc/os-release`） |
| `asuka/sdk/secure` | キーリング（libsecret：GNOME Keyring、KWallet、KeePassXC）に、キーごとに 1 つ、アプリ名のラベルを付けた項目を作ります。libsecret やキーリングがなければ、呼び出しはその旨を示して失敗します |
| `asuka/sdk/network` | GNetworkMonitor（サンドボックス内ではポータルのもの）を使います。Wi-Fi、Ethernet、モバイルデータ通信の区別は NetworkManager から取得します。constrained になることはありません |
| `asuka/sdk/location` | 位置情報ポータル（GeoClue のもので、ユーザーに確認します）、なければ GeoClue 自体を使います。`PlaceName` は geocode-glib がインストールされていれば使えます |
| `asuka/sdk/speech` | Speech Dispatcher（動いていなければ起動します）の音声、速さ、高さ、音量を使います |
| `asuka/sdk/audio`、`asuka/sdk/audio/record` | GStreamer を使います。録音には AAC エンコーダー（FDK、libav など）を使い、マイクは既定のソースです |
| `asuka/sdk/nowplaying` | 何かを表示しているあいだ、セッションバスに `org.mpris.MediaPlayer2.<app>.instance<pid>` という名前の MPRIS プレイヤーを置きます。GNOME や KDE のメディアコントロールとロック画面、メディアキー、`playerctl` から操作できます。MPRIS の `Seek` は、アプリがスキップを扱う場合はスキップ、そうでなければシークになります。アートワークは `file://` か http(s) の URL です |
| `asuka/sdk/library` | ユーザーのフォルダー（XDG のピクチャ、ビデオ、ミュージック、ダウンロード）の、指定したフォルダーに保存されます |
| `ui.ShareFiles` | 共有シートはありません。ファイルはクリップボードにコピーされ、トーストが表示されます |
| `asuka/sdk/camera`、`asuka/sdk/camera/record` | カメラポータル（ユーザーへの確認は一度だけ。PipeWire のカメラで、`gstreamer1.0-pipewire` が必要）、なければ最初の Video4Linux カメラを、GStreamer を通じて使います。`Take`：ウィンドウの上にダイアログを出し、ライブプレビューと「写真を撮る」「キャンセル」を表示します。`View`：左右反転したプレビューで、枠いっぱいに表示します。前面と背面は同じカメラです（コンピューターのカメラはユーザーのほうを向いているため）。ビデオは MPEG-4（x264 か OpenH264 の H.264 と AAC）、それができなければ WebM です。コードの読み取りには GStreamer の `zbar` エレメントが必要です。カメラがなければ `ErrNoCamera`、拒否されたら "not allowed" です |
| `asuka/sdk/scan`、`asuka/sdk/vision` | コードは ZBar（`libzbar0`）が読み取ります。Aztec と Data Matrix は読めません。テキストは Tesseract（`libtesseract5` と各言語のデータ）が読み取ります。顔を検出するものはないので、`Faces` は空です。ZBar がなければ `scan.File` は "not available" で失敗し、`vision.Analyze` はインストールされていないものを結果から省きます |
| `asuka/sdk/imageedit` | GdkPixbuf のローダーでデコードし（HEIF と WebP は、それぞれのローダーがインストールされている場合）、向きを正しく直します。各操作は Go で行います。ビデオのフレームは GStreamer で取り出します |
| `asuka/sdk/webauth` | ウィンドウの上に WebKitGTK の Web ビューを入れたダイアログを開きます（`Ephemeral` では専用のセッション）。閉じるとキャンセルになります |
| `asuka/sdk/js` | 一度も表示されない WebKitGTK の Web ビュー |
| `asuka/sdk/maps` | libshumate（`libshumate-1.0-1`）で OpenStreetMap のタイルの地図を表示します。Satellite と Hybrid は道路地図を表示します。ユーザーの位置を示すレイヤーはありません。libshumate がなければ、ビューにその旨が表示され、`OnError` が呼ばれます |
| `asuka/sdk/background` | タスクの実行中はシステムがサスペンドしないようにします。通知は出ず、`Update` は何も変えません |
| `asuka/sdk/periodic` | タスクはアプリの実行中にタイマーで動きます（タスクのためにアプリが起動されることはありません）。電源を必要とするタスクはバッテリーで動いているあいだ、ネットワークを必要とするタスクはオフラインのあいだ待ちます |
| `asuka/sdk/screen` | `Lock`、`SetBars`、`PreventCapture` は何もしません。`OnScreenshot` は Pictures/Screenshots に新しく加わった画像（GNOME や KDE のもの）を通知します |
| `asuka/sdk/sensors` | iio-sensor-proxy（2-in-1 PC、タブレット）を使います。照度センサーをルクス単位で、加速度センサーの向きを、上を向いている軸に沿った 1 g として、向きが変わるたびに伝えます。ほかのセンサーはないので、`Available` は false で、`StepsBetween` は `ErrNotAvailable` で失敗します |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | Evolution Data Server のアドレス帳とカレンダー（GNOME Contacts、GNOME Calendar、オンラインアカウントのもの）を使います。アドレス帳やカレンダーがあればアクセスは `Full` です。`Pick` は検索欄付きの連絡先のダイアログです。`Add` は確認してから、既定のアドレス帳またはイベントのカレンダーに保存します。Evolution Data Server がない場合、連絡先のアクセスは `Denied`、カレンダーのアクセスは `WriteOnly` です。`Add` は vCard か iCalendar のファイルをユーザーの連絡先アプリやカレンダーアプリで開き（`saved` は false）、`contacts.Pick` は失敗します |
| `asuka/sdk/photos` のライブラリ、`asuka/sdk/appleauth`、`asuka/sdk/transcribe`、`asuka/sdk/widget`、`asuka/sdk/liveactivity` | Linux にはありません。ライブラリのアクセスは `Denied` です。`appleauth.Available` は false で、そのボタンは何も表示しません。`transcribe` と `liveactivity.Start` は `ErrNotAvailable` で失敗します。`widget.Set` は何もしません。`asuka/sdk/splash` には保持しておく起動画面がありません |
| `asuka/sdk/biometric`、`asuka/sdk/integrity`、`asuka/sdk/agerange`、`asuka/sdk/push` | Linux にはありません。利用不可、非対応と答えます。`asuka/sdk/review` は何もせず、`asuka/sdk/tracking` は許可と答えます |
| `asuka/sdk/crash` | Go のクラッシュ出力を使います。ネイティブコードでのクラッシュ（GTK のコードでのシグナル、GLib の abort）もここに含まれ、`Native` として報告されます。システム自身の記録（systemd-coredump、apport）は読みません |
| `ui.Haptic` | デスクトップには触覚フィードバックがないので、何もしません |
| `ui.OnShare`、`app.ShareSheet`、ショートカット | iOS と Android だけです |

## ブラウザーの場合

ページは何も宣言しません。パッケージが初めてカメラ、マイク、位置情報、通知を必要としたときに、ブラウザーがユーザーに確認します。これらの API の多くは安全なページ、つまり `https` か `localhost` から配信されるページでしか使えません（`asuka dev --platform web` のアドレスは該当しますが、`--lan` のアドレスは該当しません）。各パッケージは Web の API を呼び出します。詳しくは [Web](web.md) を参照してください。通知と Web Push のための Service Worker についてもそこで説明しています。

| パッケージ | ブラウザーの場合 |
| --- | --- |
| `asuka/sdk/device` | `PickPhotos` と `PickFiles`：ブラウザーのファイル選択で、キャッシュにコピーを作ります。`Browse`：新しいタブで開きます。クリップボード：アプリ自身がコピーしたものと、ユーザーがページに読み取りを許可した場合はほかのアプリのものです。`KeepAwake`：Screen Wake Lock です。`GetInfo` はユーザーエージェントから取得します（Chromium のクライアントヒントがあれば、システムのバージョンと Android スマートフォンの機種も分かります） |
| `asuka/sdk/secure` | 値を AES-GCM で暗号化して IndexedDB に保存します。鍵はブラウザーが外に出さないものです。キーチェーンではありません（[Web](web.md) を参照） |
| `asuka/sdk/camera`、`asuka/sdk/camera/record` | `Take` はスマートフォンのカメラを開くか、ライブのカメラ映像とシャッターのあるダイアログを開きます。`View` は `<video>` に映したカメラで、`Capture`、録画が使えます。ブラウザーに `BarcodeDetector` があればコードも読み取れます |
| `asuka/sdk/scan`、`asuka/sdk/vision` | `BarcodeDetector` がコードを読み取ります。顔とテキストは、ブラウザーが対応している場合だけです |
| `asuka/sdk/imageedit` | canvas を使い、JPEG か PNG を出力します。リモートの画像には CORS が必要です |
| `asuka/sdk/photos` | ページはライブラリを読めないので、アクセスは `Denied` です（写真の選択には `device.PickPhotos` を使います） |
| `asuka/sdk/library` | `Save` はファイルをダウンロードします |
| `asuka/sdk/location` | Geolocation API を使います。Web には `PlaceName` がありません（ブラウザーにはジオコーダーがないため） |
| `asuka/sdk/network` | オンラインかどうかは `navigator.onLine` から取得します。Wi-Fi、モバイルデータ通信、Ethernet の区別はブラウザーが教える場合（Android の Chrome）だけです。モバイルデータ通信は metered、データセーバーは constrained になります |
| `asuka/sdk/notify`、`asuka/sdk/push` | Notification API と Web Push を、アプリの Service Worker を通じて使います（`push.Register` の `Service` は `"webpush"`）。後で届く通知は、アプリのページが一つも開いていないあいだは表示できません |
| `asuka/sdk/audio`、`asuka/sdk/audio/record` | プレイヤーごとに audio 要素を使います（ブラウザーの自動再生のルールが適用されるので、タップから再生してください）。録音は `MediaRecorder` で、ブラウザーが対応していれば MPEG-4 の AAC、そうでなければ WebM の Opus です |
| `asuka/sdk/nowplaying` | Media Session API（`navigator.mediaSession`）を使います。ページが音を鳴らしているあいだ、ブラウザーのメディアコントロール、メディアキー、スマートフォンのロック画面に表示されます。トグルはありません（キーは再生か一時停止のどちらかを送ります）。ブラウザーが対応していない操作（一部のブラウザーでは停止）は省かれます |
| `asuka/sdk/speech` | `speechSynthesis` と、ブラウザーの音声を使います |
| `asuka/sdk/sensors` | Generic Sensor API（Chrome）か、デバイスのモーションと向きのイベント（Safari ではタップ時に許可を求めます）を、Android の軸と単位で伝えます。気圧計と歩数計はありません |
| `asuka/sdk/biometric` | WebAuthn を通じてプラットフォームの認証器（Touch ID、Face ID、Windows Hello、Android の画面ロック）を使います。最初の `Authenticate` でそのサイトのパスキーを作ります。プロンプトはブラウザーのものなので、`Reason` と `Cancel` は表示されません |
| `asuka/sdk/webauth` | ポップアップを開き、アプリと同じオリジンのページに戻ってきたら完了です（カスタムスキームではページを開けないため） |
| `asuka/sdk/js` | 非表示の、サンドボックス化された frame |
| `asuka/sdk/contacts`、`asuka/sdk/calendar` | `contacts.Pick` はブラウザーに Contact Picker がある場合（Android の Chrome）に使えます。`Add` は vCard か `.ics` ファイルをダウンロードします。Web では連絡先やカレンダーを読み取れません |
| `asuka/sdk/screen` | `Lock` はフルスクリーンかインストールしたアプリでのみ使えます。バーを隠すとフルスクリーンになります。`PreventCapture` と `OnScreenshot` はありません |
| `asuka/sdk/background` | ページは開いているあいだ動き続けます。タスクを作っても何も変わらず、通知も出ません |
| `asuka/sdk/tracking` | プロンプトは出ません。ブラウザーが Global Privacy Control か Do Not Track を送っていれば `Denied`、そうでなければ `Allowed` です |
| `ui.Haptic` | ブラウザーが対応していれば（Android の Chrome）`navigator.vibrate` を使います |
| `asuka/sdk/review`、`asuka/sdk/integrity`、`asuka/sdk/agerange`、`asuka/sdk/appleauth`、`asuka/sdk/periodic`、`asuka/sdk/widget`、`asuka/sdk/liveactivity`、`asuka/sdk/transcribe` | Web にはありません。その旨を示すエラーか、パッケージの `ErrNotAvailable` を返します |
| `asuka/sdk/maps` | Windows と同じく、ホストが OpenStreetMap のタイルを描きます。`ShowsUser` は、ページに位置情報の許可があれば表示されます |
| `asuka/sdk/crash` | Go のクラッシュ（ホストがランタイムの出力を保存します）と、捕捉されなかった JavaScript のエラーを、致命的でない `Exception` のレポートとして扱います |
| `asuka/sdk/splash` | `Hold` と `Hide` は何もしません |
| `ui.OnShare`、`app.ShareSheet`、ショートカット | iOS と Android だけです |

## カメラ：asuka/sdk/camera

`camera.Take` はシステムのカメラ（iOS のカメラ画面、Android のカメラアプリ）を表示し、撮った写真のファイル（JPEG）を引数に `done` を呼びます。ユーザーがキャンセルした場合は `""` が渡ります。カメラのないデバイス（iOS シミュレーター）では、エラーは `camera.ErrNoCamera` です。デスクトップにはカメラ画面がないので、`Take` はウィンドウの上に Asuka 独自のダイアログを出し、ライブプレビューと「写真を撮る」「キャンセル」を表示します。ブラウザーではスマートフォンのカメラか、同様のダイアログが開きます。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Take a photo"), OnPress: func() {
	camera.Take(func(path string, err error) {
		switch {
		case errors.Is(err, camera.ErrNoCamera):
			status.Set("This device has no camera")
		case err != nil:
			status.Set(err.Error())
		case path != "":
			photo.Set(path) // ui.Image で表示する
		}
	})
}})
```

アプリ独自の撮影画面を作るときは、`camera.View` がカメラに映っているものをライブで、枠いっぱいに表示します（iOS と Mac では AVFoundation のプレビュー、Android では CameraX の `PreviewView`、Windows では Media Foundation のもの、Linux では GStreamer のもの、ブラウザーでは `<video>`）。そして `camera.Capture` でそこから写真を撮ります。

```go
camera.View(camera.ViewProps{
	Mode:    camera.Video,
	Facing:  camera.Back,
	Style:   ui.Style{Grow: 1},
	OnError: func(message string) { status.Set(message) },
}),
ui.Button(ui.ButtonProps{Title: ui.Val("Photo"), OnPress: func() {
	camera.Capture(func(path string, err error) {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set("Saved " + path)
	})
}}),
```

`ViewProps`：

- `Facing`：`camera.Back`（既定）か `camera.Front`。
- `Mode`：`camera.Photo`（既定）か、`asuka/sdk/camera/record` で録画するための `camera.Video`。`camera.Video` でも写真は撮れます。
- `Style`：ビューの枠。
- `OnError`：カメラを表示できないときに呼ばれます。ユーザーが拒否した場合は `"not allowed"`、カメラがない場合は `"no camera"` です。
- `Scan`：映っているものからコードを読み取ります。`asuka/sdk/scan` を参照してください。

ビューは初めて表示されたときにカメラの許可を求め、ビューが画面に出ているあいだカメラはオンになります。表示するビューは一度に一つにしてください。`Capture` と録画は画面に出ているビューを使い、ビューがなければ `"no camera view"` で失敗します。

`Take` と `Capture` のファイルはアプリのキャッシュに置かれ、システムに消されることがあります。残しておきたいファイルは `store.Dir()` に移すか、[`asuka/sdk/library`](#ライブラリへの保存-asukasdklibrary) で写真ライブラリに保存してください。

**Android**：`asuka/sdk/camera` はカメラの権限を宣言する（ビューに必要なため）ので、Android はカメラアプリを使うときにもその権限を要求します。そのため `Take` は先に権限を求め、ユーザーが拒否すると `"not allowed"` で失敗します。

## ビデオの録画：asuka/sdk/camera/record

`asuka/sdk/camera/record` は、画面に出ている `Mode` が `camera.Video` の `camera.View` からビデオを録画します。`record.Start` は `record.Stop` が呼ばれるまで（`MaxDuration` を設定していればその時間まで）録画し、ビデオのファイルを引数に `done` を呼びます。ファイルはアプリのキャッシュに置かれ、形式は iOS と Mac では `.mov`、Android と Windows では `.mp4`、Linux ではエンコーダーに応じて `.mp4`、それができなければ `.webm` です。

```go
ui.Button(ui.ButtonProps{
	Title: signal.Func(func() string {
		if recording.Get() {
			return "Stop"
		}
		return "Record"
	}),
	OnPress: func() {
		if recording.Peek() {
			record.Stop()
			return
		}
		recording.Set(true)
		record.Start(record.Options{MaxDuration: time.Minute}, func(path string, err error) {
			recording.Set(false)
			if err != nil {
				status.Set(err.Error())
				return
			}
			status.Set("Recorded " + path) // ui.Video で再生する
		})
	},
})
```

音声も一緒に録音し、初回にマイクの許可を求めます。ユーザーが拒否すると、ビデオは無音になります。`record.Options{Mute: true}` を指定すると、許可を求めずに音声なしで録画します。録画は一度に一つだけです。

マイクの権限と利用目的の説明文は、`asuka/sdk/camera` ではなくこのパッケージに付いてきます。写真を撮るだけのアプリには加わりません。

## バーコードと QR コード：asuka/sdk/scan

`asuka/sdk/scan` は、`camera.View` に映っているものからライブで、または画像ファイルから、バーコードと QR コードを読み取ります。iOS と Mac では AVFoundation と Vision、Android では ML Kit（`asuka/sdk/scan` をインポートしたアプリにだけ組み込まれます）、Windows では Go で書かれた Asuka 独自のリーダー、Linux では ZBar、ブラウザーでは `BarcodeDetector` を使います。すべてのリーダーがすべての種類に対応しているわけではありません。[Windows の場合](#windows-の場合)と [Linux の場合](#linux-の場合)を参照してください。

`scan.Codes` はビューの `Scan` を作ります。指定した種類（指定しなければすべての種類）を読み取り、コードが視界に入るとメインスレッドで関数を呼びます。コードが映り続けているあいだは一度だけで、1 秒間視界から外れたあとにまた映ると、もう一度呼びます。

```go
camera.View(camera.ViewProps{
	Style: ui.Style{Grow: 1},
	Scan: scan.Codes(func(c scan.Code) {
		found.Set(fmt.Sprintf("%s: %s", c.Kind, c.Value))
		ui.Haptic(ui.HapticSuccess)
	}, scan.QR, scan.EAN13),
})
```

`scan.File` は、撮影した写真や選んだ写真などの画像からコードを読み取り、それらを引数に `done` を呼びます（コードがなければ空です）。

```go
scan.File(path, func(codes []scan.Code, err error) {
	switch {
	case err != nil:
		found.Set(err.Error())
	case len(codes) == 0:
		found.Set("No code in it")
	default:
		found.Set(codes[0].Value)
	}
}, scan.QR)
```

`scan.Code` は、種類を表す `Kind` と、コードが保持するテキストである `Value` からなります。種類は `QR`、`Aztec`、`DataMatrix`、`PDF417`、`EAN13`、`EAN8`、`UPCA`、`UPCE`、`Code128`、`Code39`、`Code93`、`Codabar`、`ITF` です。

**iOS** は UPC-A を先頭に 0 の付いた EAN-13 として読み取ります。`UPCA` を指定した場合、Asuka はそれを UPC-A に戻して返します。

## 画像の内容の認識：asuka/sdk/vision

`vision.Analyze(path, options)` は、画像の中のテキスト、顔、コードと、それぞれの位置を見つけます。処理はデバイス上で行われます。iOS と Mac では Apple の Vision、Android では ML Kit の同梱モデル（`asuka/sdk/vision` をインポートしたアプリにだけ組み込まれます）、Windows では Windows の OCR と顔検出、Linux では Tesseract（顔は検出できません）、ブラウザーでは Shape Detection API が対応しているものを使います。メインスレッド以外から呼んでください。

```go
go func() {
	r, err := vision.Analyze(photo, vision.Options{Languages: []string{"zh-Hans", "en-US"}})
	if err != nil {
		return
	}
	ui.Post(func() {
		for _, l := range r.Lines {
			fmt.Println(l.Text, l.Box)
			for _, w := range l.Words {
				fmt.Println(" ", w.Text, w.Box, w.Start, w.End)
			}
		}
		fmt.Println(len(r.Faces), "faces,", len(r.Codes), "codes")
	})
}()
```

- `Box` は、画像を正しい向きにしたときの左上を起点に、画像に対する割合で表します。そのため、画像に重ねたビューでも `asuka/sdk/imageedit` のピクセルでも、どの縮尺でも同じ値で使えます。
- `Line` は `Text`、`Box`、`Words` を持ちます。`Word` は、プラットフォームの単語分割が見つけた単語（中国語と日本語では辞書に基づく分割）か、句読点一つです。`Start` と `End` は行のテキストにおける単語の位置を、rune 単位で表します。
- `Code` は `Kind`（`asuka/sdk/scan` と同じ名前）、`Value`、`Box` を持ちます。
- `Options` で不要なもの（`NoText`、`NoFaces`、`NoCodes`）をオフにできます。`Languages` はテキストの言語を可能性の高い順に並べたもので、iOS だけで使われます。Vision がうまく読めるのは先頭の言語が属する系統の文字だけなので、中国語が含まれうる場合は中国語を先頭にしてください。中国語のモデルはラテン文字も読めます。Android は常に中国語とラテン文字を読み取ります。

**iOS** は読み取ったものを一つのまとまりとして枠で囲みます。中国語の行で枠を共有する単語は、その枠を各単語の幅で分け合うので、そうした単語の枠は推定値です。Vision が返すバーコードの枠は、読み取りに使った線になることがあります。Asuka はそれをバー全体まで広げます。**Android** では、単語の枠は各文字（symbol）の枠から求めます。

OCR は渡された画像を独自のサイズに縮小するので、非常に縦長のスクリーンショットでは小さな文字が読めなくなります。`imageedit.Crop` で切り分けて、それぞれを解析してください。

## 写真ライブラリ：asuka/sdk/photos

`asuka/sdk/photos` は、ユーザーの許可を得てユーザーのライブラリを読み取ります。iOS と Mac では PhotoKit、Android では MediaStore、Windows ではピクチャフォルダーを使います。Linux と Web にはアプリが読めるライブラリがないので、アクセスは `Denied` です。このパッケージは、ライブラリそのものを画面にするアプリのためのものです。ユーザーに数枚の写真を選んでもらうだけなら、権限を求めない `device.PickPhotos` を使えます。それで足りる場合はそちらを使ってください。Google Play は、ライブラリを読むアプリにその理由を Play Console で尋ねます。どの呼び出しも待つので、メインスレッド以外から呼んでください。

```go
go func() {
	if a, _ := photos.Request(); a == photos.Denied {
		return
	}
	assets, _ := photos.List(photos.Query{Screenshots: true, Limit: 60})
	thumb, _ := photos.Thumbnail(assets[0].ID, 300) // ui.Image 用のファイル
	full, _ := photos.Export(assets[0].ID)          // オリジナルをファイルとして
	fmt.Println(thumb, full.Path, full.Name)
}()
```

- `photos.Current()` は、許可を求めずに現在の状態を返します。`NotAsked`、`Denied`、`Limited`（ユーザーが選んだ写真だけ。iOS 14、Android 14）、`Allowed` のいずれかです。`photos.Request()` は初回に許可を求めます。`photos.ManageLimited()` を使うと、`Limited` のユーザーが対象の写真を変更できます。
- `photos.List(query)` は新しい順に、`Offset` から最大 `Limit` 件を返します。`Asset` は `ID`、`Width`、`Height`、`Created()`、`Screenshot` を持ちます。`Screenshots` を指定するとスクリーンショットだけに絞り込みます。iOS はスクリーンショットかどうかを知っていますが、Android では Screenshots フォルダーにあるものがスクリーンショットとして扱われます。
- `photos.Thumbnail(id, size)` は、短辺が `size` ピクセル以上の JPEG を返します。次回のためにアプリのキャッシュに保存されます。
- `photos.Export(id)` は、ライブラリにあるままの画像（HEIC、JPEG、PNG）をファイル名ごとコピーします。iCloud の写真は先にダウンロードされます。

## 写真、ブラウザー、クリップボード：asuka/sdk/device

`asuka/sdk/device` は、ビューの外でプラットフォームが提供する機能をまとめたものです。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Pick photos"), OnPress: func() {
	device.PickPhotos(3, func(paths []string, err error) {
		if err == nil && len(paths) > 0 {
			photos.Set(paths)
		}
	})
}}),
ui.Button(ui.ButtonProps{Title: ui.Val("Help"), OnPress: func() {
	device.Browse("https://example.com/help")
}}),
```

- `device.PickPhotos(limit, done)` は、システムの写真ピッカー（iOS では PHPicker、Android では写真選択ツール、デスクトップではファイルを開くダイアログ、ブラウザーではファイル選択）を表示し、最大 `limit` 枚（1〜255）の画像を選ばせます。権限は求めません。選ぶのはユーザーで、アプリに見えるのは選ばれたものだけです。`done` には画像のファイルが渡され、`ui.Image` で表示できます。ユーザーがキャンセルした場合は空です。ファイルはアプリの一時ファイルかキャッシュに置かれたコピーなので、残しておきたいものは移動してください。
- `device.Browse(url)` は、アプリの画面の上に Web ページを表示します（SFSafariViewController、Custom Tab）。URL は http か https でなければなりません。これに対して `ui.OpenURL` は、アプリを離れてブラウザーで開きます。デスクトップではユーザーのブラウザーで、ブラウザーでは新しいタブでページを開きます。
- `device.Clipboard()` はクリップボードのテキストを読み取ります。テキストがなければ `ok` は false です。メインスレッド以外から呼んでください。iOS では、ほかのアプリが置いたものをアプリが初めて読むときにユーザーに確認します。書き込むには `ui.Copy` を使います。
- `device.KeepAwake(on)` は、`on` が true のあいだ画面をオンのままにします。ビデオ、レシピ、地図などに使います。それを必要とする画面が閉じたらオフにしてください。
- `device.PickFiles(options, done)` は、システムのドキュメントピッカー（iOS の「ファイル」ブラウザー、Android のストレージピッカー、デスクトップではファイルを開くダイアログ、ブラウザーではファイル選択。権限は不要）を表示します。`Types` は MIME タイプ（`"application/pdf"`、`"image/*"`。指定しなければどのファイルでも）で、`Multiple` を指定すると複数選べます。`done` には選ばれたファイルごとに `device.File` が渡されます。`Path` はアプリのキャッシュにあるコピーで、`Name`、`Type`、`Size` も持ちます。ユーザーがキャンセルした場合は空です。
- `device.ClipboardHas()` は、クリップボードの中身を読まずに、`Text`、`Image`、`URL` のどれがあるかを返します（iOS はこのときユーザーに確認しません）。貼り付けるものがあるときだけ「ペースト」ボタンを表示する、といった使い方ができます。`device.ClipboardImage()` はクリップボードの画像を、コピーされたときの形式のままファイルに読み出します。`device.CopyImage(path, done)` は画像ファイルをクリップボードに置きます。`device.OnClipboardChange(f)` は、`stop` を呼ぶまで、クリップボードが変わるたびに `f` を呼びます。
- `device.GetInfo()` はデバイスとアプリの情報を返します。`Model`（"iPhone"、"Pixel 9"、"Mac"、"PC"）、`System`（"iOS"、"Android"、"macOS"、"Windows"、"Linux"）、`SystemVersion`、そして `asuka.json` の `version` と `build` である `AppVersion` と `Build` です。メインスレッド以外から呼んでください。

```go
func Player() ui.Node {
	info := signal.New("")
	go func() {
		if i, err := device.GetInfo(); err == nil {
			ui.Post(func() { info.Set(i.Model + ", " + i.System + " " + i.SystemVersion) })
		}
	}()
	device.KeepAwake(true)
	signal.OnCleanup(func() { device.KeepAwake(false) })
	return ui.Text(info)
}
```

## Web でのサインイン：asuka/sdk/webauth

`webauth.Start` は、システム自身のサインインシートにサインインページを表示します。iOS では `ASWebAuthenticationSession`（SwiftUI の `webAuthenticationSession`）、Android では Custom Tab です。中身はブラウザーなので、ユーザーのセッション、パスキー、パスワードの自動入力がそのまま使えます。また、アプリ独自の `WebView` を拒否するプロバイダー（Google のサインイン）にも拒否されません。OAuth には `ui.WebView` ではなくこちらを使ってください。

Mac にも `ASWebAuthenticationSession` があります。Windows と Linux にはシステムのシートがないので、ホストがウィンドウの上に独自の Web ビュー（WebView2、WebKitGTK）を開いてページを表示します。この Web ビューはアプリの Web プロファイルを使い、ユーザーのブラウザーのセッションは持ちません。また、埋め込みの Web ビューを拒否するプロバイダーには、ここでも拒否されます。ブラウザーではページをポップアップで開き、アプリと同じオリジンのページに戻ってきたら完了です（カスタムスキームではページを開けないため）。

```go
s := webauth.Start(webauth.Options{URL: authorizeURL, CallbackScheme: "myapp"},
	func(callback string, err error) {
		if errors.Is(err, webauth.ErrCanceled) {
			return // ユーザーが閉じた
		}
		// callback は myapp://…?code=…。コードを交換する。
	})
```

終わり方は 2 通りあります。

- **コールバックスキーム。** ページが `CallbackScheme`（アプリ自身のスキーム）の URL に移動すると、シートが閉じて `done` にその URL が渡されます。Android では、スキームはアプリのもの、つまり asuka.json の `scheme` でなければなりません。その URL は `ui.OnOpenURL` ではなく `done` に届きます。
- **アプリが終わらせる。** スキームを使わない場合は、アプリが受け取れる場所にページをリダイレクトさせます。ループバックインターフェースへのリダイレクト（`http://localhost:port/callback`、RFC 8252）を許す OAuth サーバーなら、そのポートでアプリが動かしているサーバーにコードを送ってくるので、そのサーバーが `s.Cancel()` を呼んでシートを閉じます。Android では、キャンセルするとタブの上にアプリが戻ってきます。

ユーザーがシートを閉じたとき、または `Cancel` で閉じたとき、`done` には `webauth.ErrCanceled` が渡ります。`Ephemeral` を指定すると、ブラウザーの Cookie を使わないプライベートセッションを求めます（iOS の `prefersEphemeralWebBrowserSession`。Android の Custom Tab は Cookie を保持します）。[`examples/usage`](../../examples/usage) は、この方法とループバックサーバーで Claude にサインインします。

## コピー、共有、URL を開く：ui.Copy、ui.Share、ui.OpenURL

`ui` には、テキストをシステムに渡す呼び出しが 3 つあります。どれもメインスレッドで動きます。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Copy link"), OnPress: func() { ui.Copy("https://example.com/s/42") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() { ui.Share("https://example.com/s/42") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Open in browser"), OnPress: func() { ui.OpenURL("https://example.com") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Call"), OnPress: func() { ui.OpenURL("tel:+15555550100") }}),
```

- `ui.Copy(text)` はテキストをクリップボードに置きます。
- `ui.Share(text)` はシステムの共有シートを表示し、アプリ、連絡先、AirDrop、Nearby Share にテキストを送れるようにします。URL はリンクとして共有されます。Mac では共有サービスのピッカー、Windows では共有 UI、ブラウザーでは Web Share のシート（ある場合）が表示されます。Linux には共有シートがないので、テキストはクリップボードにコピーされ、トーストが表示されます（Web Share のないブラウザーでも同じくコピーされます）。
- `ui.OpenURL(url)` は、URL を開くようシステムに頼みます。Web のアドレスはブラウザーで、それ以外のスキーム（`tel:`、`mailto:`、ほかのアプリのスキーム）はそれを扱うアプリで開きます。

`ui.ShareFiles(paths, options, done)` は、ファイルの共有シートを表示します。送りたいビデオ、「ファイル」に保存したい PDF、AirDrop で送りたい写真などに使います。`done` は、ユーザーが共有したかどうかにかかわらず、シートが閉じたあとにメインスレッドで呼ばれます。シートを表示できなかった（ファイルが存在しない）場合は、エラーでそれを知らせます。

```go
var share ui.Node
share = ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() {
	ui.ShareFiles([]string{report}, ui.ShareOptions{MIME: "application/pdf", Title: "Send the report", From: share}, func(err error) {
		if err != nil {
			status.Set(err.Error())
		}
	})
}})
```

`ShareOptions` は expo-sharing と同じものです。

| オプション | |
| --- | --- |
| `MIME` | ファイルのタイプです。シートに並ぶアプリはこれを見て判断します。Android ではインテントのタイプになります。iOS では `UTI` が空のときにタイプとして使われます。既定：最初のファイルの拡張子から決めます。 |
| `UTI` | iOS：ファイルの Uniform Type Identifier（`com.adobe.pdf`）です。`MIME` より優先されます。 |
| `Title` | シートのタイトルです。Android ではチューザーのタイトル、iOS ではメールの件名になります。 |
| `From` | シートがポップオーバーになる場合（iPad、Mac）に、シートが指し示すビューです。普通は共有ボタンです。既定：画面の中央。 |

iOS では、拡張子がタイプと合わないファイルは、expo-sharing と同じく、タイプに合う名前で共有されます。たとえば JPEG として共有した `image.pdf` は `image.pdf.jpeg` として送られます。これはコピーではなく、ファイルへのリンクです。Android では、ファイルはアプリの FileProvider を通じて渡されます。アプリ自身のフォルダーの外にあるファイルは、先にキャッシュにコピーされます。Mac では共有サービスのピッカーです。Windows では共有 UI です（Windows はいつ閉じたかを知らせないので、`done` は表示された時点で呼ばれます）。Linux には共有シートがないので、テキストの共有と同じく、ファイルはクリップボードに置かれます。ブラウザーでは、可能なら Web Share のシートでファイルを共有し、できなければファイルを一つずつダウンロードします。

アプリ自身を URL で開けるようにするには、[配布](shipping.md#asukajson)の `asuka.json` の `scheme` と `links`、そして `ui.OnOpenURL` を参照してください。

## ファイルのプレビュー：asuka/sdk/preview

`preview.Show(sources, options, done)` は、メールアプリが添付ファイルを表示するように、システムのビューアーでファイルを表示します。PDF、画像、ビデオ、オーディオ、Office と iWork の書類、テキストに対応します。ソースには、ファイルのパス、アセットのパス（`ui.Image` が受け取る形式）、http(s) の URL を指定できます。URL の場合、ホストが先にアプリのキャッシュへ、元の名前で（またはサーバーが返すタイプの拡張子を付けて）ダウンロードします。メインスレッドで呼びます。

```go
var open ui.Node
open = ui.Button(ui.ButtonProps{Title: ui.Val("Open the invoice"), OnPress: func() {
	preview.Show([]string{invoice, "docs/terms.pdf"}, preview.Options{From: open}, func(err error) {
		if errors.Is(err, preview.ErrNoViewer) {
			status.Set("Nothing on this device opens it")
		}
	})
}})
```

| オプション | |
| --- | --- |
| `Start` | 最初に表示するファイルのインデックス。 |
| `From` | ビューアーが広がり出てくる元のビュー（iOS）。普通はユーザーがタップしたものです。 |
| `Edit` | ユーザーがマークアップで PDF や画像に書き込めるようにします。書き込みはファイルに保存されます（iOS）。 |

`done` はビューアーが閉じたあとにメインスレッドで呼ばれます。ファイルを表示できるものがない場合は `preview.ErrNoViewer` が、それ以外の問題（ファイルが存在しない、ダウンロードに失敗した）の場合はほかのエラーが渡ります。

| | ビューアー | `done` |
| --- | --- | --- |
| iOS | 画面の上に Quick Look（`QLPreviewController`）。スワイプでファイルを切り替え、共有、マークアップ、検索ができます | 閉じられたとき |
| Android | `Start` のファイルを `ACTION_VIEW` で、そのタイプ用にユーザーが選んだアプリ（Drive の PDF ビューアー、フォト）で開きます。アプリの FileProvider を通じ、そのファイルだけの読み取り権限を付けて渡します | ユーザーが戻ってきたとき |
| macOS | Quick Look のパネル（`QLPreviewPanel`）。矢印でファイルを切り替えます | パネルが閉じたとき |
| Windows | ファイルの既定のアプリ（`ShellExecuteEx`）か、「このファイルをどのように開きますか？」ダイアログ | 渡し終えたとき |
| Linux | ファイルの既定のアプリ（`GtkFileLauncher`、サンドボックス内ではポータルのもの） | 渡し終えたとき |
| Web | ブラウザーが表示できるもの（画像、ビデオ、オーディオ、テキスト、PDF、HTML）は新しいタブで開きます。それ以外のタイプは `ErrNoViewer` です | 開いたとき |

Android にはシステムのビューアーも、複数のファイルをめくれるビューアーもありません。そのため、Android のメールアプリが添付ファイルを一つずつ開くのと同じく、一つのファイルだけを開きます。

## 印刷と PDF：asuka/sdk/print

`asuka/sdk/print` は、ファイル、HTML マークアップ、WebView のページに対して、プリンターとオプション（部数、ページ、用紙、向き。Android と Mac では「PDF として保存」も）を備えたシステムの印刷ダイアログを表示します。また、HTML をページに割り付けて PDF にすることもできます。`PDF` 以外の呼び出しはメインスレッドで動き、ダイアログが閉じると `done` に `print.Result` が渡ります。

```go
var printButton ui.Node
printButton = ui.Button(ui.ButtonProps{Title: ui.Val("Print"), OnPress: func() {
	print.HTML(receipt, print.Options{Job: "Receipt", From: printButton}, func(r print.Result, err error) {
		if err == nil && r == print.Printed {
			status.Set("Sent to the printer")
		}
	})
}})
```

- `print.File(source, options, done)` は PDF か画像を印刷します（ソースは `preview.Show` と同じく、パス、アセット、http(s) の URL）。それ以外のタイプは `print.ErrUnsupported` で失敗します。ただし Windows と Linux は例外です（プリンターが、CUPS で印刷できるものを受け付けます）。
- `print.HTML(markup, options, done)` はマークアップを、ブラウザーがページを印刷するのと同じようにページ分けして印刷します。CSS の `@media print` と `break-before` が効きます。画像とスタイルは絶対 URL（`https:`、`data:`）から読み込みます。
- `print.WebView(web, options, done)` は `ui.WebView` に表示されているものを印刷します。

`Options` には、キューに表示されるジョブ名の `Job`（既定はファイル名、ページのタイトル、どちらもなければアプリ名）、ダイアログを開いたときの向きの `Orientation`（`print.Portrait`、`print.Landscape`）、ダイアログがポップオーバーになる場合（iPad）にダイアログが指し示すビューの `From` があります。

`Result` は、`print.Printed`（ジョブがプリンターのキューに入った、またはダイアログから PDF として保存された）、`print.Cancelled`、プラットフォームが結果を知らせない場合の `print.Unknown` のいずれかです。失敗したジョブはエラーになります。`print.ErrUnavailable` は、デバイスが印刷できないことを示します。

| | ダイアログ | ファイル | HTML、WebView | 結果 |
| --- | --- | --- | --- | --- |
| iOS | `UIPrintInteractionController`。iPad ではポップオーバー | PDF と画像（`printingItem`） | WebKit の print formatter | 印刷またはキャンセル |
| Android | `PrintManager` の印刷画面 | PDF はそのまま書き出し、画像は向きを直してページに合わせます（`PrintHelper` と同じ） | WebView の print adapter | 印刷、キャンセル、失敗（ジョブの状態） |
| macOS | `NSPrintOperation` のパネル。ウィンドウ上のシートで、プレビュー付き | PDFKit（PDF、または 1 ページに置いた画像） | WebKit の print operation | 印刷またはキャンセル |
| Windows | WebView2 のもの。HTML ではシステムの印刷ダイアログ、WebView ではブラウザーのプレビュー | タイプにかかわらず、そのファイルのアプリが印刷します（シェルの `print` verb） | WebView2 | 不明 |
| Linux | GTK の印刷ダイアログ（ファイルの場合は印刷ポータルのもの） | プリンターが受け付けます（CUPS：PDF、画像） | WebKitGTK の print operation | 印刷またはキャンセル |
| Web | ブラウザーの印刷ダイアログ | ブラウザーのビューアーで表示した PDF、画像 | ページ自身の Web ビュー | 不明 |

Android と iOS では、写真の印刷と同じく、横長の画像は横向きで始まります。

### HTML から PDF を作る

`print.PDF(markup, options)` は、`print.HTML` と同じようにマークアップをページに割り付けますが、ダイアログは出さず、アプリのキャッシュに PDF を書き出します。ファイル名はページの `<title>` から付けられます（"Receipt.pdf"。`<title>` がなければ "Document.pdf"）。戻り値はそのパスで、共有したり、メールで送ったり、移動したりできます。テキストはテキストのまま残るので、検索も選択もできます。メインスレッド以外から呼んでください。

```go
go func() {
	path, err := print.PDF(receipt, print.PDFOptions{Page: print.A4, Margins: print.Uniform(48)})
	ui.Post(func() {
		if err == nil {
			ui.ShareFiles([]string{path}, ui.ShareOptions{Title: "Receipt"}, nil)
		}
	})
}()
```

| オプション | |
| --- | --- |
| `Page` | 用紙の大きさ（ポイント単位）：`print.A3`、`A4`、`A5`、`Letter`、`Legal`、または独自のサイズ。既定：ユーザーの地域の用紙です。Letter を使う南北アメリカの国では Letter（CLDR の用紙サイズ）、それ以外では A4 です。 |
| `Landscape` | ページを横向きにし、幅を長い辺にします。 |
| `Margins` | 各ページの内容の周りの余白（ポイント単位）。既定：0.5 インチ（36 ポイント）。余白なしにするには `print.Uniform(0)`。 |

レイアウトは、iOS では WebKit（`UIPrintPageRenderer` と Web ビューの print formatter）、Android では WebView の print adapter、Mac では WebKit の print operation、Windows では WebView2（Chromium の `Page.printToPDF`）、Linux では WebKitGTK（ファイルへの印刷）で行います。ブラウザーは印刷ダイアログを通してしか PDF を作れないので、`PDF` は失敗します。代わりに `print.HTML` を使えば、ユーザーが「PDF として保存」を選べます。

## メールとメッセージ：asuka/sdk/compose

`asuka/sdk/compose` は、システムのメールとメッセージの作成画面を、内容を入れた状態で開きます。送信するのはユーザーです。メインスレッドで呼び、`done` には `compose.Result` が渡ります。`compose.Sent`、`Saved`（下書き）、`Cancelled`、またはメールをほかのアプリが扱っていて結果を知らせない場合の `Unknown` のいずれかです。

```go
compose.Mail(compose.MailMessage{
	To:      []string{"support@example.com"},
	Subject: "Order 1042",
	Body:    "<p>Hello,</p><p>The receipt is attached.</p>",
	HTML:    true,
	Attachments: []compose.Attachment{{Source: receiptPDF, Name: "Receipt 1042.pdf"}},
}, func(r compose.Result, err error) {
	if errors.Is(err, compose.ErrUnavailable) {
		status.Set("Set up a mail account to send it")
	}
})

compose.Message(compose.TextMessage{To: []string{"+15555550100"}, Body: "On my way"}, nil)
```

`MailMessage` は、宛先の `To`、`CC`、`BCC`、件名の `Subject`、本文の `Body`（`HTML` なら HTML マークアップ）、`Attachments` を持ちます。`TextMessage` は、`To`（電話番号か iMessage のアドレス）、`Subject`（「メッセージ」で件名がオンになっている場合。そうでなければ本文の前に入ります）、`Body`、`Attachments` を持ちます。`Attachment` は、`Source`（パス、アセット、http(s) の URL）、`Type`（既定は拡張子から決まるタイプ）、受信者に表示される `Name`（既定はファイル名）からなります。メールがマークアップを運べない場合（`mailto:` リンク、Simple MAPI）、本文は `compose.PlainText` で作ったものと同じテキストとして送られます。

`compose.Check()` はデバイスで何ができるかを返します（メインスレッド以外から呼んでください）。`Mail` はメールのアカウントかアプリがあること、`MailComposer` は iOS 自身の作成画面があること（ユーザーの操作の結果が分かり、添付ファイルも扱えます）、`Text` はテキストメッセージ、`TextAttachments` は添付ファイル付きのメッセージを送れることを表します。`CanSendMail`、`CanSendText`、`CanSendAttachments` はそれぞれ一つずつを調べます。デバイスでできない呼び出しは `compose.ErrUnavailable` で失敗します。

| | メール | メッセージ | 結果 |
| --- | --- | --- | --- |
| iOS | アプリの上に「メール」の作成画面（`MFMailComposeViewController`）。「メール」のアカウントがなければ、ユーザーが設定したメールアプリ向けの `mailto:` リンク（添付ファイルなし） | 「メッセージ」の作成画面（`MFMessageComposeViewController`） | 送信、保存、キャンセル。リンクの場合は不明 |
| Android | メールアプリだけを対象に、`ACTION_SENDTO` の `mailto:`、添付ファイルがあれば `mailto:` をセレクターにした `ACTION_SEND`。ファイルはアプリの FileProvider を通じて渡します。マークアップは span（`EXTRA_TEXT`）と `EXTRA_HTML_TEXT` の両方で渡します | 既定の SMS アプリ：`sms_body` を付けた `smsto:`、添付ファイルがあれば `ACTION_SEND` | ユーザーが戻ってきた時点で不明 |
| macOS | ユーザーのメールアプリ向けの `mailto:` リンク。添付ファイルがあればメールの共有サービス（`NSSharingService`）。これは CC と BCC を受け付けません（指定すると `ErrUnavailable` で失敗します） | 「メッセージ」（`NSSharingService`） | 不明、またはキャンセル |
| Windows | `mailto:` リンク。添付ファイルがあれば Simple MAPI（`MAPISendMailW`：Outlook、Thunderbird。新しい Outlook と Windows の「メール」アプリは対応していないので `ErrUnavailable`） | `ErrUnavailable` | 不明、またはキャンセル（MAPI） |
| Linux | メールポータル（メールアプリの作成画面に添付ファイル付きで）、なければ `xdg-email`、それもなければ `mailto:` リンク | `ErrUnavailable` | 不明、またはキャンセル（ポータル） |
| Web | `mailto:` リンク。添付ファイルがあれば Web Share のシート | `sms:` リンク。添付ファイルがあれば Web Share のシート | 不明 |

iOS シミュレーターには「メール」のアカウントも「メッセージ」もないので、作成画面は実機で試してください。

## 位置情報：asuka/sdk/location

`asuka/sdk/location` は、ユーザーの許可を得て、デバイスの現在地と移動中の位置を取得します。許可は初回に求めます。iOS と Mac では Core Location、Android では Android の位置情報マネージャー（Google Play 開発者サービスは不要）、Windows では WinRT の Geolocator、Linux では位置情報ポータル（または GeoClue）、ブラウザーでは Geolocation API を使います。求めるのはアプリの使用中の位置情報で、バックグラウンドでの位置情報ではありません。

`location.Current()` はデバイスの現在地を返します。ユーザーの応答を待ち、さらに測位を待つので、メインスレッド以外から呼んでください。ユーザーが許可していなければ、エラーは `location.ErrDenied` です。

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Where am I?"), OnPress: func() {
	go func() {
		p, err := location.Current()
		ui.Post(func() {
			switch {
			case errors.Is(err, location.ErrDenied):
				where.Set("Allow location in Settings")
			case err != nil:
				where.Set(err.Error())
			default:
				where.Set(fmt.Sprintf("%.4f, %.4f (within %.0f m)", p.Latitude, p.Longitude, p.Accuracy))
			}
		})
	}()
}})
```

`location.Watch(f)` は、`stop` が呼ばれるまで、デバイスが動くたびにその位置を引数に `f` をメインスレッドで呼びます。最初の呼び出しは現在地です。許可の求め方は `Current` と同じで、ユーザーが拒否すると `f` は呼ばれません。コンポーネントと一緒に止めてください。

```go
func Track() ui.Node {
	here := signal.New(location.Position{})
	stop := location.Watch(func(p location.Position) { here.Set(p) })
	signal.OnCleanup(stop)
	return ui.Text(signal.Func(func() string {
		p := here.Get()
		return fmt.Sprintf("%.5f, %.5f", p.Latitude, p.Longitude)
	}))
}
```

`location.PlaceName(lat, lon)` は、その場所の名前（"Mission District, San Francisco"）を返します。プラットフォームのジオコーダーがサーバーに問い合わせるので、ユーザーが名前を求めたときだけ呼んでください。メインスレッド以外から呼びます。Windows では OpenStreetMap の Nominatim に、Linux では geocode-glib（インストールされていれば）に問い合わせます。ブラウザーにはジオコーダーがないので、失敗します。

`location.Position` は、`Latitude`、`Longitude`、`Accuracy`（メートル単位。デバイスがその半径内にいることを表します）、`Altitude`（海抜のメートル数。分からなければ 0）、`Time` を持ちます。

**Android**：`Current` は、直近 1 分以内の測位があればそれを返します。なければ新しい測位を最大 10 秒待ち、それでも得られなければ最後に分かっている位置を返すか、`"no location"` で失敗します。

## 通知：asuka/sdk/notify

`asuka/sdk/notify` はローカル通知を、すぐに、またはアプリが決めた時刻に表示します。その時刻にアプリが動いているかどうかは問いません。通知をタップすると、その URL でアプリが開きます。メッセージングアプリに必要な機能もそろっていて、`asuka/sdk/push` の通知にも使えます。通知内で入力するアクションと返信、スレッド、Android のチャンネル、会話のメッセージ、バッジ、表示中の通知、アプリを開いているあいだにどの通知を表示するか、です。

```go
go func() {
	ok, err := notify.Request()
	if err != nil || !ok {
		return
	}
	err = notify.Schedule(notify.Notification{
		ID:    "water",
		Title: "Water the plants",
		Body:  "It's been a day.",
		URL:   "myapp://plants",
		After: 24 * time.Hour,
	})
	if err != nil {
		fmt.Println(err)
	}
}()
```

- `notify.Request()` は初回にユーザーへ通知の許可を求め、許可されているかどうかを返します。そのときのユーザーの答え、またはその後ユーザーが「設定」で変えた状態です。メインスレッド以外から呼んでください。
- `notify.Schedule(n)` は、今から `n.After` 後（1 秒以上）に `n` を表示するよう予約します。`notify.Show(n)` はすぐに表示します。待機中または表示中の通知と同じ `ID` を持つ通知は、それを置き換えます。メインスレッド以外から呼んでください。
- `notify.Cancel(id)` は、待機中か表示済みかにかかわらず通知を取り消します。メインスレッドで呼びます。

`Notification` の `URL` には、アプリのスキーム（`asuka.json` の `scheme`）の URL を指定してください。ユーザーが通知をタップすると、`ui.OnOpenURL` がそれを受け取ります。ほかのフィールドは次のとおりです。

| フィールド | 役割 |
|---|---|
| `Subtitle` | タイトルの下の 1 行（iOS）。Android ではアプリ名の横のサブテキスト |
| `Data` | アプリ独自のキー。`Showing`、`OnAction`、`Present` でそのまま返されます |
| `Category` | カテゴリーの ID。そのカテゴリーのアクションが表示されます（下記） |
| `Thread` | 同じスレッドのほかの通知とまとめます。iOS ではスレッド、Android では Asuka が管理するサマリー通知付きのグループです |
| `Summary` | Android のグループのサマリーの下に出る行（"3 new messages"）。iOS は独自の文を書きます |
| `Channel` | 通知が属する Android のチャンネル（下記）。iOS にはありません |
| `Badge` | 通知が表示されたときのアプリアイコンの数字（iOS、macOS）。Android では通知の数 |
| `Silent` | 音を鳴らしません |
| `Image` | 画像。ソースは `ui.Image` と同じ（アセット、ファイル、http(s) の URL）です。iOS では添付ファイル、Android では BigPicture になります |

**iOS** は、`Present` で別の指定をしない限り、アプリを開いているあいだも通知をバナーで表示します。**Android** は予約した通知を不正確なアラーム（exact alarm の権限は不要）で投稿するので、予定の時刻より少し遅れることがあります。

### アクションと返信

カテゴリーは、通知の種類とそのボタンを表します。アプリのカテゴリーは起動時にルートコンポーネントで、カテゴリーを指定した通知（プッシュ通知も含みます。iOS では `aps.category`、Android ではデータの `"category"`）が届く前に設定してください。設定は起動をまたいで保持されます。

```go
notify.SetCategories(notify.Category{ID: "message", Actions: []notify.Action{
	{ID: "reply", Title: "Reply", Icon: "arrowshape.turn.up.left",
		Reply: &notify.Reply{Button: "Send", Placeholder: "Message"}},
	{ID: "read", Title: "Mark as Read"},
	{ID: "delete", Title: "Delete", Destructive: true, Unlock: true},
	{ID: "open", Title: "Open", Open: true},
}})

notify.OnAction(func(r notify.Response) {
	switch r.Action {
	case "": // 通知そのもののタップ
		openChat(r.Notification.Thread)
	case "reply":
		_, done := r.Defer() // 送信には少し時間がかかる
		go func() {
			defer done()
			api.Send(r.Notification.Thread, r.Reply)
		}()
	case "read":
		notify.RemoveThread(r.Notification.Thread)
	}
})
```

- `Action` は、`ID` と `Title`、`Icon`（SF Symbol。iOS 15 と macOS 12 で表示されます。Android のアクションにはアイコンが表示されません）、`Destructive`（iOS では赤で表示されます）、`Unlock`（先にデバイスのロックを解除させます。iOS、Android 12 以降）、`Open`（アプリを開きます）、`Reply`（テキスト欄。送信ボタンのタイトルとプレースホルダーを指定します）を持ちます。
- `notify.OnAction(f)` は、`stop` を呼ぶまで、応答があるたびにそれを引数に `f` をメインスレッドで呼びます。応答は、タップ（`Action` が `""`）かアクションです。`Reply` は入力されたテキスト、`Notification` は応答の対象になった通知（`asuka/sdk/push` の通知も含みます）です。`f` を設定する前に来た応答（アプリを起動した応答や、アプリを起こした応答）は最初の `f` を待つので、`OnAction` はアプリの起動時に設定してください。
- `Open` のないアクションはバックグラウンドで実行されます。アプリの Go のコードが動きますが（アプリが動いていなければ画面なしで起動され、`main`、続いてルートコンポーネントが実行されます。ルートコンポーネントは `OnAction` を呼ぶ必要があります）、アプリは前面に出てきません。`f` が戻ったあとも続く処理がある場合は、`f` の中で `r.Defer()` を呼び、処理が終わったら `done` を呼びます。システムはそれまで、または `ctx` の期限（システム自身の期限より少し前）までアプリを動かし続けます（iOS では約 30 秒、Android では約 10 秒です）。

**Android** は、iOS と同じく、バックグラウンドのアクションが終わるまでにアプリがその通知を（同じ `ID` で `Show` して）表示し直さなければ、通知を消します。返信のあと、Android は通知がもう一度投稿されるのを待ちます（それまでスピナーが回り続けます）。そのため Asuka は、会話の通知にはユーザーの名前で返信を加え、それ以外の通知は消します。

### スレッドと表示中の通知

`Thread` はチャットの通知をまとめます。iOS では通知を重ねて表示し、Android ではサマリー通知の下のグループにまとめます。このサマリー通知は、通知の出入りに合わせて Asuka が管理します。

- `notify.Showing()` は、表示中（通知センター、通知シェード）のアプリの通知を、プッシュ通知も含めて新しい順に返します。各 `Delivered` は、`ID`、`Title`、`Subtitle`、`Body`、`URL`、`Data`、`Category`、`Thread`、`Time`、そしてプッシュ通知かどうかを示す `Remote` を持ちます。メインスレッド以外から呼んでください。
- `notify.Remove(ids...)`、`notify.RemoveThread(thread)`（読み終えたチャットに使います）、`notify.RemoveAll()` は通知を消します。メインスレッドで呼びます。

### アプリを開いているあいだに表示する通知

```go
notify.Present(func(n notify.Delivered) notify.Presentation {
	if n.Thread == openChat.Peek() {
		return notify.Hide // そのチャットが画面に出ている
	}
	return notify.Banner
})
```

`notify.Present(f)` を使うと、アプリがフォアグラウンドにあるあいだに届いた通知をどう表示するかを `f` で決められます。`Banner`（アプリを閉じているときと同じ）、`Quiet`（バナーも音もなしで、通知センターや通知シェードに入れる）、`Hide` のいずれかを返します。`f` は通知が届いたときにメインスレッドで呼ばれ、すぐに答えを返す必要があります。ホストは最大 1 秒待ち、それを過ぎると通知を表示します。`nil` を渡すと、ふたたびすべての通知を表示します。メインスレッドで呼びます。

### 会話のメッセージ

メッセージは、プラットフォームがメッセージを表示するのと同じ形で、送信者の写真付きで表示されます。iOS では通信通知（communication notification）、Android では会話（通知シェードの「会話」セクションに、会話のショートカット付きで表示）です。

```go
ana := notify.Person{ID: "u7", Name: "Ana", Image: "https://example.com/ana.jpg"}
chat := notify.Conversation{ID: "climbing", Title: "Climbing", Group: true}
go notify.Show(notify.Notification{Body: "Who's in for Saturday?", Category: "message"}.From(ana, chat))
```

- `n.From(sender, c)` は `n` をメッセージにします。`sender` が `c` で `n.Body` を発言した、という意味になります。`ID` と `Thread` の既定値は会話の `ID`（1 対 1 の会話では送信者の `ID`）、`Title` の既定値は送信者の名前（グループではグループのタイトル）です。
- `Person` は、`ID`（毎回同じもの）、`Name`、`Image`（ソースは `ui.Image` と同じ）、`Bot` を持ちます。`Conversation` は、`ID`、グループの `Title` と `Image`、`Group`、`Me`（会話の中でのユーザーの表示。Android はユーザーの返信にこの名前を付けます。既定は "You"）、それ以前のメッセージ `Earlier` を持ちます。
- **Android** は一つの会話を一つの通知として表示し、メッセージをその中に並べます。新しいメッセージは表示中の会話の通知に加わります（または `Earlier` でそれより前のメッセージを設定します）。**iOS** はメッセージを一つずつ別の通知として、会話のスレッドの中に表示します。
- **iOS** では、App ID で Communication Notifications ケイパビリティを有効にする必要があります。`From` を呼ぶアプリにはそのエンタイトルメントと、Info.plist の `NSUserActivityTypes`（`INSendMessageIntent`）が加わります。リリースビルドのプロファイルにもこのケイパビリティが含まれていなければなりません。含まれていない場合、メッセージは普通の通知として表示されます。**macOS** はメッセージを普通の通知として表示します。

### Android の通知チャンネル

チャンネルは通知の種類の一つで、ユーザーはアプリの設定でチャンネルごとにオフにしたり、音を消したりできます。Android は通知を、そのチャンネルの音、バイブレーション、重要度で表示します。iOS にはチャンネルがないので、無視されます。

```go
notify.SetChannelGroups(notify.ChannelGroup{ID: "chats", Name: "Chats"})
notify.SetChannels(
	notify.Channel{ID: "messages", Name: "Messages", Importance: notify.ImportanceHigh, Group: "chats"},
	notify.Channel{ID: "digest", Name: "Daily digest", Importance: notify.ImportanceLow, NoBadge: true},
)
```

- `notify.SetChannels(cs...)` はチャンネルを作り、以前に作ったチャンネルの名前を更新し、以前に作ったもののうち `cs` にないチャンネルを削除します。チャンネルの `Importance`（`ImportanceDefault`、`ImportanceHigh`：ヘッドアップ通知、`ImportanceLow`：音なし、`ImportanceMin`）、`Silent`、`NoVibration`、`NoBadge` は作成時に決まり、その後はユーザーが変えるものになります。`notify.SetChannelGroups(gs...)` はグループに対して同じことをします（グループを削除すると、そのチャンネルも削除されます）。メインスレッドで呼びます。
- `Channel` を指定しない通知、または存在しないチャンネルを指定した通知は、既定のチャンネル（"Notifications"）に入ります。既定のチャンネルは削除されません。プッシュ通知では、FCM の `android.notification.channel_id`、またはデータメッセージの `"channel"` でチャンネルを指定します。

### バッジ

`notify.SetBadge(n)` はアプリのアイコンの数字を設定します。`0` を渡すと消えます。**iOS** と **macOS**（Dock）は設定したとおりに表示します。**Android** には独自のバッジがありません。バッジを表示するランチャーはアプリの通知を数える（またはドットを表示する）ので、通知が読まれる（`Remove`、`RemoveThread`）とバッジは消え、`SetBadge` は何もしません。通知の `Badge` は、ランチャーが表示するかもしれない数字です。`NoBadge` を指定したチャンネルの通知は数えられません。

### プラットフォームごとの動作

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| 表示、予約、取り消し | ✓ | ✓ | ✓ | ✓（トースト） | ✓（予約：systemd のユーザータイマー。アプリが動いているかどうかは問わない） | ✓（予約：アプリのページが開いているあいだ） |
| アクション、返信 | ✓ | ✓ | ✓ | ✓（ボタン 5 つ） | アクション（返信するとアプリが開く） | ブラウザーが表示する場合はボタン（Chrome、Edge：2 つ）。インライン返信がある場合は返信 |
| バックグラウンドのアクション | ✓（約 25 秒） | ✓（約 9 秒） | ✓ | ✓（約 25 秒。そのためにアプリが起動される） | アプリの実行中 | アプリのページの中（Service Worker がページを開く） |
| スレッド | ✓ | ✓（グループとサマリー） | ✓ | まとめない。`RemoveThread` は使える | ✓ | まとめない。`RemoveThread` は使える |
| チャンネル | 無視 | ✓ | 無視 | 無視 | 無視 | 無視 |
| 会話 | ✓（通信通知） | ✓（MessagingStyle、ショートカット） | 普通の通知 | 普通の通知 | 普通の通知 | 普通の通知。送信者の写真がアイコンになる |
| バッジ | ✓ | ランチャーのもの | ✓（Dock） | タスクバーボタンのオーバーレイ | ランチャー API のもの（Dash to Dock、Plasma） | インストールしたアプリのもの（`navigator.setAppBadge`） |
| 表示中の通知の取得、削除 | ✓ | ✓ | ✓ | ✓（通知センターのもの） | 今回の実行でアプリが表示したもの | ✓（Service Worker のもの） |
| Present | ✓ | ✓ | ✓ | ✓（予約した通知を除く） | ✓ | ✓ |

**macOS** ではアプリのバンドルが必要です。`asuka dev` と `asuka build` はバンドルからアプリを実行します。単体で実行した実行ファイル（`go run`）には bundle ID がないので、呼び出しはその旨を示すエラーで失敗します。**Windows** はトーストを、すぐに、または予約して表示します。画像も表示でき、カテゴリーのアクションはボタン、返信はテキストボックスになります。アプリは初回に自分自身を登録します（AppUserModelID、つまり asuka.json の `id` を持つスタートメニューのショートカットと、COM のアクティベーター。どちらもユーザーのレジストリに、アプリの実行ファイルを指して登録します）。これにより、タップやボタンの操作がアプリに届き、アプリが動いていなければ Windows がアプリを起動します。バックグラウンドのアクションでは、アプリは見えないまま動き、`OnAction` が終わると終了します。バッジは、アプリのタスクバーボタンに描かれる数字です（パッケージ化されたアプリでは、システムのバッジ）。Windows は、予約したトーストを `Present` の答えにかかわらず表示します。**Linux** は、アプリにデスクトップファイルがある（またはサンドボックスで動いている）場合は GNotification として通知を送り、そうでなければデスクトップの通知サーバーに直接送ります（GNOME Shell は、デスクトップファイルのないアプリの通知を受け付けません）。タップやボタンの操作は、アプリの実行中にアプリに届きます。後で届く通知は systemd のユーザータイマー（`systemd-run --user`）になります。タイマーはアプリの実行ファイルをウィンドウなしで起動して通知を表示し、アプリが動いていればアプリに処理を渡し、タップされればアプリを起動します。systemd がない場合（Flatpak、`asuka dev`）は、アプリの実行中にしか表示されません。**ブラウザー**は、アプリの Service Worker を通じて通知を表示し、`asuka/sdk/push` には Web Push を使います。詳しくは [Web](web.md) を参照してください。

## サウンド: asuka/sdk/audio

`asuka/sdk/audio` は、プラットフォームのプレイヤーで効果音や音楽を再生します。iOS と Mac では AVPlayer、Android では Media3 の ExoPlayer、Windows では MCI、Linux では GStreamer の playbin、ブラウザーでは audio 要素を使います（ブラウザーでは、ユーザーがページ内をタップかクリックするまで再生されません）。音源には、アプリの `assets/` にあるファイル、アプリが書き出したファイル、http(s) の URL（ファイルでもストリームでもかまいません）を指定できます。

`audio.Play(source)` は、音を最初から一度だけ再生し、終わったら解放します。チャイムやクリック音に向いています。それ以上の操作が必要なら、`audio.Load` で `*audio.Player` を作ります。

```go
func Sounds() ui.Node {
	music := audio.Load("music/theme.m4a")
	music.SetLoop(true)
	music.SetVolume(0.5)
	signal.OnCleanup(music.Release)
	return ui.Column(nil,
		ui.Button(ui.ButtonProps{Title: ui.Val("Ding"), OnPress: func() { audio.Play("sounds/ding.wav") }}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Play"), OnPress: music.Play}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Pause"), OnPress: music.Pause}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Restart"), OnPress: func() { music.Seek(0); music.Play() }}),
	)
}
```

`Player` のメソッドは次のとおりで、どれもメインスレッドで呼びます。`Play`（現在の位置から再生します。最後まで再生し終えたあとは最初から）、`Pause`、`Seek(seconds)`、`SetVolume`（0 から 1 で、既定は 1）、`SetRate`（0.25 から 4 で、音程は変わりません）、`SetLoop`、`Status(done)`（`Position`、`Duration`、`Playing`、`Ended` を返します。再生の進み具合を追うには、1 秒に数回読みます）、そして `Release` です。`Release` は再生を止め、プレイヤーが確保しているものを解放します。不要になったプレイヤーは解放してください。

iOS では、音楽アプリやゲームと同じように、消音スイッチがオンでも音が鳴ります。また、ほかのアプリの音とは混ざりません。

### 再生中の表示とメディアキー: asuka/sdk/nowplaying

`asuka/sdk/nowplaying` は、アプリが再生しているものを、システムがメディアを表示する場所に表示し、そこにあるコントロールの操作をアプリに伝えます。表示される場所は、iOS ではロック画面、コントロールセンター、Dynamic Island、CarPlay、Android では通知シェード、ロック画面、Android Auto、Wear OS、Mac ではコントロールセンターとメニューバー、Windows ではメディアフライアウト、Linux では GNOME や KDE のメディアコントロール、そしてブラウザーのメディアコントロールです。キーボードやヘッドセットのメディアキーからも同じ操作が届きます。`Show` はコンポーネントの中で呼びます。アクセサの変化に追従し、コンポーネントのオーナーが破棄されると消えます。

```go
func Episode(ep *Episode) ui.Node {
	state := signal.New(nowplaying.Paused)
	position := signal.New(0.0) // 秒。再生位置が飛んだときに設定する
	nowplaying.Show(nowplaying.Props{
		Title:          ui.Val(ep.Title),
		Artist:         ui.Val(ep.Show),
		Artwork:        ui.Val(ep.Cover), // アセット、ファイル、または http(s) の URL
		Duration:       ui.Val(ep.Seconds),
		State:          state,
		Position:       position,
		OnPlay:         func() { player.Play(); state.Set(nowplaying.Playing) },
		OnPause:        func() { player.Pause(); state.Set(nowplaying.Paused) },
		OnSeek:         func(at float64) { player.Seek(at); position.Set(at) },
		OnSkipForward:  func(s float64) { skip(s) },
		OnSkipBackward: func(s float64) { skip(-s) },
		SkipInterval:   30,
	})
	// ...
}
```

表示されるのは `Title`、`Artist`、`Album`、`Artwork`（512 ピクセル以上の正方形の画像なら、どこでもくっきり表示されます）、`Duration`（ライブストリームでは 0 にします。その場合はシークできません）、`State`（`Playing`、`Paused`、`Stopped`）、`Position`、`Rate` です。再生中は、システムが再生速度に合わせて位置を自動で進めます。パッケージが `Position` を送り直すのは、再生位置が飛んだとき（シークや途切れ）か、状態や速度が変わったときだけです。そのため、1 秒に数回読むプレイヤーの状態に `Position` を追従させても、コストはかかりません。

各操作は、そのハンドラーを設定すると有効になります。設定していない操作はグレー表示になるか、表示されません。`OnPlay`、`OnPause`、`OnTogglePlayPause` は互いの代わりになるので（ヘッドセットのボタンは一つで再生と一時停止を切り替え、ロック画面は再生か一時停止のどちらかを表示します）、どれか一つを設定すれば三つとも有効になります。`OnSeek` を有効にするには、`Duration` がわかっている必要があります。`OnSkipForward` と `OnSkipBackward` は `SkipInterval`（既定は 15 秒）ずつスキップします。iOS と Mac では「次へ」「前へ」の代わりに表示され、Android では `OnNext` や `OnPrevious` がないときにその位置に表示されます。

`asuka/sdk/audio` のプレイヤーなら、`Player` に渡すだけで残りを任せられます。状態、位置、長さはそのプレイヤーから取得され（状態は 1 秒に一度読まれ、プレイヤーに対して実行した操作はすぐに反映されます）、再生、一時停止、停止、シーク、そして（`SkipInterval` があれば）スキップは、ハンドラーを設定しない限りそのプレイヤーに対して実行されます。

```go
music := audio.Load("music/spring.mp3")
signal.OnCleanup(music.Release)
nowplaying.Show(nowplaying.Props{
	Title:   ui.Val("Spring"),
	Artist:  ui.Val("Vivaldi"),
	Artwork: ui.Val("covers/spring.jpg"),
	Player:  music,
	OnNext:  playNext,
})
```

複数をマウントすることもできます。表示されるのは最後にマウントしたもので、それが消えると一つ前のものが表示されます。同じ更新の中で別のものに置き換わった場合（ある曲のコンポーネントが次の曲のコンポーネントに入れ替わるときなど）は、そのあいだもシステムのコントロールはアプリのものとして保たれます。

iOS では、アプリに `audio` バックグラウンドモードが付与されるので、画面をロックしても再生が続きます。また、オーディオセッションは playback カテゴリになります。iOS は、オーディオセッションが再生中のアプリを表示するためです。Android では、MediaSession とそのメディアスタイルの通知を使います（メディアセッションの通知には、通知の権限は要りません）。再生中は、Android 14 の要件どおり、メディア再生タイプのフォアグラウンドサービスがアプリを画面外でも動かし続けます。一時停止中は通知が残り、スワイプで消せます。どちらの設定も、このパッケージを使うアプリにだけ `asuka build` が追加します。`examples/player` は、このパッケージでアルバムを再生する例です。

## ネットワークの状態: asuka/sdk/network

`asuka/sdk/network` は、デバイスのネットワークの状態を伝えます。オンラインかどうか、Wi-Fi とモバイル通信のどちらでつながっているか、通信に料金がかかるかどうかです。大きなファイルは Wi-Fi のときだけダウンロードする、オフラインであることを表示する、といった用途に使います。実装は、iOS と Mac では NWPathMonitor、Android では ConnectivityManager、Windows では Network List Manager、Linux では GNetworkMonitor と NetworkManager、ブラウザーでは `navigator.onLine` です。ブラウザーで通信の種類がわかるのは、それを取得できる環境（Android の Chrome）だけです。

`network.Use()` は、ネットワークの状態をシグナルとして返します。コンポーネントの中で使い、コンポーネントが生きているあいだ変化に追従します。

```go
func Status() ui.Node {
	net := network.Use()
	return ui.Text(signal.Func(func() string {
		n := net.Get()
		if !n.Connected {
			return "Offline"
		}
		if n.Metered || n.Constrained {
			return n.Type.String() + ": large downloads wait for Wi-Fi"
		}
		return n.Type.String()
	}))
}
```

`network.State` は次のフィールドを持ちます。

- `Connected`：接続しているかどうか。
- `Type`：`network.None`、`WiFi`、`Cellular`、`Ethernet`、`Other`（VPN だけの接続、Bluetooth など）のいずれか。`String` は `"none"`、`"wifi"`、`"cellular"`、`"ethernet"`、`"other"` を返します。
- `Metered`：モバイルデータ通信やスマートフォンのインターネット共有のように、通信に料金がかかるか、量に制限があること。
- `Constrained`：ユーザーがアプリにデータ使用量を減らすよう求めていること（iOS の「省データモード」、Android の「データセーバー」、ブラウザーのデータセーバー）。Windows では、通信量がデータ制限に近づいているか、超えていることも表します。Linux では常に false です。

プラットフォームから最初の応答があるまで、`Use` の値は `Connected` で、`Type` は `Other` です。

`network.Current()` は現在のネットワークの状態を返します（メインスレッド以外から呼びます）。`network.Watch(f)` は、まず現在の状態で、その後は変わるたびに、メインスレッドで `f` を呼びます。返される `stop` を呼ぶまで続きます。

## ライブラリへの保存: asuka/sdk/library

`asuka/sdk/library` は、ユーザーやほかのアプリから見える場所にファイルを保存します。動画はギャラリーに、曲は音楽ライブラリに、どんなファイルでも「ダウンロード」に保存できます。

```go
library.Save(file, library.Options{Collection: library.Movies, Folder: "My App", Move: true},
	func(where string, err error) {
		if err != nil {
			status.Set("Couldn't save: " + err.Error())
			return
		}
		status.Set("Saved")
	})
```

`library.Save(path, o, done)` は、ファイルを `o.Collection` にメインスレッドの外でコピーし（大きな動画だと時間がかかるためです）、終わるとメインスレッドで、保存先を渡して `done` を呼びます。必要な権限は初回に求めます。`done` は nil でもかまいません。

`library.Options` は次のとおりです。

- `Collection`：`library.Downloads`（既定）、`Movies`、`Music`、`Pictures` のいずれか。
- `Name`：保存先でのファイル名（既定は元のファイル名）。同じ名前がすでにあれば番号が付きます。
- `Folder`：コレクションの中に作る、アプリのファイル用のフォルダー。空ならコレクションの直下に保存します。
- `MIME`：ファイルの種類（既定は拡張子から判断します）。
- `Move`：保存できたら元のファイルを削除します。アプリのキャッシュにダウンロードしたファイルを、コピーを二重に残さずに保存するときに使います。

各コレクションの保存先は次のとおりです。

| コレクション | Android | iOS | macOS | Windows、Linux |
| --- | --- | --- | --- | --- |
| `Downloads` | ダウンロード | アプリの Documents（「ファイル」App に表示） | `~/Downloads` | ユーザーのダウンロード |
| `Movies` | ギャラリーの動画 | 写真 | 写真 | ユーザーのビデオ |
| `Music` | ミュージック | アプリの Documents（「ファイル」App に表示） | `~/Music` | ユーザーのミュージック |
| `Pictures` | ギャラリーの画像 | 写真 | 写真 | ユーザーのピクチャ |

Windows と Linux、そして Mac の `~/Downloads` と `~/Music` では、ファイルはユーザーのフォルダーの中の `Folder` に保存され、そのパスが返されます。Linux のフォルダーは XDG の規定に従います。ブラウザーでは、コレクションにかかわらずファイルをダウンロードします。保存先は、ブラウザーがダウンロードを置く場所です。

**Android** では、Android 10 以降は権限なしで MediaStore のコレクションを使い、`content://` URI を返します。Android 10 より前は、ストレージの権限を求めてから共有フォルダーに書き込みます。

**iOS** では、「写真」への追加の許可を求め（追加のみで、アプリはライブラリを読めません）、アセットの識別子を返します。「写真」では `Folder` は使われません。それ以外のコレクションはアプリの Documents の中の `Folder` に保存され、ファイルのパスが返されます。Asuka は、Documents が「ファイル」App に表示されるように設定します。

## バックグラウンド処理: asuka/sdk/background

`asuka/sdk/background` を使うと、アプリが画面に出ていないあいだも処理を続けられます。ユーザーが始めた、数分かかるダウンロード、アップロード、書き出しなどに使います。

`background.Start` はタスクを開始します。`End` を呼ぶまでアプリは動き続け、そのあいだ `Options` の内容を通知に表示します。ユーザーが通知のボタンをタップすると、`onAction` がメインスレッドでそのボタンの ID を受け取ります。`task.Update` は新しいオプションを表示するもので、処理が進むにつれて進み具合を更新するのに使います。タスクは、ユーザーが処理を求めたときに、アプリが画面に出ているうちに開始してください。

```go
var cancelled atomic.Bool
task := background.Start(background.Options{
	Title:    "Exporting",
	Progress: -1,
	Actions:  []background.Action{{ID: "cancel", Title: "Cancel"}},
}, func(action string) {
	if action == "cancel" {
		cancelled.Store(true)
	}
})
go func() {
	for i := 1; i <= 10 && !cancelled.Load(); i++ {
		time.Sleep(time.Second) // 実際の処理
		p := float64(i) / 10
		ui.Post(func() {
			task.Update(background.Options{Title: ui.Val("Exporting"), Text: fmt.Sprintf("%.0f%%", p*100), Progress: p,
				Actions: []background.Action{{ID: "cancel", Title: "Cancel"}}})
		})
	}
	ui.Post(func() {
		task.End()
		if cancelled.Load() {
			status.Set("Cancelled")
		}
	})
}()
```

`background.Options` は次のとおりです。

- `Title` と `Text`。
- `Progress`：0 から 1。負の値は、処理中だが進み具合はわからないことを表します。
- `Actions`：通知のボタン。それぞれ `ID` と `Title` を持ちます。

`Start`、`Update`、`End` はメインスレッドで呼び、処理そのものはゴルーチンで行います。`Update` のたびにオプションはすべて置き換わるので、`Actions` も毎回指定してください。[`examples/downloader`](../../examples/downloader) は、タスクの中でダウンロードし、一時停止、再開、キャンセルができる例です。

**Android** は、バックグラウンドに回ったアプリを数分以内に停止します。タスクがあるあいだは、（データ同期タイプの）フォアグラウンドサービスでアプリを動かし続け、CPU もスリープさせません。タスクごとに常駐する通知が表示され、進み具合と最大三つのボタンが並びます。Android 13 以降では、ユーザーがまだ通知を許可していなければ、タスクの開始時に許可を求めます。許可してもしなくても処理は続きます。バックグラウンドにいるアプリはタスクを開始できません。Android 15 では、データ同期サービスは 1 日 6 時間までに制限されます。Google Play は、アプリのデータ同期フォアグラウンドサービスを何に使うのかを Play Console で尋ねるので、アップロードの前に申告してください。

**iOS** では、タスクがあるあいだ、バックグラウンドに回ったアプリに 30 秒ほどの猶予が与えられます。通知は表示されないので、`Update` は何もせず、`onAction` も呼ばれません。時間切れになると iOS がタスクを終わらせ、処理が止まることがあります。

**デスクトップ**のアプリは、画面に出ていなくても動き続けます。タスクがあるあいだは、システムがアプリを遅くしたり停止したりしないようにします（Mac の App Nap、Windows と Linux のスリープ）。通知は表示されないので、`Update` は何もせず、`onAction` も呼ばれません。**ブラウザー**は、タスクの有無にかかわらず、開いているあいだはページを動かします。

## ほかのアプリからの共有: ui.OnShare

`ui.OnShare(f)` は、ほかのアプリが共有シートからこのアプリに共有したものを渡して `f` を呼びます。YouTube やブラウザーからのリンク、メモからのテキスト、ギャラリーからの写真、「ファイル」からの PDF などです。これは iOS と Android の機能です。デスクトップとブラウザーでは、アプリがほかのアプリの共有メニューに表示されず、`f` も呼ばれません。アプリが何の共有先として表示されるかは、`asuka.json` の `shareTarget` で指定します。

```json
{ "shareTarget": true }
```

こう書くと、テキストとリンクを受け取ります。オブジェクトを指定すると、MIME タイプごとにファイルも受け取れます。一度に受け取れるのは `max` 個までです（既定は 1）。

```json
{ "shareTarget": { "types": ["text", "image/*", "video/*"], "max": 10 } }
```

タイプには `"text"`（テキストと Web のリンク）か、MIME タイプを指定します。MIME タイプは `"image/*"`、`"video/*"`、`"audio/*"`、`"application/pdf"` などで、`"*/*"` はあらゆるファイルを表します。

```go
func App() ui.Node {
	link := signal.New("")
	photos := signal.New([]string(nil))
	ui.OnShare(func(items []ui.SharedItem) {
		for _, it := range items {
			switch it.Kind {
			case ui.SharedLink:
				link.Set(it.Text)
			case ui.SharedImage:
				photos.Set(append(photos.Peek(), it.Path))
			}
		}
	})
	return ui.Text(link)
}
```

各 `ui.SharedItem` には `Kind` があり、`SharedText`、`SharedLink`、`SharedImage`、`SharedVideo`、`SharedAudio`、`SharedFile` のいずれかです。テキストやリンクは `Text` に入ります。ファイルは `Path` にあり、これはアプリのキャッシュにある、アプリ専用のコピーです。アプリで保管する場所に移すか、使い終わったら削除してください。ファイルには、共有元のアプリでの名前 `Name`（`IMG_0042.HEIC` など）、`MIME` タイプ、`Size`（不明なら -1）も付いてきます。項目は、共有元のアプリが渡した順に届きます。ブラウザーから共有されたページは、タイトル（テキスト）とリンクの二つの項目として届きます。

ハンドラーが設定される前に届いたもの、つまりアプリを起動した共有は、最初に設定されたハンドラーに渡されます。ハンドラーはコンポーネントのオーナーとともに止まります。アプリを開かずに、共有されたものに対してアプリの画面を表示するには、後述の共有シートを参照してください。アプリの中で画面を開くには、[ナビゲーション](navigation.md)を参照してください。

**Android** では、タイプごとのインテントフィルターによって、共有されたものとともにアプリ自体が開きます（`ACTION_SEND`。`max` が 1 より大きければ `ACTION_SEND_MULTIPLE` も）。アプリは、ファイルへのアクセス権が有効なうちに、`OnShare` に渡す前にファイルをキャッシュにコピーします。

**iOS** では、アプリに共有拡張機能（Share Extension）が入り、Android と同じように、共有されたものとともにアプリを開きます。拡張機能は独自の画面を表示しません。拡張機能のアクティベーションルールはタイプから作られます。テキスト、リンク、画像、動画、任意のファイルには Apple が用意したキーを使い、これらでは `max` が効きます。それ以外（オーディオや PDF）には添付ファイルのタイプに対する述語（predicate）を使い、こちらでは `max` は効きません。拡張機能は渡されたものを何でも受け取るので、`Kind` と `MIME` を確認してください。ファイルは App Group `group.<app ID>` を通じて拡張機能からアプリに渡ります。実機向けビルドのプロビジョニングプロファイルには、この App Group が含まれている必要があります（[配布](shipping.md#ios)を参照）。

`asuka dev` では、アプリの `shareTarget` にかかわらず、プレイヤーアプリがあらゆる共有を受け取ります。Android ではファイルのパスはデバイス上のもので、Go のコードが動いている Mac 上のものではありません。ファイルの共有を試すには `asuka dev --platform android --once` を使ってください。

リンクの先が何か（ページ、画像、動画）を調べるのはアプリの役目で、`net/http` を使います。expo-sharing の「resolved」ペイロードに当たる処理を、Go で書くことになります。

### アプリ独自の共有シート: app.ShareSheet

既定では、共有するとアプリが開きます。`app.ShareSheet(screen)` を使うと、代わりにアプリ独自の画面を、共有元のアプリの上に表示します。「Raindrop に保存」や「メモに追加」と同じ形で、ユーザーは保存したらすぐ元の場所に戻れます。`app.Run` の前に呼んでください。

```go
func ShareSheet(s *ui.ShareSheet) ui.Node {
	note := signal.New("")
	return ui.Column(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Text(signal.Static(s.Items[0].Text)),
		ui.TextField(ui.TextFieldProps{Value: note, Placeholder: ui.Val("Note"), Multiline: true}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), OnPress: s.Cancel}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
				save(s.Items, note.Peek())
				s.Close()
			}}),
		),
	)
}

func main() {
	app.ShareSheet(ShareSheet)
	app.Run(App)
}
```

`s.Items` は共有されたもので、`OnShare` が受け取るものと同じです。シートは `s.Close()`（完了）、`s.Cancel()`、`s.OpenApp()` のいずれかで終わります。`s.OpenApp()` は、シートがない場合と同じように、項目を `OnShare` に渡してアプリを開きます。項目が届くまで（動画をコピーしているあいだなど）は、スピナーが表示されます。

iOS が共有拡張機能を実行するのと同じように、シートは独自のプロセスの中の、独立したルートです。**iOS** ではアプリの共有拡張機能で、iOS がシートとして表示します。拡張機能は、アプリと共有するフレームワークから、アプリの Go のコードとホストを実行します。**Android** では `:share` プロセスの Activity で、ほかのアプリの上に Material のボトムシートとして表示されます（下にドラッグする、外側をタップする、戻る操作をするとキャンセルされます）。プロセスはシートとともに終わります。シートが閉じるときに `asuka/sdk/store` の保留中の内容が書き込まれ、`app.OnLifecycle` には `Background` が届きます。ファイルの `Path` はシートが閉じるまでしか使えないので、アプリで残すものはコピーしてください。

アプリが保存するデータは、シートからも同じように使えます。`asuka/sdk/store`、`store.Dir()` にある SQLite のファイル（`sqlite.Open("app.db")`）、`asuka/sdk/secure` です。Android では、二つのプロセスがアプリのファイルを共有します。iOS では、シート付きで初めて起動したときに一度だけ、アプリのデータが App Group（`group.<app ID>`）に移され、キーチェーングループも拡張機能と共有されます。アプリが動いているあいだにシートが書き込んだ内容は、アプリには通知されません。アプリが前面に戻ったときに読み直してください（`app.OnLifecycle`、`db.Changed()`）。

CLI は、アプリの Go のコードが `app.ShareSheet` を呼んでいることを検出し、アプリを開く拡張機能の代わりにシートをビルドします。このとき iOS では、アプリの Go のコードとホストはアプリ内のフレームワーク `Frameworks/AsukaApp.framework` になり、Xcode が拡張機能とコードを共有するのと同じように、アプリと拡張機能の両方がそれを読み込みます。そのため、シートがあってもアプリのサイズは増えません。`asuka dev` でも、プレイヤーアプリのシートが、Mac 上で動くアプリのシートを実行します。[`examples/bookmarks`](../../examples/bookmarks) は Raindrop のようなシートの例で、ページのタイトル、説明、画像、メモ、コレクション、タグを扱います。

## JavaScript: asuka/sdk/js

`asuka/sdk/js` は、プラットフォーム自身のエンジンを使い、画面に出さずに JavaScript を実行します。使うのは一度も表示されない Web ビュー（iOS と Mac では WKWebView、Android では WebView、Windows では WebView2、Linux では WebKitGTK）で、ブラウザーでは非表示でサンドボックス化されたフレームです。JavaScript でしか書かれていないコード、たとえば動画の URL に署名する関数を持つ、サイトのプレイヤーのようなコードを動かすためのものです。

`js.Eval(ctx, script)` は、スクリプトを専用の新しいページで、`<script>` の中身と同じく文の並びとして実行し、その値を JSON で返します。値は最後の文の値か、スクリプトが返す Promise の解決値です（`undefined` は `null` になります）。例外が投げられたり Promise が reject されたりすると、それが `*js.Error` 型のエラーになります。ページは実行後に破棄されるので、あるスクリプトが設定したものは次のスクリプトからは見えません。メインスレッド以外から呼びます。

```go
go func() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := js.Eval(ctx, `const xs = [1, 2, 3].map(x => x * 2); xs`)
	fmt.Println(string(v), err) // [2,4,6] <nil>

	s, err := js.EvalString(ctx, `new Promise(done => setTimeout(() => done("later"), 100))`)
	fmt.Println(s, err) // later <nil>
}()
```

`js.EvalString` は、値が文字列になるスクリプト用の `Eval` です。スクリプトは `ctx` の期限まで、期限がなければ `js.DefaultTimeout`（1 分）のあいだ実行できます。時間切れになると `"timeout"` で失敗します。Android では、投げられたエラーのメッセージにスタックが含まれます。

ユーザーに見えるページの中でスクリプトを実行するには、`ui.WebView` を使います（[Web ページ](components.md#web-ページ)）。

## プッシュ通知: asuka/sdk/push

`push.Register()` は、（初回は）ユーザーに通知の許可を求め、デバイスを Apple（APNs）か Google（Firebase Cloud Messaging）のサービスに登録して、`Token`、`Service`（`"apns"`、`"fcm"`）、通知が許可されているか（`Allowed`）を返します。トークンを自分のサーバーに送り、サーバーからそのトークン宛てに通知を送ります。トークンは変わることがあるので、起動のたびに登録してください。メインスレッド以外から呼びます。Mac も iOS と同じように APNs に登録します（プロビジョニングプロファイルが必要です。[Mac の場合](#mac-の場合)を参照）。Web では Web Push を使います（`Service` は `"webpush"` で、トークンはブラウザーのサブスクリプションです。[Web](web.md) を参照）。Windows と Linux にはアプリ向けのプッシュサービスがないため、`Register` は失敗します。

```go
func App() ui.Node {
	push.OnOpen(func(m push.Message) { openThread(m.Data["thread"]) })
	go func() {
		if r, err := push.Register(); err == nil {
			api.SaveDeviceToken(r.Service, r.Token)
		}
	}()
	return home()
}
```

- `push.OnReceive(f)` は、アプリの実行中に届いた各メッセージを受け取ります。タイトルか本文があるメッセージは、フォアグラウンドでも通知として表示されます。データメッセージ（サイレントプッシュ）はここにだけ届きます。
- `push.OnOpen(f)` は、ユーザーがタップした各通知を受け取ります。アプリの起動時に `f` を設定しておけば、アプリを起動した通知も含まれます。メッセージのデータの `"url"` は、`asuka/sdk/notify` の通知と同じように `ui.OnOpenURL` にも渡されます。
- `push.Message` には `ID`、`Title`、`Body`、`Data` があります（`Data` は、iOS ではペイロードの `aps` 以外のキー、Android ではメッセージのデータです）。

iOS のリリースビルドでは、App ID に Push Notifications ケイパビリティが必要です。シミュレーターでもトークンを取得できます。Android では Firebase プロジェクトが必要です。プロジェクトに Android アプリを追加し、その `google-services.json` をダウンロードして、`asuka.json` で指定します。

```json
{ "android": { "googleServices": "google-services.json" } }
```

Asuka は、Google の Gradle プラグインと同じようにこのファイルの設定をアプリに書き込みます。このファイルがないと、`Register` は対処方法を示すエラーで失敗します。

### プッシュ通知のカテゴリ、スレッド、チャンネル

プッシュ通知でも、`asuka/sdk/notify` のカテゴリ（そのアクションと `notify.OnAction`）、スレッド、チャンネル、バッジ、`Present` を使えます。ペイロードで次のように指定します。

- **iOS**：APNs の仕様どおり、`aps` の `category`、`thread-id`、`badge`。
- **Android**：通知メッセージでは `android.notification.channel_id` と `image`、データメッセージでは `"title"`、`"body"`、`"subtitle"`、`"category"`、`"thread"`、`"summary"`、`"channel"`、`"image"`、`"badge"`、`"url"`。`"title"` か `"body"` を持つデータメッセージは通知として表示されます（Asuka がアクション付きで通知を組み立てます）。どちらも持たないデータメッセージはサイレントです（`OnReceive` にだけ届きます）。アプリがバックグラウンドにあるときに届いた通知メッセージは Firebase が表示するので、アクションもスレッドも付きません。それらが必要なら、データメッセージを送ってください。

`push.Message` には、これらに対応する `Subtitle`、`Category`、`Thread`、`Channel`、`Image`、`Badge`、`Summary` もあります。

### 表示前のプッシュ通知の書き換え

`push.Rewrite(f)` を使うと、各プッシュ通知が表示される前に、アプリの Go のコードで内容を書き換えられます。エンドツーエンドで暗号化されたペイロードを復号する、タイトルと本文を設定する、画像を添付する、送信者の写真付きの会話のメッセージにする、スレッドとカテゴリを設定する、といったことができます。`main` の中で、`app.Run` の前に呼んでください。

```go
func main() {
	push.Rewrite(func(m *push.Message) error {
		text, from, err := decrypt(m.Data["box"]) // asuka/sdk/secure に保存した鍵で復号する
		if err != nil {
			return err // プッシュ通知を届いたまま表示する
		}
		m.Body = text
		m.Category = "message"
		m.From(notify.Person{ID: from.ID, Name: from.Name, Image: from.Avatar},
			notify.Conversation{ID: m.Data["chat"]})
		return nil
	})
	app.Run(App)
}
```

`f` はメインスレッドではなく、専用のゴルーチンで実行されます。エラーを返すかパニックすると、プッシュ通知は届いたまま表示されます。`m.Image` はダウンロードされて添付されます。`m.From` は、`notify.Notification` の `From` と同じように、通知をメッセージにします。

**iOS** では、Notification Service Extension の中で書き換えます。`asuka build` は、`push.Rewrite` を呼ぶアプリにこの拡張機能を作ります。拡張機能は、`aps` に `"mutable-content": 1`（と、書き換えに失敗したときに表示される `alert`）を持つプッシュ通知が届くたびに、専用のプロセスでアプリの Go のコードを実行します。まず `main` が、次に `f` が実行されます。ルートコンポーネントは実行されないので、`main` は軽くしておいてください。iOS が与えるのは 30 秒ほどの時間と 24 MB のメモリで、時間切れになるとプッシュ通知は届いたまま表示されます。アプリが保存するデータは、拡張機能からも同じように使えます。データ（`asuka/sdk/store`、`store.Dir` 内のファイル）は両者が共有する App Group に、キーチェーンの項目（`asuka/sdk/secure`）はアプリのグループにあります。リリースビルドには、拡張機能の bundle ID（アプリの ID に `.notification` を付けたもの）のプロビジョニングプロファイルが必要で、App Groups ケイパビリティとアプリのグループを含んでいなければなりません。プロファイルは `asuka.json` の `ios.notificationProfile` で指定でき、指定しなければ、インストールされているもののうち最も合うものを使います。iOS では、書き換えによってプッシュ通知を非表示にすることはできません。

**Android** では、FCM のデータメッセージをアプリのプロセスの中で書き換えます。アプリが動いていなければ Go のコードが画面なしで起動し、10 秒ほどのうちに書き換えて、通知が表示されます。書き換えた結果、タイトルも本文もなくなったデータメッセージは表示されません。

`asuka dev` のプレイヤーアプリには拡張機能がないので（アプリの Go のコードはコンピューター上で動いています）、プッシュ通知は届いたまま表示されます。`asuka dev --platform ios --once` やビルドしたアプリには拡張機能があります。シミュレーターが拡張機能を実行するのは APNs から届いたプッシュ通知だけで、`xcrun simctl push` では実行されません。

## Apple でサインイン: asuka/sdk/appleauth

`appleauth.Button` は Apple 純正のボタンで、タイトル、ロゴ、色がユーザーの言語に合わせて表示されます。`appleauth.SignIn` はシステムのシートを表示します。App Store は、アカウントのサインインにほかの会社のサインインを提供するアプリに、Apple でサインインも提供するよう求めています。

```go
appleauth.Button(appleauth.ButtonProps{
	Type:  appleauth.ContinueButton,
	Style: appleauth.Automatic, // ライトモードでは黒、ダークモードでは白
	OnPress: func() {
		go func() {
			c, err := appleauth.SignIn(appleauth.Options{Scopes: appleauth.FullName | appleauth.Email, Nonce: nonce})
			if err == nil {
				api.SignInWithApple(c.IdentityToken, c.AuthorizationCode)
			}
		}()
	},
})
```

`Credential` の `User` は、アカウントの変わらない ID です。`IdentityToken`（Apple が署名した JWT）と `AuthorizationCode` を自分のサーバーに送り、サーバーで Apple に照会して検証します。`Email`、`GivenName`、`FamilyName` が届くのは、ユーザーが初めてそのアプリにサインインしたときだけなので、そのときに保存してください。`ErrCanceled` は、ユーザーがシートを閉じたことを表します。`appleauth.CredentialState(user)` は、ユーザーがそのアプリでまだ Apple でサインインを使っているかどうかを返します（`Revoked` ならサインアウトさせてください）。Android にはこの機能がありません。`Button` は何も表示せず、`Available` は false で、`SignIn` は `ErrNotAvailable` で失敗します。Windows、Linux、Web でも同じです。これらのプラットフォームでは、Web 経由で Apple でサインインを使ってください（`asuka/sdk/webauth`）。Mac では、プロビジョニングプロファイルがあれば iOS と同じように使えます（[Mac の場合](#mac-の場合)を参照）。

## Face ID と指紋認証: asuka/sdk/biometric

`biometric.Authenticate(options)` はシステムのプロンプト（Face ID、Touch ID、Android の BiometricPrompt、Mac の Touch ID、Windows の Windows Hello、ブラウザーではパスキーの WebAuthn プロンプト）を表示し、デバイスの持ち主であれば nil を返します。そうでなければ `ErrCanceled`、`ErrNotAvailable`（生体認証もパスコードもないか、ロックアウトされている）、`ErrFailed` のいずれかを返します。メインスレッド以外から呼びます。

```go
go func() {
	err := biometric.Authenticate(biometric.Options{Reason: "Unlock your notes"})
	ui.Post(func() { unlocked.Set(err == nil) })
}()
```

- `Reason` は認証する理由です（iOS ではサブタイトル、Android ではタイトルになります）。`Cancel` はキャンセルボタンのタイトルです。
- `NoPasscode` を指定しない限り、ユーザーは代わりにデバイスのパスコード、PIN、パターンを使えます。指定すると生体認証だけになります（Android では、強力な生体認証である Class 3 に限ります）。
- `biometric.Check()` は、デバイスが備えているものを返します。`Kind`（`Face`、`Fingerprint`、`Iris`、`Optic`）、登録済みかどうか（`Enrolled`）、`Passcode` があるかどうかです。

Linux には持ち主を確認するシステムのプロンプトがありません。`Check` は何もないと返し、`Authenticate` は `ErrNotAvailable` で失敗します。

## 録音: asuka/sdk/audio/record

`record.Start(options, done)` は、`record.Stop` を呼ぶまで（または `MaxDuration` に達するまで）マイクから録音し、終わるとファイルを渡して `done` を呼びます。ファイルは MPEG-4 コンテナーの AAC（`.m4a`）でアプリのキャッシュに保存され、`asuka/sdk/audio` で再生できます。録音には、iOS と Mac では AVAudioRecorder、Windows では Media Foundation、Linux では GStreamer、ブラウザーでは `MediaRecorder` を使います（AAC で録音できないブラウザーでは、WebM コンテナーの Opus になります）。初回はマイクの使用許可を求め、拒否されると `done` は `"not allowed"` を受け取ります。

```go
record.Start(record.Options{Quality: record.Voice, MaxDuration: time.Minute}, func(path string, err error) {
	if err == nil {
		memo.Set(path)
	}
})
// あとで
record.Stop()
```

`Voice` は 64 kbit/s のモノラル、`Music` は 128 kbit/s のステレオです。`Pause` と `Resume` で、同じファイルへの録音を一時停止・再開できます。`record.Level()` は 0 から 1 の音量レベルで、ゴルーチンから 1 秒に数回呼んでレベルメーターに使います。`record.CurrentStatus(done)` はメインスレッドから呼び、レベル、いま音を記録しているかどうか、どれだけ記録したかを返します。タイマーに表示すべきなのはこの値です。システムが録音を一時停止することがあり（通話など）、iOS ではシステムが許可すれば、そのあと録音が再開されるためです。iOS では録音中、オーディオセッションが再生と録音の両方を行うモードになり（ほかのアプリの音は止まります）、録音が終わると元に戻ります。

`Options.Stream` を指定すると、ADTS ストリーム（`.aac`）で録音します。ADTS はフレームごとに完結しているので、アプリやスマートフォンが突然落ちても、それまでに録音した分は残ります。一方、MPEG-4 のファイルは閉じるまで読めません。クラッシュしたあとでもファイルを見つけられるよう `Path` を指定しておき、保存用には `record.Finalize(stream, dst)` で `.m4a` にしてください。こうして作ったファイルは正しくシークできます。停止したあとでも、クラッシュしたあとでも同じように使えます。

`Options.Background` を指定すると、アプリが画面に出ていないときや画面がロックされているときも録音を続けます。iOS では `asuka.json` に `"ios": {"backgroundModes": ["audio"]}` が必要です。Android では、マイクタイプのフォアグラウンドサービスを動かし、常駐する通知（`BackgroundTitle`）と停止ボタンを表示します。Google Play はこのサービスを何に使うのかを尋ねるので、アップロードの前に申告してください。デスクトップのアプリはもともと画面外でも録音できるので、`Background` を指定しても何も変わりません。

## 音声の文字起こし: asuka/sdk/transcribe

`transcribe.File(path, options)` は、録音をデバイス上でテキストに変換します。音声がデバイスの外に出ることはありません。デバイスで変換できない場合は、サーバーを使う代わりに `ErrNotAvailable` を返します。メインスレッド以外から呼びます。

```go
go func() {
	r, err := transcribe.File(path, transcribe.Options{Language: "en-US"})
	ui.Post(func() {
		if err == nil {
			text.Set(r.Text)
		}
	})
}()
```

`Result` には `Text` と `Language` があり、iOS と Android 14 では `Segments` もあります。`Segments` は単語の並びで、それぞれ秒単位の `Start` と `Duration` を持ちます。発話のない録音では、`Text` は空になります。`transcribe.Check(language)` は、ユーザーに何も求めずに、デバイスで何ができるかを返します。iOS では Speech フレームワークを使います（モデルは iOS に含まれていますが、シミュレーターのランタイムによっては含まれていません）。初回は音声認識の許可を求めます。Android のオンデバイス認識には Android 13 が必要で、その言語のモデルも必要です。モデルがなければ取得するので、そのあいだは `ErrDownloading` が返ります（あとでもう一度試してください）。Mac でも Speech フレームワークを、デバイス上でのみ使います。Windows、Linux、Web には、アプリが使えるオンデバイスの音声認識がないため、`File` は `ErrNotAvailable` で失敗します。

## 画面: asuka/sdk/screen

```go
screen.Lock(screen.Landscape) // 動画プレイヤー
screen.SetBars(screen.Bars{Style: screen.Light, HideStatus: true})
screen.PreventCapture(true) // チケットのコード
stop := screen.OnScreenshot(func() { note.Set("That screenshot shows your balance") })
```

- `screen.Lock(o)` は、アプリを `o` の向きに回転させ、その向きに固定します。指定できるのは `Portrait`、`PortraitUp`、`Landscape`、`LandscapeLeft`、`LandscapeRight` で、`Any` で固定を解除します。使えるのは `asuka.json` で許可した向きだけです。Split View やステージマネージャを使っている iPad と、Android 16 の大画面デバイスは、この指定を無視します。
- `screen.SetBars(bars)` はステータスバーの `Style` を設定します（`Auto` はダークモードに従い、`Light` は暗い画像の上に置く明るいアイコン、`Dark` は暗いアイコンです）。また、ステータスバー（`HideStatus`）や Android のナビゲーションバー（`HideNavigation`）を隠せます。隠したバーは、画面の端からスワイプすると一時的に表示されます。セーフエリアもそれに合わせて変わります。
- `screen.PreventCapture(on)` は、アプリがスクリーンショットや画面収録に写らないようにします。Android ではウィンドウをセキュアにするので、キャプチャは黒くなり、最近使ったアプリの一覧でも空白で表示されます。iOS ではスクリーンショットを防げませんが、画面収録、ミラーリング、AirPlay のあいだはアプリを覆い隠します。
- `screen.OnScreenshot(f)` は、ユーザーがスクリーンショットを撮ると `f` を呼びます（iOS、Android 14 以降）。返される `stop` を呼ぶまで続きます。

デスクトップのウィンドウには向きもシステムバーもないので、`Lock` と `SetBars` は何もしません。`PreventCapture` は、Mac と Windows では（Linux では効きません）アプリのウィンドウがスクリーンショットや画面収録に写らないようにします。`OnScreenshot` は、スクリーンショットが保存された時点でそれを検知します（Mac では Spotlight で、Windows と Linux では「スクリーンショット」フォルダーで検知します）。ブラウザーで向きを固定できるのは、全画面表示のときかインストールされたアプリの中だけです。バーは全画面表示にすることで隠し、`PreventCapture` と `OnScreenshot` はありません。

これらの設定は画面ごとではなく、アプリ全体のものです。ある画面で設定したら、その画面が閉じるときに元に戻してください（`signal.OnCleanup`）。

## 起動画面: asuka/sdk/splash

`asuka.json` の `splash` は起動画面です。ライトモードとダークモードそれぞれの背景色の中央に、画像（正方形の PNG、ロゴ）を `size` ポイントの幅（既定は 120、最大 160）で表示します。

```json
{ "splash": { "image": "logo.png", "size": 96, "background": "#4A63B5", "darkBackground": "#1B2440" } }
```

起動画面は、アイコンをタップしてからアプリの最初のフレームが出るまで表示されます。iOS では起動画面（Launch Screen）を表示し、続いて最初のフレームの準備ができるまで、その複製をアプリの上に重ねます。Android 12 以降ではスプラッシュ画面（アイコンの円の中央に画像を表示）を、それより前のバージョンではウィンドウの背景を使います。デスクトップと Web には起動画面がないので、`Hold` と `Hide` は何もしません。アプリの読み込み中も起動画面を表示し続けるには、起動時に（ルートコンポーネントの中で）`splash.Hold()` を呼び、準備ができたら `splash.Hide()` を呼びます。

```go
func App() ui.Node {
	splash.Hold()
	go func() {
		load()
		ui.Post(splash.Hide)
	}()
	return home()
}
```

## 地図: asuka/sdk/maps

`maps.View` は地図です。iOS と Mac では Apple マップ、Android では Google マップを表示します。Windows、ブラウザー、Linux では OpenStreetMap のタイルを使い、Windows とブラウザーではホストが、Linux では libshumate が描画します。

```go
region := signal.New(maps.Region{Center: maps.Coordinate{Lat: 37.77, Lng: -122.42}, LatDelta: 0.05, LngDelta: 0.05})
maps.View(maps.Props{
	Region:         region,
	Markers:        ui.Val([]maps.Marker{{ID: "hq", At: maps.Coordinate{Lat: 37.79, Lng: -122.39}, Title: "HQ"}}),
	OnRegionChange: func(r maps.Region) { region.Set(r) },
	OnMarkerPress:  func(id string) { show(id) },
	Style:          ui.Style{Grow: 1},
})
```

- `Region` は表示する範囲で、中心と、その範囲が覆う緯度・経度の幅（度）で表します。設定すると、地図がその範囲に移動します。`OnRegionChange` は、ユーザーが地図を動かし終えたときに、その範囲を受け取ります。
- `Markers` はピンで、`Title`、`Subtitle`、`Color` を持ちます。タップされたピンの `ID` は `OnMarkerPress` が、ユーザーが地図をタップした位置は `OnPress` が受け取ります。
- `Type` は `Standard`、`Satellite`、`Hybrid`、`Muted`（iOS）のいずれかです。`ShowsUser` は、位置情報の権限があればユーザーの現在地を表示します（`asuka/sdk/location` をインポートし、先に権限を求めてください）。`NoInteraction` を指定すると、操作できない静止したプレビューになります。
- 地図には固有のサイズがありません。サイズを指定するか、`Grow` を設定してください。

Android では、Maps SDK for Android を有効にした Google Maps Platform の API キーを、`asuka.json` で `{"android": {"mapsKey": "..."}}` のように指定する必要があります。キーがないと地図にその旨が表示され、`OnError` が理由を受け取ります。Windows、Linux、Web では、Satellite と Hybrid も通常の地図になります（無料の航空写真がないためです）。`ShowsUser` は、Windows と Linux では何も表示せず、ブラウザーではページが位置情報の権限を得ると、ユーザーの現在地を表示します。`Muted` は、iOS と Web では落ち着いた色合いの地図になります。Linux では libshumate（`libshumate-1.0-1`）が必要です。タイルは、`tile.openstreetmap.org` の利用規約に従ってそこから取得し、地図上にクレジットを表示します。Web アプリでは、独自のサーバーからタイルを取得することもできます（[Web](web.md)）。

## バックグラウンド更新: asuka/sdk/periodic

`asuka/sdk/periodic` は、アプリが画面に出ていないあいだに、ときどきアプリの Go のコードを実行します。同期や新着の取得などに使います。`Handle` はルートコンポーネントの中でタスクのコードを設定し、`Schedule` はシステムにその実行を依頼します。

```go
func App() ui.Node {
	periodic.Handle("sync", func(ctx context.Context) error {
		return syncInbox(ctx)
	})
	periodic.Schedule("sync", periodic.Options{Every: time.Hour, Network: true})
	return home()
}
```

いつ実行するかは、`Every`（最短 15 分）、バッテリー、ネットワーク、ユーザーのアプリの使い方をもとにシステムが決めます。指定よりずっと長く待つこともあり、ユーザーがオフにすることもできます（iOS の「Appのバックグラウンド更新」）。`Network` を指定するとオンラインのときだけ、`Charging` を指定すると充電中だけ実行します。`ctx` はシステムの期限より前に終了します。期限は iOS では約 25 秒（`Charging` を指定したときに使う processing タスクでは数分）、Android（WorkManager）では 9 分です。エラーを返すと、Android はタスクを再試行します。

アプリが閉じているときにシステムがタスクを実行すると、アプリの Go のコードが画面なしで起動します。`main`、（`Handle` を呼ぶ）ルートコンポーネントの順に実行され、そのあとタスクが実行されます。そのときユーザーがアプリを開くと、アプリの画面が表示されます。`Cancel(name)` はタスクを止めます。

デスクトップは、タスクのためにアプリを起動しません。macOS（`NSBackgroundActivityScheduler`）、Windows、Linux では、アプリの実行中にタイマーでタスクを実行し、電源を必要とするタスクはバッテリー駆動中は待機します。ブラウザーは閉じているページのタスクを実行しないので、`Schedule` は失敗します。

## ストア: asuka/sdk/review, asuka/sdk/tracking, asuka/sdk/integrity, asuka/sdk/agerange

`review.Request(done)` は、ストアに評価のプロンプト（App Store の評価プロンプト、Google Play のアプリ内レビュー）を表示するよう依頼します。表示するかどうかはストアが独自の回数制限に基づいて決め、表示したかどうかは知らされません。何かがうまくいったあとに依頼し、ボタンからは呼ばないでください。「評価する」ボタンには `ui.OpenURL(review.AppStoreURL(id))` か `review.PlayStoreURL(id)` を使います。

`tracking.Request()` は iOS の App Tracking Transparency のプロンプトを表示し、`Allowed`、`Denied`、`Restricted`、`NotAsked` のいずれかを返します。アプリは、広告識別子を読んだり広告ネットワークとデータを共有したりする前に、このプロンプトを表示しなければなりません。`tracking.Current()` は、尋ねずに現在の状態を読みます。Android にはこのようなプロンプトがないので、常に `Allowed` です。メインスレッド以外から呼びます。

`asuka/sdk/integrity` は、リクエストが実機上の本物のアプリから来たことを、自分のサーバーに証明します。iOS では App Attest を使います。`GenerateKey` で鍵を作り（ID を保存しておきます）、`AttestKey(id, challenge)` でサーバーのチャレンジに対して一度だけ Apple に鍵を証明してもらい、以後は `Assert(id, data)` で各リクエストに署名します。Android では Play Integrity を使います。起動時に `Prepare(cloudProjectNumber)` を呼び、リクエストごとに `Token(requestHash)` を呼びます。返された値は、サーバーが Apple や Google に照会して検証します。iOS のシミュレーターでは、`Supported` は false です。

`agerange.Request(13, 18)` は、生年月日を聞かずにユーザーの年齢層を尋ねます。子どもの利用を把握するようアプリに求める法律への対応に使うもので、Apple の Declared Age Range（iOS 26）と Play の Age Signals を使います。プラットフォームが、ユーザーやその保護者に共有するかどうかを尋ねることがあります。`Result` は、`Lower` と `Upper` の年齢（0 は制限なし）を持つ `Shared`、`Declined`、`VerificationRequired`（Play）のいずれかで、`Source` はその年齢をどうやって知ったのかを示します。プラットフォームが答えられない場合は `ErrNotAvailable` になります。

スマートフォン以外では次のとおりです。Mac には App Store の評価プロンプト、トラッキングのプロンプト、App Attest があります（プロビジョニングプロファイルが必要です。[Mac の場合](#mac-の場合)を参照）が、Declared Age Range はありません。Windows、Linux、Web には、ストアのプロンプト（`review.Request` は何も表示せず、ブラウザーでは `done` がエラーを受け取ります）、アテステーション（`integrity.Supported` は false）、年齢層（`ErrNotAvailable`）のいずれもありません。これらのプラットフォームではトラッキングにプロンプトは不要です。Windows では「設定」で広告 ID がオンになっていれば `Allowed`、Linux では `Allowed`、ブラウザーでは Global Privacy Control か Do Not Track を送っていれば `Denied` を返します。

## モーションとセンサー: asuka/sdk/sensors

```go
stop := sensors.WatchMotion(50*time.Millisecond, func(m sensors.MotionReading) {
	level.Set(m.Attitude.Roll) // 水準器
})
```

- `WatchAccelerometer`、`WatchGyroscope`、`WatchMagnetometer` は、ほぼ指定した間隔ごとに `Vector`（x、y、z）を渡します。単位は、加速度が g、回転が毎秒のラジアン、磁場がマイクロテスラ（キャリブレーション済み）です。
- `WatchMotion` は、複数のセンサーを統合したデバイスの動きです。`Gravity` とユーザーによる `Acceleration`（g 単位）、`Rotation`、そして `Attitude` を渡します。`Attitude` は、デバイスの軸を東・北・上（East-North-Up）の座標に変換する `Matrix`、`Heading`（磁北からの角度。磁気センサーがなければ -1）、`Pitch` と `Roll` を持ちます。
- `WatchPressure` は気圧計（ヘクトパスカルと、高度の変化）、`WatchLight` は周囲の明るさ（ルクス、Android）、`WatchSteps` は監視を始めてからの歩数です。`StepsBetween(from, to)` は iOS の歩数の履歴（1 週間分）を読みます。Android には履歴がありません。
- `Available(sensor)` は、デバイスにそのセンサーがあるかどうかを返します。シミュレーターやエミュレーターには、センサーがほとんどありません。Mac と PC にはセンサーがありません。Linux では、2-in-1 やタブレットで光センサーと、加速度センサーによる向きが使えます（iio-sensor-proxy）。ブラウザーでは Generic Sensor API かデバイスモーションイベントが使えますが、気圧計と歩数計はありません。

値はどちらのプラットフォームでも同じです。デバイスの軸は Android と W3C の定義どおり（x は画面の右方向、y は画面の上方向、z は画面から手前の方向）で、加速度は加速度センサーが測るとおりの値です（画面を上にして置いたデバイスでは z = +1 g になります。iOS の値はこれに合うよう向きを変えています）。姿勢もどちらでも同じ方法で計算します。読み取りは 1 秒に最大 200 回で、プラットフォームが渡すまとまりごとに、メインスレッドに届きます。

## 画像と動画フレームの編集: asuka/sdk/imageedit

`imageedit.Edit(source, actions, options)` は画像（HEIC、JPEG、PNG、WebP など、カメラや写真ピッカーが返す形式）を読み込み、向きの情報に従って正立させ、アクションを順に実行して、JPEG か PNG をアプリのキャッシュに書き出します。メインスレッド以外から呼びます。

```go
go func() {
	r, err := imageedit.Edit(photo, []imageedit.Action{
		imageedit.Resize(1080, 0), // 幅 1080。0 で縦横比を保つ
		imageedit.Crop(0, 0, 1080, 1080),
	}, imageedit.Options{Quality: 0.8})
	if err == nil {
		upload(r.Path, r.Width, r.Height)
	}
}()
```

アクションには `Resize`、`Crop`、`Rotate`（時計回り、任意の角度）、`FlipHorizontal`、`FlipVertical`、`Fill`、`Pixelate` があります。`Fill(x, y, w, h, r, g, b)` は矩形を単色で塗りつぶし、元の内容はファイルから消えます。`Pixelate(x, y, w, h, block)` は矩形をモザイクにします。各マスはその下にあるピクセルの平均色で、マスは画像全体を基準に並ぶので、隣り合う矩形も一つにつながって見えます。マスが小さいと、下にある文字を読み取れることがあります。確実に隠したいものには `Fill` を使ってください。書き出したファイルには、元の画像のメタデータ（位置情報やカメラの情報など）は一切含まれません。最初のアクションが `Resize` なら、画像をそのサイズに近い大きさでデコードするので、大きな写真ではメモリを節約できます。`imageedit.Size` は、画像をデコードせずにサイズを読みます。`imageedit.VideoFrame(source, at, actions, options)` は、動画の指定した時刻のフレームを正立させて取り出し（サムネイルやポスター用）、同じように編集します。リモートの動画は、そのフレームに必要なところまでしか読み込みません。ソースには `ui.Image` と同じもの、つまりパス、`file://` か `http(s)` の URL、アセットを指定できます。

デコードには、各プラットフォームのコーデックを使います。iOS と Mac では ImageIO、Android では Android 標準のコーデック、Windows では WIC、Linux では GdkPixbuf のローダー（HEIC と WebP は、そのコーデックやローダーがインストールされている場合のみ）、ブラウザーでは canvas（JPEG か PNG。リモートの画像には CORS が必要です）です。

## 音声読み上げ: asuka/sdk/speech

`speech.Speak(text, options, done)` は、プラットフォームの音声でテキストを読み上げるよう、キューに追加します。読み終わると `done` が nil を受け取り、止められた場合は `ErrStopped` を受け取ります。

```go
speech.Speak("Turn left in 200 meters", speech.Options{Rate: 1.1}, nil)
```

`Options` では、`Language`（BCP 47 タグ）、`Voice`（`speech.Voices()` が返す ID。各音声には名前、言語、品質があります）、`Pitch`、`Rate`、`Volume` を指定します。`Stop` はキューを破棄し、`Pause` と `Resume` は単語の区切りで一時停止・再開します（Android には一時停止の機能がありませんが、Asuka がその単語から読み上げを再開するので、Android でも使えます）。iOS では、読み上げ中はほかのアプリの音量が下がり、消音スイッチがオンでも読み上げます。Android でも同じように、ほかのアプリの音量を下げます。音声は、iOS と Mac では AVSpeechSynthesizer、Windows では SAPI、Linux では Speech Dispatcher、ブラウザーでは `speechSynthesis` のものを使います。

## 連絡先: asuka/sdk/contacts

- `contacts.Pick(done)` は、システムの連絡先ピッカーを表示します。ユーザーが一件を選び、アプリにはその連絡先だけが見えます。権限は求めません（Android では、権限がなければ名前だけを取得できます）。
- `contacts.Add(contact, done)` は、内容を入力済みにしたシステムの新規連絡先画面を表示し、ユーザーがそこで保存します。権限は求めません。
- `contacts.Request()` はアクセスの許可を求め、`contacts.List(query)` はアドレス帳を読みます（名前で検索し、上限の件数まで）。名前、組織、ラベル付きの電話番号とメールアドレス、誕生日を読めます。iOS 18 の限定アクセスでは、ユーザーが選んだ連絡先だけが返ります。

Mac では Contacts フレームワークを使います。`Pick` はシステムのピッカーで、ウィンドウの上端からポップオーバーで表示されます。`Add` はウィンドウの上のアラートで確認してから保存します（macOS には新規連絡先画面がないためです）。Linux では、[Linux の場合](#linux-の場合)で説明しているとおり、Evolution Data Server のアドレス帳を読み書きします。Windows はパッケージ化されたアプリにしか連絡先を渡さないので、`Request` は拒否を返し、`Pick` は失敗し、`Add` は vCard をユーザーの連絡先アプリで開きます。ブラウザーでは、Contact Picker がある環境（Android の Chrome）なら一件を選べ、`Add` は vCard をダウンロードします。

## カレンダー: asuka/sdk/calendar

- `calendar.Add(event, done)` は、内容を入力済みにしたシステムのイベントエディターを表示し、ユーザーがそこでイベントを保存します。権限は求めません（iOS 17 以降）。iOS は保存されたかどうかを返しますが、Android のカレンダーアプリは返しません。
- `calendar.Request()` はフルアクセスの許可を求めます。許可されると、`calendar.Calendars()` でカレンダーの一覧を、`calendar.Events(from, to, ids...)` で二つの時刻のあいだのイベントを取得できます。繰り返しイベントは、発生ごとに一件ずつ返ります。
- 終日イベントの `Start` と `End` は日付を表し、それぞれ初日の UTC の午前 0 時と、最終日の翌日の UTC の午前 0 時です。

Mac では EventKit を使います。`Add` はウィンドウの上のアラートで確認してから保存します（macOS にはアプリ用のイベントエディターがないためです）。Linux では Evolution Data Server のカレンダーを読み書きします。Windows はパッケージ化されたアプリにしかカレンダーを渡さないので、`Request` は拒否を返し、`Add` は iCalendar ファイルをユーザーのカレンダーアプリで開きます（保存されたかどうかはわからないので、`saved` は false です）。ブラウザーはカレンダーを読めず、`Add` は `.ics` ファイルをダウンロードします。

## ホーム画面ウィジェット: asuka/sdk/widget

アプリのウィジェットは `asuka.json` の `widgets` で宣言し、アプリの Go のコードから `widget.Set` で内容を設定します。

```json
{ "widgets": [{ "kind": "today", "name": "Today", "description": "What's next", "sizes": ["small", "medium", "rectangular"] }] }
```

```go
widget.Set("today", widget.Entry{
	Title: "Next", Symbol: "calendar", Value: "3 tasks", Body: "Call Ana at 4",
	Progress: 0.4, ShowProgress: true, Until: meeting, URL: "myapp://today",
})
```

ウィジェットは、プラットフォームのテンプレートを使い、サイズに合わせたレイアウトで `Entry` を表示します。表示されるのは、`Title`（`Symbol` 付きで、`Tint` の色）、大きな `Value`、`Body`、`Progress` バー、`Until` までリアルタイムで進むカウントダウン、`Caption` です。アプリが別のエントリーを設定するまで、アプリが動いているかどうかにかかわらず表示され続けます。タップすると、その `URL` でアプリが開きます（`ui.OnOpenURL`）。それぞれ `At` を持つ複数のエントリーはタイムラインになり、各エントリーはその時刻から表示されます。`Clear` でウィジェットを空にします。

サイズは `small`、`medium`、`large`、そして iOS の `extraLarge`（iPad）と、ロック画面の `circular`、`rectangular`、`inline` です。iOS では、CLI がアプリに組み込む WidgetKit 拡張機能になり、アプリと App Group を共有します。リリースビルドには App Groups ケイパビリティと、拡張機能の ID（アプリの ID に `.widgets` を付けたもの）のプロビジョニングプロファイルが必要です。Android では、種類ごとに一つの App Widget になり、Material You の色を使い、ランチャーのセルに合わせたサイズになります。デスクトップと Web にはウィジェットがないので、`widget.Set` は何もしません。

## ライブアクティビティ: asuka/sdk/liveactivity

ライブアクティビティは、いま起きていることを、変化に合わせてアプリの外に表示します。配達中の荷物、配車、試合のスコア、タイマーなどです。iOS ではライブアクティビティ（iOS 16.2）として、ロック画面と Dynamic Island に表示され、Dynamic Island のない iPhone ではバナーで表示されます。Android では常駐する通知になり、Android 16 ではこれが Live Update に昇格して、通知シェードとロック画面の最上部に表示され、ステータスバーにチップが出ます。

```go
a, err := liveactivity.Start(liveactivity.Content{
	Title: "Order #1042", Subtitle: "Ana is picking it up", Value: "12 min",
	Symbol: "bag.fill", Until: eta, URL: "myapp://orders/1042",
}, liveactivity.Options{})

// あとで、メインスレッドで:
a.Update(liveactivity.Content{Title: "Order #1042", Subtitle: "2 stops away", Value: "4 min", Until: eta},
	liveactivity.Alert{Title: "Almost there", Body: "Ana is 2 stops away"})
a.End(liveactivity.Content{Title: "Order #1042", Subtitle: "Delivered"}, liveactivity.Dismiss{})
```

アクティビティは、プラットフォームのテンプレートで `Content` を表示します。空の項目は表示されません。表示されるのは、`Title` と `Subtitle`、`Value`（横に大きく表示され、Dynamic Island のコンパクト表示や Android 16 のステータスバーのチップにも使われます）、`Tint` の色の `Symbol`、`Progress` バー、そしてアプリが動いていなくても進む `Until` までのカウントダウンです。`Since`、`Until`、`ShowProgress` を指定すると、バーが自動で伸びていきます。タップすると、その `URL` でアプリが開きます（`ui.OnOpenURL`）。

`Start` はアプリが画面に出ているあいだに、メインスレッド以外から呼びます（どちらのプラットフォームでも、バックグラウンドのアプリはアクティビティを開始できません）。`Update` と `End` はメインスレッドで呼びます。`Alert` を付けると、更新とともに画面が点灯します。iOS では Dynamic Island が展開され、Apple Watch にも表示されます。Android では通知をもう一度鳴らします。`End` は、最後の内容を、システムが取り除くまで（最大 4 時間）か `Dismiss.At` まで表示したままにします。`Dismiss.Now` を指定すると、すぐに取り除きます。`Running` は、まだ実行中のアクティビティ（再起動したあとなど）を、それぞれの表示内容とともに列挙します。`Enabled` は、ユーザーがアクティビティを許可しているかどうかを返します（Android では最初の `Start` で通知の許可を求めます）。

`Observe` は、アクティビティに起きたことを伝えます。（ユーザー、システム、サーバー、`End` によって）`Ended` になったもの、サーバーによって `Started` になったもの、そしてプッシュトークンです。`Options.Push` を指定すると、アプリが動いていないあいだも、サーバーからアクティビティを更新・終了できます。その `PushToken` と `ID` をサーバーに送ってください。iOS では、サーバーは `liveactivity` タイプの APNs プッシュを送り、その `content-state` は `Content` を JSON にしたものです（Go からは `MarshalJSON` で作れます）。Android では、`asuka.liveActivity`（`update`、`end`）、`asuka.id`、`asuka.content` を持つ Firebase のデータメッセージを送ります。サーバーからアクティビティを開始するには、iOS では iOS 17.2 の `PushToStartToken` を使い（attributes の型は `AsukaActivityAttributes` で、新しいアクティビティの `id` を含めます）、Android では `asuka.liveActivity` を `start` にします。ペイロードの詳細はパッケージのドキュメントにあります。プッシュを使うには、アプリに `asuka/sdk/push` が必要です。iOS では Push Notifications ケイパビリティが、Android では Firebase が要ります。

iOS のビルドは、このパッケージを呼ぶアプリにウィジェット拡張機能を追加し、アクティビティのテンプレート（ウィジェットと同じく SwiftUI 製）と `NSSupportsLiveActivities` を加えます。リリースには、ウィジェットと同じく、拡張機能の ID のプロビジョニングプロファイルが必要です。ユーザーは「設定」でアプリのライブアクティビティをオフにでき、Android 16 では通知の設定で Live Update への昇格をオフにできます。デスクトップと Web にはこの機能がないので、`Enabled` は false で、`Start` は `ErrNotAvailable` で失敗します。

## ショートカット: Siri、Spotlight、ランチャー

`asuka.json` の `shortcuts` は、システムがアプリのために表示するアクションです。それぞれ、その URL でアプリを開き、`ui.OnOpenURL` に渡します。

```json
{ "shortcuts": [{ "id": "newNote", "title": "New Note", "url": "notes://new", "symbol": "square.and.pencil", "phrases": ["New note in ${app}"] }] }
```

iOS では App Shortcuts になります。Siri がそのフレーズ（それぞれアプリ名を表す `${app}` を含みます）を聞き取り、ユーザーが何も設定しなくても、Spotlight、「ショートカット」App、アクションボタンで使えます（最大 10 個）。CLI はこれらを App Intents としてコンパイルし、Xcode と同じようにメタデータを抽出します。Android ではランチャーのショートカットになり、アプリのアイコンを長押しすると、アクセントカラーのシンボル付きで表示されます。デスクトップと Web には追加されません。

シミュレーター向けのビルドは、Xcode と同じように、キーチェーンにある（`asuka.json` で指定したチームの）Apple Development の証明書で署名されます。App Intents は、チームのあるアプリでしか動かないためです。証明書がなければビルドはアドホック署名になり、CLI はショートカットが動かないことを知らせます。

## 触覚フィードバック: ui.Haptic

`ui.Haptic(kind)` は触覚フィードバックを再生します（iOS では UIFeedbackGenerator、Android では `performHapticFeedback`）。種類は `ui.HapticSelection`、`HapticLight`、`HapticMedium`、`HapticHeavy`、`HapticSuccess`、`HapticWarning`、`HapticError` のいずれかです。Mac ではトラックパッドのフィードバックを再生します（感圧タッチトラックパッドで、`HapticSelection` では整列、それ以外ではレベル変更のフィードバック）。ブラウザーでは、可能な環境（Android の Chrome）で振動します。Windows と Linux には触覚フィードバックがないので、何もしません。

```go
ui.Haptic(ui.HapticSuccess)
```

アニメーションに合わせたフィードバックについては、[モーションとジェスチャー](motion.md)を参照してください。

## クラッシュレポート: asuka/sdk/crash

`asuka/sdk/crash` をインポートしたアプリは、クラッシュのたびにレポートを保存し、次の起動時にそれを読んで、自分のサーバー、Sentry、Crashlytics など好きな送り先に送れます。Asuka 自身は何もアップロードしません。

```go
func App() ui.Node {
	crash.OnReport(func(r crash.Report) {
		go func() {
			if err := send(r); err == nil { // 自前のアップロード処理（r は JSON）
				crash.Delete(r.ID)
			}
		}()
	})
	crash.Set("user", userID)
	// ...
}

// ほかの場所で、アプリの動作に合わせて:
crash.Breadcrumb("opened the cart")
crash.Record(err) // 処理済みのエラーもレポートする
```

`OnReport(f)` は、保存されている各レポートを渡してメインスレッドで `f` を呼び、その後も新しいレポート（起動の少しあとに iOS が届ける診断情報や、記録されたエラー）が届くたびに呼びます。各レポートにつき一度だけで、返される `stop` を呼ぶまで続きます。`Reports()` は、すべてのレポートを古い順に読みます（メインスレッド以外から呼びます）。レポートは `Delete(id)` を呼ぶまで残り、`MaxReports`（50）を超えると古いものから消えます。`ID` は何度読んでも同じなので、サーバーは二重に送られたレポートを見分けられます。

プラットフォームごとに報告される内容（`Kind`）は次のとおりです。

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Go`：回復されなかったパニック（どのゴルーチンでも）、致命的なエラー（map への並行書き込み、デッドロック、メモリ不足） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓（ホストがランタイムの出力を保存） |
| `Exception`：捕捉されなかった例外 | NSException（Objective-C、Swift の `NSException`） | JVM の `Throwable` | — | — | — | 捕捉されなかった JavaScript のエラー（致命的ではない） |
| `Native`：シグナル、Mach や Windows の例外 | MetricKit | システムの tombstone | MetricKit | ランタイムのレポート | ランタイムのレポート（ネイティブコードでのシグナル） | — |
| `Hang` | MetricKit のハング（致命的ではない）、ウォッチドッグ | ANR とそのトレース | MetricKit | — | — | — |
| `OutOfMemory` | メモリ上限（MetricKit が報告した場合） | メモリ不足による強制終了 | — | — | — | — |
| `Resource` | CPU とディスク書き込みの例外（致命的ではない） | リソースの過剰使用 | MetricKit | — | — | — |
| `NonFatal`：`Record(err)` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

Go のクラッシュは、すべてのゴルーチンのスタックとともに、Go のランタイム自身が保存します（`runtime/debug.SetCrashOutput`。アプリの起動時、`main` より前に設定されます）。捕捉されなかった例外は、アプリが終了するときにホストのハンドラーが書き込みます（それより前に設定されたハンドラー、たとえばクラッシュ SDK のハンドラーも、そのあとで実行されます）。それ以外は、次回以降の起動時にシステムが渡してくれます。iOS と macOS では MetricKit の診断情報、Android ではアプリのプロセスがどのように終了したかの記録です（`getHistoricalProcessExitReasons`、Android 11 以降。ネイティブクラッシュの tombstone は Android 12 から）。ホストのハンドラーが報告したクラッシュをシステムが改めて報告したもの（捕捉されなかった例外による iOS の SIGABRT、Android の `REASON_CRASH`）は除外されます。

各レポートには `Context` があります。アプリのバージョンとビルド番号、デバイスの機種と OS、プロセスとその実行時間、フォアグラウンドにいたかどうか、最近の `Breadcrumb`（`MaxBreadcrumbs`、100 件）、`Set` で設定した値です。これらは変わるたびにアプリのデータディレクトリに書き込まれます。一件ごとに一行を追記するだけで（sync はしないので、書き込み一回分のコストで済みます）、ファイルが必要な大きさの 2 倍になったら書き直します。そのため、アプリが何によって終了しても、クラッシュの前にはディスクに書かれています。ライフサイクルの変化もブレッドクラムになります（「app: background」）。

レポートの `Text` は、プラットフォームが渡す内容そのもの（Go のクラッシュ出力、スタックトレース、tombstone、ANR のトレース）です。`Threads` は、それらのスタックをフレームに分解したもので、クラッシュしたスレッドが先頭に来ます。Go のフレームは関数、ファイル、行を持ちます。ネイティブのフレームは、バイナリ、バイナリの UUID（Mach-O）またはビルド ID（ELF）、アドレス、バイナリ内のオフセットを持ちます。サービスがビルドのシンボルを使ってシンボル化するには、これらの情報が必要です（[配布](shipping.md#クラッシュレポート用のシンボル)を参照）。

**Sentry や Crashlytics への送信。** Sentry には、各レポートからイベントを作って送ります（送り先はエンベロープのエンドポイント `https://<host>/api/<project>/envelope/` で、DSN のキーを付けます）。`exception.values[0]` の `type` に `Kind`、`value` に `Message`、`stacktrace.frames` にクラッシュしたスレッドのフレームを入れます。フレームは外側から順に並べ（Sentry の順序で、`Thread` とは逆です）、それぞれ `function`、`filename`、`lineno` を、ネイティブのフレームなら `instruction_addr` と `package` を持たせます。また `debug_meta.images` には、フレームに出てくる各バイナリを `{"type": "macho", "debug_id": BinaryID, "image_addr": Address-Offset}`（Android では `"elf"`）として入れます。こうすると、`sentry-cli` でアップロードした dSYM やシンボルが適用されます。`Context` は `contexts`、`tags`、`breadcrumbs` に入れます。Crashlytics には、送信先となる公開のエンドポイントがありません。レポートを自分のサーバーに送るか、独自のネイティブモジュール（[ネイティブコード](native.md)）から Crashlytics の SDK に致命的でないエラーとして記録してください。どちらの場合も、App Store の審査ではクラッシュデータについて尋ねられます。送信するなら、そのことを申告してください（`asuka.json` の `ios.collected` に `{"type": "CrashData", "purposes": ["AppFunctionality"]}`）。

**プラットフォームごとの違い。** iOS では、レポートを読むアプリ（`crash.Reports` か `crash.OnReport` を使うアプリ）にだけ MetricKit をリンクし、MetricKit は診断情報を共有拡張機能ではなくアプリに届けます。独自の共有シート（`app.ShareSheet`）を持つアプリは、シートのクラッシュもアプリのクラッシュと一緒に両者の App Group に保存し、どちらからも読めます。MetricKit は iOS 17 以降ではクラッシュのプロセス ID を持っているので、その実行のコンテキストと結び付けられます。それより前のバージョンでは、クラッシュの直前の実行と結び付けます。シミュレーターでは MetricKit は診断情報を届けません。Android では追加の設定は要りませんが、Android 11 より前にはネイティブクラッシュと ANR の記録がありません。macOS では、MetricKit は macOS 12 から使えます。アプリを終了させた、捕捉されない Objective-C の例外は、MetricKit のクラッシュとして報告されます（メインスレッドの例外は AppKit が捕捉してログに出力します）。Windows では、プロセスが処理しなかった例外はすべて Go のランタイムが（最後のハンドラーとして）処理します。ランタイムは例外とゴルーチンを出力して終了します。コンソールがない場合（GUI 向けにビルドしたリリース）、Asuka はランタイムの出力をその実行用のファイルに送り、次の起動時にそれを読んで `Native` レポートにします。Asuka は独自の未処理例外フィルターを設定しません。Go のハンドラーが先に実行されるので、これを置き換えると Go の処理が壊れるためです。Windows エラー報告は、これまでどおり記録を残します。ブラウザーでは、Go のランタイムはクラッシュ出力をファイルに書けない（`SetCrashOutput` には `dup` が必要です）ので、Go の実行中はホストがランタイムの出力を保持します。そしてプログラムがパニックや致命的なエラーで終了するときに、それをアプリのファイル（IndexedDB）の中にある、その実行の記録に書き込み、次の起動時にそれが読まれます。捕捉されなかった JavaScript のエラー（ホストのものでも、アプリの `web/*.js` のものでも）は、その場で `Exception` レポートになります。致命的なエラーとしては扱われず（ページは動き続けます）、その実行のコンテキストが付きます。1 回の実行につき最大 20 件で、同じエラーは一度だけです。ページはオリジンの `localStorage` に自分の実行が生きていることを記録するので、同じアプリのほかの開いているページがその記録に手を出すことはありません。

**`asuka dev` では**、Go のコードは Mac（または PC）上で動きます。そのクラッシュはこれまでどおりターミナルに表示され、そこに保存されるので、次の実行の `crash.Reports` で読めます。プレイヤーアプリ自身のネイティブクラッシュはアプリのものではないので、アプリには報告されません。アプリのビルド（`asuka dev --once`、`asuka build`）では、すべてが報告されます。

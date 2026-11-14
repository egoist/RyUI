# SwiftUI・Expo との比較

各行は、アプリでよく必要になる機能です。SwiftUI と Expo がそれをどう実現しているか、Asuka には何があるかを並べています。行に特に記載がない限り、✓ は Asuka が動くすべてのプラットフォーム（iOS、Android、Mac、Windows、Linux、Web）で使えることを表します。あるプラットフォームに機能がない場合（Mac にはセンサーがなく、ブラウザーにはホーム画面ウィジェットがありません）、そのパッケージは「対応していない」と答えます。スマートフォンにできてブラウザーにはできないことについて、ブラウザーで各パッケージが代わりに何をするかは [Web](web.md#ネイティブモジュール) を参照してください。

SwiftUI は Mac でも動きます。Expo は iOS、Android、Web で動きます。React Native は、Expo の外にある Microsoft の React Native for Windows and macOS によって Windows と Mac にも対応しています。デスクトップのフレームワーク（Electron、Tauri、Wails）と Asuka の比較は[デスクトップのフレームワーク](#デスクトップのフレームワーク)を、デスクトップアプリがウィンドウ以外に必要とするものについては[デスクトップアプリの機能](#デスクトップアプリの機能)を参照してください。UI を自前で描画する MyGo との比較は [Asuka と MyGo](mygo.md) を参照してください。

✓ 対応 · ◐ 一部対応（足りないものを記載） · ✗ 未対応

## レイアウト

| | SwiftUI | Expo (React Native) | Asuka |
|---|---|---|---|
| スタック、間隔、パディング、配置 | `HStack`、`VStack`、`Spacer` | flexbox | ✓ `Row`、`Column`、`Style`（flexbox：`Grow`、`Gap`、`Align`、`Justify`、`Wrap`） |
| オーバーレイ、絶対配置 | `ZStack`、`.overlay` | `position: absolute` | ✓ `Style.Absolute`、`Top`/`Left`/... |
| スクロール | `ScrollView` | `ScrollView` | ✓ `Scroll`（縦、横。画面外の行はツリーから外れる） |
| 遅延リスト、セルの再利用 | `List`、`LazyVStack` | `FlatList` | ✓ `List`（プラットフォームのもの：UICollectionView、RecyclerView。Mac、Windows、Linux、Web では、表示中の行をホストがバインドして再利用） |
| グリッド | `LazyVGrid` | `FlatList numColumns` | ✓ `ListProps.Columns`、`Gap` |
| チャットのリスト：末尾から始め、古いものを上に読み込む | `defaultScrollAnchor(.bottom)`、`scrollPosition` | `FlatList inverted`、`maintainVisibleContentPosition` | ✓ `ListProps.Anchor`、`OnStartReached`。どのリストでも、表示中の部分より上に行を追加しても表示位置は動かない |
| セクション、固定ヘッダー | `List` の中の `Section` | `SectionList`、`stickySectionHeadersEnabled` | ✓ `ListProps.Section`、`Header`（plain と bare のリストでは固定） |
| 行の選択 | `List(selection:)`、`EditButton` | コミュニティ製 | ✓ `ListProps.Selecting`、`Selected`、`OnSelect`、`OnSelectBegin`（iOS の編集モードと 2 本指のパン、Android のチェックボックスと長押し） |
| セーフエリア、キーボード | 自動 | `SafeAreaView`、`KeyboardAvoidingView` | ✓ `SafeArea`、`UseEnvironment().Keyboard` |
| ビューのサイズの取得 | `GeometryReader` | `onLayout` | ✓ `ViewProps.OnLayout` |
| アスペクト比、最小・最大サイズ | `.aspectRatio`、`.frame(min:max:)` | スタイル | ✓ `Style` |

## コントロール

| | SwiftUI | Expo | Asuka |
|---|---|---|---|
| テキスト、装飾付きテキスト、リンク | `Text`、`AttributedString` | `Text` | ✓ `Text`、`RichText`（スパン、リンク、インラインのビュー、選択）、`asuka/sdk/markdown` |
| ボタン（バリエーション、アイコン、メニュー） | `Button`、`Menu` | `Pressable` | ✓ `Button`（filled、tinted、gray、bordered。アイコン。メニュー）、`Menu`、コンテキストメニュー。メニュー項目の無効化、チェック、サブメニュー、タイトル付きのセクション（`Action.Disabled`、`Checked`、`Children`、`Inline`。`ui.Submenu`、`ui.MenuSection`）、メニューを開くバーボタン |
| テキストフィールド | `TextField`、`SecureField`、`TextEditor` | `TextInput` | ✓ `TextField`（パスワード入力、キーボードの種類、リターンキー、複数行） |
| カーソルとその位置への挿入 | `TextSelection` | `selection`、`onSelectionChange` | ✓ `TextFieldProps.Selection`、`ui.InsertText` |
| リッチテキストの編集（メールの本文） | `AttributedString` を使う `TextEditor`（iOS 26） | コミュニティ製（10tap、pell） | ✓ `TextFieldProps.Rich`、`ToggleFormat`、`SetLink`、`OnFormats` |
| キーボードの上のバー | `.toolbar { ToolbarItemGroup(placement: .keyboard) }` | `InputAccessoryView`（iOS） | ✓ `TextFieldProps.Accessory`（両方） |
| 貼り付けた画像、キーボードの GIF とステッカー | `onPasteCommand`、`PasteButton` | コミュニティ製（`onPaste`） | ✓ `TextFieldProps.PasteTypes`、`OnPaste`（Android の `commitContent` にも対応） |
| トグル、スライダー | `Toggle`、`Slider` | コミュニティ製 | ✓ `Toggle`、`Slider` |
| セグメンテッドコントロール | `Picker(.segmented)` | コミュニティ製 | ✓ `Segmented` |
| 日付と時刻 | `DatePicker` | コミュニティ製 | ✓ `DatePicker`（日付、時刻、両方） |
| 進捗、スピナー | `ProgressView` | `ActivityIndicator` | ✓ `Progress`、`ProgressRing`（円形。値あり・なし）、`Activity` |
| ホイールとメニューのピッカー、ステッパー | `Picker`、`Stepper` | コミュニティ製 | ✓ `Wheel`、`Picker`（iOS と Mac ではポップアップボタン、Android では Material の exposed dropdown menu）、`Stepper` |
| カラーピッカー | `ColorPicker` | コミュニティ製 | ✓ `ColorPicker`（iOS ではシステムのピッカー。システムのピッカーがない Android では、Google のパレットを並べた Material のダイアログで、不透明度も指定可能） |
| アイコン | SF Symbols | `@expo/vector-icons` | ✓ `Symbol`（iOS と Mac では SF Symbols。同じ名前で、Android と Web では Material Symbols、Windows では Segoe Fluent Icons、Linux ではアイコンテーマのものか Material Symbols。厳選したセット） |
| 画像（リモート、キャッシュ、プレースホルダー） | `AsyncImage` | `expo-image` | ✓ `Image`（メモリとディスクのキャッシュ、`Placeholder`） |
| 動画 | `VideoPlayer` | `expo-video` | ✓ `Video`（iOS と Mac では AVKit、Android では Media3、Windows では WebView2、Linux では GStreamer、Web では `<video>`） |
| Web コンテンツ | `WKWebView` | `react-native-webview` | ✓ `WebView`（iOS と Mac では WKWebView、Android の WebView、Windows では WebView2、Linux では WebKitGTK。URL、HTML、双方向のメッセージ、ページより先に実行するスクリプト、リクエストのブロック、状態と履歴、ダウンロード、新しいウィンドウ、Cookie、スクリプトの値。Android では個々のリクエストも。Web では、アプリ自身のサイトと `postMessage` で協調するページならこれらすべてに対応し、ほかのサイトのページは読み込むだけ）。`asuka/sdk/js` は画面外で JavaScript を実行 |
| 地図 | `Map` | `expo-maps` | ✓ `asuka/sdk/maps`（Apple Maps。API キーがあれば Google Maps：表示範囲、マーカー、ユーザーの現在地、タップ。Linux では libshumate の OpenStreetMap の地図。Windows ではホストが OpenStreetMap のタイルを自分で描画） |
| 独自の描画 | `Shape`、`Path`、`Canvas` | `react-native-svg`、Skia | ✓ `Shape` と `Path`（塗り、線、線端、角の形、破線、アニメーション可能なトリム）。`Canvas`（パス、グラデーション、テキスト、変形、クリップ、不透明度）。SVG アイコンのネイティブ描画（`asuka/sdk/svg`） |
| グラフ | Swift Charts | `victory-native`、コミュニティ製 | ✓ `asuka/sdk/chart`（棒、線、面、点） |
| シェーダー | `.colorEffect(ShaderLibrary...)` | Skia の runtime effects | ✓ `Shader`（Kage。Go の構文で、iOS と Mac では Metal、Android では AGSL、Web では WebGL 2、Windows では Direct3D 11、Linux では OpenGL） |
| ネイティブへの抜け道 | `UIViewRepresentable` | ネイティブモジュール | ✓ ネイティブパッケージ：Go で宣言した型付きのモジュールとビューを、iOS と macOS では Swift、Android では Kotlin、Web では JavaScript、Windows と Linux では Go で実装（[ネイティブコード](native.md)） |
| ページング、ページのドット | `.scrollTargetBehavior(.paging)`、`TabView(.page)` | `react-native-pager-view` | ✓ `ScrollProps.Paging`、`PageControl` |
| チェックボックス | `Toggle`（`.checkbox`、macOS） | コミュニティ製 | ✓ `ToggleProps.Checkbox` |

## ナビゲーションと表示

| | SwiftUI | Expo (expo-router) | Asuka |
|---|---|---|---|
| ネイティブのプッシュ、ポップ、戻るジェスチャーを備えたスタック | `NavigationStack` | Stack | ✓ `Stack`、`UseNavigator` |
| タブ | `TabView` | Tabs | ✓ `Tabs` |
| サイドバー、ドロワー | `NavigationSplitView` | Drawer | ✓ `SplitView`（iPhone ではスタック、Android ではドロワー。Mac では `NSSplitViewController`、Windows ではナビゲーションペイン、Linux では libadwaita のスプリットビュー） |
| タブとサイドバーの行のバッジ | `badge` | `tabBarBadge`、`drawerItem` のカウント | ✓ `Tab.Badge`、`Destination.Badge`（`badgeValue`、`BadgeDrawable`、ドロワーのカウンター。Mac では `NSTabView` のラベルとソースリストのカウント。Fluent の `InfoBadge`。libadwaita の `badge-number`） |
| バーボタン、タイトル、検索 | `.toolbar`、`.searchable` | ヘッダーのオプション | ✓ `ScreenOptions`（`Actions`、`Search`） |
| 下部のツールバー、主要なアクション（FAB） | `ToolbarItem(placement: .bottomBar)` | — | ✓ `ScreenOptions`（`Toolbar`、`Primary`、`PrimaryExtended`）：UIKit では末尾に目立つ項目を置いたツールバー、Material では `BottomAppBar` と（extended）FAB、Mac ではウィンドウのツールバー、Windows ではコマンドバー、Linux では `GtkActionBar`、Web ではバーとフローティングボタン |
| デテントのあるシート | `.sheet`、`.presentationDetents` | モーダル | ✓ `Sheet`（`Detents`、`Grabber`） |
| 全画面のモーダル | `.fullScreenCover` | `presentation: "fullScreenModal"` | ✓ `FullScreen` を指定した `Sheet`（iOS の `.fullScreen`。Material の全画面ダイアログで、エッジツーエッジ、予測型「戻る」に対応。デスクトップではウィンドウのコンテンツ全体を覆うシートまたはウィンドウ。Web ではページを覆う `<dialog>`） |
| 戻る操作の横取り、閉じるのを防ぐ | `.navigationBarBackButtonHidden`、`.interactiveDismissDisabled` | `BackHandler`、`usePreventRemove` | ✓ `OnBack`（画面、シート、アプリ） |
| ポップオーバー | `.popover` | — | ✓ `Popover`（iPhone でもポップオーバー） |
| アラート、アクションシート | `.alert`、`.confirmationDialog` | `Alert` | ✓ `Alert`（両方のスタイル。アラートのテキストフィールド：`Fields`、パスワード用の `Secure`、`Disabled` で入力に応じて有効になるボタン） |
| スナックバー、トースト（「元に戻す」付きの短いメッセージ） | — | コミュニティ製（react-native-paper の `Snackbar`） | ✓ `Snackbar`（Android では Material のもの、Linux では `AdwToast`。iOS、macOS、Windows、Web ではプラットフォームらしい見た目で描画） |
| ディープリンク、ユニバーサルリンクと App Links | `.onOpenURL` | Linking | ✓ `OnOpenURL`、`asuka.json` の scheme と links |
| 引っ張って更新、スワイプアクション | `.refreshable`、`.swipeActions` | `RefreshControl` | ✓（両端：`SwipeActions`、`LeadingSwipeActions`） |

## モーション

| | SwiftUI | Expo (Reanimated) | Asuka |
|---|---|---|---|
| 変化のアニメーション | `withAnimation` | `withTiming`、`withSpring` | ✓ `Animate`、`AnimateWith`（イージング、リニア、スプリング。繰り返し、自動反転） |
| 変形 | `.rotationEffect`、`.scaleEffect`、`.offset` | `transform` | ✓ `Style.Transform` |
| 出現と退場のトランジション | `.transition` | entering/exiting | ✓ `Style.Transition`（`Fade`、`Pop`、`SlideUp`、独自のもの） |
| フレーム駆動のアニメーション | `TimelineView(.animation)` | `useFrameCallback` | ✓ `OnFrame` |
| 影、グラデーション、ぼかし | `.shadow`、`LinearGradient`、マテリアル | スタイル、expo-linear-gradient、expo-blur | ✓ `Shadow`、`LinearGradient`、`Material` |
| ジェスチャー | タップ、長押し、ドラッグ、拡大、回転 | Gesture Handler | ✓ タップ、長押し、パン、ピンチ、回転、スワイプ |
| ハードウェアキーボードのキー | `.onKeyPress`、`.keyboardShortcut` | `onKeyPress`（`TextInput` のもの。React Native for Windows and macOS では `onKeyDown`） | ✓ `ViewProps.Keys`、`TextFieldProps.Keys`（フォーカスのあるビューから外側へ、コントロールやメニューより先に処理）、`UseKeys`（ウィンドウ全体） |
| 押下とホバーの状態 | `ButtonStyle` の `isPressed`、`.onHover` | `Pressable` の `pressed`、`onHoverIn`/`onHoverOut` | ✓ `Style.Pressed`、`Hovered` のバリアント。グループやピアの状態には `Interaction`（`Style.When`）。`OnPressedChange`、`OnHoverChange` |
| ドラッグ＆ドロップ | `.draggable`、`.dropDestination` | （組み込みなし） | ✓ `Drag`、`Drop`（アプリ内では Go の値。ほかのアプリとはテキスト、リンク、ファイル。`Dragging` と `DragOver` のバリアント） |
| 共有要素のトランジション | `matchedGeometryEffect`、`.navigationTransition(.zoom)` | shared transitions | ✓ `Style.Match`（ビューが別のビューの位置とサイズから広がる）、`ScreenOptions.ZoomFrom`（プッシュした画面がビューからズームして現れる：iOS 18 のズーム、Material の container transform） |
| 触覚フィードバック | `sensoryFeedback` | `expo-haptics` | ✓ `Haptic` |
| 視差効果を減らす | `accessibilityReduceMotion` | `useReducedMotion` | ✓ `Environment.ReduceMotion`（iOS ではトランジションがディゾルブになる） |

## データとサービス

| | SwiftUI / Apple | Expo | Asuka |
|---|---|---|---|
| 状態 | `@State`、`@Observable` | フック | ✓ シグナル（細粒度：書き込むと、その値を読んでいるところだけが更新される） |
| 永続化する小さな値 | `@AppStorage` | AsyncStorage | ✓ `asuka/sdk/store` |
| SQLite | SwiftData / GRDB | `expo-sqlite` | ✓ `asuka/sdk/sqlite`（マイグレーション、型付きのクエリ、トランザクション、メインスレッド外での書き込み、読んでいるテーブルに書き込みがあると再実行されるライブクエリ。変化する引数にも対応：`WatchWith`。ブラウザーでも、SQLite の WebAssembly ビルドで動作） |
| セキュアストレージ | キーチェーン | `expo-secure-store` | ✓ `asuka/sdk/secure`（Web では、ブラウザーが外に出さない鍵で暗号化して IndexedDB に保存。キーチェーンではない） |
| ファイル、ネットワーク | Foundation | `expo-file-system`、fetch | ✓ Go の `os`、`net/http` |
| 写真 | `PhotosPicker` | `expo-image-picker` | ✓ `device.PickPhotos` |
| 書類 | `UIDocumentPickerViewController` | `expo-document-picker` | ✓ `device.PickFiles`（MIME タイプ、複数選択） |
| カメラ | AVFoundation | `expo-camera` | ✓ `asuka/sdk/camera`：`Take`（システムのカメラ。Mac、Windows、Linux ではライブプレビュー付きのダイアログ。Windows は Media Foundation のキャプチャーエンジン、Linux はカメラポータルか GStreamer 経由の Video4Linux）、`View`（前面か背面のライブプレビュー）、`Capture`。`asuka/sdk/camera/record` で音声付きの動画を録画 |
| バーコード、QR コード | AVFoundation、Vision | `expo-camera` のスキャン | ✓ `asuka/sdk/scan`：`camera.View` でのライブ読み取り（`Scan: scan.Codes(...)`）と画像ファイルからの読み取り（`scan.File`）。Android では ML Kit で、インポートしたアプリにだけ含まれる。Linux では ZBar。Windows では Go で書いた Asuka 独自のリーダー（`internal/barcode`：QR、Data Matrix、EAN、UPC、Code 128、Code 39） |
| 位置情報 | Core Location | `expo-location` | ✓ `asuka/sdk/location`（地名。Windows では OpenStreetMap の Nominatim） |
| 翻訳、複数形、地域のフォーマット | String Catalogs、FormatStyle | `expo-localization`、i18next | ✓ `asuka/i18n`、カタログ（[多言語対応](i18n.md)） |
| 右から左のレイアウト | `layoutDirection` | `I18nManager` | ✓ 言語に従う、`ui.WithDirection` |
| ローカル通知 | UserNotifications | `expo-notifications` | ✓ `asuka/sdk/notify`（即時またはスケジュール、画像。iOS、Android、macOS、Linux。Windows ではトーストで、アプリの COM アクティベーターを通じてボタンと返信に対応。Web ではアプリの Service Worker を通じて表示するので、ページを閉じたあとも残り、操作に応答する。ブラウザーは通知をスケジュールできないため、スケジュールした通知はアプリのページが開いていればその時刻に、開いていなければ次にページを開いたときに表示） |
| プッシュ通知 | APNs | `expo-notifications` | ✓ `asuka/sdk/push`（iOS と macOS では APNs、Firebase Cloud Messaging、Web では VAPID による Web Push：トークン、受信したメッセージとタップされたメッセージ。`asuka/sdk/push/webpush` で Go のサーバーから Web Push を送信） |
| 通知のアクション、インライン返信 | `UNNotificationCategory`、`UNTextInputNotificationAction` | `setNotificationCategoryAsync` | ✓ `notify.SetCategories`、`OnAction`（バックグラウンドのアクションはアプリの Go のコードを実行し、アプリが動いていなければ画面なしで起動する。`Response.Defer`。Android では返信を会話に投稿し直す。Web では Service Worker の通知アクションで、Chrome が対応していればインライン返信も） |
| スレッド、表示中の通知、バッジ | `threadIdentifier`、`getDeliveredNotifications`、`setBadgeCount` | `getPresentedNotificationsAsync`、`setBadgeCountAsync` | ✓ `Thread`（Android ではサマリーを保ったグループ）、`notify.Showing`、`Remove`、`RemoveThread`、`SetBadge`（Android では通知に基づくランチャーのバッジ） |
| フォアグラウンドでの通知 | `willPresent` | `setNotificationHandler` | ✓ `notify.Present`（バナー、バナーと音なしで通知一覧のみ、非表示） |
| Android の通知チャンネル | — | `setNotificationChannelAsync` | ✓ `notify.SetChannels`、`SetChannelGroups`。プッシュ通知の `channel_id`（iOS にはない） |
| 会話のメッセージ | コミュニケーション通知（`INSendMessageIntent`） | コミュニティ製 | ✓ `Notification.From`、`push.Message.From`：iOS のコミュニケーション通知（使うアプリにだけ entitlement を追加）、Android の MessagingStyle と会話のショートカット。macOS では通常の通知。バブルには非対応 |
| 表示前のプッシュ通知の書き換え | Notification Service Extension | コミュニティ製（`expo-notifications` には機能なし） | ✓ `push.Rewrite`：iOS では拡張機能の中で（`asuka build` がビルドと署名を行う）、Android のデータメッセージではアプリのプロセスの中で、アプリの Go のコードを実行 |
| クリップボード、共有シート、ブラウザー | `UIPasteboard`、`ShareLink`、`SFSafariViewController` | `expo-clipboard`、`expo-sharing`、`expo-web-browser` | ✓ `Copy`、`device.Clipboard`（テキスト。画像は `CopyImage`、`ClipboardImage`。`ClipboardHas`、`OnClipboardChange`）、`Share`（テキスト）、`ShareFiles`（ファイル。expo-sharing と同じく MIME タイプ、UTI、タイトル、iPad のアンカーを指定可能。シートが閉じたら完了）、`device.Browse` |
| ファイルのプレビュー | `QLPreviewController`、`.quickLookPreview` | `react-native-file-viewer` | ✓ `asuka/sdk/preview`（iOS と Mac では Quick Look で、複数ファイルとマークアップに対応。Android ではビューアーアプリ。Windows と Linux ではデフォルトのアプリ） |
| 印刷、HTML からの PDF 作成 | `UIPrintInteractionController`、`UIPrintPageRenderer` | `expo-print` | ✓ `asuka/sdk/print`（ファイル、HTML、WebView を、システムのダイアログで印刷し、その結果も取得。`PDF`：ページ分割、地域に合った用紙、余白） |
| メールとメッセージの作成画面 | `MFMailComposeViewController`、`MFMessageComposeViewController` | `expo-mail-composer`、`expo-sms` | ✓ `asuka/sdk/compose`（宛先、件名、HTML の本文、添付ファイル。iOS ではユーザーが何をしたかも取得。Android ではメールアプリのみ。Mac ではメッセージ） |
| スリープの抑止、デバイス情報 | `isIdleTimerDisabled`、`UIDevice` | `expo-keep-awake`、`expo-device` | ✓ `device.KeepAwake`、`device.GetInfo` |
| ネットワークの状態 | `NWPathMonitor` | `expo-network` | ✓ `asuka/sdk/network`（オンラインかどうか、Wi-Fi かモバイル通信か、従量制かどうか、データセーバー／省データモード） |
| ギャラリーや Downloads への保存 | `PHPhotoLibrary` | `expo-media-library` | ✓ `asuka/sdk/library`（MediaStore のコレクション。写真、または「ファイル」の Documents） |
| バックグラウンドでの処理 | `beginBackgroundTask`、`BGTaskScheduler` | `expo-task-manager`、`expo-background-task` | ✓ `asuka/sdk/background`：Android ではアプリを動かし続け、進捗とボタンを通知に表示。iOS では約 30 秒。Mac では動かし続け（App Nap をオフ）、Linux と Windows ではシステムのスリープを防ぐ。`asuka/sdk/periodic`：システムがときどき実行するタスクで、アプリの Go のコードを画面なしで起動（BGTaskScheduler、WorkManager。Mac ではアプリの実行中に `NSBackgroundActivityScheduler`。Linux と Windows では実行中のタイマー） |
| ほかのアプリからの共有の受け取り | 共有拡張機能 | `expo-sharing`（`useIncomingShare`） | ✓ `ui.OnShare`：テキスト、リンク、画像、動画、音声、ファイルを、一つでも複数でも、名前、MIME タイプ、サイズ付きで受け取る（`asuka.json` の `shareTarget`：types と max から、Android のインテントフィルターと iOS のアクティベーションルールを生成。iOS ではアプリを開く共有拡張機能で、ファイルは App Group 経由）。`app.ShareSheet` は、共有元のアプリの上に出すアプリ独自の画面（iOS では共有拡張機能、Android では独立したプロセスのボトムシート）で、データをアプリと共有する。リンクの解決は行わない（`net/http` を使う） |
| 音声 | AVAudioPlayer、AVAudioRecorder | `expo-audio` | ✓ `asuka/sdk/audio`（読み込み、再生、一時停止、シーク、音量、ループ）。`asuka/sdk/audio/record`（音声向けまたは音楽向けの音質、一時停止、レベルメーター） |
| 再生中の情報、ロック画面のコントロール、メディアキー | `MPNowPlayingInfoCenter`、`MPRemoteCommandCenter` | `expo-audio` のロック画面のコントロール、コミュニティ製（`react-native-track-player`） | ✓ `asuka/sdk/nowplaying`（タイトル、アーティスト、アルバム、アートワーク、長さ、システムが進める再生位置、再生、一時停止、次へ、前へ、シーク、スキップ、停止。設定したハンドラーに応じて有効化。`audio.Player` には自動で追従。Android では MediaSession とメディア再生サービス、Mac ではコントロールセンター、Windows ではメディアのフライアウト、Linux では MPRIS、Web では Media Session API） |
| Sign in with Apple | `SignInWithAppleButton` | `expo-apple-authentication` | ✓ `asuka/sdk/appleauth`（Apple のボタン、認証情報、その状態。iOS と macOS） |
| Face ID、指紋認証 | LocalAuthentication | `expo-local-authentication` | ✓ `asuka/sdk/biometric`（Windows では Windows Hello。Web では WebAuthn によるプラットフォームの認証器） |
| ストアのレビュー | `requestReview` | `expo-store-review` | ✓ `asuka/sdk/review`（App Store のプロンプト、Google Play のアプリ内レビュー） |
| トラッキングの許可 | App Tracking Transparency | `expo-tracking-transparency` | ✓ `asuka/sdk/tracking` |
| アプリの完全性 | App Attest | `expo-app-integrity` | ✓ `asuka/sdk/integrity`（App Attest、Play Integrity） |
| 年齢層 | Declared Age Range | `expo-age-range` | ✓ `asuka/sdk/agerange`（Declared Age Range、Play Age Signals） |
| センサー | Core Motion | `expo-sensors` | ✓ `asuka/sdk/sensors`（加速度計、ジャイロスコープ、磁力計、姿勢と方位を含むデバイスのモーション、気圧計、照度、歩数計。両 OS で同じ軸と単位。Mac と Windows にはセンサーなし。Linux では iio-sensor-proxy の照度と向き） |
| 画像の編集 | Core Graphics、ImageIO | `expo-image-manipulator` | ✓ `asuka/sdk/imageedit`（リサイズ、切り抜き、回転、反転、塗りつぶし、モザイク。HEIC を読み込んで JPEG か PNG で出力。向きを正し、元画像のメタデータは含めない。Linux では GdkPixbuf のデコーダー、Windows では WIC のデコーダー） |
| 画像の中のテキスト、顔、コード | Vision | なし | ✓ `asuka/sdk/vision`（それぞれの位置付き）。Android では ML Kit で、インポートしたアプリにだけ含まれる。Linux では ZBar のコードと Tesseract のテキストで、顔は非対応。Windows では OCR のテキストと顔、Asuka のリーダーのコード |
| 写真ライブラリ | PhotoKit | `expo-media-library` | ✓ `asuka/sdk/photos`：許可、一覧、サムネイル、元のファイル（Windows ではピクチャフォルダー） |
| 動画のサムネイル | `AVAssetImageGenerator` | `expo-video-thumbnails` | ✓ `imageedit.VideoFrame` |
| 音声合成 | `AVSpeechSynthesizer` | `expo-speech` | ✓ `asuka/sdk/speech`（キュー、音声の種類、ピッチ、速さ、単語の区切りでの一時停止） |
| 連絡先 | Contacts、ContactsUI | `expo-contacts` | ✓ `asuka/sdk/contacts`（許可なしでピッカーと新規連絡先の画面。許可があればアドレス帳。Windows では追加すると vCard を開き、アドレス帳はなし） |
| カレンダー | EventKit、EventKitUI | `expo-calendar` | ✓ `asuka/sdk/calendar`（許可なしでイベントの編集画面。許可があればカレンダーとイベント。Windows では追加すると iCalendar ファイルを開き、カレンダーはなし） |
| ホーム画面ウィジェット | WidgetKit | `expo-widgets` | ✓ `asuka/sdk/widget` と `asuka.json` の `widgets`（すべてのサイズのテンプレート、iOS ではロック画面のものも。タイムライン、カウントダウン、URL 付きのタップ） |
| ライブアクティビティ | ActivityKit | `expo-live-activity` | ✓ `asuka/sdk/liveactivity`（iOS ではロック画面と Dynamic Island。Android では継続中の通知と Android 16 の Live Update。カウントダウン、進捗、アラート、サーバーからのプッシュによる更新と開始） |
| App Intents、ショートカット | App Intents、App Shortcuts | — | ✓ `asuka.json` の `shortcuts`（iOS では Siri のフレーズ付きの App Shortcuts で、Spotlight とショートカットアプリに表示。Android ではランチャーのショートカット） |
| カスタムフォント | `.font(.custom)` | `expo-font` | ✓ `Font.Family` |
| ライフサイクル | `scenePhase` | AppState | ✓ `app.OnLifecycle` |
| 起動画面 | `UILaunchScreen` | `expo-splash-screen` | ✓ `asuka.json` の `splash`。`asuka/sdk/splash` でアプリの読み込み中は表示を保持 |
| ステータスバー、ナビゲーションバー | `statusBarHidden`、`preferredStatusBarStyle` | `expo-status-bar`、`expo-navigation-bar` | ✓ `asuka/sdk/screen` の `SetBars` |
| 画面の向きの固定 | `supportedInterfaceOrientations` | `expo-screen-orientation` | ✓ `asuka/sdk/screen` の `Lock` |
| スクリーンショット | `isCaptured`、`userDidTakeScreenshotNotification` | `expo-screen-capture` | ✓ `asuka/sdk/screen` の `PreventCapture`（Android ではキャプチャーが空白になる。iOS では録画中は覆い隠す。macOS と Windows ではウィンドウをキャプチャーから除外）、`OnScreenshot`（Linux と Windows ではピクチャ／スクリーンショットフォルダーの新しい画像） |
| クラッシュレポート | MetricKit、Crashlytics、Sentry | Sentry、Crashlytics（`@react-native-firebase/crashlytics`） | ✓ `asuka/sdk/crash`：Go の panic と致命的なエラー、捕捉されなかった例外、MetricKit のクラッシュとハング、Android のネイティブクラッシュと ANR を、ブレッドクラムと値付きで収集。送り先はアプリが自由に決める（サービスは組み込んでいない）。dSYM とネイティブのデバッグシンボルは `asuka build` が出力 |

## 外観とアクセシビリティ

| | SwiftUI | Expo | Asuka |
|---|---|---|---|
| ダークモード、セマンティックカラー、アクセントカラー | 自動 | `useColorScheme` | ✓ セマンティックカラー、`asuka.json` の accent（Android では Material 3 のダイナミックカラー。Mac では AppKit 自身のコントロールはユーザーのアクセントカラーのまま） |
| Dynamic Type、フォントサイズ | 自動 | `allowFontScaling` | ✓ テキストスタイルもカスタムフォントも拡大縮小される |
| スクリーンリーダーのラベル、ロール、非表示 | `.accessibilityLabel`... | `accessibilityLabel`... | ✓ `Label`、`Hint`、`Role`、`Decorative`。押せるビューはボタンになる |
| 読み上げ | `AccessibilityNotification.Announcement` | `announceForAccessibility` | ✓ `Announce` |
| スクリーンリーダーのフォーカスの移動 | `@AccessibilityFocusState` | `setAccessibilityFocus` | ✓ `AccessibilityFocus` |
| スクリーンリーダーが有効かどうか | `voiceOverEnabled` | `isScreenReaderEnabled` | ✓ `Environment.ScreenReader` |
| カスタムアクション、調整可能なビュー | `.accessibilityAction`、`.accessibilityAdjustableAction` | `accessibilityActions` | ✓ `ViewProps.Actions`、`OnAdjust`（List のスワイプアクションも TalkBack のアクションになる） |

## ツール

| | Xcode / Expo | Asuka |
|---|---|---|
| ライブリロード、状態の保持 | プレビュー、Fast Refresh | ✓ `asuka dev`：保存するとリロードし、シグナルの値を保持（スマートフォンでは Go のコードは手元のコンピューターで動く。デスクトップではアプリをビルドし直して再実行。ブラウザーではページを再読み込み） |
| リリースビルドと署名 | Xcode、EAS Build | ✓ `asuka build`：`.ipa`、`.aab`、署名と公証を済ませたユニバーサルな `.app`（または Mac App Store 用の `.pkg`）、署名済みの `.exe` を含む Windows のフォルダー、`.tar.gz` と Flatpak のマニフェストを含む Linux のインストールプレフィックス、静的サイト（[配布](shipping.md)） |
| ストアへのアップロード | Xcode、EAS Submit | ✓ `asuka publish`：TestFlight、App Review、Google Play のトラック。Mac App Store にはまだ非対応 |
| GUI のテスト | XCUITest、Detox、Maestro | ✓ `e2e/`：iOS シミュレーター、Android エミュレーター、Xvfb 上の Linux、ヘッドレス Chrome でサンプルを実行。Mac と Windows のホストのテストは、実際の AppKit のビューと Win32 のウィンドウを操作する |
| ベンチマーク | Instruments | ✓ `bench/`（SwiftUI、Compose との比較） |
| 既存のアプリへの組み込み | — | ブラウンフィールド（React Native のもの） | ✓ `asuka build --platform ios --xcframework`、`asuka build --platform android --aar` |
| OTA アップデート | — | EAS Update：✗ |

## デスクトップのフレームワーク

Mac、Windows、Linux では、HTML を Web ビューで包んだアプリもよく作られます。そうしたフレームワークと Asuka を比べると次のようになります。

| | Electron | Tauri | Wails | React Native for Windows and macOS | Asuka |
|---|---|---|---|---|---|
| プラットフォーム | Mac、Windows、Linux | Mac、Windows、Linux。iOS と Android（Tauri 2） | Mac、Windows、Linux | Windows、Mac（React Native の iOS、Android と合わせて） | Mac、Windows、Linux、iOS、Android、Web |
| UI を表示するもの | Chromium の中の HTML と CSS | システムの Web ビュー（WKWebView、WebView2、WebKitGTK）の中の HTML と CSS | システムの Web ビューの中の HTML と CSS | プラットフォームのネイティブのビュー | 各プラットフォーム純正のウィジェット：AppKit、Windows 11 風に描画した Win32 とコモンコントロール、GTK 4 と libadwaita |
| アプリの言語 | JavaScript（ページとは別に Node.js） | Rust と、ページ内の JavaScript | Go と、ページ内の JavaScript | JavaScript | Go |
| アプリと一緒に配布するもの | Chromium と Node.js | アプリのバイナリ（Web ビューはシステムのもの） | アプリのバイナリ（同上） | JavaScript エンジンと React Native のランタイム | アプリの Go のバイナリ（Linux では GTK 4 と libadwaita はシステムのもの）。アプリが `WebView` を表示しない限り Web ビューは含まない |

Web ビューのアプリは、HTML と CSS で書いたとおりに、どのシステムでも同じ見た目になり、Web のライブラリを使えます。Asuka のアプリは各プラットフォームのアプリと同じ見た目になり（スタイルを設定しないアプリは、Mac では Mac のアプリ、Linux では GNOME のアプリ、Windows では Windows 11 のアプリになります）、そのプラットフォームのアクセシビリティ、テキスト入力、メニューをそのまま使えます。しかも、レイアウトエンジンは Go で書かれた一つだけで、すべてのプラットフォームに共通です。デスクトップのホストは、アプリの Go のコードをアプリ自身のプロセスで実行し、cgo を使わずにプラットフォームを呼び出します。そのため `asuka build` は、どのシステムからでも Windows と Linux のビルドを作れます。

## デスクトップアプリの機能

デスクトップアプリがウィンドウとメニューバー以外に必要とする機能を、Electron、MyGo と比べます（[デスクトップ](desktop.md)）。行に特に記載がない限り、✓ は Mac、Windows、Linux で使えることを表します。

| | Electron | MyGo | Asuka |
|---|---|---|---|
| ウィンドウの制御 | `BrowserWindow` | `Window` | ✓ `ui.UseWindow`、`ui.MainWindow`：表示、非表示、フォーカス、最小化、最大化、フルスクリーン、サイズ、位置、常に手前に表示、状態をシグナルで取得。`OnCloseRequest`。`StateKey` による前回の位置の復元。子ウィンドウとモーダルウィンドウ。タイトルバーの非表示（Wayland では位置の指定、中央への配置、常に手前に表示はできず、コンポジターが決める。X11 では Xlib で実現） |
| トレイアイコン | `Tray` | トレイアイコン | ✓ `tray.Show`（`NSStatusItem`、通知領域、Linux では StatusNotifierItem）：アイコンまたはシンボル、タイトル、ツールチップ、メニュー、クリック |
| サブメニュー、終了の項目 | `MenuItem.submenu`、ロール | ロール、サブメニュー | ✓ `MenuItem.Submenu`、`ui.MenuQuit`。メニューバー、トレイ、Dock のメニューで使える |
| Dock のメニュー、ジャンプリストのタスク | `app.dock.setMenu`、`setUserTasks` | Dock のメニュー、ジャンプリスト | ✓ `tray.DockMenu`（Dock のメニュー、ジャンプリストのタスク、Linux のランチャーのクイックリスト） |
| バックグラウンドのアプリ | `app.dock.hide`、`window-all-closed` で終了しない | バックグラウンドのアプリ | ✓ `app.KeepRunning`、`app.RunInBackground`、`asuka.json` の `background` |
| フライアウト：トレイアイコンから垂れ下がるウィンドウ | アプリが `tray.getBounds()` の位置に置くフレームなしの `BrowserWindow` | フライアウト | ✓ `WindowProps.Flyout`。`tray.Icon.Anchor()` か、ウィンドウ内の矩形から垂れ下がる：ディスプレイに収まるように配置・反転し、ほかの場所のクリックか Escape で閉じ、上にモーダルダイアログも出せる（Linux では X11 の場合のみ配置） |
| ランチャー用のパネル | `type: 'panel'`（macOS） | パネル | ✓ `Panel`：すべてのアプリと操作スペースの上に浮かび、Mac ではアプリをアクティブにせずにキーボード入力を受け、Escape かほかの場所のクリックで閉じる（Linux の Wayland では通常のウィンドウ） |
| コンテンツに合わせたウィンドウのサイズ | アプリが `setContentSize` を呼ぶ | `FitContent` | ✓ `FitContent`：コンテンツが変わると、コアのレイアウトがウィンドウのサイズを決める |
| クリックを透過するウィンドウ | `setIgnoreMouseEvents` | `SetIgnoreMouseEvents`、`SetInputRegion` | ✓ `ClickThrough`、`WindowHandle.SetClickThrough`（ウィンドウ全体） |
| ランチャー向けのインストール済みアプリ | なし | `InstalledApps`、`FileIcon`、`LaunchApp` | ✓ `asuka/sdk/apps` の `List`、`Search`、`Icon`、`Open` |
| グローバルショートカット | `globalShortcut` | グローバルショートカット | ✓ `hotkey.Register`（Carbon のホットキー、`RegisterHotKey`、X11 のグラブまたは Wayland の GlobalShortcuts ポータル） |
| 開く、保存、フォルダーのダイアログ | `dialog.showOpenDialog`、`showSaveDialog` | ダイアログ | ✓ `asuka/sdk/dialog` の `Open`、`Save`、`Folder`（Mac ではシート。Web では File System Access のピッカーかファイル入力。スマートフォンでは `Open` のみ） |
| Finder などでの表示、既定のアプリで開く、ゴミ箱に移動 | `shell.showItemInFolder`、`openPath`、`trashItem` | シェル | ✓ `asuka/sdk/shell` の `Reveal`、`Open`、`Trash`、`Beep` |
| ファイルの関連付け | `fileAssociations`（electron-builder）、`open-file` | ファイルの関連付け | ✓ `asuka.json` の `fileTypes`、`ui.OnOpenFiles`（iOS、Android、インストールした Web アプリでも） |
| 単一インスタンスと二回目の起動 | `requestSingleInstanceLock`、`second-instance` | 単一インスタンス | ✓ `app.OnSecondInstance`、`asuka.json` の `singleInstance`（Mac では引数ではなくファイルを渡す） |
| 終了と、終了前の確認 | `app.quit`、`before-quit` | 終了 | ✓ `app.Quit`、`app.OnQuit`（`Cancel`、`SessionEnding`） |
| アプリのフォルダー | `app.getPath` | ディレクトリ | ✓ `app.DataDir`、`ConfigDir`、`CacheDir`、`LogDir`（すべてのプラットフォーム） |
| 最近使った書類 | `app.addRecentDocument` | 最近使った書類 | ✓ `asuka/sdk/recent`（Mac の「最近使った項目を開く」、ジャンプリスト、Linux のユーザーの最近使ったファイル） |
| 書類ベースのアプリ | — | 書類 | ✓ `asuka/sdk/document`：ファイルごとのウィンドウ、「ファイル」メニュー、自動保存、復元用のコピー、閉じるときと終了するときの保存の確認、ほかのアプリによるファイルの変更（スマートフォンと Web では一度に一つの書類で、その場で保存） |
| ログイン時の起動 | `app.setLoginItemSettings` | ログイン時に起動 | ✓ `asuka/sdk/login`（`SMAppService`、macOS 13 以降。Run キー。XDG autostart または Background ポータル） |
| ファイルの監視 | Node の `fs.watch`（chokidar） | `WatchFiles`、watch プラグイン | ✓ `asuka/sdk/watch` の `Path`、`New`（FSEvents、inotify、`ReadDirectoryChangesW`。iOS の kqueue、Android の inotify、Web のアプリ自身のファイルにも対応）：名前の変更は対にし、アトミックな保存は書き込みとして扱い、連続したイベントはまとめる |
| ディスプレイ | `screen` | ディスプレイ | ✓ `screen.UseDisplays`、`Nearest`、`Matching`（Wayland では作業領域なし。スマートフォンと Web ではそれぞれの画面） |
| スリープ、スリープ解除、ロック、アイドル時間、バッテリー | `powerMonitor` | 電源 | ✓ `asuka/sdk/power`（スマートフォンではバッテリー、低電力モード、ロック。Web ではバッテリー） |
| アプリのアイコンでの進捗と注意喚起 | `setProgressBar`、`flashFrame`、`dock.bounce` | Dock とタスクバー | ✓ `dock.SetProgress`、`RequestAttention`（Linux では Unity launcher API に対応したドック） |
| アクセントカラー、コントラストを上げる、透明度を下げる | `systemPreferences` | アクセントカラーとアクセシビリティ | ✓ `asuka/sdk/appearance`（Linux では「透明度を下げる」はなし） |
| 再生中の情報とメディアキー | （なし：ページ内の `navigator.mediaSession`） | Now Playing | ✓ `asuka/sdk/nowplaying`（`MPNowPlayingInfoCenter`、ウィンドウの SystemMediaTransportControls、MPRIS のプレイヤー） |
| 自動アップデート：署名付きのリリース、差分、Sparkle のウィンドウ | `autoUpdater`（Squirrel）、electron-updater | アップデーターとそのプラグイン | ✓ `asuka/sdk/update`（`Window`、`CheckForUpdates`、`Check`、`Install`、`Relaunch`。Ed25519 の署名、差分アップデート、チャンネル。GitHub のリリース、S3 のバケット、任意の HTTPS のディレクトリ、独自のサーバー。`asuka publish --platform mac\|windows\|linux`）。システムのパッケージ、Flatpak、ストアでインストールしたアプリは、それらを通じてアップデートする。Web は再読み込み |

MyGo にあって Asuka にまだないものは次のとおりです。書類については、バージョン（Mac の「すべてのバージョンをブラウズ」）とロック。クリックを透過するウィンドウについては、入力領域（ウィンドウのうち、クリックを受け付け続ける部分）の指定です。Asuka のウィンドウは、マウスをウィンドウ全体で透過させるか、まったく透過させないかのどちらかです。

## 未対応のもの

アプリでの需要が高い順に、パラメーターとエンティティを持つ App Intents（ショートカットはまだどちらも受け付けません）、アプリ独自のレイアウトのウィジェット（いまはテンプレートが一つだけです）、OTA アップデート（EAS Update のようなもの。Asuka のアプリはコンパイルされた Go で、ストアはアプリがそれを置き換えることを認めていません）です。Mac では、`ScreenOptions.ZoomFrom` のズーム（代わりにプッシュがスライドします）、ジェスチャーによるリストの選択の開始、`asuka publish` による Mac App Store へのアップロード（`.pkg` は Transporter でアップロードしてください）が未対応です。

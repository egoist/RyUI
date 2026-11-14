# 翻訳ガイド

`docs/ja` は `docs` の日本語版で、サイトでは `/ja/docs` に公開されます。このページは日本語ドキュメントを翻訳・保守する人のためのもので、サイトには公開されません。英語のページが変わったら、対応する日本語のページも合わせて更新し、見出しを一対一で揃えてください。

## 方針

**英文をなぞるのではなく、日本語の読者に向けて書きます。** 英語の原文にはカンマで節を次々につなぐ文、「the X of Y」、主語の省略、名詞化が多く出てきます。この構造をそのまま写すと、機械翻訳のような日本語になります。まず段落全体で何を言っているかをつかみ、Go とモバイル開発に詳しい日本のエンジニアが書くならどう書くかを考えて書き直してください。文を分けたり、まとめたり、語順を入れ替えたり、主語や接続詞（「そのため」「つまり」「ただし」）を補ったりしてかまいません。日本語では不要な指示語は削ります。ただし事実は足さず、削らず、細かな条件や制限、例外も一つ残らず訳します。

文体は「です・ます」調です。お手本は react.dev/ja、Vue.js の日本語ドキュメント、MDN の日本語版のような、コミュニティがよく手入れしている技術文書です。平易で正確、硬すぎず砕けすぎない文にします。読者を「あなた」と呼ぶのは必要なときだけにし、たいていは主語を省くか「アプリ」「開発者」を主語にします。流行語や過度な敬語（「〜させていただきます」）は使いません。

## 書式

- **構成を一対一で揃えます**：見出しの数、レベル、順番は英語版とまったく同じにします（`#`、`##`、`###` …… 一つも増減させない）。サイトとリンクのアンカーがこれに依存しています。見出しそのものは自然な日本語に訳し、名詞で終える形（「仕組み」「レイアウトの更新」）を基本にします。
- コードブロックのコードはそのままにします。コード中の**コメント**は日本語に訳します（シェルの `# ...`、Go の `// ...` も）。文字列リテラル、識別子、コマンド、パス、引数は変えません。
- インラインコード（`` `ui.Button` ``）、API 名、型名、ファイル名、コマンド、環境変数は英語のままにします。
- リンク：ドキュメントのページ間のリンクは日本語のページを指し、アンカーは日本語の見出しから作られるもの（GitHub の規則：小文字にし、記号を除き、スペースを `-` に置き換える。例：`parity.md#データとサービス`）を使います。リポジトリのほかのファイルへのリンクは `docs/ja/` からの相対パスで、英語版より `../` が一つ多くなります（`../../examples/gallery`）。英語版しかないページへは `../` を付けます（`../device-checklist.md`）。外部リンクはそのままにします。
- `<!-- repository-only:start -->` / `<!-- repository-only:end -->` のような HTML コメントはそのまま残し、その間の内容は通常どおり訳します。
- 表は列の数を変えず、セルの中身を訳します。
- 日本語と英数字（インラインコードを含む）のあいだには半角スペースを入れます（「iOS 16 では」「`asuka dev` で実行します」）。ただし句読点や括弧の隣には入れません（「`ui.Text`、`ui.Button`」）。
- 句読点は全角の「、」「。」を使います。括弧は全角の（）を使い、中身がすべて英数字やコードのときは半角の () でもかまいません。UI の文字列や用語を引用するときは「」を使います。
- 長音は Microsoft のスタイルガイドに合わせて付けます（ユーザー、サーバー、ブラウザー、フォルダー、エディター、パラメーター、コンピューター）。慣用が定着した語はそのままにします（ライブラリ、ディレクトリ、メモリ、バイナリ）。
- 行を折り返す必要はありません。一つの段落は一行で書きます（原文の 72 桁折り返しは引き継ぎません）。
- 太字、斜体、引用ブロックは原文と同じ位置に残します。
- OS の設定やメニューの名前は、各 OS の日本語版の表記を使います（iOS の「視差効果を減らす」、Android の「アニメーションを削除」など）。確かでないときは英語の名前を括弧で添えます。

## 用語集

| 英語 | 日本語 | 備考 |
| --- | --- | --- |
| widget | ウィジェット | "the platform's own widgets" →「各プラットフォーム純正のウィジェット」。ホーム画面のウィジェットは「ホーム画面ウィジェット」 |
| control | コントロール | Win32 の common controls は「コモンコントロール」 |
| native | ネイティブ | |
| component | コンポーネント | |
| view | ビュー | |
| host | ホスト | |
| core | コア | Asuka のコア |
| signal | シグナル | OS や GTK のシグナルは文脈で区別する（「GTK のシグナル」） |
| effect | エフェクト | |
| owner | オーナー | |
| accessor | アクセサ | |
| memo | メモ | `signal.Memo` のこと |
| props | プロパティ / props | Go の構造体のフィールドは「プロパティ」、`XxxProps` 型は props のまま |
| style | スタイル | |
| layout | レイアウト | |
| flexbox, grid | flexbox、grid | 英語のまま |
| safe area | セーフエリア | |
| screen | 画面 | |
| stack (navigation) | スタック / ナビゲーションスタック | |
| tab | タブ | tab bar →タブバー |
| sheet | シート | |
| alert | アラート | |
| menu bar | メニューバー | |
| toolbar | ツールバー | |
| window | ウィンドウ | |
| main thread | メインスレッド | |
| batch | バッチ | |
| flush | フラッシュ | |
| op | op | プロトコル上の操作。必要に応じて「操作（op）」 |
| hot reload / reload on save | ホットリロード / 保存するとリロード | |
| simulator | シミュレーター | Xcode の iOS シミュレーター |
| emulator | エミュレーター | Android Emulator |
| player (app) | プレイヤーアプリ | 開発時にデバイスに入れる Asuka のプレイヤー |
| device | デバイス / 実機 | 物理的なスマートフォンを指すときは「実機」 |
| phone | スマートフォン | 文脈によっては「モバイル」 |
| release build | リリースビルド | |
| sign / signed | 署名 / 署名済み | |
| notarize | 公証 | Apple の公証（notarization） |
| bundle ID / application ID | bundle ID / application ID | 英語のまま |
| native package | ネイティブパッケージ | |
| plugin | プラグイン | |
| accessibility | アクセシビリティ | screen reader →スクリーンリーダー |
| locale | ロケール | |
| right-to-left | 右から左（RTL） | |
| animation | アニメーション | spring →スプリング、transition →トランジション、shared element →共有要素 |
| gesture | ジェスチャー | |
| haptics | 触覚フィードバック | |
| tray icon | トレイアイコン | |
| Dock | Dock | |
| notification | 通知 | push →プッシュ通知 |
| store (App Store / Google Play) | ストア | |
| the web | Web | "on the web" →「Web では」「ブラウザーでは」 |
| the desktop | デスクトップ | |
| Mac / PC | Mac / PC | 英語のまま |
| app | アプリ | |
| goroutine | ゴルーチン | コード中の `go` 文や識別子はそのまま |
| handler | ハンドラー | event handler →イベントハンドラー |
| interface | インターフェース | |
| container | コンテナー | |
| fallback | フォールバック | |
| dispose / cleanup | 破棄 / クリーンアップ | |
| build (a view) | 組み立てる | コンポーネントがビューを作ること。ビルド（コンパイル）とは区別する |
| live query | ライブクエリ | |
| secure storage | セキュアストレージ | |
| capability (Apple) | ケイパビリティ | App ID の Push Notifications ケイパビリティなど |
| entitlement | エンタイトルメント | |
| provisioning profile | プロビジョニングプロファイル | |
| purpose string | 利用目的の説明文 | Info.plist の `NS…UsageDescription` |
| extension (iOS) | 拡張機能 | share extension →共有拡張機能 |
| ad hoc signing | アドホック署名 | |
| delta update | 差分アップデート | |
| announcement (screen reader) | 読み上げ | |
| pseudo locale | 疑似ロケール | |
| Now Playing | 再生中の情報（Now Playing） | macOS のコントロールセンターでは「再生中」 |
| predictive back | 予測型「戻る」 | |
| flyout | フライアウト | |

OS が日本語で表示する名前は、それぞれの表記に従います。Apple の UI は長音を付けない表記（「ユーザとアクセス」「ステージマネージャ」）のまま引用し、ごみ箱は Mac では「ゴミ箱」、Windows では「ごみ箱」と書きます。Asuka 自身が表示する文字列（アップデートのウィンドウ、書類のメニューなど）は、カタログ（`internal/l10n/messages/ja.json`、`sdk/document/messages/ja.json`）の訳語に合わせます。

製品名やフレームワーク名（UIKit、AppKit、Material 3、GTK、libadwaita、WebView2、SwiftUI、Expo、React Native ……）はすべて英語のままにします。

ページのタイトル（`#` の見出し）と目次での名前は次のとおりです。

| ページ | タイトル |
| --- | --- |
| getting-started.md | はじめに |
| concepts.md | 基本概念 |
| layout.md | レイアウトとスタイル |
| components.md | コンポーネント |
| navigation.md | ナビゲーション |
| motion.md | モーションとジェスチャー |
| accessibility.md | アクセシビリティ |
| i18n.md | 多言語対応 |
| data.md | データ |
| device.md | デバイスとサービス |
| desktop.md | デスクトップ |
| plugins.md | プラグイン |
| native.md | ネイティブコード |
| development.md | 開発 |
| testing.md | テスト |
| shipping.md | 配布 |
| web.md | Web |
| asuka-json.md | asuka.json |
| parity.md | SwiftUI・Expo との比較 |
| mygo.md | Asuka と MyGo |

## 例

> Asuka makes apps in Go with each platform's own widgets, on iOS, Android, macOS, Windows, Linux and the web.

よくない例：Asuka は、iOS、Android、macOS、Windows、Linux、そして Web 上で、それぞれのプラットフォーム自身のウィジェットで Go でアプリを作ります。

よい例：Asuka を使うと、Go で書いたアプリを iOS、Android、macOS、Windows、Linux、Web で、各プラットフォーム純正のウィジェットを使って動かせます。

> Save a Go file and the app reloads in a moment, the values of its signals kept.

よくない例：Go ファイルを保存すると、アプリは少しの間にリロードし、そのシグナルの値は保たれます。

よい例：Go のファイルを保存すると、アプリはすぐにリロードされます。シグナルの値はそのまま残ります。

# コンポーネント

コンポーネントは、`ui.Node` を返す Go の関数です。このページでは、パッケージ `ui` が提供するビューを役割ごとに紹介し、あわせて Markdown と SVG を扱う関連パッケージも取り上げます。どのビューも、プラットフォームに対応するウィジェットがあればそれを使います。iOS では UIKit、Android では Android Views と Material 3、macOS では AppKit、Windows では Windows 11 の見た目（Fluent）で描画した Win32 のコントロール、Linux では GTK 4 と libadwaita、ブラウザーではページの要素です。ビューの大きさと配置については[レイアウトとスタイル](layout.md)を、画面、タブ、シートについては[ナビゲーション](navigation.md)を参照してください。

変化しうるプロパティの多くは `signal.Accessor` を受け取ります。シグナルか `signal.Func` を渡すか、変わらない値なら `ui.Val(v)` を渡します。ユーザーが値を変えるコントロール（テキストフィールドやトグルなど）は、代わりに `*signal.Signal` を受け取り、そこに書き込みます。詳しくは[基本概念](concepts.md)を参照してください。

## 各プラットフォームのウィジェット

| コンポーネント | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Text` | `UILabel` | `TextView` | `NSTextField`（ラベル） | ホストのビューが描画 | `GtkLabel` | ページのテキスト |
| `Button` | `UIButton` | `MaterialButton` | `NSButton` | Fluent の見た目で描画した `BUTTON` | `GtkButton` | `<button>` |
| `TextField` | `UITextField`、複数行は `UITextView` | `EditText` | `NSTextField`、`NSSecureTextField`、複数行は `NSTextView` | Fluent の見た目で描画した枠の中の `EDIT` | `GtkEntry`、`GtkPasswordEntry`、複数行は `GtkTextView` | `<input>`、複数行は `<textarea>` |
| `Toggle` | `UISwitch` | `MaterialSwitch` | `NSSwitch` | トグルスイッチとして描画した `BUTTON` のチェックボックス | `GtkSwitch` | `<input type="checkbox" switch>` |
| `Slider` | `UISlider` | Material の `Slider` | `NSSlider` | トラックバー | `GtkScale` | `<input type="range">` |
| `Segmented` | `UISegmentedControl` | Material のセグメントボタン | `NSSegmentedControl` | Windows Community Toolkit のものに倣って描画 | 連結した `GtkToggleButton` | ラジオグループのボタン |
| `Stepper` | `UIStepper` | Material のアイコンボタン 2 つ | `NSStepper` | ボタン 2 つ | 連結した − と + のボタン | ボタン 2 つ |
| `Wheel` | `UIPickerView` | `NumberPicker` | `NSPopUpButton` | ドロップダウンの `COMBOBOX` | `GtkDropDown` | `<select>` |
| `ColorPicker` | `UIColorWell` | 色見本と Material のダイアログ | `NSColorWell` とカラーパネル | 色見本と色の設定ダイアログ（`ChooseColor`） | `GtkColorDialogButton` | `<input type="color">` |
| `DatePicker` | `UIDatePicker` | Material のピッカーを開くボタン | `NSDatePicker` | 日付と時刻の選択コントロール（`SysDateTimePick32`） | `GtkCalendar` とスピンボタンを開くボタン | `<input type="date">`、`time`、`datetime-local` |
| `Progress` | `UIProgressView` | `LinearProgressIndicator` | `NSProgressIndicator` | Fluent の見た目で描画 | `GtkProgressBar` | `<progress>` |
| `ProgressRing` | SwiftUI の円形ゲージに倣って描画。値がなければ `UIActivityIndicatorView` | `CircularProgressIndicator` | 16 ポイントと 32 ポイントでは spinning スタイルの `NSProgressIndicator`。それ以外のサイズや `Color` の指定時は、そのスタイルに倣って描画 | Fluent の `ProgressRing` に倣って描画 | GNOME のスタイルで描画。値がなければ `AdwSpinner` | SVG のリング |
| `Activity` | `UIActivityIndicatorView` | `CircularProgressIndicator` | spinning スタイルの `NSProgressIndicator` | Fluent のプログレスリングに倣って描画 | `AdwSpinner`（libadwaita 1.6 より前は `GtkSpinner`） | 描画したスピナー |
| `Image` | `UIImageView` | `ImageView` | `NSImageView` | GDI+ で描画 | GDK のテクスチャから描画 | `<img>` |
| `Symbol` | SF Symbols | Material Symbols | SF Symbols | Segoe Fluent Icons | アイコンテーマのもの。なければ Material Symbols | Material Symbols |
| `Video` | `AVPlayerViewController` | Media3 のプレイヤー | `AVPlayerView` | WebView2 内の `<video>` | `GtkVideo`（GStreamer） | `<video>` |
| `WebView` | `WKWebView` | `WebView` | `WKWebView` | WebView2 | `WebKitWebView`（WebKitGTK 6.0） | `<iframe>` |
| `Scroll` | `UIScrollView` | `NestedScrollView` | `NSScrollView` | ホストのビューと Fluent のスクロールインジケーター | `GtkScrolledWindow` | スクロールする要素 |
| `List` | `UICollectionView` | `RecyclerView` | `NSScrollView` の中でホストが再利用する行 | ホストが再利用する行 | `GtkScrolledWindow` の中でホストが再利用する行 | ホストが再利用する行 |

以降のセクションでは、プラットフォームによって動作が異なる点を説明します。

## テキスト

### Text と StaticText

`ui.Text` は、1 行または 1 段落のテキストを表示します。プラットフォームのラベルそのもので、デフォルトで折り返します。`Style.Lines` で行数を制限でき、はみ出した部分は省略記号で切り詰められます。`ui.StaticText` は、テキストが変わらない `Text` です。

```go
count := signal.New(0)
label := signal.Func(func() string { return fmt.Sprintf("%d unread", count.Get()) })
return ui.Column(ui.Style{Gap: 8},
	ui.StaticText("Inbox", ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}),
	ui.Text(label, ui.Style{Color: colors.SecondaryLabel}),
	ui.StaticText("A long description that stops after two lines.", ui.Style{Lines: 2}),
)
```

スタイルの `Font`、`Color`、`TextAlign`、`Lines` がテキストに適用されます。テキストスタイル（`fonts.Body`、`fonts.Headline`、`fonts.Footnote` など）は、ユーザーが選んだ文字サイズに従います。

### RichText

`ui.RichText` は、装飾付きのテキストを表示します。テキストは `ui.Rich`、つまりスパンのリストで表します。スパンの中の `"\n"` で新しい段落が始まります。スパンのフィールドのうちゼロ値のままのものは、`RichText` のスタイルから引き継がれます。

```go
ui.RichText(ui.RichTextProps{
	Text: ui.Val(ui.Rich{
		ui.Strong("asuka"), ui.Plain(" draws "), ui.Em("native"), ui.Plain(" views. Run "),
		ui.Code("asuka dev"), ui.Plain(", or read "), ui.Link("the docs", "https://example.com/docs"),
		ui.Plain(".\nA second paragraph, "), ui.Plain("in red").Colored(colors.Red), ui.Plain("."),
	}),
	OnLink: ui.OpenURL,
})
```

- スパンは `Plain`、`Strong`、`Em`、`Code`、`Link` で作ります。`Colored`、`Highlighted`、`Weighted` は、それぞれ色、背景、太さを付けたスパンを返します。
- そのほかの指定には `ui.Span` のフィールドを使います。`Font`、`Italic`、`Mono`、`Underline`、`Strike`、`Background`（スパンがかかる各行に、角を丸めて描画されます）があります。
- リンクがタップされると、その URL を引数に `OnLink` が呼ばれます。`OnLink` がなければ、リンクは何もしません。
- `Role: ui.RoleHeader` を指定すると、スクリーンリーダーに見出しとして伝わります。

iOS と Android では、各段落をプラットフォームの組版エンジン（CoreText、StaticLayout）が組み、内容と幅をキーにしてキャッシュします。そのため、ストリーミングで届く返信のように長いテキストに追記していく場合でも、組み直されるのは変わった段落だけです。macOS では読み取り専用の `NSTextView`（TextKit）、Windows では読み取り専用のリッチエディット（`RICHEDIT50W`）、Linux では Pango の属性で装飾した `GtkLabel`、ブラウザーでは段落の中のスパンになります。[`examples/richtext`](../../examples/richtext) は、テキストをストリーミング表示する例です。

プレーンテキストには `Text` を使ってください。プラットフォームのラベルそのものなので、ネイティブの動作がそのまま得られます。

**インラインビュー。** `ui.Embed` を使うと、テキストの中にコンポーネントを置けます。コンポーネントはベースラインに乗り、単語と一緒に折り返されます。キーが同じなら、`Rich` が新しくなっても同じコンポーネントとその状態が保たれます。

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{
	ui.Plain("Status: "),
	ui.Embed("badge", func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Background: colors.Green, CornerRadius: 6, Padding: ui.Symmetric(2, 6)}},
			ui.StaticText("live", ui.Style{Font: fonts.Caption, Color: colors.RGB(255, 255, 255)}))
	}),
	ui.Plain(" since this morning."),
})})
```

### 選択できるテキスト

`ui.Selectable` は、その中にある `RichText` ビューを選択できるようにします。複数のビューにまたがって、ひとつながりのテキストとして選択できます。長押しで単語を選択し、プラットフォームのハンドルで範囲を広げ、編集メニューからコピーします。マウスではドラッグで選択し、プラットフォームのコピーのショートカットでコピーします。Linux と Web でも複数のテキストをひとつながりで選択できますが、macOS と Windows ではテキストごとに個別に選択します。

```go
ui.Selectable(ui.SelectableProps{Style: ui.Style{Gap: 12}},
	ui.RichText(ui.RichTextProps{Text: ui.Val(question), Role: ui.RoleHeader}),
	ui.RichText(ui.RichTextProps{Text: ui.Val(answer)}),
)
```

`Text` は選択できません。選択させたいときは、`Selectable` の中で `RichText` を使ってください。選択を介さずに何かをコピーするには、`ui.Copy(text)` でクリップボードに入れます。

### Markdown

パッケージ `asuka/sdk/markdown` は、Markdown を `RichText` で組み立てたネイティブのビューとして表示します。テキストが追記されるたびに解析しますが、解析し直すのは書きかけのブロックだけで、更新されるのもそのブロックのビューだけです。

```go
markdown.View(markdown.Props{Text: reply, Streaming: writing})
```

`Streaming` は、テキストがまだ書かれている途中であることを示します。これにより、書きかけの `**bold` はアスタリスクのままではなく太字として表示されます。ビューはデフォルトで全体がひとつの選択範囲になります（`NotSelectable` で無効にできます）。リンクは、`OnLink` を指定しない限り `ui.OpenURL` で開きます。`Theme` でフォント、色、余白を変えられ、`Code` でコードブロックを独自の方法で表示できます。CommonMark と GitHub の拡張に対応しています。[`examples/markdown`](../../examples/markdown) を参照してください。

## ボタン

`ui.Button` はプラットフォームのボタンです。iOS では `UIButton` の configuration、Android では Material のボタン、macOS では `NSButton`、Windows では Fluent の見た目で描画した `BUTTON`、Linux では `GtkButton`、ブラウザーでは `<button>` になります。

```go
saving := signal.New(false)
return ui.Row(ui.Style{Gap: 12, Wrap: true},
	ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, Disabled: saving,
		OnPress: func() { saving.Set(true) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonTinted}),
	ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: ui.Val("Delete"), Variant: ui.ButtonGray}),
)
```

- `Title` はボタンのテキストで、`Icon` はその前に置くシンボルです（[シンボル](#シンボルとアイコン)を参照）。アイコンがあってタイトルがないボタンは、アイコンだけのボタンになります。
- `Label` は VoiceOver や TalkBack が読み上げる名前で、デフォルトではタイトルが使われます。アイコンだけのボタンには指定してください。
- `Tooltip` は、ポインターを重ねているあいだ表示される短いテキストです。iPad のポインター（`UIToolTipInteraction`）、Android のマウス（`tooltipText`。Android では長押しでも表示されます）、macOS、Windows、Linux、Web（要素の `title`）で表示されます。アイコンだけのボタンには、その名前をツールチップとして指定してください。`ViewProps.Tooltip` は、任意のビューで同じ働きをします。iOS ではタッチスクリーンの操作だけでは決して表示されないので、ツールチップでしか伝えない情報を作らないでください。
- `Disabled` は、プラットフォームの方法でボタンを無効にします。
- `Style.Font` と `Style.Color` で、テキストと色合い（tint）を変えられます。

`Variant` はボタンの見た目です。

| Variant | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| `ButtonPlain`（デフォルト） | plain | テキストボタン | 枠なし、アクセントカラー | subtle、アクセントカラー | フラット、アクセントカラー | プレーン |
| `ButtonFilled` | filled | Filled ボタン | アクセントカラーのプッシュボタン | アクセントボタン | suggested action | 塗りつぶし |
| `ButtonTinted` | tinted | Tonal ボタン | タイトルがアクセントカラーのプッシュボタン | アクセントカラーの淡い塗り | アクセントカラーの淡い塗り | 淡い色付き |
| `ButtonGray` | gray | Tonal ボタン | プッシュボタン | 標準のボタン | 標準のボタン | グレー |
| `ButtonBordered` | bordered | Outlined ボタン | プッシュボタン | 標準のボタン | 標準のボタン | 枠線付き |
| `ButtonGlass` | Liquid Glass（`glass()`、iOS 26。それより前は gray） | Tonal ボタン | `ButtonGray` と同じ | `ButtonGray` と同じ | 標準のボタン | `ButtonGray` と同じ |
| `ButtonGlassProminent` | 色付きの Liquid Glass（`prominentGlass()`、iOS 26。それより前は filled） | Filled ボタン | `ButtonFilled` と同じ | `ButtonFilled` と同じ | suggested action | `ButtonFilled` と同じ |

ガラスのボタンはカプセル形で、アイコンだけのガラスのボタンは円形になります。iOS 26 純正のシートの閉じるボタンと同じ形です。

### メニュー

`Menu` を指定したボタンは、タップされると `OnPress` を呼ぶ代わりにアクションの一覧を表示します（iOS では `UIMenu`、Android では `PopupMenu`、macOS では `NSMenu`、Windows ではポップアップメニュー、Linux では `GtkPopoverMenu`、ブラウザーではポップオーバー）。各 `ui.Action` には、`Title`、その横に添える任意の `Icon`、`Role`、`OnPress` があります。`ui.ActionDestructive` を指定すると赤で表示されます。

```go
ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: []ui.Action{
	{Title: ui.Val("Copy"), Icon: ui.IconCopy, OnPress: func() { ui.Copy("https://go.dev") }},
	{Title: ui.Val("Share"), Icon: ui.IconShare, OnPress: func() { ui.Share("https://go.dev") }},
	{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {}},
}})
```

アクションは `Disabled`（淡色で表示され、選べなくなる）や `Checked`（横にチェックマークが付く）にできます。どちらも、メニューが開いているあいだもシグナルに追従します。`Children` を指定するとサブメニューになります。サブメニューは `ui.Submenu` でも作れます。`Inline` を指定すると、子はサブメニューではなくメニュー内のセクションになり、タイトルの下に区切り線で仕切られて並びます（SwiftUI の `Menu` の中の `Section` に相当します）。セクションは `ui.MenuSection` でも作れます。チェックされた項目を含むセクションは、選択肢として働きます。

```go
sortBy := signal.New("Name")
sorted := func(by string) ui.Action {
	return ui.Action{Title: ui.Val(by), Checked: signal.Func(func() bool { return sortBy.Get() == by }),
		OnPress: func() { sortBy.Set(by) }}
}
ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: []ui.Action{
	ui.MenuSection(ui.Val("Sort by"), sorted("Name"), sorted("Date")),
	ui.Submenu(ui.Val("Move to"), ui.IconFolder,
		ui.Action{Title: ui.Val("Inbox"), OnPress: moveToInbox},
		ui.Action{Title: ui.Val("Archive"), OnPress: archive},
	),
	{Title: ui.Val("Delete"), Role: ui.ActionDestructive, Disabled: locked, OnPress: remove},
}})
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Disabled` | `.disabled` | 無効な項目 | 無効な項目 | 淡色表示の項目 | 無効なアクション | `disabled`、`aria-disabled` |
| `Checked` | `state = .on` | チェックされた項目。セクションの選択肢はラジオボタン | `state = on` | `MF_CHECKED` | 状態を持つアクション（チェック） | `menuitemcheckbox` |
| `Children` | 入れ子の `UIMenu` | `SubMenu` | サブメニュー | サブメニュー | GMenu のサブメニュー | サブメニュー（そちら向きの矢印キーで開く） |
| `Inline` | `displayInline` の `UIMenu` | 区切り線で仕切ったグループ。タイトルは無効な項目 | 区切り線とセクションヘッダー（macOS 14） | 区切り線とオーナードローのヘッダー | ラベル付きの GMenu セクション | 区切り線のあいだ、見出しの下に置いた `group` |

画面のバーボタン（`ScreenOptions.Actions`）も同じです。無効なボタンは淡色で表示され、`Children` を持つボタンはそれをメニューとして開きます（ツールバーの中の SwiftUI の `Menu` に相当します）。アラートのボタンも無効にでき、表示中も `Disabled` に追従します（名前が入力されるまで押せない OK ボタンなど）。スワイプアクションは、無効の状態もサブメニューも表示しません。

`ui.Menu` は、任意のコンテンツにメニューを付けます。`LongPress` を指定するとコンテキストメニューになり、長押しで表示されます。マウスを使う環境（macOS、Windows、Linux、Web）では、副ボタンのクリックで表示されます。

```go
ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
	{Title: ui.Val("Reply"), Icon: ui.IconMessage},
	{Title: ui.Val("Delete"), Role: ui.ActionDestructive},
}, Style: ui.Style{Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12}},
	ui.StaticText("Long-press for a menu"),
)
```

## テキストフィールド

`ui.TextField` はテキストの入力欄です。`Value` がそのテキストで、ユーザーが入力するとシグナルに書き込まれ、コードからシグナルに書き込むとテキストが置き換わります。

```go
email := signal.New("")
password := signal.New("")
var pw ui.Node
pw = ui.TextField(ui.TextFieldProps{Value: password, Placeholder: ui.Val("Password"),
	Secure: true, ContentType: "password", ReturnKey: ui.ReturnGo,
	OnSubmit: func() { ui.Blur(pw) }})
em := ui.TextField(ui.TextFieldProps{Value: email, Placeholder: ui.Val("Email"),
	Keyboard: ui.KeyboardEmail, ContentType: "email", Autocapitalize: ui.CapitalizeNone,
	Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnNext, OnSubmit: func() { ui.Focus(pw) }})
return ui.Column(ui.Style{Gap: 12}, em, pw)
```

- `Secure` は、パスワード用に入力内容を隠します。
- `Keyboard`：`KeyboardDefault`、`KeyboardNumber`、`KeyboardDecimal`、`KeyboardEmail`、`KeyboardURL`、`KeyboardPhone`。
- `ReturnKey` はリターンキーのラベルです：`ReturnDefault`、`ReturnDone`、`ReturnGo`、`ReturnNext`、`ReturnSearch`、`ReturnSend`。押すと `OnSubmit` が呼ばれます。
- `ContentType` は、システムの自動入力（AutoFill）のために、フィールドに入る内容の種類を示します。`ui.ContentUsername`、`ContentPassword`、`ContentNewPassword`（プラットフォームが強力なパスワードを提案します）、`ContentOneTimeCode`（届いたばかりのメッセージのコードがキーボードの上に表示されます。UIKit の `oneTimeCode`、Android の SMS コードのヒント）、`ContentEmail`、`ContentPhone`、`ContentName`、`ContentAddress`、`ContentPostalCode`、`ContentCardNumber` などがあります。各ホストは、それぞれのプラットフォームでの名前に置き換えて指定します。
- `Autocapitalize`：`CapitalizeSentences`（デフォルト）、`CapitalizeNone`、`CapitalizeWords`、`CapitalizeCharacters`。`Autocorrect`：`AutocorrectDefault`、`AutocorrectYes`、`AutocorrectNo`。
- `OnFocus` と `OnBlur` は、フィールドがキーボードのフォーカスを得たとき、失ったときに呼ばれます。`ui.Focus(field)` でフォーカスを要求し、`ui.Blur(field)` でキーボードを閉じます。

デスクトップには切り替えるオンスクリーンキーボードがないため、`Keyboard` と `ReturnKey` はほとんど見た目に表れません。それでも Return キーを押せば `OnSubmit` が呼ばれます。macOS では、`ContentType` はフィールドの AutoFill のコンテンツタイプになります。Windows では、`KeyboardNumber` を指定すると数字しか入力できなくなります。Linux では、`Keyboard`、`ContentType`、`Autocapitalize`、`Autocorrect` が、フィールドに何を入力するかをインプットメソッドに伝えます（GTK の input purpose と input hints）。ブラウザーでは、これらは入力要素の `inputmode`、`enterkeyhint`、`autocomplete`、`autocapitalize`、`spellcheck` になり、スマートフォンのキーボードはそれに従います。

**複数行のテキスト。** `Multiline` を指定すると、メッセージやコメントを入力するテキストエリアになります。リターンキーは `OnSubmit` を呼ぶ代わりに改行します。高さはテキストに合わせて `Style.Lines` 行（0 なら制限なし）まで伸び、それを超えるとスクロールします。

```go
message := signal.New("")
return ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd},
	ui.TextField(ui.TextFieldProps{Value: message, Placeholder: ui.Val("Message"), Multiline: true,
		Style: ui.Style{Grow: 1, Lines: 5}}),
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled,
		Disabled: signal.Func(func() bool { return message.Get() == "" }),
		OnPress:  func() { message.Set("") }}),
)
```

**テキストだけのフィールド。** `Plain` を指定すると、フィールドの枠線、背景、内側の余白がなくなり、テキストとプレースホルダーだけが残ります。「メモ」や「リマインダー」のように、カードの中や独自のフォームに置くフィールドに使います。

```go
ui.TextField(ui.TextFieldProps{Value: note, Placeholder: ui.Val("Note"), Multiline: true, Plain: true})
```

`SafeArea` にはキーボードの分の余白が加わるので、その下端に置いたフィールドはキーボードに隠れません。

## コントロール

各コントロールは、値を変える対象のシグナルと、`Disabled` のアクセサを受け取ります。`Picker` 以外はラベルを持たないので、横に `Text` を置いてください。

```go
return ui.Column(ui.Style{Gap: 16},
	ui.Row(ui.Style{Align: ui.AlignCenter},
		ui.StaticText("Wi-Fi", ui.Style{Grow: 1}),
		ui.Toggle(ui.ToggleProps{Value: wifi})),
	ui.Slider(ui.SliderProps{Value: volume}),
	ui.Segmented(ui.SegmentedProps{Segments: ui.Val([]string{"Day", "Week", "Month"}), Selected: period}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%.0f cups", cups.Get()) }), ui.Style{Grow: 1}),
		ui.Stepper(ui.StepperProps{Value: cups, Min: 1, Max: 8})),
	ui.Wheel(ui.WheelProps{Items: ui.Val(sizes), Selected: size}),
	ui.Picker(ui.PickerProps{Items: ui.Val(milks), Selected: milk, Label: ui.Val("Milk")}),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Tint", ui.Style{Grow: 1}),
		ui.ColorPicker(ui.ColorPickerProps{Value: tint, Alpha: true, Title: ui.Val("Tint")})),
	ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
		ui.StaticText("Starts", ui.Style{Grow: 1}),
		ui.DatePicker(ui.DatePickerProps{Value: when, Mode: ui.DateAndTime, Min: time.Now()})),
)
```

- **Toggle** はスイッチで、`*signal.Signal[bool]` を受け取ります。`Checkbox` を指定するとチェックボックスになります。Android では `MaterialCheckBox`、Mac と Windows ではそれぞれのチェックボックス、Linux では `GtkCheckButton`、ブラウザーではチェックボックスです。チェックボックスのない iOS では、「リマインダー」と同じように、チェックマークで塗りつぶされる円になります。
- **Slider** は、`Min` から `Max` までの `float64` を選びます。どちらも 0 なら範囲は 0 から 1 です。
- **Segmented** は、横に並んだ少数の選択肢から一つを選ぶコントロールで、`Selected` は選ばれた項目のインデックスです。UIKit のセグメンテッドコントロールや、Material のセグメントボタンを使います。Windows には該当するものがないため、ホストが Windows Community Toolkit の `Segmented` に倣って描画します。
- **Stepper** は、マイナスとプラスで `float64` を `Step`（デフォルトは 1）ずつ増減させます。範囲は `Min` から `Max` まで（どちらも 0 なら 0 から 100）です。iOS では `UIStepper`、macOS では `NSStepper` で、それ以外では 2 つのボタンになります（Android では Material のアイコンボタン、Linux では GNOME のスピンボタンと同じように連結したボタン）。
- **Wheel** は、回転するホイールで多数の `Items` から一つを選びます。`UIPickerView` や Android の `NumberPicker` です。デスクトップと Web にはホイールがないため、項目を並べたポップアップメニューになります（`NSPopUpButton`、WinUI の `ComboBox` に倣って描画したドロップダウンのコンボボックス、`GtkDropDown`、`<select>`）。
- **Picker** は、メニューから多数の `Items` の一つを選び、選んだ項目を表示します（メニュースタイルの SwiftUI の `Picker`、Material の exposed dropdown menu）。`Selected` が範囲外（-1）のときは何も選ばれていない状態で、ピッカーは `Label` を表示します。`Label` は、スクリーンリーダーに伝える名前にもなります。Android では、このラベルはフィールドのラベルとして、選んだ項目の上に浮かんで表示されます。`Selected` のシグナルは nil でもかまいません。その場合、ユーザーの選択はピッカー自身が保持します。

  | | Picker |
  |---|---|
  | iOS | ティントカラーのプレーンな `UIButton` に、ポップアップのインジケーターを付けたもの。メニューでは選んだ項目にチェックが付きます（`changesSelectionAsPrimaryAction`） |
  | Android | ユーザーが文字を入力できない `MaterialAutoCompleteTextView` を、exposed dropdown menu スタイルの outlined な `TextInputLayout` で包んだもの。TalkBack はドロップダウンリストとして読み上げます |
  | macOS | `NSPopUpButton`（ラベルは非表示の項目で、何も選ばれていないあいだだけ表示されます） |
  | Windows | WinUI 3 の `ComboBox` に倣って描画した、ドロップダウンリスト形式のコンボボックス（コントロールの塗りと線、末尾のシェブロン。リストはフライアウトの面にオーナードローで描画し、選んだ項目の先頭にアクセントカラーのピルを付けます。Windows 11 では DWM が角を丸めます）。何も選ばれていないあいだはラベルを二次テキストの色で描画し、UI Automation にはラベルを名前として伝えます |
  | Linux | `GtkStringList` を使う `GtkDropDown`。選んだ項目が入る位置に、ラベルを淡色のラベル（dim label）として表示します |
  | Web | `<select>`。最初の option は、ラベルを表示する非表示のプレースホルダーです |
- **ColorPicker** は、タップするとプラットフォームのピッカーを開く色見本で、選んだ色を `ui.Color` として書き込みます。`Alpha` を指定するとユーザーが不透明度も選べるようになり、`Title` はピッカーの名前になります。iOS では `UIColorWell` とシステムのピッカーです。Android には該当するものがないため、パレットを並べた Material のダイアログになり、`Alpha` を指定すると不透明度のスライダーが付きます。macOS ではカラーパネルを開く `NSColorWell`、Linux では `GtkColorDialogButton`、ブラウザーでは `<input type="color">` です。Windows の色の設定ダイアログには不透明度がないため、Windows では `Alpha` は効果がありません。
- **DatePicker** は `time.Time` を選び、デバイスのタイムゾーンで表示します。`Mode` は `ui.DateOnly`（デフォルト）、`ui.TimeOnly`、`ui.DateAndTime` のいずれかで、`Min` と `Max` で範囲を制限できます。`Value` がゼロ値のときは現在時刻を表示します。

  | | コンパクト（デフォルト） | `Inline` |
  |---|---|---|
  | iOS | UIKit のコンパクトな日付ピッカー | カレンダーと時刻をその場に表示（専用のシートに置く場合向け） |
  | Android | 日付と時刻を表示し、Material のピッカーを開くボタン | 同じボタン |
  | macOS | `NSDatePicker` のテキストフィールドとステッパー | カレンダーと時計 |
  | Windows | カレンダーがドロップダウンで開く、日付と時刻の選択コントロール | 月のカレンダー。`DateAndTime` ではその下に時刻の選択コントロール（`TimeOnly` はコンパクトのまま） |
  | Linux | GTK のカレンダーと、時と分のスピンボタンをポップオーバーで開くボタン | カレンダーとスピンボタンをその場に表示 |
  | Web | `<input type="date">`、`"time"`、`"datetime-local"` | 同じ入力要素 |

## 進行状況とアクティビティ

`ui.Progress` はプログレスバーです。値は 0 から 1 で、どこまで進んだかわからないときは `nil` にします。スタイルの `Color` がバーの塗りの色になります。`ui.ProgressRing` は、同じものを円で表します。リングは上端から時計回りに塗られ、値が変わるとアニメーションします。`nil` のときは、プラットフォームの不確定（indeterminate）の円形インジケーターを表示します。`ui.Activity` はスピナーです。

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Progress(done),
	ui.Progress(nil),
	ui.ProgressRing(done),
	ui.ProgressRing(done, ui.Style{Width: ui.Pt(24), Color: colors.Green}),
	ui.ProgressRing(nil),
	ui.Activity(),
)
```

リングの大きさはレイアウト時に測定され、プラットフォームごとに固有のサイズがあります（iOS は 58 ポイント、Android は 48dp、Mac と Windows は 32、Linux と Web は 32）。スタイルで一辺を指定するともう一辺も同じ値になり、リングは常に正方形を保ちます。スクリーンリーダーは、パーセンテージ付きの進行状況インジケーターとして読み上げます。

| | リング | 値がない場合 |
|---|---|---|
| iOS | UIKit には確定的な（determinate）リングがなく、SwiftUI の円形の `ProgressView` も iOS では値を無視します。そのため、リングは SwiftUI の円形の容量 `Gauge` と同じように描画します。ティントカラーを 30% にしたトラックの上に、ティントカラーで端の丸い線を引きます。線の太さはリングのサイズの 10 分の 1 です | `UIActivityIndicatorView` |
| Android | Material の `CircularProgressIndicator`。フレームに合った Material 3 のサイズ（extra small、small、medium と、それぞれのトラック、インセット、ギャップ）を使います | 同じもの（不確定） |
| macOS | `Color` を指定せず、AppKit 自身のサイズである 16 ポイントか 32 ポイントのときは、`NSProgressIndicator` の確定的な円です。AppKit はこの円をこの 2 つのサイズでしか、しかもアクセントカラーでしか描かないため、それ以外のサイズや `Color` を指定したときは、AppKit が描くものをフレームいっぱいに描画します。16〜32 ポイントでは 5 ポイントの端の丸い線を引き（32 ポイントより大きければ 32 ポイントの円の比率、16 ポイントより小さければ 16 ポイントの円の比率）、AppKit のトラックに重ねます。色は `Color`、または AppKit 自身のインジケーターから読み取った AppKit の描画色（アクセントカラー、非アクティブなウィンドウではグレー、ライトとダーク）です。値は AppKit のアニメーターと同じく 0.25 秒かけて動きます。ただし「視差効果を減らす」がオンのときは動きません。どちらの場合も、VoiceOver は AppKit のインジケーターとして読み上げます | 16 ポイントと 32 ポイントでは AppKit のスピナー。それ以外のサイズでは、スピナーの 8 本のスポークをそのサイズで描画し（AppKit のスピナーは拡大縮小するとぼやけ、色も指定できないため）、先頭のスポークを `Color` で描きます |
| Windows | Fluent の `ProgressRing`（WinUI のもの）で、トラックのない円弧です。ナレーターはプログレスバーとして読み上げます | `Activity` と同じく、回転する円弧 |
| Linux | `AdwSpinner` が円を描くのと同じように描画します。テキストの色を 15% にしたトラックと、アクセントカラーの円弧です | `AdwSpinner`（libadwaita 1.6 より前は `GtkSpinner`） |
| Web | SVG のリング。`aria-valuenow` を持つ `role="progressbar"` です | 回転する円弧。`prefers-reduced-motion` では回転が遅くなります |

## 画像

`ui.Image` は画像を表示します。読み込みとデコードはプラットフォームがメインスレッドの外で行い、メモリにキャッシュします。iOS と Android では、http(s) で取得した画像をディスクにも保存し、アプリを起動し直しても使えるようにします。デスクトップのホストはメモリにだけ保持し、ブラウザーは自身のキャッシュに保持します。Windows でデコードできるのは PNG、JPEG、GIF で、Linux ではそれに加えて GDK のローダーが読める形式です。

```go
return ui.Column(ui.Style{Gap: 12},
	ui.Image(ui.ImageProps{Source: ui.Val("logo.png"), Style: ui.Style{Width: ui.Pt(64)}}),
	ui.Image(ui.ImageProps{
		Source:      ui.Val("https://picsum.photos/id/1015/600/400"),
		Placeholder: "placeholder.png",
		Mode:        ui.ImageFill,
		Label:       ui.Val("A river in a valley"),
		Style:       ui.Style{AspectRatio: 3.0 / 2, CornerRadius: 12, Clip: true},
	}),
)
```

- `Source` は、http(s) の URL、アプリの `assets` ディレクトリ内のパス（`"logo.png"` は `assets/logo.png`）、または絶対パス（アプリが書き出したファイルや、ユーザーが選んだ写真）です。
- `Mode` は、画像を枠にどう収めるかを決めます。`ImageFit`（デフォルト。全体を収め、余白ができる）、`ImageFill`（枠を覆い、はみ出た部分を切り取る）、`ImageStretch`（枠の縦横比に合わせて引き伸ばす）、`ImageCenter`（本来のサイズで中央に置く）があります。
- `Placeholder` は、`Source` が読み込まれるまでと、読み込みに失敗したときに表示されます。すぐに表示できるアセットを使ってください。プレースホルダーを表示する場所を確保するため、画像にはサイズを指定してください。
- `Label` は、スクリーンリーダーに伝える画像の説明です。指定しなければ、画像は装飾として扱われます。
- `OnLoad` は読み込みが終わると画像本来のサイズを受け取り、`OnError` は読み込めなかったときにメッセージを受け取ります。

サイズを指定していない画像は、読み込みが終わるまで空です。読み込み後は、使える幅を超えない範囲で、1 ピクセルを 1 ポイントとした本来のサイズになります。一辺だけを指定した場合（`Width`、または行の中での `Grow`）は縦横比を保ちます。`AspectRatio` を指定すると、読み込む前から縦横比を決めておけます。

## シンボルとアイコン

`ui.Symbol` はシンボルを表示します。iOS と macOS では SF Symbol、Android と Web では対応する Material Symbol、Windows では対応する Segoe Fluent Icons のグリフ（Windows 11 より前は Segoe MDL2 Assets）、Linux ではアイコンテーマ（Adwaita）の対応するシンボリックアイコンで、なければ Material Symbol になります。大きさはスタイルの `Font` のテキストとして決まるので文字サイズに追従し、色は `Color`（デフォルトはラベルの色）になります。

```go
return ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
	ui.Symbol(ui.IconBell),
	ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: colors.Yellow, Font: fonts.Title}),
	ui.Symbol(ui.Icon("archivebox"), ui.Style{Color: colors.SecondaryLabel}),
)
```

`ui.Icon` は SF Symbol の名前です。ボタン、メニュー項目、バーボタン、タブもアイコンを受け取ります。よく使うものは `Icon` 定数として用意されています。`IconHome`、`IconSearch`、`IconSettings`、`IconPlus`、`IconClose`、`IconTrash`、`IconShare`、`IconMore` のほか、50 個ほどあります。`icon.Filled()` は塗りつぶした形（`"star.fill"`）を返します。

iOS と macOS は、どの SF Symbol でも描画できます。それ以外のプラットフォームが描画できるのは、厳選したセットだけです。このセットには定数とそれ以外のシンボルが含まれ、[`platform/android/gen_symbols.go`](../../platform/android/gen_symbols.go) で Material Symbols に対応付けられています（Web と Linux の対応はこのファイルから生成されます）。Windows では Segoe Fluent Icons のグリフに対応付けられています。セットにない名前は、これらのプラットフォームでは何も表示されません（ホストは警告をログに出します）。そのため、`"archivebox"` のように文字列で書いた名前は、配布するすべてのプラットフォームで確認してください。Linux では、`-symbolic` で終わる名前はアイコンテーマの名前として、そのまま使われます。

### SVG アイコン

独自のアイコンには、パッケージ `asuka/sdk/svg` を使います。SVG をプラットフォームのベクター描画で描くので、ラスター化は一切行いません。`svg.MustParse` がマークアップを読み込み（`svg.Parse` は代わりにエラーを返します）、`svg.View` がそれを描画します。

```go
var download = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>`)

func downloadIcon() ui.Node {
	return svg.View(svg.Props{Icon: download, Color: ui.Val(colors.Accent), Label: ui.Val("Download"),
		Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
}
```

`Color` は、SVG が `currentColor` で塗る部分と、SVG が塗りを指定していない部分の色になります。デフォルトは、テキストの色である `colors.Label` です。`Style` で幅も高さも指定しなければ、ビューは SVG 本来のサイズになります。一方だけを指定すると、もう一方は縦横比を保って決まります。アイコンは `//go:embed` でファイルとして埋め込み、パッケージ変数で一度だけ解析してください。

アイコンセット（Lucide、Material、Heroicons、Feather）が使う要素に対応しています。パス、基本図形、グループ、`use`、塗り、線、変形です。テキスト、グラデーション（アイコンの色で塗られます）、クリッピング、マスク、フィルター、`fill-rule="evenodd"` は無視されます。

## 動画

`ui.Video` は、プラットフォームのプレイヤーとそのコントロールで動画を再生します。iOS では `AVPlayerViewController`、Android では Media3 の ExoPlayer、macOS では `AVPlayerView`、Linux では `GtkVideo`（GStreamer）、ブラウザーでは `<video>` です。Win32 には動画のビューがないため、Windows では WebView2 の中の `<video>` になり、Edge のコントロールが表示されます。再生は `ui.Play`、`ui.Pause`、`ui.Seek` で操作します。

```go
var v ui.Node
v = ui.Video(ui.VideoProps{
	Source:   ui.Val("https://example.com/clip.mp4"),
	AutoPlay: true, Muted: true, Loop: true,
	Style: ui.Style{CornerRadius: 12},
})
return ui.Column(ui.Style{Gap: 12}, v,
	ui.Button(ui.ButtonProps{Title: ui.Val("Start over"), OnPress: func() { ui.Seek(v, 0); ui.Play(v) }}),
	ui.Button(ui.ButtonProps{Title: ui.Val("Pause"), OnPress: func() { ui.Pause(v) }}),
)
```

- `Source` は、http(s) の URL（ファイルや HLS ストリーム）か、アプリのアセット内のファイルです。
- `Mode` は `ui.ImageFit`（余白を付けて全体を収める。デフォルト）か `ui.ImageFill` です。
- `HideControls` は、プラットフォームのコントロール（再生、タイムライン、フルスクリーン、iOS では AirPlay とピクチャ・イン・ピクチャ）を隠します。
- `OnEnd` は最後まで再生するたびに呼ばれ、`OnError` は再生できないときに呼ばれます。

サイズの決まり方は `Image` と同じです。スタイルでサイズを指定しない限り、動画のサイズがわかるまでは空で、その後は動画の縦横比を保ちます。`Seek` には先頭からの秒数を渡します。

## Web ページ

`ui.WebView` は、プラットフォームの Web ビューで Web ページを表示します。スクロール、ズーム、テキストの選択もプラットフォームのものです。iOS と macOS では `WKWebView`、Android では `WebView`、Windows では WebView2（Edge のランタイム）、Linux では WebKitGTK 6.0 の `WebKitWebView` です。Linux で WebKitGTK がインストールされていない場合は何も表示せず、何をインストールすればよいかを伝えるメッセージで `OnError` を一度だけ呼びます。ブラウザーでは `<iframe>` になり、以下で説明する機能をすべて使えるのはアプリ自身のサイトのページに限られます。詳しくは [Web](web.md#web-ビュー) を参照してください。高さも `Grow` も指定しなければ、与えられた領域いっぱいに広がります。

```go
state := signal.New(ui.WebState{})
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	URL:       ui.Val("https://go.dev"),
	Block:     []string{"doubleclick.net", "/ads/"},
	OnState:   func(s ui.WebState) { state.Set(s) },
	OnMessage: func(m string) { fmt.Println("the page says", m) },
	OnNewWindow: func(url string) {
		ui.OpenURL(url)
	},
})
return ui.Column(ui.Style{Grow: 1},
	ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 12), Align: ui.AlignCenter},
		ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: ui.Val("Back"),
			Disabled: signal.Func(func() bool { return !state.Get().CanGoBack }),
			OnPress:  func() { ui.WebBack(web) }}),
		ui.Text(signal.Func(func() string { return state.Get().Title }), ui.Style{Grow: 1, Lines: 1}),
		ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Reload"), OnPress: func() { ui.WebReload(web) }}),
	),
	ui.Show(signal.Func(func() bool { return state.Get().Loading }), func() ui.Node {
		return ui.Progress(signal.Func(func() float64 { return float64(state.Get().Progress) }))
	}),
	web,
)
```

表示するものを指定するプロパティは次のとおりです。

- `URL` は、http(s) の URL か、アプリのアセット内のファイルです（ページ内の相対リンクもアセットを指します）。代わりに、表示するマークアップを `HTML` で渡すこともできます。
- `UserAgent` は、ページの取得に使うユーザーエージェントで、`navigator.userAgent` の値にもなります。空ならプラットフォームのものを使います。
- `Preload` は、各ページ自身のスクリプトより前に、すべてのフレームで実行するスクリプトです。`fetch` をフックしたり、グローバル変数を設定したりするのに使います。
- `Block` は、広告ブロッカーのように特定のリクエストを読み込ませません。各項目はホスト名（`"doubleclick.net"`。サブドメインにも一致します）か、`/` を含む場合は URL の一部（`"/ads/"`）です。

`Preload`、`UserAgent`、`Block` は、ビューを作るときに設定されます。

ページの様子を知らせるコールバックは次のとおりです。

- `OnLoad` は読み込みが完了した各ページの URL を受け取り、`OnError` は読み込めなかったときにメッセージを受け取ります。
- `OnState` は、アドレスバーのようにページの状態を追います。`URL`、`Title`、`Loading`、`Progress`（0 から 1）、`CanGoBack`、`CanGoForward` を持つ `ui.WebState` を受け取ります。
- `OnNewWindow` は、ページが新しいウィンドウで開こうとした URL（`window.open` や `target="_blank"` のリンク）を受け取ります。指定しなければ、そのページはこのビューの中で読み込まれます。
- `OnDownload` は、ページが表示できないレスポンス、つまり保存すべきファイル（`.zip` へのリンクや `Content-Disposition: attachment`）を、`URL`、`MIME`、`Filename`、`Size`（不明なら -1）を持つ `ui.WebDownload` として受け取ります。ダウンロード自体は行いません。
- `OnRequest` は、ページが送る各リクエスト（スクリプト、画像、メディア、`fetch`）の URL を受け取ります。**iOS と macOS では使えません**。`WKWebView` がアプリにリクエストを見せないためです。

`ui.WebBack`、`ui.WebForward`、`ui.WebReload`、`ui.WebStop` で、履歴を戻る・進む、ページを再読み込みする、読み込みを止める、といった操作ができます。

### ページとのやり取り

ページは `window.asuka.postMessage(text)` で Go にメッセージを送り、それによって `OnMessage` が呼ばれます。Go からページでスクリプトを実行するには、何も返さない `ui.EvalJS` か、値を JSON にしてコールバックに渡す `ui.EvalJSResult` を使います。渡される値は、最後の文の値、返した Promise が解決した値（`undefined` なら `null`）、または投げられたエラーです。

```go
var web ui.Node
web = ui.WebView(ui.WebViewProps{
	HTML: ui.Val(`<button onclick="asuka.postMessage('tapped')">Tap</button>`),
	OnMessage: func(m string) {
		ui.EvalJSResult(web, "document.title", func(v json.RawMessage, err error) {
			fmt.Println(m, string(v), err)
		})
	},
})
```

`ui.WebCookies(url, done)` は、Web ビューが持っているその URL の Cookie を、`Cookie` ヘッダーの形で取得します。ユーザーがページでサインインした状態を、アプリ自身のリクエストで使うためのものです。

[`examples/downloader`](../../examples/downloader) は、これらをすべて使った例です。アドレスバー、プリロードスクリプト、ブロック、ダウンロードを備えたブラウザーになっています。

## 図形

`ui.Shape` は輪郭を描き、塗りつぶし、線、またはその両方で描画します。`Path` は図形のサイズに合わせて輪郭を作る関数で、サイズが変わったときと、関数が読むシグナルが変わったときに再実行されます。図形のサイズはスタイルで決まります（幅と高さ、縦横比、`Grow`）。何も指定しなければ空になります。

```go
ui.Shape(ui.ShapeProps{
	Path: func(w, h float32) *ui.Path {
		return new(ui.Path).MoveTo(w/2, 0).LineTo(w, h).LineTo(0, h).Close()
	},
	Fill:  colors.Orange,
	Style: ui.Style{Width: ui.Pt(48), Height: ui.Pt(48)},
})
```

`ui.Path` の座標は、図形の左上を原点とするポイント単位です。メソッドはパスに要素を追加してパス自身を返すので、つなげて書けます。`MoveTo`、`LineTo`、`QuadTo`、`CubicTo`、`Arc`（角度はラジアンで、x 軸から時計回り）、`Close` と、図形をまるごと追加する `Rect`、`RoundedRect`、`Ellipse` があります。

`Stroke` と `LineWidth`（デフォルトは 1）で輪郭に線を引きます。線端は `LineCap`（`CapButt`、`CapRound`、`CapSquare`）で、角は `LineJoin`（`JoinMiter`、`JoinRound`、`JoinBevel`）で指定します。`TrimStart` と `TrimEnd` は 0 から 1 の値で、線の一部だけを描きます。プログレスリングや、自分自身を描いていく線に使えます。これらを `ui.Animate` の中で変更するとアニメーションします（[モーションとジェスチャー](motion.md)を参照）。`Dash` は線を破線にします。描く長さと飛ばす長さをポイント単位で交互に並べたもので（`[]float32{6, 4}`。個数が奇数なら繰り返されます）、SwiftUI の `StrokeStyle(dash:)` と同じです。`DashPhase` は、パターンのどこから描き始めるかです。[`examples/usage`](../../examples/usage) は、これを使って予測の線を描いています。

```go
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

func progressRing(done signal.Accessor[float32]) ui.Node {
	box := ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pct(100), Height: ui.Pct(100)}
	return ui.View(ui.ViewProps{Label: ui.Val("Progress"), Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.SecondaryBackground, LineWidth: 8, Style: box}),
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.Accent, LineWidth: 8, LineCap: ui.CapRound,
			TrimEnd: done, Style: box}),
	)
}
```

### Canvas

`ui.Canvas` には、アプリが自分で描くものを描画します。グラフ、メーター、手書きのサイン、ゲームの盤面など、既存のビューでは表せないものです。描画関数は `*ui.Graphics` を使い、キャンバスの左上を原点とするポイント単位で描きます。

```go
ui.Canvas(func(g *ui.Graphics) {
	w, h := g.Size()
	g.Fill(g.Path().RoundedRect(0, 0, w, h, 12), colors.SecondaryBackground)
	g.Fill(g.Path().RoundedRect(0, 0, w*level.Get(), h, 12), colors.Accent)
	g.Text(fmt.Sprintf("%.0f%%", level.Get()*100), w/2, h/2+6, ui.TextStyle{Weight: fonts.Semibold, Align: ui.TextCenter})
}, ui.Style{Height: ui.Pt(36)})
```

- `g.Path()` は組み立て用のパス（Shape のパスと同じもの）で、描画のたびに再利用されます。
- `Fill`、`Stroke`（`StrokeStyle` で太さ、線端、角、破線を指定）、`FillGradient` でパスを塗ります。`Text` は 1 行のテキストを描き、ベースラインを y に、左端、中央、右端のいずれかを x に合わせます。
- `Translate`、`Scale`、`Rotate`（度単位、時計回り）、`Clip`、`Opacity` は、`Save` と `Restore` で囲んで使います。
- 色にはセマンティックカラー（`colors.Label`、`colors.Separator`、`colors.Accent`）を使えます。これらはダークモードに追従します。

キャンバスは、レイアウトが決まると描画され、サイズが変わったときや関数が読んだシグナルが変わったときに、エフェクトと同じように再描画されます。描画のたびにディスプレイリストが記録され、前回と違うときだけ送られて、プラットフォームがそれを再生します。iOS と macOS では Core Graphics、Android では `Canvas`、Windows では GDI+、Linux では GTK のスナップショット（GSK）、ブラウザーでは 2D の `<canvas>` です。以前に同じ量を描いたことがあれば、記録の際にメモリを割り当てることはありません。キャンバス自体はタップを受け取らないので、`OnPress` や `OnPan` を持つ `ui.View` で包んでください。これらはキャンバスの座標系でポイントを報告します。スクリーンリーダーはキャンバスを読み上げないので、包んでいるビューに `Label` を付けてください。

### グラフ

`asuka/sdk/chart` は、Canvas の上にグラフを描きます。値の軸に沿った棒、折れ線、面、点を描き、ラベルにはプラットフォームのテキストを使います（プラットフォームが測定し、ユーザーの文字サイズに合わせて拡大縮小されます）。

```go
chart.View(chart.Props{
	Series: ui.Val([]chart.Series{
		{Name: "Steps", Kind: chart.Bar, Values: steps},
		{Name: "Goal", Kind: chart.Line, Values: goal, Color: colors.Hex(0xff9500)},
	}),
	Labels: ui.Val([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}),
	Label:  ui.Val("Steps this week"),
	Style:  ui.Style{Height: ui.Pt(200)},
})
```

- 系列はラベルごとに値を一つ持ち、`NaN` のところは途切れます。棒の系列は横に並び、面、折れ線、点はその上に重なります。`Smooth` は単調な曲線を描くので、曲線が値を行き過ぎることはありません。
- 値の軸は、`Min` と `Max` で固定しない限り値に合わせて決まり、切りのよい目盛りまで外側に広げられます（棒と面では 0 まで下げます）。軸のラベルは `Format` で書式を指定します（デフォルトは `2.5k` の形式）。
- 色はデフォルトで、まずアプリのアクセントカラー、続いてシステムの色が使われます。
- スクリーンリーダーは、グラフを 1 枚の画像として読み上げます。まず `Label` を、続いて各系列の値をすべて読み上げます。

### シェーダー

`ui.Shader` は、GPU のプログラムが子ビューの下に描くボックスです。流れるグラデーション、ノイズ、光、波紋などを描けます。プログラムは Kage シェーダー（Ebitengine の言語で、文法は Go と同じ）で書き、パッケージ `shader` が、パッケージ変数として一度だけコンパイルします。

```go
var glow = shader.MustCompile(`package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	d := distance(pos/rectSize(), vec2(0.5))
	g := smoothstep(0.5, 0, d) * (0.8 + 0.2*sin(Time*2))
	return vec4(Tint.rgb*g, 1) * Tint.a
}`)

ui.Shader(ui.ShaderProps{
	Program:  glow,
	Uniforms: ui.Val([]ui.Uniform{ui.ColorUniform("Tint", colors.Hex(0x38bdf8))}),
	Animated: true,
	Style:    ui.Style{Height: ui.Pt(200), CornerRadius: 16, Clip: true, Background: colors.Hex(0x0b1030)},
})
```

- `Fragment` は、1 ピクセルの色を、乗算済みの赤、緑、青、アルファ（それぞれ 0 から 1）で返します。`pos` はピクセルの中心を、ボックスの左上を原点とするポイント単位で表したもので、`dst` は同じものをピクセル単位で表したものです。`rectSize()` はボックスのサイズ（ポイント単位）、`pixelScale()` は 1 ポイントあたりのピクセル数です。
- 言語には、`bool`、`int`、`float`、`vec2`〜`vec4`、`ivec2`〜`ivec4`、`mat2`〜`mat4` とそれらの配列、GLSL の組み込み関数（`sin`、`mix`、`smoothstep`、`dot` など）、独自の関数、`if` と `for` があります。エクスポートした変数が uniform になります。
- `Uniforms` は、名前を指定して uniform を設定します。スカラーなら数値を 1 つ、ベクトルなら成分を、行列なら列を順に並べます。値を変えるとボックスが再描画されます。指定しなかった uniform は 0 です。
- `var Time float` は、シェーダーが表示されてからの秒数です。`Animated` を指定するとシェーダーが動き続け、ディスプレイが描画するフレームごとにボックスが描画されます。そのあいだ、Go の側では何も実行されません。
- `shader.Compile` は、シェーダーのエラーを行番号付きで報告します。Android で実行できない部分も、どのプラットフォームでも同じように報告します。AGSL は GLSL ES 1.00 の言語なので、符号なし整数、ビット演算、整数の剰余（浮動小数点数の `mod` は使えます）、導関数、定数でない値のあいだのループは使えません。

シェーダーは、iOS と macOS では Metal（`CAMetalLayer`）、Android では AGSL（`RuntimeShader`）、ブラウザーでは WebGL 2（プログラムを GLSL ES 3.00 に変換）、Windows では Direct3D 11（HLSL に変換。GPU がない環境では、システムのソフトウェアラスタライザーである WARP を使用）、Linux では OpenGL（GLSL に変換し、GTK のコンテキストでテクスチャに描いたものを GTK が描画）で動きます。Windows では、フレームをメインスレッドの外でレンダリングし、準備ができたところで表示します。GL を使えない Linux（ドライバーがない、`GDK_DISABLE=gl` を指定しているなど）では、代わりに CPU でプログラムを実行します。複数のコアを使い、メインスレッドの外で実行しますが、大きなボックスやアニメーションするボックスは、解像度を落としてレンダリングしてから拡大します。最初のフレームが出るまでの代わりとして、ボックスには `Background` を指定してください。

## コンテナー

`ui.View` は、ほかのビューを入れるコンテナーです。`ui.Column` と `ui.Row` は、子を上から下へ、または先頭から末尾へ並べるビューです。`ui.SafeArea` は、ノッチ、システムバー、キーボードの分の余白を持つ列です。中身の大きさと配置の決まり方は、[レイアウトとスタイル](layout.md)で説明しています。

見た目、イベント、アクセシビリティのプロパティを何も持たないビューは、自分のネイティブビューを作りません。その子は、ネイティブビューを持つ最も近い祖先に直接入ります。そのため、行や列をいくら入れ子にしてもコストはかかりません。

```go
open := signal.New(false)
return ui.View(ui.ViewProps{
	OnPress:     func() { open.Set(!open.Peek()) },
	OnLongPress: func() { ui.Haptic(ui.HapticMedium) },
	Label:       ui.Val("Ada, 2 unread"),
	Style:       ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12},
},
	ui.Symbol(ui.IconPerson.Filled()),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada", ui.Style{Font: fonts.Headline}),
		ui.StaticText("2 unread", ui.Style{Color: colors.SecondaryLabel}),
	),
)
```

`ViewProps` には次のものがあります。

- `OnPress` と `OnLongPress`。
- プラットフォームのジェスチャー認識を使うジェスチャー：`OnPan`、`OnPinch`、`OnRotate`（それぞれ `ui.Gesture` を受け取ります）と `OnSwipe`（`ui.SwipeDirection` を受け取ります）。[モーションとジェスチャー](motion.md)を参照してください。
- インタラクションの状態。ビュー自身のスタイルは、バリアント（`Style.Pressed`、`Hovered`、`Dragging`、`DragOver`）を通じて状態に追従します。ほかのビューは `Interaction`（`ui.Interaction`。`Style.When` や `StyleFunc` の中で読みます）を通じて追従します。副作用は `OnPressedChange` と `OnHoverChange` に書きます。ドラッグ＆ドロップには `Drag`（`ui.DragSource`）と `Drop`（`ui.DropTarget`）を使います。[モーションとジェスチャー](motion.md#押下とホバーの状態)を参照してください。
- `OnLayout`。レイアウトが決まったときと、その後フレームが変わるたびに、ビューの `ui.Frame` を引数に呼ばれます。
- アクセシビリティ：`Label`、`Hint`、`Role`、`Decorative`、`Actions`、`OnAdjust`。[アクセシビリティ](accessibility.md)を参照してください。

### Scroll

`ui.Scroll` は、子を一方向にスクロールさせます。スクロールする範囲ができるよう、サイズか `Grow` を指定してください。`Style` はスクロールビュー自体のボックスで、`ContentStyle` は子を並べるボックスです。

```go
down := signal.New(false)
var scroll ui.Node
scroll = ui.Scroll(ui.ScrollProps{
	Style:    ui.Style{Grow: 1},
	OnScroll: func(_, y float32) { down.Set(y > 300) },
}, ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16}, content...))
return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, scroll,
	ui.Show(down, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Right: ui.Pt(20), Bottom: ui.Pt(20)}},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Back to top"), Variant: ui.ButtonFilled,
				OnPress: func() { ui.ScrollTo(scroll, 0, 0, true) }}))
	}))
```

- `OnScroll` は、スクロール中にコンテンツのオフセット（ポイント単位）を引数に呼ばれます。
- `ui.ScrollTo(scroll, x, y, animated)` は、指定したオフセットまでスクロールします（末尾を超える値なら末尾まで）。`ui.ScrollToEnd(scroll, animated)` は一番下まで、横方向のスクロールなら末尾の端までスクロールします。どちらも `List` でも使えます。
- `Horizontal` を指定すると、横方向にスクロールします。

```go
ui.Scroll(ui.ScrollProps{Horizontal: true, ContentStyle: ui.Style{Gap: 8, Padding: ui.Symmetric(0, 16)}}, kids...)
```

**ページ。** `Paging` を指定すると、ページの切れ目でだけ止まるようになります。カルーセル、オンボーディングの各ステップ、全画面動画のフィードなどに使います。コンテンツの子がそれぞれ 1 ページになり、スクロール方向の大きさはスクロールビューと同じです。`Page` は表示中のページで、スワイプが止まるときに書き込まれ、コードから書き込むとそのページまでスクロールします。`ui.PageControl` は、ページごとに 1 つのドットで現在位置を示し、ドットがタップされるとそのページに移動します。

```go
page := signal.New(0)
return ui.Column(ui.Style{Gap: 8},
	ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(200)}},
		card(1), card(2), card(3)),
	ui.PageControl(ui.PageControlProps{Count: ui.Val(3), Page: page}))
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| ページング | `isPagingEnabled` | スワイプのたびに次のページ、または元のページにスナップ | スワイプが終わると最寄りのページで止まる | ホイールの 1 ノッチごとに 1 ページ（FlipView と同じ） | ホイールの 1 ノッチごとに 1 ページ。タッチパッドやタッチスクリーンのスワイプはページで止まり、矢印キー、Page Up、Page Down で 1 ページずつ移動 | CSS のスクロールスナップ |
| `PageControl` | `UIPageControl` | テーマのプライマリーカラーのドット（Material にはない） | アクセントカラーのドット | ピップ（PipsPager と同じ） | libadwaita に倣って描画したドット。現在のページのものは大きく濃い | ドット。現在のページのものはラベルの色 |

ページは、読む方向の始まりから数えます。右から左のレイアウトでは、最初のページは一番右です。画面の回転などでスクロールビューのサイズが変わっても、同じページにとどまります。スクリーンリーダーはドットを「3 ページ中 2 ページ目」のように読み上げ、ドットを調整してページを移動できます。

**引っ張って更新。** `OnRefresh` を指定すると有効になります（`UIRefreshControl`、`SwipeRefreshLayout`）。更新しているあいだは `Refreshing` を true にしてください。false になるとスピナーが消えます。`Refreshing` を指定しなければ、`OnRefresh` から戻った時点でスピナーが消えます。macOS では、トラックパッドや Magic Mouse のスクロールで、iOS と同じように上端を越えて引っ張れます。Windows と Linux ではデスクトップ流に更新します。F5 キー（Linux では Ctrl+R も）を押すと、フォーカスのあるスクロールビューを、なければ画面上で最も内側にあるスクロールビューを更新し、そのあいだ上端にスピナーを表示します。ブラウザーではタッチで引き下げます。

```go
refreshing := signal.New(false)
return ui.Scroll(ui.ScrollProps{
	Style:      ui.Style{Grow: 1},
	Refreshing: refreshing,
	OnRefresh: func() {
		refreshing.Set(true)
		go func() {
			time.Sleep(time.Second) // 取得する
			ui.Post(func() { refreshing.Set(false) })
		}()
	},
}, ui.StaticText("Pull down"))
```

**キーボードの上のバー。** `Accessory` は、フィールドにフォーカスがあるあいだキーボードの上に表示するバーです。文字数と「完了」ボタン、書式設定のボタン、クイック返信などに使います（SwiftUI のキーボードツールバーに相当します）。幅はウィンドウと同じで、高さは中身に合わせて決まります。バーはフィールドと一緒に作られ、フィールドと一緒に破棄されます。

```go
var field ui.Node
field = ui.TextField(ui.TextFieldProps{Value: note, Multiline: true, Accessory: func() ui.Node {
	return ui.Row(ui.Style{Padding: ui.Symmetric(6, 16), Align: ui.AlignCenter, Material: ui.MaterialBar},
		ui.Text(signal.Func(func() string { return fmt.Sprint(len(note.Get()), " characters") }), ui.Style{Grow: 1}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Done"), OnPress: func() { ui.Blur(field) }}))
}})
```

iOS では、フィールドの `inputAccessoryView` になります。キーボードと一緒に動き、ユーザーがスワイプでキーボードを閉じるときも追従します。Android にはこれに当たるものがないため、バーをフィールドのウィンドウ（シートのウィンドウも含む）の、キーボードの上端に置き、フレームごとにキーボードを追います。オンスクリーンキーボードのない macOS、Windows、Linux では表示されません。ブラウザーでは、タッチスクリーンでだけ表示されます。

**装飾付きテキスト。** `Rich` を指定すると、フィールドは装飾付きテキストのテキストエリアになります。メールの本文や書式付きのメッセージに使います。太字、斜体、下線、取り消し線、等幅のコード、リンクを扱えます（スパンが持つ色やテキストスタイルはそのまま保たれます）。値は、`RichText` が表示するのと同じ `ui.Rich` です。ユーザーの入力や書式設定はこれに書き込まれ、コードからこれに書き込むとテキストが置き換わります。`ui.ToggleFormat(field, ui.FormatBold)` は、選択範囲全体にその書式が付いていなければ付け、全体に付いていれば外します（`FormatText` を使うと、付けるか外すかを明示して設定できます）。選択範囲がなければ、次に入力する文字に適用されます。`ui.SetLink(field, url)` は選択範囲にリンクを付けます（"" ならリンクを外します。選択範囲がなくカーソルだけのときは、カーソルのあるリンクが対象です）。`OnFormats` は、カーソル位置の書式、または選択範囲全体に共通する書式を報告するので、ツールバーの表示に使えます。

```go
body := signal.NewFunc(ui.Rich{ui.Plain("Hi,\n\nThe "), ui.Strong("1.0"), ui.Plain(" build is ready.")}, nil)
formats := signal.New(ui.TextFormats{})
var field ui.Node
field = ui.TextField(ui.TextFieldProps{Rich: body, Plain: true, OnFormats: formats.Set, Style: ui.Style{Grow: 1}})
bold := ui.Button(ui.ButtonProps{Icon: "bold", Label: ui.Val("Bold"),
	Style:   ui.StyleFunc(func() ui.Style { if formats.Get().Has(ui.FormatBold) { return ui.Style{Background: colors.SecondaryBackground} }; return ui.Style{} }),
	OnPress: func() { ui.ToggleFormat(field, ui.FormatBold) }})
```

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| ビュー | `UITextView` の属性付きテキスト | `EditText` のスパン | リッチテキストの `NSTextView` | RTF を介したリッチエディット（`RICHEDIT50W`） | `GtkTextView` のバッファーのタグ | `contenteditable` な要素 |
| ユーザーによる書式設定 | 編集メニューの B/I/U | ハードウェアキーボードの Ctrl+B、Ctrl+I、Ctrl+U | 「フォーマット」メニュー、⌘B ⌘I ⌘U | Ctrl+B、Ctrl+I、Ctrl+U | Ctrl+B、Ctrl+I、Ctrl+U | ブラウザーの ⌘B ⌘I ⌘U（Mac 以外では Ctrl） |
| 書式設定の取り消し | 可能 | 可能。フィールド独自の履歴（Android 標準の履歴はテキストしか保持しない）、Ctrl+Z、Ctrl+Shift+Z、Ctrl+Y | 可能 | 可能 | GTK の履歴（Ctrl+Z、Ctrl+Shift+Z）。テキストしか保持しない | 可能。ページの編集コマンドによる |

[`examples/mail`](../../examples/mail) には、ツールバー付きのメール作成画面があります。

**パディング。** テキストやフィールドの `Padding`（と `BorderWidth`）は、CSS と同じく、フレームの内側でコンテンツの周りに取られます。背景はパディングの部分まで塗られ、テキストはその内側に描かれ、フィールドの編集もその内側で行われます。

## リスト

`ui.List` は、プラットフォームの再利用型のリスト（`UICollectionView`、`RecyclerView`）で、項目ごとに 1 行を表示します。デスクトップと Web には任意のビューを行として持てるリストがないため、ホストがプラットフォームのスクロールビューの中で、行を自前で再利用します。行は画面上のセルの分しか作られません。スクロールして画面外に出たセルは別の項目に再利用され、更新されるのは値が違うプロパティだけです。[`examples/list10k`](../../examples/list10k) には 1 万行があります。

```go
messages := signal.NewFunc(fetch(0, 30), nil)
return ui.List(ui.ListProps{}, messages, func(m message) int { return m.ID }, row)
```

```go
func row(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
	return ui.Column(ui.Style{Gap: 2, Padding: ui.Symmetric(12, 20)},
		ui.Text(signal.Func(func() string { return m.Get().From }), ui.Style{Font: fonts.Headline}),
		ui.Text(signal.Func(func() string { return m.Get().Subject }), ui.Style{Color: colors.SecondaryLabel}),
	)
}
```

リストは、項目、各項目のキー、そして項目とそのインデックス（どちらもアクセサ）から行を作る関数を受け取ります。キーは、リストが変わる前と後で、どの項目がどれに当たるかを示します。変わった項目は行が更新され、移動した項目は行も移動し、その差分をプラットフォームがアニメーションで見せます。上の例のように、項目は `signal.Func` の中で読んでください。そうすれば、再利用された行が新しい項目に追従します。

リストは与えられた領域を埋めます。高さも `Grow` も指定しなければ、広がって領域を埋めます。行のあいだには、プラットフォームの区切り線を描きます。

`ListProps` には次のものがあります。

- `SwipeActions`：行 `i` のアクションで、行をスワイプすると現れます。iOS では行の末尾側のアクションになり、破壊的なアクションは赤で表示され、最後までスワイプすると最初のアクションが実行されます。Android では、Material のスワイプして閉じる操作と同じように、最後までスワイプすると、最初のアクションの色とアイコンを背景にそのアクションが実行されます。macOS、Windows、Linux、Web では行のコンテキストメニュー（副ボタンのクリック。Linux と Web では長押しも）になり、先頭側、末尾側の順に並びます。スクリーンリーダー向けに、同じものを行のアクセシビリティの `Actions` にも指定してください。
- `OnEndReached`：最後の数行が表示されそうになると、リストの長さごとに一度だけ呼ばれます。ここで続きを読み込みます。
- `OnRefresh` と `Refreshing`：`Scroll` と同じ、引っ張って更新です（Windows と Linux では F5）。
- `OnScroll`：`Scroll` と同じです。
- `Appearance`：リストの見た目です。`ui.ListPlain` は、端から端までの行を区切り線で仕切ります。`ui.ListInsetGrouped` は、プラットフォームのグループ化されたリストです（Linux では libadwaita の boxed list）。`ui.ListBare` は区切り線も行の背景もないので、リストの後ろにあるものが透けて見えます。自分でカードを描く行に使います。bare のリストでは、行のあいだの余白は行自身が取ります。スワイプも引き続き使えます。

```go
messages := signal.NewFunc(fetch(0, 30), nil)
remove := func(i int) {
	messages.Update(func(ms []message) []message { return slices.Delete(slices.Clone(ms), i, i+1) })
}
list := ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
			{Title: ui.Val("Archive"), Icon: "archivebox", OnPress: func() { remove(i) }},
		}
	},
	OnEndReached: func() {
		ms := messages.Peek()
		messages.Set(append(slices.Clone(ms), fetch(len(ms), 30)...))
	},
}, messages, func(m message) int { return m.ID }, row)
```

`LeadingSwipeActions` は、反対方向、つまり行の先頭側の端からスワイプすると現れるアクションです（「メール」の「未開封」や「ピン」など）。最初のアクションがその端に置かれ、最後までスワイプしたときに実行されます。iOS ではアクションに色が付き、Android では Material の Filled ボタンとして表示されます。

`ui.ScrollToItem(list, index, animated)` は、項目が見えるところまでリストをスクロールします。`ScrollTo` と `ScrollToEnd` も使えます。

### チャット：下端に固定したリスト

`Anchor: ui.AnchorBottom` は、チャット向けのリストです。最後の行を表示した状態で開き、行が領域を埋めないときは下端に寄せます。末尾にいるあいだは、末尾に行が追加されたときも、行の高さが変わったときも、リストが短くなったとき（キーボードが出たときなど）も、末尾にとどまります。ユーザーが上にスクロールすると末尾から離れ、末尾までスクロールし直すか `ui.ScrollToEnd` を呼ぶと、再び末尾に固定されます。`OnStartReached` は、最初の数行が表示されそうになると、先頭の項目ごとに一度だけ呼ばれます。ここで古いメッセージを読み込みます。

アンカーがどちらであっても、画面に表示中の行より上で行が追加・削除されたとき、表示中の行は動きません（React Native の `maintainVisibleContentPosition`）。上端に古いメッセージを読み込んだり、上の項目を削除したりしても、ユーザーは読んでいたところをそのまま読み続けられます。新しい行を表示するのは、リストが固定されている側の端だけです。上から読むリストなら上端（新着メールなど）、チャットなら末尾です。

```go
msgs := signal.NewFunc(latest(40), nil)
var list ui.Node
list = ui.List(ui.ListProps{
	Anchor:         ui.AnchorBottom,
	OnStartReached: func() { go loadOlder() }, // 読み込んだら ui.Post で先頭に追加する
}, msgs, func(m message) int { return m.ID }, bubble)
send := func(text string) {
	msgs.Set(append(slices.Clone(msgs.Peek()), message{Text: text}))
	ui.ScrollToEnd(list, true) // リストがどこにあっても、自分のメッセージを表示する
}
```

[`examples/chat`](../../examples/chat) は、会話画面をひととおり実装した例です。

### セクション

`Section` は、行をセクションに分けます。同じキーを持つ隣り合った行が 1 つのセクションになり、`Header` のビューがその見出しになります（チャットの日付、連絡先の頭文字、メールボックスの「今日」と「昨日」など）。`Header` はキーをアクセサとして受け取ります。見出しはそれ自体が 1 つの行で、画面に入ってくるときに作られ、その後は別のセクションに再利用されます。

```go
ui.List(ui.ListProps{
	Section: func(i int) string { return day(msgs.Peek()[i].At) },
	Header: func(day signal.Accessor[string]) ui.Node {
		return ui.Text(day, ui.Style{Font: fonts.Footnote.Weighted(fonts.Semibold), Padding: ui.Symmetric(6, 20)})
	},
}, msgs, key, row)
```

plain または bare のリストの見出しは、セクションの行がその下をスクロールしていくあいだ上端にとどまり、次の見出しが来ると押し上げられます（UITableView の plain スタイルと同じです。これに当たるものがない Android では、上端のセクションに結び付けたセルをリストの上に重ねます。デスクトップと Web では、見出しの行そのものがとどまります）。plain のリストの見出しはリストの背景の上に表示され、bare のリストの見出しは自分で背景を描きます（メッセージの上に重ねるチャットのピルなど）。inset grouped のリストでは、各セクションがそれぞれ独立したグループとして見出しの下に置かれ、見出しはセクションと一緒にスクロールします。

### 行の選択

`Selecting` は、リストを選択モードにします（「メール」の「編集」）。各行にプラットフォームの選択コントロールが表示され、行をタップすると、行を押す代わりに選択または選択解除します。行はスワイプできなくなります。`Selected` はどの行が選択されているかを示します（エフェクトの中で追従するので、シグナルで保持している選択は、変わるとすぐに表示に反映されます）。`OnSelect` はユーザーの選択を受け取ります。アプリが受け入れなかった選択は元に戻ります。行は、コントロールの分だけ幅を狭めてレイアウトされます。

```go
selecting := signal.New(false)
chosen := signal.NewFunc(map[int]bool{}, nil) // メッセージ ID をキーにする
ui.List(ui.ListProps{
	Selecting: selecting,
	Selected:  func(i int) bool { return chosen.Get()[msgs.Peek()[i].ID] },
	OnSelect: func(i int, v bool) {
		m := maps.Clone(chosen.Peek())
		if v {
			m[msgs.Peek()[i].ID] = true
		} else {
			delete(m, msgs.Peek()[i].ID)
		}
		chosen.Set(m)
	},
	OnSelectBegin: func(i int) { selecting.Set(true) },
}, msgs, key, row)
```

| | iOS | Android | macOS、Windows、Linux | Web |
|---|---|---|---|---|
| コントロール | UIKit の編集モードの円 | 行の末尾の Material のチェックボックス | 行のハイライト（アクセントカラーの選択色。Windows では WinUI のリストのピル） | 行の先頭にある、チェックマークで塗りつぶされる円。行には色が付く |
| `OnSelectBegin` | 行の上での 2 本指のパン。そのまま、なぞった行を次々に選択する | 長押し（Gmail と同じ） | — | — |
| スクリーンリーダー | 各行が選択可能 | 各行がチェックボックスで、行のテキストがラベル | — | 各行の選択状態（`aria-selected`） |

### グリッド

`Columns` を指定すると、リストはグリッドになります（`LazyVGrid`、`GridLayoutManager`）。項目はその数ずつ行を埋め、すべて同じ幅になり、縦横に `Gap` の間隔を空けて並びます。行の高さは、その行で最も高い項目に合わせます。グリッドには区切り線もスワイプアクションもありません。

```go
ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
	func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
		return ui.Image(ui.ImageProps{
			Source: signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
			Mode:   ui.ImageFill,
			Style:  ui.Style{AspectRatio: 1},
		})
	})
```

### List か、Scroll の中の For か

`ui.For` も、同じようにキーを付けて項目ごとに行を作ります。ただし、すべての行を作り、兄弟のビューと並ぶ通常のビューとして置きます。ページの一部になる短いリスト、たとえばいくつかのタグ、フォームの行、見出しの下のセクションなどには、`Scroll` の中で `For` を使ってください。

```go
ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.All(20), Gap: 8}},
	ui.StaticText("Tags", ui.Style{Font: fonts.Headline}),
	ui.For(tags, func(t string) string { return t }, func(t signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
		return ui.Text(t)
	}),
)
```

項目が多い、または上限がない場合（フィード、受信トレイ、検索結果、写真など）や、スワイプアクション、区切り線、末尾での追加読み込み、グリッドが必要な場合は `List` を使ってください。`List` はそれ自体がスクロールするので、`Scroll` の中には入れないでください。

## 独自のビュー

Asuka が対応していないウィジェットがプラットフォームにある場合は、自分で書いて `ui.Native` で表示できます。iOS では Swift、Android では Kotlin、macOS、Windows、Linux では Go（パッケージ `native` を通じて AppKit、Win32、GTK を使います）、Web では JavaScript で書きます。詳しくは[ネイティブコード](native.md)を参照してください。

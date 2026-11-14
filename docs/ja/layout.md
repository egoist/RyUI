# レイアウトとスタイル

`ui.Style` は、ビューをどう配置し、どう見せるかを指定します。レイアウトは CSS の flexbox で、単位はポイントです。見た目は色、角丸、枠線、影、テキストで決まります。このページでは、この両方と、状況に応じて変わるスタイルについて説明します。レイアウトはどのプラットフォームでも同じです。Asuka が Go で計算し、その結果の位置にプラットフォームのビューを配置します。

## プラットフォームのデフォルト

設定しなかったスタイルは、プラットフォームのデフォルトのままになります。スタイルを何も設定しないアプリは、フォント、色、ダークモード、文字サイズまで含めて、そのプラットフォーム純正のアプリと同じ見た目になります。

- **iOS**：UIKit の見た目。
- **Android**：Material 3 の見た目。
- **macOS**：AppKit の見た目。
- **Windows**：Windows 11 の見た目（WinUI の Fluent デザイン。ホストが Win32 のコントロールの上に描画します）。
- **Linux**：GNOME の見た目（GTK 4 と libadwaita）。
- **Web**：ブラウザーのコントロールと、その周りを囲む Asuka 独自のライト・ダークのパレット（[Web](web.md) を参照）。

変えたいものだけを設定してください。また、固定の色やフォントより、後述するセマンティックカラーとセマンティックフォントを使ってください。そうすれば、アプリはプラットフォームに追従し続けます。

## スタイル

`ui.Style` はただの構造体です。コンポーネントはスタイルを `Styler` として受け取ります。`Styler` には `Style` を渡すか、変化するスタイルなら `ui.StyleFunc` を渡します（[動的なスタイル](#動的なスタイル)を参照）。

```go
ui.Text(title, ui.Style{Font: fonts.Headline, Color: colors.SecondaryLabel})
ui.Button(ui.ButtonProps{Title: ui.Val("Follow"), Style: ui.Style{Shrink: ui.NoShrink}})
ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1), Background: colors.Separator}})
```

見た目のプロパティは、ネイティブのウィジェットが対応している場合にだけ反映されます。たとえばボタンは `Color` をティントカラーとして使い、ビューは `Font` を無視します。

## ビューの並べ方

`ui.Column` は子を上から下へ、`ui.Row` は先頭（leading）から末尾（trailing）へ並べます。`ui.View` は、スタイルの `Direction` で方向が決まるコンテナーです。`Direction` には `ui.ColumnDir`（デフォルト）か `ui.RowDir` を指定します。nil のスタイルは空のスタイルとして扱われます。

```go
ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12},
	Avatar(),
	ui.Column(ui.Style{Grow: 1, Gap: 2},
		ui.StaticText("Ada Lovelace", ui.Style{Font: fonts.Headline}),
		ui.StaticText("Notes on the Analytical Engine", ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel, Lines: 1}),
	),
	ui.Button(ui.ButtonProps{Title: ui.Val("Follow"), Style: ui.Style{Shrink: ui.NoShrink}}),
)
```

レイアウトのプロパティしか持たないコンテナーは、ネイティブのビューを作りません。その子は、ネイティブのビューを持つ最も近い祖先の中に配置されます。そのため、いくら入れ子にしてもコストはかかりません。

## Flexbox

レイアウトは CSS の flexbox に従い、デフォルト値は React Native と同じです。つまり、方向は縦（column）で、サイズにはパディングと枠線が含まれます。

- **`Gap`**：子どうしの間隔です。子が折り返したときは、行と行の間隔にもなります。
- **`Justify`** は、主軸方向の子の配置です。`ui.JustifyStart`（デフォルト）、`ui.JustifyCenter`、`ui.JustifyEnd`、`ui.JustifySpaceBetween`、`ui.JustifySpaceAround`、`ui.JustifySpaceEvenly` があります。
- **`Align`** は、交差軸方向の配置です。`ui.AlignStretch`（デフォルト。子が交差軸いっぱいに広がります）、`ui.AlignStart`、`ui.AlignCenter`、`ui.AlignEnd`、`ui.AlignBaseline` があります。
- **`AlignSelf`** は、その子についてだけ親の `Align` を上書きします。ゼロ値の `ui.AlignAuto` なら、親の値に従います。
- **`Wrap`** を設定すると、子が収まらないときに複数の行に折り返します。
- **`Grow`** は、親の主軸方向に余ったスペースのうち、その子が受け取る割合です。`Grow` を持つ子は、中身のサイズより小さく縮むこともできます。そのため、`Scroll` を囲む `Grow` 付きのビューは、スクロールの中身の高さ全体に広がるのではなく、その中でスクロールします。
- **`Shrink`** は、行があふれたときにその子が譲る量です。デフォルトは 0 で、1 として扱われます。このとき、テキストは折り返し、コントロールは最小サイズを保ちます。`ui.NoShrink` を指定すると、子のサイズは縮みません。
- **`Basis`** は、主軸方向での子の初期サイズです。デフォルトの Auto は中身のサイズです。

```go
// 幅を分け合う二つのボックス。一方はもう一方の 2 倍。
ui.Row(ui.Style{Gap: 8, Height: ui.Pt(60)},
	ui.View(ui.ViewProps{Style: ui.Style{Grow: 2, Background: colors.SecondaryBackground}}),
	ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Background: colors.SecondaryBackground}}),
)

// 両端に置いたラベルと値。
ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter},
	ui.StaticText("Total"),
	ui.StaticText("$42", ui.Style{Font: fonts.Title2}),
)

// 複数行に折り返すチップ。
ui.Row(ui.Style{Wrap: true, Gap: 8},
	ui.StaticText("Go"), ui.StaticText("iOS"), ui.StaticText("Android"),
)
```

## サイズ

長さは `ui.Dim` で表します。

- `ui.Pt(v)`：ポイント。iOS と macOS ではポイント、Android では dp、Windows では拡大率 100% のときの 1 ピクセル（1/96 インチ）、Linux では GTK の論理ピクセル、Web では CSS ピクセルです。
- `ui.Pct(v)`：同じ軸方向の、親のサイズに対するパーセンテージ。
- `ui.Auto`（ゼロ値）：中身のサイズ、または flexbox が割り当てるサイズ。

`Width` と `Height` でサイズを指定し、`MinWidth`、`MinHeight`、`MaxWidth`、`MaxHeight` でその範囲を制限します。`AspectRatio` は幅を高さで割った比です。一辺のサイズが決まっていれば、もう一辺をこの比から決めます。

```go
ui.View(ui.ViewProps{Style: ui.Style{
	Width:       ui.Pct(50),
	MinWidth:    ui.Pt(120),
	MaxWidth:    ui.Pt(320),
	AspectRatio: 16.0 / 9,
	Background:  colors.Gray,
}})
```

テキストとコントロールは、プラットフォーム自身の計測方法で自分のサイズを測ります。サイズを指定するのは、それを変えたいときだけにしてください。

1 ポイントが何ピクセルになるかは、ディスプレイのスケールで決まります。スマートフォンでは 2 か 3、Retina の Mac では 2、Windows と Linux ではディスプレイの拡大率（150% なら 1.5）、Web ではブラウザーの device pixel ratio です。実際の値は `ui.UseEnvironment()` の `Scale` でわかります。デスクトップのウィンドウをスケールの異なるディスプレイに移すと、そのディスプレイのスケールで描画されます。ポイント単位のレイアウトは変わりません。

## パディングとマージン

`Padding` と `Margin` は `ui.Edges` で、辺ごとにポイントで指定します。

- `ui.All(v)`：すべての辺に v。
- `ui.Symmetric(v, h)`：上下に v、左右に h。
- `ui.Edges{Top: 24}`：辺を名前で個別に指定。

```go
ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Margin: ui.Edges{Top: 24}},
	ui.StaticText("Padded"),
)
```

右から左に書く言語では、`Row` は右から並び、`Justify` と `Align` もそれに従います。テキストの行が始まる側も右になります。一方、`Left` と `Right` は CSS と同じく、書かれたとおりの側を指します。テキストが始まる側や終わる側に余白を置きたいときは、`Start` や `End` を使います。

```go
ui.Style{Padding: ui.Edges{Start: 16, End: 8}} // 英語では左に 16、アラビア語では右に 16
```

[多言語対応](i18n.md)も参照してください。

## 絶対配置

`Absolute` を設定するとビューは通常のフローから外れ、`Top`、`Right`、`Bottom`、`Left` で親の中での位置を指定できます。次の例では、アバターの角にバッジを重ねています。

```go
ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(44), Height: ui.Pt(44)}},
	Avatar(),
	ui.View(ui.ViewProps{Style: ui.Style{
		Absolute: true, Top: ui.Pt(-4), Right: ui.Pt(-4),
		Width: ui.Pt(18), Height: ui.Pt(18), CornerRadius: 9,
		Background: colors.Red, Align: ui.AlignCenter, Justify: ui.JustifyCenter,
	}}, Badge()),
)
```

後の子ほど、前の子の上に重ねて描画されます。

## Grid

`Grid` を設定したコンテナーは、子を CSS grid で配置します。トラックの指定には、`layout` パッケージの `layout.Fr`、`layout.Px`、`layout.Repeat`、`layout.Minmax` などを使います。子を複数のトラックにまたがらせるには、`GridColumn` と `GridRow` を使います。

```go
ui.View(ui.ViewProps{Style: ui.Style{Gap: 8, Grid: &ui.Grid{
	Columns: []layout.GridTrack{layout.Repeat(3, layout.Fr(1))},
}}},
	Avatar(), Avatar(), Avatar(), Avatar(),
	ui.View(ui.ViewProps{Style: ui.Style{GridColumn: layout.Placement{Start: layout.Span(3)}, Height: ui.Pt(20), Background: colors.Gray}}),
)
```

多数の項目をスクロールできるグリッドで表示するなら、`Columns` を指定した `ui.List` を使ってください（[コンポーネント](components.md)を参照）。

## セーフエリア、キーボード、ウィンドウ

スマートフォンでは、ルートは画面全体を覆い、ステータスバー、ノッチ、ホームインジケーターの下にまで広がります。`ui.SafeArea` は、自身のパディングに加えて、セーフエリアのインセット分のパディングを持つ `Column` です。キーボードが表示されている間は下のパディングがキーボードの高さになるので、下端にあるもの（テキストフィールドや送信ボタン）はキーボードの上に出たままになります。

```go
ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12},
	ui.StaticText("Todos", ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
	ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, rows...),
	composer,
)
```

インセットは、そのビューがある画面のものです。スタックの画面、タブ、シートの中なら、その画面のインセットになります。自分で配置したいときは、`ui.UseEnvironment` でインセットとキーボードの高さを取得できます（[基本概念](concepts.md)を参照）。

macOS、Windows、Linux では、ルートはタイトルバー（Linux ではヘッダーバー）の下にある、ウィンドウのコンテンツ領域を埋めます。デスクトップのウィンドウにはセーフエリアがなく、オンスクリーンキーボードが重なることもありません。そのため、インセットとキーボードの高さは 0 になり、`ui.SafeArea` は自身のパディングだけを持つ `Column` になります。それでも `ui.SafeArea` は使っておきましょう。そうすれば、同じ画面がスマートフォンにもそのまま収まります。

アプリが指定しない限り、ウィンドウのサイズは 800 × 600 ポイントです。サイズと、ユーザーが縮められる最小サイズは `app.RunWindow` で指定します。

```go
func main() {
	app.RunWindow(ui.WindowOptions{Width: 960, Height: 640, MinWidth: 480, MinHeight: 360}, App)
}
```

スマートフォンでは、これらのオプションは無視されます。ユーザーがウィンドウのサイズを変えると、環境の `Width` と `Height` が変わってレイアウトが再実行されるので、アプリ側で処理することは何もありません。あるサイズでレイアウトを切り替えたいとき（スペースに余裕があればサイドバーを表示するなど）は、タブレットの場合と同じく、`StyleFunc` や `Show` の中で環境の `WidthClass` を読みます。

ブラウザーでは、ページ全体を占めるアプリは、デバイスのセーフエリア（ノッチのあるスマートフォンでの `env(safe-area-inset-*)`）と、visual viewport から得たオンスクリーンキーボードの高さを使います。ページの一部に埋め込まれたアプリには、どちらもありません。

## スクロール

`ui.Scroll` は子を縦方向に、`Horizontal` を指定すれば横方向にスクロールします。スクロールする範囲が決まるように、サイズか `Grow: 1` を指定してください。`Style` はスクロールビュー自体のボックスに、`ContentStyle` は子が並ぶボックスに適用されます。パディングや間隔は `ContentStyle` に指定します。

```go
ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.All(16), Gap: 12}},
	ui.StaticText("Long content"),
)
```

引っ張って更新する機能とスクロール位置については[コンポーネント](components.md)を、長いリストについては[リスト](components.md)を参照してください。

## 色

`ui.Color` は `colors` パッケージ（`github.com/egoist/asuka/colors`）で作ります。色は次のいずれかです。

- **プラットフォームのデフォルト**：ゼロ値。プロパティを設定しなかった場合です。
- **セマンティックカラー**：実際の色はプラットフォームが決めるので、ダークモードに追従します。
  - テキスト：`colors.Label`、`colors.SecondaryLabel`、`colors.TertiaryLabel`。
  - 背景：`colors.Background`、`colors.SecondaryBackground`、`colors.GroupedBackground`、そしてその上に置くグループ化されたリストの行のための `colors.SecondaryGroupedBackground`（iOS の inset grouped のセル）。
  - `colors.Separator`、`colors.Accent`（アプリのアクセントカラー）、`colors.OnAccent`（アクセントカラーの上で読みやすい色。アクセントカラーで塗りつぶしたボタンの上のアイコンなど）、`colors.Clear`（透明）。
  - システムの色：`colors.Red`、`colors.Orange`、`colors.Yellow`、`colors.Green`、`colors.Blue`、`colors.Purple`、`colors.Pink`、`colors.Gray`。
- **固定色**：`colors.RGB(r, g, b)`、`colors.RGBA(r, g, b, a)`、`colors.Hex(0x7c3aed)`、`colors.ParseHex("#7c3aed")`（`"#rrggbbaa"` も使えます）。`colors.ParseHex` は、パースできたかどうかも返します。

セマンティックカラーの出どころは次のとおりです。

| プラットフォーム | セマンティックカラー |
| --- | --- |
| iOS | UIKit の色（`labelColor`、`systemBackground`、`systemRed` など） |
| Android | Material 3 のカラーロール |
| macOS | AppKit の色（`labelColor`、`windowBackgroundColor`、`controlBackgroundColor`、`separatorColor`、`systemRedColor` など） |
| Windows | Windows 11 のテーマカラー（WinUI の Fluent テーマリソース）。ライトまたはダーク |
| Linux | libadwaita の名前付きカラー（`window_fg_color`、`window_bg_color`、`card_bg_color`、`destructive_color` など）。テーマの定義どおりで、ハイコントラストにも対応 |
| Web | Asuka のパレット。CSS 変数で、ライトとダークの両方 |

デスクトップでは、グループ化用の背景は通常の背景と同じです。`colors.GroupedBackground` はウィンドウの背景色、`colors.SecondaryGroupedBackground` は `colors.SecondaryBackground` と同じくカードの背景色になります。

```go
ui.Column(ui.Style{Padding: ui.All(16), Gap: 4, Background: colors.Background, CornerRadius: 12},
	ui.StaticText("Title", ui.Style{Font: fonts.Headline}),
	ui.StaticText("Details", ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel}),
)
```

`c.Opacity(a)` は、色を 0 から 1 の不透明度で描画した色です（SwiftUI の `.opacity`、Compose の `copy(alpha =)` にあたります）。セマンティックカラーはセマンティックカラーのままなので、`colors.Accent.Opacity(0.15)` はライトでもダークでもアクセントカラーの淡い色になります。チップの背景、グラフの塗り、トラックなどに使えます。

`Color` は、テキストの色、またはコントロールのティントカラーです。

## 背景、枠線、影

- **`Background`**：ビューの背景色。
- **`CornerRadius`**：角を丸めます。単位はポイントです。
- **`BorderWidth`** と **`BorderColor`**：枠線。ボックスの内側に描画され、パディングと同じように中身を内側に寄せます。
- **`Shadow`**：ドロップシャドウ。中に描かれた内容ではなく、ビューの形（`CornerRadius` で角を丸めた境界）から落ちる影です。指定するのは `Color`（指定しなければ影は出ません）、`Radius`（CSS や SwiftUI と同じぼかし半径で、ぼかしの σ の 2 倍）、オフセットの `X` と `Y` です。子をクリップするビュー（`Clip`）でも影は出ます。画像をクリップした角丸のカードに影が付くのと同じです。Android では代わりにエレベーションの影を描画します。半径がエレベーションになり、色は影の色味になり、オフセットは光源で決まります。macOS、Windows、Linux、Web では、iOS と同じぼかしとオフセットで描画します。
- **`Gradient`**：背景に重ねる線形グラデーション。`ui.LinearGradient(colors...)` は上から下へのグラデーションです。`ui.Gradient` では、`Start` と `End` をビューのサイズを単位とした `ui.Point` で指定し（(0, 0) が上端の先頭側の角、(1, 1) が下端の末尾側の角）、`Locations` を色ごとに一つずつ指定します。Android では 8 方向のうち最も近い方向に沿って描画し、ほかのプラットフォームでは指定した線に沿って描画します。
- **`Material`**：背後にあるものをぼかす半透明の背景で、iOS のマテリアルにあたります。`ui.MaterialUltraThin`、`ui.MaterialThin`、`ui.MaterialRegular`、`ui.MaterialThick`、`ui.MaterialBar` があります。
  - iOS：`UIVisualEffectView` のぼかし。
  - macOS：ビューの内容の背後に置いた `NSVisualEffectView`。ウィンドウ内でその背後にあるものをぼかします。
  - Windows：ホストが描画する Fluent のアクリル。ビューの背後にあるものをぼかし、ライトまたはダークの色味を付けます。
  - Web：CSS の `backdrop-filter` によるぼかしと彩度の調整。
  - Android と Linux はビューの背後をぼかせないため、サーフェスの色（Linux では libadwaita のカードの色）を半透明で表示します。
- **`Clip`**：子をビューの境界と角丸でクリップします。

```go
ui.View(ui.ViewProps{Style: ui.Style{
	Height:       ui.Pt(120),
	CornerRadius: 16,
	Gradient:     ui.LinearGradient(colors.Hex(0x6366f1), colors.Hex(0xec4899)),
	Shadow:       ui.Shadow{Color: colors.RGBA(0, 0, 0, 80), Radius: 16, Y: 8},
}})

ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(12), BorderWidth: 1, BorderColor: colors.Separator, CornerRadius: 8, Clip: true}},
	ui.StaticText("Bordered"),
)

// 左から右へ。
ui.View(ui.ViewProps{Style: ui.Style{
	Height:   ui.Pt(80),
	Gradient: &ui.Gradient{Colors: []ui.Color{colors.Blue, colors.Purple}, Start: ui.Point{X: 0, Y: 0.5}, End: ui.Point{X: 1, Y: 0.5}},
}})
```

## 不透明度と非表示

`Opacity` は 0 から 1 の値ですが、ゼロ値は、設定しなかったプロパティと同じく不透明を意味します。つまり `Opacity: 0.5` は半透明で、`Opacity: 0` は完全に不透明です。ビューを隠すには `Hidden` を設定します。隠したビューも、レイアウト上のスペースは占めたままです。レイアウトから外し、中のビューもまとめて取り除くには、`ui.Show` を使います（[基本概念](concepts.md)を参照）。

## トランスフォーム

`Transform` は、描画時にビューを移動、拡大縮小、回転させます（`TranslateX`、`TranslateY`、`Scale`、`ScaleX`、`ScaleY`、度単位の `Rotate`）。レイアウトはトランスフォームの影響を受けません。トランスフォームはすべてのプラットフォームで描画されます。トランスフォーム、`Transition`、`Match` はアニメーションのためのものです。[モーションとジェスチャー](motion.md)を参照してください。

## テキスト

`Font` はテキストのフォントを指定します。値は `fonts` パッケージ（`github.com/egoist/asuka/fonts`）の `fonts.Font` です。

- **セマンティックフォント**：プラットフォームのテキストスタイルで、ユーザーの文字サイズ設定に合わせて拡大縮小されます。`fonts.LargeTitle`、`fonts.Title`、`fonts.Title2`、`fonts.Title3`、`fonts.Headline`、`fonts.Subheadline`、`fonts.Body`、`fonts.Callout`、`fonts.Footnote`、`fonts.Caption`、`fonts.Caption2` があります。
- **`fonts.System(size)`**：指定したサイズ（ポイント）のシステムフォント。
- **`.Weighted(w)`**：フォントにウェイトを付けます。`fonts.Ultralight`、`fonts.Thin`、`fonts.Light`、`fonts.Regular`、`fonts.Medium`、`fonts.Semibold`、`fonts.Bold`、`fonts.Heavy`、`fonts.Black` があります。

`Color` はテキストの色、`TextAlign` は揃え方（`ui.TextStart`、`ui.TextCenter`、`ui.TextEnd`、`ui.TextJustify`）、`Lines` は表示する最大行数です。`Lines` のデフォルトの 0 は無制限を意味します。テキストはデフォルトで折り返します。

```go
ui.StaticText("Brand", ui.Style{Color: colors.RGB(124, 58, 237), Font: fonts.Title.Weighted(fonts.Bold)})
ui.StaticText("A long paragraph that stops after two lines.", ui.Style{Font: fonts.Body, Lines: 2, TextAlign: ui.TextCenter})
ui.StaticText("Fixed size", ui.Style{Font: fonts.System(20).Weighted(fonts.Medium)})
```

複数のスタイル、リンク、インラインのビューを含むテキストについては、[RichText](components.md) を参照してください。

テキストスタイルの名前は iOS のものです。各プラットフォームはこれを自身のタイプランプ（文字サイズの体系）に対応づけるので、`fonts.Body` は、そのプラットフォームのアプリが本文に使うサイズになります。

| プラットフォーム | テキストスタイル | `fonts.Body` |
| --- | --- | --- |
| iOS | UIKit のテキストスタイル。フォントは San Francisco | 17 |
| Android | Material 3 のタイプスケール（`fonts.Body` は Body Large）。フォントは Roboto | 16 |
| macOS | AppKit のテキストスタイル（`NSFont.preferredFont(forTextStyle:)`）。フォントは San Francisco | 13 |
| Windows | Windows 11 のタイプランプ。フォントは Segoe UI Variable（Windows 11 より前は Segoe UI） | 14 |
| Linux | libadwaita のタイプクラス（`.title-1`、`.heading`、`.caption` など）。デスクトップのインターフェースフォントに対する相対値 | インターフェースフォントのサイズ |
| Web | Asuka のタイプランプ。フォントはシステムのフォント（`system-ui`） | 16 |

Windows では、各スタイルはタイプランプの中で最も近いものになります。`fonts.LargeTitle` は Title Large (40)、`fonts.Title` は Title (28)、`fonts.Title2` は Subtitle (20)、`fonts.Headline` は Body Strong（14、semibold）、`fonts.Footnote` と `fonts.Caption` は Caption (12) です。Linux では、GNOME のインターフェースフォントが 11 pt の場合、`fonts.LargeTitle` は `.title-1`（181%、heavy）、`fonts.Title` は `.title-2`、`fonts.Headline` は `.heading`（bold）、`fonts.Caption` は `.caption`（82%）になります。

ユーザーの文字サイズ設定によって拡大縮小されるのは、次のとおりです。

- **iOS**（Dynamic Type）と **Android**（フォントサイズ）：セマンティック、固定サイズ、カスタムを問わず、すべてのフォント。
- **Windows**：すべてのフォント。「設定」>「アクセシビリティ」>「テキストのサイズ」に従います。
- **Linux**：すべてのフォント。デスクトップのテキストのスケーリング（GNOME の「大きな文字」(Large Text)、GTK のフォント DPI）に従います。スタイルの基準になるインターフェースフォントも、デスクトップの設定（GTK の `gtk-font-name`）に従います。
- **Web**：すべてのフォント。フォントは `rem` で指定されるので、ブラウザーの文字サイズ設定に従います。
- **macOS** には文字サイズの設定がないため、フォントはそのままです。

その倍率は `ui.UseEnvironment()` の `FontScale` で取得できます。デフォルトは 1 です（[アクセシビリティ](accessibility.md)を参照）。

## カスタムフォント

フォントファイル（`.ttf`、`.otf`）は、アプリの `assets/fonts` に置きます。フォントを選ぶには、フォントの `Family` か `fonts.Custom(family, font)` に次のいずれかを指定します。

- ファイル名：`assets/fonts/Inter-Bold.ttf` なら `"Inter-Bold"`。
- ファイルのファミリー：`"Inter"` を指定すると、フォントのウェイトに応じて `Inter-Regular`、`Inter-Bold` などが使われます。
- 汎用ファミリー：`"serif"`、`"monospace"`、`"rounded"`。iOS と macOS ではシステムフォントのデザイン（New York、SF Mono、SF Rounded）、Windows では Georgia と Cascadia Mono、Linux と Android ではシステムの serif と monospace のファミリー、Web ではブラウザーが対応していれば `ui-serif`、`ui-monospace`、`ui-rounded` になります。Android、Windows、Linux には rounded にあたるものがないため、システムフォントで表示されます。
- システムにあるファミリー：`"Georgia"` など。

カスタムフォントは、元にしたテキストスタイルと同じように拡大縮小されます。プラットフォームにないフォントは、システムフォントで表示されます。`assets/fonts` のファイルは、どのプラットフォームでもそのアプリ専用に読み込まれ、ほかのアプリ向けにインストールされることはありません。

```go
ui.StaticText("Inter Bold", ui.Style{Font: fonts.Custom("Inter-Bold", fonts.Title)})
ui.StaticText("Inter", ui.Style{Font: fonts.Custom("Inter", fonts.Body).Weighted(fonts.Semibold)})
ui.StaticText("Code", ui.Style{Font: fonts.Custom("monospace", fonts.Footnote)})
```

## ダークモード

セマンティックカラーはダークモードに合わせて自動的に変わるので、それを使うアプリはダークモードのためのコードを書く必要がありません。固定色は変わりません。モードごとに色を選ぶには、`StyleFunc` の中で環境の `Dark` を読みます。

```go
env := ui.UseEnvironment()
ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
	s := ui.Style{Height: ui.Pt(80), CornerRadius: 12, Background: colors.RGB(255, 255, 255)}
	if env.Get().Dark {
		s.Background = colors.RGB(28, 28, 30)
	}
	return s
})})
```

## 動的なスタイル

`ui.StyleFunc` はスタイルを返す関数で、中で読んだシグナルが変わると再実行されます。新しいスタイルは前回のものと比較され、レイアウトのプロパティが変わった場合にだけレイアウトが再実行されます。ビューに送られるのも、変わった見た目のプロパティだけです。

```go
ui.View(ui.ViewProps{
	OnPress: func() { selected.Set(!selected.Peek()) },
	Style: ui.StyleFunc(func() ui.Style {
		s := ui.Style{Padding: ui.All(12), CornerRadius: 10, Background: colors.SecondaryBackground}
		if selected.Get() {
			s.Background = colors.Accent
			s.BorderWidth = 2
			s.BorderColor = colors.Label
		}
		return s
	}),
}, ui.StaticText("Tap me"))
```

この変更を `ui.Animate` の中で行えば、アニメーションになります（[モーションとジェスチャー](motion.md)を参照）。

## アクセントカラー

`colors.Accent` と、それを使うコントロールは、`asuka.json` に書いたアプリのアクセントカラーを使います。

```json
{ "accent": "#7c3aed" }
```

アクセントカラーは、iOS ではティントカラーになり、Android では Material 3 のカラースキームのシードカラーになります（Android 12 以降）。`"android": {"dynamicColor": true}` を指定すると、Android は Material のアプリと同じく、システムが対応していればユーザーの壁紙から色を取ります。macOS、Windows、Linux では、Asuka のコントロールと色の中で、システムのアクセントカラーの代わりに使われます。Web では、さらにコントロールの `accent-color` にもなります。

アクセントカラーを指定しなければ、プラットフォームのものが使われます。macOS と Windows ではユーザーが選んだアクセントカラー（macOS は「システム設定」>「外観」、Windows は「設定」>「個人用設定」>「色」）、Linux では libadwaita の青、Web では Asuka 独自の青です。[配布](shipping.md)も参照してください。

## ライトとダーク

アプリはシステムの外観に従います。iOS と macOS のダークモード、Android のダークテーマ、Windows のアプリモード（「設定」>「個人用設定」>「色」）、GNOME のダークスタイル、ブラウザーの `prefers-color-scheme` です。`ui.SetColorScheme` を使うと、アプリ独自の外観設定と同じように、システムの設定にかかわらずライトかダークで表示できます（SwiftUI の `preferredColorScheme`、Android では AppCompat のナイトモード、macOS ではアプリの `NSAppearance`、Linux では libadwaita のスタイルマネージャーにあたります）。`ui.ColorSchemeSystem` を指定すると、再びシステムに従います。セマンティックカラー、プラットフォームのコントロール、システムバーは、アプリを作り直すことなく切り替わり、今どちらなのかは `ui.UseEnvironment()` の `Dark` でわかります。選んだ設定は [`store`](data.md) で保存し、アプリの起動時に適用しましょう。

```go
var scheme = store.Signal("colorScheme", int(ui.ColorSchemeSystem))

func App() ui.Node {
	ui.SetColorScheme(ui.ColorScheme(scheme.Peek()))
	// ...
}
```

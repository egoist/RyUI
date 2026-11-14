# アクセシビリティ

Asuka は各プラットフォーム純正のウィジェットを表示するので、スクリーンリーダーやキーボード、文字サイズの設定に必要なもののほとんどは、コードを書かなくても最初から揃っています。このページでは、何もしなくても得られるものと、それ以外を補うためのプロパティを紹介します。扱うのは、ラベル、ロール、グループ化、カスタムアクション、調整可能なビュー、読み上げ、キーボード、文字サイズ、視差効果を減らす（Reduce Motion）、そしてそれらのテスト方法です。

どのホストも同じプロパティを各プラットフォームのアクセシビリティ API に渡し、スクリーンリーダーはそこから情報を読み取ります。

| プラットフォーム | アクセシビリティ API | スクリーンリーダー |
| --- | --- | --- |
| iOS | UIAccessibility | VoiceOver |
| Android | Android のユーザー補助フレームワーク（`AccessibilityNodeInfo`） | TalkBack |
| macOS | NSAccessibility | VoiceOver |
| Windows | UI Automation | ナレーター、NVDA |
| Linux | GTK のアクセシビリティ（AT-SPI 経由） | Orca |
| Web | ページの要素に付ける ARIA のロールと属性 | ブラウザーで使うもの：VoiceOver、NVDA、TalkBack など |

## 何もしなくても得られるもの

- **コントロールはプラットフォームのものです。** `Button`、`Toggle`、`Slider`、`TextField`、`Segmented`、`Stepper`、`DatePicker` などは、UIKit、Material、AppKit、Win32、GTK、ブラウザーそれぞれの純正のコントロールです。ほかのアプリと同じように読み上げられ、操作できます。
- **押せるビューはボタンになります。** `OnPress` を持つ `View` は一つの要素になります。
  - iOS：中にあるテキストとラベル付きの画像をつなげて読み上げるボタンになります。ダブルタップで押せます。
  - Android：クリック可能なビューになり、TalkBack は中のテキストをまとめて読み上げます。
  - macOS：ボタン（`AXButton`）になります。
  - Windows：中のテキストをつなげたものを名前とするボタンになり、スクリーンリーダーから押せます（UI Automation の Invoke）。
  - Linux：ボタンになります。
  - Web：`button` ロールを持つ要素になります。

  Windows、Linux、Web ではキーボードでも操作できます（[キーボード](#キーボード)を参照）。
- **文字は拡大縮小されます。** セマンティックなフォント（`fonts.Body`、`fonts.Headline` など）も、固定サイズ（`fonts.System(15)`）も、アプリ独自のフォントも、すべてのフォントがユーザーの文字サイズの設定に従います。対象は Dynamic Type、Android のフォントサイズ、Windows のテキストのサイズ、GNOME のテキストの拡大率、ブラウザーの文字サイズです。ユーザーが設定を変えると、Asuka はテキストを測り直し、それによって動くものをレイアウトし直します。Mac には文字サイズの設定がありません。
- **色は環境に合わせて変わります。** セマンティックカラー（`colors.Label`、`colors.SecondaryLabel`、`colors.Background`、`colors.Accent` など）はプラットフォームの色です（[レイアウトとスタイル](layout.md)を参照）。ダークモードに従い、iOS と macOS では「コントラストを上げる」、Linux では GNOME のハイコントラストにも従います。
- **レイアウトのためだけのビューは邪魔をしません。** イベントもアクセシビリティのプロパティも持たない `View`、`Row`、`Column` はネイティブのビューを作らないので、アクセシビリティツリーに現れることはありません。
- **画像は説明を付けるまで読み上げられません。** `Label` のない `Image`、`Symbol`、`Shape` はスクリーンリーダーから隠されます。ただし Linux と Web では、よく使うシンボルは例外で、ラベルを付けない限り英語の名前（Back、Archive）で読み上げられます。

## ラベルとヒント

`Label` は、スクリーンリーダーがそのビューについて読み上げる内容です。`Hint` は、ラベルだけでは伝わらないときに、そのビューを使うと何が起きるかを伝えます。

```go
ui.Row(ui.Style{Gap: 12},
	ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled, OnPress: send}),
	ui.Image(ui.ImageProps{Source: ui.Val("https://picsum.photos/id/1015/600/400"), Label: ui.Val("A river in a valley"),
		Style: ui.Style{AspectRatio: 3.0 / 2}}),
	ui.View(ui.ViewProps{Label: ui.Val("Favorite"), Role: ui.RoleImage},
		ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: colors.Red})),
)
```

- `Button` はタイトルを読み上げます。アイコンだけのボタンには `Label` を付けてください。プラットフォームが自分で名前を付けるシンボルはごく一部で、Android では英語の名前しかありません。
- `Image` の `Label` はその画像の説明です。`Label` がなければ装飾として扱われます。[`asuka/sdk/svg`](../../sdk/svg) のアイコンにも同じように `Label` を付けられます。
- `Symbol` は自分のラベルを持ちません。意味のあるシンボルは、`Label` と `ui.RoleImage` を指定した `View` で包んでください。
- `Hint` は、iOS ではアクセシビリティのヒントとして少し間を置いてから読み上げられ（ユーザーはヒントをオフにできます）、Android では状態の説明（state description）としてラベルと一緒に読み上げられます。macOS ではビューのヘルプ（`accessibilityHelp`）、Windows ではヘルプテキスト、Linux ではアクセシブルな説明（accessible description）、Web では `aria-description` になります。「メッセージを開きます」のように短くしてください。

`Label`、`Hint`、`Role`、`Decorative` は、ビューを作るときに一度だけ設定されます。ラベルを変える必要があるなら、それは別のビューとして扱います。`Show` や `Dynamic` の分岐の中で作るか、押せるビューなら `Label` を付けずに、変化する中のテキストを読み上げさせてください。

## ロール

`Role` は、そのビューが何であるかを示します。

| ロール | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| `ui.RoleNone` | なし（デフォルト） | なし | なし | なし | なし | なし |
| `ui.RoleButton` | button トレイト | ボタンとして読み上げ | `AXButton` | Button | `button` | `button` |
| `ui.RoleLink` | link トレイト | ボタンとして読み上げ | `AXLink` | Hyperlink | `link` | `link` |
| `ui.RoleHeader` | header トレイト | 見出し | `AXHeading` | Text（見出しレベル 1） | `heading` | `heading` |
| `ui.RoleImage` | image トレイト | 画像として読み上げ | `AXImage` | Image | `img` | `img` |
| `ui.RoleText` | static text トレイト | なし | `AXStaticText` | Text | `label` | なし |
| `ui.RoleSearch` | search field トレイト | なし | `AXTextField` | Edit | `search` | `searchbox` |

Windows の列は UI Automation のコントロールタイプ、Linux の列は GTK のアクセシブルロール、Web の列は ARIA のロールです。押せる `View` はもともとボタンです。iOS と macOS では、`Role` を持つビューはそれ自体が一つの要素になります。そのため `Label` も付けてください。付けないと、ロールだけが読み上げられます。

## グループ化

スクリーンリーダーは要素を一つずつたどります。テキストが三つ並んだ行は三回止まることになりますが、一回で済ませたいことがよくあります。方法は二つあります。

- **押せるビューにする。** `OnPress` を持つ `View` は一つの要素になり、中身をまとめて読み上げます。行はたいていこの形です。

```go
func Row(m message, open func()) ui.Node {
	return ui.View(ui.ViewProps{
		OnPress: open,
		Hint:    ui.Val("Opens the message"),
		Style:   ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.Symmetric(12, 20)},
	},
		ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: colors.SecondaryLabel})),
		ui.Column(ui.Style{Grow: 1, Gap: 2},
			ui.StaticText(m.from, ui.Style{Font: fonts.Headline}),
			ui.StaticText(m.subject, ui.Style{Color: colors.SecondaryLabel}),
		),
	)
}
```

- **`Label` を付ける。** `Label` を持つ `View` は、そのラベルとして読み上げられます。iOS と macOS では、中身は個別には読み上げられません。Android ではラベルがビューのコンテンツの説明（content description）になり、Windows ではビューの名前、Linux ではアクセシブルなラベル、Web では `aria-label` になります。目で見てひと目でわかる内容を、言葉にして書いてください。「4:00」ではなく「Night Drive by Ada, 4 minutes」のようにします。

```go
ui.View(ui.ViewProps{Label: fmt.Sprintf("%s by %s, %d minutes", title, artist, minutes),
	Style: ui.Style{Direction: ui.RowDir, Gap: 8}},
	ui.StaticText(title, ui.Style{Font: fonts.Headline}),
	ui.StaticText(artist, ui.Style{Color: colors.SecondaryLabel}),
	ui.StaticText(fmt.Sprintf("%d:00", minutes), ui.Style{Color: colors.SecondaryLabel}),
)
```

## 装飾

`Decorative` は、ビューとその中身すべてをスクリーンリーダーから隠します。飾りや、同じことを言うテキストの隣にあるアイコン、「保存しました」と読み上げるトーストの中のチェックマークなどに使います。

```go
ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Green}))
```

## 見出し

スクリーンリーダーのユーザーは見出しから見出しへ移動します（VoiceOver のローター、TalkBack の見出し単位のナビゲーション、NVDA と Orca の H キー、ブラウザーのスクリーンリーダーの見出し一覧）。セクションのタイトルは見出しとしてマークしてください。`Role: ui.RoleHeader` を指定した `RichText` か、`Label` とこのロールを指定した `View` を使います。`Text` はロールを取りません。[`asuka/sdk/markdown`](../../sdk/markdown) は見出しを最初からマークします。

```go
ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Recent")}), Role: ui.RoleHeader,
	Style: ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}}),
ui.View(ui.ViewProps{Label: ui.Val("Settings"), Role: ui.RoleHeader},
	ui.StaticText("Settings", ui.Style{Font: fonts.Title2})),
```

`RichText` の中のリンクはそれぞれが独立した要素で、スクリーンリーダーから見つけて実行できます。

## カスタムアクション

目の見えるユーザーがスワイプや長押し、行の中の小さなボタンで行う操作に、スクリーンリーダーのユーザーはアクションを通じてたどり着きます。VoiceOver ではアクションのローターで上下にスワイプし、TalkBack ではアクションメニューを使います。`Actions` はビューにアクションを並べます。

```go
ui.View(ui.ViewProps{
	Actions: []ui.A11yAction{{Name: ui.Val("Reply"), Do: reply}, {Name: ui.Val("Archive"), Do: archive}},
	Style:   ui.Style{Padding: ui.Symmetric(12, 20)},
}, ui.Text(subject))
```

アクションを持つビューは一つの要素になり、中のテキストが読み上げられます。アクションが現れる場所は次のとおりです。

- **iOS** と **macOS**：ビューのカスタムアクション（`UIAccessibilityCustomAction`、`NSAccessibilityCustomAction`）になります。iOS では VoiceOver のアクションのローターに、Mac では VoiceOver のアクションメニュー（VO-Command-Space）に表示されます。
- **Android**：TalkBack のアクションメニューに表示されます。
- **Windows**：ビューのコンテキストメニューになります。キーボード（Shift+F10、アプリケーションキー）でも、マウスの右クリックでも開けます。ビューはタブストップになるので、キーボードでたどり着けます。
- **Linux**：AT-SPI 経由のビューのアクションになり、Orca が一覧にして実行します。あわせてビューのコンテキストメニュー（副ボタンのクリック、アプリケーションキー、Shift+F10）にもなります。ビューはタブストップになります。
- **Web**：ARIA にはカスタムアクションがないため、提供されません。アプリをブラウザーで動かす場合は、同じコマンドを別の方法（ボタンや `Menu`）でも用意してください。

`List` の `SwipeActions` は、自動でスクリーンリーダーに提供されます。UIKit のリストは自分でスワイプアクションを VoiceOver に提供し、Android では Asuka がそれを TalkBack のアクションとして行に追加します。`Actions` で重ねて並べないでください。

```go
ui.List(ui.ListProps{
	SwipeActions: func(i int) []ui.Action {
		return []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
		}
	},
}, items, key, row)
```

`Menu` のアクションや、`Menu` を持つボタンはプラットフォームのメニューなので、そのままでアクセシブルです。

## 調整可能なビュー

`OnAdjust` は、スライダーのようにビューを調整可能にします。VoiceOver の上下スワイプや TalkBack の同様の操作（または音量キー）で、値が 1 または -1 ずつ変わります。値を選ぶ独自のコントロールに使ってください。`Slider` や `Stepper` は最初から調整可能です。

Mac では、ビューはインクリメンター（`AXIncrementor`）になり、VoiceOver では VO-上矢印と VO-下矢印で値を変えます。Windows、Linux、Web ではタブストップになり、矢印キーで値を変えます。上と右で 1、下と左で -1 です（Windows と Linux では、右から左に書く言語のとき左右が入れ替わります）。Linux と Web では slider ロールを持ちます。

```go
func Rating() ui.Node {
	stars := signal.New(3)
	var icons []ui.Node
	for i := range 5 {
		icons = append(icons, ui.Symbol(ui.IconStar.Filled(), ui.StyleFunc(func() ui.Style {
			if i < stars.Get() {
				return ui.Style{Color: colors.Orange}
			}
			return ui.Style{Color: colors.TertiaryLabel}
		})))
	}
	return ui.View(ui.ViewProps{
		OnAdjust: func(by int) { stars.Set(min(max(stars.Peek()+by, 0), 5)) },
		Style:    ui.Style{Direction: ui.RowDir, Gap: 4, Align: ui.AlignCenter},
	}, append(icons, ui.Text(signal.Func(func() string { return fmt.Sprintf("Rating: %d of 5", stars.Get()) })))...)
}
```

このビューは中のテキストとして読み上げられます。テキストは現在の値を伝え、値を変えるたびに変わります。`Label` は付けていません。付けるとテキストの代わりにラベルが読み上げられ、しかもラベルは変えられないからです。値の範囲は自分で制限してください。スクリーンリーダーは限界を超えても調整を求め続けます。

## 読み上げ

`ui.Announce` は、スクリーンリーダーがオンになっていれば、テキストを読み上げさせます。フォーカスのない場所で起きた変化を伝えるためのものです。たとえばトースト、検索結果の件数、完了した保存などです。

```go
ui.Animate(func() { shown.Set(true) })
ui.Announce("Saved") // スクリーンリーダーはトーストに気づかない
```

iOS では `UIAccessibility` の読み上げ通知（announcement）、Android では `announceForAccessibility`、macOS では読み上げのリクエスト（announcement request）、Windows では UI Automation の通知、Linux では GTK の読み上げ（GTK 4.14 以降）、Web では polite の `aria-live` リージョンになります。スクリーンリーダーがオフなら何もしません。フォーカスのあるビューがすでに伝えている内容は読み上げさせないでください。

## フォーカスの移動

`ui.AccessibilityFocus(node)` は、スクリーンリーダーのフォーカスをビューに移します。ユーザーの目の前で画面が変わり、大事なものが別の場所にあるときに使います。送信に失敗したフォームの下に出るエラー、検索の最初の結果、スライドインしてきたパネルのタイトルなどです。

```go
errText := ui.Text(message)
// ...
OnPress: func() {
	if err := signIn(); err != nil {
		message.Set(err.Error())
		ui.AccessibilityFocus(errText)
	}
},
```

フォーカスの移動は、ビューが作られてフレームが決まったあと、次のフラッシュで送られます。iOS ではビューを指定した `UIAccessibility` の画面変更通知（screen-changed notification）、Android では `ACTION_ACCESSIBILITY_FOCUS`、macOS ではビューを指定したレイアウト変更の通知、Windows ではフォーカスイベントになります。自分のビューを持たないコンテナーでは、その中の最初のビューにフォーカスが移ります。この四つのプラットフォームでは、スクリーンリーダーがオフなら何もせず、キーボードのフォーカスを動かすこともありません（それは `ui.Focus` の役目です）。

Linux と Web では、キーボードのフォーカスをビューに移し、スクリーンリーダーはそれを追いかけます。GTK とページには、スクリーンリーダーにビューを指し示すほかの方法がないためです。

## スクリーンリーダーが有効かどうか

`ui.UseEnvironment()` の `ScreenReader` は、スクリーンリーダーがオンのあいだ true になり、オンとオフの切り替えに追従します。

- iOS と macOS：VoiceOver。
- Android：TalkBack。
- Windows：システムのスクリーンリーダーフラグ。ナレーターやほかのスクリーンリーダーが設定します。
- Linux：GNOME のスクリーンリーダーの設定（Orca）。
- Web：常に false です。ブラウザーはスクリーンリーダーがオンかどうかを教えてくれません。

これは、耳で使うと勝手が違うものを調整するために使い、何かを隠すためには使わないでください。たとえば、トーストを 3 秒で消すのではなくユーザーが閉じるまで表示しておく、自動で進むカルーセルを操作を待つものに変える、といった使い方です。

## テキストサイズ

文字はコードを書かなくても拡大縮小されるので、残る課題は、大きなサイズでも崩れないレイアウトです。

- テキストを含むものの高さを固定しないでください。伸びるようにしておき、最小値が必要なら `MinHeight` を使います。
- 重要なテキストに `Lines: 1` を使うのは避けてください。大きなサイズでは途中で切れてしまいます。
- 長い行は `Scroll` や `List` に入れてください。

`ui.UseEnvironment()` の `FontScale` はユーザーの文字サイズで、デフォルトが 1 です。Dynamic Type の「大」（Large）、Android のデフォルトのフォントサイズ、Windows の 100%（「設定」>「アクセシビリティ」>「テキストのサイズ」）、GNOME の拡大率 1、ブラウザーのデフォルトの文字サイズ（16 CSS ピクセル）がこれにあたります。Mac では常に 1 です。`StyleFunc` の中で読めば、大きなサイズのときにレイアウトを変えられます。iOS の純正アプリが、行のラベルと値を縦に積むのと同じです。

```go
func Adaptive() ui.Node {
	env := ui.UseEnvironment()
	return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		if env.Get().FontScale > 1.5 {
			return ui.Style{Direction: ui.ColumnDir, Gap: 4}
		}
		return ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}
	})},
		ui.StaticText("Wi-Fi", ui.Style{Grow: 1}),
		ui.StaticText("Home", ui.Style{Color: colors.SecondaryLabel}),
	)
}
```

## 視差効果を減らす

`ui.UseEnvironment()` の `ReduceMotion` は、iOS と macOS の「視差効果を減らす」、Android の「アニメーションを削除」、Windows の「アニメーション効果」のオフ、GNOME のアニメーションのオフ、ブラウザーの `prefers-reduced-motion` のいずれかを表します。iOS では、Asuka はトランジションを動きではなくディゾルブにします。それ以外のプラットフォームでは、Asuka のアニメーションはすぐに終わります。開発者に任されている部分については[モーションとジェスチャー](motion.md)を参照してください。

## キーボード

Windows、Linux、Web では、Asuka のビューはプラットフォーム純正のコントロールと同じようにキーボードで操作できます。

- **Tab** で、コントロールと押せるビューのあいだをビューの順に移動します。Shift+Tab で戻ります。
- **Enter** と **Space** で、フォーカスのある押せるビューをボタンと同じように押せます。
- フォーカスのあるビューにはフォーカスリングが表示されます。Windows では Fluent のもの、Linux ではアクセントカラーの輪郭線、Web ではブラウザーのものです。
- `Actions` か `OnAdjust` を持つビューはタブストップになります。コンテキストメニューは Shift+F10 かアプリケーションキーで開き（Windows、Linux）、矢印キーで値を変えます。

`ui.Focus` と `ui.Blur` は、キーボードのフォーカスをフィールドに移したり、外したりします（[コンポーネント](components.md)を参照）。

## スクリーンリーダーでのテスト

スクリーンリーダーをオンにして、できれば画面を見ずに、すべての画面を試してください。すべてにたどり着けるか、止まるたびにそれが何かわかるか、スワイプやドラッグのすべてにアクションがあるかを確かめます。デスクトップでは、キーボードだけでも試してください。

- **VoiceOver** は iPhone で動きます。「設定」>「アクセシビリティ」>「VoiceOver」で設定します。「ショートカット」（サイドボタンのトリプルクリック）を設定しておくと、オンとオフを切り替えられます。シミュレーターには VoiceOver がありません。シミュレーターでは、Xcode の Accessibility Inspector（Xcode > Open Developer Tool）で各要素のラベル、トレイト、アクションを確認できます。
- **TalkBack** は、Android のスマートフォンか Google Play 入りのエミュレーターで、Android ユーザー補助設定ツール（Android Accessibility Suite）から使います。「設定」>「ユーザー補助」>「TalkBack」で設定します。
- **Mac の VoiceOver**：Command-F5 を押すか、「システム設定」>「アクセシビリティ」>「VoiceOver」で設定します。Accessibility Inspector（Xcode > Open Developer Tool）で macOS アプリの要素を直接調べられます。
- **Windows のナレーター**：Ctrl+Windows+Enter で起動します。NVDA は無料でダウンロードできます。Accessibility Insights for Windows では、各要素の UI Automation の名前、コントロールタイプ、パターンを確認できます。
- **Linux の Orca**：GNOME では Super+Alt+S を押すか、「設定」>「アクセシビリティ」>「スクリーンリーダー」で設定します。Accerciser で AT-SPI のツリーを確認できます。
- **Web**：ブラウザーの開発者ツールでアクセシビリティツリーを確認できます（Chrome と Firefox の Accessibility パネル）。Safari では VoiceOver を、Chrome や Firefox では NVDA を試してください。
- **文字サイズ**：iOS では「設定」>「アクセシビリティ」>「画面表示とテキストサイズ」>「さらに大きな文字」、シミュレーターでは Xcode の Environment Overrides を使います。Android では「設定」>「ディスプレイ」>「フォントサイズ」、Windows では「設定」>「アクセシビリティ」>「テキストのサイズ」、GNOME では「設定」>「アクセシビリティ」>「大きな文字」（Large Text）、そしてブラウザーの文字サイズです。最大のサイズで試してください。
- **視差効果を減らす**：iOS では「設定」>「アクセシビリティ」>「動作」、Android では「設定」>「ユーザー補助」>「アニメーションを削除」、macOS では「システム設定」>「アクセシビリティ」>「ディスプレイ」>「視差効果を減らす」、Windows では「設定」>「アクセシビリティ」>「視覚効果」>「アニメーション効果」、GNOME では「設定」>「アクセシビリティ」>「アニメーションを減らす」（Reduce Animation）です。ブラウザーではシステムの設定に従います。

`asuka dev` は、これらの設定を変えても動き続けます。アプリは設定が変わるたびにそれに追従します。

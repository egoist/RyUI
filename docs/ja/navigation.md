# ナビゲーション

アプリの骨組みは、画面のスタック、タブ、シート、アラート、メニュー、そしてデスクトップではウィンドウとメニューバーでできています。どれも各プラットフォーム純正のものです。

- **iOS**：`UINavigationController`、`UITabBarController`、シートの表示（sheet presentation）、`UIAlertController`、`UIMenu`。
- **Android**：Material のトランジションと予測型「戻る」に対応したトップアプリバー、Material のボトムナビゲーションバー、`BottomSheetDialog`、Material のダイアログ、`PopupMenu`。
- **macOS**：ウィンドウのツールバー（戻るボタン、画面のバーボタンと検索フィールド）とタイトル、`NSTabView`、シートウィンドウ、`NSAlert`、`NSMenu`、メニューバー。
- **Windows**：画面の上に置くヘッダーバー、WinUI の `NavigationView` を上部に置いた形のタブバー、モーダルウィンドウ、タスクダイアログ、ポップアップメニュー、ウィンドウのメニューバー。いずれも Windows 11 の見た目で描きます。
- **Linux**：libadwaita のものです。ウィンドウのヘッダーバー、`AdwNavigationView`、ビュースイッチャー、`AdwDialog`、`AdwAlertDialog`、GTK のポップオーバーメニュー、メインメニュー。
- **Web**：ホストが描くヘッダーバー、`<dialog>` とポップオーバー、ブラウザーの戻るボタン（[Web](web.md) を参照）。

このページでは、これらに加えて「戻る」操作と、アプリを開くリンクや共有についても説明します。[`examples/navigation`](../../examples/navigation) にはこれらがすべて入っています。メニューバーとウィンドウの例は [`examples/notes`](../../examples/notes) にあります。

## スタック

`ui.Stack` はナビゲーションスタックです。ルート画面から始まり、その中のコンポーネントは、`ui.UseNavigator` で得たナビゲーターを使って画面を積み重ねて（push して）いきます。

```go
func App() ui.Node {
	return ui.Stack(Notes, ui.ScreenOptions{Title: ui.Val("Notes")})
}

func Notes() ui.Node {
	nav := ui.UseNavigator()
	return ui.List(ui.ListProps{}, notes, func(n note) int { return n.ID },
		func(n signal.Accessor[note], _ signal.Accessor[int]) ui.Node {
			return ui.View(ui.ViewProps{
				OnPress: func() {
					v := n.Get() // ハンドラーの中なので追跡されない
					nav.Push(func() ui.Node { return Detail(v) }, ui.ScreenOptions{Title: ui.Val(v.Text)})
				},
				Style: ui.Style{Padding: ui.Symmetric(12, 20)},
			}, ui.Text(signal.Func(func() string { return n.Get().Text })))
		})
}

func Detail(n note) ui.Node {
	nav := ui.UseNavigator()
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
		ui.StaticText(n.Text),
		ui.Button(ui.ButtonProps{Title: ui.Val("Done"), OnPress: nav.Pop}),
	)
}
```

- `nav.Push(screen, options)` は、プラットフォームの push アニメーションで画面を一番上に表示します。
- `nav.Pop()` は一番上の画面を取り除きます。ルート画面が pop されることはありません。
- `nav.Depth()` は、スタックにある画面の数です。

`ui.UseNavigator` は、上の例のようにコンポーネントを組み立てている間に呼び出し、得たナビゲーターをハンドラーで使うために保持しておきます。スタックの外で呼ぶと panic します。

iOS と Android では、スタックごとに専用のバーがあります。Windows（WinUI と同じように、ホストが画面の上に描くヘッダーバー）と Web でも同様です。一方、Mac と Linux では、それぞれのネイティブアプリと同じくウィンドウにバーは一つだけで、そのバーは画面上でもっとも内側にあるスタック（タブの中なら、表示中のタブのスタック）のものになります。つまり、そのスタックの戻るボタンと、一番上の画面のタイトル、バーボタン、検索フィールドが表示されます。Mac ではこれがウィンドウのツールバーとタイトルに、Linux ではウィンドウの `AdwHeaderBar` になります。push と pop は macOS、Linux、Web ではスライドしますが、Windows ではトランジションなしで画面が切り替わります。

スタックは与えられた領域いっぱいに広がります。各画面は [オーナー](concepts.md) です。コードからでもユーザーの戻るジェスチャーでも、pop された画面は破棄され、そのエフェクトも止まります。もう一度 push された画面はまっさらな状態から始まるので、画面より長く残したいものは、画面の外にあるシグナルか [ストレージ](data.md) に置いてください。

## 画面のオプション

`ui.ScreenOptions` はスタックの画面を設定します。ルート画面の分は `Stack` に、それ以外の画面の分は `Push` に渡します。

- `Title` はナビゲーションバーのタイトルです（Android ではトップアプリバーの、Windows・Linux・Web ではヘッダーバーのタイトルです。Mac ではウィンドウのタイトルになり、ブラウザーではページのタイトルにもなります）。アクセサなので、値を変えられます。
- `Actions` はバーのボタンです。
- `NoBackGesture` は、その画面での戻るジェスチャーを無効にします（[戻る操作](#戻る操作) を参照）。
- `Search` は、バーに検索フィールドを置きます。
- `ZoomFrom` は、下の画面にあるビューからズームして画面を表示します。
- `LargeTitle` は、iOS で画面上部にタイトルを大きく表示し、スクロールに合わせてバーの中へ縮めます。Android のトップアプリバー、デスクトップのバー、Web のバーは変わりません。バーが追従するのは画面の大部分を占める最初のスクロールビューなので、その背後に別のものがあってもかまいません。
- `BarColor` は、Android ではトップアプリバーの色、Web ではヘッダーの色です。画面が自分で描く背景をバーまでつなげるために使います。iOS のバーは画面の最上部ではもともと透明で、デスクトップのバーはそれぞれ自分の色を保ちます。
- `Toolbar` は画面下部のバーに置くボタン、`Primary` は画面の主要なアクションです（[ツールバーと主要アクション](#ツールバーと主要アクション) を参照）。`PrimaryExtended` を指定すると、`Primary` は Android の拡張フローティングアクションボタンになります。

### バーボタン

各アクションは `ui.Action` です。`Icon` を指定すると、バーにはタイトルの代わりにアイコンが表示され、スクリーンリーダーはタイトルを読み上げます（デスクトップでは、タイトルはアイコンのツールチップにもなります）。

```go
nav.Push(func() ui.Node { return Detail(n) }, ui.ScreenOptions{
	Title: ui.Val(n.Text),
	Actions: []ui.Action{
		{Title: ui.Val("Share"), Icon: ui.IconShare, OnPress: func() { ui.Share(n.Text) }},
		{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() { nav.Pop() }},
	},
})
```

Android では、アクションは入りきる分だけトップアプリバーに並び、残りはオーバーフローメニューに入ります。アイコンを長押しするとタイトルが表示されます。Mac ではツールバーの項目に、Windows ではヘッダーバーの末尾のボタンに、Linux ではヘッダーバーのボタンになります。

### ツールバーと主要アクション

`Toolbar` は、SwiftUI の `ToolbarItem(placement: .bottomBar)` と同じように、画面下部のバーにボタンを置きます。`Primary` は新規作成や追加のような画面の主要なアクションで、Material のフローティングアクションボタンにあたります。どちらもバーボタンと同じく `ui.Action` です。`Icon` はボタンに表示するもの、`Title` はスクリーンリーダーが読み上げる内容です。`Disabled` を指定するとボタンが淡色表示になり、`Children` を指定するとメニューを開くボタンになります。アクセサの値の変化にも追従します。

```go
nav.Push(func() ui.Node { return Message(m) }, ui.ScreenOptions{
	Title: ui.Val(m.From),
	Toolbar: []ui.Action{
		{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: remove},
		{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", Children: []ui.Action{
			{Title: ui.Val("Reply"), OnPress: reply},
			{Title: ui.Val("Forward"), OnPress: forward},
		}},
		{Title: ui.Val("Mark as unread"), Icon: "envelope", Disabled: unread},
	},
	Primary: &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: compose},
})
```

- iOS：ナビゲーションコントローラーのツールバーです。ボタンの間には可変スペースが入り、ツールバーは画面が一番上にある間だけ表示され、push や pop に合わせてスライドします。iOS にはフローティングボタンがないので、`Primary` はツールバーの末尾の項目になり、iOS 26 では強調表示されます（「メール」の新規作成ボタンと同じです）。`Primary` しかない画面にも、そのためのツールバーが付きます。UIKit と同じく、ツールバーの分は画面のセーフエリアから差し引かれます。`Toolbar` を持つ画面を push すると、（iOS 18 以降）タブのタブバーが隠れます。「メール」でメッセージを開いたときと同じ動作で、iOS 26 のフローティングタブバーがツールバーの上に重なってしまうのを避けるためです。
- Android：Material 3 の `BottomAppBar` です。ボタンは先頭側からアイコンボタンとして並び、入りきらないものはオーバーフローメニューに入ります。`Primary` は画面の下端の末尾側に置く `FloatingActionButton` で、`Toolbar` があるときはバーにドッキングされます（影は付きません）。`PrimaryExtended` を指定すると `ExtendedFloatingActionButton` になり、アイコンの横にタイトルが表示されます。このボタンは画面の内容を下へスクロールするとアイコンだけに縮み、上へスクロールすると再び広がります（バーがあるときはバーの上に表示されます）。どちらのボタンも画面自身のものなので、push、pop、予測型「戻る」で画面と一緒に動きます。バーはその下にあるシステムのナビゲーションバーの領域まで広がり、画面のセーフエリアからはバーの分が差し引かれます。`Toolbar` を持つ画面を push すると、Material の規定どおり、タブのナビゲーションバーが隠れます。
- macOS：SwiftUI のボトムバーは iOS 向けのものなので、Mac では `Toolbar` のボタンをウィンドウのツールバーの中、バーボタンの後ろに表示します。`Primary` は最後に強調表示されます（macOS 26 の、アクセントカラーで色付けされたツールバー項目）。
- Windows：画面の下に置くコマンドバー（Fluent の `CommandBar`）です。ボタンはアイコンとタイトル付きで先頭側から並び、入りきらないものは「その他のオプション」（…）メニューに入ります。`Primary` は末尾側のアクセントボタンになります。
- Linux：`AdwToolbarView` の中で、ページの下部に置く `GtkActionBar`（GNOME のアクションバー）です。ボタンは先頭側に並び、`Primary` は末尾側に、アイコンとタイトル付きの `suggested-action` ボタンとして置かれます。
- Web：スタックの下部に置くバーです（入りきらないボタンは「その他のオプション」メニューに入ります）。`Primary` は下端の末尾側の隅に浮かぶボタンになります。拡張版のボタンはタイトルを表示し、内容を下へスクロールするとアイコンだけに折りたたまれます。

ボタンを押すと、そのアクションが画面に通知されます。テストでは、`host/fake` の `PressToolbar` と `PressPrimary` で同じ操作ができます。

### 検索

`Search` は、SwiftUI の `searchable` と同じように、バーに検索フィールドを置きます。ユーザーが入力した内容は `Text` に書き込まれ、コードから `Text` に書き込むとフィールドの内容が変わります。`Placeholder` を指定しなければ "Search" になります。`OnSubmit` は、キーボードの検索キー、デスクトップでは Enter キーで実行されます。

```go
query := signal.New("")
shown := signal.Func(func() []note {
	var out []note
	for _, n := range notes.Get() {
		if strings.Contains(strings.ToLower(n.Text), strings.ToLower(query.Get())) {
			out = append(out, n)
		}
	}
	return out
})
return ui.Stack(func() ui.Node {
	return ui.List(ui.ListProps{}, shown, func(n note) int { return n.ID },
		func(n signal.Accessor[note], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return n.Get().Text }), ui.Style{Padding: ui.Symmetric(12, 20)})
		})
}, ui.ScreenOptions{Title: ui.Val("Notes"), Search: &ui.Search{Text: query, Placeholder: ui.Val("Search notes")}})
```

iOS では、タイトルの下に `UISearchController` が置かれ、画面をスクロールしても表示されたままになります。Android では Material のパターンに従い、トップアプリバーの検索ボタンを押すとフィールドに展開します。画面の下部に独自のコントロールがあるときは、`Button: true` を指定すると iOS でも検索フィールドが邪魔になりません。iOS 26 ではバーのボタンになり、それより前のバージョンでは、内容を引き下げるまで内容の上に隠れています。

デスクトップと Web では、フィールドは画面のバーに置かれます。

- **macOS**：ツールバーの検索フィールド（`NSSearchToolbarItem`）。
- **Windows**：検索グリフ付きの Fluent のテキストボックス。WinUI がコマンドバーに検索ボックスを置くのと同じく、バーボタンの前に置かれます。
- **Linux**：ヘッダーバーの検索ボタンと、その下に開く GTK の検索バー。GNOME のアプリと同じ検索の仕方で、ウィンドウのどこで入力しても検索バーが開きます。
- **Web**：ヘッダーの下のフィールド（`Button: true` のときは、ヘッダー内のボタン）。

### ビューからのズーム

`ZoomFrom` には、下の画面にあるビュー（グリッド内の写真やカードなど）の `Style.Match` のキーを指定します。画面は push されるときにそのビューからズームして現れ、pop されるときにそのビューへ縮んで戻ります。[共有要素](motion.md#共有要素) を参照してください。

```go
key := fmt.Sprintf("photo-%d", id)
return ui.View(ui.ViewProps{
	Label: ui.Val("Photo"),
	Style: ui.Style{AspectRatio: 1, Match: key},
	OnPress: func() {
		nav.Push(func() ui.Node { return photo(id) }, ui.ScreenOptions{Title: ui.Val("Photo"), ZoomFrom: key})
	},
})
```

iOS では、システムのズームトランジションになります（iOS 18 以降。それより前のバージョンでは通常どおり push されます）。Android は独自のズームを行い、予測型「戻る」ジェスチャーで pop すると、画面がビューの中へ戻っていきます。デスクトップと Web では通常どおり push・pop します。[`examples/gallery`](../../examples/gallery) の写真グリッドがこの機能を使っています。

## 戻る操作

戻る方法はプラットフォームごとに異なり、Asuka はそれぞれのやり方をそのまま生かします。

- **iOS**：バーの戻るボタンと、画面の先頭側の端からのスワイプ。`NoBackGesture` はスワイプを無効にしますが、戻るボタンでは引き続き pop できます。
- **Android**：ボタンまたはジェスチャーによるシステムの「戻る」と、トップアプリバーの上矢印。「戻る」は、画面上でもっとも内側にあり、戻り先の画面を持つスタックを pop します。タブの中なら、表示中のタブのスタックです。予測型「戻る」ジェスチャーでは、一番上の画面が縮んで指に追従し、下の画面が見えてきます。pop できる画面を持つスタックがないときは、ほかのアプリと同じく、「戻る」でアプリを離れます。`NoBackGesture` は予測型「戻る」のアニメーションを無効にしますが、「戻る」で画面は引き続き pop されます。
- **macOS**：ツールバーの戻るボタンと、View ▸ Back（⌘[）。戻り先の画面がない間、このメニュー項目は無効になります。Esc キーでシートを閉じます。
- **Windows**：ヘッダーバーの戻るボタンと Alt+←。Alt+← は、押されたウィンドウのもっとも内側のスタックを pop します。Esc キーでシートを閉じます。
- **Linux**：libadwaita の操作です。ヘッダーバーの戻るボタン、Alt+←、Esc キー、タッチパッドやタッチスクリーンでのスワイプ。`NoBackGesture` はスワイプを無効にしますが、戻るボタンでは引き続き pop できます。
- **Web**：ヘッダーの戻るボタン、ブラウザーの戻るボタン（とスマートフォンの戻るジェスチャー）、Esc キー、Alt+←（Mac では ⌘[）、タッチスクリーンでの先頭側の端からのスワイプ。[Web](web.md) を参照してください。

戻るジェスチャーのない macOS と Windows では、`NoBackGesture` は何も変えません。ユーザーがどの方法で戻っても、`nav.Pop()` と同じく画面は破棄されます。シートやダイアログが開いているときは、「戻る」はまずそれを閉じます。

### 「戻る」を自分で処理する

`ui.OnBack` を使うと、ユーザーが戻ろうとしたときに、実際に戻るかどうかをコンポーネントが決められます。下書きを捨てる前に確認したり、画面自身が開いているもの（検索や展開したパネル）を先に閉じたりするために使います。ハンドラーは、「戻る」を自分で処理したときに true を返します。すべてのハンドラーが false を返したときは、そのまま戻ります。

```go
func editor() ui.Node {
	dirty := signal.New(false)
	nav := ui.UseNavigator()
	ui.OnBack(func() bool {
		if !dirty.Peek() {
			return false // 失うものはないので、画面はそのまま pop される
		}
		ui.Alert(ui.AlertProps{
			Title: "Discard changes?",
			Actions: []ui.Action{
				{Title: ui.Val("Keep editing"), Role: ui.ActionCancel},
				{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() {
					dirty.Set(false)
					nav.Pop()
				}},
			},
		})
		return true
	})
	// ...
}
```

ハンドラーは、コンポーネントが組み立てられた場所に属し、コンポーネントと一緒に消えます。

- **push された画面**：その画面の戻るボタン、Android の「戻る」、iOS の端からのスワイプ、Mac の ⌘[、Windows と Linux の Alt+←、ブラウザーの「戻る」。画面にハンドラーがある間、iOS と Linux ではその画面でのスワイプが無効になり（UIKit の純正アプリと同じく、スワイプは問い合わせを挟めず、止めることしかできないためです）、Android では pop のプレビューを表示しません。iOS の戻るボタンは、画面の `backAction` です。
- **シート**：下へのスワイプ、外側のタップ、Android の「戻る」、デスクトップやブラウザーでの Esc キー、Windows ではシートウィンドウの閉じるボタン。フルスクリーンのシートでは、Android の「戻る」と Esc キーだけです。ハンドラーがある間は、シートをスワイプで閉じられません。閉じようとするとハンドラーに問い合わせます（iOS の `isModalInPresentation`、Android ではキャンセル不可のシート）。ハンドラーが閉じることを許すと、ユーザーが閉じたときと同じようにシートが閉じて、`OnDismiss` が実行されます。
- **それ以外**（画面やシートの外、またはスタックの最初の画面。最初の画面では「戻る」でスタックを離れます）：pop する画面がないときの Android の「戻る」（本来ならアプリが閉じるところ）と、ブラウザーの「戻る」（本来ならページが前に戻るところ）。iOS とデスクトップには、このような「戻る」はありません。

問い合わせは、最後に登録されたハンドラーから順に行われます。`nav.Pop()` やコードからシートを閉じる場合は問い合わせません。問い合わせるのは、ユーザーが戻ろうとしたときだけです。

## 分割ビュー

`ui.SplitView` は、行き先（destination）が多いアプリの最上位に置くものです。行き先を並べたサイドバーと、その横に選んだ行き先の画面を表示します（SwiftUI の `NavigationSplitView`）。

```go
ui.SplitView(ui.SplitViewProps{Title: ui.Val("Mail"), Sections: []ui.SidebarSection{
	{Destinations: []ui.Destination{
		{Title: ui.Val("Inbox"), Icon: ui.IconMail, Content: inbox},
		{Title: ui.Val("Sent"), Icon: "paperplane", Content: sent},
	}},
	{Title: ui.Val("Folders"), Destinations: folders},
}})
```

各行き先の `Content` はスタックの最初の画面です。その中では `ui.UseNavigator` で push できます。別の行き先を選ぶと、スタックの画面はすぐにその行き先の最初の画面に置き換わり、元の画面は破棄されます。`Selected` は選ばれている行き先で、セクションをまたいで通しで数えます。ユーザーが選ぶと書き込まれ、書き込むと別の行き先が表示されます。

| | コンパクト（スマートフォン） | 幅広 |
|---|---|---|
| iOS | iOS の流儀どおり、スタックに折りたたまれた分割ビュー。サイドバーが最初の画面（インセットグループ化されたリスト）になり、行をタップするとその行き先が push され、「戻る」でリストに戻ります | 内容の横に置く `UISplitViewController` のサイドバー（iOS 26 では Liquid Glass）。ユーザーが隠すこともできます |
| Android | Material のモーダルナビゲーションドロワー。行き先の最初の画面のメニューボタン（または端からのスワイプ）で開き、行き先を選ぶか「戻る」で閉じます | 内容の横に開いたままの標準ドロワー（幅 840dp 以上） |
| macOS | | `NSSplitViewController`。サイドバーは、ユーザーが折りたためるソースリストです |
| Windows | | WinUI の `NavigationView` のペイン。行き先が内容の横に並び、選択中のものにはアクセントカラーのピルが付きます |
| Linux | 幅 500 ポイント未満では、libadwaita の折りたたまれた分割ビュー。まずサイドバーが表示され、次に選んだ行き先の画面が、サイドバーへの戻るボタン付きで表示されます | `AdwNavigationSplitView`。サイドバーは独立したページで、そのタイトルは専用のヘッダーバーに表示されます。ウィンドウのヘッダーバーの下にある内容の横に並びます |
| Web | 詳細のヘッダーから開くドロワー | 詳細の横のサイドバー |

行き先の `Badge` は、行の末尾に表示されます。「メール」がメールボックスの未読数を表示するのと同じです（SwiftUI のサイドバー行の `badge`）。普通は数を表示しますが、短い文字列なら何でもかまいません。空文字列か nil なら、バッジは表示されません。アクセサなのでデータに追従させられます。行はその場で更新されるので、サイドバーの選択、スクロール位置、フォーカスは保たれます。

```go
{Title: ui.Val("Inbox"), Icon: ui.IconMail, Content: inbox,
	Badge: signal.Func(func() string { return strconv.Itoa(unread.Get()) })},
```

| | 行き先のバッジ |
|---|---|
| iOS | セルの末尾に置くラベルのアクセサリー（セカンダリーカラー） |
| Android | ドロワー項目の末尾に置くカウンター（Gmail と同じ） |
| macOS | ソースリストの件数表示（セカンダリーカラー）。「メール」のメールボックスと同じです |
| Windows | ペイン項目の末尾に置く Fluent の `InfoBadge` |
| Linux | 行の末尾に置く淡色の数字 |
| Web | 行の末尾に置く数字 |

スクリーンリーダーは、バッジを行のタイトルと一緒に読み上げます。数は項目数として読まれ、たとえば "Inbox, 3 items" のようになります（Android では Material のバッジの文言で、"3 new notifications" となります）。

## タブ

`ui.Tabs` は、タブバーを付けて一度に一つのページを表示します。

- **iOS**：`UITabBarController`。
- **Android**：Material のボトムナビゲーションバー。
- **macOS**：上部にタブを並べた `NSTabView`。
- **Windows**：WinUI の上部 `NavigationView` と同じ、上部に並ぶタブのバー。各タブにはアイコンとタイトルがあり、選択中のタブの下にはアクセントカラーのピルが付きます。矢印キーでタブ間を移動できます。
- **Linux**：`AdwViewStack`。GNOME のアプリと同じく、幅の広いウィンドウではヘッダーバーにビュースイッチャーを置き、幅の狭いウィンドウ（550 ポイント未満）では下部のバーにします。
- **Web**：下部のタブバー。

各 `ui.Tab` は、`Title`、`Icon`、`Content` を持ちます。`Selected` は表示中のタブのインデックスです。タブを選ぶと書き込まれ、書き込むとタブが切り替わります。

```go
tab := signal.New(0)
return ui.Tabs(ui.TabsProps{Selected: tab},
	ui.Tab{Title: ui.Val("Notes"), Icon: ui.IconList, Content: func() ui.Node {
		return ui.Stack(Notes, ui.ScreenOptions{Title: ui.Val("Notes")})
	}},
	ui.Tab{Title: ui.Val("Settings"), Icon: ui.IconSettings, Content: func() ui.Node {
		return ui.Stack(Settings, ui.ScreenOptions{Title: ui.Val("Settings")})
	}},
)
```

タブの `Badge` は、タブの上に表示されます（SwiftUI のタブ項目の `badge`、Compose の `NavigationBarItem` に付ける `BadgedBox`）。普通は "3" のような数ですが、短い文字列なら何でもかまいません。空文字列か nil なら、バッジは表示されません。アクセサなので、値が変わると、そのタブだけが更新されます。

```go
ui.Tab{Title: ui.Val("Inbox"), Icon: ui.IconMail, Content: inbox,
	Badge: signal.Func(func() string {
		if n := unread.Get(); n > 0 {
			return strconv.Itoa(n)
		}
		return ""
	})},
```

| | タブのバッジ |
|---|---|
| iOS | `UITabBarItem` の `badgeValue`。システムの赤いバッジです |
| Android | 項目の `BadgeDrawable`。数はその数字（999 を超えると 999+）で、それ以外の文字列はそのまま表示します |
| macOS | タブのラベルの中に "Inbox (3)" のように表示します。`NSTabView` はタブのラベルをすべて自分で描くためです |
| Windows | タブの末尾に置く Fluent の `InfoBadge` |
| Linux | 数はページの `badge-number` として、ビュースイッチャーのアイコンの横に表示します。それ以外の文字列は `needs-attention` のドットになります |
| Web | タブのアイコンに付く赤いバッジ |

スクリーンリーダーは、バッジをタブのタイトルと一緒に読み上げます。数は項目数として読まれ、たとえば "Inbox, 3 items" のようになります（iOS と Android では、プラットフォーム自身の文言になります）。

タブにはそれぞれ専用の `Stack` を持たせてください。そうすれば、各プラットフォームのアプリと同じく、タブごとにバーがあり、それぞれの履歴が保たれます。すべてのタブの内容は `Tabs` を組み立てるときに一緒に組み立てられ、タブを切り替えても保持されます。タブのアイコンは、ボタンと同じくシンボルです。[シンボルとアイコン](components.md#シンボルとアイコン) を参照してください。Android では、Android に用意されているアイコンでなければなりません。

## シート

`ui.Sheet` は、`Open` が true の間、内容をモーダルシートで表示します。

- **iOS**：detent 付きのシート表示。
- **Android**：`BottomSheetDialog`。
- **macOS**：使用中のウィンドウに付くシートウィンドウ。
- **Windows**：使用中のウィンドウの上に重ねるモーダルウィンドウ。その間、元のウィンドウは無効になります。
- **Linux**：`AdwDialog`。フローティングのダイアログで、幅の狭いウィンドウではボトムシートになります。
- **Web**：モーダルな `<dialog>`。幅の狭いウィンドウではボトムシート、幅の広いウィンドウでは中央に置くカードになります。

`ui.Sheet` が返すノードは領域を取らないので、シートを開く画面のどこに置いてもかまいません。

```go
open := signal.New(false)
note := signal.New("")
return ui.Column(nil,
	ui.Button(ui.ButtonProps{Title: ui.Val("Add a note"), OnPress: func() { open.Set(true) }}),
	ui.Sheet(ui.SheetProps{
		Open:    open,
		Detents: ui.DetentMedium | ui.DetentLarge,
		Grabber: true,
		Content: func() ui.Node {
			draft := signal.New(note.Peek())
			return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
				ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Write a note")}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
					note.Set(draft.Peek())
					open.Set(false)
				}}),
			)
		},
		OnDismiss: func() { fmt.Println("dismissed, not saved") },
	}),
)
```

- `Detents` は、シートが止まれる高さです。`ui.DetentMedium`（画面の半分）、`ui.DetentLarge`（画面全体）、またはその両方を指定します。両方のときは medium で始まり、ユーザーが上へドラッグして広げられます。何も指定しなければ large です。キーボードが表示されると、large になれるシートは large になります。デスクトップでは、medium だけのシートはウィンドウの高さの半分、それ以外のシートはウィンドウの高さの大部分になります。Mac では、ユーザーがシートウィンドウのサイズを変えられます。
- `Grabber` は、シートの上端にハンドルを表示します（iOS、Android、Web のボトムシート）。
- `Content` は、シートが開くときに組み立てられ、閉じるときに破棄されます。そのため、中で作った状態は毎回まっさらな状態から始まります。

### フルスクリーンのシート

`FullScreen` を指定すると、シートは代わりに画面全体を覆います。メッセージの作成、サインイン、写真の編集のように、終わるか諦めるまでユーザーの注意を引きつけておく作業のためのものです（SwiftUI の `fullScreenCover`、Material のフルスクリーンダイアログ）。`Detents` と `Grabber` は適用されず、外側をタップしたりスワイプしたりして閉じることもできません。`Open` が false になったときに閉じるので、専用の「キャンセル」と「完了」を用意してください。Android の「戻る」と、デスクトップやブラウザーでの Esc キーは、通常のシートと同じくこのシートも閉じます。ただし、`ui.OnBack` が引き止めた場合（先に破棄を確認すべき下書きがあるときなど）は閉じません。

```go
ui.Sheet(ui.SheetProps{Open: composing, FullScreen: true, Content: func() ui.Node {
	draft := signal.New("")
	ui.OnBack(func() bool { return draft.Peek() != "" }) // 「戻る」で下書きが失われないようにする
	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Row(ui.Style{Align: ui.AlignCenter},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), Variant: ui.ButtonPlain, OnPress: func() { composing.Set(false) }}),
			ui.StaticText("New Message", ui.Style{Grow: 1, TextAlign: ui.TextCenter}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Send"), Variant: ui.ButtonFilled, OnPress: send})),
		ui.TextField(ui.TextFieldProps{Value: draft, Multiline: true, Style: ui.Style{Grow: 1}}),
	)
}})
```

フルスクリーンのシートの環境は、画面そのものの環境です。スマートフォンでは画面全体の大きさを持ち、ステータスバー、ノッチ、ホームインジケーターまたはナビゲーションバー、キーボードのセーフエリアも持ちます。そのため `SafeArea` は、ルートと同じように、内容がそれらに重ならないように配置します。

| | 実体 |
|---|---|
| iOS | `.fullScreen` で表示するコントローラー。すべてのものの上にスライドして現れ、閉じるまで下の画面は見えなくなります |
| Android | 独自のテーマを持つ全画面の `Dialog`。エッジ・ツー・エッジで表示し（ステータスバーとナビゲーションバーのインセット、ディスプレイカットアウト、キーボードに対応）、Material の強調イージング（emphasized easing）でスライドして現れます。予測型「戻る」では、スワイプした側の端へ向かって縮み、角が丸くなります（`OnBack` が横取りした場合を除きます） |
| macOS | ウィンドウのタイトルバーとツールバーの下にある内容全体と同じ大きさのシート。ウィンドウのサイズ変更に追従し、ユーザーがシートのサイズを変えることはできません。Mac にはフルスクリーンのモーダルがなく、AppKit でウィンドウを一つの作業に専念させるにはシートを使うためです |
| Windows | ウィンドウの内容全体を覆う枠なしのウィンドウ（タイトルバーは元のウィンドウのもので、その間、元のウィンドウは無効になります）。元のウィンドウの移動やサイズ変更に追従し、Windows 11 がウィンドウの角を丸めるのと同じく、下の角が丸くなります。Fluent のフルサイズの `ContentDialog` にあたります |
| Linux | ウィンドウと同じ大きさのフローティングの `AdwDialog`。libadwaita は、収まりきらないダイアログを（スマートフォンや幅の狭いウィンドウで）ウィンドウ全体に重ねて表示しますが、それと同じようにウィンドウ全体を覆って表示されます。ウィンドウのサイズ変更に追従します |
| Web | ページ全体を覆うモーダルな `<dialog>`（フォーカスはその中にとどまり、Esc キーとブラウザーの「戻る」で閉じます）。Window パネルの中で宣言されたシートでは、そのパネルの内容を覆います。スライドして現れます |

ユーザーは、シートを下へスワイプして閉じられます。Android では「戻る」やシートの外側のタップで、デスクトップやブラウザーでは Esc キーでも閉じられます（Windows ではシートウィンドウの閉じるボタン、Web では外側のクリックでも閉じます）。このとき `Open` は false になり、`OnDismiss` が呼ばれます。コードから `Open` を false にした場合は、`OnDismiss` を呼ばずに閉じます。つまり `OnDismiss` は「アプリの指示によらずに閉じられた」ことを表し、保存されないまま残った下書きの処理などに使います。

## ポップオーバー

`ui.Popover` は、`Open` が true の間、ビューを指す吹き出しの中に内容を表示し、画面の上に重ねます。いくつかの選択肢、詳しい情報、小さなフォームなどに使います。子要素が、吹き出しの指す対象になります。

```go
open := signal.New(false)
return ui.Popover(ui.PopoverProps{Open: open, MaxWidth: 280, Content: func() ui.Node {
	return ui.Column(ui.Style{Padding: ui.All(16), Gap: 8},
		ui.StaticText("Shared with 3 people", ui.Style{Font: fonts.Headline}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Manage"), OnPress: manage}))
}}, ui.Button(ui.ButtonProps{Icon: "person.2", Label: ui.Val("Sharing"), OnPress: func() { open.Set(true) }}))
```

ポップオーバーの大きさは内容に合わせて決まりますが、幅は `MaxWidth`（既定は 320）を超えず、テキストはその幅で折り返します。高さも画面の大部分を超えることはありません。外側をタップすると閉じ、シートと同じく `Open` が false になって `OnDismiss` が呼ばれます。中で `ui.OnBack` を使えば、閉じるのを引き止められます。

| | ビュー | 位置 |
|---|---|---|
| iOS | `UIPopoverPresentationController`。iPhone でもポップオーバーのまま表示し（シートに変換しません）、iOS 26 では Liquid Glass になります | UIKit が収まる位置を決め、矢印がアンカーを指します |
| Android | Material のコンテナーサーフェス上の `PopupWindow`。角の丸みは 16dp で、エレベーションが付きます | アンカーの下に中央揃え（スペースがなければ上） |
| macOS | transient な `NSPopover` | アンカーの下、AppKit が収まる位置を決めます |
| Windows | ライトディスミスのポップアップウィンドウ。角は Windows 11 のものです（WinUI の Flyout） | アンカーの下に中央揃え（スペースがなければ上） |
| Linux | `GtkPopover`。Esc キーでも閉じます | アンカーの下、GTK がスペースを見つけた位置 |
| Web | ポップオーバー | アンカーの隣 |

## アラートとアクションシート

`ui.Alert` は、その場でアラートを表示します。

- **iOS**：`UIAlertController`。
- **Android**：Material のダイアログ。
- **macOS**：使用中のウィンドウにシートとして表示する `NSAlert`。
- **Windows**：使用中のウィンドウの上に表示するタスクダイアログ。
- **Linux**：`AdwAlertDialog`。
- **Web**：モーダルな `<dialog>`。

ボタンを押すとアラートが閉じ、そのボタンの `OnPress` が実行されます。`Actions` がなければ、OK ボタンが一つだけ表示されます。

```go
ui.Alert(ui.AlertProps{
	Title:   "Delete this note?",
	Message: "You can't undo this.",
	Actions: []ui.Action{
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
		{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: remove},
	},
})
```

アクションの `Role` は、そのアクションの役割を表します。

- `ui.ActionDefault` は通常のボタンです。
- `ui.ActionCancel` は、操作をやめるためのボタンです。iOS では専用の位置に置かれ、Android ではダイアログのネガティブボタンになります。デスクトップでは Esc キーでこのボタンが押されます。Mac では、デフォルトボタンの後ろの一番最後のボタンになります。
- `ui.ActionDestructive` は赤で表示されます。iOS のスタイル、Android では Material のエラーカラー、Mac では AppKit の破壊的なボタン、Linux では libadwaita の destructive な応答になります。

Android では、キャンセル以外のボタンのうち最後のものがダイアログのポジティブボタンとして末尾に置かれ、もう一つあればそれはニュートラルボタンになります。キャンセル以外のボタンが 3 つ以上あると、ダイアログはそれらをリストとして表示します。Mac では、キャンセル以外の最初のボタンがデフォルトボタンになり、Return キーで押されます。Linux では、最初の `ui.ActionDefault` のボタンがデフォルトになり、アクセントカラーで表示されます。

`ActionSheet` を指定すると、アクションをアクションシートとして表示します。iOS では、画面の下から現れる選択肢のリストです。

```go
ui.Alert(ui.AlertProps{
	Title:       "Photo",
	ActionSheet: true,
	Actions: []ui.Action{
		{Title: ui.Val("Take a photo"), OnPress: func() {}},
		{Title: ui.Val("Choose from library"), OnPress: func() {}},
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
	},
})
```

Android では、アクションシートはアクションをリストとして並べたダイアログになり、`Message` は表示されません。Windows ではボタンがタスクダイアログのコマンドリンクになり、Linux ではデフォルトボタンのないアラートダイアログに、Mac ではほかと変わらないアラートに、Web ではボタンをリストとして並べたダイアログになります。Android の「戻る」や外側のタップなど、ボタン以外の方法でアラートが閉じられた場合は、どのボタンの `OnPress` も実行されません。

### テキストフィールド

SwiftUI のアラートに `TextField` や `SecureField` を入れるのと同じように、アラートで名前やパスワードなどのテキストを入力してもらうこともできます。`Fields` は、メッセージの下に表示されます。各フィールドの `Text` はその値を初期値とし、ユーザーが入力するたびに書き込まれるので、アクションの `Disabled` をそれに追従させられます。次の例では、両方のフィールドが埋まるまで「Sign In」が無効のままになり、アラートの表示中もボタンの状態が更新されます。

```go
user, pass := signal.New(""), signal.New("")
ui.Alert(ui.AlertProps{
	Title: "Sign in",
	Fields: []ui.AlertField{
		{Text: user, Placeholder: "Name", ContentType: ui.ContentUsername},
		{Text: pass, Placeholder: "Password", Secure: true, ContentType: ui.ContentPassword},
	},
	Actions: []ui.Action{
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
		{Title: ui.Val("Sign In"), OnPress: signIn,
			Disabled: signal.Func(func() bool { return user.Get() == "" || pass.Get() == "" })},
	},
})
```

フィールドは、`TextFieldProps` と同じく、`Keyboard` と自動入力（AutoFill）のための `ContentType` も受け取ります。`Secure` を指定すると、入力内容が隠されます。最初のフィールドにフォーカスがあり、キーボードが表示された状態で始まります。フィールドで Return を押すと、次のフィールドに移ります。最後のフィールドで Return を押すと、デフォルトのアクション、つまり最初の `ui.ActionDefault` のアクション（UIAlertController の `preferredAction`）が押されます。ただし、そのアクションが有効な間だけです。アラートの表示中にフィールドの `Text` に書き込んでも、フィールドの表示は変わりません。アクションシートには、フィールドは表示されません。

| プラットフォーム | フィールド |
| --- | --- |
| iOS | `UIAlertController` のテキストフィールド。デフォルトのアクションは `preferredAction` で、太字で表示されます |
| Android | Material のダイアログ内に置くアウトライン付きの `TextInputLayout`（パスワードには表示切り替えボタン付き）。IME のアクションで次のフィールドへ移り、最後のフィールドではデフォルトのアクションのボタンを押します |
| macOS | `NSAlert` のアクセサリービューとして置く `NSTextField` と `NSSecureTextField`。Tab キーでフィールド間を移動します |
| Windows | WinUI の `ContentDialog` と同じレイアウトで、ホストが自前で用意するダイアログ（タスクダイアログにはフィールドを置けないため）。タイトル、メッセージ、Fluent のテキストボックス、ボタンの順に並び、デフォルトのボタンはアクセントカラーになります。Tab、Enter、Esc キーは通常のダイアログと同じように動作し、ナレーターは各フィールドをプレースホルダーで読み上げます |
| Linux | `AdwEntryRow` と `AdwPasswordEntryRow` を並べた boxed list。`AdwAlertDialog` の extra child として置きます |
| Web | `<dialog>` 内の input 要素。プレースホルダーをラベルとし、`type=password`、`inputmode`、`autocomplete` を設定します |

## スナックバー

`ui.Snackbar` は、ウィンドウの下部に短いメッセージをその場で表示します。「元に戻す」のようなアクションを任意で付けられ、スナックバーを閉じる関数を返します。

- **iOS**：UIKit にはスナックバーがないので、iOS 自身が短いメッセージを表示するときと同じ見た目でホストが描きます。システムのマテリアル（iOS 26 では Liquid Glass）でできたカプセル形で、最前面に表示されているコンテンツの上に重ね、タブバーやツールバー、キーボードより上の位置に中央揃えで置かれます。スプリングで出入りし（「視差効果を減らす」がオンのときはフェード）、下へスワイプすると閉じます。
- **Android**：Material の `Snackbar`。スワイプで閉じられ、画面のボトムナビゲーションバーかフローティングアクションボタン（アンカー）とキーボードより上に表示されます。シートの上では、シートの下部に表示されます。
- **macOS**：ウィンドウ下部に置く、`NSVisualEffectView` によるオーバーレイ。閉じるボタン付きです。
- **Windows**：ウィンドウ下部に置く、Fluent の InfoBar として描くオーバーレイ。閉じるボタン付きです。
- **Linux**：ウィンドウの `AdwToastOverlay` 内の `AdwToast`。
- **Web**：ウィンドウ下部に置く、role が `status` の要素（最上位レイヤーに置くポップオーバー）。閉じるボタン付きで、下へスワイプしても閉じます。

```go
ui.Snackbar(ui.SnackbarProps{
	Message: "Note deleted",
	Action:  &ui.Action{Title: ui.Val("Undo"), OnPress: restore},
	OnDismiss: func() { deleteForGood() },
})
```

`Message` は一度表示するだけのものなので、アラートと同じく文字列です。アクションの `Title` は、表示中もアプリの言語に追従します。アクションの `Role`、`Icon`、`Disabled`、`Children` は表示されません。アクションを押すとスナックバーが閉じ、その `OnPress` が実行されます。`OnDismiss` は、それ以外の方法で消えたときに実行されます。つまり、時間切れになったとき、ユーザーがスワイプしたか閉じたとき、別のスナックバーに置き換えられたとき、`ui.Snackbar` が返した関数が呼ばれたときです。そのため、「元に戻す」が押された後は `OnDismiss` は実行されません。アクションで取り消せる処理を確定させる場所が `OnDismiss` です。

`Duration` は表示時間です。`ui.SnackbarShort`（既定。Material と同じく約 4 秒）、`ui.SnackbarLong`（約 10 秒）、`ui.SnackbarIndefinite`（閉じられるか、アクションが押されるまで）から選びます。ホストは、アクセシビリティのためにプラットフォームが推奨するとおりに表示時間を延ばします。Android では `AccessibilityManager.getRecommendedTimeoutMillis` に従い、TalkBack がオンの間は、アクション付きのスナックバーを長く表示します。推奨タイムアウトのない iOS、macOS、Linux では、スクリーンリーダーがオンの間、アクション付きのスナックバーは閉じられるまで表示されたままになります。推奨タイムアウトがないときの Material の挙動と同じです。Windows では、さらに「この時間が経過した後に通知を閉じる」（Dismiss notifications after）の設定も反映します。デスクトップと Web では、マウスがスナックバーの上にある間や、キーボードのフォーカスがその中にある間は、時間が進みません。スクリーンリーダーは、表示されたときにスナックバーを読み上げ、そのボタンにも移動できます。

一度に表示されるスナックバーは一つだけです。Android と同じく、新しいスナックバーは表示中のものを置き換えます（古いほうの `OnDismiss` が先に実行されます）。デスクトップでは、アラートと同じウィンドウに表示されます。つまり、スナックバーを表示するきっかけとなったイベントのビューがあるウィンドウ、それがなければ呼び出したコンポーネントのウィンドウ、それもなければメインウィンドウです。

## メニュー

メニューは `ui.Action` のリストを表示します。アラートのボタンやバーボタンと同じ型で、各タイトルの横には任意で `Icon` を付けられます。メニューを表示する方法は 2 つあります。

- `Menu` を指定した `ui.Button` は、タップするとメニューを開きます。
- `ui.Menu` は、任意の内容にメニューを付けます。タップで開くか、`LongPress` を指定してコンテキストメニューにします。

iOS では `UIMenu`、Android では `PopupMenu` です。Mac では `NSMenu`、Windows ではポップアップメニュー（Mac と同じく、破壊的な項目は赤）、Linux では `GtkPopoverMenu`、Web ではポップオーバーになります。デスクトップと Web では、コンテキストメニューは右クリック（副ボタンのクリック）で開き、Windows、Linux、Web では長押しでも開きます。どちらの方法も [コンポーネント](components.md#メニュー) で説明しています。`List` の行にはスワイプアクションもあります。[リスト](components.md#リスト) を参照してください。

## ウィンドウ

デスクトップアプリにはメインウィンドウがあり、`app.Run` が表示するものはそこに入ります。タイトル、開くときのサイズ、最小サイズを指定するには、`app.RunWindow` でアプリを起動します。

```go
func main() {
	app.RunWindow(ui.WindowOptions{Width: 420, Height: 640, MinWidth: 320, MinHeight: 360}, App)
}
```

サイズはウィンドウの内容の大きさで、単位はポイントです。何も指定しなければ、メインウィンドウは 800 × 600 ポイントで中央に開き、ユーザーは 320 × 320 ポイントより小さくできません。`Title` が空なら、アプリの名前になります。アクセサなので、アプリの言語に追従させられます。ウィンドウはユーザーが最後に置いた場所に戻り（[後述](#ウィンドウの位置の復元)）、それが `Width`、`Height`、`X`、`Y` より優先されます。iOS と Android は、これらのオプションを無視します。

`ui.Window` は、`ui.Sheet` がシートを開くのと同じように、`Open` が true の間、別のウィンドウを開きます。

```go
stats := signal.New(false)
return ui.Column(nil,
	ui.Button(ui.ButtonProps{Title: ui.Val("Statistics"), OnPress: func() { stats.Set(true) }}),
	ui.Window(ui.WindowProps{
		Open:  stats,
		Title: ui.Val("Statistics"),
		Width: 280, Height: 140,
		Content: func() ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprintf("%d notes", len(notes.Get())) }))
		},
	}),
)
```

- `Content` は、ウィンドウが開くときに組み立てられ、閉じるときに破棄されます。その中の `ui.UseEnvironment` はそのウィンドウのもので、サイズはユーザーによるサイズ変更に追従し、スケールはウィンドウのあるディスプレイのものになります。
- ユーザーがウィンドウを閉じると、`Open` が false になり、`OnClose` が呼ばれます。コードから `Open` を false にした場合は、`OnClose` を呼ばずに閉じます。
- 指定がなければ 800 × 600 ポイントで開きます。Mac と Windows では、使用中のウィンドウから少しずらして重ねる（カスケードする）位置に開きます。Linux では、GTK 4 がデスクトップに任せているとおり、配置はデスクトップが決めます（GNOME と KDE は、新しいウィンドウをほかのウィンドウをもっとも覆わない位置に置きます）。中のスタックには、メインウィンドウと同じく専用のバーがあります。シートとアラートは、ユーザーが作業中のウィンドウの上に開きます。

| | ウィンドウ |
|---|---|
| macOS | `NSWindow`。専用のツールバーとタイトル（もっとも内側のスタックのもの）を持ちます |
| Windows | トップレベルウィンドウ。専用のタイトルとタスクバーボタンを持ちます |
| Linux | `AdwWindow`。専用のヘッダーバーを持ちます |
| Web | ページ内のパネル。ユーザーが移動したりサイズを変えたりできます |

スマートフォンにはウィンドウがないので、iOS や Android でウィンドウを開くと panic します。ウィンドウを提供するコンポーネントは、先に `ui.Platform()` を確認するか、後述のメニューバーのように、デスクトップにしかないものからだけ開くようにしてください。

### オプション

`WindowOptions`（メインウィンドウ用）と `WindowProps`（`Window` 用）は、ウィンドウの開き方について同じオプションを受け取ります。

| オプション | |
|---|---|
| `Width`, `Height` | 内容の大きさ（ポイント） |
| `MinWidth`, `MinHeight`, `MaxWidth`, `MaxHeight` | ユーザーが変更できるサイズの最小値と最大値。0 ならホストの最小値で、最大値はなし |
| `X`, `Y` | 画面上でのフレームの左上の位置。メインディスプレイの左上からのポイント数で指定します。どちらも 0 なら、メインウィンドウは中央に置かれ、`Window` はカスケードされます（`Center` で中央に置けます） |
| `DisableResize`, `DisableMinimize`, `DisableMaximize`, `DisableClose` | ユーザーの操作を禁止します。サイズ変更と、各ボタンの操作です |
| `AlwaysOnTop` | ほかのアプリのウィンドウより常に手前に表示します |
| `TitleBar` | `ui.TitleBarHidden` を指定すると、内容をタイトルバーの下、ウィンドウの上端までレイアウトし、ウィンドウのボタンをその上に重ねます |
| `Background`, `Transparent` | 内容の背後にあるウィンドウの色、または色なし。色なしにすると、内容が何も描かない部分ではウィンドウの背後にあるものが見えます |
| `Hidden` | 表示せずに開きます。`Show` で表示します |
| `SkipTaskbar` | タスクバー（Windows）と「ウインドウ」メニュー（macOS）に表示しません |
| `Maximized`, `Fullscreen` | 最大化した状態、またはフルスクリーンで開きます |
| `StateKey` | 位置を保存するときのキー。[後述](#ウィンドウの位置の復元) を参照してください |
| `FitContent` | 内容に合わせてサイズを決め、内容が変わると最小・最大サイズの範囲内で追従します（[デスクトップ](desktop.md#コンテンツに合わせたウィンドウサイズ)） |
| `Panel` | ランチャーやパレットのウィンドウ。すべてのアプリの上に浮かび、アプリを前面に出さずにキーボード入力を受け取り、Esc キーかほかの場所のクリックで閉じます（[パネル](desktop.md#パネルとランチャー)） |
| `ClickThrough` | マウスを通過させ、背後にあるものに届けます（[クリックスルー](desktop.md#クリックスルーウィンドウ)） |
| `Flyout` | `Window` 用。トレイアイコン、または親ウィンドウ内の矩形からぶら下げて表示します（[フライアウト](desktop.md#フライアウト)） |
| `Child`, `Modal` | `Window` 用。[後述](#子ウィンドウとモーダルウィンドウ) を参照してください |
| `MenuBar`, `Edited`, `File` | 文書を表示する `Window` 用。それぞれ、そのウィンドウ内のアプリのメニューバー（Windows、Linux）、Mac の閉じるボタンの中の点またはタイトルの前の `*`、Mac のタイトルバーに表示するファイルのアイコンです。[`asuka/sdk/document`](desktop.md#ドキュメント) がこれらを設定します |

タイトルバーを隠すと、内容はウィンドウのボタン（Mac の信号機ボタン、Windows のキャプションボタン、Linux ではヘッダーバーのボタン）の下を通って上端まで広がります。ウィンドウの環境には、タイトルバーの高さが `SafeTop` として入ります。そのため `ui.SafeArea` はボタンを避けて配置され、背景やサイドバー、画像はボタンの下まで広げられます。

```go
app.RunWindow(ui.WindowOptions{TitleBar: ui.TitleBarHidden, Background: colors.RGB(0x1e, 0x1e, 0x1e)}, func() ui.Node {
	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16)}, editor())
})
```

ボタンの下の帯は、内容がクリックを受け取らない部分では、引き続きウィンドウのドラッグに使えます。Windows では、タイトルバーを隠したメインウィンドウにメニューバーは表示されませんが、そのショートカットは引き続き使えます。

準備ができてから表示したいウィンドウは、`Hidden` で開いてコードから表示します。その間に内容が組み立てられ、レイアウトされ、描画されるので、空の状態が見えることはなく、完成した状態で表示されます。バックグラウンドで起動するアプリ（トレイアイコンのアプリなど）は、メインウィンドウを非表示のまま起動し、メニューから表示します。

### ウィンドウの操作

`ui.UseWindow()` は、コンポーネントが入っているウィンドウのハンドルです。つまり、コンポーネントがその内容に含まれる `Window`、またはメインウィンドウのハンドルです。`ui.MainWindow()` は、メインスレッド上のどこからでも（トレイアイコンのメニューやグローバルショートカットなど）使えるメインウィンドウのハンドルです。`Window` のハンドルをその外側から使うには、アプリがハンドルを作り（`ui.NewWindowHandle()`）、`Handle` として渡します。

```go
w := ui.UseWindow()
ui.Button(ui.ButtonProps{Title: ui.Val("Full Screen"), OnPress: func() { w.SetFullscreen(true) }})
ui.Text(signal.Func(func() string {
	if w.Focused().Get() {
		return "Typing here"
	}
	return "In the background"
}))
```

- `Show` と `Hide` は、ウィンドウを表示・非表示にします。非表示にしてもウィンドウは開いたままで、内容も生き続けます。`Focus` は、ウィンドウが非表示か最小化されていれば表示し、キーボード入力を受け取る状態で最前面に出し、アプリも前面に出します。トレイアイコンのメニューからウィンドウを呼び戻すときに使う方法です（Windows では、トレイアイコンのクリックのように、ユーザーがアプリに対して入力した後であれば、アプリが前面に出ることが許されます）。閉じている `Window` に対しては、`Show` と `Focus` でウィンドウが開きます。
- `Minimize`、`Maximize`、`Restore`（最小化と最大化のどちらからも戻します）。`SetFullscreen`。
- `SetSize`（内容の大きさ、ポイント）、`SetPosition`（フレームの左上の位置。メインディスプレイの左上から）、`Center`。
- `SetAlwaysOnTop` は、ほかのアプリのウィンドウより常に手前に表示するかどうかを切り替えます。
- `SetClickThrough` は、マウスを通過させるかどうかを切り替えます。
- `Close` は、ユーザーが閉じるときと同じようにウィンドウを閉じ、先に確認します（後述）。`Destroy` は確認せずに閉じます。メインウィンドウが閉じると、アプリは終了します。
- ウィンドウの状態はシグナルで、アプリによる変更だけでなくユーザーによる変更にも追従します。`Visible`、`Focused`、`Minimized`、`Maximized`、`Fullscreen`、`AlwaysOnTop`、`ClickThrough`、`Position`、`Size` があります。エフェクトか `StyleFunc` の中で読んでください。最小化されたウィンドウや、ほかのウィンドウの背後にあるウィンドウも、visible のままです。

これらのメソッドは、シグナルへの書き込みと同じく、フラッシュのときにウィンドウに作用します。スマートフォンでは何もせず、状態は常に表示中かつフォーカスありとして読めます。

### ウィンドウを閉じる前の確認

`OnCloseRequest` は、ユーザーがウィンドウを閉じようとしたとき（閉じるボタン、⌘W、Alt+F4、Dock やタスクバーのメニュー）や、アプリが `Close` を呼んだときに問い合わせを受けます。false を返すとウィンドウは開いたままになるので、保存されていない変更について先に確認できます。失うものがあるコンポーネントは、マウントされている間、それぞれが自分で確認します。

```go
w := ui.UseWindow()
w.OnCloseRequest(func() bool {
	if !doc.Dirty() {
		return true
	}
	ui.Alert(ui.AlertProps{Title: "Save changes?", Actions: []ui.Action{
		{Title: ui.Val("Save"), OnPress: func() { doc.Save(); w.Destroy() }},
		{Title: ui.Val("Don't Save"), Role: ui.ActionDestructive, OnPress: w.Destroy},
		{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
	}})
	return false
})
```

問い合わせを受けるすべての関数が同意しなければ、ウィンドウは閉じません。`WindowProps.OnCloseRequest` と `WindowOptions.OnCloseRequest` にも問い合わせます。ユーザーが選んだ後に呼ぶ `Destroy` は、再び確認せずにウィンドウを閉じます。Web では、メインウィンドウはタブそのものです。ページから離れるときやタブを閉じるときにも確認し、拒否するとページはブラウザー自身の「このサイトを離れますか？」という確認の背後に残ります（この文言はページからは変えられません）。アプリの終了（⌘Q）では、ウィンドウに確認しません。

### ウィンドウの位置の復元

ウィンドウは、次に起動したときも、ユーザーが最後に置いた場所に戻ります。メインウィンドウは常に（キー `"main"`、または `StateKey` で）戻り、`Window` は `StateKey` を持つときに戻ります。ウィンドウの種類ごとに別のキーを付けてください（`"inspector"`、`"preferences"`）。前回の位置は `Width`、`Height`、`X`、`Y` より優先されます。これらの値は、初回起動時に開くときに使われます。

| | 保存されるもの |
|---|---|
| macOS | フレーム。AppKit のフレーム自動保存を使います（アプリの defaults に保存） |
| Windows | 位置とサイズ（画面のピクセル単位と、そのときの DPI）、最大化されていたかどうか。接続されているモニターの上に、はみ出さないように戻します |
| Linux | サイズと、最大化されていたかどうか。Wayland ではアプリがウィンドウの位置を決められないためです |
| Web | `Window` パネルの位置とサイズ、最大化されていたかどうか。ページの `localStorage` に保存します |

デスクトップのホストは、この情報をアプリのデータディレクトリにある `asuka-window.json`（Windows、Linux）、またはアプリの defaults（macOS）に保存します。テストや自動実行では何も保存しません。

### 子ウィンドウとモーダルウィンドウ

`Child` を指定した `Window` は、宣言されたウィンドウ（メインウィンドウ、またはその `Window` を内容に持つ `Window`）の子になります。子ウィンドウは親ウィンドウより手前にとどまり、親と一緒に閉じます（オーナーが親ウィンドウの内容と一緒に破棄されるためです）。`Modal` を指定すると、閉じるまで親ウィンドウを操作できなくする子ウィンドウになります。

```go
ui.Window(ui.WindowProps{Open: prefs, Modal: true, Title: ui.Val("Preferences"), Width: 480, Height: 360,
	Content: preferences})
```

| | `Child` | `Modal` |
|---|---|---|
| macOS | 子ウィンドウ。親より手前に重なり、親と一緒に動きます | 親のシート |
| Windows | 親に所有されるウィンドウ。親より手前にあり、親と一緒に最小化されます | 所有されるウィンドウで、開いている間は親が無効になります |
| Linux | 親に対して transient で、親と一緒に破棄されます | transient かつモーダル |
| Web | ほかと変わらないパネル | ページのクリックを受け止める背景の上に置くパネル |

### デスクトップごとの対応状況

| | macOS | Windows | Linux (GTK 4) | Web：タブ | Web：`Window` |
|---|---|---|---|---|---|
| 表示・非表示・フォーカス | 対応 | 対応 | 対応 | フォーカスのみ | 対応 |
| 最小化 | 対応 | 対応 | 対応（Wayland では状態が通知されません） | 非対応 | タイトルバーだけに縮小 |
| 最大化・復元 | ズーム | 対応 | 対応 | 非対応 | アプリの領域いっぱいに広げる |
| フルスクリーン | 対応 | 対応 | 対応 | Fullscreen API | Fullscreen API |
| サイズ・最小サイズ | 対応 | 対応 | 対応 | 非対応 | 対応 |
| 最大サイズ | 対応 | 対応 | 非対応（GTK 4 にはありません） | 非対応 | 対応 |
| 位置・中央配置 | 対応 | 対応 | X11 のみ。Wayland では非対応（コンポジターがウィンドウを配置します） | 非対応（ブラウザーのウィンドウの位置は読めます） | 対応 |
| 常に手前に表示 | 対応 | 対応 | X11 のみ | 非対応 | ほかのパネルより手前 |
| タイトルバーを隠す | 対応 | 対応 | 対応 | 非対応 | 対応 |
| 背景・透明 | 対応 | 背景のみ。透明は背景で代替 | 対応（透明はコンポジターがある場合） | 背景のみ | 対応 |
| タスクバーに表示しない | 「ウインドウ」メニュー | 対応 | X11 のみ | 非対応 | 非対応 |
| パネル・フライアウト | 対応 | 対応 | X11：手前に保ち、配置も可能。Wayland：配置できず、手前にも保てません | 非対応 | 対応 |
| 内容に合わせたサイズ | 対応 | 対応 | 対応 | 非対応 | 対応 |
| クリックスルー | 対応 | 対応 | 対応 | 非対応 | ページまで通す |

## メニューバーとキーボードショートカット

`ui.MenuBar` は、デスクトップアプリにメニューを与え、その項目にキーボードショートカットを割り当てます。

```go
ui.MenuBar(signal.Static([]ui.MenuBarMenu{
	{Title: "File", Items: []ui.MenuItem{
		{Title: "New Note", Shortcut: ui.Cmd("n"), OnPress: add},
		ui.MenuSeparator,
		{Title: "Export…", Shortcut: ui.Cmd("e").With(ui.Shift), OnPress: export},
	}},
	{Title: "Edit", Items: []ui.MenuItem{
		{Title: "Clear Completed", Shortcut: ui.Cmd("k").With(ui.Shift), Destructive: true, OnPress: clear},
	}},
}))
```

- `ui.MenuItem` は、`Title`、`Shortcut`、`OnPress` を持ちます。`Disabled`、`Checked`、`Destructive`（赤で表示）で表示が変わります。`ui.MenuSeparator` は、項目のグループの間に入れる区切り線です。
- `ui.Shortcut` は、`Key` とその `Mods` からなります。`ui.Cmd(key)` は、そのキーとプラットフォームのコマンドキー（Mac では ⌘、それ以外では Ctrl）の組み合わせです。`.With(ui.Shift)`、`ui.Alt`（Mac の Option）、`ui.Control` で修飾キーを追加します。キーは文字（`"n"`、`","`、`"="`）か名前で指定します。名前は `enter`、`escape`、`tab`、`space`、`delete`、`backspace`、`up`、`down`、`left`、`right`、`home`、`end`、`pageup`、`pagedown`、`f1` から `f12` までです。
- メニューはアクセサに追従します。項目が無効になったりチェックされたり、メニューが現れたり消えたりします。複数の `MenuBar` がマウントされているときは最後のものが表示され、それが破棄されると一つ前のものが表示されます。`ui.MenuBar` が返すノードは領域を取らないので、どこに置いてもかまいません。

デスクトップごとの表示は次のとおりです。

- **macOS**：メニューバー。Mac 標準のメニューと並んで表示されます。標準のメニューは、アプリケーションメニュー（About、Hide、Quit）、File、Edit（Undo、Cut、Copy、Paste...）、View（Back、Enter Full Screen）、Window です。File、Edit、View、Window、Help というタイトルのメニューは、対応する標準メニューに項目を追加します。それ以外のメニューは View と Window の間に入ります。
- **Windows**：メインウィンドウのメニューバー。ショートカットはアプリのすべてのウィンドウで使えます。各メニューと項目にはアクセスキー（Alt+F）が付き、各項目のタイトルの後ろにはショートカット（"Ctrl+Shift+K"）が表示されます。
- **Linux**：GNOME のアプリにはメニューバーがなく、代わりにメインメニュー（ヘッダーバーの最後のボタン）があります。アプリの各メニューはそのサブメニューになり（メニューが一つだけなら、その項目がメインメニューに直接並びます）、ショートカットはすべてのウィンドウで使えます。
- **Web**：ショートカットはページ内で使えます。メニューバーは表示しません。

iOS と Android は `MenuBar` を無視するので、どのプラットフォームで動くアプリにもメニューバーを持たせられます。ただし、メニューの項目はコマンドなので、スマートフォンのために、それぞれ画面上の UI からも実行できるようにしておいてください。

## トースト

iOS にも Android にも、アプリ間で共通して使われる標準のトーストはないため、Asuka にはトーストのコンポーネントがありません。画面の上に重ねたビューを `ui.Show` で表示し、[トランジション](motion.md#表示と非表示のトランジション) で登場させて作ってください。ギャラリーのトーストはこの方法で作られていて、スクリーンリーダーにも読み上げさせるために `ui.Announce` を呼んでいます。[`examples/gallery`](../../examples/gallery) を参照してください。

## ディープリンク

`asuka.json` に記載しておくと、アプリは独自スキームの URL や、自分のドメインへの https リンクで開くようになります。

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "scheme": "notes",
  "links": ["notes.example.com"]
}
```

- `scheme` を指定すると、`notes://anything` でアプリが開きます。対応するのは、iOS と Android、Mac（アプリの `Info.plist` でスキームを宣言します）、インストール後の Linux（デスクトップファイルがスキームを処理します。GNOME のアプリはそれぞれ単一インスタンスなので、アプリの実行中に開かれたリンクは実行中のアプリに届きます）、そして Windows（アプリは起動時に `HKEY_CURRENT_USER\Software\Classes` の下にユーザー単位でスキームを登録し、アプリが移動していれば登録し直します。アプリの実行中に開かれたリンクは実行中のアプリに届きます）です。`id` を持つアプリは、Linux と Windows では一つしか起動しません。もう一度起動すると、すでに動いているアプリが前面に出ます。`asuka dev` は Linux や Windows ではスキームを登録せず、その実行がインストール済みのアプリに処理を引き渡すこともありません。リンクは、ビルドしたアプリで試してください。
- `links` を指定すると、`https://notes.example.com/...` でアプリが開きます。iOS ではユニバーサルリンク、Android では App Links です。ドメインの側でもそれを宣言する必要があり、各プラットフォームのドキュメントに従って、iOS 向けに `apple-app-site-association` を、Android 向けに `assetlinks.json` を配信します。デスクトップでは、これらのリンクでアプリは開きません。

`ui.OnOpenURL` は、起動時でも実行中でも、アプリを開いた URL を一つずつ受け取ります。アプリを起動した URL はハンドラーが登録される前に届くので、最初のハンドラーに渡されます。ハンドラーは、そのコンポーネントのオーナーと一緒に止まります。ハンドラーの中では、ナビゲーターを使うか、シグナルに書き込んで画面を切り替えてください。

```go
ui.OnOpenURL(func(raw string) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	if u.Host == "add" {
		fmt.Println("add", u.Query().Get("text"))
	}
})
```

[`examples/notes`](../../examples/notes) は、`asukanotes://add?text=...` でメモを追加します。通知の URL も、ユーザーが通知をタップしたときに、すべてのプラットフォームで同じ方法で届きます（[デバイスとサービス](device.md) を参照）。Web アプリには独自のリンクはありません。画面を移動してもページのアドレスは変わらず、ページを開いても `OnOpenURL` は呼ばれません。

## ほかのアプリからの共有

`asuka.json` に `"shareTarget": true` を指定すると、ブラウザーや YouTube の「共有」のように、ほかのアプリの共有シートにアプリが表示され、テキストやリンクを受け取れるようになります。`{"types": ["image/*", ...]}` を指定すると、ファイルも受け取れます（[デバイスとサービス](device.md#ほかのアプリからの共有-uionshare) を参照）。`ui.OnShare` は、共有された内容を受け取ります。`OnOpenURL` と同じく、アプリを起動した共有は最初のハンドラーに渡されます。

```go
ui.OnShare(func(items []ui.SharedItem) {
	for _, it := range items {
		fmt.Println("shared:", it.Kind, it.Text, it.Path)
	}
})
```

iOS では、アプリに共有拡張機能（share extension）が付きます。この拡張機能は、Android と同じように、共有された内容を持ってアプリを開きます。拡張機能が独自の画面を表示することはありません。共有される内容は、タイトルとリンクの組であることがよくあります（Safari からは、ページのタイトルがテキストとして、その URL がリンクとして届きます）。[`examples/downloader`](../../examples/downloader) は、その中からリンクを探します。デスクトップと Web は共有先にならないので、そこでは `OnShare` は呼ばれません。

自分のアプリを開かずに、共有元のアプリの上に自分のアプリのシートを表示するには、[デバイスとサービス](device.md#アプリ独自の共有シート-appsharesheet) の `app.ShareSheet` を参照してください。逆に自分のアプリから共有するには、`ui.Share(text)` でシステムの共有シートを表示し、`ui.ShareFiles` でファイルを共有します。Mac では共有メニュー、Windows では共有 UI、Web ではブラウザーの共有 UI になります。Linux には共有シートがないので、これらの関数は共有する内容をクリップボードにコピーし、そのことをトーストで知らせます。

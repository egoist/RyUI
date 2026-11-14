# 基本概念

Asuka のアプリは、ビューを一度だけ組み立てる Go の関数と、状態を保持するシグナルでできています。このページでは、これらがどう組み合わさるかを説明します。具体的には、ルートコンポーネント、シグナルとそれが更新するもの、ビューの表示の切り替えと繰り返し、オーナーとクリーンアップ、メインスレッド、そして環境です。

## アプリ

`app.Run` はルートコンポーネントを受け取り、アプリの画面としてマウントします。

```go
func App() ui.Node {
	return ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12},
		ui.StaticText("Hello", ui.Style{Font: fonts.LargeTitle}),
		Greeting("Ada"),
	)
}

func Greeting(name string) ui.Node {
	return ui.StaticText("Hello, " + name)
}

func main() { app.Run(App) }
```

ルートは、スマートフォンではノッチやホームインジケーターの下まで画面全体に広がり、デスクトップではウィンドウ全体に広がります。`ui.SafeArea` を使うと、それらに覆われる部分やキーボードを避けてコンテンツを配置できます（[レイアウトとスタイル](layout.md#セーフエリアキーボードウィンドウ)を参照）。

`app.Run` の動き方はプラットフォームによって異なります。

- **iOS と Android**：アプリを起動するのはプラットフォームです。アプリのホスト（Swift、Kotlin）が Go のコードを読み込んで `main` を呼び出します。`app.Run` はルートをマウントすると、すぐに戻ります。
- **macOS、Windows、Linux**：アプリはホストも含めて一つの Go プログラムです。`app.Run` はメインウィンドウを開いてそこにルートをマウントし、アプリが終了するまでメインスレッドでプラットフォームのイベントループを回します。上の例のように `main` から呼び出してください。
- **Web**：ページがアプリの WebAssembly を読み込み、`app.Run` がルートをページにマウントします。

`app.RunWindow` は、メインウィンドウのオプション（タイトル、サイズ、最小サイズ）を指定できる `app.Run` です。スマートフォンではこれらのオプションは無視されます。デスクトップアプリでは、`ui.Window` でウィンドウを追加で開いたり、`ui.MenuBar` でメニューバーを持たせたりできます。詳しくは[ナビゲーション](navigation.md)を参照してください。

## コンポーネント

コンポーネントは、`ui.Node` を返す Go の関数です。特別な仕組みのない普通の関数で、必要な引数を自由に受け取り、ほかのコンポーネントを呼び出して子を作ります。

コンポーネントは**一度だけ実行されます**。ビューを組み立てて返したら、それで終わりです。状態が変わっても、もう一度呼ばれることはありません。その後に変わるのは、シグナルを読んでいる部分だけです。

変わる可能性のあるプロパティは `signal.Accessor[T]` を受け取ります。シグナル、メモ、`signal.Func` のどれでも渡せます。決して変わらないプロパティには `ui.Val(v)` を使います。これは一度送られるだけで、それ以上のコストはかかりません。アクセサが nil の場合はゼロ値（`false`、`""`、0）になるので、false にしたいプロパティは省略できます。`OnPress` などのイベントハンドラーは普通の関数です。

```go
ui.Button(ui.ButtonProps{
	Title:    ui.Val("Save"),
	Disabled: saving, // *signal.Signal[bool]
	OnPress:  save,
})
```

## シグナル

シグナルは値を保持します。シグナルは `signal` パッケージで作ります。

- `signal.New(v)` は、比較可能な型のシグナルを作ります。現在の値と等しい値を書き込んでも、何も起きません。
- `signal.NewFunc(v, equal)` は、独自の等価判定を持つシグナルを作ります。`==` で比較できない値（スライス、マップ、それらを含む構造体）に使います。`equal` が nil の場合は、書き込むたびに通知されます。
- `Get` は値を読み、読んでいる側（エフェクト、プロパティ、メモ）にそのシグナルを購読させます。
- `Peek` は購読せずに値を読みます。どのみち更新されることのないイベントハンドラーの中では、こちらを使います。
- `Set` は値を書き込みます。`Update(f)` は、現在の値に `f` を適用した結果を書き込みます。

```go
count := signal.New(0)
count.Update(func(n int) int { return n + 1 })

tags := signal.NewFunc([]string{"go"}, nil)
tags.Update(func(t []string) []string { return append(slices.Clone(t), "ios") })
```

シグナルは、コンポーネントの中でローカル変数として作ります。アプリ全体で共有する状態なら、パッケージ変数として作ることもできます。

### 派生値

`signal.Func` は、ほかの値から自分の値を計算するアクセサを作ります。読まれるたびに計算が実行され、計算中に読んだものは、それを読んだ側が追跡します。

```go
first := signal.New("Ada")
last := signal.New("Lovelace")
full := signal.Func(func() string { return first.Get() + " " + last.Get() })
```

`signal.NewMemo` は値をキャッシュします。最初の `Get` で計算し、その後は読んだもののどれかが変わったときだけ計算し直します。計算にコストがかかる場合や、多くの箇所から読まれる場合に使います。

```go
left := signal.NewMemo(func() int {
	n := 0
	for _, t := range todos.Get() {
		if !t.Done {
			n++
		}
	}
	return n
})
```

`signal.NewMemoFunc` は独自の等価判定を持つメモで、シグナルにおける `NewFunc` に相当します。メモの中でシグナルに書き込んではいけません。メモの中で `Set` を呼ぶと panic します（`asuka vet` もこれを報告します）。

## 何が更新されるか

アクセサを渡したプロパティは、それぞれが独立したエフェクトになります。そのエフェクトが読んだシグナルが変わると、そのエフェクトだけが再実行され、その一つのプロパティをネイティブビューに送ります。ほかには何も実行されません。

```go
func Score() ui.Node {
	score := signal.New(0)
	return ui.Column(nil,
		// Score の実行時に一度だけ読む：このテキストは変わらない
		ui.StaticText(fmt.Sprintf("Started at %d", score.Get())),
		// アクセサの中で読む：このテキストは score に追従する
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%d points", score.Get()) })),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Score"),
			OnPress: func() { score.Update(func(n int) int { return n + 1 }) },
		}),
	)
}
```

一つ目のテキストは、もっともよくある間違いです。コンポーネントは一度しか実行されないので、コンポーネントの本体で読んだシグナルも一度しか読まれません。シグナルは、アクセサ、`ui.StyleFunc`、エフェクトのいずれかの中で読んでください。意図して一度だけ読みたいときは、`Peek`（アクセサなら `signal.Untrack`）を使ってその意図を明示します。

[`asuka vet`](development.md#asuka-vet)（`asuka dev` が保存のたびに実行します）は、こうした読み取りを見つけます。対象は、コンポーネントの本体での読み取りと、同じく一度しか実行されない `ui.Show` の分岐や `ui.For` の行での読み取りのうち、読んだ値が組み立てるビューに使われるものです。アプリが表示する言語についても同じことが言えます。カタログのメッセージを本体で呼び出しても（`locales.Title()`）、言語の切り替えには追従しません。`locales.Live.Title()` なら追従します。

`ui.StyleFunc` は、変化するスタイルです。ビューごとに一つのエフェクトの中で実行され、返された値は前回の値と比較されて、違うプロパティだけが送られます（レイアウトが再実行されるのは、レイアウトに関わるプロパティが変わったときだけです）。[レイアウトとスタイル](layout.md#動的なスタイル)を参照してください。

## エフェクト

`signal.Effect(f)` は `f` を実行し、`f` が読んだものが変わるたびに再実行します。保存、ログ出力、リクエストの開始など、ビュー以外の処理に使います。コンポーネントの組み立て中やイベントハンドラーの中で作ったエフェクトは、その処理が終わってから実行されます。それ以外の場所で作った場合は、すぐに実行されます。

エフェクトの中で `signal.OnCleanup(f)` を呼ぶと、エフェクトが再実行される前と、エフェクトが破棄されるときに `f` が実行されます。次の例では、入力が止まるまで検索を待ちます。

```go
signal.Effect(func() {
	q := strings.TrimSpace(query.Get())
	t := time.AfterFunc(300*time.Millisecond, func() {
		ui.Post(func() { results.Set(search(q)) })
	})
	signal.OnCleanup(func() { t.Stop() })
})
```

エフェクトはシグナルに書き込んでもかまいません。その書き込みによって再実行が必要になったエフェクトは、同じフラッシュの中で再実行されます。エフェクトどうしが互いを再実行させ続けて 100 回に達すると、そのうちの一つの名前を示して panic します。

`signal.RenderEffect(f)` は `f` をすぐに実行し、変更があるたびに、ユーザーのエフェクトより先に再実行します。`ui` がプロパティの更新に使っている仕組みで、アプリで必要になることはほとんどありません。

`signal.Untrack(f)` は、`f` が読んだものを購読せずに `f` を呼び出し、その戻り値を返します。`Peek` を持たないアクセサを読むときに使います。

```go
signal.Effect(func() {
	q := query.Get() // query が変わると再実行される
	who := signal.Untrack(func() string { return full.Get() }) // full が変わっても再実行されない
	fmt.Println(who, "searched for", q)
})
```

`signal.Batch(f)` は `f` を実行し、その中の書き込みで再実行が必要になったエフェクトを、最後に一度だけ実行します。イベントハンドラーと `ui.Post` はもともと書き込みをまとめて扱い、ハンドラーが戻ってからビューを一度に更新します。`Batch` は、それ以外の場所で書き込むときのためのものです。

## 表示の切り替えと繰り返し

`ui.Show` は、条件が true の間は一方の分岐を表示し、false の間はもう一方の分岐を表示します（もう一方は省略できます）。

```go
ui.Show(signedIn,
	func() ui.Node { return ui.StaticText("Welcome back") },
	func() ui.Node { return SignIn(signedIn) },
)

ui.Show(signal.Func(func() bool { return len(items.Get()) == 0 }),
	func() ui.Node { return ui.StaticText("Nothing yet") })
```

分岐は表示されるときに組み立てられ、表示されなくなると破棄されます。破棄された分岐のエフェクトは止まり、ビューも削除されます。Show が組み立て直すのは条件が切り替わったときだけで、条件が計算されるたびではありません。

`ui.For` は、スライスの要素ごとに行を一つずつ、キーで識別して表示します。

```go
ui.For(items, func(it Item) int { return it.ID }, func(item signal.Accessor[Item], index signal.Accessor[int]) ui.Node {
	return ui.Text(signal.Func(func() string {
		return fmt.Sprintf("%d. %s", index.Get()+1, item.Get().Name)
	}))
})
```

行は、そのキーがスライスにある間だけ存在します。新しいキーが現れると行が組み立てられ、キーがなくなるとその行は破棄されます。要素が移動すると、その行のビューも一緒に移動します。同じキーの要素の中身が変わった場合、行は組み立て直されません。その行の `item` シグナルに新しい値が設定され、それを読んでいる部分だけが更新されます。各要素には安定したキー（インデックスではなく ID）を与えてください。

`ui.Dynamic` は関数が返したものを表示し、関数が読んだもののどれかが変わると、すべてを組み立て直します。

```go
ui.Dynamic(func() ui.Node {
	switch mode.Get() {
	case "grid":
		return GridView()
	default:
		return ListView()
	}
})
```

関数が読んだものは、呼び出したコンポーネントの本体での読み取りも含めてすべて追跡され、どれかが変わるたびにサブツリー全体が組み立て直されます。できるだけ `Show` と `For` を使ってください。

### panic が起きたとき

`ui.Catch` を使うと、UI の一部で起きた panic でアプリ全体が落ちるのを防げます。`ui.Catch` は組み立て関数が返したものを表示し、組み立て中に panic が起きたとき、あるいは後でその中のエフェクトが panic したときは、代わりにフォールバックを表示します。

```go
ui.Catch(func() ui.Node { return Profile(id) }, func(err any, reset func()) ui.Node {
	log.Printf("profile: %v", err)
	return ui.Column(ui.Style{Gap: 8},
		ui.StaticText("Couldn't show this profile"),
		ui.Button(ui.ButtonProps{Title: ui.Val("Try again"), OnPress: reset}))
})
```

失敗した UI は、表示されなくなった `Show` の分岐と同じように破棄されます。そのエフェクトは止まり、ビューも削除されます。panic する前に組み立て途中で作られたビューも削除の対象です。`reset` を呼ぶと、もう一度組み立てます。`Catch` がオーナーとなっているエフェクトは recover 付きで実行されます。どの `Catch` にも属さないエフェクトは recover なしで実行されるので、余計なコストはかかりません。

`Catch` がオーナーでないものの panic は、`Catch` がない場合と同じようにクラッシュします。イベントハンドラー（`OnPress`）の panic、ゴルーチンの panic、フォールバック自身の panic がこれにあたります。`Catch` は、単独で失敗しうる画面の一部（サーバーから受け取ったデータを表示するウィジェット、プラグイン、リッチなドキュメントなど）を囲むために使います。バグを隠すためにアプリ全体を囲むものではありません。

signal パッケージの `signal.Catch` は、UI を持たないエフェクトに対して同じことをします。

### 長いリスト

`For` はすべての行を組み立て、すべて保持し続けます。長いリストや上限のないリスト（フィード、数千行のリストなど）には `ui.List` を使ってください。引数は `For` と同じで、内部ではプラットフォームの行を再利用するリストを使い、画面に表示されている分の行だけを組み立てます。[コンポーネント](components.md#リスト)を参照してください。

## オーナーとクリーンアップ

エフェクトとメモはすべて、どれか一つのオーナーに属します。属する先は、それが作られたときに現在のオーナーだったものです。アプリのルートにはオーナーが一つあり、`Show` の分岐、`For` の行、`Dynamic` の組み立て、スタックの画面、タブ、シートも、それぞれ親のオーナーの下に自分のオーナーを持ちます。オーナーを破棄すると、そのエフェクトが止まり、その下のオーナーも破棄され、クリーンアップが登録とは逆の順番（最後に登録したものから）で実行されます。

オーナーが破棄されるのは、次のときです。

- `Show` が分岐を切り替えたとき：古い分岐のオーナー
- `For` の要素のキーがスライスからなくなったとき：その行のオーナー
- `Dynamic` が組み立て直したとき：前回の組み立てのオーナー
- 画面がポップされたとき（戻るボタン、スワイプ、`Navigator.Pop`）：その画面のオーナー
- シートが閉じたとき：シートの中身のオーナー

Go のガベージコレクターはメモリを解放しますが、まだ購読しているエフェクトを止めることはありません。エフェクトを止めるのはオーナーの役割です。コンポーネントの本体で呼んだ `signal.OnCleanup` は、そのコンポーネントのオーナーが破棄されるときに実行されます。コンポーネントが始めた処理を止めるのに使ってください。

```go
func Clock() ui.Node {
	now := signal.New(time.Now().Format("15:04:05"))
	ticker := time.NewTicker(time.Second)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case t := <-ticker.C:
				ui.Post(func() { now.Set(t.Format("15:04:05")) })
			case <-done:
				return
			}
		}
	}()
	signal.OnCleanup(func() {
		ticker.Stop()
		close(done)
	})
	return ui.Text(now, ui.Style{Font: fonts.Title.Weighted(fonts.Semibold)})
}
```

これを `ui.Show(showClock, Clock)` で表示すると、時計が非表示になったときにティッカーが止まり、再び表示されると新しいティッカーが動き始めます。

## メインスレッド

コンポーネント、イベントハンドラー、エフェクト、そしてシグナルへのすべての書き込みは、プラットフォームのメインスレッドで実行されます。メインスレッドとは、iOS と Android では UI スレッド、macOS、Windows、Linux ではプロセスの最初のスレッド（ホストが専有します）、ブラウザーではページのスレッドのことです。メインスレッドはブロックしないでください。メインスレッドが待っている間、アプリは描画も、スクロールも、入力の受け付けもできません（デスクトップでは、ウィンドウに「応答なし」と表示されます）。時間のかかるリクエストや大きなファイルの処理はゴルーチンで行います。

ゴルーチンからは、シグナルやビューに直接触れません。代わりに関数を `ui.Post` に渡します。渡した関数はメインスレッドで実行され、その後ビューが更新されます（ゴルーチンがシグナルに直接書き込んでいると、`asuka vet` が報告します）。

```go
load := func() {
	loading.Set(true)
	go func() {
		msg := "Done"
		resp, err := http.Get("https://example.com")
		if err != nil {
			msg = err.Error()
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		ui.Post(func() {
			status.Set(msg)
			loading.Set(false)
		})
	}()
}
```

[ネイティブパッケージ](native.md)のメソッドは、それを呼び出したスレッドで実行されます。時間のかかるメソッドはゴルーチンから呼ぶか context を渡すかして、結果を `ui.Post` でメインスレッドに戻してください。

```go
go func() {
	level, err := b.Level() // ネイティブパッケージのモジュール
	ui.Post(func() {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set(fmt.Sprintf("%.0f%%", level*100))
	})
}()
```

## 環境

`ui.UseEnvironment()` は、コンポーネントが表示される場所についてプラットフォームが伝える情報を、アクセサとして返します。アクセサ、`StyleFunc`、エフェクトの中で読めば、画面の回転、キーボード、ダークモード、文字サイズの変化に追従します。返されるのは、そのコンポーネントがある画面（スタックの画面、タブ、シート、ルートのいずれか）の環境です。コンポーネントの組み立て中に呼び出してください。

```go
env := ui.UseEnvironment()
orientation := signal.Func(func() string {
	if e := env.Get(); e.Width > e.Height {
		return "Landscape"
	}
	return "Portrait"
})
```

フィールドは次のとおりです。

- `Width`、`Height`：画面のサイズ（ポイント単位）。デバイスではなく、アプリのウィンドウのサイズです。画面分割（iPad の Split View やステージマネージャ、Android の分割画面）ではアプリに割り当てられた部分のサイズに、デスクトップやブラウザーではウィンドウのコンテンツ領域のサイズになります。ユーザーがサイズを変えると、それに合わせて変わります。
- `SafeTop`、`SafeRight`、`SafeBottom`、`SafeLeft`：セーフエリアのインセット。デスクトップのウィンドウにはセーフエリアがないので、すべて 0 です。
- `Keyboard`：画面の下端のうち、オンスクリーンキーボードに覆われている高さ。
- `Scale`：1 ポイントあたりのピクセル数。スマートフォンでは 2 か 3、Retina ディスプレイの Mac では 2、Windows と Linux ではディスプレイの拡大率（150% なら 1.5）です。
- `FontScale`：ユーザーが設定した文字サイズ（iOS の Dynamic Type、Android のフォントサイズ、Windows の「テキストのサイズ」、GNOME のテキストの拡大率）。1 がデフォルトです。Mac にはこうした設定がないので、常に 1 のままです。
- `Dark`：ダークモードがオンかどうか。
- `ReduceMotion`：ユーザーが動きを減らすよう求めているかどうか（iOS と macOS の「視差効果を減らす」、Android の「アニメーションを削除」、Windows の「アニメーション効果」のオフ、GNOME のアニメーションのオフ）。[モーション](motion.md)を参照してください。
- `ScreenReader`：VoiceOver、TalkBack、ナレーター、Orca のいずれかがオンかどうか。[アクセシビリティ](accessibility.md#スクリーンリーダーが有効かどうか)を参照してください。
- `WidthClass`、`HeightClass`：ウィンドウがアプリに与えている広さを、軸ごとにプラットフォームの分類で表したもの。値は `ui.SizeClassCompact`、`ui.SizeClassMedium`、`ui.SizeClassExpanded` のいずれかです。コンテンツの横にサイドバーを置いたり、リストの横に詳細を並べたりするときは、独自に決めた幅ではなく、これらの値で分岐してください。iOS では UIKit のサイズクラス（compact か regular で、regular は Expanded として扱います）を、Android では Material のウィンドウサイズクラス（幅が 600 dp 未満なら compact、840 dp 未満なら medium、それ以上なら expanded。高さが 480 dp 未満なら compact、900 dp 未満なら medium）を返します。そのため、UIKit が regular とみなす縦向きの iPad mini は iOS では Expanded になる一方、同じ大きさのタブレットは Android では Medium になります。Android の Material では、その大きさではまだ 1 ペインで表示するからです。どちらも、それぞれのプラットフォームの流儀に従っています。macOS、Windows、Linux、Web には独自の分類がないので、ウィンドウのサイズから Material の分類を求めます。
- `Locale`、`Languages`、`FirstWeekday`、`HourCycle`：ユーザーの地域の書式のロケール、ユーザーが読める言語、週の始まりの曜日、時刻の表記（12 時間制か 24 時間制か）。ほとんどのアプリは、[`ui.Locale` と `ui.FormatLocale`](i18n.md) を通してこれらを読みます。
- `TimeZone`：デバイスのタイムゾーン（IANA の名前）。Asuka はこれを Go の `time.Local` に設定するので、`time.Now().Format` はデバイスの時刻を表示します。

セマンティックカラーとフォントは、ダークモードと文字サイズに自動で追従します。それらでまかなえない部分は、環境を読んで対応してください。

```go
env := ui.UseEnvironment()
ui.Show(signal.Func(func() bool { return env.Get().WidthClass == ui.SizeClassExpanded }),
	func() ui.Node { return ui.Row(ui.Style{Grow: 1}, sidebar(), detail()) },
	func() ui.Node { return list() })
```

### プラットフォーム

`ui.Platform()` は、アプリが動いているシステムを返します。値は `ui.PlatformIOS`、`ui.PlatformAndroid`、`ui.PlatformMacOS`、`ui.PlatformWindows`、`ui.PlatformLinux`、`ui.PlatformWeb` のいずれかです。返すのはデバイスのプラットフォームで、これは `asuka dev` でスマートフォンの Go コードを手元のコンピューターで動かしている場合も同じです。そのため、`runtime.GOOS` やビルドタグではなく、これで分岐してください。値が変わることはないので、アクセサでもエフェクトでもない、普通の値です。

```go
variant := ui.ButtonFilled
if ui.Platform() == ui.PlatformIOS {
	variant = ui.ButtonGlassProminent
}
```

`UseEnvironment` と同じく、コンポーネントの組み立て中に呼び出してください。`asuka dev` は一つのプロセスで iPhone と Android のスマートフォンを同時に扱うことができ、それぞれのプラットフォームが異なるからです。イベントハンドラーやゴルーチンで使うときは、値を変数に保存しておきます。

プラットフォームのビューは、何もしなくても本来の見た目になります。`ui.Platform()` は、アプリが自分で描くものを各プラットフォームのデザインに合わせるために使います。たとえば [`examples/usage`](../../examples/usage) では、iOS では UIKit の inset grouped リストを、Android では Material 3 のリストを描いています。

## 永続化される値

シグナルは、アプリが動いている間だけ存在します。`store.Signal(key, def)` は、値がアプリの起動をまたいで保持されるシグナルを作ります。保存された値があればその値から、なければ `def` から始まり、設定された値はそのたびに保存されます。

```go
units := store.Signal("units", "metric")
```

これは設定のような小さな値のためのものです。ストアとデータベースについては[データ](data.md)を参照してください。なお `asuka dev` では、コードを何も書かなくても、すべてのシグナルの値がリロードをまたいで保持されます（[はじめに](getting-started.md#実行)を参照）。

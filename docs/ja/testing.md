# テスト

アプリの UI は Go の関数なので、`go test` でテストできます。パッケージ `uitest` は、デバイスもウィンドウもディスプレイもなしにコンポーネントをマウントし、ユーザーと同じように、画面に表示されているものを手がかりに操作します。

```go
package main

import (
	"testing"

	"github.com/egoist/asuka/uitest"
)

func TestCounter(t *testing.T) {
	a := uitest.Mount(t, Counter)
	a.Tap("Add")
	a.Tap("Add")
	a.Expect("Count: 2")
}
```

```sh
go test ./...
```

Go が動く環境ならどこでも、CI でも実行でき、シミュレーターもエミュレーターも要りません。プラットフォームの実際のビューでテストする方法は、後述の[プラットフォームでのテスト](#プラットフォームでのテスト)を参照してください。

## 仕組み

`uitest.Mount` は、コンポーネントを Asuka のフェイクホスト（`host/fake`）にマウントします。フェイクホストはプラットフォームのビューを作る代わりに、コアが作るビューを保持します。それ以外はすべて、実際に動くときのアプリそのままです。シグナルとエフェクト、コアのレイアウト、ナビゲーション、行をバインドするリスト、シート、アラートが動きます。テストのゴルーチンがメインスレッドになります。

特に指定しなければ、アプリは iPhone（iOS、390 × 844 ポイント）で動いているものとして実行されます。

```go
uitest.Mount(t, App, uitest.Platform(ui.PlatformAndroid))  // Pixel の画面
uitest.Mount(t, App, uitest.Platform(ui.PlatformMacOS))    // 800 × 600 のウィンドウ
uitest.Mount(t, App, uitest.Size(1024, 1366), uitest.Dark())
uitest.Mount(t, App, uitest.Languages("ar"))               // アラビア語の翻訳で、右から左
uitest.Mount(t, App, uitest.FontScale(1.5))
uitest.Mount(t, App, uitest.Environment(func(e *ui.Environment) { e.ScreenReader = true }))
```

プラットフォームと異なるのは、テキストの測り方です。フォントに関係なく、1 文字 8 ポイント、1 行 20 ポイントとして測ります。フレームはコアの実際のレイアウトから得られるので、ビュー同士の位置関係は正しく保たれますが、ポイント単位の正確なサイズは実機と一致しません。

## 画面に表示されているもの

テストでは、ユーザーに見えるテキストで対象を指定します。テキスト、ボタンのタイトル、フィールドのテキストやプレースホルダー、アクセシビリティラベル、画面のタイトル、タブ、バーボタン、ツールバーの項目、アラートのボタン、開いているメニューの項目です。テキストは完全一致で照合されます。

```go
a.Expect("Inbox", "Message 20")   // どれか一つでも表示されていなければテストを失敗させる
a.ExpectNo("Message 1")           // どれか一つでも表示されていればテストを失敗させる
if a.Has("Signed in") { ... }
a.Texts()                         // 表示されているすべてのテキスト（読む順）
a.Get("Add")                      // それを表示しているビュー：Kind、Frame、props
a.All(ops.KindToggle)             // 表示されている、ある種類のビュー
a.Disabled("Save")                // タップできない状態かどうか
```

対象になるのは、ユーザーに見えるものだけです。

- 各スタックのいちばん上の画面（その下の画面は見えません）
- 選択されているタブのページと、タブバーにあるすべてのタブのタイトル
- ほかのものの上に表示されているシート、アラート、ポップオーバー
- リストの高さに収まる行。Asuka はプラットフォームに求められたときにリストの行をバインドします。フェイクホストもコレクションビューと同じように行を求めます。その先の行は `ScrollTo` でバインドされます
- 非表示のものは含みません

チェックが失敗すると、メッセージに画面の内容が含まれます。表示されている各ビューが、種類、フレーム、テキストとともに並ぶので、代わりに何が表示されていたかがわかります。

## ユーザーの操作

各アクションは、画面が落ち着いてから戻ります。つまり、変更されたものはレイアウトされ、リストはバインドされ、アプリのゴルーチンがポストしたものは実行済みになっています。

```go
a.Tap("Add")                  // ボタン、行、タブ、バーボタン、アラートのボタン
a.LongPress("Message")        // 長押し、またはコンテキストメニュー
a.Type("Email", "ada@example.com")  // そのプレースホルダーかラベルを持つフィールド
a.Submit("Email")             // そのフィールドでリターン（""：最後に入力したフィールド）
a.Toggle("Notifications")     // そのラベルの横にあるスイッチ
a.Slide("Volume", 0.8)
a.Select("Week")              // セグメンテッドコントロール、ピッカー、ホイールの中で
a.Swipe("Message 3", "Delete")  // リストの行のスワイプアクション
a.Refresh("Message 20")       // それを表示しているものを引っ張って更新
a.ScrollTo("Message 1")       // どれかのリストに表示されるまでスクロール
a.Back()                      // 戻るジェスチャー、Android の「戻る」、Escape
a.Dismiss()                   // 下へのスワイプ、外側のタップ
a.Key("primary+n")            // キーボードショートカット：Mac では ⌘N、それ以外では Ctrl+N
a.OpenURL("myapp://note/3")
```

タップは、指が触れるのと同じ場所に届きます。行の中のテキストをタップすれば行が、ボタンの中のテキストをタップすればボタンがタップされます。メニューを持つビューはメニューを開き、次の `Tap` でその中から選びます。シート、アラート、ポップオーバーが表示されているあいだは、それだけがタップを受け付けます。その下にあるものをタップしようとすると、テストは失敗します。

自分のテキストを持たないコントロール（スイッチ、スライダー、プレースホルダーのないフィールド）は、ユーザーがフォームを読むのと同じようにラベルで見つけます。ラベルと同じ行にあるコントロールか、ラベルより下でいちばん近いコントロールです。アクセシビリティラベル（`ui.ViewProps.Label`）を持つビューは、その中のコントロールの名前になります。

`Back` は、開いているメニューを閉じるか、いちばん上の画面をポップします。ポップする画面がなければ、いちばん上のシート、アラート、ポップオーバーを閉じます。`ui.OnBack` を使えば、これを確認の問いかけに変えられます。

```go
a.Type("Message", "On my way")
a.Back()
a.Expect("Discard this reply?")
```

## 同じテキストが複数ある場合

タップしようとするものが画面の複数の場所に表示されていると、テストは失敗します。たとえば、どちらも Star というタイトルのバーボタンとツールバーの項目や、すべての行にある Remove ボタンです。`In` で画面の範囲を絞り込めます。失敗したときは、どの範囲に絞り込むべきかがメッセージに示されます。

```go
a.In(uitest.Toolbar).Tap("Star")        // ナビゲーションバーのものではなく
a.In(uitest.NavigationBar).Tap("Edit")
a.In(uitest.Content).Has("Inbox")        // 画面自体のビューで、タイトルではなく
a.In(uitest.NextTo("Ada")).Tap("Remove") // Ada と同じ行の Remove
a.In(uitest.Under(a.Row("Ada"))).Tap("Remove")  // Ada を表示しているリストの行の中
```

`NextTo` は位置で判断するので、二つを含むビューがない場合にも使えます。`ui.Row` は自分のビューを作らないためです。

## アプリを待つ

ゴルーチンで処理するコード（リクエスト、タイマー）は、`ui.Post` で結果をポストします。テストは、画面を落ち着かせるときにポストされたものを実行します。まだ終わっていない処理を待つには次のようにします。

```go
a.Tap("Refresh")
a.WaitFor("Loaded 3 items")    // 表示されるまで
a.WaitGone("Loading…")         // 表示されなくなるまで
a.Until("the upload to finish", func() bool { return uploads.Peek() == 0 })
```

待機中は、アプリが何かをポストするまでブロックし、ポストされたらもう一度チェックします。10 秒、または `Mount` に渡した `uitest.Timeout` の時間が過ぎると失敗します。`uitest` の中にスリープするものは一つもありません。

`ui.OnFrame` のアニメーションは、`a.Frames(n)` で 1 秒 60 フレームとして 1 フレームずつ進みます。テストがシグナルに書き込んだ値は、`a.Settle()` のあとに画面に反映されます。

```go
items.Set(nil)
a.Settle()
a.Expect("No items")
```

## プラットフォームのサービスとネイティブパッケージ

プラットフォームに問い合わせる Asuka のパッケージ（`asuka/sdk/...`：カメラ、位置情報、通知など）はホストを呼び出します。テストではそれがフェイクホストです。応答するものがないので、そのサービスがないプラットフォームと同じようにエラーが返ります。Asuka 自身のテストと同じように、`Setup` を使えば、アプリをマウントする前にモジュール名ごとの応答（`h.Modules`、`h.AsyncModules`）をフェイクホストに与えられます。

[ネイティブパッケージ](native.md)のモジュールは Go のインターフェースです。そのため、いちばん簡単なテスト方法は、コンポーネントにはインターフェースを受け取らせ、テストでは独自の実装を渡すことです。

```go
type fakeBattery struct{ level float64 }

func (b fakeBattery) Level() (float64, error) { return b.level, nil }
func (b fakeBattery) Watch(ctx context.Context, changed func(float64)) error { return nil }

func TestLowBattery(t *testing.T) {
	a := uitest.Mount(t, func() ui.Node { return Status(fakeBattery{0.1}) })
	a.Expect("Battery low")
}
```

`a.Host` はフェイクホストそのものです。ここで紹介したメソッドでは扱えないこと、たとえば送られた op の確認、ドラッグ、特定のビューへのキー入力に使います。

## スナップショット

`a.Snapshot()` は、画面に表示されているものを 1 ビュー 1 行で書き出します。各行には、種類、フレーム、テキスト、状態（スイッチのオン、スライダーの値、無効）が入ります。`MatchSnapshot` は、それを `testdata` にあるファイルと比較します。

```go
a.MatchSnapshot("signed-in")   // testdata/TestSettings/signed-in.txt
```

```
View 0,0 390x844
  Text 16,16 358x20 "Count: 0"
  Button 16,44 358x44 "Add"
  Button 16,96 358x44 "Reset" disabled
```

`go test -uitest.update` でファイルを書き出します。変更を `git diff` で確認してからコミットしてください。スナップショットを使うと、消えてしまったビュー、画面に表示される文言の変化、ずれたレイアウトを検出できます。

## リーク

テストが終わると、`uitest` はアプリをアンマウントします。そのあとも残っているビューがあると、テストは失敗します。原因は、本来ビューを破棄すべきオーナーの外で作られたエフェクトやビューです（[基本概念](concepts.md)を参照）。

## プラットフォームでのテスト

`uitest` がテストするのは、アプリの振る舞い、つまり何が表示され、ユーザーが操作すると何が起きるかです。それを各プラットフォームがどう描画するかはプラットフォームの領分で、各ホストがコアから送られたものを正しく描画することは Asuka 自身のテストで確認しています（[開発](development.md#テスト)を参照）。各プラットフォームでアプリを確認するには、次の方法があります。

- `asuka dev --platform ios|android|web` で、シミュレーター、エミュレーター、実機、またはブラウザーでアプリを実行します。保存するたびにリロードされます。
- デスクトップでは、`ASUKA_SNAPSHOT=out.png` を指定すると、ウィンドウが表示されたあと、フォーカスを奪わずにウィンドウを PNG に書き出して終了します（`ASUKA_DUMP=1` を指定すると、その隣にビューツリーも書き出します）。Windows と Linux では、`ASUKA_SCRIPT` で先に操作を実行できます（`tap:Add`、`type:hello`）。[開発](development.md#スナップショットとスクリプト)を参照してください。
- Linux では、Xvfb（`xvfb-run -a dbus-run-session`）の下で動かせば、ディスプレイがなくても CI でこれが使えます。

アプリをマウントするテストは、並列に実行（`t.Parallel`）できません。デバイスと同じく、アプリのコアは 1 プロセスに一つだからです。

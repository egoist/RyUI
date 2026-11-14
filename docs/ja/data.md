# データ

アプリはデータをいくつかの場所に保存します。小さな値は `asuka/sdk/store` に、レコードは `asuka/sdk/sqlite` で SQLite に、秘密情報は `asuka/sdk/secure` でプラットフォームのキーチェーンに、アプリ独自のファイルは Go の `os` で保存します。このページでは、それぞれの使い方に加えて、`net/http` によるネットワーク通信、ファイルの監視、そしてどの呼び出しがメインスレッドをブロックしうるかを説明します。

## データの保存場所

`store.Dir()` はアプリのデータディレクトリです。`asuka/sdk/store` のファイルはここに置かれ、`sqlite.Open` に単なる名前を渡して作ったデータベースもここに置かれます。アプリ独自のファイルもここに置いてください。

| | `store.Dir()` |
|---|---|
| iOS | アプリのコンテナー内の Application Support（[共有シート](device.md#アプリ独自の共有シート-appsharesheet)を使う場合は App Group 内） |
| Android | アプリの files ディレクトリ |
| macOS | `~/Library/Application Support/<name>` |
| Windows | `%LOCALAPPDATA%\<name>` |
| Linux | `$XDG_DATA_HOME/<id>`（`~/.local/share/<id>`。Flatpak ではサンドボックス自身のもの） |
| Web | `/data/<name>`。ページがブラウザーのストレージ（IndexedDB）に保存する、メモリ上のファイルシステムにあります |

`<name>` は `asuka.json` の `name`、`<id>` は `id` です。CLI を使わずにアプリを実行した場合（`go run`）は、どちらもプログラムの名前になります。ディレクトリは、最初にそこへ書き込むものによって作られます。Windows と Linux では、ホストもここに独自のファイルをいくつか置きます（メインウィンドウのサイズと位置、アプリで選んだ言語）。ブラウザーでは、ディレクトリ内のファイルはアプリの起動前に読み込まれ、変更されると少し後に、またページが非表示になったときに書き戻されます。詳しくは [Web](web.md) を参照してください。

`asuka dev` では Go のコードが手元のコンピューターで動きます（デスクトップアプリの場合も、ホットリロードモードで動かす iOS と Android の場合も）。このとき `store.Dir()` は、そのコンピューター上にある `asuka dev` 専用のディレクトリ（ユーザーのキャッシュディレクトリの `asuka/` 以下に、アプリとプラットフォームごとに一つ）になります。そのため、開発中に試したことが、インストールしたビルドのデータと混ざることはありません。ホットリロードモードでは、ストア、データベース、アプリのファイルはデバイスではなくコンピューター上にあります。例外は `asuka/sdk/secure` で、リリースビルドと同じくプラットフォームの保存先を使います（開発用アプリではデバイスのキーチェーン）。Web では、`asuka dev` のページは開発サーバーのアドレスに紐づけて、ファイルをブラウザーに保存します。

## 小さな値：asuka/sdk/store

`asuka/sdk/store` は、設定、サインイン中のユーザーの名前、最後に開いていたタブといった小さな値を、起動をまたいで保持します。値は JSON として一つのファイル（`store.Dir()` 内の `store.json`）に保存されます。書き込みはメインスレッドの外で行われ、値が変わってから少し後と、アプリがバックグラウンドに移ったときに書き込まれます。バックグラウンドに移ったときとは、Mac では非表示・最小化・終了したとき、Windows ではウィンドウがフォーカスを失ったとき、Linux ではどのウィンドウも見えなくなったとき（それぞれが非表示・最小化・別のワークスペースにある状態）と、終了したとき・セッションが終わったとき・SIGTERM を受け取ったとき、ブラウザーではページが非表示になったときです。これはデータベースではありません。レコードが多い場合や大きい場合は SQLite を使ってください。

`store.Signal` は、値が保存されるシグナルです。保存されている値（初回はデフォルト値）で始まり、設定された値はそのつど保存されます。

```go
func Settings() ui.Node {
	dark := store.Signal("dark", false) // 初回は false、以降は最後に設定された値
	return ui.Toggle(ui.ToggleProps{Value: dark})
}
```

ほかのシグナルと同じく、メインスレッドで作ってください。パッケージレベルの変数にしてもかまいません（`examples/downloader` には `var wifiOnly = store.Signal("wifiOnly", false)` があります）。呼び出すたびに新しいシグナルが作られます。同じキーで作った 2 つのシグナルは互いに追従しないので、一つだけ作って共有してください。

`Get`、`Set`、`Delete` は値を直接扱います。`Set` は `encoding/json` でエンコードできるものなら何でも受け取ります。`Get` はポインターにデコードし、キーに値がない場合や、値がその型にデコードできない場合は false を返します。

```go
type Prefs struct {
	Theme    string
	FontSize int
}

store.Set("prefs", Prefs{Theme: "dark", FontSize: 17})

var p Prefs
if !store.Get("prefs", &p) {
	p = Prefs{Theme: "light", FontSize: 15}
}

store.Delete("prefs")
```

`store.Flush()` は、保留中の内容が書き込まれるまで待ちます。アプリがバックグラウンドに移るときに Asuka が呼ぶので、自分で呼ぶ必要があるのはテストのときだけです。

## レコード：sqlite プラグイン

`asuka/sdk/sqlite` はアプリの SQLite データベースで、[プラグイン](plugins.md)として提供されます。インポートすると、CLI がそのライブラリをアプリと一緒に配布します。中身は SQLite そのもの、つまりその C のコードで、一つのリリースをすべてのプラットフォームで同じ方法でビルドしています。それに Asuka の小さなライブラリが加わっており、Go からの 1 回の呼び出しでステートメント全体を実行し、結果の行をチャンク単位で返します。Go からは cgo を使わずに呼び出します。macOS、Linux、iOS、Android では purego を、Windows では syscall を使います。ブラウザーでは、同じ C のコードを WebAssembly にビルドしたものを使います（[Web](web.md#sqlite)）。[`examples/notes`](../../examples/notes) で使っています。配布の仕組みは[ライブラリ](#ライブラリ)で説明します。

### データベースを開いてマイグレーションする

```go
var db *sqlite.DB

func main() {
	var err error
	db, err = sqlite.Open("notes.db")
	if err != nil {
		panic(err)
	}
	err = db.Migrate(
		// 1
		`create table notes (id integer primary key, text text not null, done bool not null default 0)`,
		// 2
		`alter table notes add column created_at integer not null default 0;
		 create index notes_done on notes (done)`,
	)
	if err != nil {
		panic(err)
	}
	app.Run(App)
}
```

`Open` には、名前（`store.Dir()` 内のファイル。ディレクトリがなければ作られます）、絶対パス、または `":memory:"` を渡します。`Open` は WAL（write-ahead logging）と外部キーを有効にし、ビジータイムアウトを 5 秒に設定します。

データベースには、書き込み用の接続が一つあります。ファイルの場合はそれに加えて、読み込み用の接続が最大 3 つあり、読み込みで必要になったときに開かれます。書き込みは書き込み用の接続を順番に使います。ある書き込みは別の書き込みの完了を待ち、トランザクションは終わるまで接続を占有します。ビジータイムアウトより長く待った書き込みは `ErrBusy` で失敗します（`db.Tx` の関数の中で `db.Exec` を呼ぶと、こうなります。そこでは `tx.Exec` を使ってください）。読み込みは書き込みを待つことがなく、最後のコミットの時点のデータベースを読みます。`":memory:"` では、読み書きに一つの接続を使います。

`Migrate` はスキーマを最新の状態にします。引数はそれぞれデータベースのバージョンを表し、順に並べます。各バージョンの SQL（セミコロンで区切った複数のステートメント）は、トランザクションの中で一度だけ実行され、そのバージョンは `PRAGMA user_version` に記録されます。スキーマを変えるときはステップを追加してください。すでに配布したステップは決して変更してはいけません。それを実行済みのデバイスでは、もう一度実行されることはないからです。

`db.Close()` でデータベースを閉じます。ほとんどのアプリは、起動時にデータベースを一つ開いて、それを使い続けます。

### クエリ

`sqlite.Query[T]` はクエリを実行し、結果の行を `T` のスライスに読み込みます。`T` が構造体の場合、各カラムは同じ名前のフィールドに入ります。名前は、フィールドの `db` タグ、またはフィールド名で、大文字と小文字を区別せず、カラム名のアンダースコアを無視して照合します（`created_at` は `CreatedAt` に入ります）。対応するフィールドのないカラムは読み飛ばされ、`db:"-"` タグを付けたフィールドには何も入りません。`T` がそれ以外の型の場合は、最初のカラムが読み込まれます。

```go
type Note struct {
	ID      int64
	Text    string
	Done    bool
	Created int64  `db:"created_at"`
	Draft   string `db:"-"`
}

notes, err := sqlite.Query[Note](db, `select * from notes where done = ? order by id`, false)

n, ok, err := sqlite.QueryOne[Note](db, `select * from notes where id = ?`, 1)

count, _, err := sqlite.QueryOne[int](db, `select count(*) from notes`)
```

`QueryOne` は最初の行を返します。行がなければ `ok` は false です。

カラムの値は、可能な範囲でフィールドの型に変換されます。整数を `string` のフィールドに入れると数字の文字列に、テキストを `int` のフィールドに入れると、整数として読めればその値に、整数を `bool` に入れると 0 以外かどうかになります。収まらない値（実数を `int` に、300 を `int8` に）はエラーになります。NULL のときはゼロ値のまま、ポインターのフィールド（`*string`）なら nil になります。`time.Time` のフィールドは、テキスト（`sqlite.TimeFormat`。SQLite の日付・時刻関数が書き出す形式で、RFC 3339）か、1970 年からの秒数（`unixepoch()`）を読み込みます。フィールドのポインターが `sqlite.Scanner` を実装している場合（`sql.NullString` やアプリ独自の型）は、そのフィールドが自分で値を読み込みます。埋め込んだ構造体のフィールドは、外側の構造体のフィールドとして扱われます。

### 引数

`?` は引数を順番に受け取り、`?2` は 2 番目の引数を受け取ります。`:name`、`@name`、`$name` は `sqlite.Named` の引数を受け取ります。

```go
db.Exec(`update notes set text = :text where id = :id`,
	sqlite.Named("id", 1), sqlite.Named("text", "Buy milk"))
```

引数に使えるのは、整数、浮動小数点数、bool（1 または 0）、文字列、`[]byte`（BLOB。nil は NULL）、`time.Time`（`sqlite.TimeFormat` 形式のテキスト：`2006-01-02 15:04:05.999999999-07:00`。SQLite の日付関数で読めて、同じタイムゾーン内なら時刻順に並びます）、nil、これらのいずれかへのポインター（nil は NULL）、これらを基にしたアプリ独自の型（`type Status string`）、`driver.Valuer`（`sql.NullString`）です。

### 実行とトランザクション

`db.Exec` は、ステートメント（insert、update、delete、create など）か、セミコロンで区切った複数のステートメントを実行し、`sqlite.Changes`（`LastInsertID` と `RowsAffected`）を返します。`db.Tx` は関数をトランザクションの中で実行します。関数が nil を返せばコミットし、エラーを返すか（そのエラーは `Tx` が返します）パニックを起こせばロールバックします。その中では、データベースの代わりに `*sqlite.Tx` を渡して、`tx.Exec` で書き込み、`sqlite.Query` と `sqlite.QueryOne` で読み込みます。

```go
err := db.Tx(func(tx *sqlite.Tx) error {
	if _, err := tx.Exec(`update notes set done = 1 where id = ?`, 1); err != nil {
		return err // ロールバックされる
	}
	left, _, err := sqlite.QueryOne[int](tx, `select count(*) from notes where done = 0`)
	if err != nil {
		return err
	}
	fmt.Println(left, "left")
	return nil // コミットされる
})
```

### メインスレッドの外での書き込み

`Exec`、`Tx`、`Query`、`QueryOne`、`Migrate` はディスクを待ちます。メインスレッドから書き込むときは `ExecAsync` と `TxAsync` を使ってください。これらはメインスレッドの外で実行され、終わるとメインスレッドで `done` を呼びます。`done` が nil の場合、エラーはログに出力されます。

```go
db.ExecAsync(`insert into notes (text, created_at) values (?, ?)`, []any{"Buy milk", time.Now().Unix()},
	func(r sqlite.Changes, err error) {
		if err != nil {
			status.Set("Couldn't save: " + err.Error())
			return
		}
		status.Set(fmt.Sprintf("Saved note %d", r.LastInsertID))
	})

db.TxAsync(func(tx *sqlite.Tx) error {
	if _, err := tx.Exec(`delete from notes where done = 1`); err != nil {
		return err
	}
	_, err := tx.Exec(`update notes set text = trim(text)`)
	return err
}, nil)
```

メインスレッドから読み込むときは、ライブクエリ（後述）を使うか、ゴルーチンでクエリを実行してから、結果の行を `ui.Post` でメインスレッドに渡します。

### ライブクエリ

`sqlite.Watch[T]` はライブクエリで、結果の行を値に持つシグナルです。行はすぐにメインスレッドの外で取得され、クエリが読むテーブルへの書き込みがコミットされるたびに取得し直されます。コンポーネントの中で作ってください。コンポーネントのオーナーと一緒に止まります。

```go
func Notes() ui.Node {
	notes := sqlite.Watch[Note](db, `select * from notes order by id desc`)
	return ui.Text(signal.Func(func() string {
		r := notes.Get()
		switch {
		case r.Loading:
			return "Loading…"
		case r.Err != nil:
			return r.Err.Error()
		}
		return fmt.Sprintf("%d notes", len(r.Rows))
	}))
}
```

値は `sqlite.Result[T]` です。`Rows` は最新の行、`Err` はクエリが最後に受け取ったエラー、`Loading` は最初の取得が終わるまで true です。`examples/notes` のように `Rows` を `ui.List` に渡すと、リストがテーブルに追従します。

ライブクエリが再実行されるのは、読んでいるテーブルが変わったときだけです。どのテーブルを読むかは SQLite がクエリを準備するときに把握し、ビューの背後にあるテーブルも含まれます。そのいずれかへの書き込みがコミットされると再実行されます。トリガーや外部キーのアクションによって書き込まれた行も対象です。ほかのテーブルへの書き込みでは実行されません。取得中に届いた書き込みは、書き込みごとではなく、まとめてもう 1 回の取得になります。

`sqlite.WatchWith[T]` は、変化する引数を取る `Watch` です。引数はシグナルを読む関数（検索フィールドのテキスト、ページサイズなど）から取得し、データベースが変わったときと同じく、引数が変わったときにもクエリを再実行します。新しい行が届くまでは古い行が残ります。取得中に引数が変わった場合、古い引数で取得した行は破棄されます。

```go
func Search() ui.Node {
	q := signal.New("")
	notes := sqlite.WatchWith[Note](db, `select * from notes where text like ? order by id`,
		func() []any { return []any{"%" + q.Get() + "%"} })
	return ui.Column(nil,
		ui.TextField(ui.TextFieldProps{Value: q, Placeholder: ui.Val("Search")}),
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%d found", len(notes.Get().Rows)) })),
	)
}
```

書き込みを行うクエリ（`insert ... returning`）は、`Exec` と同じく書き込みとして検知されます。

### エラー

SQLite が返すエラーは `*sqlite.Error` で、結果コード（拡張結果コード。`2067` は `SQLITE_CONSTRAINT_UNIQUE`）とメッセージを持ちます。`errors.Is` は、パッケージの `Err` 値とコードで照合します。種類全体を表す値は、その種類のどのコードにもマッチします。

```go
_, err := db.Exec(`insert into users (email) values (?)`, email)
switch {
case errors.Is(err, sqlite.ErrConstraintUnique):
	status.Set("That email is taken")
case errors.Is(err, sqlite.ErrConstraint): // それ以外の制約違反
	status.Set("Check the form")
case err != nil:
	status.Set(err.Error()) // sqlite: no such table: users (SQLITE_ERROR)
}
```

### バックアップ

`db.Serialize()` は、データベースをファイルに保存したときと同じバイト列を返します。どこに保存しても、共有しても、アップロードしてもかまいません。`db.Restore(data)` はデータベースをそのバイト列で置き換え、すべてのライブクエリを再実行します。Web を含むすべてのプラットフォームで、データベースを丸ごとコピーするにはこの方法を使います。ファイルシステムがある環境では、`VACUUM INTO 'path'` でコピーをファイルに書き出すこともできます。

`db.Changed()` は、すべてのライブクエリを再実行します。データベースからは見えない変更（別のプロセスによる変更や、ファイルの置き換え）の後に呼んでください。

### ライブラリ

SQLite は、すべてのプラットフォームでリリース 3.54.1 を使います。FTS5、R*Tree、Geopoly、数学関数、`dbstat`、`STAT4`、`soundex` を組み込み、拡張機能の動的読み込み（loadable extensions）は無効にし、一時テーブルはメモリ上に置く設定です（スマートフォンには一時ディレクトリがないことがあるため）。これがプラグインのライブラリ `asqlite` で、プラットフォームごとにビルドされ、プラグインの GitHub リリースに公開されています。マニフェスト（`sdk/sqlite/asuka-plugin.json`）には各ファイルとその SHA-256 が記されており、CLI とパッケージはそこから一度だけダウンロードして、ユーザーのキャッシュに保存します。アプリの開発者に C コンパイラーは必要ありません。

- **`asuka build`** は、ほかのプラグインと同じように、ライブラリをアプリと一緒に配布します（[プラグイン](plugins.md#ライブラリの置き場所)）。macOS ではアプリの `Contents/Frameworks` に（アプリと一緒に署名されます）、Windows では `.exe` の隣に、Linux ではインストール先の `lib/<app>` に、iOS アプリでは `asqlite.framework` として、APK では各 ABI に入ります。
- デスクトップでの **`go run`、`go test`、`asuka dev`** は、公開されたライブラリをユーザーのキャッシュから読み込みます。スマートフォンでの `asuka dev` は、ビルドと同じようにライブラリをアプリに入れます。ほかのアプリに組み込むアプリ（`asuka build --xcframework`）には `asqlite.xcframework` が付属するので、組み込む側のアプリもそれを組み込んでください（[配布](shipping.md#既存アプリへの組み込み)）。
- **`ASUKA_SQLITE_LIB`** を指定すると、代わりにそのライブラリを読み込みます（プラグインのライブラリのいずれかである必要があります。パッケージが SQLite のバージョンを確認します）。

SQLite やプラグインの C のコードを変更するには、リポジトリをチェックアウトして作業します。`go generate ./sdk/sqlite` は、すべてのプラットフォームのライブラリ（Apple のプラットフォームは Xcode、Android は Android NDK、Linux・Windows・Web は Zig）を `sdk/sqlite/build` にビルドします。以降、チェックアウト内のテストやサンプルはこれを使います。ビルドしたライブラリは `go run ./cmd/asuka plugin publish sdk/sqlite` で公開します。

## 秘密情報：asuka/sdk/secure

`asuka/sdk/secure` は、トークンやパスワードといった小さな秘密情報を、プラットフォームの機能で暗号化して保存します。秘密情報のためのもので、データのためのものではありません。それ以外のものはすべて、ストアかデータベースに保存してください。

- **iOS**：キーチェーンに、アプリの bundle ID の汎用パスワードとして保存します。デバイスの初回ロック解除後に読み取れ、このデバイスでのみ使えます。
- **Android**：各値を Android Keystore 内の鍵で暗号化（AES-GCM）し、アプリのプライベートな preferences に保存します。鍵が Keystore の外に出ることはありません。
- **macOS**：ログインキーチェーンに、アプリの bundle ID（なければアプリの名前）の汎用パスワードとして保存します。「キーチェーンアクセス」で確認できます。`asuka dev` のアプリや Developer ID のないビルドのように、アドホック署名のアプリでも使えます。
- **Windows**：資格情報マネージャーに、`<name>/<key>` という名前のユーザーの汎用資格情報として保存します。このコンピューターにのみ保存され（ローミングしません）、コントロールパネルの「資格情報マネージャー」で確認できます。値は最大 2560 バイトです。
- **Linux**：libsecret と Secret Service（GNOME Keyring、KWallet、KeePassXC）を通じて、ユーザーのキーリングに保存します。キーごとに、アプリ独自のスキーマ（アプリの ID）の項目を作り、`<name>: <key>` というラベルを付けてデフォルトのコレクションに保存します。キーリングのアプリ（GNOME の「パスワードと鍵」）で確認できます。libsecret がない場合や、キーリングが動いていない場合は、呼び出しはその旨を伝えて失敗します。
- **Web**：ブラウザーにはキーチェーンがありません。各値を、WebCrypto で抽出不可として作った鍵で暗号化（AES-GCM、256 ビット）し、鍵と一緒にアプリの IndexedDB データベースに保存します。そのため、どのスクリプトも鍵のバイト列を読むことはできず、値が平文のままディスクに置かれることもありません。ただし、アプリのオリジンで動くものであれば、その鍵で値を復号させることはできます。これは Web アプリでできる最善の方法であって、キーチェーンではありません。https か localhost が必要です。

メインスレッドでは、コールバックを取る `Load`、`Save`、`Remove` を使います。コールバックはメインスレッドで呼ばれます。

```go
func Account() ui.Node {
	status := signal.New("")
	secure.Load("token", func(token string, ok bool, err error) {
		switch {
		case err != nil:
			status.Set(err.Error())
		case !ok:
			status.Set("Signed out")
		default:
			status.Set("Signed in")
		}
	})
	return ui.Column(nil,
		ui.Text(status),
		ui.Button(ui.ButtonProps{Title: ui.Val("Sign out"), OnPress: func() {
			secure.Remove("token", func(err error) { status.Set("Signed out") })
		}}),
	)
}

func signIn(token string) {
	secure.Save("token", token, nil) // nil：コールバックなし
}
```

ゴルーチンからは、`Get`、`Set`、`Delete` を使います。これらはプラットフォームの処理を待ってから戻ります。メインスレッドでは決して呼ばないでください。

```go
go func() {
	token, ok, err := secure.Get("token")
	if err != nil || !ok {
		return
	}
	// ... token を使う ...
}()
```

## ファイルとネットワーク

ファイルは Go の `os`、ネットワークは Go の `net/http` で扱います。Asuka が独自に加えるものはありません。ファイルは `store.Dir()` に保存してください。Asuka のパッケージが渡すファイル（撮影した写真、録画した動画、選択した写真、ドロップやペーストされたファイル）は、アプリのキャッシュや一時ディレクトリにあり、システムに消去されることがあります。残しておきたいものは `store.Dir()` に移動してください。ブラウザーでは、`os` はページのファイルシステムを操作します。`store.Dir()` は保存されますが、それ以外（`/tmp` やキャッシュ）はページが開いている間だけ残ります。また、ブラウザーでの `net/http` はブラウザーの `fetch` なので、別のオリジンへのリクエストには、そのオリジンの CORS ヘッダーが必要です。

どちらも処理を待つので、ゴルーチンで実行し、結果を `ui.Post` でメインスレッドに渡してください。

```go
func Avatar() ui.Node {
	path := signal.New("")
	status := signal.New("Downloading…")
	go func() {
		file := filepath.Join(store.Dir(), "avatar.jpg")
		err := download("https://example.com/avatar.jpg", file)
		ui.Post(func() {
			if err != nil {
				status.Set(err.Error())
				return
			}
			status.Set("")
			path.Set(file)
		})
	}()
	return ui.Column(nil,
		ui.Text(status),
		ui.Image(ui.ImageProps{Source: path}),
	)
}

func download(url, file string) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", res.Status)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, res.Body)
	return err
}
```

アプリが見えていない間も長いダウンロードを続けるには、[`asuka/sdk/background`](device.md#バックグラウンド処理-asukasdkbackground) を参照してください。接続が Wi-Fi か従量制かを知るには、[`asuka/sdk/network`](device.md#ネットワークの状態-asukasdknetwork) を参照してください。[`examples/downloader`](../../examples/downloader) では両方を使っています。

## ファイルの監視：asuka/sdk/watch

`asuka/sdk/watch` は、ファイルが変わったときにアプリに知らせます。アプリが表示しているフォルダー、エディターで開いているプロジェクト、ほかのプログラムが書き込む設定ファイル、ほかのアプリが保存したドキュメントなどを監視できます。スマートフォンや Web を含む、すべてのプラットフォームで動作します。

コンポーネントの中では、`watch.Path` を使います。コンポーネントがある間パスを監視し、変更のバッチごとにメインスレッドで関数を呼びます。ファイルはそこで読み直してください。

```go
func Folder(dir signal.Accessor[string]) ui.Node {
	names := signal.NewFunc[[]string](nil, slices.Equal)
	reload := func() {
		d := signal.Untrack(dir.Get)
		go func() {
			list := readNames(d) // os.ReadDir（メインスレッドの外で）
			ui.Post(func() { names.Set(list) })
		}()
	}
	signal.Effect(func() { dir.Get(); reload() }) // dir が変わったときにも
	w := watch.Path(dir, watch.Options{Ignore: watch.Patterns(".*")}, func([]watch.Event) {
		reload()
	})
	return ui.Column(nil,
		ui.Show(signal.Func(func() bool { return w.Err() != nil }), func() ui.Node {
			return ui.Text(signal.Func(func() string { return w.Err().Error() }))
		}),
		ui.For(names, func(n string) string { return n }, func(n signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
			return ui.Text(n)
		}),
	)
}
```

監視はアクセサに追従し（パスが変わると古い監視を止めて新しいパスの監視を始めます）、コンポーネントがなくなると止まります。大きなフォルダーの一覧の取得には時間がかかるため、監視はゴルーチンで始まります。監視が始まったかどうかは `w.Watching()` で、始められなかった理由（パスが存在しない、読めない、ファイルが多すぎる：`watch.ErrLimit`）は `w.Err()` でわかります。関数の実行中は監視が待機するので、アプリが忙しいときには、その間の変更が何回にも分かれず一つのバッチで届きます。

コマンドラインツールや同期ループのようにメインスレッドの外で使う場合は、`watch.New` で任意のゴルーチンから監視でき、`Next` がバッチを一つずつ返します。

```go
w, err := watch.New(ctx, dir, watch.Options{Recursive: true, Ignore: watch.Patterns("node_modules/", ".git/")})
if err != nil {
	return err
}
defer w.Close()
for {
	events, err := w.Next(ctx)
	if err != nil {
		return err // 閉じた後や、dir がなくなった後は io.EOF
	}
	for _, e := range events {
		fmt.Println(e) // "create src/a.go", "rename a.txt -> b.txt", ...
	}
}
```

イベントの `Op` は、`Create`、`Write`、`Remove`、`Rename`（`OldPath` 付き）、`Attrib`（パーミッション。`Options.Attributes` を指定した場合）、`Overflow` のいずれかです。`Path` は監視対象からの相対パスで、区切りはスラッシュ、監視対象そのものは `.` です。監視は、いま見えている状態と前に見た状態を比べます。そのため、イベントが伝えるのは何が違うかであって、そうなるまでの一つひとつの手順ではありません。

- 何度も続けて書き込まれたファイルは一つの `Write` になり、その間に作られて削除されたファイルは何のイベントにもなりません。変更は 50 ミリ秒静かになるまで待ちます（`Debounce`）。ただし変更が続いていても、500 ミリ秒（`MaxDelay`）より長くは待ちません。
- エディターが保存するときのように、一時ファイルをリネームして上書きする形で保存されたファイルは、削除と作成ではなく `Write` になります。新しいファイルも引き続き監視されます。
- 監視中のフォルダーの中で移動したファイルやフォルダーは、どれだけ離れた場所へ移動しても、すべてのプラットフォームで `Rename` になります。監視はファイルを名前ではなく ID（デバイスと inode、Windows ではファイル ID）で識別しているからです。フォルダーの中へ、または外へ移動したものは `Create` か `Remove` になります。
- システムが変更を取りこぼした（一度に多すぎた）場合、バッチの先頭が `Overflow` になります。このとき監視はすべてを見直しており、バッチの残りはその結果見つかったものです。
- 監視中のフォルダー（またはファイルを監視している場合はそのフォルダー）が削除またはリネームされると、最後のバッチに `.` の `Remove` が入り、監視は終わります。

`Recursive` を指定しない場合、フォルダーの監視は、そのフォルダーにあるファイルとフォルダーの変更を報告し、その中身までは報告しません。ファイルはそのフォルダーを通じて監視されます。ファイル自身のパスを監視すると、その置き換え、削除、再作成が `.` として報告されます。シンボリックリンクはリンクそのものとして報告され、リンク先をたどることはありません。`Ignore` は一致したものを除外し、除外したフォルダーは開きません。`watch.Patterns` は `.gitignore` の単純なパターン（`*.log`、`node_modules/`、`/dist`、`src/**/gen`）を受け取ります。

監視には、各プラットフォーム固有の仕組みを使います。macOS では FSEvents（ストリーム一つ分のコストでツリー全体を監視できます）、iOS では kqueue（ファイルとフォルダーごとにディスクリプターを使うため、iOS ではアプリ自身のフォルダーだけを監視してください）、Android と Linux では inotify（フォルダーごとに一つの watch を使い、上限は `fs.inotify.max_user_watches`）、Windows では `ReadDirectoryChangesW` です。Web では、アプリのファイルはページのものであり（[データの保存場所](#データの保存場所)を参照）、監視が検知するのは、アプリ自身のコードがそこに加えた変更です。監視がフォルダーの外へ出ることはありません。読めないものは除外され、ツリーが大きすぎる場合は `MaxEntries`（macOS ではファイルとフォルダー合わせて 65536、それ以外では 8192）に達した時点で `watch.ErrLimit` で止まります。[`examples/files`](../../examples/files) では、リアルタイムに更新されるフォルダーを表示しています。

## メインスレッド

UI はメインスレッドで動くため、そこで処理を待つ呼び出しをすると UI が固まります。ルールについては[メインスレッド](concepts.md#メインスレッド)で説明しています。データに関しては、次のようになります。

- **メインスレッドで呼ばない**：`sqlite.Open`、`Migrate`、`Exec`、`Tx`、`Query`、`QueryOne`（ただし起動時、`app.Run` の前は除く）。`secure.Get`、`Set`、`Delete`。ファイルとネットワークの呼び出し。
- **メインスレッドで呼ぶ**：`ExecAsync`、`TxAsync`、`Watch`、`WatchWith`。`secure.Load`、`Save`、`Remove`。`store.Signal`（ほかのシグナルと同じく）。
- **どちらでもよい**：`store.Get`、`Set`、`Delete`。これらはメモリを読み書きするだけです（ファイルへの書き込みはメインスレッドの外で行われます）。

ゴルーチンからは、`ui.Post(f)` で `f` をメインスレッドで実行できます。シグナルへの書き込みはその中で行ってください。

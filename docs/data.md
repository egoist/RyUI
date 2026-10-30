# Data

An app keeps its data in a few places: small values in `nui/store`, records
in SQLite with `nui/sqlite`, secrets in the platform's keychain with
`nui/secure`, and files of its own with Go's `os`. This page covers each,
the network through `net/http`, and which calls may block the main thread.

## Where data lives

`store.Dir()` is the app's data directory: on iOS, Application Support in
the app's container; on Android, the app's files directory. `nui/store`'s
file is there, and so are the databases `sqlite.Open` makes from a plain
name. Put your own files there too.

Under `nui dev`, your Go code runs on your Mac, so `store.Dir()` is a
directory on the Mac (in the user's config directory, under `nui/`), and
the store, the databases and your files are there. `nui/secure` is the
exception: it is the device's keychain, as in a release build.

## Small values: nui/store

`nui/store` keeps small values across launches: settings, a signed-in
user's name, the tab last open. Values are JSON, in one file
(`store.json` in `store.Dir()`), written off the main thread a moment
after they change, and when the app goes to the background. It's not a
database: for many or large records, use SQLite.

`store.Signal` is a signal whose value is kept. It starts with the value
kept, or the default the first time, and each value it's set to is kept:

```go
func Settings() ui.Node {
	dark := store.Signal("dark", false) // false the first time; then what it was last set to
	return ui.Toggle(ui.ToggleProps{Value: dark})
}
```

Make it on the main thread, as any signal. A package-level variable works
too (`examples/downloader` has `var wifiOnly = store.Signal("wifiOnly",
false)`). Each call makes a new signal: two made with the same key don't
follow each other, so make one and share it.

`Get`, `Set` and `Delete` work on values directly. `Set` takes anything
`encoding/json` encodes; `Get` decodes into a pointer, and returns false
if the key has no value, or the value doesn't decode into it:

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

`store.Flush()` waits until what's pending is written. nui calls it when
the app goes to the background; you need it only in tests.

## Records: nui/sqlite

`nui/sqlite` is an app's SQLite database. SQLite is
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), a translation
of SQLite's C to Go: it needs no cgo, so the same code runs on iOS,
Android and, under `nui dev`, the Mac. [`examples/notes`](../examples/notes)
uses it.

### Open and migrate

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

`Open` takes a name, a file in `store.Dir()` (the directory is made if it
isn't there), an absolute path, or `":memory:"`. It turns on write-ahead
logging and foreign keys, and sets a busy timeout of 5 seconds.

`Migrate` brings the schema up to date. Each argument is a version of the
database, in order: its SQL (statements separated by semicolons) runs
once, in a transaction, and the version is recorded in `PRAGMA
user_version`. Add a step to change the schema; never change a step that
has shipped, since phones that ran it won't run it again.

`db.Close()` closes it. Most apps open one database at launch and keep
it.

### Query

`sqlite.Query[T]` runs a query and scans its rows into a slice of `T`.
For a struct, each column goes to the field of the same name: the field's
`db` tag, or its name, case-insensitively, with the column's underscores
ignored (`created_at` fills `CreatedAt`). Columns without a field are
skipped; a field tagged `db:"-"` is never filled. For any other `T`, the
first column is scanned.

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

`QueryOne` is the first row; `ok` is false if there's none.

### Exec and transactions

`db.Exec` runs a statement (insert, update, delete, create...) and returns
its `sql.Result`. `db.Tx` runs a function in a transaction: committed if
it returns nil, rolled back if it returns an error (which `Tx` returns) or
panics. In it, `tx.Exec` writes and `sqlite.Query` and `sqlite.QueryOne`
read, given the `*sqlite.Tx` instead of the database:

```go
err := db.Tx(func(tx *sqlite.Tx) error {
	if _, err := tx.Exec(`update notes set done = 1 where id = ?`, 1); err != nil {
		return err // rolled back
	}
	left, _, err := sqlite.QueryOne[int](tx, `select count(*) from notes where done = 0`)
	if err != nil {
		return err
	}
	fmt.Println(left, "left")
	return nil // committed
})
```

### Writes off the main thread

`Exec`, `Tx`, `Query`, `QueryOne` and `Migrate` wait for the disk. On the
main thread, write with `ExecAsync` and `TxAsync`: they run off it, then
call `done` on the main thread. With a nil `done`, an error is logged.

```go
db.ExecAsync(`insert into notes (text, created_at) values (?, ?)`, []any{"Buy milk", time.Now().Unix()},
	func(r sql.Result, err error) {
		if err != nil {
			status.Set("Couldn't save: " + err.Error())
			return
		}
		id, _ := r.LastInsertId()
		status.Set(fmt.Sprintf("Saved note %d", id))
	})

db.TxAsync(func(tx *sqlite.Tx) error {
	if _, err := tx.Exec(`delete from notes where done = 1`); err != nil {
		return err
	}
	_, err := tx.Exec(`update notes set text = trim(text)`)
	return err
}, nil)
```

To read from the main thread, use a live query (below), or a goroutine
that queries, then `ui.Post`s the rows back.

### Live queries

`sqlite.Watch[T]` is a live query: a signal of its rows, fetched off the
main thread now, and again each time a write to a table it reads commits.
Make it in a component; it stops with the component's owner.

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

Its value is a `sqlite.Result[T]`: `Rows`, the latest rows; `Err`, the
error the query last got; and `Loading`, true until the first fetch is
done. Give `Rows` to a `ui.List` and the list follows the table, as in
`examples/notes`.

A live query reruns only for the tables it reads. It learns them as
SQLite prepares it, the tables behind a view included, and reruns when a
write to one of them commits, including rows written by triggers and by
foreign keys' actions. A write to another table doesn't run it. Writes
that come while it's fetching make one more fetch, not one each.

`sqlite.WatchWith[T]` is `Watch` with arguments that change. Its
arguments come from a function that reads signals (a search field's text,
a page size), and the query runs again when they change, as when the
database does. Until the new rows come, the old ones stay; rows fetched
for arguments that have since changed are dropped.

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

### Beyond this package

`db.SQL()` is the `*sql.DB` underneath, for what `nui/sqlite` doesn't
cover. Writes made through it are noted too, but the live queries hear of
them at the next `Exec` or `Tx`, or at `db.Changed()`.

`db.Changed()` runs every live query again. Call it after a change the
database can't see: another process's, or the file replaced (a backup
restored).

## Secrets: nui/secure

`nui/secure` keeps small secrets, tokens and passwords, encrypted by the
platform. It's for secrets, not data: keep everything else in the store
or a database.

- **iOS**: the Keychain, as generic passwords under the app's bundle ID,
  readable after the device's first unlock, and on this device only.
- **Android**: each value is encrypted (AES-GCM) with a key in the Android
  Keystore, which never leaves it, and kept in the app's private
  preferences.

On the main thread, `Load`, `Save` and `Remove` take a callback, called
on the main thread:

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
	secure.Save("token", token, nil) // nil: no callback
}
```

From a goroutine, `Get`, `Set` and `Delete` wait for the platform and
return. Never call them on the main thread:

```go
go func() {
	token, ok, err := secure.Get("token")
	if err != nil || !ok {
		return
	}
	// ... use token ...
}()
```

## Files and the network

Files are Go's `os`, and the network is Go's `net/http`: nui adds nothing
of its own. Keep the files in `store.Dir()`. Files nui's packages give you
(a photo taken, a video recorded, photos picked) are in the app's caches
or temporary directory, which the system may clear: move one into
`store.Dir()` to keep it.

Both wait, so do them in a goroutine, and `ui.Post` the result back to
the main thread:

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

To keep a long download going while the app is out of sight, see
[`nui/background`](device.md#background-work-nuibackground); to know
whether the connection is Wi-Fi or metered, see
[`nui/network`](device.md#network-state-nuinetwork).
[`examples/downloader`](../examples/downloader) does both.

## The main thread

The interface runs on the main thread, and a call that waits there
freezes it. [The main thread](concepts.md#the-main-thread) explains the
rule; for data, it comes to this:

- **Don't call on the main thread**: `sqlite.Open`, `Migrate`, `Exec`,
  `Tx`, `Query` and `QueryOne` (except at launch, before `app.Run`);
  `secure.Get`, `Set` and `Delete`; file and network calls.
- **Call on the main thread**: `ExecAsync`, `TxAsync`, `Watch`,
  `WatchWith`; `secure.Load`, `Save` and `Remove`; `store.Signal`, as any
  signal.
- **Either**: `store.Get`, `Set` and `Delete`, which only read and write
  memory (the file is written off the main thread).

From a goroutine, `ui.Post(f)` runs `f` on the main thread: set signals
there.

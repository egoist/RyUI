# Data

An app keeps its data in a few places: small values in `asuka/store`, records
in SQLite with `asuka/sqlite`, secrets in the platform's keychain with
`asuka/secure`, and files of its own with Go's `os`. This page covers each,
the network through `net/http`, watching files, and which calls may block
the main thread.

## Where data lives

`store.Dir()` is the app's data directory. `asuka/store`'s file is there,
and so are the databases `sqlite.Open` makes from a plain name. Put your
own files there too.

| | `store.Dir()` |
|---|---|
| iOS | Application Support in the app's container (in its App Group, with a [share sheet](device.md#the-apps-own-share-sheet-appsharesheet)) |
| Android | the app's files directory |
| macOS | `~/Library/Application Support/<name>` |
| Windows | `%LOCALAPPDATA%\<name>` |
| Linux | `$XDG_DATA_HOME/<id>` (`~/.local/share/<id>`; in a Flatpak, the sandbox's own) |
| The web | `/data/<name>`, in a file system in memory that the page keeps in the browser's storage (IndexedDB) |

`<name>` is `asuka.json`'s `name`, and `<id>` its `id`; either is the
program's name when the app is run without the CLI (`go run`). The
directory is made by what first writes there. On Windows and Linux, the
host keeps a few files of its own there too (the main window's size and
place, the app's chosen language). In a browser, the directory's files
are loaded before the app starts and written back a moment after they
change, and as the page is hidden: see [the web](web.md).

Under `asuka dev`, where your Go code runs on your computer (a desktop
app, and iOS and Android in the player), `store.Dir()` is a directory of
`asuka dev`'s own there (in the user's cache directory, under `asuka/`,
one for each app and platform), so what you try while developing never
mixes with an installed build's data. With the player, the store, the
databases and your files are on the computer, not on the device.
`asuka/secure` is the exception: it is the platform's store, as in a
release build (under the player, the device's keychain). On the web,
`asuka dev`'s page keeps its files in the browser, for the dev server's
address.

## Small values: asuka/store

`asuka/store` keeps small values across launches: settings, a signed-in
user's name, the tab last open. Values are JSON, in one file
(`store.json` in `store.Dir()`), written off the main thread a moment
after they change, and when the app goes to the background (on a Mac,
as it's hidden or minimized or quits; on Windows, as its window loses
focus; on Linux, as none of its windows shows, each hidden, minimized or
on another workspace, and as it quits, its session ends or it's sent
SIGTERM; in a browser, as the page is hidden). It's not a
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

`store.Flush()` waits until what's pending is written. Asuka calls it when
the app goes to the background; you need it only in tests.

## Records: asuka/sqlite

`asuka/sqlite` is an app's SQLite database. SQLite is
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), a translation
of SQLite's C to Go: it needs no cgo, so the same code runs on iOS,
Android and the desktops. In a browser it's SQLite's own WebAssembly
build, which the web's builds ship with the app ([The web](web.md#sqlite)).
[`examples/notes`](../examples/notes) uses it.

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
has shipped, since devices that ran it won't run it again.

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

`db.SQL()` is the `*sql.DB` underneath, for what `asuka/sqlite` doesn't
cover. Writes made through it are noted too, but the live queries hear of
them at the next `Exec` or `Tx`, or at `db.Changed()`.

`db.Changed()` runs every live query again. Call it after a change the
database can't see: another process's, or the file replaced (a backup
restored).

## Secrets: asuka/secure

`asuka/secure` keeps small secrets, tokens and passwords, encrypted by the
platform. It's for secrets, not data: keep everything else in the store
or a database.

- **iOS**: the Keychain, as generic passwords under the app's bundle ID,
  readable after the device's first unlock, and on this device only.
- **Android**: each value is encrypted (AES-GCM) with a key in the Android
  Keystore, which never leaves it, and kept in the app's private
  preferences.
- **macOS**: the login keychain, as generic passwords under the app's
  bundle ID (its name, without one), which Keychain Access shows. An app
  signed ad hoc can use it, as `asuka dev`'s and a build without a
  Developer ID are.
- **Windows**: the Credential Manager, as the user's generic credentials
  named `<name>/<key>`, kept on this computer (they don't roam), which
  Control Panel's Credential Manager shows. A value is at most 2560 bytes.
- **Linux**: the user's keyring, through libsecret and the Secret
  Service (GNOME Keyring, KWallet, KeePassXC): an item of the app's own
  schema (its ID) for each key, labeled `<name>: <key>`, in the default
  collection, which the keyring's app (GNOME's Passwords and Keys) shows. Without
  libsecret or a keyring running, the calls fail, saying so.
- **The web**: a browser has no keychain. Each value is encrypted
  (AES-GCM, 256 bits) under a key WebCrypto made non-extractable, and
  kept with it in the app's IndexedDB database, so no script can read the
  key's bytes, and the values aren't on the disk in the clear. Anything
  running in the app's origin can still have the key decrypt them: it's
  a web app's best, not a keychain. It needs https or localhost.

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

Files are Go's `os`, and the network is Go's `net/http`: Asuka adds nothing
of its own. Keep the files in `store.Dir()`. Files Asuka's packages give you
(a photo taken, a video recorded, photos picked, files dropped or pasted)
are in the app's caches or temporary directory, which the system may
clear: move one into `store.Dir()` to keep it. In a browser, `os` works on
the page's file system: `store.Dir()` is kept, and the rest (`/tmp`, the
caches) lasts as long as the page. `net/http` there is the browser's
`fetch`, so a request to another origin needs that origin's CORS
headers.

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
[`asuka/background`](device.md#background-work-asukabackground); to know
whether the connection is Wi-Fi or metered, see
[`asuka/network`](device.md#network-state-asukanetwork).
[`examples/downloader`](../examples/downloader) does both.

## Watching files: asuka/watch

`asuka/watch` tells the app when files change: a folder it shows, a
project open in an editor, a settings file another program writes, a
document another app saved. It works on every platform, phones and the
web too.

In a component, `watch.Path` watches a path while the component is
there, and calls a function on the main thread with each batch of
changes. Read the files again there:

```go
func Folder(dir signal.Accessor[string]) ui.Node {
	names := signal.NewFunc[[]string](nil, slices.Equal)
	reload := func() {
		d := signal.Untrack(dir.Get)
		go func() {
			list := readNames(d) // os.ReadDir, off the main thread
			ui.Post(func() { names.Set(list) })
		}()
	}
	signal.Effect(func() { dir.Get(); reload() }) // and as dir changes
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

The watch follows its accessor (a new path stops the old watch and
starts one of the new), and stops as the component goes. It starts on
a goroutine, since listing a large folder takes a while: `w.Watching()`
says once it has, and `w.Err()` what kept it from starting (the path
doesn't exist, can't be read, holds too many files: `watch.ErrLimit`).
While the function runs, the watch waits, so a busy app gets the
changes of the meantime in one batch rather than many.

Off the main thread, as in a command-line tool or a sync loop,
`watch.New` watches from any goroutine and `Next` returns each batch:

```go
w, err := watch.New(ctx, dir, watch.Options{Recursive: true, Ignore: watch.Patterns("node_modules/", ".git/")})
if err != nil {
	return err
}
defer w.Close()
for {
	events, err := w.Next(ctx)
	if err != nil {
		return err // io.EOF once closed, or once dir went away
	}
	for _, e := range events {
		fmt.Println(e) // "create src/a.go", "rename a.txt -> b.txt", ...
	}
}
```

An event's `Op` is `Create`, `Write`, `Remove`, `Rename` (with
`OldPath`), `Attrib` (permissions, with `Options.Attributes`) or
`Overflow`; its `Path` is relative to what's watched, separated by
slashes, `.` for itself. A watch compares what it sees with what it saw,
so events say what's different, not every step that made it so:

- A file written many times in a row is one `Write`, and one created
  and removed in between is nothing. Changes wait for 50 ms of quiet
  (`Debounce`), and never more than 500 ms while more keep coming
  (`MaxDelay`).
- A file saved as editors save, a temporary file renamed over it, is a
  `Write`, not a removal and a creation, and the new file is watched in
  its turn.
- A file or folder moved within the watched folder is a `Rename`,
  however far it moved, on every platform: the watch knows files by
  their identity (device and inode, or Windows's file ID), not their
  name. What moved in or out is a `Create` or a `Remove`.
- `Overflow` starts a batch when the system dropped changes (too many at
  once): the watch looked at everything again, and the rest of the
  batch is what it found.
- When the watched folder (or a watched file's folder) is removed or
  renamed, the last batch has a `Remove` of `.`, and the watch ends.

Without `Recursive`, a folder's watch reports its files and folders,
not what's in them. A file is watched through its folder: watch the
file's own path, and its replacement, removal and return are reported
as `.`. Symlinks are reported as themselves, never followed. `Ignore`
leaves out what it matches, and never opens a folder it ignores:
`watch.Patterns` takes `.gitignore`'s simple patterns (`*.log`,
`node_modules/`, `/dist`, `src/**/gen`).

Each platform's own way: FSEvents on macOS (a whole tree for the cost of
one stream), kqueue on iOS (a descriptor per file and folder, so keep
iOS watches to the app's own folders), inotify on Android and Linux (a
watch per folder, up to `fs.inotify.max_user_watches`), and
`ReadDirectoryChangesW` on Windows. On the web, the app's files are the
page's (see [Where data lives](#where-data-lives)), and the watch sees
what the app's own code changes in them. The watch never leaves the
folder: what it can't read is left out, and `MaxEntries` (65536 files
and folders on macOS, 8192 elsewhere) stops a watch of a tree too
large with `watch.ErrLimit`. [`examples/files`](../examples/files)
shows a folder that updates live.

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

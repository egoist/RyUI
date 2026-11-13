# The desktop

What only a desktop has, beyond windows and the menu bar
([Navigation](navigation.md#windows)): tray icons and the Dock menu,
apps that live in the background, global shortcuts, file dialogs and
the file manager, files the app opens, one instance, quitting, recent
documents, document-based apps, starting at login, the displays, power,
progress on the app's icon and the system's appearance.

Each one is a package, or a function in `ui` or `app`, and works on the
Mac, Windows and Linux. A phone or a browser that hasn't one answers
that it hasn't (`ErrUnsupported`, `ErrNotAvailable`, or `Supported()`
false), so an app can offer it only where it works.

## Tray icons and the Dock menu

`tray.Show`, called inside a component, puts an icon in the tray while
that component is mounted: an `NSStatusItem` in the Mac's menu bar, an
icon in the notification area on Windows, a StatusNotifierItem on Linux
(KDE, Xfce, Cinnamon, MATE, Budgie, and GNOME with the AppIndicator
extension).

```go
tray.Show(tray.Props{
	Symbol:  ui.Val("moon.zzz"),
	Title:   remaining, // a signal: "25:00" beside the icon
	Tooltip: ui.Val("Focus timer"),
	Menu: signal.Static([]ui.MenuItem{
		{Title: "Start", OnPress: start},
		{Title: "Length", Submenu: []ui.MenuItem{
			{Title: "25 minutes", OnPress: func() { length.Set(25) }},
			{Title: "50 minutes", OnPress: func() { length.Set(50) }},
		}},
		ui.MenuSeparator,
		ui.MenuQuit,
	}),
})
```

Its `Image` or `Symbol` (a template image on the Mac, which follows the
menu bar's appearance), `Title` (the Mac, and Linux panels that show
labels), `Tooltip` and `Menu` follow their signals, and the icon goes
when its owner is disposed. Without `OnClick`, a click opens the menu;
with it, a click runs `OnClick` and a right click opens the menu.

Menus take `Submenu`s and `ui.MenuQuit`, as `ui.MenuBar`'s do.
`tray.DockMenu(items)` adds items to the Dock icon's menu on the Mac;
on Windows they're the jump list's Tasks, which reach the running app;
on Linux, the launcher's quicklist.

`examples/tray` is a timer that lives in the menu bar.

## Apps in the background

`app.KeepRunning()` keeps the app running when its last window closes:
the window hides, and `ui.MainWindow().Show()` brings it back (on the
Mac, so does a click on the Dock icon). `app.RunInBackground()`, or
`asuka.json`'s `"background": true`, also starts the app with no
window and, on the Mac, no Dock icon: an app of the menu bar or the
tray. Call them before `app.Run`.

## Global shortcuts

`hotkey.Register` registers keys that reach the app while another app
is in front, for as long as the component that registers them lives:

```go
k := hotkey.Register(hotkey.Props{
	Shortcut:    ui.Val(ui.Cmd("space").With(ui.Alt)),
	Description: ui.Val("Show the launcher"),
	OnPress:     func() { ui.MainWindow().Focus() },
})
```

`OnPress` runs on the main thread at each press, and the shortcut
follows its accessor (keys the user picks in settings). `k.Status()`
and `k.Err()` say whether it's active, pending, or why it failed:
`ErrTaken` (another app has the keys), `ErrNotAllowed` (the user
declined, or the desktop doesn't know the app), `ErrNotAvailable`,
`ErrKey`.

- **macOS**: Carbon's hot keys, with no Accessibility permission. The
  system's own shortcuts count as taken. A press doesn't bring the app
  forward: `Focus` a window to.
- **Windows**: `RegisterHotKey`; the press lets the app take the
  foreground.
- **Linux**: under X11 the app grabs the keys itself. Under Wayland the
  desktop binds them through the GlobalShortcuts portal (GNOME 48, KDE
  Plasma 6): it may ask the user to confirm or pick other keys, and it
  needs the app installed with its desktop entry. A window `OnPress`
  shows takes the focus with the press's activation token.
- Phones and the web have none (`ErrNotAvailable`).

## File dialogs

`asuka/dialog` shows the system's file dialogs and gives the user's own
paths, not copies:

```go
dialog.Save(dialog.SaveOptions{
	Name:    "Notes.md",
	Filters: []dialog.Filter{{Name: "Markdown", Extensions: []string{"md"}}},
	From:    view, // the window it belongs to
}, func(path string, err error) {
	if path != "" {
		os.WriteFile(path, export(), 0o644)
	}
})
```

`dialog.Open` (several files with `Multiple`, `Filters` by MIME type
such as `image/*` or by extension), `dialog.Save` (a suggested `Name`;
the dialog asks before replacing a file) and `dialog.Folder`. `Title`,
`Button`, `Start` (a folder, or a file in it) and `From` set them up. A
cancel is no path and no error.

On the Mac they're sheets on the window; on Windows the common item
dialogs, modal to the window; on Linux GTK's file dialog, which is the
desktop's portal in a Flatpak. In a browser, Open and Folder copy what's
picked into the app's files, and the file Save gives is written to the
file the user chose (Chrome, Edge) or downloaded. On iOS and Android,
Open is the document picker (copies, as `device.PickFiles` gives), and
Save and Folder answer `dialog.ErrUnsupported`.

## The file manager

`asuka/shell` does what the file manager does with a file:
`shell.Reveal` shows it selected in its folder (Finder, Explorer, or the
desktop's file manager), `shell.Open` opens it in the user's app for its
type (`shell.ErrNoApp` if there's none) and a folder in the file
manager, `shell.Trash` moves it to the trash or the Recycle Bin and
never deletes it for good (it fails instead), and `shell.Beep` sounds
the alert. Phones and the web have no file manager: Reveal and Trash
answer `shell.ErrUnsupported`, and on the web Open shows the file in a
new tab. To show a file's contents in the app, use `asuka/preview`.

## Opening files

Declare the kinds of files the app opens in `asuka.json`'s `fileTypes`
([Shipping](shipping.md)), and handle them with `ui.OnOpenFiles`:

```go
ui.OnOpenFiles(func(paths []string) {
	for _, p := range paths {
		open(p)
	}
})
```

Files opened together arrive in one call, as absolute paths: a double
click in Finder, Explorer or Files, Open With, a file dropped on the
Dock icon, a recent document chosen, and on Windows and Linux the files
on the command line. Those the app launched with wait for the first
handler. On iOS the path is the document where it is, readable until
the app quits; on Android and the web it's the app's own copy in its
caches.

## One instance

On Windows and Linux an app with an `id` runs once: starting it again
brings the running one to the front, `app.OnSecondInstance(func(args
[]string, dir string))` gets the new launch's arguments and working
directory, and the files and links among them go to `ui.OnOpenFiles`
and `ui.OnOpenURL`. Set `"singleInstance": false` in `asuka.json` to run
a new instance each time. The Mac always runs an app once and passes
files, not arguments.

## Quitting

`app.Quit()` quits as the user's Quit does. `app.OnQuit(func(e
*app.QuitEvent))` runs first, however the quit started (⌘Q, the Dock,
closing the last window, Ctrl+C or SIGTERM, logging out), and
`e.Cancel()` keeps the app running, to ask about unsaved changes. When
`e.SessionEnding` is set the quit can't be canceled: save at once,
without a dialog. A window's own close is asked first, through its
`OnCloseRequest` ([Navigation](navigation.md#windows)).

`app.DataDir`, `ConfigDir`, `CacheDir` and `LogDir` are the app's own
folders, made as they're asked for.

## Recent documents

Call `recent.Add(path, nil)` for each document the user opens or saves.
The Mac lists them in the File menu's Open Recent and the Dock menu,
Windows in the jump list, Linux in the user's recent files. Choosing one
opens it through `ui.OnOpenFiles`. `recent.List` returns them newest
first, skipping files that are gone, and `recent.Clear` removes the
app's own.

## Documents

`asuka/document` makes an app whose windows each show a file, as a text
editor or a drawing app does: what NSDocument gives a Mac app, on every
platform. Describe the kind of document, and run it:

```go
func main() {
	document.New(document.Options[string]{
		Types: []document.Type{{Name: "Plain Text", Extensions: []string{"txt"}}},
		Read: func(r io.Reader) (string, error) {
			b, err := io.ReadAll(r)
			return string(b), err
		},
		Write: func(w io.Writer, text string) error {
			_, err := io.WriteString(w, text)
			return err
		},
		Equal: func(a, b string) bool { return a == b },
		View: func(doc *document.Document[string]) ui.Node {
			return ui.TextField(ui.TextFieldProps{Value: doc.Content(), Multiline: true, Plain: true})
		},
	}).Run()
}
```

List the same types in `asuka.json`'s `fileTypes`, so the system offers
the app for them. `examples/textedit` is this editor with a status line
and a bar for a file another app changed.

A document's `Content()` is a signal: the view binds it, and changing
it edits the document, unless `Equal` finds it equal to the file's
(undoing back to it, typing a letter and deleting it). Reading the file
again sets it too. A content the app changes in place (a pointer to a
model) says so with `doc.Changed()`, and gives `Snapshot`, which copies
it before it's written. `doc.State()` is a signal of the document's
name, file and whether it's edited, read-only, in conflict or deleted.

What the user gets on the desktop:

- **A window per document**, named after its file ("Untitled",
  "Untitled 2" before it has one), opening where the user left that
  file's window. A Mac shows a dot in the close button while it has
  unsaved changes and the file's icon in the title bar; Windows and
  Linux a `*` before the title, which ends with the app's name, and
  GNOME the file's folder under it.
- **The File menu**: New, Open…, Open Recent, Close, Save, Save As…,
  and on a Mac Duplicate, Rename…, Move To… and Revert to Saved
  (Revert elsewhere), with their shortcuts, acting on the window in
  use. `Options.Menus` adds the app's own menus beside it; one titled
  File adds its items to it. Windows and Linux show the menus in each
  document's window.
- **Autosave**: a document with a file is saved a moment after the
  user stops typing, when its window loses the focus and when it
  closes, as Mac apps save; it never asks. `AutosaveOff` saves when the
  user says, and asks when the window closes, as Windows apps
  traditionally do.
- **Questions only where saving can't happen by itself**: closing an
  untitled document ("Do you want to keep this new document?"), one
  whose file is read-only or another app changed ("Save Anyway",
  "Revert"), or any edited one with `AutosaveOff`.
- **Recovery copies**: unsaved changes are copied into the app's data a
  second after the user stops typing. A crash, a forced quit or a quit
  loses nothing: the next launch opens those documents again, edited.
  Quitting keeps the documents that way, as VS Code does; with
  `AskOnQuit` it asks instead (on a Mac, "You have 3 documents with
  unsaved changes. Do you want to review these changes before
  quitting?"; on Windows and Linux one question naming them). When the
  session ends there's no time to ask: everything is saved or kept at
  once.
- **Files opened from outside**: Finder, Explorer, the Dock, the
  command line, Open Recent, and Open… go to the document's window if
  it's open already.
- **Files other apps change**: an unedited document reads its file
  again; an edited one asks at its next save. A file renamed is
  followed (on a Mac, wherever the user moves it on its disk), and one
  deleted is written again by the next save.

A Mac's document app stays open with no window, and clicking its Dock
icon makes a new document; on Windows and Linux the app quits with its
last document's window. `NoUntitled` opens nothing at launch instead of
an untitled document.

On a phone and in a browser there's one document at a time: the app
opens on a list of its documents (on iOS, its folder in Files), with
New and Open…, and a document's screen is pushed over it. Each document
has a file from the moment it's made and is saved as the user edits,
when the app goes to the background and when the user goes back, as
UIDocument's apps save: nothing ever asks. Files from elsewhere are
copied into the app's documents first.

The questions and the menu speak the app's language (English, German,
French, Spanish, Japanese and Chinese so far): give others, or change
them, with keys `document.<key>` in the app's catalogs.

Left out, of what only NSDocument does on a Mac: versions (Revert To a
version), locking, renaming in the title bar's popover, and documents
that are folders.

## Starting at login

Offer `login.Set(true, done)` as a setting, and read it back with
`login.Enabled`. It's a login item on macOS 13 and later (a packaged
app only; `login.ErrNeedsApproval` until the user allows it in System
Settings), a Run entry on Windows, and an autostart entry on Linux (the
Background portal in a Flatpak). `login.OpenedAtLogin` tells a launch at
login apart, so the app can start out of sight.

## Displays, power and the app's icon

`screen.UseDisplays()` follows the displays: where each one is, its
work area, scale and refresh rate, and which is primary; `Nearest` and
`Matching` pick the one under a point or a window. Wayland has no work
area: it's the display's bounds there.

`power.Use()` follows the battery, Low Power Mode and the thermal state.
`power.OnSuspend`, `OnResume`, `OnLock` and `OnUnlock` say when the
computer sleeps and wakes and when the screen locks, and
`power.IdleTime()` how long the user has been away. Phones have lock
and unlock, but no suspend or idle time; the web, the battery alone.

`dock.SetProgress` shows a task's progress on the Dock icon, the
taskbar button, or a Linux dock that has the Unity launcher API (GNOME's
own dash shows none). `dock.RequestAttention` bounces or flashes the
icon while the app is in the background. The count on the icon is
`notify.SetBadge`.

`appearance.Use()` gives the system's accent color, Increase Contrast
and Reduce Transparency, for what the app draws itself; the views Asuka
shows follow them already.

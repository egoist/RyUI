# The desktop

What only a desktop has, beyond windows and the menu bar
([Navigation](navigation.md#windows)): tray icons and the Dock menu,
flyouts, apps that live in the background, global shortcuts, panels and
launchers, finding the apps installed, click-through windows, file
dialogs and the file manager, files the app opens, one instance,
quitting, recent documents, document-based apps, starting at login,
updates, Now Playing and the media keys, the displays, power, progress
on the app's icon and the system's appearance. To follow changes to
files and folders (a folder the app shows, a document another app
saves), see [Watching files](data.md#watching-files-asukawatch), which
phones and the web have too.

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

## Flyouts

A flyout is a window that hangs from a tray icon, as the panels of the
Mac's menu bar extras and of Windows' notification area do, or from a
rectangle of a window. It's a `ui.Window` with a `Flyout`:

```go
open := signal.New(false)
icon := tray.Show(tray.Props{
	Symbol:  ui.Val("bolt.fill"),
	OnClick: func() { open.Set(!open.Peek()) },
	Menu:    ui.Val([]ui.MenuItem{ui.MenuQuit}), // a right click
})
ui.Window(ui.WindowProps{
	Open:       open,
	Flyout:     &ui.Flyout{Anchor: icon.Anchor(), Placement: ui.PlacementBottom},
	FitContent: true, MinWidth: 260, MaxHeight: 480,
	Content:    panel,
})
```

- It has no frame and no taskbar button. On a Mac, with no `Background`,
  it shows the material of menus behind its content, with rounded
  corners and a shadow, so give its content no background there;
  Windows 11 rounds it and casts a shadow over the window color; Linux
  draws neither.
- `Placement` is the side of the anchor it goes to and how it lines up
  with it (`PlacementBottom`: below, centered). Where the display has
  no room there it flips to the other side (below a Mac's menu bar
  icon, above an icon of a taskbar at the bottom of the screen), then
  slides into the display, then shrinks to fit it. `Gap` keeps it off
  the anchor.
- `Anchor` follows its accessor: `ui.Anchor{X, Y, Width, Height}` is a
  rectangle of the window the flyout is declared in, in points from its
  content's top left (a view's frame, from `OnLayout`); there it's that
  window's child, and moves with it.
- It takes the keyboard as it shows, without bringing the app to the
  front, and closes as the user closing it does (`Open` false,
  `OnClose`, or `OnCloseRequest` asked) when they click elsewhere, go
  to another app, or press an Escape its content leaves.
- A dialog over it: a `Modal` `ui.Window` declared in its content shows
  over the flyout, above it and centered on it, and keeps the flyout
  open until it closes, when the flyout gets the keyboard back. An
  `Alert` over it keeps it open too.
- `FitContent` sizes it to its content (below), placed again against
  its anchor as that grows or shrinks.

| | Tray flyout | Placed against |
|---|---|---|
| macOS | a borderless, nonactivating `NSPanel` at the level of menus, over the material of menus | the copy of the status item the user clicked (each display's menu bar has one) |
| Windows | a borderless, topmost tool window with Windows 11's rounded corners | the icon's rectangle (`Shell_NotifyIconGetRect`), the taskbar's edge considered |
| Linux | an undecorated window, out of the taskbar | under X11, where the panel says the icon was clicked; under Wayland, which lets no app place a window, where the desktop puts it |
| The web | a panel in the page, the page's top right for a tray anchor | a rectangle of its window, in the page |

`examples/tray`'s icon opens one with the timer in it, and a dialog over
it.

## Windows sized to their content

`FitContent` (`WindowProps`', `WindowOptions`') sizes a window to what
it shows, and follows it as it changes: a flyout with a list of nodes
that grows, a launcher whose results come and go. Its width is its
content's widest line (max-content) within `MinWidth` and `MaxWidth`,
at which its text wraps; its height is its content's at that width
within `MinHeight` and `MaxHeight`, beyond which a `Scroll` in it
scrolls. The core lays the content out at that size as it lays out each
frame, and the window takes it in the same pass, so it shows at its size,
never at another first. A flyout is placed again against its anchor; a
window or a panel keeps its top edge and grows down. The user can't
resize it. On the web the main window is the tab, which fills the page.

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

## Panels and launchers

A panel is the window of a launcher, a command palette or a quick
entry, as Spotlight's and Raycast's are: summoned from anywhere by a
global shortcut, it floats above every app's windows, on whichever
Space or virtual desktop the user is on and over apps in full screen,
stays out of the taskbar, the Dock's windows and the app switcher, and
takes the keyboard without bringing the app to the front on a Mac, so
the app the user was in keeps its menu bar and gets the keyboard back
as the panel goes. It's a window with `Panel`: a launcher's main
window, in an app that runs in the background:

```go
func main() {
	app.RunInBackground()
	app.RunWindow(ui.WindowOptions{Panel: true, Hidden: true, FitContent: true,
		MinWidth: 640, MaxWidth: 640, MaxHeight: 440}, Launcher)
}

func Launcher() ui.Node {
	w := ui.UseWindow()
	w.OnCloseRequest(func() bool { w.Hide(); return false }) // Escape, a click elsewhere
	hotkey.Register(hotkey.Props{
		Shortcut: ui.Val(ui.Shortcut{Key: "space", Mods: ui.Alt}),
		OnPress:  func() { w.Show() },
	})
	// ...
}
```

- It has no frame. On a Mac, with no `Background`, it shows the
  material of popovers behind its content, rounded and with a shadow;
  Windows 11 rounds it; Linux draws it as a window's content.
- Without `X` and `Y` (or a `StateKey`), it shows on the display with
  the pointer, centered across it, a third of the way down, each time
  it shows; `SetPosition` and `Center` place it for good. With
  `FitContent` it grows down from its top as results come.
- It closes as the user closing it does on an Escape its content leaves
  (a text field's too) and as it loses the keyboard: a click elsewhere,
  another app. Hide it instead (`OnCloseRequest`, above), so it shows
  again at once, as it was; the main window of an app that keeps
  running hides as it closes anyway.
- `Show` shows it with the keyboard; `Focus` does the same, never
  bringing the app forward.

| | A panel |
|---|---|
| macOS | an `NSPanel` with the nonactivating style, on every Space and over full-screen apps (`canJoinAllSpaces`, `fullScreenAuxiliary`), out of the window cycle, floating; never the main window |
| Windows | a borderless topmost tool window owned by a hidden window (no taskbar button), Windows 11's rounded corners; it takes the foreground, which a global shortcut's key press lets it |
| Linux | an undecorated window; under X11 a utility window kept above, on every workspace, out of the taskbar and pager; under Wayland an overlay layer-shell surface where the app runs with gtk4-layer-shell (`LD_PRELOAD`) on a compositor that has the protocol (KDE Plasma, Sway, Hyprland, COSMIC), else a window as others (GNOME places it, and keeps nothing above) |
| The web | a panel over the page, a third of the way down |

`examples/launcher` is a Spotlight of its own: ⌥Space (Alt+Space) shows
a panel with a search field, the apps installed as you type, with their
icons, and Enter opens the first.

## Finding apps

`asuka/apps` lists the apps installed, as a launcher searches them:

```go
apps.List(func(list []apps.App, err error) { installed.Set(list) })
results := apps.Search(installed.Get(), query.Get())
apps.Icon(results[0], 32, func(path string, err error) { icon.Set(path) }) // a PNG for ui.Image
apps.Open(results[0], nil)
```

- `List` gives each app's `Name` (as the system shows it, in the user's
  language), `ID` and `Path`, sorted by name; it reads the disk off the
  main thread and answers on it. List them again as the panel shows: an
  app may have come or gone.
- `Search` ranks them for what the user typed, ignoring case and
  accents: the name itself, then its start, then a word's start or the
  initials ("vsc" for Visual Studio Code), then anywhere, then its
  letters in order.
- `Icon` draws an app's icon at a size in points as a PNG file in the
  app's caches, kept for the next time, and gives its path.
- `Open` starts the app, or brings it to the front. Hide the panel
  first, so the app the user was in has the keyboard until the opened
  one takes it.

| | Where | ID | Icon | Open |
|---|---|---|---|---|
| macOS | `/Applications`, `/System/Applications`, `~/Applications` and a folder below each, Finder | the bundle identifier | the workspace's icon for it | `NSWorkspace` |
| Windows | the Start menu's apps (the shell's Applications folder), packaged ones too | the AppUserModelID | the shell's image for it | the shell |
| Linux | the desktop entries a menu shows (XDG, Flatpak's and Snap's too), by `OnlyShowIn`, `NoDisplay`, `TryExec` | the desktop file id | the entry's icon from the theme | GIO, with the display's launch context |

Phones and the web answer `apps.ErrUnsupported`.

## Click-through windows

`ClickThrough` (or `ui.UseWindow().SetClickThrough(true)`) lets the
mouse go through a window to what's behind it, another app's window or
the desktop: an overlay, a HUD, a desktop pet, made `Transparent` and
`AlwaysOnTop`. `ClickThrough()` follows it as a signal. The window
takes no clicks, hover or wheel at all while it's on; switch it off to
let the user use it.

```go
ui.Window(ui.WindowProps{Open: hud, Transparent: true, AlwaysOnTop: true, ClickThrough: true,
	Width: 240, Height: 80, Content: meters})
```

It's `ignoresMouseEvents` on a Mac, a layered `WS_EX_TRANSPARENT`
window on Windows and an empty input region on Linux
(`gdk_surface_set_input_region`). On the web a window in the page lets
clicks through to the page; nothing lets them out of the browser.

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

`app.Relaunch()` quits the same way (the handlers can cancel it) and
starts the app again once it has exited: after an update, or a setting
that takes effect at launch.

## Updates

`update.Window(update.Options{})`, in the app's root, is Sparkle's
updater: it checks for a new version soon after launch and once a day,
shows its release notes with Install Update, Skip This Version and
Remind Me Later, downloads it with a progress bar (only what changed,
when there's a delta from the running version), checks that it's signed
with the app's key, installs it and offers Relaunch Now.
`update.AppMenu()` puts "Check for Updates…" after About on the Mac (in
Help elsewhere). `update.Check`, `Release.Install` and `update.Relaunch`
make an interface of the app's own.

`asuka update keygen` makes the key pair, `asuka.json`'s `updates` says
where releases go (GitHub's releases, an S3 bucket or any HTTPS
directory, or a server of yours), and `asuka publish --platform mac`
(`windows`, `linux`) builds, signs and uploads them
([Shipping](shipping.md#updates)). A Mac app's new bundle waits for it to
quit; Windows and Linux replace the app's files while it runs. An app
that the system's packages, Flatpak, Snap or an app store installed is
updated by them: `update.Supported()` says so, and phones answer
`ErrUnsupported`. On the web, a page older than the site offers to
reload.

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

## Now Playing and the media keys

`nowplaying.Show` puts what the app plays in the system's media
controls, and their buttons and the keyboard's media keys (play/pause,
next, previous) reach its handlers: Control Center's and the menu bar's
Now Playing on the Mac, the media controls of the volume flyout and
quick settings on Windows (the app window's
SystemMediaTransportControls), an MPRIS player on Linux, which GNOME's
and KDE's controls and `playerctl` drive. It's the same package as on
phones, where it's the lock screen's controls
([Device and services](device.md#now-playing-and-the-media-keys-asukanowplaying)):

```go
music := audio.Load("music/spring.mp3")
signal.OnCleanup(music.Release)
nowplaying.Show(nowplaying.Props{
	Title:  ui.Val("Spring"),
	Artist: ui.Val("Vivaldi"),
	Player: music, // its state and position; play, pause, seek on it
	OnNext: playNext,
})
```

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

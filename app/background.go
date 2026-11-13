package app

import "github.com/egoist/asuka/internal/boot"

// KeepRunning keeps a desktop app (macOS, Windows, Linux) running when
// its last window closes, as an app that lives in the tray (asuka/tray)
// does: closing the main window hides it, its root staying mounted,
// until the app shows it again. The app quits from its menus (a
// ui.MenuQuit item, as in its tray icon's menu; the Dock's Quit and ⌘Q
// on a Mac) or as the session ends. Call it in main, before Run. Phones
// and the web ignore it.
func KeepRunning() { boot.KeepRunning = true }

// RunInBackground makes a desktop app one that runs in the background, as a
// menu bar app, a launcher or a sync client does: no window shows at
// launch (the main window, holding the root, stays hidden until the app
// shows it), it keeps running with no window (KeepRunning), and its tray
// icon (asuka/tray) is the user's way in; a ui.Window it opens shows as
// any. On macOS it has no Dock icon and isn't in the app switcher (an
// accessory app), and its windows come to the front over the app in
// use. On Windows and Linux it has no taskbar button while no window
// shows (one that shows has its own). Call it in main, before Run;
// asuka.json's "background": true does it for a built app, and on macOS
// from launch (LSUIElement), so that its Dock icon never shows. Phones
// and the web ignore it.
func RunInBackground() { boot.Background = "1" }

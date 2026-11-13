//go:build windows

package windows

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/egoist/asuka/internal/appdir"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/filetype"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// The app's file types, its quitting, its recent documents (asuka/recent)
// and its start at login (asuka/login) on Windows; files.go has what's
// tested anywhere.

func init() {
	// A login entry's argument, taken out before main reads os.Args.
	boot.TakeLoginArg()
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["recent"] = h.recentModule()
		m["login"] = h.loginModule()
	})
}

// registerFileTypes registers asuka.json's fileTypes (boot.FileTypes) for
// the user, as the app starts: only the values that differ, then the
// shell is told. Not when automation or asuka dev runs the app.
func (h *Host) registerFileTypes() {
	types := filetype.Decode(boot.FileTypes)
	if len(types) == 0 || notItself(os.Getenv) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	changed := false
	for _, v := range fileTypeValues(boot.ID, exe, types) {
		have, ok := RegUserString(v.key, v.name)
		if ok && (have == v.value || v.ifUnset) {
			continue
		}
		if err := RegSetUserString(v.key, v.name, v.value); err != nil {
			fmt.Fprintf(os.Stderr, "asuka: registering the app's file types: %v\n", err)
			break
		}
		changed = true
	}
	if changed {
		AssociationsChanged()
	}
}

// Quitting. The user's quit (closing the main window, which quits the
// app), app.Quit, Ctrl+C in its console and the session ending all ask
// app.OnQuit's handlers first (boot.QuitRequested), once: a quit they
// allowed only finishes.

// quitAllowed: the handlers let the app quit; it's on its way.
var quitAllowed bool

// askQuit asks the handlers whether the app may quit (ending: the session
// ends, which they can't cancel), and reports it.
func (h *Host) askQuit(ending bool) bool {
	if quitAllowed {
		return true
	}
	canceled := false
	if boot.QuitRequested != nil {
		h.enter(func() { canceled = boot.QuitRequested(ending) })
	}
	if canceled && !ending {
		return false
	}
	quitAllowed = true
	return true
}

// startQuit lets app.Quit quit the app and, outside asuka dev (whose
// Ctrl+C keeps the run's state: state.go), Ctrl+C in the console quit it
// as the user's quit does; a second one ends it at once.
func (h *Host) startQuit() {
	boot.Quit = func() { h.Post(h.quit) }
	if os.Getenv("ASUKA_STATE") != "" {
		return
	}
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		h.Post(h.quit)
		<-sig
		os.Exit(130)
	}()
}

// quit quits the app as closing its main window does, if the handlers
// let it.
func (h *Host) quit() {
	if h.window == 0 || !h.askQuit(false) {
		return
	}
	SendMessage(h.window, WM_CLOSE, 0, 0)
}

const (
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
)

// quitMessage handles the main window's messages that quit the app:
// WM_CLOSE (the user closing it quits the app, unless a handler cancels:
// then the window stays) and the session ending (WM_QUERYENDSESSION,
// which the app doesn't refuse; WM_ENDSESSION, after which the process
// may end at any moment: what's pending is written then). It reports
// whether it handled the message; one it lets through goes on as before.
//
// NOTE: an app kept running after its last window closes (a tray icon's)
// changes what closing the main window means; this is the one place that
// asks the quit handlers for it.
func (h *Host) quitMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_CLOSE:
		if !h.askQuit(false) {
			return 0, true // canceled: the window stays
		}
	case wmQueryEndSession:
		h.askQuit(true)
		return 1, true
	case wmEndSession:
		if wp != 0 {
			h.askQuit(true)
			h.savePlacement(h.mainCtl)
			h.lifecycle(ops.LifecycleBackground)
			saveState()
		}
		return 0, true
	}
	return 0, false
}

// Recent documents (asuka/recent): the shell's recent items for the app,
// which its jump list's Recent shows (for the file types it's registered
// to open), and its own list, newest first, which List returns, kept in
// its data directory.

func recentPath() string { return filepath.Join(appdir.Data(), "recent.json") }

func readRecent() []string {
	var list []string
	if b, err := os.ReadFile(recentPath()); err == nil {
		json.Unmarshal(b, &list)
	}
	return list
}

func writeRecent(list []string) error {
	p := recentPath()
	if len(list) == 0 {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(list)
	return os.WriteFile(p, b, 0o600)
}

func (h *Host) recentModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"add": func(args []byte, reply func([]byte, error)) {
			path := string(args)
			AddRecentDocument(path)
			reply(nil, writeRecent(addRecent(readRecent(), path)))
		},
		"clear": func(_ []byte, reply func([]byte, error)) {
			err := writeRecent(nil)
			if e := ClearJumpListRecent(); err == nil {
				err = e
			}
			reply(nil, err)
		},
		"list": func(_ []byte, reply func([]byte, error)) {
			list := []string{}
			for _, p := range readRecent() {
				if _, err := os.Stat(p); err == nil {
					list = append(list, p)
				}
			}
			b, _ := json.Marshal(list)
			reply(b, nil)
		},
	}
}

// Start at login (asuka/login): a value of the user's Run key, named for
// the app, whose command starts it with boot.LoginArg; Task Manager's
// Startup apps lists it, and turns it off in StartupApproved.

func (h *Host) loginName() string {
	if boot.ID != "" {
		return boot.ID
	}
	return h.appName
}

func (h *Host) loginModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"set": func(args []byte, reply func([]byte, error)) {
			name := h.loginName()
			if len(args) == 0 || args[0] == 0 {
				reply(nil, RegDeleteUserValue(runKey, name))
				return
			}
			exe, err := os.Executable()
			if err != nil {
				reply(nil, err)
				return
			}
			if err := RegSetUserString(runKey, name, loginCommand(exe, boot.LoginArg)); err != nil {
				reply(nil, err)
				return
			}
			// Turned on again: no longer off in Task Manager's list.
			if b, ok := RegBinary(startupApprovedKey, name); ok && startupDisabled(b) {
				RegDeleteUserValue(startupApprovedKey, name)
			}
			reply(nil, nil)
		},
		"enabled": func(_ []byte, reply func([]byte, error)) {
			name := h.loginName()
			on := false
			if cmd, ok := RegUserString(runKey, name); ok {
				exe, _ := os.Executable()
				on = loginNamesExe(cmd, exe)
				if b, ok := RegBinary(startupApprovedKey, name); ok && startupDisabled(b) {
					on = false
				}
			}
			if on {
				reply([]byte{1}, nil)
			} else {
				reply([]byte{0}, nil)
			}
		},
		"openedAtLogin": func(_ []byte, reply func([]byte, error)) {
			if boot.OpenedAtLogin {
				reply([]byte{1}, nil)
			} else {
				reply([]byte{0}, nil)
			}
		},
	}
}

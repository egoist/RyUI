//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/fileurl"
)

// What the app opens, its quit, its recent documents and its login item,
// on a Mac.
//
// Files: Launch Services opens the app with them (a double click in
// Finder, Open With, a file dropped on its Dock icon) through
// application:openURLs:, together with links of its scheme; the files go
// to the core as file:// URLs, one per line (internal/fileurl), so that
// ui.OnOpenFiles gets those opened together at once.
//
// Quit: however it starts (⌘Q, the Dock's Quit, app.Quit, a signal, the
// last window closing, the session ending), AppKit asks the delegate
// first (applicationShouldTerminate:), which asks app.OnQuit's handlers
// once: a quit they allow only finishes.
//
// Recent documents are NSDocumentController's, which keeps them across
// launches and lists them in the Dock menu; the File menu's Open Recent
// lists them too, and choosing one opens it as Finder would.
//
// The login item is SMAppService's mainAppService (macOS 13).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["recent"] = h.recentModule()
		m["login"] = loginModule()
	})
	targetMethods = append(targetMethods,
		Method("applicationShouldTerminate:", func(self ID, _ SEL, app ID) uintptr { return theHost.shouldTerminate() }),
		Method("workspaceWillPowerOff:", func(self ID, _ SEL, n ID) { quitState.ending = true }),
		Method("appLaunched:", func(self ID, _ SEL, n ID) { appLaunched() }),
		Method("openRecent:", func(self ID, _ SEL, item ID) {
			if u := Send(item, "representedObject"); u != 0 {
				theHost.openURLs(Array(u))
			}
		}),
		Method("clearRecent:", func(self ID, _ SEL, item ID) { clearRecent() }),
	)
}

// openURLs is application:openURLs:: the files among urls (their paths,
// as file:// URLs again) and its other URLs, in one IncomingURL event.
func (h *Host) openURLs(urls ID) {
	var lines []string
	for _, u := range ArrayItems(urls) {
		if SendBool(u, "isFileURL") {
			if p := GoString(Send(u, "path")); p != "" {
				lines = append(lines, fileurl.FromPath(p))
			}
			continue
		}
		lines = append(lines, GoString(Send(u, "absoluteString")))
	}
	if len(lines) > 0 {
		h.openURL(strings.Join(lines, "\n"))
	}
}

// Quitting.

// NSApplicationTerminateReply's.
const (
	terminateCancel = 0
	terminateNow    = 1
)

var quitState struct {
	allowed bool // a quit the handlers allowed only finishes
	ending  bool // the session ends (NSWorkspaceWillPowerOffNotification)
	signals sync.Once
}

// startQuit, as the app starts: app.Quit, the session's end, and SIGINT
// and SIGTERM (Ctrl+C in the terminal), which quit as ⌘Q does; a second
// signal ends the app at once (its main thread may be stuck). Not under
// asuka dev, whose SIGINT saves the run's state (state.go).
func (h *Host) startQuit() {
	boot.Quit = quitApp
	ws := Send(Class("NSWorkspace"), "sharedWorkspace")
	Send(Send(ws, "notificationCenter"), "addObserver:selector:name:object:", uintptr(h.target),
		uintptr(Sel("workspaceWillPowerOff:")), uintptr(String("NSWorkspaceWillPowerOffNotification")), 0)
	Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "addObserver:selector:name:object:", uintptr(h.target),
		uintptr(Sel("appLaunched:")), uintptr(String("NSApplicationDidFinishLaunchingNotification")), 0)
	if os.Getenv("ASUKA_STATE") != "" {
		return
	}
	quitState.signals.Do(func() {
		sig := make(chan os.Signal, 2)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			s := <-sig
			quitApp()
			<-sig
			code := 128
			if n, ok := s.(syscall.Signal); ok {
				code += int(n)
			}
			os.Exit(code)
		}()
	})
}

// quitApp is boot.Quit: the application's terminate:, from the run loop
// (not from within a call into the core, which the quit's last events
// would wait for). Safe from any goroutine.
func quitApp() {
	Send(Send(Class("NSApplication"), "sharedApplication"), "performSelectorOnMainThread:withObject:waitUntilDone:",
		uintptr(Sel("terminate:")), 0, 0)
}

// shouldTerminate is applicationShouldTerminate:: app.OnQuit's handlers,
// once; they can't cancel the session's end.
func (h *Host) shouldTerminate() uintptr {
	if quitState.allowed {
		return terminateNow
	}
	ending := quitState.ending
	quitState.ending = false
	canceled := false
	if boot.QuitRequested != nil {
		h.enter(func() { canceled = boot.QuitRequested(ending) })
	}
	if canceled && !ending {
		return terminateCancel
	}
	quitState.allowed = true
	return terminateNow
}

// Recent documents.

func documentController() ID {
	return Send(Class("NSDocumentController"), "sharedDocumentController")
}

var recentState struct {
	used bool // recent.Add called: the File menu has Open Recent
	menu ID   // Open Recent's submenu, filled as it opens
	once sync.Once
}

func (h *Host) recentModule() map[string]method {
	return map[string]method{
		"add": func(args []byte, reply func([]byte, error)) {
			path := string(args)
			if !filepath.IsAbs(path) {
				reply(nil, errors.New("asuka/sdk/recent: not an absolute path"))
				return
			}
			dc := documentController()
			Send(dc, "noteNewRecentDocumentURL:", uintptr(FileURL(path)))
			// Read back at once: AppKit coalesces the notes made before
			// it reads its list, and a document noted again then keeps
			// its place.
			Send(dc, "recentDocumentURLs")
			if !recentState.used {
				recentState.used = true
				if recentState.menu == 0 { // the File menu without Open Recent
					h.makeMenu(Send(Class("NSApplication"), "sharedApplication"))
				}
			}
			reply(nil, nil)
		},
		"clear": func(_ []byte, reply func([]byte, error)) {
			clearRecent()
			reply(nil, nil)
		},
		"list": func(_ []byte, reply func([]byte, error)) {
			b, _ := json.Marshal(recentPaths())
			reply(b, nil)
		},
	}
}

func clearRecent() { Send(documentController(), "clearRecentDocuments:", 0) }

// recentPaths are the recent documents that still exist, newest first.
func recentPaths() []string {
	paths := []string{}
	for _, u := range ArrayItems(Send(documentController(), "recentDocumentURLs")) {
		if !SendBool(u, "isFileURL") {
			continue
		}
		p := GoString(Send(u, "path"))
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// hasOpenRecent is whether the File menu has Open Recent: the app opens
// files (its bundle declares document types, asuka.json's fileTypes),
// keeps recent documents (recent.Add), or has some from before.
func (h *Host) hasOpenRecent() bool {
	if recentState.used {
		return true
	}
	if Send(Send(Class("NSBundle"), "mainBundle"), "objectForInfoDictionaryKey:", uintptr(String("CFBundleDocumentTypes"))) != 0 {
		return true
	}
	return SendInt(Send(documentController(), "recentDocumentURLs"), "count") > 0
}

// addOpenRecent adds Open Recent to the File menu: its items, made as it
// opens (the recent documents that still exist, with their icons and,
// where two have one name, their folders'), then Clear Menu.
func (h *Host) addOpenRecent(file ID) {
	recentState.once.Do(func() {
		DefineClass("AsukaRecentMenu", "NSObject", nil, []objc.MethodDef{
			Method("menuNeedsUpdate:", func(self ID, _ SEL, m ID) { theHost.fillRecent(m) }),
		})
	})
	title := h.menuWord("Open Recent")
	m := menu(title)
	Send(m, "setAutoenablesItems:", 0)
	delegate := New("AsukaRecentMenu") // the menu doesn't keep it: the host does
	Send(m, "setDelegate:", uintptr(delegate))
	if recentState.menu != 0 {
		Release(Send(recentState.menu, "delegate"))
		Release(recentState.menu)
	}
	recentState.menu = Retain(m)
	h.fillRecent(m)
	sub(file, title, m)
}

// fillRecent makes Open Recent's items.
func (h *Host) fillRecent(m ID) {
	Send(m, "removeAllItems")
	paths := recentPaths()
	fm := Send(Class("NSFileManager"), "defaultManager")
	ws := Send(Class("NSWorkspace"), "sharedWorkspace")
	names := make([]string, len(paths))
	count := map[string]int{}
	for i, p := range paths {
		names[i] = GoString(Send(fm, "displayNameAtPath:", uintptr(String(p))))
		count[names[i]]++
	}
	for i, p := range paths {
		title := names[i]
		if count[title] > 1 {
			title += " — " + GoString(Send(fm, "displayNameAtPath:", uintptr(String(filepath.Dir(p)))))
		}
		it := item(m, title, "openRecent:", "", 0)
		Send(it, "setTarget:", uintptr(h.target))
		Send(it, "setRepresentedObject:", uintptr(FileURL(p)))
		icon := Send(Send(ws, "iconForFile:", uintptr(String(p))), "copy")
		MsgSetSize(icon, Sel("setSize:"), Size{Width: 16, Height: 16})
		Send(it, "setImage:", uintptr(icon))
		Release(icon)
	}
	if len(paths) > 0 {
		sep(m)
	}
	clear := item(m, h.menuWord("Clear Menu"), "clearRecent:", "", 0)
	Send(clear, "setTarget:", uintptr(h.target))
	Send(clear, "setEnabled:", Bool(len(paths) > 0))
}

// Login item.

// SMAppServiceStatus's.
const (
	smNotRegistered    = 0
	smEnabled          = 1
	smRequiresApproval = 2
)

var smOnce sync.Once

// mainAppService is the app's SMAppService, or why there's none: an app
// without a bundle (go run), or a macOS before 13.
func mainAppService() (ID, error) {
	if !bundled() {
		return 0, errors.New("asuka/sdk/login: a login item needs the app's bundle: build it with asuka build, or run it with asuka dev")
	}
	smOnce.Do(func() { Dlopen("/System/Library/Frameworks/ServiceManagement.framework/ServiceManagement") })
	if Class("SMAppService") == 0 {
		return 0, errors.New("asuka/sdk/login: login items need macOS 13 or later")
	}
	return Send(Class("SMAppService"), "mainAppService"), nil
}

var loginState struct {
	launched bool
	atLogin  bool
	waiting  []func([]byte, error)
}

// appLaunched: the app finished launching; the Apple Event that opened
// it says whether it opened as a login item.
func appLaunched() {
	if loginState.launched {
		return
	}
	loginState.launched = true
	const keyAEPropData = 0x70726474            // 'prdt'
	const keyAELaunchedAsLogInItem = 0x6c676974 // 'lgit'
	const kAEOpenApplication = 0x6f617070       // 'oapp'
	ev := Send(Send(Class("NSAppleEventManager"), "sharedAppleEventManager"), "currentAppleEvent")
	if ev != 0 && uint32(Send(ev, "eventID")) == kAEOpenApplication {
		if p := Send(ev, "paramDescriptorForKeyword:", keyAEPropData); p != 0 {
			loginState.atLogin = uint32(Send(p, "enumCodeValue")) == keyAELaunchedAsLogInItem
		}
	}
	loginState.atLogin = loginState.atLogin || boot.OpenedAtLogin
	r := []byte{0}
	if loginState.atLogin {
		r[0] = 1
	}
	for _, reply := range loginState.waiting {
		reply(r, nil)
	}
	loginState.waiting = nil
}

func loginModule() map[string]method {
	return map[string]method{
		"set": func(args []byte, reply func([]byte, error)) {
			s, err := mainAppService()
			if err != nil {
				reply(nil, err)
				return
			}
			on := len(args) > 0 && args[0] == 1
			status := SendInt(s, "status")
			var nserr ID
			switch {
			case on && status == smEnabled, !on && status != smEnabled && status != smRequiresApproval:
			case on:
				if !SendBool(s, "registerAndReturnError:", uintptr(unsafe.Pointer(&nserr))) && SendInt(s, "status") != smRequiresApproval {
					reply(nil, errorText(nserr, "asuka/sdk/login: the login item wasn't registered"))
					return
				}
			default:
				if !SendBool(s, "unregisterAndReturnError:", uintptr(unsafe.Pointer(&nserr))) && SendInt(s, "status") == smEnabled {
					reply(nil, errorText(nserr, "asuka/sdk/login: the login item wasn't removed"))
					return
				}
			}
			if on && SendInt(s, "status") == smRequiresApproval {
				reply([]byte{2}, nil)
				return
			}
			reply(nil, nil)
		},
		"enabled": func(_ []byte, reply func([]byte, error)) {
			s, err := mainAppService()
			if err != nil {
				reply([]byte{0}, nil) // not a login item: it can't be one
				return
			}
			if SendInt(s, "status") == smEnabled {
				reply([]byte{1}, nil)
				return
			}
			reply([]byte{0}, nil)
		},
		"openedAtLogin": func(_ []byte, reply func([]byte, error)) {
			if !loginState.launched {
				loginState.waiting = append(loginState.waiting, reply)
				return
			}
			if loginState.atLogin {
				reply([]byte{1}, nil)
				return
			}
			reply([]byte{0}, nil)
		},
	}
}

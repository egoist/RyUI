package ui

import (
	"encoding/binary"
	"errors"
	"slices"

	"github.com/egoist/asuka/internal/fileurl"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// calls are the Calls waiting for their Result, by call id; callRoots,
// the root each went through, whose host answers it.
var (
	calls     = map[uint32]func([]byte, error){}
	callRoots = map[uint32]*Root{}
	nextCall  uint32
)

// errRootGone answers the Calls a disposed root's host never will.
var errRootGone = errors.New("ui: the root went before the call was answered")

// failCalls answers the Calls waiting on r's host, which is gone: a
// goroutine in CallWait (a package's poll of "next") ends instead of
// waiting forever.
func failCalls(r *Root) {
	for id, cr := range callRoots {
		if cr != r {
			continue
		}
		done := calls[id]
		delete(calls, id)
		delete(callRoots, id)
		if done != nil {
			done(nil, errRootGone)
		}
	}
}

// Call runs method of a native module (the host's own, such as
// "secureStore" or "photos", or one the app registers in Swift and
// Kotlin), with args; done gets its result, on the main thread. Main
// thread.
//
// Packages wrap the built-in modules (asuka/sdk/secure); an app's own modules
// take whatever bytes it gives them, JSON for instance.
func Call(module, method string, args []byte, done func(result []byte, err error)) {
	if cur == nil && len(roots) == 0 {
		// Before Mount, or after the last root went (a package's
		// cleanup that's put off).
		if done != nil {
			done(nil, errors.New("ui: Call without a mounted root"))
		}
		return
	}
	r := activeRoot()
	nextCall++
	id := nextCall
	if done != nil {
		calls[id] = done
		callRoots[id] = r
	}
	r.batch.CommandArgs(r.el.id, ops.CmdCall, func(b []byte) []byte {
		le := binary.LittleEndian
		b = le.AppendUint32(b, id)
		b = le.AppendUint32(b, uint32(len(module)))
		b = append(b, module...)
		b = le.AppendUint32(b, uint32(len(method)))
		b = append(b, method...)
		return append(b, args...)
	})
}

// CallWait is Call from a goroutine: it waits for the result. Never on
// the main thread, which the result comes on.
func CallWait(module, method string, args []byte) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	Post(func() {
		Call(module, method, args, func(b []byte, err error) { ch <- result{b, err} })
		flushAll()
	})
	r := <-ch
	return r.b, r.err
}

// ViewID is the id the host knows n's view by, for a package's module
// call that points at a view (the button a popover comes from, the
// WebView to print); 0 if n has no view of its own, or not yet (its
// effects haven't run, it's disposed). A host answers a module call for
// a view it doesn't have as for none. Main thread.
func ViewID(n Node) uint32 {
	if n.e == nil || !n.e.real || n.e.dead {
		return 0
	}
	return uint32(n.e.id)
}

// result delivers a Result event to its Call.
func result(b []byte) {
	if len(b) < 5 {
		return
	}
	id := binary.LittleEndian.Uint32(b)
	done := calls[id]
	if done == nil {
		return
	}
	delete(calls, id)
	delete(callRoots, id)
	if b[4] == 0 {
		done(nil, errors.New(string(b[5:])))
		return
	}
	done(append([]byte(nil), b[5:]...), nil)
}

// urlHandlers get the URLs the app is opened with; pendingURL is one
// that came before any handler (the URL the app launched with).
var (
	urlHandlers []*func(string)
	pendingURL  string
)

// OnOpenURL calls f with each URL the app is opened with: its scheme
// (asuka.json's "scheme", myapp://...), or a universal or app link
// (asuka.json's "links"), at launch or since, that no Router's route
// takes (a Router goes to those itself). A URL that came before any
// handler (the one the app launched with) goes to the first, once the
// app is mounted (a Router mounted with it has it first). It stops with
// the component's owner. Main thread.
//
// Files are OnOpenFiles', not its: a file:// URL never comes here.
func OnOpenURL(f func(url string)) {
	p := &f
	urlHandlers = append(urlHandlers, p)
	signal.OnCleanup(func() { urlHandlers = without(urlHandlers, p) })
	if pendingURL != "" {
		afterBuild(func() {
			if pendingURL == "" || !slices.Contains(urlHandlers, p) {
				return
			}
			u := pendingURL
			pendingURL = ""
			if !routeURL(u) {
				f(u)
			}
		})
	}
}

// building counts the roots being built (Mount); built is what waits for
// them to be (afterBuild).
var (
	building int
	built    []func()
)

// afterBuild runs f once the root being mounted is built (now, if none
// is): a launch URL waits for the Routers mounted with it.
func afterBuild(f func()) {
	if building > 0 {
		built = append(built, f)
		return
	}
	f()
}

// fileHandlers get the files the app is opened with; pendingFiles are
// those that came before any handler, in the order they came.
var (
	fileHandlers []*func([]string)
	pendingFiles [][]string
)

// OnOpenFiles calls f with the files the app is opened with, at launch
// or since: those of asuka.json's "fileTypes" the user opens with it
// (a double click in Finder, Explorer or Files, Open With, a file
// dropped on its Dock icon), a recent document chosen (asuka/sdk/recent:
// the File menu's Open Recent, the Dock menu, the jump list), and on
// Windows and Linux the paths it's started with on the command line. The
// files opened together come in one call, as absolute paths.
//
// The app runs once on the desktop: a second launch with files hands
// them to the one running (app.OnSecondInstance), and they come here
// there. Files that came before any handler (those the app launched
// with) go to the first. It stops with the component's owner. Main
// thread.
//
// What the paths are differs by platform: on macOS, Windows and Linux,
// the files themselves (in a Flatpak, the document portal's paths to
// them). On iOS, the file in place where it is (another app's, iCloud
// Drive's), the app given access to it until it quits; on Android, the
// app's own copy, in its caches (a content:// URI is no path); in a
// browser (an installed web app, its file handlers), the file read into
// the app's caches.
func OnOpenFiles(f func(paths []string)) {
	p := &f
	fileHandlers = append(fileHandlers, p)
	signal.OnCleanup(func() { fileHandlers = without(fileHandlers, p) })
	if len(pendingFiles) > 0 {
		pending := pendingFiles
		pendingFiles = nil
		for _, ps := range pending {
			f(ps)
		}
	}
}

// without is hs without h.
func without[T any](hs []*T, h *T) []*T {
	for i, x := range hs {
		if x == h {
			return append(hs[:i:i], hs[i+1:]...)
		}
	}
	return hs
}

// incoming takes an IncomingURL event's string: one URL, or files the app
// is opened with, as file:// URLs one per line (internal/fileurl).
func incoming(s string) {
	paths, urls := fileurl.Split(s)
	if len(paths) > 0 {
		openFiles(paths)
	}
	for _, u := range urls {
		openURL(u)
	}
}

func openURL(url string) {
	if routeURL(url) {
		return
	}
	if len(urlHandlers) == 0 {
		pendingURL = url
		return
	}
	hs := make([]*func(string), len(urlHandlers))
	copy(hs, urlHandlers)
	for _, h := range hs {
		(*h)(url)
	}
}

func openFiles(paths []string) {
	if len(fileHandlers) == 0 {
		pendingFiles = append(pendingFiles, paths)
		return
	}
	hs := make([]*func([]string), len(fileHandlers))
	copy(hs, fileHandlers)
	for _, h := range hs {
		(*h)(append([]string(nil), paths...))
	}
}

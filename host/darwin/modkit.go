//go:build darwin && !ios

package darwin

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"

	. "github.com/egoist/ryui/internal/darwin"
)

// What the host's modules share: work off the main thread, the main
// dispatch queue, a framework loaded once, the app's caches.

// offMain runs work on a thread of its own (in an autorelease pool: it
// may message Objective-C objects that are safe off the main thread),
// then answers on the main thread.
func (h *Host) offMain(work func() ([]byte, error), reply func([]byte, error)) {
	go func() {
		var out []byte
		var err error
		func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			WithPool(func() { out, err = work() })
		}()
		h.Post(func() { reply(out, err) })
	}()
}

var mainQueueAddr = sync.OnceValue(func() uintptr {
	sys := Dlopen("/usr/lib/libSystem.B.dylib")
	q, _ := purego.Dlsym(sys, "_dispatch_main_q")
	return q
})

// mainQueue is the main dispatch queue (a delegate's queue: the main run
// loop serves it).
func mainQueue() uintptr { return mainQueueAddr() }

// framework opens a system framework once (0 if it isn't there).
func framework(name string) uintptr {
	frameworksMu.Lock()
	defer frameworksMu.Unlock()
	if h, ok := frameworks[name]; ok {
		return h
	}
	h := Dlopen("/System/Library/Frameworks/" + name + ".framework/" + name)
	frameworks[name] = h
	return h
}

var (
	frameworksMu sync.Mutex
	frameworks   = map[string]uintptr{}
)

// cacheFile is a new file's path in a folder of the app's caches, the
// folder made (iOS's caches hold the same: ryui-camera, ryui-audio...).
func cacheFile(folder, ext string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	app := GoString(Send(Send(Class("NSBundle"), "mainBundle"), "bundleIdentifier"))
	if app == "" && theHost != nil {
		app = theHost.appName
	}
	if app == "" {
		app = "ryui"
	}
	dir := filepath.Join(base, app, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	uuid := GoString(Send(Send(Class("NSUUID"), "UUID"), "UUIDString"))
	return filepath.Join(dir, uuid+ext), nil
}

// errorText is an NSError's description, or fallback.
func errorText(err ID, fallback string) error {
	if err == 0 {
		return errors.New(fallback)
	}
	return errors.New(GoString(Send(err, "localizedDescription")))
}

// errNotAllowed is what a module answers when the user hasn't allowed
// it (the packages map it to their ErrNotAllowed).
var errNotAllowed = errors.New("not allowed")

// hostLocale is the language the app shows, for the host's own words.
func hostLocale() string {
	if theHost == nil {
		return ""
	}
	return theHost.locale
}

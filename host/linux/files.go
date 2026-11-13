//go:build linux && !android

package linux

import (
	"os"
	"slices"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/fileurl"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ui"
)

// What the app is opened with, and its one instance. GApplication gets
// the whole command line (HANDLES_COMMAND_LINE): the instance that runs
// gets each launch's, its own first (the files and URLs it was started
// with: the desktop file's %U, a terminal's arguments), then another
// launch's, which GApplication hands it over D-Bus as that launch ends
// (a unique application: one per app ID). The files among the arguments
// (those that exist, from the launch's working directory, or file://
// URIs) and the URLs of the app's scheme reach the root together, one
// IncomingURL (internal/fileurl): ui.OnOpenFiles gets the files,
// ui.OnOpenURL the URLs; another launch's arguments go to
// app.OnSecondInstance as they are. GApplication's open (D-Bus
// activation, the file manager's Open With of a DBusActivatable app)
// comes the same way.

// GApplicationFlags the host uses.
const (
	appHandlesOpen        = 1 << 2
	appHandlesCommandLine = 1 << 3
	appNonUnique          = 1 << 5
)

// appFlags are the application's flags: it handles its command line and
// open, and is unique unless it has no ID, runs for automation, a test
// or asuka dev, or asked to run as often as it's started
// (multiple: boot.MultipleInstances).
func appFlags(id, multiple string, getenv func(string) string) uint32 {
	flags := uint32(appHandlesOpen | appHandlesCommandLine)
	if id == "" || multiple == "1" {
		return flags | appNonUnique
	}
	for _, k := range []string{"ASUKA_SNAPSHOT", "ASUKA_SCRIPT", "ASUKA_STATE", "ASUKA_COMMANDS"} {
		if getenv(k) != "" {
			return flags | appNonUnique
		}
	}
	return flags
}

// exists reports whether a path names a file (or a folder).
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// launchArgs are a launch's arguments without the program and Asuka's
// own (boot.LoginArg).
func launchArgs(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	args := slices.Clone(argv[1:])
	return slices.DeleteFunc(args, func(a string) bool { return a == boot.LoginArg })
}

// watchLaunches starts the app on its first launch (activate, open, or
// its command line) and takes the next.
func (h *Host) watchLaunches(root func() ui.Node) {
	started := false
	start := func() bool {
		if started {
			return false
		}
		started = true
		h.start(root)
		return true
	}
	Connect(h.app, "activate", func() {
		if !start() {
			h.toFront()
		}
	})
	Connect(h.app, "command-line", func(cl uintptr) uintptr {
		argv, cwd := CommandLineArgs(cl)
		args := launchArgs(argv)
		if start() {
			h.incomingURL(fileurl.FromArgs(args, cwd, boot.Scheme, exists))
			return 0
		}
		h.launchedAgain(args, cwd)
		return 0
	})
	Connect(h.app, "open", func(files, n, hint uintptr) {
		if !start() {
			h.toFront()
		}
		gfiles := make([]Ptr, int(int32(n)))
		for i := range gfiles {
			gfiles[i] = *At[Ptr](files + uintptr(i)*8)
		}
		h.incomingURL(openedString(gfiles))
	})
}

// openedString is the IncomingURL string of the GFiles the app is opened
// with: the local ones' paths as file:// URLs, the rest's URIs (its
// scheme's links).
func openedString(files []Ptr) string {
	var lines []string
	for _, f := range files {
		if p := GoStringFree(Sys.FileGetPath(f)); p != "" {
			lines = append(lines, fileurl.FromPath(p))
		} else if u := GoStringFree(FileGetURI(f)); u != "" {
			lines = append(lines, u)
		}
	}
	return strings.Join(lines, "\n")
}

// launchedAgain is another launch of the app, handed to this one: the
// main window comes to the front, OnSecondInstance gets the launch's
// arguments, and its files and links come as IncomingURL.
func (h *Host) launchedAgain(args []string, cwd string) {
	h.toFront()
	h.afterCore(func() {
		if boot.SecondInstance != nil && h.root != nil {
			h.enter(func() { boot.SecondInstance(args, cwd) })
		}
	})
	h.incomingURL(fileurl.FromArgs(args, cwd, boot.Scheme, exists))
}

// toFront brings the app's main window (else the one last active) to
// the front.
func (h *Host) toFront() {
	switch {
	case h.main != nil && !h.main.closing:
		WindowPresent(h.main.window)
	case h.activeWin != nil && !h.activeWin.closing:
		WindowPresent(h.activeWin.window)
	}
}

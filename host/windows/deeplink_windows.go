//go:build windows

package windows

import (
	"fmt"
	"os"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/fileurl"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// msgWinTitle is the message window's title: the one a second launch of a
// running-once app finds it by, else "".
var msgWinTitle string

// takeInstance makes this process the app's one instance (deeplink.go), or
// hands the running one its working directory and arguments (files.go)
// and reports false. One that COM started for a toast's click is always
// the instance (COM starts it only when none runs), and an app without an
// ID, one whose asuka.json lets it run more than once ("singleInstance":
// false), driven by automation or run by asuka dev runs as often as it's
// started.
func takeInstance(dir string, args []string) bool {
	if launchedForToast || boot.MultipleInstances == "1" || !singleInstance(boot.ID, os.Getenv) {
		return true
	}
	mutex, title := instanceNames(boot.ID)
	if !TakeInstance(mutex, classMessage, title, instanceTag, instancePayload(dir, args)) {
		return false
	}
	msgWinTitle = title
	return true
}

// launchOpens is the IncomingURL string of a launch's arguments: the
// files and the scheme's URLs among them (internal/fileurl).
func launchOpens(args []string, dir string) string {
	return fileurl.FromArgs(args, dir, boot.Scheme, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

// startLinks, once the root is mounted: registers the app's scheme and
// file types for the user, and passes on the files and URLs the app was
// opened with (launch: their IncomingURL string).
func (h *Host) startLinks(launch string) {
	if boot.Scheme != "" && !notItself(os.Getenv) {
		if exe, err := os.Executable(); err == nil {
			for _, v := range urlSchemeValues(boot.Scheme, h.appName, exe) {
				if have, ok := RegUserString(v[0], v[1]); ok && have == v[2] {
					continue
				}
				if err := RegSetUserString(v[0], v[1], v[2]); err != nil {
					fmt.Fprintf(os.Stderr, "asuka: registering the %s: scheme: %v\n", boot.Scheme, err)
					break
				}
			}
		}
	}
	h.registerFileTypes() // files_windows.go
	var urls []string
	if launch != "" {
		urls = append(urls, launch)
	}
	urls = append(urls, h.pendingURLs...)
	h.pendingURLs = nil
	for _, u := range urls {
		h.incomingURL(u)
	}
}

// launchedAgain is a second launch of the app, handed over (WM_COPYDATA:
// its working directory and arguments): the app comes to the front,
// app.OnSecondInstance gets them, and the root the files and URLs among
// them. Handled after the message is answered, so that the launch it came
// from can quit.
func (h *Host) launchedAgain(payload []byte) {
	dir, args := parseInstancePayload(payload)
	h.Post(func() {
		h.toFront()
		if boot.SecondInstance != nil {
			boot.SecondInstance(args, dir)
		}
		if u := launchOpens(args, dir); u != "" {
			h.incomingURL(u)
		}
	})
}

// incomingURL reports what the app was opened with to the root, once it's
// mounted: a URL, or files (and URLs) together, one per line
// (internal/fileurl).
func (h *Host) incomingURL(u string) {
	h.afterCore(func() {
		if h.root == nil {
			h.pendingURLs = append(h.pendingURLs, u)
			return
		}
		h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventIncomingURL, Str: u})
	})
}

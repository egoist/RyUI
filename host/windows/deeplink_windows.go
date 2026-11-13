//go:build windows

package windows

import (
	"fmt"
	"os"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// msgWinTitle is the message window's title: the one a second launch of a
// running-once app finds it by, else "".
var msgWinTitle string

// takeInstance makes this process the app's one instance (deeplink.go), or
// hands the running one the URL it was launched with (launch, or "": to
// come to the front) and reports false. One that COM started for a
// toast's click is always the instance (COM starts it only when none
// runs), and an app without an ID, driven by automation or run by asuka
// dev runs as often as it's started.
func takeInstance(launch string) bool {
	if launchedForToast || !singleInstance(boot.ID, os.Getenv) {
		return true
	}
	mutex, title := instanceNames(boot.ID)
	if !TakeInstance(mutex, classMessage, title, instanceTag, []byte(launch)) {
		return false
	}
	msgWinTitle = title
	return true
}

// startLinks, once the root is mounted: registers the app's scheme for the
// user, and passes on the URLs the app was opened with.
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

// launchedAgain is a second launch of the app, handed over (WM_COPYDATA):
// the app comes to the front, and gets the URL it was opened with, if
// any. Handled after the message is answered, so that the launch it came
// from can quit.
func (h *Host) launchedAgain(u string) {
	h.Post(func() {
		h.toFront()
		if u != "" {
			h.incomingURL(u)
		}
	})
}

// incomingURL reports a URL the app was opened with to the root: once it's
// mounted.
func (h *Host) incomingURL(u string) {
	h.afterCore(func() {
		if h.root == nil {
			h.pendingURLs = append(h.pendingURLs, u)
			return
		}
		h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventIncomingURL, Str: u})
	})
}

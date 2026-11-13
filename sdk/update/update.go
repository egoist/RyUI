// Package update keeps a desktop app up to date, as Sparkle does on the
// Mac: it checks a feed for a newer version, shows its release notes,
// downloads it (only what changed, when the feed has a delta from the
// running version), checks that it's signed with the app's key, installs
// it and relaunches the app into it.
//
// The ready-made way is the update window, Sparkle's, in Asuka's own
// components:
//
//	func Root() ui.Node {
//		return ui.Column(ui.Style{},
//			update.Window(update.Options{}), // checks in the background, shows the window
//			ui.MenuBar(ui.Val([]ui.MenuBarMenu{update.AppMenu()})),
//			...
//		)
//	}
//
// It checks soon after launch and then once a day; when a version is out,
// it offers Install Update, Skip This Version and Remind Me Later, with the
// version's release notes (Markdown), then shows the download's progress
// and offers to relaunch. CheckForUpdates checks as the user asked (the
// "Check for Updates…" item of MenuItem and AppMenu): the window says
// when the app is up to date, too.
//
// Apps that make their own interface use Check, Release.Install and
// Relaunch, or follow the updater's State (Use).
//
// Updates come from where asuka.json's "updates" says: GitHub releases,
// a static HTTPS location (an S3 bucket, a CDN: the JSON feed asuka build
// writes), or a server of the app's own that answers with the same JSON.
// asuka build builds the feed's address and the public key into the app,
// and signs the update archives with the private key (docs/shipping.md,
// Updates); an archive or delta that isn't signed with it is never
// installed.
//
// Where an app installs updates: on the Mac, its bundle, replaced once
// the app quits (its code signature is checked against the running app's
// signer first); on Windows, the folder asuka build makes, its .exe and
// assets replaced while the app runs (the running .exe is renamed aside);
// on Linux, the install prefix of the build's tarball (~/.local), the
// app's files alone. An app the system's packages, Flatpak, Snap, the Mac
// App Store or the Microsoft Store installed is updated by them: there,
// Supported says so and the updater stays quiet. Phones update from their
// stores (ErrUnsupported); the web reloads the page for a new version
// (web.go).
package update

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/asuka/internal/boot"
	iupdate "github.com/egoist/asuka/internal/update"
)

// Set by the Asuka CLI (-ldflags -X) when asuka.json configures updates:
// the feed (the URL of the target's manifest, a feed of GitHub releases,
// or a custom server's URL with {target}, {version} and {channel}), the
// public key updates must be signed with, and the build's target
// (darwin-universal, windows-amd64, linux-arm64...).
var (
	buildFeed   string
	buildKey    string
	buildTarget string
)

// Why an app can't update itself, as Supported and Check say.
var (
	// ErrUnsupported: the platform updates apps itself: the App Store and
	// Google Play on phones.
	ErrUnsupported = errors.New("update: apps update through their store here")
	// ErrNotConfigured: the build has no updates: asuka.json has no
	// "updates", or it's a development build (asuka dev).
	ErrNotConfigured = errors.New("update: this build has no updates (asuka.json's updates; asuka dev's builds have none)")
	// ErrManaged: a package manager, Flatpak, Snap, the Mac App Store or
	// the Microsoft Store installed the app, and updates it.
	ErrManaged = errors.New("update: " + iupdate.ErrManaged.Error())
	// ErrTranslocated: macOS runs the app from a read-only copy, as it
	// does for an app opened where it was downloaded: the user moves it
	// to the Applications folder.
	ErrTranslocated = errors.New("update: " + iupdate.ErrTranslocated.Error())
	// ErrNotWritable: the user can't write where the app is installed.
	ErrNotWritable = errors.New("update: " + iupdate.ErrNotWritable.Error())
	// ErrNotInstalled: the app runs from where asuka build didn't put it
	// (go run, a bare binary).
	ErrNotInstalled = errors.New("update: " + iupdate.ErrNotInstalled.Error())
)

// Version is the running app's version, asuka.json's.
func Version() string { return boot.Version }

// Release is a newer version of the app, which Check found.
type Release struct {
	// Version is the release's, and Notes its release notes, in Markdown
	// (the version's section of the app's CHANGELOG.md).
	Version string
	Notes   string
	// Date is when it was built.
	Date time.Time
	// Size is how much Install downloads: the delta from the running
	// version when there's one, else the whole archive.
	Size int64

	manifest iupdate.Manifest
	source   Source
	key      string
	current  string
}

// Source is where updates come from instead of the feed asuka.json
// configures (SetSource): a service that wants the user's credentials, a
// channel chosen by an account. It serves what asuka build publishes, and
// the updater checks it as it checks the feed's: an archive or delta that
// isn't signed with the app's key is never installed.
type Source interface {
	// Manifest returns the manifest of the newest version for target
	// (darwin-universal, windows-amd64, linux-arm64...) on channel ("" for
	// releases), the JSON asuka build writes as update-<target>.json; a
	// nil reader, with no error, when there's none.
	Manifest(ctx context.Context, target, channel string) (io.ReadCloser, error)
	// Open returns the file at url, as the manifest names it: the archive
	// or a delta.
	Open(ctx context.Context, url string) (io.ReadCloser, error)
}

var (
	source  atomic.Pointer[Source]
	channel atomic.Pointer[string]
)

// SetSource makes Check read manifests from s, and Install download
// through it, instead of from asuka.json's feed; nil goes back to the
// feed. The app must still be built with updates (the key). Any
// goroutine.
func SetSource(s Source) {
	if s == nil {
		source.Store(nil)
		return
	}
	source.Store(&s)
}

// SetChannel makes Check offer the prereleases of channel too, for the
// users who chose an app's beta: "beta" takes 1.4.0-beta.2 (a version's
// channel is the letters its prerelease starts with), "*" every
// prerelease, "" releases only (the default). Releases are offered on
// every channel, so an app on a beta takes the release that follows it.
// Leaving a channel installs no older version: the app updates again once
// a release is newer than the prerelease it runs. It lasts while the app
// runs; keep the user's choice and set it at launch. Any goroutine.
func SetChannel(name string) { channel.Store(&name) }

// Channel returns the channel SetChannel set.
func Channel() string {
	if c := channel.Load(); c != nil {
		return *c
	}
	return ""
}

// CompareVersions compares versions as semantic versioning orders them:
// -1 when a is older than b, 0 when they're the same, 1 when a is newer.
// 1.10.0 is newer than 1.9.2, 1.4.0-beta.2 older than 1.4.0-beta.10, and
// a prerelease older than its release.
func CompareVersions(a, b string) int { return iupdate.Compare(a, b) }

// Check asks the feed (or the Source of SetSource) for a version newer
// than the running one, and returns it, or nil when the app is up to
// date. It returns Supported's error where the app can't update itself.
// It blocks: not on the main thread.
func Check(ctx context.Context) (*Release, error) {
	if err := Supported(); err != nil {
		return nil, err
	}
	src := currentSource()
	return check(ctx, src, buildKey, Version(), buildTarget, Channel())
}

func currentSource() Source {
	if s := source.Load(); s != nil {
		return *s
	}
	return defaultSource()
}

// installedFns are OnInstalled's.
var installedFns struct {
	sync.Mutex
	fs []*func(*Release)
}

// OnInstalled calls f, on the main thread, when an update is installed,
// by the update window or by Release.Install: it runs at the next launch,
// or after Relaunch. On Windows and Linux the app's files are the new
// version's by then; on the Mac the new bundle waits for the app to quit.
func OnInstalled(f func(*Release)) (stop func()) {
	p := &f
	installedFns.Lock()
	installedFns.fs = append(installedFns.fs, p)
	installedFns.Unlock()
	return func() {
		installedFns.Lock()
		defer installedFns.Unlock()
		installedFns.fs = slices.DeleteFunc(installedFns.fs, func(q *func(*Release)) bool { return q == p })
	}
}

func installedHandlers() []*func(*Release) {
	installedFns.Lock()
	defer installedFns.Unlock()
	return slices.Clone(installedFns.fs)
}

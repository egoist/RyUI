package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/appdir"
)

// prefs are the user's choices about updates, which update.json keeps in
// the app's data directory.
type prefs struct {
	// AutomaticChecks is nil until the app or the user chose.
	AutomaticChecks    *bool     `json:"automaticChecks,omitempty"`
	AutomaticDownloads bool      `json:"automaticDownloads,omitzero"`
	SkippedVersion     string    `json:"skippedVersion,omitzero"`
	LastCheck          time.Time `json:"lastCheck,omitzero"`
}

const prefsFile = "update.json"

var store struct {
	sync.Mutex
	loaded bool
	file   string // "" when they can't be saved
	p      prefs
}

// prefsDir is where update.json goes (tests replace it).
var prefsDir = func() (string, error) { return appdir.Data(), nil }

// loadPrefs returns the preferences, read once. store is locked.
func loadPrefs() *prefs {
	if !store.loaded {
		store.loaded = true
		if dir, err := prefsDir(); err == nil {
			store.file = filepath.Join(dir, prefsFile)
			if b, err := os.ReadFile(store.file); err == nil {
				_ = json.Unmarshal(b, &store.p) // a damaged file is ignored
			}
		}
	}
	return &store.p
}

// readPrefs returns a copy of the preferences.
func readPrefs() prefs {
	store.Lock()
	defer store.Unlock()
	return *loadPrefs()
}

// changePrefs changes the preferences with f and saves them.
func changePrefs(f func(p *prefs)) {
	store.Lock()
	defer store.Unlock()
	p := loadPrefs()
	f(p)
	if store.file == "" {
		return
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(store.file), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(store.file+".tmp", b, 0o644); err == nil {
		_ = os.Rename(store.file+".tmp", store.file)
	}
}

// AutomaticChecks reports whether the updater (Window) checks for updates
// in the background: the user's choice, else Options.DisableAutomaticChecks'
// opposite. Any goroutine.
func AutomaticChecks() bool {
	p := readPrefs()
	if p.AutomaticChecks != nil {
		return *p.AutomaticChecks
	}
	return !ctl.opts.DisableAutomaticChecks
}

// SetAutomaticChecks turns checking in the background on or off, for a
// setting of the app (Sparkle's "Automatically check for updates"). Main
// thread.
func SetAutomaticChecks(on bool) {
	changePrefs(func(p *prefs) { p.AutomaticChecks = &on })
	ctl.schedule()
}

// AutomaticDownloads reports whether the updater installs what it finds
// in the background without asking (the update window's checkbox): it
// runs at the next launch. Any goroutine.
func AutomaticDownloads() bool { return readPrefs().AutomaticDownloads }

// SetAutomaticDownloads turns installing updates found in the background
// without asking on or off. Any goroutine.
func SetAutomaticDownloads(on bool) { changePrefs(func(p *prefs) { p.AutomaticDownloads = on }) }

// LastCheck returns when the app last checked for updates successfully,
// or the zero time. Any goroutine.
func LastCheck() time.Time { return readPrefs().LastCheck }

// Package routestate keeps where each desktop window's Router was, across
// launches, as the hosts keep where the window was: the last address its
// Location commands sent (its href and the router's state), by the
// window's key (WindowOptions.StateKey, "main" for the main window;
// WindowProps.StateKey), in asuka-routes.json in the app's data
// directory. The macOS, Windows and Linux hosts give it back as the
// window's Router mounts (host.Locator).
//
// A change is written a moment later on a goroutine of its own, so the
// main thread never waits for the disk; Flush writes what's left as the
// app quits.
package routestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileName is the file's name in the app's data directory.
const FileName = "asuka-routes.json"

// delay is how long a change waits before it's written: navigating
// quickly writes once.
var delay = 300 * time.Millisecond

type entry struct {
	Href  string `json:"href"`
	State string `json:"state,omitempty"`
}

// Store is the file's addresses.
type Store struct {
	mu      sync.Mutex
	path    string
	m       map[string]entry
	pending bool
	timer   *time.Timer
}

// Path is the file's path for a host: "" (keep none) in tests, automation
// runs and under asuka dev, whose reloads keep the router's state
// themselves (internal/devstate), and with no data directory.
func Path() string {
	for _, k := range []string{"ASUKA_SNAPSHOT", "ASUKA_SCRIPT", "ASUKA_TEST", "ASUKA_STATE"} {
		if os.Getenv(k) != "" {
			return ""
		}
	}
	d := os.Getenv("ASUKA_DATA")
	if d == "" {
		return ""
	}
	return filepath.Join(d, FileName)
}

// Open reads the file at path ("": a store that keeps nothing). A missing
// or unreadable file is an empty store.
func Open(path string) *Store {
	s := &Store{path: path, m: map[string]entry{}}
	if path == "" {
		return s
	}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s.m)
	}
	return s
}

// Location is where the window keyed key was left.
func (s *Store) Location(key string) (href string, state []byte, ok bool) {
	if s == nil || key == "" {
		return "", nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	return e.Href, []byte(e.State), ok
}

// Set keeps the window keyed key's address, written a moment later.
func (s *Store) Set(key, href string, state []byte) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := entry{Href: href, State: string(state)}
	if s.m[key] == e {
		return
	}
	s.m[key] = e
	if s.path == "" || s.pending {
		return
	}
	s.pending = true
	s.timer = time.AfterFunc(delay, s.Flush)
}

// Flush writes what changed now (the app quitting).
func (s *Store) Flush() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending || s.path == "" {
		return
	}
	s.pending = false
	if s.timer != nil {
		s.timer.Stop()
	}
	b, err := json.Marshal(s.m)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, s.path)
	}
}

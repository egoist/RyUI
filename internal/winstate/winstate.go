// Package winstate keeps where an app's windows were, by the keys the app
// gives them (WindowOptions.StateKey, WindowProps.StateKey; the main
// window's is "main"), in one JSON file in the app's data directory: the
// Windows and Linux hosts' asuka-window.json. Each host keeps what it
// needs of a window (Windows its rectangle in pixels and DPI, Linux its
// size) as a value of its own type. A file an older host wrote, the main
// window's alone, reads as the main window's.
package winstate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// Main is the main window's key when the app gives none.
const Main = "main"

// Store is the file's states, read once.
type Store struct {
	path   string
	states map[string]json.RawMessage
	saved  []byte // the file as last read or written
}

// Open reads the file at path ("": a store that keeps nothing, for tests
// and automation runs). A missing or unreadable file is an empty store.
func Open(path string) *Store {
	s := &Store{path: path, states: map[string]json.RawMessage{}}
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	s.saved = b
	var all map[string]json.RawMessage
	if json.Unmarshal(b, &all) != nil {
		return s
	}
	for _, v := range all {
		if v = bytes.TrimSpace(v); len(v) == 0 || v[0] != '{' {
			// An older file: one window's fields, the main window's.
			s.states[Main] = json.RawMessage(b)
			return s
		}
	}
	s.states = all
	return s
}

// Load reads key's state into v, reporting whether there was one.
func (s *Store) Load(key string, v any) bool {
	if s == nil || key == "" {
		return false
	}
	raw, ok := s.states[key]
	return ok && json.Unmarshal(raw, v) == nil
}

// Save keeps v as key's state, writing the file if it changed (making the
// directory first).
func (s *Store) Save(key string, v any) {
	if s == nil || s.path == "" || key == "" {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.states[key] = raw
	b, err := json.Marshal(s.states)
	if err != nil || bytes.Equal(b, s.saved) {
		return
	}
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	if os.WriteFile(s.path, b, 0o644) == nil {
		s.saved = b
	}
}

// Forget drops key's state.
func (s *Store) Forget(key string) {
	if s == nil || s.path == "" {
		return
	}
	if _, ok := s.states[key]; !ok {
		return
	}
	delete(s.states, key)
	b, _ := json.Marshal(s.states)
	if os.WriteFile(s.path, b, 0o644) == nil {
		s.saved = b
	}
}

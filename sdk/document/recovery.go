package document

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/asuka/app"
)

// The recovery copies of documents: their unsaved changes, written a
// second after the user stops editing into the app's data, as VS Code's
// backups, Zed's database and GNOME Text Editor's drafts keep them, so
// that a crash, a forced quit or a quit that keeps documents
// (Options.AskOnQuit unset) loses nothing. The next launch opens them
// again, edited; a file that changed meanwhile is in conflict, as its
// stamp says.
//
// They're in "Recovery/<kind>" in app.DataDir, in a session of the
// running instance that a lock file says is alive: another instance of
// the app leaves it alone, and the next launch takes the sessions no
// instance holds.

type recoveryEntry struct {
	ID string `json:"id"`
	// Path is the document's file; Name the name of an untitled one.
	Path  string    `json:"path,omitzero"`
	Name  string    `json:"name,omitzero"`
	Stamp fileStamp `json:"stamp"`
}

// recoveryStore is safe from any goroutine: documents' jobs use it.
type recoveryStore struct {
	name string // the kind's folder

	mu      sync.Mutex
	dir     string // the session, "" until the first copy
	lock    *os.File
	entries map[string]recoveryEntry
}

func newRecoveryStore(types []Type) *recoveryStore {
	name := "documents"
	if len(types) > 0 {
		t := types[0]
		name = t.Name
		if len(t.Extensions) > 0 {
			name += "-" + t.Extensions[0]
		}
	}
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, name)
	return &recoveryStore{name: name, entries: map[string]recoveryEntry{}}
}

// root is the folder of the kind's sessions.
func (s *recoveryStore) root() (string, error) {
	data, err := app.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, "Recovery", s.name), nil
}

// session returns this instance's session, which it makes and locks the
// first time. s.mu is held.
func (s *recoveryStore) session() (string, error) {
	if s.dir != "" {
		return s.dir, nil
	}
	root, err := s.root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, randomID()[:12])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	if err := lockFile(lock); err != nil {
		lock.Close()
		return "", err
	}
	s.dir, s.lock = dir, lock
	return dir, nil
}

// save writes a document's copy and its entry.
func (s *recoveryStore) save(e recoveryEntry, write func(io.Writer) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.session()
	if err != nil {
		return err
	}
	if err := writeAppFile(filepath.Join(dir, e.ID), write); err != nil {
		return err
	}
	s.entries[e.ID] = e
	return s.writeIndex()
}

// remove forgets the copy id.
func (s *recoveryStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return
	}
	delete(s.entries, id)
	if err := s.writeIndex(); err != nil {
		logf("%v", err)
	}
	os.Remove(filepath.Join(s.dir, id))
}

// has reports whether the copy id is kept (tests).
func (s *recoveryStore) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[id]
	return ok
}

// writeIndex writes the session's entries. s.mu is held.
func (s *recoveryStore) writeIndex() error {
	list := make([]recoveryEntry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	return writeAppFile(filepath.Join(s.dir, "index.json"), func(w io.Writer) error {
		return json.NewEncoder(w).Encode(list)
	})
}

// close lets the session go, at quit, removing it when it keeps no copy.
func (s *recoveryStore) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return
	}
	s.lock.Close()
	s.lock = nil
	if len(s.entries) == 0 {
		os.RemoveAll(s.dir)
	}
	s.dir = ""
	clear(s.entries)
}

// orphan is a copy no running instance holds.
type orphan struct {
	entry recoveryEntry
	file  string
}

type claimed struct {
	dir     string
	lock    *os.File
	entries []recoveryEntry
}

// claim takes the sessions no instance holds: those of an instance that
// crashed or quit keeping documents. The function it returns lets them
// go, removing the copies restored.
func (s *recoveryStore) claim() (orphans []orphan, done func(restored map[string]bool)) {
	root, err := s.root()
	if err != nil {
		return nil, func(map[string]bool) {}
	}
	s.mu.Lock()
	own := s.dir
	s.mu.Unlock()
	dirs, _ := os.ReadDir(root)
	var cs []claimed
	for _, d := range dirs {
		dir := filepath.Join(root, d.Name())
		if !d.IsDir() || dir == own {
			continue
		}
		lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			continue
		}
		if lockFile(lock) != nil {
			lock.Close() // alive
			continue
		}
		c := claimed{dir: dir, lock: lock}
		if data, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil {
			if err := json.Unmarshal(data, &c.entries); err != nil {
				logf("%s: %v", dir, err)
			}
		}
		cs = append(cs, c)
		for _, e := range c.entries {
			if e.ID != "" && !strings.ContainsAny(e.ID, `/\.`) {
				orphans = append(orphans, orphan{e, filepath.Join(dir, e.ID)})
			}
		}
	}
	return orphans, func(restored map[string]bool) {
		for i := range cs {
			c := &cs[i]
			var kept, gone []recoveryEntry
			for _, e := range c.entries {
				if restored[filepath.Join(c.dir, e.ID)] {
					gone = append(gone, e)
				} else {
					kept = append(kept, e)
				}
			}
			if len(gone) > 0 && len(kept) > 0 {
				err := writeAppFile(filepath.Join(c.dir, "index.json"), func(w io.Writer) error {
					return json.NewEncoder(w).Encode(kept)
				})
				if err != nil {
					logf("cannot update the recovery index of %s: %v", c.dir, err)
					gone = nil
				}
			}
			for _, e := range gone {
				os.Remove(filepath.Join(c.dir, e.ID))
			}
			c.lock.Close()
			if len(kept) == 0 {
				os.RemoveAll(c.dir)
			}
		}
	}
}

// logf logs what went wrong where nothing can show it.
func logf(format string, args ...any) { log.Printf("asuka/sdk/document: "+format, args...) }

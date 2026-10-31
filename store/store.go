// Package store keeps an app's small values across launches: settings,
// a signed-in user's name, the tab last open. Values are JSON, kept in
// one file in the app's data directory, written off the main thread a
// moment after they change. It's not a database: for many or large
// records, use one (SQLite through a Go driver, files of your own).
//
//	theme := store.Signal("theme", "light") // a signal whose value is kept
//	store.Set("onboarded", true)
//	var done bool
//	store.Get("onboarded", &done)
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// An app going to the background may be ended without a word: what's
// pending is written then.
func init() {
	boot.Lifecycle = append(boot.Lifecycle, func(state int) {
		if ops.Lifecycle(state) == ops.LifecycleBackground {
			Flush()
		}
	})
}

var (
	mu      sync.Mutex
	values  map[string]json.RawMessage
	loaded  bool
	dirty   bool
	saving  bool
	written = make(chan struct{}, 1) // a save finished (Flush waits for it)
	path    string
)

// Dir is the directory the store's file is in: the app's data directory
// (on Android its files directory, as RYUI_DATA says; on iOS Application
// Support in its container), or, running elsewhere (the ryui dev player's
// Mac), the user's config directory.
func Dir() string {
	if d := os.Getenv("RYUI_DATA"); d != "" {
		return d
	}
	// Android sets RYUI_DATA once the library is loaded, after the
	// packages' init: a store.Signal in a package variable reads the
	// store before it. The files directory is the package's, for the
	// process's user: what Context.getFilesDir returns.
	if runtime.GOOS == "android" {
		if b, err := os.ReadFile("/proc/self/cmdline"); err == nil {
			pkg, _, _ := strings.Cut(string(b), "\x00")
			pkg, _, _ = strings.Cut(pkg, ":") // a process of the app's, "pkg:sync"
			if pkg != "" && !strings.Contains(pkg, "/") {
				return fmt.Sprintf("/data/user/%d/%s/files", os.Getuid()/100000, pkg)
			}
		}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "ryui", filepath.Base(os.Args[0]))
}

func load() {
	if loaded {
		return
	}
	loaded = true
	values = map[string]json.RawMessage{}
	if path == "" {
		path = filepath.Join(Dir(), "store.json")
	}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &values)
	}
}

// Get decodes key's value into v (a pointer); false if there's none, or
// it doesn't decode into v.
func Get(key string, v any) bool {
	mu.Lock()
	defer mu.Unlock()
	load()
	b, ok := values[key]
	return ok && json.Unmarshal(b, v) == nil
}

// Set keeps v (anything encoding/json encodes) as key's value.
func Set(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	load()
	values[key] = b
	schedule()
	return nil
}

// Delete forgets key.
func Delete(key string) {
	mu.Lock()
	defer mu.Unlock()
	load()
	if _, ok := values[key]; ok {
		delete(values, key)
		schedule()
	}
}

// schedule saves the values soon, once for the changes made meanwhile.
// mu is held.
func schedule() {
	dirty = true
	if saving {
		return
	}
	saving = true
	go func() {
		time.Sleep(100 * time.Millisecond) // more changes come together
		save()
	}()
}

func save() {
	mu.Lock()
	b, _ := json.Marshal(values)
	dirty = false
	p := path
	mu.Unlock()
	os.MkdirAll(filepath.Dir(p), 0o700)
	// Written whole, then renamed over the old: never half a file.
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, p)
	}
	mu.Lock()
	saving = false
	again := dirty
	if again {
		saving = true
	}
	mu.Unlock()
	if again {
		save()
		return
	}
	select {
	case written <- struct{}{}:
	default:
	}
}

// Flush waits for the values to be written (before the app is
// suspended, in tests).
func Flush() {
	for {
		mu.Lock()
		pending := saving
		mu.Unlock()
		if !pending {
			return
		}
		<-written
	}
}

// Signal returns a signal whose value is key's, kept: it starts with the
// value kept (or def), and each value it's set to is kept. Make it on the
// main thread, as any signal.
func Signal[T any](key string, def T) *signal.Signal[T] {
	v := def
	Get(key, &v)
	s := signal.NewFunc(v, nil)
	first := true
	signal.Root(func(func()) {
		signal.Effect(func() {
			v := s.Get()
			if first {
				first = false
				return
			}
			Set(key, v)
		})
	})
	return s
}

// usePath makes the store's file p, forgetting what was loaded (tests).
func usePath(p string) {
	mu.Lock()
	defer mu.Unlock()
	path, loaded, values = p, false, nil
}

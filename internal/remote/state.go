package remote

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/egoist/ryui/signal"
)

// keepState turns on keeping signals' values across reloads, with the
// values the last run saved in path (if any).
func keepState(path string) error {
	saved := map[string][]byte{}
	defer func() { signal.KeepState(saved) }()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for k, v := range m {
		saved[k] = v
	}
	return nil
}

// saveState writes the live signals' values to path. Main thread.
func saveState(path string) error {
	vals := signal.KeptState()
	m := make(map[string]json.RawMessage, len(vals))
	for k, v := range vals {
		m[k] = v
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// serveAssets serves the app's assets directory to the player, which
// loads ui.Image's asset paths from it.
func serveAssets(addr, dir string) {
	abs, _ := filepath.Abs(dir)
	http.ListenAndServe(addr, http.FileServer(http.Dir(abs)))
}

// KeepState turns on keeping signals' values across reloads, for a host
// that runs the app in its own process under ryui dev (desktop), with the
// values the last run saved in path.
func KeepState(path string) error { return keepState(path) }

// SaveState writes the live signals' values to path. Main thread.
func SaveState(path string) error { return saveState(path) }

// Package devstate keeps the app's signals' values across asuka dev's
// reloads: read from a file when the new code starts, written to it when
// asuka dev stops the old. It is what a host that runs the app under asuka
// dev uses (internal/remote, the desktop hosts), and it needs no network,
// so a release build that links it carries none.
package devstate

import (
	"encoding/json"
	"os"

	"github.com/egoist/asuka/signal"
)

// Keep turns on keeping signals' values across reloads, with the values
// the last run saved in path (if any).
func Keep(path string) error {
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

// Save writes the live signals' values to path. Main thread.
func Save(path string) error {
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

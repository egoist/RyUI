package appdir

import (
	"path/filepath"
	"testing"
)

// Each platform's directories are named as the data directory is (the
// host named it), in the platform's places; the environment overrides
// each.
func TestDirs(t *testing.T) {
	defer func(g string) { goos = g }(goos)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	for _, k := range []string{"ASUKA_CONFIG", "ASUKA_CACHE", "ASUKA_LOGS"} {
		t.Setenv(k, "")
	}
	j := filepath.Join
	for _, c := range []struct {
		goos, data, config, cache, logs string
	}{
		{"darwin", j(home, "Library/Application Support/Notes"), j(home, "Library/Application Support/Notes"), "", j(home, "Library/Logs/Notes")},
		{"linux", j(home, ".local/share/com.example.notes"), j(home, ".config/com.example.notes"), j(home, ".cache/com.example.notes"), j(home, ".local/state/com.example.notes")},
		{"windows", `C:\Users\a\AppData\Local\Notes`, "", filepath.Join(`C:\Users\a\AppData\Local\Notes`, "Cache"), filepath.Join(`C:\Users\a\AppData\Local\Notes`, "Logs")},
		{"android", "/data/user/0/com.example.notes/files", "/data/user/0/com.example.notes/files", "/data/user/0/com.example.notes/cache", "/data/user/0/com.example.notes/files/logs"},
	} {
		goos = c.goos
		t.Setenv("ASUKA_DATA", filepath.FromSlash(c.data))
		t.Setenv("APPDATA", "")
		if c.goos == "windows" {
			// Roaming AppData, named as Local's folder.
			t.Setenv("APPDATA", `C:\Users\a\AppData\Roaming`)
			c.config = filepath.Join(`C:\Users\a\AppData\Roaming`, filepath.Base(c.data))
		}
		if got := Config(); got != filepath.FromSlash(c.config) {
			t.Errorf("%s: Config() = %q, want %q", c.goos, got, c.config)
		}
		if c.cache != "" {
			if got := Cache(); got != filepath.FromSlash(c.cache) {
				t.Errorf("%s: Cache() = %q, want %q", c.goos, got, c.cache)
			}
		}
		if got := Logs(); got != filepath.FromSlash(c.logs) {
			t.Errorf("%s: Logs() = %q, want %q", c.goos, got, c.logs)
		}
	}
	t.Setenv("ASUKA_CONFIG", "/work/config")
	t.Setenv("ASUKA_CACHE", "/work/cache")
	t.Setenv("ASUKA_LOGS", "/work/logs")
	if Config() != "/work/config" || Cache() != "/work/cache" || Logs() != "/work/logs" {
		t.Errorf("the environment's: %q %q %q", Config(), Cache(), Logs())
	}
}

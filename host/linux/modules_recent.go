//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/asuka/internal/fileurl"
	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/recent on Linux: the user's recently used files
// (recently-used.xbel in the XDG data directory, what GtkRecentManager
// keeps and the file chooser's and the file manager's Recent show), the
// app's under its name (g_set_application_name: GTK's own file chooser
// records what the user picks in it under the same). Written with GLib's
// GBookmarkFile, at once, so a list right after an add has it; the
// GtkRecentManagers of the user's apps (this one's too) watch the file.
// Clear takes the app off each file it recorded: one only it recorded
// goes, the user's other recent files stay.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["recent"] = h.recentModule()
	})
}

// recentFile is the user's recently used files' XBEL file (tests replace
// it).
var recentFile = func() string {
	return filepath.Join(GoString(GetUserDataDir()), "recently-used.xbel")
}

// recentMax is how many documents List returns at most.
const recentMax = 10

func (h *Host) recentModule() map[string]method {
	return map[string]method{
		"add": func(args []byte, reply func([]byte, error)) {
			reply(nil, h.recentAdd(string(args)))
		},
		"list": func(_ []byte, reply func([]byte, error)) {
			paths, err := h.recentList()
			if err != nil {
				reply(nil, err)
				return
			}
			b, err := json.Marshal(paths)
			reply(b, err)
		},
		"clear": func(_ []byte, reply func([]byte, error)) {
			reply(nil, h.recentClear())
		},
	}
}

// recentExec is the app's command in the recent files: its executable,
// quoted as a desktop file's Exec, then %u for the file's URI.
func recentExec() string {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return desktopExec([]string{exe}) + " %u"
}

func (h *Host) recentAdd(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("recent: an absolute path, please")
	}
	b, err := LoadBookmarks(recentFile())
	if err != nil {
		return err
	}
	defer b.Free()
	b.Add(fileurl.FromPath(path), ContentTypeMIME(filepath.Base(path)), h.appName, recentExec())
	return save(b, recentFile())
}

// save writes b to path, making its directory.
func save(b *Bookmarks, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return b.Save(path)
}

// recentList is the app's recent files that still exist, newest first.
func (h *Host) recentList() ([]string, error) {
	b, err := LoadBookmarks(recentFile())
	if err != nil {
		return nil, err
	}
	defer b.Free()
	type item struct {
		path  string
		stamp time.Time
	}
	var items []item
	for _, u := range b.URIs() {
		stamp, ok := b.Stamp(u, h.appName)
		if !ok {
			continue
		}
		p, local := fileurl.ToPath(u)
		if !local || !exists(p) {
			continue
		}
		items = append(items, item{p, stamp})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].stamp.After(items[j].stamp) })
	paths := []string{}
	for _, it := range items {
		if len(paths) == recentMax {
			break
		}
		paths = append(paths, it.path)
	}
	return paths, nil
}

func (h *Host) recentClear() error {
	path := recentFile()
	b, err := LoadBookmarks(path)
	if err != nil {
		return err
	}
	defer b.Free()
	changed := false
	for _, u := range b.URIs() {
		apps := b.Applications(u)
		mine := false
		for _, a := range apps {
			mine = mine || a == h.appName
		}
		if !mine {
			continue
		}
		changed = true
		if len(apps) == 1 {
			b.Remove(u)
		} else {
			b.RemoveApplication(u, h.appName)
		}
	}
	if !changed {
		return nil
	}
	return save(b, path)
}

// desktopExec is a command line as a desktop entry's Exec (and a recent
// file's exec) has it: each argument with a reserved character quoted
// ("...", with ", `, $ and \ escaped; % doubled). A desktop file's value
// escapes its \ again (desktopValue).
func desktopExec(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\n\"'\\><~|&;$*?#()`=%") {
			q[i] = a
			continue
		}
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range a {
			if r == '"' || r == '`' || r == '$' || r == '\\' {
				b.WriteByte('\\')
			}
			if r == '%' {
				b.WriteByte('%') // %% is a literal %
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
		q[i] = b.String()
	}
	return strings.Join(q, " ")
}

// desktopValue is s as a desktop file's string value: \ escaped, and
// line breaks.
func desktopValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s)
}

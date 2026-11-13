package windows

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/asuka/internal/filetype"
)

// The files an app opens, its second launches, its recent documents and
// its start at login, apart from the host (it builds anywhere, so it's
// tested anywhere; files_windows.go uses them).
//
// A launch's arguments (os.Args[1:]) are the files it opens, the URLs of
// its scheme and the app's own flags: the first launch passes its files
// and URLs on to the root (ui.OnOpenFiles, ui.OnOpenURL); a second one,
// while the app runs, hands its working directory and arguments to the
// first (WM_COPYDATA), which brings itself to the front, gives them to
// app.OnSecondInstance and passes their files and URLs on in turn.

// instancePayload is a second launch's WM_COPYDATA bytes: its working
// directory, then each argument, NUL-separated (an argument has no NUL).
func instancePayload(dir string, args []string) []byte {
	return []byte(strings.Join(append([]string{dir}, args...), "\x00"))
}

// parseInstancePayload is instancePayload's directory and arguments.
func parseInstancePayload(b []byte) (dir string, args []string) {
	parts := strings.Split(string(b), "\x00")
	return parts[0], parts[1:]
}

// fileTypeKey is a registry key the app sets for its file types: a value
// ("" names the key's default), and whether it's set only when the key
// has none (an extension's default program: the app's own types, where
// the user or another app hasn't chosen one).
type fileTypeKey struct {
	key, name, value string
	ifUnset          bool
}

// fileTypeValues are the values, under HKEY_CURRENT_USER, that make exe
// open types: a ProgID of each (its name, icon and command), listed among
// each extension's Open With programs, and the extensions the app
// supports (Applications\<exe>\SupportedTypes, which Open With and the
// jump list's Recent go by); an own type's extensions open with it by
// default unless they have a default already.
func fileTypeValues(appID, exe string, types []filetype.Type) []fileTypeKey {
	var out []fileTypeKey
	base := filepath.Base(strings.ReplaceAll(exe, `\`, "/"))
	for _, t := range types {
		if len(t.Extensions) == 0 {
			continue
		}
		prog := filetype.ProgID(appID, t)
		key := `Software\Classes\` + prog
		icon := `"` + exe + `",0`
		if t.Icon != 0 {
			icon = `"` + exe + `",-` + strconv.Itoa(t.Icon)
		}
		out = append(out,
			fileTypeKey{key: key, value: t.Name},
			fileTypeKey{key: key + `\DefaultIcon`, value: icon},
			fileTypeKey{key: key + `\shell\open\command`, value: `"` + exe + `" "%1"`},
		)
		for _, e := range t.Extensions {
			ext := "." + strings.ToLower(strings.TrimPrefix(e, "."))
			out = append(out, fileTypeKey{key: `Software\Classes\` + ext + `\OpenWithProgids`, name: prog})
			if t.Own {
				out = append(out, fileTypeKey{key: `Software\Classes\` + ext, value: prog, ifUnset: true})
			}
			out = append(out, fileTypeKey{key: `Software\Classes\Applications\` + base + `\SupportedTypes`, name: ext})
		}
	}
	return out
}

// maxRecent is how many recent documents the app keeps (macOS's Open
// Recent's default).
const maxRecent = 10

// addRecent is list with path first (moved up if it's there: Windows
// paths match whatever their case), at most maxRecent long.
func addRecent(list []string, path string) []string {
	out := []string{path}
	for _, p := range list {
		if !strings.EqualFold(p, path) && len(out) < maxRecent {
			out = append(out, p)
		}
	}
	return out
}

// runKey is the user's Run key: its values start their programs at login.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// startupApprovedKey is where Task Manager's Startup apps (and Settings'
// Startup) keep the user's choice of each Run value: a binary value whose
// first byte is odd while it's turned off (0x03; 0x02 or 0x06 on).
const startupApprovedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

// loginCommand is the Run value that starts exe at login, so that it
// knows (boot.LoginArg).
func loginCommand(exe, arg string) string { return `"` + exe + `" ` + arg }

// loginNamesExe reports whether a Run value's command starts exe.
func loginNamesExe(cmd, exe string) bool {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.IndexByte(cmd[1:], '"'); i >= 0 {
			return strings.EqualFold(cmd[1:1+i], exe)
		}
		return false
	}
	f := strings.Fields(cmd)
	return len(f) > 0 && strings.EqualFold(f[0], exe)
}

// startupDisabled reports whether a StartupApproved value turns its Run
// value off.
func startupDisabled(b []byte) bool { return len(b) > 0 && b[0]&1 == 1 }

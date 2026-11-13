//go:build linux && !android

package linux

import (
	"fmt"
	"os/exec"
	"strings"
)

// xdotoolKey types a key into the focused window through xdotool (X11:
// Xvfb in tests and automation runs), for the shortcuts a window's
// controllers take (F5, Alt+Left) rather than the app's menus.
func xdotoolKey(shortcut string) error {
	path, err := exec.LookPath("xdotool")
	if err != nil {
		return fmt.Errorf("%s: not an app shortcut, and xdotool isn't installed to press it", shortcut)
	}
	names := map[string]string{"enter": "Return", "escape": "Escape", "esc": "Escape", "tab": "Tab", "space": "space",
		"delete": "Delete", "backspace": "BackSpace", "left": "Left", "right": "Right", "up": "Up", "down": "Down",
		"home": "Home", "end": "End", "pageup": "Prior", "pagedown": "Next"}
	parts := strings.Split(shortcut, "+")
	for i, p := range parts {
		if n, ok := names[strings.ToLower(p)]; ok {
			parts[i] = n
		} else if strings.EqualFold(p, "cmd") || strings.EqualFold(p, "primary") {
			parts[i] = "ctrl"
		} else if len(p) > 1 && (p[0] == 'f' || p[0] == 'F') {
			parts[i] = strings.ToUpper(p)
		}
	}
	// Asynchronously: the key comes back through the main loop this step
	// runs on.
	return exec.Command(path, "key", "--clearmodifiers", strings.Join(parts, "+")).Start()
}

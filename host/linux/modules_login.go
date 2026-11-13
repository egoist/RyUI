//go:build linux && !android

package linux

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/internal/appdir"
	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/login on Linux: an XDG autostart entry of the user's
// ($XDG_CONFIG_HOME/autostart/<id>.desktop), which GNOME's and KDE's
// sessions start as the user logs in, and GNOME Tweaks and KDE's
// settings list. In a Flatpak the sandbox can't write it: the Background
// portal asks the user and writes it (RequestBackground with autostart),
// and the app keeps the answer, which it can't read back. The entry
// starts the app with boot.LoginArg, which the host takes out of its
// arguments (login.OpenedAtLogin).

func init() {
	boot.TakeLoginArg()
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["login"] = h.loginModule()
	})
}

// inFlatpak reports whether the app runs in a Flatpak sandbox (tests
// replace it).
var inFlatpak = func() bool {
	if os.Getenv("FLATPAK_ID") != "" {
		return true
	}
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

func (h *Host) loginModule() map[string]method {
	return map[string]method{
		"set": func(args []byte, reply func([]byte, error)) {
			on := len(args) > 0 && args[0] == 1
			if inFlatpak() {
				h.loginPortal(on, func(err error) { reply(nil, err) })
				return
			}
			reply(nil, h.setAutostart(on))
		},
		"enabled": func(_ []byte, reply func([]byte, error)) {
			on := false
			if inFlatpak() {
				b, _ := os.ReadFile(loginGrantFile())
				on = strings.TrimSpace(string(b)) == "1"
			} else {
				b, err := os.ReadFile(h.autostartFile())
				on = err == nil && autostartEnabled(string(b))
			}
			reply([]byte{boolByte(on)}, nil)
		},
		"openedAtLogin": func(_ []byte, reply func([]byte, error)) {
			reply([]byte{boolByte(boot.OpenedAtLogin)}, nil)
		},
	}
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// autostartFile is the app's XDG autostart entry.
func (h *Host) autostartFile() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" || !filepath.IsAbs(dir) {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", h.appID+".desktop")
}

// loginCommand is the command the session starts the app with.
func loginCommand() []string {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return []string{exe, boot.LoginArg}
}

// autostartEntry is the app's autostart desktop entry.
func autostartEntry(name, id string, command []string) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\nType=Application\n")
	b.WriteString("Name=" + desktopValue(name) + "\n")
	b.WriteString("Exec=" + desktopValue(desktopExec(command)) + "\n")
	b.WriteString("Icon=" + desktopValue(id) + "\n")
	b.WriteString("Terminal=false\nNoDisplay=true\n")
	b.WriteString("X-GNOME-Autostart-enabled=true\n")
	return b.String()
}

// autostartEnabled reports whether an autostart entry starts the app:
// neither hidden nor turned off (GNOME's key, which its settings write).
func autostartEnabled(entry string) bool {
	group := ""
	for _, line := range strings.Split(entry, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			group = line
			continue
		}
		if group != "[Desktop Entry]" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Hidden":
			if strings.TrimSpace(v) == "true" {
				return false
			}
		case "X-GNOME-Autostart-enabled":
			if strings.TrimSpace(v) == "false" {
				return false
			}
		}
	}
	return true
}

func (h *Host) setAutostart(on bool) error {
	path := h.autostartFile()
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(autostartEntry(h.appName, h.appID, loginCommand())), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loginGrantFile keeps, in a Flatpak, what the Background portal last
// granted ("1": the app starts at login).
func loginGrantFile() string { return filepath.Join(appdir.Data(), "login-autostart") }

// loginPortal asks the Background portal to start the app at login, or
// no longer; the user may refuse.
func (h *Host) loginPortal(on bool, done func(error)) {
	h.portalRequest("org.freedesktop.portal.Background", "RequestBackground", func(token string) Ptr {
		return VTuple(VStr(h.parentWindow()), VDict(map[string]Ptr{
			"handle_token": VStr(token),
			"reason":       VStr("Open " + h.appName + " when you log in"),
			"autostart":    VBool(on),
			"commandline":  VStrings(loginCommand()),
		}))
	}, nil, func(code uint32, results map[string]any, err error) {
		switch {
		case err != nil:
			done(err)
			return
		case code == 1:
			done(errCancelled)
			return
		case code != 0:
			done(errors.New("login: the Background portal refused"))
			return
		}
		granted, _ := results["autostart"].(bool)
		if on && !granted {
			done(errors.New("login: the user didn't allow the app to start at login"))
			return
		}
		v := "0"
		if on {
			v = "1"
		}
		if err := os.MkdirAll(filepath.Dir(loginGrantFile()), 0o700); err == nil {
			os.WriteFile(loginGrantFile(), []byte(v), 0o600)
		}
		done(nil)
	})
}

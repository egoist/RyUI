//go:build linux && !android

package linux

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Languages: the environment's Languages are the app's own language
// (SaveLocale's, kept in its data directory) and then the user's
// (LANGUAGE, LANG). The Locale command says which the app shows and its
// direction: GTK lays its own widgets out and aligns text right to left
// with the default direction, and each view takes its node's.

func (h *Host) initLocale() {
	h.localePath = filepath.Join(dataDir(), "ryui-locale")
	if b, err := os.ReadFile(h.localePath); err == nil {
		h.savedLocale = strings.TrimSpace(string(b))
	}
	h.languages = joinLanguages(h.savedLocale, strings.Split(userLanguages(), ","))
}

// joinLanguages is the environment's Languages: the app's own language
// first, then the user's without it.
func joinLanguages(own string, user []string) string {
	if own == "" {
		return strings.Join(user, ",")
	}
	list := []string{own}
	for _, t := range user {
		if !strings.EqualFold(t, own) {
			list = append(list, t)
		}
	}
	return strings.Join(list, ",")
}

func (h *Host) setLocale(args []byte) {
	if len(args) < 1 {
		return
	}
	rtl, tag := args[0]&1 != 0, string(args[1:])
	h.locale = tag
	if rtl == h.rtl {
		return
	}
	h.rtl = rtl
	if rtl {
		WidgetSetDefaultDirection(TextDirRTL)
	} else {
		WidgetSetDefaultDirection(TextDirLTR)
	}
	for _, n := range h.nodes {
		if n.dir == ops.LayoutDirectionInherit {
			h.applyDirection(n)
		}
	}
	h.queueChrome()
}

func (h *Host) saveLocale(tag string) {
	if tag == h.savedLocale {
		return
	}
	h.savedLocale = tag
	if tag == "" {
		os.Remove(h.localePath)
	} else {
		os.MkdirAll(filepath.Dir(h.localePath), 0o755)
		os.WriteFile(h.localePath, []byte(tag), 0o644)
	}
	h.languages = joinLanguages(tag, strings.Split(userLanguages(), ","))
	h.envChanged()
}

// applyDirection sets the way a node reads, from its LayoutDirection or
// the app's, on its widget and its control (frames stay physical: the
// core places the children).
func (h *Host) applyDirection(n *node) {
	rtl := h.rtl
	switch n.dir {
	case ops.LayoutDirectionLeftToRight:
		rtl = false
	case ops.LayoutDirectionRightToLeft:
		rtl = true
	}
	n.rtl = rtl
	d := int32(TextDirLTR)
	if rtl {
		d = TextDirRTL
	}
	if n.inner != 0 {
		WidgetSetDirection(n.inner, d)
	}
	if n.kind == ops.KindText || n.kind == ops.KindTextField || n.kind == ops.KindRichText {
		h.applyText(n)
	}
}

// dataDir is the app's data directory: RYUI_DATA, which init sets to the
// XDG data directory's (Flatpak's own) folder for the app.
func dataDir() string {
	if d := os.Getenv("RYUI_DATA"); d != "" {
		return d
	}
	return filepath.Join(xdgDataHome(), "ryui")
}

func xdgDataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

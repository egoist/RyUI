//go:build linux && !android

package linux

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
)

// Symbols are SF Symbol names. Those with a match in the freedesktop and
// Adwaita icon names are the icon theme's symbolic icons, so an app
// looks as GNOME's do; the rest are Material Symbols (as Android and the
// web draw them), written once as symbolic SVGs into an icon directory
// the theme searches. Either way the name is an icon name GTK draws and
// recolors (buttons, menus, tabs, symbols).

// adwaitaIcons maps SF Symbol names to icon names.
var adwaitaIcons = map[string]string{
	"house": "go-home-symbolic", "house.fill": "go-home-symbolic", "house.circle": "go-home-symbolic",
	"magnifyingglass": "system-search-symbolic",
	"gearshape":       "emblem-system-symbolic", "gearshape.fill": "emblem-system-symbolic", "gear": "emblem-system-symbolic",
	"person": "avatar-default-symbolic", "person.fill": "avatar-default-symbolic",
	"person.crop.circle": "avatar-default-symbolic", "person.crop.circle.fill": "avatar-default-symbolic",
	"person.circle": "avatar-default-symbolic",
	"person.2":      "system-users-symbolic", "person.2.fill": "system-users-symbolic",
	"star": "non-starred-symbolic", "star.fill": "starred-symbolic",
	"heart": "emblem-favorite-symbolic", "heart.fill": "emblem-favorite-symbolic",
	"list.bullet":       "view-list-bullet-symbolic",
	"line.3.horizontal": "open-menu-symbolic",
	"ellipsis":          "view-more-symbolic", "ellipsis.circle": "view-more-symbolic",
	"plus": "list-add-symbolic", "plus.circle": "list-add-symbolic", "plus.circle.fill": "list-add-symbolic",
	"minus": "list-remove-symbolic", "minus.circle": "list-remove-symbolic",
	"xmark": "window-close-symbolic", "xmark.circle.fill": "edit-clear-symbolic", "xmark.circle": "edit-clear-symbolic",
	"checkmark":        "object-select-symbolic",
	"checkmark.circle": "emblem-ok-symbolic", "checkmark.circle.fill": "emblem-ok-symbolic",
	"chevron.left": "go-previous-symbolic", "chevron.right": "go-next-symbolic",
	"chevron.up": "go-up-symbolic", "chevron.down": "go-down-symbolic",
	"chevron.backward": "go-previous-symbolic", "chevron.forward": "go-next-symbolic",
	"arrow.left": "go-previous-symbolic", "arrow.right": "go-next-symbolic",
	"arrow.backward": "go-previous-symbolic", "arrow.forward": "go-next-symbolic",
	"arrow.up": "go-up-symbolic", "arrow.down": "go-down-symbolic",
	"arrow.clockwise": "view-refresh-symbolic", "arrow.triangle.2.circlepath": "view-refresh-symbolic",
	"arrow.counterclockwise": "object-rotate-left-symbolic",
	"arrow.uturn.backward":   "edit-undo-symbolic", "arrow.uturn.forward": "edit-redo-symbolic",
	"trash": "user-trash-symbolic", "trash.fill": "user-trash-symbolic",
	"pencil": "document-edit-symbolic", "square.and.pencil": "document-edit-symbolic",
	"square.and.arrow.up": "emblem-shared-symbolic", "square.and.arrow.up.fill": "emblem-shared-symbolic",
	"square.and.arrow.down": "document-save-symbolic",
	"paperplane":            "mail-send-symbolic", "paperplane.fill": "mail-send-symbolic",
	"bell": "preferences-system-notifications-symbolic", "bell.fill": "preferences-system-notifications-symbolic",
	"envelope": "mail-unread-symbolic", "envelope.fill": "mail-unread-symbolic",
	"phone": "call-start-symbolic", "phone.fill": "call-start-symbolic",
	"camera": "camera-photo-symbolic", "camera.fill": "camera-photo-symbolic",
	"photo": "image-x-generic-symbolic", "photo.fill": "image-x-generic-symbolic",
	"photo.on.rectangle": "image-x-generic-symbolic",
	"video":              "camera-video-symbolic", "video.fill": "camera-video-symbolic",
	"film": "video-x-generic-symbolic",
	"play": "media-playback-start-symbolic", "play.fill": "media-playback-start-symbolic",
	"pause": "media-playback-pause-symbolic", "pause.fill": "media-playback-pause-symbolic",
	"stop": "media-playback-stop-symbolic", "stop.fill": "media-playback-stop-symbolic",
	"forward.fill": "media-seek-forward-symbolic", "backward.fill": "media-seek-backward-symbolic",
	"speaker.wave.2": "audio-volume-high-symbolic", "speaker.wave.2.fill": "audio-volume-high-symbolic",
	"speaker.slash": "audio-volume-muted-symbolic", "speaker.slash.fill": "audio-volume-muted-symbolic",
	"mic": "audio-input-microphone-symbolic", "mic.fill": "audio-input-microphone-symbolic",
	"lock": "changes-prevent-symbolic", "lock.fill": "changes-prevent-symbolic",
	"lock.open": "changes-allow-symbolic",
	"location":  "find-location-symbolic", "location.fill": "find-location-symbolic",
	"mappin": "mark-location-symbolic",
	"globe":  "web-browser-symbolic", "safari": "web-browser-symbolic",
	"link": "insert-link-symbolic",
	"doc":  "text-x-generic-symbolic", "doc.fill": "text-x-generic-symbolic", "doc.text": "text-x-generic-symbolic",
	"doc.on.doc": "edit-copy-symbolic", "doc.on.clipboard": "edit-paste-symbolic",
	"folder": "folder-symbolic", "folder.fill": "folder-symbolic",
	"archivebox": "package-x-generic-symbolic",
	"calendar":   "x-office-calendar-symbolic",
	"clock":      "preferences-system-time-symbolic", "clock.fill": "preferences-system-time-symbolic",
	"bookmark": "user-bookmarks-symbolic", "bookmark.fill": "user-bookmarks-symbolic",
	"info.circle": "dialog-information-symbolic", "info.circle.fill": "dialog-information-symbolic",
	"questionmark.circle":      "dialog-question-symbolic",
	"exclamationmark.triangle": "dialog-warning-symbolic", "exclamationmark.triangle.fill": "dialog-warning-symbolic",
	"exclamationmark.circle": "dialog-error-symbolic",
	"eye":                    "view-reveal-symbolic", "eye.slash": "view-conceal-symbolic",
	"wifi":    "network-wireless-symbolic",
	"sun.max": "display-brightness-symbolic", "moon": "weather-clear-night-symbolic", "moon.fill": "weather-clear-night-symbolic",
	"cloud":           "weather-overcast-symbolic",
	"square.grid.2x2": "view-grid-symbolic", "rectangle.grid.2x2": "view-grid-symbolic",
	"keyboard":    "input-keyboard-symbolic",
	"printer":     "printer-symbolic",
	"music.note":  "audio-x-generic-symbolic",
	"headphones":  "audio-headphones-symbolic",
	"battery.100": "battery-full-symbolic",
	"shield":      "security-high-symbolic",
	"key":         "dialog-password-symbolic",
	"scissors":    "edit-cut-symbolic",
	"bold":        "format-text-bold-symbolic", "italic": "format-text-italic-symbolic",
	"underline": "format-text-underline-symbolic", "strikethrough": "format-text-strikethrough-symbolic",
	"arrowshape.turn.up.left": "mail-reply-sender-symbolic", "arrowshape.turn.up.left.2": "mail-reply-all-symbolic",
	"paperclip":    "mail-attachment-symbolic",
	"sidebar.left": "sidebar-show-symbolic",
	"tray":         "mail-inbox-symbolic", "tray.fill": "mail-inbox-symbolic",
	"iphone": "phone-symbolic", "gearshape.2": "applications-system-symbolic",
	"wand.and.stars": "applications-graphics-symbolic", "sparkles": "applications-graphics-symbolic",
	"laptopcomputer": "computer-symbolic", "desktopcomputer": "computer-symbolic",
	"wrench": "applications-engineering-symbolic", "hammer": "applications-engineering-symbolic",
}

var (
	iconTheme Ptr
	iconDir   string
	iconNames = map[string]string{} // SF name -> icon name, as resolved
)

// iconName is the icon name GTK draws an SF Symbol name with ("" for
// none). A name that's already an icon name (adwaita's "...-symbolic")
// passes through.
func (h *Host) iconName(sf string) string {
	if sf == "" {
		return ""
	}
	if name, ok := iconNames[sf]; ok {
		return name
	}
	name := h.resolveIcon(sf)
	iconNames[sf] = name
	return name
}

func (h *Host) resolveIcon(sf string) string {
	if iconTheme == 0 {
		iconTheme = IconThemeGetForDisplay(DisplayGetDefault())
	}
	if strings.HasSuffix(sf, "-symbolic") {
		return sf
	}
	if name := adwaitaIcons[sf]; name != "" && IconThemeHasIcon(iconTheme, name) {
		return name
	}
	if i, ok := materialSymbols[sf]; ok {
		if name := writeIcon(sf, materialPaths[i]); name != "" {
			return name
		}
	}
	if name := adwaitaIcons[sf]; name != "" {
		return name // GTK shows its missing-image icon
	}
	println("asuka: no symbol named", sf, "on Linux")
	return ""
}

// writeIcon writes a Material Symbol as a symbolic SVG icon the theme
// finds, and returns its name.
func writeIcon(sf, path string) string {
	if iconDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		iconDir = filepath.Join(base, "asuka", "icons")
		if err := os.MkdirAll(iconDir, 0o755); err != nil {
			return ""
		}
		IconThemeAddSearchPath(iconTheme, iconDir)
	}
	name := "asuka-" + strings.ReplaceAll(sf, ".", "-") + "-symbolic"
	file := filepath.Join(iconDir, name+".svg")
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 -960 960 960"><path fill="#000" d="` + path + `"/></svg>`
	if old, err := os.ReadFile(file); err != nil || string(old) != svg {
		if err := os.WriteFile(file, []byte(svg), 0o644); err != nil {
			return ""
		}
	}
	return name
}

// drawSymbol draws a Symbol node's icon at its size, in its color.
func (h *Host) drawSymbol(n *node, snap Ptr, bounds Rect) {
	if n.symbol == "" || n.icon == "" {
		return
	}
	size, _ := h.font(n)
	scale := int32(1)
	if n.widget != 0 {
		scale = WidgetGetScaleFactor(n.widget)
	}
	p := iconPaintable(n.icon, int32(size+0.5), scale, n.rtl)
	if p == 0 {
		return
	}
	defer ObjectUnref(p)
	c, ok := h.resolve(n.color)
	if !ok || n.color == 0 {
		c = h.theme.fg
	}
	colors := [4]RGBA{c, h.theme.red, h.theme.yellow, h.theme.green}
	x, y := (bounds.W-size)/2, (bounds.H-size)/2
	pt := Point{X: x, Y: y}
	Call(FnSnapshotSave, snap)
	Call(FnSnapshotTranslate, snap, Addr(&pt))
	SymbolicPaintableSnapshotSymbolic(p, snap, float64(size), float64(size), &colors[0], 4)
	Call(FnSnapshotRestore, snap)
}

// iconPaintable looks an icon up in the theme at a size.
func iconPaintable(name string, size, scale int32, rtl bool) Ptr {
	if iconTheme == 0 {
		iconTheme = IconThemeGetForDisplay(DisplayGetDefault())
	}
	dir := int32(TextDirLTR)
	if rtl {
		dir = TextDirRTL
	}
	return IconThemeLookupIcon(iconTheme, name, nil, size, scale, dir, 2) // force symbolic
}

//go:build ignore

// gen_symbols writes the Android host's symbols: Material Symbols
// (Apache 2.0, github.com/google/material-design-icons) as vector
// drawables, for the SF Symbol names ui.Symbol and ui.Icon take, and the
// table from those names to the drawables (Symbols.kt).
//
//	go run hosts/android/gen_symbols.go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// symbols maps SF Symbol names to Material Symbols names. A name's
// ".fill" variant, when SF Symbols has one, is the Material symbol filled.
var symbols = map[string]string{
	"house":                              "home",
	"magnifyingglass":                    "search",
	"gearshape":                          "settings",
	"person":                             "person",
	"person.circle":                      "account_circle",
	"person.2":                           "group",
	"star":                               "star",
	"heart":                              "favorite",
	"plus":                               "add",
	"minus":                              "remove",
	"xmark":                              "close",
	"checkmark":                          "check",
	"checkmark.circle":                   "check_circle",
	"xmark.circle":                       "cancel",
	"plus.circle":                        "add_circle",
	"chevron.left":                       "chevron_left",
	"chevron.right":                      "chevron_right",
	"chevron.up":                         "expand_less",
	"chevron.down":                       "expand_more",
	"arrow.left":                         "arrow_back",
	"arrow.right":                        "arrow_forward",
	"arrow.up":                           "arrow_upward",
	"arrow.down":                         "arrow_downward",
	"arrow.clockwise":                    "refresh",
	"arrow.uturn.backward":               "undo",
	"arrow.uturn.forward":                "redo",
	"list.bullet":                        "list",
	"line.3.horizontal":                  "menu",
	"line.3.horizontal.decrease":         "filter_list",
	"slider.horizontal.3":                "tune",
	"ellipsis":                           "more_horiz",
	"ellipsis.circle":                    "more_horiz",
	"trash":                              "delete",
	"pencil":                             "edit",
	"square.and.pencil":                  "edit_square",
	"square.and.arrow.up":                "share",
	"square.and.arrow.down":              "download",
	"paperplane":                         "send",
	"bell":                               "notifications",
	"envelope":                           "mail",
	"phone":                              "call",
	"message":                            "chat_bubble",
	"bubble.left":                        "chat_bubble",
	"camera":                             "photo_camera",
	"photo":                              "image",
	"video":                              "videocam",
	"mic":                                "mic",
	"play":                               "play_arrow",
	"pause":                              "pause",
	"stop":                               "stop",
	"forward":                            "fast_forward",
	"backward":                           "fast_rewind",
	"speaker.wave.2":                     "volume_up",
	"speaker.slash":                      "volume_off",
	"calendar":                           "calendar_today",
	"clock":                              "schedule",
	"location":                           "near_me",
	"mappin":                             "location_on",
	"map":                                "map",
	"cart":                               "shopping_cart",
	"bag":                                "shopping_bag",
	"creditcard":                         "credit_card",
	"bookmark":                           "bookmark",
	"flag":                               "flag",
	"tag":                                "sell",
	"lock":                               "lock",
	"lock.open":                          "lock_open",
	"key":                                "key",
	"eye":                                "visibility",
	"eye.slash":                          "visibility_off",
	"info.circle":                        "info",
	"exclamationmark.triangle":           "warning",
	"exclamationmark.circle":             "error",
	"questionmark.circle":                "help",
	"bolt":                               "bolt",
	"cloud":                              "cloud",
	"sun.max":                            "light_mode",
	"moon":                               "dark_mode",
	"folder":                             "folder",
	"doc":                                "description",
	"doc.on.doc":                         "content_copy",
	"link":                               "link",
	"paperclip":                          "attach_file",
	"globe":                              "language",
	"wifi":                               "wifi",
	"house.circle":                       "home",
	"square.grid.2x2":                    "grid_view",
	"chart.bar":                          "bar_chart",
	"bubble.left.and.bubble.right":       "forum",
	"hand.thumbsup":                      "thumb_up",
	"hand.thumbsdown":                    "thumb_down",
	"gift":                               "redeem",
	"sparkles":                           "auto_awesome",
	"rectangle.portrait.and.arrow.right": "logout",
	"person.badge.plus":                  "person_add",
	"qrcode":                             "qr_code",
	"printer":                            "print",
	"tray":                               "inbox",
	"archivebox":                         "archive",
	"pin":                                "keep",
	"house.and.flag":                     "cottage",
}

// fills are the SF Symbols with a ".fill" variant here.
var fills = map[string]bool{
	"house": true, "gearshape": true, "person": true, "person.circle": true, "person.2": true,
	"star": true, "heart": true, "checkmark.circle": true, "xmark.circle": true, "plus.circle": true,
	"trash": true, "pencil": false, "paperplane": true, "bell": true, "envelope": true, "phone": true,
	"message": true, "bubble.left": true, "camera": true, "video": true, "mic": true, "play": true,
	"pause": true, "stop": true, "forward": true, "backward": true, "speaker.wave.2": true,
	"speaker.slash": true, "location": true, "mappin": true, "map": true, "cart": true, "bag": true,
	"creditcard": true, "bookmark": true, "flag": true, "tag": true, "lock": true, "lock.open": true,
	"key": true, "eye": true, "eye.slash": true, "info.circle": true, "exclamationmark.triangle": true,
	"exclamationmark.circle": true, "questionmark.circle": true, "bolt": true, "cloud": true,
	"sun.max": true, "moon": true, "folder": true, "doc": true, "doc.on.doc": true, "globe": false,
	"square.grid.2x2": true, "chart.bar": true, "bubble.left.and.bubble.right": true,
	"hand.thumbsup": true, "hand.thumbsdown": true, "gift": true, "tray": true, "archivebox": true,
	"pin": true, "ellipsis.circle": true,
}

// labels name the symbols for TalkBack where the Material symbol's name
// doesn't (an icon-only button's label when the app gives none); the
// others are their Material name in words.
var labels = map[string]string{
	"magnifyingglass": "Search", "gearshape": "Settings", "xmark": "Close", "plus": "Add",
	"minus": "Remove", "checkmark": "Done", "chevron.left": "Back", "chevron.right": "Forward",
	"chevron.up": "Collapse", "chevron.down": "Expand", "arrow.left": "Back", "arrow.clockwise": "Refresh",
	"ellipsis": "More", "ellipsis.circle": "More", "trash": "Delete", "pencil": "Edit",
	"square.and.pencil": "Compose", "square.and.arrow.up": "Share", "square.and.arrow.down": "Download",
	"paperplane": "Send", "bell": "Notifications", "envelope": "Mail", "phone": "Call",
	"message": "Message", "bubble.left": "Message", "camera": "Camera", "photo": "Photo",
	"play": "Play", "pause": "Pause", "heart": "Favorite", "star": "Star", "house": "Home",
	"person": "Profile", "person.circle": "Account", "line.3.horizontal": "Menu",
	"line.3.horizontal.decrease": "Filter", "slider.horizontal.3": "Adjust", "location": "Location",
	"mappin": "Location", "cart": "Cart", "bag": "Bag", "doc.on.doc": "Copy", "link": "Link",
	"paperclip": "Attach", "globe": "Language", "sun.max": "Light mode", "moon": "Dark mode",
	"info.circle": "Info", "questionmark.circle": "Help", "exclamationmark.triangle": "Warning",
	"rectangle.portrait.and.arrow.right": "Sign out", "person.badge.plus": "Add person",
}

// label is the TalkBack label of an SF Symbol name.
func label(sf, material string) string {
	if l, ok := labels[strings.TrimSuffix(sf, ".fill")]; ok {
		return l
	}
	w := strings.ReplaceAll(material, "_", " ")
	return strings.ToUpper(w[:1]) + w[1:]
}

var pathRe = regexp.MustCompile(`<path d="([^"]+)"`)

func fetch(name string, fill bool) (string, error) {
	file := name + "_24px.svg"
	if fill {
		file = name + "_fill1_24px.svg"
	}
	url := "https://raw.githubusercontent.com/google/material-design-icons/master/symbols/web/" + name + "/materialsymbolsoutlined/" + file
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var paths []string
	for _, m := range pathRe.FindAllStringSubmatch(string(b), -1) {
		paths = append(paths, m[1])
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("%s: no path", url)
	}
	return strings.Join(paths, " "), nil
}

// drawable is a vector drawable of Material's 960-unit viewbox, whose y
// runs from -960 to 0.
func drawable(material string, fill bool, d string) string {
	variant := "outlined"
	if fill {
		variant = "filled"
	}
	return fmt.Sprintf(`<!-- Generated by gen_symbols.go: Material Symbols %q, %s (Apache 2.0). -->
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="24dp" android:height="24dp"
    android:viewportWidth="960" android:viewportHeight="960">
    <group android:translateY="960">
        <path android:fillColor="#FF000000" android:pathData="%s" />
    </group>
</vector>
`, material, variant, d)
}

func main() {
	res := filepath.Join("hosts", "android", "nui", "src", "main", "res", "drawable")
	old, _ := filepath.Glob(filepath.Join(res, "nui_sym_*.xml"))
	for _, f := range old {
		os.Remove(f)
	}
	type entry struct{ sf, drawable, label string }
	var table []entry
	written := map[string]bool{}
	write := func(material string, fill bool) (string, error) {
		name := "nui_sym_" + material
		if fill {
			name += "_fill"
		}
		if written[name] {
			return name, nil
		}
		d, err := fetch(material, fill)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(res, name+".xml"), []byte(drawable(material, fill, d)), 0o644); err != nil {
			return "", err
		}
		written[name] = true
		return name, nil
	}
	var failed []string
	for sf, m := range symbols {
		name, err := write(m, false)
		if err != nil {
			failed = append(failed, err.Error())
			continue
		}
		table = append(table, entry{sf, name, label(sf, m)})
		if fills[sf] {
			name, err := write(m, true)
			if err != nil {
				failed = append(failed, err.Error())
				continue
			}
			table = append(table, entry{sf + ".fill", name, label(sf, m)})
		}
	}
	sort.Slice(table, func(i, j int) bool { return table[i].sf < table[j].sf })
	var b strings.Builder
	b.WriteString("// Code generated by gen_symbols.go; DO NOT EDIT.\n\npackage dev.nui\n\n")
	b.WriteString("/** The symbols the host draws, by SF Symbol name: Material Symbols (Apache 2.0). */\n")
	b.WriteString("internal val symbolDrawables = mapOf(\n")
	for _, e := range table {
		fmt.Fprintf(&b, "    %q to R.drawable.%s,\n", e.sf, e.drawable)
	}
	b.WriteString(")\n\n")
	b.WriteString("/** The symbols' TalkBack labels, by SF Symbol name (English). */\n")
	b.WriteString("internal val symbolLabels = mapOf(\n")
	for _, e := range table {
		fmt.Fprintf(&b, "    %q to %q,\n", e.sf, e.label)
	}
	b.WriteString(")\n")
	kt := filepath.Join("hosts", "android", "nui", "src", "main", "java", "dev", "nui", "Symbols.kt")
	if err := os.WriteFile(kt, []byte(b.String()), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("%d symbols, %d drawables\n", len(table), len(written))
	for _, f := range failed {
		fmt.Println("failed:", f)
	}
}

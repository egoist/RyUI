//go:build linux && !android

package linux

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
)

// The desktop entry spec's values and the user's languages' forms.
func TestAppsDesktopEntry(t *testing.T) {
	e, ok := parseDesktopEntry([]byte(`# a comment
[Desktop Entry]
Type=Application
Name=Text Editor
Name[de]=Texteditor
Name[de_CH]=Texteditor (CH)
Name[sr@latin]=Uređivač
Exec=gnome-text-editor %U
Icon=org.gnome.TextEditor
OnlyShowIn=GNOME;Unity;
NotShowIn=KDE\;X;
NoDisplay=false

[Desktop Action new-window]
Name=New Window
Exec=gnome-text-editor --new-window
`))
	if !ok || e.typ != "Application" || e.icon != "org.gnome.TextEditor" || e.names[""] != "Text Editor" || e.noDisplay ||
		!slices.Equal(e.onlyShowIn, []string{"GNOME", "Unity"}) || !slices.Equal(e.notShowIn, []string{"KDE;X"}) {
		t.Fatalf("%+v", e)
	}
	if _, ok := parseDesktopEntry([]byte("[Other]\nName=x\n")); ok {
		t.Fatal("a file without a Desktop Entry group")
	}
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LANG": "de_DE.UTF-8"}, "Texteditor"},
		{map[string]string{"LANG": "de_CH.UTF-8"}, "Texteditor (CH)"},
		{map[string]string{"LANG": "sr_RS.UTF-8@latin"}, "Uređivač"},
		{map[string]string{"LANG": "fr_FR.UTF-8"}, "Text Editor"},
		{map[string]string{"LANG": "en_US.UTF-8", "LANGUAGE": "fr:de"}, "Texteditor"},
		{map[string]string{"LC_ALL": "de_DE.UTF-8", "LANG": "fr_FR.UTF-8"}, "Texteditor"},
		{map[string]string{"LC_MESSAGES": "de_DE.UTF-8", "LANG": "fr_FR.UTF-8"}, "Texteditor"},
		{map[string]string{"LANG": "C", "LANGUAGE": "de"}, "Text Editor"}, // gettext: LANGUAGE isn't read in the C locale
	} {
		for _, v := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
			t.Setenv(v, c.env[v])
		}
		if got := e.localized(languageNames()); got != c.want {
			t.Errorf("%v: %q, want %q (%q)", c.env, got, c.want, languageNames())
		}
	}
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "sr_RS.UTF-8@latin")
	if got, want := languageNames(), []string{"sr_RS@latin", "sr_RS", "sr@latin", "sr"}; !slices.Equal(got, want) {
		t.Fatalf("languageNames: %q, want %q", got, want)
	}
	if got := unescapeDesktop(`a\sb\\c\nd`); got != "a b\\c\nd" {
		t.Fatalf("unescape: %q", got)
	}
}

// writeEntry writes a desktop entry: its lines after [Desktop Entry].
func writeEntry(t *testing.T, path string, lines ...string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte("[Desktop Entry]\n"+strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// asuka/sdk/apps: the entries a menu shows, from the user's folder then the
// system's, the first of an ID hiding the others, named in the user's
// language.
func TestAppsList(t *testing.T) {
	home, sys, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_DATA_DIRS", sys)
	t.Setenv("XDG_CURRENT_DESKTOP", "ubuntu:GNOME")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	ha, sa := filepath.Join(home, "applications"), filepath.Join(sys, "applications")
	app := []string{"Type=Application", "Exec=true"}
	writeEntry(t, filepath.Join(ha, "org.test.One.desktop"), append(app, "Name=One", "Name[de]=Eins")...)
	writeEntry(t, filepath.Join(sa, "org.test.One.desktop"), append(app, "Name=System One")...) // hidden by the user's
	writeEntry(t, filepath.Join(ha, "vendor", "two.desktop"), append(app, "Name=Two")...)       // vendor-two.desktop
	writeEntry(t, filepath.Join(ha, "masked.desktop"), append(app, "Name=Masked", "Hidden=true")...)
	writeEntry(t, filepath.Join(sa, "masked.desktop"), append(app, "Name=Masked")...)
	writeEntry(t, filepath.Join(sa, "nodisplay.desktop"), append(app, "Name=NoDisplay", "NoDisplay=true")...)
	writeEntry(t, filepath.Join(sa, "kde.desktop"), append(app, "Name=KDE only", "OnlyShowIn=KDE;")...)
	writeEntry(t, filepath.Join(sa, "gnome.desktop"), append(app, "Name=GNOME only", "OnlyShowIn=GNOME;")...)
	writeEntry(t, filepath.Join(sa, "notgnome.desktop"), append(app, "Name=Not GNOME", "NotShowIn=GNOME;")...)
	writeEntry(t, filepath.Join(sa, "tryexec.desktop"), append(app, "Name=Missing", "TryExec=/nonexistent/asuka-app")...)
	writeEntry(t, filepath.Join(sa, "tryexec-sh.desktop"), append(app, "Name=Shell", "TryExec=sh")...)
	writeEntry(t, filepath.Join(sa, "link.desktop"), "Type=Link", "Name=A link", "URL=https://example.com")
	writeEntry(t, filepath.Join(sa, "noexec.desktop"), "Type=Application", "Name=No Exec")
	writeEntry(t, filepath.Join(sa, "dbus.desktop"), "Type=Application", "Name=DBus", "DBusActivatable=true")
	writeEntry(t, filepath.Join(elsewhere, "flat.desktop"), append(app, "Name=Flat")...)
	os.Symlink(filepath.Join(elsewhere, "flat.desktop"), filepath.Join(sa, "org.test.Flat.desktop")) // as Flatpak's exports are
	os.WriteFile(filepath.Join(sa, "notes.txt"), []byte("x"), 0o644)

	h, _ := mount(t, emptyRoot)
	b, err := callModule(t, h, "apps", "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	var list []installedApp
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range list {
		for _, d := range []string{home, sys} {
			if rel, err := filepath.Rel(d, a.Path); err == nil && !strings.HasPrefix(rel, "..") {
				got = append(got, a.ID+" "+a.Name+" "+filepath.Base(d)+"/"+rel)
			}
		}
	}
	slices.Sort(got)
	want := []string{
		"dbus.desktop DBus " + filepath.Base(sys) + "/applications/dbus.desktop",
		"gnome.desktop GNOME only " + filepath.Base(sys) + "/applications/gnome.desktop",
		"org.test.Flat.desktop Flat " + filepath.Base(sys) + "/applications/org.test.Flat.desktop",
		"org.test.One.desktop Eins " + filepath.Base(home) + "/applications/org.test.One.desktop",
		"tryexec-sh.desktop Shell " + filepath.Base(sys) + "/applications/tryexec-sh.desktop",
		"vendor-two.desktop Two " + filepath.Base(home) + "/applications/vendor/two.desktop",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("list:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The folders: the user's, the system's, then Flatpak's and Snap's.
	dirs := appDirs()
	if len(dirs) < 2 || dirs[0] != ha || dirs[1] != sa || !slices.Contains(dirs, "/var/lib/flatpak/exports/share/applications") {
		t.Fatalf("appDirs: %q", dirs)
	}
}

// asuka/sdk/apps: Icon draws an entry's icon (a file it names, or the
// theme's) as a PNG of the size at the monitor's scale, used again
// until the entry changes; a missing entry fails, as does opening one.
func TestAppsIcon(t *testing.T) {
	cache, dir := t.TempDir(), t.TempDir()
	t.Setenv("ASUKA_CACHE", cache)
	// A 64 pixel red square.
	src := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := range src.Pix {
		src.Pix[i] = []byte{255, 0, 0, 255}[i%4]
	}
	f, _ := os.Create(filepath.Join(dir, "red.png"))
	png.Encode(f, src)
	f.Close()
	file := filepath.Join(dir, "file.desktop")
	writeEntry(t, file, "Type=Application", "Exec=true", "Name=File", "Icon="+filepath.Join(dir, "red.png"))
	themed := filepath.Join(dir, "themed.desktop")
	writeEntry(t, themed, "Type=Application", "Exec=true", "Name=Themed", "Icon=asuka-no-such-icon")
	// An icon of the theme's (in a folder it searches: an unthemed one).
	icons := t.TempDir()
	b, _ := os.ReadFile(filepath.Join(dir, "red.png"))
	os.WriteFile(filepath.Join(icons, "asuka-test-red.png"), b, 0o644)
	named := filepath.Join(dir, "named.desktop")
	writeEntry(t, named, "Type=Application", "Exec=true", "Name=Named", "Icon=asuka-test-red")

	h, _ := mount(t, emptyRoot)
	var scale float64
	onMain(func() {
		scale = monitorScale()
		IconThemeAddSearchPath(IconThemeGetForDisplay(DisplayGetDefault()), icons)
	})
	icon := func(path string, size float64) (string, error) {
		args, _ := json.Marshal(map[string]any{"path": path, "size": size})
		b, err := callModule(t, h, "apps", "icon", args)
		return string(b), err
	}
	decode := func(p string) image.Image {
		t.Helper()
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	for _, size := range []float64{32, 20.5} {
		px := int(math.Ceil(size * scale))
		for _, entry := range []string{file, named, themed} {
			p, err := icon(entry, size)
			if err != nil {
				t.Fatalf("%s: %v", entry, err)
			}
			if !strings.HasPrefix(p, cache) {
				t.Fatalf("the icon at %s, not in the caches", p)
			}
			img := decode(p)
			if b := img.Bounds(); b.Dx() != px || b.Dy() != px {
				t.Fatalf("%s: a %v point icon is %v pixels, want %d", entry, size, b, px)
			}
			if entry != themed {
				r, g, b, a := img.At(px/2, px/2).RGBA()
				if c := (color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(a)}); r < 0xf000 || g > 0x0fff || b > 0x0fff || a < 0xf000 {
					t.Fatalf("%s: the red square drawn as %v", entry, c)
				}
			}
			st, _ := os.Stat(p)
			p2, err := icon(entry, size)
			st2, _ := os.Stat(p2)
			if err != nil || p2 != p || !st2.ModTime().Equal(st.ModTime()) {
				t.Fatalf("%s again: %s %v (%v, was %v)", entry, p2, err, st2.ModTime(), st.ModTime())
			}
			// The entry changed (no earlier than the icon was drawn): drawn
			// again.
			os.Chtimes(entry, st.ModTime(), st.ModTime())
			p3, err := icon(entry, size)
			st3, _ := os.Stat(p3)
			if err != nil || p3 != p || !st3.ModTime().After(st.ModTime()) {
				t.Fatalf("%s changed: %s %v, not drawn again", entry, p3, err)
			}
		}
	}
	missing := filepath.Join(dir, "missing.desktop")
	if _, err := icon(missing, 16); err == nil || !strings.Contains(err.Error(), "missing.desktop") {
		t.Fatalf("a missing entry's icon: %v", err)
	}
	if _, err := callModule(t, h, "apps", "open", []byte(missing)); err == nil || !strings.Contains(err.Error(), "missing.desktop") {
		t.Fatalf("open of a missing entry: %v", err)
	}
	notEntry := filepath.Join(dir, "red.png")
	if _, err := callModule(t, h, "apps", "open", []byte(notEntry)); err == nil {
		t.Fatal("open of a file that's no desktop entry")
	}
}

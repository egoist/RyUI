package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/xml"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func linuxTestApp(t *testing.T, json string) *goApp {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "My Notes")
	os.MkdirAll(filepath.Join(dir, "assets", "img"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "img", "a.txt"), []byte("asset"), 0o644)
	if json != "" {
		os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(json), 0o644)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &goApp{dir: dir, name: filepath.Base(dir), bundleID: cfg.ID, cfg: cfg}
}

// The build sets the host's name, ID, version and build (and the
// accent, as every platform's).
func TestLinuxLdflags(t *testing.T) {
	app := linuxTestApp(t, `{"name":"Notes & Co","id":"com.example.notes","version":"2.1","build":7,"accent":"#ff8800"}`)
	got := linuxLdflags(app, true)
	for _, want := range []string{
		"-ldflags=-s -w", "boot.Accent=#ff8800",
		`-X "github.com/egoist/ryui/internal/boot.Name=Notes & Co"`,
		`-X "github.com/egoist/ryui/internal/boot.ID=com.example.notes"`,
		`-X "github.com/egoist/ryui/internal/boot.Version=2.1"`,
		`-X "github.com/egoist/ryui/internal/boot.Build=7"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %s", want, got)
		}
	}
	if strings.Contains(linuxLdflags(app, false), "-s -w") {
		t.Error("a dev build is stripped")
	}
}

func TestLinuxExe(t *testing.T) {
	for name, want := range map[string]string{
		"Counter": "counter", "My Notes!": "my-notes", "Notes 2 Go": "notes-2-go", "笔记": "notes", "a/b": "a-b",
	} {
		app := &goApp{bundleID: "com.example.Notes", cfg: &appConfig{Name: name}}
		if got := linuxExe(app); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestGAppIDValid(t *testing.T) {
	for id, want := range map[string]bool{
		"com.example.notes": true, "dev.ryui.my-app": true, "dev.ryui.my_app": true,
		"notes": false, "com.example.2notes": false, "com..notes": false, "com.example.notes!": false,
	} {
		if gAppIDValid(id) != want {
			t.Errorf("%q: %v", id, !want)
		}
	}
}

func TestLinuxArch(t *testing.T) {
	if a, err := linuxArch("arm64"); a != "arm64" || err != nil {
		t.Error(a, err)
	}
	if a, err := linuxArch(""); a != "arm64" && a != "amd64" || err != nil {
		t.Error(a, err)
	}
	if _, err := linuxArch("386"); err == nil {
		t.Error("386: no error")
	}
}

// The prefix the build lays out: the assets where the host finds them
// from bin/<exe>, the icon at each size, the desktop file and the
// metainfo, named for the ID; and its .tar.gz.
func TestLinuxShare(t *testing.T) {
	app := linuxTestApp(t, `{"name":"My Notes","id":"com.example.notes","version":"1.2","icon":"icon.png",
		"linux":{"summary":"Write things down","description":"Notes, kept.\n\nOn <your> machine & synced.","categories":["Office","Utility"],
		"keywords":["memo"],"license":"MIT","developer":"Example Inc.","homepage":"https://example.com"}}`)
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	if err := writePNG(app.cfg.Icon, img); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "my-notes")
	if err := writeLinuxShare(app, out, "my-notes"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"share/my-notes/assets/img/a.txt",
		"share/icons/hicolor/16x16/apps/com.example.notes.png",
		"share/icons/hicolor/512x512/apps/com.example.notes.png",
		"share/applications/com.example.notes.desktop",
		"share/metainfo/com.example.notes.metainfo.xml",
	} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Error(err)
		}
	}
	if icon, err := loadIcon(filepath.Join(out, "share/icons/hicolor/48x48/apps/com.example.notes.png")); err != nil || icon.Bounds().Dx() != 48 {
		t.Errorf("48x48: %v", err)
	}

	desktop, _ := os.ReadFile(filepath.Join(out, "share/applications/com.example.notes.desktop"))
	for _, want := range []string{
		"[Desktop Entry]\nType=Application\n", "\nName=My Notes\n", "\nComment=Write things down\n", "\nExec=my-notes\n",
		"\nIcon=com.example.notes\n", "\nCategories=Office;Utility;\n", "\nKeywords=memo;\n", "\nStartupWMClass=my-notes\n",
	} {
		if !strings.Contains(string(desktop), want) {
			t.Errorf("the desktop file has no %q:\n%s", want, desktop)
		}
	}

	meta, _ := os.ReadFile(filepath.Join(out, "share/metainfo/com.example.notes.metainfo.xml"))
	var c struct {
		Type       string   `xml:"type,attr"`
		ID         string   `xml:"id"`
		Name       string   `xml:"name"`
		Summary    string   `xml:"summary"`
		License    string   `xml:"project_license"`
		Developer  string   `xml:"developer>name"`
		Paragraphs []string `xml:"description>p"`
		Categories []string `xml:"categories>category"`
		Launchable string   `xml:"launchable"`
		Icon       string   `xml:"icon"`
		Release    struct {
			Version string `xml:"version,attr"`
		} `xml:"releases>release"`
	}
	if err := xml.Unmarshal(meta, &c); err != nil {
		t.Fatalf("%v:\n%s", err, meta)
	}
	if c.Type != "desktop-application" || c.ID != "com.example.notes" || c.Name != "My Notes" || c.Summary != "Write things down" ||
		c.License != "MIT" || c.Developer != "Example Inc." || c.Launchable != "com.example.notes.desktop" || c.Icon != "com.example.notes" ||
		c.Release.Version != "1.2" || len(c.Paragraphs) != 2 || c.Paragraphs[1] != "On <your> machine & synced." ||
		strings.Join(c.Categories, ",") != "Office,Utility" {
		t.Errorf("%+v\n%s", c, meta)
	}

	// The archive: under the folder's name, the executable executable.
	os.MkdirAll(filepath.Join(out, "bin"), 0o755)
	os.WriteFile(filepath.Join(out, "bin", "my-notes"), []byte("ELF"), 0o644)
	tgz := filepath.Join(t.TempDir(), "my-notes.tar.gz")
	if err := tarGz(out, tgz); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(tgz)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	modes := map[string]int64{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		modes[h.Name] = h.Mode
	}
	if modes["my-notes/bin/my-notes"] != 0o755 || modes["my-notes/share/my-notes/assets/img/a.txt"] != 0o644 || modes["my-notes/bin/"] != 0o755 {
		t.Errorf("%v", modes)
	}
}

// Without an icon, nothing names one; without linux's fields, the
// defaults.
func TestLinuxShareDefaults(t *testing.T) {
	app := linuxTestApp(t, "")
	if d := desktopEntry(app, "my-notes", false); strings.Contains(d, "Icon=") || !strings.Contains(d, "\nCategories=Utility;\n") || strings.Contains(d, "Comment=") {
		t.Error(d)
	}
	m := metainfo(app, false, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if strings.Contains(m, "<icon") || !strings.Contains(m, "<summary>My Notes</summary>") || !strings.Contains(m, `date="2026-01-02"`) ||
		!strings.Contains(m, "<description>\n    <p>My Notes</p>\n  </description>") || !strings.Contains(m, "<category>Utility</category>") {
		t.Error(m)
	}
}

func TestFlatpakManifest(t *testing.T) {
	app := linuxTestApp(t, `{"id":"com.example.notes"}`)
	m := flatpakManifest(app, "my-notes")
	for _, want := range []string{
		"\napp-id: com.example.notes\n", "\nruntime: org.gnome.Platform\n", "\nruntime-version: '" + flatpakRuntime + "'\n",
		"\nsdk: org.gnome.Sdk\n", "\ncommand: my-notes\n", "--socket=wayland", "cp -a bin share /app/", "\n        path: my-notes\n",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("no %q in\n%s", want, m)
		}
	}
	app.cfg.Linux.FlatpakRuntime = "47"
	if !strings.Contains(flatpakManifest(app, "my-notes"), "runtime-version: '47'") {
		t.Error("linux.flatpakRuntime")
	}
}

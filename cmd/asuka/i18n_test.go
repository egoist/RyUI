package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// appWithCatalogs makes an app directory with these catalogs.
func appWithCatalogs(t *testing.T, config string, catalogs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range catalogs {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCatalogsAreFound(t *testing.T) {
	dir := appWithCatalogs(t, "", map[string]string{"locales/en.json": `{"a": "A"}`, "locales/de.json": `{"a": "Ä"}`})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.I18n == nil || c.I18n.Dir != "locales" || c.I18n.Source != "en" {
		t.Fatalf("i18n %+v", c.I18n)
	}
	// One catalog is the source, whatever it's in.
	dir = appWithCatalogs(t, "", map[string]string{"locales/ja.json": `{"a": "あ"}`})
	if c, err = loadConfig(dir); err != nil || c.I18n.Source != "ja" {
		t.Fatalf("source %+v, %v", c.I18n, err)
	}
	// None, and none in asuka.json: nothing.
	if c, err = loadConfig(t.TempDir()); err != nil || c.I18n != nil {
		t.Fatalf("%+v, %v", c.I18n, err)
	}
	dir = appWithCatalogs(t, `{"i18n": {"dir": "tr", "source": "de"}}`, map[string]string{"tr/de.json": `{"a": "Ä"}`, "tr/en.json": `{"a": "A"}`})
	if c, err = loadConfig(dir); err != nil || c.I18n.Dir != "tr" || c.I18n.Source != "de" {
		t.Fatalf("%+v, %v", c.I18n, err)
	}
}

func TestGenerateWritesOnlyWhatChanged(t *testing.T) {
	dir := appWithCatalogs(t, "", map[string]string{"locales/en.json": `{"hello": "Hello, {name}!"}`})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.generateCatalogs(); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "locales", "catalog_gen.go")
	src, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"package locales", "func Hello(name string) string", "func (LiveMessages) Hello(name signal.Accessor[string]) signal.Accessor[string]"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("no %q in:\n%s", want, src)
		}
	}
	before, _ := os.Stat(gen)
	if err := c.generateCatalogs(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(gen)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("the same catalogs wrote the file again: a rebuild's watcher would see it")
	}
	// A broken catalog stops the build.
	if err := os.WriteFile(filepath.Join(dir, "locales", "en.json"), []byte(`{"hello": "{name"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.generateCatalogs(); err == nil {
		t.Error("no error for a message that doesn't parse")
	}
}

func TestPackagingStrings(t *testing.T) {
	dir := appWithCatalogs(t, `{"name": "Notes", "shortcuts": [{"id": "new", "title": "New note", "url": "notes://new"}]}`, map[string]string{
		"locales/en.json": `{"a": "A", "asuka.appName": "Notes"}`,
		"locales/de.json": `{"a": "Ä", "asuka.appName": "Notizen", "asuka.NSCameraUsageDescription": "Um \"Belege\" zu scannen.", "asuka.shortcut.new": "Neue Notiz"}`,
		"locales/ar.json": `{"a": "أ"}`,
	})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	info := map[string]any{}
	c.plistLocalizations(info)
	if got := info["CFBundleLocalizations"].([]string); !slices.Equal(got, []string{"en", "ar", "de"}) {
		t.Errorf("localizations %v", got)
	}
	if info["CFBundleDevelopmentRegion"] != "en" {
		t.Errorf("region %v", info["CFBundleDevelopmentRegion"])
	}
	if !strings.Contains(c.plistLocalizationKeys(), "<string>de</string>") {
		t.Errorf("keys %s", c.plistLocalizationKeys())
	}

	bundle := t.TempDir()
	if err := c.writeLprojs(bundle); err != nil {
		t.Fatal(err)
	}
	de, _ := os.ReadFile(filepath.Join(bundle, "de.lproj", "InfoPlist.strings"))
	for _, want := range []string{`"CFBundleDisplayName" = "Notizen";`, `"NSCameraUsageDescription" = "Um \"Belege\" zu scannen.";`} {
		if !strings.Contains(string(de), want) {
			t.Errorf("no %s in %s", want, de)
		}
	}
	if loc, _ := os.ReadFile(filepath.Join(bundle, "de.lproj", "Localizable.strings")); !strings.Contains(string(loc), `"New note" = "Neue Notiz";`) {
		t.Errorf("localizable: %s", loc)
	}
	// A language needs its lproj even when it says nothing of these.
	if _, err := os.Stat(filepath.Join(bundle, "ar.lproj", "InfoPlist.strings")); err != nil {
		t.Error(err)
	}

	if got := c.androidLabel(); got != "@string/asuka_app_name" {
		t.Errorf("label %s", got)
	}
	if !strings.Contains(c.androidManifest(false), `android:localeConfig="@xml/asuka_locales_config"`) {
		t.Error("no locale config in the manifest")
	}
	res := t.TempDir()
	if wrote, err := c.androidLocaleRes(res); err != nil || !wrote {
		t.Fatalf("%v, %v", wrote, err)
	}
	cfg, _ := os.ReadFile(filepath.Join(res, "xml", "asuka_locales_config.xml"))
	if !strings.Contains(string(cfg), `<locale android:name="de" />`) {
		t.Errorf("config %s", cfg)
	}
	strs, _ := os.ReadFile(filepath.Join(res, "values-b+de", "asuka_i18n.xml"))
	for _, want := range []string{`<string name="asuka_app_name">Notizen</string>`, `<string name="asuka_shortcut_new">Neue Notiz</string>`} {
		if !strings.Contains(string(strs), want) {
			t.Errorf("no %s in %s", want, strs)
		}
	}
	if def, _ := os.ReadFile(filepath.Join(res, "values", "asuka_i18n.xml")); !strings.Contains(string(def), ">Notes<") {
		t.Errorf("default %s", def)
	}
	if androidQualifier("zh-Hant") != "b+zh+Hant" {
		t.Error(androidQualifier("zh-Hant"))
	}

	// Without catalogs: nothing, and the plain name.
	plain, err := loadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if plain.androidLabel() != plain.Name || strings.Contains(plain.androidManifest(false), "localeConfig") {
		t.Error("an app without catalogs got their settings")
	}
}

func TestFrozenMessages(t *testing.T) {
	dir := appWithCatalogs(t, "", map[string]string{
		"locales/en.json": `{"title": "T"}`,
		"main.go": `package main

func main() {
	_ = ui.StaticText(locales.Title())
	_ = ui.Val(locales.Greeting("x"))
	_ = ui.Text(locales.Live.Title())
	_ = ui.Val("plain")
	_ = ui.Val(locales.In("de").Title())
	_ = signal.Func(locales.Title)
}
`,
	})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := frozenMessages(c, dir)
	if len(got) != 2 || !strings.HasPrefix(got[0], "main.go:4:") || !strings.Contains(got[1], "locales.Live.Greeting") {
		t.Errorf("warnings: %q", got)
	}
}

func TestI18nCommands(t *testing.T) {
	dir := t.TempDir()
	if err := cmdI18n([]string{"init", dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "locales", "catalog_gen.go")); err != nil {
		t.Error(err)
	}
	if err := cmdI18n([]string{"init", dir}); err == nil {
		t.Error("init twice: no error")
	}
	if err := cmdI18n([]string{"add", "de", "zh-TW", "-dir", dir}); err != nil {
		t.Fatal(err)
	}
	de, err := os.ReadFile(filepath.Join(dir, "locales", "de.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(de), `"appTitle": "Hello"`) || !strings.Contains(string(de), `"@@locale": "de"`) {
		t.Errorf("de.json: %s", de)
	}
	if err := cmdI18n([]string{"add", "en-XA", "-dir", dir}); err == nil {
		t.Error("a pseudo locale's catalog: no error")
	}
	if err := cmdI18n([]string{"check", dir}); err != nil {
		t.Error(err)
	}
	if err := setDevLocale("not a tag!", ""); err == nil {
		t.Error("-lang of no language: no error")
	}
	if err := setDevLocale("", "up"); err == nil {
		t.Error("-dir up: no error")
	}
	defer func() { devLang, devDirection = "", "" }()
	if err := setDevLocale("zh_TW", "rtl"); err != nil || devLang != "zh-TW" || devDirection != "rtl" {
		t.Errorf("%v %s %s", err, devLang, devDirection)
	}
	c := &appConfig{}
	if f := c.ldflags(false); !strings.Contains(f, "boot.Lang=zh-TW") || !strings.Contains(f, "boot.Direction=rtl") {
		t.Errorf("ldflags %s", f)
	}
}

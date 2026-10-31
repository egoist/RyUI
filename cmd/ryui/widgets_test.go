package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWidgetsConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) error {
		os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(s), 0o644)
		_, err := loadConfig(dir)
		return err
	}
	if err := write(`{"widgets": [{"kind": "today"}], "shortcuts": [{"id": "newNote", "title": "New Note", "url": "notes://new"}]}`); err != nil {
		t.Fatal(err)
	}
	c, _ := loadConfig(dir)
	if w := c.Widgets[0]; w.Name != "today" || strings.Join(w.Sizes, ",") != "small,medium" {
		t.Errorf("defaults: %+v", w)
	}
	if p := c.Shortcuts[0].Phrases; len(p) != 1 || p[0] != "New Note in ${app}" {
		t.Errorf("phrases: %v", p)
	}
	if c.appGroup() != "group."+c.ID {
		t.Errorf("widgets need the App Group: %q", c.appGroup())
	}
	for _, bad := range []string{
		`{"widgets": [{"kind": "1x"}]}`,
		`{"widgets": [{"kind": "a", "sizes": ["huge"]}]}`,
		`{"widgets": [{"kind": "aB"}, {"kind": "a_b"}]}`, // the same Android names
		`{"shortcuts": [{"id": "a", "title": "A", "url": "no scheme"}]}`,
		`{"shortcuts": [{"id": "a", "title": "A", "url": "x://a", "phrases": ["no app name"]}]}`,
		`{"shortcuts": [{"id": "a", "url": "x://a"}]}`,
	} {
		if write(bad) == nil {
			t.Errorf("no error for %s", bad)
		}
	}
}

func TestWidgetSwift(t *testing.T) {
	var c appConfig
	for i := 0; i < 12; i++ {
		c.Widgets = append(c.Widgets, widgetConfig{Kind: "k" + string(rune('a'+i)), Name: `Name "quoted"`, Sizes: []string{"small", "circular"}})
	}
	s := c.widgetKindsSwift()
	// Twelve: two nested bundles, the main one holding them.
	for _, want := range []string{`static let name = "Name \"quoted\""`, "[.systemSmall, .accessoryCircular]",
		"struct RyUIWidgets0_1: WidgetBundle", "RyUIWidgets0_0().body", "@main\nstruct RyUIWidgets: WidgetBundle"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in\n%s", want, s)
		}
	}
	c.Shortcuts = []shortcutConfig{{ID: "newNote", Title: "New Note", URL: "notes://new", Phrases: []string{"New note in ${app}"}}}
	s = c.shortcutsSwift()
	for _, want := range []string{"struct RyUIShortcut_newNote: AppIntent", `"url": "notes://new"`,
		`phrases: ["New note in \(.applicationName)"]`, `systemImageName: "app.badge"`} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in\n%s", want, s)
		}
	}
	if swiftString("a\\b\n\x01") != `"a\\b\n\u{1}"` {
		t.Errorf("swiftString: %s", swiftString("a\\b\n\x01"))
	}
}

func TestAndroidWidgetsAndShortcuts(t *testing.T) {
	c := &appConfig{ID: "com.example.app", Accent: "#336699"}
	c.Widgets = []widgetConfig{{Kind: "todayNext", Name: "Today's", Sizes: []string{"small", "large", "inline"}}, {Kind: "lock", Sizes: []string{"circular"}}}
	c.Shortcuts = []shortcutConfig{{ID: "newNote", Title: "New Note", URL: "notes://new", Symbol: "bell"}}
	if resName("todayNext") != "today_next" {
		t.Fatal(resName("todayNext"))
	}
	// Lock screen kinds aren't Android's.
	if ws := c.androidWidgets(); len(ws) != 1 {
		t.Fatalf("android widgets %v", ws)
	}
	if k := c.androidWidgetKotlin(); !strings.Contains(k, `class RyUIWidget_todayNext : dev.ryui.RyUIWidgetProvider("todayNext", "small,large")`) {
		t.Errorf("kotlin:\n%s", k)
	}
	man := c.androidManifest(false)
	for _, want := range []string{`android:name="dev.ryui.app.widgets.RyUIWidget_todayNext"`, `android:resource="@xml/ryui_widget_today_next"`,
		`android:name="dev.ryui.widget.kind" android:value="todayNext"`, `android:name="android.app.shortcuts" android:resource="@xml/ryui_shortcuts"`} {
		if !strings.Contains(man, want) {
			t.Errorf("no %q in the manifest", want)
		}
	}
	res := t.TempDir()
	if _, err := androidRes(c, res); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(res, name))
		if err != nil {
			t.Error(err)
		}
		return string(b)
	}
	if x := read("xml/ryui_widget_today_next.xml"); !strings.Contains(x, `android:minWidth="110dp"`) || !strings.Contains(x, `android:targetCellWidth="2"`) {
		t.Errorf("provider:\n%s", x)
	}
	if s := read("values/ryui_widgets.xml"); !strings.Contains(s, `Today\&#39;s`) {
		t.Errorf("strings (an apostrophe escaped for aapt):\n%s", s)
	}
	read("layout/ryui_widget_preview_today_next.xml")
	if x := read("xml/ryui_shortcuts.xml"); !strings.Contains(x, `android:targetPackage="com.example.app"`) || !strings.Contains(x, `android:data="notes://new"`) ||
		!strings.Contains(x, `android:icon="@drawable/ryui_shortcut_new_note"`) {
		t.Errorf("shortcuts:\n%s", x)
	}
	if g := read("drawable/ryui_shortcut_new_note_glyph.xml"); !strings.Contains(g, `android:fillColor="#FF336699"`) || !strings.Contains(g, "pathData") {
		t.Errorf("glyph:\n%s", g)
	}
}

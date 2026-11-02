package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	androidHost "github.com/egoist/ryui/platform/android"
	iosHost "github.com/egoist/ryui/platform/ios"
)

// ryui.json's widgets and shortcuts, and what they add to a build: on
// iOS, the widget extension (platform/ios/Widget with the app's kinds, in
// PlugIns) and the App Intents for the shortcuts (in the executable, with
// their metadata, as Xcode extracts it); on Android, an App Widget
// provider for each kind and the launcher's shortcuts.

// widgetConfig is a kind of widget.
//
//	{"kind": "today", "name": "Today", "description": "What's next today", "sizes": ["small", "medium"]}
type widgetConfig struct {
	// Kind names it, for widget.Set: letters, digits and _.
	Kind string `json:"kind"`
	// Name and Description are what the widget gallery says of it.
	Name        string `json:"name"`
	Description string `json:"description"`
	// Sizes are the sizes it comes in: small, medium, large, and iOS's
	// extraLarge (iPad), and lock screen's circular, rectangular, inline.
	// Default: small, medium.
	Sizes []string `json:"sizes"`
}

// shortcutConfig is an action the app offers the system.
//
//	{"id": "newNote", "title": "New Note", "url": "notes://new", "symbol": "square.and.pencil", "phrases": ["New note in ${app}"]}
type shortcutConfig struct {
	// ID names it: letters, digits and _.
	ID    string `json:"id"`
	Title string `json:"title"`
	// URL is what the app is opened with (ui.OnOpenURL gets it).
	URL string `json:"url"`
	// Symbol is an SF Symbol name, as ui.Symbol takes it.
	Symbol string `json:"symbol"`
	// Phrases are what Siri hears, each with ${app}, the app's name
	// (default: "<title> in ${app}"). iOS.
	Phrases []string `json:"phrases"`
}

var (
	identifier  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	widgetSizes = []string{"small", "medium", "large", "extraLarge", "circular", "rectangular", "inline"}
)

// checkWidgets checks the widgets and shortcuts, filling in defaults.
func (c *appConfig) checkWidgets() error {
	seen := map[string]bool{}
	for i := range c.Widgets {
		w := &c.Widgets[i]
		if !identifier.MatchString(w.Kind) {
			return fmt.Errorf("widget kind %q (want letters, digits and _, a letter first)", w.Kind)
		}
		if seen[resName(w.Kind)] {
			return fmt.Errorf("widget kind %q twice (as Android names it, %s)", w.Kind, resName(w.Kind))
		}
		seen[resName(w.Kind)] = true
		if w.Name == "" {
			w.Name = w.Kind
		}
		if len(w.Sizes) == 0 {
			w.Sizes = []string{"small", "medium"}
		}
		for _, s := range w.Sizes {
			if !slices.Contains(widgetSizes, s) {
				return fmt.Errorf("widget %s: size %q (want %s)", w.Kind, s, strings.Join(widgetSizes, ", "))
			}
		}
	}
	if len(c.Shortcuts) > 10 {
		return errors.New("shortcuts: at most 10 (iOS's limit for App Shortcuts)")
	}
	seen = map[string]bool{}
	for i := range c.Shortcuts {
		s := &c.Shortcuts[i]
		if !identifier.MatchString(s.ID) {
			return fmt.Errorf("shortcut id %q (want letters, digits and _, a letter first)", s.ID)
		}
		if seen[resName(s.ID)] {
			return fmt.Errorf("shortcut id %q twice (as Android names it, %s)", s.ID, resName(s.ID))
		}
		seen[resName(s.ID)] = true
		if s.Title == "" {
			return fmt.Errorf("shortcut %s: no title", s.ID)
		}
		if u, err := url.Parse(s.URL); err != nil || u.Scheme == "" {
			return fmt.Errorf("shortcut %s: url %q (want one the app opens: its scheme's, or a link's)", s.ID, s.URL)
		}
		if len(s.Phrases) == 0 {
			s.Phrases = []string{s.Title + " in ${app}"}
		}
		for _, p := range s.Phrases {
			if strings.Count(p, "${app}") != 1 {
				return fmt.Errorf("shortcut %s: phrase %q (want the app's name in it once: ${app})", s.ID, p)
			}
		}
	}
	return nil
}

// resName is an identifier as an Android resource name: lowercase, an
// underscore before each capital (openMap: open_map).
func resName(id string) string {
	var b strings.Builder
	for i, r := range id {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// swiftString is s as a Swift string literal.
func swiftString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 || !unicode.IsPrint(r):
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// iOS: the widget extension.

const widgetExe = "RyUIWidgets"

// widgetID is the widget extension's bundle ID, which a device build
// needs a provisioning profile for.
func (c *appConfig) widgetID() string { return c.ID + ".widgets" }

// widgetFamilies are WidgetKit's families for the sizes.
var widgetFamilies = map[string]string{
	"small": ".systemSmall", "medium": ".systemMedium", "large": ".systemLarge", "extraLarge": ".systemExtraLarge",
	"circular": ".accessoryCircular", "rectangular": ".accessoryRectangular", "inline": ".accessoryInline",
}

// widgetKindsSwift is the extension's Kinds.swift: a RyUIWidgetKind for
// each kind, and the bundle of them (WidgetBundleBuilder takes ten at a
// time: more go in nested bundles).
func (c *appConfig) widgetKindsSwift() string {
	var b strings.Builder
	b.WriteString("// Generated by the RyUI CLI from ryui.json's widgets.\nimport SwiftUI\nimport WidgetKit\n\n")
	var names []string
	for i, w := range c.Widgets {
		name := fmt.Sprintf("RyUIKind%d", i)
		names = append(names, fmt.Sprintf("RyUIKindWidget<%s>()", name))
		var fams []string
		for _, s := range w.Sizes {
			fams = append(fams, widgetFamilies[s])
		}
		fmt.Fprintf(&b, "enum %s: RyUIWidgetKind {\n    static let kind = %s\n    static let name = %s\n    static let summary = %s\n    static let families: [WidgetFamily] = [%s]\n}\n\n",
			name, swiftString(w.Kind), swiftString(w.Name), swiftString(w.Description), strings.Join(fams, ", "))
	}
	// Ten to a bundle, the bundles ten to a bundle, up to the one @main.
	level := 0
	for len(names) > 10 {
		var next []string
		for i := 0; i < len(names); i += 10 {
			name := fmt.Sprintf("RyUIWidgets%d_%d", level, i/10)
			fmt.Fprintf(&b, "struct %s: WidgetBundle {\n    @WidgetBundleBuilder var body: some Widget {\n", name)
			for _, n := range names[i:min(i+10, len(names))] {
				b.WriteString("        " + n + "\n")
			}
			b.WriteString("    }\n}\n\n")
			next = append(next, name+"().body")
		}
		names, level = next, level+1
	}
	b.WriteString("@main\nstruct RyUIWidgets: WidgetBundle {\n    @WidgetBundleBuilder var body: some Widget {\n")
	for _, n := range names {
		b.WriteString("        " + n + "\n")
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

// widgetEntitlements are the linker flags that give the extension its
// entitlements on the simulator: the App Group the app leaves the
// entries in.
func (t *iosToolchain) widgetEntitlements(work string, cfg *appConfig) ([]string, string, error) {
	if !strings.HasSuffix(t.target, "-simulator") {
		return nil, "", nil
	}
	ent := plist(map[string]any{
		"application-identifier":                "RYUISIMULA." + cfg.widgetID(),
		"com.apple.security.application-groups": []string{cfg.appGroup()},
	})
	path := filepath.Join(work, "widgets.entitlements")
	if err := os.WriteFile(path, []byte(ent), 0o644); err != nil {
		return nil, "", err
	}
	return []string{"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", path}, ent, nil
}

// buildWidgets compiles the widget extension's executable: the template
// (platform/ios/Widget) with the app's kinds, cached by its sources, the
// SDK and the flags.
func (t *iosToolchain) buildWidgets(work string, cfg *appConfig) (string, error) {
	ents, ent, err := t.widgetEntitlements(work, cfg)
	if err != nil {
		return "", err
	}
	opt := []string{"-Onone", "-D", "DEBUG"}
	if t.release {
		opt = []string{"-O", "-Xlinker", "-dead_strip"}
	}
	// An extension, with the extension-safe APIs only. Its entry point is
	// NSExtensionMain, as Xcode links one (the @main WidgetBundle's main
	// returns where it runs on its own); ExtensionKit finds the bundle.
	args := append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5",
		"-module-name", widgetExe, "-parse-as-library", "-application-extension",
		"-Xlinker", "-e", "-Xlinker", "_NSExtensionMain", "-Xlinker", "-application_extension",
		"-framework", "Foundation"}, opt...)
	args = append(args, ents...)
	h := sha256.New()
	h.Write([]byte(ent))
	fmt.Fprintln(h, args)
	sources := map[string][]byte{"Kinds.swift": []byte(cfg.widgetKindsSwift())}
	err = fs.WalkDir(iosHost.Files, "Widget", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".swift") {
			sources[filepath.Base(path)], err = iosHost.Files.ReadFile(path)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		h.Write([]byte(n))
		h.Write(sources[n])
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(work)), "ios-widgets", hex.EncodeToString(h.Sum(nil))[:16])
	exe := filepath.Join(dir, widgetExe)
	if exists(exe) {
		return exe, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	args = append(args, "-o", exe+".tmp")
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, sources[n], 0o644); err != nil {
			return "", err
		}
		args = append(args, p)
	}
	if err := t.run(dir, nil, "xcrun", args...); err != nil {
		return "", err
	}
	return exe, os.Rename(exe+".tmp", exe)
}

// widgetInfo is the extension's Info.plist: its ID and WidgetKit's
// extension point, the App Group and the accent, and the app's name,
// version, platform and build keys (from app, the app's Info.plist).
func (c *appConfig) widgetInfo(app map[string]any) map[string]any {
	m := map[string]any{
		"CFBundleIdentifier":            c.widgetID(),
		"CFBundleExecutable":            widgetExe,
		"CFBundlePackageType":           "XPC!",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"RyUIAppGroup":                  c.appGroup(),
		"NSExtension":                   map[string]any{"NSExtensionPointIdentifier": "com.apple.widgetkit-extension"},
	}
	if c.Accent != "" {
		m["RyUIAccent"] = c.Accent
	}
	for k, v := range app {
		switch k {
		case "CFBundleName", "CFBundleDisplayName", "CFBundleShortVersionString", "CFBundleVersion", "MinimumOSVersion", "CFBundleSupportedPlatforms", "UIDeviceFamily", "UIRequiredDeviceCapabilities":
			m[k] = v
		default:
			if strings.HasPrefix(k, "DT") {
				m[k] = v
			}
		}
	}
	return m
}

// embedWidgets puts the extension in bundle's PlugIns, returning its
// path. It's signed before the app, whose signature seals it.
func embedWidgets(bundle, exe string, info map[string]any) (string, error) {
	appex := filepath.Join(bundle, "PlugIns", widgetExe+".appex")
	if err := os.MkdirAll(appex, 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(appex, widgetExe), data, 0o755); err != nil {
		return "", err
	}
	return appex, os.WriteFile(filepath.Join(appex, "Info.plist"), []byte(plist(info)), 0o644)
}

// iOS: the shortcuts' App Intents.

const shortcutsModule = "RyUIShortcuts"

// constProtocols are the protocols whose conformances swiftc writes the
// constant values of, for appintentsmetadataprocessor: Xcode's list.
const constProtocols = `["AnyResolverProviding","AppEntity","AppEnum","AppExtension","AppIntent","AppIntentsPackage","AppShortcutProviding","AppShortcutsProvider","AppUnionValue","AppUnionValueCasesProviding","DynamicOptionsProvider","EntityQuery","ExtensionPointDefining","IntentValueQuery","Resolver","TransientEntity","_AssistantIntentsProvider","_GenerativeFunctionExtractable","_IntentValueRepresentable"]`

// shortcutsSwift is the App Intents source: an intent for each shortcut,
// which opens the app and hands the host its URL (the RyUIOpenURL
// notification, which the app delegate passes to ui.OnOpenURL), and the
// App Shortcuts that offer them, with their phrases.
func (c *appConfig) shortcutsSwift() string {
	var b strings.Builder
	b.WriteString("// Generated by the RyUI CLI from ryui.json's shortcuts.\nimport AppIntents\nimport Foundation\n\n")
	for _, s := range c.Shortcuts {
		fmt.Fprintf(&b, `struct RyUIShortcut_%s: AppIntent {
    static let title: LocalizedStringResource = %s
    static let openAppWhenRun: Bool = true

    @MainActor
    func perform() async throws -> some IntentResult {
        NotificationCenter.default.post(name: Notification.Name("RyUIOpenURL"), object: nil, userInfo: ["url": %s])
        return .result()
    }
}

`, s.ID, swiftString(s.Title), swiftString(s.URL))
	}
	b.WriteString("struct RyUIAppShortcuts: AppShortcutsProvider {\n    static var appShortcuts: [AppShortcut] {\n")
	for _, s := range c.Shortcuts {
		var phrases []string
		for _, p := range s.Phrases {
			// ${app}: the app's name, as AppShortcutPhrase interpolates it.
			lit := swiftString(p)
			phrases = append(phrases, strings.Replace(lit, "${app}", `\(.applicationName)`, 1))
		}
		symbol := s.Symbol
		if symbol == "" {
			symbol = "app.badge"
		}
		fmt.Fprintf(&b, "        AppShortcut(intent: RyUIShortcut_%s(), phrases: [%s], shortTitle: %s, systemImageName: %s)\n",
			s.ID, strings.Join(phrases, ", "), swiftString(s.Title), swiftString(symbol))
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

// buildShortcuts compiles the App Intents into an object for the app's
// executable, with their constant values for the metadata. It returns
// the object, its source and the constant values' file ("" if the app
// has no shortcuts).
func (t *iosToolchain) buildShortcuts(work string, cfg *appConfig) (obj, src, consts string, err error) {
	if len(cfg.Shortcuts) == 0 {
		return "", "", "", nil
	}
	dir := filepath.Join(work, "shortcuts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", err
	}
	// Absolute paths: the metadata processor matches the constant values'
	// files to the sources by them.
	src = filepath.Join(dir, "Shortcuts.swift")
	protocols := filepath.Join(dir, "protocols.json")
	obj = filepath.Join(dir, "Shortcuts.o")
	consts = filepath.Join(dir, "Shortcuts.swiftconstvalues")
	if err := os.WriteFile(src, []byte(cfg.shortcutsSwift()), 0o644); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(protocols, []byte(constProtocols), 0o644); err != nil {
		return "", "", "", err
	}
	opt := "-Onone"
	if t.release {
		opt = "-O"
	}
	err = t.run(dir, nil, "xcrun", "swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5", opt,
		"-module-name", shortcutsModule, "-parse-as-library", "-wmo", "-c", "-o", obj, src,
		"-Xfrontend", "-const-gather-protocols-file", "-Xfrontend", protocols, "-emit-const-values-path", consts)
	return obj, src, consts, err
}

// shortcutsMetadata writes the App Intents' metadata
// (Metadata.appintents) into bundle, as Xcode's build does: the system
// reads the shortcuts, their phrases and titles from it.
func (t *iosToolchain) shortcutsMetadata(work, bundle, src, consts string) error {
	dir := filepath.Dir(src)
	sources, constList := filepath.Join(dir, "sources.txt"), filepath.Join(dir, "consts.txt")
	if err := os.WriteFile(sources, []byte(src+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(constList, []byte(consts+"\n"), 0o644); err != nil {
		return err
	}
	toolchain, err := output("xcrun", "--find", "swiftc")
	if err != nil {
		return err
	}
	// .../XcodeDefault.xctoolchain/usr/bin/swiftc
	toolchain = filepath.Dir(filepath.Dir(filepath.Dir(strings.TrimSpace(toolchain))))
	_, build := xcodeVersion()
	out := filepath.Join(dir, "metadata")
	os.RemoveAll(out)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	family := "iOS"
	if err := t.run(dir, nil, "xcrun", "appintentsmetadataprocessor", "--output", out, "--toolchain-dir", toolchain,
		"--module-name", shortcutsModule, "--sdk-root", t.sdk, "--xcode-version", build, "--platform-family", family,
		"--deployment-target", iosMin, "--target-triple", t.target, "--source-file-list", sources,
		"--swift-const-vals-list", constList, "--force"); err != nil {
		return err
	}
	meta := filepath.Join(out, "Metadata.appintents")
	if !exists(meta) {
		return errors.New("the App Intents metadata processor wrote no Metadata.appintents")
	}
	dst := filepath.Join(bundle, "Metadata.appintents")
	os.RemoveAll(dst)
	return t.run(work, nil, "ditto", meta, dst)
}

// Android: the widgets' providers and the launcher's shortcuts.

// androidWidgetSizes are a kind's sizes Android has (it has no lock
// screen widgets).
func (w widgetConfig) androidSizes() []string {
	var out []string
	for _, s := range w.Sizes {
		if s == "small" || s == "medium" || s == "large" {
			out = append(out, s)
		}
	}
	return out
}

// androidWidgets are the kinds Android shows.
func (c *appConfig) androidWidgets() []widgetConfig {
	var out []widgetConfig
	for _, w := range c.Widgets {
		if len(w.androidSizes()) > 0 {
			out = append(out, w)
		}
	}
	return out
}

// androidWidgetKotlin is the providers' source: a RyUIWidgetProvider for
// each kind, which the manifest's receivers name.
func (c *appConfig) androidWidgetKotlin() string {
	var b strings.Builder
	b.WriteString("// Generated by the RyUI CLI from ryui.json's widgets.\npackage dev.ryui.app.widgets\n\n")
	for _, w := range c.androidWidgets() {
		fmt.Fprintf(&b, "class RyUIWidget_%s : dev.ryui.RyUIWidgetProvider(%q, %q)\n", w.Kind, w.Kind, strings.Join(w.androidSizes(), ","))
	}
	return b.String()
}

// androidWidgetReceivers are the manifest's receivers for the providers.
func (c *appConfig) androidWidgetReceivers() string {
	var b strings.Builder
	for _, w := range c.androidWidgets() {
		fmt.Fprintf(&b, `        <receiver android:name="dev.ryui.app.widgets.RyUIWidget_%[1]s" android:exported="true" android:label="@string/ryui_widget_%[2]s_name">
            <intent-filter><action android:name="android.appwidget.action.APPWIDGET_UPDATE" /></intent-filter>
            <meta-data android:name="android.appwidget.provider" android:resource="@xml/ryui_widget_%[2]s" />
            <meta-data android:name="dev.ryui.widget.kind" android:value="%[1]s" />
        </receiver>
`, w.Kind, resName(w.Kind))
	}
	return b.String()
}

// xmlText escapes s for an Android resource string.
func xmlText(s string) string {
	// aapt's escapes first: the XML's entities are decoded before it reads them.
	s = strings.NewReplacer(`\`, `\\`, `'`, `\'`, `"`, `\"`, "\n", `\n`).Replace(s)
	if strings.HasPrefix(s, "@") || strings.HasPrefix(s, "?") {
		s = `\` + s // a reference otherwise
	}
	return html.EscapeString(s)
}

// androidWidgetRes writes each kind's appwidget-provider and strings: its
// default size the smallest it comes in (small: 2×2 cells, medium: 4×2,
// large: 4×4), resizable up to the largest.
func (c *appConfig) androidWidgetRes(res string) error {
	ws := c.androidWidgets()
	if len(ws) == 0 {
		return nil
	}
	cells := map[string][2]int{"small": {2, 2}, "medium": {4, 2}, "large": {4, 4}}
	dp := func(n int) int { return 70*n - 30 } // the launcher's cell formula
	var strs strings.Builder
	strs.WriteString("<resources>\n")
	for _, w := range ws {
		sizes := w.androidSizes()
		lo, hi := cells[sizes[0]], cells[sizes[0]]
		for _, s := range sizes {
			c := cells[s]
			if c[0]*c[1] < lo[0]*lo[1] {
				lo = c
			}
			if c[0]*c[1] > hi[0]*hi[1] {
				hi = c
			}
		}
		xml := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!-- Generated by the RyUI CLI from ryui.json's widgets: %s. -->
<appwidget-provider xmlns:android="http://schemas.android.com/apk/res/android"
    android:minWidth="%ddp"
    android:minHeight="%ddp"
    android:minResizeWidth="%ddp"
    android:minResizeHeight="%ddp"
    android:maxResizeWidth="%ddp"
    android:maxResizeHeight="%ddp"
    android:targetCellWidth="%d"
    android:targetCellHeight="%d"
    android:resizeMode="horizontal|vertical"
    android:widgetCategory="home_screen"
    android:updatePeriodMillis="0"
    android:initialLayout="@layout/ryui_widget_loading"
    android:previewLayout="@layout/ryui_widget_preview_%s"
    android:description="@string/ryui_widget_%s_description" />
`, w.Kind, dp(lo[0]), dp(lo[1]), dp(lo[0]), dp(lo[1]), dp(max(hi[0], 4))+40, dp(max(hi[1], 4))+40, lo[0], lo[1], resName(w.Kind), resName(w.Kind))
		p := filepath.Join(res, "xml", "ryui_widget_"+resName(w.Kind)+".xml")
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(xml), 0o644); err != nil {
			return err
		}
		// The gallery's preview (Android 12): the template with the name.
		pv := filepath.Join(res, "layout", "ryui_widget_preview_"+resName(w.Kind)+".xml")
		os.MkdirAll(filepath.Dir(pv), 0o755)
		if err := os.WriteFile(pv, []byte(fmt.Sprintf(widgetPreview, resName(w.Kind), resName(w.Kind))), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&strs, "    <string name=\"ryui_widget_%s_name\">%s</string>\n    <string name=\"ryui_widget_%s_description\">%s</string>\n",
			resName(w.Kind), xmlText(w.Name), resName(w.Kind), xmlText(firstOf(w.Description, w.Name)))
	}
	strs.WriteString("</resources>\n")
	os.MkdirAll(filepath.Join(res, "values"), 0o755)
	return os.WriteFile(filepath.Join(res, "values", "ryui_widgets.xml"), []byte(strs.String()), 0o644)
}

// widgetPreview is a kind's preview in the widget picker: its name, as the
// template's title, a placeholder value and its description.
const widgetPreview = `<?xml version="1.0" encoding="utf-8"?>
<!-- Generated by the RyUI CLI: a widget's preview. -->
<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="match_parent"
    android:background="@drawable/ryui_widget_background"
    android:orientation="vertical"
    android:padding="14dp">
    <TextView
        android:layout_width="match_parent"
        android:layout_height="wrap_content"
        android:maxLines="1"
        android:text="@string/ryui_widget_%s_name"
        android:textColor="@color/ryui_widget_accent"
        android:textSize="13sp"
        android:textStyle="bold" />
    <FrameLayout
        android:layout_width="match_parent"
        android:layout_height="0dp"
        android:layout_weight="1" />
    <TextView
        android:layout_width="match_parent"
        android:layout_height="wrap_content"
        android:fontFamily="sans-serif-medium"
        android:text="—"
        android:textColor="@color/ryui_widget_on_surface"
        android:textSize="30sp" />
    <TextView
        android:layout_width="match_parent"
        android:layout_height="wrap_content"
        android:ellipsize="end"
        android:maxLines="2"
        android:text="@string/ryui_widget_%s_description"
        android:textColor="@color/ryui_widget_on_surface_variant"
        android:textSize="13sp" />
</LinearLayout>
`

var symbolEntry = regexp.MustCompile(`"([^"]+)" to R\.drawable\.(ryui_sym_\w+)`)

// symbolDrawable is the Android host's drawable for an SF Symbol name
// (Symbols.kt's table), "" if it has none.
func symbolDrawable(name string) string {
	data, err := androidHost.Files.ReadFile("ryui/src/main/java/dev/ryui/Symbols.kt")
	if err != nil {
		return ""
	}
	for _, m := range symbolEntry.FindAllStringSubmatch(string(data), -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

var vectorPath = regexp.MustCompile(`android:pathData="([^"]+)"`)

// shortcutGlyph is an adaptive icon's foreground (108×108) of the symbol's
// paths (Material Symbols' 960 viewport, drawn up from y 960), 48 across
// in the middle, in color.
func shortcutGlyph(drawable, color string) (string, error) {
	data, err := androidHost.Files.ReadFile("ryui/src/main/res/drawable/" + drawable + ".xml")
	if err != nil {
		return "", err
	}
	var paths strings.Builder
	for _, m := range vectorPath.FindAllStringSubmatch(string(data), -1) {
		fmt.Fprintf(&paths, "            <path android:fillColor=\"%s\" android:pathData=\"%s\" />\n", color, m[1])
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!-- Generated by the RyUI CLI: a shortcut's symbol (%s). -->
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp" android:height="108dp"
    android:viewportWidth="108" android:viewportHeight="108">
    <group android:translateX="30" android:translateY="30" android:scaleX="0.05" android:scaleY="0.05">
        <group android:translateY="960">
%s        </group>
    </group>
</vector>
`, drawable, paths.String()), nil
}

// androidShortcutRes writes the launcher's shortcuts (xml/ryui_shortcuts),
// their labels and their icons: the symbol in the accent on white, as an
// adaptive icon.
func (c *appConfig) androidShortcutRes(res string) error {
	if len(c.Shortcuts) == 0 {
		return nil
	}
	accent := "#FF6750A4" // Material 3's baseline primary
	if c.Accent != "" {
		r, g, b := rgb(c.Accent[:7])
		accent = fmt.Sprintf("#FF%02X%02X%02X", r, g, b)
	}
	pkg := androidPackage(c.ID)
	var xml, strs strings.Builder
	xml.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!-- Generated by the RyUI CLI from ryui.json's shortcuts. -->\n<shortcuts xmlns:android=\"http://schemas.android.com/apk/res/android\">\n")
	strs.WriteString("<resources>\n")
	for _, s := range c.Shortcuts {
		rn := resName(s.ID)
		icon := ""
		if d := symbolDrawable(firstOf(s.Symbol, "app.badge")); d != "" {
			glyph, err := shortcutGlyph(d, accent)
			if err != nil {
				return err
			}
			files := map[string]string{
				"drawable/ryui_shortcut_" + rn + "_glyph.xml": glyph,
				"drawable/ryui_shortcut_" + rn + ".xml": fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@android:color/white" />
    <foreground android:drawable="@drawable/ryui_shortcut_%s_glyph" />
</adaptive-icon>
`, rn),
			}
			for name, text := range files {
				p := filepath.Join(res, name)
				os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
					return err
				}
			}
			icon = fmt.Sprintf("\n        android:icon=\"@drawable/ryui_shortcut_%s\"", rn)
		}
		fmt.Fprintf(&xml, `    <shortcut
        android:shortcutId="%[1]s"
        android:enabled="true"%[2]s
        android:shortcutShortLabel="@string/ryui_shortcut_%[5]s"
        android:shortcutLongLabel="@string/ryui_shortcut_%[5]s">
        <intent
            android:action="android.intent.action.VIEW"
            android:targetPackage="%[3]s"
            android:targetClass="dev.ryui.RyUIActivity"
            android:data="%[4]s" />
    </shortcut>
`, s.ID, icon, html.EscapeString(pkg), html.EscapeString(s.URL), rn)
		fmt.Fprintf(&strs, "    <string name=\"ryui_shortcut_%s\">%s</string>\n", rn, xmlText(s.Title))
	}
	xml.WriteString("</shortcuts>\n")
	strs.WriteString("</resources>\n")
	os.MkdirAll(filepath.Join(res, "xml"), 0o755)
	os.MkdirAll(filepath.Join(res, "values"), 0o755)
	if err := os.WriteFile(filepath.Join(res, "xml", "ryui_shortcuts.xml"), []byte(xml.String()), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(res, "values", "ryui_shortcuts.xml"), []byte(strs.String()), 0o644)
}

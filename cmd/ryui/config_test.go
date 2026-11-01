package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAccent(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"accent": "#7c3aed", "android": {"dynamicColor": true}}`)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "-ldflags=-s -w -X github.com/egoist/ryui/internal/boot.Accent=#7c3aed -X github.com/egoist/ryui/internal/boot.DynamicColor=1"
	if got := c.ldflags(true); got != want {
		t.Fatalf("ldflags %q", got)
	}
	if got := (&appConfig{}).ldflags(false); got != "-ldflags=" {
		t.Fatalf("no accent: %q", got)
	}
	write(`{"accent": "purple"}`)
	if _, err := loadConfig(dir); err == nil {
		t.Fatal("a bad accent was taken")
	}
}

func TestLinks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"id": "com.example.notes", "scheme": "notes", "links": ["notes.example.com"],
		"android": {"permissions": ["android.permission.CAMERA"]}}`), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := c.androidManifest(true)
	for _, want := range []string{`android.permission.CAMERA`, `android:scheme="notes"`, `android:autoVerify="true"`,
		`android:host="notes.example.com"`, `usesCleartextTraffic`} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest lacks %s:\n%s", want, m)
		}
	}
	if strings.Contains(c.androidManifest(false), "Cleartext") {
		t.Error("a release manifest allows cleartext")
	}
	if d := c.associatedDomains(true); len(d) != 1 || d[0] != "applinks:notes.example.com?mode=developer" {
		t.Errorf("domains %v", d)
	}
	os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"scheme": "my app"}`), 0o644)
	if _, err := loadConfig(dir); err == nil {
		t.Error("a bad scheme was taken")
	}
}

// shareTarget: on iOS, the share extension's scheme beside the app's
// own, and the extension's Info.plist, which takes the app's name,
// version and build keys; on Android, the ACTION_SEND filter.
func TestShareTarget(t *testing.T) {
	c := &appConfig{ID: "com.example.dl", Scheme: "dl", ShareTarget: textShares}
	got := plist(map[string]any{"CFBundleURLTypes": c.urlTypes(true)})
	if !strings.Contains(got, "<string>dl</string>\n\t\t\t\t<string>ryuishare.com.example.dl</string>") {
		t.Errorf("iOS URL types:\n%s", got)
	}
	if t2 := c.urlTypes(false); strings.Contains(plist(map[string]any{"T": t2}), "ryuishare") {
		t.Error("macOS has the share scheme")
	}
	if (&appConfig{ID: "x"}).urlTypes(true) != nil {
		t.Error("URL types with no scheme")
	}
	info := c.shareInfo(map[string]any{"CFBundleName": "DL", "CFBundleVersion": "7", "DTSDKName": "iphoneos27.1",
		"CFBundleIdentifier": "com.example.dl", "UILaunchScreen": map[string]any{}})
	if info["CFBundleIdentifier"] != "com.example.dl.share" || info["CFBundleName"] != "DL" || info["CFBundleVersion"] != "7" ||
		info["DTSDKName"] != "iphoneos27.1" || info["RyUIShareScheme"] != "ryuishare.com.example.dl" || info["UILaunchScreen"] != nil ||
		info["CFBundlePackageType"] != "XPC!" {
		t.Errorf("extension's Info.plist: %v", info)
	}
	if m := c.androidManifest(false); !strings.Contains(m, "android.intent.action.SEND\"") || !strings.Contains(m, `android:mimeType="text/plain"`) ||
		strings.Contains(m, "SEND_MULTIPLE") {
		t.Errorf("Android's filters:\n%s", m)
	}
	rule := info["NSExtension"].(map[string]any)["NSExtensionAttributes"].(map[string]any)["NSExtensionActivationRule"].(map[string]any)
	if len(rule) != 2 || rule["NSExtensionActivationSupportsText"] != true || rule["NSExtensionActivationSupportsWebURLWithMaxCount"] != 1 {
		t.Errorf("activation rule %v", rule)
	}
	if c.appGroup() != "" || info["RyUIAppGroup"] != nil {
		t.Error("an App Group for texts")
	}
}

// shareTarget is true, false or what the app takes: texts, files by
// MIME type, and how many at once.
func TestShareTargetConfig(t *testing.T) {
	dir := t.TempDir()
	load := func(js string) (*appConfig, error) {
		os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"id": "com.example.pics", "shareTarget": `+js+`}`), 0o644)
		return loadConfig(dir)
	}
	for js, want := range map[string]shareTarget{
		"false": {},
		"true":  textShares,
		`{"types": ["Image/*", "video/*"], "max": 5}`: {on: true, Types: []string{"image/*", "video/*"}, Max: 5},
		`{}`: textShares,
	} {
		c, err := load(js)
		if err != nil || c.ShareTarget.on != want.on || !slices.Equal(c.ShareTarget.Types, want.Types) || c.ShareTarget.Max != want.Max {
			t.Errorf("%s: %+v, %v", js, c.ShareTarget, err)
		}
	}
	for _, js := range []string{`{"types": ["pictures"]}`, `{"max": -1}`, `"yes"`} {
		if _, err := load(js); err == nil {
			t.Errorf("%s was taken", js)
		}
	}

	// Images and videos, five at once: Apple's keys, an App Group, and
	// both of Android's actions.
	c, _ := load(`{"types": ["text", "image/*", "video/*"], "max": 5}`)
	rule := c.shareInfo(nil)["NSExtension"].(map[string]any)["NSExtensionAttributes"].(map[string]any)["NSExtensionActivationRule"].(map[string]any)
	if rule["NSExtensionActivationSupportsImageWithMaxCount"] != 5 || rule["NSExtensionActivationSupportsMovieWithMaxCount"] != 5 ||
		rule["NSExtensionActivationSupportsText"] != true {
		t.Errorf("rule %v", rule)
	}
	if g := c.appGroup(); g != "group.com.example.pics" || c.shareInfo(nil)["RyUIAppGroup"] != g {
		t.Errorf("group %q", g)
	}
	m := c.androidManifest(false)
	single, multiple, _ := strings.Cut(m, "SEND_MULTIPLE")
	if !strings.Contains(single, `android:mimeType="text/plain"`) || !strings.Contains(single, `android:mimeType="image/*"`) ||
		!strings.Contains(multiple, `android:mimeType="video/*"`) || strings.Contains(multiple, "text/plain") {
		t.Errorf("Android's filters:\n%s", m)
	}

	// A type Apple has no key for: a predicate on the attachments' types.
	c, _ = load(`{"types": ["audio/*", "application/pdf", "application/x-thing"]}`)
	want := `SUBQUERY(extensionItems, $e, SUBQUERY($e.attachments, $a, ANY $a.registeredTypeIdentifiers UTI-CONFORMS-TO "public.audio" OR ` +
		`ANY $a.registeredTypeIdentifiers UTI-CONFORMS-TO "com.adobe.pdf" OR ANY $a.registeredTypeIdentifiers UTI-CONFORMS-TO "public.data").@count >= 1).@count >= 1`
	if got := c.ShareTarget.activationRule(); got != want {
		t.Errorf("predicate\n%v\nwant\n%v", got, want)
	}
	if strings.Contains(c.androidManifest(false), "SEND_MULTIPLE") {
		t.Error("SEND_MULTIPLE for one file")
	}
}

func TestFeaturesFromImports(t *testing.T) {
	var c appConfig
	c.permissionsFor([]string{"github.com/egoist/ryui/ui", "github.com/egoist/ryui/scan", "github.com/egoist/ryui/camera"})
	if got := c.gradleModules(); got != "-PryuiModules=scan" {
		t.Errorf("modules: %s", got)
	}
	if !slices.Contains(c.Android.Permissions, "android.permission.CAMERA") || slices.Contains(c.Android.Permissions, "android.permission.POST_NOTIFICATIONS") {
		t.Errorf("permissions: %v", c.Android.Permissions)
	}
	var pics appConfig
	pics.permissionsFor([]string{"github.com/egoist/ryui/vision", "github.com/egoist/ryui/photos"})
	if got := pics.gradleModules(); got != "-PryuiModules=vision" {
		t.Errorf("modules: %s", got)
	}
	if !slices.Contains(pics.Android.Permissions, "android.permission.READ_MEDIA_IMAGES") || pics.IOS.Usage["NSPhotoLibraryUsageDescription"] == "" {
		t.Errorf("photos: %v %v", pics.Android.Permissions, pics.IOS.Usage)
	}
	var none appConfig
	none.permissionsFor([]string{"github.com/egoist/ryui/ui"})
	if got := none.gradleModules(); got != "-PryuiModules=" {
		t.Errorf("no modules: %s", got)
	}
	if p := playerConfig(); !slices.Contains(p.androidModules, "scan") {
		t.Errorf("the player's modules: %v", p.androidModules)
	}
}

func TestIPad(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"orientations": ["portrait"]}`), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	// An iPad app, in every orientation whatever the iPhone's: Split
	// View and Stage Manager need them.
	if d := c.iosDevices(); !slices.Equal(d, []int{1, 2}) {
		t.Errorf("devices %v", d)
	}
	if o := c.ipadOrientations(); len(o) != 4 {
		t.Errorf("iPad orientations %v", o)
	}
	os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"ios": {"iphoneOnly": true}}`), 0o644)
	if c, err = loadConfig(dir); err != nil {
		t.Fatal(err)
	}
	if d := c.iosDevices(); !slices.Equal(d, []int{1}) || c.ipadOrientations() != nil {
		t.Errorf("iPhone only: devices %v, iPad orientations %v", d, c.ipadOrientations())
	}
}

// An app with its own share sheet (app.ShareSheet): Android's share
// filters are its Activity's; iOS's extension is the host's sheet, with
// the app's ID, and an App Group for the data even for texts alone; the
// app's keychain group for both.
func TestShareSheetConfig(t *testing.T) {
	c := &appConfig{ID: "com.example.pins", ShareTarget: textShares, shareSheet: true}
	m := c.androidManifest(false)
	main, sheet, ok := strings.Cut(m, `dev.ryui.RyUIShareActivity`)
	if !ok || strings.Contains(main, "action.SEND") || !strings.Contains(sheet, "action.SEND") || !strings.Contains(sheet, `android:exported="true"`) {
		t.Errorf("manifest:\n%s", m)
	}
	info := c.shareInfo(nil)
	if info["NSExtension"].(map[string]any)["NSExtensionPrincipalClass"] != "RyUIShareSheetController" ||
		info["RyUIShareSheet"] != true || info["RyUIAppID"] != "com.example.pins" || info["RyUIAppGroup"] != "group.com.example.pins" {
		t.Errorf("extension's Info.plist: %v", info)
	}
	if g := c.keychainGroup("TEAM123456"); g != "TEAM123456.com.example.pins" {
		t.Errorf("keychain group %q", g)
	}
	c.shareSheet = false
	if c.appGroup() != "" || c.keychainGroup("T") != "" || c.sharePrincipal() != "RyUIShareViewController" {
		t.Error("no sheet: a group, or the sheet's class")
	}
	// The sheet's symbol is found in a library's function names.
	dir := t.TempDir()
	lib := filepath.Join(dir, "libryui.so")
	os.WriteFile(lib, []byte("..."+sheetSymbol+"\x00..."), 0o644)
	other := filepath.Join(dir, "other.so")
	os.WriteFile(other, []byte("..."+sheetSymbol+"Thing\x00..."), 0o644)
	if !hasSheet(lib) || hasSheet(other) {
		t.Error("hasSheet")
	}
}

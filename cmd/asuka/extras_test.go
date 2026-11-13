package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The packages an app imports bring their entitlements, Info.plist arrays
// and Android modules; a release's profile must have the entitlements.
func TestPackageExtras(t *testing.T) {
	var c appConfig
	c.permissionsFor([]string{"github.com/egoist/asuka/push", "github.com/egoist/asuka/periodic", "github.com/egoist/asuka/appleauth"})
	if got := c.gradleModules(); got != "-PasukaModules=push,periodic" {
		t.Errorf("modules: %s", got)
	}
	if c.entitlements["aps-environment"] != "development" || c.entitlements["com.apple.developer.applesignin"] == nil {
		t.Errorf("entitlements: %v", c.entitlements)
	}
	if m := c.plistArrays["UIBackgroundModes"]; !slices.Contains(m, "remote-notification") || !slices.Contains(m, "fetch") {
		t.Errorf("background modes: %v", m)
	}
	x := c.plistExtras()
	if ids, _ := x["BGTaskSchedulerPermittedIdentifiers"].([]string); !slices.Equal(ids, []string{"dev.asuka.processing", "dev.asuka.refresh"}) {
		t.Errorf("task identifiers: %v", ids)
	}
	if s := plistKeys(x); !strings.Contains(s, "<key>UILaunchScreen</key>\n<dict>\n</dict>") {
		t.Errorf("no splash, an empty launch screen:\n%s", s)
	}

	ent := filepath.Join(t.TempDir(), "e.plist")
	os.WriteFile(ent, []byte(plist(map[string]any{"aps-environment": "production"})), 0o644)
	if m := c.missingEntitlements(ent); !slices.Equal(m, []string{"Sign in with Apple (asuka/appleauth)"}) {
		t.Errorf("missing: %v", m)
	}

	// Android: biometric's permission; maps' key in the manifest.
	var a appConfig
	a.Android.MapsKey = "KEY"
	a.permissionsFor([]string{"github.com/egoist/asuka/biometric", "github.com/egoist/asuka/maps"})
	man := a.androidManifest(false)
	if !strings.Contains(man, "android.permission.USE_BIOMETRIC") || !strings.Contains(man, `android:name="com.google.android.geo.API_KEY" android:value="KEY"`) {
		t.Errorf("manifest:\n%s", man)
	}
	if a.IOS.Usage["NSFaceIDUsageDescription"] == "" {
		t.Errorf("usage: %v", a.IOS.Usage)
	}
}

// Now Playing: on iOS, the audio background mode for an app that calls
// nowplaying.Show; on Android, the media playback foreground service and
// its permissions for one that imports it, and nothing for one that
// doesn't.
func TestNowPlayingExtras(t *testing.T) {
	var c appConfig
	c.addFeatureExtras([]string{"ASUKA_NOWPLAYING"})
	if m := c.plistArrays["UIBackgroundModes"]; !slices.Equal(m, []string{"audio"}) {
		t.Errorf("background modes: %v", m)
	}
	found := false
	for _, f := range iosFeatures {
		found = found || f.flag == "ASUKA_NOWPLAYING" && f.symbol == "github.com/egoist/asuka/nowplaying.Show"
	}
	if !found {
		t.Error("nowplaying.Show's symbol isn't a feature's")
	}
	var a appConfig
	a.permissionsFor([]string{"github.com/egoist/asuka/nowplaying"})
	man := a.androidManifest(false)
	for _, want := range []string{"android.permission.FOREGROUND_SERVICE\"", "android.permission.FOREGROUND_SERVICE_MEDIA_PLAYBACK",
		`<service android:name="dev.asuka.AsukaMediaService" android:exported="false" android:foregroundServiceType="mediaPlayback" />`} {
		if !strings.Contains(man, want) {
			t.Errorf("no %s in the manifest:\n%s", want, man)
		}
	}
	var none appConfig
	none.permissionsFor([]string{"github.com/egoist/asuka/audio"})
	if man := none.androidManifest(false); strings.Contains(man, "AsukaMediaService") || strings.Contains(man, "MEDIA_PLAYBACK") {
		t.Errorf("audio alone:\n%s", man)
	}
}

// asuka.json's splash: checked and defaulted; the launch screen and the
// Android theme and images that show it.
func TestSplash(t *testing.T) {
	dir := t.TempDir()
	logo := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	logo.SetNRGBA(32, 32, color.NRGBA{255, 0, 0, 255})
	writePNG(filepath.Join(dir, "logo.png"), logo)
	os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"splash": {"image": "logo.png", "background": "#123456"}}`), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.splashOn() || c.Splash.Size != 120 || c.Splash.DarkBackground != "#123456" {
		t.Fatalf("splash %+v", c.Splash)
	}
	ls, _ := c.plistExtras()["UILaunchScreen"].(map[string]any)
	if ls["UIImageName"] != "AsukaSplash" || ls["UIColorName"] != "AsukaSplashBackground" {
		t.Errorf("launch screen %v", ls)
	}
	if !strings.Contains(c.androidManifest(false), `android:theme="@style/Theme.Asuka.Splash"`) {
		t.Error("no splash theme in the manifest")
	}
	res := filepath.Join(dir, "res")
	if wrote, err := androidRes(c, res); err != nil || !wrote {
		t.Fatal(wrote, err)
	}
	for _, f := range []string{"values/asuka_splash.xml", "values-night/asuka_splash.xml", "values-v31/asuka_splash.xml",
		"drawable/asuka_splash_window.xml", "drawable-xxhdpi/asuka_splash_logo.png", "drawable-xxhdpi/asuka_splash_icon.png"} {
		if _, err := os.Stat(filepath.Join(res, f)); err != nil {
			t.Error(err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(res, "values/asuka_splash.xml")); !strings.Contains(string(b), "#123456") {
		t.Errorf("values:\n%s", b)
	}
	cat, err := writeAssetCatalog(c, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"AsukaSplash.imageset/splash@3x.png", "AsukaSplashBackground.colorset/Contents.json"} {
		if _, err := os.Stat(filepath.Join(cat, f)); err != nil {
			t.Error(err)
		}
	}

	os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"splash": {"background": "blue"}}`), 0o644)
	if _, err := loadConfig(dir); err == nil {
		t.Error("a bad background: no error")
	}
}

// google-services.json becomes the resources Firebase reads, for the
// app's own client.
func TestFirebaseValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "google-services.json")
	os.WriteFile(path, []byte(`{
  "project_info": {"project_number": "123", "project_id": "demo", "storage_bucket": "demo.appspot.com"},
  "client": [
    {"client_info": {"mobilesdk_app_id": "1:123:android:other", "android_client_info": {"package_name": "com.other"}}, "api_key": [{"current_key": "no"}]},
    {"client_info": {"mobilesdk_app_id": "1:123:android:abc", "android_client_info": {"package_name": "com.example.app"}}, "api_key": [{"current_key": "KEY"}]}
  ]}`), 0o644)
	v, err := firebaseValues(path, "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="google_app_id" translatable="false">1:123:android:abc<`, `name="gcm_defaultSenderId" translatable="false">123<`,
		`name="google_api_key" translatable="false">KEY<`, `name="project_id" translatable="false">demo<`} {
		if !strings.Contains(v, want) {
			t.Errorf("no %s in\n%s", want, v)
		}
	}
	if _, err := firebaseValues(path, "com.missing"); err == nil {
		t.Error("another app: no error")
	}
}

func TestBackgroundModesJoinThePackages(t *testing.T) {
	c := &appConfig{}
	c.plistArrays = map[string][]string{"UIBackgroundModes": {"fetch"}}
	c.IOS.BackgroundModes = []string{"audio", "fetch"}
	got, _ := c.plistExtras()["UIBackgroundModes"].([]string)
	if len(got) != 2 || got[0] != "audio" || got[1] != "fetch" {
		t.Errorf("UIBackgroundModes = %v", got)
	}
}

func TestPrivacyCollected(t *testing.T) {
	got := privacyCollected([]collectedData{{Type: "EmailAddress", Linked: true, Purposes: []string{"AppFunctionality"}}})
	m := got[0].(map[string]any)
	if m["NSPrivacyCollectedDataType"] != "NSPrivacyCollectedDataTypeEmailAddress" || m["NSPrivacyCollectedDataTypeLinked"] != true {
		t.Errorf("%v", m)
	}
	if p := m["NSPrivacyCollectedDataTypePurposes"].([]string); p[0] != "NSPrivacyCollectedDataTypePurposeAppFunctionality" {
		t.Errorf("%v", p)
	}
}

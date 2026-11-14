package main

import (
	"slices"
	"strings"
	"testing"
)

// asuka/sdk/location's background part costs only the apps that call it:
// Always's purpose string and ACCESS_BACKGROUND_LOCATION for
// RequestAlways, the location background mode and the foreground service
// for StartUpdates, the receiver and Play services' module for
// StartUpdates and Monitor, temporary full accuracy's purposes for
// RequestPrecise.
func TestLocationExtras(t *testing.T) {
	pkg := "github.com/egoist/asuka/sdk/location."
	var fg appConfig
	fg.permissionsFor([]string{"github.com/egoist/asuka/sdk/location"})
	fg.useLocation(func(s string) bool { return s == pkg+"Current" })
	fg.addFeatureExtras([]string{"ASUKA_LOCATION"})
	man := fg.androidManifest(false)
	for _, no := range []string{"BACKGROUND_LOCATION", "AsukaLocationService", "AsukaLocationReceiver", "BOOT_COMPLETED"} {
		if strings.Contains(man, no) {
			t.Errorf("foreground only: %s in the manifest:\n%s", no, man)
		}
	}
	if fg.IOS.Usage["NSLocationAlwaysAndWhenInUseUsageDescription"] != "" || fg.plistArrays["UIBackgroundModes"] != nil || slices.Contains(fg.androidModules, "location") {
		t.Errorf("foreground only: %v %v %v", fg.IOS.Usage, fg.plistArrays, fg.androidModules)
	}

	var bg appConfig
	bg.IOS.LocationPurposes = map[string]string{"route": "To draw your route."}
	bg.permissionsFor([]string{"github.com/egoist/asuka/sdk/location"})
	bg.useLocation(func(s string) bool { return s == pkg+"RequestAlways" || s == pkg+"StartUpdates" || s == pkg+"Monitor" })
	bg.addFeatureExtras([]string{"ASUKA_LOCATION", "ASUKA_LOCATION_BACKGROUND", "ASUKA_LOCATION_ALWAYS", "ASUKA_LOCATION_UPDATES", "ASUKA_LOCATION_PRECISE"})
	man = bg.androidManifest(false)
	for _, want := range []string{"android.permission.ACCESS_BACKGROUND_LOCATION", "android.permission.FOREGROUND_SERVICE_LOCATION",
		"android.permission.RECEIVE_BOOT_COMPLETED", `android:name="dev.asuka.AsukaLocationService" android:exported="false" android:foregroundServiceType="location"`,
		`<receiver android:name="dev.asuka.AsukaLocationReceiver"`, "android.intent.action.BOOT_COMPLETED"} {
		if !strings.Contains(man, want) {
			t.Errorf("no %s in the manifest:\n%s", want, man)
		}
	}
	if !slices.Contains(bg.androidModules, "location") {
		t.Errorf("modules %v", bg.androidModules)
	}
	if bg.IOS.Usage["NSLocationAlwaysAndWhenInUseUsageDescription"] == "" || bg.IOS.Usage["NSLocationWhenInUseUsageDescription"] == "" {
		t.Errorf("usage %v", bg.IOS.Usage)
	}
	if m := bg.plistArrays["UIBackgroundModes"]; !slices.Equal(m, []string{"location"}) {
		t.Errorf("background modes %v", m)
	}
	ps, _ := bg.plistExtras()["NSLocationTemporaryUsageDescriptionDictionary"].(map[string]any)
	if ps["default"] == nil || ps["route"] != "To draw your route." {
		t.Errorf("purposes %v", ps)
	}

	// Regions alone: the receiver and the module, no service, no background permission.
	var rg appConfig
	rg.useLocation(func(s string) bool { return s == pkg+"Monitor" })
	man = rg.androidManifest(false)
	if strings.Contains(man, "AsukaLocationService") || strings.Contains(man, "BACKGROUND_LOCATION") || !strings.Contains(man, "AsukaLocationReceiver") {
		t.Errorf("regions alone:\n%s", man)
	}

	// Every function an app may call alone is a feature's symbol.
	for _, f := range []string{"StartUpdates", "Monitor", "OnUpdates", "OnCrossing", "Supported", "RequestAlways", "RequestPrecise", "UseAuthorization"} {
		if !slices.ContainsFunc(iosFeatures, func(x struct{ flag, symbol string }) bool { return x.symbol == pkg+f }) {
			t.Errorf("%s isn't a feature's symbol", f)
		}
	}
	// The player has it all.
	p := playerConfig()
	if !slices.Contains(p.Android.Permissions, "android.permission.ACCESS_BACKGROUND_LOCATION") || p.IOS.Usage["NSLocationAlwaysAndWhenInUseUsageDescription"] == "" ||
		!slices.Contains(p.plistArrays["UIBackgroundModes"], "location") {
		t.Errorf("player: %v %v %v", p.Android.Permissions, p.IOS.Usage, p.plistArrays)
	}
}

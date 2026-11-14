package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// An app that imports asuka/sdk/bluetooth gets its permissions, its
// purpose string and the Flatpak's bus name; asuka.json's "bluetooth"
// adds the background modes and the location from scans.
func TestBluetoothConfig(t *testing.T) {
	deps := []string{"github.com/egoist/asuka/ui", bluetoothPackage}
	var c appConfig
	c.permissionsFor(deps)
	m := c.androidManifest(false)
	for _, want := range []string{
		`<uses-permission android:name="android.permission.BLUETOOTH_SCAN" android:usesPermissionFlags="neverForLocation" />`,
		`<uses-permission android:name="android.permission.BLUETOOTH_CONNECT" />`,
		`<uses-permission android:name="android.permission.BLUETOOTH_ADVERTISE" />`,
		`<uses-feature android:name="android.hardware.bluetooth_le" android:required="false" />`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("the manifest has no %s:\n%s", want, m)
		}
	}
	if strings.Contains(m, "AsukaBluetoothScanReceiver") || strings.Contains(m, "LOCATION") {
		t.Errorf("background scanning without asking for it:\n%s", m)
	}
	if c.IOS.Usage["NSBluetoothAlwaysUsageDescription"] == "" || len(c.plistArrays["UIBackgroundModes"]) != 0 {
		t.Errorf("iOS: %v %v", c.IOS.Usage, c.plistArrays)
	}
	if !slices.Contains(c.flatpakArgs, "--system-talk-name=org.bluez") {
		t.Errorf("flatpak: %v", c.flatpakArgs)
	}
	app := &goApp{cfg: &c, bundleID: "dev.asuka.ble", name: "ble"}
	if f := flatpakManifest(app, "ble", false); !strings.Contains(f, "  - --device=dri\n  - --system-talk-name=org.bluez\nmodules:") {
		t.Errorf("the Flatpak's manifest:\n%s", f)
	}

	// With location (another package's too): the location permission on
	// every Android, and scans that may find it.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"bluetooth": {"background": ["central", "peripheral"], "location": true}}`), 0o644)
	bg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	bg.permissionsFor(append(deps, "github.com/egoist/asuka/sdk/location"))
	m = bg.androidManifest(false)
	for _, want := range []string{
		`<uses-permission android:name="android.permission.BLUETOOTH_SCAN" />`,
		`<uses-permission android:name="android.permission.ACCESS_FINE_LOCATION" />`,
		`<receiver android:name="dev.asuka.AsukaBluetoothScanReceiver" android:exported="false" />`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("the manifest has no %s:\n%s", want, m)
		}
	}
	if n := strings.Count(m, "ACCESS_FINE_LOCATION"); n != 1 {
		t.Errorf("ACCESS_FINE_LOCATION %d times:\n%s", n, m)
	}
	if got := bg.plistArrays["UIBackgroundModes"]; !slices.Equal(got, []string{"bluetooth-central", "bluetooth-peripheral"}) {
		t.Errorf("background modes: %v", got)
	}

	// A background mode asked for, without the package: nothing.
	var none appConfig
	none.Bluetooth.Background = []string{"central"}
	none.permissionsFor([]string{"github.com/egoist/asuka/ui"})
	if len(none.plistArrays["UIBackgroundModes"]) != 0 || len(none.androidApp) != 0 {
		t.Errorf("without the package: %v %v", none.plistArrays, none.androidApp)
	}

	os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"bluetooth": {"background": ["beacon"]}}`), 0o644)
	if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "beacon") {
		t.Errorf("a bad background role: %v", err)
	}

	// The player runs any app: the background modes too.
	if p := playerConfig(); !slices.Contains(p.plistArrays["UIBackgroundModes"], "bluetooth-central") ||
		p.IOS.Usage["NSBluetoothAlwaysUsageDescription"] == "" {
		t.Errorf("the player: %v %v", p.plistArrays, p.IOS.Usage)
	}
}

// Permissions merge by name: the widest level, the fewest flags.
func TestAddPermission(t *testing.T) {
	var c appConfig
	for _, p := range []string{"a:28", "a:30", "b", "b:30", "c|neverForLocation", "c", "d:30|x", "d:28|x"} {
		c.addPermission(p)
	}
	if want := []string{"a:30", "b", "c", "d:30|x"}; !slices.Equal(c.Android.Permissions, want) {
		t.Errorf("%v, want %v", c.Android.Permissions, want)
	}
}

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// An app that imports asuka/sdk/nfc gets Android's permission and
// uses-feature, iOS's purpose string and entitlement, and its config's
// AIDs, system codes and filters; one that doesn't gets none of them,
// config or not.
func TestNFCConfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"id": "com.example.tags", "nfc": {
		"iso7816": ["d2760000850101", "A0000002471001"],
		"felica": ["8008"],
		"ndef": [{"uri": "https://Example.com/t/"}, {"uri": "geo:"}, {"mime": "Application/vnd.example.ticket"}, {"external": "Example.com:Ticket"}],
		"tech": [["IsoDep"], ["android.nfc.tech.NfcA", "mifareultralight"]],
		"required": true
	}}`), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"android.nfc.tech.NfcA", "android.nfc.tech.MifareUltralight"}; !slices.Equal(c.NFC.Tech[1], want) {
		t.Fatalf("tech %v", c.NFC.Tech)
	}

	// Not imported: nothing of it.
	none := *c
	none.permissionsFor([]string{"github.com/egoist/asuka/ui"})
	if m := none.androidManifest(false); strings.Contains(m, "NFC") || strings.Contains(m, "nfc") {
		t.Errorf("an app without the package has NFC:\n%s", m)
	}
	if _, ok := none.plistExtras()["com.apple.developer.nfc.readersession.iso7816.select-identifiers"]; ok || none.IOS.Usage["NFCReaderUsageDescription"] != "" {
		t.Error("an app without the package has iOS's NFC keys")
	}

	c.permissionsFor([]string{"github.com/egoist/asuka/ui", nfcPkg})
	m := c.androidManifest(false)
	for _, want := range []string{
		`<uses-permission android:name="android.permission.NFC" />`,
		`<uses-feature android:name="android.hardware.nfc" android:required="true" />`,
		`<action android:name="android.nfc.action.NDEF_DISCOVERED" />`,
		`<data android:scheme="https" android:host="example.com" android:pathPrefix="/t/" />`,
		`<data android:scheme="geo" />`,
		`<data android:mimeType="application/vnd.example.ticket" />`,
		`<data android:scheme="vnd.android.nfc" android:host="ext" android:pathPrefix="/example.com:ticket" />`,
		`<action android:name="android.nfc.action.TECH_DISCOVERED" />`,
		`<meta-data android:name="android.nfc.action.TECH_DISCOVERED" android:resource="@xml/asuka_nfc_tech" />`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("the manifest lacks %s:\n%s", want, m)
		}
	}
	// The filters are the Activity's.
	if a, f := strings.Index(m, "</activity>"), strings.Index(m, "NDEF_DISCOVERED"); f < 0 || f > a {
		t.Error("the NFC filters aren't in the Activity")
	}
	if c.IOS.Usage["NFCReaderUsageDescription"] == "" {
		t.Error("no NFCReaderUsageDescription")
	}
	if f, _ := c.entitlements["com.apple.developer.nfc.readersession.formats"].([]string); !slices.Equal(f, []string{"NDEF", "TAG"}) {
		t.Errorf("formats %v", c.entitlements["com.apple.developer.nfc.readersession.formats"])
	}
	x := c.plistExtras()
	if aids, _ := x["com.apple.developer.nfc.readersession.iso7816.select-identifiers"].([]string); !slices.Equal(aids, []string{"A0000002471001", "D2760000850101"}) {
		t.Errorf("AIDs %v", aids)
	}
	if codes, _ := x["com.apple.developer.nfc.readersession.felica.systemcodes"].([]string); !slices.Equal(codes, []string{"8008"}) {
		t.Errorf("system codes %v", codes)
	}
	res := t.TempDir()
	if ok, err := androidRes(c, res); err != nil || !ok {
		t.Fatalf("res: %v %v", ok, err)
	}
	xml, _ := os.ReadFile(filepath.Join(res, "xml", "asuka_nfc_tech.xml"))
	if !strings.Contains(string(xml), "<tech-list>\n        <tech>android.nfc.tech.IsoDep</tech>\n    </tech-list>") ||
		!strings.Contains(string(xml), "<tech>android.nfc.tech.MifareUltralight</tech>") {
		t.Errorf("tech lists:\n%s", xml)
	}
	ent := filepath.Join(t.TempDir(), "e.plist")
	os.WriteFile(ent, []byte("<plist><dict></dict></plist>"), 0o644)
	if missing := c.missingEntitlements(ent); !slices.Contains(missing, "NFC Tag Reading (asuka/sdk/nfc)") {
		t.Errorf("missing %v", missing)
	}

	// Without filters or required: optional NFC, no filters.
	plain := &appConfig{ID: "com.example.plain"}
	plain.permissionsFor([]string{nfcPkg})
	pm := plain.androidManifest(false)
	if !strings.Contains(pm, `android:name="android.hardware.nfc" android:required="false"`) || strings.Contains(pm, "DISCOVERED") {
		t.Errorf("plain:\n%s", pm)
	}
	if ok, _ := androidRes(plain, t.TempDir()); ok {
		t.Error("tech lists without any")
	}

	// The player has the permission and the entitlement, as any app could.
	p := playerConfig()
	if !slices.Contains(p.Android.Permissions, "android.permission.NFC") || p.entitlements["com.apple.developer.nfc.readersession.formats"] == nil {
		t.Error("the player can't read tags")
	}
}

func TestNFCConfigErrors(t *testing.T) {
	for _, js := range []string{
		`{"iso7816": ["xyz"]}`,
		`{"iso7816": ["A000"]}`,
		`{"felica": ["FFFF"]}`,
		`{"felica": ["80080"]}`,
		`{"ndef": [{}]}`,
		`{"ndef": [{"uri": "https://a", "mime": "a/b"}]}`,
		`{"ndef": [{"uri": "example.com"}]}`,
		`{"ndef": [{"mime": "text"}]}`,
		`{"ndef": [{"external": "thing"}]}`,
		`{"tech": [[]]}`,
		`{"tech": [["Felica"]]}`,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"nfc": `+js+`}`), 0o644)
		if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "asuka.json: nfc:") {
			t.Errorf("%s: %v", js, err)
		}
	}
}

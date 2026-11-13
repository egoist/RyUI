package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// testProfile is a decoded Mac provisioning profile's plist: a Developer
// ID one for TEAM.app (its entitlements, what Apple would grant with
// push, Sign in with Apple and App Attest on), allowing the certificate
// cert.
func testProfile(appID, cert string, extra string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>AppIDName</key>
	<string>Services</string>
	<key>CreationDate</key>
	<date>2026-01-02T03:04:05Z</date>
	<key>Platform</key>
	<array>
		<string>OSX</string>
	</array>
	<key>DeveloperCertificates</key>
	<array>
		<data>` + base64.StdEncoding.EncodeToString([]byte(cert)) + `</data>
	</array>
	<key>Entitlements</key>
	<dict>
		<key>com.apple.application-identifier</key>
		<string>` + appID + `</string>
		<key>com.apple.developer.team-identifier</key>
		<string>ABCDE12345</string>
		<key>keychain-access-groups</key>
		<array>
			<string>ABCDE12345.*</string>
		</array>
		<key>com.apple.developer.aps-environment</key>
		<string>production</string>
		<key>com.apple.developer.applesignin</key>
		<array>
			<string>Default</string>
		</array>
		<key>com.apple.developer.devicecheck.appattest-environment</key>
		<string>production</string>
	</dict>
	<key>ExpirationDate</key>
	<date>2099-01-02T03:04:05Z</date>
	<key>Name</key>
	<string>Services Developer ID</string>
	<key>ProvisionsAllDevices</key>
	<true/>
	<key>TeamIdentifier</key>
	<array>
		<string>ABCDE12345</string>
	</array>
	<key>TimeToLive</key>
	<integer>364</integer>
	<key>UUID</key>
	<string>11111111-2222-3333-4444-555555555555</string>` + extra + `
</dict>
</plist>
`
}

func certSHA(cert string) string {
	sum := sha1.Sum([]byte(cert))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func writeTestProfile(t *testing.T, appID, extra string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "test.provisionprofile")
	if err := os.WriteFile(f, []byte(testProfile(appID, "cert", extra)), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParsePlist(t *testing.T) {
	v, err := parsePlist([]byte(testProfile("ABCDE12345.dev.asuka.services", "cert", "")))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["Name"] != "Services Developer ID" || m["ProvisionsAllDevices"] != true || m["TimeToLive"] != int64(364) {
		t.Errorf("scalars: %v", m)
	}
	if d, _ := m["ExpirationDate"].(time.Time); d.Year() != 2099 {
		t.Errorf("date: %v", m["ExpirationDate"])
	}
	if c, _ := m["DeveloperCertificates"].([]any); len(c) != 1 || string(c[0].([]byte)) != "cert" {
		t.Errorf("data: %v", m["DeveloperCertificates"])
	}
	ents := m["Entitlements"].(map[string]any)
	if g, _ := ents["keychain-access-groups"].([]any); len(g) != 1 || g[0] != "ABCDE12345.*" {
		t.Errorf("an array in a dict: %v", ents)
	}
	for _, bad := range []string{"", "<html/>", "<plist><dict><key>a</key></dict></plist>", "<plist><dict><string>a</string></dict></plist>", "<plist><integer>x</integer></plist>"} {
		if _, err := parsePlist([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestReadMacProfile(t *testing.T) {
	p, err := readMacProfile(writeTestProfile(t, "ABCDE12345.dev.asuka.*", ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.name != "Services Developer ID" || p.uuid != "11111111-2222-3333-4444-555555555555" || p.team != "ABCDE12345" ||
		p.appID != "ABCDE12345.dev.asuka.*" || p.kind != "developer-id" || p.signed || p.expires.Year() != 2099 {
		t.Errorf("%+v", p)
	}
	if !slices.Equal(p.certs, []string{certSHA("cert")}) {
		t.Errorf("certs: %v", p.certs)
	}
	// The kinds: a development profile lists its Macs; a Mac App Store one
	// neither them nor all.
	m := map[string]any{"Name": "x", "TeamIdentifier": []any{"T"}, "Entitlements": map[string]any{"com.apple.application-identifier": "T.a"},
		"ProvisionedDevices": []any{"UUID"}}
	if p, err := macProfileFrom("x", m); err != nil || p.kind != "development" {
		t.Errorf("development: %v, %v", p, err)
	}
	delete(m, "ProvisionedDevices")
	if p, err := macProfileFrom("x", m); err != nil || p.kind != "app-store" {
		t.Errorf("app store: %v, %v", p, err)
	}
	// An iOS profile isn't the Mac's.
	m["Platform"] = []any{"iOS", "xrOS"}
	if _, err := macProfileFrom("x", m); err == nil || !strings.Contains(err.Error(), "iOS, xrOS, not the Mac") {
		t.Errorf("iOS: %v", err)
	}
	if _, err := macProfileFrom("x", map[string]any{"Name": "x"}); err == nil {
		t.Error("no entitlements: no error")
	}
}

// The profile's entitlements the app's packages need, with its app ID
// (a wildcard's becomes the app's) and team; an error for what it lacks.
func TestMacProfileEntitlements(t *testing.T) {
	cfg := &appConfig{ID: "dev.asuka.services"}
	cfg.IOS.Usage = map[string]string{"NSCameraUsageDescription": "x"}
	cfg.permissionsFor([]string{"github.com/egoist/asuka/push", "github.com/egoist/asuka/appleauth", "github.com/egoist/asuka/integrity", "github.com/egoist/asuka/agerange"})
	if got := macProfileNeeds(cfg); len(got) != 3 {
		t.Errorf("needs: %v", got)
	}
	p, err := readMacProfile(writeTestProfile(t, "ABCDE12345.dev.asuka.*", ""))
	if err != nil {
		t.Fatal(err)
	}
	ents, err := macEntitlements(cfg, p, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"com.apple.application-identifier":                      "ABCDE12345.dev.asuka.services",
		"com.apple.developer.team-identifier":                   "ABCDE12345",
		"com.apple.developer.aps-environment":                   "production",
		"com.apple.developer.devicecheck.appattest-environment": "production",
		"com.apple.security.device.camera":                      true,
	}
	for k, v := range want {
		if ents[k] != v {
			t.Errorf("%s: %v, want %v", k, ents[k], v)
		}
	}
	if s, _ := ents["com.apple.developer.applesignin"].([]any); len(s) != 1 || s[0] != "Default" {
		t.Errorf("applesignin: %v", ents["com.apple.developer.applesignin"])
	}
	for _, not := range []string{"aps-environment", "keychain-access-groups", "com.apple.developer.declared-age-range", "com.apple.security.app-sandbox"} {
		if _, ok := ents[not]; ok {
			t.Errorf("%s in %v", not, ents)
		}
	}
	out := plist(ents)
	if !strings.Contains(out, "<key>com.apple.developer.applesignin</key>\n\t<array>\n\t\t<string>Default</string>") {
		t.Errorf("plist:\n%s", out)
	}
	// The Mac App Store's sandbox.
	if ents, _ := macEntitlements(cfg, p, true); ents["com.apple.security.app-sandbox"] != true || ents["com.apple.security.network.client"] != true {
		t.Errorf("app store: %v", ents)
	}
	// A profile without push.
	delete(p.entitlements, "com.apple.developer.aps-environment")
	if _, err := macEntitlements(cfg, p, false); err == nil || !strings.Contains(err.Error(), "lacks Push Notifications (asuka/push)") {
		t.Errorf("no push: %v", err)
	}
	// An app that needs none: no profile entitlements but its ID and team.
	plain := &appConfig{ID: "dev.asuka.services"}
	if ents, err := macEntitlements(plain, p, false); err != nil || len(ents) != 2 {
		t.Errorf("no packages: %v, %v", ents, err)
	}
}

func TestPickMacProfile(t *testing.T) {
	f := writeTestProfile(t, "ABCDE12345.dev.asuka.*", "")
	ids := map[string]string{certSHA("cert"): "Developer ID Application: Someone (ABCDE12345)", certSHA("other"): "Apple Development: Someone (XYZ)"}
	p, err := pickMacProfile("ABCDE12345", "dev.asuka.services", f, false, ids)
	if err != nil || p.identity != certSHA("cert") {
		t.Fatalf("%+v, %v", p, err)
	}
	if _, err := pickMacProfile("OTHERTEAM1", "dev.asuka.services", f, false, ids); err == nil || !strings.Contains(err.Error(), "not dev.asuka.services") {
		t.Errorf("another team's: %v", err)
	}
	if _, err := pickMacProfile("", "com.example.app", f, false, ids); err == nil {
		t.Error("another app's: no error")
	}
	if _, err := pickMacProfile("", "dev.asuka.services", f, true, ids); err == nil || !strings.Contains(err.Error(), "-appstore needs a Mac App Store one") {
		t.Errorf("-appstore with a Developer ID profile: %v", err)
	}
	expired := writeTestProfile(t, "ABCDE12345.dev.asuka.services", "")
	b, _ := os.ReadFile(expired)
	os.WriteFile(expired, []byte(strings.Replace(string(b), "2099-01-02", "2001-01-02", 1)), 0o644)
	if _, err := pickMacProfile("", "dev.asuka.services", expired, false, ids); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired: %v", err)
	}

	// The identity: the profile's; -sign's if the profile allows it; ad
	// hoc for one that isn't Apple's.
	if id, _, err := macIdentity("", p, ids); err != nil || id != certSHA("cert") {
		t.Errorf("the profile's: %q, %v", id, err)
	}
	if id, _, err := macIdentity("-", p, ids); err != nil || id != "-" {
		t.Errorf("ad hoc: %q, %v", id, err)
	}
	p.signed = true
	if id, _, err := macIdentity("Developer ID Application", p, ids); err != nil || id != certSHA("cert") {
		t.Errorf("-sign Developer ID: %q, %v", id, err)
	}
	if _, _, err := macIdentity("Apple Development", p, ids); err == nil || !strings.Contains(err.Error(), "doesn't allow") {
		t.Errorf("-sign with a certificate the profile doesn't list: %v", err)
	}
	p.identity = ""
	if _, _, err := macIdentity("", p, ids); err == nil || !strings.Contains(err.Error(), "keychain") {
		t.Errorf("no certificate in the keychain: %v", err)
	}
	p.signed = false
	if id, _, err := macIdentity("", p, ids); err != nil || id != "-" {
		t.Errorf("a plain plist: %q, %v", id, err)
	}
}

func TestPickInstaller(t *testing.T) {
	out := `  1) 0D4B346FC35E0BF35C2D1DD135B7199AAC0D55E1 "Apple Development: A (9M4KM4PK87)"
  2) 1562A9A6562237BB0C349749995EA00563B734DF "3rd Party Mac Developer Installer: A (OTHER12345)"
  3) 2562A9A6562237BB0C349749995EA00563B734DF "Mac Installer Distribution: A (ABCDE12345)"
     3 valid identities found`
	if got := pickInstaller(out, "ABCDE12345"); got != "Mac Installer Distribution: A (ABCDE12345)" {
		t.Errorf("got %q", got)
	}
	if got := pickInstaller(out, "NONE"); got != "" {
		t.Errorf("another team: %q", got)
	}
}

// A real profile (Apple's CMS) decodes with security cms: the Mac's read
// as such, an iOS one refused as not the Mac's.
func TestDecodeInstalledProfile(t *testing.T) {
	var files []string
	for _, d := range profileDirs() {
		for _, pat := range []string{"*.provisionprofile", "*.mobileprovision"} {
			m, _ := filepath.Glob(filepath.Join(d, pat))
			files = append(files, m...)
		}
	}
	if len(files) == 0 {
		t.Skip("no provisioning profile installed")
	}
	for _, f := range files {
		data, signed, err := decodeProfile(f)
		if err != nil || !signed {
			t.Fatalf("%s: %v, signed %v", f, err, signed)
		}
		v, err := parsePlist(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		m := v.(map[string]any)
		p, err := macProfileFrom(f, m)
		mac := strings.HasSuffix(f, ".provisionprofile")
		if mac && (err != nil || p.team == "" || len(p.certs) == 0) {
			t.Errorf("%s: %+v, %v", f, p, err)
		}
		if !mac && (err == nil || !strings.Contains(err.Error(), "not the Mac")) {
			t.Errorf("%s (iOS): %v", f, err)
		}
	}
	t.Logf("%d profiles decoded", len(files))
}

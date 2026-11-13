package main

import (
	"os"
	"strings"
	"testing"
)

// A Mac app's signature carries the hardened runtime's entitlements for
// what its purpose strings ask for, and none it doesn't need.
func TestMacEntitlements(t *testing.T) {
	cfg := &appConfig{}
	cfg.permissionsFor([]string{"github.com/egoist/asuka/camera", "github.com/egoist/asuka/contacts", "github.com/egoist/asuka/location"})
	app := &goApp{cfg: cfg}
	f, err := writeMacEntitlements(app, t.TempDir())
	if err != nil || f == "" {
		t.Fatalf("entitlements: %q, %v", f, err)
	}
	b, _ := os.ReadFile(f)
	s := string(b)
	for _, want := range []string{"com.apple.security.device.camera", "com.apple.security.personal-information.addressbook",
		"com.apple.security.personal-information.location"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in\n%s", want, s)
		}
	}
	for _, not := range []string{"audio-input", "calendars", "aps-environment"} {
		if strings.Contains(s, not) {
			t.Errorf("%s in\n%s", not, s)
		}
	}
	if f, err := writeMacEntitlements(&goApp{cfg: &appConfig{}}, t.TempDir()); f != "" || err != nil {
		t.Errorf("an app that asks for nothing: %q, %v", f, err)
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccent(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "nui.json"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"accent": "#7c3aed", "android": {"dynamicColor": true}}`)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "-ldflags=-s -w -X github.com/egoist/nui/internal/boot.Accent=#7c3aed -X github.com/egoist/nui/internal/boot.DynamicColor=1"
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
	os.WriteFile(filepath.Join(dir, "nui.json"), []byte(`{"id": "com.example.notes", "scheme": "notes", "links": ["notes.example.com"],
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
	os.WriteFile(filepath.Join(dir, "nui.json"), []byte(`{"scheme": "my app"}`), 0o644)
	if _, err := loadConfig(dir); err == nil {
		t.Error("a bad scheme was taken")
	}
}

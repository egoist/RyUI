package main

import (
	"os"
	"path/filepath"
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

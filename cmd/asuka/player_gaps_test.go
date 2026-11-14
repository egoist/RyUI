package main

import (
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// gapsOf are the gaps an app has on target: the one it's built for, the
// ones asuka dev only mentions.
func gapsOf(cfg *appConfig, target string, deps ...string) (build string, notes []string) {
	u := gapUse{app: &goApp{cfg: cfg}, target: target, deps: deps}
	if g, _, ok := firstGap(u); ok {
		build = g.why
	}
	for _, g := range playerGaps {
		if g.build || !slices.Contains(g.targets, target) {
			continue
		}
		if what, ok := g.uses(u); ok {
			notes = append(notes, what)
		}
	}
	return build, notes
}

func TestPlayerGaps(t *testing.T) {
	plain := &appConfig{ID: "dev.example.app"}
	if b, n := gapsOf(plain, "ios", "github.com/egoist/asuka/ui"); b != "" || n != nil {
		t.Errorf("an app with none: built for %q, notes %q", b, n)
	}

	// Purchases: built on iOS, where the build can buy; mentioned on
	// Android, where no build from asuka dev can.
	if b, _ := gapsOf(plain, "ios", purchasePkg); b == "" {
		t.Error("ios: an app that buys runs in the player")
	}
	if b, n := gapsOf(plain, "android", purchasePkg); b != "" || len(n) != 1 {
		t.Errorf("android: built for %q, notes %q; want the player and a note", b, n)
	}

	// NFC: iOS's identifiers need the app's Info.plist on a phone; the
	// simulator has no NFC. Android's filters only open the app.
	nfc := &appConfig{ID: "dev.example.app", NFC: nfcConfig{ISO7816: []string{"A0000002471001"}, NDEF: []nfcFilter{{MIME: "text/plain"}}}}
	if b, _ := gapsOf(nfc, "ios-device", nfcPkg); b == "" {
		t.Error("ios-device: an app with ISO 7816 identifiers runs in the player")
	}
	if b, _ := gapsOf(nfc, "ios", nfcPkg); b != "" {
		t.Errorf("ios simulator: built for %q, but it has no NFC", b)
	}
	if b, n := gapsOf(nfc, "android", nfcPkg); b != "" || len(n) != 1 {
		t.Errorf("android: built for %q, notes %q; want the player and a note on tags", b, n)
	}
	if b, n := gapsOf(nfc, "android"); b != "" || n != nil {
		t.Errorf("an app without asuka/sdk/nfc: built for %q, notes %q", b, n)
	}

	// Shared files: on the Mac in the simulator, on the device otherwise.
	files := &appConfig{ID: "dev.example.app", ShareTarget: shareTarget{on: true, Types: []string{"image/*"}}}
	text := &appConfig{ID: "dev.example.app", ShareTarget: shareTarget{on: true, Types: []string{"text"}}}
	for _, target := range []string{"android", "ios-device"} {
		if b, _ := gapsOf(files, target); b == "" {
			t.Errorf("%s: an app that takes files runs in the player", target)
		}
		if b, _ := gapsOf(text, target); b != "" {
			t.Errorf("%s: an app that takes text is built for %q", target, b)
		}
	}
	if b, _ := gapsOf(files, "ios"); b != "" {
		t.Errorf("ios simulator: an app that takes files is built for %q", b)
	}

	// What makes the app itself: mentioned.
	links := &appConfig{ID: "dev.example.app", Scheme: "example"}
	links.IOS.Usage = map[string]string{"NSCameraUsageDescription": "To scan."}
	if b, n := gapsOf(links, "ios"); b != "" || len(n) != 2 {
		t.Errorf("scheme and ios.usage: built for %q, notes %q; want two notes", b, n)
	}
}

func TestRefers(t *testing.T) {
	for src, want := range map[string]bool{
		`package a; import "github.com/egoist/asuka/sdk/push"; func f() { push.Rewrite(nil) }`: true,
		`package a; import p "github.com/egoist/asuka/sdk/push"; var _ = p.Rewrite`:            true,
		`package a; import "github.com/egoist/asuka/sdk/push"; func f() { push.Register() }`:   false,
		`package a; import "other/push"; func f() { push.Rewrite(nil) }`:                       false,
		`package a; import . "github.com/egoist/asuka/sdk/push"; func f() { Rewrite(nil) }`:    true,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "a.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := refers(f, pushPkg, "Rewrite"); got != want {
			t.Errorf("%s: refers %v, want %v", src, got, want)
		}
	}
}

// examples/services calls push.Rewrite; examples/counter doesn't import
// asuka/sdk/push at all.
func TestCalls(t *testing.T) {
	if !calls("../../examples/services", ".", "ios", pushPkg, "Rewrite") {
		t.Error("examples/services: push.Rewrite not found")
	}
	if calls("../../examples/counter", ".", "ios", pushPkg, "Rewrite") {
		t.Error("examples/counter: push.Rewrite found")
	}
}

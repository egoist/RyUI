package macos

import (
	"bytes"
	"os"
	"testing"
)

// Typed native modules' runtime is one file in the iOS host and here.
func TestNativeModulesShared(t *testing.T) {
	ios, err := os.ReadFile("../ios/Sources/AsukaHost/NativeModules.swift")
	if err != nil {
		t.Fatal(err)
	}
	mac, err := Files.ReadFile("Sources/AsukaMac/NativeModules.swift")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ios, mac) {
		t.Error("platform/macos/Sources/AsukaMac/NativeModules.swift isn't the iOS host's: copy it")
	}
}

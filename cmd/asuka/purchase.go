package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// asuka/sdk/purchase's StoreKit configuration file (asuka.json's
// ios.storekit): asuka dev's build for the simulator carries it, as
// AsukaStoreKit.storekit, and launched by asuka dev the host sells from it
// (platform/ios's AsukaStoreKitTest), as an app run from Xcode with the
// file in its scheme does. Other builds never have it: they buy from the
// App Store's sandbox (TestFlight, a sandbox account).

// storeKitName is the file's name in the bundle, as the host looks for it.
const storeKitName = "AsukaStoreKit.storekit"

// storeKitLaunchEnv is what asuka dev launches a simulator build with an
// ios.storekit with: StoreKitTest's session loads XCTest as the system's
// (/System/Library/Frameworks), which the simulator has only from Xcode,
// found as a test run finds it (DYLD_FRAMEWORK_PATH: Xcode's frameworks
// for the simulator). nil without the file, or without Xcode's.
func (c *appConfig) storeKitLaunchEnv() []string {
	if c.IOS.StoreKit == "" {
		return nil
	}
	dev, err := exec.Command("xcode-select", "-p").Output()
	if err != nil {
		return nil
	}
	lib := filepath.Join(strings.TrimSpace(string(dev)), "Platforms", "iPhoneSimulator.platform", "Developer", "Library")
	if _, err := os.Stat(filepath.Join(lib, "Frameworks", "XCTest.framework", "XCTest")); err != nil {
		return nil
	}
	return []string{"SIMCTL_CHILD_DYLD_FRAMEWORK_PATH=" + filepath.Join(lib, "Frameworks") + ":" + filepath.Join(lib, "PrivateFrameworks")}
}

// copyStoreKit puts the app's StoreKit configuration file, if it has
// one, in a simulator build's bundle.
func (c *appConfig) copyStoreKit(bundle string) error {
	if c.IOS.StoreKit == "" {
		return nil
	}
	src := c.IOS.StoreKit
	if !filepath.IsAbs(src) {
		src = filepath.Join(c.dir, src)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("asuka.json's ios.storekit: %v", err)
	}
	return os.WriteFile(filepath.Join(bundle, storeKitName), b, 0o644)
}

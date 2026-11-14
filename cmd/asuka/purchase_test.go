package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// An app that imports asuka/sdk/purchase gets Play Billing's module and
// permission, and the Mac's StoreKit; one that doesn't, none of them.
func TestPurchaseFeature(t *testing.T) {
	var c appConfig
	c.permissionsFor([]string{"github.com/egoist/asuka/ui", "github.com/egoist/asuka/sdk/purchase"})
	if !slices.Contains(c.androidModules, "purchase") || !slices.Contains(c.Android.Permissions, "com.android.vending.BILLING") ||
		!slices.Equal(c.macSwift, []string{"ASUKA_PURCHASE"}) {
		t.Fatalf("modules %v, permissions %v, macSwift %v", c.androidModules, c.Android.Permissions, c.macSwift)
	}
	var none appConfig
	none.permissionsFor([]string{"github.com/egoist/asuka/ui"})
	if slices.Contains(none.androidModules, "purchase") || len(none.macSwift) > 0 {
		t.Fatalf("modules %v, macSwift %v", none.androidModules, none.macSwift)
	}
	found := 0
	for _, f := range iosFeatures {
		if f.flag == "ASUKA_PURCHASE" {
			found++
		}
	}
	if found == 0 {
		t.Fatal("no ASUKA_PURCHASE in iosFeatures")
	}
}

// ios.storekit goes into a simulator build's bundle, as the host looks
// for it.
func TestCopyStoreKit(t *testing.T) {
	dir, bundle := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(dir, "Products.storekit"), []byte(`{"products":[]}`), 0o644)
	c := &appConfig{dir: dir}
	if err := c.copyStoreKit(bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bundle, storeKitName)); err == nil {
		t.Fatal("copied without ios.storekit")
	}
	c.IOS.StoreKit = "Products.storekit"
	if err := c.copyStoreKit(bundle); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(bundle, storeKitName)); string(b) != `{"products":[]}` {
		t.Fatalf("copied %q", b)
	}
	c.IOS.StoreKit = "missing.storekit"
	if err := c.copyStoreKit(bundle); err == nil {
		t.Fatal("no error for a missing file")
	}
}

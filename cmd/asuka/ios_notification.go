package main

import (
	"os"
	"path/filepath"
	"strings"
)

// The Notification Service Extension (push.Rewrite): a binary of its own
// in the app's PlugIns, which iOS starts for a push with mutable-content
// to change it before it shows. It runs the app's Go code: as the share
// sheet does (ios_framework.go), it links the app's framework, whose
// AsukaNotificationService is its principal class; main runs (setting the
// rewrite) with a root that shows nothing. What the app keeps is the
// same there: its data is in the App Group they share (store.Dir), its
// keychain items in the app's group.

const notificationExe = "AsukaNotification"

// rewriteSymbol is push.Rewrite's: the app's Go calls it if the archive
// has it.
const rewriteSymbol = "github.com/egoist/asuka/push.Rewrite"

// notificationID is the extension's bundle ID, which a device build needs
// a provisioning profile for, as the app's.
func (c *appConfig) notificationID() string { return c.ID + ".notification" }

// notificationEntitlements are the linker flags that give the extension
// its entitlements on the simulator, as the app's: the App Group and the
// app's keychain group.
func (t *iosToolchain) notificationEntitlements(work string, cfg *appConfig) ([]string, error) {
	if !strings.HasSuffix(t.target, "-simulator") {
		return nil, nil
	}
	m := map[string]any{
		"application-identifier": "ASUKASIMULA." + cfg.notificationID(),
		"keychain-access-groups": []string{"ASUKASIMULA." + cfg.ID},
	}
	if g := cfg.appGroup(); g != "" {
		m["com.apple.security.application-groups"] = []string{g}
	}
	path := filepath.Join(work, "notification.entitlements")
	if err := os.WriteFile(path, []byte(plist(m)), 0o644); err != nil {
		return nil, err
	}
	return []string{"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", path}, nil
}

// buildNotification links the extension's executable with the app's
// framework (fw), returning its path.
func (t *iosToolchain) buildNotification(work string, cfg *appConfig, fw string) (string, error) {
	ents, err := t.notificationEntitlements(work, cfg)
	if err != nil {
		return "", err
	}
	return t.linkExtension(work, fw, notificationExe, ents)
}

// notificationInfo is the extension's Info.plist, with the app's name,
// version, platform and build keys from app (the app's Info.plist).
func (c *appConfig) notificationInfo(app map[string]any) map[string]any {
	m := map[string]any{
		"CFBundleIdentifier":            c.notificationID(),
		"CFBundleExecutable":            notificationExe,
		"CFBundlePackageType":           "XPC!",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"AsukaNotificationService":      true,
		"AsukaAppID":                    c.ID,
		"NSExtension": map[string]any{
			"NSExtensionPointIdentifier": "com.apple.usernotifications.service",
			"NSExtensionPrincipalClass":  "AsukaNotificationService",
		},
	}
	if g := c.appGroup(); g != "" {
		m["AsukaAppGroup"] = g
	}
	for k, v := range app {
		switch k {
		case "CFBundleName", "CFBundleDisplayName", "CFBundleShortVersionString", "CFBundleVersion", "MinimumOSVersion", "CFBundleSupportedPlatforms", "UIDeviceFamily", "UIRequiredDeviceCapabilities":
			m[k] = v
		default:
			if strings.HasPrefix(k, "DT") {
				m[k] = v
			}
		}
	}
	return m
}

// embedNotification puts the extension in bundle's PlugIns, returning its
// path. It's signed before the app, whose signature seals it.
func embedNotification(bundle, exe string, info map[string]any) (string, error) {
	appex := filepath.Join(bundle, "PlugIns", notificationExe+".appex")
	if err := os.MkdirAll(appex, 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(appex, notificationExe), data, 0o755); err != nil {
		return "", err
	}
	return appex, os.WriteFile(filepath.Join(appex, "Info.plist"), []byte(plist(info)), 0o644)
}

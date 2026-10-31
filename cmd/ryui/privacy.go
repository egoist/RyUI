package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// privacyAPIs are the APIs Apple asks an app to give a reason for using
// (its privacy manifest, PrivacyInfo.xcprivacy; App Store Connect turns
// down an app without one, ITMS-91053), each category's with the reason
// that's RyUI's: a C function the binary imports, or an Objective-C class
// or method it names. Go's runtime alone uses two: mach_absolute_time
// (its clock) and stat (os.Stat, opening files).
var privacyAPIs = []struct {
	category, reason string
	symbols          []string // imported C functions and classes
	selectors        []string // Objective-C methods, by name
}{
	// C617.1: timestamps of files in the app's container.
	{"NSPrivacyAccessedAPICategoryFileTimestamp", "C617.1",
		[]string{"stat", "fstat", "fstatat", "lstat", "getattrlist", "getattrlistbulk", "fgetattrlist", "getattrlistat",
			"NSFileModificationDate", "NSFileCreationDate"}, // the image cache's, for which to drop first
		[]string{"creationDate", "modificationDate", "fileModificationDate", "contentModificationDateKey", "creationDateKey"}},
	// 35F9.1: the time elapsed between events in the app.
	{"NSPrivacyAccessedAPICategorySystemBootTime", "35F9.1",
		[]string{"mach_absolute_time"}, []string{"systemUptime"}},
	// E174.1: whether there's room to write files (SQLite asks).
	{"NSPrivacyAccessedAPICategoryDiskSpace", "E174.1",
		[]string{"statfs", "statvfs", "fstatfs", "fstatvfs", "NSFileSystemFreeSize", "NSFileSystemSize"},
		[]string{"volumeAvailableCapacityKey", "volumeAvailableCapacityForImportantUsageKey", "volumeTotalCapacityKey", "systemFreeSize", "systemSize"}},
	// CA92.1: values only the app reads and writes.
	{"NSPrivacyAccessedAPICategoryUserDefaults", "CA92.1",
		[]string{"OBJC_CLASS_$_NSUserDefaults"}, nil},
	// 54BD.1: the interface fit to the keyboards in use.
	{"NSPrivacyAccessedAPICategoryActiveKeyboards", "54BD.1",
		nil, []string{"activeInputModes"}},
}

// privacyManifest is the PrivacyInfo.xcprivacy for the executable bin:
// the app's own (ios/PrivacyInfo.xcprivacy beside its main package, for
// what it collects or tracks) if it has one, else the reasons for the
// APIs bin uses, no tracking and no data collected (RyUI collects none).
func privacyManifest(appDir, bin string) ([]byte, error) {
	if own, err := os.ReadFile(filepath.Join(appDir, "ios", "PrivacyInfo.xcprivacy")); err == nil {
		return own, nil
	}
	out, err := exec.Command("xcrun", "nm", "-u", bin).Output()
	if err != nil {
		return nil, err
	}
	imported := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		s := strings.TrimPrefix(strings.TrimSpace(l), "_")
		if i := strings.IndexByte(s, '$'); i > 0 && !strings.HasPrefix(s, "OBJC_") {
			s = s[:i] // stat$INODE64
		}
		imported[s] = true
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		return nil, err
	}
	return []byte(plist(map[string]any{
		"NSPrivacyTracking":           false,
		"NSPrivacyTrackingDomains":    []string{},
		"NSPrivacyCollectedDataTypes": []any{},
		"NSPrivacyAccessedAPITypes":   privacyAccessed(imported, data),
	})), nil
}

// privacyAccessed is the manifest's NSPrivacyAccessedAPITypes for a
// binary that imports those symbols and holds data.
func privacyAccessed(imported map[string]bool, data []byte) []any {
	accessed := []any{}
	for _, api := range privacyAPIs {
		used := false
		for _, s := range api.symbols {
			used = used || imported[s] || imported[s+"64"]
		}
		for _, s := range api.selectors {
			// A method's name is a string of its own in the binary.
			used = used || bytes.Contains(data, []byte("\x00"+s+"\x00")) || bytes.Contains(data, []byte("\x00"+s+":\x00"))
		}
		if used {
			accessed = append(accessed, map[string]any{
				"NSPrivacyAccessedAPIType":        api.category,
				"NSPrivacyAccessedAPITypeReasons": []string{api.reason},
			})
		}
	}
	return accessed
}

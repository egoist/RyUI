package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// appConfig is an app's nui.json: what the stores and the platforms need
// to know about it. Every field is optional.
//
//	{
//	  "name": "Counter",
//	  "id": "com.example.counter",
//	  "version": "1.2.0",
//	  "build": 7,
//	  "icon": "icon.png",
//	  "orientations": ["portrait"],
//	  "ios": {
//	    "team": "ABCDE12345",
//	    "usage": {"NSCameraUsageDescription": "To scan receipts"}
//	  },
//	  "android": {
//	    "permissions": ["android.permission.CAMERA"],
//	    "keystore": "upload.jks",
//	    "keyAlias": "upload"
//	  }
//	}
type appConfig struct {
	// Name is the name under the icon (default: the directory's name).
	Name string `json:"name"`
	// ID is the bundle ID and application ID (default: dev.nui.<dir>).
	ID string `json:"id"`
	// Version is the version users see (default 1.0); Build counts the
	// builds uploaded, and must grow with each (default 1).
	Version string `json:"version"`
	Build   int    `json:"build"`
	// Icon is a square PNG, 1024×1024, without transparency for iOS.
	Icon string `json:"icon"`
	// Orientations: portrait, landscape (default both).
	Orientations []string `json:"orientations"`
	IOS          struct {
		Team string `json:"team"`
		// Profile is a provisioning profile's path, name or UUID (default:
		// the best match installed for the ID and distribution).
		Profile string `json:"profile"`
		// Usage are the purpose strings for protected resources
		// (NSCameraUsageDescription, ...).
		Usage map[string]string `json:"usage"`
		// InfoPlist adds string keys to Info.plist.
		InfoPlist map[string]string `json:"infoPlist"`
	} `json:"ios"`
	Android struct {
		Permissions []string `json:"permissions"`
		// The upload key; its passwords come from NUI_KEYSTORE_PASSWORD
		// and NUI_KEY_PASSWORD (default: the keystore's).
		Keystore string `json:"keystore"`
		KeyAlias string `json:"keyAlias"`
	} `json:"android"`
}

var idPart = regexp.MustCompile(`[^A-Za-z0-9]+`)

// loadConfig reads dir/nui.json, filling in the defaults.
func loadConfig(dir string) (*appConfig, error) {
	c := &appConfig{}
	data, err := os.ReadFile(filepath.Join(dir, "nui.json"))
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("nui.json: %v", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	base := filepath.Base(dir)
	if c.Name == "" {
		c.Name = base
	}
	if c.ID == "" {
		c.ID = "dev.nui." + strings.Trim(strings.ToLower(idPart.ReplaceAllString(base, "-")), "-")
	}
	if c.Version == "" {
		c.Version = "1.0"
	}
	if c.Build <= 0 {
		c.Build = 1
	}
	if len(c.Orientations) == 0 {
		c.Orientations = []string{"portrait", "landscape"}
	}
	for _, o := range c.Orientations {
		if o != "portrait" && o != "landscape" {
			return nil, fmt.Errorf("nui.json: orientation %q (want portrait or landscape)", o)
		}
	}
	if c.Icon != "" && !filepath.IsAbs(c.Icon) {
		c.Icon = filepath.Join(dir, c.Icon)
	}
	if c.Android.Keystore != "" && !filepath.IsAbs(c.Android.Keystore) {
		c.Android.Keystore = filepath.Join(dir, c.Android.Keystore)
	}
	return c, nil
}

func (c *appConfig) allows(o string) bool {
	for _, x := range c.Orientations {
		if x == o {
			return true
		}
	}
	return false
}

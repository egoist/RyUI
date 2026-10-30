package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
//	  "accent": "#7c3aed",
//	  "orientations": ["portrait"],
//	  "ios": {
//	    "team": "ABCDE12345",
//	    "usage": {"NSCameraUsageDescription": "To scan receipts"}
//	  },
//	  "android": {
//	    "permissions": ["android.permission.CAMERA"],
//	    "keystore": "upload.jks",
//	    "keyAlias": "upload",
//	    "dynamicColor": true
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
	// Accent is the app's accent color, "#rrggbb" or "#rrggbbaa": the
	// tint color on iOS; on Android, the seed of the Material 3 color
	// scheme (Android 12 and later; before, nui's own uses of the accent
	// take it). Default: the platform's.
	Accent string `json:"accent"`
	// Scheme is the app's URL scheme: myapp://anything opens it
	// (ui.OnOpenURL gets the URL).
	Scheme string `json:"scheme"`
	// Links are the web domains whose https links open the app: iOS's
	// universal links (the domain serves apple-app-site-association) and
	// Android's App Links (assetlinks.json).
	Links   []string `json:"links"`
	Android struct {
		// DynamicColor takes the colors from the user's wallpaper where
		// the system has them (Android 12 and later), as Material apps do;
		// the accent is the seed elsewhere.
		DynamicColor bool     `json:"dynamicColor"`
		Permissions  []string `json:"permissions"`
		// The upload key; its passwords come from NUI_KEYSTORE_PASSWORD
		// and NUI_KEY_PASSWORD (default: the keystore's).
		Keystore string `json:"keystore"`
		KeyAlias string `json:"keyAlias"`
	} `json:"android"`
}

var (
	idPart   = regexp.MustCompile(`[^A-Za-z0-9]+`)
	hexColor = regexp.MustCompile(`^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	scheme   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*$`)
	domain   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
)

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
	if c.Accent != "" {
		if !hexColor.MatchString(c.Accent) {
			return nil, fmt.Errorf("nui.json: accent %q (want #rrggbb or #rrggbbaa)", c.Accent)
		}
	}
	if c.Scheme != "" && !scheme.MatchString(c.Scheme) {
		return nil, fmt.Errorf("nui.json: scheme %q (want letters, digits, + - .; a letter first)", c.Scheme)
	}
	for _, l := range c.Links {
		if !domain.MatchString(l) {
			return nil, fmt.Errorf("nui.json: link domain %q (want a host name, as example.com)", l)
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

// ldflags is the go build -ldflags for the app: what of nui.json its Go
// code reads (internal/boot), and, for a release, no symbol tables.
func (c *appConfig) ldflags(release bool) string {
	var f []string
	if release {
		f = append(f, "-s", "-w")
	}
	if c != nil && c.Accent != "" {
		f = append(f, "-X", "github.com/egoist/nui/internal/boot.Accent="+c.Accent)
	}
	if c != nil && c.Android.DynamicColor {
		f = append(f, "-X", "github.com/egoist/nui/internal/boot.DynamicColor=1")
	}
	return "-ldflags=" + strings.Join(f, " ")
}

// urlTypes is Info.plist's CFBundleURLTypes for the scheme.
func (c *appConfig) urlTypes() []map[string]any {
	if c == nil || c.Scheme == "" {
		return nil
	}
	return []map[string]any{{"CFBundleURLName": c.ID, "CFBundleURLSchemes": []string{c.Scheme}}}
}

// associatedDomains is the entitlement for the links (simulator: in the
// developer mode, which needs no CDN's copy of the association file).
func (c *appConfig) associatedDomains(simulator bool) []string {
	var out []string
	for _, l := range c.Links {
		d := "applinks:" + l
		if simulator {
			d += "?mode=developer"
		}
		out = append(out, d)
	}
	return out
}

// androidManifest is the app's manifest, merged over the host's: its
// permissions, and the intent filters for its scheme and links. A debug
// build's also lets nui dev serve over plain HTTP.
// featurePermissions are what nui's packages need of the platform: an
// app that imports one is given them (and, on iOS, a purpose string,
// which nui.json's ios.usage can say better).
var featurePermissions = []struct {
	pkg     string
	android []string
	usage   map[string]string
}{
	{"github.com/egoist/nui/location", []string{"android.permission.ACCESS_COARSE_LOCATION", "android.permission.ACCESS_FINE_LOCATION"},
		map[string]string{"NSLocationWhenInUseUsageDescription": "To show where you are."}},
	{"github.com/egoist/nui/notify", []string{"android.permission.POST_NOTIFICATIONS"}, nil},
	// The system's camera app takes the photo: no camera permission.
	{"github.com/egoist/nui/camera", nil, map[string]string{"NSCameraUsageDescription": "To take a photo."}},
}

// permissionsFor adds the permissions the packages deps (an app's
// imports, all the way down) need. nil deps: every package's (the player
// runs any app).
func (c *appConfig) permissionsFor(deps []string) {
	for _, f := range featurePermissions {
		if deps != nil && !slices.Contains(deps, f.pkg) {
			continue
		}
		for _, p := range f.android {
			if !slices.Contains(c.Android.Permissions, p) {
				c.Android.Permissions = append(c.Android.Permissions, p)
			}
		}
		for k, v := range f.usage {
			if c.IOS.Usage == nil {
				c.IOS.Usage = map[string]string{}
			}
			if c.IOS.Usage[k] == "" {
				c.IOS.Usage[k] = v
			}
		}
	}
}

func (c *appConfig) androidManifest(debug bool) string {
	var b strings.Builder
	b.WriteString("<manifest xmlns:android=\"http://schemas.android.com/apk/res/android\">\n")
	for _, p := range c.Android.Permissions {
		fmt.Fprintf(&b, "    <uses-permission android:name=\"%s\" />\n", html.EscapeString(p))
	}
	b.WriteString("    <application")
	if debug {
		b.WriteString(" android:usesCleartextTraffic=\"true\"")
	}
	b.WriteString(">\n        <activity android:name=\"dev.nui.NuiActivity\" android:exported=\"true\">\n")
	filter := func(verify bool, data string) {
		v := ""
		if verify {
			v = " android:autoVerify=\"true\""
		}
		fmt.Fprintf(&b, `            <intent-filter%s>
                <action android:name="android.intent.action.VIEW" />
                <category android:name="android.intent.category.DEFAULT" />
                <category android:name="android.intent.category.BROWSABLE" />
                %s
            </intent-filter>
`, v, data)
	}
	if c.Scheme != "" {
		filter(false, fmt.Sprintf(`<data android:scheme="%s" />`, html.EscapeString(c.Scheme)))
	}
	for _, l := range c.Links {
		filter(true, fmt.Sprintf(`<data android:scheme="https" android:host="%s" />`, html.EscapeString(l)))
	}
	b.WriteString("        </activity>\n    </application>\n</manifest>\n")
	return b.String()
}

func (c *appConfig) allows(o string) bool {
	for _, x := range c.Orientations {
		if x == o {
			return true
		}
	}
	return false
}

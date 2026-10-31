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

// appConfig is an app's ryui.json: what the stores and the platforms need
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
	// ID is the bundle ID and application ID (default: dev.ryui.<dir>).
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
		// ShareProfile is the share extension's (shareTarget), whose ID is
		// the app's plus ".share" (default: the best match installed).
		ShareProfile string `json:"shareProfile"`
		// Usage are the purpose strings for protected resources
		// (NSCameraUsageDescription, ...).
		Usage map[string]string `json:"usage"`
		// InfoPlist adds string keys to Info.plist.
		InfoPlist map[string]string `json:"infoPlist"`
		// IPhoneOnly leaves iPad out: the app runs there as an iPhone app,
		// in a phone-sized window. By default it's an iPad app too, in
		// Split View and Stage Manager, at any size the user gives it.
		IPhoneOnly bool `json:"iphoneOnly"`
	} `json:"ios"`
	// Accent is the app's accent color, "#rrggbb" or "#rrggbbaa": the
	// tint color on iOS; on Android, the seed of the Material 3 color
	// scheme (Android 12 and later; before, RyUI's own uses of the accent
	// take it). Default: the platform's.
	Accent string `json:"accent"`
	// Scheme is the app's URL scheme: myapp://anything opens it
	// (ui.OnOpenURL gets the URL).
	Scheme string `json:"scheme"`
	// Links are the web domains whose https links open the app: iOS's
	// universal links (the domain serves apple-app-site-association) and
	// Android's App Links (assetlinks.json).
	Links []string `json:"links"`
	// ShareTarget offers the app in other apps' share sheets, for text
	// and links (iOS: a share extension that opens the app; Android: an
	// ACTION_SEND intent filter); ui.OnShare gets what's shared.
	ShareTarget bool `json:"shareTarget"`
	// androidModules are the host's optional Gradle modules the app's
	// packages need (featurePermissions).
	androidModules []string
	// iosTrue and androidApp: featurePermissions' Info.plist keys set to
	// true, and application elements of the Android manifest.
	iosTrue    []string
	androidApp []string
	Android    struct {
		// DynamicColor takes the colors from the user's wallpaper where
		// the system has them (Android 12 and later), as Material apps do;
		// the accent is the seed elsewhere.
		DynamicColor bool     `json:"dynamicColor"`
		Permissions  []string `json:"permissions"`
		// The upload key; its passwords come from RYUI_KEYSTORE_PASSWORD
		// and RYUI_KEY_PASSWORD (default: the keystore's).
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

// loadConfig reads dir/ryui.json, filling in the defaults.
func loadConfig(dir string) (*appConfig, error) {
	c := &appConfig{}
	data, err := os.ReadFile(filepath.Join(dir, "ryui.json"))
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("ryui.json: %v", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	base := filepath.Base(dir)
	if c.Name == "" {
		c.Name = base
	}
	if c.ID == "" {
		c.ID = "dev.ryui." + strings.Trim(strings.ToLower(idPart.ReplaceAllString(base, "-")), "-")
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
			return nil, fmt.Errorf("ryui.json: orientation %q (want portrait or landscape)", o)
		}
	}
	if c.Accent != "" {
		if !hexColor.MatchString(c.Accent) {
			return nil, fmt.Errorf("ryui.json: accent %q (want #rrggbb or #rrggbbaa)", c.Accent)
		}
	}
	if c.Scheme != "" && !scheme.MatchString(c.Scheme) {
		return nil, fmt.Errorf("ryui.json: scheme %q (want letters, digits, + - .; a letter first)", c.Scheme)
	}
	for _, l := range c.Links {
		if !domain.MatchString(l) {
			return nil, fmt.Errorf("ryui.json: link domain %q (want a host name, as example.com)", l)
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

// ldflags is the go build -ldflags for the app: what of ryui.json its Go
// code reads (internal/boot), and, for a release, no symbol tables.
func (c *appConfig) ldflags(release bool) string {
	var f []string
	if release {
		f = append(f, "-s", "-w")
	}
	if c != nil && c.Accent != "" {
		f = append(f, "-X", "github.com/egoist/ryui/internal/boot.Accent="+c.Accent)
	}
	if c != nil && c.Android.DynamicColor {
		f = append(f, "-X", "github.com/egoist/ryui/internal/boot.DynamicColor=1")
	}
	return "-ldflags=" + strings.Join(f, " ")
}

// urlTypes is Info.plist's CFBundleURLTypes for the scheme, and on iOS
// the share extension's (shareScheme).
func (c *appConfig) urlTypes(ios bool) []any {
	if c == nil {
		return nil
	}
	var schemes []string
	if c.Scheme != "" {
		schemes = append(schemes, c.Scheme)
	}
	if ios && c.ShareTarget {
		schemes = append(schemes, c.shareScheme())
	}
	if len(schemes) == 0 {
		return nil
	}
	return []any{map[string]any{"CFBundleURLName": c.ID, "CFBundleURLSchemes": schemes}}
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
// build's also lets ryui dev serve over plain HTTP.
// featurePermissions are what RyUI's packages need of the platform: an
// app that imports one is given them (and, on iOS, a purpose string,
// which ryui.json's ios.usage can say better), and the Android host's
// optional Gradle modules (hosts/android/ryui-<name>) it needs.
var featurePermissions = []struct {
	pkg     string
	android []string // permissions; "name:28": up to Android 9 (API 28) only
	usage   map[string]string
	modules []string
	// iosTrue are Info.plist keys set to true; androidApp, elements of the
	// Android manifest's application (a service, a receiver).
	iosTrue    []string
	androidApp []string
}{
	{"github.com/egoist/ryui/location", []string{"android.permission.ACCESS_COARSE_LOCATION", "android.permission.ACCESS_FINE_LOCATION"},
		map[string]string{"NSLocationWhenInUseUsageDescription": "To show where you are."}, nil, nil, nil},
	{"github.com/egoist/ryui/notify", []string{"android.permission.POST_NOTIFICATIONS"}, nil, nil, nil, nil},
	// camera.View's preview is the app's own: the camera permission (and
	// with it declared, the system's camera app needs it granted too).
	{"github.com/egoist/ryui/camera", []string{"android.permission.CAMERA"}, map[string]string{"NSCameraUsageDescription": "To take a photo."}, nil, nil, nil},
	{"github.com/egoist/ryui/camera/record", []string{"android.permission.RECORD_AUDIO"}, map[string]string{"NSMicrophoneUsageDescription": "To record a video's sound."}, nil, nil, nil},
	{"github.com/egoist/ryui/network", []string{"android.permission.ACCESS_NETWORK_STATE"}, nil, nil, nil, nil},
	// MediaStore needs no permission from Android 10; before, writing to
	// the shared folders does. Files in Documents show in iOS's Files.
	{"github.com/egoist/ryui/library", []string{"android.permission.WRITE_EXTERNAL_STORAGE:28"},
		map[string]string{"NSPhotoLibraryAddUsageDescription": "To save videos and photos to your library."}, nil,
		[]string{"UIFileSharingEnabled", "LSSupportsOpeningDocumentsInPlace"}, nil},
	// A foreground service of the data sync type, its notification, the
	// CPU kept awake.
	{"github.com/egoist/ryui/background", []string{"android.permission.FOREGROUND_SERVICE", "android.permission.FOREGROUND_SERVICE_DATA_SYNC",
		"android.permission.POST_NOTIFICATIONS", "android.permission.WAKE_LOCK"}, nil, nil, nil,
		[]string{`<service android:name="dev.ryui.RyUIWorkService" android:exported="false" android:foregroundServiceType="dataSync" />`}},
	// ML Kit's barcode model: about 2 MB, only in the apps that scan.
	{"github.com/egoist/ryui/scan", nil, nil, []string{"scan"}, nil, nil},
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
		for _, m := range f.modules {
			if !slices.Contains(c.androidModules, m) {
				c.androidModules = append(c.androidModules, m)
			}
		}
		for _, k := range f.iosTrue {
			if !slices.Contains(c.iosTrue, k) {
				c.iosTrue = append(c.iosTrue, k)
			}
		}
		for _, x := range f.androidApp {
			if !slices.Contains(c.androidApp, x) {
				c.androidApp = append(c.androidApp, x)
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

// gradleModules is the Gradle property naming the app's optional modules.
func (c *appConfig) gradleModules() string {
	return "-PryuiModules=" + strings.Join(c.androidModules, ",")
}

func (c *appConfig) androidManifest(debug bool) string {
	var b strings.Builder
	b.WriteString("<manifest xmlns:android=\"http://schemas.android.com/apk/res/android\">\n")
	for _, p := range c.Android.Permissions {
		name, max, _ := strings.Cut(p, ":")
		if max != "" {
			max = fmt.Sprintf(` android:maxSdkVersion="%s"`, html.EscapeString(max))
		}
		fmt.Fprintf(&b, "    <uses-permission android:name=\"%s\"%s />\n", html.EscapeString(name), max)
	}
	b.WriteString("    <application")
	if debug {
		b.WriteString(" android:usesCleartextTraffic=\"true\"")
	}
	b.WriteString(">\n        <activity android:name=\"dev.ryui.RyUIActivity\" android:exported=\"true\">\n")
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
	if c.ShareTarget {
		b.WriteString(`            <intent-filter>
                <action android:name="android.intent.action.SEND" />
                <category android:name="android.intent.category.DEFAULT" />
                <data android:mimeType="text/plain" />
            </intent-filter>
`)
	}
	b.WriteString("        </activity>\n")
	for _, x := range c.androidApp {
		b.WriteString("        " + x + "\n")
	}
	b.WriteString("    </application>\n</manifest>\n")
	return b.String()
}

// iosDevices are Info.plist's UIDeviceFamily: iPhone, and iPad unless
// the app is iPhone only.
func (c *appConfig) iosDevices() []int {
	if c != nil && c.IOS.IPhoneOnly {
		return []int{1}
	}
	return []int{1, 2}
}

// ipadOrientations are Info.plist's UISupportedInterfaceOrientations~ipad:
// all four, whatever the app's orientations, since multitasking (Split
// View, Slide Over, Stage Manager) needs them; the app's window is any
// shape there anyway. nil for an iPhone-only app.
func (c *appConfig) ipadOrientations() []string {
	if c != nil && c.IOS.IPhoneOnly {
		return nil
	}
	return []string{"UIInterfaceOrientationPortrait", "UIInterfaceOrientationPortraitUpsideDown",
		"UIInterfaceOrientationLandscapeLeft", "UIInterfaceOrientationLandscapeRight"}
}

func (c *appConfig) allows(o string) bool {
	for _, x := range c.Orientations {
		if x == o {
			return true
		}
	}
	return false
}

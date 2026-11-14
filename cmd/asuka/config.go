package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// appConfig is an app's asuka.json: what the stores and the platforms need
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
	// ID is the bundle ID and application ID (default: dev.asuka.<dir>).
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
		// WidgetProfile is the widget extension's (widgets), whose ID is
		// the app's plus ".widgets" (default: the best match installed).
		WidgetProfile string `json:"widgetProfile"`
		// NotificationProfile is the Notification Service Extension's
		// (push.Rewrite), whose ID is the app's plus ".notification"
		// (default: the best match installed).
		NotificationProfile string `json:"notificationProfile"`
		// Usage are the purpose strings for protected resources
		// (NSCameraUsageDescription, ...).
		Usage map[string]string `json:"usage"`
		// InfoPlist adds string keys to Info.plist.
		InfoPlist map[string]string `json:"infoPlist"`
		// LocationPurposes are the reasons location.RequestPrecise shows,
		// by the purpose key it's given (Info.plist's
		// NSLocationTemporaryUsageDescriptionDictionary; default: "default",
		// "To show exactly where you are.").
		LocationPurposes map[string]string `json:"locationPurposes"`
		// NonExemptEncryption is what a build says of its encryption
		// (ITSAppUsesNonExemptEncryption), a legal statement that's yours to
		// make: "no" (the default), "yes", or "ask", which says nothing, so App
		// Store Connect asks at each upload.
		NonExemptEncryption string `json:"nonExemptEncryption"`
		// Collected is the data the app collects, for its privacy manifest:
		// what App Store Connect's report compares with its privacy labels.
		Collected []collectedData `json:"collected"`
		// BackgroundModes adds to Info.plist's UIBackgroundModes: "audio",
		// to keep recording and playing with the screen locked.
		BackgroundModes []string `json:"backgroundModes"`
		// StoreKit is a StoreKit configuration file (Xcode's .storekit: the
		// products, their prices, Ask to Buy, renewals sped up) that
		// asuka dev's build for the simulator sells from, as Xcode's scheme
		// does (asuka/sdk/purchase, which asuka dev builds into the app).
		StoreKit string `json:"storekit"`
		// IPhoneOnly leaves iPad out: the app runs there as an iPhone app,
		// in a phone-sized window. By default it's an iPad app too, in
		// Split View and Stage Manager, at any size the user gives it.
		IPhoneOnly bool `json:"iphoneOnly"`
		// AppStoreConnect is the App Store Connect API key asuka publish
		// uploads with: its ID, its issuer's (none for an individual key)
		// and its .p8's path (default: AuthKey_<id>.p8 in a private_keys
		// directory, as altool looks). ASUKA_ASC_KEY_ID, ASUKA_ASC_ISSUER_ID
		// and ASUKA_ASC_KEY (the path or the key's text) override them.
		AppStoreConnect struct {
			KeyID    string `json:"keyId"`
			IssuerID string `json:"issuerId"`
			Key      string `json:"key"`
		} `json:"appStoreConnect"`
	} `json:"ios"`
	// Mac is what a Mac build signs the app with (ios.team is its team too).
	Mac struct {
		// Profile is a Developer ID (or development) provisioning profile's
		// path, name or UUID, which push, Sign in with Apple and App Attest
		// need (default: ASUKA_MAC_PROFILE, else, for an app that uses them,
		// the best match installed); AppStoreProfile, a Mac App Store one,
		// for --appstore.
		Profile         string `json:"profile"`
		AppStoreProfile string `json:"appStoreProfile"`
	} `json:"mac"`
	// Accent is the app's accent color, "#rrggbb" or "#rrggbbaa": the
	// tint color on iOS; on Android, the seed of the Material 3 color
	// scheme (Android 12 and later; before, Asuka's own uses of the accent
	// take it). Default: the platform's.
	Accent string `json:"accent"`
	// Background makes a desktop app one that runs in the background,
	// living in the tray (asuka/sdk/tray), as app.RunInBackground does: no
	// window at launch, kept running with none, and on macOS no Dock
	// icon from launch (Info.plist's LSUIElement).
	Background bool `json:"background"`
	// Scheme is the app's URL scheme: myapp://anything opens it
	// (ui.OnOpenURL gets the URL).
	Scheme string `json:"scheme"`
	// FileTypes are the kinds of files the app opens (filetypes.go):
	// ui.OnOpenFiles gets them.
	FileTypes []fileTypeConfig `json:"fileTypes"`
	// SingleInstance false lets the app run as often as it's started on
	// Windows and Linux, each launch its own; by default (true) a second
	// launch hands its arguments to the running one
	// (app.OnSecondInstance, ui.OnOpenFiles) and quits. macOS's Launch
	// Services runs an app once either way.
	SingleInstance *bool `json:"singleInstance"`
	// Links are the web domains whose https links open the app: iOS's
	// universal links (the domain serves apple-app-site-association) and
	// Android's App Links (assetlinks.json).
	Links []string `json:"links"`
	// ShareTarget offers the app in other apps' share sheets (iOS: a
	// share extension that opens the app; Android: ACTION_SEND and
	// ACTION_SEND_MULTIPLE intent filters); ui.OnShare gets what's
	// shared. true: texts and links; or what it takes,
	// {"types": ["text", "image/*", "video/*"], "max": 10}.
	ShareTarget shareTarget `json:"shareTarget"`
	// NFC is what asuka/sdk/nfc needs of the platforms (nfc.go): iOS's
	// AIDs and FeliCa system codes, Android's tags that open the app.
	NFC nfcConfig `json:"nfc"`
	// Splash is the launch screen: an image (a square PNG, a logo) in the
	// middle of a color, until the app's first frame (or splash.Hide).
	//	"splash": {"image": "logo.png", "size": 120, "background": "#ffffff", "darkBackground": "#000000"}
	Splash struct {
		Image string `json:"image"`
		// Size is the image's width in points (dp): default 120; at most
		// 160 (Android 12's splash icon's circle holds it).
		Size int `json:"size"`
		// Background and DarkBackground are "#rrggbb" (default: white;
		// the light one).
		Background     string `json:"background"`
		DarkBackground string `json:"darkBackground"`
	} `json:"splash"`
	// Widgets are the app's home screen widgets (asuka/sdk/widget fills them).
	Widgets []widgetConfig `json:"widgets"`
	// I18n says where the app's catalogs are, when they aren't in locales.
	I18n *i18nConfig `json:"i18n"`
	// Updates are a desktop app's (asuka/sdk/update, updates.go): the public
	// key updates are signed with, and where they're published.
	Updates *updatesConfig `json:"updates"`
	// dir is the app's directory.
	dir string
	// Shortcuts are the app's actions for the system: on iOS, App
	// Shortcuts (Siri, Spotlight, the Shortcuts app, the Action button);
	// on Android, the launcher's shortcuts. Each opens the app with its URL.
	Shortcuts []shortcutConfig `json:"shortcuts"`
	// ApplePay is Apple Pay's (asuka/sdk/pay): MerchantIDs are the
	// merchant identifiers the app pays with ("merchant.com.example"),
	// its in-app-payments entitlement's (iOS, macOS).
	ApplePay struct {
		MerchantIDs []string `json:"merchantIds"`
	} `json:"applePay"`
	// shareSheet: the app's Go code calls app.ShareSheet (its archive has
	// it, sheetSymbol): the share target shows the app's sheet.
	shareSheet bool
	// pushRewrite: the app's Go code calls push.Rewrite (rewriteSymbol):
	// on iOS it has a Notification Service Extension, which runs it.
	pushRewrite bool
	// locationPrecise: the app's Go code calls location.RequestPrecise
	// (ASUKA_LOCATION_PRECISE): Info.plist has its purposes.
	locationPrecise bool
	// nfcOn: the app imports asuka/sdk/nfc (addNFC): its filters and
	// uses-feature go in the manifest.
	nfcOn bool
	// liveActivity: the app's Go code calls asuka/sdk/liveactivity
	// (ASUKA_LIVEACTIVITY): on iOS its widget extension has the Live
	// Activities' template, with or without widgets.
	liveActivity bool
	// androidModules are the host's optional Gradle modules the app's
	// packages need (featurePermissions).
	androidModules []string
	// macSwift are the Mac's Swift's compilation conditions the app's
	// packages need (featurePermissions): the Mac's library
	// (AsukaNatives.dylib) is built with them, natives or not.
	macSwift []string
	// iosTrue and androidApp: featurePermissions' Info.plist keys set to
	// true, and application elements of the Android manifest.
	iosTrue    []string
	androidApp []string
	// entitlements are the iOS entitlements the packages need, with their
	// values for a simulator build (a release's are the profile's, which
	// must have them); plistArrays, Info.plist's arrays they add to.
	entitlements map[string]any
	plistArrays  map[string][]string
	androidTop   []string
	// flatpakArgs are the Flatpak's finish-args the packages need (the
	// system bus names they talk to).
	flatpakArgs []string
	// What the app's native packages' Swift needs (their Info.plist and
	// entitlements, nativeNeeds): Info.plist's keys, and what each of
	// their entitlements is for.
	nativePlist     map[string]any
	entitlementsFor map[string]string
	// nativeModules are the app's native packages' Gradle modules: each
	// one's name and directory.
	nativeModules [][2]string
	// Linux is what a Linux build says of the app, in its desktop file
	// and its AppStream metainfo (what software centers and Flathub show).
	Linux struct {
		// Summary is a line saying what the app does; Description, a few
		// paragraphs (separated by blank lines).
		Summary     string `json:"summary"`
		Description string `json:"description"`
		// Categories are the desktop file's (freedesktop.org's main
		// categories: Utility, Office, Development, Network...; default
		// Utility), and Keywords more words a launcher finds it by.
		Categories []string `json:"categories"`
		Keywords   []string `json:"keywords"`
		// License is the app's, as an SPDX expression (MIT, GPL-3.0-or-later,
		// LicenseRef-proprietary); Developer, its developer's name;
		// Homepage, its web page.
		License   string `json:"license"`
		Developer string `json:"developer"`
		Homepage  string `json:"homepage"`
		// FlatpakRuntime is the GNOME runtime's version the Flatpak manifest
		// asks for (default: a recent one; 46 or later).
		FlatpakRuntime string `json:"flatpakRuntime"`
	} `json:"linux"`
	// Bluetooth is what an app of asuka/sdk/bluetooth asks of the
	// platforms beyond what importing the package gives (docs/bluetooth.md).
	Bluetooth struct {
		// Background are the roles the app keeps in the background:
		// "central" (iOS's bluetooth-central mode, with state restoration;
		// Android's background scan's receiver, and before Android 12 the
		// background location permission it needs) and "peripheral" (iOS's
		// bluetooth-peripheral mode).
		Background []string `json:"background"`
		// Location says the app finds where the user is from its scans
		// (beacons): Android 12's BLUETOOTH_SCAN without neverForLocation,
		// and the location permission that then needs. Without it, scans
		// say they don't, and Android leaves some beacons' advertisements
		// out of them.
		Location bool `json:"location"`
	} `json:"bluetooth"`
	// Web is what a web build needs (cmd/asuka/web.go).
	Web struct {
		// VapidPublicKey is the app server's VAPID public key (base64url, an
		// uncompressed P-256 point), for asuka/sdk/push's Web Push: the browser
		// subscribes with it, and the server signs with its private key
		// (asuka/sdk/push/webpush: GenerateKeys makes a pair).
		VapidPublicKey string `json:"vapidPublicKey"`
		// Base is the path the site is served at ("/app/" for
		// https://example.com/app/; default "/"): the page's <base href>,
		// so that its files are found from any route's path, and under
		// which a Router's hrefs are the page's addresses.
		Base string `json:"base"`
	} `json:"web"`
	Android struct {
		// DynamicColor takes the colors from the user's wallpaper where
		// the system has them (Android 12 and later), as Material apps do;
		// the accent is the seed elsewhere.
		DynamicColor bool     `json:"dynamicColor"`
		Permissions  []string `json:"permissions"`
		// The upload key; its passwords come from ASUKA_KEYSTORE_PASSWORD
		// and ASUKA_KEY_PASSWORD (default: the keystore's).
		Keystore string `json:"keystore"`
		KeyAlias string `json:"keyAlias"`
		// GoogleServices is the Firebase project's google-services.json,
		// for asuka/sdk/push (Firebase Cloud Messaging).
		GoogleServices string `json:"googleServices"`
		// MapsKey is a Google Maps Platform API key (Maps SDK for Android),
		// for asuka/sdk/maps.
		MapsKey string `json:"mapsKey"`
		// ServiceAccount is the JSON key of the Google Cloud service
		// account asuka publish uploads with (ASUKA_PLAY_SERVICE_ACCOUNT, the
		// path or the JSON, overrides it).
		ServiceAccount string `json:"serviceAccount"`
	} `json:"android"`
}

var (
	idPart   = regexp.MustCompile(`[^A-Za-z0-9]+`)
	hexColor = regexp.MustCompile(`^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	scheme   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*$`)
	domain   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
)

// multipleInstances is the -ldflags -X of an app that runs as often as
// it's started (asuka.json's "singleInstance": false), or "".
func (c *appConfig) multipleInstances() string {
	if c == nil || c.SingleInstance == nil || *c.SingleInstance {
		return ""
	}
	return " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.MultipleInstances=1")
}

// loadConfig reads dir/asuka.json, filling in the defaults.
func loadConfig(dir string) (*appConfig, error) {
	c := &appConfig{}
	data, err := os.ReadFile(filepath.Join(dir, "asuka.json"))
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("asuka.json: %v", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	c.useI18nDefaults(dir)
	base := filepath.Base(dir)
	if c.Name == "" {
		c.Name = base
	}
	if c.ID == "" {
		c.ID = "dev.asuka." + strings.Trim(strings.ToLower(idPart.ReplaceAllString(base, "-")), "-")
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
			return nil, fmt.Errorf("asuka.json: orientation %q (want portrait or landscape)", o)
		}
	}
	if c.Accent != "" {
		if !hexColor.MatchString(c.Accent) {
			return nil, fmt.Errorf("asuka.json: accent %q (want #rrggbb or #rrggbbaa)", c.Accent)
		}
	}
	if c.Scheme != "" && !scheme.MatchString(c.Scheme) {
		return nil, fmt.Errorf("asuka.json: scheme %q (want letters, digits, + - .; a letter first)", c.Scheme)
	}
	for _, l := range c.Links {
		if !domain.MatchString(l) {
			return nil, fmt.Errorf("asuka.json: link domain %q (want a host name, as example.com)", l)
		}
	}
	for _, r := range c.Bluetooth.Background {
		if r != "central" && r != "peripheral" {
			return nil, fmt.Errorf("asuka.json: bluetooth background %q (want central or peripheral)", r)
		}
	}
	if err := c.checkFileTypes(dir); err != nil {
		return nil, fmt.Errorf("asuka.json: %v", err)
	}
	if err := c.checkWidgets(); err != nil {
		return nil, fmt.Errorf("asuka.json: %v", err)
	}
	if err := c.ShareTarget.check(); err != nil {
		return nil, fmt.Errorf("asuka.json: shareTarget: %v", err)
	}
	if err := c.NFC.check(); err != nil {
		return nil, fmt.Errorf("asuka.json: nfc: %v", err)
	}
	if _, err := c.updates(); err != nil {
		return nil, err
	}
	if c.Icon != "" && !filepath.IsAbs(c.Icon) {
		c.Icon = filepath.Join(dir, c.Icon)
	}
	if c.Android.Keystore != "" && !filepath.IsAbs(c.Android.Keystore) {
		c.Android.Keystore = filepath.Join(dir, c.Android.Keystore)
	}
	if k := &c.IOS.AppStoreConnect.Key; *k != "" && !filepath.IsAbs(*k) && !strings.Contains(*k, "-----BEGIN") {
		*k = filepath.Join(dir, *k)
	}
	if c.Android.ServiceAccount != "" && !filepath.IsAbs(c.Android.ServiceAccount) {
		c.Android.ServiceAccount = filepath.Join(dir, c.Android.ServiceAccount)
	}
	if c.Android.GoogleServices != "" && !filepath.IsAbs(c.Android.GoogleServices) {
		c.Android.GoogleServices = filepath.Join(dir, c.Android.GoogleServices)
	}
	if sp := &c.Splash; sp.Image != "" || sp.Background != "" {
		if sp.Image != "" && !filepath.IsAbs(sp.Image) {
			sp.Image = filepath.Join(dir, sp.Image)
		}
		if sp.Size <= 0 {
			sp.Size = 120
		}
		sp.Size = min(sp.Size, 160)
		if sp.Background == "" {
			sp.Background = "#ffffff"
		}
		if sp.DarkBackground == "" {
			sp.DarkBackground = sp.Background
		}
		for _, b := range []string{sp.Background, sp.DarkBackground} {
			if !hexColor.MatchString(b) || len(strings.TrimPrefix(b, "#")) != 6 {
				return nil, fmt.Errorf("asuka.json: splash background %q (want #rrggbb)", b)
			}
		}
	}
	return c, nil
}

// ldflags is the go build -ldflags for the app: what of asuka.json its Go
// code reads (internal/boot), and, for a release, no symbol tables; for a
// development build, no DWARF.
func (c *appConfig) ldflags(release bool) string {
	if release {
		return c.ldflagsStrip("-s", "-w")
	}
	// A development build: without DWARF, which the linker spends a good
	// part of its time on and asuka dev's runs don't use (panics' traces
	// and runtime.Callers come from the symbol table, which stays).
	return c.ldflagsStrip("-w")
}

// ldflagsStrip is ldflags with what the linker leaves out: an iOS
// release keeps Go's debug information for its dSYM, an Android one its
// symbol table for the native debug symbols (crash reports' native
// frames), which the build strips from the binaries it ships.
func (c *appConfig) ldflagsStrip(strip ...string) string {
	f := append([]string(nil), strip...)
	if c != nil && c.Accent != "" {
		f = append(f, "-X", "github.com/egoist/asuka/internal/boot.Accent="+c.Accent)
	}
	if c != nil && c.Android.DynamicColor {
		f = append(f, "-X", "github.com/egoist/asuka/internal/boot.DynamicColor=1")
	}
	if c != nil && c.Background {
		f = append(f, "-X", "github.com/egoist/asuka/internal/boot.Background=1")
	}
	if devLang != "" {
		f = append(f, "-X", "github.com/egoist/asuka/internal/boot.Lang="+devLang)
	}
	if devDirection != "" {
		f = append(f, "-X", "github.com/egoist/asuka/internal/boot.Direction="+devDirection)
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
	if ios && c.ShareTarget.on {
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
// build's also lets asuka dev serve over plain HTTP.
// collectedData is a kind of data in a privacy manifest.
type collectedData struct {
	// Type is Apple's name without its prefix: "EmailAddress" is
	// NSPrivacyCollectedDataTypeEmailAddress.
	Type string `json:"type"`
	// Linked: tied to the user's identity. Tracking: used to track them.
	Linked   bool `json:"linked"`
	Tracking bool `json:"tracking"`
	// Purposes are Apple's, without their prefix: "AppFunctionality".
	Purposes []string `json:"purposes"`
}

// featurePermissions are what Asuka's packages need of the platform: an
// app that imports one is given them (and, on iOS, a purpose string,
// which asuka.json's ios.usage can say better), and the Android host's
// optional Gradle modules (platform/android/asuka-<name>) it needs.
var featurePermissions = []struct {
	pkg     string
	android []string // permissions; "name:28": up to Android 9 (API 28) only
	usage   map[string]string
	modules []string
	// iosTrue are Info.plist keys set to true; androidApp, elements of the
	// Android manifest's application (a service, a receiver).
	iosTrue    []string
	androidApp []string
	// entitlements (with a simulator build's values) and Info.plist arrays.
	entitlements map[string]any
	plistArrays  map[string][]string
	// androidTop are elements of the Android manifest itself (queries).
	androidTop []string
}{
	{"github.com/egoist/asuka/sdk/location", []string{"android.permission.ACCESS_COARSE_LOCATION", "android.permission.ACCESS_FINE_LOCATION"},
		map[string]string{"NSLocationWhenInUseUsageDescription": "To show where you are."}, nil, nil, nil, nil, nil, nil},
	{"github.com/egoist/asuka/sdk/notify", []string{"android.permission.POST_NOTIFICATIONS"}, nil, nil, nil, nil, nil, nil, nil},
	// camera.View's preview is the app's own: the camera permission (and
	// with it declared, the system's camera app needs it granted too).
	{"github.com/egoist/asuka/sdk/camera", []string{"android.permission.CAMERA"}, map[string]string{"NSCameraUsageDescription": "To take a photo."}, nil, nil, nil, nil, nil, nil},
	{"github.com/egoist/asuka/sdk/camera/record", []string{"android.permission.RECORD_AUDIO"}, map[string]string{"NSMicrophoneUsageDescription": "To record a video's sound."}, nil, nil, nil, nil, nil, nil},
	{"github.com/egoist/asuka/sdk/network", []string{"android.permission.ACCESS_NETWORK_STATE"}, nil, nil, nil, nil, nil, nil, nil},
	// MediaStore needs no permission from Android 10; before, writing to
	// the shared folders does. Files in Documents show in iOS's Files.
	{"github.com/egoist/asuka/sdk/library", []string{"android.permission.WRITE_EXTERNAL_STORAGE:28"},
		map[string]string{"NSPhotoLibraryAddUsageDescription": "To save videos and photos to your library."}, nil,
		[]string{"UIFileSharingEnabled", "LSSupportsOpeningDocumentsInPlace"}, nil, nil, nil, nil},
	// A foreground service of the data sync type, its notification, the
	// CPU kept awake.
	{"github.com/egoist/asuka/sdk/background", []string{"android.permission.FOREGROUND_SERVICE", "android.permission.FOREGROUND_SERVICE_DATA_SYNC",
		"android.permission.POST_NOTIFICATIONS", "android.permission.WAKE_LOCK"}, nil, nil, nil,
		[]string{`<service android:name="dev.asuka.AsukaWorkService" android:exported="false" android:foregroundServiceType="dataSync" />`}, nil, nil, nil},
	// ML Kit's barcode model: about 2 MB, only in the apps that scan.
	{"github.com/egoist/asuka/sdk/scan", nil, nil, []string{"scan"}, nil, nil, nil, nil, nil},
	// ML Kit's text, face and barcode models: only in the apps that analyze.
	{pkg: "github.com/egoist/asuka/sdk/vision", modules: []string{"vision"}},
	// Reading the library: all of it, or what the user chose (Android 14).
	{pkg: "github.com/egoist/asuka/sdk/photos",
		android: []string{"android.permission.READ_EXTERNAL_STORAGE:32", "android.permission.READ_MEDIA_IMAGES", "android.permission.READ_MEDIA_VISUAL_USER_SELECTED"},
		usage:   map[string]string{"NSPhotoLibraryUsageDescription": "To show your photos."}},
	{pkg: "github.com/egoist/asuka/sdk/screen", android: []string{"android.permission.DETECT_SCREEN_CAPTURE"}},
	{pkg: "github.com/egoist/asuka/sdk/biometric", android: []string{"android.permission.USE_BIOMETRIC"},
		usage: map[string]string{"NSFaceIDUsageDescription": "To confirm it's you."}},
	{pkg: "github.com/egoist/asuka/sdk/tracking", usage: map[string]string{"NSUserTrackingUsageDescription": "To show you ads that suit you."}},
	// Play's in-app review, Play Integrity, Play Age Signals, Google Maps:
	// Google's libraries, only in the apps that use them.
	{pkg: "github.com/egoist/asuka/sdk/review", modules: []string{"review"}},
	{pkg: "github.com/egoist/asuka/sdk/integrity", modules: []string{"integrity"},
		entitlements: map[string]any{"com.apple.developer.devicecheck.appattest-environment": "development"}},
	{pkg: "github.com/egoist/asuka/sdk/agerange", modules: []string{"agerange"},
		entitlements: map[string]any{"com.apple.developer.declared-age-range": true}},
	{pkg: "github.com/egoist/asuka/sdk/maps", modules: []string{"maps"}},
	{pkg: "github.com/egoist/asuka/sdk/appleauth", entitlements: map[string]any{"com.apple.developer.applesignin": []string{"Default"}}},
	// Firebase Cloud Messaging (its manifest has POST_NOTIFICATIONS); APNs.
	{pkg: "github.com/egoist/asuka/sdk/push", modules: []string{"push"},
		entitlements: map[string]any{"aps-environment": "development"},
		plistArrays:  map[string][]string{"UIBackgroundModes": {"remote-notification"}}},
	// The service keeps a recording with Options.Background going out of sight.
	{pkg: "github.com/egoist/asuka/sdk/audio/record", android: []string{"android.permission.RECORD_AUDIO",
		"android.permission.FOREGROUND_SERVICE", "android.permission.FOREGROUND_SERVICE_MICROPHONE", "android.permission.POST_NOTIFICATIONS"},
		usage:      map[string]string{"NSMicrophoneUsageDescription": "To record sound."},
		androidApp: []string{`<service android:name="dev.asuka.AsukaRecordService" android:exported="false" android:foregroundServiceType="microphone" />`}},
	// Now Playing: a MediaSession and its notification (which needs no
	// POST_NOTIFICATIONS: a media session's is exempt); while playing, a
	// foreground service of the media playback type keeps the app
	// running out of sight (Android 14's rules).
	{pkg: "github.com/egoist/asuka/sdk/nowplaying",
		android:    []string{"android.permission.FOREGROUND_SERVICE", "android.permission.FOREGROUND_SERVICE_MEDIA_PLAYBACK"},
		androidApp: []string{`<service android:name="dev.asuka.AsukaMediaService" android:exported="false" android:foregroundServiceType="mediaPlayback" />`}},
	// On-device speech recognition; Android's reads the file as the microphone's permission.
	{pkg: "github.com/egoist/asuka/sdk/transcribe", android: []string{"android.permission.RECORD_AUDIO"},
		usage: map[string]string{"NSSpeechRecognitionUsageDescription": "To turn your recordings into text, on this device."}},
	// WorkManager; iOS's background tasks, by the identifiers the host
	// schedules (AsukaPeriodic).
	{pkg: "github.com/egoist/asuka/sdk/periodic", modules: []string{"periodic"},
		plistArrays: map[string][]string{"UIBackgroundModes": {"fetch", "processing"},
			"BGTaskSchedulerPermittedIdentifiers": {"dev.asuka.refresh", "dev.asuka.processing"}}},
	// Live activities: an ongoing notification each, which Android 16
	// promotes to a Live Update (POST_PROMOTED_NOTIFICATIONS); the
	// receiver hears one dismissed, and refreshes a bar that fills by
	// itself. iOS's Info.plist key and widget extension come with the
	// feature (ASUKA_LIVEACTIVITY), as the Go code calls it.
	{pkg: "github.com/egoist/asuka/sdk/liveactivity",
		android:    []string{"android.permission.POST_NOTIFICATIONS", "android.permission.POST_PROMOTED_NOTIFICATIONS"},
		androidApp: []string{`<receiver android:name="dev.asuka.AsukaLiveActivityReceiver" android:exported="false" />`}},
	// In-app purchases: Play Billing (its library's manifest declares
	// BILLING; said here too), only in the apps that sell; on the Mac,
	// StoreKit 2's Swift in the app's library. iOS's comes with the
	// feature (ASUKA_PURCHASE); the In-App Purchase capability needs no
	// entitlement.
	{pkg: "github.com/egoist/asuka/sdk/purchase", android: []string{"com.android.vending.BILLING"}, modules: []string{"purchase"}},
	// Google Pay: play-services-wallet, in the asuka-pay module, whose
	// manifest enables its API and has the service Google Pay's sheet
	// calls back. Apple Pay's entitlement has asuka.json's merchant IDs
	// (payEntitlement).
	{pkg: payPkg, modules: []string{"pay"}},
	// NFC: Android's permission (uses-feature, filters: addNFC); iOS's
	// purpose string and Core NFC's entitlement, for both of its sessions.
	{pkg: nfcPkg, android: []string{"android.permission.NFC"},
		usage:        map[string]string{"NFCReaderUsageDescription": "To read and write NFC tags."},
		entitlements: map[string]any{"com.apple.developer.nfc.readersession.formats": []string{"NDEF", "TAG"}}},
	// Binding the text-to-speech engine: Android 11's package visibility.
	{pkg: "github.com/egoist/asuka/sdk/speech", androidTop: []string{`<queries><intent><action android:name="android.intent.action.TTS_SERVICE" /></intent></queries>`}},
	// The pedometer: iOS's Motion & Fitness; Android's physical activity.
	{pkg: "github.com/egoist/asuka/sdk/sensors", android: []string{"android.permission.ACTIVITY_RECOGNITION"},
		usage: map[string]string{"NSMotionUsageDescription": "To count your steps."}},
	{pkg: "github.com/egoist/asuka/sdk/contacts", android: []string{"android.permission.READ_CONTACTS"},
		usage: map[string]string{"NSContactsUsageDescription": "To find people you know."}},
	// iOS 17's full access, and before it.
	{pkg: "github.com/egoist/asuka/sdk/calendar", android: []string{"android.permission.READ_CALENDAR"},
		usage: map[string]string{"NSCalendarsFullAccessUsageDescription": "To show your events.", "NSCalendarsUsageDescription": "To show your events."}},
	// Whether an app takes mailto: and smsto: links: iOS's canOpenURL,
	// Android 11's package visibility.
	{pkg: "github.com/egoist/asuka/sdk/compose",
		plistArrays: map[string][]string{"LSApplicationQueriesSchemes": {"mailto"}},
		androidTop: []string{`<queries><intent><action android:name="android.intent.action.SENDTO" /><data android:scheme="mailto" /></intent>` +
			`<intent><action android:name="android.intent.action.SENDTO" /><data android:scheme="smsto" /></intent></queries>`}},
	// Bluetooth LE: Android 12's nearby devices permissions (the scan's
	// "neverForLocation" unless asuka.json's bluetooth.location; Asuka's
	// Android is 13 and later, so not the older Bluetooth and location
	// permissions); the companion device chooser. Core Bluetooth's
	// purpose string. BlueZ on the system bus, from a Flatpak. asuka.json's
	// bluetooth.background adds the background modes (bluetoothExtras).
	{pkg: bluetoothPackage,
		android: []string{"android.permission.BLUETOOTH_SCAN|neverForLocation", "android.permission.BLUETOOTH_CONNECT",
			"android.permission.BLUETOOTH_ADVERTISE"},
		usage: map[string]string{"NSBluetoothAlwaysUsageDescription": "To connect to your Bluetooth devices."},
		androidTop: []string{`<uses-feature android:name="android.hardware.bluetooth_le" android:required="false" />`,
			`<uses-feature android:name="android.software.companion_device_setup" android:required="false" />`}},
}

// featureMacSwift are the parts of platform/macos's Swift a package
// needs, as compilation conditions (macSwift): the Mac's library is built
// with them for an app that imports it.
var featureMacSwift = map[string][]string{
	// StoreKit 2 is Swift's alone.
	"github.com/egoist/asuka/sdk/purchase": {"ASUKA_PURCHASE"},
}

// locationSymbols are asuka/sdk/location's functions that need more of
// Android than the position in the foreground: by what each needs.
var locationSymbols = struct{ always, updates, regions []string }{
	always:  []string{"github.com/egoist/asuka/sdk/location.RequestAlways"},
	updates: []string{"github.com/egoist/asuka/sdk/location.StartUpdates"},
	regions: []string{"github.com/egoist/asuka/sdk/location.Monitor"},
}

// useLocation adds what the app's calls into asuka/sdk/location's
// background part need on Android, has telling which it calls (its
// library's symbols): ACCESS_BACKGROUND_LOCATION for RequestAlways (which
// Google Play asks about, so only then); for StartUpdates, the foreground
// service of the location type and its notification; for both
// StartUpdates and Monitor, the receiver their PendingIntents and a
// reboot (which clears them) go to, and the asuka-location module (Google
// Play services' Fused Location Provider and geofences).
func (c *appConfig) useLocation(has func(symbol string) bool) {
	uses := func(syms []string) bool { return slices.ContainsFunc(syms, has) }
	always, updates, regions := uses(locationSymbols.always), uses(locationSymbols.updates), uses(locationSymbols.regions)
	var perms, app []string
	if always {
		perms = append(perms, "android.permission.ACCESS_BACKGROUND_LOCATION")
	}
	if updates {
		perms = append(perms, "android.permission.FOREGROUND_SERVICE", "android.permission.FOREGROUND_SERVICE_LOCATION", "android.permission.POST_NOTIFICATIONS")
		app = append(app, `<service android:name="dev.asuka.AsukaLocationService" android:exported="false" android:foregroundServiceType="location" />`)
	}
	if updates || regions {
		perms = append(perms, "android.permission.RECEIVE_BOOT_COMPLETED")
		app = append(app, `<receiver android:name="dev.asuka.AsukaLocationReceiver" android:exported="false"><intent-filter>`+
			`<action android:name="android.intent.action.BOOT_COMPLETED" /><action android:name="android.intent.action.MY_PACKAGE_REPLACED" />`+
			`</intent-filter></receiver>`)
		if !slices.Contains(c.androidModules, "location") {
			c.androidModules = append(c.androidModules, "location")
		}
	}
	for _, p := range perms {
		if !slices.Contains(c.Android.Permissions, p) {
			c.Android.Permissions = append(c.Android.Permissions, p)
		}
	}
	for _, x := range app {
		if !slices.Contains(c.androidApp, x) {
			c.androidApp = append(c.androidApp, x)
		}
	}
}

// payPkg is asuka/sdk/pay.
const payPkg = "github.com/egoist/asuka/sdk/pay"

// payEntitlement gives an app that imports asuka/sdk/pay (deps; nil:
// any app) Apple Pay's entitlement, with asuka.json's merchant IDs.
func (c *appConfig) payEntitlement(deps []string) {
	if len(c.ApplePay.MerchantIDs) == 0 || deps != nil && !slices.Contains(deps, payPkg) {
		return
	}
	if c.entitlements == nil {
		c.entitlements = map[string]any{}
	}
	c.entitlements["com.apple.developer.in-app-payments"] = c.ApplePay.MerchantIDs
}

// featureFlatpak are the Flatpak's finish-args a package needs (Linux):
// the system bus names it talks to.
var featureFlatpak = map[string][]string{
	bluetoothPackage: {"--system-talk-name=org.bluez"},
}

// bluetoothPackage is asuka/sdk/bluetooth, whose needs asuka.json's
// "bluetooth" adds to (bluetoothExtras).
const bluetoothPackage = "github.com/egoist/asuka/sdk/bluetooth"

// permissionsFor adds the permissions the packages deps (an app's
// imports, all the way down) need. nil deps: every package's (the player
// runs any app).
func (c *appConfig) permissionsFor(deps []string) {
	c.payEntitlement(deps)
	for _, f := range featurePermissions {
		if deps != nil && !slices.Contains(deps, f.pkg) {
			continue
		}
		for _, p := range f.android {
			c.addPermission(p)
		}
		for _, x := range featureFlatpak[f.pkg] {
			if !slices.Contains(c.flatpakArgs, x) {
				c.flatpakArgs = append(c.flatpakArgs, x)
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
		for _, x := range featureMacSwift[f.pkg] {
			if !slices.Contains(c.macSwift, x) {
				c.macSwift = append(c.macSwift, x)
			}
		}
		for _, x := range f.androidTop {
			if !slices.Contains(c.androidTop, x) {
				c.androidTop = append(c.androidTop, x)
			}
		}
		for k, v := range f.entitlements {
			if c.entitlements == nil {
				c.entitlements = map[string]any{}
			}
			c.entitlements[k] = v
		}
		for k, vs := range f.plistArrays {
			if c.plistArrays == nil {
				c.plistArrays = map[string][]string{}
			}
			for _, v := range vs {
				if !slices.Contains(c.plistArrays[k], v) {
					c.plistArrays[k] = append(c.plistArrays[k], v)
				}
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
	c.addNFC(deps)
	if deps != nil && slices.Contains(deps, bluetoothPackage) {
		c.bluetoothExtras()
	}
}

// addPermission adds an Android permission ("name", "name:28" up to an
// API level, "name|flag" with usesPermissionFlags), once per name: a
// permission without a maximum wins over one with, as one without flags
// does over one with (the flags narrow what it's for).
func (c *appConfig) addPermission(p string) {
	parse := func(p string) (name string, max int, flags string) {
		p, flags, _ = strings.Cut(p, "|")
		name, m, _ := strings.Cut(p, ":")
		max, _ = strconv.Atoi(m) // 0: every level
		return
	}
	name, max, flags := parse(p)
	for i, q := range c.Android.Permissions {
		qn, qmax, qflags := parse(q)
		if qn != name {
			continue
		}
		if max != 0 && (qmax == 0 || qmax > max) {
			max = qmax
		}
		if qflags == "" {
			flags = ""
		}
		merged := name
		if max != 0 {
			merged += ":" + strconv.Itoa(max)
		}
		if flags != "" {
			merged += "|" + flags
		}
		c.Android.Permissions[i] = merged
		return
	}
	c.Android.Permissions = append(c.Android.Permissions, p)
}

// bluetoothExtras are what asuka.json's "bluetooth" asks for, for an app
// of asuka/sdk/bluetooth: the background modes, the location from scans.
func (c *appConfig) bluetoothExtras() {
	if c.Bluetooth.Location {
		// Scans that find where the user is: no neverForLocation, and the
		// location permission, which Android then wants granted to scan
		// (Request asks for it).
		for i, p := range c.Android.Permissions {
			if name, _, _ := strings.Cut(p, "|"); name == "android.permission.BLUETOOTH_SCAN" {
				c.Android.Permissions[i] = name
			}
		}
		c.addPermission("android.permission.ACCESS_FINE_LOCATION")
		c.addPermission("android.permission.ACCESS_COARSE_LOCATION")
		if c.IOS.Usage == nil {
			c.IOS.Usage = map[string]string{}
		}
		if c.IOS.Usage["NSLocationWhenInUseUsageDescription"] == "" {
			c.IOS.Usage["NSLocationWhenInUseUsageDescription"] = "To find the Bluetooth beacons near you."
		}
	}
	addMode := func(m string) {
		if c.plistArrays == nil {
			c.plistArrays = map[string][]string{}
		}
		if !slices.Contains(c.plistArrays["UIBackgroundModes"], m) {
			c.plistArrays["UIBackgroundModes"] = append(c.plistArrays["UIBackgroundModes"], m)
		}
	}
	for _, r := range c.Bluetooth.Background {
		switch r {
		case "central":
			addMode("bluetooth-central")
			// The background scan's results, with the app's process gone.
			x := `<receiver android:name="dev.asuka.AsukaBluetoothScanReceiver" android:exported="false" />`
			if !slices.Contains(c.androidApp, x) {
				c.androidApp = append(c.androidApp, x)
			}
		case "peripheral":
			addMode("bluetooth-peripheral")
		}
	}
}

// gradleProps are the Gradle properties naming the app's optional
// modules, and its native packages' modules (name=directory, ;-separated).
func (c *appConfig) gradleProps() []string {
	props := []string{"-PasukaModules=" + strings.Join(c.androidModules, ",")}
	if len(c.nativeModules) > 0 {
		var ms []string
		for _, m := range c.nativeModules {
			ms = append(ms, m[0]+"="+m[1])
		}
		props = append(props, "-PasukaNativeModules="+strings.Join(ms, ";"))
	}
	return props
}

func (c *appConfig) androidManifest(debug bool) string {
	var b strings.Builder
	b.WriteString("<manifest xmlns:android=\"http://schemas.android.com/apk/res/android\">\n")
	for _, p := range c.Android.Permissions {
		p, flags, _ := strings.Cut(p, "|")
		name, max, _ := strings.Cut(p, ":")
		if max != "" {
			max = fmt.Sprintf(` android:maxSdkVersion="%s"`, html.EscapeString(max))
		}
		if flags != "" {
			max += fmt.Sprintf(` android:usesPermissionFlags="%s"`, html.EscapeString(flags))
		}
		fmt.Fprintf(&b, "    <uses-permission android:name=\"%s\"%s />\n", html.EscapeString(name), max)
	}
	for _, x := range c.androidTop {
		b.WriteString("    " + x + "\n")
	}
	b.WriteString("    <application")
	if c.androidHasLocales() {
		b.WriteString(" android:localeConfig=\"@xml/asuka_locales_config\"")
	}
	if debug {
		b.WriteString(" android:usesCleartextTraffic=\"true\"")
	}
	b.WriteString(">\n")
	// The Maps SDK's key (asuka/sdk/maps), which it reads from the manifest.
	if c.Android.MapsKey != "" && slices.Contains(c.androidModules, "maps") {
		fmt.Fprintf(&b, "        <meta-data android:name=\"com.google.android.geo.API_KEY\" android:value=\"%s\" />\n", html.EscapeString(c.Android.MapsKey))
	}
	// push.Rewrite: FCM's data messages start the Go code to rewrite them.
	if c.pushRewrite {
		b.WriteString("        <meta-data android:name=\"dev.asuka.push.rewrite\" android:value=\"true\" />\n")
	}
	// The splash's theme (androidRes), which AsukaActivity leaves as it starts.
	theme := ""
	if c.splashOn() {
		theme = ` android:theme="@style/Theme.Asuka.Splash"`
	}
	fmt.Fprintf(&b, "        <activity android:name=\"dev.asuka.AsukaActivity\" android:exported=\"true\"%s>\n", theme)
	// asuka.json's shortcuts: the launcher's (androidShortcutRes).
	if len(c.Shortcuts) > 0 {
		b.WriteString("            <meta-data android:name=\"android.app.shortcuts\" android:resource=\"@xml/asuka_shortcuts\" />\n")
	}
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
	if c.ShareTarget.on && !c.shareSheet {
		b.WriteString(c.ShareTarget.intentFilters())
	}
	// asuka.json's fileTypes: opened from a file manager, or a mail's
	// attachment (AsukaActivity copies a content:// URI's file).
	b.WriteString(fileTypeFilters(c.fileTypes()))
	// asuka/sdk/nfc's tags that open the app (asuka.json's nfc).
	b.WriteString(c.nfcFilters())
	b.WriteString("        </activity>\n")
	// A share sheet (app.ShareSheet): the shares go to its Activity.
	if c.ShareTarget.on && c.shareSheet {
		b.WriteString("        <activity android:name=\"dev.asuka.AsukaShareActivity\" android:exported=\"true\">\n")
		b.WriteString(c.ShareTarget.intentFilters())
		b.WriteString("        </activity>\n")
	}
	for _, x := range c.androidApp {
		b.WriteString("        " + x + "\n")
	}
	b.WriteString(c.androidWidgetReceivers())
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

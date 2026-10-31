package main

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"html"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// What the packages an app imports and ryui.json's splash add to a build:
// Info.plist's arrays and launch screen, the entitlements a release's
// profile must have, the splash's images and colors, and Firebase's
// settings.

// splashOn: ryui.json has a splash.
func (c *appConfig) splashOn() bool { return c != nil && c.Splash.Background != "" }

// plistExtras are Info.plist's keys from the packages (UIBackgroundModes,
// BGTaskSchedulerPermittedIdentifiers) and the splash (UILaunchScreen
// from the asset catalog, compileAssets'; RyUISplash, for the host's
// copy of it that stays until the first frame).
func (c *appConfig) plistExtras() map[string]any {
	m := map[string]any{"UILaunchScreen": map[string]any{}}
	for k, vs := range c.plistArrays {
		vs = slices.Clone(vs)
		sort.Strings(vs)
		m[k] = vs
	}
	if c.splashOn() {
		ls := map[string]any{"UIColorName": "RyUISplashBackground", "UIImageRespectsSafeAreaInsets": false}
		if c.Splash.Image != "" {
			ls["UIImageName"] = "RyUISplash"
		}
		m["UILaunchScreen"] = ls
		m["RyUISplash"] = true
	}
	return m
}

// plistKeys renders keys of m as Info.plist's dict entries.
func plistKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "<key>%s</key>\n", html.EscapeString(k))
		writePlist(&b, m[k], 0)
	}
	return b.String()
}

// missingEntitlements are the entitlements the app's packages need that
// the profile's (ent, a plist file) lacks, with what each is for.
func (c *appConfig) missingEntitlements(ent string) []string {
	what := map[string]string{
		"aps-environment":                                       "Push Notifications (ryui/push)",
		"com.apple.developer.applesignin":                       "Sign in with Apple (ryui/appleauth)",
		"com.apple.developer.devicecheck.appattest-environment": "App Attest (ryui/integrity)",
		"com.apple.developer.declared-age-range":                "Declared Age Range (ryui/agerange)",
	}
	data, err := os.ReadFile(ent)
	if err != nil {
		return nil
	}
	var missing []string
	for k := range c.entitlements {
		if !strings.Contains(string(data), "<key>"+k+"</key>") {
			missing = append(missing, what[k])
		}
	}
	sort.Strings(missing)
	return missing
}

// writeAssetCatalog writes the asset catalog: the app icon (if icon) and
// the splash's image and background (if the config has one). It returns
// the catalog's directory.
func writeAssetCatalog(c *appConfig, work string, icon bool) (string, error) {
	cat := filepath.Join(work, "Assets.xcassets")
	os.RemoveAll(cat)
	if err := os.MkdirAll(cat, 0o755); err != nil {
		return "", err
	}
	os.WriteFile(filepath.Join(cat, "Contents.json"), []byte(`{"info":{"author":"ryui","version":1}}`), 0o644)
	if icon {
		img, err := loadIcon(c.Icon)
		if err != nil {
			return "", err
		}
		set := filepath.Join(cat, "AppIcon.appiconset")
		if err := writePNG(filepath.Join(set, "icon.png"), opaque(resize(img, 1024))); err != nil {
			return "", err
		}
		contents := `{"images":[{"filename":"icon.png","idiom":"universal","platform":"ios","size":"1024x1024"}],"info":{"author":"ryui","version":1}}`
		if err := os.WriteFile(filepath.Join(set, "Contents.json"), []byte(contents), 0o644); err != nil {
			return "", err
		}
	}
	if !c.splashOn() {
		return cat, nil
	}
	if c.Splash.Image != "" {
		img, err := loadIcon(c.Splash.Image)
		if err != nil {
			return "", err
		}
		set := filepath.Join(cat, "RyUISplash.imageset")
		var images []any
		for _, s := range []int{1, 2, 3} {
			name := fmt.Sprintf("splash@%dx.png", s)
			if err := writePNG(filepath.Join(set, name), resize(img, c.Splash.Size*s)); err != nil {
				return "", err
			}
			images = append(images, map[string]any{"filename": name, "idiom": "universal", "scale": fmt.Sprintf("%dx", s)})
		}
		js, _ := json.Marshal(map[string]any{"images": images, "info": map[string]any{"author": "ryui", "version": 1}})
		if err := os.WriteFile(filepath.Join(set, "Contents.json"), js, 0o644); err != nil {
			return "", err
		}
	}
	color := func(hex string) map[string]any {
		r, g, b := rgb(hex)
		return map[string]any{"color-space": "srgb", "components": map[string]any{
			"red": fmt.Sprintf("0x%02X", r), "green": fmt.Sprintf("0x%02X", g), "blue": fmt.Sprintf("0x%02X", b), "alpha": "1.000"}}
	}
	colors := []any{
		map[string]any{"idiom": "universal", "color": color(c.Splash.Background)},
		map[string]any{"idiom": "universal", "color": color(c.Splash.DarkBackground),
			"appearances": []any{map[string]any{"appearance": "luminosity", "value": "dark"}}},
	}
	js, _ := json.Marshal(map[string]any{"colors": colors, "info": map[string]any{"author": "ryui", "version": 1}})
	set := filepath.Join(cat, "RyUISplashBackground.colorset")
	os.MkdirAll(set, 0o755)
	return cat, os.WriteFile(filepath.Join(set, "Contents.json"), js, 0o644)
}

// rgb reads "#rrggbb".
func rgb(hex string) (r, g, b uint8) {
	fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &g, &b)
	return
}

// androidRes writes the resources the config adds to the Android app into
// res: the splash's (a theme the manifest gives the Activity) and
// Firebase's settings (google-services.json's, as Google's Gradle plugin
// writes them). It reports whether it wrote any.
func androidRes(c *appConfig, res string) (bool, error) {
	wrote := false
	if c.splashOn() {
		if err := androidSplash(c, res); err != nil {
			return false, err
		}
		wrote = true
	}
	if c.Android.GoogleServices != "" && slices.Contains(c.androidModules, "push") {
		values, err := firebaseValues(c.Android.GoogleServices, c.ID)
		if err != nil {
			return false, err
		}
		dir := filepath.Join(res, "values")
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "ryui_firebase.xml"), []byte(values), 0o644); err != nil {
			return false, err
		}
		wrote = true
	}
	return wrote, nil
}

// androidSplash writes the splash: Android 12's (the image in the middle
// of a 288dp icon, its circle 192dp across, on the background) and,
// before, the window's background (the image in the middle of the color),
// in Theme.RyUI.Splash, which RyUIActivity leaves for Theme.RyUI.
func androidSplash(c *appConfig, res string) error {
	var img *image.NRGBA
	if c.Splash.Image != "" {
		var err error
		if img, err = loadIcon(c.Splash.Image); err != nil {
			return err
		}
	}
	size := c.Splash.Size
	for _, d := range []struct {
		dir   string
		scale float64
	}{{"mdpi", 1}, {"hdpi", 1.5}, {"xhdpi", 2}, {"xxhdpi", 3}, {"xxxhdpi", 4}} {
		dir := filepath.Join(res, "drawable-"+d.dir)
		canvas := newNRGBA(int(288 * d.scale))
		if img != nil {
			logo := resize(img, int(float64(size)*d.scale))
			if err := writePNG(filepath.Join(dir, "ryui_splash_logo.png"), logo); err != nil {
				return err
			}
			off := (canvas.Bounds().Dx() - logo.Bounds().Dx()) / 2
			for y := 0; y < logo.Bounds().Dy(); y++ {
				for x := 0; x < logo.Bounds().Dx(); x++ {
					canvas.SetNRGBA(x+off, y+off, logo.NRGBAAt(x, y))
				}
			}
		}
		if err := writePNG(filepath.Join(dir, "ryui_splash_icon.png"), canvas); err != nil {
			return err
		}
	}
	bitmap := ""
	if img != nil {
		bitmap = `
    <item><bitmap android:gravity="center" android:src="@drawable/ryui_splash_logo" /></item>`
	}
	files := map[string]string{
		"drawable/ryui_splash_window.xml": `<layer-list xmlns:android="http://schemas.android.com/apk/res/android">
    <item android:drawable="@color/ryui_splash_background" />` + bitmap + `
</layer-list>
`,
		"values/ryui_splash.xml": fmt.Sprintf(`<resources>
    <color name="ryui_splash_background">%s</color>
    <style name="Theme.RyUI.Splash" parent="Theme.RyUI">
        <item name="android:windowBackground">@drawable/ryui_splash_window</item>
    </style>
</resources>
`, strings.ToUpper(c.Splash.Background)),
		"values-night/ryui_splash.xml": fmt.Sprintf(`<resources>
    <color name="ryui_splash_background">%s</color>
</resources>
`, strings.ToUpper(c.Splash.DarkBackground)),
		"values-v31/ryui_splash.xml": `<resources>
    <style name="Theme.RyUI.Splash" parent="Theme.RyUI">
        <item name="android:windowSplashScreenBackground">@color/ryui_splash_background</item>
        <item name="android:windowSplashScreenAnimatedIcon">@drawable/ryui_splash_icon</item>
    </style>
</resources>
`,
	}
	for name, text := range files {
		p := filepath.Join(res, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// firebaseValues are google-services.json's settings for the app id, as
// the resources Firebase reads at launch (google_app_id, ...): what the
// google-services Gradle plugin writes.
func firebaseValues(path, id string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var g struct {
		ProjectInfo struct {
			ProjectNumber string `json:"project_number"`
			ProjectID     string `json:"project_id"`
			StorageBucket string `json:"storage_bucket"`
			FirebaseURL   string `json:"firebase_url"`
		} `json:"project_info"`
		Client []struct {
			ClientInfo struct {
				MobileSDKAppID    string `json:"mobilesdk_app_id"`
				AndroidClientInfo struct {
					PackageName string `json:"package_name"`
				} `json:"android_client_info"`
			} `json:"client_info"`
			APIKey []struct {
				CurrentKey string `json:"current_key"`
			} `json:"api_key"`
		} `json:"client"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return "", fmt.Errorf("%s: %v", path, err)
	}
	pkg := androidPackage(id)
	for _, cl := range g.Client {
		if cl.ClientInfo.AndroidClientInfo.PackageName != pkg {
			continue
		}
		v := map[string]string{
			"google_app_id":         cl.ClientInfo.MobileSDKAppID,
			"gcm_defaultSenderId":   g.ProjectInfo.ProjectNumber,
			"project_id":            g.ProjectInfo.ProjectID,
			"google_storage_bucket": g.ProjectInfo.StorageBucket,
			"firebase_database_url": g.ProjectInfo.FirebaseURL,
		}
		if len(cl.APIKey) > 0 {
			v["google_api_key"] = cl.APIKey[0].CurrentKey
			v["google_crash_reporting_api_key"] = cl.APIKey[0].CurrentKey
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("<resources>\n")
		for _, k := range keys {
			if v[k] != "" {
				fmt.Fprintf(&b, "    <string name=\"%s\" translatable=\"false\">%s</string>\n", k, html.EscapeString(v[k]))
			}
		}
		b.WriteString("</resources>\n")
		return b.String(), nil
	}
	return "", fmt.Errorf("%s has no Android app %s: add it to the Firebase project, then download the file again", path, pkg)
}

// dirDigest is a digest of the files under dir: their names and contents.
func dirDigest(dir string) string {
	h := crc32.NewIEEE()
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		h.Write([]byte(p))
		if data, err := os.ReadFile(p); err == nil {
			h.Write(data)
		}
		return nil
	})
	return fmt.Sprintf("%08x", h.Sum32())
}

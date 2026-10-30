package main

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// androidABIs are the ABIs a release build carries: phones (arm64), old
// phones (armv7) and emulators and Chromebooks (x86_64).
var androidABIs = []struct{ abi, goarch, goarm, clang string }{
	{"arm64-v8a", "arm64", "", "aarch64-linux-android"},
	{"armeabi-v7a", "arm", "7", "armv7a-linux-androideabi"},
	{"x86_64", "amd64", "", "x86_64-linux-android"},
}

// buildAndroid makes a release build: an .aab for Google Play and an
// .apk to install directly, both signed with the upload key, the Go code
// built for every ABI, the assets and icons packaged, and the Kotlin
// shrunk by R8.
func buildAndroid(app *goApp, o *buildOptions) error {
	start := time.Now()
	cfg := app.cfg
	tc, err := newAndroidToolchain(o.verbose)
	if err != nil {
		return err
	}
	work, err := workDir(app, "android-release")
	if err != nil {
		return err
	}
	// The upload key: nui.json, or the environment (CI).
	keystore := firstOf(os.Getenv("NUI_KEYSTORE"), cfg.Android.Keystore)
	alias := firstOf(os.Getenv("NUI_KEY_ALIAS"), cfg.Android.KeyAlias)
	storePass := os.Getenv("NUI_KEYSTORE_PASSWORD")
	if keystore == "" || alias == "" || storePass == "" {
		return errors.New(`a release build is signed with your upload key; make one (once, and keep it safe):
  keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
then name it in nui.json ("android": {"keystore": "upload.jks", "keyAlias": "upload"})
and give its password in NUI_KEYSTORE_PASSWORD (and NUI_KEY_PASSWORD if the key's differs)`)
	}
	fmt.Printf("nui: building %s %s (%d) for Android\n", cfg.Name, cfg.Version, cfg.Build)

	jni := filepath.Join(work, "jniLibs")
	os.RemoveAll(jni)
	ov, err := writeOverlay(app, work)
	if err != nil {
		return err
	}
	for _, a := range androidABIs {
		out := filepath.Join(jni, a.abi, "libnui.so")
		env := []string{"GOOS=android", "GOARCH=" + a.goarch, "CGO_ENABLED=1",
			"CC=" + filepath.Join(tc.ndkBin, a.clang+androidMinAPI+"-clang"),
			"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
		if a.goarm != "" {
			env = append(env, "GOARM="+a.goarm)
		}
		if err := tc.run(app.dir, env, "go", "build", "-buildmode=c-shared", "-trimpath", app.cfg.ldflags(true),
			"-overlay", ov, "-o", out, "."); err != nil {
			return fmt.Errorf("%s: %v", a.abi, err)
		}
		os.Remove(strings.TrimSuffix(out, ".so") + ".h")
	}

	res := filepath.Join(work, "res")
	os.RemoveAll(res)
	icon := "@android:drawable/sym_def_app_icon"
	if cfg.Icon != "" {
		if err := androidIcons(cfg.Icon, res); err != nil {
			return err
		}
		icon = "@mipmap/ic_launcher"
	} else {
		fmt.Println("nui: no icon in nui.json; Google Play needs one (512×512 or larger PNG)")
	}
	manifest := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(cfg.androidManifest(false)), 0o644); err != nil {
		return err
	}

	src, _, err := prepareAndroidHost(app, work)
	if err != nil {
		return err
	}
	args := []string{":app:bundleRelease", ":app:assembleRelease",
		"-PnuiApplicationId=" + androidPackage(cfg.ID), "-PnuiAppName=" + cfg.Name,
		"-PnuiVersionCode=" + strconv.Itoa(cfg.Build), "-PnuiVersionName=" + cfg.Version,
		"-PnuiIcon=" + icon, "-PnuiJniLibs=" + jni, "-PnuiRes=" + res, "-PnuiManifest=" + manifest,
		"-PnuiKeystore=" + keystore, "-PnuiKeyAlias=" + alias, cfg.gradleModules()}
	// Passwords go in the environment, not on a command line.
	secrets := []string{"ORG_GRADLE_PROJECT_nuiKeystorePassword=" + storePass}
	if kp := os.Getenv("NUI_KEY_PASSWORD"); kp != "" {
		secrets = append(secrets, "ORG_GRADLE_PROJECT_nuiKeyPassword="+kp)
	}
	if assets := filepath.Join(app.dir, "assets"); exists(assets) {
		args = append(args, "-PnuiAssets="+assets)
	}
	fmt.Println("nui: building with Gradle")
	if err := tc.gradleEnv(src, secrets, args...); err != nil {
		return err
	}
	name := safeName(cfg.Name)
	outs := map[string]string{
		filepath.Join(src, "app", "build", "outputs", "bundle", "release", "app-release.aab"): filepath.Join(o.out, name+".aab"),
		filepath.Join(src, "app", "build", "outputs", "apk", "release", "app-release.apk"):    filepath.Join(o.out, name+".apk"),
	}
	for from, to := range outs {
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return err
		}
		fmt.Println("nui:", to)
	}
	fmt.Printf("nui: built in %.0fs; upload the .aab in the Play Console (or with the Play Developer API)\n", time.Since(start).Seconds())
	return nil
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// androidIcons writes the launcher icons: a legacy icon per density and
// an adaptive one (Android 8+), whose foreground is the icon inside the
// safe zone and whose background is the icon's corner color.
func androidIcons(path, res string) error {
	icon, err := loadIcon(path)
	if err != nil {
		return err
	}
	densities := []struct {
		dir   string
		scale float64
	}{{"mdpi", 1}, {"hdpi", 1.5}, {"xhdpi", 2}, {"xxhdpi", 3}, {"xxxhdpi", 4}}
	for _, d := range densities {
		dir := filepath.Join(res, "mipmap-"+d.dir)
		if err := writePNG(filepath.Join(dir, "ic_launcher.png"), resize(icon, int(48*d.scale))); err != nil {
			return err
		}
		// 108dp, the icon in the middle 72dp (the mask shows at least that).
		canvas := int(108 * d.scale)
		fg := newNRGBA(canvas)
		inner := resize(icon, int(72*d.scale))
		off := (canvas - inner.Bounds().Dx()) / 2
		for y := 0; y < inner.Bounds().Dy(); y++ {
			for x := 0; x < inner.Bounds().Dx(); x++ {
				fg.SetNRGBA(x+off, y+off, inner.NRGBAAt(x, y))
			}
		}
		if err := writePNG(filepath.Join(dir, "ic_launcher_foreground.png"), fg); err != nil {
			return err
		}
	}
	bg := icon.NRGBAAt(0, 0)
	if bg.A < 255 {
		bg = color.NRGBA{255, 255, 255, 255}
	}
	values := filepath.Join(res, "values")
	os.MkdirAll(values, 0o755)
	if err := os.WriteFile(filepath.Join(values, "nui_icon.xml"), []byte(fmt.Sprintf(`<resources>
    <color name="nui_icon_background">#%02X%02X%02X</color>
</resources>
`, bg.R, bg.G, bg.B)), 0o644); err != nil {
		return err
	}
	anydpi := filepath.Join(res, "mipmap-anydpi-v26")
	os.MkdirAll(anydpi, 0o755)
	return os.WriteFile(filepath.Join(anydpi, "ic_launcher.xml"), []byte(`<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/nui_icon_background" />
    <foreground android:drawable="@mipmap/ic_launcher_foreground" />
</adaptive-icon>
`), 0o644)
}

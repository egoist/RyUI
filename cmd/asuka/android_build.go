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

// buildAndroid makes a release build: an .aab for Google Play, signed
// with the upload key, the Go code built for every ABI, the assets and
// icons packaged, and the Kotlin shrunk by R8. No .apk: Google Play makes
// each device's from the .aab, with only that device's ABI and density.
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
	// The upload key: asuka.json, or the environment (CI).
	keystore := firstOf(os.Getenv("ASUKA_KEYSTORE"), cfg.Android.Keystore)
	alias := firstOf(os.Getenv("ASUKA_KEY_ALIAS"), cfg.Android.KeyAlias)
	storePass := os.Getenv("ASUKA_KEYSTORE_PASSWORD")
	if keystore == "" || alias == "" || storePass == "" {
		return errors.New(`a release build is signed with your upload key; make one (once, and keep it safe):
  keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
then name it in asuka.json ("android": {"keystore": "upload.jks", "keyAlias": "upload"})
and give its password in ASUKA_KEYSTORE_PASSWORD (and ASUKA_KEY_PASSWORD if the key's differs)`)
	}
	fmt.Printf("asuka: building %s %s (%d) for Android\n", cfg.Name, cfg.Version, cfg.Build)

	jni, err := buildAndroidLibs(app, tc, work)
	if err != nil {
		return err
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
		fmt.Println("asuka: no icon in asuka.json; Google Play needs one (512×512 or larger PNG)")
	}
	if _, err := androidRes(cfg, res); err != nil {
		return err
	}
	manifest := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(cfg.androidManifest(false)), 0o644); err != nil {
		return err
	}

	// The host's sources and Gradle build are shared: one build at a time.
	unlock, err := lockShared(androidHostDir(work))
	if err != nil {
		return err
	}
	defer unlock()
	src, _, err := prepareAndroidHost(app, work)
	if err != nil {
		return err
	}
	args := []string{":app:bundleRelease",
		"-PasukaApplicationId=" + androidPackage(cfg.ID), "-PasukaAppName=" + cfg.androidLabel(),
		"-PasukaVersionCode=" + strconv.Itoa(cfg.Build), "-PasukaVersionName=" + cfg.Version,
		"-PasukaIcon=" + icon, "-PasukaJniLibs=" + jni, "-PasukaRes=" + res, "-PasukaManifest=" + manifest,
		"-PasukaKeystore=" + keystore, "-PasukaKeyAlias=" + alias, "-PasukaNdkVersion=" + filepath.Base(tc.ndk)}
	args = append(args, cfg.gradleProps()...)
	// Passwords go in the environment, not on a command line.
	secrets := []string{"ORG_GRADLE_PROJECT_asukaKeystorePassword=" + storePass}
	if kp := os.Getenv("ASUKA_KEY_PASSWORD"); kp != "" {
		secrets = append(secrets, "ORG_GRADLE_PROJECT_asukaKeyPassword="+kp)
	}
	if assets := filepath.Join(app.dir, "assets"); exists(assets) {
		args = append(args, "-PasukaAssets="+assets)
	}
	fmt.Println("asuka: building with Gradle")
	if err := tc.gradleEnv(src, secrets, args...); err != nil {
		return err
	}
	name := safeName(cfg.Name)
	built := filepath.Join(src, "app", "build", "outputs")
	outs := []struct {
		from, to string
		optional bool
	}{
		{filepath.Join(built, "bundle", "release", "app-release.aab"), filepath.Base(aabPath(cfg, o)), false},
		// For a crash service (the .aab has both for Google Play): the Go
		// code's and the host's native symbols, and R8's map of the
		// Kotlin it renamed.
		{filepath.Join(built, "native-debug-symbols", "release", "native-debug-symbols.zip"), name + "-native-debug-symbols.zip", true},
		{filepath.Join(built, "mapping", "release", "mapping.txt"), name + "-mapping.txt", true},
	}
	for _, f := range outs {
		data, err := os.ReadFile(f.from)
		if err != nil {
			if f.optional {
				continue
			}
			return err
		}
		to := filepath.Join(o.out, f.to)
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return err
		}
		fmt.Println("asuka:", to)
	}
	fmt.Printf("asuka: built in %.0fs; asuka publish --platform android builds and uploads it to Google Play\n", time.Since(start).Seconds())
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
	if err := os.WriteFile(filepath.Join(values, "asuka_icon.xml"), []byte(fmt.Sprintf(`<resources>
    <color name="asuka_icon_background">#%02X%02X%02X</color>
</resources>
`, bg.R, bg.G, bg.B)), 0o644); err != nil {
		return err
	}
	anydpi := filepath.Join(res, "mipmap-anydpi-v26")
	os.MkdirAll(anydpi, 0o755)
	return os.WriteFile(filepath.Join(anydpi, "ic_launcher.xml"), []byte(`<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/asuka_icon_background" />
    <foreground android:drawable="@mipmap/ic_launcher_foreground" />
</adaptive-icon>
`), 0o644)
}

// buildAndroidLibs builds the app's Go code as a shared library for each
// ABI a release carries, in work/jniLibs/<abi>/libasuka.so, with its
// plugins' libraries beside it (plugins.go).
func buildAndroidLibs(app *goApp, tc *androidToolchain, work string) (string, error) {
	cfg := app.cfg
	jni := filepath.Join(work, "jniLibs")
	os.RemoveAll(jni)
	ov, err := writeOverlay(app, work)
	if err != nil {
		return "", err
	}
	for _, a := range androidABIs {
		out := filepath.Join(jni, a.abi, "libasuka.so")
		env := []string{"GOOS=android", "GOARCH=" + a.goarch, "CGO_ENABLED=1",
			"CC=" + filepath.Join(tc.ndkBin, a.clang+androidMinAPI+"-clang"),
			"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
		if a.goarm != "" {
			env = append(env, "GOARM="+a.goarm)
		}
		// The symbol table stays, for the native debug symbols: Gradle
		// strips the libraries it packages (with the NDK the build names).
		if err := tc.run(app.dir, env, "go", "build", "-buildmode=c-shared", "-trimpath", cfg.ldflagsStrip("-w"),
			"-overlay", ov, "-o", out, "."); err != nil {
			return "", fmt.Errorf("%s: %v", a.abi, err)
		}
		os.Remove(strings.TrimSuffix(out, ".so") + ".h")
		libs, err := appLibs(app.dir, "android", a.goarch, "android-"+a.goarch)
		if err != nil {
			return "", fmt.Errorf("%s: %v", a.abi, err)
		}
		if err := writeLibs(libs, filepath.Dir(out)); err != nil {
			return "", fmt.Errorf("%s: %v", a.abi, err)
		}
		cfg.shareSheet = hasSheet(out)
		cfg.pushRewrite = hasSymbol(out, rewriteSymbol)
		cfg.useLocation(func(s string) bool { return hasSymbol(out, s) })
	}
	return jni, nil
}

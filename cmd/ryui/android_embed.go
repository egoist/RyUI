package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// An app embedded in another (ryui build -platform android -aar): the host library
// with the app's Go code for every ABI and its assets, RyUIActivity
// declared in its manifest, published to a Maven repository in dist/maven
// with the optional modules the app uses, so an app made in Android Studio
// adds the repository and the dependency, and starts the RyUI app:
//
//	startActivity(Intent(this, dev.ryui.RyUIActivity::class.java))
//
// Its dependencies (Material, AndroidX) come through the repository's POMs.

func buildAndroidAAR(app *goApp, o *buildOptions) error {
	start := time.Now()
	cfg := app.cfg
	tc, err := newAndroidToolchain(o.verbose)
	if err != nil {
		return err
	}
	work, err := workDir(app, "android-aar")
	if err != nil {
		return err
	}
	fmt.Printf("ryui: building %s %s as a library to embed\n", cfg.Name, cfg.Version)
	jni, err := buildAndroidLibs(app, tc, work)
	if err != nil {
		return err
	}
	unlock, err := lockShared(androidHostDir(work))
	if err != nil {
		return err
	}
	defer unlock()
	src, _, err := prepareAndroidHost(app, work)
	if err != nil {
		return err
	}
	// The library's manifest, with the activity the embedding app starts.
	lib, err := os.ReadFile(filepath.Join(src, "ryui", "src", "main", "AndroidManifest.xml"))
	if err != nil {
		return err
	}
	activity := `        <!-- ryui build -aar: the RyUI app, which the embedding app starts. -->
        <activity
            android:name="dev.ryui.RyUIActivity"
            android:exported="false"
            android:theme="@style/Theme.RyUI"
            android:resizeableActivity="true"
            android:configChanges="orientation|screenSize|screenLayout|smallestScreenSize|keyboard|keyboardHidden|navigation|uiMode|fontScale|density|locale|layoutDirection"
            android:windowSoftInputMode="adjustResize" />
    </application>`
	manifest := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(strings.Replace(string(lib), "    </application>", activity, 1)), 0o644); err != nil {
		return err
	}
	repo := filepath.Join(o.out, "maven")
	os.RemoveAll(repo)
	artifact := strings.ToLower(safeName(cfg.Name))
	group := androidPackage(cfg.ID)
	version := firstOf(cfg.Version, "1.0")
	tasks := []string{":ryui:publishReleasePublicationToEmbedRepository"}
	coords := []string{group + ":" + artifact + ":" + version}
	for _, m := range cfg.androidModules {
		tasks = append(tasks, ":ryui-"+m+":publishReleasePublicationToEmbedRepository")
		coords = append(coords, group+":ryui-"+m+":"+version)
	}
	args := append(tasks, "-PryuiMavenRepo="+repo, "-PryuiJniLibs="+jni, "-PryuiManifest="+manifest,
		"-PryuiGroup="+group, "-PryuiArtifact="+artifact, "-PryuiVersion="+version,
		"-PryuiNdkVersion="+filepath.Base(tc.ndk))
	if assets := filepath.Join(app.dir, "assets"); exists(assets) {
		args = append(args, "-PryuiAssets="+assets)
	}
	fmt.Println("ryui: building with Gradle")
	if err := tc.gradleEnv(src, nil, args...); err != nil {
		return err
	}
	aar := filepath.Join(repo, strings.ReplaceAll(group, ".", string(filepath.Separator)), artifact, version, artifact+"-"+version+".aar")
	if !exists(aar) {
		return fmt.Errorf("no .aar at %s", aar)
	}
	fmt.Printf("ryui: %s in %s\n", repo, time.Since(start).Round(time.Second))
	fmt.Printf("ryui: in your app's Gradle: repositories { maven { url = uri(%q) } }\n", repo)
	for _, c := range coords {
		fmt.Printf("ryui:   dependencies { implementation(%q) }\n", c)
	}
	fmt.Println("ryui: then: startActivity(Intent(this, dev.ryui.RyUIActivity::class.java))")
	return nil
}

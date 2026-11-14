package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/asuka/internal/fileurl"
)

// An app embedded in another (asuka build --platform android --aar): the host library
// with the app's Go code for every ABI and its assets, AsukaActivity
// declared in its manifest, published to a Maven repository in dist/maven
// with the optional modules the app uses, so an app made in Android Studio
// adds the repository and the dependency, and starts the Asuka app:
//
//	startActivity(Intent(this, dev.asuka.AsukaActivity::class.java))
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
	fmt.Printf("asuka: building %s %s as a library to embed\n", cfg.Name, cfg.Version)
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
	lib, err := os.ReadFile(filepath.Join(src, "asuka", "src", "main", "AndroidManifest.xml"))
	if err != nil {
		return err
	}
	manifest := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(aarManifest(string(lib))), 0o644); err != nil {
		return err
	}
	repo := filepath.Join(o.out, "maven")
	os.RemoveAll(repo)
	artifact := strings.ToLower(safeName(cfg.Name))
	group := androidPackage(cfg.ID)
	version := firstOf(cfg.Version, "1.0")
	tasks := []string{":asuka:publishReleasePublicationToEmbedRepository"}
	coords := []string{group + ":" + artifact + ":" + version}
	for _, m := range cfg.androidModules {
		tasks = append(tasks, ":asuka-"+m+":publishReleasePublicationToEmbedRepository")
		coords = append(coords, group+":asuka-"+m+":"+version)
	}
	args := append(tasks, "-PasukaMavenRepo="+repo, "-PasukaJniLibs="+jni, "-PasukaManifest="+manifest,
		"-PasukaGroup="+group, "-PasukaArtifact="+artifact, "-PasukaVersion="+version,
		"-PasukaNdkVersion="+filepath.Base(tc.ndk))
	if assets := filepath.Join(app.dir, "assets"); exists(assets) {
		args = append(args, "-PasukaAssets="+assets)
	}
	fmt.Println("asuka: building with Gradle")
	if err := tc.gradleEnv(src, nil, args...); err != nil {
		return err
	}
	aar := filepath.Join(repo, strings.ReplaceAll(group, ".", string(filepath.Separator)), artifact, version, artifact+"-"+version+".aar")
	if !exists(aar) {
		return fmt.Errorf("no .aar at %s", aar)
	}
	fmt.Printf("asuka: %s in %s\n", repo, time.Since(start).Round(time.Second))
	// A file: URL, which Gradle reads the same on every system (a Windows
	// path in a Kotlin string would need its backslashes doubled).
	abs, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	fmt.Printf("asuka: in your app's Gradle: repositories { maven { url = uri(%q) } }\n", fileurl.FromPath(abs))
	for _, c := range coords {
		fmt.Printf("asuka:   dependencies { implementation(%q) }\n", c)
	}
	fmt.Println("asuka: then: startActivity(Intent(this, dev.asuka.AsukaActivity::class.java))")
	return nil
}

// aarManifest is the library's manifest lib with the activity the
// embedding app starts. (No "--" in its comment: XML ends a comment at
// one, and Gradle's manifest merger refuses the file.)
func aarManifest(lib string) string {
	activity := `        <!-- asuka build, for an .aar: the Asuka app, which the embedding app starts. -->
        <activity
            android:name="dev.asuka.AsukaActivity"
            android:exported="false"
            android:theme="@style/Theme.Asuka"
            android:resizeableActivity="true"
            android:configChanges="orientation|screenSize|screenLayout|smallestScreenSize|keyboard|keyboardHidden|navigation|uiMode|fontScale|density|locale|layoutDirection"
            android:windowSoftInputMode="adjustResize" />
    </application>`
	return strings.Replace(lib, "    </application>", activity, 1)
}

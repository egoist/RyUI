package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/asuka/internal/androidsdk"
	androidHost "github.com/egoist/asuka/platform/android"
)

// fakeTree makes the files (slashed paths under root).
func fakeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// A Windows PC as Android Studio leaves it, under paths with spaces: the
// toolchain is its tools by their Windows names, cgo's CC the NDK's
// clang.exe (quoted) with its target, and Gradle gradlew.bat with no
// property on its command line.
func TestAndroidToolchainWindows(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "Users", "Ada Lovelace")
	local := filepath.Join(home, "AppData", "Local")
	pf := filepath.Join(root, "Program Files")
	sdk := filepath.Join(local, "Android", "Sdk")
	fakeTree(t, sdk,
		"platform-tools/adb.exe",
		"emulator/emulator.exe",
		"ndk/26.3.11579264/toolchains/llvm/prebuilt/windows-x86_64/bin/clang.exe",
		"ndk/27.2.12479018/toolchains/llvm/prebuilt/windows-x86_64/bin/clang.exe",
	)
	fakeTree(t, pf, "Android/Android Studio/jbr/bin/java.exe")
	env := map[string]string{"LOCALAPPDATA": local, "ProgramFiles": pf, "ProgramW6432": pf}
	sys := androidsdk.System{GOOS: "windows", Home: home, Getenv: func(k string) string { return env[k] }}
	tc, err := newAndroidToolchainOn(sys, false)
	if err != nil {
		t.Fatal(err)
	}
	ndk := filepath.Join(sdk, "ndk", "27.2.12479018")
	clang := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", "windows-x86_64", "bin", "clang.exe")
	for _, c := range []struct{ what, got, want string }{
		{"sdk", tc.sdk, sdk},
		{"ndk", tc.ndk, ndk},
		{"clang", tc.clang, clang},
		{"adb", tc.adb, filepath.Join(sdk, "platform-tools", "adb.exe")},
		{"emulator", tc.emulator, filepath.Join(sdk, "emulator", "emulator.exe")},
		{"JAVA_HOME", tc.javaHome, filepath.Join(pf, "Android", "Android Studio", "jbr")},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.what, c.got, c.want)
		}
	}

	// Every ABI's CC: clang.exe itself, quoted, with the target triple and
	// API level (no NDK .cmd wrapper, no cmd.exe).
	for _, a := range androidABIs {
		env := tc.cgoEnv(a.goarch, a.goarm, a.clang)
		want := `CC="` + clang + `" --target=` + a.clang + androidMinAPI
		if !slices.Contains(env, want) {
			t.Errorf("%s: %q has no %s", a.abi, env, want)
		}
		if a.goarm != "" && !slices.Contains(env, "GOARM="+a.goarm) {
			t.Errorf("%s: %q has no GOARM", a.abi, env)
		}
	}

	// Gradle: the wrapper's batch file; the properties, whatever their
	// characters, in its environment.
	src := filepath.Join(root, "android-host")
	name, args, genv := tc.gradleCommand(src, []string{"ORG_GRADLE_PROJECT_asukaKeystorePassword=p%a&s\"s"},
		[]string{":app:bundleRelease", `-PasukaAppName=Tom & "Jerry" 100%`, "-PasukaManifest=" + filepath.Join(home, "x", "AndroidManifest.xml"), "-PasukaModules="})
	if want := filepath.Join(src, "gradlew.bat"); name != want {
		t.Errorf("gradle: %q, want %q", name, want)
	}
	if want := []string{"-q", ":app:bundleRelease"}; !slices.Equal(args, want) {
		t.Errorf("gradle's arguments: %q, want %q", args, want)
	}
	for _, want := range []string{
		"ANDROID_HOME=" + sdk,
		"JAVA_HOME=" + filepath.Join(pf, "Android", "Android Studio", "jbr"),
		"ORG_GRADLE_PROJECT_asukaKeystorePassword=p%a&s\"s",
		`ORG_GRADLE_PROJECT_asukaAppName=Tom & "Jerry" 100%`,
		"ORG_GRADLE_PROJECT_asukaManifest=" + filepath.Join(home, "x", "AndroidManifest.xml"),
		"ORG_GRADLE_PROJECT_asukaModules=",
	} {
		if !slices.Contains(genv, want) {
			t.Errorf("gradle's environment %q has no %s", genv, want)
		}
	}
}

// On a Mac the same: gradlew, clang, adb by their own names.
func TestAndroidToolchainMac(t *testing.T) {
	home := t.TempDir()
	sdk := filepath.Join(home, "Library", "Android", "sdk")
	fakeTree(t, sdk, "platform-tools/adb", "ndk/27.2.12479018/toolchains/llvm/prebuilt/darwin-x86_64/bin/clang")
	sys := androidsdk.System{GOOS: "darwin", Home: home, Root: t.TempDir(), Getenv: func(string) string { return "" }}
	tc, err := newAndroidToolchainOn(sys, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(sdk, "platform-tools", "adb"); tc.adb != want {
		t.Errorf("adb: %q, want %q", tc.adb, want)
	}
	clang := filepath.Join(sdk, "ndk", "27.2.12479018", "toolchains", "llvm", "prebuilt", "darwin-x86_64", "bin", "clang")
	if env := tc.cgoEnv("arm64", "", "aarch64-linux-android"); !slices.Contains(env, "CC="+clang+" --target=aarch64-linux-android"+androidMinAPI) {
		t.Errorf("cgo: %q", env)
	}
	if name, _, _ := tc.gradleCommand("/h", nil, nil); name != filepath.Join("/h", "gradlew") {
		t.Errorf("gradle: %q", name)
	}
	if tc.javaHome != "" {
		t.Errorf("JAVA_HOME: %q, want none (gradlew's java)", tc.javaHome)
	}
}

func TestNoAndroidSDK(t *testing.T) {
	sys := androidsdk.System{GOOS: "windows", Home: t.TempDir(), Getenv: func(string) string { return "" }}
	if _, err := newAndroidToolchainOn(sys, false); err == nil || !strings.Contains(err.Error(), "ANDROID_HOME") {
		t.Errorf("no SDK: %v", err)
	}
}

// The host's Gradle wrapper for Windows is embedded with CRLF line ends
// (cmd.exe's labels need them), whatever git did to the checkout.
func TestGradlewBat(t *testing.T) {
	b, err := androidHost.Files.ReadFile("gradlew.bat")
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(b, []byte("\n")); n == 0 || bytes.Count(b, []byte("\r\n")) != n {
		t.Errorf("gradlew.bat: %d lines, %d end in CRLF", n, bytes.Count(b, []byte("\r\n")))
	}
	if !bytes.Contains(b, []byte(`gradle\wrapper\gradle-wrapper.jar`)) {
		t.Error("gradlew.bat doesn't run the embedded wrapper")
	}
	if _, err := androidHost.Files.ReadFile("gradle/wrapper/gradle-wrapper.jar"); err != nil {
		t.Error(err)
	}
}

// The --aar build's manifest is the library's with AsukaActivity
// declared, and is XML that parses (Gradle's manifest merger refuses one
// with "--" in a comment).
func TestAARManifest(t *testing.T) {
	lib, err := androidHost.Files.ReadFile("asuka/src/main/AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	m := aarManifest(string(lib))
	if !strings.Contains(m, `android:name="dev.asuka.AsukaActivity"`) {
		t.Fatal("no AsukaActivity")
	}
	d := xml.NewDecoder(strings.NewReader(m))
	for {
		if _, err := d.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("the manifest isn't XML: %v", err)
		}
	}
}

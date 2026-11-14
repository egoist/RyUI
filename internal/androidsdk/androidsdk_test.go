package androidsdk

import (
	"os"
	"path/filepath"
	"testing"
)

// touch makes the files (slashed paths under root), and their directories.
func touch(t *testing.T, root string, files ...string) {
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

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// A Windows PC as Android Studio's installer leaves it: the SDK in
// %LOCALAPPDATA%, Studio in Program Files, both under paths with spaces.
func TestWindows(t *testing.T) {
	root := filepath.Join(t.TempDir(), "C drive")
	local := filepath.Join(root, "Users", "Ada Lovelace", "AppData", "Local")
	pf := filepath.Join(root, "Program Files")
	sdk := filepath.Join(local, "Android", "Sdk")
	touch(t, sdk,
		"platform-tools/adb.exe",
		"emulator/emulator.exe",
		"ndk/9.9.9/toolchains/llvm/prebuilt/windows-x86_64/bin/clang.exe",
		"ndk/26.1.10909125/toolchains/llvm/prebuilt/windows-x86_64/bin/clang.exe",
		"ndk/27.0.12077973/toolchains/llvm/prebuilt/windows-x86_64/bin/clang.exe",
		"ndk/27.0.12077973/toolchains/llvm/prebuilt/windows-x86_64/bin/aarch64-linux-android33-clang.cmd",
		// A newer NDK with no build for Windows (copied from a Mac) isn't it.
		"ndk/28.0.13004108/toolchains/llvm/prebuilt/darwin-x86_64/bin/clang",
	)
	touch(t, pf, "Android/Android Studio/jbr/bin/java.exe")
	s := System{GOOS: "windows", Home: filepath.Join(root, "Users", "Ada Lovelace"), Getenv: envOf(map[string]string{
		"LOCALAPPDATA": local, "ProgramFiles": pf,
	})}
	if got := s.SDK(); got != sdk {
		t.Fatalf("SDK: %q, want %q", got, sdk)
	}
	if got, want := s.ADB(sdk), filepath.Join(sdk, "platform-tools", "adb.exe"); got != want {
		t.Errorf("ADB: %q, want %q", got, want)
	}
	if got, want := s.Emulator(sdk), filepath.Join(sdk, "emulator", "emulator.exe"); got != want {
		t.Errorf("Emulator: %q, want %q", got, want)
	}
	ndk, bin, err := s.NDK(sdk)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(sdk, "ndk", "27.0.12077973"); ndk != want {
		t.Errorf("NDK: %q, want %q", ndk, want)
	}
	if want := filepath.Join(sdk, "ndk", "27.0.12077973", "toolchains", "llvm", "prebuilt", "windows-x86_64", "bin"); bin != want {
		t.Errorf("NDK bin: %q, want %q", bin, want)
	}
	clang := s.Clang(bin)
	if want := filepath.Join(bin, "clang.exe"); clang != want {
		t.Errorf("Clang: %q, want %q", clang, want)
	}
	if got, want := CC(clang, "aarch64-linux-android33"), `"`+clang+`" --target=aarch64-linux-android33`; got != want {
		t.Errorf("CC: %s, want %s", got, want)
	}
	if got, want := s.Java(), filepath.Join(pf, "Android", "Android Studio", "jbr"); got != want {
		t.Errorf("Java: %q, want %q", got, want)
	}
	if got := s.Script("gradlew"); got != "gradlew.bat" {
		t.Errorf("Script: %q", got)
	}
}

// The SDK and Java from the environment win; JetBrains Toolbox's Studio
// is found when there's no JAVA_HOME.
func TestWindowsEnvAndToolbox(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "Local")
	touch(t, local,
		"JetBrains/Toolbox/apps/AndroidStudio/ch-0/223.8836.35.2231.10406996/jbr/bin/java.exe",
		"JetBrains/Toolbox/apps/AndroidStudio/ch-0/241.15989.150.2411.11948838/jbr/bin/java.exe",
	)
	s := System{GOOS: "windows", Home: root, Getenv: envOf(map[string]string{"LOCALAPPDATA": local, "ANDROID_SDK_ROOT": `D:\Android`})}
	if got := s.SDK(); got != `D:\Android` {
		t.Errorf("SDK: %q", got)
	}
	if got, want := s.Java(), filepath.Join(local, "JetBrains", "Toolbox", "apps", "AndroidStudio", "ch-0", "241.15989.150.2411.11948838", "jbr"); got != want {
		t.Errorf("Java: %q, want %q", got, want)
	}
	touch(t, local, "Programs/Android Studio/jbr/bin/java.exe") // Toolbox 2.x
	if got, want := s.Java(), filepath.Join(local, "Programs", "Android Studio", "jbr"); got != want {
		t.Errorf("Java: %q, want %q", got, want)
	}
	s.Getenv = envOf(map[string]string{"LOCALAPPDATA": local, "JAVA_HOME": `C:\jdk-21`})
	if got := s.Java(); got != `C:\jdk-21` {
		t.Errorf("Java: %q", got)
	}
	// No SDK.
	s.Getenv = envOf(map[string]string{"LOCALAPPDATA": local})
	if got := s.SDK(); got != "" {
		t.Errorf("SDK: %q, want none", got)
	}
}

func TestMac(t *testing.T) {
	home := t.TempDir()
	sdk := filepath.Join(home, "Library", "Android", "sdk")
	touch(t, sdk, "platform-tools/adb", "ndk/27.0.12077973/toolchains/llvm/prebuilt/darwin-x86_64/bin/clang")
	touch(t, home, "Applications/Android Studio.app/Contents/jbr/Contents/Home/bin/java")
	s := System{GOOS: "darwin", Home: home, Root: t.TempDir(), Getenv: envOf(nil)}
	if got := s.SDK(); got != sdk {
		t.Fatalf("SDK: %q, want %q", got, sdk)
	}
	if got, want := s.ADB(sdk), filepath.Join(sdk, "platform-tools", "adb"); got != want {
		t.Errorf("ADB: %q", got)
	}
	_, bin, err := s.NDK(sdk)
	if err != nil {
		t.Fatal(err)
	}
	clang := s.Clang(bin)
	if want := filepath.Join(sdk, "ndk", "27.0.12077973", "toolchains", "llvm", "prebuilt", "darwin-x86_64", "bin", "clang"); clang != want {
		t.Errorf("Clang: %q, want %q", clang, want)
	}
	if got := s.Java(); got != filepath.Join(home, "Applications", "Android Studio.app", "Contents", "jbr", "Contents", "Home") {
		t.Errorf("Java: %q", got)
	}
	touch(t, s.Root, "Applications/Android Studio.app/Contents/jbr/Contents/Home/bin/java")
	if got := s.Java(); got != filepath.Join(s.Root, "Applications", "Android Studio.app", "Contents", "jbr", "Contents", "Home") {
		t.Errorf("Java: %q", got)
	}
	if got := s.Script("gradlew"); got != "gradlew" {
		t.Errorf("Script: %q", got)
	}
}

func TestLinux(t *testing.T) {
	home := t.TempDir()
	sdk := filepath.Join(home, "Android", "Sdk")
	// An NDK built for Arm64 Linux (not Google's) is taken too.
	touch(t, sdk, "ndk/27.0.12077973/toolchains/llvm/prebuilt/linux-aarch64/bin/clang")
	touch(t, home, "android-studio/jbr/bin/java")
	s := System{GOOS: "linux", Home: home, Root: t.TempDir(), Getenv: envOf(nil)}
	if got := s.SDK(); got != sdk {
		t.Fatalf("SDK: %q, want %q", got, sdk)
	}
	_, bin, err := s.NDK(sdk)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(sdk, "ndk", "27.0.12077973", "toolchains", "llvm", "prebuilt", "linux-aarch64", "bin"); bin != want {
		t.Errorf("NDK bin: %q, want %q", bin, want)
	}
	if got := s.Java(); got != filepath.Join(home, "android-studio", "jbr") {
		t.Errorf("Java: %q", got)
	}
}

func TestNoNDK(t *testing.T) {
	sdk := t.TempDir()
	s := System{GOOS: "windows", Getenv: envOf(nil)}
	if _, _, err := s.NDK(sdk); err == nil {
		t.Error("no NDK: no error")
	}
	touch(t, sdk, "ndk/27.0.12077973/toolchains/llvm/prebuilt/darwin-x86_64/bin/clang")
	_, _, err := s.NDK(sdk)
	if err == nil {
		t.Fatal("no NDK for Windows: no error")
	}
	t.Log(err)
}

func TestCC(t *testing.T) {
	for _, c := range []struct{ clang, want string }{
		{"/sdk/clang", "/sdk/clang --target=x86_64-linux-android33"},
		{"/Users/a b/sdk/clang", `"/Users/a b/sdk/clang" --target=x86_64-linux-android33`},
		{`C:\Users\Ada Lovelace\clang.exe`, `"C:\Users\Ada Lovelace\clang.exe" --target=x86_64-linux-android33`},
		{`/it's "here"/clang`, `'/it's "here"/clang' --target=x86_64-linux-android33`},
	} {
		if got := CC(c.clang, "x86_64-linux-android33"); got != c.want {
			t.Errorf("CC(%q): %s, want %s", c.clang, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("9.0.1", "27.0.12077973") >= 0 || compareVersions("27.1", "27.0.9") <= 0 || compareVersions("ndk-bundle", "1") >= 0 {
		t.Error("versions out of order")
	}
}

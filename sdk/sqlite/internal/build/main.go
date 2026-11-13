// Command build builds the sqlite plugin's library, asqlite, for every
// platform: SQLite's amalgamation (the pinned release below, verified by
// its SHA3-256) and the plugin's shim (csrc), compiled the same way for
// each, and the web's WebAssembly module, into the plugin's build
// directory (sdk/sqlite/build/<target>, not checked in), each with
// the plugin's source hash (package plugin). The plugin's tests and the
// examples use that build in a checkout; asuka plugin publish
// sdk/sqlite uploads it to the plugin's release and records it in
// its manifest (asuka-plugin.json).
//
// Toolchains: Xcode's (macOS, iOS), the Android NDK (ANDROID_NDK_HOME,
// or the newest under the SDK), Zig (Linux, Windows, the web: wasm).
//
//	go generate ./sdk/sqlite                      # every target
//	go run ./sdk/sqlite/internal/build -target darwin,linux-amd64
//	go run ./cmd/asuka plugin publish sdk/sqlite  # upload, record
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha3"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/egoist/asuka/plugin"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// The SQLite release asuka/sdk/sqlite is built from.
const (
	version    = "3.54.1"
	versionNum = 3054001
	sourceURL  = "https://www.sqlite.org/2026/sqlite-amalgamation-3540100.zip"
	sourceSHA3 = "92281c86e2957f9b9305890f9ac04204caa7b677cc868dc326d942689df28e30"
)

// Minimum OS versions: the CLI's (cmd/asuka: macMin, iosMin,
// androidMinAPI).
const (
	macMin     = "11.0"
	iosMin     = "16.0"
	androidAPI = "33"
)

// options are SQLite's compile-time options, on every platform.
var options = []string{
	"-DNDEBUG",
	"-DSQLITE_DEFAULT_MEMSTATUS=0",
	"-DSQLITE_DEFAULT_WAL_SYNCHRONOUS=1",
	"-DSQLITE_LIKE_DOESNT_MATCH_BLOBS",
	"-DSQLITE_OMIT_DEPRECATED",
	"-DSQLITE_OMIT_SHARED_CACHE",
	"-DSQLITE_OMIT_LOAD_EXTENSION",
	"-DSQLITE_USE_ALLOCA",
	// Temporary tables and indices in memory: a phone may have no
	// temporary directory SQLite finds (Android has no /tmp).
	"-DSQLITE_TEMP_STORE=2",
	"-DSQLITE_ENABLE_PREUPDATE_HOOK",
	"-DSQLITE_ENABLE_FTS5",
	"-DSQLITE_ENABLE_RTREE",
	"-DSQLITE_ENABLE_GEOPOLY",
	"-DSQLITE_ENABLE_MATH_FUNCTIONS",
	"-DSQLITE_ENABLE_DBSTAT_VTAB",
	"-DSQLITE_ENABLE_STAT4",
	"-DSQLITE_ENABLE_BYTECODE_VTAB",
	"-DSQLITE_SOUNDEX",
}

// nativeOptions are the native libraries': multi-thread, as Go uses a
// connection on one goroutine at a time (asuka/sdk/sqlite's pool), so
// SQLite's own mutexes would only cost (a lock per API call).
var nativeOptions = []string{"-DSQLITE_THREADSAFE=2"}

// unixOptions: usleep (else SQLite's busy waits sleep whole seconds),
// localtime_r.
var unixOptions = []string{"-DHAVE_USLEEP=1", "-DHAVE_LOCALTIME_R=1", "-DHAVE_GMTIME_R=1", "-DHAVE_FDATASYNC=1"}

type target struct {
	name  string // the manifest's: darwin, linux-amd64...
	file  string // the library's name (the manifest's), set by main
	build func(t target, src, out string) error
}

var targets = []target{
	{"darwin", "", buildApple("macosx", []string{"arm64", "x86_64"}, "-mmacosx-version-min="+macMin, "@rpath/libasqlite.dylib")},
	{"ios", "", buildApple("iphoneos", []string{"arm64"}, "-miphoneos-version-min="+iosMin, "@rpath/asqlite.framework/asqlite")},
	{"ios-simulator", "", buildApple("iphonesimulator", []string{"arm64", "x86_64"}, "-mios-simulator-version-min="+iosMin, "@rpath/asqlite.framework/asqlite")},
	{"android-arm64", "", buildAndroid("aarch64-linux-android")},
	{"android-arm", "", buildAndroid("armv7a-linux-androideabi")},
	{"android-amd64", "", buildAndroid("x86_64-linux-android")},
	{"linux-amd64", "", buildZig("x86_64-linux-gnu.2.17")},
	{"linux-arm64", "", buildZig("aarch64-linux-gnu.2.17")},
	{"windows-amd64", "", buildZig("x86_64-windows-gnu")},
	{"windows-arm64", "", buildZig("aarch64-windows-gnu")},
	{"wasm", "", buildWasm},
}

// dir is the plugin's directory; manifest its manifest.
var (
	dir      string
	manifest *plugin.Manifest
)

func main() {
	log.SetFlags(0)
	only := flag.String("target", "", "targets to build, comma-separated (default: all)")
	flag.Parse()
	dir = filepath.Join(findRepo(), "sdk", "sqlite")
	data, err := os.ReadFile(filepath.Join(dir, plugin.ManifestFile))
	if err != nil {
		log.Fatal(err)
	}
	if manifest, err = plugin.Parse(data); err != nil {
		log.Fatal(err)
	}
	lib, ok := manifest.Library("asqlite")
	if !ok {
		log.Fatal("the manifest has no asqlite")
	}
	source, err := plugin.SourceHash(dir, manifest)
	if err != nil {
		log.Fatal(err)
	}
	src, err := amalgamation()
	if err != nil {
		log.Fatal(err)
	}
	want := map[string]bool{}
	for _, t := range strings.Split(*only, ",") {
		if t != "" {
			want[t] = true
		}
	}
	all := len(want) == 0
	for _, t := range targets {
		if !all && !want[t.name] {
			continue
		}
		delete(want, t.name)
		f, ok := lib.Files[t.name]
		if !ok {
			log.Fatalf("%s: not in the manifest", t.name)
		}
		t.file = f.Name
		tmp, err := os.MkdirTemp("", "sqlitebuild")
		if err != nil {
			log.Fatal(err)
		}
		out := filepath.Join(tmp, t.file)
		log.Printf("%s: building", t.name)
		if err := t.build(t, src, out); err != nil {
			log.Fatalf("%s: %v", t.name, err)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			log.Fatal(err)
		}
		if err := plugin.StoreBuild(dir, t.name, t.file, b, source); err != nil {
			log.Fatalf("%s: %v", t.name, err)
		}
		log.Printf("%s: %d bytes", t.name, len(b))
		os.RemoveAll(tmp)
	}
	for t := range want {
		log.Fatalf("unknown target %q", t)
	}
	v := fmt.Sprintf("// Code generated by sdk/sqlite/internal/build. DO NOT EDIT.\n\npackage sqlite\n\n// libVersion is the SQLite the library is.\nconst libVersion = %d\n", versionNum)
	if err := os.WriteFile(filepath.Join(dir, "version_gen.go"), []byte(v), 0o644); err != nil {
		log.Fatal(err)
	}
}

func findRepo() string {
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if b, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil && bytes.Contains(b, []byte("module github.com/egoist/asuka\n")) {
			return d
		}
		if filepath.Dir(d) == d {
			log.Fatal("run in the asuka repository")
		}
	}
}

// amalgamation is the directory of the verified sqlite3.c and sqlite3.h,
// downloaded once into the user's cache.
func amalgamation() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "asuka", "sqlite-src", sourceSHA3)
	if _, err := os.Stat(filepath.Join(dir, "sqlite3.c")); err == nil {
		return dir, nil
	}
	log.Printf("downloading %s", sourceURL)
	resp, err := http.Get(sourceURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", sourceURL, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if sum := sha3.Sum256(data); hex.EncodeToString(sum[:]) != sourceSHA3 {
		return "", fmt.Errorf("%s: SHA3-256 %x, want %s", sourceURL, sum, sourceSHA3)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		if name != "sqlite3.c" && name != "sqlite3.h" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// sources are what every build compiles, and its include path.
func sources(src string) []string {
	return []string{filepath.Join(src, "sqlite3.c"), filepath.Join(dir, "csrc", "asq.c"), "-I", src}
}

// common are the flags of every build: optimized, only asq's symbols
// exported (SQLite's own stay hidden: a system SQLite in the same
// process, which GTK may load, never clashes).
func common(src string) []string {
	f := []string{"-O2", "-fPIC", "-fvisibility=hidden", "-ffunction-sections", "-fdata-sections"}
	f = append(f, options...)
	return append(f, sources(src)...)
}

func run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func buildApple(sdk string, archs []string, min, installName string) func(target, string, string) error {
	return func(t target, src, out string) error {
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("needs Xcode (macOS)")
		}
		args := []string{"-sdk", sdk, "clang", "-dynamiclib", min, "-install_name", installName,
			"-Wl,-dead_strip", "-Wl,-x", "-o", out}
		for _, a := range archs {
			args = append(args, "-arch", a)
		}
		args = append(args, unixOptions...)
		args = append(args, nativeOptions...)
		args = append(args, common(src)...)
		if err := run("", nil, "xcrun", args...); err != nil {
			return err
		}
		return run("", nil, "xcrun", "strip", "-x", out)
	}
}

func ndkBin() (string, error) {
	var roots []string
	if v := os.Getenv("ANDROID_NDK_HOME"); v != "" {
		roots = append(roots, v)
	}
	sdks := []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT")}
	if home, err := os.UserHomeDir(); err == nil {
		sdks = append(sdks, filepath.Join(home, "Library", "Android", "sdk"), filepath.Join(home, "Android", "Sdk"))
	}
	for _, s := range sdks {
		if s == "" {
			continue
		}
		vs, _ := filepath.Glob(filepath.Join(s, "ndk", "*"))
		sort.Strings(vs)
		for i := len(vs) - 1; i >= 0; i-- {
			roots = append(roots, vs[i])
		}
	}
	for _, r := range roots {
		bins, _ := filepath.Glob(filepath.Join(r, "toolchains", "llvm", "prebuilt", "*", "bin"))
		if len(bins) > 0 {
			return bins[0], nil
		}
	}
	return "", fmt.Errorf("no Android NDK: set ANDROID_NDK_HOME")
}

func buildAndroid(triple string) func(target, string, string) error {
	return func(t target, src, out string) error {
		bin, err := ndkBin()
		if err != nil {
			return err
		}
		cc := filepath.Join(bin, triple+androidAPI+"-clang")
		args := []string{"-shared", "-o", out, "-Wl,-soname,libasqlite.so", "-Wl,-z,max-page-size=16384",
			"-Wl,--gc-sections", "-Wl,--build-id=none", "-s"}
		args = append(args, unixOptions...)
		args = append(args, nativeOptions...)
		args = append(args, common(src)...)
		args = append(args, "-lm")
		return run("", nil, cc, args...)
	}
}

func buildZig(triple string) func(target, string, string) error {
	return func(t target, src, out string) error {
		args := []string{"cc", "-target", triple, "-shared", "-o", out, "-s", "-Wl,--gc-sections"}
		windows := strings.Contains(triple, "windows")
		args = append(args, nativeOptions...)
		if !windows {
			args = append(args, "-Wl,-soname,libasqlite.so", "-Wl,--build-id=none")
			args = append(args, unixOptions...)
		}
		args = append(args, common(src)...)
		if !windows {
			args = append(args, "-lm", "-lpthread")
		}
		// A cache of the build's own: zig's global one may be another
		// version's.
		cache := filepath.Join(filepath.Dir(out), "zig-cache")
		if err := run("", []string{"ZIG_LOCAL_CACHE_DIR=" + cache, "ZIG_GLOBAL_CACHE_DIR=" + cache}, "zig", args...); err != nil {
			return err
		}
		// The import library zig writes beside a DLL isn't shipped.
		os.Remove(strings.TrimSuffix(out, ".dll") + ".lib")
		os.Remove(strings.TrimSuffix(out, ".dll") + ".pdb")
		return nil
	}
}

// buildWasm builds the web's module: SQLite with asq_wasm.c's system
// (SQLITE_OS_OTHER: its files, locks, time and randomness are the
// worker's JavaScript, platform/web/src/sqlite-engine.js), one thread.
// WASI's libc, none of WASI's imports (the module imports only "asq"),
// optimized for size: it's downloaded.
func buildWasm(t target, src, out string) error {
	args := []string{"cc", "-target", "wasm32-wasi", "-mexec-model=reactor", "-Os", "-o", out, "-s",
		"-ffunction-sections", "-fdata-sections", "-Wl,--gc-sections",
		// SQLite's parser and alloca want more than wasm-ld's 64 KiB.
		"-Wl,-z,stack-size=1048576",
		"-Wl,--export=asq_call", "-Wl,--export=asq_version", "-Wl,--export=malloc", "-Wl,--export=free",
		"-DSQLITE_OS_OTHER=1", "-DSQLITE_THREADSAFE=0", "-DHAVE_LOCALTIME_R=1"}
	args = append(args, options...)
	args = append(args, sources(src)...)
	args = append(args, filepath.Join(dir, "csrc", "asq_wasm.c"))
	cache := filepath.Join(filepath.Dir(out), "zig-cache")
	return run("", []string{"ZIG_LOCAL_CACHE_DIR=" + cache, "ZIG_GLOBAL_CACHE_DIR=" + cache}, "zig", args...)
}

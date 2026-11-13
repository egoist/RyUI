//go:build e2e

package e2e

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// updateFeed is a feed of the test's own (httptest), the keys its updates
// are signed with, and the requests it served.
type updateFeed struct {
	dir  string // its files
	env  []string
	mu   sync.Mutex
	gets []string
}

func newUpdateFeed(t *testing.T) *updateFeed {
	pub, priv, _ := ed25519.GenerateKey(nil)
	f := &updateFeed{dir: t.TempDir()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.gets = append(f.gets, r.URL.Path)
		f.mu.Unlock()
		http.FileServer(http.Dir(f.dir)).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.env = append(os.Environ(),
		"ASUKA_UPDATE_PUBLIC_KEY="+base64.StdEncoding.EncodeToString(pub),
		"ASUKA_UPDATE_PRIVATE_KEY="+base64.StdEncoding.EncodeToString(priv),
		"ASUKA_UPDATE_URL="+srv.URL,
	)
	return f
}

// build builds e2e/testdata/updater at version for platform with asuka
// build, and publishes its update files on the feed, the manifest last.
// It returns the build's directory.
func (f *updateFeed) build(t *testing.T, platform, version string) string {
	out := filepath.Join(t.TempDir(), "dist")
	c := exec.Command("go", "run", "./cmd/asuka", "build", "--platform", platform, "--no-vet", "--version", version, "-o", out, "e2e/testdata/updater")
	c.Dir, c.Env = root(t), f.env
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("asuka build %s: %v\n%s", version, err, b)
	}
	t.Logf("asuka build %s:\n%s", version, b)
	files, _ := filepath.Glob(filepath.Join(out, "updates", "*"))
	var manifests []string
	for _, p := range files {
		if strings.HasSuffix(p, ".json") {
			manifests = append(manifests, p)
			continue
		}
		run(t, "cp", p, f.dir)
	}
	for _, p := range manifests {
		run(t, "cp", p, f.dir)
	}
	return out
}

// downloaded returns the requests the feed served.
func (f *updateFeed) downloaded() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.gets, " ")
}

// runUpdater runs the installed app's executable with --report, waits for
// it to quit (it relaunches), then for the relaunched one's report, and
// returns the reports.
func runUpdater(t *testing.T, exe string) string {
	report := filepath.Join(t.TempDir(), "report")
	// Its output to a file: the update's helper, which outlives it, would
	// keep a pipe open.
	outFile := filepath.Join(t.TempDir(), "out")
	logf, err := os.Create(outFile)
	if err != nil {
		t.Fatal(err)
	}
	defer logf.Close()
	app := exec.Command(exe, "--report", report)
	app.Stdout, app.Stderr = logf, logf
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- app.Wait() }()
	select { // it quits as it relaunches
	case err := <-exited:
		if err != nil {
			out, _ := os.ReadFile(outFile)
			t.Fatalf("1.0.0: %v\n%s", err, out)
		}
	case <-time.After(within):
		app.Process.Kill()
		out, _ := os.ReadFile(outFile)
		b, _ := os.ReadFile(report)
		t.Fatalf("1.0.0 didn't quit; it reported:\n%s\nits output:\n%s", b, out)
	}
	deadline := time.Now().Add(within)
	var got string
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(report)
		got = string(b)
		if strings.Contains(got, "launched 1.0.1") && strings.Contains(got, "up to date") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("the app reported:\n%s", got)
	for _, want := range []string{"launched 1.0.0", "found 1.0.1", "installed 1.0.1", "launched 1.0.1", "up to date"} {
		if !strings.Contains(got, want) {
			out, _ := os.ReadFile(outFile)
			t.Fatalf("the app didn't report %q:\n%s\n1.0.0's output:\n%s", want, got, out)
		}
	}
	return got
}

// TestUpdaterMac builds e2e/testdata/updater at 1.0.0 and at 1.0.1 with
// asuka build, signing the updates with a key of the test's and publishing
// them on a feed of its own (httptest); runs 1.0.0 from an Applications
// folder, which checks, downloads the delta from 1.0.0, installs and
// relaunches: the bundle is then 1.0.1's, its code signature valid, and
// the relaunched app says it's 1.0.1.
func TestUpdaterMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS")
	}
	f := newUpdateFeed(t)
	v100 := filepath.Join(f.build(t, "mac", "1.0.0"), "Updater.app")
	f.build(t, "mac", "1.0.1") // its delta from the published 1.0.0

	apps := t.TempDir()
	installed := filepath.Join(apps, "Updater.app")
	run(t, "ditto", v100, installed)
	runUpdater(t, filepath.Join(installed, "Contents", "MacOS", "Updater"))

	if v := strings.TrimSpace(run(t, "/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", filepath.Join(installed, "Contents", "Info.plist"))); v != "1.0.1" {
		t.Errorf("the installed bundle is %s", v)
	}
	run(t, "codesign", "--verify", "--deep", "--strict", installed)
	if got := f.downloaded(); !strings.Contains(got, "-1.0.0-to-1.0.1-darwin-universal.delta") || strings.Contains(got, "updater-1.0.1-darwin-universal.tar.gz") {
		t.Errorf("downloaded %v, want the delta alone", got)
	}
	// Nothing left beside the app: the helper removed its work.
	if left, _ := filepath.Glob(filepath.Join(apps, ".*")); len(left) > 0 {
		t.Errorf("left %v", left)
	}
}

// TestUpdaterLinux does the same with the Linux build: its tarball
// unpacked into a prefix that holds another program too, as ~/.local
// does; 1.0.1 replaces the app's files alone, from the delta, and the
// relaunched app (once 1.0.0 has quit: it runs once) says it's 1.0.1.
//
//	xvfb-run -a dbus-run-session go test -tags e2e ./e2e -run TestUpdaterLinux
func TestUpdaterLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux")
	}
	f := newUpdateFeed(t)
	arch := runtime.GOARCH
	v100 := f.build(t, "linux", "1.0.0")
	f.build(t, "linux", "1.0.1")

	prefix := t.TempDir()
	os.MkdirAll(filepath.Join(prefix, "bin"), 0o755)
	os.WriteFile(filepath.Join(prefix, "bin", "other"), []byte("another program"), 0o755)
	run(t, "tar", "-xzf", filepath.Join(v100, "linux", "updater-1.0.0-linux-"+arch+".tar.gz"), "--strip-components=1", "-C", prefix)
	runUpdater(t, filepath.Join(prefix, "bin", "updater"))

	if b, _ := os.ReadFile(filepath.Join(prefix, "bin", "other")); string(b) != "another program" {
		t.Error("the update touched another program of the prefix")
	}
	list, _ := os.ReadFile(filepath.Join(prefix, "share", "updater", ".asuka-files"))
	if !strings.Contains(string(list), "bin/updater\n") || !strings.Contains(string(list), "share/applications/dev.asuka.updatertest.desktop\n") {
		t.Errorf("the prefix's file list:\n%s", list)
	}
	if got := f.downloaded(); !strings.Contains(got, "-1.0.0-to-1.0.1-linux-"+arch+".delta") || strings.Contains(got, "updater-1.0.1-linux-"+arch+".tar.gz") {
		t.Errorf("downloaded %v, want the delta alone", got)
	}
	if left, _ := filepath.Glob(filepath.Join(prefix, "share", "updater", ".asuka-update-*")); len(left) > 0 {
		t.Errorf("left %v", left)
	}
}

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
	repo := root(t)
	pub, priv, _ := ed25519.GenerateKey(nil)
	published := t.TempDir()
	var mu sync.Mutex
	var gets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gets = append(gets, r.URL.Path)
		mu.Unlock()
		http.FileServer(http.Dir(published)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	env := append(os.Environ(),
		"ASUKA_UPDATE_PUBLIC_KEY="+base64.StdEncoding.EncodeToString(pub),
		"ASUKA_UPDATE_PRIVATE_KEY="+base64.StdEncoding.EncodeToString(priv),
		"ASUKA_UPDATE_URL="+srv.URL,
	)
	build := func(version string) string {
		out := filepath.Join(t.TempDir(), "dist")
		c := exec.Command("go", "run", "./cmd/asuka", "build", "--platform", "mac", "--no-vet", "--version", version, "-o", out, "e2e/testdata/updater")
		c.Dir, c.Env = repo, env
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("asuka build %s: %v\n%s", version, err, b)
		}
		t.Logf("asuka build %s:\n%s", version, b)
		// Published: the update files, the manifest last.
		files, _ := filepath.Glob(filepath.Join(out, "updates", "*"))
		for _, f := range files {
			run(t, "cp", f, published)
		}
		return filepath.Join(out, "Updater.app")
	}
	v100 := build("1.0.0")
	build("1.0.1") // its delta from the published 1.0.0

	// Installed: 1.0.0 in an Applications folder of the test's.
	apps := t.TempDir()
	installed := filepath.Join(apps, "Updater.app")
	run(t, "ditto", v100, installed)
	report := filepath.Join(t.TempDir(), "report")
	// Its output to a file: the update's helper, which outlives it, would
	// keep a pipe open.
	outFile := filepath.Join(t.TempDir(), "out")
	logf, err := os.Create(outFile)
	if err != nil {
		t.Fatal(err)
	}
	defer logf.Close()
	app := exec.Command(filepath.Join(installed, "Contents", "MacOS", "Updater"), "--report", report)
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
	out, _ := os.ReadFile(outFile)

	// The relaunched 1.0.1 reports and quits.
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
			t.Fatalf("the app didn't report %q:\n%s\n1.0.0's output:\n%s", want, got, out)
		}
	}
	if v := strings.TrimSpace(run(t, "/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", filepath.Join(installed, "Contents", "Info.plist"))); v != "1.0.1" {
		t.Errorf("the installed bundle is %s", v)
	}
	run(t, "codesign", "--verify", "--deep", "--strict", installed)
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(gets, " ")
	if !strings.Contains(joined, "-1.0.0-to-1.0.1-darwin-universal.delta") || strings.Contains(joined, "updater-1.0.1-darwin-universal.tar.gz") {
		t.Errorf("downloaded %v, want the delta alone", gets)
	}
	// Nothing left beside the app: the helper removed its work.
	if left, _ := filepath.Glob(filepath.Join(apps, ".*")); len(left) > 0 {
		t.Errorf("left %v", left)
	}
}

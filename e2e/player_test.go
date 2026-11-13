//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The player: asuka dev without --once runs the app's Go code here and
// its views in the player app (dev.asuka.player), which the other tests
// never use. These install the player again (the path that broke when its
// link listed its plugins in a directory that isn't a Go package), show
// the counter, reload it after an edit that used to lose its state, and
// relaunch the player against the running code (where a fresh player
// was once seen to stay blank).
//
//	go test -tags e2e ./e2e -run PlayerIOS
//	go test -tags e2e ./e2e -run PlayerAndroid

// playerDev is asuka dev serving a copy of examples/counter to the
// player, and its output.
type playerDev struct {
	t    *testing.T
	dir  string // the app's copy
	dev  *exec.Cmd
	logs syncBuffer
}

// startPlayerDev copies the counter to a module of its own (Asuka
// replaced by this checkout), so the test can edit it, makes the
// player install again, and runs asuka dev for platform.
func startPlayerDev(t *testing.T, platform string) *playerDev {
	t.Helper()
	repo := root(t)
	dir := filepath.Join(t.TempDir(), "counter")
	os.MkdirAll(dir, 0o755)
	for _, f := range []string{"main.go", "asuka.json", "icon.png"} {
		b, err := os.ReadFile(filepath.Join(repo, "examples", "counter", f))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, f), b, 0o644)
	}
	gomod := "module example.com/counter\n\ngo 1.26\n\nrequire github.com/egoist/asuka v0.0.0\n\nreplace github.com/egoist/asuka => " + repo + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)
	if sum, err := os.ReadFile(filepath.Join(repo, "go.sum")); err == nil {
		os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	}
	// The player installs again: what asuka dev remembers of its installs
	// goes (one per device: they just install again next time).
	if cache, err := os.UserCacheDir(); err == nil {
		marks, _ := filepath.Glob(filepath.Join(cache, "asuka", "player", platform, "installed-*"))
		for _, m := range marks {
			os.Remove(m)
		}
	}
	cli := filepath.Join(t.TempDir(), "asuka")
	run(t, "go", "build", "-C", repo, "-o", cli, "./cmd/asuka")
	args := []string{"dev", "--platform", platform, "--no-vet"}
	if d := os.Getenv("ASUKA_DEVICE"); d != "" {
		args = append(args, "--device", d)
	}
	p := &playerDev{t: t, dir: dir}
	c := exec.Command(cli, append(args, dir)...)
	c.Env = append(os.Environ(), "GOFLAGS=-mod=mod") // the copy's requirements are the replaced Asuka's
	var out io.Writer = &p.logs
	if testing.Verbose() {
		out = io.MultiWriter(&p.logs, os.Stdout) // to follow a slow first build
	}
	c.Stdout, c.Stderr = out, out
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	p.dev = c
	t.Cleanup(p.stop)
	p.waitLog("watching for changes", 10*time.Minute) // the player's first build takes a while
	return p
}

// waitLog waits until asuka dev has printed want, failing with its output
// if it doesn't within d, or if it says a player is stuck.
func (p *playerDev) waitLog(want string, d time.Duration) {
	p.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		out := p.logs.String()
		if strings.Contains(out, "hasn't shown the app") {
			p.t.Fatalf("the player is stuck:\n%s", out)
		}
		if strings.Contains(out, want) {
			return
		}
		if p.dev.ProcessState != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	p.t.Fatalf("asuka dev didn't print %q:\n%s", want, p.logs.String())
}

// edit changes the app's main.go, and waits for the reload.
func (p *playerDev) edit(from, to string) {
	p.t.Helper()
	path := filepath.Join(p.dir, "main.go")
	b, err := os.ReadFile(path)
	if err != nil {
		p.t.Fatal(err)
	}
	if !strings.Contains(string(b), from) {
		p.t.Fatalf("main.go has no %q", from)
	}
	reloads := strings.Count(p.logs.String(), "asuka: reloaded")
	os.WriteFile(path, []byte(strings.Replace(string(b), from, to, 1)), 0o644)
	deadline := time.Now().Add(2 * time.Minute)
	for strings.Count(p.logs.String(), "asuka: reloaded") == reloads {
		if time.Now().After(deadline) {
			p.t.Fatalf("no reload after the edit:\n%s", p.logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stop stops asuka dev (SIGINT), which stops the app's run and the
// player.
func (p *playerDev) stop() {
	if p.dev.Process == nil || p.dev.ProcessState != nil {
		return
	}
	p.dev.Process.Signal(syscall.SIGINT)
	done := make(chan struct{})
	go func() { p.dev.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		p.dev.Process.Kill()
		<-done
	}
}

// playerCounter is the same test on each platform's player.
func playerCounter(t *testing.T, p *playerDev, s screen) {
	waitFor(t, s, []string{"Count: 0", "Add", "Reset"})
	for i := 1; i <= 3; i++ {
		s.tap("Add")
		waitFor(t, s, []string{fmt.Sprintf("Count: %d", i)})
	}
	// A signal of count's type made before it: the third keeps its value
	// (it's named by its variable, not its place among the ints).
	p.edit("\tcount := signal.New(0)\n", "\tstep := signal.New(1)\n\t_ = step\n\tcount := signal.New(0)\n")
	p.edit(`"Count: %d"`, `"Total: %d"`)
	waitFor(t, s, []string{"Total: 3"}, "Count: 3")
	// A fresh player against the running code, a few times: each shows
	// the app. (Its state starts over: the core keeps the last player's
	// session for that player to resume, and the new one is another.)
	for range 3 {
		s.relaunch()
		waitFor(t, s, []string{"Add", "Reset"})
	}
	if out := p.logs.String(); strings.Contains(out, "hasn't shown the app") {
		t.Fatalf("a player was stuck:\n%s", out)
	}
}

func TestPlayerIOS(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode")
	}
	p := startPlayerDev(t, "ios")
	m := regexp.MustCompile(`\(([0-9A-F-]{36})\)`).FindStringSubmatch(p.logs.String())
	if m == nil {
		t.Fatalf("no simulator in:\n%s", p.logs.String())
	}
	const bundle = "dev.asuka.player"
	data := strings.TrimSpace(run(t, "xcrun", "simctl", "get_app_container", m[1], bundle, "data"))
	s := &iosScreen{t: t, udid: m[1], bundle: bundle, tmp: filepath.Join(data, "tmp")}
	playerCounter(t, p, s)
}

func TestPlayerAndroid(t *testing.T) {
	if _, err := exec.LookPath("adb"); err != nil {
		t.Skip("no adb")
	}
	p := startPlayerDev(t, "android")
	m := regexp.MustCompile(`(?m)^asuka: \S+ on (\S+)$`).FindStringSubmatch(p.logs.String())
	if m == nil {
		t.Fatalf("no device in:\n%s", p.logs.String())
	}
	playerCounter(t, p, &androidScreen{t: t, serial: m[1], pkg: "dev.asuka.player"})
}

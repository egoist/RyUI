package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

// The player: `ryui dev` runs the app's Go code on this machine and its
// views in a player app on the simulator, emulator or device, over a
// socket (internal/remote). The player is the platform host with a small
// Go library in place of an app; it's built and installed once, shared
// by every app, and reconnects when the app's code restarts. A change
// rebuilds and restarts only the app's process here: no install.

const (
	playerID   = "dev.ryui.player"
	playerName = "ryui"
	playerPkg  = "github.com/egoist/ryui/mobile/player"
	// devicePort is where the player connects (see mobile/player): the
	// simulator shares this machine's loopback; adb reverse maps it.
	devicePort = 7461
)

// usePlayer reports whether to run app with the player: not when it has
// its own native code for platform (ios, android), which only an app
// built with it can show.
func usePlayer(app *goApp, o *devOptions, platform string) bool {
	if o.embed || o.once {
		return false
	}
	if exists(filepath.Join(app.dir, platform)) {
		fmt.Printf("ryui: %s has native code in %s/: building it into the app (no player)\n", app.name, platform)
		return false
	}
	return true
}

func freePort(want int) int {
	if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", want)); err == nil {
		l.Close()
		return want
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return want
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// hostProcess is the app's Go code running here.
type hostProcess struct {
	app  *goApp
	work string
	env  []string
	bin  string
	cmd  *exec.Cmd
	done chan struct{}
	v    bool
	// flags is the build's -ldflags (default: ryui.json's).
	flags string
	// goarch is the build's GOARCH (default: this machine's).
	goarch string
	// stdin, when the process is told to stop by closing it (Windows,
	// which can't send it SIGINT).
	stdin io.WriteCloser
}

func (p *hostProcess) build() error {
	p.bin = filepath.Join(p.work, "app-host")
	if runtime.GOOS == "windows" {
		p.bin += ".exe" // CreateProcess runs only a name with its extension
	}
	flags := p.flags
	if flags == "" {
		flags = p.app.cfg.ldflags(false)
	}
	c := exec.Command("go", "build", flags, "-o", p.bin, ".")
	c.Dir = p.app.dir
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed:\n%s", out)
	}
	return nil
}

// restart stops the running process (it saves its persisted state on
// the way out) and starts the new build.
func (p *hostProcess) restart() error {
	p.stop()
	c := exec.Command(p.bin)
	c.Dir = p.app.dir // assets/ is served from here
	c.Env = append(os.Environ(), p.env...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	p.stdin = nil
	if slices.Contains(p.env, "RYUI_STOP=stdin") {
		in, err := c.StdinPipe()
		if err != nil {
			return err
		}
		p.stdin = in
	}
	if err := c.Start(); err != nil {
		return err
	}
	p.cmd = c
	p.done = make(chan struct{})
	go func(done chan struct{}) {
		c.Wait()
		close(done)
	}(p.done)
	return nil
}

func (p *hostProcess) stop() {
	if p.cmd == nil {
		return
	}
	if p.stdin != nil {
		p.stdin.Close() // it saves its state and quits
	} else {
		p.cmd.Process.Signal(syscall.SIGINT)
	}
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		p.cmd.Process.Kill()
		<-p.done
	}
	p.cmd = nil
}

// watchHost rebuilds and restarts the app's process on each change.
// When it's done it stops the player too, so that it doesn't join the
// next session, which may be another app's.
func watchHost(p *hostProcess, stop <-chan os.Signal, stopPlayer func()) error {
	fmt.Println("ryui: watching for changes (Ctrl-C to stop)")
	w := newWatcher(p.app.moduleDir, p.app.dir)
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	defer stopPlayer()
	defer p.stop()
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			if !w.changed() {
				continue
			}
			start := time.Now()
			if err := p.build(); err != nil {
				fmt.Fprintln(os.Stderr, "ryui:", err)
				continue
			}
			if err := p.restart(); err != nil {
				fmt.Fprintln(os.Stderr, "ryui:", err)
				continue
			}
			fmt.Printf("ryui: reloaded in %.1fs\n", time.Since(start).Seconds())
		}
	}
}

func fileHash(paths ...string) string {
	h := sha256.New()
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// playerDir is where the player is built, shared by every app (beside
// the apps' work directories, so it shares their host builds).
func playerDir(work, platform string) string {
	d := filepath.Join(filepath.Dir(filepath.Dir(work)), "player", platform)
	os.MkdirAll(d, 0o755)
	return d
}

func devIOSPlayer(app *goApp, tc *iosToolchain, work string, sim *simDevice, stop <-chan os.Signal) error {
	start := time.Now()
	port := freePort(devicePort)
	proc := &hostProcess{app: app, work: work, env: []string{
		fmt.Sprintf("RYUI_REMOTE=127.0.0.1:%d", port),
		"RYUI_STATE=" + filepath.Join(work, "state.json"),
		"RYUI_DATA=" + filepath.Join(work, "data"), // the app's own: store.Dir, sqlite files
		fmt.Sprintf("RYUI_ASSETS_URL=http://127.0.0.1:%d/", port+1),
		fmt.Sprintf("RYUI_ASSETS_ADDR=127.0.0.1:%d", port+1),
	}}
	if err := proc.build(); err != nil {
		return err
	}
	// The player: the host, and the player library instead of an app.
	dir := playerDir(work, "ios")
	hostObj, err := tc.buildHost(dir, nil, allFeatures())
	if err != nil {
		return err
	}
	lib := filepath.Join(dir, "libplayer.a")
	flags := "-isysroot " + tc.sdk + " -target " + tc.target
	// The same source makes the same archive (no paths, build ID or
	// dates in it), so an unchanged player isn't installed again.
	env := []string{"GOOS=ios", "GOARCH=" + tc.goarch, "CGO_ENABLED=1", "CC=" + tc.clang, "CGO_CFLAGS=" + flags, "CGO_LDFLAGS=" + flags, "ZERO_AR_DATE=1"}
	if err := tc.run(app.dir, env, "go", "build", "-tags", "ios", "-buildmode=c-archive", "-trimpath", "-ldflags=-buildid=", "-o", lib, playerPkg); err != nil {
		return err
	}
	// Its share extension links the same (its sheet: the shared layout).
	sum := fileHash(hostObj, lib)
	marker := filepath.Join(dir, "installed-"+sim.UDID)
	// The app's container changes with each install, and is gone from an
	// erased simulator: the marker holds both.
	container := func() string {
		out, _ := exec.Command("xcrun", "simctl", "get_app_container", sim.UDID, playerID).Output()
		return strings.TrimSpace(string(out))
	}
	if prev, _ := os.ReadFile(marker); string(prev) != sum+"\n"+container() {
		player := &goApp{dir: dir, name: playerName, bundleID: playerID, cfg: playerConfig()}
		bundle, err := tc.link(player, dir, hostObj, lib)
		if err != nil {
			return err
		}
		if err := tc.run(dir, nil, "xcrun", "simctl", "install", sim.UDID, bundle); err != nil {
			return err
		}
		os.WriteFile(marker, []byte(sum+"\n"+container()), 0o644)
	}
	if err := proc.restart(); err != nil {
		return err
	}
	launch := exec.Command("xcrun", "simctl", "launch", "--terminate-running-process", sim.UDID, playerID)
	launch.Env = append(os.Environ(), fmt.Sprintf("SIMCTL_CHILD_RYUI_REMOTE=127.0.0.1:%d", port))
	if out, err := launch.CombinedOutput(); err != nil {
		proc.stop()
		return fmt.Errorf("simctl launch: %v: %s", err, out)
	}
	fmt.Printf("ryui: running in the player in %.1fs; changes reload without reinstalling\n", time.Since(start).Seconds())
	return watchHost(proc, stop, func() { exec.Command("xcrun", "simctl", "terminate", sim.UDID, playerID).Run() })
}

func devAndroidPlayer(app *goApp, tc *androidToolchain, work, serial string, stop <-chan os.Signal) error {
	start := time.Now()
	port := freePort(devicePort)
	// An emulator reaches this machine's loopback at 10.0.2.2. A device
	// goes through adb reverse, whose tunnel adds latency to every round
	// trip (and can stall on many small ones): emulators connect directly.
	emulator := strings.HasPrefix(serial, "emulator-")
	remote := fmt.Sprintf("127.0.0.1:%d", devicePort)
	assets := fmt.Sprintf("http://127.0.0.1:%d/", devicePort+1)
	if emulator {
		remote = fmt.Sprintf("10.0.2.2:%d", port)
		assets = fmt.Sprintf("http://10.0.2.2:%d/", port+1)
	}
	proc := &hostProcess{app: app, work: work, env: []string{
		fmt.Sprintf("RYUI_REMOTE=127.0.0.1:%d", port),
		"RYUI_STATE=" + filepath.Join(work, "state.json"),
		"RYUI_DATA=" + filepath.Join(work, "data"), // the app's own: store.Dir, sqlite files
		"RYUI_ASSETS_URL=" + assets,
		fmt.Sprintf("RYUI_ASSETS_ADDR=127.0.0.1:%d", port+1),
	}}
	if err := proc.build(); err != nil {
		return err
	}
	dir := playerDir(work, "android")
	player := &goApp{dir: dir, name: playerName, bundleID: playerID, cfg: playerConfig()}
	apk, hash, err := tc.buildAPK(player, dir)
	if err != nil {
		return err
	}
	if err := tc.installOnce(serial, androidPackage(playerID), apk, hash, filepath.Join(dir, "installed-"+serial)); err != nil {
		return err
	}
	goarch, clang, err := tc.goArch(serial)
	if err != nil {
		return err
	}
	lib := filepath.Join(dir, "libryui-"+goarch+".so")
	env := []string{"GOOS=android", "GOARCH=" + goarch, "CGO_ENABLED=1", "CC=" + filepath.Join(tc.ndkBin, clang),
		"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
	if err := tc.run(app.dir, env, "go", "build", "-buildmode=c-shared", "-trimpath", "-ldflags=-buildid=", "-o", lib, playerPkg); err != nil {
		return err
	}
	pkg := androidPackage(playerID)
	// Pushed unless the device has it (asked: an app reinstalled, or an
	// emulator restored, has another or none).
	if sum := fileHash(lib); tc.deviceHash(serial, pkg, "files/ryui/libryui.so") != sum {
		tmp := "/data/local/tmp/ryui-player.so"
		if err := tc.run(dir, nil, tc.adb, "-s", serial, "push", lib, tmp); err != nil {
			return err
		}
		tc.run(dir, nil, tc.adb, "-s", serial, "shell", "am", "force-stop", pkg)
		if err := tc.run(dir, nil, tc.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
			"'mkdir -p files/ryui && rm -f files/ryui/libryui.so && cp "+tmp+" files/ryui/libryui.so && chmod 444 files/ryui/libryui.so'"); err != nil {
			return err
		}
	}
	// Tell the player where to connect.
	if err := tc.run(dir, nil, tc.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
		"'mkdir -p files/ryui && echo "+remote+" > files/ryui/remote'"); err != nil {
		return err
	}
	if !emulator {
		// On a device, localhost:7461 (and 7462, assets) map to this machine.
		for _, m := range [][2]int{{devicePort, port}, {devicePort + 1, port + 1}} {
			if err := tc.run(dir, nil, tc.adb, "-s", serial, "reverse", fmt.Sprintf("tcp:%d", m[0]), fmt.Sprintf("tcp:%d", m[1])); err != nil {
				return err
			}
		}
	}
	tc.run(dir, nil, tc.adb, "-s", serial, "shell", "am", "force-stop", pkg)
	if err := proc.restart(); err != nil {
		return err
	}
	if err := tc.run(dir, nil, tc.adb, "-s", serial, "shell", "am", "start", "-W", "-n", pkg+"/dev.ryui.RyUIActivity"); err != nil {
		proc.stop()
		return err
	}
	fmt.Printf("ryui: running in the player in %.1fs; changes reload without reinstalling\n", time.Since(start).Seconds())
	return watchHost(proc, stop, func() { exec.Command(tc.adb, "-s", serial, "shell", "am", "force-stop", pkg).Run() })
}

// playerConfig is the player's: every permission an app might need.
func playerConfig() *appConfig {
	// Shares of anything: the app under it may take files.
	// Its sheet is the app's, or opens the app (ui.ShareSheetRoot(nil)).
	c := &appConfig{ID: playerID, ShareTarget: shareTarget{on: true, Types: []string{"text", "*/*"}, Max: 20}, shareSheet: true}
	c.permissionsFor(nil)
	return c
}

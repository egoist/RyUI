package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The player: `asuka dev` runs the app's Go code on this machine and its
// views in a player app on the simulator, emulator or device, over a
// socket (internal/remote). The player is the platform host with a small
// Go library in place of an app; it's built and installed once, shared
// by every app, and reconnects when the app's code restarts. A change
// rebuilds and restarts only the app's process here: no install.

const (
	playerID   = "dev.asuka.player"
	playerName = "asuka"
	playerPkg  = "github.com/egoist/asuka/mobile/player"
	// devicePort is where the player connects (see mobile/player): the
	// simulator shares this machine's loopback; adb reverse maps it.
	devicePort = 7461
)

// usePlayer reports whether to run app with the player: not when its
// native packages (it, or its dependencies) have native code for platform
// (ios, android), which only an app built with it can show.
func usePlayer(app *goApp, o *devOptions, platform string) bool {
	if o.noHot || o.once {
		return false
	}
	natives, err := nativeFor(app, platform, "arm64")
	if err != nil {
		fmt.Fprintln(os.Stderr, "asuka:", err)
		return false // the build says so again
	}
	if len(natives) > 0 {
		fmt.Printf("asuka: %s has native code for %s (%s): building it into the app (no player)\n", app.name, platform, natives[0].path)
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
	// bin is the built executable (default: app-host in the work
	// directory).
	bin string
	run *hostRun // nil when none runs
	v   bool
	// flags is the build's -ldflags (default: asuka.json's).
	flags string
	// goos and goarch are the build's GOOS and GOARCH (default: this
	// machine's).
	goos, goarch string
	// wrap, when set, is a command that runs the build where it runs
	// (Linux: ASUKA_LINUX_RUN, a VM with this computer's paths): the
	// process is wrap, given env, the directory and the executable.
	wrap []string
	// afterBuild, when set, runs after each build (macOS: signs the app).
	afterBuild func() error
	// own: the process answers for itself, not for asuka (macOS: its
	// privacy prompts are its own, as when it's opened from the Finder).
	own bool
	// remote: the process serves the app to a player (app/remote, linked
	// in only here, so the app's own builds carry no network code).
	remote bool
	// listeners are the ones the runs take (inherit), kept across them.
	listeners []inherited
	// ctl is the runs' control connections' listener.
	ctl *control
	// places reads the source for kept state's names.
	places *devPlaces
}

// devOverlay writes the -overlay of a development build: a file in the
// app's main package that gives the run the names of its source's places
// (devPlaces, signal.KeepPlaces), and with remote links app/remote in.
func devOverlay(app *goApp, work string, remote bool, places *devPlaces) (string, error) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	if remote {
		b.WriteString("import _ \"github.com/egoist/asuka/app/remote\"\n")
	}
	if places != nil {
		table, err := places.table()
		if err != nil {
			// Kept state is known by the runtime's names instead.
			fmt.Fprintln(os.Stderr, "asuka: reading the source for kept state's names:", err)
		}
		if table != "" {
			fmt.Fprintf(&b, "import zzAsukaSignal \"github.com/egoist/asuka/signal\"\n\nfunc init() { zzAsukaSignal.KeepPlaces(%s) }\n", strconv.Quote(table))
		}
	}
	stub := filepath.Join(work, "zz_asuka_dev.go")
	if err := os.WriteFile(stub, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(app.dir, "zz_asuka_dev.go"): stub}})
	ovPath := filepath.Join(work, "dev-overlay.json")
	return ovPath, os.WriteFile(ovPath, overlay, 0o644)
}

func (p *hostProcess) build() error {
	if p.bin == "" {
		p.bin = filepath.Join(p.work, "app-host")
		if runtime.GOOS == "windows" {
			p.bin += ".exe" // CreateProcess runs only a name with its extension
		}
	}
	flags := p.flags
	if flags == "" {
		flags = p.app.cfg.ldflags(false)
	}
	if p.places == nil {
		p.places = newDevPlaces(p.app, p.goos, p.goarch)
	}
	ov, err := devOverlay(p.app, p.work, p.remote, p.places)
	if err != nil {
		return err
	}
	args := []string{"build", flags, "-overlay", ov, "-o", p.bin}
	c := exec.Command("go", append(args, ".")...)
	c.Dir = p.app.dir
	c.Env = goEnv(p.app.dir, "CGO_ENABLED=0")
	if p.goos != "" {
		c.Env = append(c.Env, "GOOS="+p.goos)
	}
	if p.goarch != "" {
		c.Env = append(c.Env, "GOARCH="+p.goarch)
	}
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed:\n%s", out)
	}
	if p.afterBuild != nil {
		return p.afterBuild()
	}
	return nil
}

// restart runs the new build in place of the running one. Under the
// control of asuka dev (devstate.Wait), the new process starts while the
// old one still runs, and waits; once it waits (it has started: on macOS,
// the system has checked the program it hasn't seen run), the old one
// stops, saving its state, and the new one goes on and takes it back. A
// build that quits before it waits leaves the old one running. Run
// elsewhere (ASUKA_LINUX_RUN), the old one stops first.
func (p *hostProcess) restart() error {
	if len(p.wrap) > 0 {
		p.stop()
		return p.start(nil)
	}
	if p.ctl == nil {
		ctl, err := newControl()
		if err != nil {
			return err
		}
		p.ctl = ctl
	}
	old := p.run
	began := time.Now()
	if err := p.start([]string{"ASUKA_CONTROL=" + p.ctl.ln.Addr().String()}); err != nil {
		return err
	}
	conn, exited := p.ctl.wait(p.run.proc.Pid, p.run.done, controlWait)
	if exited && old != nil {
		p.run = old
		return errors.New("the new build quit as it started (its output is above); the app keeps running the last one")
	}
	took := time.Since(began)
	old.stop()
	switch {
	case conn != nil:
		p.run.setConn(conn)
		conn.Write([]byte{'g'})
	case !exited:
		// Slower than asuka dev waited: it goes on once it waits.
		p.ctl.later(p.run, p.run.proc.Pid)
	}
	if runtime.GOOS == "darwin" && !p.own && took > slowStart && !slowStartTold {
		slowStartTold = true
		fmt.Printf("asuka: the new build took %.1fs to start: macOS checks a program the first time it runs, unless what runs it is a developer tool (System Settings > Privacy & Security > Developer Tools: add your terminal)\n", took.Seconds())
	}
	return nil
}

const (
	// controlWait is how long a new run may take to start before the old
	// one stops anyway.
	controlWait = 10 * time.Second
	// slowStart is a start slower than a Go program's (a few
	// milliseconds) by the system's check of a new program on macOS.
	slowStart = 150 * time.Millisecond
)

var slowStartTold bool

// start starts the build, with more in its environment.
func (p *hostProcess) start(more []string) error {
	env := append(append(os.Environ(), p.env...), more...)
	r := &hostRun{done: make(chan struct{})}
	var wait func()
	if p.own {
		proc, err := spawnOwn(p.bin, p.app.dir, env) // assets/ is served from here
		if err != nil {
			return err
		}
		r.proc = proc
		wait = func() { proc.Wait() }
	} else {
		c := exec.Command(p.bin)
		if len(p.wrap) > 0 {
			// Elsewhere: the directory and Asuka's environment go with it
			// (ASUKA_SCRIPT... of this one's, then the run's).
			args := append(p.wrap[1:len(p.wrap):len(p.wrap)], "env", "-C", p.app.dir)
			for _, kv := range os.Environ() {
				if strings.HasPrefix(kv, "ASUKA_") && !strings.HasPrefix(kv, "ASUKA_LINUX_RUN=") {
					args = append(args, kv)
				}
			}
			args = append(append(args, p.env...), p.bin)
			c = exec.Command(p.wrap[0], args...)
		}
		c.Dir = p.app.dir // assets/ is served from here
		// The listeners asuka dev keeps, from descriptor 3 on.
		for i, l := range p.listeners {
			c.ExtraFiles = append(c.ExtraFiles, l.file)
			env = append(env, fmt.Sprintf("%s=%d", l.env, 3+i))
		}
		c.Env = env
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if slices.Contains(p.env, "ASUKA_STOP=stdin") {
			in, err := c.StdinPipe()
			if err != nil {
				return err
			}
			r.stdin = in
		}
		if err := c.Start(); err != nil {
			return err
		}
		r.proc = c.Process
		wait = func() { c.Wait() }
	}
	go func() {
		wait()
		close(r.done)
	}()
	p.run = r
	return nil
}

// stop stops the running build: it saves its state and quits.
func (p *hostProcess) stop() {
	p.run.stop()
	p.run = nil
}

// done is closed when the running build exits (nil when none runs).
func (p *hostProcess) done() <-chan struct{} {
	if p.run == nil {
		return nil
	}
	return p.run.done
}

// inherit listens on addr for the app's runs, which take the listener
// on a descriptor that the environment variable env names
// (internal/remote): it stays across reloads.
func (p *hostProcess) inherit(env, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	f, err := ln.(*net.TCPListener).File()
	ln.Close() // f is a copy
	if err != nil {
		return err
	}
	p.listeners = append(p.listeners, inherited{env: env, file: f})
	return nil
}

// inheritRemote keeps the player's port, and the assets' after it, for
// the runs (internal/remote's listen).
func (p *hostProcess) inheritRemote(port int) error {
	if err := p.inherit("ASUKA_REMOTE_FD", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		return err
	}
	return p.inherit("ASUKA_ASSETS_FD", fmt.Sprintf("127.0.0.1:%d", port+1))
}

type inherited struct {
	env  string
	file *os.File
}

// hostRun is one run of the app's build.
type hostRun struct {
	proc *os.Process
	done chan struct{}
	// stdin, when the process is told to stop by closing it (Windows,
	// which can't send it SIGINT).
	stdin io.WriteCloser
	// ctl is its control connection (devstate.Wait), once it waits: closed
	// to stop it.
	mu  sync.Mutex
	ctl net.Conn
}

func (r *hostRun) setConn(c net.Conn) {
	r.mu.Lock()
	r.ctl = c
	r.mu.Unlock()
}

func (r *hostRun) conn() net.Conn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctl
}

func (r *hostRun) stop() {
	if r == nil {
		return
	}
	ctl := r.conn()
	switch {
	case ctl != nil:
		ctl.Close() // it saves its state and quits
	case r.stdin != nil:
		r.stdin.Close() // likewise
	default:
		r.proc.Signal(syscall.SIGINT)
	}
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		r.proc.Kill()
		<-r.done
	}
	if c := r.conn(); c != nil && ctl == nil {
		c.Close() // it connected meanwhile
	}
}

// control is asuka dev's end of its runs' control connections: each run
// connects, sends its process ID and waits for a byte (devstate.Wait).
type control struct {
	ln    net.Listener
	mu    sync.Mutex
	slots map[int]chan net.Conn // by process ID
}

func newControl() (*control, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := &control{ln: ln, slots: map[int]chan net.Conn{}}
	go c.accept()
	return c, nil
}

func (c *control) slot(pid int) chan net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.slots[pid]
	if ch == nil {
		ch = make(chan net.Conn, 1)
		c.slots[pid] = ch
	}
	return ch
}

func (c *control) drop(pid int) {
	c.mu.Lock()
	delete(c.slots, pid)
	c.mu.Unlock()
}

func (c *control) accept() {
	for {
		conn, err := c.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			line, err := bufio.NewReader(conn).ReadString('\n')
			conn.SetReadDeadline(time.Time{})
			pid, perr := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || perr != nil {
				conn.Close()
				return
			}
			select {
			case c.slot(pid) <- conn:
			default:
				conn.Close() // one per run
			}
		}()
	}
}

// wait waits for the run of process pid to connect, for it to exit
// (done), or for limit.
func (c *control) wait(pid int, done <-chan struct{}, limit time.Duration) (conn net.Conn, exited bool) {
	slot := c.slot(pid)
	t := time.NewTimer(limit)
	defer t.Stop()
	select {
	case conn = <-slot:
		c.drop(pid)
		return conn, false
	case <-done:
		c.drop(pid)
		return nil, true
	case <-t.C:
		return nil, false
	}
}

// later lets the run of process pid go on once it connects (it started
// slower than asuka dev waited), unless it exits first.
func (c *control) later(r *hostRun, pid int) {
	slot := c.slot(pid)
	go func() {
		defer c.drop(pid)
		select {
		case conn := <-slot:
			r.setConn(conn)
			conn.Write([]byte{'g'})
		case <-r.done:
		}
	}()
}

// watchHost rebuilds and restarts the app's process on each change: to
// its module, or to more (its plugins' native code, wherever they are).
// When it's done it stops the player too, so that it doesn't join the
// next session, which may be another app's.
func watchHost(p *hostProcess, stop <-chan os.Signal, stopPlayer func(), more ...string) error {
	fmt.Println("asuka: watching for changes (Ctrl-C to stop)")
	w := newWatcher(append([]string{p.app.moduleDir, p.app.dir}, more...)...)
	defer stopPlayer()
	defer p.stop()
	for {
		select {
		case <-stop:
			return nil
		case <-w.wake:
			if !w.changed() {
				continue
			}
			start := time.Now()
			if err := p.build(); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
				continue
			}
			if err := p.restart(); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
				continue
			}
			fmt.Printf("asuka: reloaded in %.1fs\n", time.Since(start).Seconds())
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
	proc := &hostProcess{app: app, work: work, remote: true, env: []string{
		fmt.Sprintf("ASUKA_REMOTE=127.0.0.1:%d", port),
		"ASUKA_STATE=" + filepath.Join(work, "state.json"),
		"ASUKA_DATA=" + filepath.Join(work, "data"), // the app's own: store.Dir, sqlite files
		fmt.Sprintf("ASUKA_ASSETS_URL=http://127.0.0.1:%d/", port+1),
		fmt.Sprintf("ASUKA_ASSETS_ADDR=127.0.0.1:%d", port+1),
	}}
	if err := proc.inheritRemote(port); err != nil {
		return err
	}
	if err := proc.build(); err != nil {
		return err
	}
	// The player: the host, and the player library instead of an app.
	dir := playerDir(work, "ios")
	// The player's build is shared by every app: one at a time, until
	// it's installed.
	unlock, err := lockShared(dir)
	if err != nil {
		return err
	}
	release := sync.OnceFunc(unlock)
	defer release()
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
		player := &goApp{dir: dir, name: playerName, bundleID: playerID, cfg: playerConfig(), pkg: playerPkg, pkgIn: app.dir}
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
	release()
	launch := exec.Command("xcrun", "simctl", "launch", "--terminate-running-process", sim.UDID, playerID)
	launch.Env = append(os.Environ(), fmt.Sprintf("SIMCTL_CHILD_ASUKA_REMOTE=127.0.0.1:%d", port))
	if out, err := launch.CombinedOutput(); err != nil {
		proc.stop()
		return fmt.Errorf("simctl launch: %v: %s", err, out)
	}
	fmt.Printf("asuka: running in the player in %.1fs; changes reload without reinstalling\n", time.Since(start).Seconds())
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
	proc := &hostProcess{app: app, work: work, remote: true, env: []string{
		fmt.Sprintf("ASUKA_REMOTE=127.0.0.1:%d", port),
		"ASUKA_STATE=" + filepath.Join(work, "state.json"),
		"ASUKA_DATA=" + filepath.Join(work, "data"), // the app's own: store.Dir, sqlite files
		"ASUKA_ASSETS_URL=" + assets,
		fmt.Sprintf("ASUKA_ASSETS_ADDR=127.0.0.1:%d", port+1),
	}}
	if err := proc.inheritRemote(port); err != nil {
		return err
	}
	if err := proc.build(); err != nil {
		return err
	}
	dir := playerDir(work, "android")
	// The player's build is shared by every app: one at a time, until
	// it's on the device.
	unlock, err := lockShared(dir)
	if err != nil {
		return err
	}
	release := sync.OnceFunc(unlock)
	defer release()
	player := &goApp{dir: dir, name: playerName, bundleID: playerID, cfg: playerConfig(), pkg: playerPkg, pkgIn: app.dir}
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
	lib := filepath.Join(dir, "libasuka-"+goarch+".so")
	env := []string{"GOOS=android", "GOARCH=" + goarch, "CGO_ENABLED=1", "CC=" + filepath.Join(tc.ndkBin, clang),
		"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
	if err := tc.run(app.dir, env, "go", "build", "-buildmode=c-shared", "-trimpath", "-ldflags=-buildid=", "-o", lib, playerPkg); err != nil {
		return err
	}
	pkg := androidPackage(playerID)
	// Pushed unless the device has it (asked: an app reinstalled, or an
	// emulator restored, has another or none).
	if sum := fileHash(lib); tc.deviceHash(serial, pkg, "files/asuka/libasuka.so") != sum {
		tmp := "/data/local/tmp/asuka-player.so"
		if err := tc.run(dir, nil, tc.adb, "-s", serial, "push", lib, tmp); err != nil {
			return err
		}
		tc.run(dir, nil, tc.adb, "-s", serial, "shell", "am", "force-stop", pkg)
		if err := tc.run(dir, nil, tc.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
			"'mkdir -p files/asuka && rm -f files/asuka/libasuka.so && cp "+tmp+" files/asuka/libasuka.so && chmod 444 files/asuka/libasuka.so'"); err != nil {
			return err
		}
	}
	release()
	// Tell the player where to connect.
	if err := tc.run(dir, nil, tc.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
		"'mkdir -p files/asuka && echo "+remote+" > files/asuka/remote'"); err != nil {
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
	if err := tc.run(dir, nil, tc.adb, "-s", serial, "shell", "am", "start", "-W", "-n", pkg+"/dev.asuka.AsukaActivity"); err != nil {
		proc.stop()
		return err
	}
	fmt.Printf("asuka: running in the player in %.1fs; changes reload without reinstalling\n", time.Since(start).Seconds())
	return watchHost(proc, stop, func() { exec.Command(tc.adb, "-s", serial, "shell", "am", "force-stop", pkg).Run() })
}

// playerConfig is the player's: every permission an app might need.
func playerConfig() *appConfig {
	// Shares of anything: the app under it may take files.
	// Its sheet is the app's, or opens the app (ui.ShareSheetRoot(nil)).
	c := &appConfig{ID: playerID, ShareTarget: shareTarget{on: true, Types: []string{"text", "*/*"}, Max: 20}, shareSheet: true}
	c.permissionsFor(nil)
	c.addFeatureExtras(allFeatures())
	return c
}

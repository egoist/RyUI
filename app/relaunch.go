package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/asuka/internal/boot"
)

// Relaunch quits the app as Quit does (OnQuit's handlers run first, and
// may cancel it, which cancels the relaunch too) and starts it again once
// this process has exited, with the same arguments: after an update
// (asuka/update), the new version; after a setting that only takes effect
// at launch, the same one afresh. Safe from any goroutine.
//
// The new process starts only once this one is gone, so an app that runs
// once (Windows, Linux) doesn't hand its launch to the instance that's
// quitting. macOS opens the app's bundle again through Launch Services,
// as the Dock does. A phone's app and a page in a browser don't quit
// themselves: there, Relaunch does nothing.
func Relaunch() {
	if boot.Quit == nil {
		return
	}
	relaunching.Store(true)
	boot.Quit()
}

var relaunching atomic.Bool

// startExe and startDir are the executable the app started from and its
// working directory: on Linux, os.Executable reports the old file once an
// update replaced it.
var (
	startExe, _ = os.Executable()
	startDir, _ = os.Getwd()
)

// relaunchWait, in the environment of a relaunched process, is the pid
// of the process it waits for (relaunch_*.go's waitExit).
const relaunchWait = "ASUKA_RELAUNCH_WAIT"

func init() {
	if v := os.Getenv(relaunchWait); v != "" {
		os.Unsetenv(relaunchWait)
		if pid, err := strconv.Atoi(v); err == nil {
			waitExit(pid, 30*time.Second)
		}
	}
}

// relaunchAfterQuit runs as a quit goes on (quitRequested): a relaunch
// asked for starts the process that waits for this one to exit, then
// starts the app.
func relaunchAfterQuit(canceled, ending bool) {
	if !relaunching.Swap(false) || canceled || ending {
		return
	}
	args := os.Args[1:]
	if boot.StagedRelaunch != nil && boot.StagedRelaunch(args) {
		return // asuka/update's helper puts the new version in place, then opens it
	}
	if err := startRelaunch(args); err != nil {
		os.Stderr.WriteString("asuka: relaunching: " + err.Error() + "\n")
	}
}

// startRelaunch starts what starts the app again once this process has
// exited: on macOS, for an app in a bundle, a shell that waits, then opens
// the bundle through Launch Services (a child of this process would stay
// in its coalition, which goes with it); elsewhere the executable itself,
// told to wait (relaunchWait).
func startRelaunch(args []string) error {
	pid := strconv.Itoa(os.Getpid())
	if runtime.GOOS == "darwin" {
		if bundle := bundleOf(startExe); bundle != "" {
			argv := append([]string{"/usr/bin/open", bundle}, args...)
			if len(args) > 0 {
				argv = append([]string{"/usr/bin/open", bundle, "--args"}, args...)
			}
			script := `pid=$1; shift; while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done; exec "$@"`
			cmd := exec.Command("/bin/sh", append([]string{"-c", script, "asuka-relaunch", pid}, argv...)...)
			detach(cmd)
			if err := cmd.Start(); err != nil {
				return err
			}
			return cmd.Process.Release()
		}
	}
	cmd := exec.Command(startExe, args...)
	cmd.Dir = startDir
	cmd.Env = append(os.Environ(), relaunchWait+"="+pid)
	if runtime.GOOS != "windows" {
		// GUI executables on Windows have no standard streams to pass on.
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// bundleOf returns the .app bundle exe runs in, or "".
func bundleOf(exe string) string {
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	b := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if strings.HasSuffix(b, ".app") && filepath.Base(filepath.Dir(exe)) == "MacOS" {
		return b
	}
	return ""
}

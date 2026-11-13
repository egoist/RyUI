//go:build !windows

package update

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Staged is a new version of a Mac app's bundle, waiting for the app to
// quit to take the place of the old one, whose helper process puts it in
// place. Replacing the bundle while the app runs would move the running
// app's bundle away: the Dock's tile would follow it, and the new
// version, opened where the app was, would get a second tile.
type Staged struct {
	work, bundle, target string
	helper               *exec.Cmd
}

// StageBundle leaves bundle, the new version made in work (a directory
// beside target, on its volume: MakeWork), to replace the bundle target
// once the process pid has exited, however it exits. The helper is a
// shell of its own session, which neither the app's quitting nor the
// terminal's signals end; it removes work when it's done.
func StageBundle(work, bundle, target string, pid int) (*Staged, error) {
	if fi, err := os.Stat(filepath.Join(bundle, "Contents")); err != nil || !fi.IsDir() {
		return nil, errors.New("the update holds no app bundle")
	}
	s := &Staged{work: work, bundle: bundle, target: target}
	s.helper = exec.Command("/bin/sh", "-c", installAfterExit, "asuka-update", strconv.Itoa(pid), target, bundle, work)
	s.helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := s.helper.Start(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(work, stagedFile), []byte(strconv.Itoa(s.helper.Process.Pid)), 0o644); err != nil {
		s.Cancel()
		return nil, err
	}
	// The helper is waited for, so that it's never a zombie of the app's;
	// it outlives the app otherwise.
	go s.helper.Wait()
	return s, nil
}

// installAfterExit is the helper's script: with the pid of the app, the
// target, the new bundle and the work directory, it waits for the app to
// exit, swaps the bundles (putting the old one back if the new one can't
// be moved in), tells Launch Services of the new one, and runs the
// command that work/relaunch sets as its arguments, if Relaunch wrote
// one: the arguments are known only as the app quits. Each step is a
// rename on one volume.
const installAfterExit = `pid=$1 target=$2 new=$3 work=$4
trap '' HUP INT TERM
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done
if [ -d "$new" ] && mv "$target" "$work/old"; then
	if mv "$new" "$target"; then
		touch "$target"
		lsregister=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
		[ -x "$lsregister" ] && "$lsregister" -f "$target" >/dev/null 2>&1
	else
		mv "$work/old" "$target"
	fi
fi
relaunch=
if [ -e "$work/relaunch" ]; then relaunch=1; . "$work/relaunch"; fi
rm -rf "$work"
if [ -n "$relaunch" ]; then exec "$@"; fi
`

// Relaunch makes the helper run the command argv once it has put the
// update in place: /usr/bin/open on the bundle, through Launch Services as
// the Dock opens apps (not as a child of this process: its coalition goes
// with it). Without -n: the app has exited, and the Dock gives the new
// version the app's tile.
func (s *Staged) Relaunch(argv []string) error {
	return os.WriteFile(filepath.Join(s.work, "relaunch"), []byte(SetArgs(argv)), 0o644)
}

// Target is the bundle the update replaces.
func (s *Staged) Target() string { return s.target }

// Cancel stops the helper and removes the staged update: one staged
// after it takes its place.
func (s *Staged) Cancel() {
	if s.helper != nil && s.helper.Process != nil {
		_ = s.helper.Process.Kill()
	}
	_ = os.RemoveAll(s.work)
}

// OpenArgs returns the command that opens the app bundle with the
// arguments args, through Launch Services.
func OpenArgs(bundle string, args []string) []string {
	argv := []string{"/usr/bin/open", bundle}
	if len(args) > 0 {
		argv = append(append(argv, "--args"), args...)
	}
	return argv
}

// SetArgs returns the command of /bin/sh that makes args its positional
// parameters.
func SetArgs(args []string) string {
	var b strings.Builder
	b.WriteString("set --")
	for _, a := range args {
		b.WriteString(" '")
		b.WriteString(strings.ReplaceAll(a, "'", `'\''`))
		b.WriteString("'")
	}
	b.WriteString("\n")
	return b.String()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

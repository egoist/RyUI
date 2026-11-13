//go:build (darwin && !ios) || windows || (linux && !android)

package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/internal/boot"
	iupdate "github.com/egoist/asuka/internal/update"
	"github.com/egoist/asuka/ui"
)

// startExe is the executable the app started from: on Linux,
// os.Executable reports the old file once an update replaced it.
var startExe, _ = os.Executable()

// locate finds how the app is installed (tests replace it).
var locate = func() (*iupdate.Install, error) {
	return iupdate.Locate(runtime.GOOS, startExe, os.Getenv)
}

var located = sync.OnceValues(func() (*iupdate.Install, error) {
	in, err := locate()
	if err == nil {
		err = in.Writable()
	}
	return in, err
})

func defaultSource() Source { return feedSource(buildFeed) }

func init() {
	if buildFeed == "" {
		return
	}
	boot.StagedRelaunch = stagedRelaunch // none staged: false
	// What an update before couldn't remove while its app ran.
	go func() {
		if in, err := locate(); err == nil {
			in.CleanLeftovers()
		}
	}()
}

// Supported reports whether the app can update itself, and why not: nil
// when it can. It can't in a development build or one without asuka.json's
// "updates" (ErrNotConfigured), when the system's packages, Flatpak, Snap
// or an app store installed it (ErrManaged), when it runs translocated
// on the Mac (ErrTranslocated) or where the user can't write
// (ErrNotWritable). Any goroutine.
func Supported() error {
	if buildFeed == "" || buildKey == "" || buildTarget == "" || boot.Version == "" {
		return ErrNotConfigured
	}
	_, err := located()
	return mapErr(err)
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, iupdate.ErrManaged):
		return ErrManaged
	case errors.Is(err, iupdate.ErrTranslocated):
		return ErrTranslocated
	case errors.Is(err, iupdate.ErrNotWritable):
		return ErrNotWritable
	case errors.Is(err, iupdate.ErrNotInstalled):
		return ErrNotInstalled
	}
	return err
}

// Install downloads the release, checks that it's signed with the app's
// key, and installs it; progress, when not nil, is called with the bytes
// downloaded so far, on Install's goroutine. When the release has a delta
// from the running version, only the delta is downloaded and the new
// version made from the installed one; when that fails (the app was
// changed since it was installed), the whole archive is downloaded, and
// the progress starts over.
//
// The running app goes on as it is: the new version runs after Relaunch,
// or at the next launch. On the Mac the new bundle is checked (its code
// signature, of the running app's signer) and waits for the app to quit
// to take the old one's place; on Windows and Linux the app's files are
// replaced now. It blocks: not on the main thread.
func (r *Release) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	in, err := located()
	if err != nil {
		return mapErr(err)
	}
	get := r.source.Open
	if err := install(ctx, in, get, r.manifest, r.key, r.current, progress); err != nil {
		return fmt.Errorf("update: installing %s: %w", r.Version, err)
	}
	if fns := installedHandlers(); len(fns) > 0 {
		ui.Post(func() {
			for _, f := range fns {
				(*f)(r)
			}
		})
	}
	return nil
}

// install installs the version of m in place of the app in, of version
// current: from its delta from current when there's one, else (or when
// the delta fails) from its archive, downloaded with get.
func install(ctx context.Context, in *iupdate.Install, get iupdate.Getter, m iupdate.Manifest, key, current string, progress func(downloaded, total int64)) error {
	pub, err := iupdate.ParsePublicKey(key)
	if err != nil {
		return err
	}
	work, err := in.MakeWork()
	if err != nil {
		return err
	}
	staged := false
	defer func() {
		if !staged {
			os.RemoveAll(work)
		}
	}()
	unpacked := filepath.Join(work, "app")

	tree := ""
	if d := m.Delta(current); d != nil {
		tree, err = applyDelta(ctx, in, get, *d, pub, m.Version, work, unpacked, progress)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			log.Printf("asuka: update: downloading the whole update, as its delta failed: %v", err)
			if err := os.RemoveAll(unpacked); err != nil {
				return err
			}
			tree = ""
		}
	}
	if tree == "" {
		if m.Size <= 0 || m.Size > iupdate.MaxArchiveSize {
			return fmt.Errorf("invalid size %d", m.Size)
		}
		archive := filepath.Join(work, "update.tar.gz")
		if err := download(ctx, get, m.URL, m.Size, m.Signature, pub, archive, progress); err != nil {
			return err
		}
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		err = iupdate.ExtractArchive(f, unpacked)
		f.Close()
		os.Remove(archive)
		if err != nil {
			return err
		}
		if tree, err = in.Tree(unpacked); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if in.Kind == iupdate.Bundle {
		if err := iupdate.CheckBundle(tree, in.Root); err != nil {
			return err
		}
		s, err := iupdate.StageBundle(work, tree, in.Root, os.Getpid())
		if err != nil {
			return err
		}
		staged = true
		stage(s)
		return nil
	}
	// The executable the app runs from, which Relaunch starts, must be the
	// new version's.
	if in.ExeIn(tree) == "" {
		return fmt.Errorf("the update has no %s", filepath.Base(in.Exe))
	}
	if err := in.Swap(tree); err != nil {
		return err
	}
	if in.Kind == iupdate.Prefix {
		refreshDesktop(in.Root)
	}
	return nil
}

// applyDelta downloads the delta d into work and makes the new version of
// the app in from it, in dir, and returns the new app's tree there.
func applyDelta(ctx context.Context, in *iupdate.Install, get iupdate.Getter, d iupdate.Delta, pub ed25519.PublicKey, version, work, dir string, progress func(downloaded, total int64)) (string, error) {
	if d.Size <= 0 || d.Size > iupdate.MaxArchiveSize {
		return "", fmt.Errorf("invalid size %d", d.Size)
	}
	path := filepath.Join(work, "update.delta")
	if err := download(ctx, get, d.URL, d.Size, d.Signature, pub, path, progress); err != nil {
		return "", err
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	tree := dir
	switch in.Kind {
	case iupdate.Bundle:
		tree = filepath.Join(dir, filepath.Base(in.Root))
	case iupdate.Prefix:
		tree = filepath.Join(dir, filepath.Base(in.Exe))
	}
	if err := os.MkdirAll(filepath.Dir(tree), 0o755); err != nil {
		return "", err
	}
	if err := iupdate.ApplyDelta(f, d.Size, d.From, version, in.Root, tree); err != nil {
		return "", err
	}
	return tree, nil
}

// download fetches url with get into the file path, and checks its size
// and signature.
func download(ctx context.Context, get iupdate.Getter, url string, size int64, signature string, pub ed25519.PublicKey, path string, progress func(downloaded, total int64)) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), &progressReader{r: io.LimitReader(body, size+1), total: size, fn: progress})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	if n != size {
		return fmt.Errorf("downloaded %d bytes, want %d", n, size)
	}
	return iupdate.Verify(pub, sum.Sum(nil), signature)
}

// progressReader reports how much of a download was read, ten times a
// second at most, and at its end.
type progressReader struct {
	r     io.Reader
	n     int64
	total int64
	fn    func(downloaded, total int64)
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.fn != nil && (err != nil || p.n == p.total || time.Since(p.last) > 100*time.Millisecond) {
		p.last = time.Now()
		p.fn(p.n, p.total)
	}
	return n, err
}

// pending is the Mac app's update waiting for the app to quit.
var pending struct {
	sync.Mutex
	s *iupdate.Staged
}

// stage makes s the update that replaces the app as it quits, in place of
// one staged before, and lets Relaunch open the new version after it.
func stage(s *iupdate.Staged) {
	pending.Lock()
	old := pending.s
	pending.s = s
	pending.Unlock()
	if old != nil {
		old.Cancel()
	}
}

func stagedRelaunch(args []string) bool {
	pending.Lock()
	s := pending.s
	pending.Unlock()
	return s != nil && s.Relaunch(iupdate.OpenArgs(s.Target(), args)) == nil
}

// Relaunch quits the app (app.Quit: OnQuit's handlers may cancel it) and
// starts it again: the new version, once Install has installed one.
// Safe from any goroutine.
func Relaunch() { app.Relaunch() }

// refreshDesktop tells a Linux desktop of the app's new desktop file, MIME
// types and icons, as a package manager would; desktops also notice the
// files change, so a tool that's missing is no matter.
func refreshDesktop(prefix string) {
	share := filepath.Join(prefix, "share")
	for _, c := range [][]string{
		{"update-desktop-database", filepath.Join(share, "applications")},
		{"update-mime-database", filepath.Join(share, "mime")},
		{"gtk4-update-icon-cache", "-q", "-t", "-f", filepath.Join(share, "icons", "hicolor")},
	} {
		if _, err := os.Stat(c[len(c)-1]); err != nil {
			continue
		}
		if p, err := exec.LookPath(c[0]); err == nil {
			_ = exec.Command(p, c[1:]...).Run()
		}
	}
}

//go:build (darwin && !ios) || windows || (linux && !android)

package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/asuka/internal/boot"
	iupdate "github.com/egoist/asuka/internal/update"
)

// feedServer serves files (name → content) as a feed would, counting the
// requests of each.
type feedServer struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	gets  map[string]int
}

func newFeedServer(t *testing.T) *feedServer {
	s := &feedServer{files: map[string][]byte{}, gets: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, "/")
		s.gets[name]++
		b, ok := s.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *feedServer) put(name string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = b
}

func (s *feedServer) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets[name]
}

// tree writes files (path → content) into a new directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// files returns what dir holds, its FileLists and the updater's work
// directories left out.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), iupdate.WorkPrefix) {
			return fs.SkipDir
		}
		if d.IsDir() || d.Name() == iupdate.FileList {
			return nil
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out
}

// signed returns b's size and signature with key.
func signed(key ed25519.PrivateKey, b []byte) (int64, string) {
	sum := sha256.Sum256(b)
	return int64(len(b)), iupdate.Sign(key, sum[:])
}

// release publishes version newDir (of the layout of an installed app at
// root) on s: its archive, its delta from 1.0.0's oldDir, and the
// manifest.
type fixture struct {
	t        *testing.T
	s        *feedServer
	key      ed25519.PrivateKey
	manifest iupdate.Manifest
}

func newFixture(t *testing.T) *fixture {
	pub, priv, _ := ed25519.GenerateKey(nil)
	f := &fixture{t: t, s: newFeedServer(t), key: priv}
	saved := []string{buildFeed, buildKey, buildTarget, boot.Version}
	savedLocated := located
	t.Cleanup(func() {
		buildFeed, buildKey, buildTarget, boot.Version = saved[0], saved[1], saved[2], saved[3]
		located = savedLocated
		SetChannel("")
		SetSource(nil)
	})
	buildFeed = f.s.URL + "/update-test.json"
	buildKey = base64.StdEncoding.EncodeToString(pub)
	buildTarget = "test"
	boot.Version = "1.0.0"
	return f
}

// publish makes version's archive of the app in newDir (entries: its
// top-level entries, or nil for all), and its delta from oldDir (1.0.0),
// and serves them with the manifest.
func (f *fixture) publish(version, oldDir, newDir string, archiveRoot string) {
	t := f.t
	var archive bytes.Buffer
	if archiveRoot == "" {
		archiveRoot = newDir
	}
	entries, _ := os.ReadDir(archiveRoot)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if err := iupdate.WriteArchive(&archive, archiveRoot, names); err != nil {
		t.Fatal(err)
	}
	size, sig := signed(f.key, archive.Bytes())
	f.s.put("app.tar.gz", archive.Bytes())
	f.manifest = iupdate.Manifest{Version: version, Notes: "Notes of " + version, Date: "2026-11-13T09:00:00Z",
		URL: f.s.URL + "/app.tar.gz", Size: size, Signature: sig}
	if oldDir != "" {
		var delta bytes.Buffer
		if err := iupdate.WriteDelta(&delta, "1.0.0", version, oldDir, newDir, nil); err != nil {
			t.Fatal(err)
		}
		dsize, dsig := signed(f.key, delta.Bytes())
		f.s.put("app.delta", delta.Bytes())
		f.manifest.Deltas = []iupdate.Delta{{From: "1.0.0", URL: f.s.URL + "/app.delta", Size: dsize, Signature: dsig}}
	}
	f.serveManifest()
}

func (f *fixture) serveManifest() {
	b, _ := json.Marshal(f.manifest)
	f.s.put("update-test.json", b)
}

// installAt makes the updater install into the app laid out for goos,
// running from exe.
func installAt(t *testing.T, goos, exe string) {
	in, err := iupdate.Locate(goos, exe, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	located = func() (*iupdate.Install, error) { return in, nil }
}

var (
	installedApp = map[string]string{"App.exe": "v1 code", "assets/a.png": "a", "assets/old.png": "old"}
	newApp       = map[string]string{"App.exe": "v2 code", "assets/a.png": "a", "assets/b.png": "b"}
)

// The Windows layout, from the delta: only the delta is downloaded, and the
// folder is the new version's.
func TestInstallDelta(t *testing.T) {
	f := newFixture(t)
	root := tree(t, installedApp)
	iupdate.WriteFileList(root, []string{"App.exe", "assets"})
	installAt(t, "windows", filepath.Join(root, "App.exe"))
	oldDir, newDir := tree(t, installedApp), tree(t, newApp)
	f.publish("1.1.0", oldDir, newDir, "")

	r, err := Check(context.Background())
	if err != nil || r == nil {
		t.Fatalf("Check = %v, %v", r, err)
	}
	if r.Version != "1.1.0" || r.Notes != "Notes of 1.1.0" || r.Date.IsZero() || r.Size != f.manifest.Deltas[0].Size {
		t.Errorf("the release: %+v", r)
	}
	var last [2]int64
	if err := r.Install(context.Background(), func(d, total int64) { last = [2]int64{d, total} }); err != nil {
		t.Fatal(err)
	}
	if got := files(t, root); !maps.Equal(got, newApp) {
		t.Errorf("the folder holds %v, want %v", got, newApp)
	}
	if last[0] != last[1] || last[1] != f.manifest.Deltas[0].Size {
		t.Errorf("the progress ended at %v", last)
	}
	if f.s.count("app.delta") != 1 || f.s.count("app.tar.gz") != 0 {
		t.Errorf("downloaded the delta %d times, the archive %d", f.s.count("app.delta"), f.s.count("app.tar.gz"))
	}
}

// A delta that doesn't fit the installed app (changed since it was
// installed) falls back to the whole archive.
func TestInstallDeltaFallback(t *testing.T) {
	f := newFixture(t)
	changed := maps.Clone(installedApp)
	changed["assets/a.png"] = "a, edited by the user" // the delta copies it: its SHA-256 differs
	root := tree(t, changed)
	installAt(t, "windows", filepath.Join(root, "App.exe"))
	f.publish("1.1.0", tree(t, installedApp), tree(t, newApp), "")
	r, err := Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	want := maps.Clone(newApp)
	// A folder's entries are replaced whole: assets too.
	if got := files(t, root); !maps.Equal(got, want) {
		t.Errorf("the folder holds %v, want %v", got, want)
	}
	if f.s.count("app.delta") != 1 || f.s.count("app.tar.gz") != 1 {
		t.Errorf("downloaded the delta %d times, the archive %d", f.s.count("app.delta"), f.s.count("app.tar.gz"))
	}
}

// An update that isn't signed with the app's key, or that changed since
// it was signed, is never installed.
func TestInstallSignatures(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(f *fixture)
	}{
		{"another key", func(f *fixture) {
			_, other, _ := ed25519.GenerateKey(nil)
			b := f.s.files["app.tar.gz"]
			_, f.manifest.Signature = signed(other, b)
		}},
		{"tampered", func(f *fixture) {
			b := bytes.Clone(f.s.files["app.tar.gz"])
			b[len(b)/2] ^= 0xff
			f.s.put("app.tar.gz", b)
		}},
		{"not signed", func(f *fixture) { f.manifest.Signature = "bm90IGEgc2lnbmF0dXJl" }},
		{"longer", func(f *fixture) { f.s.put("app.tar.gz", append(bytes.Clone(f.s.files["app.tar.gz"]), 0)) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			root := tree(t, installedApp)
			installAt(t, "windows", filepath.Join(root, "App.exe"))
			f.publish("1.1.0", "", tree(t, newApp), "")
			c.change(f)
			f.serveManifest()
			r, err := Check(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Install(context.Background(), nil); err == nil {
				t.Fatal("installed")
			}
			if got := files(t, root); !maps.Equal(got, installedApp) {
				t.Errorf("the folder changed: %v", got)
			}
			if works, _ := filepath.Glob(filepath.Join(root, iupdate.WorkPrefix+"*")); len(works) > 0 {
				t.Errorf("left %v", works)
			}
		})
	}
}

// The Linux layout: the tarball's directory, the app's files in a prefix
// others share.
func TestInstallPrefix(t *testing.T) {
	f := newFixture(t)
	prefix := tree(t, map[string]string{"bin/notes": "v1", "bin/theirs": "x", "share/notes/assets/a": "a1", "share/applications/notes.desktop": "e1"})
	iupdate.WriteFileList(filepath.Join(prefix, "share/notes", iupdate.FileList), []string{"bin/notes", "share/notes/assets/a", "share/applications/notes.desktop"})
	installAt(t, "linux", filepath.Join(prefix, "bin", "notes"))
	// asuka build's tarball: notes/bin/notes...
	build := tree(t, map[string]string{"notes/bin/notes": "v2", "notes/share/notes/assets/a": "a2", "notes/share/applications/notes.desktop": "e2"})
	f.publish("2.0.0", "", filepath.Join(build, "notes"), build)
	r, err := Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"bin/notes": "v2", "bin/theirs": "x", "share/notes/assets/a": "a2", "share/applications/notes.desktop": "e2"}
	if got := files(t, prefix); !maps.Equal(got, want) {
		t.Errorf("the prefix holds %v, want %v", got, want)
	}
}

// A Mac app's new bundle waits for the app to quit (its helper waits for
// this process: the test cancels it).
func TestInstallBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("app bundles")
	}
	f := newFixture(t)
	apps := tree(t, map[string]string{"Notes.app/Contents/MacOS/Notes": "v1", "Notes.app/Contents/Info.plist": "1"})
	installAt(t, "darwin", filepath.Join(apps, "Notes.app/Contents/MacOS/Notes"))
	build := tree(t, map[string]string{"Notes.app/Contents/MacOS/Notes": "v2", "Notes.app/Contents/Info.plist": "2"})
	f.publish("1.0.1", filepath.Join(apps, "Notes.app"), filepath.Join(build, "Notes.app"), build)
	r, err := Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	pending.Lock()
	s := pending.s
	pending.s = nil
	pending.Unlock()
	if s == nil {
		t.Fatal("nothing staged")
	}
	defer s.Cancel()
	if got := files(t, filepath.Join(apps, "Notes.app")); got["Contents/MacOS/Notes"] != "v1" {
		t.Errorf("the bundle was replaced while the app runs: %v", got)
	}
	staged, _ := filepath.Glob(filepath.Join(apps, ".Notes.app.update-*", "app", "Notes.app", "Contents", "MacOS", "Notes"))
	if len(staged) != 1 {
		t.Fatalf("staged %v", staged)
	}
	if b, _ := os.ReadFile(staged[0]); string(b) != "v2" {
		t.Errorf("staged %q", b)
	}
	if f.s.count("app.delta") != 1 || f.s.count("app.tar.gz") != 0 {
		t.Errorf("downloaded the delta %d times, the archive %d", f.s.count("app.delta"), f.s.count("app.tar.gz"))
	}
}

// Check: nothing newer, prereleases on a channel, a custom server's feed,
// a build without updates.
func TestCheck(t *testing.T) {
	f := newFixture(t)
	root := tree(t, installedApp)
	installAt(t, "windows", filepath.Join(root, "App.exe"))
	f.manifest = iupdate.Manifest{Version: "1.0.0", URL: f.s.URL + "/a", Size: 1, Signature: "x"}
	f.serveManifest()
	if r, err := Check(context.Background()); r != nil || err != nil {
		t.Errorf("the same version: %v, %v", r, err)
	}

	// A channel's manifest, newer than the release's.
	beta, _ := json.Marshal(iupdate.Manifest{Version: "1.1.0-beta.2", URL: f.s.URL + "/b", Size: 1, Signature: "x"})
	f.s.put("update-test-beta.json", beta)
	if r, _ := Check(context.Background()); r != nil {
		t.Errorf("releases only: %v", r.Version)
	}
	SetChannel("beta")
	if r, err := Check(context.Background()); err != nil || r == nil || r.Version != "1.1.0-beta.2" {
		t.Errorf("on beta: %v, %v", r, err)
	}
	SetChannel("rc") // no such manifest: the release's
	if r, err := Check(context.Background()); r != nil || err != nil {
		t.Errorf("on rc: %v, %v", r, err)
	}
	SetChannel("")

	// A custom server: its URL with the app's target, version and channel;
	// 204 when there's nothing newer.
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		if r.URL.Query().Get("version") == "1.0.0" {
			w.Write(beta)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	buildFeed = srv.URL + "/check?target={target}&version={version}&channel={channel}"
	SetChannel("beta")
	if r, err := Check(context.Background()); err != nil || r == nil || r.Version != "1.1.0-beta.2" {
		t.Errorf("a custom server: %v, %v", r, err)
	}
	boot.Version = "1.1.0-beta.2"
	if r, err := Check(context.Background()); err != nil || r != nil {
		t.Errorf("a custom server, nothing newer: %v, %v", r, err)
	}
	if len(asked) != 2 || asked[0] != "target=test&version=1.0.0&channel=beta" {
		t.Errorf("asked %q", asked)
	}

	// A Source of the app's own.
	SetSource(testSource{})
	if r, err := Check(context.Background()); err == nil || !strings.Contains(err.Error(), "signed in") {
		t.Errorf("a source: %v, %v", r, err)
	}
	SetSource(nil)

	buildFeed = ""
	if _, err := Check(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("without updates: %v", err)
	}
	if err := Supported(); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Supported without updates: %v", err)
	}
}

type testSource struct{}

func (testSource) Manifest(ctx context.Context, target, channel string) (io.ReadCloser, error) {
	return nil, errors.New("not signed in")
}

func (testSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	return nil, errors.New("not signed in")
}

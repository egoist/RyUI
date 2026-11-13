package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	iupdate "github.com/egoist/asuka/internal/update"
)

func testKeys(t *testing.T) (pub, priv string) {
	p, k, _ := ed25519.GenerateKey(nil)
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(k)
}

func TestUpdatesConfig(t *testing.T) {
	pub, priv := testKeys(t)
	t.Setenv("ASUKA_UPDATE_PUBLIC_KEY", "")
	t.Setenv("ASUKA_UPDATE_URL", "")
	for _, c := range []struct {
		u   updatesConfig
		err string
	}{
		{updatesConfig{GitHub: "me/notes"}, "needs the publicKey"},
		{updatesConfig{PublicKey: "nope", GitHub: "me/notes"}, "publicKey"},
		{updatesConfig{PublicKey: pub}, "either github"},
		{updatesConfig{PublicKey: pub, GitHub: "me/notes", URL: "https://dl.example.com"}, "either github"},
		{updatesConfig{PublicKey: pub, GitHub: "notes"}, "not owner/name"},
		{updatesConfig{PublicKey: pub, URL: "http://dl.example.com"}, "not an https URL"},
		{updatesConfig{PublicKey: pub, URL: "http://127.0.0.1:8080/app"}, ""},
		{updatesConfig{PublicKey: pub, GitHub: "me/notes", Feed: "https://api.example.com/u?t={target}"}, "feed needs url"},
		{updatesConfig{PublicKey: pub, GitHub: "me/notes", S3: &s3Config{Bucket: "b"}}, "s3 needs url"},
		{updatesConfig{PublicKey: pub, URL: "https://dl.example.com", Deltas: new(int)}, ""},
	} {
		cfg := &appConfig{Updates: &c.u}
		_, err := cfg.updates()
		if (err == nil) != (c.err == "") || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("%+v: %v, want %q", c.u, err, c.err)
		}
	}

	// The environment makes a build's updates, or points them elsewhere.
	t.Setenv("ASUKA_UPDATE_PUBLIC_KEY", pub)
	t.Setenv("ASUKA_UPDATE_URL", "http://127.0.0.1:9/feed")
	u, err := (&appConfig{Updates: &updatesConfig{GitHub: "me/notes"}}).updates()
	if err != nil || u.GitHub != "" || u.URL != "http://127.0.0.1:9/feed" || u.PublicKey != pub {
		t.Errorf("with the environment: %+v, %v", u, err)
	}

	// The private key: the environment's text or path, the public key's.
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", priv)
	if k, err := u.signingKey(t.TempDir()); err != nil || k == nil {
		t.Errorf("the key's text: %v", err)
	}
	file := filepath.Join(t.TempDir(), "asuka-update.key")
	os.WriteFile(file, []byte(priv+"\n"), 0o600)
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", file)
	if k, err := u.signingKey(t.TempDir()); err != nil || k == nil {
		t.Errorf("the key's path: %v", err)
	}
	_, other := testKeys(t)
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", other)
	if _, err := u.signingKey(t.TempDir()); err == nil || !strings.Contains(err.Error(), "isn't the private key") {
		t.Errorf("another key: %v", err)
	}
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", "")
	if k, err := u.signingKey(t.TempDir()); k != nil || err != nil {
		t.Errorf("no key: %v, %v", k, err)
	}
}

func TestUpdateFeeds(t *testing.T) {
	for _, c := range []struct {
		u          updatesConfig
		feed, file string
	}{
		{updatesConfig{GitHub: "me/notes", TagPrefix: "v"},
			"https://github.com/me/notes/releases/latest/download/update-linux-amd64.json",
			"https://github.com/me/notes/releases/download/v1.2.0/notes-1.2.0-linux-amd64.tar.gz"},
		{updatesConfig{GitHub: "me/notes", TagPrefix: "desktop-v"},
			"https://github.com/me/notes/releases/download/desktop-v{version}/update-linux-amd64.json",
			"https://github.com/me/notes/releases/download/desktop-v1.2.0/notes-1.2.0-linux-amd64.tar.gz"},
		{updatesConfig{URL: "https://dl.example.com/notes/"},
			"https://dl.example.com/notes/update-linux-amd64.json",
			"https://dl.example.com/notes/notes-1.2.0-linux-amd64.tar.gz"},
		{updatesConfig{URL: "https://dl.example.com/notes", Feed: "https://api.example.com/check?target={target}&version={version}"},
			"https://api.example.com/check?target={target}&version={version}",
			"https://dl.example.com/notes/notes-1.2.0-linux-amd64.tar.gz"},
	} {
		if got := c.u.feed("linux-amd64"); got != c.feed {
			t.Errorf("feed: %s, want %s", got, c.feed)
		}
		if got := c.u.fileURL("1.2.0", "notes-1.2.0-linux-amd64.tar.gz"); got != c.file {
			t.Errorf("file: %s, want %s", got, c.file)
		}
	}
}

func TestUpdateKeygen(t *testing.T) {
	dir := t.TempDir()
	if err := cmdUpdate([]string{"keygen", "--out", dir}); err != nil {
		t.Fatal(err)
	}
	pub, _ := os.ReadFile(filepath.Join(dir, "asuka-update.pub"))
	priv, _ := os.ReadFile(filepath.Join(dir, "asuka-update.key"))
	k, err := iupdate.ParsePrivateKey(string(priv))
	if err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey)) != strings.TrimSpace(string(pub)) {
		t.Error("the keys aren't a pair")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "asuka-update.key")); fi.Mode().Perm() != 0o600 {
			t.Errorf("the secret key's mode is %v", fi.Mode().Perm())
		}
	}
	if err := cmdUpdate([]string{"keygen", "--out", dir}); err == nil {
		t.Error("keygen replaced keys")
	}
	if err := cmdUpdate([]string{"keygen", "--out", dir, "--force"}); err != nil {
		t.Error(err)
	}
}

// writeTree writes files (path → content) into dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out
}

// code is an executable's worth of bytes, of version v: mostly the same
// from one version to the next, as a Go binary's.
func code(v string) string {
	var b strings.Builder
	for i := range 4000 {
		b.WriteString("func f")
		b.WriteString(strings.Repeat("x", i%13))
		b.WriteString("() {}\n")
	}
	return b.String() + "version " + v
}

// Two versions of each layout built and published: the second's manifest
// has a delta from the first, which makes the second from the first as
// installed.
func TestWriteUpdates(t *testing.T) {
	pub, priv := testKeys(t)
	t.Setenv("ASUKA_UPDATE_PUBLIC_KEY", "")
	t.Setenv("ASUKA_UPDATE_URL", "")
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", priv)
	for _, kind := range []iupdate.Kind{iupdate.Folder, iupdate.Prefix, iupdate.Bundle} {
		t.Run(map[iupdate.Kind]string{iupdate.Folder: "windows", iupdate.Prefix: "linux", iupdate.Bundle: "mac"}[kind], func(t *testing.T) {
			var mu sync.Mutex
			published := t.TempDir() // the feed's files
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				http.FileServer(http.Dir(published)).ServeHTTP(w, r)
			}))
			defer srv.Close()
			appDir := t.TempDir()
			writeFiles(t, appDir, map[string]string{"CHANGELOG.md": "# Changes\n\n## 1.0.1\n\n- Faster.\n\n## 1.0.0\n\nFirst.\n"})
			app := &goApp{dir: appDir, name: "notes", bundleID: "com.example.notes", cfg: &appConfig{
				Name: "Notes", Updates: &updatesConfig{PublicKey: pub, URL: srv.URL + "/notes"},
			}}
			target := "test-target"
			build := func(version string) (updateBuild, string) {
				out := t.TempDir()
				app.cfg.Version = version
				b := updateBuild{target: target, kind: kind}
				switch kind {
				case iupdate.Folder:
					b.dir = filepath.Join(out, "Notes")
					b.root = b.dir
					writeFiles(t, b.root, map[string]string{"Notes.exe": code(version), "assets/a.txt": "a"})
					writeFileList(b)
					entries, _ := os.ReadDir(b.root)
					for _, e := range entries {
						b.entries = append(b.entries, e.Name())
					}
				case iupdate.Prefix:
					b.dir = filepath.Join(out, "linux")
					b.root = filepath.Join(b.dir, "notes")
					writeFiles(t, b.root, map[string]string{"bin/notes": code(version), "share/notes/assets/a.txt": "a", "share/applications/com.example.notes.desktop": "[Desktop Entry]"})
					writeFileList(b)
					b.tarball = filepath.Join(b.dir, "notes-"+version+"-linux-amd64.tar.gz")
					if err := tarGz(b.root, b.tarball); err != nil {
						t.Fatal(err)
					}
				case iupdate.Bundle:
					b.dir = out
					b.root = filepath.Join(out, "Notes.app")
					b.entries = []string{"Notes.app"}
					writeFiles(t, b.root, map[string]string{"Contents/MacOS/Notes": code(version), "Contents/Info.plist": "<plist/>"})
				}
				files, err := writeUpdates(app, out, b, true)
				if err != nil {
					t.Fatal(err)
				}
				// Published: the files, the manifest last.
				mu.Lock()
				for _, f := range files {
					os.MkdirAll(filepath.Join(published, "notes"), 0o755)
					if err := linkOrCopy(f, filepath.Join(published, "notes", filepath.Base(f))); err != nil {
						t.Fatal(err)
					}
				}
				mu.Unlock()
				return b, filepath.Join(out, "updates", iupdate.ManifestName(target, ""))
			}

			old, _ := build("1.0.0")
			b, manifestPath := build("1.0.1")
			var m iupdate.Manifest
			data, _ := os.ReadFile(manifestPath)
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if m.Version != "1.0.1" || m.Notes != "- Faster." || !strings.HasPrefix(m.URL, srv.URL+"/notes/notes-1.0.1-test-target.tar.gz") {
				t.Errorf("the manifest: %+v", m)
			}
			if len(m.Deltas) != 1 || m.Deltas[0].From != "1.0.0" || m.Deltas[0].Size >= m.Size {
				t.Fatalf("the deltas: %+v (the archive: %d bytes)", m.Deltas, m.Size)
			}
			if len(m.Previous) != 1 || m.Previous[0].Version != "1.0.0" {
				t.Errorf("the versions before: %+v", m.Previous)
			}
			// The delta, applied to the first version, makes the second.
			delta, err := os.ReadFile(filepath.Join(published, "notes", filepath.Base(m.Deltas[0].URL)))
			if err != nil {
				t.Fatal(err)
			}
			made := filepath.Join(t.TempDir(), "made")
			if err := iupdate.ApplyDelta(bytes.NewReader(delta), int64(len(delta)), "1.0.0", "1.0.1", old.root, made); err != nil {
				t.Fatal(err)
			}
			if got, want := readFiles(t, made), readFiles(t, b.root); !maps.Equal(got, want) {
				t.Errorf("the delta made %v, want %v", slices.Collect(maps.Keys(got)), slices.Collect(maps.Keys(want)))
			}
			if kind == iupdate.Prefix {
				list, _ := os.ReadFile(filepath.Join(b.root, "share/notes", iupdate.FileList))
				if want := "bin/notes\nshare/applications/com.example.notes.desktop\nshare/notes/assets/a.txt\n"; string(list) != want {
					t.Errorf("the prefix's list: %q", list)
				}
			}
		})
	}
}

// A prerelease's manifest is its channel's: releases' apps don't see it.
func TestWriteUpdatesPrerelease(t *testing.T) {
	pub, priv := testKeys(t)
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", priv)
	t.Setenv("ASUKA_UPDATE_PUBLIC_KEY", "")
	t.Setenv("ASUKA_UPDATE_URL", "")
	zero := 0
	app := &goApp{dir: t.TempDir(), name: "notes", bundleID: "com.example.notes", cfg: &appConfig{
		Name: "Notes", Version: "2.0.0-beta.1", Updates: &updatesConfig{PublicKey: pub, GitHub: "me/notes", Deltas: &zero},
	}}
	out := t.TempDir()
	b := updateBuild{target: "windows-amd64", kind: iupdate.Folder, dir: filepath.Join(out, "Notes"), root: filepath.Join(out, "Notes"), entries: []string{"Notes.exe"}}
	writeFiles(t, b.root, map[string]string{"Notes.exe": "x"})
	files, err := writeUpdates(app, out, b, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(files[len(files)-1]); got != "update-windows-amd64-beta.json" {
		t.Errorf("the manifest is %s", got)
	}
	f, m, err := updateFiles(filepath.Join(out, "updates"), "notes", "2.0.0-beta.1", "windows-amd64")
	if err != nil || len(f) != 1 || len(m) != 1 {
		t.Errorf("updateFiles: %v %v %v", f, m, err)
	}
	// Without the key, publishing stops; a build goes on without updates.
	t.Setenv("ASUKA_UPDATE_PRIVATE_KEY", "")
	if _, err := writeUpdates(app, out, b, true); err == nil {
		t.Error("published without the key")
	}
	if files, err := writeUpdates(app, out, b, false); err != nil || files != nil {
		t.Errorf("a build without the key: %v, %v", files, err)
	}
}

// Publishing to S3: the archives first, the manifests last, uncached.
func TestPublishS3Updates(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	var puts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		puts = append(puts, r.URL.Path+" "+r.Header.Get("Cache-Control"))
	}))
	defer srv.Close()
	u := &updatesConfig{URL: "https://dl.example.com/notes", S3: &s3Config{Bucket: "dl", Prefix: "notes", Endpoint: srv.URL}}
	bucket, err := newS3Client(u.S3)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"notes-1.0.1-linux-amd64.tar.gz": "a", "notes-1.0.0-to-1.0.1-linux-amd64.delta": "d", "update-linux-amd64.json": "{}"})
	j := func(n string) string { return filepath.Join(dir, n) }
	if err := publishS3(bucket, u, []string{j("notes-1.0.1-linux-amd64.tar.gz"), j("notes-1.0.0-to-1.0.1-linux-amd64.delta")}, []string{j("update-linux-amd64.json")}); err != nil {
		t.Fatal(err)
	}
	want := []string{"/dl/notes/notes-1.0.1-linux-amd64.tar.gz ", "/dl/notes/notes-1.0.0-to-1.0.1-linux-amd64.delta ", "/dl/notes/update-linux-amd64.json no-cache"}
	if !slices.Equal(puts, want) {
		t.Errorf("put %q, want %q", puts, want)
	}
}

// Publishing to GitHub with gh: the draft release made, the files then the
// manifests uploaded, the release published (a prerelease as one).
func TestPublishGitHub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script as gh")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$1 $2\" in \"release view\") exit 1;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	defer func(c string) { ghCommand = c }(ghCommand)
	ghCommand = filepath.Join(dir, "gh")
	if _, err := exec.LookPath(ghCommand); err != nil {
		t.Fatal(err)
	}
	u := &updatesConfig{GitHub: "me/notes", TagPrefix: "v"}
	if err := publishGitHub(u, "1.1.0-beta.1", "Notes", []string{"/d/a.tar.gz", "/d/a.delta"}, []string{"/d/update-x-beta.json"}, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	want := "release view v1.1.0-beta.1 --repo me/notes\n" +
		"release create v1.1.0-beta.1 --repo me/notes --draft --title v1.1.0-beta.1 --notes Notes\n" +
		"release upload v1.1.0-beta.1 --repo me/notes --clobber /d/a.tar.gz /d/a.delta\n" +
		"release upload v1.1.0-beta.1 --repo me/notes --clobber /d/update-x-beta.json\n" +
		"release edit v1.1.0-beta.1 --repo me/notes --draft=false --prerelease\n"
	if string(b) != want {
		t.Errorf("gh ran:\n%s\nwant\n%s", b, want)
	}
}

package main

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	iupdate "github.com/egoist/asuka/internal/update"
)

// Updates of desktop apps (asuka/update): asuka update keygen makes the key
// pair; asuka build builds the feed's address and the public key into the
// app, and with the private key writes, in dist/updates, the signed
// archive of each target, delta updates from the versions published
// before, and the manifest (update-<target>.json) the app checks; asuka
// publish uploads them to GitHub's releases or an S3 bucket.

// updatesConfig is asuka.json's "updates".
//
//	"updates": {
//	  "publicKey": "…the contents of asuka-update.pub…",
//	  "github": "you/notes"
//	}
type updatesConfig struct {
	// PublicKey is the contents of asuka-update.pub (asuka update keygen):
	// installed apps accept only updates signed with its private key.
	// ASUKA_UPDATE_PUBLIC_KEY overrides it.
	PublicKey string `json:"publicKey"`
	// GitHub is the public repository ("owner/name") whose releases,
	// tagged TagPrefix+version ("v1.2.0"), hold the updates: apps read the
	// latest release's manifests. With another TagPrefix (desktop-v), the
	// repository may hold other releases too, and apps find the newest
	// tagged with it through the GitHub API.
	GitHub    string `json:"github"`
	TagPrefix string `json:"tagPrefix"`
	// URL is an HTTPS directory that serves the files instead (an S3
	// bucket, R2, a CDN): apps read URL/update-<target>.json.
	// ASUKA_UPDATE_URL overrides it.
	URL string `json:"url"`
	// Feed is a server of the app's own that apps ask instead, its URL
	// with {target}, {version} (the running one) and {channel}: it answers
	// with a manifest, or 204 when there's nothing newer. The files are
	// URL's.
	Feed string `json:"feed"`
	// S3 is the bucket that serves URL, which asuka publish uploads to.
	S3 *s3Config `json:"s3"`
	// PrivateKey is the path of asuka-update.key (~/ the home directory).
	// ASUKA_UPDATE_PRIVATE_KEY, the key itself or its path, overrides it.
	PrivateKey string `json:"privateKey"`
	// Changelog is a Markdown file whose "## <version>" section is the
	// release notes (default CHANGELOG.md, when there is one).
	Changelog string `json:"changelog"`
	// Deltas is how many versions before get a delta update, a download of
	// what changed alone (default 3; 0: none).
	Deltas *int `json:"deltas"`
}

// updates returns the app's updates with the environment's overrides,
// checked, or nil when it has none.
func (c *appConfig) updates() (*updatesConfig, error) {
	u := c.Updates
	key, link := os.Getenv("ASUKA_UPDATE_PUBLIC_KEY"), os.Getenv("ASUKA_UPDATE_URL")
	if u == nil && key == "" && link == "" {
		return nil, nil
	}
	cp := updatesConfig{}
	if u != nil {
		cp = *u
	}
	u = &cp
	if key != "" {
		u.PublicKey = key
	}
	if link != "" {
		u.URL, u.GitHub = link, ""
	}
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("asuka.json: %w", err)
	}
	return u, nil
}

func (u *updatesConfig) validate() error {
	if u.PublicKey == "" {
		return errors.New("updates needs the publicKey of asuka update keygen")
	}
	if _, err := iupdate.ParsePublicKey(u.PublicKey); err != nil {
		return fmt.Errorf("updates.publicKey: %w", err)
	}
	switch {
	case (u.GitHub == "") == (u.URL == ""):
		return errors.New("updates needs either github (owner/name) or url")
	case u.GitHub != "" && strings.Count(u.GitHub, "/") != 1:
		return fmt.Errorf("updates.github %q is not owner/name", u.GitHub)
	case u.URL != "" && !httpsURL(u.URL):
		return fmt.Errorf("updates.url %q is not an https URL", u.URL)
	case u.Feed != "" && !httpsURL(u.Feed):
		return fmt.Errorf("updates.feed %q is not an https URL", u.Feed)
	case u.Feed != "" && u.URL == "":
		return errors.New("updates.feed needs url, where the archives are (the manifests name them there)")
	}
	if u.S3 != nil {
		if u.URL == "" {
			return errors.New("updates.s3 needs url, the HTTPS URL that serves the bucket's files")
		}
		if err := u.S3.validate(); err != nil {
			return err
		}
	}
	if u.Deltas != nil && *u.Deltas < 0 {
		return fmt.Errorf("updates.deltas is %d, not a number of versions", *u.Deltas)
	}
	if u.TagPrefix == "" {
		u.TagPrefix = "v"
	}
	return nil
}

// httpsURL reports whether s is an https URL, or an http one on this
// machine (a test's server).
func httpsURL(s string) bool {
	p, err := url.Parse(s)
	if err != nil || p.Host == "" {
		return false
	}
	h := p.Hostname()
	return p.Scheme == "https" || p.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1")
}

func (u *updatesConfig) deltas() int {
	if u.Deltas == nil {
		return 3
	}
	return *u.Deltas
}

// tagged reports whether apps find the newest release tagged TagPrefix
// rather than read the latest release.
func (u *updatesConfig) tagged() bool { return u.GitHub != "" && u.TagPrefix != "v" }

// feed returns the feed apps of target check (update.buildFeed).
func (u *updatesConfig) feed(target string) string {
	name := iupdate.ManifestName(target, "")
	switch {
	case u.Feed != "":
		return u.Feed
	case u.GitHub == "":
		return strings.TrimSuffix(u.URL, "/") + "/" + url.PathEscape(name)
	case u.tagged():
		return iupdate.TaggedFeed(u.GitHub, u.TagPrefix, name)
	}
	return "https://github.com/" + u.GitHub + "/releases/latest/download/" + url.PathEscape(name)
}

// fileURL returns the URL of a file of version's release.
func (u *updatesConfig) fileURL(version, name string) string {
	if u.GitHub == "" {
		return strings.TrimSuffix(u.URL, "/") + "/" + url.PathEscape(name)
	}
	return "https://github.com/" + u.GitHub + "/releases/download/" + url.PathEscape(u.TagPrefix+version) + "/" + url.PathEscape(name)
}

// updateLdflags are the -ldflags that build updates into the app of
// target: its feed, the public key and the target.
func updateLdflags(app *goApp, target string) string {
	u, err := app.cfg.updates()
	if err != nil || u == nil {
		return ""
	}
	pkg := "github.com/egoist/asuka/update."
	return " -X " + strconv.Quote(pkg+"buildFeed="+u.feed(target)) +
		" -X " + strconv.Quote(pkg+"buildKey="+strings.TrimSpace(u.PublicKey)) +
		" -X " + strconv.Quote(pkg+"buildTarget="+target)
}

// signingKey returns the private key that signs updates, nil when there's
// none: ASUKA_UPDATE_PRIVATE_KEY (the key, or its file's path), else
// updates.privateKey's file. It must be the public key's.
func (u *updatesConfig) signingKey(appDir string) (ed25519.PrivateKey, error) {
	text := strings.TrimSpace(os.Getenv("ASUKA_UPDATE_PRIVATE_KEY"))
	path := ""
	if text != "" {
		if _, err := iupdate.ParsePrivateKey(text); err != nil {
			path, text = text, ""
		}
	} else {
		path = u.PrivateKey
	}
	if path != "" {
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, rest)
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(appDir, path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("the update signing key: %w", err)
		}
		text = string(b)
	}
	if text == "" {
		return nil, nil
	}
	key, err := iupdate.ParsePrivateKey(text)
	if err != nil {
		return nil, err
	}
	if base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) != strings.TrimSpace(u.PublicKey) {
		return nil, errors.New("the update signing key isn't the private key of updates.publicKey")
	}
	return key, nil
}

// releaseNotes returns the changelog's section of version: CHANGELOG.md's
// (or updates.changelog's), "" without one.
func (u *updatesConfig) releaseNotes(appDir, version string) (string, error) {
	name := u.Changelog
	if name == "" {
		name = "CHANGELOG.md"
		if _, err := os.Stat(filepath.Join(appDir, name)); err != nil {
			return "", nil
		}
	}
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(appDir, name)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	notes := iupdate.ReleaseNotes(string(data), version)
	if notes == "" {
		return "", fmt.Errorf("%s has no notes for %s: add a \"## %s\" section", name, version, version)
	}
	return notes, nil
}

// An updateBuild is one target's build, which writeUpdates makes the
// update files of.
type updateBuild struct {
	target string
	kind   iupdate.Kind
	// dir holds the app as installed: the bundle's parent (entries: the
	// bundle), a Windows folder, a Linux prefix's tarball directory.
	dir     string
	entries []string
	// tarball is a Linux build's .tar.gz, which is the update's archive
	// (its directory: the prefix); "" for the others, whose archive is
	// written.
	tarball string
	// root is the app's tree a delta is made of: the bundle, the folder,
	// the prefix.
	root string
}

// writeFileList writes the FileList of a Windows folder or a Linux
// prefix's tree, naming what the version installs, so that its update
// removes what the next one no longer has.
func writeFileList(b updateBuild) error {
	switch b.kind {
	case iupdate.Folder:
		entries, err := os.ReadDir(b.root)
		if err != nil {
			return err
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return iupdate.WriteFileList(b.root, names)
	case iupdate.Prefix:
		var files []string
		err := filepath.WalkDir(b.root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() == iupdate.FileList {
				return err
			}
			rel, _ := filepath.Rel(b.root, p)
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return err
		}
		exe := filepath.Base(b.root)
		return iupdate.WriteFileList(filepath.Join(b.root, "share", exe, iupdate.FileList), files)
	}
	return nil
}

// writeUpdates writes, in out/updates, the update files of a target's
// build: its signed archive, deltas from the versions published before,
// and its manifest. It returns the files written (none without updates
// in asuka.json); without the signing key it says so and writes none,
// unless required (asuka publish), which fails then.
func writeUpdates(app *goApp, out string, b updateBuild, required bool) ([]string, error) {
	u, err := app.cfg.updates()
	if err != nil || u == nil {
		if required && err == nil {
			err = errors.New("asuka publish needs asuka.json's updates (asuka update keygen prints them)")
		}
		return nil, err
	}
	key, err := u.signingKey(app.dir)
	if err != nil {
		return nil, err
	}
	if key == nil {
		if required {
			return nil, errors.New("publishing needs the update signing key, or installed apps couldn't update to it: set ASUKA_UPDATE_PRIVATE_KEY or updates.privateKey")
		}
		fmt.Println("asuka: no update files: set ASUKA_UPDATE_PRIVATE_KEY (or updates.privateKey) to sign them")
		return nil, nil
	}
	version := app.cfg.Version
	if !iupdate.ValidVersion(version) {
		return nil, fmt.Errorf("asuka.json: version %q isn't one updates compare (1.2.0, 1.3.0-beta.1)", version)
	}
	notes, err := u.releaseNotes(app.dir, version)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(out, "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	slug := exeSlug(app.displayName(), app.bundleID)

	// The archive: the build's tarball on Linux, else written.
	name := slug + "-" + version + "-" + b.target + ".tar.gz"
	archive := filepath.Join(dir, name)
	os.Remove(archive)
	var size int64
	var sig string
	if b.tarball != "" {
		if err := linkOrCopy(b.tarball, archive); err != nil {
			return nil, err
		}
		size, sig, err = signFile(archive, key)
	} else {
		size, sig, err = writeSigned(archive, key, func(w io.Writer) error {
			return iupdate.WriteArchive(w, b.dir, b.entries)
		})
	}
	if err != nil {
		return nil, err
	}
	m := iupdate.Manifest{
		Version:   version,
		Notes:     notes,
		Date:      time.Now().UTC().Format(time.RFC3339),
		URL:       u.fileURL(version, name),
		Size:      size,
		Signature: sig,
	}
	written := []string{archive}
	if n := u.deltas(); n > 0 {
		deltas, err := writeDeltas(u, key, slug, version, dir, b, &m, n)
		if err != nil {
			return nil, err
		}
		written = append(written, deltas...)
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	manifest := filepath.Join(dir, iupdate.ManifestName(b.target, iupdate.Channel(version)))
	if err := os.WriteFile(manifest, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	written = append(written, manifest)
	fmt.Printf("asuka: %s: the update of %s for %s (%s), and its manifest %s\n", filepath.Join(dir, name), version, b.target, formatBytes(size), filepath.Base(manifest))
	return written, nil
}

// writeSigned writes a file with write, and returns its size and its
// signature with key.
func writeSigned(path string, key ed25519.PrivateKey, write func(io.Writer) error) (int64, string, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, "", err
	}
	sum := sha256.New()
	err = write(io.MultiWriter(f, sum))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return 0, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	return info.Size(), iupdate.Sign(key, sum.Sum(nil)), nil
}

// signFile returns a file's size and signature with key.
func signFile(path string, key ed25519.PrivateKey) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, f)
	if err != nil {
		return 0, "", err
	}
	return n, iupdate.Sign(key, sum.Sum(nil)), nil
}

// linkOrCopy makes dst the file src: a hard link, else a copy.
func linkOrCopy(src, dst string) error {
	if os.Link(src, dst) == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// writeDeltas makes delta updates to the build b from the n versions
// before it, as Sparkle does: the version of target's published manifest
// and those it lists as before it, whose archives it downloads and checks.
// It adds them to m, with the archives the next version makes its deltas
// from. Deltas only make downloads smaller: one that can't be made is
// skipped, with a word why.
func writeDeltas(u *updatesConfig, key ed25519.PrivateKey, slug, version, dir string, b updateBuild, m *iupdate.Manifest, n int) ([]string, error) {
	published, err := fetchManifest(u, b.target)
	if err != nil {
		if errors.Is(err, errNotPublished) {
			fmt.Printf("asuka: no delta updates for %s: no version is published yet\n", b.target)
		} else {
			fmt.Printf("asuka: no delta updates for %s: %v\n", b.target, err)
		}
		return nil, nil
	}
	var bases []iupdate.Archive
	for _, a := range append([]iupdate.Archive{{Version: published.Version, URL: published.URL, Size: published.Size, Signature: published.Signature}}, published.Previous...) {
		if iupdate.Compare(a.Version, version) < 0 && !slices.ContainsFunc(bases, func(x iupdate.Archive) bool { return x.Version == a.Version }) {
			bases = append(bases, a)
		}
	}
	if len(bases) == 0 {
		fmt.Printf("asuka: no delta updates for %s: the published version is %s\n", b.target, published.Version)
		return nil, nil
	}
	bases = bases[:min(len(bases), n)]
	m.Previous = bases[:min(len(bases), n-1)]

	work, err := os.MkdirTemp("", "asuka-delta-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	var files []string
	for i, base := range bases {
		name := slug + "-" + base.Version + "-to-" + version + "-" + b.target + ".delta"
		path := filepath.Join(dir, name)
		d, err := writeDelta(key, base, filepath.Join(work, strconv.Itoa(i)), path, version, b)
		switch {
		case err != nil:
			fmt.Printf("asuka: no delta update from %s for %s: %v\n", base.Version, b.target, err)
			continue
		case d.Size >= m.Size:
			os.Remove(path)
			fmt.Printf("asuka: no delta update from %s for %s: it's no smaller than the archive\n", base.Version, b.target)
			continue
		}
		d.URL = u.fileURL(version, name)
		m.Deltas = append(m.Deltas, d)
		files = append(files, path)
		fmt.Printf("asuka: the delta update from %s for %s: %s, instead of %s\n", base.Version, b.target, formatBytes(d.Size), formatBytes(m.Size))
	}
	return files, nil
}

// writeDelta downloads base's archive into work, checks that it's signed
// with key, and writes the delta from it to the build b at path.
func writeDelta(key ed25519.PrivateKey, base iupdate.Archive, work, path, version string, b updateBuild) (iupdate.Delta, error) {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return iupdate.Delta{}, err
	}
	archive := filepath.Join(work, "archive.tar.gz")
	if err := fetchSigned(base.URL, base.Size, base.Signature, key.Public().(ed25519.PublicKey), archive); err != nil {
		return iupdate.Delta{}, err
	}
	f, err := os.Open(archive)
	if err != nil {
		return iupdate.Delta{}, err
	}
	oldDir := filepath.Join(work, "app")
	err = iupdate.ExtractArchive(f, oldDir)
	f.Close()
	if err != nil {
		return iupdate.Delta{}, err
	}
	// The old app's tree, as the new one's: a bundle, a prefix's
	// directory, a folder.
	in := &iupdate.Install{Kind: b.kind}
	if b.kind == iupdate.Prefix {
		in.Exe = filepath.Base(b.root)
	}
	if oldDir, err = in.Tree(oldDir); err != nil {
		return iupdate.Delta{}, fmt.Errorf("its archive: %w", err)
	}
	size, sig, err := writeSigned(path, key, func(w io.Writer) error {
		return iupdate.WriteDelta(w, base.Version, version, oldDir, b.root, nil)
	})
	if err != nil {
		return iupdate.Delta{}, err
	}
	return iupdate.Delta{From: base.Version, Size: size, Signature: sig}, nil
}

var errNotPublished = errors.New("not published")

// fetchManifest downloads target's published manifest, the one installed
// apps of its releases read.
func fetchManifest(u *updatesConfig, target string) (*iupdate.Manifest, error) {
	feed := u.feed(target)
	if u.Feed != "" {
		feed = u.fileURL("", iupdate.ManifestName(target, "")) // the server's own files
	}
	resolved, err := iupdate.ResolveFeed(context.Background(), feed, func(_ context.Context, s string) (io.ReadCloser, error) {
		resp, err := updateGet(s, 30*time.Second)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	})
	if errors.Is(err, iupdate.ErrNotPublished) {
		return nil, errNotPublished
	}
	if err != nil {
		return nil, err
	}
	resp, err := updateGet(resolved, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m iupdate.Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, iupdate.MaxManifestSize)).Decode(&m); err != nil {
		return nil, fmt.Errorf("reading %s: %w", resolved, err)
	}
	return &m, nil
}

// fetchSigned downloads a file of an update to path, and checks its size
// and signature.
func fetchSigned(u string, size int64, signature string, pub ed25519.PublicKey, path string) error {
	resp, err := updateGet(u, 10*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return fmt.Errorf("downloading %s: %w", u, err)
	case n != size:
		return fmt.Errorf("%s has %d bytes, not %d", u, n, size)
	case iupdate.Verify(pub, sum.Sum(nil), signature) != nil:
		return fmt.Errorf("%s isn't signed with the key of updates.publicKey", u)
	}
	return nil
}

// updateGet requests a published file, in timeout at most.
func updateGet(u string, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "asuka")
	// A token, as CI has, lifts the GitHub API's limit on requests.
	if token := cmp.Or(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN")); token != "" && req.URL.Host == "api.github.com" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, errNotPublished
		}
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return resp, nil
}

// formatBytes is a size for people: "12.3 MB".
func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

// cmdUpdate is asuka update: keygen.
func cmdUpdate(args []string) error {
	if len(args) == 0 || args[0] != "keygen" {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
			fmt.Println("usage: asuka update keygen [--out dir] [--force]")
			return errHelp
		}
		return errors.New("usage: asuka update keygen [--out dir] [--force]")
	}
	config, _ := os.UserConfigDir()
	var out string
	var force bool
	fs := newFlagSet("update keygen", "")
	fs.about = `Makes the Ed25519 key pair that signs a desktop app's updates: asuka-update.key,
the secret, and asuka-update.pub. Put asuka-update.pub's contents in asuka.json's
updates.publicKey, and keep asuka-update.key out of the repository: in a
password manager, and as CI's ASUKA_UPDATE_PRIVATE_KEY secret. Installed apps
accept only updates signed with it: losing it strands them, and whoever has
it can ship code to your users.`
	fs.String(&out, "out", 'o', filepath.Join(config, "asuka", "update-keys"), "the `directory` to write the keys in")
	fs.Bool(&force, "force", 0, "replace keys that are there")
	if err := fs.parse(args[1:]); err != nil {
		return err
	}
	secret, public := filepath.Join(out, "asuka-update.key"), filepath.Join(out, "asuka-update.pub")
	if !force {
		for _, p := range []string{secret, public} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("%s holds keys already; --force replaces them", out)
			}
		}
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(secret, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return err
	}
	pubText := base64.StdEncoding.EncodeToString(pub)
	if err := os.WriteFile(public, []byte(pubText+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("asuka: wrote %s (secret: keep it safe) and %s\n\nAdd to asuka.json:\n\n  \"updates\": {\n    \"publicKey\": %q,\n    \"github\": \"owner/name\"\n  }\n", secret, public, pubText)
	return nil
}

// versionFile is the web's version.json (asuka/update on the web): the
// version built, and its release notes.
func versionFile(app *goApp) []byte {
	v := map[string]string{"version": app.cfg.Version, "build": strconv.Itoa(app.cfg.Build), "date": time.Now().UTC().Format(time.RFC3339)}
	if u, err := app.cfg.updates(); err == nil && u != nil {
		if notes, err := u.releaseNotes(app.dir, app.cfg.Version); err == nil && notes != "" {
			v["notes"] = notes
		}
	} else if notes, err := (&updatesConfig{}).releaseNotes(app.dir, app.cfg.Version); err == nil && notes != "" {
		v["notes"] = notes
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}


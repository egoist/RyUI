package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/asuka/plugin"
)

// asuka plugin publish: a plugin's build of its libraries (its build
// tool's, in build/<target> beside the manifest, package plugin), each
// file gzipped and named for its hash, uploaded to its library's
// release (a GitHub release, with gh), downloaded back and checked, then
// recorded in its manifest, which is what's checked in: the apps that
// use the plugin get the files from there. A file the release has is
// left as it is; one whose build is older than the plugin's sources
// (plugin.SourceHash) stops it: build again first.

func cmdPlugin(args []string) error {
	const use = "usage: asuka plugin publish [dir]"
	if len(args) == 0 || args[0] != "publish" {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
			fmt.Println(use)
			return errHelp
		}
		return errors.New(use)
	}
	fs := newFlagSet("plugin publish", "[dir]")
	fs.about = `Publishes a plugin's libraries: its build of each (build/<target> beside its
asuka-plugin.json, as its build tool writes it), gzipped and named for its
hash, uploaded with gh to its library's release (a GitHub release, made if
it isn't there), then recorded in asuka-plugin.json, for apps to download.
A build older than the plugin's sources stops it.`
	if err := fs.parse(args[1:]); err != nil {
		return err
	}
	dir := fs.Arg(0)
	if dir == "" {
		dir = "."
	}
	return publishPlugin(dir)
}

// pluginAsset is a library's file as published.
type pluginAsset struct {
	lib, target string
	name        string // in the release
	gz          []byte
	file        plugin.File
}

func publishPlugin(dir string) error {
	path := filepath.Join(dir, plugin.ManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := plugin.Parse(data)
	if err != nil {
		return err
	}
	source, err := plugin.SourceHash(dir, m)
	if err != nil {
		return err
	}
	for i, l := range m.Libraries {
		repo, tag, err := githubRelease(l.Release)
		if err != nil {
			return fmt.Errorf("library %s: %w", l.Name, err)
		}
		var assets []pluginAsset
		targets := make([]string, 0, len(l.Files))
		for t := range l.Files {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			f := l.Files[t]
			raw, err := os.ReadFile(plugin.BuildPath(dir, t, f.Name))
			if err != nil {
				return fmt.Errorf("library %s for %s isn't built: %w", l.Name, t, err)
			}
			if got := plugin.BuildSource(dir, t, f.Name); got != source {
				return fmt.Errorf("library %s for %s: the build is older than the plugin's sources: build it again", l.Name, t)
			}
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(raw)
			zw.Close()
			rs, gs := sha256.Sum256(raw), sha256.Sum256(buf.Bytes())
			f.RawSize, f.RawSHA256 = int64(len(raw)), hex.EncodeToString(rs[:])
			f.Size, f.SHA256 = int64(buf.Len()), hex.EncodeToString(gs[:])
			f.Asset = fmt.Sprintf("%s-%s-%s-%s.gz", l.Name, t, f.RawSHA256[:12], f.Name)
			f.Source = source
			assets = append(assets, pluginAsset{lib: l.Name, target: t, name: f.Asset, gz: buf.Bytes(), file: f})
		}
		if err := uploadAssets(repo, tag, m.Name, assets); err != nil {
			return err
		}
		// What's published is what the manifest will name: each back.
		for _, a := range assets {
			b, err := httpGet(l.Release + "/" + a.name)
			if err != nil {
				return fmt.Errorf("%s: %w", a.name, err)
			}
			if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != a.file.SHA256 {
				return fmt.Errorf("%s: the release's file isn't the build", a.name)
			}
			m.Libraries[i].Files[a.target] = a.file
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("asuka: published plugin %s, recorded in %s\n", m.Name, path)
	return nil
}

// githubRelease is a release URL's repository and tag:
// https://github.com/<owner>/<repo>/releases/download/<tag>.
func githubRelease(release string) (repo, tag string, err error) {
	u, err := url.Parse(release)
	parts := []string{}
	if err == nil {
		parts = strings.Split(strings.Trim(u.Path, "/"), "/")
	}
	if err != nil || u.Host != "github.com" || len(parts) != 5 || parts[2] != "releases" || parts[3] != "download" {
		return "", "", fmt.Errorf("its release must be a GitHub release's download URL, https://github.com/<owner>/<repo>/releases/download/<tag>, not %q", release)
	}
	return parts[0] + "/" + parts[1], parts[4], nil
}

// uploadAssets uploads to the release what it hasn't yet, making it if
// it isn't there.
func uploadAssets(repo, tag, pluginName string, assets []pluginAsset) error {
	if exec.Command("gh", "release", "view", tag, "-R", repo).Run() != nil {
		fmt.Printf("asuka: making the release %s of %s\n", tag, repo)
		notes := fmt.Sprintf("The libraries of Asuka's %s plugin, each file named for its hash: what its asuka-plugin.json names.", pluginName)
		c := exec.Command("gh", "release", "create", tag, "-R", repo, "--title", "Asuka plugin: "+pluginName, "--notes", notes, "--latest=false")
		if b, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("gh release create: %v\n%s", err, b)
		}
	}
	out, err := exec.Command("gh", "release", "view", tag, "-R", repo, "--json", "assets", "-q", ".assets[].name").Output()
	if err != nil {
		return fmt.Errorf("gh release view: %w", err)
	}
	have := map[string]bool{}
	for _, n := range strings.Fields(string(out)) {
		have[n] = true
	}
	tmp, err := os.MkdirTemp("", "asuka-plugin")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var files []string
	for _, a := range assets {
		if have[a.name] {
			continue
		}
		p := filepath.Join(tmp, a.name)
		if err := os.WriteFile(p, a.gz, 0o644); err != nil {
			return err
		}
		files = append(files, p)
	}
	if len(files) == 0 {
		fmt.Println("asuka: the release has every file")
		return nil
	}
	fmt.Printf("asuka: uploading %d files to %s %s\n", len(files), repo, tag)
	c := exec.Command("gh", append([]string{"release", "upload", tag, "-R", repo}, files...)...)
	if b, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("gh release upload: %v\n%s", err, b)
	}
	return nil
}

func httpGet(u string) ([]byte, error) {
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(resp.Body)
}

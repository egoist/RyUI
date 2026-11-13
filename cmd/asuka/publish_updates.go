package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	iupdate "github.com/egoist/asuka/internal/update"
)

// asuka publish --platform mac|windows|linux: the release build, its update
// files (writeUpdates) required, uploaded where asuka.json's updates say:
// a GitHub release of the version (with the gh CLI), or an S3 bucket
// (s3.go). The manifests go last, so that no app reads one whose files
// aren't there yet.

func desktopPlatform(p string) bool {
	return p == platformMac || p == platformWindows || p == platformLinux
}

func publishDesktop(app *goApp, o *publishOptions) error {
	u, err := app.cfg.updates()
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("publishing a desktop app needs asuka.json's updates: asuka update keygen prints them")
	}
	// Where to, and the key, before building.
	var bucket *s3Client
	switch {
	case u.S3 != nil:
		if bucket, err = newS3Client(u.S3); err != nil {
			return err
		}
	case u.GitHub != "":
		if _, err := exec.LookPath(ghCommand); err != nil {
			return errors.New("publishing to GitHub needs its CLI, gh (https://cli.github.com), signed in (gh auth login, or GH_TOKEN)")
		}
	default:
		return fmt.Errorf("asuka publish uploads to updates.github or updates.s3; with updates.url alone, build (asuka build) and upload dist/updates to %s yourself, the update-*.json manifests last", u.URL)
	}
	key, err := u.signingKey(app.dir)
	if err != nil {
		return err
	}
	if key == nil {
		return errors.New("publishing needs the update signing key, or installed apps couldn't update to it: set ASUKA_UPDATE_PRIVATE_KEY or updates.privateKey")
	}
	b := o.buildOptions
	b.publishing = true
	if err := b.setVersion(app); err != nil {
		return err
	}
	if b.out == "" {
		b.out = filepath.Join(app.dir, "dist")
	}
	if err := os.MkdirAll(b.out, 0o755); err != nil {
		return err
	}
	vetBuild(app.dir, !b.noVet)
	var target string
	switch b.platform {
	case platformMac:
		target = macTarget
		err = buildMac(app, &b)
	case platformWindows:
		arch, aerr := windowsArch(b.arch)
		if aerr != nil {
			return aerr
		}
		target = "windows-" + arch
		err = buildWindows(app, &b)
	case platformLinux:
		arch, aerr := linuxArch(b.arch)
		if aerr != nil {
			return aerr
		}
		target = "linux-" + arch
		err = buildLinux(app, &b)
	}
	if err != nil {
		return err
	}
	files, manifests, err := updateFiles(filepath.Join(b.out, "updates"), exeSlug(app.displayName(), app.bundleID), app.cfg.Version, target)
	if err != nil {
		return err
	}
	if bucket != nil {
		return publishS3(bucket, u, files, manifests)
	}
	notes, _ := u.releaseNotes(app.dir, app.cfg.Version)
	return publishGitHub(u, app.cfg.Version, notes, files, manifests, o.draft)
}

// updateFiles returns the update files of version for target in dir, and
// apart its manifests.
func updateFiles(dir, slug, version, target string) (files, manifests []string, err error) {
	archive := filepath.Join(dir, slug+"-"+version+"-"+target+".tar.gz")
	if _, err := os.Stat(archive); err != nil {
		return nil, nil, fmt.Errorf("the build wrote no update: %w", err)
	}
	files = append(files, archive)
	deltas, _ := filepath.Glob(filepath.Join(dir, slug+"-*-to-"+version+"-"+target+".delta"))
	files = append(files, deltas...)
	manifest := filepath.Join(dir, iupdate.ManifestName(target, iupdate.Channel(version)))
	if _, err := os.Stat(manifest); err != nil {
		return nil, nil, err
	}
	return files, []string{manifest}, nil
}

// ghCommand is the GitHub CLI (tests replace it).
var ghCommand = "gh"

// publishGitHub uploads the files to the release tagged with the
// version (TagPrefix + version), a draft made if there's none, the
// manifests last; then publishes it unless draft, as a prerelease for a
// prerelease (which only apps on its channel take), and not as the latest
// with a tag prefix of the app's own (the repository's latest is another's).
func publishGitHub(u *updatesConfig, version, notes string, files, manifests []string, draft bool) error {
	repo, tag := u.GitHub, u.TagPrefix+version
	gh := func(args ...string) error {
		out, err := exec.Command(ghCommand, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("gh %s: %v\n%s", args[0]+" "+args[1], err, out)
		}
		return nil
	}
	if exec.Command(ghCommand, "release", "view", tag, "--repo", repo).Run() != nil {
		args := []string{"release", "create", tag, "--repo", repo, "--draft", "--title", tag, "--notes", notes}
		if u.tagged() {
			args = append(args, "--latest=false")
		}
		if err := gh(args...); err != nil {
			return err
		}
		fmt.Printf("asuka: made the draft release %s of %s\n", tag, repo)
	}
	for _, batch := range [][]string{files, manifests} {
		if err := gh(append([]string{"release", "upload", tag, "--repo", repo, "--clobber"}, batch...)...); err != nil {
			return err
		}
		fmt.Printf("asuka: uploaded %s\n", names(batch))
	}
	edit := []string{"release", "edit", tag, "--repo", repo, "--draft=false"}
	if iupdate.Channel(version) != "" {
		edit = append(edit, "--prerelease")
	}
	if u.tagged() {
		edit = append(edit, "--latest=false")
	}
	if draft {
		fmt.Printf("asuka: uploaded to the draft release %s of %s; publish it once every platform is there:\n  gh %s\n", tag, repo, strings.Join(edit, " "))
		return nil
	}
	if err := gh(edit...); err != nil {
		return err
	}
	fmt.Printf("asuka: published the release %s of %s: installed apps update to it\n", tag, repo)
	return nil
}

// publishS3 uploads the files to the bucket that updates.url serves, the
// manifests last, which a CDN is told not to keep (their names stay from
// version to version).
func publishS3(bucket *s3Client, u *updatesConfig, files, manifests []string) error {
	for _, f := range slices.Concat(files, manifests) {
		name := filepath.Base(f)
		header := http.Header{}
		if strings.HasSuffix(name, ".json") {
			header.Set("Cache-Control", "no-cache")
			header.Set("Content-Type", "application/json")
		} else {
			header.Set("Content-Type", "application/octet-stream")
		}
		if err := bucket.put(bucket.key(name), f, header); err != nil {
			return err
		}
		fmt.Printf("asuka: uploaded %s\n", name)
	}
	fmt.Printf("asuka: uploaded to s3://%s/%s, which %s serves: installed apps update to it\n", bucket.bucket, bucket.prefix, u.URL)
	return nil
}

func names(paths []string) string {
	var n []string
	for _, p := range paths {
		n = append(n, filepath.Base(p))
	}
	return strings.Join(n, ", ")
}

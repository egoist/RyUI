package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	iupdate "github.com/egoist/asuka/internal/update"
)

// Linux: the app's Go code and its host (host/linux, GTK 4 and
// libadwaita through purego) are one Go program, built with
// CGO_ENABLED=0 for amd64 or arm64, from any system.
//
// asuka dev --platform linux runs it here and, on each change, rebuilds it
// and runs it again (its signals' state kept, as with the player: Asuka
// sends it SIGINT, and it saves its state and quits). On another system
// it builds it, and runs it only where ASUKA_LINUX_RUN says how: a
// command that runs a program on a Linux machine that sees this one's
// files at the same paths (a VM: orb -m ubuntu, limactl shell default).
//
// asuka build --platform linux makes a folder laid out as an install
// prefix: bin/<exe>; share/<exe>/assets, where the host finds them from
// the executable; its plugins' libraries in lib/<exe>, where package
// plugin finds them likewise; the desktop file, icons and AppStream metainfo named
// for the app's ID, as desktops and software centers look them up; a
// .tar.gz of it, to unpack into ~/.local or /usr/local; and a Flatpak
// manifest that packages it, for flatpak-builder.

// flatpakRuntime is the GNOME runtime the Flatpak manifest asks for
// (asuka.json's linux.flatpakRuntime overrides it): the host needs GTK
// 4.14 and libadwaita 1.5, GNOME 46's, or later.
const flatpakRuntime = "50"

// linuxIconSizes are the sizes of the app's icon in the hicolor theme.
var linuxIconSizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

// linuxLdflags is the -ldflags of a Linux build: asuka.json's (its
// accent), and the app's name (its window title), ID (its GApplication's,
// which names its desktop file, its D-Bus name and its data directory),
// version and build (its crash reports' and About's).
func linuxLdflags(app *goApp, release bool) string {
	f := strings.TrimPrefix(app.cfg.ldflags(release), "-ldflags=")
	set := [][2]string{{"Name", app.displayName()}, {"ID", app.bundleID}}
	if app.cfg != nil {
		set = append(set, [2]string{"Version", app.cfg.Version}, [2]string{"Build", strconv.Itoa(app.cfg.Build)})
	}
	for _, kv := range set {
		f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot."+kv[0]+"="+kv[1])
	}
	f += app.cfg.multipleInstances()
	return "-ldflags=" + strings.TrimSpace(f)
}

// linuxExe is the executable's name: the app's, in lower case, words
// joined by hyphens, as Linux names its programs.
func linuxExe(app *goApp) string { return exeSlug(app.displayName(), app.bundleID) }

// exeSlug is name in lower case, its words joined by hyphens; for a name
// in another script, id's last part.
func exeSlug(name, id string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	if b.Len() == 0 {
		// A name in another script: the ID's last part.
		if i := strings.LastIndexByte(id, '.'); i >= 0 {
			id = id[i+1:]
		}
		if id = strings.ToLower(id); id != "" {
			return id
		}
		return "app"
	}
	return b.String()
}

// linuxArch is the GOARCH of a Linux build: arch (amd64 or arm64), else
// this machine's where it's one of them, else amd64.
func linuxArch(arch string) (string, error) {
	switch arch {
	case "amd64", "arm64":
		return arch, nil
	case "":
		if runtime.GOARCH == "arm64" {
			return "arm64", nil
		}
		return "amd64", nil
	}
	return "", fmt.Errorf("--arch %q: amd64 or arm64", arch)
}

// gAppIDValid reports whether id is a valid GApplication ID, as
// g_application_id_is_valid has it: two elements or more, of letters,
// digits, _ and -, none starting with a digit.
func gAppIDValid(id string) bool {
	parts := strings.Split(id, ".")
	if len(id) > 255 || len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || p[0] >= '0' && p[0] <= '9' {
			return false
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return false
			}
		}
	}
	return true
}

func devLinux(dir string, o *devOptions, stop <-chan os.Signal) error {
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	work, err := workDir(app, "linux")
	if err != nil {
		return err
	}
	if !gAppIDValid(app.bundleID) {
		fmt.Printf("asuka: %q isn't a valid application ID on Linux (letters, digits, _ and -, in two parts or more, none starting with a digit): the app runs under another\n", app.bundleID)
	}
	proc := &hostProcess{app: app, work: work, flags: linuxLdflags(app, false), goos: "linux",
		bin: filepath.Join(work, linuxExe(app))}
	wrap := strings.Fields(os.Getenv("ASUKA_LINUX_RUN"))
	where := "this computer"
	if runtime.GOOS != "linux" {
		if len(wrap) == 0 {
			if proc.goarch, err = linuxArch(""); err != nil {
				return err
			}
			if err := proc.build(); err != nil {
				return err
			}
			return fmt.Errorf("built %s (linux/%s), but a Linux app runs only on Linux: run asuka dev there, or set ASUKA_LINUX_RUN to a command that runs a program on a Linux machine with a display that sees this computer's files at the same paths (orb -m ubuntu, limactl shell default). asuka build --platform linux builds a release from here", proc.bin, proc.goarch)
		}
		// The machine's architecture, from uname.
		out, err := exec.Command(wrap[0], append(wrap[1:len(wrap):len(wrap)], "uname", "-m")...).Output()
		if err != nil {
			return fmt.Errorf("ASUKA_LINUX_RUN=%q: uname -m: %v", os.Getenv("ASUKA_LINUX_RUN"), err)
		}
		switch m := strings.TrimSpace(string(out)); m {
		case "aarch64", "arm64":
			proc.goarch = "arm64"
		case "x86_64", "amd64":
			proc.goarch = "amd64"
		default:
			return fmt.Errorf("ASUKA_LINUX_RUN: a %s machine; Asuka runs on amd64 and arm64", m)
		}
		proc.wrap = wrap
		where = wrap[0]
	}
	fmt.Printf("asuka: %s on %s\n", app.name, where)
	proc.env = []string{
		"ASUKA_DATA=" + filepath.Join(work, "data"),
		// Read where they are, so changing one needs no rebuild.
		"ASUKA_ASSETS=" + filepath.Join(app.dir, "assets"),
	}
	if !o.once {
		proc.env = append(proc.env, "ASUKA_STATE="+filepath.Join(work, "state.json"))
	}
	os.MkdirAll(filepath.Join(work, "data"), 0o755)
	start := time.Now()
	if err := proc.build(); err != nil {
		return err
	}
	if err := proc.restart(); err != nil {
		return err
	}
	fmt.Printf("asuka: built and started in %.1fs\n", time.Since(start).Seconds())
	if o.once {
		// Until it quits, or Asuka is stopped (it stops the app).
		select {
		case <-proc.done():
		case <-stop:
			proc.stop()
		}
		return nil
	}
	return watchHost(proc, stop, func() {})
}

func buildLinux(app *goApp, o *buildOptions) error {
	arch, err := linuxArch(o.arch)
	if err != nil {
		return err
	}
	if !gAppIDValid(app.bundleID) {
		return fmt.Errorf("asuka.json: id %q isn't a valid application ID on Linux: letters, digits, _ and -, in two parts or more, none starting with a digit (com.example.notes)", app.bundleID)
	}
	exe := linuxExe(app)
	base := filepath.Join(o.out, "linux")
	out := filepath.Join(base, exe)
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "bin"), 0o755); err != nil {
		return err
	}
	target := "linux-" + arch
	libs, err := appLibs(app.dir, "linux", arch, target)
	if err != nil {
		return err
	}
	args := append([]string{"build", "-trimpath", linuxLdflags(app, true) + updateLdflags(app, target)}, pluginBuildFlags(libs)...)
	args = append(args, "-o", filepath.Join(out, "bin", exe), ".")
	if o.verbose {
		fmt.Println("+ GOOS=linux GOARCH="+arch+" CGO_ENABLED=0 go", strings.Join(args, " "))
	}
	c := exec.Command("go", args...)
	c.Dir = app.dir
	c.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, b)
	}
	if err := writeLibs(libs, linuxPluginLibs(out, exe)); err != nil {
		return err
	}
	if err := writeLinuxShare(app, out, exe); err != nil {
		return err
	}
	u, _ := app.cfg.updates()
	b := updateBuild{target: target, kind: iupdate.Prefix, dir: base, root: out}
	if u != nil {
		// What the version installs, which its update replaces.
		if err := writeFileList(b); err != nil {
			return err
		}
	}
	tgz := filepath.Join(base, fmt.Sprintf("%s-%s-linux-%s.tar.gz", exe, app.cfg.Version, arch))
	if err := tarGz(out, tgz); err != nil {
		return err
	}
	if u != nil || o.publishing {
		// The tarball is the update's archive.
		b.tarball = tgz
		if _, err := writeUpdates(app, o.out, b, o.publishing); err != nil {
			return err
		}
	}
	manifest := filepath.Join(base, app.bundleID+".yml")
	if err := os.WriteFile(manifest, []byte(flatpakManifest(app, exe, len(libs) > 0)), 0o644); err != nil {
		return err
	}
	fmt.Printf("asuka: %s (linux/%s; it needs GTK 4.14 and libadwaita 1.5 or later)\n", filepath.Join(out, "bin", exe), arch)
	fmt.Printf("asuka: %s, to install: tar -xzf %s --strip-components=1 -C ~/.local\n", tgz, filepath.Base(tgz))
	if mimePackage(app.cfg.fileTypes()) != "" {
		fmt.Println("asuka: then update-mime-database ~/.local/share/mime and update-desktop-database ~/.local/share/applications, for its file types (a package manager and Flatpak do it themselves)")
	}
	fmt.Printf("asuka: %s, a Flatpak: flatpak-builder --user --install --force-clean build %s\n", manifest, filepath.Base(manifest))
	l := app.cfg.Linux
	if l.Summary == "" || l.Description == "" || l.License == "" || l.Developer == "" {
		fmt.Println("asuka: software centers and Flathub want linux.summary, description, license and developer in asuka.json (the AppStream metainfo's)")
	}
	return nil
}

// linuxPluginLibs is where a Linux build puts its plugins' libraries in
// the prefix out: lib/<exe>, the program's own directory in lib, where
// package plugin finds them from bin/<exe>, and where they can't be taken
// for the system's libraries.
func linuxPluginLibs(out, exe string) string {
	return filepath.Join(out, "lib", exe)
}

// writeLinuxShare writes the build's share/ in the prefix out: the
// app's assets (share/<exe>/assets, which the host finds from
// bin/<exe>), its icon at each size, its desktop file and its AppStream
// metainfo.
func writeLinuxShare(app *goApp, out, exe string) error {
	share := filepath.Join(out, "share")
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(share, exe, "assets")); err != nil {
		return err
	}
	id := app.bundleID
	icon := app.cfg.Icon != ""
	if icon {
		img, err := loadIcon(app.cfg.Icon)
		if err != nil {
			return err
		}
		for _, s := range linuxIconSizes {
			p := filepath.Join(share, "icons", "hicolor", fmt.Sprintf("%dx%d", s, s), "apps", id+".png")
			if err := writePNG(p, resize(img, s)); err != nil {
				return err
			}
		}
	}
	files := map[string]string{
		filepath.Join(share, "applications", id+".desktop"):  desktopEntry(app, exe, icon),
		filepath.Join(share, "metainfo", id+".metainfo.xml"): metainfo(app, icon, time.Now()),
	}
	// asuka.json's fileTypes: the MIME types the app defines, for
	// shared-mime-info (named for the app's ID, as Flatpak exports it),
	// and their icons in the hicolor theme.
	types := app.cfg.fileTypes()
	if pkg := mimePackage(types); pkg != "" {
		files[filepath.Join(share, "mime", "packages", id+".xml")] = pkg
	}
	for i, t := range types {
		if t.icon == "" {
			continue
		}
		img, err := loadIcon(t.icon)
		if err != nil {
			return fmt.Errorf("fileTypes[%d]'s icon: %w", i, err)
		}
		for _, m := range t.mimeTypes {
			if !t.definesMIME(m) {
				continue
			}
			for _, s := range linuxIconSizes {
				p := filepath.Join(share, "icons", "hicolor", fmt.Sprintf("%dx%d", s, s), "mimetypes", mimeIconName(m)+".png")
				if err := writePNG(p, resize(img, s)); err != nil {
					return err
				}
			}
		}
	}
	for p, s := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// desktopEntry is the app's desktop file: what launchers show and run.
// StartupWMClass is its windows' WM_CLASS on X11 (the program's name);
// on Wayland they carry its ID, which names the file.
func desktopEntry(app *goApp, exe string, icon bool) string {
	l := app.cfg.Linux
	var b strings.Builder
	b.WriteString("[Desktop Entry]\nType=Application\n")
	fmt.Fprintf(&b, "Name=%s\n", desktopEscape(app.displayName()))
	if l.Summary != "" {
		fmt.Fprintf(&b, "Comment=%s\n", desktopEscape(l.Summary))
	}
	types := app.cfg.fileTypes()
	if app.cfg.Scheme != "" || len(types) > 0 {
		// Its links and files open it: their URIs are the arguments
		// (host/linux's command line).
		fmt.Fprintf(&b, "Exec=%s %%U\n", exe)
	} else {
		fmt.Fprintf(&b, "Exec=%s\n", exe)
	}
	if icon {
		fmt.Fprintf(&b, "Icon=%s\n", app.bundleID)
	}
	b.WriteString("Terminal=false\n")
	fmt.Fprintf(&b, "Categories=%s;\n", strings.Join(linuxCategories(app), ";"))
	if len(l.Keywords) > 0 {
		fmt.Fprintf(&b, "Keywords=%s;\n", desktopEscape(strings.Join(l.Keywords, ";")))
	}
	if ms := desktopMimeTypes(app, types); len(ms) > 0 {
		fmt.Fprintf(&b, "MimeType=%s;\n", strings.Join(ms, ";"))
	}
	b.WriteString("StartupNotify=true\n")
	fmt.Fprintf(&b, "StartupWMClass=%s\n", exe)
	return b.String()
}

// linuxCategories are the app's freedesktop.org categories (default
// Utility).
func linuxCategories(app *goApp) []string {
	if c := app.cfg.Linux.Categories; len(c) > 0 {
		return c
	}
	return []string{"Utility"}
}

// desktopEscape escapes a desktop file's string value.
func desktopEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}

// metainfo is the app's AppStream metainfo: what software centers
// (GNOME Software, KDE Discover, Flathub) show of it.
func metainfo(app *goApp, icon bool, now time.Time) string {
	l := app.cfg.Linux
	e := html.EscapeString
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<component type=\"desktop-application\">\n")
	fmt.Fprintf(&b, "  <id>%s</id>\n", e(app.bundleID))
	b.WriteString("  <metadata_license>CC0-1.0</metadata_license>\n")
	if l.License != "" {
		fmt.Fprintf(&b, "  <project_license>%s</project_license>\n", e(l.License))
	}
	fmt.Fprintf(&b, "  <name>%s</name>\n", e(app.displayName()))
	summary := l.Summary
	if summary == "" {
		summary = app.displayName()
	}
	fmt.Fprintf(&b, "  <summary>%s</summary>\n", e(summary))
	if l.Developer != "" {
		// Its ID: the reverse domain name the app's ID starts with.
		dev := app.bundleID
		if i := strings.LastIndexByte(dev, '.'); i > 0 {
			dev = dev[:i]
		}
		fmt.Fprintf(&b, "  <developer id=\"%s\">\n    <name>%s</name>\n  </developer>\n", e(dev), e(l.Developer))
	}
	// A description is required: without one, the summary.
	desc := l.Description
	if strings.TrimSpace(desc) == "" {
		desc = summary
	}
	b.WriteString("  <description>\n")
	for _, p := range strings.Split(strings.ReplaceAll(desc, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			fmt.Fprintf(&b, "    <p>%s</p>\n", e(strings.Join(strings.Fields(p), " ")))
		}
	}
	b.WriteString("  </description>\n")
	b.WriteString("  <categories>\n")
	for _, c := range linuxCategories(app) {
		fmt.Fprintf(&b, "    <category>%s</category>\n", e(c))
	}
	b.WriteString("  </categories>\n")
	fmt.Fprintf(&b, "  <launchable type=\"desktop-id\">%s.desktop</launchable>\n", e(app.bundleID))
	if icon {
		fmt.Fprintf(&b, "  <icon type=\"stock\">%s</icon>\n", e(app.bundleID))
	}
	if l.Homepage != "" {
		fmt.Fprintf(&b, "  <url type=\"homepage\">%s</url>\n", e(l.Homepage))
	}
	b.WriteString("  <content_rating type=\"oars-1.1\"/>\n")
	b.WriteString("  <requires>\n    <display_length compare=\"ge\">360</display_length>\n  </requires>\n")
	b.WriteString("  <supports>\n    <control>pointing</control>\n    <control>keyboard</control>\n    <control>touch</control>\n  </supports>\n")
	fmt.Fprintf(&b, "  <releases>\n    <release version=\"%s\" date=\"%s\"/>\n  </releases>\n", e(app.cfg.Version), now.Format("2006-01-02"))
	b.WriteString("</component>\n")
	return b.String()
}

// flatpakManifest is a flatpak-builder manifest that packages the build's
// prefix (beside it, in the directory named exe) on the GNOME runtime:
// its bin and share, and its lib with its plugins' libraries (lib).
func flatpakManifest(app *goApp, exe string, lib bool) string {
	rt := app.cfg.Linux.FlatpakRuntime
	if rt == "" {
		rt = flatpakRuntime
	}
	dirs := "bin share"
	if lib {
		dirs = "bin lib share"
	}
	return fmt.Sprintf(`# A Flatpak of %[1]s, from asuka build --platform linux (the binary it built,
# for one architecture): flatpak-builder --user --install --force-clean build %[2]s.yml
# Then: flatpak run %[2]s
app-id: %[2]s
runtime: org.gnome.Platform
runtime-version: '%[3]s'
sdk: org.gnome.Sdk
command: %[4]s
finish-args:
  - --share=ipc
  - --share=network
  - --socket=wayland
  - --socket=fallback-x11
  - --device=dri
modules:
  - name: %[4]s
    buildsystem: simple
    build-commands:
      - cp -a %[5]s /app/
    sources:
      - type: dir
        path: %[4]s
`, app.displayName(), app.bundleID, rt, exe, dirs)
}

// tarGz writes dir to file as a gzipped tar, its entries under dir's
// name, with their modes (the executable's).
func tarGz(dir, file string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	parent := filepath.Dir(dir)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(parent, p)
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
		if info.IsDir() {
			h.Name += "/"
			h.Mode = 0o755
		} else if info.Mode()&0o111 != 0 || strings.HasSuffix(filepath.ToSlash(filepath.Dir(rel)), "/bin") {
			h.Mode = 0o755 // built on Windows too: bin's files are executables
		} else {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}

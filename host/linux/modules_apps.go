//go:build linux && !android

package linux

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/asuka/internal/appdir"
	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/apps on Linux: the desktop entries a menu shows, read as the
// XDG specs say (the base directories, the desktop entry spec), their
// icons from the GTK icon theme, drawn by GSK's Cairo renderer, and
// their apps launched by GIO with the display's launch context.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["apps"] = map[string]method{
			"list": func(_ []byte, reply func([]byte, error)) {
				h.offMain(func() ([]byte, error) { return json.Marshal(listApps()) }, reply)
			},
			"icon": h.appIcon,
			"open": h.openApp,
		}
	})
}

// installedApp is apps.App on the wire.
type installedApp struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

// appDirs are the folders of desktop entries, the first first: the
// user's ($XDG_DATA_HOME/applications), then each of $XDG_DATA_DIRS's,
// then Flatpak's exports (the user's and the system's) and Snap's where
// $XDG_DATA_DIRS doesn't have them (a session without their profile
// scripts).
func appDirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataHome) {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := []string{dataHome}
	for _, d := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if filepath.IsAbs(d) {
			dataDirs = append(dataDirs, d)
		}
	}
	if len(dataDirs) == 1 {
		dataDirs = append(dataDirs, "/usr/local/share", "/usr/share")
	}
	dataDirs = append(dataDirs, filepath.Join(dataHome, "flatpak", "exports", "share"), "/var/lib/flatpak/exports/share")
	var out []string
	seen := map[string]bool{}
	for _, d := range dataDirs {
		a := filepath.Join(filepath.Clean(d), "applications")
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	if snap := "/var/lib/snapd/desktop/applications"; !seen[snap] {
		out = append(out, snap)
	}
	return out
}

// listApps reads the desktop entries of appDirs: an entry's ID is its
// path under its folder, / made -, and the first entry of an ID hides
// the later ones, whatever it says (a user's copy with Hidden=true
// takes the system's away). The apps are the entries a menu shows.
func listApps() []installedApp {
	langs := languageNames()
	desktops := currentDesktops()
	seen := map[string]bool{}
	var out []installedApp
	for _, dir := range appDirs() {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".desktop") {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return nil
			}
			id := strings.ReplaceAll(rel, string(filepath.Separator), "-")
			if seen[id] {
				return nil
			}
			b, err := os.ReadFile(p) // through a link: Flatpak's exports are
			if err != nil {
				return nil
			}
			seen[id] = true
			e, ok := parseDesktopEntry(b)
			if !ok || !e.shown(desktops) {
				return nil
			}
			name := e.localized(langs)
			if name == "" {
				return nil
			}
			out = append(out, installedApp{Name: name, ID: id, Path: p})
			return nil
		})
	}
	return out
}

// desktopEntry is what a launcher reads of a desktop entry's [Desktop
// Entry] group.
type desktopEntry struct {
	typ, exec, tryExec, icon string
	names                    map[string]string // by locale; "": Name's
	noDisplay, hidden, dbus  bool
	onlyShowIn, notShowIn    []string
}

// parseDesktopEntry reads a desktop entry's main group; false if it has
// none.
func parseDesktopEntry(b []byte) (desktopEntry, bool) {
	e := desktopEntry{names: map[string]string{}}
	in, found := false, false
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if found && in {
				break // the main group's done: actions and others follow
			}
			in = line == "[Desktop Entry]"
			found = found || in
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		key, locale := k, ""
		if i := strings.IndexByte(k, '['); i > 0 && strings.HasSuffix(k, "]") {
			key, locale = k[:i], k[i+1:len(k)-1]
		}
		switch key {
		case "Name":
			if _, dup := e.names[locale]; !dup {
				e.names[locale] = unescapeDesktop(v)
			}
		}
		if locale != "" {
			continue
		}
		switch key {
		case "Type":
			e.typ = v
		case "Exec":
			e.exec = v
		case "TryExec":
			e.tryExec = unescapeDesktop(v)
		case "Icon":
			e.icon = unescapeDesktop(v)
		case "NoDisplay":
			e.noDisplay = desktopBool(v)
		case "Hidden":
			e.hidden = desktopBool(v)
		case "DBusActivatable":
			e.dbus = desktopBool(v)
		case "OnlyShowIn":
			e.onlyShowIn = desktopList(v)
		case "NotShowIn":
			e.notShowIn = desktopList(v)
		}
	}
	return e, found
}

// shown reports whether a menu shows the entry on the current desktops:
// an application's, not hidden from menus or deleted, meant for one of
// the desktops and not kept from them, its TryExec there.
func (e desktopEntry) shown(desktops []string) bool {
	if e.typ != "Application" || e.hidden || e.noDisplay || (e.exec == "" && !e.dbus) {
		return false
	}
	in := func(list []string) bool {
		for _, d := range desktops {
			for _, l := range list {
				if strings.EqualFold(d, l) {
					return true
				}
			}
		}
		return false
	}
	if len(e.onlyShowIn) > 0 && !in(e.onlyShowIn) || in(e.notShowIn) {
		return false
	}
	if e.tryExec != "" {
		if filepath.IsAbs(e.tryExec) {
			st, err := os.Stat(e.tryExec)
			if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
				return false
			}
		} else if _, err := exec.LookPath(e.tryExec); err != nil {
			return false
		}
	}
	return true
}

// localized is the entry's name in the first of langs it has
// (languageNames' forms of each), else its Name.
func (e desktopEntry) localized(langs []string) string {
	for _, l := range langs {
		if n, ok := e.names[l]; ok && n != "" {
			return n
		}
	}
	return e.names[""]
}

// languageNames are the forms of the user's languages a desktop entry's
// keys are localized by, best first, as GLib's g_get_language_names
// has them: LANGUAGE's list (unless the locale is C), then LC_ALL,
// LC_MESSAGES or LANG; each lang_COUNTRY.ENCODING@MODIFIER as
// lang_COUNTRY@MODIFIER, lang_COUNTRY, lang@MODIFIER, lang.
func languageNames() []string {
	locale := ""
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale = os.Getenv(v); locale != "" {
			break
		}
	}
	var list []string
	if l := os.Getenv("LANGUAGE"); l != "" && locale != "C" && locale != "POSIX" {
		list = strings.Split(l, ":")
	}
	list = append(list, locale)
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, l := range list {
		if l == "" || l == "C" || l == "POSIX" || strings.HasPrefix(l, "C.") {
			continue
		}
		lang, mod, _ := strings.Cut(l, "@")
		lang, _, _ = strings.Cut(lang, ".")
		base, country, _ := strings.Cut(lang, "_")
		if mod != "" {
			mod = "@" + mod
		}
		if country != "" {
			add(base + "_" + country + mod)
			add(base + "_" + country)
		}
		add(base + mod)
		add(base)
	}
	return out
}

// currentDesktops are $XDG_CURRENT_DESKTOP's (GNOME, KDE, ubuntu:GNOME).
func currentDesktops() []string {
	var out []string
	for _, d := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

func desktopBool(v string) bool { return v == "true" || v == "1" }

// desktopList splits a list value at its semicolons (\; is one in an
// item); empty items go.
func desktopList(v string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '\\' && i+1 < len(v) && v[i+1] == ';':
			b.WriteByte(';')
			i++
		case v[i] == ';':
			if b.Len() > 0 {
				out = append(out, unescapeDesktop(b.String()))
			}
			b.Reset()
		default:
			b.WriteByte(v[i])
		}
	}
	if b.Len() > 0 {
		out = append(out, unescapeDesktop(b.String()))
	}
	return out
}

// unescapeDesktop reads a string value's escapes: \s, \n, \t, \r, \\.
func unescapeDesktop(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i+1 == len(v) {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(v[i])
		}
	}
	return b.String()
}

// appIcon draws an entry's icon as a PNG in the caches, size points
// square at the first monitor's scale, and answers with its path: its
// Icon from the icon theme (application-x-executable where the theme
// has none of it), or the file it names. A file drawn since the entry
// and the icon's file last changed is used again. GTK looks up and
// draws on the main thread; the PNG's encoded off it.
func (h *Host) appIcon(args []byte, reply func([]byte, error)) {
	var a struct {
		Path string  `json:"path"`
		Size float64 `json:"size"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Path == "" || !(a.Size > 0) {
		reply(nil, errors.New("apps.icon: bad arguments"))
		return
	}
	b, err := os.ReadFile(a.Path)
	if err != nil {
		reply(nil, fmt.Errorf("no app at %s", a.Path))
		return
	}
	e, ok := parseDesktopEntry(b)
	if !ok {
		reply(nil, fmt.Errorf("%s isn't a desktop entry", a.Path))
		return
	}
	scale := monitorScale()
	px := int(math.Ceil(a.Size * scale))
	p, file := appIconPaintable(e.icon, int32(math.Ceil(a.Size)), int32(math.Ceil(scale)))
	if p == 0 {
		reply(nil, fmt.Errorf("no icon for %s", a.Path))
		return
	}
	out := appIconFile(a.Path, px)
	if appIconFresh(out, a.Path, file) {
		ObjectUnref(p)
		reply([]byte(out), nil)
		return
	}
	img := paintableImage(p, px)
	ObjectUnref(p)
	if img == nil {
		reply(nil, fmt.Errorf("couldn't draw the icon of %s", a.Path))
		return
	}
	h.offMain(func() ([]byte, error) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		if err := writeAppIcon(out, buf.Bytes()); err != nil {
			return nil, err
		}
		return []byte(out), nil
	}, reply)
}

// monitorScale is the first monitor's scale (fractional, GTK 4.14), 1
// without one.
func monitorScale() float64 {
	d := DisplayGetDefault()
	if d == 0 || Desk.DisplayGetMonitors == nil {
		return 1
	}
	m := Sys.ListModelGetItem(Desk.DisplayGetMonitors(d), 0)
	if m == 0 {
		return 1
	}
	defer ObjectUnref(m)
	s := 0.0
	if Desk.MonitorGetScale != nil {
		s = Desk.MonitorGetScale(m)
	}
	if s <= 0 {
		s = float64(Desk.MonitorGetScaleFactor(m))
	}
	return max(1, s)
}

// The functions of GTK's icons and GIO's apps this file calls.
var (
	fnIconPaintableNewForFile, fnIconPaintableGetFile Ptr
	fnCairoRendererNew, fnRendererRealize             Ptr
	fnRendererRealizeForDisplay                       Ptr
	fnDesktopAppInfoNew, fnLaunchContext              Ptr
	fnLaunchURIsAsync, fnLaunchURIsFinish             Ptr
)

func bindApps() {
	if fnIconPaintableNewForFile != 0 {
		return
	}
	fnIconPaintableNewForFile = Sym("gtk_icon_paintable_new_for_file")
	fnIconPaintableGetFile = Sym("gtk_icon_paintable_get_file")
	fnCairoRendererNew = Sym("gsk_cairo_renderer_new")
	fnRendererRealize = Sym("gsk_renderer_realize")
	fnRendererRealizeForDisplay = Sym("gsk_renderer_realize_for_display") // 4.14
	fnDesktopAppInfoNew = Sym("g_desktop_app_info_new_from_filename")
	fnLaunchContext = Sym("gdk_display_get_app_launch_context")
	fnLaunchURIsAsync = Sym("g_app_info_launch_uris_async")
	fnLaunchURIsFinish = Sym("g_app_info_launch_uris_finish")
}

// appIconPaintable is an entry's Icon at a size and scale (a reference
// the caller drops), and the file it's drawn from ("" for one of GTK's
// resources): a file it names, or the theme's icon of that name (or of
// the name without an extension, as entries written for other
// launchers have it), else the theme's application-x-executable.
func appIconPaintable(icon string, size, scale int32) (Ptr, string) {
	bindApps()
	if filepath.IsAbs(icon) {
		if st, err := os.Stat(icon); err == nil && !st.IsDir() {
			f := Sys.FileNewForPath(icon)
			p := Call(fnIconPaintableNewForFile, f, uintptr(size), uintptr(scale))
			ObjectUnref(f)
			if p != 0 {
				return p, icon
			}
		}
		icon = ""
	}
	theme := IconThemeGetForDisplay(DisplayGetDefault())
	name := "application-x-executable"
	for _, n := range []string{icon, strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(icon, ".png"), ".svg"), ".xpm")} {
		if n != "" && IconThemeHasIcon(theme, n) {
			name = n
			break
		}
	}
	p := IconThemeLookupIcon(theme, name, nil, size, scale, int32(TextDirLTR), 0)
	if p == 0 {
		return 0, ""
	}
	file := ""
	if f := Call(fnIconPaintableGetFile, p); f != 0 {
		file = GoStringFree(Sys.FileGetPath(f))
		ObjectUnref(f)
	}
	return p, file
}

// appsRenderer is a Cairo renderer of the display's, drawing icons
// without a window.
var appsRenderer Ptr

// paintableImage draws a paintable px pixels square into an image (its
// pixels premultiplied, as Go's RGBA's are); nil if it can't.
func paintableImage(p Ptr, px int) *image.RGBA {
	if appsRenderer == 0 {
		r := Call(fnCairoRendererNew)
		var gerr Ptr
		ok := false
		if fnRendererRealizeForDisplay != 0 {
			ok = int32(Call(fnRendererRealizeForDisplay, r, DisplayGetDefault(), Addr(&gerr))) != 0
		} else {
			ok = int32(Call(fnRendererRealize, r, 0, Addr(&gerr))) != 0
		}
		if !ok {
			if gerr != 0 {
				ErrorFree(gerr)
			}
			ObjectUnref(r)
			return nil
		}
		appsRenderer = r
	}
	s := SnapshotNew()
	PaintableSnapshot(p, s, float64(px), float64(px))
	node := SnapshotFreeToNode(s)
	if node == 0 {
		return nil
	}
	vp := Rect{W: float32(px), H: float32(px)}
	t := RendererRenderTexture(appsRenderer, node, &vp)
	RenderNodeUnref(node)
	if t == 0 {
		return nil
	}
	defer ObjectUnref(t)
	w, h := int(TextureGetWidth(t)), int(TextureGetHeight(t))
	if w <= 0 || h <= 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	Draw.TextureDownload(t, &img.Pix[0], uintptr(img.Stride)) // Cairo's ARGB32: B, G, R, A in memory
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
	}
	return img
}

// openApp launches the app of a desktop entry (GIO's
// g_app_info_launch_uris_async, D-Bus activation for an app that has
// it), with the display's launch context: the app gets its startup
// notification ID or activation token, and takes the focus.
func (h *Host) openApp(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Stat(path); err != nil {
		reply(nil, fmt.Errorf("no app at %s", path))
		return
	}
	bindApps()
	cpath := CString(path)
	info := Call(fnDesktopAppInfoNew, Addr(cpath))
	runtime.KeepAlive(cpath)
	if info == 0 {
		reply(nil, fmt.Errorf("%s isn't a desktop entry of an app", path))
		return
	}
	ctx := Call(fnLaunchContext, DisplayGetDefault())
	Async(func(a AsyncCall) { Call(fnLaunchURIsAsync, info, 0, ctx, 0, a.Callback, a.Data) }, func(_, res Ptr) {
		var gerr Ptr
		ok := int32(Call(fnLaunchURIsFinish, info, res, Addr(&gerr))) != 0
		ObjectUnref(info)
		if ctx != 0 {
			ObjectUnref(ctx)
		}
		if !ok {
			reply(nil, GError(gerr))
			return
		}
		reply(nil, nil)
	})
}

// appIconFile is where an app's icon px pixels square is kept: a file
// of the app's caches named for its path.
func appIconFile(path string, px int) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(appdir.Cache(), "asuka-apps", fmt.Sprintf("%x-%d.png", sum[:10], px))
}

// appIconFresh reports whether the icon at png was drawn after each of
// sources (the files it's drawn from; "" none) last changed.
func appIconFresh(png string, sources ...string) bool {
	st, err := os.Stat(png)
	if err != nil {
		return false
	}
	for _, s := range sources {
		if s == "" {
			continue
		}
		if sst, err := os.Stat(s); err == nil && !sst.ModTime().Before(st.ModTime()) {
			return false
		}
	}
	return true
}

// writeAppIcon writes an icon's PNG to path through a file beside it,
// renamed over it: a reader never sees half a file.
func writeAppIcon(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".icon-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

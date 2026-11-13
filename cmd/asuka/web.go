package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image/png"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	webhost "github.com/egoist/asuka/platform/web"
)

// The web: the app's Go code compiled to WebAssembly (GOOS=js
// GOARCH=wasm) and the web host (platform/web: the DOM, in JavaScript),
// which the page loads together.
//
// asuka dev --platform web serves the app on this machine and opens it in the
// browser; each change rebuilds it and reloads the page, the app's
// signals' state kept (the page keeps it in its session storage across
// the reload). The app's output and the page's errors are printed here.
// asuka build --platform web makes a static site: its files named by their content's
// digest (cache them for good; index.html no-cache), gzipped copies,
// a web app manifest and its icons, and the app's service worker
// (webWorker), which shows its notifications and pushes while no page is
// open.

const devWebPort = 8790

// webLdflags is the -ldflags of a web build: asuka.json's, the app's name
// (the page's title) and version.
func webLdflags(app *goApp, release bool) string {
	f := strings.TrimPrefix(app.cfg.ldflags(release), "-ldflags=")
	f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Name="+app.displayName())
	if app.cfg != nil {
		f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Version="+app.cfg.Version)
		f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Build="+strconv.Itoa(app.cfg.Build))
	}
	return "-ldflags=" + strings.TrimSpace(f)
}

// buildWasm builds the app's Go code for the browser.
func buildWasm(app *goApp, out string, release, verbose bool) error {
	args := []string{"build", webLdflags(app, release), "-o", out}
	if release {
		args = append(args, "-trimpath")
	}
	args = append(args, ".")
	if verbose {
		fmt.Println("+ GOOS=js GOARCH=wasm go", strings.Join(args, " "))
	}
	c := exec.Command("go", args...)
	c.Dir = app.dir
	c.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed:\n%s", bytes.TrimSpace(b))
	}
	return nil
}

// webSource is the web host's files: the checkout's, when the app is in
// Asuka's own module (so changing them reloads the page too), else the
// ones built into the CLI.
func webSource(app *goApp) (fs.FS, string) {
	dir := filepath.Join(app.moduleDir, "platform", "web", "src")
	if _, err := os.Stat(filepath.Join(dir, "host.js")); err == nil {
		if b, err := os.ReadFile(filepath.Join(app.moduleDir, "go.mod")); err == nil && bytes.Contains(b, []byte("module github.com/egoist/asuka\n")) {
			return os.DirFS(dir), dir
		}
	}
	sub, _ := fs.Sub(webhost.Src, "src")
	return sub, ""
}

// webPluginFiles are the web's files of the plugins app uses (their
// libraries' wasm files, plugins.go), by their paths in the host's
// directory: plugins/<plugin>/<file>.
func webPluginFiles(app *goApp) (map[string]pluginLib, error) {
	libs, err := appLibs(app.dir, "js", "wasm", "wasm")
	if err != nil {
		return nil, err
	}
	files := map[string]pluginLib{}
	for _, l := range libs {
		files["plugins/"+l.m.Name+"/"+l.file.Name] = l
	}
	return files, nil
}

// webPluginOptions sets the page's options for the plugins the host
// itself drives, their files under base (the host's directory, with a
// slash): the sqlite plugin's module, which the host's SQLite worker runs
// (platform/web/src/sqlite.js).
func webPluginOptions(files map[string]pluginLib, base string, opts map[string]any) {
	for _, l := range files {
		if l.m.Name == "sqlite" {
			opts["sqlite"] = base + "plugins/sqlite/"
		}
	}
}

// wasmExec is the Go release's JavaScript side of its WebAssembly port,
// which must be the one the app was built with.
func wasmExec() ([]byte, error) {
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return nil, err
	}
	r := strings.TrimSpace(string(root))
	for _, p := range []string{filepath.Join(r, "lib", "wasm", "wasm_exec.js"), filepath.Join(r, "misc", "wasm", "wasm_exec.js")} {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("no wasm_exec.js in this Go installation")
}

// fontFiles are the app's font files (assets/fonts), which a FontFamily
// names by their names without the extension.
func fontFiles(app *goApp) []string {
	var out []string
	es, _ := os.ReadDir(filepath.Join(app.dir, "assets", "fonts"))
	for _, e := range es {
		switch strings.ToLower(path.Ext(e.Name())) {
		case ".ttf", ".otf", ".woff", ".woff2":
			out = append(out, e.Name())
		}
	}
	return out
}

type webPage struct {
	Name, Lang       string
	Host, Wasm, Exec string // the URLs of the host's directory, the app's wasm, wasm_exec.js
	Options          map[string]any
	Icon, Manifest   bool
	// Scripts are the app's own (web/*.js), loaded before it starts: they
	// register its native views and modules (asuka.registerNative,
	// asuka.registerModule), as its ios/*.swift and android/*.kt do.
	Scripts []string
}

// webIndex is the page that runs the app.
func webIndex(p webPage) string {
	opts := map[string]any{"wasm": p.Wasm, "exec": p.Exec, "assets": "assets"}
	for k, v := range p.Options {
		opts[k] = v
	}
	o, _ := json.Marshal(opts)
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html>
<html lang="%s">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content">
<meta name="color-scheme" content="light dark">
<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#121212" media="(prefers-color-scheme: dark)">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-title" content="%[2]s">
<title>%[2]s</title>
`, html.EscapeString(p.Lang), html.EscapeString(p.Name))
	if p.Manifest {
		b.WriteString(`<link rel="manifest" href="manifest.webmanifest">` + "\n")
	}
	if p.Icon {
		b.WriteString(`<link rel="icon" type="image/png" sizes="32x32" href="icons/favicon.png">
<link rel="apple-touch-icon" href="icons/apple-touch-icon.png">
`)
	}
	fmt.Fprintf(&b, `<link rel="stylesheet" href="%[1]s/style.css">
<link rel="modulepreload" href="%[1]s/asuka.js">
<link rel="preload" href="%[2]s" as="fetch" type="application/wasm" crossorigin>
</head>
<body>
<script type="module">
import * as asuka from "./%[1]s/asuka.js";
globalThis.asuka = asuka;
%[4]sasuka.start(%[3]s);
</script>
</body>
</html>
`, p.Host, p.Wasm, o, imports(p.Scripts))
	return b.String()
}

// imports are the page's imports of the app's scripts.
func imports(scripts []string) string {
	var b strings.Builder
	for _, s := range scripts {
		q, _ := json.Marshal("./" + s)
		fmt.Fprintf(&b, "await import(%s);\n", q)
	}
	return b.String()
}

// appScripts are the app's own JavaScript (web/*.js), by name.
func appScripts(app *goApp) []string {
	var out []string
	es, _ := os.ReadDir(filepath.Join(app.dir, "web"))
	for _, e := range es {
		if !e.IsDir() && path.Ext(e.Name()) == ".js" {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// webWorker is the app's service worker: its settings (globalThis.asukaApp:
// name, title, assets, icon), then the host's notify-core.js and sw.js,
// as one classic script (platform/web/src/sw.js says what it does).
func webWorker(src fs.FS, settings map[string]any) ([]byte, error) {
	var b bytes.Buffer
	st, _ := json.Marshal(settings)
	b.WriteString("// The app's service worker: Asuka's (platform/web/src/sw.js), with the app's settings.\n")
	fmt.Fprintf(&b, "globalThis.asukaApp = %s;\n", st)
	for _, n := range []string{"notify-core.js", "sw.js"} {
		f, err := fs.ReadFile(src, n)
		if err != nil {
			return nil, fmt.Errorf("the web host's %s: %w", n, err)
		}
		b.Write(f)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// webVAPIDKey is asuka.json's web.vapidPublicKey, checked: an uncompressed
// P-256 point in base64url (unpadded), as PushManager.subscribe takes it.
func webVAPIDKey(app *goApp) (string, error) {
	if app.cfg == nil || strings.TrimSpace(app.cfg.Web.VapidPublicKey) == "" {
		return "", nil
	}
	k := strings.TrimRight(strings.TrimSpace(app.cfg.Web.VapidPublicKey), "=")
	k = strings.NewReplacer("+", "-", "/", "_").Replace(k)
	b, err := base64.RawURLEncoding.DecodeString(k)
	if err == nil {
		_, err = ecdh.P256().NewPublicKey(b)
	}
	if err != nil || len(b) != 65 {
		return "", errors.New("asuka.json: web.vapidPublicKey isn't a VAPID public key (an uncompressed P-256 point, 65 bytes, in base64url): github.com/egoist/asuka/sdk/push/webpush's GenerateKeys makes a pair")
	}
	return k, nil
}

func devWeb(dir string, o *devOptions, stop <-chan os.Signal) error {
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	work, err := workDir(app, "web")
	if err != nil {
		return err
	}
	exec_, err := wasmExec()
	if err != nil {
		return err
	}
	vapid, err := webVAPIDKey(app)
	if err != nil {
		return err
	}
	src, srcDir := webSource(app)
	s := &devServer{app: app, src: src, exec: exec_, wasm: filepath.Join(work, "app.wasm"), vapid: vapid, clients: map[chan string]bool{}}
	start := time.Now()
	if err := buildWasm(app, s.wasm, false, o.verbose); err != nil {
		return err
	}
	if err := s.findPlugins(); err != nil {
		return err
	}
	s.stamp = strconv.FormatInt(time.Now().UnixNano(), 36)
	host := "127.0.0.1"
	if o.lan {
		host = ""
	}
	port := o.port
	if port == 0 {
		port = devWebPort
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil && o.port == 0 {
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0")) // taken: any free one
	}
	if err != nil {
		return err
	}
	go http.Serve(ln, s)
	url := fmt.Sprintf("http://localhost:%d/", ln.Addr().(*net.TCPAddr).Port)
	fmt.Printf("asuka: %s at %s (built in %.1fs)\n", app.name, url, time.Since(start).Seconds())
	if o.lan {
		for _, a := range lanAddrs() {
			fmt.Printf("asuka: on this network: http://%s:%d/\n", a, ln.Addr().(*net.TCPAddr).Port)
		}
	}
	if !o.noOpen {
		openBrowser(url)
	}
	if o.once {
		<-stop
		return nil
	}
	fmt.Println("asuka: watching for changes (Ctrl-C to stop)")
	roots := []string{app.moduleDir, app.dir}
	w := newWatcher(roots...)
	hostWatch := newWebWatcher(srcDir)
	appWatch := newWebWatcher(filepath.Join(app.dir, "web"))
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			if hostWatch.changed() || appWatch.changed() {
				// The host's own JavaScript (a checkout of Asuka): the page loads it again.
				s.stamp = strconv.FormatInt(time.Now().UnixNano(), 36)
				s.send("reload", "")
			}
			if !w.changed() {
				continue
			}
			t := time.Now()
			if err := buildWasm(app, s.wasm, false, o.verbose); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
				s.send("failed", err.Error())
				s.failed = true
				continue
			}
			if s.failed {
				s.failed = false
				s.send("fixed", "")
			}
			if err := s.findPlugins(); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
			}
			s.stamp = strconv.FormatInt(time.Now().UnixNano(), 36)
			s.send("reload", "")
			fmt.Printf("asuka: rebuilt in %.1fs\n", time.Since(t).Seconds())
		}
	}
}

// devServer serves the app's page under asuka dev: the page, the host's
// files, the build, the app's assets, and its connection back (the
// reloads it waits for, the logs it sends).
type devServer struct {
	app     *goApp
	src     fs.FS
	plugins atomic.Pointer[map[string]pluginLib] // webPluginFiles, as of the last build
	exec    []byte
	wasm    string
	vapid   string // asuka.json's web.vapidPublicKey
	stamp   string // the build's, so the page never loads an old one
	failed  bool
	mu      sync.Mutex
	clients map[chan string]bool
	nextID  int64
	waiting map[int64]chan struct {
		ID         int64
		Out, Error string
	}
}

// automate sends the page the debug automation's commands (the request's
// body, a line each: tap, type, submit, back, tab, dump, reload), as the
// GUI tests send them to the mobile hosts, and answers with what the page
// does: dump's lines, or the page's error (500). Bounded: a page that
// doesn't answer fails the request.
func (s *devServer) automate(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c := make(chan struct {
		ID         int64
		Out, Error string
	}, 1)
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	if s.waiting == nil {
		s.waiting = map[int64]chan struct {
			ID         int64
			Out, Error string
		}{}
	}
	s.waiting[id] = c
	s.mu.Unlock()
	msg, _ := json.Marshal(map[string]any{"id": id, "commands": string(b)})
	s.send("automation", string(msg))
	select {
	case d := <-c:
		if d.Error != "" {
			http.Error(w, d.Error, http.StatusInternalServerError)
			return
		}
		io.WriteString(w, d.Out)
	case <-time.After(10 * time.Second):
		s.mu.Lock()
		delete(s.waiting, id)
		s.mu.Unlock()
		http.Error(w, "the page didn't answer", http.StatusGatewayTimeout)
	}
}

func (s *devServer) send(event, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := "event: " + event + "\n"
	for _, line := range strings.Split(data, "\n") {
		msg += "data: " + line + "\n"
	}
	msg += "\n"
	for c := range s.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

func (s *devServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case p == "/" || p == "/index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		opts := map[string]any{"name": s.app.cfg.ID, "title": s.app.displayName(), "dev": "/__asuka/", "fonts": fontFiles(s.app), "version": s.app.cfg.Version, "build": s.app.cfg.Build, "serviceWorker": "sw.js"}
		if s.vapid != "" {
			opts["vapidPublicKey"] = s.vapid
		}
		webPluginOptions(*s.plugins.Load(), "asuka/", opts)
		io.WriteString(w, webIndex(webPage{
			Name: s.app.displayName(), Lang: devLangOr("en"),
			Host: "asuka", Wasm: "app.wasm?" + s.stamp, Exec: "asuka/wasm_exec.js", Scripts: prefixed("web/", appScripts(s.app)),
			Options: opts,
		}))
	case p == "/app.wasm":
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, s.wasm)
	case p == "/sw.js":
		b, err := webWorker(s.src, map[string]any{"name": s.app.cfg.ID, "title": s.app.displayName(), "assets": "assets"})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType(p))
		w.Write(b)
	case p == "/asuka/wasm_exec.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(s.exec)
	case strings.HasPrefix(p, "/asuka/plugins/"):
		l, ok := (*s.plugins.Load())[strings.TrimPrefix(p, "/asuka/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := l.read()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType(p))
		w.Write(b)
	case strings.HasPrefix(p, "/asuka/"):
		b, err := fs.ReadFile(s.src, strings.TrimPrefix(p, "/asuka/"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType(p))
		w.Write(b)
	case strings.HasPrefix(p, "/web/") && path.Ext(p) == ".js":
		w.Header().Set("Content-Type", contentType(p))
		http.ServeFile(w, r, filepath.Join(s.app.dir, "web", path.Base(p)))
	case strings.HasPrefix(p, "/assets/"):
		http.ServeFile(w, r, filepath.Join(s.app.dir, filepath.FromSlash(strings.TrimPrefix(path.Clean(p), "/"))))
	case p == "/__asuka/events":
		s.events(w, r)
	case p == "/__asuka/automation" && r.Method == http.MethodPost:
		s.automate(w, r)
	case p == "/__asuka/automation/done" && r.Method == http.MethodPost:
		var d struct {
			ID         int64
			Out, Error string
		}
		if json.NewDecoder(r.Body).Decode(&d) == nil {
			s.mu.Lock()
			c := s.waiting[d.ID]
			delete(s.waiting, d.ID)
			s.mu.Unlock()
			if c != nil {
				c <- d
			}
		}
	case p == "/__asuka/log" && r.Method == http.MethodPost:
		b, _ := io.ReadAll(r.Body)
		printLog(b)
	default:
		http.NotFound(w, r)
	}
}

// events is the page's connection for reloads (server-sent events).
func (s *devServer) events(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	c := make(chan string, 8)
	s.mu.Lock()
	s.clients[c] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
	}()
	io.WriteString(w, ": asuka\n\n")
	f.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-c:
			io.WriteString(w, m)
			f.Flush()
		}
	}
}

// printLog prints what the page sent: runs of the app's output (marked
// \x01) and its errors (\x02).
func printLog(b []byte) {
	for len(b) > 0 {
		out := os.Stdout
		if b[0] == 2 {
			out = os.Stderr
		}
		b = b[1:]
		i := bytes.IndexAny(b, "\x01\x02")
		if i < 0 {
			i = len(b)
		}
		out.Write(b[:i])
		b = b[i:]
	}
}

func contentType(p string) string {
	switch path.Ext(p) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	case ".json", ".webmanifest":
		return "application/json"
	}
	return "application/octet-stream"
}

// webWatcher notices changes to JavaScript the page loads as it is, with
// no build: the host's own (a checkout of Asuka: src/ and its modules/),
// and the app's (web/*.js).
type webWatcher struct {
	dir, last string
}

func newWebWatcher(dir string) *webWatcher {
	w := &webWatcher{dir: dir}
	w.last = w.scan()
	return w
}

func (w *webWatcher) scan() string {
	if w.dir == "" {
		return ""
	}
	var b strings.Builder
	filepath.WalkDir(w.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			fmt.Fprintf(&b, "%s%d;", p, info.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
}

func (w *webWatcher) changed() bool {
	s := w.scan()
	if s == w.last {
		return false
	}
	w.last = s
	return true
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	c.Start()
}

func lanAddrs() []string {
	var out []string
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			out = append(out, n.IP.String())
		}
	}
	return out
}

func prefixed(prefix string, names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = prefix + n
	}
	return out
}

func devLangOr(def string) string {
	if devLang != "" {
		return devLang
	}
	return def
}

func buildWeb(app *goApp, o *buildOptions) error {
	out := filepath.Join(o.out, "web")
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(out, ".app.wasm")
	if err := buildWasm(app, tmp, true, o.verbose); err != nil {
		return err
	}
	defer os.Remove(tmp)
	wasm, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	exec_, err := wasmExec()
	if err != nil {
		return err
	}
	vapid, err := webVAPIDKey(app)
	if err != nil {
		return err
	}
	src, _ := webSource(app)
	// The host's files go in a directory named by their digest: they
	// import each other by name.
	files := map[string][]byte{"wasm_exec.js": exec_}
	fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files[p], _ = fs.ReadFile(src, p)
		}
		return nil
	})
	// The plugins' files.
	plugins, err := webPluginFiles(app)
	if err != nil {
		return err
	}
	for p, l := range plugins {
		if files[p], err = l.read(); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write(files[n])
	}
	hostDir := "asuka/" + hex.EncodeToString(h.Sum(nil))[:12]
	for _, n := range names {
		if err := writeWeb(filepath.Join(out, filepath.FromSlash(hostDir), n), files[n]); err != nil {
			return err
		}
	}
	sum := sha256.Sum256(wasm)
	wasmName := "app." + hex.EncodeToString(sum[:])[:12] + ".wasm"
	if err := writeWeb(filepath.Join(out, wasmName), wasm); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(out, "assets")); err != nil {
		return err
	}
	// The app's scripts, in a directory named by their digest too.
	var scripts []string
	if names := appScripts(app); len(names) > 0 {
		h := sha256.New()
		data := map[string][]byte{}
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(app.dir, "web", n))
			if err != nil {
				return err
			}
			data[n] = b
			h.Write([]byte(n))
			h.Write(b)
		}
		dir := "web/" + hex.EncodeToString(h.Sum(nil))[:12]
		for _, n := range names {
			if err := writeWeb(filepath.Join(out, filepath.FromSlash(dir), n), data[n]); err != nil {
				return err
			}
			scripts = append(scripts, dir+"/"+n)
		}
	}
	name := app.displayName()
	icon := app.cfg.Icon != ""
	if icon {
		if err := webIcons(app.cfg.Icon, filepath.Join(out, "icons")); err != nil {
			return err
		}
	}
	manifest := map[string]any{
		"name": name, "short_name": name, "id": "./", "start_url": "./", "scope": "./",
		"display": "standalone", "background_color": "#ffffff", "theme_color": "#ffffff",
	}
	if app.cfg.Accent != "" {
		manifest["theme_color"] = app.cfg.Accent
	}
	if icon {
		manifest["icons"] = []map[string]string{
			{"src": "icons/icon-192.png", "sizes": "192x192", "type": "image/png"},
			{"src": "icons/icon-512.png", "sizes": "512x512", "type": "image/png"},
			{"src": "icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
		}
	}
	// asuka.json's fileTypes: an installed app opens them (Chromium's
	// File Handling), the host reads them from the launch queue.
	if fh := webFileHandlers(app.cfg.fileTypes()); fh != nil {
		manifest["file_handlers"] = fh
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "manifest.webmanifest"), mb, 0o644); err != nil {
		return err
	}
	// The app's service worker, beside index.html (its scope is the app's
	// directory), named by its digest too.
	settings := map[string]any{"name": app.cfg.ID, "title": name, "assets": "assets"}
	if icon {
		settings["icon"] = "icons/icon-192.png"
	}
	sw, err := webWorker(src, settings)
	if err != nil {
		return err
	}
	swSum := sha256.Sum256(sw)
	swName := "sw." + hex.EncodeToString(swSum[:])[:12] + ".js"
	if err := writeWeb(filepath.Join(out, swName), sw); err != nil {
		return err
	}
	opts := map[string]any{"name": app.cfg.ID, "title": name, "fonts": fontFiles(app), "version": app.cfg.Version, "build": app.cfg.Build, "serviceWorker": swName}
	if icon {
		opts["icon"] = "icons/icon-192.png"
	}
	if vapid != "" {
		opts["vapidPublicKey"] = vapid
	}
	webPluginOptions(plugins, hostDir+"/", opts)
	page := webIndex(webPage{
		Name: name, Lang: "en", Host: hostDir, Wasm: wasmName, Exec: hostDir + "/wasm_exec.js",
		Options:  opts,
		Icon:     icon,
		Manifest: true,
		Scripts:  scripts,
	})
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	// What asuka/sdk/update reads for a newer version than the page's.
	if err := os.WriteFile(filepath.Join(out, "version.json"), versionFile(app), 0o644); err != nil {
		return err
	}
	headers := "/index.html\n  Cache-Control: no-cache\n/manifest.webmanifest\n  Cache-Control: no-cache\n/version.json\n  Cache-Control: no-cache\n/asuka/*\n  Cache-Control: public, max-age=31536000, immutable\n/web/*\n  Cache-Control: public, max-age=31536000, immutable\n/sw.*.js\n  Cache-Control: public, max-age=31536000, immutable\n/*.wasm\n  Cache-Control: public, max-age=31536000, immutable\n  Content-Type: application/wasm\n"
	if err := os.WriteFile(filepath.Join(out, "_headers"), []byte(headers), 0o644); err != nil {
		return err
	}
	fmt.Printf("asuka: %s (%s of WebAssembly, %s gzipped)\n", out, size(len(wasm)), size(gzipSize(wasm)))
	fmt.Println("asuka: a static site: serve the folder from any web server or CDN, index.html and manifest.webmanifest with no-cache, the rest for good (_headers says so for Cloudflare and Netlify)")
	return nil
}

// writeWeb writes a file of the site, and a gzipped copy of what
// compresses (servers that serve precompressed files send it).
func writeWeb(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return err
	}
	switch path.Ext(p) {
	case ".js", ".mjs", ".css", ".wasm", ".json":
		var z bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&z, gzip.BestCompression)
		zw.Write(b)
		zw.Close()
		return os.WriteFile(p+".gz", z.Bytes(), 0o644)
	}
	return nil
}

func gzipSize(b []byte) int {
	var z bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&z, gzip.BestCompression)
	zw.Write(b)
	zw.Close()
	return z.Len()
}

func size(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}

// webIcons makes the page's icons from the app's square icon: the
// manifest's (192 and 512), iOS's home screen icon (180) and the favicon.
func webIcons(src, dir string) error {
	img, err := loadIcon(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, px := range map[string]int{"icon-192.png": 192, "icon-512.png": 512, "apple-touch-icon.png": 180, "favicon.png": 32} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		err = png.Encode(w, resize(img, px))
		w.Flush()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// findPlugins finds the plugins the app uses again (its imports may have
// changed), and their files for the pages.
func (s *devServer) findPlugins() error {
	forgetPlugins(s.app.dir)
	files, err := webPluginFiles(s.app)
	if err != nil {
		return err
	}
	s.plugins.Store(&files)
	return nil
}

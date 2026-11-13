//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"github.com/egoist/asuka/plugin"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSQLitePackageWeb runs asuka/sqlite's own tests (go test -c, js/wasm)
// in headless Chrome, on a page with the web host's file system and
// SQLite worker, twice: databases under /tmp (the worker's memory), and
// under the data directory (the origin private file system); then its
// benchmarks (a 1000-insert transaction, a 10,000-row query, a one-row
// query), on the origin private file system, logged.
func TestSQLitePackageWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	dir := t.TempDir()
	wasm := filepath.Join(dir, "sqlite.test.wasm")
	build := exec.Command("go", "test", "-c", "-o", wasm, "./plugins/sqlite")
	build.Dir = root(t)
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the tests: %v\n%s", err, b)
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	srv := hostServer(t, sqliteTestPage, func(mux *http.ServeMux) {
		mux.HandleFunc("/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"))
		})
		mux.HandleFunc("/test.wasm", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, wasm) })
	})

	c := launchCDP(t, browser)
	for _, run := range []struct{ name, tmp, args string }{
		{"memory", "/tmp", "-test.v"},
		{"opfs", "/data/test/tmp", "-test.v"},
		{"bench", "/data/test/tmp", "-test.run=^$ -test.bench=. -test.benchmem"},
	} {
		t.Run(run.name, func(t *testing.T) {
			var created struct{ TargetID string }
			u := srv.URL + "/?" + url.Values{"tmp": {run.tmp}, "args": {run.args}}.Encode()
			if err := c.call("", "Target.createTarget", map[string]any{"url": u}, &created, ""); err != nil {
				t.Fatal(err)
			}
			page := c.attach(t, created.TargetID)
			state := func() string { return c.eval(t, page, `JSON.stringify(window.result ?? null)`) }
			until(t, func() bool { return state() != "null" }, func() string { return c.eval(t, page, `window.out ?? ""`) })
			var r struct {
				Code *int
				Out  string
			}
			if err := json.Unmarshal([]byte(state()), &r); err != nil {
				t.Fatal(err)
			}
			if r.Code == nil || *r.Code != 0 {
				t.Fatalf("the tests failed:\n%s", r.Out)
			}
			if run.name == "bench" {
				for _, l := range strings.Split(r.Out, "\n") {
					if strings.HasPrefix(l, "Benchmark") {
						t.Log(l)
					}
				}
				return
			}
			t.Logf("%d tests passed", strings.Count(r.Out, "--- PASS"))
		})
	}
}

// hostServer serves page at /, the web host's files at /asuka/, SQLite's
// module at /sqlite/, and what more adds.
func hostServer(t *testing.T, page string, more func(*http.ServeMux)) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	mux.Handle("/asuka/", http.StripPrefix("/asuka/", http.FileServer(http.Dir(filepath.Join(root(t), "platform", "web", "src")))))
	// The sqlite plugin's: the checkout's build of it, or the published
	// one (package plugin).
	dir := filepath.Join(root(t), "plugins", "sqlite")
	data, err := os.ReadFile(filepath.Join(dir, plugin.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := m.Library("asqlite")
	wasm, err := plugin.FilePath(dir, l, "wasm")
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/sqlite/", http.StripPrefix("/sqlite/", http.FileServer(http.Dir(filepath.Dir(wasm)))))
	if more != nil {
		more(mux)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const sqliteTestPage = `<!doctype html>
<meta charset="utf-8">
<script type="module">
import * as fs from "/asuka/fs.js";
import { load } from "/asuka/sqlite.js";
const params = new URL(location.href).searchParams;
const tmp = params.get("tmp");
window.out = "";
await fs.mount({ name: "gotest", dir: "/data/test", output: (fd, s) => { window.out += s; } });
fs.makeDir(tmp);
await new Promise((resolve, reject) => {
  const s = document.createElement("script");
  s.src = "/wasm_exec.js";
  s.onload = resolve;
  s.onerror = reject;
  document.head.appendChild(s);
});
const client = await load(new URL("/sqlite/", location.href));
const go = new Go();
let instance;
globalThis.asukaHost = { sqlite: client.api(() => instance.exports.mem, "/data/test") };
go.argv = ["sqlite.test", ...params.get("args").split(" "), "-test.timeout=5m"];
go.env = { TMPDIR: tmp, HOME: "/home", ASUKA_DATA: "/data/test", TZ: "UTC" };
let code = null;
go.exit = (c) => { code = c; };
instance = (await WebAssembly.instantiateStreaming(fetch("/test.wasm"), go.importObject)).instance;
await go.run(instance);
window.result = { code, out: window.out };
</script>
`

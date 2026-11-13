//go:build e2e

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/asuka/sdk/sqlite"
)

// TestSQLiteWeb runs e2e/testdata/websqlite in headless Chrome, through
// its window.sqliteTest:
//
//   - a database the page's files kept (an earlier version's, in
//     IndexedDB) moves into the origin private file system as it's first
//     opened, and leaves the page's files;
//   - what's written is there after a reload;
//   - two tabs writing one database at once, row by row and in
//     transactions, lose nothing;
//   - a call that would wait, on the page's main thread, fails at once;
//     ExecAsync from it works;
//   - how long a 1000-insert transaction and a 10,000-row query take.
func TestSQLiteWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	const app = "dev.asuka.websqlitetest"
	const dataDir = "/data/" + app
	pageURL := webServer(t, "e2e/testdata/websqlite")
	c := launchCDP(t, browser)

	load := func(page string) {
		t.Helper()
		var res json.RawMessage
		if err := c.call(page, "Page.navigate", map[string]any{"url": pageURL}, &res, ""); err != nil {
			t.Fatal(err)
		}
		state := func() string {
			return c.eval(t, page, `typeof sqliteTest + " " + (document.querySelector(".asuka-error")?.textContent || "")`)
		}
		until(t, func() bool { return strings.HasPrefix(state(), "function") }, state)
	}
	tab := func() string {
		t.Helper()
		var created struct{ TargetID string }
		if err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"}, &created, ""); err != nil {
			t.Fatal(err)
		}
		page := c.attach(t, created.TargetID)
		var res json.RawMessage
		if err := c.call(page, "Page.enable", map[string]any{}, &res, ""); err != nil {
			t.Fatal(err)
		}
		load(page)
		return page
	}
	call := func(page string, args ...string) string {
		t.Helper()
		b, _ := json.Marshal(args)
		return c.eval(t, page, "sqliteTest(..."+string(b)+")")
	}
	must := func(page string, args ...string) string {
		t.Helper()
		out := call(page, args...)
		if strings.HasPrefix(out, "error: ") {
			t.Fatalf("%q: %s", args, out)
		}
		return out
	}

	one := tab()

	// An earlier version's database, in the page's files (fs.js's
	// IndexedDB), in write-ahead logging as a native one is.
	legacy := filepath.Join(t.TempDir(), "legacy.db")
	ldb, err := sqlite.Open(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ldb.Exec(`create table old (x text); insert into old values ('from fs.js')`); err != nil {
		t.Fatal(err)
	}
	ldb.Close()
	raw, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	c.eval(t, one, fmt.Sprintf(`new Promise((resolve, reject) => {
		const bytes = Uint8Array.from(atob(%q), (c) => c.charCodeAt(0));
		const r = indexedDB.open("asuka-files:%s", 2);
		r.onerror = () => reject(r.error);
		r.onsuccess = () => {
			const tx = r.result.transaction(["files"], "readwrite");
			tx.objectStore("files").put({ dir: false, mode: 0o100644, mtime: Date.now(), data: bytes }, %q);
			tx.oncomplete = () => resolve("ok");
			tx.onerror = () => reject(tx.error);
		};
	})`, base64.StdEncoding.EncodeToString(raw), app, dataDir+"/legacy.db"))
	load(one) // the page's files load it
	if got := must(one, "query", "legacy.db", "select x from old"); got != `["from fs.js"]` {
		t.Fatalf("the moved database: %s", got)
	}
	inIDB := func() string {
		return c.eval(t, one, fmt.Sprintf(`new Promise((resolve) => {
			const r = indexedDB.open("asuka-files:%s", 2);
			r.onsuccess = () => {
				const g = r.result.transaction(["files"]).objectStore("files").get(%q);
				g.onsuccess = () => resolve(g.result ? "there" : "gone");
			};
		})`, app, dataDir+"/legacy.db"))
	}
	until(t, func() bool { return inIDB() == "gone" }, inIDB)
	opfsSize := c.eval(t, one, fmt.Sprintf(`(async () => {
		let d = await navigator.storage.getDirectory();
		for (const p of %q.split("/").filter(Boolean)) d = await d.getDirectoryHandle(p);
		return (await (await d.getFileHandle("legacy.db")).getFile()).size;
	})()`, dataDir))
	if opfsSize == "0" {
		t.Fatalf("legacy.db in the origin private file system: %s bytes", opfsSize)
	}
	load(one)
	if got := must(one, "query", "legacy.db", "select x from old"); got != `["from fs.js"]` {
		t.Fatalf("the moved database, after a reload: %s", got)
	}

	// Kept across a reload; the live query counts it.
	must(one, "insert", "reload", "5")
	load(one)
	if got := must(one, "query", "e2e.db", "select count(*) from t where tab = 'reload'"); got != `["5"]` {
		t.Fatalf("after a reload: %s", got)
	}
	shows := func() string { return c.eval(t, one, `document.body.innerText`) }
	until(t, func() bool { return strings.Contains(shows(), "rows: 5") }, shows)

	// The main thread: an error at once; ExecAsync works.
	if got := call(one, "mainthread"); !strings.Contains(got, "main thread") {
		t.Fatalf("a call on the main thread: %s", got)
	}
	must(one, "async")

	// Two tabs at once.
	two := tab()
	var wg sync.WaitGroup
	outs := make([]string, 4)
	for i, args := range [][]string{{one, "insert", "a", "150"}, {two, "insert", "b", "150"}, {one, "tx", "c", "500"}, {two, "tx", "d", "500"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := json.Marshal(args[1:])
			var r struct {
				Result struct{ Value any }
			}
			if err := c.call(args[0], "Runtime.evaluate", map[string]any{"expression": "sqliteTest(..." + string(b) + ")", "awaitPromise": true, "returnByValue": true}, &r, ""); err != nil {
				outs[i] = "error: " + err.Error()
				return
			}
			outs[i] = fmt.Sprint(r.Result.Value)
		}()
	}
	wg.Wait()
	for i, o := range outs {
		if o != "ok" {
			t.Fatalf("writer %d: %s", i, o)
		}
	}
	for _, page := range []string{one, two} {
		if got := must(page, "query", "e2e.db", "select tab || ' ' || count(*) from t where tab in ('a', 'b', 'c', 'd') group by tab order by tab"); got != `["a 150","b 150","c 500","d 500"]` {
			t.Fatalf("after both tabs wrote: %s", got)
		}
		if got := must(page, "query", "e2e.db", "pragma integrity_check"); got != `["ok"]` {
			t.Fatalf("integrity: %s", got)
		}
	}

	t.Logf("speed: %s", must(one, "bench"))
}

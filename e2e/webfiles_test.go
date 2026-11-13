//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"
)

// TestFilesTabsWeb opens two tabs of one app's file system (the web
// host's fs.js, kept in IndexedDB) in headless Chrome: a file one writes
// reaches the other, which hears of it through its watch; a file a tab
// changed and hasn't written yet stays its own, and is the one kept
// once it's written; a large file (in chunks) changed in a few bytes
// arrives whole.
func TestFilesTabsWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	srv := hostServer(t, filesTabsPage, nil)
	c := launchCDP(t, browser)
	tab := func() string {
		var created struct{ TargetID string }
		if err := c.call("", "Target.createTarget", map[string]any{"url": srv.URL + "/"}, &created, ""); err != nil {
			t.Fatal(err)
		}
		page := c.attach(t, created.TargetID)
		until(t, func() bool { return c.eval(t, page, `String(window.ready === true)`) == "true" }, func() string { return "not mounted" })
		return page
	}
	one, two := tab(), tab()
	eval := func(page, js string) string { return c.eval(t, page, js) }
	read := func(page, path string) string {
		b, _ := json.Marshal(path)
		return eval(page, `(() => { const b = fsx.readFile(`+string(b)+`); return b ? new TextDecoder().decode(b) : "(none)"; })()`)
	}
	waitRead := func(page, path, want string) {
		t.Helper()
		until(t, func() bool { return read(page, path) == want }, func() string { return read(page, path) })
	}

	// One writes; the other reads it, and its watch hears.
	eval(two, `window.heard = []; fsx.memfs.watch("/data/tabs", {}, (type, name) => heard.push(type + " " + name)); "ok"`)
	eval(one, `fsx.writeFile("/data/tabs/a.txt", new TextEncoder().encode("one")); fsx.flush().then(() => "ok")`)
	waitRead(two, "/data/tabs/a.txt", "one")
	until(t, func() bool { return eval(two, `heard.join(",")`) == "rename a.txt" }, func() string { return eval(two, `heard.join(",")`) })

	// Two changes b.txt and keeps it unwritten while one writes its own.
	eval(two, `fsx.writeFile("/data/tabs/b.txt", new TextEncoder().encode("two's")); "ok"`)
	eval(one, `fsx.writeFile("/data/tabs/b.txt", new TextEncoder().encode("one's")); fsx.flush().then(() => "ok")`)
	if got := read(two, "/data/tabs/b.txt"); got != "two's" {
		t.Fatalf("two's unwritten change: %q", got)
	}
	eval(two, `fsx.flush().then(() => "ok")`)
	waitRead(one, "/data/tabs/b.txt", "two's")

	// A large file: one's whole, then a few bytes of it changed.
	eval(one, `window.big = new Uint8Array(300000).map((_, i) => i % 251); fsx.writeFile("/data/tabs/big", big); fsx.flush().then(() => "ok")`)
	sum := func(page string) string {
		return eval(page, `(() => { const b = fsx.readFile("/data/tabs/big"); if (!b) return "none"; let s = 0; for (const x of b) s = (s * 31 + x) >>> 0; return b.length + ":" + s; })()`)
	}
	until(t, func() bool { return sum(two) == sum(one) }, func() string { return sum(two) + " want " + sum(one) })
	eval(one, `(async () => {
		const fd = await new Promise((r) => fsx.memfs.open("/data/tabs/big", 2, 0, (e, fd) => r(fd)));
		await new Promise((r) => fsx.memfs.write(fd, new Uint8Array([7, 7, 7]), 0, 3, 200000, () => r()));
		await new Promise((r) => fsx.memfs.close(fd, () => r()));
		await fsx.flush();
		return "ok";
	})()`)
	until(t, func() bool { return sum(two) == sum(one) }, func() string { return sum(two) + " want " + sum(one) })

	// Removed in one: gone in the other.
	eval(one, `new Promise((r) => fsx.memfs.unlink("/data/tabs/a.txt", () => r())).then(() => fsx.flush()).then(() => "ok")`)
	waitRead(two, "/data/tabs/a.txt", "(none)")
}

const filesTabsPage = `<!doctype html>
<meta charset="utf-8">
<script type="module">
import * as fs from "/asuka/fs.js";
await fs.mount({ name: "tabs", dir: "/data/tabs" });
window.fsx = fs;
window.ready = true;
</script>
`

// TestSQLiteIDBWeb runs the SQLite worker's files with the store a
// browser without the origin private file system's sync access handles
// gets (IndexedDB), on two tabs' main threads: they take turns at one
// database, each reading what the other wrote, and it's kept across a
// reload.
func TestSQLiteIDBWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	srv := hostServer(t, idbPage, nil)
	c := launchCDP(t, browser)
	tab := func() string {
		var created struct{ TargetID string }
		if err := c.call("", "Target.createTarget", map[string]any{"url": srv.URL + "/"}, &created, ""); err != nil {
			t.Fatal(err)
		}
		page := c.attach(t, created.TargetID)
		until(t, func() bool { return c.eval(t, page, `String(window.ready === true)`) == "true" }, func() string { return "not loaded" })
		return page
	}
	must := func(page, js, want string) {
		t.Helper()
		if got := c.eval(t, page, js); got != want {
			t.Fatalf("%s: %q, want %q", js, got, want)
		}
	}
	one := tab()
	must(one, `db.open("/data/idb/x.db")`, "ok")
	must(one, `db.exec("create table t (x); insert into t values (1)")`, "ok")
	two := tab()
	must(two, `db.open("/data/idb/x.db")`, "ok")
	must(two, `db.one("select count(*) from t")`, "1")
	must(two, `db.exec("insert into t values (2)")`, "ok")
	must(one, `db.one("select sum(x) from t")`, "3")
	must(one, `db.exec("insert into t values (3)")`, "ok")
	must(two, `db.one("select sum(x) from t")`, "6")
	// Kept: a reload reads it from IndexedDB.
	var res json.RawMessage
	if err := c.call(one, "Page.reload", map[string]any{}, &res, ""); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return c.eval(t, one, `String(window.ready === true)`) == "true" }, func() string { return "not loaded" })
	must(one, `db.open("/data/idb/x.db")`, "ok")
	must(one, `db.one("select sum(x) from t")`, "6")
	must(one, `db.one("pragma integrity_check")`, "ok")
}

const idbPage = `<!doctype html>
<meta charset="utf-8">
<script type="module">
import { Engine, Reader, Writer } from "/asuka/sqlite-engine.js";
import { VFS } from "/asuka/sqlite-vfs.js";
import { IDBStore, openIDB } from "/asuka/sqlite-store.js";
const OPEN = 1, EXEC = 3, QUERY = 4, BUSY = 5;
const idb = openIDB("asuka-sqlite-e2e");
const vfs = new VFS({ kept: (p) => p.startsWith("/data/"), store: (p) => new IDBStore(idb, p) });
const engine = await Engine.load(await fetch("/sqlite/asqlite.wasm"), vfs);
let conn = 0;
const call = (op, input) => {
  const b = engine.call(op, conn, 0, input, 65536);
  vfs.afterCall();
  const r = new Reader(b);
  const ok = r.u8() === 1;
  r.u32();
  return ok ? { ok, r } : { ok, code: r.u32(), auto: r.u8(), message: r.string() };
};
const retry = async (f) => {
  for (let i = 0; i < 1000; i++) {
    const a = f();
    if (a.ok || (a.code & 0xff) !== BUSY) return a;
    await new Promise((r) => setTimeout(r, 5));
  }
  return { ok: false, message: "still busy" };
};
const req = (sql) => new Writer().u8(0).string(sql).u32(0).bytes();
window.db = {
  async open(path) {
    await vfs.prepare(path);
    const a = call(OPEN, new Writer().u32(0).string(path).bytes());
    if (!a.ok) return a.message;
    conn = Number(a.r.u64());
    return "ok";
  },
  async exec(sql) {
    const a = await retry(() => call(EXEC, req(sql)));
    return a.ok ? "ok" : a.message;
  },
  async one(sql) {
    const a = await retry(() => call(QUERY, req(sql)));
    if (!a.ok) return a.message;
    const r = a.r;
    r.u32(); // cursor
    for (let n = r.u32(); n > 0; n--) { r.string(); r.string(); }
    r.u8();
    if (!r.u32()) return "no rows";
    switch (r.u8()) {
      case 1: return String(r.i64());
      case 3: return r.string();
      default: return "?";
    }
  },
};
window.ready = true;
</script>
`

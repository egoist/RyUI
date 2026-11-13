// SQLite's web side, in Node: the module the sqlite plugin builds
// (asqlite.wasm: ../testing/asqlite.js) driven as the page's worker drives it
// (sqlite-engine.js), its files (sqlite-vfs.js): answers, the tables
// commits write and queries read, long answers, the browser's time zone,
// locks between connections, and two tabs taking turns at one kept
// database (Node's Web Locks and BroadcastChannel; a store in memory).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { loadWasm } from "../testing/asqlite.js";
import { Engine, Reader, Writer, errorAnswer, localtime } from "../src/sqlite-engine.js";
import { MemoryFile, VFS } from "../src/sqlite-vfs.js";

const OPEN = 1, CLOSE = 2, EXEC = 3, QUERY = 4, NEXT = 5, CLOSE_ROWS = 6, SERIALIZE = 9;
const BUSY = 5;

const wasm = await loadWasm();
const module = await WebAssembly.compile(wasm);

async function engine(options) {
  const vfs = new VFS(options);
  return { vfs, e: await Engine.load(module, vfs) };
}

const enc = new TextEncoder();

// A tab's (or a page's) calls, as Go makes them.
function program({ e, vfs }, cap = 65536) {
  const call = (op, conn, arg, input) => {
    const a = answer(e.call(op, conn, arg, input, cap));
    vfs.afterCall();
    return a;
  };
  const req = (sql, args = [], record = false) => {
    const w = new Writer().u8(record ? 1 : 0).string(sql).u32(args.length);
    for (const a of args) {
      w.string("");
      if (a === null) w.u8(0);
      else if (typeof a === "bigint") w.u8(1).i64(a);
      else if (typeof a === "number") w.u8(2).f64(a);
      else if (a instanceof Uint8Array) w.u8(4).u32(a.length).raw(a);
      else { const b = enc.encode(a); w.u8(3).u32(b.length).raw(b); }
    }
    return w.bytes();
  };
  const p = {
    open(path) {
      const r = call(OPEN, 0, 0, new Writer().u32(0).string(path).bytes());
      assert.ok(r.ok, r.message);
      return Number(r.body.u64());
    },
    close(c) { e.call(CLOSE, c, 0, new Uint8Array(0), cap); },
    exec(c, sql, args) { return call(EXEC, c, 0, req(sql, args)); },
    must(c, sql, args) {
      const r = p.exec(c, sql, args);
      assert.ok(r.ok, `${sql}: ${r.message}`);
      return r;
    },
    query(c, sql, args, record) {
      const r = call(QUERY, c, 0, req(sql, args, record));
      if (!r.ok) return r;
      const cursor = r.body.u32();
      const cols = [];
      for (let i = r.body.u32(); i > 0; i--) cols.push([r.body.string(), r.body.string()]);
      const rows = [];
      let done = rowsOf(r.body, cols.length, rows), chunks = 1;
      while (!done) {
        const n = call(NEXT, c, cursor, new Uint8Array([0]));
        assert.ok(n.ok, n.message);
        done = rowsOf(n.body, cols.length, rows);
        chunks++;
      }
      return { ok: true, cols, rows, chunks, written: r.written, read: r.read };
    },
    rows(c, sql, args) {
      const r = p.query(c, sql, args);
      assert.ok(r.ok, `${sql}: ${r.message}`);
      return r.rows;
    },
    raw: call,
  };
  return p;
}

function answer(b) {
  const r = new Reader(b);
  const ok = r.u8() === 1;
  const at = r.u32();
  const t = new Reader(b);
  t.p = at;
  const written = [], read = [];
  for (let n = t.u32(); n > 0; n--) written.push(t.string());
  for (let n = t.u32(); n > 0; n--) read.push(t.string());
  const body = new Reader(b.subarray(0, at));
  body.p = 5;
  if (!ok) return { ok, code: body.u32(), autocommit: body.u8() === 1, message: body.string(), written, read, length: b.length };
  return { ok, body, written, read, length: b.length };
}

function rowsOf(r, ncols, rows) {
  const done = r.u8() === 1;
  for (let n = r.u32(); n > 0; n--) {
    const row = [];
    for (let i = 0; i < ncols; i++) {
      switch (r.u8()) {
        case 0: row.push(null); break;
        case 1: row.push(r.i64()); break;
        case 2: row.push(r.f64()); break;
        case 3: row.push(r.string()); break;
        case 4: row.push(r.bytes(r.u32()).slice()); break;
      }
    }
    rows.push(row);
  }
  return done;
}

test("the module is the SQLite the Go package was built for", async () => {
  const { e } = await engine();
  const src = await readFile(new URL("../../../plugins/sqlite/version_gen.go", import.meta.url), "utf8");
  assert.equal(e.version(), Number(/libVersion = (\d+)/.exec(src)[1]));
});

test("statements, rows, and the tables commits write and queries read", async () => {
  const p = program(await engine());
  const c = p.open("/tmp/t1.db");
  p.must(c, "create table notes (id integer primary key, text text, n real, b blob)");
  const w = p.must(c, "insert into notes (text, n, b) values (?, ?, ?)", ["héllo", 1.5, new Uint8Array([1, 2])]);
  assert.deepEqual(w.written, ["notes"]);
  assert.equal(w.body.i64(), 1n); // last insert id
  assert.equal(w.body.i64(), 1n); // changes
  const q = p.query(c, "select id, text, n, b, null from notes", [], true);
  assert.deepEqual(q.cols.map((x) => x[0]), ["id", "text", "n", "b", "null"]);
  assert.deepEqual(q.rows, [[1n, "héllo", 1.5, new Uint8Array([1, 2]), null]]);
  assert.deepEqual(q.read, ["notes"]);
  const bad = p.exec(c, "insert into nope values (1)");
  assert.equal(bad.ok, false);
  assert.equal(bad.code, 1);
  assert.match(bad.message, /no such table: nope/);
  p.close(c);
});

test("a long answer comes whole: the page keeps what doesn't fit Go's buffer", async () => {
  const p = program(await engine(), 2048);
  const c = p.open("/tmp/t2.db");
  p.must(c, "create table t (x text)");
  const big = "x".repeat(100000);
  p.must(c, "insert into t values (?)", [big]);
  for (let i = 0; i < 200; i++) p.must(c, "insert into t values (?)", [`row ${i}`]);
  const q = p.query(c, "select x from t");
  assert.equal(q.rows.length, 201);
  assert.equal(q.rows[0][0], big);
  assert.ok(q.chunks > 2, `rows in chunks the buffer's size: ${q.chunks}`);
  const s = p.raw(SERIALIZE, c, 0, new Uint8Array([0]));
  assert.ok(s.ok && s.length > 100000);
});

test("localtime is the browser's time zone", async () => {
  const tz = process.env.TZ;
  process.env.TZ = "America/New_York";
  try {
    const p = program(await engine());
    const c = p.open("/tmp/t3.db");
    // 2024-07-01 12:00 UTC is 08:00 in New York (daylight saving time), and
    // 2024-01-01 12:00 UTC 07:00.
    assert.deepEqual(p.rows(c, "select datetime(1719835200, 'unixepoch', 'localtime'), datetime(1704110400, 'unixepoch', 'localtime')"),
      [["2024-07-01 08:00:00", "2024-01-01 07:00:00"]]);
    assert.deepEqual(p.rows(c, "select datetime('2024-07-01 08:00:00', 'utc')"), [["2024-07-01 12:00:00"]]);
    const v = new Int32Array(10);
    localtime(1719835200, v);
    assert.deepEqual([...v], [0, 0, 8, 1, 6, 124, 1, 182, 1, -4 * 3600]);
    const now = p.rows(c, "select unixepoch('now')")[0][0];
    assert.ok(Math.abs(Number(now) - Date.now() / 1000) < 5);
  } finally {
    process.env.TZ = tz;
  }
});

test("randomness and the extensions built in", async () => {
  const p = program(await engine());
  const c = p.open(":memory:");
  const [[a, b]] = p.rows(c, "select random(), random()");
  assert.notEqual(a, b);
  p.must(c, "create virtual table f using fts5(body)");
  p.must(c, "insert into f values ('the quick brown fox'), ('lazy dogs')");
  assert.deepEqual(p.rows(c, "select body from f where f match 'quick'"), [["the quick brown fox"]]);
  assert.deepEqual(p.rows(c, "select round(sqrt(16)), soundex('Robert')"), [[4, "R163"]]);
});

test("a page's connections to one database lock as processes would", async () => {
  const p = program(await engine());
  const a = p.open("/tmp/t4.db"), b = p.open("/tmp/t4.db");
  p.must(a, "create table t (x)");
  p.must(a, "begin immediate");
  p.must(a, "insert into t values (1)");
  const busy = p.exec(b, "insert into t values (2)");
  assert.equal(busy.code & 0xff, BUSY);
  assert.equal(busy.autocommit, true);
  assert.deepEqual(p.rows(b, "select count(*) from t"), [[0n]], "b reads what's committed");
  p.must(a, "commit");
  p.must(b, "insert into t values (2)");
  assert.deepEqual(p.rows(a, "select x from t order by x"), [[1n], [2n]]);
});

// disk is a store's files, shared by the tabs, which only one may open
// at a time (as a sync access handle's).
function disk() {
  const files = new Map(), holder = new Map();
  const store = (path) => {
    const s = {
      opened: 0,
      async prepare() {
        for (const x of ["", "-journal"]) if (!files.has(path + x)) files.set(path + x, new MemoryFile());
      },
      async open() {
        if (holder.get(path)) throw Object.assign(new Error("held"), { name: "NoModificationAllowedError" });
        holder.set(path, s);
        s.opened++;
      },
      async close() { holder.delete(path); },
      file: (x) => (holder.get(path) === s ? files.get(path + x) : null),
    };
    return s;
  };
  return { files, holder, store };
}

// retry runs f until it isn't SQLITE_BUSY, as Go does (conn.retry).
async function retry(f) {
  for (let i = 0; i < 200; i++) {
    const r = f();
    if (r.ok || (r.code & 0xff) !== BUSY) return r;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error("still busy");
}

test("two tabs take turns at a kept database: Web Locks, asked for on a BroadcastChannel", async () => {
  const d = disk();
  const kept = (path) => path.startsWith("/data/app/");
  const path = "/data/app/turns.db";
  const t1 = await engine({ kept, store: d.store }), t2 = await engine({ kept, store: d.store });
  const p1 = program(t1), p2 = program(t2);
  await t1.vfs.prepare(path);
  const a = p1.open(path);
  p1.must(a, "create table t (x)");
  p1.must(a, "insert into t values ('one')");
  // Tab 2 opens: tab 1, idle, lets it go when asked.
  await t2.vfs.prepare(path);
  assert.equal(d.holder.get(path) !== undefined, true);
  const b = p2.open(path);
  assert.deepEqual(p2.rows(b, "select x from t"), [["one"]]);
  p2.must(b, "insert into t values ('two')");
  // Tab 1 needs it again: busy until tab 2 gives it back.
  const first = p1.exec(a, "insert into t values ('three')");
  assert.equal(first.code & 0xff, BUSY);
  assert.equal(first.autocommit, true);
  await retry(() => p1.exec(a, "insert into t values ('three')"));
  assert.deepEqual((await retry(() => p1.query(a, "select x from t order by rowid"))).rows, [["one"], ["two"], ["three"]]);
  // A transaction keeps it: tab 2 waits for the commit.
  p1.must(a, "begin immediate");
  p1.must(a, "insert into t values ('four')");
  const waiting = retry(() => p2.exec(b, "insert into t values ('five')"));
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(d.holder.get(path)?.opened !== undefined && t1.vfs.databases.get(path).held, true, "tab 1 keeps it in its transaction");
  p1.must(a, "commit");
  t1.vfs.afterCall();
  await waiting;
  assert.deepEqual((await retry(() => p2.query(b, "select x from t order by rowid"))).rows, [["one"], ["two"], ["three"], ["four"], ["five"]]);
  p1.close(a);
  p2.close(b);
  await t1.vfs.shutdown();
  await t2.vfs.shutdown();
});

test("a database from where the page's files kept it moves in, once", async () => {
  // Made elsewhere: its bytes.
  const src = program(await engine());
  const c = src.open("/tmp/old.db");
  src.must(c, "create table t (x); insert into t values ('kept')");
  const bytes = new Uint8Array(src.raw(SERIALIZE, c, 0, new Uint8Array([0])).body.b);
  const s = new Reader(bytes);
  s.p = 5;
  const n = Number(s.u64());
  const old = bytes.slice(13, 13 + n);
  const d = disk();
  const kept = () => true;
  const t = await engine({ kept, store: d.store });
  const path = "/data/app/moved.db";
  assert.equal(await t.vfs.prepare(path, { db: old, journal: null }), true);
  const p = program(t);
  const db = p.open(path);
  assert.deepEqual(p.rows(db, "select x from t"), [["kept"]]);
  p.must(db, "insert into t values ('new')");
  // Not again: the store has it.
  assert.equal(await t.vfs.prepare(path, { db: old, journal: null }), false);
  assert.deepEqual(p.rows(db, "select count(*) from t"), [[2n]]);
  await t.vfs.shutdown();
});

test("an error answer, as the page makes one", () => {
  const a = answer(errorAnswer(14, "no worker"));
  assert.deepEqual([a.ok, a.code, a.autocommit, a.message, a.written, a.read], [false, 14, true, "no worker", [], []]);
});

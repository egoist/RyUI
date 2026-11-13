// SQLite's web side (src/sqlite.js), as sqlite/driver_js.go drives it:
// its answers, the tables commits write and queries read, the databases
// in the page's file system, and its locks.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { Engine, Reader, Writer } from "../src/sqlite.js";
import * as fs from "../src/fs.js";

const OPEN = 1, CLOSE = 2, EXEC = 3, QUERY = 4, NEXT = 5, CLOSE_ROWS = 6, TAKE = 7;

// SQLite in Node: its module, given the wasm (it would fetch it).
async function engine() {
  const dir = new URL("../sqlite/", import.meta.url);
  const wasm = await readFile(new URL("sqlite3.wasm", dir));
  globalThis.sqlite3ApiConfig = { warn: () => {}, log: () => {}, debug: () => {}, disable: { vfs: { kvvfs: true, opfs: true, "opfs-sahpool": true, "opfs-wl": true } } };
  const { default: init } = await import(new URL("sqlite3.mjs", dir).href);
  const sqlite3 = await init({
    print: () => {}, printErr: () => {},
    instantiateWasm: (imports, ok) => { WebAssembly.instantiate(wasm, imports).then((r) => ok(r.instance, r.module)); return {}; },
  });
  delete globalThis.sqlite3ApiConfig;
  fs.writeFile("/data/test/.keep", new Uint8Array(0)); // the directory (fs.mount is the page's)
  return new Engine(sqlite3);
}

// A Go program's memory, as the engine sees it, and its calls.
function program(e) {
  const mem = new WebAssembly.Memory({ initial: 64 });
  const api = e.api(() => mem);
  // A small buffer, so the long answers are taken with a bigger one.
  const IN = 0, OUT = 65536, CAP = 128;
  const call = (op, conn, arg, input) => {
    new Uint8Array(mem.buffer).set(input, IN);
    let at = OUT;
    let n = api.call(op, conn, arg, IN, input.length, OUT, CAP);
    if (n < 0) {
      at = OUT + 1024;
      n = api.call(TAKE, 0, 0, 0, 0, at, -n);
    }
    return answer(new Uint8Array(mem.buffer, at, n).slice());
  };
  const enc = new TextEncoder();
  const req = (sql, args = [], record = false) => {
    const w = new Writer().u8(record ? 1 : 0).string(sql).u32(args.length);
    for (const a of args) {
      w.string("");
      if (a === null) w.u8(0);
      else if (typeof a === "bigint") w.u8(1).i64(a);
      else if (typeof a === "number") w.u8(2).f64(a);
      else { const b = enc.encode(a); w.u8(3).u32(b.length).raw(b); }
    }
    return w.bytes().slice();
  };
  return {
    open(path) { const r = call(OPEN, 0, 0, new Writer().string(path).bytes().slice()); return r.body.u32(); },
    close(c) { call(CLOSE, c, 0, new Uint8Array(0)); },
    exec(c, sql, args) { return call(EXEC, c, 0, req(sql, args)); },
    query(c, sql, args, record) {
      const r = call(QUERY, c, 0, req(sql, args, record));
      if (!r.ok) return r;
      const cursor = r.body.u32();
      const cols = [];
      for (let i = r.body.u32(); i > 0; i--) cols.push([r.body.string(), r.body.string()]);
      const rows = [];
      let done = rowsOf(r.body, cols.length, rows);
      let tables = { written: r.written, read: r.read };
      while (!done) {
        const n = call(NEXT, c, cursor, new Uint8Array([0]));
        done = rowsOf(n.body, cols.length, rows);
      }
      return { ok: true, cursor, cols, rows, ...tables };
    },
    next(c, cursor) { return call(NEXT, c, cursor, new Uint8Array([0])); },
    closeRows(c, cursor) { return call(CLOSE_ROWS, c, cursor, new Uint8Array([0])); },
  };
}

function answer(b) {
  const r = new Reader(b);
  const ok = r.u8() === 1;
  const written = [], read = [];
  for (let n = r.u32(); n > 0; n--) written.push(r.string());
  for (let n = r.u32(); n > 0; n--) read.push(r.string());
  if (!ok) return { ok, written, read, code: r.u32(), autocommit: r.u8(), message: r.string() };
  return { ok, written, read, body: r };
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
        case 3: row.push(new TextDecoder().decode(r.bytes(r.u32()))); break;
        case 4: row.push(r.bytes(r.u32()).slice()); break;
      }
    }
    rows.push(row);
  }
  return done;
}

const e = await engine();

test("a database in the page's files: written, read, and the tables each touched", () => {
  const p = program(e);
  const c = p.open("/data/test/notes.db");
  assert.ok(c > 0);
  let r = p.exec(c, "create table notes (id integer primary key, text text not null, done bool not null default 0); create view open as select * from notes where not done");
  assert.ok(r.ok, r.message);
  r = p.exec(c, "insert into notes (text) values (?)", ["Buy milk"]);
  assert.ok(r.ok, r.message);
  assert.equal(r.body.i64(), 1n); // last insert id
  assert.equal(r.body.i64(), 1n); // changes
  assert.deepEqual(r.written, ["notes"]);
  // A view's tables are read.
  const q = p.query(c, "select id, text, done from open order by id", [], true);
  assert.deepEqual(q.cols, [["id", "INTEGER"], ["text", "TEXT"], ["done", "bool"]]);
  assert.deepEqual(q.rows, [[1n, "Buy milk", 0n]]);
  assert.deepEqual(q.read.sort(), ["notes", "open"]);
  // Again, from the cache: the same tables.
  const q2 = p.query(c, "select id, text, done from open order by id", [], true);
  assert.deepEqual(q2.read.sort(), ["notes", "open"]);
  // The file is the page's: Go's os sees it.
  assert.ok(fs.file.exists("/data/test/notes.db"));
  assert.ok(fs.file.size("/data/test/notes.db") >= 4096);
  p.close(c);
});

test("transactions: a commit reports its tables, a rollback none", () => {
  const p = program(e);
  const c = p.open("/data/test/tx.db");
  p.exec(c, "create table a (x); create table b (x)");
  assert.ok(p.exec(c, "begin").ok);
  let r = p.exec(c, "insert into a values (1)");
  assert.deepEqual(r.written, []); // not yet committed
  r = p.exec(c, "commit");
  assert.deepEqual(r.written, ["a"]);
  p.exec(c, "begin");
  p.exec(c, "insert into b values (1)");
  r = p.exec(c, "rollback");
  assert.deepEqual(r.written, []);
  // A delete of every row (SQLite's truncate optimization) is a write, from the cache too.
  p.exec(c, "delete from a");
  p.exec(c, "insert into a values (2)");
  r = p.exec(c, "delete from a");
  assert.deepEqual(r.written, ["a"]);
  p.close(c);
});

test("errors read as modernc.org/sqlite's", () => {
  const p = program(e);
  const c = p.open("/data/test/err.db");
  const r = p.exec(c, "select * from nope");
  assert.equal(r.ok, false);
  assert.equal(r.message, "SQL logic error: no such table: nope (1)");
  const m = p.exec(c, "select ?, ?", ["x"]);
  assert.equal(m.message, "missing argument with index 2");
  p.close(c);
});

test("values: integers past 2^53, floats, text, blobs, nulls, named arguments", () => {
  const p = program(e);
  const c = p.open(":memory:");
  const big = 9007199254740993n;
  const w = new Writer().u8(0).string("select ?1, ?2, :name, ?4, ?5").u32(5);
  w.string("").u8(1).i64(big);
  w.string("").u8(2).f64(1.5);
  w.string("name").u8(3).u32(2).raw(new TextEncoder().encode("hi"));
  w.string("").u8(4).u32(3).raw(new Uint8Array([1, 2, 3]));
  w.string("").u8(0);
  const mem = new WebAssembly.Memory({ initial: 2 });
  const api = e.api(() => mem);
  const input = w.bytes().slice();
  new Uint8Array(mem.buffer).set(input, 0);
  // Answered in one go: the buffer is big enough.
  const n = api.call(QUERY, c, 0, 0, input.length, 4096, 4096);
  const r = answer(new Uint8Array(mem.buffer, 4096, n).slice());
  assert.ok(r.ok, r.message);
  r.body.u32();
  const cols = [];
  for (let i = r.body.u32(); i > 0; i--) cols.push([r.body.string(), r.body.string()]);
  const rows = [];
  rowsOf(r.body, cols.length, rows);
  assert.deepEqual(rows, [[big, 1.5, "hi", new Uint8Array([1, 2, 3]), null]]);
  p.close(c);
});

test("many rows come in chunks", () => {
  const p = program(e);
  const c = p.open("/data/test/many.db");
  p.exec(c, "create table t (s text)");
  p.exec(c, "begin");
  for (let i = 0; i < 3000; i++) p.exec(c, "insert into t values (?)", ["x".repeat(200) + i]);
  p.exec(c, "commit");
  const q = p.query(c, "select s from t", []);
  assert.equal(q.rows.length, 3000);
  assert.equal(q.rows[2999][0], "x".repeat(200) + 2999);
  assert.deepEqual(p.query(c, "select count(*) from t", []).rows, [[3000n]]);
  // A cursor closed early gives its statement back, to run again.
  const enc = new TextEncoder();
  const mem = new WebAssembly.Memory({ initial: 64 });
  const api = e.api(() => mem);
  const sql = enc.encode("select s from t");
  const input = new Uint8Array([0, sql.length, 0, 0, 0, ...sql, 0, 0, 0, 0]);
  new Uint8Array(mem.buffer).set(input);
  assert.ok(api.call(QUERY, c, 0, 0, input.length, 4096, 1 << 20) > 0);
  const conn = e.conns.get(c);
  assert.equal(conn.cursors.size, 1);
  const [cursor] = conn.cursors.keys();
  assert.ok(p.closeRows(c, cursor).ok);
  assert.equal(conn.cursors.size, 0);
  assert.equal(p.query(c, "select s from t", []).rows.length, 3000);
  p.close(c);
});

test("two connections to one file lock each other out, as two processes would", () => {
  const p = program(e);
  const a = p.open("/data/test/locks.db"), b = p.open("/data/test/locks.db");
  p.exec(a, "create table t (x)");
  assert.ok(p.exec(a, "begin immediate").ok);
  p.exec(a, "insert into t values (1)");
  const busy = p.exec(b, "insert into t values (2)");
  assert.equal(busy.ok, false);
  assert.equal(busy.code & 0xff, 5); // SQLITE_BUSY
  assert.equal(busy.autocommit, 1);
  p.exec(a, "commit");
  assert.ok(p.exec(b, "insert into t values (2)").ok);
  assert.deepEqual(p.query(a, "select x from t order by x", []).rows, [[1n], [2n]]);
  p.close(a);
  p.close(b);
});

test("a database reopened has what was committed, and a journal left by a crash is rolled back", () => {
  const p = program(e);
  let c = p.open("/data/test/keep.db");
  p.exec(c, "create table t (x)");
  p.exec(c, "insert into t values ('kept')");
  // A transaction under way when the page goes: its journal is there.
  p.exec(c, "begin");
  p.exec(c, "pragma cache_size = 1");
  for (let i = 0; i < 200; i++) p.exec(c, "insert into t values (?)", ["y".repeat(1000)]);
  const db = fs.readFile("/data/test/keep.db").slice();
  const journal = fs.readFile("/data/test/keep.db-journal")?.slice();
  assert.ok(journal, "a hot journal");
  p.close(c);
  // As the page would have kept them.
  fs.writeFile("/data/test/keep2.db", db);
  fs.writeFile("/data/test/keep2.db-journal", journal);
  c = p.open("/data/test/keep2.db");
  assert.deepEqual(p.query(c, "select x from t", []).rows, [["kept"]]);
  assert.equal(fs.file.exists("/data/test/keep2.db-journal"), false);
  p.close(c);
});

// The page's side of SQLite (src/sqlite.js), as sdk/sqlite/lib_js.go calls
// it, with a worker that answers what the test says: answers written into
// Go's memory, a long one kept for TAKE, a database the page's files kept
// sent to the worker and removed once it's moved, a worker that won't
// start.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client } from "../src/sqlite.js";
import { Reader, Writer } from "../src/sqlite-engine.js";
import * as fs from "../src/fs.js";

const OPEN = 1, EXEC = 3, TAKE = 7;

// fakeWorker answers each call with reply(call), a moment later.
function fakeWorker(reply) {
  const w = {
    sent: [],
    postMessage(m) {
      w.sent.push(m);
      if (m.call) setTimeout(() => w.onmessage({ data: { id: m.call.id, ...reply(m.call) } }), 0);
    },
  };
  return w;
}

function setup(reply) {
  let worker;
  const c = new Client("http://x/lib/asqlite.wasm", () => (worker = fakeWorker(reply)));
  const mem = new WebAssembly.Memory({ initial: 1 });
  const api = c.api(() => mem, "/data/app");
  const woken = [];
  api.connect((id, n) => woken.push([id, n]));
  const heap = () => new Uint8Array(mem.buffer);
  return { c, api, mem, heap, woken, worker: () => worker };
}

const tick = () => new Promise((r) => setTimeout(r, 5));

test("an answer is written into Go's buffer; a long one is kept for TAKE", async () => {
  const long = new Uint8Array(100).map((_, i) => i);
  const s = setup((call) => ({ answer: (call.outCap >= 100 ? long.slice(0, 10) : long.slice()).buffer, took: false }));
  assert.equal(s.worker().sent[0].init.wasm, "http://x/lib/asqlite.wasm");
  assert.deepEqual(s.worker().sent[1], { roots: ["/data/app"] });
  s.heap().set([1, 2, 3], 0);
  assert.equal(s.api.call(EXEC, 7, 0, 0, 3, 1000, 16, 1), undefined, "Go waits");
  const sent = s.worker().sent[2].call;
  assert.deepEqual([sent.op, sent.conn, [...new Uint8Array(sent.input)], sent.outCap], [EXEC, 7, [1, 2, 3], 16]);
  await tick();
  assert.deepEqual(s.woken, [[1, -100]]);
  assert.equal(s.api.call(TAKE, 7, 0, 0, 0, 2000, 50, 2), -100, "still too long");
  assert.equal(s.api.call(TAKE, 7, 0, 0, 0, 2000, 100, 3), 100);
  assert.deepEqual(s.heap().slice(2000, 2100), long);
  assert.equal(s.api.call(TAKE, 7, 0, 0, 0, 2000, 100, 4), 0, "taken once");
  // One that fits goes straight into the buffer.
  s.api.call(EXEC, 7, 0, 0, 0, 3000, 100, 5);
  await tick();
  assert.deepEqual(s.woken[1], [5, 10]);
  assert.deepEqual(s.heap().slice(3000, 3010), long.slice(0, 10));
});

test("a database the page's files kept goes to the worker, and leaves them once it's moved", async () => {
  fs.writeFile("/data/app/old.db", new Uint8Array([1, 2, 3]));
  fs.writeFile("/data/app/old.db-journal", new Uint8Array([4]));
  const ok = new Writer().u8(1).u32(13).i64(42n).u32(0).u32(0).bytes();
  const s = setup(() => ({ answer: ok.slice().buffer, took: true }));
  const input = new Writer().u32(0).string("/data/app/old.db").bytes();
  s.heap().set(input, 0);
  s.api.call(OPEN, 0, 0, 0, input.length, 1000, 65536, 1);
  const m = s.worker().sent.at(-1).call;
  assert.deepEqual([...m.migrate.db], [1, 2, 3]);
  assert.deepEqual([...m.migrate.journal], [4]);
  assert.ok(fs.readFile("/data/app/old.db"), "until it's moved");
  await tick();
  assert.equal(fs.readFile("/data/app/old.db"), null);
  assert.equal(fs.readFile("/data/app/old.db-journal"), null);
  const r = new Reader(s.heap().slice(1000, 1000 + s.woken[0][1]));
  r.p = 5;
  assert.equal(r.u64(), 42n);
});

test("a worker that won't start answers each call with an error", async () => {
  const c = new Client("http://x/lib/asqlite.wasm", () => { throw new Error("no workers here"); });
  const mem = new WebAssembly.Memory({ initial: 1 });
  const api = c.api(() => mem, "/data/app");
  let n = 0;
  api.connect((_, k) => { n = k; });
  api.call(EXEC, 1, 0, 0, 0, 100, 1000, 1);
  await tick();
  const r = new Reader(new Uint8Array(mem.buffer).slice(100, 100 + n));
  assert.equal(r.u8(), 0);
  r.u32();
  assert.equal(r.u32(), 1);
  r.u8();
  assert.match(r.string(), /SQLite's worker didn't start: no workers here/);
});

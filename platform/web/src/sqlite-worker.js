// The page's SQLite worker: SQLite's WebAssembly module (sqlite-engine.js)
// and its files (sqlite-vfs.js), which keep an app's databases in the
// origin private file system (else IndexedDB: sqlite-store.js). The page
// (sqlite.js) sends it Go's calls, a message each, and it answers each
// with asq_call's answer.
//
// Messages: { init: { wasm } } loads the module (its URL); { roots }
// adds data directories, whose databases are kept; { call } is a call:
// { id, op, conn, arg, input, outCap, migrate }, answered
// { id, answer, took }, took saying the database is kept here now (the
// page then removes the copy an earlier version kept in its files).

import { Engine, Reader, errorAnswer } from "./sqlite-engine.js";
import { VFS } from "./sqlite-vfs.js";
import { IDBStore, OPFSStore, openIDB, opfsRoot } from "./sqlite-store.js";

const OPEN = 1, INTERRUPT = 8;
const CANTOPEN = 14;

const roots = new Set();
const kept = (path) => {
  for (const r of roots) if (path.startsWith(r + "/")) return true;
  return false;
};

let vfs = null, engine = null, ready = null;

/** init loads the module, with the best store the browser has. */
async function init({ wasm }) {
  const res = fetch(wasm, { credentials: "same-origin" });
  let store = null;
  const root = await opfsRoot();
  if (root) store = (path) => new OPFSStore(async () => root, path);
  else if (globalThis.indexedDB) {
    const idb = openIDB();
    idb.catch(() => {});
    store = (path) => new IDBStore(idb, path);
  } else {
    console.warn("asuka/sqlite: no origin private file system or IndexedDB here: databases last only as long as the page");
  }
  vfs = new VFS({ kept, store });
  engine = await Engine.load(await res, vfs);
}

async function call({ id, op, conn, arg, input, outCap, migrate }) {
  let answer, took = false;
  try {
    await ready;
    const b = new Uint8Array(input);
    if (op === OPEN) {
      const r = new Reader(b);
      r.u32(); // busy
      const path = r.string();
      if (kept(path) && vfs.store) {
        // Held as SQLite opens it (it reads the header then): another
        // call's end may have let it go to another tab meanwhile.
        do await vfs.prepare(path, migrate);
        while (!vfs.databases.get(path)?.held);
        took = true;
      }
    }
    answer = op === INTERRUPT ? new Uint8Array(0) : engine.call(op, conn, arg, b, outCap);
    vfs.afterCall();
  } catch (e) {
    answer = errorAnswer(e?.code > 0 ? e.code : CANTOPEN, `${e?.message ?? e}`);
    took = false;
  }
  postMessage({ id, answer: answer.buffer, took }, [answer.buffer]);
}

onmessage = (e) => {
  const m = e.data;
  if (m.init) {
    ready = init(m.init).catch((e) => {
      console.error("asuka/sqlite: SQLite didn't load:", e);
      throw new Error(`SQLite didn't load: ${e?.message ?? e}`);
    });
    ready.catch(() => {}); // each call says it
  }
  if (m.roots) for (const r of m.roots) roots.add(r);
  if (m.call) call(m.call);
};

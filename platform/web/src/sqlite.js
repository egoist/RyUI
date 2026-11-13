// SQLite on the page, for asuka's sqlite plugin (sdk/sqlite/lib_js.go): the page's
// side of its SQLite worker (sqlite-worker.js), which runs SQLite's
// WebAssembly module (asqlite.wasm, which the CLI ships with the apps
// that use asuka/sdk/sqlite) and keeps databases in the origin private file
// system.
//
// Go calls api.call(op, conn, arg, inPtr, inLen, outPtr, outCap, id) from
// a goroutine, which then waits: the arguments are read from the Go
// program's memory and sent to the worker; its answer is written into
// Go's buffer, and api.done(id, n) wakes the goroutine (n: the answer's
// length). An answer longer than the buffer is kept here, and n is minus
// its length: Go takes it (TAKE) with a bigger buffer, which call answers
// at once, returning n rather than undefined.
//
// A database an earlier version kept in the page's files (fs.js, in
// IndexedDB) moves to the worker's store as it's first opened.

import * as fs from "./fs.js";
import { Reader, errorAnswer } from "./sqlite-engine.js";

const OPEN = 1, TAKE = 7, INTERRUPT = 8;
const ERROR = 1;

let client = null;

/** load starts the page's SQLite worker once, with its module (asqlite.wasm) at the URL wasm. */
export function load(wasm) {
  client ??= new Client(wasm);
  return Promise.resolve(client);
}

export class Client {
  constructor(wasm, makeWorker = (url) => new Worker(url, { type: "module" })) {
    this.waiting = new Map(); // message id -> answer handler
    this.nextID = 0;
    this.broken = null; // why the worker doesn't answer
    try {
      this.worker = makeWorker(new URL("./sqlite-worker.js", import.meta.url));
      this.worker.onmessage = (e) => this.answer(e.data);
      this.worker.onerror = (e) => this.fail(e.message || "the worker failed");
      this.worker.postMessage({ init: { wasm: new URL(wasm).href } });
    } catch (e) {
      this.fail(e.message);
    }
  }

  fail(why) {
    this.broken = `SQLite's worker didn't start: ${why}`;
    console.error("asuka/sdk/sqlite:", this.broken);
    const waiting = [...this.waiting.values()];
    this.waiting.clear();
    for (const f of waiting) f({ answer: errorAnswer(ERROR, this.broken).buffer, took: false });
  }

  answer(m) {
    const f = this.waiting.get(m.id);
    this.waiting.delete(m.id);
    f?.(m);
  }

  send(call, transfer, f) {
    if (this.broken) {
      queueMicrotask(() => f({ answer: errorAnswer(ERROR, this.broken).buffer, took: false }));
      return;
    }
    const id = ++this.nextID;
    this.waiting.set(id, f);
    this.worker.postMessage({ call: { ...call, id } }, transfer);
  }

  /**
   * api is an app's entry point: its Go program's memory is mem() (a
   * WebAssembly.Memory), its data directory dir (whose databases are
   * kept).
   */
  api(mem, dir) {
    if (dir && this.worker) this.worker.postMessage({ roots: [dir] });
    const kept = new Map(); // conn -> an answer too long for Go's buffer
    let done = null;
    const heap = () => new Uint8Array(mem().buffer);
    return {
      /** connect gives the page Go's function that wakes a call's goroutine: done(id, n). */
      connect(f) { done = f; },
      call: (op, conn, arg, inPtr, inLen, outPtr, outCap, id) => {
        if (op === TAKE) {
          const b = kept.get(conn);
          if (!b) return 0;
          if (b.length > outCap) return -b.length;
          kept.delete(conn);
          heap().set(b, outPtr);
          return b.length;
        }
        if (op === INTERRUPT) return 0; // a statement runs to its end: the worker answers one call at a time
        kept.delete(conn);
        const input = heap().slice(inPtr, inPtr + inLen);
        const transfer = [input.buffer];
        let path = null, migrate = null;
        if (op === OPEN) {
          const r = new Reader(input);
          r.u32();
          path = r.string();
          if (dir && path.startsWith(dir + "/")) {
            const db = fs.readFile(path);
            if (db?.length) {
              migrate = { db: db.slice(), journal: fs.readFile(path + "-journal")?.slice() ?? null };
              transfer.push(migrate.db.buffer);
              if (migrate.journal) transfer.push(migrate.journal.buffer);
            }
          }
        }
        this.send({ op, conn, arg, input: input.buffer, outCap, migrate }, transfer, (m) => {
          const answer = new Uint8Array(m.answer);
          if (migrate && m.took && answer[0] === 1) {
            // Moved: no longer the page's file.
            fs.removeFile(path);
            fs.removeFile(path + "-journal");
          }
          let n = answer.length;
          if (n > outCap) {
            kept.set(conn, answer);
            n = -n;
          } else heap().set(answer, outPtr);
          done(id, n);
        });
        return undefined;
      },
    };
  }
}

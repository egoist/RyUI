// Where the page's SQLite worker keeps databases (sqlite-vfs.js's
// stores): a database and its journal, read and written in place while
// the worker holds the database's lock.
//
//   - OPFSStore: files of the origin private file system, through sync
//     access handles (a worker's), open only while the database is held.
//     A database isn't loaded whole, and a commit is on disk when it
//     returns (SQLite syncs, the handle flushes).
//   - IDBStore: where there's no such file system (a page that isn't a
//     secure context, a private window of some browsers): the files in
//     memory, kept in IndexedDB in chunks of 64 KiB, those a call changed
//     written once it returns, all in one transaction; read again when the
//     database is held, if another tab wrote it meanwhile.

import { MemoryFile } from "./sqlite-vfs.js";

const SUFFIXES = ["", "-journal"];

// What createSyncAccessHandle throws on a file another handle has open
// (Chromium and Firefox; WebKit), which a tab closing may hold a moment
// after it let its lock go: tried again for a while.
const HELD = new Set(["NoModificationAllowedError", "InvalidStateError"]);
const HELD_FOR = 5000;

/** OPFSStore is a database's files in the origin private file system. */
export class OPFSStore {
  constructor(root, path) {
    this.root = root; // () => Promise<FileSystemDirectoryHandle>
    this.path = path;
    this.handles = null; // suffix -> FileSystemFileHandle
    this.open_ = null; // suffix -> file, while held
  }

  async prepare() {
    let dir = await this.root();
    const parts = this.path.split("/").filter((p) => p);
    const name = parts.pop();
    for (const p of parts) dir = await dir.getDirectoryHandle(p, { create: true });
    this.handles = {};
    for (const s of SUFFIXES) this.handles[s] = await dir.getFileHandle(name + s, { create: true });
  }

  async open() {
    const deadline = Date.now() + HELD_FOR;
    for (;;) {
      const got = {};
      const results = await Promise.allSettled(SUFFIXES.map(async (s) => { got[s] = await this.handles[s].createSyncAccessHandle(); }));
      const failed = results.find((r) => r.status === "rejected");
      if (!failed) {
        this.open_ = {};
        for (const s of SUFFIXES) this.open_[s] = syncFile(got[s]);
        return;
      }
      for (const h of Object.values(got)) h.close();
      if (!HELD.has(failed.reason?.name) || Date.now() >= deadline) throw failed.reason;
      await new Promise((r) => setTimeout(r, 10));
    }
  }

  async close() {
    const open = this.open_;
    this.open_ = null;
    for (const f of Object.values(open ?? {})) f.handle.close();
  }

  file(suffix) { return this.open_?.[suffix] ?? null; }
}

function syncFile(h) {
  return {
    handle: h,
    read: (dst, at) => h.read(dst, { at }),
    write: (src, at) => h.write(src, { at }),
    truncate: (n) => h.truncate(n),
    size: () => h.getSize(),
    flush: () => h.flush(),
  };
}

/** opfsRoot is the origin private file system's root, if a worker here can use its files synchronously; else null. */
export async function opfsRoot() {
  try {
    if (typeof FileSystemFileHandle === "undefined" || !FileSystemFileHandle.prototype.createSyncAccessHandle) return null;
    const root = await navigator.storage.getDirectory();
    // Some browsers have it but refuse it (a private window): tried.
    const probe = await root.getFileHandle(".asuka-probe", { create: true });
    const h = await probe.createSyncAccessHandle();
    h.close();
    await root.removeEntry(".asuka-probe").catch(() => {});
    return root;
  } catch {
    return null;
  }
}

const CHUNK = 64 * 1024;

/** openIDB opens the IndexedDB database IDBStores keep their files in. */
export function openIDB(name = "asuka-sqlite") {
  return new Promise((resolve, reject) => {
    const r = indexedDB.open(name, 1);
    r.onupgradeneeded = () => {
      r.result.createObjectStore("files");
      r.result.createObjectStore("chunks");
    };
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

const req = (r) => new Promise((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error); });
const done = (tx) => new Promise((resolve, reject) => { tx.oncomplete = resolve; tx.onerror = tx.onabort = () => reject(tx.error); });

/** IDBStore is a database's files in memory, kept in IndexedDB. */
export class IDBStore {
  constructor(idb, path) {
    this.idb = idb; // Promise<IDBDatabase>
    this.path = path;
    this.db = null;
    this.files = {}; // suffix -> { mem, gen, dirty: Set<chunk> | true }
    this.held = false;
    this.writing = Promise.resolve();
  }

  async prepare() {
    this.db = await this.idb;
    for (const s of SUFFIXES) this.files[s] = { mem: new MemoryFile(), gen: null, dirty: null };
  }

  /** open reads again the files another tab wrote since this one last had them. */
  async open() {
    const tx = this.db.transaction(["files", "chunks"], "readonly");
    const meta = tx.objectStore("files"), chunks = tx.objectStore("chunks");
    for (const s of SUFFIXES) {
      const p = this.path + s, f = this.files[s];
      const m = await req(meta.get(p));
      if ((m?.gen ?? null) === f.gen) continue;
      const data = new Uint8Array(m?.size ?? 0);
      if (m) {
        const [keys, values] = await Promise.all([req(chunks.getAllKeys(range(p))), req(chunks.getAll(range(p)))]);
        keys.forEach(([, i], j) => { if (i * CHUNK < m.size) data.set(values[j].subarray(0, m.size - i * CHUNK), i * CHUNK); });
      }
      f.mem = new MemoryFile(data);
      f.gen = m?.gen ?? null;
      f.dirty = null;
    }
    this.held = true;
  }

  async close() {
    this.afterCall();
    this.held = false;
    await this.writing;
  }

  file(suffix) {
    const f = this.files[suffix];
    const mark = (from, to) => {
      if (f.dirty === true) return;
      f.dirty ??= new Set();
      for (let i = Math.floor(from / CHUNK); i <= Math.floor(Math.max(from, to - 1) / CHUNK); i++) f.dirty.add(i);
    };
    return {
      read: (dst, at) => f.mem.read(dst, at),
      write: (src, at) => {
        mark(at, at + src.length);
        return f.mem.write(src, at);
      },
      truncate: (n) => {
        mark(Math.min(n, f.mem.size()), Math.max(n, f.mem.size()));
        f.mem.truncate(n);
      },
      size: () => f.mem.size(),
      flush: () => {},
    };
  }

  /** afterCall writes what the call changed, in one transaction. */
  afterCall() {
    const changed = SUFFIXES.filter((s) => this.files[s].dirty);
    if (!changed.length) return;
    const tx = this.db.transaction(["files", "chunks"], "readwrite");
    const meta = tx.objectStore("files"), chunks = tx.objectStore("chunks");
    for (const s of changed) {
      const p = this.path + s, f = this.files[s];
      const size = f.mem.size(), n = Math.ceil(size / CHUNK);
      const which = f.dirty === true ? [...Array(n).keys()] : [...f.dirty].filter((i) => i < n);
      for (const i of which) chunks.put(f.mem.data.slice(i * CHUNK, Math.min(size, (i + 1) * CHUNK)), [p, i]);
      chunks.delete(IDBKeyRange.bound([p, n], [p, Infinity]));
      f.gen = Math.random().toString(36).slice(2) + Date.now().toString(36);
      meta.put({ size, gen: f.gen }, p);
      f.dirty = null;
    }
    const prev = this.writing;
    this.writing = Promise.all([prev, done(tx)]).catch((e) => console.warn("asuka/sdk/sqlite: a database wasn't saved:", this.path, e));
  }
}

const range = (p) => IDBKeyRange.bound([p, 0], [p, Infinity]);

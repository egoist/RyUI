// The app's files: a file system in memory with the API of Node's fs that
// Go's syscall package calls on js/wasm, answered at once (Go waits for
// each call's callback, so its os package works unchanged: ryui/store,
// crash records, the app's own files). What's under the app's data
// directory (RYUI_DATA, /data/<the app's name>) is kept in IndexedDB: loaded before Go
// starts, written back shortly after each change and as the page hides.
// /tmp is the app's caches, kept for the page's life: what's dropped or
// pasted into it, files it makes to share.
//
// IndexedDB keeps a small file whole, and a large one (a SQLite database)
// in chunks of 64 KiB: a write stores only the chunks it changed.

const S_IFDIR = 0o040000, S_IFREG = 0o100000;

const files = new Map(); // path -> { dir, data, size, mode, mtime }
const fds = new Map(); // fd -> { path, pos, append, write }
let nextFD = 100;
// The data directories kept in IndexedDB: an app's each (several apps
// may run on one page, each with its own), and its database.
const stores = []; // { dir, db, dirty: Map<path, Set<chunk> | true (all of it)> }
const CHUNK = 64 * 1024;
let flushTimer = 0;
let out = { 1: "", 2: "" };
let onOutput = null;

const now = () => Date.now();
const put = (path, entry) => { files.set(path, entry); markDirty(path); };

function err(code) {
  const e = new Error(code);
  e.code = code;
  return e;
}

function clean(p) {
  if (!p.startsWith("/")) p = "/" + p;
  const parts = [];
  for (const s of p.split("/")) {
    if (!s || s === ".") continue;
    if (s === "..") parts.pop();
    else parts.push(s);
  }
  return "/" + parts.join("/");
}

const parent = (p) => p.slice(0, p.lastIndexOf("/")) || "/";

function mkdirs(p) {
  if (files.has(p)) return;
  if (p !== "/") mkdirs(parent(p));
  put(p, { dir: true, mode: S_IFDIR | 0o755, mtime: now(), size: 0, data: null });
}

function stat(e, path) {
  const mtime = e.mtime;
  return {
    dev: 1, ino: hashPath(path), mode: e.mode, nlink: 1, uid: 0, gid: 0, rdev: 0,
    size: e.dir ? 0 : e.size, blksize: 4096, blocks: Math.ceil((e.size || 0) / 512),
    atimeMs: mtime, mtimeMs: mtime, ctimeMs: mtime,
    isDirectory: () => !!e.dir,
  };
}

function hashPath(s) {
  let x = 7;
  for (let i = 0; i < s.length; i++) x = (x * 31 + s.charCodeAt(i)) | 0;
  return x >>> 0;
}

function grow(e, need) {
  if (e.data.length >= need) return;
  const b = new Uint8Array(Math.max(need, e.data.length * 2, 64));
  b.set(e.data.subarray(0, e.size));
  e.data = b;
}

const constants = { O_RDONLY: 0, O_WRONLY: 1, O_RDWR: 2, O_CREAT: 64, O_EXCL: 128, O_TRUNC: 512, O_APPEND: 1024, O_DIRECTORY: 65536 };

const decoder = new TextDecoder();

/** fs is what Go calls (globalThis.fs). Each call answers through its callback before it returns. */
export const memfs = {
  constants,
  writeSync(fd, buf) {
    if (fd === 1 || fd === 2) {
      out[fd] += decoder.decode(buf);
      const nl = out[fd].lastIndexOf("\n");
      if (nl >= 0) {
        const lines = out[fd].slice(0, nl);
        out[fd] = out[fd].slice(nl + 1);
        (fd === 2 ? console.error : console.log)(lines);
        onOutput?.(fd, lines + "\n");
      }
      return buf.length;
    }
    return writeAt(fd, buf, 0, buf.length, null);
  },
  write(fd, buf, offset, length, position, cb) {
    try {
      if (fd === 1 || fd === 2) return cb(null, this.writeSync(fd, buf.subarray(offset, offset + length)));
      cb(null, writeAt(fd, buf, offset, length, position));
    } catch (e) { cb(e); }
  },
  read(fd, buf, offset, length, position, cb) {
    const f = fds.get(fd);
    if (!f) return cb(err("EBADF"));
    const e = files.get(f.path);
    if (!e || e.dir) return cb(err("EISDIR"));
    const at = position ?? f.pos;
    const n = Math.max(0, Math.min(length, e.size - at));
    buf.set(e.data.subarray(at, at + n), offset);
    if (position == null) f.pos += n;
    cb(null, n);
  },
  open(path, flags, mode, cb) {
    path = clean(path);
    let e = files.get(path);
    if (flags & constants.O_CREAT) {
      if (e && flags & constants.O_EXCL) return cb(err("EEXIST"));
      if (!e) {
        const p = files.get(parent(path));
        if (!p) return cb(err("ENOENT"));
        if (!p.dir) return cb(err("ENOTDIR"));
        e = { dir: false, mode: S_IFREG | (mode & 0o777), mtime: now(), size: 0, data: new Uint8Array(0) };
        put(path, e);
      }
    }
    if (!e) return cb(err("ENOENT"));
    if (flags & constants.O_DIRECTORY && !e.dir) return cb(err("ENOTDIR"));
    const write = (flags & 3) !== 0;
    if (e.dir && write) return cb(err("EISDIR"));
    if (flags & constants.O_TRUNC && !e.dir && e.size) resize(path, e, 0);
    const fd = nextFD++;
    fds.set(fd, { path, pos: 0, append: !!(flags & constants.O_APPEND), write });
    cb(null, fd);
  },
  close(fd, cb) { fds.delete(fd); cb(null); },
  fsync(fd, cb) { cb(null); },
  fstat(fd, cb) {
    const f = fds.get(fd);
    const e = f && files.get(f.path);
    if (!e) return cb(err("EBADF"));
    cb(null, stat(e, f.path));
  },
  stat(path, cb) {
    path = clean(path);
    const e = files.get(path);
    if (!e) return cb(err("ENOENT"));
    cb(null, stat(e, path));
  },
  lstat(path, cb) { this.stat(path, cb); },
  mkdir(path, perm, cb) {
    path = clean(path);
    if (files.has(path)) return cb(err("EEXIST"));
    const p = files.get(parent(path));
    if (!p) return cb(err("ENOENT"));
    if (!p.dir) return cb(err("ENOTDIR"));
    put(path, { dir: true, mode: S_IFDIR | (perm & 0o777), mtime: now(), size: 0, data: null });
    cb(null);
  },
  readdir(path, cb) {
    path = clean(path);
    const e = files.get(path);
    if (!e) return cb(err("ENOENT"));
    if (!e.dir) return cb(err("ENOTDIR"));
    const prefix = path === "/" ? "/" : path + "/";
    const names = [];
    for (const k of files.keys()) {
      if (k !== path && k.startsWith(prefix) && !k.slice(prefix.length).includes("/")) names.push(k.slice(prefix.length));
    }
    cb(null, names.sort());
  },
  unlink(path, cb) {
    path = clean(path);
    const e = files.get(path);
    if (!e) return cb(err("ENOENT"));
    if (e.dir) return cb(err("EISDIR"));
    remove(path);
    cb(null);
  },
  rmdir(path, cb) {
    path = clean(path);
    const e = files.get(path);
    if (!e) return cb(err("ENOENT"));
    if (!e.dir) return cb(err("ENOTDIR"));
    const prefix = path + "/";
    for (const k of files.keys()) if (k.startsWith(prefix)) return cb(err("ENOTEMPTY"));
    remove(path);
    cb(null);
  },
  rename(from, to, cb) {
    from = clean(from);
    to = clean(to);
    const e = files.get(from);
    if (!e) return cb(err("ENOENT"));
    if (!files.get(parent(to))) return cb(err("ENOENT"));
    const moved = [[from, e]];
    if (e.dir) for (const [k, v] of files) if (k.startsWith(from + "/")) moved.push([k, v]);
    for (const [k] of moved) remove(k);
    for (const [k, v] of moved) put(to + k.slice(from.length), v);
    cb(null);
  },
  truncate(path, length, cb) {
    path = clean(path);
    const e = files.get(path);
    if (!e) return cb(err("ENOENT"));
    resize(path, e, length);
    cb(null);
  },
  ftruncate(fd, length, cb) {
    const f = fds.get(fd);
    const e = f && files.get(f.path);
    if (!e) return cb(err("EBADF"));
    resize(f.path, e, length);
    cb(null);
  },
  utimes(path, atime, mtime, cb) {
    path = clean(path);
    const e = files.get(path);
    if (!e) return cb(err("ENOENT"));
    e.mtime = mtime * 1000;
    put(path, e);
    cb(null);
  },
  chmod(path, mode, cb) {
    const e = files.get(clean(path));
    if (!e) return cb(err("ENOENT"));
    e.mode = (e.mode & ~0o777) | (mode & 0o777);
    cb(null);
  },
  fchmod(fd, mode, cb) { const f = fds.get(fd); f ? this.chmod(f.path, mode, cb) : cb(err("EBADF")); },
  chown(path, uid, gid, cb) { cb(null); },
  fchown(fd, uid, gid, cb) { cb(null); },
  lchown(path, uid, gid, cb) { cb(null); },
  link(path, link, cb) { cb(err("ENOSYS")); },
  symlink(path, link, cb) { cb(err("ENOSYS")); },
  readlink(path, cb) { cb(err("EINVAL")); },
};

function resize(path, e, length) {
  grow(e, length);
  if (length > e.size) e.data.fill(0, e.size, length);
  const old = e.size;
  e.size = length;
  e.mtime = now();
  markDirty(path, Math.min(old, length), Math.max(old, length));
}

function writeAt(fd, buf, offset, length, position) {
  const f = fds.get(fd);
  if (!f) throw err("EBADF");
  const e = files.get(f.path);
  if (!e || e.dir) throw err("EISDIR");
  let at = position ?? (f.append ? e.size : f.pos);
  grow(e, at + length);
  if (at > e.size) e.data.fill(0, e.size, at);
  e.data.set(buf.subarray(offset, offset + length), at);
  e.size = Math.max(e.size, at + length);
  e.mtime = now();
  if (position == null) f.pos = at + length;
  markDirty(f.path, at, at + length);
  return length;
}

function remove(path) {
  files.delete(path);
  markDirty(path);
}

// --- Keeping the data directory ---

// markDirty notes that path changed (its bytes from..to, or all of it), to
// be written to IndexedDB shortly.
function markDirty(path, from, to) {
  const st = stores.find((st) => path.startsWith(st.dir + "/"));
  if (!st?.db) return;
  let d = st.dirty.get(path);
  if (from === undefined) d = true;
  else if (d !== true) {
    d ??= new Set();
    for (let i = Math.floor(from / CHUNK), last = Math.floor(Math.max(from, to - 1) / CHUNK); i <= last; i++) d.add(i);
  }
  st.dirty.set(path, d);
  if (!flushTimer) flushTimer = setTimeout(flush, 50);
}

const chunksOf = (path, from = 0) => IDBKeyRange.bound([path, from], [path, Infinity]);

/**
 * flush writes the changed files of the data directories to IndexedDB,
 * each directory's in one transaction; it resolves once they're written
 * (and those before them).
 */
export function flush() {
  clearTimeout(flushTimer);
  flushTimer = 0;
  const done = [];
  for (const st of stores) {
    if (st.saving) done.push(st.saving);
    if (!st.db || !st.dirty.size) continue;
    const dirty = st.dirty;
    st.dirty = new Map();
    const tx = st.db.transaction(["files", "chunks"], "readwrite");
    const meta = tx.objectStore("files"), chunks = tx.objectStore("chunks");
    for (const [p, d] of dirty) {
      const e = files.get(p);
      if (!e) {
        meta.delete(p);
        chunks.delete(chunksOf(p));
      } else if (e.dir || e.size <= CHUNK) {
        meta.put({ dir: !!e.dir, mode: e.mode, mtime: e.mtime, data: e.dir ? null : e.data.slice(0, e.size) }, p);
        chunks.delete(chunksOf(p));
      } else {
        const n = Math.ceil(e.size / CHUNK);
        meta.put({ dir: false, mode: e.mode, mtime: e.mtime, size: e.size, chunked: true }, p);
        const put = (i) => chunks.put(e.data.slice(i * CHUNK, Math.min(e.size, (i + 1) * CHUNK)), [p, i]);
        // A file that was small has no chunks yet: all of them.
        if (d === true || !e.chunked) for (let i = 0; i < n; i++) put(i);
        else for (const i of d) if (i < n) put(i);
        chunks.delete(chunksOf(p, n));
      }
      if (e && !e.dir) e.chunked = e.size > CHUNK;
    }
    st.saving = new Promise((resolve) => {
      tx.oncomplete = () => resolve();
      tx.onerror = tx.onabort = () => {
        console.warn("ryui: the app's files weren't saved:", tx.error);
        resolve();
      };
    });
    done.push(st.saving);
  }
  return Promise.all(done).then(() => {});
}

/**
 * mount sets the file system up before Go starts: its directories, and
 * the data directory (dir) as the app last left it, from the IndexedDB
 * database name. output, if given, gets what Go prints.
 */
export async function mount({ name, dir = "/data", output } = {}) {
  if (output) onOutput = output;
  const st = { dir, db: null, dirty: new Map() };
  stores.push(st);
  for (const d of ["/", "/tmp", "/home", dir]) mkdirs(d);
  globalThis.fs = memfs;
  globalThis.process ??= {};
  Object.assign(globalThis.process, {
    getuid: () => -1, getgid: () => -1, geteuid: () => -1, getegid: () => -1,
    getgroups: () => { throw err("ENOSYS"); },
    pid: -1, ppid: -1,
    umask: () => 0o22,
    cwd: () => "/",
    chdir: () => { throw err("ENOSYS"); },
  });
  globalThis.path ??= { resolve: (...p) => clean(p.join("/")) };
  if (!globalThis.indexedDB) return;
  try {
    const db = await new Promise((resolve, reject) => {
      const req = indexedDB.open("ryui-files:" + (name || location.pathname), 2);
      req.onupgradeneeded = () => {
        const db = req.result;
        if (!db.objectStoreNames.contains("files")) db.createObjectStore("files");
        if (!db.objectStoreNames.contains("chunks")) db.createObjectStore("chunks");
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    const tx = db.transaction(["files", "chunks"], "readonly");
    const meta = tx.objectStore("files"), chunks = tx.objectStore("chunks");
    const [keys, values, ckeys, cvalues] = await Promise.all([req(meta.getAllKeys()), req(meta.getAll()), req(chunks.getAllKeys()), req(chunks.getAll())]);
    keys.forEach((k, i) => {
      const v = values[i];
      if (v.dir) files.set(k, { dir: true, mode: v.mode, mtime: v.mtime, size: 0, data: null });
      else if (v.chunked) files.set(k, { dir: false, mode: v.mode, mtime: v.mtime, size: v.size, data: new Uint8Array(v.size), chunked: true });
      else files.set(k, { dir: false, mode: v.mode, mtime: v.mtime, size: v.data.length, data: v.data });
    });
    ckeys.forEach(([p, i], j) => {
      const e = files.get(p);
      if (e?.chunked && i * CHUNK < e.size) e.data.set(cvalues[j].subarray(0, e.size - i * CHUNK), i * CHUNK);
    });
    for (const k of keys) mkdirs(parent(k));
    st.db = db;
  } catch (e) {
    console.warn("ryui: the app's files won't be kept:", e);
  }
  if (stores.length === 1) {
    addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", () => { if (document.hidden) flush(); });
  }
}

const req = (r) => new Promise((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error); });

// --- For SQLite (sqlite.js's VFS) ---

/**
 * file is a file SQLite opens, by its (clean) path: its bytes read and
 * written in place, as a disk's are, and kept as the data directory's
 * other files are.
 */
export const file = {
  /** exists reports whether there's a file at path. */
  exists(path) { const e = files.get(path); return !!e && !e.dir; },
  /** open makes the file if create is set and there's none; false if there's none (or it's a directory, or its directory isn't there). */
  open(path, create) {
    const e = files.get(path);
    if (e) return !e.dir;
    if (!create) return false;
    if (!files.get(parent(path))?.dir) return false;
    put(path, { dir: false, mode: S_IFREG | 0o644, mtime: now(), size: 0, data: new Uint8Array(0) });
    return true;
  },
  size(path) { return files.get(path)?.size ?? 0; },
  /** read copies the bytes at offset into dst, and returns how many there were. */
  read(path, dst, offset) {
    const e = files.get(path);
    if (!e || e.dir || offset >= e.size) return 0;
    const n = Math.min(dst.length, e.size - offset);
    dst.set(e.data.subarray(offset, offset + n));
    return n;
  },
  write(path, src, offset) {
    const e = files.get(path);
    if (!e || e.dir) throw err("ENOENT");
    const end = offset + src.length;
    grow(e, end);
    if (offset > e.size) e.data.fill(0, e.size, offset);
    e.data.set(src, offset);
    e.size = Math.max(e.size, end);
    e.mtime = now();
    markDirty(path, offset, end);
  },
  truncate(path, n) {
    const e = files.get(path);
    if (e && !e.dir) resize(path, e, n);
  },
  remove(path) {
    const e = files.get(path);
    if (e && !e.dir) remove(path);
  },
  clean,
};

// --- For the host ---

/** readFile is a file's bytes (null if there's none). */
export function readFile(path) {
  const e = files.get(clean(path));
  return e && !e.dir ? e.data.subarray(0, e.size) : null;
}

/** writeFile makes a file (and its directories). */
export function writeFile(path, bytes) {
  path = clean(path);
  mkdirs(parent(path));
  put(path, { dir: false, mode: S_IFREG | 0o644, mtime: now(), size: bytes.length, data: bytes });
}

/** fileOf is a File of a file's bytes, to drag or share. */
export function fileOf(path, name, type) {
  const b = readFile(path);
  if (!b) return null;
  return new File([b], name || path.slice(path.lastIndexOf("/") + 1), { type: type || "application/octet-stream" });
}

/** blobURL is a URL a page element loads a file from (an image the app saved). */
const urls = new Map();
export function blobURL(path) {
  path = clean(path);
  const e = files.get(path);
  if (!e || e.dir) return "";
  const old = urls.get(path);
  if (old && old.mtime === e.mtime) return old.url;
  if (old) URL.revokeObjectURL(old.url);
  const url = URL.createObjectURL(new Blob([e.data.subarray(0, e.size)]));
  urls.set(path, { url, mtime: e.mtime });
  return url;
}

let uploads = 0;

/** saveUpload copies a file the user gave the app (a drop, a paste, a picker) into its caches: its path there. */
export async function saveUpload(file) {
  const bytes = new Uint8Array(await file.arrayBuffer());
  const safe = (file.name || "file").replace(/[\/\\]/g, "_");
  const path = `/tmp/shared/${++uploads}/${safe}`;
  writeFile(path, bytes);
  return path;
}

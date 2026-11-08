// The app's files: a file system in memory with the API of Node's fs that
// Go's syscall package calls on js/wasm, answered at once (Go waits for
// each call's callback, so its os package works unchanged: ryui/store,
// crash records, the app's own files). What's under the app's data
// directory (RYUI_DATA, /data/<the app's name>) is kept in IndexedDB: loaded before Go
// starts, written back shortly after each change and as the page hides.
// /tmp is the app's caches, kept for the page's life: what's dropped or
// pasted into it, files it makes to share.

const S_IFDIR = 0o040000, S_IFREG = 0o100000;

const files = new Map(); // path -> { dir, data, size, mode, mtime }
const fds = new Map(); // fd -> { path, pos, append, write }
let nextFD = 100;
// The data directories kept in IndexedDB: an app's each (several apps
// may run on one page, each with its own), and its database.
const stores = []; // { dir, db, dirty }
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
    if (flags & constants.O_TRUNC && !e.dir) { e.size = 0; e.mtime = now(); put(path, e); }
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
    resize(e, length);
    put(path, e);
    cb(null);
  },
  ftruncate(fd, length, cb) {
    const f = fds.get(fd);
    const e = f && files.get(f.path);
    if (!e) return cb(err("EBADF"));
    resize(e, length);
    put(f.path, e);
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

function resize(e, length) {
  grow(e, length);
  if (length > e.size) e.data.fill(0, e.size, length);
  e.size = length;
  e.mtime = now();
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
  put(f.path, e);
  return length;
}

function remove(path) {
  files.delete(path);
  markDirty(path);
}

// --- Keeping the data directory ---

function markDirty(path) {
  const st = stores.find((st) => path.startsWith(st.dir + "/"));
  if (!st?.db) return;
  st.dirty.add(path);
  if (!flushTimer) flushTimer = setTimeout(flush, 250);
}

/** flush writes the changed files of the data directories to IndexedDB. */
export function flush() {
  clearTimeout(flushTimer);
  flushTimer = 0;
  for (const st of stores) {
    if (!st.db || !st.dirty.size) continue;
    const paths = st.dirty;
    st.dirty = new Set();
    const tx = st.db.transaction("files", "readwrite");
    const os = tx.objectStore("files");
    for (const p of paths) {
      const e = files.get(p);
      if (!e) os.delete(p);
      else os.put({ dir: !!e.dir, mode: e.mode, mtime: e.mtime, data: e.dir ? null : e.data.slice(0, e.size) }, p);
    }
  }
}

/**
 * mount sets the file system up before Go starts: its directories, and
 * the data directory (dir) as the app last left it, from the IndexedDB
 * database name. output, if given, gets what Go prints.
 */
export async function mount({ name, dir = "/data", output } = {}) {
  if (output) onOutput = output;
  const st = { dir, db: null, dirty: new Set() };
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
      const req = indexedDB.open("ryui-files:" + (name || location.pathname), 1);
      req.onupgradeneeded = () => req.result.createObjectStore("files");
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    const tx = db.transaction("files", "readonly");
    const os = tx.objectStore("files");
    const [keys, values] = await Promise.all([req(os.getAllKeys()), req(os.getAll())]);
    keys.forEach((k, i) => {
      const v = values[i];
      if (v.dir) files.set(k, { dir: true, mode: v.mode, mtime: v.mtime, size: 0, data: null });
      else files.set(k, { dir: false, mode: v.mode, mtime: v.mtime, size: v.data.length, data: v.data });
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

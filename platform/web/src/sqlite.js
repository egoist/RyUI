// SQLite in the browser, for asuka/sqlite (sqlite/driver_js.go): SQLite's
// own WebAssembly build (sqlite3.mjs and sqlite3.wasm from sqlite.org,
// which the CLI ships with the apps that import asuka/sqlite), driven
// synchronously from the app's Go code.
//
// Databases are files of the page's file system (fs.js), which Go's os
// sees too: a VFS ("asuka") reads and writes their bytes in place, and the
// data directory's files are kept in IndexedDB as they change. Each call
// from Go runs to its end before the page does anything else, so what the
// page keeps is always a state SQLite left between two calls, which its
// rollback journal recovers from, as it would after a crash.
//
// Go calls api.call(op, conn, arg, inPtr, inLen, outPtr, outCap): the
// arguments are read from the Go program's memory, the answer written
// into it (or, when it doesn't fit, kept until Go takes it with a bigger
// buffer: the call returns minus its length).

import * as fs from "./fs.js";

const OPEN = 1, CLOSE = 2, EXEC = 3, QUERY = 4, NEXT = 5, CLOSE_ROWS = 6, TAKE = 7;

// SQLite's codes.
const OK = 0, ERROR = 1, BUSY = 5, CANTOPEN = 14, NOTFOUND = 12, IOERR = 10, IOERR_SHORT_READ = 522, IOERR_WRITE = 778;
const ROW = 100, DONE = 101;
const LOCK_NONE = 0, LOCK_SHARED = 1, LOCK_RESERVED = 2, LOCK_PENDING = 3, LOCK_EXCLUSIVE = 4;
const AUTH_DELETE = 9, AUTH_INSERT = 18, AUTH_READ = 20, AUTH_UPDATE = 23;
const NULL = 5, INTEGER = 1, FLOAT = 2, TEXT = 3;

let loading = null;

/**
 * load loads SQLite from base (the directory of sqlite3.mjs and
 * sqlite3.wasm) once, and installs the file system it keeps databases in.
 */
export function load(base) {
  loading ??= (async () => {
    // Only the VFS of this file: the others (OPFS's) need a worker.
    globalThis.sqlite3ApiConfig = {
      warn: () => {}, log: () => {}, debug: () => {},
      error: console.error.bind(console),
      disable: { vfs: { kvvfs: true, opfs: true, "opfs-vfs": true, "opfs-sahpool": true, "opfs-wl": true } },
    };
    const wasm = fetch(new URL("sqlite3.wasm", base), { credentials: "same-origin" });
    const { default: init } = await import(new URL("sqlite3.mjs", base).href);
    let failed;
    const failure = new Promise((_, reject) => { failed = reject; });
    const sqlite3 = await Promise.race([failure, init({
      print: () => {},
      printErr: (s) => console.error("sqlite:", s),
      // Fetched at once, and loaded whatever type the server says it is.
      instantiateWasm(imports, ok) {
        wasm
          .then((r) => {
            if (!r.ok) throw new Error(`${r.url}: ${r.status} ${r.statusText}`);
            return WebAssembly.instantiateStreaming(r.clone(), imports).catch(() => r.arrayBuffer().then((b) => WebAssembly.instantiate(b, imports)));
          })
          .then((r) => ok(r.instance, r.module), failed);
        return {};
      },
    })]);
    delete globalThis.sqlite3ApiConfig;
    return new Engine(sqlite3);
  })();
  return loading;
}

/** Engine is SQLite on the page, shared by its apps. */
export class Engine {
  constructor(sqlite3) {
    this.sqlite3 = sqlite3;
    this.x = sqlite3.wasm.exports;
    this.conns = new Map(); // id -> Conn
    this.nextConn = 1;
    this.locks = new Map(); // path -> { shared, writer }
    this.files = new Map(); // sqlite3_file* -> { path, flags, lock, temp }
    this.installVFS();
  }

  heap() { return this.sqlite3.wasm.heap8u(); }

  /** api is an app's entry point: its Go program's memory is mem() (a WebAssembly.Memory). */
  api(mem) {
    let pending = null;
    const out = new Writer();
    return {
      call: (op, conn, arg, inPtr, inLen, outPtr, outCap) => {
        const memory = mem();
        if (op === TAKE) {
          new Uint8Array(memory.buffer, outPtr, pending.length).set(pending);
          const n = pending.length;
          pending = null;
          return n;
        }
        const input = new Uint8Array(memory.buffer, inPtr, inLen);
        out.reset();
        this.run(op, conn, arg, input, out);
        const b = out.bytes();
        if (b.length > outCap) {
          pending = b.slice();
          return -b.length;
        }
        new Uint8Array(memory.buffer, outPtr, b.length).set(b);
        return b.length;
      },
    };
  }

  /** run does op and writes its answer: u8 ok, then the tables written and read, then op's own. */
  run(op, id, arg, input, w) {
    const c = this.conns.get(id);
    const r = new Reader(input);
    const flags = op === OPEN || op === CLOSE ? 0 : r.u8();
    if (c) c.reads = flags & 1 ? new Set() : null;
    const mark = w.length;
    w.u8(1);
    const tables = w.length;
    w.u32(0).u32(0); // filled in below
    try {
      switch (op) {
        case OPEN: w.u32(this.open(r.string())); break;
        case CLOSE: this.close(c); break;
        case EXEC: this.exec(need(c), r.string(), args(r), w); break;
        case QUERY: this.query(need(c), r.string(), args(r), w); break;
        case NEXT: this.fill(need(c), need(c.cursors.get(arg)), w); break;
        case CLOSE_ROWS: if (c) this.closeRows(c, arg); break;
      }
    } catch (e) {
      w.truncate(mark);
      w.u8(0);
      w.u32(0).u32(0);
      w.u32(e.code ?? ERROR).u8(c && this.x.sqlite3_get_autocommit(c.db) ? 1 : 0).string(String(e.message ?? e));
    }
    // The tables the call's commits wrote, and those its queries read.
    if (c && (c.committed.size || c.reads?.size)) {
      const body = w.take(tables + 8);
      w.truncate(tables);
      w.strings(c.committed).strings(c.reads ?? []);
      w.raw(body);
      c.committed.clear();
    }
    if (c) c.reads = null;
  }

  // --- Connections ---

  open(path) {
    const { capi, wasm } = this.sqlite3;
    const memory = path === ":memory:";
    const st = wasm.pstack.pointer;
    try {
      const pp = wasm.pstack.allocPtr();
      const flags = capi.SQLITE_OPEN_READWRITE | capi.SQLITE_OPEN_CREATE | capi.SQLITE_OPEN_URI;
      const rc = capi.sqlite3_open_v2(memory ? ":memory:" : fs.file.clean(path), pp, flags, memory ? null : "asuka");
      const db = wasm.peekPtr(pp);
      if (rc) {
        const msg = db ? capi.sqlite3_errmsg(db) : capi.sqlite3_errstr(rc);
        if (db) capi.sqlite3_close_v2(db);
        throw sqliteError(rc, `${capi.sqlite3_errstr(rc)}: ${msg}`);
      }
      const c = new Conn(this, db);
      const id = this.nextConn++;
      this.conns.set(id, c);
      return id;
    } finally {
      wasm.pstack.restore(st);
    }
  }

  close(c) {
    if (!c) return;
    const { capi } = this.sqlite3;
    for (const id of [...c.cursors.keys()]) this.closeRows(c, id);
    for (const list of c.cache.values()) for (const s of list) this.x.sqlite3_finalize(s.ptr);
    c.cache.clear();
    capi.sqlite3_set_authorizer(c.db, 0, 0);
    capi.sqlite3_commit_hook(c.db, 0, 0);
    capi.sqlite3_rollback_hook(c.db, 0, 0);
    capi.sqlite3_preupdate_hook?.(c.db, 0, 0);
    capi.sqlite3_close_v2(c.db);
    for (const [id, x] of this.conns) if (x === c) this.conns.delete(id);
  }

  // --- Statements ---

  /**
   * statements runs f on each statement of sql in turn (prepared, or the
   * one cached for it), until f returns true; each is reset (and put back
   * in the cache) or finalized after, unless f gave it to a cursor, which
   * does that as it closes.
   */
  statements(c, sql, f) {
    const x = this.x, { wasm } = this.sqlite3;
    const cached = c.cache.get(sql)?.pop();
    if (cached) {
      cached.reused();
      const keep = f(cached);
      if (!cached.cursor) c.release(cached);
      return keep;
    }
    const src = new TextEncoder().encode(sql);
    const p = wasm.alloc(src.length + 1);
    const st = wasm.pstack.pointer;
    try {
      this.heap().set(src, p);
      this.heap()[p + src.length] = 0;
      const end = p + src.length;
      const ppStmt = wasm.pstack.allocPtr(), ppTail = wasm.pstack.allocPtr();
      let at = p, first = true;
      while (at < end) {
        const s = new Stmt(c, sql);
        c.preparing = s;
        // Persistent: kept in the cache, prepared once.
        const rc = x.sqlite3_prepare_v3(c.db, at, end - at, 1, ppStmt, ppTail);
        c.preparing = null;
        if (rc) throw this.error(c, rc);
        s.ptr = wasm.peekPtr(ppStmt);
        const tail = wasm.peekPtr(ppTail);
        if (!s.ptr) break; // only space or comments left
        // A statement alone is cached; a script's are prepared each time.
        s.cache = first && blank(this.heap(), tail, end);
        first = false;
        at = tail;
        let stop;
        try {
          stop = f(s);
        } finally {
          if (!s.cursor) s.cache ? c.release(s) : x.sqlite3_finalize(s.ptr);
        }
        if (stop) return true;
      }
      return false;
    } finally {
      wasm.pstack.restore(st);
      wasm.dealloc(p);
    }
  }

  /** bind binds args to s's parameters, as modernc.org/sqlite does: ?NNN, $NNN and the n-th by number, :name, @name and $name by name. */
  bind(s, args) {
    const x = this.x, { wasm } = this.sqlite3;
    const n = x.sqlite3_bind_parameter_count(s.ptr);
    for (let i = 1; i <= n; i++) {
      const pName = x.sqlite3_bind_parameter_name(s.ptr, i);
      const name = pName ? wasm.cstrToJs(pName) : "";
      let ord = i;
      if (name) {
        ord = 0;
        if ((name[0] === "?" || name[0] === "$") && /^[1-9][0-9]*$/.test(name.slice(1))) ord = Number(name.slice(1));
      }
      let j = ord >= 1 && ord <= args.length ? ord - 1 : -1;
      if (name) {
        const k = args.findIndex((a) => a.name !== "" && a.name === name.slice(1));
        if (k >= 0 && (j < 0 || k < j)) j = k;
      }
      if (j < 0) throw sqliteError(ERROR, name ? `missing named argument "${name.slice(1)}"` : `missing argument with index ${i}`);
      const a = args[j];
      let rc;
      switch (a.type) {
        case 0: rc = x.sqlite3_bind_null(s.ptr, i); break;
        case 1: rc = x.sqlite3_bind_int64(s.ptr, i, a.value); break;
        case 2: rc = x.sqlite3_bind_double(s.ptr, i, a.value); break;
        case 3: case 4: {
          const b = a.value;
          const p = wasm.alloc(Math.max(1, b.length));
          this.heap().set(b, p);
          // SQLITE_WASM_DEALLOC: SQLite frees it.
          rc = a.type === 3
            ? x.sqlite3_bind_text(s.ptr, i, p, b.length, this.sqlite3.capi.SQLITE_WASM_DEALLOC)
            : x.sqlite3_bind_blob(s.ptr, i, p, b.length, this.sqlite3.capi.SQLITE_WASM_DEALLOC);
          break;
        }
      }
      if (rc) throw this.error(s.c, rc);
    }
  }

  step(s) {
    s.c.current = s;
    try {
      return this.x.sqlite3_step(s.ptr);
    } finally {
      s.c.current = null;
    }
  }

  exec(c, sql, args, w) {
    this.statements(c, sql, (s) => {
      this.bind(s, args);
      let rc;
      while ((rc = this.step(s)) === ROW);
      if (rc !== DONE) throw this.error(c, rc);
      return false;
    });
    w.i64(this.x.sqlite3_last_insert_rowid(c.db)).i64(this.x.sqlite3_changes64(c.db));
  }

  query(c, sql, args, w) {
    const x = this.x, { wasm } = this.sqlite3;
    let first = null;
    const columns = (s) => {
      const n = x.sqlite3_column_count(s.ptr);
      const out = [];
      for (let i = 0; i < n; i++) {
        const name = x.sqlite3_column_name(s.ptr, i), decl = x.sqlite3_column_decltype(s.ptr, i);
        out.push([name ? wasm.cstrToJs(name) : "", decl ? wasm.cstrToJs(decl) : ""]);
      }
      return out;
    };
    const found = this.statements(c, sql, (s) => {
      this.bind(s, args);
      const rc = this.step(s);
      if (rc === ROW) {
        const id = c.nextCursor++;
        const cur = { id, s, n: x.sqlite3_column_count(s.ptr), step: false };
        s.cursor = true;
        c.cursors.set(id, cur);
        w.u32(id);
        writeColumns(w, columns(s));
        this.fill(c, cur, w);
        return true;
      }
      if (rc !== DONE) throw this.error(c, rc);
      first ??= columns(s);
      return false;
    });
    if (!found) {
      w.u32(0);
      writeColumns(w, first ?? []);
      w.u8(1).u32(0); // done, no rows
    }
  }

  /** fill writes the cursor's next rows: u8 done, u32 rows, then each row's values; up to about 256 KiB of them. */
  fill(c, cur, w) {
    const x = this.x, s = cur.s;
    const doneAt = w.length;
    w.u8(0).u32(0);
    const start = w.length;
    let rows = 0, done = false;
    for (;;) {
      if (cur.step) {
        const rc = this.step(s);
        if (rc === DONE) { done = true; break; }
        if (rc !== ROW) {
          this.closeRows(c, cur.id);
          throw this.error(c, rc);
        }
      }
      cur.step = true;
      for (let i = 0; i < cur.n; i++) {
        switch (x.sqlite3_column_type(s.ptr, i)) {
          case NULL: w.u8(0); break;
          case INTEGER: w.u8(1).i64(x.sqlite3_column_int64(s.ptr, i)); break;
          case FLOAT: w.u8(2).f64(x.sqlite3_column_double(s.ptr, i)); break;
          case TEXT: {
            const p = x.sqlite3_column_text(s.ptr, i), n = x.sqlite3_column_bytes(s.ptr, i);
            w.u8(3).u32(n).raw(this.heap().subarray(p, p + n));
            break;
          }
          default: {
            const p = x.sqlite3_column_blob(s.ptr, i), n = x.sqlite3_column_bytes(s.ptr, i);
            w.u8(4).u32(n).raw(n ? this.heap().subarray(p, p + n) : new Uint8Array(0));
          }
        }
      }
      rows++;
      if (w.length - start > 256 * 1024) break;
    }
    w.setU8(doneAt, done ? 1 : 0).setU32(doneAt + 1, rows);
    if (done) this.closeRows(c, cur.id);
  }

  closeRows(c, id) {
    const cur = c.cursors.get(id);
    if (!cur) return;
    c.cursors.delete(id);
    cur.s.cache ? c.release(cur.s) : this.x.sqlite3_finalize(cur.s.ptr);
  }

  error(c, rc) {
    const { capi } = this.sqlite3;
    const str = capi.sqlite3_errstr(rc);
    const msg = capi.sqlite3_errmsg(c.db);
    const busy = (rc & 0xff) === BUSY ? " (SQLITE_BUSY)" : "";
    // As modernc.org/sqlite says it, so an app's errors read the same on every platform.
    return sqliteError(rc, msg === str ? `${str} (${rc})${busy}` : `${str}: ${msg} (${rc})${busy}`);
  }

  // --- The file system SQLite keeps databases in: fs.js's ---

  installVFS() {
    const { capi, wasm } = this.sqlite3;
    const files = this.files;
    const eng = this;
    const fileOf = (p) => files.get(p);
    const lockOf = (path) => {
      let l = this.locks.get(path);
      if (!l) this.locks.set(path, (l = { shared: 0, writer: null }));
      return l;
    };
    const io = {
      xClose(p) {
        const f = fileOf(p);
        if (f) {
          io.xUnlock(p, LOCK_NONE);
          files.delete(p);
          if (f.temp) fs.file.remove(f.path);
        }
        return 0;
      },
      xRead(p, dst, n, offset) {
        const f = fileOf(p);
        const buf = eng.heap().subarray(dst, dst + n);
        const got = fs.file.read(f.path, buf, Number(offset));
        if (got < n) {
          buf.fill(0, got);
          return IOERR_SHORT_READ;
        }
        return 0;
      },
      xWrite(p, src, n, offset) {
        const f = fileOf(p);
        try {
          fs.file.write(f.path, eng.heap().subarray(src, src + n), Number(offset));
          return 0;
        } catch {
          return IOERR_WRITE;
        }
      },
      xTruncate(p, size) { fs.file.truncate(fileOf(p).path, Number(size)); return 0; },
      // The page's file system has nothing to flush: fs.js keeps what's
      // written, in IndexedDB, a moment later, all at once.
      xSync() { return 0; },
      xFileSize(p, out) { wasm.poke64(out, BigInt(fs.file.size(fileOf(p).path))); return 0; },
      // Locks between this page's connections (a browser tab's databases
      // are its own), as SQLite's unix VFS takes them between processes.
      xLock(p, level) {
        const f = fileOf(p);
        if (f.lock >= level) return 0;
        const l = lockOf(f.path);
        if (level === LOCK_SHARED) {
          if (l.writer && l.writer !== f && l.writer.lock >= LOCK_PENDING) return BUSY;
          l.shared++;
          f.lock = LOCK_SHARED;
          return 0;
        }
        if (l.writer && l.writer !== f) return BUSY;
        l.writer = f;
        if (level === LOCK_RESERVED) {
          f.lock = LOCK_RESERVED;
          return 0;
        }
        // Exclusive: pending until the other readers are gone.
        f.lock = LOCK_PENDING;
        if (l.shared > 1) return BUSY;
        f.lock = LOCK_EXCLUSIVE;
        return 0;
      },
      xUnlock(p, level) {
        const f = fileOf(p);
        if (!f || f.lock <= level) return 0;
        const l = lockOf(f.path);
        if (f.lock >= LOCK_RESERVED && level < LOCK_RESERVED && l.writer === f) l.writer = null;
        if (level === LOCK_NONE) l.shared--;
        f.lock = level;
        if (!l.shared && !l.writer) eng.locks.delete(f.path);
        return 0;
      },
      xCheckReservedLock(p, out) {
        const l = eng.locks.get(fileOf(p).path);
        wasm.poke32(out, l?.writer && l.writer.lock >= LOCK_RESERVED ? 1 : 0);
        return 0;
      },
      xFileControl() { return NOTFOUND; },
      xSectorSize() { return 4096; },
      // Each write lands whole and in order (a page's code runs to its
      // end): what's kept is a state between two of SQLite's calls.
      xDeviceCharacteristics() {
        return capi.SQLITE_IOCAP_SAFE_APPEND | capi.SQLITE_IOCAP_SEQUENTIAL | capi.SQLITE_IOCAP_POWERSAFE_OVERWRITE | capi.SQLITE_IOCAP_UNDELETABLE_WHEN_OPEN;
      },
    };
    const ioStruct = new capi.sqlite3_io_methods();
    ioStruct.$iVersion = 1;
    let temps = 0;
    const vfs = {
      xOpen(pVfs, zName, pFile, flags, pOutFlags) {
        let path, temp = false;
        if (zName && wasm.peek8(zName)) path = fs.file.clean(wasm.cstrToJs(zName));
        else {
          path = `/tmp/sqlite-${Date.now().toString(36)}-${++temps}`;
          temp = true;
        }
        if (flags & capi.SQLITE_OPEN_DELETEONCLOSE) temp = true;
        const create = !!(flags & capi.SQLITE_OPEN_CREATE);
        if (create && flags & capi.SQLITE_OPEN_EXCLUSIVE && fs.file.exists(path)) return CANTOPEN;
        if (!fs.file.open(path, create)) return CANTOPEN;
        files.set(pFile, { path, flags, lock: LOCK_NONE, temp });
        const f = new capi.sqlite3_file(pFile);
        f.$pMethods = ioStruct.pointer;
        f.dispose();
        if (pOutFlags) wasm.poke32(pOutFlags, flags);
        return 0;
      },
      xDelete(pVfs, zName) { fs.file.remove(fs.file.clean(wasm.cstrToJs(zName))); return 0; },
      xAccess(pVfs, zName, flags, pOut) { wasm.poke32(pOut, fs.file.exists(fs.file.clean(wasm.cstrToJs(zName))) ? 1 : 0); return 0; },
      xFullPathname(pVfs, zName, nOut, pOut) {
        const b = new TextEncoder().encode(fs.file.clean(wasm.cstrToJs(zName)));
        if (b.length + 1 > nOut) return CANTOPEN;
        eng.heap().set(b, pOut);
        eng.heap()[pOut + b.length] = 0;
        return 0;
      },
      xRandomness(pVfs, n, pOut) {
        crypto.getRandomValues(eng.heap().subarray(pOut, pOut + n));
        return n;
      },
      // The page can't wait: a connection that finds the database locked
      // returns SQLITE_BUSY at once, and Go tries again once it has let the
      // other run (driver_js.go).
      xSleep() { return 0; },
      xCurrentTime(pVfs, pOut) { wasm.poke(pOut, 2440587.5 + Date.now() / 86400000, "double"); return 0; },
      xCurrentTimeInt64(pVfs, pOut) { wasm.poke(pOut, 2440587.5 * 86400000 + Date.now(), "i64"); return 0; },
      xGetLastError() { return 0; },
    };
    const vfsStruct = new capi.sqlite3_vfs();
    vfsStruct.$iVersion = 2;
    vfsStruct.$szOsFile = capi.sqlite3_file.structInfo.sizeof;
    vfsStruct.$mxPathname = 1024;
    this.sqlite3.vfs.installVfs({ io: { struct: ioStruct, methods: io }, vfs: { struct: vfsStruct, methods: vfs, name: "asuka" } });
  }
}

/** Conn is a connection: its statements cached by SQL, its open cursors, and the tables it writes and reads. */
class Conn {
  constructor(engine, db) {
    this.engine = engine;
    this.db = db;
    this.cache = new Map(); // sql -> idle Stmts
    this.cached = 0;
    this.cursors = new Map();
    this.nextCursor = 1;
    this.written = new Set(); // by the transaction going on
    this.committed = new Set(); // since the last answer
    this.reads = null; // recording: the tables read
    this.preparing = null; // the Stmt being prepared
    this.current = null; // the Stmt stepping (SQLite may prepare it again)
    const { capi } = engine.sqlite3;
    // SQLite asks as it prepares a statement: the tables it reads and
    // writes (a view's too; and a delete of every row, which SQLite may do
    // without visiting them).
    capi.sqlite3_set_authorizer(db, (_, code, a) => {
      if (!a || a.startsWith("sqlite_")) return 0;
      const s = this.preparing ?? this.current;
      if (code === AUTH_READ) {
        s?.reads.add(a);
        this.reads?.add(a);
      } else if (code === AUTH_INSERT || code === AUTH_UPDATE || code === AUTH_DELETE) {
        s?.writes.add(a);
        this.written.add(a);
      }
      return 0;
    }, 0);
    // Every row written, by triggers and foreign keys' actions too.
    capi.sqlite3_preupdate_hook?.(db, (_p, _db, _op, _zDb, table) => { this.written.add(table); }, 0);
    capi.sqlite3_commit_hook(db, () => {
      for (const t of this.written) this.committed.add(t);
      this.written.clear();
      return 0;
    }, 0);
    capi.sqlite3_rollback_hook(db, () => { this.written.clear(); }, 0);
  }

  /** release resets s and keeps it for its SQL's next run (finalized if the cache is full). */
  release(s) {
    const x = this.engine.x;
    x.sqlite3_reset(s.ptr);
    x.sqlite3_clear_bindings(s.ptr);
    if (this.cached >= 64) {
      // The oldest SQL's statements go.
      const [sql, list] = this.cache.entries().next().value;
      for (const t of list) x.sqlite3_finalize(t.ptr);
      this.cached -= list.length;
      this.cache.delete(sql);
    }
    let list = this.cache.get(s.sql);
    if (list) this.cache.delete(s.sql); // to the end: the newest
    else list = [];
    list.push(s);
    this.cache.set(s.sql, list);
    this.cached++;
  }
}

class Stmt {
  constructor(c, sql) {
    this.c = c;
    this.sql = sql;
    this.ptr = 0;
    this.cache = false;
    this.cursor = false; // a cursor's: it gives the statement back
    this.reads = new Set(); // what it read and wrote as SQLite prepared it
    this.writes = new Set();
  }

  /** reused notes a cached statement's tables, as preparing it again would. */
  reused() {
    const c = this.c;
    c.cached--;
    this.cursor = false;
    for (const t of this.writes) c.written.add(t);
    if (c.reads) for (const t of this.reads) c.reads.add(t);
  }
}

function need(v) {
  if (!v) throw sqliteError(ERROR, "sqlite: closed");
  return v;
}

function sqliteError(code, message) {
  const e = new Error(message);
  e.code = code;
  return e;
}

/** blank reports whether heap[from:to] is only space and semicolons. */
function blank(heap, from, to) {
  for (let i = from; i < to; i++) {
    const c = heap[i];
    if (c !== 32 && c !== 9 && c !== 10 && c !== 13 && c !== 59) return false;
  }
  return true;
}

function writeColumns(w, cols) {
  w.u32(cols.length);
  for (const [name, decl] of cols) w.string(name).string(decl);
}

/** args reads a call's arguments: u32 count, then each: its name (string), its type and value. */
function args(r) {
  const n = r.u32();
  const out = [];
  for (let i = 0; i < n; i++) {
    const name = r.string();
    const type = r.u8();
    let value = null;
    switch (type) {
      case 1: value = r.i64(); break;
      case 2: value = r.f64(); break;
      case 3: case 4: value = r.bytes(r.u32()); break;
    }
    out.push({ name, type, value });
  }
  return out;
}

const dec = new TextDecoder();
const enc = new TextEncoder();

export class Reader {
  constructor(b) {
    this.b = b;
    this.v = new DataView(b.buffer, b.byteOffset, b.byteLength);
    this.p = 0;
  }
  u8() { return this.b[this.p++]; }
  u32() { const v = this.v.getUint32(this.p, true); this.p += 4; return v; }
  i64() { const v = this.v.getBigInt64(this.p, true); this.p += 8; return v; }
  f64() { const v = this.v.getFloat64(this.p, true); this.p += 8; return v; }
  bytes(n) { const b = this.b.subarray(this.p, this.p + n); this.p += n; return b; }
  string() { return dec.decode(this.bytes(this.u32())); }
}

/** Writer is a growing buffer, kept from call to call. */
export class Writer {
  constructor() {
    this.b = new Uint8Array(4096);
    this.v = new DataView(this.b.buffer);
    this.length = 0;
  }
  reset() { this.length = 0; }
  need(n) {
    if (this.length + n <= this.b.length) return;
    const b = new Uint8Array(Math.max(this.length + n, this.b.length * 2));
    b.set(this.b.subarray(0, this.length));
    this.b = b;
    this.v = new DataView(b.buffer);
  }
  u8(x) { this.need(1); this.b[this.length++] = x; return this; }
  u32(x) { this.need(4); this.v.setUint32(this.length, x, true); this.length += 4; return this; }
  i64(x) { this.need(8); this.v.setBigInt64(this.length, BigInt.asIntN(64, BigInt(x)), true); this.length += 8; return this; }
  f64(x) { this.need(8); this.v.setFloat64(this.length, x, true); this.length += 8; return this; }
  raw(b) { this.need(b.length); this.b.set(b, this.length); this.length += b.length; return this; }
  string(s) { const b = enc.encode(s); return this.u32(b.length).raw(b); }
  strings(set) { this.u32(set.size ?? set.length); for (const s of set) this.string(s); return this; }
  setU8(at, x) { this.b[at] = x; return this; }
  setU32(at, x) { this.v.setUint32(at, x, true); return this; }
  truncate(n) { this.length = n; }
  take(from) { return this.b.slice(from, this.length); }
  bytes() { return this.b.subarray(0, this.length); }
}

// The files SQLite keeps databases in on the web: what sqlite-engine.js
// gives asq_wasm.c's VFS, in the page's SQLite worker.
//
// A database in an app's data directory is kept (a store: the origin
// private file system, else IndexedDB, sqlite-store.js); any other one
// (under /tmp, say) is the page's, in memory, as the page's own files
// there are. A database and its rollback journal are a pair: one store,
// one lock.
//
// Several tabs of an app may open one kept database. A tab uses it only
// while it holds the database's Web Lock, which it keeps until another
// tab asks for it (on a BroadcastChannel) and it isn't in a transaction
// (wa-sqlite's OPFSCoopSyncVFS works the same way). A statement that
// needs the database while another tab holds it gets SQLITE_BUSY at once
// and starts asking for the lock: Go runs it again (sqlite/conn.go's
// retry) until the lock comes or the busy timeout passes.
//
// Between this page's own connections to one database, the VFS locks as
// SQLite's unix VFS does between processes: readers share, one writer,
// and the writer waits for the readers to go before it writes.

// SQLite's codes.
const OK = 0, BUSY = 5, CANTOPEN = 14;
const IOERR_READ = 266, IOERR_SHORT_READ = 522, IOERR_WRITE = 778, IOERR_FSYNC = 1034, IOERR_TRUNCATE = 1546,
  IOERR_FSTAT = 1802, IOERR_DELETE = 2570, IOERR_LOCK = 3850, IOERR_CLOSE = 4106;
const OPEN_CREATE = 0x4, OPEN_DELETEONCLOSE = 0x8, OPEN_MAIN_DB = 0x100, OPEN_MAIN_JOURNAL = 0x800;
const NONE = 0, SHARED = 1, RESERVED = 2, PENDING = 3, EXCLUSIVE = 4;

/** LOCK_PREFIX names a kept database's Web Lock and BroadcastChannel, with its path. */
export const LOCK_PREFIX = "asuka-sqlite:";

/** MemoryFile is a file's bytes in memory. */
export class MemoryFile {
  constructor(data = new Uint8Array(0)) {
    this.data = data;
    this.n = data.length;
  }
  read(dst, off) {
    if (off >= this.n) return 0;
    const k = Math.min(dst.length, this.n - off);
    dst.set(this.data.subarray(off, off + k));
    return k;
  }
  write(src, off) {
    const end = off + src.length;
    if (end > this.data.length) {
      const b = new Uint8Array(Math.max(end, this.data.length * 2, 4096));
      b.set(this.data.subarray(0, this.n));
      this.data = b;
    }
    if (off > this.n) this.data.fill(0, this.n, off);
    this.data.set(src, off);
    this.n = Math.max(this.n, end);
    return src.length;
  }
  truncate(n) {
    if (n > this.n) this.write(new Uint8Array(n - this.n), this.n);
    this.n = n;
  }
  size() { return this.n; }
  flush() {}
  /** bytes is a copy of the file's bytes. */
  bytes() { return this.data.slice(0, this.n); }
}

/**
 * Database is a database file and its journal, in this worker: its
 * store (null: in memory), its locks between this page's connections,
 * and, kept, whether this page holds it.
 */
class Database {
  constructor(vfs, path, store) {
    this.vfs = vfs;
    this.path = path;
    this.store = store;
    this.mem = store ? null : { "": new MemoryFile(), "-journal": new MemoryFile() };
    this.ready = !store; // the store prepared
    this.prepared = null;
    this.shared = 0; // this page's connections that read (SHARED or more)
    this.writer = null; // the file of the one that writes (RESERVED or more)
    this.held = !store; // this page holds the database (its Web Lock)
    this.acquiring = null;
    this.unlock = null; // releases the Web Lock
    this.wanted = false; // another tab asked for it
    this.failure = null; // why getting it last failed
    this.channel = null;
    if (store && vfs.BroadcastChannel) {
      this.channel = new vfs.BroadcastChannel(LOCK_PREFIX + path);
      this.channel.onmessage = (e) => {
        if (e.data?.want !== path || !this.held) return;
        if (this.idle()) this.release();
        else this.wanted = true;
      };
    }
  }

  /** file is the database's (suffix "") or its journal's ("-journal") bytes, while this page may use them; else null. */
  file(suffix) {
    if (!this.store) return this.mem[suffix];
    return this.held ? this.store.file(suffix) : null;
  }

  idle() { return this.shared === 0 && !this.writer; }

  /** acquire gets the database for this page: its Web Lock (asking the tab that holds it), then its store's files. */
  acquire() {
    if (this.held) return Promise.resolve();
    if (this.acquiring) return this.acquiring;
    this.failure = null;
    const run = async () => {
      const ask = () => this.channel?.postMessage({ want: this.path });
      ask();
      const timer = setInterval(ask, 1000);
      let release;
      try {
        await new Promise((granted, failed) => {
          this.vfs.locks.request(LOCK_PREFIX + this.path, () => new Promise((r) => { release = r; granted(); })).catch(failed);
        });
      } finally {
        clearInterval(timer);
      }
      try {
        await this.store.open();
      } catch (e) {
        release();
        throw e;
      }
      this.unlock = release;
      this.held = true;
      this.wanted = false;
    };
    this.acquiring = run().then(
      () => { this.acquiring = null; },
      (e) => {
        this.acquiring = null;
        this.failure = e;
        throw e;
      },
    );
    return this.acquiring;
  }

  /** release gives the database up (its files closed, written), for another tab. */
  async release() {
    if (!this.held || !this.store) return;
    this.held = false;
    this.wanted = false;
    try {
      await this.store.close();
    } catch (e) {
      console.warn("asuka/sqlite:", this.path, e);
    } finally {
      const unlock = this.unlock;
      this.unlock = null;
      unlock?.();
    }
  }
}

/**
 * VFS is the files of the worker's databases, by the ids asq_wasm.c
 * knows them by. options: kept(path) says whether a database is kept;
 * store(path) makes its store; locks (navigator.locks) and
 * BroadcastChannel, which tests replace.
 */
export class VFS {
  constructor({ kept = () => false, store = null, locks = globalThis.navigator?.locks, BroadcastChannel = globalThis.BroadcastChannel } = {}) {
    this.kept = kept;
    this.store = store;
    this.locks = locks ?? localLocks();
    this.BroadcastChannel = BroadcastChannel;
    this.databases = new Map(); // path -> Database
    this.temps = new Map(); // path -> MemoryFile: other files SQLite names
    this.files = new Map(); // id -> { path, flags, db, suffix, mem, lock }
    this.nextID = 1;
    this.lastError = null;
  }

  /**
   * prepare readies a kept database for a connection to open it: its
   * store, and the database held by this page. migrate, if given, is its
   * bytes from where an earlier version kept it ({ db, journal }), copied
   * in if the store's database is empty.
   */
  async prepare(path, migrate) {
    if (!this.kept(path) || !this.store) return;
    let db = this.databases.get(path);
    if (!db) {
      db = new Database(this, path, this.store(path));
      this.databases.set(path, db);
      db.prepared = db.store.prepare().then(
        () => { db.ready = true; },
        (e) => {
          this.databases.delete(path);
          db.channel?.close();
          throw e;
        },
      );
    }
    await db.prepared;
    await db.acquire();
    if (migrate?.db?.length) {
      const main = db.file(""), journal = db.file("-journal");
      if (main && main.size() === 0) {
        // A database in write-ahead logging says so in its header (bytes
        // 18 and 19), which needs shared memory this VFS hasn't: it's
        // read in the rollback journal's mode, as Restore does.
        const b = migrate.db;
        if (b.length >= 100 && b[18] === 2 && b[19] === 2) b[18] = b[19] = 1;
        main.write(b, 0);
        main.flush();
        if (migrate.journal?.length) {
          journal.truncate(0);
          journal.write(migrate.journal, 0);
          journal.flush();
        }
        db.store.afterCall?.();
        return true;
      }
    }
    return false;
  }

  /** afterCall is what's done once a call returns: kept databases written, and given to the tabs that asked for them. */
  afterCall() {
    for (const db of this.databases.values()) {
      if (!db.store) continue;
      if (db.held) db.store.afterCall?.();
      if (db.held && db.wanted && db.idle()) db.release();
    }
  }

  /** shutdown gives up every database this page holds, and stops listening for other tabs. */
  async shutdown() {
    for (const db of this.databases.values()) {
      db.channel?.close();
      await db.release();
    }
    this.databases.clear();
  }

  fail(code, e) {
    this.lastError = e;
    if (e) console.warn("asuka/sqlite:", e);
    return code;
  }

  data(f) { return f.mem ?? f.db.file(f.suffix); }

  // --- What asq_wasm.c calls ---

  open(path, flags) {
    try {
      const f = { path, flags, db: null, suffix: "", mem: null, lock: NONE };
      if (path && flags & OPEN_MAIN_DB) {
        let db = this.databases.get(path);
        if (!db) {
          if (this.kept(path) && this.store) {
            // Not prepared (an attached database): prepared meanwhile,
            // and the statement run again.
            this.prepare(path).catch((e) => { this.lastError = e; });
            return -BUSY;
          }
          db = new Database(this, path, null);
          this.databases.set(path, db);
        }
        if (!db.ready) return -BUSY;
        f.db = db;
      } else if (path && flags & OPEN_MAIN_JOURNAL && journalOf(this, path)) {
        f.db = journalOf(this, path);
        f.suffix = "-journal";
      } else if (path) {
        let m = this.temps.get(path);
        if (!m) {
          if (!(flags & OPEN_CREATE)) return -CANTOPEN;
          m = new MemoryFile();
          this.temps.set(path, m);
        }
        f.mem = m;
      } else {
        f.mem = new MemoryFile(); // a temporary file
      }
      const id = this.nextID++;
      this.files.set(id, f);
      return id;
    } catch (e) {
      return -this.fail(CANTOPEN, e);
    }
  }

  close(id) {
    const f = this.files.get(id);
    if (!f) return OK;
    try {
      if (f.lock) this.unlock(id, NONE);
      this.files.delete(id);
      if (f.path && f.mem && f.flags & OPEN_DELETEONCLOSE) this.temps.delete(f.path);
      return OK;
    } catch (e) {
      return this.fail(IOERR_CLOSE, e);
    }
  }

  read(id, dst, off) {
    try {
      const d = this.data(this.files.get(id));
      if (!d) return this.fail(IOERR_READ, new Error("read without the database's lock"));
      const n = d.read(dst, off);
      if (n < dst.length) {
        dst.fill(0, n);
        return IOERR_SHORT_READ;
      }
      return OK;
    } catch (e) {
      return this.fail(IOERR_READ, e);
    }
  }

  write(id, src, off) {
    try {
      const d = this.data(this.files.get(id));
      if (!d) return this.fail(IOERR_WRITE, new Error("write without the database's lock"));
      if (d.write(src, off) !== src.length) return this.fail(IOERR_WRITE, new Error("short write"));
      return OK;
    } catch (e) {
      return this.fail(IOERR_WRITE, e);
    }
  }

  truncate(id, size) {
    try {
      const d = this.data(this.files.get(id));
      if (!d) return this.fail(IOERR_TRUNCATE, new Error("truncate without the database's lock"));
      d.truncate(size);
      return OK;
    } catch (e) {
      return this.fail(IOERR_TRUNCATE, e);
    }
  }

  sync(id) {
    try {
      const d = this.data(this.files.get(id));
      if (!d) return this.fail(IOERR_FSYNC, new Error("sync without the database's lock"));
      d.flush();
      return OK;
    } catch (e) {
      return this.fail(IOERR_FSYNC, e);
    }
  }

  size(id) {
    try {
      const d = this.data(this.files.get(id));
      if (!d) {
        this.fail(IOERR_FSTAT, new Error("size without the database's lock"));
        return -1;
      }
      return d.size();
    } catch (e) {
      this.fail(IOERR_FSTAT, e);
      return -1;
    }
  }

  lock(id, level) {
    const f = this.files.get(id);
    if (!f?.db || f.lock >= level) return OK;
    const db = f.db;
    if (!db.held) {
      if (db.failure) {
        // Getting it failed (its files couldn't open): said once, and
        // tried again by the next statement.
        const e = db.failure;
        db.failure = null;
        return this.fail(IOERR_LOCK, e);
      }
      db.acquire().catch(() => {});
      return BUSY;
    }
    if (level === SHARED) {
      if (db.writer && db.writer !== f && db.writer.lock >= PENDING) return BUSY;
      db.shared++;
      f.lock = SHARED;
      return OK;
    }
    if (db.writer && db.writer !== f) return BUSY;
    db.writer = f;
    if (level === RESERVED) {
      f.lock = RESERVED;
      return OK;
    }
    // Exclusive: pending until this page's other readers are done.
    f.lock = PENDING;
    if (db.shared > 1) return BUSY;
    f.lock = EXCLUSIVE;
    return OK;
  }

  unlock(id, level) {
    const f = this.files.get(id);
    if (!f?.db || f.lock <= level) return OK;
    const db = f.db;
    if (f.lock >= RESERVED && level < RESERVED && db.writer === f) db.writer = null;
    if (level === NONE) db.shared--;
    f.lock = level;
    return OK;
  }

  reserved(id) {
    const db = this.files.get(id)?.db;
    return db?.writer && db.writer.lock >= RESERVED ? 1 : 0;
  }

  remove(path) {
    try {
      const db = journalOf(this, path);
      if (db) {
        // The journal's file stays (a store keeps its handle): emptied,
        // which SQLite reads as none.
        const d = db.file("-journal");
        if (!d) return this.fail(IOERR_DELETE, new Error("delete without the database's lock"));
        d.truncate(0);
        d.flush();
        return OK;
      }
      this.temps.delete(path);
      return OK;
    } catch (e) {
      return this.fail(IOERR_DELETE, e);
    }
  }

  access(path) {
    try {
      const db = journalOf(this, path);
      if (db) {
        const d = db.file("-journal");
        return d && d.size() > 0 ? 1 : 0;
      }
      if (this.databases.has(path)) return 1;
      return this.temps.has(path) ? 1 : 0;
    } catch (e) {
      this.lastError = e;
      return 0;
    }
  }
}

/** journalOf is the database whose journal path is, or undefined. */
function journalOf(vfs, path) {
  return path.endsWith("-journal") ? vfs.databases.get(path.slice(0, -"-journal".length)) : undefined;
}

let warned = false;

/**
 * localLocks are Web Locks within this worker, for a page without them
 * (one that isn't a secure context: asuka dev --lan's): two tabs then
 * don't take turns, and may overwrite each other's writes.
 */
function localLocks() {
  if (!warned) {
    warned = true;
    console.warn("asuka/sqlite: this page has no Web Locks (it isn't a secure context): two tabs of the app writing one database may lose each other's writes");
  }
  const queues = new Map();
  return {
    request(name, f) {
      const prev = queues.get(name) ?? Promise.resolve();
      const run = prev.then(() => f({ name }));
      const next = run.catch(() => {});
      queues.set(name, next);
      return run;
    },
  };
}

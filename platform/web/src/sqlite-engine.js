// SQLite's WebAssembly module (the sqlite plugin's asqlite.wasm: SQLite
// and Asuka's shim, sqlite/csrc, built by internal/sqlitebuild), and the
// system it imports (sqlite/csrc/asq_wasm.c): its files are a VFS's
// (sqlite-vfs.js), its time the browser's, in the page's time zone, its
// randomness crypto.getRandomValues. It runs in the page's SQLite worker
// (sqlite-worker.js), and in Node for the tests.
//
// A call is asq_call's (sqlite/csrc/asq.h): a whole statement in, its
// answer out, written into the module's memory and copied out of it.

const TAKE = 7;

/** Engine is the module, instantiated, and its calls. */
export class Engine {
  /**
   * load instantiates the module (its bytes, a Response, or a compiled
   * WebAssembly.Module) with vfs as its files.
   */
  static async load(source, vfs) {
    const engine = new Engine(vfs);
    const imports = { asq: engine.imports() };
    let instance;
    if (source instanceof WebAssembly.Module) instance = await WebAssembly.instantiate(source, imports);
    else if (typeof Response !== "undefined" && source instanceof Response) {
      const r = source;
      if (!r.ok) throw new Error(`${r.url}: ${r.status} ${r.statusText}`);
      // A server that doesn't say application/wasm: compiled from its bytes.
      instance = await WebAssembly.instantiateStreaming(r.clone(), imports)
        .then((x) => x.instance)
        .catch(async () => (await WebAssembly.instantiate(await r.arrayBuffer(), imports)).instance);
    } else instance = (await WebAssembly.instantiate(source, imports)).instance;
    engine.attach(instance);
    return engine;
  }

  constructor(vfs) {
    this.vfs = vfs;
    this.x = null;
    this.memory = null;
    this.buf = null; // memory.buffer, when heap was made
    this.u8 = null;
    this.in = 0; // the input buffer, malloc'd
    this.inCap = 0;
    this.out = 0;
    this.outCap = 0;
  }

  attach(instance) {
    this.x = instance.exports;
    this.memory = this.x.memory;
    this.x._initialize?.();
  }

  /** heap is the module's memory as bytes (made again once it grows). */
  heap() {
    if (this.buf !== this.memory.buffer) {
      this.buf = this.memory.buffer;
      this.u8 = new Uint8Array(this.buf);
    }
    return this.u8;
  }

  /** version is SQLite's, as 3054001. */
  version() { return this.x.asq_version(); }

  /**
   * call is asq_call: its whole answer (outCap only sizes a query's
   * chunks of rows; a longer answer is taken whole, and the caller keeps
   * what doesn't fit its buffer).
   */
  call(op, conn, arg, input, outCap) {
    const x = this.x;
    if (input.length > this.inCap) {
      if (this.in) x.free(this.in);
      this.inCap = Math.max(input.length, 2 * this.inCap, 4096);
      this.in = x.malloc(this.inCap);
      if (!this.in) throw new Error("sqlite: out of memory");
    }
    if (outCap > this.outCap) {
      if (this.out) x.free(this.out);
      this.outCap = outCap;
      this.out = x.malloc(outCap);
      if (!this.out) throw new Error("sqlite: out of memory");
    }
    if (input.length) this.heap().set(input, this.in);
    let n = x.asq_call(op, conn, arg, this.in, input.length, this.out, outCap);
    if (n >= 0) return this.heap().slice(this.out, this.out + n);
    // Kept by the shim (OPEN's answer never is: it's short): taken whole.
    const p = x.malloc(-n);
    if (!p) throw new Error("sqlite: out of memory");
    try {
      n = x.asq_call(TAKE, conn, 0, 0, 0, p, -n);
      return this.heap().slice(p, p + Math.max(0, n));
    } finally {
      x.free(p);
    }
  }

  /** imports are what asq_wasm.c imports from "asq". */
  imports() {
    const eng = this;
    const str = (p) => (p ? cstr(eng.heap(), p) : null);
    const vfs = () => eng.vfs;
    return {
      open: (path, flags) => vfs().open(str(path), flags),
      close: (id) => vfs().close(id),
      read: (id, p, n, off) => vfs().read(id, eng.heap().subarray(p, p + n), off),
      write: (id, p, n, off) => vfs().write(id, eng.heap().subarray(p, p + n), off),
      truncate: (id, size) => vfs().truncate(id, size),
      sync: (id, flags) => vfs().sync(id, flags),
      size: (id) => vfs().size(id),
      lock: (id, level) => vfs().lock(id, level),
      unlock: (id, level) => vfs().unlock(id, level),
      reserved: (id) => vfs().reserved(id),
      remove: (path, syncDir) => vfs().remove(str(path), syncDir),
      access: (path, flags) => vfs().access(str(path), flags),
      random: (p, n) => {
        // getRandomValues takes at most 64 KiB at a time.
        for (let at = 0; at < n; at += 65536) crypto.getRandomValues(eng.heap().subarray(p + at, p + Math.min(n, at + 65536)));
      },
      now: () => Date.now(),
      localtime: (t, p) => {
        const v = new Int32Array(eng.memory.buffer, p, 10);
        localtime(t, v);
      },
    };
  }
}

const dec = new TextDecoder();
const enc = new TextEncoder();

function cstr(heap, p) {
  let end = p;
  while (heap[end]) end++;
  return dec.decode(heap.subarray(p, end));
}

/**
 * localtime fills v with t's (seconds since 1970) time in the browser's
 * zone, as asq_wasm.c's js_localtime says.
 */
export function localtime(t, v) {
  const d = new Date(t * 1000);
  v[0] = d.getSeconds();
  v[1] = d.getMinutes();
  v[2] = d.getHours();
  v[3] = d.getDate();
  v[4] = d.getMonth();
  v[5] = d.getFullYear() - 1900;
  v[6] = d.getDay();
  v[7] = Math.round((Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()) - Date.UTC(d.getFullYear(), 0, 1)) / 86400000);
  const offset = -d.getTimezoneOffset();
  const jan = -new Date(d.getFullYear(), 0, 1).getTimezoneOffset();
  const jul = -new Date(d.getFullYear(), 6, 1).getTimezoneOffset();
  v[8] = jan !== jul && offset === Math.max(jan, jul) ? 1 : 0;
  v[9] = offset * 60;
}

// --- Answers ---

/** Reader reads a call's input or answer (sqlite/csrc/asq.h). */
export class Reader {
  constructor(b) {
    this.b = b;
    this.v = new DataView(b.buffer, b.byteOffset, b.byteLength);
    this.p = 0;
  }
  u8() { return this.b[this.p++]; }
  u32() { const v = this.v.getUint32(this.p, true); this.p += 4; return v; }
  i64() { const v = this.v.getBigInt64(this.p, true); this.p += 8; return v; }
  u64() { const v = this.v.getBigUint64(this.p, true); this.p += 8; return v; }
  f64() { const v = this.v.getFloat64(this.p, true); this.p += 8; return v; }
  bytes(n) { const b = this.b.subarray(this.p, this.p + n); this.p += n; return b; }
  string() { return dec.decode(this.bytes(this.u32())); }
}

/** Writer is a growing buffer. */
export class Writer {
  constructor() {
    this.b = new Uint8Array(256);
    this.v = new DataView(this.b.buffer);
    this.length = 0;
  }
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
  setU32(at, x) { this.v.setUint32(at, x, true); return this; }
  bytes() { return this.b.slice(0, this.length); }
}

/**
 * errorAnswer is an answer that says a call failed: SQLite's code, and
 * a message (no tables written or read).
 */
export function errorAnswer(code, message) {
  const w = new Writer().u8(0).u32(0).u32(code).u8(1).string(message);
  w.setU32(1, w.length);
  return w.u32(0).u32(0).bytes();
}

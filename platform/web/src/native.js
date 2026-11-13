// Typed native modules (//asuka:native) and views (//asuka:view) on the
// web: what the JavaScript that asuka native generates for a Go package
// calls (through globalThis.asuka.native), and what Go's calls reach,
// through its host (Host's nativeNew, nativeCall...).
//
// The wire format is package native's: values in declaration order,
// little-endian. int64 and uint64 are BigInts; Go's int and uint, and the
// smaller integers, are numbers; a time is a Date; a duration is
// milliseconds; a []byte is a Uint8Array; a map is an object.

const enc = new TextEncoder();
const dec = new TextDecoder();

/** Encodes values (package native's Writer). */
export class NativeWriter {
  constructor() {
    this.b = new Uint8Array(64);
    this.v = new DataView(this.b.buffer);
    this.n = 0;
  }

  /** An answer's writer: its status, ok, first. */
  static ok() { return new NativeWriter().uint8(0); }

  room(k) {
    if (this.n + k <= this.b.length) return;
    const b = new Uint8Array(Math.max(this.b.length * 2, this.n + k));
    b.set(this.b.subarray(0, this.n));
    this.b = b;
    this.v = new DataView(b.buffer);
  }

  bool(x) { return this.uint8(x ? 1 : 0); }
  int8(x) { this.room(1); this.v.setInt8(this.n, x); this.n += 1; return this; }
  uint8(x) { this.room(1); this.v.setUint8(this.n, x); this.n += 1; return this; }
  int16(x) { this.room(2); this.v.setInt16(this.n, x, true); this.n += 2; return this; }
  uint16(x) { this.room(2); this.v.setUint16(this.n, x, true); this.n += 2; return this; }
  int32(x) { this.room(4); this.v.setInt32(this.n, x, true); this.n += 4; return this; }
  uint32(x) { this.room(4); this.v.setUint32(this.n, x, true); this.n += 4; return this; }
  int64(x) { this.room(8); this.v.setBigInt64(this.n, BigInt(x), true); this.n += 8; return this; }
  uint64(x) { this.room(8); this.v.setBigUint64(this.n, BigInt(x), true); this.n += 8; return this; }
  int(x) { return this.int64(Math.trunc(x)); }
  uint(x) { return this.uint64(Math.trunc(x)); }
  float32(x) { this.room(4); this.v.setFloat32(this.n, x, true); this.n += 4; return this; }
  float64(x) { this.room(8); this.v.setFloat64(this.n, x, true); this.n += 8; return this; }
  string(x) { return this.data(enc.encode(String(x))); }

  data(x) {
    this.uint32(x.length);
    this.room(x.length);
    this.b.set(x, this.n);
    this.n += x.length;
    return this;
  }

  /** A time: seconds and nanoseconds since 1970. */
  time(d) {
    const ms = d.getTime();
    const s = Math.floor(ms / 1000);
    return this.int64(s).int32(Math.round((ms - s * 1000) * 1e6));
  }

  /** A duration, in milliseconds (Go's: nanoseconds). */
  duration(ms) { return this.int64(BigInt(Math.round(ms * 1e6))); }
  len(k) { return this.uint32(k); }
  present(x) { return this.bool(x); }
  bytes() { return this.b.slice(0, this.n); }
}

/** Decodes values (package native's Reader); past what's there, every read is its zero value and failed is true. */
export class NativeReader {
  constructor(b, back) {
    this.b = b instanceof Uint8Array ? b : new Uint8Array(b);
    this.v = new DataView(this.b.buffer, this.b.byteOffset, this.b.byteLength);
    this.p = 0;
    this.failed = false;
    this.back = back; // the Go it's from: its callbacks' way back
  }

  take(k) {
    if (this.failed || this.b.length - this.p < k) {
      this.failed = true;
      return -1;
    }
    const at = this.p;
    this.p += k;
    return at;
  }

  bool() { return this.uint8() !== 0; }
  int8() { const at = this.take(1); return at < 0 ? 0 : this.v.getInt8(at); }
  uint8() { const at = this.take(1); return at < 0 ? 0 : this.v.getUint8(at); }
  int16() { const at = this.take(2); return at < 0 ? 0 : this.v.getInt16(at, true); }
  uint16() { const at = this.take(2); return at < 0 ? 0 : this.v.getUint16(at, true); }
  int32() { const at = this.take(4); return at < 0 ? 0 : this.v.getInt32(at, true); }
  uint32() { const at = this.take(4); return at < 0 ? 0 : this.v.getUint32(at, true); }
  int64() { const at = this.take(8); return at < 0 ? 0n : this.v.getBigInt64(at, true); }
  uint64() { const at = this.take(8); return at < 0 ? 0n : this.v.getBigUint64(at, true); }
  int() { return Number(this.int64()); }
  uint() { return Number(this.uint64()); }
  float32() { const at = this.take(4); return at < 0 ? 0 : this.v.getFloat32(at, true); }
  float64() { const at = this.take(8); return at < 0 ? 0 : this.v.getFloat64(at, true); }
  string() { return dec.decode(this.data()); }

  data() {
    const k = this.uint32();
    const at = this.take(k);
    return at < 0 ? new Uint8Array(0) : this.b.slice(at, at + k);
  }

  time() {
    const s = this.int64(), ns = this.int32();
    return new Date(Number(s) * 1000 + ns / 1e6);
  }

  duration() { return Number(this.int64()) / 1e6; }

  /** A slice's or a map's length, bounded by what's left. */
  len() {
    const k = this.uint32();
    if (k > this.b.length - this.p) {
      this.failed = true;
      return 0;
    }
    return k;
  }

  present() { return this.bool(); }
  /** A callback's id (0: none). */
  callback() { return Number(this.uint64()); }

  /** Calls a Go callback that answers nothing (it runs on Go's main thread). */
  notify(id, w) { if (id) this.back.invoke(id, w.bytes()); }

  /** Calls a Go callback and reads its answer. */
  ask(id, w) { return new NativeReader(id ? (this.back.invoke(id, w.bytes()) ?? new Uint8Array(0)) : new Uint8Array(0), this.back); }

  /** Throws when a read went past what's there. */
  check() { if (this.failed) throw new Error("asuka: a malformed call"); }
}

/** An error a package declares (native.NewError): its Go gets that value. Generated code subclasses it. */
export class DeclaredError extends Error {
  constructor(index, message) {
    super(message);
    this.asukaIndex = index;
  }
}

/** What the platform doesn't have: Go gets errors.ErrUnsupported. */
export class UnsupportedError extends Error {}

const providers = new Map(); // module id → provider
const objects = new Map(); // handle → { obj, provider, iface }
const calls = new Map(); // call → AbortController
let nextHandle = 0;

/** Registers a package's native code (its generated JavaScript does, as it loads). */
export function registerModule(id, provider) { providers.set(id, provider); }

/** Keeps an object for Go: its handle. */
export function retain(obj, provider, iface) {
  objects.set(++nextHandle, { obj, provider, iface });
  return nextHandle;
}

/** The object of a handle Go passed (null for 0, or one let go). */
export function object(h) { return objects.get(Number(h))?.obj ?? null; }

/** An error's answer: declared (its index), unsupported, canceled, or its name, 0 and message. */
export function answer(e) {
  const w = new NativeWriter();
  if (e instanceof DeclaredError) w.uint8(1).uint32(e.asukaIndex);
  else if (e instanceof UnsupportedError) w.uint8(3).string(e.message);
  else if (e?.name === "AbortError") w.uint8(4);
  else w.uint8(2).string(e?.name ?? "Error").int64(0).string(e?.message ?? String(e));
  return w.bytes();
}

// What Go calls, through its host; back is the host's way back to its Go.

export function makeObject(id, iface, back) {
  const p = providers.get(id);
  if (!p) return answer(new UnsupportedError(`no JavaScript ${id}: build the app with its packages' native code`));
  try {
    const obj = p.make(iface);
    return NativeWriter.ok().uint64(retain(obj, p, iface)).bytes();
  } catch (e) {
    return answer(e);
  }
}

export function call(h, method, input, back) {
  const e = objects.get(h);
  if (!e) return answer(new Error("asuka: the object is closed"));
  const w = NativeWriter.ok();
  try {
    e.provider.call(e.obj, e.iface, method, new NativeReader(input, back), w);
    return w.bytes();
  } catch (err) {
    return answer(err);
  }
}

export function start(h, method, input, id, back) {
  const e = objects.get(h);
  if (!e) {
    back.done(id, answer(new Error("asuka: the object is closed")));
    return;
  }
  const abort = new AbortController();
  calls.set(id, abort);
  // Started now, so a cancel after this returns reaches it.
  let p;
  try {
    p = Promise.resolve(e.provider.start(e.obj, e.iface, method, new NativeReader(input, back), abort.signal));
  } catch (err) {
    p = Promise.reject(err);
  }
  p.then((w) => w.bytes(), (err) => answer(abort.signal.aborted ? Object.assign(new Error("canceled"), { name: "AbortError" }) : err))
    .then((b) => {
      calls.delete(id);
      back.done(id, b);
    });
}

export function cancel(id) { calls.get(id)?.abort(); }
export function release(h) { objects.delete(h); }

/** A typed view of a kind (its module's id # its name). */
export function makeView(kind, ctx) {
  const i = kind.lastIndexOf("#");
  if (i < 0) return null;
  return providers.get(kind.slice(0, i))?.makeView(kind.slice(i + 1), ctx) ?? null;
}

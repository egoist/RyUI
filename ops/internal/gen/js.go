package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/egoist/asuka/ops"
)

// jsFile is the web host's side of the wire (platform/web/src/ops.js): an
// ES module with the schema's numbers, the batch decoder and the event
// encoders. Plain JavaScript, so the CLI ships it as it is.
func jsFile(s ops.SchemaDef) []byte {
	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("// %s\n\n", header)

	p("/** View kinds. */\nexport const Kind = Object.freeze({\n")
	for i, k := range s.Kinds {
		p("  %s: %d,\n", k.Name, i+1)
	}
	p("});\n\n/** Kind names, by kind. */\nexport const kindNames = [\"\"")
	for _, k := range s.Kinds {
		p(", %q", k.Name)
	}
	p("];\n\n")

	p("/** Props every kind has. */\nexport const Prop = Object.freeze({\n")
	for i, pr := range s.Common {
		p("  %s: %d,\n", pr.Name, i+1)
	}
	p("});\n\n/** Each kind's own props, from %d. */\nexport const KindProp = Object.freeze({\n", ops.FirstKindProp)
	for _, k := range s.Kinds {
		if len(k.Props) == 0 {
			continue
		}
		p("  %s: Object.freeze({", k.Name)
		for i, pr := range k.Props {
			if i > 0 {
				p(",")
			}
			p(" %s: %d", pr.Name, ops.FirstKindProp+i)
		}
		p(" }),\n")
	}
	p("});\n\n")

	p("/** Events hosts send. */\nexport const Event = Object.freeze({\n")
	for i, e := range s.Events {
		p("  %s: %d,\n", e.Name, i+1)
	}
	p("});\n\n/** Commands the core sends. */\nexport const Command = Object.freeze({\n")
	for i, c := range s.Commands {
		p("  %s: %d,\n", c.Name, i+1)
	}
	p("});\n\n")
	for _, e := range s.Enums {
		p("export const %s = Object.freeze({", e.Name)
		for i, v := range e.Values {
			if i > 0 {
				p(",")
			}
			p(" %s: %d", v, i)
		}
		p(" });\n")
	}
	p("\n")
	b.WriteString(jsRuntime)

	p("\n/** Encoders for the events hosts send, one per event kind: each returns the event's bytes. */\nexport const Events = Object.freeze({\n")
	for i, e := range s.Events {
		name := strings.Trim(lowerCamel(e.Name), "`")
		switch e.Payload {
		case ops.PNone:
			p("  %s: (id) => new Writer().header(id, %d).done(),\n", name, i+1)
		case ops.PBool:
			p("  %s: (id, v) => new Writer().header(id, %d).u8(v ? 1 : 0).done(),\n", name, i+1)
		case ops.PInt:
			p("  %s: (id, v) => new Writer().header(id, %d).i64(v).done(),\n", name, i+1)
		case ops.PFloat:
			p("  %s: (id, v) => new Writer().header(id, %d).f64(v).done(),\n", name, i+1)
		case ops.PString:
			p("  %s: (id, v) => new Writer().header(id, %d).string(v).done(),\n", name, i+1)
		case ops.PPoint:
			p("  %s: (id, x, y) => new Writer().header(id, %d).f64(x).f64(y).done(),\n", name, i+1)
		case ops.PEnvironment:
			p("  %s: (id, e) => new Writer().header(id, %d).environment(e).done(),\n", name, i+1)
		case ops.PGesture:
			p("  %s: (id, g) => new Writer().header(id, %d).gesture(g).done(),\n", name, i+1)
		case ops.PBytes:
			p("  %s: (id, v) => new Writer().header(id, %d).bytes(v).done(),\n", name, i+1)
		}
	}
	p("});\n")
	return b.Bytes()
}

// jsRuntime is the fixed part of the JavaScript side: the wire format.
const jsRuntime = `const utf8 = new TextDecoder();
const utf8Out = new TextEncoder();

/** Reads the little-endian wire format from a Uint8Array. */
export class Reader {
  constructor(bytes) {
    this.b = bytes;
    this.v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    this.p = 0;
  }
  get atEnd() { return this.p >= this.b.length; }
  u8() { return this.b[this.p++]; }
  u16() { const v = this.v.getUint16(this.p, true); this.p += 2; return v; }
  u32() { const v = this.v.getUint32(this.p, true); this.p += 4; return v; }
  i32() { const v = this.v.getInt32(this.p, true); this.p += 4; return v; }
  /** An i64 as a Number (exact up to 2^53: node IDs, colors, instants). */
  i64() { const lo = this.v.getUint32(this.p, true), hi = this.v.getInt32(this.p + 4, true); this.p += 8; return hi * 4294967296 + lo; }
  u64() { const lo = this.v.getUint32(this.p, true), hi = this.v.getUint32(this.p + 4, true); this.p += 8; return hi * 4294967296 + lo; }
  f32() { const v = this.v.getFloat32(this.p, true); this.p += 4; return v; }
  f64() { const v = this.v.getFloat64(this.p, true); this.p += 8; return v; }
  /** A u32-length run of bytes: a view into the batch, valid during the call. */
  bytes() { const n = this.u32(); const v = this.b.subarray(this.p, this.p + n); this.p += n; return v; }
  string() { return utf8.decode(this.bytes()); }
}

/** Value types (ops.Type). */
export const Type = Object.freeze({ Bool: 1, Int: 2, Float: 3, String: 4, Color: 5, Edges: 6, Enum: 7, Bytes: 8 });

/**
 * Decodes a batch of ops (see ops/ops.go) into h, which has create(id,
 * kind), destroy(id), insert(parent, child, index), remove(parent, child),
 * setProp(id, prop, type, value), setFrame(id, x, y, w, h), listen(id,
 * event), unlisten(id, event), command(id, cmd, args) and setRoot(id).
 * A value is a boolean, a Number (int, float, color, enum), a string, an
 * [top, right, bottom, left] array (edges) or a Uint8Array (bytes, valid
 * during the call).
 */
export function decode(bytes, h) {
  const r = new Reader(bytes);
  const n = r.u32();
  for (let i = 0; i < n; i++) {
    const code = r.u8();
    const id = r.u32();
    switch (code) {
      case 1: h.create(id, r.u16()); break;
      case 2: h.destroy(id); break;
      case 3: { const child = r.u32(); h.insert(id, child, r.u32()); break; }
      case 4: h.remove(id, r.u32()); break;
      case 5: { const prop = r.u16(); const t = r.u8(); h.setProp(id, prop, t, readValue(r, t)); break; }
      case 6: h.setFrame(id, r.f32(), r.f32(), r.f32(), r.f32()); break;
      case 7: h.listen(id, r.u16()); break;
      case 8: h.unlisten(id, r.u16()); break;
      case 9: { const cmd = r.u16(); h.command(id, cmd, r.bytes()); break; }
      case 10: h.setRoot(id); break;
      default: throw new Error("asuka: bad op code " + code);
    }
  }
}

function readValue(r, t) {
  switch (t) {
    case 1: return r.u8() !== 0;
    case 2: return r.i64();
    case 3: return r.f64();
    case 4: return r.string();
    case 5: return r.u64();
    case 6: return [r.f32(), r.f32(), r.f32(), r.f32()];
    case 7: return r.i32();
    case 8: return r.bytes();
  }
  throw new Error("asuka: bad value type " + t);
}

/** Writes the little-endian wire format. */
export class Writer {
  constructor() { this.buf = new Uint8Array(64); this.v = new DataView(this.buf.buffer); this.n = 0; }
  room(k) {
    if (this.n + k <= this.buf.length) return;
    const b = new Uint8Array(Math.max(this.buf.length * 2, this.n + k));
    b.set(this.buf.subarray(0, this.n));
    this.buf = b;
    this.v = new DataView(b.buffer);
  }
  u8(x) { this.room(1); this.buf[this.n++] = x; return this; }
  u16(x) { this.room(2); this.v.setUint16(this.n, x, true); this.n += 2; return this; }
  u32(x) { this.room(4); this.v.setUint32(this.n, x >>> 0, true); this.n += 4; return this; }
  i64(x) {
    this.room(8);
    const hi = Math.floor(x / 4294967296);
    this.v.setUint32(this.n, (x - hi * 4294967296) >>> 0, true);
    this.v.setInt32(this.n + 4, hi, true);
    this.n += 8;
    return this;
  }
  f32(x) { this.room(4); this.v.setFloat32(this.n, x, true); this.n += 4; return this; }
  f64(x) { this.room(8); this.v.setFloat64(this.n, x, true); this.n += 8; return this; }
  raw(a) { this.room(a.length); this.buf.set(a, this.n); this.n += a.length; return this; }
  bytes(a) { return this.u32(a.length).raw(a); }
  string(s) { return this.bytes(utf8Out.encode(s)); }
  header(id, kind) { return this.u32(id).u16(kind); }
  /** e: an Environment (see below). */
  environment(e) {
    for (const f of [e.width, e.height, e.safeTop, e.safeRight, e.safeBottom, e.safeLeft, e.keyboard, e.scale, e.fontScale]) this.f32(f);
    this.u8((e.dark ? 1 : 0) | (e.reduceMotion ? 2 : 0) | (e.platform & 7) << 2 | (e.screenReader ? 32 : 0));
    this.u8((e.widthClass & 3) | (e.heightClass & 3) << 2);
    this.string(e.locale).string(e.timeZone).string(e.languages);
    return this.u8(e.firstWeekday).u8(e.hourCycle);
  }
  /** g: a Gesture (see below). */
  gesture(g) {
    this.u8(g.state);
    for (const f of [g.x, g.y, g.translationX, g.translationY, g.velocityX, g.velocityY, g.scale, g.rotation]) this.f32(f);
    return this;
  }
  done() { return this.buf.slice(0, this.n); }
}

/** What a host reports about a root view (see ops.Environment). */
export function environment() {
  return {
    width: 0, height: 0, safeTop: 0, safeRight: 0, safeBottom: 0, safeLeft: 0, keyboard: 0,
    scale: 1, fontScale: 1, dark: false, reduceMotion: false, screenReader: false,
    platform: 5, // ops.PlatformWeb
    widthClass: 0, heightClass: 0, locale: "", timeZone: "", languages: "", firstWeekday: 0, hourCycle: 0,
  };
}

/** A pan or pinch (see ops.Gesture), in points relative to the view. */
export function gesture() {
  return { state: 0, x: 0, y: 0, translationX: 0, translationY: 0, velocityX: 0, velocityY: 0, scale: 1, rotation: 0 };
}
`

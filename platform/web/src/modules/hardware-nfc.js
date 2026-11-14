// asuka/sdk/nfc on the web (internal/nfcspec has the wire): Web NFC's
// NDEFReader, which Chrome on Android has (and no other browser: they
// answer "not available"). It reads and writes NDEF messages only: no
// tag IDs but its serial number, no raw commands, no capacity. A session
// is a scan(); one tag at a time, as the other hosts: a reading while the
// session holds a tag waits for "restart". Writing and making read-only
// act on the tag in range (Web NFC's write() and makeReadOnly() wait for
// one), which is the one held while the user keeps it there. A page has
// no NFC settings, and is opened by no tag (a URL tag opens the browser).
//
// The wire's NDEF messages are their bytes; Web NFC's are records of its
// own (recordType, mediaType, data...): fromWeb and toWeb turn one into
// the other, with an NDEF codec of their own (decodeNDEF, encodeNDEF).

const enc = new TextEncoder();
const dec = new TextDecoder();

export const b64 = (u8) => {
  let s = "";
  for (const c of u8 ?? []) s += String.fromCharCode(c);
  return btoa(s);
};
export const unb64 = (s) => Uint8Array.from(atob(s ?? ""), (c) => c.charCodeAt(0));

const cat = (...parts) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let i = 0;
  for (const p of parts) {
    out.set(p, i);
    i += p.length;
  }
  return out;
};

/** decodeNDEF reads an NDEF message's bytes: its records ({tnf, type, id, payload}), chunks joined. */
export function decodeNDEF(b) {
  const out = [];
  let p = 0;
  let chunk = null;
  const bad = () => new Error("not an NDEF message");
  while (p < b.length) {
    if (b.length - p < 3) throw bad();
    const h = b[p];
    const typeLen = b[p + 1];
    p += 2;
    let len;
    if (h & 0x10) len = b[p++];
    else {
      if (b.length - p < 4) throw bad();
      len = new DataView(b.buffer, b.byteOffset + p, 4).getUint32(0);
      p += 4;
    }
    const idLen = h & 0x08 ? b[p++] : 0;
    if (p + typeLen + idLen + len > b.length) throw bad();
    const r = { tnf: h & 7, type: b.slice(p, p + typeLen), id: b.slice(p + typeLen, p + typeLen + idLen), payload: b.slice(p + typeLen + idLen, p + typeLen + idLen + len) };
    p += typeLen + idLen + len;
    if (chunk) {
      if (r.tnf !== 6) throw bad();
      chunk.payload = cat(chunk.payload, r.payload);
      if (!(h & 0x20)) {
        out.push(chunk);
        chunk = null;
      }
    } else if (r.tnf === 6) throw bad();
    else if (h & 0x20) chunk = r;
    else out.push(r);
    if (h & 0x40) break;
  }
  if (chunk) throw bad();
  if (out.length === 1 && out[0].tnf === 0) return [];
  return out;
}

/** encodeNDEF is the records' message, as a tag holds it (an empty one: one empty record). */
export function encodeNDEF(records) {
  if (records.length === 0) return new Uint8Array([0xd0, 0, 0]);
  const parts = records.map((r, i) => {
    const short = r.payload.length < 256;
    let h = r.tnf & 7;
    if (i === 0) h |= 0x80;
    if (i === records.length - 1) h |= 0x40;
    if (short) h |= 0x10;
    if (r.id.length) h |= 0x08;
    const len = short ? [r.payload.length] : [r.payload.length >>> 24, (r.payload.length >>> 16) & 0xff, (r.payload.length >>> 8) & 0xff, r.payload.length & 0xff];
    return cat(new Uint8Array([h, r.type.length, ...len, ...(r.id.length ? [r.id.length] : [])]), r.type, r.id, r.payload);
  });
  return cat(...parts);
}

// The URI record's abbreviations, by code (NFC Forum's URI RTD).
const uriPrefixes = ["", "http://www.", "https://www.", "http://", "https://", "tel:", "mailto:",
  "ftp://anonymous:anonymous@", "ftp://ftp.", "ftps://", "sftp://", "smb://", "nfs://", "ftp://", "dav://",
  "news:", "telnet://", "imap:", "rtsp://", "urn:", "pop:", "sip:", "sips:", "tftp:", "btspp://", "btl2cap://",
  "btgoep://", "tcpobex://", "irdaobex://", "file://", "urn:epc:id:", "urn:epc:tag:", "urn:epc:pat:",
  "urn:epc:raw:", "urn:epc:", "urn:nfc:"];

const bytesOf = (data) => (data ? new Uint8Array(data.buffer, data.byteOffset, data.byteLength).slice() : new Uint8Array(0));
const wk = (type, payload, id) => ({ tnf: 1, type: enc.encode(type), id, payload });

/** fromWeb is a Web NFC NDEFRecord as the wire's record. */
export function fromWeb(r) {
  const id = enc.encode(r.id ?? "");
  const data = bytesOf(r.data);
  switch (r.recordType) {
    case "empty":
      return { tnf: 0, type: new Uint8Array(0), id: new Uint8Array(0), payload: new Uint8Array(0) };
    case "text": {
      const lang = enc.encode(r.lang || "en");
      const utf16 = (r.encoding ?? "utf-8").startsWith("utf-16");
      return wk("T", cat(new Uint8Array([(utf16 ? 0x80 : 0) | (lang.length & 0x3f)]), lang, data), id);
    }
    case "url": // data is the URL whole, its prefix spelled out
      return wk("U", cat(new Uint8Array([0]), data), id);
    case "absolute-url":
      return { tnf: 3, type: data, id, payload: new Uint8Array(0) };
    case "mime":
      return { tnf: 2, type: enc.encode(r.mediaType ?? "application/octet-stream"), id, payload: data };
    case "smart-poster": // data is its payload: the poster's message
      return wk("Sp", data, id);
    case "unknown":
      return { tnf: 5, type: new Uint8Array(0), id, payload: data };
    default:
      // ":t": a local type (a well-known type in a nested message); else
      // an external type, domain:type.
      if (r.recordType.startsWith(":")) return wk(r.recordType.slice(1), data, id);
      return { tnf: 4, type: enc.encode(r.recordType), id, payload: data };
  }
}

/**
 * toWeb is the wire's record as a Web NFC NDEFRecordInit; nested, in a
 * smart poster's message, its well-known types are local (":act").
 */
export function toWeb(r, nested = false) {
  const init = {};
  if (r.id.length) init.id = dec.decode(r.id);
  const type = dec.decode(r.type);
  switch (r.tnf) {
    case 0:
      return { recordType: "empty" };
    case 1:
      if (type === "T" && r.payload.length > 0) {
        const n = r.payload[0] & 0x3f;
        const utf16 = (r.payload[0] & 0x80) !== 0;
        return { ...init, recordType: "text", lang: dec.decode(r.payload.slice(1, 1 + n)), encoding: utf16 ? "utf-16be" : "utf-8", data: r.payload.slice(1 + n) };
      }
      if (type === "U" && r.payload.length > 0) {
        return { ...init, recordType: "url", data: (uriPrefixes[r.payload[0]] ?? "") + dec.decode(r.payload.slice(1)) };
      }
      if (type === "Sp") {
        return { ...init, recordType: "smart-poster", data: { records: decodeNDEF(r.payload).map((x) => toWeb(x, true)) } };
      }
      if (!nested) throw new Error(`not supported: Web NFC writes no well-known type ${type} outside a smart poster`);
      return { ...init, recordType: ":" + type, data: r.payload };
    case 2:
      return { ...init, recordType: "mime", mediaType: type, data: r.payload };
    case 3:
      return { ...init, recordType: "absolute-url", data: type };
    case 4:
      return { ...init, recordType: type, data: r.payload };
    case 5:
      return { ...init, recordType: "unknown", data: r.payload };
  }
  throw new Error(`not supported: a record of TNF ${r.tnf}`);
}

/** serial is a reading's serial number ("04:a2:...") as bytes. */
export function serial(s) {
  if (!s) return new Uint8Array(0);
  return Uint8Array.from(s.split(":").filter(Boolean), (x) => parseInt(x, 16));
}

/** word is a Web NFC error as the wire's words say it. */
export function word(e) {
  switch (e?.name) {
    case "NotAllowedError": return "denied";
    case "NotSupportedError": return "not available: " + e.message;
    case "NotReadableError": return "disabled";
    case "AbortError": return "ended";
    case "NetworkError": return "tag lost";
    default: return e?.message ?? String(e);
  }
}

const none = "not available: Web NFC is Chrome's on Android; this browser has none";
const ndefOnly = "not supported: the web reads and writes NDEF only";

export function modules() {
  let session = null;
  let lastEnded = null;
  let handles = 0;
  const has = () => typeof globalThis.NDEFReader === "function";
  const json = (args) => JSON.parse(dec.decode(args));

  const end = (s, w) => {
    if (!s || session !== s) return;
    session = null;
    lastEnded = [s.id, w];
    s.held = null;
    s.ctrl.abort();
    const wait = s.waiting;
    s.waiting = null;
    wait?.reject(new Error(w));
  };

  const found = (s, e) => {
    if (session !== s) return;
    if (!s.looking) {
      s.pending = e; // for the next restart
      return;
    }
    s.looking = false;
    s.handle = ++handles;
    let message;
    try {
      message = encodeNDEF(e.message.records.map((r) => fromWeb(r)));
    } catch {
      message = new Uint8Array([0xd0, 0, 0]);
    }
    s.held = { message, readOnly: false };
    const tag = JSON.stringify({ handle: s.handle, id: b64(serial(e.serialNumber)), techs: ["ndef"], ndef: { format: 2, capacity: -1 }, message: b64(message) });
    if (s.waiting) {
      const w = s.waiting;
      s.waiting = null;
      w.resolve(tag);
    } else s.queue.push(tag);
  };

  const mine = (args) => {
    const id = json(args).id;
    return session && session.id === id ? session : null;
  };

  const held = (args) => {
    const o = json(args);
    if (!session || !o.handle || session.handle !== o.handle || !session.held) throw new Error("tag lost");
    return [session, o];
  };

  return {
    nfc: {
      state: () => JSON.stringify({ supported: has(), enabled: has() }),
      // A page can't tell NFC turned on or off: a session finds it off.
      nextState: () => { throw new Error("not available"); },
      stopState: () => {},
      settings: () => { throw new Error("not available: a page can't open the system's settings"); },
      async start(args) {
        if (!has()) throw new Error(none);
        const o = json(args);
        end(session, "ended");
        const s = { id: o.id, ctrl: new AbortController(), queue: [], waiting: null, looking: true, pending: null, handle: 0, held: null };
        s.reader = new globalThis.NDEFReader();
        s.reader.onreading = (e) => found(s, e);
        session = s;
        try {
          await s.reader.scan({ signal: s.ctrl.signal });
        } catch (e) {
          const w = word(e);
          if (session === s) {
            session = null;
            lastEnded = [s.id, w];
          }
          throw new Error(w);
        }
      },
      next(args) {
        const id = json(args).id;
        const s = session;
        if (!s || s.id !== id) throw new Error(lastEnded && lastEnded[0] === id ? lastEnded[1] : "ended");
        if (s.queue.length) return s.queue.shift();
        return new Promise((resolve, reject) => { s.waiting = { resolve, reject }; });
      },
      restart(args) {
        const s = mine(args);
        if (!s) return;
        s.held = null;
        s.handle = 0;
        s.looking = true;
        const p = s.pending;
        s.pending = null;
        if (p) found(s, p);
      },
      alert: () => {}, // no sheet: the page shows its own
      stop(args) {
        end(mine(args), "ended");
      },
      read(args) {
        const [s] = held(args);
        return s.held.message; // as read: a page reads a tag again only as it comes near
      },
      async write(args) {
        const [s, o] = held(args);
        const bytes = unb64(o.message);
        if (s.held.readOnly) throw new Error("read only");
        const records = decodeNDEF(bytes).map((r) => toWeb(r));
        try {
          await s.reader.write({ records: records.length ? records : [{ recordType: "empty" }] }, { signal: s.ctrl.signal, overwrite: true });
        } catch (e) {
          throw new Error(word(e));
        }
        if (s.held) s.held.message = bytes;
      },
      async lock(args) {
        const [s] = held(args);
        if (typeof s.reader.makeReadOnly !== "function") throw new Error("not supported: this browser can't make tags read-only");
        try {
          await s.reader.makeReadOnly({ signal: s.ctrl.signal });
        } catch (e) {
          throw new Error(word(e));
        }
        if (s.held) s.held.readOnly = true;
      },
      status(args) {
        const [s] = held(args);
        return JSON.stringify({ format: s.held.readOnly ? 3 : 2, capacity: -1, canLock: !s.held.readOnly && typeof s.reader.makeReadOnly === "function" });
      },
      transceive: () => { throw new Error(ndefOnly); },
      classic: () => { throw new Error(ndefOnly); },
      // A tag opens the browser, not a page.
      nextOpened: () => { throw new Error("not available"); },
      stopOpened: () => {},
    },
  };
}

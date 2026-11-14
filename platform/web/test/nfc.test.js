// asuka/sdk/nfc's wire on the web (internal/nfcspec), over Web NFC's
// NDEFReader: node --test platform/web.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeNDEF, encodeNDEF, fromWeb, toWeb, serial, word, b64, unb64, modules } from "../src/modules/hardware-nfc.js";
import { modules as hardware } from "../src/modules/hardware.js";

const enc = new TextEncoder();
const dec = new TextDecoder();
const hex = (s) => Uint8Array.from(s.replace(/ /g, "").match(/../g), (x) => parseInt(x, 16));
const args = (o) => enc.encode(JSON.stringify(o));
const view = (u8) => new DataView(u8.buffer, u8.byteOffset, u8.byteLength);

test("NDEF bytes: Go's (sdk/nfc's Encode) both ways, chunks joined", () => {
  // nfc.URI("https://example.com"), nfc.Text("hi", "en").
  const m = hex("91 01 0c 55 04 6578616d706c652e636f6d 51 01 05 54 02 656e 6869");
  const rs = decodeNDEF(m);
  assert.equal(rs.length, 2);
  assert.equal(dec.decode(rs[0].type), "U");
  assert.deepEqual([...encodeNDEF(rs)], [...m]);
  assert.deepEqual(decodeNDEF(hex("d0 00 00")), []);
  assert.deepEqual([...encodeNDEF([])], [0xd0, 0, 0]);
  const chunked = decodeNDEF(hex("b2 03 02 612f62 6865 36 00 02 6c6c 56 00 01 6f"));
  assert.equal(dec.decode(chunked[0].payload), "hello");
  const long = { tnf: 2, type: enc.encode("a/b"), id: enc.encode("x"), payload: new Uint8Array(300).fill(7) };
  assert.deepEqual(decodeNDEF(encodeNDEF([long]))[0].payload.length, 300);
  for (const bad of ["d1", "d1 01 09 55 04", "b2 03 02 612f62 6865", "d6 00 00"]) assert.throws(() => decodeNDEF(hex(bad)), /not an NDEF message/);
});

test("Web NFC's records as the wire's and back", () => {
  const url = fromWeb({ recordType: "url", data: view(enc.encode("https://asuka.dev/t")) });
  assert.equal(url.tnf, 1);
  assert.deepEqual([...url.payload], [0, ...enc.encode("https://asuka.dev/t")]);
  assert.deepEqual(toWeb({ tnf: 1, type: enc.encode("U"), id: new Uint8Array(0), payload: hex("04 6173756b612e646576") }), { recordType: "url", data: "https://asuka.dev" });
  const text = fromWeb({ recordType: "text", lang: "ja", encoding: "utf-8", id: "a", data: view(enc.encode("こんにちは")) });
  assert.deepEqual([...text.payload.slice(0, 3)], [2, ...enc.encode("ja")]);
  assert.equal(dec.decode(text.id), "a");
  const back = toWeb(text);
  assert.equal(back.recordType, "text");
  assert.equal(back.lang, "ja");
  assert.equal(back.id, "a");
  assert.equal(dec.decode(back.data), "こんにちは");
  assert.deepEqual(fromWeb({ recordType: "mime", mediaType: "application/json", data: view(enc.encode("{}")) }).tnf, 2);
  assert.deepEqual(toWeb({ tnf: 2, type: enc.encode("text/vcard"), id: new Uint8Array(0), payload: enc.encode("x") }).mediaType, "text/vcard");
  const ext = fromWeb({ recordType: "example.com:thing", data: view(new Uint8Array([1])) });
  assert.equal(ext.tnf, 4);
  assert.equal(toWeb(ext).recordType, "example.com:thing");
  assert.equal(fromWeb({ recordType: "absolute-url", data: view(enc.encode("https://x/s")) }).tnf, 3);
  assert.equal(toWeb({ tnf: 3, type: enc.encode("https://x/s"), id: new Uint8Array(0), payload: new Uint8Array(0) }).data, "https://x/s");
  assert.equal(fromWeb({ recordType: "unknown", data: view(new Uint8Array([9])) }).tnf, 5);
  assert.equal(fromWeb({ recordType: "empty" }).tnf, 0);
  // A smart poster: its records nested, the well-known ones local.
  const inner = encodeNDEF([
    { tnf: 1, type: enc.encode("U"), id: new Uint8Array(0), payload: hex("04 61") },
    { tnf: 1, type: enc.encode("act"), id: new Uint8Array(0), payload: new Uint8Array([0]) },
  ]);
  const sp = toWeb({ tnf: 1, type: enc.encode("Sp"), id: new Uint8Array(0), payload: inner });
  assert.equal(sp.recordType, "smart-poster");
  assert.deepEqual(sp.data.records.map((r) => r.recordType), ["url", ":act"]);
  assert.deepEqual([...fromWeb({ recordType: "smart-poster", data: view(inner) }).payload], [...inner]);
  assert.throws(() => toWeb({ tnf: 1, type: enc.encode("act"), id: new Uint8Array(0), payload: new Uint8Array(1) }), /not supported/);
});

test("serial numbers, errors, base64", () => {
  assert.deepEqual([...serial("04:a2:0f")], [4, 0xa2, 0x0f]);
  assert.deepEqual([...serial("")], []);
  assert.equal(word({ name: "NotAllowedError" }), "denied");
  assert.equal(word({ name: "NotReadableError" }), "disabled");
  assert.equal(word({ name: "NetworkError" }), "tag lost");
  assert.match(word({ name: "NotSupportedError", message: "no" }), /^not available/);
  assert.deepEqual([...unb64(b64(new Uint8Array([0, 255, 7])))], [0, 255, 7]);
});

/** FakeReader is Chrome's NDEFReader, its readings and writes the test's. */
class FakeReader {
  static last = null;
  constructor() { FakeReader.last = this; this.written = []; this.locked = false; }
  async scan({ signal }) { this.signal = signal; if (FakeReader.deny) throw Object.assign(new Error("no"), { name: "NotAllowedError" }); }
  async write(message, opts) { this.written.push({ message, opts }); }
  async makeReadOnly() { this.locked = true; }
  tap(serialNumber, records) { this.onreading({ serialNumber, message: { records } }); }
}

test("nfc: a scan's tags one at a time, written and locked; other browsers have none", async () => {
  const { nfc } = modules();
  assert.deepEqual(JSON.parse(nfc.state()), { supported: false, enabled: false });
  await assert.rejects(nfc.start(args({ id: 1 })), /not available: Web NFC is Chrome's/);
  globalThis.NDEFReader = FakeReader;
  try {
    assert.deepEqual(JSON.parse(nfc.state()), { supported: true, enabled: true });
    await nfc.start(args({ id: 2, alert: "Hold it near" }));
    const r = FakeReader.last;
    const next = nfc.next(args({ id: 2 }));
    r.tap("04:01:02", [{ recordType: "url", data: view(enc.encode("https://example.com/a")) }]);
    const tag = JSON.parse(await next);
    assert.equal(tag.handle, 1);
    assert.deepEqual([...unb64(tag.id)], [4, 1, 2]);
    assert.deepEqual(tag.techs, ["ndef"]);
    const m = decodeNDEF(unb64(tag.message));
    assert.equal(dec.decode(m[0].payload.slice(1)), "https://example.com/a");
    // Another tag waits for restart.
    r.tap("04:09", [{ recordType: "empty" }]);
    const second = nfc.next(args({ id: 2 }));
    let got = null;
    second.then((t) => { got = t; });
    await new Promise((res) => setTimeout(res, 5));
    assert.equal(got, null);
    // Write and lock the one held.
    const msg = encodeNDEF([{ tnf: 1, type: enc.encode("T"), id: new Uint8Array(0), payload: hex("02 656e 6869") }]);
    await nfc.write(args({ handle: 1, message: b64(msg) }));
    assert.equal(r.written[0].message.records[0].recordType, "text");
    assert.equal(r.written[0].opts.overwrite, true);
    assert.deepEqual([...nfc.read(args({ handle: 1 }))], [...msg]);
    await nfc.lock(args({ handle: 1 }));
    assert.equal(JSON.parse(nfc.status(args({ handle: 1 }))).format, 3);
    await assert.rejects(nfc.write(args({ handle: 1, message: b64(msg) })), /read only/);
    assert.throws(() => nfc.transceive(args({ handle: 1, tech: "mifare" })), /NDEF only/);
    nfc.restart(args({ id: 2 }));
    const t2 = JSON.parse(await second);
    assert.equal(t2.handle, 2);
    assert.throws(() => nfc.read(args({ handle: 1 })), /tag lost/);
    nfc.stop(args({ id: 2 }));
    assert.equal(r.signal.aborted, true);
    assert.throws(() => nfc.next(args({ id: 2 })), /ended/);
    // Denied: the session never begins, and next says why.
    FakeReader.deny = true;
    await assert.rejects(nfc.start(args({ id: 3 })), /denied/);
    assert.throws(() => nfc.next(args({ id: 3 })), /denied/);
  } finally {
    delete globalThis.NDEFReader;
    FakeReader.deny = false;
  }
  assert.throws(() => nfc.nextOpened(), /not available/);
  assert.throws(() => nfc.nextState(), /not available/);
  assert.ok(hardware({ prefix: () => "asuka.test.", options: {}, assets: "assets" }).nfc, "hardware has nfc");
});

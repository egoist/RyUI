// The web host's system modules (src/modules/system*.js): their
// arguments and results as the Go packages encode and decode them
// (secure, ui.ShareFiles, compose, notify), and the secure store's
// encryption, with Node's WebCrypto.
import { test } from "node:test";
import assert from "node:assert/strict";
import { splitKey, decodeShare, smsURL, deviceInfo } from "../src/modules/system.js";
import { SecureStore, migrateLocalStorage } from "../src/modules/system-secure.js";
import { WaitQueue } from "../src/modules/system-notify.js";
import { typeOf } from "../src/modules/system-clipboard.js";

const enc = new TextEncoder();

// Go's little-endian u32 and u32-length strings.
function u32(n) { const b = new Uint8Array(4); new DataView(b.buffer).setUint32(0, n, true); return b; }
function str(s) { const b = enc.encode(s); return concat(u32(b.length), b); }
function concat(...bs) {
  const out = new Uint8Array(bs.reduce((n, b) => n + b.length, 0));
  let p = 0;
  for (const b of bs) { out.set(b, p); p += b.length; }
  return out;
}

test("secureStore.set's arguments (secure.keyValue)", () => {
  const k = splitKey(concat(u32(5), enc.encode("token"), enc.encode("s3cr3t")));
  assert.equal(k.key, "token");
  assert.equal(new TextDecoder().decode(k.value), "s3cr3t");
  assert.equal(splitKey(concat(u32(9), enc.encode("ab"))), null);
  assert.equal(splitKey(new Uint8Array(2)), null);
});

test("share.files' arguments (ui.ShareFiles)", () => {
  const b = concat(u32(42), str("application/pdf"), str("com.adobe.pdf"), str("Invoice"), u32(2), str("/data/app/a.pdf"), str("/tmp/b.pdf"));
  assert.deepEqual(decodeShare(b), { view: 42, mime: "application/pdf", uti: "com.adobe.pdf", title: "Invoice", paths: ["/data/app/a.pdf", "/tmp/b.pdf"] });
});

test("compose.message's sms: link", () => {
  assert.equal(smsURL(["+15551234", "+15555678"], "Hi there", false), "sms:+15551234,+15555678?body=Hi%20there");
  assert.equal(smsURL(["+15551234"], "Hi", true), "sms:+15551234&body=Hi");
  assert.equal(smsURL([], "", false), "sms:");
});

test("MIME types from names", () => {
  assert.equal(typeOf("/tmp/shared/1/Photo.JPG"), "image/jpeg");
  assert.equal(typeOf("https://example.com/a.pdf?x=1"), "application/pdf");
  assert.equal(typeOf("README"), "");
});

test("device.info from the user agent and client hints", async () => {
  const h = { options: { version: "1.2", build: 7 } };
  const iphone = JSON.parse(await deviceInfo(h, { userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1", maxTouchPoints: 5 }));
  assert.deepEqual(iphone, { model: "iPhone", system: "iOS", systemVersion: "17.4", appVersion: "1.2", build: "7" });
  const pixel = JSON.parse(await deviceInfo(h, {
    userAgent: "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36",
    userAgentData: { platform: "Android", getHighEntropyValues: async () => ({ platformVersion: "15.0.0", model: "Pixel 9" }) },
  }));
  assert.equal(pixel.model, "Pixel 9");
  assert.equal(pixel.system, "Android");
  assert.equal(pixel.systemVersion, "15.0.0");
  const win = JSON.parse(await deviceInfo(h, {
    userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
    userAgentData: { platform: "Windows", getHighEntropyValues: async () => ({ platformVersion: "15.0.0", model: "" }) },
  }));
  assert.deepEqual([win.model, win.system, win.systemVersion], ["PC", "Windows", "11"]);
});

// A SecureStore backend in memory, keeping values as structured clone would.
function memoryBackend() {
  const stores = { keys: new Map(), values: new Map() };
  return {
    stores,
    get: async (s, k) => stores[s].get(k),
    put: async (s, k, v) => { stores[s].set(k, v); },
    delete: async (s, k) => { stores[s].delete(k); },
  };
}

test("the secure store encrypts under a key it can't export", async () => {
  const be = memoryBackend();
  const s = new SecureStore(be);
  await s.set("token", enc.encode("hunter2"));
  assert.equal(new TextDecoder().decode(await s.get("token")), "hunter2");
  assert.equal(await s.get("missing"), null);
  const rec = be.stores.values.get("token");
  assert.ok(!new TextDecoder().decode(rec.data).includes("hunter2"), "kept encrypted");
  assert.equal(rec.iv.length, 12);
  const key = be.stores.keys.get("key");
  assert.equal(key.extractable, false);
  await assert.rejects(crypto.subtle.exportKey("raw", key));
  // A value moved to another name doesn't decrypt there.
  be.stores.values.set("other", rec);
  assert.equal(await s.get("other"), null);
  // The same key, from storage, for a store made again (a reload).
  assert.equal(new TextDecoder().decode(await new SecureStore(be).get("token")), "hunter2");
  await s.delete("token");
  assert.equal(await s.get("token"), null);
});

test("the secure store takes in what localStorage kept", async () => {
  const m = new Map([["ryui.app.secure.a", btoa("one")], ["ryui.app.locale", "fr"], ["ryui.app.secure.b", btoa("two")]]);
  const storage = { get length() { return m.size; }, key: (i) => [...m.keys()][i], getItem: (k) => m.get(k) ?? null, removeItem: (k) => m.delete(k) };
  const s = new SecureStore(memoryBackend());
  await migrateLocalStorage(s, "ryui.app.", storage);
  assert.deepEqual([...m.keys()], ["ryui.app.locale"]);
  assert.equal(new TextDecoder().decode(await s.get("a")), "one");
  assert.equal(new TextDecoder().decode(await s.get("b")), "two");
});

test("notify's wait queue: kept until asked, stopped with an error", async () => {
  const q = new WaitQueue();
  q.deliver("first");
  assert.equal(await q.next(), "first");
  const p = q.next();
  q.deliver("second");
  assert.equal(await p, "second");
  const w = q.next();
  q.stop();
  await assert.rejects(w, /stopped/);
});

// The WebView's helpers: Block's patterns as the other hosts match them,
// what a frame shows or downloads, a download's name.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeStrings, blocked, isHTML, canShow, isAttachment, filenameOf, sizeOf, cookieHeader, toJSON } from "../src/webview-util.js";

function encodeStrings(ss) {
  const parts = ss.map((s) => new TextEncoder().encode(s));
  const b = new Uint8Array(4 + parts.reduce((a, p) => a + 4 + p.length, 0));
  const v = new DataView(b.buffer);
  v.setUint32(0, ss.length, true);
  let o = 4;
  for (const p of parts) { v.setUint32(o, p.length, true); b.set(p, o + 4); o += 4 + p.length; }
  return b;
}

test("decodes Block's patterns as ui.encodeStrings writes them", () => {
  assert.deepEqual(decodeStrings(encodeStrings(["doubleclick.net", "/ads/", "é.example"])), ["doubleclick.net", "/ads/", "é.example"]);
  assert.deepEqual(decodeStrings(new Uint8Array(0)), []);
  assert.deepEqual(decodeStrings(encodeStrings(["a"]).subarray(0, 6)), []); // cut short
});

test("a host blocks it and its subdomains; a pattern with / any URL with it", () => {
  const p = ["doubleclick.net", "/ads/"];
  assert.equal(blocked(p, "https://doubleclick.net/x.js"), true);
  assert.equal(blocked(p, "https://ad.g.DoubleClick.net/x.js"), true);
  assert.equal(blocked(p, "https://notdoubleclick.net/x.js"), false);
  assert.equal(blocked(p, "https://example.com/ads/banner.png"), true);
  assert.equal(blocked(p, "https://example.com/adsense.js"), false);
  assert.equal(blocked([], "https://doubleclick.net/"), false);
  assert.equal(blocked(p, ""), false);
});

test("what a frame shows, and what it downloads", () => {
  assert.equal(isHTML("text/html; charset=utf-8"), true);
  assert.equal(isHTML("application/xhtml+xml"), true);
  assert.equal(isHTML("image/png"), false);
  for (const t of ["text/plain", "image/svg+xml", "video/mp4", "application/pdf", "application/json", "application/ld+json"]) assert.equal(canShow(t), true, t);
  for (const t of ["application/zip", "application/octet-stream", "application/vnd.ms-excel"]) assert.equal(canShow(t), false, t);
  assert.equal(isAttachment('attachment; filename="a.zip"'), true);
  assert.equal(isAttachment("inline"), false);
});

test("a download's name and size", () => {
  assert.equal(filenameOf('attachment; filename="report 1.pdf"', "https://x.dev/get?id=1"), "report 1.pdf");
  assert.equal(filenameOf("attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf", "https://x.dev/get"), "résumé.pdf");
  assert.equal(filenameOf("", "https://x.dev/files/a%20b.zip?x=1"), "a b.zip");
  assert.equal(filenameOf("", "https://x.dev/"), "download");
  assert.equal(sizeOf("1234"), 1234);
  assert.equal(sizeOf(null), -1);
  assert.equal(sizeOf("nope"), -1);
});

test("cookies as a header, values as JSON", () => {
  assert.equal(cookieHeader("a=1;  b=2; "), "a=1; b=2");
  assert.equal(cookieHeader(""), "");
  assert.equal(toJSON(undefined), "null");
  assert.equal(toJSON(() => 1), "null");
  assert.equal(toJSON({ a: [1, "x"] }), '{"a":[1,"x"]}');
});

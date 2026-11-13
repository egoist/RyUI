// The JavaScript reader of a sidebar agrees with Go's (ops/sidebar.go):
// it decodes ops/testdata/sidebar.bin to ops/testdata/sidebar.txt.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { decodeSidebar, sameShape, badgeNumber } from "../src/sidebar.js";

const testdata = fileURLToPath(new URL("../../../ops/testdata/", import.meta.url));

test("decodes the golden sidebar", () => {
  const b = new Uint8Array(readFileSync(testdata + "sidebar.bin"));
  const sections = decodeSidebar(b);
  let out = "";
  sections.forEach((s, i) => {
    out += `#\t${s.title}\n`;
    for (const it of s.items) out += `${i}\t${it.title}\t${it.icon}\t${it.badge}\n`;
  });
  assert.equal(out, readFileSync(testdata + "sidebar.txt", "utf8"));
  // Cut short, it ends where it's cut, with whole items.
  const cut = decodeSidebar(b.subarray(0, b.length - 18));
  assert.equal(cut.length, 2);
  assert.deepEqual(cut[1].items.map((it) => it.title), ["Archiv"]);
  assert.deepEqual(decodeSidebar(new Uint8Array(0)), []);
  const more = structuredClone(sections);
  more[0].items[0].badge = "4";
  assert.ok(sameShape(sections, more));
  more[2].items.push({ title: "Spam", icon: "", badge: "" });
  assert.ok(!sameShape(sections, more));
});

test("reads a badge's count", () => {
  assert.equal(badgeNumber("3"), 3);
  assert.equal(badgeNumber("1234"), 1234);
  for (const s of ["", "New", "99+", "٣", "1234567890"]) assert.equal(badgeNumber(s), null);
});

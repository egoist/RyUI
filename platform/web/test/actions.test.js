// The JavaScript reader of action menus agrees with Go's (ops/actions.go):
// it decodes ops/testdata/actions.bin to ops/testdata/actions.txt.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { decodeActions, flatActions } from "../src/actions.js";

const testdata = fileURLToPath(new URL("../../../ops/testdata/", import.meta.url));

test("decodes the golden menu", () => {
  const items = decodeActions(new Uint8Array(readFileSync(testdata + "actions.bin")));
  let out = "";
  const walk = (as, depth) => {
    for (const a of as) {
      out += `${a.index}\t${depth}\t${a.role}\t${a.flags}\t${a.title}\t${a.icon}\n`;
      walk(a.items, depth + 1);
    }
  };
  walk(items, 0);
  assert.equal(out, readFileSync(testdata + "actions.txt", "utf8"));
  assert.deepEqual(flatActions(items).map((a) => a.index), [...Array(12).keys()]);
});

test("a cut menu ends where it's cut", () => {
  const b = new Uint8Array(readFileSync(testdata + "actions.bin"));
  const items = decodeActions(b.subarray(0, b.length - 3));
  assert.equal(flatActions(items).length, 11);
  assert.deepEqual(decodeActions(new Uint8Array(0)), []);
});

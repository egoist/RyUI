// The JavaScript reader of an alert's fields agrees with Go's
// (ops/alert.go): it decodes ops/testdata/alertfields.bin to
// ops/testdata/alertfields.txt, and writes FieldChanged's payload as
// ops/testdata/fieldchanged.bin.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { decodeAlertFields, fieldChanged } from "../src/alertfields.js";

const testdata = fileURLToPath(new URL("../../../ops/testdata/", import.meta.url));

test("decodes the golden fields", () => {
  const b = new Uint8Array(readFileSync(testdata + "alertfields.bin"));
  let out = "";
  for (const f of decodeAlertFields(b)) out += `${f.placeholder}\t${f.text}\t${f.flags}\t${f.keyboard}\t${f.contentType}\n`;
  assert.equal(out, readFileSync(testdata + "alertfields.txt", "utf8"));
  assert.equal(decodeAlertFields(b.subarray(0, b.length - 3)).length, 3);
  assert.deepEqual(decodeAlertFields(new Uint8Array(0)), []);
});

test("writes FieldChanged's payload", () => {
  assert.deepEqual(fieldChanged(2, "Grüße"), new Uint8Array(readFileSync(testdata + "fieldchanged.bin")));
});

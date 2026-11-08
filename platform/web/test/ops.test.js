// The Go side writes ops/testdata (go test ./ops -update). These tests
// check the generated JavaScript decoder and event encoders agree with it,
// as platform/ios's and platform/android's do: node --test platform/web.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { decode, Events, Writer, environment, gesture, Lifecycle, GestureState } from "../src/ops.js";

const testdata = fileURLToPath(new URL("../../../ops/testdata/", import.meta.url));
const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
const f32 = (f) => { const v = new DataView(new ArrayBuffer(4)); v.setFloat32(0, f, true); return v.getUint32(0, true).toString(16).padStart(8, "0"); };
const f64 = (f) => { const v = new DataView(new ArrayBuffer(8)); v.setFloat64(0, f, true); return v.getBigUint64(0, true).toString(16).padStart(16, "0"); };

test("decodes the golden batch", () => {
  let out = "";
  decode(new Uint8Array(readFileSync(testdata + "golden.bin")), {
    create: (id, kind) => { out += `create ${id} ${kind}\n`; },
    destroy: (id) => { out += `destroy ${id}\n`; },
    insert: (p, c, i) => { out += `insert ${p} ${c} ${i}\n`; },
    remove: (p, c) => { out += `remove ${p} ${c}\n`; },
    setProp: (id, prop, t, v) => {
      const s = [null,
        () => (v ? "true" : "false"),
        () => String(v),
        () => f64(v),
        () => hex(new TextEncoder().encode(v)),
        () => v.toString(16).padStart(16, "0"),
        () => v.map(f32).join(" "),
        () => String(v),
        () => hex(v)][t]();
      out += `set ${id} ${prop} ${t} ${s}\n`;
    },
    setFrame: (id, x, y, w, h) => { out += `frame ${id} ${f32(x)} ${f32(y)} ${f32(w)} ${f32(h)}\n`; },
    listen: (id, e) => { out += `listen ${id} ${e}\n`; },
    unlisten: (id, e) => { out += `unlisten ${id} ${e}\n`; },
    command: (id, c, args) => { out += `command ${id} ${c} ${hex(args)}\n`; },
    setRoot: (id) => { out += `root ${id}\n`; },
  });
  assert.equal(out, readFileSync(testdata + "golden.txt", "utf8"));
});

test("encodes the golden events", () => {
  const env = Object.assign(environment(), {
    width: 402, height: 874, safeTop: 62, safeBottom: 34, keyboard: 336, scale: 3, fontScale: 1.25,
    dark: true, reduceMotion: true, screenReader: true, platform: 1, widthClass: 1, heightClass: 3,
    locale: "en_US", timeZone: "Asia/Shanghai", languages: "en-US,zh-Hans", firstWeekday: 2, hourCycle: 24,
  });
  const pan = Object.assign(gesture(), { state: GestureState.Changed, x: 10, y: 20, translationX: 5, translationY: -3, velocityX: 100, velocityY: -50, scale: 1, rotation: 0.5 });
  const events = [
    Events.press(7),
    Events.textChanged(7, "hé"),
    Events.toggled(8, true),
    Events.valueChanged(9, 0.25),
    Events.scrolled(10, 1.5, -2),
    Events.lifecycle(1, Lifecycle.Background),
    Events.environment(1, env),
    Events.pan(11, pan),
    Events.native(12, new Uint8Array([1, 2, 3])),
  ];
  const w = new Writer();
  for (const e of events) w.u32(e.length).raw(e);
  assert.equal(hex(w.done()), hex(readFileSync(testdata + "events.bin")));
});

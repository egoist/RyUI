// The displays, power, dock and appearance modules' wire formats, as
// asuka/screen, asuka/power, asuka/dock and asuka/appearance read them:
// node --test platform/web.
import { test } from "node:test";
import assert from "node:assert/strict";
import { displaysJSON, batteryState, parseRGB, appearanceState, watcher, modules } from "../src/modules/desktop.js";

const enc = new TextEncoder();
const dec = new TextDecoder();

test("displays: window.screen alone is one display, the primary", () => {
  const s = { width: 1512, height: 982, availWidth: 1512, availHeight: 949, availLeft: 0, availTop: 33 };
  assert.deepEqual(JSON.parse(displaysJSON([s], 2, false)), [{
    id: "screen", name: "",
    bounds: { x: 0, y: 0, width: 1512, height: 982 },
    workArea: { x: 0, y: 33, width: 1512, height: 949 },
    scale: 2, refreshRate: 0, primary: true,
  }]);
  // Firefox has no availLeft: the work area starts at the screen's corner.
  const ff = JSON.parse(displaysJSON([{ width: 800, height: 600, availWidth: 800, availHeight: 570 }], 1, false))[0];
  assert.deepEqual(ff.workArea, { x: 0, y: 0, width: 800, height: 570 });
});

test("displays: the screen details, the primary first, the same JSON for the same screens", () => {
  const screens = [
    { label: "DELL U2720Q", left: 1512, top: -200, width: 2560, height: 1440, availLeft: 1512, availTop: -175, availWidth: 2560, availHeight: 1415, devicePixelRatio: 1, isPrimary: false },
    { label: "Built-in Retina Display", left: 0, top: 0, width: 1512, height: 982, availLeft: 0, availTop: 33, availWidth: 1512, availHeight: 949, devicePixelRatio: 2, isPrimary: true },
  ];
  const j = displaysJSON(screens, 1, true);
  assert.equal(j, displaysJSON(screens, 1, true));
  const ds = JSON.parse(j);
  assert.deepEqual(ds.map((d) => [d.id, d.name, d.primary, d.scale]), [
    ["Built-in Retina Display", "Built-in Retina Display", true, 2],
    ["DELL U2720Q", "DELL U2720Q", false, 1],
  ]);
  assert.deepEqual(ds[1].bounds, { x: 1512, y: -200, width: 2560, height: 1440 });
  assert.deepEqual(ds[1].workArea, { x: 1512, y: -175, width: 2560, height: 1415 });
  // A screen without a label is named by its place in the list.
  assert.equal(JSON.parse(displaysJSON([{ width: 1, height: 1, isPrimary: true }], 1, true))[0].id, "0");
});

test("power: the battery's state bytes", () => {
  assert.deepEqual([...batteryState(null)], [0, 0, 255, 0, 0, 0]);
  // Chrome's answer on a computer without a battery.
  assert.deepEqual([...batteryState({ charging: true, level: 1, chargingTime: 0, dischargingTime: Infinity })], [0, 0, 255, 0, 0, 0]);
  assert.deepEqual([...batteryState({ charging: false, level: 0.42, chargingTime: Infinity, dischargingTime: 3600 })], [1, 1, 42, 0, 0, 0]);
  assert.deepEqual([...batteryState({ charging: true, level: 0.8, chargingTime: 1200, dischargingTime: Infinity })], [1, 0, 80, 1, 0, 0]);
  // Plugged in and full: not charging.
  assert.deepEqual([...batteryState({ charging: true, level: 1, chargingTime: 0, dischargingTime: 7200 })], [1, 0, 100, 0, 0, 0]);
});

test("appearance: computed colors and the state bytes", () => {
  assert.deepEqual(parseRGB("rgb(0, 122, 255)"), [0, 122, 255]);
  assert.deepEqual(parseRGB("rgba(165, 80, 167, 1)"), [165, 80, 167]);
  assert.deepEqual(parseRGB("rgb(0 122.4 255)"), [0, 122, 255]);
  assert.deepEqual(parseRGB("color(srgb 1 0.5 0)"), [255, 128, 0]);
  assert.equal(parseRGB("AccentColor"), null);
  assert.equal(parseRGB(undefined), null);
  assert.deepEqual([...appearanceState([0, 122, 255], true, false)], [1, 0, 122, 255, 1, 0]);
  assert.deepEqual([...appearanceState(null, false, true)], [0, 0, 0, 0, 0, 1]);
});

test("dock: a page has no icon: nothing asked", async () => {
  const { dock } = modules({});
  assert.equal(await dock.progress(new Uint8Array(9)), null);
  assert.deepEqual([...await dock.attention(new Uint8Array([1]))], [0, 0, 0, 0]);
  assert.equal(await dock.cancelAttention(new Uint8Array(4)), null);
});

test("power.idle is unknown", async () => {
  const { power } = modules({});
  assert.equal(await power.idle(new Uint8Array(0)), null);
});

test("next answers at once for another state, else at the change; stop fails it with stopped", async () => {
  let state = new Uint8Array([1, 2]);
  let changed = null;
  let listens = 0;
  const w = watcher(() => state, (f) => { listens++; changed = f; });
  assert.deepEqual([...await w.next(new Uint8Array(0))], [1, 2]); // no last state: now
  assert.deepEqual([...await w.next(new Uint8Array([9]))], [1, 2]);
  const a = w.next(new Uint8Array([1, 2]));
  const b = w.next(new Uint8Array([1, 2]));
  await Promise.resolve();
  assert.equal(listens, 1);
  await changed(); // nothing changed: they wait on
  state = new Uint8Array([3]);
  await changed();
  assert.deepEqual([...await a], [3]);
  assert.deepEqual([...await b], [3]);
  const c = w.next(new Uint8Array([3]));
  await Promise.resolve();
  assert.equal(w.stop(), null);
  await assert.rejects(c, { message: "stopped" });
  assert.equal(listens, 1);
});

test("displays: current and next from window.screen, a resize answering", async () => {
  const target = new EventTarget();
  const saved = {};
  const globals = {
    screen: { width: 1920, height: 1080, availWidth: 1920, availHeight: 1040, addEventListener() {} },
    devicePixelRatio: 1,
    addEventListener: target.addEventListener.bind(target),
    matchMedia: () => ({ matches: false, addEventListener() {} }),
    navigator: {},
  };
  for (const k of Object.keys(globals)) {
    saved[k] = Object.getOwnPropertyDescriptor(globalThis, k);
    Object.defineProperty(globalThis, k, { configurable: true, writable: true, value: globals[k] });
  }
  try {
    const { displays } = modules({});
    const now = await displays.current(new Uint8Array(0));
    assert.equal(JSON.parse(now)[0].bounds.width, 1920);
    const next = displays.next(enc.encode(now));
    await new Promise((r) => setTimeout(r));
    globalThis.screen.width = 2560;
    target.dispatchEvent(new Event("resize"));
    assert.equal(JSON.parse(dec.decode(await next))[0].bounds.width, 2560);
  } finally {
    for (const k of Object.keys(globals)) {
      if (saved[k]) Object.defineProperty(globalThis, k, saved[k]);
      else delete globalThis[k];
    }
  }
});

// asuka/crash in the browser (crash.js): Go's crash output kept as the
// program ends, the run's mark, uncaught errors as exception records,
// and what the page says of the browser.
import { test } from "node:test";
import assert from "node:assert/strict";
import * as fs from "../src/fs.js";
import { Crash, frames, systemOf, browserOf } from "../src/crash.js";

const target = new EventTarget();
globalThis.addEventListener ??= target.addEventListener.bind(target);
const items = new Map();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: { getItem: (k) => items.get(k) ?? null, setItem: (k, v) => items.set(k, String(v)), removeItem: (k) => items.delete(k) },
});
const dec = new TextDecoder();

/** fakeGo is a Go of wasm_exec.js, as far as Crash needs one: its imports, its memory, whether it exited. */
function fakeGo() {
  const buffer = new ArrayBuffer(1 << 16);
  const go = { mem: new DataView(buffer), exited: false, written: [], importObject: { gojs: {} } };
  go.importObject.gojs["runtime.wasmWrite"] = (sp) => go.written.push(sp);
  let heap = 4096;
  // print writes s to fd as Go's runtime does (runtime.wasmWrite(fd, p, n), its arguments on the stack at sp+8).
  go.print = (fd, s) => {
    const b = new TextEncoder().encode(s);
    new Uint8Array(buffer, heap, b.length).set(b);
    const sp = 64;
    go.mem.setUint32(sp + 8, fd, true);
    go.mem.setInt32(sp + 12, 0, true);
    go.mem.setUint32(sp + 16, heap, true);
    go.mem.setInt32(sp + 20, 0, true);
    go.mem.setInt32(sp + 24, b.length, true);
    heap += b.length;
    go.importObject.gojs["runtime.wasmWrite"](sp);
  };
  return go;
}

const fakeHost = () => {
  const h = { notified: 0, afterCore: (f) => f(), core: { crashNotify: () => h.notified++ } };
  return h;
};

const tick = () => new Promise((r) => setTimeout(r, 0));

test("a crash: what the runtime printed from the panic on goes in the run's .crash file", async () => {
  const go = fakeGo(), h = fakeHost();
  const c = new Crash(go, h);
  const path = "/data/crash1/crashes/runs/1-7.crash";
  fs.writeFile(path, new Uint8Array(0));
  c.begin(path);
  assert.ok(c.alive(path), "the run is marked alive");
  // Printed while Go ran before: not a crash, forgotten once Go returns.
  go.print(2, "a debug line\n");
  await tick();
  assert.equal(c.printed, "");
  // A crash: the program prints and ends at once.
  go.print(2, "earlier output\n");
  go.print(2, "panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n\t/app/main.go:5 +0x1d\n");
  go.print(1, "stdout isn't kept\n");
  go.exited = true;
  await tick();
  c.exited(2);
  assert.ok(c.alive(path), "alive until its crash is kept");
  await tick();
  assert.equal(dec.decode(fs.readFile(path)), "panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n\t/app/main.go:5 +0x1d\n");
  assert.ok(!c.alive(path), "an ended run isn't alive");
  assert.equal(go.written.length, 4, "the runtime's writes still go to the console");
});

test("an exit that isn't a crash (os.Exit(1), no panic printed) writes nothing", async () => {
  const go = fakeGo();
  const c = new Crash(go, fakeHost());
  const path = "/data/crash2/crashes/runs/2-7.crash";
  fs.writeFile(path, new Uint8Array(0));
  c.begin(path);
  go.print(2, "something went wrong\n");
  go.exited = true;
  c.exited(1);
  assert.equal(fs.readFile(path).length, 0);
});

test("a fatal error is a crash too", async () => {
  const go = fakeGo();
  const c = new Crash(go, fakeHost());
  const path = "/data/crash3/crashes/runs/3-7.crash";
  fs.writeFile(path, new Uint8Array(0));
  c.begin(path);
  go.print(2, "fatal error: all goroutines are asleep - deadlock!\n\ngoroutine 1 [chan receive]:\n");
  go.exited = true;
  c.exited(2);
  assert.match(dec.decode(fs.readFile(path)), /^fatal error: all goroutines are asleep/);
});

test("uncaught errors: an exception record each in the inbox, not fatal, each once, Go told", async () => {
  const go = fakeGo(), h = fakeHost();
  const c = new Crash(go, h);
  fs.writeFile("/data/crash4/crashes/runs/4-7.crash", new Uint8Array(0));
  c.begin("/data/crash4/crashes/runs/4-7.crash");
  const err = new TypeError("x is undefined");
  c.exception(err);
  c.exception(err); // the same again
  c.exception("a rejected string");
  const names = [];
  fs.memfs.readdir("/data/crash4/crashes/inbox", (e, ns) => names.push(...ns));
  assert.equal(names.length, 2);
  assert.ok(names.every((n) => /^exception-web-\w+\.json$/.test(n)));
  const recs = names.map((n) => JSON.parse(dec.decode(fs.readFile("/data/crash4/crashes/inbox/" + n))));
  const te = recs.find((r) => r.name === "TypeError");
  assert.equal(te.reason, "x is undefined");
  assert.equal(te.fatal, false);
  assert.equal(te.thread, "main");
  assert.ok(te.frames.length > 0 && te.frames[0].file);
  assert.ok(!te.stack.startsWith("TypeError"), "the stack without its message line");
  const s = recs.find((r) => r.name === "Uncaught");
  assert.equal(s.reason, "a rejected string");
  assert.equal(h.notified, 3 - 1);
  // Once Go has ended, the errors of calls into it aren't reports.
  go.exited = true;
  c.exception(new Error("Go program has already exited"));
  assert.equal(h.notified, 2);
  c.exited(0);
});

test("a stack's frames, V8's and Firefox's and Safari's", () => {
  assert.deepEqual(frames("    at set (http://localhost/asuka/views.js:12:5)\n    at http://localhost/web/natives.js:3:9"), [
    { function: "set", file: "http://localhost/asuka/views.js", line: 12 },
    { file: "http://localhost/web/natives.js", line: 3 },
  ]);
  assert.deepEqual(frames("set@http://localhost/asuka/views.js:12:5\n@http://localhost/web/natives.js:3:9"), [
    { function: "set", file: "http://localhost/asuka/views.js", line: 12 },
    { file: "http://localhost/web/natives.js", line: 3 },
  ]);
});

test("the browser and its system", () => {
  const mac = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15";
  assert.equal(systemOf({ userAgent: mac }), "macOS");
  assert.equal(browserOf({ userAgent: mac }), "Safari 18.1");
  const iphone = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1";
  assert.equal(systemOf({ userAgent: iphone }), "iOS 18.1");
  const android = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36";
  assert.equal(systemOf({ userAgent: android }), "Android 14");
  assert.equal(browserOf({ userAgent: android }), "Chrome 131");
  const firefox = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0";
  assert.equal(systemOf({ userAgent: firefox }), "Windows");
  assert.equal(browserOf({ userAgent: firefox }), "Firefox 133");
  const edge = { userAgent: "… Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0", userAgentData: { brands: [{ brand: "Not_A Brand", version: "24" }, { brand: "Chromium", version: "131" }, { brand: "Microsoft Edge", version: "131" }] } };
  assert.equal(browserOf(edge), "Microsoft Edge 131");
  const chrome = { userAgent: "", userAgentData: { brands: [{ brand: "Chromium", version: "131" }, { brand: "Google Chrome", version: "131" }, { brand: "Not_A Brand", version: "24" }] } };
  assert.equal(browserOf(chrome), "Chrome 131");
});

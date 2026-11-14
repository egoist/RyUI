// The hardware modules' wire formats, as asuka's Go packages read and write
// them (network, location, sensors, audio, audio/record, scan, imageedit,
// contacts, calendar, screen, biometric): node --test platform/web.
import { test } from "node:test";
import assert from "node:assert/strict";
import { networkState, positionJSON, accessState, encodeSample, quaternion, motionValues, Sensor, modules as sensorModules } from "../src/modules/hardware-sensors.js";
import { splitID, f64, encodeStatus, parseRecordArgs, recordingType, levelOf, nowPlaying, encodeCommands } from "../src/modules/hardware-audio.js";
import { encodeCodes, formatsOf, kindOf, target, rotatedSize, pixelate } from "../src/modules/hardware-camera.js";
import { vCard, iCalendar, orientation, userVerified, biometricKind, modules as moreModules } from "../src/modules/hardware-more.js";
import { modules as hardware, natives } from "../src/modules/hardware.js";

const le32 = (n) => { const b = new Uint8Array(4); new DataView(b.buffer).setUint32(0, n, true); return b; };
const cat = (...parts) => { const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0)); let i = 0; for (const p of parts) { out.set(p, i); i += p.length; } return out; };
const str = (s) => { const b = new TextEncoder().encode(s); return cat(le32(b.length), b); };
const f64b = (x) => { const b = new Uint8Array(8); new DataView(b.buffer).setFloat64(0, x, true); return b; };
const fakeHost = { prefix: () => "asuka.test.", options: {}, assets: "assets" };

test("network: u8 connected, type, metered, constrained", () => {
  assert.deepEqual([...networkState(false, { type: "wifi" })], [0, 0, 0, 0]);
  assert.deepEqual([...networkState(true, { type: "none" })], [0, 0, 0, 0]);
  assert.deepEqual([...networkState(true, { type: "wifi" })], [1, 1, 0, 0]);
  assert.deepEqual([...networkState(true, { type: "cellular", saveData: true })], [1, 2, 1, 1]);
  assert.deepEqual([...networkState(true, { type: "ethernet" })], [1, 3, 0, 0]);
  assert.deepEqual([...networkState(true, undefined)], [1, 4, 0, 0]); // the browser doesn't say: Other
});

test("network.next answers once the state changes; stop fails it with stopped", async () => {
  const target = new EventTarget();
  const saved = { add: globalThis.addEventListener, nav: Object.getOwnPropertyDescriptor(globalThis, "navigator") };
  let onLine = true;
  globalThis.addEventListener = target.addEventListener.bind(target);
  Object.defineProperty(globalThis, "navigator", { configurable: true, get: () => ({ onLine, connection: undefined }) });
  try {
    const { network } = sensorModules(fakeHost);
    const now = network.current();
    assert.deepEqual([...now], [1, 4, 0, 0]);
    assert.deepEqual([...network.next(new Uint8Array(0))], [1, 4, 0, 0]); // no last state: now
    const next = network.next(now);
    assert.ok(next instanceof Promise);
    onLine = false;
    target.dispatchEvent(new Event("offline"));
    assert.deepEqual([...await next], [0, 0, 0, 0]);
    const waiting = network.next(new Uint8Array([0, 0, 0, 0]));
    network.stop();
    await assert.rejects(waiting, { message: "stopped" });
  } finally {
    globalThis.addEventListener = saved.add;
    if (saved.nav) Object.defineProperty(globalThis, "navigator", saved.nav);
  }
});

test("location: JSON lat, lon, acc, alt, spd, crs, t", () => {
  const j = JSON.parse(positionJSON({ coords: { latitude: 37.5, longitude: -122.25, accuracy: 12, altitude: null, speed: 3, heading: NaN }, timestamp: 1700000000123.4 }));
  assert.deepEqual(j, { lat: 37.5, lon: -122.25, acc: 12, alt: 0, spd: 3, crs: -1, t: 1700000000123 });
});

test("locationAccess: the permission's state as three bytes; the background part computed", () => {
  assert.deepEqual([...accessState("granted")], [3, 1, 0]);
  assert.deepEqual([...accessState("denied")], [1, 1, 0]);
  assert.deepEqual([...accessState("prompt")], [0, 1, 0]);
  const m = hardware(fakeHost);
  assert.deepEqual(JSON.parse(m.locationBackground.capabilities()), { updates: false, regions: false, limit: 0, dwell: false, relaunch: false });
  assert.throws(() => m.locationBackground.monitor(), /computed by the Go code/);
});

// As sensors.decode reads them.
function decodeSamples(b) {
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const out = [];
  let p = 0;
  while (b.length - p >= 10) {
    const kind = b[p], t = v.getFloat64(p + 1, true), n = b[p + 9];
    const vals = [];
    for (let i = 0; i < n; i++) vals.push(v.getFloat64(p + 10 + 8 * i, true));
    out.push({ kind, t, vals });
    p += 10 + 8 * n;
  }
  return out;
}

test("sensors: samples as u8 sensor, f64 seconds, u8 count, f64s", () => {
  const b = cat(encodeSample(Sensor.Accelerometer, 1.5, [0, 0, 1]), encodeSample(Sensor.Light, 2, [300]));
  assert.deepEqual(decodeSamples(b), [{ kind: 0, t: 1.5, vals: [0, 0, 1] }, { kind: 5, t: 2, vals: [300] }]);
});

test("sensors: device motion in g and rad/s; orientation as a unit quaternion", () => {
  const v = motionValues({ accelerationIncludingGravity: { x: 0, y: 0, z: 9.80665 }, acceleration: { x: 0, y: 0, z: 0 }, rotationRate: { alpha: 180, beta: 90, gamma: 0 } });
  assert.deepEqual(v.slice(0, 6), [0, 0, 1, 0, 0, 0]); // face up: gravity's z is +1 g
  assert.ok(Math.abs(v[6] - Math.PI) < 1e-12 && Math.abs(v[7] - Math.PI / 2) < 1e-12 && v[8] === 0); // x from alpha, y from beta
  assert.deepEqual(quaternion(0, 0, 0), [0, 0, 0, 1]);
  const q = quaternion(90, 0, 0); // turned about z
  assert.ok(Math.abs(q[2] - Math.SQRT1_2) < 1e-12 && Math.abs(q[3] - Math.SQRT1_2) < 1e-12);
  const q2 = quaternion(30, 40, 50);
  assert.ok(Math.abs(Math.hypot(...q2) - 1) < 1e-12);
});

test("sensors: next waits for samples; stopAll fails it with stopped; no steps", async () => {
  const { sensors } = sensorModules(fakeHost);
  const p = sensors.next();
  sensors.stopAll();
  await assert.rejects(p, { message: "stopped" });
  assert.throws(() => sensors.steps(new Uint8Array(16)), { message: "not available" });
  assert.deepEqual([...await sensors.available(new Uint8Array([Sensor.Barometer]))], [0]);
  assert.deepEqual([...await sensors.available(new Uint8Array([Sensor.Pedometer]))], [0]);
});

test("audio: the player's ID, f64 args, the status", () => {
  const [id, rest] = splitID(cat(str("12"), f64b(3.25)));
  assert.equal(id, "12");
  assert.equal(f64(rest), 3.25);
  assert.deepEqual(splitID(new Uint8Array([9, 0, 0, 0, 1])), ["", new Uint8Array(0)]);
  const s = encodeStatus(1.5, NaN, true, false);
  const dv = new DataView(s.buffer);
  assert.equal(s.length, 18);
  assert.equal(dv.getFloat64(0, true), 1.5);
  assert.equal(dv.getFloat64(8, true), 0); // a stream's unknown duration
  assert.deepEqual([s[16], s[17]], [1, 0]);
});

// A fake navigator.mediaSession: what the page set, and its handlers.
function fakeSession() {
  const s = { handlers: {}, positions: [], metadata: undefined, playbackState: "none" };
  s.setActionHandler = (name, f) => {
    if (name === "stop" && s.noStop) throw new TypeError("not supported");
    if (f) s.handlers[name] = f;
    else delete s.handlers[name];
  };
  s.setPositionState = (p) => s.positions.push(p);
  return s;
}

test("nowPlaying: the Media Session's metadata, state, position and handlers; commands through next", async () => {
  const s = fakeSession();
  s.noStop = true; // a browser without the stop action
  const np = nowPlaying(fakeHost, { mediaSession: s });
  const bit = (...cs) => cs.reduce((m, c) => m | (1 << c), 0);
  const set = (o) => np.set(new TextEncoder().encode(JSON.stringify(o)));
  set({ title: "Spring", artist: "Vivaldi", album: "Seasons", artwork: "covers/spring.png", duration: 200, position: 12, rate: 1, state: "paused", commands: bit(1, 2, 3, 4, 7, 8), skip: 30 });
  assert.equal(s.metadata.title, "Spring");
  assert.equal(s.metadata.artist, "Vivaldi");
  assert.equal(s.metadata.album, "Seasons");
  assert.equal(s.metadata.artwork[0].src, "assets/covers/spring.png");
  assert.equal(s.playbackState, "paused");
  assert.deepEqual(s.positions.at(-1), { duration: 200, position: 12, playbackRate: 1 });
  assert.deepEqual(Object.keys(s.handlers).sort(), ["pause", "play", "seekforward", "seekto"]);

  // Commands come once "next" waits; one that came between polls waits for the next.
  const first = np.next();
  s.handlers.play();
  const read = (b) => { const v = new DataView(b.buffer); const out = []; for (let i = 0; i < b.length; i += 9) out.push([b[i], v.getFloat64(i + 1, true)]); return out; };
  assert.deepEqual(read(await first), [[1, 0]]);
  s.handlers.seekto({ seekTime: 42 });
  s.handlers.seekforward({}); // no offset: the interval
  assert.deepEqual(read(await np.next()), [[7, 42], [8, 30]]);
  assert.deepEqual(read(encodeCommands([[9, 15]])), [[9, 15]]);

  // A live stream: no position; playing; stopped is "none".
  set({ title: "Radio", rate: 1, position: 0, state: "playing", commands: bit(1, 2, 3) });
  assert.equal(s.positions.at(-1), undefined);
  assert.equal(s.playbackState, "playing");
  assert.deepEqual(Object.keys(s.handlers).sort(), ["pause", "play"]);
  set({ title: "Radio", rate: 1, position: 0, state: "stopped", commands: bit(1, 2, 3) });
  assert.equal(s.playbackState, "none");

  // Stopped following: the waiting poll fails, commands are dropped.
  const waiting = np.next();
  np.stop();
  await assert.rejects(waiting, { message: "stopped" });
  s.handlers.play();
  np.clear();
  assert.equal(s.metadata, null);
  assert.equal(s.playbackState, "none");
  assert.deepEqual(Object.keys(s.handlers), []);
  // No Media Session: nothing to do.
  assert.equal(nowPlaying(fakeHost, {}).set(new TextEncoder().encode("{}")), null);
});

test("audioRecord: start's args as record.Start writes them", () => {
  // u8 quality, u32 max ms, u8 flags, u32-length title, u32-length path.
  const b = cat(new Uint8Array([1]), le32(90000), new Uint8Array([3]), str("Recording"), str("/data/app/a.m4a"));
  assert.deepEqual(parseRecordArgs(b), { quality: 1, maxMs: 90000, background: true, stream: true, title: "Recording", path: "/data/app/a.m4a" });
  assert.deepEqual(parseRecordArgs(cat(new Uint8Array(6), le32(0), le32(0))), { quality: 0, maxMs: 0, background: false, stream: false, title: "", path: "" });
  assert.deepEqual(recordingType((t) => t.startsWith("audio/mp4")), { type: "audio/mp4;codecs=mp4a.40.2", ext: "m4a" });
  assert.deepEqual(recordingType((t) => t === "audio/webm;codecs=opus"), { type: "audio/webm;codecs=opus", ext: "webm" });
  assert.deepEqual(recordingType(() => false), { type: "", ext: "webm" }); // the browser's default
  assert.equal(recordingType((t) => t === "video/mp4", true).ext, "mp4");
  assert.equal(levelOf(0), 0);
  assert.equal(levelOf(0.0005), 0); // below -60 dB
  assert.equal(levelOf(1), 255);
  assert.equal(levelOf(0.5), 128);
});

test("scan: codes as u32 count, then kind and value strings", () => {
  const b = encodeCodes([{ kind: "qr", value: "héllo" }, { kind: "ean13", value: "4006381333931" }]);
  assert.deepEqual(b, cat(le32(2), str("qr"), str("héllo"), str("ean13"), str("4006381333931")));
  assert.deepEqual(formatsOf("qr, ean13"), ["qr_code", "ean_13"]);
  assert.equal(formatsOf("").length, 13);
  assert.equal(kindOf("data_matrix"), "datamatrix");
  assert.equal(kindOf("upc_a"), "upca");
});

test("imageEdit: resize targets, turned sizes, pixelation aligned to the image", () => {
  assert.deepEqual(target({ width: 1080 }, 4032, 3024), [1080, 810]);
  assert.deepEqual(target({ height: 100 }, 400, 200), [200, 100]);
  assert.deepEqual(target({}, 400, 200), [400, 200]);
  assert.deepEqual(rotatedSize(400, 200, 90), [200, 400]);
  assert.deepEqual(rotatedSize(100, 100, 45), [142, 142]);
  // A 4×2 image, pixelated in squares of 2 over its right half only.
  const d = new Uint8ClampedArray([
    0, 0, 0, 255, 10, 10, 10, 255, 20, 0, 0, 255, 40, 0, 0, 255,
    0, 0, 0, 255, 10, 10, 10, 255, 60, 0, 0, 255, 80, 0, 0, 255,
  ]);
  pixelate(d, 4, 2, 2, 0, 4, 2, 2);
  assert.deepEqual([...d.slice(8, 12)], [50, 0, 0, 255]);
  assert.deepEqual([...d.slice(28, 32)], [50, 0, 0, 255]);
  assert.deepEqual([...d.slice(4, 8)], [10, 10, 10, 255]); // outside: as it was
});

test("contacts.add's vCard and calendar.add's iCalendar", () => {
  const v = vCard({ name: "Ada Lovelace", givenName: "Ada", familyName: "Lovelace", organization: "Analytical; Engines", phones: [{ label: "mobile", value: "+44 1" }], emails: [{ label: "", value: "ada@example.com" }] });
  assert.match(v, /^BEGIN:VCARD\r\nVERSION:3.0\r\nN:Lovelace;Ada;;;\r\nFN:Ada Lovelace\r\nORG:Analytical\\; Engines\r\nTEL;TYPE=MOBILE:\+44 1\r\nEMAIL:ada@example.com\r\nEND:VCARD\r\n$/);
  const ics = iCalendar({ title: "Launch, day", startMs: Date.UTC(2026, 0, 2, 9, 30), endMs: Date.UTC(2026, 0, 2, 10), location: "HQ" }, "x@asuka", Date.UTC(2026, 0, 1));
  assert.match(ics, /DTSTART:20260102T093000Z\r\nDTEND:20260102T100000Z\r\nSUMMARY:Launch\\, day\r\nLOCATION:HQ\r\n/);
  assert.match(ics, /UID:x@asuka\r\nDTSTAMP:20260101T000000Z/);
});

test("screen, biometric, tracking and what the web hasn't", async () => {
  assert.equal(orientation(0), null);
  assert.equal(orientation(2), "portrait-primary");
  assert.equal(orientation(4), "landscape-primary");
  const auth = new Uint8Array(37);
  assert.equal(userVerified(auth.buffer), false);
  auth[32] = 0x05;
  assert.equal(userVerified(auth.buffer), true);
  assert.equal(biometricKind("Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)"), 2);
  assert.equal(biometricKind("Mozilla/5.0 (Macintosh)"), 1);
  const m = moreModules(fakeHost);
  assert.throws(() => m.review.request(), /review.request isn't on the web/);
  assert.deepEqual([...m.integrity.supported()], [0]);
  assert.deepEqual([...m.appleAuth.available()], [0]);
  assert.throws(() => m.appleAuth.signIn(), { message: "not available" });
  assert.throws(() => m.ageRange.request(), { message: "not available" });
  assert.equal(m.photoLibrary.current(), "denied");
  assert.deepEqual([...m.contacts.request()], [1]);
  assert.throws(() => m.contacts.list(), { message: "not allowed" });
  assert.throws(() => m.transcribe.file(), { message: "not available" });
  assert.deepEqual(JSON.parse(m.transcribe.check(new TextEncoder().encode("fr-FR"))), { onDevice: false, language: "fr-FR" });
  // background: next waits while a task lives, and fails "ended" when the last ends.
  const enc = (o) => new TextEncoder().encode(JSON.stringify(o));
  m.background.start(enc({ id: 1, title: "Upload" }));
  const next = m.background.next();
  m.background.end(enc({ id: 1 }));
  await assert.rejects(next, { message: "ended" });
  await assert.rejects(m.background.next(), { message: "ended" });
});

test("every module the hardware file has, and camera.View's native view", () => {
  const all = hardware(fakeHost);
  for (const name of ["network", "location", "locationAccess", "locationBackground", "sensors", "speech", "audio", "audioRecord", "nowPlaying", "camera", "scan", "vision", "imageEdit", "biometric", "review", "tracking", "integrity", "ageRange", "appleAuth", "webAuth", "periodic", "background", "widget", "splash", "screen", "transcribe", "photoLibrary", "library", "contacts", "calendar"]) {
    assert.ok(all[name], name);
  }
  assert.equal(typeof natives["asuka.camera"], "function");
});

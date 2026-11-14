// asuka/sdk/bluetooth's module on the web, Web Bluetooth (a fake
// navigator.bluetooth), as internal/bluetoothspec has its wire:
// node --test platform/web.
import { test } from "node:test";
import assert from "node:assert/strict";
import { modules, properties, requestOptions, advertisement, gattError, chooseError, b64, unb64 } from "../src/modules/bluetooth.js";
import { readFileSync } from "node:fs";

const enc = (o) => new TextEncoder().encode(JSON.stringify(o ?? {}));
const json = async (p) => JSON.parse(await p);
const HR = "0000180d-0000-1000-8000-00805f9b34fb";
const HRM = "00002a37-0000-1000-8000-00805f9b34fb";
const BODY = "00002a38-0000-1000-8000-00805f9b34fb";
const CTRL = "00002a39-0000-1000-8000-00805f9b34fb";
const CCCD = "00002902-0000-1000-8000-00805f9b34fb";
const tick = () => new Promise((r) => setTimeout(r, 0));
const dv = (a) => new DataView(new Uint8Array(a).buffer);
const domErr = (name, msg) => new DOMException(msg, name);

// --- a fake Web Bluetooth ---

class FakeDescriptor {
  constructor(uuid, value) { this.uuid = uuid; this.value = dv(value); this.writes = []; }
  async readValue() { return this.value; }
  async writeValue(v) {
    if (this.uuid === CCCD) throw domErr("SecurityError", "writeValue() called on blocklisted object marked exclude-writes.");
    this.writes.push([...v]); this.value = dv(v);
  }
}

class FakeCharacteristic extends EventTarget {
  constructor(server, uuid, props, value = [], descriptors = []) {
    super();
    this.server = server; this.uuid = uuid; this.properties = props; this.value = dv(value);
    this.descriptors = descriptors; this.writes = []; this.notifying = false;
  }
  check() { if (!this.server.connected) throw domErr("NetworkError", "GATT Server is disconnected. Cannot perform GATT operations."); }
  async readValue() {
    this.check();
    await tick();
    this.dispatchEvent(new Event("characteristicvaluechanged")); // as the spec says: a read fires it too
    return this.value;
  }
  async writeValueWithResponse(v) { this.check(); this.writes.push(["with", [...v]]); this.value = dv(v); }
  async writeValueWithoutResponse(v) { this.check(); this.writes.push(["without", [...v]]); }
  async startNotifications() { this.check(); this.notifying = true; return this; }
  async stopNotifications() { this.check(); this.notifying = false; return this; }
  async getDescriptors() {
    if (!this.descriptors.length) throw domErr("NotFoundError", "No Descriptors found in Characteristic.");
    return this.descriptors;
  }
  notify(v) { this.value = dv(v); this.dispatchEvent(new Event("characteristicvaluechanged")); }
}

class FakeServer {
  constructor(device) { this.device = device; this.connected = false; this.services = []; this.connects = 0; this.fail = null; }
  async connect() {
    this.connects++;
    const attempt = this.attempt = {};
    await tick();
    if (attempt.aborted) throw domErr("AbortError", "Connection attempt aborted.");
    if (this.fail) { const f = this.fail; this.fail = null; throw f; }
    this.connected = true;
    return this;
  }
  disconnect() {
    if (this.attempt) this.attempt.aborted = true; // as Chrome: a pending connect fails
    if (!this.connected) return;
    this.connected = false;
    this.device.dispatchEvent(new Event("gattserverdisconnected"));
  }
  async getPrimaryServices() { return this.services; }
  async getPrimaryService(u) {
    const s = this.services.find((s) => s.uuid === u);
    if (!s) throw domErr("NotFoundError", "No Services matching UUID found in Device.");
    return s;
  }
}

class FakeDevice extends EventTarget {
  constructor(id, name) { super(); this.id = id; this.name = name; this.gatt = new FakeServer(this); }
}

function heartRateDevice() {
  const d = new FakeDevice("dev-1", "Polar H10");
  const hrm = new FakeCharacteristic(d.gatt, HRM, { notify: true }, [0, 60], [new FakeDescriptor(CCCD, [0, 0])]);
  const body = new FakeCharacteristic(d.gatt, BODY, { read: true }, [1]);
  const ctrl = new FakeCharacteristic(d.gatt, CTRL, { write: true, writeWithoutResponse: true }, []);
  d.gatt.services = [{ uuid: HR, isPrimary: true, getCharacteristics: async () => [hrm, body, ctrl] }];
  return { d, hrm, body, ctrl };
}

class FakeBluetooth extends EventTarget {
  constructor() { super(); this.available = true; this.chooser = null; this.requests = []; this.permitted = []; }
  async getAvailability() { return this.available; }
  async requestDevice(o) {
    this.requests.push(o);
    const d = this.chooser?.(o);
    if (!d) throw domErr("NotFoundError", "User cancelled the requestDevice() chooser.");
    return d;
  }
  async getDevices() { return this.permitted; }
}

function withNavigator(nav, f) {
  return async () => {
    const saved = Object.getOwnPropertyDescriptor(globalThis, "navigator");
    Object.defineProperty(globalThis, "navigator", { configurable: true, get: () => nav });
    try {
      await f();
    } finally {
      if (saved) Object.defineProperty(globalThis, "navigator", saved);
      else delete globalThis.navigator;
    }
  };
}

// --- the wire's parts ---

test("properties: the core spec's bits", () => {
  assert.equal(properties({ read: true, notify: true }), 0x12);
  assert.equal(properties({ broadcast: true, writeWithoutResponse: true, write: true, indicate: true, authenticatedSignedWrites: true, reliableWrite: true }), 0xed);
  assert.equal(properties({ writableAuxiliaries: true }), 0x80);
  assert.equal(properties(undefined), 0);
});

test("requestOptions: filters as requestDevice's; none (or an empty one) is every device", () => {
  assert.deepEqual(requestOptions({ filters: [{ services: [HR] }, { namePrefix: "Polar", manufacturer: 107 }, { name: "X" }], optionalServices: [BODY] }), {
    filters: [{ services: [HR] }, { namePrefix: "Polar", manufacturerData: [{ companyIdentifier: 107 }] }, { name: "X" }],
    optionalServices: [BODY],
  });
  assert.deepEqual(requestOptions({}), { acceptAllDevices: true, optionalServices: [] });
  assert.deepEqual(requestOptions({ filters: [{ services: [HR] }, {}] }), { acceptAllDevices: true, optionalServices: [HR] });
  assert.deepEqual(requestOptions({ filters: [{ manufacturer: 0 }] }), { filters: [{ manufacturerData: [{ companyIdentifier: 0 }] }], optionalServices: [] });
});

test("advertisement: requestLEScan's event as the wire's", () => {
  const e = {
    device: { id: "d", name: "Tag" }, rssi: -61, txPower: -4, name: "Tag", uuids: [HR],
    manufacturerData: new Map([[76, dv([2, 21])]]), serviceData: new Map([[HR, dv([9])]]),
  };
  assert.deepEqual(advertisement(e, 5), {
    device: { id: "d", name: "Tag" }, rssi: -61, localName: "Tag", services: [HR],
    serviceData: { [HR]: "CQ==" }, manufacturerData: { 76: "AhU=" }, txPower: -4, time: 5,
  });
  assert.deepEqual(advertisement({ device: { id: "e" }, rssi: null, uuids: [] }, 1), { device: { id: "e" }, rssi: 0, time: 1 });
});

test("errors: DOMExceptions as the wire's", () => {
  const m = (e, op) => gattError(e, op).message;
  assert.equal(m(domErr("NetworkError", "GATT Server is disconnected."), "read"), "disconnected");
  assert.equal(m(domErr("NotSupportedError", "GATT operation not permitted."), "read"), "att 2: GATT operation not permitted.");
  assert.equal(m(domErr("NotSupportedError", "GATT operation not permitted."), "write"), "att 3: GATT operation not permitted.");
  assert.equal(m(domErr("NotSupportedError", "GATT Error: Not supported."), "read"), "att 6: GATT Error: Not supported.");
  assert.equal(m(domErr("InvalidModificationError", "GATT Error: invalid attribute length."), "write"), "att 13: GATT Error: invalid attribute length.");
  assert.equal(m(domErr("SecurityError", "GATT operation not authorized."), "read"), "att 8: GATT operation not authorized.");
  assert.match(m(domErr("SecurityError", "readValue() called on blocklisted object marked exclude-reads."), "read"), /^denied: /);
  assert.equal(m(domErr("InvalidStateError", "GATT Characteristic no longer exists."), "read"), "not found");
  assert.equal(chooseError(domErr("NotFoundError", "User cancelled the requestDevice() chooser.")).message, "canceled");
  assert.equal(chooseError(domErr("NotFoundError", "Bluetooth adapter not available.")).message, "unsupported");
  assert.match(chooseError(domErr("SecurityError", "Must be handling a user gesture to show a permission request.")).message, /only right after the user taps/);
  assert.equal(chooseError(domErr("SecurityError", "Access to the feature \"bluetooth\" is disallowed by permissions policy.")).message, "denied");
  assert.deepEqual([...unb64(b64(new Uint8Array([0, 255, 7])))], [0, 255, 7]);
  assert.deepEqual([...unb64(null)], []);
});

// --- the module ---

test("no Web Bluetooth (Safari, Firefox): unsupported, and the rest not available", withNavigator({}, async () => {
  const { bluetooth } = modules();
  assert.deepEqual(await json(bluetooth.state()), { state: "unsupported" });
  assert.deepEqual(await json(bluetooth.request()), { state: "unsupported" });
  for (const m of ["choose", "scan", "known", "connected", "connect", "advertise", "serve", "mtu"]) {
    await assert.rejects(async () => bluetooth[m](enc({})), { message: /^not available: this browser has no Web Bluetooth/ }, m);
  }
  assert.equal(bluetooth.restore(), "{}");
  assert.equal(bluetooth.stopBackgroundScan(), "{}");
}));

test("state: getAvailability, and availabilitychanged as state events through next; stop fails a waiting next", async () => {
  const nav = { bluetooth: new FakeBluetooth() };
  await withNavigator(nav, async () => {
    const { bluetooth } = modules();
    assert.deepEqual(await json(bluetooth.state()), { state: "on" });
    nav.bluetooth.available = false;
    assert.deepEqual(await json(bluetooth.request()), { state: "off" });
    const next = bluetooth.next();
    assert.ok(next instanceof Promise);
    nav.bluetooth.dispatchEvent(Object.assign(new Event("availabilitychanged"), { value: true }));
    assert.deepEqual(JSON.parse(await next), [{ type: "state", state: "on" }]);
    // Queued while no next waits, then all at once.
    nav.bluetooth.dispatchEvent(Object.assign(new Event("availabilitychanged"), { value: false }));
    nav.bluetooth.dispatchEvent(Object.assign(new Event("availabilitychanged"), { value: true }));
    assert.deepEqual(JSON.parse(await bluetooth.next()), [{ type: "state", state: "off" }, { type: "state", state: "on" }]);
    const waiting = bluetooth.next();
    assert.equal(bluetooth.stop(), "{}");
    await assert.rejects(waiting, { message: "stopped" });
  })();
});

test("scan: not available without requestLEScan; with it, advertisements to each scan they match", async () => {
  const nav = { bluetooth: new FakeBluetooth() };
  await withNavigator(nav, async () => {
    const { bluetooth } = modules();
    await assert.rejects(bluetooth.scan(enc({ id: 1 })), { message: /^not available: browsers let a page find devices only through their chooser/ });

    const stopped = [];
    const opts = [];
    nav.bluetooth.requestLEScan = async (o) => { opts.push(o); return { active: true, stop() { stopped.push(o); } }; };
    assert.equal(await bluetooth.scan(enc({ id: 1, services: [HR] })), "{}");
    assert.equal(await bluetooth.scan(enc({ id: 2, duplicates: true })), "{}");
    assert.deepEqual(opts, [
      { filters: [{ services: [HR] }], keepRepeatedDevices: false },
      { acceptAllAdvertisements: true, keepRepeatedDevices: true },
    ]);
    const dev = new FakeDevice("hr", "Strap");
    const ad = (uuids, rssi) => Object.assign(new Event("advertisementreceived"), { device: dev, rssi, uuids, manufacturerData: new Map(), serviceData: new Map() });
    nav.bluetooth.dispatchEvent(ad([HR], -50));
    nav.bluetooth.dispatchEvent(ad([HR], -55)); // scan 1 hears it once
    const evs = JSON.parse(await bluetooth.next());
    assert.deepEqual(evs.map((e) => [e.scan, e.advertisement.rssi]), [[1, -50], [2, -50], [2, -55]]);
    assert.deepEqual(evs[0].advertisement.device, { id: "hr", name: "Strap" });
    assert.equal(bluetooth.stopScan(enc({ id: 1 })), "{}");
    assert.equal(stopped.length, 1);
    // A scanned device connects.
    assert.equal(await bluetooth.connect(enc({ conn: 9, device: "hr" })), "{}");
    // The adapter off: the scans stop, the connections end.
    nav.bluetooth.dispatchEvent(Object.assign(new Event("availabilitychanged"), { value: false }));
    assert.deepEqual(JSON.parse(await bluetooth.next()), [
      { type: "state", state: "off" },
      { type: "scanStopped", scan: 2, error: "off" },
      { type: "disconnected", conn: 9, reason: "off", error: "Bluetooth turned off" },
    ]);
    // Denied: requestLEScan's prompt refused.
    nav.bluetooth.available = true;
    nav.bluetooth.requestLEScan = async () => { throw domErr("NotAllowedError", "User denied permission to scan for BLE devices."); };
    await assert.rejects(bluetooth.scan(enc({ id: 3 })), { message: "denied" });
    // A stopped scan hears nothing.
    nav.bluetooth.dispatchEvent(ad([HR], -40));
    assert.equal(await Promise.race([bluetooth.next().then(() => "event"), tick().then(() => "none")]), "none");
    bluetooth.stop();
  })();
});

test("choose, known, connected: the chooser's device, getDevices'", async () => {
  const nav = { bluetooth: new FakeBluetooth() };
  await withNavigator(nav, async () => {
    const { bluetooth } = modules();
    const { d } = heartRateDevice();
    await assert.rejects(bluetooth.choose(enc({})), { message: "canceled" });
    nav.bluetooth.chooser = () => d;
    assert.deepEqual(await json(bluetooth.choose(enc({ filters: [{ services: [HR] }], optionalServices: [BODY] }))), { id: "dev-1", name: "Polar H10" });
    assert.deepEqual(nav.bluetooth.requests.at(-1), { filters: [{ services: [HR] }], optionalServices: [BODY] });
    nav.bluetooth.requestDevice = async () => { throw domErr("SecurityError", "Must be handling a user gesture to show a permission request."); };
    await assert.rejects(bluetooth.choose(enc({})), { message: /only right after the user taps/ });

    const old = new FakeDevice("dev-0", "");
    nav.bluetooth.permitted = [old];
    assert.deepEqual(await json(bluetooth.known(enc({ ids: ["dev-0", "nope", "dev-1"] }))), [{ id: "dev-0" }, { id: "dev-1", name: "Polar H10" }]);
    assert.deepEqual(await json(bluetooth.connected(enc({}))), []);
    await bluetooth.connect(enc({ conn: 1, device: "dev-1" }));
    assert.deepEqual(await json(bluetooth.connected(enc({}))), [{ id: "dev-1", name: "Polar H10" }]);
    assert.deepEqual(await json(bluetooth.connected(enc({ services: [HR] }))), [{ id: "dev-1", name: "Polar H10" }]);
    assert.deepEqual(await json(bluetooth.connected(enc({ services: [BODY] }))), []);
    await assert.rejects(bluetooth.connect(enc({ conn: 2, device: "gone" })), { message: "not found" });
  })();
});

test("connect, services, read, write, descriptors, subscribe, disconnect", async () => {
  const nav = { bluetooth: new FakeBluetooth() };
  await withNavigator(nav, async () => {
    const { bluetooth } = modules();
    const { d, hrm, body, ctrl } = heartRateDevice();
    nav.bluetooth.chooser = () => d;
    await bluetooth.choose(enc({}));
    await assert.rejects(bluetooth.services(enc({ conn: 1 })), { message: "disconnected" });
    assert.equal(await bluetooth.connect(enc({ conn: 1, device: "dev-1" })), "{}");

    const tree = await json(bluetooth.services(enc({ conn: 1 })));
    assert.deepEqual(tree, [{
      handle: 1, uuid: HR, primary: true, characteristics: [
        { handle: 2, uuid: HRM, properties: 0x10, descriptors: [{ handle: 3, uuid: CCCD }] },
        { handle: 4, uuid: BODY, properties: 0x02 },
        { handle: 5, uuid: CTRL, properties: 0x0c },
      ],
    }]);
    assert.deepEqual(await json(bluetooth.services(enc({ conn: 1 }))), tree); // the same handles

    // read: the bytes themselves.
    assert.deepEqual([...await bluetooth.read(enc({ conn: 1, handle: 4 }))], [1]);
    await assert.rejects(bluetooth.read(enc({ conn: 1, handle: 5 })), { message: "att 2: read not permitted" });
    await assert.rejects(bluetooth.read(enc({ conn: 1, handle: 99 })), { message: "not found" });
    await assert.rejects(bluetooth.read(enc({ conn: 1, handle: 3 })), { message: "not found" }); // a descriptor's handle

    // write, with and without response.
    assert.equal(await bluetooth.write(enc({ conn: 1, handle: 5, value: b64(new Uint8Array([1, 2])), response: true })), "{}");
    assert.equal(await bluetooth.write(enc({ conn: 1, handle: 5, value: b64(new Uint8Array([3])) })), "{}");
    assert.deepEqual(ctrl.writes, [["with", [1, 2]], ["without", [3]]]);
    await assert.rejects(bluetooth.write(enc({ conn: 1, handle: 4, value: "AA==", response: true })), { message: "att 3: write not permitted" });

    // descriptors: the CCCD's read; its write is the blocklist's.
    assert.deepEqual([...await bluetooth.readDescriptor(enc({ conn: 1, handle: 3 }))], [0, 0]);
    await assert.rejects(bluetooth.writeDescriptor(enc({ conn: 1, handle: 3, value: "AQA=", response: true })), { message: /^denied: / });

    // subscribe: values as "value" events, the connection's handle.
    await assert.rejects(bluetooth.subscribe(enc({ conn: 1, handle: 4 })), { message: "att 6: the characteristic doesn't notify" });
    assert.equal(await bluetooth.subscribe(enc({ conn: 1, handle: 2 })), "{}");
    assert.ok(hrm.notifying);
    hrm.notify([0, 72]);
    hrm.notify([0, 73]);
    assert.deepEqual(JSON.parse(await bluetooth.next()), [
      { type: "value", conn: 1, handle: 2, value: b64(new Uint8Array([0, 72])) },
      { type: "value", conn: 1, handle: 2, value: b64(new Uint8Array([0, 73])) },
    ]);
    // A second connection to the device shares its server and its notifications.
    assert.equal(await bluetooth.connect(enc({ conn: 2, device: "dev-1" })), "{}");
    await bluetooth.services(enc({ conn: 2 }));
    await bluetooth.subscribe(enc({ conn: 2, handle: 2 }));
    hrm.notify([0, 80]);
    assert.deepEqual(JSON.parse(await bluetooth.next()).map((e) => e.conn), [1, 2]);
    assert.equal(await bluetooth.unsubscribe(enc({ conn: 1, handle: 2 })), "{}");
    assert.ok(hrm.notifying); // conn 2's still
    // Closing one connection leaves the device connected for the other.
    assert.equal(bluetooth.disconnect(enc({ conn: 1 })), "{}");
    assert.ok(d.gatt.connected);
    await assert.rejects(bluetooth.read(enc({ conn: 1, handle: 4 })), { message: "disconnected" });

    // The device goes: a "disconnected" event, the calls waiting fail.
    const next = bluetooth.next();
    d.gatt.disconnect();
    assert.deepEqual(JSON.parse(await next), [{ type: "disconnected", conn: 2, reason: "unknown", error: "the device disconnected (the browser doesn't say why)" }]);
    await assert.rejects(bluetooth.read(enc({ conn: 2, handle: 4 })), { message: "disconnected" });
    await assert.rejects(async () => bluetooth.mtu(enc({ conn: 2 })), { message: /^not available: browsers don't give a connection's MTU/ });
    await assert.rejects(async () => bluetooth.rssi(enc({ conn: 2 })), { message: /^not available: browsers don't give a connection's signal strength/ });

    // Go's own disconnect: no event; the calls waiting on it fail "disconnected".
    await bluetooth.connect(enc({ conn: 3, device: "dev-1" }));
    await bluetooth.services(enc({ conn: 3 }));
    const reading = bluetooth.read(enc({ conn: 3, handle: 4 }));
    bluetooth.disconnect(enc({ conn: 3 }));
    await assert.rejects(reading, { message: "disconnected" });
    assert.equal(d.gatt.connected, false);
    assert.equal(await Promise.race([bluetooth.next().then(() => "event"), tick().then(() => "none")]), "none");
    bluetooth.stop();
  })();
});

test("a read of a subscribed characteristic isn't a value event; a pending connect canceled", async () => {
  const nav = { bluetooth: new FakeBluetooth() };
  await withNavigator(nav, async () => {
    const { bluetooth } = modules();
    const d = new FakeDevice("n", "Both");
    const ch = new FakeCharacteristic(d.gatt, HRM, { read: true, notify: true }, [5]);
    d.gatt.services = [{ uuid: HR, isPrimary: true, getCharacteristics: async () => [ch] }];
    nav.bluetooth.chooser = () => d;
    await bluetooth.choose(enc({}));
    await bluetooth.connect(enc({ conn: 1, device: "n" }));
    await bluetooth.services(enc({ conn: 1 }));
    await bluetooth.subscribe(enc({ conn: 1, handle: 2 }));
    assert.deepEqual([...await bluetooth.read(enc({ conn: 1, handle: 2 }))], [5]);
    ch.notify([6]);
    assert.deepEqual(JSON.parse(await bluetooth.next()), [{ type: "value", conn: 1, handle: 2, value: b64(new Uint8Array([6])) }]);
    bluetooth.disconnect(enc({ conn: 1 }));

    // "disconnect" while connecting: connect fails "canceled".
    const connecting = bluetooth.connect(enc({ conn: 2, device: "n" }));
    bluetooth.disconnect(enc({ conn: 2 }));
    await assert.rejects(connecting, { message: "canceled" });
    // A connect the device doesn't answer; off when the adapter is.
    d.gatt.fail = domErr("NetworkError", "Bluetooth Device is no longer in range.");
    await assert.rejects(bluetooth.connect(enc({ conn: 3, device: "n" })), { message: "disconnected" });
    nav.bluetooth.available = false;
    d.gatt.fail = domErr("NetworkError", "Bluetooth adapter not available.");
    await assert.rejects(bluetooth.connect(enc({ conn: 4, device: "n" })), { message: "off" });
    nav.bluetooth.available = true;

    // AutoConnect: waits for the device's advertisement, then connects.
    d.gatt.fail = domErr("NetworkError", "Bluetooth Device is no longer in range.");
    let watching = null;
    d.watchAdvertisements = async ({ signal }) => { watching = signal; };
    const auto = bluetooth.connect(enc({ conn: 5, device: "n", autoConnect: true }));
    while (!watching) await tick();
    d.dispatchEvent(new Event("advertisementreceived"));
    assert.equal(await auto, "{}");
    assert.ok(watching.aborted);
    assert.equal(d.gatt.connects, 6);
    bluetooth.stop();
  })();
});

test("the peripheral role isn't on the web; priority answers {}", withNavigator({ bluetooth: new FakeBluetooth() }, async () => {
  const { bluetooth } = modules();
  for (const m of ["advertise", "serve", "respond", "notify"]) {
    await assert.rejects(async () => bluetooth[m](enc({})), { message: "not available: the web has no peripheral role" }, m);
  }
  assert.equal(bluetooth.stopAdvertise(enc({ id: 1 })), "{}");
  assert.equal(bluetooth.unserve(enc({ id: 1 })), "{}");
  assert.throws(() => bluetooth.priority(enc({ conn: 7, priority: "high" })), { message: "disconnected" });
  assert.equal(bluetooth.restore(), "{}");
}));

test("the module is among the host's built-in ones", () => {
  // (Not by making a Modules: the other modules' listeners keep node running.)
  const src = readFileSync(new URL("../src/modules.js", import.meta.url), "utf8");
  assert.match(src, /import \{ modules as bluetooth \} from "\.\/modules\/bluetooth\.js";/);
  assert.match(src, /\.\.\.bluetooth\(this\.h\)/);
  assert.equal(typeof modules().bluetooth.choose, "function");
});

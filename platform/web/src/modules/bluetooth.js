// asuka/sdk/bluetooth in the browser: Web Bluetooth (Chrome and Edge, on
// the desktop and Android; not Safari or Firefox), as the "bluetooth"
// module's wire (internal/bluetoothspec): JSON arguments and replies,
// byte values in base64, "read" and "readDescriptor" answering the raw
// bytes. host/fake's bluetooth.go is the reference for its semantics.
//
//   - state, request: getAvailability(), "on" or "off" (no adapter, or
//     off; also a page the permissions policy keeps from Bluetooth);
//     availabilitychanged sends "state" events. The browser asks for each
//     device, in its chooser: "request" asks nothing. Without
//     navigator.bluetooth (Safari, Firefox, a page not on https):
//     "unsupported", and every other method fails "not available".
//   - choose: requestDevice, its filters the ChooseArgs' (acceptAllDevices
//     with none). The browser shows it only while the user's tap is
//     recent (its transient activation: five seconds in Chrome): Go's
//     Choose, called from a tap's goroutine, gets there in milliseconds.
//   - scan: only through requestLEScan (Chrome's, behind the
//     experimental web platform features flag), one per Go scan, their
//     advertisements (one event on navigator.bluetooth for all of them)
//     given to each scan they match, once per device unless it asked for
//     duplicates. Without it: "not available".
//   - known: the devices of this page (chosen, scanned) and getDevices()'s
//     (those the user allowed the site before, where the browser has it).
//     connected: those of them this page is connected to (the browser
//     doesn't tell a site of other connections).
//   - connect: gatt.connect(), with AutoConnect waiting for the device's
//     advertisements (watchAdvertisements) between tries where the
//     browser has it. A device has one GATT server for the page: Go's
//     connections to it share it, the last one closed disconnecting it.
//     gattserverdisconnected: a "disconnected" event, reason "unknown"
//     (the web doesn't say why), "off" when the adapter went.
//   - services: getPrimaryServices, getCharacteristics, getDescriptors,
//     each attribute's handle its own for the connection (the same object
//     the same handle). Only the services the chooser's filters and
//     OptionalServices name are there: the browser hides the others, and
//     the attributes of its blocklist (a SecurityError: "denied").
//   - read, write, readDescriptor, writeDescriptor, subscribe
//     (startNotifications: the browser picks notifications or indications
//     itself), unsubscribe: one at a time per device (Chrome refuses an
//     operation while another is in progress). A read of a subscribed
//     characteristic doesn't come as a "value" event too.
//   - mtu, requestMtu, rssi: "not available" (a browser doesn't give
//     them); priority: {}. The peripheral role (advertise, serve,
//     respond, notify): "not available". restore, stopBackgroundScan: {}.

const dec = new TextDecoder();

const NO_WEB_BLUETOOTH = "not available: this browser has no Web Bluetooth (Chrome and Edge have it; Safari and Firefox don't)";
const NO_SCAN = "not available: browsers let a page find devices only through their chooser (bluetooth.Choose)";
const NO_PERIPHERAL = "not available: the web has no peripheral role";
const GESTURE = "the browser shows its Bluetooth prompts only right after the user taps: call it as the user taps (within a few seconds)";

const fail = (msg) => new Error(msg);
const ok = "{}";

/** args decodes a call's JSON arguments ({} for none). */
export function args(b) {
  const s = b?.length ? dec.decode(b) : "";
  return s ? JSON.parse(s) : {};
}

/** b64 is bytes (a Uint8Array, a DataView, an ArrayBuffer) as encoding/json writes a []byte. */
export function b64(v) {
  const b = bytes(v);
  let s = "";
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000));
  return btoa(s);
}

/** unb64 is a []byte's base64 (null: none) as bytes. */
export function unb64(s) {
  if (!s) return new Uint8Array(0);
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** bytes copies a value's bytes (a DataView's, the browser's own buffer, may change). */
export function bytes(v) {
  if (!v) return new Uint8Array(0);
  if (v instanceof Uint8Array) return v.slice();
  if (ArrayBuffer.isView(v)) return new Uint8Array(v.buffer.slice(v.byteOffset, v.byteOffset + v.byteLength));
  return new Uint8Array(v.slice(0));
}

/** properties is a BluetoothCharacteristicProperties as the core spec's bits (bluetoothspec's Prop*). */
export function properties(p) {
  if (!p) return 0;
  return (p.broadcast ? 0x01 : 0) | (p.read ? 0x02 : 0) | (p.writeWithoutResponse ? 0x04 : 0) |
    (p.write ? 0x08 : 0) | (p.notify ? 0x10 : 0) | (p.indicate ? 0x20 : 0) |
    (p.authenticatedSignedWrites ? 0x40 : 0) | (p.reliableWrite || p.writableAuxiliaries ? 0x80 : 0);
}

/** requestOptions are requestDevice's for ChooseArgs. */
export function requestOptions(a) {
  const optionalServices = [...(a.optionalServices ?? [])];
  const filters = [];
  let all = !a.filters?.length;
  for (const f of a.filters ?? []) {
    const w = {};
    if (f.services?.length) w.services = [...f.services];
    if (f.name) w.name = f.name;
    if (f.namePrefix) w.namePrefix = f.namePrefix;
    if (f.manufacturer != null) w.manufacturerData = [{ companyIdentifier: f.manufacturer }];
    if (!Object.keys(w).length) all = true; // a filter of nothing matches every device
    filters.push(w);
  }
  if (all) {
    // The filters' services are those the page may use once connected:
    // with every device shown, it may use them only if it names them.
    for (const f of filters) for (const s of f.services ?? []) if (!optionalServices.includes(s)) optionalServices.push(s);
    return { acceptAllDevices: true, optionalServices };
  }
  return { filters, optionalServices };
}

/** device is a BluetoothDevice as the wire's Device. */
export function device(d) {
  return d.name ? { id: d.id, name: d.name } : { id: d.id };
}

/** advertisement is a BluetoothAdvertisingEvent as the wire's Advertisement. */
export function advertisement(e, now = Date.now()) {
  const a = { device: device(e.device), rssi: e.rssi ?? 0, time: now };
  if (e.name) a.localName = e.name;
  if (e.uuids?.length) a.services = [...e.uuids];
  if (e.serviceData?.size) {
    a.serviceData = {};
    for (const [k, v] of e.serviceData) a.serviceData[k] = b64(v);
  }
  if (e.manufacturerData?.size) {
    a.manufacturerData = {};
    for (const [k, v] of e.manufacturerData) a.manufacturerData[String(k)] = b64(v);
  }
  if (e.txPower != null) a.txPower = e.txPower;
  return a; // connectable: the web doesn't say
}

/**
 * gattError is a GATT operation's DOMException (op: "read", "write" or
 * another) as the wire's error. Chrome's: NetworkError for a server
 * disconnected (and a device not paired), NotSupportedError for an
 * operation the characteristic's properties don't allow or the device
 * refused, InvalidModificationError for a value too long, SecurityError
 * for the blocklist's attributes and an authorization the device wants,
 * InvalidStateError for an attribute that's gone.
 */
export function gattError(e, op) {
  const m = String(e?.message ?? e);
  switch (e?.name) {
    case "NetworkError":
      if (/not paired/i.test(m)) return fail("att 5: " + m); // insufficient authentication
      if (/in progress/i.test(m)) return fail(m);
      return fail("disconnected");
    case "NotSupportedError":
      if (/not permitted/i.test(m)) return fail(op === "read" ? "att 2: " + m : op === "write" ? "att 3: " + m : "att 6: " + m);
      if (/not supported/i.test(m)) return fail("att 6: " + m);
      return fail("att 14: " + m); // Chrome's "GATT Error Unknown.": the device's, unlikely
    case "InvalidModificationError":
      return fail("att 13: " + m); // invalid attribute value length
    case "SecurityError":
      if (/authoriz|authenticat/i.test(m) && !/blocklist/i.test(m)) return fail("att 8: " + m);
      return fail("denied: " + m);
    case "NotAllowedError":
      return fail("denied: " + m);
    case "InvalidStateError":
      if (/no longer exists/i.test(m)) return fail("not found");
      return fail("disconnected");
    case "NotFoundError":
      return fail("not found");
  }
  return e instanceof Error ? e : fail(m);
}

/** chooseError is requestDevice's (or requestLEScan's) DOMException as the wire's error. */
export function chooseError(e) {
  const m = String(e?.message ?? e);
  switch (e?.name) {
    case "NotFoundError":
      if (/adapter not available|no bluetooth adapter/i.test(m)) return fail("unsupported");
      if (/globally disabled|blocked|denied/i.test(m)) return fail("denied");
      return fail("canceled"); // "User cancelled the requestDevice() chooser."
    case "SecurityError":
      if (/gesture|activation/i.test(m)) return fail(GESTURE + " (" + m + ")");
      return fail("denied"); // the permissions policy, an iframe
    case "NotAllowedError":
      if (/gesture|activation/i.test(m)) return fail(GESTURE + " (" + m + ")");
      return fail("denied"); // the user said no (requestLEScan's prompt)
    case "AbortError":
      return fail("canceled");
  }
  return e instanceof Error ? e : fail(m);
}

const sameBytes = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

export function modules() {
  const bt = () => globalThis.navigator?.bluetooth ?? null;
  const need = () => {
    const b = bt();
    if (!b) throw fail(NO_WEB_BLUETOOTH);
    init(b);
    return b;
  };

  // --- events ("next") ---
  const queue = [];
  let waiting = null; // { resolve, reject }
  // Queued while no "next" waits, advertisements too: they're only the
  // running scans', which Go stops itself (a "stop" may come just before
  // a scan starts, its "next" after).
  const push = (e) => {
    queue.push(e);
    if (waiting) {
      const w = waiting;
      waiting = null;
      w.resolve(JSON.stringify(queue.splice(0)));
    }
  };

  // --- devices, scans, connections ---
  const devices = new Map(); // id -> BluetoothDevice: chosen, scanned, getDevices'
  const records = new Map(); // id -> the device's record (its GATT server's users)
  const scans = new Map(); // Go's id -> { le, services, duplicates, seen }
  const conns = new Map(); // Go's conn -> connection
  let inited = null;

  const keep = (d) => {
    devices.set(d.id, d);
    return d;
  };

  function init(b) {
    if (inited === b) return;
    inited = b;
    b.addEventListener?.("availabilitychanged", (e) => {
      const on = !!e.value;
      push({ type: "state", state: on ? "on" : "off" });
      if (on) return;
      for (const [id, s] of scans) {
        try { s.le?.stop(); } catch {}
        push({ type: "scanStopped", scan: id, error: "off" });
      }
      scans.clear();
      for (const c of [...conns.values()]) {
        if (!c.up) continue;
        push({ type: "disconnected", conn: c.id, reason: "off", error: "Bluetooth turned off" });
        end(c, "disconnected");
      }
    });
    b.addEventListener?.("advertisementreceived", (e) => {
      if (!e.device) return;
      keep(e.device);
      let a = null;
      for (const [id, s] of scans) {
        if (!s.le) continue; // not started yet
        if (s.services.length && !s.services.some((u) => e.uuids?.includes(u))) continue;
        if (!s.duplicates) {
          if (s.seen.has(e.device.id)) continue;
          s.seen.add(e.device.id);
        }
        a ??= advertisement(e);
        push({ type: "advertisement", scan: id, advertisement: a });
      }
    });
  }

  const permitted = async (b) => {
    if (!b.getDevices) return;
    try {
      for (const d of await b.getDevices()) keep(d);
    } catch {}
  };

  const off = async (b) => {
    try {
      return b.getAvailability && !(await b.getAvailability());
    } catch {
      return false;
    }
  };

  // record is a device's: its connections (Go may have several to one
  // device, which share its one GATT server), their subscriptions by
  // characteristic, the reads in flight, its operations' queue.
  function record(d) {
    let r = records.get(d.id);
    if (r) return r;
    r = { device: d, conns: new Set(), subs: new Map(), reading: new Map(), tail: Promise.resolve() };
    records.set(d.id, r);
    d.addEventListener?.("gattserverdisconnected", () => {
      // The browser forgets the notifications (and, by the spec, the
      // attributes' objects: those of the next connection are new).
      for (const [ch, s] of r.subs) ch.removeEventListener?.("characteristicvaluechanged", s.listener);
      r.subs.clear();
      for (const c of [...r.conns]) {
        if (!c.up) continue; // connecting: its connect fails
        push({ type: "disconnected", conn: c.id, reason: "unknown", error: "the device disconnected (the browser doesn't say why)" });
        end(c, "disconnected");
      }
    });
    const changed = () => {
      for (const c of r.conns) {
        if (!c.up) continue;
        c.tree = null;
        c.discovering = null;
        push({ type: "servicesChanged", conn: c.id });
      }
    };
    for (const t of ["serviceadded", "servicechanged", "serviceremoved"]) d.addEventListener?.(t, changed);
    return r;
  }

  // serial runs f after the device's operations before it (Chrome takes
  // one GATT operation at a time), unless the connection c has ended
  // meanwhile (another of the device's may still be up).
  function serial(c, f) {
    const r = c.rec;
    const run = r.tail.then(() => {
      if (c.ended) throw fail("disconnected");
      return f();
    });
    r.tail = run.catch(() => {});
    return run;
  }

  // guard is p, or "disconnected" (or why) once the connection ends first.
  function guard(c, p) {
    return new Promise((resolve, reject) => {
      const f = (why) => reject(fail(why));
      c.pending.add(f);
      p.then((v) => { c.pending.delete(f); resolve(v); }, (e) => { c.pending.delete(f); reject(e); });
    });
  }

  // end forgets a connection, failing what waits on it with why.
  function end(c, why) {
    if (c.ended) return;
    c.ended = true;
    c.up = false;
    conns.delete(c.id);
    c.rec.conns.delete(c);
    for (const f of [...c.pending]) f(why);
    c.pending.clear();
    for (const [ch, s] of c.rec.subs) {
      if (!s.conns.delete(c) || s.conns.size) continue;
      c.rec.subs.delete(ch);
      ch.removeEventListener?.("characteristicvaluechanged", s.listener);
      if (c.rec.device.gatt?.connected) ch.stopNotifications?.().catch(() => {});
    }
  }

  const upConn = (id) => {
    const c = conns.get(id);
    if (!c || !c.up) throw fail("disconnected");
    return c;
  };

  const attr = (c, handle, kind) => {
    const a = c.handles.get(handle);
    if (!a || a.kind !== kind) throw fail("not found");
    return a.obj;
  };

  const handleOf = (c, obj) => {
    let h = c.back.get(obj);
    if (!h) {
      h = ++c.nextHandle;
      c.back.set(obj, h);
    }
    return h;
  };

  const none = (p) => p.catch((e) => {
    if (e?.name === "NotFoundError") return [];
    throw e;
  });

  async function discover(c) {
    const gatt = c.rec.device.gatt;
    const handles = new Map();
    const tree = [];
    for (const s of await none(gatt.getPrimaryServices())) {
      const ws = { handle: handleOf(c, s), uuid: s.uuid, primary: s.isPrimary !== false };
      handles.set(ws.handle, { kind: "service", obj: s });
      const chars = [];
      for (const ch of await none(s.getCharacteristics())) {
        const wc = { handle: handleOf(c, ch), uuid: ch.uuid, properties: properties(ch.properties) };
        handles.set(wc.handle, { kind: "char", obj: ch });
        const ds = [];
        for (const d of ch.getDescriptors ? await none(ch.getDescriptors()) : []) {
          const wd = { handle: handleOf(c, d), uuid: d.uuid };
          handles.set(wd.handle, { kind: "desc", obj: d });
          ds.push(wd);
        }
        if (ds.length) wc.descriptors = ds;
        chars.push(wc);
      }
      if (chars.length) ws.characteristics = chars;
      tree.push(ws);
    }
    c.handles = handles; // the attributes gone since fail "not found"
    c.tree = tree;
    return tree;
  }

  // deliver sends a characteristic's value to the connections subscribed.
  const deliver = (r, ch, v) => {
    const s = r.subs.get(ch);
    if (!s) return;
    for (const [c, handle] of s.conns) push({ type: "value", conn: c.id, handle, value: b64(v) });
  };

  async function connectGatt(c, auto) {
    const d = c.rec.device;
    for (;;) {
      try {
        return await d.gatt.connect();
      } catch (e) {
        if (!auto || c.ended || e?.name !== "NetworkError" || !d.watchAdvertisements) throw e;
        // AutoConnect: wait for the device to advertise, then try again.
        const ac = new AbortController();
        let heard;
        const done = new Promise((resolve) => { heard = resolve; });
        d.addEventListener("advertisementreceived", heard);
        ac.signal.addEventListener("abort", heard);
        c.stopWaiting = () => ac.abort();
        try {
          await d.watchAdvertisements({ signal: ac.signal });
          await done;
        } catch {
          throw e; // the browser can't watch: the connect's own error
        } finally {
          d.removeEventListener("advertisementreceived", heard);
          ac.abort();
          c.stopWaiting = null;
        }
        if (c.ended) throw fail("canceled");
      }
    }
  }

  const notOnWeb = (msg) => (b) => {
    need();
    throw fail(msg);
  };

  const state = async () => {
    const b = bt();
    if (!b) return JSON.stringify({ state: "unsupported" });
    init(b);
    // A browser without getAvailability lets its chooser tell the user.
    return JSON.stringify({ state: (await off(b)) ? "off" : "on" });
  };

  const bluetooth = {
    state,
    request: state,

    next: () => {
      if (queue.length) return JSON.stringify(queue.splice(0));
      waiting?.reject(fail("stopped"));
      return new Promise((resolve, reject) => { waiting = { resolve, reject }; });
    },

    stop: () => {
      const w = waiting;
      waiting = null;
      w?.reject(fail("stopped"));
      return ok;
    },

    scan: async (body) => {
      const a = args(body);
      const b = need();
      if (!b.requestLEScan) throw fail(NO_SCAN);
      const services = a.services ?? [];
      const duplicates = !!a.duplicates;
      const s = { le: null, services, duplicates, seen: new Set() };
      scans.set(a.id, s);
      const opts = services.length
        ? { filters: services.map((u) => ({ services: [u] })), keepRepeatedDevices: duplicates }
        : { acceptAllAdvertisements: true, keepRepeatedDevices: duplicates };
      let le;
      try {
        le = await b.requestLEScan(opts);
      } catch (e) {
        if (scans.get(a.id) === s) scans.delete(a.id);
        if (await off(b)) throw fail("off");
        throw chooseError(e);
      }
      if (scans.get(a.id) !== s) {
        le.stop(); // stopped meanwhile
        return ok;
      }
      s.le = le;
      return ok;
    },

    stopScan: (body) => {
      const { id } = args(body);
      const s = scans.get(id);
      scans.delete(id);
      try { s?.le?.stop(); } catch {}
      return ok;
    },

    stopBackgroundScan: () => ok,

    choose: async (body) => {
      const a = args(body);
      const b = need();
      // requestDevice first, before any await: the user's tap is recent.
      let d;
      try {
        d = await b.requestDevice(requestOptions(a));
      } catch (e) {
        throw chooseError(e);
      }
      return JSON.stringify(device(keep(d)));
    },

    known: async (body) => {
      const { ids = [] } = args(body);
      const b = need();
      await permitted(b);
      return JSON.stringify(ids.filter((id) => devices.has(id)).map((id) => device(devices.get(id))));
    },

    connected: async (body) => {
      const { services = [] } = args(body);
      const b = need();
      await permitted(b);
      const out = [];
      for (const d of devices.values()) {
        if (!d.gatt?.connected) continue;
        if (services.length) {
          let has = false;
          for (const u of services) {
            try {
              await d.gatt.getPrimaryService(u);
              has = true;
              break;
            } catch {}
          }
          if (!has) continue;
        }
        out.push(device(d));
      }
      return JSON.stringify(out);
    },

    connect: async (body) => {
      const a = args(body);
      const b = need();
      if (!devices.has(a.device)) await permitted(b);
      const d = devices.get(a.device);
      if (!d?.gatt) throw fail("not found");
      const r = record(d);
      const c = {
        id: a.conn, rec: r, up: false, ended: false, pending: new Set(), stopWaiting: null,
        handles: new Map(), back: new WeakMap(), nextHandle: 0, tree: null, discovering: null,
      };
      conns.get(a.conn) && end(conns.get(a.conn), "disconnected");
      conns.set(a.conn, c);
      r.conns.add(c);
      try {
        await guard(c, connectGatt(c, !!a.autoConnect));
      } catch (e) {
        if (c.ended) throw fail("canceled"); // "disconnect" while connecting
        end(c, "disconnected");
        if (await off(b)) throw fail("off");
        throw gattError(e, "connect");
      }
      if (c.ended) throw fail("canceled");
      c.up = true;
      return ok;
    },

    disconnect: (body) => {
      const { conn } = args(body);
      const c = conns.get(conn);
      if (!c) return ok;
      const r = c.rec;
      c.stopWaiting?.();
      end(c, c.up ? "disconnected" : "canceled");
      // The device's server is the page's: the last connection disconnects it.
      if (!r.conns.size) {
        try { r.device.gatt?.disconnect(); } catch {}
      }
      return ok;
    },

    services: async (body) => {
      const c = upConn(args(body).conn);
      if (c.tree) return JSON.stringify(c.tree);
      c.discovering ??= serial(c, () => discover(c));
      const p = c.discovering;
      try {
        return JSON.stringify(await guard(c, p));
      } catch (e) {
        if (c.discovering === p) c.discovering = null;
        throw gattError(e, "services");
      }
    },

    read: async (body) => {
      const a = args(body);
      const c = upConn(a.conn);
      const ch = attr(c, a.handle, "char");
      if (!ch.properties?.read) throw fail("att 2: read not permitted");
      const r = c.rec;
      try {
        return await guard(c, serial(c, async () => {
          // The read's value comes as characteristicvaluechanged too: not
          // a notification, kept from the subscriptions.
          const held = [];
          r.reading.set(ch, held);
          let v;
          try {
            v = bytes(await ch.readValue());
          } catch (e) {
            r.reading.delete(ch);
            for (const x of held) deliver(r, ch, x);
            throw e;
          }
          r.reading.delete(ch);
          const i = held.findLastIndex((x) => sameBytes(x, v));
          if (i >= 0) held.splice(i, 1);
          for (const x of held) deliver(r, ch, x);
          return v;
        }));
      } catch (e) {
        throw gattError(e, "read");
      }
    },

    write: async (body) => {
      const a = args(body);
      const c = upConn(a.conn);
      const ch = attr(c, a.handle, "char");
      const p = ch.properties ?? {};
      if (a.response ? !p.write : !p.writeWithoutResponse) throw fail("att 3: write not permitted");
      const v = unb64(a.value);
      try {
        await guard(c, serial(c, () => a.response
          ? (ch.writeValueWithResponse ?? ch.writeValue).call(ch, v)
          : (ch.writeValueWithoutResponse ?? ch.writeValue).call(ch, v)));
      } catch (e) {
        throw gattError(e, "write");
      }
      return ok;
    },

    readDescriptor: async (body) => {
      const a = args(body);
      const c = upConn(a.conn);
      const d = attr(c, a.handle, "desc");
      try {
        return bytes(await guard(c, serial(c, () => d.readValue())));
      } catch (e) {
        throw gattError(e, "read");
      }
    },

    writeDescriptor: async (body) => {
      const a = args(body);
      const c = upConn(a.conn);
      const d = attr(c, a.handle, "desc");
      const v = unb64(a.value);
      try {
        await guard(c, serial(c, () => d.writeValue(v)));
      } catch (e) {
        throw gattError(e, "write");
      }
      return ok;
    },

    subscribe: async (body) => {
      // Indicate: the browser picks notifications or indications itself.
      const a = args(body);
      const c = upConn(a.conn);
      const ch = attr(c, a.handle, "char");
      if (!ch.properties?.notify && !ch.properties?.indicate) throw fail("att 6: the characteristic doesn't notify");
      const r = c.rec;
      try {
        await guard(c, serial(c, async () => {
          let s = r.subs.get(ch);
          if (!s) {
            await ch.startNotifications();
            s = r.subs.get(ch);
            if (!s) {
              s = {
                conns: new Map(),
                listener: (e) => {
                  const v = bytes(e.target?.value ?? ch.value);
                  const held = r.reading.get(ch);
                  if (held) held.push(v);
                  else deliver(r, ch, v);
                },
              };
              ch.addEventListener("characteristicvaluechanged", s.listener);
              r.subs.set(ch, s);
            }
          }
          if (!c.ended) s.conns.set(c, a.handle);
          else if (!s.conns.size) { // the connection ended meanwhile
            r.subs.delete(ch);
            ch.removeEventListener("characteristicvaluechanged", s.listener);
            if (r.device.gatt?.connected) await ch.stopNotifications().catch(() => {});
          }
        }));
      } catch (e) {
        throw gattError(e, "subscribe");
      }
      return ok;
    },

    unsubscribe: async (body) => {
      const a = args(body);
      const c = upConn(a.conn);
      const ch = attr(c, a.handle, "char");
      const r = c.rec;
      const s = r.subs.get(ch);
      if (!s || !s.conns.delete(c) || s.conns.size) return ok;
      r.subs.delete(ch);
      ch.removeEventListener("characteristicvaluechanged", s.listener);
      try {
        await guard(c, serial(c, () => ch.stopNotifications()));
      } catch (e) {
        throw gattError(e, "subscribe");
      }
      return ok;
    },

    mtu: notOnWeb("not available: browsers don't give a connection's MTU"),
    requestMtu: notOnWeb("not available: browsers don't give a connection's MTU"),
    rssi: notOnWeb("not available: browsers don't give a connection's signal strength"),

    priority: (body) => {
      need();
      upConn(args(body).conn);
      return ok;
    },

    advertise: notOnWeb(NO_PERIPHERAL),
    stopAdvertise: () => ok,
    serve: notOnWeb(NO_PERIPHERAL),
    unserve: () => ok,
    respond: notOnWeb(NO_PERIPHERAL),
    notify: notOnWeb(NO_PERIPHERAL),

    restore: () => ok,
  };
  return { bluetooth };
}

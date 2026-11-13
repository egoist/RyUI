// asuka/sdk/network, asuka/sdk/location and asuka/sdk/sensors in the browser.
//
//   - network: navigator.onLine and the Network Information API
//     (navigator.connection, Chrome's): "current", "next" (answered once
//     the state isn't the one given: u8 connected, u8 type, u8 metered, u8
//     constrained) and "stop". The type is Wi-Fi, cellular or Ethernet only
//     where the browser says (Chrome on Android); elsewhere Other. Metered:
//     cellular; constrained: the user's Data Saver (saveData).
//   - location: the Geolocation API: "current", "next", "stop", as JSON
//     (lat, lon, acc, alt, t); "denied" when the user refused. "name" needs
//     a geocoder, which a browser hasn't.
//   - sensors: the Generic Sensor API where the browser has it
//     (Accelerometer, Gyroscope, Magnetometer, AbsoluteOrientationSensor,
//     AmbientLightSensor: Chrome), else DeviceMotion and DeviceOrientation
//     events (Safari asks the user, on a tap). Samples as Android's host
//     sends them: u8 sensor, f64 seconds, u8 count, count f64s;
//     accelerations in g, rotation rates in radians per second, the field
//     in microteslas, device motion's attitude as a quaternion. No
//     barometer or pedometer: a page can't read them.

const G = 9.80665;
const deg = Math.PI / 180;

// --- network ---

/** networkState is the network as asuka/sdk/network's reply: u8 connected, u8 type, u8 metered, u8 constrained. */
export function networkState(onLine, c) {
  if (!onLine || c?.type === "none") return new Uint8Array([0, 0, 0, 0]);
  const type = { wifi: 1, cellular: 2, ethernet: 3 }[c?.type] ?? 4;
  return new Uint8Array([1, type, type === 2 ? 1 : 0, c?.saveData ? 1 : 0]);
}

const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

function network() {
  const waits = []; // { last, resolve, reject }
  let listening = false;
  const now = () => networkState(navigator.onLine, navigator.connection);
  const changed = () => {
    const s = now();
    for (let i = waits.length - 1; i >= 0; i--) {
      if (!same(waits[i].last, s)) {
        waits[i].resolve(s);
        waits.splice(i, 1);
      }
    }
  };
  const listen = () => {
    if (listening) return;
    listening = true;
    addEventListener("online", changed);
    addEventListener("offline", changed);
    navigator.connection?.addEventListener?.("change", changed);
  };
  return {
    current: () => now(),
    next: (last) => {
      const s = now();
      if (!last.length || !same(last, s)) return s;
      listen();
      return new Promise((resolve, reject) => waits.push({ last: last.slice(), resolve, reject }));
    },
    stop: () => {
      for (const w of waits.splice(0)) w.reject(new Error("stopped"));
    },
  };
}

// --- location ---

/** positionJSON is a GeolocationPosition as asuka/sdk/location's reply. */
export function positionJSON(p) {
  const c = p.coords;
  return JSON.stringify({ lat: c.latitude, lon: c.longitude, acc: c.accuracy, alt: c.altitude ?? 0, t: Math.round(p.timestamp) });
}

function positionError(e) {
  if (e?.code === 1) return new Error("denied"); // PERMISSION_DENIED
  return new Error(e?.message || "location: no position");
}

function location() {
  let watch = null; // watchPosition's id
  let unseen = null, last = null;
  let next = []; // { resolve, reject }
  const geo = () => {
    if (!navigator.geolocation) throw new Error("asuka: location isn't in this browser");
    return navigator.geolocation;
  };
  const stop = () => {
    if (watch != null) geo().clearWatch(watch);
    watch = null;
    unseen = last = null;
  };
  return {
    current: () => new Promise((resolve, reject) => {
      geo().getCurrentPosition((p) => resolve(positionJSON(p)), (e) => reject(positionError(e)), { enableHighAccuracy: true, maximumAge: 0 });
    }),
    next: () => new Promise((resolve, reject) => {
      if (unseen) {
        const u = unseen;
        unseen = null;
        resolve(u);
      } else next.push({ resolve, reject });
      if (watch != null) return;
      const fix = (p) => {
        if (watch == null) return; // stopped meanwhile
        const s = positionJSON(p);
        if (s === last) return; // the watch's and the first fix's, the same
        last = s;
        const ws = next;
        next = [];
        if (ws.length) for (const w of ws) w.resolve(s);
        else unseen = s;
      };
      const failed = (e) => {
        // A fix that didn't come is transient; a refusal isn't.
        if (e.code !== 1) return;
        stop();
        for (const w of next.splice(0)) w.reject(positionError(e));
      };
      watch = geo().watchPosition(fix, failed, { enableHighAccuracy: true });
      // The first is the position now: a browser may not give a watch one
      // until the device moves.
      geo().getCurrentPosition(fix, failed, { enableHighAccuracy: true, maximumAge: 30000 });
    }),
    stop: () => {
      stop();
      for (const w of next.splice(0)) w.reject(new Error("stopped"));
    },
    name: () => { throw new Error("asuka: location.name isn't on the web: a browser has no geocoder (ask a geocoding service with net/http)"); },
  };
}

// --- sensors ---

// As sensors.Sensor.
export const Sensor = { Accelerometer: 0, Gyroscope: 1, Magnetometer: 2, Motion: 3, Barometer: 4, Light: 5, Pedometer: 6 };

/** encodeSample is a sample on the wire: u8 sensor, f64 seconds, u8 count, count f64s. */
export function encodeSample(kind, seconds, values) {
  const b = new Uint8Array(10 + 8 * values.length);
  const v = new DataView(b.buffer);
  b[0] = kind;
  v.setFloat64(1, seconds, true);
  b[9] = values.length;
  values.forEach((x, i) => v.setFloat64(10 + 8 * i, x, true));
  return b;
}

/**
 * quaternion is the device-to-Earth (East, North, Up) rotation of a
 * DeviceOrientation's alpha, beta and gamma (degrees, Z-X'-Y''), as x, y,
 * z, w: the W3C spec's.
 */
export function quaternion(alpha, beta, gamma) {
  const x = (beta || 0) * deg / 2, y = (gamma || 0) * deg / 2, z = (alpha || 0) * deg / 2;
  const cX = Math.cos(x), cY = Math.cos(y), cZ = Math.cos(z);
  const sX = Math.sin(x), sY = Math.sin(y), sZ = Math.sin(z);
  return [
    sX * cY * cZ - cX * sY * sZ,
    cX * sY * cZ + sX * cY * sZ,
    cX * cY * sZ + sX * sY * cZ,
    cX * cY * cZ - sX * sY * sZ,
  ];
}

/** motionValues is device motion's 9 values (gravity and the user's acceleration in g, the rotation rate in rad/s) from a DeviceMotionEvent. */
export function motionValues(e) {
  const ag = e.accelerationIncludingGravity || {}, a = e.acceleration || {}, r = e.rotationRate || {};
  const lin = [a.x ?? 0, a.y ?? 0, a.z ?? 0];
  const grav = [(ag.x ?? 0) - lin[0], (ag.y ?? 0) - lin[1], (ag.z ?? 0) - lin[2]];
  // rotationRate's alpha, beta, gamma are about x, y, z (as browsers give them).
  return [...grav.map((v) => v / G), ...lin.map((v) => v / G), (r.alpha ?? 0) * deg, (r.beta ?? 0) * deg, (r.gamma ?? 0) * deg];
}

const generic = (name) => typeof globalThis[name] === "function" ? globalThis[name] : null;

/** probeGeneric starts a Generic Sensor briefly: whether it reads. */
function probeGeneric(Cls, opts = {}) {
  return new Promise((resolve) => {
    let s;
    try { s = new Cls({ frequency: 10, ...opts }); } catch { resolve(false); return; }
    const done = (ok) => { clearTimeout(t); try { s.stop(); } catch {} resolve(ok); };
    const t = setTimeout(() => done(false), 1500);
    s.addEventListener("reading", () => done(true));
    s.addEventListener("error", () => done(false));
    try { s.start(); } catch { done(false); }
  });
}

/** probeEvent waits briefly for a window event whose test passes. */
function probeEvent(name, test) {
  return new Promise((resolve) => {
    const on = (e) => { if (test(e)) done(true); };
    const done = (ok) => { clearTimeout(t); removeEventListener(name, on); resolve(ok); };
    const t = setTimeout(() => done(false), 1500);
    addEventListener(name, on);
  });
}

// Safari asks for motion and orientation (on a tap).
const askable = () => typeof DeviceMotionEvent !== "undefined" && typeof DeviceMotionEvent.requestPermission === "function";

async function allowMotion() {
  if (!askable()) return true;
  try {
    const [m, o] = await Promise.all([
      DeviceMotionEvent.requestPermission(),
      typeof DeviceOrientationEvent?.requestPermission === "function" ? DeviceOrientationEvent.requestPermission() : "granted",
    ]);
    return m === "granted" && o === "granted";
  } catch {
    return false;
  }
}

function sensors() {
  let buffer = []; // encoded samples
  const waiting = [];
  const running = new Map(); // kind -> stop()
  const known = new Map(); // kind -> Promise<bool>

  const add = (kind, seconds, values) => {
    buffer.push(encodeSample(kind, seconds, values));
    if (buffer.length > 1400) buffer.splice(0, 200); // the Go side behind: the oldest go
    const w = waiting.shift();
    if (w) w.resolve(flush());
  };
  const flush = () => {
    let n = 0;
    for (const b of buffer) n += b.length;
    const out = new Uint8Array(n);
    let p = 0;
    for (const b of buffer) { out.set(b, p); p += b.length; }
    buffer = [];
    return out;
  };
  const motionEvents = () => typeof DeviceMotionEvent !== "undefined";
  const touch = () => (navigator.maxTouchPoints || 0) > 0;

  const available = (kind) => {
    if (!known.has(kind)) known.set(kind, probe(kind));
    return known.get(kind);
  };
  // probe finds where a sensor's readings come from: "generic" (the
  // Generic Sensor API), "events" (DeviceMotion and DeviceOrientation), or
  // nowhere (false).
  const probe = async (kind) => {
    // Motion events only on touch screens (a desktop browser has the
    // events, not the sensors). Phones and tablets that ask first (Safari)
    // all have the motion sensors: they can't be probed before the user
    // allows them.
    const events = (test) => {
      if (!motionEvents() || !touch()) return false;
      if (askable()) return "events";
      return probeEvent("devicemotion", test).then((ok) => ok && "events");
    };
    const gen = async (name) => !!generic(name) && await probeGeneric(generic(name)) && "generic";
    switch (kind) {
      case Sensor.Accelerometer:
        return await gen("Accelerometer") || events((e) => e.accelerationIncludingGravity?.x != null);
      case Sensor.Gyroscope:
        return await gen("Gyroscope") || events((e) => e.rotationRate?.alpha != null);
      case Sensor.Magnetometer:
        return gen("Magnetometer");
      case Sensor.Motion:
        return events((e) => e.accelerationIncludingGravity?.x != null && e.rotationRate?.alpha != null);
      case Sensor.Light:
        return gen("AmbientLightSensor");
    }
    return false; // a barometer, a pedometer: not in a browser
  };
  let orientation = null; // Promise: whether AbsoluteOrientationSensor reads

  const listen = (name, f) => {
    addEventListener(name, f);
    return () => removeEventListener(name, f);
  };
  const genericSensor = (Cls, hz, read) => {
    const s = new Cls({ frequency: hz });
    s.addEventListener("reading", () => read(s));
    s.addEventListener("error", (e) => console.warn("asuka: sensor:", e.error?.message || e));
    s.start();
    return () => s.stop();
  };

  const start = async (kind, micros) => {
    stop(kind);
    const source = await available(kind);
    if (!source) throw new Error("not available");
    if (source === "events" && !await allowMotion()) throw new Error("not allowed");
    const hz = Math.min(60, Math.max(1, 1e6 / Math.max(micros, 5000)));
    const secs = (t) => t / 1000;
    let stopFn;
    switch (kind) {
      case Sensor.Accelerometer: {
        const A = generic("Accelerometer");
        if (source === "generic") stopFn = genericSensor(A, hz, (s) => add(kind, secs(s.timestamp), [s.x / G, s.y / G, s.z / G]));
        else stopFn = listen("devicemotion", (e) => {
          const a = e.accelerationIncludingGravity;
          if (a?.x != null) add(kind, secs(e.timeStamp), [a.x / G, a.y / G, a.z / G]);
        });
        break;
      }
      case Sensor.Gyroscope: {
        const Gy = generic("Gyroscope");
        if (source === "generic") stopFn = genericSensor(Gy, hz, (s) => add(kind, secs(s.timestamp), [s.x, s.y, s.z]));
        else stopFn = listen("devicemotion", (e) => {
          const r = e.rotationRate;
          if (r?.alpha != null) add(kind, secs(e.timeStamp), [r.alpha * deg, r.beta * deg, r.gamma * deg]); // about x, y, z
        });
        break;
      }
      case Sensor.Magnetometer:
        stopFn = genericSensor(generic("Magnetometer"), hz, (s) => add(kind, secs(s.timestamp), [s.x, s.y, s.z]));
        break;
      case Sensor.Light:
        stopFn = genericSensor(generic("AmbientLightSensor"), hz, (s) => add(kind, secs(s.timestamp), [s.illuminance]));
        break;
      case Sensor.Motion: {
        // The newest orientation, sent with each motion: a quaternion where
        // it's relative to north, else none (the Go side then has the tilt,
        // without a heading).
        let q = null;
        const stops = [];
        const O = generic("AbsoluteOrientationSensor");
        orientation ??= O ? probeGeneric(O) : Promise.resolve(false);
        const sensor = await orientation;
        if (sensor) stops.push(genericSensor(O, hz, (s) => { q = Array.from(s.quaternion); }));
        else {
          const onOrientation = (e) => {
            let alpha = e.alpha;
            if (e.webkitCompassHeading != null) alpha = 360 - e.webkitCompassHeading; // Safari: north
            else if (!e.absolute) return;
            if (alpha != null) q = quaternion(alpha, e.beta, e.gamma);
          };
          stops.push(listen("ondeviceorientationabsolute" in globalThis ? "deviceorientationabsolute" : "deviceorientation", onOrientation));
        }
        stops.push(listen("devicemotion", (e) => {
          if (e.accelerationIncludingGravity?.x == null || sensor && !q) return; // the sensor's first reading to come
          const v = motionValues(e);
          add(kind, secs(e.timeStamp), q ? [...v, ...q] : v);
        }));
        stopFn = () => stops.forEach((f) => f());
        break;
      }
      default:
        throw new Error("not available");
    }
    running.set(kind, stopFn);
  };
  const stop = (kind) => {
    running.get(kind)?.();
    running.delete(kind);
  };
  return {
    available: async (b) => new Uint8Array([(await available(b[0])) ? 1 : 0]),
    start: (b) => {
      const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
      return start(b[0], b.length >= 5 ? v.getUint32(1, true) : 0);
    },
    stop: (b) => { if (b.length) stop(b[0]); },
    stopAll: () => {
      for (const k of [...running.keys()]) stop(k);
      buffer = [];
      for (const w of waiting.splice(0)) w.reject(new Error("stopped"));
    },
    next: () => buffer.length ? flush() : new Promise((resolve, reject) => waiting.push({ resolve, reject })),
    steps: () => { throw new Error("not available"); },
  };
}

export function modules() {
  return { network: network(), location: location(), sensors: sensors() };
}

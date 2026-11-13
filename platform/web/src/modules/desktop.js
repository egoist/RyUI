// asuka/sdk/screen's displays, asuka/sdk/power, asuka/sdk/dock and asuka/sdk/appearance in
// the browser.
//
//   - displays: "current", "next" (answered once the list isn't the one
//     given) and "stop", as JSON (screen.Display's). One display from
//     window.screen; every display, with their places and names, from the
//     Window Management API (getScreenDetails: Chrome) once the user has
//     granted it, which the host never asks for. A page isn't told the
//     refresh rate: 0.
//   - power: "current", "next" (the state, then the events since: a
//     browser has none) and "stop": u8 battery, u8 on battery, u8 level
//     (255 not known), u8 charging, u8 low power, u8 thermal, from the
//     Battery Status API (navigator.getBattery: Chrome). No Low Power
//     Mode, heat, sleep or lock screen; "idle" is unknown (the Idle
//     Detection API asks the user).
//   - dock: a page has no icon of its own: progress and attention do
//     nothing ("attention" answers 0, nothing asked).
//   - appearance: "current", "next", "stop": u8 has accent, u8 red,
//     green, blue, u8 increase contrast, u8 reduce transparency: CSS's
//     AccentColor where the browser supports it (Firefox, Safari), and
//     prefers-contrast and prefers-reduced-transparency.

const enc = new TextEncoder();
const dec = new TextDecoder();

const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

/**
 * watcher keeps "next"s waiting until now() (a Uint8Array) isn't what
 * they last got; listen() is called once, as the first waits.
 */
export function watcher(now, listen) {
  const waits = []; // { last, resolve, reject }
  let listening = false;
  const changed = async () => {
    if (!waits.length) return;
    const s = await now();
    for (let i = waits.length - 1; i >= 0; i--) {
      if (!same(waits[i].last, s)) {
        waits[i].resolve(s);
        waits.splice(i, 1);
      }
    }
  };
  return {
    changed,
    current: () => now(),
    next: async (last) => {
      const s = await now();
      if (!last.length || !same(last, s)) return s;
      if (!listening) {
        listening = true;
        listen(changed);
      }
      return new Promise((resolve, reject) => waits.push({ last: last.slice(), resolve, reject }));
    },
    stop: () => {
      for (const w of waits.splice(0)) w.reject(new Error("stopped"));
      return null;
    },
  };
}

// --- displays ---

const rect = (x, y, width, height) => ({ x: x ?? 0, y: y ?? 0, width: width ?? 0, height: height ?? 0 });

/**
 * displaysJSON is screens (window.screen, or getScreenDetails' screens) as
 * asuka/sdk/screen's displays: the primary one first. Without details, the
 * one screen is the primary one.
 */
export function displaysJSON(screens, scale, detailed) {
  const ds = screens.map((s, i) => ({
    id: detailed ? (s.label || String(i)) : "screen",
    name: detailed ? (s.label ?? "") : "",
    bounds: rect(s.left, s.top, s.width, s.height),
    workArea: rect(s.availLeft ?? s.left, s.availTop ?? s.top, s.availWidth ?? s.width, s.availHeight ?? s.height),
    scale: (detailed ? s.devicePixelRatio : scale) || 1,
    refreshRate: 0,
    primary: detailed ? !!s.isPrimary : (s.isPrimary ?? true),
  }));
  // The primary first; the others as the browser lists them.
  ds.sort((a, b) => (b.primary ? 1 : 0) - (a.primary ? 1 : 0));
  return JSON.stringify(ds);
}

/** screenDetails is getScreenDetails() if the user granted it, else null; never asks. */
async function screenDetails() {
  if (typeof getScreenDetails !== "function" || !navigator.permissions?.query) return null;
  try {
    const p = await navigator.permissions.query({ name: "window-management" });
    if (p.state !== "granted") return null;
    return await getScreenDetails();
  } catch {
    return null;
  }
}

function displays() {
  let details; // a promise of ScreenDetails or null, once asked
  const now = async () => {
    details ??= screenDetails();
    const d = await details;
    const json = d ? displaysJSON([...d.screens], 1, true) : displaysJSON([screen], devicePixelRatio, false);
    return enc.encode(json);
  };
  const listen = async (changed) => {
    screen.addEventListener?.("change", changed);
    addEventListener("resize", changed);
    // The scale (a window moved to another display, the page zoomed).
    const watchScale = () => {
      const q = matchMedia(`(resolution: ${devicePixelRatio}dppx)`);
      q.addEventListener?.("change", () => { changed(); watchScale(); }, { once: true });
    };
    if (typeof matchMedia === "function") watchScale();
    const d = await details;
    if (d) {
      const each = () => { for (const s of d.screens) s.addEventListener?.("change", changed); };
      d.addEventListener?.("screenschange", () => { each(); changed(); });
      each();
    }
  };
  const w = watcher(now, listen);
  return {
    current: async () => dec.decode(await w.current()),
    next: (last) => w.next(last),
    stop: w.stop,
  };
}

// --- power ---

/**
 * batteryState is a BatteryManager (or none) as asuka/sdk/power's state: u8
 * battery, u8 on battery, u8 level (255 not known), u8 charging, u8 low
 * power, u8 thermal. Chrome answers a computer without a battery as one
 * charging, full, charged in no time and never discharging: no battery.
 */
export function batteryState(b) {
  if (!b) return new Uint8Array([0, 0, 255, 0, 0, 0]);
  if (b.charging && b.level === 1 && b.chargingTime === 0 && b.dischargingTime === Infinity) {
    return new Uint8Array([0, 0, 255, 0, 0, 0]);
  }
  const level = Math.round(Math.min(Math.max(b.level, 0), 1) * 100);
  return new Uint8Array([1, b.charging ? 0 : 1, level, b.charging && b.level < 1 ? 1 : 0, 0, 0]);
}

function power() {
  let battery; // a promise of the BatteryManager or null, once asked
  const manager = () => (battery ??= typeof navigator.getBattery === "function" ? navigator.getBattery().catch(() => null) : Promise.resolve(null));
  const now = async () => batteryState(await manager());
  const listen = async (changed) => {
    const b = await manager();
    for (const e of ["levelchange", "chargingchange", "chargingtimechange", "dischargingtimechange"]) b?.addEventListener?.(e, changed);
  };
  // A browser tells a page of no sleep or lock screen: answers carry no events.
  const w = watcher(now, listen);
  return { current: w.current, next: w.next, stop: w.stop, idle: () => null };
}

// --- dock ---

const dock = {
  progress: () => null,
  attention: () => new Uint8Array(4), // u32 0: nothing asked
  cancelAttention: () => null,
};

// --- appearance ---

/** parseRGB reads a computed color ("rgb(r, g, b)", "rgba(...)", "color(srgb r g b)") as [r, g, b], or null. */
export function parseRGB(s) {
  let m = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)/.exec(s ?? "");
  if (m) return [m[1], m[2], m[3]].map((v) => Math.round(Math.min(Math.max(+v, 0), 255)));
  m = /^color\(srgb\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)/.exec(s ?? "");
  if (m) return [m[1], m[2], m[3]].map((v) => Math.round(Math.min(Math.max(+v, 0), 1) * 255));
  return null;
}

/** appearanceState is asuka/sdk/appearance's reply: u8 has accent, r, g, b, u8 increase contrast, u8 reduce transparency. */
export function appearanceState(accent, contrast, transparency) {
  const b = new Uint8Array(6);
  if (accent) b.set([1, ...accent]);
  b[4] = contrast ? 1 : 0;
  b[5] = transparency ? 1 : 0;
  return b;
}

/** accentColor is the system's accent, from CSS's AccentColor, or null where the browser has none. */
function accentColor() {
  if (typeof CSS === "undefined" || !CSS.supports?.("color", "AccentColor") || !document.body) return null;
  const probe = document.createElement("span");
  probe.style.cssText = "position:absolute;visibility:hidden;color:AccentColor";
  document.body.appendChild(probe);
  const c = parseRGB(getComputedStyle(probe).color);
  probe.remove();
  return c;
}

const query = (q) => (typeof matchMedia === "function" ? matchMedia(q) : null);

function appearance() {
  const queries = ["(prefers-contrast: more)", "(prefers-reduced-transparency: reduce)", "(prefers-color-scheme: dark)"];
  const now = () => appearanceState(accentColor(), !!query(queries[0])?.matches, !!query(queries[1])?.matches);
  const listen = (changed) => {
    for (const q of queries) query(q)?.addEventListener?.("change", changed);
  };
  const w = watcher(now, listen);
  return { current: w.current, next: w.next, stop: w.stop };
}

export function modules() {
  return { displays: displays(), power: power(), dock, appearance: appearance() };
}

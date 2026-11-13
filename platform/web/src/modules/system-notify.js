// asuka/sdk/notify on the web: the Notification API, with the user's
// permission (notify.Request asks; browsers ask only from a tap or click).
//
//   - Show now, through the app's service worker (sw.js) where it's
//     registered, so a notification keeps its buttons and answers after
//     the page closes; else the page's own (new Notification), whose
//     clicks only an open page gets.
//   - Schedule for later: kept in the app's store (notify-core.js: Cache
//     Storage, shared with its worker and its other pages). A browser has
//     no way to show one at a time while no page is open (Chrome's
//     Notification Triggers were abandoned), so: each page of the app
//     that's open keeps a timer for the next one (the others are told of
//     a change), and the first to find it due shows it, once (a lock);
//     one that came due while no page was open shows when the next opens,
//     or when the worker runs before (a push, or Chrome's periodic
//     background sync, which an installed app may get, now and then).
//   - A notification's ID is its tag: one with the same ID replaces it.
//     Subtitle is the body's first line, Image its picture; Badge sets the
//     installed app's badge (navigator.setAppBadge), as SetBadge does.
//   - Clicking one focuses the page, goes to its URL (ui.OnOpenURL) and
//     reaches OnAction as the default action. A category's actions are
//     its buttons (the worker's notifications only; Chrome shows two), a
//     Reply action a text field where the browser has them (Chrome); the
//     categories are kept for the worker.
//   - Present asks the app how to show one that comes while the page is
//     in front: Banner shows it, Quiet shows it without a sound, Hide
//     doesn't.
//   - Android's channels don't exist here: accepted and ignored, as on iOS.

import "../notify-core.js";
import * as fs from "../fs.js";
import { Events } from "../ops.js";
import { WaitQueue, channel, registration } from "../worker.js";

export { WaitQueue };

const core = globalThis.asukaNotify;
const dec = new TextDecoder();
const enc = new TextEncoder();

const modules = new WeakMap();

/** notifyFor is h's notify module: one a host (asuka.js makes it as the app starts, to show what came due). */
export function notifyFor(h) {
  let m = modules.get(h);
  if (!m) {
    m = notifyModule(h);
    modules.set(h, m);
  }
  return m;
}

const actionSeconds = 25;

// Periodic background sync's least interval: the browser's own is
// longer (Chrome: about 12 hours, as the user engages with the app).
const syncInterval = 15 * 60 * 1000;

export function notifyModule(h, env = {}) {
  const app = h.options.name || "app";
  const ch = channel(h);
  const st = env.store || core.store(app);
  const reg = env.registration || (() => registration());
  const N = () => ("Notification" in env ? env.Notification : globalThis.Notification);
  const own = new Map(); // the page's own notifications: id -> { n, s: Shown }
  const presents = new WaitQueue();
  const pendingPresent = new Map(); // token -> resolve(how)
  let presenting = false;
  let categories = [];
  let categoriesSet = false;
  let timer = 0;

  st.get("categories").then((c) => { if (!categoriesSet && Array.isArray(c)) categories = c; }, () => {});

  const unsupported = (m, why) => new Error(`asuka: notify.${m} isn't on the web here: ${why}`);
  const noNotifications = () => "this browser has no notifications" + (/iPhone|iPad/.test(globalThis.navigator?.userAgent || "") ? " (on iOS, only an app added to the Home Screen has them)" : "");
  const allowed = () => N()?.permission === "granted";

  const imageURL = (src) => {
    if (!src) return undefined;
    if (/^(https?|blob|data):/.test(src)) return src;
    if (src.startsWith("file://")) src = src.slice(7);
    if (src.startsWith("/")) return fs.blobURL(src) || undefined;
    return new URL(h.assets + "/" + src.replace(/^\.?\//, ""), globalThis.location?.href).href;
  };
  const appIcon = () => (h.options.icon ? new URL(h.options.icon, globalThis.location?.href).href : undefined);

  // Present: asked as one comes while the page is in front (a second at most).
  const presentHow = (shown) => {
    if (!presenting || globalThis.document?.visibilityState !== "visible") return Promise.resolve(0);
    const t = core.newID();
    presents.deliver(enc.encode(JSON.stringify({ token: t, notification: shown })));
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        pendingPresent.delete(t);
        resolve(0);
      }, 1000);
      pendingPresent.set(t, (how) => {
        clearTimeout(timer);
        resolve(how);
      });
    });
  };
  ch.present = presentHow;
  ch.closed = (id) => own.delete(id);

  /** show shows s (notifyspec.Spec) now. */
  const show = async (s) => {
    if (!allowed()) return;
    const time = Date.now();
    const how = await presentHow(core.shownOf(s, time));
    if (how === 2) return;
    if (s.badge > 0) globalThis.navigator?.setAppBadge?.(s.badge).catch(() => {});
    const { title, options } = core.notificationOf(s, { categories, time, app, how, image: imageURL, icon: appIcon() });
    const r = await reg();
    if (r) {
      await r.showNotification(title, options);
      return;
    }
    // No worker: the page's own, without actions (only a worker's have them).
    delete options.actions;
    own.get(s.id)?.n?.close?.();
    let n;
    try {
      n = new (N())(title, options);
    } catch (e) {
      // Chrome on Android: only a service worker shows notifications.
      throw new Error("asuka/sdk/notify: this browser shows notifications only through a service worker, and the app's isn't registered (" + (e?.message || e) + ")");
    }
    const entry = { n, s: options.data.shown };
    own.set(s.id, entry);
    n.onclose = () => { if (own.get(s.id) === entry) own.delete(s.id); };
    n.onclick = (e) => {
      e?.preventDefault?.();
      try { globalThis.focus?.(); } catch {}
      n.close();
      ch.actions.deliver(enc.encode(JSON.stringify({ token: core.newID(), action: "", reply: "", seconds: actionSeconds, notification: entry.s })));
      if (s.url) h.dispatch(Events.incomingURL(0, s.url));
    };
  };

  // --- Scheduled ones ---

  const others = typeof BroadcastChannel === "function" && !env.store ? new BroadcastChannel("asuka-notify:" + app) : null;
  const changed = () => others?.postMessage("changed");

  /** fire shows the scheduled ones that are due, then waits for the next. */
  const fire = async () => {
    for (const s of await core.takeDue(st, app, Date.now())) {
      try { await show(s); } catch (e) { console.warn("asuka/sdk/notify:", e); }
    }
    await arm();
  };

  /** arm sets the timer for the next one due, and the worker's periodic sync while there are any. */
  const arm = async () => {
    const all = await core.scheduled(st);
    clearTimeout(timer);
    timer = 0;
    if (all.length) {
      const at = Math.min(...all.map((e) => e.at));
      // setTimeout's longest is about 24.8 days: wait again from there.
      timer = setTimeout(fire, Math.min(Math.max(0, at - Date.now()), 0x7fffffff));
    }
    periodic(all.length > 0);
  };

  let syncing = null;
  const periodic = async (on) => {
    if (syncing === on) return;
    syncing = on;
    try {
      const r = await reg();
      if (!r?.periodicSync) return;
      if (on) await r.periodicSync.register("asuka-notify", { minInterval: syncInterval });
      else await r.periodicSync.unregister("asuka-notify");
    } catch {} // not granted: only an installed app may have it
  };

  // What the last runs scheduled (kept in the page's storage before),
  // then what came due while no page was open.
  const ready = (async () => {
    if (!env.store) {
      try {
        const old = globalThis.localStorage?.getItem(h.prefix() + "notify.scheduled");
        if (old) {
          for (const [id, e] of Object.entries(JSON.parse(old) || {})) if (e?.s?.id === id) await core.schedule(st, app, e.s, e.at);
          globalThis.localStorage.removeItem(h.prefix() + "notify.scheduled");
        }
      } catch {}
    }
    await fire();
  })();
  if (others) others.onmessage = () => arm();
  globalThis.document?.addEventListener?.("visibilitychange", () => {
    // Timers wait longer in a hidden page: back in front, what's due shows.
    if (globalThis.document.visibilityState === "visible") fire();
  });

  // --- The notifications showing ---

  const workerShowing = async () => {
    const r = await reg();
    if (!r?.getNotifications) return [];
    try {
      return (await r.getNotifications()).filter((n) => n.data?.asuka && (n.data.app || app) === app);
    } catch {
      return [];
    }
  };

  const remove = async (pred) => {
    for (const [id, e] of own) if (pred(e.s)) { e.n?.close(); own.delete(id); }
    for (const n of await workerShowing()) if (pred(n.data.shown || {})) n.close();
  };

  return {
    ready,
    fire,
    request: async () => {
      if (!N()) throw unsupported("Request", noNotifications());
      const p = N().permission === "default" ? await N().requestPermission() : N().permission;
      return new Uint8Array([p === "granted" ? 1 : 0]);
    },
    schedule: async (b) => {
      if (!N()) throw unsupported("Show", noNotifications());
      if (!allowed()) throw new Error("asuka/sdk/notify: notifications aren't allowed: ask with notify.Request first");
      const s = JSON.parse(dec.decode(b));
      if (!s.id) s.id = core.newID();
      if (s.seconds > 0) {
        await ready;
        await core.schedule(st, app, s, Date.now() + Math.max(1, s.seconds) * 1000);
        changed();
        await arm();
      } else {
        // Shown now, in place of one waiting with its ID.
        await core.unschedule(st, app, [s.id]);
        await show(s);
      }
    },
    cancel: async (b) => {
      const id = dec.decode(b);
      await core.unschedule(st, app, [id]);
      changed();
      await arm();
      await remove((s) => s.id === id);
    },
    badge: async (b) => {
      const n = b.length >= 4 ? new DataView(b.buffer, b.byteOffset, 4).getUint32(0, true) : 0;
      const nav = globalThis.navigator;
      if (!nav?.setAppBadge) return; // only an installed app has a badge
      try {
        if (n > 0) await nav.setAppBadge(n);
        else await nav.clearAppBadge();
      } catch {}
    },
    showing: async () => {
      const out = [...own.values()].map((e) => e.s);
      const ids = new Set(out.map((s) => s.id));
      for (const n of await workerShowing()) if (!ids.has(n.data.shown?.id)) out.push(n.data.shown);
      return JSON.stringify(out.sort((a, b) => b.time - a.time));
    },
    remove: (b) => {
      const ids = new Set(JSON.parse(dec.decode(b) || "[]") || []);
      return remove((s) => ids.has(s.id));
    },
    removeThread: (b) => {
      const thread = dec.decode(b);
      return remove((s) => s.thread === thread);
    },
    removeAll: () => remove(() => true),
    categories: async (b) => {
      categories = JSON.parse(dec.decode(b) || "[]") || [];
      categoriesSet = true;
      await st.set("categories", categories);
    },
    channels: () => {},
    channelGroups: () => {},
    present: (b) => {
      presenting = b[0] === 1;
      if (!presenting) {
        for (const [t, resolve] of pendingPresent) { pendingPresent.delete(t); resolve(0); }
      }
    },
    nextPresent: () => presents.next(),
    presented: (b) => {
      if (!b.length) return;
      const t = dec.decode(b.subarray(1));
      const resolve = pendingPresent.get(t);
      if (resolve) { pendingPresent.delete(t); resolve(b[0]); }
    },
    nextAction: () => ch.actions.next(),
    // Nothing waits for a background action's work: the page runs on.
    actionDone: () => {},
    stop: (b) => {
      const m = dec.decode(b);
      if (m === "nextPresent") presents.stop();
      else if (m === "nextAction") ch.actions.stop();
    },
  };
}

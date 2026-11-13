// asuka/push on the web: Web Push, with the app's VAPID key (asuka.json's
// web.vapidPublicKey, which the page has as options.vapidPublicKey).
//
//   - register asks to allow notifications, subscribes the app's service
//     worker (PushManager, userVisibleOnly: a browser subscribes only a
//     site that shows its pushes) and answers push.Registration's JSON:
//     the token is the subscription's JSON (PushSubscription.toJSON():
//     endpoint, expirationTime, keys.p256dh, keys.auth), which the app's
//     server sends to (asuka/push/webpush), service "webpush". A
//     subscription made with another key is replaced.
//   - The worker (sw.js) shows each push; the page gets it
//     (nextReceived), and its tap (nextOpened), through worker.js.
//   - push.Rewrite: the page in front rewrites a push before the worker
//     shows it (nextRewrite, rewritten), within a few seconds; with no
//     page open, a push shows as it came.

import "../notify-core.js";
import { WaitQueue, channel, registration } from "../worker.js";

const core = globalThis.asukaNotify;
const dec = new TextDecoder();
const enc = new TextEncoder();

// How long a rewrite may take: the worker waits 10 seconds for the page.
const rewriteMs = 9000;

export function pushModule(h, env = {}) {
  const ch = channel(h);
  const rewrites = new WaitQueue();
  const pending = new Map(); // token -> resolve(spec or null)
  let serving = false;
  let tokens = 0;

  ch.rewrite = (message) => {
    if (!serving) return Promise.resolve(null);
    const token = "rw" + ++tokens;
    rewrites.deliver(enc.encode(JSON.stringify({ token, message })));
    return new Promise((resolve) => {
      const t = setTimeout(() => {
        pending.delete(token);
        resolve(null);
      }, rewriteMs);
      pending.set(token, (spec) => {
        clearTimeout(t);
        resolve(spec);
      });
    });
  };

  return {
    register: () => subscribe({
      key: h.options.vapidPublicKey,
      store: env.store || core.store(h.options.name || "app"),
      registration: env.registration || (() => registration()),
      Notification: "Notification" in env ? env.Notification : globalThis.Notification,
      PushManager: "PushManager" in env ? env.PushManager : globalThis.PushManager,
    }),
    nextReceived: () => ch.received.next(),
    nextOpened: () => ch.opened.next(),
    stop: (b) => {
      const m = dec.decode(b);
      if (m === "nextReceived") ch.received.stop();
      else if (m === "nextOpened") ch.opened.stop();
      else if (m === "nextRewrite") rewrites.stop();
    },
    nextRewrite: () => {
      serving = true;
      return rewrites.next();
    },
    rewritten: (b) => {
      let r;
      try { r = JSON.parse(dec.decode(b)); } catch { return; }
      const resolve = pending.get(r?.token);
      if (resolve) {
        pending.delete(r.token);
        resolve(r.ok ? r.spec : null);
      }
    },
  };
}

const ios = () => /iPhone|iPad/.test(globalThis.navigator?.userAgent || "");

/** subscribe is push.Register's answer: the subscription, made with key (base64url, an uncompressed P-256 point). */
export async function subscribe({ key, registration, Notification, PushManager, store }) {
  if (!key) throw new Error("asuka/push: Web Push needs the app's VAPID public key: set web.vapidPublicKey in asuka.json (see docs/web.md, Web Push)");
  if (!PushManager || !Notification) {
    throw new Error("asuka/push: this browser has no Web Push" + (ios() ? " (on iOS, only an app added to the Home Screen has it)" : ""));
  }
  const r = await registration();
  if (!r?.pushManager) throw new Error("asuka/push: the app's service worker isn't registered (a page served over https, or from localhost, has one)");
  const p = Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
  if (p !== "granted") throw new Error("asuka/push: notifications aren't allowed, and a browser subscribes only a site that shows its pushes");
  const want = base64url(key);
  if (want.length !== 65 || want[0] !== 4) throw new Error("asuka/push: web.vapidPublicKey isn't a P-256 public key (65 bytes, base64url)");
  let sub = await r.pushManager.getSubscription();
  if (sub && !same(sub.options?.applicationServerKey, want)) {
    await sub.unsubscribe().catch(() => {});
    sub = null;
  }
  sub ??= await r.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: want });
  // The worker subscribes with it again if the browser ends the subscription.
  await store?.set("vapid", key);
  return JSON.stringify({ token: JSON.stringify(sub.toJSON()), service: "webpush", allowed: true });
}

/** base64url decodes s (padded or not, base64url or base64). */
export function base64url(s) {
  let t = String(s).trim().replace(/-/g, "+").replace(/_/g, "/").replace(/=+$/, "");
  t += "=".repeat((4 - (t.length % 4)) % 4);
  return Uint8Array.from(atob(t), (c) => c.charCodeAt(0));
}

function same(a, b) {
  if (!a) return true; // the browser doesn't say: keep it
  const x = a instanceof ArrayBuffer ? new Uint8Array(a) : ArrayBuffer.isView(a) ? new Uint8Array(a.buffer, a.byteOffset, a.byteLength) : null;
  return !!x && x.length === b.length && x.every((v, i) => v === b[i]);
}

// The page's side of the app's service worker (sw.js): asuka.js registers
// it as the page starts, and this listens for what it sends (the user's
// answers to notifications, pushes) from then on, before the app's Go
// code asks for them: each host (app) on the page has its channel, whose
// queues keep them until asuka/notify and asuka/push ask ("next...").
//
//   - "response": notify.OnAction's Response (actions), a push's to
//     push.OnOpen too (opened), a tap's URL to ui.OnOpenURL.
//   - "received": a push, for push.OnReceive.
//   - "push": a push for the page in front, which answers on the port it
//     came with: its rewrite (push.Rewrite) and how it shows
//     (notify.Present).
//   - "closed": a notification the user dismissed.

import "./notify-core.js";
import { Events } from "./ops.js";

const core = globalThis.asukaNotify;
const enc = new TextEncoder();

/** WaitQueue is what Go waits for ("next..." methods): items delivered to the first waiting, else kept. */
export class WaitQueue {
  constructor() { this.items = []; this.waiting = []; }
  next() {
    if (this.items.length) return Promise.resolve(this.items.shift());
    return new Promise((resolve, reject) => this.waiting.push({ resolve, reject }));
  }
  deliver(b) {
    const w = this.waiting.shift();
    if (w) w.resolve(b);
    else this.items.push(b);
  }
  stop() {
    const ws = this.waiting;
    this.waiting = [];
    for (const w of ws) w.reject(new Error("stopped"));
  }
}

const channels = new WeakMap();
const hosts = []; // attached, in order
const early = []; // [message, port] that came before their app's host
let registering = null;
const sent = new Set(); // the apps that said hello

/**
 * channel is a host's: its queues, and the hooks its modules set
 * (present: Present's answer for a Shown; rewrite: push.Rewrite's spec
 * for a push.Message, or null; closed: a dismissed one's ID).
 */
export function channel(h) {
  let c = channels.get(h);
  if (!c) {
    c = { actions: new WaitQueue(), received: new WaitQueue(), opened: new WaitQueue(), present: null, rewrite: null, closed: null };
    channels.set(h, c);
  }
  return c;
}

const appOf = (h) => h.options?.name || "app";

/**
 * register registers the app's service worker (url, scoped to its
 * directory) and listens to it. Once a page: the first app's. Resolves
 * to the registration, or null (no worker: no URL, an insecure page, a
 * browser without them).
 */
export function register(url, nav = globalThis.navigator) {
  if (registering) return registering;
  const sw = nav?.serviceWorker;
  if (!url || !sw || globalThis.isSecureContext === false) {
    registering = Promise.resolve(null);
    return registering;
  }
  sw.addEventListener("message", (e) => receive(e.data, e.ports?.[0]));
  sw.startMessages?.();
  registering = sw.register(url)
    .then(() => sw.ready)
    .then((r) => {
      for (const h of hosts) hello(r, appOf(h));
      return r;
    })
    .catch((e) => {
      console.warn("asuka: the app's service worker didn't register:", e);
      return null;
    });
  return registering;
}

/** registration is the app's service worker's registration, or null; an embedding page's own, if it registered one. */
export async function registration(nav = globalThis.navigator) {
  if (registering) return registering;
  try {
    return (await nav?.serviceWorker?.getRegistration?.()) || null;
  } catch {
    return null;
  }
}

function hello(r, app) {
  if (sent.has(app)) return;
  sent.add(app);
  (r.active || r.waiting || r.installing)?.postMessage({ asuka: "hello", app });
}

/** attach gives h (an app on the page) what its worker sends. */
export function attach(h) {
  hosts.push(h);
  channel(h);
  const waiting = early.splice(0);
  for (const [m, port] of waiting) receive(m, port);
  registering?.then((r) => r && hello(r, appOf(h)));
}

/** receive handles a message from the worker. Exported for the tests. */
export function receive(m, port) {
  if (!m?.asuka) return;
  const h = hosts.find((x) => appOf(x) === m.app) || (hosts.length === 1 ? hosts[0] : null);
  if (!h) {
    if (m.asuka === "push") port?.postMessage({ spec: null, how: 0 });
    if (m.asuka !== "closed") early.push([m.asuka === "push" ? { ...m, asuka: "received" } : m, null]);
    return;
  }
  const c = channel(h);
  const json = (v) => enc.encode(JSON.stringify(v));
  switch (m.asuka) {
    case "response":
      c.actions.deliver(json(m.response));
      if (m.push) c.opened.deliver(json(m.push));
      if (m.url) whenConnected(h, () => h.dispatch(Events.incomingURL(0, m.url)));
      break;
    case "received":
      c.received.deliver(json(m.message));
      break;
    case "push":
      c.received.deliver(json(m.message));
      answerPush(c, m.message).then((a) => port?.postMessage(a), () => port?.postMessage({ spec: null, how: 0 }));
      break;
    case "closed":
      c.closed?.(m.id);
      break;
  }
}

/** answerPush is the page's answer to a push: its rewrite (or null) and how it shows. */
export async function answerPush(c, message) {
  let spec = null;
  if (c.rewrite) {
    try { spec = await c.rewrite(message); } catch { spec = null; }
  }
  const s = spec || core.specOfMessage(message);
  let how = 0;
  if (c.present && (s.title || s.body || s.sender)) how = await c.present(core.shownOf(s, Date.now(), true));
  return { spec, how };
}

/** whenConnected runs f once h's Go code is connected (it takes events then). */
function whenConnected(h, f) {
  if (h.core) {
    f();
    return;
  }
  (h.onConnect ??= []).push(f);
}

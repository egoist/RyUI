// The app's service worker (src/sw.js, with src/notify-core.js), run in a
// context of its own with fakes for what a worker has (registration,
// clients, Cache Storage), and the page's side of it (src/worker.js) and
// the notify and push modules that use it, with fakes for the browser.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import { attach, channel, receive, answerPush, openLaunchFiles } from "../src/worker.js";
import { notifyModule } from "../src/modules/system-notify.js";
import { pushModule, subscribe, base64url } from "../src/modules/system-push.js";

const src = (n) => readFileSync(new URL("../src/" + n, import.meta.url), "utf8");
const enc = new TextEncoder();
const dec = new TextDecoder();
const json = (b) => JSON.parse(dec.decode(b));
// The worker's values are its context's (another realm's objects): compared as JSON.
const same = (a, b, ...msg) => assert.deepEqual(JSON.parse(JSON.stringify(a)), JSON.parse(JSON.stringify(b)), ...msg);

/** fakeCaches is Cache Storage in memory. */
function fakeCaches() {
  const m = new Map();
  const cache = {
    match: async (u) => (m.has(u) ? new Response(m.get(u)) : undefined),
    put: async (u, r) => { m.set(u, await r.text()); },
    delete: async (u) => m.delete(u),
  };
  return { open: async () => cache, entries: m };
}

/** fakeClient is a page of the app, as the worker sees it; answer(m) answers a message sent with a port. */
function fakeClient({ focused = false, visible = false, answer = null } = {}) {
  return {
    focused, visibilityState: visible ? "visible" : "hidden", posted: [], focusedBy: 0,
    postMessage(m, ports) {
      this.posted.push(m);
      if (ports?.length && answer) Promise.resolve(answer(m)).then((a) => ports[0].postMessage(a));
    },
    async focus() { this.focusedBy++; this.focused = true; return this; },
  };
}

/** worker runs sw.js in a context of its own: its listeners, what it shows, its pages. */
function worker(app = { name: "com.example.app", title: "Example" }) {
  const shown = [];
  const opened = [];
  const listeners = {};
  const caches = fakeCaches();
  const ctx = {
    console, URL, Response, MessageChannel, setTimeout, clearTimeout, Promise,
    caches,
    navigator: {},
    asukaApp: app,
    registration: {
      scope: "https://app.example/chat/",
      async showNotification(title, options) {
        const n = { title, ...options, closed: false, close() { this.closed = true; } };
        shown.push(n);
      },
      async getNotifications() { return shown.filter((n) => !n.closed); },
    },
    clients: {
      list: [],
      async matchAll() { return this.list; },
      async openWindow(u) { opened.push(u); return null; },
      async claim() {},
    },
    skipWaiting() {},
    addEventListener(type, f) { (listeners[type] ??= []).push(f); },
  };
  vm.createContext(ctx);
  vm.runInContext(src("notify-core.js"), ctx);
  vm.runInContext(src("sw.js"), ctx);
  const dispatch = async (type, e) => {
    const waits = [];
    for (const f of listeners[type] || []) f({ ...e, waitUntil: (p) => waits.push(p) });
    await Promise.all(waits);
  };
  const store = ctx.asukaNotify.store(app.name);
  const pushData = (v) => ({ text: () => (typeof v === "string" ? v : JSON.stringify(v)) });
  return { ctx, shown, opened, listeners, dispatch, store, pushData, clients: ctx.clients, core: ctx.asukaNotify };
}

test("the worker listens for pushes, clicks, closes, its pages' messages and periodic sync", () => {
  const w = worker();
  for (const t of ["install", "activate", "push", "notificationclick", "notificationclose", "message", "periodicsync", "pushsubscriptionchange"]) {
    assert.equal(w.listeners[t]?.length, 1, t);
  }
});

test("a push with no page open shows, with its category's actions", async () => {
  const w = worker();
  await w.store.set("categories", [{ id: "chat", actions: [
    { id: "like", title: "Like" },
    { id: "reply", title: "Reply", reply: { button: "Send", placeholder: "Message" } },
    { id: "open", title: "Open", open: true },
  ] }]);
  await w.dispatch("push", { data: w.pushData({
    id: "m1", title: "Ada", subtitle: "Climbing", body: "Lunch?", category: "chat", thread: "t1", badge: 3,
    data: { url: "myapp://chat/7", n: 7, ok: true, o: { a: 1 } }, image: "https://img.example/a.png",
  }) });
  assert.equal(w.shown.length, 1);
  const n = w.shown[0];
  assert.equal(n.title, "Ada");
  assert.equal(n.body, "Climbing\nLunch?");
  assert.equal(n.tag, "m1");
  assert.equal(n.renotify, true);
  assert.equal(n.silent, false);
  assert.equal(n.image, "https://img.example/a.png");
  same(n.actions, [
    { action: "like", title: "Like" },
    { action: "reply", title: "Reply", type: "text", placeholder: "Message" },
    { action: "open", title: "Open" },
  ]);
  same(n.data.opens, ["open"]);
  assert.equal(n.data.app, "com.example.app");
  // The click's: push.Message's JSON, and the Shown.
  same(n.data.push, {
    id: "m1", title: "Ada", body: "Lunch?", subtitle: "Climbing", data: { url: "myapp://chat/7", n: "7", ok: "true", o: '{"a":1}' },
    category: "chat", thread: "t1", channel: "", image: "https://img.example/a.png", badge: 3, summary: "",
  });
  assert.equal(n.data.shown.url, "myapp://chat/7");
  assert.equal(n.data.shown.remote, true);
  assert.equal(n.data.shown.thread, "t1");
});

test("a push asks the page in front (rewrite, Present), the others just get it", async () => {
  const w = worker();
  const front = fakeClient({ focused: true, visible: true, answer: () => ({ spec: { id: "m2", title: "Grace", body: "decrypted", data: { url: "myapp://x" } }, how: 1 }) });
  const back = fakeClient();
  w.clients.list = [back, front];
  await w.dispatch("push", { data: w.pushData({ id: "m2", title: "New message", data: { box: "..." } }) });
  assert.equal(front.posted[0].asuka, "push");
  assert.equal(front.posted[0].message.title, "New message");
  same(back.posted, [{ asuka: "received", app: "com.example.app", message: front.posted[0].message }]);
  assert.equal(w.shown.length, 1);
  assert.equal(w.shown[0].title, "Grace");
  assert.equal(w.shown[0].body, "decrypted");
  assert.equal(w.shown[0].silent, true); // Quiet
  assert.equal(w.shown[0].renotify, false);
  assert.equal(w.shown[0].data.shown.url, "myapp://x");
  assert.equal(w.shown[0].data.push.title, "Grace");
});

test("a push the page in front hides doesn't show; a data message never does", async () => {
  const w = worker();
  const front = fakeClient({ visible: true, answer: () => ({ spec: null, how: 2 }) });
  w.clients.list = [front];
  await w.dispatch("push", { data: w.pushData({ title: "Hidden" }) });
  assert.equal(w.shown.length, 0);
  await w.dispatch("push", { data: w.pushData({ data: { sync: "1" } }) });
  assert.equal(w.shown.length, 0);
  assert.equal(front.posted.length, 2);
  same(front.posted[1].message.data, { sync: "1" });
  // Not JSON: the body.
  const w2 = worker();
  await w2.dispatch("push", { data: w2.pushData("plain text") });
  assert.equal(w2.shown[0].body, "plain text");
});

test("a tap goes to the page in front, focused: OnAction's Response, the push's OnOpen, its URL", async () => {
  const w = worker();
  await w.dispatch("push", { data: w.pushData({ id: "m3", title: "Ada", body: "Hi", data: { url: "myapp://chat/3" } }) });
  const page = fakeClient({ visible: true });
  w.clients.list = [page];
  const n = w.shown[0];
  await w.dispatch("notificationclick", { notification: n, action: "" });
  assert.equal(n.closed, true);
  assert.equal(page.focusedBy, 1);
  const m = page.posted[0];
  assert.equal(m.asuka, "response");
  assert.equal(m.app, "com.example.app");
  assert.equal(m.url, "myapp://chat/3");
  assert.equal(m.response.action, "");
  assert.equal(m.response.seconds, 25);
  assert.ok(m.response.token);
  assert.equal(m.response.notification.id, "m3");
  assert.equal(m.push.title, "Ada");
});

test("an action with no page open waits for the page it opens, which gets it as it says hello", async () => {
  const w = worker();
  await w.store.set("categories", [{ id: "chat", actions: [{ id: "reply", title: "Reply", reply: {} }] }]);
  await w.dispatch("push", { data: w.pushData({ id: "m4", title: "Ada", body: "Hi", category: "chat", data: { url: "myapp://chat/4" } }) });
  await w.dispatch("notificationclick", { notification: w.shown[0], action: "reply", reply: "On my way" });
  same(w.opened, ["https://app.example/chat/"]);
  // The page starts.
  const page = fakeClient();
  await w.dispatch("message", { data: { asuka: "hello", app: "com.example.app" }, source: page });
  assert.equal(page.posted.length, 1);
  const m = page.posted[0];
  assert.equal(m.response.action, "reply");
  assert.equal(m.response.reply, "On my way");
  assert.equal(m.url, ""); // an action's: no URL
  assert.equal(m.push.id, "m4");
  // Once.
  const again = fakeClient();
  await w.dispatch("message", { data: { asuka: "hello", app: "com.example.app" }, source: again });
  assert.equal(again.posted.length, 0);
});

test("a background action goes to an open page without focusing it; a dismissal is told", async () => {
  const w = worker();
  await w.store.set("categories", [{ id: "c", actions: [{ id: "done", title: "Done" }, { id: "go", title: "Go", open: true }] }]);
  await w.dispatch("push", { data: w.pushData({ id: "a", title: "T", category: "c" }) });
  await w.dispatch("push", { data: w.pushData({ id: "b", title: "T", category: "c" }) });
  const page = fakeClient();
  w.clients.list = [page];
  await w.dispatch("notificationclick", { notification: w.shown[0], action: "done" });
  assert.equal(page.focusedBy, 0);
  assert.equal(page.posted.at(-1).response.action, "done");
  await w.dispatch("notificationclick", { notification: w.shown[1], action: "go" });
  assert.equal(page.focusedBy, 1);
  await w.dispatch("notificationclose", { notification: { data: { asuka: 1, app: "com.example.app", shown: { id: "zz" } } } });
  same(page.posted.at(-1), { asuka: "closed", app: "com.example.app", id: "zz" });
  // Not the app's: left alone.
  const other = { data: { foo: 1 }, closed: false, close() { this.closed = true; } };
  await w.dispatch("notificationclick", { notification: other, action: "" });
  assert.equal(other.closed, false);
});

test("periodic sync and activation show the scheduled notifications that are due, once, with no page open", async () => {
  const w = worker();
  const now = Date.now();
  await w.core.schedule(w.store, "com.example.app", { id: "due", title: "Due" }, now - 1000);
  await w.core.schedule(w.store, "com.example.app", { id: "later", title: "Later" }, now + 3600_000);
  // A page is open: it shows its own.
  w.clients.list = [fakeClient()];
  await w.dispatch("periodicsync", { tag: "asuka-notify" });
  assert.equal(w.shown.length, 0);
  w.clients.list = [];
  await w.dispatch("periodicsync", { tag: "something-else" });
  assert.equal(w.shown.length, 0);
  await w.dispatch("periodicsync", { tag: "asuka-notify" });
  same(w.shown.map((n) => n.title), ["Due"]);
  assert.equal(w.shown[0].data.shown.remote, false);
  await w.dispatch("activate", {});
  assert.equal(w.shown.length, 1);
  same((await w.core.scheduled(w.store)).map((e) => e.s.id), ["later"]);
});

test("an ended subscription is made again with the app's key", async () => {
  const w = worker();
  const made = [];
  w.ctx.registration.pushManager = { sub: null, async getSubscription() { return this.sub; }, async subscribe(o) { made.push(o); this.sub = {}; return this.sub; } };
  await w.store.set("vapid", "BKey");
  await w.dispatch("pushsubscriptionchange", { oldSubscription: null });
  same(made, [{ userVisibleOnly: true, applicationServerKey: "BKey" }]);
  // One already made again (by a page): left.
  await w.dispatch("pushsubscriptionchange", { oldSubscription: { options: { applicationServerKey: "Old" } } });
  assert.equal(made.length, 1);
});

// --- The page's side ---

function fakeHost(name, extra = {}) {
  const dispatched = [];
  return {
    options: { name, ...extra }, assets: "https://app.example/assets", core: {}, dispatched,
    dispatch(b) { dispatched.push(b); },
    prefix() { return "asuka." + name + "."; },
  };
}

test("the page keeps what the worker sends before its app attaches, then gives it to the app's queues", async () => {
  receive({ asuka: "received", app: "early.app", message: { id: "e1", title: "Early" } });
  let answered = null;
  receive({ asuka: "push", app: "early.app", message: { id: "e2", title: "Early 2" } }, { postMessage: (a) => { answered = a; } });
  assert.deepEqual(answered, { spec: null, how: 0 });
  const h = fakeHost("early.app");
  attach(h);
  const c = channel(h);
  assert.equal(json(await c.received.next()).id, "e1");
  assert.equal(json(await c.received.next()).id, "e2");
});

test("a response reaches OnAction, a push's OnOpen, its URL OnOpenURL", async () => {
  const h = fakeHost("resp.app");
  attach(h);
  const c = channel(h);
  receive({ asuka: "response", app: "resp.app", response: { token: "t", action: "", reply: "", seconds: 25, notification: { id: "n" } }, push: { id: "p" }, url: "myapp://x" });
  assert.equal(json(await c.actions.next()).notification.id, "n");
  assert.equal(json(await c.opened.next()).id, "p");
  assert.equal(h.dispatched.length, 1);
  assert.ok(dec.decode(h.dispatched[0]).includes("myapp://x"));
  // Before Go is connected: once it is.
  const late = fakeHost("late.app");
  late.core = null;
  attach(late);
  receive({ asuka: "response", app: "late.app", response: { notification: {} }, url: "myapp://y" });
  assert.equal(late.dispatched.length, 0);
  late.core = {};
  for (const f of late.onConnect) f();
  assert.equal(late.dispatched.length, 1);
});

test("the page's answer to a push: its rewrite, then Present's", async () => {
  const c = { rewrite: null, present: null };
  assert.deepEqual(await answerPush(c, { id: "1", title: "T", body: "B", data: {} }), { spec: null, how: 0 });
  c.rewrite = async (m) => ({ id: m.id, title: "Rewritten", body: "x" });
  let asked = null;
  c.present = async (shown) => { asked = shown; return 2; };
  const a = await answerPush(c, { id: "1", title: "T", body: "B", data: {} });
  assert.deepEqual(a, { spec: { id: "1", title: "Rewritten", body: "x" }, how: 2 });
  assert.equal(asked.title, "Rewritten");
  assert.equal(asked.remote, true);
});

// --- asuka/sdk/notify's module ---

function notifyEnv() {
  const caches = fakeCaches();
  const core = globalThis.asukaNotify;
  const shown = [];
  const registration = {
    async showNotification(title, options) { shown.push({ title, ...options, closed: false, close() { this.closed = true; } }); },
    async getNotifications() { return shown.filter((n) => !n.closed); },
  };
  return { shown, core, store: core.store("n.app", { caches }), registration: async () => registration, Notification: { permission: "granted" } };
}

test("notify: shown through the worker with its category's actions; Showing, Remove, Cancel", async () => {
  const env = notifyEnv();
  const m = notifyModule(fakeHost("n.app"), env);
  await m.ready;
  await m.categories(enc.encode(JSON.stringify([{ id: "chat", actions: [{ id: "like", title: "Like" }] }])));
  assert.deepEqual(await env.store.get("categories"), [{ id: "chat", actions: [{ id: "like", title: "Like" }] }]);
  await m.schedule(enc.encode(JSON.stringify({ id: "a", title: "Ada", body: "Hi", category: "chat", thread: "t", url: "myapp://a" })));
  await m.schedule(enc.encode(JSON.stringify({ id: "b", title: "Bob", body: "Yo", image: "pic.png" })));
  assert.deepEqual(env.shown.map((n) => n.title), ["Ada", "Bob"]);
  assert.deepEqual(env.shown[0].actions, [{ action: "like", title: "Like" }]);
  assert.equal(env.shown[1].image, "https://app.example/assets/pic.png");
  const showing = JSON.parse(await m.showing());
  assert.deepEqual(showing.map((s) => s.id).sort(), ["a", "b"]);
  await m.removeThread(enc.encode("t"));
  assert.deepEqual(JSON.parse(await m.showing()).map((s) => s.id), ["b"]);
  await m.cancel(enc.encode("b"));
  assert.deepEqual(JSON.parse(await m.showing()), []);
});

test("notify: what came due while no page was open shows as the module starts; the rest waits", async () => {
  const env = notifyEnv();
  const now = Date.now();
  await env.core.schedule(env.store, "n.app", { id: "missed", title: "Missed" }, now - 60_000);
  await env.core.schedule(env.store, "n.app", { id: "later", title: "Later" }, now + 3600_000);
  const m = notifyModule(fakeHost("n.app"), env);
  await m.ready;
  assert.deepEqual(env.shown.map((n) => n.title), ["Missed"]);
  assert.deepEqual((await env.core.scheduled(env.store)).map((e) => e.s.id), ["later"]);
  // Scheduled: kept for the worker and the other pages; Cancel forgets it (and the timer).
  await m.schedule(enc.encode(JSON.stringify({ id: "soon", title: "Soon", seconds: 30 })));
  const all = await env.core.scheduled(env.store);
  assert.deepEqual(all.map((e) => e.s.id).sort(), ["later", "soon"]);
  assert.ok(all.find((e) => e.s.id === "soon").at >= now + 30_000);
  // Due (as if the time came): fire shows it, once.
  await env.core.schedule(env.store, "n.app", { id: "soon", title: "Soon" }, Date.now() - 1);
  await m.fire();
  await m.fire();
  assert.deepEqual(env.shown.map((n) => n.title), ["Missed", "Soon"]);
  await m.cancel(enc.encode("later"));
  assert.deepEqual(await env.core.scheduled(env.store), []);
});

test("notify: Present hides one that comes while the page is in front", async () => {
  const env = notifyEnv();
  const h = fakeHost("n.app");
  const m = notifyModule(h, env);
  await m.ready;
  const doc = globalThis.document;
  globalThis.document = { visibilityState: "visible" };
  try {
    m.present(new Uint8Array([1]));
    const asked = m.nextPresent();
    const shown = m.schedule(enc.encode(JSON.stringify({ id: "x", title: "X" })));
    const p = json(await asked);
    assert.equal(p.notification.id, "x");
    m.presented(new Uint8Array([2, ...enc.encode(p.token)]));
    await shown;
    assert.equal(env.shown.length, 0);
    // A push the worker asks about goes through the same.
    const how = channel(h).present({ id: "y", title: "Y" });
    const q = json(await m.nextPresent());
    m.presented(new Uint8Array([1, ...enc.encode(q.token)]));
    assert.equal(await how, 1);
  } finally {
    globalThis.document = doc;
  }
});

// --- asuka/sdk/push's module ---

const vapid = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8";

function fakePushManager(existing = null) {
  const pm = {
    sub: existing, subscribed: 0,
    async getSubscription() { return this.sub; },
    async subscribe(o) {
      this.subscribed++;
      assert.equal(o.userVisibleOnly, true);
      const key = o.applicationServerKey;
      this.sub = {
        options: { applicationServerKey: key.buffer.slice(key.byteOffset, key.byteOffset + key.byteLength) },
        toJSON: () => ({ endpoint: "https://push.example/sub/1", expirationTime: null, keys: { p256dh: "pk", auth: "au" } }),
        unsubscribe: async () => { pm.sub = null; return true; },
      };
      return this.sub;
    },
  };
  return pm;
}

test("push.Register subscribes with the app's VAPID key and answers the subscription as the token", async () => {
  const pm = fakePushManager();
  const env = { key: vapid, registration: async () => ({ pushManager: pm }), Notification: { permission: "granted" }, PushManager: function () {} };
  const r = JSON.parse(await subscribe(env));
  assert.equal(r.service, "webpush");
  assert.equal(r.allowed, true);
  assert.deepEqual(JSON.parse(r.token), { endpoint: "https://push.example/sub/1", expirationTime: null, keys: { p256dh: "pk", auth: "au" } });
  // Again: the same subscription.
  await subscribe(env);
  assert.equal(pm.subscribed, 1);
  // Another key: a new one.
  const other = base64url(vapid);
  other[5] ^= 1;
  pm.sub.options.applicationServerKey = other.buffer;
  await subscribe(env);
  assert.equal(pm.subscribed, 2);
  // What fails, and says why.
  await assert.rejects(subscribe({ ...env, key: "" }), /web\.vapidPublicKey/);
  await assert.rejects(subscribe({ ...env, key: "AAAA" }), /P-256/);
  await assert.rejects(subscribe({ ...env, Notification: { permission: "denied" } }), /aren't allowed/);
  await assert.rejects(subscribe({ ...env, registration: async () => null }), /service worker/);
  await assert.rejects(subscribe({ ...env, PushManager: undefined }), /no Web Push/);
  let asked = 0;
  await subscribe({ ...env, Notification: { permission: "default", requestPermission: async () => { asked++; return "granted"; } } });
  assert.equal(asked, 1);
});

test("push: OnReceive and OnOpen's queues, and a rewrite through the page", async () => {
  const h = fakeHost("p.app", { vapidPublicKey: vapid });
  attach(h);
  const m = pushModule(h, { registration: async () => ({ pushManager: fakePushManager() }), Notification: { permission: "granted" }, PushManager: function () {} });
  assert.equal(JSON.parse(await m.register()).service, "webpush");
  const c = channel(h);
  // Not serving rewrites: none.
  assert.equal(await c.rewrite({ id: "1" }), null);
  receive({ asuka: "received", app: "p.app", message: { id: "r1" } });
  assert.equal(json(await m.nextReceived()).id, "r1");
  const opened = m.nextOpened();
  m.stop(enc.encode("nextOpened"));
  await assert.rejects(opened, /stopped/);
  // Serving: the page's Go code rewrites it.
  const next = m.nextRewrite();
  const spec = c.rewrite({ id: "2", title: "box" });
  const req = json(await next);
  assert.equal(req.message.title, "box");
  m.rewritten(enc.encode(JSON.stringify({ token: req.token, ok: true, spec: { id: "2", title: "Opened" } })));
  assert.deepEqual(await spec, { id: "2", title: "Opened" });
  // A failed rewrite: shown as it came.
  const next2 = m.nextRewrite();
  const spec2 = c.rewrite({ id: "3" });
  m.rewritten(enc.encode(JSON.stringify({ token: json(await next2).token, ok: false })));
  assert.equal(await spec2, null);
});

// An installed app opened with files (the launch queue): each saved into
// the app's caches, together, as one IncomingURL of file URLs, once Go
// is connected; folders are left out.
test("launch queue files", async () => {
  let consumer;
  const queue = { setConsumer(f) { consumer = f; } };
  const dispatched = [];
  const h = { core: null, dispatch: (b) => dispatched.push(b) };
  const saved = [];
  openLaunchFiles(h, async (f) => { saved.push(f.name); return `/tmp/shared/${saved.length}/${f.name}`; }, queue);
  const file = (name) => ({ kind: "file", getFile: async () => ({ name }) });
  await consumer({ files: [file("a b.md"), { kind: "directory" }, file("c.md")] });
  assert.deepEqual(saved, ["a b.md", "c.md"]);
  assert.equal(dispatched.length, 0, "waits for Go");
  h.core = {};
  for (const f of h.onConnect) f();
  assert.equal(dispatched.length, 1);
  const s = dec.decode(dispatched[0]);
  assert.ok(s.includes("file:///tmp/shared/1/a%20b.md\nfile:///tmp/shared/2/c.md"), s);
  openLaunchFiles(h, null, undefined); // no launch queue: nothing
});

// A Router's address in the browser (src/history.js), against a fake
// window whose history behaves as a browser's: pushState drops the
// entries after the current one, go(n) moves and fires popstate later,
// with the entry's state.
import { test } from "node:test";
import assert from "node:assert/strict";
import { RouteHistory, basePath, hrefOf, urlOf, decodeLocation, encodeLocationEvent } from "../src/history.js";
import { LocationAction as A } from "../src/ops.js";

function fakeWindow(base = "http://example.com/", start = base) {
  const w = {
    entries: [{ url: start, state: null }],
    at: 0,
    listeners: [],
    document: { baseURI: base },
    get location() {
      const u = new URL(this.entries[this.at].url);
      return { pathname: u.pathname, search: u.search, href: u.href };
    },
  };
  w.history = {
    get state() { return w.entries[w.at].state; },
    pushState(state, _, url) {
      w.entries = w.entries.slice(0, w.at + 1);
      w.entries.push({ url: new URL(url, w.entries[w.at].url).href, state: structuredClone(state) });
      w.at++;
    },
    replaceState(state, _, url) {
      w.entries[w.at] = { url: new URL(url, w.entries[w.at].url).href, state: structuredClone(state) };
    },
    go(n) {
      const i = w.at + n;
      if (i < 0 || i >= w.entries.length) return;
      w.at = i;
      setTimeout(() => w.fire(), 0);
    },
  };
  // The user's buttons: the browser moves, then fires popstate.
  w.back = () => { w.at--; w.fire(); };
  w.forward = () => { w.at++; w.fire(); };
  w.fire = () => { for (const f of w.listeners) f({ state: w.entries[w.at].state }); };
  w.urls = () => w.entries.map((e) => new URL(e.url).pathname + new URL(e.url).search);
  return w;
}

const tick = () => new Promise((r) => setTimeout(r, 5));

function setup(base, start) {
  const w = fakeWindow(base, start);
  const located = [];
  let guardBacks = 0;
  const h = new RouteHistory(w, { located: (href, state) => located.push([href, state]), guardBack: () => guardBacks++ });
  w.listeners.push((e) => h.popstate(e));
  return { w, h, located, guardBacks: () => guardBacks };
}

const loc = (action, href, state = "{}", delta = 0) => ({ action, delta, href, state });

test("the base path and hrefs under it", () => {
  assert.equal(basePath("http://x/"), "/");
  assert.equal(basePath("http://x/app/"), "/app/");
  assert.equal(basePath("http://x/app/index.html"), "/app/");
  assert.equal(hrefOf("/app/", "/app/chat/7", "?q=1"), "/chat/7?q=1");
  assert.equal(hrefOf("/app/", "/app"), "/");
  assert.equal(hrefOf("/app/", "/app/index.html"), "/");
  assert.equal(hrefOf("/", "/"), "/");
  assert.equal(urlOf("/app/", "/chat/7?q=1"), "/app/chat/7?q=1");
  assert.equal(urlOf("/", "/"), "/");
});

test("a Location command's args and the event's payload", () => {
  const href = new TextEncoder().encode("/chat/7"), state = new TextEncoder().encode('{"e":[]}');
  const b = new Uint8Array(1 + 4 + 4 + href.length + 4 + state.length);
  const dv = new DataView(b.buffer);
  dv.setUint8(0, A.Back);
  dv.setInt32(1, -2, true);
  dv.setUint32(5, href.length, true);
  b.set(href, 9);
  dv.setUint32(9 + href.length, state.length, true);
  b.set(state, 13 + href.length);
  assert.deepEqual(decodeLocation(b), { action: A.Back, delta: -2, href: "/chat/7", state: '{"e":[]}', window: "" });
  const key = new TextEncoder().encode("main");
  const withKey = new Uint8Array(b.length + 4 + key.length);
  withKey.set(b);
  new DataView(withKey.buffer).setUint32(b.length, key.length, true);
  withKey.set(key, b.length + 4);
  assert.equal(decodeLocation(withKey).window, "main");
  const ev = encodeLocationEvent("/a", "s");
  assert.deepEqual([...ev], [2, 0, 0, 0, 47, 97, 1, 0, 0, 0, 115]);
});

test("push, replace and back follow the router; the address is the screen's", async () => {
  const { w, h } = setup("http://example.com/app/", "http://example.com/app/");
  h.apply(loc(A.Replace, "/inbox", "s0"));
  h.apply(loc(A.Push, "/inbox/3", "s1"));
  h.apply(loc(A.Push, "/inbox/3/info", "s2"));
  assert.deepEqual(w.urls(), ["/app/inbox", "/app/inbox/3", "/app/inbox/3/info"]);
  assert.deepEqual(w.history.state, { asuka: { i: 2, href: "/inbox/3/info", route: "s2" } });
  // Back to the latest entry with the address: two at once.
  h.apply(loc(A.Back, "/inbox", "s0'", -2));
  await tick();
  assert.equal(w.at, 0);
  assert.equal(w.history.state.asuka.route, "s0'");
  // Commands while a back waits go after it.
  h.apply(loc(A.Push, "/inbox/4", "s3"));
  h.apply(loc(A.Back, "/inbox", "s4", -1));
  h.apply(loc(A.Push, "/inbox/5", "s5"));
  await tick();
  assert.deepEqual(w.urls(), ["/app/inbox", "/app/inbox/5"]);
  assert.equal(w.at, 1);
  // A back to an address the history doesn't have replaces.
  h.apply(loc(A.Back, "/inbox/5/x", "s6", -1));
  assert.deepEqual(w.urls(), ["/app/inbox", "/app/inbox/5/x"]);
  assert.equal(w.at, 1);
});

test("the user's back and forward go to the core with the entry's state", () => {
  const { w, h, located } = setup();
  h.apply(loc(A.Replace, "/", "a"));
  h.apply(loc(A.Push, "/chat/1", "b"));
  w.back();
  assert.deepEqual(located, [["/", "a"]]);
  // The core's answer, as it handles it, rewrites the entry the browser
  // is at, whatever it says.
  h.located = (href, state) => { located.push([href, state]); h.apply(loc(A.Push, "/chat/1", "b2")); };
  w.forward();
  assert.deepEqual(located.at(-1), ["/chat/1", "b"]);
  assert.equal(w.at, 1);
  assert.deepEqual(w.urls(), ["/", "/chat/1"]);
  assert.equal(w.history.state.asuka.route, "b2");
});

test("a back the app refuses goes forward again", async () => {
  const { w, h } = setup();
  h.apply(loc(A.Replace, "/", "a"));
  h.apply(loc(A.Push, "/edit", "b"));
  h.located = () => h.apply(loc(A.Forward, "/edit", "b"));
  w.back();
  await tick();
  assert.equal(w.at, 1);
  assert.deepEqual(w.urls(), ["/", "/edit"]);
});

test("a reload knows where it is in the history", () => {
  const w = fakeWindow();
  const h1 = new RouteHistory(w, { located() {}, guardBack() {} });
  h1.apply(loc(A.Replace, "/", "a"));
  h1.apply(loc(A.Push, "/chat/2", "b"));
  const h2 = new RouteHistory(w, { located() {}, guardBack() {} });
  assert.equal(h2.index, 1);
  assert.deepEqual(h2.location(), { href: "/chat/2", state: "b" });
});

test("a guard stands for the app's back while a dialog is open", async () => {
  const { w, h, located, guardBacks } = setup();
  h.apply(loc(A.Replace, "/", "a"));
  h.apply(loc(A.Push, "/chat/1", "b"));
  h.guard(true);
  assert.equal(w.entries.length, 3);
  assert.equal(w.history.state.asuka.guard, true);
  w.back(); // closes the dialog, not the chat
  assert.equal(guardBacks(), 1);
  assert.equal(located.length, 0);
  // Opened and closed by the app: the guard goes with a back of its own.
  h.guard(true);
  h.guard(false);
  await tick();
  assert.equal(w.at, 1);
  assert.equal(located.length, 0);
  // A dialog's button that navigates: the new entry takes the guard's place.
  h.guard(true);
  h.apply(loc(A.Push, "/chat/2", "c"));
  h.guard(false);
  await tick();
  assert.deepEqual(w.urls(), ["/", "/chat/1", "/chat/2"]);
  assert.equal(w.at, 2);
});

test("a URL of the app's own pages is its href", async () => {
  const { appURL } = await import("../src/history.js");
  assert.equal(appURL("https://example.com/app/chat/7?x=1", "https://example.com/app/", "https://example.com/app/"), "/chat/7?x=1");
  assert.equal(appURL("/app/chat/7", "https://example.com/app/inbox", "https://example.com/app/"), "/chat/7");
  assert.equal(appURL("https://other.com/app/chat/7", "https://example.com/app/", "https://example.com/app/"), "https://other.com/app/chat/7");
  assert.equal(appURL("https://example.com/blog", "https://example.com/app/", "https://example.com/app/"), "https://example.com/blog");
  assert.equal(appURL("mailto:a@b.c", "https://example.com/", "https://example.com/"), "mailto:a@b.c");
});

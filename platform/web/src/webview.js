// WebView: an <iframe>, made anew for each page RyUI loads (so the tab's
// history gets no entries of its own: the view keeps its own, as a web
// view does, and Back and Forward go through it).
//
// A page of the app's own origin (its assets, HTML, a file it wrote, any
// URL of the page's own site) is fetched by the host and written into a
// fresh frame, its URL set to the page's (history.replaceState), after
// window.ryui (postMessage) and Preload are in it: so they run before the
// page's own scripts, as WKUserScript at document start does. The host
// then has all of it: Eval and EvalJSResult (awaiting a promise), its
// title and URL as they change (pushState too), Block (its fetch, XHR,
// beacons, sockets and elements' src, and the markup's before it's
// parsed), Requests (the Resource Timing entries), new windows
// (window.open, target=_blank, a modifier click), downloads (an
// attachment, a type a frame can't show, <a download>), and its links and
// forms load through the host (the Navigation API's navigate event).
//
// Another site's page only loads: browsers keep sites apart. The view
// knows when it has loaded and when it couldn't be reached (a no-cors
// probe fails), but not where its links lead, its title, or its
// requests; it can't run script there, read its cookies or set the user
// agent. A page that cooperates talks to the view through postMessage
// (see the protocol in docs/web.md): messages, its URL and title, and
// script run for Eval. It's sandboxed (no top-level navigation: a link
// can't take the app away).

import { Event, Events, Command, KindProp as P, Writer } from "./ops.js";
import { own } from "./views.js";
import * as fs from "./fs.js";
import { decodeStrings, blocked, isHTML, canShow, isAttachment, filenameOf, sizeOf, mimeOf, toJSON, cookieHeader } from "./webview-util.js";

// The document a WebView wrote, by its load: a frame's document without
// it was loaded by the browser (a navigation the host didn't make).
const MARK = Symbol("ryui.webview");

// Another site's frame: everything a page does but navigate the app
// away (allow-top-navigation is left out) or reach the app's own origin.
const SANDBOX = "allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads allow-presentation allow-pointer-lock allow-orientation-lock allow-top-navigation-to-custom-protocols";
const ALLOW = "fullscreen; clipboard-write; autoplay; encrypted-media; picture-in-picture; web-share";

const warned = new Set();
function warnOnce(key, msg) {
  if (warned.has(key)) return;
  warned.add(key);
  console.warn("ryui: " + msg);
}

/** sameOrigin is a frame's window if the host may reach into it. */
export function sameOrigin(f) {
  try {
    const w = f?.contentWindow;
    if (w && w.document) return w;
  } catch {}
  return null;
}

export const webView = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "ryui-webview";
    const s = n.web = {
      el, frame: null,
      preload: "", block: [], requests: false,
      entries: [], index: -1, // the view's history: { kind: "url"|"html"|"file", url, html, path, method, body }
      gen: 0, abort: null, saved: null, expect: null, loadedGen: -1,
      url: "", title: "", loading: false, progress: 0, sent: "",
      coop: null, evals: new Map(), nextEval: 1,
      observers: [], pending: null,
    };
    s.onMessage = (e) => message(h, n, e);
    addEventListener("message", s.onMessage);
  },
  prop(h, n, p, v) {
    const s = n.web;
    switch (p) {
      case P.WebView.URL:
        if (v) schedule(h, n, { kind: "url", raw: v });
        break;
      case P.WebView.HTML:
        if (v) schedule(h, n, { kind: "html", html: v });
        break;
      case P.WebView.Preload:
        s.preload = v || "";
        break;
      case P.WebView.UserAgent:
        if (v) warnOnce("ua", "a WebView on the web can't set its user agent: a page can't choose a frame's");
        break;
      case P.WebView.Block:
        s.block = decodeStrings(v);
        break;
      case P.WebView.Requests:
        s.requests = !!v;
        break;
    }
  },
  command(h, n, cmd, args) {
    switch (cmd) {
      case Command.Eval:
        evalIn(n, new TextDecoder().decode(args)).catch((e) => console.warn("ryui: WebView script:", e?.message ?? e));
        return true;
      case Command.WebNavigate:
        [back, forward, reload, stop][args[0]]?.(h, n);
        return true;
    }
    return false;
  },
  destroy(h, n) {
    const s = n.web;
    removeEventListener("message", s.onMessage);
    s.gen++;
    s.abort?.abort();
    reset(s, "the WebView is gone");
    return false;
  },
  measure() { return [0, 0]; },
};

// --- Loading ---

/** schedule loads t once the batch's props are in (Preload, Block come before URL). */
function schedule(h, n, t) {
  const s = n.web;
  const first = !s.pending;
  s.pending = t;
  if (first) queueMicrotask(() => {
    const t = s.pending;
    s.pending = null;
    if (t) navigate(h, n, t, "push");
  });
}

/** resolve is the URL of a WebView's source: an asset's, a file's (file://), or as it is. */
function resolve(h, raw) {
  if (!raw) return { url: "" };
  if (raw.startsWith("file://")) {
    let path = raw.slice(7);
    try { path = decodeURIComponent(path); } catch {}
    return { url: raw, path };
  }
  if (raw.startsWith("/")) return { url: "file://" + encodeURI(raw), path: raw };
  if (/^[a-z][a-z0-9+.-]*:/i.test(raw)) return { url: raw };
  return { url: new URL(h.assets + "/" + raw.replace(/^\.?\//, ""), location.href).href };
}

const assetsBase = (h) => new URL(h.assets + "/", location.href).href;

/**
 * navigate loads an entry of the view's history: mode "push" adds it after
 * the current one, "replace" puts it in the current one's place, "traverse"
 * goes to the one s.index already is.
 */
async function navigate(h, n, t, mode) {
  const s = n.web;
  const gen = ++s.gen;
  s.abort?.abort();
  s.abort = null;
  s.expect = null;
  s.saved = { entries: s.entries.slice(), index: s.index };
  if (t.kind !== "html") Object.assign(t, resolve(h, t.raw ?? t.url));
  else t.url = "about:blank";
  if (mode === "push") {
    s.entries.splice(s.index + 1);
    s.entries.push(t);
    s.index = s.entries.length - 1;
  } else if (mode === "replace") {
    if (s.index < 0) s.index = 0;
    s.entries[s.index] = t;
    s.entries.length = Math.max(s.entries.length, s.index + 1);
  }
  s.url = t.url;
  s.title = "";
  s.loading = true;
  s.progress = 0.1;
  state(h, n);
  if (s.requests && t.url && t.kind !== "html") h.emit(n, Event.Request, (id) => Events.request(id, t.url));

  if (t.kind === "html") return mount(h, n, gen, t.html, { docURL: assetsBase(h) });
  if (!t.url) return fail(h, n, gen, "no URL");
  if (blocked(s.block, t.url)) return fail(h, n, gen, "blocked: " + t.url);
  if (t.path) {
    const b = fs.readFile(t.path);
    if (!b) return fail(h, n, gen, "no such file: " + t.path);
    if (/\.x?html?$/i.test(t.path)) return mount(h, n, gen, new TextDecoder().decode(b), { dir: t.path.slice(0, t.path.lastIndexOf("/") + 1) });
    return native(h, n, gen, fs.blobURL(t.path), false, false);
  }
  let u;
  try { u = new URL(t.url); } catch { return fail(h, n, gen, "not a URL: " + t.url); }
  if (u.origin !== location.origin || !/^https?:$/.test(u.protocol)) {
    const web = /^https?:$/.test(u.protocol);
    if (web && (s.preload || s.block.length || s.requests)) {
      warnOnce("other-site", "a WebView on the web runs Preload, applies Block and reports Requests only in pages of the app's own site: a browser keeps another site's page (" + u.origin + ") apart");
    }
    return native(h, n, gen, t.url, web, web);
  }
  // A page of the app's own site: fetched, then written into a frame.
  const ac = new AbortController();
  s.abort = ac;
  let r, body;
  try {
    r = await fetch(t.url, { signal: ac.signal, credentials: "same-origin", method: t.method || "GET", body: t.body });
    if (gen !== s.gen) return;
    s.progress = 0.4;
    state(h, n);
    const ct = r.headers.get("content-type") || "", cd = r.headers.get("content-disposition") || "";
    if (isAttachment(cd) || !canShow(ct)) {
      ac.abort();
      return download(h, n, gen, { url: r.url, mime: mimeOf(ct), filename: filenameOf(cd, r.url), size: sizeOf(r.headers.get("content-length")) });
    }
    if (!isHTML(ct)) {
      ac.abort();
      return native(h, n, gen, r.url, false, false);
    }
    body = await r.text();
  } catch (e) {
    if (gen !== s.gen) return;
    if (e?.name === "AbortError") return stopped(h, n, gen);
    return fail(h, n, gen, String(e?.message ?? e));
  }
  if (gen !== s.gen) return;
  s.abort = null;
  if (r.url !== t.url) s.url = t.url = r.url; // redirected
  mount(h, n, gen, body, { docURL: r.url });
}

/** frame replaces the view's frame with a new one: src's (another site's, sandboxed if web), or a blank one to write into. */
function frame(h, n, src, sandbox) {
  const s = n.web;
  reset(s, "the page changed");
  s.mounting = false;
  const f = document.createElement("iframe");
  f.setAttribute("allow", ALLOW);
  if (sandbox) f.setAttribute("sandbox", SANDBOX);
  f.addEventListener("load", () => frameLoaded(h, n, f));
  if (src) f.src = src;
  s.frame?.remove();
  s.el.appendChild(f);
  s.frame = f;
  n.frameEl = f; // print.WebView's
  return f;
}

/** reset forgets the frame's page: its observers, its cooperation, the scripts waiting for it. */
function reset(s, why) {
  for (const o of s.observers) try { o.disconnect(); } catch {}
  s.observers = [];
  s.coop = null;
  const evals = s.evals;
  s.evals = new Map();
  for (const p of evals.values()) p.reject(new Error(why));
}

/** blank is the page a frame loads before RyUI writes one into it: a real
 * document of the app's origin (the initial about:blank has the Navigation
 * API turned off, so its links couldn't be followed through the host). */
let blankURL = "";
const blank = () => (blankURL ||= URL.createObjectURL(new Blob(["<!doctype html>"], { type: "text/html" })));

/** mount writes a page of the app's own into a new frame: window.ryui and Preload first. */
function mount(h, n, gen, html, { docURL, dir }) {
  const s = n.web;
  try {
    if (s.block.length || dir) html = rewrite(html, s.block, dir, docURL);
  } catch (e) {
    return fail(h, n, gen, String(e?.message ?? e));
  }
  const f = frame(h, n, blank(), false);
  s.mounting = true;
  f.addEventListener("load", () => {
    if (gen !== s.gen || f !== s.frame) return;
    let w;
    try {
      w = f.contentWindow;
      const d = f.contentDocument;
      d.open();
      d[MARK] = gen;
      // Its URL is the page's, so its links, fetches and location are too.
      if (docURL) try { w.history.replaceState(null, "", docURL); } catch {}
      attach(h, n, w, gen);
      w.addEventListener("load", () => loaded(h, n, gen));
      d.write(html);
      d.close();
    } catch (e) {
      s.mounting = false;
      return fail(h, n, gen, String(e?.message ?? e));
    }
    s.mounting = false;
    if (gen === s.gen && w.document.readyState === "complete") loaded(h, n, gen);
  }, { once: true });
}

/** native loads url into a new frame, as the browser does: another site's page, a file a frame shows. */
function native(h, n, gen, url, sandbox, probe) {
  const s = n.web;
  let probed = !probe, failure = "", done = false;
  const finish = () => {
    if (gen !== s.gen || !probed || !done) return;
    if (failure) return fail(h, n, gen, failure);
    loaded(h, n, gen);
  };
  s.expect = { gen, loaded: () => { done = true; finish(); } };
  frame(h, n, url, sandbox);
  if (probe) {
    // A frame's load fires for an error page too: whether the site
    // answers at all is a no-cors request's (opaque, but it fails when
    // the network does).
    const ac = new AbortController();
    s.abort = ac;
    fetch(url, { mode: "no-cors", method: "HEAD", credentials: "omit", cache: "no-store", signal: ac.signal })
      .then(() => { probed = true; finish(); }, (e) => {
        if (e?.name === "AbortError") return;
        probed = true;
        failure = "couldn't load " + url + ": " + (e?.message || "the network failed");
        finish();
      });
  }
}

/** frameLoaded is the frame's load: the end of a load the host made, or a navigation it didn't (a link in another site's page). */
function frameLoaded(h, n, f) {
  const s = n.web;
  if (f !== s.frame || s.mounting) return;
  const w = sameOrigin(f);
  const mark = w?.document?.[MARK];
  if (mark !== undefined) {
    if (mark === s.gen) loaded(h, n, s.gen);
    return;
  }
  if (s.expect) {
    const e = s.expect;
    s.expect = null;
    if (e.gen === s.gen) e.loaded();
    return;
  }
  if (w && w.location.href === "about:blank" && !w.document.body?.childNodes.length) return; // a frame's first, empty document
  // A navigation the host didn't make.
  const gen = ++s.gen;
  s.abort?.abort();
  s.abort = null;
  reset(s, "the page changed");
  const url = w ? w.location.href : null;
  if (url && s.entries[s.index - 1]?.url === url) s.index--;
  else if (url && s.entries[s.index + 1]?.url === url) s.index++;
  else {
    s.entries.splice(s.index + 1);
    s.entries.push({ kind: "url", url, raw: url });
    s.index = s.entries.length - 1;
  }
  if (url) s.url = url;
  s.title = "";
  if (w) attach(h, n, w, gen); // too late for the page's scripts, but its links, title, messages and script
  else hello(s);
  loaded(h, n, gen);
}

/** loaded reports a page loaded, once. */
function loaded(h, n, gen) {
  const s = n.web;
  if (gen !== s.gen || s.loadedGen === gen) return;
  s.loadedGen = gen;
  s.loading = false;
  s.progress = 1;
  s.saved = null;
  const w = sameOrigin(s.frame);
  if (w) try { s.title = w.document.title; } catch {}
  else hello(s);
  if (s.frame && s.title) s.frame.title = s.title;
  h.emit(n, Event.Navigated, (id) => Events.navigated(id, s.url));
  state(h, n);
}

/** revert gives the view back the history it had before a load that didn't happen. */
function revert(s) {
  if (!s.saved) return;
  s.entries = s.saved.entries;
  s.index = s.saved.index;
  s.saved = null;
  s.url = s.entries[s.index]?.url ?? "";
}

function fail(h, n, gen, message) {
  const s = n.web;
  if (gen !== s.gen) return;
  s.abort = null;
  s.expect = null;
  s.loading = false;
  s.progress = 0;
  revert(s);
  h.emit(n, Event.Failed, (id) => Events.failed(id, message));
  state(h, n);
}

function stopped(h, n, gen) {
  const s = n.web;
  if (gen !== s.gen) return;
  s.abort = null;
  s.loading = false;
  s.progress = 0;
  revert(s);
  state(h, n);
}

function download(h, n, gen, d) {
  const s = n.web;
  stopped(h, n, gen);
  emitDownload(h, n, d);
}

function emitDownload(h, n, d) {
  h.emit(n, Event.Download, (id) => Events.download(id, new Writer().string(d.url).string(d.mime || "").string(d.filename || "").i64(d.size ?? -1).done()));
}

/** state reports the page's state (WebState), once for the changes of a turn. */
function state(h, n) {
  const s = n.web;
  if (!n.listening(Event.WebState) || s.stateQueued) return;
  s.stateQueued = true;
  queueMicrotask(() => {
    s.stateQueued = false;
    const flags = (s.loading ? 1 : 0) | (canBack(s) ? 2 : 0) | (canForward(s) ? 4 : 0);
    const key = `${s.url}\n${s.title}\n${flags}\n${s.progress}`;
    if (key === s.sent) return;
    s.sent = key;
    h.dispatch(Events.webState(n.id, new Writer().string(s.url).string(s.title).u8(flags).f32(s.progress).done()));
  });
}

// --- History ---

function frameNavigation(s) {
  try { return sameOrigin(s.frame)?.navigation ?? null; } catch { return null; }
}

function canBack(s) {
  if (frameNavigation(s)?.canGoBack) return true;
  for (let i = s.index - 1; i >= 0; i--) if (s.entries[i].url) return true;
  return false;
}

function canForward(s) {
  if (frameNavigation(s)?.canGoForward) return true;
  for (let i = s.index + 1; i < s.entries.length; i++) if (s.entries[i].url) return true;
  return false;
}

function back(h, n) {
  const s = n.web;
  const nav = frameNavigation(s);
  if (nav?.canGoBack) return void nav.back();
  let i = s.index - 1;
  while (i >= 0 && !s.entries[i].url) i--;
  if (i < 0) return;
  s.index = i;
  navigate(h, n, s.entries[i], "traverse");
}

function forward(h, n) {
  const s = n.web;
  const nav = frameNavigation(s);
  if (nav?.canGoForward) return void nav.forward();
  let i = s.index + 1;
  while (i < s.entries.length && !s.entries[i].url) i++;
  if (i >= s.entries.length) return;
  s.index = i;
  navigate(h, n, s.entries[i], "traverse");
}

function reload(h, n) {
  const s = n.web;
  const e = s.entries[s.index];
  if (e?.url) navigate(h, n, e, "traverse");
}

function stop(h, n) {
  const s = n.web;
  if (!s.loading) return;
  s.abort?.abort();
  s.abort = null;
  try { sameOrigin(s.frame)?.stop(); } catch {}
  s.expect = null;
  stopped(h, n, s.gen);
}

// --- A page of the app's own ---

/** attach gives a page of the app's own the bridge, Preload and the host's hooks. */
function attach(h, n, w, gen) {
  const s = n.web;
  const live = () => gen === s.gen && s.frame?.contentWindow === w;
  const doc = w.document;
  try {
    w.ryui = { postMessage: (m) => { if (live()) h.emit(n, Event.Message, (id) => Events.message(id, String(m))); } };
  } catch {}
  if (s.preload) {
    try { w.eval(s.preload); } catch (e) { console.warn("ryui: WebView Preload:", e?.message ?? e); }
  }
  const abs = (u) => { try { return new URL(String(u), doc.baseURI).href; } catch { return ""; } };
  if (s.block.length) blockIn(s, w, abs);

  if (s.requests) {
    try {
      const o = new w.PerformanceObserver((list) => {
        if (!live()) return;
        for (const e of list.getEntries()) h.emit(n, Event.Request, (id) => Events.request(id, e.name));
      });
      o.observe({ type: "resource", buffered: true });
      s.observers.push(o);
    } catch {}
  }

  // Its title and URL as they change.
  const changed = () => {
    if (!live()) return;
    let t = "";
    try { t = doc.title; } catch {}
    const e = s.entries[s.index];
    const url = e?.kind === "html" || e?.kind === "file" || e?.path ? s.url : w.location.href;
    if (t !== s.title || url !== s.url) {
      s.title = t;
      s.url = url;
      if (e && e.kind === "url") e.url = url;
      if (s.frame && t) s.frame.title = t;
      state(h, n);
    }
  };
  if (n.listening(Event.WebState)) {
    try {
      const o = new w.MutationObserver(changed);
      o.observe(doc, { subtree: true, childList: true, characterData: true });
      s.observers.push(o);
    } catch {}
  }
  for (const k of ["pushState", "replaceState"]) {
    const orig = w.history[k];
    w.history[k] = function () { const r = orig.apply(this, arguments); changed(); return r; };
  }
  w.addEventListener("popstate", changed);
  w.addEventListener("hashchange", changed);

  // New windows, downloads, links.
  const newWindow = (url) => {
    if (!url || url === "about:blank") return;
    if (n.listening(Event.NewWindow)) h.emit(n, Event.NewWindow, (id) => Events.newWindow(id, url));
    else follow(h, n, url, {});
  };
  w.open = function (u, target) {
    const url = u == null || u === "" ? "" : abs(u);
    if (!live() || !url) return null;
    if (/^_(self|parent|top)$/i.test(target || "")) follow(h, n, url, {});
    else newWindow(url);
    return null;
  };
  const onClick = (e) => {
    if (!live() || e.defaultPrevented || e.button > 1) return;
    const a = e.target?.closest?.("a[href], area[href]");
    if (!a) return;
    const url = a.href;
    if (/^javascript:/i.test(url)) return;
    if (a.hasAttribute("download")) {
      e.preventDefault();
      emitDownload(h, n, { url, mime: a.type || "", filename: a.getAttribute("download") || filenameOf("", url), size: -1 });
      return;
    }
    const target = (a.getAttribute("target") || doc.querySelector("base[target]")?.getAttribute("target") || "").toLowerCase();
    if ((target && !/^_(self|parent|top)$/.test(target)) || e.ctrlKey || e.metaKey || e.shiftKey || e.button === 1) {
      e.preventDefault();
      newWindow(url);
      return;
    }
    // Without the Navigation API (and for a file's page, whose links are
    // the app's files), the host follows the link itself.
    const here = w.location.href.split("#")[0];
    if (url.split("#")[0] === here && url.includes("#")) return;
    if (!w.navigation || url.startsWith("file:")) {
      e.preventDefault();
      follow(h, n, url, {});
    }
  };
  w.addEventListener("click", onClick);
  w.addEventListener("auxclick", onClick);
  if (!w.navigation) {
    // Without the Navigation API, a form's submission goes through the host too.
    w.addEventListener("submit", (e) => {
      if (!live() || e.defaultPrevented) return;
      const form = e.target, submitter = e.submitter;
      const target = (submitter?.getAttribute("formtarget") || form.getAttribute("target") || "").toLowerCase();
      const method = (submitter?.getAttribute("formmethod") || form.method || "get").toLowerCase();
      if (method === "dialog") return;
      const action = abs(submitter?.getAttribute("formaction") || form.getAttribute("action") || w.location.href);
      const data = new w.FormData(form, submitter);
      e.preventDefault();
      if (method === "post") {
        if (target && !/^_(self|parent|top)$/.test(target)) return newWindow(action);
        return follow(h, n, action, { method: "POST", body: data });
      }
      const u = new URL(action);
      u.search = new URLSearchParams(data).toString();
      if (target && !/^_(self|parent|top)$/.test(target)) newWindow(u.href);
      else follow(h, n, u.href, {});
    });
  }
  // The page's navigations (links, forms, location = ...) load through
  // the host: Preload and the bridge again, Block, its history.
  try {
    w.navigation?.addEventListener("navigate", (e) => {
      if (!live() || !e.cancelable || e.navigationType === "traverse" || e.hashChange || e.destination.sameDocument) return;
      const url = e.destination.url;
      if (/^javascript:/i.test(url)) return;
      e.preventDefault();
      if (e.downloadRequest != null) {
        emitDownload(h, n, { url, mime: "", filename: e.downloadRequest || filenameOf("", url), size: -1 });
        return;
      }
      if (e.navigationType === "reload") return reload(h, n);
      const fd = e.formData;
      follow(h, n, url, { replace: e.navigationType === "replace", method: fd ? "POST" : undefined, body: fd ?? undefined });
    });
  } catch {}
}

/** follow loads a URL the page leads to. */
function follow(h, n, url, { replace, method, body }) {
  const s = n.web;
  if (blocked(s.block, url)) return;
  navigate(h, n, { kind: "url", raw: url, method, body }, replace ? "replace" : "push");
}

/** blockIn keeps a page's requests to Block's patterns from going out: its scripts' (fetch, XHR, beacons, sockets, event sources) and its elements'. */
function blockIn(s, w, abs) {
  const no = (u) => u != null && blocked(s.block, abs(u));
  const fetch0 = w.fetch;
  w.fetch = function (input, init) {
    const u = typeof input === "string" || input instanceof w.URL ? input : input?.url;
    if (no(u)) return w.Promise.reject(new w.TypeError("Failed to fetch: blocked"));
    return fetch0.apply(this, arguments);
  };
  const X = w.XMLHttpRequest?.prototype;
  if (X) {
    const open0 = X.open, send0 = X.send;
    X.open = function (m, u) { this.__ryuiBlocked = no(u); return open0.apply(this, arguments); };
    X.send = function () {
      if (!this.__ryuiBlocked) return send0.apply(this, arguments);
      const x = this;
      setTimeout(() => { x.dispatchEvent(new w.ProgressEvent("error")); x.dispatchEvent(new w.ProgressEvent("loadend")); });
    };
  }
  if (w.navigator.sendBeacon) {
    const beacon0 = w.navigator.sendBeacon.bind(w.navigator);
    w.navigator.sendBeacon = (u, d) => (no(u) ? false : beacon0(u, d));
  }
  for (const C of ["WebSocket", "EventSource"]) {
    if (!w[C]) continue;
    w[C] = new Proxy(w[C], {
      construct(t, args, nt) {
        if (no(args[0])) throw new w.DOMException("blocked", "SecurityError");
        return Reflect.construct(t, args, nt);
      },
    });
  }
  const guard = (C, prop) => {
    const proto = w[C]?.prototype;
    const d = proto && Object.getOwnPropertyDescriptor(proto, prop);
    if (!d?.set) return;
    Object.defineProperty(proto, prop, { ...d, set(v) { if (!no(v)) d.set.call(this, v); } });
  };
  for (const C of ["HTMLImageElement", "HTMLScriptElement", "HTMLIFrameElement", "HTMLSourceElement", "HTMLMediaElement", "HTMLEmbedElement", "HTMLTrackElement"]) guard(C, "src");
  guard("HTMLLinkElement", "href");
  guard("HTMLObjectElement", "data");
  const resources = /^(img|script|iframe|source|video|audio|embed|track|link|object|frame)$/i;
  const setAttribute0 = w.Element.prototype.setAttribute;
  w.Element.prototype.setAttribute = function (name, value) {
    if (resources.test(this.localName) && /^(src|href|data)$/i.test(name) && no(value)) return;
    return setAttribute0.call(this, name, value);
  };
  // Elements made from markup (innerHTML) after the page's own was cleaned.
  const o = new w.MutationObserver((records) => {
    for (const r of records) for (const el of r.addedNodes) {
      if (el.nodeType !== 1) continue;
      for (const e of [el, ...el.querySelectorAll("[src], link[href], object[data]")]) unblock(e, no);
    }
  });
  o.observe(w.document, { subtree: true, childList: true });
  s.observers.push(o);
}

function unblock(e, no) {
  for (const a of ["src", "href", "data"]) {
    const v = e.getAttribute?.(a);
    if (v != null && /^(img|script|iframe|source|video|audio|embed|track|link|object|frame)$/i.test(e.localName) && no(v)) {
      e.removeAttribute(a);
      if (e.localName === "script") e.type = "text/plain";
    }
  }
}

/**
 * rewrite cleans a page's markup before it's parsed: what Block blocks
 * doesn't load; a file's page (dir: its directory in the app's files)
 * gets its relative resources from the app's files.
 */
function rewrite(html, block, dir, base) {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const baseURL = dir ? "file://" + encodeURI(dir) : base || location.href;
  const abs = (u) => { try { return new URL(u, baseURL).href; } catch { return ""; } };
  if (block.length) {
    const no = (u) => blocked(block, abs(u));
    for (const e of doc.querySelectorAll("[src], link[href], object[data]")) unblock(e, no);
  }
  if (dir) {
    for (const e of doc.querySelectorAll("[src], link[href], [poster], a[href], area[href]")) {
      for (const a of ["src", "href", "poster"]) {
        const v = e.getAttribute(a);
        if (!v || /^[a-z][a-z0-9+.-]*:|^#|^\/\//i.test(v)) continue;
        const u = abs(v);
        if (!u.startsWith("file://")) continue;
        let path = u.slice(7).split(/[?#]/)[0];
        try { path = decodeURIComponent(path); } catch {}
        if (e.localName === "a" || e.localName === "area") e.setAttribute(a, u); // followed through the host
        else if (fs.readFile(path)) e.setAttribute(a, fs.blobURL(path));
      }
    }
  }
  return (doc.doctype ? "<!doctype html>\n" : "") + doc.documentElement.outerHTML;
}

// --- Messages: the app's own page, and another site's that cooperates ---

/**
 * message is what the view's frame posts (event.source is its window):
 *   { ryui: true, text }             a message (window.ryui.postMessage)
 *   { ryui: "ready", url, title }     the page cooperates: Eval goes to it
 *   { ryui: "state", url, title }     its URL or title changed
 *   { ryui: "result", id, value }     an Eval's value (JSON), or { error }
 */
function message(h, n, e) {
  const s = n.web;
  if (!s.frame || e.source !== s.frame.contentWindow) return;
  const d = e.data;
  if (!d || typeof d !== "object") return;
  if (d.ryui === true) {
    h.emit(n, Event.Message, (id) => Events.message(id, String(d.text)));
  } else if (d.ryui === "ready" || d.ryui === "state") {
    s.coop = e.origin;
    let changed = false;
    if (typeof d.url === "string" && d.url !== s.url) {
      s.url = d.url;
      const en = s.entries[s.index];
      if (en && !sameOrigin(s.frame)) en.url = d.url;
      changed = true;
    }
    if (typeof d.title === "string" && d.title !== s.title) {
      s.title = d.title;
      if (d.title) s.frame.title = d.title;
      changed = true;
    }
    if (changed) state(h, n);
  } else if (d.ryui === "result") {
    const p = s.evals.get(d.id);
    if (!p) return;
    s.evals.delete(d.id);
    if (typeof d.error === "string") p.reject(new Error(d.error));
    else p.resolve(typeof d.value === "string" ? d.value : toJSON(d.value));
  }
}

/** hello asks another site's page whether it cooperates (it answers "ready"). */
function hello(s) {
  try { s.frame?.contentWindow?.postMessage({ ryui: "hello" }, "*"); } catch {}
}

/**
 * evalIn runs script in a WebView's page and resolves to its value as
 * JSON (the last statement's, or what the promise it returns resolves
 * to; null for undefined).
 */
export async function evalIn(n, script) {
  const s = n?.web;
  if (!s) throw new Error("not a WebView");
  const w = sameOrigin(s.frame);
  if (w) {
    const v = await w.eval(script);
    return toJSON(v, w.JSON);
  }
  if (s.coop && s.frame) {
    const id = s.nextEval++;
    return new Promise((resolve, reject) => {
      s.evals.set(id, { resolve, reject });
      s.frame.contentWindow.postMessage({ ryui: "eval", id, script }, s.coop);
    });
  }
  throw new Error("a WebView on the web runs script only in a page of the app's own (its assets, its HTML), or in another site's page that answers RyUI's messages: browsers keep sites apart");
}

/** cookies are the page's own site's cookies for url (document.cookie: not HttpOnly ones), as a Cookie header. */
export function cookies(url) {
  let u;
  try { u = new URL(url, location.href); } catch { return ""; }
  if (u.origin !== location.origin) {
    throw new Error("WebCookies: on the web, the app reads only its own site's cookies (" + location.origin + "): a browser keeps another site's from it");
  }
  return cookieHeader(document.cookie);
}

/**
 * runScript runs a script in a page of its own (ryui/js): a hidden frame,
 * sandboxed (an origin of its own: it can't reach the app or its
 * storage), made for it and let go after. It resolves to the value as
 * JSON, or rejects with what it threw, or "timeout".
 */
export function runScript(ms, script) {
  return new Promise((resolve, reject) => {
    const token = globalThis.crypto?.randomUUID?.() ?? String(Math.random()).slice(2);
    const f = document.createElement("iframe");
    f.setAttribute("sandbox", "allow-scripts");
    f.setAttribute("aria-hidden", "true");
    f.tabIndex = -1;
    f.style.cssText = "position:fixed;left:-10000px;top:0;width:1px;height:1px;border:0;visibility:hidden";
    let timer = 0;
    const end = (ok, v) => {
      clearTimeout(timer);
      removeEventListener("message", onMessage);
      f.remove();
      ok ? resolve(v) : reject(new Error(v));
    };
    const onMessage = (e) => {
      if (e.source !== f.contentWindow || !e.data || e.data.t !== token) return;
      if (e.data.ready) f.contentWindow.postMessage({ t: token, s: script }, "*");
      else end(e.data.ok, e.data.v);
    };
    addEventListener("message", onMessage);
    timer = setTimeout(() => end(false, "timeout"), Math.max(0, ms));
    const t = JSON.stringify(token);
    f.srcdoc = `<!doctype html><meta charset="utf-8"><script>
addEventListener("message", async function (e) {
  if (e.source !== parent || !e.data || e.data.t !== ${t}) return;
  var out;
  try {
    var v = await (0, eval)(e.data.s);
    var j = JSON.stringify(v === undefined ? null : v);
    out = { t: ${t}, ok: true, v: j === undefined ? "null" : j };
  } catch (err) {
    out = { t: ${t}, ok: false, v: String(err && err.message !== undefined ? err.message : err) };
  }
  parent.postMessage(out, "*");
});
parent.postMessage({ t: ${t}, ready: true }, "*");
<\/script>`;
    document.body.appendChild(f);
  });
}

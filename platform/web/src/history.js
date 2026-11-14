// A Router's address in the browser: the screen shown is the page's
// address (real paths, under the app's base: <base href>), and each step
// of its navigation an entry of the page's history, keeping the router's
// state, so the back and forward buttons, a reload and a link pasted in
// the address bar show the screens they stand for.
//
// The core says how its state moved (the Location command): Push a new
// entry (pushState), Replace this one's (replaceState), Back to an
// earlier one (to the latest entry back with the same address, as
// react-navigation's linking goes back; none: the state shrank in a way
// the history never grew, a deep link's screens popped one by one, and
// this entry is replaced), Forward to the one the user left (a back the
// app's OnBack refused). The user's back and forward (popstate) go to the
// core as a Location event, with the entry's address and the state it
// kept; while the core handles it, its answer only rewrites the entry the
// browser is at.
//
// Entries are { asuka: { i, href, route } }: i is the entry's place in
// the page's history (so it's known after a reload), route the router's
// state. A guard entry (guard: true) stands for the app's back while a
// dialog or a stack the router doesn't route is open (nav.js): the
// user's back from it goes back in the app, not to another address.

import { LocationAction } from "./ops.js";

const utf8 = new TextDecoder();
const utf8Out = new TextEncoder();

/** basePath is the app's base: the directory of the page's base URL ("/" or "/app/"). */
export function basePath(baseURI) {
  const p = new URL(baseURI, "http://x/").pathname;
  return p.endsWith("/") ? p : p.slice(0, p.lastIndexOf("/") + 1);
}

/** hrefOf is the app's href for a page address under base: "/app/chat/7?x" is "/chat/7?x". */
export function hrefOf(base, pathname, search = "") {
  let p = pathname;
  if (p.startsWith(base)) p = "/" + p.slice(base.length);
  else if (p + "/" === base) p = "/";
  if (p.endsWith("/index.html")) p = p.slice(0, -"index.html".length);
  return p + search;
}

/** urlOf is the page address of an href under base. */
export function urlOf(base, href) {
  return base + href.replace(/^\//, "");
}

/**
 * appURL is a URL the app is opened with (a notification's) as the
 * core's routes read it: one of the app's own pages, under its base, is
 * its href ("https://example.com/app/chat/7" is "/chat/7"); another is as
 * it is.
 */
export function appURL(url, page = globalThis.location?.href, baseURI = globalThis.document?.baseURI) {
  if (!page || !baseURI) return url;
  try {
    const u = new URL(url, page), base = basePath(baseURI);
    if (u.origin !== new URL(page).origin || !(u.pathname + "/").startsWith(base)) return url;
    return hrefOf(base, u.pathname, u.search);
  } catch {
    return url;
  }
}

/** decodeLocation reads a Location command's args (ops.AppendLocation). */
export function decodeLocation(args) {
  const dv = new DataView(args.buffer, args.byteOffset, args.byteLength);
  const action = dv.getUint8(0);
  const delta = dv.getInt32(1, true);
  let o = 5;
  const hl = dv.getUint32(o, true);
  o += 4;
  const href = utf8.decode(args.subarray(o, o + hl));
  o += hl;
  const sl = dv.getUint32(o, true);
  o += 4;
  const state = utf8.decode(args.subarray(o, o + sl));
  o += sl;
  let window = "";
  if (o + 4 <= args.length) {
    const wl = dv.getUint32(o, true);
    window = utf8.decode(args.subarray(o + 4, o + 4 + wl));
  }
  return { action, delta, href, state, window };
}

/** encodeLocationEvent is a Location event's payload (ops.AppendLocationEvent). */
export function encodeLocationEvent(href, state) {
  const h = utf8Out.encode(href), s = utf8Out.encode(state || "");
  const b = new Uint8Array(8 + h.length + s.length);
  const dv = new DataView(b.buffer);
  dv.setUint32(0, h.length, true);
  b.set(h, 4);
  dv.setUint32(4 + h.length, s.length, true);
  b.set(s, 8 + h.length);
  return b;
}

export class RouteHistory {
  /**
   * win: the window (its history, location and document); located(href,
   * state): the user went back or forward to an entry of the router's;
   * guardBack(): the user went back from a guard entry.
   */
  constructor(win, { located, guardBack }) {
    this.win = win;
    this.located = located;
    this.guardBack = guardBack;
    this.base = basePath(win.document.baseURI);
    const st = win.history.state?.asuka;
    this.index = typeof st?.i === "number" ? st.i : 0;
    this.items = []; // what's known of the entries, by index: { href }
    this.items[this.index] = { href: this.href() };
    this.waiting = null; // a history.go of ours, until its popstate: { n, done }
    this.queue = []; // commands that came while one waited
    this.popFrom = null; // the entry the user's back or forward left, while the core handles it
    this.guardAt = -1; // the guard entry's index, while there's one
  }

  /** href is the app's href of the page's address. */
  href() {
    const l = this.win.location;
    return hrefOf(this.base, l.pathname, l.search);
  }

  /** location is where the page is: its href and the router's state its entry kept ("" for none). */
  location() {
    const st = this.win.history.state?.asuka;
    return { href: this.href(), state: st?.route || "" };
  }

  record(href, state, guard) {
    const r = { i: this.index, href, route: state };
    if (guard) r.guard = true;
    return { asuka: r };
  }

  /** apply follows a Location command. */
  apply(l) {
    if (this.waiting) {
      this.queue.push(l);
      return;
    }
    const h = this.win.history;
    if (this.popFrom !== null) {
      // The browser is at the entry the user went to already.
      if (l.action === LocationAction.Forward && this.popFrom !== this.index) {
        this.go(this.popFrom - this.index, () => this.replace(l));
        return;
      }
      this.replace(l);
      return;
    }
    switch (l.action) {
      case LocationAction.Push:
        if (this.guardAt === this.index) {
          // Over a guard (a dialog's button that navigates): it takes the
          // guard's place, which the dialog closing needn't go back from.
          this.guardAt = -1;
          this.replace(l);
          return;
        }
        this.index++;
        this.items.length = this.index;
        this.items[this.index] = { href: l.href };
        h.pushState(this.record(l.href, l.state), "", urlOf(this.base, l.href));
        return;
      case LocationAction.Back: {
        let j = -1;
        for (let k = this.index - 1; k >= 0; k--) {
          if (this.items[k]?.href === l.href) { j = k; break; }
        }
        if (j < 0) { this.replace(l); return; }
        this.go(j - this.index, () => this.replace(l));
        return;
      }
      default:
        this.replace(l);
    }
  }

  replace(l) {
    this.items[this.index] = { href: l.href };
    if (this.guardAt === this.index) this.guardAt = -1;
    this.win.history.replaceState(this.record(l.href, l.state), "", urlOf(this.base, l.href));
  }

  /** go moves n entries, then calls done once the browser has (its popstate isn't the user's). */
  go(n, done) {
    if (!n) { done?.(); return; }
    this.waiting = { n, done };
    this.win.history.go(n);
  }

  /** guard keeps an entry for the app's back while want (nav.js: a dialog, a stack the router doesn't route). */
  guard(want) {
    if (this.waiting) return;
    if (want && this.guardAt < 0) {
      const cur = this.win.history.state?.asuka;
      this.index++;
      this.items.length = this.index;
      this.items[this.index] = { href: this.href() };
      this.guardAt = this.index;
      this.win.history.pushState(this.record(this.href(), cur?.route || "", true), "", this.win.location.href);
    } else if (!want && this.guardAt >= 0) {
      const at = this.guardAt;
      this.guardAt = -1;
      if (at === this.index) this.go(-1);
    }
  }

  /** popstate is the browser moving in its history. */
  popstate(ev) {
    const st = ev.state?.asuka;
    const from = this.index;
    this.index = typeof st?.i === "number" ? st.i : Math.max(0, from - 1);
    this.items[this.index] = { href: st?.href ?? this.href() };
    if (this.waiting) {
      const { done } = this.waiting;
      this.waiting = null;
      done?.();
      while (!this.waiting && this.queue.length) this.apply(this.queue.shift());
      return;
    }
    if (this.guardAt >= 0 && from === this.guardAt && this.index < from) {
      // The user's back from the guard: the app goes back.
      this.guardAt = -1;
      this.guardBack();
      if (this.index === from - 1) return;
    }
    this.popFrom = from;
    try {
      this.located(this.href(), st?.route || "");
    } finally {
      this.popFrom = null;
    }
  }
}

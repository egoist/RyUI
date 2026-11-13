// Controlling windows in a browser (ui.WindowHandle, WindowOptions,
// WindowProps).
//
// The main window is the browser's tab: of what an app may do to a
// window, a page can go full screen (the Fullscreen API, which a browser
// allows in answer to the user: a press's handler, which the core runs
// synchronously in the click's event) and take the focus (window.focus).
// It can't hide, minimize, maximize, move, size or keep the tab on top,
// so those do nothing. Its state is the tab's: visible while the tab is
// the one shown (visibilitychange), focused while the page has the
// keyboard, full screen while the page is, its size the app's area, its
// position the browser window's on the screen (screenX, screenY). The
// user closing the tab, or leaving the page, asks the app if it wants to
// be asked (CloseRequest, from beforeunload): a refusal keeps the page,
// behind the browser's own "Leave site?" question (a page can't word it),
// and the app's answer (its Alert) shows if the user stays.
//
// A Window is a panel in the page (nav.js): of those, a handle shows and
// hides it, brings it to the front with the keyboard in it, maximizes it
// over the app's area and restores it, collapses it to its title bar for
// Minimize, puts it in full screen (the Fullscreen API on the panel),
// moves, sizes and centers it, and keeps it above the others (always on
// top). Its options are a panel's: not resizable (no grip), no close or
// maximize button (a double click on the bar maximizes), its least and
// most size, its place, its background, hidden, maximized, a hidden title
// bar (the bar's buttons over the content, its height the content's
// SafeTop), modal (a backdrop over the page until it closes). With a
// state key its place, size and whether it was maximized are kept in the
// page's localStorage.
//
// A window sized to its content (FitContent) is sized by WindowAction
// Fit, keeping its top left (it opens at the first one's size); the tab
// can't be. A panel (Panel: a launcher's, a palette's) is a frameless
// panel over the others, centered across the app's area a third of the
// way down; a flyout (Flyout) a frameless one with a popover's look,
// placed against its anchor (place: a rectangle of its parent's content,
// or for a tray icon, which a page has none of, the top right corner).
// Both take the keyboard as they show and close as the user closes them
// on a press outside them and on an Escape their content leaves (nav.js).
// A click-through window lets the pointer through to the page under it;
// the tab can't let it out of the browser.

import { Event, Events, WindowAction, TitleBar } from "./ops.js";
import * as color from "./color.js";

/** Window flags (ops.WindowFlags). */
export const Flag = Object.freeze({
  FixedSize: 1, NoMinimize: 2, NoMaximize: 4, NoClose: 8, AlwaysOnTop: 16, Hidden: 32, SkipTaskbar: 64,
  Centered: 128, Positioned: 256, Transparent: 512, Maximized: 1024, Fullscreen: 2048, Child: 4096, Modal: 8192,
  Panel: 16384, Flyout: 32768, FitContent: 65536, ClickThrough: 131072,
});

/** Window state bits (ops.WindowStateFlags). */
export const State = Object.freeze({ Visible: 1, Focused: 2, Minimized: 4, Maximized: 8, Fullscreen: 16, OnTop: 32, ClickThrough: 64 });

/**
 * center is a w by h rectangle centered across r, its top a third of the
 * way down the room left (as Spotlight's panel shows), within r
 * (internal/flyout's Center).
 */
export function center(w, h, r) {
  w = Math.min(w, r.w); h = Math.min(h, r.h);
  return { x: r.x + (r.w - w) / 2, y: r.y + (r.h - h) / 3, w, h };
}

/**
 * place is the frame of a w by h flyout next to anchor ({x, y, w, h}),
 * gap away, on the side Placement p names, within work (internal/flyout's
 * Place): flipped to the other side where that has no room and the other
 * has more, lined up with the anchor along it (flipped to the other end
 * where that has no room), then slid back into work, then shrunk to it.
 */
export function place(anchor, w, h, p, gap, work) {
  if (!(p >= 0 && p <= 11)) p = 0;
  let side = Math.floor(p / 3); // 0 bottom, 1 top, 2 right, 3 left
  const align = p % 3; // 0 center, 1 start, 2 end
  const r = { x: 0, y: 0, w, h };
  const aRight = anchor.x + anchor.w, aBottom = anchor.y + anchor.h;
  const wRight = work.x + work.w, wBottom = work.y + work.h;
  if (side <= 1) {
    const after = wBottom - (aBottom + gap), before = anchor.y - gap - work.y;
    if (side === 0 && h > after && before > after) side = 1;
    else if (side === 1 && h > before && after > before) side = 0;
    r.y = side === 0 ? aBottom + gap : anchor.y - gap - h;
    r.x = lineUp(anchor.x, anchor.w, w, align, work.x, wRight);
  } else {
    const after = wRight - (aRight + gap), before = anchor.x - gap - work.x;
    if (side === 2 && w > after && before > after) side = 3;
    else if (side === 3 && w > before && after > before) side = 2;
    r.x = side === 2 ? aRight + gap : anchor.x - gap - w;
    r.y = lineUp(anchor.y, anchor.h, h, align, work.y, wBottom);
  }
  r.w = Math.min(r.w, work.w); r.h = Math.min(r.h, work.h);
  r.x = Math.max(Math.min(r.x, wRight - r.w), work.x);
  r.y = Math.max(Math.min(r.y, wBottom - r.h), work.y);
  return r;
}

/**
 * lineUp is where a flyout size long starts along an anchor at pos, n
 * long, lined up as align says; an end with no room flips to the other
 * end where that has room.
 */
function lineUp(pos, n, size, align, lo, hi) {
  if (align === 1) return pos + size > hi && pos + n - size >= lo ? pos + n - size : pos;
  if (align === 2) return pos + n - size < lo && pos + size <= hi ? pos : pos + n - size;
  return pos + (n - size) / 2;
}

/**
 * trayAnchor is where a flyout hanging from a tray icon goes in a page,
 * which has no menu bar: under where an icon at the top right corner of
 * the app's area (w wide) would be.
 */
export function trayAnchor(w) {
  return { x: Math.max(0, w - 40), y: 0, w: 32, h: 0 };
}

const focusable = "input:not([disabled]):not([type=hidden]), textarea:not([disabled]), select:not([disabled]), button:not([disabled]), a[href], [contenteditable]:not([contenteditable=false]), [tabindex]:not([tabindex='-1'])";

/** focusIn gives the keyboard to el's first focusable element, or el itself. */
export function focusIn(el) {
  for (const x of el.querySelectorAll(focusable)) {
    x.focus({ preventScroll: true });
    if (document.activeElement === x) return;
  }
  el.focus({ preventScroll: true });
}

/** transient reports whether a Window's panel is a flyout or a panel: closed by a press outside it and an Escape. */
export function transient(n) {
  return !!(n?.win?.cfg && n.win.cfg.flags & (Flag.Flyout | Flag.Panel));
}

export class Windows {
  constructor(h) {
    this.h = h;
    this.closeAllowed = false;
    const tab = () => { if (this.h.root) this.report(this.h.root); };
    document.addEventListener("visibilitychange", tab);
    window.addEventListener("focus", tab);
    window.addEventListener("blur", tab);
    window.addEventListener("resize", tab);
    document.addEventListener("fullscreenchange", () => {
      for (const n of this.h.nodes.values()) if (n === this.h.root || n.win) this.report(n);
    });
    window.addEventListener("beforeunload", (e) => this.beforeUnload(e));
  }

  /** listen: the core watches n's window (the root's, a Window's). */
  listen(n, e) {
    if (e !== Event.WindowState) return false;
    n.winSent = null;
    this.h.afterCore(() => this.report(n));
    return true;
  }

  /** state is n's window's, as WindowState reports it. */
  state(n) {
    const fs = document.fullscreenElement;
    if (n === this.h.root) {
      const c = this.h.container.getBoundingClientRect();
      let f = 0;
      if (document.visibilityState === "visible") f |= State.Visible;
      if (document.hasFocus()) f |= State.Focused;
      if (fs) f |= State.Fullscreen;
      return { x: window.screenX, y: window.screenY, w: c.width, h: c.height, f };
    }
    const w = n.win;
    if (!w || !n.dialog) return { x: 0, y: 0, w: 0, h: 0, f: 0 };
    let f = 0;
    if (!w.hidden) f |= State.Visible;
    if (!w.hidden && n.dialog.contains(document.activeElement) && document.hasFocus()) f |= State.Focused;
    if (w.minimized) f |= State.Minimized;
    if (w.maximized) f |= State.Maximized;
    if (fs === n.dialog) f |= State.Fullscreen;
    if (w.onTop) f |= State.OnTop;
    if (w.clickThrough) f |= State.ClickThrough;
    return { x: w.x, y: w.y, w: w.w, h: w.h - w.bar, f };
  }

  /** report sends n's window's state if the core listens and it changed. */
  report(n) {
    if (!n || !n.listening(Event.WindowState)) return;
    const s = this.state(n);
    const key = `${s.x},${s.y},${s.w},${s.h},${s.f}`;
    if (n.winSent === key) return;
    n.winSent = key;
    const b = new Uint8Array(17);
    const v = new DataView(b.buffer);
    v.setFloat32(0, s.x, true); v.setFloat32(4, s.y, true); v.setFloat32(8, s.w, true); v.setFloat32(12, s.h, true);
    b[16] = s.f;
    this.h.dispatch(Events.windowState(n.id, b));
  }

  /** action does a WindowAction on n's window. */
  action(n, args) {
    if (args.length < 1) return;
    const a = args[0];
    const v = new DataView(args.buffer, args.byteOffset, args.byteLength);
    const x = args.length >= 9 ? v.getFloat32(1, true) : 0, y = args.length >= 9 ? v.getFloat32(5, true) : 0;
    if (n === this.h.root || !n.win) {
      this.tabAction(a);
    } else {
      this.panelAction(n, a, x, y);
    }
    this.h.afterCore(() => this.report(n));
  }

  tabAction(a) {
    const el = document.documentElement;
    switch (a) {
      case WindowAction.Focus: window.focus(); break;
      case WindowAction.Fullscreen:
        if (!document.fullscreenElement && el.requestFullscreen) el.requestFullscreen().catch(() => {});
        break;
      case WindowAction.ExitFullscreen:
        if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
        break;
      case WindowAction.Close:
        // Past the app's question: the tab closes if a script opened it;
        // otherwise the page can't close it.
        this.closeAllowed = true;
        window.close();
        break;
    }
  }

  panelAction(n, a, x, y) {
    const nav = this.h.nav, w = n.win, d = n.dialog;
    if (!d) return;
    switch (a) {
      case WindowAction.Show:
        if (!w.hidden) break;
        this.setHidden(n, false);
        // A flyout or a panel takes the keyboard as it shows.
        if (transient(n)) this.h.afterCore(() => { if (n.dialog === d && !w.hidden) focusIn(d); });
        break;
      case WindowAction.Hide: this.setHidden(n, true); break;
      case WindowAction.Focus:
        this.setHidden(n, false);
        w.minimized = false;
        nav.raiseWindow(n);
        focusIn(d);
        break;
      case WindowAction.Minimize: if (!w.flyout) w.minimized = true; break;
      case WindowAction.Maximize: if (!w.flyout) this.maximize(n, true); break;
      case WindowAction.Restore:
        if (w.minimized) w.minimized = false;
        else this.maximize(n, false);
        break;
      case WindowAction.Fullscreen: if (d.requestFullscreen) d.requestFullscreen().catch(() => {}); break;
      case WindowAction.ExitFullscreen: if (document.fullscreenElement === d) document.exitFullscreen().catch(() => {}); break;
      case WindowAction.Center: {
        const c = this.h.container.getBoundingClientRect();
        if (w.cfg?.flags & Flag.Panel) {
          ({ x: w.x, y: w.y } = center(w.w, w.h, { x: 0, y: 0, w: c.width, h: c.height }));
          break;
        }
        w.x = Math.max(0, (c.width - w.w) / 2);
        w.y = Math.max(0, (c.height - w.h) / 2);
        break;
      }
      case WindowAction.Size:
        if (w.flyout) { w.want = { w: x, h: y }; break; }
        w.w = this.clamp(x, w.minWidth || 200, w.maxWidth);
        w.h = this.clamp(y, w.minHeight || 120, w.maxHeight) + w.bar;
        break;
      case WindowAction.Fit: this.fit(n, x, y); break;
      case WindowAction.Position: w.x = x; w.y = y; break;
      case WindowAction.AlwaysOnTop: w.onTop = true; break;
      case WindowAction.NotAlwaysOnTop: w.onTop = false; break;
      case WindowAction.ClickThrough: this.setClickThrough(n, true); break;
      case WindowAction.NotClickThrough: this.setClickThrough(n, false); break;
    }
    nav.sizeDialog(n);
    this.save(n);
  }

  /**
   * fit sizes a panel to its content's size (WindowFitContent): a flyout
   * is placed again at it; another keeps its top left, but at the first,
   * before the page draws it, it's placed as it opens at that size.
   */
  fit(n, width, height) {
    const w = n.win;
    if (w.flyout) { w.want = { w: width, h: height }; return; }
    const c = this.h.container.getBoundingClientRect();
    w.w = Math.max(0, Math.min(width, c.width - 16));
    w.h = Math.max(0, Math.min(height + w.bar, c.height - 16));
    if (!w.fitted && w.autoPlaced) this.h.nav.placeNew(n);
    w.fitted = true;
  }

  /** setClickThrough lets the pointer through a panel to the page under it, or stops it. */
  setClickThrough(n, on) {
    n.win.clickThrough = on;
    n.dialog?.classList.toggle("asuka-window-click-through", on);
  }

  clamp(v, least, most) {
    v = Math.max(v, least);
    return most > 0 ? Math.min(v, most) : v;
  }

  setHidden(n, hidden) {
    const w = n.win;
    if (w.hidden === hidden) return;
    w.hidden = hidden;
    n.dialog.hidden = hidden;
    if (n.backdrop) n.backdrop.hidden = hidden;
    if (!hidden) this.h.nav.raiseWindow(n);
  }

  maximize(n, on) {
    const w = n.win;
    if (w.maximized === on) return;
    if (on) {
      w.normal = { x: w.x, y: w.y, w: w.w, h: w.h };
      w.maximized = true;
    } else {
      w.maximized = false;
      if (w.normal) Object.assign(w, w.normal);
    }
  }

  // --- Remembering where a panel was (its state key) ---

  storageKey(n) {
    const k = n.win?.cfg?.stateKey;
    return k ? `${this.h.prefix ? this.h.prefix() : "asuka:"}window:${k}` : "";
  }

  /** restore puts a panel where it was left; it reports whether it was. */
  restore(n) {
    const key = this.storageKey(n);
    if (!key) return false;
    try {
      const s = JSON.parse(localStorage.getItem(key) || "null");
      if (!s || !(s.w > 0) || !(s.h > 0)) return false;
      const w = n.win;
      w.x = s.x; w.y = s.y; w.w = s.w; w.h = s.h;
      if (s.maximized) this.maximize(n, true);
      return true;
    } catch { return false; }
  }

  save(n) {
    const key = this.storageKey(n), w = n.win;
    if (!key || !w) return;
    const r = w.maximized && w.normal ? w.normal : w;
    try { localStorage.setItem(key, JSON.stringify({ x: r.x, y: r.y, w: r.w, h: r.h, maximized: !!w.maximized })); } catch {}
  }

  /** userClose: the panel's close button. */
  userClose(n) {
    if (n.listening(Event.CloseRequest)) this.h.dispatch(Events.closeRequest(n.id));
    else this.h.emit(n, Event.Dismissed, Events.dismissed);
  }

  /** beforeUnload asks the app, if it wants to be, before the page goes. */
  beforeUnload(e) {
    const r = this.h.root;
    if (!r || !r.listening(Event.CloseRequest) || this.closeAllowed) return;
    // The core answers at once: WindowAction Close if the app lets it.
    this.h.dispatch(Events.closeRequest(r.id));
    if (this.closeAllowed) return;
    e.preventDefault();
    e.returnValue = "";
  }

  /** background is a panel's own background color, if its options give one. */
  background(n) {
    const c = n.win?.cfg;
    if (!c) return "";
    if (c.flags & Flag.Transparent) return "transparent";
    return c.background ? color.css(this.h, c.background) : "";
  }
}

export { TitleBar };

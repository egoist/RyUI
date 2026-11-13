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

import { Event, Events, WindowAction, TitleBar } from "./ops.js";
import * as color from "./color.js";

/** Window flags (ops.WindowFlags). */
export const Flag = Object.freeze({
  FixedSize: 1, NoMinimize: 2, NoMaximize: 4, NoClose: 8, AlwaysOnTop: 16, Hidden: 32, SkipTaskbar: 64,
  Centered: 128, Positioned: 256, Transparent: 512, Maximized: 1024, Fullscreen: 2048, Child: 4096, Modal: 8192,
});

/** Window state bits (ops.WindowStateFlags). */
const State = Object.freeze({ Visible: 1, Focused: 2, Minimized: 4, Maximized: 8, Fullscreen: 16, OnTop: 32 });

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
      case WindowAction.Show: this.setHidden(n, false); break;
      case WindowAction.Hide: this.setHidden(n, true); break;
      case WindowAction.Focus:
        this.setHidden(n, false);
        w.minimized = false;
        nav.raiseWindow(n);
        (d.querySelector("input, textarea, button, [tabindex]") || d).focus({ preventScroll: true });
        break;
      case WindowAction.Minimize: w.minimized = true; break;
      case WindowAction.Maximize: this.maximize(n, true); break;
      case WindowAction.Restore:
        if (w.minimized) w.minimized = false;
        else this.maximize(n, false);
        break;
      case WindowAction.Fullscreen: if (d.requestFullscreen) d.requestFullscreen().catch(() => {}); break;
      case WindowAction.ExitFullscreen: if (document.fullscreenElement === d) document.exitFullscreen().catch(() => {}); break;
      case WindowAction.Center: {
        const c = this.h.container.getBoundingClientRect();
        w.x = Math.max(0, (c.width - w.w) / 2);
        w.y = Math.max(0, (c.height - w.h) / 2);
        break;
      }
      case WindowAction.Size:
        w.w = this.clamp(x, w.minWidth || 200, w.maxWidth);
        w.h = this.clamp(y, w.minHeight || 120, w.maxHeight) + w.bar;
        break;
      case WindowAction.Position: w.x = x; w.y = y; break;
      case WindowAction.AlwaysOnTop: w.onTop = true; break;
      case WindowAction.NotAlwaysOnTop: w.onTop = false; break;
    }
    nav.sizeDialog(n);
    this.save(n);
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

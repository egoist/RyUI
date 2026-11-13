// Navigation in a browser:
//
//   - A Stack shows its top Screen under a header bar of its own (as the
//     Windows host's): a back button while there's a screen to go back
//     to, the screen's title, its bar buttons, its search field. Pushes
//     and pops slide (they cross-fade with reduced motion).
//   - Tabs are a bar of tabs at the bottom, the selected page above it.
//   - A Sheet is a modal <dialog> (the top layer: what's under it is
//     inert): a bottom sheet on a narrow window, a centered card on a
//     wide one. Escape, a click outside or a swipe down dismisses it.
//   - An Alert is a modal <dialog> too; an action sheet's buttons a list.
//   - A Popover is a popover element next to what it points at.
//   - A SplitView is a sidebar beside its detail Stack; on a narrow
//     window, a drawer the detail's header opens.
//   - A Window is a panel in the page the user can move and resize
//     (window.js: what a WindowHandle does to it, and its options). A
//     flyout and a panel are frameless ones over the others, closed as
//     the user closes them on a press outside them (but in a window of
//     theirs, a modal one over a flyout) and on an Escape their content
//     leaves.
//
// Going back is the browser's: its back button (a history entry stands
// for the app's back while there's somewhere to go back to), Escape,
// Alt+Left (⌘[ on a Mac) and a swipe from the leading edge on touch
// screens: it closes the newest dialog, or pops the innermost stack.
//
// Screens, sheets, popovers and windows are sized by the host, which
// reports each one's Environment.

import { Kind, KindProp as P, Event, Events, Command, Reader, TitleBar, environment as newEnv } from "./ops.js";
import { Flag, place, center, trayAnchor, focusIn, transient } from "./window.js";
import { own } from "./views.js";
import { decodeActions } from "./host.js";
import { symbolSVG } from "./media.js";
import * as text from "./text.js";

const headerHeight = 52;
const searchHeight = 48;
const tabBarHeight = 56;
const sidebarWidth = 280;

function div(cls) {
  const d = document.createElement("div");
  if (cls) d.className = cls;
  return d;
}

function iconButton(symbol, label, rtl) {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "asuka-bar-button asuka-control";
  b.setAttribute("aria-label", label);
  const s = symbolSVG(symbol, rtl);
  if (s) b.appendChild(s);
  return b;
}

export class Navigation {
  constructor(h) {
    this.h = h;
    this.dialogs = []; // shown sheets, alerts, popovers and windows, oldest first
    this.guard = false; // a history entry stands for the app's back
    this.words = {};
    addEventListener("popstate", () => this.popstate());
    addEventListener("keydown", (e) => this.key(e));
    addEventListener("pointerdown", (e) => this.pressOutside(e), true);
    this.edgeSwipe();
  }

  word(key) {
    const loc = this.h.locale || navigator.language;
    let w = this.words[loc];
    if (!w) {
      try { w = this.words[loc] = JSON.parse(this.h.core.words(loc)); } catch { w = { back: "Back", ok: "OK", search: "Search", increase: "Increase", decrease: "Decrease" }; }
    }
    return w[key] || key;
  }

  rootChanged() { this.updateGuard(); }

  directionChanged() {
    for (const n of this.h.nodes.values()) if (n.kind === Kind.Stack && n.stack) this.updateHeader(n);
  }

  /** layout sizes the host-sized views again (the window or a stack resized). */
  layout() {
    for (const n of this.h.nodes.values()) {
      if (n.kind === Kind.Stack || n.kind === Kind.Tabs || n.kind === Kind.Split) this.layoutScreens(n);
      else if ((n.kind === Kind.Sheet || n.kind === Kind.Window) && n.shown) this.sizeDialog(n);
      else if (n.kind === Kind.Alert && n.shown) this.placeOnWindow(n);
      else if (n.kind === Kind.Popover && n.shown) this.placePopover(n);
    }
  }

  // --- Screens' environments ---

  /** report sends a screen's, sheet's or popover's environment if it changed. */
  report(n, w, h, el, top = 0) {
    if (w <= 0 || h <= 0) return;
    const e = this.h.env();
    e.width = w;
    e.height = h;
    // The safe area it overlaps: the root's insets less how far it is from each edge.
    const c = this.h.container.getBoundingClientRect();
    const r = (el || n.el).getBoundingClientRect();
    const dt = r.top - c.top, dl = r.left - c.left, db = c.bottom - (r.top + h), dr = c.right - (r.left + w);
    e.safeTop = Math.max(0, e.safeTop - dt);
    e.safeLeft = Math.max(0, e.safeLeft - dl);
    e.safeBottom = Math.max(0, e.safeBottom - db);
    e.safeRight = Math.max(0, e.safeRight - dr);
    if (e.keyboard) e.keyboard = Math.max(0, e.keyboard - db);
    e.safeTop = Math.max(e.safeTop, top); // a Window's hidden title bar
    const key = JSON.stringify(e);
    if (n.envKey === key) return;
    n.envKey = key;
    this.h.dispatch(Events.environment(n.id, e));
  }

  layoutScreens(n) {
    if (!n.placed) return;
    const [, , w, h] = n.frame;
    if (n.kind === Kind.Stack) {
      const bar = this.barHeight(n);
      n.stack.bar.style.height = bar + "px";
      n.stack.bar.hidden = bar === 0;
      for (const s of n.children) this.placeScreen(s, 0, bar, w, h - bar);
    } else if (n.kind === Kind.Tabs) {
      const bar = tabBarHeight + this.h.safeArea()[2] * (this.touchesBottom(n) ? 1 : 0);
      n.tabs.bar.style.height = bar + "px";
      for (const s of n.children) this.placeScreen(s, 0, 0, w, h - bar);
    } else if (n.kind === Kind.Split) {
      this.layoutSplit(n);
    }
  }

  touchesBottom(n) {
    const c = this.h.container.getBoundingClientRect(), r = n.el.getBoundingClientRect();
    return Math.abs(c.bottom - r.bottom) < 1;
  }

  touchesTop(n) {
    const c = this.h.container.getBoundingClientRect(), r = n.el.getBoundingClientRect();
    return Math.abs(r.top - c.top) < 1;
  }

  placeScreen(s, x, y, w, h) {
    if (s.leaving) return;
    const st = s.el.style;
    st.left = x + "px";
    st.top = y + "px";
    st.width = w + "px";
    st.height = Math.max(h, 0) + "px";
    s.frame = [x, y, w, h];
    this.report(s, w, h);
  }

  // --- Stack ---

  barHeight(stack) {
    const top = this.topScreen(stack);
    if (!top) return 0;
    const shown = top.title || top.actions?.length || top.search != null || this.live(stack) > 1 || this.drawerButton(stack);
    if (!shown) return 0;
    const safe = this.touchesTop(stack) ? this.h.safeArea()[0] : 0;
    return headerHeight + safe + (top.search != null && !top.searchButton ? searchHeight : 0);
  }

  live(stack) { return stack.children.filter((s) => !s.leaving).length; }

  topScreen(stack) {
    for (let i = stack.children.length - 1; i >= 0; i--) if (!stack.children[i].leaving) return stack.children[i];
    return null;
  }

  updateHeader(stack) {
    const s = stack.stack;
    const top = this.topScreen(stack);
    const bar = s.bar;
    const rtl = getComputedStyle(stack.el).direction === "rtl";
    bar.style.paddingTop = (this.touchesTop(stack) ? this.h.safeArea()[0] : 0) + "px";
    s.leading.textContent = "";
    s.trailing.textContent = "";
    s.title.textContent = top?.title || "";
    if (top && this.live(stack) > 1) {
      const b = iconButton("chevron.backward", this.word("back"), rtl);
      b.classList.add("asuka-back");
      b.addEventListener("click", () => this.goBack(stack));
      s.leading.appendChild(b);
    } else if (this.drawerButton(stack)) {
      const b = iconButton("sidebar.left", stack.parent.title || "Sidebar", rtl);
      b.addEventListener("click", () => this.openDrawer(stack.parent));
      s.leading.appendChild(b);
    }
    (top?.actions || []).forEach((a, i) => {
      const b = a.icon ? iconButton(a.icon, a.title, rtl) : document.createElement("button");
      if (!a.icon) {
        b.type = "button";
        b.className = "asuka-bar-button asuka-bar-text asuka-control";
        b.textContent = a.title;
      }
      if (a.role === 2) b.classList.add("asuka-destructive");
      b.addEventListener("click", () => this.h.emit(top, Event.Action, (id) => Events.action(id, i)));
      s.trailing.appendChild(b);
    });
    s.search.hidden = top?.search == null || top.searchButton;
    if (top && top.search != null) {
      s.searchInput.placeholder = top.search || this.word("search");
      if (s.searchInput.value !== (top.searchText || "")) s.searchInput.value = top.searchText || "";
    }
    if (top?.barColor) bar.style.backgroundColor = this.h.css(top.barColor);
    else bar.style.backgroundColor = "";
    this.layoutScreens(stack);
    this.updateTitle();
    this.updateGuard();
  }

  /** updateTitle makes the page's title the innermost shown stack's top screen's, or the window's. */
  updateTitle() {
    let title = "";
    const visit = (n) => {
      if (!n || (n.el && n.el.closest(".asuka-hidden-page"))) return;
      if (n.kind === Kind.Stack) {
        const t = this.topScreen(n);
        if (t?.title) title = t.title;
      }
      if (n.kind === Kind.Tabs) {
        visit(n.children[n.tabs.selected]);
        return;
      }
      for (const c of n.children) visit(c);
    };
    visit(this.h.root);
    if (!this.baseTitle) this.baseTitle = document.title;
    const app = this.h.windowTitle || this.h.options.title || this.baseTitle;
    document.title = title && title !== app ? `${title} · ${app}` : app || title;
  }

  /** slide animates a push (in comes from the trailing edge over out) or a pop (out leaves to it). */
  slide(stack, inS, outS, push, done) {
    const rtl = getComputedStyle(stack.el).direction === "rtl";
    const w = stack.frame[2] || 1;
    const away = rtl ? -w : w;
    const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const opts = { duration: reduce ? 150 : 350, easing: "cubic-bezier(0.2, 0.8, 0.2, 1)" };
    const finish = () => done?.();
    if (!stack.el.isConnected) { finish(); return; }
    if (reduce) {
      (push ? inS : outS).el.animate([{ opacity: push ? 0 : 1 }, { opacity: push ? 1 : 0 }], opts).onfinish = finish;
      return;
    }
    if (push) {
      inS.el.animate([{ transform: `translateX(${away}px)` }, { transform: "none" }], opts).onfinish = finish;
      outS?.el.animate([{ transform: "none" }, { transform: `translateX(${-away / 3}px)` }], opts);
    } else {
      outS.el.animate([{ transform: "none" }, { transform: `translateX(${away}px)` }], opts).onfinish = finish;
      inS?.el.animate([{ transform: `translateX(${-away / 3}px)` }, { transform: "none" }], opts);
    }
  }

  /** goBack is the user's back on a stack: the top screen asks the core (ui.OnBack), or slides out, then the core hears it was popped. */
  goBack(stack) {
    const top = this.topScreen(stack);
    if (!top || this.live(stack) < 2) return false;
    if (top.interceptBack) {
      this.h.dispatch(Events.back(top.id));
      return true;
    }
    if (!top.backGesture && top.backGesture !== undefined) {
      // No interactive back: the back button still goes back.
    }
    this.popScreen(stack, top);
    this.h.emit(stack, Event.Popped, (id) => Events.popped(id, top.id));
    return true;
  }

  popScreen(stack, top) {
    top.leaving = true;
    top.el.classList.add("asuka-exiting");
    top.el.setAttribute("inert", "");
    const below = this.topScreen(stack);
    if (below) this.showScreen(below, true);
    this.updateHeader(stack);
    this.slide(stack, below, top, false, () => this.release(top));
  }

  showScreen(s, on) {
    s.el.classList.toggle("asuka-covered", !on);
    if (on) s.el.removeAttribute("inert"); else s.el.setAttribute("inert", "");
  }

  release(s) {
    s.el.remove();
    s.released = true;
  }

  // --- Sheets, alerts, popovers, windows ---

  present(n) {
    switch (n.kind) {
      case Kind.Sheet: this.presentSheet(n); break;
      case Kind.Alert: this.presentAlert(n); break;
      case Kind.Popover: this.h.afterCore(() => { if (!n.dying) this.presentPopover(n); }); break;
      case Kind.Window: this.presentWindow(n); break;
    }
  }

  wide() { return this.h.container.getBoundingClientRect().width >= 600; }

  presentSheet(n) {
    if (n.shown) return;
    const d = document.createElement("dialog");
    d.className = "asuka-sheet-dialog";
    this.h.container.appendChild(d);
    d.appendChild(n.el);
    if (n.grabber) {
      const g = div("asuka-grabber");
      d.appendChild(g);
      this.swipeDown(n, g);
    }
    n.dialog = d;
    n.shown = true;
    this.dialogs.push(n);
    d.addEventListener("cancel", (e) => { e.preventDefault(); this.dismiss(n); });
    d.addEventListener("click", (e) => { if (e.target === d) this.dismiss(n); });
    d.showModal();
    this.sizeDialog(n);
    const wide = this.wide();
    if (!matchMedia("(prefers-reduced-motion: reduce)").matches) {
      d.animate(wide ? [{ opacity: 0, transform: "scale(0.96)" }, { opacity: 1, transform: "none" }] : [{ transform: "translateY(100%)" }, { transform: "none" }], { duration: 300, easing: "cubic-bezier(0.2, 0.8, 0.2, 1)" });
    }
    this.updateGuard();
  }

  sizeDialog(n) {
    const c = this.h.container.getBoundingClientRect();
    const d = n.dialog;
    if (!d) return;
    let w, h;
    if (n.kind === Kind.Window) {
      const wn = n.win;
      if (wn.flyout) this.placeFlyout(n, c);
      else if (wn.maximized) { wn.x = 0; wn.y = 0; wn.w = c.width; wn.h = c.height; }
      w = wn.w; h = wn.minimized ? Math.max(wn.bar, 36) : wn.h;
      d.style.left = wn.x + "px";
      d.style.top = wn.y + "px";
      const z = String(20 + this.windowLayer(n));
      d.style.zIndex = z;
      if (n.backdrop) n.backdrop.style.zIndex = z;
      d.classList.toggle("asuka-window-top", !!wn.onTop);
      d.classList.toggle("asuka-window-minimized", !!wn.minimized);
      d.classList.toggle("asuka-window-maximized", !!wn.maximized);
    } else if ((n.kind === Kind.Sheet) && this.panelOf(n)) {
      // A sheet of a Window's panel: a card over the panel's content,
      // as a desktop's sheet over its window.
      const a = this.panelArea(this.panelOf(n), c);
      d.classList.add("asuka-wide", "asuka-on-window");
      w = Math.min(Math.max(a.w - 40, 280), c.width - 16);
      h = Math.min(n.detents === 1 ? Math.max(a.h / 2, 200) : Math.max(a.h - 40, 200), c.height - 16);
      this.placeOver(d, a, w, h, c);
    } else {
      const wide = this.wide();
      d.classList.remove("asuka-on-window");
      d.style.left = d.style.top = "";
      d.classList.toggle("asuka-wide", wide);
      // It opens at its smallest detent, as iOS's sheets do; the keyboard expands it.
      const medium = (n.detents & 1) !== 0 && !n.expanded;
      if (wide) {
        w = Math.min(c.width - 80, 560);
        // A card over a wide window, as iPad's form sheets: half height only for a medium-only sheet.
        h = Math.min(c.height - 80, n.detents === 1 ? Math.max(c.height / 2, 320) : 720);
      } else {
        w = c.width;
        const top = this.h.safeArea()[0] + 10;
        h = medium ? Math.max(c.height / 2, 300) : c.height - top;
        if (medium && (this.h.env().keyboard || n.el.contains(document.activeElement) && matchMedia("(pointer: coarse)").matches)) {
          n.expanded = true;
          h = c.height - top;
        }
      }
    }
    d.style.width = w + "px";
    d.style.height = h + "px";
    const s = n.el.style;
    const top = n.kind === Kind.Window ? n.win.bar : 0;
    if (n.kind === Kind.Window && n.win.minimized) {
      this.h.windows.report(n);
      return;
    }
    s.left = "0px"; s.top = top + "px"; s.width = w + "px"; s.height = h - top + "px";
    n.frame = [0, top, w, h - top];
    n.placed = true;
    this.report(n, w, h - top, n.el, n.kind === Kind.Window ? n.win.safeTop : 0);
    if (n.kind === Kind.Window) {
      this.h.windows.report(n);
      // Its sheets and alerts go with it.
      for (const x of this.dialogs) {
        if (x.winID !== n.id || x === n) continue;
        if (x.kind === Kind.Sheet) this.sizeDialog(x);
        else if (x.kind === Kind.Alert) this.placeOnWindow(x);
      }
    }
  }

  /**
   * panelOf is the Window node whose panel a sheet or an alert goes on:
   * its Window prop's (the window it's declared in), while that panel
   * shows; null for the main window's (the page), or a panel hidden or
   * minimized.
   */
  panelOf(n) {
    const w = this.h.nodes.get(n.winID);
    if (!w || w.kind !== Kind.Window || !w.shown || !w.dialog || !w.win || w.win.hidden || w.win.minimized) return null;
    return w;
  }

  /** panelArea is a Window panel's content, in the viewport's coordinates (c: the container's rectangle). */
  panelArea(w, c) {
    const wn = w.win;
    return { x: c.left + wn.x, y: c.top + wn.y + wn.bar, w: wn.w, h: wn.h - wn.bar };
  }

  /** placeOver puts dialog d, w by h, centered over area a, within the container's rectangle c. */
  placeOver(d, a, w, h, c) {
    const x = Math.min(Math.max(a.x + (a.w - w) / 2, c.left + 8), Math.max(c.left + 8, c.right - w - 8));
    const y = Math.min(Math.max(a.y + (a.h - h) / 2, c.top + 8), Math.max(c.top + 8, c.bottom - h - 8));
    d.style.left = x + "px";
    d.style.top = y + "px";
  }

  /** placeOnWindow puts an alert over its Window's panel, or where the page puts it. */
  placeOnWindow(n) {
    const d = n.dialog;
    if (!d) return;
    const w = this.panelOf(n);
    d.classList.toggle("asuka-on-window", !!w);
    if (!w) { d.style.left = d.style.top = ""; return; }
    const c = this.h.container.getBoundingClientRect();
    const r = d.getBoundingClientRect();
    this.placeOver(d, this.panelArea(w, c), r.width, r.height, c);
  }

  /** dismiss is the user's: the sheet's ui.OnBack asks the core, or the core hears it was dismissed. */
  dismiss(n) {
    if (n.interceptBack) {
      this.h.dispatch(Events.back(n.id));
      return;
    }
    this.h.emit(n, Event.Dismissed, Events.dismissed);
  }

  closeDialog(n) {
    const d = n.dialog;
    if (!d) return;
    n.shown = false;
    n.dialog = null;
    this.dialogs = this.dialogs.filter((x) => x !== n);
    d.classList.add("asuka-exiting");
    const done = () => { d.close(); d.remove(); };
    if (matchMedia("(prefers-reduced-motion: reduce)").matches || !d.isConnected) done();
    else {
      const wide = d.classList.contains("asuka-wide") || n.kind !== Kind.Sheet;
      d.animate(wide ? [{ opacity: 1 }, { opacity: 0 }] : [{ transform: "none" }, { transform: "translateY(100%)" }], { duration: 220, easing: "ease-in", fill: "forwards" }).onfinish = done;
    }
    this.updateGuard();
  }

  swipeDown(n, handle) {
    let y0 = null;
    handle.addEventListener("pointerdown", (e) => { y0 = e.clientY; handle.setPointerCapture(e.pointerId); });
    handle.addEventListener("pointermove", (e) => {
      if (y0 == null || !n.dialog) return;
      n.dialog.style.translate = `0 ${Math.max(0, e.clientY - y0)}px`;
    });
    handle.addEventListener("pointerup", (e) => {
      if (y0 == null) return;
      const dy = e.clientY - y0;
      y0 = null;
      if (n.dialog) n.dialog.style.translate = "";
      if (dy > 80) this.dismiss(n);
      else if (dy < -60 && n.detents & 2 && !n.expanded) {
        // Pulled up from medium: large.
        n.expanded = true;
        this.sizeDialog(n);
      }
    });
  }

  presentAlert(n) {
    if (n.shown) return;
    const d = document.createElement("dialog");
    const sheet = n.alertStyle === 1;
    d.className = sheet ? "asuka-alert asuka-action-sheet" : "asuka-alert";
    d.setAttribute("role", "alertdialog");
    const body = div("asuka-alert-body");
    if (n.title) { const t = div("asuka-alert-title"); t.textContent = n.title; body.appendChild(t); d.setAttribute("aria-label", n.title); }
    if (n.message) { const m = div("asuka-alert-message"); m.textContent = n.message; body.appendChild(m); }
    d.appendChild(body);
    const buttons = div("asuka-alert-buttons");
    const actions = n.actions?.length ? n.actions : [{ role: 0, title: this.word("ok"), icon: "", ok: true }];
    // The cancel button goes last in a sheet's list and first in a row of two, as platforms place it.
    const order = actions.map((a, i) => i);
    order.sort((a, b) => (actions[a].role === 1) - (actions[b].role === 1));
    if (!sheet && actions.length <= 2) order.sort((a, b) => (actions[b].role === 1) - (actions[a].role === 1));
    buttons.classList.toggle("asuka-stacked", sheet || actions.length > 2);
    for (const i of order) {
      const a = actions[i];
      const b = document.createElement("button");
      b.type = "button";
      b.className = "asuka-control";
      b.textContent = a.title;
      if (a.role === 1) b.classList.add("asuka-cancel");
      if (a.role === 2) b.classList.add("asuka-destructive");
      b.addEventListener("click", () => {
        if (!n.shown) return;
        this.closeDialog(n);
        if (a.ok) this.h.emit(n, Event.Dismissed, Events.dismissed);
        else this.h.emit(n, Event.Action, (id) => Events.action(id, i));
      });
      buttons.appendChild(b);
    }
    d.appendChild(buttons);
    this.h.container.appendChild(d);
    n.dialog = d;
    n.shown = true;
    this.dialogs.push(n);
    const cancelIdx = actions.findIndex((a) => a.role === 1);
    const cancel = () => {
      if (!n.shown) return;
      this.closeDialog(n);
      if (cancelIdx >= 0 && !actions[cancelIdx].ok) this.h.emit(n, Event.Action, (id) => Events.action(id, cancelIdx));
      else this.h.emit(n, Event.Dismissed, Events.dismissed);
    };
    d.addEventListener("cancel", (e) => { e.preventDefault(); cancel(); });
    d.addEventListener("click", (e) => { if (e.target === d && sheet) cancel(); });
    d.showModal();
    this.placeOnWindow(n);
    const def = buttons.querySelector("button:not(.asuka-cancel)") || buttons.querySelector("button");
    def?.focus();
    if (!matchMedia("(prefers-reduced-motion: reduce)").matches) {
      d.animate([{ opacity: 0, transform: sheet && !this.wide() ? "translateY(40px)" : "scale(1.08)" }, { opacity: 1, transform: "none" }], { duration: 200, easing: "ease-out" });
    }
    this.updateGuard();
  }

  presentPopover(n) {
    if (n.shown) return;
    const p = div("asuka-popover");
    p.popover = "auto";
    p.appendChild(n.el);
    this.h.container.appendChild(p);
    n.dialog = p;
    n.shown = true;
    this.dialogs.push(n);
    p.addEventListener("toggle", (e) => {
      if (e.newState === "closed" && n.shown) {
        // A click outside or Escape: light dismissed.
        n.shown = false;
        this.dialogs = this.dialogs.filter((x) => x !== n);
        if (n.interceptBack) {
          this.h.dispatch(Events.back(n.id));
          p.showPopover();
          n.shown = true;
          this.dialogs.push(n);
          return;
        }
        this.h.emit(n, Event.Dismissed, Events.dismissed);
      }
    });
    p.showPopover();
    this.placePopover(n);
    this.updateGuard();
  }

  placePopover(n) {
    const p = n.dialog;
    if (!p) return;
    const w = n.pop.w, h = n.pop.h;
    const c = this.h.container.getBoundingClientRect();
    const a = this.h.nodes.get(n.pop.anchor);
    const r = a?.el?.getBoundingClientRect() || { left: c.left + c.width / 2, right: c.left + c.width / 2, top: c.top + c.height / 3, bottom: c.top + c.height / 3, width: 0 };
    let x = r.left + r.width / 2 - w / 2;
    x = Math.min(Math.max(x, c.left + 8), c.right - w - 8);
    let y = r.bottom + 8;
    if (y + h > c.bottom - 8 && r.top - 8 - h >= c.top + 8) y = r.top - 8 - h;
    p.style.left = x + "px";
    p.style.top = y + "px";
    p.style.width = w + "px";
    p.style.height = h + "px";
    const s = n.el.style;
    s.left = "0px"; s.top = "0px"; s.width = w + "px"; s.height = h + "px";
    n.frame = [0, 0, w, h];
    n.placed = true;
    this.report(n, w, h, p);
  }

  presentWindow(n) {
    if (n.shown) return;
    const c = this.h.container.getBoundingClientRect();
    const cfg = n.win.cfg || decodeWindowConfig(new Uint8Array(0));
    n.win.cfg = cfg;
    // A flyout and a panel are frameless: no title bar, not moved.
    const flyout = !!(cfg.flags & Flag.Flyout), frameless = flyout || !!(cfg.flags & Flag.Panel);
    const hiddenBar = !frameless && cfg.titleBar === TitleBar.Hidden;
    const barH = 36, bar = hiddenBar || frameless ? 0 : barH;
    const w = Math.min(n.win.width || (flyout ? 320 : 480), c.width - 16), h = Math.min((n.win.height || (flyout ? 400 : 600)) + bar, c.height - 16);
    n.win = { ...n.win, w, h, x: 0, y: 0, bar, safeTop: hiddenBar ? barH : 0, maxWidth: cfg.maxWidth, maxHeight: cfg.maxHeight,
      hidden: false, minimized: false, maximized: false, onTop: !!(cfg.flags & Flag.AlwaysOnTop),
      cascade: this.dialogs.filter((x) => x.kind === Kind.Window && !transient(x)).length,
      flyout: flyout ? n.win.flyout || decodeFlyout(new Uint8Array(0)) : null, want: flyout ? { w: n.win.width || 320, h: n.win.height || 400 } : null,
      autoPlaced: false, fitted: false, clickThrough: !!(cfg.flags & Flag.ClickThrough) };
    const d = div("asuka-window");
    d.setAttribute("role", "dialog");
    d.tabIndex = -1;
    d.classList.toggle("asuka-flyout", flyout);
    d.classList.toggle("asuka-window-panel", frameless && !flyout);
    d.classList.toggle("asuka-window-click-through", n.win.clickThrough);
    let title = null, name = null;
    if (!frameless) {
      title = div("asuka-window-bar");
      title.classList.toggle("asuka-window-bar-hidden", hiddenBar);
      name = div("asuka-window-title");
      name.textContent = windowTitle(n);
      title.append(name);
      if (!(cfg.flags & Flag.NoClose)) {
        const close = iconButton("xmark", "Close", false);
        close.addEventListener("click", () => this.h.windows.userClose(n));
        title.append(close);
      }
      d.append(title);
    } else if (n.title) d.setAttribute("aria-label", n.title);
    d.append(n.el);
    const bg = this.h.windows.background(n);
    if (bg) d.style.background = bg;
    if (cfg.flags & Flag.Modal) {
      // Modal: the page under it takes no input until it closes.
      n.backdrop = div("asuka-window-backdrop");
      this.h.container.appendChild(n.backdrop);
    }
    const resizable = !(cfg.flags & Flag.FixedSize) && !flyout;
    if (resizable) {
      const grip = div("asuka-window-grip");
      d.appendChild(grip);
      this.drag(grip, (dx, dy) => {
        const wn = n.win;
        if (wn.maximized) return;
        wn.w = this.h.windows.clamp(wn.w + dx, wn.minWidth || 200, wn.maxWidth);
        wn.h = this.h.windows.clamp(wn.h - wn.bar + dy, wn.minHeight || 120, wn.maxHeight) + wn.bar;
        this.sizeDialog(n);
      }, () => this.h.windows.save(n));
    }
    this.h.container.appendChild(d);
    n.dialog = d;
    n.winTitle = name;
    n.shown = true;
    this.dialogs.push(n);
    if (title) {
      this.drag(title, (dx, dy) => {
        const wn = n.win;
        if (wn.maximized) return;
        wn.x += dx; wn.y += dy;
        this.sizeDialog(n);
      }, () => this.h.windows.save(n));
    }
    if (title && resizable && !(cfg.flags & Flag.NoMaximize)) {
      title.addEventListener("dblclick", (e) => {
        if (e.target.closest("button")) return;
        this.h.windows.maximize(n, !n.win.maximized);
        this.sizeDialog(n);
        this.h.windows.save(n);
      });
    }
    // The window clicked comes to the front; its keyboard focus is reported.
    d.addEventListener("pointerdown", () => this.raiseWindow(n));
    d.addEventListener("focusin", () => this.h.windows.report(n));
    d.addEventListener("focusout", () => queueMicrotask(() => this.h.windows.report(n)));
    const restored = !flyout && this.h.windows.restore(n);
    if (!restored) {
      // Placed where it opens; a window sized to its content again at
      // its first size, which comes before the page draws it.
      n.win.autoPlaced = !flyout && !(cfg.flags & Flag.Positioned);
      this.placeNew(n);
      if (cfg.flags & Flag.Maximized && !frameless) this.h.windows.maximize(n, true);
    }
    if (cfg.flags & Flag.Hidden) this.h.windows.setHidden(n, true);
    else this.raiseWindow(n);
    this.sizeDialog(n);
    this.updateGuard();
    // A flyout or a panel takes the keyboard as it shows, once its content is in.
    if (frameless && !n.win.hidden) this.h.afterCore(() => { if (n.dialog === d && !n.win.hidden) focusIn(d); });
  }

  /**
   * placeNew places a Window's panel where one opens: where its options
   * say, centered a third of the way down for a panel, else centered,
   * each one opened after another a little lower and to the right.
   */
  placeNew(n) {
    const wn = n.win, f = wn.cfg?.flags || 0;
    if (wn.flyout) return;
    if (f & Flag.Positioned) { wn.x = wn.cfg.x; wn.y = wn.cfg.y; return; }
    const c = this.h.container.getBoundingClientRect();
    const w = wn.w, h = wn.h;
    if (f & Flag.Panel) {
      ({ x: wn.x, y: wn.y } = center(w, h, { x: 0, y: 0, w: c.width, h: c.height }));
    } else if (f & Flag.Centered) {
      wn.x = Math.max(0, (c.width - w) / 2); wn.y = Math.max(0, (c.height - h) / 2);
    } else {
      wn.x = Math.max(8, (c.width - w) / 2 + wn.cascade * 24); wn.y = Math.max(8, (c.height - h) / 2 + wn.cascade * 24);
    }
  }

  /**
   * placeFlyout places a flyout against its anchor, within the app's
   * area: a rectangle of its parent's content (a Window's, the root's),
   * or the top right corner for a tray icon.
   */
  placeFlyout(n, c) {
    const wn = n.win, f = wn.flyout;
    const work = { x: 8, y: 8, w: Math.max(0, c.width - 16), h: Math.max(0, c.height - 16) };
    let anchor;
    if (f.tray) anchor = trayAnchor(c.width);
    else {
      const p = this.h.nodes.get(wn.cfg?.parent) || this.h.root;
      const r = p?.el?.getBoundingClientRect() || c;
      anchor = { x: r.left - c.left + f.x, y: r.top - c.top + f.y, w: f.width, h: f.height };
    }
    const r = place(anchor, wn.want.w, wn.want.h, f.placement, f.gap, work);
    wn.x = r.x; wn.y = r.y; wn.w = r.w; wn.h = r.h;
  }

  /**
   * windowLayer is how high a Window's panel goes: a flyout's and a
   * panel's over those always on top, over the others; one belonging to
   * another (a child, a modal one, a flyout) at least as high as it.
   */
  windowLayer(n) {
    let l = 0;
    for (let x = n, i = 0; x && x.kind === Kind.Window && x.win && i < 64; x = this.h.nodes.get(x.win.cfg?.parent), i++) {
      l = Math.max(l, transient(x) ? 2 : x.win.onTop ? 1 : 0);
    }
    return l;
  }

  /** belongsTo reports whether Window x is n's, or one of its own's (its Config's Parent). */
  belongsTo(x, n) {
    for (let p = this.h.nodes.get(x.win?.cfg?.parent), i = 0; p && i < 64; p = this.h.nodes.get(p.win?.cfg?.parent), i++) {
      if (p === n) return true;
      if (p.kind !== Kind.Window) return false;
    }
    return false;
  }

  /** raiseWindow brings a Window's panel to the front: under those higher (windowLayer) and its own. */
  raiseWindow(n) {
    const c = this.h.container;
    const tail = [n.backdrop, n.dialog];
    const l = this.windowLayer(n);
    for (const x of this.dialogs) {
      if (x !== n && x.kind === Kind.Window && x.dialog && (this.windowLayer(x) > l || this.belongsTo(x, n))) tail.push(x.backdrop, x.dialog);
    }
    const els = tail.filter(Boolean);
    // Moved only if they aren't where they go already: a press whose
    // element moves in the page gets no click.
    let at = c.lastElementChild;
    let placed = true;
    for (let i = els.length - 1; i >= 0; i--, at = at?.previousElementSibling) {
      if (at !== els[i]) { placed = false; break; }
    }
    if (!placed) for (const el of els) c.appendChild(el);
  }

  drag(handle, move, end) {
    let last = null;
    handle.addEventListener("pointerdown", (e) => {
      if (e.target.closest("button")) return;
      last = [e.clientX, e.clientY];
      handle.setPointerCapture(e.pointerId);
    });
    handle.addEventListener("pointermove", (e) => {
      if (!last) return;
      move(e.clientX - last[0], e.clientY - last[1]);
      last = [e.clientX, e.clientY];
    });
    handle.addEventListener("pointerup", () => {
      if (last && end) end();
      last = null;
    });
  }

  // --- SplitView ---

  drawerButton(stack) {
    const p = stack.parent;
    return p?.kind === Kind.Split && !p.split.beside && this.live(stack) <= 1;
  }

  layoutSplit(n) {
    const [, , w, h] = n.frame;
    const s = n.split;
    const beside = w >= 700;
    if (beside !== s.beside) {
      s.beside = beside;
      n.el.classList.toggle("asuka-split-beside", beside);
      this.closeDrawer(n);
      for (const c of n.children) if (c.kind === Kind.Stack && c.stack) this.updateHeader(c);
    }
    const side = beside ? sidebarWidth : 0;
    s.sidebar.style.width = (beside ? sidebarWidth : Math.min(sidebarWidth + 40, w - 56)) + "px";
    const rtl = getComputedStyle(n.el).direction === "rtl";
    for (const c of n.children) {
      if (!c.el) continue;
      const st = c.el.style;
      st.left = (rtl ? 0 : side) + "px";
      st.top = "0px";
      st.width = w - side + "px";
      st.height = h + "px";
      c.frame = [rtl ? 0 : side, 0, w - side, h];
      c.placed = true;
      if (c.kind === Kind.Stack) this.layoutScreens(c);
    }
  }

  openDrawer(n) {
    n.el.classList.add("asuka-drawer-open");
    n.split.sidebar.removeAttribute("inert");
    this.dialogs.push(n);
    this.updateGuard();
  }

  closeDrawer(n) {
    n.el.classList.remove("asuka-drawer-open");
    const i = this.dialogs.indexOf(n);
    if (i >= 0) this.dialogs.splice(i, 1);
    this.updateGuard();
  }

  // --- Back ---

  /** canGoBack: there's a dialog to close or a stack to pop. */
  canGoBack() {
    if (this.dialogs.length) return true;
    return !!this.innermostStack();
  }

  /** innermostStack is the innermost shown stack that can pop. */
  innermostStack() {
    let found = null;
    const visit = (n) => {
      if (!n) return;
      if (n.kind === Kind.Tabs) { visit(n.children[n.tabs.selected]); return; }
      if (n.kind === Kind.Stack) {
        if (this.live(n) > 1) found = n;
        visit(this.topScreen(n));
        return;
      }
      for (const c of n.children) visit(c);
    };
    const top = this.dialogs[this.dialogs.length - 1];
    visit(top ? top : this.h.root);
    return found;
  }

  /** back is the user's back: the newest dialog closes, or the innermost stack pops. */
  back() {
    const top = this.dialogs[this.dialogs.length - 1];
    const stack = this.innermostStack();
    if (stack) return this.goBack(stack);
    if (top) {
      if (top.kind === Kind.Split) this.closeDrawer(top);
      else if (top.kind === Kind.Alert) top.dialog?.dispatchEvent(new window.Event("cancel"));
      else if (top.kind === Kind.Popover) top.dialog?.hidePopover();
      else if (top.kind === Kind.Window && transient(top)) this.h.windows.userClose(top);
      else this.dismiss(top);
      return true;
    }
    if (this.h.root?.interceptBack) {
      this.h.dispatch(Events.back(this.h.root.id));
      return true;
    }
    return false;
  }

  /** backExit: the app's OnBack let the user's back go on from the root: the page goes back. */
  backExit() {
    this.leaving = true;
    history.back();
  }

  /**
   * transientFor is the flyout or panel an Escape typed at t goes to: the
   * one t is in, or with the keyboard in none of the windows, the newest
   * window if it's one.
   */
  transientFor(t) {
    const el = t?.nodeType === 1 ? t : null;
    for (let i = this.dialogs.length - 1; i >= 0; i--) {
      const x = this.dialogs[i];
      if (x.kind !== Kind.Window || !x.dialog || x.win.hidden) continue;
      if (el && x.dialog.contains(el)) return transient(x) ? x : null;
    }
    if (el?.closest(".asuka-window")) return null;
    const top = this.dialogs[this.dialogs.length - 1];
    return top?.kind === Kind.Window && top.dialog && !top.win.hidden && transient(top) ? top : null;
  }

  /**
   * pressOutside closes, as the user closes them, the flyouts and panels
   * a press lands outside of: not in one of its own flyouts or modal
   * windows, nor over everything (a sheet, an alert, a popover, a menu).
   */
  pressOutside(e) {
    const el = e.target?.nodeType === 1 ? e.target : null;
    if (el?.closest("dialog, [popover]")) return;
    for (let i = this.dialogs.length - 1; i >= 0; i--) {
      const n = this.dialogs[i];
      if (n.kind !== Kind.Window || !n.shown || !n.dialog || n.win.hidden || !transient(n)) continue;
      if (el && this.within(n, el)) continue;
      this.h.windows.userClose(n);
    }
  }

  /** within reports whether el is in Window n's panel, or a modal window or a flyout of its own over it. */
  within(n, el) {
    if (n.dialog.contains(el)) return true;
    for (const x of this.dialogs) {
      if (x === n || x.kind !== Kind.Window || !x.dialog || x.win.hidden) continue;
      if (!(x.win.cfg?.flags & (Flag.Modal | Flag.Flyout)) || !this.belongsTo(x, n)) continue;
      if (x.dialog.contains(el) || x.backdrop?.contains(el)) return true;
    }
    return false;
  }

  key(e) {
    if (e.defaultPrevented) return;
    // An app in an element of a page (not the whole page) takes the keys typed in it.
    if (!this.h.options.fullPage && !this.h.container.contains(e.target)) return;
    const mac = /Mac|iPhone|iPad/.test(navigator.platform);
    const backKey = (e.altKey && e.key === "ArrowLeft" && !mac) || (e.metaKey && e.key === "[" && mac) || e.key === "BrowserBack";
    const escape = e.key === "Escape" && !this.dialogs.some((d) => d.dialog?.tagName === "DIALOG" || d.kind === Kind.Popover);
    // A flyout or a panel with the keyboard closes as the user closes it,
    // from a text field in it too (a launcher's), unless a stack in it
    // goes back.
    const f = escape ? this.transientFor(e.target) : null;
    if (f) {
      e.preventDefault();
      const stack = this.dialogs[this.dialogs.length - 1] === f ? this.innermostStack() : null;
      if (stack) this.goBack(stack);
      else this.h.windows.userClose(f);
      return;
    }
    if (escape && e.target.closest?.("input, textarea")) { e.target.blur(); return; }
    if (backKey || escape) {
      if (this.back()) e.preventDefault();
    }
  }

  /**
   * updateGuard keeps a history entry for the app's back while there's
   * somewhere to go back to, so the browser's back button (and a phone's
   * back gesture) goes back in the app.
   */
  updateGuard() {
    if (!this.h.options.fullPage) return;
    queueMicrotask(() => {
      const want = this.canGoBack() || !!this.h.root?.interceptBack;
      if (want && !this.guard && !this.dropping) {
        history.pushState({ asuka: true }, "");
        this.guard = true;
      } else if (!want && this.guard && history.state?.asuka && !this.dropping) {
        // Once: the guard is gone as this back's popstate comes.
        this.guard = false;
        this.dropping = true;
        history.back();
      }
    });
  }

  popstate() {
    if (this.dropping) {
      this.dropping = false;
      this.updateGuard(); // something to go back to came meanwhile
      return;
    }
    if (this.leaving) return;
    if (!this.guard) return;
    this.guard = false;
    this.h.afterCore(() => {
      this.back();
      this.updateGuard();
    });
  }

  /** edgeSwipe: a touch from the leading edge, moving across, goes back in the innermost stack. */
  edgeSwipe() {
    let start = null;
    const c = this.h.container;
    c.addEventListener("pointerdown", (e) => {
      if (e.pointerType !== "touch") return;
      const r = c.getBoundingClientRect();
      const rtl = this.h.rtl;
      const edge = rtl ? r.right - e.clientX : e.clientX - r.left;
      start = edge < 20 ? { x: e.clientX, y: e.clientY, rtl } : null;
    });
    c.addEventListener("pointerup", (e) => {
      if (!start) return;
      const dx = (e.clientX - start.x) * (start.rtl ? -1 : 1), dy = Math.abs(e.clientY - start.y);
      start = null;
      if (dx > 80 && dy < dx / 2) {
        const stack = this.innermostStack();
        const top = stack && this.topScreen(stack);
        if (top && top.backGesture !== false) this.goBack(stack);
      }
    });
  }

  fieldFocused(n, on) {
    if (on) for (const d of this.dialogs) if (d.kind === Kind.Sheet && d.el.contains(n.el) && !d.expanded) this.h.afterCore(() => this.sizeDialog(d));
    if (n.accessory) {
      const a = this.h.nodes.get(n.accessory);
      if (a) showAccessory(this.h, a, on);
    }
  }
}

function showAccessory(h, a, on) {
  if (!matchMedia("(pointer: coarse)").matches) return; // no on-screen keyboard
  a.el.classList.toggle("asuka-accessory-shown", on);
  if (!a.el.isConnected) h.container.appendChild(a.el);
  const place = () => {
    const vv = window.visualViewport;
    const bottom = vv ? window.innerHeight - (vv.height + vv.offsetTop) : 0;
    a.el.style.bottom = Math.max(0, bottom) + "px";
  };
  place();
  if (on) {
    a.place = place;
    window.visualViewport?.addEventListener("resize", place);
  } else if (a.place) {
    window.visualViewport?.removeEventListener("resize", a.place);
  }
}

// --- The kinds ---

const stack = {
  create(h, n) {
    const el = own(n, div("asuka-stack"));
    const bar = div("asuka-bar");
    const row = div("asuka-bar-row");
    const leading = div("asuka-bar-leading");
    const title = div("asuka-bar-title");
    title.setAttribute("role", "heading");
    title.setAttribute("aria-level", "1");
    const trailing = div("asuka-bar-trailing");
    row.append(leading, title, trailing);
    const search = div("asuka-bar-search");
    const input = document.createElement("input");
    input.type = "search";
    input.className = "asuka-search asuka-control";
    input.enterKeyHint = "search";
    search.appendChild(input);
    search.hidden = true;
    bar.append(row, search);
    bar.hidden = true;
    el.appendChild(bar);
    n.stack = { bar, leading, title, trailing, search, searchInput: input };
    input.addEventListener("input", () => {
      const top = h.nav.topScreen(n);
      if (!top) return;
      top.searchText = input.value;
      h.emit(top, Event.TextChanged, (id) => Events.textChanged(id, input.value));
    });
    input.addEventListener("keydown", (e) => {
      const top = h.nav.topScreen(n);
      if (e.key === "Enter" && !e.isComposing && top) h.emit(top, Event.Submit, Events.submit);
    });
  },
  insert(h, p, c, i) {
    c.leaving = false;
    const top = h.nav.topScreen(p) === c ? null : null;
    const below = p.children.filter((s) => s !== c && !s.leaving);
    const isTop = p.children.indexOf(c) === p.children.length - 1;
    const prevTop = isTop ? below[below.length - 1] : null;
    // Above the screens before it, below the bar.
    let next = null;
    for (let k = i + 1; k < p.children.length; k++) if (p.children[k].el?.parentNode === p.el) { next = p.children[k].el; break; }
    p.el.insertBefore(c.el, next || p.stack.bar);
    h.nav.updateHeader(p);
    if (isTop) {
      if (prevTop) {
        h.nav.showScreen(prevTop, true);
        const instant = p.instant || !p.shownOnce;
        if (!instant) h.nav.slide(p, c, prevTop, true, () => { if (h.nav.topScreen(p) !== prevTop) h.nav.showScreen(prevTop, false); });
        else h.nav.showScreen(prevTop, false);
      }
      p.instant = false;
    } else {
      h.nav.showScreen(c, false);
    }
    p.shownOnce = true;
    return true;
  },
  remove(h, p, c) {
    c.el?.remove();
    h.nav.updateHeader(p);
    return true;
  },
  frame(h, n, x, y, w, hh) {
    h.place(n, x, y, w, hh);
    h.nav.layoutScreens(n);
    return true;
  },
  command(h, n, cmd) {
    if (cmd === Command.Replace) { n.instant = true; return true; }
    return false;
  },
  measure() { return [0, 0]; },
};

const screen = {
  create(h, n) {
    own(n, div("asuka-screen"));
    n.title = "";
    n.actions = [];
    n.search = null;
    n.backGesture = true;
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Screen.Title: n.title = v; break;
      case P.Screen.Actions: n.actions = decodeActions(v); break;
      case P.Screen.BackGesture: n.backGesture = !!v; return;
      case P.Screen.Icon: n.icon = v; break;
      case P.Screen.Search: n.search = v; break;
      case P.Screen.SearchText: n.searchText = v; break;
      case P.Screen.SearchButton: n.searchButton = !!v; break;
      case P.Screen.BarColor: n.barColor = v; break;
      default: return;
    }
    const parent = n.parent;
    if (parent?.kind === Kind.Stack) h.nav.updateHeader(parent);
    if (parent?.kind === Kind.Tabs) parent.tabs.render(h, parent);
  },
  frame() { return true; }, // sized by the host
  destroy(h, n) {
    const p = n.parent;
    if (p?.kind === Kind.Stack) {
      const wasTop = h.nav.topScreen(p) === n;
      if (n.leaving) {
        // The user popped it: it's sliding out already.
        p.children.splice(p.children.indexOf(n), 1);
        n.parent = null;
        h.nav.updateHeader(p);
        return true;
      }
      p.children.splice(p.children.indexOf(n), 1);
      n.parent = null;
      if (wasTop && !p.instant && p.el.isConnected && h.nav.topScreen(p)) {
        // Popped by the app: it slides out, then goes.
        n.leaving = true;
        p.children.push(n);
        n.parent = p;
        h.nav.popScreen(p, n);
        p.children.splice(p.children.indexOf(n), 1);
        n.parent = null;
        h.nav.updateHeader(p);
        return true;
      }
      const below = h.nav.topScreen(p);
      if (below) h.nav.showScreen(below, true);
      n.el.remove();
      h.nav.updateHeader(p);
      return true;
    }
    if (p?.kind === Kind.Tabs) {
      p.children.splice(p.children.indexOf(n), 1);
      n.parent = null;
      n.el.remove();
      p.tabs.render(h, p);
      return true;
    }
    return false;
  },
  measure() { return [0, 0]; },
};

const tabs = {
  create(h, n) {
    const el = own(n, div("asuka-tabs"));
    const bar = div("asuka-tabbar");
    bar.setAttribute("role", "tablist");
    el.appendChild(bar);
    n.tabs = {
      bar,
      selected: 0,
      render(h, n) {
        bar.textContent = "";
        const rtl = getComputedStyle(n.el).direction === "rtl";
        n.children.forEach((s, i) => {
          const b = document.createElement("button");
          b.type = "button";
          b.className = "asuka-tab asuka-control";
          b.setAttribute("role", "tab");
          b.setAttribute("aria-selected", String(i === this.selected));
          if (s.icon) {
            const sym = symbolSVG(this.selected === i ? s.icon + ".fill" : s.icon, rtl) || symbolSVG(s.icon, rtl);
            if (sym) b.appendChild(sym);
          }
          const t = document.createElement("span");
          t.textContent = s.title;
          b.appendChild(t);
          b.addEventListener("click", () => {
            if (this.selected === i) {
              // Tapping the selected tab again pops its stack to its root, as UIKit's does.
              return;
            }
            this.selected = i;
            this.show(h, n);
            h.emit(n, Event.Selected, (id) => Events.selected(id, i));
          });
          bar.appendChild(b);
        });
        bar.style.paddingBottom = (h.nav.touchesBottom(n) ? h.safeArea()[2] : 0) + "px";
        this.show(h, n);
      },
      show(h, n) {
        n.children.forEach((s, i) => {
          s.el.classList.toggle("asuka-hidden-page", i !== this.selected);
          if (i !== this.selected) s.el.setAttribute("inert", ""); else s.el.removeAttribute("inert");
        });
        [...bar.children].forEach((b, i) => b.setAttribute("aria-selected", String(i === this.selected)));
        h.nav.updateTitle();
        h.nav.updateGuard();
      },
    };
  },
  prop(h, n, p, v) {
    if (p === P.Tabs.Selected) {
      n.tabs.selected = v;
      n.tabs.render(h, n);
    }
  },
  insert(h, p, c, i) {
    let next = null;
    for (let k = i + 1; k < p.children.length; k++) if (p.children[k].el?.parentNode === p.el) { next = p.children[k].el; break; }
    p.el.insertBefore(c.el, next || p.tabs.bar);
    p.tabs.render(h, p);
    h.nav.layoutScreens(p);
    return true;
  },
  remove(h, p, c) {
    c.el.remove();
    p.tabs.render(h, p);
    return true;
  },
  frame(h, n, x, y, w, hh) {
    h.place(n, x, y, w, hh);
    h.nav.layoutScreens(n);
    n.tabs.bar.style.paddingBottom = (h.nav.touchesBottom(n) ? h.safeArea()[2] : 0) + "px";
    return true;
  },
  measure() { return [0, 0]; },
};

const sheet = {
  create(h, n) {
    own(n, div("asuka-sheet"));
    n.detents = 0;
  },
  prop(h, n, p, v) {
    if (p === P.Sheet.Detents) n.detents = v;
    if (p === P.Sheet.Grabber) n.grabber = !!v;
    if (p === P.Sheet.Window) n.winID = v; // the window it's declared in: the root's, a Window's
  },
  frame() { return true; },
  destroy(h, n) {
    h.nav.closeDialog(n);
    return true;
  },
  measure() { return [0, 0]; },
};

const alert = {
  create(h, n) {
    n.el = null;
    n.title = "";
    n.message = "";
    n.actions = [];
    n.alertStyle = 0;
  },
  prop(h, n, p, v) {
    if (p === P.Alert.Title) n.title = v;
    if (p === P.Alert.Message) n.message = v;
    if (p === P.Alert.Buttons) n.actions = decodeActions(v);
    if (p === P.Alert.Style) n.alertStyle = v;
    if (p === P.Alert.Window) n.winID = v;
  },
  common() { return true; },
  destroy(h, n) {
    if (n.shown) h.nav.closeDialog(n);
    return true;
  },
  measure() { return [0, 0]; },
};

const popover = {
  create(h, n) {
    own(n, div("asuka-popover-content"));
    n.pop = { anchor: 0, w: 0, h: 0 };
  },
  prop(h, n, p, v) {
    if (p === P.Popover.Anchor) n.pop.anchor = v;
    if (p === P.Popover.Width) n.pop.w = v;
    if (p === P.Popover.Height) n.pop.h = v;
    if (n.shown) h.nav.placePopover(n);
  },
  frame() { return true; },
  destroy(h, n) {
    if (n.shown) {
      n.shown = false;
      h.nav.dialogs = h.nav.dialogs.filter((x) => x !== n);
      n.dialog.hidePopover();
      n.dialog.remove();
      h.nav.updateGuard();
    }
    return true;
  },
  measure() { return [0, 0]; },
};

/**
 * decodeWindowConfig reads a window's options (ops.WindowConfig): the
 * Window kind's Config prop, the root's WindowOptions command.
 */
export function decodeWindowConfig(b) {
  const c = { width: 0, height: 0, minWidth: 0, minHeight: 0, maxWidth: 0, maxHeight: 0, x: 0, y: 0, flags: 0, titleBar: 0, background: 0, parent: 0, stateKey: "", title: "" };
  const r = new Reader(b);
  if (b.length < 32) {
    if (b.length >= 16) {
      c.width = r.f32(); c.height = r.f32(); c.minWidth = r.f32(); c.minHeight = r.f32();
      c.title = new TextDecoder().decode(b.subarray(16));
    }
    return c;
  }
  c.width = r.f32(); c.height = r.f32(); c.minWidth = r.f32(); c.minHeight = r.f32();
  c.maxWidth = r.f32(); c.maxHeight = r.f32(); c.x = r.f32(); c.y = r.f32();
  if (b.length < 32 + 4 + 1 + 8 + 4 + 4) return c;
  c.flags = r.u32(); c.titleBar = r.u8(); c.background = r.u64(); c.parent = r.u32();
  const n = r.u32();
  if (r.p + n > b.length) return c;
  c.stateKey = new TextDecoder().decode(b.subarray(r.p, r.p + n));
  c.title = new TextDecoder().decode(b.subarray(r.p + n));
  return c;
}

/**
 * decodeFlyout reads what a flyout hangs from (ops.Flyout): the Window
 * kind's Flyout prop, u32 tray icon (0: none), f32 x, y, width, height (a
 * rectangle of its parent's content), u8 Placement, f32 gap. Args cut
 * short read as a point at the parent's top left, below it.
 */
export function decodeFlyout(b) {
  const f = { tray: 0, x: 0, y: 0, width: 0, height: 0, placement: 0, gap: 0 };
  if (b.length < 25) return f;
  const r = new Reader(b);
  f.tray = r.u32();
  f.x = r.f32(); f.y = r.f32(); f.width = r.f32(); f.height = r.f32();
  f.placement = r.u8();
  f.gap = r.f32();
  return f;
}

/** A Window's title as its bar shows it: a * first while edited. */
function windowTitle(n) {
  return (n.edited ? "*" : "") + (n.title || "");
}

const windowKind = {
  create(h, n) {
    own(n, div("asuka-window-content"));
    n.win = {};
    n.title = "";
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Window.Title:
        n.title = v;
        if (n.winTitle) n.winTitle.textContent = windowTitle(n);
        else if (n.dialog) n.dialog.setAttribute("aria-label", v);
        break;
      case P.Window.Flyout:
        // Placed again against its new anchor (a shown window that isn't
        // a flyout doesn't become one).
        if (n.shown && !n.win.want) break;
        n.win.flyout = decodeFlyout(v);
        if (n.shown) h.nav.sizeDialog(n);
        break;
      // A document's: a * before its title while it has unsaved changes.
      case P.Window.Edited: n.edited = !!v; if (n.winTitle) n.winTitle.textContent = windowTitle(n); break;
      case P.Window.Config: {
        const c = decodeWindowConfig(v);
        n.win.cfg = c;
        n.win.width = c.width; n.win.height = c.height;
        n.win.minWidth = c.minWidth; n.win.minHeight = c.minHeight;
        break;
      }
    }
  },
  frame() { return true; },
  destroy(h, n) {
    if (n.shown) {
      n.shown = false;
      h.windows.save(n);
      if (document.fullscreenElement === n.dialog) document.exitFullscreen().catch(() => {});
      h.nav.dialogs = h.nav.dialogs.filter((x) => x !== n);
      n.dialog.remove();
      n.backdrop?.remove();
      h.nav.updateGuard();
    }
    return true;
  },
  measure() { return [0, 0]; },
};

const accessory = {
  create(h, n) {
    own(n, div("asuka-accessory"));
  },
  prop(h, n, p, v) {
    if (p === P.Accessory.Width) n.el.style.width = v + "px";
    if (p === P.Accessory.Height) n.el.style.height = v + "px";
  },
  frame() { return true; },
  measure() { return [0, 0]; },
};

const split = {
  create(h, n) {
    const el = own(n, div("asuka-split"));
    const sidebar = div("asuka-sidebar");
    sidebar.setAttribute("role", "navigation");
    const head = div("asuka-sidebar-title");
    const list = div("asuka-sidebar-list");
    sidebar.append(head, list);
    const scrim = div("asuka-scrim");
    scrim.addEventListener("click", () => h.nav.closeDrawer(n));
    el.append(sidebar, scrim);
    n.split = { sidebar, head, list, scrim, sections: [], selected: -1, beside: null };
  },
  prop(h, n, p, v) {
    const s = n.split;
    if (p === P.Split.Items) {
      const r = new Reader(v);
      const count = r.u32();
      s.sections = [];
      for (let i = 0; i < count; i++) {
        const title = r.string();
        const k = r.u32();
        const items = [];
        for (let j = 0; j < k; j++) items.push({ title: r.string(), icon: r.string() });
        s.sections.push({ title, items });
      }
    }
    if (p === P.Split.Selected) s.selected = v;
    if (p === P.Split.Title) { n.title = v; s.head.textContent = v; s.head.hidden = !v; }
    renderSidebar(h, n);
  },
  insert(h, p, c, i) {
    p.el.insertBefore(c.el, p.split.sidebar);
    h.nav.layoutSplit(p);
    if (c.kind === Kind.Stack) h.nav.updateHeader(c);
    return true;
  },
  frame(h, n, x, y, w, hh) {
    h.place(n, x, y, w, hh);
    h.nav.layoutSplit(n);
    return true;
  },
  measure() { return [0, 0]; },
};

function renderSidebar(h, n) {
  const s = n.split;
  s.list.textContent = "";
  let index = 0;
  for (const sec of s.sections) {
    if (sec.title) {
      const t = div("asuka-sidebar-section");
      t.textContent = sec.title;
      s.list.appendChild(t);
    }
    for (const it of sec.items) {
      const i = index++;
      const b = document.createElement("button");
      b.type = "button";
      b.className = "asuka-sidebar-item asuka-control";
      b.setAttribute("aria-current", String(i === s.selected));
      if (it.icon) { const sym = symbolSVG(it.icon, h.rtl); if (sym) b.appendChild(sym); }
      const t = document.createElement("span");
      t.textContent = it.title;
      b.appendChild(t);
      b.addEventListener("click", () => {
        h.nav.closeDrawer(n);
        if (i === s.selected) return;
        s.selected = i;
        renderSidebar(h, n);
        h.emit(n, Event.Selected, (id) => Events.selected(id, i));
      });
      s.list.appendChild(b);
    }
  }
}

export const navKinds = {
  [Kind.Stack]: stack,
  [Kind.Screen]: screen,
  [Kind.Tabs]: tabs,
  [Kind.Sheet]: sheet,
  [Kind.Alert]: alert,
  [Kind.Popover]: popover,
  [Kind.Window]: windowKind,
  [Kind.Accessory]: accessory,
  [Kind.Split]: split,
};

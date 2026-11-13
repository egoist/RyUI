// A Snackbar in a browser: an element in the top layer (a manual
// popover), role status, at the bottom of the window it's on (the page,
// or a Window's panel), above a bottom tab bar, the safe area and the
// on-screen keyboard; over a modal sheet or alert, inside it (outside a
// modal dialog everything is inert). A rounded bar in the bar material,
// as the other hosts' system looks: its message, its action's button in
// the accent and a close button. It rises in (fades with reduced
// motion), a swipe down dismisses it, its time waits while the pointer
// is over it or the focus in it, and screen readers read it as it shows
// (a polite live region, filled once it's in the page).

import { KindProp as P, Event, Events } from "./ops.js";

/** The bar's most width, and its margin from the window's edges (CSS pixels). */
export const maxWidth = 560;
export const margin = 16;

/** timeout is how long a snackbar shows, in milliseconds: 0, until it's dismissed. */
export function timeout(duration) {
  switch (duration) {
    case 1: return 10000; // Long
    case 2: return 0; // Indefinite
    default: return 4000; // Short: Material's
  }
}

/**
 * place is where the bar goes, w by h, in area (the window's content, in
 * the viewport's coordinates) whose bottom insets is covered (a tab bar,
 * the safe area, the keyboard): centered, margin from the edges.
 */
export function place(area, inset, w, h) {
  const width = Math.min(w, maxWidth, Math.max(0, area.w - 2 * margin));
  return {
    x: Math.round(area.x + (area.w - width) / 2),
    y: Math.round(area.y + area.h - Math.max(inset, 0) - margin - h),
    w: width,
  };
}

function reduced() { return matchMedia("(prefers-reduced-motion: reduce)").matches; }

export const snackbarKind = {
  create(h, n) {
    n.el = null;
    n.message = "";
    n.actionTitle = "";
    n.duration = 0;
  },
  prop(h, n, p, v) {
    if (p === P.Snackbar.Message) {
      n.message = v;
      if (n.bar) n.bar.text.textContent = v;
    }
    if (p === P.Snackbar.Action) {
      n.actionTitle = v;
      if (n.bar) {
        n.bar.action.textContent = v;
        n.bar.action.hidden = !v;
        placeSnackbar(h, n);
      }
    }
    if (p === P.Snackbar.Duration) n.duration = v;
    if (p === P.Snackbar.Window) n.winID = v;
  },
  common() { return true; },
  destroy(h, n) {
    n.done = true;
    leave(h, n);
    return true;
  },
};

/** presentSnackbar shows it on its window's topmost content (symbolSVG: media.js's, which nav.js has). */
export function presentSnackbar(h, n, symbolSVG) {
  if (n.bar || n.done) return;
  const el = document.createElement("div");
  el.className = "asuka-snackbar";
  el.setAttribute("role", "status");
  el.setAttribute("aria-live", "polite");
  el.setAttribute("aria-atomic", "true");
  el.dir = h.rtl ? "rtl" : "ltr";
  const text = document.createElement("span");
  text.className = "asuka-snackbar-message";
  const action = document.createElement("button");
  action.type = "button";
  action.className = "asuka-snackbar-action asuka-control";
  action.textContent = n.actionTitle;
  action.hidden = !n.actionTitle;
  const close = document.createElement("button");
  close.type = "button";
  close.className = "asuka-snackbar-close asuka-control";
  const label = h.nav.word("close");
  close.setAttribute("aria-label", label);
  close.title = label;
  const x = symbolSVG("xmark", h.rtl);
  if (x) close.appendChild(x);
  el.append(text, action, close);
  n.bar = { el, text, action, close, left: timeout(n.duration), timer: 0, started: 0, held: 0 };

  action.addEventListener("click", () => report(h, n, Event.Action));
  close.addEventListener("click", () => report(h, n, Event.Dismissed));
  // Its time waits while the pointer is over it or the focus in it.
  const hold = (on) => { n.bar.held += on ? 1 : -1; if (n.bar.held > 0) pause(n); else resume(h, n); };
  el.addEventListener("pointerenter", () => hold(true));
  el.addEventListener("pointerleave", () => hold(false));
  el.addEventListener("focusin", () => hold(true));
  el.addEventListener("focusout", () => hold(false));
  el.addEventListener("keydown", (e) => { if (e.key === "Escape") { e.stopPropagation(); report(h, n, Event.Dismissed); } });
  swipe(h, n, el);

  // Over the topmost modal dialog on its window, inside it; else in the container.
  const top = h.nav.dialogs.findLast((d) => d.dialog?.tagName === "DIALOG" && d.dialog.open && d.winID === n.winID);
  (top?.dialog || h.container).appendChild(el);
  if (el.showPopover) { el.popover = "manual"; el.showPopover(); }
  placeSnackbar(h, n);
  // Filled once it's in the page: a live region reads what changes in it.
  requestAnimationFrame(() => { if (n.bar) text.textContent = n.message; placeSnackbar(h, n); });
  if (!reduced()) {
    el.animate([{ opacity: 0, transform: "translateY(24px) scale(0.96)" }, { opacity: 1, transform: "none" }],
      { duration: 350, easing: "cubic-bezier(0.2, 0.9, 0.3, 1.15)" });
  } else {
    el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 200 });
  }
  resume(h, n);
}

/** placeSnackbar puts it at the bottom of its window's content. */
export function placeSnackbar(h, n) {
  const b = n.bar;
  if (!b || b.leaving) return;
  const nav = h.nav;
  const c = h.container.getBoundingClientRect();
  const w = nav.panelOf(n);
  let area, inset = 0;
  if (w) {
    area = nav.panelArea(w, c);
  } else {
    area = { x: c.left, y: c.top, w: c.width, h: c.height };
    const e = h.env();
    inset = Math.max(e.safeBottom, e.keyboard);
    // A bottom tab bar that shows: above it.
    for (const t of h.container.querySelectorAll(".asuka-tabbar")) {
      if (!t.offsetParent || t.closest(".asuka-covered, [inert]")) continue;
      const r = t.getBoundingClientRect();
      if (r.height > 0 && Math.abs(r.bottom - c.bottom) < 1) inset = Math.max(inset, c.bottom - r.top);
    }
  }
  const s = b.el.style;
  s.maxWidth = Math.min(maxWidth, Math.max(0, area.w - 2 * margin)) + "px";
  const r = b.el.getBoundingClientRect();
  const at = place(area, inset, r.width, r.height);
  s.left = at.x + "px";
  s.top = at.y + "px";
}

function pause(n) {
  const b = n.bar;
  if (!b?.timer) return;
  clearTimeout(b.timer);
  b.timer = 0;
  b.left = Math.max(1000, b.left - (performance.now() - b.started));
}

function resume(h, n) {
  const b = n.bar;
  if (!b || b.timer || b.left <= 0 || b.held > 0 || n.done) return;
  b.started = performance.now();
  b.timer = setTimeout(() => { b.timer = 0; report(h, n, Event.Dismissed); }, b.left);
}

/** report tells the core it went (its action, its time, closed, swiped away) and takes it out; the core destroys it. */
function report(h, n, kind) {
  if (n.done) return;
  n.done = true;
  leave(h, n);
  if (kind === Event.Action) h.emit(n, Event.Action, (id) => Events.action(id, 0));
  else h.emit(n, Event.Dismissed, Events.dismissed);
}

/** leave takes it out, sinking and fading (fading with reduced motion), from where a swipe left it. */
function leave(h, n, dy = 0) {
  const b = n.bar;
  if (!b || b.leaving) return;
  b.leaving = true;
  clearTimeout(b.timer);
  b.timer = 0;
  const el = b.el;
  el.inert = true;
  const done = () => { if (el.hidePopover && el.matches(":popover-open")) el.hidePopover(); el.remove(); };
  if (!el.isConnected) return done();
  const from = `translateY(${dy}px)`;
  const a = reduced()
    ? el.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 150, fill: "forwards" })
    : el.animate([{ opacity: 1, transform: from }, { opacity: 0, transform: `translateY(${dy + 32}px)` }], { duration: 200, easing: "ease-in", fill: "forwards" });
  a.onfinish = done;
}

/** swipe lets a touch or a drag move it down, and dismisses it past a third of its height or flicked. */
function swipe(h, n, el) {
  let start = null;
  el.addEventListener("pointerdown", (e) => {
    if (e.target.closest("button") || e.button !== 0) return;
    start = { y: e.clientY, t: performance.now(), id: e.pointerId };
    el.setPointerCapture(e.pointerId);
  });
  el.addEventListener("pointermove", (e) => {
    if (!start || e.pointerId !== start.id) return;
    const dy = e.clientY - start.y;
    el.style.transform = `translateY(${dy > 0 ? dy : -Math.sqrt(-dy) * 2}px)`;
  });
  const end = (e, cancelled) => {
    if (!start || e.pointerId !== start.id) return;
    const dy = e.clientY - start.y, v = dy / Math.max(1, performance.now() - start.t);
    start = null;
    if (!cancelled && (dy > el.offsetHeight / 3 || v > 0.6)) {
      if (n.done) return;
      n.done = true;
      el.style.transform = "";
      leave(h, n, dy);
      h.emit(n, Event.Dismissed, Events.dismissed);
      return;
    }
    el.style.transform = "";
    if (!reduced()) el.animate([{ transform: `translateY(${Math.max(dy, 0)}px)` }, { transform: "none" }], { duration: 250, easing: "cubic-bezier(0.2, 0.9, 0.3, 1.15)" });
  };
  el.addEventListener("pointerup", (e) => end(e, false));
  el.addEventListener("pointercancel", (e) => end(e, true));
}

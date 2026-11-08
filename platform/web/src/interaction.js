// Interaction: presses, long presses, pressed and hover states, pans,
// pinches, rotations and swipes, drag and drop, and the screen reader's
// adjusting. One set of pointer listeners on the app's container finds
// the view each pointer is for: the innermost one that's pressable (or a
// control, which keeps its own presses), as UIKit's and Android's views
// behave.

import { Event, Events, Writer, gesture, Reader } from "./ops.js";
import * as fs from "./fs.js";

const slop = 10; // points a touch moves before it's a drag, not a tap
const longPressMs = 500;
const tapDelayMs = 150; // UIScrollView's delay before a touch in it shows pressed
const pressedMinMs = 64; // a quick tap still shows its pressed look

const pressEvents = new Set([Event.Press, Event.LongPress, Event.Pressed]);
const gestureEvents = new Set([Event.Pan, Event.Pinch, Event.Rotate, Event.Swipe]);

export class Interaction {
  constructor(h) {
    this.h = h;
    this.press = null; // the primary pointer's press
    this.pointers = new Map(); // pointerId -> {x, y} (for pinches)
    this.gesture = null; // a pan, pinch or rotation under way
    this.wheel = null; // a trackpad's pinch (ctrl + wheel)
    this.dragSource = null;
    const c = h.container;
    c.addEventListener("pointerdown", (e) => this.down(e));
    c.addEventListener("pointermove", (e) => this.move(e));
    c.addEventListener("pointerup", (e) => this.up(e));
    c.addEventListener("pointercancel", (e) => this.cancel(e));
    c.addEventListener("contextmenu", (e) => this.context(e));
    c.addEventListener("keydown", (e) => this.key(e));
    c.addEventListener("wheel", (e) => this.wheelPinch(e), { passive: false });
    c.addEventListener("scroll", () => this.scrolled(), true);
    c.addEventListener("dragstart", (e) => this.dragStart(e));
    c.addEventListener("dragend", (e) => this.dragEnd(e));
    c.addEventListener("dragover", (e) => this.dragOver(e));
    c.addEventListener("dragleave", (e) => this.dragLeave(e));
    c.addEventListener("drop", (e) => this.drop(e));
    window.addEventListener("blur", () => this.cancelAll());
  }

  /** listen makes n report what the core listens for. */
  listen(n, e) {
    if (!n.el) return;
    if (pressEvents.has(e)) this.pressable(n);
    else if (gestureEvents.has(e)) n.el.classList.add("ryui-gesture");
    else if (e === Event.Hover) this.hoverable(n);
  }

  unlisten(n, e) {
    if (e === Event.Hover && n.hover) {
      n.el.removeEventListener("pointerenter", n.hover.enter);
      n.el.removeEventListener("pointerleave", n.hover.leave);
      if (n.hovered) this.h.dispatch(Events.hover(n.id, false));
      n.hover = null;
      n.hovered = false;
    }
    if (pressEvents.has(e) && !n.listening(Event.Press) && !n.listening(Event.LongPress) && !n.listening(Event.Pressed) && !n.menu) {
      n.el.classList.remove("ryui-pressable");
    }
  }

  /** pressable marks n as a view that takes presses: a button for screen readers and the keyboard, if it fires one. */
  pressable(n) {
    n.el.classList.add("ryui-pressable");
    if (n.listening(Event.Press) || n.menu) {
      if (!n.el.hasAttribute("role")) n.el.setAttribute("role", "button");
      if (!n.el.hasAttribute("tabindex")) n.el.tabIndex = 0;
    }
  }

  hoverable(n) {
    if (n.hover) return;
    n.hover = {
      enter: (e) => { if (e.pointerType !== "touch" && !n.hovered) { n.hovered = true; this.h.dispatch(Events.hover(n.id, true)); } },
      leave: () => { if (n.hovered) { n.hovered = false; this.h.dispatch(Events.hover(n.id, false)); } },
    };
    n.el.addEventListener("pointerenter", n.hover.enter);
    n.el.addEventListener("pointerleave", n.hover.leave);
    if (n.el.matches(":hover")) n.hover.enter({ pointerType: "mouse" });
  }

  /** forget: a view is going; what it was in leaves, so what follows it doesn't stay stuck. */
  forget(n) {
    if (n === this.press?.n) this.endPress(false);
    if (n.hovered) { n.hovered = false; this.h.dispatch(Events.hover(n.id, false)); }
    if (this.gesture?.n === n) this.gesture = null;
  }

  // --- Finding the view a pointer is for ---

  nodeOf(el) {
    for (; el && el !== this.h.container; el = el.parentElement) {
      if (el.ryuiNode) return el.ryuiNode;
    }
    return null;
  }

  /** pressTarget is the innermost pressable view at el, or null where a control (a button, a field) takes the press. */
  pressTarget(el) {
    for (; el && el !== this.h.container; el = el.parentElement) {
      if (el.matches("button, input, textarea, select, a[href], [contenteditable=true], .ryui-control")) return null;
      const n = el.ryuiNode;
      if (n && (n.listening(Event.Press) || n.listening(Event.LongPress) || n.listening(Event.Pressed) || n.menu)) return n;
    }
    return null;
  }

  gestureTarget(el, kinds) {
    for (; el && el !== this.h.container; el = el.parentElement) {
      const n = el.ryuiNode;
      if (n && kinds.some((k) => n.listening(k))) return n;
    }
    return null;
  }

  inScroll(el) {
    for (; el && el !== this.h.container; el = el.parentElement) {
      if (el.classList.contains("ryui-scroller")) return true;
    }
    return false;
  }

  // --- Pointers ---

  down(e) {
    this.pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (this.pointers.size === 2) this.twoFingers(e);
    if (!e.isPrimary || (e.pointerType === "mouse" && e.button !== 0)) return;
    const n = this.pressTarget(e.target);
    const g = this.gestureTarget(e.target, [Event.Pan, Event.Swipe]);
    this.gesture = g ? { n: g, kind: "pan", x0: e.clientX, y0: e.clientY, t0: e.timeStamp, began: false, samples: [[e.timeStamp, e.clientX, e.clientY]], id: e.pointerId } : this.gesture;
    if (!n) return;
    const p = { n, id: e.pointerId, x0: e.clientX, y0: e.clientY, t0: performance.now(), shown: false, timer: 0, long: 0, inside: true, type: e.pointerType };
    this.press = p;
    if (n.listening(Event.Pressed)) {
      if (this.inScroll(n.el.parentElement)) p.timer = setTimeout(() => this.showPressed(p, true), tapDelayMs);
      else this.showPressed(p, true);
    }
    if (n.listening(Event.LongPress) || (n.menu && n.menuTrigger === 1)) {
      p.long = setTimeout(() => this.longPress(p), longPressMs);
    }
  }

  showPressed(p, on) {
    if (on === p.shown) return;
    p.shown = on;
    p.shownAt = performance.now();
    this.h.dispatch(Events.pressed(p.n.id, on));
  }

  longPress(p) {
    if (this.press !== p) return;
    this.endPress(false);
    const n = p.n;
    if (n.menu && n.menuTrigger === 1) this.h.menus.open(n, p.x0, p.y0);
    else this.h.emit(n, Event.LongPress, Events.longPress);
  }

  move(e) {
    if (this.pointers.has(e.pointerId)) this.pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const g = this.gesture;
    if (g && g.kind === "two") { this.twoMoved(e); return; }
    const p = this.press;
    if (p && p.id === e.pointerId) {
      const moved = Math.hypot(e.clientX - p.x0, e.clientY - p.y0) > slop;
      if (moved && (this.inScroll(p.n.el.parentElement) || g)) {
        this.endPress(false); // a scroll or a pan takes the touch
      } else if (moved) {
        clearTimeout(p.long);
        p.long = 0;
        const r = p.n.el.getBoundingClientRect();
        const m = p.type === "touch" ? 20 : 0;
        p.inside = e.clientX >= r.left - m && e.clientX <= r.right + m && e.clientY >= r.top - m && e.clientY <= r.bottom + m;
        if (p.n.listening(Event.Pressed) && !p.timer) this.showPressed(p, p.inside);
      }
    }
    if (g && g.kind === "pan" && g.id === e.pointerId) {
      g.samples.push([e.timeStamp, e.clientX, e.clientY]);
      if (g.samples.length > 6) g.samples.shift();
      if (!g.began && Math.hypot(e.clientX - g.x0, e.clientY - g.y0) > slop) {
        g.began = true;
        try { g.n.el.setPointerCapture(e.pointerId); } catch {}
        this.pan(g, e, 0);
      } else if (g.began) {
        this.pan(g, e, 1);
      }
    }
  }

  pan(g, e, state) {
    if (!g.n.listening(Event.Pan)) return;
    const r = g.n.el.getBoundingClientRect();
    const [vx, vy] = velocity(g.samples);
    const s = gesture();
    Object.assign(s, { state, x: e.clientX - r.left, y: e.clientY - r.top, translationX: e.clientX - g.x0, translationY: e.clientY - g.y0, velocityX: vx, velocityY: vy });
    this.h.dispatch(Events.pan(g.n.id, s));
  }

  up(e) {
    this.pointers.delete(e.pointerId);
    const g = this.gesture;
    if (g && g.kind === "two") {
      if (this.pointers.size < 2) this.twoEnded(2);
      return;
    }
    if (g && g.kind === "pan" && g.id === e.pointerId) {
      this.gesture = null;
      if (g.began) {
        this.pan(g, e, 2);
        this.swipe(g, e);
      }
    }
    const p = this.press;
    if (!p || p.id !== e.pointerId) return;
    this.press = null;
    clearTimeout(p.long);
    const fire = p.inside;
    if (p.timer) {
      // Released before the tap delay: shown pressed for a moment anyway.
      clearTimeout(p.timer);
      if (fire && p.n.listening(Event.Pressed)) {
        this.showPressed(p, true);
        setTimeout(() => this.showPressed(p, false), pressedMinMs);
      }
    } else if (p.shown) {
      const left = pressedMinMs - (performance.now() - p.shownAt);
      if (left > 0) setTimeout(() => this.showPressed(p, false), left);
      else this.showPressed(p, false);
    }
    if (!fire) return;
    if (p.n.menu && p.n.menuTrigger !== 1) this.h.menus.open(p.n, e.clientX, e.clientY);
    else this.h.emit(p.n, Event.Press, Events.press);
  }

  swipe(g, e) {
    if (!g.n.listening(Event.Swipe)) return;
    const dx = e.clientX - g.x0, dy = e.clientY - g.y0;
    const [vx, vy] = velocity(g.samples);
    let dir = 0;
    if (Math.abs(dx) > Math.abs(dy) && Math.abs(dx) > 30 && Math.abs(vx) > 300) dir = dx > 0 ? 1 : 2;
    else if (Math.abs(dy) > 30 && Math.abs(vy) > 300) dir = dy > 0 ? 8 : 4;
    if (dir) this.h.dispatch(Events.swipe(g.n.id, dir));
  }

  cancel(e) {
    this.pointers.delete(e.pointerId);
    if (this.press?.id === e.pointerId) this.endPress(false);
    const g = this.gesture;
    if (g?.kind === "pan" && g.id === e.pointerId) {
      this.gesture = null;
      if (g.began) this.pan(g, e, 3);
    }
    if (g?.kind === "two") this.twoEnded(3);
  }

  cancelAll() {
    if (this.press) this.endPress(false);
    this.pointers.clear();
  }

  scrolled() {
    // A scroll takes the touch: the press doesn't fire.
    if (this.press && this.press.type !== "mouse") this.endPress(false);
  }

  endPress() {
    const p = this.press;
    if (!p) return;
    this.press = null;
    clearTimeout(p.timer);
    clearTimeout(p.long);
    if (p.shown) this.showPressed(p, false);
  }

  context(e) {
    const n = this.pressTarget(e.target) || this.gestureTarget(e.target, [Event.LongPress]);
    if (!n) return;
    if (n.menu && n.menuTrigger === 1) {
      e.preventDefault();
      this.endPress();
      this.h.menus.open(n, e.clientX, e.clientY);
    } else if (n.listening(Event.LongPress) && e.pointerType !== "touch") {
      // A secondary click is a long press with a mouse, as on a Mac.
      e.preventDefault();
      this.endPress();
      this.h.emit(n, Event.LongPress, Events.longPress);
    } else if (n.listening(Event.LongPress)) {
      e.preventDefault(); // the long press timer has it
    }
  }

  key(e) {
    const n = e.target.ryuiNode;
    if (!n || e.target !== n.el) return;
    if (n.adjustable && (e.key === "ArrowUp" || e.key === "ArrowRight" || e.key === "ArrowDown" || e.key === "ArrowLeft")) {
      e.preventDefault();
      const up = e.key === "ArrowUp" || e.key === "ArrowRight";
      this.h.dispatch(Events.adjust(n.id, up ? 1 : -1));
      return;
    }
    if ((e.key === "Enter" || e.key === " ") && (n.listening(Event.Press) || n.menu)) {
      e.preventDefault();
      if (n.menu && n.menuTrigger !== 1) {
        const r = n.el.getBoundingClientRect();
        this.h.menus.open(n, r.left, r.bottom);
      } else {
        this.h.emit(n, Event.Press, Events.press);
      }
    }
  }

  // --- Pinches and rotations ---

  twoFingers(e) {
    const n = this.gestureTarget(e.target, [Event.Pinch, Event.Rotate]);
    if (!n) return;
    this.endPress();
    if (this.gesture?.kind === "pan" && this.gesture.began) this.pan(this.gesture, e, 3);
    const [a, b] = [...this.pointers.values()];
    this.gesture = { kind: "two", n, d0: Math.hypot(b.x - a.x, b.y - a.y) || 1, a0: Math.atan2(b.y - a.y, b.x - a.x), began: false, scale: 1, rot: 0 };
  }

  twoMoved() {
    const g = this.gesture;
    if (this.pointers.size < 2) return;
    const [a, b] = [...this.pointers.values()];
    g.scale = Math.hypot(b.x - a.x, b.y - a.y) / g.d0;
    g.rot = Math.atan2(b.y - a.y, b.x - a.x) - g.a0;
    g.cx = (a.x + b.x) / 2;
    g.cy = (a.y + b.y) / 2;
    this.two(g, g.began ? 1 : 0);
    g.began = true;
  }

  two(g, state) {
    const r = g.n.el.getBoundingClientRect();
    const s = gesture();
    Object.assign(s, { state, x: (g.cx ?? r.left) - r.left, y: (g.cy ?? r.top) - r.top, scale: g.scale, rotation: g.rot });
    if (g.n.listening(Event.Pinch)) this.h.dispatch(Events.pinch(g.n.id, s));
    if (g.n.listening(Event.Rotate)) this.h.dispatch(Events.rotate(g.n.id, s));
  }

  twoEnded(state) {
    const g = this.gesture;
    this.gesture = null;
    if (g?.began) this.two(g, state);
  }

  /** wheelPinch: a trackpad's pinch comes as a wheel with ctrl; it pinches the view, not the page. */
  wheelPinch(e) {
    if (!e.ctrlKey) return;
    const n = this.gestureTarget(e.target, [Event.Pinch]);
    if (!n) return;
    e.preventDefault();
    let w = this.wheel;
    if (!w || w.n !== n) {
      w = this.wheel = { kind: "two", n, scale: 1, rot: 0, began: false };
    }
    w.scale *= Math.exp(-e.deltaY / 100);
    w.cx = e.clientX;
    w.cy = e.clientY;
    this.two(w, w.began ? 1 : 0);
    w.began = true;
    clearTimeout(w.timer);
    w.timer = setTimeout(() => { this.two(w, 2); this.wheel = null; }, 150);
  }

  // --- A screen reader's ---

  setActions(n, b) {
    // The web has no custom actions for screen readers: they stay the
    // view's context menu's, which the core gives it too.
    n.a11yActions = b;
  }

  setAdjustable(n, on) {
    n.adjustable = !!on;
    if (on) {
      n.el.setAttribute("role", "slider");
      if (!n.el.hasAttribute("tabindex")) n.el.tabIndex = 0;
    }
  }

  // --- Drag and drop ---

  dragStart(e) {
    const n = this.nodeOf(e.target);
    if (!n || !n.dragItems) return;
    this.endPress();
    const dt = e.dataTransfer;
    let onlyValues = true;
    for (const it of n.dragItems) {
      if (it.value) continue;
      onlyValues = false;
      if (it.kind === 1) {
        dt.setData("text/uri-list", it.text);
        dt.setData("text/plain", it.text);
      } else if (it.kind === 0) {
        dt.setData("text/plain", it.text);
      } else if (it.path) {
        const f = fs.fileOf(it.path, it.name, it.mime);
        if (f && dt.items) dt.items.add(f);
      }
    }
    dt.setData("application/x-ryui-source", String(n.id));
    for (const it of n.dragItems) if (it.mime) dt.setData("application/x-ryui-type." + it.mime.toLowerCase(), "1");
    dt.effectAllowed = onlyValues ? "move" : "copyMove";
    const r = n.el.getBoundingClientRect();
    dt.setDragImage(n.el, e.clientX - r.left, e.clientY - r.top);
    this.dragSource = n;
    this.h.dispatch(Events.dragging(n.id, 0));
  }

  dragEnd(e) {
    const n = this.dragSource;
    this.dragSource = null;
    if (n) this.h.dispatch(Events.dragging(n.id, e.dataTransfer.dropEffect !== "none" ? 1 : 2));
  }

  /** dropTarget is the innermost view under the drag that takes it. */
  dropTarget(e) {
    const types = dragTypes(e.dataTransfer);
    for (let el = e.target; el && el !== this.h.container; el = el.parentElement) {
      const n = el.ryuiNode;
      if (n && n.dropAccept && types.some((t) => matchMIME(n.dropAccept, t))) return n;
    }
    return null;
  }

  dragOver(e) {
    const n = this.dropTarget(e);
    if (this.over && this.over !== n) this.overState(this.over, 3, e);
    if (!n) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = e.dataTransfer.types.includes("application/x-ryui-source") ? "move" : "copy";
    this.overState(n, this.over === n ? 1 : 0, e);
    this.over = n;
  }

  overState(n, state, e) {
    const r = n.el.getBoundingClientRect();
    const g = gesture();
    Object.assign(g, { state, x: e.clientX - r.left, y: e.clientY - r.top });
    this.h.emit(n, Event.DragOver, (id) => Events.dragOver(id, g));
    if (state === 3) this.over = null;
  }

  dragLeave(e) {
    if (this.over && !this.over.el.contains(e.relatedTarget)) this.overState(this.over, 3, e);
  }

  async drop(e) {
    const n = this.dropTarget(e);
    if (!n) return;
    e.preventDefault();
    this.overState(n, 3, e);
    const r = n.el.getBoundingClientRect();
    const x = e.clientX - r.left, y = e.clientY - r.top;
    const dt = e.dataTransfer;
    const source = Number(dt.getData("application/x-ryui-source")) || 0;
    const items = [];
    if (!source) {
      for (const f of [...dt.files]) {
        if (!matchMIME(n.dropAccept, f.type || "application/octet-stream")) continue;
        items.push({ kind: shareKind(f.type), text: "", path: await fs.saveUpload(f), name: f.name, mime: f.type || "application/octet-stream", size: f.size });
      }
      const uri = dt.getData("text/uri-list");
      if (uri && matchMIME(n.dropAccept, "text/uri-list")) {
        for (const u of uri.split(/\r?\n/)) if (u && !u.startsWith("#")) items.push({ kind: 1, text: u, path: "", name: "", mime: "text/uri-list", size: -1 });
      } else {
        const t = dt.getData("text/plain");
        if (t && matchMIME(n.dropAccept, "text/plain")) items.push({ kind: 0, text: t, path: "", name: "", mime: "text/plain", size: -1 });
      }
    }
    const w = new Writer().f32(x).f32(y).u32(source).u8(source ? 0 : 1);
    appendItems(w, items);
    this.h.emit(n, Event.Drop, (id) => Events.drop(id, w.done()));
  }
}

/** appendItems writes items as a Shared event carries them. */
export function appendItems(w, items) {
  w.u32(items.length);
  for (const it of items) w.u8(it.kind).string(it.text || "").string(it.path || "").string(it.name || "").string(it.mime || "").i64(it.size ?? -1);
  return w;
}

/** decodeDragItems reads a view's DragItems. */
export function decodeDragItems(b) {
  if (!b || b.length < 4) return null;
  const r = new Reader(b);
  const n = r.u32();
  const out = [];
  for (let i = 0; i < n && !r.atEnd; i++) out.push({ kind: r.u8(), text: r.string(), path: r.string(), name: r.string(), mime: r.string(), value: (r.u8() & 1) !== 0 });
  return out;
}

export function shareKind(mime) {
  if (/^image\//.test(mime)) return 2;
  if (/^video\//.test(mime)) return 3;
  if (/^audio\//.test(mime)) return 4;
  return 5;
}

function dragTypes(dt) {
  const out = [];
  for (const t of dt.types) {
    if (t === "Files") {
      for (const it of dt.items || []) if (it.kind === "file") out.push(it.type || "application/octet-stream");
      if (!dt.items) out.push("application/octet-stream");
    } else if (t.startsWith("application/x-ryui-type.")) {
      out.push(t.slice("application/x-ryui-type.".length));
    } else if (t !== "application/x-ryui-source") {
      out.push(t);
    }
  }
  return out;
}

/** matchMIME is ops.MatchMIME: exact, type/*, * / * or *, without regard to case or parameters. */
export function matchMIME(patterns, mime) {
  const m = mime.split(";")[0].trim().toLowerCase();
  for (let p of patterns) {
    p = p.split(";")[0].trim().toLowerCase();
    if (p === "*" || p === "*/*") return true;
    if (p.endsWith("/*") ? m.startsWith(p.slice(0, -1)) && m.length > p.length - 1 : p === m) return true;
  }
  return false;
}

/** velocity is a pointer's, in points per second, from its last samples. */
function velocity(samples) {
  if (samples.length < 2) return [0, 0];
  const [t0, x0, y0] = samples[0];
  const [t1, x1, y1] = samples[samples.length - 1];
  const dt = (t1 - t0) / 1000;
  if (dt <= 0) return [0, 0];
  return [(x1 - x0) / dt, (y1 - y0) / dt];
}

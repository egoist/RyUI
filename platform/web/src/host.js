// Asuka's web host: the DOM, driven by the app's Go code compiled to
// WebAssembly (host/web). Each view the core makes is an absolutely
// positioned element at the frame the core laid out, so the browser's own
// layout never moves it; leaves (text, controls) are measured by the real
// element, as the native hosts measure their widgets.
//
// The core hands over a batch at a time (apply): its encoding, read in
// place out of the WebAssembly memory by the generated decoder (ops.js).
// Events go back encoded. Calls into Go happen only from the page's own
// events, never while Go is calling us: what happens meanwhile (a field's
// focus event as the core focuses it, a list tiling as its frame changes)
// waits until Go has returned.

import { decode, Reader, Events, Kind, Prop, Event, Command, environment as newEnvironment, Type } from "./ops.js";
import { kinds } from "./views.js";
import { Interaction } from "./interaction.js";
import { Navigation, decodeWindowConfig } from "./nav.js";
import { Windows } from "./window.js";
import { Modules } from "./modules.js";
import { Menus } from "./menu.js";
import { Motion } from "./motion.js";
import * as color from "./color.js";
import * as text from "./text.js";
import { redrawCanvases } from "./draw.js";

const NaN32 = NaN;
const HostIDBit = 0x80000000;

/** A view the core made, or one the host made for it (a list's cell). */
export class Node {
  constructor(id, kind) {
    this.id = id;
    this.kind = kind;
    this.el = null; // its element, placed at its frame
    this.box = null; // where its children's elements go (el by default)
    this.parent = null;
    this.children = [];
    this.listens = new Set();
    this.frame = [0, 0, 0, 0];
    this.placed = false;
    this.impl = null;
    // Common props, as last set.
    this.font = { style: 0, size: 0, weight: 0, family: "" };
    this.color = 0;
    this.background = 0;
    this.borderColor = 0;
    this.borderWidth = 0;
    this.radius = 0;
    this.align = 0;
    this.lines = 0;
    this.inset = null; // ContentInset: [top, right, bottom, left]
    this.disabled = false;
    this.shadow = null;
    this.gradient = null;
    this.transform = null;
    this.transition = null;
    this.interceptBack = false;
    this.dir = 0; // its LayoutDirection prop
    this.dying = false;
  }
  listening(e) { return this.listens.has(e); }
}

export class Host {
  /**
   * container: the element the app fills (it follows the container's
   * size). options: { assets: the URL the app's assets are served from,
   * dev: under asuka dev, name: the app's }.
   */
  constructor(container, options = {}) {
    this.container = container;
    this.options = options;
    this.assets = (options.assets || "assets").replace(/\/$/, "");
    this.nodes = new Map();
    this.root = null;
    this.core = null;
    this.mem = null;
    this.io = null; // the shared buffer: Uint32Array and Float32Array views
    this.inGo = 1; // Go is running main until it says it started
    this.pending = []; // events waiting for Go to return
    this.later = []; // work waiting for Go to return
    this.accent = 0;
    this.scheme = 0; // ColorScheme: 0 the system's
    this.locale = "";
    this.rtl = false;
    this.nextCell = 0;
    this.envSent = null;
    this.fontScale = 1;
    this.natives = new Map(); // asuka.registerNative's factories, by kind
    this.fonts = options.fonts || []; // the app's font files (assets/fonts)
    container.classList.add("asuka");
    this.interaction = new Interaction(this);
    this.nav = new Navigation(this);
    this.windows = new Windows(this);
    this.modules = new Modules(this);
    this.menus = new Menus(this);
    this.motion = new Motion(this);
    this.measurer = document.createElement("div");
    this.measurer.className = "asuka-measurer";
    container.appendChild(this.measurer);
    this.watchEnvironment();
  }

  // --- Calls between JavaScript and Go ---

  /** connect: Go's entry points and the shared buffer's address. */
  connect(core, ioPtr) {
    this.core = core;
    this.ioPtr = ioPtr;
    // What waited for Go (worker.js: a notification's URL).
    const fs = this.onConnect || [];
    this.onConnect = null;
    for (const f of fs) f();
  }

  /** attach gives the host the WebAssembly memory (the loader does). */
  attach(memory) { this.mem = memory; }

  views() {
    if (!this.io || this.io.u32.buffer !== this.mem.buffer) {
      this.io = { u32: new Uint32Array(this.mem.buffer, this.ioPtr, 8), f32: new Float32Array(this.mem.buffer, this.ioPtr, 8) };
    }
    return this.io;
  }

  /** started: Go's main mounted the root and waits for the page's events. */
  started() {
    this.inGo = 0;
    this.drain();
  }

  /** call runs f, a call into Go, then what waited for it to return. */
  call(f) {
    if (this.inGo > 0) {
      // Go is on the stack: an event raised while it's calling us.
      this.pending.push(f);
      return undefined;
    }
    this.inGo++;
    let r;
    try {
      r = f();
    } finally {
      this.inGo--;
    }
    this.drain();
    return r;
  }

  drain() {
    while (this.inGo === 0 && (this.later.length || this.pending.length)) {
      if (this.later.length) {
        const fs = this.later;
        this.later = [];
        for (const f of fs) f();
        continue;
      }
      const f = this.pending.shift();
      this.inGo++;
      try { f(); } finally { this.inGo--; }
    }
  }

  /** afterCore runs f once Go has returned (now, if it isn't running). */
  afterCore(f) {
    if (this.inGo === 0) f();
    else this.later.push(f);
  }

  /** dispatch sends an encoded event to the core (once Go has returned). */
  dispatch(bytes) {
    if (!this.core) return;
    this.call(() => this.core.event(bytes));
  }

  /** emit sends the event if the core listens for it on n. */
  emit(n, kind, encode) {
    if (!n || !n.listening(kind)) return;
    this.dispatch(encode(n.id));
  }

  listCount(id) { return this.callNow(() => this.core.listCount(id)); }
  listBind(id, index, cell) { this.callNow(() => this.core.listBind(id, index, cell)); }
  listUnbind(id, cell) { this.callNow(() => this.core.listUnbind(id, cell)); }

  /** callNow calls into Go now: the caller made sure Go isn't running. */
  callNow(f) {
    this.inGo++;
    try { return f(); } finally { this.inGo--; }
  }

  /** post: Go's goroutines posted work; it runs as a task of its own. */
  post() {
    if (!this.postChannel) {
      this.postChannel = new MessageChannel();
      this.postChannel.port1.onmessage = () => this.call(() => this.core.runPosted());
    }
    this.postChannel.port2.postMessage(0);
  }

  /** savedState: asuka dev's signal values from the last build (null: not dev). */
  savedState() {
    if (!this.options.dev) return null;
    try {
      return sessionStorage.getItem(this.stateKey()) || "{}";
    } catch {
      return "{}";
    }
  }

  stateKey() { return "asuka.state." + (this.options.name || location.pathname); }

  /** saveState keeps the signals' values for the next build (asuka dev). */
  saveState() {
    if (!this.core || this.inGo) return;
    const s = this.callNow(() => this.core.saveState());
    try {
      if (s) sessionStorage.setItem(this.stateKey(), s);
    } catch {}
  }

  // --- Ops ---

  apply() {
    const { u32 } = this.views();
    // Copied out of Go's memory: a call back into Go meanwhile (a word,
    // a shader's GLSL) may grow it, which detaches the old buffer.
    const bytes = new Uint8Array(this.mem.buffer, u32[0], u32[1]).slice();
    this.motion.batchBegin();
    decode(bytes, this);
    this.motion.batchEnd();
  }

  create(id, kind) {
    const n = new Node(id, kind);
    const impl = kinds[kind];
    if (!impl) throw new Error(`asuka: the web host can't show a ${kindName(kind)} yet`);
    n.impl = impl;
    impl.create(this, n);
    if (n.el) {
      n.el.classList.add("asuka-view");
      n.el.asukaNode = n;
      if (!n.box) n.box = n.el;
    }
    this.nodes.set(id, n);
    return n;
  }

  /** hostNode makes a node of the host's own (a list's cell), with a host ID. */
  hostNode(kind, impl) {
    this.nextCell++;
    const n = new Node((HostIDBit | this.nextCell) >>> 0, kind);
    n.impl = impl;
    impl.create(this, n);
    n.el.classList.add("asuka-view");
    n.el.asukaNode = n;
    if (!n.box) n.box = n.el;
    this.nodes.set(n.id, n);
    return n;
  }

  destroy(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    this.nodes.delete(id);
    n.dying = true;
    this.interaction.forget(n);
    if (n.impl.destroy && n.impl.destroy(this, n)) return;
    for (const c of n.children) c.parent = null;
    const p = n.parent;
    if (p) {
      const i = p.children.indexOf(n);
      if (i >= 0) p.children.splice(i, 1);
      n.parent = null;
    }
    if (n.el) {
      // Inside a view going out (a popped screen sliding away, a view
      // leaving with its transition): it goes with that view.
      if (n.el.parentElement?.closest(".asuka-exiting")) return;
      if (this.motion.exit(n)) return; // goes out with its transition
      n.el.remove();
    }
  }

  insert(pid, cid, index) {
    const p = this.nodes.get(pid), c = this.nodes.get(cid);
    if (!p || !c) return;
    if (c.parent) this.removeChild(c.parent, c);
    const i = Math.min(index, p.children.length);
    p.children.splice(i, 0, c);
    c.parent = p;
    if (p.impl.insert && p.impl.insert(this, p, c, i)) return;
    this.placeChild(p, c, i);
  }

  /** placeChild puts c's element in p's, before the next child's. */
  placeChild(p, c, i) {
    if (!c.el || !p.box) return;
    let next = null;
    for (let k = i + 1; k < p.children.length; k++) {
      const e = p.children[k].el;
      if (e && e.parentNode === p.box) { next = e; break; }
    }
    p.box.insertBefore(c.el, next);
    this.motion.entering(c);
  }

  remove(pid, cid) {
    const p = this.nodes.get(pid), c = this.nodes.get(cid);
    if (p && c && c.parent === p) this.removeChild(p, c);
  }

  removeChild(p, c) {
    const i = p.children.indexOf(c);
    if (i >= 0) p.children.splice(i, 1);
    c.parent = null;
    if (p.impl.remove && p.impl.remove(this, p, c)) return;
    if (c.el && c.el.parentNode === p.box) c.el.remove();
  }

  setRoot(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    if (this.root && this.root !== n && this.root.el) this.root.el.remove();
    this.root = n;
    n.el.classList.add("asuka-root");
    this.container.appendChild(n.el);
    this.nav.rootChanged();
    this.sendEnvironment(true);
  }

  listen(id, e) {
    const n = this.nodes.get(id);
    if (!n) return;
    n.listens.add(e);
    if (e === Event.WindowState && this.windows.listen(n, e)) return;
    if (n.impl.listen && n.impl.listen(this, n, e)) return;
    this.interaction.listen(n, e);
  }

  unlisten(id, e) {
    const n = this.nodes.get(id);
    if (!n) return;
    n.listens.delete(e);
    this.interaction.unlisten(n, e);
  }

  setFrame(id, x, y, w, h) {
    const n = this.nodes.get(id);
    if (!n) return;
    if (n.impl.frame && n.impl.frame(this, n, x, y, w, h)) return;
    const p = n.parent;
    if (p?.impl.childFrame && p.impl.childFrame(this, p, n, x, y, w, h)) return;
    this.place(n, x, y, w, h);
  }

  /** place moves n's element to its frame, and what follows its size. */
  place(n, x, y, w, h) {
    const f = n.frame;
    const moved = f[0] !== x || f[1] !== y, sized = f[2] !== w || f[3] !== h;
    if (!moved && !sized && n.placed) return;
    const first = !n.placed;
    f[0] = x; f[1] = y; f[2] = w; f[3] = h;
    n.placed = true;
    if (!n.el) return;
    this.motion.frame(n, first, () => {
      const s = n.el.style;
      s.left = x + "px";
      s.top = y + "px";
      s.width = w + "px";
      s.height = h + "px";
    });
    if (sized) {
      if (n.gradient) this.applyGradient(n);
      if (n.impl.resized) n.impl.resized(this, n);
    }
  }

  command(id, cmd, args) {
    const n = this.nodes.get(id);
    if (!n) return;
    if (n.impl.command && n.impl.command(this, n, cmd, args)) return;
    this.rootCommand(n, cmd, args);
  }

  setProp(id, prop, type, v) {
    const n = this.nodes.get(id);
    if (!n) return;
    if (prop >= 64) {
      if (n.impl.prop) n.impl.prop(this, n, prop, v, type);
      return;
    }
    if (n.impl.common && n.impl.common(this, n, prop, v)) return;
    this.setCommon(n, prop, v);
  }

  setCommon(n, prop, v) {
    const el = n.el;
    if (!el) return;
    const s = el.style;
    switch (prop) {
      case Prop.Background:
        n.background = v;
        this.applyBackground(n);
        break;
      case Prop.CornerRadius:
        n.radius = v;
        s.borderRadius = v ? v + "px" : "";
        if (n.impl.radius) n.impl.radius(this, n);
        break;
      case Prop.BorderWidth:
        n.borderWidth = v;
        this.applyBorder(n);
        break;
      case Prop.BorderColor:
        n.borderColor = v;
        this.applyBorder(n);
        break;
      case Prop.Opacity:
        this.motion.style(n, "opacity", v === 1 ? "" : String(v));
        break;
      case Prop.Hidden:
        el.classList.toggle("asuka-hidden", !!v);
        if (v) el.setAttribute("inert", ""); else el.removeAttribute("inert");
        break;
      case Prop.Clip:
        el.classList.toggle("asuka-clip", !!v);
        break;
      case Prop.A11yLabel:
        if (v) el.setAttribute("aria-label", v); else el.removeAttribute("aria-label");
        if (n.impl.label) n.impl.label(this, n, v);
        break;
      case Prop.A11yHint:
        if (v) el.setAttribute("aria-description", v); else el.removeAttribute("aria-description");
        break;
      case Prop.A11yRole:
        setRole(el, v);
        break;
      case Prop.A11yHidden:
        if (v) el.setAttribute("aria-hidden", "true"); else el.removeAttribute("aria-hidden");
        break;
      case Prop.A11yActions:
        this.interaction.setActions(n, v);
        break;
      case Prop.A11yAdjustable:
        this.interaction.setAdjustable(n, v);
        break;
      case Prop.Color:
        n.color = v;
        if (n.impl.color) n.impl.color(this, n);
        else s.color = this.css(v);
        break;
      case Prop.TextStyle:
        n.font.style = v;
        this.fontChanged(n);
        break;
      case Prop.FontSize:
        n.font.size = v;
        this.fontChanged(n);
        break;
      case Prop.FontWeight:
        n.font.weight = v;
        this.fontChanged(n);
        break;
      case Prop.FontFamily:
        n.font.family = v;
        text.loadFamily(this, v);
        this.fontChanged(n);
        break;
      case Prop.TextAlign:
        n.align = v;
        if (n.impl.align) n.impl.align(this, n);
        else s.textAlign = text.align(v);
        break;
      case Prop.LineLimit:
        n.lines = v;
        if (n.impl.lines) n.impl.lines(this, n);
        break;
      case Prop.Disabled:
        n.disabled = !!v;
        if (n.impl.disabled) n.impl.disabled(this, n);
        else el.classList.toggle("asuka-disabled", !!v);
        if (v) el.setAttribute("aria-disabled", "true"); else el.removeAttribute("aria-disabled");
        break;
      case Prop.Menu:
        this.menus.set(n, v);
        break;
      case Prop.MenuTrigger:
        this.menus.trigger(n, v);
        break;
      case Prop.Transform:
        n.transform = v.length >= 20 ? readFloats(v, 5) : null;
        this.applyTransform(n);
        break;
      case Prop.Shadow:
        n.shadow = v.length >= 20 ? v.slice() : null;
        this.applyShadow(n);
        break;
      case Prop.Gradient:
        n.gradient = v.length >= 4 && new DataView(v.buffer, v.byteOffset).getUint32(0, true) > 0 ? v.slice() : null;
        this.applyGradient(n);
        break;
      case Prop.Transition:
        n.transition = v.length >= 13 ? v.slice() : null;
        break;
      case Prop.Material:
        el.dataset.material = v ? String(v) : "";
        if (!v) delete el.dataset.material;
        break;
      case Prop.SwipeActions:
      case Prop.LeadingSwipeActions:
      case Prop.Selected:
        if (n.cell) n.cell.prop(this, n, prop, v);
        break;
      case Prop.LayoutDirection:
        n.dir = v;
        el.dir = v === 1 ? "ltr" : v === 2 ? "rtl" : "";
        if (n.impl.direction) n.impl.direction(this, n);
        break;
      case Prop.ContentInset:
        n.inset = v;
        if (n.impl.inset) n.impl.inset(this, n);
        else s.padding = `${v[0]}px ${v[1]}px ${v[2]}px ${v[3]}px`;
        break;
      case Prop.Match:
        this.motion.match(n, v);
        break;
      case Prop.InterceptBack:
        n.interceptBack = !!v;
        break;
      case Prop.Tooltip:
        if (v) el.title = v; else el.removeAttribute("title");
        break;
    }
  }

  // --- Visual props ---

  /** css is a color's CSS value ("" for the platform's default). */
  css(c) { return color.css(this, c); }

  applyBackground(n) {
    if (n.impl.background && n.impl.background(this, n)) return;
    this.motion.style(n, "backgroundColor", this.css(n.background));
  }

  applyBorder(n) {
    // Drawn inside the frame, over the content, as a layer's border:
    // children are placed from the frame's corner, not inside a border.
    const s = n.el.style;
    if (n.borderWidth > 0) {
      s.outline = `${n.borderWidth}px solid ${this.css(n.borderColor) || "var(--asuka-separator)"}`;
      s.outlineOffset = -n.borderWidth + "px";
    } else {
      s.outline = "";
      s.outlineOffset = "";
    }
  }

  applyShadow(n) {
    const b = n.shadow;
    const s = n.el.style;
    if (!b) { s.boxShadow = ""; return; }
    const r = new Reader(b);
    const c = r.u64(), blur = r.f32(), x = r.f32(), y = r.f32();
    s.boxShadow = c ? `${x}px ${y}px ${blur}px ${this.css(c)}` : "";
  }

  applyTransform(n) {
    const t = n.transform;
    const v = t ? `translate(${t[0]}px, ${t[1]}px) rotate(${t[4]}deg) scale(${t[2]}, ${t[3]})` : "";
    this.motion.style(n, "transform", v);
  }

  /** applyGradient draws a gradient between two points of the box (unit coordinates) with CSS's angle and stops. */
  applyGradient(n) {
    const s = n.el.style;
    const b = n.gradient;
    if (!b) { s.backgroundImage = ""; return; }
    const r = new Reader(b);
    const count = r.u32();
    const stops = [];
    for (let i = 0; i < count; i++) stops.push([r.u64(), r.f32()]);
    const sx = r.f32(), sy = r.f32(), ex = r.f32(), ey = r.f32();
    const w = n.frame[2] || 1, h = n.frame[3] || 1;
    const dx = (ex - sx) * w, dy = (ey - sy) * h;
    const len = Math.hypot(dx, dy) || 1;
    const ux = dx / len, uy = dy / len;
    // CSS: 0deg points up, clockwise; the line goes through the center,
    // as long as the box's corners need.
    const angle = Math.atan2(ux, -uy);
    const L = Math.abs(w * Math.sin(angle)) + Math.abs(h * Math.cos(angle)) || 1;
    const t = (px, py) => ((px * w - w / 2) * ux + (py * h - h / 2) * uy) / L + 0.5;
    const t0 = t(sx, sy), t1 = t(ex, ey);
    const list = stops.map(([c, at]) => `${this.css(c) || "transparent"} ${((t0 + at * (t1 - t0)) * 100).toFixed(3)}%`);
    s.backgroundImage = `linear-gradient(${(angle * 180 / Math.PI).toFixed(3)}deg, ${list.join(", ")})`;
  }

  fontChanged(n) {
    if (n.impl.font) n.impl.font(this, n);
    else text.applyFont(this, n, n.el);
  }

  // --- Measuring ---

  /** measure: Go asks the size of a view's content within constraints. */
  measure() {
    const { u32, f32 } = this.views();
    const n = this.nodes.get(u32[2]);
    const c = { width: f32[3], height: f32[4], availW: f32[5], availH: f32[6] };
    let size = [0, 0];
    if (n && n.impl.measure) size = n.impl.measure(this, n, c);
    let [w, h] = size;
    if (c.width === c.width) w = c.width;
    if (c.height === c.height) h = c.height;
    const k = window.devicePixelRatio || 1;
    f32[3] = Math.ceil(w * k - 0.001) / k;
    f32[4] = Math.ceil(h * k - 0.001) / k;
  }

  /**
   * measureElement measures el's content as the browser lays it out
   * within the constraints: the known width, the available width (a
   * definite one wraps; max-content doesn't; min-content is the widest
   * word). Its own size styles are restored after.
   */
  measureElement(n, el, c, opts = {}) {
    return this.measurable(el, () => this.measureNow(n, el, c, opts));
  }

  /**
   * measurable runs f with el laid out: an element not in the page, or in
   * a part of it that isn't displayed (a recycled list row), is measured
   * in the measurer, then put back where it was.
   */
  measurable(el, f) {
    if (el.isConnected && el.getClientRects().length) return f();
    const mark = el.parentNode ? document.createComment("") : null;
    if (mark) el.parentNode.replaceChild(mark, el);
    this.measurer.appendChild(el);
    try {
      return f();
    } finally {
      if (mark) mark.parentNode.replaceChild(el, mark);
      else el.remove();
    }
  }

  measureNow(n, el, c, opts) {
    const s = el.style;
    const saved = [s.width, s.height, s.maxWidth, s.left, s.top];
    s.maxWidth = "none";
    s.height = "auto";
    if (c.width === c.width) s.width = c.width + "px";
    else if (c.availW === Infinity) s.width = "max-content";
    else if (c.availW === -Infinity) s.width = "min-content";
    else {
      s.width = "max-content";
      if (el.getBoundingClientRect().width > c.availW) s.width = Math.max(c.availW, 0) + "px";
    }
    let r = el.getBoundingClientRect();
    let w = r.width, h = r.height;
    if (opts.tight && c.width !== c.width && c.availW === c.availW && Math.abs(c.availW) !== Infinity && w >= c.availW - 0.5) {
      // Wrapped: as wide as its widest line, as UIKit and Android measure.
      const tw = text.linesWidth(el);
      if (tw > 0) w = Math.min(w, tw + text.horizontalInset(n));
    }
    s.width = saved[0]; s.height = saved[1]; s.maxWidth = saved[2];
    return [w, h];
  }

  // --- Commands to the root ---

  rootCommand(n, cmd, args) {
    switch (cmd) {
      case Command.Accent: {
        const r = new Reader(args);
        this.accent = r.u64();
        color.setAccent(this, this.accent);
        break;
      }
      case Command.ColorScheme:
        this.scheme = args[0] || 0;
        color.applyScheme(this);
        this.sendEnvironment();
        break;
      case Command.Locale: {
        this.rtl = (args[0] & 1) !== 0;
        this.locale = new TextDecoder().decode(args.subarray(1));
        this.container.dir = this.rtl ? "rtl" : "ltr";
        this.container.lang = this.locale;
        document.documentElement.lang = this.locale;
        this.nav.directionChanged();
        break;
      }
      case Command.SaveLocale:
        try {
          const tag = new TextDecoder().decode(args);
          if (tag) localStorage.setItem(this.prefix() + "locale", tag);
          else localStorage.removeItem(this.prefix() + "locale");
        } catch {}
        break;
      case Command.WindowOptions: {
        const c = decodeWindowConfig(args);
        this.windowTitle = c.title;
        this.nav.updateTitle();
        // Of the main window's options, a tab takes its title and its
        // background (window.js: what else a page can't do).
        if (c.background) this.container.style.background = color.css(this, c.background);
        break;
      }
      case Command.WindowAction:
        this.windows.action(n, args);
        break;
      case Command.WindowTitle:
        this.windowTitle = new TextDecoder().decode(args);
        this.nav.updateTitle();
        break;
      case Command.Haptic:
        if (navigator.vibrate) navigator.vibrate([10, 15, 20, 30, 15, 25, 40][args[0]] || 10);
        break;
      case Command.AnimateBegin:
        this.motion.begin(args);
        break;
      case Command.AnimateEnd:
        this.motion.end();
        break;
      case Command.OpenURL:
        window.open(new TextDecoder().decode(args), "_blank", "noopener");
        break;
      case Command.Copy:
        navigator.clipboard?.writeText(new TextDecoder().decode(args)).catch(() => {});
        break;
      case Command.Share: {
        const t = new TextDecoder().decode(args);
        const url = /^https?:\/\/\S+$/.test(t) ? t : undefined;
        if (navigator.share) navigator.share(url ? { url } : { text: t }).catch(() => {});
        else navigator.clipboard?.writeText(t).catch(() => {});
        break;
      }
      case Command.Announce:
        this.announce(new TextDecoder().decode(args));
        break;
      case Command.Frames:
        this.motion.frames(args[0] !== 0, n);
        break;
      case Command.Call:
        this.modules.call(n, args);
        break;
      case Command.MenuBar:
        this.menus.menuBar(n, args);
        break;
      case Command.BackExit:
        this.nav.backExit();
        break;
      case Command.A11yFocus:
        if (n.el) { if (!n.el.hasAttribute("tabindex")) n.el.tabIndex = -1; n.el.focus({ preventScroll: true }); }
        break;
      case Command.Focus:
        (n.focusEl || n.el)?.focus({ preventScroll: true });
        break;
      case Command.Blur:
        (n.focusEl || n.el)?.blur();
        break;
      case Command.Present:
        this.nav.present(n);
        break;
      case Command.ScrollToOffset:
      case Command.ScrollTo:
        break;
    }
  }

  announce(msg) {
    if (!this.live) {
      this.live = document.createElement("div");
      this.live.className = "asuka-live";
      this.live.setAttribute("aria-live", "polite");
      this.container.appendChild(this.live);
    }
    this.live.textContent = "";
    setTimeout(() => { this.live.textContent = msg; }, 50);
  }

  prefix() { return "asuka." + (this.options.name || "") + "."; }

  /** schemeChanged: light or dark changed; what resolved colors itself draws again. */
  schemeChanged() { redrawCanvases(this); }

  /** fontFiles are the app's font files a FontFamily names: the file itself, or a family's. */
  fontFiles(name) {
    return this.fonts.filter((f) => {
      const base = f.replace(/\.[^.]+$/, "");
      return base === name || base.startsWith(name + "-");
    });
  }

  // --- Environment ---

  watchEnvironment() {
    const send = () => this.sendEnvironment();
    new ResizeObserver(send).observe(this.container);
    for (const q of ["(prefers-color-scheme: dark)", "(prefers-reduced-motion: reduce)"]) {
      matchMedia(q).addEventListener("change", () => { color.applyScheme(this); send(); });
    }
    window.visualViewport?.addEventListener("resize", send);
    let ratio = matchMedia(`(resolution: ${devicePixelRatio}dppx)`);
    const onRatio = () => {
      ratio = matchMedia(`(resolution: ${devicePixelRatio}dppx)`);
      ratio.addEventListener("change", onRatio, { once: true });
      send();
    };
    ratio.addEventListener("change", onRatio, { once: true });
    document.addEventListener("visibilitychange", () => {
      if (this.root) this.dispatch(Events.lifecycle(this.root.id, document.hidden ? 1 : 0));
    });
    color.applyScheme(this);
  }

  /** environment is the root's, encoded (Go reads it as the app mounts). */
  environment() { return Events.environment(0, this.env()); }

  env() {
    const e = newEnvironment();
    const r = this.container.getBoundingClientRect();
    e.width = r.width;
    e.height = r.height;
    const safe = this.safeArea();
    e.safeTop = safe[0]; e.safeRight = safe[1]; e.safeBottom = safe[2]; e.safeLeft = safe[3];
    const vv = window.visualViewport;
    if (vv && this.container === this.fullPage()) {
      e.keyboard = Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop));
    }
    e.scale = window.devicePixelRatio || 1;
    this.fontScale = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
    e.fontScale = this.fontScale;
    e.dark = color.isDark(this);
    e.reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const lang = navigator.languages?.length ? navigator.languages : [navigator.language || "en-US"];
    e.locale = lang[0];
    e.languages = lang.join(",");
    try {
      e.timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
      const loc = new Intl.Locale(lang[0]);
      const week = loc.getWeekInfo?.() || loc.weekInfo;
      if (week) e.firstWeekday = (week.firstDay % 7) + 1;
      const hc = new Intl.DateTimeFormat(lang[0], { hour: "numeric" }).resolvedOptions().hourCycle;
      if (hc) e.hourCycle = hc === "h23" || hc === "h24" ? 24 : 12;
    } catch {}
    try {
      const saved = localStorage.getItem(this.prefix() + "locale");
      if (saved) e.languages = saved + "," + e.languages;
    } catch {}
    return e;
  }

  /** fullPage is the container when the app fills the page. */
  fullPage() { return this.options.fullPage ? this.container : null; }

  safeArea() {
    if (!this.safeProbe) {
      this.safeProbe = document.createElement("div");
      this.safeProbe.className = "asuka-safe-probe";
      this.container.appendChild(this.safeProbe);
    }
    if (!this.options.fullPage) return [0, 0, 0, 0];
    const cs = getComputedStyle(this.safeProbe);
    return [cs.paddingTop, cs.paddingRight, cs.paddingBottom, cs.paddingLeft].map((v) => parseFloat(v) || 0);
  }

  sendEnvironment(force) {
    if (!this.root) return;
    const e = this.env();
    const key = JSON.stringify(e);
    if (!force && key === this.envSent) {
      this.nav.layout();
      return;
    }
    this.envSent = key;
    this.dispatch(Events.environment(this.root.id, e));
    this.nav.layout();
  }
}

function kindName(k) {
  for (const [name, v] of Object.entries(Kind)) if (v === k) return name;
  return "Kind(" + k + ")";
}

const roles = [null, "button", "heading", "img", "link", null, "searchbox"];

function setRole(el, r) {
  const role = roles[r];
  if (role) el.setAttribute("role", role);
  else if (el.dataset.ownRole === undefined) el.removeAttribute("role");
  if (r === 2) el.setAttribute("aria-level", "2");
  if (r === 1 && !el.hasAttribute("tabindex")) el.tabIndex = 0;
}

export function readFloats(b, count) {
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const out = [];
  for (let i = 0; i < count; i++) out.push(v.getFloat32(i * 4, true));
  return out;
}

/** decodeStrings reads a u32 count, then u32-length strings. */
export function decodeStrings(b) {
  if (!b || b.length < 4) return [];
  const r = new Reader(b);
  const n = r.u32();
  const out = [];
  for (let i = 0; i < n && !r.atEnd; i++) out.push(r.string());
  return out;
}

/** decodeActions reads a Menu: per item a role, a title and an icon. */
export function decodeActions(b) {
  if (!b || b.length < 4) return [];
  const r = new Reader(b);
  const n = r.u32();
  const out = [];
  for (let i = 0; i < n && !r.atEnd; i++) out.push({ role: r.u8(), title: r.string(), icon: r.string() });
  return out;
}

export { Kind, Prop, Event, Command, Events, Type };

// Motion: ui.Animate's batches (frames and visual props animate with the
// Web Animations API), enter and exit transitions (Style.Transition),
// matched views (Style.Match) and the Frames clock (ui.OnFrame).

import { Reader, Events } from "./ops.js";

const curves = ["ease-in-out", "linear", "ease-in", "ease-out"];

export class Motion {
  constructor(h) {
    this.h = h;
    this.anim = null; // the animation the batch's changes take, between AnimateBegin and AnimateEnd
    this.running = new Map(); // el -> Map(prop -> Animation): a repeating one stops when its prop is set again
    this.gone = new Map(); // a Match key -> the rect of the view going out with it, this batch
    this.come = new Map(); // a Match key -> the node coming in with it
    this.ticking = false;
  }

  begin(args) {
    const r = new Reader(args);
    const duration = r.f32() * 1000, curve = r.u8(), repeat = r.i32(), reverse = r.u8() !== 0;
    const delay = r.atEnd ? 0 : r.f32() * 1000;
    const damping = r.atEnd ? 1 : r.f32();
    const velocity = r.atEnd ? 0 : r.f32();
    if (this.h.envSent && JSON.parse(this.h.envSent).reduceMotion) {
      this.anim = null;
      return;
    }
    this.anim = {
      duration: Math.max(duration, 1),
      easing: curve === 4 ? spring(damping, velocity, duration / 1000) : curves[curve] || "ease-in-out",
      iterations: repeat < 0 ? Infinity : repeat + 1,
      direction: reverse ? "alternate" : "normal",
      delay,
      fill: "backwards",
    };
  }

  end() { this.anim = null; }

  batchBegin() {}

  batchEnd() {
    if (this.come.size) this.matchViews();
    this.gone.clear();
    this.come.clear();
  }

  /** style sets a visual prop, animated in an animated batch. */
  style(n, prop, value) {
    const el = n.el;
    const runs = this.running.get(el);
    const old = runs?.get(prop);
    if (old) { old.cancel(); runs.delete(prop); }
    if (!this.anim || !n.placed) {
      el.style[prop] = value;
      return;
    }
    const from = getComputedStyle(el)[prop];
    el.style[prop] = value;
    const to = getComputedStyle(el)[prop];
    if (from === to) return;
    this.run(el, prop, [{ [prop]: from }, { [prop]: to }]);
  }

  run(el, prop, frames) {
    const a = el.animate(frames, this.anim);
    let runs = this.running.get(el);
    if (!runs) { runs = new Map(); this.running.set(el, runs); }
    runs.set(prop, a);
    a.onfinish = a.oncancel = () => {
      if (runs.get(prop) === a) runs.delete(prop);
      if (!runs.size) this.running.delete(el);
    };
  }

  /** frame moves n to its frame (set), animated in an animated batch; never its first frame, which wasn't on screen. */
  frame(n, first, set) {
    const el = n.el;
    if (!this.anim || first || !el.isConnected) {
      set();
      if (first && n.entering) this.enter(n);
      return;
    }
    const s = el.style;
    const from = { left: s.left, top: s.top, width: s.width, height: s.height };
    set();
    const to = { left: s.left, top: s.top, width: s.width, height: s.height };
    const runs = this.running.get(el);
    const old = runs?.get("frame");
    if (old) old.cancel();
    this.run(el, "frame", [from, to]);
  }

  /** entering notes a view inserted in an animated batch: it comes in from its transition's state. */
  entering(n) {
    if (this.anim && n.transition && !n.placed) n.entering = true;
    if (this.anim && n.match) this.come.set(n.match, n);
  }

  enter(n) {
    n.entering = false;
    const t = transition(n.transition);
    if (!t) return;
    this.run(n.el, "transition", [t, { opacity: getComputedStyle(n.el).opacity, transform: n.el.style.transform || "none" }]);
  }

  /** exit keeps a destroyed view on screen as it goes out to its transition's state (or to the view matching it); true if it does. */
  exit(n) {
    const el = n.el;
    if (!this.anim || !el.isConnected) return false;
    if (n.match) {
      const r = el.getBoundingClientRect();
      this.gone.set(n.match, { el, rect: r });
      el.classList.add("asuka-exiting");
      return true; // matchViews takes it out
    }
    const t = transition(n.transition);
    if (!t) return false;
    el.setAttribute("inert", "");
    el.classList.add("asuka-exiting");
    el.style.pointerEvents = "none";
    const a = el.animate([{ opacity: getComputedStyle(el).opacity, transform: el.style.transform || "none" }, t], { ...this.anim, fill: "forwards", iterations: 1 });
    a.onfinish = a.oncancel = () => el.remove();
    return true;
  }

  match(n, key) { n.match = key || ""; }

  /** matchViews moves each view that came in with a Match key from where the one that went out was, and that one to it, fading. */
  matchViews() {
    for (const [key, n] of this.come) {
      const g = this.gone.get(key);
      if (!g || !n.el.isConnected) continue;
      const to = n.el.getBoundingClientRect();
      const from = g.rect;
      const fx = from.left - to.left, fy = from.top - to.top;
      const sx = from.width / (to.width || 1), sy = from.height / (to.height || 1);
      n.el.style.transformOrigin = "0 0";
      const opts = { ...this.anim, iterations: 1 };
      n.el.animate([{ transform: `translate(${fx}px, ${fy}px) scale(${sx}, ${sy})` }, { transform: "none" }], opts);
      const ghost = g.el;
      ghost.style.pointerEvents = "none";
      ghost.style.transformOrigin = "0 0";
      const a = ghost.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translate(${-fx}px, ${-fy}px) scale(${1 / sx}, ${1 / sy})` }], { ...opts, fill: "forwards" });
      a.onfinish = a.oncancel = () => ghost.remove();
      this.gone.delete(key);
    }
    for (const g of this.gone.values()) g.el.remove();
  }

  /** frames sends Frame to the root at each frame the display draws, while on. */
  frames(on, n) {
    this.frameRoot = n;
    if (on === this.ticking) return;
    this.ticking = on;
    if (!on) return;
    const tick = (t) => {
      if (!this.ticking) return;
      const root = this.frameRoot;
      if (root) this.h.dispatch(Events.frame(root.id, t / 1000));
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }
}

/** transition is a Transition's state as a keyframe (null: none). */
function transition(b) {
  if (!b || b.length < 13) return null;
  const r = new Reader(b);
  const fade = r.u8() !== 0, scale = r.f32(), x = r.f32(), y = r.f32();
  const k = { transform: `translate(${x}px, ${y}px)${scale ? ` scale(${scale})` : ""}` };
  if (fade) k.opacity = 0;
  return k;
}

/**
 * spring is a damped spring's curve as CSS's linear() easing, sampled
 * over the animation's duration: damping ratio ζ (1: no overshoot) and
 * the initial velocity (the change's fraction per second).
 */
export function spring(zeta, v0, duration) {
  zeta = Math.max(0.01, zeta || 1);
  const T = Math.max(duration, 0.05);
  // ω so the spring settles (within 0.1%) by the end of the duration.
  const w = Math.min(Math.max(-Math.log(0.001) / (zeta * T), 1), 200);
  const x = (t) => {
    // Displacement from the target, starting at 1, velocity -v0.
    if (zeta < 1) {
      const wd = w * Math.sqrt(1 - zeta * zeta);
      return Math.exp(-zeta * w * t) * (Math.cos(wd * t) + ((zeta * w - v0) / wd) * Math.sin(wd * t));
    }
    return Math.exp(-w * t) * (1 + (w - v0) * t);
  };
  const steps = 60;
  const pts = [];
  for (let i = 0; i <= steps; i++) pts.push((1 - x((i / steps) * T)).toFixed(4));
  pts[steps] = "1";
  return `linear(${pts.join(", ")})`;
}

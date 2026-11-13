// maps.View in the browser: the native view "asuka.map", a map the host
// draws of OpenStreetMap's tiles, as the Windows host does (and GNOME
// Maps' libshumate on Linux): <img> tiles placed in the view, the pins,
// a callout, the user's location.
//
// The tiles come from tile.openstreetmap.org (the start option mapTiles:
// another server's URL, with {z}, {x} and {y}) as its usage policy asks
// of a web page: the browser sends its Referer and keeps the tiles as the
// server says (its HTTP cache), only what's on screen is asked for (a
// tile that leaves the view before it comes is let go), and the
// attribution "© OpenStreetMap contributors", linking to its copyright
// page, is shown on the map. While a tile comes, one above it that's
// here is drawn bigger.
//
// Props: region and markers (JSON), type (Satellite and Hybrid have no
// free imagery source: they show the streets; Muted shows them in
// quieter colors), showsUser (the user's location, once the page has the
// location permission: asuka/sdk/location asks for it), interactive. The user
// moves it with a drag, zooms with the wheel (a trackpad's pinch comes as
// Ctrl and the wheel), a finger's pinch, a double click (Shift: out), and
// + and -; the arrow keys move it. Events: region (lat, lng, latDelta,
// lngDelta, once the user has moved it), marker (id: a click on a pin,
// which shows its title and subtitle in a callout above it), press (lat,
// lng: a click off the markers), error (message: tiles that can't be
// had). It has no size of its own: its style gives it one.

const tileSize = 256;
const maxZoom = 19;
const retryAfter = 30_000; // a tile that failed is asked for again after this
const osm = "https://tile.openstreetmap.org/{z}/{x}/{y}.png";
const blank = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==";

// What the page knows of each tile's URL: "loaded" (in the browser's
// cache now), or when it failed.
const tiles = new Map();

export function mercY(lat) {
  lat = (Math.max(Math.min(lat, 85.05112878), -85.05112878) * Math.PI) / 180;
  return (1 - Math.log(Math.tan(lat) + 1 / Math.cos(lat)) / Math.PI) / 2;
}

export function mercLat(y) {
  return (Math.atan(Math.sinh(Math.PI * (1 - 2 * y))) * 180) / Math.PI;
}

/** zoomFor is the zoom that shows region r in a view w×h points wide (at most maxZoom). */
export function zoomFor(r, w, h) {
  let z = maxZoom;
  if (r.lngDelta > 0) z = Math.min(z, Math.log2((w * 360) / (tileSize * r.lngDelta)));
  if (r.latDelta > 0) {
    const span = Math.abs(mercY(r.center.lat + r.latDelta / 2) - mercY(r.center.lat - r.latDelta / 2));
    if (span > 0) z = Math.min(z, Math.log2(h / (tileSize * span)));
  }
  return Math.max(z, 0);
}

/** tileURL is a tile's URL from a template ({z}, {x}, {y}). */
export function tileURL(template, z, x, y) {
  return template.replace("{z}", z).replace("{x}", x).replace("{y}", y);
}

/** mapView is asuka.map's native view: ctx as asuka.registerNative's. */
export function mapView(ctx) {
  return new MapView(ctx).api();
}

class MapView {
  constructor(ctx) {
    this.ctx = ctx;
    this.template = ctx.host?.options?.mapTiles || osm;
    // The view: its center in the world's unit square (Web Mercator), its
    // zoom (the world is tileSize·2^zoom points wide).
    this.cx = 0.5;
    this.cy = 0.5;
    this.zoom = 1;
    this.w = 0;
    this.h = 0;
    this.pending = null; // a region waiting for the view's size
    this.shown = false; // a region was shown: the next one animates there
    this.markers = [];
    this.selected = "";
    this.interactive = true;
    this.showsUser = false;
    this.user = null; // { lat, lng, acc }
    this.watch = null;
    this.moved = false; // the user moved it since the last report
    this.failed = false;
    this.alive = true;
    this.frame = 0;
    this.anim = null;
    this.wheelTimer = 0;
    this.pointers = new Map(); // id -> { x, y }
    this.drag = null;
    this.suppressClick = false;

    const el = (this.el = document.createElement("div"));
    el.className = "asuka-map";
    el.tabIndex = 0;
    el.setAttribute("role", "application");
    el.setAttribute("aria-roledescription", "map");
    this.layer = div("asuka-map-tiles");
    this.pins = div("asuka-map-pins");
    this.attribution = document.createElement("a");
    this.attribution.className = "asuka-map-attribution";
    this.attribution.href = "https://www.openstreetmap.org/copyright";
    this.attribution.target = "_blank";
    this.attribution.rel = "noopener";
    this.attribution.textContent = "© OpenStreetMap contributors";
    el.append(this.layer, this.pins, this.attribution);
    this.tileEls = new Map(); // key -> { el, img, url, fallback }

    this.resize = new ResizeObserver(() => this.sized());
    this.resize.observe(el);
    this.listen();
  }

  api() {
    return {
      el: this.el,
      set: (name, value) => this.set(name, value),
      destroy: () => this.destroy(),
    };
  }

  // --- Props ---

  set(name, value) {
    switch (name) {
      case "region": {
        let r;
        try {
          r = JSON.parse(value);
        } catch {
          return;
        }
        if (!r?.center) return;
        this.pending = r;
        this.applyRegion();
        break;
      }
      case "markers": {
        let ms = [];
        try {
          ms = JSON.parse(value) || [];
        } catch {}
        this.markers = Array.isArray(ms) ? ms : [];
        if (!this.markers.some((m) => m.id === this.selected)) this.selected = "";
        this.drawPins();
        break;
      }
      case "type":
        // Satellite and Hybrid: no free imagery; Muted: quieter streets.
        this.el.classList.toggle("asuka-map-muted", Number(value) === 3);
        break;
      case "showsUser":
        this.showsUser = !!value;
        this.followUser();
        break;
      case "interactive":
        this.interactive = !!value;
        this.el.tabIndex = this.interactive ? 0 : -1;
        this.el.classList.toggle("asuka-map-static", !this.interactive);
        break;
    }
  }

  sized() {
    const w = this.el.clientWidth, h = this.el.clientHeight;
    if (w === this.w && h === this.h) return;
    this.w = w;
    this.h = h;
    if (this.pending) this.applyRegion();
    else {
      this.clamp();
      this.draw();
    }
  }

  /** applyRegion shows the pending region once the view has a size: animated there if it showed one before. */
  applyRegion() {
    const r = this.pending;
    if (!r || this.w <= 0 || this.h <= 0) return;
    this.pending = null;
    const to = {
      cx: (r.center.lng + 180) / 360,
      cy: mercY(r.center.lat),
      zoom: zoomFor(r, this.w, this.h),
    };
    if (!this.shown || matchMedia("(prefers-reduced-motion: reduce)").matches) {
      this.shown = true;
      this.stopAnim();
      Object.assign(this, to);
      this.clamp();
      this.draw();
      return;
    }
    const from = { cx: this.cx, cy: this.cy, zoom: this.zoom };
    // The short way round the world.
    let dx = to.cx - from.cx;
    dx -= Math.round(dx);
    this.animate(300, (f) => {
      this.cx = from.cx + dx * f;
      this.cy = from.cy + (to.cy - from.cy) * f;
      this.zoom = from.zoom + (to.zoom - from.zoom) * f;
      this.clamp();
    });
  }

  // --- Geometry ---

  world() { return tileSize * 2 ** this.zoom; }

  /** clamp keeps the zoom in range and the poles out of view; the world wraps east and west. */
  clamp() {
    const least = this.h > 0 ? Math.max(Math.log2(this.h / tileSize), 0) : 0;
    this.zoom = Math.max(Math.min(this.zoom, maxZoom), least);
    this.cx -= Math.floor(this.cx);
    const half = this.h / 2 / this.world();
    this.cy = Math.max(Math.min(this.cy, 1 - half), half);
  }

  region() {
    const W = this.world();
    const top = mercLat(this.cy - this.h / 2 / W), bottom = mercLat(this.cy + this.h / 2 / W);
    return {
      lat: mercLat(this.cy), lng: this.cx * 360 - 180,
      latDelta: Math.abs(top - bottom), lngDelta: Math.min((this.w / W) * 360, 360),
    };
  }

  /** at is the place at a point of the view. */
  at(x, y) {
    const W = this.world();
    let px = this.cx + (x - this.w / 2) / W;
    const py = this.cy + (y - this.h / 2) / W;
    px -= Math.floor(px);
    return { lat: mercLat(py), lng: px * 360 - 180 };
  }

  /** point is where a place is in the view: the copy of the world nearest the center. */
  point(lat, lng) {
    const W = this.world();
    let dx = (lng + 180) / 360 - this.cx;
    dx -= Math.round(dx);
    return [this.w / 2 + dx * W, this.h / 2 + (mercY(lat) - this.cy) * W];
  }

  zoomBy(dz, x, y) {
    const ox = x - this.w / 2, oy = y - this.h / 2;
    const W0 = this.world();
    const px = this.cx + ox / W0, py = this.cy + oy / W0;
    this.zoom += dz;
    this.clamp();
    const W = this.world();
    this.cx = px - ox / W;
    this.cy = py - oy / W;
    this.clamp();
    this.moved = true;
    this.draw();
  }

  panBy(dx, dy) {
    const W = this.world();
    this.cx -= dx / W;
    this.cy -= dy / W;
    this.clamp();
    this.moved = true;
    this.draw();
  }

  report() {
    if (!this.moved || !this.alive) return;
    this.moved = false;
    this.ctx.emit("region", this.region());
  }

  // --- Drawing ---

  draw() {
    if (this.frame || !this.alive) return;
    this.frame = requestAnimationFrame(() => {
      this.frame = 0;
      this.drawTiles();
      this.drawPins();
    });
  }

  /**
   * drawTiles places the tiles of the zoom level nearest the view's at the
   * screen's pixels (a 2× display: the next level's), each scaled to fit.
   */
  drawTiles() {
    if (this.w <= 0 || this.h <= 0) return;
    const k = devicePixelRatio || 1;
    const z = Math.max(0, Math.min(Math.round(this.zoom + Math.log2(k)), maxZoom));
    const n = 2 ** z;
    const W = this.world();
    const tw = W / n;
    const ox = this.cx * W - this.w / 2, oy = this.cy * W - this.h / 2;
    const tx0 = Math.floor(ox / tw), tx1 = Math.floor((ox + this.w - 1) / tw);
    const ty0 = Math.max(Math.floor(oy / tw), 0), ty1 = Math.min(Math.floor((oy + this.h - 1) / tw), n - 1);
    const keep = new Set();
    const now = Date.now();
    for (let ty = ty0; ty <= ty1; ty++) {
      for (let tx = tx0; tx <= tx1; tx++) {
        const key = `${z}/${tx}/${ty}`;
        keep.add(key);
        const wx = ((tx % n) + n) % n;
        let t = this.tileEls.get(key);
        if (!t) {
          t = this.tile(z, wx, ty, now);
          this.tileEls.set(key, t);
          this.layer.appendChild(t.el);
        } else if (t.failed && now - t.failed > retryAfter) {
          t.failed = 0;
          t.img.src = t.url;
        }
        const x0 = Math.floor(tx * tw - ox), x1 = Math.floor((tx + 1) * tw - ox);
        const y0 = Math.floor(ty * tw - oy), y1 = Math.floor((ty + 1) * tw - oy);
        const s = t.el.style;
        s.left = x0 + "px";
        s.top = y0 + "px";
        s.width = x1 - x0 + "px";
        s.height = y1 - y0 + "px";
      }
    }
    for (const [key, t] of this.tileEls) {
      if (keep.has(key)) continue;
      // One that hasn't come is let go: the browser stops asking.
      if (tiles.get(t.url) !== "loaded") t.img.src = blank;
      t.el.remove();
      this.tileEls.delete(key);
    }
  }

  /** tile makes a tile's element: its image, and while it comes one above it that's here. */
  tile(z, x, y, now) {
    const el = div("asuka-map-tile");
    const url = tileURL(this.template, z, x, y);
    const t = { el, url, img: document.createElement("img"), fallback: null, failed: 0 };
    const state = tiles.get(url);
    if (state !== "loaded") {
      for (let dz = 1; dz <= Math.min(z, 6); dz++) {
        const purl = tileURL(this.template, z - dz, x >> dz, y >> dz);
        if (tiles.get(purl) !== "loaded") continue;
        const f = (t.fallback = document.createElement("img"));
        f.alt = "";
        f.draggable = false;
        const parts = 2 ** dz;
        f.className = "asuka-map-fallback";
        f.style.width = f.style.height = parts * 100 + "%";
        f.style.left = -(x & (parts - 1)) * 100 + "%";
        f.style.top = -(y & (parts - 1)) * 100 + "%";
        f.src = purl;
        el.appendChild(f);
        break;
      }
    }
    const img = t.img;
    img.alt = "";
    img.draggable = false;
    img.decoding = "async";
    img.className = "asuka-map-img";
    img.onload = () => {
      if (img.src === blank) return;
      tiles.set(url, "loaded");
      img.classList.add("asuka-map-loaded");
      t.fallback?.remove();
      t.fallback = null;
    };
    img.onerror = () => {
      if (img.src === blank) return;
      t.failed = Date.now();
      tiles.set(url, t.failed);
      this.tileFailed();
    };
    const failedAt = typeof state === "number" ? state : 0;
    if (failedAt && now - failedAt < retryAfter) t.failed = failedAt;
    else img.src = url;
    el.appendChild(img);
    return t;
  }

  tileFailed() {
    if (this.failed || !this.alive) return;
    this.failed = true;
    const why = navigator.onLine === false ? "the device is offline" : "the tile server didn't answer";
    this.ctx.emit("error", { message: "maps: the map's tiles can't be loaded: " + why });
  }

  /** drawPins places each marker's pin, the selected one's callout, and the user's location. */
  drawPins() {
    if (!this.alive) return;
    const box = this.pins;
    box.textContent = "";
    if (this.w <= 0 || this.h <= 0) return;
    if (this.showsUser && this.user) {
      const [x, y] = this.point(this.user.lat, this.user.lng);
      const d = div("asuka-map-user");
      d.style.transform = `translate(${x}px, ${y}px)`;
      const acc = (this.user.acc / (40075016.686 * Math.cos((this.user.lat * Math.PI) / 180))) * this.world();
      if (acc > 12) {
        const a = div("asuka-map-accuracy");
        a.style.width = a.style.height = 2 * acc + "px";
        d.appendChild(a);
      }
      d.appendChild(div("asuka-map-dot"));
      box.appendChild(d);
    }
    let sel = null;
    for (const m of this.markers) {
      const lat = Number(m.at?.lat), lng = Number(m.at?.lng);
      if (!Number.isFinite(lat) || !Number.isFinite(lng)) continue;
      const [x, y] = this.point(lat, lng);
      if (x < -40 || x > this.w + 40 || y < -10 || y > this.h + 60) continue;
      const b = document.createElement("button");
      b.type = "button";
      b.className = "asuka-map-pin";
      b.tabIndex = this.interactive ? 0 : -1;
      b.dataset.id = m.id ?? "";
      b.setAttribute("aria-label", [m.title, m.subtitle].filter(Boolean).join(", ") || "Marker");
      b.style.transform = `translate(${x - 13}px, ${y - 36}px)`;
      b.innerHTML = pinSVG;
      b.firstChild.style.color = cssColor(m.color) || "var(--asuka-red)";
      box.appendChild(b);
      if (m.id === this.selected && (m.title || m.subtitle)) sel = { m, x, y };
    }
    if (sel) {
      const c = div("asuka-map-callout");
      if (sel.m.title) c.appendChild(div("asuka-map-callout-title")).textContent = sel.m.title;
      if (sel.m.subtitle) c.appendChild(div("asuka-map-callout-subtitle")).textContent = sel.m.subtitle;
      c.style.transform = `translate(${sel.x}px, ${sel.y - 40}px) translate(-50%, -100%)`;
      box.appendChild(c);
    }
  }

  // --- The user's location ---

  async followUser() {
    if (!this.showsUser) {
      if (this.watch != null) navigator.geolocation?.clearWatch(this.watch);
      this.watch = null;
      this.user = null;
      this.draw();
      return;
    }
    if (this.watch != null || !navigator.geolocation) return;
    // Only with the permission: the map never asks for it.
    try {
      const p = await navigator.permissions?.query({ name: "geolocation" });
      if (p && p.state !== "granted") {
        p.onchange = () => { if (p.state === "granted") this.followUser(); };
        return;
      }
    } catch {}
    if (!this.alive || !this.showsUser || this.watch != null) return;
    this.watch = navigator.geolocation.watchPosition(
      (pos) => {
        this.user = { lat: pos.coords.latitude, lng: pos.coords.longitude, acc: pos.coords.accuracy || 0 };
        this.draw();
      },
      () => {},
      { enableHighAccuracy: false, maximumAge: 30_000 },
    );
  }

  // --- Input ---

  listen() {
    const el = this.el;
    el.addEventListener("pointerdown", (e) => {
      if (!this.interactive || e.button > 0 || e.target === this.attribution) return;
      this.stopAnim();
      this.pointers.set(e.pointerId, this.local(e));
      this.startGesture();
    });
    el.addEventListener("pointermove", (e) => {
      if (!this.pointers.has(e.pointerId)) return;
      this.pointers.set(e.pointerId, this.local(e));
      this.moveGesture(e);
    });
    const up = (e) => {
      if (!this.pointers.has(e.pointerId)) return;
      this.pointers.delete(e.pointerId);
      if (el.hasPointerCapture?.(e.pointerId)) el.releasePointerCapture(e.pointerId);
      if (this.drag?.moved) {
        // The click that follows a drag isn't one.
        this.suppressClick = true;
        setTimeout(() => (this.suppressClick = false), 0);
        if (this.pointers.size === 0) this.report();
      }
      this.startGesture();
    };
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
    el.addEventListener("click", (e) => {
      if (this.suppressClick || !this.interactive || e.target === this.attribution) return;
      const pin = e.target.closest?.(".asuka-map-pin");
      if (pin) {
        this.selected = pin.dataset.id;
        this.drawPins();
        this.ctx.emit("marker", { id: pin.dataset.id });
        return;
      }
      if (e.target.closest?.(".asuka-map-callout")) return;
      if (this.selected) {
        this.selected = "";
        this.drawPins();
      }
      if (e.detail > 1) return; // a double click's second: a zoom
      const { x, y } = this.local(e);
      this.ctx.emit("press", this.at(x, y));
    });
    el.addEventListener("dblclick", (e) => {
      if (!this.interactive || e.target.closest?.(".asuka-map-pin, .asuka-map-callout") || e.target === this.attribution) return;
      e.preventDefault();
      const { x, y } = this.local(e);
      this.zoomTo(e.shiftKey ? -1 : 1, x, y);
    });
    el.addEventListener("wheel", (e) => {
      if (!this.interactive) return;
      e.preventDefault();
      this.stopAnim();
      const px = e.deltaY * (e.deltaMode === 1 ? 33 : e.deltaMode === 2 ? this.h : 1);
      // A notch (100 points) is half a level; a trackpad's pinch (Ctrl)
      // comes in small steps, as fine as it is.
      const { x, y } = this.local(e);
      this.zoomBy(-px / (e.ctrlKey ? 100 : 200), x, y);
      clearTimeout(this.wheelTimer);
      this.wheelTimer = setTimeout(() => this.report(), 300);
    }, { passive: false });
    el.addEventListener("keydown", (e) => {
      if (!this.interactive || e.target !== el || e.metaKey || e.ctrlKey || e.altKey) return;
      const step = Math.min(this.w, this.h) / 8;
      switch (e.key) {
        case "+": case "=": this.zoomTo(1, this.w / 2, this.h / 2); break;
        case "-": case "_": this.zoomTo(-1, this.w / 2, this.h / 2); break;
        case "ArrowLeft": this.panBy(step, 0); this.report(); break;
        case "ArrowRight": this.panBy(-step, 0); this.report(); break;
        case "ArrowUp": this.panBy(0, step); this.report(); break;
        case "ArrowDown": this.panBy(0, -step); this.report(); break;
        default: return;
      }
      e.preventDefault();
    });
  }

  local(e) {
    const r = this.el.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  /** startGesture begins a drag (one pointer) or a pinch (two) from where the pointers are. */
  startGesture() {
    const ps = [...this.pointers.values()];
    if (!ps.length) {
      this.drag = null;
      return;
    }
    const moved = this.drag?.moved || false;
    const mid = midpoint(ps);
    this.drag = { start: mid, last: mid, dist: ps.length > 1 ? distance(ps[0], ps[1]) : 0, moved };
  }

  moveGesture(e) {
    const d = this.drag;
    if (!d) return;
    const ps = [...this.pointers.values()];
    const mid = midpoint(ps);
    if (!d.moved) {
      if (Math.hypot(mid.x - d.start.x, mid.y - d.start.y) <= 3 && ps.length < 2) return;
      d.moved = true;
      for (const id of this.pointers.keys()) {
        try { this.el.setPointerCapture(id); } catch {}
      }
    }
    this.panBy(mid.x - d.last.x, mid.y - d.last.y);
    d.last = mid;
    if (ps.length > 1) {
      const dist = distance(ps[0], ps[1]);
      if (d.dist > 0 && dist > 0) this.zoomBy(Math.log2(dist / d.dist), mid.x, mid.y);
      d.dist = dist;
    }
  }

  /** zoomTo zooms by dz around a point over a fifth of a second. */
  zoomTo(dz, x, y) {
    const from = this.zoom, to = Math.max(Math.min(this.zoom + dz, maxZoom), 0);
    this.animate(200, (f) => this.zoomBy(from + (to - from) * f - this.zoom, x, y), () => this.report());
  }

  animate(ms, step, done) {
    this.stopAnim();
    const start = performance.now();
    const tick = (t) => {
      if (!this.alive) return;
      let f = Math.min((t - start) / ms, 1);
      f = 1 - (1 - f) * (1 - f); // easing out
      step(f);
      this.draw();
      if (f < 1) this.anim = requestAnimationFrame(tick);
      else {
        this.anim = null;
        done?.();
      }
    };
    this.anim = requestAnimationFrame(tick);
  }

  stopAnim() {
    if (this.anim) cancelAnimationFrame(this.anim);
    this.anim = null;
  }

  destroy() {
    this.alive = false;
    this.stopAnim();
    if (this.frame) cancelAnimationFrame(this.frame);
    clearTimeout(this.wheelTimer);
    this.resize.disconnect();
    if (this.watch != null) navigator.geolocation?.clearWatch(this.watch);
    for (const t of this.tileEls.values()) if (tiles.get(t.url) !== "loaded") t.img.src = blank;
    this.tileEls.clear();
  }
}

function div(cls) {
  const d = document.createElement("div");
  d.className = cls;
  return d;
}

const midpoint = (ps) => ({ x: ps.reduce((s, p) => s + p.x, 0) / ps.length, y: ps.reduce((s, p) => s + p.y, 0) / ps.length });
const distance = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);

/** cssColor is a marker's "#rrggbbaa" as CSS, "" if it has none. */
export function cssColor(c) {
  return typeof c === "string" && /^#[0-9a-f]{8}$/i.test(c) ? c : "";
}

// A teardrop pin, 26×36, its tip at the bottom: currentColor, a white
// outline and dot.
const pinSVG = `<svg width="26" height="36" viewBox="0 0 26 36" aria-hidden="true"><path d="M13 35C13 35 2 21.5 2 13a11 11 0 0 1 22 0c0 8.5-11 22-11 22z" fill="currentColor" stroke="#fff" stroke-width="1.5"/><circle cx="13" cy="13" r="4.2" fill="#fff"/></svg>`;

// asuka/sdk/lottie's player on the web: lottie-web (vendor/: its light
// build, 5.12.2, without expressions), loaded when a player is first made,
// drawing SVG. It's driven as lottieplayer's props say (lottieplayer.go):
// lottie-web plays its frames; a seek that animates and the progress
// reports run on requestAnimationFrame. A dotLottie is unzipped here
// (DecompressionStream), and overrides rewrite the animation's JSON
// before it loads.

let lottieLib = null;
function library() {
  lottieLib ??= import("./vendor/lottie_light.min.js").then(() => globalThis.lottie);
  return lottieLib;
}

/** sourceURL is where a source is: a URL, or one of the app's assets. */
function sourceURL(src) {
  if (/^(https?|blob|data|file):/.test(src)) return src;
  const base = (globalThis.asukaHost?.assets ?? "assets").replace(/\/$/, "");
  return new URL(base + "/" + src.replace(/^\.?\//, ""), document.baseURI).href;
}

/** unzip reads a zip's files (stored or deflated) into a Map of name → bytes. */
async function unzip(b) {
  const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  let end = -1;
  for (let i = b.length - 22; i >= Math.max(0, b.length - 65557); i--) {
    if (dv.getUint32(i, true) === 0x06054b50) { end = i; break; }
  }
  if (end < 0) throw new Error("not a zip");
  const count = dv.getUint16(end + 10, true);
  let p = dv.getUint32(end + 16, true);
  const files = new Map();
  for (let k = 0; k < count; k++) {
    if (dv.getUint32(p, true) !== 0x02014b50) break;
    const method = dv.getUint16(p + 10, true);
    const size = dv.getUint32(p + 20, true);
    const nameLen = dv.getUint16(p + 28, true), extraLen = dv.getUint16(p + 30, true), commentLen = dv.getUint16(p + 32, true);
    const local = dv.getUint32(p + 42, true);
    const name = new TextDecoder().decode(b.subarray(p + 46, p + 46 + nameLen));
    p += 46 + nameLen + extraLen + commentLen;
    const start = local + 30 + dv.getUint16(local + 26, true) + dv.getUint16(local + 28, true);
    const raw = b.subarray(start, start + size);
    if (method === 0) files.set(name, raw);
    else if (method === 8) {
      const s = new Blob([raw]).stream().pipeThrough(new DecompressionStream("deflate-raw"));
      files.set(name, new Uint8Array(await new Response(s).arrayBuffer()));
    }
  }
  return files;
}

/** animationJSON is a Lottie file's animation as an object: its JSON, or a dotLottie's animation (by id; its images inlined). */
async function animationJSON(b, id) {
  const text = (u8) => new TextDecoder().decode(u8);
  if (!(b[0] === 0x50 && b[1] === 0x4b && b[2] === 3 && b[3] === 4)) return JSON.parse(text(b));
  const files = await unzip(b);
  let manifest = {};
  if (files.has("manifest.json")) manifest = JSON.parse(text(files.get("manifest.json")));
  id ||= manifest.activeAnimationId || manifest.animations?.[0]?.id;
  let json = null;
  for (const dir of ["a", "animations"]) {
    if (id && files.has(`${dir}/${id}.json`)) json = files.get(`${dir}/${id}.json`);
  }
  if (!json && !id) for (const [n, v] of files) if (/^(a|animations)\/[^/]+\.json$/.test(n)) { json = v; break; }
  if (!json) throw new Error(`the dotLottie has no animation ${JSON.stringify(id)}`);
  const anim = JSON.parse(text(json));
  for (const a of anim.assets || []) {
    if (!a.p || a.e === 1 || a.layers) continue;
    for (const dir of ["i/", "images/"]) {
      const f = files.get(dir + a.p);
      if (!f) continue;
      const mime = /\.png$/i.test(a.p) ? "image/png" : /\.webp$/i.test(a.p) ? "image/webp" : /\.svg$/i.test(a.p) ? "image/svg+xml" : "image/jpeg";
      let s = "";
      for (let i = 0; i < f.length; i += 0x8000) s += String.fromCharCode(...f.subarray(i, i + 0x8000));
      a.u = "";
      a.p = `data:${mime};base64,${btoa(s)}`;
      a.e = 1;
      break;
    }
  }
  return anim;
}

/** match reports whether a keypath pattern (with * and **) names a path: exactly, or an ancestor of it (prefix). */
function match(pattern, path, prefix) {
  if (!pattern.length) return !path.length || prefix;
  const [p, ...rest] = pattern;
  if (p === "**") {
    for (let i = 0; i <= path.length; i++) if (match(rest, path.slice(i), prefix)) return true;
    return false;
  }
  if (p === "*") return path.length > 0 && match(rest, path.slice(1), prefix);
  return path.length > 0 && path[0] === p && match(rest, path.slice(1), prefix);
}

/** overridden is a copy of an animation with the overrides written into it: colors of fills and strokes under a keypath, an opacity on what it names, widths of strokes under it. */
function overridden(anim, overrides) {
  if (!overrides.length) return anim;
  const a = structuredClone(anim);
  const comps = new Map((a.assets || []).filter((x) => x.layers).map((x) => [x.id, x.layers]));
  const constant = (v) => ({ a: 0, k: v });
  const opacityOf = (path) => {
    let v = null;
    for (const o of overrides) if (o.kind === 1 && match(o.keypath.split("."), path, false)) v = o.value * 100;
    return v;
  };
  const items = (list, path) => {
    for (const it of list || []) {
      const p = [...path, it.nm || ""];
      if (it.ty === "gr") {
        const op = opacityOf(p);
        if (op !== null) for (const t of it.it || []) if (t.ty === "tr") t.o = constant(op);
        items(it.it, p);
        continue;
      }
      for (const o of overrides) {
        if (!match(o.keypath.split("."), p, o.kind !== 1)) continue;
        if (o.kind === 0 && (it.ty === "fl" || it.ty === "st")) {
          it.c = constant([o.r, o.g, o.b, o.a]);
          it.o = constant(o.a * 100);
          it.c.k = [o.r, o.g, o.b, 1];
        }
        if (o.kind === 1 && (it.ty === "fl" || it.ty === "st" || it.ty === "gf" || it.ty === "gs")) it.o = constant(o.value * 100);
        if (o.kind === 2 && (it.ty === "st" || it.ty === "gs")) it.w = constant(o.value);
      }
    }
  };
  const layers = (list, path, seen) => {
    for (const l of list || []) {
      const p = [...path, l.nm || ""];
      const op = opacityOf(p);
      if (op !== null && l.ks) l.ks.o = constant(op);
      items(l.shapes, p);
      if (l.ty === 0 && comps.has(l.refId) && !seen.has(l.refId)) layers(comps.get(l.refId), p, new Set([...seen, l.refId]));
    }
  };
  layers(a.layers, [], new Set());
  return a;
}

/** ease is a seek's curve at x (0 to 1) of its time: ui.Curve's, as package lottie's Go computes it. */
function ease(curve, damping, x, seconds) {
  const bezier = (x1, y1, x2, y2) => {
    let lo = 0, hi = 1, t = x;
    for (let i = 0; i < 30; i++) {
      const u = 1 - t, v = 3 * u * u * t * x1 + 3 * u * t * t * x2 + t * t * t;
      if (Math.abs(v - x) < 1e-6) break;
      if (v < x) lo = t; else hi = t;
      t = (lo + hi) / 2;
    }
    const u = 1 - t;
    return 3 * u * u * t * y1 + 3 * u * t * t * y2 + t * t * t;
  };
  switch (curve) {
    case 1: return x;
    case 2: return bezier(0.42, 0, 1, 1);
    case 3: return bezier(0, 0, 0.58, 1);
    case 4: {
      const z = Math.max(0.01, Math.min(1, damping)), w = 10 / Math.max(seconds, 0.01), t = x * seconds;
      if (z >= 1) return 1 - Math.exp(-w * t) * (1 + w * t);
      const wd = w * Math.sqrt(1 - z * z);
      return 1 - Math.exp(-z * w * t) * (Math.cos(wd * t) + (z * w / wd) * Math.sin(wd * t));
    }
  }
  return bezier(0.42, 0, 0.58, 1);
}

const fits = ["xMidYMid meet", "xMidYMid slice", "none", "xMidYMid meet"];

export function makePlayer(events) {
  const el = document.createElement("div");
  el.style.cssText = "position:relative;overflow:hidden";
  let source = "", data = null, animationId = "", mode = 0, loop = false, report = false;
  let playing = false, speed = 1, segment = { from: 0, to: 0 }, marker = "", overrides = [];
  let seekSerial = -1, pendingSeek = null, json = null, anim = null, token = 0, done = false;
  let tween = null, raf = 0, lastReported = -1;

  const destroyAnim = () => { anim?.destroy(); anim = null; el.textContent = ""; };

  async function load() {
    const t = ++token;
    try {
      let b = data;
      if (source) {
        const r = await fetch(sourceURL(source));
        if (!r.ok) throw new Error(`${source}: HTTP ${r.status}`);
        b = new Uint8Array(await r.arrayBuffer());
      }
      const j = await animationJSON(b, animationId);
      const lib = await library();
      if (t !== token) return;
      json = j;
      build(lib);
    } catch (e) {
      if (t === token) events.failed(String(e?.message || e));
    }
  }

  /** build makes lottie-web's animation of the JSON (overridden), at the frame the last one was at. */
  function build(lib, keepFrame) {
    const at = anim ? anim.currentFrame + anim.firstFrame : null;
    destroyAnim();
    if (mode === 3) el.style.cssText = "position:relative;overflow:hidden;display:flex;align-items:center;justify-content:center";
    anim = lib.loadAnimation({
      container: el, renderer: "svg", loop: false, autoplay: false,
      animationData: overridden(json, overrides),
      rendererSettings: { preserveAspectRatio: fits[Number(mode)] || fits[0], progressiveLoad: false },
    });
    if (mode === 3) {
      const svg = el.querySelector("svg");
      if (svg) { svg.style.width = json.w + "px"; svg.style.height = json.h + "px"; svg.style.flex = "none"; }
    }
    anim.addEventListener("enterFrame", () => reportNow());
    anim.addEventListener("complete", () => {
      if (loop) { play(true); return; }
      done = true;
      reportNow();
      events.completed();
    });
    anim.setSpeed(Math.abs(speed));
    if (keepFrame && at !== null) {
      goTo(at);
      if (playing) play();
      return;
    }
    events.loaded({
      width: json.w, height: json.h, start: json.ip, end: json.op, frameRate: json.fr,
      markers: (json.markers || []).map((m) => ({ name: m.cm, start: m.tm, duration: m.dr })),
    });
    done = false;
    goTo(span()[0]);
    if (pendingSeek) { const s = pendingSeek; pendingSeek = null; seekSerial = -1; setSeek(s); }
    if (playing) play(); else reportNow();
  }

  /** span is the range it plays: a marker's, the segment, or all of it. */
  function span() {
    if (!json) return [0, 0];
    let from = json.ip, to = json.op;
    const m = marker && (json.markers || []).find((x) => x.cm === marker);
    if (m) return [m.tm, m.tm + m.dr];
    if (segment.to > segment.from) { from = Math.max(segment.from, from); to = Math.min(segment.to, to); }
    return [from, to];
  }

  const frameNow = () => (anim ? anim.firstFrame + anim.currentFrame : 0);
  /** goTo shows absolute frame f (lottie-web's frames count from its segment's first). */
  function goTo(f) {
    if (!anim) return;
    anim.resetSegments(true);
    anim.goToAndStop(Math.max(0, f - json.ip), true);
  }

  function play(fromStart) {
    if (!anim || tween) return;
    const [from, to] = span();
    const back = speed < 0;
    let at = frameNow();
    if (fromStart || done || (back ? at <= from + 0.001 : at >= to - 0.001) || at < from || at > to) at = back ? to : from;
    done = false;
    anim.setDirection(back ? -1 : 1);
    // Its segment: from where it is to the range's end.
    anim.playSegments(back ? [at, from] : [at, to], true);
  }

  function setSeek(s) {
    if (s.serial === seekSerial) return;
    if (!anim) { pendingSeek = s; return; }
    seekSerial = s.serial;
    const [from, to] = span();
    let target = json.ip + s.progress * (json.op - json.ip);
    if (s.to === 1) target = from;
    if (s.to === 2) target = to;
    done = false;
    if (s.duration > 0) {
      const at = frameNow();
      anim.pause();
      tween = { from: at, to: target, start: performance.now(), seconds: s.duration, curve: s.curve, damping: s.damping };
      frame();
      return;
    }
    tween = null;
    goTo(target);
    if (playing) play(); else reportNow();
  }

  function frame() {
    cancelAnimationFrame(raf);
    raf = 0;
    if (!tween) return;
    const x = (performance.now() - tween.start) / 1000 / tween.seconds;
    if (x >= 1) {
      goTo(tween.to);
      tween = null;
      reportNow();
      if (playing) play();
      return;
    }
    goTo(tween.from + (tween.to - tween.from) * ease(tween.curve, tween.damping, x, tween.seconds));
    reportNow();
    raf = requestAnimationFrame(frame);
  }

  function reportNow() {
    if (!report || !json || json.op <= json.ip) return;
    const p = Math.min(1, Math.max(0, (frameNow() - json.ip) / (json.op - json.ip)));
    if (p !== lastReported) { lastReported = p; events.progress(p); }
  }

  function rangeChanged() {
    if (!anim) return;
    const [from, to] = span();
    done = false;
    tween = null;
    goTo(speed < 0 ? to : from);
    if (playing) play(); else reportNow();
  }

  return {
    el,
    setSource(v) { source = v; if (v) load(); },
    setData(v) { data = v; if (v.length && !source) load(); },
    setAnimation(v) { animationId = v; },
    setMode(v) { mode = v; if (anim) library().then((lib) => build(lib, true)); },
    setLoop(v) { loop = v; },
    setReport(v) { report = v; },
    setPlaying(v) {
      playing = v;
      if (!anim) return;
      if (v) play(); else { anim.pause(); reportNow(); }
    },
    setSpeed(v) {
      const flipped = (v < 0) !== (speed < 0);
      speed = v || 1;
      anim?.setSpeed(Math.abs(speed));
      if (flipped && playing && anim && !anim.isPaused) play();
    },
    setSegment(v) { segment = v; rangeChanged(); },
    setMarker(v) { marker = v; rangeChanged(); },
    setSeek,
    setOverrides(v) { overrides = v; if (anim) library().then((lib) => build(lib, true)); },
    destroy() { token++; cancelAnimationFrame(raf); destroyAnim(); },
  };
}

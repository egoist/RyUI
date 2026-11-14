// Images, symbols, videos and web pages.
//
//   - Image: an <img>, decoded off the main thread (decoding="async"),
//     cached by the browser; Loaded reports its natural size. Color tints
//     it, as a template image: the image is the mask of the color. Its
//     placeholder (an asset, or the core's decoded hash, in its own mode)
//     is a layer under it until it arrives, fading in (Transition). An
//     animated one (GIF, APNG, WebP, AVIF) plays: with WebCodecs'
//     ImageDecoder, on a canvas here, so Paused stops it where it is;
//     without (Safari), the <img> plays it, and Paused shows its first
//     frame.
//   - Symbol: an SF Symbol name drawn as its Material Symbol (the Android
//     host's, symbols.js), sized by the text props, colored by Color.
//   - Video: a <video> with the browser's controls.
//   - WebView: an <iframe> (webview.js). A page of the app's own site (its
//     assets, HTML, its files) gets all of it; another site's page loads,
//     and talks to the app only if it cooperates, as browsers keep sites
//     apart.

import { Kind, KindProp as P, Event, Events, Command, Reader, Writer } from "./ops.js";
import { own } from "./views.js";
import { paths, labels } from "./symbols.js";
import * as text from "./text.js";
import * as fs from "./fs.js";
import { webView } from "./webview.js";

const svgNS = "http://www.w3.org/2000/svg";

/** symbolSVG is a symbol's SVG, or null for a name the host has none for (reported once). */
const missing = new Set();
export function symbolSVG(name, rtl) {
  let d = paths[name];
  if (!d && name.endsWith(".fill")) d = paths[name.slice(0, -5)];
  if (!d) {
    if (name && !missing.has(name)) {
      missing.add(name);
      console.warn(`asuka: the web host has no symbol ${JSON.stringify(name)}`);
    }
    return null;
  }
  const s = document.createElementNS(svgNS, "svg");
  s.setAttribute("viewBox", "0 -960 960 960");
  s.setAttribute("aria-hidden", "true");
  s.classList.add("asuka-symbol");
  if (rtl && /\.(forward|backward)\b/.test(name)) s.classList.add("asuka-mirror");
  const p = document.createElementNS(svgNS, "path");
  p.setAttribute("d", d);
  p.setAttribute("fill", "currentColor");
  s.appendChild(p);
  return s;
}

export function symbolLabel(name) { return labels[name] || labels[name.replace(/\.fill$/, "")] || ""; }

/** sourceURL is where an image or video loads from: a URL, a file of the app's (a path), or one of its assets. */
export function sourceURL(h, src) {
  if (!src) return "";
  if (/^(https?|blob|data):/.test(src)) return src;
  if (src.startsWith("/")) return fs.blobURL(src);
  return h.assets + "/" + src.replace(/^\.?\//, "");
}

const fits = ["contain", "cover", "fill", "none"];

const image = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-image";
    const ph = document.createElement("div");
    ph.className = "asuka-image-placeholder";
    el.appendChild(ph);
    const img = document.createElement("img");
    img.decoding = "async";
    img.alt = "";
    img.draggable = false;
    el.appendChild(img);
    n.img = img;
    n.ph = ph;
    n.natural = null;
    n.mode = 0;
    n.phMode = -1;
    n.transition = 0;
    n.paused = false;
    n.gen = 0;
    img.addEventListener("load", () => {
      n.natural = [img.naturalWidth, img.naturalHeight];
      n.loadedSrc = img.currentSrc;
      arrive(n);
      tint(h, n);
      h.emit(n, Event.Loaded, (id) => Events.loaded(id, img.naturalWidth, img.naturalHeight));
      animate(h, n, img.currentSrc || img.src);
    });
    img.addEventListener("error", () => {
      h.emit(n, Event.Failed, (id) => Events.failed(id, "the image couldn't load: " + n.source));
    });
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Image.Source:
        n.source = v;
        n.natural = null;
        stopFrames(n);
        n.img.style.transition = "none";
        n.img.style.opacity = "0";
        showPlaceholder(h, n);
        n.img.src = sourceURL(h, v);
        if (!v) n.img.removeAttribute("src");
        break;
      case P.Image.Mode:
        n.mode = v;
        n.img.style.objectFit = fits[v] || "contain";
        if (n.canvas) n.canvas.style.objectFit = n.img.style.objectFit;
        showPlaceholder(h, n);
        tint(h, n);
        break;
      case P.Image.Placeholder:
        n.placeholder = v;
        showPlaceholder(h, n);
        break;
      case P.Image.PlaceholderImage:
        n.hashURL = hashURL(v);
        showPlaceholder(h, n);
        break;
      case P.Image.PlaceholderMode:
        n.phMode = v;
        showPlaceholder(h, n);
        break;
      case P.Image.Transition:
        n.transition = v;
        break;
      case P.Image.Paused:
        n.paused = v;
        if (n.frames) v ? stopFrames(n, true) : playFrames(n);
        else if (n.animatedSrc) stillFirstFrame(n, v);
        break;
    }
  },
  color(h, n) { tint(h, n); },
  label(h, n, v) { n.img.alt = v || ""; },
  measure(h, n) { return n.natural || [0, 0]; },
  destroy(h, n) { stopFrames(n); },
};

/** hashURL is the core's decoded placeholder (u32 width, u32 height, RGBA) as a data URL. */
function hashURL(b) {
  if (!b || b.length < 8) return "";
  const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const w = dv.getUint32(0, true), hh = dv.getUint32(4, true);
  if (!w || !hh || b.length < 8 + w * hh * 4) return "";
  const c = document.createElement("canvas");
  c.width = w;
  c.height = hh;
  c.getContext("2d").putImageData(new ImageData(new Uint8ClampedArray(b.slice(8, 8 + w * hh * 4)), w, hh), 0, 0);
  return c.toDataURL();
}

function sizeFor(mode) {
  return fits[mode] === "fill" ? "100% 100%" : fits[mode] === "none" ? "auto" : fits[mode] || "contain";
}

/** showPlaceholder shows the placeholder (the hash's, else Placeholder's) until the image arrives. */
function showPlaceholder(h, n) {
  const url = n.hashURL || (n.placeholder ? sourceURL(h, n.placeholder) : "");
  const s = n.ph.style;
  if (n.natural && s.opacity === "0") return;
  s.transition = "none";
  s.opacity = url ? "1" : "0";
  s.backgroundImage = url ? `url(${JSON.stringify(url)})` : "";
  s.backgroundSize = sizeFor(n.phMode >= 0 ? n.phMode : n.mode);
  n.ph.hidden = !url;
}

/** arrive fades the image in over its placeholder (or from nothing) for Transition. */
function arrive(n) {
  const had = !n.ph.hidden && n.ph.style.opacity !== "0";
  let d = n.transition;
  if (d < 0) d = 0;
  else if (d === 0 && had) d = 0.25;
  const img = n.img.style, ph = n.ph.style;
  if (d <= 0 || !n.el.isConnected) {
    img.transition = "none";
    img.opacity = "1";
    ph.opacity = "0";
    return;
  }
  img.transition = ph.transition = `opacity ${d}s ease-in-out`;
  requestAnimationFrame(() => {
    img.opacity = "1";
    ph.opacity = "0";
  });
}

/** animate plays an animated image's frames, if it's one: with ImageDecoder on a canvas; without, it's the <img>'s. */
async function animate(h, n, url) {
  const gen = ++n.gen;
  let buf;
  try {
    buf = await (await fetch(url)).arrayBuffer();
  } catch { return; }
  if (gen !== n.gen) return;
  const b = new Uint8Array(buf);
  if (!animatedBytes(b)) return;
  h.emit(n, Event.Animated, (id) => Events.animated(id));
  if (typeof ImageDecoder === "undefined") {
    n.animatedSrc = url;
    if (n.paused) stillFirstFrame(n, true);
    return;
  }
  const type = b[0] === 0x47 ? "image/gif" : b[0] === 0x89 ? "image/png" : b[8] === 0x57 ? "image/webp" : "image/avif";
  let dec;
  try {
    dec = new ImageDecoder({ data: buf, type });
    await dec.tracks.ready;
  } catch { return; }
  if (gen !== n.gen) { dec.close(); return; }
  const track = dec.tracks.selectedTrack;
  if (!track || track.frameCount < 2) { dec.close(); return; }
  const c = document.createElement("canvas");
  c.width = n.natural[0];
  c.height = n.natural[1];
  c.style.objectFit = n.img.style.objectFit || "contain";
  n.el.appendChild(c);
  n.canvas = c;
  n.img.style.visibility = "hidden";
  n.frames = { dec, gen, i: 0, plays: 0, count: track.frameCount, loops: track.repetitionCount, timer: 0 };
  await drawFrame(n);
  if (!n.paused) playFrames(n);
}

/** animatedBytes reports whether a file is an animated GIF, APNG, WebP or AVIF, from its headers. */
function animatedBytes(b) {
  const at = (i, s) => [...s].every((ch, k) => b[i + k] === ch.charCodeAt(0));
  if (at(0, "GIF8")) {
    let n = 0;
    for (let i = 0; i + 2 < b.length && n < 2; i++) if (b[i] === 0x21 && b[i + 1] === 0xf9 && b[i + 2] === 0x04) n++;
    return n > 1;
  }
  if (b[0] === 0x89 && at(1, "PNG")) {
    for (let p = 8; p + 8 <= b.length;) {
      const len = (b[p] << 24 | b[p + 1] << 16 | b[p + 2] << 8 | b[p + 3]) >>> 0;
      if (at(p + 4, "acTL")) return true;
      if (at(p + 4, "IDAT")) return false;
      p += 12 + len;
    }
    return false;
  }
  if (at(0, "RIFF") && at(8, "WEBPVP8X")) return (b[20] & 2) !== 0;
  if (at(4, "ftyp")) return at(8, "avis");
  return false;
}

async function drawFrame(n) {
  const f = n.frames;
  const { image } = await f.dec.decode({ frameIndex: f.i });
  if (n.frames !== f) { image.close(); return 0; }
  n.canvas.getContext("2d").clearRect(0, 0, n.canvas.width, n.canvas.height);
  n.canvas.getContext("2d").drawImage(image, 0, 0);
  const d = (image.duration || 0) / 1000; // microseconds to milliseconds
  image.close();
  return d <= 10 ? 100 : d; // as browsers show it
}

function playFrames(n) {
  const f = n.frames;
  if (!f || f.timer || n.paused) return;
  const step = async () => {
    f.timer = 0;
    if (n.frames !== f || n.paused) return;
    let next = f.i + 1;
    if (next >= f.count) {
      f.plays++;
      if (Number.isFinite(f.loops) && f.plays > f.loops) return;
      next = 0;
    }
    f.i = next;
    const d = await drawFrame(n);
    if (n.frames === f && !n.paused) f.timer = setTimeout(step, d);
  };
  drawFrame(n).then(() => {}); // the frame it's at, again: it may have been cleared
  f.timer = setTimeout(step, 100);
}

/** stopFrames stops the canvas's frames (keep: where they are), or takes the canvas away. */
function stopFrames(n, keep) {
  const f = n.frames;
  if (f && f.timer) { clearTimeout(f.timer); f.timer = 0; }
  if (keep) return;
  n.gen++;
  if (f) f.dec.close();
  n.frames = null;
  n.animatedSrc = null;
  if (n.canvas) { n.canvas.remove(); n.canvas = null; }
  if (n.img) n.img.style.visibility = "";
}

/** stillFirstFrame shows an animated <img>'s first frame on a canvas while paused (no ImageDecoder). */
function stillFirstFrame(n, paused) {
  if (!paused) {
    if (n.canvas) { n.canvas.remove(); n.canvas = null; }
    n.img.style.visibility = "";
    return;
  }
  if (n.canvas || !n.natural) return;
  const c = document.createElement("canvas");
  c.width = n.natural[0];
  c.height = n.natural[1];
  c.style.objectFit = n.img.style.objectFit || "contain";
  c.getContext("2d").drawImage(n.img, 0, 0); // a canvas draws an animation's first frame
  n.el.appendChild(c);
  n.canvas = c;
  n.img.style.visibility = "hidden";
}

/** tint draws the color through the image's shape (Color on an image). */
function tint(h, n) {
  const c = h.css(n.color);
  const s = n.el.style;
  if (!c || !n.natural) {
    n.img.style.visibility = "";
    s.maskImage = s.webkitMaskImage = "";
    if (!n.background) s.backgroundColor = "";
    return;
  }
  const url = `url(${JSON.stringify(n.img.currentSrc || n.img.src)})`;
  s.maskImage = s.webkitMaskImage = url;
  const size = fits[n.mode] === "fill" ? "100% 100%" : fits[n.mode] === "none" ? "auto" : fits[n.mode];
  s.maskSize = s.webkitMaskSize = size;
  s.maskRepeat = s.webkitMaskRepeat = "no-repeat";
  s.maskPosition = s.webkitMaskPosition = "center";
  s.backgroundImage = "";
  s.backgroundColor = c;
  n.img.style.visibility = "hidden";
}

const symbol = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-symbol-box";
    el.setAttribute("role", "img");
    text.applyFont(h, n, el);
  },
  prop(h, n, p, v) {
    if (p !== P.Symbol.Name) return;
    n.name = v;
    n.el.textContent = "";
    const s = symbolSVG(v, h.rtl);
    if (s) n.el.appendChild(s);
    if (!n.el.hasAttribute("aria-label")) {
      const l = symbolLabel(v);
      if (l) n.el.setAttribute("aria-label", l);
    }
  },
  direction(h, n) { if (n.name) symbol.prop(h, n, P.Symbol.Name, n.name); },
  measure(h, n) {
    // A symbol is as tall as its text's line, and as wide: Material's are square.
    const [size] = text.fontOf(h, n.font);
    const px = size * h.fontScale * 1.25;
    return [px, px];
  },
};

const video = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-video";
    const v = document.createElement("video");
    v.playsInline = true;
    v.controls = true;
    v.preload = "metadata";
    el.appendChild(v);
    n.video = v;
    v.addEventListener("loadedmetadata", () => {
      n.natural = [v.videoWidth, v.videoHeight];
      h.emit(n, Event.Loaded, (id) => Events.loaded(id, v.videoWidth, v.videoHeight));
    });
    v.addEventListener("ended", () => h.emit(n, Event.Ended, Events.ended));
    v.addEventListener("error", () => h.emit(n, Event.Failed, (id) => Events.failed(id, v.error?.message || "the video couldn't play")));
  },
  prop(h, n, p, val) {
    const v = n.video;
    switch (p) {
      case P.Video.Source: v.src = sourceURL(h, val); break;
      case P.Video.Mode: v.style.objectFit = fits[val] || "contain"; break;
      case P.Video.HideControls: v.controls = !val; break;
      case P.Video.AutoPlay: v.autoplay = !!val; if (val) v.play().catch(() => {}); break;
      case P.Video.Loop: v.loop = !!val; break;
      case P.Video.Muted: v.muted = !!val; break;
    }
  },
  command(h, n, cmd, args) {
    const v = n.video;
    switch (cmd) {
      case Command.Play: v.play().catch(() => {}); return true;
      case Command.Pause: v.pause(); return true;
      case Command.Seek: v.currentTime = new Reader(args).f64(); return true;
    }
    return false;
  },
  destroy(h, n) { n.video.pause(); n.video.removeAttribute("src"); n.video.load(); return false; },
  measure(h, n) { return n.natural || [0, 0]; },
};

export const mediaKinds = {
  [Kind.Image]: image,
  [Kind.Symbol]: symbol,
  [Kind.Video]: video,
  [Kind.WebView]: webView,
};

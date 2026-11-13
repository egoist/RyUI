// Images, symbols, videos and web pages.
//
//   - Image: an <img>, decoded off the main thread (decoding="async"),
//     cached by the browser; Loaded reports its natural size. Color tints
//     it, as a template image: the image is the mask of the color.
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
    const img = document.createElement("img");
    img.decoding = "async";
    img.alt = "";
    img.draggable = false;
    el.appendChild(img);
    n.img = img;
    n.natural = null;
    n.mode = 0;
    img.addEventListener("load", () => {
      if (img.dataset.placeholder === "1") return;
      n.natural = [img.naturalWidth, img.naturalHeight];
      n.loadedSrc = img.currentSrc;
      el.classList.remove("asuka-loading");
      tint(h, n);
      h.emit(n, Event.Loaded, (id) => Events.loaded(id, img.naturalWidth, img.naturalHeight));
    });
    img.addEventListener("error", () => {
      if (img.dataset.placeholder === "1") return;
      if (n.placeholder) showPlaceholder(h, n);
      h.emit(n, Event.Failed, (id) => Events.failed(id, "the image couldn't load: " + n.source));
    });
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Image.Source:
        n.source = v;
        n.natural = null;
        if (n.placeholder) showPlaceholder(h, n);
        n.img.dataset.placeholder = "0";
        n.img.src = sourceURL(h, v);
        if (!v) n.img.removeAttribute("src");
        break;
      case P.Image.Mode:
        n.mode = v;
        n.img.style.objectFit = fits[v] || "contain";
        tint(h, n);
        break;
      case P.Image.Placeholder:
        n.placeholder = v;
        if (!n.natural) showPlaceholder(h, n);
        break;
    }
  },
  color(h, n) { tint(h, n); },
  label(h, n, v) { n.img.alt = v || ""; },
  measure(h, n) { return n.natural || [0, 0]; },
};

function showPlaceholder(h, n) {
  n.el.style.backgroundImage = `url(${JSON.stringify(sourceURL(h, n.placeholder))})`;
  n.el.style.backgroundSize = fits[n.mode] === "fill" ? "100% 100%" : fits[n.mode] === "none" ? "auto" : fits[n.mode];
  n.el.classList.add("asuka-loading");
}

/** tint draws the color through the image's shape (Color on an image). */
function tint(h, n) {
  const c = h.css(n.color);
  const s = n.el.style;
  if (!c || !n.natural) {
    n.img.style.visibility = "";
    s.maskImage = s.webkitMaskImage = "";
    if (!n.background) s.backgroundColor = "";
    if (!n.el.classList.contains("asuka-loading")) s.backgroundImage = "";
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

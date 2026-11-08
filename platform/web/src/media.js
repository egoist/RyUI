// Images, symbols, videos and web pages.
//
//   - Image: an <img>, decoded off the main thread (decoding="async"),
//     cached by the browser; Loaded reports its natural size. Color tints
//     it, as a template image: the image is the mask of the color.
//   - Symbol: an SF Symbol name drawn as its Material Symbol (the Android
//     host's, symbols.js), sized by the text props, colored by Color.
//   - Video: a <video> with the browser's controls.
//   - WebView: an <iframe>. A page of the app's (its assets, or HTML) gets
//     window.ryui.postMessage and runs Eval; another site's page only
//     loads, as browsers keep sites apart.

import { Kind, KindProp as P, Event, Events, Command, Reader, Writer } from "./ops.js";
import { own } from "./views.js";
import { paths, labels } from "./symbols.js";
import * as text from "./text.js";
import * as fs from "./fs.js";

const svgNS = "http://www.w3.org/2000/svg";

/** symbolSVG is a symbol's SVG, or null for a name the host has none for (reported once). */
const missing = new Set();
export function symbolSVG(name, rtl) {
  let d = paths[name];
  if (!d && name.endsWith(".fill")) d = paths[name.slice(0, -5)];
  if (!d) {
    if (name && !missing.has(name)) {
      missing.add(name);
      console.warn(`ryui: the web host has no symbol ${JSON.stringify(name)}`);
    }
    return null;
  }
  const s = document.createElementNS(svgNS, "svg");
  s.setAttribute("viewBox", "0 -960 960 960");
  s.setAttribute("aria-hidden", "true");
  s.classList.add("ryui-symbol");
  if (rtl && /\.(forward|backward)\b/.test(name)) s.classList.add("ryui-mirror");
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
    el.className = "ryui-image";
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
      el.classList.remove("ryui-loading");
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
  n.el.classList.add("ryui-loading");
}

/** tint draws the color through the image's shape (Color on an image). */
function tint(h, n) {
  const c = h.css(n.color);
  const s = n.el.style;
  if (!c || !n.natural) {
    n.img.style.visibility = "";
    s.maskImage = s.webkitMaskImage = "";
    if (!n.background) s.backgroundColor = "";
    if (!n.el.classList.contains("ryui-loading")) s.backgroundImage = "";
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
    el.className = "ryui-symbol-box";
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
    el.className = "ryui-video";
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

const webView = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "ryui-webview";
    const f = document.createElement("iframe");
    f.setAttribute("allow", "fullscreen; clipboard-write; autoplay");
    el.appendChild(f);
    n.frameEl = f;
    n.preload = "";
    f.addEventListener("load", () => {
      const w = sameOrigin(f);
      let url = f.src || "about:srcdoc";
      let title = "";
      if (w) {
        try {
          url = w.location.href;
          title = w.document.title;
          inject(h, n, w);
        } catch {}
      }
      h.emit(n, Event.Navigated, (id) => Events.navigated(id, url));
      if (n.listening(Event.WebState)) {
        const flags = 0;
        h.dispatch(Events.webState(n.id, new Writer().string(url).string(title).u8(flags).f32(1).done()));
      }
    });
    n.onMessage = (e) => {
      if (e.source !== f.contentWindow || !e.data || e.data.ryui !== true) return;
      h.emit(n, Event.Message, (id) => Events.message(id, String(e.data.text)));
    };
    addEventListener("message", n.onMessage);
  },
  prop(h, n, p, v) {
    const f = n.frameEl;
    switch (p) {
      case P.WebView.URL:
        if (v) { f.removeAttribute("srcdoc"); f.src = sourceURL(h, v); }
        break;
      case P.WebView.HTML:
        if (v) f.srcdoc = (n.preload ? `<script>${n.preload}<\/script>` : "") + bridgeScript + v;
        break;
      case P.WebView.Preload:
        n.preload = v;
        break;
      // UserAgent, Block and Requests are the browser's to decide: a page can't set them for a frame.
    }
  },
  command(h, n, cmd, args) {
    const w = sameOrigin(n.frameEl);
    switch (cmd) {
      case Command.Eval:
        if (w) {
          try { w.eval(new TextDecoder().decode(args)); } catch (e) { console.warn("ryui: WebView script:", e); }
        } else console.warn("ryui: a WebView runs script only in a page of the app's own");
        return true;
      case Command.WebNavigate: {
        const k = args[0];
        try {
          if (k === 0) n.frameEl.contentWindow.history.back();
          else if (k === 1) n.frameEl.contentWindow.history.forward();
          else if (k === 2) w ? w.location.reload() : (n.frameEl.src = n.frameEl.src);
          else if (k === 3) w?.stop();
        } catch {}
        return true;
      }
    }
    return false;
  },
  destroy(h, n) { removeEventListener("message", n.onMessage); return false; },
  measure() { return [0, 0]; },
};

const bridgeScript = "<script>window.ryui={postMessage:function(t){parent.postMessage({ryui:true,text:String(t)},'*')}}<\/script>";

function sameOrigin(f) {
  try {
    const w = f.contentWindow;
    if (w && w.document) return w;
  } catch {}
  return null;
}

/** inject gives a page of the app's own window.ryui.postMessage and its Preload script. */
function inject(h, n, w) {
  if (w.ryui) return;
  w.ryui = { postMessage: (t) => window.postMessage({ ryui: true, text: String(t) }, "*") };
  // Messages come from the frame's window.
  w.ryui.postMessage = (t) => n.onMessage({ source: n.frameEl.contentWindow, data: { ryui: true, text: String(t) } });
  if (n.preload) try { w.eval(n.preload); } catch {}
}

export const mediaKinds = {
  [Kind.Image]: image,
  [Kind.Symbol]: symbol,
  [Kind.Video]: video,
  [Kind.WebView]: webView,
};

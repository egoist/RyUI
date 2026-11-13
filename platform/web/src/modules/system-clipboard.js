// The clipboard on the web (device.Clipboard, ClipboardHas,
// ClipboardImage, CopyImage, OnClipboardChange), through the async
// Clipboard API. Browsers read the clipboard only for a focused page, and
// ask the user first (Chrome a permission, Safari and Firefox a Paste
// button by the pointer); images are PNG (what browsers put there and
// take).

import * as fs from "../fs.js";

const enc = new TextEncoder();

/** clipboardText is device.clipboard's reply: u8 has, then the text. */
export async function clipboardText() {
  let t;
  try {
    t = await navigator.clipboard.readText();
  } catch (e) {
    if (e?.name === "NotAllowedError" && document.hasFocus()) throw new Error("ryui: the browser didn't allow reading the clipboard");
    return new Uint8Array([0]);
  }
  if (t === "") return new Uint8Array([0]);
  const b = enc.encode(t);
  const out = new Uint8Array(b.length + 1);
  out[0] = 1;
  out.set(b, 1);
  return out;
}

const urlish = /^\s*[a-z][a-z0-9+.-]*:\/\/\S+\s*$/i;

/** pngOf is a PNG of an image's bytes (as they are if they're one). */
export async function pngOf(bytes, type) {
  if (type === "image/png" || (bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47)) {
    return new Blob([bytes], { type: "image/png" });
  }
  let bmp;
  try {
    bmp = await createImageBitmap(new Blob([bytes], { type: type || "" }));
  } catch {
    throw new Error("not an image");
  }
  const c = document.createElement("canvas");
  c.width = bmp.width;
  c.height = bmp.height;
  c.getContext("2d").drawImage(bmp, 0, 0);
  bmp.close?.();
  return new Promise((resolve, reject) => c.toBlob((b) => (b ? resolve(b) : reject(new Error("not an image"))), "image/png"));
}

let names = 0;

/** clipboardModule is the "clipboard" module. */
export function clipboardModule() {
  // OnClipboardChange: what changes it is seen, as browsers tell a page:
  // copies and cuts in it, the app's own writes, and, where the user has
  // let the page read the clipboard (Chrome's permission), another app's
  // copy, read again as the page is focused and every second while it is.
  let waiting = [];
  let last = null; // what the clipboard held when last read
  let timer = 0;
  let installed = false;

  const changed = () => {
    const ws = waiting;
    waiting = [];
    stopPolling();
    for (const w of ws) w.resolve();
  };
  const signature = async () => {
    try {
      if (navigator.clipboard.read) {
        const items = await navigator.clipboard.read();
        let s = "";
        for (const it of items) {
          s += it.types.join(",") + ";";
          if (it.types.includes("text/plain")) s += await (await it.getType("text/plain")).text();
          else for (const t of it.types) s += (await it.getType(t)).size + ";";
        }
        return s;
      }
      return await navigator.clipboard.readText();
    } catch {
      return null;
    }
  };
  const check = async () => {
    if (!waiting.length || !document.hasFocus()) return;
    const s = await signature();
    if (s == null) return;
    if (last != null && s !== last) {
      last = s;
      changed();
    } else last = s;
  };
  const readable = async () => {
    try {
      return (await navigator.permissions?.query({ name: "clipboard-read" }))?.state === "granted";
    } catch {
      return false;
    }
  };
  const startPolling = async () => {
    if (timer || !(await readable()) || !waiting.length) return;
    last = await signature();
    timer = setInterval(check, 1000);
  };
  const stopPolling = () => { clearInterval(timer); timer = 0; };
  const install = () => {
    if (installed) return;
    installed = true;
    for (const e of ["copy", "cut"]) document.addEventListener(e, () => setTimeout(changed, 0), true);
    addEventListener("focus", check);
    document.addEventListener("visibilitychange", () => { if (!document.hidden) check(); });
    // The app's own writes (ui.Copy, CopyImage) go through these.
    const cb = navigator.clipboard;
    for (const m of ["writeText", "write"]) {
      const f = cb?.[m];
      if (typeof f !== "function") continue;
      cb[m] = function (...args) {
        const p = f.apply(cb, args);
        p.then(() => changed(), () => {});
        return p;
      };
    }
  };

  return {
    // u8 text, u8 image, u8 URL.
    has: async () => {
      const out = new Uint8Array(3);
      try {
        if (navigator.clipboard.read) {
          for (const it of await navigator.clipboard.read()) {
            if (it.types.some((t) => t.startsWith("image/"))) out[1] = 1;
            if (it.types.includes("text/uri-list")) out[2] = 1;
            if (it.types.includes("text/plain")) {
              out[0] = 1;
              if (urlish.test(await (await it.getType("text/plain")).text())) out[2] = 1;
            }
          }
        } else {
          const t = await navigator.clipboard.readText();
          if (t) { out[0] = 1; out[2] = urlish.test(t) ? 1 : 0; }
        }
      } catch (e) {
        if (e?.name === "NotAllowedError" && document.hasFocus()) throw new Error("ryui: the browser didn't allow reading the clipboard");
      }
      return out;
    },
    // The image's file in the app's caches (a PNG); nothing if there's none.
    image: async () => {
      if (!navigator.clipboard.read) throw new Error("ryui: clipboard.image isn't on the web in this browser: it reads only text from the clipboard");
      let items;
      try {
        items = await navigator.clipboard.read();
      } catch (e) {
        if (e?.name === "NotAllowedError" && document.hasFocus()) throw new Error("ryui: the browser didn't allow reading the clipboard");
        return null;
      }
      for (const it of items) {
        const t = it.types.find((t) => t.startsWith("image/"));
        if (!t) continue;
        const blob = await it.getType(t);
        const ext = { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "image/svg+xml": "svg" }[t] || "png";
        const path = `/tmp/ryui-clipboard/${Date.now().toString(36)}-${++names}.${ext}`;
        fs.writeFile(path, new Uint8Array(await blob.arrayBuffer()));
        return path;
      }
      return null;
    },
    copyImage: async (b) => {
      const path = new TextDecoder().decode(b);
      const bytes = fs.readFile(path);
      if (!bytes) throw new Error("no file at " + path);
      if (!navigator.clipboard?.write || !globalThis.ClipboardItem) throw new Error("ryui: clipboard.copyImage isn't on the web in this browser: it puts only text on the clipboard");
      const copy = bytes.slice();
      // A promise, as Safari wants it, so the write keeps the user's gesture.
      await navigator.clipboard.write([new ClipboardItem({ "image/png": pngOf(copy, typeOf(path)) })]);
    },
    next: () => {
      install();
      return new Promise((resolve, reject) => {
        waiting.push({ resolve: () => resolve(null), reject });
        startPolling();
      });
    },
    stop: () => {
      const ws = waiting;
      waiting = [];
      stopPolling();
      for (const w of ws) w.reject(new Error("stopped"));
    },
  };
}

const types = {
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", avif: "image/avif",
  bmp: "image/bmp", svg: "image/svg+xml", heic: "image/heic", heif: "image/heif", ico: "image/x-icon", tif: "image/tiff", tiff: "image/tiff",
  mp4: "video/mp4", m4v: "video/mp4", mov: "video/quicktime", webm: "video/webm", ogv: "video/ogg",
  mp3: "audio/mpeg", m4a: "audio/mp4", aac: "audio/aac", wav: "audio/wav", flac: "audio/flac", ogg: "audio/ogg", oga: "audio/ogg", opus: "audio/ogg",
  pdf: "application/pdf", txt: "text/plain", md: "text/markdown", csv: "text/csv", html: "text/html", htm: "text/html",
  json: "application/json", xml: "application/xml", js: "text/javascript", css: "text/css", zip: "application/zip",
  doc: "application/msword", docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xls: "application/vnd.ms-excel", xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  ppt: "application/vnd.ms-powerpoint", pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  ics: "text/calendar", vcf: "text/vcard", epub: "application/epub+zip",
};

/** typeOf is a file's MIME type from its name's extension ("" if not known). */
export function typeOf(name) {
  const base = String(name).split(/[?#]/)[0];
  const dot = base.lastIndexOf(".");
  return dot < 0 ? "" : types[base.slice(dot + 1).toLowerCase()] || "";
}

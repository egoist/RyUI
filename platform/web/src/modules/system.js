// The system's modules on the web, as the other hosts have them, with
// what a browser offers:
//
//   - secureStore (asuka/sdk/secure): IndexedDB, encrypted under a
//     non-extractable WebCrypto key (system-secure.js). Not a keychain.
//   - device: info (the browser's platform, from User-Agent Client Hints
//     where there are, else the user agent), the clipboard's text,
//     keeping the screen on (the Screen Wake Lock, taken again as the
//     page comes back in front: browsers drop it when it's hidden).
//   - clipboard: images, what it holds, its changes (system-clipboard.js).
//   - browser: a new tab.
//   - photos, files: the browser's file picker; the files are copied into
//     the app's caches (/tmp), as iOS and Android give them.
//   - share: the system's share sheet with the files (Web Share), or, where
//     the browser can't share files, a download of each.
//   - preview: the file in a new tab, which the browser shows (images,
//     video, audio, PDFs, text, HTML).
//   - print: the browser's print dialog, for HTML, a file it can show, or
//     a WebView of the app's own page.
//   - compose: mailto: and sms: links; files go through the share sheet.
//   - notify: the Notification API (system-notify.js), through the app's
//     service worker (sw.js).
//   - push: Web Push (system-push.js).
//
// A method a browser can't do answers with an error that says why.

import { Reader } from "../ops.js";
import * as fs from "../fs.js";
import { SecureStore, idbBackend, migrateLocalStorage } from "./system-secure.js";
import { clipboardModule, clipboardText, typeOf } from "./system-clipboard.js";
import { notifyFor } from "./system-notify.js";
import { pushModule } from "./system-push.js";

const dec = new TextDecoder();

const notOnWeb = (module, method, why) => new Error(`asuka: ${module}.${method} isn't on the web: ${why}`);

export function modules(h) {
  const name = h.options.name || "app";
  // The secure store, its old values (kept in localStorage) moved in first.
  let secure = null;
  const store = () => {
    secure ??= (async () => {
      const s = new SecureStore(idbBackend("asuka-secure:" + name));
      await migrateLocalStorage(s, h.prefix());
      return s;
    })().catch((e) => { secure = null; throw e; });
    return secure;
  };
  return {
    secureStore: {
      // args: the key; result: u8 found, then the value.
      get: async (b) => {
        const v = await (await store()).get(dec.decode(b));
        if (!v) return new Uint8Array([0]);
        const out = new Uint8Array(v.length + 1);
        out[0] = 1;
        out.set(v, 1);
        return out;
      },
      // args: u32 key length, the key, the value.
      set: async (b) => {
        const k = splitKey(b);
        if (!k) throw new Error("secureStore.set: bad arguments");
        await (await store()).set(k.key, k.value.slice());
      },
      delete: async (b) => { await (await store()).delete(dec.decode(b)); },
    },
    device: {
      info: () => deviceInfo(h),
      clipboard: clipboardText,
      keepAwake: (b) => keepAwake(b[0] === 1),
    },
    clipboard: clipboardModule(),
    browser: {
      open: (b) => {
        const w = window.open(dec.decode(b), "_blank");
        if (w) w.opener = null;
      },
    },
    // args: u8 limit; result: the picked images' paths, a line each.
    photos: { pick: (b) => pickFiles(["image/*"], b[0] || 1).then((fs) => fs.map((f) => f.path).join("\n")) },
    files: {
      // args: u8 multiple, then the MIME types, a line each; result: JSON,
      // the files (path, name, type, size); nothing if the user cancelled.
      pick: async (b) => {
        const types = dec.decode(b.subarray(1)).split("\n").filter(Boolean);
        const files = await pickFiles(types, b[0] === 1 ? 0 : 1);
        return files.length ? JSON.stringify(files) : null;
      },
    },
    share: { files: (b) => shareFiles(b) },
    preview: { show: (b) => preview(h, b) },
    print: {
      html: (b) => printHTML(h, JSON.parse(dec.decode(b))),
      file: (b) => printFile(h, JSON.parse(dec.decode(b))),
      webView: (b) => printWebView(h, JSON.parse(dec.decode(b))),
      pdf: () => { throw notOnWeb("print", "pdf", "a browser makes a PDF only through its print dialog (print.HTML, then Save as PDF there)"); },
    },
    compose: {
      can: () => composeCan(),
      mail: (b) => composeMail(h, JSON.parse(dec.decode(b))),
      message: (b) => composeMessage(h, JSON.parse(dec.decode(b))),
    },
    notify: notifyFor(h),
    push: pushModule(h),
  };
}

/** splitKey splits secureStore.set's args: u32 key length, the key, the value. */
export function splitKey(b) {
  if (b.length < 4) return null;
  const n = new DataView(b.buffer, b.byteOffset, 4).getUint32(0, true);
  if (n > b.length - 4) return null;
  return { key: dec.decode(b.subarray(4, 4 + n)), value: b.subarray(4 + n) };
}

// --- device ---

/** deviceInfo is device.info's JSON: model, system, systemVersion, appVersion, build. */
export async function deviceInfo(h, nav = navigator) {
  const ua = nav.userAgent || "";
  let system = "", version = "", model = "";
  if (/iPhone|iPod/.test(ua)) { system = "iOS"; model = "iPhone"; }
  else if (/iPad/.test(ua) || (/Macintosh/.test(ua) && nav.maxTouchPoints > 1)) { system = "iPadOS"; model = "iPad"; }
  else if (/Android/.test(ua)) system = "Android";
  else if (/CrOS/.test(ua)) { system = "ChromeOS"; model = "Chromebook"; }
  else if (/Mac/.test(ua)) { system = "macOS"; model = "Mac"; }
  else if (/Win/.test(ua)) { system = "Windows"; model = "PC"; }
  else if (/Linux/.test(ua)) { system = "Linux"; model = "PC"; }
  const m = ua.match(/OS (\d+(?:_\d+)*) like Mac OS X/) || ua.match(/Android (\d+(?:\.\d+)*)/) || ua.match(/Mac OS X (\d+(?:[._]\d+)*)/) || ua.match(/Windows NT (\d+\.\d+)/);
  if (m) version = m[1].replace(/_/g, ".");
  if (/Version\/(\d+(?:\.\d+)*).*Safari/.test(ua) && (system === "iOS" || system === "iPadOS")) version = ua.match(/Version\/(\d+(?:\.\d+)*)/)[1];
  // User-Agent Client Hints (Chromium): the real version, and an Android phone's model.
  const hints = nav.userAgentData;
  if (hints?.getHighEntropyValues) {
    try {
      const v = await hints.getHighEntropyValues(["platformVersion", "model"]);
      const p = hints.platform || "";
      if (p === "Windows" && v.platformVersion) {
        // 13 and later: Windows 11; 1 to 10: Windows 10.
        const major = parseInt(v.platformVersion, 10);
        version = major >= 13 ? "11" : major > 0 ? "10" : version;
      } else if (v.platformVersion) version = v.platformVersion;
      if (v.model) model = v.model;
    } catch {}
  }
  if (system === "Android" && !model) model = "Android";
  return JSON.stringify({
    model: model || "Browser", system: system || nav.platform || "Web", systemVersion: version,
    appVersion: h.options.version || "", build: String(h.options.build || ""),
  });
}

let wakeLock = null, wantAwake = false, awakeWatch = false;

async function keepAwake(on) {
  wantAwake = on;
  if (!on) {
    const l = wakeLock;
    wakeLock = null;
    await l?.release().catch(() => {});
    return;
  }
  if (!navigator.wakeLock) throw notOnWeb("device", "keepAwake", "this browser can't keep the screen on (no Screen Wake Lock)");
  if (!awakeWatch) {
    awakeWatch = true;
    // The browser lets the lock go as the page hides: taken again as it shows.
    document.addEventListener("visibilitychange", () => {
      if (wantAwake && !document.hidden && !wakeLock) acquire().catch(() => {});
    });
  }
  if (!wakeLock && !document.hidden) await acquire();
}

async function acquire() {
  const l = await navigator.wakeLock.request("screen");
  if (!wantAwake) { l.release(); return; }
  wakeLock = l;
  l.addEventListener("release", () => { if (wakeLock === l) wakeLock = null; });
}

// --- picking files ---

/** pickFiles asks for files with the browser's picker (limit 0: any number); their copies in the app's caches. */
export function pickFiles(types, limit) {
  return new Promise((resolve) => {
    const i = document.createElement("input");
    i.type = "file";
    if (types.length && !types.some((t) => t === "*/*" || t === "")) i.accept = types.join(",");
    i.multiple = limit !== 1;
    i.style.display = "none";
    let done = false;
    const finish = async (list) => {
      if (done) return;
      done = true;
      i.remove();
      const out = [];
      for (const f of limit > 0 ? list.slice(0, limit) : list) {
        out.push({ path: await fs.saveUpload(f), name: f.name, type: f.type || typeOf(f.name), size: f.size });
      }
      resolve(out);
    };
    i.addEventListener("change", () => finish([...i.files]));
    i.addEventListener("cancel", () => finish([]));
    document.body.appendChild(i);
    i.click();
  });
}

// --- sharing ---

/** decodeShare decodes share.files' args: u32 view, string MIME, UTI, title, u32 count, the paths. */
export function decodeShare(b) {
  const r = new Reader(b);
  const view = r.u32();
  const mime = r.string(), uti = r.string(), title = r.string();
  const n = r.u32();
  const paths = [];
  for (let i = 0; i < n; i++) paths.push(r.string());
  return { view, mime, uti, title, paths };
}

async function shareFiles(b) {
  const a = decodeShare(b);
  const files = a.paths.map((p, i) => {
    const f = fs.fileOf(p, undefined, typeOf(p) || (i === 0 && a.mime) || "application/octet-stream");
    if (!f) throw new Error("no file at " + p);
    return f;
  });
  const data = a.title ? { files, title: a.title } : { files };
  if (navigator.canShare?.(data)) {
    try {
      await navigator.share(data);
    } catch (e) {
      if (e?.name === "AbortError") return; // closed without sharing: done
      if (e?.name !== "NotAllowedError") throw e;
      for (const f of files) download(f); // no gesture left to show the sheet with
    }
    return;
  }
  // No share sheet for files (a desktop browser): saved, as Downloads.
  for (const f of files) download(f);
}

/** download saves a file with the browser's download. */
export function download(file) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(file);
  a.download = file.name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 60000);
}

// --- files from sources (preview, print, compose) ---

/** sourceURL is where a source (a URL, a file of the app's, an asset) loads from. */
function sourceURL(h, src) {
  if (/^(https?|blob|data):/.test(src)) return src;
  if (src.startsWith("file://")) src = decodeURIComponent(src.slice(7));
  if (src.startsWith("/")) return null;
  return new URL(h.assets + "/" + src.replace(/^\.?\//, ""), location.href).href;
}

/** sourceBlob is a source's bytes, as a Blob of type. */
async function sourceBlob(h, src, type) {
  const url = sourceURL(h, src);
  if (url == null) {
    const path = src.startsWith("file://") ? decodeURIComponent(src.slice(7)) : src;
    const b = fs.readFile(path);
    if (!b) throw new Error("no file at " + path);
    return new Blob([b.slice()], { type });
  }
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status} ${r.statusText}`);
  const blob = await r.blob();
  return type && blob.type !== type ? new Blob([blob], { type }) : blob;
}

const viewable = (t) => /^(image|video|audio|text)\//.test(t) || ["application/pdf", "application/json", "application/xml", "application/xhtml+xml"].includes(t);

// --- preview ---

export async function preview(h, b) {
  const a = JSON.parse(dec.decode(b));
  if (!a.files?.length) throw new Error("preview.show: bad arguments");
  const f = a.files[Math.min(Math.max(a.start | 0, 0), a.files.length - 1)];
  const type = f.type || typeOf(f.source);
  if (!viewable(type)) throw new Error("no viewer");
  // The tab opens now, while the user's tap still counts; the file follows.
  const w = window.open("", "_blank");
  if (!w) throw new Error("asuka: the browser blocked the preview's new tab");
  w.opener = null;
  try {
    const url = sourceURL(h, f.source);
    if (url && /^https?:/.test(url)) {
      w.location.href = url;
      return;
    }
    const blob = await sourceBlob(h, f.source, type.startsWith("text/") && !type.includes("charset") ? type + ";charset=utf-8" : type);
    const u = URL.createObjectURL(blob);
    w.location.href = u;
    setTimeout(() => URL.revokeObjectURL(u), 10 * 60000);
  } catch (e) {
    w.close();
    throw e;
  }
}

// --- print ---

const PRINTED_UNKNOWN = new Uint8Array([2]); // print.Unknown: browsers don't say

function pageStyle(a) {
  return `@page { size: ${a.letter ? "letter" : "A4"} ${a.landscape ? "landscape" : "portrait"}; }`;
}

/**
 * printDialog prints w's page and resolves once the print dialog is gone:
 * its afterprint; where print doesn't wait for the dialog (phones), the
 * page coming back in front; and at once where no dialog showed (no
 * beforeprint within a second: printing turned off).
 */
function printDialog(w) {
  return new Promise((resolve) => {
    let opened = false, done = false;
    const finish = () => {
      if (done) return;
      done = true;
      removeEventListener("focus", back);
      document.removeEventListener("visibilitychange", back);
      resolve();
    };
    const back = () => { if (opened && !document.hidden) setTimeout(finish, 300); };
    w.addEventListener("beforeprint", () => { opened = true; }, { once: true });
    w.addEventListener("afterprint", finish, { once: true });
    w.print();
    setTimeout(() => {
      if (!opened) finish();
      else {
        addEventListener("focus", back);
        document.addEventListener("visibilitychange", back);
      }
    }, 1000);
  });
}

/** printFrame prints a hidden iframe once it's loaded (load sets it up), and answers once the dialog is gone. */
function printFrame(load) {
  return new Promise((resolve, reject) => {
    const f = document.createElement("iframe");
    f.setAttribute("aria-hidden", "true");
    f.style.cssText = "position:fixed;right:0;bottom:0;width:0;height:0;border:0;visibility:hidden";
    let settled = false;
    const finish = (err) => {
      if (settled) return;
      settled = true;
      setTimeout(() => f.remove(), 1000);
      err ? reject(err) : resolve(PRINTED_UNKNOWN);
    };
    f.addEventListener("load", async () => {
      const w = f.contentWindow;
      try {
        await w.document.fonts?.ready;
        await Promise.all([...w.document.images].map((i) => (i.complete ? null : new Promise((r) => { i.onload = i.onerror = r; }))));
      } catch {}
      try {
        printDialog(w).then(() => finish());
      } catch (e) {
        finish(new Error("unsupported"));
      }
    }, { once: true });
    document.body.appendChild(f);
    load(f);
  });
}

function printHTML(h, a) {
  let html = String(a.html || "");
  const style = `<style>${pageStyle(a)}</style>` + (a.job ? `<title>${escapeHTML(a.job)}</title>` : "");
  html = /<head[^>]*>/i.test(html) ? html.replace(/<head[^>]*>/i, (m) => m + style) : style + html;
  return printFrame((f) => { f.srcdoc = html; });
}

async function printFile(h, a) {
  const type = a.type || typeOf(a.source);
  if (!viewable(type) || type.startsWith("video/") || type.startsWith("audio/")) throw new Error("unsupported");
  const blob = await sourceBlob(h, a.source, type);
  const title = a.job ? `<title>${escapeHTML(a.job)}</title>` : "";
  if (type === "application/pdf") {
    // The browser's PDF viewer prints it (a page of the same origin: a blob).
    const u = URL.createObjectURL(blob);
    try {
      return await printFrame((f) => { f.src = u; });
    } finally {
      setTimeout(() => URL.revokeObjectURL(u), 60000);
    }
  }
  if (type.startsWith("image/")) {
    const u = URL.createObjectURL(blob);
    const doc = `<!doctype html><html><head>${title}<style>${pageStyle(a)} html,body{margin:0;height:100%} img{display:block;max-width:100%;max-height:100vh;margin:auto;object-fit:contain}</style></head><body><img src="${u}"></body></html>`;
    try {
      return await printFrame((f) => { f.srcdoc = doc; });
    } finally {
      setTimeout(() => URL.revokeObjectURL(u), 60000);
    }
  }
  const text = await blob.text();
  if (type === "text/html" || type === "application/xhtml+xml") return printHTML(h, { ...a, html: text });
  const doc = `<!doctype html><html><head>${title}<style>${pageStyle(a)} pre{white-space:pre-wrap;font:12px ui-monospace,monospace}</style></head><body><pre>${escapeHTML(text)}</pre></body></html>`;
  return printFrame((f) => { f.srcdoc = doc; });
}

function printWebView(h, a) {
  const n = h.nodes.get(a.view);
  const f = n?.frameEl || n?.el?.querySelector?.("iframe");
  if (!f) throw new Error("print: not a WebView on screen");
  let w;
  try {
    w = f.contentWindow;
    void w.document; // another site's page throws here
  } catch {
    throw notOnWeb("print", "webView", "a WebView of another site's page can't be printed: browsers keep sites apart (print.File with its URL prints a file the browser shows)");
  }
  return new Promise((resolve) => {
    let done = false;
    const finish = () => { if (!done) { done = true; resolve(PRINTED_UNKNOWN); } };
    const st = w.document.createElement("style");
    st.textContent = pageStyle(a);
    w.document.head?.appendChild(st);
    printDialog(w).then(() => { st.remove(); finish(); });
  });
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

// --- compose ---

const phone = () => /iPhone|iPod|iPad|Android/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
const apple = () => /iPhone|iPod|iPad|Macintosh/.test(navigator.userAgent);

/** composeCan: mail (a mailto: link: the user's mail app), no composer of the page's own, messages (an sms: link) where there's a messages app. */
export function composeCan() {
  return new Uint8Array([1, 0, phone() || /Macintosh/.test(navigator.userAgent) ? 1 : 0, 0]);
}

function openLink(url) {
  const a = document.createElement("a");
  a.href = url;
  a.rel = "noopener";
  a.click();
}

async function shareWith(h, as, data) {
  const files = [];
  for (const at of as) {
    const blob = await sourceBlob(h, at.source, at.type);
    files.push(new File([blob], at.name, { type: at.type }));
  }
  const d = { ...data, files };
  if (!navigator.canShare?.(d)) return false;
  try {
    await navigator.share(d);
    return new Uint8Array([3]);
  } catch (e) {
    if (e?.name === "AbortError") return new Uint8Array([0]);
    throw e;
  }
}

async function composeMail(h, a) {
  if (a.attachments?.length) {
    // A mailto: link carries no files: the share sheet does, to a mail app.
    const r = await shareWith(h, a.attachments, { title: a.subject || undefined, text: a.text || undefined });
    if (r) return r;
    throw notOnWeb("compose", "Mail", "a mailto: link carries no attachments, and this browser can't share files");
  }
  openLink(a.mailto);
  return new Uint8Array([3]); // unknown: the mail app has it
}

/** smsURL is an sms: link to the recipients with the body (iOS and the Mac take it after &, the rest after ?). */
export function smsURL(to, body, isApple) {
  let u = "sms:" + to.map((t) => encodeURIComponent(t).replace(/%2B/g, "+")).join(",");
  if (body) u += (isApple ? "&" : "?") + "body=" + encodeURIComponent(body);
  return u;
}

async function composeMessage(h, a) {
  const body = [a.subject, a.body].filter(Boolean).join("\n");
  if (a.attachments?.length) {
    const r = await shareWith(h, a.attachments, { text: body || undefined });
    if (r) return r;
    throw notOnWeb("compose", "Message", "an sms: link carries no attachments, and this browser can't share files");
  }
  if (!composeCan()[2]) throw new Error("unavailable");
  openLink(smsURL(a.to || [], body, apple()));
  return new Uint8Array([3]);
}

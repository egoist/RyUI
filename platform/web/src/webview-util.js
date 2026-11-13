// The WebView's pure helpers (no DOM): what Block blocks, what a frame
// can show, a download's file name. Tested in node (test/webview.test.js).

/** decodeStrings reads Block's bytes: u32 count, then u32-length UTF-8 strings. */
export function decodeStrings(b) {
  if (!b || b.length < 4) return [];
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const dec = new TextDecoder();
  const n = v.getUint32(0, true);
  const out = [];
  let p = 4;
  for (let i = 0; i < n && p + 4 <= b.length; i++) {
    const l = v.getUint32(p, true);
    p += 4;
    if (p + l > b.length) break;
    out.push(dec.decode(b.subarray(p, p + l)));
    p += l;
  }
  return out;
}

/**
 * blocked reports whether one of Block's patterns blocks url: a pattern
 * with no / is a host, which matches it and its subdomains; one with a /
 * is a part of the URL.
 */
export function blocked(patterns, url) {
  if (!patterns.length || !url) return false;
  let host = "";
  try { host = new URL(url).hostname.toLowerCase(); } catch {}
  for (const p of patterns) {
    if (!p) continue;
    if (p.includes("/")) {
      if (url.includes(p)) return true;
    } else {
      const q = p.toLowerCase();
      if (host === q || host.endsWith("." + q)) return true;
    }
  }
  return false;
}

/** mimeOf is a Content-Type's type, lower case, without its parameters. */
export const mimeOf = (ct) => (ct || "").split(";")[0].trim().toLowerCase();

/** isHTML reports whether a Content-Type is a page Asuka loads itself (its bridge and Preload before the page's scripts). */
export function isHTML(ct) {
  const m = mimeOf(ct);
  return m === "" || m === "text/html" || m === "application/xhtml+xml";
}

/** canShow reports whether a frame shows a response of this type, rather than download it. */
export function canShow(ct) {
  const m = mimeOf(ct);
  if (m === "" || m.startsWith("text/") || m.startsWith("image/") || m.startsWith("audio/") || m.startsWith("video/")) return true;
  if (m.endsWith("+xml") || m.endsWith("+json")) return true;
  return ["application/pdf", "application/json", "application/xml", "application/javascript", "application/xhtml+xml"].includes(m);
}

/** isAttachment reports whether a Content-Disposition makes the response a file to save. */
export const isAttachment = (cd) => /^\s*attachment/i.test(cd || "");

/** filenameOf is a download's file name: Content-Disposition's, else the URL's last part. */
export function filenameOf(cd, url) {
  if (cd) {
    let m = /filename\*\s*=\s*(?:UTF-8|utf-8)?''([^;]+)/.exec(cd);
    if (m) {
      try { return decodeURIComponent(m[1].trim().replace(/^"|"$/g, "")); } catch {}
    }
    m = /filename\s*=\s*("([^"]*)"|[^;]+)/i.exec(cd);
    if (m) return (m[2] ?? m[1]).trim();
  }
  try {
    const p = new URL(url).pathname;
    const last = decodeURIComponent(p.slice(p.lastIndexOf("/") + 1));
    if (last) return last;
  } catch {}
  return "download";
}

/** sizeOf is a Content-Length's size, -1 if unknown. */
export function sizeOf(cl) {
  if (cl == null || cl === "") return -1;
  const n = Number(cl);
  return Number.isFinite(n) && n >= 0 ? n : -1;
}

/** cookieHeader is document.cookie's cookies, as a Cookie header ("a=1; b=2"). */
export function cookieHeader(cookie) {
  return (cookie || "").split(";").map((c) => c.trim()).filter(Boolean).join("; ");
}

/** toJSON is a script's value as JSON: null for undefined (and what JSON can't say, a function). */
export function toJSON(v, JSONof = JSON) {
  const j = JSONof.stringify(v === undefined ? null : v);
  return j === undefined ? "null" : j;
}

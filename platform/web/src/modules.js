// Native modules (ui.Call): the ones a browser has. A method answers once,
// with Result on the root: u32 call id, u8 ok, then the result's bytes (an
// error's message when not ok). A module the web host doesn't have
// answers with an error that says so.

import { Events, Reader, Writer } from "./ops.js";
import * as fs from "./fs.js";

const enc = new TextEncoder();
const dec = new TextDecoder();

export class Modules {
  constructor(h) {
    this.h = h;
    this.registered = new Map(); // ryui.registerModule's
    this.wakeLock = null;
  }

  call(root, args) {
    const r = new Reader(args);
    const id = r.u32();
    const module = r.string(), method = r.string();
    const body = args.slice(r.p);
    const reply = (result, err) => {
      const w = new Writer().u32(id).u8(err ? 0 : 1);
      if (err) w.raw(enc.encode(String(err.message || err)));
      else if (result) w.raw(result instanceof Uint8Array ? result : enc.encode(String(result)));
      this.h.dispatch(Events.result(root.id, w.done()));
    };
    const m = this.registered.get(module)?.[method] || this.builtin(module)?.[method];
    if (!m) {
      reply(null, new Error(`the web host has no ${module}.${method}`));
      return;
    }
    // Answered once the core has returned: never inside the batch.
    this.h.afterCore(() => {
      Promise.resolve()
        .then(() => m(body))
        .then((v) => reply(v, null), (e) => reply(null, e));
    });
  }

  builtin(name) {
    const prefix = this.h.prefix();
    switch (name) {
      // A browser has no keychain: the values are the page's own, in its
      // origin's storage, as a web app keeps a session's tokens.
      case "secureStore":
        return {
          get: (b) => {
            const v = localStorage.getItem(prefix + "secure." + dec.decode(b));
            if (v == null) return new Uint8Array([0]);
            const bytes = Uint8Array.from(atob(v), (c) => c.charCodeAt(0));
            const out = new Uint8Array(bytes.length + 1);
            out[0] = 1;
            out.set(bytes, 1);
            return out;
          },
          set: (b) => {
            const r = new Reader(b);
            const n = r.u32();
            const key = dec.decode(b.subarray(4, 4 + n));
            const value = b.subarray(4 + n);
            localStorage.setItem(prefix + "secure." + key, btoa(String.fromCharCode(...value)));
          },
          delete: (b) => { localStorage.removeItem(prefix + "secure." + dec.decode(b)); },
        };
      case "device":
        return {
          info: () => {
            const ua = navigator.userAgentData;
            const system = ua?.platform || (/Mac/.test(navigator.platform) ? "macOS" : /Win/.test(navigator.platform) ? "Windows" : /iPhone|iPad/.test(navigator.userAgent) ? "iOS" : /Android/.test(navigator.userAgent) ? "Android" : navigator.platform);
            return JSON.stringify({ model: "Browser", system, systemVersion: "", appVersion: this.h.options.version || "", build: String(this.h.options.build || "") });
          },
          clipboard: async () => {
            try {
              const t = await navigator.clipboard.readText();
              const b = enc.encode(t);
              const out = new Uint8Array(b.length + 1);
              out[0] = 1;
              out.set(b, 1);
              return out;
            } catch {
              return new Uint8Array([0]);
            }
          },
          keepAwake: async (b) => {
            const on = b[0] === 1;
            if (!on) { await this.wakeLock?.release(); this.wakeLock = null; return; }
            if (!this.wakeLock && navigator.wakeLock) this.wakeLock = await navigator.wakeLock.request("screen");
          },
        };
      case "browser":
        return { open: (b) => { window.open(dec.decode(b), "_blank", "noopener"); } };
      case "photos":
        return { pick: (b) => this.pickFiles("image/*", b[0] || 1).then((ps) => ps.join("\n")) };
      case "share":
        return {
          files: async (b) => {
            const paths = dec.decode(b).split("\n").filter(Boolean);
            const files = paths.map((p) => fs.fileOf(p)).filter(Boolean);
            if (navigator.canShare?.({ files })) await navigator.share({ files });
            else for (const f of files) download(f);
          },
        };
    }
    return null;
  }

  /** pickFiles asks for files with the browser's picker; their copies' paths in the app's caches. */
  pickFiles(accept, limit) {
    return new Promise((resolve) => {
      const i = document.createElement("input");
      i.type = "file";
      i.accept = accept;
      i.multiple = limit > 1;
      i.addEventListener("change", async () => {
        const out = [];
        for (const f of [...i.files].slice(0, limit)) out.push(await fs.saveUpload(f));
        resolve(out);
      });
      i.addEventListener("cancel", () => resolve([]));
      i.click();
    });
  }
}

function download(file) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(file);
  a.download = file.name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 10000);
}

// Crash reports (asuka/crash) in the browser: the web host's side of the
// run's record (internal/crashrun, which an app that imports asuka/crash
// starts as the host does).
//
//   - Go's crash output. On js/wasm Go's runtime can't write it to a file
//     of its own (debug.SetCrashOutput needs dup), so the host keeps what
//     the runtime prints (runtime.wasmWrite, standard error: panics,
//     fatal errors, the builtin print) for as long as Go runs at once.
//     A crash prints and ends the program without returning to the page
//     between, so when the program ends with a nonzero status, what it
//     printed last from "panic: " or "fatal error: " on is the crash: it
//     goes in the run's .crash file (crashrun's begin names it), in the
//     app's files, which IndexedDB keeps; asuka/crash reads it into a
//     report on the next launch.
//   - Which runs live. The app's files are the origin's, shared by its
//     pages (each reads them as it loads), so a page marks its run alive
//     in localStorage, every page of the origin seeing it, while it's
//     open: asuka/crash leaves the runs of the others alone.
//   - Uncaught JavaScript errors (the host's, or the app's web/*.js): an
//     exception report each, not fatal (the page goes on), written to the
//     inbox as the mobile hosts write theirs, and Go told (crashNotify),
//     so OnReport hears of it at once. At most maxErrors a run, each
//     message and stack once.

import * as fs from "./fs.js";

const aliveKey = (path) => "asuka-run:" + path;
const heartbeat = 30_000; // how often a live page marks its run
const stale = 10 * 60_000; // a mark older than this is a page that's gone (a background tab's timers run once a minute at least)
const maxCrash = 8 << 20; // what's kept of a crash's output
const maxErrors = 20;
const markers = /^(panic: |fatal error: )/m;
const enc = new TextEncoder();
const dec = new TextDecoder();

function storage() {
  try {
    return globalThis.localStorage || null;
  } catch {
    return null; // blocked
  }
}

/**
 * Crash keeps the run's record with Go's program go (a Go of wasm_exec.js,
 * before it runs): host is the web host (crashNotify tells its Go side).
 */
export class Crash {
  constructor(go, host) {
    this.go = go;
    this.host = host;
    this.path = ""; // the run's .crash file, once Go began its record
    this.printed = ""; // what the runtime printed while Go last ran
    this.clearing = false;
    this.ended = false;
    this.timer = 0;
    this.errors = 0;
    this.seen = new Set();
    const gojs = go.importObject.gojs;
    const write = gojs["runtime.wasmWrite"];
    gojs["runtime.wasmWrite"] = (sp) => {
      try {
        this.runtimeWrite(sp);
      } catch {}
      write(sp);
    };
    this.onError = (e) => {
      if (e.error !== undefined && e.error !== null) this.exception(e.error);
    };
    this.onRejection = (e) => this.exception(e.reason);
    this.onHide = () => this.mark(false);
    this.onShow = () => this.mark(true);
    addEventListener("error", this.onError);
    addEventListener("unhandledrejection", this.onRejection);
    addEventListener("pagehide", this.onHide);
    addEventListener("pageshow", this.onShow);
  }

  // --- Go's crash output ---

  /** runtimeWrite keeps what Go's runtime writes to standard error (runtime.wasmWrite's arguments). */
  runtimeWrite(sp) {
    sp >>>= 0;
    const mem = this.go.mem;
    const fd = mem.getUint32(sp + 8, true) + mem.getInt32(sp + 12, true) * 4294967296;
    if (fd !== 2) return;
    const p = mem.getUint32(sp + 16, true) + mem.getInt32(sp + 20, true) * 4294967296;
    const n = mem.getInt32(sp + 24, true);
    if (this.printed.length < maxCrash) this.printed += dec.decode(new Uint8Array(mem.buffer, p, n));
    // Go is running now: once it returns to the page, what it printed
    // wasn't a crash (one ends the program before it returns).
    if (!this.clearing) {
      this.clearing = true;
      queueMicrotask(() => {
        this.clearing = false;
        if (!this.go.exited) this.printed = "";
      });
    }
  }

  /** exited: the program ended with status code. A crash goes in the run's .crash file. */
  exited(code) {
    this.ended = true;
    const out = this.printed;
    this.printed = "";
    if (this.path && code !== 0) {
      const i = out.search(markers);
      if (i >= 0) {
        fs.writeFile(this.path, enc.encode(out.slice(i)));
        // The run is marked ended once its crash is kept: a page that
        // closes before then leaves a mark that goes stale.
        clearInterval(this.timer);
        this.timer = 0;
        fs.flush().then(() => this.mark(false));
        return;
      }
    }
    this.mark(false);
  }

  // --- The run's mark ---

  /** begin: Go began the run's record, its crash output at path (crashrun's lock). */
  begin(path) {
    this.path = path;
    this.mark(true);
  }

  /** alive reports whether a page has the run of path marked alive (crashrun's Alive). */
  alive(path) {
    const v = Number(storage()?.getItem(aliveKey(path)));
    return v > 0 && Date.now() - v < stale;
  }

  mark(on) {
    if (!this.path) return;
    if (on && this.ended) return;
    clearInterval(this.timer);
    this.timer = 0;
    const st = storage();
    try {
      if (on && !this.ended) {
        st?.setItem(aliveKey(this.path), String(Date.now()));
        this.timer = setInterval(() => {
          try { st?.setItem(aliveKey(this.path), String(Date.now())); } catch {}
        }, heartbeat);
      } else {
        st?.removeItem(aliveKey(this.path));
      }
    } catch {}
  }

  // --- Uncaught JavaScript errors ---

  /** exception writes an uncaught error (a thrown value) to the inbox as an exception report. */
  exception(err) {
    // Once Go has ended, each call into it throws: the crash is reported.
    if (!this.path || this.ended || this.go.exited || this.errors >= maxErrors) return;
    const isError = err instanceof Error || (err && typeof err === "object" && "message" in err);
    const name = isError ? String(err.name || "Error") : "Uncaught";
    const reason = isError ? String(err.message ?? "") : String(err);
    const stack = isError ? stackText(err) : "";
    const key = name + "\n" + reason + "\n" + stack;
    if (this.seen.has(key)) return;
    this.seen.add(key);
    this.errors++;
    const rec = {
      time: Date.now(), pid: globalThis.process?.pid ?? 0, thread: "main",
      name, reason, stack, frames: frames(stack), fatal: false,
      run: this.path.replace(/^.*\//, "").replace(/\.crash$/, ""), // this run's: its context
    };
    const dir = this.path.replace(/\/runs\/[^/]*$/, "/inbox");
    const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 10);
    fs.writeFile(`${dir}/exception-web-${id}.json`, enc.encode(JSON.stringify(rec)));
    this.host.afterCore(() => this.host.core?.crashNotify?.());
  }

  /** info is the browser and its system, for the run's context. */
  info() {
    return { model: systemOf(navigator), systemVersion: browserOf(navigator) };
  }
}

/** stackText is an error's stack without its first line where that repeats the message (V8's). */
function stackText(err) {
  const s = String(err.stack || "");
  const head = `${err.name}: ${err.message}`;
  return s.startsWith(head) ? s.slice(head.length).replace(/^\n/, "") : s;
}

/** frames reads a stack's frames: V8's "at f (url:1:2)", Firefox's and Safari's "f@url:1:2". */
export function frames(stack) {
  const out = [];
  for (let l of stack.split("\n")) {
    l = l.trim();
    let m = /^at (?:(.*?) \()?(.*?):(\d+):\d+\)?$/.exec(l);
    if (m) {
      out.push(frame(m[1] || "", m[2], m[3]));
      continue;
    }
    m = /^(.*?)@(.*?):(\d+):\d+$/.exec(l);
    if (m) out.push(frame(m[1], m[2], m[3]));
  }
  return out;
}

function frame(fn, file, line) {
  const f = {};
  if (fn) f.function = fn;
  if (file) f.file = file;
  const n = Number(line);
  if (n > 0) f.line = n;
  return f;
}

/** systemOf is the system the browser runs on, and its version where the browser says it: "macOS 15.1", "Android 14", "Windows". */
export function systemOf(nav) {
  const ua = String(nav?.userAgent || "");
  let m;
  if ((m = /\b(?:iPhone|iPad|iPod).*? OS (\d+(?:_\d+)*)/.exec(ua))) return "iOS " + m[1].replace(/_/g, ".");
  if ((m = /Android (\d+(?:\.\d+)*)/.exec(ua))) return "Android " + m[1];
  if (/Windows/.test(ua)) return "Windows";
  if (/CrOS/.test(ua)) return "ChromeOS";
  if ((m = /Mac OS X (\d+(?:[._]\d+)*)/.exec(ua))) {
    // Browsers freeze macOS's version at 10.15.7: only an older one is real.
    const v = m[1].replace(/_/g, ".");
    return v === "10.15.7" ? "macOS" : "macOS " + v;
  }
  if (/Linux/.test(ua)) return "Linux";
  return String(nav?.userAgentData?.platform || nav?.platform || "");
}

/** browserOf is the browser and its major version: "Chrome 131", "Safari 18.1", "Firefox 133". */
export function browserOf(nav) {
  const ua = String(nav?.userAgent || "");
  const brands = nav?.userAgentData?.brands;
  if (Array.isArray(brands)) {
    const b = brands.find((b) => !/Not.?A.?Brand|Chromium/i.test(b.brand)) || brands.find((b) => /Chromium/i.test(b.brand));
    if (b) return `${b.brand.replace(/^Google /, "")} ${b.version}`;
  }
  let m;
  if ((m = /Edg(?:e|A|iOS)?\/(\d+)/.exec(ua))) return "Edge " + m[1];
  if ((m = /Firefox\/(\d+)/.exec(ua)) || (m = /FxiOS\/(\d+)/.exec(ua))) return "Firefox " + m[1];
  if ((m = /(?:Chrome|CriOS)\/(\d+)/.exec(ua))) return "Chrome " + m[1];
  if ((m = /Version\/(\d+(?:\.\d+)?).*Safari/.exec(ua))) return "Safari " + m[1];
  return "";
}

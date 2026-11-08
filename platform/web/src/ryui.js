// RyUI's web loader: it starts an app's Go code (compiled to WebAssembly)
// with the web host on a page.
//
//	import { start } from "./ryui/ryui.js";
//	start({ wasm: "app.wasm", container: document.body });
//
// Options: container (the element the app fills; the page's body by
// default, the app then filling the page), wasm (its URL), exec (the URL
// of the Go release's wasm_exec.js; "wasm_exec.js" beside this file by
// default), assets (where its assets are served: "assets"), name (the
// app's: its stored data is kept under it), fonts (the app's font files,
// assets/fonts), env (more environment variables for the Go code), dev
// (ryui dev's: the page reloads with each build, keeping the app's state,
// and the app's logs go to the terminal).

import { Host } from "./host.js";
import * as fs from "./fs.js";

let hosts = 0;
const natives = new Map();
const modules = new Map();

/** registerNative gives the page a native view kind (ui.Native): factory(ctx) returns { el, set(name, value), measure?(c), destroy?() }; ctx.emit(name, values) sends its events. */
export function registerNative(kind, factory) { natives.set(kind, factory); }

/** registerModule gives the page a native module (ui.Call): methods by name, each taking the call's bytes and returning (or resolving to) the result's. */
export function registerModule(name, methods) { modules.set(name, methods); }

export async function start(options = {}) {
  const fullPage = !options.container;
  const container = options.container || document.body.appendChild(document.createElement("div"));
  if (fullPage) container.classList.add("ryui-page");
  const name = options.name || "app";
  const dev = options.dev ? devClient(options.dev) : null;
  const data = "/data/" + name.replace(/[^\w.-]/g, "_");
  await fs.mount({ name, dir: data, output: dev?.log });
  const execURL = options.exec || new URL("wasm_exec.js", import.meta.url).href;
  if (!globalThis.Go) await loadScript(execURL);
  const host = new Host(container, { ...options, fullPage, name });
  for (const [k, f] of natives) host.natives.set(k, f);
  for (const [k, m] of modules) host.modules.registered.set(k, m);
  const hostName = "ryuiHost" + (hosts++ || "");
  globalThis[hostName] = host;
  const go = new globalThis.Go();
  go.argv = [name];
  go.env = { RYUI_WEB_HOST: hostName, RYUI_DATA: data, HOME: "/home", TMPDIR: "/tmp", TZ: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", ...(options.env || {}) };
  const res = fetch(options.wasm || "app.wasm");
  let instance;
  try {
    const r = await WebAssembly.instantiateStreaming(res, go.importObject).catch(async (e) => {
      // A server that doesn't send application/wasm.
      const bytes = await (await fetch(options.wasm || "app.wasm")).arrayBuffer();
      return WebAssembly.instantiate(bytes, go.importObject);
    });
    instance = r.instance;
  } catch (e) {
    showError(container, "The app couldn't load: " + e.message);
    throw e;
  }
  host.attach(instance.exports.mem);
  if (dev) dev.attach(host);
  go.run(instance).then(() => {
    if (go.exitCode) showError(container, dev?.lastError() || `The app stopped (exit status ${go.exitCode}).`);
  });
  return host;
}

function loadScript(src) {
  return new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = src;
    s.onload = resolve;
    s.onerror = () => reject(new Error("can't load " + src));
    document.head.appendChild(s);
  });
}

function showError(container, msg) {
  const d = document.createElement("pre");
  d.className = "ryui-error";
  d.textContent = msg;
  container.appendChild(d);
}

/** devClient is ryui dev's side of the page: logs to the terminal, reloads with each build. */
function devClient(url) {
  const base = typeof url === "string" ? url : "/__ryui/";
  let pending = "";
  let timer = 0;
  let errorText = "";
  const send = () => {
    timer = 0;
    const body = pending;
    pending = "";
    if (body) fetch(base + "log", { method: "POST", body, keepalive: true }).catch(() => {});
  };
  const log = (fd, text) => {
    if (fd === 2) errorText += text;
    pending += (fd === 2 ? "\x02" : "\x01") + text;
    if (!timer) timer = setTimeout(send, 30);
  };
  addEventListener("error", (e) => log(2, `page: ${e.message} (${e.filename}:${e.lineno})\n${e.error?.stack || ""}\n`));
  addEventListener("unhandledrejection", (e) => log(2, `page: ${e.reason?.stack || e.reason}\n`));
  let host = null;
  const events = new EventSource(base + "events");
  events.addEventListener("reload", () => {
    host?.saveState();
    fs.flush();
    send();
    location.reload();
  });
  events.addEventListener("failed", (e) => {
    let d = document.querySelector(".ryui-build-error");
    if (!d) {
      d = document.createElement("pre");
      d.className = "ryui-error ryui-build-error";
      document.body.appendChild(d);
    }
    d.textContent = e.data;
  });
  events.addEventListener("fixed", () => document.querySelector(".ryui-build-error")?.remove());
  // GUI tests' commands (e2e/), as the mobile hosts' debug builds take them.
  events.addEventListener("automation", async (e) => {
    const { id, commands } = JSON.parse(e.data);
    let out = "", error = "";
    try {
      const { run } = await import("./automation.js");
      out = await run(host, commands);
    } catch (err) {
      error = String(err?.message || err);
    }
    fetch(base + "automation/done", { method: "POST", body: JSON.stringify({ id, out, error }) }).catch(() => {});
  });
  return {
    log,
    attach(h) { host = h; },
    lastError: () => errorText.trim().split("\n").slice(-30).join("\n"),
  };
}

// webfs_exec runs a Go program compiled for js/wasm under Node, as Go's
// go_js_wasm_exec does, but with the web host's file system in memory
// (platform/web/src/fs.js) as its files, as in a browser: so that
//
//	GOOS=js GOARCH=wasm go test -exec "node $PWD/sdk/watch/testdata/webfs_exec.mjs" ./sdk/watch
//
// tests asuka/sdk/watch against what a web app's watch sees.
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { webcrypto } from "node:crypto";
import { mount } from "../../platform/web/src/fs.js";

// Node's process, before the page's (mount's) takes its place.
const node = globalThis.process;
if (node.argv.length < 3) {
  console.error("usage: webfs_exec.mjs [wasm binary] [arguments]");
  node.exit(1);
}
const binary = readFileSync(node.argv[2]);
const goroot = node.env.GOROOT || execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();
globalThis.crypto ??= webcrypto;
globalThis.process = {};
await mount({ name: "webfs_exec" });
createRequire(import.meta.url)(goroot + "/lib/wasm/wasm_exec.js");

const go = new globalThis.Go();
go.argv = node.argv.slice(2);
go.env = { TMPDIR: "/tmp", HOME: "/home" };
go.exit = (code) => node.exit(code);
const { instance } = await WebAssembly.instantiate(binary, go.importObject);
node.on("exit", (code) => {
  if (code === 0 && !go.exited) {
    // A deadlock: Go prints it and its goroutines.
    go._pendingEvent = { id: 0 };
    go._resume();
  }
});
await go.run(instance);

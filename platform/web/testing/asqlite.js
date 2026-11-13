// asqlite.wasm for the tests, the sqlite plugin's: this checkout's build
// (go generate ./plugins/sqlite, in plugins/sqlite/build/wasm), else the
// published module (the plugin's manifest, asuka-plugin.json), from the
// user's cache, where package plugin keeps it, downloaded and verified
// there first if need be.

import { createHash } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { gunzipSync } from "node:zlib";

const dir = new URL("../../../plugins/sqlite/", import.meta.url);

/** cacheDir is Go's os.UserCacheDir. */
function cacheDir() {
  switch (process.platform) {
    case "darwin": return join(homedir(), "Library", "Caches");
    case "win32": return process.env.LocalAppData;
    default: return process.env.XDG_CACHE_HOME || join(homedir(), ".cache");
  }
}

const sha256 = (b) => createHash("sha256").update(b).digest("hex");

/** loadWasm is the module's bytes. */
export async function loadWasm() {
  try {
    return await readFile(new URL("build/wasm/asqlite.wasm", dir));
  } catch {}
  const manifest = JSON.parse(await readFile(new URL("asuka-plugin.json", dir), "utf8"));
  const lib = manifest.libraries.find((l) => l.name === "asqlite");
  const f = lib.files.wasm;
  const path = join(cacheDir(), "asuka", "plugins", f.rawSHA256.slice(0, 16), f.name);
  try {
    const b = await readFile(path);
    if (b.length === f.rawSize) return b;
  } catch {}
  const resp = await fetch(`${lib.release}/${f.asset}`);
  if (!resp.ok) throw new Error(`${f.asset}: ${resp.status}`);
  const gz = Buffer.from(await resp.arrayBuffer());
  if (sha256(gz) !== f.sha256) throw new Error(`${f.asset}: not the file the manifest names`);
  const raw = gunzipSync(gz);
  if (sha256(raw) !== f.rawSHA256) throw new Error(`${f.asset}: not the module the manifest names`);
  await mkdir(dirname(path), { recursive: true });
  const tmp = `${path}.${process.pid}.tmp`;
  await writeFile(tmp, raw);
  await rename(tmp, path);
  return raw;
}

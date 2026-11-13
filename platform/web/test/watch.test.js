// The app's file system's fs.watch (asuka/watch on the web): what changes
// in a directory, told a moment after the call that changed it; inode
// numbers kept as an entry is renamed.
import { test } from "node:test";
import assert from "node:assert/strict";
import { memfs, writeFile, makeDir } from "../src/fs.js";

const call = (f, ...args) => new Promise((resolve, reject) => f.call(memfs, ...args, (e, v) => (e ? reject(e) : resolve(v))));
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

test("watch tells what changed in a directory, after the call", async () => {
  makeDir("/w/a/deep");
  const heard = [];
  const w = memfs.watch("/w/a", { recursive: false }, (type, name) => heard.push([type, name]));
  writeFile("/w/a/f", new Uint8Array([1]));
  assert.deepEqual(heard, [], "told at once");
  await tick();
  assert.deepEqual(heard, [["rename", "f"]]);
  heard.length = 0;
  const fd = await call(memfs.open, "/w/a/f", memfs.constants.O_WRONLY, 0);
  memfs.writeSync(fd, new Uint8Array([2, 3]));
  await call(memfs.close, fd);
  await call(memfs.chmod, "/w/a/f", 0o600);
  writeFile("/w/a/deep/g", new Uint8Array()); // deeper: not without recursive
  writeFile("/w/other", new Uint8Array()); // elsewhere
  await call(memfs.rename, "/w/a/f", "/w/a/f2");
  await tick();
  assert.deepEqual(heard, [["change", "f"], ["rename", "f"], ["rename", "f"], ["rename", "f2"]]);
  heard.length = 0;
  w.close();
  writeFile("/w/a/h", new Uint8Array());
  await tick();
  assert.deepEqual(heard, [], "after close");
});

test("watch, recursive, names what changes deeper, and the directory itself", async () => {
  makeDir("/r/x/y");
  const heard = [];
  const w = memfs.watch("/r", { recursive: true }, (type, name) => heard.push([type, name]));
  writeFile("/r/x/y/z", new Uint8Array([1]));
  await call(memfs.unlink, "/r/x/y/z");
  await call(memfs.rmdir, "/r/x/y");
  await call(memfs.rmdir, "/r/x");
  await call(memfs.rmdir, "/r");
  await tick();
  assert.deepEqual(heard, [["rename", "x/y/z"], ["rename", "x/y/z"], ["rename", "x/y"], ["rename", "x"], ["rename", null]]);
  w.close();
  assert.throws(() => memfs.watch("/nowhere", {}, () => {}), { code: "ENOENT" });
});

test("an entry keeps its inode number as it's renamed; a new one has its own", async () => {
  makeDir("/i");
  writeFile("/i/a", new Uint8Array([1]));
  writeFile("/i/b", new Uint8Array([2]));
  const a = (await call(memfs.stat, "/i/a")).ino;
  const b = (await call(memfs.stat, "/i/b")).ino;
  assert.notEqual(a, b);
  await call(memfs.rename, "/i/a", "/i/c");
  assert.equal((await call(memfs.stat, "/i/c")).ino, a);
  // Saved as editors save: the file at b is another.
  await call(memfs.rename, "/i/c", "/i/b");
  assert.equal((await call(memfs.stat, "/i/b")).ino, a);
});

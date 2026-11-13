// asuka/sdk/dialog and asuka/sdk/shell on the web (src/modules/dialog.js), with
// the File System Access API's pickers and handles faked: what's picked
// is copied into the app's files, a folder with all it holds, and the
// file Save gives goes to the handle the user chose once it's written.
import { test } from "node:test";
import assert from "node:assert/strict";
import * as fs from "../src/fs.js";
import { modules, pickerTypes, acceptList } from "../src/modules/dialog.js";

const enc = new TextEncoder();
const dec = new TextDecoder();
const args = (a) => enc.encode(JSON.stringify(a));

// fileHandle and dirHandle are FileSystemHandles of the API's shape.
function fileHandle(name, text, written) {
  return {
    kind: "file", name,
    getFile: async () => new File([text], name, { type: "text/plain" }),
    createWritable: async () => {
      const parts = [];
      return { write: async (b) => parts.push(dec.decode(b)), close: async () => written?.push(parts.join("")) };
    },
  };
}
function dirHandle(name, entries) {
  return { kind: "directory", name, async *entries() { for (const e of entries) yield [e.name, e]; } };
}

// writeGo writes a file as Go's os.WriteFile does: open, write, close.
function writeGo(path, text) {
  const { O_WRONLY, O_CREAT, O_TRUNC } = fs.memfs.constants;
  const fd = new Promise((res, rej) => fs.memfs.open(path, O_WRONLY | O_CREAT | O_TRUNC, 0o644, (e, fd) => (e ? rej(e) : res(fd))));
  return fd.then((fd) => {
    const b = enc.encode(text);
    fs.memfs.writeSync(fd, b);
    return new Promise((res) => fs.memfs.close(fd, res));
  });
}
const tick = () => new Promise((r) => setTimeout(r, 5));

test("the filters as the pickers' types and an input's accept", () => {
  const filters = [{ name: "Images", types: ["image/*"] }, { name: "Notes", types: ["text/plain"], extensions: ["md", "png", "asukanote"] }];
  assert.deepEqual(pickerTypes(filters), [
    { description: "Images", accept: { "image/*": [] } },
    { description: "Notes", accept: { "text/plain": [], "text/markdown": [".md"], "image/png": [".png"], "application/octet-stream": [".asukanote"] } },
  ]);
  assert.equal(pickerTypes([{ name: "All", extensions: ["*"] }]), null);
  assert.equal(pickerTypes([]), null);
  assert.deepEqual(acceptList(filters), ["image/*", "text/plain", ".md", ".png", ".asukanote"]);
  assert.deepEqual(acceptList([{ name: "Any", types: ["*/*"] }]), []);
});

test("open copies the files picked in; a cancel is nothing", async () => {
  let asked;
  const win = {
    showOpenFilePicker: async (o) => {
      asked = o;
      return [fileHandle("a.txt", "alpha"), fileHandle("b.txt", "beta")];
    },
  };
  const m = modules({}, win);
  const paths = JSON.parse(await m.dialog.open(args({ multiple: true, filters: [{ name: "Text", types: ["text/plain"] }] })));
  assert.equal(asked.multiple, true);
  assert.deepEqual(asked.types, [{ description: "Text", accept: { "text/plain": [] } }]);
  assert.equal(paths.length, 2);
  assert.match(paths[0], /^\/tmp\/shared\/\d+\/a\.txt$/);
  assert.equal(dec.decode(fs.readFile(paths[1])), "beta");

  // A later dialog starts where a path it gave is.
  await m.dialog.open(args({ start: paths[0] }));
  assert.equal(asked.startIn.name, "a.txt");

  win.showOpenFilePicker = async () => { throw new DOMException("closed", "AbortError"); };
  assert.equal(await m.dialog.open(args({})), null);
  win.showOpenFilePicker = async () => { throw new DOMException("no gesture", "SecurityError"); };
  await assert.rejects(m.dialog.open(args({})), /no gesture/);
});

test("folder copies the folder in, with all it holds", async () => {
  let asked;
  const win = {
    showDirectoryPicker: async (o) => {
      asked = o;
      return dirHandle("Notes", [fileHandle("a.md", "# A"), dirHandle("Old", [fileHandle("b.md", "# B")]), dirHandle("Empty", [])]);
    },
  };
  const m = modules({}, win);
  const [root] = JSON.parse(await m.dialog.folder(args({ multiple: true })));
  assert.match(root, /^\/tmp\/folders\/\d+\/Notes$/);
  assert.equal(asked.types, undefined);
  assert.equal(dec.decode(fs.readFile(root + "/a.md")), "# A");
  assert.equal(dec.decode(fs.readFile(root + "/Old/b.md")), "# B");
  const names = await new Promise((res) => fs.memfs.readdir(root, (e, n) => res(n)));
  assert.deepEqual(names, ["Empty", "Old", "a.md"]);
});

test("save's file goes to the file chosen each time the app writes it", async () => {
  const written = [];
  let asked;
  const win = {
    showSaveFilePicker: async (o) => {
      asked = o;
      return fileHandle("Notes.md", "", written);
    },
  };
  const m = modules({}, win);
  const path = await m.dialog.save(args({ name: "Untitled.md", filters: [{ name: "Markdown", extensions: ["md"] }] }));
  assert.equal(asked.suggestedName, "Untitled.md");
  assert.deepEqual(asked.types, [{ description: "Markdown", accept: { "text/markdown": [".md"] } }]);
  assert.match(path, /^\/tmp\/saved\/\d+\/Notes\.md$/);

  await writeGo(path, "first");
  await tick();
  assert.deepEqual(written, ["first"]);
  // Written to a temporary file and renamed over it, as a careful save does.
  await writeGo(path + ".tmp", "second");
  await new Promise((res) => fs.memfs.rename(path + ".tmp", path, res));
  await tick();
  assert.deepEqual(written, ["first", "second"]);
  // Read, not written: nothing goes.
  await new Promise((res) => fs.memfs.open(path, 0, 0, (e, fd) => fs.memfs.close(fd, res)));
  await tick();
  assert.equal(written.length, 2);

  // Start a file's path the app knows of: its name.
  await m.dialog.save(args({ start: "/data/app/Report.txt" }));
  assert.equal(asked.suggestedName, "Report.txt");

  win.showSaveFilePicker = async () => { throw new DOMException("closed", "AbortError"); };
  assert.equal(await m.dialog.save(args({})), null);
});

test("shell: no file manager on the web", async () => {
  const m = modules({});
  assert.throws(() => m.shell.reveal(enc.encode("/tmp/a")), /^Error: unsupported$/);
  assert.throws(() => m.shell.trash(enc.encode("/tmp/a")), /^Error: unsupported$/);
  await assert.rejects(m.shell.open(enc.encode("/tmp/nothing-here")), /no file at/);
  assert.equal(m.shell.beep(), undefined);
});

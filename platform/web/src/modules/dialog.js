// asuka/dialog and asuka/shell on the web. A browser gives a page files,
// never their paths, so the paths these give are the app's (fs.js):
//
//   - dialog.open: the files copied into the app's caches (/tmp/shared,
//     as a drop or device.PickFiles), each its own name. Through the
//     system's dialog where the browser has the File System Access API
//     (showOpenFilePicker: Chrome, Edge), with the filters as its types;
//     elsewhere the browser's file input.
//   - dialog.folder: the folder copied in, with all it holds
//     (/tmp/folders/<n>/<its name>): showDirectoryPicker, else the
//     file input's folder mode (webkitdirectory). One folder.
//   - dialog.save: a path in the app's caches (/tmp/saved/<n>/<name>)
//     whose file, each time the app has written it, goes to the file the
//     user chose (showSaveFilePicker, which asks before replacing one), or
//     where the browser has no such dialog, is downloaded by its name.
//   - Start: a path one of these gave, whose file or folder the system's
//     dialog starts by (startIn); else none.
//   - shell.open: the file in a new tab (preview's: images, video, audio,
//     PDFs, text); shell.reveal and shell.trash: unsupported, a page has
//     no file manager; shell.beep: nothing.

import * as fs from "../fs.js";
import { download, pickFiles, preview } from "./system.js";
import { typeOf } from "./system-clipboard.js";

const dec = new TextDecoder();
const enc = new TextEncoder();
const unsupported = () => new Error("unsupported");

// handles are the File System Access handles behind paths the dialogs
// gave, for a later dialog's Start.
const handles = new Map();
let folders = 0, saves = 0;

/** modules are dialog and shell; win is the page's window (tests give their own). */
export function modules(h, win = globalThis) {
  return {
    dialog: {
      open: (b) => open(JSON.parse(dec.decode(b)), win),
      save: (b) => save(JSON.parse(dec.decode(b)), win),
      folder: (b) => folder(JSON.parse(dec.decode(b)), win),
    },
    shell: {
      reveal: () => { throw unsupported(); },
      trash: () => { throw unsupported(); },
      open: async (b) => {
        const path = dec.decode(b);
        if (!fs.readFile(path)) throw new Error("no file at " + path);
        try {
          await preview(h, enc.encode(JSON.stringify({ files: [{ source: path, type: typeOf(path) }], start: 0 })));
        } catch (e) {
          throw e?.message === "no viewer" ? new Error("no app") : e;
        }
      },
      beep: () => {},
    },
  };
}

/** pickerTypes are the filters as showOpenFilePicker's and showSaveFilePicker's types; null: any file. */
export function pickerTypes(filters) {
  const out = [];
  for (const f of filters || []) {
    const accept = {};
    for (const t of f.types || []) {
      if (t === "*/*" || t === "*") return null;
      accept[t] ??= [];
    }
    for (const e of f.extensions || []) {
      if (e === "*") return null;
      (accept[typeOf("x." + e) || "application/octet-stream"] ??= []).push("." + e);
    }
    out.push({ description: f.name, accept });
  }
  return out.length ? out : null;
}

/** acceptList is the filters as a file input's accept: types and extensions; none: any file. */
export function acceptList(filters) {
  const out = [];
  for (const f of filters || []) {
    for (const t of f.types || []) {
      if (t === "*/*" || t === "*") return [];
      out.push(t);
    }
    for (const e of f.extensions || []) {
      if (e === "*") return [];
      out.push("." + e);
    }
  }
  return out;
}

// pickerOptions are what the system's dialogs share: the types, where
// they start.
function pickerOptions(a) {
  const o = {};
  const types = pickerTypes(a.filters);
  if (types) o.types = types;
  const start = a.start && handles.get(fs.clean(a.start));
  if (start) o.startIn = start;
  return o;
}

// picked is a dialog's answer, null when the user closed it.
async function picked(f) {
  try {
    return await f();
  } catch (e) {
    if (e?.name === "AbortError") return null;
    throw e;
  }
}

async function open(a, win) {
  if (!win.showOpenFilePicker) {
    const files = await pickFiles(acceptList(a.filters), a.multiple ? 0 : 1);
    return files.length ? JSON.stringify(files.map((f) => f.path)) : null;
  }
  const hs = await picked(() => win.showOpenFilePicker({ ...pickerOptions(a), multiple: !!a.multiple }));
  if (!hs?.length) return null;
  const paths = [];
  for (const fh of hs) {
    const path = await fs.saveUpload(await fh.getFile());
    handles.set(path, fh);
    paths.push(path);
  }
  return JSON.stringify(paths);
}

async function folder(a, win) {
  const at = `/tmp/folders/${++folders}`;
  if (!win.showDirectoryPicker) {
    const root = await pickFolder(at);
    return root ? JSON.stringify([root]) : null;
  }
  const opts = pickerOptions(a);
  delete opts.types;
  const dh = await picked(() => win.showDirectoryPicker(opts));
  if (!dh) return null;
  const root = `${at}/${safe(dh.name)}`;
  await copyFolder(dh, root);
  handles.set(root, dh);
  return JSON.stringify([root]);
}

/** copyFolder copies a directory handle's files into the app's, at path. */
export async function copyFolder(dh, path) {
  fs.makeDir(path);
  for await (const [name, h] of dh.entries()) {
    const to = `${path}/${safe(name)}`;
    if (h.kind === "directory") await copyFolder(h, to);
    else fs.writeFile(to, new Uint8Array(await (await h.getFile()).arrayBuffer()));
  }
}

// pickFolder is the file input's folder mode: the folder's files, each
// at its path in it, copied in at at; the folder's path, or null (a
// cancel, or an empty folder, whose name the input doesn't say).
function pickFolder(at) {
  return new Promise((resolve) => {
    const i = document.createElement("input");
    i.type = "file";
    i.webkitdirectory = true;
    i.style.display = "none";
    let done = false;
    const finish = async (list) => {
      if (done) return;
      done = true;
      i.remove();
      let root = null;
      for (const f of list) {
        const rel = (f.webkitRelativePath || f.name).split("/").map(safe);
        root ??= `${at}/${rel[0]}`;
        fs.writeFile(`${at}/${rel.join("/")}`, new Uint8Array(await f.arrayBuffer()));
      }
      resolve(root);
    };
    i.addEventListener("change", () => finish([...i.files]));
    i.addEventListener("cancel", () => finish([]));
    document.body.appendChild(i);
    i.click();
  });
}

async function save(a, win) {
  const at = `/tmp/saved/${++saves}`;
  if (!win.showSaveFilePicker) {
    // No dialog: the file's downloaded by its name each time it's written.
    const name = safe(suggested(a) || "Untitled");
    const path = `${at}/${name}`;
    fs.makeDir(at);
    fs.onWritten(path, (b) => download(new File([b], name, { type: typeOf(name) || "application/octet-stream" })));
    return path;
  }
  const opts = pickerOptions(a);
  const name = suggested(a);
  if (name) opts.suggestedName = name;
  const fh = await picked(() => win.showSaveFilePicker(opts));
  if (!fh) return null;
  const path = `${at}/${safe(fh.name)}`;
  fs.makeDir(at);
  handles.set(path, fh);
  // Each write after the one before: the file the user chose has the
  // app's last.
  let writing = Promise.resolve();
  fs.onWritten(path, (b) => {
    writing = writing.then(async () => {
      const w = await fh.createWritable();
      await w.write(b);
      await w.close();
    }).catch((e) => fs.log(`asuka: couldn't write ${fh.name}: ${e?.message ?? e}`));
  });
  return path;
}

// suggested is the name Save suggests: the app's, else Start's (a file's,
// not a folder a dialog gave).
function suggested(a) {
  if (a.name) return a.name;
  if (!a.start || handles.get(fs.clean(a.start))?.kind === "directory") return "";
  return a.start.slice(a.start.lastIndexOf("/") + 1);
}

const safe = (name) => String(name || "file").replace(/[\/\\]/g, "_");

// asuka/sdk/camera, asuka/sdk/camera/record, asuka/sdk/scan, asuka/sdk/vision and
// asuka/sdk/imageedit in the browser.
//
//   - camera.take: a phone's camera app (<input capture>, where the screen
//     is touch), else a dialog of the page's own: the camera's preview
//     (getUserMedia) and a shutter. Answers with a JPEG in the caches, ""
//     if cancelled, or fails "no camera" / "not allowed".
//   - camera.View is the native view "asuka.camera" (cameraView, which the
//     host registers): a <video> of the camera, front or back; "capture"
//     takes a JPEG of what it shows, "record" / "stopRecording" record it
//     with a MediaRecorder (MP4 where the browser records it, else WebM);
//     its "scan" reads codes with the browser's BarcodeDetector, where
//     there is one (Chrome on Android and macOS).
//   - scan.file and vision.analyze: the Shape Detection API
//     (BarcodeDetector; FaceDetector and TextDetector only where the browser
//     turns them on).
//   - imageEdit: decoding with createImageBitmap (turned upright as the
//     image's EXIF says), the actions on a canvas, JPEG or PNG written to
//     the caches; videoFrame seeks a <video> to the time.

import * as fs from "../fs.js";
import { Recording, mediaError, recordingType, sourceURL, uuid } from "./hardware-audio.js";

const dec = new TextDecoder();
const enc = new TextEncoder();

// --- writing images ---

function canvas(w, h) {
  if (typeof OffscreenCanvas !== "undefined") return new OffscreenCanvas(w, h);
  const c = document.createElement("canvas");
  c.width = w;
  c.height = h;
  return c;
}

async function encodeImage(c, png, quality) {
  const type = png ? "image/png" : "image/jpeg";
  const blob = c.convertToBlob
    ? await c.convertToBlob({ type, quality })
    : await new Promise((resolve) => c.toBlob(resolve, type, quality));
  if (!blob) throw new Error("can't write the image");
  return new Uint8Array(await blob.arrayBuffer());
}

/** writeImage writes a canvas (or a source drawable) to the caches: PNG, or JPEG on white. Its path. */
async function writeImage(dir, src, w, h, png, quality = 0.9) {
  let c = src;
  if (!png || typeof src.getContext !== "function") {
    c = canvas(w, h);
    const g = c.getContext("2d");
    if (!png) { g.fillStyle = "#fff"; g.fillRect(0, 0, w, h); }
    g.drawImage(src, 0, 0, w, h);
  }
  const path = `/tmp/${dir}/${uuid()}.${png ? "png" : "jpg"}`;
  fs.writeFile(path, await encodeImage(c, png, quality));
  return path;
}

// --- the camera ---

const facingMode = (f) => (f === "front" ? "user" : "environment");

/** The camera views on the page, newest last: capture and record use the newest. */
const views = [];

/** cameraView is asuka.camera's native view (camera.View): ctx as asuka.registerNative's. */
export function cameraView(ctx) {
  const el = document.createElement("div");
  el.style.cssText = "position:relative;overflow:hidden;background:#000";
  const video = document.createElement("video");
  video.muted = true;
  video.playsInline = true;
  video.autoplay = true;
  video.style.cssText = "width:100%;height:100%;object-fit:cover;display:block";
  el.appendChild(video);
  const v = { video, stream: null, props: { facing: "back", video: false, scan: false, codes: "" }, recording: null, alive: true };
  views.push(v);
  let timer = 0, scanTimer = 0;
  const restart = () => {
    clearTimeout(timer);
    timer = setTimeout(start, 0); // once this batch's props are all set
  };
  const stopStream = () => {
    v.stream?.getTracks().forEach((t) => t.stop());
    v.stream = null;
  };
  const start = async () => {
    if (!v.alive) return;
    stopStream();
    if (!navigator.mediaDevices?.getUserMedia) {
      ctx.emit("error", { message: "there's no camera" });
      return;
    }
    try {
      const s = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: facingMode(v.props.facing), width: { ideal: 1920 }, height: { ideal: 1080 } },
        audio: !!v.props.video,
      });
      if (!v.alive) { s.getTracks().forEach((t) => t.stop()); return; }
      v.stream = s;
      video.srcObject = s;
      video.style.transform = v.props.facing === "front" ? "scaleX(-1)" : "";
      video.play().catch(() => {});
      scan();
    } catch (e) {
      const err = mediaError(e);
      ctx.emit("error", { message: err ? (err.message === "not allowed" ? "the camera isn't allowed" : err.message) : "there's no camera" });
    }
  };
  // Codes: read every quarter second while the view scans; each code once
  // until it's out of sight.
  const seen = new Set();
  const scan = () => {
    clearTimeout(scanTimer);
    if (!v.props.scan || !v.alive || typeof BarcodeDetector === "undefined") return;
    const detector = barcodeDetector(v.props.codes);
    const tick = async () => {
      if (!v.alive || !v.props.scan) return;
      if (video.readyState >= 2) {
        try {
          const found = await (await detector).detect(video);
          const now = new Set();
          for (const c of found) {
            const kind = kindOf(c.format), key = kind + "\n" + c.rawValue;
            now.add(key);
            if (!seen.has(key)) ctx.emit("code", { kind, value: c.rawValue });
          }
          seen.clear();
          for (const k of now) seen.add(k);
        } catch {}
      }
      scanTimer = setTimeout(tick, 250);
    };
    tick();
  };
  restart();
  return {
    el,
    set(name, value) {
      const old = v.props[name];
      v.props[name] = value;
      if (name === "facing" || name === "video") { if (old !== value) restart(); }
      else if (name === "scan" || name === "codes") scan();
    },
    destroy() {
      v.alive = false;
      clearTimeout(timer);
      clearTimeout(scanTimer);
      v.recording?.stop();
      stopStream();
      views.splice(views.indexOf(v), 1);
    },
  };
}

const current = () => [...views].reverse().find((v) => v.alive && v.stream);

/** captureFrame takes a JPEG of what a camera view shows. */
async function captureFrame(v) {
  const w = v.video.videoWidth, h = v.video.videoHeight;
  if (!w || !h) throw new Error("the camera isn't ready");
  return writeImage("asuka-camera", v.video, w, h, false, 0.92);
}

/** takeWithDialog shows the camera's preview in a dialog of the page's own, with a shutter. */
async function takeWithDialog() {
  if (!navigator.mediaDevices?.getUserMedia) throw new Error("no camera");
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment", width: { ideal: 1920 }, height: { ideal: 1080 } } });
  } catch (e) {
    throw mediaError(e) || new Error("no camera");
  }
  return new Promise((resolve, reject) => {
    const d = document.createElement("dialog");
    d.className = "asuka-camera-dialog";
    d.style.cssText = "padding:0;border:0;background:#000;width:min(100vw,960px);max-width:100vw;max-height:100dvh;overflow:hidden";
    const video = document.createElement("video");
    video.muted = video.playsInline = video.autoplay = true;
    video.style.cssText = "display:block;width:100%;max-height:calc(100dvh - 96px);object-fit:contain;background:#000";
    video.srcObject = stream;
    const bar = document.createElement("div");
    bar.style.cssText = "display:flex;align-items:center;justify-content:center;gap:48px;height:96px";
    const shutter = document.createElement("button");
    shutter.setAttribute("aria-label", "Take photo");
    shutter.style.cssText = "width:64px;height:64px;border-radius:50%;border:4px solid #fff;background:#fff;box-shadow:inset 0 0 0 3px #000;cursor:pointer";
    const cancel = document.createElement("button");
    cancel.setAttribute("aria-label", "Cancel");
    cancel.textContent = "✕";
    cancel.style.cssText = "width:44px;height:44px;border-radius:50%;border:0;background:rgba(255,255,255,.2);color:#fff;font-size:20px;cursor:pointer";
    bar.append(cancel, shutter);
    d.append(video, bar);
    document.body.appendChild(d);
    let done = false;
    const end = (path, err) => {
      if (done) return;
      done = true;
      stream.getTracks().forEach((t) => t.stop());
      d.close();
      d.remove();
      err ? reject(err) : resolve(path);
    };
    shutter.onclick = async () => {
      try { end(await writeImage("asuka-camera", video, video.videoWidth, video.videoHeight, false, 0.92)); } catch (e) { end("", e); }
    };
    cancel.onclick = () => end("");
    d.addEventListener("cancel", (e) => { e.preventDefault(); end(""); });
    d.showModal();
    video.play().catch(() => {});
  });
}

/** takeWithInput asks a phone's camera app for a photo (an <input capture>). */
function takeWithInput() {
  return new Promise((resolve) => {
    const i = document.createElement("input");
    i.type = "file";
    i.accept = "image/*";
    i.capture = "environment";
    i.addEventListener("change", async () => {
      const f = i.files?.[0];
      if (!f) return resolve("");
      const ext = f.type === "image/png" ? "png" : f.type === "image/heic" ? "heic" : "jpg";
      const path = `/tmp/asuka-camera/${uuid()}.${ext}`;
      fs.writeFile(path, new Uint8Array(await f.arrayBuffer()));
      resolve(path);
    });
    i.addEventListener("cancel", () => resolve(""));
    i.click();
  });
}

function camera() {
  return {
    take: () => {
      const phone = matchMedia?.("(pointer: coarse)").matches && (navigator.maxTouchPoints || 0) > 0;
      return phone ? takeWithInput() : takeWithDialog();
    },
    capture: () => {
      const v = current();
      if (!v) throw new Error("no camera view");
      return captureFrame(v);
    },
    record: (b) => {
      const v = current();
      if (!v) throw new Error("no camera view");
      if (v.recording) throw new Error("already recording");
      const mute = b[0] === 1;
      const maxMs = b.length >= 5 ? new DataView(b.buffer, b.byteOffset, b.byteLength).getUint32(1, true) : 0;
      if (typeof MediaRecorder === "undefined") throw new Error("asuka: camera.record isn't in this browser");
      const tracks = v.stream.getVideoTracks().concat(mute ? [] : v.stream.getAudioTracks());
      const { type, ext } = recordingType((t) => MediaRecorder.isTypeSupported(t), true);
      return new Promise((resolve, reject) => {
        v.recording = new Recording(new MediaStream(tracks), { type, path: `/tmp/asuka-camera/${uuid()}.${ext}`, video: true, maxMs }, (err, p) => {
          v.recording = null;
          err ? reject(err) : resolve(p);
        });
      });
    },
    stopRecording: () => { for (const v of views) v.recording?.stop(); },
  };
}

// --- codes ---

// asuka/sdk/scan's kinds and the Shape Detection API's formats.
const formats = {
  qr: "qr_code", aztec: "aztec", datamatrix: "data_matrix", pdf417: "pdf417", ean13: "ean_13", ean8: "ean_8",
  upca: "upc_a", upce: "upc_e", code128: "code_128", code39: "code_39", code93: "code_93", codabar: "codabar", itf: "itf",
};
const kinds = Object.fromEntries(Object.entries(formats).map(([k, f]) => [f, k]));
export const kindOf = (format) => kinds[format] || format;

/** formatsOf is the detector's formats for asuka/sdk/scan's kinds (a comma-separated list; empty: all). */
export function formatsOf(list) {
  const ks = (list || "").split(",").map((s) => s.trim()).filter(Boolean);
  return (ks.length ? ks : Object.keys(formats)).map((k) => formats[k]).filter(Boolean);
}

async function barcodeDetector(list) {
  const supported = await BarcodeDetector.getSupportedFormats?.() ?? [];
  const want = formatsOf(list).filter((f) => !supported.length || supported.includes(f));
  return new BarcodeDetector(want.length ? { formats: want } : undefined);
}

/** encodeCodes is asuka/sdk/scan's reply: u32 count, then each code's kind and value, u32-length strings. */
export function encodeCodes(codes) {
  const parts = codes.map((c) => [enc.encode(c.kind), enc.encode(c.value)]);
  let n = 4;
  for (const [k, v] of parts) n += 8 + k.length + v.length;
  const b = new Uint8Array(n);
  const dv = new DataView(b.buffer);
  dv.setUint32(0, codes.length, true);
  let p = 4;
  for (const s of parts.flat()) {
    dv.setUint32(p, s.length, true);
    b.set(s, p + 4);
    p += 4 + s.length;
  }
  return b;
}

// --- decoding images ---

const noImage = () => new Error("no image");

/** sourceBlob reads an image source (a file of the app's, a URL, an asset) as a Blob. */
async function sourceBlob(h, src) {
  if (src.startsWith("file://")) src = decodeURIComponent(src.slice(7));
  if (src.startsWith("/")) {
    const b = fs.readFile(src);
    if (!b) throw noImage();
    return new Blob([b]);
  }
  try {
    const r = await fetch(sourceURL(h, src));
    if (!r.ok) throw noImage();
    return await r.blob();
  } catch {
    throw noImage();
  }
}

async function decodeImage(h, src) {
  const blob = await sourceBlob(h, src);
  try {
    return await createImageBitmap(blob, { imageOrientation: "from-image" });
  } catch {
    throw noImage();
  }
}

function scanModule(h) {
  return {
    file: async (b) => {
      if (typeof BarcodeDetector === "undefined") throw new Error("asuka: scan.file isn't in this browser: it has no BarcodeDetector");
      const s = dec.decode(b);
      const nl = s.indexOf("\n");
      const img = await decodeImage(h, s.slice(nl + 1));
      const found = await (await barcodeDetector(s.slice(0, nl))).detect(img);
      img.close?.();
      return encodeCodes(found.map((c) => ({ kind: kindOf(c.format), value: c.rawValue })));
    },
  };
}

function vision(h) {
  return {
    analyze: async (b) => {
      const o = JSON.parse(dec.decode(b));
      const want = { codes: o.codes && typeof BarcodeDetector !== "undefined", faces: o.faces && typeof FaceDetector !== "undefined", text: o.text && typeof TextDetector !== "undefined" };
      if (!want.codes && !want.faces && !want.text) throw new Error("asuka: vision.analyze isn't in this browser: it has no Shape Detection API (BarcodeDetector, FaceDetector, TextDetector)");
      const img = await decodeImage(h, o.path);
      const W = img.width, H = img.height;
      const box = (r) => ({ x: r.x / W, y: r.y / H, w: r.width / W, h: r.height / H });
      const out = { width: W, height: H, lines: [], faces: [], codes: [] };
      if (want.codes) out.codes = (await (await barcodeDetector("")).detect(img)).map((c) => ({ kind: kindOf(c.format), value: c.rawValue, box: box(c.boundingBox) }));
      if (want.faces) out.faces = (await new FaceDetector({ fastMode: false }).detect(img)).map((f) => box(f.boundingBox));
      if (want.text) out.lines = (await new TextDetector().detect(img)).map((t) => ({ text: t.rawValue, box: box(t.boundingBox), words: [] }));
      img.close?.();
      return JSON.stringify(out);
    },
  };
}

// --- imageEdit ---

/** target is a resize's size: a 0 keeps the aspect ratio from the other. */
export function target(a, w, h) {
  let tw = a.width || 0, th = a.height || 0;
  if (tw <= 0 && th <= 0) return [w, h];
  if (tw <= 0) tw = Math.max(1, Math.round(w * th / h));
  if (th <= 0) th = Math.max(1, Math.round(h * tw / w));
  return [tw, th];
}

/** rotatedSize is the size of a w×h image turned by degrees: whole pixels for quarter turns, enough to hold it otherwise. */
export function rotatedSize(w, h, degrees) {
  const t = degrees * Math.PI / 180;
  const nw = w * Math.abs(Math.cos(t)) + h * Math.abs(Math.sin(t)), nh = w * Math.abs(Math.sin(t)) + h * Math.abs(Math.cos(t));
  return degrees % 90 === 0 ? [Math.round(nw), Math.round(nh)] : [Math.ceil(nw), Math.ceil(nh)];
}

/** pixelate averages squares of block pixels, aligned to the image, within x0..x1, y0..y1 of RGBA data w wide. */
export function pixelate(data, w, h, x0, y0, x1, y1, block) {
  for (let by = Math.floor(y0 / block) * block; by < y1; by += block) {
    const by1 = Math.min(by + block, h);
    for (let bx = Math.floor(x0 / block) * block; bx < x1; bx += block) {
      const bx1 = Math.min(bx + block, w);
      const sum = [0, 0, 0, 0];
      for (let y = by; y < by1; y++) for (let x = bx; x < bx1; x++) {
        const i = (y * w + x) * 4;
        for (let k = 0; k < 4; k++) sum[k] += data[i + k];
      }
      const n = (by1 - by) * (bx1 - bx);
      const avg = sum.map((s) => Math.floor(s / n));
      for (let y = Math.max(by, y0); y < Math.min(by1, y1); y++) for (let x = Math.max(bx, x0); x < Math.min(bx1, x1); x++) {
        const i = (y * w + x) * 4;
        for (let k = 0; k < 4; k++) data[i + k] = avg[k];
      }
    }
  }
}

const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);

/** applyActions does asuka/sdk/imageedit's actions on an image (anything drawImage takes), returning a canvas. */
function applyActions(src, w, h, actions) {
  let c = canvas(w, h);
  c.getContext("2d").drawImage(src, 0, 0, w, h);
  const replace = (nw, nh, draw) => {
    const n = canvas(nw, nh);
    const g = n.getContext("2d");
    g.imageSmoothingQuality = "high";
    draw(g);
    c = n;
  };
  for (const a of actions) {
    const W = c.width, H = c.height;
    switch (a.op) {
      case "resize": {
        const [tw, th] = target(a, W, H);
        if (tw !== W || th !== H) { const old = c; replace(tw, th, (g) => g.drawImage(old, 0, 0, tw, th)); }
        break;
      }
      case "crop": {
        const x = clamp(a.x || 0, 0, W), y = clamp(a.y || 0, 0, H);
        const cw = Math.min(a.width || 0, W - x), ch = Math.min(a.height || 0, H - y);
        if (cw < 1 || ch < 1) throw new Error(`the crop is outside the ${W}×${H} image`);
        const old = c;
        replace(cw, ch, (g) => g.drawImage(old, x, y, cw, ch, 0, 0, cw, ch));
        break;
      }
      case "rotate": {
        const d = (a.degrees || 0) % 360;
        if (d === 0) break;
        const [ow, oh] = rotatedSize(W, H, d);
        const old = c;
        replace(ow, oh, (g) => {
          g.translate(ow / 2, oh / 2);
          g.rotate(d * Math.PI / 180); // clockwise: the canvas's y is down
          g.drawImage(old, -W / 2, -H / 2);
        });
        break;
      }
      case "flipH":
      case "flipV": {
        const old = c;
        replace(W, H, (g) => {
          if (a.op === "flipH") { g.translate(W, 0); g.scale(-1, 1); } else { g.translate(0, H); g.scale(1, -1); }
          g.drawImage(old, 0, 0);
        });
        break;
      }
      case "fill":
      case "pixelate": {
        const x = clamp(a.x || 0, 0, W), y = clamp(a.y || 0, 0, H);
        const x1 = clamp((a.x || 0) + (a.width || 0), 0, W), y1 = clamp((a.y || 0) + (a.height || 0), 0, H);
        if (x1 <= x || y1 <= y) break;
        const g = c.getContext("2d");
        if (a.op === "fill") {
          g.fillStyle = "#" + ((a.rgb || 0) & 0xffffff).toString(16).padStart(6, "0");
          g.fillRect(x, y, x1 - x, y1 - y);
        } else {
          const block = Math.max(a.block || 1, 1);
          // The squares' whole area, so the averages see all they cover.
          const bx = Math.floor(x / block) * block, by = Math.floor(y / block) * block;
          const ex = Math.min(Math.ceil(x1 / block) * block, W), ey = Math.min(Math.ceil(y1 / block) * block, H);
          const img = g.getImageData(bx, by, ex - bx, ey - by);
          pixelate(img.data, ex - bx, ey - by, x - bx, y - by, x1 - bx, y1 - by, block);
          g.putImageData(img, bx, by);
        }
        break;
      }
      default:
        throw new Error(`unknown action ${a.op}`);
    }
  }
  return c;
}

function videoFrame(h, src, atMs) {
  return new Promise((resolve, reject) => {
    let url = sourceURL(h, src.startsWith("file://") ? decodeURIComponent(src.slice(7)) : src);
    if (!url) return reject(noImage());
    const v = document.createElement("video");
    v.muted = true;
    v.preload = "auto";
    v.crossOrigin = "anonymous";
    const fail = () => reject(noImage());
    v.onerror = fail;
    v.onloadedmetadata = () => { v.currentTime = Math.min(Math.max(atMs, 0) / 1000, v.duration || 0); };
    v.onseeked = () => resolve(v);
    v.src = url;
  });
}

function imageEdit(h) {
  const run = async (b, frame) => {
    const r = JSON.parse(dec.decode(b));
    const png = r.format === "png";
    let src, w, ht;
    if (frame) {
      src = await videoFrame(h, r.source, r.at || 0);
      w = src.videoWidth; ht = src.videoHeight;
    } else {
      src = await decodeImage(h, r.source);
      w = src.width; ht = src.height;
    }
    if (!w || !ht) throw noImage();
    const c = applyActions(src, w, ht, r.actions || []);
    src.close?.();
    if (frame) src.removeAttribute("src");
    const path = await writeImage("asuka-images", c, c.width, c.height, png, r.quality ?? 0.9);
    return JSON.stringify({ path, width: c.width, height: c.height });
  };
  return {
    size: async (b) => {
      const r = JSON.parse(dec.decode(b));
      const img = await decodeImage(h, r.source);
      const out = JSON.stringify({ path: "", width: img.width, height: img.height });
      img.close?.();
      return out;
    },
    edit: (b) => run(b, false),
    videoFrame: (b) => run(b, true),
  };
}

export function modules(h) {
  return { camera: camera(), scan: scanModule(h), vision: vision(h), imageEdit: imageEdit(h) };
}

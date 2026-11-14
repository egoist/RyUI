// Run: npm install blurhash@2.0.5 thumbhash@0.1.1, then node gen.mjs internal/imagehash/testdata.
// The reference implementations' outputs (blurhash 2.0.5, thumbhash 0.1.1),
// for internal/imagehash's tests: hashes of made-up images, and their decodings.
import { encode, decode } from "blurhash";
import { rgbaToThumbHash, thumbHashToRGBA } from "thumbhash";
import { writeFileSync } from "node:fs";

function image(w, h, f) {
  const px = new Uint8ClampedArray(w * h * 4);
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
    const [r, g, b, a] = f(x / (w - 1), y / (h - 1));
    px.set([r, g, b, a], (y * w + x) * 4);
  }
  return px;
}
const pictures = [
  ["gradient", 64, 48, (x, y) => [255 * x, 128, 255 * y, 255]],
  ["sunset", 40, 80, (x, y) => [255, 200 * (1 - y), 80 + 100 * x, 255]],
  ["rings", 50, 50, (x, y) => { const d = Math.hypot(x - 0.5, y - 0.5); return [128 + 127 * Math.sin(d * 20), 60, 128 + 127 * Math.cos(d * 13), 255]; }],
  ["fade", 60, 30, (x, y) => [30, 160, 90, 255 * x * (1 - y * 0.5)]],
  ["dot", 32, 32, (x, y) => Math.hypot(x - 0.5, y - 0.5) < 0.3 ? [250, 220, 30, 255] : [20, 20, 80, 0]],
];
const blur = [], thumb = [];
const b64 = (u8) => Buffer.from(u8).toString("base64");
for (const [name, w, h, f] of pictures) {
  const px = image(w, h, f);
  for (const [cx, cy] of [[4, 3], [1, 1], [9, 9], [5, 7]]) {
    const hash = encode(px, w, h, cx, cy);
    for (const [dw, dh] of [[32, 32], [20, 12], [7, 33]]) blur.push({ name, hash, w: dw, h: dh, rgba: b64(decode(hash, dw, dh)) });
  }
  const th = rgbaToThumbHash(w, h, px);
  const d = thumbHashToRGBA(th);
  thumb.push({ name, hash: b64(th), w: d.w, h: d.h, rgba: b64(d.rgba) });
}
// Hashes as apps get them: blurhash's README's, and arbitrary ThumbHash bytes.
for (const hash of ["LEHV6nWB2yk8pyo0adR*.7kCMdnj", "LGF5]+Yk^6#M@-5c,1J5@[or[Q6.", "L6PZfSi_.AyE_3t7t7R**0o#DgR4"])
  blur.push({ name: "readme", hash, w: 32, h: 32, rgba: b64(decode(hash, 32, 32)) });
for (const s of ["1QcSHQRnh493V4dIh4eXh1h4kJUI", "3OcRJYB4d3h/iIeHeEh3eIhw+j2w", "3PcNNYSFeXh/d3eld0iHZoZgVwh2"]) {
  const d = thumbHashToRGBA(Buffer.from(s, "base64"));
  thumb.push({ name: "sample", hash: s, w: d.w, h: d.h, rgba: b64(d.rgba) });
}
writeFileSync(process.argv[2] + "/blurhash.json", JSON.stringify(blur, null, 1) + "\n");
writeFileSync(process.argv[2] + "/thumbhash.json", JSON.stringify(thumb, null, 1) + "\n");

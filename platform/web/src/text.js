// Text: the type ramp, the app's fonts and text measuring helpers.

import { Events } from "./ops.js";

// Text styles (ops.TextStyle): size in CSS pixels at the user's default
// text size, weight, line height. The web's ramp: body 16.
const ramp = [
  [16, 400, 1.375], // Default: Body
  [32, 700, 1.2], // LargeTitle
  [26, 700, 1.25], // Title
  [22, 600, 1.27], // Title2
  [19, 600, 1.3], // Title3
  [16, 600, 1.375], // Headline
  [14, 400, 1.43], // Subheadline
  [16, 400, 1.375], // Body
  [15, 400, 1.4], // Callout
  [13, 400, 1.38], // Footnote
  [12, 400, 1.33], // Caption
  [11, 400, 1.3], // Caption2
];

const weights = [0, 200, 100, 300, 400, 500, 600, 700, 800, 900];

export const systemFont = 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI Variable Text", "Segoe UI", Roboto, "Noto Sans", Ubuntu, Cantarell, "Helvetica Neue", Arial, sans-serif';
const generic = {
  serif: 'ui-serif, "New York", Georgia, "Times New Roman", serif',
  monospace: 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, "Cascadia Mono", Consolas, "Liberation Mono", monospace',
  rounded: 'ui-rounded, "SF Pro Rounded", system-ui, sans-serif',
};

/** style is a node's font: [px at the default text size, weight, line height, family]. */
export function fontOf(h, f) {
  const r = ramp[f.style] || ramp[0];
  const size = f.size > 0 ? f.size : r[0];
  const weight = f.weight > 0 ? weights[f.weight] : r[1];
  let family = systemFont;
  if (f.family) family = familyCSS(h, f.family);
  return [size, weight, r[2], family];
}

/** applyFont sets el's font from n's text props, in rem: it follows the browser's text size. */
export function applyFont(h, n, el, scale = 1) {
  const [size, weight, line, family] = fontOf(h, n.font);
  const s = el.style;
  s.fontSize = (size * scale) / 16 + "rem";
  s.fontWeight = String(weight);
  s.lineHeight = String(line);
  s.fontFamily = family;
}

export function align(a) {
  return ["start", "center", "end", "justify"][a] || "";
}

// The app's fonts (assets/fonts), by file name without its extension; the
// loader passes the list (options.fonts).
const faces = new Map(); // family name -> Promise
const fileWeights = [
  ["thin", 100], ["hairline", 100], ["extralight", 200], ["ultralight", 200], ["light", 300],
  ["regular", 400], ["book", 400], ["normal", 400], ["medium", 500], ["semibold", 600], ["demibold", 600],
  ["extrabold", 800], ["ultrabold", 800], ["bold", 700], ["black", 900], ["heavy", 900],
];

function familyCSS(h, name) {
  if (generic[name]) return generic[name];
  if (/^https?:\/\//.test(name)) return `"ryui-url-${hash(name)}", ${systemFont}`;
  if (h.fontFiles(name).length) return `"ryui-${name}", ${systemFont}`;
  return `"${name.replace(/"/g, "")}", ${systemFont}`;
}

function hash(s) {
  let x = 0;
  for (let i = 0; i < s.length; i++) x = (x * 31 + s.charCodeAt(i)) | 0;
  return (x >>> 0).toString(36);
}

/** loadFamily loads the app's font files a family names, then tells the core to measure again. */
export function loadFamily(h, name) {
  if (!name || generic[name] || faces.has(name)) return;
  let loads = [];
  if (/^https?:\/\//.test(name)) {
    loads = [new FontFace(`ryui-url-${hash(name)}`, `url(${JSON.stringify(name)})`)];
  } else {
    for (const file of h.fontFiles(name)) {
      const base = file.replace(/\.[^.]+$/, "");
      const suffix = base === name ? "" : base.slice(name.length + 1).toLowerCase();
      let weight = 400;
      for (const [w, v] of fileWeights) if (suffix.replace(/italic|oblique/g, "").includes(w)) { weight = v; break; }
      const style = /italic|oblique/.test(suffix) ? "italic" : "normal";
      // A file named for itself is the family, at every weight.
      const desc = base === name ? {} : { weight: String(weight), style };
      loads.push(new FontFace(`ryui-${name}`, `url(${JSON.stringify(h.assets + "/fonts/" + file)})`, desc));
    }
  }
  if (!loads.length) return;
  const p = Promise.all(loads.map((f) => f.load().then((f) => document.fonts.add(f)))).then(
    () => { if (h.root) h.dispatch(Events.fontLoaded(h.root.id, name)); },
    (err) => console.warn("ryui: font", name, err),
  );
  faces.set(name, p);
}

/** linesWidth is the width of an element's widest line of text. */
export function linesWidth(el) {
  const r = document.createRange();
  r.selectNodeContents(el);
  return r.getBoundingClientRect().width;
}

export function horizontalInset(n) {
  return n.inset ? n.inset[1] + n.inset[3] : 0;
}

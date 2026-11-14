// Shapes and canvases. A Shape is an SVG path; a Canvas replays its
// display list with the 2D context, at the display's pixels, again when
// its size or the appearance (semantic colors) changes.

import { Kind, KindProp as P, Reader } from "./ops.js";
import { own } from "./views.js";
import * as color from "./color.js";

const svgNS = "http://www.w3.org/2000/svg";

/** pathData turns Asuka's path encoding (a u8 PathOp, then its f32 arguments, per segment) into SVG path data. */
export function pathData(b) {
  const r = new Reader(b);
  const d = [];
  let cur = null, start = null;
  const f = (x) => +x.toFixed(3);
  while (!r.atEnd) {
    const op = r.u8();
    switch (op) {
      case 0: return d.join(" ");
      case 1: { const x = r.f32(), y = r.f32(); d.push(`M${f(x)} ${f(y)}`); cur = start = [x, y]; break; }
      case 2: { const x = r.f32(), y = r.f32(); d.push(cur ? `L${f(x)} ${f(y)}` : `M${f(x)} ${f(y)}`); if (!cur) start = [x, y]; cur = [x, y]; break; }
      case 3: { const cx = r.f32(), cy = r.f32(), x = r.f32(), y = r.f32(); d.push(`Q${f(cx)} ${f(cy)} ${f(x)} ${f(y)}`); cur = [x, y]; break; }
      case 4: { const a = [r.f32(), r.f32(), r.f32(), r.f32(), r.f32(), r.f32()]; d.push(`C${a.map(f).join(" ")}`); cur = [a[4], a[5]]; break; }
      case 5: d.push("Z"); cur = start; break;
      case 6: { const x = r.f32(), y = r.f32(), w = r.f32(), h = r.f32(); d.push(`M${f(x)} ${f(y)}h${f(w)}v${f(h)}h${f(-w)}Z`); cur = start = [x, y]; break; }
      case 7: {
        const x = r.f32(), y = r.f32(), w = r.f32(), h = r.f32();
        const k = Math.min(r.f32(), w / 2, h / 2);
        d.push(`M${f(x + k)} ${f(y)}H${f(x + w - k)}A${f(k)} ${f(k)} 0 0 1 ${f(x + w)} ${f(y + k)}V${f(y + h - k)}A${f(k)} ${f(k)} 0 0 1 ${f(x + w - k)} ${f(y + h)}H${f(x + k)}A${f(k)} ${f(k)} 0 0 1 ${f(x)} ${f(y + h - k)}V${f(y + k)}A${f(k)} ${f(k)} 0 0 1 ${f(x + k)} ${f(y)}Z`);
        cur = start = [x + k, y];
        break;
      }
      case 8: {
        const x = r.f32(), y = r.f32(), w = r.f32(), h = r.f32();
        const rx = w / 2, ry = h / 2, cx = x + rx;
        d.push(`M${f(cx)} ${f(y)}A${f(rx)} ${f(ry)} 0 1 1 ${f(cx)} ${f(y + h)}A${f(rx)} ${f(ry)} 0 1 1 ${f(cx)} ${f(y)}Z`);
        cur = start = [cx, y];
        break;
      }
      case 9: {
        const cx = r.f32(), cy = r.f32(), rad = r.f32(), a0 = r.f32(), a1 = r.f32(), cw = r.f32() !== 0;
        let delta = cw ? a1 - a0 : a0 - a1;
        const full = Math.abs(delta) >= Math.PI * 2 - 1e-6;
        delta = full ? Math.PI * 2 : ((delta % (Math.PI * 2)) + Math.PI * 2) % (Math.PI * 2);
        const sx = cx + rad * Math.cos(a0), sy = cy + rad * Math.sin(a0);
        d.push(cur ? `L${f(sx)} ${f(sy)}` : `M${f(sx)} ${f(sy)}`);
        if (!cur) start = [sx, sy];
        const pieces = Math.max(1, Math.ceil(delta / Math.PI));
        for (let i = 1; i <= pieces; i++) {
          const a = a0 + (cw ? 1 : -1) * delta * (i / pieces);
          d.push(`A${f(rad)} ${f(rad)} 0 0 ${cw ? 1 : 0} ${f(cx + rad * Math.cos(a))} ${f(cy + rad * Math.sin(a))}`);
        }
        const a = a0 + (cw ? 1 : -1) * delta;
        cur = [cx + rad * Math.cos(a), cy + rad * Math.sin(a)];
        break;
      }
      default: return d.join(" ");
    }
  }
  return d.join(" ");
}

const caps = ["butt", "round", "square"];
const joins = ["miter", "round", "bevel"];

const shape = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-shape";
    const svg = document.createElementNS(svgNS, "svg");
    const path = document.createElementNS(svgNS, "path");
    svg.appendChild(path);
    el.appendChild(svg);
    n.path = path;
    n.svg = svg;
    n.trim = [0, 1];
    n.lineWidth = 1;
    n.dash = null;
    path.style.fill = "none";
  },
  prop(h, n, p, v) {
    const s = n.path.style;
    switch (p) {
      case P.Shape.Path: n.path.setAttribute("d", pathData(v)); break;
      case P.Shape.Fill: n.fill = v; s.fill = h.css(v) || "none"; break;
      case P.Shape.Stroke: n.stroke = v; s.stroke = h.css(v) || "none"; break;
      case P.Shape.LineWidth: n.lineWidth = v; s.strokeWidth = String(v); break;
      case P.Shape.LineCap: s.strokeLinecap = caps[v] || "butt"; break;
      case P.Shape.LineJoin: s.strokeLinejoin = joins[v] || "miter"; break;
      case P.Shape.TrimStart: n.trim[0] = v; break;
      case P.Shape.TrimEnd: n.trim[1] = v; break;
      case P.Shape.Dash: {
        if (v.length < 4) { n.dash = null; break; }
        const r = new Reader(v);
        const phase = r.f32();
        const ls = [];
        while (!r.atEnd) ls.push(r.f32());
        n.dash = { phase, ls };
        break;
      }
    }
    strokeStyle(n);
  },
  resized(h, n) {
    n.svg.setAttribute("viewBox", `0 0 ${n.frame[2]} ${n.frame[3]}`);
    n.svg.setAttribute("width", n.frame[2]);
    n.svg.setAttribute("height", n.frame[3]);
  },
  measure() { return [0, 0]; },
};

/** strokeStyle draws the trimmed part of the outline (pathLength 1), or its dashes. */
function strokeStyle(n) {
  const s = n.path.style;
  const [a, b] = n.trim;
  if (a > 0 || b < 1) {
    n.path.setAttribute("pathLength", "1");
    const len = Math.max(0, b - a);
    s.strokeDasharray = `${len} ${2}`;
    s.strokeDashoffset = String(-a);
  } else if (n.dash) {
    n.path.removeAttribute("pathLength");
    s.strokeDasharray = n.dash.ls.join(" ");
    s.strokeDashoffset = String(n.dash.phase);
  } else {
    n.path.removeAttribute("pathLength");
    s.strokeDasharray = "";
    s.strokeDashoffset = "";
  }
}

const canvases = new Set();

/** redrawCanvases draws every canvas again (the appearance changed: its semantic colors). */
export function redrawCanvases(h) { for (const n of canvases) draw(h, n); }

const canvas = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-canvas";
    const c = document.createElement("canvas");
    el.appendChild(c);
    n.canvas = c;
    n.commands = null;
    canvases.add(n);
  },
  prop(h, n, p, v) {
    if (p === P.Canvas.Commands) {
      n.commands = v.slice();
      draw(h, n);
    }
  },
  resized(h, n) { draw(h, n); },
  destroy(h, n) { canvases.delete(n); return false; },
  measure() { return [0, 0]; },
};

const weights = [0, 200, 100, 300, 400, 500, 600, 700, 800, 900];

function draw(h, n) {
  const c = n.canvas;
  const k = window.devicePixelRatio || 1;
  const w = n.frame[2], hh = n.frame[3];
  const pw = Math.max(1, Math.round(w * k)), ph = Math.max(1, Math.round(hh * k));
  if (c.width !== pw) c.width = pw;
  if (c.height !== ph) c.height = ph;
  c.style.width = w + "px";
  c.style.height = hh + "px";
  const g = c.getContext("2d");
  g.setTransform(1, 0, 0, 1, 0, 0);
  g.clearRect(0, 0, pw, ph);
  if (!n.commands) return;
  g.scale(k, k);
  const r = new Reader(n.commands);
  const path = () => new Path2D(pathData(r.bytes()));
  const col = (v) => color.resolve(h, v, "rgba(0,0,0,0)");
  const alphas = [1];
  const rules = []; // FillRule's, kept with each save
  let rule = "nonzero";
  const font = getComputedStyle(n.el).fontFamily;
  const stops = () => {
    const ns = r.u32();
    const out = [];
    for (let i = 0; i < ns; i++) out.push([r.u64(), r.f32()]);
    return out;
  };
  const paint = (gr, st) => {
    for (const [cl, at] of st) gr.addColorStop(Math.min(Math.max(at, 0), 1), col(cl));
    return gr;
  };
  // A radial gradient from its focus out to its circle: center, radius, focus.
  const radial = (st) => {
    const cx = r.f32(), cy = r.f32(), rad = r.f32(), fx = r.f32(), fy = r.f32();
    return paint(g.createRadialGradient(fx, fy, 0, cx, cy, Math.max(rad, 0)), st);
  };
  const strokeStyle = () => {
    g.lineWidth = r.f32();
    g.lineCap = caps[r.u8()] || "butt";
    g.lineJoin = joins[r.u8()] || "miter";
    const nd = r.u32();
    const phase = r.f32();
    const ds = [];
    for (let i = 0; i < nd; i++) ds.push(r.f32());
    g.setLineDash(ds);
    g.lineDashOffset = phase;
  };
  while (!r.atEnd) {
    const op = r.u8();
    switch (op) {
      case 0: return;
      case 1: g.save(); alphas.push(g.globalAlpha); rules.push(rule); break;
      case 2: g.restore(); alphas.pop(); if (rules.length) rule = rules.pop(); break;
      case 3: g.translate(r.f32(), r.f32()); break;
      case 4: g.scale(r.f32(), r.f32()); break;
      case 5: g.rotate((r.f32() * Math.PI) / 180); break;
      case 6: g.globalAlpha *= r.f32(); break;
      case 7: g.clip(path(), rule); break;
      case 8: { const cl = col(r.u64()); g.fillStyle = cl; g.fill(path(), rule); break; }
      case 9: {
        g.strokeStyle = col(r.u64());
        strokeStyle();
        g.stroke(path());
        g.setLineDash([]);
        break;
      }
      case 10: {
        const st = stops();
        g.fillStyle = paint(g.createLinearGradient(r.f32(), r.f32(), r.f32(), r.f32()), st);
        g.fill(path(), rule);
        break;
      }
      case 12: g.transform(r.f32(), r.f32(), r.f32(), r.f32(), r.f32(), r.f32()); break;
      case 13: rule = r.u8() === 1 ? "evenodd" : "nonzero"; break;
      case 14: {
        const st = stops();
        g.fillStyle = radial(st);
        g.fill(path(), rule);
        break;
      }
      case 15: {
        strokeStyle();
        const kind = r.u8();
        const st = stops();
        g.strokeStyle = kind === 1 ? radial(st) : paint(g.createLinearGradient(r.f32(), r.f32(), r.f32(), r.f32()), st);
        g.stroke(path());
        g.setLineDash([]);
        break;
      }
      case 11: {
        const cl = col(r.u64()), size = r.f32(), weight = r.u8(), align = r.u8(), x = r.f32(), y = r.f32();
        const s = r.string();
        g.fillStyle = cl;
        g.font = `${weights[weight] || 400} ${size}px ${font}`;
        g.textAlign = ["left", "center", "right"][align] || "left";
        g.textBaseline = "alphabetic";
        g.fillText(s, x, y);
        break;
      }
      default: return;
    }
  }
}

export const drawKinds = {
  [Kind.Shape]: shape,
  [Kind.Canvas]: canvas,
};

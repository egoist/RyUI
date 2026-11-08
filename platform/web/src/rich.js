// RichText: styled paragraphs as the browser typesets them (spans in
// paragraphs), so selection, find in page, copying and screen readers
// come with them. Span backgrounds are drawn per line, fitted to the text
// with rounded corners (box-decoration-break), links are links, and an
// inline component sits on the baseline at its run's place: a box of its
// size there, its view placed over it.

import { Kind, KindProp as P, Event, Events, Reader } from "./ops.js";
import { own, lineClamp } from "./views.js";
import * as text from "./text.js";

const weights = [0, 200, 100, 300, 400, 500, 600, 700, 800, 900];

function decodeContent(b) {
  const r = new Reader(b);
  const ns = r.u32();
  const styles = [];
  for (let i = 0; i < ns; i++) {
    styles.push({ style: r.u8(), size: r.f32(), weight: r.u8(), flags: r.u8(), color: r.u64(), background: r.u64(), link: r.string() });
  }
  const paras = [];
  const np = r.u32();
  for (let i = 0; i < np; i++) {
    const runs = [];
    const nr = r.u32();
    for (let j = 0; j < nr; j++) runs.push({ style: r.u32(), text: r.string() });
    paras.push(runs);
  }
  return { styles, paras };
}

function decodeInlines(b) {
  if (!b || b.length < 4) return [];
  const r = new Reader(b);
  const n = r.u32();
  const out = [];
  for (let i = 0; i < n; i++) out.push({ id: r.u32(), w: r.f32(), h: r.f32() });
  return out;
}

function render(h, n) {
  const el = n.text;
  el.textContent = "";
  n.slots = [];
  if (!n.content) return;
  const { styles, paras } = n.content;
  let next = 0;
  for (const runs of paras) {
    const p = document.createElement("div");
    p.className = "ryui-paragraph";
    for (const run of runs) {
      const st = styles[run.style] || { flags: 0 };
      if (st.flags & 16) {
        const inl = n.inlines[next++];
        const slot = document.createElement("span");
        slot.className = "ryui-inline";
        if (inl) { slot.style.width = inl.w + "px"; slot.style.height = inl.h + "px"; slot.dataset.id = inl.id; }
        n.slots.push(slot);
        p.appendChild(slot);
        continue;
      }
      const s = document.createElement(st.link ? "a" : "span");
      s.textContent = run.text;
      if (st.link) {
        s.href = st.link;
        s.addEventListener("click", (e) => {
          e.preventDefault();
          if (n.listening(Event.Link)) h.dispatch(Events.link(n.id, st.link));
          else window.open(st.link, "_blank", "noopener");
        });
      }
      const css = s.style;
      if (st.style || st.size) {
        const [size, weight, line] = text.fontOf(h, { style: st.style || n.font.style, size: st.size || 0, weight: 0, family: "" });
        css.fontSize = size / 16 + "rem";
        if (!st.weight && st.style) css.fontWeight = String(weight);
        css.lineHeight = String(line);
      }
      if (st.weight) css.fontWeight = String(weights[st.weight]);
      if (st.flags & 1) css.fontStyle = "italic";
      if (st.flags & 2) { css.fontFamily = "ui-monospace, 'SF Mono', SFMono-Regular, Menlo, Consolas, monospace"; css.fontSize = css.fontSize || "0.9em"; }
      const deco = [st.flags & 4 ? "underline" : "", st.flags & 8 ? "line-through" : ""].filter(Boolean).join(" ");
      if (deco) css.textDecorationLine = deco;
      if (st.color) css.color = h.css(st.color);
      if (st.background) { css.backgroundColor = h.css(st.background); s.classList.add("ryui-marked"); }
      p.appendChild(s);
    }
    if (!p.childNodes.length) p.appendChild(document.createElement("br"));
    el.appendChild(p);
  }
}

/** placeInlines puts each inline component's view over its box in the text. */
function placeInlines(h, n) {
  if (!n.slots?.length || !n.el.isConnected) return;
  const base = n.el.getBoundingClientRect();
  for (const slot of n.slots) {
    const c = h.nodes.get(Number(slot.dataset.id));
    if (!c?.el) continue;
    const r = slot.getBoundingClientRect();
    c.el.style.left = r.left - base.left + "px";
    c.el.style.top = r.top - base.top + "px";
  }
}

const richText = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "ryui-rich";
    n.text = document.createElement("div");
    n.text.className = "ryui-rich-text";
    el.appendChild(n.text);
    n.inlines = [];
    text.applyFont(h, n, el);
  },
  prop(h, n, p, v) {
    if (p === P.RichText.Content) n.content = decodeContent(v);
    if (p === P.RichText.Inlines) n.inlines = decodeInlines(v);
    render(h, n);
    h.afterCore(() => placeInlines(h, n));
  },
  lines(h, n) { lineClamp(n.text, n.lines); },
  align(h, n) { n.el.style.textAlign = text.align(n.align); },
  insert(h, p, c, i) {
    p.el.appendChild(c.el);
    h.afterCore(() => placeInlines(h, p));
    return true;
  },
  frame(h, n, x, y, w, hh) {
    h.place(n, x, y, w, hh);
    placeInlines(h, n);
    return true;
  },
  childFrame(h, p, c, x, y, w, hh) {
    inlineFrame(h, c, x, y, w, hh);
    placeInlines(h, p);
    return true;
  },
  inset(h, n) { const v = n.inset; n.text.style.padding = `${v[0]}px ${v[1]}px ${v[2]}px ${v[3]}px`; },
  measure(h, n, c) {
    if (!n.content) return [0, 0];
    return h.measureElement(n, n.el, c, { tight: true });
  },
};

// Inline components are placed by the text, not their frames' origins.
const inlineFrame = (h, c, x, y, w, hh) => {
  c.frame = [x, y, w, hh];
  c.el.style.width = w + "px";
  c.el.style.height = hh + "px";
  c.placed = true;
};

export const richKinds = {
  [Kind.RichText]: richText,
};

export { inlineFrame };

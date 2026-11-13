// A Rich TextField: a contenteditable element, from and to the core's
// styled text (RichText's Content encoding). Bold, italic, underline,
// strikethrough, monospaced and links are what the text has, as the
// browser lays it out (its own ⌘B, ⌘I, ⌘U, and the Format and SetLink
// commands change them, through the page's editing commands, so its undo
// covers them); each run's style as the core sent it rides along (an
// index into the field's styles, data-s on its span), so what the user
// didn't change comes back as it was sent. Paragraphs are line breaks
// (<br>), as the field's Enter makes them.

import { Event, Events, Reader, Writer } from "./ops.js";
import * as text from "./text.js";

const weights = [0, 200, 100, 300, 400, 500, 600, 700, 800, 900];
const Semibold = 6, Bold = 7;
const mono = "ui-monospace, 'SF Mono', SFMono-Regular, Menlo, Consolas, monospace";
const enc = new TextEncoder();

const zero = () => ({ style: 0, size: 0, weight: 0, flags: 0, color: 0, bg: 0, link: "" });
const key = (s) => `${s.style}|${s.size}|${s.weight}|${s.flags}|${s.color}|${s.bg}|${s.link}`;

/** format is a style's TextFormat bits (1 bold, 2 italic, 4 underline, 8 strikethrough, 16 monospaced). */
function format(s) {
  return (s.weight >= Semibold ? 1 : 0) | (s.flags & 1 ? 2 : 0) | (s.flags & 4 ? 4 : 0) | (s.flags & 8 ? 8 : 0) | (s.flags & 2 ? 16 : 0);
}

/** makeRich turns n's field into a rich one: its textarea becomes an editable element. */
export function makeRich(h, n, wire) {
  const old = n.input;
  const ed = document.createElement("div");
  ed.className = (old?.className || "asuka-field asuka-control") + " asuka-richfield";
  ed.contentEditable = "true";
  ed.setAttribute("role", "textbox");
  ed.setAttribute("aria-multiline", "true");
  if (old) {
    ed.style.cssText = old.style.cssText;
    for (const a of ["inputmode", "enterkeyhint", "autocapitalize", "autocorrect", "spellcheck"]) {
      const v = old.getAttribute(a);
      if (v != null) ed.setAttribute(a, v);
    }
    if (old.placeholder) ed.dataset.placeholder = old.placeholder;
    old.replaceWith(ed);
  } else {
    n.el.appendChild(ed);
  }
  n.input = n.focusEl = ed;
  n.multiline = true;
  n.richField = { h, styles: [], setting: false, formats: -1, link: "", range: null };
  text.applyFont(h, n, ed);
  ed.appendChild(end());
  wire(ed);

  ed.addEventListener("beforeinput", (e) => {
    // Enter is a line break (a paragraph, to the core), undoable.
    if (e.inputType === "insertParagraph") {
      e.preventDefault();
      if (!document.execCommand("insertLineBreak")) document.execCommand("insertHTML", false, "<br>");
    }
  });
  ed.addEventListener("input", () => changed(h, n));
  ed.addEventListener("paste", (e) => {
    // Files go to the app (PasteTypes); text comes in as text, without another page's styles.
    if (e.defaultPrevented || e.clipboardData?.files?.length) return;
    e.preventDefault();
    const t = e.clipboardData?.getData("text/plain") || "";
    if (t) document.execCommand("insertText", false, t);
  });
  ed.addEventListener("drop", (e) => {
    const t = e.dataTransfer?.getData("text/plain");
    if (!t || e.dataTransfer.files?.length) return;
    e.preventDefault();
    const r = document.caretRangeFromPoint?.(e.clientX, e.clientY);
    if (r) { const s = getSelection(); s.removeAllRanges(); s.addRange(r); }
    document.execCommand("insertText", false, t);
  });
  ed.addEventListener("focus", () => {
    // Back where it was: focusing an editable element can move its caret to the start.
    const f = n.richField;
    if (f.range && ed.contains(f.range.startContainer)) {
      const s = getSelection();
      s.removeAllRanges();
      s.addRange(f.range);
    }
  });
  if (!document.asukaRichSelection) {
    document.asukaRichSelection = true;
    document.addEventListener("selectionchange", () => {
      const s = getSelection();
      const node = s.anchorNode;
      const el = node && (node.nodeType === 1 ? node : node.parentElement)?.closest(".asuka-richfield");
      const m = el?.parentElement?.asukaNode;
      if (!m?.richField) return;
      if (s.rangeCount) m.richField.range = s.getRangeAt(0).cloneRange();
      reportSelection(m.richField.h, m);
      reportFormats(m.richField.h, m);
    });
  }
  updateEmpty(n);
}

/** end is the line break that keeps a last empty line showing (it isn't the text's). */
function end() {
  const b = document.createElement("br");
  b.dataset.end = "";
  return b;
}

// --- From the core ---

/** setRich is the Rich prop: the field's text, styled. */
export function setRich(h, n, b) {
  const f = n.richField;
  const ed = n.input;
  const styles = [];
  const r = new Reader(b);
  const paras = [];
  if (b.length >= 4) {
    const count = r.u32();
    for (let i = 0; i < count && !r.atEnd; i++) {
      styles.push({ style: r.u8(), size: r.f32(), weight: r.u8(), flags: r.u8(), color: r.u64(), bg: r.u64(), link: r.string() });
    }
    const np = r.atEnd ? 0 : r.u32();
    for (let p = 0; p < np && !r.atEnd; p++) {
      const runs = [];
      const nr = r.u32();
      for (let j = 0; j < nr && !r.atEnd; j++) runs.push([r.u32(), r.string()]);
      paras.push(runs);
    }
  }
  const sel = selectionOf(n);
  f.setting = true;
  ed.textContent = "";
  paras.forEach((runs, p) => {
    if (p > 0) ed.appendChild(document.createElement("br"));
    for (const [si, t] of runs) {
      const s = styles[si];
      if (!s || s.flags & 16) continue; // inline components: not in a field
      ed.appendChild(spanOf(h, n, s, t));
    }
  });
  ed.appendChild(end());
  n.text = plainText(ed);
  if (sel && document.activeElement === ed) select(n, sel[0], sel[1]);
  f.setting = false;
  updateEmpty(n);
  reportFormats(h, n);
}

/** spanOf is a run's element: its style's look, its index, in a link if it has one. */
function spanOf(h, n, s, t) {
  const f = n.richField;
  let i = f.styles.findIndex((x) => key(x) === key(s));
  if (i < 0) { f.styles.push({ ...s }); i = f.styles.length - 1; }
  const span = document.createElement("span");
  span.dataset.s = String(i);
  span.textContent = t;
  const css = span.style;
  if (s.style || s.size) {
    const [size, weight, line] = text.fontOf(h, { style: s.style || n.font.style, size: s.size || 0, weight: 0, family: "" });
    css.fontSize = size / 16 + "rem";
    if (!s.weight && s.style) css.fontWeight = String(weight);
    css.lineHeight = String(line);
  }
  if (s.weight) css.fontWeight = String(weights[s.weight]);
  if (s.flags & 1) css.fontStyle = "italic";
  if (s.flags & 2) css.fontFamily = mono;
  const deco = [s.flags & 4 ? "underline" : "", s.flags & 8 ? "line-through" : ""].filter(Boolean).join(" ");
  if (deco) css.textDecorationLine = deco;
  if (s.color) css.color = h.css(s.color);
  if (s.bg) { css.backgroundColor = h.css(s.bg); span.classList.add("asuka-marked"); }
  if (!s.link) return span;
  const a = document.createElement("a");
  a.href = s.link;
  a.appendChild(span);
  return a;
}

// --- Reading the field ---

/** walk calls f for each piece of the field's text in order: a text node, or a line break. */
function walk(ed, f) {
  const w = document.createTreeWalker(ed, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
  let first = true;
  for (let node = w.nextNode(); node; node = w.nextNode()) {
    if (node.nodeType === 3) {
      if (node.data) f(node, node.data);
    } else if (node.tagName === "BR") {
      if (node.dataset.end === undefined) f(node, "\n");
    } else if ((node.tagName === "DIV" || node.tagName === "P") && !first) {
      f(node, "\n"); // a block a browser's editing made: a line of its own
    }
    first = false;
  }
}

function plainText(ed) {
  let s = "";
  walk(ed, (_, t) => { s += t; });
  return s;
}

/** styleOf is a text node's style: the core's as it was sent, with what the user changed. */
function styleOf(n, node, base) {
  const f = n.richField;
  const el = node.nodeType === 3 ? node.parentElement : node;
  const holder = el.closest("[data-s]");
  const ed = n.input;
  let s = holder && ed.contains(holder) ? { ...(f.styles[+holder.dataset.s] || zero()) } : zero();
  const cs = getComputedStyle(el);
  const bold = parseInt(cs.fontWeight, 10) >= 600;
  const italic = cs.fontStyle === "italic" || cs.fontStyle.startsWith("oblique");
  const isMono = /mono|menlo|consolas|courier/i.test(cs.fontFamily);
  // What the field's own font has (a headline's bold) isn't the run's:
  // the run keeps what it was sent with for it.
  if (!base.bold) {
    if (bold && s.weight < Semibold) s.weight = Bold;
    else if (!bold && s.weight >= Semibold) s.weight = 0;
  }
  let flags = s.flags & 3;
  if (!base.italic) flags = (flags & ~1) | (italic ? 1 : 0);
  if (!base.mono) flags = (flags & ~2) | (isMono ? 2 : 0);
  // Decorations aren't inherited: what the elements up to the field draw.
  for (let e = el; e && e !== ed; e = e.parentElement) {
    const d = getComputedStyle(e).textDecorationLine;
    if (d.includes("underline") && e.tagName !== "A") flags |= 4;
    if (d.includes("line-through")) flags |= 8;
  }
  s.flags = flags;
  const a = el.closest("a[href]");
  s.link = a && ed.contains(a) ? a.getAttribute("href") : "";
  return s;
}

function baseOf(ed) {
  const cs = getComputedStyle(ed);
  return { bold: parseInt(cs.fontWeight, 10) >= 600, italic: cs.fontStyle === "italic", mono: /mono|menlo|consolas|courier/i.test(cs.fontFamily) };
}

/** encode is the field's styled text as the core encodes it: styles in the order runs first use them, a paragraph per line, runs of one style merged. */
function encode(n) {
  const ed = n.input;
  const base = baseOf(ed);
  const styles = [], index = new Map();
  const paras = [[]];
  let last = zero();
  walk(ed, (node, t) => {
    if (t === "\n" && node.nodeType !== 3) {
      paras.push([]);
      return;
    }
    const s = styleOf(n, node, base);
    last = s;
    const parts = t.split("\n");
    parts.forEach((part, i) => {
      if (i > 0) paras.push([]);
      if (!part) return;
      const k = key(s);
      let si = index.get(k);
      if (si === undefined) { si = styles.length; index.set(k, si); styles.push(s); }
      const p = paras[paras.length - 1];
      if (p.length && p[p.length - 1][0] === si) p[p.length - 1][1] += part;
      else p.push([si, part]);
    });
  });
  const w = new Writer().u32(styles.length);
  for (const s of styles) w.u8(s.style).f32(s.size).u8(s.weight).u8(s.flags).i64(s.color).i64(s.bg).string(s.link);
  w.u32(paras.length);
  for (const p of paras) {
    w.u32(p.length);
    for (const [si, t] of p) w.u32(si).string(t);
  }
  return w.done();
}

/** changed reports the user's change of the text or its styles. */
function changed(h, n) {
  const f = n.richField;
  const ed = n.input;
  if (f.setting) return;
  if (ed.lastChild?.dataset?.end === undefined) {
    ed.querySelectorAll("br[data-end]").forEach((b) => b.remove());
    ed.appendChild(end());
  }
  const t = plainText(ed);
  n.text = t;
  updateEmpty(n);
  h.emit(n, Event.TextChanged, (id) => Events.textChanged(id, t));
  h.emit(n, Event.RichChanged, (id) => Events.richChanged(id, encode(n)));
  reportSelection(h, n);
  reportFormats(h, n);
}

function updateEmpty(n) {
  n.input.classList.toggle("asuka-empty", plainText(n.input) === "");
}

export function setPlaceholder(n, v) { n.input.dataset.placeholder = v; }

// --- Selection, in UTF-16 units of the plain text ---

/** offsetOf is the plain text's offset at a DOM point. */
function offsetOf(ed, container, offset) {
  const r = document.createRange();
  r.setStart(ed, 0);
  try { r.setEnd(container, offset); } catch { return 0; }
  const frag = r.cloneContents();
  let s = 0;
  walk(frag, (_, t) => { s += t.length; });
  return s;
}

function selectionOf(n) {
  const s = getSelection();
  const ed = n.input;
  if (!s.rangeCount || !ed.contains(s.anchorNode)) return null;
  const r = s.getRangeAt(0);
  return [offsetOf(ed, r.startContainer, r.startOffset), offsetOf(ed, r.endContainer, r.endOffset)];
}

/** pointAt is the DOM point at a plain text offset. */
function pointAt(ed, at) {
  let left = at, point = null;
  walk(ed, (node, t) => {
    if (point) return;
    if (left <= t.length && node.nodeType === 3) { point = [node, left]; return; }
    if (left < t.length || (left === 0 && t === "\n")) {
      // Before a line break.
      point = [node.parentNode, [...node.parentNode.childNodes].indexOf(node)];
      return;
    }
    left -= t.length;
  });
  if (point) return point;
  const e = ed.querySelector("br[data-end]");
  return e ? [ed, [...ed.childNodes].indexOf(e)] : [ed, ed.childNodes.length];
}

export function select(n, start, endAt) {
  const ed = n.input;
  const s = getSelection();
  const r = document.createRange();
  const [a, ao] = pointAt(ed, start);
  const [b, bo] = pointAt(ed, endAt);
  r.setStart(a, ao);
  r.setEnd(b, bo);
  n.richField.range = r.cloneRange();
  if (document.activeElement === ed) {
    s.removeAllRanges();
    s.addRange(r);
  }
}

function reportSelection(h, n) {
  if (!n.listening(Event.SelectionChanged)) return;
  const sel = selectionOf(n);
  if (!sel) return;
  if (n.selSent && n.selSent[0] === sel[0] && n.selSent[1] === sel[1]) return;
  n.selSent = sel;
  h.dispatch(Events.selectionChanged(n.id, new Writer().u32(sel[0]).u32(sel[1]).done()));
}

// --- Formats ---

/** textNodes are the text nodes the selection covers. */
function textNodes(ed, r) {
  const out = [];
  walk(ed, (node) => { if (node.nodeType === 3 && r.intersectsNode(node)) out.push(node); });
  return out.filter((t) => !(t === r.endContainer && r.endOffset === 0) && !(t === r.startContainer && r.startOffset === t.data.length));
}

/** formatsOf is the styles at the cursor (what's typed next) or that all the selection has, and its link; any is what some of the selection has. */
function formatsOf(n) {
  const ed = n.input;
  const s = getSelection();
  const r = s.rangeCount && ed.contains(s.anchorNode) ? s.getRangeAt(0) : n.richField.range;
  const base = baseOf(ed);
  if (!r || r.collapsed) {
    let f = 0;
    if (document.activeElement === ed) {
      // The typing style: what the editing commands will give what's typed.
      if (document.queryCommandState("bold") && !base.bold) f |= 1;
      if (document.queryCommandState("italic") && !base.italic) f |= 2;
      if (document.queryCommandState("underline")) f |= 4;
      if (document.queryCommandState("strikeThrough")) f |= 8;
      if (/mono|menlo|consolas|courier/i.test(document.queryCommandValue("fontName")) && !base.mono) f |= 16;
    } else if (r) {
      const node = r.startContainer;
      f = format(styleOf(n, node.nodeType === 3 ? node : node.childNodes[r.startOffset - 1] || node, base));
    }
    const node = r?.startContainer;
    const el = node && (node.nodeType === 3 ? node.parentElement : node);
    const a = el?.closest("a[href]");
    return { all: f, any: f, link: a && ed.contains(a) ? a.getAttribute("href") : "" };
  }
  let all = 31, any = 0, link = null;
  for (const t of textNodes(ed, r)) {
    const st = styleOf(n, t, base);
    const f = format(st);
    all &= f;
    any |= f;
    link = link === null ? st.link : link === st.link ? link : "";
  }
  if (link === null) { all = 0; link = ""; }
  return { all, any, link };
}

function reportFormats(h, n) {
  const f = n.richField;
  if (!n.listening(Event.Formats)) return;
  const { all, link } = formatsOf(n);
  if (f.formats === all && f.link === link) return;
  f.formats = all;
  f.link = link;
  const b = enc.encode(link);
  h.dispatch(Events.formats(n.id, new Writer().u8(all).u32(b.length).raw(b).done()));
}

/** focusKept focuses the field with its selection as it was (a toolbar's button took the focus). */
function focusKept(n) {
  const ed = n.input;
  const range = n.richField.range;
  if (document.activeElement !== ed) ed.focus({ preventScroll: true });
  if (range && ed.contains(range.startContainer)) {
    const s = getSelection();
    s.removeAllRanges();
    s.addRange(range);
  }
}

const commands = [[1, "bold"], [2, "italic"], [4, "underline"], [8, "strikeThrough"]];

/** formatField is the Format command: u8 TextFormat bits, u8 0 off, 1 on, 2 toggle (on unless all the selection has them). */
export function formatField(h, n, args) {
  if (args.length < 2) return;
  const bits = args[0], how = args[1];
  focusKept(n);
  const has = formatsOf(n).all;
  const on = how === 1 || (how === 2 && (has & bits) !== bits);
  document.execCommand("styleWithCSS", false, false);
  for (const [bit, cmd] of commands) {
    if (!(bits & bit)) continue;
    // An editing command toggles: done when all the selection has it (on), or none of it does (off).
    for (let i = 0; i < 3; i++) {
      const f = formatsOf(n);
      if (on ? f.all & bit : !(f.any & bit)) break;
      document.execCommand(cmd);
    }
  }
  if (bits & 16) {
    const family = on ? mono : getComputedStyle(n.input).fontFamily;
    document.execCommand("fontName", false, family);
  }
  reportFormats(h, n);
}

/** setLink is the SetLink command: the selection links there (at the cursor, the link around it); "" unlinks. */
export function setLink(h, n, url) {
  focusKept(n);
  const s = getSelection();
  if (!s.rangeCount) return;
  let r = s.getRangeAt(0);
  if (r.collapsed) {
    // The link the cursor is in.
    const node = r.startContainer;
    const a = (node.nodeType === 3 ? node.parentElement : node)?.closest("a[href]");
    if (!a || !n.input.contains(a)) return;
    r = document.createRange();
    r.selectNodeContents(a);
    s.removeAllRanges();
    s.addRange(r);
  }
  if (url) document.execCommand("createLink", false, url);
  else document.execCommand("unlink");
  reportFormats(h, n);
}

/** insertText is InsertText: in place of the selection, as typed. */
export function insertText(n, t) {
  focusKept(n);
  document.execCommand("insertText", false, t);
}

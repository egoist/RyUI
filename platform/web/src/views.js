// The views of each kind: create, props, measuring. Controls are the
// browser's own elements (button, input, select, progress), so they keep
// the platform's keyboard handling, focus, autofill and accessibility;
// the app's accent is their accent-color.

import { Kind, KindProp as P, Event, Events, Command, Reader, Writer } from "./ops.js";
import * as text from "./text.js";
import { decodeStrings, decodeActions } from "./host.js";
import { decodeDragItems, appendItems, shareKind, matchMIME } from "./interaction.js";
import { navKinds } from "./nav.js";
import { listKind, scrollKind } from "./list.js";
import { mediaKinds, symbolSVG } from "./media.js";
import { richKinds } from "./rich.js";
import { drawKinds } from "./draw.js";
import { shaderKind } from "./shader.js";
import * as color from "./color.js";
import * as fs from "./fs.js";
import * as rich from "./richfield.js";
import * as nativeModules from "./native.js";

const enc = new TextEncoder();

function div(cls) {
  const d = document.createElement("div");
  if (cls) d.className = cls;
  return d;
}

/** own makes el the node's, so events find their view. */
export function own(n, el) {
  n.el = el;
  el.asukaNode = n;
  return el;
}

const finite = (f) => f === f && f !== Infinity && f !== -Infinity;

/** fitWidth is a control's width: its own, no wider than the space it has. */
function fitWidth(c, natural, least = natural) {
  if (c.width === c.width) return c.width;
  if (c.availW === -Infinity) return least;
  if (finite(c.availW)) return Math.min(natural, Math.max(c.availW, 0));
  return natural;
}

/** fillWidth is the width of a control that takes what it's given (a slider). */
function fillWidth(c, natural) {
  if (c.width === c.width) return c.width;
  if (finite(c.availW)) return Math.max(c.availW, 0);
  return natural;
}

function heightOf(h, el) {
  return h.measurable(el, () => {
    const s = el.style;
    const saved = s.height;
    s.height = "auto";
    const r = el.getBoundingClientRect().height;
    s.height = saved;
    return r;
  });
}

const view = {
  create(h, n) { own(n, div()); },
  prop(h, n, p, v) {
    switch (p) {
      case P.View.DropAccept:
        n.dropAccept = v.length >= 4 ? decodeStrings(v) : null;
        break;
      case P.View.DragItems:
        n.dragItems = decodeDragItems(v);
        n.el.draggable = !!n.dragItems?.length;
        break;
    }
  },
  measure() { return [0, 0]; },
};

const textView = {
  create(h, n) {
    own(n, div("asuka-text"));
    n.text = "";
    text.applyFont(h, n, n.el);
  },
  prop(h, n, p, v) {
    if (p === P.Text.Text) {
      n.text = v;
      n.el.textContent = v;
    }
  },
  lines(h, n) { lineClamp(n.el, n.lines); },
  measure(h, n, c) {
    if (!n.text) return [0, 0];
    return h.measureElement(n, n.el, c, { tight: true });
  },
};

export function lineClamp(el, lines) {
  el.classList.toggle("asuka-one-line", lines === 1);
  el.classList.toggle("asuka-clamp", lines > 1);
  el.style.webkitLineClamp = lines > 1 ? String(lines) : "";
}

// --- Button ---

const variants = ["plain", "filled", "tinted", "gray", "bordered", "glass", "glass-prominent"];

const button = {
  create(h, n) {
    const b = own(n, document.createElement("button"));
    b.type = "button";
    b.className = "asuka-button asuka-control";
    b.dataset.variant = "plain";
    n.label = document.createElement("span");
    n.label.className = "asuka-button-title";
    b.appendChild(n.label);
    n.title = "";
    n.icon = "";
    b.addEventListener("click", (e) => {
      e.stopPropagation();
      if (n.menu && n.menuTrigger !== 1) {
        const r = b.getBoundingClientRect();
        h.menus.open(n, r.left, r.bottom, b);
        return;
      }
      h.emit(n, Event.Press, Events.press);
    });
    text.applyFont(h, n, b);
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Button.Title:
        n.title = v;
        n.label.textContent = v;
        n.label.hidden = !v;
        break;
      case P.Button.Variant:
        n.el.dataset.variant = variants[v] || "plain";
        break;
      case P.Button.Icon:
        n.icon = v;
        n.el.querySelector(".asuka-symbol")?.remove();
        if (v) {
          const s = symbolSVG(v, h.rtl);
          if (s) n.el.insertBefore(s, n.label);
        }
        break;
    }
  },
  color(h, n) { n.el.style.setProperty("--tint", h.css(n.color) || "var(--asuka-accent)"); },
  background(h, n) {
    n.el.style.setProperty("--fill", h.css(n.background) || "");
    if (!n.background) n.el.style.removeProperty("--fill");
    return true;
  },
  disabled(h, n) { n.el.disabled = n.disabled; },
  label(h, n, v) { if (!n.title && v) n.el.title = v; },
  inset(h, n) { padInset(n); },
  measure(h, n, c) {
    const [w, hh] = h.measureElement(n, n.el, { width: NaN, height: NaN, availW: Infinity, availH: Infinity });
    return [fitWidth(c, w, w), hh];
  },
};

function padInset(n) {
  const v = n.inset;
  if (v) n.el.style.padding = `${v[0]}px ${v[1]}px ${v[2]}px ${v[3]}px`;
}

// --- TextField ---

const autocomplete = {
  username: "username", password: "current-password", newPassword: "new-password", oneTimeCode: "one-time-code",
  email: "email", phone: "tel", name: "name", givenName: "given-name", familyName: "family-name",
  address: "street-address", postalCode: "postal-code", city: "address-level2", country: "country-name",
  cardNumber: "cc-number", url: "url",
};
const inputModes = ["text", "numeric", "decimal", "email", "url", "tel"];
const enterKeys = ["", "done", "go", "next", "search", "send"];
const capitalize = ["sentences", "none", "words", "characters"];

const textField = {
  create(h, n) {
    n.text = "";
    n.multiline = false;
    n.sel = [0, 0];
    n.selSent = null;
    makeField(h, n, document.createElement("input"));
  },
  prop(h, n, p, v) {
    const el = n.input;
    switch (p) {
      case P.TextField.Value:
        if (n.richField) break; // its text is Rich's
        n.text = v;
        if (el.value !== v) {
          el.value = v;
          if (n.multiline) h.afterCore(() => fieldResized(h, n));
        }
        break;
      case P.TextField.Placeholder:
        if (n.richField) rich.setPlaceholder(n, v);
        else el.placeholder = v;
        break;
      case P.TextField.Keyboard:
        el.inputMode = inputModes[v] || "text";
        break;
      case P.TextField.ReturnKey:
        if (enterKeys[v]) el.enterKeyHint = enterKeys[v]; else el.removeAttribute("enterkeyhint");
        break;
      case P.TextField.Secure:
        if (!n.multiline) el.type = v ? "password" : "text";
        break;
      case P.TextField.Autocapitalize:
        el.autocapitalize = capitalize[v] || "sentences";
        break;
      case P.TextField.Autocorrect:
        if (v === 1) { el.setAttribute("autocorrect", "on"); el.spellcheck = true; }
        else if (v === 2) { el.setAttribute("autocorrect", "off"); el.spellcheck = false; }
        else { el.removeAttribute("autocorrect"); el.removeAttribute("spellcheck"); }
        break;
      case P.TextField.ContentType:
        el.autocomplete = autocomplete[v] || v || "";
        break;
      case P.TextField.Multiline:
        if (!!v !== n.multiline && !n.richField) {
          n.multiline = !!v;
          makeField(h, n, document.createElement(v ? "textarea" : "input"));
        }
        break;
      case P.TextField.Plain:
        n.el.classList.toggle("asuka-plain", !!v);
        break;
      case P.TextField.Selection: {
        if (v.length < 8) break;
        const r = new Reader(v);
        const s = r.u32(), e = r.u32();
        n.sel = [s, e];
        if (n.richField) { rich.select(n, s, e); break; }
        try { el.setSelectionRange(Math.min(s, el.value.length), Math.min(e, el.value.length)); } catch {}
        break;
      }
      case P.TextField.PasteTypes:
        n.pasteTypes = decodeStrings(v);
        break;
      case P.TextField.Rich:
        if (!n.richField) rich.makeRich(h, n, (ed) => wireField(h, n, ed));
        rich.setRich(h, n, v);
        h.afterCore(() => fieldResized(h, n));
        break;
      case P.TextField.Accessory:
        n.accessory = v;
        break;
    }
  },
  common(h, n, p, v) { return false; },
  font(h, n) { text.applyFont(h, n, n.input); },
  color(h, n) { n.input.style.color = h.css(n.color); },
  align(h, n) { n.input.style.textAlign = text.align(n.align); },
  disabled(h, n) {
    if (n.richField) n.input.contentEditable = n.disabled ? "false" : "true";
    else n.input.disabled = n.disabled;
  },
  lines(h, n) { if (n.multiline) h.afterCore(() => fieldResized(h, n)); },
  inset(h, n) { const v = n.inset; n.input.style.padding = `${v[0]}px ${v[1]}px ${v[2]}px ${v[3]}px`; },
  command(h, n, cmd, args) {
    const el = n.input;
    switch (cmd) {
      case Command.Focus: el.focus({ preventScroll: true }); return true;
      case Command.Blur: el.blur(); return true;
      case Command.Format:
        if (n.richField) rich.formatField(h, n, args);
        return true;
      case Command.SetLink:
        if (n.richField) rich.setLink(h, n, new TextDecoder().decode(args));
        return true;
      case Command.InsertText: {
        const t = new TextDecoder().decode(args);
        if (n.richField) { rich.insertText(n, t); return true; }
        el.focus({ preventScroll: true });
        // Through the page's own editing, so it's undoable and reported as typing.
        if (!document.execCommand("insertText", false, t)) {
          el.setRangeText(t, el.selectionStart, el.selectionEnd, "end");
          el.dispatchEvent(new InputEvent("input", { bubbles: true, data: t, inputType: "insertText" }));
        }
        return true;
      }
    }
    return false;
  },
  measure(h, n, c) {
    const el = n.el;
    const w = fitWidth(c, 200, 80);
    if (!n.multiline) {
      return [w, heightOf(h, el)];
    }
    return [w, multilineHeight(h, n, w)];
  },
  resized(h, n) {},
};

/** makeField makes the field's input or textarea (Multiline comes before it's inserted). */
function makeField(h, n, input) {
  const old = n.input;
  input.className = "asuka-field asuka-control";
  if (old) {
    for (const a of ["placeholder", "autocomplete", "inputmode", "enterkeyhint", "autocapitalize", "autocorrect", "spellcheck"]) {
      const v = old.getAttribute(a);
      if (v != null) input.setAttribute(a, v);
    }
    input.value = old.value;
    input.style.cssText = old.style.cssText;
    input.className = old.className;
    old.replaceWith(input);
  }
  n.input = input;
  n.focusEl = input;
  if (input.tagName === "TEXTAREA") input.rows = 1;
  if (!n.el) {
    const wrap = own(n, div("asuka-field-box"));
    wrap.appendChild(input);
  }
  text.applyFont(h, n, input);
  input.addEventListener("input", (e) => {
    n.text = input.value;
    h.emit(n, Event.TextChanged, (id) => Events.textChanged(id, input.value));
    reportSelection(h, n);
    if (n.multiline) fieldResized(h, n);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.isComposing && e.keyCode !== 229 && !n.multiline) {
      h.emit(n, Event.Submit, Events.submit);
    }
  });
  wireField(h, n, input);
  for (const ev of ["select", "keyup", "pointerup"]) input.addEventListener(ev, () => reportSelection(h, n));
}

/** wireField reports a field's focus and what's pasted into it of its PasteTypes (a rich field's editor too). */
function wireField(h, n, input) {
  input.addEventListener("focus", () => {
    h.emit(n, Event.Focus, Events.focus);
    h.nav.fieldFocused(n, true);
  });
  input.addEventListener("blur", () => {
    h.emit(n, Event.Blur, Events.blur);
    h.nav.fieldFocused(n, false);
  });
  input.addEventListener("paste", (e) => paste(h, n, e));
}

function reportSelection(h, n) {
  const el = n.input;
  if (!n.listening(Event.SelectionChanged) || el.selectionStart == null) return;
  const s = el.selectionStart, e = el.selectionEnd;
  if (n.selSent && n.selSent[0] === s && n.selSent[1] === e) return;
  n.selSent = [s, e];
  h.dispatch(Events.selectionChanged(n.id, new Writer().u32(s).u32(e).done()));
}

async function paste(h, n, e) {
  if (!n.pasteTypes?.length || !n.listening(Event.Pasted)) return;
  const files = [...(e.clipboardData?.files || [])].filter((f) => matchMIME(n.pasteTypes, f.type || "application/octet-stream"));
  if (!files.length) return;
  e.preventDefault();
  const items = [];
  for (const f of files) items.push({ kind: shareKind(f.type), text: "", path: await fs.saveUpload(f), name: f.name, mime: f.type || "application/octet-stream", size: f.size });
  h.dispatch(Events.pasted(n.id, appendItems(new Writer(), items).done()));
}

/** multilineHeight is a multiline field's height at width w: its text's, up to its line limit, then it scrolls. */
function multilineHeight(h, n, w) {
  return h.measurable(n.el, () => multilineHeightNow(n, w));
}

function multilineHeightNow(n, w) {
  const el = n.el, input = n.input;
  const s = el.style, saved = [s.width, s.height];
  s.width = w + "px";
  s.height = "auto";
  input.style.height = "0px";
  const cs = getComputedStyle(input);
  const line = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.3;
  const pad = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom) + parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth);
  let content = Math.max(input.scrollHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom), line);
  if (n.lines > 0) content = Math.min(content, line * n.lines);
  input.style.height = "";
  s.width = saved[0];
  s.height = saved[1];
  return content + pad;
}

/** fieldResized measures a multiline field again as its text grows (the core lays it out again). */
function fieldResized(h, n) {
  const w = n.frame[2] || 200;
  const hh = multilineHeight(h, n, w);
  if (Math.abs(hh - n.frame[3]) > 0.5 && n.placed) {
    n.lastHeight = hh;
    // The core measures it as its Value changes (TextChanged comes first).
  }
}

// --- Toggle, Slider, Stepper, Segmented, Wheel, ColorPicker, Progress, DatePicker, PageControl, Activity ---

const toggle = {
  create(h, n) {
    const box = own(n, div("asuka-toggle-box"));
    const i = document.createElement("input");
    i.type = "checkbox";
    i.className = "asuka-switch asuka-control";
    i.setAttribute("role", "switch");
    i.setAttribute("switch", "");
    box.appendChild(i);
    n.input = n.focusEl = i;
    i.addEventListener("change", () => h.emit(n, Event.Toggled, (id) => Events.toggled(id, i.checked)));
  },
  prop(h, n, p, v) {
    if (p === P.Toggle.On) n.input.checked = !!v;
    if (p === P.Toggle.Checkbox) {
      n.input.className = v ? "asuka-checkbox asuka-control" : "asuka-switch asuka-control";
      if (v) { n.input.removeAttribute("role"); n.input.removeAttribute("switch"); }
    }
  },
  color(h, n) { n.input.style.setProperty("--tint", h.css(n.color) || ""); },
  disabled(h, n) { n.input.disabled = n.disabled; },
  label(h, n, v) { n.input.setAttribute("aria-label", v); },
  measure(h, n) {
    const r = rectOf(h, n.el, n.input);
    return [r.width, r.height];
  },
};

function rectOf(h, el, inner) {
  return h.measurable(el, () => {
    const r = inner.getBoundingClientRect();
    const cs = getComputedStyle(inner);
    return { width: r.width + parseFloat(cs.marginLeft) + parseFloat(cs.marginRight), height: r.height + parseFloat(cs.marginTop) + parseFloat(cs.marginBottom) };
  });
}

const slider = {
  create(h, n) {
    const box = own(n, div("asuka-slider-box"));
    const i = document.createElement("input");
    i.type = "range";
    i.step = "any";
    i.min = "0";
    i.max = "1";
    i.className = "asuka-slider asuka-control";
    box.appendChild(i);
    n.input = n.focusEl = i;
    i.addEventListener("input", () => h.emit(n, Event.ValueChanged, (id) => Events.valueChanged(id, parseFloat(i.value))));
  },
  prop(h, n, p, v) {
    const i = n.input;
    if (p === P.Slider.Min) i.min = String(v);
    if (p === P.Slider.Max) i.max = String(v);
    if (p === P.Slider.Value) i.value = String(v);
  },
  color(h, n) { n.input.style.accentColor = h.css(n.color); },
  disabled(h, n) { n.input.disabled = n.disabled; },
  label(h, n, v) { n.input.setAttribute("aria-label", v); },
  measure(h, n, c) { return [fillWidth(c, 200), rectOf(h, n.el, n.input).height]; },
};

const stepper = {
  create(h, n) {
    const box = own(n, div("asuka-stepper"));
    box.setAttribute("role", "group");
    n.value = 0; n.min = 0; n.max = 100; n.step = 1;
    const mk = (label, sign) => {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "asuka-control";
      b.textContent = sign > 0 ? "+" : "−";
      b.setAttribute("aria-label", label);
      b.addEventListener("click", () => {
        const v = Math.min(n.max, Math.max(n.min, n.value + sign * n.step));
        if (v === n.value) return;
        n.value = v;
        stepperState(n);
        h.emit(n, Event.ValueChanged, (id) => Events.valueChanged(id, v));
      });
      box.appendChild(b);
      return b;
    };
    n.minus = mk("Decrement", -1);
    n.plus = mk("Increment", 1);
  },
  prop(h, n, p, v) {
    if (p === P.Stepper.Value) n.value = v;
    if (p === P.Stepper.Min) n.min = v;
    if (p === P.Stepper.Max) n.max = v;
    if (p === P.Stepper.Step) n.step = v || 1;
    stepperState(n);
  },
  disabled(h, n) { stepperState(n); },
  measure(h, n) {
    const r = rectOf(h, n.el, n.el);
    return [r.width, r.height];
  },
};

function stepperState(n) {
  n.minus.disabled = n.disabled || n.value <= n.min;
  n.plus.disabled = n.disabled || n.value >= n.max;
}

const segmented = {
  create(h, n) {
    const box = own(n, div("asuka-segmented asuka-control"));
    box.setAttribute("role", "radiogroup");
    n.segments = [];
    n.selected = 0;
  },
  prop(h, n, p, v) {
    if (p === P.Segmented.Segments) {
      n.el.textContent = "";
      n.segments = decodeStrings(v).map((title, i) => {
        const b = document.createElement("button");
        b.type = "button";
        b.textContent = title;
        b.setAttribute("role", "radio");
        b.addEventListener("click", () => {
          if (n.selected === i) return;
          n.selected = i;
          segmentState(n);
          h.emit(n, Event.Selected, (id) => Events.selected(id, i));
        });
        n.el.appendChild(b);
        return b;
      });
      text.applyFont(h, n, n.el);
    }
    if (p === P.Segmented.Selected) n.selected = v;
    segmentState(n);
  },
  color(h, n) { n.el.style.setProperty("--tint", h.css(n.color) || ""); },
  disabled(h, n) { for (const b of n.segments) b.disabled = n.disabled; },
  measure(h, n, c) {
    const [w, hh] = h.measureElement(n, n.el, { width: NaN, height: NaN, availW: Infinity, availH: Infinity });
    return [fitWidth(c, w, w), hh];
  },
};

function segmentState(n) {
  n.segments.forEach((b, i) => b.setAttribute("aria-checked", String(i === n.selected)));
}

const wheel = {
  create(h, n) {
    const box = own(n, div("asuka-select-box"));
    const s = document.createElement("select");
    s.className = "asuka-select asuka-control";
    box.appendChild(s);
    n.input = n.focusEl = s;
    s.addEventListener("change", () => h.emit(n, Event.Selected, (id) => Events.selected(id, s.selectedIndex)));
    text.applyFont(h, n, s);
  },
  prop(h, n, p, v) {
    const s = n.input;
    if (p === P.Wheel.Items) {
      s.textContent = "";
      for (const t of decodeStrings(v)) s.add(new Option(t));
      if (n.selected != null) s.selectedIndex = n.selected;
    }
    if (p === P.Wheel.Selected) { n.selected = v; s.selectedIndex = v; }
  },
  font(h, n) { text.applyFont(h, n, n.input); },
  disabled(h, n) { n.input.disabled = n.disabled; },
  label(h, n, v) { n.input.setAttribute("aria-label", v); },
  measure(h, n, c) {
    const r = rectOf(h, n.el, n.input);
    return [fitWidth(c, r.width, r.width), r.height];
  },
};

const colorPicker = {
  create(h, n) {
    const box = own(n, div("asuka-color-box"));
    const i = document.createElement("input");
    i.type = "color";
    i.className = "asuka-color asuka-control";
    box.appendChild(i);
    n.input = n.focusEl = i;
    i.addEventListener("input", () => {
      const v = i.value; // #rrggbb
      const r = parseInt(v.slice(1, 3), 16), g = parseInt(v.slice(3, 5), 16), b = parseInt(v.slice(5, 7), 16);
      const rgba = 2 * 4294967296 + ((r << 24) >>> 0) + (g << 16) + (b << 8) + 255;
      h.emit(n, Event.ColorChanged, (id) => Events.colorChanged(id, rgba));
    });
  },
  prop(h, n, p, v) {
    if (p === P.ColorPicker.Value) {
      const css = color.resolve(h, v, "rgb(0, 0, 0)");
      const m = css.match(/\d+(\.\d+)?/g) || [0, 0, 0];
      n.input.value = "#" + m.slice(0, 3).map((x) => Math.round(+x).toString(16).padStart(2, "0")).join("");
    }
    if (p === P.ColorPicker.Alpha && v) n.input.setAttribute("alpha", "");
    if (p === P.ColorPicker.Title) n.input.title = v;
  },
  disabled(h, n) { n.input.disabled = n.disabled; },
  measure(h, n) {
    const r = rectOf(h, n.el, n.input);
    return [r.width, r.height];
  },
};

const progress = {
  create(h, n) {
    const box = own(n, div("asuka-progress-box"));
    const p = document.createElement("progress");
    p.className = "asuka-progress";
    p.max = 1;
    p.value = 0;
    box.appendChild(p);
    n.input = p;
  },
  prop(h, n, p, v) {
    if (p === P.Progress.Value) { n.value = v; if (!n.indeterminate) n.input.value = v; }
    if (p === P.Progress.Indeterminate) {
      n.indeterminate = !!v;
      if (v) n.input.removeAttribute("value"); else n.input.value = n.value || 0;
    }
  },
  color(h, n) { n.input.style.accentColor = h.css(n.color); n.input.style.setProperty("--tint", h.css(n.color) || ""); },
  measure(h, n, c) { return [fillWidth(c, 100), rectOf(h, n.el, n.input).height]; },
};

const dateTypes = ["date", "time", "datetime-local"];

const datePicker = {
  create(h, n) {
    const box = own(n, div("asuka-date-box"));
    const i = document.createElement("input");
    i.type = "date";
    i.className = "asuka-date asuka-control";
    box.appendChild(i);
    n.input = n.focusEl = i;
    n.mode = 0;
    n.value = Date.now();
    i.addEventListener("change", () => {
      const ms = parseLocal(i.value, n.mode, n.value);
      if (ms == null) return;
      n.value = ms;
      h.emit(n, Event.ValueChanged, (id) => Events.valueChanged(id, ms));
    });
    text.applyFont(h, n, i);
  },
  prop(h, n, p, v) {
    const i = n.input;
    switch (p) {
      case P.DatePicker.Value: n.value = v; i.value = formatLocal(v, n.mode); break;
      case P.DatePicker.Mode: n.mode = v; i.type = dateTypes[v] || "date"; i.value = formatLocal(n.value, v); break;
      case P.DatePicker.Min: if (v) i.min = formatLocal(v, n.mode); else i.removeAttribute("min"); break;
      case P.DatePicker.Max: if (v) i.max = formatLocal(v, n.mode); else i.removeAttribute("max"); break;
    }
  },
  font(h, n) { text.applyFont(h, n, n.input); },
  disabled(h, n) { n.input.disabled = n.disabled; },
  measure(h, n, c) {
    const r = rectOf(h, n.el, n.input);
    return [fitWidth(c, r.width, r.width), r.height];
  },
};

const pad = (x, k = 2) => String(x).padStart(k, "0");

/** formatLocal is an instant as a date input shows it, in the device's time zone. */
function formatLocal(ms, mode) {
  const d = new Date(ms);
  const date = `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return mode === 1 ? time : mode === 2 ? `${date}T${time}` : date;
}

function parseLocal(s, mode, prev) {
  if (!s) return null;
  const d = new Date(prev);
  if (mode === 1) {
    const [hh, mm] = s.split(":").map(Number);
    d.setHours(hh, mm, 0, 0);
    return d.getTime();
  }
  if (mode === 0) {
    const [y, m, day] = s.split("-").map(Number);
    d.setFullYear(y, m - 1, day);
    return d.getTime();
  }
  const t = new Date(s).getTime();
  return isNaN(t) ? null : t;
}

const pageControl = {
  create(h, n) {
    const box = own(n, div("asuka-dots"));
    box.setAttribute("role", "tablist");
    n.count = 0;
    n.page = 0;
  },
  prop(h, n, p, v) {
    if (p === P.PageControl.Count) {
      n.count = v;
      n.el.textContent = "";
      for (let i = 0; i < v; i++) {
        const b = document.createElement("button");
        b.type = "button";
        b.setAttribute("role", "tab");
        b.setAttribute("aria-label", `${i + 1} / ${v}`);
        b.addEventListener("click", () => h.emit(n, Event.Selected, (id) => Events.selected(id, i)));
        n.el.appendChild(b);
      }
    }
    if (p === P.PageControl.Page) n.page = v;
    [...n.el.children].forEach((b, i) => b.setAttribute("aria-selected", String(i === n.page)));
  },
  color(h, n) { n.el.style.setProperty("--tint", h.css(n.color) || ""); },
  measure(h, n) {
    const [w, hh] = h.measureElement(n, n.el, { width: NaN, height: NaN, availW: Infinity, availH: Infinity });
    return [w, hh];
  },
};

const activity = {
  create(h, n) {
    const el = own(n, div("asuka-activity"));
    el.setAttribute("role", "progressbar");
    el.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-dasharray="42 100"/></svg>';
  },
  prop(h, n, p, v) {
    if (p === P.Activity.Animating) n.el.classList.toggle("asuka-animating", !!v);
  },
  color(h, n) { n.el.style.color = h.css(n.color); },
  measure() { return [20, 20]; },
};

const selectable = {
  create(h, n) { own(n, div("asuka-selectable")); },
  command(h, n, cmd) {
    if (cmd === Command.Copy) {
      const s = getSelection()?.toString();
      if (s) navigator.clipboard?.writeText(s).catch(() => {});
      return true;
    }
    return false;
  },
  measure() { return [0, 0]; },
};

// Native: a view of a kind registered with the page (asuka.registerNative),
// else a package's typed view (//asuka:view: its module's id # its name),
// ({ el, set(name, value), measure?(c), destroy?() }).
const native = {
  create(h, n) {
    own(n, div("asuka-native"));
  },
  prop(h, n, p, v) {
    if (p === P.Native.Type) {
      const ctx = {
        emit: (name, values = {}) => h.dispatch(Events.native(n.id, encodeNative(name, values))),
        host: h,
      };
      n.native = h.natives.get(v)?.(ctx) ?? nativeModules.makeView(v, ctx);
      if (!n.native) {
        // As the other hosts: said, and nothing shown in its place.
        fs.log(`asuka: no native view ${JSON.stringify(v)}: build the app with its packages' native code`);
        return;
      }
      n.el.appendChild(n.native.el);
      n.native.el.style.width = n.native.el.style.height = "100%";
    }
    if (p === P.Native.Prop && n.native) {
      const r = new Reader(v);
      const name = r.string(), t = r.u8();
      const value = t === 1 ? r.u8() !== 0 : t === 2 ? r.i64() : t === 3 ? r.f64() : t === 5 ? r.bytes().slice() : r.string();
      n.native.set?.(name, value);
    }
  },
  measure(h, n, c) { return n.native?.measure ? n.native.measure(c) : [0, 0]; },
  destroy(h, n) { n.native?.destroy?.(); return false; },
};

function encodeNative(name, values) {
  const w = new Writer().string(name);
  const entries = Object.entries(values);
  w.u32(entries.length);
  for (const [k, v] of entries) {
    w.string(k);
    if (typeof v === "boolean") w.u8(1).u8(v ? 1 : 0);
    else if (typeof v === "number" && Number.isInteger(v)) w.u8(2).i64(v);
    else if (typeof v === "number") w.u8(3).f64(v);
    else if (v instanceof Uint8Array) w.u8(5).u32(v.length).raw(v);
    else w.u8(4).string(String(v));
  }
  return w.done();
}

export const kinds = {
  [Kind.View]: view,
  [Kind.Text]: textView,
  [Kind.Button]: button,
  [Kind.TextField]: textField,
  [Kind.Toggle]: toggle,
  [Kind.Slider]: slider,
  [Kind.Activity]: activity,
  [Kind.Segmented]: segmented,
  [Kind.Wheel]: wheel,
  [Kind.Stepper]: stepper,
  [Kind.ColorPicker]: colorPicker,
  [Kind.Progress]: progress,
  [Kind.DatePicker]: datePicker,
  [Kind.PageControl]: pageControl,
  [Kind.Selectable]: selectable,
  [Kind.Native]: native,
  [Kind.List]: listKind,
  [Kind.Scroll]: scrollKind,
  [Kind.Shader]: shaderKind,
  ...navKinds,
  ...mediaKinds,
  ...richKinds,
  ...drawKinds,
};

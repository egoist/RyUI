// Keys: what views do with a hardware keyboard's keys (the Keys prop:
// ui.KeyMap, a view's or a text field's, and ui.UseKeys, the root's or a
// Window's). One keydown listener on the page, in the capture phase and
// ahead of the host's own (the menu bar's shortcuts, a panel's Escape, a
// field's submit), routes a key going down, and each repeat, as the
// fake host does: from the element with the keyboard's focus, its view,
// out through the views' parents, the nearest whose Keys have the key
// takes it (preventDefault and stopImmediatePropagation: the focused
// control, its caret, and the page's own listeners don't get it) and gets
// Key with the key's index. From a node the core leaves without a
// parent, a key goes on where it shows (up): a list's cell to its list, a
// sheet's node to the window it's declared in (its Window prop: a Window
// node, or the root) and a popover's to the root of what its anchor is in (a window's,
// or a sheet's node). A Window node is an end (a Window's keys are its
// own, its UseKeys'), and the root; with nothing focused, a key starts at
// the root, or at the node of the panel or dialog that has the keyboard.

import { Events, Kind } from "./ops.js";

const utf8 = new TextDecoder();

/** decodeKeys reads a Keys prop (ops.AppendKeys): u32 count, per key a u32-length name and u8 modifiers; what's cut short is left out. */
export function decodeKeys(b) {
  const out = [];
  if (!b || b.length < 4) return out;
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const n = v.getUint32(0, true);
  if (n > b.length) return out;
  let p = 4;
  for (let i = 0; i < n; i++) {
    if (p + 4 > b.length) break;
    const len = v.getUint32(p, true);
    p += 4;
    if (len > b.length - p - 1) break;
    const name = utf8.decode(b.subarray(p, p + len));
    p += len;
    out.push({ name, mods: b[p++] });
  }
  return out;
}

/** apple reports whether the page runs on a Mac, an iPhone or an iPad: ⌘ is the primary modifier. */
export function apple() {
  const p = globalThis.navigator?.userAgentData?.platform || globalThis.navigator?.platform || "";
  return /Mac|iPhone|iPad|iPod|macOS|iOS/.test(p);
}

/** modsOf is a key event's modifiers as ops has them: 1 primary (⌘ on Apple's platforms, Ctrl elsewhere), 2 shift, 4 alt (⌥), 8 control (a Mac's ⌃). */
export function modsOf(e, mac) {
  return ((mac ? e.metaKey : e.ctrlKey) ? 1 : 0) | (e.shiftKey ? 2 : 0) | (e.altKey ? 4 : 0) | (mac && e.ctrlKey ? 8 : 0);
}

/** declaredMods is a shortcut's modifiers as a key event has them: off Apple's platforms, Control is Ctrl, the primary modifier. */
export function declaredMods(mods, mac) {
  mods &= 15;
  return !mac && mods & 8 ? (mods & ~8) | 1 : mods;
}

// Named keys, by KeyboardEvent.key (and older browsers' names).
const named = {
  Enter: "enter", Escape: "escape", Esc: "escape", Tab: "tab", " ": "space", Spacebar: "space",
  Delete: "delete", Del: "delete", Backspace: "backspace",
  ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right", Up: "up", Down: "down", Left: "left", Right: "right",
  Home: "home", End: "end", PageUp: "pageup", PageDown: "pagedown",
};

// What a US keyboard's keys type, by KeyboardEvent.code: a key's
// character when its event doesn't say (a modifier changed it) and the
// keyboard's layout is unknown.
const usCodes = { Minus: "-", Equal: "=", BracketLeft: "[", BracketRight: "]", Backslash: "\\", Semicolon: ";", Quote: "'", Comma: ",", Period: ".", Slash: "/", Backquote: "`" };

/** codeChar is the character a key types on a US keyboard, by its code (KeyA: "a", Digit1: "1"), or "". */
export function codeChar(code) {
  if (/^Key[A-Z]$/.test(code)) return code[3].toLowerCase();
  if (/^Digit[0-9]$/.test(code)) return code[5];
  return usCodes[code] || "";
}

const single = (s) => s.length === 1 || (s.length === 2 && s.codePointAt(0) > 0xffff);
const ascii = (s) => /^[\x21-\x7e]$/.test(s);

/**
 * keyName is a key event's key as the Keys prop names it: a named key,
 * or the character the key types without modifiers, lower case ("" for
 * a modifier alone, a dead key's with no layout, a key of no name).
 * Shift changes a letter's case only; other characters typed with Shift
 * or ⌥ (a Mac's other characters) are read off the keyboard's layout
 * (layout: navigator.keyboard's map, code to character, where the
 * browser has it), else off a US keyboard's.
 */
export function keyName(e, layout) {
  const k = e.key || "";
  if (named[k]) return named[k];
  if (/^F([1-9]|1[0-2])$/.test(k)) return k.toLowerCase();
  if (e.code === "Space") return "space"; // ⌥Space: a no-break space
  if (single(k) && !e.altKey && (!e.shiftKey || k.toLowerCase() !== k.toUpperCase())) return k.toLowerCase();
  const m = layout?.get?.(e.code);
  if (m) return m.toLowerCase();
  const c = codeChar(e.code || "");
  if (c) return c;
  return single(k) ? k.toLowerCase() : "";
}

/**
 * keyOf is what a keydown matches: { names, mods }, names the key's name
 * then, for a character not of a Latin keyboard (a Russian one's), the
 * same key's on a US keyboard, so a view's Primary-C works there too, as
 * the menus' shortcuts do. Null while an input method composes text (its
 * keys are its own), for a key of no name, and off Apple's platforms
 * with the Windows key or AltGr (typing a character) down.
 */
export function keyOf(e, mac, layout) {
  if (e.isComposing || e.keyCode === 229 || e.key === "Process") return null;
  if (!mac && (e.metaKey || e.getModifierState?.("AltGraph"))) return null;
  const name = keyName(e, layout);
  if (!name) return null;
  const names = [name];
  if (single(name) && !ascii(name)) {
    const c = codeChar(e.code || "");
    if (c && c !== name) names.push(c);
  }
  return { names, mods: modsOf(e, mac) };
}

/** matchKey is the index of the key in keys (decodeKeys') that k (keyOf's) is, or -1: ops.MatchKey's, the names in order. */
export function matchKey(keys, k, mac) {
  for (const name of k.names) {
    for (let i = 0; i < keys.length; i++) {
      const d = keys[i];
      if (d.name === name && declaredMods(d.mods, mac) === k.mods) return i;
    }
  }
  return -1;
}

export class Keys {
  constructor(h) {
    this.h = h;
    this.mac = apple();
    this.layout = null; // navigator.keyboard's layout map, where the browser has one
    this.loading = false;
    this.stale = "";
    this.loadLayout();
    if (typeof addEventListener === "function") {
      // On the window, in the capture phase: before the page's elements
      // and before the menu bar's shortcuts (menu.js listens after).
      addEventListener("keydown", (e) => this.key(e), true);
      // A layout changed while the page was away.
      addEventListener("focus", () => this.loadLayout());
    }
  }

  /** set takes a view's Keys prop. */
  set(n, b) {
    const keys = decodeKeys(b);
    n.keys = keys.length ? keys : null;
  }

  loadLayout() {
    const kb = globalThis.navigator?.keyboard;
    if (!kb?.getLayoutMap || this.loading) return;
    this.loading = true;
    kb.getLayoutMap().then((m) => { this.layout = m; }, () => {}).finally(() => { this.loading = false; });
  }

  /** key routes a key going down; it reports whether a view took it. */
  key(e) {
    // A character typed on its own that the layout map has otherwise:
    // the user switched layouts; read it again (once per difference).
    const k0 = e.key || "";
    if (this.layout && single(k0) && !e.altKey && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
      const m = this.layout.get(e.code);
      const diff = e.code + k0;
      if (m && m !== k0.toLowerCase() && this.stale !== diff) {
        this.stale = diff;
        this.loadLayout();
      }
    }
    const k = keyOf(e, this.mac, this.layout);
    if (!k) return false;
    for (let n = this.start(e.target), i = 0; n && i < 4096; i++, n = this.up(n)) {
      if (n.keys) {
        const at = matchKey(n.keys, k, this.mac);
        if (at >= 0) {
          e.preventDefault();
          e.stopImmediatePropagation();
          this.h.dispatch(Events.key(n.id, at));
          return true;
        }
      }
    }
    return false;
  }

  /**
   * start is the view a key typed at target starts at: its element's
   * (or the nearest of its ancestors' the host knows), a Window panel's
   * Window node in its title bar, the root with nothing focused (the
   * page's body). Null where the keys aren't the views' (a popup menu, an
   * alert: the host's own controls), and outside an app that doesn't fill
   * the page.
   */
  start(target) {
    const h = this.h, c = h.container;
    const el = target?.nodeType === 1 ? target : null;
    if (el === c) return h.root;
    if (!el || !c.contains(el)) return h.options.fullPage ? h.root : null;
    const r = this.nodeAt(el, null);
    return r === undefined ? h.root : r;
  }

  /**
   * up is where a key goes on from n: its parent; from a node the core
   * leaves without one, where it shows: a list's cell its list, a
   * sheet's node the window it's declared in (its Window prop: a Window
   * node, the root), a popover's the root of what its anchor is in (a
   * Window's, a sheet's node, the root). Null at the root and a Window
   * node: ends.
   */
  up(n) {
    if (n.parent) return n.parent;
    if (n.listCell) {
      const l = n.listCell.l?.n;
      return l && !l.dying ? l : null;
    }
    const root = this.h.root;
    if (n.kind === Kind.Sheet) {
      const o = this.h.nodes?.get(n.winID);
      return o && o !== n && !o.dying && (o === root || o.kind === Kind.Window) ? o : root;
    }
    if (n.kind === Kind.Popover) {
      let a = this.h.nodes?.get(n.pop?.anchor);
      for (let i = 0; a && i < 4096; i++) {
        const p = a.parent || a.listCell?.l?.n;
        if (!p) break;
        a = p;
      }
      return a && a !== n && !a.dying ? a : root;
    }
    return null;
  }

  /**
   * nodeAt is the node of el or its nearest ancestor below the container
   * (other than self): a view's element, or a sheet's, popover's or
   * Window's own element around it. Null in what's the host's alone (an
   * alert, a popup menu); undefined if nothing of the container's has it.
   */
  nodeAt(el, self) {
    const c = this.h.container;
    for (; el && el !== c; el = el.parentElement) {
      const n = el.asukaNode;
      if (n && n !== self) return n;
      const d = this.dialogOf(el);
      if (d && d !== self) return d.kind === Kind.Alert ? null : d;
      if (el.classList?.contains("asuka-menu")) return null;
    }
    return undefined;
  }

  dialogOf(el) {
    for (const x of this.h.nav?.dialogs || []) if (x.dialog === el) return x;
    return null;
  }
}

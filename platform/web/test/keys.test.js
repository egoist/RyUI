// A view's keys (src/keys.js): the Keys prop's encoding as ops.AppendKeys
// writes it, a key event's name and modifiers as ops.MatchKey matches
// them, and the routing the fake host does (host/fake's Key): from the
// focused element's view out, the nearest whose keys have it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeKeys, keyName, keyOf, matchKey, declaredMods, Keys } from "../src/keys.js";
import { Kind, Event } from "../src/ops.js";

const fromHex = (s) => new Uint8Array(s.match(/../g).map((x) => parseInt(x, 16)));

// ops.AppendKeys(nil, []ops.Key{{"down", 0}, {"n", 8}, {"é", 5}}), and
// how many keys ops.DecodeKeys reads of it cut to each length.
const golden = "0300000004000000646f776e00010000006e0802000000c3a905";
const goCounts = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3];

test("decodeKeys reads ops.AppendKeys; cut short, as ops.DecodeKeys", () => {
  const b = fromHex(golden);
  assert.deepEqual(decodeKeys(b), [{ name: "down", mods: 0 }, { name: "n", mods: 8 }, { name: "é", mods: 5 }]);
  for (let n = 0; n <= b.length; n++) assert.equal(decodeKeys(b.subarray(0, n)).length, goCounts[n], `cut to ${n} bytes`);
  // A count past the bytes: none.
  assert.deepEqual(decodeKeys(fromHex("ffffff7f")), []);
  assert.deepEqual(decodeKeys(new Uint8Array(0)), []);
  // At an offset in a bigger buffer, as a batch's bytes are.
  const big = new Uint8Array(b.length + 7);
  big.set(b, 7);
  assert.equal(decodeKeys(big.subarray(7)).length, 3);
});

// ev is a keydown: the key, its code, and what's held.
function ev(key, code, mods = {}) {
  return { key, code, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, isComposing: false, keyCode: 0, repeat: false, ...mods };
}

test("keyName: named keys, and a character as the key types it without modifiers", () => {
  const cases = [
    [ev("Enter", "Enter"), "enter"],
    [ev("Enter", "NumpadEnter"), "enter"],
    [ev("Escape", "Escape"), "escape"],
    [ev("Tab", "Tab", { shiftKey: true }), "tab"],
    [ev(" ", "Space"), "space"],
    [ev(" ", "Space", { altKey: true }), "space"], // ⌥Space on a Mac
    [ev("Delete", "Delete"), "delete"],
    [ev("Backspace", "Backspace"), "backspace"],
    [ev("ArrowUp", "ArrowUp"), "up"],
    [ev("ArrowDown", "ArrowDown"), "down"],
    [ev("ArrowLeft", "ArrowLeft"), "left"],
    [ev("ArrowRight", "ArrowRight"), "right"],
    [ev("Home", "Home"), "home"],
    [ev("End", "End"), "end"],
    [ev("PageUp", "PageUp"), "pageup"],
    [ev("PageDown", "PageDown"), "pagedown"],
    [ev("F1", "F1"), "f1"],
    [ev("F12", "F12"), "f12"],
    [ev("a", "KeyA"), "a"],
    [ev("A", "KeyA", { shiftKey: true }), "a"], // Shift is a modifier
    [ev("A", "KeyA"), "a"], // Caps Lock
    [ev("!", "Digit1", { shiftKey: true }), "1"], // Shift's other character: the key's own
    [ev("?", "Slash", { shiftKey: true }), "/"],
    [ev("å", "KeyA", { altKey: true }), "a"], // a Mac's ⌥ types another character
    [ev("Dead", "KeyE", { altKey: true }), "e"], // ⌥E: a dead key
    [ev("≤", "Comma", { altKey: true }), ","],
    [ev("a", "KeyQ"), "a"], // AZERTY: the character typed, not the key's place
    [ev("ф", "KeyA"), "ф"], // a Russian keyboard
    [ev(",", "Comma"), ","],
    [ev("Shift", "ShiftLeft", { shiftKey: true }), ""], // a modifier alone
    [ev("Meta", "MetaLeft", { metaKey: true }), ""],
    [ev("Unidentified", ""), ""],
  ];
  for (const [e, want] of cases) assert.equal(keyName(e, null), want, `${e.key} ${e.code}`);
});

test("keyName reads a key's other characters off the keyboard's layout, where the browser has it", () => {
  const azerty = new Map([["KeyQ", "a"], ["KeyA", "q"], ["Digit1", "&"], ["KeyE", "e"]]);
  assert.equal(keyName(ev("1", "Digit1", { shiftKey: true }), azerty), "&");
  assert.equal(keyName(ev("æ", "KeyQ", { altKey: true }), azerty), "a");
  assert.equal(keyName(ev("A", "KeyQ", { shiftKey: true }), azerty), "a");
  // Without one, a US keyboard's.
  assert.equal(keyName(ev("1", "Digit1", { shiftKey: true }), null), "1");
});

test("keyOf: the modifiers, ⌘ on a Mac and Ctrl elsewhere", () => {
  assert.deepEqual(keyOf(ev("d", "KeyD", { metaKey: true }), true), { names: ["d"], mods: 1 });
  assert.deepEqual(keyOf(ev("d", "KeyD", { ctrlKey: true }), false), { names: ["d"], mods: 1 });
  assert.deepEqual(keyOf(ev("n", "KeyN", { ctrlKey: true }), true), { names: ["n"], mods: 8 }); // a Mac's ⌃
  assert.deepEqual(keyOf(ev("Z", "KeyZ", { metaKey: true, shiftKey: true }), true), { names: ["z"], mods: 3 });
  assert.deepEqual(keyOf(ev("ArrowUp", "ArrowUp", { altKey: true }), true), { names: ["up"], mods: 4 });
  assert.deepEqual(keyOf(ev("π", "KeyP", { altKey: true, metaKey: true }), true), { names: ["p"], mods: 5 });
  // A non-Latin keyboard: the key's US character too, as the menus match it.
  assert.deepEqual(keyOf(ev("с", "KeyC", { metaKey: true }), true), { names: ["с", "c"], mods: 1 });
  // A Latin one's character is the one typed only (AZERTY's A, where a US Q is).
  assert.deepEqual(keyOf(ev("a", "KeyQ", { ctrlKey: true }), false), { names: ["a"], mods: 1 });
  // The Windows key, AltGr (a character typed): not a view's.
  assert.equal(keyOf(ev("d", "KeyD", { metaKey: true }), false), null);
  assert.equal(keyOf({ ...ev("@", "KeyQ", { ctrlKey: true, altKey: true }), getModifierState: (m) => m === "AltGraph" }, false), null);
});

test("keyOf: never while an input method composes text", () => {
  assert.equal(keyOf(ev("ArrowDown", "ArrowDown", { isComposing: true }), true), null);
  assert.equal(keyOf(ev("Process", "KeyK", { keyCode: 229 }), false), null);
  assert.equal(keyOf(ev("Enter", "Enter", { keyCode: 229 }), true), null);
});

test("matchKey: the key and exactly its modifiers; Control is Ctrl off a Mac", () => {
  const keys = [{ name: "down", mods: 0 }, { name: "n", mods: 8 }, { name: "s", mods: 1 }, { name: "z", mods: 3 }];
  assert.equal(matchKey(keys, { names: ["down"], mods: 0 }, true), 0);
  assert.equal(matchKey(keys, { names: ["down"], mods: 2 }, true), -1);
  assert.equal(matchKey(keys, { names: ["n"], mods: 8 }, true), 1);
  assert.equal(matchKey(keys, { names: ["n"], mods: 1 }, true), -1);
  assert.equal(matchKey(keys, { names: ["n"], mods: 1 }, false), 1);
  assert.equal(matchKey(keys, { names: ["s"], mods: 1 }, false), 2);
  assert.equal(matchKey(keys, { names: ["z"], mods: 3 }, true), 3);
  assert.equal(matchKey(keys, { names: ["ы", "s"], mods: 1 }, true), 2);
  assert.equal(declaredMods(8 | 2, false), 3);
  assert.equal(declaredMods(8 | 2, true), 10);
});

// --- Routing, over a page of elements and nodes ---

globalThis.document ??= {};

/** element is a DOM element under parent: what keys.js reads of one. */
function element(parent, ...classes) {
  return { nodeType: 1, parentElement: parent, classList: { contains: (c) => classes.includes(c) } };
}

/** page is a host over a container: a root view, nodes made by node(). */
function page({ fullPage = true } = {}) {
  const body = element(null);
  const container = element(body);
  container.contains = (el) => { for (; el; el = el.parentElement) if (el === container) return true; return false; };
  const sent = [];
  let next = 1;
  const h = { container, options: { fullPage }, nav: { dialogs: [] }, dispatch: (b) => sent.push(decodeEvent(b)) };
  const node = (parent, kind = Kind.View, keys = null, inEl = parent?.el) => {
    const n = { id: next++, kind, parent, el: element(inEl ?? container), keys: null, dialog: null };
    n.el.asukaNode = n;
    if (keys) n.keys = keys;
    return n;
  };
  h.root = node(null);
  const k = new Keys(h);
  k.mac = true;
  const press = (target, key, code, mods) => {
    const e = { ...ev(key, code, mods), target, prevented: 0, stopped: 0 };
    e.preventDefault = () => e.prevented++;
    e.stopImmediatePropagation = () => e.stopped++;
    const took = k.key(e);
    return { took, e };
  };
  return { h, k, body, container, node, sent, press };
}

function decodeEvent(b) {
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  return { id: v.getUint32(0, true), kind: v.getUint16(4, true), index: v.getUint32(6, true) };
}

const K = (...names) => names.map((s) => { const [name, mods = "0"] = s.split("+"); return { name, mods: +mods }; });

test("a key goes to the nearest view with it, from the focused one out", () => {
  const { h, node, sent, press } = page();
  const results = node(h.root, Kind.View, K("down", "up", "enter"));
  const row = node(results);
  const field = node(row, Kind.TextField, K("escape"));
  const input = element(field.el); // the field's <input>, in its box

  // The nearest with the key: the field's own first, then its ancestors'.
  let r = press(input, "Escape", "Escape");
  assert.equal(r.took, true);
  assert.deepEqual(sent.pop(), { id: field.id, kind: Event.Key, index: 0 });
  assert.equal(r.e.prevented, 1, "the field doesn't get it");
  assert.equal(r.e.stopped, 1, "nor the page's listeners after (the menu bar's)");
  r = press(input, "ArrowDown", "ArrowDown");
  assert.deepEqual(sent.pop(), { id: results.id, kind: Event.Key, index: 0 });
  press(input, "Enter", "Enter");
  assert.deepEqual(sent.pop(), { id: results.id, kind: Event.Key, index: 2 });
  // Held: each repeat.
  r = press(input, "ArrowUp", "ArrowUp", { repeat: true });
  assert.deepEqual(sent.pop(), { id: results.id, kind: Event.Key, index: 1 });

  // A key no view has goes on to the focused control.
  r = press(input, "ArrowLeft", "ArrowLeft");
  assert.equal(r.took, false);
  assert.equal(r.e.prevented, 0);
  assert.equal(r.e.stopped, 0);
  // Other modifiers: not the key.
  r = press(input, "ArrowDown", "ArrowDown", { shiftKey: true });
  assert.equal(r.took, false);
  // Composing: the input method's.
  r = press(input, "Enter", "Enter", { isComposing: true });
  assert.equal(r.took, false);
  assert.equal(sent.length, 0);
});

test("the window's keys (UseKeys: the root's) come last, and first with nothing focused", () => {
  const { h, node, sent, press, body, container } = page();
  h.root.keys = K("space", "down", "s+1");
  const list = node(h.root, Kind.View, K("down"));
  const button = element(list.el);
  press(button, "ArrowDown", "ArrowDown");
  assert.deepEqual(sent.pop(), { id: list.id, kind: Event.Key, index: 0 });
  press(button, "s", "KeyS", { metaKey: true });
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 2 });
  // Nothing focused: the page's body, the document, the container.
  press(body, " ", "Space");
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 0 });
  press(globalThis.document, "ArrowDown", "ArrowDown");
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 1 });
  press(container, "ArrowDown", "ArrowDown");
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 1 });
  press(null, " ", "Space");
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 0 });
});

test("an app in an element of a page takes only the keys typed in it", () => {
  const { h, sent, press, body, container } = page({ fullPage: false });
  h.root.keys = K("space");
  assert.equal(press(body, " ", "Space").took, false);
  assert.equal(press(element(body), " ", "Space").took, false);
  assert.equal(press(container, " ", "Space").took, true);
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 0 });
});

test("a Window panel's keys end at its Window node, its UseKeys'", () => {
  const { h, node, sent, press, container } = page();
  h.root.keys = K("escape", "space");
  // The panel: its own element around the title bar and the Window node's.
  const panel = element(container, "asuka-window");
  const win = node(h.root, Kind.Window, K("escape"), panel);
  win.dialog = panel;
  h.nav.dialogs.push(win);
  const bar = element(panel, "asuka-window-bar");
  const content = node(win);
  const field = node(content, Kind.TextField);
  const input = element(field.el);
  press(input, "Escape", "Escape");
  assert.deepEqual(sent.pop(), { id: win.id, kind: Event.Key, index: 0 });
  // Not on to the main window's.
  assert.equal(press(input, " ", "Space").took, false);
  // The panel itself focused, or its title bar: the Window's.
  press(panel, "Escape", "Escape");
  assert.deepEqual(sent.pop(), { id: win.id, kind: Event.Key, index: 0 });
  press(bar, "Escape", "Escape");
  assert.deepEqual(sent.pop(), { id: win.id, kind: Event.Key, index: 0 });
  // A panel with no keys of its own: a declared escape is none, the host's Escape closes it.
  win.keys = null;
  assert.equal(press(input, "Escape", "Escape").took, false);
});

test("a sheet's content goes out through its node's parents; an alert's and a popup menu's keys are their own", () => {
  const { h, node, sent, press, container } = page();
  h.root.keys = K("enter");
  const screen = node(h.root);
  const dialog = element(container);
  const sheet = node(screen, Kind.Sheet, null, dialog);
  sheet.dialog = dialog;
  h.nav.dialogs.push(sheet);
  const grabber = element(dialog, "asuka-grabber");
  press(element(sheet.el), "Enter", "Enter");
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 0 });
  press(grabber, "Enter", "Enter");
  assert.deepEqual(sent.pop(), { id: h.root.id, kind: Event.Key, index: 0 });

  const alertDialog = element(container, "asuka-alert");
  const alert = { id: 99, kind: Kind.Alert, parent: screen, el: null, keys: null, dialog: alertDialog };
  h.nav.dialogs.push(alert);
  const ok = element(element(alertDialog, "asuka-alert-buttons"));
  assert.equal(press(ok, "Enter", "Enter").took, false);
  const menu = element(container, "asuka-menu");
  assert.equal(press(element(menu), "Enter", "Enter").took, false);
  assert.equal(sent.length, 0);
});

test("a list's row goes out through the host's cell to the list", () => {
  const { h, node, sent, press } = page();
  const list = node(h.root, Kind.List, K("pagedown"));
  const scroller = element(list.el, "asuka-scroller");
  // The host's cell: a node of no parent, its element in the list's.
  const cell = node(null, 1000, null, scroller);
  const row = node(cell);
  press(element(row.el), "PageDown", "PageDown");
  assert.deepEqual(sent.pop(), { id: list.id, kind: Event.Key, index: 0 });
});

test("the Keys prop: set, replaced, emptied", () => {
  const { h, k, node, sent, press } = page();
  const v = node(h.root);
  k.set(v, fromHex(golden));
  press(v.el, "n", "KeyN", { ctrlKey: true });
  assert.deepEqual(sent.pop(), { id: v.id, kind: Event.Key, index: 1 });
  k.set(v, new Uint8Array([0, 0, 0, 0]));
  assert.equal(v.keys, null);
  assert.equal(press(v.el, "n", "KeyN", { ctrlKey: true }).took, false);
});

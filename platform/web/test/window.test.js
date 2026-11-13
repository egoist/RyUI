// Windows in a page (src/window.js, src/nav.js): a flyout's anchor as
// its Window's Flyout prop carries it, where a flyout goes against its
// anchor (internal/flyout's Place, the same cases), where a panel opens,
// and what WindowAction Fit and ClickThrough do to a Window's panel and
// the tab.
import { test } from "node:test";
import assert from "node:assert/strict";
import "../src/host.js"; // nav.js is loaded through it (they import each other)
import { decodeFlyout } from "../src/nav.js";
import { Windows, Flag, State, place, center, trayAnchor, transient } from "../src/window.js";
import { WindowAction, Placement } from "../src/ops.js";

// What window.js reads of the page: nothing in full screen, no focus.
globalThis.document ??= { documentElement: {}, fullscreenElement: null, activeElement: null, hasFocus: () => false };

// flyoutBytes is ops.AppendFlyout's encoding.
function flyoutBytes(f) {
  const b = new Uint8Array(25);
  const v = new DataView(b.buffer);
  v.setUint32(0, f.tray, true);
  v.setFloat32(4, f.x, true); v.setFloat32(8, f.y, true); v.setFloat32(12, f.width, true); v.setFloat32(16, f.height, true);
  b[20] = f.placement;
  v.setFloat32(21, f.gap, true);
  return b;
}

test("decodeFlyout reads ops.Flyout; cut short, a point below the parent's top left", () => {
  const f = { tray: 3, x: 10, y: 20, width: 30, height: 40, placement: Placement.TopEnd, gap: 4 };
  assert.deepEqual(decodeFlyout(flyoutBytes(f)), f);
  const none = { tray: 0, x: 0, y: 0, width: 0, height: 0, placement: 0, gap: 0 };
  const b = flyoutBytes(f);
  for (let n = 0; n < b.length; n++) assert.deepEqual(decodeFlyout(b.subarray(0, n)), none, `cut to ${n} bytes`);
});

test("place: the desktop hosts' cases (internal/flyout)", () => {
  const work = { x: 0, y: 25, w: 1440, h: 875 }; // under a 25-point menu bar
  const icon = { x: 1200, y: 0, w: 24, h: 25 };
  const R = (x, y, w, h) => ({ x, y, w, h });
  const cases = [
    ["under a menu bar icon, centered", icon, 280, 300, Placement.Bottom, 0, R(1072, 25, 280, 300)],
    ["near the right edge, slid in", R(1420, 0, 20, 25), 280, 300, Placement.Bottom, 0, R(1160, 25, 280, 300)],
    ["start, flipped to the end", R(1300, 100, 100, 20), 200, 100, Placement.BottomStart, 4, R(1200, 124, 200, 100)],
    ["end", R(100, 100, 100, 20), 60, 50, Placement.BottomEnd, 0, R(140, 120, 60, 50)],
    ["bottom flips to top", R(1300, 870, 24, 30), 280, 300, Placement.Bottom, 8, R(1160, 562, 280, 300)],
    ["top", R(600, 500, 40, 20), 100, 100, Placement.Top, 4, R(570, 396, 100, 100)],
    ["right", R(100, 300, 50, 20), 100, 60, Placement.RightStart, 0, R(150, 300, 100, 60)],
    ["right flips to left", R(1400, 300, 20, 20), 100, 60, Placement.Right, 0, R(1300, 280, 100, 60)],
    ["taller than the work area: shrunk", icon, 200, 2000, Placement.Bottom, 0, R(1112, 25, 200, 875)],
    ["left flips to right", R(20, 300, 20, 20), 100, 60, Placement.LeftEnd, 0, R(40, 260, 100, 60)],
    ["top flips to bottom", R(600, 40, 40, 20), 100, 100, Placement.Top, 4, R(570, 64, 100, 100)],
    ["a placement out of range is Bottom", icon, 280, 300, 200, 0, R(1072, 25, 280, 300)],
  ];
  for (const [name, anchor, w, h, p, gap, want] of cases) assert.deepEqual(place(anchor, w, h, p, gap, work), want, name);
});

test("center: a third of the way down, within the area", () => {
  assert.deepEqual(center(600, 60, { x: 0, y: 25, w: 1440, h: 875 }), { x: 420, y: 25 + 815 / 3, w: 600, h: 60 });
  assert.deepEqual(center(2000, 2000, { x: 0, y: 0, w: 800, h: 600 }), { x: 0, y: 0, w: 800, h: 600 });
});

test("a tray icon's flyout hangs from the top right corner", () => {
  const work = { x: 8, y: 8, w: 984, h: 784 };
  assert.deepEqual(place(trayAnchor(1000), 280, 300, Placement.Bottom, 4, work), { x: 712, y: 8, w: 280, h: 300 });
});

test("transient: flyouts and panels", () => {
  assert.equal(transient({ win: { cfg: { flags: Flag.Flyout } } }), true);
  assert.equal(transient({ win: { cfg: { flags: Flag.Panel | Flag.AlwaysOnTop } } }), true);
  assert.equal(transient({ win: { cfg: { flags: Flag.Modal | Flag.Child } } }), false);
  assert.equal(transient({ win: {} }), false);
});

// fakeWindows is a Windows over a host with an app area w by h, counting
// what it asks the navigation for.
function fakeWindows(w = 1000, h = 800) {
  const calls = { sized: 0, placed: 0, events: [] };
  const later = [];
  const host = {
    root: { id: 1, listening: () => false },
    container: { getBoundingClientRect: () => ({ left: 0, top: 0, width: w, height: h }) },
    nav: { sizeDialog: () => calls.sized++, placeNew: () => calls.placed++, raiseWindow() {} },
    afterCore: (f) => later.push(f),
    dispatch: (b) => calls.events.push(b),
  };
  const ws = Object.create(Windows.prototype);
  ws.h = host;
  return { ws, host, calls };
}

function action(a, x = 0, y = 0) {
  const b = new Uint8Array(9);
  const v = new DataView(b.buffer);
  b[0] = a;
  v.setFloat32(1, x, true); v.setFloat32(5, y, true);
  return a === WindowAction.Fit || a === WindowAction.Size || a === WindowAction.Position ? b : b.subarray(0, 1);
}

function panel(flags, win = {}) {
  const classes = new Set();
  return {
    id: 7, listening: () => false,
    dialog: { classList: { toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)) }, contains: () => false },
    classes,
    win: { cfg: { flags, stateKey: "" }, x: 100, y: 50, w: 480, h: 600, bar: 0, hidden: false, ...win },
  };
}

test("Fit sizes a panel to its content, keeping its top left after the first", () => {
  const { ws, calls } = fakeWindows();
  const n = panel(Flag.Panel | Flag.FitContent, { autoPlaced: true });
  ws.action(n, action(WindowAction.Fit, 600, 56));
  assert.deepEqual([n.win.w, n.win.h], [600, 56]);
  assert.equal(calls.placed, 1, "placed as it opens, at its first size");
  n.win.x = 200; n.win.y = 100;
  ws.action(n, action(WindowAction.Fit, 600, 300));
  assert.deepEqual([n.win.x, n.win.y, n.win.w, n.win.h], [200, 100, 600, 300]);
  assert.equal(calls.placed, 1, "not placed again");
  // A title bar adds to the content's height; the app's area bounds it.
  const t = panel(Flag.FitContent, { bar: 36, autoPlaced: false });
  ws.action(t, action(WindowAction.Fit, 5000, 100));
  assert.deepEqual([t.win.w, t.win.h], [984, 136]);
  assert.equal(calls.placed, 1, "a window placed where its options say stays there");
});

test("Fit on a flyout is the size it's placed at again", () => {
  const { ws, calls } = fakeWindows();
  const n = panel(Flag.Flyout | Flag.FitContent, { flyout: decodeFlyout(new Uint8Array(0)), want: { w: 320, h: 400 } });
  const sized = calls.sized;
  ws.action(n, action(WindowAction.Fit, 280, 120));
  assert.deepEqual(n.win.want, { w: 280, h: 120 });
  assert.equal(calls.sized, sized + 1);
  assert.equal(calls.placed, 0);
});

test("Fit and ClickThrough do nothing to the tab", () => {
  const { ws, host, calls } = fakeWindows();
  ws.action(host.root, action(WindowAction.Fit, 300, 200));
  ws.action(host.root, action(WindowAction.ClickThrough));
  assert.equal(calls.sized, 0);
  assert.equal(calls.placed, 0);
});

test("ClickThrough lets the pointer through a panel and is reported", () => {
  const { ws } = fakeWindows();
  const n = panel(0);
  ws.action(n, action(WindowAction.ClickThrough));
  assert.equal(n.win.clickThrough, true);
  assert.ok(n.classes.has("asuka-window-click-through"));
  assert.equal(ws.state(n).f, State.Visible | State.ClickThrough);
  ws.action(n, action(WindowAction.NotClickThrough));
  assert.ok(!n.classes.has("asuka-window-click-through"));
  assert.equal(ws.state(n).f, State.Visible);
});

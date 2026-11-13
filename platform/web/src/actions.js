// Action menus as the core sends them (ops/actions.go): a view's Menu, a
// list cell's swipe actions, a screen's bar buttons and an alert's
// buttons. A menu is u32 count, then per item u8 role (AlertButtonRole),
// a string title, a string icon and u8 MenuFlags; with the submenu flag,
// its items follow, a menu. Items are numbered depth first, a submenu or
// section before its own items: Action carries the number.

import { Reader } from "./ops.js";

/** MenuFlags (ops/menu.go) an action's item has. */
export const MenuFlags = Object.freeze({ Disabled: 2, Checked: 4, Submenu: 16, Inline: 32 });

/** decodeActions reads a menu: [{ index, role, title, icon, flags, items }], items a submenu's or section's; a cut menu ends where it's cut. */
export function decodeActions(b) {
  if (!b || b.length < 4) return [];
  const r = new Reader(b);
  let next = 0, cut = false;
  // A short read (past the end) stops the menu where it's cut.
  const read = (f) => {
    if (cut) return undefined;
    try {
      const v = f();
      if (r.p > r.b.length) cut = true;
      return v;
    } catch {
      cut = true;
      return undefined;
    }
  };
  const menu = (depth) => {
    const out = [];
    const n = read(() => r.u32());
    for (let i = 0; !cut && i < n; i++) {
      const role = read(() => r.u8()), title = read(() => r.string()), icon = read(() => r.string()), flags = read(() => r.u8());
      if (cut) break;
      const it = { index: next++, role, title, icon, flags, items: [] };
      out.push(it);
      if (flags & MenuFlags.Submenu) {
        if (depth >= 16) { cut = true; break; }
        it.items = menu(depth + 1);
      }
    }
    return out;
  };
  return menu(0);
}

/** flatActions lists items and their children's items, depth first. */
export function flatActions(items) {
  const out = [];
  const walk = (as) => { for (const a of as) { out.push(a); walk(a.items); } };
  walk(items);
  return out;
}

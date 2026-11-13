// Menus: a view's or a button's (Menu, MenuTrigger) as a popup of the
// page's (the top layer), opened by a click or, for a long press menu,
// a long press or a secondary click; and the app's menu bar's shortcuts
// (MenuBar), which work in the page as the desktop hosts' do. A menu's
// items follow the ARIA menu pattern: arrows move between the items
// that can be chosen, a submenu opens with the arrow toward its side
// (the right, the left right to left) and closes with the other or
// Escape; a section is a group set off by separators, under its title.

import { Event, Events, Reader } from "./ops.js";
import { decodeActions, MenuFlags } from "./actions.js";
import { symbolSVG } from "./media.js";
import { apple, modsOf, declaredMods } from "./keys.js";

/** withPicks gives each item that can be chosen pick(): f(item). */
export function withPicks(items, f) {
  return items.map((a) => ({ ...a, items: withPicks(a.items || [], f), pick: () => f(a) }));
}

export class Menus {
  constructor(h) {
    this.h = h;
    this.popup = null;
    this.shortcuts = [];
    addEventListener("keydown", (e) => this.key(e), true);
  }

  set(n, b) {
    const items = decodeActions(b);
    n.menu = items.length ? items : null;
    if (n.menu && n.el) this.h.interaction.pressable(n);
  }

  trigger(n, v) { n.menuTrigger = v; }

  /** open shows n's menu at x, y (or below anchor); a pick sends Action with the item's number. */
  open(n, x, y, anchor) {
    if (!n.menu) return;
    const id = n.id;
    this.show(withPicks(n.menu, (a) => this.h.dispatch(Events.action(id, a.index))), x, y, anchor);
  }

  /** show shows items (each with pick()) at x, y, or below anchor. */
  show(items, x, y, anchor) {
    this.close();
    const m = this.build(items, null);
    m.popover = "auto";
    m.addEventListener("toggle", (e) => { if (e.newState === "closed") { m.remove(); if (this.popup === m) this.popup = null; } });
    this.h.container.appendChild(m);
    m.showPopover();
    const r = m.getBoundingClientRect();
    const c = this.h.container.getBoundingClientRect();
    let left = x, top = y;
    if (anchor) {
      const a = anchor.getBoundingClientRect();
      left = this.h.rtl ? a.right - r.width : a.left;
      top = a.bottom + 4;
      if (top + r.height > c.bottom - 8) top = a.top - 4 - r.height;
    }
    this.place(m, left, top);
    this.popup = m;
    this.first(m)?.focus({ preventScroll: true });
  }

  /** place puts a menu at left, top, kept in the page's box. */
  place(m, left, top) {
    const r = m.getBoundingClientRect();
    const c = this.h.container.getBoundingClientRect();
    left = Math.min(Math.max(left, c.left + 8), c.right - r.width - 8);
    top = Math.min(Math.max(top, c.top + 8), c.bottom - r.height - 8);
    m.style.left = left + "px";
    m.style.top = top + "px";
  }

  /** The items of menu m that can be chosen (or open a submenu), in order. */
  choosable(m) { return [...m.querySelectorAll(":scope > .asuka-menu-item, :scope > [role=group] > .asuka-menu-item")].filter((b) => !b.disabled); }
  first(m) { return this.choosable(m)[0]; }

  /** build makes a menu element of items; parent is the item that opens it (a submenu). */
  build(items, parent) {
    const m = document.createElement("div");
    m.className = "asuka-menu";
    m.setAttribute("role", "menu");
    m.dir = this.h.rtl ? "rtl" : "ltr";
    if (parent) m.setAttribute("aria-label", parent.title);
    const checks = items.some((a) => a.flags & MenuFlags.Checked) ||
      items.some((a) => (a.flags & MenuFlags.Inline) && a.items.some((c) => c.flags & MenuFlags.Checked));
    m.classList.toggle("asuka-menu-checks", checks);
    let last = null; // what was added last: "item", "section"
    const separator = () => {
      const s = document.createElement("div");
      s.className = "asuka-menu-separator";
      s.setAttribute("role", "separator");
      m.appendChild(s);
    };
    for (const a of items) {
      if (a.role === 1) continue; // a cancel action: the menu's dismissal
      if ((a.flags & MenuFlags.Submenu) && (a.flags & MenuFlags.Inline)) {
        if (!a.items.length) continue;
        if (last) separator();
        const g = document.createElement("div");
        g.setAttribute("role", "group");
        if (a.title) {
          const hd = document.createElement("div");
          hd.className = "asuka-menu-header";
          hd.id = "asuka-menu-h" + (Menus.ids = (Menus.ids || 0) + 1);
          hd.setAttribute("role", "presentation");
          hd.textContent = a.title;
          g.appendChild(hd);
          g.setAttribute("aria-labelledby", hd.id);
        }
        for (const c of a.items) if (c.role !== 1) g.appendChild(this.item(c, m));
        m.appendChild(g);
        last = "section";
        continue;
      }
      if (last === "section") separator();
      m.appendChild(this.item(a, m));
      last = "item";
    }
    m.addEventListener("keydown", (e) => this.menuKey(m, e));
    return m;
  }

  /** item makes a menu's item: a button, which picks it or opens its submenu. */
  item(a, m) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "asuka-menu-item asuka-control";
    b.tabIndex = -1;
    const sub = (a.flags & MenuFlags.Submenu) !== 0;
    const checked = (a.flags & MenuFlags.Checked) !== 0;
    b.setAttribute("role", checked ? "menuitemcheckbox" : "menuitem");
    if (checked) b.setAttribute("aria-checked", "true");
    if (a.flags & MenuFlags.Disabled) {
      b.disabled = true;
      b.setAttribute("aria-disabled", "true");
    }
    if (a.role === 2) b.classList.add("asuka-destructive");
    if (checked) b.classList.add("asuka-checked");
    const check = document.createElement("span");
    check.className = "asuka-menu-check";
    check.setAttribute("aria-hidden", "true");
    if (checked) { const s = symbolSVG("checkmark", this.h.rtl); if (s) check.appendChild(s); }
    b.appendChild(check);
    const t = document.createElement("span");
    t.className = "asuka-menu-title";
    t.textContent = a.title;
    b.appendChild(t);
    if (a.icon) { const s = symbolSVG(a.icon, this.h.rtl); if (s) b.appendChild(s); }
    if (sub) {
      b.setAttribute("aria-haspopup", "menu");
      b.setAttribute("aria-expanded", "false");
      const s = symbolSVG("chevron.forward", this.h.rtl);
      if (s) { s.classList.add("asuka-menu-arrow"); b.appendChild(s); }
      b.addEventListener("click", () => this.openSub(b, a, true));
      b.addEventListener("pointerenter", () => { clearTimeout(this.hover); this.hover = setTimeout(() => this.openSub(b, a, false), 200); });
      b.addEventListener("pointerleave", () => clearTimeout(this.hover));
    } else {
      b.addEventListener("click", () => {
        this.close();
        this.h.afterCore(a.pick);
      });
      // Moving onto another item closes a submenu open beside it.
      b.addEventListener("pointerenter", () => { clearTimeout(this.hover); this.hover = setTimeout(() => this.closeSubs(m), 200); });
    }
    return b;
  }

  /** openSub opens item b's submenu beside it; focus: its first item. */
  openSub(b, a, focus) {
    const m = b.closest(".asuka-menu");
    if (!m || !b.isConnected) return;
    if (b.sub?.isConnected) {
      if (focus) this.first(b.sub)?.focus({ preventScroll: true });
      return;
    }
    this.closeSubs(m);
    const s = this.build(a.items, a);
    s.popover = "auto";
    s.addEventListener("toggle", (e) => {
      if (e.newState === "closed") { s.remove(); b.setAttribute("aria-expanded", "false"); if (b.sub === s) b.sub = null; }
    });
    m.appendChild(s); // inside its menu: its popover stays open with it
    s.showPopover();
    b.sub = s;
    b.setAttribute("aria-expanded", "true");
    const r = b.getBoundingClientRect();
    const w = s.getBoundingClientRect().width;
    const c = this.h.container.getBoundingClientRect();
    // On the reading direction's side of the item, or the other where it doesn't fit.
    let left = this.h.rtl ? r.left - w + 4 : r.right - 4;
    if (!this.h.rtl && left + w > c.right - 8) left = r.left - w + 4;
    if (this.h.rtl && left < c.left + 8) left = r.right - 4;
    this.place(s, left, r.top - 5);
    if (focus) this.first(s)?.focus({ preventScroll: true });
  }

  /** closeSubs closes the submenus open from menu m. */
  closeSubs(m) {
    for (const b of m.querySelectorAll(":scope > .asuka-menu-item, :scope > [role=group] > .asuka-menu-item")) {
      if (b.sub) { try { b.sub.hidePopover(); } catch {} b.sub?.remove(); b.sub = null; b.setAttribute("aria-expanded", "false"); }
    }
  }

  menuKey(m, e) {
    if (e.target.closest(".asuka-menu") !== m) return; // a submenu's own
    const bs = this.choosable(m);
    const i = bs.indexOf(document.activeElement);
    const into = this.h.rtl ? "ArrowLeft" : "ArrowRight";
    const out = this.h.rtl ? "ArrowRight" : "ArrowLeft";
    const opener = m.parentElement?.closest(".asuka-menu") ? [...m.parentElement.querySelectorAll(".asuka-menu-item")].find((b) => b.sub === m) : null;
    switch (e.key) {
      case "ArrowDown": bs[(i + 1) % bs.length]?.focus(); break;
      case "ArrowUp": bs[(i - 1 + bs.length) % bs.length]?.focus(); break;
      case "Home": bs[0]?.focus(); break;
      case "End": bs[bs.length - 1]?.focus(); break;
      case into: {
        const b = bs[i];
        if (b?.getAttribute("aria-haspopup")) b.click();
        else return;
        break;
      }
      case out:
      case "Escape":
        if (!opener) return; // the top menu: Escape closes it as a popover
        try { m.hidePopover(); } catch {}
        opener.focus();
        break;
      default:
        return;
    }
    e.preventDefault();
    e.stopPropagation();
  }

  close() {
    clearTimeout(this.hover);
    if (this.popup) {
      try { this.popup.hidePopover(); } catch {}
      this.popup?.remove();
      this.popup = null;
    }
  }

  /** menuBar takes the app's menus: their items' shortcuts work in the page, sending Action on the root with the item's index among all the menus' items, depth first (a submenu's after the item that opens it). */
  menuBar(n, args) {
    const r = new Reader(args);
    const count = r.u32();
    const out = [];
    let index = 0;
    // A menu (ops/menu.go): u32 count, per item u8 flags (1 separator,
    // 2 disabled, 16 submenu), string title, string key, u8 modifiers,
    // u8 role, then a submenu's menu.
    const menu = (depth) => {
      const k = r.u32();
      for (let j = 0; j < k; j++) {
        const flags = r.u8();
        r.string();
        const key = r.string();
        const mods = r.u8();
        const role = r.u8();
        const at = index++;
        if (key && !(flags & (1 | 2 | 16)) && !role) out.push({ key: key.toLowerCase(), mods, index: at, root: n.id });
        if (flags & 16 && depth < 16) menu(depth + 1);
      }
    };
    for (let i = 0; i < count; i++) {
      r.string();
      menu(0);
    }
    this.shortcuts = out;
  }

  key(e) {
    if (!this.shortcuts.length) return;
    if (!this.h.options.fullPage && !this.h.container.contains(e.target)) return;
    // The modifiers as a view's Keys have them (keys.js routes a key
    // first: one a view takes is stopped before it comes here).
    const mac = apple();
    const mods = modsOf(e, mac);
    const names = { enter: "Enter", escape: "Escape", tab: "Tab", space: " ", delete: "Delete", backspace: "Backspace", up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight", home: "Home", end: "End", pageup: "PageUp", pagedown: "PageDown" };
    for (const s of this.shortcuts) {
      const want = names[s.key] || (/^f\d+$/.test(s.key) ? s.key.toUpperCase() : s.key);
      const got = want.length === 1 ? e.key.toLowerCase() : e.key;
      const code = want.length === 1 && e.code === "Key" + want.toUpperCase();
      if ((got === want || code) && mods === declaredMods(s.mods, mac)) {
        e.preventDefault();
        e.stopPropagation();
        this.h.dispatch(Events.action(s.root, s.index));
        return;
      }
    }
  }
}

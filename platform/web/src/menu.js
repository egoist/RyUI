// Menus: a view's or a button's (Menu, MenuTrigger) as a popup of the
// page's (the top layer), opened by a click or, for a long press menu,
// a long press or a secondary click; and the app's menu bar's shortcuts
// (MenuBar), which work in the page as the desktop hosts' do.

import { Event, Events, Reader } from "./ops.js";
import { decodeActions } from "./host.js";
import { symbolSVG } from "./media.js";

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

  /** open shows n's menu at x, y (or below anchor); a pick sends Action with its index. */
  open(n, x, y, anchor) {
    if (!n.menu) return;
    this.show(n.menu.map((a, i) => ({ ...a, pick: () => this.h.dispatch(Events.action(n.id, i)) })), x, y, anchor);
  }

  show(items, x, y, anchor) {
    this.close();
    const m = document.createElement("div");
    m.className = "ryui-menu";
    m.popover = "auto";
    m.setAttribute("role", "menu");
    for (const it of items) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "ryui-menu-item ryui-control";
      b.setAttribute("role", "menuitem");
      if (it.role === 2) b.classList.add("ryui-destructive");
      if (it.icon) { const s = symbolSVG(it.icon, this.h.rtl); if (s) b.appendChild(s); }
      const t = document.createElement("span");
      t.textContent = it.title;
      b.appendChild(t);
      b.addEventListener("click", () => {
        this.close();
        this.h.afterCore(it.pick);
      });
      m.appendChild(b);
    }
    m.addEventListener("keydown", (e) => {
      const bs = [...m.querySelectorAll("button")];
      const i = bs.indexOf(document.activeElement);
      if (e.key === "ArrowDown") { bs[(i + 1) % bs.length]?.focus(); e.preventDefault(); }
      if (e.key === "ArrowUp") { bs[(i - 1 + bs.length) % bs.length]?.focus(); e.preventDefault(); }
    });
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
    left = Math.min(Math.max(left, c.left + 8), c.right - r.width - 8);
    top = Math.min(Math.max(top, c.top + 8), c.bottom - r.height - 8);
    m.style.left = left + "px";
    m.style.top = top + "px";
    this.popup = m;
    m.querySelector("button")?.focus({ preventScroll: true });
  }

  close() {
    if (this.popup) {
      try { this.popup.hidePopover(); } catch {}
      this.popup?.remove();
      this.popup = null;
    }
  }

  /** menuBar takes the app's menus: their items' shortcuts work in the page, sending Action on the root with the item's index among all the menus' items. */
  menuBar(n, args) {
    const r = new Reader(args);
    const count = r.u32();
    const out = [];
    let index = 0;
    for (let i = 0; i < count; i++) {
      r.string();
      const k = r.u32();
      for (let j = 0; j < k; j++) {
        const flags = r.u8();
        r.string();
        const key = r.string();
        const mods = r.u8();
        const at = index++;
        if (key && !(flags & 1) && !(flags & 2)) out.push({ key: key.toLowerCase(), mods, index: at, root: n.id });
      }
    }
    this.shortcuts = out;
  }

  key(e) {
    if (!this.shortcuts.length) return;
    if (!this.h.options.fullPage && !this.h.container.contains(e.target)) return;
    const mac = /Mac|iPhone|iPad/.test(navigator.platform);
    const mods = ((mac ? e.metaKey : e.ctrlKey) ? 1 : 0) | (e.shiftKey ? 2 : 0) | (e.altKey ? 4 : 0) | (mac && e.ctrlKey ? 8 : 0);
    const names = { enter: "Enter", escape: "Escape", tab: "Tab", space: " ", delete: "Delete", backspace: "Backspace", up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight", home: "Home", end: "End", pageup: "PageUp", pagedown: "PageDown" };
    for (const s of this.shortcuts) {
      const want = names[s.key] || (/^f\d+$/.test(s.key) ? s.key.toUpperCase() : s.key);
      const got = want.length === 1 ? e.key.toLowerCase() : e.key;
      const code = want.length === 1 && e.code === "Key" + want.toUpperCase();
      if ((got === want || code) && mods === (s.mods & 15)) {
        e.preventDefault();
        e.stopPropagation();
        this.h.dispatch(Events.action(s.root, s.index));
        return;
      }
    }
  }
}

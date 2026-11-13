// Scroll and List. A Scroll is the browser's scrolling element, its one
// child the content (the content's frame is its size). A List is
// core-virtualized, as on the desktop hosts: a scrolling element whose
// rows the host places itself. As it scrolls, rows coming into view take
// a row view (a recycled one, or a new one) and its cells, host-made
// views the core binds rows into through ListBind; rows going out are
// kept for reuse. A row's height is its cells' (a grid's row holds
// Columns of them), from their frames; rows not yet bound are estimated.

import { Event, Events, Command, Reader, KindProp as P, Prop } from "./ops.js";
import { own } from "./views.js";
import { decodeActions } from "./host.js";

const KindRow = 0xfff0, KindCell = 0xfff1;
const cardGap = 0; // an inset grouped list's rows touch, as one card

// --- Scroll ---

export const scrollKind = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-scroller asuka-vertical";
    n.horizontal = false;
    n.paging = false;
    wireScroll(h, n, el);
  },
  prop(h, n, p, v) {
    switch (p) {
      case P.Scroll.Horizontal:
        n.horizontal = !!v;
        n.el.classList.toggle("asuka-horizontal", !!v);
        n.el.classList.toggle("asuka-vertical", !v);
        break;
      case P.Scroll.Paging:
        n.paging = !!v;
        n.el.classList.toggle("asuka-paging", !!v);
        pages(n);
        break;
      case P.Scroll.Refreshable:
        refreshable(h, n, !!v);
        break;
      case P.Scroll.Refreshing:
        refreshing(n, !!v);
        break;
    }
  },
  insert(h, p, c, i) {
    h.placeChild(p, c, i);
    return true;
  },
  frame(h, n, x, y, w, hh) {
    h.place(n, x, y, w, hh);
    pages(n);
    return true;
  },
  command(h, n, cmd, args) {
    if (cmd === Command.ScrollToOffset) {
      const r = new Reader(args);
      const x = r.f32(), y = r.f32(), animated = r.u8() !== 0;
      n.el.scrollTo({ left: x, top: y, behavior: animated ? "smooth" : "instant" });
      return true;
    }
    return false;
  },
  measure() { return [0, 0]; },
};

/** wireScroll reports a scrolling element's offset (Scrolled), and a paging one's page (Paged). */
function wireScroll(h, n, el) {
  let queued = false;
  el.addEventListener("scroll", () => {
    if (n.list) n.list.scrolled();
    if (queued || !n.listening(Event.Scrolled)) return;
    queued = true;
    requestAnimationFrame(() => {
      queued = false;
      h.dispatch(Events.scrolled(n.id, el.scrollLeft, el.scrollTop));
    });
  }, { passive: true });
  el.addEventListener("scrollend", () => {
    if (!n.paging || !n.listening(Event.Paged)) return;
    const size = n.horizontal ? el.clientWidth : el.clientHeight;
    const page = Math.round((n.horizontal ? el.scrollLeft : el.scrollTop) / (size || 1));
    if (page !== n.page) {
      n.page = page;
      h.dispatch(Events.paged(n.id, page));
    }
  });
}

/** pages puts a snap point at each page of a paging Scroll. */
function pages(n) {
  n.snaps?.forEach((s) => s.remove());
  n.snaps = null;
  if (!n.paging) return;
  const content = n.children[0];
  const size = n.horizontal ? n.frame[2] : n.frame[3];
  if (!content || size <= 0) return;
  const total = n.horizontal ? content.frame[2] : content.frame[3];
  const count = Math.max(1, Math.round(total / size));
  n.snaps = [];
  for (let i = 0; i < count; i++) {
    const s = document.createElement("div");
    s.className = "asuka-snap";
    s.style[n.horizontal ? "left" : "top"] = i * size + "px";
    s.style.width = (n.horizontal ? size : 1) + "px";
    s.style.height = (n.horizontal ? 1 : size) + "px";
    n.el.appendChild(s);
    n.snaps.push(s);
  }
}

// Pull to refresh: a touch pulling the content down from its top, past
// the threshold, sends Refresh; the indicator shows while Refreshing.
function refreshable(h, n, on) {
  if (!on) {
    n.refresher?.remove();
    n.refresher = null;
    return;
  }
  if (n.refresher) return;
  const r = document.createElement("div");
  r.className = "asuka-refresher";
  r.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-dasharray="42 100"/></svg>';
  n.refresher = r;
  n.el.appendChild(r);
  let y0 = null, pull = 0;
  n.el.addEventListener("touchstart", (e) => { y0 = n.el.scrollTop <= 0 ? e.touches[0].clientY : null; pull = 0; }, { passive: true });
  n.el.addEventListener("touchmove", (e) => {
    if (y0 == null || n.refreshing) return;
    pull = Math.max(0, e.touches[0].clientY - y0);
    r.style.opacity = String(Math.min(pull / 70, 1));
    r.style.transform = `translateY(${Math.min(pull, 90) / 2}px) rotate(${pull * 3}deg)`;
  }, { passive: true });
  n.el.addEventListener("touchend", () => {
    if (y0 == null) return;
    y0 = null;
    if (pull > 70 && !n.refreshing) h.emit(n, Event.Refresh, Events.refresh);
    if (!n.refreshing) { r.style.opacity = ""; r.style.transform = ""; }
  });
}

function refreshing(n, on) {
  n.refreshing = on;
  if (!n.refresher) return;
  n.refresher.classList.toggle("asuka-refreshing", on);
  if (!on) { n.refresher.style.opacity = ""; n.refresher.style.transform = ""; }
}

// --- List ---

export const listKind = {
  create(h, n) {
    const el = own(n, document.createElement("div"));
    el.className = "asuka-scroller asuka-vertical asuka-list";
    el.tabIndex = -1;
    n.list = new List(h, n);
    wireScroll(h, n, el);
  },
  prop(h, n, p, v) { n.list.prop(p, v); },
  frame(h, n, x, y, w, hh) {
    h.place(n, x, y, w, hh);
    n.list.resized();
    return true;
  },
  background(h, n) {
    n.el.style.backgroundColor = h.css(n.background);
    return true;
  },
  command(h, n, cmd, args) {
    const l = n.list;
    switch (cmd) {
      case Command.ListUpdate: l.update(args); return true;
      case Command.ScrollTo: {
        const r = new Reader(args);
        l.scrollToItem(r.u32(), r.u8() !== 0);
        return true;
      }
      case Command.ScrollToOffset: {
        const r = new Reader(args);
        r.f32();
        const y = r.f32(), animated = r.u8() !== 0;
        h.afterCore(() => { l.flush(); l.scrollTo(y, animated); });
        return true;
      }
    }
    return false;
  },
  destroy(h, n) {
    n.list.dispose();
    return false;
  },
  measure() { return [0, 0]; },
};

class List {
  constructor(h, n) {
    this.h = h;
    this.n = n;
    this.ids = [];
    this.sections = [];
    this.rows = []; // { header, section, first, count }
    this.tops = [];
    this.heights = new Map(); // by item ID, or a header's by its section's ID
    this.shown = new Map(); // row index -> row view
    this.free = [];
    this.columns = 1;
    this.gap = 0;
    this.appearance = 0;
    this.inset = 16;
    this.bottom = false;
    this.stuck = true;
    this.lead = 0;
    this.selecting = false;
    this.est = 44;
    this.width = 0;
    this.pending = null;
    this.queued = false;
    this.tiling = false;
    this.rebind = false;
    this.spacer = document.createElement("div");
    this.spacer.className = "asuka-list-spacer";
    n.el.appendChild(this.spacer);
  }

  prop(p, v) {
    switch (p) {
      case P.List.Columns: this.columns = Math.max(v, 1); break;
      case P.List.Gap: this.gap = v; break;
      case P.List.Appearance:
        this.appearance = v;
        this.n.el.dataset.appearance = ["plain", "grouped", "bare"][v] || "plain";
        break;
      case P.List.Inset: this.inset = v; break;
      case P.List.Anchor: this.bottom = v === 1; this.stuck = true; break;
      case P.List.Selecting:
        this.selecting = !!v;
        this.n.el.classList.toggle("asuka-selecting", this.selecting);
        // The selection control's room, at the rows' leading edge.
        this.h.afterCore(() => this.h.dispatch(Events.rowInset(this.n.id, this.selecting ? 40 : 0, 0)));
        for (const rv of this.shown.values()) this.paintRow(rv);
        return;
      case P.List.Refreshable: refreshable(this.h, this.n, !!v); return;
      case P.List.Refreshing: refreshing(this.n, !!v); return;
    }
    this.rebind = true;
    this.queue();
  }

  /** update takes the core's item IDs and sections; rows bind again once the core has returned. */
  update(args) {
    const r = new Reader(args);
    const count = r.u32();
    const ids = new Array(count);
    for (let i = 0; i < count; i++) ids[i] = r.i64();
    const secs = [];
    if (!r.atEnd) {
      const ns = r.u32();
      for (let i = 0; i < ns; i++) secs.push({ start: r.u32(), id: r.i64() });
    }
    this.pending = { ids, secs };
    this.rebind = true;
    this.queue();
  }

  queue() {
    if (this.queued) return;
    this.queued = true;
    this.h.afterCore(() => {
      this.queued = false;
      if (!this.n.dying) this.flush();
    });
  }

  get offset() { return this.n.el.scrollTop; }

  viewport() { return this.n.el.clientHeight || this.n.frame[3]; }

  flush() {
    if (!this.pending) { this.tile(); return; }
    const anchor = this.anchorRow();
    let keep = anchor;
    if ((this.bottom && this.stuck) || (!this.bottom && this.offset <= 0)) keep = null;
    const { ids, secs } = this.pending;
    this.pending = null;
    this.ids = ids;
    this.sections = secs;
    const live = new Map();
    for (const id of ids) if (this.heights.has(id)) live.set(id, this.heights.get(id));
    for (const s of secs) if (this.heights.has(s.id)) live.set(s.id, this.heights.get(s.id));
    this.heights = live;
    this.makeRows();
    this.tileKeeping(keep);
  }

  makeRows() {
    const rows = [];
    const add = (sec, from, to) => {
      for (let i = from; i < to; i += this.columns) rows.push({ header: false, section: sec, first: i, count: Math.min(this.columns, to - i) });
    };
    if (!this.sections.length) add(0, 0, this.ids.length);
    else {
      this.sections.forEach((s, k) => {
        const end = k + 1 < this.sections.length ? this.sections[k + 1].start : this.ids.length;
        rows.push({ header: true, section: k, first: 0, count: 1 });
        add(k, Math.min(s.start, this.ids.length), Math.min(Math.max(end, s.start), this.ids.length));
      });
    }
    this.rows = rows;
  }

  grouped() { return this.appearance === 1 && this.columns <= 1; }
  sticky() { return this.sections.length > 0 && this.appearance !== 1; }
  pad() { return this.grouped() ? this.inset : 0; }

  rowKey(r) {
    const row = this.rows[r];
    return row.header ? this.sections[row.section].id : this.ids[row.first];
  }

  rowHeight(r) {
    const row = this.rows[r];
    let height = 0, known = false;
    if (row.header) {
      const v = this.heights.get(this.sections[row.section].id);
      if (v != null) { height = v; known = true; }
    } else {
      for (let i = 0; i < row.count; i++) {
        const v = this.heights.get(this.ids[row.first + i]);
        if (v != null) { height = Math.max(height, v); known = true; }
      }
    }
    if (!known) height = this.est;
    const next = this.rows[r + 1];
    if (!row.header && next && !next.header) {
      if (this.columns > 1) height += this.gap;
      if (this.grouped()) height += cardGap;
    }
    return height;
  }

  measureTops() {
    const tops = this.tops;
    tops.length = 0;
    let y = this.pad();
    for (let r = 0; r < this.rows.length; r++) {
      tops.push(y);
      y += this.rowHeight(r);
    }
    tops.push(y + this.pad());
    this.lead = 0;
    if (this.bottom) {
      const view = this.viewport(), total = tops[tops.length - 1];
      if (total < view) {
        this.lead = view - total;
        for (let i = 0; i < tops.length; i++) tops[i] += this.lead;
      }
    }
  }

  search(f) {
    let lo = 0, hi = this.rows.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      if (f(m)) hi = m; else lo = m + 1;
    }
    return lo;
  }

  /** anchorRow is the row at the top of what shows, and how far below the top its top is. */
  anchorRow() {
    if (!this.rows.length || this.tops.length !== this.rows.length + 1) return null;
    const off = this.offset;
    const r = this.search((r) => this.tops[r + 1] > off);
    if (r >= this.rows.length) return null;
    return { key: this.rowKey(r), dy: this.tops[r] - off };
  }

  atEnd() {
    const t = this.tops;
    return !t.length || this.offset + this.viewport() >= t[t.length - 1] - 1;
  }

  pinnedRow() {
    if (!this.sticky() || !this.rows.length) return -1;
    const off = this.offset;
    const r = this.search((r) => this.tops[r + 1] > off);
    if (r >= this.rows.length) return -1;
    const s = this.rows[r].section;
    for (let k = r; k >= 0; k--) {
      if (this.rows[k].header && this.rows[k].section === s) return this.tops[k] >= off ? -1 : k;
    }
    return -1;
  }

  scrolled() {
    if (this.tiling || this.setting) return;
    if (this.bottom) this.stuck = this.atEnd();
    if (this.tickQueued) return;
    this.tickQueued = true;
    requestAnimationFrame(() => {
      this.tickQueued = false;
      if (!this.n.dying) this.h.afterCore(() => this.tile());
    });
  }

  tile() { this.tileKeeping(this.anchorRow()); }

  /**
   * tileKeeping shows the rows in view (and half a screen around them),
   * binds those new to the view, and places them, keeping the row at the
   * top where it was as rows above it change height (estimates made
   * real); a list anchored at its bottom and at its end stays there.
   */
  tileKeeping(keep) {
    const h = this.h;
    if (this.tiling || !h.core || h.inGo) return;
    this.tiling = true;
    try {
      for (let pass = 0; pass < 4; pass++) {
        this.measureTops();
        const total = this.tops[this.tops.length - 1];
        const view = this.viewport();
        this.spacer.style.height = total + "px";
        let off = this.offset;
        if (this.bottom && this.stuck) off = total - view;
        else if (keep) {
          for (let r = 0; r < this.rows.length; r++) {
            if (this.rowKey(r) === keep.key) { off = this.tops[r] - keep.dy; break; }
          }
        }
        off = Math.min(Math.max(off, 0), Math.max(total - view, 0));
        if (Math.abs(off - this.offset) > 0.5) {
          this.setting = true;
          this.n.el.scrollTop = off;
          this.setting = false;
        }
        const top = off - view / 2, bottom = off + view * 1.5;
        const first = this.search((r) => this.tops[r + 1] > top);
        const last = this.search((r) => this.tops[r] >= bottom);
        const pin = this.pinnedRow();
        for (const [r, rv] of this.shown) {
          if ((r < first || r >= last) && r !== pin) {
            this.shown.delete(r);
            rv.el.hidden = true;
            this.free.push(rv);
          }
        }
        let changed = false;
        const show = (r) => {
          let rv = this.shown.get(r);
          const fresh = !rv;
          if (fresh) { rv = this.take(); this.shown.set(r, rv); }
          if (fresh || this.rebind || rv.row !== r) {
            rv.row = r;
            rv.el.hidden = false; // laid out as it binds: what's in it is measured
            const was = this.rowHeight(r);
            this.bind(rv, r);
            if (this.rowHeight(r) !== was) changed = true;
          }
        };
        for (let r = first; r < last; r++) show(r);
        if (pin >= 0 && (pin < first || pin >= last)) show(pin);
        this.rebind = false;
        this.placeRows();
        if (!changed) break;
      }
    } finally {
      this.tiling = false;
    }
  }

  placeRows() {
    const off = this.offset;
    const [x, w] = this.rowX();
    for (const [r, rv] of this.shown) {
      let y = this.tops[r];
      const height = this.rowHeight(r);
      rv.pinned = false;
      if (this.rows[r].header && this.sticky() && y < off) {
        y = off;
        for (let k = r + 1; k < this.rows.length; k++) {
          if (this.rows[k].header) { y = Math.min(y, this.tops[k] - height); break; }
        }
        rv.pinned = true;
      }
      const s = rv.el.style;
      s.transform = `translateY(${y}px)`;
      s.left = x + "px";
      s.width = w + "px";
      s.height = height + "px";
      rv.el.hidden = false;
      rv.el.classList.toggle("asuka-pinned", rv.pinned);
      this.placeCells(rv, r);
      this.paintRow(rv);
    }
  }

  rowX() {
    return this.grouped() ? [this.inset, Math.max(this.width - 2 * this.inset, 0)] : [0, this.width];
  }

  take() {
    const rv = this.free.pop();
    if (rv) return rv;
    const el = document.createElement("div");
    el.className = "asuka-row";
    el.setAttribute("role", "listitem");
    this.n.el.appendChild(el);
    const row = { el, cells: [], row: -1, pinned: false };
    el.addEventListener("click", (e) => this.rowClicked(row, e));
    el.addEventListener("contextmenu", (e) => this.rowMenu(row, e));
    return row;
  }

  bind(rv, r) {
    const row = this.rows[r];
    const k = row.header ? 1 : row.count;
    while (rv.cells.length < k) rv.cells.push(this.newCell(rv));
    rv.cells.forEach((c, i) => { c.n.el.hidden = i >= k; });
    rv.header = row.header;
    rv.el.classList.toggle("asuka-header", row.header);
    rv.sep = this.columns <= 1 && this.appearance !== 2 && !row.header && r < this.rows.length - 1 && !this.rows[r + 1].header;
    rv.first = !row.header && (r === 0 || this.rows[r - 1].header);
    rv.last = !row.header && (r === this.rows.length - 1 || this.rows[r + 1].header);
    for (let i = 0; i < k; i++) {
      const c = rv.cells[i];
      c.header = row.header;
      let index = row.first + i;
      if (row.header) { c.item = this.sections[row.section].id; index = -1 - row.section; }
      else c.item = this.ids[index];
      const known = this.heights.get(c.item);
      if (known != null) c.height = known;
      this.h.listBind(this.n.id, index, c.n.id);
    }
  }

  newCell(rv) {
    const l = this;
    const n = this.h.hostNode(KindCell, cellImpl);
    const c = { n, l, rv, item: 0, height: 0, header: false, selected: false, swipe: [], leading: [] };
    n.cell = {
      prop(h, node, p, v) {
        if (p === Prop.SwipeActions || p === Prop.LeadingSwipeActions) {
          const a = decodeActions(v);
          if (p === Prop.SwipeActions) c.swipe = a; else c.leading = a;
        } else {
          c.selected = !!v;
          l.paintRow(rv);
        }
      },
    };
    n.listCell = c;
    rv.el.appendChild(n.el);
    return c;
  }

  /** setHeight takes a row's height from its cell's frame. */
  setHeight(c, height) {
    c.height = height;
    const old = this.heights.get(c.item);
    if (old === height) return;
    if (old == null && this.heights.size < 64 && !c.header) {
      this.est = (this.est * this.heights.size + height) / (this.heights.size + 1);
    }
    this.heights.set(c.item, height);
    if (!this.tiling) this.queue();
  }

  placeCells(rv, r) {
    if (this.columns <= 1 || this.rows[r].header) {
      const [, w] = this.rowX();
      for (const c of rv.cells) {
        const s = c.n.el.style;
        s.left = "0px";
        s.top = "0px";
        s.width = w + "px";
        s.height = c.height + "px";
      }
      return;
    }
    const cw = (this.width - this.gap * (this.columns - 1)) / this.columns;
    const rtl = getComputedStyle(this.n.el).direction === "rtl";
    rv.cells.forEach((c, i) => {
      let x = i * (cw + this.gap);
      if (rtl) x = this.width - x - cw;
      const s = c.n.el.style;
      s.left = x + "px";
      s.top = "0px";
      s.width = cw + "px";
      s.height = c.height + "px";
    });
  }

  paintRow(rv) {
    const el = rv.el;
    el.classList.toggle("asuka-sep", !!rv.sep);
    el.classList.toggle("asuka-first", !!rv.first);
    el.classList.toggle("asuka-last", !!rv.last);
    const selected = this.selecting && rv.cells.some((c) => c.selected && !c.header && !c.n.el.hidden);
    el.classList.toggle("asuka-selected", selected);
    el.setAttribute("aria-selected", this.selecting ? String(selected) : "false");
  }

  rowClicked(rv, e) {
    if (!this.selecting || rv.header) return;
    const c = rv.cells.find((c) => c.n.el.contains(e.target)) || rv.cells[0];
    if (!c) return;
    e.stopPropagation();
    this.h.dispatch(Events.rowSelected(c.n.id, !c.selected));
  }

  /** rowMenu: a row's swipe actions are its context menu on the web, as on the desktop hosts. */
  rowMenu(rv, e) {
    const c = rv.cells.find((c) => c.n.el.contains(e.target));
    if (!c || (!c.swipe.length && !c.leading.length)) return;
    if (e.defaultPrevented) return;
    e.preventDefault();
    const items = [...c.leading.map((a, i) => ({ ...a, pick: () => this.h.dispatch(Events.action(c.n.id, -1 - i)) })),
      ...c.swipe.map((a, i) => ({ ...a, pick: () => this.h.dispatch(Events.action(c.n.id, i)) }))];
    this.h.menus.show(items, e.clientX, e.clientY);
  }

  resized() {
    const w = this.n.frame[2];
    if (w !== this.width) {
      this.width = w;
      this.rebind = true;
    }
    this.queue();
  }

  scrollTo(y, animated) {
    if (!this.tops.length) return;
    const total = this.tops[this.tops.length - 1];
    y = Math.min(Math.max(y, 0), Math.max(total - this.viewport(), 0));
    if (this.bottom) this.stuck = y >= Math.max(total - this.viewport(), 0) - 1;
    this.n.el.scrollTo({ top: y, behavior: animated ? "smooth" : "instant" });
    this.tile();
  }

  scrollToItem(index, animated) {
    this.h.afterCore(() => {
      this.flush();
      for (let r = 0; r < this.rows.length; r++) {
        const row = this.rows[r];
        if (!row.header && index >= row.first && index < row.first + row.count) {
          const view = this.viewport();
          const top = this.tops[r], bottom = this.tops[r + 1];
          let y = this.offset;
          if (top < y) y = top;
          else if (bottom > y + view) y = bottom - view;
          this.scrollTo(y, animated);
          return;
        }
      }
    });
  }

  dispose() {
    for (const rv of [...this.shown.values(), ...this.free]) {
      for (const c of rv.cells) this.h.nodes.delete(c.n.id);
    }
  }
}

const cellImpl = {
  create(h, n) {
    const el = document.createElement("div");
    el.className = "asuka-cell";
    n.el = el;
  },
  frame(h, n, x, y, w, hh) {
    n.frame = [x, y, w, hh];
    n.placed = true;
    n.listCell.l.setHeight(n.listCell, hh);
    n.el.style.height = hh + "px";
    return true;
  },
  measure() { return [0, 0]; },
};

// A SplitView's sidebar as the core sends it (ops/sidebar.go): Split's
// Items. It's u32 section count, per section a string title (empty: none)
// and u32 item count, per item a string title, icon and badge (empty:
// none).

import { Reader } from "./ops.js";

/** decodeSidebar reads the sections: [{ title, items: [{ title, icon, badge }] }]; a cut one ends where it's cut, items whole. */
export function decodeSidebar(b) {
  if (!b || b.length < 4) return [];
  const r = new Reader(b);
  const fits = () => r.p <= r.b.length;
  // A string that runs past the end is cut: null.
  const str = () => {
    if (r.p + 4 > r.b.length || r.p + 4 + r.v.getUint32(r.p, true) > r.b.length) { r.p = r.b.length; return null; }
    return r.string();
  };
  const u32 = () => (r.p + 4 <= r.b.length ? r.u32() : null);
  const out = [];
  const count = u32();
  for (let s = 0; count !== null && s < count && fits(); s++) {
    const title = str();
    const n = title === null ? null : u32();
    if (n === null) break;
    const items = [];
    let cut = false;
    for (let i = 0; i < n; i++) {
      const t = str(), icon = t === null ? null : str(), badge = icon === null ? null : str();
      if (badge === null) { cut = true; break; }
      items.push({ title: t, icon, badge });
    }
    out.push({ title, items });
    if (cut) break;
  }
  return out;
}

/** sameShape reports whether a and b have as many sections, each as many items (ops.SameSidebarShape): the rows are updated in place. */
export function sameShape(a, b) {
  return a.length === b.length && a.every((s, i) => s.items.length === b[i].items.length);
}

/** badgeNumber is the count a badge says, if it's only ASCII digits (at most nine), as ops.BadgeNumber reads it; null for other text. */
export function badgeNumber(s) {
  return /^[0-9]{1,9}$/.test(s) ? Number(s) : null;
}

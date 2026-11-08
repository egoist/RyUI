// Colors: RGBA, or semantic colors the stylesheet defines for light and
// dark (style.css's --ryui-* variables), so dark mode needs no app code.

const names = [
  "", "label", "secondary-label", "tertiary-label", "background", "secondary-background",
  "grouped-background", "separator", "accent", "red", "orange", "yellow", "green", "blue",
  "purple", "pink", "gray", "clear", "secondary-grouped-background", "on-accent",
];

/** css is c's CSS value: "" for the platform's default (0). */
export function css(h, c) {
  if (!c) return "";
  const kind = Math.floor(c / 4294967296);
  const lo = c % 4294967296;
  if (kind === 2) {
    const r = (lo >>> 24) & 255, g = (lo >>> 16) & 255, b = (lo >>> 8) & 255, a = lo & 255;
    return a === 255 ? `rgb(${r} ${g} ${b})` : `rgb(${r} ${g} ${b} / ${(a / 255).toFixed(3)})`;
  }
  if (kind === 1) {
    const name = names[lo & 0xffff];
    if (!name) return "";
    const alpha = 255 - ((lo >>> 24) & 255);
    const v = `var(--ryui-${name})`;
    return alpha === 255 ? v : `color-mix(in srgb, ${v} ${(alpha / 2.55).toFixed(2)}%, transparent)`;
  }
  return "";
}

/** resolve is c as a color a canvas takes: its variables resolved. */
export function resolve(h, c, fallback = "") {
  const v = css(h, c);
  if (!v) return fallback;
  if (!v.includes("var(")) return v;
  if (!h.colorProbe) {
    h.colorProbe = document.createElement("div");
    h.colorProbe.className = "ryui-color-probe";
    h.container.appendChild(h.colorProbe);
  }
  h.colorProbe.style.color = v;
  return getComputedStyle(h.colorProbe).color;
}

/** setAccent makes the app's accent the controls' (0: the platform's). */
export function setAccent(h, c) {
  const s = h.container.style;
  const v = css(h, c);
  if (v && Math.floor(c / 4294967296) === 2) {
    s.setProperty("--ryui-accent", v);
    s.setProperty("--ryui-accent-dark", v);
  } else {
    s.removeProperty("--ryui-accent");
    s.removeProperty("--ryui-accent-dark");
  }
}

export function isDark(h) {
  if (h.scheme === 1) return false;
  if (h.scheme === 2) return true;
  return matchMedia("(prefers-color-scheme: dark)").matches;
}

/** applyScheme shows light or dark (ColorScheme), or the system's. */
export function applyScheme(h) {
  const c = h.container;
  if (h.scheme === 1) c.dataset.scheme = "light";
  else if (h.scheme === 2) c.dataset.scheme = "dark";
  else delete c.dataset.scheme;
  c.classList.toggle("ryui-dark", isDark(h));
  h.schemeChanged?.();
}

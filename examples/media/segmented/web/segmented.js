// The segmented control on the web: radio buttons in a row.

export function makeSegmented(events) {
  const el = document.createElement("div");
  el.setAttribute("role", "radiogroup");
  el.style.cssText = "display:flex;gap:2px;padding:2px;border-radius:9px;background:var(--asuka-fill)";
  let items = [], selected = 0;
  const render = () => {
    el.textContent = "";
    items.forEach((t, i) => {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = t;
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(i === selected));
      b.style.cssText = `flex:1;border:0;border-radius:7px;padding:5px 12px;font:inherit;color:var(--asuka-label);cursor:pointer;background:${i === selected ? "var(--asuka-card)" : "none"}`;
      b.onclick = () => { selected = i; render(); events.changed(i); };
      el.appendChild(b);
    });
  };
  return {
    el,
    setItems(v) { items = v; render(); },
    setSelected(v) { selected = v; render(); },
    measure: () => [items.length * 72, 32],
  };
}

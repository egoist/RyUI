// The app's native views for ui.Native, in the browser. The RyUI CLI loads
// this file with the page, before the app starts, as it compiles
// ios/Natives.swift with the iOS host.

// Segmented: radio buttons in a row; props "items" (comma-separated) and
// "selected"; event "changed" with "index".
ryui.registerNative("Segmented", (ctx) => {
  const el = document.createElement("div");
  el.setAttribute("role", "radiogroup");
  el.style.cssText = "display:flex;gap:2px;padding:2px;border-radius:9px;background:var(--ryui-fill)";
  let items = [], selected = 0;
  const render = () => {
    el.textContent = "";
    items.forEach((t, i) => {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = t;
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(i === selected));
      b.style.cssText = `flex:1;border:0;border-radius:7px;padding:5px 12px;font:inherit;color:var(--ryui-label);cursor:pointer;background:${i === selected ? "var(--ryui-card)" : "none"}`;
      b.onclick = () => { selected = i; render(); ctx.emit("changed", { index: i }); };
      el.appendChild(b);
    });
  };
  return {
    el,
    set(name, value) {
      if (name === "items") items = String(value).split(",");
      if (name === "selected") selected = Number(value);
      render();
    },
    measure() {
      return [Math.max(items.length * 80, 160), 36];
    },
  };
});

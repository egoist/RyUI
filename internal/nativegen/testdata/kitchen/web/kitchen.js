// The kitchen package's JavaScript on the web: what its Go declares,
// implemented (platform/web's test and the GUI tests' plugin app call it).
import { KitchenError, Unit, Level } from "./kitchen.asuka.js";

export function makeKitchen() {
  return {
    name: () => "home",
    items: (filter) => [{
      name: filter, weight: 1.5, unit: Unit.gram, tags: ["a", "b"], notes: { k: "v" },
      best: new Date(1_700_000_000_250), next: { name: "inner" }, data: new Uint8Array([1, 2, 3]),
      took: 2500, id: 18446744073709551615n, count: -3, ratio: 0.5, fresh: true,
    }],
    add(item) {
      if (!item.name) throw new KitchenError("busy");
    },
    async cook(signal, recipe, progress) {
      if (recipe === "slow") {
        await new Promise((resolve, reject) => {
          if (signal.aborted) return reject(signal.reason);
          const t = setTimeout(resolve, 10_000);
          signal.addEventListener("abort", () => { clearTimeout(t); reject(signal.reason); });
        });
      }
      progress(0.5);
      progress(1);
      return { name: recipe + " done" };
    },
    ask: (question) => question("ok?"),
    open(name) {
      if (name === "none") throw new RangeError("no oven");
      return makeOven();
    },
    show() {},
    level: () => Level.high,
    counts: () => ({ a: 1n, b: -2n }),
    when: (at, every) => (every === 0 ? null : new Date(at.getTime() + every)),
  };
}

export function makeOven() {
  return {
    close() {},
    heat(to, unit) {
      if (unit === Unit.ounce) throw new (globalThis.asuka.native.UnsupportedError)("no ounces");
    },
  };
}

export function makeDial(events) {
  const el = document.createElement("input");
  el.type = "range";
  return {
    el,
    setValue(v) {
      el.value = String(v * 100);
      events.changed(v * 2);
    },
    setLabel(v) { el.setAttribute("aria-label", v); },
    setItems(items) {
      if (items.length) events.picked(items[items.length - 1], items.length - 1);
    },
  };
}

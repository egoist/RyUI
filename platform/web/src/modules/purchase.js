// asuka/sdk/purchase on the web: Google Play, through the Digital Goods API
// (its details, the user's purchases, consuming) and the Payment Request
// API (a purchase), as a Trusted Web Activity that Play installs has them
// (Chrome 101, Android and ChromeOS). Elsewhere, a browser has no store:
// every method but "available" and "stop" fails "not available".
// internal/purchasespec is the wire.

const dec = new TextDecoder();
const PLAY = "https://play.google.com/billing";
const noStore = () => new Error("not available: a browser has no store (Google Play's Digital Goods API is a Trusted Web Activity's)");

const args = (b) => (b && b.length ? JSON.parse(dec.decode(b)) : {});

// micros is a price's value ("4.99") in millionths.
const micros = (v) => Math.round(Number(v || 0) * 1e6);

// period is an ISO 8601 duration ("P1M", "P7D") as the wire's.
function period(iso) {
  const m = /^P(\d+)([DWMY])$/.exec(iso || "");
  if (!m) return null;
  return { unit: { D: "day", W: "week", M: "month", Y: "year" }[m[2]], count: Number(m[1]) };
}

function format(price) {
  if (!price) return "";
  try {
    return new Intl.NumberFormat(undefined, { style: "currency", currency: price.currency }).format(Number(price.value));
  } catch {
    return `${price.value} ${price.currency}`;
  }
}

export function modules(h) {
  let svc = null;
  const kinds = new Map(); // product ID → the app's kind
  const given = new Set(); // purchase tokens given through "next"
  const events = [];
  let waiting = null;
  let stops = 0; // "stop"s: one that comes while a "next" starts stops it

  async function service() {
    if (typeof window === "undefined" || !("getDigitalGoodsService" in window)) throw noStore();
    try {
      svc ??= await window.getDigitalGoodsService(PLAY);
    } catch {
      throw noStore();
    }
    if (!svc) throw noStore();
    return svc;
  }

  const kindOf = (id, type) => kinds.get(id) ?? (type === "subscription" ? "subs" : "inapp");

  const tx = (p, state = "purchased") => ({
    id: p.purchaseToken, productId: p.itemId, kind: kindOf(p.itemId), quantity: 1, state,
    token: p.purchaseToken, acknowledged: !!p.acknowledged,
  });

  function give(e) {
    if (waiting) {
      const w = waiting;
      waiting = null;
      w.resolve(JSON.stringify(e));
    } else events.push(e);
  }

  // The purchases not acknowledged (left from before, paid outside the
  // page): given once, as the page starts and as it's shown again.
  async function reconcile() {
    let s;
    try {
      s = await service();
    } catch {
      return;
    }
    for (const p of await s.listPurchases().catch(() => [])) {
      if (p.acknowledged || given.has(p.purchaseToken)) continue;
      given.add(p.purchaseToken);
      give({ type: "transaction", transaction: tx(p) });
    }
  }
  if (typeof window !== "undefined" && "getDigitalGoodsService" in window) {
    reconcile();
    document.addEventListener("visibilitychange", () => document.visibilityState === "visible" && reconcile());
  }

  function product(d) {
    const kind = kinds.get(d.itemId) ?? (d.type === "subscription" ? "autoRenewable" : "nonConsumable");
    const p = { id: d.itemId, kind, title: d.title, description: d.description, price: format(d.price),
      micros: micros(d.price?.value), currency: d.price?.currency };
    if (d.type === "subscription") {
      p.group = d.itemId;
      p.period = period(d.subscriptionPeriod);
      const phases = [];
      if (d.freeTrialPeriod) {
        phases.push({ price: format({ value: 0, currency: d.price?.currency }), micros: 0, currency: d.price?.currency,
          period: period(d.freeTrialPeriod), cycles: 1, mode: "freeTrial" });
      }
      if (d.introductoryPrice) {
        phases.push({ price: format(d.introductoryPrice), micros: micros(d.introductoryPrice.value), currency: d.introductoryPrice.currency,
          period: period(d.introductoryPricePeriod), cycles: d.introductoryPriceCycles || 1, mode: "payAsYouGo" });
      }
      if (phases.length) {
        p.offers = [{ type: "introductory", phases }];
        p.introEligible = true;
      }
    }
    return p;
  }

  return {
    purchase: {
      available: async () => {
        try {
          await service();
          return new Uint8Array([typeof PaymentRequest === "function" ? 1 : 0]);
        } catch {
          return new Uint8Array([0]);
        }
      },
      products: async (b) => {
        const s = await service();
        const items = args(b).items || [];
        for (const it of items) if (it.kind) kinds.set(it.id, it.kind);
        const ids = items.map((it) => it.id);
        const got = await s.getDetails(ids);
        const found = new Set(got.map((d) => d.itemId));
        return JSON.stringify({ products: got.map(product), missing: ids.filter((id) => !found.has(id)) });
      },
      buy: async (b) => {
        await service();
        const a = args(b);
        if (a.kind) kinds.set(a.id, a.kind);
        const req = new PaymentRequest([{ supportedMethods: PLAY, data: { sku: a.id } }],
          { total: { label: "Total", amount: { currency: "USD", value: "0" } } });
        let res;
        try {
          res = await req.show();
        } catch (e) {
          if (e?.name === "AbortError") return JSON.stringify({ status: "cancelled" });
          throw e;
        }
        const token = res.details?.purchaseToken || res.details?.token;
        await res.complete("success");
        if (!token) return JSON.stringify({ status: "cancelled" });
        given.add(token);
        return JSON.stringify({ status: "purchased", transaction: tx({ itemId: a.id, purchaseToken: token }) });
      },
      finish: async (b) => {
        const s = await service();
        const a = args(b);
        if (a.consume) await s.consume(a.token);
        // The API's first version acknowledged here; the second leaves it
        // to the app's server (the Play Developer API), within 3 days.
        else if (typeof s.acknowledge === "function") await s.acknowledge(a.token, a.kind === "autoRenewable" ? "repeatable" : "onetime");
      },
      entitlements: async () => JSON.stringify((await (await service()).listPurchases()).map((p) => tx(p))),
      restore: async () => {
        await reconcile();
        return JSON.stringify((await (await service()).listPurchases()).map((p) => tx(p)));
      },
      status: async (b) => {
        const g = args(b).group;
        const ps = (await (await service()).listPurchases()).filter((p) => p.itemId === g);
        return JSON.stringify(ps.map((p) => ({ productId: g, state: "subscribed", willRenew: true, transaction: tx(p) })));
      },
      manage: async (b) => {
        await service();
        const id = args(b).productId;
        window.open("https://play.google.com/store/account/subscriptions" + (id ? `?sku=${encodeURIComponent(id)}` : ""), "_blank");
      },
      refund: () => { throw new Error("not available: Play's refunds are the developer's (Play Console, the Developer API)"); },
      redeem: async () => {
        await service();
        window.open("https://play.google.com/redeem", "_blank");
      },
      storefront: async () => {
        await service();
        const region = new Intl.Locale(navigator.language).maximize().region || "";
        return JSON.stringify({ country: region });
      },
      next: async () => {
        const at = stops;
        await service();
        if (at !== stops) throw new Error("stopped");
        if (events.length) return JSON.stringify(events.shift());
        waiting?.reject(new Error("stopped"));
        return new Promise((resolve, reject) => { waiting = { resolve, reject }; });
      },
      stop: () => {
        stops++;
        waiting?.reject(new Error("stopped"));
        waiting = null;
      },
    },
  };
}

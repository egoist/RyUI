// asuka/sdk/purchase's web module: Google Play through the Digital Goods
// API and Payment Request (a Trusted Web Activity's), else no store:
// node --test platform/web.
import { test } from "node:test";
import assert from "node:assert/strict";
import { modules } from "../src/modules/purchase.js";

const enc = (o) => new TextEncoder().encode(JSON.stringify(o));

test("without the Digital Goods API: no store", async () => {
  const { purchase } = modules({});
  assert.deepEqual([...(await purchase.available())], [0]);
  await assert.rejects(purchase.products(enc({ items: [{ id: "pro", kind: "nonConsumable" }] })), /^Error: not available/);
  await assert.rejects(purchase.next(), /^Error: not available/);
  purchase.stop();
});

test("with Play's Digital Goods service: products, a purchase, finishing, entitlements, kept purchases", async () => {
  const consumed = [], acked = [];
  let owned = [{ itemId: "gift", purchaseToken: "t-gift", acknowledged: false }];
  const svc = {
    getDetails: async (ids) => [
      { itemId: "coins", title: "Coins", description: "100", price: { currency: "USD", value: "0.99" }, type: "product" },
      { itemId: "plus", title: "Plus", description: "", price: { currency: "USD", value: "4.99" }, type: "subscription",
        subscriptionPeriod: "P1M", freeTrialPeriod: "P1W" },
    ].filter((d) => ids.includes(d.itemId)),
    listPurchases: async () => owned,
    consume: async (t) => consumed.push(t),
    acknowledge: async (t, kind) => acked.push([t, kind]),
  };
  const listeners = [];
  globalThis.window = { getDigitalGoodsService: async (m) => (m === "https://play.google.com/billing" ? svc : null), open: () => {} };
  globalThis.document = { visibilityState: "visible", addEventListener: (_, f) => listeners.push(f) };
  globalThis.PaymentRequest = class {
    constructor(methods) { this.sku = methods[0].data.sku; }
    async show() {
      if (this.sku === "nope") throw Object.assign(new Error("cancelled"), { name: "AbortError" });
      return { details: { purchaseToken: "t-" + this.sku }, complete: async () => {} };
    }
  };
  try {
    const { purchase } = modules({});
    assert.deepEqual([...(await purchase.available())], [1]);
    // The unacknowledged purchase from before is kept for "next".
    const kept = JSON.parse(await purchase.next());
    assert.equal(kept.type, "transaction");
    assert.equal(kept.transaction.productId, "gift");

    const ps = JSON.parse(await purchase.products(enc({ items: [{ id: "coins", kind: "consumable" }, { id: "plus", kind: "autoRenewable" }, { id: "x", kind: "nonConsumable" }] })));
    assert.deepEqual(ps.missing, ["x"]);
    assert.equal(ps.products[0].kind, "consumable");
    assert.equal(ps.products[0].micros, 990000);
    assert.deepEqual(ps.products[1].period, { unit: "month", count: 1 });
    assert.equal(ps.products[1].offers[0].phases[0].mode, "freeTrial");

    const r = JSON.parse(await purchase.buy(enc({ id: "coins", kind: "consumable" })));
    assert.equal(r.status, "purchased");
    assert.equal(r.transaction.token, "t-coins");
    assert.equal(r.transaction.kind, "consumable");
    assert.equal(JSON.parse(await purchase.buy(enc({ id: "nope", kind: "consumable" }))).status, "cancelled");

    await purchase.finish(enc({ id: "t-coins", token: "t-coins", consume: true }));
    await purchase.finish(enc({ id: "t-gift", token: "t-gift", kind: "nonConsumable" }));
    assert.deepEqual(consumed, ["t-coins"]);
    assert.deepEqual(acked, [["t-gift", "onetime"]]);

    owned = [{ itemId: "plus", purchaseToken: "t-plus", acknowledged: true }];
    const ents = JSON.parse(await purchase.entitlements());
    assert.equal(ents[0].productId, "plus");
    assert.equal(ents[0].kind, "autoRenewable");

    // A "next" waits; stop fails it with stopped.
    const waiting = purchase.next();
    purchase.stop();
    await assert.rejects(waiting, /stopped/);
    await assert.rejects(Promise.resolve().then(() => purchase.refund()), /^Error: not available/);
  } finally {
    delete globalThis.window;
    delete globalThis.document;
    delete globalThis.PaymentRequest;
  }
});

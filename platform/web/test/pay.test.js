// asuka/sdk/pay in the browser (modules/pay.js): Apple Pay's Payment
// Request from the wire, its events and errors; Google Pay's flow through
// its web API's callbacks.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  appleData, details, updateErrors, retryErrors, paymentFromResponse, networkName, lineItem,
  appleButtonType, googleButtonType, modules,
} from "../src/modules/pay.js";

const enc = new TextEncoder();
const bytes = (o) => enc.encode(typeof o === "string" ? o : JSON.stringify(o));

const apple = {
  merchant: "merchant.dev.asuka.tea", country: "US", currency: "USD",
  networks: ["visa", "mastercard", "amex", "nope"], capabilities: ["3DS", "credit"],
  items: [{ label: "Tea", amount: "40.00" }, { label: "Club", amount: "20.00", kind: "recurring", interval: "month", every: 1, startMs: 1_800_000_000_000 }, { label: "Tea Shop", amount: "60.00" }],
  shippingFields: ["name", "email", "address"], billingFields: ["address"],
  shippingMethods: [{ id: "ground", label: "Ground", amount: "0.00" }, { id: "express", label: "Express", amount: "9.00" }],
  coupons: true, coupon: "TEA10",
  recurring: { description: "Tea every month", managementURL: "https://tea.example/club", regular: { label: "Club", amount: "20.00", kind: "recurring", interval: "month", every: 1 } },
  events: { shippingContact: true, shippingMethod: true, coupon: true },
};

test("Apple Pay's method data: Apple Pay JS's names, the contact fields, coupons, the subscription", () => {
  const d = appleData(apple, 14);
  assert.equal(d.version, 14);
  assert.equal(d.merchantIdentifier, "merchant.dev.asuka.tea");
  assert.deepEqual(d.supportedNetworks, ["visa", "masterCard", "amex"]);
  assert.deepEqual(d.merchantCapabilities, ["supports3DS", "supportsCredit"]);
  assert.deepEqual(d.requiredShippingContactFields, ["name", "email", "postalAddress"]);
  assert.deepEqual(d.requiredBillingContactFields, ["postalAddress"]);
  assert.equal(d.supportsCouponCode, true);
  assert.equal(d.couponCode, "TEA10");
  assert.equal(d.recurringPaymentRequest.managementURL, "https://tea.example/club");
  assert.equal(d.recurringPaymentRequest.regularBilling.paymentTiming, "recurring");
  assert.equal(d.recurringPaymentRequest.regularBilling.recurringPaymentIntervalUnit, "month");
  assert.equal(lineItem(apple.items[1]).recurringPaymentStartDate, new Date(1_800_000_000_000).toISOString());
  assert.equal(networkName("masterCard"), "mastercard");
  assert.equal(networkName("chinaUnionPay"), "unionpay");
});

test("details: the items but a subscription's, the total last, the chosen option selected", () => {
  const d = details("USD", apple.items, apple.shippingMethods, "express");
  assert.deepEqual(d.total, { label: "Tea Shop", amount: { currency: "USD", value: "60.00" }, pending: false });
  assert.deepEqual(d.displayItems.map((i) => i.label), ["Tea"]);
  assert.deepEqual(d.shippingOptions.map((o) => [o.id, o.selected]), [["ground", false], ["express", true]]);
  // A method that's gone: the first.
  assert.equal(details("USD", apple.items, apple.shippingMethods, "gone").shippingOptions[0].selected, true);
});

test("errors: an address's by its part, the rest said", () => {
  assert.deepEqual(updateErrors([{ field: "shippingPostalCode", message: "ZIP?" }, { field: "unserviceable", message: "Not there" }]),
    { shippingAddressErrors: { postalCode: "ZIP?" }, error: "Not there" });
  assert.deepEqual(retryErrors([{ field: "shippingCity", message: "City?" }, { field: "shippingEmail", message: "Email?" }]),
    { shippingAddress: { city: "City?" }, payer: { email: "Email?" } });
  assert.deepEqual(retryErrors([{ field: "payment", message: "Declined" }]), { error: "Declined" });
});

test("a response: the token as JSON, the card, the contacts", () => {
  const p = paymentFromResponse({
    details: {
      token: { paymentData: { data: "x", version: "EC_v1" }, transactionIdentifier: "t1", paymentMethod: { displayName: "Visa 1234", network: "Visa", type: "credit" } },
      shippingContact: { givenName: "Ada", familyName: "Lovelace", emailAddress: "ada@example.com", addressLines: ["1 Infinite Loop"], locality: "Cupertino", postalCode: "95014", countryCode: "us" },
    },
    shippingOption: "express",
  });
  assert.equal(p.token, '{"data":"x","version":"EC_v1"}');
  assert.deepEqual(p.card, { name: "Visa 1234", network: "visa", type: "credit" });
  assert.equal(p.shipping.address.street, "1 Infinite Loop");
  assert.equal(p.shipping.address.countryCode, "US");
  assert.equal(p.shipping.email, "ada@example.com");
  assert.equal(p.shippingMethod, "express");
});

test("the buttons' types: Apple's CSS names, Google's", () => {
  assert.equal(appleButtonType(5), "check-out");
  assert.equal(appleButtonType(2), "set-up");
  assert.equal(appleButtonType(99), "plain");
  assert.equal(googleButtonType(3), "checkout");
  assert.equal(googleButtonType(8), "plain");
});

// next waits for the sheet's next event.
async function next(pay, id) {
  return JSON.parse(await pay.next(bytes(id)));
}

test("Google Pay: the callbacks are events, Go's answers their results, done at the end", async () => {
  let callbacks, request, env;
  globalThis.google = {
    payments: {
      api: {
        PaymentsClient: class {
          constructor(o) { env = o.environment; callbacks = o.paymentDataCallbacks; }
          isReadyToPay() { return Promise.resolve({ result: true, paymentMethodPresent: true }); }
          async loadPaymentData(r) {
            request = r;
            const u = await callbacks.onPaymentDataChanged({ callbackTrigger: "SHIPPING_ADDRESS", shippingAddress: { postalCode: "95014" } });
            assert.equal(u.newTransactionInfo.totalPrice, "43.20");
            const a = await callbacks.onPaymentAuthorized({ paymentMethodData: { tokenizationData: { token: "tok" } } });
            assert.equal(a.transactionState, "SUCCESS");
            return {};
          }
        },
      },
    },
  };
  try {
    const { pay } = modules();
    const google = { apiVersion: 2, callbackIntents: ["PAYMENT_AUTHORIZATION", "SHIPPING_ADDRESS"], allowedPaymentMethods: [] };
    assert.deepEqual(JSON.parse(await pay.available(bytes({ googleEnv: "TEST", googleReady: {} }))), { wallet: "google", canPay: true, hasCard: true });
    pay.present(bytes({ id: "s1", googleEnv: "TEST", google, apple: {} }));
    assert.throws(() => pay.present(bytes({ id: "s2", google })), /busy/);
    let e = await next(pay, "s1");
    assert.equal(e.type, "google");
    assert.equal(e.google.shippingAddress.postalCode, "95014");
    pay.answer(bytes({ id: "s1", seq: e.seq, google: { newTransactionInfo: { totalPrice: "43.20" } } }));
    e = await next(pay, "s1");
    assert.equal(e.type, "googleAuthorize");
    assert.equal(e.google.paymentMethodData.tokenizationData.token, "tok");
    pay.answer(bytes({ id: "s1", seq: e.seq, google: { transactionState: "SUCCESS" } }));
    e = await next(pay, "s1");
    assert.deepEqual(e, { type: "done" });
    assert.throws(() => pay.next(bytes("s1")), /stopped/);
    assert.equal(env, "TEST");
    assert.deepEqual(request, google);
  } finally {
    delete globalThis.google;
  }
});

test("Apple Pay: the merchant's validation, a shipping address, an authorization retried, then done", async () => {
  const listeners = {};
  let shown, retried = 0, completed = "";
  globalThis.ApplePaySession = { canMakePayments: () => true, supportsVersion: (v) => v <= 12 };
  globalThis.PaymentRequest = class {
    constructor(methods, d, options) {
      shown = { methods, details: d, options };
      this.shippingAddress = { country: "US", postalCode: "95014", city: "Cupertino", region: "CA", addressLine: [] };
    }
    addEventListener(name, f) { listeners[name] = f; }
    async show() {
      let session;
      listeners.merchantvalidation({ validationURL: "https://apple-pay-gateway.apple.com/x", complete: (p) => (session = p) });
      assert.deepEqual(await session, { merchantSessionIdentifier: "m" });
      let update;
      listeners.shippingaddresschange({ updateWith: (p) => (update = p) });
      update = await update;
      assert.equal(update.total.amount.value, "43.20");
      assert.deepEqual(update.shippingAddressErrors, { postalCode: "ZIP?" });
      return {
        details: { token: { paymentData: { data: "x" }, paymentMethod: { network: "Visa", type: "debit", displayName: "Visa 4242" } } },
        async retry(errs) { retried++; assert.equal(errs.error, "Declined"); },
        async complete(s) { completed = s; },
      };
    }
  };
  try {
    const { pay } = modules();
    assert.throws(() => pay.present(bytes({ id: "a1", apple, validate: false })), /OnValidateMerchant/);
    pay.present(bytes({ id: "a1", apple, validate: true }));
    assert.equal(shown.methods[0].supportedMethods, "https://apple.com/apple-pay");
    assert.equal(shown.methods[0].data.version, 12);
    assert.equal(shown.options.requestShipping, true);
    assert.equal(shown.options.requestPayerEmail, true);
    let e = await next(pay, "a1");
    assert.equal(e.type, "validate");
    pay.answer(bytes({ id: "a1", seq: e.seq, session: { merchantSessionIdentifier: "m" } }));
    e = await next(pay, "a1");
    assert.equal(e.type, "shippingContact");
    assert.equal(e.contact.address.postalCode, "95014");
    const items = [{ label: "Tea", amount: "40.00" }, { label: "Tax", amount: "3.20" }, { label: "Tea Shop", amount: "43.20" }];
    pay.answer(bytes({ id: "a1", seq: e.seq, items, errors: [{ field: "shippingPostalCode", message: "ZIP?" }] }));
    e = await next(pay, "a1");
    assert.equal(e.type, "authorize");
    assert.equal(e.payment.token, '{"data":"x"}');
    assert.equal(e.payment.card.network, "visa");
    pay.answer(bytes({ id: "a1", seq: e.seq, errors: [{ field: "payment", message: "Declined" }] }));
    e = await next(pay, "a1");
    assert.equal(e.type, "authorize");
    pay.answer(bytes({ id: "a1", seq: e.seq }));
    e = await next(pay, "a1");
    assert.deepEqual(e, { type: "done" });
    assert.equal(retried, 1);
    assert.equal(completed, "success");
  } finally {
    delete globalThis.ApplePaySession;
    delete globalThis.PaymentRequest;
  }
});

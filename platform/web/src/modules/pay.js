// asuka/sdk/pay in the browser (internal/payspec has the wire):
//
//   - Apple Pay where the browser has it (Safari, and the browsers of an
//     iPhone or iPad: ApplePaySession.canMakePayments), through the Payment
//     Request API's Apple Pay method: the request's data are Apple's (its
//     merchant, networks, capabilities, contact fields, coupons,
//     subscriptions), its details the items and shipping options. Its
//     events (merchantvalidation, shippingaddresschange,
//     shippingoptionchange, paymentmethodchange, couponcodechange) go to
//     the Go code, whose answers update the sheet; the response's retry
//     shows the authorization's errors by their fields. The merchant's
//     validation is the app's server's (pay.Request.OnValidateMerchant),
//     for a domain registered with the merchant ID.
//   - Google Pay elsewhere, with Google's own web API (pay.js, loaded once,
//     from pay.google.com): the same PaymentDataRequest and callbacks as
//     on Android, in every browser.
//   - The button, "asuka.payButton": Safari's own Apple Pay button (the
//     CSS -apple-pay-button), or the one Google's API draws (createButton),
//     each with its brand's words in the page's language.

import { isDark } from "../color.js";

const appleMethod = "https://apple.com/apple-pay";
const googleScript = "https://pay.google.com/gp/p/js/pay.js";

// Apple Pay JS's names for asuka/sdk/pay's networks, capabilities and fields.
const appleNetworks = {
  visa: "visa", mastercard: "masterCard", amex: "amex", discover: "discover", jcb: "jcb", interac: "interac",
  unionpay: "chinaUnionPay", maestro: "maestro", electron: "electron", elo: "elo", cartesBancaires: "cartesBancaires",
  eftpos: "eftpos", mada: "mada", mir: "mir", girocard: "girocard", bancontact: "bancontact", dankort: "dankort",
  privateLabel: "privateLabel",
};
const appleCapabilities = { "3DS": "supports3DS", EMV: "supportsEMV", credit: "supportsCredit", debit: "supportsDebit" };
const appleFields = { name: "name", email: "email", phone: "phone", address: "postalAddress", phoneticName: "phoneticName" };
const timings = { recurring: "recurring", deferred: "deferred", reload: "automaticReload" };

/** networkName is an Apple Pay JS network's asuka/sdk/pay name. */
export function networkName(n) {
  const s = String(n ?? "");
  for (const [k, v] of Object.entries(appleNetworks)) if (v.toLowerCase() === s.toLowerCase()) return k;
  return s.toLowerCase();
}

/** hasApplePay reports whether the browser pays with Apple Pay. */
export function hasApplePay() {
  try {
    return !!globalThis.ApplePaySession && globalThis.ApplePaySession.canMakePayments() && !!globalThis.PaymentRequest;
  } catch {
    return false;
  }
}

let googleLoad = null;

/** loadGoogle loads Google Pay's API once. */
export function loadGoogle() {
  if (globalThis.google?.payments?.api) return Promise.resolve(globalThis.google.payments.api);
  googleLoad ??= new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = googleScript;
    s.async = true;
    s.onload = () => (globalThis.google?.payments?.api ? resolve(globalThis.google.payments.api) : reject(new Error("not available: no Google Pay API")));
    s.onerror = () => {
      googleLoad = null;
      reject(new Error("not available: Google Pay's script (pay.google.com) didn't load"));
    };
    document.head.appendChild(s);
  });
  return googleLoad;
}

const isoDate = (ms) => (ms > 0 ? new Date(ms).toISOString() : undefined);

/** lineItem is a wire item as Apple Pay JS's ApplePayLineItem. */
export function lineItem(it) {
  const l = { label: it.label, amount: it.amount, type: it.pending ? "pending" : "final" };
  if (it.kind) l.paymentTiming = timings[it.kind];
  if (it.kind === "recurring") {
    l.recurringPaymentIntervalUnit = it.interval || "month";
    l.recurringPaymentIntervalCount = Math.max(it.every || 1, 1);
    if (it.startMs) l.recurringPaymentStartDate = isoDate(it.startMs);
    if (it.endMs) l.recurringPaymentEndDate = isoDate(it.endMs);
  }
  if (it.kind === "deferred" && it.dateMs) l.deferredPaymentDate = isoDate(it.dateMs);
  if (it.kind === "reload") l.automaticReloadPaymentThresholdAmount = it.threshold;
  return l;
}

/** appleVersion is the newest Apple Pay JS version the browser has (3 at least). */
function appleVersion() {
  const s = globalThis.ApplePaySession;
  for (let v = 14; v > 3; v--) {
    try {
      if (s?.supportsVersion?.(v)) return v;
    } catch {}
  }
  return 3;
}

/** appleData is the Payment Request's Apple Pay method data (ApplePayRequest). */
export function appleData(a, version = appleVersion()) {
  const d = {
    version,
    merchantIdentifier: a.merchant,
    merchantCapabilities: (a.capabilities ?? []).map((c) => appleCapabilities[c]).filter(Boolean),
    supportedNetworks: (a.networks ?? []).map((n) => appleNetworks[n]).filter(Boolean),
    countryCode: a.country,
  };
  if (a.cardCountries?.length) d.supportedCountries = a.cardCountries;
  const fields = (f) => (f ?? []).map((x) => appleFields[x]).filter(Boolean);
  if (a.shippingFields?.length) d.requiredShippingContactFields = fields(a.shippingFields);
  if (a.billingFields?.length) d.requiredBillingContactFields = fields(a.billingFields);
  if (a.coupons) d.supportsCouponCode = true;
  if (a.coupon) d.couponCode = a.coupon;
  if (a.shippingFixed) d.shippingContactEditingMode = "storePickup";
  if (a.applicationData) d.applicationData = a.applicationData;
  const extra = (x) => {
    const o = { paymentDescription: x.description, managementURL: x.managementURL };
    if (x.billingAgreement) o.billingAgreement = x.billingAgreement;
    if (x.tokenNotificationURL) o.tokenNotificationURL = x.tokenNotificationURL;
    return o;
  };
  if (a.recurring) {
    d.recurringPaymentRequest = { ...extra(a.recurring), regularBilling: lineItem(a.recurring.regular) };
    if (a.recurring.trial) d.recurringPaymentRequest.trialBilling = lineItem(a.recurring.trial);
  }
  if (a.reload) d.automaticReloadPaymentRequest = { ...extra(a.reload), automaticReloadBilling: lineItem(a.reload.billing) };
  if (a.deferred) {
    d.deferredPaymentRequest = { ...extra(a.deferred), deferredBilling: lineItem(a.deferred.billing) };
    if (a.deferred.freeCancellationMs) {
      d.deferredPaymentRequest.freeCancellationDate = isoDate(a.deferred.freeCancellationMs);
      d.deferredPaymentRequest.freeCancellationDateTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    }
  }
  return d;
}

/**
 * details are a Payment Request's details: the items (but a
 * subscription's or later payment's own, which the method data has) and
 * the total, the last; the shipping options, selected's chosen.
 */
export function details(currency, items, methods, selected) {
  const amount = (value) => ({ currency, value: String(value) });
  const total = items[items.length - 1] ?? { label: "", amount: "0" };
  const d = {
    total: { label: total.label, amount: amount(total.amount), pending: !!total.pending },
    displayItems: items.slice(0, -1).filter((i) => !i.kind).map((i) => ({ label: i.label, amount: amount(i.amount), pending: !!i.pending })),
  };
  if (methods?.length) {
    const chosen = methods.some((m) => m.id === selected) ? selected : methods[0].id;
    d.shippingOptions = methods.map((m) => ({ id: m.id, label: m.label, amount: amount(m.amount), selected: m.id === chosen }));
  }
  return d;
}

const addressErrorKeys = {
  shippingStreet: "addressLine", shippingCity: "city", shippingState: "region", shippingPostalCode: "postalCode",
  shippingCountry: "country", shippingName: "recipient", shippingPhone: "phone", shippingAddress: "addressLine",
};

/** updateErrors are the wire's errors in a PaymentDetailsUpdate (error, shippingAddressErrors). */
export function updateErrors(errors) {
  const out = {};
  const messages = [];
  for (const e of errors ?? []) {
    const k = addressErrorKeys[e.field];
    if (k) (out.shippingAddressErrors ??= {})[k] = e.message;
    else messages.push(e.message);
  }
  if (messages.length) out.error = messages.join("\n");
  return out;
}

/** retryErrors are the wire's errors as PaymentValidationErrors (a response's retry). */
export function retryErrors(errors) {
  const out = {};
  const messages = [];
  for (const e of errors ?? []) {
    const k = addressErrorKeys[e.field];
    if (k) (out.shippingAddress ??= {})[k] = e.message;
    else if (e.field === "shippingEmail") (out.payer ??= {}).email = e.message;
    else if (e.field === "billingName") (out.payer ??= {}).name = e.message;
    else messages.push(e.message);
  }
  if (messages.length) out.error = messages.join("\n");
  else if (!out.shippingAddress && !out.payer) out.error = "";
  return out;
}

/** contactFromAddress is a PaymentAddress as the wire's contact. */
export function contactFromAddress(a, payer = {}) {
  if (!a && !payer.name && !payer.email && !payer.phone) return undefined;
  const c = { name: payer.name || a?.recipient || "", email: payer.email || "", phone: payer.phone || a?.phone || "" };
  if (a) {
    c.address = {
      street: (a.addressLine ?? []).join("\n"), subLocality: a.dependentLocality || "", city: a.city || "",
      state: a.region || "", postalCode: a.postalCode || "", countryCode: a.country || "",
    };
  }
  return c;
}

/** contactFromApple is Apple Pay JS's ApplePayPaymentContact as the wire's. */
export function contactFromApple(c) {
  if (!c) return undefined;
  const out = {
    givenName: c.givenName || "", familyName: c.familyName || "", email: c.emailAddress || "", phone: c.phoneNumber || "",
  };
  if (c.addressLines || c.locality || c.postalCode || c.countryCode) {
    out.address = {
      street: (c.addressLines ?? []).join("\n"), subLocality: c.subLocality || "", city: c.locality || "",
      subAdministrativeArea: c.subAdministrativeArea || "", state: c.administrativeArea || "", postalCode: c.postalCode || "",
      country: c.country || "", countryCode: (c.countryCode || "").toUpperCase(),
    };
  }
  return out;
}

/** cardFromApple is an ApplePayPaymentMethod as the wire's card. */
export function cardFromApple(m) {
  if (!m) return {};
  const c = { name: m.displayName || "", network: networkName(m.network), type: m.type || "" };
  if (m.billingContact) c.billing = contactFromApple(m.billingContact);
  return c;
}

/** paymentFromResponse is a PaymentResponse for Apple Pay as the wire's payment. */
export function paymentFromResponse(r) {
  const d = r.details ?? {};
  const t = d.token ?? {};
  const data = t.paymentData;
  const payer = { name: r.payerName, email: r.payerEmail, phone: r.payerPhone };
  const p = {
    token: typeof data === "string" ? data : JSON.stringify(data ?? {}),
    transactionId: t.transactionIdentifier || "",
    card: cardFromApple(t.paymentMethod),
    shipping: contactFromApple(d.shippingContact) ?? contactFromAddress(r.shippingAddress, payer),
    billing: contactFromApple(d.billingContact),
    shippingMethod: r.shippingOption || "",
  };
  if (p.shipping && !p.shipping.email && payer.email) p.shipping.email = payer.email;
  return p;
}

/** The wallet's sheet: its events for Go ("next"), the answers awaited. */
class Sheet {
  constructor(id) {
    this.id = id;
    this.seq = 0;
    this.queue = [];
    this.waiting = null;
    this.pending = new Map();
    this.open = true;
  }

  send(e) {
    const b = JSON.stringify(e);
    if (this.waiting) {
      const w = this.waiting;
      this.waiting = null;
      w.resolve(b);
    } else this.queue.push(b);
  }

  /** ask sends an event and resolves with Go's answer. */
  ask(e) {
    return new Promise((resolve) => {
      const seq = ++this.seq;
      this.pending.set(seq, resolve);
      this.send({ ...e, seq });
    });
  }

  done(error) {
    if (!this.open) return;
    this.open = false;
    for (const r of this.pending.values()) r({ errors: [{ field: "payment", message: "closed" }] });
    this.pending.clear();
    this.send(error ? { type: "done", error } : { type: "done" });
  }
}

export function modules() {
  let sheet = null;
  let merchant = "";
  const dec = new TextDecoder();
  const json = (b) => JSON.parse(dec.decode(b));

  async function presentApple(p, s) {
    const a = p.apple;
    let items = a.items, methods = a.shippingMethods ?? [], selected = methods[0]?.id ?? "";
    const shipping = (a.shippingFields ?? []).includes("address") || methods.length > 0;
    const fields = new Set(a.shippingFields ?? []);
    const options = {
      requestShipping: shipping, requestPayerName: fields.has("name"), requestPayerEmail: fields.has("email"),
      requestPayerPhone: fields.has("phone"),
    };
    if (shipping) options.shippingType = a.shippingType === "delivery" ? "delivery" : a.shippingType?.endsWith("Pickup") ? "pickup" : "shipping";
    const req = new PaymentRequest([{ supportedMethods: appleMethod, data: appleData(a) }], details(a.currency, items, methods, selected), options);
    const keep = (ans) => {
      if (ans.items) items = ans.items;
      if (ans.shippingMethods) methods = ans.shippingMethods;
      return { ...details(a.currency, items, methods, selected), ...updateErrors(ans.errors) };
    };
    req.addEventListener("merchantvalidation", (e) => {
      e.complete(s.ask({ type: "validate", url: e.validationURL }).then((ans) => {
        if (ans.errors?.length || !ans.session) throw new Error(ans.errors?.[0]?.message || "pay: no merchant session");
        return ans.session;
      }));
    });
    if (a.events?.shippingContact) {
      req.addEventListener("shippingaddresschange", (e) => {
        e.updateWith(s.ask({ type: "shippingContact", contact: contactFromAddress(req.shippingAddress) }).then(keep));
      });
    }
    req.addEventListener("shippingoptionchange", (e) => {
      selected = req.shippingOption;
      if (!a.events?.shippingMethod) return e.updateWith(details(a.currency, items, methods, selected));
      e.updateWith(s.ask({ type: "shippingMethod", method: selected }).then(keep));
    });
    if (a.events?.paymentMethod) {
      req.addEventListener("paymentmethodchange", (e) => {
        e.updateWith(s.ask({ type: "paymentMethod", card: cardFromApple(e.methodDetails) }).then(keep));
      });
    }
    if (a.events?.coupon) {
      req.addEventListener("couponcodechange", (e) => {
        const code = e.couponCode ?? e.methodDetails?.couponCode ?? "";
        e.updateWith(s.ask({ type: "coupon", coupon: code }).then(keep));
      });
    }
    let r;
    try {
      r = await req.show();
    } catch (e) {
      s.done(e?.name === "AbortError" ? "" : String(e?.message ?? e));
      return;
    }
    for (;;) {
      const ans = await s.ask({ type: "authorize", payment: paymentFromResponse(r) });
      if (!ans.errors?.length) {
        await r.complete("success").catch(() => {});
        break;
      }
      try {
        await r.retry(retryErrors(ans.errors)); // the user tries again: r has their new choice
      } catch {
        break; // they closed the sheet
      }
    }
    s.done("");
  }

  async function presentGoogle(p, s) {
    const api = await loadGoogle();
    const intents = p.google.callbackIntents ?? [];
    const callbacks = {
      onPaymentAuthorized: (d) => s.ask({ type: "googleAuthorize", google: d }).then((a) => a.google ?? { transactionState: "ERROR" }),
    };
    if (intents.some((i) => i !== "PAYMENT_AUTHORIZATION")) {
      callbacks.onPaymentDataChanged = (d) => s.ask({ type: "google", google: d }).then((a) => a.google ?? {});
    }
    const client = new api.PaymentsClient({ environment: p.googleEnv, paymentDataCallbacks: callbacks });
    try {
      await client.loadPaymentData(p.google);
      s.done("");
    } catch (e) {
      s.done(e?.statusCode === "CANCELED" ? "" : String(e?.statusMessage || e?.statusCode || e?.message || e));
    }
  }

  return {
    pay: {
      async available(args) {
        const p = json(args);
        if (p.apple?.merchant) merchant = p.apple.merchant;
        if (hasApplePay()) {
          let card = false;
          try {
            card = !!p.apple?.merchant && (await globalThis.ApplePaySession.canMakePaymentsWithActiveCard(p.apple.merchant));
          } catch {}
          return JSON.stringify({ wallet: "apple", canPay: true, hasCard: card });
        }
        try {
          const api = await loadGoogle();
          const client = new api.PaymentsClient({ environment: p.googleEnv || "TEST" });
          const r = await client.isReadyToPay(p.googleReady);
          return JSON.stringify({ wallet: "google", canPay: !!r.result, hasCard: !!r.result && r.paymentMethodPresent !== false });
        } catch {
          return JSON.stringify({ wallet: "", canPay: false, hasCard: false });
        }
      },
      async setUp() {
        const s = globalThis.ApplePaySession;
        if (!hasApplePay() || !s.openPaymentSetup || !merchant) throw new Error("not available: Apple Pay's setup needs Safari, and pay.Available's merchant ID");
        await s.openPaymentSetup(merchant);
      },
      present(args) {
        if (sheet?.open) throw new Error("busy");
        const p = json(args);
        if (p.apple?.merchant) merchant = p.apple.merchant;
        const apple = hasApplePay();
        if (apple && p.appleError) throw new Error(p.appleError);
        if (apple && !p.validate) throw new Error("pay: Request.OnValidateMerchant is nil: Apple Pay on the web needs the app's server to validate the merchant");
        if (!apple && p.googleError) throw new Error(p.googleError);
        if (!apple && !p.google) throw new Error("not available");
        const s = (sheet = new Sheet(p.id));
        // Shown now, within the tap's activation; "present" answers once
        // it's asked for, the rest comes as events.
        (apple ? presentApple(p, s) : presentGoogle(p, s)).catch((e) => s.done(String(e?.message ?? e)));
      },
      next(args) {
        const id = dec.decode(args);
        const s = sheet;
        if (!s || s.id !== id || (!s.open && !s.queue.length)) throw new Error("stopped");
        if (s.queue.length) return s.queue.shift();
        s.waiting?.reject(new Error("stopped"));
        return new Promise((resolve, reject) => (s.waiting = { resolve, reject }));
      },
      answer(args) {
        const a = json(args);
        const s = sheet;
        if (!s || a.id !== s.id) return;
        const r = s.pending.get(a.seq);
        if (r) {
          s.pending.delete(a.seq);
          r(a);
        }
      },
    },
  };
}

// The button.

const appleTypes = ["plain", "buy", "set-up", "plain", "donate", "check-out", "book", "subscribe", "reload", "add-money",
  "top-up", "order", "rent", "support", "contribute", "tip", "continue"];
const googleTypes = { 1: "buy", 2: "book", 3: "checkout", 4: "donate", 5: "order", 6: "pay", 7: "subscribe", 8: "plain" };

/** appleButtonType is a PKPaymentButtonType's CSS name (-apple-pay-button-type). */
export const appleButtonType = (t) => appleTypes[t] ?? "plain";
/** googleButtonType is a ButtonConstants.ButtonType's name for createButton. */
export const googleButtonType = (t) => googleTypes[t] ?? "buy";

/** payButton is "asuka.payButton": ctx as asuka.registerNative's. */
export function payButton(ctx) {
  const el = document.createElement("div");
  el.className = "asuka-pay-button";
  el.style.display = "flex";
  const s = { appleType: 1, googleType: 1, style: 0, radius: -1, methods: "[]", appleLabel: "", googleLabel: "", label: "" };
  let queued = false, alive = true;
  const apple = hasApplePay();
  const dark = () => (ctx.host ? isDark(ctx.host) : matchMedia("(prefers-color-scheme: dark)").matches);
  const scheme = matchMedia("(prefers-color-scheme: dark)");
  const make = () => {
    queued = false;
    if (!alive) return;
    if (apple) {
      const b = document.createElement("div");
      b.setAttribute("role", "button");
      b.tabIndex = 0;
      b.setAttribute("aria-label", s.appleLabel || "Apple Pay");
      b.lang = document.documentElement.lang || navigator.language;
      const style = s.style === 0 ? (dark() ? "white" : "black") : ["black", "black", "white", "white-outline"][s.style] ?? "black";
      b.style.setProperty("-webkit-appearance", "-apple-pay-button");
      b.style.setProperty("-apple-pay-button-type", appleButtonType(s.appleType));
      b.style.setProperty("-apple-pay-button-style", style);
      b.style.flex = "1";
      b.style.cursor = "pointer";
      if (s.radius >= 0) b.style.borderRadius = `${s.radius}px`;
      b.addEventListener("click", () => ctx.emit("press"));
      b.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          ctx.emit("press");
        }
      });
      el.replaceChildren(b);
      return;
    }
    loadGoogle().then((api) => {
      if (!alive) return;
      let methods = [];
      try {
        methods = JSON.parse(s.methods);
      } catch {}
      const color = s.style === 0 ? (dark() ? "white" : "black") : s.style === 1 ? "black" : "white";
      const o = {
        onClick: () => ctx.emit("press"), buttonType: googleButtonType(s.googleType), buttonColor: color,
        buttonSizeMode: "fill", allowedPaymentMethods: methods, buttonLocale: (document.documentElement.lang || navigator.language).slice(0, 2),
      };
      if (s.radius >= 0) o.buttonRadius = s.radius;
      const b = new api.PaymentsClient({ environment: "TEST" }).createButton(o);
      b.style.flex = "1";
      el.replaceChildren(b);
    }, () => el.replaceChildren());
  };
  const queue = () => {
    if (!queued) {
      queued = true;
      queueMicrotask(make);
    }
  };
  const onScheme = () => s.style === 0 && queue();
  scheme.addEventListener?.("change", onScheme);
  queue();
  return {
    el,
    set(name, v) {
      if (!(name in s)) return;
      s[name] = typeof v === "bigint" ? Number(v) : v;
      queue();
    },
    measure(c) {
      const w = c.width === c.width ? c.width : Number.isFinite(c.availW) ? Math.max(Math.min(c.availW, 600), 140) : 240;
      return [w, apple ? 44 : 48];
    },
    destroy() {
      alive = false;
      scheme.removeEventListener?.("change", onScheme);
    },
  };
}

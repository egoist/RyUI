# In-app purchases

`asuka/sdk/purchase` sells in the app, through each platform's own store:
consumables (coins, a hint), non-consumables (a level, "Pro" for life),
subscriptions that renew, and those that don't. It covers what
react-native-iap, expo-iap and RevenueCat's SDK do on the device, with
the stores' current APIs:

| Platform | Store | API |
| --- | --- | --- |
| iOS, macOS | the App Store | StoreKit 2 |
| Android | Google Play | the Play Billing Library 9 |
| Windows | the Microsoft Store | `Windows.Services.Store`, for an app installed from the Store |
| The web | Google Play | the Digital Goods API and Payment Request, in a Trusted Web Activity |
| Linux | none | |

Where there's no store (Linux, a browser without the Digital Goods API, a
Windows app that wasn't installed from the Store, the player in
`asuka dev`), `purchase.Available()` is false and the rest fail with
`purchase.ErrNotAvailable`.

## Products

An `Item` is a product the app sells: its ID in the store and its kind.
Play doesn't know a consumable from a non-consumable, so the app says.

```go
var (
	coins = purchase.Item{ID: "coins_100", Kind: purchase.Consumable}
	pro   = purchase.Item{ID: "pro", Kind: purchase.NonConsumable}
	plus  = purchase.Item{ID: "plus_monthly", Kind: purchase.AutoRenewable}
)

products, err := purchase.Products(coins, pro, plus)
```

The calls wait for the store, so make them off the main thread (in a
goroutine, with `ui.Post` for what they change). `Products` answers
those the store sells, in the user's country and language; the others
are left out. Each `Product` has its `Title`, `Description`, `Price` for
display ("$4.99", "4,99 €") and `PriceMicros` and `Currency` for
arithmetic. A subscription's `Subscription` has its group, its level in
the group, its period and its offers: Apple's introductory, promotional
and win-back offers, Play's base plans and the offers on them (each
with its phases: a free trial, a lower price for some periods, the
regular price). `IntroEligible` says whether the user can still have
the introductory offer.

The IDs are App Store Connect's product IDs, Play Console's, and on
Windows each add-on's product ID in Partner Center (its
`InAppOfferToken`) or its Store ID.

## Buying

```go
r, err := purchase.Buy(coins, purchase.Options{AccountToken: userUUID})
switch {
case err != nil:
	// purchase.ErrNotAllowed, ErrUnknownProduct, ErrAlreadyOwned...
case r.Status == purchase.Bought:
	deliver(r.Transaction)
	r.Transaction.Finish()
case r.Status == purchase.PendingApproval:
	// Ask to Buy, a payment Play waits for: Observe gives it later
case r.Status == purchase.Cancelled:
}
```

`Buy` shows the store's sheet and answers once the user has bought,
cancelled, or the purchase waits for approval. `Options` has:

- `Quantity`: a consumable's (Apple's; on Play the user picks it, when
  multi-quantity is on in Play Console).
- `AccountToken`: ties the purchase to the app's user. Use a UUID:
  Apple's `appAccountToken` must be one, Play takes it as its
  `obfuscatedAccountId`. It comes back in the transaction and in the
  stores' server notifications. Never an email or a name.
  `ProfileToken` is Play's `obfuscatedProfileId`.
- `Offer`: one of the product's offers. Play needs one for a
  subscription; without it, `Buy` takes the first base plan. An Apple
  promotional offer also needs `Signature`, which the app's server signs
  with its In-App Purchase key (the key's ID, a nonce, the signature and
  its time; or, from iOS 18.4, a JWS).
- `Replace`: the Play subscription this one replaces, an upgrade or a
  downgrade, and how (`WithTimeProration`, `ChargeProratedPrice`,
  `ChargeFullPrice`, `WithoutProration`, `Deferred`). Apple moves a
  subscriber within a subscription group by itself.
- `Personalized`: Play's flag for a personalized price, which the EU
  asks apps to show.

## Transactions

A `Transaction` is a purchase: its `ID`, `ProductID`, `Kind`, `State`
(`Purchased`, `Pending`, `Revoked`), `Quantity`, `Date`, a
subscription's `Expires`, and when one was revoked, `Revoked` and why.

Deliver what a transaction sold, then `Finish` it. Until it's finished,
the store gives it again at each start. StoreKit keeps it unfinished,
and Play refunds a purchase that isn't acknowledged within three days.
`Finish` consumes a `Consumable` (Play's `consumeAsync`, the Microsoft
Store's fulfillment), so the user can buy it again; for the others it's
StoreKit's `finish` and Play's acknowledgement. A `Pending` transaction
can't be finished.

`Observe` gives everything that happens outside a `Buy`:

```go
stop := purchase.Observe(func(e purchase.Event) {
	switch e.Type {
	case purchase.TransactionUpdated:
		t := e.Transaction
		switch t.State {
		case purchase.Purchased:
			deliver(t)
			go t.Finish()
		case purchase.Revoked:
			takeBack(t)
		}
	case purchase.StorefrontChanged:
		// prices are another country's: ask for the products again
	}
})
```

That's a pending purchase approved or paid, a purchase made outside the
app (an offer code redeemed in the store, a purchase on another device),
a subscription renewed, a purchase refunded or revoked, and those left
unfinished from before. The host listens from the app's start (StoreKit's
`Transaction.updates`, Play's `PurchasesUpdatedListener` and the
unacknowledged purchases it finds as the app starts and comes back to
the front), and keeps what comes before the first observer for it, so
none is lost. Observe as the app starts, in its root component.

Call `purchase.Declare(coins, pro, plus)` before you observe, so that a
consumable left unfinished from before is consumed when you finish it:
Play doesn't say which kind it is. `Products` and `Buy` declare their
items too.

What each store tells the device differs:

- Apple's says everything: renewals are transactions, refunds are
  transactions with `Revoked` set.
- Play's client sees no renewals (a subscription keeps its purchase
  token), no refunds and no expiries: they're in `CurrentEntitlements`
  as they happen, and your server hears them through Play's Real-time
  developer notifications.
- The Microsoft Store says when the user's licenses change: a new one is
  a transaction, a durable's that's gone is revoked.

## Entitlements and restoring

`CurrentEntitlements()` is what the user owns now: non-consumables,
subscriptions in their period or grace period, and Apple's non-renewing
subscriptions once finished (the app decides how long those last).
`Entitlements.Has(id)` checks one.

In a component, `purchase.Use()` is the entitlements as a signal. It's
asked for as the component mounts, and again after each `Buy`, `Finish`
and `Restore` and each transaction the app's `Observe` gets:

```go
owned := purchase.Use()
ui.Show(signal.Func(func() bool { return owned.Get().Has(pro.ID) }), proFeatures)
```

`Restore()` asks the store again and answers the entitlements. On iOS
it's `AppStore.sync`, which asks the user to sign in, so call it only
from a "Restore purchases" button, which App Review expects an app with
non-consumables or subscriptions to have.

## Subscriptions

`SubscriptionStatuses(group)` has each subscription of a group (Apple's
subscription group ID; on Play, the product ID): `Subscribed`,
`Expired`, `BillingRetry` (the payment failed and the store retries;
Play's account hold), `GracePeriod` (still entitled while it retries) or
`SubscriptionRevoked`. It also says whether it renews (`WillRenew`), as
what (`RenewsTo`, after an upgrade or a downgrade), when, and why it
expired. `RenewalJWS` is Apple's signed renewal info.

`ManageSubscriptions(productID)` shows the store's page of the user's
subscriptions, where they cancel or change one: iOS's sheet, Play's
subscription center (at that product), the App Store's or Microsoft's
account page on the desktop. `RedeemCode()` shows Apple's offer code
sheet (iOS 16, macOS 15) or opens Play's redeem page; what the code buys
comes through `Observe`. `RequestRefund(t)` shows Apple's refund request
sheet. Play has no such sheet: refunds are the developer's, from Play
Console or its server.

`CurrentStorefront()` is the store's country, which prices and products
follow.

## Verifying on a server

A server checks a purchase with the store before it grants what was
sold. `Transaction.Verification` has what each store's API takes:

- Apple: `JWS`, the transaction signed by the App Store. Verify it with
  the App Store Server Library (its `SignedDataVerifier`), or check its
  certificate chain up to Apple's root yourself. `Verified` says
  whether StoreKit already checked it on the device.
- Google Play: `Token`, the purchase token, with `PackageName`, for the
  Google Play Developer API (`purchases.productsv2`,
  `purchases.subscriptionsv2`). `OriginalJSON` and `Signature` are the
  purchase signed with the app's license key.
- Windows: `Token`, the add-on's Store ID, for the Microsoft Store
  collections API.

With a server that grants purchases, the app can deliver what the
server says, and finish the transaction once it has.

## Testing your purchases

The stores sell only an app they know: the player in `asuka dev` can't
sell your app's products. Test with a built app.

**The iOS simulator.** Make a StoreKit configuration file in Xcode (File ›
New › File › StoreKit Configuration File; or sync it from App Store
Connect), with the products, and name it in `asuka.json`:

```json
{
  "ios": { "storekit": "Products.storekit" }
}
```

`asuka dev`'s build for the simulator carries it, and as `asuka dev`
launches it, sells from it, as an app run from Xcode with the file in
its scheme does. Prices, Ask to Buy, failed transactions and faster
renewals come from the file:

```sh
asuka dev --platform ios --no-hot examples/store   # built, rebuilt on save
asuka dev --platform ios --once examples/store     # built and launched once
```

Launched another way (from the home screen), the app can't load the
file itself (its session needs Xcode's XCTest, which `asuka dev` gives
it as a test run has it); StoreKit goes on with the configuration
`asuka dev` last gave it for the app. Other builds never
have the file: they buy from the App Store's sandbox. Sign in with a sandbox account (Settings › Developer on the
device, or as you buy), or install a TestFlight build.

**Android.** Play sells only to a build it has. Upload one to the
internal track (`asuka publish --platform android --track internal`),
add the products in Play Console, add your Google account to the
license testers (Play Console › Setup › License testing), and install
the build from the track's link. License testers are never charged, and
renewals come faster.

**The Mac.** As on iOS, with a sandbox account; a Mac build from
`asuka build` has StoreKit 2 in its Swift library.

**Windows.** The Store sells only to the app it installed: associate the
app with the Store in Partner Center and install it from there (a
package flight for testing). An app run with `asuka dev` has no store.

**In `go test`.** `host/fake`'s `Store` sells what a test gives it, and
keeps the transactions as StoreKit does: unfinished until the app
finishes them, given again until then.

```go
var store *fake.Store
a := uitest.Mount(t, Store, uitest.Setup(func(h *fake.Host) {
	store = h.Store()
	store.Products = []fake.StoreProduct{
		{ID: "coins_100", Kind: "consumable", Title: "100 Coins", Price: "$0.99", PriceMicros: 990000},
		{ID: "plus_monthly", Kind: "autoRenewable", Title: "Plus", Price: "$4.99", Period: "P1M", Trial: "P1W"},
	}
}))
a.Tap("$0.99")
a.WaitFor("100 coins")

store.Outcome = fake.PurchasePending // Ask to Buy
a.Tap("$0.99")
store.Approve("coins_100")           // the parent approves: Observe gets it
store.Renew("plus_monthly")          // a renewal
store.Refund(id)                     // a refund
store.Deliver("pro")                 // bought on another device
```

`Outcome` also has `PurchaseCancelled` and `PurchaseFailed`;
`Unavailable` and `NotAllowed` are a store that isn't there and payments
turned off; `Unfinished` lists what the app hasn't finished.

## Setting up the app

- **iOS and macOS.** The In-App Purchase capability is on for every App
  ID, and it needs no entitlement: nothing to add. Agree to the Paid
  Apps Agreement in App Store Connect, and add the products there.
- **Android.** An app that imports `asuka/sdk/purchase` is built with
  Play Billing and the `com.android.vending.BILLING` permission; others
  aren't.
- **Windows.** Add the add-ons in Partner Center, to an app that's
  packaged (MSIX) and associated with the Store.
- **The web.** Package the site as a Trusted Web Activity with Play
  Billing enabled (Bubblewrap's `playBilling`), and add the products in
  Play Console. The Digital Goods API acknowledges nothing for a
  non-consumable: acknowledge it from your server, with the Play
  Developer API, within three days.

[examples/store](../examples/store) sells 100 coins, Pro and a monthly
subscription with a free week, with a StoreKit configuration file and
tests on `fake.Store`.

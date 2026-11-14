// Package purchase sells in the app (react-native-iap, expo-iap,
// RevenueCat's SDK): consumables (coins, a hint), non-consumables (a
// level, "Pro" for life), subscriptions that renew, and those that don't.
// Each platform's own store:
//
//   - iOS and macOS: the App Store, through StoreKit 2.
//   - Android: Google Play, through the Play Billing Library (9).
//   - Windows: the Microsoft Store (Windows.Services.Store), for an app
//     installed from it.
//   - The web: Google Play, through the Digital Goods API, in a Trusted
//     Web Activity that Play installs.
//
// Linux has no store, nor has a browser without the Digital Goods API:
// Available is false there, and the rest fail with ErrNotAvailable.
//
//	var (
//		coins   = purchase.Item{ID: "coins_100", Kind: purchase.Consumable}
//		pro     = purchase.Item{ID: "pro", Kind: purchase.NonConsumable}
//		monthly = purchase.Item{ID: "plus_monthly", Kind: purchase.AutoRenewable}
//	)
//
//	stop := purchase.Observe(func(e purchase.Event) {
//		if t := e.Transaction; t != nil && t.State == purchase.Purchased {
//			deliver(t) // add the coins, unlock Pro
//			t.Finish()
//		}
//	})
//	...
//	go func() {
//		products, err := purchase.Products(coins, pro, monthly)
//		...
//		r, err := purchase.Buy(coins, purchase.Options{})
//		if err == nil && r.Status == purchase.Bought {
//			deliver(r.Transaction)
//			r.Transaction.Finish()
//		}
//	}()
//
// # Transactions
//
// Buy answers with what the purchase came to: Bought (with its
// Transaction), Pending (Ask to Buy, a payment Play waits for: the
// transaction comes later, through Observe) or Cancelled. Observe gives
// the rest: a pending purchase completed, a purchase made outside the app
// (an offer code redeemed in the store, on another device), a
// subscription renewed, a purchase refunded or revoked, and those left
// unfinished from before. Until the app observes, the host keeps them,
// from the app's start: none is lost.
//
// Deliver a transaction's content, then Finish it. Until it's finished
// the store gives it again (Observe, at each start): StoreKit keeps it
// unfinished; Play refunds a purchase not finished within three days.
// Finish consumes a Consumable, so it can be bought again.
//
// Entitlements are what the user owns now: the non-consumables, the
// subscriptions that are active, those that don't renew while the app
// says they last. Restore asks the store again (on iOS, after the user
// signs in): put it behind a "Restore purchases" button, as the App
// Store's review wants.
//
// # Verifying on a server
//
// A server checks a purchase with the store before it grants what it
// sold: Transaction.Verification has what each store's API takes (the
// JWS that the App Store Server API and its libraries verify; Play's
// purchase token for the Google Play Developer API's purchases.products
// and purchases.subscriptionsv2). Options.AccountToken ties a purchase to
// the app's user, and comes back in the transaction and in the stores'
// server notifications.
//
// # Testing
//
// The stores sell only an app they know: in asuka dev the player can't
// sell the app's products. Test with a built app: on the iOS simulator,
// asuka.json's ios.storekit (a StoreKit configuration file, Xcode's) and
// asuka dev --no-hot (or --once); on a device, a
// TestFlight build (the sandbox); on Android, a build on Play's internal
// track, installed by one of the license testers. In go test, host/fake's
// Store sells what a test gives it (fake.Host.Store).
package purchase

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	spec "github.com/egoist/asuka/internal/purchasespec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Kind is what kind of product it is.
type Kind uint8

const (
	// Consumable: bought again and again, used up (coins, lives). Finish
	// consumes it.
	Consumable Kind = iota
	// NonConsumable: bought once, kept (a level, Pro for life); in
	// Entitlements, restored on a new device.
	NonConsumable
	// AutoRenewable: a subscription that renews until cancelled.
	AutoRenewable
	// NonRenewing: a subscription for a time, which the app ends (Apple's
	// non-renewing subscriptions; Play's prepaid plans).
	NonRenewing
)

func (k Kind) String() string {
	return [...]string{"consumable", "nonConsumable", "autoRenewable", "nonRenewing", "?"}[min(int(k), 4)]
}

func kindOf(s string) Kind {
	switch s {
	case spec.Consumable:
		return Consumable
	case spec.AutoRenewable, spec.Subs:
		return AutoRenewable
	case spec.NonRenewing:
		return NonRenewing
	}
	return NonConsumable
}

// Item is a product the app sells: its ID in the store (App Store
// Connect's product ID; Play Console's; Partner Center's add-on's
// product ID, its InAppOfferToken), and its kind. Play doesn't know a
// consumable from a non-consumable, so the app says.
type Item struct {
	ID   string
	Kind Kind
}

// Period is how long a subscription lasts before it renews: Count Units.
type Period struct {
	Unit  PeriodUnit
	Count int
}

// PeriodUnit is a Period's unit.
type PeriodUnit uint8

const (
	Day PeriodUnit = iota
	Week
	Month
	Year
)

func (u PeriodUnit) String() string {
	return [...]string{"day", "week", "month", "year", "?"}[min(int(u), 4)]
}

func periodOf(p *spec.Period) Period {
	if p == nil {
		return Period{}
	}
	u := map[string]PeriodUnit{"day": Day, "week": Week, "month": Month, "year": Year}[p.Unit]
	return Period{Unit: u, Count: p.Count}
}

// Mode is how an offer's phase is paid.
type Mode uint8

const (
	// Recurring: the price each period (a subscription's base price).
	Recurring Mode = iota
	// FreeTrial: nothing, for the phase.
	FreeTrial
	// PayAsYouGo: a lower price each period, for Cycles periods.
	PayAsYouGo
	// PayUpFront: one lower price for the phase.
	PayUpFront
)

// Phase is one price of an offer: Cycles periods at Price.
type Phase struct {
	Price       string
	PriceMicros int64
	Currency    string
	Period      Period
	Cycles      int
	Mode        Mode
}

// OfferType is what kind of offer it is.
type OfferType uint8

const (
	// Base: Play's base plan, the subscription at its own price.
	Base OfferType = iota
	// Introductory: a new subscriber's (Apple's; on Play, an offer whose
	// eligibility is "new customer").
	Introductory
	// Promotional: Apple's for existing or lapsed subscribers, signed by
	// the app's server (Options.Signature); Play's developer-determined
	// offers.
	Promotional
	// WinBack: Apple's for lapsed subscribers (iOS 18).
	WinBack
	// Code: an offer code redeemed (Apple's).
	Code
	// Offer: Play's offer on a base plan.
	Offer
	// OneTime: Play's purchase option of a one-time product.
	OneTime
)

var offerTypes = []string{"base", "introductory", "promotional", "winBack", "code", "offer", "oneTime"}

func (t OfferType) String() string {
	if int(t) < len(offerTypes) {
		return offerTypes[t]
	}
	return "?"
}

func offerTypeOf(s string) OfferType {
	for i, n := range offerTypes {
		if n == s {
			return OfferType(i)
		}
	}
	return Base
}

// SubscriptionOffer is a way to buy a subscription: Apple's introductory,
// promotional and win-back offers; Play's base plans and their offers,
// each with the token Buy takes (Options.Offer).
type SubscriptionOffer struct {
	ID       string
	Type     OfferType
	BasePlan string   // Play's
	Tags     []string // Play's
	Phases   []Phase
	token    string
}

// Subscription is what a subscription's product has.
type Subscription struct {
	// Group is Apple's subscription group (one subscription a group at a
	// time; moving within it is an upgrade or a downgrade, by Level, 1
	// the highest). On Play, the product.
	Group string
	Level int
	// Period is the base plan's.
	Period Period
	// Offers are those the user can buy it with. Play's include each base
	// plan (Type Base) and the offers on it the user is eligible for.
	Offers []SubscriptionOffer
	// IntroEligible: the user can have the introductory offer (Apple's;
	// Play leaves out the offers a user can't have).
	IntroEligible bool
}

// Product is a product as the store sells it, in the user's country and
// language.
type Product struct {
	Item
	Title, Description string
	// Price is for display, in the store's currency: "$4.99", "4,99 €".
	Price string
	// PriceMicros is the price in millionths of the currency's unit
	// (4990000 is 4.99); Currency, its ISO 4217 code.
	PriceMicros int64
	Currency    string
	// Subscription is a subscription's (AutoRenewable, NonRenewing on
	// Play), nil otherwise.
	Subscription *Subscription
	// FamilyShareable: Family Sharing shares it (Apple's).
	FamilyShareable bool
	// Offers are a one-time product's purchase options (Play's).
	Offers []SubscriptionOffer
}

func productOf(p spec.Product) Product {
	out := Product{Item: Item{ID: p.ID, Kind: kindOf(p.Kind)}, Title: p.Title, Description: p.Description,
		Price: p.Price, PriceMicros: p.Micros, Currency: p.Currency, FamilyShareable: p.FamilyShareable}
	if k, ok := declared(p.ID); ok {
		out.Kind = k
	}
	var offers []SubscriptionOffer
	for _, o := range p.Offers {
		so := SubscriptionOffer{ID: o.ID, Type: offerTypeOf(o.Type), BasePlan: o.BasePlan, Tags: o.Tags, token: o.Token}
		for _, ph := range o.Phases {
			so.Phases = append(so.Phases, Phase{Price: ph.Price, PriceMicros: ph.Micros, Currency: ph.Currency,
				Period: periodOf(&ph.Period), Cycles: ph.Cycles, Mode: modeOf(ph.Mode)})
		}
		offers = append(offers, so)
	}
	if p.Period != nil || p.Group != "" || out.Kind == AutoRenewable {
		out.Subscription = &Subscription{Group: p.Group, Level: p.Level, Period: periodOf(p.Period), Offers: offers, IntroEligible: p.IntroEligible}
	} else {
		out.Offers = offers
	}
	return out
}

func modeOf(s string) Mode {
	switch s {
	case "freeTrial":
		return FreeTrial
	case "payAsYouGo":
		return PayAsYouGo
	case "payUpFront":
		return PayUpFront
	}
	return Recurring
}

// State is where a transaction is.
type State uint8

const (
	// Purchased: paid for (bought, renewed, restored): deliver it.
	Purchased State = iota
	// Pending: waiting for a parent's approval (Ask to Buy) or a payment
	// (Play's cash, bank transfers). Don't deliver it yet: Observe gives
	// it again, Purchased, once it is.
	Pending
	// Revoked: refunded, or taken back (Family Sharing stopped sharing
	// it): take back what it gave.
	Revoked
)

func (s State) String() string {
	return [...]string{"purchased", "pending", "revoked", "?"}[min(int(s), 3)]
}

// Verification is what a server checks a purchase with.
type Verification struct {
	// JWS is Apple's: the transaction signed by the App Store, a JSON Web
	// Signature whose certificate chain goes to Apple's root (the App
	// Store Server Library's SignedDataVerifier checks it).
	JWS string
	// Token is Play's purchase token, with PackageName, for the Google
	// Play Developer API (purchases.productsv2, purchases.subscriptionsv2);
	// OriginalJSON and Signature are the purchase signed with the app's
	// license key (Play Console's). On Windows, the Store's ID of the
	// add-on (a server asks the Microsoft Store collections API).
	Token, PackageName, OriginalJSON, Signature string
	// Verified: the device checked the store's signature (StoreKit's
	// VerificationResult). A server checks for itself.
	Verified bool
}

// Transaction is a purchase: a product bought, renewed, restored, pending
// or revoked.
type Transaction struct {
	// ID is the store's: Apple's transaction ID, Play's order ID (Pending:
	// the purchase token), the Microsoft Store's tracking ID.
	ID string
	// OriginalID is the first purchase's, which a subscription's renewals
	// and restores share (Apple's).
	OriginalID string
	ProductID  string
	Kind       Kind
	State      State
	// Quantity bought (a consumable's, Apple's and Play's multi-quantity).
	Quantity int
	// Date is when it was bought (renewed); Expires, when a subscription's
	// period ends (Apple's: Play's client doesn't say, its server does).
	Date, Expires time.Time
	// Revoked is when it was revoked, and why ("refundedForIssue",
	// "refundedForOther": Apple's).
	Revoked          time.Time
	RevocationReason string
	// Upgraded: the user moved to a higher subscription in its group,
	// which replaces this one (Apple's).
	Upgraded bool
	// AutoRenews: the subscription will renew (Play's).
	AutoRenews bool
	// FamilyShared: a family member bought it (Apple's).
	FamilyShared bool
	// AccountToken and ProfileToken are Options' of its Buy.
	AccountToken, ProfileToken string
	// Environment is Apple's: "Production", "Sandbox", "Xcode".
	Environment string
	// Storefront is the country it was bought in.
	Storefront string
	// OfferID and OfferType are the offer it was bought with.
	OfferID   string
	OfferType OfferType

	Verification Verification

	token string // Play's
}

func transactionOf(w *spec.Transaction) *Transaction {
	if w == nil {
		return nil
	}
	t := &Transaction{ID: w.ID, OriginalID: w.OriginalID, ProductID: w.ProductID, Kind: kindOf(w.Kind),
		Quantity: max(w.Quantity, 1), Upgraded: w.Upgraded, AutoRenews: w.AutoRenews, FamilyShared: w.FamilyShared,
		RevocationReason: w.RevocationReason, AccountToken: w.AccountToken, ProfileToken: w.ProfileToken,
		Environment: w.Environment, Storefront: w.Storefront, OfferID: w.OfferID, OfferType: offerTypeOf(w.OfferType),
		Verification: Verification{JWS: w.JWS, Token: w.Token, PackageName: w.PackageName, OriginalJSON: w.OriginalJSON,
			Signature: w.Signature, Verified: w.Verified},
		token: w.Token}
	if k, ok := declared(w.ProductID); ok && (w.Kind == spec.InApp || w.Kind == spec.Subs || w.Kind == "") {
		t.Kind = k
	}
	switch w.State {
	case "pending":
		t.State = Pending
	case "revoked":
		t.State = Revoked
	}
	t.Date, t.Expires, t.Revoked = msTime(w.DateMs), msTime(w.ExpiresMs), msTime(w.RevokedMs)
	return t
}

func msTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// Errors.
var (
	// ErrNotAvailable: the platform has no store (Linux, a browser without
	// the Digital Goods API, a Windows app not installed from the
	// Microsoft Store, an Android without Play), or the app isn't built
	// with one (the player in asuka dev).
	ErrNotAvailable = errors.New("purchase: no store")
	// ErrNotAllowed: the user can't pay (parental controls, a device
	// management profile).
	ErrNotAllowed = errors.New("purchase: payments not allowed")
	// ErrUnknownProduct: the store sells no such product, to this app, in
	// the user's country.
	ErrUnknownProduct = errors.New("purchase: unknown product")
	// ErrAlreadyOwned: the user owns it (a non-consumable; a consumable
	// not yet finished on Play).
	ErrAlreadyOwned = errors.New("purchase: already owned")
)

// The module's name.
const module = "purchase"

// call is ui.CallWait, its errors the package's.
func call(method string, args any) ([]byte, error) {
	var b []byte
	if args != nil {
		b, _ = json.Marshal(args)
	}
	out, err := ui.CallWait(module, method, b)
	if err != nil {
		return nil, errorOf(err)
	}
	return out, nil
}

func errorOf(err error) error {
	m := err.Error()
	switch {
	case strings.HasPrefix(m, "not available"), strings.HasPrefix(m, "no module"):
		return ErrNotAvailable
	case strings.HasPrefix(m, "not allowed"):
		return ErrNotAllowed
	case strings.HasPrefix(m, "unknown product"):
		return ErrUnknownProduct
	case strings.HasPrefix(m, "already owned"):
		return ErrAlreadyOwned
	}
	return errors.New("purchase: " + m)
}

var (
	kindsMu sync.Mutex
	kinds   = map[string]Kind{}
)

// Declare says what the app sells, as Products does, without asking the
// store: Play doesn't know a consumable from a non-consumable, so a
// consumable's transaction that comes before Products (one unfinished
// from before, given at the start) needs it to be consumed. Any thread.
func Declare(items ...Item) {
	kindsMu.Lock()
	defer kindsMu.Unlock()
	for _, it := range items {
		kinds[it.ID] = it.Kind
	}
}

func declared(id string) (Kind, bool) {
	kindsMu.Lock()
	defer kindsMu.Unlock()
	k, ok := kinds[id]
	return k, ok
}

func items(its []Item) []spec.Item {
	out := make([]spec.Item, len(its))
	for i, it := range its {
		out[i] = spec.Item{ID: it.ID, Kind: it.Kind.String()}
	}
	return out
}

// Available reports whether the user can buy: the platform has a store,
// the app is its, and payments are allowed (iOS's Screen Time, for one,
// can turn them off). Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Available() bool {
	b, err := call("available", nil)
	return err == nil && len(b) > 0 && b[0] == 1
}

// Products asks the store for items, in its user's country and language
// (Declare's too). Those it doesn't sell are left out. Not on the main
// thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Products(its ...Item) ([]Product, error) {
	Declare(its...)
	b, err := call("products", map[string]any{"items": items(its)})
	if err != nil {
		return nil, err
	}
	var w spec.Products
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, err
	}
	out := make([]Product, 0, len(w.Products))
	for _, p := range w.Products {
		out = append(out, productOf(p))
	}
	return out, nil
}

// ReplaceMode is how a Play subscription replaces another (Options.Replace),
// as Play Console's upgrade and downgrade settings say.
type ReplaceMode uint8

const (
	// WithTimeProration: now, the time left on the old one credited to
	// the new.
	WithTimeProration ReplaceMode = iota
	// ChargeProratedPrice: now, charging the difference for the rest of
	// the period (an upgrade to a pricier one, same period).
	ChargeProratedPrice
	// ChargeFullPrice: now, charging the new one's full price.
	ChargeFullPrice
	// WithoutProration: now, the new price from the next renewal.
	WithoutProration
	// Deferred: at the old one's renewal (a downgrade).
	Deferred
)

func (m ReplaceMode) String() string {
	return [...]string{"withTimeProration", "chargeProratedPrice", "chargeFullPrice", "withoutProration", "deferred", "?"}[min(int(m), 5)]
}

// Replace is the subscription a Play purchase replaces: an upgrade, a
// downgrade, a change of plan. Apple moves a subscriber within a
// subscription group by itself.
type Replace struct {
	Old  *Transaction
	Mode ReplaceMode
}

// OfferSignature is an Apple promotional offer's, signed by the app's
// server with its In-App Purchase key (App Store Connect's): the key's ID,
// the nonce (a UUID, lower case), the signature and its time; or, from
// iOS 18.4 and macOS 15.4, a JWS of the same (JWS).
type OfferSignature struct {
	KeyID     string
	Nonce     string
	Signature []byte
	Timestamp time.Time
	JWS       string
}

// Options are how Buy buys.
type Options struct {
	// Quantity is how many (a consumable's; default 1). Play's
	// multi-quantity purchases must be on in Play Console.
	Quantity int
	// AccountToken ties the purchase to the app's user: a UUID (Apple's
	// appAccountToken must be one; Play's obfuscatedAccountId takes it as
	// it is). It comes back in the transaction and the stores' server
	// notifications. Never an email or a name.
	AccountToken string
	// ProfileToken is Play's obfuscatedProfileId: a profile within the
	// user's account.
	ProfileToken string
	// Offer buys a subscription (or a Play one-time product) with one of
	// its offers. Play needs one for a subscription: without, Buy takes
	// the first base plan. Apple's promotional offers need Signature.
	Offer *SubscriptionOffer
	// Signature is an Apple promotional offer's.
	Signature *OfferSignature
	// Replace is the Play subscription this one replaces.
	Replace *Replace
	// Personalized says the price is personalized (Play: the EU's
	// Consumer Rights Directive wants it said).
	Personalized bool
}

// Status is what a purchase came to.
type Status uint8

const (
	// Bought: paid for. Deliver the transaction, then Finish it.
	Bought Status = iota
	// PendingApproval: waiting (Ask to Buy, a payment Play waits for):
	// Observe gives the transaction once it's paid for, or not.
	PendingApproval
	// Cancelled: the user didn't buy.
	Cancelled
)

func (s Status) String() string {
	return [...]string{"bought", "pending", "cancelled", "?"}[min(int(s), 3)]
}

// Result is what Buy came to: a Bought one's transaction; Play's Pending
// one's too (State Pending).
type Result struct {
	Status      Status
	Transaction *Transaction
}

// Buy shows the store's purchase sheet for item and answers once the user
// has bought, cancelled, or it waits for approval. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Buy(item Item, o Options) (Result, error) {
	Declare(item)
	w := spec.Buy{ID: item.ID, Kind: item.Kind.String(), Quantity: o.Quantity, AccountToken: o.AccountToken,
		ProfileToken: o.ProfileToken, Personalized: o.Personalized}
	if f := o.Offer; f != nil {
		w.OfferID, w.OfferType, w.OfferToken = f.ID, f.Type.String(), f.token
	}
	if s := o.Signature; s != nil {
		w.Signature = &spec.Signature{KeyID: s.KeyID, Nonce: s.Nonce, JWS: s.JWS}
		if len(s.Signature) > 0 {
			w.Signature.Signature = base64.StdEncoding.EncodeToString(s.Signature)
		}
		if !s.Timestamp.IsZero() {
			w.Signature.Timestamp = s.Timestamp.UnixMilli()
		}
	}
	if r := o.Replace; r != nil && r.Old != nil {
		w.Replace = &spec.Replace{Token: r.Old.token, ProductID: r.Old.ProductID, Mode: r.Mode.String()}
	}
	b, err := call("buy", w)
	if err != nil {
		return Result{}, err
	}
	defer entitlementsChanged()
	var res spec.Result
	if err := json.Unmarshal(b, &res); err != nil {
		return Result{}, err
	}
	out := Result{Transaction: transactionOf(res.Transaction)}
	switch res.Status {
	case "purchased":
		out.Status = Bought
	case "pending":
		out.Status = PendingApproval
	default:
		out.Status = Cancelled
	}
	return out, nil
}

// Finish says the app delivered what t sold, so the store stops giving it:
// StoreKit's finish; Play's acknowledgement, or a consumable's
// consumption (which lets the user buy it again); the Microsoft Store's
// fulfillment of a consumable. A Pending one isn't finished: Observe gives
// it again once it's paid for. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func (t *Transaction) Finish() error {
	if t.State == Pending {
		return errors.New("purchase: a pending transaction can't be finished")
	}
	_, err := call("finish", spec.Finish{ID: t.ID, ProductID: t.ProductID, Token: t.token, Kind: t.Kind.String(),
		Consume: t.Kind == Consumable && t.State == Purchased, Quantity: t.Quantity})
	entitlementsChanged() // a non-renewing subscription's counts once finished
	return err
}

// Entitlements are transactions: what the user owns.
type Entitlements []*Transaction

// Has reports whether one of e is productID's.
func (e Entitlements) Has(productID string) bool {
	for _, t := range e {
		if t.ProductID == productID {
			return true
		}
	}
	return false
}

func transactions(b []byte) (Entitlements, error) {
	var ws []spec.Transaction
	if err := json.Unmarshal(b, &ws); err != nil {
		return nil, err
	}
	out := make(Entitlements, 0, len(ws))
	for i := range ws {
		t := transactionOf(&ws[i])
		if t.Kind == Consumable {
			continue // Play's unconsumed: Observe's
		}
		out = append(out, t)
	}
	return out, nil
}

// CurrentEntitlements is what the user owns now: non-consumables,
// subscriptions in their period (or grace period), non-renewing ones
// (Apple's: those the app finished; it says how long they last). Not on
// the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func CurrentEntitlements() (Entitlements, error) {
	b, err := call("entitlements", nil)
	if err != nil {
		return nil, err
	}
	return transactions(b)
}

// Restore asks the store for the user's purchases again, and answers the
// entitlements: iOS's AppStore.sync, which asks the user to sign in, so
// only from a button ("Restore purchases"). Unfinished transactions come
// through Observe. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Restore() (Entitlements, error) {
	b, err := call("restore", nil)
	if err != nil {
		return nil, err
	}
	entitlementsChanged()
	return transactions(b)
}

// SubscriptionState is where a subscription is.
type SubscriptionState uint8

const (
	Subscribed SubscriptionState = iota
	Expired
	// BillingRetry: the renewal's payment failed; the store retries
	// (Play's account hold). Not entitled.
	BillingRetry
	// GracePeriod: the renewal's payment failed; still entitled while the
	// store retries.
	GracePeriod
	SubscriptionRevoked
)

// SubscriptionStatus is a subscription's status, and its renewal's.
type SubscriptionStatus struct {
	ProductID string
	State     SubscriptionState
	// WillRenew: it renews at the period's end (RenewsTo: the product it
	// renews as, after an upgrade or a downgrade).
	WillRenew bool
	RenewsTo  string
	Renewal   time.Time
	// GraceExpires is when a GracePeriod ends.
	GraceExpires time.Time
	// ExpirationReason is why it expired (Apple's: "autoRenewDisabled",
	// "billingError", "didNotConsentToPriceIncrease",
	// "productUnavailable").
	ExpirationReason string
	// RenewalJWS is Apple's renewal info, signed.
	RenewalJWS string
	// Transaction is its latest.
	Transaction *Transaction
}

// SubscriptionStatuses are the statuses of group's subscriptions (Apple's
// subscription group ID; on Play, the subscription's product ID): a
// subscription shared with the family has one each. Not on the main
// thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func SubscriptionStatuses(group string) ([]SubscriptionStatus, error) {
	b, err := call("status", map[string]string{"group": group})
	if err != nil {
		return nil, err
	}
	var ws []spec.Status
	if err := json.Unmarshal(b, &ws); err != nil {
		return nil, err
	}
	out := make([]SubscriptionStatus, len(ws))
	for i, w := range ws {
		s := SubscriptionStatus{ProductID: w.ProductID, WillRenew: w.WillRenew, RenewsTo: w.RenewsTo,
			Renewal: msTime(w.RenewalMs), GraceExpires: msTime(w.GraceExpiresMs), ExpirationReason: w.ExpirationReason,
			RenewalJWS: w.RenewalJWS, Transaction: transactionOf(w.Transaction)}
		switch w.State {
		case "expired":
			s.State = Expired
		case "billingRetry":
			s.State = BillingRetry
		case "gracePeriod":
			s.State = GracePeriod
		case "revoked":
			s.State = SubscriptionRevoked
		}
		out[i] = s
	}
	return out, nil
}

// ManageSubscriptions shows the store's page of the user's subscriptions,
// to cancel or change one, and answers once it's closed: iOS's sheet
// (AppStore.showManageSubscriptions), Play's subscriptions center (at
// productID's, if not ""), the App Store's or the Microsoft Store's
// account page. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func ManageSubscriptions(productID string) error {
	_, err := call("manage", map[string]string{"productId": productID})
	return err
}

// RequestRefund shows Apple's refund request sheet for t, and reports
// whether the user asked for one (Apple decides later: Observe gives t
// Revoked if it refunds). Apple's alone: elsewhere, ErrNotAvailable (Play's
// refunds are the developer's, from Play Console or its server). Not on
// the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func RequestRefund(t *Transaction) (requested bool, err error) {
	b, err := call("refund", map[string]string{"id": t.ID})
	if err != nil {
		return false, err
	}
	var r struct {
		Requested bool `json:"requested"`
	}
	json.Unmarshal(b, &r)
	return r.Requested, nil
}

// RedeemCode shows the store's sheet for an offer code (Apple's, iOS 16
// and macOS 15), or Play's redeem page, and answers once it's closed. What
// it bought comes through Observe. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func RedeemCode() error {
	_, err := call("redeem", nil)
	return err
}

// Storefront is the store's country: what prices are in, what it sells.
type Storefront struct {
	// Country is ISO 3166-1's: alpha-3 on Apple ("USA"), alpha-2 on Play
	// and the Microsoft Store ("US").
	Country string
	// ID is Apple's.
	ID string
}

// CurrentStorefront is the user's storefront. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func CurrentStorefront() (Storefront, error) {
	b, err := call("storefront", nil)
	if err != nil {
		return Storefront{}, err
	}
	var w spec.Storefront
	if err := json.Unmarshal(b, &w); err != nil {
		return Storefront{}, err
	}
	return Storefront{Country: w.Country, ID: w.ID}, nil
}

// EventType is what an Event says.
type EventType uint8

const (
	// TransactionUpdated: a transaction outside a Buy (the package's
	// doc): Event.Transaction.
	TransactionUpdated EventType = iota
	// StorefrontChanged: the user's storefront changed (Event.Storefront):
	// ask for the products again, their prices are another country's.
	StorefrontChanged
)

// Event is something the store says.
type Event struct {
	Type        EventType
	Transaction *Transaction
	Storefront  Storefront
}

var (
	mu        sync.Mutex
	observers = map[*func(Event)]bool{}
	polling   bool
)

// Observe calls f, on the main thread, with what the store says (Event),
// until stop: transactions outside a Buy, storefront changes. What the
// store said before the first observer, from the app's start, is kept for
// it. Observe as the app starts, so purchases are delivered as they
// complete.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Observe(f func(Event)) (stop func()) {
	fp := &f
	mu.Lock()
	observers[fp] = true
	poll := !polling
	polling = true
	mu.Unlock()
	if poll {
		go follow()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(observers, fp)
			last := len(observers) == 0
			mu.Unlock()
			if last {
				ui.Post(func() { ui.Call(module, "stop", nil, nil) })
			}
		})
	}
}

// follow asks the host for each event ("next" answers with one), while
// someone observes.
func follow() {
	for {
		b, err := ui.CallWait(module, "next", nil)
		mu.Lock()
		fs := make([]func(Event), 0, len(observers))
		for f := range observers {
			fs = append(fs, *f)
		}
		// "stopped": the last observer went as another came: ask again.
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			polling = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		if err != nil {
			continue
		}
		var w spec.Event
		if json.Unmarshal(b, &w) != nil {
			continue
		}
		var e Event
		switch w.Type {
		case "transaction":
			if w.Transaction == nil {
				continue
			}
			e = Event{Type: TransactionUpdated, Transaction: transactionOf(w.Transaction)}
		case "storefront":
			if w.Storefront == nil {
				continue
			}
			e = Event{Type: StorefrontChanged, Storefront: Storefront{Country: w.Storefront.Country, ID: w.Storefront.ID}}
		default:
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f(e)
			}
		})
		if e.Type == TransactionUpdated {
			entitlementsChanged()
		}
	}
}

// users are Use's refreshes, which a Buy, a Finish and a Restore call
// (Observe calls them for the rest).
var (
	usersMu sync.Mutex
	users   = map[*func()]bool{}
)

// entitlementsChanged has each Use ask again, on the main thread.
func entitlementsChanged() {
	usersMu.Lock()
	fs := make([]func(), 0, len(users))
	for f := range users {
		fs = append(fs, *f)
	}
	usersMu.Unlock()
	if len(fs) > 0 {
		ui.Post(func() {
			for _, f := range fs {
				f()
			}
		})
	}
}

// Use is the user's entitlements as a signal, for a component: asked for
// as it mounts, and again after each Buy, Finish and Restore and each
// transaction the app's Observe gets, while the component lives (nil until
// the first answer; nil where there's no store). It doesn't observe: the
// transactions kept for the app's Observe stay its. Main thread.
func Use() signal.Accessor[Entitlements] {
	s := signal.NewFunc[Entitlements](nil, sameEntitlements)
	alive := true
	var gen int
	refresh := func() {
		gen++
		g := gen
		go func() {
			e, err := CurrentEntitlements()
			ui.Post(func() {
				if alive && g == gen && err == nil {
					s.Set(e)
				}
			})
		}()
	}
	refresh()
	again := func() {
		if alive {
			refresh()
		}
	}
	usersMu.Lock()
	users[&again] = true
	usersMu.Unlock()
	signal.OnCleanup(func() {
		alive = false
		usersMu.Lock()
		delete(users, &again)
		usersMu.Unlock()
	})
	return s
}

func sameEntitlements(a, b Entitlements) bool {
	if len(a) != len(b) || (a == nil) != (b == nil) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.ID != y.ID || x.ProductID != y.ProductID || x.State != y.State || !x.Expires.Equal(y.Expires) {
			return false
		}
	}
	return true
}

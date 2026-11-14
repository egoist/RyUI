// Package purchasespec is the "purchase" module's wire (asuka/sdk/purchase):
// JSON, the same from every host (StoreKit 2's on iOS and macOS, Play
// Billing's on Android, the Microsoft Store's on Windows, the Digital
// Goods API's on the web, host/fake's), so the Go hosts and the package
// share it. Times are milliseconds since 1970; prices, a display string
// and an amount in millionths of the currency's unit.
//
// The methods (args → answer):
//
//	available                      → [1] if the user can pay, [0] if not
//	products     {"items":[Item]}  → Products
//	buy          Buy               → Result
//	finish       Finish            → nothing
//	entitlements                   → [Transaction]
//	restore                        → [Transaction] (entitlements, after syncing with the store)
//	status       {"group": id}     → [Status]
//	manage       {"productId": id} → nothing (the store's subscriptions page, closed)
//	refund       {"id": txID}      → {"requested": bool}
//	redeem                         → nothing (the offer code sheet, closed)
//	storefront                     → Storefront
//	next                           → Event (one; "stopped" after stop)
//	stop                           → nothing
//
// A host with no store answers each with an error that starts "not
// available".
package purchasespec

// Kinds of product.
const (
	Consumable    = "consumable"
	NonConsumable = "nonConsumable"
	AutoRenewable = "autoRenewable"
	NonRenewing   = "nonRenewing"
	// InApp and Subs are a kind as Play knows it, when the app hasn't
	// said which of its kinds it is: a one-time product, a subscription.
	InApp = "inapp"
	Subs  = "subs"
)

// Item is a product the app sells: its store's ID, and its kind.
type Item struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// Period is a span of a subscription: Count Units ("day", "week",
// "month", "year").
type Period struct {
	Unit  string `json:"unit"`
	Count int    `json:"count"`
}

// Phase is one price of an offer: Cycles periods at Price, paid as Mode
// ("freeTrial", "payAsYouGo", "payUpFront", "recurring").
type Phase struct {
	Price    string `json:"price"`
	Micros   int64  `json:"micros"`
	Currency string `json:"currency,omitempty"`
	Period   Period `json:"period"`
	Cycles   int    `json:"cycles"`
	Mode     string `json:"mode"`
}

// Offer is a way to buy a subscription (or, on Play, a one-time product):
// Type "introductory", "promotional", "winBack", "code" (Apple's), "base"
// (Play's base plan), "offer" (Play's offer on a base plan), "oneTime"
// (Play's one-time product's purchase option).
type Offer struct {
	ID       string   `json:"id,omitempty"`
	Type     string   `json:"type"`
	Token    string   `json:"token,omitempty"`
	BasePlan string   `json:"basePlan,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Phases   []Phase  `json:"phases,omitempty"`
}

// Product is a product as the store sells it, in the user's storefront.
type Product struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Micros      int64  `json:"micros"`
	Currency    string `json:"currency,omitempty"`
	// Subscriptions: the group (Apple's subscription group; Play's
	// product), the level in it (1 the highest), the base period, the
	// offers, and whether the user can have the introductory one.
	Group         string  `json:"group,omitempty"`
	Level         int     `json:"level,omitempty"`
	Period        *Period `json:"period,omitempty"`
	Offers        []Offer `json:"offers,omitempty"`
	IntroEligible bool    `json:"introEligible,omitempty"`
	// FamilyShareable: Family Sharing shares it (Apple's).
	FamilyShareable bool `json:"familyShareable,omitempty"`
}

// Products answers "products": those the store has, and the IDs it
// doesn't.
type Products struct {
	Products []Product `json:"products"`
	Missing  []string  `json:"missing,omitempty"`
}

// Signature signs an Apple promotional offer, from the app's server: the
// key's ID, a nonce (a UUID), the signature (base64) and its time; or,
// iOS 18.4 and macOS 15.4, a JWS of the same.
type Signature struct {
	KeyID     string `json:"keyId,omitempty"`
	Nonce     string `json:"nonce,omitempty"`
	Signature string `json:"signature,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	JWS       string `json:"jws,omitempty"`
}

// Replace is a Play subscription the purchase replaces (an upgrade, a
// downgrade): its purchase token and product, and how ("withTimeProration",
// "chargeProratedPrice", "withoutProration", "chargeFullPrice",
// "deferred").
type Replace struct {
	Token     string `json:"token"`
	ProductID string `json:"productId"`
	Mode      string `json:"mode"`
}

// Buy is "buy"'s args.
type Buy struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Quantity     int        `json:"quantity,omitempty"`
	AccountToken string     `json:"accountToken,omitempty"`
	ProfileToken string     `json:"profileToken,omitempty"`
	OfferID      string     `json:"offerId,omitempty"`
	OfferType    string     `json:"offerType,omitempty"`
	OfferToken   string     `json:"offerToken,omitempty"`
	Signature    *Signature `json:"signature,omitempty"`
	Replace      *Replace   `json:"replace,omitempty"`
	Personalized bool       `json:"personalized,omitempty"`
}

// Transaction is a purchase: a product bought, renewed, restored, pending
// or revoked.
type Transaction struct {
	ID         string `json:"id"`
	OriginalID string `json:"originalId,omitempty"`
	ProductID  string `json:"productId"`
	Kind       string `json:"kind"`
	Quantity   int    `json:"quantity,omitempty"`
	// State: "purchased", "pending" or "revoked".
	State            string `json:"state"`
	DateMs           int64  `json:"dateMs,omitempty"`
	ExpiresMs        int64  `json:"expiresMs,omitempty"`
	RevokedMs        int64  `json:"revokedMs,omitempty"`
	RevocationReason string `json:"revocationReason,omitempty"`
	Upgraded         bool   `json:"upgraded,omitempty"`
	AutoRenews       bool   `json:"autoRenews,omitempty"`
	Acknowledged     bool   `json:"acknowledged,omitempty"`
	FamilyShared     bool   `json:"familyShared,omitempty"`
	AccountToken     string `json:"accountToken,omitempty"`
	ProfileToken     string `json:"profileToken,omitempty"`
	Environment      string `json:"environment,omitempty"`
	Storefront       string `json:"storefront,omitempty"`
	OfferID          string `json:"offerId,omitempty"`
	OfferType        string `json:"offerType,omitempty"`
	// What a server verifies: Apple's JWS; Play's purchase token, package
	// name, signed JSON and its signature; the Microsoft Store's ID.
	JWS          string `json:"jws,omitempty"`
	Token        string `json:"token,omitempty"`
	PackageName  string `json:"packageName,omitempty"`
	OriginalJSON string `json:"originalJson,omitempty"`
	Signature    string `json:"signature,omitempty"`
	// Verified: the store's signature checked on the device (StoreKit's).
	Verified bool `json:"verified,omitempty"`
}

// Result answers "buy": Status "purchased", "pending" or "cancelled".
type Result struct {
	Status      string       `json:"status"`
	Transaction *Transaction `json:"transaction,omitempty"`
}

// Finish is "finish"'s args: the transaction (its ID, its Play token),
// consumed or not.
type Finish struct {
	ID        string `json:"id"`
	ProductID string `json:"productId,omitempty"`
	Token     string `json:"token,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Consume   bool   `json:"consume,omitempty"`
	Quantity  int    `json:"quantity,omitempty"`
}

// Status is a subscription's: State "subscribed", "expired",
// "billingRetry", "gracePeriod" or "revoked".
type Status struct {
	ProductID        string       `json:"productId"`
	State            string       `json:"state"`
	WillRenew        bool         `json:"willRenew"`
	RenewsTo         string       `json:"renewsTo,omitempty"`
	RenewalMs        int64        `json:"renewalMs,omitempty"`
	GraceExpiresMs   int64        `json:"graceExpiresMs,omitempty"`
	ExpirationReason string       `json:"expirationReason,omitempty"`
	RenewalJWS       string       `json:"renewalJws,omitempty"`
	Transaction      *Transaction `json:"transaction,omitempty"`
}

// Storefront is the store's country (ISO 3166-1 alpha-3 on Apple, alpha-2
// on Play and the Microsoft Store), and its ID (Apple's).
type Storefront struct {
	Country string `json:"country"`
	ID      string `json:"id,omitempty"`
}

// Event answers "next": Type "transaction" (a purchase completed or
// renewed outside a Buy, a pending one approved, one revoked or
// refunded, one unfinished from before) or "storefront" (the user's
// changed).
type Event struct {
	Type        string       `json:"type"`
	Transaction *Transaction `json:"transaction,omitempty"`
	Storefront  *Storefront  `json:"storefront,omitempty"`
}

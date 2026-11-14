// Package pay takes payments with the platform's wallet: Apple Pay on
// iOS, macOS and in Safari, Google Pay on Android and in the other
// browsers (@stripe/stripe-react-native's Platform Pay,
// react-native-payments). It's for physical goods and services (a
// store's order, a ride, a ticket, a donation); digital goods and
// subscriptions to the app's own content are the stores' in-app
// purchases.
//
// The app shows the wallet's own button (Button) where Available says it
// can pay, and calls Pay with a Request: the amounts, the merchant's
// accounts and what to ask the user for (a shipping address, an email).
// The wallet's sheet shows; as the user changes the address, the
// shipping method or a coupon, the Request's handlers give the new
// totals. When the user pays, OnAuthorize gets the Payment: its Token is
// the wallet's payment token, for the app's processor (Stripe, Adyen,
// Braintree...: Google Pay's through the Request's gateway), which the
// app's server charges. OnAuthorize answers with the server's success or
// errors, which the sheet shows. Asuka never charges a card itself.
//
// Apple Pay needs a merchant ID (developer.apple.com: Certificates,
// Identifiers & Profiles, Merchant IDs), in asuka.json's
// "applePay": {"merchantIds": [...]}, for the app's entitlement
// (com.apple.developer.in-app-payments, given to an app that imports
// this package), and its payment processing certificate with the
// processor. Google Pay needs the processor's gateway ID and the
// merchant's ID from the Google Pay & Wallet Console for production
// (Google.Production); its test environment needs neither account.
//
// Windows and Linux have no wallet for apps: Available says so, Pay
// fails with ErrNotAvailable and Button is nothing.
package pay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	spec "github.com/egoist/asuka/internal/payspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Wallet is the platform's wallet.
type Wallet uint8

const (
	NoWallet  Wallet = iota // none: Windows, Linux, a browser with neither
	ApplePay                // iOS, macOS, Safari
	GooglePay               // Android, the other browsers
)

func (w Wallet) String() string {
	switch w {
	case ApplePay:
		return "Apple Pay"
	case GooglePay:
		return "Google Pay"
	}
	return "none"
}

func walletFrom(s string) Wallet {
	switch s {
	case "apple":
		return ApplePay
	case "google":
		return GooglePay
	}
	return NoWallet
}

// Network is a card network.
type Network string

const (
	Visa            Network = "visa"
	Mastercard      Network = "mastercard"
	Amex            Network = "amex"
	Discover        Network = "discover"
	JCB             Network = "jcb"
	Interac         Network = "interac"
	UnionPay        Network = "unionpay" // Apple Pay
	Maestro         Network = "maestro"  // Apple Pay; Google Pay in Brazil
	Electron        Network = "electron" // Apple Pay; Google Pay in Brazil
	Elo             Network = "elo"      // Apple Pay; Google Pay in Brazil
	CartesBancaires Network = "cartesBancaires"
	Eftpos          Network = "eftpos"
	Mada            Network = "mada"
	Mir             Network = "mir"
	Girocard        Network = "girocard"
	Bancontact      Network = "bancontact"
	Dankort         Network = "dankort"
	PrivateLabel    Network = "privateLabel" // a store's own card (Apple Pay)
)

// defaultNetworks are what a Request without Networks takes.
var defaultNetworks = []Network{Visa, Mastercard, Amex, Discover}

// googleNetworks are Google Pay's names for the networks it takes.
var googleNetworks = map[Network]string{
	Visa: "VISA", Mastercard: "MASTERCARD", Amex: "AMEX", Discover: "DISCOVER", JCB: "JCB",
	Interac: "INTERAC", Maestro: "MAESTRO", Electron: "ELECTRON", Elo: "ELO",
}

// Cards are kinds of cards, to accept only some.
type Cards uint8

const (
	Credit  Cards = 1 << iota // credit cards
	Debit                     // debit cards
	Prepaid                   // prepaid cards (Google Pay tells them apart; Apple Pay counts them as debit)
)

// CardKind is a card's funding.
type CardKind uint8

const (
	UnknownCard CardKind = iota
	CreditCard
	DebitCard
	PrepaidCard
	StoreCard  // a store's own (Apple Pay)
	EMoneyCard // a stored value card (Apple Pay)
)

// ItemKind is what a line is, for Google Pay's sheet (Apple Pay's shows
// every line alike).
type ItemKind uint8

const (
	LineItem ItemKind = iota
	Subtotal
	Tax
	Discount
	ShippingCost
)

// Item is a line of the sheet: a label and an amount.
type Item struct {
	Label string
	// Amount is a decimal in the currency's major unit, as "12.75" (at
	// most two decimals for Google Pay); negative for a discount.
	Amount string
	// Pending: the amount isn't known yet (a tip, a fare's estimate):
	// Apple Pay shows "Amount Pending"; Google Pay, an estimated total.
	Pending bool
	Kind    ItemKind
}

// ShippingType is how the order gets to the user, as Apple Pay words
// the address and the methods.
type ShippingType uint8

const (
	Shipping      ShippingType = iota // shipped to them
	Delivery                          // delivered (a meal)
	StorePickup                       // they pick it up at a store: its address can't be changed
	ServicePickup                     // they're picked up (a ride)
)

// ShippingMethod is a way to ship, with its cost.
type ShippingMethod struct {
	// ID names it in Payment.ShippingMethod and OnShippingMethod.
	ID     string
	Label  string
	Detail string // "Arrives in 5 to 7 days"
	// Amount is its cost: put it in the Request's Items too (the wallets
	// show it beside the method, not in the total).
	Amount string
	// From and To are when it arrives (Apple Pay, iOS 15; zero: unsaid).
	From, To time.Time
}

// Fields are what the app asks the user for, with an address.
type Fields uint8

const (
	Name         Fields = 1 << iota // their name
	Email                           // their email (Apple Pay: with the shipping contact)
	Phone                           // their phone number
	Address                         // a postal address
	PhoneticName                    // their name's reading (Apple Pay; Japan)
)

// Interval is a recurring payment's period.
type Interval uint8

const (
	Month Interval = iota
	Day
	Week
	Year
)

// Billing is a recurring or deferred payment's charge.
type Billing struct {
	Label  string
	Amount string
	// Pending: the amount isn't known yet (Google Pay: estimated).
	Pending bool
	// Start and End are the first and last charges (zero: now, and
	// until canceled).
	Start, End time.Time
	// Interval and Every are how often it's charged: every 1 Month by
	// default.
	Interval Interval
	Every    int
}

// Recurring makes the payment a subscription's: the wallet shows the
// charges to come, and its token is for them (a merchant token).
type Recurring struct {
	// Description says what it is, for the sheet and the user's wallet.
	Description string
	Regular     Billing
	// Trial is a period before the regular charges (Apple Pay: its
	// amount; Google Pay: an introductory period, from Start to End).
	Trial *Billing
	// ManagementURL is the page where the user changes or cancels it
	// (required).
	ManagementURL string
	// BillingAgreement is the terms the user agrees to (at most 300
	// characters for Google Pay).
	BillingAgreement string
	// TokenNotificationURL is the processor's or the app's server's URL
	// that hears of the token's changes (a new card).
	TokenNotificationURL string
}

// Deferred makes the payment a later one's (a hotel's stay, a
// preorder): authorized now, charged on Date.
type Deferred struct {
	Description          string
	Label                string
	Amount               string
	Pending              bool
	Date                 time.Time
	ManagementURL        string
	BillingAgreement     string
	TokenNotificationURL string
	// FreeCancellation is until when the user can cancel without a fee
	// (Apple Pay; zero: unsaid).
	FreeCancellation time.Time
}

// AutomaticReload makes the payment a stored balance's top up when it
// falls below Threshold (a transit card, a coffee shop's card).
type AutomaticReload struct {
	Description string
	Label       string
	// Amount is what each reload charges; Threshold, the balance it
	// reloads under.
	Amount, Threshold    string
	ManagementURL        string
	BillingAgreement     string
	TokenNotificationURL string
}

// Apple is the request's Apple Pay accounts.
type Apple struct {
	// MerchantID is the merchant identifier ("merchant.com.example.shop"),
	// one of asuka.json's applePay.merchantIds.
	MerchantID string
	// EMV also takes EMV payments (China UnionPay's cards).
	EMV bool
	// ApplicationData is the app's (an order's ID), signed into the token
	// as its hash.
	ApplicationData []byte
}

// Google is the request's Google Pay accounts.
type Google struct {
	// Production pays for real; else it's Google Pay's TEST environment,
	// which returns test tokens and needs no merchant ID.
	Production bool
	// MerchantID is the merchant's ID from the Google Pay & Wallet
	// Console (required in Production).
	MerchantID string
	// MerchantName is shown in the sheet (default: the Total's Label).
	MerchantName string
	// Gateway is the processor's gateway ID ("stripe", "adyen",
	// "braintree"...), and GatewayParams its parameters
	// ("gatewayMerchantId", or Stripe's "stripe:publishableKey" and
	// "stripe:version"), as the processor's documentation says.
	Gateway       string
	GatewayParams map[string]string
	// DirectKey, instead of a Gateway, has the token encrypted for the
	// app's own server (Google's ECv2 public key, base64): for a merchant
	// certified for PCI DSS.
	DirectKey string
	// DeviceTokensOnly takes only cards tokenized on the device
	// (CRYPTOGRAM_3DS), not those kept in the Google account (PAN_ONLY).
	DeviceTokensOnly bool
	// AssuranceDetails asks what Google verified of the card (Payment.Raw
	// has them).
	AssuranceDetails bool
	// TransactionID names this attempt, for Google's support.
	TransactionID string
	// PayNow labels the sheet's button "Pay now" (a final total).
	PayNow bool
	// TotalLabel labels the total among the lines (default "Total").
	TotalLabel string
}

// Request is a payment to ask the wallet for.
type Request struct {
	// Country is the merchant's (its acquirer's), ISO 3166 ("US");
	// Currency, ISO 4217 ("USD").
	Country, Currency string
	// Items are the lines above the total (subtotal, shipping, tax).
	Items []Item
	// Total is the amount charged. Its Label is the business's name, as
	// the user's statement shows it: Apple Pay shows "Pay <Label>".
	Total Item
	// Networks are the cards taken (default Visa, Mastercard, Amex,
	// Discover); Cards, their kinds (default all).
	Networks []Network
	Cards    Cards
	// CardCountries are the countries whose cards are taken (ISO 3166;
	// default any).
	CardCountries []string
	// Shipping and Billing are what's asked for the shipping and the
	// billing contacts. Google Pay's billing contact has a name and an
	// address; its shipping contact's email is the user's.
	Shipping, Billing Fields
	ShippingType      ShippingType
	// ShippingMethods are the ways to ship, the first chosen.
	ShippingMethods []ShippingMethod
	// ShippingCountries are where the app ships (Google Pay; ISO 3166;
	// default anywhere). Apple Pay's: answer OnShippingContact with
	// Unserviceable.
	ShippingCountries []string
	// Coupons lets the user enter a coupon code (Apple Pay, iOS 15), and
	// Coupon is one applied already.
	Coupons bool
	Coupon  string
	// At most one of Recurring, Deferred and AutomaticReload.
	Recurring       *Recurring
	Deferred        *Deferred
	AutomaticReload *AutomaticReload

	Apple  Apple
	Google Google

	// The handlers run on the goroutine that called Pay, in turn, while
	// the sheet waits (about 30 seconds at most): they may call the
	// app's server. An Update's nil fields are as they were; an error
	// (Invalid, or several in errors.Join) shows in the sheet, by its
	// fields.
	//
	// OnShippingContact: the user chose a shipping address. Until they
	// pay, the wallets give only its city, state, postal code and
	// country, enough for the shipping methods and the tax.
	OnShippingContact func(Contact) (Update, error)
	// OnShippingMethod: the user chose a shipping method.
	OnShippingMethod func(ShippingMethod) (Update, error)
	// OnPaymentMethod: the user chose a card (a surcharge for credit).
	OnPaymentMethod func(Card) (Update, error)
	// OnCoupon: the user entered a coupon code (Apple Pay). Its error
	// is Invalid(CouponInvalid, ...) or CouponExpired.
	OnCoupon func(code string) (Update, error)
	// OnAuthorize: the user paid. Send the Payment's Token to the app's
	// server, which charges it with the processor, and answer with its
	// error (nil: done), which the sheet shows. Required.
	OnAuthorize func(Payment) error
	// OnValidateMerchant (Apple Pay on the web): ask the app's server for
	// a merchant session from Apple's validation URL, with the merchant
	// ID's identity certificate, and answer with it as Apple gave it.
	// The site's domain must be registered for the merchant ID.
	OnValidateMerchant func(validationURL string) (session []byte, err error)
}

// Update is the sheet's new totals and methods.
type Update struct {
	// Items, nil: as they were ([]Item{}: none).
	Items []Item
	// Total, its Amount empty: as it was (its Label empty: the same).
	Total Item
	// ShippingMethods, nil: as they were; the one chosen stays chosen
	// if it's still there, else the first.
	ShippingMethods []ShippingMethod
}

// Contact is a shipping or billing contact.
type Contact struct {
	// Name is their whole name, as the locale writes it; GivenName and
	// FamilyName are its parts (Apple Pay).
	Name, GivenName, FamilyName string
	Email, Phone                string
	Address                     PostalAddress
}

// PostalAddress is a postal address.
type PostalAddress struct {
	// Lines are the street's.
	Lines []string
	// City is the locality, SubLocality a part of it (a district).
	City, SubLocality string
	// State is the region (a state, a province), SubAdministrativeArea a
	// part of it (a county).
	State, SubAdministrativeArea string
	PostalCode                   string
	// CountryCode is ISO 3166 ("US"); Country, its name (Apple Pay).
	CountryCode, Country string
	// SortingCode is Google Pay's (a CEDEX).
	SortingCode string
}

// Card is the card the user pays with.
type Card struct {
	Network Network
	Kind    CardKind
	// Name is how the wallet shows it ("Visa 1234"), Last4 its last
	// digits.
	Name, Last4 string
	// Billing is its billing contact, when the user chooses it (for a
	// tax by the billing address).
	Billing Contact
}

// Payment is what the user authorized.
type Payment struct {
	Wallet Wallet
	// Token is the payment token, for the processor: Apple Pay's
	// paymentData (a JSON object, as Stripe's and Braintree's SDKs send
	// it), Google Pay's tokenizationData.token (the gateway's token, or
	// the ECv2 message for DirectKey).
	Token string
	// TransactionID is Apple Pay's transaction identifier.
	TransactionID string
	Card          Card
	// Shipping and Billing are the contacts asked for (Shipping's Email
	// and Phone also when asked for with Billing).
	Shipping, Billing Contact
	// ShippingMethod is the chosen method's ID.
	ShippingMethod string
	// Raw is Google Pay's PaymentData, as JSON (assurance details,
	// anything else it has).
	Raw []byte
}

// Field is what a FieldError is about.
type Field uint8

const (
	// PaymentField: the payment itself (a declined card).
	PaymentField Field = iota
	ShippingAddress
	ShippingStreet
	ShippingCity
	ShippingState
	ShippingPostalCode
	ShippingCountry
	ShippingName
	ShippingEmail
	ShippingPhone
	// Unserviceable: no shipping to the address.
	Unserviceable
	ShippingMethodField
	BillingAddress
	BillingStreet
	BillingCity
	BillingState
	BillingPostalCode
	BillingCountry
	BillingName
	CouponInvalid
	CouponExpired
)

var fieldNames = [...]string{
	"payment", "shippingAddress", "shippingStreet", "shippingCity", "shippingState",
	"shippingPostalCode", "shippingCountry", "shippingName", "shippingEmail", "shippingPhone",
	"unserviceable", "shippingMethod", "billingAddress", "billingStreet", "billingCity",
	"billingState", "billingPostalCode", "billingCountry", "billingName", "couponInvalid", "couponExpired",
}

func (f Field) String() string {
	if int(f) < len(fieldNames) {
		return fieldNames[f]
	}
	return "payment"
}

// FieldError is an error the sheet shows by a field: Apple Pay marks
// the field; Google Pay says it in a dialog.
type FieldError struct {
	Field Field
	// Message is shown to the user: write it for them, in their
	// language.
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Invalid is a FieldError.
func Invalid(f Field, message string) error { return &FieldError{Field: f, Message: message} }

// Errors.
var (
	// ErrNotAvailable: no wallet here, or it can't pay (see Available).
	ErrNotAvailable = errors.New("pay: not available")
	// ErrCanceled: the user closed the sheet before paying.
	ErrCanceled = errors.New("pay: canceled")
	// ErrBusy: a sheet shows already.
	ErrBusy = errors.New("pay: a payment sheet shows already")
)

const module = "pay"

func call(method string, args []byte) ([]byte, error) {
	b, err := ui.CallWait(module, method, args)
	if err != nil {
		s := err.Error()
		switch {
		case strings.HasPrefix(s, "not available"), strings.HasPrefix(s, "no module"),
			strings.HasSuffix(s, "isn't on the web"):
			return nil, ErrNotAvailable
		case s == "busy":
			return nil, ErrBusy
		}
	}
	return b, err
}

// Availability is what the platform's wallet can do.
type Availability struct {
	Wallet Wallet
	// CanPay: the wallet takes payments here (Apple Pay's
	// canMakePayments, Google Pay's isReadyToPay): show its Button.
	CanPay bool
	// HasCard: the user has a card the Request takes. Without one, Apple
	// Pay's guidelines show a SetUp button (or the Buy button, whose
	// sheet adds a card); Google Pay's sheet adds one.
	HasCard bool
}

// Available reports what the wallet can do for r (its networks, cards
// and accounts; its amounts don't matter). Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Available(r Request) Availability {
	p, err := r.present("")
	if err != nil {
		return Availability{}
	}
	args, _ := json.Marshal(p)
	b, err := call("available", args)
	if err != nil {
		return Availability{}
	}
	var a spec.Availability
	if json.Unmarshal(b, &a) != nil {
		return Availability{}
	}
	return Availability{Wallet: walletFrom(a.Wallet), CanPay: a.CanPay, HasCard: a.CanPay && a.HasCard}
}

// SetUp opens Apple Pay's screen to add a card (for a SetUp button), and
// returns as it shows. Google Pay has none (its sheet adds a card):
// ErrNotAvailable. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func SetUp() error {
	_, err := call("setUp", nil)
	return err
}

// Pay shows the wallet's sheet for r, runs r's handlers as the user
// changes it, and returns the Payment OnAuthorize accepted, once the
// sheet closes: ErrCanceled if the user closed it first. Not on the
// main thread. One sheet shows at a time: ErrBusy while one does.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Pay(r Request) (Payment, error) {
	if r.OnAuthorize == nil {
		return Payment{}, errors.New("pay: Request.OnAuthorize is nil")
	}
	s := &session{r: r, id: newID(), items: r.Items, total: r.Total, methods: r.ShippingMethods}
	if len(r.ShippingMethods) > 0 {
		s.selected = r.ShippingMethods[0].ID
	}
	p, err := r.present(s.id)
	if err != nil {
		return Payment{}, err
	}
	args, _ := json.Marshal(p)
	if _, err := call("present", args); err != nil {
		return Payment{}, err
	}
	return s.run()
}

func newID() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var amountRE = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// check reports what's wrong with r for any wallet.
func (r *Request) check() error {
	if len(r.Country) != 2 {
		return fmt.Errorf("pay: Request.Country %q isn't an ISO 3166 code", r.Country)
	}
	if len(r.Currency) != 3 {
		return fmt.Errorf("pay: Request.Currency %q isn't an ISO 4217 code", r.Currency)
	}
	n := 0
	for _, x := range []bool{r.Recurring != nil, r.Deferred != nil, r.AutomaticReload != nil} {
		if x {
			n++
		}
	}
	if n > 1 {
		return errors.New("pay: at most one of Request.Recurring, Deferred and AutomaticReload")
	}
	amounts := []string{r.Total.Amount}
	for _, it := range r.Items {
		amounts = append(amounts, it.Amount)
	}
	for _, m := range r.ShippingMethods {
		amounts = append(amounts, m.Amount)
	}
	for _, a := range amounts {
		if !amountRE.MatchString(a) {
			return fmt.Errorf("pay: %q isn't an amount (a decimal, as \"12.75\")", a)
		}
	}
	return nil
}

// present is r as the hosts take it.
func (r *Request) present(id string) (spec.Present, error) {
	if err := r.check(); err != nil {
		return spec.Present{}, err
	}
	p := spec.Present{ID: id, Apple: r.apple(nil, nil, nil), GoogleEnv: "TEST",
		Validate: r.OnValidateMerchant != nil}
	if r.Google.Production {
		p.GoogleEnv = "PRODUCTION"
	}
	g, err := r.google()
	if err != nil {
		p.GoogleError = err.Error()
	} else {
		p.Google, _ = json.Marshal(g)
	}
	p.GoogleReady, _ = json.Marshal(r.googleReady())
	if r.Apple.MerchantID == "" {
		p.AppleError = "pay: Request.Apple.MerchantID is empty (one of asuka.json's applePay.merchantIds)"
	}
	return p, nil
}

func (r *Request) networks() []Network {
	if len(r.Networks) > 0 {
		return r.Networks
	}
	return defaultNetworks
}

// Apple Pay's request.

func appleItem(it Item) spec.AppleItem {
	return spec.AppleItem{Label: it.Label, Amount: it.Amount, Pending: it.Pending}
}

var intervalNames = [...]string{"month", "day", "week", "year"}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func (b Billing) apple() spec.AppleItem {
	every := b.Every
	if every <= 0 {
		every = 1
	}
	return spec.AppleItem{Label: b.Label, Amount: b.Amount, Pending: b.Pending, Kind: "recurring",
		StartMs: ms(b.Start), EndMs: ms(b.End), Interval: intervalNames[b.Interval%4], Every: every}
}

func fieldNamesOf(f Fields) []string {
	var out []string
	for i, n := range []string{"name", "email", "phone", "address", "phoneticName"} {
		if f&(1<<i) != 0 {
			out = append(out, n)
		}
	}
	return out
}

func appleMethods(ms []ShippingMethod) []spec.AppleShippingMethod {
	out := make([]spec.AppleShippingMethod, 0, len(ms))
	for _, m := range ms {
		x := spec.AppleShippingMethod{ID: m.ID, Label: m.Label, Detail: m.Detail, Amount: m.Amount}
		if !m.From.IsZero() {
			x.From = m.From.Format(time.DateOnly)
			x.To = x.From
		}
		if !m.To.IsZero() {
			x.To = m.To.Format(time.DateOnly)
			if x.From == "" {
				x.From = x.To
			}
		}
		out = append(out, x)
	}
	return out
}

// appleItems are the summary items: the lines, a subscription's or a
// later payment's own, the total last.
func (r *Request) appleItems(items []Item, total Item) []spec.AppleItem {
	out := make([]spec.AppleItem, 0, len(items)+3)
	for _, it := range items {
		out = append(out, appleItem(it))
	}
	switch {
	case r.Recurring != nil:
		if t := r.Recurring.Trial; t != nil {
			out = append(out, t.apple())
		}
		out = append(out, r.Recurring.Regular.apple())
	case r.Deferred != nil:
		out = append(out, r.Deferred.apple())
	case r.AutomaticReload != nil:
		out = append(out, r.AutomaticReload.apple())
	}
	return append(out, appleItem(total))
}

func (d *Deferred) apple() spec.AppleItem {
	return spec.AppleItem{Label: d.Label, Amount: d.Amount, Pending: d.Pending, Kind: "deferred", DateMs: ms(d.Date)}
}

func (a *AutomaticReload) apple() spec.AppleItem {
	return spec.AppleItem{Label: a.Label, Amount: a.Amount, Kind: "reload", Threshold: a.Threshold}
}

// apple is the request as Apple Pay takes it (items, total and methods:
// as they are now, nil: the Request's).
func (r *Request) apple(items []Item, total *Item, methods []ShippingMethod) spec.AppleRequest {
	if items == nil {
		items = r.Items
	}
	if total == nil {
		total = &r.Total
	}
	if methods == nil {
		methods = r.ShippingMethods
	}
	// Apple Pay's billing contact has a name and an address: its email
	// and phone come with the shipping contact.
	shipping, billing := r.Shipping|r.Billing&(Email|Phone), r.Billing&^(Email|Phone)
	a := spec.AppleRequest{
		Merchant: r.Apple.MerchantID, Country: r.Country, Currency: r.Currency,
		CardCountries: r.CardCountries, Items: r.appleItems(items, *total),
		ShippingFields: fieldNamesOf(shipping), BillingFields: fieldNamesOf(billing),
		ShippingType:    [...]string{"shipping", "delivery", "storePickup", "servicePickup"}[r.ShippingType%4],
		ShippingMethods: appleMethods(methods), ShippingFixed: r.ShippingType == StorePickup,
		Coupons: r.Coupons || r.OnCoupon != nil, Coupon: r.Coupon,
		Events: spec.AppleEventsOn{
			ShippingContact: r.OnShippingContact != nil, ShippingMethod: r.OnShippingMethod != nil,
			PaymentMethod: r.OnPaymentMethod != nil, Coupon: r.OnCoupon != nil,
		},
	}
	for _, n := range r.networks() {
		a.Networks = append(a.Networks, string(n))
	}
	a.Capabilities = []string{"3DS"}
	if r.Apple.EMV {
		a.Capabilities = append(a.Capabilities, "EMV")
	}
	if r.Cards != 0 {
		if r.Cards&Credit != 0 {
			a.Capabilities = append(a.Capabilities, "credit")
		}
		if r.Cards&(Debit|Prepaid) != 0 {
			a.Capabilities = append(a.Capabilities, "debit")
		}
	}
	if len(r.Apple.ApplicationData) > 0 {
		a.ApplicationData = base64.StdEncoding.EncodeToString(r.Apple.ApplicationData)
	}
	if x := r.Recurring; x != nil {
		a.Recurring = &spec.AppleRecurring{Description: x.Description, Regular: x.Regular.apple(),
			ManagementURL: x.ManagementURL, BillingAgreement: x.BillingAgreement, TokenNotificationURL: x.TokenNotificationURL}
		if x.Trial != nil {
			t := x.Trial.apple()
			a.Recurring.Trial = &t
		}
	}
	if x := r.Deferred; x != nil {
		a.Deferred = &spec.AppleDeferred{Description: x.Description, Billing: x.apple(), ManagementURL: x.ManagementURL,
			BillingAgreement: x.BillingAgreement, TokenNotificationURL: x.TokenNotificationURL, FreeCancellationMs: ms(x.FreeCancellation)}
	}
	if x := r.AutomaticReload; x != nil {
		a.Reload = &spec.AppleReload{Description: x.Description, Billing: x.apple(), ManagementURL: x.ManagementURL,
			BillingAgreement: x.BillingAgreement, TokenNotificationURL: x.TokenNotificationURL}
	}
	return a
}

// Google Pay's request.

func (r *Request) googleCard() spec.GoogleCardParameters {
	c := spec.GoogleCardParameters{AllowedAuthMethods: []string{"PAN_ONLY", "CRYPTOGRAM_3DS"}}
	if r.Google.DeviceTokensOnly {
		c.AllowedAuthMethods = []string{"CRYPTOGRAM_3DS"}
	}
	for _, n := range r.networks() {
		if g := googleNetworks[n]; g != "" {
			c.AllowedCardNetworks = append(c.AllowedCardNetworks, g)
		}
	}
	if r.Cards != 0 {
		no := false
		if r.Cards&Credit == 0 {
			c.AllowCreditCards = &no
		}
		if r.Cards&Prepaid == 0 {
			c.AllowPrepaidCards = &no
		}
	}
	c.AllowedIssuerCountryCodes = r.CardCountries
	c.AssuranceDetailsRequired = r.Google.AssuranceDetails
	if r.Billing&(Name|Address|Phone) != 0 {
		c.BillingAddressRequired = true
		c.BillingAddressParameters = &spec.GoogleBillingAddress{Format: "MIN", PhoneNumberRequired: r.Billing&Phone != 0}
		if r.Billing&Address != 0 {
			c.BillingAddressParameters.Format = "FULL"
		}
	}
	return c
}

func (r *Request) googleReady() spec.GoogleReady {
	return spec.GoogleReady{APIVersion: 2, AllowedPaymentMethods: []spec.GoogleMethod{{Type: "CARD", Parameters: r.googleCard()}},
		ExistingPaymentMethodRequired: true}
}

var googleItemTypes = [...]string{"LINE_ITEM", "SUBTOTAL", "TAX", "DISCOUNT", "SHIPPING_OPTION"}

func googleItems(items []Item) []spec.GoogleItem {
	var out []spec.GoogleItem
	for _, it := range items {
		g := spec.GoogleItem{Label: it.Label, Type: googleItemTypes[it.Kind%5], Price: it.Amount, Status: "FINAL"}
		if it.Pending {
			g.Status = "PENDING"
		}
		out = append(out, g)
	}
	return out
}

func priceStatus(pending bool, amount string) string {
	switch {
	case amount == "":
		return "NOT_CURRENTLY_KNOWN"
	case pending:
		return "ESTIMATED"
	}
	return "FINAL"
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// googleTransaction is the transaction info for the items and total.
func (r *Request) googleTransaction(items []Item, total Item) *spec.GoogleTransaction {
	t := &spec.GoogleTransaction{CurrencyCode: r.Currency, CountryCode: r.Country, TransactionID: r.Google.TransactionID,
		TotalPriceStatus: "FINAL", TotalPrice: total.Amount, DisplayItems: googleItems(items), CheckoutOption: "DEFAULT"}
	if total.Pending {
		t.TotalPriceStatus = "ESTIMATED"
	} else if r.Google.PayNow {
		t.CheckoutOption = "COMPLETE_IMMEDIATE_PURCHASE"
	}
	if len(t.DisplayItems) > 0 {
		t.TotalPriceLabel = r.Google.TotalLabel
		if t.TotalPriceLabel == "" {
			t.TotalPriceLabel = "Total"
		}
	}
	return t
}

func googleOptions(ms []ShippingMethod, selected string) *spec.GoogleShippingOptions {
	o := &spec.GoogleShippingOptions{DefaultSelectedOptionID: selected}
	for _, m := range ms {
		d := m.Detail
		o.ShippingOptions = append(o.ShippingOptions, spec.GoogleOption{ID: m.ID, Label: m.Label, Description: d})
	}
	return o
}

// google is the request as Google Pay takes it (a PaymentDataRequest).
func (r *Request) google() (*spec.GoogleRequest, error) {
	m := spec.GoogleMethod{Type: "CARD", Parameters: r.googleCard()}
	switch {
	case r.Google.Gateway != "":
		params := map[string]string{"gateway": r.Google.Gateway}
		for k, v := range r.Google.GatewayParams {
			params[k] = v
		}
		m.TokenizationSpecification = &spec.GoogleTokenization{Type: "PAYMENT_GATEWAY", Parameters: params}
	case r.Google.DirectKey != "":
		m.TokenizationSpecification = &spec.GoogleTokenization{Type: "DIRECT",
			Parameters: map[string]string{"protocolVersion": "ECv2", "publicKey": r.Google.DirectKey}}
	default:
		return nil, errors.New("pay: Request.Google has no Gateway (or DirectKey): the processor's gateway that tokenizes the card")
	}
	if len(m.Parameters.AllowedCardNetworks) == 0 {
		return nil, errors.New("pay: Google Pay takes none of Request.Networks")
	}
	if r.Google.Production && r.Google.MerchantID == "" {
		return nil, errors.New("pay: Request.Google.MerchantID is empty (the Google Pay & Wallet Console's), which Production needs")
	}
	g := &spec.GoogleRequest{APIVersion: 2, AllowedPaymentMethods: []spec.GoogleMethod{m},
		MerchantInfo:    &spec.GoogleMerchant{MerchantID: r.Google.MerchantID, MerchantName: r.Google.MerchantName},
		CallbackIntents: []string{"PAYMENT_AUTHORIZATION"},
		EmailRequired:   (r.Shipping|r.Billing)&Email != 0,
	}
	if g.MerchantInfo.MerchantName == "" {
		g.MerchantInfo.MerchantName = r.Total.Label
	}
	if r.Shipping&(Address|Name|Phone) != 0 || len(r.ShippingMethods) > 0 {
		g.ShippingAddressRequired = true
		g.ShippingAddressParameters = &spec.GoogleShippingAddress{AllowedCountryCodes: r.ShippingCountries,
			PhoneNumberRequired: r.Shipping&Phone != 0}
		if r.OnShippingContact != nil {
			g.CallbackIntents = append(g.CallbackIntents, "SHIPPING_ADDRESS")
		}
	}
	if len(r.ShippingMethods) > 0 {
		g.ShippingOptionRequired = true
		g.ShippingOptionParameters = googleOptions(r.ShippingMethods, r.ShippingMethods[0].ID)
		if r.OnShippingMethod != nil {
			g.CallbackIntents = append(g.CallbackIntents, "SHIPPING_OPTION")
		}
	}
	if r.OnPaymentMethod != nil {
		g.CallbackIntents = append(g.CallbackIntents, "PAYMENT_METHOD")
	}
	switch {
	case r.Recurring != nil:
		x := r.Recurring
		g.RecurringTransactionInfo = &spec.GoogleRecurring{CurrencyCode: r.Currency, CountryCode: r.Country,
			TransactionID: r.Google.TransactionID, TokenUpdateURL: x.TokenNotificationURL, ManagementURL: x.ManagementURL,
			BillingAgreement: x.BillingAgreement, ImmediateTotalPrice: r.Total.Amount, ImmediateDisplayItems: googleItems(r.Items)}
		if t := x.Trial; t != nil {
			price := t.Amount
			if price == "" {
				price = "0.00"
			}
			g.RecurringTransactionInfo.IntroductoryPeriod = &spec.GoogleIntroductory{Start: rfc3339(t.Start), End: rfc3339(t.End),
				Label: t.Label, TotalPrice: price}
		}
		every := x.Regular.Every
		if every <= 0 {
			every = 1
		}
		g.RecurringTransactionInfo.RecurrenceItems = []spec.GoogleRecurrenceItem{{
			Start: rfc3339(x.Regular.Start), End: rfc3339(x.Regular.End), Label: x.Regular.Label, Price: x.Regular.Amount,
			PriceStatus: priceStatus(x.Regular.Pending, x.Regular.Amount),
			Period:      strings.ToUpper(intervalNames[x.Regular.Interval%4]), PeriodCount: every,
		}}
	case r.Deferred != nil:
		x := r.Deferred
		g.DeferredTransactionInfo = &spec.GoogleDeferred{CurrencyCode: r.Currency, CountryCode: r.Country,
			TransactionID: r.Google.TransactionID, TokenUpdateURL: x.TokenNotificationURL, ManagementURL: x.ManagementURL,
			BillingAgreement: x.BillingAgreement, ImmediateTotalPrice: r.Total.Amount, ImmediateDisplayItems: googleItems(r.Items),
			BillingDateTime: rfc3339(x.Date), PriceStatus: priceStatus(x.Pending, x.Amount), Price: x.Amount, Label: x.Label}
	case r.AutomaticReload != nil:
		x := r.AutomaticReload
		g.AutomaticReloadTransactionInfo = &spec.GoogleReload{CurrencyCode: r.Currency, CountryCode: r.Country,
			TransactionID: r.Google.TransactionID, TokenUpdateURL: x.TokenNotificationURL, ManagementURL: x.ManagementURL,
			BillingAgreement: x.BillingAgreement, ImmediateTotalPrice: r.Total.Amount, ImmediateDisplayItems: googleItems(r.Items),
			MinimumBalanceAmount: x.Threshold, ReloadAmount: x.Amount, Label: x.Label}
	default:
		g.TransactionInfo = r.googleTransaction(r.Items, r.Total)
	}
	return g, nil
}

// The sheet's session.

type session struct {
	r        Request
	id       string
	items    []Item
	total    Item
	methods  []ShippingMethod
	selected string
	paid     *Payment
}

func (s *session) run() (Payment, error) {
	for {
		b, err := call("next", []byte(s.id))
		if err != nil {
			if s.paid != nil {
				return *s.paid, nil
			}
			return Payment{}, err
		}
		var e spec.Event
		if json.Unmarshal(b, &e) != nil {
			continue
		}
		if e.Type == spec.Done {
			if s.paid != nil {
				return *s.paid, nil
			}
			if e.Error != "" {
				return Payment{}, errors.New("pay: " + e.Error)
			}
			return Payment{}, ErrCanceled
		}
		a := s.handle(e)
		a.ID, a.Seq = s.id, e.Seq
		args, _ := json.Marshal(a)
		ui.Post(func() { ui.Call(module, "answer", args, nil) })
	}
}

// apply keeps an update's totals and methods.
func (s *session) apply(u Update) {
	if u.Items != nil {
		s.items = u.Items
	}
	if u.Total.Amount != "" {
		if u.Total.Label == "" {
			u.Total.Label = s.total.Label
		}
		s.total = u.Total
	}
	if u.ShippingMethods != nil {
		s.methods = u.ShippingMethods
		found := false
		for _, m := range s.methods {
			found = found || m.ID == s.selected
		}
		if !found {
			s.selected = ""
			if len(s.methods) > 0 {
				s.selected = s.methods[0].ID
			}
		}
	}
}

// fieldErrors are err's FieldErrors (errors.Join's each); an error that
// isn't one is about def.
func fieldErrors(err error, def Field) []spec.FieldError {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		var out []spec.FieldError
		for _, e := range j.Unwrap() {
			out = append(out, fieldErrors(e, def)...)
		}
		return out
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		return []spec.FieldError{{Field: fe.Field.String(), Message: fe.Message}}
	}
	return []spec.FieldError{{Field: def.String(), Message: err.Error()}}
}

// update runs a handler and keeps its update.
func (s *session) update(f func() (Update, error), def Field) []spec.FieldError {
	u, err := f()
	s.apply(u)
	return fieldErrors(err, def)
}

func (s *session) handle(e spec.Event) spec.Answer {
	r := &s.r
	var a spec.Answer
	switch e.Type {
	case spec.ShippingContact:
		if r.OnShippingContact != nil {
			c := contactFrom(e.Contact)
			a.Errors = s.update(func() (Update, error) { return r.OnShippingContact(c) }, ShippingAddress)
		}
	case spec.ShippingMethod:
		s.selected = e.Method
		if r.OnShippingMethod != nil {
			m := s.method(e.Method)
			a.Errors = s.update(func() (Update, error) { return r.OnShippingMethod(m) }, ShippingMethodField)
		}
	case spec.PaymentMethod:
		if r.OnPaymentMethod != nil {
			c := cardFrom(e.Card)
			a.Errors = s.update(func() (Update, error) { return r.OnPaymentMethod(c) }, PaymentField)
		}
	case spec.Coupon:
		if r.OnCoupon != nil {
			code := e.Coupon
			a.Errors = s.update(func() (Update, error) { return r.OnCoupon(code) }, CouponInvalid)
		}
	case spec.Validate:
		if r.OnValidateMerchant == nil {
			a.Errors = []spec.FieldError{{Field: "payment", Message: "pay: Request.OnValidateMerchant is nil (Apple Pay on the web needs it)"}}
			break
		}
		session, err := r.OnValidateMerchant(e.URL)
		if err == nil && !json.Valid(session) {
			err = errors.New("pay: the merchant session isn't JSON")
		}
		if err != nil {
			a.Errors = fieldErrors(err, PaymentField)
		} else {
			a.Session = session
		}
		return a
	case spec.Authorize:
		p := applePayment(e.Payment)
		if err := r.OnAuthorize(p); err != nil {
			a.Errors = fieldErrors(err, PaymentField)
			if len(a.Errors) == 0 {
				a.Errors = []spec.FieldError{{Field: "payment", Message: err.Error()}}
			}
		} else {
			s.paid = &p
		}
		return a
	case spec.Google:
		return s.googleChanged(e.Google)
	case spec.GoogleAuthorize:
		return s.googleAuthorize(e.Google)
	}
	a.Items = r.appleItems(s.items, s.total)
	a.ShippingMethods = appleMethods(s.methods)
	return a
}

func (s *session) method(id string) ShippingMethod {
	for _, m := range s.methods {
		if m.ID == id {
			return m
		}
	}
	return ShippingMethod{ID: id}
}

// googleReasons are Google Pay's error reasons and intents for a field.
func googleError(errs []spec.FieldError, intent string) *spec.GoogleError {
	if len(errs) == 0 {
		return nil
	}
	e := errs[0]
	g := &spec.GoogleError{Message: e.Message, Reason: "OTHER_ERROR", Intent: intent}
	switch {
	case e.Field == "unserviceable":
		g.Reason, g.Intent = "SHIPPING_ADDRESS_UNSERVICEABLE", "SHIPPING_ADDRESS"
	case strings.HasPrefix(e.Field, "shipping") && e.Field != "shippingMethod":
		g.Reason, g.Intent = "SHIPPING_ADDRESS_INVALID", "SHIPPING_ADDRESS"
	case e.Field == "shippingMethod":
		g.Reason, g.Intent = "SHIPPING_OPTION_INVALID", "SHIPPING_OPTION"
	case e.Field == "payment" || strings.HasPrefix(e.Field, "billing"):
		g.Reason = "PAYMENT_DATA_INVALID"
		if intent == "PAYMENT_METHOD" || intent == "PAYMENT_AUTHORIZATION" {
			g.Intent = intent
		} else {
			g.Intent = "PAYMENT_AUTHORIZATION"
		}
	}
	return g
}

// googleChanged answers IntermediatePaymentData with a
// PaymentDataRequestUpdate.
func (s *session) googleChanged(data []byte) spec.Answer {
	r := &s.r
	var d spec.GoogleIntermediate
	json.Unmarshal(data, &d)
	if d.ShippingOptionData != nil && d.ShippingOptionData.ID != "" && d.ShippingOptionData.ID != "shipping_option_unselected" {
		s.selected = d.ShippingOptionData.ID
	}
	var errs []spec.FieldError
	intent := d.CallbackTrigger
	switch d.CallbackTrigger {
	case "INITIALIZE", "SHIPPING_ADDRESS":
		if r.OnShippingContact != nil && d.ShippingAddress != nil {
			c := googleContact(d.ShippingAddress, "")
			errs = s.update(func() (Update, error) { return r.OnShippingContact(c) }, ShippingAddress)
		}
		intent = "SHIPPING_ADDRESS"
	case "SHIPPING_OPTION":
		if r.OnShippingMethod != nil {
			m := s.method(s.selected)
			errs = s.update(func() (Update, error) { return r.OnShippingMethod(m) }, ShippingMethodField)
		}
	case "PAYMENT_METHOD":
		if r.OnPaymentMethod != nil && d.PaymentMethodData != nil {
			c := googleCard(d.PaymentMethodData)
			errs = s.update(func() (Update, error) { return r.OnPaymentMethod(c) }, PaymentField)
		}
	}
	u := spec.GoogleUpdate{Error: googleError(errs, intent)}
	if r.Recurring == nil && r.Deferred == nil && r.AutomaticReload == nil {
		u.NewTransactionInfo = r.googleTransaction(s.items, s.total)
	}
	if len(s.methods) > 0 {
		u.NewShippingOptionParameters = googleOptions(s.methods, s.selected)
	}
	b, _ := json.Marshal(u)
	return spec.Answer{Google: b}
}

// googleAuthorize answers PaymentData with a PaymentAuthorizationResult.
func (s *session) googleAuthorize(data []byte) spec.Answer {
	var d spec.GooglePaymentData
	res := spec.GoogleResult{TransactionState: "SUCCESS"}
	if err := json.Unmarshal(data, &d); err != nil {
		res = spec.GoogleResult{TransactionState: "ERROR", Error: &spec.GoogleError{Reason: "OTHER_ERROR", Message: err.Error(), Intent: "PAYMENT_AUTHORIZATION"}}
	} else {
		p := googlePayment(d, data)
		if err := s.r.OnAuthorize(p); err != nil {
			res = spec.GoogleResult{TransactionState: "ERROR", Error: googleError(fieldErrors(err, PaymentField), "PAYMENT_AUTHORIZATION")}
		} else {
			s.paid = &p
		}
	}
	b, _ := json.Marshal(res)
	return spec.Answer{Google: b}
}

// The wallets' answers.

func contactFrom(c *spec.Contact) Contact {
	if c == nil {
		return Contact{}
	}
	out := Contact{Name: c.Name, GivenName: c.GivenName, FamilyName: c.FamilyName, Email: c.Email, Phone: c.Phone}
	if out.Name == "" {
		out.Name = strings.TrimSpace(c.GivenName + " " + c.FamilyName)
	}
	if a := c.Address; a != nil {
		out.Address = PostalAddress{City: a.City, SubLocality: a.SubLocality, State: a.State,
			SubAdministrativeArea: a.SubAdministrativeArea, PostalCode: a.PostalCode,
			CountryCode: strings.ToUpper(a.CountryCode), Country: a.Country}
		if a.Street != "" {
			out.Address.Lines = strings.Split(a.Street, "\n")
		}
	}
	return out
}

var cardKinds = map[string]CardKind{"credit": CreditCard, "debit": DebitCard, "prepaid": PrepaidCard, "store": StoreCard, "eMoney": EMoneyCard,
	"CREDIT": CreditCard, "DEBIT": DebitCard, "PREPAID": PrepaidCard}

var lastDigits = regexp.MustCompile(`([0-9]{4})\s*$`)

func cardFrom(c *spec.Card) Card {
	if c == nil {
		return Card{}
	}
	out := Card{Network: Network(c.Network), Kind: cardKinds[c.Type], Name: c.Name}
	if m := lastDigits.FindStringSubmatch(c.Name); m != nil {
		out.Last4 = m[1]
	}
	out.Billing = contactFrom(c.Billing)
	return out
}

func applePayment(p *spec.ApplePayment) Payment {
	if p == nil {
		return Payment{Wallet: ApplePay}
	}
	out := Payment{Wallet: ApplePay, Token: p.Token, TransactionID: p.TransactionID, Card: cardFrom(&p.Card),
		Shipping: contactFrom(p.Shipping), Billing: contactFrom(p.Billing), ShippingMethod: p.ShippingMethod}
	return out
}

func googleContact(a *spec.GoogleAddress, email string) Contact {
	if a == nil {
		return Contact{Email: email}
	}
	c := Contact{Name: a.Name, Phone: a.PhoneNumber, Email: email, Address: PostalAddress{
		City: a.Locality, State: a.AdministrativeArea, PostalCode: a.PostalCode,
		CountryCode: a.CountryCode, SortingCode: a.SortingCode,
	}}
	for _, l := range []string{a.Address1, a.Address2, a.Address3} {
		if l != "" {
			c.Address.Lines = append(c.Address.Lines, l)
		}
	}
	return c
}

func googleCard(m *spec.GoogleMethodData) Card {
	c := Card{Kind: cardKinds[m.Info.CardFundingSource], Name: m.Description, Last4: m.Info.CardDetails}
	for n, g := range googleNetworks {
		if g == m.Info.CardNetwork {
			c.Network = n
		}
	}
	if c.Network == "" {
		c.Network = Network(strings.ToLower(m.Info.CardNetwork))
	}
	c.Billing = googleContact(m.Info.BillingAddress, "")
	return c
}

func googlePayment(d spec.GooglePaymentData, raw []byte) Payment {
	p := Payment{Wallet: GooglePay, Card: googleCard(&d.PaymentMethodData), Raw: raw,
		Shipping: googleContact(d.ShippingAddress, d.Email)}
	if d.ShippingAddress == nil {
		p.Shipping = Contact{Email: d.Email}
	}
	if t := d.PaymentMethodData.TokenizationData; t != nil {
		p.Token = t.Token
	}
	p.Billing = p.Card.Billing
	if d.ShippingOptionData != nil && d.ShippingOptionData.ID != "shipping_option_unselected" {
		p.ShippingMethod = d.ShippingOptionData.ID
	}
	return p
}

// The button.

// ButtonType is the button's words.
type ButtonType uint8

const (
	Plain       ButtonType = iota // the wallet's logo alone
	Buy                           // "Buy with"
	Checkout                      // "Check out with"
	Book                          // "Book with"
	Donate                        // "Donate with" (Apple Pay: for an approved nonprofit)
	Order                         // "Order with"
	Subscribe                     // "Subscribe with"
	PayButton                     // "Pay with" (Apple Pay: Plain)
	Continue                      // "Continue with" (Apple Pay; Google Pay: Pay)
	SetUpButton                   // "Set up Apple Pay" (Apple Pay; Google Pay: Plain)
	Reload                        // Apple Pay's "Reload with" (Google Pay: Pay)
	AddMoney                      // Apple Pay's "Add Money with" (Google Pay: Pay)
	TopUp                         // Apple Pay's "Top Up with" (Google Pay: Pay)
	Rent                          // Apple Pay's "Rent with" (Google Pay: Book)
	Support                       // Apple Pay's "Support with" (Google Pay: Donate)
	Contribute                    // Apple Pay's "Contribute with" (Google Pay: Donate)
	Tip                           // Apple Pay's "Tip with" (Google Pay: Pay)
)

// appleButtonTypes are PKPaymentButtonType's values for each ButtonType.
var appleButtonTypes = [...]int{0, 1, 5, 6, 4, 11, 7, 0, 16, 2, 8, 9, 10, 12, 13, 14, 15}

// googleButtonTypes are Google Pay's ButtonConstants.ButtonType for each.
var googleButtonTypes = [...]int{8, 1, 3, 2, 4, 5, 7, 6, 6, 8, 6, 6, 6, 2, 4, 4, 6}

// buttonWords are what each type says, for screen readers (the web's)
// and uitest: Apple's and Google's English titles.
var buttonWords = [...]string{"", "Buy with", "Check out with", "Book with", "Donate with", "Order with", "Subscribe with",
	"Pay with", "Continue with", "Set up", "Reload with", "Add money with", "Top up with", "Rent with", "Support with",
	"Contribute with", "Tip with"}

// ButtonStyle is the button's colors.
type ButtonStyle uint8

const (
	Automatic    ButtonStyle = iota // black in light mode, white in dark
	Black                           // on light backgrounds
	White                           // on dark ones
	WhiteOutline                    // white, outlined: on white or light backgrounds (Google Pay: White, which is)
)

// ButtonProps are the button's.
type ButtonProps struct {
	Type  ButtonType
	Style ButtonStyle
	// CornerRadius rounds it (0: the wallet's own: Apple's, Google's
	// pill).
	CornerRadius float64
	// Networks are what Google Pay's button shows a card of the user's
	// for (default the Request's default).
	Networks []Network
	// OnPress is the tap: call Pay (from a goroutine).
	OnPress func()
	// Layout is the button's box: as wide as it's given, at the wallet's
	// height (Apple Pay's 44 points on iOS, 30 on macOS; Google Pay's
	// 48) unless the style says.
	Layout ui.Styler
}

func (t ButtonType) label(w Wallet) string {
	name := w.String()
	i := int(t)
	if i >= len(buttonWords) {
		i = 0
	}
	if t == PayButton && w == ApplePay {
		i = 0
	}
	if t == SetUpButton && w == GooglePay {
		i = 0
	}
	if buttonWords[i] == "" {
		return name
	}
	return buttonWords[i] + " " + name
}

// Button is the wallet's own button, which its guidelines ask for:
// Apple Pay's (PKPaymentButton, the CSS one in Safari), Google Pay's
// (PayButton, Google's in the other browsers), with its logo and words
// in the user's language. Show it only where Available says the wallet
// can pay; elsewhere (Windows, Linux) it's nothing.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Button(p ButtonProps) ui.Node {
	w := NoWallet
	switch ui.Platform() {
	case ui.PlatformIOS, ui.PlatformMacOS:
		w = ApplePay
	case ui.PlatformAndroid, ui.PlatformWeb:
		w = GooglePay
	}
	if w == NoWallet {
		return ui.Show(signal.Static(false), nil)
	}
	t := p.Type
	if int(t) >= len(appleButtonTypes) {
		t = Plain
	}
	r := Request{Networks: p.Networks}
	methods, _ := json.Marshal([]spec.GoogleMethod{{Type: "CARD", Parameters: r.googleCard()}})
	radius := p.CornerRadius
	if radius <= 0 {
		radius = -1
	}
	np := ui.NativeProps{Style: p.Layout, Props: map[string]any{
		"appleType": appleButtonTypes[t], "googleType": googleButtonTypes[t], "style": int(p.Style),
		"cornerRadius": radius, "methods": string(methods),
		"label": t.label(w), "appleLabel": t.label(ApplePay), "googleLabel": t.label(GooglePay),
	}, On: map[string]func(ui.NativeEvent){}}
	if p.OnPress != nil {
		np.On["press"] = func(ui.NativeEvent) { p.OnPress() }
	}
	return ui.Native("asuka.payButton", np)
}

// Package payspec is asuka/sdk/pay's wire: what the core sends the hosts'
// "pay" module and what they answer, shared by the package and the fake
// host (host/fake's Wallet).
//
// The module's methods:
//
//   - "available" (Present's JSON, its handlers left out): answers
//     Availability's JSON.
//   - "setUp": Apple Pay's screen to add a card (PKPassLibrary's
//     openPaymentSetup); fails "not available" elsewhere.
//   - "present" (Present): shows the wallet's sheet, answering once it
//     shows; fails "not available" (no wallet), "busy" (a sheet shows).
//   - "next" (the sheet's ID): answers the sheet's next Event; after
//     "done", fails "stopped".
//   - "answer" (Answer): completes the Event of its Seq.
//
// Apple Pay's (iOS, macOS, Safari) events and answers are Apple shaped:
// the host turns PassKit's objects into Contact, Card and ApplePayment,
// and Answer's items, methods and errors into PassKit's. Google Pay's
// (Android, other browsers) are Google's own JSON, passed through: the
// core builds the PaymentDataRequest (Present.Google) and the
// PaymentDataRequestUpdate and PaymentAuthorizationResult (Answer.Google),
// and reads IntermediatePaymentData and PaymentData (Event.Google).
package payspec

import "encoding/json"

// Present is "present"'s (and "available"'s) argument.
type Present struct {
	ID string `json:"id"`
	// Apple is the request as Apple Pay takes it.
	Apple AppleRequest `json:"apple"`
	// Google is Google Pay's PaymentDataRequest, as its API takes it.
	Google json.RawMessage `json:"google"`
	// GoogleEnv is "TEST" or "PRODUCTION".
	GoogleEnv string `json:"googleEnv"`
	// GoogleReady is Google's IsReadyToPayRequest (for "available").
	GoogleReady json.RawMessage `json:"googleReady,omitempty"`
	// AppleError and GoogleError say what the request lacks for the
	// wallet ("": nothing): "present" fails with it.
	AppleError  string `json:"appleError,omitempty"`
	GoogleError string `json:"googleError,omitempty"`
	// Validate: the app validates the merchant (the web's Apple Pay):
	// "validate" events come.
	Validate bool `json:"validate,omitempty"`
}

// Availability is "available"'s answer.
type Availability struct {
	// Wallet is "apple", "google" or "" (none).
	Wallet  string `json:"wallet"`
	CanPay  bool   `json:"canPay"`
	HasCard bool   `json:"hasCard"`
}

// AppleRequest is a PKPaymentRequest.
type AppleRequest struct {
	Merchant string `json:"merchant"`
	Country  string `json:"country"`
	Currency string `json:"currency"`
	// Networks are asuka/sdk/pay's names ("visa", "mastercard"), which the
	// hosts map to PKPaymentNetwork's.
	Networks []string `json:"networks"`
	// Capabilities: "3DS", "EMV", "credit", "debit", "instantFundsOut".
	Capabilities  []string `json:"capabilities"`
	CardCountries []string `json:"cardCountries,omitempty"`
	// Items are the summary items, the total last.
	Items []AppleItem `json:"items"`
	// ShippingFields and BillingFields are "name", "email", "phone",
	// "address" and "phoneticName" (PKContactField's).
	ShippingFields []string `json:"shippingFields,omitempty"`
	BillingFields  []string `json:"billingFields,omitempty"`
	// ShippingType: "shipping", "delivery", "storePickup", "servicePickup".
	ShippingType    string                `json:"shippingType,omitempty"`
	ShippingMethods []AppleShippingMethod `json:"shippingMethods,omitempty"`
	// ShippingFixed: the address can't be changed (a store pickup).
	ShippingFixed   bool            `json:"shippingFixed,omitempty"`
	ShippingContact *Contact        `json:"shippingContact,omitempty"`
	Coupons         bool            `json:"coupons,omitempty"`
	Coupon          string          `json:"coupon,omitempty"`
	ApplicationData string          `json:"applicationData,omitempty"` // base64
	Recurring       *AppleRecurring `json:"recurring,omitempty"`
	Deferred        *AppleDeferred  `json:"deferred,omitempty"`
	Reload          *AppleReload    `json:"reload,omitempty"`
	Events          AppleEventsOn   `json:"events"`
}

// AppleEventsOn are the callbacks the app has: the host completes the
// others itself, with what it has.
type AppleEventsOn struct {
	ShippingContact bool `json:"shippingContact,omitempty"`
	ShippingMethod  bool `json:"shippingMethod,omitempty"`
	PaymentMethod   bool `json:"paymentMethod,omitempty"`
	Coupon          bool `json:"coupon,omitempty"`
}

// AppleItem is a PKPaymentSummaryItem, or one of its kinds.
type AppleItem struct {
	Label   string `json:"label"`
	Amount  string `json:"amount"`
	Pending bool   `json:"pending,omitempty"`
	// Kind: "" (a summary item), "recurring", "deferred", "reload".
	Kind string `json:"kind,omitempty"`
	// Recurring: its dates (Unix ms, 0: none), interval ("day", "week",
	// "month", "year") and count.
	StartMs  int64  `json:"startMs,omitempty"`
	EndMs    int64  `json:"endMs,omitempty"`
	Interval string `json:"interval,omitempty"`
	Every    int    `json:"every,omitempty"`
	// Deferred: when it's charged (Unix ms).
	DateMs int64 `json:"dateMs,omitempty"`
	// Reload: the balance it reloads under.
	Threshold string `json:"threshold,omitempty"`
}

// AppleShippingMethod is a PKShippingMethod.
type AppleShippingMethod struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Amount string `json:"amount"`
	// From and To are its delivery dates, "2026-11-20" (none: "").
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// AppleRecurring is a PKRecurringPaymentRequest.
type AppleRecurring struct {
	Description          string     `json:"description"`
	Regular              AppleItem  `json:"regular"`
	Trial                *AppleItem `json:"trial,omitempty"`
	ManagementURL        string     `json:"managementURL"`
	BillingAgreement     string     `json:"billingAgreement,omitempty"`
	TokenNotificationURL string     `json:"tokenNotificationURL,omitempty"`
}

// AppleDeferred is a PKDeferredPaymentRequest.
type AppleDeferred struct {
	Description          string    `json:"description"`
	Billing              AppleItem `json:"billing"`
	ManagementURL        string    `json:"managementURL"`
	BillingAgreement     string    `json:"billingAgreement,omitempty"`
	TokenNotificationURL string    `json:"tokenNotificationURL,omitempty"`
	FreeCancellationMs   int64     `json:"freeCancellationMs,omitempty"`
}

// AppleReload is a PKAutomaticReloadPaymentRequest.
type AppleReload struct {
	Description          string    `json:"description"`
	Billing              AppleItem `json:"billing"`
	ManagementURL        string    `json:"managementURL"`
	BillingAgreement     string    `json:"billingAgreement,omitempty"`
	TokenNotificationURL string    `json:"tokenNotificationURL,omitempty"`
}

// Contact is a PKContact.
type Contact struct {
	GivenName  string   `json:"givenName,omitempty"`
	FamilyName string   `json:"familyName,omitempty"`
	Name       string   `json:"name,omitempty"` // formatted, as the locale writes it
	Email      string   `json:"email,omitempty"`
	Phone      string   `json:"phone,omitempty"`
	Address    *Address `json:"address,omitempty"`
}

// Address is a CNPostalAddress.
type Address struct {
	Street                string `json:"street,omitempty"` // lines joined with "\n"
	SubLocality           string `json:"subLocality,omitempty"`
	City                  string `json:"city,omitempty"`
	SubAdministrativeArea string `json:"subAdministrativeArea,omitempty"`
	State                 string `json:"state,omitempty"`
	PostalCode            string `json:"postalCode,omitempty"`
	Country               string `json:"country,omitempty"`
	CountryCode           string `json:"countryCode,omitempty"`
}

// Card is a PKPaymentMethod.
type Card struct {
	// Network is asuka/sdk/pay's name ("visa"), or PKPaymentNetwork's
	// lowercased for one it hasn't.
	Network string `json:"network,omitempty"`
	// Type: "debit", "credit", "prepaid", "store", "eMoney", "".
	Type    string   `json:"type,omitempty"`
	Name    string   `json:"name,omitempty"` // "Visa 1234"
	Billing *Contact `json:"billing,omitempty"`
}

// ApplePayment is a PKPayment.
type ApplePayment struct {
	// Token is its token's paymentData, as UTF-8 (a JSON object).
	Token          string   `json:"token"`
	TransactionID  string   `json:"transactionId,omitempty"`
	Card           Card     `json:"card"`
	Shipping       *Contact `json:"shipping,omitempty"`
	Billing        *Contact `json:"billing,omitempty"`
	ShippingMethod string   `json:"shippingMethod,omitempty"`
}

// Event types.
const (
	ShippingContact = "shippingContact"
	ShippingMethod  = "shippingMethod"
	PaymentMethod   = "paymentMethod"
	Coupon          = "coupon"
	Authorize       = "authorize"
	Validate        = "validate" // the web's Apple Pay: a merchant session for URL
	Google          = "google"   // Google Pay's IntermediatePaymentData
	GoogleAuthorize = "googleAuthorize"
	Done            = "done"
)

// Event is "next"'s answer: what happened in the sheet.
type Event struct {
	Type string `json:"type"`
	// Seq is answered with (Answer.Seq), but for Done.
	Seq     int           `json:"seq"`
	Contact *Contact      `json:"contact,omitempty"` // ShippingContact
	Method  string        `json:"method,omitempty"`  // ShippingMethod: its ID
	Card    *Card         `json:"card,omitempty"`    // PaymentMethod
	Coupon  string        `json:"coupon,omitempty"`  // Coupon
	URL     string        `json:"url,omitempty"`     // Validate
	Payment *ApplePayment `json:"payment,omitempty"` // Authorize
	// Google is Google Pay's IntermediatePaymentData (Google) or
	// PaymentData (GoogleAuthorize).
	Google json.RawMessage `json:"google,omitempty"`
	// Error is Done's, when the sheet failed (not when it was closed).
	Error string `json:"error,omitempty"`
}

// Answer is "answer"'s argument.
type Answer struct {
	ID  string `json:"id"`
	Seq int    `json:"seq"`
	// Items (the total last) and ShippingMethods: Apple Pay's, as they
	// are now (nil: as they were).
	Items           []AppleItem           `json:"items,omitempty"`
	ShippingMethods []AppleShippingMethod `json:"shippingMethods,omitempty"`
	// Errors, none: it's fine (an authorization: it succeeded).
	Errors []FieldError `json:"errors,omitempty"`
	// Session is the merchant session (Validate), as Apple's server gave it.
	Session json.RawMessage `json:"session,omitempty"`
	// Google is Google Pay's PaymentDataRequestUpdate (Google) or
	// PaymentAuthorizationResult (GoogleAuthorize).
	Google json.RawMessage `json:"google,omitempty"`
}

// FieldError is an error the sheet shows by a field.
type FieldError struct {
	// Field: "payment", "shippingAddress", "shippingStreet",
	// "shippingCity", "shippingState", "shippingPostalCode",
	// "shippingCountry", "shippingName", "shippingEmail",
	// "shippingPhone", "unserviceable", "shippingMethod",
	// "billingAddress", "billingStreet", "billingCity", "billingState",
	// "billingPostalCode", "billingCountry", "billingName",
	// "couponInvalid", "couponExpired".
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Google Pay's API objects (developers.google.com/pay/api), the fields
// Asuka uses.

// GoogleRequest is a PaymentDataRequest.
type GoogleRequest struct {
	APIVersion                     int                    `json:"apiVersion"`
	APIVersionMinor                int                    `json:"apiVersionMinor"`
	MerchantInfo                   *GoogleMerchant        `json:"merchantInfo,omitempty"`
	AllowedPaymentMethods          []GoogleMethod         `json:"allowedPaymentMethods"`
	TransactionInfo                *GoogleTransaction     `json:"transactionInfo,omitempty"`
	RecurringTransactionInfo       *GoogleRecurring       `json:"recurringTransactionInfo,omitempty"`
	DeferredTransactionInfo        *GoogleDeferred        `json:"deferredTransactionInfo,omitempty"`
	AutomaticReloadTransactionInfo *GoogleReload          `json:"automaticReloadTransactionInfo,omitempty"`
	CallbackIntents                []string               `json:"callbackIntents,omitempty"`
	EmailRequired                  bool                   `json:"emailRequired,omitempty"`
	ShippingAddressRequired        bool                   `json:"shippingAddressRequired,omitempty"`
	ShippingAddressParameters      *GoogleShippingAddress `json:"shippingAddressParameters,omitempty"`
	ShippingOptionRequired         bool                   `json:"shippingOptionRequired,omitempty"`
	ShippingOptionParameters       *GoogleShippingOptions `json:"shippingOptionParameters,omitempty"`
}

// GoogleReady is an IsReadyToPayRequest.
type GoogleReady struct {
	APIVersion                    int            `json:"apiVersion"`
	APIVersionMinor               int            `json:"apiVersionMinor"`
	AllowedPaymentMethods         []GoogleMethod `json:"allowedPaymentMethods"`
	ExistingPaymentMethodRequired bool           `json:"existingPaymentMethodRequired,omitempty"`
}

type GoogleMerchant struct {
	MerchantID   string `json:"merchantId,omitempty"`
	MerchantName string `json:"merchantName,omitempty"`
}

type GoogleMethod struct {
	Type                      string               `json:"type"`
	Parameters                GoogleCardParameters `json:"parameters"`
	TokenizationSpecification *GoogleTokenization  `json:"tokenizationSpecification,omitempty"`
}

type GoogleCardParameters struct {
	AllowedAuthMethods        []string              `json:"allowedAuthMethods"`
	AllowedCardNetworks       []string              `json:"allowedCardNetworks"`
	AllowPrepaidCards         *bool                 `json:"allowPrepaidCards,omitempty"`
	AllowCreditCards          *bool                 `json:"allowCreditCards,omitempty"`
	AllowedIssuerCountryCodes []string              `json:"allowedIssuerCountryCodes,omitempty"`
	AssuranceDetailsRequired  bool                  `json:"assuranceDetailsRequired,omitempty"`
	BillingAddressRequired    bool                  `json:"billingAddressRequired,omitempty"`
	BillingAddressParameters  *GoogleBillingAddress `json:"billingAddressParameters,omitempty"`
}

type GoogleBillingAddress struct {
	Format              string `json:"format,omitempty"`
	PhoneNumberRequired bool   `json:"phoneNumberRequired,omitempty"`
}

type GoogleShippingAddress struct {
	AllowedCountryCodes []string `json:"allowedCountryCodes,omitempty"`
	PhoneNumberRequired bool     `json:"phoneNumberRequired,omitempty"`
}

type GoogleShippingOptions struct {
	ShippingOptions         []GoogleOption `json:"shippingOptions"`
	DefaultSelectedOptionID string         `json:"defaultSelectedOptionId,omitempty"`
}

type GoogleOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type GoogleTokenization struct {
	Type       string            `json:"type"`
	Parameters map[string]string `json:"parameters"`
}

type GoogleTransaction struct {
	CurrencyCode     string       `json:"currencyCode"`
	CountryCode      string       `json:"countryCode,omitempty"`
	TransactionID    string       `json:"transactionId,omitempty"`
	TotalPriceStatus string       `json:"totalPriceStatus"`
	TotalPrice       string       `json:"totalPrice"`
	TotalPriceLabel  string       `json:"totalPriceLabel,omitempty"`
	DisplayItems     []GoogleItem `json:"displayItems,omitempty"`
	CheckoutOption   string       `json:"checkoutOption,omitempty"`
}

type GoogleItem struct {
	Label  string `json:"label"`
	Type   string `json:"type"`
	Price  string `json:"price"`
	Status string `json:"status,omitempty"`
}

type GoogleRecurring struct {
	CurrencyCode          string                 `json:"currencyCode"`
	CountryCode           string                 `json:"countryCode"`
	TransactionID         string                 `json:"transactionId,omitempty"`
	TokenUpdateURL        string                 `json:"tokenUpdateUrl,omitempty"`
	ManagementURL         string                 `json:"managementUrl,omitempty"`
	BillingAgreement      string                 `json:"billingAgreement,omitempty"`
	ImmediateTotalPrice   string                 `json:"immediateTotalPrice"`
	ImmediateDisplayItems []GoogleItem           `json:"immediateDisplayItems,omitempty"`
	IntroductoryPeriod    *GoogleIntroductory    `json:"introductoryPeriodInfo,omitempty"`
	RecurrenceItems       []GoogleRecurrenceItem `json:"recurrenceItems"`
}

type GoogleIntroductory struct {
	Start      string `json:"introductoryPeriodStartDateTime,omitempty"`
	End        string `json:"introductoryPeriodEndDateTime"`
	Label      string `json:"label"`
	TotalPrice string `json:"totalPrice"`
}

type GoogleRecurrenceItem struct {
	Start       string `json:"billingInitialDateTime,omitempty"`
	End         string `json:"billingFinalDateTime,omitempty"`
	Label       string `json:"label"`
	Price       string `json:"price,omitempty"`
	PriceStatus string `json:"priceStatus"`
	Period      string `json:"recurrencePeriod"`
	PeriodCount int    `json:"recurrencePeriodCount"`
}

type GoogleDeferred struct {
	CurrencyCode          string       `json:"currencyCode"`
	CountryCode           string       `json:"countryCode"`
	TransactionID         string       `json:"transactionId,omitempty"`
	TokenUpdateURL        string       `json:"tokenUpdateUrl,omitempty"`
	ManagementURL         string       `json:"managementUrl,omitempty"`
	BillingAgreement      string       `json:"billingAgreement,omitempty"`
	ImmediateTotalPrice   string       `json:"immediateTotalPrice"`
	ImmediateDisplayItems []GoogleItem `json:"immediateDisplayItems,omitempty"`
	BillingDateTime       string       `json:"billingDateTime"`
	PriceStatus           string       `json:"priceStatus"`
	Price                 string       `json:"price,omitempty"`
	Label                 string       `json:"label"`
}

type GoogleReload struct {
	CurrencyCode          string       `json:"currencyCode"`
	CountryCode           string       `json:"countryCode"`
	TransactionID         string       `json:"transactionId,omitempty"`
	TokenUpdateURL        string       `json:"tokenUpdateUrl,omitempty"`
	ManagementURL         string       `json:"managementUrl,omitempty"`
	BillingAgreement      string       `json:"billingAgreement,omitempty"`
	ImmediateTotalPrice   string       `json:"immediateTotalPrice"`
	ImmediateDisplayItems []GoogleItem `json:"immediateDisplayItems,omitempty"`
	MinimumBalanceAmount  string       `json:"minimumBalanceAmount"`
	ReloadAmount          string       `json:"reloadAmount"`
	Label                 string       `json:"label"`
}

// GoogleUpdate is a PaymentDataRequestUpdate.
type GoogleUpdate struct {
	NewTransactionInfo          *GoogleTransaction     `json:"newTransactionInfo,omitempty"`
	NewShippingOptionParameters *GoogleShippingOptions `json:"newShippingOptionParameters,omitempty"`
	Error                       *GoogleError           `json:"error,omitempty"`
}

// GoogleResult is a PaymentAuthorizationResult.
type GoogleResult struct {
	TransactionState string       `json:"transactionState"`
	Error            *GoogleError `json:"error,omitempty"`
}

// GoogleError is a PaymentDataError.
type GoogleError struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Intent  string `json:"intent"`
}

// GoogleIntermediate is IntermediatePaymentData.
type GoogleIntermediate struct {
	CallbackTrigger    string            `json:"callbackTrigger,omitempty"`
	ShippingAddress    *GoogleAddress    `json:"shippingAddress,omitempty"`
	ShippingOptionData *GoogleOptionData `json:"shippingOptionData,omitempty"`
	PaymentMethodData  *GoogleMethodData `json:"paymentMethodData,omitempty"`
}

type GoogleOptionData struct {
	ID string `json:"id"`
}

// GooglePaymentData is PaymentData.
type GooglePaymentData struct {
	APIVersion         int               `json:"apiVersion"`
	APIVersionMinor    int               `json:"apiVersionMinor"`
	PaymentMethodData  GoogleMethodData  `json:"paymentMethodData"`
	Email              string            `json:"email,omitempty"`
	ShippingAddress    *GoogleAddress    `json:"shippingAddress,omitempty"`
	ShippingOptionData *GoogleOptionData `json:"shippingOptionData,omitempty"`
}

type GoogleMethodData struct {
	Type             string           `json:"type"`
	Description      string           `json:"description,omitempty"`
	Info             GoogleCardInfo   `json:"info"`
	TokenizationData *GoogleTokenData `json:"tokenizationData,omitempty"`
}

type GoogleCardInfo struct {
	CardNetwork       string         `json:"cardNetwork,omitempty"`
	CardDetails       string         `json:"cardDetails,omitempty"`
	CardFundingSource string         `json:"cardFundingSource,omitempty"`
	BillingAddress    *GoogleAddress `json:"billingAddress,omitempty"`
}

// GoogleTokenData is PaymentMethodTokenizationData.
type GoogleTokenData struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

// GoogleAddress is an Address (or an IntermediateAddress).
type GoogleAddress struct {
	Name               string `json:"name,omitempty"`
	Address1           string `json:"address1,omitempty"`
	Address2           string `json:"address2,omitempty"`
	Address3           string `json:"address3,omitempty"`
	Locality           string `json:"locality,omitempty"`
	AdministrativeArea string `json:"administrativeArea,omitempty"`
	CountryCode        string `json:"countryCode,omitempty"`
	PostalCode         string `json:"postalCode,omitempty"`
	SortingCode        string `json:"sortingCode,omitempty"`
	PhoneNumber        string `json:"phoneNumber,omitempty"`
}

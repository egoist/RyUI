package pay_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/pay"
	"github.com/egoist/asuka/ui"
)

// env is a fake host with a wallet, and the app mounted.
type env struct {
	h *fake.Host
	r *ui.Root
	w *fake.Wallet
}

func newEnv(t *testing.T, p ops.Platform, root func() ui.Node) *env {
	t.Helper()
	e := &env{h: fake.New()}
	e.h.Env.Platform = p
	e.w = e.h.Wallet()
	if root == nil {
		root = func() ui.Node { return ui.StaticText("x") }
	}
	e.r = ui.Mount(e.h, root)
	t.Cleanup(e.r.Dispose)
	return e
}

// until runs the main thread until cond holds.
func (e *env) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 5000 { // a safety net, not what passes
		e.h.RunPosted()
		e.r.Flush()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiting for %s", what)
}

// answered waits for the app's answer to what the test did.
func (e *env) answered(t *testing.T) {
	t.Helper()
	e.until(t, "the app's answer", func() bool { return !e.w.Waiting() })
}

type result struct {
	p   pay.Payment
	err error
}

func (e *env) pay(t *testing.T, r pay.Request) chan result {
	t.Helper()
	ch := make(chan result, 1)
	go func() {
		p, err := pay.Pay(r)
		ch <- result{p, err}
	}()
	e.until(t, "the sheet", func() bool { return e.w.Sheet != nil || len(ch) > 0 })
	return ch
}

func (e *env) done(t *testing.T, ch chan result) result {
	t.Helper()
	var res result
	e.until(t, "Pay's return", func() bool {
		select {
		case res = <-ch:
			return true
		default:
			return false
		}
	})
	return res
}

// order is a store's order: shipping by address, two methods, tax by
// the state.
func order(calls *[]string) pay.Request {
	ship := []pay.ShippingMethod{
		{ID: "ground", Label: "Ground", Detail: "5 to 7 days", Amount: "0.00"},
		{ID: "express", Label: "Express", Detail: "Tomorrow", Amount: "9.00"},
	}
	items := func(shipping, tax string) []pay.Item {
		return []pay.Item{
			{Label: "Tea set", Amount: "40.00"},
			{Label: "Shipping", Amount: shipping, Kind: pay.ShippingCost},
			{Label: "Tax", Amount: tax, Kind: pay.Tax},
		}
	}
	shipping, tax := "0.00", "0.00"
	return pay.Request{
		Country: "US", Currency: "USD",
		Items: items("0.00", "0.00"), Total: pay.Item{Label: "Tea Shop", Amount: "40.00"},
		Shipping: pay.Name | pay.Address | pay.Email, Billing: pay.Address,
		ShippingMethods: ship, Coupons: true,
		Apple:  pay.Apple{MerchantID: "merchant.dev.asuka.tea"},
		Google: pay.Google{Gateway: "example", GatewayParams: map[string]string{"gatewayMerchantId": "tea"}},
		OnShippingContact: func(c pay.Contact) (pay.Update, error) {
			*calls = append(*calls, "contact "+c.Address.State+" "+c.Address.PostalCode)
			if c.Address.CountryCode != "US" {
				return pay.Update{}, pay.Invalid(pay.Unserviceable, "We ship in the US only")
			}
			tax = "3.20"
			return pay.Update{Items: items(shipping, tax), Total: pay.Item{Amount: "43.20"}}, nil
		},
		OnShippingMethod: func(m pay.ShippingMethod) (pay.Update, error) {
			*calls = append(*calls, "method "+m.ID)
			shipping = m.Amount
			total := map[string]string{"0.00": "43.20", "9.00": "52.20"}[shipping]
			return pay.Update{Items: items(shipping, tax), Total: pay.Item{Amount: total}}, nil
		},
		OnCoupon: func(code string) (pay.Update, error) {
			*calls = append(*calls, "coupon "+code)
			return pay.Update{}, pay.Invalid(pay.CouponInvalid, "No such coupon")
		},
		OnAuthorize: func(p pay.Payment) error {
			*calls = append(*calls, "authorize "+p.Token)
			if p.Token == "declined" {
				return errors.Join(pay.Invalid(pay.ShippingPostalCode, "Check the ZIP code"), errors.New("Your card was declined"))
			}
			return nil
		},
	}
}

var home = fake.PayContact{GivenName: "Ada", FamilyName: "Lovelace", Email: "ada@example.com",
	Lines: []string{"1 Infinite Loop"}, City: "Cupertino", State: "CA", PostalCode: "95014", CountryCode: "US"}

// The order on Apple Pay: the sheet follows the app's updates, shows its
// errors by their fields, and Pay returns the payment it accepted.
func TestApplePayFlow(t *testing.T) {
	var calls []string
	e := newEnv(t, ops.PlatformIOS, nil)
	ch := e.pay(t, order(&calls))
	s := e.w.Sheet
	if s.Wallet != "apple" || s.Merchant != "merchant.dev.asuka.tea" || s.Total.Amount != "40.00" || s.Total.Label != "Tea Shop" ||
		len(s.Items) != 3 || len(s.ShippingMethods) != 2 || s.Selected != "ground" {
		t.Fatalf("sheet: %+v", s)
	}
	var p struct {
		Apple struct {
			Networks, Capabilities, ShippingFields, BillingFields []string
			Coupons                                               bool
			Events                                                map[string]bool
		}
	}
	json.Unmarshal(s.Request, &p)
	if strings.Join(p.Apple.Networks, ",") != "visa,mastercard,amex,discover" || strings.Join(p.Apple.Capabilities, ",") != "3DS" ||
		strings.Join(p.Apple.ShippingFields, ",") != "name,email,address" || strings.Join(p.Apple.BillingFields, ",") != "address" ||
		!p.Apple.Coupons || !p.Apple.Events["shippingContact"] || !p.Apple.Events["coupon"] || p.Apple.Events["paymentMethod"] {
		t.Fatalf("apple request: %+v", p.Apple)
	}

	e.w.ChangeShippingContact(home)
	e.answered(t)
	if s.Total.Amount != "43.20" || s.Items[2].Amount != "3.20" || len(s.Errors) != 0 {
		t.Fatalf("after the contact: %+v", s)
	}
	e.w.ChooseShippingMethod("express")
	e.answered(t)
	if s.Total.Amount != "52.20" || s.Items[1].Amount != "9.00" || s.Selected != "express" {
		t.Fatalf("after the method: %+v", s)
	}
	e.w.EnterCoupon("TEA10")
	e.answered(t)
	if len(s.Errors) != 1 || s.Errors[0] != (fake.PayError{Field: "couponInvalid", Message: "No such coupon"}) || s.Total.Amount != "52.20" {
		t.Fatalf("after the coupon: %+v", s)
	}
	abroad := home
	abroad.CountryCode = "FR"
	e.w.ChangeShippingContact(abroad)
	e.answered(t)
	if len(s.Errors) != 1 || s.Errors[0].Field != "unserviceable" {
		t.Fatalf("abroad: %+v", s.Errors)
	}

	card := fake.PayCard{Network: "visa", Kind: "credit", Name: "Visa 1234"}
	e.w.Authorize(fake.PayAuthorization{Token: "declined", Card: card, Shipping: &home})
	e.answered(t)
	if e.w.Sheet == nil || len(s.Errors) != 2 || s.Errors[0].Field != "shippingPostalCode" ||
		s.Errors[1] != (fake.PayError{Field: "payment", Message: "Your card was declined"}) {
		t.Fatalf("declined: %+v", s.Errors)
	}
	e.w.Authorize(fake.PayAuthorization{Token: `{"data":"..."}`, Card: card, Shipping: &home, Billing: &home})
	res := e.done(t, ch)
	if res.err != nil || !s.Authorized || e.w.Sheet != nil {
		t.Fatalf("Pay: %v, %+v", res.err, s)
	}
	got := res.p
	if got.Wallet != pay.ApplePay || got.Token != `{"data":"..."}` || got.TransactionID != "fake-transaction" ||
		got.Card.Network != pay.Visa || got.Card.Kind != pay.CreditCard || got.Card.Last4 != "1234" ||
		got.Shipping.Name != "Ada Lovelace" || got.Shipping.Email != "ada@example.com" || got.Shipping.Address.Lines[0] != "1 Infinite Loop" ||
		got.Billing.Address.PostalCode != "95014" || got.ShippingMethod != "express" {
		t.Fatalf("payment: %+v", got)
	}
	want := []string{"contact CA 95014", "method express", "coupon TEA10", "contact CA 95014", "authorize declined", `authorize {"data":"..."}`}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls:\n%q\nwant\n%q", calls, want)
	}
}

// The order on Google Pay (Android): the PaymentDataRequest has the
// gateway, the callbacks and the shipping options; the updates are
// newTransactionInfo; errors are Google's reasons.
func TestGooglePayFlow(t *testing.T) {
	var calls []string
	e := newEnv(t, ops.PlatformAndroid, nil)
	ch := e.pay(t, order(&calls))
	s := e.w.Sheet
	if s.Wallet != "google" || s.Merchant != "Tea Shop" || s.Total.Amount != "40.00" || s.Total.Label != "Total" ||
		len(s.Items) != 3 || s.Selected != "ground" || len(s.ShippingMethods) != 2 {
		t.Fatalf("sheet: %+v", s)
	}
	var p struct {
		Google    json.RawMessage
		GoogleEnv string
	}
	json.Unmarshal(s.Request, &p)
	var g map[string]any
	json.Unmarshal(p.Google, &g)
	if p.GoogleEnv != "TEST" || g["apiVersion"] != 2.0 || g["emailRequired"] != true || g["shippingAddressRequired"] != true ||
		g["shippingOptionRequired"] != true {
		t.Fatalf("google request: %s", p.Google)
	}
	if ci := g["callbackIntents"].([]any); len(ci) != 3 || ci[0] != "PAYMENT_AUTHORIZATION" || ci[1] != "SHIPPING_ADDRESS" || ci[2] != "SHIPPING_OPTION" {
		t.Fatalf("callbackIntents: %v", ci)
	}
	m := g["allowedPaymentMethods"].([]any)[0].(map[string]any)
	params := m["parameters"].(map[string]any)
	tok := m["tokenizationSpecification"].(map[string]any)
	if tok["type"] != "PAYMENT_GATEWAY" || tok["parameters"].(map[string]any)["gateway"] != "example" ||
		tok["parameters"].(map[string]any)["gatewayMerchantId"] != "tea" ||
		params["billingAddressRequired"] != true || params["billingAddressParameters"].(map[string]any)["format"] != "FULL" {
		t.Fatalf("payment method: %v", m)
	}
	if n := params["allowedCardNetworks"].([]any); len(n) != 4 || n[0] != "VISA" || n[3] != "DISCOVER" {
		t.Fatalf("networks: %v", n)
	}
	ti := g["transactionInfo"].(map[string]any)
	if ti["totalPrice"] != "40.00" || ti["totalPriceStatus"] != "FINAL" || ti["currencyCode"] != "USD" || ti["countryCode"] != "US" ||
		ti["displayItems"].([]any)[2].(map[string]any)["type"] != "TAX" {
		t.Fatalf("transactionInfo: %v", ti)
	}

	e.w.ChangeShippingContact(home)
	e.answered(t)
	if s.Total.Amount != "43.20" || s.Items[2].Amount != "3.20" || s.Selected != "ground" {
		t.Fatalf("after the address: %+v", s)
	}
	e.w.ChooseShippingMethod("express")
	e.answered(t)
	if s.Total.Amount != "52.20" || s.Selected != "express" {
		t.Fatalf("after the option: %+v", s)
	}
	abroad := home
	abroad.CountryCode = "FR"
	e.w.ChangeShippingContact(abroad)
	e.answered(t)
	if len(s.Errors) != 1 || s.Errors[0] != (fake.PayError{Field: "unserviceable", Message: "We ship in the US only"}) {
		t.Fatalf("abroad: %+v", s.Errors)
	}
	card := fake.PayCard{Network: "visa", Kind: "debit", Name: "Visa •••• 4242"}
	e.w.Authorize(fake.PayAuthorization{Token: "declined", Card: card, Shipping: &home})
	e.answered(t)
	// Google Pay shows one error: the first.
	if e.w.Sheet == nil || len(s.Errors) != 1 || s.Errors[0] != (fake.PayError{Field: "shippingAddress", Message: "Check the ZIP code"}) {
		t.Fatalf("declined: %+v", s.Errors)
	}
	e.w.Authorize(fake.PayAuthorization{Token: "tok_visa", Card: card, Shipping: &home, Billing: &home})
	res := e.done(t, ch)
	got := res.p
	if res.err != nil || got.Wallet != pay.GooglePay || got.Token != "tok_visa" || got.Card.Network != pay.Visa ||
		got.Card.Kind != pay.DebitCard || got.Card.Last4 != "4242" || got.Shipping.Name != "Ada Lovelace" ||
		got.Shipping.Email != "ada@example.com" || got.Shipping.Address.PostalCode != "95014" ||
		got.Billing.Address.Lines[0] != "1 Infinite Loop" || got.ShippingMethod != "express" || !json.Valid(got.Raw) {
		t.Fatalf("Pay: %v, %+v", res.err, got)
	}
}

// Closing the sheet is ErrCanceled; a host without a wallet is
// ErrNotAvailable, and Available says so.
func TestCancelAndNotAvailable(t *testing.T) {
	var calls []string
	e := newEnv(t, ops.PlatformIOS, nil)
	ch := e.pay(t, order(&calls))
	e.w.Cancel()
	if res := e.done(t, ch); !errors.Is(res.err, pay.ErrCanceled) {
		t.Fatalf("Pay: %v", res.err)
	}

	avail := func() pay.Availability {
		ch := make(chan pay.Availability, 1)
		go func() { ch <- pay.Available(order(&calls)) }()
		var a pay.Availability
		e.until(t, "Available", func() bool {
			select {
			case a = <-ch:
				return true
			default:
				return false
			}
		})
		return a
	}
	if a := avail(); a != (pay.Availability{Wallet: pay.ApplePay, CanPay: true, HasCard: true}) {
		t.Fatalf("Available: %+v", a)
	}
	e.w.NoCard = true
	if a := avail(); a != (pay.Availability{Wallet: pay.ApplePay, CanPay: true}) {
		t.Fatalf("Available without a card: %+v", a)
	}
	e.w.Unavailable = true
	if a := avail(); a != (pay.Availability{}) {
		t.Fatalf("Available without a wallet: %+v", a)
	}
	if res := e.done(t, e.payNow(order(&calls))); !errors.Is(res.err, pay.ErrNotAvailable) {
		t.Fatalf("Pay without a wallet: %v", res.err)
	}

	// No "pay" module at all (a host without it): not available either.
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	e2 := &env{h: h, r: r, w: &fake.Wallet{}}
	if res := e2.done(t, e2.payNow(order(&calls))); !errors.Is(res.err, pay.ErrNotAvailable) {
		t.Fatalf("Pay with no module: %v", res.err)
	}
}

func (e *env) payNow(r pay.Request) chan result {
	ch := make(chan result, 1)
	go func() {
		p, err := pay.Pay(r)
		ch <- result{p, err}
	}()
	return ch
}

// A request missing a wallet's accounts fails for that wallet, saying
// what's missing; a bad amount fails before anything shows.
func TestRequestChecks(t *testing.T) {
	var calls []string
	e := newEnv(t, ops.PlatformAndroid, nil)
	r := order(&calls)
	r.Google = pay.Google{}
	if res := e.done(t, e.payNow(r)); res.err == nil || !strings.Contains(res.err.Error(), "Gateway") {
		t.Fatalf("no gateway: %v", res.err)
	}
	r = order(&calls)
	r.Google.Production = true
	if res := e.done(t, e.payNow(r)); res.err == nil || !strings.Contains(res.err.Error(), "MerchantID") {
		t.Fatalf("production without a merchant: %v", res.err)
	}
	r = order(&calls)
	r.Total.Amount = "12,50"
	if res := e.done(t, e.payNow(r)); res.err == nil || !strings.Contains(res.err.Error(), "isn't an amount") {
		t.Fatalf("a bad amount: %v", res.err)
	}
	r = order(&calls)
	r.OnAuthorize = nil
	if res := e.done(t, e.payNow(r)); res.err == nil {
		t.Fatal("no OnAuthorize: no error")
	}
	e.w.Kind = "apple"
	r = order(&calls)
	r.Apple = pay.Apple{}
	if res := e.done(t, e.payNow(r)); res.err == nil || !strings.Contains(res.err.Error(), "MerchantID") {
		t.Fatalf("no merchant ID: %v", res.err)
	}
	if e.w.Presented != 0 {
		t.Fatalf("presented %d", e.w.Presented)
	}
}

// A subscription: Apple Pay's recurring request and its summary item;
// Google Pay's recurringTransactionInfo.
func TestRecurring(t *testing.T) {
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	var calls []string
	r := order(&calls)
	r.Items, r.ShippingMethods, r.Shipping, r.Billing = nil, nil, 0, 0
	r.OnShippingContact, r.OnShippingMethod, r.OnCoupon = nil, nil, nil
	r.Total = pay.Item{Label: "Tea Club", Amount: "0.00"}
	r.Recurring = &pay.Recurring{
		Description: "A box of tea every month", ManagementURL: "https://tea.example/club",
		Regular: pay.Billing{Label: "Tea Club", Amount: "20.00", Start: start, Interval: pay.Month},
		Trial:   &pay.Billing{Label: "First box free", Amount: "0.00", End: start},
	}

	e := newEnv(t, ops.PlatformIOS, nil)
	ch := e.pay(t, r)
	var p struct {
		Apple struct {
			Items     []map[string]any
			Recurring map[string]any
		}
	}
	json.Unmarshal(e.w.Sheet.Request, &p)
	if len(p.Apple.Items) != 3 || p.Apple.Items[1]["kind"] != "recurring" || p.Apple.Items[1]["interval"] != "month" ||
		p.Apple.Items[1]["startMs"] != float64(start.UnixMilli()) || p.Apple.Recurring["managementURL"] != "https://tea.example/club" ||
		p.Apple.Recurring["trial"] == nil {
		t.Fatalf("apple: %+v", p.Apple)
	}
	e.w.Cancel()
	e.done(t, ch)

	e = newEnv(t, ops.PlatformAndroid, nil)
	ch = e.pay(t, r)
	var q struct{ Google map[string]any }
	json.Unmarshal(e.w.Sheet.Request, &q)
	rt, ok := q.Google["recurringTransactionInfo"].(map[string]any)
	if !ok || q.Google["transactionInfo"] != nil || rt["immediateTotalPrice"] != "0.00" || rt["managementUrl"] != "https://tea.example/club" {
		t.Fatalf("google: %v", q.Google)
	}
	item := rt["recurrenceItems"].([]any)[0].(map[string]any)
	if item["recurrencePeriod"] != "MONTH" || item["recurrencePeriodCount"] != 1.0 || item["price"] != "20.00" ||
		item["priceStatus"] != "FINAL" || item["billingInitialDateTime"] != "2026-12-01T00:00:00Z" {
		t.Fatalf("recurrence: %v", item)
	}
	if intro := rt["introductoryPeriodInfo"].(map[string]any); intro["introductoryPeriodEndDateTime"] != "2026-12-01T00:00:00Z" || intro["totalPrice"] != "0.00" {
		t.Fatalf("intro: %v", intro)
	}
	e.w.Cancel()
	e.done(t, ch)
}

// One sheet at a time.
func TestBusy(t *testing.T) {
	var calls []string
	e := newEnv(t, ops.PlatformIOS, nil)
	ch := e.pay(t, order(&calls))
	if res := e.done(t, e.payNow(order(&calls))); !errors.Is(res.err, pay.ErrBusy) {
		t.Fatalf("second Pay: %v", res.err)
	}
	e.w.Cancel()
	e.done(t, ch)
}

// The button is the platform's wallet's: its kind, types and label;
// nothing on Windows and Linux. Its press calls OnPress.
func TestButton(t *testing.T) {
	pressed := 0
	button := func() ui.Node {
		return pay.Button(pay.ButtonProps{Type: pay.Checkout, Style: pay.Black, OnPress: func() { pressed++ }})
	}
	for _, c := range []struct {
		p     ops.Platform
		label string
	}{{ops.PlatformIOS, "Check out with Apple Pay"}, {ops.PlatformAndroid, "Check out with Google Pay"}, {ops.PlatformMacOS, "Check out with Apple Pay"}} {
		e := newEnv(t, c.p, button)
		vs := e.h.OfKind(ops.KindNative)
		if len(vs) != 1 {
			t.Fatalf("%v: %d native views", c.p, len(vs))
		}
		v := vs[0]
		if v.NativeType() != "asuka.payButton" || v.NativeString("label") != c.label || v.Native["appleType"] != int64(5) ||
			v.Native["googleType"] != int64(3) || v.Native["style"] != int64(1) || v.Native["cornerRadius"] != -1.0 ||
			!strings.Contains(v.NativeString("methods"), `"VISA"`) {
			t.Fatalf("%v: %v", c.p, v.Native)
		}
		e.h.EmitNative(v, "press", nil)
		e.r.Flush()
	}
	if pressed != 3 {
		t.Fatalf("pressed %d", pressed)
	}
	for _, p := range []ops.Platform{ops.PlatformWindows, ops.PlatformLinux} {
		e := newEnv(t, p, button)
		if vs := e.h.OfKind(ops.KindNative); len(vs) != 0 {
			t.Fatalf("%v: a button", p)
		}
	}
}

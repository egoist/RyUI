//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"math"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	spec "github.com/egoist/asuka/internal/payspec"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ui"
)

// The pay module is there, and answers what this Mac can do; its
// PKPaymentRequest is the wire's: the networks, capabilities, items (a
// subscription's kind), fields and methods.
func TestPayRequest(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		for _, m := range []string{"available", "setUp", "present", "next", "answer"} {
			if h.module("pay")[m] == nil {
				t.Errorf("pay.%s isn't on macOS", m)
			}
		}
		if builtinViews["asuka.payButton"] == nil {
			t.Error("no asuka.payButton view on macOS")
		}
		if !loadPassKit() {
			t.Skip("no PassKit")
		}
		b, err, ok := callNow(t, h, "pay", "available", []byte(`{"id":"","apple":{"networks":["visa"],"capabilities":["3DS"]}}`))
		var a spec.Availability
		if !ok || err != nil || json.Unmarshal(b, &a) != nil || a.Wallet != "apple" {
			t.Fatalf("available: %s %v", b, err)
		}
		if _, err, _ := callNow(t, h, "pay", "next", []byte("nope")); errText(err) != "stopped" {
			t.Errorf("next with no sheet: %v", err)
		}
		if _, err, _ := callNow(t, h, "pay", "present", []byte(`{"id":"a","appleError":"pay: no merchant"}`)); errText(err) != "pay: no merchant" {
			t.Errorf("present without a merchant: %v", err)
		}

		r := payRequest(spec.AppleRequest{
			Merchant: "merchant.dev.asuka.tea", Country: "US", Currency: "USD",
			Networks: []string{"visa", "mastercard", "amex", "nope"}, Capabilities: []string{"3DS", "credit"},
			Items: []spec.AppleItem{
				{Label: "Tea", Amount: "40.00"},
				{Label: "Club", Amount: "20.00", Kind: "recurring", Interval: "month", Every: 1, StartMs: 1_800_000_000_000},
				{Label: "Tea Shop", Amount: "60.00"},
			},
			ShippingFields: []string{"name", "address", "email"}, BillingFields: []string{"address"},
			ShippingType:    "delivery",
			ShippingMethods: []spec.AppleShippingMethod{{ID: "ground", Label: "Ground", Amount: "0.00", From: "2026-11-20", To: "2026-11-22"}},
			Coupons:         true, Coupon: "TEA10",
			Recurring: &spec.AppleRecurring{Description: "Tea every month", ManagementURL: "https://tea.example/club",
				Regular: spec.AppleItem{Label: "Club", Amount: "20.00", Kind: "recurring", Interval: "month", Every: 1}},
		})
		defer Release(r)
		if GoString(Send(r, "merchantIdentifier")) != "merchant.dev.asuka.tea" || GoString(Send(r, "currencyCode")) != "USD" {
			t.Errorf("merchant %q, currency %q", GoString(Send(r, "merchantIdentifier")), GoString(Send(r, "currencyCode")))
		}
		nets := ArrayItems(Send(r, "supportedNetworks"))
		if len(nets) != 3 || payNetworkName(nets[0]) != "visa" || payNetworkName(nets[1]) != "mastercard" || payNetworkName(nets[2]) != "amex" {
			t.Errorf("networks: %d", len(nets))
		}
		if c := SendInt(r, "merchantCapabilities"); c != 1|4 {
			t.Errorf("capabilities %d", c)
		}
		items := ArrayItems(Send(r, "paymentSummaryItems"))
		if len(items) != 3 || GoString(Send(items[2], "label")) != "Tea Shop" ||
			GoString(Send(Send(items[2], "amount"), "stringValue")) != "60" {
			t.Fatalf("items: %d", len(items))
		}
		if HasClass("PKRecurringPaymentSummaryItem") {
			if !SendBool(items[1], "isKindOfClass:", uintptr(Class("PKRecurringPaymentSummaryItem"))) || SendInt(items[1], "intervalUnit") != 8 {
				t.Error("the recurring item isn't one")
			}
		}
		if RespondsTo(r, "recurringPaymentRequest") && Send(r, "recurringPaymentRequest") == 0 {
			t.Error("no recurring payment request")
		}
		if n := SendInt(Send(r, "requiredShippingContactFields"), "count"); n != 3 {
			t.Errorf("shipping fields: %d", n)
		}
		if SendInt(r, "shippingType") != 1 {
			t.Errorf("shipping type %d", SendInt(r, "shippingType"))
		}
		ms := ArrayItems(Send(r, "shippingMethods"))
		if len(ms) != 1 || GoString(Send(ms[0], "identifier")) != "ground" {
			t.Errorf("methods: %d", len(ms))
		}
		if RespondsTo(r, "couponCode") && GoString(Send(r, "couponCode")) != "TEA10" {
			t.Errorf("coupon %q", GoString(Send(r, "couponCode")))
		}

		// Errors are PassKit's, by their fields.
		errs := ArrayItems(payErrors([]spec.FieldError{{Field: "shippingPostalCode", Message: "ZIP?"}, {Field: "unserviceable", Message: "No"},
			{Field: "payment", Message: "Declined"}, {Field: "billingName", Message: "Name?"}}))
		if len(errs) != 4 {
			t.Fatalf("errors: %d", len(errs))
		}
		domain := GoString(Const(framework("PassKit"), "PKPaymentErrorDomain"))
		for i, want := range []int{1, 3, -1, 2} { // shippingContactInvalid, shippingAddressUnserviceable, unknown, billingContactInvalid
			if GoString(Send(errs[i], "domain")) != domain || SendInt(errs[i], "code") != want {
				t.Errorf("error %d: %s %d, want %d", i, GoString(Send(errs[i], "domain")), SendInt(errs[i], "code"), want)
			}
		}
		if GoString(Send(errs[2], "localizedDescription")) != "Declined" {
			t.Errorf("message %q", GoString(Send(errs[2], "localizedDescription")))
		}

		// A contact, as the sheet gives it.
		c := Autorelease(New("PKContact"))
		n := Autorelease(New("NSPersonNameComponents"))
		Send(n, "setGivenName:", uintptr(String("Ada")))
		Send(n, "setFamilyName:", uintptr(String("Lovelace")))
		Send(c, "setName:", uintptr(n))
		addr := Autorelease(New("CNMutablePostalAddress"))
		Send(addr, "setStreet:", uintptr(String("1 Infinite Loop")))
		Send(addr, "setPostalCode:", uintptr(String("95014")))
		Send(addr, "setISOCountryCode:", uintptr(String("US")))
		Send(c, "setPostalAddress:", uintptr(addr))
		Send(c, "setEmailAddress:", uintptr(String("ada@example.com")))
		pc := payContact(c)
		if pc.GivenName != "Ada" || pc.FamilyName != "Lovelace" || pc.Name == "" || pc.Email != "ada@example.com" ||
			pc.Address == nil || pc.Address.Street != "1 Infinite Loop" || pc.Address.CountryCode != "US" {
			t.Errorf("contact: %+v %+v", pc, pc.Address)
		}
	})
}

// The button is PassKit's, made again as its type and style change; a
// click presses it.
func TestPayButton(t *testing.T) {
	_, _ = mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if !loadPassKit() {
			t.Skip("no PassKit")
		}
		var pressed int
		v := builtinViews["asuka.payButton"](native.NewContext(0, func(name string, _ map[string]any) {
			if name == "press" {
				pressed++
			}
		}))
		if v == nil {
			fatal(t, "no Apple Pay button")
		}
		b := v.(*payButton)
		v.Set("appleType", int64(1))
		v.Set("style", int64(1))
		v.Set("cornerRadius", 8.0)
		if !SendBool(b.button, "isKindOfClass:", uintptr(Class("PKPaymentButton"))) {
			fatal(t, "the button isn't PassKit's")
		}
		if RespondsTo(b.button, "cornerRadius") {
			if r := MsgFloat(b.button, Sel("cornerRadius")); math.Abs(r-8) > 0.01 {
				t.Errorf("corner radius %v", r)
			}
		}
		if w, ht := v.(native.Measurer).Measure(float32(math.Inf(1)), 0); w != 200 || ht != 30 {
			t.Errorf("measured %v×%v", w, ht)
		}
		if w, _ := v.(native.Measurer).Measure(80, 0); w != 140 {
			t.Errorf("narrow: %v, want Apple's least, 140", w)
		}
		SendBool(b.button, "sendAction:to:", uintptr(Send(b.button, "action")), uintptr(Send(b.button, "target")))
		if pressed != 1 {
			t.Errorf("a click pressed it %d times", pressed)
		}
		v.(native.Destroyer).Destroy()
		if pay.buttons[b.box] != nil {
			t.Error("the destroyed button is still known")
		}
	})
}

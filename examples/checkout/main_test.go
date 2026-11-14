package main

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/pay"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/uitest"
)

var ada = fake.PayContact{GivenName: "Ada", FamilyName: "Lovelace", Email: "ada@example.com",
	Lines: []string{"1 Infinite Loop"}, City: "Cupertino", State: "CA", PostalCode: "95014", CountryCode: "US"}

// mount is the app on p with a wallet the test drives.
func mount(t *testing.T, p ui.PlatformKind) (*uitest.App, *fake.Wallet) {
	var w *fake.Wallet
	a := uitest.Mount(t, Checkout, uitest.Platform(p), uitest.Setup(func(h *fake.Host) { w = h.Wallet() }))
	return a, w
}

// answered waits for the app's answer to what the wallet's user did.
func answered(a *uitest.App, w *fake.Wallet) {
	a.Until("the app's answer", func() bool { return !w.Waiting() })
}

// On Apple Pay: the tax follows the address, the shipping the method, a
// bad coupon shows by its field, a declined card in the sheet; the
// payment that goes through thanks the user.
func TestApplePay(t *testing.T) {
	a, w := mount(t, ui.PlatformIOS)
	a.WaitFor("Buy with Apple Pay")
	a.Tap("+")
	a.Tap("Buy with Apple Pay")
	a.Until("the sheet", func() bool { return w.Sheet != nil })
	s := w.Sheet
	if s.Total.Amount != "80.00" || s.Total.Label != "Asuka Tea" || s.Items[0].Label != "Tea set × 2" {
		t.Fatalf("sheet: %+v", s)
	}
	w.ChangeShippingContact(ada)
	answered(a, w)
	if s.Total.Amount != "86.40" { // 8% in California
		t.Fatalf("with California's tax: %+v", s.Total)
	}
	w.ChooseShippingMethod("express")
	answered(a, w)
	if s.Total.Amount != "95.40" {
		t.Fatalf("express: %+v", s.Total)
	}
	w.EnterCoupon("FREE")
	answered(a, w)
	if len(s.Errors) != 1 || s.Errors[0].Field != pay.CouponInvalid.String() {
		t.Fatalf("a bad coupon: %+v", s.Errors)
	}
	w.EnterCoupon("tea10")
	answered(a, w)
	if s.Total.Amount != "86.76" || len(s.Errors) != 0 { // 80 - 8 + 9 + 8% of 72
		t.Fatalf("TEA10: %+v %+v", s.Total, s.Errors)
	}
	card := fake.PayCard{Network: "visa", Kind: "credit", Name: "Visa 1234"}
	w.Authorize(fake.PayAuthorization{Token: "declined", Card: card, Shipping: &ada})
	answered(a, w)
	if w.Sheet == nil || len(s.Errors) != 1 || s.Errors[0].Message != "Your card was declined. Try another." {
		t.Fatalf("declined: %+v", s.Errors)
	}
	w.Authorize(fake.PayAuthorization{Token: `{"data":"…"}`, Card: card, Shipping: &ada})
	a.WaitFor("Thanks, Ada! Your tea is on its way (express).")
	if w.Sheet != nil || !s.Authorized {
		t.Fatal("the sheet stayed")
	}
}

// On Google Pay: the same order, through Google's callbacks.
func TestGooglePay(t *testing.T) {
	a, w := mount(t, ui.PlatformAndroid)
	a.WaitFor("Buy with Google Pay")
	a.Tap("Buy with Google Pay")
	a.Until("the sheet", func() bool { return w.Sheet != nil })
	w.ChangeShippingContact(ada)
	answered(a, w)
	if w.Sheet.Total.Amount != "43.20" {
		t.Fatalf("with California's tax: %+v", w.Sheet.Total)
	}
	abroad := ada
	abroad.CountryCode = "FR"
	w.ChangeShippingContact(abroad)
	answered(a, w)
	if e := w.Sheet.Errors; len(e) != 1 || e[0].Field != "unserviceable" {
		t.Fatalf("abroad: %+v", e)
	}
	w.Authorize(fake.PayAuthorization{Token: "tok_visa", Card: fake.PayCard{Network: "visa", Kind: "debit", Name: "Visa •••• 4242"}, Shipping: &ada})
	a.WaitFor("Thanks, Ada! Your tea is on its way (ground).")
}

// Closed without paying: nothing said. Without a wallet (a desktop, a
// browser with neither), no button.
func TestCanceledAndNoWallet(t *testing.T) {
	a, w := mount(t, ui.PlatformIOS)
	a.WaitFor("Buy with Apple Pay")
	a.Tap("Buy with Apple Pay")
	a.Until("the sheet", func() bool { return w.Sheet != nil })
	w.Cancel()
	a.Until("Pay's return", func() bool { return w.Sheet == nil })
	a.ExpectNo("Thanks, Ada! Your tea is on its way (ground).")

	var none *fake.Wallet
	b := uitest.Mount(t, Checkout, uitest.Platform(ui.PlatformWindows), uitest.Setup(func(h *fake.Host) {
		none = h.Wallet()
		none.Unavailable = true
	}))
	b.WaitFor("This device has no wallet to pay with.")
	b.ExpectNo("Buy with Apple Pay", "Buy with Google Pay")
}

// Apple Pay without a card: its Set Up button, which opens Wallet's.
func TestSetUpApplePay(t *testing.T) {
	var w *fake.Wallet
	a := uitest.Mount(t, Checkout, uitest.Platform(ui.PlatformIOS), uitest.Setup(func(h *fake.Host) {
		w = h.Wallet()
		w.NoCard = true
	}))
	a.WaitFor("Set up Apple Pay")
	a.Tap("Set up Apple Pay")
	a.Until("setUp", func() bool { return w.SetUps == 1 })
}

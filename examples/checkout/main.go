// Checkout sells a tea set with the platform's wallet (asuka/sdk/pay):
// Apple Pay on iOS, macOS and in Safari, Google Pay on Android and in
// the other browsers. The wallet's sheet asks for a shipping address and
// a method; the tax follows the address, the shipping the method, and
// the coupon TEA10 takes 10% off (Apple Pay). Its server is pretend: it
// accepts any token but "declined".
//
//	go run ./cmd/asuka dev --platform ios examples/checkout      # the simulator's Apple Pay, with its test cards
//	go run ./cmd/asuka dev --platform android examples/checkout  # Google Pay's TEST environment
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/pay"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

const price = 4000 // cents

var methods = []pay.ShippingMethod{
	{ID: "ground", Label: "Ground", Detail: "Arrives in 5 to 7 days", Amount: "0.00"},
	{ID: "express", Label: "Express", Detail: "Arrives tomorrow", Amount: "9.00"},
}

// taxRates are the states' sales tax, in basis points: a pretend table.
var taxRates = map[string]int{"CA": 800, "NY": 400, "WA": 650}

func money(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// order is what the sheet shows as the user changes it.
type order struct {
	quantity              int
	state, method, coupon string
}

func (o *order) shipping() int {
	if o.method == "express" {
		return 900
	}
	return 0
}

func (o *order) discount() int {
	if o.coupon == "TEA10" {
		return o.quantity * price / 10
	}
	return 0
}

func (o *order) tax() int { return (o.quantity*price - o.discount()) * taxRates[o.state] / 10000 }

func (o *order) total() int { return o.quantity*price - o.discount() + o.shipping() + o.tax() }

func (o *order) update() pay.Update {
	return pay.Update{Items: o.items(), Total: pay.Item{Amount: money(o.total())}}
}

func (o *order) items() []pay.Item {
	items := []pay.Item{{Label: fmt.Sprintf("Tea set × %d", o.quantity), Amount: money(o.quantity * price)}}
	if d := o.discount(); d > 0 {
		items = append(items, pay.Item{Label: "Coupon TEA10", Amount: money(-d), Kind: pay.Discount})
	}
	return append(items,
		pay.Item{Label: "Shipping", Amount: money(o.shipping()), Kind: pay.ShippingCost},
		pay.Item{Label: "Tax", Amount: money(o.tax()), Kind: pay.Tax})
}

// charge is the app's server: it would charge the token with the
// processor (Stripe, Adyen, Braintree...). Here: any token but
// "declined".
var charge = func(p pay.Payment, cents int) error {
	if p.Token == "declined" {
		return errors.New("Your card was declined. Try another.")
	}
	return nil
}

// request is the payment for quantity sets.
func request(quantity int) pay.Request {
	o := &order{quantity: quantity, method: methods[0].ID}
	return pay.Request{
		Country: "US", Currency: "USD",
		Items: o.items(), Total: pay.Item{Label: "Asuka Tea", Amount: money(o.total())},
		Shipping: pay.Name | pay.Address | pay.Email, ShippingMethods: methods, ShippingCountries: []string{"US"},
		Coupons: true,
		Apple:   pay.Apple{MerchantID: "merchant.dev.asuka.checkout"},
		// Google's example gateway: TEST tokens, for any app.
		Google: pay.Google{Gateway: "example", GatewayParams: map[string]string{"gatewayMerchantId": "exampleGatewayMerchantId"}},
		OnShippingContact: func(c pay.Contact) (pay.Update, error) {
			if c.Address.CountryCode != "" && c.Address.CountryCode != "US" {
				return pay.Update{}, pay.Invalid(pay.Unserviceable, "We ship in the United States only.")
			}
			o.state = strings.ToUpper(c.Address.State)
			return o.update(), nil
		},
		OnShippingMethod: func(m pay.ShippingMethod) (pay.Update, error) {
			o.method = m.ID
			return o.update(), nil
		},
		OnCoupon: func(code string) (pay.Update, error) {
			if code = strings.ToUpper(strings.TrimSpace(code)); code != "" && code != "TEA10" {
				o.coupon = ""
				return o.update(), pay.Invalid(pay.CouponInvalid, "That coupon isn't ours.")
			}
			o.coupon = code
			return o.update(), nil
		},
		OnAuthorize: func(p pay.Payment) error {
			if p.Shipping.Address.PostalCode == "" {
				return pay.Invalid(pay.ShippingPostalCode, "Enter a ZIP code.")
			}
			return charge(p, o.total())
		},
	}
}

func Checkout() ui.Node {
	quantity := signal.New(1)
	wallet := signal.New(pay.Availability{})
	status := signal.New("")
	paying := signal.New(false)
	go func() {
		a := pay.Available(request(1))
		ui.Post(func() { wallet.Set(a) })
	}()
	buy := func() {
		if paying.Peek() {
			return
		}
		paying.Set(true)
		status.Set("")
		r := request(quantity.Peek())
		go func() {
			p, err := pay.Pay(r)
			ui.Post(func() {
				paying.Set(false)
				switch {
				case err == nil:
					status.Set(fmt.Sprintf("Thanks, %s! Your tea is on its way (%s).", firstName(p.Shipping), p.ShippingMethod))
				case errors.Is(err, pay.ErrCanceled):
				default:
					status.Set(err.Error())
				}
			})
		}()
	}
	return ui.SafeArea(ui.Style{Padding: ui.All(24), Gap: 16, Justify: ui.JustifyCenter},
		ui.StaticText("Tea set", ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
		ui.StaticText("A pot and four cups in celadon.", ui.Style{Color: colors.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Button(ui.ButtonProps{Title: ui.Val("−"), Variant: ui.ButtonGray,
				OnPress: func() { quantity.Update(func(n int) int { return max(n-1, 1) }) }}),
			ui.Text(signal.Func(func() string {
				return fmt.Sprintf("%d × $%s", quantity.Get(), money(price))
			}), ui.Style{Font: fonts.Headline}),
			ui.Button(ui.ButtonProps{Title: ui.Val("+"), Variant: ui.ButtonGray,
				OnPress: func() { quantity.Update(func(n int) int { return n + 1 }) }}),
		),
		ui.Show(signal.Func(func() bool { return wallet.Get().CanPay }),
			func() ui.Node {
				// Apple's guidelines: Set Up Apple Pay where the user has no
				// card yet (its sheet adds one too).
				t := pay.Buy
				if w := wallet.Peek(); w.Wallet == pay.ApplePay && !w.HasCard {
					t = pay.SetUpButton
				}
				return pay.Button(pay.ButtonProps{Type: t, Style: pay.Automatic, OnPress: func() {
					if t == pay.SetUpButton {
						go pay.SetUp()
						return
					}
					buy()
				}})
			},
			func() ui.Node {
				return ui.StaticText("This device has no wallet to pay with.", ui.Style{Color: colors.SecondaryLabel})
			}),
		ui.Text(status, ui.Style{Color: colors.SecondaryLabel}),
	)
}

func firstName(c pay.Contact) string {
	if c.GivenName != "" {
		return c.GivenName
	}
	if f := strings.Fields(c.Name); len(f) > 0 {
		return f[0]
	}
	return "friend"
}

func main() { app.Run(Checkout) }

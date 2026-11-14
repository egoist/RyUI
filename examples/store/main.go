// Store sells in the app (asuka/sdk/purchase): coins, a consumable bought
// again and again; Pro, a non-consumable kept for life; and Plus, a
// subscription that renews each month, with a week's free trial.
//
// The stores sell only an app they know, so the player in asuka dev
// can't: on the iOS simulator, run it built, selling from its StoreKit
// configuration file (Products.storekit, asuka.json's ios.storekit):
//
//	go run ./cmd/asuka dev --platform ios --no-hot examples/store
//
// On Android, upload a build to Play's internal track with the same
// products, and install it as a license tester.
package main

import (
	"fmt"
	"strings"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/purchase"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// What the app sells: their IDs in App Store Connect and Play Console.
var (
	coins = purchase.Item{ID: "dev.asuka.store.coins100", Kind: purchase.Consumable}
	pro   = purchase.Item{ID: "dev.asuka.store.pro", Kind: purchase.NonConsumable}
	plus  = purchase.Item{ID: "dev.asuka.store.plus.monthly", Kind: purchase.AutoRenewable}
)

// coinsPer is what a coins purchase adds.
const coinsPer = 100

func Store() ui.Node {
	purchase.Declare(coins, pro, plus)
	balance := signal.New(0)
	status := signal.New("")
	products := signal.NewFunc[[]purchase.Product](nil, func(a, b []purchase.Product) bool { return false })
	busy := signal.New(false)
	owned := purchase.Use()

	// deliver gives what a transaction sold, then finishes it. The coins
	// here are the app's to keep (a server's, an app's own storage).
	deliver := func(t *purchase.Transaction) {
		switch {
		case t.State == purchase.Pending:
			status.Set("Waiting for approval…")
			return
		case t.State == purchase.Revoked:
			status.Set("A purchase was refunded")
		case t.ProductID == coins.ID:
			balance.Update(func(n int) int { return n + coinsPer*t.Quantity })
			status.Set(fmt.Sprintf("+%d coins", coinsPer*t.Quantity))
		default:
			status.Set("Thanks!")
		}
		go t.Finish()
	}
	// From the start: Ask to Buy approved, a renewal, a purchase on
	// another device, those left unfinished last time.
	signal.OnCleanup(purchase.Observe(func(e purchase.Event) {
		if e.Transaction != nil {
			deliver(e.Transaction)
		}
	}))

	// run does a store call off the main thread, and shows its error.
	run := func(f func() error) {
		busy.Set(true)
		go func() {
			err := f()
			ui.Post(func() {
				busy.Set(false)
				if err != nil {
					status.Set(err.Error())
				}
			})
		}()
	}
	run(func() error {
		ps, err := purchase.Products(coins, pro, plus)
		ui.Post(func() { products.Set(ps) })
		return err
	})
	buy := func(p purchase.Product) {
		run(func() error {
			r, err := purchase.Buy(p.Item, purchase.Options{})
			ui.Post(func() {
				switch {
				case err != nil:
				case r.Status == purchase.Bought:
					deliver(r.Transaction)
				case r.Status == purchase.PendingApproval:
					status.Set("Waiting for approval…")
				default:
					status.Set("")
				}
			})
			return err
		})
	}

	return ui.SafeArea(ui.Style{},
		ui.Scroll(ui.ScrollProps{ContentStyle: ui.Style{Gap: 16, Padding: ui.All(20)}},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("%d coins", balance.Get()) }),
				ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
			ui.Show(signal.Func(func() bool { return owned.Get().Has(pro.ID) || owned.Get().Has(plus.ID) }),
				func() ui.Node {
					return ui.Text(signal.Func(func() string {
						var has []string
						if owned.Get().Has(pro.ID) {
							has = append(has, "Pro")
						}
						if owned.Get().Has(plus.ID) {
							has = append(has, "Plus")
						}
						return "You have " + strings.Join(has, " and ")
					}), ui.Style{Color: colors.SecondaryLabel})
				}),
			ui.For(products, func(p purchase.Product) string { return p.ID }, func(p signal.Accessor[purchase.Product], _ signal.Accessor[int]) ui.Node {
				return ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
					ui.Column(ui.Style{Grow: 1, Gap: 2},
						ui.Text(signal.Func(func() string { return p.Get().Title }), ui.Style{Font: fonts.Headline}),
						ui.Text(signal.Func(func() string { return describe(p.Get()) }), ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel}),
					),
					ui.Button(ui.ButtonProps{
						Title: signal.Func(func() string {
							if p.Get().Kind != purchase.Consumable && owned.Get().Has(p.Get().ID) {
								return "Owned"
							}
							return p.Get().Price
						}),
						Variant:  ui.ButtonFilled,
						Disabled: signal.Func(func() bool { return busy.Get() || p.Get().Kind != purchase.Consumable && owned.Get().Has(p.Get().ID) }),
						OnPress:  func() { buy(signal.Untrack(p.Get)) },
					}),
				)
			}),
			ui.Text(status, ui.Style{Color: colors.SecondaryLabel}),
			ui.Row(ui.Style{Gap: 12},
				ui.Button(ui.ButtonProps{
					Title:    ui.Val("Restore Purchases"),
					Variant:  ui.ButtonGray,
					Disabled: busy,
					OnPress: func() {
						run(func() error {
							_, err := purchase.Restore()
							return err
						})
					},
				}),
				ui.Button(ui.ButtonProps{
					Title:   ui.Val("Manage"),
					Variant: ui.ButtonGray,
					OnPress: func() { run(func() error { return purchase.ManageSubscriptions(plus.ID) }) },
				}),
			),
		),
	)
}

// describe is a product's line under its title: a subscription's period
// and its trial.
func describe(p purchase.Product) string {
	s := p.Subscription
	if s == nil {
		return p.Description
	}
	line := p.Price + " a " + s.Period.Unit.String()
	if s.Period.Count > 1 {
		line = fmt.Sprintf("%s every %d %ss", p.Price, s.Period.Count, s.Period.Unit)
	}
	for _, o := range s.Offers {
		if s.IntroEligible && o.Type == purchase.Introductory && len(o.Phases) > 0 && o.Phases[0].Mode == purchase.FreeTrial {
			line += fmt.Sprintf(", first %d %s free", o.Phases[0].Period.Count, o.Phases[0].Period.Unit)
		}
	}
	return line
}

func main() { app.Run(Store) }

package main

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/uitest"
)

// The store the tests sell from: the app's three products.
func products(h *fake.Host) *fake.Store {
	s := h.Store()
	s.Products = []fake.StoreProduct{
		{ID: coins.ID, Kind: "consumable", Title: "100 Coins", Price: "$0.99", PriceMicros: 990_000},
		{ID: pro.ID, Kind: "nonConsumable", Title: "Pro", Description: "Every level, for life", Price: "$9.99", PriceMicros: 9_990_000},
		{ID: plus.ID, Kind: "autoRenewable", Title: "Plus", Price: "$4.99", PriceMicros: 4_990_000, Period: "P1M", Trial: "P1W"},
	}
	return s
}

// Coins are bought again and again, each purchase delivered and finished
// (consumed); Pro once, then owned.
func TestBuy(t *testing.T) {
	var store *fake.Store
	a := uitest.Mount(t, Store, uitest.Setup(func(h *fake.Host) { store = products(h) }))
	a.WaitFor("100 Coins")
	a.Expect("0 coins", "$4.99 a month, first 1 week free")
	a.Tap("$0.99")
	a.WaitFor("100 coins")
	a.Tap("$0.99")
	a.WaitFor("200 coins")
	a.Until("finished", func() bool { return len(store.Unfinished()) == 0 })
	for _, tx := range store.Transactions {
		if !tx.Consumed {
			t.Fatalf("not consumed: %+v", tx)
		}
	}
	a.Tap("$9.99")
	a.WaitFor("You have Pro")
	a.WaitFor("Owned")
	if !a.Disabled("Owned") {
		t.Fatal("Pro can be bought twice")
	}
}

// Ask to Buy: pending, then approved by the parent, delivered through
// Observe; a subscription renewed outside the app is too; a refund takes
// Pro back.
func TestOutside(t *testing.T) {
	var store *fake.Store
	a := uitest.Mount(t, Store, uitest.Setup(func(h *fake.Host) { store = products(h) }))
	a.WaitFor("100 Coins")
	store.Outcome = fake.PurchasePending
	a.Tap("$0.99")
	a.WaitFor("Waiting for approval…")
	store.Approve(coins.ID)
	a.WaitFor("100 coins")

	store.Outcome = fake.PurchaseBought
	a.Tap("$4.99")
	a.WaitFor("You have Plus")
	store.Renew(plus.ID)
	a.WaitFor("Thanks!")

	gift := store.Deliver(pro.ID) // bought on another device
	a.WaitFor("You have Pro and Plus")
	store.Refund(gift.ID)
	a.WaitFor("A purchase was refunded")
	a.WaitFor("You have Plus")
}

// What was bought before the app started, and not finished, is delivered
// as it starts.
func TestUnfinished(t *testing.T) {
	a := uitest.Mount(t, Store, uitest.Setup(func(h *fake.Host) {
		s := products(h)
		s.Deliver(coins.ID)
	}))
	a.WaitFor("100 coins")
}

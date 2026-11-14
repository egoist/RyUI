package purchase_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/purchase"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var (
	coins   = purchase.Item{ID: "coins_100", Kind: purchase.Consumable}
	pro     = purchase.Item{ID: "pro", Kind: purchase.NonConsumable}
	monthly = purchase.Item{ID: "plus_monthly", Kind: purchase.AutoRenewable}
	missing = purchase.Item{ID: "nope", Kind: purchase.NonConsumable}
)

type env struct {
	h     *fake.Host
	r     *ui.Root
	store *fake.Store
}

func newEnv(t *testing.T, root func() ui.Node) *env {
	t.Helper()
	e := &env{h: fake.New()}
	e.store = e.h.Store()
	e.store.Products = []fake.StoreProduct{
		{ID: "coins_100", Kind: "consumable", Title: "100 Coins", Price: "$0.99", PriceMicros: 990_000},
		{ID: "pro", Kind: "nonConsumable", Title: "Pro", Description: "Everything, for life", Price: "$9.99", PriceMicros: 9_990_000},
		{ID: "plus_monthly", Kind: "autoRenewable", Title: "Plus", Price: "$4.99", PriceMicros: 4_990_000, Group: "plus", Level: 2, Period: "P1M", Trial: "P1W"},
	}
	if root == nil {
		root = func() ui.Node { return ui.StaticText("x") }
	}
	e.r = ui.Mount(e.h, root)
	t.Cleanup(e.r.Dispose)
	return e
}

// wait runs the main thread until cond holds.
func (e *env) wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 5000 { // a safety net, not what passes
		e.h.RunPosted()
		e.r.Flush()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiting for %s; store:\n%s", what, e.store)
}

// do runs f off the main thread (the package's calls wait), running the
// main thread until it's done.
func (e *env) do(t *testing.T, f func()) {
	t.Helper()
	done := make(chan bool)
	go func() { f(); close(done) }()
	e.wait(t, "the call", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}

// Products come with their kinds, prices and subscriptions' periods and
// offers; those the store doesn't sell are left out.
func TestProducts(t *testing.T) {
	e := newEnv(t, nil)
	var ps []purchase.Product
	var err error
	var ok bool
	e.do(t, func() {
		ok = purchase.Available()
		ps, err = purchase.Products(coins, pro, monthly, missing)
	})
	if !ok || err != nil || len(ps) != 3 {
		t.Fatalf("Available %v; Products %+v, %v", ok, ps, err)
	}
	c, p, m := ps[0], ps[1], ps[2]
	if c.ID != "coins_100" || c.Kind != purchase.Consumable || c.Price != "$0.99" || c.PriceMicros != 990_000 || c.Currency != "USD" || c.Subscription != nil {
		t.Errorf("coins: %+v", c)
	}
	if p.Kind != purchase.NonConsumable || p.Description != "Everything, for life" {
		t.Errorf("pro: %+v", p)
	}
	s := m.Subscription
	if m.Kind != purchase.AutoRenewable || s == nil || s.Group != "plus" || s.Level != 2 || s.Period != (purchase.Period{Unit: purchase.Month, Count: 1}) || !s.IntroEligible {
		t.Fatalf("monthly: %+v %+v", m, s)
	}
	if len(s.Offers) != 1 || s.Offers[0].Type != purchase.Introductory || s.Offers[0].Phases[0].Mode != purchase.FreeTrial ||
		s.Offers[0].Phases[0].Period != (purchase.Period{Unit: purchase.Week, Count: 1}) {
		t.Errorf("offers: %+v", s.Offers)
	}
}

// A consumable bought is delivered and finished (consumed): it can be
// bought again, and it's never an entitlement. A non-consumable bought is
// an entitlement, and can't be bought twice.
func TestBuyAndFinish(t *testing.T) {
	e := newEnv(t, nil)
	var r purchase.Result
	var err error
	e.do(t, func() {
		r, err = purchase.Buy(coins, purchase.Options{Quantity: 2, AccountToken: "5f1c2a3e-0000-4000-8000-000000000001"})
	})
	if err != nil || r.Status != purchase.Bought || r.Transaction == nil {
		t.Fatalf("Buy: %+v, %v", r, err)
	}
	tx := r.Transaction
	if tx.ProductID != "coins_100" || tx.Kind != purchase.Consumable || tx.State != purchase.Purchased || tx.Quantity != 2 ||
		tx.AccountToken != "5f1c2a3e-0000-4000-8000-000000000001" || tx.Verification.JWS == "" || tx.Verification.Token == "" || tx.Date.IsZero() {
		t.Fatalf("transaction: %+v", tx)
	}
	if len(e.store.Unfinished()) != 1 {
		t.Fatalf("unfinished: %v", e.store)
	}
	e.do(t, func() { err = tx.Finish() })
	if err != nil || !e.store.Transactions[0].Finished || !e.store.Transactions[0].Consumed || len(e.store.Unfinished()) != 0 {
		t.Fatalf("Finish: %v\n%s", err, e.store)
	}
	e.do(t, func() { r, err = purchase.Buy(coins, purchase.Options{}) })
	if err != nil || r.Status != purchase.Bought {
		t.Fatalf("again: %+v, %v", r, err)
	}

	e.do(t, func() {
		r, err = purchase.Buy(pro, purchase.Options{})
		if err == nil {
			err = r.Transaction.Finish()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var ents purchase.Entitlements
	e.do(t, func() {
		ents, err = purchase.CurrentEntitlements()
		_, err2 := purchase.Buy(pro, purchase.Options{})
		if !errors.Is(err2, purchase.ErrAlreadyOwned) {
			t.Errorf("twice: %v", err2)
		}
		_, err2 = purchase.Buy(missing, purchase.Options{})
		if !errors.Is(err2, purchase.ErrUnknownProduct) {
			t.Errorf("missing: %v", err2)
		}
	})
	if err != nil || len(ents) != 1 || !ents.Has("pro") || ents.Has("coins_100") {
		t.Fatalf("entitlements: %v, %v", ents, err)
	}
}

// Cancelled and failed purchases; no store; payments not allowed.
func TestOutcomes(t *testing.T) {
	e := newEnv(t, nil)
	e.store.Outcome = fake.PurchaseCancelled
	var r purchase.Result
	var err error
	e.do(t, func() { r, err = purchase.Buy(pro, purchase.Options{}) })
	if err != nil || r.Status != purchase.Cancelled || r.Transaction != nil {
		t.Fatalf("cancelled: %+v, %v", r, err)
	}
	e.store.Outcome = fake.PurchaseFailed
	e.do(t, func() { _, err = purchase.Buy(pro, purchase.Options{}) })
	if err == nil {
		t.Fatal("failed: no error")
	}
	e.store.NotAllowed = true
	var ok bool
	e.do(t, func() { ok = purchase.Available(); _, err = purchase.Buy(pro, purchase.Options{}) })
	if ok || !errors.Is(err, purchase.ErrNotAllowed) {
		t.Fatalf("not allowed: %v, %v", ok, err)
	}
	e.store.Unavailable = true
	e.do(t, func() { ok = purchase.Available(); _, err = purchase.Products(pro) })
	if ok || !errors.Is(err, purchase.ErrNotAvailable) {
		t.Fatalf("unavailable: %v, %v", ok, err)
	}
}

// A host without the module (Linux, the player) has no store.
func TestNoStore(t *testing.T) {
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	var err error
	var ok bool
	done := make(chan bool)
	go func() {
		ok = purchase.Available()
		_, err = purchase.CurrentEntitlements()
		close(done)
	}()
	for {
		h.RunPosted()
		select {
		case <-done:
			if ok || !errors.Is(err, purchase.ErrNotAvailable) {
				t.Fatalf("Available %v, CurrentEntitlements %v", ok, err)
			}
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// Ask to Buy: Buy answers pending; the approval comes through Observe,
// is delivered and finished. Purchases outside the app, renewals and
// refunds come through Observe too, and what came before the app
// observed is kept for it.
func TestObserve(t *testing.T) {
	e := newEnv(t, nil)
	purchase.Declare(coins, pro, monthly)
	gift := e.store.Deliver("pro") // before anyone observes: kept
	var got []purchase.Event
	stop := purchase.Observe(func(ev purchase.Event) {
		got = append(got, ev)
		if tx := ev.Transaction; tx != nil && tx.State == purchase.Purchased {
			go tx.Finish()
		}
	})
	e.wait(t, "the kept transaction", func() bool { return len(got) == 1 && e.store.Following() })
	if tx := got[0].Transaction; got[0].Type != purchase.TransactionUpdated || tx.ID != gift.ID || tx.ProductID != "pro" || tx.Kind != purchase.NonConsumable {
		t.Fatalf("kept: %+v", got[0])
	}
	e.wait(t, "finished", func() bool { return gift.Finished })

	e.store.Outcome = fake.PurchasePending
	var r purchase.Result
	e.do(t, func() { r, _ = purchase.Buy(coins, purchase.Options{}) })
	if r.Status != purchase.PendingApproval {
		t.Fatalf("pending: %+v", r)
	}
	approved := e.store.Approve("coins_100")
	e.wait(t, "the approval", func() bool { return len(got) == 2 })
	if tx := got[1].Transaction; tx.ID != approved.ID || tx.State != purchase.Purchased || tx.Kind != purchase.Consumable {
		t.Fatalf("approved: %+v", tx)
	}
	e.wait(t, "consumed", func() bool { return approved.Consumed })

	e.store.Outcome = fake.PurchaseBought
	e.do(t, func() {
		r, _ = purchase.Buy(monthly, purchase.Options{})
		r.Transaction.Finish()
	})
	renewal := e.store.Renew("plus_monthly")
	e.wait(t, "the renewal", func() bool { return len(got) == 3 })
	if tx := got[2].Transaction; tx.ID != renewal.ID || tx.OriginalID != r.Transaction.ID || !tx.Expires.After(r.Transaction.Expires) {
		t.Fatalf("renewal: %+v (first %+v)", tx, r.Transaction)
	}
	e.store.Refund(gift.ID)
	e.wait(t, "the refund", func() bool { return len(got) == 4 })
	if tx := got[3].Transaction; tx.ID != gift.ID || tx.State != purchase.Revoked || tx.Revoked.IsZero() {
		t.Fatalf("refund: %+v", tx)
	}
	e.store.SetCountry("FRA")
	e.wait(t, "the storefront", func() bool { return len(got) == 5 })
	if got[4].Type != purchase.StorefrontChanged || got[4].Storefront.Country != "FRA" {
		t.Fatalf("storefront: %+v", got[4])
	}
	stop()
	e.wait(t, "the poll to stop", func() bool { return !e.store.Following() })
}

// Use follows the entitlements: asked as it mounts, again at each
// transaction; it stops with its owner.
func TestUse(t *testing.T) {
	var ents signal.Accessor[purchase.Entitlements]
	e := newEnv(t, nil)
	var dispose func()
	signal.Root(func(d func()) { dispose = d; ents = purchase.Use() })
	e.wait(t, "the first answer", func() bool { return ents.Get() != nil })
	if len(ents.Get()) != 0 {
		t.Fatalf("entitlements: %v", ents.Get())
	}
	// Use doesn't observe: what's kept is the app's Observe's.
	if e.store.Following() {
		t.Fatal("Use polls the store")
	}
	var got []purchase.Event
	stop := purchase.Observe(func(ev purchase.Event) { got = append(got, ev) })
	defer stop()
	e.store.Deliver("pro")
	e.wait(t, "pro", func() bool { return ents.Get().Has("pro") && len(got) == 1 })
	// A Buy refreshes it too.
	e.do(t, func() { purchase.Buy(monthly, purchase.Options{}) })
	e.wait(t, "plus", func() bool { return ents.Get().Has("plus_monthly") })
	dispose()
}

// A subscription's status; restore, the sheets and the storefront.
func TestSubscriptionAndSheets(t *testing.T) {
	e := newEnv(t, nil)
	var err error
	var sts []purchase.SubscriptionStatus
	var r purchase.Result
	var asked bool
	var sf purchase.Storefront
	e.do(t, func() {
		r, _ = purchase.Buy(monthly, purchase.Options{})
		r.Transaction.Finish()
		sts, err = purchase.SubscriptionStatuses("plus")
	})
	if err != nil || len(sts) != 1 || sts[0].State != purchase.Subscribed || !sts[0].WillRenew || sts[0].Transaction.ID != r.Transaction.ID {
		t.Fatalf("status: %+v, %v", sts, err)
	}
	e.store.Expire("plus_monthly")
	var ents purchase.Entitlements
	e.do(t, func() {
		sts, _ = purchase.SubscriptionStatuses("plus")
		ents, _ = purchase.Restore()
		purchase.ManageSubscriptions("plus_monthly")
		purchase.RedeemCode()
		asked, err = purchase.RequestRefund(r.Transaction)
		sf, _ = purchase.CurrentStorefront()
	})
	if sts[0].State != purchase.Expired || len(ents) != 0 || e.store.Restores != 1 || e.store.Manages != 1 || e.store.Redeems != 1 ||
		!asked || err != nil || len(e.store.Refunds) != 1 || sf.Country != "USA" {
		t.Fatalf("status %+v, ents %v, refund %v %v, storefront %+v, store %+v", sts, ents, asked, err, sf, e.store)
	}
}

// Buy sends its options as the hosts take them: an offer's token, an
// Apple promotional offer's signature, the Play subscription replaced.
func TestBuyOptions(t *testing.T) {
	h := fake.New()
	var args []byte
	h.Modules = map[string]func(string, []byte) ([]byte, error){
		"purchase": func(method string, a []byte) ([]byte, error) {
			if method == "products" {
				return []byte(`{"products":[{"id":"plus","kind":"subs","title":"Plus","price":"$4.99","micros":4990000,"period":{"unit":"month","count":1},
					"offers":[{"type":"base","basePlan":"monthly","token":"tok-base"},{"id":"trial","type":"offer","basePlan":"monthly","token":"tok-trial","tags":["intro"],
					"phases":[{"price":"Free","micros":0,"period":{"unit":"week","count":1},"cycles":1,"mode":"freeTrial"}]}]}]}`), nil
			}
			args = a
			return []byte(`{"status":"purchased","transaction":{"id":"GPA.1","productId":"plus","kind":"subs","state":"purchased","token":"new-token"}}`), nil
		},
	}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	run := func(f func()) {
		done := make(chan bool)
		go func() { f(); close(done) }()
		for {
			h.RunPosted()
			select {
			case <-done:
				return
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}
	var ps []purchase.Product
	var res purchase.Result
	old := &purchase.Transaction{}
	run(func() {
		ps, _ = purchase.Products(purchase.Item{ID: "plus", Kind: purchase.AutoRenewable})
		res, _ = purchase.Buy(ps[0].Item, purchase.Options{AccountToken: "acct", ProfileToken: "prof", Personalized: true,
			Offer: &ps[0].Subscription.Offers[1], Replace: &purchase.Replace{Old: old, Mode: purchase.Deferred},
			Signature: &purchase.OfferSignature{KeyID: "K1", Nonce: "n", Signature: []byte{1, 2, 3}, Timestamp: time.UnixMilli(42)}})
	})
	if ps[0].Kind != purchase.AutoRenewable || ps[0].Subscription.Offers[1].Tags[0] != "intro" || res.Transaction.Kind != purchase.AutoRenewable {
		t.Fatalf("products %+v, result %+v", ps, res)
	}
	var got map[string]any
	json.Unmarshal(args, &got)
	if got["offerToken"] != "tok-trial" || got["offerId"] != "trial" || got["offerType"] != "offer" || got["accountToken"] != "acct" ||
		got["profileToken"] != "prof" || got["personalized"] != true || got["kind"] != "autoRenewable" {
		t.Fatalf("buy args: %s", args)
	}
	sig := got["signature"].(map[string]any)
	if sig["keyId"] != "K1" || sig["signature"] != "AQID" || sig["timestamp"] != 42.0 {
		t.Fatalf("signature: %v", sig)
	}
	if rep := got["replace"].(map[string]any); rep["mode"] != "deferred" {
		t.Fatalf("replace: %v", rep)
	}
	// The replaced subscription's token is the old transaction's.
	run(func() {
		res, _ = purchase.Buy(ps[0].Item, purchase.Options{Replace: &purchase.Replace{Old: res.Transaction}})
	})
	json.Unmarshal(args, &got)
	if rep := got["replace"].(map[string]any); rep["token"] != "new-token" || rep["productId"] != "plus" || rep["mode"] != "withTimeProration" {
		t.Fatalf("replace: %v", rep)
	}
}

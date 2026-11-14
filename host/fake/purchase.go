package fake

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	spec "github.com/egoist/asuka/internal/purchasespec"
)

// Store is a store, as the hosts' "purchase" module sells through it
// (package purchase), for tests: it sells Products, ends each purchase as
// Outcome says, and keeps the transactions as StoreKit does: unfinished
// until the app finishes them, given again (Observe) until it does, a
// consumable gone once finished. Host.Store installs it; without one,
// there's no store (purchase.ErrNotAvailable).
//
//	store := h.Store()
//	store.Products = []fake.StoreProduct{
//		{ID: "coins_100", Kind: "consumable", Title: "100 Coins", Price: "$0.99", PriceMicros: 990000},
//		{ID: "plus_monthly", Kind: "autoRenewable", Title: "Plus", Price: "$4.99", PriceMicros: 4990000, Period: "P1M", Trial: "P1W"},
//	}
//	store.Outcome = fake.PurchasePending // Ask to Buy
//	... the app buys ...
//	store.Approve("coins_100") // the parent approves: the app's Observe gets it
type Store struct {
	// Products are what it sells.
	Products []StoreProduct
	// Outcome is how each purchase ends (default PurchaseBought).
	Outcome PurchaseOutcome
	// Unavailable: no store (purchase.ErrNotAvailable); NotAllowed:
	// payments turned off (purchase.Available false, Buy fails
	// purchase.ErrNotAllowed).
	Unavailable, NotAllowed bool
	// Country is the storefront's (default "USA").
	Country string
	// Transactions are every transaction, in order.
	Transactions []*StoreTransaction
	// Restores, Manages, Redeems count those calls; Refunds are the
	// transactions a refund was asked for.
	Restores, Manages, Redeems int
	Refunds                    []string

	mu        sync.Mutex
	h         *Host
	following bool
	waiting   func([]byte, error)
	queue     []spec.Event
	next      int
}

// StoreProduct is a product the fake store sells.
type StoreProduct struct {
	ID string
	// Kind is "consumable", "nonConsumable", "autoRenewable" or
	// "nonRenewing".
	Kind               string
	Title, Description string
	Price              string
	PriceMicros        int64
	Currency           string // default "USD"
	// Group is a subscription's group (default: its ID), Level its level
	// in it.
	Group string
	Level int
	// Period is a subscription's, ISO 8601's: "P1W", "P1M", "P1Y".
	Period string
	// Trial is an introductory free trial's period, "" for none.
	Trial string
}

// PurchaseOutcome is how the fake store ends a purchase.
type PurchaseOutcome uint8

const (
	// PurchaseBought: bought.
	PurchaseBought PurchaseOutcome = iota
	// PurchasePending: waiting (Ask to Buy): Approve or Decline it.
	PurchasePending
	// PurchaseCancelled: the user cancelled.
	PurchaseCancelled
	// PurchaseFailed: the store failed (Buy's error).
	PurchaseFailed
)

// StoreTransaction is a transaction of the fake store.
type StoreTransaction struct {
	ID, OriginalID, ProductID, Kind string
	// State is "purchased", "pending" or "revoked".
	State        string
	Quantity     int
	Date         time.Time
	Expires      time.Time // a subscription's
	AccountToken string
	// Finished: the app finished it; Consumed, a consumable's.
	Finished, Consumed bool
	// Expired: a subscription whose period ended (Expire).
	Expired bool
	// Outside: bought outside a Buy (Deliver, Renew, Approve).
	Outside bool
}

// Store gives the host a store (the "purchase" module).
func (h *Host) Store() *Store {
	s := &Store{h: h, Country: "USA"}
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["purchase"] = s.call
	return s
}

func (s *Store) product(id string) *StoreProduct {
	for i := range s.Products {
		if s.Products[i].ID == id {
			return &s.Products[i]
		}
	}
	return nil
}

func period(p string) *spec.Period {
	if len(p) < 3 || p[0] != 'P' {
		return nil
	}
	var n int
	fmt.Sscanf(p[1:len(p)-1], "%d", &n)
	unit := map[byte]string{'D': "day", 'W': "week", 'M': "month", 'Y': "year"}[p[len(p)-1]]
	if unit == "" || n < 1 {
		return nil
	}
	return &spec.Period{Unit: unit, Count: n}
}

func addPeriod(t time.Time, p string) time.Time {
	q := period(p)
	if q == nil {
		return time.Time{}
	}
	switch q.Unit {
	case "day":
		return t.AddDate(0, 0, q.Count)
	case "week":
		return t.AddDate(0, 0, 7*q.Count)
	case "month":
		return t.AddDate(0, q.Count, 0)
	}
	return t.AddDate(q.Count, 0, 0)
}

func (p *StoreProduct) wire(s *Store) spec.Product {
	cur := p.Currency
	if cur == "" {
		cur = "USD"
	}
	w := spec.Product{ID: p.ID, Kind: p.Kind, Title: p.Title, Description: p.Description, Price: p.Price,
		Micros: p.PriceMicros, Currency: cur}
	if p.Kind == spec.AutoRenewable || p.Kind == spec.NonRenewing && p.Period != "" {
		w.Group, w.Level, w.Period = p.Group, p.Level, period(p.Period)
		if w.Group == "" {
			w.Group = p.ID
		}
		if p.Trial != "" {
			w.IntroEligible = !s.subscribed(w.Group)
			if tp := period(p.Trial); tp != nil {
				w.Offers = append(w.Offers, spec.Offer{Type: "introductory", Phases: []spec.Phase{
					{Price: "Free", Currency: cur, Period: *tp, Cycles: 1, Mode: "freeTrial"}}})
			}
		}
	}
	return w
}

// subscribed reports whether the user ever subscribed in group.
func (s *Store) subscribed(group string) bool {
	for _, t := range s.Transactions {
		if p := s.product(t.ProductID); p != nil && (p.Group == group || p.ID == group) && t.State != "pending" {
			return true
		}
	}
	return false
}

func (t *StoreTransaction) wire() *spec.Transaction {
	w := &spec.Transaction{ID: t.ID, OriginalID: t.OriginalID, ProductID: t.ProductID, Kind: t.Kind, Quantity: t.Quantity,
		State: t.State, AccountToken: t.AccountToken, Environment: "Xcode", Storefront: "USA",
		JWS: "fake." + t.ID + ".jws", Token: "token-" + t.ID, PackageName: "dev.asuka.fake", Verified: true,
		Acknowledged: t.Finished, AutoRenews: t.Kind == spec.AutoRenewable && !t.Expired}
	if !t.Date.IsZero() {
		w.DateMs = t.Date.UnixMilli()
	}
	if !t.Expires.IsZero() {
		w.ExpiresMs = t.Expires.UnixMilli()
	}
	if t.State == "revoked" {
		w.RevokedMs, w.RevocationReason = t.Date.UnixMilli(), "refundedForIssue"
	}
	return w
}

func (s *Store) newTransaction(p *StoreProduct, quantity int, state string) *StoreTransaction {
	s.next++
	t := &StoreTransaction{ID: fmt.Sprint(2000000000 + s.next), ProductID: p.ID, Kind: p.Kind, State: state,
		Quantity: max(quantity, 1), Date: time.Now()}
	t.OriginalID = t.ID
	if p.Period != "" {
		t.Expires = addPeriod(t.Date, p.Period)
	}
	s.Transactions = append(s.Transactions, t)
	return t
}

// owned reports whether the user owns a non-consumable of p's, or an
// active subscription of its group.
func (s *Store) owned(p *StoreProduct) bool {
	for _, t := range s.Transactions {
		if t.ProductID == p.ID && t.State == "purchased" && !t.Expired && (p.Kind == spec.NonConsumable || p.Kind == spec.AutoRenewable) {
			return true
		}
	}
	return false
}

// entitled are the transactions the user owns now: StoreKit's current
// entitlements (the latest of each subscription).
func (s *Store) entitled() []*spec.Transaction {
	out := []*spec.Transaction{}
	latest := map[string]int{}
	for _, t := range s.Transactions {
		if t.State != "purchased" || t.Expired || t.Kind == spec.Consumable || t.Kind == spec.NonRenewing && !t.Finished {
			continue
		}
		if i, ok := latest[t.ProductID]; ok && t.Kind == spec.AutoRenewable {
			out[i] = t.wire()
			continue
		}
		latest[t.ProductID] = len(out)
		out = append(out, t.wire())
	}
	return out
}

func (s *Store) find(id string) *StoreTransaction {
	for _, t := range s.Transactions {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func jsonReply(reply func([]byte, error), v any) {
	b, err := json.Marshal(v)
	reply(b, err)
}

func (s *Store) call(method string, args []byte, reply func([]byte, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Unavailable && method != "stop" {
		reply(nil, errors.New("not available: no store"))
		return
	}
	switch method {
	case "available":
		reply([]byte{boolByte(!s.NotAllowed)}, nil)
	case "products":
		var in struct {
			Items []spec.Item `json:"items"`
		}
		json.Unmarshal(args, &in)
		out := spec.Products{Products: []spec.Product{}}
		for _, it := range in.Items {
			if p := s.product(it.ID); p != nil {
				out.Products = append(out.Products, p.wire(s))
			} else {
				out.Missing = append(out.Missing, it.ID)
			}
		}
		jsonReply(reply, out)
	case "buy":
		var in spec.Buy
		json.Unmarshal(args, &in)
		p := s.product(in.ID)
		switch {
		case p == nil:
			reply(nil, errors.New("unknown product: "+in.ID))
			return
		case s.NotAllowed:
			reply(nil, errors.New("not allowed: payments are turned off"))
			return
		case s.owned(p):
			reply(nil, errors.New("already owned: "+in.ID))
			return
		}
		switch s.Outcome {
		case PurchaseCancelled:
			jsonReply(reply, spec.Result{Status: "cancelled"})
		case PurchaseFailed:
			reply(nil, errors.New("the store failed"))
		case PurchasePending:
			t := s.newTransaction(p, in.Quantity, "pending")
			t.AccountToken = in.AccountToken
			jsonReply(reply, spec.Result{Status: "pending"})
		default:
			t := s.newTransaction(p, in.Quantity, "purchased")
			t.AccountToken = in.AccountToken
			jsonReply(reply, spec.Result{Status: "purchased", Transaction: t.wire()})
		}
	case "finish":
		var in spec.Finish
		json.Unmarshal(args, &in)
		t := s.find(in.ID)
		if t == nil {
			reply(nil, errors.New("no transaction "+in.ID))
			return
		}
		if t.State == "pending" {
			reply(nil, errors.New("pending"))
			return
		}
		t.Finished = true
		t.Consumed = t.Consumed || in.Consume && t.Kind == spec.Consumable
		reply(nil, nil)
	case "entitlements":
		jsonReply(reply, s.entitled())
	case "restore":
		s.Restores++
		jsonReply(reply, s.entitled())
	case "status":
		var in struct {
			Group string `json:"group"`
		}
		json.Unmarshal(args, &in)
		out := []spec.Status{}
		for _, t := range s.Transactions {
			p := s.product(t.ProductID)
			if p == nil || p.Kind != spec.AutoRenewable || (p.Group != in.Group && p.ID != in.Group) || t.State == "pending" {
				continue
			}
			st := spec.Status{ProductID: t.ProductID, State: "subscribed", WillRenew: !t.Expired, Transaction: t.wire()}
			if !t.Expires.IsZero() {
				st.RenewalMs = t.Expires.UnixMilli()
			}
			switch {
			case t.State == "revoked":
				st.State, st.WillRenew = "revoked", false
			case t.Expired:
				st.State, st.ExpirationReason = "expired", "autoRenewDisabled"
			}
			if len(out) > 0 && out[len(out)-1].ProductID == t.ProductID {
				out[len(out)-1] = st
			} else {
				out = append(out, st)
			}
		}
		jsonReply(reply, out)
	case "manage":
		s.Manages++
		reply(nil, nil)
	case "refund":
		var in struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &in)
		s.Refunds = append(s.Refunds, in.ID)
		reply([]byte(`{"requested":true}`), nil)
	case "redeem":
		s.Redeems++
		reply(nil, nil)
	case "storefront":
		jsonReply(reply, spec.Storefront{Country: s.Country, ID: "143441"})
	case "next":
		s.following = true
		if len(s.queue) > 0 {
			e := s.queue[0]
			s.queue = s.queue[1:]
			jsonReply(reply, e)
			return
		}
		s.waiting = reply
	case "stop":
		if w := s.waiting; w != nil {
			s.waiting = nil
			w(nil, errors.New("stopped"))
		}
		s.following = false
		reply(nil, nil)
	default:
		reply(nil, errors.New("no module method purchase."+method))
	}
}

// send gives the app an event: to the waiting "next", or kept until one
// comes (the host keeps what comes before the app observes). s.mu held.
func (s *Store) send(e spec.Event) {
	if w := s.waiting; w != nil {
		s.waiting = nil
		jsonReply(w, e)
		return
	}
	s.queue = append(s.queue, e)
}

func (s *Store) sendTransaction(t *StoreTransaction) {
	s.send(spec.Event{Type: "transaction", Transaction: t.wire()})
}

// pending is productID's pending transaction.
func (s *Store) pending(productID string) *StoreTransaction {
	for _, t := range s.Transactions {
		if t.ProductID == productID && t.State == "pending" {
			return t
		}
	}
	return nil
}

// Approve completes productID's pending purchase (a parent approved it,
// the payment came), which the app's Observe gets. It answers the
// transaction, nil if there's none pending.
func (s *Store) Approve(productID string) *StoreTransaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.pending(productID)
	if t == nil {
		return nil
	}
	t.State, t.Outside, t.Date = "purchased", true, time.Now()
	s.sendTransaction(t)
	return t
}

// Decline drops productID's pending purchase (the parent declined): the
// app hears nothing, as on iOS.
func (s *Store) Decline(productID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.pending(productID); t != nil {
		t.State = "declined"
	}
}

// Deliver is a purchase of productID outside the app (on another device,
// an offer code redeemed in the store), which the app's Observe gets.
func (s *Store) Deliver(productID string) *StoreTransaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.product(productID)
	if p == nil {
		panic("fake.Store.Deliver: no product " + productID)
	}
	t := s.newTransaction(p, 1, "purchased")
	t.Outside = true
	s.sendTransaction(t)
	return t
}

// Renew renews productID's subscription: a new transaction of the same
// original, for the next period, which the app's Observe gets.
func (s *Store) Renew(productID string) *StoreTransaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last *StoreTransaction
	for _, t := range s.Transactions {
		if t.ProductID == productID && t.State == "purchased" {
			last = t
		}
	}
	p := s.product(productID)
	if last == nil || p == nil {
		return nil
	}
	t := s.newTransaction(p, 1, "purchased")
	t.OriginalID, t.Outside, t.AccountToken = last.OriginalID, true, last.AccountToken
	if !last.Expires.IsZero() {
		t.Date, t.Expires = last.Expires, addPeriod(last.Expires, p.Period)
	}
	last.Expired = true
	s.sendTransaction(t)
	return t
}

// Expire ends productID's subscription (the user turned renewal off, its
// period ended): no transaction, as with the stores, but the
// entitlements no longer have it.
func (s *Store) Expire(productID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.Transactions {
		if t.ProductID == productID && t.State == "purchased" {
			t.Expired = true
		}
	}
}

// Refund refunds the transaction id (Apple granted a refund; Family
// Sharing stopped): revoked, which the app's Observe gets.
func (s *Store) Refund(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.find(id); t != nil && t.State == "purchased" {
		t.State = "revoked"
		s.sendTransaction(t)
	}
}

// SetCountry changes the storefront, which the app's Observe gets.
func (s *Store) SetCountry(country string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Country = country
	s.send(spec.Event{Type: "storefront", Storefront: &spec.Storefront{Country: country}})
}

// Unfinished are the transactions the app hasn't finished (not pending).
func (s *Store) Unfinished() []*StoreTransaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*StoreTransaction
	for _, t := range s.Transactions {
		if !t.Finished && (t.State == "purchased" || t.State == "revoked") {
			out = append(out, t)
		}
	}
	return out
}

// Relaunch is the app starting again: the store gives its unfinished
// transactions again (StoreKit's Transaction.unfinished; Play's
// unacknowledged purchases), to whoever observes next.
func (s *Store) Relaunch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = nil
	for _, t := range s.Transactions {
		if !t.Finished && t.State == "purchased" {
			s.queue = append(s.queue, spec.Event{Type: "transaction", Transaction: t.wire()})
		}
	}
}

// Following reports whether the app observes the store (a "next" waits).
func (s *Store) Following() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting != nil
}

// String lists the transactions, for a test's failure.
func (s *Store) String() string {
	var b strings.Builder
	for _, t := range s.Transactions {
		fmt.Fprintf(&b, "%s %s %s finished=%v consumed=%v\n", t.ID, t.ProductID, t.State, t.Finished, t.Consumed)
	}
	return b.String()
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

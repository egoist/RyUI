//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	spec "github.com/egoist/asuka/internal/purchasespec"
	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/purchase on Windows: the Microsoft Store (Windows.Services.Store),
// for an app installed from it; an app that isn't (no package identity:
// asuka dev, a build installed by hand) has no store. Products are the
// app's add-ons, by the product ID Partner Center gave each (its
// InAppOfferToken) or their Store ID. A consumable's purchase is a
// transaction until the app finishes it (the Store's fulfillment: the
// user can't buy it again until then, and those left unfulfilled come
// again at the next start); a durable's or a subscription's is its
// license. The Store says when the user's licenses change (bought on
// another PC, renewed, refunded): new ones come through "next", a
// durable's gone as revoked. The Store's calls wait, so each runs on a
// goroutine in COM's multithreaded apartment, answering on the main
// thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["purchase"] = h.purchaseModule() })
}

type winStore struct {
	h       *Host
	mu      sync.Mutex
	ctx     *StoreContext
	err     error
	once    sync.Once
	addOns  map[string]StoreAddOn        // by Store ID
	active  map[string]StoreAddOnLicense // the licenses last seen active, by the add-on's Store ID
	bought  map[string]bool              // Store IDs bought here, whose license isn't news
	events  [][]byte                     // kept for "next"
	waiting func([]byte, error)
	stops   int // "stop"s: one that comes while a "next" starts stops it
}

// storeIDOf is an add-on's Store ID from a SKU's (9NBLGGH4R315/0010).
func storeIDOf(sku string) string {
	id, _, _ := strings.Cut(sku, "/")
	return id
}

func (h *Host) purchaseModule() map[string]asyncMethod {
	s := &winStore{h: h, addOns: map[string]StoreAddOn{}, active: map[string]StoreAddOnLicense{}, bought: map[string]bool{}}
	// Listening from the start: what the user bought while the app
	// didn't observe comes to the first "next".
	if Packaged() {
		go WithCOM(func() { s.context() })
	}
	run := func(reply func([]byte, error), f func(ctx *StoreContext) (any, error)) {
		go WithCOM(func() {
			ctx, err := s.context()
			var out any
			if err == nil {
				out, err = f(ctx)
			}
			var b []byte
			if err == nil && out != nil {
				b, err = json.Marshal(out)
			}
			h.Post(func() { reply(b, err) })
		})
	}
	return map[string]asyncMethod{
		"available": func(_ []byte, reply func([]byte, error)) {
			if !Packaged() {
				reply([]byte{0}, nil)
				return
			}
			run(func(b []byte, err error) { reply([]byte{b2u(err == nil)}, nil) }, func(*StoreContext) (any, error) { return nil, nil })
		},
		"products": func(args []byte, reply func([]byte, error)) {
			var in struct {
				Items []spec.Item `json:"items"`
			}
			json.Unmarshal(args, &in)
			run(reply, func(ctx *StoreContext) (any, error) {
				if err := s.refresh(ctx); err != nil {
					return nil, err
				}
				out := spec.Products{Products: []spec.Product{}}
				for _, it := range in.Items {
					if a, ok := s.find(it.ID); ok {
						out.Products = append(out.Products, s.product(a, it))
					} else {
						out.Missing = append(out.Missing, it.ID)
					}
				}
				return out, nil
			})
		},
		"buy": func(args []byte, reply func([]byte, error)) {
			var in spec.Buy
			json.Unmarshal(args, &in)
			run(reply, func(ctx *StoreContext) (any, error) {
				a, ok := s.find(in.ID)
				if !ok {
					if err := s.refresh(ctx); err != nil {
						return nil, err
					}
					if a, ok = s.find(in.ID); !ok {
						return nil, errors.New("unknown product: " + in.ID)
					}
				}
				status, err := ctx.Purchase(a.StoreID)
				switch status {
				case StorePurchased:
					s.mu.Lock()
					s.bought[a.StoreID] = true
					s.mu.Unlock()
					t := s.transaction(a, in.ID, in.Kind)
					t.DateMs = time.Now().UnixMilli()
					return spec.Result{Status: "purchased", Transaction: t}, nil
				case StoreAlreadyPurchased:
					return nil, errors.New("already owned: " + in.ID)
				case StoreNotPurchased:
					return spec.Result{Status: "cancelled"}, nil
				case StoreNetworkError:
					return nil, errors.New("the network: the Microsoft Store can't be reached")
				}
				if err == nil {
					err = errors.New("the Microsoft Store failed")
				}
				return nil, err
			})
		},
		"finish": func(args []byte, reply func([]byte, error)) {
			var in spec.Finish
			json.Unmarshal(args, &in)
			if !in.Consume {
				reply(nil, nil) // a durable's license is its own
				return
			}
			run(reply, func(ctx *StoreContext) (any, error) {
				tracking := in.ID
				if !strings.HasPrefix(tracking, "{") {
					tracking = NewTrackingID()
				}
				status, err := ctx.Fulfill(storeIDOf(in.Token), uint32(max(in.Quantity, 1)), tracking)
				if status != 0 && err == nil {
					err = errors.New("the Microsoft Store didn't take the fulfillment")
				}
				return nil, err
			})
		},
		"entitlements": func(_ []byte, reply func([]byte, error)) {
			run(reply, func(ctx *StoreContext) (any, error) { return s.entitlements(ctx) })
		},
		"restore": func(_ []byte, reply func([]byte, error)) {
			run(reply, func(ctx *StoreContext) (any, error) {
				s.licensesChanged(ctx)
				return s.entitlements(ctx)
			})
		},
		"status": func(args []byte, reply func([]byte, error)) {
			var in struct {
				Group string `json:"group"`
			}
			json.Unmarshal(args, &in)
			run(reply, func(ctx *StoreContext) (any, error) {
				ls, err := ctx.Licenses()
				if err != nil {
					return nil, err
				}
				out := []spec.Status{}
				for _, l := range ls {
					a, ok := s.addOn(storeIDOf(l.SkuStoreID))
					if !ok || !a.Subscription || (l.OfferToken != in.Group && a.StoreID != in.Group) {
						continue
					}
					st := spec.Status{ProductID: productIDOf(a), State: "subscribed", WillRenew: l.Active, Transaction: s.transaction(a, "", "")}
					if !l.Active {
						st.State = "expired"
					}
					if !l.Expires.IsZero() {
						st.RenewalMs = l.Expires.UnixMilli()
						st.Transaction.ExpiresMs = st.RenewalMs
					}
					out = append(out, st)
				}
				return out, nil
			})
		},
		"manage": func(_ []byte, reply func([]byte, error)) {
			OpenURL("https://account.microsoft.com/services")
			reply(nil, nil)
		},
		"refund": failing("not available: the Microsoft Store's refunds are asked for on its website"),
		"redeem": func(_ []byte, reply func([]byte, error)) {
			OpenURL("https://www.microsoft.com/redeem")
			reply(nil, nil)
		},
		"storefront": func(_ []byte, reply func([]byte, error)) {
			if !Packaged() {
				reply(nil, ErrNotPackaged)
				return
			}
			b, err := json.Marshal(spec.Storefront{Country: UserCountry()})
			reply(b, err)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			s.mu.Lock()
			at := s.stops
			s.mu.Unlock()
			go WithCOM(func() {
				if _, err := s.context(); err != nil {
					h.Post(func() { reply(nil, err) })
					return
				}
				s.mu.Lock()
				defer s.mu.Unlock()
				if at != s.stops {
					h.Post(func() { reply(nil, errors.New("stopped")) })
					return
				}
				if len(s.events) > 0 {
					e := s.events[0]
					s.events = s.events[1:]
					h.Post(func() { reply(e, nil) })
					return
				}
				if w := s.waiting; w != nil {
					h.Post(func() { w(nil, errors.New("stopped")) })
				}
				s.waiting = reply
			})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			s.mu.Lock()
			w := s.waiting
			s.waiting = nil
			s.stops++
			s.mu.Unlock()
			if w != nil {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// context is the Store's context, made once (over the main window, its
// dialogs'), with its add-ons and licenses read and their changes
// followed. On a goroutine in COM's multithreaded apartment.
func (s *winStore) context() (*StoreContext, error) {
	s.once.Do(func() {
		hwnd := make(chan uintptr, 1)
		s.h.Post(func() { hwnd <- s.h.window })
		s.ctx, s.err = NewStoreContext(<-hwnd)
		if s.err != nil {
			return
		}
		s.refresh(s.ctx)
		// The licenses as the app starts: a consumable's not yet
		// fulfilled is a transaction left unfinished.
		ls, _ := s.ctx.Licenses()
		s.mu.Lock()
		for _, l := range ls {
			if !l.Active {
				continue
			}
			id := storeIDOf(l.SkuStoreID)
			s.active[id] = l
			if a, ok := s.addOns[id]; ok && strings.HasSuffix(a.Kind, "Consumable") {
				s.give(s.transaction(a, "", ""))
			}
		}
		s.mu.Unlock()
		ctx := s.ctx
		ctx.OnLicensesChanged(func() { go WithCOM(func() { s.licensesChanged(ctx) }) })
	})
	return s.ctx, s.err
}

// refresh reads the add-ons again.
func (s *winStore) refresh(ctx *StoreContext) error {
	as, err := ctx.AddOns()
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, a := range as {
		s.addOns[a.StoreID] = a
	}
	s.mu.Unlock()
	return nil
}

func (s *winStore) addOn(storeID string) (StoreAddOn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.addOns[storeID]
	return a, ok
}

// find is the add-on of a product ID (its offer token) or Store ID.
func (s *winStore) find(id string) (StoreAddOn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.addOns[id]; ok {
		return a, true
	}
	for _, a := range s.addOns {
		if a.OfferToken == id {
			return a, true
		}
	}
	return StoreAddOn{}, false
}

func productIDOf(a StoreAddOn) string {
	if a.OfferToken != "" {
		return a.OfferToken
	}
	return a.StoreID
}

func kindOf(a StoreAddOn) string {
	switch {
	case strings.HasSuffix(a.Kind, "Consumable"):
		return spec.Consumable
	case a.Subscription:
		return spec.AutoRenewable
	}
	return spec.NonConsumable
}

func storePeriod(unit string, n int) *spec.Period {
	if n == 0 {
		return nil
	}
	return &spec.Period{Unit: strings.ToLower(unit), Count: n}
}

func (s *winStore) product(a StoreAddOn, it spec.Item) spec.Product {
	p := spec.Product{ID: it.ID, Kind: kindOf(a), Title: a.Title, Description: a.Description, Price: a.Price,
		Micros: a.Micros, Currency: a.Currency}
	if a.Subscription {
		p.Group, p.Period = productIDOf(a), storePeriod(a.PeriodUnit, a.PeriodCount)
		if t := storePeriod(a.TrialUnit, a.TrialCount); t != nil {
			p.IntroEligible = !a.Owned
			p.Offers = []spec.Offer{{Type: "introductory", Phases: []spec.Phase{{Price: "Free", Currency: a.Currency, Period: *t, Cycles: 1, Mode: "freeTrial"}}}}
		}
	}
	return p
}

// transaction is a purchase of a: a consumable's under a new tracking ID
// (what its fulfillment reports), the others' under the add-on's Store ID.
func (s *winStore) transaction(a StoreAddOn, id, kind string) *spec.Transaction {
	if id == "" {
		id = productIDOf(a)
	}
	if kind == "" {
		kind = kindOf(a)
	}
	t := &spec.Transaction{ID: a.StoreID, ProductID: id, Kind: kind, Quantity: 1, State: "purchased", Token: a.StoreID,
		AutoRenews: a.Subscription, Storefront: UserCountry()}
	if kindOf(a) == spec.Consumable {
		t.ID = NewTrackingID()
	}
	return t
}

func (s *winStore) entitlements(ctx *StoreContext) ([]*spec.Transaction, error) {
	ls, err := ctx.Licenses()
	if err != nil {
		return nil, err
	}
	out := []*spec.Transaction{}
	for _, l := range ls {
		a, ok := s.addOn(storeIDOf(l.SkuStoreID))
		if !l.Active || !ok || kindOf(a) == spec.Consumable {
			continue
		}
		t := s.transaction(a, "", "")
		if !l.Expires.IsZero() {
			t.ExpiresMs = l.Expires.UnixMilli()
		}
		out = append(out, t)
	}
	return out, nil
}

// licensesChanged compares the licenses with those seen: one new is a
// purchase outside a Buy (another PC, a renewal's new period), a
// durable's gone is revoked (refunded).
func (s *winStore) licensesChanged(ctx *StoreContext) {
	ls, err := ctx.Licenses()
	if err != nil {
		return
	}
	s.refresh(ctx)
	now := map[string]StoreAddOnLicense{}
	for _, l := range ls {
		if l.Active {
			now[storeIDOf(l.SkuStoreID)] = l
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, l := range now {
		old, seen := s.active[id]
		a, ok := s.addOns[id]
		if !ok {
			continue
		}
		if s.bought[id] {
			delete(s.bought, id)
			continue
		}
		if !seen || !l.Expires.Equal(old.Expires) {
			t := s.transaction(a, "", "")
			t.DateMs = time.Now().UnixMilli()
			if !l.Expires.IsZero() {
				t.ExpiresMs = l.Expires.UnixMilli()
			}
			s.give(t)
		}
	}
	for id := range s.active {
		if _, still := now[id]; still {
			continue
		}
		if a, ok := s.addOns[id]; ok && kindOf(a) == spec.NonConsumable {
			t := s.transaction(a, "", "")
			t.State, t.RevokedMs = "revoked", time.Now().UnixMilli()
			s.give(t)
		}
	}
	s.active = now
}

// give keeps a transaction for "next", or answers the one waiting. s.mu
// held.
func (s *winStore) give(t *spec.Transaction) {
	b, err := json.Marshal(spec.Event{Type: "transaction", Transaction: t})
	if err != nil {
		return
	}
	if w := s.waiting; w != nil {
		s.waiting = nil
		s.h.Post(func() { w(b, nil) })
		return
	}
	s.events = append(s.events, b)
}

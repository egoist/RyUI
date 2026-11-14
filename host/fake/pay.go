package fake

import (
	"encoding/json"
	"errors"
	"strings"

	spec "github.com/egoist/asuka/internal/payspec"
	"github.com/egoist/asuka/ops"
)

// Wallet is the platform's wallet, as the hosts' "pay" module has it
// (package pay), for tests: Host.Wallet installs it. The test plays the
// user: it changes the sheet (ChangeShippingContact, ChooseShippingMethod,
// ChoosePaymentMethod, EnterCoupon) and pays (Authorize) or closes it
// (Cancel), and reads what the app's Go code answered in Sheet. Main
// thread, as everything of the host's; the app's answers come as its
// goroutine runs (uitest's Until).
type Wallet struct {
	// Kind is "apple" or "google": the wallet emulated, its events and
	// answers shaped as its host's are. "": Google Pay on Android and the
	// web, Apple Pay elsewhere.
	Kind string
	// Unavailable: no wallet here (Windows, Linux): Available says none,
	// Pay fails. NoCard: it has none of the request's cards.
	Unavailable, NoCard bool
	// Sheet is the sheet that shows, nil when none; Last is the last one
	// (after it closed too).
	Sheet, Last *PaySheet
	// Presented counts the sheets shown, SetUps the calls to add a card.
	Presented, SetUps int

	h       *Host
	seq     int
	queue   [][]byte // events for "next"
	waiting func([]byte, error)
	pending map[int]string // events waiting for their answers: their types
}

// PaySheet is a sheet as the app's Go code made it, and as its answers
// changed it.
type PaySheet struct {
	// Wallet is "apple" or "google".
	Wallet string
	// Merchant is Apple Pay's merchant ID, or Google Pay's merchant name.
	Merchant          string
	Country, Currency string
	// Items are the lines above the total, as Google Pay's sheet shows
	// them; Apple Pay's include a subscription's or later payment's own.
	Items           []PayItem
	Total           PayItem
	ShippingMethods []PayShippingMethod
	// Selected is the chosen shipping method's ID.
	Selected string
	// Errors are what the sheet shows, from the last answer.
	Errors []PayError
	// Answers counts the app's answers; Authorized: it accepted the
	// payment (the sheet closed).
	Answers    int
	Authorized bool
	// Request is the request as the host got it (JSON).
	Request []byte

	id string
}

// PayItem is a line, or the total.
type PayItem struct {
	Label, Amount string
	Pending       bool
}

// PayShippingMethod is a shipping method in the sheet.
type PayShippingMethod struct {
	ID, Label, Detail, Amount string
}

// PayError is an error the sheet shows: Field is package pay's Field's
// name ("shippingPostalCode", "payment"...; Google Pay's: its reason's,
// "shippingAddress", "unserviceable", "shippingMethod" or "payment").
type PayError struct {
	Field, Message string
}

// PayContact is a contact the user chose.
type PayContact struct {
	GivenName, FamilyName, Email, Phone string
	Lines                               []string
	City, State, PostalCode             string
	CountryCode                         string
}

// PayCard is the card the user chose. Network is package pay's name
// ("visa"); Kind is "credit", "debit" or "prepaid"; Name is how the
// wallet shows it ("Visa 1234").
type PayCard struct {
	Network, Kind, Name string
	Billing             *PayContact
}

// PayAuthorization is what the user pays with.
type PayAuthorization struct {
	// Token is the payment token the app's Go code gets.
	Token             string
	Card              PayCard
	Shipping, Billing *PayContact
}

// Wallet gives the host a wallet (the "pay" module).
func (h *Host) Wallet() *Wallet {
	w := &Wallet{h: h, pending: map[int]string{}}
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["pay"] = w.call
	return w
}

func (w *Wallet) kind() string {
	if w.Kind != "" {
		return w.Kind
	}
	if p := w.h.Env.Platform; p == ops.PlatformAndroid || p == ops.PlatformWeb {
		return "google"
	}
	return "apple"
}

func (w *Wallet) call(method string, args []byte, reply func([]byte, error)) {
	switch method {
	case "available":
		a := spec.Availability{}
		if !w.Unavailable {
			a = spec.Availability{Wallet: w.kind(), CanPay: true, HasCard: !w.NoCard}
		}
		b, _ := json.Marshal(a)
		reply(b, nil)
	case "setUp":
		if w.Unavailable || w.kind() != "apple" {
			reply(nil, errors.New("not available"))
			return
		}
		w.SetUps++
		reply(nil, nil)
	case "present":
		if w.Unavailable {
			reply(nil, errors.New("not available"))
			return
		}
		if w.Sheet != nil {
			reply(nil, errors.New("busy"))
			return
		}
		var p spec.Present
		if err := json.Unmarshal(args, &p); err != nil {
			reply(nil, err)
			return
		}
		s, err := w.sheet(p, args)
		if err != nil {
			reply(nil, err)
			return
		}
		w.Sheet, w.Last = s, s
		w.Presented++
		w.queue, w.pending = nil, map[int]string{}
		reply(nil, nil)
	case "next":
		if w.Sheet == nil && len(w.queue) == 0 {
			reply(nil, errors.New("stopped"))
			return
		}
		if len(w.queue) > 0 {
			e := w.queue[0]
			w.queue = w.queue[1:]
			reply(e, nil)
			return
		}
		w.waiting = reply
	case "answer":
		var a spec.Answer
		if err := json.Unmarshal(args, &a); err != nil {
			reply(nil, err)
			return
		}
		w.answer(a)
		reply(nil, nil)
	default:
		reply(nil, errors.New("no module method pay."+method))
	}
}

// sheet is the sheet for p, as its wallet shows it.
func (w *Wallet) sheet(p spec.Present, raw []byte) (*PaySheet, error) {
	s := &PaySheet{Wallet: w.kind(), id: p.ID, Request: raw}
	if s.Wallet == "apple" {
		if p.AppleError != "" {
			return nil, errors.New(p.AppleError)
		}
		a := p.Apple
		s.Merchant, s.Country, s.Currency = a.Merchant, a.Country, a.Currency
		s.setAppleItems(a.Items)
		s.setAppleMethods(a.ShippingMethods)
		if len(s.ShippingMethods) > 0 {
			s.Selected = s.ShippingMethods[0].ID
		}
		return s, nil
	}
	if p.GoogleError != "" {
		return nil, errors.New(p.GoogleError)
	}
	var g spec.GoogleRequest
	if err := json.Unmarshal(p.Google, &g); err != nil {
		return nil, err
	}
	if g.MerchantInfo != nil {
		s.Merchant = g.MerchantInfo.MerchantName
	}
	switch {
	case g.TransactionInfo != nil:
		s.setGoogleTransaction(g.TransactionInfo)
	case g.RecurringTransactionInfo != nil:
		t := g.RecurringTransactionInfo
		s.Country, s.Currency, s.Total = t.CountryCode, t.CurrencyCode, PayItem{Amount: t.ImmediateTotalPrice}
		s.Items = googleItems(t.ImmediateDisplayItems)
	case g.DeferredTransactionInfo != nil:
		t := g.DeferredTransactionInfo
		s.Country, s.Currency, s.Total = t.CountryCode, t.CurrencyCode, PayItem{Amount: t.ImmediateTotalPrice}
		s.Items = googleItems(t.ImmediateDisplayItems)
	case g.AutomaticReloadTransactionInfo != nil:
		t := g.AutomaticReloadTransactionInfo
		s.Country, s.Currency, s.Total = t.CountryCode, t.CurrencyCode, PayItem{Amount: t.ImmediateTotalPrice}
		s.Items = googleItems(t.ImmediateDisplayItems)
	}
	if o := g.ShippingOptionParameters; o != nil {
		s.setGoogleOptions(o)
	}
	return s, nil
}

func (s *PaySheet) setAppleItems(items []spec.AppleItem) {
	if len(items) == 0 {
		return
	}
	s.Items = s.Items[:0]
	for _, it := range items[:len(items)-1] {
		s.Items = append(s.Items, PayItem{Label: it.Label, Amount: it.Amount, Pending: it.Pending})
	}
	t := items[len(items)-1]
	s.Total = PayItem{Label: t.Label, Amount: t.Amount, Pending: t.Pending}
}

func (s *PaySheet) setAppleMethods(ms []spec.AppleShippingMethod) {
	s.ShippingMethods = nil
	for _, m := range ms {
		s.ShippingMethods = append(s.ShippingMethods, PayShippingMethod{ID: m.ID, Label: m.Label, Detail: m.Detail, Amount: m.Amount})
	}
}

func googleItems(items []spec.GoogleItem) []PayItem {
	var out []PayItem
	for _, it := range items {
		out = append(out, PayItem{Label: it.Label, Amount: it.Price, Pending: it.Status == "PENDING"})
	}
	return out
}

func (s *PaySheet) setGoogleTransaction(t *spec.GoogleTransaction) {
	s.Country, s.Currency = t.CountryCode, t.CurrencyCode
	s.Items = googleItems(t.DisplayItems)
	s.Total = PayItem{Label: t.TotalPriceLabel, Amount: t.TotalPrice, Pending: t.TotalPriceStatus == "ESTIMATED"}
}

func (s *PaySheet) setGoogleOptions(o *spec.GoogleShippingOptions) {
	s.ShippingMethods = nil
	for _, m := range o.ShippingOptions {
		s.ShippingMethods = append(s.ShippingMethods, PayShippingMethod{ID: m.ID, Label: m.Label, Detail: m.Description})
	}
	s.Selected = o.DefaultSelectedOptionID
	if s.Selected == "" && len(s.ShippingMethods) > 0 {
		s.Selected = s.ShippingMethods[0].ID
	}
}

// send gives "next" an event, numbered (but for done).
func (w *Wallet) send(e spec.Event) {
	if e.Type != spec.Done {
		w.seq++
		e.Seq = w.seq
		w.pending[e.Seq] = e.Type
	}
	b, _ := json.Marshal(e)
	if r := w.waiting; r != nil {
		w.waiting = nil
		r(b, nil)
		return
	}
	w.queue = append(w.queue, b)
}

// Waiting reports whether an event waits for the app's answer.
func (w *Wallet) Waiting() bool { return len(w.pending) > 0 }

func (w *Wallet) showing(what string) *PaySheet {
	if w.Sheet == nil {
		panic("fake: Wallet." + what + ": no sheet shows")
	}
	return w.Sheet
}

func (c *PayContact) apple(redacted bool) *spec.Contact {
	if c == nil {
		return nil
	}
	out := &spec.Contact{GivenName: c.GivenName, FamilyName: c.FamilyName, Email: c.Email, Phone: c.Phone,
		Address: &spec.Address{Street: strings.Join(c.Lines, "\n"), City: c.City, State: c.State, PostalCode: c.PostalCode, CountryCode: c.CountryCode}}
	if redacted { // as Apple Pay gives it before the payment
		out = &spec.Contact{Address: &spec.Address{City: c.City, State: c.State, PostalCode: c.PostalCode, CountryCode: c.CountryCode}}
	}
	return out
}

func (c *PayContact) google(redacted bool) *spec.GoogleAddress {
	if c == nil {
		return nil
	}
	a := &spec.GoogleAddress{Locality: c.City, AdministrativeArea: c.State, PostalCode: c.PostalCode, CountryCode: c.CountryCode}
	if !redacted {
		a.Name = strings.TrimSpace(c.GivenName + " " + c.FamilyName)
		a.PhoneNumber = c.Phone
		for i, l := range c.Lines {
			switch i {
			case 0:
				a.Address1 = l
			case 1:
				a.Address2 = l
			case 2:
				a.Address3 = l
			}
		}
	}
	return a
}

func (c PayCard) googleData() spec.GoogleMethodData {
	net := strings.ToUpper(c.Network)
	last4 := c.Name
	if len(last4) > 4 {
		last4 = last4[len(last4)-4:]
	}
	return spec.GoogleMethodData{Type: "CARD", Description: c.Name, Info: spec.GoogleCardInfo{CardNetwork: net,
		CardDetails: last4, CardFundingSource: strings.ToUpper(c.Kind), BillingAddress: c.Billing.google(false)}}
}

func (w *Wallet) googleChanged(trigger string, s *PaySheet, d spec.GoogleIntermediate) {
	d.CallbackTrigger = trigger
	if s.Selected != "" {
		d.ShippingOptionData = &spec.GoogleOptionData{ID: s.Selected}
	}
	b, _ := json.Marshal(d)
	w.send(spec.Event{Type: spec.Google, Google: b})
}

// ChangeShippingContact chooses a shipping contact, as the user does:
// the app's Go code gets its city, state, postal code and country.
func (w *Wallet) ChangeShippingContact(c PayContact) {
	s := w.showing("ChangeShippingContact")
	if s.Wallet == "apple" {
		w.send(spec.Event{Type: spec.ShippingContact, Contact: c.apple(true)})
		return
	}
	w.googleChanged("SHIPPING_ADDRESS", s, spec.GoogleIntermediate{ShippingAddress: c.google(true)})
}

// ChooseShippingMethod chooses the shipping method of id.
func (w *Wallet) ChooseShippingMethod(id string) {
	s := w.showing("ChooseShippingMethod")
	s.Selected = id
	if s.Wallet == "apple" {
		w.send(spec.Event{Type: spec.ShippingMethod, Method: id})
		return
	}
	w.googleChanged("SHIPPING_OPTION", s, spec.GoogleIntermediate{})
}

// ChoosePaymentMethod chooses the card to pay with.
func (w *Wallet) ChoosePaymentMethod(c PayCard) {
	s := w.showing("ChoosePaymentMethod")
	if s.Wallet == "apple" {
		w.send(spec.Event{Type: spec.PaymentMethod, Card: &spec.Card{Network: c.Network, Type: c.Kind, Name: c.Name, Billing: c.Billing.apple(false)}})
		return
	}
	m := c.googleData()
	m.TokenizationData = nil
	w.googleChanged("PAYMENT_METHOD", s, spec.GoogleIntermediate{PaymentMethodData: &m})
}

// EnterCoupon enters a coupon code (Apple Pay's; Google Pay has none:
// it panics).
func (w *Wallet) EnterCoupon(code string) {
	s := w.showing("EnterCoupon")
	if s.Wallet != "apple" {
		panic("fake: Wallet.EnterCoupon: Google Pay's sheet has no coupon codes")
	}
	w.send(spec.Event{Type: spec.Coupon, Coupon: code})
}

// Authorize pays, as the user does with Face ID or their PIN: the app's
// OnAuthorize gets the Payment. If it accepts, the sheet closes.
func (w *Wallet) Authorize(p PayAuthorization) {
	s := w.showing("Authorize")
	if s.Wallet == "apple" {
		w.send(spec.Event{Type: spec.Authorize, Payment: &spec.ApplePayment{Token: p.Token, TransactionID: "fake-transaction",
			Card:     spec.Card{Network: p.Card.Network, Type: p.Card.Kind, Name: p.Card.Name},
			Shipping: p.Shipping.apple(false), Billing: p.Billing.apple(false), ShippingMethod: s.Selected}})
		return
	}
	m := p.Card.googleData()
	if p.Billing != nil {
		m.Info.BillingAddress = p.Billing.google(false)
	}
	m.TokenizationData = &spec.GoogleTokenData{Type: "PAYMENT_GATEWAY", Token: p.Token}
	d := spec.GooglePaymentData{APIVersion: 2, PaymentMethodData: m, ShippingAddress: p.Shipping.google(false)}
	if p.Shipping != nil {
		d.Email = p.Shipping.Email
	}
	if s.Selected != "" {
		d.ShippingOptionData = &spec.GoogleOptionData{ID: s.Selected}
	}
	b, _ := json.Marshal(d)
	w.send(spec.Event{Type: spec.GoogleAuthorize, Google: b})
}

// Cancel closes the sheet, as the user does.
func (w *Wallet) Cancel() {
	w.showing("Cancel")
	w.close()
}

func (w *Wallet) close() {
	w.Sheet = nil
	w.pending = map[int]string{}
	w.send(spec.Event{Type: spec.Done})
}

func (w *Wallet) answer(a spec.Answer) {
	s := w.Sheet
	typ, ok := w.pending[a.Seq]
	if s == nil || a.ID != s.id || !ok {
		return // the sheet closed: too late
	}
	delete(w.pending, a.Seq)
	s.Answers++
	s.Errors = nil
	switch typ {
	case spec.Google:
		var u spec.GoogleUpdate
		json.Unmarshal(a.Google, &u)
		if u.NewTransactionInfo != nil {
			s.setGoogleTransaction(u.NewTransactionInfo)
		}
		if u.NewShippingOptionParameters != nil {
			sel := s.Selected
			s.setGoogleOptions(u.NewShippingOptionParameters)
			if u.NewShippingOptionParameters.DefaultSelectedOptionID == "" && sel != "" {
				s.Selected = sel
			}
		}
		if u.Error != nil {
			s.Errors = []PayError{{Field: googleField(u.Error.Reason), Message: u.Error.Message}}
		}
	case spec.GoogleAuthorize:
		var r spec.GoogleResult
		json.Unmarshal(a.Google, &r)
		if r.TransactionState == "SUCCESS" {
			s.Authorized = true
			w.close()
			return
		}
		if r.Error != nil {
			s.Errors = []PayError{{Field: googleField(r.Error.Reason), Message: r.Error.Message}}
		}
	default:
		if a.Items != nil {
			s.setAppleItems(a.Items)
		}
		if a.ShippingMethods != nil {
			s.setAppleMethods(a.ShippingMethods)
		}
		for _, e := range a.Errors {
			s.Errors = append(s.Errors, PayError{Field: e.Field, Message: e.Message})
		}
		if typ == spec.Authorize && len(a.Errors) == 0 {
			s.Authorized = true
			w.close()
		}
	}
}

func googleField(reason string) string {
	switch reason {
	case "SHIPPING_ADDRESS_INVALID":
		return "shippingAddress"
	case "SHIPPING_ADDRESS_UNSERVICEABLE":
		return "unserviceable"
	case "SHIPPING_OPTION_INVALID":
		return "shippingMethod"
	}
	return "payment"
}

//go:build darwin && !ios

package darwin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	spec "github.com/egoist/asuka/internal/payspec"
	"github.com/egoist/asuka/native"
)

// Apple Pay on a Mac (asuka/sdk/pay), with PassKit as on iOS:
// PKPaymentAuthorizationController's sheet (a Mac with Touch ID, or an
// iPhone or Apple Watch nearby, pays) and PKPaymentButton. The wire is
// internal/payspec's, as iOS's host has it: "available", "setUp",
// "present", then the sheet's callbacks as events ("next"), each
// completed with the Go code's answer ("answer"), and "done". The app's
// signature needs the in-app-payments entitlement with its merchant IDs,
// which a provisioning profile grants (asuka.json's applePay).

type paySheet struct {
	delegate   ID
	controller ID
	id         string
	seq        int
	request    spec.AppleRequest
	items      ID // the sheet's summary items now (an NSArray), retained
	methods    ID // and its shipping methods
	queue      [][]byte
	waiting    func([]byte, error)
	pending    map[int]func(spec.Answer)
	buttons    map[ID]*payButton
	classes    bool
}

var pay paySheet

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["pay"] = h.payModule() })
	builtinViews["asuka.payButton"] = newPayButton
}

func loadPassKit() bool {
	return framework("PassKit") != 0 && HasClass("PKPaymentAuthorizationController")
}

func (h *Host) payModule() map[string]method {
	return map[string]method{
		"available": func(args []byte, reply func([]byte, error)) {
			var p spec.Present
			json.Unmarshal(args, &p)
			a := spec.Availability{Wallet: "apple"}
			if loadPassKit() {
				c := Class("PKPaymentAuthorizationController")
				a.CanPay = SendBool(c, "canMakePayments")
				a.HasCard = a.CanPay && SendBool(c, "canMakePaymentsUsingNetworks:capabilities:",
					uintptr(Array(payNetworks(p.Apple.Networks)...)), payCapabilities(p.Apple.Capabilities))
			}
			b, _ := json.Marshal(a)
			reply(b, nil)
		},
		"setUp": func(_ []byte, reply func([]byte, error)) {
			if !loadPassKit() || !HasClass("PKPassLibrary") {
				reply(nil, errors.New("not available"))
				return
			}
			lib := New("PKPassLibrary")
			defer Release(lib)
			if !RespondsTo(lib, "openPaymentSetup") {
				reply(nil, errors.New("not available: this Mac can't add a card to Apple Pay"))
				return
			}
			Send(lib, "openPaymentSetup")
			reply(nil, nil)
		},
		"present": h.payPresent,
		"next": func(args []byte, reply func([]byte, error)) {
			if string(args) != pay.id || (pay.controller == 0 && len(pay.queue) == 0) {
				reply(nil, errors.New("stopped"))
				return
			}
			if len(pay.queue) > 0 {
				b := pay.queue[0]
				pay.queue = pay.queue[1:]
				reply(b, nil)
				return
			}
			if w := pay.waiting; w != nil {
				w(nil, errors.New("stopped"))
			}
			pay.waiting = reply
		},
		"answer": func(args []byte, reply func([]byte, error)) {
			var a spec.Answer
			if json.Unmarshal(args, &a) == nil && a.ID == pay.id {
				if done := pay.pending[a.Seq]; done != nil {
					delete(pay.pending, a.Seq)
					done(a)
				}
			}
			reply(nil, nil)
		},
	}
}

func (h *Host) payPresent(args []byte, reply func([]byte, error)) {
	if pay.controller != 0 {
		reply(nil, errors.New("busy"))
		return
	}
	var p spec.Present
	if err := json.Unmarshal(args, &p); err != nil {
		reply(nil, err)
		return
	}
	if p.AppleError != "" {
		reply(nil, errors.New(p.AppleError))
		return
	}
	if !loadPassKit() || !SendBool(Class("PKPaymentAuthorizationController"), "canMakePayments") {
		reply(nil, errors.New("not available: Apple Pay isn't on this Mac (it needs Touch ID, or an iPhone or Apple Watch nearby), or it's restricted"))
		return
	}
	pay.request, pay.id, pay.seq, pay.queue, pay.pending = p.Apple, p.ID, 0, nil, map[int]func(spec.Answer){}
	r := payRequest(p.Apple)
	pay.keep(Send(r, "paymentSummaryItems"), Send(r, "shippingMethods"))
	c := Send(Send(Class("PKPaymentAuthorizationController"), "alloc"), "initWithPaymentRequest:", uintptr(r))
	Release(r)
	Send(c, "setDelegate:", uintptr(payDelegate()))
	pay.controller = c
	var done objc.Block
	done = NewBlock(func(_ objc.Block, ok bool) {
		h.Post(func() {
			done.Release()
			if ok {
				reply(nil, nil)
				return
			}
			if pay.controller == c {
				Release(c)
				pay.controller = 0
			}
			reply(nil, errors.New("not available: the payment sheet didn't show (check the merchant ID, the networks and the entitlement)"))
		})
	})
	Send(c, "presentWithCompletion:", uintptr(done))
}

// keep makes items and methods (NSArrays, 0: as they are) the sheet's.
func (s *paySheet) keep(items, methods ID) {
	if items != 0 {
		if s.items != 0 {
			Release(s.items)
		}
		s.items = Retain(items)
	}
	if methods != 0 {
		if s.methods != 0 {
			Release(s.methods)
		}
		s.methods = Retain(methods)
	}
}

// keepAnswer keeps an answer's items and methods.
func (s *paySheet) keepAnswer(a spec.Answer) {
	var items, methods ID
	if a.Items != nil {
		items = paySummaryItems(a.Items)
	}
	if a.ShippingMethods != nil {
		methods = payShippingMethods(a.ShippingMethods)
	}
	s.keep(items, methods)
}

// send gives the Go code an event; done (if not nil) gets its answer.
func (s *paySheet) send(e map[string]any, done func(spec.Answer)) {
	if done != nil {
		s.seq++
		e["seq"] = s.seq
		s.pending[s.seq] = done
	}
	b, _ := json.Marshal(e)
	if w := s.waiting; w != nil {
		s.waiting = nil
		w(b, nil)
		return
	}
	s.queue = append(s.queue, b)
}

// payHandler is a delegate's completion block, copied, called once with obj.
func payHandler(handler ID) func(obj ID) {
	done := Retain(Send(handler, "copy"))
	return func(obj ID) {
		CallBlock(uintptr(done), uintptr(obj))
		Release(done)
	}
}

func payDelegate() ID {
	if pay.delegate != 0 {
		return pay.delegate
	}
	cls := DefineClass("AsukaPayDelegate", "NSObject", []string{"PKPaymentAuthorizationControllerDelegate"}, []objc.MethodDef{
		Method("presentationWindowForPaymentAuthorizationController:", func(self ID, _ SEL, c ID) ID {
			return theHost.keyWin().window
		}),
		Method("paymentAuthorizationController:didAuthorizePayment:handler:", func(self ID, _ SEL, c, payment, handler ID) {
			done := payHandler(handler)
			pay.send(map[string]any{"type": "authorize", "payment": payPayment(payment)}, func(a spec.Answer) {
				status := uintptr(0) // success
				if len(a.Errors) > 0 {
					status = 1 // failure
				}
				r := Send(Send(Class("PKPaymentAuthorizationResult"), "alloc"), "initWithStatus:errors:", status, uintptr(payErrors(a.Errors)))
				done(r)
				Release(r)
			})
		}),
		Method("paymentAuthorizationController:didSelectShippingContact:handler:", func(self ID, _ SEL, c, contact, handler ID) {
			done := payHandler(handler)
			update := func(errs []spec.FieldError) {
				u := Send(Send(Class("PKPaymentRequestShippingContactUpdate"), "alloc"), "initWithErrors:paymentSummaryItems:shippingMethods:",
					uintptr(payErrors(errs)), uintptr(pay.items), uintptr(payOr(pay.methods)))
				done(u)
				Release(u)
			}
			if !pay.request.Events.ShippingContact {
				update(nil)
				return
			}
			pay.send(map[string]any{"type": "shippingContact", "contact": payContact(contact)}, func(a spec.Answer) {
				pay.keepAnswer(a)
				update(a.Errors)
			})
		}),
		Method("paymentAuthorizationController:didSelectShippingMethod:handler:", func(self ID, _ SEL, c, m, handler ID) {
			done := payHandler(handler)
			update := func(failed bool) {
				u := Send(Send(Class("PKPaymentRequestShippingMethodUpdate"), "alloc"), "initWithPaymentSummaryItems:", uintptr(pay.items))
				if failed {
					Send(u, "setStatus:", 1)
				}
				done(u)
				Release(u)
			}
			if !pay.request.Events.ShippingMethod {
				update(false)
				return
			}
			pay.send(map[string]any{"type": "shippingMethod", "method": GoString(Send(m, "identifier"))}, func(a spec.Answer) {
				pay.keepAnswer(a)
				update(len(a.Errors) > 0)
			})
		}),
		Method("paymentAuthorizationController:didSelectPaymentMethod:handler:", func(self ID, _ SEL, c, m, handler ID) {
			done := payHandler(handler)
			update := func(errs []spec.FieldError) {
				u := Send(Send(Class("PKPaymentRequestPaymentMethodUpdate"), "alloc"), "initWithErrors:paymentSummaryItems:",
					uintptr(payErrors(errs)), uintptr(pay.items))
				done(u)
				Release(u)
			}
			if !pay.request.Events.PaymentMethod {
				update(nil)
				return
			}
			pay.send(map[string]any{"type": "paymentMethod", "card": payCard(m)}, func(a spec.Answer) {
				pay.keepAnswer(a)
				update(a.Errors)
			})
		}),
		Method("paymentAuthorizationController:didChangeCouponCode:handler:", func(self ID, _ SEL, c, code, handler ID) {
			done := payHandler(handler)
			update := func(errs []spec.FieldError) {
				u := Send(Send(Class("PKPaymentRequestCouponCodeUpdate"), "alloc"), "initWithErrors:paymentSummaryItems:shippingMethods:",
					uintptr(payErrors(errs)), uintptr(pay.items), uintptr(payOr(pay.methods)))
				done(u)
				Release(u)
			}
			if !pay.request.Events.Coupon {
				update(nil)
				return
			}
			pay.send(map[string]any{"type": "coupon", "coupon": GoString(code)}, func(a spec.Answer) {
				pay.keepAnswer(a)
				update(a.Errors)
			})
		}),
		Method("paymentAuthorizationControllerDidFinish:", func(self ID, _ SEL, c ID) {
			Send(c, "dismissWithCompletion:", 0)
			if c != pay.controller {
				return
			}
			Release(c)
			pay.controller = 0
			pay.pending = map[int]func(spec.Answer){}
			pay.send(map[string]any{"type": "done"}, nil)
		}),
	})
	pay.delegate = Send(cls, "new")
	return pay.delegate
}

// payOr is an array, or an empty one for none.
func payOr(a ID) ID {
	if a == 0 {
		return Array()
	}
	return a
}

// The request.

// payNetworkNames are PKPaymentNetwork's constants by asuka/sdk/pay's names.
var payNetworkNames = [][2]string{
	{"visa", "PKPaymentNetworkVisa"}, {"mastercard", "PKPaymentNetworkMasterCard"}, {"amex", "PKPaymentNetworkAmex"},
	{"discover", "PKPaymentNetworkDiscover"}, {"jcb", "PKPaymentNetworkJCB"}, {"interac", "PKPaymentNetworkInterac"},
	{"unionpay", "PKPaymentNetworkChinaUnionPay"}, {"maestro", "PKPaymentNetworkMaestro"}, {"electron", "PKPaymentNetworkElectron"},
	{"elo", "PKPaymentNetworkElo"}, {"cartesBancaires", "PKPaymentNetworkCartesBancaires"}, {"eftpos", "PKPaymentNetworkEftpos"},
	{"mada", "PKPaymentNetworkMada"}, {"mir", "PKPaymentNetworkMir"}, {"girocard", "PKPaymentNetworkGirocard"},
	{"bancontact", "PKPaymentNetworkBancontact"}, {"dankort", "PKPaymentNetworkDankort"}, {"privateLabel", "PKPaymentNetworkPrivateLabel"},
}

func payNetworks(names []string) []ID {
	lib := framework("PassKit")
	var out []ID
	for _, n := range names {
		for _, x := range payNetworkNames {
			if x[0] == n {
				if c := Const(lib, x[1]); c != 0 {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

// payNetworkName is a PKPaymentNetwork's asuka/sdk/pay name.
func payNetworkName(n ID) string {
	if n == 0 {
		return ""
	}
	lib := framework("PassKit")
	for _, x := range payNetworkNames {
		if c := Const(lib, x[1]); c != 0 && SendBool(c, "isEqualToString:", uintptr(n)) {
			return x[0]
		}
	}
	return strings.ToLower(GoString(n))
}

func payCapabilities(names []string) uintptr {
	var c uintptr
	for _, n := range names {
		switch n {
		case "3DS":
			c |= 1
		case "EMV":
			c |= 2
		case "credit":
			c |= 4
		case "debit":
			c |= 8
		}
	}
	if c == 0 {
		c = 1
	}
	return c
}

func payFields(names []string) ID {
	lib := framework("PassKit")
	consts := map[string]string{"name": "PKContactFieldName", "email": "PKContactFieldEmailAddress", "phone": "PKContactFieldPhoneNumber",
		"address": "PKContactFieldPostalAddress", "phoneticName": "PKContactFieldPhoneticName"}
	var fs []ID
	for _, n := range names {
		if c := Const(lib, consts[n]); c != 0 {
			fs = append(fs, c)
		}
	}
	return Send(Class("NSSet"), "setWithArray:", uintptr(Array(fs...)))
}

func payDecimal(s string) ID {
	if s == "" {
		s = "0"
	}
	posix := Send(Class("NSLocale"), "localeWithLocaleIdentifier:", uintptr(String("en_US_POSIX")))
	return Send(Class("NSDecimalNumber"), "decimalNumberWithString:locale:", uintptr(String(s)), uintptr(posix))
}

func payDate(ms int64) ID {
	if ms <= 0 {
		return 0
	}
	return MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSince1970:"), float64(ms)/1000)
}

// payItem is a PKPaymentSummaryItem (or its kind's, where the Mac has it).
func payItem(it spec.AppleItem) ID {
	typ := uintptr(0) // final
	if it.Pending {
		typ = 1
	}
	label, amount := uintptr(String(it.Label)), uintptr(payDecimal(it.Amount))
	item := func(cls string) ID { return Send(Class(cls), "summaryItemWithLabel:amount:type:", label, amount, typ) }
	switch it.Kind {
	case "recurring":
		if HasClass("PKRecurringPaymentSummaryItem") {
			r := item("PKRecurringPaymentSummaryItem")
			if d := payDate(it.StartMs); d != 0 {
				Send(r, "setStartDate:", uintptr(d))
			}
			if d := payDate(it.EndMs); d != 0 {
				Send(r, "setEndDate:", uintptr(d))
			}
			unit := uintptr(8) // NSCalendarUnitMonth
			switch it.Interval {
			case "day":
				unit = 16
			case "week":
				unit = 8192 // NSCalendarUnitWeekOfYear
			case "year":
				unit = 4
			}
			Send(r, "setIntervalUnit:", unit)
			Send(r, "setIntervalCount:", uintptr(max(it.Every, 1)))
			return r
		}
	case "deferred":
		if d := payDate(it.DateMs); d != 0 && HasClass("PKDeferredPaymentSummaryItem") {
			r := item("PKDeferredPaymentSummaryItem")
			Send(r, "setDeferredDate:", uintptr(d))
			return r
		}
	case "reload":
		if HasClass("PKAutomaticReloadPaymentSummaryItem") {
			r := item("PKAutomaticReloadPaymentSummaryItem")
			Send(r, "setThresholdAmount:", uintptr(payDecimal(it.Threshold)))
			return r
		}
	}
	return item("PKPaymentSummaryItem")
}

func paySummaryItems(items []spec.AppleItem) ID {
	out := make([]ID, 0, len(items))
	for _, it := range items {
		out = append(out, payItem(it))
	}
	return Array(out...)
}

// payDay is "2026-11-20" as NSDateComponents.
func payDay(s string) ID {
	p := strings.Split(s, "-")
	if len(p) != 3 {
		return 0
	}
	c := Autorelease(New("NSDateComponents"))
	cal := Send(Class("NSCalendar"), "calendarWithIdentifier:", uintptr(String("gregorian")))
	Send(c, "setCalendar:", uintptr(cal))
	for i, sel := range []string{"setYear:", "setMonth:", "setDay:"} {
		n, err := strconv.Atoi(p[i])
		if err != nil {
			return 0
		}
		Send(c, sel, uintptr(n))
	}
	return c
}

func payShippingMethods(ms []spec.AppleShippingMethod) ID {
	out := make([]ID, 0, len(ms))
	for _, m := range ms {
		x := Send(Class("PKShippingMethod"), "summaryItemWithLabel:amount:", uintptr(String(m.Label)), uintptr(payDecimal(m.Amount)))
		Send(x, "setIdentifier:", uintptr(String(m.ID)))
		if m.Detail != "" {
			Send(x, "setDetail:", uintptr(String(m.Detail)))
		}
		if from, to := payDay(m.From), payDay(m.To); from != 0 && to != 0 && HasClass("PKDateComponentsRange") && RespondsTo(x, "setDateComponentsRange:") {
			r := Autorelease(Send(Send(Class("PKDateComponentsRange"), "alloc"), "initWithStartDateComponents:endDateComponents:", uintptr(from), uintptr(to)))
			Send(x, "setDateComponentsRange:", uintptr(r))
		}
		out = append(out, x)
	}
	return Array(out...)
}

// payRequest is a PKPaymentRequest (retained) from the wire's.
func payRequest(a spec.AppleRequest) ID {
	r := New("PKPaymentRequest")
	Send(r, "setMerchantIdentifier:", uintptr(String(a.Merchant)))
	Send(r, "setCountryCode:", uintptr(String(a.Country)))
	Send(r, "setCurrencyCode:", uintptr(String(a.Currency)))
	Send(r, "setSupportedNetworks:", uintptr(Array(payNetworks(a.Networks)...)))
	Send(r, "setMerchantCapabilities:", payCapabilities(a.Capabilities))
	if len(a.CardCountries) > 0 {
		var cs []ID
		for _, c := range a.CardCountries {
			cs = append(cs, String(c))
		}
		Send(r, "setSupportedCountries:", uintptr(Send(Class("NSSet"), "setWithArray:", uintptr(Array(cs...)))))
	}
	Send(r, "setPaymentSummaryItems:", uintptr(paySummaryItems(a.Items)))
	Send(r, "setRequiredShippingContactFields:", uintptr(payFields(a.ShippingFields)))
	Send(r, "setRequiredBillingContactFields:", uintptr(payFields(a.BillingFields)))
	shipping := map[string]uintptr{"delivery": 1, "storePickup": 2, "servicePickup": 3}
	Send(r, "setShippingType:", shipping[a.ShippingType])
	if len(a.ShippingMethods) > 0 {
		Send(r, "setShippingMethods:", uintptr(payShippingMethods(a.ShippingMethods)))
	}
	if RespondsTo(r, "setShippingContactEditingMode:") {
		mode := uintptr(1) // available
		if a.ShippingFixed {
			mode = 2 // store pickup
		}
		Send(r, "setShippingContactEditingMode:", mode)
	}
	if RespondsTo(r, "setSupportsCouponCode:") {
		Send(r, "setSupportsCouponCode:", Bool(a.Coupons))
		if a.Coupon != "" {
			Send(r, "setCouponCode:", uintptr(String(a.Coupon)))
		}
	}
	if a.ApplicationData != "" {
		if b, err := base64.StdEncoding.DecodeString(a.ApplicationData); err == nil {
			Send(r, "setApplicationData:", uintptr(Data(b)))
		}
	}
	tokenURL := func(s string) uintptr {
		if s == "" {
			return 0
		}
		return uintptr(URL(s))
	}
	if x := a.Recurring; x != nil && HasClass("PKRecurringPaymentRequest") && RespondsTo(r, "setRecurringPaymentRequest:") {
		rr := Autorelease(Send(Send(Class("PKRecurringPaymentRequest"), "alloc"), "initWithPaymentDescription:regularBilling:managementURL:",
			uintptr(String(x.Description)), uintptr(payItem(x.Regular)), uintptr(URL(x.ManagementURL))))
		if x.Trial != nil {
			Send(rr, "setTrialBilling:", uintptr(payItem(*x.Trial)))
		}
		if x.BillingAgreement != "" {
			Send(rr, "setBillingAgreement:", uintptr(String(x.BillingAgreement)))
		}
		Send(rr, "setTokenNotificationURL:", tokenURL(x.TokenNotificationURL))
		Send(r, "setRecurringPaymentRequest:", uintptr(rr))
	}
	if x := a.Reload; x != nil && HasClass("PKAutomaticReloadPaymentRequest") && RespondsTo(r, "setAutomaticReloadPaymentRequest:") {
		rr := Autorelease(Send(Send(Class("PKAutomaticReloadPaymentRequest"), "alloc"), "initWithPaymentDescription:automaticReloadBilling:managementURL:",
			uintptr(String(x.Description)), uintptr(payItem(x.Billing)), uintptr(URL(x.ManagementURL))))
		if x.BillingAgreement != "" {
			Send(rr, "setBillingAgreement:", uintptr(String(x.BillingAgreement)))
		}
		Send(rr, "setTokenNotificationURL:", tokenURL(x.TokenNotificationURL))
		Send(r, "setAutomaticReloadPaymentRequest:", uintptr(rr))
	}
	if x := a.Deferred; x != nil && HasClass("PKDeferredPaymentRequest") && RespondsTo(r, "setDeferredPaymentRequest:") {
		d := Autorelease(Send(Send(Class("PKDeferredPaymentRequest"), "alloc"), "initWithPaymentDescription:deferredBilling:managementURL:",
			uintptr(String(x.Description)), uintptr(payItem(x.Billing)), uintptr(URL(x.ManagementURL))))
		if x.BillingAgreement != "" {
			Send(d, "setBillingAgreement:", uintptr(String(x.BillingAgreement)))
		}
		Send(d, "setTokenNotificationURL:", tokenURL(x.TokenNotificationURL))
		if f := payDate(x.FreeCancellationMs); f != 0 {
			Send(d, "setFreeCancellationDate:", uintptr(f))
			Send(d, "setFreeCancellationDateTimeZone:", uintptr(Send(Class("NSTimeZone"), "localTimeZone")))
		}
		Send(r, "setDeferredPaymentRequest:", uintptr(d))
	}
	return r
}

// The errors: PassKit's, which the sheet shows by their fields.

func payErrors(errs []spec.FieldError) ID {
	if len(errs) == 0 {
		return Array()
	}
	pk := Class("PKPaymentRequest")
	contacts := framework("Contacts")
	key := func(name string) uintptr { return uintptr(Const(contacts, name)) }
	field := func(name string) uintptr { return uintptr(Const(framework("PassKit"), name)) }
	parts := map[string]string{"Street": "CNPostalAddressStreetKey", "City": "CNPostalAddressCityKey", "State": "CNPostalAddressStateKey",
		"PostalCode": "CNPostalAddressPostalCodeKey", "Country": "CNPostalAddressISOCountryCodeKey"}
	var out []ID
	for _, e := range errs {
		msg := uintptr(String(e.Message))
		var err ID
		switch {
		case strings.HasPrefix(e.Field, "shipping") && parts[strings.TrimPrefix(e.Field, "shipping")] != "":
			err = Send(pk, "paymentShippingAddressInvalidErrorWithKey:localizedDescription:", key(parts[strings.TrimPrefix(e.Field, "shipping")]), msg)
		case e.Field == "billingAddress":
			err = Send(pk, "paymentBillingAddressInvalidErrorWithKey:localizedDescription:", key("CNPostalAddressStreetKey"), msg)
		case strings.HasPrefix(e.Field, "billing") && parts[strings.TrimPrefix(e.Field, "billing")] != "":
			err = Send(pk, "paymentBillingAddressInvalidErrorWithKey:localizedDescription:", key(parts[strings.TrimPrefix(e.Field, "billing")]), msg)
		case e.Field == "shippingAddress":
			err = Send(pk, "paymentContactInvalidErrorWithContactField:localizedDescription:", field("PKContactFieldPostalAddress"), msg)
		case e.Field == "shippingName":
			err = Send(pk, "paymentContactInvalidErrorWithContactField:localizedDescription:", field("PKContactFieldName"), msg)
		case e.Field == "shippingEmail":
			err = Send(pk, "paymentContactInvalidErrorWithContactField:localizedDescription:", field("PKContactFieldEmailAddress"), msg)
		case e.Field == "shippingPhone":
			err = Send(pk, "paymentContactInvalidErrorWithContactField:localizedDescription:", field("PKContactFieldPhoneNumber"), msg)
		case e.Field == "unserviceable":
			err = Send(pk, "paymentShippingAddressUnserviceableErrorWithLocalizedDescription:", msg)
		case e.Field == "couponInvalid" && RespondsTo(pk, "paymentCouponCodeInvalidErrorWithLocalizedDescription:"):
			err = Send(pk, "paymentCouponCodeInvalidErrorWithLocalizedDescription:", msg)
		case e.Field == "couponExpired" && RespondsTo(pk, "paymentCouponCodeExpiredErrorWithLocalizedDescription:"):
			err = Send(pk, "paymentCouponCodeExpiredErrorWithLocalizedDescription:", msg)
		}
		if err == 0 {
			// PKPaymentErrorDomain's: billingContactInvalidError (2) by
			// its name, else unknownError (-1).
			code, info := -1, Dict(Const(Foundation(), "NSLocalizedDescriptionKey"), String(e.Message))
			if e.Field == "billingName" {
				code = 2
				Send(info, "setObject:forKey:", field("PKContactFieldName"), uintptr(Const(framework("PassKit"), "PKPaymentErrorContactFieldUserInfoKey")))
			}
			err = Send(Class("NSError"), "errorWithDomain:code:userInfo:", uintptr(Const(framework("PassKit"), "PKPaymentErrorDomain")), uintptr(code), uintptr(info))
		}
		out = append(out, err)
	}
	return Array(out...)
}

// PassKit's objects as the wire's.

func payPayment(p ID) spec.ApplePayment {
	t := Send(p, "token")
	out := spec.ApplePayment{Token: string(Bytes(Send(t, "paymentData"))), TransactionID: GoString(Send(t, "transactionIdentifier")),
		Card: payCard(Send(t, "paymentMethod")), Shipping: payContact(Send(p, "shippingContact")), Billing: payContact(Send(p, "billingContact"))}
	if m := Send(p, "shippingMethod"); m != 0 {
		out.ShippingMethod = GoString(Send(m, "identifier"))
	}
	return out
}

func payCard(m ID) spec.Card {
	if m == 0 {
		return spec.Card{}
	}
	c := spec.Card{Name: GoString(Send(m, "displayName")), Network: payNetworkName(Send(m, "network"))}
	types := []string{"", "debit", "credit", "prepaid", "store", "eMoney"}
	if t := SendInt(m, "type"); t > 0 && t < len(types) {
		c.Type = types[t]
	}
	if RespondsTo(m, "billingAddress") {
		if b := Send(m, "billingAddress"); b != 0 {
			bc := &spec.Contact{GivenName: GoString(Send(b, "givenName")), FamilyName: GoString(Send(b, "familyName"))}
			if as := ArrayItems(Send(b, "postalAddresses")); len(as) > 0 {
				bc.Address = payAddress(Send(as[0], "value"))
			}
			c.Billing = bc
		}
	}
	return c
}

func payContact(c ID) *spec.Contact {
	if c == 0 {
		return nil
	}
	out := &spec.Contact{Email: GoString(Send(c, "emailAddress"))}
	if n := Send(c, "name"); n != 0 {
		out.GivenName, out.FamilyName = GoString(Send(n, "givenName")), GoString(Send(n, "familyName"))
		out.Name = GoString(Send(Class("NSPersonNameComponentsFormatter"), "localizedStringFromPersonNameComponents:style:options:", uintptr(n), 2, 0))
	}
	if p := Send(c, "phoneNumber"); p != 0 {
		out.Phone = GoString(Send(p, "stringValue"))
	}
	out.Address = payAddress(Send(c, "postalAddress"))
	return out
}

func payAddress(a ID) *spec.Address {
	if a == 0 {
		return nil
	}
	s := func(sel string) string { return GoString(Send(a, sel)) }
	return &spec.Address{Street: s("street"), SubLocality: s("subLocality"), City: s("city"), SubAdministrativeArea: s("subAdministrativeArea"),
		State: s("state"), PostalCode: s("postalCode"), Country: s("country"), CountryCode: s("ISOCountryCode")}
}

// The button.

// payButton is pay.Button: a PKPaymentButton in a box of the host's,
// made again when its type or style changes (they're fixed as it's
// made). Props: appleType (PKPaymentButtonType), style (automatic,
// black, white, white outline), cornerRadius (-1: the system's); event:
// press.
type payButton struct {
	ctx         native.Context
	box, button ID
	typ, style  int
	radius      float64
}

func newPayButton(ctx native.Context) native.View {
	if !loadPassKit() || !HasClass("PKPaymentButton") {
		return nil
	}
	if !pay.classes {
		pay.classes = true
		DefineClass("AsukaPayButtonBox", "NSView", nil, []objc.MethodDef{
			Method("isFlipped", func(self ID, _ SEL) bool { return true }),
			Method("pressed:", func(self ID, _ SEL, sender ID) {
				if b := pay.buttons[self]; b != nil {
					b.ctx.Emit("press", nil)
				}
			}),
		})
	}
	if pay.buttons == nil {
		pay.buttons = map[ID]*payButton{}
	}
	b := &payButton{ctx: ctx, radius: -1}
	b.box = MsgInitRect(Send(Class("AsukaPayButtonBox"), "alloc"), Sel("initWithFrame:"), Rect{Size: Size{Width: 200, Height: 30}})
	pay.buttons[b.box] = b
	b.make()
	return b
}

func (b *payButton) Handle() uintptr { return uintptr(b.box) }

func (b *payButton) Set(name string, v any) {
	switch name {
	case "appleType":
		n, _ := v.(int64)
		b.typ = int(n)
	case "style":
		n, _ := v.(int64)
		b.style = int(n)
	case "cornerRadius":
		f, _ := v.(float64)
		b.radius = -1
		if f >= 0 {
			b.radius = f
		}
		if b.radius >= 0 && b.button != 0 && RespondsTo(b.button, "setCornerRadius:") {
			MsgSetFloat(b.button, Sel("setCornerRadius:"), b.radius)
		}
		return
	default:
		return
	}
	b.make()
}

// payButtonStyle is a style prop (automatic, black, white, white
// outline) as PKPaymentButtonStyle (white 0, white outline 1, black 2,
// automatic 3).
func payButtonStyle(style int) uintptr {
	return [...]uintptr{3, 2, 0, 1}[min(max(style, 0), 3)]
}

func (b *payButton) make() {
	if b.button != 0 {
		Send(b.button, "removeFromSuperview")
		Release(b.button)
	}
	btn := Retain(Send(Class("PKPaymentButton"), "buttonWithType:style:", uintptr(max(b.typ, 0)), payButtonStyle(b.style)))
	if b.radius >= 0 && RespondsTo(btn, "setCornerRadius:") {
		MsgSetFloat(btn, Sel("setCornerRadius:"), b.radius)
	}
	Send(btn, "setTarget:", uintptr(b.box))
	Send(btn, "setAction:", uintptr(Sel("pressed:")))
	MsgSetRect(btn, Sel("setFrame:"), MsgRect(b.box, Sel("bounds")))
	Send(btn, "setAutoresizingMask:", 18)
	Send(b.box, "addSubview:", uintptr(btn))
	b.button = btn
}

// Measure: as wide as it's given (at least Apple's 140), 30 points tall
// (Apple's least).
func (b *payButton) Measure(w, _ float32) (float32, float32) {
	if !finite(w) {
		w = 200
	}
	return max(min(w, 600), 140), 30
}

func (b *payButton) Destroy() {
	delete(pay.buttons, b.box)
	if b.button != 0 {
		Send(b.button, "setTarget:", 0)
		Release(b.button)
		b.button = 0
	}
	Release(b.box)
}

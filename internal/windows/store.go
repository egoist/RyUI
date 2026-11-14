//go:build windows

package windows

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// The Microsoft Store, for an app installed from it (a packaged app):
// Windows.Services.Store's StoreContext, its add-ons (products), their
// purchase, consumables' fulfillment and the licenses the user has. A
// desktop app's context shows its dialogs over a window it's initialized
// with (IInitializeWithWindow). The calls wait for their operations: make
// them off the main thread, in COM's multithreaded apartment (WithCOM).

var (
	iidStoreContextStatics     = NewGUID("9C06EE5F-15C0-4E72-9330-D6191CEBD19C")
	iidStoreContext            = NewGUID("AC98B6BE-F4FD-4912-BABD-5035E5E8BCAB")
	iidStoreProductQueryResult = NewGUID("D805E6C5-D456-4FF6-8049-9076D5165F73")
	iidStoreProduct            = NewGUID("320E2C52-D760-450A-A42B-67D1E901AC90")
	iidStorePurchaseResult     = NewGUID("ADD28552-F96A-463D-A7BB-C20B4FCA6952")
	iidStoreConsumableResult   = NewGUID("EA5DAB72-6A00-4052-BE5B-BFDAB4433352")
	iidStoreAppLicense         = NewGUID("F389F9DE-73C0-45CE-9BAB-B2FE3E5EAFD3")
	iidStoreLicense            = NewGUID("26DC9579-4C4F-4F30-BC89-649F60E36055")
	iidInitializeWithWindow    = NewGUID("3E68D4BD-7135-4D10-8018-9FB6D9F33FA1")

	storeRC = func(name string, iid GUID) string { return RuntimeClassSig("Windows.Services.Store."+name, iid) }

	iidProductQueryCompleted = ParamIID("pinterface(" + asyncHandler + ";" + storeRC("StoreProductQueryResult", iidStoreProductQueryResult) + ")")
	iidPurchaseCompleted     = ParamIID("pinterface(" + asyncHandler + ";" + storeRC("StorePurchaseResult", iidStorePurchaseResult) + ")")
	iidConsumableCompleted   = ParamIID("pinterface(" + asyncHandler + ";" + storeRC("StoreConsumableResult", iidStoreConsumableResult) + ")")
	iidAppLicenseCompleted   = ParamIID("pinterface(" + asyncHandler + ";" + storeRC("StoreAppLicense", iidStoreAppLicense) + ")")
	// IIterable<IKeyValuePair<String, T>>: a map view's pairs.
	iidProductPairs = ParamIID("pinterface({faa585ea-6214-4217-afda-7f46de5869b3};pinterface({02b51929-c1c4-4a7e-8940-0381e2b86a5f};string;" +
		storeRC("StoreProduct", iidStoreProduct) + "))")
	iidLicensePairs = ParamIID("pinterface({faa585ea-6214-4217-afda-7f46de5869b3};pinterface({02b51929-c1c4-4a7e-8940-0381e2b86a5f};string;" +
		storeRC("StoreLicense", iidStoreLicense) + "))")
	iidLicensesChanged = ParamIID("pinterface({9de1c534-6ae1-11e0-84e1-18a905bcc53f};" +
		storeRC("StoreContext", iidStoreContext) + ";cinterface(IInspectable))")
	// IIterable<String>, IIterator<String>: the kinds asked for.
	iidIterableStrings = ParamIID("pinterface({faa585ea-6214-4217-afda-7f46de5869b3};string)")
	iidIteratorStrings = ParamIID("pinterface({6a79e863-4300-459a-9966-cbb660963ee1};string)")
)

// StoreAddOn is an add-on the Store sells (StoreProduct).
type StoreAddOn struct {
	// StoreID is the Store's (9NBLGGH...); OfferToken, the product ID the
	// app gave it in Partner Center (its InAppOfferToken).
	StoreID, OfferToken string
	Title, Description  string
	// Kind is the Store's: "Durable", "Consumable" (the app's to
	// fulfill), "UnmanagedConsumable" (the Store keeps its balance).
	Kind string
	// Price is for display; Micros and Currency, from its listing's data.
	Price    string
	Micros   int64
	Currency string
	// A subscription's: its billing period and trial ("Day", "Week",
	// "Month", "Year").
	Subscription            bool
	PeriodUnit, TrialUnit   string
	PeriodCount, TrialCount int
	// Owned: in the user's collection.
	Owned bool
}

// StoreAddOnLicense is a license the user has for an add-on.
type StoreAddOnLicense struct {
	SkuStoreID, OfferToken string
	Active                 bool
	// Expires is a subscription's end (zero for a durable's, which
	// doesn't).
	Expires time.Time
}

// Store purchase and fulfillment statuses.
const (
	StorePurchased        = 0 // StorePurchaseStatus.Succeeded
	StoreAlreadyPurchased = 1
	StoreNotPurchased     = 2
	StoreNetworkError     = 3
	StoreServerError      = 4
)

// ErrNotPackaged: the app wasn't installed from the Store (it has no
// package identity), so it has no Store context.
var ErrNotPackaged = errors.New("not available: the Microsoft Store sells only to an app installed from it (packaged)")

// StoreContext is the Store's context for the user, its dialogs owned by
// a window.
type StoreContext struct {
	ctx uintptr
	mu  sync.Mutex
	// event: the licenses-changed handler's registration.
	event   int64
	changed uintptr
}

// NewStoreContext makes the Store's context for the signed-in user, its
// dialogs over hwnd.
func NewStoreContext(hwnd uintptr) (*StoreContext, error) {
	if !Packaged() {
		return nil, ErrNotPackaged
	}
	st, err := ActivationFactory("Windows.Services.Store.StoreContext", iidStoreContextStatics)
	if err != nil {
		return nil, err
	}
	defer Release(st)
	var ctx uintptr
	if hr := ComCall(st, 6, uintptr(unsafe.Pointer(&ctx))); Failed(hr) || ctx == 0 { // GetDefault
		return nil, fmt.Errorf("StoreContext: %#x", uint32(hr))
	}
	if hwnd != 0 {
		if iw := QueryInterface(ctx, &iidInitializeWithWindow); iw != 0 {
			ComCall(iw, 3, hwnd) // Initialize
			Release(iw)
		}
	}
	return &StoreContext{ctx: ctx}, nil
}

// Close lets the context go.
func (s *StoreContext) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed != 0 {
		ComCall(s.ctx, 8, uintptr(s.event)) // remove_OfflineLicensesChanged
		Release(s.changed)
		s.changed = 0
	}
	if s.ctx != 0 {
		Release(s.ctx)
		s.ctx = 0
	}
}

// strings is an IIterable<String> of Go's, holding one reference.
type stringsIterable struct{ list []string }

type stringsIterator struct {
	of *stringsIterable
	at int
}

var (
	stringsIterableVtbl [16]uintptr
	stringsIteratorVtbl [16]uintptr
	stringsSetup        sync.Once
)

func newStrings(ss []string) uintptr {
	stringsSetup.Do(func() {
		base := inspectableVtbl()
		copy(stringsIterableVtbl[:], base[:])
		stringsIterableVtbl[6] = syscall.NewCallback(func(this, out uintptr) uintptr { // First
			o := objectOf(this)
			if o == nil {
				return 0x80004005
			}
			*(*uintptr)(native(out)) = newObject(&stringsIteratorVtbl, iidIteratorStrings, &stringsIterator{of: o.impl.(*stringsIterable)})
			return 0
		})
		copy(stringsIteratorVtbl[:], base[:])
		cur := func(this uintptr) (*stringsIterator, bool) {
			o := objectOf(this)
			if o == nil {
				return nil, false
			}
			i := o.impl.(*stringsIterator)
			return i, i.at < len(i.of.list)
		}
		stringsIteratorVtbl[6] = syscall.NewCallback(func(this, out uintptr) uintptr { // get_Current
			i, ok := cur(this)
			if !ok {
				return 0x8000000B // E_BOUNDS
			}
			*(*uintptr)(native(out)) = uintptr(NewHString(i.of.list[i.at]))
			return 0
		})
		stringsIteratorVtbl[7] = syscall.NewCallback(func(this, out uintptr) uintptr { // get_HasCurrent
			_, ok := cur(this)
			*(*uint8)(native(out)) = boolByte(ok)
			return 0
		})
		stringsIteratorVtbl[8] = syscall.NewCallback(func(this, out uintptr) uintptr { // MoveNext
			i, ok := cur(this)
			if ok {
				i.at++
			}
			_, ok = cur(this)
			*(*uint8)(native(out)) = boolByte(ok)
			return 0
		})
		stringsIteratorVtbl[9] = syscall.NewCallback(func(this, capacity, out, actual uintptr) uintptr { // GetMany
			i, _ := cur(this)
			n := 0
			for i != nil && n < int(capacity) && i.at < len(i.of.list) {
				*(*uintptr)(unsafe.Add(native(out), n*int(unsafe.Sizeof(uintptr(0))))) = uintptr(NewHString(i.of.list[i.at]))
				i.at++
				n++
			}
			*(*uint32)(native(actual)) = uint32(n)
			return 0
		})
	})
	return newObject(&stringsIterableVtbl, iidIterableStrings, &stringsIterable{list: ss})
}

// pairs are a map view's values (references the caller releases), by
// key, through its IIterable of pairs (iid).
func pairs(m uintptr, iid GUID) map[string]uintptr {
	out := map[string]uintptr{}
	it := QueryInterface(m, &iid)
	if it == 0 {
		return out
	}
	defer Release(it)
	var iter uintptr
	if Failed(ComCall(it, 6, uintptr(unsafe.Pointer(&iter)))) || iter == 0 { // First
		return out
	}
	defer Release(iter)
	for {
		var has uint8
		ComCall(iter, 7, uintptr(unsafe.Pointer(&has))) // get_HasCurrent
		if has == 0 {
			break
		}
		var kv uintptr
		if !Failed(ComCall(iter, 6, uintptr(unsafe.Pointer(&kv)))) && kv != 0 { // get_Current
			key := hstringOf(kv, 6)
			var v uintptr
			if !Failed(ComCall(kv, 7, uintptr(unsafe.Pointer(&v)))) && v != 0 { // get_Value
				out[key] = v
			}
			Release(kv)
		}
		ComCall(iter, 8, uintptr(unsafe.Pointer(&has))) // MoveNext
	}
	return out
}

// extendedError is a result's ExtendedError (an HRESULT, at index i).
func extendedError(obj uintptr, i int) error {
	var hr int32
	ComCall(obj, i, uintptr(unsafe.Pointer(&hr)))
	if hr < 0 {
		return fmt.Errorf("the Microsoft Store: %#x", uint32(hr))
	}
	return nil
}

// AddOns are the add-ons the Store sells with the app (durables,
// consumables, subscriptions).
func (s *StoreContext) AddOns() ([]StoreAddOn, error) {
	kinds := newStrings([]string{"Durable", "Consumable", "UnmanagedConsumable"})
	defer Release(kinds)
	var op uintptr
	if hr := ComCall(s.ctx, 14, kinds, uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 { // GetAssociatedStoreProductsAsync
		return nil, fmt.Errorf("the Microsoft Store: %#x", uint32(hr))
	}
	res := awaitOp(op, iidProductQueryCompleted)
	Release(op)
	if res == 0 {
		return nil, errors.New("the Microsoft Store didn't answer")
	}
	defer Release(res)
	if err := extendedError(res, 7); err != nil {
		return nil, err
	}
	var m uintptr
	if Failed(ComCall(res, 6, uintptr(unsafe.Pointer(&m)))) || m == 0 { // get_Products
		return nil, nil
	}
	defer Release(m)
	var out []StoreAddOn
	for _, p := range pairs(m, iidProductPairs) {
		out = append(out, addOn(p))
		Release(p)
	}
	return out, nil
}

// addOn reads a StoreProduct.
func addOn(p uintptr) StoreAddOn {
	a := StoreAddOn{StoreID: hstringOf(p, 6), Title: hstringOf(p, 8), Description: hstringOf(p, 9), Kind: hstringOf(p, 10),
		OfferToken: hstringOf(p, 23)}
	var owned uint8
	ComCall(p, 16, uintptr(unsafe.Pointer(&owned))) // get_IsInUserCollection
	a.Owned = owned != 0
	var price uintptr
	if !Failed(ComCall(p, 17, uintptr(unsafe.Pointer(&price)))) && price != 0 { // get_Price
		a.Price, a.Currency = hstringOf(price, 7), hstringOf(price, 10) // FormattedPrice, CurrencyCode
		Release(price)
	}
	var skus uintptr
	if !Failed(ComCall(p, 15, uintptr(unsafe.Pointer(&skus)))) && skus != 0 { // get_Skus
		list := vectorItems(skus)
		Release(skus)
		if len(list) > 0 {
			sku := list[0]
			a.Micros = listPriceMicros(hstringOf(sku, 16)) // ExtendedJsonData
			var sub uint8
			ComCall(sku, 23, uintptr(unsafe.Pointer(&sub))) // get_IsSubscription
			if sub != 0 {
				var info uintptr
				if !Failed(ComCall(sku, 24, uintptr(unsafe.Pointer(&info)))) && info != 0 { // get_SubscriptionInfo
					a.Subscription = true
					var n uint32
					var unit, tunit int32
					var trial uint8
					ComCall(info, 6, uintptr(unsafe.Pointer(&n)))    // BillingPeriod
					ComCall(info, 7, uintptr(unsafe.Pointer(&unit))) // BillingPeriodUnit
					a.PeriodCount, a.PeriodUnit = int(n), durationUnit(unit)
					ComCall(info, 8, uintptr(unsafe.Pointer(&trial))) // HasTrialPeriod
					if trial != 0 {
						ComCall(info, 9, uintptr(unsafe.Pointer(&n)))      // TrialPeriod
						ComCall(info, 10, uintptr(unsafe.Pointer(&tunit))) // TrialPeriodUnit
						a.TrialCount, a.TrialUnit = int(n), durationUnit(tunit)
					}
					Release(info)
				}
			}
		}
		releaseAll(list)
	}
	return a
}

// durationUnit is StoreDurationUnit's name.
func durationUnit(u int32) string {
	switch u {
	case 0, 1, 2: // Minute, Hour, Day
		return "Day"
	case 3:
		return "Week"
	case 4:
		return "Month"
	}
	return "Year"
}

// listPriceMicros reads a SKU's list price from its listing's JSON
// (Availabilities[0].OrderManagementData.Price.ListPrice): StorePrice has
// only formatted strings.
func listPriceMicros(js string) int64 {
	var d struct {
		Availabilities []struct {
			OrderManagementData struct {
				Price struct {
					ListPrice float64
				}
			}
		}
	}
	if json.Unmarshal([]byte(js), &d) != nil || len(d.Availabilities) == 0 {
		return 0
	}
	return int64(d.Availabilities[0].OrderManagementData.Price.ListPrice*1e6 + 0.5)
}

// Purchase shows the Store's purchase dialog for the add-on storeID, and
// answers its status (StorePurchased...).
func (s *StoreContext) Purchase(storeID string) (int, error) {
	id := NewHString(storeID)
	defer id.Delete()
	var op uintptr
	if hr := ComCall(s.ctx, 21, uintptr(id), uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 { // RequestPurchaseAsync
		return StoreServerError, fmt.Errorf("the Microsoft Store: %#x", uint32(hr))
	}
	res := awaitOp(op, iidPurchaseCompleted)
	Release(op)
	if res == 0 {
		return StoreServerError, errors.New("the Microsoft Store didn't answer")
	}
	defer Release(res)
	var status int32
	ComCall(res, 6, uintptr(unsafe.Pointer(&status))) // get_Status
	return int(status), extendedError(res, 7)
}

// NewTrackingID is a fulfillment's ID: a GUID, as "{...}".
func NewTrackingID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("{%x-%x-%x-%x-%x}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Fulfill reports a consumable's purchase delivered (quantity of it,
// under the tracking ID, NewTrackingID's), so the user can buy it again
// (StoreConsumableStatus: 0 succeeded).
func (s *StoreContext) Fulfill(storeID string, quantity uint32, tracking string) (int, error) {
	id := NewHString(storeID)
	defer id.Delete()
	g := NewGUID(tracking[1 : len(tracking)-1])
	var op uintptr
	var hr uintptr
	// ReportConsumableFulfillmentAsync(HSTRING, UINT32, GUID, out): a GUID
	// goes by value, by reference on x64, in two registers on ARM64.
	if runtime.GOARCH == "arm64" {
		w := (*[2]uintptr)(unsafe.Pointer(&g))
		hr = ComCall(s.ctx, 18, uintptr(id), uintptr(quantity), w[0], w[1], uintptr(unsafe.Pointer(&op)))
	} else {
		hr = ComCall(s.ctx, 18, uintptr(id), uintptr(quantity), uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&op)))
	}
	if Failed(hr) || op == 0 {
		return 3, fmt.Errorf("the Microsoft Store: %#x", uint32(hr))
	}
	res := awaitOp(op, iidConsumableCompleted)
	Release(op)
	if res == 0 {
		return 3, errors.New("the Microsoft Store didn't answer")
	}
	defer Release(res)
	var status int32
	ComCall(res, 6, uintptr(unsafe.Pointer(&status))) // get_Status
	return int(status), extendedError(res, 9)
}

// Licenses are the user's licenses for the app's add-ons.
func (s *StoreContext) Licenses() ([]StoreAddOnLicense, error) {
	var op uintptr
	if hr := ComCall(s.ctx, 11, uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 { // GetAppLicenseAsync
		return nil, fmt.Errorf("the Microsoft Store: %#x", uint32(hr))
	}
	lic := awaitOp(op, iidAppLicenseCompleted)
	Release(op)
	if lic == 0 {
		return nil, errors.New("the Microsoft Store didn't answer")
	}
	defer Release(lic)
	var m uintptr
	if Failed(ComCall(lic, 11, uintptr(unsafe.Pointer(&m)))) || m == 0 { // get_AddOnLicenses
		return nil, nil
	}
	defer Release(m)
	var out []StoreAddOnLicense
	for _, l := range pairs(m, iidLicensePairs) {
		a := StoreAddOnLicense{SkuStoreID: hstringOf(l, 6), OfferToken: hstringOf(l, 10)}
		var active uint8
		ComCall(l, 7, uintptr(unsafe.Pointer(&active))) // get_IsActive
		a.Active = active != 0
		var ticks int64
		ComCall(l, 8, uintptr(unsafe.Pointer(&ticks))) // get_ExpirationDate: 100 ns since 1601
		if t := winTime(ticks); t.Year() < 9000 {
			a.Expires = t
		}
		out = append(out, a)
		Release(l)
	}
	return out, nil
}

// winTime is a WinRT DateTime.
func winTime(ticks int64) time.Time {
	const epoch = 116444736000000000 // 1970 in 100 ns since 1601
	if ticks <= 0 {
		return time.Time{}
	}
	return time.Unix(0, (ticks-epoch)*100)
}

// OnLicensesChanged calls f (on another thread) as the user's licenses
// change: an add-on bought on another device, a subscription renewed or
// ended, a refund.
func (s *StoreContext) OnLicensesChanged(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed != 0 {
		return
	}
	s.changed = NewDelegate(iidLicensesChanged, func(_, _ uintptr) { f() })
	ComCall(s.ctx, 7, s.changed, uintptr(unsafe.Pointer(&s.event))) // add_OfflineLicensesChanged
}

var procGetUserDefaultGeoName = kernel32.NewProc("GetUserDefaultGeoName")

// UserCountry is the user's country or region, as Settings' Region has it
// (ISO 3166-1 alpha-2: what the Store's prices follow), "" if Windows
// doesn't say.
func UserCountry() string {
	if !Has(procGetUserDefaultGeoName) {
		return ""
	}
	var buf [16]uint16
	n, _, _ := procGetUserDefaultGeoName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

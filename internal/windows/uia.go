//go:build windows

package windows

import (
	"math"
	"syscall"
	"unsafe"
)

// UI Automation, as screen readers (Narrator, NVDA) see the host's views:
// a provider of Go's for a view's window (IRawElementProviderSimple,
// returned for WM_GETOBJECT), on top of the window's own (its host
// provider: position, focus), saying its name, help text, control type
// and whether a screen reader skips it, with the Invoke pattern for a view
// that presses; and notifications (UiaRaiseNotificationEvent), for what a
// screen reader announces.

var (
	uiautomationcore = systemDLL("UIAutomationCore.dll")
	oleaut32         = systemDLL("oleaut32.dll")

	procUiaReturnRawElementProvider = uiautomationcore.NewProc("UiaReturnRawElementProvider")
	procUiaHostProviderFromHwnd     = uiautomationcore.NewProc("UiaHostProviderFromHwnd")
	procUiaRaiseNotificationEvent   = uiautomationcore.NewProc("UiaRaiseNotificationEvent")
	procUiaClientsAreListening      = uiautomationcore.NewProc("UiaClientsAreListening")
	procUiaRaisePropertyChanged     = uiautomationcore.NewProc("UiaRaiseAutomationPropertyChangedEvent")
	procSysAllocString              = oleaut32.NewProc("SysAllocString")
	procSysFreeString               = oleaut32.NewProc("SysFreeString")
)

const (
	WM_GETOBJECT    = 0x003D
	UiaRootObjectID = -25

	// Property IDs.
	UIA_ControlTypePropertyId      = 30003
	UIA_NamePropertyId             = 30005
	UIA_HelpTextPropertyId         = 30013
	UIA_IsControlElementPropertyId = 30016
	UIA_IsContentElementPropertyId = 30017
	UIA_HeadingLevelPropertyId     = 30173
	UIA_RangeValueValuePropertyId  = 30047
	UIA_ItemStatusPropertyId       = 30026

	// Control types.
	UIA_ButtonControlTypeId      = 50000
	UIA_EditControlTypeId        = 50004
	UIA_HyperlinkControlTypeId   = 50005
	UIA_ImageControlTypeId       = 50006
	UIA_ListItemControlTypeId    = 50007
	UIA_ProgressBarControlTypeId = 50012
	UIA_TabItemControlTypeId     = 50019
	UIA_TextControlTypeId        = 50020
	UIA_GroupControlTypeId       = 50026

	HeadingLevel1 = 80051

	uiaInvokePatternId     = 10000
	uiaRangeValuePatternId = 10003
)

var (
	iidIRawElementProviderSimple = NewGUID("d6dd68d1-86fd-4332-8666-9abedea2d24c")
	iidIInvokeProvider           = NewGUID("54fcb24b-e18e-47a2-b4d3-eccbe77599a2")
	iidIRangeValueProvider       = NewGUID("36dc7aef-33e6-4691-afe1-2be7274b3d33")

	uiaOnce      bool
	providerVtbl [7]uintptr
	invokeVtbl   [4]uintptr
	rangeVtbl    [10]uintptr
	uiaAvailable bool
)

// Accessible is a view's side of its provider. Property returns a
// property's value (a string, an int32, a bool), or nil for the window's
// own; Invokes says whether it presses, Invoke presses it.
type Accessible interface {
	Property(id int32) any
	Invokes() bool
	Invoke()
}

// Ranged is an Accessible with a value in a range that screen readers
// read and the user doesn't set (a progress indicator): UI Automation's
// RangeValue pattern, read only. Range reports the value and its bounds,
// and whether it has one now (an indeterminate indicator hasn't).
type Ranged interface {
	Range() (value, min, max float64, ok bool)
}

type uiaProvider struct {
	hwnd uintptr
	acc  Accessible
}

func uiaInit() bool {
	if uiaOnce {
		return uiaAvailable
	}
	uiaOnce = true
	if procUiaReturnRawElementProvider.Find() != nil || procUiaHostProviderFromHwnd.Find() != nil {
		return false
	}
	comInit()
	qi := syscall.NewCallback(comQueryInterface)
	addRef := syscall.NewCallback(func(this uintptr) uintptr { return uintptr(comRef(this, 1)) })
	release := syscall.NewCallback(func(this uintptr) uintptr { return uintptr(comRef(this, -1)) })
	providerVtbl = [7]uintptr{qi, addRef, release,
		syscall.NewCallback(func(this, out uintptr) uintptr { // get_ProviderOptions
			*(*int32)(native(out)) = 1 // ProviderOptions_ServerSideProvider
			return 0
		}),
		syscall.NewCallback(providerPattern),
		syscall.NewCallback(providerProperty),
		syscall.NewCallback(func(this, out uintptr) uintptr { // get_HostRawElementProvider
			p, _ := comImpl(this).(*uiaProvider)
			*(*uintptr)(native(out)) = 0
			if p == nil {
				return 0
			}
			var host uintptr
			procUiaHostProviderFromHwnd.Call(p.hwnd, uintptr(unsafe.Pointer(&host)))
			*(*uintptr)(native(out)) = host
			return 0
		}),
	}
	invokeVtbl = [4]uintptr{qi, addRef, release,
		syscall.NewCallback(func(this uintptr) uintptr {
			if p, _ := comImpl(this).(*uiaProvider); p != nil && p.acc.Invokes() {
				p.acc.Invoke()
			}
			return 0
		}),
	}
	// IRangeValueProvider, read only: SetValue (a double, which a
	// callback of Go's can't take) refuses; the getters write doubles.
	getter := func(f func(v, lo, hi float64) float64) uintptr {
		return syscall.NewCallback(func(this, out uintptr) uintptr {
			*(*float64)(native(out)) = 0
			if p, _ := comImpl(this).(*uiaProvider); p != nil {
				if r, ok := p.acc.(Ranged); ok {
					if v, lo, hi, ok := r.Range(); ok {
						*(*float64)(native(out)) = f(v, lo, hi)
					}
				}
			}
			return 0
		})
	}
	rangeVtbl = [10]uintptr{qi, addRef, release,
		syscall.NewCallback(func(this, value uintptr) uintptr { return uiaElementNotEnabled }), // SetValue
		getter(func(v, _, _ float64) float64 { return v }),                                     // get_Value
		syscall.NewCallback(func(this, out uintptr) uintptr { // get_IsReadOnly
			*(*int32)(native(out)) = 1
			return 0
		}),
		getter(func(_, _, hi float64) float64 { return hi }),               // get_Maximum
		getter(func(_, lo, _ float64) float64 { return lo }),               // get_Minimum
		getter(func(_, lo, hi float64) float64 { return (hi - lo) / 10 }),  // get_LargeChange
		getter(func(_, lo, hi float64) float64 { return (hi - lo) / 100 }), // get_SmallChange
	}
	uiaAvailable = true
	return true
}

// uiaElementNotEnabled is UIA_E_ELEMENTNOTENABLED: a read-only value
// refuses to be set.
const uiaElementNotEnabled = 0x80040200

func providerPattern(this uintptr, id int32, out uintptr) uintptr {
	*(*uintptr)(native(out)) = 0
	p, _ := comImpl(this).(*uiaProvider)
	if p == nil {
		return 0
	}
	switch id {
	case uiaInvokePatternId:
		if p.acc.Invokes() {
			*(*uintptr)(native(out)) = newComObject(&invokeVtbl[0], &iidIInvokeProvider, p)
		}
	case uiaRangeValuePatternId:
		if r, ok := p.acc.(Ranged); ok {
			if _, _, _, ok := r.Range(); ok {
				*(*uintptr)(native(out)) = newComObject(&rangeVtbl[0], &iidIRangeValueProvider, p)
			}
		}
	}
	return 0
}

// variant is a VARIANT: its type, then its value.
type variant struct {
	vt        uint16
	_         [3]uint16
	val, val2 uintptr
}

func providerProperty(this uintptr, id int32, out uintptr) uintptr {
	v := (*variant)(native(out))
	*v = variant{} // VT_EMPTY: the window's own
	p, _ := comImpl(this).(*uiaProvider)
	if p == nil {
		return 0
	}
	switch x := p.acc.Property(id).(type) {
	case string:
		v.vt = 8 // VT_BSTR
		v.val = SysAllocString(x)
	case int32:
		v.vt = 3 // VT_I4
		v.val = uintptr(uint32(x))
	case bool:
		v.vt = 11 // VT_BOOL
		if x {
			v.val = 0xFFFF // VARIANT_TRUE
		}
	}
	return 0
}

// NewProvider makes a view's provider, holding one reference; the host
// releases it (Release) once the window's gone.
func NewProvider(hwnd uintptr, acc Accessible) uintptr {
	if !uiaInit() {
		return 0
	}
	return newComObject(&providerVtbl[0], &iidIRawElementProviderSimple, &uiaProvider{hwnd: hwnd, acc: acc})
}

// ReturnProvider answers WM_GETOBJECT with a provider.
func ReturnProvider(hwnd, wp, lp, provider uintptr) uintptr {
	r, _, _ := procUiaReturnRawElementProvider.Call(hwnd, wp, lp, provider)
	return r
}

// DropProviders tells UI Automation a window's providers are gone.
func DropProviders(hwnd uintptr) {
	if uiaAvailable {
		procUiaReturnRawElementProvider.Call(hwnd, 0, 0, 0)
	}
}

// ClientsListening reports whether a screen reader (or another UI
// Automation client) is listening.
func ClientsListening() bool {
	if procUiaClientsAreListening.Find() != nil {
		return false
	}
	r, _, _ := procUiaClientsAreListening.Call()
	return r != 0
}

// Announce has screen readers say text, through hwnd's provider
// (Windows 10 1709 and later).
func Announce(hwnd uintptr, text string) {
	if procUiaRaiseNotificationEvent.Find() != nil || procUiaHostProviderFromHwnd.Find() != nil {
		return
	}
	var host uintptr
	if hr, _, _ := procUiaHostProviderFromHwnd.Call(hwnd, uintptr(unsafe.Pointer(&host))); Failed(hr) || host == 0 {
		return
	}
	defer Release(host)
	s, id := SysAllocString(text), SysAllocString("asuka.announce")
	defer SysFreeString(s)
	defer SysFreeString(id)
	const kindOther, processingImportantAll = 4, 0
	procUiaRaiseNotificationEvent.Call(host, kindOther, processingImportantAll, s, id)
}

// RaiseRangeValueChanged tells screen readers a provider's RangeValue
// changed, so they read a progress indicator's new value as it fills.
// The VARIANTs go by reference, as the 64-bit ABIs pass a struct their
// size.
func RaiseRangeValueChanged(provider uintptr, old, now float64) {
	if provider == 0 || procUiaRaisePropertyChanged.Find() != nil || unsafe.Sizeof(uintptr(0)) != 8 || !ClientsListening() {
		return
	}
	o := variant{vt: 5, val: uintptr(math.Float64bits(old))} // VT_R8
	n := variant{vt: 5, val: uintptr(math.Float64bits(now))}
	procUiaRaisePropertyChanged.Call(provider, UIA_RangeValueValuePropertyId, uintptr(unsafe.Pointer(&o)), uintptr(unsafe.Pointer(&n)))
}

// SysAllocString is a BSTR of s, which its taker frees.
func SysAllocString(s string) uintptr {
	r, _, _ := procSysAllocString.Call(Str(s))
	return r
}

func SysFreeString(b uintptr) { procSysFreeString.Call(b) }

//go:build windows

package windows

import (
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

	// Control types.
	UIA_ButtonControlTypeId    = 50000
	UIA_EditControlTypeId      = 50004
	UIA_HyperlinkControlTypeId = 50005
	UIA_ImageControlTypeId     = 50006
	UIA_TextControlTypeId      = 50020
	UIA_GroupControlTypeId     = 50026

	HeadingLevel1 = 80051

	uiaInvokePatternId = 10000
)

var (
	iidIRawElementProviderSimple = NewGUID("d6dd68d1-86fd-4332-8666-9abedea2d24c")
	iidIInvokeProvider           = NewGUID("54fcb24b-e18e-47a2-b4d3-eccbe77599a2")

	uiaOnce      bool
	providerVtbl [7]uintptr
	invokeVtbl   [4]uintptr
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
	uiaAvailable = true
	return true
}

func providerPattern(this uintptr, id int32, out uintptr) uintptr {
	*(*uintptr)(native(out)) = 0
	p, _ := comImpl(this).(*uiaProvider)
	if p == nil || id != uiaInvokePatternId || !p.acc.Invokes() {
		return 0
	}
	*(*uintptr)(native(out)) = newComObject(&invokeVtbl[0], &iidIInvokeProvider, p)
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
	s, id := SysAllocString(text), SysAllocString("ryui.announce")
	defer SysFreeString(s)
	defer SysFreeString(id)
	const kindOther, processingImportantAll = 4, 0
	procUiaRaiseNotificationEvent.Call(host, kindOther, processingImportantAll, s, id)
}

// SysAllocString is a BSTR of s, which its taker frees.
func SysAllocString(s string) uintptr {
	r, _, _ := procSysAllocString.Call(Str(s))
	return r
}

func SysFreeString(b uintptr) { procSysFreeString.Call(b) }

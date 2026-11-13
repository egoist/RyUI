//go:build windows

package windows

import (
	"runtime"
	"unsafe"
)

// A common control's accessible name, for one with no text of its own
// to be named by (a drop-down list combo box, whose text is its
// selection): Dynamic Annotation's IAccPropServices sets its MSAA
// name, which UI Automation's proxy for the control reads.

var (
	clsidAccPropServices = NewGUID("b5f8350b-0548-48b1-a6ee-88bd00b4a5e7")
	iidAccPropServices   = NewGUID("6e26e776-04f0-495d-80e4-3330352e3169")
	propidAccName        = NewGUID("608d3df8-8128-4aa7-a428-f55e49267291")
	accProps             uintptr // the IAccPropServices, made once
)

func accPropServices() uintptr {
	if accProps == 0 {
		accProps = CoCreate(&clsidAccPropServices, &iidAccPropServices)
	}
	return accProps
}

// SetAccessibleName names hwnd (its client object) for screen readers;
// empty: its own name again.
func SetAccessibleName(hwnd uintptr, name string) {
	s := accPropServices()
	if s == 0 || hwnd == 0 {
		return
	}
	if name == "" {
		ClearAccessibleName(hwnd)
		return
	}
	str := Str(name)
	// SetHwndPropStr(hwnd, OBJID_CLIENT, CHILDID_SELF, MSAAPROPID, str):
	// the GUID is passed by value, which x64 passes as a pointer to a
	// copy and arm64 in two registers.
	g := propidAccName
	if runtime.GOARCH == "arm64" {
		words := (*[2]uintptr)(unsafe.Pointer(&g))
		ComCall(s, 7, hwnd, 0xFFFFFFFC, 0, words[0], words[1], str)
		return
	}
	ComCall(s, 7, hwnd, 0xFFFFFFFC, 0, uintptr(unsafe.Pointer(&g)), str)
}

// ClearAccessibleName drops the name SetAccessibleName gave hwnd, as
// Dynamic Annotation asks before the window goes.
func ClearAccessibleName(hwnd uintptr) {
	if accProps == 0 || hwnd == 0 {
		return
	}
	g := propidAccName
	// ClearHwndProps(hwnd, OBJID_CLIENT, CHILDID_SELF, props, count)
	ComCall(accProps, 9, hwnd, 0xFFFFFFFC, 0, uintptr(unsafe.Pointer(&g)), 1)
}

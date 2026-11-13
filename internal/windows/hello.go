//go:build windows

package windows

import (
	"errors"
	"unsafe"
)

// Windows Hello: WinRT's UserConsentVerifier (its availability, and its
// prompt over a window through IUserConsentVerifierInterop), and the
// Windows Biometric Framework's sensors, for which kind it is.

var (
	iidUserConsentVerifierStatics = NewGUID("AF4F3F91-564C-4DDC-B8B8-973447627C65")
	iidUserConsentVerifierInterop = NewGUID("39E050C3-4E74-441A-8DC0-B81104DF949C")

	asyncOperation = "{9fc2b0bb-e446-44e2-aa61-9cab8f636af2}"
	availabilityT  = "enum(Windows.Security.Credentials.UI.UserConsentVerifierAvailability;i4)"
	verificationT  = "enum(Windows.Security.Credentials.UI.UserConsentVerificationResult;i4)"

	iidAvailabilityCompleted = ParamIID("pinterface(" + asyncHandler + ";" + availabilityT + ")")
	iidVerificationOperation = ParamIID("pinterface(" + asyncOperation + ";" + verificationT + ")")
	iidVerificationCompleted = ParamIID("pinterface(" + asyncHandler + ";" + verificationT + ")")

	winbio                       = systemDLL("winbio.dll")
	procWinBioEnumBiometricUnits = winbio.NewProc("WinBioEnumBiometricUnits")
	procWinBioFree               = winbio.NewProc("WinBioFree")
)

// HelloAvailability is UserConsentVerifierAvailability.
type HelloAvailability int32

const (
	HelloAvailable HelloAvailability = iota
	HelloDeviceNotPresent
	HelloNotConfigured
	HelloDisabledByPolicy
	HelloDeviceBusy
)

// HelloResult is UserConsentVerificationResult.
type HelloResult int32

const (
	HelloVerified HelloResult = iota
	HelloNoDevice
	HelloNotConfiguredForUser
	HelloDisabled
	HelloBusy
	HelloRetriesExhausted
	HelloCanceled
)

// HelloCheck calls done (on another thread) with Windows Hello's
// availability.
func HelloCheck(done func(HelloAvailability, error)) {
	st, err := ActivationFactory("Windows.Security.Credentials.UI.UserConsentVerifier", iidUserConsentVerifierStatics)
	if err != nil {
		done(HelloDeviceNotPresent, err)
		return
	}
	defer Release(st)
	var op uintptr
	if hr := ComCall(st, 6, uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 { // CheckAvailabilityAsync
		done(HelloDeviceNotPresent, errors.New("Windows Hello: can't ask"))
		return
	}
	h := NewDelegate(iidAvailabilityCompleted, func(op, status uintptr) {
		a := int32(HelloDeviceNotPresent)
		if status == 1 {
			ComCall(op, 8, uintptr(unsafe.Pointer(&a))) // GetResults
		}
		done(HelloAvailability(a), nil)
	})
	ComCall(op, 6, h) // put_Completed
	Release(h)
	Release(op)
}

// HelloVerify shows Windows Hello's prompt over the top-level window
// hwnd, saying message, and calls done (on another thread) with what
// came of it.
func HelloVerify(hwnd uintptr, message string, done func(HelloResult, error)) {
	interop, err := ActivationFactory("Windows.Security.Credentials.UI.UserConsentVerifier", iidUserConsentVerifierInterop)
	if err != nil {
		done(HelloNoDevice, err)
		return
	}
	defer Release(interop)
	msg := NewHString(message)
	defer msg.Delete()
	var op uintptr
	// RequestVerificationForWindowAsync(HWND, HSTRING, REFIID, void**).
	if hr := ComCall(interop, 6, hwnd, uintptr(msg), uintptr(unsafe.Pointer(&iidVerificationOperation)), uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 {
		done(HelloNoDevice, errors.New("Windows Hello: can't ask"))
		return
	}
	h := NewDelegate(iidVerificationCompleted, func(op, status uintptr) {
		r := int32(HelloCanceled)
		switch status {
		case 1: // Completed
			ComCall(op, 8, uintptr(unsafe.Pointer(&r)))
		case 3: // Error
			r = int32(HelloNoDevice)
		}
		done(HelloResult(r), nil)
	})
	ComCall(op, 6, h)
	Release(h)
	Release(op)
}

// BiometricSensor is the kind of biometric sensor the PC has, for
// Windows Hello: 2 face, 1 fingerprint, 0 none.
func BiometricSensor() int {
	if !Has(procWinBioEnumBiometricUnits) {
		return 0
	}
	has := func(t uint32) bool {
		var units uintptr
		var n uintptr
		if hr, _, _ := procWinBioEnumBiometricUnits.Call(uintptr(t), uintptr(unsafe.Pointer(&units)), uintptr(unsafe.Pointer(&n))); Failed(hr) {
			return false
		}
		if units != 0 {
			procWinBioFree.Call(units)
		}
		return n > 0
	}
	const facial, fingerprint = 0x2, 0x8 // WINBIO_TYPE_
	switch {
	case has(facial):
		return 2
	case has(fingerprint):
		return 1
	}
	return 0
}

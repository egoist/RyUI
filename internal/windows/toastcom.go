//go:build windows

package windows

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// The toasts' COM activator, as an unpackaged app has one (Microsoft's
// "Send a local toast notification from a C++ WRL app", the Windows App
// SDK's AppNotificationManager): a class of the app's, registered in the
// user's registry (HKCU\Software\Classes\CLSID\{clsid}\LocalServer32,
// the app's executable), named by the app's Start menu shortcut
// (System.AppUserModel.ToastActivatorCLSID) and its AppUserModelId key
// (CustomActivator). A click on a toast, or on one of its buttons, calls
// the class's INotificationActivationCallback.Activate with the toast's
// or the button's arguments and what the user typed: in the running app,
// whose class object is registered (CoRegisterClassObject), or in the
// app COM starts for it (its command line ends with -ToastActivated, then
// -Embedding).

var (
	iidClassFactory          = NewGUID("00000001-0000-0000-C000-000000000046")
	iidActivationCallback    = NewGUID("53E31837-6600-4A81-9395-75CFFE746F94")
	procCoRegisterClassObj   = ole32.NewProc("CoRegisterClassObject")
	procCoRevokeClassObject  = ole32.NewProc("CoRevokeClassObject")
	procRegDeleteTreeW       = advapi32.NewProc("RegDeleteTreeW")
	pkeyToastActivatorCLSID  = uint32(26)
	pkeyAppUserModelIDPID    = uint32(5)
	toastActivatedArg        = "-ToastActivated"
	toastEmbeddingArg        = "-Embedding"
	toastActivatorSetupOnce  sync.Once
	toastFactoryVtbl         [5]uintptr
	toastCallbackVtbl        [4]uintptr
	toastFactoryObj          = &struct{ vtbl *[5]uintptr }{&toastFactoryVtbl}
	toastCallbackObj         = &struct{ vtbl *[4]uintptr }{&toastCallbackVtbl}
	toastActivated           func(ToastActivation)
	toastCookie              uint32
	launchedByToast          bool
	launchedByToastArgsTaken bool
)

// ToastActivation is a click on a toast: the toast's arguments (its
// launch attribute) or the button's, and what the user typed in its
// inputs, by their IDs.
type ToastActivation struct {
	Args   string
	Inputs map[string]string
}

// ToastCLSID is the CLSID of an app's activator: a name-based UUID of
// its AppUserModelID, the same for every build of the app.
func ToastCLSID(aumid string) GUID {
	sum := sha1.Sum([]byte("asuka.toast-activator:" + aumid))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	g := GUID{Data1: binary.BigEndian.Uint32(b[0:]), Data2: binary.BigEndian.Uint16(b[4:]), Data3: binary.BigEndian.Uint16(b[6:])}
	copy(g.Data4[:], b[8:])
	return g
}

// String is the GUID in braces, upper case, as the registry writes CLSIDs.
func (g GUID) String() string { return strings.ToUpper(g.Signature()) }

// TakeToastLaunch reports whether COM started the process for a toast's
// activation, and takes the arguments it added (-ToastActivated,
// -Embedding) out of os.Args, so that the app's own flags don't see
// them. Call it before the app reads its arguments.
func TakeToastLaunch() bool {
	if launchedByToastArgsTaken {
		return launchedByToast
	}
	launchedByToastArgsTaken = true
	out := os.Args[:1]
	for _, a := range os.Args[1:] {
		switch strings.ToLower(a) {
		case strings.ToLower(toastActivatedArg), strings.ToLower(toastEmbeddingArg), "/embedding":
			launchedByToast = true
		default:
			out = append(out, a)
		}
	}
	os.Args = out
	return launchedByToast
}

// regSetString sets a REG_SZ value of the user's key path ("" names its
// default value), making the key.
func regSetString(path, name, v string) error {
	var k uintptr
	if r, _, _ := procRegCreateKeyEx.Call(hkeyCurrentUser, Str(path), 0, 0, 0, 0x20006, 0, uintptr(unsafe.Pointer(&k)), 0); r != 0 { // KEY_WRITE
		return fmt.Errorf("the registry's %s: %d", path, r)
	}
	defer procRegCloseKey.Call(k)
	u := UTF16(v)
	n := (len(UTF16Units(v)) + 1) * 2
	if r, _, _ := procRegSetValueEx.Call(k, Str(name), 0, 1, uintptr(unsafe.Pointer(u)), uintptr(n)); r != 0 { // REG_SZ
		return fmt.Errorf("the registry's %s: %d", path, r)
	}
	return nil
}

// regUserString reads a string value of the user's key path.
func regUserString(path, name string) string {
	var key uintptr
	if r, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, Str(path), 0, 0x0001, uintptr(unsafe.Pointer(&key))); r != 0 { // KEY_QUERY_VALUE
		return ""
	}
	defer procRegCloseKey.Call(key)
	var typ, size uint32
	if r, _, _ := procRegQueryValueExW.Call(key, Str(name), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size))); r != 0 || size == 0 {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if r, _, _ := procRegQueryValueExW.Call(key, Str(name), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func activatorKey(clsid GUID) string { return `Software\Classes\CLSID\` + clsid.String() }

// RegisterToastActivator registers the app's activator class clsid: its
// server, exe, which COM starts with -ToastActivated when the app isn't
// running; and names it the activator of aumid's toasts.
func RegisterToastActivator(aumid string, clsid GUID, exe string) error {
	key := activatorKey(clsid)
	cmd := `"` + exe + `" ` + toastActivatedArg
	if regUserString(key+`\LocalServer32`, "") != cmd {
		if err := regSetString(key, "", "Asuka toast activator"); err != nil {
			return err
		}
		if err := regSetString(key+`\LocalServer32`, "", cmd); err != nil {
			return err
		}
	}
	return regSetString(`Software\Classes\AppUserModelId\`+aumid, "CustomActivator", clsid.String())
}

// ToastActivatorRegistered reports whether clsid is registered (the app
// has shown toasts before), so that its class object should be ready for
// clicks on them.
func ToastActivatorRegistered(clsid GUID) bool {
	return regUserString(activatorKey(clsid)+`\LocalServer32`, "") != ""
}

// UnregisterToastActivator takes clsid's registration away (tests).
func UnregisterToastActivator(clsid GUID) {
	procRegDeleteTreeW.Call(hkeyCurrentUser, Str(activatorKey(clsid)))
}

// StartToastActivator registers the activator's class object, from the
// main thread (an STA whose message loop runs Activate): each click on
// one of the app's toasts goes to f, on that thread.
func StartToastActivator(clsid GUID, f func(ToastActivation)) error {
	toastActivated = f
	if toastCookie != 0 {
		return nil
	}
	toastActivatorSetupOnce.Do(setupToastActivator)
	var cookie uint32
	hr, _, _ := procCoRegisterClassObj.Call(uintptr(unsafe.Pointer(&clsid)), uintptr(unsafe.Pointer(toastFactoryObj)),
		4, 1, uintptr(unsafe.Pointer(&cookie))) // CLSCTX_LOCAL_SERVER, REGCLS_MULTIPLEUSE
	if Failed(hr) {
		return fmt.Errorf("the toasts' activator: %#x", uint32(hr))
	}
	toastCookie = cookie
	return nil
}

// StopToastActivator revokes the class object.
func StopToastActivator() {
	if toastCookie != 0 {
		procCoRevokeClassObject.Call(uintptr(toastCookie))
		toastCookie = 0
	}
}

// ActivateToast calls the activator of clsid as the shell does, with a
// toast's arguments and inputs (tests: through COM, as a click would).
func ActivateToast(clsid GUID, aumid, args string, inputs map[string]string) error {
	var cb uintptr
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsid)), 0, 4, uintptr(unsafe.Pointer(&iidActivationCallback)), uintptr(unsafe.Pointer(&cb))) // CLSCTX_LOCAL_SERVER
	if Failed(hr) || cb == 0 {
		return fmt.Errorf("the toasts' activator: %#x", uint32(hr))
	}
	defer Release(cb)
	type input struct{ Key, Value *uint16 }
	var data []input
	for k, v := range inputs {
		data = append(data, input{UTF16(k), UTF16(v)})
	}
	var p uintptr
	if len(data) > 0 {
		p = uintptr(unsafe.Pointer(&data[0]))
	}
	if hr := ComCall(cb, 3, Str(aumid), Str(args), p, uintptr(len(data))); Failed(hr) {
		return fmt.Errorf("activating: %#x", uint32(hr))
	}
	runtime.KeepAlive(data)
	return nil
}

func setupToastActivator() {
	qi := func(iid GUID) uintptr {
		return syscall.NewCallback(func(this, riid, out uintptr) uintptr {
			g := *(*GUID)(native(riid))
			if g != iidIUnknown && g != iid && g != iidIAgile {
				*(*uintptr)(native(out)) = 0
				return 0x80004002 // E_NOINTERFACE
			}
			*(*uintptr)(native(out)) = this
			return 0
		})
	}
	// Both objects live as long as the process: their counts are fixed.
	addRef := syscall.NewCallback(func(this uintptr) uintptr { return 2 })
	release := syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	toastFactoryVtbl = [5]uintptr{
		qi(iidClassFactory), addRef, release,
		syscall.NewCallback(func(this, outer, riid, out uintptr) uintptr { // CreateInstance
			*(*uintptr)(native(out)) = 0
			if outer != 0 {
				return 0x80040110 // CLASS_E_NOAGGREGATION
			}
			g := *(*GUID)(native(riid))
			if g != iidIUnknown && g != iidActivationCallback && g != iidIAgile {
				return 0x80004002
			}
			*(*uintptr)(native(out)) = uintptr(unsafe.Pointer(toastCallbackObj))
			return 0
		}),
		syscall.NewCallback(func(this, lock uintptr) uintptr { return 0 }), // LockServer
	}
	toastCallbackVtbl = [4]uintptr{
		qi(iidActivationCallback), addRef, release,
		syscall.NewCallback(func(this, aumid, args, data, count uintptr) uintptr { // Activate
			a := ToastActivation{Args: GoStr(args), Inputs: map[string]string{}}
			type input struct{ Key, Value uintptr }
			if data != 0 && count > 0 && count < 64 {
				for _, in := range unsafe.Slice((*input)(native(data)), int(count)) {
					a.Inputs[GoStr(in.Key)] = GoStr(in.Value)
				}
			}
			if f := toastActivated; f != nil {
				f(a)
			}
			return 0
		}),
	}
}

// errNoHistory: this Windows keeps no history of toasts for the app.
var errNoHistory = errors.New("toasts: no history")

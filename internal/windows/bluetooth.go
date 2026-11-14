//go:build windows

package windows

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Bluetooth Low Energy through WinRT (asuka/sdk/bluetooth's host, in
// host/windows): the adapter and its radio (Windows.Devices.Radios), the
// advertisement watcher and publisher, LE devices, their GATT services
// and a GATT server of the app's own
// (Windows.Devices.Bluetooth.GenericAttributeProfile), the system's
// device picker (Windows.Devices.Enumeration). Everything is
// asynchronous: callbacks come on WinRT's threads, never the caller's,
// and the host takes them to its main thread. An unpackaged desktop app
// needs no capability for any of it.
//
// The IIDs and the vtables' slots are Windows SDK metadata's, as
// microsoft/windows-rs generates them (crates/libs/windows/src/Windows/
// Devices/{Bluetooth,Bluetooth/Advertisement,
// Bluetooth/GenericAttributeProfile,Radios,Enumeration}/mod.rs,
// Storage/Streams/mod.rs and Foundation/mod.rs; Win32/shobjidl_core/mod.rs
// for IInitializeWithWindow; crates/libs/future and collections for
// IAsyncInfo, IAsyncOperation, IVector and IVectorView): an interface's
// slots are its _Vtbl struct's fields after IInspectable's six, in order.
// A parameterized interface's IID is computed from its signature
// (ParamIID), the runtime classes' default interfaces as their
// RuntimeType SIGNATUREs name them.

var (
	// Windows.Devices.Radios (Radios/mod.rs).
	iidRadio        = NewGUID("252118DF-B33E-416A-875F-1CF38AE2D83E")
	iidRadioStatics = NewGUID("5FB6A12E-67CB-46AE-AAE9-65919F86EFF4")

	// Windows.Devices.Bluetooth (Bluetooth/mod.rs).
	iidBTAdapter           = NewGUID("7974F04C-5F7A-4A34-9225-A855F84B1A8B")
	iidBTAdapterStatics    = NewGUID("8B02FB6A-AC4C-4741-8661-8EAB7D17EA9F")
	iidBTLEDevice          = NewGUID("B5EE2F7B-4AD8-4642-AC48-80A0B500E887")
	iidBTLEDevice3         = NewGUID("AEE9E493-44AC-40DC-AF33-B2C13C01CA46")
	iidBTLEDevice4         = NewGUID("2B605031-2248-4B2F-ACF0-7CEE36FC5870")
	iidBTLEDevice6         = NewGUID("CA7190EF-0CAE-573C-A1CA-E1FC5BFC39E2")
	iidBTLEDeviceStatics   = NewGUID("C8CF1A19-F0B6-4BF0-8689-41303DE2D9F4")
	iidBTLEDeviceStatics2  = NewGUID("5F12C06B-3BAC-43E8-AD16-563271BD41C2")
	iidBTDeviceID          = NewGUID("C17949AF-57C1-4642-BCCE-E6C06B20AE76")
	iidBTPreferredParamsSt = NewGUID("0E3E8EDC-2751-55AA-A838-8FAEEE818D72")

	// Windows.Devices.Bluetooth.Advertisement (Bluetooth/Advertisement/mod.rs).
	iidBTAdvertisement        = NewGUID("066FB2B7-33D1-4E7D-8367-CF81D0F79653")
	iidBTPublisher            = NewGUID("CDE820F9-D9FA-43D6-A264-DDD8B7DA8B78")
	iidBTPublisher2           = NewGUID("FBDB545E-56F1-510F-A434-217FBD9E7BD2")
	iidBTPublisherFactory     = NewGUID("5C5F065E-B863-4981-A1AF-1C544D8B0C0D")
	iidBTPublisherStatusArgs  = NewGUID("09C2BD9F-2DFF-4B23-86EE-0D14FB94AEAE")
	iidBTReceivedArgs         = NewGUID("27987DDF-E596-41BE-8D43-9E6731D4A913")
	iidBTReceivedArgs2        = NewGUID("12D9C87B-0399-5F0E-A348-53B02B6B162E")
	iidBTWatcher              = NewGUID("A6AC336F-F3D3-4297-8D6C-C81EA6623F40")
	iidBTWatcher2             = NewGUID("01BF26BC-B164-5805-90A3-E8A7997FF225")
	iidBTWatcherStoppedArgs   = NewGUID("DD40F84D-E7B9-43E3-9C04-0685D085FD8C")
	iidBTManufacturerData     = NewGUID("912DBA18-6963-4533-B061-4694DAFB34E5")
	iidBTManufacturerDataFact = NewGUID("C09B39F8-319A-441E-8DE5-66A81E877A6C")

	// Windows.Devices.Bluetooth.GenericAttributeProfile
	// (Bluetooth/GenericAttributeProfile/mod.rs).
	iidGattCharacteristic        = NewGUID("59CB50C1-5934-4F68-A198-EB864FA44E6B")
	iidGattCharacteristic3       = NewGUID("3F3C663E-93D4-406B-B817-DB81F8ED53B3")
	iidGattCharacteristicsResult = NewGUID("1194945C-B257-4F3E-9DB7-F68BC9A9AEF2")
	iidGattClientNotifyResult    = NewGUID("506D5599-0112-419A-8E3B-AE21AFABD2C2")
	iidGattDescriptor2           = NewGUID("8F563D39-D630-406C-BA11-10CDD16B0E5E")
	iidGattDescriptorsResult     = NewGUID("9BC091F3-95E7-4489-8D25-FF81955A57B9")
	iidGattDeviceService         = NewGUID("AC7B7C05-B33C-47CF-990F-6B8F5577DF71")
	iidGattDeviceService3        = NewGUID("B293A950-0C53-437C-A9B3-5C3210C6E569")
	iidGattDeviceServicesResult  = NewGUID("171DD3EE-016D-419D-838A-576CF475A3D8")
	iidGattLocalChar             = NewGUID("AEDE376D-5412-4D74-92A8-8DEB8526829C")
	iidGattLocalCharParams       = NewGUID("FAF73DB4-4CFF-44C7-8445-040E6EAD0063")
	iidGattLocalCharResult       = NewGUID("7975DE9B-0170-4397-9666-92F863F12EE6")
	iidGattLocalDescParams       = NewGUID("5FDEDE6A-F3C1-4B66-8C4B-E3D2293B40E9")
	iidGattLocalDescResult       = NewGUID("375791BE-321F-4366-BFC1-3BC6B82C79F8")
	iidGattLocalService          = NewGUID("F513E258-F7F7-4902-B803-57FCC7D6FE83")
	iidGattPresentationFormatSt2 = NewGUID("A9C21713-B82F-435E-B634-21FD85A43C07")
	iidGattReadRequest           = NewGUID("F1DD6535-6ACD-42A6-A4BB-D789DAE0043E")
	iidGattReadRequestedArgs     = NewGUID("93497243-F39C-484B-8AB6-996BA486CFA3")
	iidGattReadResult            = NewGUID("63A66F08-1AEA-4C4C-A50F-97BAE474B348")
	iidGattReadResult2           = NewGUID("A10F50A0-FB43-48AF-BAAA-638A5C6329FE")
	iidGattServiceProvider       = NewGUID("7822B3CD-2889-4F86-A051-3F0AED1C2760")
	iidGattServiceProvider2      = NewGUID("9EF531A9-CF12-59A3-A81C-362F4AABAACF")
	iidGattProviderStatusArgs    = NewGUID("59A5AA65-FA21-4FFC-B155-04D928012686")
	iidGattAdvertisingParams     = NewGUID("E2CE31AB-6315-4C22-9BD7-781DBC3D8D82")
	iidGattAdvertisingParams2    = NewGUID("FF68468D-CA92-4434-9743-0E90988AD879")
	iidGattServiceProviderResult = NewGUID("764696D8-C53E-428C-8A48-67AFE02C3AE6")
	iidGattServiceProviderSt     = NewGUID("31794063-5256-4054-A4F4-7BBE7755A57E")
	iidGattSession               = NewGUID("D23B5143-E04E-4C24-999C-9C256F9856B1")
	iidGattSessionStatics        = NewGUID("2E65B95C-539F-4DB7-82A8-73BDBBF73EBF")
	iidGattValueChangedArgs      = NewGUID("D21BDB54-06E3-4ED8-A263-ACFAC8BA7313")
	iidGattWriteRequest          = NewGUID("AEB6A9ED-DE2F-4FC2-A9A8-94EA7844F13D")
	iidGattWriteRequestedArgs    = NewGUID("2DEC8BBE-A73A-471A-94D5-037DEADD0806")
	iidGattWriteResult           = NewGUID("4991DDB1-CB2B-44F7-99FC-D29A2871DC9B")

	// Windows.Devices.Enumeration (Enumeration/mod.rs).
	iidDeviceInformation        = NewGUID("ABA0FB95-4398-489D-8E44-E6130927011F")
	iidDeviceInformationStatics = NewGUID("C17F100E-3A46-4A78-8013-769DC9B97390")
	iidDevicePicker             = NewGUID("84997AA2-034A-4440-8813-7D0BD479BF5A")

	// Windows.Foundation (Foundation/mod.rs; crates/libs/future). Win32's
	// IInitializeWithWindow is store.go's iidInitializeWithWindow.
	iidClosable  = NewGUID("30D5A829-7FA4-4026-83BB-D75BAE4EA99E")
	iidDeferral  = NewGUID("D6269732-3B7F-46A7-B40B-4FDCA2A2C693")
	iidAsyncInfo = NewGUID("00000036-0000-0000-C000-000000000046")
)

// The parameterized interfaces' signatures (ParamIID).
const (
	typedHandler = "{9de1c534-6ae1-11e0-84e1-18a905bcc53f}" // TypedEventHandler`2
	vectorView   = "{bbe1fa4c-b0e3-4583-baef-1f1b2e483e56}" // IVectorView`1 (crates/libs/collections)
	inspectable  = "cinterface(IInspectable)"               // Object
	nsBT         = "Windows.Devices.Bluetooth."
	nsAdv        = "Windows.Devices.Bluetooth.Advertisement."
	nsGatt       = "Windows.Devices.Bluetooth.GenericAttributeProfile."
)

func btCompleted(result string) GUID {
	return ParamIID("pinterface(" + asyncHandler + ";" + result + ")")
}

func btHandler(sender, args string) GUID {
	return ParamIID("pinterface(" + typedHandler + ";" + sender + ";" + args + ")")
}

func btVectorView(of string) string { return "pinterface(" + vectorView + ";" + of + ")" }

var (
	sigRadio            = RuntimeClassSig("Windows.Devices.Radios.Radio", iidRadio)
	sigAdapter          = RuntimeClassSig(nsBT+"BluetoothAdapter", iidBTAdapter)
	sigLEDevice         = RuntimeClassSig(nsBT+"BluetoothLEDevice", iidBTLEDevice)
	sigWatcher          = RuntimeClassSig(nsAdv+"BluetoothLEAdvertisementWatcher", iidBTWatcher)
	sigPublisher        = RuntimeClassSig(nsAdv+"BluetoothLEAdvertisementPublisher", iidBTPublisher)
	sigGattSession      = RuntimeClassSig(nsGatt+"GattSession", iidGattSession)
	sigGattChar         = RuntimeClassSig(nsGatt+"GattCharacteristic", iidGattCharacteristic)
	sigGattProvider     = RuntimeClassSig(nsGatt+"GattServiceProvider", iidGattServiceProvider)
	sigGattLocalChar    = RuntimeClassSig(nsGatt+"GattLocalCharacteristic", iidGattLocalChar)
	sigDeviceInfo       = RuntimeClassSig("Windows.Devices.Enumeration.DeviceInformation", iidDeviceInformation)
	sigGattNotifyResult = RuntimeClassSig(nsGatt+"GattClientNotificationResult", iidGattClientNotifyResult)

	iidRadiosCompleted         = btCompleted(btVectorView(sigRadio))
	iidRadioAccessCompleted    = btCompleted("enum(Windows.Devices.Radios.RadioAccessStatus;i4)")
	iidAdapterCompleted        = btCompleted(sigAdapter)
	iidRadioCompleted          = btCompleted(sigRadio)
	iidLEDeviceCompleted       = btCompleted(sigLEDevice)
	iidGattSessionCompleted    = btCompleted(sigGattSession)
	iidServicesResultCompleted = btCompleted(RuntimeClassSig(nsGatt+"GattDeviceServicesResult", iidGattDeviceServicesResult))
	iidCharsResultCompleted    = btCompleted(RuntimeClassSig(nsGatt+"GattCharacteristicsResult", iidGattCharacteristicsResult))
	iidDescsResultCompleted    = btCompleted(RuntimeClassSig(nsGatt+"GattDescriptorsResult", iidGattDescriptorsResult))
	iidReadResultCompleted     = btCompleted(RuntimeClassSig(nsGatt+"GattReadResult", iidGattReadResult))
	iidWriteResultCompleted    = btCompleted(RuntimeClassSig(nsGatt+"GattWriteResult", iidGattWriteResult))
	iidProviderResultCompleted = btCompleted(RuntimeClassSig(nsGatt+"GattServiceProviderResult", iidGattServiceProviderResult))
	iidLocalCharCompleted      = btCompleted(RuntimeClassSig(nsGatt+"GattLocalCharacteristicResult", iidGattLocalCharResult))
	iidLocalDescCompleted      = btCompleted(RuntimeClassSig(nsGatt+"GattLocalDescriptorResult", iidGattLocalDescResult))
	iidReadRequestCompleted    = btCompleted(RuntimeClassSig(nsGatt+"GattReadRequest", iidGattReadRequest))
	iidWriteRequestCompleted   = btCompleted(RuntimeClassSig(nsGatt+"GattWriteRequest", iidGattWriteRequest))
	iidNotifyAllCompleted      = btCompleted(btVectorView(sigGattNotifyResult))
	iidNotifyOneCompleted      = btCompleted(sigGattNotifyResult)
	iidDeviceInfoCompleted     = btCompleted(sigDeviceInfo)
	// DeviceInformationCollection's default interface is
	// IVectorView<DeviceInformation>.
	iidDeviceInfosCompleted = btCompleted("rc(Windows.Devices.Enumeration.DeviceInformationCollection;" + btVectorView(sigDeviceInfo) + ")")

	iidRadioStateHandler       = btHandler(sigRadio, inspectable)
	iidReceivedHandler         = btHandler(sigWatcher, RuntimeClassSig(nsAdv+"BluetoothLEAdvertisementReceivedEventArgs", iidBTReceivedArgs))
	iidWatcherStoppedHandler   = btHandler(sigWatcher, RuntimeClassSig(nsAdv+"BluetoothLEAdvertisementWatcherStoppedEventArgs", iidBTWatcherStoppedArgs))
	iidLEDeviceHandler         = btHandler(sigLEDevice, inspectable) // ConnectionStatusChanged, GattServicesChanged
	iidValueChangedHandler     = btHandler(sigGattChar, RuntimeClassSig(nsGatt+"GattValueChangedEventArgs", iidGattValueChangedArgs))
	iidPublisherStatusHandler  = btHandler(sigPublisher, RuntimeClassSig(nsAdv+"BluetoothLEAdvertisementPublisherStatusChangedEventArgs", iidBTPublisherStatusArgs))
	iidProviderStatusHandler   = btHandler(sigGattProvider, RuntimeClassSig(nsGatt+"GattServiceProviderAdvertisementStatusChangedEventArgs", iidGattProviderStatusArgs))
	iidReadRequestedHandler    = btHandler(sigGattLocalChar, RuntimeClassSig(nsGatt+"GattReadRequestedEventArgs", iidGattReadRequestedArgs))
	iidWriteRequestedHandler   = btHandler(sigGattLocalChar, RuntimeClassSig(nsGatt+"GattWriteRequestedEventArgs", iidGattWriteRequestedArgs))
	iidSubscribersChangedHndlr = btHandler(sigGattLocalChar, inspectable)
)

// Enumerations' values, as the metadata has them.
const (
	RadioUnknown  = 0
	RadioOn       = 1
	RadioOff      = 2
	RadioDisabled = 3

	RadioAccessUnspecified    = 0
	RadioAccessAllowed        = 1
	RadioAccessDeniedByUser   = 2
	RadioAccessDeniedBySystem = 3

	// BluetoothError.
	BTSuccess               = 0
	BTRadioNotAvailable     = 1
	BTResourceInUse         = 2
	BTDeviceNotConnected    = 3
	BTOtherError            = 4
	BTDisabledByPolicy      = 5
	BTNotSupported          = 6
	BTDisabledByUser        = 7
	BTConsentRequired       = 8
	BTTransportNotSupported = 9

	// GattCommunicationStatus.
	GattSuccess       = 0
	GattUnreachable   = 1
	GattProtocolError = 2
	GattAccessDenied  = 3

	// GattClientCharacteristicConfigurationDescriptorValue.
	CCCDNone     = 0
	CCCDNotify   = 1
	CCCDIndicate = 2

	// GattProtectionLevel.
	GattPlain              = 0
	GattEncryptionRequired = 2

	// BluetoothLEAdvertisementType.
	AdvConnectableUndirected    = 0
	AdvConnectableDirected      = 1
	AdvScannableUndirected      = 2
	AdvNonConnectableUndirected = 3
	AdvScanResponse             = 4
	AdvExtended                 = 5

	// BluetoothLEAdvertisementPublisherStatus.
	PublisherCreated  = 0
	PublisherWaiting  = 1
	PublisherStarted  = 2
	PublisherStopping = 3
	PublisherStopped  = 4
	PublisherAborted  = 5

	// GattServiceProviderAdvertisementStatus.
	ProviderCreated            = 0
	ProviderStopped            = 1
	ProviderStarted            = 2
	ProviderAborted            = 3
	ProviderStartedWithoutData = 4

	// BluetoothLEPreferredConnectionParameters' statics, by slot.
	BTPriorityBalanced   = 6
	BTPriorityThroughput = 7
	BTPriorityPower      = 8
)

// Errors of the asynchronous operations.
var (
	ErrBTCanceled = errors.New("canceled")
	errBTNoBuffer = errors.New("no buffer")
)

// btAwait sets op's Completed handler (of iid) and lets op go: done gets
// GetResults' object (a reference done releases; 0 for null), or why
// there's none.
func btAwait(op uintptr, iid GUID, done func(r uintptr, err error)) {
	h := NewDelegate(iid, func(op, status uintptr) {
		switch uint32(status) {
		case 1: // Completed
			var r uintptr
			if hr := ComCall(op, 8, uintptr(unsafe.Pointer(&r))); Failed(hr) { // GetResults
				done(0, fmt.Errorf("%#x", uint32(hr)))
				return
			}
			done(r, nil)
		case 2: // Canceled
			done(0, ErrBTCanceled)
		default:
			done(0, asyncError(op))
		}
	})
	if hr := ComCall(op, 6, h); Failed(hr) { // put_Completed
		Release(h)
		Release(op)
		done(0, fmt.Errorf("%#x", uint32(hr)))
		return
	}
	Release(h)
	Release(op)
}

// btAwaitEnum is btAwait for an operation whose result is an enum (i4).
func btAwaitEnum(op uintptr, iid GUID, done func(v int32, err error)) {
	h := NewDelegate(iid, func(op, status uintptr) {
		var v int32
		switch uint32(status) {
		case 1:
			ComCall(op, 8, uintptr(unsafe.Pointer(&v)))
			done(v, nil)
		case 2:
			done(0, ErrBTCanceled)
		default:
			done(0, asyncError(op))
		}
	})
	if hr := ComCall(op, 6, h); Failed(hr) {
		Release(h)
		Release(op)
		done(0, fmt.Errorf("%#x", uint32(hr)))
		return
	}
	Release(h)
	Release(op)
}

// AsyncHRESULT is an operation's error code, as IAsyncInfo.ErrorCode
// has it.
type AsyncHRESULT uint32

func (e AsyncHRESULT) Error() string { return fmt.Sprintf("%#x", uint32(e)) }

func asyncError(op uintptr) error {
	info := QueryInterface(op, &iidAsyncInfo)
	if info == 0 {
		return errors.New("failed")
	}
	defer Release(info)
	var hr uint32
	ComCall(info, 8, uintptr(unsafe.Pointer(&hr))) // ErrorCode
	return AsyncHRESULT(hr)
}

// btCall calls a method that makes an operation (its last argument the
// out pointer, after args: COM objects, HSTRINGs, numbers, never Go
// memory), and awaits it.
func btCall(obj uintptr, slot int, iid GUID, done func(uintptr, error), args ...uintptr) {
	var op uintptr
	var hr uintptr
	// The out pointer is converted in ComCall's own arguments, so that op
	// is on the heap (go:uintptrescapes).
	switch len(args) {
	case 0:
		hr = ComCall(obj, slot, uintptr(unsafe.Pointer(&op)))
	case 1:
		hr = ComCall(obj, slot, args[0], uintptr(unsafe.Pointer(&op)))
	case 2:
		hr = ComCall(obj, slot, args[0], args[1], uintptr(unsafe.Pointer(&op)))
	default:
		panic("btCall: too many arguments")
	}
	if Failed(hr) || op == 0 {
		done(0, AsyncHRESULT(hr))
		return
	}
	btAwait(op, iid, done)
}

// btEvent adds a handler (of iid) to obj's event whose add method is
// slot (remove is slot+1); it returns the token, 0 if it couldn't.
func btEvent(obj uintptr, slot int, iid GUID, fn func(sender, args uintptr)) int64 {
	h := NewDelegate(iid, fn)
	var token int64
	if Failed(ComCall(obj, slot, h, uintptr(unsafe.Pointer(&token)))) {
		token = 0
	}
	Release(h)
	return token
}

func btRemove(obj uintptr, slot int, token *int64) {
	if obj != 0 && *token != 0 {
		ComCall(obj, slot+1, uintptr(*token))
		*token = 0
	}
}

// btClose closes an object that's IClosable (a device, a session, a
// service: what holds the connection), and releases it.
func btClose(obj uintptr) {
	if obj == 0 {
		return
	}
	if c := QueryInterface(obj, &iidClosable); c != 0 {
		ComCall(c, 6) // Close
		Release(c)
	}
	Release(obj)
}

func btQI(obj uintptr, iid GUID) uintptr { return QueryInterface(obj, &iid) }

func btStatics(class string, iid GUID) (uintptr, error) { return ActivationFactory(class, iid) }

func btNew(class string, iid GUID) (uintptr, error) {
	o, err := ActivateInstance(class)
	if err != nil {
		return 0, err
	}
	defer Release(o)
	x := QueryInterface(o, &iid)
	if x == 0 {
		return 0, errors.New(class + ": no interface")
	}
	return x, nil
}

func btU8(obj uintptr, slot int) uint8 {
	var v uint8
	ComCall(obj, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

func btU16(obj uintptr, slot int) uint16 {
	var v uint16
	ComCall(obj, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

func btI32(obj uintptr, slot int) int32 {
	var v int32
	ComCall(obj, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

func btU32(obj uintptr, slot int) uint32 {
	var v uint32
	ComCall(obj, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

func btBool(obj uintptr, slot int) bool { return btU8(obj, slot) != 0 }

func btObj(obj uintptr, slot int) uintptr {
	var v uintptr
	if Failed(ComCall(obj, slot, uintptr(unsafe.Pointer(&v)))) {
		return 0
	}
	return v
}

func btGUID(obj uintptr, slot int) GUID {
	var g GUID
	ComCall(obj, slot, uintptr(unsafe.Pointer(&g)))
	return g
}

// btByteRef reads an IReference<UInt8> (a GATT result's ProtocolError):
// -1 for null.
func btByteRef(obj uintptr, slot int) int {
	r := btObj(obj, slot)
	if r == 0 {
		return -1
	}
	defer Release(r)
	return int(btU8(r, 6)) // IReference.get_Value
}

// btBytes reads an IBuffer.
func btBytes(buf uintptr) []byte {
	if buf == 0 {
		return nil
	}
	n := btU32(buf, 7) // IBuffer.get_Length
	out := make([]byte, n)
	if n == 0 {
		return out
	}
	acc := QueryInterface(buf, &iidBufferByteAccess)
	if acc == 0 {
		return nil
	}
	defer Release(acc)
	var p uintptr
	ComCall(acc, 3, uintptr(unsafe.Pointer(&p))) // IBufferByteAccess.Buffer
	if p == 0 {
		return nil
	}
	copy(out, unsafe.Slice((*byte)(native(p)), n))
	return out
}

// btBuffer makes an IBuffer of b (a reference the caller releases).
func btBuffer(b []byte) (uintptr, error) {
	bf, err := ActivationFactory("Windows.Storage.Streams.Buffer", iidBufferFactory)
	if err != nil {
		return 0, err
	}
	defer Release(bf)
	var buf uintptr
	if Failed(ComCall(bf, 6, uintptr(max(len(b), 1)), uintptr(unsafe.Pointer(&buf)))) || buf == 0 { // Create
		return 0, errBTNoBuffer
	}
	if len(b) > 0 {
		acc := QueryInterface(buf, &iidBufferByteAccess)
		if acc == 0 {
			Release(buf)
			return 0, errBTNoBuffer
		}
		var p uintptr
		ComCall(acc, 3, uintptr(unsafe.Pointer(&p)))
		Release(acc)
		copy(unsafe.Slice((*byte)(native(p)), len(b)), b)
	}
	ComCall(buf, 8, uintptr(len(b))) // put_Length
	return buf, nil
}

// BTGUID is a UUID ([16]byte, as written: 0000180d-...) as a GUID, and
// back.
func BTGUID(u [16]byte) GUID {
	g := GUID{Data1: uint32(u[0])<<24 | uint32(u[1])<<16 | uint32(u[2])<<8 | uint32(u[3]),
		Data2: uint16(u[4])<<8 | uint16(u[5]), Data3: uint16(u[6])<<8 | uint16(u[7])}
	copy(g.Data4[:], u[8:])
	return g
}

func BTUUID(g GUID) [16]byte {
	var u [16]byte
	u[0], u[1], u[2], u[3] = byte(g.Data1>>24), byte(g.Data1>>16), byte(g.Data1>>8), byte(g.Data1)
	u[4], u[5], u[6], u[7] = byte(g.Data2>>8), byte(g.Data2), byte(g.Data3>>8), byte(g.Data3)
	copy(u[8:], g.Data4[:])
	return u
}

// --- The adapter and its radio.

// BTAdapter is the default Bluetooth adapter.
type BTAdapter struct {
	LowEnergy, Peripheral bool
	adapter, radio        uintptr
	token                 int64
}

// BTDefaultAdapter finds the default adapter and its radio: nil without
// one (no Bluetooth, or no WinRT).
func BTDefaultAdapter(done func(*BTAdapter)) {
	st, err := btStatics(nsBT+"BluetoothAdapter", iidBTAdapterStatics)
	if err != nil {
		done(nil)
		return
	}
	defer Release(st)
	btCall(st, 8, iidAdapterCompleted, func(a uintptr, err error) { // GetDefaultAsync
		if a == 0 {
			done(nil)
			return
		}
		ad := &BTAdapter{adapter: a, LowEnergy: btBool(a, 9), Peripheral: btBool(a, 10)} // IsLowEnergySupported, IsPeripheralRoleSupported
		btCall(a, 13, iidRadioCompleted, func(r uintptr, _ error) {                      // GetRadioAsync
			ad.radio = r
			done(ad)
		})
	})
}

// State is the radio's (a Radio* constant).
func (a *BTAdapter) State() int32 {
	if a.radio == 0 {
		return RadioUnknown
	}
	return btI32(a.radio, 9) // IRadio.get_State
}

// OnStateChanged calls f as the radio's state changes.
func (a *BTAdapter) OnStateChanged(f func()) {
	if a.radio != 0 && a.token == 0 {
		a.token = btEvent(a.radio, 7, iidRadioStateHandler, func(_, _ uintptr) { f() }) // add_StateChanged
	}
}

// BTRequestAccess asks for the radios' access (Radio.RequestAccessAsync):
// a RadioAccess* constant.
func BTRequestAccess(done func(int32)) {
	st, err := btStatics("Windows.Devices.Radios.Radio", iidRadioStatics)
	if err != nil {
		done(RadioAccessUnspecified)
		return
	}
	defer Release(st)
	var op uintptr
	if Failed(ComCall(st, 9, uintptr(unsafe.Pointer(&op)))) || op == 0 { // RequestAccessAsync
		done(RadioAccessUnspecified)
		return
	}
	btAwaitEnum(op, iidRadioAccessCompleted, func(v int32, _ error) { done(v) })
}

// --- Scanning.

// BTSection is an advertisement's data section: its type and data.
type BTSection struct {
	Type byte
	Data []byte
}

// BTReceived is an advertisement the watcher heard.
type BTReceived struct {
	Address      uint64
	RSSI         int
	Type         int32 // an Adv* constant
	Time         int64 // ms since 1970
	TxPower      *int
	Connectable  *bool // Windows 10 2004 and later
	Scannable    bool
	ScanResponse bool
	LocalName    string
	Sections     []BTSection
}

// BTWatcher is an advertisement watcher, scanning actively (scan
// responses, with their names, come too).
type BTWatcher struct {
	w                 uintptr
	received, stopped int64
}

// NewBTWatcher makes a watcher: received gets what it hears, stopped
// the BluetoothError it stopped with (BTSuccess once Stop has it stop).
func NewBTWatcher(received func(BTReceived), stopped func(int32)) (*BTWatcher, error) {
	w, err := btNew(nsAdv+"BluetoothLEAdvertisementWatcher", iidBTWatcher)
	if err != nil {
		return nil, err
	}
	ComCall(w, 12, 1) // put_ScanningMode: Active
	if w2 := btQI(w, iidBTWatcher2); w2 != 0 {
		ComCall(w2, 7, 1) // put_AllowExtendedAdvertisements
		Release(w2)
	}
	x := &BTWatcher{w: w}
	x.received = btEvent(w, 19, iidReceivedHandler, func(_, args uintptr) { // add_Received
		received(readReceived(args))
	})
	x.stopped = btEvent(w, 21, iidWatcherStoppedHandler, func(_, args uintptr) { // add_Stopped
		stopped(btI32(args, 6)) // Error
	})
	return x, nil
}

func readReceived(args uintptr) BTReceived {
	r := BTReceived{Address: uint64(0), Type: btI32(args, 8)}
	var rssi int16
	ComCall(args, 6, uintptr(unsafe.Pointer(&rssi))) // RawSignalStrengthInDBm
	r.RSSI = int(rssi)
	ComCall(args, 7, uintptr(unsafe.Pointer(&r.Address))) // BluetoothAddress
	var ts int64
	ComCall(args, 9, uintptr(unsafe.Pointer(&ts))) // Timestamp: 100 ns since 1601
	r.Time = ts/10000 - 11644473600000
	r.ScanResponse = r.Type == AdvScanResponse
	r.Scannable = r.Type == AdvConnectableUndirected || r.Type == AdvScannableUndirected
	if a2 := btQI(args, iidBTReceivedArgs2); a2 != 0 {
		if ref := btObj(a2, 7); ref != 0 { // TransmitPowerLevelInDBm: IReference<Int16>
			var p int16
			ComCall(ref, 6, uintptr(unsafe.Pointer(&p)))
			Release(ref)
			n := int(p)
			r.TxPower = &n
		}
		c := btBool(a2, 9) // IsConnectable
		r.Connectable = &c
		r.Scannable = btBool(a2, 10)    // IsScannable
		r.ScanResponse = btBool(a2, 12) // IsScanResponse
		Release(a2)
	}
	if ad := btObj(args, 10); ad != 0 { // Advertisement
		r.LocalName = hstringOf(ad, 8)        // LocalName
		if list := btObj(ad, 12); list != 0 { // DataSections: IVector<BluetoothLEAdvertisementDataSection>
			for _, s := range vectorItems(list) {
				r.Sections = append(r.Sections, BTSection{Type: btU8(s, 6), Data: bufferOf(s, 8)})
				Release(s)
			}
			Release(list)
		}
		Release(ad)
	}
	return r
}

// bufferOf reads the IBuffer a getter gives.
func bufferOf(obj uintptr, slot int) []byte {
	b := btObj(obj, slot)
	if b == 0 {
		return nil
	}
	defer Release(b)
	return btBytes(b)
}

// Start starts scanning.
func (x *BTWatcher) Start() error {
	if hr := ComCall(x.w, 17); Failed(hr) {
		return AsyncHRESULT(hr)
	}
	return nil
}

// Close stops the watcher, without its stopped callback, and lets it go.
func (x *BTWatcher) Close() {
	btRemove(x.w, 19, &x.received)
	btRemove(x.w, 21, &x.stopped)
	ComCall(x.w, 18) // Stop
	Release(x.w)
	x.w = 0
}

// --- Devices.

// BTDevice is a BluetoothLEDevice, and its GATT session once opened.
type BTDevice struct {
	dev, session         uintptr
	status, changed, pdu int64
	priority             uintptr // the preferred connection parameters' request
}

func btLEStatics() (uintptr, error) { return btStatics(nsBT+"BluetoothLEDevice", iidBTLEDeviceStatics) }

func wrapDevice(done func(*BTDevice, error)) func(uintptr, error) {
	return func(d uintptr, err error) {
		if d == 0 {
			done(nil, err)
			return
		}
		done(&BTDevice{dev: d}, nil)
	}
}

// BTDeviceFromAddress is the device of an address: nil if Windows has
// none.
func BTDeviceFromAddress(addr uint64, done func(*BTDevice, error)) {
	st, err := btLEStatics()
	if err != nil {
		done(nil, err)
		return
	}
	defer Release(st)
	btCall(st, 7, iidLEDeviceCompleted, wrapDevice(done), uintptr(addr)) // FromBluetoothAddressAsync
}

// BTDeviceFromID is the device of a device interface ID.
func BTDeviceFromID(id string, done func(*BTDevice, error)) {
	st, err := btLEStatics()
	if err != nil {
		done(nil, err)
		return
	}
	defer Release(st)
	hs := NewHString(id)
	defer hs.Delete()
	btCall(st, 6, iidLEDeviceCompleted, wrapDevice(done), uintptr(hs)) // FromIdAsync
}

// Address, Name and Connected are the device's.
func (d *BTDevice) Address() uint64 {
	var a uint64
	ComCall(d.dev, 10, uintptr(unsafe.Pointer(&a)))
	return a
}

func (d *BTDevice) Name() string    { return hstringOf(d.dev, 7) }
func (d *BTDevice) Connected() bool { return btI32(d.dev, 9) == 1 } // ConnectionStatus: Connected

// BTSession is a device's GATT session, which OpenSession opens.
type BTSession uintptr

// Close closes a session no device took (Attach).
func (s BTSession) Close() { btClose(uintptr(s)) }

// OpenSession opens the device's GATT session, maintaining the
// connection (what holds one on Windows, as long as the session is
// open): done gets it, for Attach on the thread that owns the device.
func (d *BTDevice) OpenSession(done func(BTSession, error)) {
	d4 := btQI(d.dev, iidBTLEDevice4)
	if d4 == 0 {
		done(0, errors.New("not available: this Windows has no GattSession (Windows 10 1803 and later)"))
		return
	}
	id := btObj(d4, 6) // BluetoothDeviceId
	Release(d4)
	st, err := btStatics(nsGatt+"GattSession", iidGattSessionStatics)
	if err != nil || id == 0 {
		Release(id)
		done(0, errors.New("not available: this Windows has no GattSession"))
		return
	}
	defer Release(st)
	btCall(st, 6, iidGattSessionCompleted, func(s uintptr, err error) { // FromDeviceIdAsync
		Release(id)
		if s == 0 {
			if err == nil {
				err = errors.New("no session")
			}
			done(0, err)
			return
		}
		if btBool(s, 7) { // CanMaintainConnection
			ComCall(s, 8, 1) // put_MaintainConnection
		}
		done(BTSession(s), nil)
	}, id)
}

// Attach makes the session the device's: closing the device closes it.
func (d *BTDevice) Attach(s BTSession) {
	btClose(d.session)
	d.session = uintptr(s)
}

// OnConnectionChanged calls f as the device connects and disconnects;
// OnServicesChanged as its services change.
func (d *BTDevice) OnConnectionChanged(f func()) {
	d.status = btEvent(d.dev, 16, iidLEDeviceHandler, func(_, _ uintptr) { f() }) // add_ConnectionStatusChanged
}

func (d *BTDevice) OnServicesChanged(f func()) {
	d.changed = btEvent(d.dev, 14, iidLEDeviceHandler, func(_, _ uintptr) { f() }) // add_GattServicesChanged
}

// MaxPDU is the session's: the ATT MTU (23 before it's negotiated).
func (d *BTDevice) MaxPDU() int {
	if d.session == 0 {
		return 23
	}
	return max(int(btU16(d.session, 10)), 23)
}

// RequestPriority asks for connection parameters (a BTPriority*
// constant): Windows 11 only; false where it can't.
func (d *BTDevice) RequestPriority(which int) bool {
	d6 := btQI(d.dev, iidBTLEDevice6)
	if d6 == 0 {
		return false
	}
	defer Release(d6)
	st, err := btStatics(nsBT+"BluetoothLEPreferredConnectionParameters", iidBTPreferredParamsSt)
	if err != nil {
		return false
	}
	defer Release(st)
	p := btObj(st, which)
	if p == 0 {
		return false
	}
	defer Release(p)
	var req uintptr
	if Failed(ComCall(d6, 8, p, uintptr(unsafe.Pointer(&req)))) || req == 0 { // RequestPreferredConnectionParameters
		return false
	}
	// The parameters hold while the request does: until the next one, or
	// the device closes.
	btClose(d.priority)
	d.priority = req
	return true
}

// Close lets the device go: its session closed, the connection ends
// unless another app holds it.
func (d *BTDevice) Close() {
	btRemove(d.dev, 16, &d.status)
	btRemove(d.dev, 14, &d.changed)
	btClose(d.priority)
	d.priority = 0
	btClose(d.session)
	d.session = 0
	btClose(d.dev)
	d.dev = 0
}

// GattResult is how a GATT operation went: Status a Gatt* constant,
// Protocol the ATT error (-1: none), Err the operation's own failure.
type GattResult struct {
	Status   int32
	Protocol int
	Err      error
}

func gattResult(r uintptr, err error, statusSlot, protoSlot int) GattResult {
	if r == 0 {
		if err == nil {
			err = errors.New("no result")
		}
		return GattResult{Status: -1, Protocol: -1, Err: err}
	}
	res := GattResult{Status: btI32(r, statusSlot), Protocol: -1}
	if protoSlot >= 0 {
		res.Protocol = btByteRef(r, protoSlot)
	}
	return res
}

// BTService, BTCharacteristic and BTDescriptor are a device's GATT
// attributes, as discovered.
type BTService struct {
	obj    uintptr
	UUID   [16]byte
	Handle uint16
}

type BTCharacteristic struct {
	obj    uintptr
	UUID   [16]byte
	Handle uint16
	Props  uint32
	token  int64
}

type BTDescriptor struct {
	obj    uintptr
	UUID   [16]byte
	Handle uint16
}

const btUncached = 1 // BluetoothCacheMode

func cacheMode(uncached bool) uintptr {
	if uncached {
		return btUncached
	}
	return 0
}

// Services discovers the device's primary services (Uncached: from the
// device, which connects to it).
func (d *BTDevice) Services(uncached bool, done func([]*BTService, GattResult)) {
	d3 := btQI(d.dev, iidBTLEDevice3)
	if d3 == 0 {
		done(nil, GattResult{Status: -1, Protocol: -1, Err: errors.New("not available: this Windows can't discover services")})
		return
	}
	defer Release(d3)
	btCall(d3, 9, iidServicesResultCompleted, func(r uintptr, err error) { // GetGattServicesWithCacheModeAsync
		res := gattResult(r, err, 6, 7)
		var out []*BTService
		if r != 0 {
			if list := btObj(r, 8); list != 0 { // Services
				for _, s := range vectorItems(list) {
					out = append(out, &BTService{obj: s, UUID: BTUUID(btGUID(s, 9)), Handle: btU16(s, 10)}) // Uuid, AttributeHandle
				}
				Release(list)
			}
			Release(r)
		}
		done(out, res)
	}, cacheMode(uncached))
}

// Close closes the service (the connection's hold on it).
func (s *BTService) Close() { btClose(s.obj); s.obj = 0 }

// Characteristics discovers the service's.
func (s *BTService) Characteristics(uncached bool, done func([]*BTCharacteristic, GattResult)) {
	s3 := btQI(s.obj, iidGattDeviceService3)
	if s3 == 0 {
		done(nil, GattResult{Status: -1, Protocol: -1, Err: errors.New("no IGattDeviceService3")})
		return
	}
	defer Release(s3)
	btCall(s3, 12, iidCharsResultCompleted, func(r uintptr, err error) { // GetCharacteristicsWithCacheModeAsync
		res := gattResult(r, err, 6, 7)
		var out []*BTCharacteristic
		if r != 0 {
			if list := btObj(r, 8); list != 0 {
				for _, c := range vectorItems(list) {
					out = append(out, &BTCharacteristic{obj: c, UUID: BTUUID(btGUID(c, 11)), Handle: btU16(c, 12), Props: btU32(c, 7)}) // Uuid, AttributeHandle, CharacteristicProperties
				}
				Release(list)
			}
			Release(r)
		}
		done(out, res)
	}, cacheMode(uncached))
}

// Descriptors discovers the characteristic's.
func (c *BTCharacteristic) Descriptors(uncached bool, done func([]*BTDescriptor, GattResult)) {
	c3 := btQI(c.obj, iidGattCharacteristic3)
	if c3 == 0 {
		done(nil, GattResult{Status: -1, Protocol: -1, Err: errors.New("no IGattCharacteristic3")})
		return
	}
	defer Release(c3)
	btCall(c3, 7, iidDescsResultCompleted, func(r uintptr, err error) { // GetDescriptorsWithCacheModeAsync
		res := gattResult(r, err, 6, 7)
		var out []*BTDescriptor
		if r != 0 {
			if list := btObj(r, 8); list != 0 {
				for _, d := range vectorItems(list) {
					out = append(out, &BTDescriptor{obj: d, UUID: BTUUID(btGUID(d, 8)), Handle: btU16(d, 9)}) // Uuid, AttributeHandle
				}
				Release(list)
			}
			Release(r)
		}
		done(out, res)
	}, cacheMode(uncached))
}

func readResult(done func([]byte, GattResult)) func(uintptr, error) {
	return func(r uintptr, err error) {
		res := gattResult(r, err, 6, -1)
		var v []byte
		if r != 0 {
			v = bufferOf(r, 7) // Value
			if r2 := btQI(r, iidGattReadResult2); r2 != 0 {
				res.Protocol = btByteRef(r2, 6) // ProtocolError
				Release(r2)
			}
			Release(r)
		}
		done(v, res)
	}
}

func writeResult(done func(GattResult)) func(uintptr, error) {
	return func(r uintptr, err error) {
		res := gattResult(r, err, 6, 7)
		Release(r)
		done(res)
	}
}

func failedResult(err error) GattResult { return GattResult{Status: -1, Protocol: -1, Err: err} }

// Read reads the characteristic's value from the device.
func (c *BTCharacteristic) Read(done func([]byte, GattResult)) {
	btCall(c.obj, 15, iidReadResultCompleted, readResult(done), btUncached) // ReadValueWithCacheModeAsync
}

// Write writes it, with a response or without.
func (c *BTCharacteristic) Write(v []byte, response bool, done func(GattResult)) {
	c3 := btQI(c.obj, iidGattCharacteristic3)
	buf, err := btBuffer(v)
	if c3 == 0 || err != nil {
		Release(c3)
		done(failedResult(errors.New("can't write")))
		return
	}
	defer Release(c3)
	defer Release(buf)
	opt := uintptr(0) // GattWriteOption: WriteWithResponse
	if !response {
		opt = 1
	}
	btCall(c3, 11, iidWriteResultCompleted, writeResult(done), buf, opt) // WriteValueWithResultAndOptionAsync
}

// WriteCCCD writes its Client Characteristic Configuration (a CCCD*
// constant).
func (c *BTCharacteristic) WriteCCCD(v int32, done func(GattResult)) {
	c3 := btQI(c.obj, iidGattCharacteristic3)
	if c3 == 0 {
		done(failedResult(errors.New("can't subscribe")))
		return
	}
	defer Release(c3)
	btCall(c3, 12, iidWriteResultCompleted, writeResult(done), uintptr(v)) // WriteClientCharacteristicConfigurationDescriptorWithResultAsync
}

// OnValue calls f with each notification's (indication's) value, until
// StopValues.
func (c *BTCharacteristic) OnValue(f func([]byte)) {
	if c.token != 0 {
		return
	}
	c.token = btEvent(c.obj, 20, iidValueChangedHandler, func(_, args uintptr) { // add_ValueChanged
		f(bufferOf(args, 6)) // CharacteristicValue
	})
}

func (c *BTCharacteristic) StopValues() { btRemove(c.obj, 20, &c.token) }

// Release lets the characteristic go.
func (c *BTCharacteristic) Release() {
	c.StopValues()
	Release(c.obj)
	c.obj = 0
}

// Read reads the descriptor's value from the device.
func (d *BTDescriptor) Read(done func([]byte, GattResult)) {
	btCall(d.obj, 11, iidReadResultCompleted, readResult(done), btUncached) // ReadValueWithCacheModeAsync
}

// Write writes it.
func (d *BTDescriptor) Write(v []byte, done func(GattResult)) {
	d2 := btQI(d.obj, iidGattDescriptor2)
	buf, err := btBuffer(v)
	if d2 == 0 || err != nil {
		Release(d2)
		done(failedResult(errors.New("can't write")))
		return
	}
	defer Release(d2)
	defer Release(buf)
	btCall(d2, 6, iidWriteResultCompleted, writeResult(done), buf) // WriteValueWithResultAsync
}

func (d *BTDescriptor) Release() { Release(d.obj); d.obj = 0 }

// BTSelectorConnected is the AQS of the LE devices the system is
// connected to; BTSelectorPaired of those paired (or not).
func BTSelectorConnected() string { return btSelector(7, 1) }

func BTSelectorPaired(paired bool) string {
	v := uintptr(0)
	if paired {
		v = 1
	}
	return btSelector(6, v)
}

func btSelector(slot int, arg uintptr) string {
	st, err := btStatics(nsBT+"BluetoothLEDevice", iidBTLEDeviceStatics2)
	if err != nil {
		return ""
	}
	defer Release(st)
	var hs HString
	if Failed(ComCall(st, slot, arg, uintptr(unsafe.Pointer(&hs)))) { // GetDeviceSelectorFromPairingState, ...FromConnectionStatus
		return ""
	}
	defer hs.Delete()
	return hs.String()
}

// BTFindDevices calls done with the device interface IDs an AQS finds
// (DeviceInformation.FindAllAsync).
func BTFindDevices(aqs string, done func([]string, error)) {
	st, err := btStatics("Windows.Devices.Enumeration.DeviceInformation", iidDeviceInformationStatics)
	if err != nil {
		done(nil, err)
		return
	}
	defer Release(st)
	hs := NewHString(aqs)
	defer hs.Delete()
	btCall(st, 10, iidDeviceInfosCompleted, func(list uintptr, err error) { // FindAllAsyncAqsFilter
		if list == 0 {
			done(nil, err)
			return
		}
		var ids []string
		for _, di := range vectorItems(list) {
			ids = append(ids, hstringOf(di, 6)) // Id
			Release(di)
		}
		Release(list)
		done(ids, nil)
	}, uintptr(hs))
}

// BTPickDevice shows the system's device picker over hwnd, listing the
// devices any of the AQS selectors finds, beside rect (in the window's
// DIPs: x, y, width, height); done gets the device interface ID chosen,
// "" if the user closed it.
func BTPickDevice(hwnd uintptr, selectors []string, rect [4]float32, done func(string, error)) {
	p, err := btNew("Windows.Devices.Enumeration.DevicePicker", iidDevicePicker)
	if err != nil {
		done("", errors.New("not available: no device picker"))
		return
	}
	if iw := btQI(p, iidInitializeWithWindow); iw != 0 {
		ComCall(iw, 3, hwnd) // IInitializeWithWindow.Initialize
		Release(iw)
	}
	if f := btObj(p, 6); f != 0 { // Filter
		if list := btObj(f, 7); list != 0 { // SupportedDeviceSelectors: IVector<String>
			for _, s := range selectors {
				hs := NewHString(s)
				ComCall(list, 13, uintptr(hs)) // IVector.Append
				hs.Delete()
			}
			Release(list)
		}
		Release(f)
	}
	hr, op := comCallRectOp(p, 17, rect) // PickSingleDeviceAsync
	if Failed(hr) || op == 0 {
		Release(p)
		done("", fmt.Errorf("device picker: %#x", uint32(hr)))
		return
	}
	// The picker lives until the user picks or closes it.
	btAwait(op, iidDeviceInfoCompleted, func(di uintptr, err error) {
		Release(p)
		if di == 0 {
			if err == ErrBTCanceled {
				err = nil
			}
			done("", err)
			return
		}
		id := hstringOf(di, 6)
		Release(di)
		done(id, nil)
	})
}

// --- Advertising.

// BTPublisher publishes an advertisement of manufacturer data (all
// Windows lets an app's advertisement have: names, service UUIDs and
// service data are the system's).
type BTPublisher struct {
	p     uintptr
	token int64
}

// NewBTPublisher makes a publisher of the manufacturer data; status gets
// each of its statuses (a Publisher* constant) and the BluetoothError.
func NewBTPublisher(manufacturer map[uint16][]byte, txPower bool, status func(st, btErr int32)) (*BTPublisher, error) {
	ad, err := btNew(nsAdv+"BluetoothLEAdvertisement", iidBTAdvertisement)
	if err != nil {
		return nil, err
	}
	defer Release(ad)
	list := btObj(ad, 11) // ManufacturerData: IVector<BluetoothLEManufacturerData>
	if list == 0 {
		return nil, errors.New("no manufacturer data")
	}
	defer Release(list)
	fact, err := btStatics(nsAdv+"BluetoothLEManufacturerData", iidBTManufacturerDataFact)
	if err != nil {
		return nil, err
	}
	defer Release(fact)
	for id, v := range manufacturer {
		buf, err := btBuffer(v)
		if err != nil {
			return nil, err
		}
		var md uintptr
		hr := ComCall(fact, 6, uintptr(id), buf, uintptr(unsafe.Pointer(&md))) // Create
		Release(buf)
		if Failed(hr) || md == 0 {
			return nil, AsyncHRESULT(hr)
		}
		hr = ComCall(list, 13, md) // IVector.Append
		Release(md)
		if Failed(hr) {
			return nil, AsyncHRESULT(hr)
		}
	}
	pf, err := btStatics(nsAdv+"BluetoothLEAdvertisementPublisher", iidBTPublisherFactory)
	if err != nil {
		return nil, err
	}
	defer Release(pf)
	var p uintptr
	if hr := ComCall(pf, 6, ad, uintptr(unsafe.Pointer(&p))); Failed(hr) || p == 0 { // Create(advertisement)
		return nil, AsyncHRESULT(hr)
	}
	if txPower {
		if p2 := btQI(p, iidBTPublisher2); p2 != 0 {
			ComCall(p2, 13, 1) // put_IncludeTransmitPowerLevel (Windows 10 2004)
			Release(p2)
		}
	}
	x := &BTPublisher{p: p}
	x.token = btEvent(p, 10, iidPublisherStatusHandler, func(_, args uintptr) { // add_StatusChanged
		status(btI32(args, 6), btI32(args, 7)) // Status, Error
	})
	return x, nil
}

// Start starts publishing (its status follows).
func (x *BTPublisher) Start() error {
	if hr := ComCall(x.p, 8); Failed(hr) {
		return AsyncHRESULT(hr)
	}
	return nil
}

// Close stops publishing and lets it go.
func (x *BTPublisher) Close() {
	btRemove(x.p, 10, &x.token)
	ComCall(x.p, 9) // Stop
	Release(x.p)
	x.p = 0
}

// --- The GATT server.

// BTProvider is a GattServiceProvider: one of the app's services.
type BTProvider struct {
	p, svc uintptr
	token  int64
	chars  []*BTLocalChar
}

// BTCreateProvider makes the provider of a service: done gets it, or
// the BluetoothError (BTSuccess) or the failure.
func BTCreateProvider(uuid [16]byte, done func(*BTProvider, int32, error)) {
	st, err := btStatics(nsGatt+"GattServiceProvider", iidGattServiceProviderSt)
	if err != nil {
		done(nil, BTNotSupported, err)
		return
	}
	defer Release(st)
	hr, op := comCallGUIDOp(st, 6, BTGUID(uuid), 0, false) // CreateAsync
	if Failed(hr) || op == 0 {
		done(nil, BTOtherError, AsyncHRESULT(hr))
		return
	}
	btAwait(op, iidProviderResultCompleted, func(r uintptr, err error) {
		x := providerOf(r, err)
		done(x.p, x.e, x.err)
	})
}

type providerResult struct {
	p   *BTProvider
	e   int32
	err error
}

func providerOf(r uintptr, err error) providerResult {
	if r == 0 {
		return providerResult{nil, BTOtherError, err}
	}
	defer Release(r)
	if e := btI32(r, 6); e != BTSuccess { // Error
		return providerResult{nil, e, nil}
	}
	p := btObj(r, 7) // ServiceProvider
	if p == 0 {
		return providerResult{nil, BTOtherError, errors.New("no provider")}
	}
	return providerResult{&BTProvider{p: p, svc: btObj(p, 6)}, BTSuccess, nil} // Service
}

// BTLocalCharParams describe a local characteristic.
type BTLocalCharParams struct {
	UUID                [16]byte
	Props               uint32 // GattCharacteristicProperties
	ReadProt, WriteProt int32  // GattProtectionLevel
	Static              []byte // nil: reads come to ReadRequested
	UserDescription     string
	HasUserDescription  bool
	Formats             [][7]byte // Characteristic Presentation Formats, as the descriptor's value
}

// BTLocalChar is a local characteristic.
type BTLocalChar struct {
	c                 uintptr
	read, write, subs int64
}

// AddCharacteristic adds a characteristic to the provider's service.
func (x *BTProvider) AddCharacteristic(cp BTLocalCharParams, done func(*BTLocalChar, int32, error)) {
	params, err := btNew(nsGatt+"GattLocalCharacteristicParameters", iidGattLocalCharParams)
	if err != nil {
		done(nil, BTOtherError, err)
		return
	}
	defer Release(params)
	ComCall(params, 8, uintptr(cp.Props))      // put_CharacteristicProperties
	ComCall(params, 10, uintptr(cp.ReadProt))  // put_ReadProtectionLevel
	ComCall(params, 12, uintptr(cp.WriteProt)) // put_WriteProtectionLevel
	if cp.Static != nil {
		buf, err := btBuffer(cp.Static)
		if err != nil {
			done(nil, BTOtherError, err)
			return
		}
		ComCall(params, 6, buf) // put_StaticValue
		Release(buf)
	}
	if cp.HasUserDescription {
		hs := NewHString(cp.UserDescription)
		ComCall(params, 14, uintptr(hs)) // put_UserDescription
		hs.Delete()
	}
	if len(cp.Formats) > 0 {
		if st, err := btStatics(nsGatt+"GattPresentationFormat", iidGattPresentationFormatSt2); err == nil {
			if list := btObj(params, 16); list != 0 { // PresentationFormats: IVector<GattPresentationFormat>
				for _, f := range cp.Formats {
					var pf uintptr
					exp := int32(int8(f[1]))
					unit := uint16(f[2]) | uint16(f[3])<<8
					desc := uint16(f[5]) | uint16(f[6])<<8
					if !Failed(ComCall(st, 6, uintptr(f[0]), uintptr(uint32(exp)), uintptr(unit), uintptr(f[4]), uintptr(desc), uintptr(unsafe.Pointer(&pf)))) && pf != 0 { // FromParts
						ComCall(list, 13, pf)
						Release(pf)
					}
				}
				Release(list)
			}
			Release(st)
		}
	}
	hr, op := comCallGUIDOp(x.svc, 7, BTGUID(cp.UUID), params, true) // CreateCharacteristicAsync
	if Failed(hr) || op == 0 {
		done(nil, BTOtherError, AsyncHRESULT(hr))
		return
	}
	btAwait(op, iidLocalCharCompleted, func(r uintptr, err error) {
		if r == 0 {
			done(nil, BTOtherError, err)
			return
		}
		defer Release(r)
		if e := btI32(r, 7); e != BTSuccess { // Error
			done(nil, e, nil)
			return
		}
		c := btObj(r, 6) // Characteristic
		if c == 0 {
			done(nil, BTOtherError, errors.New("no characteristic"))
			return
		}
		lc := &BTLocalChar{c: c}
		x.chars = append(x.chars, lc)
		done(lc, BTSuccess, nil)
	})
}

// AddDescriptor adds a descriptor of a fixed value, readable.
func (c *BTLocalChar) AddDescriptor(uuid [16]byte, value []byte, done func(int32, error)) {
	params, err := btNew(nsGatt+"GattLocalDescriptorParameters", iidGattLocalDescParams)
	if err != nil {
		done(BTOtherError, err)
		return
	}
	defer Release(params)
	buf, err := btBuffer(value)
	if err != nil {
		done(BTOtherError, err)
		return
	}
	ComCall(params, 6, buf) // put_StaticValue
	Release(buf)
	ComCall(params, 8, GattPlain)                                // put_ReadProtectionLevel
	hr, op := comCallGUIDOp(c.c, 11, BTGUID(uuid), params, true) // CreateDescriptorAsync
	if Failed(hr) || op == 0 {
		done(BTOtherError, AsyncHRESULT(hr))
		return
	}
	btAwait(op, iidLocalDescCompleted, func(r uintptr, err error) {
		if r == 0 {
			done(BTOtherError, err)
			return
		}
		e := btI32(r, 7) // Error
		if d := btObj(r, 6); d != 0 {
			Release(d)
		}
		Release(r)
		done(e, nil)
	})
}

// BTRequest is a central's read or write of a local characteristic,
// answered by Respond or Fail (once).
type BTRequest struct {
	req, deferral uintptr
	write         bool
	Central       string // its device ID (GattSession.DeviceId's)
	Offset        int
	Value         []byte // a write's
	Response      bool   // a write that waits for the answer
	once          sync.Once
}

// Respond answers: a read with value, a write with success.
func (r *BTRequest) Respond(value []byte) {
	r.once.Do(func() {
		if r.write {
			ComCall(r.req, 12) // Respond
		} else if buf, err := btBuffer(value); err == nil {
			ComCall(r.req, 11, buf) // RespondWithValue
			Release(buf)
		} else {
			ComCall(r.req, 12, 0x0e) // RespondWithProtocolError: Unlikely Error
		}
		r.finish()
	})
}

// Fail answers with an ATT error.
func (r *BTRequest) Fail(code byte) {
	r.once.Do(func() {
		slot := 12 // GattReadRequest.RespondWithProtocolError
		if r.write {
			slot = 13 // GattWriteRequest.RespondWithProtocolError
		}
		ComCall(r.req, slot, uintptr(code))
		r.finish()
	})
}

// Drop completes a request no one answers (a write without response).
func (r *BTRequest) Drop() { r.once.Do(r.finish) }

func (r *BTRequest) finish() {
	if r.deferral != 0 {
		ComCall(r.deferral, 6) // IDeferral.Complete
		Release(r.deferral)
		r.deferral = 0
	}
	Release(r.req)
	r.req = 0
}

// btSessionDevice is a GattSession's device ID.
func btSessionDevice(session uintptr) string {
	if session == 0 {
		return ""
	}
	id := btObj(session, 6) // DeviceId: BluetoothDeviceId
	if id == 0 {
		return ""
	}
	defer Release(id)
	return hstringOf(id, 6) // Id
}

// OnRequests calls read and write with centrals' requests.
func (c *BTLocalChar) OnRequests(read, write func(*BTRequest)) {
	take := func(args uintptr, isWrite bool, iid GUID, f func(*BTRequest)) {
		s := btObj(args, 6) // Session
		central := btSessionDevice(s)
		Release(s)
		deferral := btObj(args, 7)                        // GetDeferral
		btCall(args, 8, iid, func(req uintptr, _ error) { // GetRequestAsync
			r := &BTRequest{req: req, deferral: deferral, write: isWrite, Central: central}
			if req == 0 {
				r.Drop()
				return
			}
			if isWrite {
				r.Value = bufferOf(req, 6)      // Value
				r.Offset = int(btU32(req, 7))   // Offset
				r.Response = btI32(req, 8) == 0 // Option: WriteWithResponse
			} else {
				r.Offset = int(btU32(req, 6)) // Offset
			}
			f(r)
		})
	}
	c.read = btEvent(c.c, 18, iidReadRequestedHandler, func(_, args uintptr) { // add_ReadRequested
		take(args, false, iidReadRequestCompleted, read)
	})
	c.write = btEvent(c.c, 20, iidWriteRequestedHandler, func(_, args uintptr) { // add_WriteRequested
		take(args, true, iidWriteRequestCompleted, write)
	})
}

// BTClient is a central subscribed to a local characteristic.
type BTClient struct {
	obj     uintptr
	Central string // its device ID
}

// Release lets it go.
func (b *BTClient) Release() { Release(b.obj); b.obj = 0 }

// OnSubscribers calls f with the subscribed centrals (references f
// keeps or releases) each time they change.
func (c *BTLocalChar) OnSubscribers(f func([]*BTClient)) {
	c.subs = btEvent(c.c, 16, iidSubscribersChangedHndlr, func(sender, _ uintptr) { // add_SubscribedClientsChanged
		f(c.Subscribers())
	})
}

// Subscribers are the subscribed centrals now.
func (c *BTLocalChar) Subscribers() []*BTClient {
	list := btObj(c.c, 15) // SubscribedClients: IVectorView<GattSubscribedClient>
	if list == 0 {
		return nil
	}
	defer Release(list)
	var out []*BTClient
	for _, x := range vectorItems(list) {
		s := btObj(x, 6) // Session
		out = append(out, &BTClient{obj: x, Central: btSessionDevice(s)})
		Release(s)
	}
	return out
}

// Notify sends value to the subscribed centrals: to clients (nil: all).
func (c *BTLocalChar) Notify(value []byte, clients []*BTClient, done func(error)) {
	buf, err := btBuffer(value)
	if err != nil {
		done(err)
		return
	}
	defer Release(buf)
	if clients == nil {
		btCall(c.c, 22, iidNotifyAllCompleted, func(r uintptr, err error) { // NotifyValueAsync
			Release(r)
			done(err)
		}, buf)
		return
	}
	left := len(clients)
	if left == 0 {
		done(nil)
		return
	}
	var mu sync.Mutex
	var first error
	for _, cl := range clients {
		btCall(c.c, 23, iidNotifyOneCompleted, func(r uintptr, err error) { // NotifyValueForSubscribedClientAsync
			Release(r)
			mu.Lock()
			if first == nil {
				first = err
			}
			left--
			last := left == 0
			e := first
			mu.Unlock()
			if last {
				done(e)
			}
		}, buf, cl.obj)
	}
}

func (c *BTLocalChar) close() {
	btRemove(c.c, 18, &c.read)
	btRemove(c.c, 20, &c.write)
	btRemove(c.c, 16, &c.subs)
	Release(c.c)
	c.c = 0
}

// OnStatus calls f with the provider's advertising statuses (a
// Provider* constant) and the BluetoothError.
func (x *BTProvider) OnStatus(f func(st, btErr int32)) {
	x.token = btEvent(x.p, 8, iidProviderStatusHandler, func(_, args uintptr) { // add_AdvertisementStatusChanged
		f(btI32(args, 7), btI32(args, 6)) // Status, Error
	})
}

// Status is the provider's advertising status.
func (x *BTProvider) Status() int32 { return btI32(x.p, 7) }

// Advertise publishes the service: connectable (centrals connected to
// the computer use it), discoverable (its UUID, and serviceData if any,
// advertised). A provider advertising already changes its parameters.
func (x *BTProvider) Advertise(connectable, discoverable bool, serviceData []byte) error {
	params, err := btNew(nsGatt+"GattServiceProviderAdvertisingParameters", iidGattAdvertisingParams)
	if err != nil {
		return err
	}
	defer Release(params)
	ComCall(params, 6, uintptr(boolByte(connectable)))  // put_IsConnectable
	ComCall(params, 8, uintptr(boolByte(discoverable))) // put_IsDiscoverable
	if serviceData != nil {
		if p2 := btQI(params, iidGattAdvertisingParams2); p2 != 0 {
			if buf, err := btBuffer(serviceData); err == nil {
				ComCall(p2, 6, buf) // put_ServiceData
				Release(buf)
			}
			Release(p2)
		}
	}
	st := x.Status()
	if st == ProviderStarted || st == ProviderStartedWithoutData {
		if p2 := btQI(x.p, iidGattServiceProvider2); p2 != 0 {
			hr := ComCall(p2, 6, params) // UpdateAdvertisingParameters (Windows 11)
			Release(p2)
			if !Failed(hr) {
				return nil
			}
		}
		ComCall(x.p, 12) // StopAdvertising
	}
	if hr := ComCall(x.p, 11, params); Failed(hr) { // StartAdvertisingWithParameters
		return AsyncHRESULT(hr)
	}
	return nil
}

// Close stops the provider (its service goes) and lets it go.
func (x *BTProvider) Close() {
	btRemove(x.p, 8, &x.token)
	for _, c := range x.chars {
		c.close()
	}
	x.chars = nil
	ComCall(x.p, 12) // StopAdvertising
	Release(x.svc)
	Release(x.p)
	x.p, x.svc = 0, 0
}

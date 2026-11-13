//go:build windows

package windows

import (
	"errors"
	"math"
	"unsafe"
)

// Location: WinRT's Geolocator (Windows.Devices.Geolocation), the user's
// permission asked through RequestAccessAsync. Its answers come on other
// threads: the callbacks are called there.

var (
	iidGeolocator         = NewGUID("A9C3BF62-4524-4989-8AA2-DE5F28A8A49C")
	iidGeolocatorStatics  = NewGUID("9A8E7571-2DF5-4591-9F87-EB5FD894E9B7")
	iidGeoposition        = NewGUID("C18D0454-7D41-4FF7-A957-9DBA05AB9CE8")
	iidPositionChangedArg = NewGUID("37859CE5-9D1E-46C5-BF3B-6AD8CAC1A093")

	asyncHandler        = "{fcdcf02c-e5d8-4478-915a-4d90b74b83a5}"
	iidAccessCompleted  = ParamIID("pinterface(" + asyncHandler + ";enum(Windows.Devices.Geolocation.GeolocationAccessStatus;i4))")
	iidPositionComplete = ParamIID("pinterface(" + asyncHandler + ";" + RuntimeClassSig("Windows.Devices.Geolocation.Geoposition", iidGeoposition) + ")")
	iidPositionChanged  = ParamIID("pinterface({9de1c534-6ae1-11e0-84e1-18a905bcc53f};" +
		RuntimeClassSig("Windows.Devices.Geolocation.Geolocator", iidGeolocator) + ";" +
		RuntimeClassSig("Windows.Devices.Geolocation.PositionChangedEventArgs", iidPositionChangedArg) + ")")

	// ErrLocationDenied is the user's (or the system's) no.
	ErrLocationDenied = errors.New("location: denied")
)

// Fix is a position: degrees, meters (altitude 0: not known), Unix
// milliseconds.
type Fix struct {
	Lat, Lon, Acc, Alt float64
	T                  int64
}

// Geolocator follows the device's position.
type Geolocator struct {
	g     uintptr
	token int64
}

// RequestLocation asks for the user's permission (once; then it's
// remembered by Windows) and calls done with whether it's given.
func RequestLocation(done func(bool, error)) {
	st, err := ActivationFactory("Windows.Devices.Geolocation.Geolocator", iidGeolocatorStatics)
	if err != nil {
		done(false, err)
		return
	}
	defer Release(st)
	var op uintptr
	if hr := ComCall(st, 6, uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 { // RequestAccessAsync
		done(false, errors.New("location: can't ask"))
		return
	}
	h := NewDelegate(iidAccessCompleted, func(op, status uintptr) {
		var access int32
		if status == 1 {
			ComCall(op, 8, uintptr(unsafe.Pointer(&access))) // GetResults
		}
		done(access == 1, nil) // Allowed
	})
	ComCall(op, 6, h) // put_Completed
	Release(h)
	Release(op)
}

// NewGeolocator makes a locator.
func NewGeolocator() (*Geolocator, error) {
	g, err := ActivateInstance("Windows.Devices.Geolocation.Geolocator")
	if err != nil {
		return nil, err
	}
	defer Release(g)
	l := QueryInterface(g, &iidGeolocator)
	if l == 0 {
		return nil, errors.New("location: no Geolocator")
	}
	ComCall(l, 7, 1) // put_DesiredAccuracy: High
	return &Geolocator{g: l}, nil
}

// readFix reads an IGeoposition.
func readFix(pos uintptr) (Fix, bool) {
	var c uintptr
	if Failed(ComCall(pos, 6, uintptr(unsafe.Pointer(&c)))) || c == 0 { // get_Coordinate
		return Fix{}, false
	}
	defer Release(c)
	var f Fix
	var ts int64
	ComCall(c, 6, uintptr(unsafe.Pointer(&f.Lat))) // get_Latitude
	ComCall(c, 7, uintptr(unsafe.Pointer(&f.Lon))) // get_Longitude
	ComCall(c, 9, uintptr(unsafe.Pointer(&f.Acc))) // get_Accuracy
	ComCall(c, 13, uintptr(unsafe.Pointer(&ts)))   // get_Timestamp: 100 ns since 1601
	var alt uintptr
	if !Failed(ComCall(c, 8, uintptr(unsafe.Pointer(&alt)))) && alt != 0 { // get_Altitude: IReference<double>
		ComCall(alt, 6, uintptr(unsafe.Pointer(&f.Alt)))
		Release(alt)
	}
	f.T = ts/10000 - 11644473600000
	if math.IsNaN(f.Lat) || math.IsNaN(f.Lon) {
		return Fix{}, false
	}
	return f, true
}

// Current calls done with a position, once there's one.
func (l *Geolocator) Current(done func(Fix, error)) {
	var op uintptr
	if hr := ComCall(l.g, 13, uintptr(unsafe.Pointer(&op))); Failed(hr) || op == 0 { // GetGeopositionAsync
		if uint32(hr) == 0x80070005 { // E_ACCESSDENIED
			done(Fix{}, ErrLocationDenied)
			return
		}
		done(Fix{}, errors.New("location: unavailable"))
		return
	}
	h := NewDelegate(iidPositionComplete, func(op, status uintptr) {
		var pos uintptr
		if status == 1 {
			ComCall(op, 8, uintptr(unsafe.Pointer(&pos)))
		}
		if pos == 0 {
			if status == 3 { // Error
				done(Fix{}, errors.New("location: unavailable (denied, or off)"))
			} else {
				done(Fix{}, errors.New("location: unavailable"))
			}
			return
		}
		f, ok := readFix(pos)
		Release(pos)
		if !ok {
			done(Fix{}, errors.New("location: unavailable"))
			return
		}
		done(f, nil)
	})
	ComCall(op, 6, h)
	Release(h)
	Release(op)
}

// Watch calls moved with each position as the device moves, until
// Stop.
func (l *Geolocator) Watch(moved func(Fix)) {
	if l.token != 0 {
		return
	}
	h := NewDelegate(iidPositionChanged, func(_, args uintptr) {
		var pos uintptr
		if ComCall(args, 6, uintptr(unsafe.Pointer(&pos))); pos != 0 { // get_Position
			if f, ok := readFix(pos); ok {
				moved(f)
			}
			Release(pos)
		}
	})
	ComCall(l.g, 15, h, uintptr(unsafe.Pointer(&l.token))) // add_PositionChanged
	Release(h)
}

// Stop stops watching.
func (l *Geolocator) Stop() {
	if l.token != 0 {
		ComCall(l.g, 16, uintptr(l.token)) // remove_PositionChanged
		l.token = 0
	}
}

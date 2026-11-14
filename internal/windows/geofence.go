//go:build windows

package windows

import (
	"errors"
	"time"
	"unsafe"
)

// Geofencing: WinRT's GeofenceMonitor (Windows.Devices.Geolocation.
// Geofencing), the app's one, its geofences circles (Geocircle). Its
// reports come on another thread: the callback is called there.

var (
	iidGeofenceFactory       = NewGUID("841F624B-325F-4B90-BCA7-2B8022A93796")
	iidGeofenceMonitor       = NewGUID("4C0F5F78-1C1F-4621-BBBD-833B92247226")
	iidGeofenceMonitorStatic = NewGUID("2DD32FCF-7E75-4899-ACE3-2BD0A65CCE06")
	iidGeocircleFactory      = NewGUID("AFD6531F-72B1-4F7D-87CC-4ED4C9849C05")
	iidGeoshape              = NewGUID("C99CA2AF-C729-43C1-8FAB-D6DEC914DF7E")

	iidGeofenceStateChanged = ParamIID("pinterface({9de1c534-6ae1-11e0-84e1-18a905bcc53f};" +
		RuntimeClassSig("Windows.Devices.Geolocation.Geofencing.GeofenceMonitor", iidGeofenceMonitor) + ";cinterface(IInspectable))")
)

// Geofence states (a report's), and the states a geofence reports.
const (
	GeofenceEntered = 1
	GeofenceExited  = 2
	GeofenceRemoved = 4
)

// GeofenceReport is a geofence's new state, and where the device was.
type GeofenceReport struct {
	ID    string
	State int
	At    *Fix
}

// Geofences is the app's GeofenceMonitor.
type Geofences struct {
	m     uintptr
	token int64
}

// OpenGeofences gets the app's monitor.
func OpenGeofences() (*Geofences, error) {
	st, err := ActivationFactory("Windows.Devices.Geolocation.Geofencing.GeofenceMonitor", iidGeofenceMonitorStatic)
	if err != nil {
		return nil, err
	}
	defer Release(st)
	var m uintptr
	if hr := ComCall(st, 6, uintptr(unsafe.Pointer(&m))); Failed(hr) || m == 0 { // get_Current
		return nil, errors.New("location: no geofence monitor")
	}
	return &Geofences{m: m}, nil
}

// vector is the monitor's IVector<Geofence>.
func (g *Geofences) vector() uintptr {
	var v uintptr
	ComCall(g.m, 7, uintptr(unsafe.Pointer(&v))) // get_Geofences
	return v
}

func fenceID(f uintptr) string {
	var h HString
	ComCall(f, 9, uintptr(unsafe.Pointer(&h))) // get_Id
	defer h.Delete()
	return h.String()
}

// IDs are the geofences monitored.
func (g *Geofences) IDs() []string {
	v := g.vector()
	if v == 0 {
		return nil
	}
	defer Release(v)
	var n uint32
	ComCall(v, 7, uintptr(unsafe.Pointer(&n))) // get_Size
	var out []string
	for i := uint32(0); i < n; i++ {
		var f uintptr
		if Failed(ComCall(v, 6, uintptr(i), uintptr(unsafe.Pointer(&f)))) || f == 0 { // GetAt
			continue
		}
		out = append(out, fenceID(f))
		Release(f)
	}
	return out
}

// Remove stops monitoring the geofence id ("": all of them).
func (g *Geofences) Remove(id string) {
	v := g.vector()
	if v == 0 {
		return
	}
	defer Release(v)
	if id == "" {
		ComCall(v, 15) // Clear
		return
	}
	var n uint32
	ComCall(v, 7, uintptr(unsafe.Pointer(&n)))
	for i := int(n) - 1; i >= 0; i-- {
		var f uintptr
		if Failed(ComCall(v, 6, uintptr(i), uintptr(unsafe.Pointer(&f)))) || f == 0 {
			continue
		}
		if fenceID(f) == id {
			ComCall(v, 12, uintptr(i)) // RemoveAt
		}
		Release(f)
	}
}

// Add monitors a circle as the geofence id (replacing one with the ID),
// reporting states (GeofenceEntered | GeofenceExited) once the device has
// been in (or out) for dwell.
func (g *Geofences) Add(id string, lat, lon, radius float64, states int, dwell time.Duration) error {
	cf, err := ActivationFactory("Windows.Devices.Geolocation.Geocircle", iidGeocircleFactory)
	if err != nil {
		return err
	}
	defer Release(cf)
	var circle uintptr
	if hr := comCallCircle(cf, 6, lat, lon, 0, radius, &circle); Failed(hr) || circle == 0 { // Create
		return errors.New("location: can't make the geofence's circle")
	}
	defer Release(circle)
	shape := QueryInterface(circle, &iidGeoshape)
	if shape == 0 {
		return errors.New("location: no geoshape")
	}
	defer Release(shape)
	ff, err := ActivationFactory("Windows.Devices.Geolocation.Geofencing.Geofence", iidGeofenceFactory)
	if err != nil {
		return err
	}
	defer Release(ff)
	name := NewHString(id)
	defer name.Delete()
	var fence uintptr
	// CreateWithMonitorStatesAndDwellTime(id, shape, states, singleUse, dwellTime: 100 ns ticks)
	if hr := ComCall(ff, 8, uintptr(name), shape, uintptr(states), 0, uintptr(dwell/100), uintptr(unsafe.Pointer(&fence))); Failed(hr) || fence == 0 {
		return errors.New("location: can't make the geofence")
	}
	defer Release(fence)
	g.Remove(id)
	v := g.vector()
	if v == 0 {
		return errors.New("location: no geofences")
	}
	defer Release(v)
	if hr := ComCall(v, 13, fence); Failed(hr) { // Append
		return errors.New("location: Windows didn't take the geofence")
	}
	return nil
}

// Watch calls reported with the monitor's reports as they come.
func (g *Geofences) Watch(reported func([]GeofenceReport)) {
	if g.token != 0 {
		return
	}
	h := NewDelegate(iidGeofenceStateChanged, func(_, _ uintptr) {
		var view uintptr
		if Failed(ComCall(g.m, 11, uintptr(unsafe.Pointer(&view)))) || view == 0 { // ReadReports
			return
		}
		defer Release(view)
		var n uint32
		ComCall(view, 7, uintptr(unsafe.Pointer(&n)))
		var rs []GeofenceReport
		for i := uint32(0); i < n; i++ {
			var r uintptr
			if Failed(ComCall(view, 6, uintptr(i), uintptr(unsafe.Pointer(&r)))) || r == 0 {
				continue
			}
			var state int32
			ComCall(r, 6, uintptr(unsafe.Pointer(&state))) // get_NewState
			rep := GeofenceReport{State: int(state)}
			var f uintptr
			if ComCall(r, 7, uintptr(unsafe.Pointer(&f))); f != 0 { // get_Geofence
				rep.ID = fenceID(f)
				Release(f)
			}
			var pos uintptr
			if ComCall(r, 8, uintptr(unsafe.Pointer(&pos))); pos != 0 { // get_Geoposition
				if fix, ok := readFix(pos); ok {
					rep.At = &fix
				}
				Release(pos)
			}
			Release(r)
			rs = append(rs, rep)
		}
		if len(rs) > 0 {
			reported(rs)
		}
	})
	ComCall(g.m, 9, h, uintptr(unsafe.Pointer(&g.token))) // add_GeofenceStateChanged
	Release(h)
}

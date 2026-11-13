//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"
	"math"
	"syscall"
	"unsafe"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sensors on Linux: iio-sensor-proxy (net.hadess.SensorProxy, on
// the system bus), which convertible laptops and tablets run. It gives
// the ambient light (in lux, when the sensor says so) and the
// accelerometer's orientation, not its readings: the accelerometer reads
// 1 g along the axis that's up (portrait upright: y; on its left side:
// -x; flat or unknown: z, face up), each time the orientation changes.
// Gyroscopes, magnetometers, barometers and pedometers have no service
// an app can read: not available. "available" (u8 sensor), "start" (u8
// sensor, u32 µs: the proxy reports changes, not at a rate), "stop" (u8
// sensor), "stopAll", "next" (the samples since the last, once there are
// some: u8 sensor, f64 seconds since boot, u8 count, count f64s), "steps"
// (not available).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["sensors"] = h.sensorsModule()
	})
}

const (
	sensorAccelerometer = 0
	sensorLight         = 5

	sensorProxyDest  = "net.hadess.SensorProxy"
	sensorProxyPath  = "/net/hadess/SensorProxy"
	sensorProxyIface = "net.hadess.SensorProxy"
)

var sensorState struct {
	claimed map[byte]bool
	sub     uint32
	buf     []byte
	count   int
	waiting []func([]byte, error)
	// props are the proxy's properties last read.
	orientation string
	light       float64
	lux         bool
}

// sensorProps reads the proxy's properties: done gets them (nil: no
// proxy).
func sensorProps(done func(map[string]any)) {
	conn, err := Bus(BusSystem)
	if err != nil {
		done(nil)
		return
	}
	DBusCall(conn, sensorProxyDest, sensorProxyPath, "org.freedesktop.DBus.Properties", "GetAll",
		VTuple(VStr(sensorProxyIface)), 3000, func(r Ptr, err error) {
			if err != nil {
				done(nil)
				return
			}
			args, _ := VariantGo(r).([]any)
			if len(args) == 0 {
				done(nil)
				return
			}
			props, _ := args[0].(map[string]any)
			done(props)
		})
}

func sensorHas(props map[string]any, kind byte) bool {
	switch kind {
	case sensorAccelerometer:
		b, _ := props["HasAccelerometer"].(bool)
		return b
	case sensorLight:
		b, _ := props["HasAmbientLight"].(bool)
		unit, _ := props["LightLevelUnit"].(string)
		return b && unit == "lux"
	}
	return false
}

func (h *Host) sensorsModule() map[string]method {
	s := &sensorState
	na := func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not available")) }
	return map[string]method{
		"available": func(args []byte, reply func([]byte, error)) {
			if len(args) < 1 || args[0] != sensorAccelerometer && args[0] != sensorLight {
				reply([]byte{0}, nil)
				return
			}
			sensorProps(func(props map[string]any) { reply([]byte{b2u(sensorHas(props, args[0]))}, nil) })
		},
		"start": func(args []byte, reply func([]byte, error)) {
			if len(args) < 1 {
				reply(nil, errors.New("bad args"))
				return
			}
			kind := args[0]
			sensorProps(func(props map[string]any) {
				if !sensorHas(props, kind) {
					reply(nil, errors.New("not available"))
					return
				}
				h.sensorClaim(kind, props)
				reply(nil, nil)
			})
		},
		"stop": func(args []byte, reply func([]byte, error)) {
			if len(args) > 0 {
				sensorRelease(args[0])
			}
			reply(nil, nil)
		},
		"stopAll": func(_ []byte, reply func([]byte, error)) {
			for k := range s.claimed {
				sensorRelease(k)
			}
			s.buf, s.count = nil, 0
			ws := s.waiting
			s.waiting = nil
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if s.count > 0 {
				sensorFlush(reply)
				return
			}
			s.waiting = append(s.waiting, reply)
		},
		"steps":    na,
		"simulate": na,
	}
}

// sensorClaim asks the proxy for a sensor's updates, follows them, and
// reports its reading now.
func (h *Host) sensorClaim(kind byte, props map[string]any) {
	s := &sensorState
	conn, err := Bus(BusSystem)
	if err != nil {
		return
	}
	if s.claimed == nil {
		s.claimed = map[byte]bool{}
	}
	if !s.claimed[kind] {
		s.claimed[kind] = true
		method := "ClaimAccelerometer"
		if kind == sensorLight {
			method = "ClaimLight"
		}
		DBusCall(conn, sensorProxyDest, sensorProxyPath, sensorProxyIface, method, 0, 3000, func(Ptr, error) {})
	}
	if s.sub == 0 {
		s.sub = DBusSubscribe(conn, sensorProxyDest, "org.freedesktop.DBus.Properties", "PropertiesChanged", sensorProxyPath, func(sig DBusSignal) {
			args, _ := VariantGo(sig.Params).([]any)
			if len(args) < 2 {
				return
			}
			changed, _ := args[1].(map[string]any)
			sensorUpdate(changed)
		})
	}
	// The reading now: it changes only as the device turns.
	o, _ := props["AccelerometerOrientation"].(string)
	l, _ := props["LightLevel"].(float64)
	if kind == sensorAccelerometer {
		sensorAdd(kind, orientationGravity(o))
	} else {
		sensorAdd(kind, []float64{l})
	}
	s.orientation, s.light = o, l
}

func sensorUpdate(changed map[string]any) {
	s := &sensorState
	if o, ok := changed["AccelerometerOrientation"].(string); ok && o != s.orientation {
		s.orientation = o
		if s.claimed[sensorAccelerometer] {
			sensorAdd(sensorAccelerometer, orientationGravity(o))
		}
	}
	if l, ok := changed["LightLevel"].(float64); ok && l != s.light {
		s.light = l
		if s.claimed[sensorLight] {
			sensorAdd(sensorLight, []float64{l})
		}
	}
}

func sensorRelease(kind byte) {
	s := &sensorState
	if !s.claimed[kind] {
		return
	}
	delete(s.claimed, kind)
	if conn, err := Bus(BusSystem); err == nil {
		method := "ReleaseAccelerometer"
		if kind == sensorLight {
			method = "ReleaseLight"
		}
		DBusCall(conn, sensorProxyDest, sensorProxyPath, sensorProxyIface, method, 0, 3000, func(Ptr, error) {})
		if len(s.claimed) == 0 && s.sub != 0 {
			DBusUnsubscribe(conn, s.sub)
			s.sub = 0
		}
	}
}

// orientationGravity is what an accelerometer reads held still in the
// orientation iio-sensor-proxy names, in g, the device's axes (x right,
// y up the screen, z out of it).
func orientationGravity(o string) []float64 {
	switch o {
	case "normal":
		return []float64{0, 1, 0}
	case "bottom-up":
		return []float64{0, -1, 0}
	case "left-up":
		return []float64{-1, 0, 0}
	case "right-up":
		return []float64{1, 0, 0}
	}
	return []float64{0, 0, 1} // flat (or not known): face up
}

// sensorAdd queues a sample, and answers a waiting "next".
func sensorAdd(kind byte, v []float64) {
	s := &sensorState
	le := binary.LittleEndian
	s.buf = append(s.buf, kind)
	s.buf = le.AppendUint64(s.buf, math.Float64bits(sinceBoot()))
	s.buf = append(s.buf, byte(len(v)))
	for _, x := range v {
		s.buf = le.AppendUint64(s.buf, math.Float64bits(x))
	}
	s.count++
	if len(s.waiting) > 0 {
		w := s.waiting[0]
		s.waiting = s.waiting[1:]
		sensorFlush(w)
	}
}

func sensorFlush(reply func([]byte, error)) {
	s := &sensorState
	b := s.buf
	s.buf, s.count = nil, 0
	reply(b, nil)
}

// sinceBoot is the seconds since the system booted (CLOCK_BOOTTIME, as
// Android's sensor timestamps; suspended time counts).
func sinceBoot() float64 {
	var ts syscall.Timespec
	const clockBoottime = 7
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, clockBoottime, uintptr(unsafe.Pointer(&ts)), 0)
	return float64(ts.Sec) + float64(ts.Nsec)/1e9
}

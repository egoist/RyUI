// Package sensors reads the device's motion and environment sensors
// (expo-sensors): the accelerometer, gyroscope and magnetometer, the
// fused device motion (attitude, gravity, the user's acceleration), the
// barometer, the light sensor and the pedometer. Core Motion on iOS;
// Android's SensorManager.
//
//	stop := sensors.WatchAccelerometer(50*time.Millisecond, func(a sensors.Vector) { tilt.Set(a.X) })
//
// The values are the same on both platforms: device axes as Android and
// the W3C define them (x to the right of the screen, y up it, z out of
// it), acceleration as the accelerometer measures it (a device lying face
// up reads z = +1 g), rotations counterclockwise positive, attitude in
// East-North-Up world axes. Readings come on the main thread.
package sensors

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/egoist/ryui/ui"
)

// Sensor is a kind of sensor.
type Sensor uint8

const (
	Accelerometer Sensor = iota
	Gyroscope
	Magnetometer
	Motion // fused: attitude, gravity, user acceleration, rotation rate
	Barometer
	Light // Android
	Pedometer
	sensorCount
)

func (s Sensor) String() string {
	return [...]string{"accelerometer", "gyroscope", "magnetometer", "motion", "barometer", "light", "pedometer", "?"}[min(int(s), int(sensorCount))]
}

// Vector is a reading along the device's axes.
type Vector struct {
	X, Y, Z float64
	// Time is when it was read, since the device booted.
	Time time.Duration
}

// Attitude is the device's orientation in the world.
type Attitude struct {
	// Matrix turns a vector in device axes into East-North-Up world axes
	// (row-major): world = Matrix × device.
	Matrix [9]float64
	// Heading is the direction the top of the device points, in degrees
	// clockwise from magnetic north (0 to 360), or -1 where the device
	// can't tell (no magnetometer, the simulator).
	Heading float64
	// Pitch and Roll are in radians, as Android's getOrientation gives
	// them: pitch about the x axis (-π/2 to π/2; negative as the top
	// tips up), roll about the y axis (-π to π).
	Pitch, Roll float64
}

// MotionReading is the fused device motion.
type MotionReading struct {
	Attitude Attitude
	// Gravity is the accelerometer's part that's gravity, in g (face up:
	// z = +1).
	Gravity Vector
	// Acceleration is the user's, without gravity, in g.
	Acceleration Vector
	// Rotation is the rotation rate, in radians per second.
	Rotation Vector
	Time     time.Duration
}

// Pressure is the barometer's reading.
type Pressure struct {
	// HPa is the air's pressure, in hectopascals (millibars).
	HPa float64
	// Altitude is the change in altitude since watching began, in meters
	// (from the pressure; iOS fuses it with GPS where it can).
	Altitude float64
	Time     time.Duration
}

// Steps are the steps counted.
type Steps struct {
	Steps int
	// Distance is in meters, where the platform estimates it (iOS); else
	// -1.
	Distance float64
}

// ErrNotAvailable: the device has no such sensor (or, for StepsBetween,
// the platform keeps no history: Android).
var ErrNotAvailable = errors.New("sensors: not available")

// Available reports whether the device has s. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Available(s Sensor) bool {
	b, err := ui.CallWait("sensors", "available", []byte{byte(s)})
	return err == nil && len(b) > 0 && b[0] == 1
}

// WatchAccelerometer calls f with the acceleration, in g, about every
// interval (at most 200 times a second) until stop. Main thread.
func WatchAccelerometer(every time.Duration, f func(Vector)) (stop func()) {
	return watch(Accelerometer, every, func(s sample) { f(vector(s, 0)) })
}

// WatchGyroscope calls f with the rotation rate, in radians per second,
// about every interval until stop. Main thread.
func WatchGyroscope(every time.Duration, f func(Vector)) (stop func()) {
	return watch(Gyroscope, every, func(s sample) { f(vector(s, 0)) })
}

// WatchMagnetometer calls f with the magnetic field, calibrated, in
// microteslas, about every interval until stop. Main thread.
func WatchMagnetometer(every time.Duration, f func(Vector)) (stop func()) {
	return watch(Magnetometer, every, func(s sample) { f(vector(s, 0)) })
}

// WatchMotion calls f with the fused device motion about every interval
// until stop. Main thread.
func WatchMotion(every time.Duration, f func(MotionReading)) (stop func()) {
	return watch(Motion, every, func(s sample) { f(motion(s)) })
}

// WatchPressure calls f with the air's pressure and the altitude's change
// about every interval until stop. Main thread.
func WatchPressure(every time.Duration, f func(Pressure)) (stop func()) {
	var p0 float64
	return watch(Barometer, every, func(s sample) {
		p := Pressure{HPa: s.v[0], Time: s.t}
		if s.n > 1 && !math.IsNaN(s.v[1]) {
			p.Altitude = s.v[1] // the platform's
		} else {
			if p0 == 0 {
				p0 = p.HPa
			}
			p.Altitude = altitude(p0, p.HPa)
		}
		f(p)
	})
}

// WatchLight calls f with the ambient light, in lux, as it changes, about
// every interval at most, until stop (Android: iOS has no light sensor
// apps can read). Main thread.
func WatchLight(every time.Duration, f func(lux float64, at time.Duration)) (stop func()) {
	return watch(Light, every, func(s sample) { f(s.v[0], s.t) })
}

// WatchSteps calls f with the steps taken since watching began, as they
// come, until stop. It asks for the permission the first time (iOS's
// Motion & Fitness; Android's physical activity). Main thread.
func WatchSteps(f func(Steps)) (stop func()) {
	return watch(Pedometer, 0, func(s sample) { f(Steps{Steps: int(s.v[0]), Distance: s.v[1]}) })
}

// StepsBetween is the steps taken between from and to (iOS keeps a
// week's). Android keeps none: ErrNotAvailable. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func StepsBetween(from, to time.Time) (Steps, error) {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[:], uint64(from.UnixMilli()))
	binary.LittleEndian.PutUint64(b[8:], uint64(to.UnixMilli()))
	r, err := ui.CallWait("sensors", "steps", b[:])
	if err != nil {
		if err.Error() == "not available" {
			return Steps{}, ErrNotAvailable
		}
		return Steps{}, err
	}
	if len(r) < 16 {
		return Steps{}, errors.New("sensors: a bad reply")
	}
	return Steps{Steps: int(int64(binary.LittleEndian.Uint64(r))), Distance: math.Float64frombits(binary.LittleEndian.Uint64(r[8:]))}, nil
}

// altitude is the change from p0 to p, in meters, by the barometric
// formula (as Android's SensorManager.getAltitude).
func altitude(p0, p float64) float64 {
	h := func(p float64) float64 { return 44330 * (1 - math.Pow(p/1013.25, 1/5.255)) }
	return h(p) - h(p0)
}

// A sample on the wire: u8 sensor, f64 seconds since boot, u8 count, then
// count f64 values.
type sample struct {
	kind Sensor
	t    time.Duration
	n    int
	v    [16]float64
}

func vector(s sample, i int) Vector { return Vector{X: s.v[i], Y: s.v[i+1], Z: s.v[i+2], Time: s.t} }

// motion reads a Motion sample: gravity, user acceleration, rotation rate
// (each x, y, z), then the attitude's source: a 4th group, the magnetic
// field (iOS: getRotationMatrix's way), or a rotation vector quaternion
// x, y, z, w with a 5th value (Android's rotation vector sensor).
func motion(s sample) MotionReading {
	m := MotionReading{Gravity: vector(s, 0), Acceleration: vector(s, 3), Rotation: vector(s, 6), Time: s.t}
	switch s.n {
	case 13: // a rotation vector: x, y, z, w
		m.Attitude = fromMatrix(rotationFromQuaternion(s.v[9], s.v[10], s.v[11], s.v[12]), true)
	case 12: // the magnetic field
		g := [3]float64{s.v[0], s.v[1], s.v[2]}
		mag := [3]float64{s.v[9], s.v[10], s.v[11]}
		r, ok := rotationFromGravity(g, mag)
		m.Attitude = fromMatrix(r, ok)
	default:
		r, _ := rotationFromGravity([3]float64{s.v[0], s.v[1], s.v[2]}, [3]float64{})
		m.Attitude = fromMatrix(r, false)
	}
	return m
}

// rotationFromQuaternion is Android's getRotationMatrixFromVector: the
// device-to-ENU matrix of a unit quaternion.
func rotationFromQuaternion(x, y, z, w float64) [9]float64 {
	return [9]float64{
		1 - 2*y*y - 2*z*z, 2*x*y - 2*z*w, 2*x*z + 2*y*w,
		2*x*y + 2*z*w, 1 - 2*x*x - 2*z*z, 2*y*z - 2*x*w,
		2*x*z - 2*y*w, 2*y*z + 2*x*w, 1 - 2*x*x - 2*y*y,
	}
}

// rotationFromGravity is Android's getRotationMatrix: the device-to-ENU
// matrix from gravity (as the accelerometer reads it: up) and the
// magnetic field, both in device axes. Without a usable field (none, or
// along gravity), ok is false and the matrix has the device's y axis as
// north: tilt, without a heading.
func rotationFromGravity(g, m [3]float64) (r [9]float64, ok bool) {
	cross := func(a, b [3]float64) [3]float64 {
		return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
	}
	norm := func(a [3]float64) ([3]float64, bool) {
		l := math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
		if l < 1e-9 {
			return a, false
		}
		return [3]float64{a[0] / l, a[1] / l, a[2] / l}, true
	}
	up, okUp := norm(g)
	if !okUp {
		return [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, false
	}
	east, ok := norm(cross(m, up))
	if !ok || math.Hypot(m[0], math.Hypot(m[1], m[2])) < 0.1 {
		// No field: the device's y, flattened, is north.
		east, okE := norm(cross([3]float64{0, 1, 0}, up))
		if !okE {
			east, _ = norm(cross([3]float64{0, 0, -1}, up))
		}
		north := cross(up, east)
		return [9]float64{east[0], east[1], east[2], north[0], north[1], north[2], up[0], up[1], up[2]}, false
	}
	north := cross(up, east)
	return [9]float64{east[0], east[1], east[2], north[0], north[1], north[2], up[0], up[1], up[2]}, true
}

// fromMatrix is the attitude of a device-to-ENU matrix, its angles as
// Android's getOrientation gives them.
func fromMatrix(r [9]float64, heading bool) Attitude {
	a := Attitude{Matrix: r, Heading: -1}
	// + 0 makes a -0 (a negated exact 0, as iOS's readings have) +0:
	// atan2's answer at ±0 is the sign's, and upright (pitch ±π/2) both
	// platforms should say roll 0.
	a.Pitch = math.Asin(-max(-1, min(1, r[7])))
	a.Roll = math.Atan2(-r[6]+0, r[8]+0)
	if heading {
		az := math.Atan2(r[1], r[4]) * 180 / math.Pi
		if az < 0 {
			az += 360
		}
		a.Heading = az
	}
	return a
}

// Watching: one poll for every sensor watched; each watcher gets the
// samples no closer than its interval.

type watcher struct {
	kind  Sensor
	every time.Duration
	last  time.Duration
	seen  bool
	f     func(sample)
}

var (
	mu       sync.Mutex
	watchers = map[*watcher]bool{}
	running  bool
)

// intervals are the fastest each sensor is watched at (host side).
func fastest(kind Sensor) (time.Duration, bool) {
	var best time.Duration
	found := false
	for w := range watchers {
		if w.kind == kind && (!found || w.every < best) {
			best, found = w.every, true
		}
	}
	return best, found
}

func watch(kind Sensor, every time.Duration, f func(sample)) (stop func()) {
	every = max(every, 5*time.Millisecond)
	if kind == Pedometer {
		every = 0
	}
	w := &watcher{kind: kind, every: every, f: f}
	mu.Lock()
	prev, had := fastest(kind)
	watchers[w] = true
	start := !running
	running = true
	mu.Unlock()
	if !had || every < prev {
		setRate(kind, every)
	}
	if start {
		go follow()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(watchers, w)
			next, still := fastest(kind)
			last := len(watchers) == 0
			mu.Unlock()
			switch {
			case !still:
				ui.Call("sensors", "stop", []byte{byte(kind)}, nil)
			case next != every:
				setRate(kind, next)
			}
			if last {
				ui.Call("sensors", "stopAll", nil, nil)
			}
		})
	}
}

// setRate starts the sensor at the interval (or changes it): u8 sensor,
// u32 microseconds. Every Watch calls it.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func setRate(kind Sensor, every time.Duration) {
	b := binary.LittleEndian.AppendUint32([]byte{byte(kind)}, uint32(every/time.Microsecond))
	ui.Call("sensors", "start", b, nil)
}

// follow asks for the samples since the last ("next" answers once there
// are some), until no one watches.
func follow() {
	for {
		b, err := ui.CallWait("sensors", "next", nil)
		mu.Lock()
		// Stopped as another began watching: wait on for that one.
		if len(watchers) == 0 || err != nil && err.Error() != "stopped" {
			running = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		if err != nil {
			continue
		}
		samples := decode(b)
		ui.Post(func() {
			mu.Lock()
			ws := make([]*watcher, 0, len(watchers))
			for w := range watchers {
				ws = append(ws, w)
			}
			mu.Unlock()
			for _, s := range samples {
				for _, w := range ws {
					if w.kind != s.kind || w.seen && s.t-w.last < w.every-w.every/10 {
						continue
					}
					w.seen, w.last = true, s.t
					w.f(s)
				}
			}
		})
	}
}

func decode(b []byte) []sample {
	var out []sample
	le := binary.LittleEndian
	for len(b) >= 10 {
		s := sample{kind: Sensor(b[0]), t: time.Duration(math.Float64frombits(le.Uint64(b[1:])) * float64(time.Second)), n: int(b[9])}
		b = b[10:]
		if s.n > len(s.v) || len(b) < 8*s.n {
			break
		}
		for i := 0; i < s.n; i++ {
			s.v[i] = math.Float64frombits(le.Uint64(b[8*i:]))
		}
		b = b[8*s.n:]
		out = append(out, s)
	}
	return out
}

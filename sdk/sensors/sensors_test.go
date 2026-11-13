package sensors

import (
	"encoding/binary"
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// A device lying face up, its top to the north: the identity, heading 0.
// Turned so its top points east: heading 90. Upright, facing the user
// from the south, its top up: pitch -π/2.
func TestAttitudeFromGravityAndField(t *testing.T) {
	// The field points north and down (as in the northern hemisphere).
	r, ok := rotationFromGravity([3]float64{0, 0, 1}, [3]float64{0, 20, -40})
	a := fromMatrix(r, ok)
	if !ok || !near(a.Heading, 0) || !near(a.Pitch, 0) || !near(a.Roll, 0) || !near(r[0], 1) || !near(r[4], 1) || !near(r[8], 1) {
		t.Fatalf("face up, north: %+v", a)
	}
	// Top to the east: north is to the device's left (-x).
	r, ok = rotationFromGravity([3]float64{0, 0, 1}, [3]float64{-20, 0, -40})
	if a = fromMatrix(r, ok); !near(a.Heading, 90) {
		t.Fatalf("top east: heading %v", a.Heading)
	}
	// Upright: gravity (as read, up) along the device's y.
	r, ok = rotationFromGravity([3]float64{0, 1, 0}, [3]float64{0, -40, -20})
	if a = fromMatrix(r, ok); !near(a.Pitch, -math.Pi/2) {
		t.Fatalf("upright: pitch %v", a.Pitch)
	}
	// Upright from iOS's exact zeros, negated (-0): roll 0, not π.
	r, ok = rotationFromGravity([3]float64{math.Copysign(0, -1), 1, math.Copysign(0, -1)}, [3]float64{0, -40, -20})
	if a = fromMatrix(r, ok); !near(a.Roll, 0) || !near(a.Pitch, -math.Pi/2) {
		t.Fatalf("upright, signed zeros: %+v", a)
	}
	// No field: tilt only, no heading.
	r, ok = rotationFromGravity([3]float64{0, 0, 1}, [3]float64{})
	if a = fromMatrix(r, ok); ok || a.Heading != -1 || !near(r[4], 1) {
		t.Fatalf("no field: %+v %v", a, ok)
	}
}

// Android's rotation vector: the identity quaternion is face up, north;
// a quarter turn about z (counterclockwise from above) points the top
// west: heading 270.
func TestAttitudeFromQuaternion(t *testing.T) {
	if a := fromMatrix(rotationFromQuaternion(0, 0, 0, 1), true); !near(a.Heading, 0) || !near(a.Pitch, 0) {
		t.Fatalf("identity: %+v", a)
	}
	s := math.Sin(math.Pi / 4)
	if a := fromMatrix(rotationFromQuaternion(0, 0, s, s), true); !near(a.Heading, 270) {
		t.Fatalf("a quarter turn: heading %v", a.Heading)
	}
}

func TestDecodeAndMotion(t *testing.T) {
	put := func(b []byte, kind Sensor, secs float64, v ...float64) []byte {
		b = append(b, byte(kind))
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(secs))
		b = append(b, byte(len(v)))
		for _, x := range v {
			b = binary.LittleEndian.AppendUint64(b, math.Float64bits(x))
		}
		return b
	}
	b := put(nil, Accelerometer, 1.5, 0, 0, 1)
	b = put(b, Motion, 2, 0, 0, 1, 0.1, 0, 0, 0, 0, 0.5, 0, 0, 0, 1)
	ss := decode(append(b, 9)) // a trailing byte: ignored
	if len(ss) != 2 || ss[0].kind != Accelerometer || vector(ss[0], 0).Z != 1 || ss[0].t.Seconds() != 1.5 {
		t.Fatalf("%+v", ss)
	}
	m := motion(ss[1])
	if m.Acceleration.X != 0.1 || m.Rotation.Z != 0.5 || !near(m.Attitude.Heading, 0) {
		t.Fatalf("%+v", m)
	}
	if math.Abs(altitude(1013.25, 1001.29)-100) > 1 {
		t.Fatalf("100 m up: %v", altitude(1013.25, 1001.29))
	}
}

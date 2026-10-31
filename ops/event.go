package ops

import (
	"fmt"
	"math"
)

// Environment is what a host reports about a root view: its size, safe
// area insets, the keyboard, and display and text settings. Sizes are in
// points.
//
// Wire format: f32 width, height, safe top, right, bottom, left,
// keyboard, scale, font scale; u8 flags (1 dark, 2 reduce motion, bits
// 2-4 the Platform); u8 size classes (bits 0-1 the width's, 2-3 the
// height's); string locale; string time zone.
type Environment struct {
	Width, Height                            float32
	SafeTop, SafeRight, SafeBottom, SafeLeft float32
	Keyboard                                 float32 // how much of the root's bottom the keyboard covers
	Scale                                    float32 // pixels per point
	FontScale                                float32 // Dynamic Type / Android font scale; 1 is the default
	Dark                                     bool
	ReduceMotion                             bool // the user asked for less motion (iOS's Reduce Motion; Android's animations off)
	Platform                                 Platform
	// WidthClass and HeightClass are the platform's size classes for
	// the window: UIKit's (compact; regular, reported as SizeClassExpanded)
	// on iOS, Material's window size classes on Android. A host that
	// leaves them unspecified gets Material's, from the size
	// (WidthClassOf, HeightClassOf).
	WidthClass, HeightClass SizeClass
	Locale                  string
	TimeZone                string // IANA, e.g. Asia/Shanghai: the core's time.Local
}

// Platform is the system a host runs on.
type Platform uint8

const (
	PlatformUnknown Platform = iota
	PlatformIOS
	PlatformAndroid
	PlatformMacOS
	PlatformWindows
)

func (p Platform) String() string {
	switch p {
	case PlatformIOS:
		return "iOS"
	case PlatformAndroid:
		return "Android"
	case PlatformMacOS:
		return "macOS"
	case PlatformWindows:
		return "Windows"
	}
	return "unknown"
}

// SizeClass is how much room a window gives an app along one axis, as
// the platform classes it: what to show side by side (a sidebar, a list
// and its detail) rather than one after the other.
type SizeClass uint8

const (
	SizeClassUnspecified SizeClass = iota
	SizeClassCompact               // a phone (in portrait, for the width); a narrow split screen
	SizeClassMedium                // a tablet in portrait, a foldable open (Android; iOS has none)
	SizeClassExpanded              // a tablet in landscape, a desktop window; iOS's regular
)

func (c SizeClass) String() string {
	switch c {
	case SizeClassCompact:
		return "compact"
	case SizeClassMedium:
		return "medium"
	case SizeClassExpanded:
		return "expanded"
	}
	return "unspecified"
}

// WidthClassOf is Material 3's window width class for a width in points
// (dp): compact under 600, medium under 840, else expanded.
func WidthClassOf(w float32) SizeClass {
	switch {
	case w < 600:
		return SizeClassCompact
	case w < 840:
		return SizeClassMedium
	}
	return SizeClassExpanded
}

// HeightClassOf is Material 3's window height class for a height in
// points (dp): compact under 480, medium under 900, else expanded.
func HeightClassOf(h float32) SizeClass {
	switch {
	case h < 480:
		return SizeClassCompact
	case h < 900:
		return SizeClassMedium
	}
	return SizeClassExpanded
}

// Gesture is a continuous gesture's state, in points relative to the
// view (velocity in points per second).
//
// Wire format: u8 state; f32 x, y, translation x, y, velocity x, y,
// scale, rotation.
type Gesture struct {
	State                      GestureState
	X, Y                       float32
	TranslationX, TranslationY float32
	VelocityX, VelocityY       float32
	Scale                      float32
	Rotation                   float32 // a rotation's, in radians clockwise since it began
}

// Event is something a host reports.
type Event struct {
	ID    NodeID
	Kind  EventKind
	Bool  bool
	Int   int64
	Float float64
	X, Y  float64
	Str   string
	Bytes []byte
	Env   Environment
	G     Gesture
}

func (e Event) String() string {
	return fmt.Sprintf("%s %d", e.Kind, e.ID)
}

// AppendEvent appends the encoding of e (hosts do this in their own
// language; Go does it for tests and transports).
func AppendEvent(buf []byte, e *Event) []byte {
	buf = le.AppendUint32(buf, uint32(e.ID))
	buf = le.AppendUint16(buf, uint16(e.Kind))
	switch EventPayload(e.Kind) {
	case PBool:
		b := byte(0)
		if e.Bool {
			b = 1
		}
		buf = append(buf, b)
	case PInt:
		buf = le.AppendUint64(buf, uint64(e.Int))
	case PFloat:
		buf = le.AppendUint64(buf, math.Float64bits(e.Float))
	case PString:
		buf = le.AppendUint32(buf, uint32(len(e.Str)))
		buf = append(buf, e.Str...)
	case PPoint:
		buf = le.AppendUint64(buf, math.Float64bits(e.X))
		buf = le.AppendUint64(buf, math.Float64bits(e.Y))
	case PEnvironment:
		v := &e.Env
		for _, f := range [...]float32{v.Width, v.Height, v.SafeTop, v.SafeRight, v.SafeBottom, v.SafeLeft, v.Keyboard, v.Scale, v.FontScale} {
			buf = le.AppendUint32(buf, math.Float32bits(f))
		}
		d := byte(v.Platform&7) << 2 // flags: 1 dark, 2 reduce motion, bits 2-4 the platform
		if v.Dark {
			d |= 1
		}
		if v.ReduceMotion {
			d |= 2
		}
		buf = append(buf, d, byte(v.WidthClass&3)|byte(v.HeightClass&3)<<2)
		buf = le.AppendUint32(buf, uint32(len(v.Locale)))
		buf = append(buf, v.Locale...)
		buf = le.AppendUint32(buf, uint32(len(v.TimeZone)))
		buf = append(buf, v.TimeZone...)
	case PGesture:
		g := &e.G
		buf = append(buf, byte(g.State))
		for _, f := range [...]float32{g.X, g.Y, g.TranslationX, g.TranslationY, g.VelocityX, g.VelocityY, g.Scale, g.Rotation} {
			buf = le.AppendUint32(buf, math.Float32bits(f))
		}
	case PBytes:
		buf = le.AppendUint32(buf, uint32(len(e.Bytes)))
		buf = append(buf, e.Bytes...)
	}
	return buf
}

// DecodeEvent decodes one event.
func DecodeEvent(data []byte) (Event, error) {
	r := &reader{b: data}
	e := Event{ID: NodeID(r.u32()), Kind: EventKind(r.u16())}
	switch EventPayload(e.Kind) {
	case PNone:
	case PBool:
		e.Bool = r.u8() != 0
	case PInt:
		e.Int = int64(r.u64())
	case PFloat:
		e.Float = r.f64()
	case PString:
		e.Str = string(r.bytes())
	case PPoint:
		e.X, e.Y = r.f64(), r.f64()
	case PEnvironment:
		v := &e.Env
		for _, f := range [...]*float32{&v.Width, &v.Height, &v.SafeTop, &v.SafeRight, &v.SafeBottom, &v.SafeLeft, &v.Keyboard, &v.Scale, &v.FontScale} {
			*f = r.f32()
		}
		f := r.u8()
		v.Dark, v.ReduceMotion, v.Platform = f&1 != 0, f&2 != 0, Platform(f>>2&7)
		c := r.u8()
		v.WidthClass, v.HeightClass = SizeClass(c&3), SizeClass(c>>2&3)
		v.Locale = string(r.bytes())
		v.TimeZone = string(r.bytes())
	case PGesture:
		g := &e.G
		g.State = GestureState(r.u8())
		for _, f := range [...]*float32{&g.X, &g.Y, &g.TranslationX, &g.TranslationY, &g.VelocityX, &g.VelocityY, &g.Scale, &g.Rotation} {
			*f = r.f32()
		}
	case PBytes:
		e.Bytes = append([]byte(nil), r.bytes()...)
	default:
		return e, fmt.Errorf("ops: unknown event kind %d", e.Kind)
	}
	if r.err == nil && len(r.b) != 0 {
		return e, fmt.Errorf("ops: %d bytes after event", len(r.b))
	}
	return e, r.err
}

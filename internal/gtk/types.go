//go:build linux && !android

package gtk

// RGBA is GdkRGBA.
type RGBA struct{ R, G, B, A float32 }

// Point is graphene_point_t.
type Point struct{ X, Y float32 }

// Size is graphene_size_t.
type Size struct{ W, H float32 }

// Rect is graphene_rect_t.
type Rect struct {
	X, Y, W, H float32
}

// RoundedRect is GskRoundedRect: the bounds and the corners' radii (top
// left, top right, bottom right, bottom left).
type RoundedRect struct {
	Bounds  Rect
	Corners [4]Size
}

// Rounded is r with every corner rounded by radius.
func Rounded(r Rect, radius float32) RoundedRect {
	radius = min(radius, r.W/2, r.H/2)
	s := Size{radius, radius}
	return RoundedRect{Bounds: r, Corners: [4]Size{s, s, s, s}}
}

// IntRect is GdkRectangle.
type IntRect struct{ X, Y, W, H int32 }

// PangoRectangle is PangoRectangle (Pango units).
type PangoRectangle struct{ X, Y, W, H int32 }

// ColorStop is GskColorStop.
type ColorStop struct {
	Offset float32
	Color  RGBA
}

// Value is GValue.
type Value struct {
	Type GType
	Data [2]uint64
}

// TextIter is GtkTextIter (opaque, 80 bytes).
type TextIter struct{ _ [80]byte }

// Fundamental GTypes.
const (
	TypeBoolean GType = 5 << 2
	TypeInt     GType = 6 << 2
	TypeUint    GType = 7 << 2
	TypeFloat   GType = 14 << 2
	TypeDouble  GType = 15 << 2
	TypeString  GType = 16 << 2
	TypeObject  GType = 20 << 2
)

// GetBoolProperty reads a gboolean property.
func GetBoolProperty(o Ptr, name string) bool {
	var v Value
	ValueInit(&v, TypeBoolean)
	ObjectGetProperty(o, name, &v)
	b := ValueGetBoolean(&v)
	ValueUnset(&v)
	return b
}

// GetIntProperty reads a gint property.
func GetIntProperty(o Ptr, name string) int {
	var v Value
	ValueInit(&v, TypeInt)
	ObjectGetProperty(o, name, &v)
	i := ValueGetInt(&v)
	ValueUnset(&v)
	return int(i)
}

// GetStringProperty reads a string property.
func GetStringProperty(o Ptr, name string) string {
	var v Value
	ValueInit(&v, TypeString)
	ObjectGetProperty(o, name, &v)
	s := GoString(ValueGetString(&v))
	ValueUnset(&v)
	return s
}

// SetBoolProperty sets a gboolean property.
func SetBoolProperty(o Ptr, name string, b bool) {
	var v Value
	ValueInit(&v, TypeBoolean)
	ValueSetBoolean(&v, b)
	ObjectSetProperty(o, name, &v)
	ValueUnset(&v)
}

// SetStringProperty sets a string property.
func SetStringProperty(o Ptr, name string, s string) {
	var v Value
	ValueInit(&v, TypeString)
	ValueSetString(&v, s)
	ObjectSetProperty(o, name, &v)
	ValueUnset(&v)
}

// Enums.
const (
	OrientationHorizontal = 0
	OrientationVertical   = 1

	OverflowVisible = 0
	OverflowHidden  = 1

	PolicyAlways    = 0
	PolicyAutomatic = 1
	PolicyNever     = 2
	PolicyExternal  = 3

	EllipsizeNone   = 0
	EllipsizeStart  = 1
	EllipsizeMiddle = 2
	EllipsizeEnd    = 3

	WrapWord     = 0 // PangoWrapMode
	WrapChar     = 1
	WrapWordChar = 2

	JustifyLeft   = 0
	JustifyRight  = 1
	JustifyCenter = 2
	JustifyFill   = 3

	TextDirNone = 0
	TextDirLTR  = 1
	TextDirRTL  = 2

	ContentFitFill      = 0
	ContentFitContain   = 1
	ContentFitCover     = 2
	ContentFitScaleDown = 3

	PhaseNone    = 0
	PhaseCapture = 1
	PhaseBubble  = 2
	PhaseTarget  = 3

	EventSequenceNone    = 0
	EventSequenceClaimed = 1
	EventSequenceDenied  = 2

	ScrollVertical   = 1
	ScrollHorizontal = 2
	ScrollBothAxes   = 3
	ScrollDiscrete   = 4
	ScrollKinetic    = 8

	ModShift   = 1 << 0
	ModControl = 1 << 2
	ModAlt     = 1 << 3
	ModSuper   = 1 << 26
	ModMeta    = 1 << 28

	FillWinding = 0 // GskFillRule
	FillEvenOdd = 1

	ResponseDefault     = 0 // AdwResponseAppearance
	ResponseSuggested   = 1
	ResponseDestructive = 2

	ColorSchemeDefault     = 0 // AdwColorScheme
	ColorSchemeForceLight  = 1
	ColorSchemePreferLight = 2
	ColorSchemePreferDark  = 3
	ColorSchemeForceDark   = 4

	AlignFill   = 0
	AlignStart  = 1
	AlignEnd    = 2
	AlignCenter = 3

	MemoryB8G8R8A8Premultiplied = 0 // GdkMemoryFormat
	MemoryR8G8B8A8Premultiplied = 2
	MemoryR8G8B8A8              = 5
)

// EnumValue is an enum type's value by its nick (GTK's enums, so the
// host needn't copy their numbers).
func EnumValue(t GType, nick string) (int32, bool) {
	class := Call(FnTypeClassRef, t)
	if class == 0 {
		return 0, false
	}
	s := CString(nick)
	v := Call(FnEnumGetValueByNick, class, Addr(s))
	if v == 0 {
		return 0, false
	}
	return *At[int32](v), true // GEnumValue.value
}

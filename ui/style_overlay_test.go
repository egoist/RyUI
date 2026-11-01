package ui

import (
	"reflect"
	"testing"
)

// overlay lays every field a style sets, so a variant or a When never
// drops one silently; this fails for a field added to Style and not to
// overlay.
func TestOverlayLaysEveryField(t *testing.T) {
	typ := reflect.TypeOf(Style{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		var over Style
		nonZero(reflect.ValueOf(&over).Elem().Field(i))
		var s Style
		s.overlay(&over)
		if !reflect.DeepEqual(s, over) {
			t.Errorf("overlay drops %s", f.Name)
		}
	}
	// A zero field keeps the base's; a Font is laid field by field.
	s := Style{Background: Accent, Font: Body}
	s.overlay(&Style{Color: Red, Font: Font{Weight: Bold}})
	if s.Background != Accent || s.Color != Red || s.Font != Body.Weighted(Bold) {
		t.Fatalf("%+v", s)
	}
}

// nonZero sets v to a value other than its zero one.
func nonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.String:
		v.SetString("x")
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.Struct:
		for i := range v.NumField() {
			nonZero(v.Field(i))
		}
	default:
		panic("nonZero: " + v.Kind().String())
	}
}

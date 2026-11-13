package sqlite

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Scanning: a type's plan, made once (reflection), then each row's
// columns written into its fields where they are (no reflection, no
// interfaces per value).

// Scanner is a type that reads a column's value itself (sql.NullString,
// and an app's own). Its Scan gets nil, an int64, a float64, a string or
// a []byte (a copy).
type Scanner interface {
	Scan(value any) error
}

type kind uint8

const (
	kSkip kind = iota
	kBool
	kInt
	kInt8
	kInt16
	kInt32
	kInt64
	kUint
	kUint8
	kUint16
	kUint32
	kUint64
	kFloat32
	kFloat64
	kString
	kBytes
	kTime
	kAny     // an interface{} field: the value as Scan gets it
	kScanner // *T is a Scanner
	kPointer // *T of the others: nil for NULL
)

// field is where and how a column's value goes.
type field struct {
	off  uintptr
	kind kind
	typ  reflect.Type // a scanner's or a pointer's
	elem *field       // a pointer's
}

// plan is a type's: its fields by name, or, for a type that isn't a
// struct, the first column into it.
type plan struct {
	typ    reflect.Type
	byName map[string]field // lower case: a db tag, or the field's name
	scalar *field
}

var (
	plans       sync.Map // reflect.Type → *plan
	scannerType = reflect.TypeFor[Scanner]()
	timeType    = reflect.TypeFor[time.Time]()
)

func planOf(t reflect.Type) (*plan, error) {
	if p, ok := plans.Load(t); ok {
		return p.(*plan), nil
	}
	p := &plan{typ: t}
	if t.Kind() == reflect.Struct && t != timeType && !reflect.PointerTo(t).Implements(scannerType) {
		p.byName = map[string]field{}
		if err := addFields(p.byName, t, 0, map[reflect.Type]bool{}); err != nil {
			return nil, err
		}
	} else {
		f, err := fieldFor(t, 0)
		if err != nil {
			return nil, err
		}
		p.scalar = &f
	}
	v, _ := plans.LoadOrStore(t, p)
	return v.(*plan), nil
}

// addFields adds t's exported fields, and those of the structs it embeds
// (a name's first, shallowest, wins).
func addFields(m map[string]field, t reflect.Type, off uintptr, seen map[reflect.Type]bool) error {
	if seen[t] {
		return nil
	}
	seen[t] = true
	var embedded []reflect.StructField
	for i := range t.NumField() {
		sf := t.Field(i)
		name, tagged := sf.Tag.Lookup("db")
		if name == "-" {
			continue
		}
		if sf.Anonymous && !tagged && sf.Type.Kind() == reflect.Struct && sf.Type != timeType &&
			!reflect.PointerTo(sf.Type).Implements(scannerType) {
			embedded = append(embedded, sf)
			continue
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		f, err := fieldFor(sf.Type, off+sf.Offset)
		if err != nil {
			// A field Query can't fill is a column nobody can select into
			// it: say so if one is.
			f = field{off: off + sf.Offset, kind: kSkip, typ: sf.Type}
		}
		key := strings.ToLower(name)
		if _, dup := m[key]; !dup {
			m[key] = f
		}
	}
	for _, sf := range embedded {
		if err := addFields(m, sf.Type, off+sf.Offset, seen); err != nil {
			return err
		}
	}
	return nil
}

func fieldFor(t reflect.Type, off uintptr) (field, error) {
	f := field{off: off, typ: t}
	if t != timeType && reflect.PointerTo(t).Implements(scannerType) {
		f.kind = kScanner
		return f, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		f.kind = kBool
	case reflect.Int:
		f.kind = kInt
	case reflect.Int8:
		f.kind = kInt8
	case reflect.Int16:
		f.kind = kInt16
	case reflect.Int32:
		f.kind = kInt32
	case reflect.Int64:
		f.kind = kInt64
	case reflect.Uint, reflect.Uintptr:
		f.kind = kUint
	case reflect.Uint8:
		f.kind = kUint8
	case reflect.Uint16:
		f.kind = kUint16
	case reflect.Uint32:
		f.kind = kUint32
	case reflect.Uint64:
		f.kind = kUint64
	case reflect.Float32:
		f.kind = kFloat32
	case reflect.Float64:
		f.kind = kFloat64
	case reflect.String:
		f.kind = kString
	case reflect.Slice:
		if t.Elem().Kind() != reflect.Uint8 {
			return f, fmt.Errorf("sqlite: can't scan into %v", t)
		}
		f.kind = kBytes
	case reflect.Interface:
		if t.NumMethod() != 0 {
			return f, fmt.Errorf("sqlite: can't scan into %v", t)
		}
		f.kind = kAny
	case reflect.Struct:
		if t != timeType {
			return f, fmt.Errorf("sqlite: can't scan into %v", t)
		}
		f.kind = kTime
	case reflect.Pointer:
		e, err := fieldFor(t.Elem(), 0)
		if err != nil {
			return f, err
		}
		f.kind = kPointer
		f.elem = &e
	default:
		return f, fmt.Errorf("sqlite: can't scan into %v", t)
	}
	return f, nil
}

// columns maps a query's columns to p's fields: a column with no field
// is skipped. A column's name matches a field's case-insensitively, and
// without its underscores (due_on fills DueOn).
func (p *plan) columns(cols [][]byte) ([]*field, error) {
	out := make([]*field, len(cols))
	if p.scalar != nil {
		if len(cols) > 0 {
			out[0] = p.scalar
		}
		return out, nil
	}
	var buf [64]byte
	for i, c := range cols {
		l := lower(buf[:0], c, false)
		f, ok := p.byName[string(l)]
		if !ok {
			l = lower(buf[:0], c, true)
			f, ok = p.byName[string(l)]
		}
		if !ok {
			continue
		}
		if f.kind == kSkip {
			return nil, fmt.Errorf("sqlite: column %s: can't scan into %v", c, f.typ)
		}
		out[i] = &f
	}
	return out, nil
}

func lower(b, s []byte, dropUnderscores bool) []byte {
	for _, c := range s {
		if dropUnderscores && c == '_' {
			continue
		}
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return b
}

// set writes a value into the field at p.
func (f *field) set(p unsafe.Pointer, typ byte, i int64, fl float64, b []byte) error {
	p = unsafe.Add(p, f.off)
	switch f.kind {
	case kSkip:
		return nil
	case kString:
		switch typ {
		case 0:
			*(*string)(p) = ""
		case 1:
			*(*string)(p) = strconv.FormatInt(i, 10)
		case 2:
			*(*string)(p) = strconv.FormatFloat(fl, 'g', -1, 64)
		default:
			*(*string)(p) = string(b)
		}
		return nil
	case kBytes:
		var v []byte
		switch typ {
		case 1:
			v = strconv.AppendInt(nil, i, 10)
		case 2:
			v = strconv.AppendFloat(nil, fl, 'g', -1, 64)
		case 3, 4:
			v = append(make([]byte, 0, len(b)), b...)
		}
		*(*[]byte)(p) = v
		return nil
	case kBool:
		switch typ {
		case 0:
			*(*bool)(p) = false
		case 1:
			*(*bool)(p) = i != 0
		case 2:
			*(*bool)(p) = fl != 0
		default:
			v, err := strconv.ParseBool(strings.TrimSpace(string(b)))
			if err != nil {
				return fmt.Errorf("sqlite: %q isn't a bool", b)
			}
			*(*bool)(p) = v
		}
		return nil
	case kFloat32, kFloat64:
		var v float64
		switch typ {
		case 1:
			v = float64(i)
		case 2:
			v = fl
		case 3, 4:
			x, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			if err != nil {
				return fmt.Errorf("sqlite: %q isn't a number", b)
			}
			v = x
		}
		if f.kind == kFloat32 {
			*(*float32)(p) = float32(v)
		} else {
			*(*float64)(p) = v
		}
		return nil
	case kTime:
		v, err := timeOf(typ, i, fl, b)
		*(*time.Time)(p) = v
		return err
	case kAny:
		var v any
		switch typ {
		case 1:
			v = i
		case 2:
			v = fl
		case 3:
			v = string(b)
		case 4:
			v = append([]byte{}, b...)
		}
		*(*any)(p) = v
		return nil
	case kScanner:
		var v any
		switch typ {
		case 1:
			v = i
		case 2:
			v = fl
		case 3:
			v = string(b)
		case 4:
			v = append([]byte{}, b...)
		}
		return reflect.NewAt(f.typ, p).Interface().(Scanner).Scan(v)
	case kPointer:
		if typ == 0 {
			*(*unsafe.Pointer)(p) = nil
			return nil
		}
		e := reflect.New(f.typ.Elem()).UnsafePointer()
		if err := f.elem.set(e, typ, i, fl, b); err != nil {
			return err
		}
		*(*unsafe.Pointer)(p) = e
		return nil
	}
	// The integers.
	var v int64
	var u uint64
	unsigned := f.kind >= kUint && f.kind <= kUint64
	switch typ {
	case 1:
		v = i
		if unsigned && i < 0 {
			return fmt.Errorf("sqlite: %d into an unsigned integer", i)
		}
		u = uint64(i)
	case 2:
		if fl != math.Trunc(fl) || math.IsInf(fl, 0) || fl >= 1<<63 || fl < -(1<<63) {
			return fmt.Errorf("sqlite: %v isn't an integer", fl)
		}
		v, u = int64(fl), uint64(fl)
		if unsigned && fl < 0 {
			return fmt.Errorf("sqlite: %v into an unsigned integer", fl)
		}
	case 3, 4:
		s := strings.TrimSpace(string(b))
		var err error
		if unsigned {
			u, err = strconv.ParseUint(s, 10, 64)
		} else {
			v, err = strconv.ParseInt(s, 10, 64)
		}
		if err != nil {
			return fmt.Errorf("sqlite: %q isn't an integer", b)
		}
	}
	switch f.kind {
	case kInt:
		*(*int)(p) = int(v)
		if int64(int(v)) != v {
			return fmt.Errorf("sqlite: %d overflows int", v)
		}
	case kInt8:
		*(*int8)(p) = int8(v)
		if int64(int8(v)) != v {
			return fmt.Errorf("sqlite: %d overflows int8", v)
		}
	case kInt16:
		*(*int16)(p) = int16(v)
		if int64(int16(v)) != v {
			return fmt.Errorf("sqlite: %d overflows int16", v)
		}
	case kInt32:
		*(*int32)(p) = int32(v)
		if int64(int32(v)) != v {
			return fmt.Errorf("sqlite: %d overflows int32", v)
		}
	case kInt64:
		*(*int64)(p) = v
	case kUint:
		*(*uint)(p) = uint(u)
		if uint64(uint(u)) != u {
			return fmt.Errorf("sqlite: %d overflows uint", u)
		}
	case kUint8:
		*(*uint8)(p) = uint8(u)
		if uint64(uint8(u)) != u {
			return fmt.Errorf("sqlite: %d overflows uint8", u)
		}
	case kUint16:
		*(*uint16)(p) = uint16(u)
		if uint64(uint16(u)) != u {
			return fmt.Errorf("sqlite: %d overflows uint16", u)
		}
	case kUint32:
		*(*uint32)(p) = uint32(u)
		if uint64(uint32(u)) != u {
			return fmt.Errorf("sqlite: %d overflows uint32", u)
		}
	case kUint64:
		*(*uint64)(p) = u
	}
	return nil
}

// timeFormats are the texts a time is read from: TimeFormat, what
// SQLite's date and time functions write, and RFC 3339.
var timeFormats = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// timeOf reads a time: text in one of timeFormats (or as time.Time's
// String writes it, which modernc.org/sqlite did), or an integer or real
// number of seconds since 1970 (SQLite's unixepoch()).
func timeOf(typ byte, i int64, fl float64, b []byte) (time.Time, error) {
	switch typ {
	case 0:
		return time.Time{}, nil
	case 1:
		return time.Unix(i, 0).UTC(), nil
	case 2:
		sec, frac := math.Modf(fl)
		return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
	}
	s := strings.TrimSpace(string(b))
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	// time.Time's String: 2006-01-02 15:04:05.999999999 -0700 MST, maybe
	// with its monotonic reading after.
	if i := strings.Index(s, " m="); i > 0 {
		s = s[:i]
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("sqlite: %q isn't a time", b)
}

// scan reads the rows into a []T: up to max of them (0: all).
func scan[T any](r *rows, max int) ([]T, error) {
	p, err := planOf(reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	fields, err := p.columns(r.cols)
	if err != nil {
		return nil, err
	}
	var out []T
	var zero T
	for max == 0 || len(out) < max {
		ok, err := r.next()
		if err != nil {
			return out, err
		}
		if !ok {
			return out, nil
		}
		if len(out) == cap(out) {
			// Room for the rest of the chunk at once.
			out = slices.Grow(out, r.left+1)
		}
		out = append(out, zero)
		at := unsafe.Pointer(&out[len(out)-1])
		a := &r.a
		for _, f := range fields {
			if f == nil {
				a.skipValue()
				continue
			}
			typ, i, fl, b := a.value()
			if err := f.set(at, typ, i, fl, b); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

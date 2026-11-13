package sqlite

import (
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"time"
)

// NamedArg is an argument bound by name: :name, @name or $name in the
// SQL.
type NamedArg struct {
	Name  string
	Value any
}

// Named is an argument bound by name.
//
//	db.Exec(`update notes set text = :text where id = :id`,
//		sqlite.Named("id", 1), sqlite.Named("text", "milk"))
func Named(name string, value any) NamedArg { return NamedArg{name, value} }

// TimeFormat is how a time.Time argument is written: text SQLite's date
// and time functions read, which sorts as the time does in one zone.
const TimeFormat = "2006-01-02 15:04:05.999999999-07:00"

// appendArgs encodes arguments: u32 n, then each: its name, its value.
func appendArgs(b []byte, args []any) ([]byte, error) {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(args)))
	for i, v := range args {
		name := ""
		if n, ok := v.(NamedArg); ok {
			name, v = n.Name, n.Value
		}
		b = appendString(b, name)
		var err error
		if b, err = appendValue(b, v); err != nil {
			return b, fmt.Errorf("sqlite: argument %d: %w", i+1, err)
		}
	}
	return b, nil
}

func appendInt(b []byte, v int64) []byte {
	return binary.LittleEndian.AppendUint64(append(b, 1), uint64(v))
}

func appendFloat(b []byte, v float64) []byte {
	return binary.LittleEndian.AppendUint64(append(b, 2), math.Float64bits(v))
}

func appendText(b []byte, typ byte, v string) []byte {
	return appendString(append(b, typ), v)
}

func appendValue(b []byte, v any) ([]byte, error) {
	switch v := v.(type) {
	case nil:
		return append(b, 0), nil
	case int:
		return appendInt(b, int64(v)), nil
	case int64:
		return appendInt(b, v), nil
	case int32:
		return appendInt(b, int64(v)), nil
	case int16:
		return appendInt(b, int64(v)), nil
	case int8:
		return appendInt(b, int64(v)), nil
	case uint:
		return appendUint(b, uint64(v))
	case uint64:
		return appendUint(b, v)
	case uint32:
		return appendInt(b, int64(v)), nil
	case uint16:
		return appendInt(b, int64(v)), nil
	case uint8:
		return appendInt(b, int64(v)), nil
	case bool:
		if v {
			return appendInt(b, 1), nil
		}
		return appendInt(b, 0), nil
	case float64:
		return appendFloat(b, v), nil
	case float32:
		return appendFloat(b, float64(v)), nil
	case string:
		return appendText(b, 3, v), nil
	case []byte:
		if v == nil {
			return append(b, 0), nil
		}
		b = binary.LittleEndian.AppendUint32(append(b, 4), uint32(len(v)))
		return append(b, v...), nil
	case time.Time:
		return appendTime(b, v), nil
	case driver.Valuer:
		// sql.NullString and its kind, and an app's own types.
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			return append(b, 0), nil
		}
		x, err := v.Value()
		if err != nil {
			return b, err
		}
		if _, again := x.(driver.Valuer); again {
			return b, fmt.Errorf("%T: Value returned a Valuer", v)
		}
		return appendValue(b, x)
	}
	// An app's own types (type Status string), and pointers (nil: NULL).
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return append(b, 0), nil
		}
		return appendValue(b, rv.Elem().Interface())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendInt(b, rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return appendUint(b, rv.Uint())
	case reflect.Float32, reflect.Float64:
		return appendFloat(b, rv.Float()), nil
	case reflect.Bool:
		return appendValue(b, rv.Bool())
	case reflect.String:
		return appendText(b, 3, rv.String()), nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			if rv.IsNil() {
				return append(b, 0), nil
			}
			return appendValue(b, rv.Bytes())
		}
	}
	return b, fmt.Errorf("unsupported type %T", v)
}

func appendUint(b []byte, v uint64) ([]byte, error) {
	if v > math.MaxInt64 {
		return b, fmt.Errorf("uint64 %d is more than SQLite's integers hold", v)
	}
	return appendInt(b, int64(v)), nil
}

func appendTime(b []byte, t time.Time) []byte {
	b = append(b, 3)
	at := len(b)
	b = append(b, 0, 0, 0, 0)
	b = t.AppendFormat(b, TimeFormat)
	binary.LittleEndian.PutUint32(b[at:], uint32(len(b)-at-4))
	return b
}

package signal

// Accessor is what components take for values that can change: a
// *Signal, a *Memo, Func or Static.
type Accessor[T any] interface{ Get() T }

type funcAccessor[T any] func() T

func (f funcAccessor[T]) Get() T { return f() }

// Func makes an accessor that calls f. Reads inside f are tracked by
// whoever calls Get.
func Func[T any](f func() T) Accessor[T] { return funcAccessor[T](f) }

type staticAccessor[T any] struct{ v T }

func (s *staticAccessor[T]) Get() T { return s.v }

// Static makes an accessor for a value that never changes. Components
// set such props once and make no effect for them.
func Static[T any](v T) Accessor[T] { return &staticAccessor[T]{v} }

// StaticValue reports whether a was made by Static, and its value.
func StaticValue[T any](a Accessor[T]) (T, bool) {
	if s, ok := a.(*staticAccessor[T]); ok {
		return s.v, true
	}
	var zero T
	return zero, false
}

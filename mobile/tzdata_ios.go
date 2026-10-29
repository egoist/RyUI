//go:build ios

package mobile

// Go has no time zone database on iOS: the app carries one, so that the
// device's time zone (ops.Environment.TimeZone) can be Go's local time.
import _ "time/tzdata"

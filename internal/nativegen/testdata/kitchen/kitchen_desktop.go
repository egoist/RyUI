//go:build windows || (linux && !android)

package kitchen

// On Windows and Linux the kitchen's native code is Go.
func init() {
	implementKitchen(func() (Kitchen, error) { return nil, ErrBusy })
	implementDial(func(events DialEvents) DialNative { return nil })
}

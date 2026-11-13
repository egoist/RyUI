//go:build !darwin

package update

// CheckBundle checks a Mac app's update (bundle_darwin.go); elsewhere
// there's no code signature to check.
func CheckBundle(bundle, running string) error { return nil }

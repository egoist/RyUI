//go:build !ios || !cgo

package shareddir

// Dir is "": only an iOS app has its data elsewhere for its share sheet.
func Dir() string { return "" }

// Package ios embeds the iOS host's sources for the nui CLI, which
// compiles them into apps. The Swift package around them (Package.swift)
// is for editing and testing them.
package ios

import "embed"

// Files holds Sources/ (the decoder, the UIKit host, the C header) and
// App/ (the app delegate).
//
//go:embed Sources App
var Files embed.FS

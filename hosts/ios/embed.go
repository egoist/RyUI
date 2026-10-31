// Package ios embeds the iOS host's sources for the nui CLI, which
// compiles them into apps. The Swift package around them (Package.swift)
// is for editing and testing them.
package ios

import "embed"

// Files holds Sources/ (the decoder, the UIKit host, the C header),
// App/ (the app delegate) and Share/ (the share extension, a binary of
// its own).
//
//go:embed Sources App Share
var Files embed.FS

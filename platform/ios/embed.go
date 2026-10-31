// Package ios embeds the iOS host's sources for the RyUI CLI, which
// compiles them into apps. The Swift package around them (Package.swift)
// is for editing and testing them.
package ios

import "embed"

// Files holds Sources/ (the decoder, the UIKit host, the C header),
// App/ (the app delegate), Share/ (the share extension, a binary of its
// own) and Widget/ (the widget extension's template).
//
//go:embed Sources App Share Widget
var Files embed.FS

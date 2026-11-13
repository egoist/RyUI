// Package macos embeds the Swift an app's native code on the Mac builds
// on, for the Asuka CLI, which compiles it with the app's macos/*.swift
// into a library the Mac host (host/darwin) loads.
package macos

import "embed"

// Files holds Sources/AsukaMac.
//
//go:embed Sources
var Files embed.FS

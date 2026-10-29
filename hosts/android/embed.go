// Package android embeds the Android host's Gradle project for the nui
// CLI, which builds the shell app from it.
package android

import "embed"

// Files holds the nui library, the shell app and the Gradle wrapper.
//
//go:embed settings.gradle.kts build.gradle.kts gradle.properties gradlew gradle/wrapper nui/build.gradle.kts nui/consumer-rules.pro nui/src app/build.gradle.kts app/src
var Files embed.FS

// Package android embeds the Android host's Gradle project for the RyUI
// CLI, which builds the shell app from it.
package android

import "embed"

// Files holds the RyUI library, the shell app and the Gradle wrapper.
//
//go:embed settings.gradle.kts build.gradle.kts gradle.properties gradlew gradle/wrapper ryui/build.gradle.kts ryui/consumer-rules.pro ryui/src ryui-scan/build.gradle.kts ryui-scan/consumer-rules.pro ryui-scan/src app/build.gradle.kts app/src
var Files embed.FS

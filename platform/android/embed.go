// Package android embeds the Android host's Gradle project for the RyUI
// CLI, which builds the shell app from it.
package android

import "embed"

// Files holds the RyUI library, its optional modules (ryui-*), the shell
// app and the Gradle wrapper.
//
//go:embed settings.gradle.kts build.gradle.kts gradle.properties gradlew gradle/wrapper ryui/build.gradle.kts ryui/consumer-rules.pro ryui/src ryui-scan/build.gradle.kts ryui-scan/consumer-rules.pro ryui-scan/src ryui-vision/build.gradle.kts ryui-vision/consumer-rules.pro ryui-vision/src ryui-review/build.gradle.kts ryui-review/src ryui-push/build.gradle.kts ryui-push/src ryui-integrity/build.gradle.kts ryui-integrity/src ryui-agerange/build.gradle.kts ryui-agerange/src ryui-maps/build.gradle.kts ryui-maps/src ryui-periodic/build.gradle.kts ryui-periodic/src app/build.gradle.kts app/src
var Files embed.FS

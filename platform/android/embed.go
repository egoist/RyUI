// Package android embeds the Android host's Gradle project for the Asuka
// CLI, which builds the shell app from it.
package android

import "embed"

// Files holds the Asuka library, its optional modules (asuka-*), the shell
// app and the Gradle wrapper.
//
//go:embed settings.gradle.kts build.gradle.kts gradle.properties gradlew gradle/wrapper asuka/build.gradle.kts asuka/consumer-rules.pro asuka/src asuka-scan/build.gradle.kts asuka-scan/consumer-rules.pro asuka-scan/src asuka-vision/build.gradle.kts asuka-vision/consumer-rules.pro asuka-vision/src asuka-review/build.gradle.kts asuka-review/src asuka-push/build.gradle.kts asuka-push/src asuka-integrity/build.gradle.kts asuka-integrity/src asuka-agerange/build.gradle.kts asuka-agerange/src asuka-maps/build.gradle.kts asuka-maps/src asuka-periodic/build.gradle.kts asuka-periodic/src asuka-purchase/build.gradle.kts asuka-purchase/src app/build.gradle.kts app/src
var Files embed.FS

plugins {
    id("com.android.application") version "8.12.0" apply false
    id("com.android.library") version "8.12.0" apply false
    id("org.jetbrains.kotlin.android") version "2.1.20" apply false
}

// asuka build --platform android --aar: the optional modules (asuka-*) an embedded app
// uses are published beside it, their dependency on :asuka as the app's
// coordinates.
providers.gradleProperty("asukaMavenRepo").orNull?.let { repo ->
    subprojects {
        if (!name.startsWith("asuka-")) return@subprojects
        apply(plugin = "maven-publish")
        plugins.withId("com.android.library") {
            extensions.configure<com.android.build.gradle.LibraryExtension> { publishing { singleVariant("release") } }
            // After the Android plugin's own: its components exist by then.
            afterEvaluate {
                extensions.configure<PublishingExtension> {
                    publications {
                        create<MavenPublication>("release") {
                            from(components["release"])
                            groupId = providers.gradleProperty("asukaGroup").orNull ?: "dev.asuka"
                            artifactId = project.name
                            version = providers.gradleProperty("asukaVersion").orNull ?: "1.0"
                        }
                    }
                    repositories { maven { name = "embed"; url = uri(repo) } }
                }
            }
        }
    }
}

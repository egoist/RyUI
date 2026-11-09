plugins {
    id("com.android.application") version "8.12.0" apply false
    id("com.android.library") version "8.12.0" apply false
    id("org.jetbrains.kotlin.android") version "2.1.20" apply false
}

// ryui build -platform android -aar: the optional modules (ryui-*) an embedded app
// uses are published beside it, their dependency on :ryui as the app's
// coordinates.
providers.gradleProperty("ryuiMavenRepo").orNull?.let { repo ->
    subprojects {
        if (!name.startsWith("ryui-")) return@subprojects
        apply(plugin = "maven-publish")
        plugins.withId("com.android.library") {
            extensions.configure<com.android.build.gradle.LibraryExtension> { publishing { singleVariant("release") } }
            // After the Android plugin's own: its components exist by then.
            afterEvaluate {
                extensions.configure<PublishingExtension> {
                    publications {
                        create<MavenPublication>("release") {
                            from(components["release"])
                            groupId = providers.gradleProperty("ryuiGroup").orNull ?: "dev.ryui"
                            artifactId = project.name
                            version = providers.gradleProperty("ryuiVersion").orNull ?: "1.0"
                        }
                    }
                    repositories { maven { name = "embed"; url = uri(repo) } }
                }
            }
        }
    }
}

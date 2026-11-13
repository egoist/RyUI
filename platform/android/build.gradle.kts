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

// The app's native packages' modules (settings.gradle.kts): built where
// the host is (not in the package, which may be read-only), with the
// host's SDK levels and Java, and its Kotlin to call (dev.asuka): a
// package's build.gradle.kts says only its namespace and its own
// dependencies.
subprojects {
    if (!name.startsWith("native-")) return@subprojects
    layout.buildDirectory.set(rootProject.layout.buildDirectory.dir("native/$name"))
    plugins.withId("com.android.library") {
        extensions.configure<com.android.build.gradle.LibraryExtension> {
            compileSdk = 36
            defaultConfig { minSdk = 33 }
            compileOptions {
                sourceCompatibility = JavaVersion.VERSION_17
                targetCompatibility = JavaVersion.VERSION_17
            }
        }
        dependencies { add("implementation", project(":asuka")) }
    }
    plugins.withId("org.jetbrains.kotlin.android") {
        extensions.configure<org.jetbrains.kotlin.gradle.dsl.KotlinAndroidProjectExtension> { jvmToolchain(17) }
    }
}

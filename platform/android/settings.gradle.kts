// Asuka's Android host: the `asuka` library (decoder, Views host, JNI
// bindings) and `app`, the shell app the Asuka CLI builds for each Asuka app.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "asuka-android"
include(":asuka", ":app", ":asuka-scan", ":asuka-vision", ":asuka-review", ":asuka-push", ":asuka-integrity", ":asuka-agerange", ":asuka-maps", ":asuka-periodic")

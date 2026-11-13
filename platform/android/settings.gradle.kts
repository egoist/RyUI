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
    // A native package's module may declare repositories of its own
    // (then its own are its: google() and mavenCentral() too).
    repositoriesMode.set(RepositoriesMode.PREFER_PROJECT)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "asuka-android"
include(":asuka", ":app", ":asuka-scan", ":asuka-vision", ":asuka-review", ":asuka-push", ":asuka-integrity", ":asuka-agerange", ":asuka-maps", ":asuka-periodic")

// The app's native packages' modules (their android/, the CLI's
// asukaNativeModules: name=directory, ;-separated).
providers.gradleProperty("asukaNativeModules").orNull?.split(';')?.filter { it.isNotEmpty() }?.forEach {
    val (name, dir) = it.split('=', limit = 2)
    include(":$name")
    project(":$name").projectDir = file(dir)
}

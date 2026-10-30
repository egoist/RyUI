// nui's Android host: the `nui` library (decoder, Views host, JNI
// bindings) and `app`, the shell app the nui CLI builds for each nui app.
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
rootProject.name = "nui-android"
include(":nui", ":app", ":nui-scan")

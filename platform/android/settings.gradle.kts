// RyUI's Android host: the `ryui` library (decoder, Views host, JNI
// bindings) and `app`, the shell app the RyUI CLI builds for each RyUI app.
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
rootProject.name = "ryui-android"
include(":ryui", ":app", ":ryui-scan")

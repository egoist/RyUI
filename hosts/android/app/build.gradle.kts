// The shell app around a nui app. The nui CLI passes the app's ID and
// name; in development the Go library is pushed to the app's files
// directory instead of being packaged (see NuiNative.load).
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val appID = providers.gradleProperty("nuiApplicationId").getOrElse("dev.nui.app")
val appName = providers.gradleProperty("nuiAppName").getOrElse("nui")

android {
    namespace = "dev.nui.app"
    compileSdk = 36
    defaultConfig {
        applicationId = appID
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"
        manifestPlaceholders["nuiAppName"] = appName
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":nui"))
}

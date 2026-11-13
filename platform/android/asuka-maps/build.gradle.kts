// asuka/sdk/maps's Google Maps (the Maps SDK for Android; its key is
// asuka.json's android.mapsKey). Only the apps that import asuka/sdk/maps are
// built with it (the Asuka CLI's asukaModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.maps"
    compileSdk = 36
    defaultConfig {
        minSdk = 33
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":asuka"))
    implementation("com.google.android.gms:play-services-maps:20.1.0")
}

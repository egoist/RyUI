// ryui/maps's Google Maps (the Maps SDK for Android; its key is
// ryui.json's android.mapsKey). Only the apps that import ryui/maps are
// built with it (the RyUI CLI's ryuiModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui.maps"
    compileSdk = 36
    defaultConfig {
        minSdk = 26
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":ryui"))
    implementation("com.google.android.gms:play-services-maps:20.1.0")
}

// asuka/sdk/integrity's Play Integrity (standard requests). Only the apps that import asuka/sdk/integrity are built with it (the Asuka CLI's
// asukaModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.integrity"
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
    implementation("com.google.android.play:integrity:1.6.0")
}

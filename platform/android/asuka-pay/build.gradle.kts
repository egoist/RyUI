// asuka/sdk/pay's Google Pay: the Google Pay API (play-services-wallet),
// its PayButton, and the service its sheet calls back for the dynamic
// price updates and the authorization (this module's manifest). Only the
// apps that import asuka/sdk/pay are built with it (the Asuka CLI's
// asukaModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.pay"
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
    // 20.0.0: the Activity Result contracts, and the payment data
    // callbacks on Android.
    implementation("com.google.android.gms:play-services-wallet:20.0.0")
    implementation("androidx.activity:activity:1.10.1")
}

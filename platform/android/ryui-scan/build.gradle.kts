// ryui/scan's reader: ML Kit's barcode scanning, with its model (about
// 2 MB), so it works without Google Play services. Only the apps that
// import ryui/scan are built with it (the RyUI CLI's ryuiModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui.scan"
    compileSdk = 36
    defaultConfig {
        minSdk = 26
        consumerProguardFiles("consumer-rules.pro")
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":ryui"))
    implementation("androidx.camera:camera-core:1.6.2")
    implementation("com.google.mlkit:barcode-scanning:17.3.0")
}

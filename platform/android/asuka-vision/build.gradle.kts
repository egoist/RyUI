// asuka/vision's reader: ML Kit's text recognition (Chinese and Latin
// scripts), face detection and barcode scanning, with their models, so it
// works without Google Play services. Only the apps that import asuka/vision
// are built with it (the Asuka CLI's asukaModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.vision"
    compileSdk = 36
    defaultConfig {
        minSdk = 33
        consumerProguardFiles("consumer-rules.pro")
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":asuka"))
    implementation("com.google.mlkit:text-recognition-chinese:16.0.1")
    implementation("com.google.mlkit:face-detection:16.1.7")
    implementation("com.google.mlkit:barcode-scanning:17.3.0")
}

// asuka/sdk/review's prompt: Google Play's in-app review. Only the apps that
// import asuka/sdk/review are built with it (the Asuka CLI's asukaModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.review"
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
    implementation("com.google.android.play:review:2.0.2")
}

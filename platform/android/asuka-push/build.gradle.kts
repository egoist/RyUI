// asuka/push's Android side: Firebase Cloud Messaging. Only the apps that
// import asuka/push are built with it (the Asuka CLI's asukaModules); the
// CLI writes the project's Firebase settings (google-services.json) as
// resources, as Google's Gradle plugin would.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.push"
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
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("com.google.firebase:firebase-messaging:26.0.1")
}

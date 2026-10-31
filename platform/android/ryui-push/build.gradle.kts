// ryui/push's Android side: Firebase Cloud Messaging. Only the apps that
// import ryui/push are built with it (the RyUI CLI's ryuiModules); the
// CLI writes the project's Firebase settings (google-services.json) as
// resources, as Google's Gradle plugin would.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui.push"
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
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("com.google.firebase:firebase-messaging:26.0.1")
}

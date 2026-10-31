// ryui/review's prompt: Google Play's in-app review. Only the apps that
// import ryui/review are built with it (the RyUI CLI's ryuiModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui.review"
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
    implementation("com.google.android.play:review:2.0.2")
}

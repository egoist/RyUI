// ryui/periodic's WorkManager: periodic work that runs the app's Go code
// in the background. Only the apps that import ryui/periodic are built
// with it (the RyUI CLI's ryuiModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui.periodic"
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
    implementation("androidx.work:work-runtime:2.12.0")
}

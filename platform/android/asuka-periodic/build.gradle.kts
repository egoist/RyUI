// asuka/periodic's WorkManager: periodic work that runs the app's Go code
// in the background. Only the apps that import asuka/periodic are built
// with it (the Asuka CLI's asukaModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.periodic"
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
    implementation("androidx.work:work-runtime:2.12.0")
}

// asuka/sdk/location's background updates and geofences through Google Play
// services (the Fused Location Provider, GeofencingClient). Only the apps
// that call the package's background part are built with it (the Asuka
// CLI's asukaModules); on a device without Play services, the host uses the
// location manager instead.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.location"
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
    implementation("com.google.android.gms:play-services-location:21.3.0")
}

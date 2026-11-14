// asuka/sdk/purchase's Android side: Google Play Billing. Only the apps that
// import asuka/sdk/purchase are built with it (the Asuka CLI's asukaModules);
// its library's manifest declares com.android.vending.BILLING.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.asuka.purchase"
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
    implementation("com.android.billingclient:billing:9.1.0")
}

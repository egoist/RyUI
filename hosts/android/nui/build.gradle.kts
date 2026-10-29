plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.nui"
    compileSdk = 36
    defaultConfig {
        minSdk = 26
        consumerProguardFiles("consumer-rules.pro")
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    testOptions.unitTests.isReturnDefaultValues = true
}

kotlin { jvmToolchain(17) }

dependencies {
    api("com.google.android.material:material:1.13.0")
    api("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.recyclerview:recyclerview:1.4.0")
    implementation("androidx.activity:activity:1.10.1") // predictive back, the photo picker
    implementation("androidx.browser:browser:1.9.0") // in-app browser tabs
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")
    // Video: ExoPlayer and its PlayerView (controls), HLS streams.
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("androidx.media3:media3-exoplayer-hls:1.11.1")
    implementation("androidx.media3:media3-ui:1.11.1")
    testImplementation("junit:junit:4.13.2")
}

plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui"
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
    api("com.google.android.material:material:1.14.0")
    api("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.recyclerview:recyclerview:1.4.0")
    implementation("androidx.activity:activity:1.10.1") // predictive back, the photo picker
    implementation("androidx.browser:browser:1.9.0") // in-app browser tabs
    implementation("androidx.biometric:biometric:1.1.0") // ryui/biometric's prompt
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")
    // Video: ExoPlayer and its PlayerView (controls), HLS streams.
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("androidx.media3:media3-exoplayer-hls:1.11.1")
    implementation("androidx.media3:media3-ui:1.11.1")
    // ryui/camera's View: CameraX's preview and photo capture.
    implementation("androidx.exifinterface:exifinterface:1.4.1") // a photo's orientation
    implementation("androidx.webkit:webkit:1.14.0") // WebView's document-start scripts
    implementation("androidx.camera:camera-camera2:1.6.2")
    implementation("androidx.camera:camera-lifecycle:1.6.2")
    implementation("androidx.camera:camera-view:1.6.2")
    implementation("androidx.camera:camera-video:1.6.2")
    testImplementation("junit:junit:4.13.2")
}

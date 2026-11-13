plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
    `maven-publish`
}

fun prop(name: String) = providers.gradleProperty(name).orNull

// asuka build --platform android --aar: an app to embed in another, this library with
// the app's Go code (asukaJniLibs) and assets, and AsukaActivity declared,
// published to a Maven repository (asukaMavenRepo) so its dependencies come
// with it.
val embed = prop("asukaMavenRepo") != null

android {
    namespace = "dev.asuka"
    compileSdk = 36
    defaultConfig {
        minSdk = 33
        consumerProguardFiles("consumer-rules.pro")
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    testOptions.unitTests.isReturnDefaultValues = true
    if (embed) {
        sourceSets.getByName("main") {
            prop("asukaJniLibs")?.let { jniLibs.srcDir(it) }
            prop("asukaAssets")?.let { assets.srcDir(it) }
            prop("asukaManifest")?.let { manifest.srcFile(it) }
        }
        publishing { singleVariant("release") }
    }
}

if (embed) afterEvaluate {
    publishing {
        publications {
            create<MavenPublication>("release") {
                from(components["release"])
                groupId = prop("asukaGroup") ?: "dev.asuka"
                artifactId = prop("asukaArtifact") ?: "app"
                version = prop("asukaVersion") ?: "1.0"
            }
        }
        repositories { maven { name = "embed"; url = uri(prop("asukaMavenRepo")!!) } }
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    api("com.google.android.material:material:1.14.0")
    api("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.recyclerview:recyclerview:1.4.0")
    implementation("androidx.activity:activity:1.10.1") // predictive back, the photo picker
    implementation("androidx.browser:browser:1.9.0") // in-app browser tabs
    implementation("androidx.biometric:biometric:1.1.0") // asuka/biometric's prompt
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")
    // Video: ExoPlayer and its PlayerView (controls), HLS streams.
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("androidx.media3:media3-exoplayer-hls:1.11.1")
    implementation("androidx.media3:media3-ui:1.11.1")
    // asuka/camera's View: CameraX's preview and photo capture.
    implementation("androidx.exifinterface:exifinterface:1.4.1") // a photo's orientation
    implementation("androidx.webkit:webkit:1.14.0") // WebView's document-start scripts
    implementation("androidx.camera:camera-camera2:1.6.2")
    implementation("androidx.camera:camera-lifecycle:1.6.2")
    implementation("androidx.camera:camera-view:1.6.2")
    implementation("androidx.camera:camera-video:1.6.2")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303") // the real org.json: android.jar's is a stub
}

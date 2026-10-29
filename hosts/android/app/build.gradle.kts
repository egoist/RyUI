// The shell app around a nui app. The nui CLI passes the app's ID, name,
// version, signing key and (for release builds) the Go library for each
// ABI, the assets and the icons; in development the Go library is pushed
// to the app's files directory instead of being packaged (see
// NuiNative.load).
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

fun prop(name: String) = providers.gradleProperty(name).orNull

android {
    namespace = "dev.nui.app"
    compileSdk = 36
    defaultConfig {
        applicationId = prop("nuiApplicationId") ?: "dev.nui.app"
        minSdk = 26
        targetSdk = 36
        versionCode = (prop("nuiVersionCode") ?: "1").toInt()
        versionName = prop("nuiVersionName") ?: "1.0"
        manifestPlaceholders["nuiAppName"] = prop("nuiAppName") ?: "nui"
        manifestPlaceholders["nuiIcon"] = prop("nuiIcon") ?: "@android:drawable/sym_def_app_icon"
    }
    signingConfigs {
        create("release") {
            prop("nuiKeystore")?.let {
                storeFile = file(it)
                storePassword = prop("nuiKeystorePassword")
                keyAlias = prop("nuiKeyAlias")
                keyPassword = prop("nuiKeyPassword") ?: prop("nuiKeystorePassword")
            }
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            // The Go libraries are stripped; Go's stack traces don't need
            // the symbols (they're in its own tables).
            ndk { debugSymbolLevel = "NONE" }
            if (prop("nuiKeystore") != null) signingConfig = signingConfigs.getByName("release")
        }
    }
    sourceSets {
        getByName("release") {
            prop("nuiJniLibs")?.let { jniLibs.srcDir(it) }
            prop("nuiAssets")?.let { assets.srcDir(it) }
            prop("nuiRes")?.let { res.srcDir(it) }
            prop("nuiManifest")?.let { manifest.srcFile(it) }
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":nui"))
}

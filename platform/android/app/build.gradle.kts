// The shell app around a RyUI app. The RyUI CLI passes the app's ID, name,
// version, signing key and (for release builds) the Go library for each
// ABI, the assets and the icons; in development the Go library is pushed
// to the app's files directory instead of being packaged (see
// RyUINative.load).
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

fun prop(name: String) = providers.gradleProperty(name).orNull

android {
    namespace = "dev.ryui.app"
    compileSdk = 36
    defaultConfig {
        applicationId = prop("ryuiApplicationId") ?: "dev.ryui.app"
        minSdk = 26
        targetSdk = 36
        versionCode = (prop("ryuiVersionCode") ?: "1").toInt()
        versionName = prop("ryuiVersionName") ?: "1.0"
        manifestPlaceholders["ryuiAppName"] = prop("ryuiAppName") ?: "ryui"
        manifestPlaceholders["ryuiIcon"] = prop("ryuiIcon") ?: "@android:drawable/sym_def_app_icon"
    }
    signingConfigs {
        create("release") {
            prop("ryuiKeystore")?.let {
                storeFile = file(it)
                storePassword = prop("ryuiKeystorePassword")
                keyAlias = prop("ryuiKeyAlias")
                keyPassword = prop("ryuiKeyPassword") ?: prop("ryuiKeystorePassword")
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
            if (prop("ryuiKeystore") != null) signingConfig = signingConfigs.getByName("release")
        }
    }
    sourceSets {
        // The app's manifest (ryui.json's permissions, scheme and links),
        // over the debug one in development.
        getByName("debug") {
            prop("ryuiManifest")?.let { manifest.srcFile(it) }
            prop("ryuiRes")?.let { res.srcDir(it) } // the splash, Firebase's settings
        }
        getByName("release") {
            prop("ryuiJniLibs")?.let { jniLibs.srcDir(it) }
            prop("ryuiAssets")?.let { assets.srcDir(it) }
            prop("ryuiRes")?.let { res.srcDir(it) }
            prop("ryuiManifest")?.let { manifest.srcFile(it) }
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":ryui"))
    // The optional modules the app's packages need (the CLI's ryuiModules).
    prop("ryuiModules")?.split(',')?.filter { it.isNotEmpty() }?.forEach { implementation(project(":ryui-$it")) }
}

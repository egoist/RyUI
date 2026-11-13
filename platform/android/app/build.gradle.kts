// The shell app around a Asuka app. The Asuka CLI passes the app's ID, name,
// version, signing key and (for release builds) the Go library for each
// ABI, the assets and the icons; in development the Go library is pushed
// to the app's files directory instead of being packaged (see
// AsukaNative.load).
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

fun prop(name: String) = providers.gradleProperty(name).orNull

android {
    namespace = "dev.asuka.app"
    compileSdk = 36
    // The NDK the CLI built the Go libraries with (the SDK's ndk/<version>):
    // Gradle strips them with it, and keeps their symbols
    // (native-debug-symbols.zip).
    prop("asukaNdkVersion")?.let { ndkVersion = it }
    defaultConfig {
        applicationId = prop("asukaApplicationId") ?: "dev.asuka.app"
        minSdk = 33
        targetSdk = 36
        versionCode = (prop("asukaVersionCode") ?: "1").toInt()
        versionName = prop("asukaVersionName") ?: "1.0"
        manifestPlaceholders["asukaAppName"] = prop("asukaAppName") ?: "asuka"
        manifestPlaceholders["asukaIcon"] = prop("asukaIcon") ?: "@android:drawable/sym_def_app_icon"
    }
    signingConfigs {
        create("release") {
            prop("asukaKeystore")?.let {
                storeFile = file(it)
                storePassword = prop("asukaKeystorePassword")
                keyAlias = prop("asukaKeyAlias")
                keyPassword = prop("asukaKeyPassword") ?: prop("asukaKeystorePassword")
            }
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            // Go's stack traces don't need the libraries' symbols (they're
            // in its own tables), a native crash's frames do: Gradle strips
            // the libraries it packages and keeps their symbol tables, in
            // the .aab for Google Play (and native-debug-symbols.zip).
            ndk { debugSymbolLevel = "SYMBOL_TABLE" }
            if (prop("asukaKeystore") != null) signingConfig = signingConfigs.getByName("release")
        }
    }
    // Language splits would leave out the Material strings of a language the
    // app chooses in-app (ui.SetLocale) rather than the device's.
    bundle { language { enableSplit = false } }
    sourceSets {
        // The app's manifest (asuka.json's permissions, scheme and links),
        // over the debug one in development.
        getByName("debug") {
            prop("asukaManifest")?.let { manifest.srcFile(it) }
            prop("asukaRes")?.let { res.srcDir(it) } // the splash, Firebase's settings
        }
        getByName("release") {
            prop("asukaJniLibs")?.let { jniLibs.srcDir(it) }
            prop("asukaAssets")?.let { assets.srcDir(it) }
            prop("asukaRes")?.let { res.srcDir(it) }
            prop("asukaManifest")?.let { manifest.srcFile(it) }
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":asuka"))
    // The optional modules the app's packages need (the CLI's asukaModules).
    prop("asukaModules")?.split(',')?.filter { it.isNotEmpty() }?.forEach { implementation(project(":asuka-$it")) }
}

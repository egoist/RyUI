// ryui/agerange's Play Age Signals. Only the apps that import ryui/agerange are built with it (the RyUI CLI's
// ryuiModules).
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.ryui.agerange"
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
    implementation("com.google.android.play:age-signals:0.0.4")
}

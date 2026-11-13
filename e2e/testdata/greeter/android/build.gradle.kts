// The greeter package's Kotlin for Android: a library module the Asuka
// CLI builds into the apps that import the package. Its dependencies are
// Maven's, as any module's: Gson, which its reply goes through.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.github.egoist.asuka.e2e.testdata.greeter"
}

dependencies {
    implementation("com.google.code.gson:gson:2.11.0")
}

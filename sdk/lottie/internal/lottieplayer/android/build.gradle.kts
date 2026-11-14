// asuka/sdk/lottie's player for Android: lottie-android, in a library
// module the Asuka CLI builds into the apps that import asuka/sdk/lottie.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.github.egoist.asuka.sdk.lottie.internal.lottieplayer"
}

dependencies {
    implementation("com.airbnb.android:lottie:6.7.1")
}

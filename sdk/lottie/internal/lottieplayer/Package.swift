// swift-tools-version:5.9
// asuka/sdk/lottie's player for iOS and macOS: lottie-ios (its binary
// distribution, lottie-spm). The Asuka CLI builds it with the apps that
// import asuka/sdk/lottie.
import PackageDescription

let package = Package(
    name: "LottieplayerNative",
    platforms: [.iOS("16.0"), .macOS("11.0")],
    products: [.library(name: "LottieplayerNative", targets: ["LottieplayerNative"])],
    dependencies: [
        .package(url: "https://github.com/egoist/asuka", from: "0.1.0"),
        .package(url: "https://github.com/airbnb/lottie-spm.git", from: "4.5.0"),
    ],
    targets: [
        .target(name: "LottieplayerNative", dependencies: [
            .product(name: "Asuka", package: "asuka"),
            .product(name: "Lottie", package: "lottie-spm"),
        ], path: "swift"),
    ]
)

// swift-tools-version:5.9
// The segmented control's Swift, for iOS and macOS: the Asuka CLI builds it
// with the apps that import it.
import PackageDescription

let package = Package(
    name: "SegmentedNative",
    platforms: [.iOS("16.0"), .macOS("11.0")],
    products: [.library(name: "SegmentedNative", targets: ["SegmentedNative"])],
    dependencies: [
        .package(url: "https://github.com/egoist/asuka", from: "0.1.0"),
    ],
    targets: [
        .target(name: "SegmentedNative", dependencies: [.product(name: "Asuka", package: "asuka")], path: "swift"),
    ]
)

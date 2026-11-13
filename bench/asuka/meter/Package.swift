// swift-tools-version:5.9
// The meter's Swift: iOS only (the benchmarks run on phones).
import PackageDescription

let package = Package(
    name: "MeterNative",
    platforms: [.iOS("16.0")],
    products: [.library(name: "MeterNative", targets: ["MeterNative"])],
    dependencies: [
        .package(url: "https://github.com/egoist/asuka", from: "0.1.0"),
    ],
    targets: [
        .target(name: "MeterNative", dependencies: [.product(name: "Asuka", package: "asuka")], path: "swift"),
    ]
)

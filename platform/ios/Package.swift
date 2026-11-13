// swift-tools-version:5.9
// Asuka's iOS host. The Asuka CLI compiles these sources straight into the
// app (with App/); this package is for editing them in Xcode and for the
// decoder's tests (`swift test`, on macOS).
import PackageDescription

let package = Package(
    name: "AsukaHost",
    platforms: [.iOS(.v16), .macOS(.v13)],
    products: [.library(name: "AsukaHost", targets: ["AsukaHost"])],
    targets: [
        .target(name: "CAsuka"),
        .target(name: "AsukaOps"),
        .target(name: "AsukaHost", dependencies: ["AsukaOps", "CAsuka"]),
        .testTarget(name: "AsukaOpsTests", dependencies: ["AsukaOps"]),
    ]
)

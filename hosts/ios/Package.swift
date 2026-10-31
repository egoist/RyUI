// swift-tools-version:5.9
// RyUI's iOS host. The RyUI CLI compiles these sources straight into the
// app (with App/); this package is for editing them in Xcode and for the
// decoder's tests (`swift test`, on macOS).
import PackageDescription

let package = Package(
    name: "RyUIHost",
    platforms: [.iOS(.v16), .macOS(.v13)],
    products: [.library(name: "RyUIHost", targets: ["RyUIHost"])],
    targets: [
        .target(name: "CRyUI"),
        .target(name: "RyUIOps"),
        .target(name: "RyUIHost", dependencies: ["RyUIOps", "CRyUI"]),
        .testTarget(name: "RyUIOpsTests", dependencies: ["RyUIOps"]),
    ]
)

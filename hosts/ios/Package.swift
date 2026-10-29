// swift-tools-version:5.9
// nui's iOS host. The nui CLI compiles these sources straight into the
// app (with App/); this package is for editing them in Xcode and for the
// decoder's tests (`swift test`, on macOS).
import PackageDescription

let package = Package(
    name: "NuiHost",
    platforms: [.iOS(.v16), .macOS(.v13)],
    products: [.library(name: "NuiHost", targets: ["NuiHost"])],
    targets: [
        .target(name: "CNui"),
        .target(name: "NuiOps"),
        .target(name: "NuiHost", dependencies: ["NuiOps", "CNui"]),
        .testTarget(name: "NuiOpsTests", dependencies: ["NuiOps"]),
    ]
)

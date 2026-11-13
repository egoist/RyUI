// swift-tools-version:5.9
// The greeter package's Swift, for iOS and macOS: the Asuka CLI builds it
// with the apps that import the package. It depends on a package of its
// own, GreeterKit (beside it), as on any Swift package.
import PackageDescription

let package = Package(
    name: "GreeterNative",
    platforms: [.iOS("16.0"), .macOS("11.0")],
    products: [.library(name: "GreeterNative", targets: ["GreeterNative"])],
    dependencies: [
        .package(url: "https://github.com/egoist/asuka", from: "0.1.0"),
        .package(path: "GreeterKit"),
    ],
    targets: [
        .target(name: "GreeterNative", dependencies: [
            .product(name: "Asuka", package: "asuka"),
            .product(name: "GreeterKit", package: "GreeterKit"),
        ], path: "swift"),
    ]
)

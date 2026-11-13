// swift-tools-version:5.9
// The kitchen package's Swift, for iOS and macOS: the Asuka CLI builds it
// with the apps that import the package (asuka native).
import PackageDescription

let package = Package(
    name: "KitchenNative",
    platforms: [.iOS("16.0"), .macOS("11.0")],
    products: [.library(name: "KitchenNative", targets: ["KitchenNative"])],
    dependencies: [
        .package(url: "https://github.com/egoist/asuka", from: "0.1.0"),
    ],
    targets: [
        .target(name: "KitchenNative", dependencies: [.product(name: "Asuka", package: "asuka")], path: "swift"),
    ]
)

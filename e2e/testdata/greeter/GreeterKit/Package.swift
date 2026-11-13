// swift-tools-version:5.9
// GreeterKit is the greeter plugin's Swift package (its manifest's
// packages, by path): Swift with a resource, over a C target, as a
// third-party package's might be.
import PackageDescription

let package = Package(
    name: "GreeterKit",
    platforms: [.iOS("16.0"), .macOS("11.0")],
    products: [.library(name: "GreeterKit", targets: ["GreeterKit"])],
    targets: [
        .target(name: "CGreet"),
        .target(name: "GreeterKit", dependencies: ["CGreet"], resources: [.process("Resources")]),
    ]
)

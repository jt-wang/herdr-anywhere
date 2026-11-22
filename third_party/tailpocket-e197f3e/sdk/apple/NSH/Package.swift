// swift-tools-version:5.10
import PackageDescription

// The NSH adapter: registers the bridge on an NshAppView. NativeShell.xcframework is the one your
// app already embeds (nsh's ~/.nsh/embed/ios); link or copy it next to this file.
let package = Package(
    name: "TailPocketNSH",
    platforms: [.iOS(.v15)],
    products: [.library(name: "TailPocketNSH", targets: ["TailPocketNSH"])],
    dependencies: [.package(name: "TailPocket", path: "../../..")],
    targets: [
        .binaryTarget(name: "NativeShell", path: "NativeShell.xcframework"),
        .target(name: "TailPocketNSH", dependencies: [
            "NativeShell",
            .product(name: "TailPocket", package: "TailPocket"),
            .product(name: "TailPocketUI", package: "TailPocket"),
        ]),
        .testTarget(name: "TailPocketNSHTests", dependencies: ["TailPocketNSH"]),
    ]
)

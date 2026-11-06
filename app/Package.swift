// swift-tools-version: 5.9
import PackageDescription

// Pouple's native shell. The logic that can be tested without a screen lives in PoupleCore (core supervision,
// API client, snapshot stream, placement, markdown, visibility); Pouple holds the windows, menu bar item and views.
let package = Package(
    name: "Pouple",
    platforms: [.macOS(.v14)],
    targets: [
        .target(name: "PoupleCore"),
        .executableTarget(name: "Pouple", dependencies: ["PoupleCore"]),
        .testTarget(name: "PoupleCoreTests", dependencies: ["PoupleCore"]),
    ]
)

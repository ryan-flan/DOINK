// swift-tools-version:5.9
// DOINK's macOS menu bar app. Build the app bundle with ./build.sh, which
// also builds the Go companion it runs; `swift build` alone only compiles
// the Swift part (set DOINK_BIN to a doink binary to run it that way).
import PackageDescription

let package = Package(
    name: "DOINKBar",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(name: "DOINKBar", path: "Sources/DOINKBar")
    ]
)
